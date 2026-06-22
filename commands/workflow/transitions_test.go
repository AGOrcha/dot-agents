package workflow

import (
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/events"
)

// ── §6.1 Verifier: in_progress → awaiting_agent_review ─────────────────────────

type verifierTransitionCase struct {
	name    string
	from    string
	snap    SignalSnapshot
	wantErr bool
	wantTo  string
}

// assertVerifierTransition runs one verifier case (default policy) and checks the
// outcome. It is extracted from the table loop so neither function carries the
// combined branching of error-path and happy-path assertions.
func assertVerifierTransition(t *testing.T, tc verifierTransitionCase) {
	t.Helper()
	dec, err := verifierTransition("t-verify", tc.from, defaultPreconditionPolicy, tc.snap)
	if tc.wantErr {
		if err == nil {
			t.Fatalf("expected error, got decision %+v", dec)
		}
		return
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dec.Owner != ownerVerifier {
		t.Fatalf("owner = %q, want %q", dec.Owner, ownerVerifier)
	}
	if dec.From != tc.from || dec.To != tc.wantTo {
		t.Fatalf("edge = %q→%q, want %q→%q", dec.From, dec.To, tc.from, tc.wantTo)
	}
}

func TestVerifierTransition(t *testing.T) {
	tests := []verifierTransitionCase{
		{
			name:   "all §2.3 preconditions met opens agent-review gate",
			from:   TaskStatusInProgress,
			snap:   greenAndCleanSnapshot(),
			wantTo: TaskStatusAwaitingAgentReview,
		},
		{
			name:    "PR not open is refused (no partial-green shortcut)",
			from:    TaskStatusInProgress,
			snap:    snapWithout(greenAndCleanSnapshot(), "event.pr.open"),
			wantErr: true,
		},
		{
			name:    "rollup not green is refused",
			from:    TaskStatusInProgress,
			snap:    snapWith(greenAndCleanSnapshot(), "signal.ci.rollup", events.RollupFailing),
			wantErr: true,
		},
		{
			name:    "sonar gate not pass is refused",
			from:    TaskStatusInProgress,
			snap:    snapWithout(greenAndCleanSnapshot(), "gate.quality.sonar"),
			wantErr: true,
		},
		{
			name:    "open new-code issues remaining is refused",
			from:    TaskStatusInProgress,
			snap:    snapWith(greenAndCleanSnapshot(), "metric.new_code_issues", "3"),
			wantErr: true,
		},
		{
			name:    "ownership guard: refuses a non-in_progress source (§6.1/DC#8)",
			from:    TaskStatusAwaitingAgentReview,
			snap:    greenAndCleanSnapshot(),
			wantErr: true,
		},
		{
			name:    "ownership guard: refuses an awaiting_owner_review source",
			from:    TaskStatusAwaitingOwnerReview,
			snap:    greenAndCleanSnapshot(),
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertVerifierTransition(t, tc)
		})
	}
}

// ── §6.2 Lens-gate: awaiting_agent_review → owner-review | in_progress ──────────

type lensGateTransitionCase struct {
	name     string
	from     string
	verdicts []lensVerdict
	wantErr  bool
	wantTo   string
}

// assertLensGateTransition runs one lens-gate case and checks the outcome,
// keeping the error-path and happy-path branching out of the table loop.
func assertLensGateTransition(t *testing.T, tc lensGateTransitionCase) {
	t.Helper()
	dec, err := lensGateTransition("t-lens", tc.from, tc.verdicts)
	if tc.wantErr {
		if err == nil {
			t.Fatalf("expected error, got decision %+v", dec)
		}
		return
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dec.Owner != ownerLensGate {
		t.Fatalf("owner = %q, want %q", dec.Owner, ownerLensGate)
	}
	if dec.To != tc.wantTo {
		t.Fatalf("to = %q, want %q", dec.To, tc.wantTo)
	}
}

func TestLensGateTransition(t *testing.T) {
	accept := func(names ...string) []lensVerdict {
		out := make([]lensVerdict, len(names))
		for i, n := range names {
			out[i] = lensVerdict{Lens: n, Accept: true}
		}
		return out
	}

	tests := []lensGateTransitionCase{
		{
			name:     "all lenses accept promotes to owner review",
			from:     TaskStatusAwaitingAgentReview,
			verdicts: accept("architecture-standards", "acceptance-invariants", "adversarial"),
			wantTo:   TaskStatusAwaitingOwnerReview,
		},
		{
			name: "any lens rejects bounces back to in_progress",
			from: TaskStatusAwaitingAgentReview,
			verdicts: []lensVerdict{
				{Lens: "architecture-standards", Accept: true},
				{Lens: "adversarial", Accept: false},
			},
			wantTo: TaskStatusInProgress,
		},
		{
			name:     "single accepting lens promotes",
			from:     TaskStatusAwaitingAgentReview,
			verdicts: accept("acceptance-invariants"),
			wantTo:   TaskStatusAwaitingOwnerReview,
		},
		{
			name:     "empty verdict set is refused (no silent promotion)",
			from:     TaskStatusAwaitingAgentReview,
			verdicts: nil,
			wantErr:  true,
		},
		{
			name:     "ownership guard: refuses an in_progress source (§6.2/DC#8)",
			from:     TaskStatusInProgress,
			verdicts: accept("architecture-standards"),
			wantErr:  true,
		},
		{
			name:     "ownership guard: refuses an awaiting_owner_review source",
			from:     TaskStatusAwaitingOwnerReview,
			verdicts: accept("architecture-standards"),
			wantErr:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertLensGateTransition(t, tc)
		})
	}
}

