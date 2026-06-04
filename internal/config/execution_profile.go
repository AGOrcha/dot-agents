package config

// ExecutionProfile is the config-v2 §15-shaped, scope-mergeable layer that
// routes a task's workflow execution shape by app_type. It bundles three
// facets per app_type — unit relevance (the noise filter), topology (the
// executor:verifier:reviewer fan-out), and lenses (the review-lens config) —
// so the routing logic today scattered across app_type_verifier_map,
// lens_routing, and max_parallel_tasks resolves through one mergeable surface.
//
// It is purely additive and forward-compatible with the §15 units model: an
// execution_profile entry is a kind=layer config layer that merges by scope
// precedence (org → team → repo → project-local). No CLI consumes it yet
// (that is t2); this slice defines the types, the AgentsRC wiring, and the
// relevance-class helper.
//
// See design .agents/proposals/skill-relevance-filter.md §2 (facets) + §4
// (config-v2 §15 mapping).
type ExecutionProfile struct {
	// ByAppType maps an app_type string (e.g. "go-cli", "ideation") to its
	// resolved execution shape. Each task in a plan picks its profile by
	// naming an app_type; unmatched app_types fall back to caller defaults.
	ByAppType map[string]AppTypeProfile `json:"by_app_type,omitempty"`
	// DefaultClass is the relevance class assigned to any unit not explicitly
	// listed in a stage's RelevanceClasses. It defaults to "situational" so a
	// unit is never silently dropped from the working set — see ClassOf and
	// DefaultRelevanceClass.
	DefaultClass string `json:"default_class,omitempty"`
}

// AppTypeProfile is the per-app_type execution shape: the three independently
// scope-overridable facets bundled into one routed layer entry.
type AppTypeProfile struct {
	// Relevance is facet 1 (the noise filter): per-stage core/situational/noise
	// classification of skills/agents/lenses. The map key is the stage name
	// (e.g. "orchestrate", "verify", "review").
	Relevance map[string]RelevanceClasses `json:"relevance,omitempty"`
	// Topology is facet 2: the executor:verifier:reviewer fan-out shape. It
	// supersedes the flat app_type_verifier_map (verifier_sequence moves here).
	Topology Topology `json:"topology,omitempty"`
	// Lenses is facet 3: the review-lens config folded in from
	// lens-evidence-policy (lens_set + lens_concurrency).
	Lenses Lenses `json:"lenses,omitempty"`
}

// RelevanceClasses partitions the units relevant to one app_type × stage into
// three reversible-view classes. Anything not listed defaults to the profile's
// DefaultClass (situational), so nothing unlisted is silently dropped.
type RelevanceClasses struct {
	// Core units are the always-relevant working set for this stage.
	Core []string `json:"core,omitempty"`
	// Situational units are conditionally useful; also the default for any
	// unlisted unit (DefaultRelevanceClass).
	Situational []string `json:"situational,omitempty"`
	// Noise units are suppressed from the working set for this stage. The
	// suppression is a reversible view, not a delete.
	Noise []string `json:"noise,omitempty"`
}

// Topology encodes the executor:verifier:reviewer fan-out a task runs under.
// It is what app_type "structuralizes": n executors → VerifiersPerExecutor·n
// verifiers, with Reviewers per verifier/executor or a fixed count.
type Topology struct {
	// Executors is the number of parallel executor workers for the task.
	Executors int `json:"executors,omitempty"`
	// VerifiersPerExecutor is the verifier fan-out per executor (n exec →
	// VerifiersPerExecutor·n verifiers).
	VerifiersPerExecutor int `json:"verifiers_per_executor,omitempty"`
	// Reviewers is the reviewer fan-out: a keyword ("per_verifier",
	// "per_executor") or a stringified integer count. Kept as a string so the
	// keyword and fixed-count forms share one field.
	Reviewers string `json:"reviewers,omitempty"`
	// VerifierSequence is the ordered verifier-profile ids for this app_type,
	// superseding the flat app_type_verifier_map.
	VerifierSequence []string `json:"verifier_sequence,omitempty"`
}

// Lenses is the review-lens facet folded in from lens-evidence-policy.
type Lenses struct {
	// LensSet is the ordered review lenses applied (e.g.
	// "architecture-standards", "acceptance-invariants", "adversarial").
	LensSet []string `json:"lens_set,omitempty"`
	// LensConcurrency is how the lens set runs: "parallel", "gated", or
	// "tiered".
	LensConcurrency string `json:"lens_concurrency,omitempty"`
}

