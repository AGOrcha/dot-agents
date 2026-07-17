// mergeback.go implements the first-class sub-branch create / merge-back
// workflow on top of wt1's Manager (branch + recorded base ref) and wt2's
// Registry (semantic metadata).
//
// Load-bearing property: merge-back resolves the integration base EXCLUSIVELY
// from the recorded base ref — it NEVER calls git merge-base to re-derive a
// fork point. Re-derivation is the stale-base trap this workflow exists to kill:
// after a parent is advanced or force-pushed, merge-base returns a fork point
// that silently rebases the sub-branch onto the wrong commit. Here the recorded
// base is authoritative, any drift between it and the parent's current tip is a
// loud failure (ErrStaleBase), and a sub-branch HEAD that moved out from under
// the integration is caught (ErrBranchDrift) rather than assumed intact.
package gitwt

import (
	"errors"
	"fmt"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/storer"
)

// Merge-back errors. Callers match with errors.Is.
var (
	// ErrStaleBase is returned by MergeBack when the recorded base ref no longer
	// equals the parent branch's current tip: the parent advanced or was
	// force-pushed after the sub-branch was created, so the recorded fork point
	// no longer describes the real relationship. MergeBack refuses to re-derive
	// a base with git merge-base and fails here instead of silently rebasing
	// onto the wrong commit.
	ErrStaleBase = errors.New("gitwt: recorded base is stale (parent moved since sub-branch creation)")

	// ErrBranchDrift is returned by MergeBack when, after integration, the
	// sub-branch worktree's HEAD no longer matches the commit that was
	// integrated. A concurrent pre-commit-hook stash/restore can silently land a
	// commit on a sibling worktree's branch (the parallel-worker-branch-drift
	// lesson); rather than assume the integrated tip was current, MergeBack
	// re-reads the worktree HEAD and fails loud on a mismatch.
	ErrBranchDrift = errors.New("gitwt: sub-branch HEAD drifted during merge-back")

	// ErrBaseNotAncestor is returned by MergeBack when the recorded base is not
	// an ancestor of the sub-branch tip, so advancing the parent to the sub tip
	// would not be a fast-forward and would orphan the parent's history.
	ErrBaseNotAncestor = errors.New("gitwt: recorded base is not an ancestor of the sub-branch tip")
)

// Coordinator wires wt1's Manager and wt2's Registry into the sub-branch create
// / merge-back workflow. It binds to the concrete go-git Manager so it can
// resolve branch refs and walk ancestry without widening the public Manager
// interface with those seams.
type Coordinator struct {
	mgr *manager
	reg *Registry

	// driftHook, when non-nil, runs inside MergeBack after the sub-branch tip is
	// captured and before the post-integration HEAD verification. It is nil in
	// every production path; same-package tests set it to simulate a concurrent
	// hook landing a commit on the branch (mirrors Registry.now's test seam).
	driftHook func()
}

// NewCoordinator binds a Coordinator to a Manager and Registry. The Manager must
// be the go-git implementation returned by NewManager (the same constraint
// NewRegistry enforces) so base resolution and ancestry checks can read the
// underlying repository.
func NewCoordinator(mgr Manager, reg *Registry) (*Coordinator, error) {
	m, ok := mgr.(*manager)
	if !ok {
		return nil, fmt.Errorf("gitwt: coordinator requires the go-git Manager, got %T", mgr)
	}
	if reg == nil {
		return nil, errors.New("gitwt: coordinator requires a registry")
	}
	return &Coordinator{mgr: m, reg: reg}, nil
}

// CreateOptions parameterizes CreateSubBranch.
type CreateOptions struct {
	// Name is the worktree name; the created branch is named identically. It
	// must satisfy the ^[a-zA-Z0-9-]+$ charset (use SafeName to encode).
	Name string
	// Path is the working-tree directory for the new linked worktree.
	Path string
	// BaseBranch is the parent branch whose current tip becomes the recorded
	// base (fork point). One of BaseBranch / Base must be set; Base wins when
	// both are provided.
	BaseBranch string
	// Base is an explicit fork-point commit. Takes precedence over BaseBranch
	// when non-zero.
	Base plumbing.Hash
	// Purpose is the free-form registry note (the task/slice the worktree is
	// for).
	Purpose string
	// ParentPR is the pull-request number the work feeds into (0 when none).
	ParentPR int
	// AppType is the app_type the delegated task runs under; it is recorded on
	// the worktree metadata and is the routing key the resolved execution shape
	// below was loaded from. Empty when the caller supplies no app_type.
	AppType string
	// Profile is the free-form execution profile (e.g. "loop-worker") recorded
	// verbatim on the worktree metadata.
	Profile string
	// VerifierSequence / LensSet / LensConcurrency / GraphBackend are the
	// app_type-routed execution shape the caller already resolved from the
	// project's execution_profile.by_app_type[AppType]. They are recorded as-is
	// so the worktree self-describes its execution shape; all are empty when no
	// profile entry resolved.
	VerifierSequence []string
	LensSet          []string
	LensConcurrency  string
	GraphBackend     string
}

