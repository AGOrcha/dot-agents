package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// TestWorkflowLifecycle_PlanToCloseout exercises the full happy-path workflow
// lifecycle end-to-end on a single synthetic project: plan create → task add
// (with a dependency) → eligible filter → fanout → bundle inspection →
// merge-back → verify record (test + review) → checkpoint →
// delegation closeout → re-check eligible after dependency unblock.
//
// The test isolates filesystem state to t.TempDir() (HOME, AGENTS_HOME,
// project root) and exercises drift between TASKS.yaml, the delegation
// contract, the bundle, the merge-back artifact, and the closeout archive
// in a single run.
// lifecycleE2EContext carries the shared identifiers and paths threaded
// through the lifecycle steps below. Splitting each numbered step into its
// own helper preserves the exact sequence, assertions, and intent of the
// original monolithic test while keeping each function's cognitive
// complexity low.
type lifecycleE2EContext struct {
	repo       string
	agentsHome string
	planID     string
	taskAID    string
	taskBID    string
	owner      string
	projName   string
}

func TestWorkflowLifecycle_PlanToCloseout(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	fakeHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", fakeHome)

	oldwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(oldwd) })
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	c := lifecycleE2EContext{
		repo:       repo,
		agentsHome: agentsHome,
		planID:     "test-plan",
		taskAID:    "task-a",
		taskBID:    "task-b",
		owner:      "test-worker",
		projName:   "workflow-proj",
	}

	lifecycleStepPlanCreate(t, c)
	lifecycleStepTasksAdd(t, c)
	lifecycleStepEligiblePreFanout(t, c)
	contract := lifecycleStepFanout(t, c)
	lifecycleStepBundleParse(t, c, contract)
	mergeBackPath := lifecycleStepMergeBack(t, c)
	lifecycleStepVerifyRecords(t, c)
	lifecycleStepCheckpoint(t, c)
	lifecycleStepCloseout(t, c, mergeBackPath)
	lifecycleStepEligiblePostCloseout(t, c)
}

// 1. plan create + draft-hint surface + activation.
func lifecycleStepPlanCreate(t *testing.T, c lifecycleE2EContext) {
	t.Helper()
	if err := runWorkflowPlanCreate(c.planID, "Lifecycle E2E plan",
		"end-to-end synthetic plan", "dot-agents",
		"all tasks complete", "go test ./..."); err != nil {
		t.Fatalf("plan create: %v", err)
	}
	planDir := filepath.Join(c.repo, ".agents", "workflow", "plans", c.planID)
	for _, rel := range []string{"PLAN.yaml", "TASKS.yaml"} {
		if _, err := os.Stat(filepath.Join(planDir, rel)); err != nil {
			t.Fatalf("plan artifact %s missing: %v", rel, err)
		}
	}

	// Plans are created with status=draft. Before activation, the eligible
	// surface should explicitly call out the unactivated draft instead of
	// silently reporting "no tasks". Verify that hint surfaces, then promote
	// the plan and continue the lifecycle.
	captureStdoutWhileRunning(t, c.repo,
		func() error { return runWorkflowEligible("", 0) },
		"Found 1 draft plan(s) not yet activated",
		c.planID,
		"da workflow plan update --status active",
	)
	if err := runWorkflowPlanUpdate(c.planID, "active", "", "", "", "", ""); err != nil {
		t.Fatalf("plan update active: %v", err)
	}
}

// 2 + 3. task add task-a (no deps) and task-b (depends on task-a).
func lifecycleStepTasksAdd(t *testing.T, c lifecycleE2EContext) {
	t.Helper()
	if err := runWorkflowTaskAdd(taskAddInputs{
		PlanID:               c.planID,
		TaskID:               c.taskAID,
		Title:                "Implement foo",
		Notes:                "first task",
		Owner:                "dot-agents",
		WriteScope:           "commands/foo",
		VerificationRequired: true,
	}); err != nil {
		t.Fatalf("task add task-a: %v", err)
	}

	if err := runWorkflowTaskAdd(taskAddInputs{
		PlanID:               c.planID,
		TaskID:               c.taskBID,
		Title:                "Follow-up bar",
		Notes:                "second task, blocked on task-a",
		Owner:                "dot-agents",
		DependsOn:            c.taskAID,
		WriteScope:           "commands/bar",
		VerificationRequired: true,
	}); err != nil {
		t.Fatalf("task add task-b: %v", err)
	}

	tf, err := loadCanonicalTasks(c.repo, c.planID)
	if err != nil {
		t.Fatalf("load tasks after add: %v", err)
	}
	if len(tf.Tasks) != 2 {
		t.Fatalf("tasks after add = %d, want 2", len(tf.Tasks))
	}
	assertWorkflowTaskStatus(t, tf, c.taskAID, "pending")
	assertWorkflowTaskStatus(t, tf, c.taskBID, "pending")
	var taskBDeps []string
	for _, task := range tf.Tasks {
		if task.ID == c.taskBID {
			taskBDeps = task.DependsOn
		}
	}
	if len(taskBDeps) != 1 || taskBDeps[0] != c.taskAID {
		t.Fatalf("task-b depends_on = %v, want [%s]", taskBDeps, c.taskAID)
	}
}

