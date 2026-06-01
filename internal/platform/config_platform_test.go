package platform

import (
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/config"
)

func TestInstalledEnabledPlatforms_AllDisabled(t *testing.T) {
	cfg := &config.Config{
		Agents: map[string]config.Agent{
			"cursor":   {Enabled: false},
			"claude":   {Enabled: false},
			"codex":    {Enabled: false},
			"opencode": {Enabled: false},
			"copilot":  {Enabled: false},
		},
	}
	got := InstalledEnabledPlatforms(cfg)
	if len(got) != 0 {
		t.Fatalf("expected empty when all platforms disabled, got %d entries", len(got))
	}
}

func TestInstalledEnabledPlatforms_OnlyInstalled(t *testing.T) {
	cfg := &config.Config{}
	out := InstalledEnabledPlatforms(cfg)
	for _, p := range out {
		if !p.IsInstalled() {
			t.Errorf("included platform %q is not installed", p.ID())
		}
		if !cfg.IsPlatformEnabled(p.ID()) {
			t.Errorf("included platform %q is not enabled in config", p.ID())
		}
	}
}

// claudeOnly enables only the claude platform so the loop deterministically
// checks exactly one IsInstalled(); the others short-circuit on the disabled
// (continue) path.
func claudeOnly() *config.Config {
	return &config.Config{
		Agents: map[string]config.Agent{
			"cursor":   {Enabled: false},
			"claude":   {Enabled: true},
			"codex":    {Enabled: false},
			"opencode": {Enabled: false},
			"copilot":  {Enabled: false},
		},
	}
}

func platformIDs(ps []Platform) []string {
	ids := make([]string, len(ps))
	for i, p := range ps {
		ids[i] = p.ID()
	}
	return ids
}

func containsPlatform(ps []Platform, id string) bool {
	for _, p := range ps {
		if p.ID() == id {
			return true
		}
	}
	return false
}

// TestInstalledEnabledPlatforms_IncludesInstalled covers the enabled+installed
// append branch: claude is made "installed" via a real `claude` binary on PATH —
// the correct signal (probeInstalled/exec.LookPath), not the presence of a config
// dir. installFakeCLI skips on Windows, so the branch is covered in the merged
// multi-OS profile via the POSIX runners.
func TestInstalledEnabledPlatforms_IncludesInstalled(t *testing.T) {
	t.Setenv("PATH", installFakeCLI(t, "claude", "v1"))
	out := InstalledEnabledPlatforms(claudeOnly())
	if !containsPlatform(out, "claude") {
		t.Fatalf("expected claude (binary on PATH) in result, got %v", platformIDs(out))
	}
}

// TestInstalledEnabledPlatforms_ExcludesEnabledNotInstalled covers the
// enabled-but-not-installed path: an empty PATH means claude is enabled yet
// IsInstalled() is false, so it is excluded.
func TestInstalledEnabledPlatforms_ExcludesEnabledNotInstalled(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	out := InstalledEnabledPlatforms(claudeOnly())
	if containsPlatform(out, "claude") {
		t.Fatalf("claude not on PATH; expected exclusion, got %v", platformIDs(out))
	}
}
