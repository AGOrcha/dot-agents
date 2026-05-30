package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/NikashPrakash/dot-agents/internal/ui"
)

// Slot accounting and downstream dependency-satisfaction predicates for the
// layered-PR-fanout model. Centralized here (package workflow) so eligibility
// computation, concurrency accounting, and the `da workflow slots` inspector
// all read the §2.8 table and the §3.4.6/§4 dep rule from one place.
//
// See .agents/workflow/specs/layered-pr-fanout/design.md §2.8 (concurrency
// accounting table), §3.4.3 (separate `blocked` ledger bucket), §3.4.6 and §4
// (dependency satisfaction: an upstream in {completed, awaiting_owner_review}
// satisfies a downstream dep; in_progress does NOT).

// blockedOnStatusPrefix marks a parameterized `blocked-on:<ref>` status
// (design.md §3.4). The `<ref>` is part of the persisted status string, not a
// separate field, so accounting must recognize the prefix rather than an exact
// match. The concrete ref vocabulary (task:/secret:/decision:/condition:) is
// owned by lpf-c; this file only needs to detect the umbrella for slot freeing
// and ledger bucketing.
const blockedOnStatusPrefix = "blocked-on:"

// isBlockedOnStatus reports whether s is a parameterized `blocked-on:<ref>`
// status (§3.4). A bare `blocked` is the terminal/external block (§3.1) and is
// NOT a `blocked-on:<ref>`; the two are bucketed together for slot accounting
// (both free the slot) but distinguished where the ref matters.
func isBlockedOnStatus(s string) bool {
	return strings.HasPrefix(s, blockedOnStatusPrefix)
}

// countsAgainstParallelTasks reports whether a task in status s occupies a slot
// against max_parallel_tasks (design.md §2.8). Exactly two statuses hold a
// slot: in_progress (actively implementing) and awaiting_agent_review (lens
// dispatch can bounce work back to in_progress within the bounded lens budget).
// Every other status — pending, awaiting_owner_review, blocked, blocked-on:*,
// completed, cancelled — does not count.
func countsAgainstParallelTasks(s string) bool {
	return s == TaskStatusInProgress || s == TaskStatusAwaitingAgentReview
}

// freesSlot reports whether a task in status s, having previously held a slot,
// frees it (design.md §2.8). awaiting_owner_review frees the slot (human-review
// latency is unbounded) and blocked-on:<ref> frees the slot (§3.4.3, no active
// compute). Consulted by bucketForStatus to split the freed-slot statuses from
// the occupied ones, and exported as the seam lpf-c/lpf-e reuse for the
// owner-review / blocked-on transitions.
func freesSlot(s string) bool {
	return s == TaskStatusAwaitingOwnerReview || isBlockedOnStatus(s)
}

// depSatisfiesDownstream reports whether an upstream dependency in status s
// satisfies a downstream task's dependency (design.md §3.4.6 + §4). This is the
// load-bearing change of the layered-fanout model: an upstream PR that is open
// and accepted by the lens reviewers (awaiting_owner_review) unblocks its
// dependents BEFORE the maintainer merges it — decoupling downstream velocity
// from review latency (§1.2 goal 1).
//
//   - completed              → satisfies (PR merged on master)
//   - awaiting_owner_review  → satisfies (PR open, green, lens-accepted)
//   - in_progress            → does NOT satisfy (impl may still bounce)
//   - awaiting_agent_review  → does NOT satisfy (lens dispatch may reject)
//   - blocked / blocked-on:* → does NOT satisfy (§3.4.6: no block-on-block)
//   - pending / cancelled    → does NOT satisfy
func depSatisfiesDownstream(s string) bool {
	return s == TaskStatusCompleted || s == TaskStatusAwaitingOwnerReview
}

// slotLedgerBucket names a slot-ledger accounting bucket. The `blocked` bucket
// is tracked separately (design.md §3.4.3) so the orchestrator can see the
// "all slots blocked, nothing actually running" pathology rather than reading
// it as idle capacity.
type slotLedgerBucket string

const (
	// slotBucketOccupied counts tasks holding a slot against max_parallel_tasks
	// (in_progress + awaiting_agent_review per §2.8).
	slotBucketOccupied slotLedgerBucket = "occupied"
	// slotBucketAwaitingOwner counts tasks in awaiting_owner_review — slot
	// freed, but tracked so the review backlog is visible.
	slotBucketAwaitingOwner slotLedgerBucket = "awaiting_owner"
	// slotBucketBlocked counts blocked + blocked-on:<ref> tasks (§3.4.3). Slot
	// freed; surfaced separately so an all-blocked DAG is not mistaken for idle
	// capacity.
	slotBucketBlocked slotLedgerBucket = "blocked"
	// slotBucketPending counts not-yet-started tasks (pending).
	slotBucketPending slotLedgerBucket = "pending"
	// slotBucketTerminal counts completed + cancelled tasks.
	slotBucketTerminal slotLedgerBucket = "terminal"
)

// SlotLedger is the §2.8 / §3.4.3 accounting snapshot across one or more plans.
// Occupied is the live count against MaxParallel; the remaining buckets make
// the freed-slot reasons (owner-review backlog, blocked pathology) inspectable.
type SlotLedger struct {
	Occupied      int `json:"occupied"`
	AwaitingOwner int `json:"awaiting_owner"`
	Blocked       int `json:"blocked"`
	Pending       int `json:"pending"`
	Terminal      int `json:"terminal"`
	MaxParallel   int `json:"max_parallel"`
	// Available is MaxParallel - Occupied, floored at zero: how many new tasks
	// could start a slot right now.
	Available int `json:"available"`
}

