package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// tokenBytes is the entropy size of an issued secret before base64 encoding.
const tokenBytes = 32

// tokenPrefix marks an issued plaintext token so it is recognizable in logs
// and config (and so a stray non-token string is rejected early).
const tokenPrefix = "rvw_"

// argon2id parameters for token-at-rest hashing. These follow OWASP guidance
// (m=64MiB, t=1, p=4) and produce a 32-byte derived key. They are encoded into
// every PHC string so verification re-derives with the exact params used at
// hash time, allowing the cost to be raised later without invalidating old
// hashes.
const (
	argonTime    = 1
	argonMemory  = 64 * 1024 // KiB => 64 MiB
	argonThreads = 4
	argonKeyLen  = 32
	argonSaltLen = 16
)

// argonVersion mirrors argon2.Version; pinned as a const so the PHC string is
// stable and verifiable.
const argonVersion = argon2.Version

// ErrTokenMalformed is returned when a presented token is empty or does not
// carry the expected prefix.
var ErrTokenMalformed = errors.New("auth: malformed token")

// ErrHashMalformed is returned by VerifyToken when the stored hash is not a
// well-formed argon2id PHC string.
var ErrHashMalformed = errors.New("auth: malformed hash")

// randRead and argonKey are package seams over the crypto primitives so the
// otherwise-unreachable error branches can be exercised in tests. Production
// code uses the real implementations.
var (
	randRead = rand.Read
	argonKey = argon2.IDKey
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

// HashToken returns an argon2id PHC-encoded hash of a plaintext token, suitable
// for storage in the users file. A fresh random salt is generated per call, so
// hashing the same token twice yields distinct strings.
func HashToken(plaintext string) (string, error) {
	if err := validateTokenShape(plaintext); err != nil {
		return "", err
	}
	salt := make([]byte, argonSaltLen)
	if _, err := randRead(salt); err != nil {
		return "", fmt.Errorf("auth: hash token: %w", err)
	}
	key := argonKey([]byte(plaintext), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return encodeHash(salt, key), nil
}

// VerifyToken reports whether the plaintext matches the stored argon2id hash.
// A mismatch (or a malformed token) returns false with no error; an error is
// returned only when the stored hash itself is unusable.
func VerifyToken(plaintext, hash string) (bool, error) {
	if validateTokenShape(plaintext) != nil {
		return false, nil
	}
	params, salt, want, err := decodeHash(hash)
	if err != nil {
		return false, fmt.Errorf("auth: verify token: %w", err)
	}
	got := argonKey([]byte(plaintext), salt, params.time, params.memory, params.threads, uint32(len(want)))
	if subtle.ConstantTimeCompare(got, want) == 1 {
		return true, nil
	}
	return false, nil
}

// argonParams holds the cost parameters parsed from a PHC string.
type argonParams struct {
	memory  uint32
	time    uint32
	threads uint8
}

// encodeHash renders salt and derived key into a standard argon2id PHC string:
//
//	$argon2id$v=19$m=65536,t=1,p=4$<b64-salt>$<b64-key>
func encodeHash(salt, key []byte) string {
	b64 := base64.RawStdEncoding.EncodeToString
	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argonVersion, argonMemory, argonTime, argonThreads,
		b64(salt), b64(key),
	)
}

// decodeHash parses an argon2id PHC string back into its params, salt, and key.
func decodeHash(hash string) (argonParams, []byte, []byte, error) {
	parts := strings.Split(hash, "$")
	// Leading "$" yields an empty first field: ["", "argon2id", "v=..", "m=..", salt, key]
	if len(parts) != 6 || parts[1] != "argon2id" {
		return argonParams{}, nil, nil, ErrHashMalformed
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argonVersion {
		return argonParams{}, nil, nil, ErrHashMalformed
	}

	var p argonParams
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.memory, &p.time, &p.threads); err != nil {
		return argonParams{}, nil, nil, ErrHashMalformed
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return argonParams{}, nil, nil, ErrHashMalformed
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(key) == 0 {
		return argonParams{}, nil, nil, ErrHashMalformed
	}

	return p, salt, key, nil
}

// validateTokenShape rejects empty or unprefixed tokens before any crypto work.
func validateTokenShape(plaintext string) error {
	if plaintext == "" || !strings.HasPrefix(plaintext, tokenPrefix) {
		return ErrTokenMalformed
	}
	return nil
}
