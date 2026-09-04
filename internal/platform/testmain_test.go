package platform

import (
	"os"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/testutil"
)

// TestMain installs the package-wide hermeticity guard. This package IS the
// mirror mechanism (BuildSharedSkillMirrorIntents, ExecuteSharedSkillMirrorPlan,
// and the per-platform CreateLinks/RemoveLinks implementations) that resolves
// AGENTS_HOME/HOME to write into a user's real ~/.claude, ~/.cursor, ~/.codex,
// etc. A test here that forgets to sandbox HOME alongside AGENTS_HOME is the
// most direct way for this class of bug to leak into the developer's machine.
// See internal/testutil/homeguard.go and
// .agents/lessons/hermetic-home-for-state-resolving-tests/LESSON.md.
func TestMain(m *testing.M) {
	homeGuard := testutil.HomeGuardBefore()
	code := m.Run()
	if n := homeGuard.CheckAndReport(); n > 0 && code == 0 {
		code = 1
	}
	os.Exit(code)
}
