// Package workflow — seventh batch of coverage tests covering the
// remaining long-tail branches needed to reach the 95% threshold.
package workflow

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// ─── review_gate.go: evaluateDelegationGate full happy + edge paths ────────

func TestEvaluateDelegationGate_EmptyTaskID_Push7(t *testing.T) {
	_, err := evaluateDelegationGate(t.TempDir(), "", "")
	if err == nil || !strings.Contains(err.Error(), "task_id is required") {
		t.Fatalf("expected task_id required, got %v", err)
	}
}

func TestEvaluateDelegationGate_MissingContract(t *testing.T) {
	_, err := evaluateDelegationGate(t.TempDir(), "no-task", "")
	if err == nil || !strings.Contains(err.Error(), "load delegation contract") {
		t.Fatalf("expected load-contract error, got %v", err)
	}
}

func TestEvaluateDelegationGate_PlanIDMismatch(t *testing.T) {
	repo := t.TempDir()
	saveTestDelegationContract(t, repo, "task-a", "plan-a", "d-a")
	_, err := evaluateDelegationGate(repo, "task-a", "plan-other")
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("expected plan-mismatch error, got %v", err)
	}
}

func TestEvaluateDelegationGate_MissingMergeback(t *testing.T) {
	repo := t.TempDir()
	saveTestDelegationContract(t, repo, "task-b", "plan-b", "d-b")
	_, err := evaluateDelegationGate(repo, "task-b", "")
	if err == nil || !strings.Contains(err.Error(), "merge-back") {
		t.Fatalf("expected mergeback-required error, got %v", err)
	}
}

func TestEvaluateDelegationGate_ReviewDecisionMissing(t *testing.T) {
	repo := t.TempDir()
	saveTestDelegationContract(t, repo, "task-c", "plan-c", "d-c")
	if err := saveMergeBack(repo, &MergeBackSummary{TaskID: "task-c", SchemaVersion: 1}); err != nil {
		t.Fatal(err)
	}
	out, err := evaluateDelegationGate(repo, "task-c", "")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if out.Outcome != "reject" || out.CloseoutAllowed {
		t.Fatalf("expected reject + no closeout, got %+v", out)
	}
	if !strings.Contains(out.Reason, "review-decision.yaml missing") {
		t.Fatalf("expected missing reason, got %q", out.Reason)
	}
}

func TestEvaluateDelegationGate_AcceptDecision(t *testing.T) {
	repo := t.TempDir()
	saveTestDelegationContract(t, repo, "task-d", "plan-d", "d-d")
	if err := saveMergeBack(repo, &MergeBackSummary{TaskID: "task-d", SchemaVersion: 1}); err != nil {
		t.Fatal(err)
	}
	doc := newValidReviewDecisionDoc()
	doc.TaskID = "task-d"
	doc.ParentPlanID = "plan-d"
	doc.OverallDecision = "accept"
	doc.Phase1Decision = "accept"
	doc.Phase2Decision = "accept"
	if err := writeReviewDecisionYAML(repo, doc); err != nil {
		t.Fatal(err)
	}
	out, err := evaluateDelegationGate(repo, "task-d", "")
	if err != nil {
		t.Fatal(err)
	}
	if out.Outcome != "accept" || !out.CloseoutAllowed {
		t.Fatalf("expected accept + closeout, got %+v", out)
	}
}

