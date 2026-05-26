package platform

// CLI version-probe + IsInstalled tests for each platform, exercising the
// fake-PATH binary approach plus extension-dir / app-bundle fallbacks.
// Relocated from internal/platform/coverage_gap_test.go.

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// installFakeCLI writes an executable shell script that prints `out` and exits
// 0; returns the directory containing the script for use in PATH. Skips on
// non-unix because we rely on `#!/bin/sh`.
func installFakeCLI(t *testing.T, name, out string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI shim relies on POSIX shell semantics")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, name)
	body := "#!/bin/sh\nprintf '%s\\n' " + shellQuote(out) + "\n"
	if err := os.WriteFile(script, []byte(body), 0755); err != nil {
		t.Fatalf("write fake CLI: %v", err)
	}
	return dir
}

// shellQuote is a minimal POSIX-shell single-quote escape.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func TestPeekCLIVersionLine_TrimsAndReturnsFirstLine(t *testing.T) {
	dir := installFakeCLI(t, "fake-cli", "  v1.2.3 (build abc)\nextra-line\n")
	got, err := peekCLIVersionLine(filepath.Join(dir, "fake-cli"))
	if err != nil {
		t.Fatalf("peekCLIVersionLine: %v", err)
	}
	if got != "v1.2.3 (build abc)" {
		t.Errorf("peekCLIVersionLine = %q, want %q", got, "v1.2.3 (build abc)")
	}
}

func TestPeekCLIVersionLine_NonexistentBinaryErrors(t *testing.T) {
	if _, err := peekCLIVersionLine("/no/such/binary/probe-xyz"); err == nil {
		t.Error("expected error for missing binary")
	}
}

func TestFirstCLIPeekVersion_PicksFirstAvailable(t *testing.T) {
	dir := installFakeCLI(t, "agent", "Cursor Agent 2.0.0")
	// Strip PATH so only our fake `agent` binary is resolvable.
	t.Setenv("PATH", dir)
	got := firstCLIPeekVersion("agent", "cursor")
	if got != "Cursor Agent 2.0.0" {
		t.Errorf("firstCLIPeekVersion = %q, want %q", got, "Cursor Agent 2.0.0")
	}
}

func TestFirstCLIPeekVersion_AllMissingReturnsEmpty(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if got := firstCLIPeekVersion("nope-a", "nope-b"); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

// TestClaudeVersion_WithFakeBinary exercises the happy path of
// (*claude).Version through a fake `claude` binary on PATH.
func TestClaudeVersion_WithFakeBinary(t *testing.T) {
	dir := installFakeCLI(t, "claude", "Claude Code 2.5.1\nfoo")
	t.Setenv("PATH", dir)
	got := NewClaude().Version()
	if got != "Claude Code 2.5.1" {
		t.Errorf("claude.Version() = %q, want %q", got, "Claude Code 2.5.1")
	}
}

func TestClaudeVersion_NoBinaryReturnsEmpty(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if got := NewClaude().Version(); got != "" {
		t.Errorf("expected empty version, got %q", got)
	}
}

func TestCodexVersion_WithFakeBinary(t *testing.T) {
	dir := installFakeCLI(t, "codex", "codex-cli 0.4.2")
	t.Setenv("PATH", dir)
	got := NewCodex().Version()
	if got != "codex-cli 0.4.2" {
		t.Errorf("codex.Version() = %q", got)
	}
}

func TestCodexVersion_NoBinaryReturnsEmpty(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if got := NewCodex().Version(); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestOpenCodeVersion_WithFakeBinary(t *testing.T) {
	dir := installFakeCLI(t, "opencode", "opencode v0.9.0\nbuild=xyz")
	t.Setenv("PATH", dir)
	got := NewOpenCode().Version()
	if got != "opencode v0.9.0" {
		t.Errorf("opencode.Version() = %q", got)
	}
}

func TestOpenCodeVersion_NoBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if got := NewOpenCode().Version(); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

// TestCopilotVersion_ExtensionDir exercises the VSCode-extension discovery
// branch of (*copilot).Version by seeding a fake `~/.vscode/extensions/github.copilot-1.2.3/`.
func TestCopilotVersion_ExtensionDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir()) // no `copilot` binary; force extension branch
	extDir := filepath.Join(home, ".vscode", "extensions", "github.copilot-1.234.5")
	if err := os.MkdirAll(extDir, 0755); err != nil {
		t.Fatal(err)
	}
	got := NewCopilot().Version()
	if !strings.HasSuffix(got, "(Extension)") {
		t.Errorf("expected extension suffix, got %q", got)
	}
	if !strings.Contains(got, "1.234.5") {
		t.Errorf("expected version segment, got %q", got)
	}
}

func TestCopilotVersion_CLIFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := installFakeCLI(t, "copilot", "copilot 0.0.99")
	t.Setenv("PATH", dir)
	got := NewCopilot().Version()
	if got != "copilot 0.0.99" {
		t.Errorf("copilot.Version() = %q", got)
	}
}

func TestCopilotIsInstalled_ViaExtensionDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())
	ext := filepath.Join(home, ".vscode", "extensions", "github.copilot-1.0.0")
	if err := os.MkdirAll(ext, 0755); err != nil {
		t.Fatal(err)
	}
	if !NewCopilot().IsInstalled() {
		t.Error("expected IsInstalled to return true via extension dir")
	}
}

// TestClaudeIsInstalled_ViaClaudeDir covers the home-dir fallback (no `claude`
// binary, but ~/.claude exists).
func TestClaudeIsInstalled_ViaClaudeDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0755); err != nil {
		t.Fatal(err)
	}
	if !NewClaude().IsInstalled() {
		t.Error("expected IsInstalled true via ~/.claude")
	}
}

func TestCursorIsInstalled_AgentBinary(t *testing.T) {
	dir := installFakeCLI(t, "agent", "v1")
	t.Setenv("PATH", dir)
	// Cursor.app may or may not exist on the runner; behaviour:
	// if Cursor.app exists -> true; else if agent on PATH -> true.
	// Either way the function returns true here.
	if !NewCursor().IsInstalled() {
		t.Error("expected IsInstalled true with agent on PATH")
	}
}

// TestCursorVersion_NoAppFallsBackToCLI: on non-Darwin (or if Cursor.app is
// absent) the path is the `firstCLIPeekVersion("agent", "cursor")` fallback.
func TestCursorVersion_NoAppFallsBackToCLI(t *testing.T) {
	if _, err := os.Stat("/Applications/Cursor.app"); err == nil {
		t.Skip("Cursor.app exists on this host — fallback branch not reachable")
	}
	dir := installFakeCLI(t, "agent", "Cursor Agent 9.9.9")
	t.Setenv("PATH", dir)
	got := NewCursor().Version()
	if got != "Cursor Agent 9.9.9" {
		t.Errorf("cursor.Version() = %q", got)
	}
}

// TestMacOSCursorAppShortVersion_MissingPlist exercises the error branch.
// We can't easily construct a fake plist at /Applications, so we accept that
// when the file is absent the call returns an error.
func TestMacOSCursorAppShortVersion_MissingPlist(t *testing.T) {
	if _, err := os.Stat("/Applications/Cursor.app/Contents/Info.plist"); err == nil {
		t.Skip("plist exists; error branch not reachable")
	}
	if _, err := macOSCursorAppShortVersion(); err == nil {
		t.Error("expected error when plist is missing")
	}
}
