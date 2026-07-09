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

// TestDriftCheckpointPhase_MalformedYAML drives the early-return in
// driftCheckpointPhase (drift.go:86-88) when the checkpoint file exists but
// fails to unmarshal cleanly. The function must return silently — no warning
// added, no panic — so the rest of drift detection continues.
func TestDriftCheckpointPhase_MalformedYAML(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", tmp)

	// Write a malformed checkpoint.yaml under the project's context dir.
	ctx := filepath.Join(tmp, "context", "bad-cp")
	if err := os.MkdirAll(ctx, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ctx, "checkpoint.yaml"), []byte(":\n  - not valid yaml ["), 0644); err != nil {
		t.Fatal(err)
	}
	rep := &RepoDriftReport{CheckpointAgeDays: -1}
	driftCheckpointPhase(rep, ManagedProject{Name: "bad-cp"}, 7)
	// Malformed YAML must NOT count as MissingCheckpoint (the file did exist).
	if rep.MissingCheckpoint {
		t.Error("MissingCheckpoint should be false when file exists (even if malformed)")
	}
	// And must NOT set a stale-checkpoint warning since timestamp never resolved.
	if rep.StaleCheckpoint {
		t.Error("StaleCheckpoint should be false on malformed yaml")
	}
}

// TestDriftCheckpointPhase_InvalidTimestamp drives the time.Parse error
// branch (drift.go:90-92). Checkpoint unmarshals fine but the timestamp is
// not RFC3339, so age computation is skipped and the function returns
// silently.
func TestDriftCheckpointPhase_InvalidTimestamp(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", tmp)

	ctx := filepath.Join(tmp, "context", "bad-ts")
	if err := os.MkdirAll(ctx, 0755); err != nil {
		t.Fatal(err)
	}
	// Valid YAML but bogus timestamp.
	if err := os.WriteFile(filepath.Join(ctx, "checkpoint.yaml"), []byte("timestamp: 'not-a-date'\n"), 0644); err != nil {
		t.Fatal(err)
	}
	rep := &RepoDriftReport{CheckpointAgeDays: -1}
	driftCheckpointPhase(rep, ManagedProject{Name: "bad-ts"}, 7)
	if rep.MissingCheckpoint {
		t.Error("MissingCheckpoint should be false when file exists")
	}
	if rep.StaleCheckpoint {
		t.Error("StaleCheckpoint should be false on invalid timestamp")
	}
	if rep.CheckpointAgeDays != -1 {
		t.Errorf("CheckpointAgeDays should remain -1 when timestamp unparseable, got %d", rep.CheckpointAgeDays)
	}
}

// TestDriftWorkflowDirPhase_WorkflowPresentPlansMissing drives the
// `if !report.MissingWorkflowDir` branch (drift.go:127-129). The project has
// .agents/workflow/ but no .agents/workflow/plans/ — the warning must mention
// the missing plans subdir specifically and MissingPlanStructure must flip
// without MissingWorkflowDir flipping.
func TestDriftWorkflowDirPhase_WorkflowPresentPlansMissing(t *testing.T) {
	target := t.TempDir()
	// Create .agents/workflow but NOT .agents/workflow/plans
	if err := os.MkdirAll(filepath.Join(target, ".agents", "workflow"), 0755); err != nil {
		t.Fatal(err)
	}
	rep := &RepoDriftReport{}
	driftWorkflowDirPhase(rep, ManagedProject{Path: target})

	if rep.MissingWorkflowDir {
		t.Error("MissingWorkflowDir should be false when .agents/workflow/ exists")
	}
	if !rep.MissingPlanStructure {
		t.Error("MissingPlanStructure should be true when plans/ is missing")
	}
	if len(rep.Warnings) != 1 {
		t.Fatalf("expected exactly one warning, got %v", rep.Warnings)
	}
	if !strings.Contains(rep.Warnings[0], "no canonical plans") {
		t.Errorf("expected canonical-plans warning, got %q", rep.Warnings[0])
	}
}

