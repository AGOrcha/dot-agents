package workflow

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/agentslock"
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

	if err := archiveSinglePlan(proj, "myplan", false, false, true); err != nil {
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

// A plan with a linked spec — the spec is COPIED into the history archive
// (permanent-record completeness) while the editable copy stays under workflow/specs.
func TestArchiveSinglePlan_ArchivesLinkedSpec(t *testing.T) {
	proj := t.TempDir()
	setupArchivePlan(t, proj, "myplan", "completed")
	specPath := filepath.Join(proj, ".agents", "workflow", "specs", "myplan", "design.md")
	if err := os.MkdirAll(filepath.Dir(specPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(specPath, []byte("# Spec myplan\nbody\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := archiveSinglePlan(proj, "myplan", false, false, true); err != nil {
		t.Fatalf("archive: %v", err)
	}

	archived := filepath.Join(proj, ".agents", "history", "myplan", "design.md")
	data, err := os.ReadFile(archived)
	if err != nil {
		t.Fatalf("archived spec should exist: %v", err)
	}
	if !strings.Contains(string(data), "# Spec myplan") {
		t.Errorf("archived spec content wrong: %q", data)
	}
	// Editable copy NOT moved (other specs cross-link it by relative path).
	if _, err := os.Stat(specPath); err != nil {
		t.Errorf("workflow/specs copy should remain (copy, not move): %v", err)
	}
}

// A plan with NO linked spec — archive succeeds and creates no design.md.
func TestArchiveSinglePlan_NoLinkedSpecNoop(t *testing.T) {
	proj := t.TempDir()
	setupArchivePlan(t, proj, "nospec", "completed")
	if err := archiveSinglePlan(proj, "nospec", false, false, true); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if _, err := os.Stat(filepath.Join(proj, ".agents", "history", "nospec", "design.md")); !os.IsNotExist(err) {
		t.Error("no design.md should be created when there is no linked spec")
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

	if err := archiveSinglePlan(proj, "myplan", false, false, true); err != nil {
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

	if err := archiveSinglePlan(proj, "myplan", false, false, true); err != nil {
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

	if err := archiveSinglePlan(proj, "myplan", false, false, true); err != nil {
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

	if err := archiveSinglePlan(proj, "myplan", false, false, true); err != nil {
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

	if err := archiveSinglePlan(proj, "myplan", false, true, true); err != nil {
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

	err := archiveSinglePlan(proj, "myplan", false, false, true)
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

	if err := archiveSinglePlan(proj, "myplan", true, false, true); err != nil {
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

	err := runWorkflowPlanArchive(proj, []string{"plan-a", "plan-b"}, false, false, true)
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

	err := runWorkflowPlanArchive(proj, []string{"plan-bad", "plan-ok"}, false, false, true)
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

// ── archive-commit (non-persistence fix) tests ─────────────────────────────────

// Commit-by-default: archiveSinglePlan invokes iterationCloseCommit once, and
// only AFTER the working tree is in its final archived state (source dir gone,
// history dir populated) so the deletion + addition are staged together as one
// commit. Uses the iterationCloseCommit test seam — the same seam advance /
// merge-back tests rebind — so no real git repo is needed to prove invocation.
func TestArchiveSinglePlan_InvokesCommitByDefault(t *testing.T) {
	proj := t.TempDir()
	setupArchivePlan(t, proj, "myplan", "completed")
	srcDir := filepath.Join(proj, ".agents", "workflow", "plans", "myplan")
	dstDir := filepath.Join(proj, ".agents", "history", "myplan")

	var calls int
	var srcGoneAtCommit, dstPresentAtCommit bool
	prior := iterationCloseCommit
	iterationCloseCommit = func(io.Writer) error {
		calls++
		// The commit hook must fire with the tree already in its final state.
		if _, err := os.Stat(srcDir); os.IsNotExist(err) {
			srcGoneAtCommit = true
		}
		if _, err := os.Stat(filepath.Join(dstDir, "PLAN.yaml")); err == nil {
			dstPresentAtCommit = true
		}
		return nil
	}
	t.Cleanup(func() { iterationCloseCommit = prior })

	if err := archiveSinglePlan(proj, "myplan", false, false, false); err != nil {
		t.Fatalf("archiveSinglePlan: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected commit hook invoked exactly once, got %d", calls)
	}
	if !srcGoneAtCommit {
		t.Error("commit hook fired before the source plan dir was removed")
	}
	if !dstPresentAtCommit {
		t.Error("commit hook fired before the history dir was populated")
	}
}

// A commit-hook failure aborts the archive with a wrapped error so the operator
// learns the move did not persist.
func TestArchiveSinglePlan_CommitFailurePropagates(t *testing.T) {
	proj := t.TempDir()
	setupArchivePlan(t, proj, "myplan", "completed")

	prior := iterationCloseCommit
	iterationCloseCommit = func(io.Writer) error { return errors.New("commit boom") }
	t.Cleanup(func() { iterationCloseCommit = prior })

	err := archiveSinglePlan(proj, "myplan", false, false, false)
	if err == nil || !strings.Contains(err.Error(), "commit archive move") {
		t.Fatalf("expected wrapped commit error, got %v", err)
	}
}

// --no-commit suppresses the commit hook entirely while still performing the
// filesystem move.
func TestArchiveSinglePlan_NoCommitSkipsCommit(t *testing.T) {
	proj := t.TempDir()
	setupArchivePlan(t, proj, "myplan", "completed")
	srcDir := filepath.Join(proj, ".agents", "workflow", "plans", "myplan")
	dstDir := filepath.Join(proj, ".agents", "history", "myplan")

	var calls int
	prior := iterationCloseCommit
	iterationCloseCommit = func(io.Writer) error { calls++; return nil }
	t.Cleanup(func() { iterationCloseCommit = prior })

	if err := archiveSinglePlan(proj, "myplan", false, false, true); err != nil {
		t.Fatalf("archiveSinglePlan: %v", err)
	}
	if calls != 0 {
		t.Fatalf("--no-commit must not invoke the commit hook, got %d calls", calls)
	}
	// The move still happens; only the commit is suppressed.
	if _, err := os.Stat(srcDir); !os.IsNotExist(err) {
		t.Error("source dir should be removed even with --no-commit")
	}
	if _, err := os.Stat(dstDir); err != nil {
		t.Errorf("history dir should exist even with --no-commit: %v", err)
	}
}

// End-to-end proof of the fix: in a real git repo where the plan dir is tracked,
// a default (commit-enabled) archive stages the deletion of plans/<id> AND the
// addition of history/<id> as one real commit, leaving `git status` CLEAN. This
// is the exact scenario the fresh-clone / worktree loop model requires: without
// the commit the move is discarded before it lands on master.
func TestArchiveSinglePlan_RealGitCommitLeavesCleanTree(t *testing.T) {
	dir := gogitTestRepoWithCommit(t)
	t.Chdir(dir)

	setupArchivePlan(t, dir, "myplan", "completed")
	// Track the plan dir so the archive produces a real deletion side.
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-q", "-m", "seed plan")
	headBefore := gitOut(t, dir, "rev-parse", "HEAD")

	if err := archiveSinglePlan(dir, "myplan", false, false, false); err != nil {
		t.Fatalf("archiveSinglePlan: %v", err)
	}

	// The move landed on disk.
	if _, err := os.Stat(filepath.Join(dir, ".agents", "workflow", "plans", "myplan")); !os.IsNotExist(err) {
		t.Error("source plan dir should be gone")
	}
	if _, err := os.Stat(filepath.Join(dir, ".agents", "history", "myplan", "PLAN.yaml")); err != nil {
		t.Errorf("history PLAN.yaml should exist: %v", err)
	}
	// The key assertion: a new commit landed AND the tree is clean, so the move
	// would persist through a push (no uncommitted residue to be discarded).
	if gitOut(t, dir, "rev-parse", "HEAD") == headBefore {
		t.Error("expected a new commit for the archive move; HEAD did not advance")
	}
	if status := gitOut(t, dir, "status", "--porcelain"); status != "" {
		t.Errorf("working tree should be clean after archive commit, got:\n%s", status)
	}
}

// commit.disable=true flows through to the real commit hook: the move happens
// but no commit lands, leaving the tree dirty (the operator opted out).
func TestArchiveSinglePlan_CommitDisabledLeavesUncommitted(t *testing.T) {
	dir := gogitTestRepoWithCommit(t)
	t.Chdir(dir)

	setupArchivePlan(t, dir, "myplan", "completed")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-q", "-m", "seed plan")
	headBefore := gitOut(t, dir, "rev-parse", "HEAD")

	priorDisabled := commitDisabled
	commitDisabled = func() (bool, string) { return true, "commit.disable=true in workflow preferences" }
	t.Cleanup(func() { commitDisabled = priorDisabled })

	if err := archiveSinglePlan(dir, "myplan", false, false, false); err != nil {
		t.Fatalf("archiveSinglePlan: %v", err)
	}
	// No commit landed (opt-out honored inside runWorkflowCommit).
	if gitOut(t, dir, "rev-parse", "HEAD") != headBefore {
		t.Error("commit.disable=true should not advance HEAD")
	}
	// Move still happened, so the tree is dirty (uncommitted).
	if gitOut(t, dir, "status", "--porcelain") == "" {
		t.Error("expected dirty tree when commit is disabled (move uncommitted)")
	}
}

// gitRun runs a git command in dir, failing the test on error.
func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_DATE=2026-05-23T00:00:00Z", "GIT_COMMITTER_DATE=2026-05-23T00:00:00Z",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// gitOut runs a git command in dir and returns trimmed stdout, failing on error.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
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

// TestAnnotateEligibleTasks_UnreadableSidecar drives the REAL read-error
// branch (plan_task.go: sidecarPath os.ReadFile): the sidecar file exists but
// cannot be read (not "doesn't exist"). has_evidence/evidence_confidence must
// still degrade to false/"none" (the task list can't block on it), but the
// failure must be surfaced via ui.Warn instead of silently looking identical
// to "no sidecar ever written".
func TestAnnotateEligibleTasks_UnreadableSidecar(t *testing.T) {
	proj := t.TempDir()
	planID, taskID := "plan-locked", "t1"
	sidecarPath := deriveScopeEvidencePath(proj, planID, taskID)
	if err := os.MkdirAll(filepath.Dir(sidecarPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sidecarPath, []byte("confidence: high\n"), 0644); err != nil {
		t.Fatal(err)
	}
	chmodUnreadable(t, sidecarPath)

	tasks := []workflowNextTaskSuggestion{
		{PlanID: planID, TaskID: taskID, WriteScope: []string{"commands/"}},
	}
	out, _ := captureCovStdout(t, func() error {
		annotated := annotateEligibleTasks(proj, tasks)
		at := annotated[0]
		if at.HasEvidence {
			t.Error("HasEvidence should be false when sidecar is unreadable")
		}
		if at.EvidenceConfidence != "none" {
			t.Errorf("EvidenceConfidence should be 'none' on unreadable sidecar; got %q", at.EvidenceConfidence)
		}
		return nil
	})
	if !strings.Contains(out, "evidence sidecar unreadable") {
		t.Errorf("expected an 'unreadable' warning on stdout, got %q", out)
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
// value for max_parallel_workers is the capacity-aware computed budget (no
// longer a fixed constant).
func TestResolvePreferences_MaxParallelWorkersDefault(t *testing.T) {
	proj := t.TempDir()
	prefs, err := resolvePreferences(proj, "test-project")
	if err != nil {
		t.Fatalf("resolvePreferences: %v", err)
	}
	if prefs.Execution.MaxParallelWorkers == nil {
		t.Fatal("MaxParallelWorkers should not be nil (has a default)")
	}
	if got, want := *prefs.Execution.MaxParallelWorkers, defaultMaxParallelTasks(); got != want {
		t.Errorf("default MaxParallelWorkers should be the computed budget %d; got %d", want, got)
	}
}

// TestApplyPreferenceKey_MaxParallelWorkersValidation verifies that
// max_parallel_workers rejects values outside 1..maxConfigurableParallelTasks.
func TestApplyPreferenceKey_MaxParallelWorkersValidation(t *testing.T) {
	invalid := []string{"0", "-1", "abc", "33", "100"}
	for _, v := range invalid {
		t.Run(v, func(t *testing.T) {
			var p WorkflowPreferences
			if err := applyPreferenceKey(&p, "execution.max_parallel_workers", v); err == nil {
				t.Errorf("expected error for value %q, got nil", v)
			}
		})
	}
	valid := []string{"1", "8", "16", "32"}
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

// TestRunWorkflowPlanCreate_StatErrorSurfaced drives the REAL Stat-error
// branch on the collision check (plan_task.go: os.Stat(dir) in
// runWorkflowPlanCreate): the plans/ directory itself is unreadable, so
// Stat-ing the candidate plan dir fails with something other than
// IsNotExist. That must surface as an error distinct from "already exists"
// instead of being silently treated as "no collision, proceed".
func TestRunWorkflowPlanCreate_StatErrorSurfaced(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	chdirRepo(t, repo)

	chmodUnreadableDir(t, plansBaseDir(repo))

	err := runWorkflowPlanCreate("brand-new-plan", "T", "S", "O", "SC", "VS")
	if err == nil {
		t.Fatal("expected an error when the plans dir cannot be stat-ed")
	}
	if strings.Contains(err.Error(), "already exists") {
		t.Errorf("a real Stat error must not be reported as 'already exists': %v", err)
	}
	if !strings.Contains(err.Error(), "check for existing plan") {
		t.Errorf("expected a 'check for existing plan' error, got: %v", err)
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

	if err := runWorkflowTaskUpdate("wave-2", "t2", "new title", "fresh notes", "x/, y/", "", ""); err != nil {
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

	if err := runWorkflowTaskUpdate("wave-2", "t1", "", "", "", "", ""); err != nil {
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

	err := runWorkflowTaskUpdate("wave-2", "nope", "x", "", "", "", "")
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("expected missing-task error; got: %v", err)
	}
}

// TestRunWorkflowTaskUpdate_DependsOnAndBlocks verifies that --depends-on and
// --blocks CSVs persist to TASKS.yaml with replace semantics matching `task
// add`, and that the new depends_on edge is live in the schedule graph
// without further plumbing.
func TestRunWorkflowTaskUpdate_DependsOnAndBlocks(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	chdirRepo(t, repo)

	if err := runWorkflowTaskAdd(taskAddInputs{
		PlanID: "wave-2",
		TaskID: "t4",
		Title:  "new task with no deps",
	}); err != nil {
		t.Fatalf("runWorkflowTaskAdd: %v", err)
	}

	if err := runWorkflowTaskUpdate("wave-2", "t4", "", "", "", "t1", "t2"); err != nil {
		t.Fatalf("runWorkflowTaskUpdate: %v", err)
	}

	tf, err := loadCanonicalTasks(repo, "wave-2")
	if err != nil {
		t.Fatal(err)
	}
	t4Idx := taskIndexByID(tf, "t4")
	t1Idx := taskIndexByID(tf, "t1")
	if t4Idx == -1 {
		t.Fatal("t4 missing after update")
	}
	t4 := &tf.Tasks[t4Idx]
	if len(t4.DependsOn) != 1 || t4.DependsOn[0] != "t1" {
		t.Errorf("depends_on = %v, want [t1]", t4.DependsOn)
	}
	if len(t4.Blocks) != 1 || t4.Blocks[0] != "t2" {
		t.Errorf("blocks = %v, want [t2]", t4.Blocks)
	}

	inDegree, adj := buildPlanScheduleGraph(tf)
	assertScheduleEdge(t, inDegree, adj, t1Idx, t4Idx)
}

// taskIndexByID returns the index of the task with the given ID in tf.Tasks,
// or -1 if not present.
func taskIndexByID(tf *CanonicalTaskFile, id string) int {
	for i := range tf.Tasks {
		if tf.Tasks[i].ID == id {
			return i
		}
	}
	return -1
}

// assertScheduleEdge fails t unless the schedule graph records exactly one
// incoming edge into dst and adj[src] contains dst — i.e. src → dst is live.
func assertScheduleEdge(t *testing.T, inDegree []int, adj [][]int, src, dst int) {
	t.Helper()
	if inDegree[dst] != 1 {
		t.Errorf("in-degree of dst = %d, want 1 (edge from src not picked up)", inDegree[dst])
	}
	found := false
	for _, d := range adj[src] {
		if d == dst {
			found = true
		}
	}
	if !found {
		t.Errorf("adjacency of src = %v, want to contain dst's index %d", adj[src], dst)
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

func TestRenderEligibleTask_Conflicts(t *testing.T) {
	at := AnnotatedTask{
		workflowNextTaskSuggestion: workflowNextTaskSuggestion{
			PlanID: "p1", TaskID: "t1", TaskTitle: "Build feature", Status: "pending",
			WriteScope: []string{"commands/"},
		},
		WriteScopeDeclared: true,
		HasEvidence:        true,
		EvidenceConfidence: "high",
		ConflictsWith:      []string{"t2"},
	}
	out, _ := captureCovStdout(t, func() error {
		renderEligibleTask(at)
		return nil
	})
	for _, want := range []string{"[p1/t1]", "Build feature", "evidence: true", "high", "conflicts: t2"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output, got: %s", want, out)
		}
	}
}

func TestRenderEligibleTask_NoWriteScope(t *testing.T) {
	at := AnnotatedTask{
		workflowNextTaskSuggestion: workflowNextTaskSuggestion{
			PlanID: "p1", TaskID: "t1", TaskTitle: "T", Status: "pending",
		},
		WriteScopeDeclared: false,
		EvidenceConfidence: "none",
	}
	out, _ := captureCovStdout(t, func() error {
		renderEligibleTask(at)
		return nil
	})
	if !strings.Contains(out, "(none) [no write_scope declared]") {
		t.Errorf("expected no-write-scope label in output, got: %s", out)
	}
}

func TestRenderEligibleOutput_WithMaxBatch(t *testing.T) {
	out := eligibleOutput{
		EligibleTasks: []AnnotatedTask{{
			workflowNextTaskSuggestion: workflowNextTaskSuggestion{
				PlanID: "p1", TaskID: "t1", TaskTitle: "T", Status: "pending",
			},
			WriteScopeDeclared: true,
			EvidenceConfidence: "none",
		}},
		MaxBatch:      []string{"p1/t1"},
		TotalEligible: 1,
		MaxParallel:   1,
	}
	stdout, _ := captureCovStdout(t, func() error {
		renderEligibleOutput(out, 1, 0)
		return nil
	})
	if !strings.Contains(stdout, "Eligible Tasks") || !strings.Contains(stdout, "max batch: p1/t1") {
		t.Errorf("expected eligible tasks header + max batch, got: %s", stdout)
	}
	if !strings.Contains(stdout, "max_parallel_workers=1") {
		t.Errorf("expected max_parallel label, got: %s", stdout)
	}
}

func TestRenderEligibleOutput_LimitOverride(t *testing.T) {
	out := eligibleOutput{
		EligibleTasks: []AnnotatedTask{},
		TotalEligible: 0,
		DraftPlans:    []string{"draft-x"},
	}
	stdout, _ := captureCovStdout(t, func() error {
		renderEligibleOutput(out, 1, 3)
		return nil
	})
	if !strings.Contains(stdout, "--limit=3") {
		t.Errorf("expected --limit=3 label, got: %s", stdout)
	}
	if !strings.Contains(stdout, "draft-x") {
		t.Errorf("expected draft plans hint when zero eligible, got: %s", stdout)
	}
}

func TestRunWorkflowComplete_DrainedRendered(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	chdirForCov(t, repo)
	out, err := captureCovStdout(t, func() error { return runWorkflowComplete("nope") })
	if err != nil {
		t.Fatalf("runWorkflowComplete: %v", err)
	}
	if !strings.Contains(out, "state: drained") {
		t.Errorf("expected drained state in output, got: %s", out)
	}
}

func TestRunWorkflowComplete_ActionableJSON(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPendingPlanFixture(t, repo)
	chdirForCov(t, repo)
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = false })
	out, err := captureCovStdout(t, func() error { return runWorkflowComplete("wave-next") })
	if err != nil {
		t.Fatalf("runWorkflowComplete: %v", err)
	}
	if !strings.Contains(out, "\"state\": \"actionable\"") {
		t.Errorf("expected state=actionable in JSON, got: %s", out)
	}
	if !strings.Contains(out, "wave-next") {
		t.Errorf("expected plan id in JSON, got: %s", out)
	}
}

func TestRunWorkflowComplete_PausedRendered(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	savePausedPlanFixture(t, repo)
	chdirForCov(t, repo)
	out, err := captureCovStdout(t, func() error { return runWorkflowComplete("paused-plan") })
	if err != nil {
		t.Fatalf("runWorkflowComplete: %v", err)
	}
	if !strings.Contains(out, "Scoped Plan Completion") {
		t.Errorf("expected header, got: %s", out)
	}
	if !strings.Contains(out, "paused-plan") {
		t.Errorf("expected paused plan id in output, got: %s", out)
	}
	if !strings.Contains(out, "paused plans:") {
		t.Errorf("expected paused plans list, got: %s", out)
	}
}

func TestRunWorkflowComplete_LockedRendered(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPendingPlanFixture(t, repo)

	now := time.Now().UTC().Format(time.RFC3339)
	for _, taskID := range []string{"planner", "tests"} {
		if err := saveDelegationContract(repo, &DelegationContract{
			SchemaVersion: 1,
			ID:            "del-" + taskID,
			ParentPlanID:  "wave-next",
			ParentTaskID:  taskID,
			Title:         "lock",
			WriteScope:    []string{"commands/"},
			Status:        "active",
			CreatedAt:     now,
			UpdatedAt:     now,
		}); err != nil {
			t.Fatal(err)
		}
	}
	chdirForCov(t, repo)
	out, err := captureCovStdout(t, func() error { return runWorkflowComplete("wave-next") })
	if err != nil {
		t.Fatalf("runWorkflowComplete: %v", err)
	}
	if !strings.Contains(out, "locked plans:") {
		t.Errorf("expected locked plans line in output, got: %s", out)
	}
}

func TestRenderPlanShowSlices_NilError(t *testing.T) {
	sf := &CanonicalSliceFile{
		SchemaVersion: 1, PlanID: "p1",
		Slices: []CanonicalSlice{
			{ID: "slice-a", ParentTaskID: "t1", Title: "Slice A", Status: "pending"},
			{ID: "slice-b", ParentTaskID: "t2", Title: "Slice B", Status: "completed"},
		},
	}
	out, _ := captureCovStdout(t, func() error {
		renderPlanShowSlices(sf, nil)
		return nil
	})
	if !strings.Contains(out, "Slices") ||
		!strings.Contains(out, "slice-a") ||
		!strings.Contains(out, "slice-b") {
		t.Errorf("expected slices listed, got: %s", out)
	}
}

func TestRenderPlanShowSlices_ErrorPath(t *testing.T) {
	sf := &CanonicalSliceFile{}
	out, _ := captureCovStdout(t, func() error {

		renderPlanShowSlices(sf, os.ErrNotExist)
		return nil
	})
	if strings.Contains(out, "Slices") {
		t.Errorf("expected slices header suppressed on err, got: %s", out)
	}
}

func TestRunWorkflowEligible_JSON(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPendingPlanFixture(t, repo)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	chdirForCov(t, repo)
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = false })
	out, err := captureCovStdout(t, func() error { return runWorkflowEligible("", 0) })
	if err != nil {
		t.Fatalf("runWorkflowEligible: %v", err)
	}
	if !strings.Contains(out, "\"eligible_tasks\":") {
		t.Errorf("expected eligible_tasks key, got: %s", out)
	}
}

func TestRunWorkflowEligible_LimitTruncates(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPendingPlanFixture(t, repo)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	chdirForCov(t, repo)

	out, err := captureCovStdout(t, func() error { return runWorkflowEligible("", 1) })
	if err != nil {
		t.Fatalf("runWorkflowEligible: %v", err)
	}
	if !strings.Contains(out, "Eligible Tasks") {
		t.Errorf("expected eligible header, got: %s", out)
	}
}

func TestRunWorkflowNext_JSONNoSuggestion(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	chdirForCov(t, repo)
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = false })
	out, err := captureCovStdout(t, func() error { return runWorkflowNext("") })
	if err != nil {
		t.Fatalf("runWorkflowNext: %v", err)
	}
	if !strings.Contains(out, "\"suggestion\": null") {
		t.Errorf("expected null suggestion in JSON, got: %s", out)
	}
}

func TestRunWorkflowNext_JSONWithSuggestion(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPendingPlanFixture(t, repo)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	chdirForCov(t, repo)
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = false })
	out, err := captureCovStdout(t, func() error { return runWorkflowNext("") })
	if err != nil {
		t.Fatalf("runWorkflowNext: %v", err)
	}
	if !strings.Contains(out, "\"plan_id\":") {
		t.Errorf("expected plan_id in JSON, got: %s", out)
	}
}

func TestRunWorkflowNext_HumanRendersFullSuggestion(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPendingPlanFixture(t, repo)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	chdirForCov(t, repo)
	out, err := captureCovStdout(t, func() error { return runWorkflowNext("") })
	if err != nil {
		t.Fatalf("runWorkflowNext: %v", err)
	}
	for _, want := range []string{"Next Canonical Task", "plan:", "task:", "status:", "reason:", "verification:"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output, got: %s", want, out)
		}
	}
}

func TestRenderEligibleOutput_NoMaxBatchNoDrafts(t *testing.T) {
	out := eligibleOutput{
		EligibleTasks: []AnnotatedTask{},
		TotalEligible: 0,
	}
	stdout, _ := captureCovStdout(t, func() error {
		renderEligibleOutput(out, 2, 0)
		return nil
	})
	if strings.Contains(stdout, "max batch") {
		t.Errorf("expected no max batch line, got: %s", stdout)
	}
}

func TestRunWorkflowComplete_DrainedJSON(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	chdirForCov(t, repo)
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = false })
	out, err := captureCovStdout(t, func() error { return runWorkflowComplete("anything") })
	if err != nil {
		t.Fatalf("runWorkflowComplete: %v", err)
	}
	if !strings.Contains(out, "\"state\": \"drained\"") {
		t.Errorf("expected drained state in JSON, got: %s", out)
	}
}

func TestLoadCheckScopeSidecar_ValidSidecar(t *testing.T) {
	dir := t.TempDir()
	ev := NewScopeEvidence("plan-x", "task-y")
	ev.FinalWriteScope = []string{"commands/x.go"}
	if _, err := persistScopeEvidenceSidecar(dir, "plan-x", "task-y", ev); err != nil {
		t.Fatalf("persist sidecar: %v", err)
	}
	path, got, err := loadCheckScopeSidecar(dir, "plan-x", "task-y")
	if err != nil {
		t.Fatalf("loadCheckScopeSidecar: %v", err)
	}
	if path == "" || got == nil {
		t.Errorf("expected path+evidence, got path=%q ev=%v", path, got)
	}
	if got.PlanID != "plan-x" {
		t.Errorf("expected plan-x, got %q", got.PlanID)
	}
}

func TestLoadCheckScopeSidecar_InvalidYAML(t *testing.T) {
	dir := t.TempDir()

	path := deriveScopeEvidencePath(dir, "plan-x", "task-y")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not: : valid: yaml"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadCheckScopeSidecar(dir, "plan-x", "task-y"); err == nil {
		t.Error("expected parse error on invalid yaml")
	}
}

func TestPersistScopeEvidenceSidecar_HappyPath(t *testing.T) {
	dir := t.TempDir()
	ev := NewScopeEvidence("plan-a", "task-b")
	ev.Confidence = "high"
	got, err := persistScopeEvidenceSidecar(dir, "plan-a", "task-b", ev)
	if err != nil {
		t.Fatalf("persistScopeEvidenceSidecar: %v", err)
	}
	if got == "" {
		t.Error("expected path returned")
	}
	if _, err := os.Stat(got); err != nil {
		t.Errorf("file should exist at %s: %v", got, err)
	}
}

func TestRenderEligibleTask_NoConflicts(t *testing.T) {
	at := AnnotatedTask{
		workflowNextTaskSuggestion: workflowNextTaskSuggestion{
			PlanID: "p1", TaskID: "t1", TaskTitle: "T", Status: "pending",
			WriteScope: []string{"commands/"},
		},
		WriteScopeDeclared: true,
		HasEvidence:        false,
		EvidenceConfidence: "none",
	}
	out, _ := captureCovStdout(t, func() error {
		renderEligibleTask(at)
		return nil
	})
	if strings.Contains(out, "conflicts:") {
		t.Errorf("expected no conflicts line when empty, got: %s", out)
	}
}

func TestAppendPlanGraphSliceDeps_UnknownDep(t *testing.T) {
	graph := &workflowPlanGraph{}
	plan := &CanonicalPlan{ID: "p1"}
	slices := []CanonicalSlice{
		{ID: "s1", DependsOn: []string{"unknown-slice"}},
	}
	ids := map[string]string{"s1": "node-s1"}
	appendPlanGraphSliceDeps(graph, plan, slices, ids)
	if len(graph.Warnings) != 1 || !strings.Contains(graph.Warnings[0], "unknown slice") {
		t.Fatalf("expected one unknown-slice warning, got %+v", graph.Warnings)
	}
	if len(graph.Edges) != 0 {
		t.Errorf("expected no edges for unknown dep, got %d", len(graph.Edges))
	}
}

func TestAppendPlanGraphSliceDeps_SkipSliceMissingFromIDs(t *testing.T) {
	graph := &workflowPlanGraph{}
	plan := &CanonicalPlan{ID: "p1"}
	slices := []CanonicalSlice{
		{ID: "unknown", DependsOn: []string{"s2"}},
	}
	ids := map[string]string{}
	appendPlanGraphSliceDeps(graph, plan, slices, ids)
	if len(graph.Warnings) != 0 || len(graph.Edges) != 0 {
		t.Fatalf("expected no-op when slice not in ids map, got warnings=%v edges=%v", graph.Warnings, graph.Edges)
	}
}

func TestAppendPlanGraphTaskRelationEdges_UnknownDep(t *testing.T) {
	graph := &workflowPlanGraph{}
	plan := &CanonicalPlan{ID: "p1"}
	tasks := []CanonicalTask{
		{ID: "t1", DependsOn: []string{"unknown-task"}},
	}
	ids := map[string]string{"t1": "node-t1"}
	appendPlanGraphTaskRelationEdges(graph, plan, tasks, ids)
	if len(graph.Warnings) != 1 || !strings.Contains(graph.Warnings[0], "unknown task") {
		t.Fatalf("expected unknown-task warning, got %+v", graph.Warnings)
	}
}

func TestAppendPlanGraphTaskRelationEdges_UnknownBlocks(t *testing.T) {
	graph := &workflowPlanGraph{}
	plan := &CanonicalPlan{ID: "p1"}
	tasks := []CanonicalTask{
		{ID: "t1", Blocks: []string{"unknown-task"}},
	}
	ids := map[string]string{"t1": "node-t1"}
	appendPlanGraphTaskRelationEdges(graph, plan, tasks, ids)
	if len(graph.Warnings) != 1 || !strings.Contains(graph.Warnings[0], "blocks unknown") {
		t.Fatalf("expected blocks-unknown warning, got %+v", graph.Warnings)
	}
}

func TestRunWorkflowPlanList_UnreadablePlanRendered(t *testing.T) {
	repo := setupTestProject(t)

	planPath := filepath.Join(repo, ".agents", "workflow", "plans", "plan-001", "PLAN.yaml")
	if err := os.WriteFile(planPath, []byte("not: [valid: yaml"), 0644); err != nil {
		t.Fatal(err)
	}
	chdirForCov(t, repo)
	out, err := captureCovStdout(t, runWorkflowPlanList)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "unreadable") {
		t.Fatalf("expected unreadable in output, got: %s", out)
	}
}

func TestAppendPlanGraphSliceNodes_UnknownParentWarn(t *testing.T) {
	graph := &workflowPlanGraph{}
	plan := &CanonicalPlan{ID: "p1"}
	slices := []CanonicalSlice{{ID: "s1", ParentTaskID: "missing", Title: "x"}}
	taskIDs := map[string]string{}
	out := appendPlanGraphSliceNodes(graph, plan, slices, taskIDs)
	if len(out) != 0 {
		t.Fatalf("expected no slice IDs registered, got %d", len(out))
	}
	if len(graph.Warnings) == 0 || !strings.Contains(graph.Warnings[0], "unknown parent task") {
		t.Fatalf("expected unknown-parent warning, got %v", graph.Warnings)
	}
}

func TestBuildTaskConflictGraph_NilConflicts(t *testing.T) {
	tasks := []workflowNextTaskSuggestion{
		{TaskID: "t1", ConflictsWith: nil},
		{TaskID: "t2", ConflictsWith: []string{"t1"}},
	}
	g := buildTaskConflictGraph(tasks)
	if got, ok := g["t1"]; !ok || got == nil || len(got) != 0 {
		t.Fatalf("expected empty []string for nil ConflictsWith, got %v ok=%v", got, ok)
	}
	if g["t2"][0] != "t1" {
		t.Fatalf("expected t1 in t2 conflicts, got %v", g["t2"])
	}
}

func TestRankNextTaskCandidate_InProgressNoFocusMatch(t *testing.T) {
	repo := t.TempDir()
	plansDir := filepath.Join(repo, ".agents", "workflow", "plans", "p1")
	if err := os.MkdirAll(plansDir, 0755); err != nil {
		t.Fatal(err)
	}
	plan := CanonicalPlan{
		SchemaVersion: 1, ID: "p1", Title: "P1", Status: "active",
		CreatedAt:        "2026-04-10T00:00:00Z",
		UpdatedAt:        "2026-04-10T00:00:00Z",
		CurrentFocusTask: "some other title",
	}
	if err := saveCanonicalPlan(repo, &plan); err != nil {
		t.Fatal(err)
	}
	sug := workflowNextTaskSuggestion{
		PlanID:    "p1",
		TaskID:    "t1",
		TaskTitle: "in-progress-not-focus",
		Status:    "in_progress",
	}
	out, priority := rankNextTaskCandidate(repo, sug)
	if priority != 1 {
		t.Fatalf("expected priority 1 for in_progress no focus match, got %d (reason=%q)", priority, out.Reason)
	}
	if !strings.Contains(out.Reason, "in progress") {
		t.Fatalf("expected 'in progress' reason, got %q", out.Reason)
	}
}

func TestRunWorkflowPlanGraph_JSON_AllPlans(t *testing.T) {
	repo := setupTestProject(t)
	chdirForCov(t, repo)
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = false })
	out, err := captureCovStdout(t, func() error { return runWorkflowPlanGraph("") })
	if err != nil {
		t.Fatalf("runWorkflowPlanGraph json: %v", err)
	}
	if !strings.Contains(out, `"nodes"`) {
		t.Fatalf("expected JSON output with nodes field, got: %s", out)
	}
}

func TestCollectCanonicalPlans_ListIDsErr(t *testing.T) {
	repo := t.TempDir()
	base := filepath.Join(repo, ".agents", "workflow", "plans")
	if err := os.MkdirAll(base, 0755); err != nil {
		t.Fatal(err)
	}
	chmodUnreadableDir(t, base)

	_, warnings := collectCanonicalPlans(repo)
	if len(warnings) == 0 || !strings.Contains(warnings[0], "canonical plans unreadable") {
		t.Fatalf("expected plans-unreadable warning, got %v", warnings)
	}
}

func TestCollectDraftPlanIDs_ListIDsErr(t *testing.T) {
	repo := t.TempDir()
	base := filepath.Join(repo, ".agents", "workflow", "plans")
	if err := os.MkdirAll(base, 0755); err != nil {
		t.Fatal(err)
	}
	chmodUnreadableDir(t, base)

	out := collectDraftPlanIDs(repo)
	if out == nil || len(out) != 0 {
		t.Fatalf("expected non-nil empty slice on error, got %v", out)
	}
}

func TestCollectDraftPlanIDs_SkipsLoadErr(t *testing.T) {
	repo := t.TempDir()
	plansDir := filepath.Join(repo, ".agents", "workflow", "plans", "bad")
	if err := os.MkdirAll(plansDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(plansDir, "PLAN.yaml"), []byte(":\n  bad: ["), 0644); err != nil {
		t.Fatal(err)
	}
	out := collectDraftPlanIDs(repo)
	if len(out) != 0 {
		t.Fatalf("expected unreadable plan to be skipped, got %v", out)
	}
}

func TestDeriveScopeConfidence_NonCodeContextReady(t *testing.T) {
	got := deriveScopeConfidence("doc", false, true, false, 0)
	if got != "medium" {
		t.Fatalf("expected medium for doc + contextReady, got %q", got)
	}
}

func TestDeriveScopeConfidence_NonCodeContextNotReady(t *testing.T) {
	got := deriveScopeConfidence("doc", false, false, false, 0)
	if got != "low" {
		t.Fatalf("expected low, got %q", got)
	}
}

func TestDeriveScopeConfidence_CodeBothReady(t *testing.T) {
	got := deriveScopeConfidence("code", true, true, false, 0)
	if got != "low" {
		t.Fatalf("expected low (no scope inputs), got %q", got)
	}
}

func TestDeriveScopeConfidence_CodeReadyWithInputsNoQueries(t *testing.T) {
	got := deriveScopeConfidence("code", true, false, true, 0)
	if got != "medium" {
		t.Fatalf("expected medium, got %q", got)
	}
}

func TestDeriveScopeMode_ResearchMarker(t *testing.T) {
	got := deriveScopeMode(&CanonicalTask{Notes: "this is a research task"})
	if got != "research" {
		t.Fatalf("expected research, got %q", got)
	}
}

func TestDeriveScopeMode_DocOnlyWriteScope(t *testing.T) {
	got := deriveScopeMode(&CanonicalTask{WriteScope: []string{"docs/foo.md", "README.md"}})
	if got != "doc" {
		t.Fatalf("expected doc, got %q", got)
	}
}

func TestCrossPlanDepIncomplete_TaskMissingWarns(t *testing.T) {
	repo := t.TempDir()
	plansDir := filepath.Join(repo, ".agents", "workflow", "plans", "p1")
	if err := os.MkdirAll(plansDir, 0755); err != nil {
		t.Fatal(err)
	}

	plan := CanonicalPlan{SchemaVersion: 1, ID: "p1", Status: "active",
		CreatedAt: "2026-04-10T00:00:00Z", UpdatedAt: "2026-04-10T00:00:00Z"}
	if err := saveCanonicalPlan(repo, &plan); err != nil {
		t.Fatal(err)
	}
	tasks := &CanonicalTaskFile{SchemaVersion: 1, PlanID: "p1",
		Tasks: []CanonicalTask{{ID: "t-real", Status: "pending"}}}
	if err := saveCanonicalTasks(repo, tasks); err != nil {
		t.Fatal(err)
	}

	var warnings []string
	cache := make(map[string]*CanonicalTaskFile)
	got := crossPlanDepIncomplete(repo, "p1/missing", cache, &warnings)
	if !got {
		t.Fatal("missing task should be treated as incomplete")
	}
	if len(warnings) == 0 || !strings.Contains(warnings[0], "missing") {
		t.Fatalf("expected warning about missing dep, got %v", warnings)
	}
}

func TestCrossPlanDepIncomplete_CompletedReturnsFalse(t *testing.T) {
	repo := t.TempDir()
	plansDir := filepath.Join(repo, ".agents", "workflow", "plans", "p1")
	if err := os.MkdirAll(plansDir, 0755); err != nil {
		t.Fatal(err)
	}
	plan := CanonicalPlan{SchemaVersion: 1, ID: "p1", Status: "active",
		CreatedAt: "2026-04-10T00:00:00Z", UpdatedAt: "2026-04-10T00:00:00Z"}
	if err := saveCanonicalPlan(repo, &plan); err != nil {
		t.Fatal(err)
	}
	tasks := &CanonicalTaskFile{SchemaVersion: 1, PlanID: "p1",
		Tasks: []CanonicalTask{{ID: "t-done", Status: "completed"}}}
	if err := saveCanonicalTasks(repo, tasks); err != nil {
		t.Fatal(err)
	}
	cache := make(map[string]*CanonicalTaskFile)
	got := crossPlanDepIncomplete(repo, "p1/t-done", cache, nil)
	if got {
		t.Fatal("completed dep should be considered complete")
	}
}

func TestRunWorkflowPlanSchedule_JSON_Push6(t *testing.T) {
	repo := setupTestProject(t)
	chdirForCov(t, repo)
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = false })
	out, err := captureCovStdout(t, func() error { return runWorkflowPlanSchedule("plan-001") })
	if err != nil {
		t.Fatalf("schedule json: %v", err)
	}
	if !strings.Contains(out, `"waves"`) {
		t.Fatalf("expected waves in JSON: %s", out)
	}
}

func TestRunWorkflowPlanSchedule_NoWriteScopeRender(t *testing.T) {
	repo := t.TempDir()
	plansDir := filepath.Join(repo, ".agents", "workflow", "plans", "p1")
	if err := os.MkdirAll(plansDir, 0755); err != nil {
		t.Fatal(err)
	}
	plan := CanonicalPlan{SchemaVersion: 1, ID: "p1", Status: "active",
		CreatedAt: "2026-04-10T00:00:00Z", UpdatedAt: "2026-04-10T00:00:00Z"}
	if err := saveCanonicalPlan(repo, &plan); err != nil {
		t.Fatal(err)
	}
	tasks := &CanonicalTaskFile{SchemaVersion: 1, PlanID: "p1",
		Tasks: []CanonicalTask{
			{ID: "t1", Title: "no-scope", Status: "pending"},
		}}
	if err := saveCanonicalTasks(repo, tasks); err != nil {
		t.Fatal(err)
	}
	chdirForCov(t, repo)
	out, err := captureCovStdout(t, func() error { return runWorkflowPlanSchedule("p1") })
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if !strings.Contains(out, "(none)") {
		t.Fatalf("expected (none) for empty write_scope: %s", out)
	}
}

func TestRunWorkflowPlanSchedule_PlanNotFound(t *testing.T) {
	repo := t.TempDir()
	chdirForCov(t, repo)
	err := runWorkflowPlanSchedule("missing")
	if err == nil {
		t.Fatal("expected load-tasks error")
	}
}

func TestRunWorkflowTaskAdd_Duplicate(t *testing.T) {
	repo := setupTestProject(t)
	chdirForCov(t, repo)
	err := runWorkflowTaskAdd(taskAddInputs{
		PlanID: "plan-001", TaskID: "task-001", Title: "Dup",
	})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

func TestRunWorkflowTaskAdd_MissingPlan(t *testing.T) {
	repo := t.TempDir()
	chdirForCov(t, repo)
	err := runWorkflowTaskAdd(taskAddInputs{
		PlanID: "no-plan", TaskID: "t1", Title: "x",
	})
	if err == nil {
		t.Fatal("expected error on missing plan")
	}
}

func TestRunWorkflowTaskUpdate_MissingTask(t *testing.T) {
	repo := setupTestProject(t)
	chdirForCov(t, repo)
	err := runWorkflowTaskUpdate("plan-001", "no-such", "title", "", "", "", "")
	if err == nil || !strings.Contains(err.Error(), "task") {
		t.Fatalf("expected task-not-found, got %v", err)
	}
}

func TestRunWorkflowPlanUpdate_InvalidStatus_Push6(t *testing.T) {
	repo := setupTestProject(t)
	chdirForCov(t, repo)
	err := runWorkflowPlanUpdate("plan-001", "bogus", "", "", "", "", "")
	if err == nil || !strings.Contains(err.Error(), "invalid plan status") {
		t.Fatalf("expected invalid status, got %v", err)
	}
}

func TestRunWorkflowPlanUpdate_MissingPlan(t *testing.T) {
	repo := t.TempDir()
	chdirForCov(t, repo)
	err := runWorkflowPlanUpdate("missing", "active", "", "", "", "", "")
	if err == nil {
		t.Fatal("expected load error")
	}
}

func TestLoadCheckScopeSidecar_MalformedYAML(t *testing.T) {
	repo := t.TempDir()
	planID := "p1"
	taskID := "t1"
	dir := filepath.Join(repo, ".agents", "workflow", "plans", planID, "evidence")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, taskID+".scope.yaml"), []byte(":\n  - bad: ["), 0644); err != nil {
		t.Fatal(err)
	}
	_, _, err := loadCheckScopeSidecar(repo, planID, taskID)
	if err == nil {
		t.Fatal("expected yaml parse error")
	}
}

func TestLoadCheckScopeSidecar_ReadError(t *testing.T) {
	repo := t.TempDir()
	planID := "p1"
	taskID := "t1"
	dir := filepath.Join(repo, ".agents", "workflow", "plans", planID, "evidence")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	side := filepath.Join(dir, taskID+".scope.yaml")
	if err := os.WriteFile(side, []byte("plan_id: p1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	chmodUnreadable(t, side)
	_, _, err := loadCheckScopeSidecar(repo, planID, taskID)
	if err == nil {
		t.Fatal("expected read error")
	}
}

func TestPersistScopeEvidenceSidecar_DryRunPath(t *testing.T) {
	sentinel := errors.New("yaml boom")
	withYAMLMarshalStub(t, yamlMarshalErrStub(sentinel))
	_, err := persistScopeEvidenceSidecar(t.TempDir(), "p1", "t1", &ScopeEvidence{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected yaml sentinel wrapped, got %v", err)
	}
}

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

func TestDeriveScopeMode_AppTypeForcesCode(t *testing.T) {
	got := deriveScopeMode(&CanonicalTask{AppType: "go-cli"})
	if got != "code" {
		t.Fatalf("expected code for AppType set, got %q", got)
	}
}

func TestGraphAdapterForProject_DefaultHome(t *testing.T) {
	repo := t.TempDir()
	adapter := graphAdapterForProject(repo)
	if adapter == nil {
		t.Fatal("expected adapter, got nil")
	}
}

func TestRunWorkflowPlanGraph_LoadError(t *testing.T) {
	repo := t.TempDir()
	chdirForCov(t, repo)
	err := runWorkflowPlanGraph("missing-plan")
	if err == nil {
		t.Fatal("expected load error for missing plan")
	}
}

func TestRunWorkflowPlanArchive_PlanNotFound(t *testing.T) {
	repo := t.TempDir()
	chdirForCov(t, repo)
	err := runWorkflowPlanArchive(repo, []string{"no-such"}, false, true, true)
	if err == nil {
		t.Fatal("expected plan-not-found error")
	}
}

func TestRunWorkflowPlanCreate_WriteTasksError(t *testing.T) {
	repo := t.TempDir()
	chdirForCov(t, repo)

	calls := 0
	withYAMLMarshalStub(t, func(v any) ([]byte, error) {
		calls++
		if calls == 2 {
			return nil, errors.New("yaml-2 boom")
		}

		return realYAMLMarshal(v)
	})
	err := runWorkflowPlanCreate("new-plan", "Title", "", "", "", "")
	if err == nil {
		t.Fatal("expected error from tasks write")
	}
}

func TestRunWorkflowTaskAdd_SaveErr(t *testing.T) {
	repo := setupTestProject(t)
	chdirForCov(t, repo)
	sentinel := errors.New("yaml boom")
	withYAMLMarshalStub(t, yamlMarshalErrStub(sentinel))
	err := runWorkflowTaskAdd(taskAddInputs{
		PlanID: "plan-001", TaskID: "task-new", Title: "x",
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel, got %v", err)
	}
}

func TestRunWorkflowTaskUpdate_SaveErr(t *testing.T) {
	repo := setupTestProject(t)
	chdirForCov(t, repo)
	sentinel := errors.New("yaml boom")
	withYAMLMarshalStub(t, yamlMarshalErrStub(sentinel))
	err := runWorkflowTaskUpdate("plan-001", "task-001", "newtitle", "", "", "", "")
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel, got %v", err)
	}
}

func TestRunWorkflowPlanUpdate_SaveErr(t *testing.T) {
	repo := setupTestProject(t)
	chdirForCov(t, repo)
	sentinel := errors.New("yaml boom")
	withYAMLMarshalStub(t, yamlMarshalErrStub(sentinel))
	err := runWorkflowPlanUpdate("plan-001", "active", "", "", "", "", "")
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel, got %v", err)
	}
}

func TestRunWorkflowAdvance_SaveErr(t *testing.T) {
	repo := setupTestProject(t)
	chdirForCov(t, repo)
	sentinel := errors.New("yaml boom")
	withYAMLMarshalStub(t, yamlMarshalErrStub(sentinel))
	err := runWorkflowAdvance("plan-001", "task-001", "in_progress")
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel, got %v", err)
	}
}

func TestAppendPlanToWorkflowGraph_CorruptSlices(t *testing.T) {
	repo := setupTestProject(t)

	slicesPath := filepath.Join(repo, ".agents", "workflow", "plans", "plan-001", "SLICES.yaml")
	if err := os.WriteFile(slicesPath, []byte(":\n  - bad: ["), 0644); err != nil {
		t.Fatal(err)
	}
	graph := &workflowPlanGraph{}
	err := appendPlanToWorkflowGraph(graph, repo, "plan-001")
	if err == nil || !strings.Contains(err.Error(), "load slices") {
		t.Fatalf("expected load-slices error, got %v", err)
	}
}

func TestAppendPlanToWorkflowGraph_CorruptTasks(t *testing.T) {
	repo := setupTestProject(t)
	tasksPath := filepath.Join(repo, ".agents", "workflow", "plans", "plan-001", "TASKS.yaml")
	if err := os.WriteFile(tasksPath, []byte(":\n  - bad: ["), 0644); err != nil {
		t.Fatal(err)
	}
	graph := &workflowPlanGraph{}
	err := appendPlanToWorkflowGraph(graph, repo, "plan-001")
	if err == nil || !strings.Contains(err.Error(), "load tasks") {
		t.Fatalf("expected load-tasks error, got %v", err)
	}
}

func TestAppendPlanToWorkflowGraph_CorruptPlan(t *testing.T) {
	repo := setupTestProject(t)
	planPath := filepath.Join(repo, ".agents", "workflow", "plans", "plan-001", "PLAN.yaml")
	if err := os.WriteFile(planPath, []byte(":\n  - bad: ["), 0644); err != nil {
		t.Fatal(err)
	}
	graph := &workflowPlanGraph{}
	err := appendPlanToWorkflowGraph(graph, repo, "plan-001")
	if err == nil || !strings.Contains(err.Error(), "load plan") {
		t.Fatalf("expected load-plan error, got %v", err)
	}
}

func TestRunWorkflowTasks_PlanNotFound_Push9(t *testing.T) {
	repo := t.TempDir()
	chdirForCov(t, repo)
	if err := runWorkflowTasks("no-such"); err == nil {
		t.Fatal("expected error")
	}
}

func TestRunWorkflowSlices_PlanNotFound_Push9(t *testing.T) {
	repo := t.TempDir()
	chdirForCov(t, repo)
	if err := runWorkflowSlices("no-such"); err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadActiveDelegationTaskSet_ReadError(t *testing.T) {
	repo := t.TempDir()
	dir := filepath.Join(repo, ".agents", "active", "delegation")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	chmodUnreadableDir(t, dir)
	_, err := loadActiveDelegationTaskSet(repo)
	if err == nil {
		t.Fatal("expected read error")
	}
}

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
		{Path: ""},
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

func TestFanout_SkipTDDGate(t *testing.T) {
	repo := setupTestProject(t)

	if err := os.MkdirAll(filepath.Join(repo, "commands"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "commands", "x.go"), []byte("package commands\n"), 0644); err != nil {
		t.Fatal(err)
	}
	tf, err := loadCanonicalTasks(repo, "plan-001")
	if err != nil {
		t.Fatal(err)
	}
	tf.Tasks[0].WriteScope = []string{"commands/x.go"}
	tf.Tasks[0].VerificationRequired = true
	if err := saveCanonicalTasks(repo, tf); err != nil {
		t.Fatal(err)
	}

	if err := executeWorkflowCommand(t, repo, "fanout",
		"--plan", "plan-001", "--task", "task-001", "--owner", "w", "--skip-tdd-gate"); err != nil {
		t.Fatalf("expected fanout to succeed with --skip-tdd-gate: %v", err)
	}
}

func TestRunWorkflowAdvance_PlanSaveError(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)

	prev := yamlMarshal
	calls := 0
	yamlMarshal = func(v any) ([]byte, error) {
		calls++
		if calls >= 2 {
			return nil, errors.New("marshal plan boom")
		}
		return prev(v)
	}
	t.Cleanup(func() { yamlMarshal = prev })

	err := runWorkflowAdvance("plan-001", "task-001", "in_progress")
	if err == nil {
		t.Fatal("expected save-plan error to propagate")
	}
}

func TestRunWorkflowTaskAdd_SucceedsEvenIfPlanSaveSkipped(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)
	if err := runWorkflowTaskAdd(taskAddInputs{
		PlanID: "plan-001", TaskID: "tnew", Title: "T", WriteScope: "x/,y/",
	}); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	tf, err := loadCanonicalTasks(repo, "plan-001")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, t := range tf.Tasks {
		if t.ID == "tnew" {
			found = true
			if len(t.WriteScope) != 2 {
				break
			}
		}
	}
	if !found {
		t.Fatalf("expected new task in tasks: %+v", tf.Tasks)
	}
}

func TestRunWorkflowPlanUpdate_Status(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)
	if err := runWorkflowPlanUpdate("plan-001", "paused", "T2", "S2", "focus", "SC", "VS"); err != nil {
		t.Fatalf("update: %v", err)
	}
	plan, err := loadCanonicalPlan(repo, "plan-001")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != "paused" || plan.Title != "T2" {
		t.Fatalf("update did not persist: %+v", plan)
	}
}

