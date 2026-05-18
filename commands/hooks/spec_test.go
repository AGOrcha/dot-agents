package hooks

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/platform"
)

func TestHookKindLabel(t *testing.T) {
	cases := map[platform.HookSourceKind]string{
		platform.HookSourceCanonicalBundle: "canonical bundle",
		platform.HookSourceLegacyFile:      "legacy file",
		platform.HookSourceKind("other"):   "other",
	}
	for kind, want := range cases {
		if got := hookKindLabel(kind); got != want {
			t.Errorf("hookKindLabel(%q)=%q want %q", kind, got, want)
		}
	}
}

// TestFindHookSpecEmptyName covers the trimmed-empty-name guard, which
// returns a usage error before touching the filesystem.
func TestFindHookSpecEmptyName(t *testing.T) {
	deps := testDeps()
	_, err := findHookSpec(deps, t.TempDir(), "global", "   ")
	if err == nil {
		t.Fatal("expected usage error for empty hook name")
	}
	if !strings.Contains(err.Error(), "hook name is empty") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestFindHookSpecMissingScopeDir covers the os.IsNotExist branch: a scope
// with no hooks directory yields the "no hooks directory" hinted error.
func TestFindHookSpecMissingScopeDir(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	deps := testDeps()
	_, err := findHookSpec(deps, agentsHome, "ghost", "alpha")
	if err == nil {
		t.Fatal("expected error for missing scope dir")
	}
	if !strings.Contains(err.Error(), "no hooks directory for scope") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestFindHookSpecListError covers the non-IsNotExist error path: the
// scope path exists but is a regular file, so os.ReadDir returns ENOTDIR
// (which is not os.IsNotExist) and findHookSpec must surface it verbatim.
func TestFindHookSpecListError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("ENOTDIR list-error path is POSIX-only: a file-where-dir-expected yields an IsNotExist-equivalent on Windows, so findHookSpec correctly reports the missing-dir branch there (covered cross-platform by TestFindHookSpecMissingScopeDir)")
	}
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	scope := "broken"
	hooksDir := filepath.Join(agentsHome, "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Place a file where the scope directory is expected.
	if err := os.WriteFile(filepath.Join(hooksDir, scope), []byte("not a dir"), 0644); err != nil {
		t.Fatal(err)
	}
	deps := testDeps()
	_, err := findHookSpec(deps, agentsHome, scope, "alpha")
	if err == nil {
		t.Fatal("expected non-IsNotExist error when scope path is a file")
	}
	if os.IsNotExist(err) {
		t.Fatalf("error should not be IsNotExist, got: %v", err)
	}
	if strings.Contains(err.Error(), "no hooks directory for scope") {
		t.Fatalf("ENOTDIR must not be reported as missing dir: %v", err)
	}
}
