package codegraph

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/object"
)

// Changed-file discovery for the release's change-oriented tools.
//
// Upstream (code_review_graph/incremental.py) shells out to git twice:
//
//	get_changed_files(root, base)  -> git diff --name-status -z <base> --
//	get_staged_and_unstaged(root)  -> git status --porcelain=v1 -z --untracked-files=all
//
// Both are reproduced here in pure Go over go-git. That is not a stylistic
// preference: `tools/execguard` default-denies process execution in shipping
// Go, so a subprocess here could only ever exist as a tracked-debt record.
//
// The two helpers live in their own file, shared by every tool lane, because
// they are the ONLY changed-file discovery the native tool surface may use.
// In particular they are NOT interchangeable with engine.go's build-side git
// helpers, which diff `base...HEAD` and fall back to every tracked file: that
// is a different SET of files, so answering a tool payload from it would
// silently change which files the tool reports.
//
// Two semantics are load-bearing and easy to get wrong:
//
//  1. `git diff <base> --` compares base against the WORKING TREE, not
//     against HEAD. An uncommitted edit IS a change; a file edited after
//     base and then reverted is NOT, even though it appears in the commit
//     range. Both cases are pinned by the tests.
//  2. Untracked files are invisible to `git diff` but visible to
//     `git status --untracked-files=all`. That asymmetry is exactly why
//     upstream calls the second helper only when the first returns nothing.
//
// Upstream treats "git could not answer" as "nothing changed" rather than as
// a failure — an unresolvable ref, a non-zero exit, a spawn failure and a
// timeout all return an empty list. The helpers keep that, because a tool
// that errored there would break the release's contract for a repository
// whose only commit is its root commit.

// releaseSafeGitRef is upstream's `_SAFE_GIT_REF`. A ref that does not match
// it never reaches git.
var releaseSafeGitRef = regexp.MustCompile(`^[A-Za-z0-9_.~^/@{}\-]+$`)

// releaseGitTimeoutDefault is upstream's `_GIT_TIMEOUT` default, overridable
// through the same environment variable the release reads.
const releaseGitTimeoutDefault = 30 * time.Second

// ErrSubversionWorkingCopy reports a Subversion working copy, whose changed
// files upstream discovers with `svn status`.
//
// It is an explicit capability error rather than an empty list: silently
// reporting "nothing changed" for an SVN checkout would make every
// change-oriented tool return an empty, confidently wrong answer.
var ErrSubversionWorkingCopy = errors.New(
	"codegraph: changed-file discovery for Subversion working copies is not implemented natively")

// ReleaseChangedFiles returns the repository-relative paths that differ
// between base and the working tree — upstream's `get_changed_files`.
//
// A rename reports BOTH paths. Upstream switched to `--name-status` precisely
// so the old path reaches the incremental purge loop; without it a rename
// leaves the old path's nodes and edges in the graph and an incremental
// update diverges from a full rebuild. Rename DETECTION is irrelevant to this
// set: whether git pairs the paths as `R100 old new` or reports a separate
// delete and add, the resulting path set is identical.
//
// base is used exactly as given — no default is applied, because callers
// resolve it themselves (a stored build sha, "HEAD~1", or a full rebuild) and
// a second default here would silently override that decision.
func ReleaseChangedFiles(root, base string) ([]string, error) {
	if isSubversionWorkingCopy(root) {
		return nil, ErrSubversionWorkingCopy
	}
	// Upstream rejects a `-`-prefixed ref separately from the charset check,
	// because the charset itself allows `-`: without the extra guard "-rf"
	// would reach git as an option rather than as a revision.
	if strings.HasPrefix(base, "-") || !releaseSafeGitRef.MatchString(base) {
		return []string{}, nil
	}
	repo, err := openReleaseRepo(root)
	if err != nil {
		// `git diff` outside a repository exits non-zero, and so does the
		// `--cached` retry, so upstream returns an empty list.
		return []string{}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), releaseGitTimeout())
	defer cancel()

	baseTree, err := resolveReleaseTree(repo, base)
	if err != nil {
		// The unresolvable-base branch. Upstream retries the diff against the
		// index (`git diff --name-status -z --cached`) and returns an empty
		// list if that fails too. Answering "nothing changed" directly would
		// be wrong: an unresolvable base is a failure to DETERMINE, and the
		// staged set is what upstream substitutes for it.
		return releaseStagedFiles(repo)
	}

	candidates, err := releaseChangeCandidates(ctx, repo, baseTree)
	if err != nil {
		return nil, err
	}
	return releaseKeepDiffering(ctx, repo, root, baseTree, candidates)
}

