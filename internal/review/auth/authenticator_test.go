package auth

import (
	"errors"
	"path/filepath"
	"testing"
)

// seedUsersFile creates a users file on disk with two users and returns the
// path plus the issued plaintext tokens keyed by email.
func seedUsersFile(t *testing.T) (string, map[string]string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "review", "users.yaml")
	uf := &UsersFile{}
	tokens := make(map[string]string)
	for email, role := range map[string]Role{
		"admin@example.com":    RoleAdmin,
		"reviewer@example.com": RoleReviewer,
	} {
		tok, err := uf.AddUser(email, role)
		if err != nil {
			t.Fatalf("seed AddUser(%s): %v", email, err)
		}
		tokens[email] = tok
	}
	if err := uf.Save(path); err != nil {
		t.Fatalf("seed Save: %v", err)
	}
	return path, tokens
}

func TestIdentityCan(t *testing.T) {
	id := Identity{Email: "a@b.com", Role: RoleReadonly}
	if !id.Can(PermReadLabels) {
		t.Error("readonly identity should read labels")
	}
	if id.Can(PermWriteLabels) {
		t.Error("readonly identity should not write labels")
	}
}

func TestLocalUsersAuthenticatorSuccess(t *testing.T) {
	path, tokens := seedUsersFile(t)
	auth := NewLocalUsersAuthenticator(path)

	tests := []struct {
		email    string
		wantRole Role
	}{
		{"admin@example.com", RoleAdmin},
		{"reviewer@example.com", RoleReviewer},
	}
	for _, tt := range tests {
		t.Run(tt.email, func(t *testing.T) {
			id, err := auth.Authenticate(tokens[tt.email])
			if err != nil {
				t.Fatalf("Authenticate: %v", err)
			}
			if id.Email != tt.email || id.Role != tt.wantRole {
				t.Fatalf("identity=%+v want %s/%s", id, tt.email, tt.wantRole)
			}
		})
	}
}

func TestLocalUsersAuthenticatorRejections(t *testing.T) {
	path, _ := seedUsersFile(t)
	auth := NewLocalUsersAuthenticator(path)

	tests := []struct {
		name  string
		token string
	}{
		{"malformed", "garbage"},
		{"empty", ""},
		{"valid shape but unknown", func() string { tok, _ := GenerateToken(); return tok }()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := auth.Authenticate(tt.token)
			if !errors.Is(err, ErrUnauthenticated) {
				t.Fatalf("Authenticate(%q) err=%v want ErrUnauthenticated", tt.token, err)
			}
		})
	}
}

func TestLocalUsersAuthenticatorLoadError(t *testing.T) {
	// Point at a directory so LoadUsersFile returns an operational error
	// (distinct from ErrUnauthenticated).
	dir := t.TempDir()
	auth := NewLocalUsersAuthenticator(dir) // dir, not a file
	tok, _ := GenerateToken()
	_, err := auth.Authenticate(tok)
	if err == nil {
		t.Fatal("expected operational error")
	}
	if errors.Is(err, ErrUnauthenticated) {
		t.Fatal("load failure must not masquerade as ErrUnauthenticated")
	}
}

// TestLocalUsersAuthenticatorSkipsCorruptRow proves one user row with a
// malformed hash does not lock out a valid user that appears after it.
func TestLocalUsersAuthenticatorSkipsCorruptRow(t *testing.T) {
	tok, _ := GenerateToken()
	hash, _ := HashToken(tok)
	uf := &UsersFile{Users: []User{
		{Email: "corrupt@b.com", Role: RoleAdmin, TokenHash: "not-a-hash"},
		{Email: "good@b.com", Role: RoleReviewer, TokenHash: hash},
	}}
	auth := &LocalUsersAuthenticator{
		path: "irrelevant",
		load: func(string) (*UsersFile, error) { return uf, nil },
	}
	id, err := auth.Authenticate(tok)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if id.Email != "good@b.com" {
		t.Fatalf("identity=%+v want good@b.com", id)
	}
}

// TestLocalUsersAuthenticatorNilLoadFallback exercises the default-loader
// branch when the load hook is nil (e.g. zero-value struct).
func TestLocalUsersAuthenticatorNilLoadFallback(t *testing.T) {
	path, tokens := seedUsersFile(t)
	auth := &LocalUsersAuthenticator{path: path} // load is nil
	id, err := auth.Authenticate(tokens["admin@example.com"])
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if id.Role != RoleAdmin {
		t.Fatalf("role=%q want admin", id.Role)
	}
}
