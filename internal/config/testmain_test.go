package config_test

import (
	"os"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/testutil"
)

// TestMain installs the package-wide hermeticity guard. This package owns
// AgentsHome()/UserHomeDir() themselves — the single resolution point every
// other package's home-touching code depends on — so a test here that
// resolves the real home without sandboxing HOME is the highest-leverage
// place for this class of bug to hide. See internal/testutil/homeguard.go
// and .agents/lessons/hermetic-home-for-state-resolving-tests/LESSON.md.
//
// Lives in the config_test (external) package rather than config: the
// internal config test files are compiled as part of package config itself,
// and testutil imports config directly, so importing testutil from an
// internal test file would create an import cycle (config -> testutil ->
// config). The external package has no such constraint.
func TestMain(m *testing.M) {
	homeGuard := testutil.HomeGuardBefore()
	code := m.Run()
	if n := homeGuard.CheckAndReport(); n > 0 && code == 0 {
		code = 1
	}
	os.Exit(code)
}
