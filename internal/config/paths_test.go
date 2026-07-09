package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAgentsHomeRespectsOverride(t *testing.T) {
	t.Setenv("AGENTS_HOME", "/tmp/custom-agents")
	if got := AgentsHome(); got != "/tmp/custom-agents" {
		t.Errorf("AgentsHome with override: got %q", got)
	}
}

func TestAgentsHomeDefault(t *testing.T) {
	t.Setenv("AGENTS_HOME", "")
	home, _ := os.UserHomeDir()
	got := AgentsHome()
	// Uniform ~/.agents on every OS, including Windows (no %APPDATA% split).
	if got != filepath.Join(home, ".agents") {
		t.Errorf("AgentsHome default: got %q, want %q", got, filepath.Join(home, ".agents"))
	}
}

// TestPreflightUserHome_OverrideBypassesHomeResolution covers the top-risk
// #2 remediation: an explicit AGENTS_HOME override short-circuits the
// preflight, so an unresolvable $HOME is irrelevant.
func TestPreflightUserHome_OverrideBypassesHomeResolution(t *testing.T) {
	t.Setenv("AGENTS_HOME", "/tmp/explicit-agents-home")
	t.Setenv("HOME", "")
	if err := PreflightUserHome(); err != nil {
		t.Errorf("expected nil error with AGENTS_HOME override, got: %v", err)
	}
}

// TestPreflightUserHome_ResolvableHomeSucceeds covers the normal case: no
// AGENTS_HOME override, but $HOME resolves fine.
func TestPreflightUserHome_ResolvableHomeSucceeds(t *testing.T) {
	t.Setenv("AGENTS_HOME", "")
	t.Setenv("HOME", t.TempDir())
	if err := PreflightUserHome(); err != nil {
		t.Errorf("expected nil error with a resolvable $HOME, got: %v", err)
	}
}

// TestPreflightUserHome_UnresolvableHomeHardFails covers the actual
// remediation: AGENTS_HOME unset AND $HOME unresolvable must hard-fail with
// an actionable message, never silently succeed (which previously let
// AgentsHome() degrade to a relative "./.agents" path).
func TestPreflightUserHome_UnresolvableHomeHardFails(t *testing.T) {
	t.Setenv("AGENTS_HOME", "")
	t.Setenv("HOME", "")
	err := PreflightUserHome()
	if err == nil {
		t.Fatal("expected a hard error when home is unresolvable and AGENTS_HOME is unset")
	}
	if !strings.Contains(err.Error(), "HOME") || !strings.Contains(err.Error(), "AGENTS_HOME") {
		t.Errorf("expected actionable error mentioning $HOME and $AGENTS_HOME, got: %v", err)
	}
}

func TestUserHome(t *testing.T) {
	got := UserHome()
	want, _ := os.UserHomeDir()
	if got != want {
		t.Errorf("UserHome: got %q, want %q", got, want)
	}
}

func TestAgentsStateDir(t *testing.T) {
	// With XDG_STATE_HOME
	t.Setenv("XDG_STATE_HOME", "/tmp/xdg-state")
	if want := filepath.Join("/tmp/xdg-state", "dot-agents"); AgentsStateDir() != want {
		t.Errorf("AgentsStateDir xdg: got %q, want %q", AgentsStateDir(), want)
	}
	// Without XDG_STATE_HOME (falls back to ~/.local/state)
	t.Setenv("XDG_STATE_HOME", "")
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".local", "state", "dot-agents")
	if got := AgentsStateDir(); got != want {
		t.Errorf("AgentsStateDir default: got %q, want %q", got, want)
	}
}

func TestAgentsContextDir(t *testing.T) {
	t.Setenv("AGENTS_HOME", "/tmp/myhome")
	if want := filepath.Join("/tmp/myhome", "context"); AgentsContextDir() != want {
		t.Errorf("AgentsContextDir: got %q, want %q", AgentsContextDir(), want)
	}
}

func TestProjectContextDir(t *testing.T) {
	t.Setenv("AGENTS_HOME", "/tmp/myhome")
	if want := filepath.Join("/tmp/myhome", "context", "proj"); ProjectContextDir("proj") != want {
		t.Errorf("ProjectContextDir: got %q, want %q", ProjectContextDir("proj"), want)
	}
}

func TestHooksScopeDirIn(t *testing.T) {
	got := HooksScopeDirIn("/some/root", "billing-api")
	if want := filepath.Join("/some/root", "hooks", "billing-api"); got != want {
		t.Errorf("HooksScopeDirIn: got %q, want %q", got, want)
	}
	// Explicit-root form must NOT consult the environment: the scope-tree
	// guard relies on this to honor a test-injected (even relative) root.
	t.Setenv("AGENTS_HOME", "/env/should/be/ignored")
	if got := HooksScopeDirIn("rel-root", "global"); got != filepath.Join("rel-root", "hooks", "global") {
		t.Errorf("HooksScopeDirIn ignored explicit root: got %q", got)
	}
}