// ReleaseWorkingTreeFiles returns every modified, staged and untracked
// repository-relative path — upstream's `get_staged_and_unstaged`.
//
// Upstream parses `git status --porcelain=v1 -z --untracked-files=all` and,
// for a rename or copy record, keeps only the DESTINATION path: the record
// that follows carries the source and is explicitly skipped. This reproduces
// that by pairing a staged delete with a staged add carrying the same blob.
func ReleaseWorkingTreeFiles(root string) ([]string, error) {
	if isSubversionWorkingCopy(root) {
		return nil, ErrSubversionWorkingCopy
	}
	repo, err := openReleaseRepo(root)
	if err != nil {
		return []string{}, nil
	}
	status, err := releaseStatus(repo)
	if err != nil {
		// `git status` failing is upstream's "return an empty list" branch.
		return []string{}, nil
	}
	renamedSources, err := releaseRenameSources(repo, status)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(status))
	for path, entry := range status {
		if entry.Staging == git.Unmodified && entry.Worktree == git.Unmodified {
			continue
		}
		if renamedSources[path] {
			continue
		}
		paths = append(paths, path)
	}
	return releaseSortedPaths(paths), nil
}

// releaseChangeCandidates collects every path that COULD differ between
// baseTree and the working tree: the paths the commits since base touched,
// plus the paths the index or working tree touched.
//
// It is deliberately a superset. Narrowing it to the real answer is
// releaseKeepDiffering's content comparison, and a superset plus an exact
// filter is both cheaper and far easier to reason about than trying to
// classify each path from status codes alone.
func releaseChangeCandidates(
	ctx context.Context, repo *git.Repository, baseTree *object.Tree,
) (map[string]bool, error) {
	candidates := map[string]bool{}
	if err := releaseCommitRangeCandidates(ctx, repo, baseTree, candidates); err != nil {
		return nil, err
	}
	if err := releaseStatusCandidates(repo, candidates); err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, nil
	}
	return candidates, nil
}

// releaseCommitRangeCandidates adds every path the commits between baseTree
// and HEAD touched.
func releaseCommitRangeCandidates(
	ctx context.Context, repo *git.Repository, baseTree *object.Tree, candidates map[string]bool,
) error {
	headTree, err := releaseHeadTree(repo)
	if err != nil {
		return err
	}
	if headTree == nil {
		// No HEAD, but base resolved. Every path in base is a candidate:
		// there is no commit history that could have left one unchanged.
		return releaseCollectTreePaths(baseTree, candidates)
	}
	// Rename detection is off: this diff exists only to NAME candidate
	// paths, and an undetected rename contributes its source and
	// destination separately, which is the pair we want anyway.
	changes, err := object.DiffTreeContext(ctx, baseTree, headTree)
	if err != nil {
		return err
	}
	for _, change := range changes {
		if change.From.Name != "" {
			candidates[change.From.Name] = true
		}
		if change.To.Name != "" {
			candidates[change.To.Name] = true
		}
	}
	return nil
}

// releaseStatusCandidates adds every path the index or the working tree
// touched, except the untracked ones: `git diff` never reports one, and that
// is the one difference between the two discovery helpers that callers
// depend on.
func releaseStatusCandidates(repo *git.Repository, candidates map[string]bool) error {
	status, err := releaseStatus(repo)
	if err != nil {
		return err
	}
	for path, entry := range status {
		if entry.Staging == git.Unmodified && entry.Worktree == git.Unmodified {
			continue
		}
		if entry.Staging == git.Untracked && entry.Worktree == git.Untracked {
			continue
		}
		candidates[path] = true
	}
	return nil
}

// releaseCollectTreePaths adds every blob path in tree to paths.
func releaseCollectTreePaths(tree *object.Tree, paths map[string]bool) error {
	files := tree.Files()
	defer files.Close()
	return files.ForEach(func(file *object.File) error {
		paths[file.Name] = true
		return nil
	})
}