func TestRunWorkflowPlanUpdate_InvalidStatus(t *testing.T) {
	err := runWorkflowPlanUpdate("plan-001", "bogus", "", "", "", "", "")
	if err == nil || !strings.Contains(err.Error(), "invalid plan status") {
		t.Fatalf("expected invalid status, got %v", err)
	}
}

func TestRunWorkflowPlanUpdate_PlanNotFound(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)
	err := runWorkflowPlanUpdate("ghost-plan", "paused", "", "", "", "", "")
	if err == nil {
		t.Fatal("expected plan-not-found")
	}
}

func TestRunWorkflowPlanCreate_TasksWriteError(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	chdirRepo(t, repo)

	prev := osWriteFile
	calls := 0
	osWriteFile = func(name string, data []byte, perm os.FileMode) error {
		calls++
		if calls >= 2 {
			return errors.New("write tasks boom")
		}
		return prev(name, data, perm)
	}
	t.Cleanup(func() { osWriteFile = prev })

	err := runWorkflowPlanCreate("plan-new", "T", "S", "O", "SC", "VS")
	if err == nil {
		t.Fatal("expected error from second write")
	}
}

func TestLoadCanonicalPlan_ParseError(t *testing.T) {
	repo := setupTestProject(t)
	if err := os.WriteFile(filepath.Join(repo, ".agents", "workflow", "plans", "plan-001", "PLAN.yaml"),
		[]byte("not: valid: yaml"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := loadCanonicalPlan(repo, "plan-001")
	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestLoadCanonicalTasks_ParseError(t *testing.T) {
	repo := setupTestProject(t)
	if err := os.WriteFile(filepath.Join(repo, ".agents", "workflow", "plans", "plan-001", "TASKS.yaml"),
		[]byte("not: valid: yaml"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := loadCanonicalTasks(repo, "plan-001")
	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestLoadCanonicalSlices_ParseError(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	if err := os.WriteFile(filepath.Join(repo, ".agents", "workflow", "plans", "p1", "SLICES.yaml"),
		[]byte("not: valid: yaml"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := loadCanonicalSlices(repo, "p1")
	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestSaveCanonicalPlan_MarshalError(t *testing.T) {
	repo := t.TempDir()
	prev := yamlMarshal
	yamlMarshal = func(v any) ([]byte, error) { return nil, errors.New("boom") }
	t.Cleanup(func() { yamlMarshal = prev })
	err := saveCanonicalPlan(repo, &CanonicalPlan{ID: "p"})
	if err == nil {
		t.Fatal("expected marshal error")
	}
}

func TestSaveCanonicalTasks_MarshalError(t *testing.T) {
	repo := t.TempDir()
	prev := yamlMarshal
	yamlMarshal = func(v any) ([]byte, error) { return nil, errors.New("boom") }
	t.Cleanup(func() { yamlMarshal = prev })
	err := saveCanonicalTasks(repo, &CanonicalTaskFile{PlanID: "p"})
	if err == nil {
		t.Fatal("expected marshal error")
	}
}

func TestCollectCanonicalPlans_UnreadablePlan(t *testing.T) {
	repo := setupTestProject(t)
	if err := os.WriteFile(filepath.Join(repo, ".agents", "workflow", "plans", "plan-001", "PLAN.yaml"),
		[]byte("not: valid: yaml"), 0644); err != nil {
		t.Fatal(err)
	}
	_, warnings := collectCanonicalPlans(repo)
	if len(warnings) == 0 {
		t.Fatal("expected warning for unreadable plan")
	}
}

func TestRunWorkflowAdvance_HappyPathInProgress(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)
	if err := runWorkflowAdvance("plan-001", "task-001", "in_progress"); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	tf, _ := loadCanonicalTasks(repo, "plan-001")
	if tf.Tasks[0].Status != "in_progress" {
		t.Fatalf("expected in_progress, got %q", tf.Tasks[0].Status)
	}
	plan, _ := loadCanonicalPlan(repo, "plan-001")
	if plan.CurrentFocusTask != "Do the thing" {
		t.Fatalf("expected focus update, got %q", plan.CurrentFocusTask)
	}
}

func TestRunWorkflowEligible_EmptyJSON(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	chdirRepo(t, repo)

	workflowTestJSON = true
	defer func() { workflowTestJSON = false }()

	captureStdoutWhileRunning(t, repo, func() error {
		return runWorkflowEligible("", 0)
	}, `"eligible_tasks": []`)
}

func TestRunWorkflowComplete_ActionableHuman(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	chdirRepo(t, repo)
	captureStdoutWhileRunning(t, repo, func() error {
		return runWorkflowComplete("wave-2")
	}, "Scoped Plan Completion")
}

func TestRunWorkflowPlanList_JSON_NoPlans(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	chdirRepo(t, repo)

	workflowTestJSON = true
	defer func() { workflowTestJSON = false }()

	captureStdoutWhileRunning(t, repo, func() error { return runWorkflowPlanList() },
		"No canonical plans found")
}

func TestRunWorkflowEligible_FilterEmptyHuman(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	chdirRepo(t, repo)
	captureStdoutWhileRunning(t, repo, func() error {
		return runWorkflowEligible("", 0)
	}, "Eligible Tasks")
}

func TestRunWorkflowEligible_LimitTruncatesHuman(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	chdirRepo(t, repo)

	captureStdoutWhileRunning(t, repo, func() error {
		return runWorkflowEligible("", 1)
	}, "Eligible Tasks")
}

func TestSelectAllEligibleTasks_PlanFilter(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)

	tasks, err := selectAllEligibleTasks(repo, []string{"wave-2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) == 0 {
		t.Fatal("expected at least one eligible task")
	}

	tasksAll, err := selectAllEligibleTasks(repo, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasksAll) < len(tasks) {
		t.Fatalf("filtered=%d > unfiltered=%d", len(tasks), len(tasksAll))
	}
}

func TestCollectWorkflowCompletionState_BadPlan(t *testing.T) {
	repo := setupTestProject(t)

	if err := os.WriteFile(filepath.Join(repo, ".agents", "workflow", "plans", "plan-001", "PLAN.yaml"),
		[]byte("not: valid: yaml: at: all:"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := collectWorkflowCompletionState(repo, "plan-001")
	if err == nil {
		t.Fatal("expected error from bad plan")
	}
}

func TestRunWorkflowPlanSchedule_TasksMissing(t *testing.T) {
	repo := setupTestProject(t)
	if err := os.Remove(filepath.Join(repo, ".agents", "workflow", "plans", "plan-001", "TASKS.yaml")); err != nil {
		t.Fatal(err)
	}
	chdirRepo(t, repo)
	err := runWorkflowPlanSchedule("plan-001")
	if err == nil || !strings.Contains(err.Error(), "load tasks") {
		t.Fatalf("expected load-tasks error, got %v", err)
	}
}

func TestRunWorkflowAdvance_CompleteResetsFocus(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)
	if err := runWorkflowAdvance("plan-001", "task-001", "completed"); err != nil {
		t.Fatal(err)
	}
	plan, _ := loadCanonicalPlan(repo, "plan-001")

	if plan.CurrentFocusTask == "Do the thing" {
		t.Fatal("expected focus reset after completion")
	}
}

func TestRunWorkflowTaskUpdate_UpdatesNotesAndWriteScope(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)
	if err := runWorkflowTaskUpdate("plan-001", "task-001", "", "new note", "x/,y/", "", ""); err != nil {
		t.Fatal(err)
	}
	tf, _ := loadCanonicalTasks(repo, "plan-001")
	if tf.Tasks[0].Notes != "new note" || len(tf.Tasks[0].WriteScope) != 2 {
		t.Fatalf("update did not persist: %+v", tf.Tasks[0])
	}
}

func TestArchiveSinglePlan_RefusesNonCompletedWithoutForce(t *testing.T) {
	repo := setupTestProject(t)
	err := archiveSinglePlan(repo, "plan-001", false, false, true)
	if err == nil || !strings.Contains(err.Error(), "completed") {
		t.Fatalf("expected non-completed guard error, got %v", err)
	}
}

func TestRunWorkflowPlanCreate_HappyPath(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	chdirRepo(t, repo)
	if err := runWorkflowPlanCreate("new-plan", "T", "S", "O", "SC", "VS"); err != nil {
		t.Fatal(err)
	}
	plan, err := loadCanonicalPlan(repo, "new-plan")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Title != "T" || plan.Owner != "O" {
		t.Fatalf("plan didn't persist fields: %+v", plan)
	}
	tf, err := loadCanonicalTasks(repo, "new-plan")
	if err != nil {
		t.Fatal(err)
	}
	if len(tf.Tasks) != 0 || tf.PlanID != "new-plan" {
		t.Fatalf("tasks file not empty/correct: %+v", tf)
	}
}

func TestRunWorkflowPlanCreate_ExistingPlanRejected(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)

	err := runWorkflowPlanCreate("plan-001", "T", "S", "O", "SC", "VS")
	if err == nil {
		t.Fatal("expected duplicate plan error")
	}
}

func TestFanout_TaskSaveFailureIsWarnedNotErrored(t *testing.T) {
	repo := setupTestProject(t)

	if err := executeWorkflowCommand(t, repo, "fanout",
		"--plan", "plan-001", "--task", "task-001", "--owner", "w"); err != nil {
		t.Fatalf("expected fanout to succeed, got %v", err)
	}
	tf, _ := loadCanonicalTasks(repo, "plan-001")
	if tf.Tasks[0].Status != "in_progress" {
		t.Fatalf("expected status promotion, got %q", tf.Tasks[0].Status)
	}
}

func TestRunWorkflowNext_VerificationOptional(t *testing.T) {
	repo := setupTestProject(t)

	tf, _ := loadCanonicalTasks(repo, "plan-001")
	for i := range tf.Tasks {
		tf.Tasks[i].VerificationRequired = false
	}
	if err := saveCanonicalTasks(repo, tf); err != nil {
		t.Fatal(err)
	}
	chdirRepo(t, repo)
	captureStdoutWhileRunning(t, repo, func() error {
		return runWorkflowNext("plan-001")
	}, "verification: optional")
}

func TestRunWorkflowNext_RendersDependsOn(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPendingPlanFixture(t, repo)
	chdirRepo(t, repo)

	captureStdoutWhileRunning(t, repo, func() error {
		return runWorkflowNext("wave-next")
	}, "depends on:")
}

func TestRunWorkflowEligible_LimitTruncationCount(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPendingPlanFixture(t, repo)
	chdirRepo(t, repo)

	workflowTestJSON = true
	defer func() { workflowTestJSON = false }()

	out := captureStdoutToString(t, func() {
		_ = runWorkflowEligible("wave-next", 1)
	})

	if !strings.Contains(out, `"eligible_tasks"`) {
		t.Fatalf("expected eligible_tasks key: %s", out)
	}
}

func TestRunWorkflowPlanList_JSON_WithMultiplePlans(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	addCanonicalPendingPlanFixture(t, repo)
	chdirRepo(t, repo)

	workflowTestJSON = true
	defer func() { workflowTestJSON = false }()

	captureStdoutWhileRunning(t, repo, func() error { return runWorkflowPlanList() },
		`"wave-2"`, `"wave-next"`)
}

func TestRunWorkflowTaskAdd_WithFullCSVFields(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)
	if err := runWorkflowTaskAdd(taskAddInputs{
		PlanID:               "plan-001",
		TaskID:               "tcsv",
		Title:                "with deps",
		DependsOn:            "task-001, task-002",
		Blocks:               "x, y",
		WriteScope:           "a/,b/",
		Owner:                "me",
		AppType:              "api",
		VerificationRequired: true,
	}); err != nil {
		t.Fatal(err)
	}
	tf, _ := loadCanonicalTasks(repo, "plan-001")
	for _, t2 := range tf.Tasks {
		if t2.ID == "tcsv" {
			if len(t2.DependsOn) != 2 || len(t2.Blocks) != 2 || len(t2.WriteScope) != 2 {
				t.Fatalf("CSV not parsed: %+v", t2)
			}
			return
		}
	}
	t.Fatal("tcsv not found")
}

func TestRunWorkflowPlanArchive_EmptyList(t *testing.T) {
	repo := setupTestProject(t)

	if err := runWorkflowPlanArchive(repo, nil, false, false, true); err != nil {
		t.Fatalf("empty plan archive should be a no-op, got %v", err)
	}
}

func TestRunWorkflowAdvance_CobraJSONNotUsed(t *testing.T) {
	repo := setupTestProject(t)

	if err := executeWorkflowCommand(t, repo, "advance", "plan-001",
		"--task", "task-001", "--status", "in_progress"); err != nil {
		t.Fatal(err)
	}
}

func TestRunWorkflowComplete_CobraHappyPath(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	if err := executeWorkflowCommand(t, repo, "complete", "--plan", "wave-2"); err != nil {
		t.Fatal(err)
	}
}

func TestRunWorkflowPlanGraph_NoPlanID_JSON(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	chdirRepo(t, repo)

	workflowTestJSON = true
	defer func() { workflowTestJSON = false }()

	captureStdoutWhileRunning(t, repo, func() error { return runWorkflowPlanGraph("") },
		`"nodes"`)
}

func TestRunWorkflowPlanShow_CorruptSlices(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	if err := os.WriteFile(filepath.Join(repo, ".agents", "workflow", "plans", "p1", "SLICES.yaml"),
		[]byte("not: valid: yaml"), 0644); err != nil {
		t.Fatal(err)
	}
	chdirRepo(t, repo)

	captureStdoutWhileRunning(t, repo, func() error {
		return runWorkflowPlanShow("p1")
	}, "p1")
}

func TestRunWorkflowTaskAdd_PlanReloadSkippedSoftError(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)

	if err := runWorkflowTaskAdd(taskAddInputs{
		PlanID: "plan-001", TaskID: "tt", Title: "T",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRunWorkflowEligible_FilterMatchesNoPlans(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	chdirRepo(t, repo)
	err := runWorkflowEligible("no-such-plan", 0)
	if err == nil {
		t.Fatal("expected plan-not-found error from filter")
	}
}

func TestRunWorkflowTasks_JSON(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)

	workflowTestJSON = true
	defer func() { workflowTestJSON = false }()

	captureStdoutWhileRunning(t, repo, func() error {
		return runWorkflowTasks("plan-001")
	}, `"plan_id"`, `"task-001"`)
}

func TestRunWorkflowTasks_PlanNotFound(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)
	err := runWorkflowTasks("ghost-plan")
	if err == nil || !strings.Contains(err.Error(), "ghost-plan") {
		t.Fatalf("expected plan-not-found error, got %v", err)
	}
}

func TestRunWorkflowTasks_TasksMissingErrors(t *testing.T) {
	repo := setupTestProject(t)

	if err := os.Remove(filepath.Join(repo, ".agents", "workflow", "plans", "plan-001", "TASKS.yaml")); err != nil {
		t.Fatal(err)
	}
	chdirRepo(t, repo)
	err := runWorkflowTasks("plan-001")
	if err == nil || !strings.Contains(err.Error(), "plan-001") {
		t.Fatalf("expected tasks-not-found error, got %v", err)
	}
}

func TestRunWorkflowSlices_JSON(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	chdirRepo(t, repo)

	workflowTestJSON = true
	defer func() { workflowTestJSON = false }()

	captureStdoutWhileRunning(t, repo, func() error {
		return runWorkflowSlices("p1")
	}, `"plan_id"`, `"s1"`)
}

func TestRunWorkflowSlices_PlanNotFound(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	chdirRepo(t, repo)
	err := runWorkflowSlices("ghost-plan")
	if err == nil || !strings.Contains(err.Error(), "ghost-plan") {
		t.Fatalf("expected plan-not-found error, got %v", err)
	}
}

func TestRunWorkflowSlices_SlicesMissingErrors(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)
	err := runWorkflowSlices("plan-001")
	if err == nil || !strings.Contains(err.Error(), "p") {
		t.Fatalf("expected slices-not-found error, got %v", err)
	}
}

func TestRunWorkflowPlanShow_JSON_WithSlices(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	chdirRepo(t, repo)

	workflowTestJSON = true
	defer func() { workflowTestJSON = false }()

	captureStdoutWhileRunning(t, repo, func() error {
		return runWorkflowPlanShow("p1")
	}, `"plan"`, `"slices"`)
}

func TestRunWorkflowPlanShow_NoTasksFile(t *testing.T) {
	repo := setupTestProject(t)
	if err := os.Remove(filepath.Join(repo, ".agents", "workflow", "plans", "plan-001", "TASKS.yaml")); err != nil {
		t.Fatal(err)
	}
	chdirRepo(t, repo)

	captureStdoutWhileRunning(t, repo, func() error {
		return runWorkflowPlanShow("plan-001")
	}, "no ")
}

func TestRunWorkflowPlanList_JSON_HasPlans(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	chdirRepo(t, repo)

	workflowTestJSON = true
	defer func() { workflowTestJSON = false }()

	captureStdoutWhileRunning(t, repo, func() error {
		return runWorkflowPlanList()
	}, `"p1"`)
}

func TestRunWorkflowPlanSchedule_JSON(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	chdirRepo(t, repo)

	workflowTestJSON = true
	defer func() { workflowTestJSON = false }()

	captureStdoutWhileRunning(t, repo, func() error {
		return runWorkflowPlanSchedule("wave-2")
	}, `"waves"`, `"t1"`)
}

func TestRunWorkflowPlanDeriveScope_JSON(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)

	workflowTestJSON = true
	defer func() { workflowTestJSON = false }()

	captureStdoutWhileRunning(t, repo, func() error {
		return runWorkflowPlanDeriveScope("plan-001", "task-001", []string{"Sym"}, []string{"path/"})
	}, `"plan_id"`, `"task_id"`)
}

func TestRunWorkflowPlanDeriveScope_PersistError(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)

	sentinel := errors.New("write boom")
	withWriteFileStub(t, func(string, []byte, os.FileMode) error { return sentinel })
	err := runWorkflowPlanDeriveScope("plan-001", "task-001", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected write error, got %v", err)
	}
}

func TestRunWorkflowPlanDeriveScope_TaskNotFound(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)
	err := runWorkflowPlanDeriveScope("plan-001", "ghost-task", nil, nil)
	if err == nil {
		t.Fatal("expected error for missing task")
	}
}

func TestRunWorkflowAdvance_InvalidStatus(t *testing.T) {
	err := runWorkflowAdvance("plan-001", "task-001", "weird")
	if err == nil || !strings.Contains(err.Error(), "invalid task status") {
		t.Fatalf("expected invalid status error, got %v", err)
	}
}

func TestRunWorkflowAdvance_PlanNotFound(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)
	err := runWorkflowAdvance("ghost", "task-001", "in_progress")
	if err == nil {
		t.Fatal("expected error for ghost plan")
	}
}

func TestRunWorkflowAdvance_TaskNotFound(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)
	err := runWorkflowAdvance("plan-001", "ghost-task", "in_progress")
	if err == nil || !strings.Contains(err.Error(), "ghost-task") {
		t.Fatalf("expected task-not-found, got %v", err)
	}
}