func TestEvaluateDelegationGate_RejectDecision(t *testing.T) {
	repo := t.TempDir()
	saveTestDelegationContract(t, repo, "task-r", "plan-r", "d-r")
	if err := saveMergeBack(repo, &MergeBackSummary{TaskID: "task-r", SchemaVersion: 1}); err != nil {
		t.Fatal(err)
	}
	doc := newValidReviewDecisionDoc()
	doc.TaskID = "task-r"
	doc.ParentPlanID = "plan-r"
	doc.OverallDecision = "reject"
	doc.Phase1Decision = "reject"
	doc.Phase2Decision = "reject"
	doc.FailedGates = []string{"test-coverage"}
	if err := writeReviewDecisionYAML(repo, doc); err != nil {
		t.Fatal(err)
	}
	out, err := evaluateDelegationGate(repo, "task-r", "")
	if err != nil {
		t.Fatal(err)
	}
	if out.Outcome != "reject" {
		t.Fatalf("expected reject outcome, got %s", out.Outcome)
	}
	if !strings.Contains(out.Reason, "failed_gates") {
		t.Fatalf("expected failed_gates reason, got %q", out.Reason)
	}
}

func TestEvaluateDelegationGate_EscalateDecision(t *testing.T) {
	repo := t.TempDir()
	saveTestDelegationContract(t, repo, "task-e", "plan-e", "d-e")
	if err := saveMergeBack(repo, &MergeBackSummary{TaskID: "task-e", SchemaVersion: 1}); err != nil {
		t.Fatal(err)
	}
	doc := newValidReviewDecisionDoc()
	doc.TaskID = "task-e"
	doc.ParentPlanID = "plan-e"
	doc.OverallDecision = "escalate"
	doc.Phase1Decision = "escalate"
	doc.Phase2Decision = "escalate"
	doc.EscalationReason = "needs planning review"
	if err := writeReviewDecisionYAML(repo, doc); err != nil {
		t.Fatal(err)
	}
	out, err := evaluateDelegationGate(repo, "task-e", "")
	if err != nil {
		t.Fatal(err)
	}
	if out.Outcome != "escalate" || !out.PlanningRequired {
		t.Fatalf("expected escalate + planning_required, got %+v", out)
	}
	if !strings.Contains(out.Reason, "needs planning review") {
		t.Fatalf("expected escalation reason, got %q", out.Reason)
	}
}

// ─── review_gate.go: decisionReason for accept with notes ────────────────

func TestDecisionReason_AcceptWithReviewerNotes(t *testing.T) {
	doc := &ReviewDecisionDoc{
		OverallDecision: "accept",
		ReviewerNotes:   "looks good",
	}
	if got := decisionReason(doc); got != "looks good" {
		t.Fatalf("expected reviewer notes, got %q", got)
	}
}

func TestDecisionReason_RejectWithReviewerNotes(t *testing.T) {
	doc := &ReviewDecisionDoc{
		OverallDecision: "reject",
		ReviewerNotes:   "needs work",
	}
	if got := decisionReason(doc); got != "needs work" {
		t.Fatalf("expected reviewer notes, got %q", got)
	}
}

func TestDecisionReason_EscalateFallsBackToNotes(t *testing.T) {
	doc := &ReviewDecisionDoc{
		OverallDecision:  "escalate",
		EscalationReason: "",
		ReviewerNotes:    "ask the lead",
	}
	if got := decisionReason(doc); got != "ask the lead" {
		t.Fatalf("expected fallback notes, got %q", got)
	}
}

func TestDecisionReason_EscalateDefaultMessage(t *testing.T) {
	doc := &ReviewDecisionDoc{OverallDecision: "escalate"}
	if got := decisionReason(doc); !strings.Contains(got, "review escalated") {
		t.Fatalf("expected default escalate text, got %q", got)
	}
}

func TestDecisionReason_NilDoc(t *testing.T) {
	if got := decisionReason(nil); got != "" {
		t.Fatalf("expected empty for nil doc, got %q", got)
	}
}

func TestDecisionReason_UnknownDecision(t *testing.T) {
	doc := &ReviewDecisionDoc{OverallDecision: "weird"}
	if got := decisionReason(doc); got != "" {
		t.Fatalf("expected empty for unknown decision, got %q", got)
	}
}

// ─── review_gate.go: loadReviewDecisionYAML malformed YAML ────────────────

