package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPlanSweep_MissingPlanStructureOnly(t *testing.T) {
	reports := []RepoDriftReport{
		{
			Project:              ManagedProject{Name: "needs-plans", Path: "/tmp/x"},
			Reachable:            true,
			MissingPlanStructure: true,
			MissingWorkflowDir:   false,
			Status:               "warn",
		},
	}
	plan := planSweep(reports)
	found := false
	for _, a := range plan.Actions {
		if a.Action == SweepActionCreatePlanStructure {
			found = true
		}
	}
	if !found {
		t.Error("expected create_plan_structure action")
	}
}

func TestPlanSweep_StaleProposalActionPresent(t *testing.T) {
	reports := []RepoDriftReport{
		{
			Project:            ManagedProject{Name: "proposals", Path: "/tmp/p"},
			Reachable:          true,
			StaleProposalCount: 3,
			Status:             "warn",
		},
	}
	plan := planSweep(reports)
	var found *SweepActionItem
	for i, a := range plan.Actions {
		if a.Action == SweepActionFlagStaleProposals {
			found = &plan.Actions[i]
		}
	}
	if found == nil {
		t.Fatal("expected flag_stale_proposals action")
	}
	if found.RequiresConfirmation {
		t.Error("flag_stale_proposals should NOT require confirmation (read-only)")
	}
}

func TestAppendSweepLog_DirCreation(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", tmp)

	logPath := sweepLogPath()
	_ = os.RemoveAll(filepath.Dir(logPath))
	appendSweepLog(SweepLogEntry{Timestamp: time.Now().UTC().Format(time.RFC3339), Project: "p"})
	if _, err := os.Stat(logPath); err != nil {
		t.Errorf("sweep log not created: %v", err)
	}
}

func TestRunWorkflowSweep_WithHealthyProject(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", tmp)
	target := t.TempDir()

	if err := os.MkdirAll(filepath.Join(target, ".agents", "workflow", "plans"), 0755); err != nil {
		t.Fatal(err)
	}

	chDir := filepath.Join(tmp, "context", "healthy-x")
	if err := os.MkdirAll(chDir, 0755); err != nil {
		t.Fatal(err)
	}
	cp := []byte("schema_version: 1\ntimestamp: " + time.Now().UTC().Format(time.RFC3339) + "\n")
	if err := os.WriteFile(filepath.Join(chDir, "checkpoint.yaml"), cp, 0644); err != nil {
		t.Fatal(err)
	}
	if err := seedManagedProject(tmp, "healthy-x", target); err != nil {
		t.Fatal(err)
	}
	cmd := newSweepTestCommand(false, 7, 30)
	out, _ := captureCovStdout(t, func() error { return runWorkflowSweep(cmd, nil) })
	if !strings.Contains(out, "No sweep actions needed") {
		t.Errorf("expected 'No sweep actions needed', got %s", out)
	}
}

func TestRunWorkflowSweep_DryRunGeneratesPlan(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", tmp)
	target := t.TempDir()

	if err := seedManagedProject(tmp, "drift-x", target); err != nil {
		t.Fatal(err)
	}
	cmd := newSweepTestCommand(false, 7, 30)
	out, _ := captureCovStdout(t, func() error { return runWorkflowSweep(cmd, nil) })
	if !strings.Contains(out, "Sweep Plan") {
		t.Errorf("expected 'Sweep Plan' header in output, got: %s", out)
	}
}

// TestRunWorkflowSweepWithLister_ListerError drives the previously
// unreachable load-projects failure branch by passing a stub lister that
// returns a non-nil error.
func TestRunWorkflowSweepWithLister_ListerError(t *testing.T) {
	cmd := newSweepTestCommand(false, 7, 30)
	stub := func() ([]ManagedProject, error) {
		return nil, errSentinelDriftLister
	}
	err := runWorkflowSweepWithLister(cmd, stub, nil)
	if err == nil {
		t.Fatal("expected lister error to propagate")
	}
	if !strings.Contains(err.Error(), "load managed projects") {
		t.Errorf("expected wrapped error, got %v", err)
	}
}

