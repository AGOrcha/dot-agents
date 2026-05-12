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
