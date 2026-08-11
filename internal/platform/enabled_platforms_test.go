package platform

// Coverage for EnabledPlatforms — the config-enabled set with no is-installed
// probe, which is what the managed-.gitignore block is keyed off so the block
// stays identical across machines (config-distribution-model §15 D14).

import (
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/config"
)

// idsOf reduces a platform slice to its IDs for order-sensitive comparison —
// EnabledPlatforms documents that order matches All().
func idsOf(ps []Platform) []string {
	ids := make([]string, 0, len(ps))
	for _, p := range ps {
		ids = append(ids, p.ID())
	}
	return ids
}

func TestEnabledPlatforms(t *testing.T) {
	allIDs := idsOf(All())

	disabledEverything := map[string]config.Agent{}
	for _, id := range allIDs {
		disabledEverything[id] = config.Agent{Enabled: false}
	}

	subset := map[string]config.Agent{}
	for _, id := range allIDs {
		subset[id] = config.Agent{Enabled: id == "cursor" || id == "claude"}
	}

	tests := []struct {
		name string
		cfg  *config.Config
		want []string
	}{
		{
			name: "all disabled yields none",
			cfg:  &config.Config{Agents: disabledEverything},
			want: nil,
		},
		{
			name: "a subset enabled yields exactly that subset in All() order",
			cfg:  &config.Config{Agents: subset},
			want: []string{"cursor", "claude"},
		},
		{
			// IsPlatformEnabled treats an absent entry as enabled, so an empty
			// config is the "everything projects here" case — which is why the
			// block is stable on a machine that has installed nothing yet.
			name: "empty config enables everything",
			cfg:  &config.Config{},
			want: allIDs,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := idsOf(EnabledPlatforms(tc.cfg))
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// TestEnabledPlatforms_IgnoresInstallState is the distinction from
// InstalledEnabledPlatforms and the whole reason the function exists: the
// committed .gitignore block must not vary with what happens to be installed
// on the machine running the command.
func TestEnabledPlatforms_IgnoresInstallState(t *testing.T) {
	cfg := &config.Config{}
	enabled := EnabledPlatforms(cfg)
	installed := InstalledEnabledPlatforms(cfg)

	if len(enabled) != len(All()) {
		t.Fatalf("EnabledPlatforms should return every platform for an empty config, got %d of %d", len(enabled), len(All()))
	}
	if len(installed) > len(enabled) {
		t.Errorf("installed set (%d) cannot exceed the enabled set (%d)", len(installed), len(enabled))
	}
	// Every installed-enabled platform must also appear in the enabled set.
	enabledIDs := map[string]bool{}
	for _, id := range idsOf(enabled) {
		enabledIDs[id] = true
	}
	for _, id := range idsOf(installed) {
		if !enabledIDs[id] {
			t.Errorf("installed platform %q missing from the enabled set", id)
		}
	}
}
