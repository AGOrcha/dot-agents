package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// optSOB / optBool build the optional-field pointers under test. Named
// distinctly from any other test helper in the package so they never collide.
func optSOB(s StringsOrBool) *StringsOrBool { return &s }
func optBool(b bool) *bool                  { return &b }

// TestAgentsRC_AbsentOptionalKeysSurviveRoundTrip is the direct regression test
// for the manifest-corruption bug: a manifest that omits hooks/mcp/settings must
// still omit them after load→save.
//
// Before the fix these were non-pointer fields with no usable omitempty, so an
// absent key decoded to the Go zero value and was re-serialized as an explicit
// `false`. Because the layer merge is key-presence driven, that injected false
// then beat an org layer's `true` and silently disabled hooks/mcp/settings
// projection. Key ABSENCE — not just the decoded value — is the contract.
func TestAgentsRC_AbsentOptionalKeysSurviveRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		// manifest is the on-disk JSON before the round trip.
		manifest string
		// wantAbsent are keys that must NOT appear in the re-saved file.
		wantAbsent []string
		// wantPresent maps a key that MUST appear to its expected JSON value.
		wantPresent map[string]any
	}{
		{
			name: "all three absent stay absent",
			manifest: `{
  "version": 2,
  "repo_id": "github.com/acme/fixture",
  "sources": [{"id": "acme", "type": "local", "path": "../orglayer"}],
  "extends": ["acme:base.json"]
}`,
			wantAbsent: []string{"hooks", "mcp", "settings"},
		},
		{
			name: "explicit false is a real declaration and is preserved",
			manifest: `{
  "version": 2,
  "hooks": false,
  "mcp": false,
  "settings": false,
  "sources": [{"type": "local"}]
}`,
			wantPresent: map[string]any{"hooks": false, "mcp": false, "settings": false},
		},
		{
			name: "explicit true is preserved",
			manifest: `{
  "version": 2,
  "hooks": true,
  "mcp": true,
  "settings": true,
  "sources": [{"type": "local"}]
}`,
			wantPresent: map[string]any{"hooks": true, "mcp": true, "settings": true},
		},
		{
			name: "named lists are preserved and settings stays absent",
			manifest: `{
  "version": 2,
  "hooks": ["PreToolUse"],
  "sources": [{"type": "local"}]
}`,
			wantAbsent:  []string{"mcp", "settings"},
			wantPresent: map[string]any{"hooks": []any{"PreToolUse"}},
		},
		{
			name: "partial declaration: only mcp declared",
			manifest: `{
  "version": 2,
  "mcp": true,
  "sources": [{"type": "local"}]
}`,
			wantAbsent:  []string{"hooks", "settings"},
			wantPresent: map[string]any{"mcp": true},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			path := filepath.Join(tmp, AgentsRCFile)
			if err := os.WriteFile(path, []byte(tc.manifest), 0644); err != nil {
				t.Fatalf("seeding manifest: %v", err)
			}

			rc, err := LoadAgentsRC(tmp)
			if err != nil {
				t.Fatalf("LoadAgentsRC: %v", err)
			}
			if err := rc.Save(tmp); err != nil {
				t.Fatalf("Save: %v", err)
			}

			saved, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading saved manifest: %v", err)
			}
			var raw map[string]any
			if err := json.Unmarshal(saved, &raw); err != nil {
				t.Fatalf("parsing saved manifest: %v\n%s", err, saved)
			}

			for _, key := range tc.wantAbsent {
				if v, ok := raw[key]; ok {
					t.Errorf("key %q must stay ABSENT after round-trip, got %v\nsaved:\n%s", key, v, saved)
				}
			}
			for key, want := range tc.wantPresent {
				got, ok := raw[key]
				if !ok {
					t.Errorf("key %q must be present after round-trip, but it was dropped\nsaved:\n%s", key, saved)
					continue
				}
				if !jsonEqual(got, want) {
					t.Errorf("key %q: got %v, want %v", key, got, want)
				}
			}
		})
	}
}

// TestAgentsRC_AbsentOptionalKeysDecodeToNil pins the in-memory half of the
// contract: absent must decode to a nil pointer (the "defer to layers" signal),
// while an explicit false must decode to a non-nil pointer to false.
func TestAgentsRC_AbsentOptionalKeysDecodeToNil(t *testing.T) {
	var absent AgentsRC
	if err := json.Unmarshal([]byte(`{"version":2}`), &absent); err != nil {
		t.Fatalf("unmarshal absent: %v", err)
	}
	if absent.Hooks != nil || absent.MCP != nil || absent.Settings != nil {
		t.Errorf("absent keys must decode to nil pointers, got hooks=%v mcp=%v settings=%v",
			absent.Hooks, absent.MCP, absent.Settings)
	}
	// Nil pointers must be safe to interrogate — callers should not need guards.
	if absent.Hooks.IsEnabled() || absent.MCP.IsEnabled() || absent.Hooks.Contains("PreToolUse") {
		t.Error("nil optional fields must report not-enabled / not-contained")
	}

	var explicit AgentsRC
	if err := json.Unmarshal([]byte(`{"version":2,"hooks":false,"mcp":false,"settings":false}`), &explicit); err != nil {
		t.Fatalf("unmarshal explicit: %v", err)
	}
	if explicit.Hooks == nil || explicit.MCP == nil || explicit.Settings == nil {
		t.Fatalf("explicit false must decode to non-nil pointers, got hooks=%v mcp=%v settings=%v",
			explicit.Hooks, explicit.MCP, explicit.Settings)
	}
	if *explicit.Settings {
		t.Error("explicit settings:false must decode to false")
	}
}

// jsonEqual compares two decoded JSON values structurally.
func jsonEqual(a, b any) bool {
	ab, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bb, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return string(ab) == string(bb)
}
