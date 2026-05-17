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
// Note: cursor IsInstalled() checks /Applications/Cursor.app first, then
// exec.LookPath("agent"), then exec.LookPath("cursor"). We seed an `agent`
// shim, which satisfies the second check on any OS without writing under
// /Applications.
func seedAllPlatformInstallSignals(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("PATH/shim seeding semantics differ on Windows; skip there")
	}
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	if err := os.MkdirAll(filepath.Join(tmp, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}

	copilotExt := filepath.Join(tmp, ".vscode", "extensions", "github.copilot-1.0.0")
	if err := os.MkdirAll(copilotExt, 0o755); err != nil {
		t.Fatal(err)
	}

	binDir := filepath.Join(tmp, "fakebin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	shim := "#!/bin/sh\necho \"$(basename \"$0\") 0.0.0\"\n"
	for _, name := range []string{"agent", "codex", "opencode"} {
		p := filepath.Join(binDir, name)
		if err := os.WriteFile(p, []byte(shim), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+oldPath)

	return tmp
}
