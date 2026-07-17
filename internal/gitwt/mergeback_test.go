package gitwt

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v6/plumbing"
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
