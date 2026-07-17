package gitwt

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
)

// newCoordinator binds a Coordinator to the fixture manager + a registry.
func newCoordinator(t *testing.T, f *fixture) *Coordinator {
	t.Helper()
	reg, err := NewRegistry(f.mgr, time.Hour)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	coord, err := NewCoordinator(f.mgr, reg)
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}
	return coord
}

// setBranch points a local branch ref at h (used to stand up / move the parent).
func setBranch(t *testing.T, f *fixture, name string, h plumbing.Hash) {
	t.Helper()
	ref := plumbing.NewHashReference(plumbing.NewBranchReferenceName(name), h)
	if err := f.repo.Storer.SetReference(ref); err != nil {
		t.Fatalf("set branch %s: %v", name, err)
	}
}

// branchHash resolves a local branch to its current tip.
func branchHash(t *testing.T, f *fixture, name string) plumbing.Hash {
	t.Helper()
	ref, err := f.repo.Reference(plumbing.NewBranchReferenceName(name), true)
	if err != nil {
		t.Fatalf("resolve branch %s: %v", name, err)
	}
	return ref.Hash()
}

// createSub is the common setup: a "parent" branch at the fixture base and a
// registered sub-branch worktree forked from it.
func createSub(t *testing.T, f *fixture, coord *Coordinator, name string) CreateResult {
	t.Helper()
	setBranch(t, f, "parent", f.base)
	res, err := coord.CreateSubBranch(CreateOptions{
		Name:       name,
		Path:       f.wtPath(name),
		BaseBranch: "parent",
		Purpose:    "wt3 slice",
		ParentPR:   409,
	})
	if err != nil {
		t.Fatalf("CreateSubBranch: %v", err)
	}
	return res
}

func TestNewCoordinatorGuards(t *testing.T) {
	f := newFixture(t)
	t.Run("foreign manager", func(t *testing.T) {
		if _, err := NewCoordinator(stubManager{}, &Registry{}); err == nil {
			t.Fatal("NewCoordinator accepted a non-go-git Manager, want error")
		}
	})
	t.Run("nil registry", func(t *testing.T) {
		if _, err := NewCoordinator(f.mgr, nil); err == nil {
			t.Fatal("NewCoordinator accepted a nil registry, want error")
		}
	})
}

func TestCreateSubBranch(t *testing.T) {
	f := newFixture(t)
	coord := newCoordinator(t, f)
	res := createSub(t, f, coord, "sub")

	// Sub-branch exists in the shared store, rooted at the recorded base.
	if got := branchHash(t, f, "sub"); got != f.base {
		t.Fatalf("branch sub at %s, want base %s", got, f.base)
	}
	// Base recorded via wt1's base ref, and echoed back in the result.
	if res.Base != f.base {
		t.Fatalf("CreateResult.Base=%s, want %s", res.Base, f.base)
	}
	recorded, err := f.mgr.BaseRef("sub")
	if err != nil {
		t.Fatalf("BaseRef: %v", err)
	}
	if recorded != f.base {
		t.Fatalf("recorded base=%s, want %s", recorded, f.base)
	}
	// Metadata registered via the wt2 registry.
	meta, err := coord.reg.Get("sub")
	if err != nil {
		t.Fatalf("registry Get: %v", err)
	}
	if meta.Name != "sub" || meta.Purpose != "wt3 slice" || meta.ParentPR != 409 {
		t.Fatalf("metadata=%+v, want name=sub purpose=\"wt3 slice\" parentPR=409", meta)
	}
	if meta.CreatedAt.IsZero() {
		t.Fatal("metadata CreatedAt not stamped")
	}
}

func TestCreateSubBranchRequiresBase(t *testing.T) {
	f := newFixture(t)
	coord := newCoordinator(t, f)
	if _, err := coord.CreateSubBranch(CreateOptions{Name: "sub", Path: f.wtPath("sub")}); err == nil {
		t.Fatal("CreateSubBranch without a base or base-branch succeeded, want error")
	}
}

