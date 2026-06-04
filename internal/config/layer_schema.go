package config

import (
	"encoding/json"
	"fmt"
)

// layerForbiddenFields are top-level keys an imported config layer may NOT set.
// They split into two classes:
//
//   - Protected identity fields (repo_id, project): repo-owned per
//     org-config-resolution §7.4. An imported layer carrying one is not a fatal
//     schema error — the value is dropped and a ProvenanceWarning is recorded,
//     mirroring the resolver's protected-field handling in resolveSnapshot.
//   - Structural-only fields (version, $schema): a layer is a policy fragment,
//     not a full manifest, so these carry no meaning and are dropped silently.
//
// The set is intentionally a subset of the full AgentsRC surface: a layer
// contributes policy (skills, rules, agents, verifier_profiles, features, kg,
// sources, extends, packages, execution_profile overrides), never a full repo
// manifest. execution_profile is deliberately absent here: it is the
// config-v2 §15 execution-profile layer (kind=layer) and MUST merge by scope
// precedence, so it is layer-settable and never a protected identity field.
var layerForbiddenFields = map[string]struct{}{
	"repo_id": {},
	"project": {},
	"version": {},
	"$schema": {},
}

// validateLayer checks a fetched layer's decoded top-level object against the
// AgentsRC layer schema (the subset valid for imported layers) and returns the
// sanitized object to merge plus any non-fatal warnings.
//
// Validation rules:
//   - A nil payload is rejected as a schema error so an empty/null fetch is
//     loud rather than a silent no-op layer.
//   - Protected identity fields (repo_id, project) set by the layer are dropped
//     and surfaced as a ProvenanceWarning attributed to layerID, matching
//     resolveSnapshot's protected-field handling for the local layers.
//   - Structural-only fields (version, $schema) are dropped silently.
//
// A schema-level violation (nil payload) returns a non-nil error so the caller
// can map it to config.import.failed reason=schema. Field-level drops are
// non-fatal and reported via the returned warnings.
func validateLayer(layerID string, raw map[string]any) (map[string]any, []ProvenanceWarning, error) {
	if raw == nil {
		return nil, nil, fmt.Errorf("layer %q is null, expected a JSON object", layerID)
	}
	sanitized := make(map[string]any, len(raw))
	warnings := []ProvenanceWarning{}
	for k, v := range raw {
		if _, forbidden := layerForbiddenFields[k]; forbidden {
			if _, prot := protectedSet[k]; prot {
				warnings = append(warnings, ProvenanceWarning{
					FieldPath:        k,
					AttemptedByLayer: layerID,
					Outcome:          "dropped",
				})
			}
			continue
		}
		sanitized[k] = v
	}
	return sanitized, warnings, nil
}

// decodeLayerBytes parses raw layer.json bytes into a top-level JSON object.
// A payload that is not a JSON object is a schema violation (the caller maps it
// to config.import.failed reason=schema).
func decodeLayerBytes(layerID string, data []byte) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("layer %q is not valid JSON: %w", layerID, err)
	}
	if m == nil {
		return nil, fmt.Errorf("layer %q is null, expected a JSON object", layerID)
	}
	return m, nil
}
