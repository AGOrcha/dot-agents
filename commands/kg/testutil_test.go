package kg

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// execCommandCombined is a thin wrapper around exec.Command used by the
// runGit helper below.
func execCommandCombined(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

// hasWarningContaining reports whether any warning string contains substr.
func hasWarningContaining(warnings []string, substr string) bool {
	for _, w := range warnings {
		if strings.Contains(w, substr) {
			return true
		}
	}
	return false
}

// fakeCRGEmittingJSON installs a fake binary that prints the given JSON on
// a `python -c` style invocation. The flows / communities commands invoke
// runPyQuery (python3), so we also need a python interpreter — we run python
// via a shell-script wrapper that uses real python3 if present, otherwise we
// skip. Simpler: write a fake python that ignores args and prints the JSON.
func fakeCRGEmittingJSON(t *testing.T, repo, body string) {
	t.Helper()
	crgShellShimSkip(t)
	binDir := filepath.Join(repo, ".venv", "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(binDir, "code-review-graph"), []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}

	script := "#!/bin/sh\ncat <<'__EOF__'\n" + body + "\n__EOF__\n"
	if err := os.WriteFile(filepath.Join(binDir, "python3"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
}

// useBridgeBackend pins one test to the legacy Python CRG subprocess backend.
//
// After the §11 cutover the default backend is the kg-native engine, so the
// tests that assert subprocess routing (fake `code-review-graph` / `python3`
// shims, "no CRG binary" degradation, CRG JSON parsing) must say so explicitly.
// Keeping them pinned rather than deleting them is deliberate: they are the
// regression coverage for the §11.4 rollback path, which stays supported until
// the decommissioning gate passes.
func useBridgeBackend(t *testing.T) {
	t.Helper()
	t.Setenv(graphBackendEnv, "crg-bridge")
}

// runGit is a tiny helper to invoke git in a workdir.
func runGit(t *testing.T, dir string, args ...string) ([]byte, error) {
	t.Helper()
	full := []string{}
	if dir != "" {
		full = append(full, "-C", dir)
	}
	full = append(full, args...)
	return execCommandCombined("git", full...)
}

var (
	_ = io.Discard
	_ = time.Now
	_ = errors.New
	_ = fmt.Sprintf
)
