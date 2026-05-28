package gitwt

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/storage/memory"
)

// dotgitWorktreesDir returns the repo's .git/worktrees path.
func dotgitWorktreesDir(repoPath string) string {
	return filepath.Join(repoPath, ".git", "worktrees")
}

func TestAddBranch_WorktreeExistsAfterBranchCheck(t *testing.T) {
	f := newFixture(t)
	// AddDetached creates worktree "shared" but NO branch "shared".
	if err := f.mgr.AddDetached("shared", f.wtPath("shared"), f.base); err != nil {
		t.Fatalf("AddDetached: %v", err)
	}
	// AddBranch("shared") passes the branch-exists check (no such branch) then
	// fails inside add() because the worktree admin already exists.
	err := f.mgr.AddBranch("shared", f.wtPath("shared"), f.base)
	if !errors.Is(err, ErrWorktreeExists) {
		t.Fatalf("want ErrWorktreeExists, got %v", err)
	}
}

func TestAdd_MkdirParentFails(t *testing.T) {
	f := newFixture(t)
	// Make the intended parent dir a regular file so MkdirAll fails.
	blocker := filepath.Join(f.worktreeRoot, "blocker")
	if err := os.MkdirAll(f.worktreeRoot, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	// Target path is blocker/child, so MkdirAll(filepath.Dir) hits the file.
	err := f.mgr.AddDetached("mk", filepath.Join(blocker, "child"), f.base)
	if err == nil {
		t.Fatal("want mkdir error")
	}
}

func TestAdd_CheckoutFailsBadCommit(t *testing.T) {
	f := newFixture(t)
	// A commit hash that does not exist makes go-git's Checkout fail inside Add.
	bogus := plumbing.NewHash("deadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	err := f.mgr.AddDetached("bogus", f.wtPath("bogus"), bogus)
	if err == nil {
		t.Fatal("want add/checkout error for bogus commit")
	}
	if errors.Is(err, ErrWorktreeExists) {
		t.Fatalf("unexpected typed error: %v", err)
	}
}

func TestList_CorruptWorktreesEntry(t *testing.T) {
	f := newFixture(t)
	// Replace .git/worktrees with a regular file so ReadDir fails.
	wtDir := dotgitWorktreesDir(f.repoPath)
	if err := os.MkdirAll(filepath.Dir(wtDir), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if err := os.WriteFile(wtDir, []byte("not a dir"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if _, err := f.mgr.List(); err == nil {
		t.Fatal("want List error when .git/worktrees is a file")
	}
	// Prune surfaces the same List error.
	if _, err := f.mgr.Prune(); err == nil {
		t.Fatal("want Prune error propagated from List")
	}
}

func TestPrune_WorktreeMissingGitdir(t *testing.T) {
	f := newFixture(t)
	if err := f.mgr.AddBranch("nogitdir", f.wtPath("nogitdir"), f.base); err != nil {
		t.Fatalf("AddBranch: %v", err)
	}
	// Remove the admin gitdir pointer so worktreeDir() returns ok=false and
	// Prune skips the entry (continue branch).
	dir, ok := f.mgr.(*manager).adminDir("nogitdir")
	if !ok {
		t.Fatal("adminDir missing")
	}
	if err := os.Remove(filepath.Join(dir, "gitdir")); err != nil {
		t.Fatalf("rm gitdir: %v", err)
	}
	pruned, err := f.mgr.Prune()
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	for _, p := range pruned {
		if p == "nogitdir" {
			t.Fatal("entry with missing gitdir should be skipped, not pruned")
		}
	}
}

func TestOpen_BadGitFile(t *testing.T) {
	tests := []struct {
		name        string
		gitContent  string
		makeGitFile bool
	}{
		{"nonexistent gitdir", "gitdir: /nonexistent/path/.git\n", true},
		{"garbage no gitdir prefix", "garbage\n", true}, // would silently open MAIN repo
		{"missing .git entirely", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			bad := f.wtPath("badgit")
			if err := os.MkdirAll(bad, 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			if tc.makeGitFile {
				if err := os.WriteFile(filepath.Join(bad, ".git"), []byte(tc.gitContent), 0o644); err != nil {
					t.Fatalf("write .git: %v", err)
				}
			}
			wt, err := f.mgr.Open(bad)
			if err == nil {
				t.Fatalf("want Open error, got worktree %v (silent fallback to main repo?)", wt)
			}
		})
	}

	t.Run("main worktree (.git is a dir)", func(t *testing.T) {
		f := newFixture(t)
		// The main repo's .git is a directory, not a linked-worktree pointer.
		if _, err := f.mgr.Open(f.repoPath); err == nil {
			t.Fatal("want Open error: main worktree is not a linked worktree")
		}
	})

	t.Run("gitdir points outside a worktree admin dir", func(t *testing.T) {
		f := newFixture(t)
		bad := f.wtPath("noadmin")
		if err := os.MkdirAll(bad, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		// gitdir points at an existing dir that lacks the commondir marker.
		target := filepath.Join(f.worktreeRoot, "plain")
		if err := os.MkdirAll(target, 0o755); err != nil {
			t.Fatalf("mkdir target: %v", err)
		}
		if err := os.WriteFile(filepath.Join(bad, ".git"), []byte("gitdir: "+target+"\n"), 0o644); err != nil {
			t.Fatalf("write .git: %v", err)
		}
		if _, err := f.mgr.Open(bad); err == nil {
			t.Fatal("want Open error: admin dir lacks commondir marker")
		}
	})
}

func TestWorktreeDir_NoGitdir(t *testing.T) {
	f := newFixture(t)
	if err := f.mgr.AddBranch("wd", f.wtPath("wd"), f.base); err != nil {
		t.Fatalf("AddBranch: %v", err)
	}
	m := f.mgr.(*manager)
	// Unknown name -> adminDir not ok.
	if _, ok := m.worktreeDir("does-not-exist"); ok {
		t.Fatal("unknown worktree should not resolve a dir")
	}
	// Existing admin but gitdir removed -> ReadFile error path.
	dir, _ := m.adminDir("wd")
	if err := os.Remove(filepath.Join(dir, "gitdir")); err != nil {
		t.Fatalf("rm gitdir: %v", err)
	}
	if _, ok := m.worktreeDir("wd"); ok {
		t.Fatal("missing gitdir should yield ok=false")
	}
}

func TestRecordBaseRef_WriteFails(t *testing.T) {
	f := newFixture(t)
	if err := f.mgr.AddBranch("ro", f.wtPath("ro"), f.base); err != nil {
		t.Fatalf("AddBranch: %v", err)
	}
	dir, _ := f.mgr.(*manager).adminDir("ro")
	// Replace the base-ref file with a directory so WriteFile to that path fails.
	bf := filepath.Join(dir, baseRefFile)
	if err := os.Remove(bf); err != nil {
		t.Fatalf("rm base-ref: %v", err)
	}
	if err := os.Mkdir(bf, 0o755); err != nil {
		t.Fatalf("mkdir base-ref: %v", err)
	}
	if err := f.mgr.RecordBaseRef("ro", f.base); err == nil {
		t.Fatal("want RecordBaseRef write error when base-ref path is a dir")
	}
}

func TestNewManagerFromRepo_NonWorktreeStorer(t *testing.T) {
	// A repository backed by an in-memory storer does not implement
	// WorktreeStorer, so gogitwt.New errors and newManagerFromRepo wraps it.
	repo, err := git.Init(memory.NewStorage(), nil)
	if err != nil {
		t.Fatalf("init mem repo: %v", err)
	}
	if _, err := newManagerFromRepo(repo); err == nil {
		t.Fatal("want error: in-memory storer is not a WorktreeStorer")
	}
}

func TestBaseRef_ReadFails(t *testing.T) {
	f := newFixture(t)
	if err := f.mgr.AddBranch("rderr", f.wtPath("rderr"), f.base); err != nil {
		t.Fatalf("AddBranch: %v", err)
	}
	dir, _ := f.mgr.(*manager).adminDir("rderr")
	// Replace the base-ref file with a directory so ReadFile fails with a
	// non-NotExist error.
	bf := filepath.Join(dir, baseRefFile)
	if err := os.Remove(bf); err != nil {
		t.Fatalf("rm base-ref: %v", err)
	}
	if err := os.Mkdir(bf, 0o755); err != nil {
		t.Fatalf("mkdir base-ref: %v", err)
	}
	_, err := f.mgr.BaseRef("rderr")
	if err == nil {
		t.Fatal("want BaseRef read error")
	}
	if errors.Is(err, ErrBaseRefNotRecorded) {
		t.Fatalf("a directory should be a read error, not not-recorded: %v", err)
	}
}

// TestWorktreeOps_BrokenHandle drives Status/Head/Branch error wrappers by
// corrupting the worktree's HEAD after opening the handle.
func TestWorktreeOps_BrokenHandle(t *testing.T) {
	f := newFixture(t)
	path := f.wtPath("broken")
	if err := f.mgr.AddBranch("broken", path, f.base); err != nil {
		t.Fatalf("AddBranch: %v", err)
	}
	// Corrupt the worktree's HEAD pointer (admin dir) to a ref that resolves to
	// nothing, then open a FRESH handle so the corruption is read on demand.
	dir, _ := f.mgr.(*manager).adminDir("broken")
	headFile := filepath.Join(dir, "HEAD")
	if err := os.WriteFile(headFile, []byte("ref: refs/heads/does-not-exist\n"), 0o644); err != nil {
		t.Fatalf("corrupt HEAD: %v", err)
	}
	wt, err := f.mgr.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := wt.Head(); err == nil {
		t.Fatal("want Head error on dangling HEAD")
	}
	if _, err := wt.Branch(); err == nil {
		t.Fatal("want Branch error on dangling HEAD")
	}
}

func TestCommit_EmptyErrors(t *testing.T) {
	f := newFixture(t)
	path := f.wtPath("empty")
	if err := f.mgr.AddBranch("empty", path, f.base); err != nil {
		t.Fatalf("AddBranch: %v", err)
	}
	wt, err := f.mgr.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// No changes + AllowEmpty=false -> go-git ErrEmptyCommit, wrapped by Commit.
	if _, err := wt.Commit("nothing to do", nil); err == nil {
		t.Fatal("want Commit error for empty commit")
	}
}

func TestStatus_CorruptIndex(t *testing.T) {
	f := newFixture(t)
	path := f.wtPath("statfail")
	if err := f.mgr.AddBranch("statfail", path, f.base); err != nil {
		t.Fatalf("AddBranch: %v", err)
	}
	wt, err := f.mgr.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// Corrupt the worktree's per-worktree index so Status fails to decode it.
	dir, _ := f.mgr.(*manager).adminDir("statfail")
	if err := os.WriteFile(filepath.Join(dir, "index"), []byte("\x00\x01\x02garbage"), 0o644); err != nil {
		t.Fatalf("corrupt index: %v", err)
	}
	if _, err := wt.Status(); err == nil {
		t.Fatal("want Status error on corrupt index")
	}
}

func TestRemove_InvalidWorktreeEntry(t *testing.T) {
	f := newFixture(t)
	// Create .git/worktrees/<name> as a FILE (not a dir). go-git's Remove finds
	// it via Lstat but rejects it as "invalid worktree" — a non-NotFound error.
	wtRoot := dotgitWorktreesDir(f.repoPath)
	if err := os.MkdirAll(wtRoot, 0o755); err != nil {
		t.Fatalf("mkdir worktrees: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wtRoot, "afile"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write file entry: %v", err)
	}
	err := f.mgr.Remove("afile", "")
	if err == nil {
		t.Fatal("want error removing a non-dir worktree entry")
	}
	if errors.Is(err, ErrWorktreeNotFound) {
		t.Fatalf("a file entry is invalid, not not-found: %v", err)
	}
}
