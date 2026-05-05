package workflow

import (
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
	if bundle.Verification.EvidencePolicy == nil || bundle.Verification.EvidencePolicy.PrimaryChainMax == nil || *bundle.Verification.EvidencePolicy.PrimaryChainMax != 4 {
		t.Fatalf("expected primary_chain_max 4, got %+v", bundle.Verification.EvidencePolicy)
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
	if !strings.Contains(err.Error(), "invalid verification status") {
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
