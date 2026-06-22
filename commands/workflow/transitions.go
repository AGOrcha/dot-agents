package workflow

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/AGOrcha/dot-agents/internal/events"
)

// Verifier-vs-lens-gate-vs-owner transition split (design.md §6) and the PR
// poll detector (§3.2).
//
// The path from in_progress to completed crosses three edges, each owned by a
// distinct dispatcher (§6). The ownership is load-bearing: Done Criterion #8
// requires that the verifier dispatcher is the *sole* code path performing the
// in_progress → awaiting_agent_review transition and the lens-gate dispatcher
// the *sole* code path performing awaiting_agent_review → awaiting_owner_review.
// To make that guarantee enforceable rather than a convention, each owner
// function refuses to act on a source status it does not own, in addition to
// validating the edge against the §3.1 state machine in task_status.go.
//
// The PR poll detector (§3.2) is the owner of the awaiting_owner_review-side
// edges. It consumes event.pr.* envelopes produced by the internal/events PR
// producer (lpf-pr-producer) — it never shells out to gh directly — and maps
// each envelope kind onto the transition it triggers.

// transitionOwner identifies which §6 dispatcher is permitted to perform a
// given edge. It is recorded on every transitionDecision so a caller (and a
// test) can assert Done-Criterion-#8 ownership without re-deriving it.
type transitionOwner string

const (
	// ownerVerifier owns in_progress → awaiting_agent_review (§6.1).
	ownerVerifier transitionOwner = "verifier"
	// ownerLensGate owns awaiting_agent_review → {awaiting_owner_review,
	// in_progress} (§6.2).
	ownerLensGate transitionOwner = "lens-gate"
	// ownerPollDetector owns every awaiting_owner_review-side edge and the
	// force-rebase regression, all driven by event.pr.* envelopes (§3.2/§6.3).
	ownerPollDetector transitionOwner = "poll-detector"
)

// transitionDecision is the pure result of evaluating an owner's transition: a
// from→to edge attributed to the owning dispatcher, plus optional flags the
// caller persists alongside the status mutation. It carries no side effects so
// the decision can be unit-tested independently of TASKS.yaml persistence.
type transitionDecision struct {
	// TaskID is the task whose status is being transitioned.
	TaskID string
	// Owner is the §6 dispatcher that produced the decision.
	Owner transitionOwner
	// From is the task's status before the transition.
	From string
	// To is the task's status after the transition.
	To string
	// RebaseOnly marks a §7.3 downstream rebase-only iteration: the base
	// moved (force-rebase) but the impl is not wrong, so the worker runs a
	// rebase pass rather than a full re-impl.
	RebaseOnly bool
	// Cascade marks a §2.6 hard-block of downstream tasks: the upstream PR was
	// closed without merge, so every downstream task branched off it blocks.
	Cascade bool
}

// ── §6.1 Verifier: in_progress → awaiting_agent_review ─────────────────────────

// verifierTransition is the §6.1 dispatcher: the sole code path that performs
// in_progress → awaiting_agent_review. It refuses any source status other than
// in_progress (ownership guard for Done Criterion #8) and evaluates the resolved
// §2.3 precondition policy against the observed signal snapshot before emitting
// the decision. The policy is a configurable, table-driven set of predicates
// (see preconditions.go) — not a hardcoded VCS/quality-tool struct — and an
// empty policy falls back to the built-in default (the gate is never open by
// omission). A failed predicate returns an error so the caller leaves the task
// untouched (the verifier may retry until its retry budget is exhausted, at
// which point the caller routes to blocked per §6.1).
func verifierTransition(taskID, from string, policy PreconditionPolicy, snap SignalSnapshot) (transitionDecision, error) {
	if from != TaskStatusInProgress {
		return transitionDecision{}, fmt.Errorf(
			"verifier owns only %s → %s; refusing to act on source status %q (§6.1)",
			TaskStatusInProgress, TaskStatusAwaitingAgentReview, from)
	}
	if ok, reason := evaluatePolicy(policy, snap); !ok {
		return transitionDecision{}, fmt.Errorf("verifier gate not open for task %q: %s", taskID, reason)
	}
	if err := validateTaskStatusTransition(taskID, from, TaskStatusAwaitingAgentReview); err != nil {
		return transitionDecision{}, err
	}
	return transitionDecision{
		TaskID: taskID,
		Owner:  ownerVerifier,
		From:   from,
		To:     TaskStatusAwaitingAgentReview,
	}, nil
}

