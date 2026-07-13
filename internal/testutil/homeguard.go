package testutil

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// RunPackageTestsWithHomeGuard wraps m.Run() with a package-wide hermeticity
// check: it snapshots the developer's real ~/.claude/skills and
// ~/.agents/skills directories before any test runs, then re-snapshots after
// the whole suite completes and fails the run if new entries appeared there.
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
func RunPackageTestsWithHomeGuard(m *testing.M) int {
	realHome, homeErr := os.UserHomeDir()

	var before map[string]bool
	if homeErr == nil {
		before = snapshotHomeSkillEntries(realHome)
	}

	code := m.Run()

	if homeErr == nil {
		after := snapshotHomeSkillEntries(realHome)
		var leaked []string
		for entry := range after {
			if !before[entry] {
				leaked = append(leaked, entry)
			}
		}
		if len(leaked) > 0 {
			fmt.Fprintf(os.Stderr,
				"HERMETIC HOME GUARD: tests wrote into the real home directory %q: %v\n"+
					"A test resolved os.UserHomeDir()/$HOME without isolating it first via\n"+
					"t.Setenv(\"HOME\", t.TempDir()) (see NewTempProject / WritePreservationManifest\n"+
					"and .agents/lessons/hermetic-home-for-state-resolving-tests/LESSON.md).\n",
				realHome, leaked)
			if code == 0 {
				code = 1
			}
		}
	}

	return code
}

// snapshotHomeSkillEntries returns the set of top-level paths under
// homeDir/.claude/skills and homeDir/.agents/skills — the two mirror targets
// commands/skills' promote step writes to via os.UserHomeDir() (see
// refreshSkillMirror in commands/skills/promote.go). A missing directory is
// simply absent from the set, not an error: most machines won't have both
// populated.
func snapshotHomeSkillEntries(homeDir string) map[string]bool {
	entries := make(map[string]bool)
	for _, root := range []string{
		filepath.Join(homeDir, ".claude", "skills"),
		filepath.Join(homeDir, ".agents", "skills"),
	} {
		dirEntries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range dirEntries {
			entries[filepath.Join(root, e.Name())] = true
		}
	}
	return entries
}
