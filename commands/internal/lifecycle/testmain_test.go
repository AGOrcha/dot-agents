package lifecycle

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestMain installs a hermetic base PATH for the whole package test binary.
//
// Most tests here exercise runInit / doctor / status, which probe every
// platform's CLI for a `--version` string (internal/platform/cliprobe.go).
// With the developer's real PATH inherited, every probe for a platform a
// given test did not stub falls through to a REAL agent CLI on disk
// (claude/codex/copilot in /opt/homebrew/bin, cursor in ~/.local/bin). Each
// such probe spawns a real `--version` subprocess (~1s+ apiece, bounded at
// 5s); across the package's ~100 runInit/detect call sites that compounds
// until the suite hugs the pre-push gate's 300s per-package timeout — a
// machine-dependent failure that does not reproduce on CI (no CLIs installed)
// and is pure environment leak, not a real defect.
//
// Pinning the base PATH to the Go toolchain + system tool dirs — and
// deliberately dropping the dirs that hold the agent CLIs (/opt/homebrew/bin,
// ~/.local/bin) — makes every unstubbed probe a fast LookPath miss (correctly
// "not installed" for that test's intent). The Go toolchain dir
// (runtime.GOROOT()/bin) keeps `go`/`gofmt` available — install_test.go builds
// a fake git via `go build` — without re-admitting the agent CLIs that share
// /opt/homebrew/bin; /usr/bin + /bin keep git/defaults/coreutils available.
// Tests that DO want a platform present still prepend their fake
// version-returning shim via seedCLIShimsOnPathLifecycle, so the version-parse
// path stays exercised against a deterministic mock rather than a real binary.
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
