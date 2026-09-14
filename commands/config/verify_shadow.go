package config

import (
	"encoding/json"
	"fmt"
	"sort"

	cfg "github.com/AGOrcha/dot-agents/internal/config"
)

// shadowStructuralKeys are repo-local keys the shadow check never reports.
//
// They are manifest STRUCTURE rather than policy: `version` is required by
// schemas/agentsrc.schema.json (so a repo restating it is mandatory, not
// redundant) and `$schema` is an editor/validator pointer with no projection
// behavior behind it. Every layer carries both, so without this exemption the
// check would flag both on essentially every layered repo — noise that would
// train operators to ignore the real findings below it.
var shadowStructuralKeys = map[string]struct{}{
	"version": {},
	"$schema": {},
}

// verifyLayerShadows reports repo-local manifest keys that the layer stack
// underneath also supplies.
//
// This is the diagnostic for the aftermath of the injected-`false` corruption
// fixed in #535, and for the same shape reached by hand-editing. The layer
// merge is key-PRESENCE driven: a repo-local key wins over every lower layer
// whether or not the author meant it to. Once a bad write (or a copy-paste)
// leaves `"settings": false` sitting beside an `extends`, nothing in the
// resolved output says whether that false is a deliberate override or a
// leftover — the value simply wins, silently, forever. Two shapes matter and
// they are reported differently:
//
//   - REDUNDANT (warn): the repo value equals what the stack would supply
//     anyway. Harmless today, but it silently pins the repo against a future
//     org-layer change — exactly the failure mode where an org flips a default
//     and one repo mysteriously does not follow. Actionable: delete the key.
//   - OVERRIDE (pass): the repo value differs. That is the feature working as
//     designed, so it does not warn — but both values and the losing layer are
//     named, because an operator auditing an org rollout needs to see which
//     repos flipped a layer value and to what.
//
// Nothing here can fail the report: a manifest key is never wrong on its own,
// only surprising.
func verifyLayerShadows(snap *cfg.Snapshot) []VerifyCheck {
	repo := repoLocalRaw(snap)
	if len(repo) == 0 {
		return nil
	}
	below := &cfg.Snapshot{Layers: layersBelowRepoLocal(snap)}

	var checks []VerifyCheck
	for _, key := range shadowCandidateKeys(repo) {
		if check, ok := shadowCheck(key, repo[key], below); ok {
			checks = append(checks, check)
		}
	}
	if len(checks) == 0 {
		return []VerifyCheck{{"layer-shadows", verifyPass, "no repo-local key restates a layer-supplied value"}}
	}
	return checks
}

// layersBelowRepoLocal returns the layer stack with the repo-local layer
// removed — i.e. what would resolve if this manifest declared nothing.
//
// Only repo-local is dropped. The project-local overlay (.agentsrc.local.json)
// is uncommitted but still outranks every imported layer, so it IS what would
// take effect if a repo-local key were deleted, and keeping it in the stack is
// what makes the comparison answer the operator's actual question.
func layersBelowRepoLocal(snap *cfg.Snapshot) []cfg.ResolvedLayer {
	if snap == nil {
		return nil
	}
	out := make([]cfg.ResolvedLayer, 0, len(snap.Layers))
	for _, layer := range snap.Layers {
		if layer.ID != cfg.LayerRepoLocal {
			out = append(out, layer)
		}
	}
	return out
}

// shadowCandidateKeys returns the sorted repo-local keys worth comparing
// against the layer stack.
//
// Only CategoryScalar keys qualify: a scalar is replaced wholesale by the
// highest-precedence writer, which is what makes shadowing lossy and worth
// reporting. Set-union, map-merge and ordered-replace keys combine (or are
// declared repo-local by design, as `sources` is), so a repo-local entry there
// is a contribution rather than a shadow. Protected fields (repo_id, project)
// are repo-local-only by construction and can never shadow anything.
func shadowCandidateKeys(repo map[string]any) []string {
	protected := make(map[string]struct{}, len(cfg.ProtectedFields))
	for _, f := range cfg.ProtectedFields {
		protected[f] = struct{}{}
	}

	keys := make([]string, 0, len(repo))
	for key := range repo {
		if _, skip := shadowStructuralKeys[key]; skip {
			continue
		}
		if _, skip := protected[key]; skip {
			continue
		}
		if cfg.FieldMergeCategory(key) != cfg.CategoryScalar {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// shadowCheck compares one repo-local key against what the stack below would
// supply. The bool is false when no lower layer supplies the key at all — the
// repo is the sole declarer, so there is nothing to report.
func shadowCheck(key string, repoValue any, below *cfg.Snapshot) (VerifyCheck, bool) {
	layered := below.FieldAt(key)
	if layered.ActiveLayer == "" {
		return VerifyCheck{}, false
	}
	name := "shadow:" + key
	if shadowValuesEqual(repoValue, layered.Value) {
		return VerifyCheck{name, verifyWarn, fmt.Sprintf(
			"REDUNDANT — repo-local %s=%s restates what %s already supplies; remove the key to defer to the layer stack",
			key, renderShadowValue(repoValue), layered.ActiveLayer,
		)}, true
	}
	return VerifyCheck{name, verifyPass, fmt.Sprintf(
		"OVERRIDE — repo-local %s=%s replaces %s=%s from %s",
		key, renderShadowValue(repoValue), key, renderShadowValue(layered.Value), layered.ActiveLayer,
	)}, true
}

// shadowValuesEqual compares two decoded JSON values structurally. Both sides
// come from encoding/json, so marshaling back is a faithful comparison —
// json.Marshal sorts map keys, which keeps object comparison order-independent.
// A value that cannot be re-marshaled is reported as unequal, which downgrades
// the finding to an OVERRIDE rather than claiming a redundancy it cannot prove.
func shadowValuesEqual(a, b any) bool {
	left, err := json.Marshal(a)
	if err != nil {
		return false
	}
	right, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return string(left) == string(right)
}

// renderShadowValue formats a decoded JSON value for a one-line check detail.
func renderShadowValue(v any) string {
	encoded, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(encoded)
}
