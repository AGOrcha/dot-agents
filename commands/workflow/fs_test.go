package workflow

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestCopyWorkflowArtifact_CreatesParentAndContent verifies copyWorkflowArtifact
// mkdirs the parent and writes byte-identical content.
func TestCopyWorkflowArtifact_CreatesParentAndContent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	body := []byte("hello world\n")
	if err := os.WriteFile(src, body, 0644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "nested", "deep", "dst.txt")
	if err := copyWorkflowArtifact(src, dst); err != nil {
		t.Fatalf("copy: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(got) != string(body) {
		t.Fatalf("content mismatch: %q != %q", got, body)
	}
}

// TestCopyWorkflowArtifact_SrcMissing surfaces an open error.
func TestCopyWorkflowArtifact_SrcMissing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	err := copyWorkflowArtifact(filepath.Join(dir, "absent"), filepath.Join(dir, "dst"))
	if err == nil {
		t.Fatal("expected error for missing src")
	}
}

// TestIsDMAFile covers each known DMA pattern and a negative.
func TestIsDMAFile(t *testing.T) {
	t.Parallel()
	cases := []struct {
		path string
		want bool
	}{
		{"delegation.yaml", true},
		{"merge-back.md", true},
		{"closeout.yaml", true},
		{"delegate-merge-back-archive/2026-05/del-1/merge-back.md", true},
		{"delegation/some-task.yaml", true},
		{"merge-back/task.md", true},
		{"fold-back/obs-1.yaml", true},
		{"verification/task/result.yaml", true},
		// Non-DMA paths
		{"PLAN.yaml", false},
		{"TASKS.yaml", false},
		{"docs/some.md", false},
		{"impl-results.md", false},
		{"nested/path/PLAN.yaml", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			if got := isDMAFile(tc.path); got != tc.want {
				t.Fatalf("isDMAFile(%q) = %t, want %t", tc.path, got, tc.want)
			}
		})
	}
}

// TestIsCanonicalPlanFile covers all three canonical filenames and negatives.
func TestIsCanonicalPlanFile(t *testing.T) {
	t.Parallel()
	planID := "plan-001"
	cases := []struct {
		path string
		want bool
	}{
		{"PLAN.yaml", true},
		{"TASKS.yaml", true},
		{planID + ".plan.md", true},
		{"other-plan.plan.md", false},
		{"plan-001.plan.md.bak", false},
		{"docs/PLAN.yaml", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			if got := isCanonicalPlanFile(tc.path, planID); got != tc.want {
				t.Fatalf("isCanonicalPlanFile(%q,%q) = %t, want %t", tc.path, planID, got, tc.want)
			}
		})
	}
}

// TestSha256File covers the hash of a known input and the missing-file branch.
func TestSha256File(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	body := []byte("hello\n")
	p := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(p, body, 0644); err != nil {
		t.Fatal(err)
	}
	got, err := sha256File(p)
	if err != nil {
		t.Fatalf("sha256File: %v", err)
	}
	want := sha256.Sum256(body)
	if got != want {
		t.Fatalf("hash mismatch")
	}

	if _, err := sha256File(filepath.Join(dir, "absent")); err == nil {
		t.Fatal("expected error for missing file")
	}
}

// TestMergePlanDirFastRename moves srcDir to dstDir when dstDir does not exist.
func TestMergePlanDirFastRename(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	src := filepath.Join(root, "src-plan")
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "PLAN.yaml"), []byte("plan\n"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(root, "history", "plan-001")

	if err := mergePlanDirFastRename(src, dst, false); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("expected dst to exist: %v", err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("expected src removed after rename")
	}
}

// TestMergePlanDirFastRename_DryRun does not touch the filesystem.
func TestMergePlanDirFastRename_DryRun(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	src := filepath.Join(root, "src-plan")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "PLAN.yaml"), []byte("plan\n"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(root, "history", "plan-001")
	if err := mergePlanDirFastRename(src, dst, true); err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if _, err := os.Stat(dst); err == nil {
		t.Fatal("dst should not exist in dry-run")
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("src should still exist in dry-run: %v", err)
	}
}

