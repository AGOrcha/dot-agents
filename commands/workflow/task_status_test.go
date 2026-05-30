package workflow

import (
	"strings"
	"testing"
)

// TestIsValidTaskStatusVocabulary covers the full status vocabulary: every
// legal status (including the two new awaiting_review sub-statuses) and a
// representative set of rejects.
func TestIsValidTaskStatusVocabulary(t *testing.T) {
	tests := []struct {
		name   string
		status string
		want   bool
	}{
		{"pending", TaskStatusPending, true},
		{"in_progress", TaskStatusInProgress, true},
		{"blocked", TaskStatusBlocked, true},
		{"completed", TaskStatusCompleted, true},
		{"cancelled", TaskStatusCancelled, true},
		{"awaiting_agent_review", TaskStatusAwaitingAgentReview, true},
		{"awaiting_owner_review", TaskStatusAwaitingOwnerReview, true},
		{"awaiting_review umbrella is not persistable", "awaiting_review", false},
		{"empty", "", false},
		{"unknown", "in-review", false},
		{"typo", "complete", false},
		{"pr_open rejected name", "pr_open", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isValidTaskStatus(tc.status); got != tc.want {
				t.Errorf("isValidTaskStatus(%q) = %v, want %v", tc.status, got, tc.want)
			}
		})
	}
}

// TestIsAwaitingReviewStatus verifies the umbrella predicate matches exactly
// the two sub-statuses and nothing else.
func TestIsAwaitingReviewStatus(t *testing.T) {
	tests := []struct {
		status string
		want   bool
	}{
		{TaskStatusAwaitingAgentReview, true},
		{TaskStatusAwaitingOwnerReview, true},
		{"awaiting_review", false},
		{TaskStatusInProgress, false},
		{TaskStatusCompleted, false},
		{"", false},
	}
	for _, tc := range tests {
		t.Run(tc.status, func(t *testing.T) {
			if got := isAwaitingReviewStatus(tc.status); got != tc.want {
				t.Errorf("isAwaitingReviewStatus(%q) = %v, want %v", tc.status, got, tc.want)
			}
		})
	}
}

// TestIsValidTaskStatusTransition exercises the §3.1/§3.2 state-machine
// matrix: every legal edge, a spread of illegal edges, self-transitions,
// terminal states, and the permissive unknown-from fallback.
func TestIsValidTaskStatusTransition(t *testing.T) {
	tests := []struct {
		name string
		from string
		to   string
		want bool
	}{
		// ── legal edges (§3.2) ──────────────────────────────────────────
		{"pending→in_progress", TaskStatusPending, TaskStatusInProgress, true},
		{"pending→cancelled", TaskStatusPending, TaskStatusCancelled, true},
		{"pending→blocked (cascade)", TaskStatusPending, TaskStatusBlocked, true},
		{"pending→completed (direct)", TaskStatusPending, TaskStatusCompleted, true},
		{"in_progress→awaiting_agent_review", TaskStatusInProgress, TaskStatusAwaitingAgentReview, true},
		{"in_progress→blocked", TaskStatusInProgress, TaskStatusBlocked, true},
		{"in_progress→cancelled", TaskStatusInProgress, TaskStatusCancelled, true},
		{"in_progress→completed (direct)", TaskStatusInProgress, TaskStatusCompleted, true},
		{"agent_review→owner_review", TaskStatusAwaitingAgentReview, TaskStatusAwaitingOwnerReview, true},
		{"agent_review→in_progress (lens reject)", TaskStatusAwaitingAgentReview, TaskStatusInProgress, true},
		{"agent_review→blocked (cascade)", TaskStatusAwaitingAgentReview, TaskStatusBlocked, true},
		{"owner_review→completed (merge)", TaskStatusAwaitingOwnerReview, TaskStatusCompleted, true},
		{"owner_review→in_progress (request changes)", TaskStatusAwaitingOwnerReview, TaskStatusInProgress, true},
		{"owner_review→blocked (PR closed)", TaskStatusAwaitingOwnerReview, TaskStatusBlocked, true},
		{"blocked→in_progress (unblock)", TaskStatusBlocked, TaskStatusInProgress, true},
		{"blocked→cancelled", TaskStatusBlocked, TaskStatusCancelled, true},

		// ── self-transitions are idempotent no-ops ──────────────────────
		{"self in_progress", TaskStatusInProgress, TaskStatusInProgress, true},
		{"self completed", TaskStatusCompleted, TaskStatusCompleted, true},
		{"self awaiting_owner_review", TaskStatusAwaitingOwnerReview, TaskStatusAwaitingOwnerReview, true},

		// ── illegal edges ───────────────────────────────────────────────
		{"pending→awaiting_agent_review (skips in_progress)", TaskStatusPending, TaskStatusAwaitingAgentReview, false},
		{"pending→awaiting_owner_review", TaskStatusPending, TaskStatusAwaitingOwnerReview, false},
		{"in_progress→awaiting_owner_review (skips agent review)", TaskStatusInProgress, TaskStatusAwaitingOwnerReview, false},
		{"in_progress→completed-via... agent_review→completed (skips owner)", TaskStatusAwaitingAgentReview, TaskStatusCompleted, false},
		{"agent_review→cancelled", TaskStatusAwaitingAgentReview, TaskStatusCancelled, false},
		{"owner_review→cancelled", TaskStatusAwaitingOwnerReview, TaskStatusCancelled, false},
		{"owner_review→agent_review (no backward)", TaskStatusAwaitingOwnerReview, TaskStatusAwaitingAgentReview, false},
		{"blocked→completed (must resume first)", TaskStatusBlocked, TaskStatusCompleted, false},
		{"blocked→awaiting_agent_review", TaskStatusBlocked, TaskStatusAwaitingAgentReview, false},

		// ── terminal states have no outbound edges ──────────────────────
		{"completed→in_progress (terminal)", TaskStatusCompleted, TaskStatusInProgress, false},
		{"completed→pending (terminal)", TaskStatusCompleted, TaskStatusPending, false},
		{"cancelled→in_progress (terminal)", TaskStatusCancelled, TaskStatusInProgress, false},

		// ── unknown from-status is permissive (legacy rows not wedged) ──
		{"unknown-from is permissive", "legacy_status", TaskStatusInProgress, true},
		{"unknown-from self", "legacy_status", "legacy_status", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isValidTaskStatusTransition(tc.from, tc.to); got != tc.want {
				t.Errorf("isValidTaskStatusTransition(%q, %q) = %v, want %v", tc.from, tc.to, got, tc.want)
			}
		})
	}
}

