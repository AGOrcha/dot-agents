package auth

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestDefaultUsersPath(t *testing.T) {
	t.Run("AGENTS_HOME override", func(t *testing.T) {
		t.Setenv("AGENTS_HOME", "/tmp/agents-home")
		got, err := DefaultUsersPath()
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join("/tmp/agents-home", "review", "users.yaml")
		if got != want {
			t.Fatalf("got %q want %q", got, want)
		}
	})
	t.Run("home fallback", func(t *testing.T) {
		t.Setenv("AGENTS_HOME", "")
		got, err := DefaultUsersPath()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(got, filepath.Join(".agents", "review", "users.yaml")) {
			t.Fatalf("unexpected path %q", got)
		}
	})
}

func TestLoadUsersFileMissingReturnsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope.yaml")
	uf, err := LoadUsersFile(path)
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if uf.SchemaVersion != usersSchemaVersion {
		t.Fatalf("schema version=%d want %d", uf.SchemaVersion, usersSchemaVersion)
	}
	if len(uf.Users) != 0 {
		t.Fatalf("expected no users, got %d", len(uf.Users))
	}
}

func TestLoadUsersFileMalformedYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(path, []byte("this: : : not yaml\n  - broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadUsersFile(path); err == nil {
		t.Fatal("malformed YAML should error")
	}
}

func TestLoadUsersFileUnreadable(t *testing.T) {
	dir := t.TempDir()
	// A directory at the file path makes ReadFile fail with a non-NotExist error.
	path := filepath.Join(dir, "users.yaml")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadUsersFile(path); err == nil {
		t.Fatal("reading a directory as a file should error")
	}
}

func writeUsersYAML(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadUsersFileValidationErrors(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"empty email", "schema_version: 1\nusers:\n  - email: \"\"\n    role: admin\n    token_hash: x\n"},
		{"invalid role", "schema_version: 1\nusers:\n  - email: a@b.com\n    role: wizard\n    token_hash: x\n"},
		{"empty hash", "schema_version: 1\nusers:\n  - email: a@b.com\n    role: admin\n    token_hash: \"\"\n"},
		{"duplicate email", "schema_version: 1\nusers:\n  - email: a@b.com\n    role: admin\n    token_hash: x\n  - email: A@B.com\n    role: reviewer\n    token_hash: y\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "users.yaml")
			writeUsersYAML(t, path, tt.body)
			if _, err := LoadUsersFile(path); err == nil {
				t.Fatalf("%s should fail validation", tt.name)
			}
		})
	}
}

func TestLoadUsersFileDefaultsSchemaVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.yaml")
	writeUsersYAML(t, path, "users:\n  - email: a@b.com\n    role: admin\n    token_hash: deadbeef\n")
	uf, err := LoadUsersFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if uf.SchemaVersion != usersSchemaVersion {
		t.Fatalf("schema version not defaulted: %d", uf.SchemaVersion)
	}
}

func TestAddFindRemoveRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "review", "users.yaml")
	uf := &UsersFile{}

	tok, err := uf.AddUser("Reviewer@Example.com", RoleReviewer)
	if err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	if !strings.HasPrefix(tok, tokenPrefix) {
		t.Fatalf("issued token has wrong shape: %q", tok)
	}

	if err := uf.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// File must be 0600 because it stores secrets.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Fatalf("users file perm=%o want 0600", perm)
	}

	reloaded, err := LoadUsersFile(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	u, ok := reloaded.Find("reviewer@example.com") // case-insensitive
	if !ok {
		t.Fatal("Find should locate the user case-insensitively")
	}
	if u.Role != RoleReviewer {
		t.Fatalf("role=%q want reviewer", u.Role)
	}
	// The stored hash must verify against the issued plaintext, and never be
	// the plaintext itself.
	if u.TokenHash == tok {
		t.Fatal("token_hash must not be plaintext")
	}
	verified, err := VerifyToken(tok, u.TokenHash)
	if err != nil || !verified {
		t.Fatalf("issued token must verify: ok=%v err=%v", verified, err)
	}

	// Remove and confirm gone.
	if err := reloaded.RemoveUser("REVIEWER@example.com"); err != nil {
		t.Fatalf("RemoveUser: %v", err)
	}
	if _, ok := reloaded.Find("reviewer@example.com"); ok {
		t.Fatal("user should be gone after RemoveUser")
	}
}

func TestAddUserErrors(t *testing.T) {
	uf := &UsersFile{}
	if _, err := uf.AddUser("   ", RoleAdmin); err == nil {
		t.Error("empty email should error")
	}
	if _, err := uf.AddUser("a@b.com", Role("bogus")); err == nil {
		t.Error("invalid role should error")
	}
	if _, err := uf.AddUser("dup@b.com", RoleAdmin); err != nil {
		t.Fatalf("first add: %v", err)
	}
	if _, err := uf.AddUser("DUP@b.com", RoleReviewer); !errors.Is(err, ErrUserExists) {
		t.Errorf("duplicate add err=%v want ErrUserExists", err)
	}
}

func TestRemoveUserNotFound(t *testing.T) {
	uf := &UsersFile{}
	if err := uf.RemoveUser("ghost@b.com"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("err=%v want ErrUserNotFound", err)
	}
}

func TestFindMiss(t *testing.T) {
	uf := &UsersFile{}
	if _, ok := uf.Find("nobody@b.com"); ok {
		t.Error("Find on empty file should miss")
	}
}

func TestSaveRejectsInvalid(t *testing.T) {
	uf := &UsersFile{Users: []User{{Email: "", Role: RoleAdmin, TokenHash: "x"}}}
	if err := uf.Save(filepath.Join(t.TempDir(), "users.yaml")); err == nil {
		t.Fatal("Save should reject an invalid file")
	}
}

func TestSaveMarshalsKnownSchema(t *testing.T) {
	uf := &UsersFile{}
	if _, err := uf.AddUser("a@b.com", RoleAdmin); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "users.yaml")
	if err := uf.Save(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var probe UsersFile
	if err := yaml.Unmarshal(data, &probe); err != nil {
		t.Fatalf("written file must re-parse: %v", err)
	}
	if probe.SchemaVersion != usersSchemaVersion || len(probe.Users) != 1 {
		t.Fatalf("unexpected round-trip: %+v", probe)
	}
	if probe.Users[0].CreatedAt == "" {
		t.Error("CreatedAt should be populated")
	}
}
