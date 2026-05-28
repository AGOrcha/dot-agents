package gitwt

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
)

// fixture is a temp git repo with one commit on the default branch, plus a
// gitwt Manager bound to it. worktreeRoot is a sibling dir for linked worktrees.
type fixture struct {
	repoPath     string
	repo         *git.Repository
	mgr          Manager
	worktreeRoot string
	base         plumbing.Hash
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	repoPath := filepath.Join(root, "repo")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	repo, err := git.PlainInit(repoPath, false)
	if err != nil {
		t.Fatalf("init repo: %v", err)
	}
	base := commitFile(t, repo, "README.md", "hello\n", "initial")

	mgr, err := NewManager(repoPath)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return &fixture{
		repoPath:     repoPath,
		repo:         repo,
		mgr:          mgr,
		worktreeRoot: filepath.Join(root, "worktrees"),
		base:         base,
	}
}

// commitFile writes a file in the main worktree, stages it, and commits.
func commitFile(t *testing.T, repo *git.Repository, name, content, msg string) plumbing.Hash {
	t.Helper()
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	abs := filepath.Join(wt.Filesystem().Root(), name)
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	if _, err := wt.Add(name); err != nil {
		t.Fatalf("add %s: %v", name, err)
	}
	hash, err := wt.Commit(msg, &git.CommitOptions{
		Author: &object.Signature{Name: "Test", Email: "t@example.com", When: time.Now()},
	})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	return hash
}

func (f *fixture) wtPath(name string) string {
	return filepath.Join(f.worktreeRoot, name)
}

func TestSafeName(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"slash branch", "feature/foo-bar"},
		{"dotted", "release.1.2"},
		{"underscore", "my_task_id"},
		{"plain", "already-safe"},
		{"empty", ""},
	}
	seen := map[string]string{}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SafeName(tc.input)
			if !worktreeNameRE.MatchString(got) {
				t.Fatalf("SafeName(%q)=%q does not match charset", tc.input, got)
			}
			if again := SafeName(tc.input); again != got {
				t.Fatalf("SafeName not deterministic: %q vs %q", got, again)
			}
			if prev, ok := seen[got]; ok {
				t.Fatalf("collision: %q and %q both -> %q", prev, tc.input, got)
			}
			seen[got] = tc.input
		})
	}
}

func TestValidateName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid alnum dash", "wt-abc123", false},
		{"slash rejected", "a/b", true},
		{"dot rejected", "a.b", true},
		{"underscore rejected", "a_b", true},
		{"empty rejected", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateName(tc.input)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidName) {
					t.Fatalf("want ErrInvalidName, got %v", err)
				}
			} else if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
		})
	}
}

func TestNewManagerErrors(t *testing.T) {
	if _, err := NewManager(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("want error opening non-repo path")
	}
}

func TestAddBranch(t *testing.T) {
	f := newFixture(t)
	path := f.wtPath("feature-x")

	if err := f.mgr.AddBranch("feature-x", path, f.base); err != nil {
		t.Fatalf("AddBranch: %v", err)
	}

	// Working tree checked out at path.
	if _, err := os.Stat(filepath.Join(path, "README.md")); err != nil {
		t.Fatalf("worktree file missing: %v", err)
	}

	// The new branch ref exists in the shared store and points at base.
	ref, err := f.repo.Reference(plumbing.NewBranchReferenceName("feature-x"), false)
	if err != nil {
		t.Fatalf("branch ref not created: %v", err)
	}
	if ref.Hash() != f.base {
		t.Fatalf("branch at %s, want base %s", ref.Hash(), f.base)
	}

	// Worktree HEAD is on the branch (not detached).
	wt, err := f.mgr.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	branch, err := wt.Branch()
	if err != nil {
		t.Fatalf("Branch: %v", err)
	}
	if branch != "feature-x" {
		t.Fatalf("branch=%q, want feature-x", branch)
	}

	// Base ref recorded automatically by AddBranch.
	got, err := f.mgr.BaseRef("feature-x")
	if err != nil {
		t.Fatalf("BaseRef: %v", err)
	}
	if got != f.base {
		t.Fatalf("BaseRef=%s, want %s", got, f.base)
	}
}

