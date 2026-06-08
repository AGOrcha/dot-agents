package config

import (
	"encoding/json"
	"reflect"
	"testing"
)

func newLayer(id string, raw map[string]any) ResolvedLayer {
	return ResolvedLayer{ID: id, Present: raw != nil, Raw: raw}
}

func TestSplitFieldPath(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"repo_id", []string{"repo_id"}},
		{"kg.backend", []string{"kg", "backend"}},
		{"stage_profiles.verifier.unit", []string{"stage_profiles", "verifier", "unit"}},
		{"a.b.c.d", []string{"a", "b", "c", "d"}},
	}
	for _, tc := range cases {
		got := splitFieldPath(tc.in)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("splitFieldPath(%q) = %#v, want %#v", tc.in, got, tc.want)
		}
	}
}

func TestLookupPath(t *testing.T) {
	layer := map[string]any{
		"version": float64(2),
		"kg":      map[string]any{"backend": "sqlite"},
	}
	cases := []struct {
		name      string
		parts     []string
		wantValue any
		wantOK    bool
	}{
		{"top-level scalar", []string{"version"}, float64(2), true},
		{"nested key", []string{"kg", "backend"}, "sqlite", true},
		{"missing top-level", []string{"absent"}, nil, false},
		{"missing nested", []string{"kg", "absent"}, nil, false},
		{"descend into scalar", []string{"version", "x"}, nil, false},
		{"empty parts", nil, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := lookupPath(layer, tc.parts)
			if ok != tc.wantOK || !reflect.DeepEqual(got, tc.wantValue) {
				t.Errorf("lookupPath(%v) = (%v,%v), want (%v,%v)", tc.parts, got, ok, tc.wantValue, tc.wantOK)
			}
		})
	}
	if _, ok := lookupPath(nil, []string{"x"}); ok {
		t.Error("lookupPath on nil layer should be (nil,false)")
	}
}

func TestFieldAtActiveLayerIsHighestPrecedence(t *testing.T) {
	snap := &Snapshot{
		Layers: []ResolvedLayer{
			newLayer(LayerProductDefaults, map[string]any{"version": float64(1)}),
			newLayer(LayerUserLocal, map[string]any{"version": float64(2)}),
			newLayer(LayerRepoLocal, map[string]any{"version": float64(3)}),
		},
	}
	fp := snap.FieldAt("version")
	if fp.ActiveLayer != LayerRepoLocal {
		t.Fatalf("ActiveLayer = %q, want %q", fp.ActiveLayer, LayerRepoLocal)
	}
	if len(fp.Layers) != 3 {
		t.Fatalf("want full 3-layer stack, got %d", len(fp.Layers))
	}
	activeCount := 0
	for _, lv := range fp.Layers {
		if lv.Active {
			activeCount++
			if lv.Layer != LayerRepoLocal || lv.Value != float64(3) {
				t.Errorf("active entry wrong: %+v", lv)
			}
		}
	}
	if activeCount != 1 {
		t.Errorf("want exactly one active layer, got %d", activeCount)
	}
}

func TestFieldAtUnsetEverywhere(t *testing.T) {
	snap := &Snapshot{
		Layers: []ResolvedLayer{
			newLayer(LayerProductDefaults, map[string]any{}),
			newLayer(LayerRepoLocal, map[string]any{"version": float64(2)}),
		},
	}
	fp := snap.FieldAt("does-not-exist")
	if fp.ActiveLayer != "" {
		t.Errorf("ActiveLayer = %q, want empty", fp.ActiveLayer)
	}
	for _, lv := range fp.Layers {
		if lv.Active || lv.Value != nil {
			t.Errorf("unset field should have no active/value entries: %+v", lv)
		}
	}
}

func TestFieldAtMiddleLayerWins(t *testing.T) {
	snap := &Snapshot{
		Layers: []ResolvedLayer{
			newLayer(LayerProductDefaults, map[string]any{"project": "p0"}),
			newLayer(LayerUserLocal, map[string]any{"project": "p1"}),
			newLayer(LayerRepoLocal, map[string]any{}),
		},
	}
	fp := snap.FieldAt("project")
	if fp.ActiveLayer != LayerUserLocal {
		t.Errorf("ActiveLayer = %q, want %q", fp.ActiveLayer, LayerUserLocal)
	}
}

func TestFieldNames(t *testing.T) {
	snap := &Snapshot{
		Layers: []ResolvedLayer{
			newLayer(LayerUserLocal, map[string]any{"skills": []any{}, "version": float64(2)}),
			newLayer(LayerRepoLocal, map[string]any{"version": float64(2), "project": "x"}),
		},
	}
	got := snap.FieldNames()
	want := []string{"project", "skills", "version"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FieldNames() = %v, want %v", got, want)
	}
}

func TestEffectiveRaw(t *testing.T) {
	snap := &Snapshot{Effective: AgentsRC{Version: 2, Project: "demo", RepoID: "github.com/acme/demo"}}
	raw, err := snap.EffectiveRaw()
	if err != nil {
		t.Fatal(err)
	}
	if raw["version"] != float64(2) {
		t.Errorf("version = %v, want 2", raw["version"])
	}
	if raw["repo_id"] != "github.com/acme/demo" {
		t.Errorf("repo_id = %v", raw["repo_id"])
	}
}

func TestSnapshotJSONMarshalsEmptySlicesNotNull(t *testing.T) {
	// [[additive-state-fields]]: slices must marshal to [] not null.
	snap := &Snapshot{
		Provenance: map[string]FieldProvenance{"version": {Layers: []LayerValue{}}},
		Layers:     []ResolvedLayer{},
		Warnings:   []ProvenanceWarning{},
	}
	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"layers", "warnings"} {
		if string(decoded[key]) != "[]" {
			t.Errorf("%s marshaled to %s, want []", key, decoded[key])
		}
	}
}
