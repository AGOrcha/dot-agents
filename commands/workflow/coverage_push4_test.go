// Package workflow — fourth batch of coverage tests targeting various
// error and fixture branches not exercised by the seam tests.
package workflow

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ─── upsertVerifierIterLog error branches ────────────────────────────────────

// Invalid verifier type stem rejected by verificationResultFilePath.
func TestUpsertVerifierIterLog_InvalidVerifierType(t *testing.T) {
	dst := &iterLogEntry{}
	err := upsertVerifierIterLog(dst, t.TempDir(), "task-1", "BAD-TYPE")
	if err == nil {
		t.Fatal("expected error for invalid verifier type")
	}
}

// Verifier result YAML missing on disk — ReadFile error path.
func TestUpsertVerifierIterLog_ResultFileMissing(t *testing.T) {
	dst := &iterLogEntry{}
	err := upsertVerifierIterLog(dst, t.TempDir(), "task-1", "merge-back")
	if err == nil || !strings.Contains(err.Error(), "read verifier result") {
		t.Fatalf("expected read error, got %v", err)
	}
}

// Verifier result YAML present but malformed — YAML parse error path.
func TestUpsertVerifierIterLog_MalformedYAML(t *testing.T) {
	repo := t.TempDir()
	resultDir := filepath.Join(repo, ".agents", "active", "verification", "task-1")
	if err := osMkdirAll(resultDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := osWriteFile(filepath.Join(resultDir, "merge-back.result.yaml"), []byte(":\n - oops"), 0644); err != nil {
		t.Fatal(err)
	}

	dst := &iterLogEntry{}
	err := upsertVerifierIterLog(dst, repo, "task-1", "merge-back")
	if err == nil || !strings.Contains(err.Error(), "parse verifier result") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

// Verifier result fails schema validation (missing required fields).
func TestUpsertVerifierIterLog_SchemaInvalid(t *testing.T) {
	repo := t.TempDir()
	resultDir := filepath.Join(repo, ".agents", "active", "verification", "task-1")
	if err := osMkdirAll(resultDir, 0755); err != nil {
		t.Fatal(err)
	}
	// schema_version missing → schema validation fails
	if err := osWriteFile(filepath.Join(resultDir, "merge-back.result.yaml"), []byte("task_id: t1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	dst := &iterLogEntry{}
	err := upsertVerifierIterLog(dst, repo, "task-1", "merge-back")
	if err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("expected schema invalid error, got %v", err)
	}
}

// Happy path: append a new verifier entry into an empty dst.
func TestUpsertVerifierIterLog_AppendNew(t *testing.T) {
	repo := t.TempDir()
	doc := newValidVerificationResultDoc()
	if err := writeVerificationResultYAML(repo, doc); err != nil {
		t.Fatal(err)
	}

	dst := &iterLogEntry{}
	if err := upsertVerifierIterLog(dst, repo, doc.TaskID, doc.VerifierType); err != nil {
		t.Fatalf("upsertVerifierIterLog: %v", err)
	}
	if len(dst.Verifiers) != 1 || dst.Verifiers[0].Type != "merge-back" {
		t.Fatalf("verifier not appended: %+v", dst.Verifiers)
	}
	if !dst.Verifiers[0].GatePassed {
		t.Errorf("expected GatePassed=true for status=pass")
	}
}

// Replace path: an existing verifier of the same type is overwritten,
// preserving carried-over fields (TestsAdded, ScenarioTags, etc.).
func TestUpsertVerifierIterLog_ReplaceExisting(t *testing.T) {
	repo := t.TempDir()
	doc := newValidVerificationResultDoc()
	if err := writeVerificationResultYAML(repo, doc); err != nil {
		t.Fatal(err)
	}

	dst := &iterLogEntry{
		Verifiers: []iterLogVerifierEntry{
			{
				Type:           "merge-back",
				Status:         "fail",
				TestsAdded:     5,
				TestsTotalPass: 3,
				ScenarioTags:   []string{"smoke"},
				Retries:        1,
			},
			{
				Type:   "lint",
				Status: "pass",
			},
		},
	}
	if err := upsertVerifierIterLog(dst, repo, doc.TaskID, doc.VerifierType); err != nil {
		t.Fatalf("upsertVerifierIterLog: %v", err)
	}
	if len(dst.Verifiers) != 2 {
		t.Fatalf("expected 2 verifiers, got %d", len(dst.Verifiers))
	}
	got := dst.Verifiers[0]
	if got.Status != "pass" {
		t.Errorf("expected replaced status=pass, got %q", got.Status)
	}
	if got.TestsAdded != 5 {
		t.Errorf("expected TestsAdded preserved as 5, got %d", got.TestsAdded)
	}
	if got.Retries != 1 {
		t.Errorf("expected Retries preserved as 1, got %d", got.Retries)
	}
	if len(got.ScenarioTags) != 1 || got.ScenarioTags[0] != "smoke" {
		t.Errorf("expected ScenarioTags preserved, got %v", got.ScenarioTags)
	}
}

// ─── appendPlanGraphSliceDeps unknown-slice warning branch ───────────────────

func TestAppendPlanGraphSliceDeps_UnknownDep(t *testing.T) {
	graph := &workflowPlanGraph{}
	plan := &CanonicalPlan{ID: "p1"}
	slices := []CanonicalSlice{
		{ID: "s1", DependsOn: []string{"unknown-slice"}},
	}
	ids := map[string]string{"s1": "node-s1"}
	appendPlanGraphSliceDeps(graph, plan, slices, ids)
	if len(graph.Warnings) != 1 || !strings.Contains(graph.Warnings[0], "unknown slice") {
		t.Fatalf("expected one unknown-slice warning, got %+v", graph.Warnings)
	}
	if len(graph.Edges) != 0 {
		t.Errorf("expected no edges for unknown dep, got %d", len(graph.Edges))
	}
}

func TestAppendPlanGraphSliceDeps_SkipSliceMissingFromIDs(t *testing.T) {
	graph := &workflowPlanGraph{}
	plan := &CanonicalPlan{ID: "p1"}
	slices := []CanonicalSlice{
		{ID: "unknown", DependsOn: []string{"s2"}},
	}
	ids := map[string]string{}
	appendPlanGraphSliceDeps(graph, plan, slices, ids)
	if len(graph.Warnings) != 0 || len(graph.Edges) != 0 {
		t.Fatalf("expected no-op when slice not in ids map, got warnings=%v edges=%v", graph.Warnings, graph.Edges)
	}
}

// ─── appendPlanGraphTaskRelationEdges unknown-task warning branches ──────────

func TestAppendPlanGraphTaskRelationEdges_UnknownDep(t *testing.T) {
	graph := &workflowPlanGraph{}
	plan := &CanonicalPlan{ID: "p1"}
	tasks := []CanonicalTask{
		{ID: "t1", DependsOn: []string{"unknown-task"}},
	}
	ids := map[string]string{"t1": "node-t1"}
	appendPlanGraphTaskRelationEdges(graph, plan, tasks, ids)
	if len(graph.Warnings) != 1 || !strings.Contains(graph.Warnings[0], "unknown task") {
		t.Fatalf("expected unknown-task warning, got %+v", graph.Warnings)
	}
}

func TestAppendPlanGraphTaskRelationEdges_UnknownBlocks(t *testing.T) {
	graph := &workflowPlanGraph{}
	plan := &CanonicalPlan{ID: "p1"}
	tasks := []CanonicalTask{
		{ID: "t1", Blocks: []string{"unknown-task"}},
	}
	ids := map[string]string{"t1": "node-t1"}
	appendPlanGraphTaskRelationEdges(graph, plan, tasks, ids)
	if len(graph.Warnings) != 1 || !strings.Contains(graph.Warnings[0], "blocks unknown") {
		t.Fatalf("expected blocks-unknown warning, got %+v", graph.Warnings)
	}
}

// ─── runWorkflowDelegationGate text-output paths ─────────────────────────────

func TestWorkflowDelegationGate_TextOutput_Reject(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	saveTestDelegationContract(t, repo, "t1", "p1", "del-t1")
	writeMergeBackFixture(t, repo, "t1", "p1")
	writeReviewDecisionFixture(t, repo, "t1", &ReviewDecisionDoc{
		SchemaVersion:   1,
		TaskID:          "t1",
		ParentPlanID:    "p1",
		Phase1Decision:  "reject",
		Phase2Decision:  "accept",
		OverallDecision: "reject",
		FailedGates:     []string{"unit"},
		RecordedAt:      "2026-04-19T12:00:00Z",
	})

	out := executeWorkflowCommandOutput(t, repo, "delegation", "gate", "--plan", "p1", "--task", "t1")
	if !strings.Contains(out, "outcome: reject") {
		t.Fatalf("expected outcome: reject in text output, got:\n%s", out)
	}
	if !strings.Contains(out, "closeout_allowed: false") {
		t.Fatalf("expected closeout_allowed: false, got:\n%s", out)
	}
	if !strings.Contains(out, "reason:") {
		t.Fatalf("expected reason: line, got:\n%s", out)
	}
}

// ─── checkFanoutWriteScopeConflicts conflict path ────────────────────────────

func TestCheckFanoutWriteScopeConflicts_DetectsOverlap(t *testing.T) {
	repo := t.TempDir()
	// Pre-existing active contract claiming "commands/" as write scope.
	now := time.Now().UTC().Format(time.RFC3339)
	existing := &DelegationContract{
		SchemaVersion: 1, ID: "del-existing", ParentPlanID: "p1", ParentTaskID: "tx",
		Title: "x", WriteScope: []string{"commands/"}, Status: "active",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := saveDelegationContract(repo, existing); err != nil {
		t.Fatal(err)
	}

	err := checkFanoutWriteScopeConflicts(repo, []string{"commands/foo.go"}, "ty")
	if err == nil || !strings.Contains(err.Error(), "write scope overlaps") {
		t.Fatalf("expected overlap error, got %v", err)
	}
}

func TestCheckFanoutWriteScopeConflicts_SameTaskAllowed(t *testing.T) {
	repo := t.TempDir()
	now := time.Now().UTC().Format(time.RFC3339)
	existing := &DelegationContract{
		SchemaVersion: 1, ID: "del-tx", ParentPlanID: "p1", ParentTaskID: "tx",
		Title: "x", WriteScope: []string{"commands/"}, Status: "active",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := saveDelegationContract(repo, existing); err != nil {
		t.Fatal(err)
	}

	// Same taskID → should not conflict.
	if err := checkFanoutWriteScopeConflicts(repo, []string{"commands/foo.go"}, "tx"); err != nil {
		t.Fatalf("same task should not conflict: %v", err)
	}
}

// ─── persistFanoutArtifacts rollback on bundle save failure ──────────────────

func TestPersistFanoutArtifacts_BundleSaveRollback(t *testing.T) {
	repo := t.TempDir()
	now := time.Now().UTC().Format(time.RFC3339)
	contract := &DelegationContract{
		SchemaVersion: 1, ID: "del-1", ParentPlanID: "p1", ParentTaskID: "t1",
		Title: "x", WriteScope: []string{"commands/"}, Status: "active",
		CreatedAt: now, UpdatedAt: now,
	}
	// Bundle without DelegationID will fail to save (empty delegation_id rejected).
	bundle := &delegationBundleYAML{}

	err := persistFanoutArtifacts(repo, contract, bundle, "t1")
	if err == nil || !strings.Contains(err.Error(), "save delegation bundle") {
		t.Fatalf("expected bundle save error, got %v", err)
	}
	// Contract should have been rolled back.
	contractPath := filepath.Join(delegationDir(repo), "t1.yaml")
	if _, err := os.Stat(contractPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected contract file to be removed after bundle save failure, got stat err=%v", err)
	}
}

// ─── sweep helpers ───────────────────────────────────────────────────────────

func TestApplySweepAction_ScaffoldWorkflowDir(t *testing.T) {
	repo := t.TempDir()
	action := SweepActionItem{
		Project: ManagedProject{Name: "p", Path: repo},
		Action:  SweepActionScaffoldWorkflowDir,
	}
	if err := applySweepAction(action); err != nil {
		t.Fatalf("applySweepAction: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".agents", "workflow")); err != nil {
		t.Errorf("expected workflow dir created, got %v", err)
	}
}

func TestApplySweepAction_CreatePlanStructure(t *testing.T) {
	repo := t.TempDir()
	action := SweepActionItem{
		Project: ManagedProject{Name: "p", Path: repo},
		Action:  SweepActionCreatePlanStructure,
	}
	if err := applySweepAction(action); err != nil {
		t.Fatalf("applySweepAction: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".agents", "workflow", "plans")); err != nil {
		t.Errorf("expected plans dir created, got %v", err)
	}
}

func TestApplySweepAction_InformationalNoOp(t *testing.T) {
	for _, kind := range []SweepActionType{SweepActionCreateCheckpointReminder, SweepActionFlagStaleProposals} {
		action := SweepActionItem{
			Project: ManagedProject{Name: "p", Path: t.TempDir()},
			Action:  kind,
		}
		if err := applySweepAction(action); err != nil {
			t.Errorf("applySweepAction(%s): %v", kind, err)
		}
	}
}

func TestApplySweepAction_UnknownAction(t *testing.T) {
	action := SweepActionItem{
		Project: ManagedProject{Name: "p", Path: t.TempDir()},
		Action:  SweepActionType("bogus"),
	}
	err := applySweepAction(action)
	if err == nil || !strings.Contains(err.Error(), "unknown sweep action") {
		t.Fatalf("expected unknown action error, got %v", err)
	}
}

// confirmSweepAction returns true when Flags.Yes is set, regardless of confirmation requirement.
func TestConfirmSweepAction_AutoYesBypassesPrompt(t *testing.T) {
	saved := deps.Flags
	deps.Flags = GlobalFlags{
		JSON: func() bool { return false },
		Yes:  func() bool { return true },
	}
	defer func() { deps.Flags = saved }()

	action := SweepActionItem{
		Project:              ManagedProject{Name: "p"},
		Action:               SweepActionScaffoldWorkflowDir,
		Description:          "scaffold",
		RequiresConfirmation: true,
	}
	if !confirmSweepAction(action) {
		t.Error("expected confirmSweepAction to return true when Yes flag set")
	}
}

func TestConfirmSweepAction_NoConfirmReturnsTrue(t *testing.T) {
	saved := deps.Flags
	deps.Flags = GlobalFlags{
		JSON: func() bool { return false },
		Yes:  func() bool { return false },
	}
	defer func() { deps.Flags = saved }()

	action := SweepActionItem{
		Project:              ManagedProject{Name: "p"},
		Action:               SweepActionFlagStaleProposals,
		Description:          "informational",
		RequiresConfirmation: false,
	}
	if !confirmSweepAction(action) {
		t.Error("expected confirmSweepAction to return true when no confirmation required")
	}
}

// ─── validateFoldBackPriorAgreement branches ─────────────────────────────────

func TestValidateFoldBackPriorAgreement_PlanMismatch(t *testing.T) {
	prior := &foldBackArtifact{ID: "fb1", PlanID: "p1", Classification: "small"}
	in := &foldBackUpsertInputs{slug: "fb1", planID: "p2"}
	err := validateFoldBackPriorAgreement(prior, in)
	if err == nil || !strings.Contains(err.Error(), "belongs to plan") {
		t.Fatalf("expected plan mismatch error, got %v", err)
	}
}

func TestValidateFoldBackPriorAgreement_ProposeWithExisting(t *testing.T) {
	prior := &foldBackArtifact{ID: "fb1", PlanID: "p1", Classification: "small"}
	in := &foldBackUpsertInputs{slug: "fb1", planID: "p1", propose: true}
	err := validateFoldBackPriorAgreement(prior, in)
	if err == nil || !strings.Contains(err.Error(), "--propose is not valid") {
		t.Fatalf("expected propose error, got %v", err)
	}
}

func TestValidateFoldBackPriorAgreement_ProposalClassificationOK(t *testing.T) {
	prior := &foldBackArtifact{ID: "fb1", PlanID: "p1", Classification: "proposal"}
	in := &foldBackUpsertInputs{slug: "fb1", planID: "p1"}
	if err := validateFoldBackPriorAgreement(prior, in); err != nil {
		t.Fatalf("proposal classification should pass through: %v", err)
	}
}

func TestValidateFoldBackPriorAgreement_TaskScopedMissing(t *testing.T) {
	prior := &foldBackArtifact{ID: "fb1", PlanID: "p1", Classification: "small", TaskID: "t1"}
	in := &foldBackUpsertInputs{slug: "fb1", planID: "p1"}
	err := validateFoldBackPriorAgreement(prior, in)
	if err == nil || !strings.Contains(err.Error(), "task-scoped") {
		t.Fatalf("expected task-scoped error, got %v", err)
	}
}

func TestValidateFoldBackPriorAgreement_TaskScopedMismatch(t *testing.T) {
	prior := &foldBackArtifact{ID: "fb1", PlanID: "p1", Classification: "small", TaskID: "t1"}
	in := &foldBackUpsertInputs{slug: "fb1", planID: "p1", taskID: "t2"}
	err := validateFoldBackPriorAgreement(prior, in)
	if err == nil || !strings.Contains(err.Error(), "does not match fold-back scope") {
		t.Fatalf("expected task mismatch error, got %v", err)
	}
}

func TestValidateFoldBackPriorAgreement_TaskScopedMatches(t *testing.T) {
	prior := &foldBackArtifact{ID: "fb1", PlanID: "p1", Classification: "small", TaskID: "t1"}
	in := &foldBackUpsertInputs{slug: "fb1", planID: "p1", taskID: "t1"}
	if err := validateFoldBackPriorAgreement(prior, in); err != nil {
		t.Fatalf("matching task should pass: %v", err)
	}
}

func TestValidateFoldBackPriorAgreement_PlanScopedRejectsTask(t *testing.T) {
	prior := &foldBackArtifact{ID: "fb1", PlanID: "p1", Classification: "small"}
	in := &foldBackUpsertInputs{slug: "fb1", planID: "p1", taskID: "t1"}
	err := validateFoldBackPriorAgreement(prior, in)
	if err == nil || !strings.Contains(err.Error(), "plan-scoped") {
		t.Fatalf("expected plan-scoped error, got %v", err)
	}
}

// ─── dispatchFoldBackUpsert default error ────────────────────────────────────

func TestDispatchFoldBackUpsert_RoutingError(t *testing.T) {
	// priorExists=true but Classification is neither "proposal" nor "small" → default branch
	prior := &foldBackArtifact{Classification: "unknown"}
	in := &foldBackUpsertInputs{slug: "fb1"}
	artifact := &foldBackArtifact{}
	err := dispatchFoldBackUpsert(t.TempDir(), in, prior, true, 0, "", artifact)
	if err == nil || !strings.Contains(err.Error(), "internal fold-back routing error") {
		t.Fatalf("expected routing error, got %v", err)
	}
}

// ─── updateTaskFoldBackNote not-found path ───────────────────────────────────

func TestUpdateTaskFoldBackNote_TaskNotFound(t *testing.T) {
	repo := t.TempDir()
	if err := saveCanonicalTasks(repo, &CanonicalTaskFile{
		SchemaVersion: 1,
		PlanID:        "p1",
		Tasks:         []CanonicalTask{{ID: "t1", Title: "T", Status: "pending"}},
	}); err != nil {
		t.Fatal(err)
	}
	err := updateTaskFoldBackNote(repo, "p1", "nonexistent", func(n string) string { return n })
	if err == nil {
		t.Fatal("expected error for missing task")
	}
}

func TestUpdateTaskFoldBackNote_LoadError(t *testing.T) {
	// Missing TASKS.yaml file → load error
	err := updateTaskFoldBackNote(t.TempDir(), "missing-plan", "t1", func(n string) string { return n })
	if err == nil {
		t.Fatal("expected load tasks error")
	}
}

func TestWorkflowDelegationGate_TextOutput_MissingDecision(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	saveTestDelegationContract(t, repo, "t1", "p1", "del-t1")
	writeMergeBackFixture(t, repo, "t1", "p1")
	// No review-decision.yaml written → triggers the "review_overall_decision: missing" branch.

	out := executeWorkflowCommandOutput(t, repo, "delegation", "gate", "--plan", "p1", "--task", "t1")
	if !strings.Contains(out, "review_overall_decision: missing") {
		t.Fatalf("expected missing-decision text line, got:\n%s", out)
	}
}
