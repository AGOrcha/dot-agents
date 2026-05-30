package config

// Local source auto-setup (config-distribution-model §7A.1).
//
// The `local` source is the git-backed `~/.agents` repo. Under the unified
// units model (§7A.2) a local-authored skill/agent/layer must version by ref
// exactly like any remote source: the lock records `local:<path>@<ref> ->
// digest` uniformly, where <ref> is the local repo's resolved git commit (plus
// a working-tree dirty marker per §7A.4, so uncommitted authoring is caught).
//
// This file owns the local-source API and its first-resolve bootstrap, behind a
// GitRunner seam so the logic is hermetic and testable with no real git or
// network. It deliberately does NOT wire into resolver.go or any command — later
// tasks (resolver offline-read seam, source routing, gitignore auto-fill)
// consume the API defined here.
//
// "One asset dir, provenance-gitignored" (§7A.5): the local source has a single
// asset directory; remote-materialized units are gitignored from it so
// `da sync` never commits fetched assets. Provenance is therefore the unit's
// source: git-tracked ⇔ local-authored, gitignored ⇔ remote-materialized.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	git "github.com/go-git/go-git/v6"

	"github.com/NikashPrakash/dot-agents/internal/fsops"
)

// localSourceID is the source id the local git-backed `~/.agents` repo resolves
// under. Refs are addressed `local:<path>@<version>` (§7A.1 / §5).
const localSourceID = "local"

// pathSeparator / versionSeparator join the source id, path, and resolved
// version in a lock key / unit ref: "<source>:<path>@<version>" (§7A.3).
const (
	pathSeparator    = ":"
	versionSeparator = "@"
)

// errEmptyRoot is the message used when a LocalSource is missing its Root path;
// both bootstrap and gitignore paths guard on it, so it is named once.
const errEmptyRoot = "local source: empty root"

// dirtySuffix is appended to a resolved commit when the local source's working
// tree has uncommitted changes (§7A.4: the `local` content key is the resolved
// commit PLUS working-tree content, so authoring-in-progress is a distinct
// version and re-triggers a re-check).
const dirtySuffix = "-dirty"

// emptyTreeRef is the version recorded for a freshly-initialized local repo that
// has no commits yet (git rev-parse HEAD fails on an unborn branch). It still
// versions deterministically so a local unit authored before the first commit
// carries a real, stable ref rather than an empty string.
const emptyTreeRef = "0000000000000000000000000000000000000000"

// gitignoreBlockBegin / gitignoreBlockEnd delimit the idempotent da-owned block
// in the local source's `.gitignore`. Everything between the markers is
// regenerated on each EnsureProvenanceGitignore call; content outside the
// markers (user-authored ignores) is preserved verbatim.
const (
	gitignoreBlockBegin = "# >>> dot-agents managed (local source provenance) >>>"
	gitignoreBlockEnd   = "# <<< dot-agents managed (local source provenance) <<<"
	gitignoreFileName   = ".gitignore"
)

// GitRepo is the seam over the git operations the local source needs. The
// production implementation is in-process via go-git (no `git` subprocess, no
// PATH lookup — the repo-wide policy since the config fetcher rewrite); tests
// inject a fake so bootstrap and ref resolution run with no real repo or
// network.
//
// IsRepo reports whether dir is a git work tree. Init initializes a repo at dir.
// Resolve returns the HEAD commit hash plus whether the working tree is dirty;
// an unborn branch (freshly-init'd, no commits) is reported as an empty commit
// with no error so the caller can fall back to a deterministic empty-tree ref.
type GitRepo interface {
	// IsRepo reports whether dir is a git work tree.
	IsRepo(dir string) bool
	// Init initializes a new git repository rooted at dir.
	Init(dir string) error
	// Resolve returns the HEAD commit hash and working-tree dirtiness for the
	// repo at dir. An unborn branch yields ("", dirty, nil).
	Resolve(dir string) (commit string, dirty bool, err error)
}

// goGitRepo is the production GitRepo: every operation runs in-process via
// go-git, so there is no exec, no $PATH, and no command-injection surface.
type goGitRepo struct{}

// NewGoGitRepo returns the production GitRepo backed by the in-process go-git
// library.
func NewGoGitRepo() GitRepo { return goGitRepo{} }

func (goGitRepo) IsRepo(dir string) bool {
	_, err := git.PlainOpen(dir)
	return err == nil
}

func (goGitRepo) Init(dir string) error {
	if err := fsops.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("local source: mkdir %s: %w", dir, err)
	}
	if _, err := git.PlainInit(dir, false); err != nil {
		return fmt.Errorf("local source: git init %s: %w", dir, err)
	}
	return nil
}

