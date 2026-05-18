package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunWorkflowDrift_NoProjects(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", tmp)
	cmd := newDriftTestCommand("", 7, 30)
	out, _ := captureCovStdout(t, func() error { return runWorkflowDrift(cmd, nil) })
	if !strings.Contains(out, "No managed projects") {
		t.Errorf("expected 'No managed projects' notice, got %s", out)
	}
}

func TestRunWorkflowDrift_MissingProjectFilter(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", tmp)

	if err := seedManagedProject(tmp, "alpha", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	cmd := newDriftTestCommand("missing", 7, 30)
	err := runWorkflowDrift(cmd, nil)
	if err == nil {
		t.Error("expected error for missing project filter")
	}
}

func TestRunWorkflowDrift_RealProject(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", tmp)
	target := t.TempDir()
	if err := seedManagedProject(tmp, "ok-proj", target); err != nil {
		t.Fatal(err)
	}
	cmd := newDriftTestCommand("", 7, 30)
	out, _ := captureCovStdout(t, func() error { return runWorkflowDrift(cmd, nil) })
	if !strings.Contains(out, "Workflow Drift Report") {
		t.Errorf("expected drift report header, got: %s", out)
	}
}

func TestRunWorkflowDrift_JSON(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", tmp)
	target := t.TempDir()
	if err := seedManagedProject(tmp, "ok-json", target); err != nil {
		t.Fatal(err)
	}
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = false })
	cmd := newDriftTestCommand("", 7, 30)
	out, _ := captureCovStdout(t, func() error { return runWorkflowDrift(cmd, nil) })
	if !strings.Contains(out, "\"timestamp\"") {
		t.Errorf("expected json timestamp field, got %s", out)
	}
}

// TestRunWorkflowDriftWithLister_ListerError drives the previously
// unreachable load-projects failure branch by passing a stub lister that
// returns a non-nil error.
func TestRunWorkflowDriftWithLister_ListerError(t *testing.T) {
	cmd := newDriftTestCommand("", 7, 30)
	stub := func() ([]ManagedProject, error) {
		return nil, errSentinelDriftLister
	}
	err := runWorkflowDriftWithLister(cmd, stub)
	if err == nil {
		t.Fatal("expected error from lister to propagate")
	}
	if !strings.Contains(err.Error(), "load managed projects") {
		t.Errorf("expected wrapped error, got %v", err)
	}
}

// TestRunWorkflowDriftWithLister_EmptySlice exercises the no-projects
// notice branch independently of the global config tree.
func TestRunWorkflowDriftWithLister_EmptySlice(t *testing.T) {
	cmd := newDriftTestCommand("", 7, 30)
	stub := func() ([]ManagedProject, error) {
		return []ManagedProject{}, nil
	}
	out, _ := captureCovStdout(t, func() error {
		return runWorkflowDriftWithLister(cmd, stub)
	})
	if !strings.Contains(out, "No managed projects") {
		t.Errorf("expected no-projects notice, got %s", out)
	}
}

// TestRunWorkflowDriftWithLister_SyntheticProject feeds a single synthetic
// project pointing at a temp dir so drift detection runs end-to-end without
// requiring real ~/.agents/projects/ state.
func TestRunWorkflowDriftWithLister_SyntheticProject(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", tmp)
	target := t.TempDir()
	cmd := newDriftTestCommand("", 7, 30)
	stub := func() ([]ManagedProject, error) {
		return []ManagedProject{{Name: "synth", Path: target}}, nil
	}
	out, _ := captureCovStdout(t, func() error {
		return runWorkflowDriftWithLister(cmd, stub)
	})
	if !strings.Contains(out, "Workflow Drift Report") {
		t.Errorf("expected drift report header from synthetic project, got %s", out)
	}
}

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

	driftPlanScanPhase(report, ManagedProject{Name: "p", Path: "/nonexistent"})
	if len(report.CompletedPlanIDs) != 0 {
		t.Fatal("expected no scan when structure missing")
	}
}

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
