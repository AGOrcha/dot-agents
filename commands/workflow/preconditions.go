package workflow

import (
	"fmt"
	"strings"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/events"
)

// Configurable verifier precondition policy (design.md §2.3 amended).
//
// The in_progress → awaiting_agent_review gate is a *policy* of *predicates over
// the unified event contract*, not a hardcoded struct of VCS/quality-tool fields
// (the lpf-e first cut, superseded by fold-back
// verifier-preconditions-should-be-configurable). A predicate names a registered
// signal kind and the condition under which it is satisfied; predicates are
// looked up table-driven in a kind registry, so a new precondition kind is a
// *registration*, never a struct/switch edit (D2/D3).
//
// The built-in `default` policy reproduces the historical PR/go-cli gate exactly
// (event.pr.open ∧ signal.ci.rollup=green ∧ gate.quality.sonar=pass ∧
// metric.new_code_issues=0), so behavior is unchanged until a project declares a
// different policy. An empty policy evaluates the default — the gate is never
// open by omission (§2.3 no partial-green shortcut).

// Predicate is one condition over a single registered event/signal kind.
type Predicate struct {
	// Signal is the registered kind, e.g. "event.pr.open", "gate.quality.sonar".
	Signal string
	// Args are kind-specific parameters, e.g. {"equals":"green"}.
	Args map[string]string
}

// PreconditionPolicy is a named, unordered set of predicates. The gate opens only
// when every predicate is satisfied; there is no partial-green shortcut (§2.3).
type PreconditionPolicy struct {
	// Name is the policy's registry key (e.g. "default").
	Name string
	// Predicates is the set evaluated against a SignalSnapshot. Order does not
	// affect satisfaction, only which failing reason surfaces first.
	Predicates []Predicate
}

// SignalSnapshot is the observed signal set the verifier evaluates a policy
// against: kind → observed value, e.g. "signal.ci.rollup":"green". The producer
// (lpf PR-producer / poll-detector) fills it; this package only consumes it.
type SignalSnapshot map[string]string

// PredicateEvaluator decides whether one predicate is satisfied against a
// snapshot. reason is the operator hint surfaced when !ok.
type PredicateEvaluator func(p Predicate, snap SignalSnapshot) (ok bool, reason string)

// Signal kind names. These are the predicate registry keys (the precondition
// signal kinds), distinct from the internal/events event-kind constants which
// name event *envelopes*. Where an event constant already carries a value a
// predicate compares against (e.g. the rollup green state), the predicate
// references that constant rather than re-spelling the literal.
const (
	signalPROpen        = "event.pr.open"
	signalCIRollup      = "signal.ci.rollup"
	signalGateQuality   = "gate.quality."
	signalNewCodeIssues = "metric.new_code_issues"
)

// predicateEvaluators is the kind → evaluator registry (D2/D3). A new
// precondition kind is added by registering an evaluator here, never by editing a
// switch. Keys may be exact kinds or trailing-dot prefixes (see evaluatorFor).
var predicateEvaluators = map[string]PredicateEvaluator{}

// registerPredicate installs an evaluator for a signal kind. A trailing-dot key
// (e.g. "gate.quality.") registers a prefix evaluator matched by evaluatorFor.
func registerPredicate(kind string, ev PredicateEvaluator) {
	predicateEvaluators[kind] = ev
}

// evaluatorFor resolves the evaluator for a signal: exact match first, then the
// longest registered prefix (so "gate.quality.sonar" resolves the
// "gate.quality." evaluator). Returns ok=false when no kind is registered, which
// the policy evaluator treats as a fail-closed unregistered kind.
func evaluatorFor(signal string) (PredicateEvaluator, bool) {
	if ev, ok := predicateEvaluators[signal]; ok {
		return ev, true
	}
	bestLen := -1
	var best PredicateEvaluator
	for kind, ev := range predicateEvaluators {
		if !strings.HasSuffix(kind, ".") {
			continue
		}
		if strings.HasPrefix(signal, kind) && len(kind) > bestLen {
			bestLen = len(kind)
			best = ev
		}
	}
	if bestLen >= 0 {
		return best, true
	}
	return nil, false
}

// blankUnknown renders a value placeholder for reason strings when the observed
// value is empty, so an absent signal reads "unknown" rather than "".
func blankUnknown(v string) string {
	if strings.TrimSpace(v) == "" {
		return "unknown"
	}
	return v
}

