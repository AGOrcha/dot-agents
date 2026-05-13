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
	if runtime.GOOS == "windows" {
		if appData := os.Getenv("APPDATA"); appData != "" {
			if got != filepath.Join(appData, ".agents") {
				t.Errorf("AgentsHome windows: got %q", got)
			}
			return
		}
	}
	if got != filepath.Join(home, ".agents") {
		t.Errorf("AgentsHome default: got %q, want %q", got, filepath.Join(home, ".agents"))
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
	if got := AgentsStateDir(); got != "/tmp/xdg-state/dot-agents" {
		t.Errorf("AgentsStateDir xdg: got %q", got)
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
	if got := AgentsContextDir(); got != "/tmp/myhome/context" {
		t.Errorf("AgentsContextDir: got %q", got)
	}
}

func TestProjectContextDir(t *testing.T) {
	t.Setenv("AGENTS_HOME", "/tmp/myhome")
	if got := ProjectContextDir("proj"); got != "/tmp/myhome/context/proj" {
		t.Errorf("ProjectContextDir: got %q", got)
	}
}

func TestExpandPath_AbsolutePassThrough(t *testing.T) {
	if got := ExpandPath("/already/abs"); got != "/already/abs" {
		t.Errorf("absolute path should pass through, got %q", got)
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