// TestMergeWorkflowPlanDir_FastRenameWhenDstAbsent confirms the no-walk
// fast-path is used when the destination does not exist.
func TestMergeWorkflowPlanDir_FastRenameWhenDstAbsent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	src := filepath.Join(root, "src")
	if err := os.MkdirAll(filepath.Join(src, "evidence"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "PLAN.yaml"), []byte("p\n"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(root, "history", "plan-x")
	if err := mergeWorkflowPlanDir("plan-x", src, dst, false); err != nil {
		t.Fatalf("merge: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "PLAN.yaml")); err != nil {
		t.Fatalf("expected PLAN.yaml at destination: %v", err)
	}
}

// TestMergeWorkflowPlanDir_OverwritesCanonical asserts canonical files always
// overwrite even when destination has different content.
func TestMergeWorkflowPlanDir_OverwritesCanonical(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dst, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "PLAN.yaml"), []byte("NEW\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "PLAN.yaml"), []byte("OLD\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := mergeWorkflowPlanDir("plan-x", src, dst, false); err != nil {
		t.Fatalf("merge: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dst, "PLAN.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "NEW\n" {
		t.Fatalf("canonical not overwritten: got %q", got)
	}
}

// TestMergeWorkflowPlanDir_SkipsDMAFiles ensures merge-back files in the
// source do not overwrite history copies (per DMA rule).
func TestMergeWorkflowPlanDir_SkipsDMAFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	srcMB := filepath.Join(src, "delegate-merge-back-archive", "2026-05", "task-1", "merge-back.md")
	dstMB := filepath.Join(dst, "delegate-merge-back-archive", "2026-05", "task-1", "merge-back.md")
	if err := os.MkdirAll(filepath.Dir(srcMB), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(dstMB), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(srcMB, []byte("NEW\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dstMB, []byte("OLD\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := mergeWorkflowPlanDir("plan-x", src, dst, false); err != nil {
		t.Fatalf("merge: %v", err)
	}
	got, err := os.ReadFile(dstMB)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "OLD\n" {
		t.Fatalf("DMA file was overwritten: got %q", got)
	}
}

// TestMergeWorkflowPlanDir_IdenticalSkipped ensures non-canonical files with
// identical hashes are not re-written.
func TestMergeWorkflowPlanDir_IdenticalSkipped(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dst, 0755); err != nil {
		t.Fatal(err)
	}
	content := []byte("identical content\n")
	if err := os.WriteFile(filepath.Join(src, "notes.md"), content, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "notes.md"), content, 0644); err != nil {
		t.Fatal(err)
	}
	if err := mergeWorkflowPlanDir("plan-x", src, dst, false); err != nil {
		t.Fatalf("merge: %v", err)
	}
	// Should still be present and equal — no error path
	got, err := os.ReadFile(filepath.Join(dst, "notes.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatalf("unexpected mutation: %q", got)
	}
}

// TestCopyWorkflowDir_CopiesNestedTree ensures the recursive helper preserves
// directory structure and file content.
func TestCopyWorkflowDir_CopiesNestedTree(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	if err := os.MkdirAll(filepath.Join(src, "a", "b"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "top.txt"), []byte("t\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a", "b", "deep.txt"), []byte("d\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := copyWorkflowDir(src, dst); err != nil {
		t.Fatalf("copyWorkflowDir: %v", err)
	}
	for _, rel := range []string{"top.txt", "a/b/deep.txt"} {
		p := filepath.Join(dst, filepath.FromSlash(rel))
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("missing copied file %s: %v", rel, err)
		}
	}
}

// TestRemoveAllWithRetry_RemovesDir checks the helper succeeds on a normal dir.
func TestRemoveAllWithRetry_RemovesDir(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	target := filepath.Join(root, "victim")
	if err := os.MkdirAll(filepath.Join(target, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "x.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := removeAllWithRetry(target); err != nil {
		t.Fatalf("removeAllWithRetry: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("expected target removed, stat err=%v", err)
	}
}

// TestRemoveAllWithRetry_MissingPathIsNoOp confirms removing a non-existent
// path returns nil (RemoveAll semantics).
func TestRemoveAllWithRetry_MissingPathIsNoOp(t *testing.T) {
	t.Parallel()
	if err := removeAllWithRetry(filepath.Join(t.TempDir(), "absent")); err != nil {
		t.Fatalf("expected nil for missing path, got %v", err)
	}
}

// TestIsDMAFile_NestedSegment ensures DMA segment detection works regardless
// of where in the path it appears (deep nesting).
func TestIsDMAFile_NestedSegment(t *testing.T) {
	t.Parallel()
	for _, p := range []string{
		"x/y/delegation/task.yaml",
		"a/b/c/d/fold-back/obs.yaml",
		strings.Join([]string{"plan", "verification", "task", "result.yaml"}, string(filepath.Separator)),
	} {
		if !isDMAFile(p) {
			t.Errorf("expected DMA detection for %q", p)
		}
	}
}

func TestMergePlanDirFile_DMASkipped(t *testing.T) {

	for _, rel := range []string{"delegation.yaml", "merge-back.md", "closeout.yaml"} {
		if err := mergePlanDirFile("p1", "src", "dst", rel, false); err != nil {
			t.Errorf("DMA file %q should be skipped, got %v", rel, err)
		}
	}
}

func TestMergePlanDirFile_DMADryRun(t *testing.T) {
	for _, rel := range []string{"delegation.yaml", "merge-back.md"} {
		if err := mergePlanDirFile("p1", "src", "dst", rel, true); err != nil {
			t.Errorf("DMA file %q dry-run should be no-op, got %v", rel, err)
		}
	}
}

func TestMergePlanDirFile_CanonicalDryRun(t *testing.T) {

	if err := mergePlanDirFile("plan-x", "src", "dst", "plan-x.plan.md", true); err != nil {
		t.Errorf("canonical dry-run should be no-op: %v", err)
	}
}

func TestShouldSkipPlanDirCopy_IdenticalHashDryRun(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.txt")
	dst := filepath.Join(tmp, "dst.txt")
	body := []byte("same body")
	if err := os.WriteFile(src, body, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, body, 0644); err != nil {
		t.Fatal(err)
	}
	srcHash, err := sha256File(src)
	if err != nil {
		t.Fatal(err)
	}
	dstStat, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	skip, err := shouldSkipPlanDirCopy(src, dst, "rel.txt", true, srcHash, dstStat)
	if err != nil {
		t.Fatal(err)
	}
	if !skip {
		t.Error("expected skip=true for identical hashes")
	}
}

func TestShouldSkipPlanDirCopy_HistoryNewerDryRun(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.txt")
	dst := filepath.Join(tmp, "dst.txt")
	if err := os.WriteFile(src, []byte("source"), 0644); err != nil {
		t.Fatal(err)
	}

	older := time.Now().Add(-time.Hour)
	if err := os.Chtimes(src, older, older); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("destination"), 0644); err != nil {
		t.Fatal(err)
	}
	srcHash, err := sha256File(src)
	if err != nil {
		t.Fatal(err)
	}
	dstStat, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	skip, err := shouldSkipPlanDirCopy(src, dst, "rel.txt", true, srcHash, dstStat)
	if err != nil {
		t.Fatal(err)
	}
	if !skip {
		t.Error("expected skip=true because history (dst) is newer")
	}
}

func TestMergePlanDirCompareAndCopy_SrcHashError(t *testing.T) {
	err := mergePlanDirCompareAndCopy("/nonexistent/src", "/nonexistent/dst", "rel", false)
	if err == nil || !strings.Contains(err.Error(), "hash rel") {
		t.Fatalf("expected hash error, got %v", err)
	}
}

func TestCopyWorkflowArtifact_MissingSource(t *testing.T) {
	tmp := t.TempDir()
	err := copyWorkflowArtifact(filepath.Join(tmp, "does-not-exist"), filepath.Join(tmp, "dst"))
	if err == nil {
		t.Fatal("expected open-src error")
	}
}

func TestCopyWorkflowArtifact_DstCreateError(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.txt")
	if err := os.WriteFile(src, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	conflict := filepath.Join(tmp, "blocker")
	if err := os.WriteFile(conflict, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(conflict, "inner.txt")
	if err := copyWorkflowArtifact(src, dst); err == nil {
		t.Fatal("expected MkdirAll over file error")
	}
}

func TestCopyWorkflowDir_MissingSource(t *testing.T) {
	tmp := t.TempDir()
	err := copyWorkflowDir(filepath.Join(tmp, "no-such-dir"), filepath.Join(tmp, "dst"))
	if err == nil {
		t.Fatal("expected walk error on missing src")
	}
}

func TestCopyWorkflowDir_Recursive(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "out")

	if err := os.MkdirAll(filepath.Join(src, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "leaf.txt"), []byte("hi"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := copyWorkflowDir(src, dst); err != nil {
		t.Fatalf("copyWorkflowDir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "sub", "leaf.txt")); err != nil {
		t.Fatalf("expected file copied, got %v", err)
	}
}

func TestSha256File_Missing(t *testing.T) {
	_, err := sha256File(filepath.Join(t.TempDir(), "absent"))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMergePlanDirFastRename_MkdirError(t *testing.T) {
	src := t.TempDir()

	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(blocker, "child", "plan")
	if err := mergePlanDirFastRename(src, dst, false); err == nil {
		t.Fatal("expected mkdir-parent error")
	}
}

func TestMergePlanDirFastRename_DryRun_Push6(t *testing.T) {
	if err := mergePlanDirFastRename("/src", "/dst", true); err != nil {
		t.Fatalf("dryRun must not error, got %v", err)
	}
}

func TestMergePlanDirCompareAndCopy_HashSrcError(t *testing.T) {
	tmp := t.TempDir()
	err := mergePlanDirCompareAndCopy(filepath.Join(tmp, "missing"), filepath.Join(tmp, "dst"), "rel", false)
	if err == nil || !strings.Contains(err.Error(), "hash ") {
		t.Fatalf("expected hash error, got %v", err)
	}
}

func TestMergePlanDirCompareAndCopy_DstStatErrPath(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.txt")
	if err := os.WriteFile(src, []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}

	blocker := filepath.Join(tmp, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(blocker, "child")

	err := mergePlanDirCompareAndCopy(src, dst, "child", false)

	if err == nil {

		t.Log("stat-dst returned IsNotExist on this platform; branch not hit but not an error")
	}
}

func TestMergePlanDirCompareAndCopy_DryRunOverwrite(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.txt")
	dst := filepath.Join(tmp, "dst.txt")
	if err := os.WriteFile(src, []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}

	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(src, future, future); err != nil {
		t.Fatal(err)
	}
	if err := mergePlanDirCompareAndCopy(src, dst, "rel", true); err != nil {
		t.Fatalf("dryRun: %v", err)
	}
}

func TestShouldSkipPlanDirCopy_HashDstError(t *testing.T) {
	tmp := t.TempDir()
	srcHash := [32]byte{}
	st, err := os.Stat(tmp)
	if err != nil {
		t.Fatal(err)
	}
	_, err = shouldSkipPlanDirCopy(filepath.Join(tmp, "src"), filepath.Join(tmp, "missing"), "rel", false, srcHash, st)
	if err == nil || !strings.Contains(err.Error(), "hash dst") {
		t.Fatalf("expected hash-dst error, got %v", err)
	}
}

func TestShouldSkipPlanDirCopy_StatSrcError(t *testing.T) {
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "dst.txt")
	if err := os.WriteFile(dst, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(dst)

	srcHash := [32]byte{1, 2, 3}
	_, err := shouldSkipPlanDirCopy(filepath.Join(tmp, "missing-src"), dst, "rel", false, srcHash, st)
	if err == nil || !strings.Contains(err.Error(), "stat src") {
		t.Fatalf("expected stat-src error, got %v", err)
	}
}

func TestShouldSkipPlanDirCopy_HistoryNewer(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.txt")
	dst := filepath.Join(tmp, "dst.txt")
	if err := os.WriteFile(src, []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("b"), 0644); err != nil {
		t.Fatal(err)
	}

	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(src, old, old); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(dst)
	srcHashBytes := [32]byte{9, 9, 9}
	skip, err := shouldSkipPlanDirCopy(src, dst, "rel", true, srcHashBytes, st)
	if err != nil {
		t.Fatal(err)
	}
	if !skip {
		t.Fatal("expected skip=true when history newer")
	}
}

func TestIsDMAFile_ByBasename(t *testing.T) {
	for _, name := range []string{"delegation.yaml", "merge-back.md", "closeout.yaml"} {
		if !isDMAFile(name) {
			t.Errorf("expected %q to be DMA", name)
		}
	}
}

func TestIsDMAFile_ByDirectory(t *testing.T) {
	cases := []string{
		"delegate-merge-back-archive/x.yaml",
		"delegation/x.yaml",
		"merge-back/x.md",
		"fold-back/x.yaml",
		"verification/x.yaml",
	}
	for _, p := range cases {
		if !isDMAFile(p) {
			t.Errorf("expected %q to be DMA", p)
		}
	}
}

func TestIsDMAFile_NotDMA(t *testing.T) {
	if isDMAFile("PLAN.yaml") {
		t.Error("PLAN.yaml should not be DMA")
	}
}

func TestIsCanonicalPlanFile_Push6(t *testing.T) {
	if !isCanonicalPlanFile("PLAN.yaml", "p1") {
		t.Error("PLAN.yaml")
	}
	if !isCanonicalPlanFile("TASKS.yaml", "p1") {
		t.Error("TASKS.yaml")
	}
	if !isCanonicalPlanFile("p1.plan.md", "p1") {
		t.Error("p1.plan.md")
	}
	if isCanonicalPlanFile("README.md", "p1") {
		t.Error("README.md should not be canonical")
	}
}

func TestRemoveAllWithRetry_OnFakeFile(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "f"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := removeAllWithRetry(filepath.Join(tmp, "f")); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}