// DefaultRelevanceClass is the class assigned to any unit not explicitly listed
// in a stage's RelevanceClasses. Per the design (§2), an unlisted unit is
// "situational" — never silently dropped from the working set.
const DefaultRelevanceClass = "situational"

// EffectiveDefaultClass returns the profile's configured DefaultClass, falling
// back to DefaultRelevanceClass ("situational") when it is unset. A nil profile
// also resolves to the safe default so callers never have to nil-check before
// classifying.
func (p *ExecutionProfile) EffectiveDefaultClass() string {
	if p == nil || p.DefaultClass == "" {
		return DefaultRelevanceClass
	}
	return p.DefaultClass
}

// ClassOf returns the relevance class ("core" | "situational" | "noise") of
// unit within the given app_type × stage. Any unit not explicitly listed
// resolves to the profile's effective default class (situational), so an
// unlisted unit is classed — never silently dropped.
//
// Resolution is explicit-list-first: an explicit listing in any class wins over
// the default. If a unit is (mis)listed in more than one class, the most
// conservative wins — noise over situational over core — so an operator
// suppressing a unit in one place is never overridden by a stale core listing.
func (p *ExecutionProfile) ClassOf(appType, stage, unit string) string {
	def := p.EffectiveDefaultClass()
	if p == nil || p.ByAppType == nil {
		return def
	}
	prof, ok := p.ByAppType[appType]
	if !ok || prof.Relevance == nil {
		return def
	}
	classes, ok := prof.Relevance[stage]
	if !ok {
		return def
	}
	if contains(classes.Noise, unit) {
		return "noise"
	}
	if contains(classes.Situational, unit) {
		return "situational"
	}
	if contains(classes.Core, unit) {
		return "core"
	}
	return def
}

// WorkingSet is the reversible result of suppressing noise-classed units from a
// candidate set for one app_type × stage. Kept holds the effective working set
// (core + situational, in input order); Suppressed holds the noise-classed units
// (also in input order). The two together reconstruct the original candidate set
// — suppression is a view, never a delete — so Restore can losslessly rejoin them.
type WorkingSet struct {
	// Kept is the effective working set: every candidate whose relevance class is
	// core or situational (including the default for unlisted units), in the order
	// it appeared in the candidate slice.
	Kept []string `json:"kept,omitempty"`
	// Suppressed is the noise-classed candidates removed from the working set for
	// this stage, in input order. They are retained (not deleted) so the view is
	// reversible.
	Suppressed []string `json:"suppressed,omitempty"`
	// order records, per element, which class slice it came from so Restore can
	// re-interleave Kept and Suppressed back into the original candidate ordering.
	// true == kept, false == suppressed.
	order []bool
}

// SuppressNoise partitions candidates into a reversible WorkingSet for the given
// app_type × stage: units classed "noise" are moved to Suppressed and everything
// else (core, situational, and the default class for unlisted units) stays in
// Kept. Input order is preserved within each list and the candidate slice is
// never mutated, so this is a pure function safe to call on resolved working sets.
//
// Because classification routes through ClassOf, the default_class contract holds:
// an unlisted unit is situational (kept) by default and is never silently dropped.
// A profile with default_class=noise suppresses unlisted units instead — the same
// reversible view, just a different default. A nil profile keeps every candidate.
func (p *ExecutionProfile) SuppressNoise(appType, stage string, candidates []string) WorkingSet {
	ws := WorkingSet{order: make([]bool, 0, len(candidates))}
	for _, unit := range candidates {
		if p.ClassOf(appType, stage, unit) == "noise" {
			ws.Suppressed = append(ws.Suppressed, unit)
			ws.order = append(ws.order, false)
			continue
		}
		ws.Kept = append(ws.Kept, unit)
		ws.order = append(ws.order, true)
	}
	return ws
}

// Restore rejoins Kept and Suppressed back into the original candidate ordering,
// undoing the suppression view. A zero-value WorkingSet (no recorded order)
// restores Kept followed by Suppressed, which is also the original order when the
// view was produced by SuppressNoise on a fully-kept or fully-suppressed set.
func (ws WorkingSet) Restore() []string {
	if len(ws.order) == 0 {
		out := make([]string, 0, len(ws.Kept)+len(ws.Suppressed))
		out = append(out, ws.Kept...)
		out = append(out, ws.Suppressed...)
		return out
	}
	out := make([]string, 0, len(ws.order))
	ki, si := 0, 0
	for _, kept := range ws.order {
		if kept {
			out = append(out, ws.Kept[ki])
			ki++
			continue
		}
		out = append(out, ws.Suppressed[si])
		si++
	}
	return out
}

// contains reports whether s is present in list.
func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
