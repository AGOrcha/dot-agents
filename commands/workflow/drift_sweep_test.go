package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

// writeMinimalPlanYAML mkdirs <dir>/.agents/workflow/plans/<planID>/
// and writes PLAN.yaml with the supplied status.
func writeMinimalPlanYAML(t *testing.T, dir, planID, status string) {
	t.Helper()
	plansDir := filepath.Join(dir, ".agents", "workflow", "plans", planID)
	if err := os.MkdirAll(plansDir, 0755); err != nil {
		t.Fatal(err)
	}
	planYAML := []byte("schema_version: 1\nid: " + planID + "\nstatus: " + status + "\n")
	if err := os.WriteFile(filepath.Join(plansDir, "PLAN.yaml"), planYAML, 0644); err != nil {
		t.Fatal(err)
	}
}

// TestDetectRepoDrift_CompletedPlanIDs asserts completed plans are detected.
func TestDetectRepoDrift_CompletedPlanIDs(t *testing.T) {
	dir := t.TempDir()
	writeMinimalPlanYAML(t, dir, "my-plan", "completed")
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
	writeMinimalPlanYAML(t, dir, "old-plan", "archived")
	project := ManagedProject{Name: "test-proj2", Path: dir}
	report := detectRepoDrift(project, 7, 30)
	if len(report.InconsistentArchivedPlanIDs) != 1 || report.InconsistentArchivedPlanIDs[0] != "old-plan" {
		t.Errorf("expected InconsistentArchivedPlanIDs=[old-plan], got %v", report.InconsistentArchivedPlanIDs)
	}
	if len(report.CompletedPlanIDs) != 0 {
		t.Errorf("expected no completed plans, got %v", report.CompletedPlanIDs)
	}
}

// ── p6-tests: CompletedPlanIDs + InconsistentArchivedPlanIDs behavior ─────────

// TestDetectRepoDrift_BothFieldsPopulated verifies both fields are populated when
// a fixture has one plan of each kind.
func TestDetectRepoDrift_BothFieldsPopulated(t *testing.T) {
	dir := t.TempDir()
	plansDir := filepath.Join(dir, ".agents", "workflow", "plans")

	for planID, status := range map[string]string{
		"plan-done":   "completed",
		"plan-stray":  "archived",
		"plan-active": "active",
	} {
		d := filepath.Join(plansDir, planID)
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
		content := []byte("schema_version: 1\nid: " + planID + "\nstatus: " + status + "\n")
		if err := os.WriteFile(filepath.Join(d, "PLAN.yaml"), content, 0644); err != nil {
			t.Fatal(err)
		}
	}

	project := ManagedProject{Name: "mixed", Path: dir}
	report := detectRepoDrift(project, 7, 30)

	if len(report.CompletedPlanIDs) != 1 || report.CompletedPlanIDs[0] != "plan-done" {
		t.Errorf("CompletedPlanIDs = %v, want [plan-done]", report.CompletedPlanIDs)
	}
	if len(report.InconsistentArchivedPlanIDs) != 1 || report.InconsistentArchivedPlanIDs[0] != "plan-stray" {
		t.Errorf("InconsistentArchivedPlanIDs = %v, want [plan-stray]", report.InconsistentArchivedPlanIDs)
	}
}

// TestDetectRepoDrift_EmptyFieldsWhenNoMatchingPlans confirms both slice fields
// remain empty (not nil) when there are only active plans.
func TestDetectRepoDrift_EmptyFieldsWhenNoMatchingPlans(t *testing.T) {
	dir := t.TempDir()
	plansDir := filepath.Join(dir, ".agents", "workflow", "plans", "plan-active")
	if err := os.MkdirAll(plansDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plansDir, "PLAN.yaml"),
		[]byte("schema_version: 1\nid: plan-active\nstatus: active\n"), 0644); err != nil {
		t.Fatal(err)
	}

	project := ManagedProject{Name: "only-active", Path: dir}
	report := detectRepoDrift(project, 7, 30)

	if report.CompletedPlanIDs == nil {
		t.Error("CompletedPlanIDs must not be nil")
	}
	if len(report.CompletedPlanIDs) != 0 {
		t.Errorf("expected no completed plans, got %v", report.CompletedPlanIDs)
	}
	if report.InconsistentArchivedPlanIDs == nil {
		t.Error("InconsistentArchivedPlanIDs must not be nil")
	}
	if len(report.InconsistentArchivedPlanIDs) != 0 {
		t.Errorf("expected no inconsistent plans, got %v", report.InconsistentArchivedPlanIDs)
	}
}

