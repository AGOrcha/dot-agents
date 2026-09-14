package codegraph

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/storage"
)

// Changed-file discovery, part two: the cases tools_git_test.go's four
// happy-path parity tests do not reach.
//
// tools_git_test.go oracles the COMMON set against real git. This file pins
// the rest of the contract — the shapes a repository can take that are not a
// clean add/edit/delete (a mode-only change, a symlink, a nested path, an
// unborn HEAD), and the failure modes each helper documents a specific answer
// for. The distinction that matters throughout: a helper either reports a
// path SET or refuses; it never invents a confidently wrong empty answer.
//
// Where the expected set is git's own, the expectation is taken from the
// oracle helpers in tools_git_test.go rather than hand-written, for the same
// reason stated there.

// ── fixtures ─────────────────────────────────────────────────────────────────

// covGitRepo initialises a repository with git's background maintenance off.
// `git commit` otherwise spawns a detached `git maintenance run --auto` that
// keeps creating and unlinking paths under .git after the commit returns,
// which races the fixture's own TempDir teardown.
func covGitRepo(t *testing.T) string {
	t.Helper()
	root := gitOracleRepo(t)
	gitOracleRun(t, root, "config", "maintenance.auto", "false")
	gitOracleRun(t, root, "config", "gc.auto", "0")
	return root
}

// covGitCommitAll stages everything and commits it.
func covGitCommitAll(t *testing.T, root, message string) {
	t.Helper()
	gitOracleRun(t, root, "add", "-A")
	gitOracleRun(t, root, "commit", "-m", message)
}

// covGitOneCommitRepo is the smallest repository with a resolvable HEAD.
func covGitOneCommitRepo(t *testing.T) string {
	t.Helper()
	root := covGitRepo(t)
	gitOracleWrite(t, root, "keep.go", "package p\n")
	covGitCommitAll(t, root, "root")
	return root
}

// covGitOpen opens a fixture repository the way the helpers under test do.
func covGitOpen(t *testing.T, root string) *git.Repository {
	t.Helper()
	repo, err := openReleaseRepo(root)
	if err != nil {
		t.Fatalf("open %s: %v", root, err)
	}
	return repo
}

// covGitTreeAt resolves a revision's tree, failing the test if it cannot.
func covGitTreeAt(t *testing.T, repo *git.Repository, revision string) *object.Tree {
	t.Helper()
	tree, err := resolveReleaseTree(repo, revision)
	if err != nil {
		t.Fatalf("resolve %s: %v", revision, err)
	}
	return tree
}

// covGitCancelled returns a context that is already past its deadline, which
// is how the tests drive the abandon-early arms without racing a timeout.
func covGitCancelled() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

