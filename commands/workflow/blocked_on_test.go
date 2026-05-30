package workflow

import (
	"errors"
	"testing"
	"time"
)

// fakeBlockerEnv is the test double for the BlockerEnv seam so predicate
// evaluation never touches the network or filesystem.
type fakeBlockerEnv struct {
	secrets        map[string]bool
	secretErr      error
	taskStatuses   map[string]string // key: "<plan>/<task>"
	decisions      map[string]bool
	decisionErr    error
	conditions     map[string]bool
	conditionErr   error
	conditionKnown map[string]bool
}

func (f fakeBlockerEnv) SecretExists(name string) (bool, error) {
	if f.secretErr != nil {
		return false, f.secretErr
	}
	return f.secrets[name], nil
}

func (f fakeBlockerEnv) TaskStatus(plan, task string) (string, bool) {
	s, ok := f.taskStatuses[plan+"/"+task]
	return s, ok
}

func (f fakeBlockerEnv) DecisionResolved(id string) (bool, error) {
	if f.decisionErr != nil {
		return false, f.decisionErr
	}
	return f.decisions[id], nil
}

func (f fakeBlockerEnv) EvalCondition(predicate string) (bool, error) {
	if f.conditionKnown != nil && !f.conditionKnown[predicate] {
		return false, errors.New("unregistered condition predicate: " + predicate)
	}
	if f.conditionErr != nil {
		return false, f.conditionErr
	}
	return f.conditions[predicate], nil
}