// releaseKeepDiffering reduces the candidate set to the paths whose base
// state actually differs from the working tree.
//
// This step is what makes a change-then-revert correctly ABSENT: such a path
// appears in the commit range, so it is a candidate, but its worktree bytes
// equal the blob at base and git would not list it.
func releaseKeepDiffering(
	ctx context.Context,
	repo *git.Repository,
	root string,
	baseTree *object.Tree,
	candidates map[string]bool,
) ([]string, error) {
	tracksMode, err := releaseTracksFileMode(repo)
	if err != nil {
		return nil, err
	}
	changed := make([]string, 0, len(candidates))
	for path := range candidates {
		if ctx.Err() != nil {
			return []string{}, nil
		}
		baseEntry, baseExists, err := releaseTreeEntry(baseTree, path)
		if err != nil {
			return nil, err
		}
		workMode, workContent, workExists, err := releaseWorktreeBlob(root, path)
		if err != nil {
			return nil, err
		}
		switch {
		case !baseExists && !workExists:
			// Present in neither side: a path the commit range added and the
			// working tree removed again, or a stale status entry.
			continue
		case baseExists != workExists:
			changed = append(changed, path)
			continue
		}
		if tracksMode && baseEntry.Mode != workMode {
			changed = append(changed, path)
			continue
		}
		if !releaseBlobEquals(repo, baseEntry.Hash, workContent) {
			changed = append(changed, path)
		}
	}
	return releaseSortedPaths(changed), nil
}

// releaseStagedFiles is upstream's `git diff --name-status -z --cached`
// retry: the paths whose index state differs from HEAD.
func releaseStagedFiles(repo *git.Repository) ([]string, error) {
	status, err := releaseStatus(repo)
	if err != nil {
		return []string{}, nil
	}
	paths := make([]string, 0, len(status))
	for path, entry := range status {
		if entry.Staging == git.Unmodified || entry.Staging == git.Untracked {
			continue
		}
		paths = append(paths, path)
	}
	return releaseSortedPaths(paths), nil
}

// releaseRenameSources pairs a staged delete with a staged add carrying the
// same blob — the rename `git status` collapses into one `R` record, whose
// source path upstream skips.
//
// Only EXACT renames are paired. git also pairs a rename that was edited
// (any similarity at or above its 50% score), and doing that needs diffcore's
// similarity estimator: the same deferred work that keeps detect_changes_tool
// on the bridge. Until then a renamed-AND-edited file reports both of its
// paths instead of only the destination, which over-reports rather than
// losing a change.
func releaseRenameSources(repo *git.Repository, status git.Status) (map[string]bool, error) {
	headTree, err := releaseHeadTree(repo)
	if err != nil || headTree == nil {
		return nil, err
	}
	var deleted, added []string
	for path, entry := range status {
		switch entry.Staging {
		case git.Deleted:
			deleted = append(deleted, path)
		case git.Added:
			added = append(added, path)
		}
	}
	if len(deleted) == 0 || len(added) == 0 {
		return nil, nil
	}
	index, err := repo.Storer.Index()
	if err != nil {
		return nil, err
	}
	// Keyed on the HEX rendering, not on plumbing.Hash: the struct carries
	// an object-format field alongside the digest and go-git compares hashes
	// with Equal (digest only) for exactly that reason, so struct equality
	// could miss a match between two equal digests.
	addedHashes := make(map[string]bool, len(added))
	for _, path := range added {
		entry, err := index.Entry(path)
		if err != nil {
			continue
		}
		addedHashes[entry.Hash.String()] = true
	}
	sources := map[string]bool{}
	for _, path := range deleted {
		entry, exists, err := releaseTreeEntry(headTree, path)
		if err != nil {
			return nil, err
		}
		if exists && addedHashes[entry.Hash.String()] {
			sources[path] = true
		}
	}
	return sources, nil
}

// releaseStatus reads the working-tree status. `Preload` reads the index once
// up front, which is what makes the whole-tree walk affordable.
func releaseStatus(repo *git.Repository) (git.Status, error) {
	worktree, err := repo.Worktree()
	if err != nil {
		return nil, err
	}
	return worktree.StatusWithOptions(git.StatusOptions{Strategy: git.Preload})
}

// releaseHeadTree returns HEAD's tree, or (nil, nil) in a repository with no
// commits yet.
func releaseHeadTree(repo *git.Repository) (*object.Tree, error) {
	head, err := repo.Head()
	if err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			return nil, nil
		}
		return nil, err
	}
	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		return nil, err
	}
	return commit.Tree()
}

// resolveReleaseTree resolves a revision to the tree it names.
func resolveReleaseTree(repo *git.Repository, revision string) (*object.Tree, error) {
	hash, err := repo.ResolveRevision(plumbing.Revision(revision))
	if err != nil {
		return nil, err
	}
	commit, err := repo.CommitObject(*hash)
	if err != nil {
		return nil, err
	}
	return commit.Tree()
}