// ── §2.7 force-rebase SHA1 set diff ────────────────────────────────────────────

func TestDetectForceRebase(t *testing.T) {
	tests := []struct {
		name string
		prev []string
		cur  []string
		want bool
	}{
		{
			name: "dropped commit with replacement is a force-rebase",
			prev: []string{"aaa", "bbb"},
			cur:  []string{"ccc", "ddd"},
			want: true,
		},
		{
			name: "appended commits, none dropped, is an ordinary push",
			prev: []string{"aaa", "bbb"},
			cur:  []string{"aaa", "bbb", "ccc"},
			want: false,
		},
		{
			name: "identical set (reordered) is not a rebase",
			prev: []string{"aaa", "bbb"},
			cur:  []string{"bbb", "aaa"},
			want: false,
		},
		{
			name: "one commit dropped, rest kept, is a rebase",
			prev: []string{"aaa", "bbb", "ccc"},
			cur:  []string{"aaa", "ccc"},
			want: true,
		},
		{
			name: "empty current list cannot assert a replacement",
			prev: []string{"aaa"},
			cur:  nil,
			want: false,
		},
		{
			name: "first observation (no prev) is not a rebase",
			prev: nil,
			cur:  []string{"aaa", "bbb"},
			want: false,
		},
		{
			name: "whitespace-only entries are ignored",
			prev: []string{"aaa", "  "},
			cur:  []string{"aaa", ""},
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectForceRebase(tc.prev, tc.cur); got != tc.want {
				t.Fatalf("detectForceRebase(%v, %v) = %v, want %v", tc.prev, tc.cur, got, tc.want)
			}
		})
	}
}

// ── §3.2 single-envelope kind→edge mapping ─────────────────────────────────────

type pollEventTransitionCase struct {
	name        string
	from        string
	kind        string
	wantTrigger bool
	wantErr     bool
	wantTo      string
	wantRebase  bool
	wantCascade bool
}

// mkPollEnv builds a minimal PR envelope of the given kind for the single-edge
// mapping tests.
func mkPollEnv(t *testing.T, kind string) events.Envelope {
	t.Helper()
	env, err := events.NewEnvelope(kind, "github", "k-"+kind, time.Now(), []byte(`{"number":7}`))
	if err != nil {
		t.Fatal(err)
	}
	return env
}

// assertPollDecision checks the decision fields of a triggered poll transition.
func assertPollDecision(t *testing.T, tc pollEventTransitionCase, dec transitionDecision) {
	t.Helper()
	if dec.Owner != ownerPollDetector {
		t.Fatalf("owner = %q, want %q", dec.Owner, ownerPollDetector)
	}
	if dec.To != tc.wantTo {
		t.Fatalf("to = %q, want %q", dec.To, tc.wantTo)
	}
	if dec.RebaseOnly != tc.wantRebase {
		t.Fatalf("rebaseOnly = %v, want %v", dec.RebaseOnly, tc.wantRebase)
	}
	if dec.Cascade != tc.wantCascade {
		t.Fatalf("cascade = %v, want %v", dec.Cascade, tc.wantCascade)
	}
}

