// Package auth provides password hashing, JWT and request auth helpers.
package auth

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/alexedwards/argon2id"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// accessTokenIssuer is the issuer claim set by MakeJWT and required by ValidateJWT.
const accessTokenIssuer = "chirpy-access"

// HashPassword returns an argon2id hash of password using the default params.
func HashPassword(password string) (string, error) {
	return argon2id.CreateHash(password, argon2id.DefaultParams)
}

// CheckPasswordHash reports whether password matches the argon2id hash.
func CheckPasswordHash(password, hash string) (bool, error) {
	return argon2id.ComparePasswordAndHash(password, hash)
}

// MakeJWT returns an HS256-signed JWT for userID, signed with tokenSecret,
// that expires expiresIn after it is issued. The user ID is the subject claim.
func MakeJWT(userID uuid.UUID, tokenSecret string, expiresIn time.Duration) (string, error) {
	now := time.Now().UTC()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Issuer:    accessTokenIssuer,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(expiresIn)),
		Subject:   userID.String(),
	})
	return token.SignedString([]byte(tokenSecret))
}

// ValidateJWT verifies tokenString's HS256 signature, expiry and issuer using
// tokenSecret and returns the user ID from its subject claim.
func ValidateJWT(tokenString, tokenSecret string) (uuid.UUID, error) {
	var claims jwt.RegisteredClaims
	_, err := jwt.ParseWithClaims(tokenString, &claims, func(*jwt.Token) (any, error) {
		return []byte(tokenSecret), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(accessTokenIssuer),
		jwt.WithExpirationRequired())
	if err != nil {
		return uuid.Nil, err
	}
	id, err := uuid.Parse(claims.Subject)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid subject claim: %w", err)
	}
	return id, nil
}

// GetBearerToken returns the token from an "Authorization: Bearer TOKEN"
// header, with the scheme and surrounding whitespace removed. It returns an
// error if the header is missing, isn't a Bearer credential, or has no token.
func GetBearerToken(headers http.Header) (string, error) {
	value := strings.TrimSpace(headers.Get("Authorization"))
	if value == "" {
		return "", errors.New("missing Authorization header")
	}
	scheme, token, _ := strings.Cut(value, " ")
	if !strings.EqualFold(scheme, "Bearer") {
		return "", errors.New("Authorization header is not a Bearer token")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return "", errors.New("Authorization header has an empty Bearer token")
	}
	return token, nil
}
