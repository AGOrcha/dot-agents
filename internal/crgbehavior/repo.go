package crgbehavior

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/utils/merkletrie"
)

// maxScannedFileBytes bounds how much of one changed file the declaration
// extractor reads. A generated bundle or a vendored blob carries no review
// interest and would otherwise dominate corpus generation.
const maxScannedFileBytes = 1 << 20

// FileChange is one name-level change a commit made: the post-image path plus
// the file's lines before and after.
//
// Declarations are derived from CONTENT, not from diff hunks. Git's hunk
// boundaries come from its own xdiff implementation (Myers plus change
// compaction and an indent heuristic), which no other differ reproduces, so an
// extractor reading `+`/`-` lines silently depends on which of several equally
// valid alignments git happened to choose. Comparing the declaration SETS of
// the two revisions is the contract the corpus actually wants — "the
// declarations this commit added or removed" — and it is differ-independent.
type FileChange struct {
	// Path is the post-image path; a deletion contributes no post-image file.
	Path string
	// Before and After are the file's lines at the parent and at the commit.
	Before []string
	After  []string
}

// repoReader is the repository read seam the corpus builder depends on, so
// every failure branch is reachable from a test without staging a repository.
type repoReader interface {
	// Resolve turns a revision into a commit hash.
	Resolve(rev string) (plumbing.Hash, error)
	// Window returns the newest count non-merge commit hashes reachable from
	// rev, newest first.
	Window(rev string, count int) ([]plumbing.Hash, error)
	// Subject returns a commit's subject line.
	Subject(hash plumbing.Hash) (string, error)
	// Changes returns a commit's name-level changes against its first parent.
	Changes(hash plumbing.Hash) ([]FileChange, error)
}

// gitRepo reads a repository in-process through go-git. The gate executes no
// git subprocess: revision resolution, history walking, commit metadata, tree
// diffing and blob reads are all native, and the per-commit worktrees the
// materializer needs come from internal/gitwt.
type gitRepo struct {
	repo *git.Repository
}

// openRepo opens the repository containing root.
func openRepo(root string) (*gitRepo, error) {
	repo, err := git.PlainOpenWithOptions(root, &git.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		return nil, fmt.Errorf("crgbehavior: open repository %s: %w", root, err)
	}
	return &gitRepo{repo: repo}, nil
}

// Resolve turns a revision into a commit hash.
func (g *gitRepo) Resolve(rev string) (plumbing.Hash, error) {
	hash, err := g.repo.ResolveRevision(plumbing.Revision(rev))
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("crgbehavior: resolve %s: %w", rev, err)
	}
	return *hash, nil
}

// Window returns the newest count non-merge commits reachable from rev.
// Committer-time order is used because it is the order `git log` presents and
// the order a reviewer means by "the last N commits".
func (g *gitRepo) Window(rev string, count int) ([]plumbing.Hash, error) {
	head, err := g.Resolve(rev)
	if err != nil {
		return nil, err
	}
	iter, err := g.repo.Log(&git.LogOptions{From: head, Order: git.LogOrderCommitterTime})
	if err != nil {
		return nil, fmt.Errorf("crgbehavior: walk history from %s: %w", rev, err)
	}
	defer iter.Close()
	out := make([]plumbing.Hash, 0, count)
	err = iter.ForEach(func(c *object.Commit) error {
		if c.NumParents() > 1 {
			return nil // a merge introduces no review task of its own
		}
		out = append(out, c.Hash)
		if len(out) >= count {
			return errStopWalk
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStopWalk) {
		return nil, fmt.Errorf("crgbehavior: walk history from %s: %w", rev, err)
	}
	return out, nil
}

// errStopWalk ends a commit walk early once the window is full.
var errStopWalk = errors.New("crgbehavior: stop walk")

// Subject returns a commit's subject line.
func (g *gitRepo) Subject(hash plumbing.Hash) (string, error) {
	commit, err := g.repo.CommitObject(hash)
	if err != nil {
		return "", fmt.Errorf("crgbehavior: read commit %s: %w", short(hash.String()), err)
	}
	subject := commit.Message
	if i := strings.IndexByte(subject, '\n'); i >= 0 {
		subject = subject[:i]
	}
	return strings.TrimSpace(subject), nil
}

// Changes returns a commit's name-level changes against its first parent, with
// each changed file's content on both sides. Rename detection is deliberately
// off: a rename is reported as a delete plus an add, which is the same set of
// post-image files a review of that commit reads.
func (g *gitRepo) Changes(hash plumbing.Hash) ([]FileChange, error) {
	commit, err := g.repo.CommitObject(hash)
	if err != nil {
		return nil, fmt.Errorf("crgbehavior: read commit %s: %w", short(hash.String()), err)
	}
	to, err := commit.Tree()
	if err != nil {
		return nil, fmt.Errorf("crgbehavior: read tree of %s: %w", short(hash.String()), err)
	}
	from, err := firstParentTree(commit)
	if err != nil {
		return nil, err
	}
	changes, err := object.DiffTree(from, to)
	if err != nil {
		return nil, fmt.Errorf("crgbehavior: diff %s against its parent: %w", short(hash.String()), err)
	}
	return fileChanges(changes)
}

// firstParentTree returns the tree of a commit's first parent, or nil for a
// root commit (whose every file is an addition against the empty tree).
func firstParentTree(commit *object.Commit) (*object.Tree, error) {
	if commit.NumParents() == 0 {
		return nil, nil
	}
	parent, err := commit.Parent(0)
	if err != nil {
		return nil, fmt.Errorf("crgbehavior: read parent of %s: %w", short(commit.Hash.String()), err)
	}
	tree, err := parent.Tree()
	if err != nil {
		return nil, fmt.Errorf("crgbehavior: read parent tree of %s: %w", short(commit.Hash.String()), err)
	}
	return tree, nil
}

// fileChanges lowers go-git's tree changes onto the corpus builder's shape,
// dropping deletions (no post-image file carries symbols to query).
func fileChanges(changes object.Changes) ([]FileChange, error) {
	out := make([]FileChange, 0, len(changes))
	for _, change := range changes {
		action, err := change.Action()
		if err != nil {
			return nil, fmt.Errorf("crgbehavior: classify tree change: %w", err)
		}
		if action == merkletrie.Delete {
			continue
		}
		before, after, err := change.Files()
		if err != nil {
			return nil, fmt.Errorf("crgbehavior: read changed file %s: %w", change.To.Name, err)
		}
		if after == nil {
			continue // a non-file entry (submodule gitlink, mode-only change)
		}
		afterLines, err := boundedLines(after)
		if err != nil {
			return nil, err
		}
		beforeLines, err := boundedLines(before)
		if err != nil {
			return nil, err
		}
		out = append(out, FileChange{Path: change.To.Name, Before: beforeLines, After: afterLines})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// boundedLines reads a blob's lines, skipping anything past the scan bound.
func boundedLines(file *object.File) ([]string, error) {
	if file == nil || file.Size > maxScannedFileBytes {
		return nil, nil
	}
	lines, err := file.Lines()
	if err != nil {
		return nil, fmt.Errorf("crgbehavior: read %s: %w", file.Name, err)
	}
	return lines, nil
}