// dispatchVerifierTransition is the seam that wires the §6.1 verifier dispatch:
// it resolves the task's app_type precondition policy from the LOCKED config
// (Slice B3/B4) and evaluates it against the producer's observed SignalSnapshot,
// then delegates to verifierTransition. The policy resolution is lockfile-backed
// (never raw .agentsrc.json), so the verifier gate observes the same merged,
// locked effective config as `da config explain`.
//
// Deferred wiring (owned by lpf-pr-producer): there is no LIVE caller of this seam yet — lpf-e landed
// the verifier transition + policy evaluation pure, and the producer that fills
// `snap` (the unified SignalSnapshot from event.pr.* / poll-detector signals) is
// owned by the lpf PR-producer / poll-detector work, NOT this plan. When that
// producer lands, the dispatch loop must call dispatchVerifierTransition with the
// task id, its current status, the resolved projectPath+appType, and the
// producer-built snapshot. Until then this seam is exercised only by unit tests,
// and the resolver (resolvePreconditionPolicy) is covered independently.
func dispatchVerifierTransition(projectPath, appType, taskID, from string, snap SignalSnapshot) (transitionDecision, error) {
	policy, err := resolvePreconditionPolicy(projectPath, appType)
	if err != nil {
		return transitionDecision{}, fmt.Errorf("resolving precondition policy for app_type %q: %w", appType, err)
	}
	return verifierTransition(taskID, from, policy, snap)
}

// ── §6.2 Lens-gate: awaiting_agent_review → awaiting_owner_review | in_progress ─

// lensVerdict is one lens reviewer's terminal outcome on a task in
// awaiting_agent_review (§6.2).
type lensVerdict struct {
	// Lens names the reviewer (architecture-standards, acceptance-invariants,
	// adversarial, optionally thermo-nuclear).
	Lens string
	// Accept is true when the lens accepts the change, false when it rejects.
	Accept bool
}

// lensGateTransition is the §6.2 dispatcher: the sole code path that performs
// awaiting_agent_review → awaiting_owner_review (on all-accept) or
// awaiting_agent_review → in_progress (on any reject). It refuses any source
// status other than awaiting_agent_review (ownership guard) and requires at
// least one verdict so an empty lens set cannot silently promote a task past
// review. On reject the worker re-engages with the lens findings as fresh
// context; the task already holds its slot (§2.8), so no slot is re-acquired.
func lensGateTransition(taskID, from string, verdicts []lensVerdict) (transitionDecision, error) {
	if from != TaskStatusAwaitingAgentReview {
		return transitionDecision{}, fmt.Errorf(
			"lens-gate owns only %s → %s; refusing to act on source status %q (§6.2)",
			TaskStatusAwaitingAgentReview, TaskStatusAwaitingOwnerReview, from)
	}
	if len(verdicts) == 0 {
		return transitionDecision{}, fmt.Errorf(
			"lens-gate for task %q has no lens verdicts; cannot promote past agent review (§6.2)", taskID)
	}
	to := TaskStatusAwaitingOwnerReview
	for _, v := range verdicts {
		if !v.Accept {
			to = TaskStatusInProgress
			break
		}
	}
	if err := validateTaskStatusTransition(taskID, from, to); err != nil {
		return transitionDecision{}, err
	}
	return transitionDecision{
		TaskID: taskID,
		Owner:  ownerLensGate,
		From:   from,
		To:     to,
	}, nil
}

// ── §3.2 / §6.3 PR poll detector ──────────────────────────────────────────────

// detectForceRebase implements the §2.7 SHA1 set-difference rule: a force-rebase
// is any drop of previously-observed commits (with replacement) between two
// eligible polls of a PR's commit list. It returns true only when at least one
// previously-seen SHA is absent from the current list AND the current list is
// non-empty (a replacement occurred) — a strict superset (new commits appended,
// none dropped) is an ordinary push, not a rebase. Order is irrelevant; only set
// membership matters, so the comparison is commutative across client/UI rebases.
func detectForceRebase(prevSHAs, curSHAs []string) bool {
	cur := make(map[string]bool, len(curSHAs))
	for _, s := range curSHAs {
		if t := strings.TrimSpace(s); t != "" {
			cur[t] = true
		}
	}
	if len(cur) == 0 {
		// No current commits observed: cannot assert a replacement happened.
		return false
	}
	for _, s := range prevSHAs {
		t := strings.TrimSpace(s)
		if t == "" {
			continue
		}
		if !cur[t] {
			// A previously-observed commit is gone and the PR still has
			// commits: the tip was rewritten (§2.7 force-rebase).
			return true
		}
	}
	return false
}

