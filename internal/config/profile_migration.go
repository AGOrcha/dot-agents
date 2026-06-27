package config

import (
	"encoding/json"
	"sort"
)

// profile_migration.go re-expresses today's execution_profile.by_app_type and
// stage_profiles config as kind:profile units (§5.1). It is the dogfood that
// proves the unified engine subsumes the legacy profile surfaces with ZERO
// behavioral diff (§5.2): for every (app_type, stage) context the legacy paths
// handle, ResolveProfile over these derived units yields the byte-identical
// effective fragment the legacy CategoryMapMerge produces. The helpers are pure
// projections — they do not change the legacy types or their resolution.

// AppTypeProfileRefPrefix is the synthetic source segment for app_type profiles
// derived from execution_profile.by_app_type. The absolute ref is
// "<prefix>:<app_type>".
const AppTypeProfileRefPrefix = "execution-profile"

// StageProfileRefPrefix is the synthetic source segment for stage profiles
// derived from stage_profiles. The absolute ref is "<prefix>:<stage>".
const StageProfileRefPrefix = "stage-profile"

// profilesFromExecutionProfile re-expresses each execution_profile.by_app_type
// entry as one app_type-kind profile selected by {app_type: <key>}, carried at
// the given source-derived authority scope. source is the real provenance of the
// contributing layer (its lock/source ref), woven into the absolute ref so two
// DIFFERENT source sets produce DISTINGUISHABLE refs — and therefore distinct
// digests (Decision 7 / FIX 4) — even when their resolved values coincide. The
// bundle is the AppTypeProfile marshaled to the same JSON object the legacy merge
// operates on, so a deep-map merge of these fragments equals the legacy
// execution_profile merge.
func profilesFromExecutionProfile(ep *ExecutionProfile, scope AuthorityScope, source string, order int) []ConfigProfile {
	if ep == nil || len(ep.ByAppType) == 0 {
		return nil
	}
	out := make([]ConfigProfile, 0, len(ep.ByAppType))
	for _, appType := range sortedStringKeys(ep.ByAppType) {
		out = append(out, ConfigProfile{
			Ref:      derivedRef(source, AppTypeProfileRefPrefix, appType),
			Kind:     ProfileKindAppType,
			Scope:    scope,
			Order:    order,
			Selector: ProfileSelector{AppType: appType},
			Bundle:   toBundle(ep.ByAppType[appType]),
		})
	}
	return out
}

// profilesFromStageProfiles re-expresses stage_profiles (stage → slug → profile)
// as one stage-kind profile per stage, selected by {stage: <stage>}, carried at
// the given source-derived authority scope, with source provenance woven into the
// ref (FIX 4). The bundle is the slug→profile map for that stage; a deep-map merge
// across scopes equals the legacy stage_profiles merge.
func profilesFromStageProfiles(sp map[string]map[string]StageProfile, scope AuthorityScope, source string, order int) []ConfigProfile {
	if len(sp) == 0 {
		return nil
	}
	out := make([]ConfigProfile, 0, len(sp))
	for _, stage := range sortedStageKeys(sp) {
		out = append(out, ConfigProfile{
			Ref:      derivedRef(source, StageProfileRefPrefix, stage),
			Kind:     ProfileKindStage,
			Scope:    scope,
			Order:    order,
			Selector: ProfileSelector{Stage: stage},
			Bundle:   toBundle(sp[stage]),
		})
	}
	return out
}

// derivedRef builds a source-provenanced absolute ref "<source>:<prefix>:<name>"
// for a profile derived from a contributing layer's config. The source segment is
// what carries provenance into the digest (FIX 4); it falls back to the bare
// prefix when the layer has no distinct source id.
func derivedRef(source, prefix, name string) string {
	if source == "" {
		return prefix + ":" + name
	}
	return source + ":" + prefix + ":" + name
}

// toBundle marshals a typed value into the generic JSON object the engine merges
// — the same shape the legacy CategoryMapMerge sees, which is what makes the
// re-expression zero-diff. The inputs are always plain structs/maps (never
// channels/funcs/cycles), so the round-trip cannot fail; the errors are
// discarded by the same convention the rest of the package uses for an
// impossible-marshal (e.g. WriteUnitsLock), keeping the helper branch-free.
func toBundle(v any) map[string]any {
	data, _ := json.Marshal(v)
	m := map[string]any{}
	_ = json.Unmarshal(data, &m)
	return m
}

// sortedStringKeys returns the keys of a string-keyed map in deterministic order.
func sortedStringKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// sortedStageKeys returns the stage keys of a stage_profiles map in order.
func sortedStageKeys(m map[string]map[string]StageProfile) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
