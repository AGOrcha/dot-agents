package workflow

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

func loadFanoutContractAndBundle(t *testing.T, repo, taskID string) (*DelegationContract, delegationBundleYAML) {
	t.Helper()
	contract, err := loadDelegationContract(repo, taskID)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(repo, ".agents", "active", "delegation-bundles", contract.ID+".yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var bundle delegationBundleYAML
	if err := yaml.Unmarshal(data, &bundle); err != nil {
		t.Fatal(err)
	}
	return contract, bundle
}

func loadFanoutBundleYAML(t *testing.T, repo, taskID string) delegationBundleYAML {
	t.Helper()
	_, bundle := loadFanoutContractAndBundle(t, repo, taskID)
	return bundle
}

func assertFanoutContract(t *testing.T, contract *DelegationContract, owner, taskID string, wantScope []string) {
	t.Helper()
	if contract.ParentTaskID != taskID {
		t.Fatalf("parent_task_id = %q, want %s", contract.ParentTaskID, taskID)
	}
	if contract.Owner != owner {
		t.Fatalf("owner = %q, want %s", contract.Owner, owner)
	}
	if !strings.HasPrefix(contract.ID, "del-"+taskID+"-") {
		t.Fatalf("contract id = %q, want prefix del-%s-", contract.ID, taskID)
	}
	if len(contract.WriteScope) != len(wantScope) {
		t.Fatalf("write_scope = %+v, want %+v", contract.WriteScope, wantScope)
	}
	for i := range wantScope {
		if contract.WriteScope[i] != wantScope[i] {
			t.Fatalf("write_scope = %+v, want %+v", contract.WriteScope, wantScope)
		}
	}
}

func assertFanoutBundleDefaults(t *testing.T, bundle delegationBundleYAML, delegationID, planID, taskID, sliceID string) {
	t.Helper()
	if bundle.DelegationID != delegationID || bundle.PlanID != planID || bundle.TaskID != taskID {
		t.Fatalf("bundle ids: %+v", bundle)
	}
	if bundle.SliceID != sliceID {
		t.Fatalf("slice_id = %q, want %s", bundle.SliceID, sliceID)
	}
	if bundle.Worker.Profile != defaultDelegateProfile {
		t.Fatalf("profile = %q", bundle.Worker.Profile)
	}
	if bundle.Verification.FeedbackGoal == "" {
		t.Fatal("expected default feedback_goal")
	}
	if len(bundle.Closeout.WorkerMust) == 0 || len(bundle.Closeout.ParentMust) == 0 {
		t.Fatal("expected closeout defaults")
	}
	if got := bundle.Closeout.ParentMust; len(got) != 1 || got[0] != "workflow_delegation_closeout" {
		t.Fatalf("parent closeout defaults = %+v, want [workflow_delegation_closeout]", got)
	}
}

func assertDistinctFanoutBundles(t *testing.T, x1, x2 delegationBundleYAML) {
	t.Helper()
	t.Run("task-ids-correct", func(t *testing.T) {
		if x1.TaskID != "t1" || x2.TaskID != "t2" {
			t.Fatalf("task mismatch: %s / %s", x1.TaskID, x2.TaskID)
		}
	})
	t.Run("owners-distinct", func(t *testing.T) {
		if x1.Owner != "a" || x2.Owner != "b" {
			t.Fatalf("owner leak: %s / %s", x1.Owner, x2.Owner)
		}
	})
	t.Run("slice-id-only-on-slice-task", func(t *testing.T) {
		if x2.SliceID != "" {
			t.Fatalf("t2 bundle should not set slice_id, got %q", x2.SliceID)
		}
	})
}

func writeFanoutPromptFixture(t *testing.T, repo string) {
	t.Helper()
	promptPath := filepath.Join(repo, ".agents", "ctx", "prompt.md")
	if err := os.MkdirAll(filepath.Dir(promptPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(promptPath, []byte("# hi\n"), 0644); err != nil {
		t.Fatal(err)
	}
}

func assertFanoutWorkerProfile(t *testing.T, bundle delegationBundleYAML) {
	t.Helper()
	if bundle.Worker.Profile != "custom-worker" {
		t.Fatalf("profile %q", bundle.Worker.Profile)
	}
}

func assertFanoutPromptFields(t *testing.T, bundle delegationBundleYAML) {
	t.Helper()
	if len(bundle.Prompt.Inline) != 2 || bundle.Prompt.Inline[0] != "line one" {
		t.Fatalf("inline prompt: %+v", bundle.Prompt.Inline)
	}
	if len(bundle.Prompt.PromptFiles) != 1 || bundle.Prompt.PromptFiles[0] != ".agents/ctx/prompt.md" {
		t.Fatalf("prompt_files: %+v", bundle.Prompt.PromptFiles)
	}
}

func assertFanoutContextFields(t *testing.T, bundle delegationBundleYAML) {
	t.Helper()
	if len(bundle.Context.RequiredFiles) != 1 {
		t.Fatalf("context: %+v", bundle.Context.RequiredFiles)
	}
}

func assertFanoutVerificationFields(t *testing.T, bundle delegationBundleYAML) {
	t.Helper()
	if bundle.Verification.FeedbackGoal != "Prove fanout bundles persist." {
		t.Fatalf("feedback_goal %q", bundle.Verification.FeedbackGoal)
	}
	if len(bundle.Verification.ScenarioTags) != 2 {
		t.Fatalf("scenario_tags: %+v", bundle.Verification.ScenarioTags)
	}
	if len(bundle.Verification.RegressionArtifacts) != 1 || !strings.HasSuffix(bundle.Verification.RegressionArtifacts[0], "TASKS.yaml") {
		t.Fatalf("regression: %+v", bundle.Verification.RegressionArtifacts)
	}
}

func assertFanoutSelectionPolicy(t *testing.T, bundle delegationBundleYAML) {
	t.Helper()
	if bundle.Selection == nil || bundle.Selection.Reason != "integration test" {
		t.Fatalf("selection: %+v", bundle.Selection)
	}
	if bundle.Verification.EvidencePolicy == nil || bundle.Verification.EvidencePolicy.RequireNegativeCoverage == nil || !*bundle.Verification.EvidencePolicy.RequireNegativeCoverage {
		t.Fatal("expected require_negative_coverage")
	}
}

func assertFanoutPromptBundle(t *testing.T, bundle delegationBundleYAML) {
	t.Helper()
	t.Run("worker-profile", func(t *testing.T) {
		assertFanoutWorkerProfile(t, bundle)
	})
	t.Run("prompt-fields", func(t *testing.T) {
		assertFanoutPromptFields(t, bundle)
	})
	t.Run("context-fields", func(t *testing.T) {
		assertFanoutContextFields(t, bundle)
	})
	t.Run("verification-fields", func(t *testing.T) {
		assertFanoutVerificationFields(t, bundle)
	})
	t.Run("selection-policy", func(t *testing.T) {
		assertFanoutSelectionPolicy(t, bundle)
	})
}

func TestLoadSaveDelegationContract_RoundTrip(t *testing.T) {
	dir := setupTestProject(t)
	now := time.Now().UTC().Format(time.RFC3339)
	c := &DelegationContract{
		SchemaVersion: 1, ID: "del-task-001", ParentPlanID: "plan-001", ParentTaskID: "task-001",
		Title: "Do the thing", WriteScope: []string{"commands/"}, Status: "active",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := saveDelegationContract(dir, c); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := loadDelegationContract(dir, "task-001")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.ID != c.ID || loaded.Status != "active" {
		t.Errorf("round-trip mismatch: %+v", loaded)
	}
}

func TestListDelegationContracts(t *testing.T) {
	dir := setupTestProject(t)
	now := time.Now().UTC().Format(time.RFC3339)
	for _, id := range []string{"task-001", "task-002"} {
		c := &DelegationContract{
			SchemaVersion: 1, ID: "del-" + id, ParentPlanID: "plan-001", ParentTaskID: id,
			Title: id, WriteScope: []string{id + "/"}, Status: "active",
			CreatedAt: now, UpdatedAt: now,
		}
		if err := saveDelegationContract(dir, c); err != nil {
			t.Fatalf("save %s: %v", id, err)
		}
	}
	contracts, err := listDelegationContracts(dir)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(contracts) != 2 {
		t.Errorf("expected 2 contracts, got %d", len(contracts))
	}
}

func TestWriteScopeOverlaps_NoConflict(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	existing := []DelegationContract{
		{ParentTaskID: "task-001", WriteScope: []string{"commands/"}, Status: "active", CreatedAt: now, UpdatedAt: now},
	}
	conflicts := writeScopeOverlaps(existing, []string{"internal/"}, "task-002")
	if len(conflicts) != 0 {
		t.Errorf("expected no conflicts, got: %v", conflicts)
	}
}

func TestWriteScopeOverlaps_DetectsConflict(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	existing := []DelegationContract{
		{ParentTaskID: "task-001", WriteScope: []string{"commands/"}, Status: "active", CreatedAt: now, UpdatedAt: now},
	}
	// commands/workflow.go is contained within commands/ — should conflict
	conflicts := writeScopeOverlaps(existing, []string{"commands/workflow.go"}, "task-002")
	if len(conflicts) == 0 {
		t.Error("expected conflict for commands/workflow.go vs commands/")
	}
}

func TestWriteScopeOverlaps_IdenticalScope(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	existing := []DelegationContract{
		{ParentTaskID: "task-001", WriteScope: []string{"commands/"}, Status: "active", CreatedAt: now, UpdatedAt: now},
	}
	conflicts := writeScopeOverlaps(existing, []string{"commands/"}, "task-002")
	if len(conflicts) == 0 {
		t.Error("expected conflict for identical scope")
	}
}

func TestWriteScopeOverlaps_SkipsCompletedDelegation(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	existing := []DelegationContract{
		{ParentTaskID: "task-001", WriteScope: []string{"commands/"}, Status: "completed", CreatedAt: now, UpdatedAt: now},
	}
	// Completed delegation should not block new delegation with same scope
	conflicts := writeScopeOverlaps(existing, []string{"commands/"}, "task-002")
	if len(conflicts) != 0 {
		t.Errorf("completed delegation should not block, got: %v", conflicts)
	}
}

func TestFanoutFromSlice(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	if err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--slice", "s1", "--owner", "test"); err != nil {
		t.Fatal(err)
	}

	contract, bundle := loadFanoutContractAndBundle(t, repo, "t1")
	assertFanoutContract(t, contract, "test", "t1", []string{"commands/"})
	assertFanoutBundleDefaults(t, bundle, contract.ID, "p1", "t1", "s1")
}

func TestFanoutTwoTasksDistinctBundles(t *testing.T) {
	repo := setupFanoutTwoTaskProject(t)
	if err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--slice", "s1", "--owner", "a"); err != nil {
		t.Fatal(err)
	}
	if err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--task", "t2", "--owner", "b", "--write-scope", "internal/"); err != nil {
		t.Fatal(err)
	}

	c1, err := loadDelegationContract(repo, "t1")
	if err != nil {
		t.Fatal(err)
	}
	c2, err := loadDelegationContract(repo, "t2")
	if err != nil {
		t.Fatal(err)
	}

	if c1.ID == c2.ID {
		t.Fatalf("expected distinct delegation ids: %s", c1.ID)
	}

	x1 := loadFanoutBundleYAML(t, repo, "t1")
	x2 := loadFanoutBundleYAML(t, repo, "t2")
	assertDistinctFanoutBundles(t, x1, x2)
}

func TestFanoutDelegationBundlePromptAndFiles(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	writeFanoutPromptFixture(t, repo)
	err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--slice", "s1", "--owner", "w",
		"--delegate-profile", "custom-worker",
		"--prompt", "line one", "--prompt", "line two",
		"--prompt-file", ".agents/ctx/prompt.md",
		"--context-file", ".agents/ctx/prompt.md",
		"--feedback-goal", "Prove fanout bundles persist.",
		"--scenario-tag", "a", "--scenario-tag", "b",
		"--regression-artifact", ".agents/workflow/plans/p1/TASKS.yaml",
		"--selection-reason", "integration test",
		"--require-negative-coverage", "--sandbox-mutations",
	)
	if err != nil {
		t.Fatal(err)
	}
	bundle := loadFanoutBundleYAML(t, repo, "t1")
	assertFanoutPromptBundle(t, bundle)
}

