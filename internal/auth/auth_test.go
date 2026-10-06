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
