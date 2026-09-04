package projectsync

import (
	"os"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/testutil"
)

// TestMain installs the package-wide hermeticity guard. This package owns
// PromoteResource — the shared skill/agent promote mechanism whose mirror
// step is the proven historical leak vector (see
// .agents/lessons/hermetic-home-for-state-resolving-tests/LESSON.md) — so a
// test here that forgets to sandbox HOME alongside AGENTS_HOME is exactly
// the class of bug this guard exists to catch. See
// internal/testutil/homeguard.go.
func TestMain(m *testing.M) {
	homeGuard := testutil.HomeGuardBefore()
	code := m.Run()
	if n := homeGuard.CheckAndReport(); n > 0 && code == 0 {
		code = 1
	}
	os.Exit(code)
}
