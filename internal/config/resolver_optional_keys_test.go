package config

import "testing"

// TestResolveSnapshot_OptionalKeyLayerPrecedence pins the merge semantics that
// the manifest-corruption bug silently broke.
//
// The layer merge is KEY-PRESENCE driven: a layer only competes for a key that
// is physically present in its raw JSON object. So for hooks/mcp/settings:
//
//   - repo-local omits the key      => the org layer's value survives
//   - repo-local sets it explicitly => repo-local wins (that is a real override)
//
// The merge code itself was always correct. The bug was upstream: every manifest
// da re-saved gained an explicit `false`, which turned case 1 into case 2 and
// silently disabled the org layer's projection. These cases lock the intended
// behavior in place so a future re-serialization regression fails loudly here.
func TestResolveSnapshot_OptionalKeyLayerPrecedence(t *testing.T) {
	const orgLayer = "org-layer"

	tests := []struct {
		name string
		// repoRaw is the repo-local layer's raw object (nil entry = key absent).
		repoRaw map[string]any
		// wantValue is the expected effective value for "hooks".
		wantValue any
		// wantActiveLayer is the layer provenance explain should attribute it to.
		wantActiveLayer string
	}{
		{
			name:            "absent repo-local defers to org layer true",
			repoRaw:         map[string]any{"version": float64(2)},
			wantValue:       true,
			wantActiveLayer: orgLayer,
		},
		{
			name:            "explicit repo-local false overrides org layer true",
			repoRaw:         map[string]any{"version": float64(2), "hooks": false},
			wantValue:       false,
			wantActiveLayer: LayerRepoLocal,
		},
		{
			name:            "explicit repo-local true is preserved",
			repoRaw:         map[string]any{"version": float64(2), "hooks": true},
			wantValue:       true,
			wantActiveLayer: LayerRepoLocal,
		},
		{
			name:            "explicit repo-local named list overrides org layer true",
			repoRaw:         map[string]any{"version": float64(2), "hooks": []any{"PreToolUse"}},
			wantValue:       []any{"PreToolUse"},
			wantActiveLayer: LayerRepoLocal,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			layers := []ResolvedLayer{
				{ID: LayerProductDefaults, Present: true, Raw: map[string]any{}},
				// The org layer arrives through `extends` and sets hooks/mcp/settings.
				{ID: orgLayer, Present: true, Raw: map[string]any{
					"hooks": true, "mcp": true, "settings": true,
				}},
				{ID: LayerRepoLocal, Present: true, Raw: tc.repoRaw},
			}

			snap, err := resolveSnapshot(layers)
			if err != nil {
				t.Fatalf("resolveSnapshot: %v", err)
			}

			raw, err := snap.EffectiveRaw()
			if err != nil {
				t.Fatalf("EffectiveRaw: %v", err)
			}
			if got := raw["hooks"]; !jsonEqual(got, tc.wantValue) {
				t.Errorf("effective hooks: got %v, want %v", got, tc.wantValue)
			}

			if got := snap.FieldAt("hooks").ActiveLayer; got != tc.wantActiveLayer {
				t.Errorf("hooks provenance: got active layer %q, want %q", got, tc.wantActiveLayer)
			}
		})
	}
}

// TestResolveSnapshot_AbsentRepoLocalKeepsOrgLayerForAllThree is the broader
// version of the case that actually bit in production: a minimal repo manifest
// that declares none of the three must inherit all three from the org layer.
func TestResolveSnapshot_AbsentRepoLocalKeepsOrgLayerForAllThree(t *testing.T) {
	layers := []ResolvedLayer{
		{ID: LayerProductDefaults, Present: true, Raw: map[string]any{}},
		{ID: "org-layer", Present: true, Raw: map[string]any{
			"hooks": true, "mcp": true, "settings": true,
		}},
		{ID: LayerRepoLocal, Present: true, Raw: map[string]any{
			"version": float64(2),
			"repo_id": "github.com/acme/fixture",
		}},
	}

	snap, err := resolveSnapshot(layers)
	if err != nil {
		t.Fatalf("resolveSnapshot: %v", err)
	}

	if !snap.Effective.Hooks.IsEnabled() {
		t.Error("hooks must resolve to enabled from the org layer")
	}
	if !snap.Effective.MCP.IsEnabled() {
		t.Error("mcp must resolve to enabled from the org layer")
	}
	if snap.Effective.Settings == nil || !*snap.Effective.Settings {
		t.Errorf("settings must resolve to true from the org layer, got %v", snap.Effective.Settings)
	}
}
