package mcp

import (
	"os"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/testutil"
)

// TestMain installs the package-wide hermeticity guard. This package resolves
// AGENTS_HOME/HOME for MCP config scope trees; a test that forgets to
// sandbox HOME alongside AGENTS_HOME leaks real files into the developer's
// machine. See internal/testutil/homeguard.go and
// .agents/lessons/hermetic-home-for-state-resolving-tests/LESSON.md.
func TestMain(m *testing.M) {
	homeGuard := testutil.HomeGuardBefore()
	code := m.Run()
	if n := homeGuard.CheckAndReport(); n > 0 && code == 0 {
		code = 1
	}
	os.Exit(code)
}
