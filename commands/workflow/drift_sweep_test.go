package workflow

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/NikashPrakash/dot-agents/internal/config"
)

func TestDetectRepoDrift_Unreachable(t *testing.T) {
	project := ManagedProject{Name: "gone", Path: "/nonexistent/path/does/not/exist"}
	report := detectRepoDrift(project, 7, 30)
	if report.Reachable {
		t.Error("expected unreachable")
	}
	if report.Status != "unreachable" {
		t.Errorf("expected status=unreachable, got %s", report.Status)
	}
}

func TestDetectRepoDrift_FreshProject(t *testing.T) {
	dir := t.TempDir()
	// A brand-new project: no checkpoint, no workflow dir
	project := ManagedProject{Name: "fresh", Path: dir}
	report := detectRepoDrift(project, 7, 30)
	if !report.Reachable {
		t.Error("expected reachable")
	}
	if !report.MissingCheckpoint {
		t.Error("expected missing_checkpoint")
	}
	if !report.MissingWorkflowDir {
		t.Error("expected missing_workflow_dir")
	}
	if report.Status != "warn" {
		t.Errorf("expected warn, got %s", report.Status)
	}
}

func TestDetectRepoDrift_HealthyProject(t *testing.T) {
	dir := t.TempDir()
	// Create a workflow dir, plans dir, and a fresh checkpoint
	if err := os.MkdirAll(filepath.Join(dir, ".agents", "workflow", "plans"), 0755); err != nil {
		t.Fatal(err)
	}
	// Write a recent checkpoint (today)
	projectName := "healthy-proj"
	checkpointDir := filepath.Join(config.AgentsContextDir(), projectName)
	if err := os.MkdirAll(checkpointDir, 0755); err != nil {
		t.Fatal(err)
	}
	checkpointData := []byte("schema_version: 1\ntimestamp: " + time.Now().UTC().Format(time.RFC3339) + "\n")
	checkpointPath := filepath.Join(checkpointDir, "checkpoint.yaml")
	if err := os.WriteFile(checkpointPath, checkpointData, 0644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(checkpointDir) })

	project := ManagedProject{Name: projectName, Path: dir}
	report := detectRepoDrift(project, 7, 30)
	if report.MissingCheckpoint {
		t.Error("should not flag missing checkpoint")
	}
	if report.StaleCheckpoint {
		t.Error("should not flag stale checkpoint for fresh checkpoint")
	}
	if report.Status != "healthy" {
		t.Errorf("expected healthy, got %s — warnings: %v", report.Status, report.Warnings)
	}
}

func TestDetectRepoDrift_StaleCheckpoint(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".agents", "workflow", "plans"), 0755); err != nil {
		t.Fatal(err)
	}
	projectName := "stale-cp-proj"
	checkpointDir := filepath.Join(config.AgentsContextDir(), projectName)
	if err := os.MkdirAll(checkpointDir, 0755); err != nil {
		t.Fatal(err)
	}
	// 30-day-old checkpoint
	oldTime := time.Now().AddDate(0, 0, -30).UTC().Format(time.RFC3339)
	checkpointData := []byte("schema_version: 1\ntimestamp: " + oldTime + "\n")
	if err := os.WriteFile(filepath.Join(checkpointDir, "checkpoint.yaml"), checkpointData, 0644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(checkpointDir) })

	project := ManagedProject{Name: projectName, Path: dir}
	report := detectRepoDrift(project, 7, 30)
	if !report.StaleCheckpoint {
		t.Error("expected stale_checkpoint")
	}
	if report.CheckpointAgeDays < 28 {
		t.Errorf("expected checkpoint age >= 28 days, got %d", report.CheckpointAgeDays)
	}
}

func TestAggregateDrift_Summary(t *testing.T) {
	reports := []RepoDriftReport{
		{Project: ManagedProject{Name: "a"}, Status: "healthy"},
		{Project: ManagedProject{Name: "b"}, Status: "warn", Warnings: []string{"stale checkpoint"}},
		{Project: ManagedProject{Name: "c"}, Status: "unreachable", Warnings: []string{"path missing"}},
	}
	agg := aggregateDrift(reports)
	if agg.HealthyCount != 1 {
		t.Errorf("healthy: want 1, got %d", agg.HealthyCount)
	}
	if agg.WarnCount != 1 {
		t.Errorf("warn: want 1, got %d", agg.WarnCount)
	}
	if agg.UnreachableCount != 1 {
		t.Errorf("unreachable: want 1, got %d", agg.UnreachableCount)
	}
	if len(agg.TopWarnings) != 2 {
		t.Errorf("top_warnings: want 2, got %d", len(agg.TopWarnings))
	}
}

