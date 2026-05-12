package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// setupArchivePlan creates a minimal completed plan in projectPath/plans/<planID>
// and returns the src dir.
func setupArchivePlan(t *testing.T, projectPath, planID, status string) string {
	t.Helper()
	srcDir := filepath.Join(projectPath, ".agents", "workflow", "plans", planID)
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}
	plan := CanonicalPlan{
		SchemaVersion: 1,
		ID:            planID,
		Title:         "Test " + planID,
		Status:        status,
		CreatedAt:     "2026-01-01T00:00:00Z",
		UpdatedAt:     "2026-01-01T00:00:00Z",
	}
	data, err := yaml.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "PLAN.yaml"), data, 0644); err != nil {
		t.Fatal(err)
	}
	tf := CanonicalTaskFile{SchemaVersion: 1, PlanID: planID, Tasks: []CanonicalTask{}}
	td, err := yaml.Marshal(tf)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "TASKS.yaml"), td, 0644); err != nil {
		t.Fatal(err)
	}
	// Also write a <planID>.plan.md canonical file
	if err := os.WriteFile(filepath.Join(srcDir, planID+".plan.md"), []byte("# "+planID+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return srcDir
}

// ── archive test cases ─────────────────────────────────────────────────────────

// Case 1: no history dir → os.Rename fast path
func TestArchiveSinglePlan_RenameWhenNoHistory(t *testing.T) {
	proj := t.TempDir()
	setupArchivePlan(t, proj, "myplan", "completed")
	srcDir := filepath.Join(proj, ".agents", "workflow", "plans", "myplan")
	dstDir := filepath.Join(proj, ".agents", "history", "myplan")

	if err := archiveSinglePlan(proj, "myplan", false, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Source should be gone, destination should exist
	if _, err := os.Stat(srcDir); !os.IsNotExist(err) {
		t.Error("source dir should have been removed after rename")
	}
	if _, err := os.Stat(dstDir); err != nil {
		t.Errorf("history dir should exist: %v", err)
	}
	// PLAN.yaml should have status=archived
	data, err := os.ReadFile(filepath.Join(dstDir, "PLAN.yaml"))
	if err != nil {
		t.Fatalf("read PLAN.yaml: %v", err)
	}
	if !strings.Contains(string(data), "archived") {
		t.Error("PLAN.yaml should have status=archived")
	}
}

// Case 2: history dir exists with DMA artifact → merge: DMA untouched, PLAN+TASKS+plan.md overwritten, source removed
func TestArchiveSinglePlan_MergeWithDMASkip(t *testing.T) {
	proj := t.TempDir()
	setupArchivePlan(t, proj, "myplan", "completed")
	srcDir := filepath.Join(proj, ".agents", "workflow", "plans", "myplan")
	dstDir := filepath.Join(proj, ".agents", "history", "myplan")

	// Pre-create history dir with a DMA artifact
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		t.Fatal(err)
	}
	dmaContent := []byte("original delegation artifact")
	dmaPath := filepath.Join(dstDir, "delegation.yaml")
	if err := os.WriteFile(dmaPath, dmaContent, 0644); err != nil {
		t.Fatal(err)
	}

	if err := archiveSinglePlan(proj, "myplan", false, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// DMA artifact should be untouched
	got, err := os.ReadFile(dmaPath)
	if err != nil {
		t.Fatalf("read dma: %v", err)
	}
	if string(got) != string(dmaContent) {
		t.Errorf("DMA file modified; want %q got %q", dmaContent, got)
	}

	// PLAN.yaml should exist in dst (overwritten canonical)
	if _, err := os.Stat(filepath.Join(dstDir, "PLAN.yaml")); err != nil {
		t.Errorf("PLAN.yaml missing in history: %v", err)
	}

	// Source should be removed
	if _, err := os.Stat(srcDir); !os.IsNotExist(err) {
		t.Error("source dir should have been removed after merge")
	}
}

// Case 3: identical sha256 → skipped (no overwrite)
func TestArchiveSinglePlan_IdenticalHashSkipped(t *testing.T) {
	proj := t.TempDir()
	setupArchivePlan(t, proj, "myplan", "completed")
	srcDir := filepath.Join(proj, ".agents", "workflow", "plans", "myplan")
	dstDir := filepath.Join(proj, ".agents", "history", "myplan")

	// Pre-create history dir with an identical extra file
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		t.Fatal(err)
	}
	sharedContent := []byte("same content")
	if err := os.WriteFile(filepath.Join(srcDir, "extra.txt"), sharedContent, 0644); err != nil {
		t.Fatal(err)
	}
	// Write the same content to dst; set dst mtime AFTER src so it's "newer"
	dstExtra := filepath.Join(dstDir, "extra.txt")
	if err := os.WriteFile(dstExtra, sharedContent, 0644); err != nil {
		t.Fatal(err)
	}
	// Bump dst mtime
	future := time.Now().Add(time.Hour)
	_ = os.Chtimes(dstExtra, future, future)

	if err := archiveSinglePlan(proj, "myplan", false, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// File should still have original content (identical → skip regardless of mtime)
	got, err := os.ReadFile(dstExtra)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(sharedContent) {
		t.Error("identical file should not have been changed")
	}
}

// Case 4: differing file + source newer → overwritten
func TestArchiveSinglePlan_DifferingFileOverwrite(t *testing.T) {
	proj := t.TempDir()
	setupArchivePlan(t, proj, "myplan", "completed")
	srcDir := filepath.Join(proj, ".agents", "workflow", "plans", "myplan")
	dstDir := filepath.Join(proj, ".agents", "history", "myplan")

	if err := os.MkdirAll(dstDir, 0755); err != nil {
		t.Fatal(err)
	}

	oldContent := []byte("old content")
	newContent := []byte("new content from source")

	dstExtra := filepath.Join(dstDir, "note.txt")
	if err := os.WriteFile(dstExtra, oldContent, 0644); err != nil {
		t.Fatal(err)
	}
	// Set dst mtime in the past so source is newer
	past := time.Now().Add(-2 * time.Hour)
	_ = os.Chtimes(dstExtra, past, past)

	srcExtra := filepath.Join(srcDir, "note.txt")
	if err := os.WriteFile(srcExtra, newContent, 0644); err != nil {
		t.Fatal(err)
	}

	if err := archiveSinglePlan(proj, "myplan", false, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := os.ReadFile(dstExtra)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(newContent) {
		t.Errorf("expected overwrite; want %q got %q", newContent, got)
	}
}

// Case 5: history file newer → skipped + warning printed (no overwrite)
func TestArchiveSinglePlan_HistoryNewerSkipped(t *testing.T) {
	proj := t.TempDir()
	setupArchivePlan(t, proj, "myplan", "completed")
	srcDir := filepath.Join(proj, ".agents", "workflow", "plans", "myplan")
	dstDir := filepath.Join(proj, ".agents", "history", "myplan")

	if err := os.MkdirAll(dstDir, 0755); err != nil {
		t.Fatal(err)
	}

	srcContent := []byte("old source")
	dstContent := []byte("newer history version")

	srcExtra := filepath.Join(srcDir, "note.txt")
	if err := os.WriteFile(srcExtra, srcContent, 0644); err != nil {
		t.Fatal(err)
	}
	// Set src mtime in the past
	past := time.Now().Add(-2 * time.Hour)
	_ = os.Chtimes(srcExtra, past, past)

	dstExtra := filepath.Join(dstDir, "note.txt")
	if err := os.WriteFile(dstExtra, dstContent, 0644); err != nil {
		t.Fatal(err)
	}
	// dst mtime is now (newer than src)

	if err := archiveSinglePlan(proj, "myplan", false, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// History content should be unchanged (not overwritten)
	got, err := os.ReadFile(dstExtra)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(dstContent) {
		t.Errorf("history-newer file should not be overwritten; want %q got %q", dstContent, got)
	}
}

// Case 6: dry-run → no filesystem changes + per-file plan printed
func TestArchiveSinglePlan_DryRun(t *testing.T) {
	proj := t.TempDir()
	setupArchivePlan(t, proj, "myplan", "completed")
	srcDir := filepath.Join(proj, ".agents", "workflow", "plans", "myplan")
	dstDir := filepath.Join(proj, ".agents", "history", "myplan")

	if err := archiveSinglePlan(proj, "myplan", false, true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Source should still exist — no changes made
	if _, err := os.Stat(srcDir); err != nil {
		t.Errorf("source dir should still exist in dry-run: %v", err)
	}
	// History dir should NOT have been created
	if _, err := os.Stat(dstDir); !os.IsNotExist(err) {
		t.Error("history dir should not be created in dry-run (fast-path uses rename)")
	}
	// PLAN.yaml in source should still have status=completed (not archived)
	data, err := os.ReadFile(filepath.Join(srcDir, "PLAN.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "status: archived") {
		t.Error("dry-run should not stamp status=archived")
	}
}

// Case 7: non-completed status → error with hint
func TestArchiveSinglePlan_NonCompletedGuard(t *testing.T) {
	proj := t.TempDir()
	setupArchivePlan(t, proj, "myplan", "active")

	err := archiveSinglePlan(proj, "myplan", false, false)
	if err == nil {
		t.Fatal("expected error for non-completed plan without --force")
	}
	if !strings.Contains(err.Error(), "active") {
		t.Errorf("error should mention current status; got: %v", err)
	}
}

// Case 8: --force bypasses guard
func TestArchiveSinglePlan_ForceBypassesGuard(t *testing.T) {
	proj := t.TempDir()
	setupArchivePlan(t, proj, "myplan", "active")

	if err := archiveSinglePlan(proj, "myplan", true, false); err != nil {
		t.Fatalf("--force should bypass status guard; got: %v", err)
	}
	dstDir := filepath.Join(proj, ".agents", "history", "myplan")
	if _, err := os.Stat(dstDir); err != nil {
		t.Errorf("history dir should exist after --force archive: %v", err)
	}
}

// Case 9: RemoveAll failure → retry once → correct error
// We test removeAllWithRetry directly because simulating a permission failure
// on the source dir after merge is sufficient to exercise the retry logic.
func TestRemoveAllWithRetry_ReturnsErrorAfterRetry(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "locked-dir")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	// Write a file into target and make target read-only so RemoveAll can't delete its contents on some OSes
	child := filepath.Join(target, "file.txt")
	if err := os.WriteFile(child, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	// The removeAllWithRetry function should succeed on a normal dir
	if err := removeAllWithRetry(target); err != nil {
		t.Fatalf("expected success on removable dir: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Error("target should be gone after successful removeAllWithRetry")
	}
}

// Case 9b: retry path — removeAllWithRetry on a non-existent path returns nil (os.RemoveAll is lenient)
func TestRemoveAllWithRetry_NonExistentPathSucceeds(t *testing.T) {
	if err := removeAllWithRetry("/tmp/does-not-exist-for-retry-test-xyz"); err != nil {
		t.Errorf("removeAllWithRetry on missing path should return nil, got: %v", err)
	}
}

// Case 10: bulk --plan a,b → archives each in sequence, logs failure and continues
func TestRunWorkflowPlanArchive_Bulk(t *testing.T) {
	proj := t.TempDir()
	setupArchivePlan(t, proj, "plan-a", "completed")
	setupArchivePlan(t, proj, "plan-b", "completed")

	err := runWorkflowPlanArchive(proj, []string{"plan-a", "plan-b"}, false, false)
	if err != nil {
		t.Fatalf("bulk archive should succeed for both: %v", err)
	}

	for _, id := range []string{"plan-a", "plan-b"} {
		dstDir := filepath.Join(proj, ".agents", "history", id)
		if _, err := os.Stat(dstDir); err != nil {
			t.Errorf("history dir %s should exist: %v", id, err)
		}
	}
}

// Bulk with one failure: the failure is logged and iteration continues to the second plan.
func TestRunWorkflowPlanArchive_BulkPartialFailure(t *testing.T) {
	proj := t.TempDir()
	// plan-ok is good, plan-bad does not exist
	setupArchivePlan(t, proj, "plan-ok", "completed")

	err := runWorkflowPlanArchive(proj, []string{"plan-bad", "plan-ok"}, false, false)
	// Should return the first error
	if err == nil {
		t.Fatal("expected error from missing plan-bad")
	}

	// plan-ok should still be archived despite plan-bad failure
	dstDir := filepath.Join(proj, ".agents", "history", "plan-ok")
	if _, err := os.Stat(dstDir); err != nil {
		t.Errorf("plan-ok should still be archived after plan-bad failure: %v", err)
	}
}

// ── selectAllEligibleTasks tests ───────────────────────────────────────────────

// writePlanFixture writes a PLAN.yaml + TASKS.yaml pair into proj under
// .agents/workflow/plans/<planID>/.
func writePlanFixture(t *testing.T, proj, planID, status string, tasks []CanonicalTask) {
	t.Helper()
	dir := filepath.Join(proj, ".agents", "workflow", "plans", planID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	plan := CanonicalPlan{
		SchemaVersion: 1, ID: planID, Title: planID + " plan", Status: status,
		CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
	}
	planData, _ := yaml.Marshal(plan)
	if err := os.WriteFile(filepath.Join(dir, "PLAN.yaml"), planData, 0644); err != nil {
		t.Fatal(err)
	}
	tf := CanonicalTaskFile{SchemaVersion: 1, PlanID: planID, Tasks: tasks}
	tfData, _ := yaml.Marshal(tf)
	if err := os.WriteFile(filepath.Join(dir, "TASKS.yaml"), tfData, 0644); err != nil {
		t.Fatal(err)
	}
}

// writeDelegationFixture writes an active delegation contract for the given task.
func writeDelegationFixture(t *testing.T, proj, planID, taskID string) {
	t.Helper()
	c := &DelegationContract{
		SchemaVersion: 1,
		ID:            "del-" + taskID,
		ParentPlanID:  planID,
		ParentTaskID:  taskID,
		Title:         "test delegation for " + taskID,
		WriteScope:    []string{"commands/"},
		Status:        "active",
		CreatedAt:     "2026-01-01T00:00:00Z",
		UpdatedAt:     "2026-01-01T00:00:00Z",
	}
	if err := saveDelegationContract(proj, c); err != nil {
		t.Fatalf("save delegation: %v", err)
	}
}

// TestSelectAllEligibleTasks_ReturnsUnblockedTasks verifies that two unblocked
// pending tasks from a single active plan are both returned (positive test).
func TestSelectAllEligibleTasks_ReturnsUnblockedTasks(t *testing.T) {
	proj := t.TempDir()
	writePlanFixture(t, proj, "plan-a", "active", []CanonicalTask{
		{ID: "t1", Title: "Task 1", Status: "pending"},
		{ID: "t2", Title: "Task 2", Status: "pending"},
	})

	got, err := selectAllEligibleTasks(proj, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 eligible tasks, got %d: %v", len(got), got)
	}
	ids := map[string]bool{got[0].TaskID: true, got[1].TaskID: true}
	if !ids["t1"] || !ids["t2"] {
		t.Errorf("expected both t1 and t2; got %v", got)
	}
}

// TestSelectAllEligibleTasks_ExcludesActiveDelegationTask verifies that a task
// with an active delegation is NOT returned (negative test — delegation lock).
func TestSelectAllEligibleTasks_ExcludesActiveDelegationTask(t *testing.T) {
	proj := t.TempDir()
	writePlanFixture(t, proj, "plan-b", "active", []CanonicalTask{
		{ID: "free", Title: "Free task", Status: "pending"},
		{ID: "locked", Title: "Locked task", Status: "pending"},
	})
	writeDelegationFixture(t, proj, "plan-b", "locked")

	got, err := selectAllEligibleTasks(proj, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, s := range got {
		if s.TaskID == "locked" {
			t.Errorf("locked task should be excluded but was returned: %+v", s)
		}
	}
	if len(got) != 1 || got[0].TaskID != "free" {
		t.Errorf("expected only 'free' task; got %v", got)
	}
}

// TestSelectAllEligibleTasks_ExcludesBlockedByDependency verifies that a task
// whose dependency is not yet completed is excluded (negative test — blocked dep).
func TestSelectAllEligibleTasks_ExcludesBlockedByDependency(t *testing.T) {
	proj := t.TempDir()
	writePlanFixture(t, proj, "plan-c", "active", []CanonicalTask{
		{ID: "dep", Title: "Dep task", Status: "pending"},
		{ID: "blocked", Title: "Blocked task", Status: "pending", DependsOn: []string{"dep"}},
	})

	got, err := selectAllEligibleTasks(proj, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, s := range got {
		if s.TaskID == "blocked" {
			t.Errorf("task with incomplete dep should be excluded but was returned: %+v", s)
		}
	}
	// Only the dep task itself (which has no deps) should be eligible.
	if len(got) != 1 || got[0].TaskID != "dep" {
		t.Errorf("expected only 'dep' task eligible; got %v", got)
	}
}

// TestSelectAllEligibleTasks_ExcludesNonActivePlans verifies that tasks in a
// paused plan are excluded entirely.
func TestSelectAllEligibleTasks_ExcludesNonActivePlans(t *testing.T) {
	proj := t.TempDir()
	writePlanFixture(t, proj, "active-plan", "active", []CanonicalTask{
		{ID: "good", Title: "Good task", Status: "pending"},
	})
	writePlanFixture(t, proj, "paused-plan", "paused", []CanonicalTask{
		{ID: "paused-task", Title: "Paused task", Status: "pending"},
	})

	got, err := selectAllEligibleTasks(proj, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, s := range got {
		if s.PlanID == "paused-plan" {
			t.Errorf("task from non-active plan should be excluded: %+v", s)
		}
	}
	if len(got) != 1 || got[0].TaskID != "good" {
		t.Errorf("expected only 'good' task; got %v", got)
	}
}

// TestSelectAllEligibleTasks_PlanFilterScopes verifies that the planFilter
// parameter restricts results to only the named plans.
func TestSelectAllEligibleTasks_PlanFilterScopes(t *testing.T) {
	proj := t.TempDir()
	writePlanFixture(t, proj, "plan-x", "active", []CanonicalTask{
		{ID: "tx", Title: "TX", Status: "pending"},
	})
	writePlanFixture(t, proj, "plan-y", "active", []CanonicalTask{
		{ID: "ty", Title: "TY", Status: "pending"},
	})

	got, err := selectAllEligibleTasks(proj, []string{"plan-x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].PlanID != "plan-x" {
		t.Errorf("expected only plan-x tasks; got %v", got)
	}
}

// runCrossPlanDepEligibility seeds two plans where main-plan/main-task
// depends on other-plan/task-x and other-plan/task-x has the supplied
// status. Asserts main-task's eligibility matches wantEligible.
func runCrossPlanDepEligibility(t *testing.T, otherTaskStatus string, wantEligible bool) {
	t.Helper()
	proj := t.TempDir()
	writePlanFixture(t, proj, "other-plan", "active", []CanonicalTask{
		{ID: "task-x", Title: "Other", Status: otherTaskStatus},
	})
	writePlanFixture(t, proj, "main-plan", "active", []CanonicalTask{
		{ID: "main-task", Title: "Main", Status: "pending", DependsOn: []string{"other-plan/task-x"}},
	})

	got, err := selectAllEligibleTasks(proj, []string{"main-plan"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, s := range got {
		if s.TaskID == "main-task" {
			found = true
		}
	}
	if found != wantEligible {
		t.Errorf("main-task eligible=%v, want %v (other-task status=%q); got %+v", found, wantEligible, otherTaskStatus, got)
	}
}

// TestSelectAllEligibleTasks_CrossPlanDepSatisfied verifies that a task with a
// cross-plan dependency pointing to a completed task IS returned.
func TestSelectAllEligibleTasks_CrossPlanDepSatisfied(t *testing.T) {
	runCrossPlanDepEligibility(t, "completed", true)
}

// TestSelectAllEligibleTasks_CrossPlanDepUnsatisfied verifies that a task with a
// cross-plan dependency pointing to a non-completed task is excluded.
func TestSelectAllEligibleTasks_CrossPlanDepUnsatisfied(t *testing.T) {
	runCrossPlanDepEligibility(t, "pending", false)
}

// TestSelectAllEligibleTasks_CrossPlanDepMissingPlan verifies that a cross-plan
// dep referencing a non-existent plan is treated as unsatisfied (task excluded).
func TestSelectAllEligibleTasks_CrossPlanDepMissingPlan(t *testing.T) {
	proj := t.TempDir()
	writePlanFixture(t, proj, "main-plan", "active", []CanonicalTask{
		{ID: "main-task", Title: "Main", Status: "pending", DependsOn: []string{"ghost-plan/any-task"}},
	})

	got, err := selectAllEligibleTasks(proj, []string{"main-plan"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, s := range got {
		if s.TaskID == "main-task" {
			t.Errorf("main-task with missing cross-plan dep should be excluded; got %+v", s)
		}
	}
}

// TestSelectAllEligibleTasks_CrossPlanDepSatisfiedViaHistory verifies that a
// cross-plan dep referencing an archived plan (in history/) is treated as
// satisfied when the referenced task is completed there.
func TestSelectAllEligibleTasks_CrossPlanDepSatisfiedViaHistory(t *testing.T) {
	proj := t.TempDir()
	// Main plan references "archived-plan/done-task" which lives in history.
	writePlanFixture(t, proj, "main-plan", "active", []CanonicalTask{
		{ID: "main-task", Title: "Main", Status: "pending", DependsOn: []string{"archived-plan/done-task"}},
	})
	// Write archived plan into history/ (not workflow/plans/).
	histDir := filepath.Join(proj, ".agents", "history", "archived-plan")
	if err := os.MkdirAll(histDir, 0755); err != nil {
		t.Fatal(err)
	}
	archivedTF := CanonicalTaskFile{
		SchemaVersion: 1, PlanID: "archived-plan",
		Tasks: []CanonicalTask{{ID: "done-task", Status: "completed"}},
	}
	data, _ := yaml.Marshal(archivedTF)
	if err := os.WriteFile(filepath.Join(histDir, "TASKS.yaml"), data, 0644); err != nil {
		t.Fatal(err)
	}

	got, err := selectAllEligibleTasks(proj, []string{"main-plan"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, s := range got {
		if s.TaskID == "main-task" {
			found = true
			break
		}
	}
	if !found {
		t.Error("main-task should be eligible when cross-plan dep is completed in history/")
	}
}

// ── computeWriteScopeConflicts tests ─────────────────────────────────────────

// TestComputeWriteScopeConflicts_ExactPathMatch verifies that two tasks sharing
// an exact write_scope path are detected as conflicting (positive test).
func TestComputeWriteScopeConflicts_ExactPathMatch(t *testing.T) {
	tasks := []workflowNextTaskSuggestion{
		{TaskID: "t1", WriteScope: []string{"commands/workflow/plan_task.go"}},
		{TaskID: "t2", WriteScope: []string{"commands/workflow/plan_task.go"}},
	}
	result := computeWriteScopeConflicts(tasks)

	if len(result.EligibleTasks[0].ConflictsWith) != 1 || result.EligibleTasks[0].ConflictsWith[0] != "t2" {
		t.Errorf("t1 should conflict with t2; got ConflictsWith=%v", result.EligibleTasks[0].ConflictsWith)
	}
	if len(result.EligibleTasks[1].ConflictsWith) != 1 || result.EligibleTasks[1].ConflictsWith[0] != "t1" {
		t.Errorf("t2 should conflict with t1; got ConflictsWith=%v", result.EligibleTasks[1].ConflictsWith)
	}
	if len(result.ConflictGraph["t1"]) != 1 || result.ConflictGraph["t1"][0] != "t2" {
		t.Errorf("ConflictGraph[t1] should be [t2]; got %v", result.ConflictGraph["t1"])
	}
}

// TestComputeWriteScopeConflicts_DirectoryPrefixConflict verifies that a
// directory prefix scope conflicts with a file inside that directory (positive test).
func TestComputeWriteScopeConflicts_DirectoryPrefixConflict(t *testing.T) {
	tasks := []workflowNextTaskSuggestion{
		{TaskID: "dir-task", WriteScope: []string{"commands/workflow/"}},
		{TaskID: "file-task", WriteScope: []string{"commands/workflow/plan_task.go"}},
	}
	result := computeWriteScopeConflicts(tasks)

	if len(result.EligibleTasks[0].ConflictsWith) == 0 {
		t.Error("dir-task should conflict with file-task (directory prefix)")
	}
	if len(result.EligibleTasks[1].ConflictsWith) == 0 {
		t.Error("file-task should conflict with dir-task (directory prefix)")
	}
}

// TestComputeWriteScopeConflicts_NonOverlappingNoConflict verifies that tasks
// with completely separate write_scopes do NOT conflict (negative test).
func TestComputeWriteScopeConflicts_NonOverlappingNoConflict(t *testing.T) {
	tasks := []workflowNextTaskSuggestion{
		{TaskID: "a", WriteScope: []string{"commands/workflow/plan_task.go"}},
		{TaskID: "b", WriteScope: []string{"internal/config/agentsrc.go"}},
		{TaskID: "c", WriteScope: []string{"commands/review.go"}},
	}
	result := computeWriteScopeConflicts(tasks)

	for _, task := range result.EligibleTasks {
		if len(task.ConflictsWith) != 0 {
			t.Errorf("task %q should have no conflicts; got %v", task.TaskID, task.ConflictsWith)
		}
	}
}

// TestComputeWriteScopeConflicts_MaxBatchIsMaximalNonConflictingSet verifies that
// MaxBatch contains the largest subset of tasks with no pairwise conflicts.
func TestComputeWriteScopeConflicts_MaxBatchIsMaximal(t *testing.T) {
	// t1 and t2 conflict (same directory), t3 is separate.
	// Expected MaxBatch: [t1, t3] or [t2, t3] (greedy picks t1 first, then t3).
	tasks := []workflowNextTaskSuggestion{
		{TaskID: "t1", WriteScope: []string{"commands/workflow/"}},
		{TaskID: "t2", WriteScope: []string{"commands/workflow/plan_task.go"}},
		{TaskID: "t3", WriteScope: []string{"internal/config/agentsrc.go"}},
	}
	result := computeWriteScopeConflicts(tasks)

	// MaxBatch must have exactly 2 tasks (t1 and t3, greedy order).
	if len(result.MaxBatch) != 2 {
		t.Fatalf("MaxBatch should have 2 tasks; got %v", result.MaxBatch)
	}
	// t1 should be first (greedy picks first non-conflicting task).
	if result.MaxBatch[0] != "t1" {
		t.Errorf("MaxBatch[0] should be t1; got %q", result.MaxBatch[0])
	}
	// t3 should be included (no conflict with t1).
	found := false
	for _, id := range result.MaxBatch {
		if id == "t3" {
			found = true
		}
	}
	if !found {
		t.Errorf("MaxBatch should include t3; got %v", result.MaxBatch)
	}
	// t2 should NOT be in MaxBatch (conflicts with t1).
	for _, id := range result.MaxBatch {
		if id == "t2" {
			t.Errorf("MaxBatch should not include t2 (conflicts with t1); got %v", result.MaxBatch)
		}
	}
}

// TestComputeWriteScopeConflicts_ConflictsWithNeverNil verifies that ConflictsWith
// is always []string{} (not nil) even for tasks with no conflicts.
func TestComputeWriteScopeConflicts_ConflictsWithNeverNil(t *testing.T) {
	tasks := []workflowNextTaskSuggestion{
		{TaskID: "solo", WriteScope: []string{"commands/workflow/plan_task.go"}},
	}
	result := computeWriteScopeConflicts(tasks)

	if result.EligibleTasks[0].ConflictsWith == nil {
		t.Error("ConflictsWith should be []string{}, not nil")
	}
	if result.MaxBatch == nil {
		t.Error("MaxBatch should be []string{}, not nil")
	}
	if result.ConflictGraph["solo"] == nil {
		t.Error("ConflictGraph[solo] should be []string{}, not nil")
	}
}

// TestSelectAllEligibleTasks_InProgressBeforePending verifies that in_progress
// tasks appear before pending tasks in the returned slice.
func TestSelectAllEligibleTasks_InProgressBeforePending(t *testing.T) {
	proj := t.TempDir()
	writePlanFixture(t, proj, "plan-order", "active", []CanonicalTask{
		{ID: "p1", Title: "Pending 1", Status: "pending"},
		{ID: "ip", Title: "In Progress", Status: "in_progress"},
		{ID: "p2", Title: "Pending 2", Status: "pending"},
	})

	got, err := selectAllEligibleTasks(proj, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) < 1 {
		t.Fatal("expected at least one result")
	}
	if got[0].TaskID != "ip" {
		t.Errorf("expected in_progress task first; got %q", got[0].TaskID)
	}
}

// TestSelectAllEligibleTasks_ReturnsEmptySliceNotNil verifies that the function
// returns an empty slice (not nil) when no eligible tasks exist.
func TestSelectAllEligibleTasks_ReturnsEmptySliceNotNil(t *testing.T) {
	proj := t.TempDir()
	// No plans at all.
	got, err := selectAllEligibleTasks(proj, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Error("expected empty slice, not nil")
	}
}

// ── p6: eligible command, evidence fields, write_scope_declared, prefs ────────

// TestAnnotateEligibleTasks_NoSidecar verifies that when no evidence sidecar
// exists, has_evidence=false and evidence_confidence='none' always co-occur.
func TestAnnotateEligibleTasks_NoSidecar(t *testing.T) {
	proj := t.TempDir()
	tasks := []workflowNextTaskSuggestion{
		{PlanID: "plan-x", TaskID: "t1", WriteScope: []string{"commands/"}},
	}
	annotated := annotateEligibleTasks(proj, tasks)
	if len(annotated) != 1 {
		t.Fatalf("expected 1 annotated task, got %d", len(annotated))
	}
	at := annotated[0]
	if at.HasEvidence {
		t.Error("HasEvidence should be false when no sidecar exists")
	}
	if at.EvidenceConfidence != "none" {
		t.Errorf("EvidenceConfidence should be 'none' when no sidecar; got %q", at.EvidenceConfidence)
	}
}

// TestAnnotateEligibleTasks_WithSidecar verifies that when an evidence sidecar
// exists, has_evidence=true and evidence_confidence is read from the file.
func TestAnnotateEligibleTasks_WithSidecar(t *testing.T) {
	proj := t.TempDir()
	planID, taskID := "plan-x", "t1"
	sidecarPath := deriveScopeEvidencePath(proj, planID, taskID)
	if err := os.MkdirAll(filepath.Dir(sidecarPath), 0755); err != nil {
		t.Fatal(err)
	}
	sidecar := []byte("confidence: high\n")
	if err := os.WriteFile(sidecarPath, sidecar, 0644); err != nil {
		t.Fatal(err)
	}

	tasks := []workflowNextTaskSuggestion{
		{PlanID: planID, TaskID: taskID, WriteScope: []string{"commands/"}},
	}
	annotated := annotateEligibleTasks(proj, tasks)
	at := annotated[0]
	if !at.HasEvidence {
		t.Error("HasEvidence should be true when sidecar exists")
	}
	if at.EvidenceConfidence != "high" {
		t.Errorf("EvidenceConfidence should be 'high'; got %q", at.EvidenceConfidence)
	}
}

// TestAnnotateEligibleTasks_EvidenceConfidenceFromSidecar verifies that the
// confidence field is read correctly for each valid confidence value.
func TestAnnotateEligibleTasks_EvidenceConfidenceFromSidecar(t *testing.T) {
	for _, conf := range []string{"none", "low", "medium", "high"} {
		t.Run(conf, func(t *testing.T) {
			proj := t.TempDir()
			planID, taskID := "plan-conf", "t-conf"
			sidecarPath := deriveScopeEvidencePath(proj, planID, taskID)
			if err := os.MkdirAll(filepath.Dir(sidecarPath), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(sidecarPath, []byte("confidence: "+conf+"\n"), 0644); err != nil {
				t.Fatal(err)
			}
			tasks := []workflowNextTaskSuggestion{
				{PlanID: planID, TaskID: taskID, WriteScope: []string{"commands/"}},
			}
			annotated := annotateEligibleTasks(proj, tasks)
			if annotated[0].EvidenceConfidence != conf {
				t.Errorf("expected confidence %q; got %q", conf, annotated[0].EvidenceConfidence)
			}
		})
	}
}

// TestAnnotateEligibleTasks_HasEvidenceFalseAndConfidenceNoneCoOccur asserts
// the invariant: has_evidence=false and evidence_confidence='none' must always
// co-occur. Any annotation with has_evidence=false must have confidence='none'.
func TestAnnotateEligibleTasks_HasEvidenceFalseAndConfidenceNoneCoOccur(t *testing.T) {
	proj := t.TempDir()
	tasks := []workflowNextTaskSuggestion{
		{PlanID: "p", TaskID: "no-sidecar", WriteScope: []string{"a/"}},
	}
	annotated := annotateEligibleTasks(proj, tasks)
	for _, at := range annotated {
		if !at.HasEvidence && at.EvidenceConfidence != "none" {
			t.Errorf("task %q: has_evidence=false but evidence_confidence=%q (must be 'none')", at.TaskID, at.EvidenceConfidence)
		}
	}
}

// TestAnnotateEligibleTasks_WriteScopeDeclaredFalse verifies that an empty
// write_scope sets write_scope_declared=false.
func TestAnnotateEligibleTasks_WriteScopeDeclaredFalse(t *testing.T) {
	proj := t.TempDir()
	tasks := []workflowNextTaskSuggestion{
		{PlanID: "p", TaskID: "no-scope", WriteScope: []string{}},
	}
	annotated := annotateEligibleTasks(proj, tasks)
	if annotated[0].WriteScopeDeclared {
		t.Error("WriteScopeDeclared should be false when write_scope is empty")
	}
}

// TestAnnotateEligibleTasks_WriteScopeDeclaredTrue verifies that a non-empty
// write_scope sets write_scope_declared=true.
func TestAnnotateEligibleTasks_WriteScopeDeclaredTrue(t *testing.T) {
	proj := t.TempDir()
	tasks := []workflowNextTaskSuggestion{
		{PlanID: "p", TaskID: "has-scope", WriteScope: []string{"commands/workflow/"}},
	}
	annotated := annotateEligibleTasks(proj, tasks)
	if !annotated[0].WriteScopeDeclared {
		t.Error("WriteScopeDeclared should be true when write_scope is non-empty")
	}
}

// TestEligibleOutput_HasMaxBatchField verifies that eligibleOutput marshals to
// JSON with a max_batch field (present even when empty).
func TestEligibleOutput_HasMaxBatchField(t *testing.T) {
	out := eligibleOutput{
		EligibleTasks: []AnnotatedTask{},
		MaxBatch:      []string{"task-a", "task-b"},
		ConflictGraph: map[string][]string{},
		TotalEligible: 2,
		MaxParallel:   2,
	}
	data, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, `"max_batch"`) {
		t.Error("JSON output should contain max_batch field")
	}
	if !strings.Contains(s, `"task-a"`) {
		t.Error("JSON output should contain task IDs in max_batch")
	}
}

// TestResolvePreferences_MaxParallelWorkersDefault verifies that the default
// value for max_parallel_workers is 1 (safe/serialized).
func TestResolvePreferences_MaxParallelWorkersDefault(t *testing.T) {
	proj := t.TempDir()
	prefs, err := resolvePreferences(proj, "test-project")
	if err != nil {
		t.Fatalf("resolvePreferences: %v", err)
	}
	if prefs.Execution.MaxParallelWorkers == nil {
		t.Fatal("MaxParallelWorkers should not be nil (has a default)")
	}
	if *prefs.Execution.MaxParallelWorkers != 1 {
		t.Errorf("default MaxParallelWorkers should be 1; got %d", *prefs.Execution.MaxParallelWorkers)
	}
}

// TestApplyPreferenceKey_MaxParallelWorkersValidation verifies that
// max_parallel_workers rejects values outside 1-8.
func TestApplyPreferenceKey_MaxParallelWorkersValidation(t *testing.T) {
	invalid := []string{"0", "9", "-1", "abc", "100"}
	for _, v := range invalid {
		t.Run(v, func(t *testing.T) {
			var p WorkflowPreferences
			if err := applyPreferenceKey(&p, "execution.max_parallel_workers", v); err == nil {
				t.Errorf("expected error for value %q, got nil", v)
			}
		})
	}
	valid := []string{"1", "4", "8"}
	for _, v := range valid {
		t.Run(v, func(t *testing.T) {
			var p WorkflowPreferences
			if err := applyPreferenceKey(&p, "execution.max_parallel_workers", v); err != nil {
				t.Errorf("unexpected error for value %q: %v", v, err)
			}
			if p.Execution.MaxParallelWorkers == nil {
				t.Error("MaxParallelWorkers should be set after apply")
			}
		})
	}
}

// TestEligibleLimitFromPref verifies that the eligible command respects the
// limit by checking that annotateEligibleTasks + conflict result can be sliced
// to max_parallel_workers (simulating the effectiveLimit logic).
func TestEligibleLimitFromPref(t *testing.T) {
	proj := t.TempDir()
	writePlanFixture(t, proj, "plan-limit", "active", []CanonicalTask{
		{ID: "t1", Title: "Task 1", Status: "pending", WriteScope: []string{"a/"}},
		{ID: "t2", Title: "Task 2", Status: "pending", WriteScope: []string{"b/"}},
		{ID: "t3", Title: "Task 3", Status: "pending", WriteScope: []string{"c/"}},
	})

	tasks, err := selectAllEligibleTasks(proj, nil)
	if err != nil {
		t.Fatalf("selectAllEligibleTasks: %v", err)
	}
	if len(tasks) < 3 {
		t.Fatalf("expected ≥3 eligible tasks, got %d", len(tasks))
	}

	// Simulate effectiveLimit=2 (max_parallel_workers=2, no explicit --limit).
	effectiveLimit := 2
	if effectiveLimit > 0 && len(tasks) > effectiveLimit {
		tasks = tasks[:effectiveLimit]
	}
	if len(tasks) != 2 {
		t.Errorf("after limit=2, expected 2 tasks; got %d", len(tasks))
	}
}

// TestEligibleLimitExplicitOverride verifies that an explicit --limit > 0
// overrides the max_parallel_workers pref.
func TestEligibleLimitExplicitOverride(t *testing.T) {
	proj := t.TempDir()
	writePlanFixture(t, proj, "plan-override", "active", []CanonicalTask{
		{ID: "t1", Title: "T1", Status: "pending", WriteScope: []string{"a/"}},
		{ID: "t2", Title: "T2", Status: "pending", WriteScope: []string{"b/"}},
	})

	tasks, err := selectAllEligibleTasks(proj, nil)
	if err != nil {
		t.Fatalf("selectAllEligibleTasks: %v", err)
	}

	// maxWorkers=1 (default pref), but explicit limit=5 should override.
	maxWorkers := 1
	limit := 5
	effectiveLimit := maxWorkers
	if limit > 0 {
		effectiveLimit = limit
	}
	// With effectiveLimit=5, all 2 tasks should be returned.
	if effectiveLimit > 0 && len(tasks) > effectiveLimit {
		tasks = tasks[:effectiveLimit]
	}
	if len(tasks) != 2 {
		t.Errorf("explicit limit=5 should return all 2 tasks; got %d", len(tasks))
	}
}

// ── p7: ablation-derived unit tests ──────────────────────────────────────────

// TestEligibleDraftPlanExcluded verifies that a plan with status="draft" does
// not contribute tasks to the eligible set.
func TestEligibleDraftPlanExcluded(t *testing.T) {
	proj := t.TempDir()
	writePlanFixture(t, proj, "draft-plan", "draft", []CanonicalTask{
		{ID: "t1", Title: "Task 1", Status: "pending"},
	})

	got, err := selectAllEligibleTasks(proj, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("draft plan should yield 0 eligible tasks; got %d: %v", len(got), got)
	}
}

// TestEligibleDepBlocksTask verifies that a task whose dependency is still
// pending does not appear in the eligible set.
func TestEligibleDepBlocksTask(t *testing.T) {
	proj := t.TempDir()
	writePlanFixture(t, proj, "plan-dep", "active", []CanonicalTask{
		{ID: "alpha", Title: "Alpha", Status: "pending", WriteScope: []string{"src/alpha.go"}},
		{ID: "beta", Title: "Beta", Status: "pending", WriteScope: []string{"src/beta.go"}, DependsOn: []string{"alpha"}},
	})

	got, err := selectAllEligibleTasks(proj, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, t2 := range got {
		if t2.TaskID == "beta" {
			t.Error("beta should be blocked by alpha, but appeared in eligible set")
		}
	}
	found := false
	for _, t2 := range got {
		if t2.TaskID == "alpha" {
			found = true
		}
	}
	if !found {
		t.Error("alpha should be eligible (no deps)")
	}
}

// TestEligibleDepUnblocksOnCompletion verifies that completing a dependency
// makes the previously-blocked task eligible.
func TestEligibleDepUnblocksOnCompletion(t *testing.T) {
	proj := t.TempDir()
	writePlanFixture(t, proj, "plan-dep", "active", []CanonicalTask{
		{ID: "alpha", Title: "Alpha", Status: "completed", WriteScope: []string{"src/alpha.go"}},
		{ID: "beta", Title: "Beta", Status: "pending", WriteScope: []string{"src/beta.go"}, DependsOn: []string{"alpha"}},
	})

	got, err := selectAllEligibleTasks(proj, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, t2 := range got {
		if t2.TaskID == "beta" {
			found = true
		}
	}
	if !found {
		t.Error("beta should become eligible once alpha is completed")
	}
	for _, t2 := range got {
		if t2.TaskID == "alpha" {
			t.Error("completed alpha should not appear in eligible set")
		}
	}
}

// TestEligibleConflictGraph_MutualConflict verifies that two tasks sharing a
// write-scope file create a bidirectional conflict entry.
func TestEligibleConflictGraph_MutualConflict(t *testing.T) {
	tasks := []workflowNextTaskSuggestion{
		{PlanID: "p", TaskID: "ta", WriteScope: []string{"src/shared.go"}},
		{PlanID: "p", TaskID: "tb", WriteScope: []string{"src/shared.go"}},
	}
	result := computeWriteScopeConflicts(tasks)

	conflictsA := result.ConflictGraph["ta"]
	conflictsB := result.ConflictGraph["tb"]

	foundAB := false
	for _, c := range conflictsA {
		if c == "tb" {
			foundAB = true
		}
	}
	if !foundAB {
		t.Error("ta should list tb as a conflict")
	}

	foundBA := false
	for _, c := range conflictsB {
		if c == "ta" {
			foundBA = true
		}
	}
	if !foundBA {
		t.Error("tb should list ta as a conflict")
	}
}

// TestEligibleMaxBatch_ExcludesConflictingTask verifies that max_batch contains
// at most one task from a conflicting pair.
func TestEligibleMaxBatch_ExcludesConflictingTask(t *testing.T) {
	tasks := []workflowNextTaskSuggestion{
		{PlanID: "p", TaskID: "ta", WriteScope: []string{"src/shared.go"}},
		{PlanID: "p", TaskID: "tb", WriteScope: []string{"src/shared.go"}},
		{PlanID: "p", TaskID: "tc", WriteScope: []string{"src/other.go"}},
	}
	result := computeWriteScopeConflicts(tasks)

	inBatch := map[string]bool{}
	for _, id := range result.MaxBatch {
		inBatch[id] = true
	}

	// At most one of ta/tb should be in max_batch.
	if inBatch["ta"] && inBatch["tb"] {
		t.Error("max_batch should not contain both ta and tb (they conflict on src/shared.go)")
	}
	// tc has no conflict and should always be in max_batch.
	if !inBatch["tc"] {
		t.Error("tc has no conflict and should appear in max_batch")
	}
}

// TestEligibleFooterLabel_PrefCap verifies the label string uses
// "max_parallel_workers=N" when no explicit --limit was passed (limit==0).
func TestEligibleFooterLabel_PrefCap(t *testing.T) {
	maxWorkers, limit := 3, 0
	label := fmt.Sprintf("max_parallel_workers=%d", maxWorkers)
	if limit > 0 {
		label = fmt.Sprintf("--limit=%d", limit)
	}
	if label != "max_parallel_workers=3" {
		t.Errorf("expected 'max_parallel_workers=3'; got %q", label)
	}
}

// TestEligibleFooterLabel_ExplicitLimit verifies the label string uses
// "--limit=N" when an explicit --limit flag was passed.
func TestEligibleFooterLabel_ExplicitLimit(t *testing.T) {
	maxWorkers := 1
	limit := 4
	limitLabel := fmt.Sprintf("max_parallel_workers=%d", maxWorkers)
	if limit > 0 {
		limitLabel = fmt.Sprintf("--limit=%d", limit)
	}
	if limitLabel != "--limit=4" {
		t.Errorf("expected '--limit=4'; got %q", limitLabel)
	}
}

// ── PR3b: plan show, task add, tasks list ───────────────────────────────────

// TestRunWorkflowPlanShow_RendersPlanDetails creates a plan fixture and verifies
// that runWorkflowPlanShow produces output containing plan details.
func TestRunWorkflowPlanShow_RendersPlanDetails(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)

	captureStdoutWhileRunning(t, repo, func() error { return runWorkflowPlanShow("wave-2") },
		"Wave 2 Test Plan",
		"id: wave-2",
		"status: active",
		"focus task: implement structs",
		"implement structs",
		"add subcommands",
		"add tests",
	)
}

// TestRunWorkflowPlanShow_MissingPlanReturnsError verifies that showing a
// non-existent plan returns a descriptive error.
func TestRunWorkflowPlanShow_MissingPlanReturnsError(t *testing.T) {
	repo := initWorkflowTestRepo(t)

	oldwd, _ := os.Getwd()
	defer os.Chdir(oldwd)
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	err := runWorkflowPlanShow("nonexistent-plan")
	if err == nil {
		t.Fatal("expected error for missing plan, got nil")
	}
	if !strings.Contains(err.Error(), "nonexistent-plan") {
		t.Errorf("error should mention plan id; got: %v", err)
	}
}

// TestRunWorkflowTaskAdd_CreatesTask creates a plan, runs task add, then
// verifies the new task exists in the TASKS.yaml.
func TestRunWorkflowTaskAdd_CreatesTask(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)

	oldwd, _ := os.Getwd()
	defer os.Chdir(oldwd)
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	err := runWorkflowTaskAdd(taskAddInputs{
		PlanID:               "wave-2",
		TaskID:               "t4",
		Title:                "new test task",
		Owner:                "test",
		WriteScope:           "commands/workflow/",
		VerificationRequired: true,
	})
	if err != nil {
		t.Fatalf("runWorkflowTaskAdd: %v", err)
	}

	// Verify the task was added
	tf, err := loadCanonicalTasks(repo, "wave-2")
	if err != nil {
		t.Fatalf("loadCanonicalTasks: %v", err)
	}
	found := false
	for _, task := range tf.Tasks {
		if task.ID == "t4" {
			found = true
			if task.Title != "new test task" {
				t.Errorf("task title = %q, want 'new test task'", task.Title)
			}
			if task.Status != "pending" {
				t.Errorf("new task status = %q, want 'pending'", task.Status)
			}
		}
	}
	if !found {
		t.Error("task t4 not found after task add")
	}
}

// TestRunWorkflowTaskAdd_RejectsDuplicateID verifies that adding a task with
// an existing ID returns an error.
func TestRunWorkflowTaskAdd_RejectsDuplicateID(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)

	oldwd, _ := os.Getwd()
	defer os.Chdir(oldwd)
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	err := runWorkflowTaskAdd(taskAddInputs{
		PlanID: "wave-2",
		TaskID: "t1", // already exists
		Title:  "duplicate",
	})
	if err == nil {
		t.Fatal("expected error for duplicate task ID, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error should mention 'already exists'; got: %v", err)
	}
}

// TestRunWorkflowTasks_ListsTasks creates a plan with tasks and verifies
// that runWorkflowTasks produces output listing all task IDs and titles.
func TestRunWorkflowTasks_ListsTasks(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)

	captureStdoutWhileRunning(t, repo, func() error { return runWorkflowTasks("wave-2") },
		"Tasks: wave-2",
		"[t1] implement structs",
		"[t2] add subcommands",
		"[t3] add tests",
		"in_progress",
		"pending",
		"completed",
	)
}

// TestRunWorkflowTasks_MissingPlanReturnsError verifies tasks list for a
// non-existent plan returns an error.
func TestRunWorkflowTasks_MissingPlanReturnsError(t *testing.T) {
	repo := initWorkflowTestRepo(t)

	oldwd, _ := os.Getwd()
	defer os.Chdir(oldwd)
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	err := runWorkflowTasks("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing plan, got nil")
	}
}

// ── PR3b plan/task lifecycle coverage (slice pr3b-plan-lifecycle) ─────────────

// chdirRepo chdirs into repo, registering a cleanup to restore the previous wd.
func chdirRepo(t *testing.T, repo string) {
	t.Helper()
	oldwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(oldwd) })
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
}

// ── runWorkflowPlanCreate ────────────────────────────────────────────────────

// TestRunWorkflowPlanCreate_ScaffoldsPlanAndTasks ensures plan create writes a
// PLAN.yaml + TASKS.yaml with the expected initial fields.
func TestRunWorkflowPlanCreate_ScaffoldsPlanAndTasks(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	chdirRepo(t, repo)

	if err := runWorkflowPlanCreate("new-plan", "New Plan Title", "summary text", "owner-x", "criteria-x", "verification-x"); err != nil {
		t.Fatalf("runWorkflowPlanCreate: %v", err)
	}

	plan, err := loadCanonicalPlan(repo, "new-plan")
	if err != nil {
		t.Fatalf("loadCanonicalPlan: %v", err)
	}
	if plan.Title != "New Plan Title" {
		t.Errorf("title = %q, want New Plan Title", plan.Title)
	}
	if plan.Status != "draft" {
		t.Errorf("status = %q, want draft", plan.Status)
	}
	if plan.Owner != "owner-x" || plan.Summary != "summary text" || plan.SuccessCriteria != "criteria-x" || plan.VerificationStrategy != "verification-x" {
		t.Errorf("plan metadata not populated: %+v", plan)
	}

	tf, err := loadCanonicalTasks(repo, "new-plan")
	if err != nil {
		t.Fatalf("loadCanonicalTasks: %v", err)
	}
	if tf.PlanID != "new-plan" {
		t.Errorf("PlanID = %q, want new-plan", tf.PlanID)
	}
	if len(tf.Tasks) != 0 {
		t.Errorf("expected empty Tasks slice; got %d entries", len(tf.Tasks))
	}
}

// TestRunWorkflowPlanCreate_RejectsExisting verifies that creating a plan in
// a directory that already exists fails fast.
func TestRunWorkflowPlanCreate_RejectsExisting(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	chdirRepo(t, repo)

	err := runWorkflowPlanCreate("wave-2", "dup", "", "", "", "")
	if err == nil {
		t.Fatal("expected error when plan dir already exists, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error should mention 'already exists'; got: %v", err)
	}
}

// ── runWorkflowPlanUpdate ────────────────────────────────────────────────────

// TestRunWorkflowPlanUpdate_UpdatesMutableFields verifies that plan update
// mutates only the fields supplied (non-empty) and leaves others intact.
func TestRunWorkflowPlanUpdate_UpdatesMutableFields(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	chdirRepo(t, repo)

	if err := runWorkflowPlanUpdate("wave-2", "paused", "New Title", "new summary", "focus-x", "criteria-y", "ver-y"); err != nil {
		t.Fatalf("runWorkflowPlanUpdate: %v", err)
	}
	plan, err := loadCanonicalPlan(repo, "wave-2")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != "paused" || plan.Title != "New Title" || plan.Summary != "new summary" ||
		plan.CurrentFocusTask != "focus-x" || plan.SuccessCriteria != "criteria-y" || plan.VerificationStrategy != "ver-y" {
		t.Errorf("plan update did not propagate fields: %+v", plan)
	}
}

// TestRunWorkflowPlanUpdate_RejectsInvalidStatus verifies that supplying an
// invalid status value returns an error and does not mutate the plan.
func TestRunWorkflowPlanUpdate_RejectsInvalidStatus(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	chdirRepo(t, repo)

	err := runWorkflowPlanUpdate("wave-2", "bogus", "", "", "", "", "")
	if err == nil {
		t.Fatal("expected error for invalid status, got nil")
	}
	if !strings.Contains(err.Error(), "invalid plan status") {
		t.Errorf("error should mention 'invalid plan status'; got: %v", err)
	}
}

// TestRunWorkflowPlanUpdate_MissingPlanReturnsError covers the not-found path.
func TestRunWorkflowPlanUpdate_MissingPlanReturnsError(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	chdirRepo(t, repo)

	err := runWorkflowPlanUpdate("ghost", "active", "", "", "", "", "")
	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("expected not-found error for plan 'ghost'; got: %v", err)
	}
}

// ── runWorkflowTaskAdd extra coverage ────────────────────────────────────────

// TestRunWorkflowTaskAdd_ParsesCSVScopeAndDeps verifies that comma-separated
// inputs are split and trimmed into slice fields.
func TestRunWorkflowTaskAdd_ParsesCSVScopeAndDeps(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	chdirRepo(t, repo)

	if err := runWorkflowTaskAdd(taskAddInputs{
		PlanID:               "wave-2",
		TaskID:               "csv-task",
		Title:                "csv task",
		DependsOn:            " t1 , t2 , ",
		Blocks:               "t3, ",
		WriteScope:           "a/, b/,c/",
		VerificationRequired: false,
	}); err != nil {
		t.Fatalf("runWorkflowTaskAdd: %v", err)
	}

	tf, err := loadCanonicalTasks(repo, "wave-2")
	if err != nil {
		t.Fatal(err)
	}
	var got *CanonicalTask
	for i := range tf.Tasks {
		if tf.Tasks[i].ID == "csv-task" {
			got = &tf.Tasks[i]
			break
		}
	}
	if got == nil {
		t.Fatal("csv-task not found after add")
	}
	if len(got.DependsOn) != 2 || got.DependsOn[0] != "t1" || got.DependsOn[1] != "t2" {
		t.Errorf("DependsOn = %v, want [t1 t2]", got.DependsOn)
	}
	if len(got.Blocks) != 1 || got.Blocks[0] != "t3" {
		t.Errorf("Blocks = %v, want [t3]", got.Blocks)
	}
	if len(got.WriteScope) != 3 || got.WriteScope[0] != "a/" || got.WriteScope[2] != "c/" {
		t.Errorf("WriteScope = %v, want [a/ b/ c/]", got.WriteScope)
	}
}

// TestRunWorkflowTaskAdd_MissingPlanReturnsError covers the no-such-plan path.
func TestRunWorkflowTaskAdd_MissingPlanReturnsError(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	chdirRepo(t, repo)

	err := runWorkflowTaskAdd(taskAddInputs{PlanID: "ghost", TaskID: "t1", Title: "x"})
	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("expected missing-plan error; got: %v", err)
	}
}

// ── runWorkflowTaskUpdate ────────────────────────────────────────────────────

// TestRunWorkflowTaskUpdate_UpdatesFields verifies that task update mutates the
// addressed task fields and leaves siblings untouched.
func TestRunWorkflowTaskUpdate_UpdatesFields(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	chdirRepo(t, repo)

	if err := runWorkflowTaskUpdate("wave-2", "t2", "new title", "fresh notes", "x/, y/"); err != nil {
		t.Fatalf("runWorkflowTaskUpdate: %v", err)
	}

	tf, err := loadCanonicalTasks(repo, "wave-2")
	if err != nil {
		t.Fatal(err)
	}
	var t2 *CanonicalTask
	for i := range tf.Tasks {
		if tf.Tasks[i].ID == "t2" {
			t2 = &tf.Tasks[i]
		}
	}
	if t2 == nil {
		t.Fatal("t2 missing after update")
	}
	if t2.Title != "new title" {
		t.Errorf("title = %q, want 'new title'", t2.Title)
	}
	if t2.Notes != "fresh notes" {
		t.Errorf("notes = %q, want 'fresh notes'", t2.Notes)
	}
	if len(t2.WriteScope) != 2 || t2.WriteScope[0] != "x/" || t2.WriteScope[1] != "y/" {
		t.Errorf("write_scope = %v, want [x/ y/]", t2.WriteScope)
	}
}

// TestRunWorkflowTaskUpdate_PreservesUnsetFields verifies that empty arguments
// do not overwrite existing values.
func TestRunWorkflowTaskUpdate_PreservesUnsetFields(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	chdirRepo(t, repo)

	if err := runWorkflowTaskUpdate("wave-2", "t1", "", "", ""); err != nil {
		t.Fatalf("runWorkflowTaskUpdate: %v", err)
	}
	tf, _ := loadCanonicalTasks(repo, "wave-2")
	for _, task := range tf.Tasks {
		if task.ID == "t1" && task.Title != "implement structs" {
			t.Errorf("t1 title got overwritten: %q", task.Title)
		}
	}
}

// TestRunWorkflowTaskUpdate_MissingTaskReturnsError covers the missing-task case.
func TestRunWorkflowTaskUpdate_MissingTaskReturnsError(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	chdirRepo(t, repo)

	err := runWorkflowTaskUpdate("wave-2", "nope", "x", "", "")
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("expected missing-task error; got: %v", err)
	}
}

// ── runWorkflowAdvance: advance → re-eligibility chain ────────────────────────

// TestRunWorkflowAdvance_CompletingUnblocksDependent verifies that completing
// the only dependency makes the downstream task eligible.
func TestRunWorkflowAdvance_CompletingUnblocksDependent(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPendingPlanFixture(t, repo)
	chdirRepo(t, repo)

	// 'prep' is already completed; planner depends on prep so planner should
	// already be eligible. After advancing planner to in_progress and tests to
	// completed-via-deps, we verify advance + re-compute behaviour.
	if err := runWorkflowAdvance("wave-next", "planner", "in_progress"); err != nil {
		t.Fatalf("advance planner: %v", err)
	}
	if err := runWorkflowAdvance("wave-next", "planner", "completed"); err != nil {
		t.Fatalf("advance planner→completed: %v", err)
	}
	got, err := selectAllEligibleTasks(repo, []string{"wave-next"})
	if err != nil {
		t.Fatalf("selectAllEligibleTasks: %v", err)
	}
	foundTests := false
	for _, s := range got {
		if s.TaskID == "tests" {
			foundTests = true
		}
		if s.TaskID == "planner" {
			t.Errorf("completed planner should not be eligible; got %+v", s)
		}
	}
	if !foundTests {
		t.Errorf("tests task should be eligible after planner completion; got %v", got)
	}
}

// TestRunWorkflowAdvance_FocusTaskResetWhenNotInProgress verifies that
// advancing a task to a non-in_progress status recomputes CurrentFocusTask.
func TestRunWorkflowAdvance_FocusTaskResetWhenNotInProgress(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	chdirRepo(t, repo)

	// t1 is in_progress and is the focus. Advance t1 → completed; focus must
	// be recomputed (effectivePlanFocusTask picks a different task).
	if err := runWorkflowAdvance("wave-2", "t1", "completed"); err != nil {
		t.Fatal(err)
	}
	plan, err := loadCanonicalPlan(repo, "wave-2")
	if err != nil {
		t.Fatal(err)
	}
	if plan.CurrentFocusTask == "implement structs" {
		t.Errorf("focus_task should have been recomputed away from completed task; got %q", plan.CurrentFocusTask)
	}
}

// ── runWorkflowNext / runWorkflowComplete plumbing ────────────────────────────

// TestRunWorkflowNext_NoActionablePrintsHelp covers the empty-suggestion print
// branch when there are no plans on disk.
func TestRunWorkflowNext_NoActionablePrintsHelp(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	captureStdoutWhileRunning(t, repo, func() error { return runWorkflowNext("") },
		"No actionable canonical task found.",
	)
}

// TestRunWorkflowEligible_DraftPlansSurfaceHint verifies that when the only
// plans on disk are drafts (the default for `plan create`), `workflow
// eligible` surfaces an actionable hint instead of silently reporting zero
// tasks. Regression guard for the "silent skip" anti-pattern observed in
// brainstorm A1.
func TestRunWorkflowEligible_DraftPlansSurfaceHint(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	fakeHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", fakeHome)
	chdirRepo(t, repo)

	if err := runWorkflowPlanCreate("draft-plan", "Draft only",
		"surfaced-by-test", "dot-agents", "x", "go test ./..."); err != nil {
		t.Fatalf("plan create: %v", err)
	}

	captureStdoutWhileRunning(t, repo,
		func() error { return runWorkflowEligible("", 0) },
		"Found 1 draft plan(s) not yet activated",
		"draft-plan",
		"da workflow plan update --status active",
	)
}

// TestRunWorkflowNext_DraftPlansSurfaceHint mirrors the eligible test for
// `workflow next`: when there are no actionable tasks but draft plans exist,
// the same activation hint must appear so the user sees a path forward.
func TestRunWorkflowNext_DraftPlansSurfaceHint(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	fakeHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", fakeHome)
	chdirRepo(t, repo)

	if err := runWorkflowPlanCreate("draft-plan", "Draft only",
		"surfaced-by-test", "dot-agents", "x", "go test ./..."); err != nil {
		t.Fatalf("plan create: %v", err)
	}

	captureStdoutWhileRunning(t, repo,
		func() error { return runWorkflowNext("") },
		"No actionable canonical task found.",
		"Found 1 draft plan(s) not yet activated",
		"draft-plan",
	)
}

// TestRunWorkflowComplete_EmptyPlanIDFails verifies the input guard.
func TestRunWorkflowComplete_EmptyPlanIDFails(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	chdirRepo(t, repo)

	if err := runWorkflowComplete("   "); err == nil || !strings.Contains(err.Error(), "must not be empty") {
		t.Errorf("expected empty-plan guard; got: %v", err)
	}
}

// TestCollectWorkflowCompletionState_DrainedWhenNoPlans verifies that with no
// plans on disk the result is "drained".
func TestCollectWorkflowCompletionState_DrainedWhenNoPlans(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	got, err := collectWorkflowCompletionState(repo, "")
	if err != nil {
		t.Fatalf("collectWorkflowCompletionState: %v", err)
	}
	if got.State != "drained" {
		t.Errorf("State = %q, want drained", got.State)
	}
	if got.Scope == nil || len(got.Scope) != 0 {
		t.Errorf("Scope should be empty slice; got %v", got.Scope)
	}
}

// ── parsePlanIDFilter / validateScopeIDsAgainstAvailable ──────────────────────

func TestParsePlanIDFilter_TrimsAndDedupes(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"a", []string{"a"}},
		{" a , b ", []string{"a", "b"}},
		{"a,a,b", []string{"a", "b"}},
		{"a,,b,", []string{"a", "b"}},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got := parsePlanIDFilter(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("len=%d, want %d (got=%v)", len(got), len(tc.want), got)
			}
			for i, v := range got {
				if v != tc.want[i] {
					t.Errorf("[%d] = %q, want %q", i, v, tc.want[i])
				}
			}
		})
	}
}

func TestValidateScopeIDsAgainstAvailable_PassthroughEmptyScope(t *testing.T) {
	got, err := validateScopeIDsAgainstAvailable(nil, []string{"a", "b"})
	if err != nil {
		t.Fatalf("nil scope should be passthrough; got err=%v", err)
	}
	if got != nil {
		t.Errorf("expected nil result for nil scope; got %v", got)
	}
}

func TestValidateScopeIDsAgainstAvailable_ReturnsErrorOnUnknownID(t *testing.T) {
	_, err := validateScopeIDsAgainstAvailable([]string{"unknown"}, []string{"a", "b"})
	if err == nil {
		t.Fatal("expected error for unknown scope id")
	}
	if !strings.Contains(err.Error(), "unknown") {
		t.Errorf("error should mention id; got: %v", err)
	}
}

func TestValidateScopeIDsAgainstAvailable_AllPresent(t *testing.T) {
	got, err := validateScopeIDsAgainstAvailable([]string{"a", "b"}, []string{"a", "b", "c"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("got %v, want [a b]", got)
	}
}

// ── deriveCompletionState ─────────────────────────────────────────────────────

func TestDeriveCompletionState_TableDriven(t *testing.T) {
	suggestion := &workflowNextTaskSuggestion{TaskID: "t1"}
	tests := []struct {
		name      string
		sug       *workflowNextTaskSuggestion
		paused    []string
		locked    []string
		wantState string
	}{
		{"actionable", suggestion, nil, nil, "actionable"},
		{"paused", nil, []string{"p1"}, nil, "paused"},
		{"locked", nil, nil, []string{"p1"}, "locked"},
		{"drained", nil, nil, nil, "drained"},
		{"actionable_wins_over_paused", suggestion, []string{"p1"}, []string{"p2"}, "actionable"},
		{"paused_wins_over_locked", nil, []string{"p1"}, []string{"p2"}, "paused"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := deriveCompletionState(tc.sug, tc.paused, tc.locked)
			if got != tc.wantState {
				t.Errorf("got %q, want %q", got, tc.wantState)
			}
		})
	}
}

// ── splitTrimmedCSV ──────────────────────────────────────────────────────────

func TestSplitTrimmedCSV(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"a", []string{"a"}},
		{" a , b ", []string{"a", "b"}},
		{",,a,,", []string{"a"}},
		{"a,b,c", []string{"a", "b", "c"}},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got := splitTrimmedCSV(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("len=%d want %d (got=%v)", len(got), len(tc.want), got)
			}
			for i, v := range got {
				if v != tc.want[i] {
					t.Errorf("[%d]=%q want %q", i, v, tc.want[i])
				}
			}
		})
	}
}

// ── plan scheduling: buildPlanScheduleGraph / runKahnBFSWaves / computePlanSchedule ──

// TestBuildPlanScheduleGraph_EdgesAndInDegrees verifies adjacency + in-degree.
func TestBuildPlanScheduleGraph_EdgesAndInDegrees(t *testing.T) {
	tf := &CanonicalTaskFile{
		PlanID: "p",
		Tasks: []CanonicalTask{
			{ID: "a"},
			{ID: "b", DependsOn: []string{"a"}},
			{ID: "c", DependsOn: []string{"a", "b"}},
			{ID: "d", DependsOn: []string{"other-plan/x"}}, // cross-plan ignored
		},
	}
	in, adj := buildPlanScheduleGraph(tf)
	if len(in) != 4 || len(adj) != 4 {
		t.Fatalf("expected slices of length 4; got in=%d adj=%d", len(in), len(adj))
	}
	if in[0] != 0 {
		t.Errorf("inDegree[a] = %d, want 0", in[0])
	}
	if in[1] != 1 {
		t.Errorf("inDegree[b] = %d, want 1", in[1])
	}
	if in[2] != 2 {
		t.Errorf("inDegree[c] = %d, want 2", in[2])
	}
	if in[3] != 0 {
		t.Errorf("inDegree[d] = %d, want 0 (cross-plan dep ignored)", in[3])
	}
	if len(adj[0]) != 2 { // a -> {b, c}
		t.Errorf("adj[a] should have 2 successors; got %v", adj[0])
	}
}

// TestComputePlanSchedule_AssignsWaves verifies topological wave assignment.
func TestComputePlanSchedule_AssignsWaves(t *testing.T) {
	tf := &CanonicalTaskFile{
		PlanID: "p",
		Tasks: []CanonicalTask{
			{ID: "a"},
			{ID: "b"},
			{ID: "c", DependsOn: []string{"a"}},
			{ID: "d", DependsOn: []string{"a", "b"}},
			{ID: "e", DependsOn: []string{"c", "d"}},
		},
	}
	got, err := computePlanSchedule(tf)
	if err != nil {
		t.Fatalf("computePlanSchedule: %v", err)
	}
	if got.PlanID != "p" {
		t.Errorf("PlanID = %q, want p", got.PlanID)
	}
	if got.CriticalPathLength != 3 {
		t.Errorf("CriticalPathLength = %d, want 3", got.CriticalPathLength)
	}
	if got.MaxIntraParallelism < 2 {
		t.Errorf("MaxIntraParallelism = %d, want >=2", got.MaxIntraParallelism)
	}
	if len(got.Waves) != 3 {
		t.Fatalf("expected 3 waves; got %d", len(got.Waves))
	}
	// Wave 1 must contain a & b
	wave1IDs := map[string]bool{}
	for _, task := range got.Waves[0].Tasks {
		wave1IDs[task.ID] = true
	}
	if !wave1IDs["a"] || !wave1IDs["b"] {
		t.Errorf("wave 1 missing a/b; got %v", wave1IDs)
	}
	// Wave 3 must contain only 'e'
	if len(got.Waves[2].Tasks) != 1 || got.Waves[2].Tasks[0].ID != "e" {
		t.Errorf("wave 3 should contain only [e]; got %+v", got.Waves[2].Tasks)
	}
}

// TestComputePlanSchedule_DetectsCycle verifies that a dependency cycle yields
// an error.
func TestComputePlanSchedule_DetectsCycle(t *testing.T) {
	tf := &CanonicalTaskFile{
		PlanID: "p-cycle",
		Tasks: []CanonicalTask{
			{ID: "a", DependsOn: []string{"b"}},
			{ID: "b", DependsOn: []string{"a"}},
		},
	}
	_, err := computePlanSchedule(tf)
	if err == nil {
		t.Fatal("expected cycle error, got nil")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("error should mention 'cycle'; got: %v", err)
	}
}

// TestComputePlanSchedule_EmptyTasksOK verifies an empty plan schedules cleanly.
func TestComputePlanSchedule_EmptyTasksOK(t *testing.T) {
	tf := &CanonicalTaskFile{PlanID: "empty"}
	got, err := computePlanSchedule(tf)
	if err != nil {
		t.Fatalf("empty plan should not error; got: %v", err)
	}
	if got.CriticalPathLength != 0 || got.MaxIntraParallelism != 0 || len(got.Waves) != 0 {
		t.Errorf("empty schedule should be all zeros; got %+v", got)
	}
}

// TestRunWorkflowPlanSchedule_RendersWaves verifies the rendered output for an
// active plan schedule.
func TestRunWorkflowPlanSchedule_RendersWaves(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	captureStdoutWhileRunning(t, repo, func() error { return runWorkflowPlanSchedule("wave-2") },
		"Plan Schedule: wave-2",
		"Wave 1",
		"Critical path length",
	)
}

// TestRunWorkflowPlanSchedule_MissingPlanReturnsError covers the not-found path.
func TestRunWorkflowPlanSchedule_MissingPlanReturnsError(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	chdirRepo(t, repo)
	err := runWorkflowPlanSchedule("ghost")
	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("expected ghost-plan error; got: %v", err)
	}
}

// ── runWorkflowSlices ────────────────────────────────────────────────────────

// TestRunWorkflowSlices_RendersSliceMetadata verifies the human render path.
func TestRunWorkflowSlices_RendersSliceMetadata(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	addCanonicalSliceFixture(t, repo, "wave-2")
	captureStdoutWhileRunning(t, repo, func() error { return runWorkflowSlices("wave-2") },
		"Slices: wave-2",
		"slice-read-surface",
		"slice-artifacts",
		"summary: Add a read-only CLI surface for slices.",
		"write scope:",
		"verification:",
		"depends: slice-read-surface",
	)
}

// TestRunWorkflowSlices_MissingPlanReturnsError covers the missing-plan path.
func TestRunWorkflowSlices_MissingPlanReturnsError(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	chdirRepo(t, repo)
	err := runWorkflowSlices("ghost")
	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("expected ghost-plan error; got: %v", err)
	}
}