// releaseTreeEntry looks one path up in a tree, distinguishing "absent" from
// a read failure. A path that is a DIRECTORY in the tree reports absent: git
// compares blobs, and a tree there means no file exists at that path.
func releaseTreeEntry(tree *object.Tree, path string) (object.TreeEntry, bool, error) {
	entry, err := tree.FindEntry(path)
	switch {
	case errors.Is(err, object.ErrEntryNotFound),
		errors.Is(err, object.ErrDirectoryNotFound),
		errors.Is(err, object.ErrFileNotFound):
		return object.TreeEntry{}, false, nil
	case err != nil:
		return object.TreeEntry{}, false, err
	}
	if entry.Mode == filemode.Dir {
		return object.TreeEntry{}, false, nil
	}
	return *entry, true, nil
}

// releaseWorktreeBlob reads one working-tree path the way git would stage it:
// a file's bytes, or a symlink's target. A directory or a missing path
// reports absent, because neither can be the blob side of a diff.
func releaseWorktreeBlob(root, path string) (filemode.FileMode, []byte, bool, error) {
	absolute := filepath.Join(root, filepath.FromSlash(path))
	info, err := os.Lstat(absolute)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return filemode.Empty, nil, false, nil
	case err != nil:
		return filemode.Empty, nil, false, err
	}
	mode, err := filemode.NewFromOSFileMode(info.Mode())
	if err != nil {
		return filemode.Empty, nil, false, err
	}
	switch mode {
	case filemode.Dir:
		return filemode.Empty, nil, false, nil
	case filemode.Symlink:
		target, err := os.Readlink(absolute)
		if err != nil {
			return filemode.Empty, nil, false, err
		}
		return mode, []byte(target), true, nil
	}
	content, err := os.ReadFile(absolute)
	if err != nil {
		return filemode.Empty, nil, false, err
	}
	return mode, content, true, nil
}

// releaseBlobEquals reports whether a stored blob holds exactly content.
//
// The size is compared first, so an edit that changes a file's length costs
// one object header read instead of a full decode. A blob that cannot be read
// counts as DIFFERENT: git would fail the diff, and claiming the two sides
// are equal would silently drop a real change.
func releaseBlobEquals(repo *git.Repository, hash plumbing.Hash, content []byte) bool {
	blob, err := repo.BlobObject(hash)
	if err != nil {
		return false
	}
	if blob.Size != int64(len(content)) {
		return false
	}
	reader, err := blob.Reader()
	if err != nil {
		return false
	}
	defer reader.Close()
	stored, err := io.ReadAll(reader)
	if err != nil {
		return false
	}
	return bytes.Equal(stored, content)
}

// releaseTracksFileMode reports whether this repository records the
// executable bit, mirroring git's `core.fileMode`. git writes the probed
// value at init, and when it is false git ignores a mode-only difference — so
// comparing modes unconditionally would invent changes on a filesystem that
// cannot store them.
func releaseTracksFileMode(repo *git.Repository) (bool, error) {
	config, err := repo.Config()
	if err != nil {
		return false, err
	}
	if config.Raw == nil {
		return true, nil
	}
	value := config.Raw.Section("core").Option("filemode")
	if value == "" {
		return true, nil
	}
	tracks, err := strconv.ParseBool(value)
	if err != nil {
		return true, nil
	}
	return tracks, nil
}

// openReleaseRepo opens the repository containing root. The ancestor walk
// matches running git with `cwd=root`, and the paths it yields stay
// repository-relative exactly like git's own output.
func openReleaseRepo(root string) (*git.Repository, error) {
	return git.PlainOpenWithOptions(root, &git.PlainOpenOptions{DetectDotGit: true})
}

// isSubversionWorkingCopy mirrors upstream's `detect_vcs`, which tests for a
// marker directory at the root it was handed rather than searching upward,
// and which prefers git when both markers are present.
func isSubversionWorkingCopy(root string) bool {
	if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
		return false
	}
	_, err := os.Stat(filepath.Join(root, ".svn"))
	return err == nil
}

// releaseGitTimeout reads upstream's `CRG_GIT_TIMEOUT`, in seconds.
func releaseGitTimeout() time.Duration {
	raw := os.Getenv("CRG_GIT_TIMEOUT")
	if raw == "" {
		return releaseGitTimeoutDefault
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		return releaseGitTimeoutDefault
	}
	return time.Duration(seconds) * time.Second
}

// releaseSortedPaths returns paths deduplicated, slash-separated and ordered.
// git prints its diff and status output deterministically; sorting gives
// callers the same property without depending on map iteration order.
func releaseSortedPaths(paths []string) []string {
	unique := make([]string, 0, len(paths))
	seen := make(map[string]bool, len(paths))
	for _, path := range paths {
		normalized := filepath.ToSlash(path)
		if seen[normalized] {
			continue
		}
		seen[normalized] = true
		unique = append(unique, normalized)
	}
	sort.Strings(unique)
	return unique
}