// 4. eligible — task-b filtered out because it depends on task-a.
func lifecycleStepEligiblePreFanout(t *testing.T, c lifecycleE2EContext) {
	t.Helper()
	eligible, err := selectAllEligibleTasks(c.repo, []string{c.planID})
	if err != nil {
		t.Fatalf("select eligible (pre-fanout): %v", err)
	}
	if len(eligible) != 1 || eligible[0].TaskID != c.taskAID {
		ids := make([]string, len(eligible))
		for i, e := range eligible {
			ids[i] = e.TaskID
		}
		t.Fatalf("eligible (pre-fanout) = %v, want [%s]", ids, c.taskAID)
	}
}

// 5. fanout — returns the created delegation contract.
func lifecycleStepFanout(t *testing.T, c lifecycleE2EContext) *DelegationContract {
	t.Helper()
	if err := executeWorkflowCommand(t, c.repo,
		"fanout", "--plan", c.planID, "--task", c.taskAID,
		"--owner", c.owner, "--write-scope", "commands/foo",
		"--skip-tdd-gate", "--skip-evidence-check"); err != nil {
		t.Fatalf("fanout: %v", err)
	}

	contract, err := loadDelegationContract(c.repo, c.taskAID)
	if err != nil {
		t.Fatalf("load delegation contract: %v", err)
	}
	if contract.ParentPlanID != c.planID || contract.ParentTaskID != c.taskAID {
		t.Fatalf("contract plan/task = %s/%s, want %s/%s",
			contract.ParentPlanID, contract.ParentTaskID, c.planID, c.taskAID)
	}
	if contract.Owner != c.owner {
		t.Fatalf("contract owner = %q, want %q", contract.Owner, c.owner)
	}
	if contract.Status != "active" {
		t.Fatalf("contract status = %q, want active", contract.Status)
	}
	if !strings.HasPrefix(contract.ID, "del-"+c.taskAID+"-") {
		t.Fatalf("contract id = %q, want del-%s-* prefix", contract.ID, c.taskAID)
	}
	return contract
}

// 6. bundle parse — load and inspect.
func lifecycleStepBundleParse(t *testing.T, c lifecycleE2EContext, contract *DelegationContract) {
	t.Helper()
	bundlePath := filepath.Join(c.repo, ".agents", "active", "delegation-bundles", contract.ID+".yaml")
	if _, err := os.Stat(bundlePath); err != nil {
		t.Fatalf("bundle file missing: %v", err)
	}

	bundleData, err := os.ReadFile(bundlePath)
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}
	var bundle delegationBundleYAML
	if err := yaml.Unmarshal(bundleData, &bundle); err != nil {
		t.Fatalf("parse bundle: %v", err)
	}
	if bundle.PlanID != c.planID || bundle.TaskID != c.taskAID {
		t.Fatalf("bundle plan/task = %s/%s, want %s/%s",
			bundle.PlanID, bundle.TaskID, c.planID, c.taskAID)
	}
	if bundle.DelegationID != contract.ID {
		t.Fatalf("bundle delegation_id = %q, want %q", bundle.DelegationID, contract.ID)
	}
	if bundle.Owner != c.owner {
		t.Fatalf("bundle owner = %q, want %q", bundle.Owner, c.owner)
	}
	if len(bundle.Scope.WriteScope) == 0 || bundle.Scope.WriteScope[0] != "commands/foo" {
		t.Fatalf("bundle scope.write_scope = %v, want [commands/foo]", bundle.Scope.WriteScope)
	}
	// bundle stages should also parse without error (covers the public surface)
	if err := executeWorkflowCommand(t, c.repo, "bundle", "stages", bundlePath); err != nil {
		t.Fatalf("bundle stages: %v", err)
	}
}

// 7. merge-back — returns the active merge-back artifact path.
func lifecycleStepMergeBack(t *testing.T, c lifecycleE2EContext) string {
	t.Helper()
	if err := executeWorkflowCommand(t, c.repo,
		"merge-back", "--task", c.taskAID,
		"--summary", "delegate completed task-a slice",
		"--verification-status", "pass",
		"--integration-notes", "ready to merge"); err != nil {
		t.Fatalf("merge-back: %v", err)
	}
	mergeBackPath := filepath.Join(c.repo, ".agents", "active", "merge-back", c.taskAID+".md")
	mergeBackBytes, err := os.ReadFile(mergeBackPath)
	if err != nil {
		t.Fatalf("merge-back artifact missing: %v", err)
	}
	mergeBackText := string(mergeBackBytes)
	if !strings.HasPrefix(mergeBackText, "---") {
		t.Fatalf("merge-back %s missing frontmatter delimiter", mergeBackPath)
	}
	if !strings.Contains(mergeBackText, "task_id: "+c.taskAID) {
		t.Fatalf("merge-back missing task_id %s:\n%s", c.taskAID, mergeBackText)
	}
	if !strings.Contains(mergeBackText, "delegate completed task-a slice") {
		t.Fatalf("merge-back missing summary text:\n%s", mergeBackText)
	}
	// loadMergeBack verifies frontmatter is structurally valid
	if _, err := loadMergeBack(c.repo, c.taskAID); err != nil {
		t.Fatalf("loadMergeBack: %v", err)
	}
	return mergeBackPath
}