// init registers the four built-in evaluators that compose the default policy.
func init() {
	// event.pr.open — satisfied when a PR is open for the task branch.
	registerPredicate(signalPROpen, func(p Predicate, snap SignalSnapshot) (bool, string) {
		if snap[signalPROpen] == "true" {
			return true, ""
		}
		return false, "PR is not open (branch pushed + PR opened required, §2.3)"
	})

	// signal.ci.rollup — satisfied when the rollup equals the expected value
	// (default events.RollupGreen, overridable via Args["equals"]).
	registerPredicate(signalCIRollup, func(p Predicate, snap SignalSnapshot) (bool, string) {
		want := p.Args["equals"]
		if want == "" {
			want = events.RollupGreen
		}
		got := snap[signalCIRollup]
		if got == want {
			return true, ""
		}
		return false, fmt.Sprintf("primary verifier chain not terminal %s (rollup=%s, §2.3)", want, blankUnknown(got))
	})

	// gate.quality.<provider> (prefix) — satisfied when the named gate passed.
	// The provider is the suffix (sonar, codeql, …); no privileged field.
	registerPredicate(signalGateQuality, func(p Predicate, snap SignalSnapshot) (bool, string) {
		if snap[p.Signal] == "pass" {
			return true, ""
		}
		return false, fmt.Sprintf("quality gate %q not pass (got %s, §2.3)", p.Signal, blankUnknown(snap[p.Signal]))
	})

	// metric.new_code_issues — satisfied when the count equals the expected
	// value (default "0", overridable via Args["equals"]).
	registerPredicate(signalNewCodeIssues, func(p Predicate, snap SignalSnapshot) (bool, string) {
		want := p.Args["equals"]
		if want == "" {
			want = "0"
		}
		got := snap[signalNewCodeIssues]
		if got == want {
			return true, ""
		}
		return false, fmt.Sprintf("new-code issues=%s; §2.3 requires %s", blankUnknown(got), want)
	})
}

// defaultPreconditionPolicy is the built-in `default` policy: the historical
// PR/go-cli gate. It reproduces the four checks of the retired
// verifierPreconditions struct exactly, so behavior is unchanged when a profile
// names no policy.
var defaultPreconditionPolicy = PreconditionPolicy{
	Name: "default",
	Predicates: []Predicate{
		{Signal: signalPROpen},
		{Signal: signalCIRollup, Args: map[string]string{"equals": events.RollupGreen}},
		{Signal: signalGateQuality + "sonar"},
		{Signal: signalNewCodeIssues, Args: map[string]string{"equals": "0"}},
	},
}

// evaluatePolicy reports whether every predicate in the policy is satisfied
// against the snapshot, returning the first failing predicate's reason. An empty
// policy evaluates defaultPreconditionPolicy instead — the gate is never open by
// omission (§2.3). An unregistered signal kind is fail-closed unsatisfied with a
// clear operator reason; it never silently passes.
func evaluatePolicy(policy PreconditionPolicy, snap SignalSnapshot) (ok bool, reason string) {
	if len(policy.Predicates) == 0 {
		policy = defaultPreconditionPolicy
	}
	for _, p := range policy.Predicates {
		ev, found := evaluatorFor(p.Signal)
		if !found {
			return false, fmt.Sprintf("unregistered precondition signal kind %q", p.Signal)
		}
		if ok, reason := ev(p, snap); !ok {
			return false, reason
		}
	}
	return true, ""
}

// ── Lockfile policy resolution (Slice B3/B4) ───────────────────────────────────

// resolvePreconditionPolicy is the thin accessor the verifier dispatch call site
// uses to obtain the resolved §2.3 policy for a task's app_type. It reads the
// LOCKED effective config (internal/config.ResolvePreconditionPolicy →
// ResolveLocked, never raw .agentsrc.json) and converts the config-side
// ResolvedPreconditionPolicy into this package's PreconditionPolicy.
//
// The conversion crosses the package boundary deliberately: internal/config must
// not import commands/workflow (which would be an import cycle), so config returns
// its own mirror shape and the call site — here — converts it. An unset policy or
// an absent registry entry resolves to the built-in default on the config side,
// which arrives as an empty-predicate policy; evaluatePolicy then applies
// defaultPreconditionPolicy, so the gate is never open by omission (§2.3).
//
// resolvePreconditionPolicy is overridable as a package var so the future
// dispatch call site (and its tests) can inject a fixed policy without touching
// the on-disk lock/cache.
var resolvePreconditionPolicy = func(projectPath, appType string) (PreconditionPolicy, error) {
	resolved, err := config.ResolvePreconditionPolicy(projectPath, appType)
	if err != nil {
		return PreconditionPolicy{}, err
	}
	return preconditionPolicyFromConfig(resolved), nil
}

// preconditionPolicyFromConfig converts the config-side ResolvedPreconditionPolicy
// into this package's PreconditionPolicy (field-for-field). It is the sole
// boundary translation that lets internal/config stay free of a commands/workflow
// import. A policy with no predicates is preserved as-is (empty Predicates) so
// evaluatePolicy applies the built-in default.
func preconditionPolicyFromConfig(rp config.ResolvedPreconditionPolicy) PreconditionPolicy {
	out := PreconditionPolicy{Name: rp.Name}
	if len(rp.Predicates) == 0 {
		return out
	}
	out.Predicates = make([]Predicate, 0, len(rp.Predicates))
	for _, p := range rp.Predicates {
		out.Predicates = append(out.Predicates, Predicate{Signal: p.Signal, Args: p.Args})
	}
	return out
}