func TestIsBlockedOnStatus(t *testing.T) {
	cases := []struct {
		status string
		want   bool
	}{
		{"blocked-on:secret:FOO", true},
		{"blocked-on:", true}, // prefix matches even if ref empty; parse rejects later
		{"blocked", false},
		{"in_progress", false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsBlockedOnStatus(c.status); got != c.want {
			t.Errorf("IsBlockedOnStatus(%q) = %v, want %v", c.status, got, c.want)
		}
	}
}

func TestParseBlockedOnStatusRoundTrip(t *testing.T) {
	cases := []struct {
		status   string
		wantKind string
		wantArg  string
	}{
		{"blocked-on:task:plan1/task2", blockerKindTask, "plan1/task2"},
		{"blocked-on:secret:APPLE_CERT", blockerKindSecret, "APPLE_CERT"},
		{"blocked-on:decision:module-path-cut", blockerKindDecision, "module-path-cut"},
		{"blocked-on:condition:gh-checks(pr149)=green", blockerKindCondition, "gh-checks(pr149)=green"},
	}
	for _, c := range cases {
		ref, err := ParseBlockedOnStatus(c.status)
		if err != nil {
			t.Fatalf("ParseBlockedOnStatus(%q) unexpected err: %v", c.status, err)
		}
		if ref.Kind != c.wantKind || ref.Arg != c.wantArg {
			t.Errorf("ParseBlockedOnStatus(%q) = %+v, want kind=%q arg=%q", c.status, ref, c.wantKind, c.wantArg)
		}
		if got := formatBlockedOnStatus(ref); got != c.status {
			t.Errorf("round-trip formatBlockedOnStatus = %q, want %q", got, c.status)
		}
		if got := ref.String(); got != c.wantKind+":"+c.wantArg {
			t.Errorf("ref.String() = %q, want %q", got, c.wantKind+":"+c.wantArg)
		}
	}
}

func TestParseBlockedOnStatusErrors(t *testing.T) {
	cases := []string{
		"in_progress",               // missing prefix
		"blocked-on:",               // empty ref
		"blocked-on:secret",         // missing kind/arg separator
		"blocked-on:secret:",        // empty arg
		"blocked-on:bogus:whatever", // unknown kind
	}
	for _, status := range cases {
		if _, err := ParseBlockedOnStatus(status); err == nil {
			t.Errorf("ParseBlockedOnStatus(%q) expected error, got nil", status)
		}
	}
}

func TestEvaluateBlockerTaskKind(t *testing.T) {
	env := fakeBlockerEnv{taskStatuses: map[string]string{
		"plan1/done":    TaskStatusCompleted,
		"plan1/running": TaskStatusInProgress,
	}}

	// Positive: named task completed → predicate true.
	got, err := EvaluateBlocker(env, "blocked-on:task:plan1/done")
	if err != nil || !got {
		t.Fatalf("completed dep: got (%v,%v), want (true,nil)", got, err)
	}

	// Negative: named task not yet completed → false, no error.
	got, err = EvaluateBlocker(env, "blocked-on:task:plan1/running")
	if err != nil || got {
		t.Fatalf("running dep: got (%v,%v), want (false,nil)", got, err)
	}

	// Negative: unknown task → false, no error (keep waiting).
	got, err = EvaluateBlocker(env, "blocked-on:task:plan1/ghost")
	if err != nil || got {
		t.Fatalf("unknown dep: got (%v,%v), want (false,nil)", got, err)
	}

	// Error: malformed task arg (no plan/task split).
	if _, err = EvaluateBlocker(env, "blocked-on:task:nosplit"); err == nil {
		t.Error("malformed task arg: expected error, got nil")
	}
}

func TestEvaluateBlockerSecretKind(t *testing.T) {
	env := fakeBlockerEnv{secrets: map[string]bool{"APPLE_CERT": true}}

	got, err := EvaluateBlocker(env, "blocked-on:secret:APPLE_CERT")
	if err != nil || !got {
		t.Fatalf("present secret: got (%v,%v), want (true,nil)", got, err)
	}
	got, err = EvaluateBlocker(env, "blocked-on:secret:MISSING")
	if err != nil || got {
		t.Fatalf("missing secret: got (%v,%v), want (false,nil)", got, err)
	}

	// Error: env (gh) failure propagates.
	failEnv := fakeBlockerEnv{secretErr: errors.New("gh boom")}
	if _, err = EvaluateBlocker(failEnv, "blocked-on:secret:X"); err == nil {
		t.Error("secret env error: expected error, got nil")
	}
}

func TestEvaluateBlockerDecisionKind(t *testing.T) {
	env := fakeBlockerEnv{decisions: map[string]bool{"module-path-cut": true}}

	got, err := EvaluateBlocker(env, "blocked-on:decision:module-path-cut")
	if err != nil || !got {
		t.Fatalf("landed decision: got (%v,%v), want (true,nil)", got, err)
	}
	got, err = EvaluateBlocker(env, "blocked-on:decision:pending-one")
	if err != nil || got {
		t.Fatalf("pending decision: got (%v,%v), want (false,nil)", got, err)
	}

	failEnv := fakeBlockerEnv{decisionErr: errors.New("store boom")}
	if _, err = EvaluateBlocker(failEnv, "blocked-on:decision:x"); err == nil {
		t.Error("decision env error: expected error, got nil")
	}
}

func TestEvaluateBlockerConditionKind(t *testing.T) {
	env := fakeBlockerEnv{
		conditions:     map[string]bool{"gh-checks(pr1)=green": true},
		conditionKnown: map[string]bool{"gh-checks(pr1)=green": true, "ci(pr2)": true},
	}

	got, err := EvaluateBlocker(env, "blocked-on:condition:gh-checks(pr1)=green")
	if err != nil || !got {
		t.Fatalf("green condition: got (%v,%v), want (true,nil)", got, err)
	}
	got, err = EvaluateBlocker(env, "blocked-on:condition:ci(pr2)")
	if err != nil || got {
		t.Fatalf("false condition: got (%v,%v), want (false,nil)", got, err)
	}

	// Error: unregistered predicate.
	if _, err = EvaluateBlocker(env, "blocked-on:condition:unknown(pr3)"); err == nil {
		t.Error("unregistered condition: expected error, got nil")
	}
}

func TestEvaluateBlockerRejectsBadStatus(t *testing.T) {
	env := fakeBlockerEnv{}
	if _, err := EvaluateBlocker(env, "in_progress"); err == nil {
		t.Error("non-blocked status: expected error, got nil")
	}
}

func TestEvaluateBlockerNoPredicateForKind(t *testing.T) {
	// A kind marked valid for parsing but with no registered predicate must
	// surface an error rather than silently resuming.
	validBlockerKinds["orphan"] = true
	t.Cleanup(func() { delete(validBlockerKinds, "orphan") })

	env := fakeBlockerEnv{}
	if _, err := EvaluateBlocker(env, "blocked-on:orphan:x"); err == nil {
		t.Error("orphan kind: expected 'no predicate registered' error, got nil")
	}
}

func TestRegisterBlockerPredicate(t *testing.T) {
	const kind = "http"
	t.Cleanup(func() {
		delete(blockerPredicates, kind)
		delete(validBlockerKinds, kind)
	})

	called := false
	err := RegisterBlockerPredicate(kind, func(_ BlockerEnv, arg string) (bool, error) {
		called = true
		return arg == "ready", nil
	})
	if err != nil {
		t.Fatalf("RegisterBlockerPredicate err: %v", err)
	}

	// Parse now accepts the new kind, and the evaluator runs.
	got, err := EvaluateBlocker(fakeBlockerEnv{}, "blocked-on:http:ready")
	if err != nil || !got || !called {
		t.Fatalf("custom predicate: got (%v,%v) called=%v, want (true,nil) called=true", got, err, called)
	}

	// Negative: empty kind and nil predicate are rejected.
	if err := RegisterBlockerPredicate("", func(BlockerEnv, string) (bool, error) { return true, nil }); err == nil {
		t.Error("empty kind: expected error, got nil")
	}
	if err := RegisterBlockerPredicate("x", nil); err == nil {
		t.Error("nil predicate: expected error, got nil")
	}
}

func TestNormalizeResumeAs(t *testing.T) {
	cases := []struct {
		override string
		want     string
		wantErr  bool
	}{
		{"", TaskStatusInProgress, false}, // implicit default
		{TaskStatusInProgress, TaskStatusInProgress, false},
		{TaskStatusPending, TaskStatusPending, false},
		{TaskStatusCompleted, "", true}, // not a legal resume target
		{"garbage", "", true},
	}
	for _, c := range cases {
		got, err := NormalizeResumeAs(c.override)
		if c.wantErr {
			if err == nil {
				t.Errorf("NormalizeResumeAs(%q): expected error, got nil", c.override)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("NormalizeResumeAs(%q) = (%q,%v), want (%q,nil)", c.override, got, err, c.want)
		}
	}
}

func TestBlockerStaleThresholdDays(t *testing.T) {
	if got := blockerStaleThresholdDays(0); got != defaultBlockerStaleDays {
		t.Errorf("zero override: got %d, want default %d", got, defaultBlockerStaleDays)
	}
	if got := blockerStaleThresholdDays(-3); got != defaultBlockerStaleDays {
		t.Errorf("negative override: got %d, want default %d", got, defaultBlockerStaleDays)
	}
	if got := blockerStaleThresholdDays(3); got != 3 {
		t.Errorf("positive override: got %d, want 3", got)
	}
}

func TestEvaluateBlockerDecay(t *testing.T) {
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)

	// Fresh: blocked 2 days ago, default 7-day threshold → not stale.
	fresh := now.Add(-2 * 24 * time.Hour).Format(time.RFC3339)
	d, err := EvaluateBlockerDecay(fresh, 0, now)
	if err != nil || d.Stale {
		t.Fatalf("fresh: got (%+v,%v), want not stale", d, err)
	}
	if ann := blockerStaleAnnotation(d); ann != "" {
		t.Errorf("fresh annotation: got %q, want empty", ann)
	}

	// Stale: blocked 10 days ago, default 7-day threshold → stale.
	old := now.Add(-10 * 24 * time.Hour).Format(time.RFC3339)
	d, err = EvaluateBlockerDecay(old, 0, now)
	if err != nil || !d.Stale {
		t.Fatalf("stale: got (%+v,%v), want stale", d, err)
	}
	if d.Since != old {
		t.Errorf("stale Since = %q, want %q", d.Since, old)
	}
	if ann := blockerStaleAnnotation(d); ann != "blocker_stale_since="+old {
		t.Errorf("stale annotation = %q, want blocker_stale_since=%s", ann, old)
	}

	// Configurable: 1-day threshold makes the 2-day-old block stale.
	d, err = EvaluateBlockerDecay(fresh, 1, now)
	if err != nil || !d.Stale {
		t.Fatalf("configured threshold: got (%+v,%v), want stale", d, err)
	}

	// Empty timestamp → non-stale, no error (cannot prove staleness).
	d, err = EvaluateBlockerDecay("", 0, now)
	if err != nil || d.Stale {
		t.Fatalf("empty since: got (%+v,%v), want not stale", d, err)
	}

	// Negative: unparseable timestamp errors.
	if _, err = EvaluateBlockerDecay("not-a-time", 0, now); err == nil {
		t.Error("bad timestamp: expected error, got nil")
	}
}

func TestRegisteredBlockerKinds(t *testing.T) {
	kinds := registeredBlockerKinds()
	want := map[string]bool{
		blockerKindTask:      true,
		blockerKindSecret:    true,
		blockerKindDecision:  true,
		blockerKindCondition: true,
	}
	for _, k := range kinds {
		delete(want, k)
	}
	if len(want) != 0 {
		t.Errorf("registeredBlockerKinds missing kinds: %v (got %v)", want, kinds)
	}
	// Assert sorted order.
	for i := 1; i < len(kinds); i++ {
		if kinds[i-1] > kinds[i] {
			t.Errorf("registeredBlockerKinds not sorted: %v", kinds)
		}
	}
}