// 8 + 9. verify record test + review, then assert both log entries.
func lifecycleStepVerifyRecords(t *testing.T, c lifecycleE2EContext) {
	t.Helper()
	if err := runWorkflowVerifyRecord(verifyRecordInputs{
		Kind:    "test",
		Status:  "pass",
		Command: "go test ./...",
		Scope:   "repo",
		Summary: "all tests green for task-a",
	}); err != nil {
		t.Fatalf("verify record test: %v", err)
	}

	if err := runWorkflowVerifyRecordReview(reviewRecordInputs{
		Scope:    "repo",
		Summary:  "task-a slice LGTM",
		Phase1In: "accept",
		Phase2In: "accept",
		TaskFlag: c.taskAID,
	}); err != nil {
		t.Fatalf("verify record review: %v", err)
	}

	records, err := readVerificationLog(c.projName, 0)
	if err != nil {
		t.Fatalf("read verification log: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("verification log count = %d, want 2", len(records))
	}
	var sawTest, sawReview bool
	for _, r := range records {
		switch r.Kind {
		case "test":
			sawTest = true
			if r.Status != "pass" {
				t.Fatalf("test verification status = %q, want pass", r.Status)
			}
		case "review":
			sawReview = true
			if r.Status != "pass" {
				t.Fatalf("review verification status = %q, want pass", r.Status)
			}
		}
	}
	if !sawTest || !sawReview {
		t.Fatalf("expected both test+review entries, got %+v", records)
	}
}

// 10. checkpoint.
func lifecycleStepCheckpoint(t *testing.T, c lifecycleE2EContext) {
	t.Helper()
	if err := runWorkflowCheckpoint(
		"lifecycle E2E checkpoint", "pass", "all tests green"); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	checkpointPath := filepath.Join(c.agentsHome, "context", c.projName, "checkpoint.yaml")
	cpData, err := os.ReadFile(checkpointPath)
	if err != nil {
		t.Fatalf("checkpoint.yaml missing: %v", err)
	}
	var cp workflowCheckpoint
	if err := yaml.Unmarshal(cpData, &cp); err != nil {
		t.Fatalf("parse checkpoint.yaml: %v", err)
	}
	if cp.Verification.Status != "pass" {
		t.Fatalf("checkpoint verification status = %q, want pass", cp.Verification.Status)
	}
	if cp.Message != "lifecycle E2E checkpoint" {
		t.Fatalf("checkpoint message = %q, want %q", cp.Message, "lifecycle E2E checkpoint")
	}
}

// 11 + 12. delegation closeout, archive assertions, task-a completed.
func lifecycleStepCloseout(t *testing.T, c lifecycleE2EContext, mergeBackPath string) {
	t.Helper()
	if err := executeWorkflowCommand(t, c.repo,
		"delegation", "closeout",
		"--plan", c.planID, "--task", c.taskAID,
		"--decision", "accept"); err != nil {
		t.Fatalf("delegation closeout: %v", err)
	}
	archiveGlob := filepath.Join(c.repo, ".agents", "history", c.planID,
		"delegate-merge-back-archive", "*", c.taskAID, "merge-back.md")
	matches, err := filepath.Glob(archiveGlob)
	if err != nil {
		t.Fatalf("glob archive: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("merge-back archive matches = %v (glob %s), want 1", matches, archiveGlob)
	}
	closeoutMatches, err := filepath.Glob(filepath.Join(filepath.Dir(matches[0]), "closeout.yaml"))
	if err != nil {
		t.Fatalf("glob closeout: %v", err)
	}
	if len(closeoutMatches) != 1 {
		t.Fatalf("closeout.yaml missing in archive (matches=%v)", closeoutMatches)
	}
	if _, err := os.Stat(mergeBackPath); !os.IsNotExist(err) {
		t.Fatalf("expected active merge-back removed after closeout (err=%v)", err)
	}

	// After closeout, task-a should already be completed (closeout reconciles).
	tf, err := loadCanonicalTasks(c.repo, c.planID)
	if err != nil {
		t.Fatalf("load tasks after closeout: %v", err)
	}
	assertWorkflowTaskStatus(t, tf, c.taskAID, "completed")
}

// 13. eligible — task-b now eligible because task-a is completed.
func lifecycleStepEligiblePostCloseout(t *testing.T, c lifecycleE2EContext) {
	t.Helper()
	eligibleAfter, err := selectAllEligibleTasks(c.repo, []string{c.planID})
	if err != nil {
		t.Fatalf("select eligible (post-closeout): %v", err)
	}
	var foundB bool
	for _, e := range eligibleAfter {
		if e.TaskID == c.taskBID {
			foundB = true
			break
		}
	}
	if !foundB {
		ids := make([]string, len(eligibleAfter))
		for i, e := range eligibleAfter {
			ids[i] = e.TaskID
		}
		t.Fatalf("expected task-b eligible after closeout, got %v", ids)
	}
}
