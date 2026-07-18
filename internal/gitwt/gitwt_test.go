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

	"github.com/AGOrcha/dot-agents/internal/testutil"
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
	// Set a committer identity in repo config so nil-opts commits do not depend
	// on the CI runner's ambient git config (which is absent on CI).
	cfg, err := repo.Config()
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	cfg.User.Name = "Fixture"
	cfg.User.Email = "fixture@example.com"
	if err := repo.SetConfig(cfg); err != nil {
		t.Fatalf("set config: %v", err)
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
		assertBaseRefUnknownWorktree(t, f)
	})

	t.Run("not recorded", func(t *testing.T) {
		assertBaseRefNotRecorded(t, f)
	})
}

// assertBaseRefUnknownWorktree checks both readers reject an unknown worktree.
func assertBaseRefUnknownWorktree(t *testing.T, f *fixture) {
	if _, err := f.mgr.BaseRef("nope"); !errors.Is(err, ErrWorktreeNotFound) {
		t.Fatalf("want ErrWorktreeNotFound, got %v", err)
	}
	if err := f.mgr.RecordBaseRef("nope", f.base); !errors.Is(err, ErrWorktreeNotFound) {
		t.Fatalf("RecordBaseRef want ErrWorktreeNotFound, got %v", err)
	}
}