func TestLoadReviewDecisionYAML_MalformedYAML(t *testing.T) {
	repo := t.TempDir()
	path, err := reviewDecisionYAMLPath(repo, "task-x")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(":\n  - bad: ["), 0644); err != nil {
		t.Fatal(err)
	}
	_, err = loadReviewDecisionYAML(repo, "task-x")
	if err == nil || !strings.Contains(err.Error(), "parse review decision") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

// ─── delegation.go: runWorkflowFoldBackList missing dir / JSON path ───────

func TestRunWorkflowFoldBackList_MissingDirText(t *testing.T) {
	repo := setupTestProject(t)
	chdirForCov(t, repo)
	cmd := &cobra.Command{}
	cmd.Flags().String("plan", "", "")
	var buf strings.Builder
	cmd.SetOut(&buf)
	if err := runWorkflowFoldBackList(cmd, nil); err != nil {
		t.Fatalf("foldback list: %v", err)
	}
	if !strings.Contains(buf.String(), "No fold-back observations recorded") {
		t.Fatalf("expected empty message, got %s", buf.String())
	}
}

func TestRunWorkflowFoldBackList_MissingDirJSON(t *testing.T) {
	repo := setupTestProject(t)
	chdirForCov(t, repo)
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = false })
	cmd := &cobra.Command{}
	cmd.Flags().String("plan", "", "")
	var buf strings.Builder
	cmd.SetOut(&buf)
	if err := runWorkflowFoldBackList(cmd, nil); err != nil {
		t.Fatalf("foldback list json: %v", err)
	}
	if !strings.Contains(buf.String(), "[]") {
		t.Fatalf("expected empty-JSON array, got %s", buf.String())
	}
}

// ─── delegation.go: ensureTaskVerificationDir creates dir ─────────────────

func TestEnsureTaskVerificationDir(t *testing.T) {
	repo := t.TempDir()
	if err := ensureTaskVerificationDir(repo, "task-x"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".agents", "active", "verification", "task-x")); err != nil {
		t.Fatalf("expected dir, got %v", err)
	}
}

// ─── delegation.go: writeScopeHasAdjacentGoTests true branch ──────────────

func TestWriteScopeHasAdjacentGoTests_HasTests(t *testing.T) {
	repo := t.TempDir()
	dir := filepath.Join(repo, "commands", "foo")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "foo.go"), []byte("package foo"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "foo_test.go"), []byte("package foo"), 0644); err != nil {
		t.Fatal(err)
	}
	got := writeScopeHasAdjacentGoTests(repo, []string{"commands/foo/foo.go"})
	if !got {
		t.Fatal("expected adjacent test detected")
	}
}

func TestWriteScopeHasAdjacentGoTests_NoTests_Push7(t *testing.T) {
	repo := t.TempDir()
	dir := filepath.Join(repo, "commands", "bar")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bar.go"), []byte("package bar"), 0644); err != nil {
		t.Fatal(err)
	}
	got := writeScopeHasAdjacentGoTests(repo, []string{"commands/bar/bar.go"})
	if got {
		t.Fatal("expected no adjacent test")
	}
}

func TestWriteScopeHasAdjacentGoTests_DirEntry(t *testing.T) {
	repo := t.TempDir()
	dir := filepath.Join(repo, "internal", "x")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "x_test.go"), []byte("package x"), 0644); err != nil {
		t.Fatal(err)
	}
	if !writeScopeHasAdjacentGoTests(repo, []string{"internal/x"}) {
		t.Fatal("expected test detected via dir entry")
	}
}

// ─── delegation.go: writeScopeImpliesNonTestGo branches ───────────────────

func TestWriteScopeImpliesNonTestGo_GoFile(t *testing.T) {
	if !writeScopeImpliesNonTestGo([]string{"commands/x.go"}) {
		t.Fatal("expected true")
	}
}

func TestWriteScopeImpliesNonTestGo_OnlyTests(t *testing.T) {
	if writeScopeImpliesNonTestGo([]string{"commands/x_test.go"}) {
		t.Fatal("expected false for tests-only")
	}
}

