// Package auth provides password hashing, JWT and request auth helpers.
package auth

import (
	"crypto/rand"
	"encoding/hex"
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
	return getAuthorizationCredential(headers, "Bearer")
}

// GetAPIKey returns the key from an "Authorization: ApiKey THE_KEY" header,
// with the scheme and surrounding whitespace removed. It returns an error if
// the header is missing, isn't an ApiKey credential, or has no key.
func GetAPIKey(headers http.Header) (string, error) {
	return getAuthorizationCredential(headers, "ApiKey")
}

// getAuthorizationCredential returns the credential from an Authorization
// header of the form "<scheme> <credential>". The scheme is matched
// case-insensitively.
func getAuthorizationCredential(headers http.Header, wantScheme string) (string, error) {
	value := strings.TrimSpace(headers.Get("Authorization"))
	if value == "" {
		return "", errors.New("missing Authorization header")
	}
	scheme, credential, _ := strings.Cut(value, " ")
	if !strings.EqualFold(scheme, wantScheme) {
		return "", fmt.Errorf("Authorization header is not an %s credential", wantScheme)
	}
	credential = strings.TrimSpace(credential)
	if credential == "" {
		return "", fmt.Errorf("Authorization header has an empty %s credential", wantScheme)
	}
	return credential, nil
}

// MakeRefreshToken returns a random 256-bit token as a 64-character hex string.
func MakeRefreshToken() string {
	b := make([]byte, 32)
	// Since Go 1.24 crypto/rand.Read never returns an error: a failure of the
	// system random source crashes the program instead.
	rand.Read(b)
	return hex.EncodeToString(b)
}