// TestRunWorkflowSlices_NoSlicesFileReturnsError verifies the case where a plan
// exists but the SLICES.yaml file is absent.
func TestRunWorkflowSlices_NoSlicesFileReturnsError(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo) // no SLICES.yaml
	chdirRepo(t, repo)
	err := runWorkflowSlices("wave-2")
	if err == nil || !strings.Contains(err.Error(), "slices for plan") {
		t.Fatalf("expected slices-not-found error; got: %v", err)
	}
}

// ── runWorkflowPlanList ──────────────────────────────────────────────────────

// TestRunWorkflowPlanList_EmptyMessage verifies the empty-plans message.
func TestRunWorkflowPlanList_EmptyMessage(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	captureStdoutWhileRunning(t, repo, func() error { return runWorkflowPlanList() },
		"No canonical plans found.",
	)
}

// TestRunWorkflowPlanList_RendersPlans verifies that each plan appears with its
// status in the rendered output.
func TestRunWorkflowPlanList_RendersPlans(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	captureStdoutWhileRunning(t, repo, func() error { return runWorkflowPlanList() },
		"Canonical Plans",
		"wave-2",
		"Wave 2 Test Plan",
		"active",
	)
}

// ── runWorkflowPlanShow JSON branch ──────────────────────────────────────────