// assertPollEventTransition runs one single-envelope mapping case end to end.
func assertPollEventTransition(t *testing.T, tc pollEventTransitionCase) {
	t.Helper()
	dec, triggered, err := pollEventTransition("t-poll", tc.from, mkPollEnv(t, tc.kind))
	if tc.wantErr {
		if err == nil {
			t.Fatalf("expected error, got triggered=%v dec=%+v", triggered, dec)
		}
		return
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if triggered != tc.wantTrigger {
		t.Fatalf("triggered = %v, want %v", triggered, tc.wantTrigger)
	}
	if !tc.wantTrigger {
		return
	}
	assertPollDecision(t, tc, dec)
}

func TestPollEventTransition(t *testing.T) {
	tests := []pollEventTransitionCase{
		{
			name:        "merged → completed",
			from:        TaskStatusAwaitingOwnerReview,
			kind:        events.KindPRMerged,
			wantTrigger: true,
			wantTo:      TaskStatusCompleted,
		},
		{
			name:        "review_requested_change → in_progress",
			from:        TaskStatusAwaitingOwnerReview,
			kind:        events.KindPRReviewRequestedChange,
			wantTrigger: true,
			wantTo:      TaskStatusInProgress,
		},
		{
			name:        "closed → blocked with cascade flag (§2.6)",
			from:        TaskStatusAwaitingOwnerReview,
			kind:        events.KindPRClosed,
			wantTrigger: true,
			wantTo:      TaskStatusBlocked,
			wantCascade: true,
		},
		{
			name:        "force_rebased → in_progress with rebase-only flag (§7.3)",
			from:        TaskStatusAwaitingAgentReview,
			kind:        events.KindPRForceRebased,
			wantTrigger: true,
			wantTo:      TaskStatusInProgress,
			wantRebase:  true,
		},
		{
			name:        "opened is a recognized non-trigger kind (skipped)",
			from:        TaskStatusAwaitingOwnerReview,
			kind:        events.KindPROpened,
			wantTrigger: false,
		},
		{
			name:        "ci_green is a recognized non-trigger kind (skipped)",
			from:        TaskStatusAwaitingOwnerReview,
			kind:        events.KindPRCIGreen,
			wantTrigger: false,
		},
		{
			name:        "comment_posted is a recognized non-trigger kind (skipped)",
			from:        TaskStatusAwaitingOwnerReview,
			kind:        events.KindPRCommentPosted,
			wantTrigger: false,
		},
		{
			name:    "illegal edge (merged from terminal cancelled) is refused",
			from:    TaskStatusCancelled,
			kind:    events.KindPRMerged,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertPollEventTransition(t, tc)
		})
	}
}