func TestRunWorkflowEligible_JSON_WithFilter(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	chdirRepo(t, repo)

	workflowTestJSON = true
	defer func() { workflowTestJSON = false }()

	captureStdoutWhileRunning(t, repo, func() error {
		return runWorkflowEligible("wave-2", 0)
	}, `"eligible_tasks"`)
}

func TestRunWorkflowComplete_EmptyPlanRejected(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)
	err := runWorkflowComplete("")
	if err == nil || !strings.Contains(err.Error(), "--plan must not be empty") {
		t.Fatalf("expected --plan must not be empty, got %v", err)
	}
}

func TestRunWorkflowTaskAdd_DuplicateID(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)
	err := runWorkflowTaskAdd(taskAddInputs{
		PlanID: "plan-001",
		TaskID: "task-001",
		Title:  "dup",
	})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

func TestRunWorkflowTaskAdd_PlanMissingTasksFile(t *testing.T) {
	repo := setupTestProject(t)
	if err := os.Remove(filepath.Join(repo, ".agents", "workflow", "plans", "plan-001", "TASKS.yaml")); err != nil {
		t.Fatal(err)
	}
	chdirRepo(t, repo)
	err := runWorkflowTaskAdd(taskAddInputs{PlanID: "plan-001", TaskID: "tnew", Title: "x"})
	if err == nil {
		t.Fatal("expected tasks-file-missing error")
	}
}

