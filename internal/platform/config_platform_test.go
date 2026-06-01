package platform

import (
	"os"
	"path/filepath"
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
// append branch hermetically: claude is made "installed" via its HOME-relative
// marker (~/.claude) under an isolated HOME, so the branch no longer depends on a
// CLI being on PATH or the developer's real HOME (which previously covered it
// incidentally — and broke once test HOME isolation landed).
func TestInstalledEnabledPlatforms_IncludesInstalled(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	if err := os.MkdirAll(filepath.Join(tmp, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	out := InstalledEnabledPlatforms(claudeOnly())
	if !containsPlatform(out, "claude") {
		t.Fatalf("expected claude (installed via ~/.claude) in result, got %v", platformIDs(out))
	}
}

// TestInstalledEnabledPlatforms_ExcludesEnabledNotInstalled covers the
// enabled-but-not-installed path deterministically: an isolated HOME with no
// markers and an empty PATH means claude is enabled yet IsInstalled() is false,
// so it is excluded.
func TestInstalledEnabledPlatforms_ExcludesEnabledNotInstalled(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	t.Setenv("PATH", tmp) // no platform CLIs reachable via probeInstalled
	out := InstalledEnabledPlatforms(claudeOnly())
	if containsPlatform(out, "claude") {
		t.Fatalf("claude has no install marker; expected exclusion, got %v", platformIDs(out))
	}
}
