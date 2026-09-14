package crgbehavior

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/storage"
)

// *gitRepo is the production implementation behind the corpus builder's read
// seam; the interface is what keeps the builder's failure branches reachable.
var _ repoReader = (*gitRepo)(nil)

// repoTBase anchors fixture committer times. Every staged commit gets a
// distinct, increasing timestamp: Window orders by committer time, so equal
// timestamps would leave "newest first" ambiguous.
var repoTBase = time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC)

// repoTSignature stamps hand-built commit objects.
var repoTSignature = object.Signature{Name: "Repo Test", Email: "repo@example.test", When: repoTBase}

// repoTBogusHash is well formed but names no object, which is the state an
// interrupted fetch or a half-finished gc leaves behind.
var repoTBogusHash = plumbing.NewHash("0123456789abcdef0123456789abcdef01234567")

// repoTErrUnreadable stands in for storage that fails a read after the object
// header was accepted, the way a loose object with a corrupt body behaves.
var repoTErrUnreadable = errors.New("repoT: object content unreadable")

// Fixture bodies and the lines the reader must recover from them. The pairing
// is the oracle: boundedLines strips the trailing newline and nothing else.
const (
	repoTBodyV1    = "package a\n\nfunc One() {}\n"
	repoTBodyV2    = "package a\n\nfunc One() {}\n\nfunc Two() {}\n"
	repoTBodyV3    = "package a\n\nfunc Two() {}\n"
	repoTBodyKeep  = "keep\n"
	repoTBodyOther = "package b\n"
)

var (
	repoTLinesV1    = []string{"package a", "", "func One() {}"}
	repoTLinesV2    = []string{"package a", "", "func One() {}", "", "func Two() {}"}
	repoTLinesV3    = []string{"package a", "", "func Two() {}"}
	repoTLinesKeep  = []string{"keep"}
	repoTLinesOther = []string{"package b"}
)

// repoTFile is one path a staged commit writes, or deletes when remove is set.
type repoTFile struct {
	path   string
	body   string
	remove bool
}

// repoTCommit is one commit of a staged fixture repository.
type repoTCommit struct {
	subject string
	files   []repoTFile
	// parents overrides the commit's parents, which is how a fixture stages a
	// real two-parent merge instead of a linear commit.
	parents []plumbing.Hash
}

// repoTFixture is a staged repository: its root, the go-git handle and the
// commit hashes in staging order, oldest first.
type repoTFixture struct {
	root   string
	repo   *git.Repository
	hashes []plumbing.Hash
}

// repoTStage stages a repository from a commit-by-commit spec. Staging is
// native go-git, in process: the gate runs no git subprocess and neither does
// its test bed, so a fixture cannot depend on an ambient git binary or on the
// developer's global git configuration.
func repoTStage(t *testing.T, commits ...repoTCommit) repoTFixture {
	t.Helper()
	root := t.TempDir()
	repo, err := git.PlainInit(root, false)
	if err != nil {
		t.Fatalf("PlainInit: %v", err)
	}
	fixture := repoTFixture{root: root, repo: repo}
	for i, spec := range commits {
		when := repoTBase.Add(time.Duration(i) * time.Minute)
		fixture.hashes = append(fixture.hashes, repoTApplyCommit(t, fixture, spec, when))
	}
	return fixture
}

// repoTApplyCommit stages a spec's files and commits them at a fixed time.
func repoTApplyCommit(t *testing.T, fixture repoTFixture, spec repoTCommit, when time.Time) plumbing.Hash {
	t.Helper()
	wt := repoTWorktree(t, fixture)
	for _, f := range spec.files {
		repoTStageFile(t, wt, fixture.root, f)
	}
	sig := &object.Signature{Name: repoTSignature.Name, Email: repoTSignature.Email, When: when}
	hash, err := wt.Commit(spec.subject, &git.CommitOptions{Author: sig, Committer: sig, Parents: spec.parents})
	if err != nil {
		t.Fatalf("commit %q: %v", spec.subject, err)
	}
	return hash
}

// repoTStageFile writes or removes one path and stages the result.
func repoTStageFile(t *testing.T, wt *git.Worktree, root string, f repoTFile) {
	t.Helper()
	if f.remove {
		if _, err := wt.Remove(f.path); err != nil {
			t.Fatalf("remove %s: %v", f.path, err)
		}
		return
	}
	path := filepath.Join(root, filepath.FromSlash(f.path))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", f.path, err)
	}
	if err := os.WriteFile(path, []byte(f.body), 0o600); err != nil {
		t.Fatalf("write %s: %v", f.path, err)
	}
	if _, err := wt.Add(f.path); err != nil {
		t.Fatalf("add %s: %v", f.path, err)
	}
}