func TestFanoutDelegationBundleRejectsEscapePath(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--slice", "s1", "--owner", "w",
		"--prompt-file", "../../../etc/passwd",
	)
	if err == nil {
		t.Fatal("expected error for path escape")
	}
	if !strings.Contains(err.Error(), "prompt-file") {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestFanoutSliceAndTaskMutuallyExclusive(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--slice", "s1", "--task", "t1", "--owner", "test")
	if err == nil {
		t.Fatal("expected error for mutually exclusive flags, got nil")
	}
	if !strings.Contains(err.Error(), "not both") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFanout_CreatesVerificationDir(t *testing.T) {
	repo := setupTestProject(t)
	if err := executeWorkflowCommand(t, repo, "fanout", "--plan", "plan-001", "--task", "task-001", "--owner", "w"); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Join(repo, ".agents", "active", "verification", "task-001"))
	if err != nil || !st.IsDir() {
		t.Fatalf("verification dir: %v", err)
	}
}

func TestFanout_TDDGateRejectsGoScopeWithoutTests(t *testing.T) {
	repo := setupTestProject(t)
	if err := os.MkdirAll(filepath.Join(repo, "commands"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "commands", "x.go"), []byte("package x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	tf, err := loadCanonicalTasks(repo, "plan-001")
	if err != nil {
		t.Fatal(err)
	}
	tf.Tasks[0].WriteScope = []string{"commands/x.go"}
	tf.Tasks[0].VerificationRequired = true
	if err := saveCanonicalTasks(repo, tf); err != nil {
		t.Fatal(err)
	}

	err = executeWorkflowCommand(t, repo, "fanout", "--plan", "plan-001", "--task", "task-001", "--owner", "w")
	if err == nil {
		t.Fatal("expected TDD gate error")
	}
	if !strings.Contains(err.Error(), "pre-verifier TDD gate") {
		t.Fatalf("unexpected err: %v", err)
	}
}

// seedAssertingTestGateProject writes a source symbol inside task-001's
// write_scope (commands/) and a cross-package *_test.go asserting on it OUTSIDE
// scope, so a real `da workflow fanout` run exercises the §0d gate end-to-end.
func seedAssertingTestGateProject(t *testing.T, repo string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(repo, "commands"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "commands", "thing.go"),
		[]byte("package commands\n\nfunc ExportedThing() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "other"), 0755); err != nil {
		t.Fatal(err)
	}
	crossTest := "package other\n\nimport \"testing\"\n\nvar _ = ExportedThing\n\nfunc TestX(t *testing.T) {}\n"
	if err := os.WriteFile(filepath.Join(repo, "other", "x_test.go"), []byte(crossTest), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestFanout_AssertingTestGateRefusesByDefault(t *testing.T) {
	repo := setupTestProject(t)
	seedAssertingTestGateProject(t, repo)

	err := executeWorkflowCommand(t, repo, "fanout", "--plan", "plan-001", "--task", "task-001", "--owner", "w")
	if err == nil {
		t.Fatal("gate should be ON by default and REFUSE the cross-package asserter")
	}
	if !strings.Contains(err.Error(), "asserting-test scope gate") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), "ExportedThing") {
		t.Fatalf("error should name the symbol, got: %v", err)
	}
	// A refused fanout must not have created a delegation contract.
	if _, err := loadDelegationContract(repo, "task-001"); err == nil {
		t.Fatal("refused fanout must not create a delegation contract")
	}
}

func TestFanout_AssertingTestGateSkipFlagBypasses(t *testing.T) {
	repo := setupTestProject(t)
	seedAssertingTestGateProject(t, repo)

	if err := executeWorkflowCommand(t, repo, "fanout", "--plan", "plan-001", "--task", "task-001",
		"--owner", "w", "--skip-asserting-test-gate"); err != nil {
		t.Fatalf("--skip-asserting-test-gate should bypass the gate, got: %v", err)
	}
	if _, err := loadDelegationContract(repo, "task-001"); err != nil {
		t.Fatalf("expected delegation contract after bypass, got: %v", err)
	}
}

func TestFanout_VerifierRetryMaxInBundle(t *testing.T) {
	repo := setupTestProject(t)
	if err := executeWorkflowCommand(t, repo, "fanout", "--plan", "plan-001", "--task", "task-001", "--owner", "w", "--verifier-retry-max", "4"); err != nil {
		t.Fatal(err)
	}
	c, err := loadDelegationContract(repo, "task-001")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(repo, ".agents", "active", "delegation-bundles", c.ID+".yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var bundle delegationBundleYAML
	if err := yaml.Unmarshal(data, &bundle); err != nil {
		t.Fatal(err)
	}
	if bundle.Verification.EvidencePolicy == nil || bundle.Verification.EvidencePolicy.VerifierChainMax == nil || *bundle.Verification.EvidencePolicy.VerifierChainMax != 4 {
		t.Fatalf("expected verifier_chain_max 4, got %+v", bundle.Verification.EvidencePolicy)
	}
}

func TestFanout_VerifierSequenceFromAppTypeInAgentsrc(t *testing.T) {
	repo := setupVerifierDispatchProject(t, "api", "")
	if err := executeWorkflowCommand(t, repo, "fanout", "--plan", "plan-vd", "--task", "task-vd", "--owner", "w", "--skip-tdd-gate"); err != nil {
		t.Fatal(err)
	}
	b := loadFanoutBundle(t, repo, "task-vd")
	if b.Verification.AppType != "api" {
		t.Fatalf("app_type = %q, want api", b.Verification.AppType)
	}
	want := []string{"unit", "api"}
	if len(b.Verification.VerifierSequence) != len(want) {
		t.Fatalf("verifier_sequence = %#v, want %#v", b.Verification.VerifierSequence, want)
	}
	for i := range want {
		if b.Verification.VerifierSequence[i] != want[i] {
			t.Fatalf("verifier_sequence = %#v, want %#v", b.Verification.VerifierSequence, want)
		}
	}
}

func TestFanout_VerifierSequenceUsesPlanDefaultAppType(t *testing.T) {
	repo := setupVerifierDispatchProject(t, "", "api")
	if err := executeWorkflowCommand(t, repo, "fanout", "--plan", "plan-vd", "--task", "task-vd", "--owner", "w", "--skip-tdd-gate"); err != nil {
		t.Fatal(err)
	}
	b := loadFanoutBundle(t, repo, "task-vd")
	if b.Verification.AppType != "api" {
		t.Fatalf("app_type = %q, want api", b.Verification.AppType)
	}
	if len(b.Verification.VerifierSequence) != 2 {
		t.Fatalf("verifier_sequence = %#v", b.Verification.VerifierSequence)
	}
}

func TestFanout_VerifierSequenceFlagOverridesMap(t *testing.T) {
	repo := setupVerifierDispatchProject(t, "api", "")
	if err := executeWorkflowCommand(t, repo, "fanout", "--plan", "plan-vd", "--task", "task-vd", "--owner", "w",
		"--skip-tdd-gate", "--verifier-sequence", "api,unit"); err != nil {
		t.Fatal(err)
	}
	b := loadFanoutBundle(t, repo, "task-vd")
	if len(b.Verification.VerifierSequence) != 2 || b.Verification.VerifierSequence[0] != "api" || b.Verification.VerifierSequence[1] != "unit" {
		t.Fatalf("verifier_sequence = %#v, want [api unit]", b.Verification.VerifierSequence)
	}
}

func TestFanout_VerifierSequenceRejectsUnknownProfile(t *testing.T) {
	repo := setupVerifierDispatchProject(t, "api", "")
	err := executeWorkflowCommand(t, repo, "fanout", "--plan", "plan-vd", "--task", "task-vd", "--owner", "w",
		"--skip-tdd-gate", "--verifier-sequence", "unit,notdefined")
	if err == nil {
		t.Fatal("expected error for unknown verifier profile")
	}
	if !strings.Contains(err.Error(), "not defined") {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestFanoutTaskWriteScopeFallback(t *testing.T) {
	// fanout --plan X --task Y without --write-scope should pull write_scope from task definition
	repo := setupTestProject(t)
	if err := executeWorkflowCommand(t, repo, "fanout", "--plan", "plan-001", "--task", "task-001", "--owner", "w"); err != nil {
		t.Fatal(err)
	}
	contract, err := loadDelegationContract(repo, "task-001")
	if err != nil {
		t.Fatal(err)
	}
	if len(contract.WriteScope) != 1 || contract.WriteScope[0] != "commands/" {
		t.Fatalf("write_scope = %+v, want [commands/]", contract.WriteScope)
	}
	bundleData, err := os.ReadFile(filepath.Join(repo, ".agents", "active", "delegation-bundles", contract.ID+".yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var bundle delegationBundleYAML
	if err := yaml.Unmarshal(bundleData, &bundle); err != nil {
		t.Fatal(err)
	}
	if len(bundle.Scope.WriteScope) != 1 || bundle.Scope.WriteScope[0] != "commands/" {
		t.Fatalf("bundle write_scope = %+v, want [commands/]", bundle.Scope.WriteScope)
	}
}

func TestFanoutSliceNotFound(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--slice", "missing", "--owner", "test")
	if err == nil {
		t.Fatal("expected error for missing slice, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFanoutSliceAlreadyCompleted(t *testing.T) {
	repo := setupFanoutSliceProject(t, "completed")
	err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--slice", "s1", "--owner", "test")
	if err == nil {
		t.Fatal("expected error for completed slice, got nil")
	}
	if !strings.Contains(err.Error(), "already completed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSaveLoadMergeBack_RoundTrip(t *testing.T) {
	dir := setupTestProject(t)
	s := &MergeBackSummary{
		SchemaVersion: 1, TaskID: "task-001", ParentPlanID: "plan-001",
		Title: "Do the thing", Summary: "Implemented the feature.",
		FilesChanged:       []string{"commands/workflow.go"},
		VerificationResult: MergeBackVerification{Status: "pass", Summary: "tests green"},
		IntegrationNotes:   "No conflicts expected.",
		CreatedAt:          time.Now().UTC().Format(time.RFC3339),
	}
	if err := saveMergeBack(dir, s); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := loadMergeBack(dir, "task-001")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.TaskID != "task-001" || loaded.VerificationResult.Status != "pass" {
		t.Errorf("round-trip mismatch: %+v", loaded)
	}
}

func TestValidateVerificationResultDoc_Table(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	base := VerificationResultDoc{
		SchemaVersion: 1,
		TaskID:        "t1",
		ParentPlanID:  "p1",
		VerifierType:  "unit",
		Status:        "pass",
		Summary:       "go test ./...",
		RecordedAt:    now,
	}
	t.Run("valid minimal", func(t *testing.T) {
		d := base
		if err := validateVerificationResultDoc(&d); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("invalid status", func(t *testing.T) {
		d := base
		d.Status = "green"
		if err := validateVerificationResultDoc(&d); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("invalid verifier_type pattern", func(t *testing.T) {
		d := base
		d.VerifierType = "Unit"
		if err := validateVerificationResultDoc(&d); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("missing required field", func(t *testing.T) {
		d := base
		d.ParentPlanID = ""
		if err := validateVerificationResultDoc(&d); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestMergeBack_WritesVerificationResultYAML(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	if err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--slice", "s1", "--owner", "w"); err != nil {
		t.Fatal(err)
	}
	if err := executeWorkflowCommand(t, repo, "merge-back", "--task", "t1", "--summary", "done", "--verification-status", "pass", "--integration-notes", "go test ./commands"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(repo, ".agents", "active", "verification", "t1", VerifierTypeMergeBack+".result.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read verification result: %v", err)
	}
	var got VerificationResultDoc
	if err := yaml.Unmarshal(data, &got); err != nil {
		t.Fatalf("yaml: %v", err)
	}
	if got.TaskID != "t1" || got.ParentPlanID != "p1" || got.VerifierType != VerifierTypeMergeBack {
		t.Fatalf("unexpected doc: %+v", got)
	}
	if got.Status != "pass" || !strings.Contains(got.Summary, "go test") {
		t.Fatalf("status/summary: %+v", got)
	}
	if err := validateVerificationResultDoc(&got); err != nil {
		t.Fatalf("re-validate: %v", err)
	}
}

func TestMergeBack_InvalidVerificationStatus(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	if err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--slice", "s1", "--owner", "w"); err != nil {
		t.Fatal(err)
	}
	err := executeWorkflowCommand(t, repo, "merge-back", "--task", "t1", "--summary", "done", "--verification-status", "bogus")
	if err == nil {
		t.Fatal("expected error")
	}
	// The enum PreRunE rejects the value before the runner is reached, and
	// names the vocabulary the caller should have picked from.
	if !strings.Contains(err.Error(), "--verification-status must be one of pass|fail|partial|unknown") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ── p6-fanout-dispatch: typed verifier artifact from verify record ────────────

// setupFanoutSliceDispatch creates an in_progress fanout slice project
// and runs `workflow fanout --plan p1 --slice s1 --owner w` so the
// task fixture is ready for downstream `verify record` calls.
func setupFanoutSliceDispatch(t *testing.T) string {
	t.Helper()
	repo := setupFanoutSliceProject(t, "in_progress")
	if err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--slice", "s1", "--owner", "w"); err != nil {
		t.Fatal(err)
	}
	return repo
}

func TestVerifyRecord_WritesTypedArtifact_WithTask(t *testing.T) {
	repo := setupFanoutSliceDispatch(t)
	err := executeWorkflowCommand(t, repo,
		"verify", "record",
		"--kind", "test",
		"--status", "pass",
		"--task", "t1",
		"--verifier-type", "unit",
		"--command", "go test ./...",
		"--summary", "all packages green",
	)
	if err != nil {
		t.Fatalf("verify record: %v", err)
	}
	path := filepath.Join(repo, ".agents", "active", "verification", "t1", "unit.result.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read typed artifact: %v", err)
	}
	var got VerificationResultDoc
	if err := yaml.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.VerifierType != "unit" {
		t.Fatalf("verifier_type = %q, want unit", got.VerifierType)
	}
	if got.TaskID != "t1" || got.ParentPlanID != "p1" {
		t.Fatalf("ids: task=%q plan=%q", got.TaskID, got.ParentPlanID)
	}
	if got.Status != "pass" {
		t.Fatalf("status = %q, want pass", got.Status)
	}
	if len(got.Commands) != 1 || got.Commands[0] != "go test ./..." {
		t.Fatalf("commands = %v", got.Commands)
	}
	if err := validateVerificationResultDoc(&got); err != nil {
		t.Fatalf("schema validate: %v", err)
	}
}

func TestVerifyRecord_DefaultsVerifierTypeToKind(t *testing.T) {
	repo := setupFanoutSliceDispatch(t)
	// --task provided without --verifier-type: falls back to --kind as stem
	err := executeWorkflowCommand(t, repo,
		"verify", "record",
		"--kind", "custom",
		"--status", "pass",
		"--task", "t1",
		"--summary", "custom check passed",
	)
	if err != nil {
		t.Fatalf("verify record: %v", err)
	}
	path := filepath.Join(repo, ".agents", "active", "verification", "t1", "custom.result.yaml")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected custom.result.yaml: %v", err)
	}
}

func TestVerifyRecord_NoTaskNoArtifact(t *testing.T) {
	repo := setupTestProject(t)
	err := executeWorkflowCommand(t, repo,
		"verify", "record",
		"--kind", "test",
		"--status", "pass",
		"--summary", "tests green",
	)
	if err != nil {
		t.Fatalf("verify record without --task: %v", err)
	}
	// No typed artifact should be written
	matches, _ := filepath.Glob(filepath.Join(repo, ".agents", "active", "verification", "*", "*.result.yaml"))
	if len(matches) != 0 {
		t.Fatalf("expected no typed artifacts without --task, found: %v", matches)
	}
}

func TestVerifyRecord_VerifyLogGetsArtifactEntry(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	if err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--slice", "s1", "--owner", "w"); err != nil {
		t.Fatal(err)
	}
	if err := executeWorkflowCommand(t, repo,
		"verify", "record",
		"--kind", "test", "--status", "pass",
		"--task", "t1", "--verifier-type", "unit",
		"--summary", "pass",
	); err != nil {
		t.Fatal(err)
	}
	// The log uses the project name (base dir name of the temp repo).
	projectName := filepath.Base(repo)
	records, err := readVerificationLog(projectName, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) == 0 {
		t.Fatal("expected log entry")
	}
	last := records[len(records)-1]
	if len(last.Artifacts) != 1 || !strings.Contains(last.Artifacts[0], "unit.result.yaml") {
		t.Fatalf("expected artifact path in log entry, got: %v", last.Artifacts)
	}
}

// ── Wave 6 Step 7: orient/status delegation summary ───────────────────────────

func TestCollectDelegationSummary_Empty(t *testing.T) {
	dir := setupTestProject(t)
	summary, mergebacks := collectDelegationSummary(dir)
	if summary.ActiveCount != 0 || summary.PendingIntents != 0 {
		t.Errorf("expected empty summary on fresh project: %+v", summary)
	}
	if mergebacks != 0 {
		t.Errorf("expected 0 merge-backs, got %d", mergebacks)
	}
}

func TestCollectDelegationSummary_WithActiveContracts(t *testing.T) {
	dir := setupTestProject(t)
	now := time.Now().UTC().Format(time.RFC3339)
	// One active with pending intent, one active without
	for _, tc := range []struct {
		taskID string
		intent CoordinationIntent
	}{
		{"task-001", CoordinationIntentStatusRequest},
		{"task-002", CoordinationIntentNone},
	} {
		c := &DelegationContract{
			SchemaVersion: 1, ID: "del-" + tc.taskID, ParentPlanID: "plan-001",
			ParentTaskID: tc.taskID, Title: tc.taskID, WriteScope: []string{tc.taskID + "/"},
			Status: "active", PendingIntent: tc.intent, CreatedAt: now, UpdatedAt: now,
		}
		if err := saveDelegationContract(dir, c); err != nil {
			t.Fatalf("save %s: %v", tc.taskID, err)
		}
	}
	summary, _ := collectDelegationSummary(dir)
	if summary.ActiveCount != 2 {
		t.Errorf("expected 2 active, got %d", summary.ActiveCount)
	}
	if summary.PendingIntents != 1 {
		t.Errorf("expected 1 pending intent, got %d", summary.PendingIntents)
	}
}

func TestCollectDelegationSummary_CompletedNotCounted(t *testing.T) {
	dir := setupTestProject(t)
	now := time.Now().UTC().Format(time.RFC3339)
	c := &DelegationContract{
		SchemaVersion: 1, ID: "del-task-001", ParentPlanID: "plan-001",
		ParentTaskID: "task-001", Title: "done", WriteScope: []string{"commands/"},
		Status: "completed", CreatedAt: now, UpdatedAt: now,
	}
	if err := saveDelegationContract(dir, c); err != nil {
		t.Fatalf("save: %v", err)
	}
	summary, _ := collectDelegationSummary(dir)
	if summary.ActiveCount != 0 {
		t.Errorf("completed delegation should not count as active, got %d", summary.ActiveCount)
	}
}

// ── fanout-evidence-integration: scope-evidence sidecar warnings ──────────────

// setupGraphHome creates a minimal graph home directory that satisfies
// LocalGraphAdapter.Health() AdapterAvailable == true.
func setupGraphHome(t *testing.T, projectPath string) string {
	t.Helper()
	graphHome := filepath.Join(projectPath, ".kg")
	if err := os.MkdirAll(filepath.Join(graphHome, "self"), 0755); err != nil {
		t.Fatalf("mkdir graph self: %v", err)
	}
	if err := os.WriteFile(filepath.Join(graphHome, "self", "config.yaml"), []byte("version: 1\n"), 0644); err != nil {
		t.Fatalf("write graph config: %v", err)
	}
	// Write graph-bridge.yaml so loadGraphBridgeConfig picks up the custom graphHome.
	bridgeDir := filepath.Join(projectPath, ".agents", "workflow")
	if err := os.MkdirAll(bridgeDir, 0755); err != nil {
		t.Fatalf("mkdir bridge dir: %v", err)
	}
	bridgeCfg := "schema_version: 1\nenabled: true\ngraph_home: " + graphHome + "\n"
	if err := os.WriteFile(filepath.Join(bridgeDir, "graph-bridge.yaml"), []byte(bridgeCfg), 0644); err != nil {
		t.Fatalf("write graph-bridge.yaml: %v", err)
	}
	return graphHome
}

// captureStderr redirects os.Stderr to a pipe, calls fn, then returns the captured text.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	old := os.Stderr
	os.Stderr = w
	fn()
	_ = w.Close()
	os.Stderr = old
	var buf strings.Builder
	b := make([]byte, 4096)
	for {
		n, err := r.Read(b)
		if n > 0 {
			buf.Write(b[:n])
		}
		if err != nil {
			break
		}
	}
	return buf.String()
}

// TestFanoutEvidenceWarning_NoSidecarGraphHealthy asserts a warning is emitted when there
// is no sidecar and the graph adapter is available.
func TestFanoutEvidenceWarning_NoSidecarGraphHealthy(t *testing.T) {
	repo := setupTestProject(t)
	setupGraphHome(t, repo)

	got := captureStderr(t, func() {
		checkFanoutScopeEvidenceWarnings(repo, "plan-001", "task-001", false)
	})
	if !strings.Contains(got, "no scope-evidence sidecar") {
		t.Fatalf("expected no-sidecar warning in stderr, got: %q", got)
	}
	if !strings.Contains(got, "task-001") {
		t.Fatalf("expected task id in warning, got: %q", got)
	}
}

// TestFanoutEvidenceWarning_NoSidecarGraphDegraded asserts no warning is emitted when
// the graph adapter is not available (graph home absent or not configured).
func TestFanoutEvidenceWarning_NoSidecarGraphDegraded(t *testing.T) {
	repo := setupTestProject(t)
	// Write a graph-bridge.yaml pointing to a non-existent graph home so the
	// adapter reports AdapterAvailable == false regardless of the host machine state.
	bridgeDir := filepath.Join(repo, ".agents", "workflow")
	if err := os.MkdirAll(bridgeDir, 0755); err != nil {
		t.Fatalf("mkdir bridge dir: %v", err)
	}
	nonExistentHome := filepath.Join(repo, ".kg-absent")
	bridgeCfg := "schema_version: 1\nenabled: true\ngraph_home: " + nonExistentHome + "\n"
	if err := os.WriteFile(filepath.Join(bridgeDir, "graph-bridge.yaml"), []byte(bridgeCfg), 0644); err != nil {
		t.Fatalf("write graph-bridge.yaml: %v", err)
	}

	got := captureStderr(t, func() {
		checkFanoutScopeEvidenceWarnings(repo, "plan-001", "task-001", false)
	})
	if strings.Contains(got, "no scope-evidence sidecar") {
		t.Fatalf("expected no warning when graph degraded, got: %q", got)
	}
}

// writeScopeEvidenceSidecar mkdirs the evidence directory under
// .agents/workflow/plans/<planID>/evidence/ and writes a sidecar with
// the supplied confidence. Used by TestFanoutEvidenceWarning_Sidecar*
// pair tests that differ only in confidence value.
func writeScopeEvidenceSidecar(t *testing.T, repo, planID, taskID, confidence string) {
	t.Helper()
	evidenceDir := filepath.Join(repo, ".agents", "workflow", "plans", planID, "evidence")
	if err := os.MkdirAll(evidenceDir, 0755); err != nil {
		t.Fatalf("mkdir evidence: %v", err)
	}
	ev := NewScopeEvidence(planID, taskID)
	ev.Confidence = confidence
	data, err := yaml.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal sidecar: %v", err)
	}
	sidecarPath := deriveScopeEvidencePath(repo, planID, taskID)
	if err := os.WriteFile(sidecarPath, data, 0644); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}
}

// TestFanoutEvidenceWarning_SidecarLowConfidence asserts a warning is emitted when a
// sidecar exists but its confidence is "low".
func TestFanoutEvidenceWarning_SidecarLowConfidence(t *testing.T) {
	repo := setupTestProject(t)
	writeScopeEvidenceSidecar(t, repo, "plan-001", "task-001", "low")

	got := captureStderr(t, func() {
		checkFanoutScopeEvidenceWarnings(repo, "plan-001", "task-001", false)
	})
	if !strings.Contains(got, "confidence is low") {
		t.Fatalf("expected low-confidence warning in stderr, got: %q", got)
	}
	if !strings.Contains(got, "task-001") {
		t.Fatalf("expected task id in warning, got: %q", got)
	}
}

// TestFanoutEvidenceWarning_SidecarHighConfidence asserts no warning when sidecar has
// confidence != "low" (negative path for the low-confidence warning).
func TestFanoutEvidenceWarning_SidecarHighConfidence(t *testing.T) {
	repo := setupTestProject(t)
	writeScopeEvidenceSidecar(t, repo, "plan-001", "task-001", "high")

	got := captureStderr(t, func() {
		checkFanoutScopeEvidenceWarnings(repo, "plan-001", "task-001", false)
	})
	if got != "" {
		t.Fatalf("expected no warning for high confidence sidecar, got: %q", got)
	}
}

// TestFanoutEvidenceWarning_SkipCheckSuppressesNoSidecar asserts --skip-evidence-check
// suppresses the no-sidecar warning even when graph is healthy.
func TestFanoutEvidenceWarning_SkipCheckSuppressesNoSidecar(t *testing.T) {
	repo := setupTestProject(t)
	setupGraphHome(t, repo)

	got := captureStderr(t, func() {
		checkFanoutScopeEvidenceWarnings(repo, "plan-001", "task-001", true /* skip */)
	})
	if got != "" {
		t.Fatalf("expected no warning when skip=true, got: %q", got)
	}
}

// TestFanoutEvidenceWarning_SkipCheckSuppressesLowConfidence asserts --skip-evidence-check
// suppresses the low-confidence warning.
func TestFanoutEvidenceWarning_SkipCheckSuppressesLowConfidence(t *testing.T) {
	repo := setupTestProject(t)

	evidenceDir := filepath.Join(repo, ".agents", "workflow", "plans", "plan-001", "evidence")
	if err := os.MkdirAll(evidenceDir, 0755); err != nil {
		t.Fatalf("mkdir evidence: %v", err)
	}
	ev := NewScopeEvidence("plan-001", "task-001")
	ev.Confidence = "low"
	data, err := yaml.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal sidecar: %v", err)
	}
	if err := os.WriteFile(deriveScopeEvidencePath(repo, "plan-001", "task-001"), data, 0644); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}

	got := captureStderr(t, func() {
		checkFanoutScopeEvidenceWarnings(repo, "plan-001", "task-001", true /* skip */)
	})
	if got != "" {
		t.Fatalf("expected no warning when skip=true, got: %q", got)
	}
}

// ── Wave 7: Drift & Sweep ─────────────────────────────────────────────────────

// TestLoadMergeBack_MissingFrontmatter ensures loadMergeBack returns a clear
// error when the file has no leading `---\n` block.
func TestLoadMergeBack_MissingFrontmatter(t *testing.T) {
	t.Parallel()
	repo := setupTestProject(t)
	dir := mergeBackDir(repo)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "t1.md"), []byte("just a body\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadMergeBack(repo, "t1"); err == nil || !strings.Contains(err.Error(), "missing frontmatter") {
		t.Fatalf("expected missing frontmatter error, got %v", err)
	}
}

// TestLoadMergeBack_UnterminatedFrontmatter ensures unterminated `---` blocks
// surface as a parse error.
func TestLoadMergeBack_UnterminatedFrontmatter(t *testing.T) {
	t.Parallel()
	repo := setupTestProject(t)
	dir := mergeBackDir(repo)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "t1.md"), []byte("---\ntask_id: t1\nno close\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadMergeBack(repo, "t1"); err == nil || !strings.Contains(err.Error(), "unterminated frontmatter") {
		t.Fatalf("expected unterminated frontmatter error, got %v", err)
	}
}

// TestLoadMergeBack_ParseError ensures malformed YAML inside the frontmatter
// is reported as a parse error.
func TestLoadMergeBack_ParseError(t *testing.T) {
	t.Parallel()
	repo := setupTestProject(t)
	dir := mergeBackDir(repo)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	body := "---\n\t:bogus yaml\n---\n\n## Summary\n\nbody\n"
	if err := os.WriteFile(filepath.Join(dir, "t1.md"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadMergeBack(repo, "t1"); err == nil {
		t.Fatal("expected parse error")
	}
}

// TestSaveMergeBack_Format checks the on-disk shape: frontmatter delimiters
// and the two ## sections (Summary / Integration Notes).
func TestSaveMergeBack_Format(t *testing.T) {
	t.Parallel()
	repo := setupTestProject(t)
	s := &MergeBackSummary{
		SchemaVersion:      1,
		TaskID:             "t1",
		ParentPlanID:       "p1",
		Title:              "Title",
		Summary:            "summary text",
		FilesChanged:       []string{"a.go"},
		VerificationResult: MergeBackVerification{Status: "pass", Summary: "ok"},
		IntegrationNotes:   "integration body",
		CreatedAt:          time.Now().UTC().Format(time.RFC3339),
	}
	if err := saveMergeBack(repo, s); err != nil {
		t.Fatalf("save: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(mergeBackDir(repo), "t1.md"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{"---\n", "## Summary", "summary text", "## Integration Notes", "integration body"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in merge-back body, got:\n%s", want, got)
		}
	}
}

// TestValidateFoldBackSlug_TableDriven exercises the allowed character set,
// length cap, and edge boundaries for slug validation.
func TestValidateFoldBackSlug_TableDriven(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		slug    string
		wantErr bool
	}{
		{"valid_lower", "abc-123", false},
		{"valid_underscore", "abc_def", false},
		{"valid_mixed_case", "AbcDef", false},
		{"empty", "", true},
		{"whitespace_only", "   ", true},
		{"leading_dash", "-abc", true},
		{"trailing_dash", "abc-", true},
		{"invalid_dot", "abc.def", true},
		{"invalid_slash", "abc/def", true},
		{"invalid_space_inside", "abc def", true},
		{"too_long", strings.Repeat("a", 201), true},
		{"max_length", strings.Repeat("a", 200), false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateFoldBackSlug(tc.slug)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tc.slug)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err for %q: %v", tc.slug, err)
			}
		})
	}
}

// TestAppendFoldBackBullet covers empty/non-empty notes and trailing newline
// trimming.
func TestAppendFoldBackBullet(t *testing.T) {
	t.Parallel()
	if got := appendFoldBackBullet("", "first"); got != "- first" {
		t.Fatalf("empty notes: got %q", got)
	}
	if got := appendFoldBackBullet("- existing\n", "next"); got != "- existing\n- next" {
		t.Fatalf("non-empty: got %q", got)
	}
	if got := appendFoldBackBullet("- a\n- b\n\n", "c"); got != "- a\n- b\n- c" {
		t.Fatalf("trailing newlines: got %q", got)
	}
}

// TestSetFoldBackTaggedNote covers append + dedupe behavior for tagged lines.
func TestSetFoldBackTaggedNote(t *testing.T) {
	t.Parallel()
	got := setFoldBackTaggedNote("", "slug-x", "obs one")
	want := "- (fb:slug-x) obs one"
	if got != want {
		t.Fatalf("empty: got %q want %q", got, want)
	}

	// existing untagged note preserved, tagged note appended
	got = setFoldBackTaggedNote("- pre-existing\n", "slug-x", "second")
	if !strings.Contains(got, "- pre-existing") || !strings.HasSuffix(got, "- (fb:slug-x) second") {
		t.Fatalf("append: got %q", got)
	}

	// re-running with same slug replaces previous tagged line in place
	got = setFoldBackTaggedNote(got, "slug-x", "third")
	if strings.Count(got, "(fb:slug-x)") != 1 {
		t.Fatalf("expected exactly one tagged line, got: %q", got)
	}
	if !strings.HasSuffix(got, "- (fb:slug-x) third") {
		t.Fatalf("expected refreshed tag, got %q", got)
	}
	if !strings.Contains(got, "- pre-existing") {
		t.Fatalf("expected untagged line preserved, got %q", got)
	}
}

// TestSetFoldBackTaggedNote_StrippedObservation ensures leading/trailing
// whitespace in the observation is normalized.
func TestSetFoldBackTaggedNote_StrippedObservation(t *testing.T) {
	t.Parallel()
	got := setFoldBackTaggedNote("", "s", "  spaced  ")
	if got != "- (fb:s) spaced" {
		t.Fatalf("expected trimmed observation, got %q", got)
	}
}

// TestScopePathsOverlap covers identical / prefix / disjoint / sibling cases
// used by writeScopeOverlaps for conflict detection.
func TestScopePathsOverlap(t *testing.T) {
	t.Parallel()
	cases := []struct {
		a, b string
		want bool
	}{
		{"commands/", "commands/", true},
		{"commands", "commands/workflow.go", true},
		{"commands/workflow.go", "commands", true},
		{"commands", "commands_extra", false},
		{"internal/", "commands/", false},
		{"a/b", "a/b/c", true},
		{"a/b", "a/bc", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.a+"_vs_"+tc.b, func(t *testing.T) {
			t.Parallel()
			if got := scopePathsOverlap(tc.a, tc.b); got != tc.want {
				t.Fatalf("scopePathsOverlap(%q,%q) = %t, want %t", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

// TestWriteScopeOverlaps_ExcludesSelf ensures the excludeTaskID is honored:
// a delegation does not conflict with itself when updated in place.
func TestWriteScopeOverlaps_ExcludesSelf(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Format(time.RFC3339)
	existing := []DelegationContract{
		{ParentTaskID: "task-001", WriteScope: []string{"commands/"}, Status: "active", CreatedAt: now, UpdatedAt: now},
	}
	conflicts := writeScopeOverlaps(existing, []string{"commands/"}, "task-001")
	if len(conflicts) != 0 {
		t.Errorf("self exclusion failed: %v", conflicts)
	}
}

// TestIsValidDelegationStatus checks the status whitelist.
func TestIsValidDelegationStatus(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"pending", "active", "completed", "failed", "cancelled"} {
		if !isValidDelegationStatus(s) {
			t.Errorf("%q should be valid", s)
		}
	}
	for _, s := range []string{"", "in_progress", "done", "open"} {
		if isValidDelegationStatus(s) {
			t.Errorf("%q should be invalid", s)
		}
	}
}

// TestSaveDelegationContract_UpdatesUpdatedAt verifies saveDelegationContract
// stamps a fresh UpdatedAt on each save (used by status transitions).
func TestSaveDelegationContract_UpdatesUpdatedAt(t *testing.T) {
	t.Parallel()
	repo := setupTestProject(t)
	c := &DelegationContract{
		SchemaVersion: 1, ID: "del-x", ParentPlanID: "p1", ParentTaskID: "x",
		Title: "x", WriteScope: []string{"commands/"}, Status: "active",
		CreatedAt: "2026-04-10T10:00:00Z", UpdatedAt: "2026-04-10T10:00:00Z",
	}
	if err := saveDelegationContract(repo, c); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadDelegationContract(repo, "x")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.UpdatedAt == "2026-04-10T10:00:00Z" {
		t.Fatal("expected UpdatedAt to be refreshed on save")
	}
}

// TestListDelegationContracts_EmptyDirReturnsNil exercises the missing-dir
// short circuit.
func TestListDelegationContracts_EmptyDirReturnsNil(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	got, err := listDelegationContracts(repo)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty slice, got %d", len(got))
	}
}

func TestFanout_SkipEvidenceCheck(t *testing.T) {
	repo := setupTestProject(t)
	setupGraphHome(t, repo)
	chdirRepo(t, repo)

	if err := executeWorkflowCommand(t, repo, "fanout",
		"--plan", "plan-001", "--task", "task-001", "--owner", "w", "--skip-evidence-check"); err != nil {
		t.Fatalf("expected fanout to succeed with --skip-evidence-check: %v", err)
	}
}

func TestFanout_BundleMarshalError(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)

	prev := yamlMarshal
	calls := 0
	yamlMarshal = func(v any) ([]byte, error) {
		calls++

		if calls >= 2 {
			return nil, errors.New("marshal bundle boom")
		}
		return prev(v)
	}
	t.Cleanup(func() { yamlMarshal = prev })

	err := executeWorkflowCommand(t, repo, "fanout",
		"--plan", "plan-001", "--task", "task-001", "--owner", "w")
	if err == nil {
		t.Fatal("expected marshal failure")
	}
}

func TestFanout_TasksMissingAfterPlanFound(t *testing.T) {
	repo := setupTestProject(t)

	if err := os.Remove(filepath.Join(repo, ".agents", "workflow", "plans", "plan-001", "TASKS.yaml")); err != nil {
		t.Fatal(err)
	}
	err := executeWorkflowCommand(t, repo, "fanout",
		"--plan", "plan-001", "--task", "task-001", "--owner", "w")
	if err == nil || !strings.Contains(err.Error(), "tasks for plan plan-001") {
		t.Fatalf("expected tasks-not-found, got %v", err)
	}
}

func TestFanout_SelectionReasonPropagatedToBundle(t *testing.T) {
	repo := setupTestProject(t)
	if err := executeWorkflowCommand(t, repo, "fanout",
		"--plan", "plan-001", "--task", "task-001", "--owner", "w",
		"--selection-reason", "for tests",
	); err != nil {
		t.Fatal(err)
	}
	bundle := loadFanoutBundle(t, repo, "task-001")
	if bundle.Selection == nil || bundle.Selection.Reason != "for tests" {
		t.Fatalf("selection reason missing: %+v", bundle.Selection)
	}
}

func TestFanout_VerifierRetryMax(t *testing.T) {
	repo := setupTestProject(t)
	if err := executeWorkflowCommand(t, repo, "fanout",
		"--plan", "plan-001", "--task", "task-001", "--owner", "w",
		"--verifier-retry-max", "5",
	); err != nil {
		t.Fatal(err)
	}
}

func TestFanout_SelectionReasonFromFlag(t *testing.T) {
	repo := setupTestProject(t)
	if err := executeWorkflowCommand(t, repo, "fanout",
		"--plan", "plan-001", "--task", "task-001", "--owner", "w",
		"--selection-reason", "smoke",
	); err != nil {
		t.Fatal(err)
	}
	b := loadFanoutBundle(t, repo, "task-001")
	if b.Selection == nil || b.Selection.Reason != "smoke" {
		t.Fatalf("expected selection reason 'smoke': %+v", b.Selection)
	}
}

func TestFanout_PlanNotFound(t *testing.T) {
	repo := setupTestProject(t)
	err := executeWorkflowCommand(t, repo, "fanout", "--plan", "no-such-plan", "--task", "task-001", "--owner", "w")
	if err == nil || !strings.Contains(err.Error(), "plan no-such-plan not found") {
		t.Fatalf("expected plan-not-found, got %v", err)
	}
}

func TestFanout_ExistingDelegationRejected(t *testing.T) {
	repo := setupTestProject(t)

	saveTestDelegationContract(t, repo, "task-001", "plan-001", "del-existing")

	err := executeWorkflowCommand(t, repo, "fanout", "--plan", "plan-001", "--task", "task-001", "--owner", "w")
	if err == nil || !strings.Contains(err.Error(), "already has an active delegation") {
		t.Fatalf("expected already-delegated error, got %v", err)
	}
}

func TestFanout_TaskNotFoundInPlan(t *testing.T) {
	repo := setupTestProject(t)
	err := executeWorkflowCommand(t, repo, "fanout", "--plan", "plan-001", "--task", "ghost-task", "--owner", "w")
	if err == nil {
		t.Fatal("expected task-not-found error")
	}
}

func TestFanout_WriteScopeConflictRejected(t *testing.T) {
	repo := setupFanoutTwoTaskProject(t)

	if err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--task", "t1", "--owner", "a", "--write-scope", "commands/"); err != nil {
		t.Fatal(err)
	}

	err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--task", "t2", "--owner", "b", "--write-scope", "commands/")
	if err == nil {
		t.Fatal("expected write-scope conflict error")
	}
}

func TestFanout_PromptBundleAcceptsRelativePath(t *testing.T) {
	repo := setupTestProject(t)
	promptPath := filepath.Join(repo, ".agents", "prompts")
	if err := os.MkdirAll(promptPath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(promptPath, "p.md"), []byte("# p\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := executeWorkflowCommand(t, repo, "fanout",
		"--plan", "plan-001", "--task", "task-001", "--owner", "w",
		"--prompt-file", ".agents/prompts/p.md",
	); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

func TestFanout_MkdirVerifyDirError(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)

	sentinel := errors.New("mkdir boom")
	withMkdirAllStub(t, func(string, os.FileMode) error { return sentinel })

	err := executeWorkflowCommand(t, repo, "fanout",
		"--plan", "plan-001", "--task", "task-001", "--owner", "w")
	if err == nil {
		t.Fatal("expected mkdir fault")
	}
}

func TestFanout_LensRetryMaxInBundle(t *testing.T) {
	repo := setupTestProject(t)
	if err := executeWorkflowCommand(t, repo, "fanout",
		"--plan", "plan-001", "--task", "task-001", "--owner", "w",
		"--lens-retry-max", "2",
	); err != nil {
		t.Fatal(err)
	}
	bundle := loadFanoutBundle(t, repo, "task-001")
	if bundle.Verification.EvidencePolicy == nil || bundle.Verification.EvidencePolicy.LensChainMax == nil || *bundle.Verification.EvidencePolicy.LensChainMax != 2 {
		t.Fatalf("expected lens_chain_max 2, got %+v", bundle.Verification.EvidencePolicy)
	}
	if bundle.Verification.EvidencePolicy.VerifierChainMax != nil {
		t.Fatalf("expected verifier_chain_max unset when only --lens-retry-max passed, got %+v", bundle.Verification.EvidencePolicy.VerifierChainMax)
	}
}

func TestFanout_BothRetryMaxesInBundle(t *testing.T) {
	repo := setupTestProject(t)
	if err := executeWorkflowCommand(t, repo, "fanout",
		"--plan", "plan-001", "--task", "task-001", "--owner", "w",
		"--verifier-retry-max", "3", "--lens-retry-max", "5",
	); err != nil {
		t.Fatal(err)
	}
	bundle := loadFanoutBundle(t, repo, "task-001")
	if bundle.Verification.EvidencePolicy == nil {
		t.Fatalf("expected evidence_policy, got nil")
	}
	if bundle.Verification.EvidencePolicy.VerifierChainMax == nil || *bundle.Verification.EvidencePolicy.VerifierChainMax != 3 {
		t.Fatalf("expected verifier_chain_max 3, got %+v", bundle.Verification.EvidencePolicy.VerifierChainMax)
	}
	if bundle.Verification.EvidencePolicy.LensChainMax == nil || *bundle.Verification.EvidencePolicy.LensChainMax != 5 {
		t.Fatalf("expected lens_chain_max 5, got %+v", bundle.Verification.EvidencePolicy.LensChainMax)
	}
}

func TestEvidencePolicyUnmarshal_PrimaryChainMaxDeprecationAlias(t *testing.T) {
	var captured []string
	prev := evidencePolicyDeprecationWarner
	evidencePolicyDeprecationWarner = func(format string, args ...any) {
		captured = append(captured, format)
	}
	t.Cleanup(func() { evidencePolicyDeprecationWarner = prev })

	src := []byte("primary_chain_max: 4\nsandbox_mutations: true\n")
	var p delegationEvidencePolicy
	if err := yaml.Unmarshal(src, &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.VerifierChainMax == nil || *p.VerifierChainMax != 4 {
		t.Fatalf("expected legacy primary_chain_max=4 to alias to VerifierChainMax, got %+v", p.VerifierChainMax)
	}
	if p.SandboxMutations == nil || !*p.SandboxMutations {
		t.Fatalf("expected sandbox_mutations to round-trip, got %+v", p.SandboxMutations)
	}
	if len(captured) != 1 || !strings.Contains(captured[0], "primary_chain_max is deprecated") {
		t.Fatalf("expected deprecation warning, got %#v", captured)
	}
}

func TestEvidencePolicyUnmarshal_NewKeyWinsOverDeprecatedAlias(t *testing.T) {
	prev := evidencePolicyDeprecationWarner
	evidencePolicyDeprecationWarner = func(format string, args ...any) {}
	t.Cleanup(func() { evidencePolicyDeprecationWarner = prev })

	src := []byte("verifier_chain_max: 7\nprimary_chain_max: 4\n")
	var p delegationEvidencePolicy
	if err := yaml.Unmarshal(src, &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.VerifierChainMax == nil || *p.VerifierChainMax != 7 {
		t.Fatalf("expected new verifier_chain_max=7 to win over legacy alias, got %+v", p.VerifierChainMax)
	}
}

func TestEvidencePolicyUnmarshal_NoWarningWithoutLegacyKey(t *testing.T) {
	var called int
	prev := evidencePolicyDeprecationWarner
	evidencePolicyDeprecationWarner = func(format string, args ...any) { called++ }
	t.Cleanup(func() { evidencePolicyDeprecationWarner = prev })

	src := []byte("verifier_chain_max: 2\nlens_chain_max: 3\n")
	var p delegationEvidencePolicy
	if err := yaml.Unmarshal(src, &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if called != 0 {
		t.Fatalf("expected no deprecation warning when only new keys present, got %d", called)
	}
	if p.VerifierChainMax == nil || *p.VerifierChainMax != 2 {
		t.Fatalf("expected verifier_chain_max=2, got %+v", p.VerifierChainMax)
	}
	if p.LensChainMax == nil || *p.LensChainMax != 3 {
		t.Fatalf("expected lens_chain_max=3, got %+v", p.LensChainMax)
	}
}

func TestEvidencePolicyUnmarshal_DefaultWarnerWritesToStderr(t *testing.T) {
	// Cover the default evidencePolicyDeprecationWarner closure (writes via
	// fmt.Fprintf to os.Stderr). We redirect os.Stderr to a pipe so the
	// production code path executes without polluting test output.
	origStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = origStderr })

	evidencePolicyDeprecationWarner("warning: %s\n", "deprecated")
	_ = w.Close()
	buf, _ := io.ReadAll(r)
	if !strings.Contains(string(buf), "warning: deprecated") {
		t.Fatalf("expected default warner to write to stderr, got %q", string(buf))
	}
}

func TestEvidencePolicyUnmarshal_DecodeError(t *testing.T) {
	// Feed a scalar node where a mapping is expected; value.Decode should fail
	// and UnmarshalYAML must propagate the error.
	var p delegationEvidencePolicy
	// "42" is a plain scalar; decoding into the struct alias produces an error.
	if err := yaml.Unmarshal([]byte("42\n"), &p); err == nil {
		t.Fatal("expected decode error for non-mapping yaml, got nil")
	}
}
