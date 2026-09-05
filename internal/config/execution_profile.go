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
	// Topology is facet 2: the executor:verifier:reviewer fan-out shape. It is the
	// successor to the retired flat app_type_verifier_map — verifier_sequence lives
	// here, and legacy app_type_verifier_map entries are folded in on load.
	Topology Topology `json:"topology,omitempty"`
	// Lenses is facet 3: the review-lens config folded in from
	// lens-evidence-policy (lens_set + lens_concurrency).
	Lenses Lenses `json:"lenses,omitempty"`
	// GraphBackend is facet 4: the graph-backend adapter-ref the profile
	// selects (app-type-profiles §2.6 / §3.1). It is an open adapter-ref of the
	// form `dotagents-builtin:graph/<name>@<constraint>` (or a bare `<name>`),
	// resolved against the graph-backend adapter registry
	// (graph-backend-adapter-contract §4/§8) — not a closed enum. An empty value
	// means the profile inherits the pipeline's default backend (crg). The
	// resolver (commands/config relevance --filter graph) turns this ref into a
	// registered adapter and surfaces whether it resolves.
	GraphBackend string `json:"graph_backend,omitempty"`
	// Model is facet 5: the model route this app_type defaults to
	// (.agents/proposals/model-facet-apptypeprofile.md; the flat per-app_type pin
	// of task-tier-model-suggestion D4). Like GraphBackend it is an OPEN
	// adapter-ref-style string — a bare tier alias ("haiku", "sonnet", "opus") or
	// a provider-qualified ref ("anthropic:claude-opus-4-8") — never a closed
	// enum, so model churn is not a schema bump (task-tier-model-suggestion D5).
	//
	// Empty means INHERIT, and absence must round-trip as absence: an explicit
	// stage_profiles[<stage>][<slug>].model always wins over this app_type
	// default, and an omitted value at one scope inherits the merged value from
	// the layers below (org → team → repo → project-local) rather than blanking
	// it. A still-empty value is "no opinion" — the harness default applies; no
	// model is ever fabricated.
	Model string `json:"model,omitempty"`
}

// GraphBackendRef returns the profile's declared graph-backend adapter ref, or
// the empty string when the profile selects none (and inherits the pipeline
// default). Centralising the read keeps callers from poking the field directly,
// so a later default-backend policy has one place to land.
func (p AppTypeProfile) GraphBackendRef() string {
	return p.GraphBackend
}

// ModelRef returns the profile's declared model route, or the empty string when
// the profile pins none (and therefore inherits). Centralising the read — as
// GraphBackendRef does for facet 4 — keeps callers from poking the field
// directly, so a later default-model policy has exactly one place to land.
func (p AppTypeProfile) ModelRef() string {
	return p.Model
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
	// VerifierSequence is the ordered verifier-profile ids for this app_type; each
	// id references a stage_profiles.verifier slug. It is the successor to the
	// retired flat app_type_verifier_map (legacy entries fold in on load).
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

// ClassifiedUnit is one candidate paired with the relevance class it resolved to
// for the active app_type × stage. It is the unit of a WorkingSet: the ordered
// list of these entries is the single source of truth from which both the kept
// working set and the suppressed/original candidate sets are derived, so the view
// carries no parallel bookkeeping that could drift or be lost on serialization.
type ClassifiedUnit struct {
	// Unit is the candidate skill/agent/lens id.
	Unit string `json:"unit"`
	// Class is the resolved relevance class: "core", "situational", or "noise".
	Class string `json:"class"`
}

// suppressed reports whether this entry was filtered out of the working set
// (i.e. classed "noise"). It is the single predicate that defines suppression,
// so Kept/Suppressed/Candidates all agree by construction.
func (c ClassifiedUnit) suppressed() bool { return c.Class == "noise" }

// WorkingSet is the reversible result of suppressing noise-classed units from a
// candidate set for one app_type × stage. Units is the ordered, fully-classified
// candidate list — the authoritative view. Because every candidate is retained
// here with its class, suppression is a non-destructive view (never a delete):
// Candidates losslessly reconstructs the original input, Kept yields the effective
// working set (core + situational), and Suppressed yields the noise-classed units.
//
// Units carries a JSON tag and no unexported state, so the view survives a JSON
// round-trip intact — the t2/t4 resolvers render it as --json without losing the
// per-unit class or the ability to reverse the suppression.
type WorkingSet struct {
	// Units is every candidate in input order, each tagged with its resolved
	// relevance class. This is the load-bearing field; the helper methods are
	// pure projections of it.
	Units []ClassifiedUnit `json:"units,omitempty"`
}

// SuppressNoise classifies candidates for the given app_type × stage and returns a
// reversible WorkingSet: units classed "noise" are marked suppressed while
// everything else (core, situational, and the default class for unlisted units)
// stays in the effective working set. Input order is preserved and the candidate
// slice is never mutated, so this is a pure function safe to call on resolved
// working sets.
//
// Because classification routes through ClassOf, the default_class contract holds:
// an unlisted unit is situational (kept) by default and is never silently dropped.
// A profile with default_class=noise suppresses unlisted units instead — the same
// reversible view, just a different default. A nil profile keeps every candidate.
func (p *ExecutionProfile) SuppressNoise(appType, stage string, candidates []string) WorkingSet {
	if len(candidates) == 0 {
		return WorkingSet{}
	}
	units := make([]ClassifiedUnit, 0, len(candidates))
	for _, unit := range candidates {
		units = append(units, ClassifiedUnit{Unit: unit, Class: p.ClassOf(appType, stage, unit)})
	}
	return WorkingSet{Units: units}
}

// Kept returns the effective working set in input order: every candidate whose
// class is not "noise" (core, situational, and the default for unlisted units).
// Returns nil when nothing is kept so an empty view marshals cleanly.
func (ws WorkingSet) Kept() []string {
	var out []string
	for _, u := range ws.Units {
		if !u.suppressed() {
			out = append(out, u.Unit)
		}
	}
	return out
}

// Suppressed returns the noise-classed candidates removed from the working set,
// in input order. They are retained in Units (not deleted), so the suppression is
// a reversible view. Returns nil when nothing is suppressed.
func (ws WorkingSet) Suppressed() []string {
	var out []string
	for _, u := range ws.Units {
		if u.suppressed() {
			out = append(out, u.Unit)
		}
	}
	return out
}

// Candidates reconstructs the original candidate set in input order, undoing the
// suppression view losslessly. Because Units retains every candidate with its
// class, this round-trips the input regardless of how units interleave and
// survives JSON serialization — the real "reversible view, no delete" guarantee.
func (ws WorkingSet) Candidates() []string {
	var out []string
	for _, u := range ws.Units {
		out = append(out, u.Unit)
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
