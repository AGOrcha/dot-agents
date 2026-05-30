package workflow

import "strings"

// Task status vocabulary for canonical TASKS.yaml rows and the
// `da workflow advance --status` command. Centralized here so every
// predicate (eligibility, concurrency accounting, advance validation)
// reads from a single source of truth.
//
// See .agents/workflow/specs/layered-pr-fanout/design.md §2.1, §2.2,
// §3.1, §3.2 for the state machine these constants encode.
const (
	// TaskStatusPending — task created, dependencies not yet satisfied or
	// not yet picked up by a worker.
	TaskStatusPending = "pending"
	// TaskStatusInProgress — a worker holds the task and is actively
	// implementing it.
	TaskStatusInProgress = "in_progress"
	// TaskStatusBlocked — task cannot proceed (external block, cascade,
	// or unrecoverable verify failure); manual recovery required.
	TaskStatusBlocked = "blocked"
	// TaskStatusCompleted — terminal success (e.g. PR merged on GitHub).
	TaskStatusCompleted = "completed"
	// TaskStatusCancelled — terminal manual abandonment.
	TaskStatusCancelled = "cancelled"

	// TaskStatusAwaitingAgentReview is the first sub-status of the
	// `awaiting_review` umbrella (§2.2): branch pushed, PR open, verifier
	// chain green; lens reviewers are dispatched or running. Counts
	// against max_parallel_tasks (§2.8) because lens dispatch can bounce
	// work back to in_progress.
	TaskStatusAwaitingAgentReview = "awaiting_agent_review"
	// TaskStatusAwaitingOwnerReview is the second sub-status of
	// `awaiting_review` (§2.2): lens verdicts accepted; the human
	// maintainer owns the merge decision. Frees its slot (§2.8) because
	// human-review latency is unbounded.
	TaskStatusAwaitingOwnerReview = "awaiting_owner_review"
)

// awaitingReviewSubStatuses lists the concrete persisted statuses that
// make up the `awaiting_review` umbrella concept (§2.2). The umbrella
// name itself is never persisted on a task row; the sub-status is.
var awaitingReviewSubStatuses = map[string]bool{
	TaskStatusAwaitingAgentReview: true,
	TaskStatusAwaitingOwnerReview: true,
}

// validTaskStatuses is the full set of persistable task statuses.
var validTaskStatuses = map[string]bool{
	TaskStatusPending:             true,
	TaskStatusInProgress:          true,
	TaskStatusBlocked:             true,
	TaskStatusCompleted:           true,
	TaskStatusCancelled:           true,
	TaskStatusAwaitingAgentReview: true,
	TaskStatusAwaitingOwnerReview: true,
}

// validTaskStatusList renders the vocabulary for error hints in a stable
// order (state-machine order, not map iteration order).
var validTaskStatusList = []string{
	TaskStatusPending,
	TaskStatusInProgress,
	TaskStatusAwaitingAgentReview,
	TaskStatusAwaitingOwnerReview,
	TaskStatusBlocked,
	TaskStatusCompleted,
	TaskStatusCancelled,
}

// isValidTaskStatus reports whether s is a recognized task status.
func isValidTaskStatus(s string) bool {
	return validTaskStatuses[s]
}

// isAwaitingReviewStatus reports whether s is one of the `awaiting_review`
// sub-statuses (§2.2). Centralized so eligibility ("is dep in
// awaiting_review-or-better?") and concurrency accounting can split on the
// umbrella without re-listing the sub-statuses.
func isAwaitingReviewStatus(s string) bool {
	return awaitingReviewSubStatuses[s]
}

// validTaskStatusTransitions encodes the §3.1/§3.2 state machine as an
// adjacency map: from-status → set of legal to-statuses. A status absent as
// a key (or a to-status absent from its set) is an illegal transition.
//
// Self-transitions (status == newStatus) are handled separately in
// isValidTaskStatusTransition as idempotent no-ops, so they are not listed
// here.
var validTaskStatusTransitions = map[string]map[string]bool{
	TaskStatusPending: {
		TaskStatusInProgress: true,
		TaskStatusCancelled:  true,
		// A pending task may be blocked by an upstream cascade (§2.6,
		// `* → blocked`) before it is ever picked up.
		TaskStatusBlocked: true,
		// Direct (non-staged) reconciliation: a pending row may be marked
		// completed in one step when the work was done outside the staged
		// PR model (legacy direct-advance / state catch-up).
		TaskStatusCompleted: true,
	},
	TaskStatusInProgress: {
		TaskStatusAwaitingAgentReview: true, // §2.3 precondition met (verifier-owned)
		TaskStatusBlocked:             true, // §3.2 in_progress → blocked
		TaskStatusCancelled:           true, // §3.2 in_progress → cancelled
		// Direct (non-staged) completion: the legacy direct worker closeout
		// (`workflow advance --status completed`) skips the awaiting_review
		// gate because there is no PR to merge. The staged path routes
		// completion through awaiting_owner_review (§3.2); both are legal.
		TaskStatusCompleted: true,
	},
	TaskStatusAwaitingAgentReview: {
		TaskStatusAwaitingOwnerReview: true, // §3.2 all lens verdicts accept
		TaskStatusInProgress:          true, // §3.2 lens reject / verifier bounce
		TaskStatusBlocked:             true, // §3.2 `* → blocked` cascade
	},
	TaskStatusAwaitingOwnerReview: {
		TaskStatusCompleted:  true, // §3.2 PR merged
		TaskStatusInProgress: true, // §3.2 maintainer requests changes
		TaskStatusBlocked:    true, // §3.2 maintainer closes PR (cascade)
	},
	// blocked is a recovery state: manual unblock re-enters in_progress, or
	// the task may be cancelled outright.
	TaskStatusBlocked: {
		TaskStatusInProgress: true,
		TaskStatusCancelled:  true,
	},
	// completed and cancelled are terminal: no outbound edges.
	TaskStatusCompleted: {},
	TaskStatusCancelled: {},
}

// isValidTaskStatusTransition reports whether moving a task from->to is a
// legal edge in the §3.1 state machine. A self-transition (from == to) is
// always treated as a legal idempotent no-op. An unrecognized from-status
// is treated permissively (returns true) so that legacy rows carrying a
// status this binary does not know about are not wedged — only the to-status
// is then constrained by isValidTaskStatus at the call site.
func isValidTaskStatusTransition(from, to string) bool {
	if from == to {
		return true
	}
	edges, known := validTaskStatusTransitions[from]
	if !known {
		return true
	}
	return edges[to]
}

// allowedTaskStatusTransitions returns the legal to-statuses for from, in
// state-machine order, for use in error hints. Returns nil for terminal or
// unknown from-statuses.
func allowedTaskStatusTransitions(from string) []string {
	edges := validTaskStatusTransitions[from]
	if len(edges) == 0 {
		return nil
	}
	var out []string
	for _, s := range validTaskStatusList {
		if edges[s] {
			out = append(out, s)
		}
	}
	return out
}

// taskStatusVocabularyHint renders the valid-status list for error messages.
func taskStatusVocabularyHint() string {
	quoted := make([]string, len(validTaskStatusList))
	for i, s := range validTaskStatusList {
		quoted[i] = "`" + s + "`"
	}
	return "Valid values: " + strings.Join(quoted, ", ") + "."
}
