package agents

import (
	"os"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/testutil"
)

// TestMain installs the package-wide hermeticity guard: this package drives
// CreateAgent/PromoteAgentIn end-to-end, which resolve AGENTS_HOME/HOME for
// canonical storage and (for skills' sibling flows) user-level mirrors. A
// test that forgets to t.Setenv("HOME", ...) alongside AGENTS_HOME leaks real
// files/symlinks into the developer's machine. See
// internal/testutil/homeguard.go and
// .agents/lessons/hermetic-home-for-state-resolving-tests/LESSON.md.
func TestMain(m *testing.M) {
	homeGuard := testutil.HomeGuardBefore()
	code := m.Run()
	if n := homeGuard.CheckAndReport(); n > 0 && code == 0 {
		code = 1
	}
	os.Exit(code)
}