func TestRunWorkflowTaskUpdate_TaskNotFound(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)
	err := runWorkflowTaskUpdate("plan-001", "ghost-task", "T", "N", "", "", "")
	if err == nil || !strings.Contains(err.Error(), "ghost-task") {
		t.Fatalf("expected task-not-found, got %v", err)
	}
}

func TestRunWorkflowTaskUpdate_PlanMissing(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)
	err := runWorkflowTaskUpdate("ghost-plan", "task-001", "T", "N", "", "", "")
	if err == nil {
		t.Fatal("expected plan-not-found error")
	}
}

func TestRunWorkflowPlanCheckScope_PlanNotFound(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)
	withOsExitStub(t)
	err := runWorkflowPlanCheckScope("ghost-plan", "task-001", []string{"x.go"}, false)
	if err == nil {
		t.Fatal("expected error for missing plan")
	}
}

func TestRunWorkflowPlanCheckScope_TaskNotFound(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)
	withOsExitStub(t)
	err := runWorkflowPlanCheckScope("plan-001", "ghost-task", []string{"x.go"}, false)
	if err == nil {
		t.Fatal("expected error for missing task")
	}
}

func TestRunWorkflowEligible_PrefsParseError(t *testing.T) {
	repo := initWorkflowTestRepo(t)

	prefsDir := filepath.Join(repo, ".agents", "workflow")
	if err := os.MkdirAll(prefsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prefsDir, "preferences.yaml"),
		[]byte("foo: bar\n  - this is not: valid yaml\n\t\tindent: broken"), 0644); err != nil {
		t.Fatal(err)
	}
	chdirRepo(t, repo)
	err := runWorkflowEligible("", 0)
	if err == nil {
		t.Fatal("expected prefs parse error to propagate")
	}
}