// covGitLoopHeadRepo points HEAD at a self-referential symbolic ref. Reading
// HEAD then fails with a resolution error rather than "no commits yet", which
// is the difference the helpers branch on.
func covGitLoopHeadRepo(t *testing.T) string {
	t.Helper()
	root := covGitOneCommitRepo(t)
	covGitWriteRaw(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/loop\n")
	covGitWriteRaw(t, filepath.Join(root, ".git", "refs", "heads", "loop"), "ref: refs/heads/loop\n")
	return root
}

// covGitWriteRaw writes a repository-metadata file verbatim.
func covGitWriteRaw(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// covGitWorktreelessRepo reopens a fixture without a working tree, which is
// what a bare repository looks like to every helper that asks for status.
func covGitWorktreelessRepo(t *testing.T, root string) *git.Repository {
	t.Helper()
	repo, err := git.Open(covGitOpen(t, root).Storer, nil)
	if err != nil {
		t.Fatalf("open worktree-less: %v", err)
	}
	return repo
}

// ── storer fault injection ───────────────────────────────────────────────────

// covGitErrStoreClosed stands in for an object database that stops answering.
var covGitErrStoreClosed = errors.New("covGit: object database closed")

// covGitRationedStorer serves one object hash a fixed number of times and
// then refuses, so a two-step lookup can be failed on its second step.
type covGitRationedStorer struct {
	storage.Storer
	hash   string
	budget int
}

func (s *covGitRationedStorer) EncodedObject(
	kind plumbing.ObjectType, hash plumbing.Hash,
) (plumbing.EncodedObject, error) {
	if hash.String() == s.hash {
		if s.budget <= 0 {
			return nil, covGitErrStoreClosed
		}
		s.budget--
	}
	return s.Storer.EncodedObject(kind, hash)
}

// covGitUnreadableBlobStorer hands out one object whose content stream is
// broken: either it cannot be opened, or it fails partway through.
type covGitUnreadableBlobStorer struct {
	storage.Storer
	hash    string
	openErr bool
}

func (s covGitUnreadableBlobStorer) EncodedObject(
	kind plumbing.ObjectType, hash plumbing.Hash,
) (plumbing.EncodedObject, error) {
	stored, err := s.Storer.EncodedObject(kind, hash)
	if err != nil || hash.String() != s.hash {
		return stored, err
	}
	return covGitBrokenObject{EncodedObject: stored, openErr: s.openErr}, nil
}

// covGitBrokenObject keeps the real object's type and size — the size is what
// releaseBlobEquals screens on before it ever opens the content.
type covGitBrokenObject struct {
	plumbing.EncodedObject
	openErr bool
}

func (o covGitBrokenObject) Reader() (io.ReadCloser, error) {
	if o.openErr {
		return nil, covGitErrStoreClosed
	}
	return io.NopCloser(iotest.ErrReader(covGitErrStoreClosed)), nil
}

// covGitRawlessConfigStorer reports a configuration with no raw sections at
// all, which is how a storer that does not keep git's config file answers.
type covGitRawlessConfigStorer struct {
	storage.Storer
}

func (covGitRawlessConfigStorer) Config() (*config.Config, error) {
	return &config.Config{}, nil
}

// ── platform probes ──────────────────────────────────────────────────────────

// covGitDenyRead drops read permission and reports whether the platform
// enforced it. Windows' Chmod only toggles the read-only bit and a root user
// bypasses the mode entirely, so the caller asserts the contract that
// actually applies instead of failing on a filesystem that cannot express it.
func covGitDenyRead(t *testing.T, path string) bool {
	t.Helper()
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("chmod %s: %v", path, err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	_, err := os.ReadFile(path)
	return err != nil
}

// covGitSymlink creates a symlink and reports whether the platform allowed
// it; Windows needs Developer Mode or elevation.
func covGitSymlink(target, link string) bool {
	return os.Symlink(target, link) == nil
}

// covGitLink points name at target, falling back to a regular file holding
// the target's name where symlinks cannot be created. Either way the fixture
// has a path whose content changes when it is repointed, which is what the
// changed-path set is being asserted on.
func covGitLink(t *testing.T, root, name, target string) {
	t.Helper()
	if !covGitSymlink(target, filepath.Join(root, filepath.FromSlash(name))) {
		gitOracleWrite(t, root, name, target)
	}
}

// covGitDeviceFile names a character device that exists on the running
// platform, plus whether the OS really reports it as neither file, directory
// nor symlink — the kinds git has no mode for.
func covGitDeviceFile(t *testing.T) (root, name string, irregular bool) {
	t.Helper()
	root, name = "/dev", "null"
	if runtime.GOOS == "windows" {
		root, name = ".", "NUL"
	}
	info, err := os.Lstat(filepath.Join(root, name))
	if err != nil {
		return root, name, false
	}
	switch info.Mode().Type() {
	case 0, os.ModeDir, os.ModeSymlink:
		return root, name, false
	}
	return root, name, true
}

// ── the changed-path set ─────────────────────────────────────────────────────

// TestCovGitChangedFilesCoversTheAwkwardShapes extends the parity oracle over
// the working-tree shapes a plain add/edit/delete fixture never produces: a
// mode-only change, a symlink whose target moved, a rename into a nested
// directory, a staged-but-uncommitted add, and an untracked file.
//
// The set is what the incremental purge loop consumes, so a path that is
// missing here silently leaves stale nodes in the graph and a path that is
// present but should not be forces a needless reparse.
func TestCovGitChangedFilesCoversTheAwkwardShapes(t *testing.T) {
	root := covGitAwkwardRepo(t)

	want := gitOracleChangedFiles(t, root, "HEAD~1")
	got, err := ReleaseChangedFiles(root, "HEAD~1")
	if err != nil {
		t.Fatalf("ReleaseChangedFiles: %v", err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("want %v (git), got %v", want, got)
	}

	// Both halves of the rename must reach the purge loop, and the nested
	// path must survive as a slash-separated repository-relative path.
	for _, path := range []string{
		"moved.go", "renamed/moved.go", "nested/dir.go", "link.go", "staged.go",
	} {
		if !gitOracleHas(got, path) {
			t.Errorf("want %s reported as changed, got %v", path, got)
		}
	}
	// An untracked file is invisible to `git diff`; that asymmetry is the
	// whole reason ReleaseWorkingTreeFiles exists as a separate helper.
	for _, path := range []string{"same.go", "untracked.go"} {
		if gitOracleHas(got, path) {
			t.Errorf("want %s absent (git does not list it), got %v", path, got)
		}
	}
	covGitAssertModeOnlyChange(t, root, got)
}

// covGitAwkwardRepo builds the fixture the awkward-shape tests share.
func covGitAwkwardRepo(t *testing.T) string {
	t.Helper()
	root := covGitRepo(t)
	gitOracleWrite(t, root, "moved.go", "package p\n\nfunc Moved() {}\n")
	gitOracleWrite(t, root, "nested/dir.go", "package p\n")
	gitOracleWrite(t, root, "mode.go", "package p\n")
	gitOracleWrite(t, root, "same.go", "package p\n")
	gitOracleWrite(t, root, "target.go", "package p\n")
	gitOracleWrite(t, root, "other.go", "package p\n")
	covGitLink(t, root, "link.go", "target.go")
	covGitCommitAll(t, root, "baseline")

	if err := os.MkdirAll(filepath.Join(root, "renamed"), 0o750); err != nil {
		t.Fatalf("mkdir renamed: %v", err)
	}
	gitOracleRun(t, root, "mv", "moved.go", "renamed/moved.go")
	gitOracleWrite(t, root, "nested/dir.go", "package p\n\nfunc Dir() {}\n")
	covGitCommitAll(t, root, "churn")

	// Working tree: a mode-only change, a retargeted symlink, a rewrite with
	// identical bytes, a staged add and an untracked file.
	covGitChmodExecutable(t, filepath.Join(root, "mode.go"))
	covGitRetarget(t, root, "link.go", "other.go")
	gitOracleWrite(t, root, "same.go", "package p\n")
	gitOracleWrite(t, root, "staged.go", "package p\n")
	gitOracleRun(t, root, "add", "staged.go")
	gitOracleWrite(t, root, "untracked.go", "package p\n")
	return root
}

// covGitChmodExecutable sets the executable bit, which only some filesystems
// can store — the caller oracles the consequence against git.
func covGitChmodExecutable(t *testing.T, path string) {
	t.Helper()
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatalf("chmod %s: %v", path, err)
	}
}

// covGitRetarget repoints an existing link at a new target.
func covGitRetarget(t *testing.T, root, name, target string) {
	t.Helper()
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(name))); err != nil {
		t.Fatalf("remove %s: %v", name, err)
	}
	covGitLink(t, root, name, target)
}

// covGitAssertModeOnlyChange pins the executable-bit case against git's own
// answer: git records whether the filesystem can store the bit, and ignores a
// mode difference when it cannot, so the expectation has to come from git.
func covGitAssertModeOnlyChange(t *testing.T, root string, got []string) {
	t.Helper()
	want := gitOracleHas(gitOracleChangedFiles(t, root, "HEAD"), "mode.go")
	if gitOracleHas(got, "mode.go") != want {
		t.Errorf("mode-only change: want reported=%v (git), got %v", want, got)
	}
}

// TestCovGitWorkingTreeFilesReportsNestedAndUntrackedPaths pins the
// fallback helper over the same awkward shapes: it is the set upstream
// substitutes when the diff finds nothing, so an untracked file inside a
// nested directory has to survive it.
func TestCovGitWorkingTreeFilesReportsNestedAndUntrackedPaths(t *testing.T) {
	root := covGitRepo(t)
	gitOracleWrite(t, root, "nested/keep.go", "package p\n")
	covGitCommitAll(t, root, "baseline")
	gitOracleWrite(t, root, "nested/fresh.go", "package p\n")
	gitOracleWrite(t, root, "nested/keep.go", "package p\n\nfunc Keep() {}\n")

	want := gitOracleWorkingTreeFiles(t, root)
	got, err := ReleaseWorkingTreeFiles(root)
	if err != nil {
		t.Fatalf("ReleaseWorkingTreeFiles: %v", err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("want %v (git), got %v", want, got)
	}
	for _, path := range []string{"nested/fresh.go", "nested/keep.go"} {
		if !gitOracleHas(got, path) {
			t.Errorf("want %s reported, got %v", path, got)
		}
	}
}

// TestCovGitChangedFilesEmptyRepository covers a repository with no commits
// at all: no revision resolves, so the answer is upstream's `--cached` retry
// over the index rather than an error.
func TestCovGitChangedFilesEmptyRepository(t *testing.T) {
	root := covGitRepo(t)
	gitOracleWrite(t, root, "staged.go", "package p\n")
	gitOracleRun(t, root, "add", "staged.go")
	gitOracleWrite(t, root, "untracked.go", "package p\n")

	got, err := ReleaseChangedFiles(root, "HEAD")
	if err != nil {
		t.Fatalf("ReleaseChangedFiles: %v", err)
	}
	if !reflect.DeepEqual([]string{"staged.go"}, got) {
		t.Fatalf("want the staged path from the index retry, got %v", got)
	}
}

// TestCovGitChangedFilesMissingPath covers a root that does not exist at all,
// which cannot be opened and so reports nothing changed rather than failing.
func TestCovGitChangedFilesMissingPath(t *testing.T) {
	got, err := ReleaseChangedFiles(filepath.Join(t.TempDir(), "absent"), "HEAD")
	if err != nil {
		t.Fatalf("ReleaseChangedFiles: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want an empty list for a path outside a repository, got %v", got)
	}
}

// TestCovGitChangedFilesUnbornHead covers a base that resolves in a
// repository whose HEAD does not: after `git checkout --orphan` there is no
// commit history that could have left a path unchanged, so every path in base
// is a candidate.
func TestCovGitChangedFilesUnbornHead(t *testing.T) {
	root, repo := covGitUnbornHeadRepo(t)

	candidates, err := releaseChangeCandidates(
		context.Background(), repo, covGitTreeAt(t, repo, "main"))
	if err != nil {
		t.Fatalf("releaseChangeCandidates: %v", err)
	}
	want := map[string]bool{"a.go": true, "nested/b.go": true}
	if !reflect.DeepEqual(want, candidates) {
		t.Fatalf("want every base path as a candidate %v, got %v", want, candidates)
	}

	// The candidates are all still present and unchanged on disk, so the
	// filtered answer is empty even though the candidate set is not.
	changed, err := ReleaseChangedFiles(root, "main")
	if err != nil {
		t.Fatalf("ReleaseChangedFiles: %v", err)
	}
	if len(changed) != 0 {
		t.Fatalf("want no changes against an identical worktree, got %v", changed)
	}
}

// covGitUnbornHeadRepo commits on main and then checks out an orphan branch,
// leaving HEAD pointing at a branch that does not exist yet.
func covGitUnbornHeadRepo(t *testing.T) (string, *git.Repository) {
	t.Helper()
	root := covGitRepo(t)
	gitOracleWrite(t, root, "a.go", "package p\n")
	gitOracleWrite(t, root, "nested/b.go", "package p\n")
	covGitCommitAll(t, root, "one")
	gitOracleRun(t, root, "checkout", "-q", "--orphan", "blank")
	return root, covGitOpen(t, root)
}

// TestCovGitChangeCandidatesAbandonsACancelledWalk pins the two places the
// candidate walk can run out of time. Both answer "no candidates" rather than
// a half-collected set: a partial set would make the caller purge a subset of
// the graph and call the result an incremental update.
func TestCovGitChangeCandidatesAbandonsACancelledWalk(t *testing.T) {
	t.Run("duringTheCommitRangeDiff", func(t *testing.T) {
		repo := covGitOpen(t, covGitOneCommitRepo(t))
		got, err := releaseChangeCandidates(
			covGitCancelled(), repo, covGitTreeAt(t, repo, "HEAD"))
		if err == nil {
			t.Fatalf("want the cancellation surfaced, got %v", got)
		}
	})

	t.Run("afterTheStatusWalk", func(t *testing.T) {
		_, repo := covGitUnbornHeadRepo(t)
		got, err := releaseChangeCandidates(
			covGitCancelled(), repo, covGitTreeAt(t, repo, "main"))
		if err != nil {
			t.Fatalf("releaseChangeCandidates: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("want no candidates from an abandoned walk, got %v", got)
		}
	})
}

// TestCovGitChangeCandidatesSurfacesAnUnreadableBaseTree covers a base tree
// one of whose blob objects is gone. Upstream's "git could not answer" rule
// covers a ref that will not resolve, not a repository that cannot be read:
// swallowing this would report a corrupt repository as having no changes.
func TestCovGitChangeCandidatesSurfacesAnUnreadableBaseTree(t *testing.T) {
	root, repo := covGitUnbornHeadRepo(t)
	base := covGitTreeAt(t, repo, "main")
	covGitRemoveObject(t, root, covGitEntryHash(t, base, "a.go"))

	got, err := releaseChangeCandidates(context.Background(), repo, base)
	if err == nil {
		t.Fatalf("want the unreadable base tree surfaced, got %v", got)
	}
}

// covGitEntryHash reads one entry's object id out of a tree.
func covGitEntryHash(t *testing.T, tree *object.Tree, name string) string {
	t.Helper()
	entry, err := tree.FindEntry(name)
	if err != nil {
		t.Fatalf("find %s: %v", name, err)
	}
	return entry.Hash.String()
}

// covGitRemoveObject deletes one loose object from the object database.
func covGitRemoveObject(t *testing.T, root, hash string) {
	t.Helper()
	path := filepath.Join(root, ".git", "objects", hash[:2], hash[2:])
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove object %s: %v", hash, err)
	}
}

// TestCovGitHelpersSurfaceAnUnreadableHead covers a HEAD that neither
// resolves nor reports "no commits yet".
//
// The two helpers answer differently on purpose. ReleaseChangedFiles was
// given a base that DID resolve, so it must not claim the repository is
// unchanged; ReleaseWorkingTreeFiles is upstream's `git status`, whose
// failure upstream turns into an empty list.
func TestCovGitHelpersSurfaceAnUnreadableHead(t *testing.T) {
	root := covGitLoopHeadRepo(t)

	if got, err := ReleaseChangedFiles(root, "main"); err == nil {
		t.Errorf("ReleaseChangedFiles: want the unreadable HEAD surfaced, got %v", got)
	}
	got, err := ReleaseWorkingTreeFiles(root)
	if err != nil {
		t.Fatalf("ReleaseWorkingTreeFiles: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want an empty list when status cannot run, got %v", got)
	}
}

// TestCovGitWorkingTreeFilesSurfacesAnUnresolvableDeletedPath covers a HEAD
// tree carrying a `.git`-disguised path, which go-git refuses to resolve.
// Such a tree is reachable: git itself writes one with core.protectNTFS off.
//
// Rename pairing has to look the deleted path up in HEAD, and a lookup that
// fails is not the same as "this delete is not a rename source" — reporting
// the path set anyway would drop or duplicate paths with no way to tell.
func TestCovGitWorkingTreeFilesSurfacesAnUnresolvableDeletedPath(t *testing.T) {
	root := covGitRepo(t)
	gitOracleWrite(t, root, "git~1", "x\n")
	gitOracleWrite(t, root, "keep.go", "package p\n")
	gitOracleRun(t, root, "-c", "core.protectNTFS=false", "add", "-A")
	gitOracleRun(t, root, "commit", "-m", "baseline")
	gitOracleRun(t, root, "rm", "-q", "git~1")
	gitOracleWrite(t, root, "added.go", "package p\n")
	gitOracleRun(t, root, "add", "added.go")

	got, err := ReleaseWorkingTreeFiles(root)
	if err == nil {
		t.Fatalf("want the unresolvable HEAD path surfaced, got %v", got)
	}
}

// ── narrowing candidates to real changes ─────────────────────────────────────

// TestCovGitKeepDifferingComparesContentNotMembership pins the filter that
// turns the deliberately-oversized candidate set into git's answer: a
// candidate is only a change if the two sides actually differ.
func TestCovGitKeepDifferingComparesContentNotMembership(t *testing.T) {
	root := covGitRepo(t)
	gitOracleWrite(t, root, "same.go", "package p\n")
	gitOracleWrite(t, root, "edited.go", "package p\n")
	gitOracleWrite(t, root, "mode.go", "package p\n")
	covGitCommitAll(t, root, "baseline")
	gitOracleWrite(t, root, "edited.go", "package p\n\nfunc Edited() {}\n")
	covGitChmodExecutable(t, filepath.Join(root, "mode.go"))

	repo := covGitOpen(t, root)
	candidates := map[string]bool{
		"same.go": true, "edited.go": true, "mode.go": true, "ghost.go": true,
	}
	got, err := releaseKeepDiffering(
		context.Background(), repo, root, covGitTreeAt(t, repo, "HEAD"), candidates)
	if err != nil {
		t.Fatalf("releaseKeepDiffering: %v", err)
	}

	if gitOracleHas(got, "same.go") {
		t.Errorf("a candidate whose bytes match base is not a change, got %v", got)
	}
	// "ghost.go" is in neither side: the commit range added it and the
	// working tree removed it again, so it is not a change either.
	if gitOracleHas(got, "ghost.go") {
		t.Errorf("a candidate present in neither side is not a change, got %v", got)
	}
	if !gitOracleHas(got, "edited.go") {
		t.Errorf("want the edited path, got %v", got)
	}
	covGitAssertModeOnlyChange(t, root, got)
}

// TestCovGitKeepDifferingRefusesWhatItCannotCompare covers the three inputs
// that make the comparison impossible. Each must refuse rather than drop the
// path, because a dropped path is a change the purge loop never sees.
func TestCovGitKeepDifferingRefusesWhatItCannotCompare(t *testing.T) {
	t.Run("unresolvableCandidatePath", func(t *testing.T) {
		root := covGitOneCommitRepo(t)
		repo := covGitOpen(t, root)
		got, err := releaseKeepDiffering(context.Background(), repo, root,
			covGitTreeAt(t, repo, "HEAD"), map[string]bool{"git~1": true})
		if err == nil {
			t.Fatalf("want the unresolvable path surfaced, got %v", got)
		}
	})

	t.Run("unreadableWorktreePath", func(t *testing.T) {
		deviceRoot, device, irregular := covGitDeviceFile(t)
		repo := covGitOpen(t, covGitOneCommitRepo(t))
		got, err := releaseKeepDiffering(context.Background(), repo, deviceRoot,
			covGitTreeAt(t, repo, "HEAD"), map[string]bool{device: true})
		if irregular != (err != nil) {
			t.Fatalf("device %s/%s: irregular=%v, got %v / %v",
				deviceRoot, device, irregular, got, err)
		}
	})

	t.Run("unreadableConfiguration", func(t *testing.T) {
		root := covGitOneCommitRepo(t)
		repo := covGitOpen(t, root)
		base := covGitTreeAt(t, repo, "HEAD")
		covGitWriteRaw(t, filepath.Join(root, ".git", "config"), "[[[not a section\n")
		got, err := releaseKeepDiffering(context.Background(), repo, root,
			base, map[string]bool{"keep.go": true})
		if err == nil {
			t.Fatalf("want the unreadable configuration surfaced, got %v", got)
		}
	})
}

// TestCovGitKeepDifferingAbandonsACancelledWalk pins the abandoned answer:
// an empty list rather than the paths compared so far, for the same reason
// the candidate walk gives one.
func TestCovGitKeepDifferingAbandonsACancelledWalk(t *testing.T) {
	root := covGitOneCommitRepo(t)
	gitOracleWrite(t, root, "keep.go", "package p\n\nfunc Keep() {}\n")
	repo := covGitOpen(t, root)

	got, err := releaseKeepDiffering(covGitCancelled(), repo, root,
		covGitTreeAt(t, repo, "HEAD"), map[string]bool{"keep.go": true})
	if err != nil {
		t.Fatalf("releaseKeepDiffering: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want no paths from an abandoned walk, got %v", got)
	}
}

// ── status, staged fallback and rename pairing ───────────────────────────────

// TestCovGitStatusHelpersWithoutAWorktree covers a repository with no working
// tree. Status cannot mean anything there, and each caller has its own
// documented answer for that.
func TestCovGitStatusHelpersWithoutAWorktree(t *testing.T) {
	root := covGitOneCommitRepo(t)
	repo := covGitWorktreelessRepo(t, root)

	if _, err := releaseStatus(repo); err == nil {
		t.Error("releaseStatus: want a failure without a working tree")
	}
	staged, err := releaseStagedFiles(repo)
	if err != nil {
		t.Fatalf("releaseStagedFiles: %v", err)
	}
	if len(staged) != 0 {
		t.Errorf("want an empty staged list without a working tree, got %v", staged)
	}
	// The candidate walk is the one caller that refuses: it is mid-way
	// through determining an answer, not substituting for one.
	if got, err := releaseChangeCandidates(
		context.Background(), repo, covGitTreeAt(t, repo, "HEAD")); err == nil {
		t.Errorf("releaseChangeCandidates: want a failure, got %v", got)
	}
}

// TestCovGitRenameSourcesPairsOnlyExactRenames pins which staged deletes are
// suppressed as the source half of a rename. Only a delete whose blob is
// carried by a staged add is a rename source; anything else keeps its path,
// because over-reporting a path costs a reparse while losing one corrupts the
// graph.
func TestCovGitRenameSourcesPairsOnlyExactRenames(t *testing.T) {
	root := covGitRepo(t)
	gitOracleWrite(t, root, "moved.go", "package p\n\nfunc Moved() {}\n")
	gitOracleWrite(t, root, "dropped.go", "package p\n")
	covGitCommitAll(t, root, "baseline")
	gitOracleRun(t, root, "mv", "moved.go", "renamed.go")
	gitOracleRun(t, root, "rm", "-q", "dropped.go")

	repo := covGitOpen(t, root)
	status, err := releaseStatus(repo)
	if err != nil {
		t.Fatalf("releaseStatus: %v", err)
	}
	sources, err := releaseRenameSources(repo, status)
	if err != nil {
		t.Fatalf("releaseRenameSources: %v", err)
	}
	if !sources["moved.go"] {
		t.Errorf("want moved.go suppressed as a rename source, got %v", sources)
	}
	if sources["dropped.go"] {
		t.Errorf("a plain delete is not a rename source, got %v", sources)
	}
}

// TestCovGitRenameSourcesHandlesAnIncompleteIndex covers the two index
// states pairing has to survive: an index it cannot read at all, and a staged
// add whose index entry is missing.
func TestCovGitRenameSourcesHandlesAnIncompleteIndex(t *testing.T) {
	deleteAndAdd := git.Status{
		"keep.go":  &git.FileStatus{Staging: git.Deleted},
		"ghost.go": &git.FileStatus{Staging: git.Added},
		"idle.go":  &git.FileStatus{Staging: git.Unmodified},
	}

	t.Run("unreadableIndex", func(t *testing.T) {
		root := covGitOneCommitRepo(t)
		repo := covGitOpen(t, root)
		covGitWriteRaw(t, filepath.Join(root, ".git", "index"), "not an index")
		got, err := releaseRenameSources(repo, deleteAndAdd)
		if err == nil {
			t.Fatalf("want the unreadable index surfaced, got %v", got)
		}
	})

	t.Run("stagedAddMissingFromTheIndex", func(t *testing.T) {
		repo := covGitOpen(t, covGitOneCommitRepo(t))
		sources, err := releaseRenameSources(repo, deleteAndAdd)
		if err != nil {
			t.Fatalf("releaseRenameSources: %v", err)
		}
		if len(sources) != 0 {
			t.Fatalf("an add with no index entry pairs with nothing, got %v", sources)
		}
	})
}

// ── HEAD and revision resolution ─────────────────────────────────────────────

// TestCovGitHeadTree pins the three answers HEAD can produce. Only the
// no-commits-yet case is "nothing"; the other two are failures, and reporting
// them as no-commits-yet would make a corrupt repository look empty.
func TestCovGitHeadTree(t *testing.T) {
	t.Run("noCommitsYet", func(t *testing.T) {
		tree, err := releaseHeadTree(covGitOpen(t, covGitRepo(t)))
		if err != nil || tree != nil {
			t.Fatalf("want (nil, nil) for a repository with no commits, got %v / %v", tree, err)
		}
	})

	t.Run("unreadableHead", func(t *testing.T) {
		tree, err := releaseHeadTree(covGitOpen(t, covGitLoopHeadRepo(t)))
		if err == nil {
			t.Fatalf("want the unresolvable HEAD surfaced, got %v", tree)
		}
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			t.Fatalf("an unreadable HEAD is not a missing reference: %v", err)
		}
	})

	t.Run("headCommitMissing", func(t *testing.T) {
		root := covGitOneCommitRepo(t)
		covGitWriteRaw(t, filepath.Join(root, ".git", "HEAD"),
			"1111111111111111111111111111111111111111\n")
		tree, err := releaseHeadTree(covGitOpen(t, root))
		if err == nil {
			t.Fatalf("want the missing commit surfaced, got %v", tree)
		}
	})
}

// TestCovGitResolveTreeSurfacesAnObjectStoreFailure covers a revision that
// names a commit the object database then refuses to hand back. Answering
// "the base does not resolve" there would silently downgrade the caller to
// the staged-only retry and report the wrong path set.
func TestCovGitResolveTreeSurfacesAnObjectStoreFailure(t *testing.T) {
	root := covGitOneCommitRepo(t)
	repo := covGitOpen(t, root)
	head := strings.TrimSpace(gitOracleRun(t, root, "rev-parse", "HEAD"))
	repo.Storer = &covGitRationedStorer{Storer: repo.Storer, hash: head, budget: 1}

	tree, err := resolveReleaseTree(repo, "HEAD")
	if !errors.Is(err, covGitErrStoreClosed) {
		t.Fatalf("want the object-store failure surfaced, got %v / %v", tree, err)
	}
}

// ── tree and working-tree readers ────────────────────────────────────────────

// TestCovGitTreeEntry pins how a tree lookup classifies its three outcomes.
// "Absent" and "unreadable" have to stay distinct: a diff can treat an absent
// path as one side of an add or delete, but not a path it failed to read.
func TestCovGitTreeEntry(t *testing.T) {
	root := covGitRepo(t)
	gitOracleWrite(t, root, "keep.go", "package p\n")
	gitOracleWrite(t, root, "nested/deep.go", "package p\n")
	covGitCommitAll(t, root, "baseline")
	tree := covGitTreeAt(t, covGitOpen(t, root), "HEAD")

	t.Run("blob", func(t *testing.T) {
		entry, exists, err := releaseTreeEntry(tree, "nested/deep.go")
		if err != nil || !exists {
			t.Fatalf("want the nested blob, got %v / %v / %v", entry, exists, err)
		}
	})

	t.Run("missingPath", func(t *testing.T) {
		_, exists, err := releaseTreeEntry(tree, "nested/ghost.go")
		if err != nil || exists {
			t.Fatalf("want absent with no error, got %v / %v", exists, err)
		}
	})

	t.Run("directory", func(t *testing.T) {
		// git compares blobs; a tree at the path means no file exists there.
		_, exists, err := releaseTreeEntry(tree, "nested")
		if err != nil || exists {
			t.Fatalf("want a directory reported absent, got %v / %v", exists, err)
		}
	})

	t.Run("unresolvablePath", func(t *testing.T) {
		_, exists, err := releaseTreeEntry(tree, "git~1")
		if err == nil {
			t.Fatalf("want the refused lookup surfaced, got exists=%v", exists)
		}
	})
}

// TestCovGitWorktreeBlob pins what the working-tree side of a diff reads.
// Only a file and a symlink can be a blob; everything else is either absent
// or a read failure, and conflating the two would invent or lose a change.
func TestCovGitWorktreeBlob(t *testing.T) {
	root := covGitRepo(t)
	gitOracleWrite(t, root, "keep.go", "package p\n")
	gitOracleWrite(t, root, "nested/deep.go", "package p\n")

	t.Run("regularFile", func(t *testing.T) {
		mode, content, exists, err := releaseWorktreeBlob(root, "keep.go")
		if err != nil || !exists || mode != filemode.Regular {
			t.Fatalf("want a regular blob, got %v / %v / %v", mode, exists, err)
		}
		if string(content) != "package p\n" {
			t.Fatalf("want the file's bytes, got %q", content)
		}
	})

	t.Run("missingPath", func(t *testing.T) {
		_, _, exists, err := releaseWorktreeBlob(root, "ghost.go")
		if err != nil || exists {
			t.Fatalf("want absent with no error, got %v / %v", exists, err)
		}
	})

	t.Run("directory", func(t *testing.T) {
		_, _, exists, err := releaseWorktreeBlob(root, "nested")
		if err != nil || exists {
			t.Fatalf("want a directory reported absent, got %v / %v", exists, err)
		}
	})

	t.Run("symlink", covGitSymlinkBlobCase(root))
	t.Run("unstatablePath", covGitUnstatableBlobCase(root))
	t.Run("irregularFile", covGitIrregularBlobCase())
	t.Run("unreadableFile", covGitUnreadableBlobCase())
}

// covGitSymlinkBlobCase pins that a symlink stages as its target string,
// which is exactly what git stores in the blob.
func covGitSymlinkBlobCase(root string) func(*testing.T) {
	return func(t *testing.T) {
		if !covGitSymlink("keep.go", filepath.Join(root, "link.go")) {
			_, _, exists, err := releaseWorktreeBlob(root, "link.go")
			if err != nil || exists {
				t.Fatalf("no symlink was created, so the path is absent: %v / %v", exists, err)
			}
			return
		}
		mode, content, exists, err := releaseWorktreeBlob(root, "link.go")
		if err != nil || !exists || mode != filemode.Symlink {
			t.Fatalf("want a symlink blob, got %v / %v / %v", mode, exists, err)
		}
		if string(content) != "keep.go" {
			t.Fatalf("want the link target as the blob content, got %q", content)
		}
	}
}

// covGitUnstatableBlobCase routes a path through a component that is a FILE.
// POSIX answers ENOTDIR — a read failure that has to be surfaced — while
// Windows answers "path not found", which is genuinely absent; os.Lstat is
// the oracle for which one the running platform gives.
func covGitUnstatableBlobCase(root string) func(*testing.T) {
	return func(t *testing.T) {
		const through = "keep.go/inner"
		_, lstatErr := os.Lstat(filepath.Join(root, filepath.FromSlash(through)))
		_, _, exists, err := releaseWorktreeBlob(root, through)
		if exists {
			t.Fatalf("a path under a file is never a blob, got err=%v", err)
		}
		if errors.Is(lstatErr, os.ErrNotExist) != (err == nil) {
			t.Fatalf("want the os.Lstat outcome mirrored (%v), got %v", lstatErr, err)
		}
	}
}

// covGitIrregularBlobCase covers a character device: git has no mode for it,
// and reporting it as absent would make the path look deleted.
func covGitIrregularBlobCase() func(*testing.T) {
	return func(t *testing.T) {
		root, name, irregular := covGitDeviceFile(t)
		_, _, exists, err := releaseWorktreeBlob(root, name)
		if exists {
			t.Fatalf("%s/%s is not a blob, got err=%v", root, name, err)
		}
		if irregular != (err != nil) {
			t.Fatalf("%s/%s: irregular=%v, got err=%v", root, name, irregular, err)
		}
	}
}

// covGitUnreadableBlobCase covers a file the process may not read. git fails
// the diff there, so the helper must too rather than calling it unchanged.
func covGitUnreadableBlobCase() func(*testing.T) {
	return func(t *testing.T) {
		root := t.TempDir()
		gitOracleWrite(t, root, "denied.go", "package p\n")
		path := filepath.Join(root, "denied.go")
		if !covGitDenyRead(t, path) {
			mode, content, exists, err := releaseWorktreeBlob(root, "denied.go")
			if err != nil || !exists || mode != filemode.Regular || len(content) == 0 {
				t.Fatalf("a still-readable file is a blob, got %v / %v / %v", mode, exists, err)
			}
			return
		}
		_, _, exists, err := releaseWorktreeBlob(root, "denied.go")
		if err == nil || exists {
			t.Fatalf("want the read failure surfaced, got %v / %v", exists, err)
		}
	}
}

// ── blob comparison ──────────────────────────────────────────────────────────

// TestCovGitBlobEquals pins the comparison that decides whether a candidate
// is a real change. A blob the object database cannot serve counts as
// DIFFERENT: claiming the sides are equal would silently drop a change, and
// git would have failed the diff outright.
func TestCovGitBlobEquals(t *testing.T) {
	root := covGitRepo(t)
	const body = "package p\n\nfunc Keep() {}\n"
	gitOracleWrite(t, root, "keep.go", body)
	covGitCommitAll(t, root, "baseline")
	repo := covGitOpen(t, root)
	entry, _, err := releaseTreeEntry(covGitTreeAt(t, repo, "HEAD"), "keep.go")
	if err != nil {
		t.Fatalf("tree entry: %v", err)
	}
	blob := entry.Hash

	t.Run("identicalContent", covGitBlobEqualsCase(repo, blob, body, true))
	t.Run("differentLength", covGitBlobEqualsCase(repo, blob, body+"\n", false))
	t.Run("sameLengthDifferentBytes", covGitBlobEqualsCase(
		repo, blob, strings.Repeat("x", len(body)), false))
	t.Run("missingBlob", covGitBlobEqualsCase(
		repo, plumbing.NewHash("1111111111111111111111111111111111111111"), body, false))

	t.Run("unopenableContent", func(t *testing.T) {
		faulty := covGitOpen(t, root)
		faulty.Storer = covGitUnreadableBlobStorer{
			Storer: faulty.Storer, hash: blob.String(), openErr: true}
		covGitBlobEqualsCase(faulty, blob, body, false)(t)
	})

	t.Run("truncatedContent", func(t *testing.T) {
		faulty := covGitOpen(t, root)
		faulty.Storer = covGitUnreadableBlobStorer{
			Storer: faulty.Storer, hash: blob.String()}
		covGitBlobEqualsCase(faulty, blob, body, false)(t)
	})
}

// covGitBlobEqualsCase asserts one comparison outcome.
func covGitBlobEqualsCase(
	repo *git.Repository, hash plumbing.Hash, content string, want bool,
) func(*testing.T) {
	return func(t *testing.T) {
		if got := releaseBlobEquals(repo, hash, []byte(content)); got != want {
			t.Fatalf("want equal=%v, got %v", want, got)
		}
	}
}

// ── recorded file mode ───────────────────────────────────────────────────────

// TestCovGitTracksFileMode pins git's core.fileMode reading. The default is
// ON: a repository that says nothing about the executable bit records it, and
// defaulting to OFF instead would hide every mode-only change.
func TestCovGitTracksFileMode(t *testing.T) {
	t.Run("configuredValue", func(t *testing.T) {
		root := covGitRepo(t)
		gitOracleRun(t, root, "config", "core.filemode", "false")
		covGitAssertTracksFileMode(t, covGitOpen(t, root), false)
	})

	t.Run("unset", func(t *testing.T) {
		root := covGitRepo(t)
		gitOracleRun(t, root, "config", "--unset", "core.filemode")
		covGitAssertTracksFileMode(t, covGitOpen(t, root), true)
	})

	t.Run("unparseableValue", func(t *testing.T) {
		root := covGitRepo(t)
		gitOracleRun(t, root, "config", "core.filemode", "perhaps")
		covGitAssertTracksFileMode(t, covGitOpen(t, root), true)
	})

	t.Run("configurationWithoutRawSections", func(t *testing.T) {
		repo := covGitOpen(t, covGitRepo(t))
		repo.Storer = covGitRawlessConfigStorer{Storer: repo.Storer}
		covGitAssertTracksFileMode(t, repo, true)
	})

	t.Run("unreadableConfiguration", func(t *testing.T) {
		root := covGitRepo(t)
		repo := covGitOpen(t, root)
		covGitWriteRaw(t, filepath.Join(root, ".git", "config"), "[[[not a section\n")
		if tracks, err := releaseTracksFileMode(repo); err == nil {
			t.Fatalf("want the unreadable configuration surfaced, got %v", tracks)
		}
	})
}

func covGitAssertTracksFileMode(t *testing.T, repo *git.Repository, want bool) {
	t.Helper()
	tracks, err := releaseTracksFileMode(repo)
	if err != nil {
		t.Fatalf("releaseTracksFileMode: %v", err)
	}
	if tracks != want {
		t.Fatalf("want tracksMode=%v, got %v", want, tracks)
	}
}

// ── timeout and output ordering ──────────────────────────────────────────────

// TestCovGitGitTimeout pins upstream's CRG_GIT_TIMEOUT. Only a positive
// integer overrides the default; a zero or negative budget would abandon
// every walk before it started and report every repository as unchanged.
func TestCovGitGitTimeout(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want time.Duration
	}{
		{"unset", "", releaseGitTimeoutDefault},
		{"seconds", "5", 5 * time.Second},
		{"notANumber", "soon", releaseGitTimeoutDefault},
		{"zero", "0", releaseGitTimeoutDefault},
		{"negative", "-3", releaseGitTimeoutDefault},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("CRG_GIT_TIMEOUT", testCase.raw)
			if got := releaseGitTimeout(); got != testCase.want {
				t.Fatalf("want %v, got %v", testCase.want, got)
			}
		})
	}
}

// TestCovGitSortedPaths pins the output shape every helper returns through:
// deduplicated, slash-separated and ordered, so a caller never sees the same
// path twice or an order that depends on map iteration.
func TestCovGitSortedPaths(t *testing.T) {
	got := releaseSortedPaths([]string{
		"b.go",
		filepath.Join("nested", "a.go"),
		"b.go",
		"a.go",
		filepath.Join("nested", "a.go"),
	})
	want := []string{"a.go", "b.go", "nested/a.go"}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("want %v, got %v", want, got)
	}
}
