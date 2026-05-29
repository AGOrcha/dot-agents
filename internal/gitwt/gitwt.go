// Package gitwt is the typed git/worktree seam for the dot-agents managed
// worktree platform. It exposes worktree lifecycle (create/list/remove/prune),
// branch operations, base-ref record/read, and per-worktree index/status/commit
// as typed Go operations — never as stringly-typed shell calls.
//
// The concrete implementation (NewManager) wraps go-git v6
// (github.com/go-git/go-git/v6/x/plumbing/worktree), verified in the wt0 spike
// as a pure-Go, zero-shell-git mechanism (Decision A). Callers and skills bind
// to the Manager / Worktree interfaces only, so a later mechanism swap — or the
// hybrid shell-git fallback the spike proved unnecessary — stays invisible to
// them.
//
// Load-bearing constraints inherited from the go-git v6 x/plumbing/worktree
// package (see wt0 spike findings in the design doc):
//
//   - A worktree name must match ^[a-zA-Z0-9-]+$ (no slashes, dots, or
//     underscores). Callers with arbitrary branch names must encode them into
//     this charset; SafeName provides the canonical hash-based encoding.
//   - Branch-mode Add always creates a NEW branch named identically to the
//     worktree and errors if that branch already exists.
//   - The object store is shared across the main repo and all linked worktrees;
//     isolation is at the index / HEAD / working-tree level, which is exactly
//     what concurrent committers require.
package gitwt

import (
	"errors"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
)

// Typed errors. Callers should match these with errors.Is rather than string
// comparison. They wrap (or stand in for) the go-git package errors so the
// underlying mechanism stays hidden.
var (
	// ErrWorktreeNotFound is returned when an operation targets a worktree
	// (by name) that has no admin metadata under .git/worktrees/<name>.
	ErrWorktreeNotFound = errors.New("gitwt: worktree not found")

	// ErrWorktreeExists is returned by AddBranch/AddDetached when a worktree
	// with the given name already exists.
	ErrWorktreeExists = errors.New("gitwt: worktree already exists")

	// ErrBranchExists is returned by AddBranch when the branch it would create
	// (named identically to the worktree) already exists in the shared object
	// store. Branch-mode Add cannot reuse an existing branch.
	ErrBranchExists = errors.New("gitwt: branch already exists")

	// ErrInvalidName is returned when a worktree name does not match the
	// charset go-git enforces (^[a-zA-Z0-9-]+$). Use SafeName to encode.
	ErrInvalidName = errors.New("gitwt: invalid worktree name")

	// ErrBaseRefNotRecorded is returned by Worktree.BaseRef when no base ref
	// has been recorded for the worktree.
	ErrBaseRefNotRecorded = errors.New("gitwt: base ref not recorded")
)

// Manager is the lifecycle seam for linked worktrees attached to one repository.
//
// Implementations are tied to a single repository (the "main" worktree's
// repository). All names passed here are worktree names subject to the
// ^[a-zA-Z0-9-]+$ constraint; in branch mode the created branch is named
// identically to the worktree.
type Manager interface {
	// AddBranch creates a linked worktree at path checked out onto a NEW branch
	// named identically to name, rooted at base. The base hash is recorded so
	// later rebase/merge-back never re-derives it. Returns ErrWorktreeExists if
	// the worktree name is taken, ErrBranchExists if the branch already exists,
	// or ErrInvalidName if name is malformed.
	AddBranch(name, path string, base plumbing.Hash) error

	// AddDetached creates a linked worktree at path with a detached HEAD at
	// commit (swarm-cd style) — for read-only or ephemeral checkouts that need
	// no branch. The commit is also recorded as the base ref.
	AddDetached(name, path string, commit plumbing.Hash) error

	// Remove deletes the worktree's admin metadata (.git/worktrees/<name>) and
	// its working-tree directory at path. A zero-value path removes only the
	// metadata. Returns ErrWorktreeNotFound if the worktree is unknown.
	Remove(name, path string) error

	// List returns the names of all linked worktrees known to the repository
	// (enumerated natively from .git/worktrees, not from any external
	// registry).
	List() ([]string, error)

	// Prune removes admin metadata for linked worktrees whose working-tree
	// directory no longer exists on disk (the deterministic prune-if-gone
	// contract). It returns the names that were pruned.
	Prune() ([]string, error)

	// Open returns a Worktree handle for operating the linked worktree rooted
	// at path. The handle has its own index / HEAD / working tree, independent
	// of the main repository and of every other linked worktree.
	Open(path string) (Worktree, error)

	// RecordBaseRef stores base as the recorded base ref for the named
	// worktree, overwriting any prior value. Returns ErrWorktreeNotFound if the
	// worktree is unknown.
	RecordBaseRef(name string, base plumbing.Hash) error

	// BaseRef reads the recorded base ref for the named worktree. Returns
	// ErrBaseRefNotRecorded if none was recorded, or ErrWorktreeNotFound if the
	// worktree is unknown.
	BaseRef(name string) (plumbing.Hash, error)
}

// Worktree is the per-worktree operations seam: index/status/commit plus branch
// inspection, all scoped to a single linked worktree's isolated index and HEAD.
type Worktree interface {
	// Stage adds the working-tree path (file or directory, relative to the
	// worktree root) to this worktree's index. Staging here can never touch
	// another worktree's index.
	Stage(path string) error

	// Status returns the porcelain status of this worktree.
	Status() (git.Status, error)

	// Commit records the current index of this worktree as a new commit on its
	// current branch and returns the new commit hash. opts may be nil.
	Commit(message string, opts *CommitOptions) (plumbing.Hash, error)

	// Head returns the worktree's current HEAD reference.
	Head() (*plumbing.Reference, error)

	// Branch returns the short name of the branch HEAD points to, or "" if HEAD
	// is detached.
	Branch() (string, error)
}

// CommitOptions is the typed, mechanism-agnostic subset of commit configuration
// the seam exposes. A nil *CommitOptions is valid and means "use defaults".
type CommitOptions struct {
	// AuthorName and AuthorEmail set the commit author. When empty, go-git
	// falls back to repository/user config.
	AuthorName  string
	AuthorEmail string

	// All stages modified and deleted tracked files before committing (it does
	// not stage new untracked files), mirroring `git commit -a`.
	All bool

	// AllowEmpty permits a commit with no tree changes.
	AllowEmpty bool
}
