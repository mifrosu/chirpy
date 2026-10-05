package auth

import "testing"

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