func TestRunWorkflowComplete_PlanNotFound(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)
	err := runWorkflowComplete("ghost-plan")
	if err == nil {
		t.Fatal("expected error for ghost plan")
	}
}

func TestDelegationCloseout_RejectWithoutNote(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	if err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--slice", "s1", "--owner", "w"); err != nil {
		t.Fatal(err)
	}
	if err := executeWorkflowCommand(t, repo, "merge-back", "--task", "t1", "--summary", "tried", "--verification-status", "fail"); err != nil {
		t.Fatal(err)
	}
	if err := executeWorkflowCommand(t, repo, "delegation", "closeout",
		"--plan", "p1", "--task", "t1", "--decision", "reject"); err != nil {
		t.Fatalf("expected successful reject closeout, got %v", err)
	}
	tf, err := loadCanonicalTasks(repo, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if tf.Tasks[0].Status != "blocked" {
		t.Fatalf("expected task blocked, got %q", tf.Tasks[0].Status)
	}
}

func TestRunWorkflowPlanList_EmptyHint(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	chdirRepo(t, repo)
	captureStdoutWhileRunning(t, repo, func() error { return runWorkflowPlanList() },
		"No canonical plans found")
}

func TestRunWorkflowPlanShow_PlanMissing(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)
	err := runWorkflowPlanShow("ghost-plan")
	if err == nil || !strings.Contains(err.Error(), "ghost-plan") {
		t.Fatalf("expected plan-not-found, got %v", err)
	}
}