// pollEventTransition maps a single event.pr.* envelope onto the transition it
// triggers for the task backing that PR (§3.2 edge table). It is the heart of
// the poll detector and is intentionally pure: it consumes a canonical envelope
// (never a gh call) and the current status of the task, and returns a decision
// the caller persists. Mapping:
//
//	event.pr.merged                 → completed              (§6.3)
//	event.pr.review_requested_change → in_progress           (§3.2 "request changes")
//	event.pr.closed                 → blocked  (Cascade)     (§2.6 cascade-block downstream)
//	event.pr.force_rebased          → in_progress (RebaseOnly) (§2.7/§7.3 rebase-only iteration)
//
// A non-event.pr namespace, or a PR kind that is not a transition trigger
// (opened / ci_green / ci_failed / comment_posted), returns ok=false with no
// error so the detector skips it. An unrecognized event.pr.* kind is a loud
// error: the control-plane namespace is reject-on-unknown (pr-event-source D1),
// so a new PR kind that this detector has not been taught about must fail
// rather than be silently dropped.
func pollEventTransition(taskID, from string, env events.Envelope) (transitionDecision, bool, error) {
	if env.Namespace() != events.PRNamespace {
		return transitionDecision{}, false, nil
	}
	to, flags, trigger := pollTargetForKind(env.Type)
	if !trigger {
		switch env.Type {
		case events.KindPROpened, events.KindPRCIGreen, events.KindPRCIFailed, events.KindPRCommentPosted:
			// Recognized PR kinds that the poll detector does not map to a
			// task transition (they drive the verifier gate / observability,
			// not the poll-detector edges). Skip silently.
			return transitionDecision{}, false, nil
		default:
			return transitionDecision{}, false, fmt.Errorf(
				"poll detector: unrecognized control-plane PR kind %q for task %q (§3.2)", env.Type, taskID)
		}
	}
	if err := validateTaskStatusTransition(taskID, from, to); err != nil {
		return transitionDecision{}, false, err
	}
	return transitionDecision{
		TaskID:     taskID,
		Owner:      ownerPollDetector,
		From:       from,
		To:         to,
		RebaseOnly: flags.rebaseOnly,
		Cascade:    flags.cascade,
	}, true, nil
}

// pollDecisionFlags carries the §2.6 / §7.3 side-effect markers a poll-detector
// decision may set, kept separate so pollTargetForKind returns a small value.
type pollDecisionFlags struct {
	rebaseOnly bool
	cascade    bool
}

// pollTargetForKind returns the target status, decision flags, and whether the
// kind is a transition trigger, for a single event.pr.* kind. Centralizing the
// kind→edge map here keeps pollEventTransition's control flow flat and makes the
// §3.2 mapping a single readable table.
func pollTargetForKind(kind string) (to string, flags pollDecisionFlags, trigger bool) {
	switch kind {
	case events.KindPRMerged:
		return TaskStatusCompleted, pollDecisionFlags{}, true
	case events.KindPRReviewRequestedChange:
		return TaskStatusInProgress, pollDecisionFlags{}, true
	case events.KindPRClosed:
		return TaskStatusBlocked, pollDecisionFlags{cascade: true}, true
	case events.KindPRForceRebased:
		return TaskStatusInProgress, pollDecisionFlags{rebaseOnly: true}, true
	default:
		return "", pollDecisionFlags{}, false
	}
}

// taskPRBinding maps a task id to the PR number whose events drive its
// poll-detector transitions, plus the last-observed commit SHA set used for the
// §2.7 force-rebase diff. The poll detector resolves an envelope's PR number to
// a task through this binding so it never has to embed git topology.
type taskPRBinding struct {
	// TaskID is the task this PR backs.
	TaskID string
	// PRNumber is the PR whose event.pr.* envelopes transition the task.
	PRNumber int
	// Status is the task's current status (the From of any transition).
	Status string
	// LastSHAs is the commit SHA set observed on the previous poll, compared
	// against the envelope's current SHAs for §2.7 force-rebase detection.
	LastSHAs []string
}

// pollEnvelopePR is the slice of the canonical events.PR payload the poll
// detector reads: the PR number (to bind the envelope to a task) and the head
// commit list (for the §2.7 SHA1 set diff). The producer maps each source PR
// onto these fields, so this decode is platform-independent.
type pollEnvelopePR struct {
	Number  int      `json:"number"`
	Commits []string `json:"commits"`
}