func TestAddBranchErrors(t *testing.T) {
	f := newFixture(t)

	t.Run("invalid name", func(t *testing.T) {
		err := f.mgr.AddBranch("bad/name", f.wtPath("bad"), f.base)
		if !errors.Is(err, ErrInvalidName) {
			t.Fatalf("want ErrInvalidName, got %v", err)
		}
	})

	t.Run("worktree exists", func(t *testing.T) {
		if err := f.mgr.AddDetached("dup", f.wtPath("dup"), f.base); err != nil {
			t.Fatalf("first add: %v", err)
		}
		// Re-add the same worktree name (admin metadata still present) -> the
		// go-git ErrWorktreeAlreadyExists path mapped to ErrWorktreeExists.
		err := f.mgr.AddDetached("dup", f.wtPath("dup"), f.base)
		if !errors.Is(err, ErrWorktreeExists) {
			t.Fatalf("want ErrWorktreeExists, got %v", err)
		}
	})

	t.Run("branch exists", func(t *testing.T) {
		// Create branch "preexist" directly, then AddBranch must reject it.
		if err := f.repo.Storer.SetReference(plumbing.NewHashReference(
			plumbing.NewBranchReferenceName("preexist"), f.base)); err != nil {
			t.Fatalf("set ref: %v", err)
		}
		err := f.mgr.AddBranch("preexist", f.wtPath("preexist"), f.base)
		if !errors.Is(err, ErrBranchExists) {
			t.Fatalf("want ErrBranchExists, got %v", err)
		}
	})
}

func TestAddDetached(t *testing.T) {
	f := newFixture(t)
	path := f.wtPath("ephemeral")

	if err := f.mgr.AddDetached("ephemeral", path, f.base); err != nil {
		t.Fatalf("AddDetached: %v", err)
	}
	wt, err := f.mgr.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	branch, err := wt.Branch()
	if err != nil {
		t.Fatalf("Branch: %v", err)
	}
	if branch != "" {
		t.Fatalf("detached HEAD should yield empty branch, got %q", branch)
	}
	head, err := wt.Head()
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if head.Hash() != f.base {
		t.Fatalf("HEAD=%s, want %s", head.Hash(), f.base)
	}
	// No branch ref named "ephemeral" should have been created.
	if _, err := f.repo.Reference(plumbing.NewBranchReferenceName("ephemeral"), false); err == nil {
		t.Fatal("detached add must not create a branch ref")
	}

	if err := f.mgr.AddDetached("bad/name", f.wtPath("x"), f.base); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("want ErrInvalidName, got %v", err)
	}
}

func TestList(t *testing.T) {
	f := newFixture(t)
	got, err := f.mgr.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want empty list, got %v", got)
	}

	for _, n := range []string{"alpha", "beta"} {
		if err := f.mgr.AddBranch(n, f.wtPath(n), f.base); err != nil {
			t.Fatalf("AddBranch %s: %v", n, err)
		}
	}
	got, err = f.mgr.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 worktrees, got %v", got)
	}
}

func TestRemove(t *testing.T) {
	f := newFixture(t)
	path := f.wtPath("gone")
	if err := f.mgr.AddBranch("gone", path, f.base); err != nil {
		t.Fatalf("AddBranch: %v", err)
	}

	if err := f.mgr.Remove("gone", path); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("worktree dir should be gone, stat err=%v", err)
	}
	names, _ := f.mgr.List()
	if len(names) != 0 {
		t.Fatalf("worktree metadata should be gone, got %v", names)
	}

	t.Run("not found", func(t *testing.T) {
		if err := f.mgr.Remove("missing", ""); !errors.Is(err, ErrWorktreeNotFound) {
			t.Fatalf("want ErrWorktreeNotFound, got %v", err)
		}
	})
	t.Run("invalid name", func(t *testing.T) {
		if err := f.mgr.Remove("bad/name", ""); !errors.Is(err, ErrInvalidName) {
			t.Fatalf("want ErrInvalidName, got %v", err)
		}
	})
	t.Run("metadata only", func(t *testing.T) {
		p := f.wtPath("meta")
		if err := f.mgr.AddBranch("meta", p, f.base); err != nil {
			t.Fatalf("AddBranch: %v", err)
		}
		if err := f.mgr.Remove("meta", ""); err != nil {
			t.Fatalf("Remove metadata-only: %v", err)
		}
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("metadata-only remove must keep dir: %v", err)
		}
	})
}