// TestPlanSweep_ArchiveCompletedPlansAction verifies that planSweep emits one
// SweepActionArchiveCompletedPlans action per CompletedPlanID.
func TestPlanSweep_ArchiveCompletedPlansAction(t *testing.T) {
	reports := []RepoDriftReport{
		{
			Project:          ManagedProject{Name: "proj", Path: "/tmp/proj"},
			Reachable:        true,
			CompletedPlanIDs: []string{"plan-alpha", "plan-beta"},
			Status:           "warn",
		},
	}
	plan := planSweep(reports)

	var archiveActions []SweepActionItem
	for _, a := range plan.Actions {
		if a.Action == SweepActionArchiveCompletedPlans {
			archiveActions = append(archiveActions, a)
		}
	}
	if len(archiveActions) != 2 {
		t.Errorf("expected 2 archive_completed_plans actions, got %d", len(archiveActions))
	}
	for _, a := range archiveActions {
		if !a.RequiresConfirmation {
			t.Errorf("archive_completed_plans action should require confirmation (destructive)")
		}
	}
}

// TestPlanSweep_NoArchiveActionsForCleanProject verifies no archive actions are
// emitted when CompletedPlanIDs is empty.
func TestPlanSweep_NoArchiveActionsForCleanProject(t *testing.T) {
	reports := []RepoDriftReport{
		{
			Project:          ManagedProject{Name: "clean", Path: "/tmp/clean"},
			Reachable:        true,
			CompletedPlanIDs: []string{},
			Status:           "healthy",
		},
	}
	plan := planSweep(reports)
	for _, a := range plan.Actions {
		if a.Action == SweepActionArchiveCompletedPlans {
			t.Error("expected no archive_completed_plans for healthy project")
		}
	}
}

// ── Phase 6: fold-back ───────────────────────────────────────────────────────

// ── pr3b coverage: helpers that previously had no direct tests ───────────────

func TestExtractPlanStatus(t *testing.T) {
	cases := []struct {
		name string
		data string
		want string
	}{
		{"active", "schema_version: 1\nid: p\nstatus: active\n", "active"},
		{"completed", "status: completed\n", "completed"},
		{"missing-status", "schema_version: 1\nid: p\n", ""},
		{"empty", "", ""},
		{"malformed", "::not yaml::\n  - [", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractPlanStatus([]byte(tc.data)); got != tc.want {
				t.Errorf("extractPlanStatus(%q) = %q, want %q", tc.data, got, tc.want)
			}
		})
	}
}