func (goGitRepo) Resolve(dir string) (string, bool, error) {
	repo, err := git.PlainOpen(dir)
	if err != nil {
		return "", false, fmt.Errorf("local source: open %s: %w", dir, err)
	}
	return headCommitHash(repo), worktreeDirty(repo), nil
}

// headCommitHash returns HEAD's full commit hash, or "" when there is no
// resolvable commit yet — an unborn branch on a freshly-init'd repo, which
// go-git surfaces either as a Head error or as an all-zero reference. Both map
// to "" so the caller falls back to the deterministic empty-tree ref (§7A.1);
// the version is never an empty string.
func headCommitHash(repo *git.Repository) string {
	head, err := repo.Head()
	if err != nil {
		return ""
	}
	if hash := head.Hash(); !hash.IsZero() {
		return hash.String()
	}
	return ""
}

// worktreeDirty reports whether the repo's working tree has uncommitted
// changes. An error obtaining the worktree status conservatively reports clean,
// because the commit ref alone is already a valid version (§7A.4).
func worktreeDirty(repo *git.Repository) bool {
	st, err := worktreeStatus(repo)
	if err != nil {
		return false
	}
	return !st.IsClean()
}

// worktreeStatus returns the repo's working-tree status, folding the two
// fallible go-git steps (open worktree, compute status) into one error path so
// worktreeDirty has a single, testable failure branch.
func worktreeStatus(repo *git.Repository) (git.Status, error) {
	wt, err := repo.Worktree()
	if err != nil {
		return nil, err
	}
	return wt.Status()
}

// LocalSource is the git-backed `local` source rooted at Root (the `~/.agents`
// repo). All git access goes through Git so the type is hermetic in tests.
type LocalSource struct {
	// Root is the absolute path of the local source repo (`~/.agents`).
	Root string
	// Git is the seam over git operations; never nil after NewLocalSource.
	Git GitRepo
}

// NewLocalSource builds a LocalSource for the repo rooted at root. A nil repo
// defaults to the production in-process go-git implementation, so callers that
// do not need a fake can pass nil.
func NewLocalSource(root string, repo GitRepo) *LocalSource {
	if repo == nil {
		repo = NewGoGitRepo()
	}
	return &LocalSource{Root: root, Git: repo}
}

// EnsureBootstrapped makes the local source resolvable on first resolve / init
// (§7A.1): if Root is not yet a git work tree, it is git-initialized so any
// local-authored unit can version by the repo's ref. It is idempotent — an
// already-initialized repo is left untouched — and reports whether it performed
// an init, so a caller can log first-time setup.
func (s *LocalSource) EnsureBootstrapped() (initialized bool, err error) {
	if s.Root == "" {
		return false, errors.New(errEmptyRoot)
	}
	if s.Git.IsRepo(s.Root) {
		return false, nil
	}
	if err := s.Git.Init(s.Root); err != nil {
		return false, err
	}
	return true, nil
}

// ResolvedRef returns the local source's resolved version: the current commit,
// suffixed with dirtySuffix when the working tree has uncommitted changes
// (§7A.4). A repo with no commits yet resolves to emptyTreeRef (still
// deterministic), optionally dirty when the tree already has staged/working
// content. The repo must be bootstrapped first; a non-repo Root is an error.
func (s *LocalSource) ResolvedRef() (string, error) {
	if !s.Git.IsRepo(s.Root) {
		return "", fmt.Errorf("local source: %s is not a git repository (call EnsureBootstrapped)", s.Root)
	}
	commit, dirty, err := s.Git.Resolve(s.Root)
	if err != nil {
		return "", err
	}
	if commit == "" {
		commit = emptyTreeRef
	}
	if dirty {
		return commit + dirtySuffix, nil
	}
	return commit, nil
}

// LockKey returns the uniform unit ref / lock key for a unit at unitPath within
// the local source: "local:<rel-path>@<resolved-ref>" (§7A.3). unitPath may be
// absolute (it is made relative to Root) or already repo-relative. The path is
// always slash-normalized so the key is stable across OSes. The local source
// must be bootstrapped first.
func (s *LocalSource) LockKey(unitPath string) (string, error) {
	ref, err := s.ResolvedRef()
	if err != nil {
		return "", err
	}
	rel := s.relPath(unitPath)
	return localSourceID + pathSeparator + rel + versionSeparator + ref, nil
}