// pollEventTransition must ignore non-PR namespaces entirely.
func TestPollEventTransitionIgnoresNonPRNamespace(t *testing.T) {
	env, err := events.NewEnvelope("event.metric.cpu", "src", "k", time.Now(), []byte(`{"number":7}`))
	if err != nil {
		t.Fatal(err)
	}
	dec, triggered, err := pollEventTransition("t", TaskStatusAwaitingOwnerReview, env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if triggered {
		t.Fatalf("non-PR namespace must not trigger, got %+v", dec)
	}
}

// An unrecognized event.pr.* kind must fail loud (control-plane reject-on-unknown).
func TestPollEventTransitionUnknownPRKindErrors(t *testing.T) {
	env := events.Envelope{
		Type:           "event.pr.teleported",
		Source:         "github",
		IdempotencyKey: "x",
		Payload:        []byte(`{"number":7}`),
	}
	if _, _, err := pollEventTransition("t", TaskStatusAwaitingOwnerReview, env); err == nil {
		t.Fatal("expected error for unrecognized control-plane PR kind")
	}
}

// ── §3.2 full poll-detector cycle ──────────────────────────────────────────────

// mkRunPollEnv builds a PR envelope with an embedded commit list for the
// full-cycle poll-detector tests.
func mkRunPollEnv(t *testing.T, kind string, number int, commits string) events.Envelope {
	t.Helper()
	payload := []byte(`{"number":` + itoa(number) + `,"commits":` + commits + `}`)
	e, err := events.NewEnvelope(kind, "github", kind+"-"+itoa(number), time.Now(), payload)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestRunPollDetector(t *testing.T) {
	t.Run("maps merged + closed across bound tasks, ordered by task id", testRunPollDetectorOrdered)
	t.Run("SHA1 set diff synthesizes a rebase-only regression overriding the kind", testRunPollDetectorRebase)
	t.Run("envelope for an unbound PR is ignored", testRunPollDetectorUnbound)
	t.Run("malformed payload fails loud", testRunPollDetectorMalformed)
	t.Run("non-PR namespace envelopes are skipped", testRunPollDetectorNonPR)
}

func testRunPollDetectorOrdered(t *testing.T) {
	bindings := []taskPRBinding{
		{TaskID: "t-merge", PRNumber: 10, Status: TaskStatusAwaitingOwnerReview, LastSHAs: []string{"a"}},
		{TaskID: "t-close", PRNumber: 20, Status: TaskStatusAwaitingOwnerReview, LastSHAs: []string{"b"}},
	}
	envs := []events.Envelope{
		mkRunPollEnv(t, events.KindPRClosed, 20, `["b"]`),
		mkRunPollEnv(t, events.KindPRMerged, 10, `["a"]`),
	}
	res, err := runPollDetector(envs, bindings)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Decisions) != 2 {
		t.Fatalf("decisions = %+v, want 2", res.Decisions)
	}
	// Deterministic task-id order: t-close before t-merge.
	if res.Decisions[0].TaskID != "t-close" || res.Decisions[0].To != TaskStatusBlocked || !res.Decisions[0].Cascade {
		t.Fatalf("decision[0] = %+v", res.Decisions[0])
	}
	if res.Decisions[1].TaskID != "t-merge" || res.Decisions[1].To != TaskStatusCompleted {
		t.Fatalf("decision[1] = %+v", res.Decisions[1])
	}
}

func testRunPollDetectorRebase(t *testing.T) {
	bindings := []taskPRBinding{
		{TaskID: "t-rb", PRNumber: 30, Status: TaskStatusAwaitingAgentReview, LastSHAs: []string{"old1", "old2"}},
	}
	// The producer only emitted a benign ci_green this cycle, but the commit
	// set was rewritten — the detector must synthesize the §2.7 rebase.
	envs := []events.Envelope{mkRunPollEnv(t, events.KindPRCIGreen, 30, `["new1","new2"]`)}
	res, err := runPollDetector(envs, bindings)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Decisions) != 1 {
		t.Fatalf("decisions = %+v, want 1", res.Decisions)
	}
	d := res.Decisions[0]
	if d.To != TaskStatusInProgress || !d.RebaseOnly || d.Owner != ownerPollDetector {
		t.Fatalf("rebase decision = %+v", d)
	}
	if got := res.ObservedSHAs["t-rb"]; len(got) != 2 || got[0] != "new1" || got[1] != "new2" {
		t.Fatalf("observed SHAs = %v", got)
	}
}

func testRunPollDetectorUnbound(t *testing.T) {
	bindings := []taskPRBinding{
		{TaskID: "t", PRNumber: 99, Status: TaskStatusAwaitingOwnerReview},
	}
	envs := []events.Envelope{mkRunPollEnv(t, events.KindPRMerged, 1234, `[]`)}
	res, err := runPollDetector(envs, bindings)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Decisions) != 0 {
		t.Fatalf("unbound PR must not transition, got %+v", res.Decisions)
	}
}

func testRunPollDetectorMalformed(t *testing.T) {
	bindings := []taskPRBinding{{TaskID: "t", PRNumber: 7, Status: TaskStatusAwaitingOwnerReview}}
	bad := events.Envelope{
		Type:           events.KindPRMerged,
		Source:         "github",
		IdempotencyKey: "bad",
		Payload:        []byte(`{"number":"not-a-number"}`),
	}
	if _, err := runPollDetector([]events.Envelope{bad}, bindings); err == nil {
		t.Fatal("expected decode error on malformed PR payload")
	}
}

func testRunPollDetectorNonPR(t *testing.T) {
	bindings := []taskPRBinding{{TaskID: "t", PRNumber: 7, Status: TaskStatusAwaitingOwnerReview}}
	other, err := events.NewEnvelope("event.metric.cpu", "src", "k", time.Now(), []byte(`{"number":7}`))
	if err != nil {
		t.Fatal(err)
	}
	res, err := runPollDetector([]events.Envelope{other}, bindings)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Decisions) != 0 {
		t.Fatalf("non-PR envelope must not transition, got %+v", res.Decisions)
	}
}

// itoa is a tiny dependency-free int formatter for building test JSON payloads
// without pulling strconv into the test's import surface noise.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