// CreateResult reports the outcome of a successful CreateSubBranch.
type CreateResult struct {
	// Metadata is the registry record stamped for the new worktree.
	Metadata Metadata
	// Base is the fork-point commit recorded as the worktree's base ref.
	Base plumbing.Hash
}

// CreateSubBranch creates a linked worktree on a new branch rooted at the
// resolved base, records that base as the worktree's base ref (via wt1's
// Manager), and registers semantic metadata (via wt2's Registry). The recorded
// base is exactly what MergeBack reads later — it is never re-derived.
//
// The worktree+branch is created before the metadata is registered; a metadata
// failure therefore leaves an unregistered worktree recoverable via
// Registry.Reconcile rather than a partially-created branch.
func (c *Coordinator) CreateSubBranch(opts CreateOptions) (CreateResult, error) {
	base := opts.Base
	if base.IsZero() {
		if opts.BaseBranch == "" {
			return CreateResult{}, errors.New("gitwt: create sub-branch requires a base branch or base commit")
		}
		resolved, err := c.resolveBranch(opts.BaseBranch)
		if err != nil {
			return CreateResult{}, err
		}
		base = resolved
	}
	// AddBranch creates the worktree + branch AND records the base ref (wt1).
	if err := c.mgr.AddBranch(opts.Name, opts.Path, base); err != nil {
		return CreateResult{}, err
	}
	meta, err := c.reg.Create(opts.Name, Metadata{
		Purpose:          opts.Purpose,
		ParentPR:         opts.ParentPR,
		AppType:          opts.AppType,
		Profile:          opts.Profile,
		VerifierSequence: opts.VerifierSequence,
		LensSet:          opts.LensSet,
		LensConcurrency:  opts.LensConcurrency,
		GraphBackend:     opts.GraphBackend,
	})
	if err != nil {
		return CreateResult{}, err
	}
	return CreateResult{Metadata: meta, Base: base}, nil
}

// MergeBackOptions parameterizes MergeBack.
type MergeBackOptions struct {
	// Name is the sub-branch / worktree to merge back.
	Name string
	// ParentBranch is the branch the sub-branch is integrated into. Its current
	// tip MUST still equal the recorded base, or MergeBack fails with
	// ErrStaleBase — the parent is not allowed to have moved since creation.
	ParentBranch string
}

// MergeBackResult reports the outcome of a successful merge-back.
type MergeBackResult struct {
	// Base is the recorded fork point read back from the base ref (NOT derived).
	Base plumbing.Hash
	// SubHead is the sub-branch tip that was integrated.
	SubHead plumbing.Hash
	// ParentBranch is the branch that was fast-forwarded.
	ParentBranch string
	// ParentTip is the parent branch tip after integration (equals SubHead).
	ParentTip plumbing.Hash
}