func TestWriteScopeImpliesNonTestGo_DocOnly(t *testing.T) {
	if writeScopeImpliesNonTestGo([]string{"README.md", "docs/x.md"}) {
		t.Fatal("expected false for docs-only")
	}
}

// ─── delegation.go: checkPreVerifierTDDGate branches ──────────────────────

func TestCheckPreVerifierTDDGate_Skip(t *testing.T) {
	if err := checkPreVerifierTDDGate(t.TempDir(), []string{"x.go"}, true, true); err != nil {
		t.Fatalf("skip path should pass, got %v", err)
	}
}

func TestCheckPreVerifierTDDGate_NotRequired(t *testing.T) {
	if err := checkPreVerifierTDDGate(t.TempDir(), []string{"x.go"}, false, false); err != nil {
		t.Fatalf("not-required path should pass, got %v", err)
	}
}

func TestCheckPreVerifierTDDGate_DocOnly(t *testing.T) {
	if err := checkPreVerifierTDDGate(t.TempDir(), []string{"README.md"}, true, false); err != nil {
		t.Fatalf("doc-only path should pass, got %v", err)
	}
}

func TestCheckPreVerifierTDDGate_HasAdjacentTest(t *testing.T) {
	repo := t.TempDir()
	dir := filepath.Join(repo, "pkg")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pkg.go"), []byte("package pkg"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pkg_test.go"), []byte("package pkg"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := checkPreVerifierTDDGate(repo, []string{"pkg/pkg.go"}, true, false); err != nil {
		t.Fatalf("adjacent-test path should pass, got %v", err)
	}
}

func TestCheckPreVerifierTDDGate_Fails(t *testing.T) {
	repo := t.TempDir()
	dir := filepath.Join(repo, "pkg")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pkg.go"), []byte("package pkg"), 0644); err != nil {
		t.Fatal(err)
	}
	err := checkPreVerifierTDDGate(repo, []string{"pkg/pkg.go"}, true, false)
	if err == nil || !strings.Contains(err.Error(), "pre-verifier TDD gate") {
		t.Fatalf("expected TDD-gate fail, got %v", err)
	}
}

// ─── delegation.go: resolveFanoutSliceTask branches ───────────────────────

func TestResolveFanoutSliceTask_EmptyReturnsTask(t *testing.T) {
	tid, ws, err := resolveFanoutSliceTask(t.TempDir(), "p", "", "tx", false)
	if err != nil {
		t.Fatal(err)
	}
	if tid != "tx" || ws != nil {
		t.Fatalf("expected pass-through, got tid=%q ws=%v", tid, ws)
	}
}

func TestResolveFanoutSliceTask_MissingSlice(t *testing.T) {
	repo := setupFanoutSliceProject(t, "pending")
	_, _, err := resolveFanoutSliceTask(repo, "p1", "missing-slice", "", false)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected slice-not-found, got %v", err)
	}
}

func TestResolveFanoutSliceTask_CompletedSliceRejected(t *testing.T) {
	repo := setupFanoutSliceProject(t, "completed")
	_, _, err := resolveFanoutSliceTask(repo, "p1", "s1", "", false)
	if err == nil || !strings.Contains(err.Error(), "already completed") {
		t.Fatalf("expected completed-rejection, got %v", err)
	}
}

// ─── delegation.go: resolveFanoutTargetTask branches ──────────────────────

func TestResolveFanoutTargetTask_TaskNotFound(t *testing.T) {
	tf := &CanonicalTaskFile{PlanID: "p", Tasks: []CanonicalTask{{ID: "t1", Status: "pending"}}}
	_, err := resolveFanoutTargetTask(tf, "missing", "p")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected task-not-found, got %v", err)
	}
}

