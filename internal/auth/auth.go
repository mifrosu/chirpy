// Package auth provides password hashing helpers.
package auth

import "github.com/alexedwards/argon2id"

// HashPassword returns an argon2id hash of password using the default params.
func HashPassword(password string) (string, error) {
	return argon2id.CreateHash(password, argon2id.DefaultParams)
}

// CheckPasswordHash reports whether password matches the argon2id hash.
func CheckPasswordHash(password, hash string) (bool, error) {
	return argon2id.ComparePasswordAndHash(password, hash)
}
