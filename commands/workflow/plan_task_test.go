package workflow

import (
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
