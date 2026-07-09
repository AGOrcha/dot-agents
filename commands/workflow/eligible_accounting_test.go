package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCountsAgainstParallelTasks asserts the §2.8 slot-occupancy column:
// exactly in_progress and awaiting_agent_review hold a slot.
func TestCountsAgainstParallelTasks(t *testing.T) {
	cases := []struct {
		status string
		want   bool
	}{
		{TaskStatusInProgress, true},
		{TaskStatusAwaitingAgentReview, true},
		{TaskStatusAwaitingOwnerReview, false},
		{TaskStatusPending, false},
		{TaskStatusBlocked, false},
		{TaskStatusCompleted, false},
		{TaskStatusCancelled, false},
		{"blocked-on:secret:FOO", false},
		{"unknown-legacy-status", false},
	}
	for _, c := range cases {
		if got := countsAgainstParallelTasks(c.status); got != c.want {
			t.Errorf("countsAgainstParallelTasks(%q) = %v, want %v", c.status, got, c.want)
		}
	}
}

// TestFreesSlot asserts the §2.8 / §3.4.3 freed-slot statuses:
// awaiting_owner_review and every blocked-on:<ref> variant free the slot.
func TestFreesSlot(t *testing.T) {
	cases := []struct {
		status string
		want   bool
	}{
		{TaskStatusAwaitingOwnerReview, true},
		{"blocked-on:task:p1/t1", true},
		{"blocked-on:decision:x", true},
		{TaskStatusInProgress, false},
		{TaskStatusAwaitingAgentReview, false},
		{TaskStatusBlocked, false},
		{TaskStatusPending, false},
	}
	for _, c := range cases {
		if got := freesSlot(c.status); got != c.want {
			t.Errorf("freesSlot(%q) = %v, want %v", c.status, got, c.want)
		}
	}
}

// TestSlotIsBlockedOnStatus distinguishes a parameterized blocked-on:<ref> from
// the bare terminal/external `blocked` status (§3.1 vs §3.4), for the slot-ledger
// helper. Named distinctly from blocked_on_test.go's TestIsBlockedOnStatus (which
// covers the exported IsBlockedOnStatus) to avoid a redeclaration after the
// lpf-b/lpf-c merges.
func TestSlotIsBlockedOnStatus(t *testing.T) {
	cases := []struct {
		status string
		want   bool
	}{
		{"blocked-on:secret:APPLE_ID", true},
		{"blocked-on:", true},
		{TaskStatusBlocked, false},
		{TaskStatusInProgress, false},
		{"", false},
	}
	for _, c := range cases {
		if got := isBlockedOnStatus(c.status); got != c.want {
			t.Errorf("isBlockedOnStatus(%q) = %v, want %v", c.status, got, c.want)
		}
	}
}

// TestDepSatisfiesDownstream is the load-bearing §3.4.6 / §4 predicate: an
// upstream in {completed, awaiting_owner_review} satisfies a downstream dep;
// in_progress and awaiting_agent_review do NOT (impl/lens may still bounce).
func TestDepSatisfiesDownstream(t *testing.T) {
	cases := []struct {
		status string
		want   bool
	}{
		{TaskStatusCompleted, true},
		{TaskStatusAwaitingOwnerReview, true},
		{TaskStatusInProgress, false},
		{TaskStatusAwaitingAgentReview, false},
		{TaskStatusPending, false},
		{TaskStatusBlocked, false},
		{"blocked-on:task:p/t", false},
		{TaskStatusCancelled, false},
	}
	for _, c := range cases {
		if got := depSatisfiesDownstream(c.status); got != c.want {
			t.Errorf("depSatisfiesDownstream(%q) = %v, want %v", c.status, got, c.want)
		}
	}
}

