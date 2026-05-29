package auth

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// User is one entry in the admin-managed users file. The token plaintext is
// never persisted — only TokenHash (argon2id) is stored.
type User struct {
	Email     string `yaml:"email"`
	Role      Role   `yaml:"role"`
	TokenHash string `yaml:"token_hash"`
	CreatedAt string `yaml:"created_at"`
}

// UsersFile is the on-disk shape of ~/.agents/review/users.yaml.
type UsersFile struct {
	SchemaVersion int    `yaml:"schema_version"`
	Users         []User `yaml:"users"`
}

// usersSchemaVersion is the current users-file schema version.
const usersSchemaVersion = 1

// Seams over OS / library primitives so the otherwise-unreachable error
// branches (marshal failure, temp-file creation/IO failure) can be exercised
// in tests. Production code uses the real implementations.
var (
	// userHomeDir backs DefaultUsersPath's home resolution.
	userHomeDir = os.UserHomeDir
	// yamlMarshal backs Save's serialization.
	yamlMarshal = yaml.Marshal
	// createTemp backs Save's atomic temp-file creation. It returns the
	// tempFile abstraction so write/chmod/close failures are injectable.
	createTemp = func(dir, pattern string) (tempFile, error) {
		return os.CreateTemp(dir, pattern)
	}
)

// tempFile is the subset of *os.File that Save relies on, narrowed to a seam
// so a fake can force the write/chmod/close error branches.
type tempFile interface {
	Name() string
	Chmod(os.FileMode) error
	Write([]byte) (int, error)
	Close() error
}

var (
	// ErrUserNotFound is returned when a lookup matches no user.
	ErrUserNotFound = errors.New("auth: user not found")
	// ErrUserExists is returned when adding a user whose email already exists.
	ErrUserExists = errors.New("auth: user already exists")
)

// DefaultUsersPath returns ~/.agents/review/users.yaml. It honors the
// AGENTS_HOME override (used by tests and non-standard installs) before falling
// back to the OS home directory.
func DefaultUsersPath() (string, error) {
	if home := os.Getenv("AGENTS_HOME"); home != "" {
		return filepath.Join(home, "review", "users.yaml"), nil
	}
	home, err := userHomeDir()
	if err != nil {
		return "", fmt.Errorf("auth: resolve home dir: %w", err)
	}
	return filepath.Join(home, ".agents", "review", "users.yaml"), nil
}

// LoadUsersFile reads and parses a users file. A missing file is not an error:
// it returns an empty, current-schema UsersFile so first-run callers can add
// the first user without a separate init step.
func LoadUsersFile(path string) (*UsersFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &UsersFile{SchemaVersion: usersSchemaVersion}, nil
		}
		return nil, fmt.Errorf("auth: read users file: %w", err)
	}
	var uf UsersFile
	if err := yaml.Unmarshal(data, &uf); err != nil {
		return nil, fmt.Errorf("auth: parse users file %s: %w", path, err)
	}
	if uf.SchemaVersion == 0 {
		uf.SchemaVersion = usersSchemaVersion
	}
	if err := uf.validate(); err != nil {
		return nil, err
	}
	return &uf, nil
}

// validate checks structural invariants on a parsed users file.
func (uf *UsersFile) validate() error {
	seen := make(map[string]struct{}, len(uf.Users))
	for i, u := range uf.Users {
		if u.Email == "" {
			return fmt.Errorf("auth: users[%d]: empty email", i)
		}
		key := normalizeEmail(u.Email)
		if _, dup := seen[key]; dup {
			return fmt.Errorf("auth: duplicate user %q", u.Email)
		}
		seen[key] = struct{}{}
		if !u.Role.Valid() {
			return fmt.Errorf("auth: users[%d] (%s): invalid role %q", i, u.Email, u.Role)
		}
		if u.TokenHash == "" {
			return fmt.Errorf("auth: users[%d] (%s): empty token_hash", i, u.Email)
		}
	}
	return nil
}

// Save persists the users file atomically (temp file + rename) so concurrent
// readers never observe a partially-written file. Parent directories are
// created as needed, and the file is written 0600 because it holds secrets.
func (uf *UsersFile) Save(path string) error {
	if uf.SchemaVersion == 0 {
		uf.SchemaVersion = usersSchemaVersion
	}
	if err := uf.validate(); err != nil {
		return err
	}
	data, err := yamlMarshal(uf)
	if err != nil {
		return fmt.Errorf("auth: marshal users file: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("auth: create users dir: %w", err)
	}
	tmp, err := createTemp(dir, ".users-*.yaml.tmp")
	if err != nil {
		return fmt.Errorf("auth: temp users file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("auth: chmod temp users file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("auth: write temp users file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("auth: close temp users file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return fmt.Errorf("auth: rename users file: %w", err)
	}
	return nil
}

// Find returns the user with the given email (case-insensitive) and true, or
// the zero value and false.
func (uf *UsersFile) Find(email string) (User, bool) {
	key := normalizeEmail(email)
	for _, u := range uf.Users {
		if normalizeEmail(u.Email) == key {
			return u, true
		}
	}
	return User{}, false
}

// AddUser issues a fresh token for a new user, appends the user (storing only
// the token hash), and returns the issued plaintext token. The caller is
// responsible for persisting via Save and for displaying the plaintext exactly
// once. ErrUserExists is returned if the email is already present.
func (uf *UsersFile) AddUser(email string, role Role) (string, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return "", errors.New("auth: empty email")
	}
	if !role.Valid() {
		return "", fmt.Errorf("auth: invalid role %q", role)
	}
	if _, exists := uf.Find(email); exists {
		return "", ErrUserExists
	}
	plaintext, err := GenerateToken()
	if err != nil {
		return "", err
	}
	hash, err := HashToken(plaintext)
	if err != nil {
		return "", err
	}
	uf.Users = append(uf.Users, User{
		Email:     email,
		Role:      role,
		TokenHash: hash,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})
	return plaintext, nil
}

// RemoveUser deletes the user with the given email (case-insensitive). It
// returns ErrUserNotFound if no such user exists.
func (uf *UsersFile) RemoveUser(email string) error {
	key := normalizeEmail(email)
	for i, u := range uf.Users {
		if normalizeEmail(u.Email) == key {
			uf.Users = append(uf.Users[:i], uf.Users[i+1:]...)
			return nil
		}
	}
	return ErrUserNotFound
}

// normalizeEmail lowercases and trims an email for case-insensitive comparison.
func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
