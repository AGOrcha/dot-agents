// Package workflow — broad coverage tests targeting previously untested
// functions across plan_task.go, sweep.go, drift.go, graph.go, iter_log.go,
// and state.go. These tests focus on the no-mocking exec paths: list/show/graph
// runners, render helpers, sweep apply/dry-run helpers, drift renderer,
// recordPlanStatusDrift, and so on.
package workflow

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// ─────────────────────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────────────────────

// captureCovStdout runs fn while os.Stdout is piped, then returns the captured
// bytes. It does NOT change cwd — callers must already be in the right repo.
func captureCovStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	runErr := fn()
	_ = w.Close()
	os.Stdout = old
	data, _ := io.ReadAll(r)
	_ = r.Close()
	return string(data), runErr
}

// chdirForCov chdir's to dir, registers a cleanup to restore the original cwd.
func chdirForCov(t *testing.T, dir string) {
	t.Helper()
	oldwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(oldwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// plan_task.go: list/show/tasks/slices/graph runners
// ─────────────────────────────────────────────────────────────────────────────

func TestRunWorkflowPlanList_NoPlans(t *testing.T) {
	dir := t.TempDir()
	chdirForCov(t, dir)
	out, err := captureCovStdout(t, runWorkflowPlanList)
	if err != nil {
		t.Fatalf("runWorkflowPlanList: %v", err)
	}
	if !strings.Contains(out, "No canonical plans") {
		t.Errorf("expected 'No canonical plans' in output, got: %s", out)
	}
}

func TestRunWorkflowPlanList_WithPlans(t *testing.T) {
	dir := setupTestProject(t)
	chdirForCov(t, dir)
	out, err := captureCovStdout(t, runWorkflowPlanList)
	if err != nil {
		t.Fatalf("runWorkflowPlanList: %v", err)
	}
	if !strings.Contains(out, "plan-001") {
		t.Errorf("expected plan id in output, got: %s", out)
	}
}

func TestRunWorkflowPlanList_JSON(t *testing.T) {
	dir := setupTestProject(t)
	chdirForCov(t, dir)
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = false })
	out, err := captureCovStdout(t, runWorkflowPlanList)
	if err != nil {
		t.Fatalf("runWorkflowPlanList json: %v", err)
	}
	if !strings.Contains(out, "plan-001") {
		t.Errorf("expected plan-001 in JSON output, got: %s", out)
	}
}

func TestRunWorkflowPlanShow_Basic(t *testing.T) {
	dir := setupTestProject(t)
	chdirForCov(t, dir)
	out, err := captureCovStdout(t, func() error { return runWorkflowPlanShow("plan-001") })
	if err != nil {
		t.Fatalf("runWorkflowPlanShow: %v", err)
	}
	if !strings.Contains(out, "Test Plan") {
		t.Errorf("expected plan title in show output, got: %s", out)
	}
}

func TestRunWorkflowPlanShow_NotFound(t *testing.T) {
	dir := t.TempDir()
	chdirForCov(t, dir)
	err := runWorkflowPlanShow("does-not-exist")
	if err == nil {
		t.Error("expected error for missing plan")
	}
}

func TestRunWorkflowPlanShow_JSON(t *testing.T) {
	dir := setupTestProject(t)
	chdirForCov(t, dir)
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = false })
	out, err := captureCovStdout(t, func() error { return runWorkflowPlanShow("plan-001") })
	if err != nil {
		t.Fatalf("runWorkflowPlanShow json: %v", err)
	}
	if !strings.Contains(out, "\"plan\"") {
		t.Errorf("expected JSON plan key, got: %s", out)
	}
}

func TestRunWorkflowTasks_Basic(t *testing.T) {
	dir := setupTestProject(t)
	chdirForCov(t, dir)
	out, err := captureCovStdout(t, func() error { return runWorkflowTasks("plan-001") })
	if err != nil {
		t.Fatalf("runWorkflowTasks: %v", err)
	}
	if !strings.Contains(out, "task-001") {
		t.Errorf("expected task-001 in output, got: %s", out)
	}
}