// TestBucketForStatus asserts the ledger bucket mapping, including the
// unknown-status fall-through to terminal (so a legacy row never inflates the
// live occupancy count).
func TestBucketForStatus(t *testing.T) {
	cases := []struct {
		status string
		want   slotLedgerBucket
	}{
		{TaskStatusInProgress, slotBucketOccupied},
		{TaskStatusAwaitingAgentReview, slotBucketOccupied},
		{TaskStatusAwaitingOwnerReview, slotBucketAwaitingOwner},
		{TaskStatusBlocked, slotBucketBlocked},
		{"blocked-on:secret:X", slotBucketBlocked},
		{TaskStatusPending, slotBucketPending},
		{TaskStatusCompleted, slotBucketTerminal},
		{TaskStatusCancelled, slotBucketTerminal},
		{"legacy-unknown", slotBucketTerminal},
	}
	for _, c := range cases {
		if got := bucketForStatus(c.status); got != c.want {
			t.Errorf("bucketForStatus(%q) = %q, want %q", c.status, got, c.want)
		}
	}
}

// TestComputeSlotLedger covers occupancy tallying, the §3.4.3 blocked bucket,
// and the derived Available field including the over-capacity floor at zero.
func TestComputeSlotLedger(t *testing.T) {
	statuses := []string{
		TaskStatusInProgress,          // occupied
		TaskStatusAwaitingAgentReview, // occupied
		TaskStatusAwaitingOwnerReview, // awaiting_owner (freed)
		TaskStatusBlocked,             // blocked
		"blocked-on:task:p/t",         // blocked
		TaskStatusPending,             // pending
		TaskStatusCompleted,           // terminal
		TaskStatusCancelled,           // terminal
	}
	got := computeSlotLedger(statuses, 7)
	want := SlotLedger{
		Occupied:      2,
		AwaitingOwner: 1,
		Blocked:       2,
		Pending:       1,
		Terminal:      2,
		MaxParallel:   7,
		Available:     5,
	}
	if got != want {
		t.Fatalf("computeSlotLedger = %+v, want %+v", got, want)
	}
}

// TestComputeSlotLedger_AvailableFloor asserts Available never goes negative
// when occupancy exceeds max_parallel_tasks (over-subscription edge).
func TestComputeSlotLedger_AvailableFloor(t *testing.T) {
	statuses := []string{TaskStatusInProgress, TaskStatusInProgress, TaskStatusInProgress}
	got := computeSlotLedger(statuses, 2)
	if got.Occupied != 3 {
		t.Fatalf("Occupied = %d, want 3", got.Occupied)
	}
	if got.Available != 0 {
		t.Fatalf("Available = %d, want 0 (floored)", got.Available)
	}
}

// TestResolveMaxParallelTasks covers both the configured-preference path and
// the capacity-aware default fall-through (design.md §2.9).
func TestResolveMaxParallelTasks(t *testing.T) {
	configured := 3
	if got := resolveMaxParallelTasks(WorkflowPreferences{Execution: WorkflowExecutionPrefs{MaxParallelWorkers: &configured}}); got != 3 {
		t.Fatalf("configured resolveMaxParallelTasks = %d, want 3", got)
	}
	if got := resolveMaxParallelTasks(WorkflowPreferences{}); got != defaultMaxParallelTasks() {
		t.Fatalf("default resolveMaxParallelTasks = %d, want %d", got, defaultMaxParallelTasks())
	}
}

// TestClampParallelTasks pins the capacity-derivation clamp at its boundaries so
// the slot budget stays in [minMaxParallelTasks, maxMaxParallelTasks] regardless
// of host core count.
func TestClampParallelTasks(t *testing.T) {
	cases := []struct{ in, want int }{
		{-4, minMaxParallelTasks},
		{0, minMaxParallelTasks},
		{minMaxParallelTasks, minMaxParallelTasks},
		{minMaxParallelTasks + 1, minMaxParallelTasks + 1},
		{maxMaxParallelTasks, maxMaxParallelTasks},
		{maxMaxParallelTasks + 5, maxMaxParallelTasks},
	}
	for _, c := range cases {
		if got := clampParallelTasks(c.in); got != c.want {
			t.Errorf("clampParallelTasks(%d) = %d, want %d", c.in, got, c.want)
		}
	}
	if got := defaultMaxParallelTasks(); got < minMaxParallelTasks || got > maxMaxParallelTasks {
		t.Errorf("defaultMaxParallelTasks() = %d, want within [%d,%d]", got, minMaxParallelTasks, maxMaxParallelTasks)
	}
}

