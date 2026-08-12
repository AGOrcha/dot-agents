package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestAgentsRC_AbsentSourcesAndVersionSurviveRoundTrip extends the
// manifest-corruption contract to the two remaining fields that could not be
// omitted by encoding/json.
//
// `sources` is the sharper of the two: LoadAgentsRC synthesizes the default
// local source into the struct for consumers, and with no presence tracking
// that synthesized value serialized back on the next save — writing a
// declaration the author never made. Sources merge as an ordered REPLACE, so
// an injected local-only list beats (and wholly replaces) the source list an
// org layer supplies, exactly like the injected `false` beat an org layer's
// `true`.
//
// `version` is the milder case — only an already-invalid manifest omits it —
// but re-emitting `"version": 0` writes a value the schema's enum rejects, so
// it gets the same omitempty discipline.
func TestAgentsRC_AbsentSourcesAndVersionSurviveRoundTrip(t *testing.T) {
	tests := []struct {
		name        string
		manifest    string
		wantAbsent  []string
		wantPresent map[string]any
	}{
		{
			name: "absent sources stays absent",
			manifest: `{
  "version": 2,
  "repo_id": "github.com/acme/fixture",
  "extends": ["acme:base.json"]
}`,
			wantAbsent:  []string{"sources"},
			wantPresent: map[string]any{"version": float64(2)},
		},
		{
			name: "explicit sources is a real declaration and is preserved",
			manifest: `{
  "version": 2,
  "sources": [{"id": "acme", "type": "local", "path": "../orglayer"}]
}`,
			wantPresent: map[string]any{
				"sources": []any{map[string]any{"id": "acme", "type": "local", "path": "../orglayer"}},
			},
		},
		{
			name: "an explicitly declared default-shaped local source is NOT mistaken for the synthesized one",
			manifest: `{
  "version": 1,
  "sources": [{"type": "local"}]
}`,
			wantPresent: map[string]any{
				"sources": []any{map[string]any{"type": "local"}},
			},
		},
		{
			name: "absent version stays absent rather than re-emitting 0",
			manifest: `{
  "project": "fixture",
  "sources": [{"type": "local"}]
}`,
			wantAbsent: []string{"version"},
		},
		{
			name: "both absent at once",
			manifest: `{
  "project": "fixture",
  "extends": ["acme:base.json"]
}`,
			wantAbsent: []string{"version", "sources"},
			wantPresent: map[string]any{
				"project": "fixture",
				"extends": []any{"acme:base.json"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			saved, raw := roundTripManifest(t, tc.manifest)
			assertKeysAbsent(t, raw, saved, tc.wantAbsent)
			assertKeysEqual(t, raw, saved, tc.wantPresent)
		})
	}
}

// TestAgentsRC_SynthesizedSourcesSuppressionIsValueGuarded proves the
// suppression cannot swallow a real edit. `da agents import` and friends load
// the manifest, append a source, and save; the presence flag is still set from
// load, so only the value check keeps that new source from being dropped.
func TestAgentsRC_SynthesizedSourcesSuppressionIsValueGuarded(t *testing.T) {
	tests := []struct {
		name string
		// mutate runs between load and save.
		mutate func(rc *AgentsRC)
		// wantSources is the expected re-saved value; nil means the key must
		// stay absent.
		wantSources []any
	}{
		{
			name:        "untouched synthesized default stays absent",
			mutate:      func(*AgentsRC) {},
			wantSources: nil,
		},
		{
			name: "appending a git source re-declares the list",
			mutate: func(rc *AgentsRC) {
				rc.Sources = append(rc.Sources, Source{Type: "git", URL: "https://example.test/org.git"})
			},
			wantSources: []any{
				map[string]any{"type": "local"},
				map[string]any{"type": "git", "url": "https://example.test/org.git"},
			},
		},
		{
			name: "replacing the list outright re-declares it",
			mutate: func(rc *AgentsRC) {
				rc.Sources = []Source{{Type: "local", Path: "../shared"}}
			},
			wantSources: []any{map[string]any{"type": "local", "path": "../shared"}},
		},
		{
			name: "clearing the list leaves the key absent",
			mutate: func(rc *AgentsRC) {
				rc.Sources = nil
			},
			wantSources: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			raw := roundTripWithMutation(t, `{"version": 2, "project": "fixture"}`, tc.mutate)
			got, present := raw["sources"]
			if tc.wantSources == nil {
				if present {
					t.Errorf("sources must stay ABSENT, got %v", got)
				}
				return
			}
			if !present {
				t.Fatal("sources must be present after a real edit, but it was dropped")
			}
			if !jsonEqual(got, tc.wantSources) {
				t.Errorf("sources: got %v, want %v", got, tc.wantSources)
			}
		})
	}
}

// TestAgentsRC_LoadSynthesizesSourcesForConsumers pins the other half of the
// contract: suppressing the key on SAVE must not take the convenience default
// away from readers, which all assume LoadAgentsRC handed them a usable entry.
func TestAgentsRC_LoadSynthesizesSourcesForConsumers(t *testing.T) {
	tmp := t.TempDir()
	seedManifest(t, tmp, `{"version": 2, "project": "fixture"}`)

	rc, err := LoadAgentsRC(tmp)
	if err != nil {
		t.Fatalf("LoadAgentsRC: %v", err)
	}
	if len(rc.Sources) != 1 || rc.Sources[0].Type != "local" {
		t.Fatalf("Sources: got %+v, want one implicit local source", rc.Sources)
	}
	if !rc.sourcesSynthesized {
		t.Error("sourcesSynthesized must be set so Save can keep the key absent")
	}
}

// TestAgentsRC_SourcesSynthesisDefaultsToDeclared pins the fail-safe direction
// of the flag: anything this package did not synthesize itself — a plain
// Unmarshal, a struct literal built by another package, GenerateAgentsRC —
// serializes its sources exactly as before.
func TestAgentsRC_SourcesSynthesisDefaultsToDeclared(t *testing.T) {
	rc := &AgentsRC{Version: 2, Sources: []Source{{Type: "local"}}}
	data, err := json.Marshal(rc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := raw["sources"]; !ok {
		t.Errorf("a struct built outside LoadAgentsRC must emit sources, got %s", data)
	}
}

// seedManifest writes manifest into dir as .agentsrc.json.
func seedManifest(t *testing.T, dir, manifest string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, AgentsRCFile), []byte(manifest), 0644); err != nil {
		t.Fatalf("seeding manifest: %v", err)
	}
}

// roundTripWithMutation seeds a manifest, loads it, applies mutate, saves it
// back, and returns the re-saved manifest decoded as a JSON object.
func roundTripWithMutation(t *testing.T, manifest string, mutate func(rc *AgentsRC)) map[string]any {
	t.Helper()
	tmp := t.TempDir()
	seedManifest(t, tmp, manifest)

	rc, err := LoadAgentsRC(tmp)
	if err != nil {
		t.Fatalf("LoadAgentsRC: %v", err)
	}
	mutate(rc)
	if err := rc.Save(tmp); err != nil {
		t.Fatalf("Save: %v", err)
	}
	saved, err := os.ReadFile(filepath.Join(tmp, AgentsRCFile))
	if err != nil {
		t.Fatalf("reading saved manifest: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(saved, &raw); err != nil {
		t.Fatalf("parsing saved manifest: %v\n%s", err, saved)
	}
	return raw
}