// repoTWorktree is the fixture's worktree handle.
func repoTWorktree(t *testing.T, fixture repoTFixture) *git.Worktree {
	t.Helper()
	wt, err := fixture.repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}
	return wt
}

// repoTOpen reads a staged fixture through the production entry point.
func repoTOpen(t *testing.T, root string) *gitRepo {
	t.Helper()
	g, err := openRepo(root)
	if err != nil {
		t.Fatalf("openRepo(%s): %v", root, err)
	}
	return g
}

// repoTWantHash asserts a revision resolves to the expected commit.
func repoTWantHash(t *testing.T, g *gitRepo, rev string, want plumbing.Hash) {
	t.Helper()
	got, err := g.Resolve(rev)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", rev, err)
	}
	if got != want {
		t.Fatalf("Resolve(%q) = %s, want %s", rev, got, want)
	}
}

// repoTWantErr asserts a call fails with an error naming the failed operation.
func repoTWantErr(t *testing.T, call func() error, want string) {
	t.Helper()
	err := call()
	if err == nil {
		t.Fatalf("call succeeded, want an error containing %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want one containing %q", err, want)
	}
}

func TestRepoTOpenRepoFindsTheEnclosingRepository(t *testing.T) {
	fixture := repoTStage(t, repoTCommit{
		subject: "feat: seed",
		files:   []repoTFile{{path: "pkg/a.go", body: repoTBodyV1}},
	})

	t.Run("from the repository root", func(t *testing.T) {
		repoTWantHash(t, repoTOpen(t, fixture.root), "HEAD", fixture.hashes[0])
	})

	t.Run("from a subdirectory", func(t *testing.T) {
		g := repoTOpen(t, filepath.Join(fixture.root, "pkg"))
		repoTWantHash(t, g, "HEAD", fixture.hashes[0])
	})

	t.Run("rejects a directory that is not a repository", func(t *testing.T) {
		root := t.TempDir()
		g, err := openRepo(root)
		if g != nil {
			t.Fatalf("openRepo(%s) = %v, want nil reader", root, g)
		}
		if err == nil || !strings.Contains(err.Error(), "open repository "+root) {
			t.Fatalf("openRepo(%s) error = %v, want one naming the directory", root, err)
		}
	})
}

func TestRepoTResolveAcceptsEveryRevisionSpelling(t *testing.T) {
	fixture := repoTStage(t,
		repoTCommit{subject: "feat: first", files: []repoTFile{{path: "a.go", body: repoTBodyV1}}},
		repoTCommit{subject: "feat: second", files: []repoTFile{{path: "b.go", body: repoTBodyOther}}},
	)
	g := repoTOpen(t, fixture.root)
	first, second := fixture.hashes[0], fixture.hashes[1]

	for _, tc := range []struct {
		name string
		rev  string
		want plumbing.Hash
	}{
		// The branch name is read, not assumed: the test must not depend on
		// go-git's default branch.
		{name: "branch name", rev: repoTBranchName(t, fixture), want: second},
		{name: "HEAD", rev: "HEAD", want: second},
		{name: "full sha", rev: second.String(), want: second},
		{name: "first parent", rev: "HEAD^", want: first},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repoTWantHash(t, g, tc.rev, tc.want)
		})
	}

	t.Run("bogus revision", func(t *testing.T) {
		got, err := g.Resolve("no/such/rev")
		if got != plumbing.ZeroHash {
			t.Fatalf("Resolve(bogus) = %s, want the zero hash", got)
		}
		if err == nil || !strings.Contains(err.Error(), "resolve no/such/rev") {
			t.Fatalf("Resolve(bogus) error = %v, want one naming the revision", err)
		}
	})
}

// repoTBranchName is the fixture's checked-out branch.
func repoTBranchName(t *testing.T, fixture repoTFixture) string {
	t.Helper()
	head, err := fixture.repo.Head()
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	return head.Name().Short()
}