func TestPlanSweep_GeneratesActions(t *testing.T) {
	reports := []RepoDriftReport{
		{
			Project:            ManagedProject{Name: "needs-workflow", Path: "/tmp/x"},
			Reachable:          true,
			MissingWorkflowDir: true,
			MissingCheckpoint:  true,
			Status:             "warn",
		},
	}
	plan := planSweep(reports)
	if len(plan.Actions) == 0 {
		t.Fatal("expected sweep actions")
	}
	// Scaffold workflow dir should be present
	found := false
	for _, a := range plan.Actions {
		if a.Action == SweepActionScaffoldWorkflowDir {
			found = true
			if !a.RequiresConfirmation {
				t.Error("scaffold_workflow_dir should require confirmation")
			}
		}
	}
	if !found {
		t.Error("expected scaffold_workflow_dir action")
	}
}

func TestPlanSweep_UnreachableSkipped(t *testing.T) {
	reports := []RepoDriftReport{
		{Project: ManagedProject{Name: "gone"}, Reachable: false, Status: "unreachable"},
	}
	plan := planSweep(reports)
	if len(plan.Actions) != 0 {
		t.Errorf("expected no actions for unreachable project, got %d", len(plan.Actions))
	}
}

func TestPlanSweep_AllMutatingActionsRequireConfirmation(t *testing.T) {
	reports := []RepoDriftReport{
		{
			Project:              ManagedProject{Name: "x"},
			Reachable:            true,
			MissingWorkflowDir:   true,
			MissingPlanStructure: true,
			Status:               "warn",
		},
	}
	plan := planSweep(reports)
	for _, a := range plan.Actions {
		if a.Action == SweepActionScaffoldWorkflowDir || a.Action == SweepActionCreatePlanStructure {
			if !a.RequiresConfirmation {
				t.Errorf("action %s should require confirmation", a.Action)
			}
		}
	}
}

// ── p4-drift-extension: constructor-level slice init tests ──────────────────

// TestDetectRepoDrift_SliceFieldsNeverNil asserts both new slice fields are
// initialized as []string{} (not nil) even when no plans exist.
func TestDetectRepoDrift_SliceFieldsNeverNil(t *testing.T) {
	dir := t.TempDir()
	project := ManagedProject{Name: "no-plans", Path: dir}
	report := detectRepoDrift(project, 7, 30)
	if report.CompletedPlanIDs == nil {
		t.Error("CompletedPlanIDs must not be nil — JSON must marshal to [] not null")
	}
	if report.InconsistentArchivedPlanIDs == nil {
		t.Error("InconsistentArchivedPlanIDs must not be nil — JSON must marshal to [] not null")
	}
}

// TestDetectRepoDrift_CompletedPlanIDs asserts completed plans are detected.
func TestDetectRepoDrift_CompletedPlanIDs(t *testing.T) {
	dir := t.TempDir()
	plansDir := filepath.Join(dir, ".agents", "workflow", "plans", "my-plan")
	if err := os.MkdirAll(plansDir, 0755); err != nil {
		t.Fatal(err)
	}
	planYAML := []byte("schema_version: 1\nid: my-plan\nstatus: completed\n")
	if err := os.WriteFile(filepath.Join(plansDir, "PLAN.yaml"), planYAML, 0644); err != nil {
		t.Fatal(err)
	}
	project := ManagedProject{Name: "test-proj", Path: dir}
	report := detectRepoDrift(project, 7, 30)
	if len(report.CompletedPlanIDs) != 1 || report.CompletedPlanIDs[0] != "my-plan" {
		t.Errorf("expected CompletedPlanIDs=[my-plan], got %v", report.CompletedPlanIDs)
	}
	if len(report.InconsistentArchivedPlanIDs) != 0 {
		t.Errorf("expected no inconsistent archived plans, got %v", report.InconsistentArchivedPlanIDs)
	}
}

// TestDetectRepoDrift_InconsistentArchivedPlanIDs asserts archived-but-present plans are detected.
func TestDetectRepoDrift_InconsistentArchivedPlanIDs(t *testing.T) {
	dir := t.TempDir()
	plansDir := filepath.Join(dir, ".agents", "workflow", "plans", "old-plan")
	if err := os.MkdirAll(plansDir, 0755); err != nil {
		t.Fatal(err)
	}
	planYAML := []byte("schema_version: 1\nid: old-plan\nstatus: archived\n")
	if err := os.WriteFile(filepath.Join(plansDir, "PLAN.yaml"), planYAML, 0644); err != nil {
		t.Fatal(err)
	}
	project := ManagedProject{Name: "test-proj2", Path: dir}
	report := detectRepoDrift(project, 7, 30)
	if len(report.InconsistentArchivedPlanIDs) != 1 || report.InconsistentArchivedPlanIDs[0] != "old-plan" {
		t.Errorf("expected InconsistentArchivedPlanIDs=[old-plan], got %v", report.InconsistentArchivedPlanIDs)
	}
	if len(report.CompletedPlanIDs) != 0 {
		t.Errorf("expected no completed plans, got %v", report.CompletedPlanIDs)
	}
}

// ── Phase 6: fold-back ───────────────────────────────────────────────────────
