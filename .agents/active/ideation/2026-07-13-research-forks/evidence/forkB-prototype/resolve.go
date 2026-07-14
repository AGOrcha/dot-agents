package forkbproto

import "sort"

// ResolveTwoPhase is H_B1: model_family is resolved in a FROZEN phase-1 (only
// family-AGNOSTIC fragments contribute; family-scoped selectors cannot match
// because ctxFamily is ""), then phase-2 applies family-scoped fragments against
// that frozen family. NO-SELF-REFERENCE rule: a family-scoped fragment may NOT
// write model_family — any such write in phase-2 is dropped, so a family-scoped
// fragment can never change the frozen family it (or its peers) selected on. This
// is exactly the rule pre-registered as the condition for H_B1 to be deterministic.
func ResolveTwoPhase(set ProfileSet, ctx Context) Resolution {
	// --- Phase 1: freeze the effective model_family (family-agnostic only) ---
	p1policy := resolveEffectivePolicy(set, ctx, "")
	var p1frags []ConfigProfile
	for _, pr := range set.Profiles {
		if !ctx.inChain(pr.Scope) || pr.Selector.familyScoped() {
			continue
		}
		if pr.Selector.matches(ctx, "") {
			p1frags = append(p1frags, pr)
		}
	}
	orderProfiles(p1frags, p1policy)
	p1bundle := map[string]any{}
	for _, pr := range p1frags {
		mergeInto(p1bundle, pr, p1policy)
	}
	// Locks are absolute across BOTH phases: a value-lock on model_family binds the
	// frozen family here (real: locks fold through the authority pass regardless of
	// which fragments won the value merge).
	applyLocks(p1bundle, p1policy.Locks, ctx, "")
	frozen := familyOf(p1bundle)

	// --- Phase 2: apply ALL matching fragments against the frozen family ---
	p2policy := resolveEffectivePolicy(set, ctx, frozen)
	var p2frags []ConfigProfile
	for _, pr := range set.Profiles {
		if !ctx.inChain(pr.Scope) {
			continue
		}
		if pr.Selector.matches(ctx, frozen) {
			p2frags = append(p2frags, pr)
		}
	}
	orderProfiles(p2frags, p2policy)
	bundle := map[string]any{}
	var refs []string
	for _, pr := range p2frags {
		mergeIntoNoFamilySelfRef(bundle, pr, p2policy)
		refs = append(refs, pr.Ref)
	}
	// Re-assert the frozen family (no-self-reference guarantee): phase-2 never
	// changes it, even if a family-agnostic higher-Order fragment also matched in
	// phase-2 — the frozen value is authoritative. Only when phase-1 actually
	// resolved a family; an unresolved family leaves model_family absent (no
	// synthetic empty key).
	if frozen != "" {
		bundle[ModelFamilyKey] = frozen
	}
	applied := applyLocks(bundle, p2policy.Locks, ctx, frozen)
	// A value-lock could legitimately pin model_family; honor it as the frozen value.
	frozen = familyOf(bundle)
	sort.Strings(refs)
	return Resolution{
		Bundle: bundle, Contributing: refs, Family: frozen,
		Mode: p2policy.Mode, ReplacedBy: p2policy.Replaced, AppliedLocks: applied,
	}
}

// ResolveNaiveSinglePhase is the BROKEN control (H_B1 without the phase split):
// one pass in INPUT ORDER, maintaining a running bundle; a family-scoped fragment
// is matched against whatever model_family the running bundle holds AT THE MOMENT
// it is visited. Because family-value fragments and family-selector fragments
// interleave in input order, the match decision — and often the final family —
// depend on input order. This is the resolution-order hazard the two-phase rule
// exists to kill.
func ResolveNaiveSinglePhase(set ProfileSet, ctx Context) Resolution {
	policy := resolveEffectivePolicy(set, ctx, "")
	bundle := map[string]any{}
	var refs []string
	for _, pr := range set.Profiles { // INPUT ORDER — deliberately not sorted
		if !ctx.inChain(pr.Scope) {
			continue
		}
		running := familyOf(bundle)
		if !pr.Selector.matches(ctx, running) {
			continue
		}
		mergeInto(bundle, pr, policy)
		refs = append(refs, pr.Ref)
	}
	applied := applyLocks(bundle, policy.Locks, ctx, familyOf(bundle))
	sort.Strings(refs)
	return Resolution{
		Bundle: bundle, Contributing: refs, Family: familyOf(bundle),
		Mode: policy.Mode, ReplacedBy: policy.Replaced, AppliedLocks: applied,
	}
}

// mergeInto deep-merges a fragment's bundle, gated by the permission cap.
func mergeInto(acc map[string]any, pr ConfigProfile, policy EffectivePolicy) {
	for _, key := range sortedKeys(pr.Bundle) {
		if !mayChange(policy.Permissions, pr.Scope, key) {
			continue
		}
		acc[key] = mergeMaps(acc[key], pr.Bundle[key])
	}
}

// mergeIntoNoFamilySelfRef is mergeInto plus the NO-SELF-REFERENCE rule: a
// family-SCOPED fragment may not write model_family (the frozen phase-1 value is
// immutable in phase-2).
func mergeIntoNoFamilySelfRef(acc map[string]any, pr ConfigProfile, policy EffectivePolicy) {
	for _, key := range sortedKeys(pr.Bundle) {
		if pr.Selector.familyScoped() && key == ModelFamilyKey {
			continue // no self-reference: a family-scoped fragment cannot re-pin family
		}
		if !mayChange(policy.Permissions, pr.Scope, key) {
			continue
		}
		acc[key] = mergeMaps(acc[key], pr.Bundle[key])
	}
}
