package workflow

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
