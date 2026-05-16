//go:build !windows

package fsops

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFsopsDefault_RoundTrip(t *testing.T) {
	root := t.TempDir()

	nested := filepath.Join(root, "a", "b", "c")
	if err := MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if fi, err := os.Stat(nested); err != nil || !fi.IsDir() {
		t.Fatalf("expected nested dir, stat err=%v", err)
	}

	f := filepath.Join(nested, "f.txt")
	if err := WriteFile(f, []byte("payload"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := os.ReadFile(f)
	if err != nil || string(got) != "payload" {
		t.Fatalf("ReadFile after WriteFile: got=%q err=%v", got, err)
	}

	if err := Remove(f); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(f); !os.IsNotExist(err) {
		t.Errorf("expected file gone after Remove, err=%v", err)
	}

	if err := RemoveAll(filepath.Join(root, "a")); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "a")); !os.IsNotExist(err) {
		t.Errorf("expected tree gone after RemoveAll, err=%v", err)
	}
}