// PollDetectorResult is the aggregate outcome of one poll cycle: the transition
// decisions to persist (in deterministic task-id order) and the per-task SHA
// sets observed this cycle (so the caller can persist them as next cycle's
// LastSHAs for force-rebase detection).
type PollDetectorResult struct {
	Decisions    []transitionDecision
	ObservedSHAs map[string][]string
}

// runPollDetector is the §3.2 poll detector entry point. It consumes a batch of
// event.pr.* envelopes (produced upstream by the internal/events PR producer —
// no gh calls here) against the current task↔PR bindings and returns the
// transition decisions plus the SHA sets observed this cycle. It is pure: it
// performs no persistence and no network I/O, so the whole §3.2 mapping —
// including the §2.7 force-rebase detection that the producer cannot express as
// a single kind — is unit-testable from envelopes alone.
//
// Force-rebase is synthesized here rather than consumed as a kind because §2.7
// detects it by SHA1 set difference across two polls, which only the detector
// (holding LastSHAs) can compute. When an envelope's PR shows a force-rebase,
// the detector emits the same decision an event.pr.force_rebased envelope would
// (in_progress + RebaseOnly), even if the producer only emitted a benign kind
// for that PR this cycle.
func runPollDetector(envs []events.Envelope, bindings []taskPRBinding) (PollDetectorResult, error) {
	byPR := make(map[int]taskPRBinding, len(bindings))
	for _, b := range bindings {
		byPR[b.PRNumber] = b
	}
	result := PollDetectorResult{ObservedSHAs: map[string][]string{}}
	var decisions []transitionDecision

	for _, env := range envs {
		dec, triggered, err := pollDetectEnvelope(env, byPR, result.ObservedSHAs)
		if err != nil {
			return PollDetectorResult{}, err
		}
		if triggered {
			decisions = append(decisions, dec)
		}
	}

	sort.SliceStable(decisions, func(i, j int) bool {
		return decisions[i].TaskID < decisions[j].TaskID
	})
	result.Decisions = decisions
	return result, nil
}

// pollDetectEnvelope resolves a single envelope to its (optional) transition
// decision within one poll cycle, recording the observed SHA set into observed
// as a side effect. It returns triggered=false (no error) for envelopes that do
// not transition a task — a non-PR namespace, an unbound PR, or a recognized
// non-trigger kind — so runPollDetector's loop body stays flat. Force-rebase
// (§2.7) is detected here because it overrides the envelope's own kind.
func pollDetectEnvelope(
	env events.Envelope,
	byPR map[int]taskPRBinding,
	observed map[string][]string,
) (transitionDecision, bool, error) {
	if env.Namespace() != events.PRNamespace {
		return transitionDecision{}, false, nil
	}
	var p pollEnvelopePR
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return transitionDecision{}, false, fmt.Errorf(
			"poll detector: decode pr envelope %q: %w", env.IdempotencyKey, err)
	}
	binding, ok := byPR[p.Number]
	if !ok {
		// An event for a PR no task is bound to: not ours to transition.
		return transitionDecision{}, false, nil
	}
	if len(p.Commits) > 0 {
		observed[binding.TaskID] = normalizeSHAs(p.Commits)
	}

	// §2.7: a force-rebase observed by SHA1 set diff overrides the envelope's
	// own kind — the base moved, which is a rebase-only regression regardless of
	// what kind the producer emitted this cycle.
	if detectForceRebase(binding.LastSHAs, p.Commits) {
		dec, err := synthesizeForceRebase(binding)
		return dec, err == nil, err
	}

	return pollEventTransition(binding.TaskID, binding.Status, env)
}

// synthesizeForceRebase builds the in_progress + RebaseOnly decision for a task
// whose PR force-rebased (§2.7/§7.3), validating the edge against the §3.1 state
// machine so a terminal task is not regressed.
func synthesizeForceRebase(b taskPRBinding) (transitionDecision, error) {
	if err := validateTaskStatusTransition(b.TaskID, b.Status, TaskStatusInProgress); err != nil {
		return transitionDecision{}, err
	}
	return transitionDecision{
		TaskID:     b.TaskID,
		Owner:      ownerPollDetector,
		From:       b.Status,
		To:         TaskStatusInProgress,
		RebaseOnly: true,
	}, nil
}

// normalizeSHAs trims and drops empty entries from a commit SHA list so the
// persisted LastSHAs set is clean for the next cycle's diff.
func normalizeSHAs(shas []string) []string {
	out := make([]string, 0, len(shas))
	for _, s := range shas {
		if t := strings.TrimSpace(s); t != "" {
			out = append(out, t)
		}
	}
	return out
}
