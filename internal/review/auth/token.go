package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// tokenBytes is the entropy size of an issued secret before base64 encoding.
const tokenBytes = 32

// tokenPrefix marks an issued plaintext token so it is recognizable in logs
// and config (and so a stray non-token string is rejected early).
const tokenPrefix = "rvw_"

// ErrTokenMalformed is returned when a presented token is empty or does not
// carry the expected prefix.
var ErrTokenMalformed = errors.New("auth: malformed token")

// randRead and bcryptHash are package seams over the crypto primitives so the
// otherwise-unreachable error branches can be exercised in tests. Production
// code uses the real implementations.
var (
	randRead   = rand.Read
	bcryptHash = bcrypt.GenerateFromPassword
)

// GenerateToken returns a fresh cryptographically-random plaintext token. The
// plaintext is shown to the operator exactly once at issuance; only its hash
// is persisted (design D5.3, OQ1 print-once).
func GenerateToken() (string, error) {
	buf := make([]byte, tokenBytes)
	if _, err := randRead(buf); err != nil {
		return "", fmt.Errorf("auth: generate token: %w", err)
	}
	return tokenPrefix + base64.RawURLEncoding.EncodeToString(buf), nil
}

// HashToken returns the bcrypt hash of a plaintext token, suitable for storage
// in the users file.
func HashToken(plaintext string) (string, error) {
	if err := validateTokenShape(plaintext); err != nil {
		return "", err
	}
	hash, err := bcryptHash([]byte(plaintext), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("auth: hash token: %w", err)
	}
	return string(hash), nil
}

// VerifyToken reports whether the plaintext matches the stored bcrypt hash.
// A mismatch (or a malformed token) returns false with no error; an error is
// returned only when the stored hash itself is unusable.
func VerifyToken(plaintext, hash string) (bool, error) {
	if validateTokenShape(plaintext) != nil {
		return false, nil
	}
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(plaintext))
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, bcrypt.ErrMismatchedHashAndPassword):
		return false, nil
	default:
		return false, fmt.Errorf("auth: verify token: %w", err)
	}
}

// validateTokenShape rejects empty or unprefixed tokens before any crypto work.
func validateTokenShape(plaintext string) error {
	if plaintext == "" || !strings.HasPrefix(plaintext, tokenPrefix) {
		return ErrTokenMalformed
	}
	return nil
}
