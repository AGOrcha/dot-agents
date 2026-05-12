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

	const (
		planID   = "test-plan"
		taskAID  = "task-a"
		taskBID  = "task-b"
		owner    = "test-worker"
		projName = "workflow-proj"
	)

	// 1. plan create
	if err := runWorkflowPlanCreate(planID, "Lifecycle E2E plan",
		"end-to-end synthetic plan", "dot-agents",
		"all tasks complete", "go test ./..."); err != nil {
		t.Fatalf("plan create: %v", err)
	}
	planDir := filepath.Join(repo, ".agents", "workflow", "plans", planID)
	for _, rel := range []string{"PLAN.yaml", "TASKS.yaml"} {
		if _, err := os.Stat(filepath.Join(planDir, rel)); err != nil {
			t.Fatalf("plan artifact %s missing: %v", rel, err)
		}
	}

	// Plans are created with status=draft. Before activation, the eligible
	// surface should explicitly call out the unactivated draft instead of
	// silently reporting "no tasks". Verify that hint surfaces, then promote
	// the plan and continue the lifecycle.
	captureStdoutWhileRunning(t, repo,
		func() error { return runWorkflowEligible("", 0) },
		"Found 1 draft plan(s) not yet activated",
		planID,
		"da workflow plan update --status active",
	)
	if err := runWorkflowPlanUpdate(planID, "active", "", "", "", "", ""); err != nil {
		t.Fatalf("plan update active: %v", err)
	}

	// 2. task add task-a (no dependencies, declared write_scope)
	if err := runWorkflowTaskAdd(taskAddInputs{
		PlanID:               planID,
		TaskID:               taskAID,
		Title:                "Implement foo",
		Notes:                "first task",
		Owner:                "dot-agents",
		WriteScope:           "commands/foo",
		VerificationRequired: true,
	}); err != nil {
		t.Fatalf("task add task-a: %v", err)
	}

	// 3. task add task-b — depends on task-a (should NOT be eligible yet)
	if err := runWorkflowTaskAdd(taskAddInputs{
		PlanID:               planID,
		TaskID:               taskBID,
		Title:                "Follow-up bar",
		Notes:                "second task, blocked on task-a",
		Owner:                "dot-agents",
		DependsOn:            taskAID,
		WriteScope:           "commands/bar",
		VerificationRequired: true,
	}); err != nil {
		t.Fatalf("task add task-b: %v", err)
	}

	tf, err := loadCanonicalTasks(repo, planID)
	if err != nil {
		t.Fatalf("load tasks after add: %v", err)
	}
	if len(tf.Tasks) != 2 {
		t.Fatalf("tasks after add = %d, want 2", len(tf.Tasks))
	}
	assertWorkflowTaskStatus(t, tf, taskAID, "pending")
	assertWorkflowTaskStatus(t, tf, taskBID, "pending")
	var taskBDeps []string
	for _, task := range tf.Tasks {
		if task.ID == taskBID {
			taskBDeps = task.DependsOn
		}
	}
	if len(taskBDeps) != 1 || taskBDeps[0] != taskAID {
		t.Fatalf("task-b depends_on = %v, want [%s]", taskBDeps, taskAID)
	}

	// 4. eligible — task-b should be filtered out because it depends on task-a
	eligible, err := selectAllEligibleTasks(repo, []string{planID})
	if err != nil {
		t.Fatalf("select eligible (pre-fanout): %v", err)
	}
	if len(eligible) != 1 || eligible[0].TaskID != taskAID {
		ids := make([]string, len(eligible))
		for i, e := range eligible {
			ids[i] = e.TaskID
		}
		t.Fatalf("eligible (pre-fanout) = %v, want [%s]", ids, taskAID)
	}

	// 5. fanout — uses cobra surface because runWorkflowFanout takes *cobra.Command
	if err := executeWorkflowCommand(t, repo,
		"fanout", "--plan", planID, "--task", taskAID,
		"--owner", owner, "--write-scope", "commands/foo",
		"--skip-tdd-gate", "--skip-evidence-check"); err != nil {
		t.Fatalf("fanout: %v", err)
	}

	contract, err := loadDelegationContract(repo, taskAID)
	if err != nil {
		t.Fatalf("load delegation contract: %v", err)
	}
	if contract.ParentPlanID != planID || contract.ParentTaskID != taskAID {
		t.Fatalf("contract plan/task = %s/%s, want %s/%s",
			contract.ParentPlanID, contract.ParentTaskID, planID, taskAID)
	}
	if contract.Owner != owner {
		t.Fatalf("contract owner = %q, want %q", contract.Owner, owner)
	}
	if contract.Status != "active" {
		t.Fatalf("contract status = %q, want active", contract.Status)
	}
	if !strings.HasPrefix(contract.ID, "del-"+taskAID+"-") {
		t.Fatalf("contract id = %q, want del-%s-* prefix", contract.ID, taskAID)
	}

	bundlePath := filepath.Join(repo, ".agents", "active", "delegation-bundles", contract.ID+".yaml")
	if _, err := os.Stat(bundlePath); err != nil {
		t.Fatalf("bundle file missing: %v", err)
	}

	// 6. bundle parse — load and inspect
	bundleData, err := os.ReadFile(bundlePath)
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}
	var bundle delegationBundleYAML
	if err := yaml.Unmarshal(bundleData, &bundle); err != nil {
		t.Fatalf("parse bundle: %v", err)
	}
	if bundle.PlanID != planID || bundle.TaskID != taskAID {
		t.Fatalf("bundle plan/task = %s/%s, want %s/%s",
			bundle.PlanID, bundle.TaskID, planID, taskAID)
	}
	if bundle.DelegationID != contract.ID {
		t.Fatalf("bundle delegation_id = %q, want %q", bundle.DelegationID, contract.ID)
	}
	if bundle.Owner != owner {
		t.Fatalf("bundle owner = %q, want %q", bundle.Owner, owner)
	}
	if len(bundle.Scope.WriteScope) == 0 || bundle.Scope.WriteScope[0] != "commands/foo" {
		t.Fatalf("bundle scope.write_scope = %v, want [commands/foo]", bundle.Scope.WriteScope)
	}
	// bundle stages should also parse without error (covers the public surface)
	if err := executeWorkflowCommand(t, repo, "bundle", "stages", bundlePath); err != nil {
		t.Fatalf("bundle stages: %v", err)
	}

	// 7. merge-back — uses cobra surface (cmd takes *cobra.Command)
	if err := executeWorkflowCommand(t, repo,
		"merge-back", "--task", taskAID,
		"--summary", "delegate completed task-a slice",
		"--verification-status", "pass",
		"--integration-notes", "ready to merge"); err != nil {
		t.Fatalf("merge-back: %v", err)
	}
	mergeBackPath := filepath.Join(repo, ".agents", "active", "merge-back", taskAID+".md")
	mergeBackBytes, err := os.ReadFile(mergeBackPath)
	if err != nil {
		t.Fatalf("merge-back artifact missing: %v", err)
	}
	mergeBackText := string(mergeBackBytes)
	if !strings.HasPrefix(mergeBackText, "---") {
		t.Fatalf("merge-back %s missing frontmatter delimiter", mergeBackPath)
	}
	if !strings.Contains(mergeBackText, "task_id: "+taskAID) {
		t.Fatalf("merge-back missing task_id %s:\n%s", taskAID, mergeBackText)
	}
	if !strings.Contains(mergeBackText, "delegate completed task-a slice") {
		t.Fatalf("merge-back missing summary text:\n%s", mergeBackText)
	}
	// loadMergeBack verifies frontmatter is structurally valid
	if _, err := loadMergeBack(repo, taskAID); err != nil {
		t.Fatalf("loadMergeBack: %v", err)
	}

	// 8. verify record --kind test --status pass
	if err := runWorkflowVerifyRecord(verifyRecordInputs{
		Kind:    "test",
		Status:  "pass",
		Command: "go test ./...",
		Scope:   "repo",
		Summary: "all tests green for task-a",
	}); err != nil {
		t.Fatalf("verify record test: %v", err)
	}

	// 9. verify record --kind review (phase decisions, no --status)
	if err := runWorkflowVerifyRecordReview(reviewRecordInputs{
		Scope:    "repo",
		Summary:  "task-a slice LGTM",
		Phase1In: "accept",
		Phase2In: "accept",
		TaskFlag: taskAID,
	}); err != nil {
		t.Fatalf("verify record review: %v", err)
	}

	records, err := readVerificationLog(projName, 0)
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

	// 10. checkpoint
	if err := runWorkflowCheckpoint(
		"lifecycle E2E checkpoint", "pass", "all tests green"); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	checkpointPath := filepath.Join(agentsHome, "context", projName, "checkpoint.yaml")
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

	// 11. delegation closeout — archive merge-back and mark task-a completed
	if err := executeWorkflowCommand(t, repo,
		"delegation", "closeout",
		"--plan", planID, "--task", taskAID,
		"--decision", "accept"); err != nil {
		t.Fatalf("delegation closeout: %v", err)
	}
	archiveGlob := filepath.Join(repo, ".agents", "history", planID,
		"delegate-merge-back-archive", "*", taskAID, "merge-back.md")
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

	// 12. After closeout, task-a should already be completed (closeout reconciles).
	// Calling advance with the same status should still succeed.
	tf, err = loadCanonicalTasks(repo, planID)
	if err != nil {
		t.Fatalf("load tasks after closeout: %v", err)
	}
	assertWorkflowTaskStatus(t, tf, taskAID, "completed")

	// 13. eligible — task-b should now be eligible because task-a is completed.
	eligibleAfter, err := selectAllEligibleTasks(repo, []string{planID})
	if err != nil {
		t.Fatalf("select eligible (post-closeout): %v", err)
	}
	var foundB bool
	for _, e := range eligibleAfter {
		if e.TaskID == taskBID {
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
