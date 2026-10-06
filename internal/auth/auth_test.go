package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func TestHashAndCheckPassword(t *testing.T) {
	hash, err := HashPassword("hunter2")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == "hunter2" {
		t.Fatal("hash must not equal the plaintext password")
	}

	ok, err := CheckPasswordHash("hunter2", hash)
	if err != nil || !ok {
		t.Fatalf("correct password: ok=%v err=%v", ok, err)
	}

	ok, err = CheckPasswordHash("wrong", hash)
	if err != nil || ok {
		t.Fatalf("wrong password: ok=%v err=%v", ok, err)
	}
}

func TestMakeJWT(t *testing.T) {
	id := uuid.New()
	secret := "s3cret"

	signed, err := MakeJWT(id, secret, time.Hour)
	if err != nil {
		t.Fatalf("MakeJWT: %v", err)
	}

	var claims jwt.RegisteredClaims
	token, err := jwt.ParseWithClaims(signed, &claims, func(*jwt.Token) (any, error) {
		return []byte(secret), nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	if err != nil || !token.Valid {
		t.Fatalf("parse: valid=%v err=%v", token != nil && token.Valid, err)
	}
	if claims.Subject != id.String() {
		t.Errorf("subject = %q, want %q", claims.Subject, id)
	}
	if claims.Issuer != "chirpy-access" {
		t.Errorf("issuer = %q, want chirpy-access", claims.Issuer)
	}

	if _, err := jwt.ParseWithClaims(signed, &jwt.RegisteredClaims{}, func(*jwt.Token) (any, error) {
		return []byte("wrong"), nil
	}); err == nil {
		t.Error("token verified with the wrong secret")
	}

	expired, err := MakeJWT(id, secret, -time.Minute)
	if err != nil {
		t.Fatalf("MakeJWT expired: %v", err)
	}
	if _, err := jwt.ParseWithClaims(expired, &jwt.RegisteredClaims{}, func(*jwt.Token) (any, error) {
		return []byte(secret), nil
	}); err == nil {
		t.Error("expired token was accepted")
	}
}

func TestValidateJWT(t *testing.T) {
	id := uuid.New()
	secret := "s3cret"

	valid, err := MakeJWT(id, secret, time.Hour)
	if err != nil {
		t.Fatalf("MakeJWT: %v", err)
	}
	expired, err := MakeJWT(id, secret, -time.Minute)
	if err != nil {
		t.Fatalf("MakeJWT expired: %v", err)
	}
	wrongIssuer, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Issuer:    "someone-else",
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		Subject:   id.String(),
	}).SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign wrong issuer: %v", err)
	}
	badSubject, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Issuer:    accessTokenIssuer,
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		Subject:   "not-a-uuid",
	}).SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign bad subject: %v", err)
	}
	noneAlg, err := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.RegisteredClaims{
		Issuer:    accessTokenIssuer,
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		Subject:   id.String(),
	}).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("sign none: %v", err)
	}

	tests := []struct {
		name, token, secret string
		wantErr             bool
	}{
		{"valid", valid, secret, false},
		{"wrong secret", valid, "wrong", true},
		{"expired", expired, secret, true},
		{"wrong issuer", wrongIssuer, secret, true},
		{"non-uuid subject", badSubject, secret, true},
		{"alg none", noneAlg, secret, true},
		{"garbage", "not.a.jwt", secret, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ValidateJWT(tt.token, tt.secret)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != id {
				t.Errorf("id = %v, want %v", got, id)
			}
			if tt.wantErr && got != uuid.Nil {
				t.Errorf("id = %v, want uuid.Nil on error", got)
			}
		})
	}
}
