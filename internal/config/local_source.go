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
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/sys/execabs"

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

// GitRunner is the seam over the git operations the local source needs. The real
// implementation shells out to git; tests inject a fake so bootstrap and ref
// resolution run with no real git binary, repo, or network.
//
// Run executes a git subcommand in dir and returns trimmed stdout. IsRepo
// reports whether dir is the top of a git work tree. Init initializes a repo at
// dir. Keeping these three apart (rather than one opaque Run) lets a fake model
// "not a repo yet" → "init" → "is a repo" without parsing git argv.
type GitRunner interface {
	// IsRepo reports whether dir is a git work tree.
	IsRepo(dir string) bool
	// Init initializes a new git repository rooted at dir.
	Init(dir string) error
	// Run executes `git <args...>` in dir and returns trimmed stdout.
	Run(dir string, args ...string) (string, error)
}

// execGitRunner is the production GitRunner: it shells out to the git binary.
type execGitRunner struct{}

// NewExecGitRunner returns the production GitRunner backed by the git binary.
func NewExecGitRunner() GitRunner { return execGitRunner{} }

func (execGitRunner) IsRepo(dir string) bool {
	out, err := runGit(dir, "rev-parse", "--is-inside-work-tree")
	return err == nil && out == "true"
}

func (execGitRunner) Init(dir string) error {
	if err := fsops.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("local source: mkdir %s: %w", dir, err)
	}
	_, err := runGit(dir, "init")
	return err
}

func (execGitRunner) Run(dir string, args ...string) (string, error) {
	return runGit(dir, args...)
}

// runGit invokes the git binary in dir and returns trimmed stdout, wrapping a
// non-zero exit with the captured stderr for a diagnosable error.
//
// It uses execabs (not os/exec) so "git" is resolved to an absolute path on
// PATH and a relative resolution from the current working directory is refused
// — the canonical mitigation for an untrusted-PATH lookup (S4036), matching
// internal/scoring/signal_git.go.
func runGit(dir string, args ...string) (string, error) {
	cmd := execabs.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", gitError(args, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// gitError formats a git invocation failure, surfacing stderr when the failure
// was a non-zero exit so the caller sees git's own diagnostic.
func gitError(args []string, err error) error {
	joined := strings.Join(args, " ")
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return fmt.Errorf("git %s: %w: %s", joined, err, strings.TrimSpace(string(exitErr.Stderr)))
	}
	return fmt.Errorf("git %s: %w", joined, err)
}

// LocalSource is the git-backed `local` source rooted at Root (the `~/.agents`
// repo). All git access goes through Git so the type is hermetic in tests.
type LocalSource struct {
	// Root is the absolute path of the local source repo (`~/.agents`).
	Root string
	// Git is the seam over git operations; never nil after NewLocalSource.
	Git GitRunner
}

// NewLocalSource builds a LocalSource for the repo rooted at root. A nil runner
// defaults to the production exec-backed runner, so callers that do not need a
// fake can pass nil.
func NewLocalSource(root string, runner GitRunner) *LocalSource {
	if runner == nil {
		runner = NewExecGitRunner()
	}
	return &LocalSource{Root: root, Git: runner}
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
	commit := s.headCommit()
	if s.isDirty() {
		return commit + dirtySuffix, nil
	}
	return commit, nil
}

// headCommit resolves HEAD to a full commit hash, falling back to emptyTreeRef
// for an unborn branch (a freshly-init'd repo with no commits).
func (s *LocalSource) headCommit() string {
	out, err := s.Git.Run(s.Root, "rev-parse", "HEAD")
	if err != nil || out == "" {
		return emptyTreeRef
	}
	return out
}

// isDirty reports whether the working tree or index has changes relative to
// HEAD. `git status --porcelain` prints one line per change, so any output means
// dirty; an error (e.g. unborn branch quirks) conservatively reports clean,
// because the commit ref alone is already a valid version.
func (s *LocalSource) isDirty() bool {
	out, err := s.Git.Run(s.Root, "status", "--porcelain")
	if err != nil {
		return false
	}
	return strings.TrimSpace(out) != ""
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