// TestAllowedTaskStatusTransitions verifies the hint helper returns the legal
// next statuses in state-machine order and nil for terminal/unknown states.
func TestAllowedTaskStatusTransitions(t *testing.T) {
	tests := []struct {
		name string
		from string
		want []string
	}{
		{
			name: "in_progress",
			from: TaskStatusInProgress,
			want: []string{TaskStatusAwaitingAgentReview, TaskStatusBlocked, TaskStatusCompleted, TaskStatusCancelled},
		},
		{
			name: "awaiting_agent_review",
			from: TaskStatusAwaitingAgentReview,
			want: []string{TaskStatusInProgress, TaskStatusAwaitingOwnerReview, TaskStatusBlocked},
		},
		{
			name: "awaiting_owner_review",
			from: TaskStatusAwaitingOwnerReview,
			want: []string{TaskStatusInProgress, TaskStatusBlocked, TaskStatusCompleted},
		},
		{"completed terminal", TaskStatusCompleted, nil},
		{"cancelled terminal", TaskStatusCancelled, nil},
		{"unknown from", "legacy_status", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := allowedTaskStatusTransitions(tc.from)
			if len(got) != len(tc.want) {
				t.Fatalf("allowedTaskStatusTransitions(%q) = %v, want %v", tc.from, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("allowedTaskStatusTransitions(%q)[%d] = %q, want %q (full: %v)", tc.from, i, got[i], tc.want[i], got)
				}
			}
		})
	}
}

// TestRunWorkflowAdvance_AcceptsAwaitingReviewStatuses verifies the staged
// path (in_progress → awaiting_agent_review → awaiting_owner_review) is
// accepted end-to-end by the advance command and persisted to TASKS.yaml.
func TestRunWorkflowAdvance_AcceptsAwaitingReviewStatuses(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	chdirRepo(t, repo)

	// t1 starts in_progress in the fixture.
	if err := runWorkflowAdvance("wave-2", "t1", TaskStatusAwaitingAgentReview); err != nil {
		t.Fatalf("advance t1 → awaiting_agent_review: %v", err)
	}
	if err := runWorkflowAdvance("wave-2", "t1", TaskStatusAwaitingOwnerReview); err != nil {
		t.Fatalf("advance t1 → awaiting_owner_review: %v", err)
	}
	tf, err := loadCanonicalTasks(repo, "wave-2")
	if err != nil {
		t.Fatalf("loadCanonicalTasks: %v", err)
	}
	for _, task := range tf.Tasks {
		if task.ID == "t1" && task.Status != TaskStatusAwaitingOwnerReview {
			t.Errorf("t1 status = %q, want %q", task.Status, TaskStatusAwaitingOwnerReview)
		}
	}
}