// TestMergeBackReadsRecordedBase is the happy path: merge-back reads the base
// from the recorded base ref and fast-forwards the parent to the sub tip.
func TestMergeBackReadsRecordedBase(t *testing.T) {
	f := newFixture(t)
	coord := newCoordinator(t, f)
	createSub(t, f, coord, "sub")

	advanceHead(t, f, "sub") // commit real work on the sub-branch
	subHead := branchHash(t, f, "sub")
	if subHead == f.base {
		t.Fatal("sub-branch did not advance past base")
	}

	res, err := coord.MergeBack(MergeBackOptions{Name: "sub", ParentBranch: "parent"})
	if err != nil {
		t.Fatalf("MergeBack: %v", err)
	}
	// The base MergeBack acted on is exactly the recorded fork point.
	if res.Base != f.base {
		t.Fatalf("result base=%s, want recorded %s", res.Base, f.base)
	}
	if res.SubHead != subHead || res.ParentTip != subHead {
		t.Fatalf("result subHead=%s parentTip=%s, want %s", res.SubHead, res.ParentTip, subHead)
	}
	// Parent branch fast-forwarded to the integrated tip.
	if got := branchHash(t, f, "parent"); got != subHead {
		t.Fatalf("parent tip after merge-back=%s, want %s", got, subHead)
	}
}