func TestResolveFanoutTargetTask_BadStatus(t *testing.T) {
	tf := &CanonicalTaskFile{PlanID: "p", Tasks: []CanonicalTask{{ID: "t1", Status: "completed"}}}
	_, err := resolveFanoutTargetTask(tf, "t1", "p")
	if err == nil || !strings.Contains(err.Error(), "only pending or in_progress") {
		t.Fatalf("expected status-rejection, got %v", err)
	}
}

// ─── drift.go: driftPlanScanPhase reads PLAN.yaml status ─────────────────

func TestDriftPlanScanPhase_RecordsCompleted(t *testing.T) {
	repo := t.TempDir()
	plansDir := filepath.Join(repo, ".agents", "workflow", "plans", "done-plan")
	if err := os.MkdirAll(plansDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plansDir, "PLAN.yaml"), []byte("status: completed\n"), 0644); err != nil {
		t.Fatal(err)
	}
	report := &RepoDriftReport{}
	driftPlanScanPhase(report, ManagedProject{Name: "p", Path: repo})
	if len(report.CompletedPlanIDs) != 1 {
		t.Fatalf("expected one completed plan, got %v", report.CompletedPlanIDs)
	}
}

func TestDriftPlanScanPhase_MissingStructureShortCircuits(t *testing.T) {
	report := &RepoDriftReport{MissingPlanStructure: true}
	// Must not panic / read anywhere.
	driftPlanScanPhase(report, ManagedProject{Name: "p", Path: "/nonexistent"})
	if len(report.CompletedPlanIDs) != 0 {
		t.Fatal("expected no scan when structure missing")
	}
}

// ─── verification.go: writeVerifyResultArtifact happy round-trip ─────────