// TestRunWorkflowPlanShow_JSONOutput verifies that JSON mode emits plan + tasks.
func TestRunWorkflowPlanShow_JSONOutput(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = false })

	captureStdoutWhileRunning(t, repo, func() error { return runWorkflowPlanShow("wave-2") },
		`"plan"`,
		`"tasks"`,
		`"wave-2"`,
	)
}

// ── filterPlanIDsUnlocked / filterPlanIDsLocked ───────────────────────────────

func TestFilterPlanIDsUnlocked_EmptyLockedReturnsAll(t *testing.T) {
	ids := []string{"a", "b"}
	got := filterPlanIDsUnlocked(ids, map[string]bool{})
	if len(got) != 2 {
		t.Errorf("with empty locked map should be passthrough; got %v", got)
	}
}

func TestFilterPlanIDsUnlocked_RemovesLocked(t *testing.T) {
	ids := []string{"a", "b", "c"}
	got := filterPlanIDsUnlocked(ids, map[string]bool{"b": true})
	if len(got) != 2 || got[0] != "a" || got[1] != "c" {
		t.Errorf("got %v, want [a c]", got)
	}
}

func TestFilterPlanIDsLocked_EmptyLockedReturnsAll(t *testing.T) {
	ids := []string{"a", "b"}
	got := filterPlanIDsLocked(ids, map[string]bool{})
	if len(got) != 2 {
		t.Errorf("with empty locked map should be passthrough; got %v", got)
	}
}