// assertBaseRefNotRecorded creates a worktree, strips its base-ref file, and
// checks BaseRef reports it as not recorded.
func assertBaseRefNotRecorded(t *testing.T, f *fixture) {
	if err := f.mgr.AddBranch("nobase", f.wtPath("nobase"), f.base); err != nil {
		t.Fatalf("AddBranch: %v", err)
	}
	dir, err := f.mgr.(*manager).adminDir("nobase")
	if err != nil {
		t.Fatalf("adminDir: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, baseRefFile)); err != nil {
		t.Fatalf("rm base-ref: %v", err)
	}
	if _, err := f.mgr.BaseRef("nobase"); !errors.Is(err, ErrBaseRefNotRecorded) {
		t.Fatalf("want ErrBaseRefNotRecorded, got %v", err)
	}
}

// TestAdminDir_RealStatErrorSurfaces exercises the should-be-LOUD fix: a
// real Lstat failure on .git/worktrees (permission-denied, not "never
// existed") must not be reported as ErrWorktreeNotFound.
func TestAdminDir_RealStatErrorSurfaces(t *testing.T) {
	f := newFixture(t)
	if err := f.mgr.AddBranch("locked", f.wtPath("locked"), f.base); err != nil {
		t.Fatalf("AddBranch: %v", err)
	}
	testutil.MakeDirUnreadable(t, dotgitWorktreesDir(f.repoPath))

	_, err := f.mgr.(*manager).adminDir("locked")
	if err == nil {
		t.Fatal("want a surfaced error for a real stat failure, not silent not-found")
	}
	if errors.Is(err, ErrWorktreeNotFound) {
		t.Errorf("a real stat error must not be reported as ErrWorktreeNotFound: %v", err)
	}
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

	wtA := openWorktree(t, f, pathA, "a")
	wtB := openWorktree(t, f, pathB, "b")

	hashA := commitOnlyInA(t, wtA, pathA)
	assertBranchAdvanced(t, f, hashA)
	assertWorktreeBClean(t, wtB, pathB)
	assertMainRepoClean(t, f)
}

func openWorktree(t *testing.T, f *fixture, path, label string) Worktree {
	wt, err := f.mgr.Open(path)
	if err != nil {
		t.Fatalf("Open %s: %v", label, err)
	}
	return wt
}

// commitOnlyInA writes, stages, and commits a file only in worktree A.
func commitOnlyInA(t *testing.T, wtA Worktree, pathA string) plumbing.Hash {
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
	return hashA
}

// assertBranchAdvanced checks A advanced to hashA while B stayed at base.
func assertBranchAdvanced(t *testing.T, f *fixture, hashA plumbing.Hash) {
	branchA, _ := f.repo.Reference(plumbing.NewBranchReferenceName("iso-a"), false)
	if branchA.Hash() != hashA || hashA == f.base {
		t.Fatalf("iso-a should advance to %s (got %s, base %s)", hashA, branchA.Hash(), f.base)
	}
	branchB, _ := f.repo.Reference(plumbing.NewBranchReferenceName("iso-b"), false)
	if branchB.Hash() != f.base {
		t.Fatalf("iso-b must stay at base %s, got %s", f.base, branchB.Hash())
	}
}

// assertWorktreeBClean checks B never saw a-only.txt and stays clean.
func assertWorktreeBClean(t *testing.T, wtB Worktree, pathB string) {
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
}

// assertMainRepoClean checks a-only.txt never leaked into the main repo.
func assertMainRepoClean(t *testing.T, f *fixture) {
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
	wt := openWorktree(t, f, path, "flow")

	assertCleanThenDirty(t, wt, path)
	commitInitialFile(t, wt, path)

	t.Run("commit options - all and empty", func(t *testing.T) {
		assertEmptyCommit(t, wt)
	})

	t.Run("commit All stages tracked modification", func(t *testing.T) {
		assertCommitAll(t, wt, path)
	})

	t.Run("stage missing path errors", func(t *testing.T) {
		if err := wt.Stage("does-not-exist.txt"); err == nil {
			t.Fatal("staging a missing path should error")
		}
	})
}

// assertCleanThenDirty checks a fresh worktree is clean, then dirty after write.
func assertCleanThenDirty(t *testing.T, wt Worktree, path string) {
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
}

// commitInitialFile stages f.txt (nil opts), commits, and checks HEAD advanced.
func commitInitialFile(t *testing.T, wt Worktree, path string) {
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
}

// assertEmptyCommit drives the AllowEmpty path with an explicit author.
func assertEmptyCommit(t *testing.T, wt Worktree) {
	h2, err := wt.Commit("empty", &CommitOptions{
		AuthorName: "X", AuthorEmail: "x@y", AllowEmpty: true,
	})
	if err != nil {
		t.Fatalf("empty commit: %v", err)
	}
	if h2.IsZero() {
		t.Fatal("empty commit returned zero hash")
	}
}

// assertCommitAll checks commit -a stages a tracked modification.
func assertCommitAll(t *testing.T, wt Worktree, path string) {
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
}

// TestManagerFromLinkedWorktreeUsesSharedGitDir reproduces the dogfood failure:
// a Manager opened from a LINKED worktree (as `da worktree` runs when invoked
// inside a worktree) must resolve worktree admin metadata against the SHARED git
// dir, not the per-worktree git dir — otherwise base-ref and registry paths
// double-nest under .git/worktrees/<self>/worktrees/... and reads/writes miss.
func TestManagerFromLinkedWorktreeUsesSharedGitDir(t *testing.T) {
	f := newFixture(t)
	if err := f.mgr.AddBranch("alpha", f.wtPath("alpha"), f.base); err != nil {
		t.Fatalf("AddBranch: %v", err)
	}
	// Open a fresh Manager rooted at the LINKED worktree (not the main repo).
	linked, err := NewManager(f.wtPath("alpha"))
	if err != nil {
		t.Fatalf("NewManager(linked): %v", err)
	}
	m := linked.(*manager)

	// adminDir must resolve to the SHARED <main>/.git/worktrees/alpha and exist
	// (the bug produced .git/worktrees/alpha/worktrees/alpha, which does not).
	got, err := m.adminDir("alpha")
	if err != nil {
		t.Fatalf("adminDir from linked worktree: %v", err)
	}
	wantMain, err := f.mgr.(*manager).adminDir("alpha")
	if err != nil {
		t.Fatalf("adminDir from main: %v", err)
	}
	if got != wantMain {
		t.Fatalf("adminDir(linked)=%q, want shared %q", got, wantMain)
	}

	// RecordBaseRef (the exact call that failed in the dogfood) must round-trip
	// via the shared dir: recorded from the linked manager, read from the main.
	if err := linked.RecordBaseRef("alpha", f.base); err != nil {
		t.Fatalf("RecordBaseRef from linked worktree: %v", err)
	}
	if ref, err := f.mgr.BaseRef("alpha"); err != nil || ref != f.base {
		t.Fatalf("BaseRef(main) after linked RecordBaseRef = %v, %v; want %v", ref, err, f.base)
	}

	// Registry roster/sidecar (gitDir) must likewise use the shared dir: a record
	// created via the linked-worktree manager is visible from the main-repo one.
	regLinked, err := NewRegistry(linked, time.Hour)
	if err != nil {
		t.Fatalf("NewRegistry(linked): %v", err)
	}
	if _, err := regLinked.Create("alpha", Metadata{Purpose: "dogfood"}); err != nil {
		t.Fatalf("Create via linked worktree: %v", err)
	}
	regMain, err := NewRegistry(f.mgr, time.Hour)
	if err != nil {
		t.Fatalf("NewRegistry(main): %v", err)
	}
	meta, err := regMain.Get("alpha")
	if err != nil {
		t.Fatalf("Get from main after linked Create: %v", err)
	}
	if meta.Purpose != "dogfood" {
		t.Fatalf("roster/sidecar not shared across checkouts: got purpose %q", meta.Purpose)
	}
}
