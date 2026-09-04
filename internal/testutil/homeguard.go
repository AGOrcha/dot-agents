package testutil

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// homeGuardBuckets lists the managed resource buckets that dot-agents mirrors
// into a developer's real home directory under BOTH `~/.agents/<bucket>` and
// `~/.claude/<bucket>` (the two roots the skill/agent "global scope" and
// "promote" flows write to via config.UserHomeDir() — see
// commands/skills/promote.go:refreshSkillMirror and
// commands/skills/new.go:EnsureUserSkillLinks). skills is the bucket with the
// proven historical leak (see
// .agents/lessons/hermetic-home-for-state-resolving-tests/LESSON.md);
// agents/hooks/plugins share the identical mirror mechanism and code shape,
// so a future test that promotes/creates one of those globally is covered by
// the same guard without needing a bespoke snapshot.
var homeGuardBuckets = []string{"skills", "agents", "hooks", "plugins"}

// homeGuardRoots lists the real-home platform roots under which the managed
// buckets above can be mirrored.
var homeGuardRoots = []string{".agents", ".claude"}

// HomeGuardSnapshot is a point-in-time set of managed entries under the
// developer's real home directory, taken via HomeGuardBefore.
type HomeGuardSnapshot struct {
	homeDir string
	entries map[string]bool
	// homeErr is set when os.UserHomeDir() could not resolve at snapshot
	// time; CheckAndReport is then a no-op (nothing to compare against)
	// rather than a false-positive/false-negative source.
	homeErr error
}

// HomeGuardBefore snapshots the developer's REAL home directory (resolved via
// os.UserHomeDir(), never a test's sandboxed $HOME) before any test in the
// package runs. Call this at the very top of a package's TestMain, before
// m.Run(), then call CheckAndReport() on the result after m.Run() completes:
//
//	func TestMain(m *testing.M) {
//	    snap := testutil.HomeGuardBefore()
//	    code := m.Run()
//	    if n := snap.CheckAndReport(); n > 0 && code == 0 {
//	        code = 1
//	    }
//	    os.Exit(code)
//	}
//
// Packages with no other TestMain setup/teardown needs can skip this and use
// the RunPackageTestsWithHomeGuard(m) one-liner instead.
//
// Deliberately independent of any test's t.Setenv("HOME", ...): TestMain runs
// once, outside any individual test's env-var scope, so os.UserHomeDir() here
// always resolves the actual developer machine home — exactly the directory a
// test must never write into.
func HomeGuardBefore() HomeGuardSnapshot {
	realHome, err := os.UserHomeDir()
	if err != nil {
		return HomeGuardSnapshot{homeErr: err}
	}
	return HomeGuardSnapshot{homeDir: realHome, entries: snapshotHomeManagedEntries(realHome)}
}

// CheckAndReport re-snapshots the real home directory, compares it against
// the snapshot taken by HomeGuardBefore, and — if any managed entry appeared
// in the interim — prints a diagnostic to stderr naming the leaked paths and
// returns the leak count (0 means hermetic). Callers fail their TestMain's
// exit code on a non-zero return.
func (s HomeGuardSnapshot) CheckAndReport() int {
	if s.homeErr != nil {
		return 0
	}
	after := snapshotHomeManagedEntries(s.homeDir)
	var leaked []string
	for entry := range after {
		if !s.entries[entry] {
			leaked = append(leaked, entry)
		}
	}
	if len(leaked) == 0 {
		return 0
	}
	fmt.Fprintf(os.Stderr,
		"HERMETIC HOME GUARD: tests wrote into the real home directory %q: %v\n"+
			"A test resolved os.UserHomeDir()/$HOME without isolating it first via\n"+
			"t.Setenv(\"HOME\", t.TempDir()) (see NewTempProject / WritePreservationManifest\n"+
			"and .agents/lessons/hermetic-home-for-state-resolving-tests/LESSON.md).\n",
		s.homeDir, leaked)
	return len(leaked)
}

// RunPackageTestsWithHomeGuard wraps m.Run() with a package-wide hermeticity
// check: it snapshots the developer's real ~/.agents/{skills,agents,hooks,plugins}
// and ~/.claude/{skills,agents,hooks,plugins} directories before any test
// runs, then re-snapshots after the whole suite completes and fails the run
// if new entries appeared there.
//
// This exists because os.UserHomeDir()/$HOME resolution (e.g. the skill
// platform mirror in commands/skills/promote.go) writes real files/symlinks
// into the caller's actual home directory unless a test isolates HOME first
// (t.Setenv("HOME", t.TempDir())) — see NewTempProject and
// WritePreservationManifest in this package, and
// .agents/lessons/hermetic-home-for-state-resolving-tests/LESSON.md. A single
// test that forgets to isolate HOME leaves dangling symlinks on the
// developer's machine that `da doctor` later flags as broken links.
//
// The check is filesystem-observation based rather than per-test env
// inspection, so it produces no false positives for tests that never touch
// ~/.claude or ~/.agents at all, and it covers every test in the package
// (not just ones that opt in) since it wraps the whole run via TestMain:
//
//	func TestMain(m *testing.M) { os.Exit(testutil.RunPackageTestsWithHomeGuard(m)) }
//
// Packages with a pre-existing TestMain that needs to do its own setup/
// teardown around m.Run() should call HomeGuardBefore()/CheckAndReport()
// directly instead of this wrapper (see the doc comment on HomeGuardBefore).
func RunPackageTestsWithHomeGuard(m *testing.M) int {
	snap := HomeGuardBefore()
	code := m.Run()
	if n := snap.CheckAndReport(); n > 0 && code == 0 {
		code = 1
	}
	return code
}

// snapshotHomeManagedEntries returns the set of top-level paths under
// homeDir/<root>/<bucket> for every root in homeGuardRoots and bucket in
// homeGuardBuckets — the real-home mirror targets the skill/agent/hook/plugin
// "global scope" and "promote" flows write to via os.UserHomeDir(). A missing
// directory is simply absent from the set, not an error: most machines won't
// have every bucket populated under every root.
func snapshotHomeManagedEntries(homeDir string) map[string]bool {
	entries := make(map[string]bool)
	for _, bucket := range homeGuardBuckets {
		for _, root := range homeGuardRoots {
			dir := filepath.Join(homeDir, root, bucket)
			dirEntries, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			for _, e := range dirEntries {
				entries[filepath.Join(dir, e.Name())] = true
			}
		}
	}
	return entries
}
