package commands

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// seedAllPlatformInstallSignals sets up HOME and PATH so every platform's
// IsInstalled() returns true. The bin directory is automatically cleaned by
// t.TempDir(). Returns the temp HOME path.
//
// Note: claude IsInstalled() resolves the `claude` CLI on PATH (the ~/.claude
// dir is no longer an install signal — it persists after uninstall), so we seed
// a `claude` PATH shim. cursor IsInstalled() checks /Applications/Cursor.app
// first, then exec.LookPath("agent"), then exec.LookPath("cursor"); the `agent`
// shim satisfies the second check on any OS without writing under /Applications.
func seedAllPlatformInstallSignals(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("PATH/shim seeding semantics differ on Windows; skip there")
	}
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	// ~/.claude is no longer the claude install signal (that is the PATH shim
	// seeded below), but several callers of this helper read managed files under
	// it (settings.json, stats-cache.json), so keep the directory present.
	if err := os.MkdirAll(filepath.Join(tmp, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}

	copilotExt := filepath.Join(tmp, ".vscode", "extensions", "github.copilot-1.0.0")
	if err := os.MkdirAll(copilotExt, 0o755); err != nil {
		t.Fatal(err)
	}

	seedCLIShimsOnPath(t, tmp, "claude", "agent", "codex", "opencode")

	return tmp
}

// seedCLIShimsOnPath writes a POSIX shim for each named CLI into a fakebin
// directory under root and prepends it to PATH, so exec.LookPath(name) resolves
// for the duration of the test. Each shim prints "<name> 0.0.0" so a --version
// probe also succeeds. Skips on Windows, where the shim contract differs.
//
// This is the single place that knows how to fabricate a CLI on PATH; both the
// all-platform seeder and the focused single-CLI install-signal tests call it so
// the shim-writing logic is never duplicated.
func seedCLIShimsOnPath(t *testing.T, root string, names ...string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("PATH/shim seeding semantics differ on Windows; skip there")
	}
	binDir := filepath.Join(root, "fakebin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	shim := "#!/bin/sh\necho \"$(basename \"$0\") 0.0.0\"\n"
	for _, name := range names {
		p := filepath.Join(binDir, name)
		if err := os.WriteFile(p, []byte(shim), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// seedClaudeInstalledSignal makes claude.IsInstalled() report true by placing a
// `claude` CLI shim on PATH (the detection seam since ~/.claude stopped being an
// install signal). Tests that previously created ~/.claude purely to be detected
// as installed call this instead.
func seedClaudeInstalledSignal(t *testing.T, root string) {
	t.Helper()
	seedCLIShimsOnPath(t, root, "claude")
}