func TestRepoTWindowWalksNewestFirst(t *testing.T) {
	fixture := repoTStage(t,
		repoTCommit{subject: "feat: one", files: []repoTFile{{path: "a.go", body: repoTBodyV1}}},
		repoTCommit{subject: "feat: two", files: []repoTFile{{path: "b.go", body: repoTBodyOther}}},
		repoTCommit{subject: "feat: three", files: []repoTFile{{path: "c.go", body: repoTBodyOther}}},
	)
	g := repoTOpen(t, fixture.root)
	one, two, three := fixture.hashes[0], fixture.hashes[1], fixture.hashes[2]

	for _, tc := range []struct {
		name  string
		count int
		want  []plumbing.Hash
	}{
		{name: "count below history keeps only the newest", count: 2, want: []plumbing.Hash{three, two}},
		{name: "count equal to history", count: 3, want: []plumbing.Hash{three, two, one}},
		{name: "count above history returns everything", count: 10, want: []plumbing.Hash{three, two, one}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := g.Window("HEAD", tc.count)
			if err != nil {
				t.Fatalf("Window(HEAD, %d): %v", tc.count, err)
			}
			repoTWantHashes(t, got, tc.want)
		})
	}
}

// repoTWantHashes asserts a window's length and order.
func repoTWantHashes(t *testing.T, got, want []plumbing.Hash) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("window = %v, want %v", repoTShorten(got), repoTShorten(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("window[%d] = %s, want %s (full window %v)",
				i, short(got[i].String()), short(want[i].String()), repoTShorten(got))
		}
	}
}

// repoTShorten abbreviates a window for failure messages.
func repoTShorten(hashes []plumbing.Hash) []string {
	out := make([]string, 0, len(hashes))
	for _, h := range hashes {
		out = append(out, short(h.String()))
	}
	return out
}

func TestRepoTWindowSkipsMergeCommits(t *testing.T) {
	fixture := repoTStage(t,
		repoTCommit{subject: "feat: base", files: []repoTFile{{path: "a.go", body: repoTBodyV1}}},
		repoTCommit{subject: "feat: mainline", files: []repoTFile{{path: "main.go", body: repoTBodyOther}}},
	)
	base, mainline := fixture.hashes[0], fixture.hashes[1]
	side := repoTStageSideBranch(t, fixture, base)
	merge := repoTStageMerge(t, fixture, mainline, side)
	g := repoTOpen(t, fixture.root)

	got, err := g.Window(merge.String(), 10)
	if err != nil {
		t.Fatalf("Window(%s, 10): %v", short(merge.String()), err)
	}
	// The merge itself introduces no review task, but the walk must still
	// reach both of its sides.
	repoTWantHashes(t, got, []plumbing.Hash{side, mainline, base})
}

// repoTStageSideBranch commits on a branch forked at base, producing the real
// divergence a merge commit joins.
func repoTStageSideBranch(t *testing.T, fixture repoTFixture, base plumbing.Hash) plumbing.Hash {
	t.Helper()
	opts := &git.CheckoutOptions{Branch: plumbing.NewBranchReferenceName("side"), Hash: base, Create: true}
	if err := repoTWorktree(t, fixture).Checkout(opts); err != nil {
		t.Fatalf("checkout side branch at %s: %v", short(base.String()), err)
	}
	spec := repoTCommit{subject: "feat: side", files: []repoTFile{{path: "side.go", body: repoTBodyOther}}}
	return repoTApplyCommit(t, fixture, spec, repoTBase.Add(2*time.Minute))
}

// repoTStageMerge joins two tips into a real two-parent commit.
func repoTStageMerge(t *testing.T, fixture repoTFixture, first, second plumbing.Hash) plumbing.Hash {
	t.Helper()
	if err := repoTWorktree(t, fixture).Checkout(&git.CheckoutOptions{Hash: first}); err != nil {
		t.Fatalf("checkout %s: %v", short(first.String()), err)
	}
	spec := repoTCommit{
		subject: "merge: side into mainline",
		files:   []repoTFile{{path: "side.go", body: repoTBodyOther}},
		parents: []plumbing.Hash{first, second},
	}
	return repoTApplyCommit(t, fixture, spec, repoTBase.Add(3*time.Minute))
}

