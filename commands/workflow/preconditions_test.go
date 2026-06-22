package workflow

import (
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/events"
)

// greenAndCleanSnapshot is the snapshot under which the built-in default policy
// is fully satisfied — the §2.3 all-green PR/go-cli state. Each test mutates a
// copy of it to exercise one failing predicate.
func greenAndCleanSnapshot() SignalSnapshot {
	return SignalSnapshot{
		"event.pr.open":          "true",
		"signal.ci.rollup":       events.RollupGreen,
		"gate.quality.sonar":     "pass",
		"metric.new_code_issues": "0",
	}
}

// snapWith returns a copy of base with key set to value.
func snapWith(base SignalSnapshot, key, value string) SignalSnapshot {
	out := make(SignalSnapshot, len(base))
	for k, v := range base {
		out[k] = v
	}
	out[key] = value
	return out
}

// snapWithout returns a copy of base with key removed (the signal absent).
func snapWithout(base SignalSnapshot, key string) SignalSnapshot {
	out := make(SignalSnapshot, len(base))
	for k, v := range base {
		if k != key {
			out[k] = v
		}
	}
	return out
}

// ── per-built-in predicate: satisfied + unsatisfied-with-reason ────────────────

type predicateCase struct {
	name    string
	pred    Predicate
	snap    SignalSnapshot
	wantOK  bool
	wantSub string // substring expected in reason when !ok
}

func runPredicateCase(t *testing.T, tc predicateCase) {
	t.Helper()
	ev, found := evaluatorFor(tc.pred.Signal)
	if !found {
		t.Fatalf("no evaluator registered for %q", tc.pred.Signal)
	}
	ok, reason := ev(tc.pred, tc.snap)
	if ok != tc.wantOK {
		t.Fatalf("ok = %v, want %v (reason=%q)", ok, tc.wantOK, reason)
	}
	if tc.wantOK {
		return
	}
	if reason == "" {
		t.Fatalf("expected a non-empty reason when unsatisfied")
	}
	if tc.wantSub != "" && !strings.Contains(reason, tc.wantSub) {
		t.Fatalf("reason %q does not contain %q", reason, tc.wantSub)
	}
}

func TestBuiltinPredicates(t *testing.T) {
	tests := []predicateCase{
		{
			name:   "event.pr.open satisfied",
			pred:   Predicate{Signal: "event.pr.open"},
			snap:   SignalSnapshot{"event.pr.open": "true"},
			wantOK: true,
		},
		{
			name:    "event.pr.open unsatisfied (absent)",
			pred:    Predicate{Signal: "event.pr.open"},
			snap:    SignalSnapshot{},
			wantSub: "PR is not open",
		},
		{
			name:   "signal.ci.rollup satisfied (default green)",
			pred:   Predicate{Signal: "signal.ci.rollup"},
			snap:   SignalSnapshot{"signal.ci.rollup": events.RollupGreen},
			wantOK: true,
		},
		{
			name:    "signal.ci.rollup unsatisfied (failing)",
			pred:    Predicate{Signal: "signal.ci.rollup"},
			snap:    SignalSnapshot{"signal.ci.rollup": events.RollupFailing},
			wantSub: "rollup=",
		},
		{
			name:   "signal.ci.rollup satisfied with explicit equals arg",
			pred:   Predicate{Signal: "signal.ci.rollup", Args: map[string]string{"equals": "AMBER"}},
			snap:   SignalSnapshot{"signal.ci.rollup": "AMBER"},
			wantOK: true,
		},
		{
			name:   "gate.quality.sonar satisfied (prefix evaluator)",
			pred:   Predicate{Signal: "gate.quality.sonar"},
			snap:   SignalSnapshot{"gate.quality.sonar": "pass"},
			wantOK: true,
		},
		{
			name:   "gate.quality.codeql satisfied (provider is just the suffix)",
			pred:   Predicate{Signal: "gate.quality.codeql"},
			snap:   SignalSnapshot{"gate.quality.codeql": "pass"},
			wantOK: true,
		},
		{
			name:    "gate.quality.sonar unsatisfied (not pass)",
			pred:    Predicate{Signal: "gate.quality.sonar"},
			snap:    SignalSnapshot{"gate.quality.sonar": "fail"},
			wantSub: "quality gate",
		},
		{
			name:   "metric.new_code_issues satisfied (zero)",
			pred:   Predicate{Signal: "metric.new_code_issues"},
			snap:   SignalSnapshot{"metric.new_code_issues": "0"},
			wantOK: true,
		},
		{
			name:    "metric.new_code_issues unsatisfied (nonzero)",
			pred:    Predicate{Signal: "metric.new_code_issues"},
			snap:    SignalSnapshot{"metric.new_code_issues": "4"},
			wantSub: "new-code issues",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runPredicateCase(t, tc)
		})
	}
}

