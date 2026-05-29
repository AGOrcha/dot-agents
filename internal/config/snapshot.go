package config

import (
	"encoding/json"
	"sort"
)

// Layer identifiers, lowest precedence first. These mirror the layer model in
// org-config-resolution §4. The FlatResolver only produces the three FLAT-scope
// layers below; imported org/team/repo extends layers (config-v2 p1b) slot in
// between LayerUserLocal and LayerRepoLocal with the same provenance surface.
//
// They are the canonical identifiers that `da config explain` (config-v2 p4)
// renders.
const (
	// LayerProductDefaults is the built-in defaults layer shipped by dot-agents.
	LayerProductDefaults = "product-defaults"
	// LayerUserLocal is ~/.agents/.agentsrc.json (machine-local preferences).
	LayerUserLocal = "user-local"
	// LayerRepoLocal is the committed repo-local <project>/.agentsrc.json.
	LayerRepoLocal = "repo-local"
)

// ProtectedFields are repo-owned scalars that an imported (non-repo-local)
// layer must not override (org-config-resolution §7.4). An attempt to set one
// from a lower-precedence layer is dropped and recorded as a non-fatal
// ProvenanceWarning. In FLAT scope there are no imported layers, so a protected
// field simply resolves from whichever local layer set it; the guard becomes
// load-bearing in p1b when extends layers can carry these keys.
var ProtectedFields = []string{"repo_id", "project"}

// LayerValue is one slot in a single field's provenance stack: the value (if
// any) that a given layer contributed for that field path.
//
// Active is true on exactly one entry per field — the winning (highest
// precedence) layer that set a value. When no layer sets the field, every entry
// has Active=false and Value=nil.
type LayerValue struct {
	// Layer is the layer identifier (LayerProductDefaults, …).
	Layer string `json:"layer"`
	// Value is the JSON-decoded value this layer contributed, or nil if the
	// layer did not set this field.
	Value any `json:"value"`
	// Active marks the winning layer for the field.
	Active bool `json:"active"`
}

// FieldProvenance is the full layer stack for a single field path, ordered by
// precedence (lowest first). ActiveLayer is the identifier of the winning layer,
// or "" when no layer set the field.
type FieldProvenance struct {
	// ActiveLayer is the winning layer id, or "" when the field is unset
	// everywhere.
	ActiveLayer string `json:"active_layer"`
	// Layers is the ordered (lowest precedence first) per-layer stack. Always
	// a non-nil slice so JSON marshals to [] not null.
	Layers []LayerValue `json:"layers"`
}

// ResolvedLayer is one input layer that participated in resolution, in
// precedence order. Raw holds the layer's decoded top-level JSON object (nil
// when the layer was absent, e.g. no user-local file), so explain surfaces can
// distinguish "layer present but empty" from "layer absent".
type ResolvedLayer struct {
	// ID is the layer identifier (LayerProductDefaults, …).
	ID string `json:"id"`
	// Present reports whether the layer existed (file on disk / built-in stub).
	Present bool `json:"present"`
	// Raw is the layer's decoded top-level object, or nil when absent.
	Raw map[string]any `json:"raw,omitempty"`
}

// ProvenanceWarning is a non-fatal event emitted during resolution — currently
// only protected-field override attempts (org-config-resolution §7.4). It maps
// to the config.field.protection_violation audit event landed in config-v2 p3.
type ProvenanceWarning struct {
	// FieldPath is the dot-separated path of the offending field.
	FieldPath string `json:"field_path"`
	// AttemptedByLayer is the layer that tried to set a protected field.
	AttemptedByLayer string `json:"attempted_by_layer"`
	// Outcome is the disposition of the attempt; always "dropped" today.
	Outcome string `json:"outcome"`
}

// Snapshot is the resolved effective-config view produced by a Resolver. It is
// the canonical surface consumed by `da config explain`, `workflow app-types`,
// bundle materialization, and config validation (config-distribution-model §10).
type Snapshot struct {
	// Effective is the merged manifest after applying every layer per the
	// category merge rules (org-config-resolution §7.2).
	Effective AgentsRC `json:"effective"`
	// Provenance maps a dot-separated field path to its full layer stack. Only
	// top-level manifest keys are pre-populated; explain surfaces may request
	// deeper paths via FieldAt.
	Provenance map[string]FieldProvenance `json:"provenance"`
	// Layers is the ordered (lowest precedence first) set of input layers.
	Layers []ResolvedLayer `json:"layers"`
	// Warnings holds non-fatal resolution events (protected-field violations).
	// Always non-nil ([]ProvenanceWarning{}) so JSON marshals to [].
	Warnings []ProvenanceWarning `json:"warnings"`
}

// EffectiveRaw returns the effective config as a decoded top-level JSON object.
// This is the shape explain surfaces walk for arbitrary dot-paths; it round-trips
// through the AgentsRC marshaler so ExtraFields (verifier_profiles,
// app_type_verifier_map, …) are included.
func (s *Snapshot) EffectiveRaw() (map[string]any, error) {
	data, err := json.Marshal(s.Effective)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// FieldAt returns the FieldProvenance for an arbitrary dot-separated path
// (e.g. "kg.backend", "app_type_verifier_map.go-cli"), computing it on demand by
// walking each layer's raw object. Top-level paths are also available directly
// in s.Provenance; FieldAt is the general accessor that also handles nested keys.
func (s *Snapshot) FieldAt(path string) FieldProvenance {
	parts := splitFieldPath(path)
	out := FieldProvenance{Layers: make([]LayerValue, 0, len(s.Layers))}
	for _, layer := range s.Layers {
		entry := LayerValue{Layer: layer.ID}
		if val, ok := lookupPath(layer.Raw, parts); ok {
			entry.Value = val
			out.ActiveLayer = layer.ID // last (highest precedence) writer wins
		}
		out.Layers = append(out.Layers, entry)
	}
	if out.ActiveLayer != "" {
		for i := range out.Layers {
			if out.Layers[i].Layer == out.ActiveLayer {
				out.Layers[i].Active = true
			}
		}
	}
	return out
}

// FieldNames returns the sorted union of top-level field names that any layer
// sets — the keys explain's --all view iterates.
func (s *Snapshot) FieldNames() []string {
	seen := map[string]struct{}{}
	for _, layer := range s.Layers {
		for k := range layer.Raw {
			seen[k] = struct{}{}
		}
	}
	names := make([]string, 0, len(seen))
	for k := range seen {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// splitFieldPath splits a dot-separated path into traversal parts. Empty input
// returns nil so callers short-circuit on len==0.
func splitFieldPath(path string) []string {
	if path == "" {
		return nil
	}
	parts := make([]string, 0)
	start := 0
	for i := 0; i < len(path); i++ {
		if path[i] == '.' {
			parts = append(parts, path[start:i])
			start = i + 1
		}
	}
	return append(parts, path[start:])
}

// lookupPath walks layer following parts and returns (value, true) only when
// every step resolved against an object key. A non-object intermediate or a
// missing key short-circuits to (nil, false).
func lookupPath(layer map[string]any, parts []string) (any, bool) {
	if layer == nil || len(parts) == 0 {
		return nil, false
	}
	var cur any = layer
	for _, p := range parts {
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		v, present := obj[p]
		if !present {
			return nil, false
		}
		cur = v
	}
	return cur, true
}