// bucketForStatus maps a persisted task status to its ledger bucket. Unknown
// statuses fall through to terminal so a legacy row never inflates the live
// occupancy count.
func bucketForStatus(s string) slotLedgerBucket {
	switch {
	case countsAgainstParallelTasks(s):
		return slotBucketOccupied
	case freesSlot(s):
		// awaiting_owner_review and blocked-on:<ref> both free the slot (§2.8,
		// §3.4.3) but bucket differently: the review backlog vs the blocked
		// pathology.
		if s == TaskStatusAwaitingOwnerReview {
			return slotBucketAwaitingOwner
		}
		return slotBucketBlocked
	case s == TaskStatusBlocked:
		return slotBucketBlocked
	case s == TaskStatusPending:
		return slotBucketPending
	default:
		return slotBucketTerminal
	}
}

// addToLedger increments the bucket for status s on the ledger.
func (l *SlotLedger) addToLedger(s string) {
	switch bucketForStatus(s) {
	case slotBucketOccupied:
		l.Occupied++
	case slotBucketAwaitingOwner:
		l.AwaitingOwner++
	case slotBucketBlocked:
		l.Blocked++
	case slotBucketPending:
		l.Pending++
	case slotBucketTerminal:
		l.Terminal++
	}
}

// computeSlotLedger builds the §2.8 ledger from the given task statuses and the
// configured max_parallel_tasks. Available is derived (MaxParallel - Occupied,
// floored at zero).
func computeSlotLedger(statuses []string, maxParallel int) SlotLedger {
	ledger := SlotLedger{MaxParallel: maxParallel}
	for _, s := range statuses {
		ledger.addToLedger(s)
	}
	ledger.Available = maxParallel - ledger.Occupied
	if ledger.Available < 0 {
		ledger.Available = 0
	}
	return ledger
}

// renderSlotLedger writes the human-readable §2.8 ledger to stdout. The
// `blocked` bucket is always shown (even at zero) so the all-blocked pathology
// is never hidden by omission (§3.4.3).
func renderSlotLedger(l SlotLedger) {
	ui.Header("Slot Ledger")
	fmt.Fprintf(os.Stdout, "  occupied:        %d / %d (max_parallel_tasks)\n", l.Occupied, l.MaxParallel)
	fmt.Fprintf(os.Stdout, "  available:       %d\n", l.Available)
	fmt.Fprintf(os.Stdout, "  awaiting_owner:  %d (slot freed; review backlog)\n", l.AwaitingOwner)
	fmt.Fprintf(os.Stdout, "  blocked:         %d (slot freed; §3.4.3 bucket)\n", l.Blocked)
	fmt.Fprintf(os.Stdout, "  pending:         %d\n", l.Pending)
	fmt.Fprintf(os.Stdout, "  terminal:        %d (completed/cancelled)\n", l.Terminal)
	if l.Occupied > 0 && l.Available == 0 && l.Blocked > 0 {
		fmt.Fprintln(os.Stdout, "  note: all slots occupied while tasks sit blocked — orchestrator attention warranted")
	}
	fmt.Fprintln(os.Stdout)
}

// emitSlotLedgerJSON encodes the ledger as indented JSON to stdout.
func emitSlotLedgerJSON(l SlotLedger) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(l)
}

// collectTaskStatuses returns the status of every task across the named active
// plans (or all active plans when planFilter is empty). Non-active plans and
// unloadable task files are skipped — the ledger reflects in-flight work, not
// archived or draft plans.
func collectTaskStatuses(projectPath string, planFilter []string) ([]string, error) {
	ids, err := listCanonicalPlanIDs(projectPath)
	if err != nil {
		return nil, err
	}
	ids, err = applyEligiblePlanFilter(ids, planFilter)
	if err != nil {
		return nil, err
	}
	var statuses []string
	for _, id := range ids {
		statuses = append(statuses, planTaskStatuses(projectPath, id)...)
	}
	return statuses, nil
}

// planTaskStatuses returns the statuses of an active plan's tasks, or nil when
// the plan is missing, not active, or its task file cannot be loaded.
func planTaskStatuses(projectPath, planID string) []string {
	plan, err := loadCanonicalPlan(projectPath, planID)
	if err != nil || plan.Status != "active" {
		return nil
	}
	tf, err := loadCanonicalTasks(projectPath, planID)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(tf.Tasks))
	for _, t := range tf.Tasks {
		out = append(out, t.Status)
	}
	return out
}

// resolveMaxParallelTasks reads the configured slot budget (design.md §2.9
// default 7 when unset; the resolved preference layer supplies the project
// value or its own default when present).
func resolveMaxParallelTasks(prefs WorkflowPreferences) int {
	if prefs.Execution.MaxParallelWorkers != nil {
		return *prefs.Execution.MaxParallelWorkers
	}
	return defaultMaxParallelTasks
}

// defaultMaxParallelTasks is the §2.9 fallback when no preference is resolved.
const defaultMaxParallelTasks = 7

// runWorkflowSlots implements `da workflow slots`: renders the §2.8 / §3.4.3
// slot ledger across active plans so the orchestrator can see live occupancy,
// the owner-review backlog, and the blocked-bucket pathology at a glance.
func runWorkflowSlots(planFilter string) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	prefs, err := resolvePreferences(project.Path, project.Name)
	if err != nil {
		return err
	}
	statuses, err := collectTaskStatuses(project.Path, parsePlanIDFilter(planFilter))
	if err != nil {
		return err
	}
	ledger := computeSlotLedger(statuses, resolveMaxParallelTasks(prefs))
	if deps.Flags.JSON() {
		return emitSlotLedgerJSON(ledger)
	}
	renderSlotLedger(ledger)
	return nil
}