// TestRenderSlotLedger_AllBlockedNote asserts the pathology note fires only
// when slots are fully occupied while tasks sit blocked (§3.4.3 visibility).
func TestRenderSlotLedger_AllBlockedNote(t *testing.T) {
	withNote := captureStdoutToString(t, func() {
		renderSlotLedger(SlotLedger{Occupied: 2, MaxParallel: 2, Available: 0, Blocked: 1})
	})
	if !strings.Contains(withNote, "orchestrator attention warranted") {
		t.Fatalf("expected all-blocked note, got:\n%s", withNote)
	}
	noNote := captureStdoutToString(t, func() {
		renderSlotLedger(SlotLedger{Occupied: 1, MaxParallel: 4, Available: 3, Blocked: 1})
	})
	if strings.Contains(noNote, "orchestrator attention warranted") {
		t.Fatalf("did not expect all-blocked note, got:\n%s", noNote)
	}
	if !strings.Contains(noNote, "Slot Ledger") || !strings.Contains(noNote, "blocked:") {
		t.Fatalf("expected ledger body, got:\n%s", noNote)
	}
}

// TestEmitSlotLedgerJSON asserts the JSON shape carries every bucket field.
func TestEmitSlotLedgerJSON(t *testing.T) {
	out := captureStdoutToString(t, func() {
		if err := emitSlotLedgerJSON(SlotLedger{Occupied: 1, AwaitingOwner: 2, Blocked: 3, Pending: 4, Terminal: 5, MaxParallel: 7, Available: 6}); err != nil {
			t.Fatalf("emitSlotLedgerJSON: %v", err)
		}
	})
	for _, want := range []string{`"occupied": 1`, `"awaiting_owner": 2`, `"blocked": 3`, `"pending": 4`, `"terminal": 5`, `"max_parallel": 7`, `"available": 6`} {
		if !strings.Contains(out, want) {
			t.Errorf("JSON missing %q in:\n%s", want, out)
		}
	}
}

// seedSlotsPlan writes an active plan with the given task statuses for runner
// and collector tests.
func seedSlotsPlan(t *testing.T, repo, planID string, statuses ...string) {
	t.Helper()
	tasks := make([]CanonicalTask, len(statuses))
	for i, s := range statuses {
		tasks[i] = CanonicalTask{ID: planID + "-t" + string(rune('a'+i)), Title: "T", Status: s}
	}
	if err := saveCanonicalPlan(repo, &CanonicalPlan{SchemaVersion: 1, ID: planID, Title: planID, Status: "active"}); err != nil {
		t.Fatal(err)
	}
	if err := saveCanonicalTasks(repo, &CanonicalTaskFile{SchemaVersion: 1, PlanID: planID, Tasks: tasks}); err != nil {
		t.Fatal(err)
	}
}

// TestCollectTaskStatuses_ActivePlanOnly asserts collection gathers every
// active-plan task status and skips an inactive plan.
func TestCollectTaskStatuses_ActivePlanOnly(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	seedSlotsPlan(t, repo, "alpha", TaskStatusInProgress, TaskStatusAwaitingOwnerReview)
	// Inactive plan: tasks must be excluded.
	if err := saveCanonicalPlan(repo, &CanonicalPlan{SchemaVersion: 1, ID: "beta", Title: "beta", Status: "draft"}); err != nil {
		t.Fatal(err)
	}
	if err := saveCanonicalTasks(repo, &CanonicalTaskFile{SchemaVersion: 1, PlanID: "beta", Tasks: []CanonicalTask{{ID: "b1", Status: TaskStatusInProgress}}}); err != nil {
		t.Fatal(err)
	}

	statuses, err := collectTaskStatuses(repo, nil)
	if err != nil {
		t.Fatalf("collectTaskStatuses: %v", err)
	}
	if len(statuses) != 2 {
		t.Fatalf("expected 2 statuses from active plan, got %d: %v", len(statuses), statuses)
	}
}

