package commands

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/journal"
	"github.com/AGOrcha/dot-agents/internal/testutil"
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
//
// It also isolates the session-handoff journal under a throwaway XDG_STATE_HOME
// for the whole package run: the review approve/reject commands now append typed
// journal events (p3b), and every existing review test that drives those runners
// would otherwise write events.log into the developer's real ~/.local/state.
func TestMain(m *testing.M) {
	// Hermeticity guard: snapshot the developer's real ~/.agents and
	// ~/.claude managed-bucket trees before any test runs. This package
	// drives `da skills`/`da agents` create+promote flows end-to-end
	// (skills_test.go, agentsrc_mutations_test.go, run_test.go), which reach
	// os.UserHomeDir() for the global-scope mirror step — a test that forgets
	// to t.Setenv("HOME", ...) leaks real symlinks into the developer's
	// machine. See internal/testutil/homeguard.go and
	// .agents/lessons/hermetic-home-for-state-resolving-tests/LESSON.md.
	homeGuard := testutil.HomeGuardBefore()

	if runtime.GOOS != "windows" {
		sep := string(os.PathListSeparator)
		os.Setenv("PATH", filepath.Join(runtime.GOROOT(), "bin")+sep+"/usr/bin"+sep+"/bin"+sep+"/usr/sbin"+sep+"/sbin")
	}
	dir, err := os.MkdirTemp("", "commands-journal-state-")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_STATE_HOME", dir)
	// Neuter the real journal append path for the package run: the review runners
	// resolve a constant repoPath (the cwd), so the events.log interprocess lock
	// would serialize across tests. The wiring still executes (build → NewEvent →
	// reviewJournalEmit); only the disk write is a no-op. Capture tests override
	// reviewJournalEmit with their own recorder.
	reviewJournalEmit = func(string, journal.Envelope) error { return nil }
	code := m.Run()
	os.RemoveAll(dir)
	if n := homeGuard.CheckAndReport(); n > 0 && code == 0 {
		code = 1
	}
	os.Exit(code)
}