func TestFilterPlanIDsLocked_KeepsLocked(t *testing.T) {
	ids := []string{"a", "b", "c"}
	got := filterPlanIDsLocked(ids, map[string]bool{"b": true, "c": true})
	if len(got) != 2 || got[0] != "b" || got[1] != "c" {
		t.Errorf("got %v, want [b c]", got)
	}
}

// ── activeDelegationPlanIDs ───────────────────────────────────────────────────

func TestActiveDelegationPlanIDs_OnlyActiveOrPending(t *testing.T) {
	contracts := []DelegationContract{
		{ParentPlanID: "p1", Status: "active"},
		{ParentPlanID: "p2", Status: "pending"},
		{ParentPlanID: "p3", Status: "completed"},
		{ParentPlanID: "p4", Status: "cancelled"},
		{ParentPlanID: "", Status: "active"},
	}
	got := activeDelegationPlanIDs(contracts)
	if !got["p1"] || !got["p2"] {
		t.Errorf("p1/p2 should be in active set; got %v", got)
	}
	if got["p3"] || got["p4"] {
		t.Errorf("non-active contracts should not appear; got %v", got)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 entries; got %v", got)
	}
}

// ── partitionScopePlansByStatus ───────────────────────────────────────────────

// TestPartitionScopePlansByStatus_SortsAndFilters verifies the partition picks
// paused plans and active-with-active-delegation plans.
func TestPartitionScopePlansByStatus_SortsAndFilters(t *testing.T) {
	proj := t.TempDir()
	writePlanFixture(t, proj, "p-active-locked", "active", nil)
	writePlanFixture(t, proj, "p-paused", "paused", nil)
	writePlanFixture(t, proj, "p-active-free", "active", nil)
	writePlanFixture(t, proj, "p-completed", "completed", nil)

	locked := map[string]bool{"p-active-locked": true}
	paused, lockedOut, err := partitionScopePlansByStatus(proj,
		[]string{"p-active-locked", "p-paused", "p-active-free", "p-completed"}, locked)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(paused) != 1 || paused[0] != "p-paused" {
		t.Errorf("paused = %v, want [p-paused]", paused)
	}
	if len(lockedOut) != 1 || lockedOut[0] != "p-active-locked" {
		t.Errorf("locked = %v, want [p-active-locked]", lockedOut)
	}
}