// TestMergeBackRequiresRecordedBase proves base resolution has NO merge-base
// fallback: with the recorded base ref removed, merge-back fails rather than
// silently re-deriving the fork point.
func TestMergeBackRequiresRecordedBase(t *testing.T) {
	f := newFixture(t)
	coord := newCoordinator(t, f)
	createSub(t, f, coord, "sub")
	advanceHead(t, f, "sub")

	// Strip the recorded base ref. A merge-base-based impl would recompute the
	// fork point and proceed; ours must refuse.
	dir, err := f.mgr.(*manager).adminDir("sub")
	if err != nil {
		t.Fatalf("adminDir: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, baseRefFile)); err != nil {
		t.Fatalf("rm base-ref: %v", err)
	}

	_, err = coord.MergeBack(MergeBackOptions{Name: "sub", ParentBranch: "parent"})
	if !errors.Is(err, ErrBaseRefNotRecorded) {
		t.Fatalf("want ErrBaseRefNotRecorded, got %v", err)
	}
	// No integration happened: parent stays at base.
	if got := branchHash(t, f, "parent"); got != f.base {
		t.Fatalf("parent moved to %s despite failed merge-back, want %s", got, f.base)
	}
}

// TestMergeBackStaleBaseParentAdvanced is the core regression: after the parent
// advances (a still-valid descendant fork point that git merge-base would
// happily re-derive to), merge-back must FAIL rather than silently rebase.
func TestMergeBackStaleBaseParentAdvanced(t *testing.T) {
	f := newFixture(t)
	coord := newCoordinator(t, f)
	createSub(t, f, coord, "sub")
	advanceHead(t, f, "sub")
	subHead := branchHash(t, f, "sub")

	// Parent advances to a new descendant commit AFTER the base was recorded.
	// merge-base(sub, parent) would still resolve to f.base and a naive impl
	// would proceed; ours compares recorded base vs parent tip and refuses.
	advanced := commitFile(t, f.repo, "parent-adv.txt", "moved\n", "parent advanced")
	setBranch(t, f, "parent", advanced)

	_, err := coord.MergeBack(MergeBackOptions{Name: "sub", ParentBranch: "parent"})
	if !errors.Is(err, ErrStaleBase) {
		t.Fatalf("want ErrStaleBase, got %v", err)
	}
	// Parent untouched (no partial integration), sub untouched.
	if got := branchHash(t, f, "parent"); got != advanced {
		t.Fatalf("parent tip=%s, want unchanged %s", got, advanced)
	}
	if got := branchHash(t, f, "sub"); got != subHead {
		t.Fatalf("sub tip=%s, want unchanged %s", got, subHead)
	}
}

// TestMergeBackStaleBaseForcePushed covers a rewritten parent whose new tip is
// not even a locally-known descendant (force-push): the recorded base no longer
// matches, so merge-back fails loud.
func TestMergeBackStaleBaseForcePushed(t *testing.T) {
	f := newFixture(t)
	coord := newCoordinator(t, f)
	createSub(t, f, coord, "sub")
	advanceHead(t, f, "sub")

	// Simulate a force-push: the parent now points at a rewritten tip distinct
	// from the recorded base.
	forced := plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	setBranch(t, f, "parent", forced)

	_, err := coord.MergeBack(MergeBackOptions{Name: "sub", ParentBranch: "parent"})
	if !errors.Is(err, ErrStaleBase) {
		t.Fatalf("want ErrStaleBase, got %v", err)
	}
}

// TestMergeBackBranchDrift honors the parallel-worker-branch-drift lesson: if
// the sub-branch HEAD moves out from under the merge-back (a sibling hook
// landing a commit), the post-integration HEAD check catches it.
func TestMergeBackBranchDrift(t *testing.T) {
	f := newFixture(t)
	coord := newCoordinator(t, f)
	createSub(t, f, coord, "sub")
	advanceHead(t, f, "sub")
	subHead := branchHash(t, f, "sub")

	// Between capturing the sub tip and verifying it, a concurrent hook rewinds
	// the branch — modelled here by rewinding refs/heads/sub to base.
	coord.driftHook = func() { setBranch(t, f, "sub", f.base) }

	_, err := coord.MergeBack(MergeBackOptions{Name: "sub", ParentBranch: "parent"})
	if !errors.Is(err, ErrBranchDrift) {
		t.Fatalf("want ErrBranchDrift, got %v", err)
	}
	// The integrated commit was the captured (pre-drift) tip, but the worktree
	// HEAD no longer matches it — proving detection, not assumption.
	if branchHash(t, f, "sub") == subHead {
		t.Fatal("drift hook did not move the sub-branch; test is not exercising drift")
	}
}

func TestMergeBackUnregisteredWorktree(t *testing.T) {
	f := newFixture(t)
	coord := newCoordinator(t, f)
	setBranch(t, f, "parent", f.base)
	// Create the worktree via the raw Manager, skipping registry.Create.
	if err := f.mgr.AddBranch("sub", f.wtPath("sub"), f.base); err != nil {
		t.Fatalf("AddBranch: %v", err)
	}
	_, err := coord.MergeBack(MergeBackOptions{Name: "sub", ParentBranch: "parent"})
	if !errors.Is(err, ErrMetadataNotRecorded) {
		t.Fatalf("want ErrMetadataNotRecorded, got %v", err)
	}
}

func TestMergeBackRequiresParentBranch(t *testing.T) {
	f := newFixture(t)
	coord := newCoordinator(t, f)
	createSub(t, f, coord, "sub")
	if _, err := coord.MergeBack(MergeBackOptions{Name: "sub"}); err == nil {
		t.Fatal("MergeBack without a parent branch succeeded, want error")
	}
}

// --- error-path coverage ---------------------------------------------------

// buildCommit stores a commit object (reusing the fixture base's tree) with the
// given parents and returns its hash. With no parents it is an orphan root that
// does NOT descend from the base; with a bogus parent its ancestry walk cannot
// complete.
func buildCommit(t *testing.T, f *fixture, parents []plumbing.Hash, msg string) plumbing.Hash {
	t.Helper()
	baseCommit, err := f.repo.CommitObject(f.base)
	if err != nil {
		t.Fatalf("load base commit: %v", err)
	}
	sig := object.Signature{Name: "T", Email: "t@x", When: time.Now()}
	c := &object.Commit{
		Author:       sig,
		Committer:    sig,
		Message:      msg,
		TreeHash:     baseCommit.TreeHash,
		ParentHashes: parents,
	}
	obj := f.repo.Storer.NewEncodedObject()
	if err := c.Encode(obj); err != nil {
		t.Fatalf("encode commit: %v", err)
	}
	h, err := f.repo.Storer.SetEncodedObject(obj)
	if err != nil {
		t.Fatalf("store commit: %v", err)
	}
	return h
}

// TestCreateSubBranchBadBaseBranch proves an unresolvable base branch fails
// create before any worktree is made.
func TestCreateSubBranchBadBaseBranch(t *testing.T) {
	f := newFixture(t)
	coord := newCoordinator(t, f)
	if _, err := coord.CreateSubBranch(CreateOptions{
		Name: "sub", Path: f.wtPath("sub"), BaseBranch: "ghost",
	}); err == nil {
		t.Fatal("CreateSubBranch with an unknown base branch succeeded, want error")
	}
}

// TestCreateSubBranchAddBranchFails proves a duplicate worktree name fails the
// AddBranch step.
func TestCreateSubBranchAddBranchFails(t *testing.T) {
	f := newFixture(t)
	coord := newCoordinator(t, f)
	createSub(t, f, coord, "sub")
	if _, err := coord.CreateSubBranch(CreateOptions{
		Name: "sub", Path: f.wtPath("sub"), BaseBranch: "parent",
	}); err == nil {
		t.Fatal("CreateSubBranch with a duplicate name succeeded, want error")
	}
}

// TestCreateSubBranchRegistryCreateFails proves that when the worktree+branch
// are created but registry metadata cannot be recorded, CreateSubBranch fails
// and leaves the worktree recoverable via Reconcile (per the doc contract).
func TestCreateSubBranchRegistryCreateFails(t *testing.T) {
	f := newFixture(t)
	coord := newCoordinator(t, f)
	setBranch(t, f, "parent", f.base)
	// Make the roster path a directory so reg.Create's addToRoster fails AFTER
	// AddBranch has already created the worktree.
	if err := os.Mkdir(coord.reg.rosterPath(), 0o755); err != nil {
		t.Fatalf("mkdir roster: %v", err)
	}
	if _, err := coord.CreateSubBranch(CreateOptions{
		Name: "sub", Path: f.wtPath("sub"), BaseBranch: "parent",
	}); err == nil {
		t.Fatal("CreateSubBranch succeeded despite a registry Create failure, want error")
	}
	names, err := f.mgr.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !contains(names, "sub") {
		t.Fatalf("List=%v, want sub present (worktree created before metadata failed)", names)
	}
}

// TestMergeBackParentBranchUnresolvable proves an unknown parent branch fails
// merge-back at the parent-tip resolution step.
func TestMergeBackParentBranchUnresolvable(t *testing.T) {
	f := newFixture(t)
	coord := newCoordinator(t, f)
	createSub(t, f, coord, "sub")
	if _, err := coord.MergeBack(MergeBackOptions{Name: "sub", ParentBranch: "ghost"}); err == nil {
		t.Fatal("MergeBack with an unknown parent branch succeeded, want error")
	}
}

// TestMergeBackSubBranchUnresolvable proves a registered worktree whose branch
// ref was deleted fails at the sub-tip resolution step (after the stale guard).
func TestMergeBackSubBranchUnresolvable(t *testing.T) {
	f := newFixture(t)
	coord := newCoordinator(t, f)
	createSub(t, f, coord, "sub")
	// Delete the sub-branch ref while base + parent stay put so the stale guard
	// passes and it is the sub-tip resolution that fails.
	if err := f.repo.Storer.RemoveReference(plumbing.NewBranchReferenceName("sub")); err != nil {
		t.Fatalf("remove sub ref: %v", err)
	}
	if _, err := coord.MergeBack(MergeBackOptions{Name: "sub", ParentBranch: "parent"}); err == nil {
		t.Fatal("MergeBack succeeded despite a missing sub-branch ref, want error")
	}
}

// TestMergeBackSubTipInvalidCommit proves a sub tip that is not a valid commit
// object surfaces the ancestry-check load error (not ErrBaseNotAncestor).
func TestMergeBackSubTipInvalidCommit(t *testing.T) {
	f := newFixture(t)
	coord := newCoordinator(t, f)
	createSub(t, f, coord, "sub")
	bogus := plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	setBranch(t, f, "sub", bogus)
	_, err := coord.MergeBack(MergeBackOptions{Name: "sub", ParentBranch: "parent"})
	if err == nil {
		t.Fatal("MergeBack succeeded despite an invalid sub tip, want error")
	}
	if errors.Is(err, ErrBaseNotAncestor) {
		t.Fatalf("an unreadable sub tip should be a load error, not ErrBaseNotAncestor: %v", err)
	}
}

// TestMergeBackBaseNotAncestor proves that when the recorded base is not an
// ancestor of the sub tip, merge-back refuses with ErrBaseNotAncestor rather
// than performing a history-orphaning non-fast-forward.
func TestMergeBackBaseNotAncestor(t *testing.T) {
	f := newFixture(t)
	coord := newCoordinator(t, f)
	createSub(t, f, coord, "sub")
	// Point the sub-branch at an orphan commit that does NOT descend from the
	// recorded base, so base is unreachable from the sub tip.
	orphan := buildCommit(t, f, nil, "orphan")
	setBranch(t, f, "sub", orphan)
	if _, err := coord.MergeBack(MergeBackOptions{Name: "sub", ParentBranch: "parent"}); !errors.Is(err, ErrBaseNotAncestor) {
		t.Fatalf("want ErrBaseNotAncestor, got %v", err)
	}
}

// TestMergeBackAncestryWalkError proves a sub tip whose parent object is missing
// surfaces the ancestry-walk error (the tip itself loads, but the walk cannot).
func TestMergeBackAncestryWalkError(t *testing.T) {
	f := newFixture(t)
	coord := newCoordinator(t, f)
	createSub(t, f, coord, "sub")
	broken := buildCommit(t, f, []plumbing.Hash{plumbing.NewHash("cccccccccccccccccccccccccccccccccccccccc")}, "broken-parent")
	setBranch(t, f, "sub", broken)
	_, err := coord.MergeBack(MergeBackOptions{Name: "sub", ParentBranch: "parent"})
	if err == nil {
		t.Fatal("MergeBack succeeded despite a broken ancestry walk, want error")
	}
	if errors.Is(err, ErrBaseNotAncestor) {
		t.Fatalf("a walk load error should not be ErrBaseNotAncestor: %v", err)
	}
}

// TestMergeBackNoNewWork proves merging back a sub with no commits past base is
// a valid no-op fast-forward (the base == sub-tip ancestry leg).
func TestMergeBackNoNewWork(t *testing.T) {
	f := newFixture(t)
	coord := newCoordinator(t, f)
	createSub(t, f, coord, "sub") // no advanceHead: sub tip == recorded base
	res, err := coord.MergeBack(MergeBackOptions{Name: "sub", ParentBranch: "parent"})
	if err != nil {
		t.Fatalf("MergeBack: %v", err)
	}
	if res.SubHead != f.base || res.ParentTip != f.base {
		t.Fatalf("result subHead=%s parentTip=%s, want base %s", res.SubHead, res.ParentTip, f.base)
	}
	if got := branchHash(t, f, "parent"); got != f.base {
		t.Fatalf("parent tip=%s, want base %s", got, f.base)
	}
}

// TestMergeBackSetReferenceFails proves a failure fast-forwarding the parent ref
// surfaces from merge-back. The parent loose ref is replaced by a directory via
// the drift-hook seam AFTER the parent tip was read but BEFORE the ref write.
func TestMergeBackSetReferenceFails(t *testing.T) {
	f := newFixture(t)
	coord := newCoordinator(t, f)
	createSub(t, f, coord, "sub")
	advanceHead(t, f, "sub")
	parentRefPath := filepath.Join(f.mgr.(*manager).gitDir(), "refs", "heads", "parent")
	coord.driftHook = func() {
		_ = os.Remove(parentRefPath)
		_ = os.Mkdir(parentRefPath, 0o755)
	}
	if _, err := coord.MergeBack(MergeBackOptions{Name: "sub", ParentBranch: "parent"}); err == nil {
		t.Fatal("MergeBack succeeded despite a parent-ref write failure, want error")
	}
}

// TestMergeBackVerifyBranchHeadErrors drives the three post-integration
// verification error legs: the sub worktree becomes unresolvable / unopenable /
// its HEAD unreadable AFTER the fast-forward, so merge-back reports the
// verification failure rather than assuming the integrated tip is current. Each
// corruption is injected via the drift-hook seam (after the ref write).
func TestMergeBackVerifyBranchHeadErrors(t *testing.T) {
	t.Run("worktree dir unresolvable", func(t *testing.T) {
		f := newFixture(t)
		coord := newCoordinator(t, f)
		createSub(t, f, coord, "sub")
		advanceHead(t, f, "sub")
		dir, _ := f.mgr.(*manager).adminDir("sub")
		coord.driftHook = func() { _ = os.Remove(filepath.Join(dir, "gitdir")) }
		if _, err := coord.MergeBack(MergeBackOptions{Name: "sub", ParentBranch: "parent"}); err == nil {
			t.Fatal("MergeBack succeeded despite an unresolvable worktree dir, want error")
		}
	})

	t.Run("worktree unopenable", func(t *testing.T) {
		f := newFixture(t)
		coord := newCoordinator(t, f)
		createSub(t, f, coord, "sub")
		advanceHead(t, f, "sub")
		coord.driftHook = func() {
			_ = os.WriteFile(filepath.Join(f.wtPath("sub"), ".git"), []byte("garbage"), 0o644)
		}
		if _, err := coord.MergeBack(MergeBackOptions{Name: "sub", ParentBranch: "parent"}); err == nil {
			t.Fatal("MergeBack succeeded despite an unopenable worktree, want error")
		}
	})

	t.Run("worktree head unreadable", func(t *testing.T) {
		f := newFixture(t)
		coord := newCoordinator(t, f)
		createSub(t, f, coord, "sub")
		advanceHead(t, f, "sub")
		dir, _ := f.mgr.(*manager).adminDir("sub")
		coord.driftHook = func() {
			_ = os.WriteFile(filepath.Join(dir, "HEAD"), []byte("ref: refs/heads/does-not-exist\n"), 0o644)
		}
		if _, err := coord.MergeBack(MergeBackOptions{Name: "sub", ParentBranch: "parent"}); err == nil {
			t.Fatal("MergeBack succeeded despite an unreadable worktree HEAD, want error")
		}
	})
}