func TestHooksScopeDirResolvesAgentsHome(t *testing.T) {
	t.Setenv("AGENTS_HOME", "/tmp/myhome")
	if want := filepath.Join("/tmp/myhome", "hooks", "global"); HooksScopeDir("global") != want {
		t.Errorf("HooksScopeDir: got %q, want %q", HooksScopeDir("global"), want)
	}
	// HooksScopeDir must be the home-resolving wrapper over HooksScopeDirIn.
	if HooksScopeDir("proj") != HooksScopeDirIn(AgentsHome(), "proj") {
		t.Errorf("HooksScopeDir not consistent with HooksScopeDirIn(AgentsHome(), ...)")
	}
}

func TestExpandPath_AbsolutePassThrough(t *testing.T) {
	abs := "/already/abs"
	if runtime.GOOS == "windows" {
		abs = `C:\already\abs`
	}
	if got := ExpandPath(abs); got != filepath.Clean(abs) {
		t.Errorf("absolute path should pass through, got %q want %q", got, filepath.Clean(abs))
	}
}

func TestDisplayPath_NoHomePrefix(t *testing.T) {
	// Path that does not begin with home stays unchanged
	got := DisplayPath("/tmp/somewhere")
	if got != "/tmp/somewhere" {
		t.Errorf("non-home path: got %q", got)
	}
}

func TestUserHomeRoots_NoMirror(t *testing.T) {
	t.Setenv("DOT_AGENTS_WINDOWS_MIRROR", "")
	t.Setenv("DOT_AGENTS_WINDOWS_HOME", "")
	roots := UserHomeRoots()
	if len(roots) != 1 {
		t.Errorf("expected 1 root without mirror, got %v", roots)
	}
}

func TestUserHomeRoots_WithMirror(t *testing.T) {
	home, _ := os.UserHomeDir()
	t.Setenv("DOT_AGENTS_WINDOWS_MIRROR", "true")
	t.Setenv("DOT_AGENTS_WINDOWS_HOME", "/mnt/c/Users/alice")
	roots := UserHomeRoots()
	if len(roots) != 2 {
		t.Fatalf("expected 2 roots, got %v", roots)
	}
	if roots[0] != home {
		t.Errorf("first root should be home, got %q", roots[0])
	}
	if roots[1] != "/mnt/c/Users/alice" {
		t.Errorf("second root should be windows home, got %q", roots[1])
	}
}

func TestUserHomeRoots_MirrorSameAsHome(t *testing.T) {
	home, _ := os.UserHomeDir()
	t.Setenv("DOT_AGENTS_WINDOWS_MIRROR", "true")
	t.Setenv("DOT_AGENTS_WINDOWS_HOME", home)
	roots := UserHomeRoots()
	if len(roots) != 1 {
		t.Errorf("mirror equal to home should not duplicate, got %v", roots)
	}
}

func TestSetWindowsMirrorContext_Match(t *testing.T) {
	t.Setenv("DOT_AGENTS_WINDOWS_MIRROR", "")
	t.Setenv("DOT_AGENTS_WINDOWS_HOME", "")
	SetWindowsMirrorContext("/mnt/c/Users/alice/repo")
	if got := os.Getenv("DOT_AGENTS_WINDOWS_MIRROR"); got != "true" {
		t.Errorf("MIRROR: got %q", got)
	}
	if got := os.Getenv("DOT_AGENTS_WINDOWS_HOME"); got != "/mnt/c/Users/alice" {
		t.Errorf("HOME: got %q", got)
	}
}

func TestSetWindowsMirrorContext_NoMatch(t *testing.T) {
	t.Setenv("DOT_AGENTS_WINDOWS_MIRROR", "leftover")
	t.Setenv("DOT_AGENTS_WINDOWS_HOME", "leftover")
	SetWindowsMirrorContext("/Users/alice/repo")
	if got := os.Getenv("DOT_AGENTS_WINDOWS_MIRROR"); got != "false" {
		t.Errorf("MIRROR: got %q", got)
	}
	if got := os.Getenv("DOT_AGENTS_WINDOWS_HOME"); got != "" {
		t.Errorf("HOME: got %q", got)
	}
}

func TestExpandPath_ErrorPaths(t *testing.T) {
	// Non-absolute path that filepath.Abs handles cleanly.
	got := ExpandPath("./nested/../sibling")
	if !strings.HasSuffix(got, "sibling") {
		t.Errorf("relative expansion: got %q", got)
	}
}