// TestDriftPlanScanPhase_SkipsNonDirAndUnreadablePlanFile drives two
// continue branches in driftPlanScanPhase: non-directory entries
// (drift.go:154-155) and entries where PLAN.yaml cannot be read
// (drift.go:159-160). Neither path should record any plan status drift.
func TestDriftPlanScanPhase_SkipsNonDirAndUnreadablePlanFile(t *testing.T) {
	repo := t.TempDir()
	plansDir := filepath.Join(repo, ".agents", "workflow", "plans")
	if err := os.MkdirAll(plansDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Non-dir entry inside plans/ — must be skipped.
	if err := os.WriteFile(filepath.Join(plansDir, "stray-file.md"), []byte("not a plan dir"), 0644); err != nil {
		t.Fatal(err)
	}
	// Plan directory with NO PLAN.yaml — os.ReadFile must error and the entry
	// must be skipped (no panic, no recorded status).
	if err := os.MkdirAll(filepath.Join(plansDir, "no-plan-file"), 0755); err != nil {
		t.Fatal(err)
	}

	rep := &RepoDriftReport{}
	driftPlanScanPhase(rep, ManagedProject{Path: repo})
	if len(rep.CompletedPlanIDs) != 0 {
		t.Errorf("expected no completed plans recorded, got %v", rep.CompletedPlanIDs)
	}
	if len(rep.InconsistentArchivedPlanIDs) != 0 {
		t.Errorf("expected no archived inconsistencies recorded, got %v", rep.InconsistentArchivedPlanIDs)
	}
}

// TestDriftCheckpointPhase_UnreadableCheckpoint drives the REAL-read-error
// branch (drift.go:94-99): checkpoint.yaml exists but os.ReadFile fails with
// something other than IsNotExist. The warning must name the file as
// unreadable, distinct from the generic "no checkpoint found" message used
// for legitimate absence.
func TestDriftCheckpointPhase_UnreadableCheckpoint(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", tmp)

	ctx := filepath.Join(tmp, "context", "unreadable-cp")
	if err := os.MkdirAll(ctx, 0755); err != nil {
		t.Fatal(err)
	}
	cpPath := filepath.Join(ctx, "checkpoint.yaml")
	if err := os.WriteFile(cpPath, []byte("timestamp: '2020-01-01T00:00:00Z'\n"), 0644); err != nil {
		t.Fatal(err)
	}
	chmodUnreadable(t, cpPath)

	rep := &RepoDriftReport{CheckpointAgeDays: -1}
	driftCheckpointPhase(rep, ManagedProject{Name: "unreadable-cp"}, 7)
	if !rep.MissingCheckpoint {
		t.Error("expected MissingCheckpoint true even on a real read error")
	}
	found := false
	for _, w := range rep.Warnings {
		if strings.Contains(w, "checkpoint.yaml unreadable") {
			found = true
		}
		if w == "no checkpoint found" {
			t.Error("real read error must not be reported as the generic absence message")
		}
	}
	if !found {
		t.Errorf("expected an 'unreadable' warning, got %v", rep.Warnings)
	}
}

// TestDriftPlanScanPhase_UnreadablePlansDir drives the real ReadDir-error
// branch (drift.go:167-173): workflow/plans/ exists (MissingPlanStructure
// stays false) but cannot be enumerated. The scan must warn instead of
// silently reporting zero plans.
func TestDriftPlanScanPhase_UnreadablePlansDir(t *testing.T) {
	repo := t.TempDir()
	plansDir := filepath.Join(repo, ".agents", "workflow", "plans")
	if err := os.MkdirAll(plansDir, 0755); err != nil {
		t.Fatal(err)
	}
	chmodUnreadableDir(t, plansDir)

	rep := &RepoDriftReport{}
	driftPlanScanPhase(rep, ManagedProject{Path: repo})
	found := false
	for _, w := range rep.Warnings {
		if strings.Contains(w, "could not scan workflow/plans/") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a 'could not scan' warning, got %v", rep.Warnings)
	}
}

// TestDriftPlanScanPhase_UnreadablePlanFile drives the real ReadFile-error
// branch on an individual PLAN.yaml (drift.go:181-186): the plan directory
// exists but its PLAN.yaml cannot be read (not "doesn't exist"). The scan
// must warn and continue to the next entry, not skip silently.
func TestDriftPlanScanPhase_UnreadablePlanFile(t *testing.T) {
	repo := t.TempDir()
	planDir := filepath.Join(repo, ".agents", "workflow", "plans", "locked-plan")
	if err := os.MkdirAll(planDir, 0755); err != nil {
		t.Fatal(err)
	}
	planFile := filepath.Join(planDir, "PLAN.yaml")
	if err := os.WriteFile(planFile, []byte("status: active\n"), 0644); err != nil {
		t.Fatal(err)
	}
	chmodUnreadable(t, planFile)

	rep := &RepoDriftReport{}
	driftPlanScanPhase(rep, ManagedProject{Path: repo})
	found := false
	for _, w := range rep.Warnings {
		if strings.Contains(w, "could not read locked-plan/PLAN.yaml") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a 'could not read' warning, got %v", rep.Warnings)
	}
	if len(rep.CompletedPlanIDs) != 0 || len(rep.InconsistentArchivedPlanIDs) != 0 {
		t.Error("unreadable plan file must not record a status")
	}
}

// TestDriftPlanScanPhase_MalformedPlanYAML drives extractPlanStatusChecked's
// error branch (drift.go:188-191): PLAN.yaml exists and reads fine but fails
// to parse as YAML. The scan must warn distinctly from "could not read" and
// must not record a status for that plan.
func TestDriftPlanScanPhase_MalformedPlanYAML(t *testing.T) {
	repo := t.TempDir()
	planDir := filepath.Join(repo, ".agents", "workflow", "plans", "corrupt-plan")
	if err := os.MkdirAll(planDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(planDir, "PLAN.yaml"), []byte("status: [unterminated"), 0644); err != nil {
		t.Fatal(err)
	}

	rep := &RepoDriftReport{}
	driftPlanScanPhase(rep, ManagedProject{Path: repo})
	found := false
	for _, w := range rep.Warnings {
		if strings.Contains(w, "could not parse corrupt-plan/PLAN.yaml") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a 'could not parse' warning, got %v", rep.Warnings)
	}
	if len(rep.CompletedPlanIDs) != 0 || len(rep.InconsistentArchivedPlanIDs) != 0 {
		t.Error("malformed PLAN.yaml must not record a status")
	}
}