// MergeBack integrates the sub-branch into its parent by fast-forwarding the
// parent branch to the sub-branch tip. It is the anti-stale-base path:
//
//   - The base is read EXCLUSIVELY from the recorded base ref (recordedBase). No
//     git merge-base call re-derives a fork point anywhere in this function.
//   - The recorded base must still equal the parent's current tip; otherwise the
//     parent moved (advanced or force-pushed) and MergeBack fails with
//     ErrStaleBase rather than silently rebasing onto a re-derived base.
//   - After integration it re-reads the sub-branch worktree HEAD and confirms it
//     still matches the integrated commit, failing with ErrBranchDrift if a
//     concurrent hook moved the branch (the parallel-worker-branch-drift
//     lesson).
func (c *Coordinator) MergeBack(opts MergeBackOptions) (MergeBackResult, error) {
	if opts.ParentBranch == "" {
		return MergeBackResult{}, errors.New("gitwt: merge-back requires a parent branch")
	}
	// The worktree must be registry-managed; refuse to merge back one the
	// registry never recorded, so the recorded metadata is always the source.
	if _, err := c.reg.Get(opts.Name); err != nil {
		return MergeBackResult{}, fmt.Errorf("gitwt: merge-back %q: %w", opts.Name, err)
	}

	// 1. Base comes ONLY from the recorded base ref. Never git merge-base.
	base, err := c.recordedBase(opts.Name)
	if err != nil {
		return MergeBackResult{}, err
	}

	// 2. Resolve the parent branch's CURRENT tip.
	parentTip, err := c.resolveBranch(opts.ParentBranch)
	if err != nil {
		return MergeBackResult{}, err
	}

	// 3. Stale-base guard: the recorded fork point must still be the parent tip.
	//    A naive merge-base re-derive would happily rebase onto a moved parent;
	//    we refuse and fail loud instead.
	if parentTip != base {
		return MergeBackResult{}, fmt.Errorf(
			"%w: recorded base %s for %q no longer matches parent %q tip %s; refusing to re-derive the base",
			ErrStaleBase, base, opts.Name, opts.ParentBranch, parentTip)
	}

	// 4. Capture the sub-branch tip we intend to integrate.
	subHead, err := c.resolveBranch(opts.Name)
	if err != nil {
		return MergeBackResult{}, err
	}

	// 5. The recorded base must be an ancestor of the sub tip — otherwise this
	//    is not a fast-forward and integration would orphan parent history.
	ok, err := c.isAncestor(base, subHead)
	if err != nil {
		return MergeBackResult{}, err
	}
	if !ok {
		return MergeBackResult{}, fmt.Errorf(
			"%w: base %s is not an ancestor of %q tip %s", ErrBaseNotAncestor, base, opts.Name, subHead)
	}

	// Test seam: simulate a concurrent hook landing a commit on the branch
	// between capture and verification. Nil in production.
	if c.driftHook != nil {
		c.driftHook()
	}

	// 6. Integrate: fast-forward the parent branch ref to the sub tip. Because
	//    base == parentTip and base is an ancestor of subHead, the sub-branch is
	//    already linear on the current parent, so advancing the ref is the
	//    complete integration (a --ff-only merge, no commit replay needed).
	parentRef := plumbing.NewBranchReferenceName(opts.ParentBranch)
	if err := c.mgr.repo.Storer.SetReference(plumbing.NewHashReference(parentRef, subHead)); err != nil {
		return MergeBackResult{}, fmt.Errorf("gitwt: merge-back fast-forward parent %q: %w", opts.ParentBranch, err)
	}

	// 7. Branch-drift guard: the sub-branch worktree HEAD must still be the
	//    commit we integrated; a mismatch means a commit landed out from under
	//    us and the parent now points at a stale tip.
	if err := c.verifyBranchHead(opts.Name, subHead); err != nil {
		return MergeBackResult{}, err
	}

	return MergeBackResult{
		Base:         base,
		SubHead:      subHead,
		ParentBranch: opts.ParentBranch,
		ParentTip:    subHead,
	}, nil
}

// recordedBase reads the fork point from wt1's recorded base ref. This is the
// ONLY base source MergeBack consults; it deliberately does not fall back to git
// merge-base when the record is absent.
func (c *Coordinator) recordedBase(name string) (plumbing.Hash, error) {
	base, err := c.mgr.BaseRef(name)
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("gitwt: merge-back read recorded base for %q: %w", name, err)
	}
	return base, nil
}

// resolveBranch resolves a local branch name to its current tip hash.
func (c *Coordinator) resolveBranch(branch string) (plumbing.Hash, error) {
	ref, err := c.mgr.repo.Reference(plumbing.NewBranchReferenceName(branch), true)
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("gitwt: resolve branch %q: %w", branch, err)
	}
	return ref.Hash(), nil
}

// verifyBranchHead re-reads the named worktree's branch HEAD through a fresh
// worktree Open and confirms it still equals want. Reading through the worktree
// (not the main repo's copy of the ref) reflects the on-disk HEAD the worker
// process would see, catching a branch that drifted underneath the merge-back.
func (c *Coordinator) verifyBranchHead(name string, want plumbing.Hash) error {
	path, err := c.mgr.worktreeDir(name)
	if err != nil {
		return fmt.Errorf("gitwt: merge-back locate worktree %q: %w", name, err)
	}
	wt, err := c.mgr.Open(path)
	if err != nil {
		return fmt.Errorf("gitwt: merge-back open worktree %q: %w", name, err)
	}
	head, err := wt.Head()
	if err != nil {
		return fmt.Errorf("gitwt: merge-back read worktree %q HEAD: %w", name, err)
	}
	if head.Hash() != want {
		return fmt.Errorf(
			"%w: worktree %q HEAD is %s, expected %s", ErrBranchDrift, name, head.Hash(), want)
	}
	return nil
}

// isAncestor reports whether ancestor is reachable from descendant by walking
// the commit graph from descendant back through its parents. It is a plain
// reachability check — deliberately NOT git merge-base — so merge-back never
// re-derives a fork point, even for its safety guard.
func (c *Coordinator) isAncestor(ancestor, descendant plumbing.Hash) (bool, error) {
	if ancestor == descendant {
		return true, nil
	}
	tip, err := c.mgr.repo.CommitObject(descendant)
	if err != nil {
		return false, fmt.Errorf("gitwt: load commit %s: %w", descendant, err)
	}
	found := false
	iter := object.NewCommitPreorderIter(tip, nil, nil)
	err = iter.ForEach(func(comm *object.Commit) error {
		if comm.Hash == ancestor {
			found = true
			return storer.ErrStop
		}
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("gitwt: walk ancestry from %s: %w", descendant, err)
	}
	return found, nil
}