// ── evaluatorFor resolution: exact, longest-prefix, unregistered ───────────────

func TestEvaluatorFor(t *testing.T) {
	tests := []struct {
		name    string
		signal  string
		wantHit bool
	}{
		{"exact match", "event.pr.open", true},
		{"prefix match resolves provider suffix", "gate.quality.sonar", true},
		{"prefix match for a different provider", "gate.quality.semgrep", true},
		{"unregistered kind returns no evaluator", "event.doc.section_approved", false},
		{"partial that is not a registered prefix", "gate.quality", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, ok := evaluatorFor(tc.signal)
			if ok != tc.wantHit {
				t.Fatalf("evaluatorFor(%q) found = %v, want %v", tc.signal, ok, tc.wantHit)
			}
		})
	}
}

// ── evaluatePolicy: unknown-kind fail-closed, empty→default, default met/unmet ─

func TestEvaluatePolicyUnknownKindFailsClosed(t *testing.T) {
	policy := PreconditionPolicy{
		Name:       "bad",
		Predicates: []Predicate{{Signal: "event.dataset.partition_ready"}},
	}
	ok, reason := evaluatePolicy(policy, SignalSnapshot{})
	if ok {
		t.Fatal("unknown kind must fail-closed, got ok=true")
	}
	if !strings.Contains(reason, "unregistered precondition signal kind") {
		t.Fatalf("reason %q does not name the unregistered kind", reason)
	}
}

func TestEvaluatePolicyEmptyFallsBackToDefault(t *testing.T) {
	empty := PreconditionPolicy{Name: "empty"}

	// Default snapshot satisfies the fallback default policy.
	if ok, reason := evaluatePolicy(empty, greenAndCleanSnapshot()); !ok {
		t.Fatalf("empty policy on green snapshot should pass via default; reason=%q", reason)
	}
	// An empty policy must NEVER be an open gate: a failing snapshot fails.
	if ok, _ := evaluatePolicy(empty, SignalSnapshot{}); ok {
		t.Fatal("empty policy on empty snapshot must fail (default gate, never open)")
	}
}

func TestEvaluatePolicyDefaultMetAndUnmet(t *testing.T) {
	if ok, reason := evaluatePolicy(defaultPreconditionPolicy, greenAndCleanSnapshot()); !ok {
		t.Fatalf("default policy on green snapshot should pass; reason=%q", reason)
	}

	// Each removed/altered signal in turn must fail the default policy with a
	// non-empty reason (the first failing predicate's hint).
	mutations := []SignalSnapshot{
		snapWithout(greenAndCleanSnapshot(), "event.pr.open"),
		snapWith(greenAndCleanSnapshot(), "signal.ci.rollup", events.RollupFailing),
		snapWithout(greenAndCleanSnapshot(), "gate.quality.sonar"),
		snapWith(greenAndCleanSnapshot(), "metric.new_code_issues", "2"),
	}
	for i, snap := range mutations {
		ok, reason := evaluatePolicy(defaultPreconditionPolicy, snap)
		if ok {
			t.Fatalf("mutation %d should fail the default policy", i)
		}
		if reason == "" {
			t.Fatalf("mutation %d failed without a reason", i)
		}
	}
}

// TestDefaultPolicyShape pins the built-in default to the historical four-check
// PR/go-cli gate so the "behavior unchanged" guarantee is enforced.
func TestDefaultPolicyShape(t *testing.T) {
	want := []string{
		"event.pr.open",
		"signal.ci.rollup",
		"gate.quality.sonar",
		"metric.new_code_issues",
	}
	if len(defaultPreconditionPolicy.Predicates) != len(want) {
		t.Fatalf("default policy has %d predicates, want %d", len(defaultPreconditionPolicy.Predicates), len(want))
	}
	for i, sig := range want {
		if got := defaultPreconditionPolicy.Predicates[i].Signal; got != sig {
			t.Fatalf("default predicate[%d] = %q, want %q", i, got, sig)
		}
	}
}
