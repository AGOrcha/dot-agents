package fsops

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func withCreateTemp(t *testing.T, fn func(dir, pattern string) (atomicTempFile, error)) {
	t.Helper()
	orig := createTemp
	createTemp = fn
	t.Cleanup(func() { createTemp = orig })
}

func withRenameFunc(t *testing.T, fn func(oldPath, newPath string) error) {
	t.Helper()
	orig := renameFunc
	renameFunc = fn
	t.Cleanup(func() { renameFunc = orig })
}

func TestWriteFileAtomicHappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	if err := WriteFileAtomic(path, []byte("hello")); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "hello" {
		t.Fatalf("read back: %q err=%v", got, err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("perm = %v, want 0600", info.Mode().Perm())
		}
	}
	// No temp file left behind.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("want exactly the target file, got %d entries", len(entries))
	}
}

func TestWriteFileAtomicOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	if err := WriteFileAtomic(path, []byte("first")); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := WriteFileAtomic(path, []byte("second")); err != nil {
		t.Fatalf("second write: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "second" {
		t.Fatalf("read back: %q err=%v", got, err)
	}
}

func TestWriteFileAtomicTempCreateFailure(t *testing.T) {
	withCreateTemp(t, func(string, string) (atomicTempFile, error) {
		return nil, errors.New("no temp")
	})
	if err := WriteFileAtomic(filepath.Join(t.TempDir(), "x"), []byte("y")); err == nil {
		t.Fatal("want temp-create error")
	}
}

// errWriteTemp is a temp file whose Write always fails.
type errWriteTemp struct{ name string }

func (errWriteTemp) Write([]byte) (int, error) { return 0, errors.New("disk full") }
func (errWriteTemp) Close() error              { return nil }
func (f errWriteTemp) Name() string            { return f.name }

func TestWriteFileAtomicWriteFailure(t *testing.T) {
	dir := t.TempDir()
	withCreateTemp(t, func(d, _ string) (atomicTempFile, error) {
		// Real temp so cleanup's Remove has something to delete.
		real, err := os.CreateTemp(d, ".fsops-*.tmp")
		if err != nil {
			return nil, err
		}
		_ = real.Close()
		return errWriteTemp{name: real.Name()}, nil
	})
	if err := WriteFileAtomic(filepath.Join(dir, "x"), []byte("y")); err == nil {
		t.Fatal("want write-temp error")
	}
}

// errCloseTemp writes successfully but fails on Close.
type errCloseTemp struct{ f *os.File }

func (e errCloseTemp) Write(p []byte) (int, error) { return e.f.Write(p) }
func (e errCloseTemp) Close() error                { _ = e.f.Close(); return errors.New("close failed") }
func (e errCloseTemp) Name() string                { return e.f.Name() }

func TestWriteFileAtomicCloseFailure(t *testing.T) {
	dir := t.TempDir()
	withCreateTemp(t, func(d, pattern string) (atomicTempFile, error) {
		real, err := os.CreateTemp(d, pattern)
		if err != nil {
			return nil, err
		}
		return errCloseTemp{f: real}, nil
	})
	if err := WriteFileAtomic(filepath.Join(dir, "x"), []byte("y")); err == nil {
		t.Fatal("want close-temp error")
	}
}

func TestWriteFileAtomicRenameFailure(t *testing.T) {
	dir := t.TempDir()
	withRenameFunc(t, func(string, string) error { return errors.New("rename boom") })
	if err := WriteFileAtomic(filepath.Join(dir, "x"), []byte("y")); err == nil {
		t.Fatal("want rename error")
	}
	// The temp file must have been cleaned up on the failed rename.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("temp not cleaned up after rename failure: %d entries", len(entries))
	}
}