// TestRunWorkflowSweepWithLister_EmptySlice triggers the no-projects notice
// path without touching the global config tree.
func TestRunWorkflowSweepWithLister_EmptySlice(t *testing.T) {
	cmd := newSweepTestCommand(false, 7, 30)
	stub := func() ([]ManagedProject, error) {
		return []ManagedProject{}, nil
	}
	out, _ := captureCovStdout(t, func() error {
		return runWorkflowSweepWithLister(cmd, stub, nil)
	})
	if !strings.Contains(out, "No managed projects") {
		t.Errorf("expected no-projects notice, got %s", out)
	}
}

// TestRunWorkflowSweepWithLister_ApplyWithConfirmer drives apply-mode end
// to end against a synthetic project, declining the scaffold-dir prompt via
// an injected reader so the apply branch and its loop body are both
// exercised without seam swap.
func TestRunWorkflowSweepWithLister_ApplyWithConfirmer(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", tmp)
	target := t.TempDir()
	cmd := newSweepTestCommand(true, 7, 30)
	stub := func() ([]ManagedProject, error) {
		return []ManagedProject{{Name: "synth-apply", Path: target}}, nil
	}

	oldYes := deps.Flags.Yes
	deps.Flags.Yes = func() bool { return false }
	t.Cleanup(func() { deps.Flags.Yes = oldYes })
	out, _ := captureCovStdout(t, func() error {
		return runWorkflowSweepWithLister(cmd, stub, strings.NewReader("n\n"))
	})
	if !strings.Contains(out, "Sweep complete") {
		t.Errorf("expected 'Sweep complete' summary, got %s", out)
	}
}

func TestConfirmSweepAction_YesFlagBypasses(t *testing.T) {
	old := deps.Flags.Yes
	deps.Flags.Yes = func() bool { return true }
	t.Cleanup(func() { deps.Flags.Yes = old })

	action := SweepActionItem{
		Project: ManagedProject{Name: "p"}, Action: SweepActionCreateCheckpointReminder,
		RequiresConfirmation: true, Description: "test reminder",
	}
	if !confirmSweepAction(action, nil) {
		t.Error("expected confirm with --yes to return true even when confirmation required")
	}
}

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

// Covers the SweepActionArchiveCompletedPlans branch: the sweep performs the
// on-disk plans/->history/ move but passes noCommit=true (the archive commit
// helper is cwd-bound and would target the wrong repo cross-project), so the
// move lands uncommitted. Asserts the plan is archived without a git commit.
func TestApplySweepAction_ArchiveCompletedPlan(t *testing.T) {
	repo := t.TempDir()
	setupArchivePlan(t, repo, "sweepable", "completed")
	srcDir := filepath.Join(repo, ".agents", "workflow", "plans", "sweepable")
	dstDir := filepath.Join(repo, ".agents", "history", "sweepable")

	action := SweepActionItem{
		Project: ManagedProject{Name: "p", Path: repo},
		Action:  SweepActionArchiveCompletedPlans,
		PlanID:  "sweepable",
	}
	if err := applySweepAction(action); err != nil {
		t.Fatalf("applySweepAction(archive): %v", err)
	}
	if _, err := os.Stat(srcDir); !os.IsNotExist(err) {
		t.Error("source plan dir should be removed after sweep archive")
	}
	if _, err := os.Stat(dstDir); err != nil {
		t.Errorf("history dir should exist after sweep archive: %v", err)
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
	if !confirmSweepAction(action, nil) {
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
	if !confirmSweepAction(action, nil) {
		t.Error("expected confirmSweepAction to return true when no confirmation required")
	}
}

func TestAppendSweepLog_OpenFileErrorSilent(t *testing.T) {

	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", filepath.Join(blocker, "child"))

	appendSweepLog(SweepLogEntry{Action: "x"})
}

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