func TestRepoTWindowReportsUnwalkableHistory(t *testing.T) {
	fixture := repoTStage(t, repoTCommit{
		subject: "feat: seed",
		files:   []repoTFile{{path: "a.go", body: repoTBodyV1}},
	})
	g := repoTOpen(t, fixture.root)

	t.Run("bogus revision", func(t *testing.T) {
		repoTWantErr(t, func() error {
			_, err := g.Window("no/such/rev", 3)
			return err
		}, "resolve no/such/rev")
	})

	t.Run("dangling parent aborts the walk", func(t *testing.T) {
		seed := repoTCommitObject(t, fixture, fixture.hashes[0])
		orphan := repoTStore(t, fixture, &object.Commit{
			Author: repoTSignature, Committer: repoTSignature,
			Message:      "craft: dangling parent\n",
			TreeHash:     seed.TreeHash,
			ParentHashes: []plumbing.Hash{repoTBogusHash},
		})
		repoTWantErr(t, func() error {
			_, err := g.Window(orphan.String(), 3)
			return err
		}, "walk history from "+orphan.String())
	})

	t.Run("storage failure while opening the walk", func(t *testing.T) {
		faulty := repoTRepoOver(t, fixture, repoTFaultStorer{
			Storer: fixture.repo.Storer,
			hash:   repoTResolvedHead(t, fixture),
			reads:  new(int),
		})
		repoTWantErr(t, func() error {
			_, err := faulty.Window("HEAD", 3)
			return err
		}, "walk history from HEAD")
	})
}

// repoTResolvedHead is the commit HEAD names.
func repoTResolvedHead(t *testing.T, fixture repoTFixture) plumbing.Hash {
	t.Helper()
	head, err := fixture.repo.Head()
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	return head.Hash()
}

// repoTFaultStorer wraps a repository's object storage and fails every read of
// one hash after the first. It models storage that degrades mid-operation: one
// read of an object lands and the next one does not.
type repoTFaultStorer struct {
	storage.Storer
	hash  plumbing.Hash
	reads *int
}

func (s repoTFaultStorer) EncodedObject(t plumbing.ObjectType, h plumbing.Hash) (plumbing.EncodedObject, error) {
	if h == s.hash {
		*s.reads++
		if *s.reads > 1 {
			return nil, repoTErrUnreadable
		}
	}
	return s.Storer.EncodedObject(t, h)
}

// repoTPoisonStorer serves one object whose header reads fine but whose content
// stream fails, the way a loose object with a corrupt body does.
type repoTPoisonStorer struct {
	storage.Storer
	hash plumbing.Hash
}

func (s repoTPoisonStorer) EncodedObject(t plumbing.ObjectType, h plumbing.Hash) (plumbing.EncodedObject, error) {
	obj, err := s.Storer.EncodedObject(t, h)
	if err != nil || h != s.hash {
		return obj, err
	}
	return repoTBrokenObject{EncodedObject: obj}, nil
}

// repoTBrokenObject reports its metadata but cannot be read.
type repoTBrokenObject struct {
	plumbing.EncodedObject
}

func (repoTBrokenObject) Reader() (io.ReadCloser, error) { return nil, repoTErrUnreadable }

// repoTRepoOver rebuilds the fixture's reader over a substituted object store.
func repoTRepoOver(t *testing.T, fixture repoTFixture, store storage.Storer) *gitRepo {
	t.Helper()
	repo, err := git.Open(store, repoTWorktree(t, fixture).Filesystem())
	if err != nil {
		t.Fatalf("Open over substituted storage: %v", err)
	}
	return &gitRepo{repo: repo}
}

func TestRepoTSubjectIsTheFirstMessageLine(t *testing.T) {
	fixture := repoTStage(t,
		repoTCommit{subject: "feat: one line only", files: []repoTFile{{path: "a.go", body: repoTBodyV1}}},
		repoTCommit{
			subject: "feat: with a body\n\nThe body explains why, and is not the subject.\n",
			files:   []repoTFile{{path: "b.go", body: repoTBodyOther}},
		},
		repoTCommit{subject: "  feat: padded  \n\nbody\n", files: []repoTFile{{path: "c.go", body: repoTBodyOther}}},
	)
	g := repoTOpen(t, fixture.root)

	for _, tc := range []struct {
		name string
		hash plumbing.Hash
		want string
	}{
		{name: "a single-line message is the subject", hash: fixture.hashes[0], want: "feat: one line only"},
		{name: "a multi-line message keeps only its first line", hash: fixture.hashes[1], want: "feat: with a body"},
		{name: "surrounding space is trimmed", hash: fixture.hashes[2], want: "feat: padded"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := g.Subject(tc.hash)
			if err != nil {
				t.Fatalf("Subject(%s): %v", short(tc.hash.String()), err)
			}
			if got != tc.want {
				t.Fatalf("Subject = %q, want %q", got, tc.want)
			}
		})
	}

	t.Run("a hash with no commit object", func(t *testing.T) {
		got, err := g.Subject(repoTBogusHash)
		if got != "" {
			t.Fatalf("Subject(dangling) = %q, want the empty string", got)
		}
		if err == nil || !strings.Contains(err.Error(), "read commit "+short(repoTBogusHash.String())) {
			t.Fatalf("Subject(dangling) error = %v, want one naming the commit", err)
		}
	})
}