// TestCollectTaskStatuses_PlanFilterError asserts an unknown plan filter is a
// hard error (negative path).
func TestCollectTaskStatuses_PlanFilterError(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	seedSlotsPlan(t, repo, "alpha", TaskStatusInProgress)
	if _, err := collectTaskStatuses(repo, []string{"does-not-exist"}); err == nil {
		t.Fatal("expected error for unknown plan filter")
	}
}

// TestPlanTaskStatuses_MissingPlan asserts a missing/unloadable plan yields
// (nil, nil) rather than an error (legitimate-absence skip).
func TestPlanTaskStatuses_MissingPlan(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	got, err := planTaskStatuses(repo, "nope")
	if err != nil {
		t.Fatalf("expected no error for a missing plan, got %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for missing plan, got %v", got)
	}
}

// TestPlanTaskStatuses_MalformedPlan covers the loadCanonicalPlan real-error
// branch (corrupt PLAN.yaml itself, not TASKS.yaml) — must also error rather
// than silently returning nil.
func TestPlanTaskStatuses_MalformedPlan(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	planDir := filepath.Join(plansBaseDir(repo), "brokenplan")
	if err := os.MkdirAll(planDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(planDir, workflowPlanFileName), []byte("status: [this is not valid yaml"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := planTaskStatuses(repo, "brokenplan")
	if err == nil {
		t.Fatalf("expected an error for malformed PLAN.yaml, got nil (statuses=%v)", got)
	}
	if got != nil {
		t.Fatalf("expected nil statuses alongside the error, got %v", got)
	}
}

// TestPlanTaskStatuses_ActivePlanNoTasksFile covers the legitimate-absence
// case where an active plan has no TASKS.yaml yet — must stay (nil, nil),
// not an error.
func TestPlanTaskStatuses_ActivePlanNoTasksFile(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	if err := saveCanonicalPlan(repo, &CanonicalPlan{SchemaVersion: 1, ID: "notasksyet", Title: "notasksyet", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	got, err := planTaskStatuses(repo, "notasksyet")
	if err != nil {
		t.Fatalf("expected no error for a legitimately-absent TASKS.yaml, got %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil statuses, got %v", got)
	}
}

// TestPlanTaskStatuses_MalformedTasks asserts an active plan with an
// unparseable TASKS.yaml is a REAL error (should-be-ATOMIC), not a silent
// nil — a corrupt TASKS.yaml must abort the ledger instead of silently
// understating in-flight concurrency (see planTaskStatuses doc comment).
func TestPlanTaskStatuses_MalformedTasks(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	if err := saveCanonicalPlan(repo, &CanonicalPlan{SchemaVersion: 1, ID: "broken", Title: "broken", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	tasksPath := filepath.Join(plansBaseDir(repo), "broken", workflowTasksFileName)
	if err := os.WriteFile(tasksPath, []byte("this: : not: valid: yaml: ["), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := planTaskStatuses(repo, "broken")
	if err == nil {
		t.Fatalf("expected an error for malformed TASKS.yaml, got nil (statuses=%v)", got)
	}
	if got != nil {
		t.Fatalf("expected nil statuses alongside the error, got %v", got)
	}
}

// TestCollectTaskStatuses_MalformedTasksAborts asserts the malformed-TASKS
// error from planTaskStatuses propagates through collectTaskStatuses,
// aborting the whole ledger computation rather than silently omitting the
// corrupt plan's tasks from the occupied-slot count.
func TestCollectTaskStatuses_MalformedTasksAborts(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	seedSlotsPlan(t, repo, "alpha", TaskStatusInProgress)
	if err := saveCanonicalPlan(repo, &CanonicalPlan{SchemaVersion: 1, ID: "broken", Title: "broken", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	tasksPath := filepath.Join(plansBaseDir(repo), "broken", workflowTasksFileName)
	if err := os.WriteFile(tasksPath, []byte("this: : not: valid: yaml: ["), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := collectTaskStatuses(repo, nil); err == nil {
		t.Fatal("expected collectTaskStatuses to abort on a corrupt TASKS.yaml, got nil error")
	}
}

// TestRunWorkflowSlots_Text drives the full runner text path against a seeded
// project and asserts the rendered ledger.
func TestRunWorkflowSlots_Text(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	seedSlotsPlan(t, repo, "alpha",
		TaskStatusInProgress,
		TaskStatusAwaitingAgentReview,
		TaskStatusAwaitingOwnerReview,
		TaskStatusBlocked,
	)
	captureStdoutWhileRunning(t, repo, func() error { return runWorkflowSlots("") },
		"Slot Ledger", "occupied:", "blocked:", "awaiting_owner:")
}

// TestRunWorkflowSlots_JSON drives the runner --json branch.
func TestRunWorkflowSlots_JSON(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	seedSlotsPlan(t, repo, "alpha", TaskStatusInProgress, TaskStatusAwaitingOwnerReview)

	priorJSON := deps.Flags.JSON
	deps.Flags.JSON = func() bool { return true }
	t.Cleanup(func() { deps.Flags.JSON = priorJSON })

	captureStdoutWhileRunning(t, repo, func() error { return runWorkflowSlots("alpha") },
		`"occupied": 1`, `"awaiting_owner": 1`, `"max_parallel"`)
}

// TestRunWorkflowSlots_PlanFilterError asserts the runner surfaces an unknown
// plan-filter error.
func TestRunWorkflowSlots_PlanFilterError(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	seedSlotsPlan(t, repo, "alpha", TaskStatusInProgress)
	chdirForTest(t, repo)
	if err := runWorkflowSlots("missing-plan"); err == nil {
		t.Fatal("expected plan-filter error from runner")
	}
}

// TestEligibilityUnblocksOnAwaitingOwnerReview is the §9 done-criterion #1
// end-to-end check: a downstream dep on an upstream task becomes satisfied the
// moment the upstream flips to awaiting_owner_review, and NOT while the upstream
// is in_progress or awaiting_agent_review. Drives the dep-satisfaction wiring in
// incompleteCanonicalDependenciesCrossplan via depSatisfiesDownstream.
func TestEligibilityUnblocksOnAwaitingOwnerReview(t *testing.T) {
	cases := []struct {
		upstream      string
		wantSatisfied bool
	}{
		{TaskStatusInProgress, false},
		{TaskStatusAwaitingAgentReview, false},
		{TaskStatusAwaitingOwnerReview, true},
		{TaskStatusCompleted, true},
		{TaskStatusBlocked, false},
	}
	for _, c := range cases {
		t.Run(c.upstream, func(t *testing.T) {
			tasks := []CanonicalTask{
				{ID: "up", Status: c.upstream},
				{ID: "down", Status: TaskStatusPending, DependsOn: []string{"up"}},
			}
			incomplete := incompleteCanonicalDependenciesCrossplan(t.TempDir(), tasks, []string{"up"}, nil)
			satisfied := len(incomplete) == 0
			if satisfied != c.wantSatisfied {
				t.Fatalf("upstream %q: dep satisfied=%v, want %v (incomplete=%v)", c.upstream, satisfied, c.wantSatisfied, incomplete)
			}
		})
	}
}

// TestIncompleteCanonicalDependencies_IntraPlan covers the intra-plan
// (non-crossplan) dep helper with the same §3.4.6 predicate.
func TestIncompleteCanonicalDependencies_IntraPlan(t *testing.T) {
	tasks := []CanonicalTask{
		{ID: "a", Status: TaskStatusAwaitingOwnerReview},
		{ID: "b", Status: TaskStatusInProgress},
	}
	got := incompleteCanonicalDependencies(tasks, []string{"a", "b"})
	if len(got) != 1 || got[0] != "b" {
		t.Fatalf("expected only in_progress dep 'b' incomplete, got %v", got)
	}
}