// relPath normalizes unitPath to a slash-delimited path relative to Root. A
// path that resolves under Root is made relative; one already relative is just
// slash-normalized; one that resolves outside Root is returned cleaned as given
// (the caller passed a deliberately external path).
//
// It is separator-agnostic: both inputs are slash-normalized before the
// containment check and the result is slash-joined, so a lock key is byte-for-
// byte identical on POSIX and Windows regardless of which separator the caller
// used. filepath.Rel handles OS-native absolute paths (drive letters/volumes);
// the slash-prefix fallback handles POSIX-style paths that are not "absolute"
// under Windows' filepath but still sit under a slash-style Root.
func (s *LocalSource) relPath(unitPath string) string {
	slashPath := filepath.ToSlash(filepath.Clean(unitPath))
	slashRoot := filepath.ToSlash(filepath.Clean(s.Root))
	if rel, ok := underRoot(slashRoot, slashPath); ok {
		return rel
	}
	return slashPath
}

// underRoot reports whether slashPath sits under slashRoot and, if so, returns
// the slash-delimited path relative to root. Both arguments must already be
// slash-normalized and cleaned. The comparison is purely lexical (no filesystem
// access) so it behaves identically on every OS.
func underRoot(slashRoot, slashPath string) (string, bool) {
	if slashRoot == "" || slashRoot == "." {
		return "", false
	}
	if slashPath == slashRoot {
		return ".", true
	}
	prefix := strings.TrimSuffix(slashRoot, "/") + "/"
	if rel := strings.TrimPrefix(slashPath, prefix); rel != slashPath {
		return rel, true
	}
	return "", false
}

// EnsureProvenanceGitignore writes the idempotent da-owned block into the local
// source's `.gitignore` so the given remote-materialized asset paths are
// excluded from the local repo's git tracking (§7A.5: "one asset dir,
// provenance-gitignored"). After this, `da sync` never commits fetched assets,
// and provenance is readable from git state alone: a tracked unit is
// local-authored, a gitignored one is remote-materialized.
//
// It preserves any user-authored content outside the managed markers, sorts and
// de-duplicates the managed paths for a stable diff, and is a no-op-stable
// rewrite (calling it twice with the same inputs yields the same bytes). An
// empty path set removes the managed block entirely.
func (s *LocalSource) EnsureProvenanceGitignore(remotePaths []string) error {
	if s.Root == "" {
		return errors.New(errEmptyRoot)
	}
	path := filepath.Join(s.Root, gitignoreFileName)
	existing, err := readGitignore(path)
	if err != nil {
		return err
	}
	outside := stripManagedBlock(existing)
	next := joinGitignore(outside, managedBlock(remotePaths))
	return fsops.WriteFileAtomic(path, []byte(next))
}

// readGitignore reads the .gitignore at path, treating a missing file as empty.
func readGitignore(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("local source: read %s: %w", path, err)
	}
	return string(data), nil
}

// stripManagedBlock returns content with the da-owned block (markers inclusive)
// removed, leaving user-authored lines intact. Content with no managed block is
// returned unchanged.
func stripManagedBlock(content string) string {
	lines := splitLines(content)
	kept := make([]string, 0, len(lines))
	inBlock := false
	for _, line := range lines {
		switch {
		case line == gitignoreBlockBegin:
			inBlock = true
		case line == gitignoreBlockEnd:
			inBlock = false
		case !inBlock:
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

// managedBlock renders the da-owned block for the given remote asset paths. An
// empty path set yields an empty string so the block is omitted entirely.
func managedBlock(remotePaths []string) string {
	normalized := normalizePaths(remotePaths)
	if len(normalized) == 0 {
		return ""
	}
	lines := make([]string, 0, len(normalized)+2)
	lines = append(lines, gitignoreBlockBegin)
	lines = append(lines, normalized...)
	lines = append(lines, gitignoreBlockEnd)
	return strings.Join(lines, "\n")
}

// normalizePaths slash-normalizes, trims, de-duplicates, and sorts the paths so
// the managed block is deterministic regardless of caller ordering. Blank
// entries are dropped.
func normalizePaths(paths []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		norm := filepath.ToSlash(strings.TrimSpace(p))
		if norm == "" || seen[norm] {
			continue
		}
		seen[norm] = true
		out = append(out, norm)
	}
	sort.Strings(out)
	return out
}

// joinGitignore combines the preserved (outside-block) content with the managed
// block, collapsing leading/trailing blank noise and guaranteeing a single
// trailing newline. Either part may be empty.
func joinGitignore(outside, block string) string {
	outside = strings.TrimRight(outside, "\n")
	parts := make([]string, 0, 2)
	if strings.TrimSpace(outside) != "" {
		parts = append(parts, outside)
	}
	if block != "" {
		parts = append(parts, block)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n") + "\n"
}

// splitLines splits content into lines without a trailing empty element when the
// content ends in a newline, so round-tripping does not accrete blank lines.
func splitLines(content string) []string {
	if content == "" {
		return nil
	}
	return strings.Split(strings.TrimRight(content, "\n"), "\n")
}