func TestRunWorkflowTasks_NotFound(t *testing.T) {
	dir := t.TempDir()
	chdirForCov(t, dir)
	if err := runWorkflowTasks("nope"); err == nil {
		t.Error("expected error for missing plan")
	}
}

func TestRunWorkflowSlices_Basic(t *testing.T) {
	dir := setupFanoutSliceProject(t, "pending")
	chdirForCov(t, dir)
	out, err := captureCovStdout(t, func() error { return runWorkflowSlices("p1") })
	if err != nil {
		t.Fatalf("runWorkflowSlices: %v", err)
	}
	if !strings.Contains(out, "s1") {
		t.Errorf("expected slice id in output, got: %s", out)
	}
}

func TestRunWorkflowSlices_NoSlices(t *testing.T) {
	dir := setupTestProject(t)
	chdirForCov(t, dir)
	err := runWorkflowSlices("plan-001")
	if err == nil {
		t.Error("expected error when SLICES.yaml missing")
	}
}

func TestRunWorkflowPlanGraph_Basic(t *testing.T) {
	dir := setupFanoutSliceProject(t, "pending")
	chdirForCov(t, dir)
	out, err := captureCovStdout(t, func() error { return runWorkflowPlanGraph("p1") })
	if err != nil {
		t.Fatalf("runWorkflowPlanGraph: %v", err)
	}
	if !strings.Contains(out, "Fanout Test Plan") {
		t.Errorf("expected plan title in graph output, got: %s", out)
	}
}

func TestRunWorkflowPlanGraph_NotFound(t *testing.T) {
	dir := t.TempDir()
	chdirForCov(t, dir)
	err := runWorkflowPlanGraph("missing")
	if err == nil {
		t.Error("expected error for missing plan")
	}
}

func TestRunWorkflowPlanGraph_AllPlans(t *testing.T) {
	dir := setupFanoutSliceProject(t, "pending")
	chdirForCov(t, dir)
	out, err := captureCovStdout(t, func() error { return runWorkflowPlanGraph("") })
	if err != nil {
		t.Fatalf("runWorkflowPlanGraph empty: %v", err)
	}
	if !strings.Contains(out, "p1") {
		t.Errorf("expected p1 in graph output, got: %s", out)
	}
}

