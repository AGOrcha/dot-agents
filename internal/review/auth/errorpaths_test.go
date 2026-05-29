package auth

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateTokenRandError(t *testing.T) {
	orig := randRead
	randRead = func([]byte) (int, error) { return 0, errors.New("boom") }
	defer func() { randRead = orig }()
	if _, err := GenerateToken(); err == nil {
		t.Fatal("expected error when rand fails")
	}
}

func TestHashTokenSaltError(t *testing.T) {
	orig := randRead
	randRead = func([]byte) (int, error) { return 0, errors.New("boom") }
	defer func() { randRead = orig }()
	if _, err := HashToken(tokenPrefix + "abc"); err == nil {
		t.Fatal("expected error when salt generation fails")
	}
}

func TestAddUserTokenGenError(t *testing.T) {
	orig := randRead
	randRead = func([]byte) (int, error) { return 0, errors.New("boom") }
	defer func() { randRead = orig }()
	uf := &UsersFile{}
	if _, err := uf.AddUser("a@b.com", RoleAdmin); err == nil {
		t.Fatal("AddUser should propagate token-generation failure")
	}
}

func TestAddUserHashError(t *testing.T) {
	// AddUser calls GenerateToken (first randRead) then HashToken (second
	// randRead for the salt). Fail only the salt draw so the hash step is the
	// one that errors.
	orig := randRead
	calls := 0
	randRead = func(b []byte) (int, error) {
		calls++
		if calls == 1 {
			return orig(b)
		}
		return 0, errors.New("boom")
	}
	defer func() { randRead = orig }()
	uf := &UsersFile{}
	if _, err := uf.AddUser("a@b.com", RoleAdmin); err == nil {
		t.Fatal("AddUser should propagate hash failure")
	}
}

func TestDefaultUsersPathHomeError(t *testing.T) {
	t.Setenv("AGENTS_HOME", "")
	orig := userHomeDir
	userHomeDir = func() (string, error) { return "", errors.New("no home") }
	defer func() { userHomeDir = orig }()
	if _, err := DefaultUsersPath(); err == nil {
		t.Fatal("expected error when home dir cannot be resolved")
	}
}

func TestSaveMkdirError(t *testing.T) {
	dir := t.TempDir()
	// Make a regular file where Save wants to create a directory tree.
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	uf := &UsersFile{}
	if _, err := uf.AddUser("a@b.com", RoleAdmin); err != nil {
		t.Fatal(err)
	}
	// blocker/sub/users.yaml -> MkdirAll fails because blocker is a file.
	target := filepath.Join(blocker, "sub", "users.yaml")
	if err := uf.Save(target); err == nil {
		t.Fatal("Save should fail when parent path is a file")
	}
}

func TestSaveMarshalError(t *testing.T) {
	orig := yamlMarshal
	yamlMarshal = func(any) ([]byte, error) { return nil, errors.New("boom") }
	defer func() { yamlMarshal = orig }()
	uf := &UsersFile{}
	if _, err := uf.AddUser("a@b.com", RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if err := uf.Save(filepath.Join(t.TempDir(), "users.yaml")); err == nil {
		t.Fatal("Save should propagate marshal failure")
	}
}

func TestSaveCreateTempError(t *testing.T) {
	orig := createTemp
	createTemp = func(string, string) (tempFile, error) { return nil, errors.New("boom") }
	defer func() { createTemp = orig }()
	uf := &UsersFile{}
	if _, err := uf.AddUser("a@b.com", RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if err := uf.Save(filepath.Join(t.TempDir(), "users.yaml")); err == nil {
		t.Fatal("Save should propagate temp-file creation failure")
	}
}

// fakeTempFile lets Save's chmod/write/close branches be exercised. The real
// temp file is created so Name() points at a removable path (cleanup runs).
type fakeTempFile struct {
	name     string
	failOn   string // "chmod" | "write" | "close"
	closeErr error
}

func (f *fakeTempFile) Name() string { return f.name }
func (f *fakeTempFile) Chmod(os.FileMode) error {
	if f.failOn == "chmod" {
		return errors.New("chmod boom")
	}
	return nil
}
func (f *fakeTempFile) Write(p []byte) (int, error) {
	if f.failOn == "write" {
		return 0, errors.New("write boom")
	}
	return len(p), nil
}
func (f *fakeTempFile) Close() error {
	if f.failOn == "close" {
		return errors.New("close boom")
	}
	return f.closeErr
}

func saveWithFailingTemp(t *testing.T, failOn string) error {
	t.Helper()
	dir := t.TempDir()
	orig := createTemp
	createTemp = func(d, _ string) (tempFile, error) {
		real, err := os.CreateTemp(d, ".users-*.tmp")
		if err != nil {
			return nil, err
		}
		name := real.Name()
		_ = real.Close()
		return &fakeTempFile{name: name, failOn: failOn}, nil
	}
	defer func() { createTemp = orig }()
	uf := &UsersFile{}
	if _, err := uf.AddUser("a@b.com", RoleAdmin); err != nil {
		t.Fatal(err)
	}
	return uf.Save(filepath.Join(dir, "users.yaml"))
}

func TestSaveTempFileErrorBranches(t *testing.T) {
	for _, failOn := range []string{"chmod", "write", "close"} {
		t.Run(failOn, func(t *testing.T) {
			if err := saveWithFailingTemp(t, failOn); err == nil {
				t.Fatalf("Save should fail when temp %s fails", failOn)
			}
		})
	}
}

func TestSaveRenameError(t *testing.T) {
	dir := t.TempDir()
	uf := &UsersFile{}
	if _, err := uf.AddUser("a@b.com", RoleAdmin); err != nil {
		t.Fatal(err)
	}
	// Target path is itself a directory; rename(tmpfile, dir) fails.
	target := filepath.Join(dir, "users.yaml")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	if err := uf.Save(target); err == nil {
		t.Fatal("Save should fail when target is an existing directory")
	}
}