func TestJoinIDs(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want string
	}{
		{"empty", []string{}, ""},
		{"single", []string{"a"}, "a"},
		{"multi", []string{"a", "b", "c"}, "a, b, c"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := joinIDs(tc.in); got != tc.want {
				t.Errorf("joinIDs(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestAggregateDrift_EmptyReports(t *testing.T) {
	agg := aggregateDrift(nil)
	if agg.TotalProjects != 0 {
		t.Errorf("TotalProjects = %d, want 0", agg.TotalProjects)
	}
	if agg.HealthyCount+agg.WarnCount+agg.UnreachableCount != 0 {
		t.Error("expected all counts to be zero on empty input")
	}
	if agg.Timestamp == "" {
		t.Error("expected timestamp populated even on empty input")
	}
}

func TestAggregateDrift_DeduplicatesTopWarnings(t *testing.T) {
	reports := []RepoDriftReport{
		{Project: ManagedProject{Name: "a"}, Status: "warn", Warnings: []string{"shared warning"}},
		{Project: ManagedProject{Name: "b"}, Status: "warn", Warnings: []string{"shared warning", "unique-b"}},
	}
	agg := aggregateDrift(reports)
	if len(agg.TopWarnings) != 2 {
		t.Errorf("top warnings dedup: got %d, want 2 (one shared, one unique)", len(agg.TopWarnings))
	}
}

func TestFilterDriftProjects(t *testing.T) {
	all := []ManagedProject{
		{Name: "alpha", Path: "/tmp/a"},
		{Name: "beta", Path: "/tmp/b"},
	}
	t.Run("empty-filter-returns-all", func(t *testing.T) {
		got, err := filterDriftProjects(all, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 {
			t.Errorf("got %d, want 2", len(got))
		}
	})
	t.Run("matching-filter", func(t *testing.T) {
		got, err := filterDriftProjects(all, "beta")
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].Name != "beta" {
			t.Errorf("got %v, want [beta]", got)
		}
	})
	t.Run("missing-filter-errors", func(t *testing.T) {
		_, err := filterDriftProjects(all, "gamma")
		if err == nil {
			t.Error("expected error for missing filter")
		}
	})
}

func TestDriftStatusBadge(t *testing.T) {
	for _, s := range []string{"warn", "unreachable", "healthy", "unknown-default"} {
		got := driftStatusBadge(s)
		if got == "" {
			t.Errorf("driftStatusBadge(%q) = empty", s)
		}
	}
}

func TestSaveDriftReportRoundTrip(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("AGENTS_HOME", tmpHome)

	agg := AggregateDriftReport{
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		TotalProjects: 1,
		HealthyCount:  1,
		Reports: []RepoDriftReport{
			{Project: ManagedProject{Name: "x", Path: "/tmp/x"}, Status: "healthy"},
		},
	}
	if err := saveDriftReport(agg); err != nil {
		t.Fatalf("saveDriftReport: %v", err)
	}
	data, err := os.ReadFile(driftReportPath())
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	if len(data) == 0 {
		t.Error("expected non-empty report")
	}
}

func TestApplySweepAction_AllBranches(t *testing.T) {
	dir := t.TempDir()
	project := ManagedProject{Name: "p", Path: dir}

	t.Run("scaffold-workflow-dir", func(t *testing.T) {
		err := applySweepAction(SweepActionItem{
			Project: project, Action: SweepActionScaffoldWorkflowDir,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(dir, ".agents", "workflow")); err != nil {
			t.Errorf("workflow dir not created: %v", err)
		}
	})

	t.Run("create-plan-structure", func(t *testing.T) {
		err := applySweepAction(SweepActionItem{
			Project: project, Action: SweepActionCreatePlanStructure,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(dir, ".agents", "workflow", "plans")); err != nil {
			t.Errorf("plans dir not created: %v", err)
		}
	})

	t.Run("informational-actions-no-op", func(t *testing.T) {
		for _, a := range []SweepActionType{SweepActionCreateCheckpointReminder, SweepActionFlagStaleProposals} {
			if err := applySweepAction(SweepActionItem{Project: project, Action: a}); err != nil {
				t.Errorf("informational action %s returned error: %v", a, err)
			}
		}
	})

	t.Run("unknown-action-errors", func(t *testing.T) {
		err := applySweepAction(SweepActionItem{Project: project, Action: SweepActionType("bogus")})
		if err == nil {
			t.Error("expected error for unknown action")
		}
	})
}

func TestSweepLogEntryFields(t *testing.T) {
	action := SweepActionItem{
		Project: ManagedProject{Name: "proj"}, Action: SweepActionScaffoldWorkflowDir, Description: "desc",
	}
	entry := sweepLogEntry(action, true, false)
	if entry.Project != "proj" || entry.Action != SweepActionScaffoldWorkflowDir || entry.Description != "desc" {
		t.Errorf("entry fields not populated: %+v", entry)
	}
	if !entry.Applied || entry.DryRun {
		t.Errorf("expected Applied=true DryRun=false, got %+v", entry)
	}
	if entry.Timestamp == "" {
		t.Error("expected timestamp populated")
	}
}

func TestAppendSweepLog_AppendsJSONL(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", tmp)

	for i := 0; i < 3; i++ {
		appendSweepLog(SweepLogEntry{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Project:   "p", Action: SweepActionScaffoldWorkflowDir, Description: "d", DryRun: true,
		})
	}
	data, err := os.ReadFile(sweepLogPath())
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 jsonl lines, got %d", len(lines))
	}
	for _, line := range lines {
		var entry SweepLogEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Errorf("invalid jsonl line %q: %v", line, err)
		}
	}
}

// drift report saved to context dir uses AGENTS_HOME — verify aggregate path stable.
func TestDriftReportPathUnderContextDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", tmp)
	p := driftReportPath()
	if !strings.HasSuffix(p, "drift-report.json") {
		t.Errorf("expected drift-report.json suffix, got %s", p)
	}
}