func TestWriteVerifyResultArtifact_HappyPath(t *testing.T) {
	repo := t.TempDir()
	saveTestDelegationContract(t, repo, "task-h", "plan-h", "d-h")
	rel, err := writeVerifyResultArtifact(verifyResultArtifactInputs{
		ProjectPath: repo, TaskID: "task-h", Kind: "test", Status: "pass",
		Summary: "ok", Command: "go test", VerifierType: "unit",
		Now: "2026-05-12T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(rel, "unit.result.yaml") {
		t.Fatalf("unexpected rel path: %s", rel)
	}
}

// ─── state.go: gatherWorkflowStateInputs collectWorkflowPlans err ────────

func TestGatherWorkflowStateInputs_PlansError(t *testing.T) {
	repo := t.TempDir()
	planDir := filepath.Join(repo, ".agents", "active")
	if err := os.MkdirAll(planDir, 0755); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(planDir, "x.plan.md")
	if err := os.WriteFile(bad, []byte("# plan\n"), 0644); err != nil {
		t.Fatal(err)
	}
	chmodUnreadable(t, bad)
	chdirForCov(t, repo)
	_, err := gatherWorkflowStateInputs()
	if err == nil {
		t.Fatal("expected propagated read error")
	}
}

// ─── plan_task.go: persistScopeEvidenceSidecar happy round-trip ──────────

func TestPersistScopeEvidenceSidecar_HappyPath_Push7(t *testing.T) {
	repo := t.TempDir()
	out, err := persistScopeEvidenceSidecar(repo, "p1", "t1", &ScopeEvidence{Mode: "code"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("expected file written, got %v", err)
	}
}

// ─── delegation.go: parseFoldBackUpsertInputs happy path ─────────────────

func TestParseFoldBackUpsertInputs_Happy(t *testing.T) {
	in, err := parseFoldBackUpsertInputs(newFoldBackTestCmd("body", "valid-slug", "p1"), false)
	if err != nil {
		t.Fatal(err)
	}
	if in.slug != "valid-slug" || in.observation != "body" {
		t.Fatalf("unexpected: %+v", in)
	}
}

// ─── delegation.go: loadPriorFoldBackArtifact happy path ─────────────────

func TestLoadPriorFoldBackArtifact_Happy(t *testing.T) {
	repo := t.TempDir()
	dir := foldBackDir(repo)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := writeFoldBackArtifact(repo, foldBackArtifact{
		ID:             "good-slug",
		Classification: "small",
		PlanID:         "p1",
		CreatedAt:      "2026-05-12T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	a, ok, err := loadPriorFoldBackArtifact(repo, "good-slug")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || a == nil || a.PlanID != "p1" {
		t.Fatalf("expected loaded artifact, got ok=%v a=%+v", ok, a)
	}
}

// ─── delegation.go: dispatchFoldBackUpsert default branch ────────────────

func TestDispatchFoldBackUpsert_DefaultErrors(t *testing.T) {
	// priorExists=true with unknown classification falls through neither
	// "proposal" nor "small" prior-update branches and lands in the
	// !priorExists arms — but priorExists=true here so we hit the default.
	prior := &foldBackArtifact{Classification: "bogus"}
	in := &foldBackUpsertInputs{planID: "p1", observation: "x"}
	artifact := &foldBackArtifact{}
	err := dispatchFoldBackUpsert(t.TempDir(), in, prior, true, 1, "2026-05-12T00:00:00Z", artifact)
	if err == nil || !strings.Contains(err.Error(), "internal fold-back routing") {
		t.Fatalf("expected routing error, got %v", err)
	}
}

// ─── delegation.go: validatePriorFoldBack rejects propose on small ────────

func TestValidatePriorFoldBack_SmallWithPropose(t *testing.T) {
	prior := &foldBackArtifact{Classification: "small", PlanID: "p1"}
	in := &foldBackUpsertInputs{planID: "p1", slug: "x", propose: true}
	err := validatePriorFoldBack(prior, in)
	if err == nil {
		t.Fatal("expected error for propose-on-existing-small")
	}
}

// ─── verification_result_schema.go: write error injection via yaml seam ──

func TestWriteVerificationResultYAML_YAMLMarshalError_Wrapped(t *testing.T) {
	sentinel := errors.New("yaml boom")
	withYAMLMarshalStub(t, yamlMarshalErrStub(sentinel))
	err := writeVerificationResultYAML(t.TempDir(), newValidVerificationResultDoc())
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected wrapped sentinel, got %v", err)
	}
}

// ─── sweep.go: appendSweepLog OpenFile error swallowed ───────────────────

func TestAppendSweepLog_OpenFileErrorSilent(t *testing.T) {
	// AGENTS_HOME points at a path that cannot be created.
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", filepath.Join(blocker, "child"))
	// Must not panic; error is intentionally swallowed.
	appendSweepLog(SweepLogEntry{Action: "x"})
}

// ─── delegation.go: checkFanoutScopeEvidenceWarnings sidecar-missing branch

func TestCheckFanoutScopeEvidenceWarnings_NoSidecarSilent(t *testing.T) {
	// graph not configured -> warn suppressed silently.
	checkFanoutScopeEvidenceWarnings(t.TempDir(), "p1", "t1", false)
}

func TestCheckFanoutScopeEvidenceWarnings_LowConfidence(t *testing.T) {
	repo := t.TempDir()
	dir := filepath.Join(repo, ".agents", "workflow", "plans", "p1", "evidence")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "t1.scope.yaml"), []byte("confidence: low\n"), 0644); err != nil {
		t.Fatal(err)
	}
	checkFanoutScopeEvidenceWarnings(repo, "p1", "t1", false)
}

func TestCheckFanoutScopeEvidenceWarnings_SkipShortCircuit(t *testing.T) {
	checkFanoutScopeEvidenceWarnings(t.TempDir(), "p1", "t1", true)
}

// ─── plan_task.go: deriveScopeMode AppType branch ────────────────────────

func TestDeriveScopeMode_AppTypeForcesCode(t *testing.T) {
	got := deriveScopeMode(&CanonicalTask{AppType: "go-cli"})
	if got != "code" {
		t.Fatalf("expected code for AppType set, got %q", got)
	}
}