// repoTChangesFixture stages a root commit, a modification, a deletion and a
// rename, which is every name-level shape Changes has to lower.
func repoTChangesFixture(t *testing.T) repoTFixture {
	t.Helper()
	return repoTStage(t,
		repoTCommit{subject: "feat: root", files: []repoTFile{
			{path: "a.go", body: repoTBodyV1},
			{path: "keep.txt", body: repoTBodyKeep},
		}},
		repoTCommit{subject: "feat: extend", files: []repoTFile{
			{path: "a.go", body: repoTBodyV2},
			{path: "pkg/b.go", body: repoTBodyOther},
		}},
		repoTCommit{subject: "refactor: drop keep.txt", files: []repoTFile{
			{path: "a.go", body: repoTBodyV3},
			{path: "keep.txt", remove: true},
		}},
		// A rename staged the way git records one: a delete plus an add.
		repoTCommit{subject: "refactor: move a.go", files: []repoTFile{
			{path: "a.go", remove: true},
			{path: "moved/a.go", body: repoTBodyV3},
		}},
	)
}

func TestRepoTChangesLowersEveryNameLevelShape(t *testing.T) {
	fixture := repoTChangesFixture(t)
	g := repoTOpen(t, fixture.root)

	for _, tc := range []struct {
		name string
		hash plumbing.Hash
		want []FileChange
	}{
		{
			name: "a root commit reports every file as an addition",
			hash: fixture.hashes[0],
			want: []FileChange{
				{Path: "a.go", After: repoTLinesV1},
				{Path: "keep.txt", After: repoTLinesKeep},
			},
		},
		{
			name: "a modification carries both sides, an addition only the post-image",
			hash: fixture.hashes[1],
			want: []FileChange{
				{Path: "a.go", Before: repoTLinesV1, After: repoTLinesV2},
				{Path: "pkg/b.go", After: repoTLinesOther},
			},
		},
		{
			name: "a deletion is dropped",
			hash: fixture.hashes[2],
			want: []FileChange{
				{Path: "a.go", Before: repoTLinesV2, After: repoTLinesV3},
			},
		},
		{
			name: "a rename reports only the post-image path",
			hash: fixture.hashes[3],
			want: []FileChange{
				{Path: "moved/a.go", After: repoTLinesV3},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := g.Changes(tc.hash)
			if err != nil {
				t.Fatalf("Changes(%s): %v", short(tc.hash.String()), err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Changes = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestRepoTChangesDropsNonFileEntries(t *testing.T) {
	fixture := repoTStage(t, repoTCommit{
		subject: "feat: seed",
		files:   []repoTFile{{path: "a.go", body: repoTBodyV1}},
	})
	seed := repoTCommitObject(t, fixture, fixture.hashes[0])
	// A gitlink: a tree entry that is not a file, so it carries no post-image
	// content the declaration extractor could read. Tree entries are stored in
	// name order, as git requires.
	tree := repoTStore(t, fixture, &object.Tree{Entries: []object.TreeEntry{
		{Name: "a.go", Mode: filemode.Regular, Hash: repoTEntryHash(t, seed, "a.go")},
		{Name: "vendored", Mode: filemode.Submodule, Hash: fixture.hashes[0]},
	}})
	withSubmodule := repoTStore(t, fixture, &object.Commit{
		Author: repoTSignature, Committer: repoTSignature,
		Message:  "craft: add a gitlink\n",
		TreeHash: tree,
	})

	got, err := repoTOpen(t, fixture.root).Changes(withSubmodule)
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	want := []FileChange{{Path: "a.go", After: repoTLinesV1}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Changes = %+v, want %+v", got, want)
	}
}

func TestRepoTChangesBoundsHugeFiles(t *testing.T) {
	atBound := repoTFiller(maxScannedFileBytes)
	overBound := repoTFiller(maxScannedFileBytes + 1)
	fixture := repoTStage(t, repoTCommit{subject: "chore: vendor two bundles", files: []repoTFile{
		{path: "at-bound.txt", body: atBound},
		{path: "over-bound.txt", body: overBound},
	}})

	got, err := repoTOpen(t, fixture.root).Changes(fixture.hashes[0])
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}

	t.Run("a file at the bound is read", func(t *testing.T) {
		change := repoTFind(t, got, "at-bound.txt")
		if len(change.After) != 1 || len(change.After[0]) != maxScannedFileBytes-1 {
			t.Fatalf("at-bound lines = %d line(s) of %d byte(s), want 1 line of %d",
				len(change.After), repoTFirstLineLen(change.After), maxScannedFileBytes-1)
		}
	})

	t.Run("a file past the bound is reported with no lines", func(t *testing.T) {
		change := repoTFind(t, got, "over-bound.txt")
		if change.After != nil || change.Before != nil {
			t.Fatalf("over-bound change = %d before / %d after line(s), want none on either side",
				len(change.Before), len(change.After))
		}
	})
}

// repoTFiller builds a single-line body of exactly size bytes.
func repoTFiller(size int) string {
	return strings.Repeat("x", size-1) + "\n"
}

// repoTFirstLineLen is the length of a change's first post-image line.
func repoTFirstLineLen(lines []string) int {
	if len(lines) == 0 {
		return 0
	}
	return len(lines[0])
}

// repoTFind is the change for one path, which must be present.
func repoTFind(t *testing.T, changes []FileChange, path string) FileChange {
	t.Helper()
	for _, change := range changes {
		if change.Path == path {
			return change
		}
	}
	t.Fatalf("no change for %s in %v", path, repoTPaths(changes))
	return FileChange{}
}

// repoTPaths lists the changed paths for failure messages.
func repoTPaths(changes []FileChange) []string {
	out := make([]string, 0, len(changes))
	for _, change := range changes {
		out = append(out, change.Path)
	}
	return out
}

func TestRepoTChangesReportsUnreadableObjects(t *testing.T) {
	fixture := repoTStage(t, repoTCommit{
		subject: "feat: seed",
		files:   []repoTFile{{path: "a.go", body: repoTBodyV1}},
	})
	seed := repoTCommitObject(t, fixture, fixture.hashes[0])
	danglingTree := repoTCraftCommit(t, fixture, "dangling tree", repoTBogusHash)
	danglingParent := repoTCraftCommit(t, fixture, "dangling parent", seed.TreeHash, repoTBogusHash)
	parentWithoutTree := repoTCraftCommit(t, fixture, "parent without a tree", seed.TreeHash, danglingTree)
	danglingBlob := repoTCraftCommit(t, fixture, "dangling blob", repoTStore(t, fixture,
		&object.Tree{Entries: []object.TreeEntry{{Name: "gone.go", Mode: filemode.Regular, Hash: repoTBogusHash}}}))
	g := repoTOpen(t, fixture.root)

	for _, tc := range []struct {
		name string
		hash plumbing.Hash
		want string
	}{
		{name: "no commit object", hash: repoTBogusHash, want: "read commit " + short(repoTBogusHash.String())},
		{name: "no tree object", hash: danglingTree, want: "read tree of " + short(danglingTree.String())},
		{name: "no parent object", hash: danglingParent, want: "read parent of " + short(danglingParent.String())},
		{
			name: "no parent tree object",
			hash: parentWithoutTree,
			want: "read parent tree of " + short(parentWithoutTree.String()),
		},
		{name: "no blob object", hash: danglingBlob, want: "read changed file gone.go"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := g.Changes(tc.hash)
			if got != nil {
				t.Fatalf("Changes = %+v, want no changes", got)
			}
			repoTWantErr(t, func() error { return err }, tc.want)
		})
	}
}

// TestRepoTFileChangesRejectsAMalformedChange drives the classify guard at the
// function boundary. A change carrying neither a pre- nor a post-image is what
// go-git calls malformed; a tree diff never emits one, so the guard is
// unreachable through Changes and is exercised where it is written.
func TestRepoTFileChangesRejectsAMalformedChange(t *testing.T) {
	got, err := fileChanges(object.Changes{{}})
	if got != nil {
		t.Fatalf("fileChanges = %+v, want no changes", got)
	}
	repoTWantErr(t, func() error { return err }, "classify tree change")
}

func TestRepoTChangesReportsUnreadableBlobContent(t *testing.T) {
	fixture := repoTStage(t,
		repoTCommit{subject: "feat: add a.go", files: []repoTFile{{path: "a.go", body: repoTBodyV1}}},
		repoTCommit{subject: "feat: extend a.go", files: []repoTFile{{path: "a.go", body: repoTBodyV2}}},
	)
	before := repoTEntryHash(t, repoTCommitObject(t, fixture, fixture.hashes[0]), "a.go")
	after := repoTEntryHash(t, repoTCommitObject(t, fixture, fixture.hashes[1]), "a.go")

	for _, tc := range []struct {
		name     string
		poisoned plumbing.Hash
	}{
		{name: "post-image content", poisoned: after},
		{name: "pre-image content", poisoned: before},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := repoTPoisonStorer{Storer: fixture.repo.Storer, hash: tc.poisoned}
			g := repoTRepoOver(t, fixture, store)
			repoTWantErr(t, func() error {
				_, err := g.Changes(fixture.hashes[1])
				return err
			}, "crgbehavior: read a.go")
		})
	}
}

// repoTEncodable is a hand-built git object a fixture writes into storage.
type repoTEncodable interface {
	Encode(plumbing.EncodedObject) error
}

// repoTStore writes a hand-built object into the fixture's object storage.
// Dangling trees, parents and blobs are real repository states — an
// interrupted fetch or a half-finished gc leaves exactly that — and no
// worktree API produces one.
func repoTStore(t *testing.T, fixture repoTFixture, obj repoTEncodable) plumbing.Hash {
	t.Helper()
	enc := fixture.repo.Storer.NewEncodedObject()
	if err := obj.Encode(enc); err != nil {
		t.Fatalf("encode fixture object: %v", err)
	}
	hash, err := fixture.repo.Storer.SetEncodedObject(enc)
	if err != nil {
		t.Fatalf("store fixture object: %v", err)
	}
	return hash
}

// repoTCraftCommit stores a commit object with the given tree and parents.
func repoTCraftCommit(t *testing.T, fixture repoTFixture, what string, tree plumbing.Hash, parents ...plumbing.Hash) plumbing.Hash {
	t.Helper()
	return repoTStore(t, fixture, &object.Commit{
		Author: repoTSignature, Committer: repoTSignature,
		Message:      "craft: " + what + "\n",
		TreeHash:     tree,
		ParentHashes: parents,
	})
}

// repoTCommitObject reads a fixture commit.
func repoTCommitObject(t *testing.T, fixture repoTFixture, hash plumbing.Hash) *object.Commit {
	t.Helper()
	commit, err := fixture.repo.CommitObject(hash)
	if err != nil {
		t.Fatalf("CommitObject(%s): %v", short(hash.String()), err)
	}
	return commit
}

// repoTEntryHash is the object one of a commit's paths points at.
func repoTEntryHash(t *testing.T, commit *object.Commit, path string) plumbing.Hash {
	t.Helper()
	tree, err := commit.Tree()
	if err != nil {
		t.Fatalf("Tree of %s: %v", short(commit.Hash.String()), err)
	}
	entry, err := tree.FindEntry(path)
	if err != nil {
		t.Fatalf("FindEntry(%s): %v", path, err)
	}
	return entry.Hash
}

// TestRepoTChangesReportsAnUndiffableTree drives the diff failure with a
// subtree that reads once and then does not. go-git's tree walker silently
// truncates the walk when a subtree object is missing outright, so a dangling
// subtree diffs to no changes at all; only storage that degrades between the
// enumeration read and the descent read makes DiffTree itself fail.
func TestRepoTChangesReportsAnUndiffableTree(t *testing.T) {
	fixture := repoTStage(t, repoTCommit{
		subject: "feat: seed a subdirectory",
		files:   []repoTFile{{path: "pkg/b.go", body: repoTBodyOther}},
	})
	root := fixture.hashes[0]
	subtree := repoTEntryHash(t, repoTCommitObject(t, fixture, root), "pkg")
	store := repoTFaultStorer{Storer: fixture.repo.Storer, hash: subtree, reads: new(int)}

	g := repoTRepoOver(t, fixture, store)
	repoTWantErr(t, func() error {
		_, err := g.Changes(root)
		return err
	}, "diff "+short(root.String())+" against its parent")
}
