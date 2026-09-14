// Package graphstore — in-process Git access.
//
// Every Git read this package performs goes through go-git here. There is no
// `git` subprocess in shipping graphstore code, and tools/execguard enforces
// that mechanically: a graphstore Git shell-out cannot be allowlisted.
//
// Two readers live here, and each one's ground truth is deliberate:
//
//   - the INDEX is the source of truth for submodules and tracked files. A
//     gitlink is a `filemode.Submodule` index entry; reading HEAD's tree or
//     .gitmodules instead would lose a staged gitlink and a gitlink that no
//     .gitmodules ever declared (both real states after a partial merge), and
//     those are exactly the roots a plain enumeration silently skips.
//   - the MERGE BASE is the source of truth for "what changed since base".
//     `base...HEAD` is three-dot syntax: it diffs merge-base(base, HEAD)
//     against HEAD, not base against HEAD. A two-dot substitution would change
//     the operator-visible changed-file set the moment base moved on.
package graphstore

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/format/index"
	"github.com/go-git/go-git/v6/plumbing/object"
)

// openRepo opens dir as a repository ROOT. DetectDotGit is deliberately off:
// every caller here passes a root (a superproject, a submodule working tree,
// or CRGBridge.RepoRoot), and silently walking up to an ancestor repository
// would enumerate a different repository than the one asked for.
//
// A submodule working tree carries a `.git` FILE pointing at
// `<super>/.git/modules/<name>`; go-git follows that pointer, so an
// initialized submodule opens the same way a standalone checkout does.
func openRepo(dir string) (*git.Repository, error) {
	repo, err := git.PlainOpen(dir)
	if err != nil {
		return nil, fmt.Errorf("open git repository %s: %w", dir, err)
	}
	return repo, nil
}

// readIndex returns dir's git index.
func readIndex(dir string) (*index.Index, error) {
	repo, err := openRepo(dir)
	if err != nil {
		return nil, err
	}
	idx, err := repo.Storer.Index()
	if err != nil {
		return nil, fmt.Errorf("read git index %s: %w", dir, err)
	}
	return idx, nil
}

// gitlinkPaths returns the slash-separated paths of the direct gitlink entries
// in dir's index, deduplicated and lexically sorted.
//
// Deduplication matters for a conflicted gitlink: an unmerged path carries one
// index entry per stage, and three identical submodule roots would be walked,
// built, and merged three times.
func gitlinkPaths(dir string) ([]string, error) {
	idx, err := readIndex(dir)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	var paths []string
	for _, entry := range idx.Entries {
		// An escaping or duplicate name is dropped rather than walked: the
		// index of a cloned superproject is untrusted input, and a conflicted
		// gitlink carries one entry per stage.
		if entry.Mode != filemode.Submodule || seen[entry.Name] || !containedIn(entry.Name) {
			continue
		}
		seen[entry.Name] = true
		paths = append(paths, entry.Name)
	}
	sort.Strings(paths)
	return paths, nil
}

// EnumerateTrackedFiles lists the tracked files of repoRoot as slash-separated
// repo-relative paths, lexically sorted.
//
// recurseSubmodules is the whole point of this helper: with it false the result
// is what the index literally holds (submodule contents invisible, one gitlink
// entry per submodule); with it true each initialized submodule's own index is
// read and its files appear under the submodule's path. Callers use the
// difference to report how much a non-recursive enumeration would have missed.
//
// An UNINITIALIZED submodule contributes its gitlink entry and nothing else:
// there is no checkout to read, and dropping the entry would understate the
// enumeration. A submodule that is initialized but whose repository cannot be
// read is an error, never a silent zero.
func EnumerateTrackedFiles(repoRoot string, recurseSubmodules bool) ([]string, error) {
	return enumerateTrackedFiles(repoRoot, recurseSubmodules, 0)
}

// enumerateTrackedFiles is the depth-bounded worker behind
// EnumerateTrackedFiles. The bound is the same one discovery uses, so a
// pathological nesting chain cannot drive an unbounded walk.
func enumerateTrackedFiles(repoRoot string, recurseSubmodules bool, depth int) ([]string, error) {
	idx, err := readIndex(repoRoot)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, entry := range idx.Entries {
		nested, err := submoduleFiles(repoRoot, entry, recurseSubmodules, depth)
		if err != nil {
			return nil, err
		}
		if nested == nil {
			files = append(files, entry.Name)
			continue
		}
		files = append(files, nested...)
	}
	sort.Strings(files)
	return files, nil
}