func TestPrune(t *testing.T) {
	f := newFixture(t)
	// keep stays; stale's working dir is deleted out-of-band.
	for _, n := range []string{"keep", "stale"} {
		if err := f.mgr.AddBranch(n, f.wtPath(n), f.base); err != nil {
			t.Fatalf("AddBranch %s: %v", n, err)
		}
	}
	if err := os.RemoveAll(f.wtPath("stale")); err != nil {
		t.Fatalf("rm stale dir: %v", err)
	}

	pruned, err := f.mgr.Prune()
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(pruned) != 1 || pruned[0] != "stale" {
		t.Fatalf("want [stale] pruned, got %v", pruned)
	}
	names, _ := f.mgr.List()
	if len(names) != 1 || names[0] != "keep" {
		t.Fatalf("want [keep] remaining, got %v", names)
	}

	// Idempotent: a second prune removes nothing.
	pruned, err = f.mgr.Prune()
	if err != nil {
		t.Fatalf("second Prune: %v", err)
	}
	if len(pruned) != 0 {
		t.Fatalf("second prune should be no-op, got %v", pruned)
	}
}

func TestBaseRefRecordRead(t *testing.T) {
	f := newFixture(t)
	if err := f.mgr.AddBranch("br", f.wtPath("br"), f.base); err != nil {
		t.Fatalf("AddBranch: %v", err)
	}

	// Overwrite with a different hash and read it back.
	other := commitFile(t, f.repo, "second.txt", "x\n", "second")
	if err := f.mgr.RecordBaseRef("br", other); err != nil {
		t.Fatalf("RecordBaseRef: %v", err)
	}
	got, err := f.mgr.BaseRef("br")
	if err != nil {
		t.Fatalf("BaseRef: %v", err)
	}
	if got != other {
		t.Fatalf("BaseRef=%s, want %s", got, other)
	}

	t.Run("unknown worktree", func(t *testing.T) {
		if _, err := f.mgr.BaseRef("nope"); !errors.Is(err, ErrWorktreeNotFound) {
			t.Fatalf("want ErrWorktreeNotFound, got %v", err)
		}
		if err := f.mgr.RecordBaseRef("nope", f.base); !errors.Is(err, ErrWorktreeNotFound) {
			t.Fatalf("RecordBaseRef want ErrWorktreeNotFound, got %v", err)
		}
	})

	t.Run("not recorded", func(t *testing.T) {
		// AddDetached records a base too, so use a worktree then strip the file.
		if err := f.mgr.AddBranch("nobase", f.wtPath("nobase"), f.base); err != nil {
			t.Fatalf("AddBranch: %v", err)
		}
		dir, ok := f.mgr.(*manager).adminDir("nobase")
		if !ok {
			t.Fatal("adminDir not found")
		}
		if err := os.Remove(filepath.Join(dir, baseRefFile)); err != nil {
			t.Fatalf("rm base-ref: %v", err)
		}
		if _, err := f.mgr.BaseRef("nobase"); !errors.Is(err, ErrBaseRefNotRecorded) {
			t.Fatalf("want ErrBaseRefNotRecorded, got %v", err)
		}
	})
}

func TestOpenError(t *testing.T) {
	f := newFixture(t)
	if _, err := f.mgr.Open(filepath.Join(t.TempDir(), "not-a-worktree")); err == nil {
		t.Fatal("want error opening non-worktree path")
	}
}