// TestRunWorkflowAdvance_RejectsIllegalTransition verifies the advance
// command rejects a transition that is not a legal §3.2 edge and leaves the
// persisted status untouched.
func TestRunWorkflowAdvance_RejectsIllegalTransition(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	chdirRepo(t, repo)

	// t1 is in_progress; jumping straight to awaiting_owner_review skips the
	// agent-review gate and must be rejected.
	err := runWorkflowAdvance("wave-2", "t1", TaskStatusAwaitingOwnerReview)
	if err == nil {
		t.Fatal("expected illegal-transition error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid status transition") {
		t.Errorf("error %q should mention invalid status transition", err.Error())
	}
	tf, err := loadCanonicalTasks(repo, "wave-2")
	if err != nil {
		t.Fatalf("loadCanonicalTasks: %v", err)
	}
	for _, task := range tf.Tasks {
		if task.ID == "t1" && task.Status != TaskStatusInProgress {
			t.Errorf("rejected advance must not mutate status; t1 = %q, want %q", task.Status, TaskStatusInProgress)
		}
	}
}

// TestRunWorkflowAdvance_RejectsUnknownStatus verifies an unrecognized
// --status value is rejected by the vocabulary gate before any transition
// check.
func TestRunWorkflowAdvance_RejectsUnknownStatus(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	chdirRepo(t, repo)

	err := runWorkflowAdvance("wave-2", "t1", "pr_open")
	if err == nil {
		t.Fatal("expected invalid-status error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid task status") {
		t.Errorf("error %q should mention invalid task status", err.Error())
	}
}

// TestTaskStatusVocabularyHint asserts the hint string lists every valid
// status and stays free of the rejected pr_open name.
func TestTaskStatusVocabularyHint(t *testing.T) {
	hint := taskStatusVocabularyHint()
	for _, s := range validTaskStatusList {
		if !strings.Contains(hint, s) {
			t.Errorf("vocabulary hint missing status %q: %s", s, hint)
		}
	}
	if strings.Contains(hint, "pr_open") {
		t.Errorf("vocabulary hint must not mention rejected pr_open name: %s", hint)
	}
}

// TestTransitionRejectedHint covers both branches: a from-status with legal
// next statuses lists them; a terminal from-status falls back to the generic
// state-machine message.
func TestTransitionRejectedHint(t *testing.T) {
	withNext := transitionRejectedHint(TaskStatusInProgress)
	if !strings.Contains(withNext, TaskStatusAwaitingAgentReview) {
		t.Errorf("hint for in_progress should name allowed next statuses; got %q", withNext)
	}
	terminal := transitionRejectedHint(TaskStatusCompleted)
	if !strings.Contains(terminal, "§3.1") {
		t.Errorf("terminal from-status should fall back to state-machine hint; got %q", terminal)
	}
}

// TestApplyTaskStatusTransition exercises the helper directly: legal edge
// mutates in place and returns the title; rejected edge and missing task both
// error and leave the slice untouched.
func TestApplyTaskStatusTransition(t *testing.T) {
	newTaskFile := func() *CanonicalTaskFile {
		return &CanonicalTaskFile{
			SchemaVersion: 1,
			PlanID:        "p",
			Tasks: []CanonicalTask{
				{ID: "t1", Title: "first", Status: TaskStatusInProgress},
			},
		}
	}

	t.Run("legal edge mutates and returns title", func(t *testing.T) {
		tf := newTaskFile()
		title, err := applyTaskStatusTransition(tf, "p", "t1", TaskStatusAwaitingAgentReview)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if title != "first" {
			t.Errorf("title = %q, want %q", title, "first")
		}
		if tf.Tasks[0].Status != TaskStatusAwaitingAgentReview {
			t.Errorf("status not applied: %q", tf.Tasks[0].Status)
		}
	})

	t.Run("illegal edge (skips agent gate) leaves status untouched", func(t *testing.T) {
		tf := newTaskFile()
		_, err := applyTaskStatusTransition(tf, "p", "t1", TaskStatusAwaitingOwnerReview)
		if err == nil {
			t.Fatal("expected rejection for in_progress→awaiting_owner_review")
		}
		if tf.Tasks[0].Status != TaskStatusInProgress {
			t.Errorf("rejected transition mutated status to %q", tf.Tasks[0].Status)
		}
	})

	t.Run("missing task errors", func(t *testing.T) {
		tf := newTaskFile()
		_, err := applyTaskStatusTransition(tf, "p", "nope", TaskStatusInProgress)
		if err == nil {
			t.Fatal("expected not-found error")
		}
		if !strings.Contains(err.Error(), "nope") {
			t.Errorf("error should name the missing task id; got %q", err.Error())
		}
	})
}