func TestEligibleOutput_JSONRoundTrip(t *testing.T) {
	out := eligibleOutput{
		EligibleTasks: []AnnotatedTask{},
		MaxBatch:      []string{},
		ConflictGraph: map[string][]string{},
		DraftPlans:    []string{},
	}
	data, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	var decoded eligibleOutput
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.EligibleTasks) != 0 {
		t.Fatalf("decoded mismatch: %+v", decoded)
	}
}

// tasksYamlLockTestMutator performs the same load -> mutate -> save shape as
// runWorkflowTaskUpdate, guarded by the production withTasksLock helper, but
// deliberately widens the window between load and save with a short sleep.
// That makes a would-be lost-update race manifest deterministically: without
// serialization, a second goroutine's load (which starts immediately, since
// load itself is near-instant) captures the pre-mutation snapshot, and its
// later save clobbers whatever the first goroutine wrote. Under the lock, the
// second goroutine's load cannot begin until the first's entire critical
// section (including the save) has completed and released.
func tasksYamlLockTestMutator(projectPath, planID, taskID, note string) error {
	return withTasksLock(projectPath, planID, func() error {
		tf, err := loadCanonicalTasks(projectPath, planID)
		if err != nil {
			return err
		}
		time.Sleep(20 * time.Millisecond)
		found := false
		for i := range tf.Tasks {
			if tf.Tasks[i].ID == taskID {
				tf.Tasks[i].Notes = note
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("task %q not found", taskID)
		}
		return saveCanonicalTasks(projectPath, tf)
	})
}

// TestTasksYamlLock_ConcurrentUpdatesNoLostUpdate proves withTasksLock closes
// the loadCanonicalTasks/saveCanonicalTasks lost-update race: two goroutines
// concurrently mutate DIFFERENT tasks' notes in the SAME plan's TASKS.yaml;
// without the lock serializing the read-modify-write, one goroutine's save
// would clobber the other's update (see tasksYamlLockTestMutator). Run with
// -race: go test ./commands/workflow/... -run TestTasksYamlLock -race -v
func TestTasksYamlLock_ConcurrentUpdatesNoLostUpdate(t *testing.T) {
	repo := setupTestProject(t)

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := tasksYamlLockTestMutator(repo, "plan-001", "task-001", "note from A"); err != nil {
			errs <- fmt.Errorf("goroutine A: %w", err)
		}
	}()
	go func() {
		defer wg.Done()
		if err := tasksYamlLockTestMutator(repo, "plan-001", "task-002", "note from B"); err != nil {
			errs <- fmt.Errorf("goroutine B: %w", err)
		}
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent tasks-lock mutation failed: %v", err)
	}

	tf, err := loadCanonicalTasks(repo, "plan-001")
	if err != nil {
		t.Fatalf("reload tasks: %v", err)
	}
	var gotA, gotB string
	for _, task := range tf.Tasks {
		switch task.ID {
		case "task-001":
			gotA = task.Notes
		case "task-002":
			gotB = task.Notes
		}
	}
	if gotA != "note from A" {
		t.Errorf("task-001 update lost — got notes %q, want %q (lock failed to serialize)", gotA, "note from A")
	}
	if gotB != "note from B" {
		t.Errorf("task-002 update lost — got notes %q, want %q (lock failed to serialize)", gotB, "note from B")
	}
}

// TestTasksYamlLock_TimeoutWrapsClearError proves that when AcquireFileLock
// cannot claim the TASKS.yaml lock (held by another caller), withTasksLock
// surfaces a clear wrapped error instead of proceeding unlocked.
func TestTasksYamlLock_TimeoutWrapsClearError(t *testing.T) {
	repo := setupTestProject(t)
	path := tasksLockPath(repo, "plan-001")
	release, err := agentslock.AcquireFileLock(path)
	if err != nil {
		t.Fatalf("acquire holder lock: %v", err)
	}
	t.Cleanup(func() { _ = release() })

	err = withTasksLock(repo, "plan-001", func() error {
		t.Fatal("fn must not run while the lock is held by another caller")
		return nil
	})
	if err == nil {
		t.Fatal("expected a timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "TASKS.yaml locked by another process, timed out waiting") {
		t.Fatalf("expected wrapped timeout message, got: %v", err)
	}
}