func TestRunWorkflowPlanGraph_JSON(t *testing.T) {
	dir := setupFanoutSliceProject(t, "pending")
	chdirForCov(t, dir)
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = false })
	out, err := captureCovStdout(t, func() error { return runWorkflowPlanGraph("p1") })
	if err != nil {
		t.Fatalf("runWorkflowPlanGraph json: %v", err)
	}
	if !strings.Contains(out, "\"nodes\"") {
		t.Errorf("expected nodes key, got: %s", out)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// plan_task.go: rendering helpers
// ─────────────────────────────────────────────────────────────────────────────

func TestPlanShowTaskMarker(t *testing.T) {
	cases := map[string]string{
		"completed":   "✓",
		"in_progress": "▶",
		"blocked":     "✗",
		"pending":     " ",
		"unknown":     " ",
	}
	for status, want := range cases {
		if got := planShowTaskMarker(status); got != want {
			t.Errorf("planShowTaskMarker(%q) = %q, want %q", status, got, want)
		}
	}
}

func TestSummarizeTaskCounts(t *testing.T) {
	tasks := []CanonicalTask{
		{Status: "pending"},
		{Status: "in_progress"},
		{Status: "blocked"},
		{Status: "completed"},
		{Status: "cancelled"},
	}
	p, b, c, total := summarizeTaskCounts(tasks)
	if p != 2 || b != 1 || c != 1 || total != 5 {
		t.Errorf("summarizeTaskCounts: got p=%d b=%d c=%d total=%d, want 2,1,1,5", p, b, c, total)
	}
}

func TestIsValidPlanStatusAndTaskStatus(t *testing.T) {
	for _, s := range []string{"draft", "active", "paused", "completed", "archived"} {
		if !isValidPlanStatus(s) {
			t.Errorf("plan status %q should be valid", s)
		}
	}
	if isValidPlanStatus("bogus") {
		t.Error("bogus plan status should be invalid")
	}
	for _, s := range []string{"pending", "in_progress", "blocked", "completed", "cancelled"} {
		if !isValidTaskStatus(s) {
			t.Errorf("task status %q should be valid", s)
		}
	}
	if isValidTaskStatus("ghosts") {
		t.Error("ghosts should not be valid")
	}
}

func TestRenderDraftPlansHint(t *testing.T) {
	var buf bytes.Buffer
	renderDraftPlansHint(&buf, nil)
	if buf.Len() != 0 {
		t.Errorf("expected empty output for nil drafts, got %q", buf.String())
	}
	renderDraftPlansHint(&buf, []string{"alpha", "beta"})
	if !strings.Contains(buf.String(), "alpha") || !strings.Contains(buf.String(), "beta") {
		t.Errorf("expected draft ids in output, got %q", buf.String())
	}
}

func TestNewScopeEvidence_SlicesNonNil(t *testing.T) {
	ev := NewScopeEvidence("p", "t")
	if ev.DecisionLocks == nil || ev.RequiredReads == nil || ev.Queries == nil ||
		ev.RequiredPaths == nil || ev.OptionalPaths == nil || ev.ExcludedPaths == nil ||
		ev.Provides == nil || ev.Consumes == nil || ev.FinalWriteScope == nil ||
		ev.VerificationFocus == nil || ev.AllowedLocalChoices == nil ||
		ev.StopConditions == nil || ev.OpenGaps == nil {
		t.Error("NewScopeEvidence must initialize all slice fields to []T{} not nil")
	}
	if ev.PlanID != "p" || ev.TaskID != "t" {
		t.Errorf("plan/task id not propagated: %+v", ev)
	}
	if ev.Status != "draft" || ev.Confidence != "low" {
		t.Errorf("defaults wrong: status=%q conf=%q", ev.Status, ev.Confidence)
	}
}

func TestAppendScopeRequiredReads(t *testing.T) {
	ev := NewScopeEvidence("p", "t")
	appendScopeRequiredReads(ev, []GraphBridgeResult{
		{Path: "x.md", Title: "T", Summary: "S"},
		{Path: ""}, // skipped
	})
	if len(ev.RequiredReads) != 1 {
		t.Fatalf("required reads = %d, want 1", len(ev.RequiredReads))
	}
	if ev.RequiredReads[0].Path != "x.md" {
		t.Errorf("unexpected path: %+v", ev.RequiredReads[0])
	}
}

func TestLoadCanonicalTaskByID(t *testing.T) {
	dir := setupTestProject(t)
	task, err := loadCanonicalTaskByID(dir, "plan-001", "task-001")
	if err != nil {
		t.Fatalf("loadCanonicalTaskByID: %v", err)
	}
	if task.ID != "task-001" {
		t.Errorf("got %s", task.ID)
	}
	if _, err := loadCanonicalTaskByID(dir, "plan-001", "missing"); err == nil {
		t.Error("expected error for missing task")
	}
	if _, err := loadCanonicalTaskByID(dir, "missing-plan", "x"); err == nil {
		t.Error("expected error for missing plan")
	}
}

func TestCollectDraftPlanIDs(t *testing.T) {
	dir := t.TempDir()
	if got := collectDraftPlanIDs(dir); len(got) != 0 {
		t.Errorf("empty project should yield no drafts, got %v", got)
	}
	// Now seed a draft plan
	planDir := filepath.Join(dir, ".agents", "workflow", "plans", "draft-plan")
	if err := os.MkdirAll(planDir, 0755); err != nil {
		t.Fatal(err)
	}
	planYAML := []byte("schema_version: 1\nid: draft-plan\nstatus: draft\ntitle: D\n")
	if err := os.WriteFile(filepath.Join(planDir, "PLAN.yaml"), planYAML, 0644); err != nil {
		t.Fatal(err)
	}
	drafts := collectDraftPlanIDs(dir)
	if len(drafts) != 1 || drafts[0] != "draft-plan" {
		t.Errorf("expected [draft-plan], got %v", drafts)
	}
}

func TestGraphAdapterForProject(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	adapter := graphAdapterForProject(dir)
	if adapter == nil {
		t.Fatal("expected non-nil adapter")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// sweep.go: render / dry-run / apply helpers
// ─────────────────────────────────────────────────────────────────────────────

func TestRenderSweepPlanHeader(t *testing.T) {
	plan := SweepPlan{
		Actions: []SweepActionItem{
			{Project: ManagedProject{Name: "proj-x"}, Action: SweepActionScaffoldWorkflowDir, Description: "scaffold", RequiresConfirmation: true},
			{Project: ManagedProject{Name: "proj-y"}, Action: SweepActionCreateCheckpointReminder, Description: "remind"},
		},
	}
	out, _ := captureCovStdout(t, func() error {
		renderSweepPlanHeader(plan, true)
		return nil
	})
	if !strings.Contains(out, "Sweep Plan") {
		t.Errorf("expected Sweep Plan header in output, got %s", out)
	}
	if !strings.Contains(out, "proj-x") {
		t.Errorf("expected proj-x in output, got %s", out)
	}
	// Apply mode header (uses different marker)
	out2, _ := captureCovStdout(t, func() error {
		renderSweepPlanHeader(plan, false)
		return nil
	})
	if !strings.Contains(out2, "apply") {
		t.Errorf("expected apply label, got %s", out2)
	}
}

func TestRunSweepDryRun(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", tmp)
	plan := SweepPlan{Actions: []SweepActionItem{
		{Project: ManagedProject{Name: "p"}, Action: SweepActionScaffoldWorkflowDir, Description: "d"},
	}}
	out, _ := captureCovStdout(t, func() error {
		runSweepDryRun(plan)
		return nil
	})
	if !strings.Contains(out, "--apply") {
		t.Errorf("expected --apply hint in output, got %s", out)
	}
	// Log file should be present
	if _, err := os.Stat(sweepLogPath()); err != nil {
		t.Errorf("sweep log not created: %v", err)
	}
}

func TestRunSweepApply(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", tmp)
	target := t.TempDir()
	plan := SweepPlan{Actions: []SweepActionItem{
		{
			Project:              ManagedProject{Name: "p", Path: target},
			Action:               SweepActionScaffoldWorkflowDir,
			Description:          "scaffold dir",
			RequiresConfirmation: false,
		},
		{
			Project:     ManagedProject{Name: "p", Path: target},
			Action:      SweepActionCreateCheckpointReminder,
			Description: "remind",
		},
	}}
	out, _ := captureCovStdout(t, func() error {
		runSweepApply(plan, nil)
		return nil
	})
	if !strings.Contains(out, "Sweep complete") {
		t.Errorf("expected 'Sweep complete' summary, got %s", out)
	}
	if _, err := os.Stat(filepath.Join(target, ".agents", "workflow")); err != nil {
		t.Errorf("scaffold dir not created: %v", err)
	}
}

func TestConfirmSweepAction_NoConfirmationNeeded(t *testing.T) {
	action := SweepActionItem{
		Project: ManagedProject{Name: "p"}, Action: SweepActionCreateCheckpointReminder,
		RequiresConfirmation: false,
	}
	if !confirmSweepAction(action, nil) {
		t.Error("expected confirmSweepAction to return true when no confirmation needed")
	}
}

func TestRunWorkflowSweep_NoProjects(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", tmp)
	cmd := newSweepTestCommand(false, 7, 30)
	out, _ := captureCovStdout(t, func() error { return runWorkflowSweep(cmd, nil) })
	if !strings.Contains(out, "No managed projects") {
		t.Errorf("expected 'No managed projects' message, got %s", out)
	}
}

// newSweepTestCommand builds a cobra.Command with the flags runWorkflowSweep reads.
func newSweepTestCommand(apply bool, staleDays, proposalDays int) *cobra.Command {
	c := &cobra.Command{}
	c.Flags().Int("stale-days", staleDays, "")
	c.Flags().Int("proposal-days", proposalDays, "")
	c.Flags().Bool("apply", apply, "")
	return c
}

// ─────────────────────────────────────────────────────────────────────────────
// drift.go: renderer / runner / recordPlanStatusDrift
// ─────────────────────────────────────────────────────────────────────────────

func TestRecordPlanStatusDrift(t *testing.T) {
	rep := &RepoDriftReport{}
	recordPlanStatusDrift(rep, "p-done", "completed")
	if len(rep.CompletedPlanIDs) != 1 || rep.CompletedPlanIDs[0] != "p-done" {
		t.Errorf("expected p-done in completed, got %v", rep.CompletedPlanIDs)
	}
	recordPlanStatusDrift(rep, "p-stray", "archived")
	if len(rep.InconsistentArchivedPlanIDs) != 1 {
		t.Errorf("expected 1 inconsistent, got %v", rep.InconsistentArchivedPlanIDs)
	}
	// Unknown status — should be no-op
	recordPlanStatusDrift(rep, "p-active", "active")
	if len(rep.CompletedPlanIDs) != 1 || len(rep.InconsistentArchivedPlanIDs) != 1 {
		t.Errorf("active status should not record, got completed=%v archived=%v",
			rep.CompletedPlanIDs, rep.InconsistentArchivedPlanIDs)
	}
}

func TestRenderDriftReport(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", tmp)
	reports := []RepoDriftReport{
		{Project: ManagedProject{Name: "alpha"}, Status: "healthy"},
		{Project: ManagedProject{Name: "beta"}, Status: "warn", Warnings: []string{"stale"},
			CompletedPlanIDs: []string{"p1"}, InconsistentArchivedPlanIDs: []string{"p2"}},
	}
	agg := aggregateDrift(reports)
	out, _ := captureCovStdout(t, func() error {
		renderDriftReport(reports, agg)
		return nil
	})
	if !strings.Contains(out, "alpha") || !strings.Contains(out, "beta") {
		t.Errorf("expected project names in render, got %s", out)
	}
	if !strings.Contains(out, "completed plans pending archive") {
		t.Errorf("expected completed plans line, got %s", out)
	}
	if !strings.Contains(out, "inconsistent archived plans") {
		t.Errorf("expected inconsistent line, got %s", out)
	}
}

func TestDriftCheckpointPhase_NoCheckpoint(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", tmp)
	rep := &RepoDriftReport{}
	driftCheckpointPhase(rep, ManagedProject{Name: "no-cp"}, 7)
	if !rep.MissingCheckpoint {
		t.Error("expected MissingCheckpoint true")
	}
}

func TestDriftStaleProposalPhase_NoProposals(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", tmp)
	rep := &RepoDriftReport{}
	driftStaleProposalPhase(rep, 30)
	if rep.StaleProposalCount != 0 {
		t.Errorf("expected 0 stale proposals, got %d", rep.StaleProposalCount)
	}
}

func TestDriftWorkflowDirPhase(t *testing.T) {
	dir := t.TempDir()
	rep := &RepoDriftReport{}
	driftWorkflowDirPhase(rep, ManagedProject{Path: dir})
	if !rep.MissingWorkflowDir {
		t.Error("expected MissingWorkflowDir true")
	}
	if !rep.MissingPlanStructure {
		t.Error("expected MissingPlanStructure true")
	}
}

func TestDriftPlanScanPhase_NoPlansSkipped(t *testing.T) {
	rep := &RepoDriftReport{MissingPlanStructure: true}
	driftPlanScanPhase(rep, ManagedProject{Path: "/non/existent"})
	if len(rep.CompletedPlanIDs) != 0 {
		t.Errorf("expected no plans when MissingPlanStructure=true, got %v", rep.CompletedPlanIDs)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// graph.go: render and runner
// ─────────────────────────────────────────────────────────────────────────────

func TestRenderGraphQueryResults_TextMode(t *testing.T) {
	resp := GraphBridgeResponse{
		Results: []GraphBridgeResult{
			{Type: "decision", Title: "T1", Summary: "S1"},
		},
		Warnings: []string{"sparse"},
	}
	out, _ := captureCovStdout(t, func() error {
		renderGraphQueryResults("decision_lookup", "q", resp)
		return nil
	})
	if !strings.Contains(out, "decision") || !strings.Contains(out, "T1") {
		t.Errorf("expected results in output, got %s", out)
	}
}

func TestRenderGraphQueryResults_NoResults(t *testing.T) {
	out, _ := captureCovStdout(t, func() error {
		renderGraphQueryResults("plan_context", "q", GraphBridgeResponse{})
		return nil
	})
	if !strings.Contains(out, "No results") {
		t.Errorf("expected 'No results' in output, got %s", out)
	}
}

func TestRenderGraphQueryResults_JSON(t *testing.T) {
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = false })
	resp := GraphBridgeResponse{Intent: "plan_context", Query: "x"}
	out, _ := captureCovStdout(t, func() error {
		renderGraphQueryResults("plan_context", "x", resp)
		return nil
	})
	if !strings.Contains(out, "\"intent\"") {
		t.Errorf("expected JSON, got %s", out)
	}
}

func TestRunWorkflowGraphHealth(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	chdirForCov(t, dir)
	cmd := &cobra.Command{}
	out, err := captureCovStdout(t, func() error { return runWorkflowGraphHealth(cmd, nil) })
	if err != nil {
		t.Fatalf("runWorkflowGraphHealth: %v", err)
	}
	if !strings.Contains(out, "Graph Bridge Health") {
		t.Errorf("expected health header, got %s", out)
	}
}

func TestRunWorkflowGraphHealth_JSON(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	chdirForCov(t, dir)
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = false })
	cmd := &cobra.Command{}
	out, err := captureCovStdout(t, func() error { return runWorkflowGraphHealth(cmd, nil) })
	if err != nil {
		t.Fatalf("runWorkflowGraphHealth json: %v", err)
	}
	if !strings.Contains(out, "\"status\"") {
		t.Errorf("expected JSON status field, got %s", out)
	}
}

func TestReadGraphBridgeHealth_Missing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	h, err := readGraphBridgeHealth("nope-project")
	if err != nil {
		t.Fatalf("readGraphBridgeHealth: %v", err)
	}
	if h != nil {
		t.Errorf("expected nil for missing file, got %+v", h)
	}
}

func TestReadGraphBridgeHealth_Malformed(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", tmp)
	dir := filepath.Join(tmp, "context", "p")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "graph-bridge-health.json"), []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := readGraphBridgeHealth("p")
	if err == nil {
		t.Error("expected error for malformed JSON")
	}
}

func TestIsWorkflowGraphCodeBridgeIntent(t *testing.T) {
	for _, i := range []string{"symbol_lookup", "impact_radius", "callers_of", "callees_of",
		"community_context", "symbol_decisions", "decision_symbols", "change_analysis", "tests_for"} {
		if !isWorkflowGraphCodeBridgeIntent(i) {
			t.Errorf("expected %q to be code-bridge intent", i)
		}
	}
	if isWorkflowGraphCodeBridgeIntent("plan_context") {
		t.Error("plan_context should not be a code-bridge intent")
	}
}