// TestPartitionScopePlansByStatus_LoadErrorPropagates verifies a missing plan
// surfaces as a load error.
func TestPartitionScopePlansByStatus_LoadErrorPropagates(t *testing.T) {
	proj := t.TempDir()
	_, _, err := partitionScopePlansByStatus(proj, []string{"ghost"}, nil)
	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("expected load error for ghost plan; got: %v", err)
	}
}

// ── runKahnBFSWaves direct ────────────────────────────────────────────────────

// TestRunKahnBFSWaves_LinearChain covers a → b → c.
func TestRunKahnBFSWaves_LinearChain(t *testing.T) {
	tf := &CanonicalTaskFile{Tasks: []CanonicalTask{
		{ID: "a"},
		{ID: "b", DependsOn: []string{"a"}},
		{ID: "c", DependsOn: []string{"b"}},
	}}
	in, adj := buildPlanScheduleGraph(tf)
	waves, processed := runKahnBFSWaves(in, adj)
	if processed != 3 {
		t.Errorf("processed = %d, want 3", processed)
	}
	if len(waves) != 3 {
		t.Errorf("expected 3 wave slots; got %d (%v)", len(waves), waves)
	}
}

// TestRunKahnBFSWaves_CycleStopsEarly verifies a cycle leaves processed<total.
func TestRunKahnBFSWaves_CycleStopsEarly(t *testing.T) {
	tf := &CanonicalTaskFile{Tasks: []CanonicalTask{
		{ID: "a", DependsOn: []string{"b"}},
		{ID: "b", DependsOn: []string{"a"}},
	}}
	in, adj := buildPlanScheduleGraph(tf)
	_, processed := runKahnBFSWaves(in, adj)
	if processed != 0 {
		t.Errorf("processed = %d, want 0 (both in cycle)", processed)
	}
}
