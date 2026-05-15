package hooks

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyMissingGlobalBundlesCopiesGraphHooks(t *testing.T) {
	tmp := t.TempDir()
	if err := CopyMissingGlobalBundles(tmp); err != nil {
		t.Fatalf("CopyMissingGlobalBundles: %v", err)
	}
	for _, name := range []string{"graph-update", "graph-orient", "graph-precommit"} {
		p := filepath.Join(tmp, name, "HOOK.yaml")
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected %s: %v", p, err)
		}
	}
	sh := filepath.Join(tmp, "graph-precommit", "graph-precommit.sh")
	if fi, err := os.Stat(sh); err != nil {
		t.Fatalf("graph-precommit.sh: %v", err)
	} else if fi.Mode()&0111 == 0 {
		t.Fatalf("graph-precommit.sh should be executable, got %v", fi.Mode())
	}
}

func TestCopyMissingGlobalBundlesCopiesAutoFormatBundle(t *testing.T) {
	tmp := t.TempDir()
	if err := CopyMissingGlobalBundles(tmp); err != nil {
		t.Fatalf("CopyMissingGlobalBundles: %v", err)
	}
	for _, rel := range []string{
		filepath.Join("auto-format", "HOOK.yaml"),
		filepath.Join("auto-format", "auto-format.sh"),
	} {
		if _, err := os.Stat(filepath.Join(tmp, rel)); err != nil {
			t.Fatalf("expected %s: %v", filepath.Join(tmp, rel), err)
		}
	}
}
