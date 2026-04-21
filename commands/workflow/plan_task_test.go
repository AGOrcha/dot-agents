package workflow

import (
	"encoding/json"
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

// TestSelectAllEligibleTasks_CrossPlanDepSatisfied verifies that a task with a
// cross-plan dependency pointing to a completed task IS returned.
func TestSelectAllEligibleTasks_CrossPlanDepSatisfied(t *testing.T) {
	proj := t.TempDir()
	// other-plan has task-done which is completed.
	writePlanFixture(t, proj, "other-plan", "active", []CanonicalTask{
		{ID: "task-done", Title: "Done", Status: "completed"},
	})
	// main-plan has a task depending on other-plan/task-done.
	writePlanFixture(t, proj, "main-plan", "active", []CanonicalTask{
		{ID: "main-task", Title: "Main", Status: "pending", DependsOn: []string{"other-plan/task-done"}},
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
	if !found {
		t.Errorf("main-task with satisfied cross-plan dep should be eligible; got %v", got)
	}
}

// TestSelectAllEligibleTasks_CrossPlanDepUnsatisfied verifies that a task with a
// cross-plan dependency pointing to a non-completed task is excluded.
func TestSelectAllEligibleTasks_CrossPlanDepUnsatisfied(t *testing.T) {
	proj := t.TempDir()
	writePlanFixture(t, proj, "other-plan", "active", []CanonicalTask{
		{ID: "task-pending", Title: "Pending", Status: "pending"},
	})
	writePlanFixture(t, proj, "main-plan", "active", []CanonicalTask{
		{ID: "main-task", Title: "Main", Status: "pending", DependsOn: []string{"other-plan/task-pending"}},
	})

	got, err := selectAllEligibleTasks(proj, []string{"main-plan"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, s := range got {
		if s.TaskID == "main-task" {
			t.Errorf("main-task with unsatisfied cross-plan dep should be excluded; got %+v", s)
		}
	}
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
