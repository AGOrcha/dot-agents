package config

// Lockfile resolution of a named verifier precondition policy
// (verifier-precondition-policy plan, Slice B3/B4).
//
// The in_progress → awaiting_agent_review gate is a *policy* of predicates over
// the unified event/signal contract (design.md §2.3). A policy is a named entry
// in the top-level precondition_policies registry, referenced by name from a
// verifier stage profile, and the verifier reads the policy *resolved from the
// lockfile* — never raw .agentsrc.json — so it observes the same merged, locked
// effective config as `da config explain` (offline, no fetch, no lock mutation).
//
// Import-cycle note: internal/config must not import commands/workflow (which
// already imports internal/config). This resolver therefore returns a config-side
// ResolvedPreconditionPolicy/ResolvedPredicate shape that mirrors the workflow
// Predicate/PreconditionPolicy fields exactly; the workflow call site converts it
// to its own types. This is the lower-churn option vs. relocating the Slice A
// types to a new leaf package and rewiring every workflow reference.

// ResolvedPredicate is the config-side mirror of the commands/workflow Predicate:
// one predicate over a single registered event/signal kind. The workflow call
// site converts this into workflow.Predicate (field-for-field).
type ResolvedPredicate struct {
	// Signal is the registered kind, e.g. "event.pr.open", "gate.quality.sonar".
	Signal string
	// Args are kind-specific parameters, e.g. {"equals":"green"}.
	Args map[string]string
}

// ResolvedPreconditionPolicy is the config-side mirror of the commands/workflow
// PreconditionPolicy. Name is the registry key the policy resolved from (or
// "default" for the built-in fallback). An empty Predicates slice means the
// workflow evaluator should apply its built-in default policy.
type ResolvedPreconditionPolicy struct {
	// Name is the resolved policy's registry key, or "default" when the
	// resolution fell back to the built-in gate.
	Name string
	// Predicates is the ordered predicate set. Empty ⇒ built-in default.
	Predicates []ResolvedPredicate
}

// defaultPolicyName is the built-in fallback policy name. The workflow package
// owns the actual default predicate set; the resolver returns an empty-predicate
// policy under this name so the workflow evaluator applies its built-in default
// (the gate is never open by omission, §2.3).
const defaultPolicyName = "default"

// verifierStageKey is the stage_profiles outer-map key for verifier profiles —
// the stage whose profiles carry the precondition_policy reference.
const verifierStageKey = "verifier"

// ResolvePreconditionPolicy resolves the verifier precondition policy for an
// app_type from the LOCKED effective config (the lockfile-backed Snapshot, never
// raw LoadAgentsRC). Resolution chain:
//
//	app_type
//	  → execution_profile.by_app_type[app_type].topology.verifier_sequence (slugs)
//	  → the first verifier stage profile in that sequence naming a
//	    precondition_policy
//	  → that name's entry in the top-level precondition_policies registry
//	  → converted ResolvedPreconditionPolicy
//
// An unset name (no profile names a policy) OR an unset/absent registry entry
// yields the built-in default (Name="default", empty predicates) — never an
// error. A profile naming a policy that IS declared but with no predicates also
// resolves to that named-but-empty policy; the workflow evaluator falls back to
// its default for an empty predicate set, so the gate still holds.
//
// Hard validation of a profile naming a *missing* registry key is Slice B5's
// job (da config verify); here a missing key simply degrades to the default so
// the verifier always has a usable gate.
func ResolvePreconditionPolicy(projectPath, appType string) (ResolvedPreconditionPolicy, error) {
	snap, err := NewLayeredResolver().ResolveLocked(projectPath)
	if err != nil {
		return ResolvedPreconditionPolicy{}, err
	}
	return resolvePreconditionPolicyFromSnapshot(snap, appType), nil
}

// resolvePreconditionPolicyFromSnapshot is the pure resolution core, split from
// ResolvePreconditionPolicy so it is unit-testable against a synthetic Snapshot
// without touching the on-disk lock/cache.
func resolvePreconditionPolicyFromSnapshot(snap *Snapshot, appType string) ResolvedPreconditionPolicy {
	defaultPolicy := ResolvedPreconditionPolicy{Name: defaultPolicyName}
	if snap == nil {
		return defaultPolicy
	}
	name := preconditionPolicyName(&snap.Effective, appType)
	if name == "" {
		return defaultPolicy
	}
	spec, ok := snap.Effective.PreconditionPolicies[name]
	if !ok {
		// Slice B5 turns this into a hard error in `da config verify`; the
		// resolver degrades to the default so the gate is never open by omission.
		return defaultPolicy
	}
	return ResolvedPreconditionPolicy{Name: name, Predicates: convertPredicates(spec.Predicates)}
}

// preconditionPolicyName walks app_type → verifier_sequence → first profile that
// names a precondition_policy, and returns that name. Returns "" when no verifier
// profile in the sequence names a policy (⇒ caller applies the built-in default).
func preconditionPolicyName(rc *AgentsRC, appType string) string {
	sequence := verifierSequenceFor(rc.ExecutionProfile, appType)
	if len(sequence) == 0 {
		return ""
	}
	verifierProfiles := rc.StageProfiles[verifierStageKey]
	for _, slug := range sequence {
		if name := verifierProfiles[slug].PreconditionPolicy; name != "" {
			return name
		}
	}
	return ""
}

// verifierSequenceFor returns the ordered verifier-profile slugs for an app_type
// from the execution_profile topology, or nil when the profile, app_type, or
// sequence is absent.
func verifierSequenceFor(ep *ExecutionProfile, appType string) []string {
	if ep == nil || ep.ByAppType == nil || appType == "" {
		return nil
	}
	prof, ok := ep.ByAppType[appType]
	if !ok {
		return nil
	}
	return prof.Topology.VerifierSequence
}

// convertPredicates maps the config PreconditionPolicySpec predicates onto the
// resolver's ResolvedPredicate shape. Returns nil for an empty input so the
// workflow evaluator applies its built-in default for a named-but-empty policy.
func convertPredicates(specs []PredicateSpec) []ResolvedPredicate {
	if len(specs) == 0 {
		return nil
	}
	out := make([]ResolvedPredicate, 0, len(specs))
	for _, s := range specs {
		out = append(out, ResolvedPredicate{Signal: s.Signal, Args: s.Args})
	}
	return out
}