// TestIndexIsolation is the load-bearing workflow-commit-command guarantee:
// a commit in one worktree's isolated index must not touch another worktree or
// the main repo.
func TestIndexIsolation(t *testing.T) {
	f := newFixture(t)
	pathA := f.wtPath("iso-a")
	pathB := f.wtPath("iso-b")
	if err := f.mgr.AddBranch("iso-a", pathA, f.base); err != nil {
		t.Fatalf("AddBranch a: %v", err)
	}
	if err := f.mgr.AddBranch("iso-b", pathB, f.base); err != nil {
		t.Fatalf("AddBranch b: %v", err)
	}

	wtA, err := f.mgr.Open(pathA)
	if err != nil {
		t.Fatalf("Open a: %v", err)
	}
	wtB, err := f.mgr.Open(pathB)
	if err != nil {
		t.Fatalf("Open b: %v", err)
	}

	// Write + stage + commit a file only in worktree A.
	if err := os.WriteFile(filepath.Join(pathA, "a-only.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatalf("write a-only: %v", err)
	}
	if err := wtA.Stage("a-only.txt"); err != nil {
		t.Fatalf("Stage a: %v", err)
	}
	hashA, err := wtA.Commit("a commit", &CommitOptions{AuthorName: "A", AuthorEmail: "a@x"})
	if err != nil {
		t.Fatalf("Commit a: %v", err)
	}

	// A's branch advanced past base; B's branch did not.
	branchA, _ := f.repo.Reference(plumbing.NewBranchReferenceName("iso-a"), false)
	if branchA.Hash() != hashA || hashA == f.base {
		t.Fatalf("iso-a should advance to %s (got %s, base %s)", hashA, branchA.Hash(), f.base)
	}
	branchB, _ := f.repo.Reference(plumbing.NewBranchReferenceName("iso-b"), false)
	if branchB.Hash() != f.base {
		t.Fatalf("iso-b must stay at base %s, got %s", f.base, branchB.Hash())
	}

	// B's index/working tree never saw a-only.txt.
	if _, err := os.Stat(filepath.Join(pathB, "a-only.txt")); !os.IsNotExist(err) {
		t.Fatalf("a-only.txt must not exist in worktree B, stat err=%v", err)
	}
	statusB, err := wtB.Status()
	if err != nil {
		t.Fatalf("Status b: %v", err)
	}
	if !statusB.IsClean() {
		t.Fatalf("worktree B should be clean, got %v", statusB)
	}

	// Main repo branch is untouched and clean.
	mainWT, err := f.repo.Worktree()
	if err != nil {
		t.Fatalf("main worktree: %v", err)
	}
	mainStatus, err := mainWT.Status()
	if err != nil {
		t.Fatalf("main status: %v", err)
	}
	if _, ok := mainStatus["a-only.txt"]; ok {
		t.Fatal("a-only.txt leaked into main repo status")
	}
}

func TestStatusAndCommitFlow(t *testing.T) {
	f := newFixture(t)
	path := f.wtPath("flow")
	if err := f.mgr.AddBranch("flow", path, f.base); err != nil {
		t.Fatalf("AddBranch: %v", err)
	}
	wt, err := f.mgr.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Clean to start.
	st, err := wt.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !st.IsClean() {
		t.Fatalf("fresh worktree not clean: %v", st)
	}

	if err := os.WriteFile(filepath.Join(path, "f.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	st, _ = wt.Status()
	if st.IsClean() {
		t.Fatal("status should be dirty after write")
	}

	if err := wt.Stage("f.txt"); err != nil {
		t.Fatalf("Stage: %v", err)
	}
	hash, err := wt.Commit("add f", nil)
	if err != nil {
		t.Fatalf("Commit (nil opts): %v", err)
	}
	if hash.IsZero() {
		t.Fatal("commit returned zero hash")
	}
	head, _ := wt.Head()
	if head.Hash() != hash {
		t.Fatalf("HEAD=%s, want commit %s", head.Hash(), hash)
	}

	t.Run("commit options - all and empty", func(t *testing.T) {
		// AllowEmpty path with explicit author.
		h2, err := wt.Commit("empty", &CommitOptions{
			AuthorName: "X", AuthorEmail: "x@y", AllowEmpty: true,
		})
		if err != nil {
			t.Fatalf("empty commit: %v", err)
		}
		if h2.IsZero() {
			t.Fatal("empty commit returned zero hash")
		}
	})

	t.Run("commit All stages tracked modification", func(t *testing.T) {
		if err := os.WriteFile(filepath.Join(path, "f.txt"), []byte("v2\n"), 0o644); err != nil {
			t.Fatalf("modify: %v", err)
		}
		if _, err := wt.Commit("modify f", &CommitOptions{All: true}); err != nil {
			t.Fatalf("commit -a: %v", err)
		}
		st, _ := wt.Status()
		if !st.IsClean() {
			t.Fatalf("after commit -a worktree should be clean: %v", st)
		}
	})

	t.Run("stage missing path errors", func(t *testing.T) {
		if err := wt.Stage("does-not-exist.txt"); err == nil {
			t.Fatal("staging a missing path should error")
		}
	})
}
