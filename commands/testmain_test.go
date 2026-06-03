package commands

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestMain installs a hermetic base PATH for the whole package test binary.
//
// Many tests here drive install / doctor / status / refresh, which probe every
// platform's CLI for a `--version` string (internal/platform/cliprobe.go). With
// the developer's real PATH inherited, each probe for a platform a given test
// did not stub falls through to a REAL agent CLI on disk (claude/codex/copilot
// in /opt/homebrew/bin, cursor in ~/.local/bin), spawning a real `--version`
// subprocess (~1s+ apiece, bounded at 5s). Across this package's install/status
// tests that compounds until the suite approaches the pre-push gate's 300s
// per-package timeout — a machine-dependent slowdown that does not reproduce on
// CI (no CLIs installed) and is pure environment leak, not a real defect.
//
// Pinning the base PATH to the Go toolchain + system tool dirs — and dropping
// the dirs that hold the agent CLIs — makes every unstubbed probe a fast
// LookPath miss. runtime.GOROOT()/bin keeps `go`/`gofmt` available (some tests
// build a fake binary via `go build`) without re-admitting the agent CLIs that
// share /opt/homebrew/bin; /usr/bin + /bin keep git/defaults/coreutils. Tests
// that DO want a platform present still prepend their fake version-returning
// shim via seedCLIShimsOnPath, so the version-parse path stays exercised
// against a deterministic mock rather than a real binary.
//
// Windows is left untouched: the PATH/shim seeding helpers already t.Skip
// there, and this POSIX base PATH is not meaningful on Windows.
func TestMain(m *testing.M) {
	if runtime.GOOS != "windows" {
		sep := string(os.PathListSeparator)
		os.Setenv("PATH", filepath.Join(runtime.GOROOT(), "bin")+sep+"/usr/bin"+sep+"/bin"+sep+"/usr/sbin"+sep+"/sbin")
	}
	os.Exit(m.Run())
}