// submoduleFiles returns the prefixed tracked files of entry when entry is a
// gitlink that should be walked, or nil when the entry stands for itself. A nil
// slice with a nil error means "emit the entry's own path".
func submoduleFiles(repoRoot string, entry *index.Entry, recurseSubmodules bool, depth int) ([]string, error) {
	if !recurseSubmodules || entry.Mode != filemode.Submodule ||
		depth >= maxSubmoduleDepth || !containedIn(entry.Name) {
		return nil, nil
	}
	abs := filepath.Join(repoRoot, filepath.FromSlash(entry.Name))
	if !submoduleInitialized(abs) {
		return nil, nil
	}
	inner, err := enumerateTrackedFiles(abs, true, depth+1)
	if err != nil {
		return nil, fmt.Errorf("enumerate submodule %s: %w", entry.Name, err)
	}
	// An initialized submodule with an empty index still replaces its gitlink
	// entry: that is what the submodule actually tracks.
	out := make([]string, 0, len(inner))
	for _, f := range inner {
		out = append(out, entry.Name+"/"+f)
	}
	return out, nil
}

// changedFilesAgainst reports the files that changed between
// merge-base(base, HEAD) and HEAD, as lexically sorted slash-separated
// repository-relative paths.
//
// It is the native equivalent of
// `git diff --name-only --diff-filter=ACMRTUXB base...HEAD`:
//
//   - three-dot semantics: the diff starts at the MERGE BASE, so work that
//     landed on base after the branch point is not reported as changed here.
//   - ACMRTUXB is every filter letter EXCEPT D. In a tree-to-tree diff that is
//     exactly "every change with a destination path": added, copied, modified
//     and type-changed entries keep their path, a rename/copy reports its
//     DESTINATION (what `--name-only` prints), and a deletion has no
//     destination and is excluded. U/X/B describe unmerged, unknown and broken
//     index states that cannot occur between two committed trees.
//   - rename/copy detection is on, matching git's own default for `git diff`.
func changedFilesAgainst(repoRoot, base string) ([]string, error) {
	repo, err := openRepo(repoRoot)
	if err != nil {
		return nil, err
	}
	headCommit, err := resolveCommit(repo, "HEAD")
	if err != nil {
		return nil, err
	}
	baseCommit, err := resolveCommit(repo, base)
	if err != nil {
		return nil, err
	}
	mergeBases, err := baseCommit.MergeBase(headCommit)
	if err != nil {
		return nil, fmt.Errorf("merge base of %s and HEAD: %w", base, err)
	}
	if len(mergeBases) == 0 {
		return nil, fmt.Errorf("no merge base between %s and HEAD", base)
	}
	// The diff runs under the package's provider-owned request timeout
	// (CONTRACT.md guarantee #2) rather than an unbounded background context:
	// a tree walk over a very large history must not be able to wedge the
	// process the way an unbounded read could.
	ctx, cancel := requestContext(nil)
	defer cancel()
	return changedPaths(ctx, mergeBases[0], headCommit)
}

// changedPaths diffs two commits' trees and returns the destination path of
// every non-deletion change. A cancelled ctx aborts the walk with an error
// rather than reporting a truncated changed-file set as complete.
func changedPaths(ctx context.Context, from, to *object.Commit) ([]string, error) {
	fromTree, err := from.Tree()
	if err != nil {
		return nil, fmt.Errorf("read tree of %s: %w", from.Hash, err)
	}
	toTree, err := to.Tree()
	if err != nil {
		return nil, fmt.Errorf("read tree of %s: %w", to.Hash, err)
	}
	changes, err := object.DiffTreeWithOptions(ctx, fromTree, toTree, object.DefaultDiffTreeOptions)
	if err != nil {
		return nil, fmt.Errorf("diff trees %s..%s: %w", from.Hash, to.Hash, err)
	}
	seen := make(map[string]bool, len(changes))
	var files []string
	for _, change := range changes {
		// An empty destination name is a deletion — the one filter letter
		// ACMRTUXB excludes. Everything else is reported at its destination,
		// which is also the path `--name-only` prints for a rename or copy.
		if change.To.Name == "" || seen[change.To.Name] {
			continue
		}
		seen[change.To.Name] = true
		files = append(files, change.To.Name)
	}
	sort.Strings(files)
	return files, nil
}

// resolveCommit resolves a revision (a ref, a hash prefix, or a `~`/`^`
// expression such as HEAD~1) to its commit.
//
// A detached HEAD resolves like any other: HEAD is a direct hash reference.
// A repository with no commits, and a `HEAD~1` in a repository whose HEAD has
// no parent, both fail here — the same states `git diff` refuses rather than
// reporting an empty diff.
func resolveCommit(repo *git.Repository, rev string) (*object.Commit, error) {
	hash, err := repo.ResolveRevision(plumbing.Revision(rev))
	if err == nil {
		// ResolveRevision always lands on a commit hash, so this second step
		// only fails when the object store itself cannot produce it. Both
		// failures are the same thing to the caller — "this revision did not
		// resolve" — so they share one error.
		var commit *object.Commit
		if commit, err = repo.CommitObject(*hash); err == nil {
			return commit, nil
		}
	}
	return nil, fmt.Errorf("resolve %s: %w", rev, err)
}
