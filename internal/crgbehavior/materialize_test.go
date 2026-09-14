package crgbehavior

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"

	"github.com/AGOrcha/dot-agents/internal/gitwt"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// fakeWorktrees records the worktree lifecycle calls the materializer makes.
// The gate runs NO git subprocess: the per-commit checkout goes through the
// native internal/gitwt manager, and this double stands in for it.
type fakeWorktrees struct {
	calls []string
	fail  map[string]error
}

func (w *fakeWorktrees) AddDetached(name, path string, commit plumbing.Hash) error {
	w.calls = append(w.calls, "add-detached "+name+" "+path+" "+commit.String())
	return w.fail["add"]
}

func (w *fakeWorktrees) Remove(name, path string) error {
	w.calls = append(w.calls, "remove "+name+" "+path)
	return w.fail["remove"]
}

func (w *fakeWorktrees) AddBranch(string, string, plumbing.Hash) error { return nil }
func (w *fakeWorktrees) List() ([]string, error)                       { return nil, nil }
func (w *fakeWorktrees) Prune() ([]string, error)                      { return nil, nil }
func (w *fakeWorktrees) Open(string) (gitwt.Worktree, error)           { return nil, nil }
func (w *fakeWorktrees) RecordBaseRef(string, plumbing.Hash) error     { return nil }
func (w *fakeWorktrees) BaseRef(string) (plumbing.Hash, error)         { return plumbing.ZeroHash, nil }

// materializerWith binds a materializer to a recording worktree manager.
func materializerWith(t *testing.T, worktrees *fakeWorktrees) *WorktreeMaterializer {
	t.Helper()
	return &WorktreeMaterializer{
		WorkDir: filepath.Join(t.TempDir(), "worktrees"), Release: testRelease(),
		Depth: DefaultDepth, MaxResults: DefaultMaxResults, worktrees: worktrees,
	}
}

// Each historical task is replayed against a DETACHED worktree at its own
// commit. A worktree left by an interrupted run is cleared first, so a second
// run cannot silently build the wrong tree.
func TestAddWorktreeChecksOutTheTaskCommitDetached(t *testing.T) {
	worktrees := &fakeWorktrees{}
	m := materializerWith(t, worktrees)
	commit := hashOf("1")
	name := "crg-" + short(commit.String())
	dir := filepath.Join(m.WorkDir, name)
	if err := m.addWorktree(name, dir, commit); err != nil {
		t.Fatalf("addWorktree: %v", err)
	}
	if len(worktrees.calls) != 2 {
		t.Fatalf("worktree calls = %v, want a stale clear then a detached add", worktrees.calls)
	}
	if !strings.HasPrefix(worktrees.calls[0], "remove "+name+" ") {
		t.Fatalf("first call = %q, want the stale worktree cleared", worktrees.calls[0])
	}
	want := "add-detached " + name + " " + dir + " " + commit.String()
	if worktrees.calls[1] != want {
		t.Fatalf("second call = %q, want %q", worktrees.calls[1], want)
	}
}

// A first run has no worktree to clear; that is not a failure.
func TestAddWorktreeToleratesAnAbsentPriorWorktree(t *testing.T) {
	worktrees := &fakeWorktrees{fail: map[string]error{"remove": gitwt.ErrWorktreeNotFound}}
	m := materializerWith(t, worktrees)
	if err := m.addWorktree("crg-a", filepath.Join(m.WorkDir, "crg-a"), hashOf("1")); err != nil {
		t.Fatalf("addWorktree on a first run: %v", err)
	}
}

// A commit that cannot be checked out is a failure naming the commit, not a
// silently skipped task.
func TestAddWorktreeReportsAFailedCheckout(t *testing.T) {
	worktrees := &fakeWorktrees{fail: map[string]error{"add": errors.New("unknown revision")}}
	m := materializerWith(t, worktrees)
	commit := hashOf("1")
	err := m.addWorktree("crg-a", filepath.Join(m.WorkDir, "crg-a"), commit)
	if err == nil || !strings.Contains(err.Error(), short(commit.String())) {
		t.Fatalf("addWorktree error = %v, want it to name the commit", err)
	}
}

// The worktree is torn down after every task; a real teardown failure is
// surfaced rather than left to poison the next commit's build.
func TestRemoveWorktreeSurfacesTeardownFailures(t *testing.T) {
	worktrees := &fakeWorktrees{fail: map[string]error{"remove": errors.New("locked")}}
	m := materializerWith(t, worktrees)
	if err := m.removeWorktree("crg-a", "/tmp/somewhere"); err == nil {
		t.Fatal("a failed worktree teardown was swallowed")
	}
}

// The release's own FTS5 search answers every changed identifier, once, at
// materialization time — the gate never re-approximates it later.
func TestSearchIdentifiersUsesTheReleaseIndex(t *testing.T) {
	store := openGraph(t, twoFlowSeed)
	got, err := searchIdentifiers(store, []string{"Entry", "Missing"})
	if err != nil {
		t.Fatalf("searchIdentifiers: %v", err)
	}
	if !equalStrings(got["Entry"], []string{"pkg/a.go::Entry"}) {
		t.Fatalf("search(Entry) = %v, want the indexed symbol", got["Entry"])
	}
	if len(got["Missing"]) != 0 {
		t.Fatalf("search(Missing) = %v, want no hits recorded rather than none attempted", got["Missing"])
	}
	if len(got) != 2 {
		t.Fatalf("recorded %d search results, want one per changed identifier", len(got))
	}
}

// mlTFixtureFile is the one tracked file the staged repository commits.
const mlTFixtureFile = "entry.go"

// mlTRepoFixture stages a real single-commit repository IN PROCESS with go-git
// — the gate runs no git subprocess — and returns its root and that commit.
func mlTRepoFixture(t *testing.T) (string, plumbing.Hash) {
	t.Helper()
	root := t.TempDir()
	repo, err := git.PlainInit(root, false)
	if err != nil {
		t.Fatalf("PlainInit: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, mlTFixtureFile),
		[]byte("package a\n\nfunc Entry() {}\n"), 0o644); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}
	if _, err := wt.Add(mlTFixtureFile); err != nil {
		t.Fatalf("Add: %v", err)
	}
	who := &object.Signature{Name: "gate", Email: "gate@example", When: time.Unix(1735689600, 0).UTC()}
	commit, err := wt.Commit("seed the pinned corpus", &git.CommitOptions{Author: who, Committer: who})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	return root, commit
}

// mlTNativeMaterializer binds a materializer to a staged repository through the
// REAL native worktree manager, so the worktree lifecycle is exercised rather
// than recorded.
func mlTNativeMaterializer(t *testing.T) (*WorktreeMaterializer, plumbing.Hash) {
	t.Helper()
	root, commit := mlTRepoFixture(t)
	m, err := NewWorktreeMaterializer(root, filepath.Join(t.TempDir(), "worktrees"),
		testRelease(), DefaultDepth, DefaultMaxResults, nil)
	if err != nil {
		t.Fatalf("NewWorktreeMaterializer: %v", err)
	}
	return m, commit
}

// mlTRegistered reports whether name is one of the repository's linked
// worktrees.
func mlTRegistered(t *testing.T, m *WorktreeMaterializer, name string) bool {
	t.Helper()
	names, err := m.worktrees.List()
	if err != nil {
		t.Fatalf("list worktrees: %v", err)
	}
	for _, got := range names {
		if got == name {
			return true
		}
	}
	return false
}

// mlTNoCheckout asserts the recorded lifecycle never reached a checkout.
func mlTNoCheckout(t *testing.T, worktrees *fakeWorktrees) {
	t.Helper()
	for _, call := range worktrees.calls {
		if strings.HasPrefix(call, "add-detached") {
			t.Fatalf("worktree calls = %v, want no checkout after a failed clear", worktrees.calls)
		}
	}
}

// A materializer records the parameters every per-commit build is driven with.
// A run that silently defaulted the query budget would compare the two sides at
// a depth neither review skill uses.
func TestMlTNewWorktreeMaterializerBindsTheRepositoryAndQueryBudget(t *testing.T) {
	root, _ := mlTRepoFixture(t)
	work := filepath.Join(t.TempDir(), "worktrees")
	m, err := NewWorktreeMaterializer(root, work, testRelease(), 3, 17, nil)
	if err != nil {
		t.Fatalf("NewWorktreeMaterializer: %v", err)
	}
	if m.RepoRoot != root || m.WorkDir != work {
		t.Fatalf("bound to (%q,%q), want (%q,%q)", m.RepoRoot, m.WorkDir, root, work)
	}
	if m.Depth != 3 || m.MaxResults != 17 {
		t.Fatalf("query budget = (%d,%d), want the caller's (3,17)", m.Depth, m.MaxResults)
	}
	if m.Release.Version != PinnedVersion {
		t.Fatalf("release = %q, want the pinned %q", m.Release.Version, PinnedVersion)
	}
	if m.repo == nil || m.worktrees == nil {
		t.Fatal("the materializer left its repository or worktree seam unbound")
	}
}

// A root that is not inside a repository cannot pin a corpus at all; the
// failure names the root instead of surfacing later as an empty comparison.
func TestMlTNewWorktreeMaterializerRejectsANonRepository(t *testing.T) {
	root := t.TempDir()
	_, err := NewWorktreeMaterializer(root, filepath.Join(root, "worktrees"),
		testRelease(), DefaultDepth, DefaultMaxResults, nil)
	if err == nil || !strings.Contains(err.Error(), root) {
		t.Fatalf("NewWorktreeMaterializer error = %v, want it to name %q", err, root)
	}
}

// The worktree manager must own the repository's MAIN worktree. A subdirectory
// still RESOLVES a repository (go-git walks up for that), so binding is where
// the mismatch has to be caught — otherwise it would surface per commit, deep
// inside a run that has already spent minutes per task.
func TestMlTNewWorktreeMaterializerRejectsARootThatIsNotTheMainWorktree(t *testing.T) {
	root, _ := mlTRepoFixture(t)
	sub := filepath.Join(root, "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("stage subdirectory: %v", err)
	}
	_, err := NewWorktreeMaterializer(sub, filepath.Join(t.TempDir(), "worktrees"),
		testRelease(), DefaultDepth, DefaultMaxResults, nil)
	if err == nil || !strings.Contains(err.Error(), "worktree manager") {
		t.Fatalf("NewWorktreeMaterializer error = %v, want the worktree manager to refuse %q", err, sub)
	}
}

// Driven through the real native manager: the commit's tracked content lands in
// an ISOLATED tree, the worktree is registered, and teardown removes both the
// directory and the registration — the invariant a multi-commit run depends on.
func TestMlTAddWorktreeMaterializesTheCommitThroughTheNativeManager(t *testing.T) {
	m, commit := mlTNativeMaterializer(t)
	name := "crg-" + short(commit.String())
	dir := filepath.Join(m.WorkDir, name)
	if err := m.addWorktree(name, dir, commit); err != nil {
		t.Fatalf("addWorktree: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, mlTFixtureFile)); err != nil {
		t.Fatalf("the commit's tracked file is missing from the isolated worktree: %v", err)
	}
	if !mlTRegistered(t, m, name) {
		t.Fatalf("worktree %q was checked out without being registered", name)
	}
	if err := m.removeWorktree(name, dir); err != nil {
		t.Fatalf("removeWorktree: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("the isolated worktree survived teardown: %v", err)
	}
	if mlTRegistered(t, m, name) {
		t.Fatalf("worktree %q stayed registered after teardown", name)
	}
}

// A run that died before its worktree was registered leaves a DIRECTORY behind.
// The next run clears it first: without that, the checkout either fails outright
// or the gate builds whatever the interrupted run happened to leave on disk.
func TestMlTAddWorktreeClearsALeftoverDirectoryFromAnInterruptedRun(t *testing.T) {
	m, commit := mlTNativeMaterializer(t)
	name := "crg-leftover"
	dir := filepath.Join(m.WorkDir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("stage leftover: %v", err)
	}
	stale := filepath.Join(dir, "half-written.go")
	if err := os.WriteFile(stale, []byte("package wrong\n"), 0o644); err != nil {
		t.Fatalf("stage leftover file: %v", err)
	}
	if err := m.addWorktree(name, dir, commit); err != nil {
		t.Fatalf("addWorktree over a leftover directory: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("a leftover from an interrupted run survived into the build: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, mlTFixtureFile)); err != nil {
		t.Fatalf("the commit was not checked out over the cleared directory: %v", err)
	}
}

// The FIRST run has nothing to tear down. A never-registered worktree is the
// first-run state, not a failure — reporting it would fail every fresh corpus.
func TestMlTRemoveWorktreeToleratesANeverRegisteredWorktree(t *testing.T) {
	m, _ := mlTNativeMaterializer(t)
	if err := m.removeWorktree("crg-never", filepath.Join(m.WorkDir, "crg-never")); err != nil {
		t.Fatalf("removeWorktree on a first run: %v", err)
	}
}

// Only ABSENCE is tolerated. Any other refusal from the native manager is
// surfaced, so a worktree it will not touch cannot be mistaken for a clean slate.
func TestMlTRemoveWorktreeSurfacesANonAbsenceFailureFromTheNativeManager(t *testing.T) {
	m, _ := mlTNativeMaterializer(t)
	dir := filepath.Join(m.WorkDir, "crg bad")
	err := m.removeWorktree("crg bad", dir)
	if !errors.Is(err, gitwt.ErrInvalidName) {
		t.Fatalf("removeWorktree error = %v, want the native manager's name rejection", err)
	}
	if !strings.Contains(err.Error(), dir) {
		t.Fatalf("removeWorktree error = %q, want it to name %q", err, dir)
	}
}

// The per-commit worktree parent must be creatable before anything is torn down
// or checked out; a work directory under a regular file is reported as such.
func TestMlTAddWorktreeReportsAnUncreatableWorkDir(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatalf("stage blocker: %v", err)
	}
	worktrees := &fakeWorktrees{}
	m := materializerWith(t, worktrees)
	m.WorkDir = filepath.Join(blocker, "worktrees")
	err := m.addWorktree("crg-a", filepath.Join(m.WorkDir, "crg-a"), hashOf("1"))
	if err == nil || !strings.Contains(err.Error(), m.WorkDir) {
		t.Fatalf("addWorktree error = %v, want it to name %q", err, m.WorkDir)
	}
	if len(worktrees.calls) != 0 {
		t.Fatalf("worktree calls = %v, want none before the parent exists", worktrees.calls)
	}
}

// A stale worktree that cannot be UNREGISTERED stops the task: checking the
// commit out over a registration the manager still owns would build a tree the
// gate cannot attribute to that SHA.
func TestMlTAddWorktreeStopsWhenAStaleRegistrationCannotBeCleared(t *testing.T) {
	worktrees := &fakeWorktrees{fail: map[string]error{"remove": errors.New("admin dir is locked")}}
	m := materializerWith(t, worktrees)
	if err := m.addWorktree("crg-a", filepath.Join(m.WorkDir, "crg-a"), hashOf("1")); err == nil {
		t.Fatal("addWorktree proceeded over a stale registration it could not clear")
	}
	mlTNoCheckout(t, worktrees)
}

// A leftover directory that cannot be REMOVED is reported too — building over
// it is the one outcome the clearing step exists to prevent.
func TestMlTAddWorktreeReportsAnUnclearableLeftover(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("directory permissions do not gate removal for this platform/user")
	}
	worktrees := &fakeWorktrees{fail: map[string]error{"remove": gitwt.ErrWorktreeNotFound}}
	m := materializerWith(t, worktrees)
	dir := filepath.Join(m.WorkDir, "crg-a")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("stage leftover: %v", err)
	}
	if err := os.Chmod(m.WorkDir, 0o500); err != nil {
		t.Fatalf("seal the worktree parent: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(m.WorkDir, 0o755) })
	err := m.addWorktree("crg-a", dir, hashOf("1"))
	if err == nil || !strings.Contains(err.Error(), dir) {
		t.Fatalf("addWorktree error = %v, want it to name the leftover %q", err, dir)
	}
	mlTNoCheckout(t, worktrees)
}

// Materialization takes minutes per commit, so progress goes to the caller's
// sink when there is one — formatted, not as a raw format string — and a
// materializer without a sink must not panic.
func TestMlTLogfReportsProgressOnlyWhenAsked(t *testing.T) {
	quiet := &WorktreeMaterializer{}
	quiet.logf("building %s", PackageName)

	var lines []string
	loud := &WorktreeMaterializer{Log: func(line string) { lines = append(lines, line) }}
	loud.logf("building %s %s graph at %s", PackageName, PinnedVersion, "abc123def456")
	want := "building " + PackageName + " " + PinnedVersion + " graph at abc123def456"
	if len(lines) != 1 || lines[0] != want {
		t.Fatalf("progress lines = %v, want exactly [%q]", lines, want)
	}
}

// A search the release could not ANSWER is an error, not an empty result set:
// recording "no hits" would report a false FTS divergence for the task.
func TestMlTSearchIdentifiersSurfacesAFailedSearch(t *testing.T) {
	store := openGraph(t, twoFlowSeed)
	if err := store.Close(); err != nil {
		t.Fatalf("close the fixture store: %v", err)
	}
	got, err := searchIdentifiers(store, []string{"Entry"})
	if err == nil {
		t.Fatalf("searchIdentifiers = %v over a closed store, want the failure surfaced", got)
	}
	if got != nil {
		t.Fatalf("searchIdentifiers returned %v alongside a failure", got)
	}
}

// Nothing is materialized before the commit RESOLVES: an unpinnable revision
// must not create a worktree or spend a full graph build proving it is unusable.
func TestMlTMaterializeFailsBeforeAnyWorktreeWhenTheCommitCannotResolve(t *testing.T) {
	m, _ := mlTNativeMaterializer(t)
	missing := hashOf("0").String()
	state, err := m.Materialize(Task{Commit: missing})
	if err == nil {
		t.Fatal("an unresolvable commit was materialized")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Fatalf("Materialize error = %q, want it to name the revision", err)
	}
	if state.Commit != "" {
		t.Fatalf("Materialize returned state %+v for an unresolvable commit", state)
	}
	if _, statErr := os.Stat(m.WorkDir); !os.IsNotExist(statErr) {
		t.Fatalf("the worktree parent was created for an unresolvable commit: %v", statErr)
	}
}

// A machine without the pinned release cannot replay the task — but the
// isolated worktree it already created is still torn down, directory AND
// registration, because a corpus run drives dozens of commits in sequence.
func TestMlTMaterializeTearsTheWorktreeDownWhenTheBridgeIsUnavailable(t *testing.T) {
	m, commit := mlTNativeMaterializer(t)
	t.Setenv("PATH", t.TempDir())
	name := "crg-" + short(commit.String())
	if _, err := m.Materialize(Task{Commit: commit.String()}); !errors.Is(err, ErrBridgeUnavailable) {
		t.Fatalf("Materialize error = %v, want ErrBridgeUnavailable", err)
	}
	if _, err := os.Stat(filepath.Join(m.WorkDir, name)); !os.IsNotExist(err) {
		t.Fatalf("the isolated worktree was left behind: %v", err)
	}
	if mlTRegistered(t, m, name) {
		t.Fatalf("worktree %q stayed registered after a failed task", name)
	}
}

// A discovered release whose interpreter cannot import the package fails at the
// BUILD — after the worktree exists. The task reports that build failure rather
// than any comparison result, announces the commit it was working on, and still
// leaves no worktree behind.
func TestMlTMaterializeReportsAFailedBuildAndStillTearsDown(t *testing.T) {
	m, commit := mlTNativeMaterializer(t)
	mlTStageShim(t, filepath.Join(m.WorkDir, ".venv", "bin"))
	var lines []string
	m.Log = func(line string) { lines = append(lines, line) }
	if _, err := m.Materialize(Task{Commit: commit.String()}); err == nil {
		t.Fatal("a failed pinned-release build was reported as a materialized task")
	} else if !strings.Contains(err.Error(), "build the pinned release graph") {
		t.Fatalf("Materialize error = %q, want the build failure", err)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], PinnedVersion) ||
		!strings.Contains(lines[0], short(commit.String())) {
		t.Fatalf("progress lines = %v, want one line naming the release and the commit", lines)
	}
	if _, err := os.Stat(filepath.Join(m.WorkDir, "crg-"+short(commit.String()))); !os.IsNotExist(err) {
		t.Fatalf("the isolated worktree was left behind after a failed build: %v", err)
	}
}

// mlTLateFailingWorktrees fails teardown only AFTER the worktree was added, so
// the pre-checkout clear succeeds and it is the DEFERRED teardown that fails.
type mlTLateFailingWorktrees struct {
	fakeWorktrees
	added bool
}

func (w *mlTLateFailingWorktrees) AddDetached(name, path string, commit plumbing.Hash) error {
	w.added = true
	return w.fakeWorktrees.AddDetached(name, path, commit)
}

func (w *mlTLateFailingWorktrees) Remove(name, path string) error {
	if w.added {
		return errors.New("worktree admin dir is locked")
	}
	return w.fakeWorktrees.Remove(name, path)
}

// mlTStubRepo resolves every revision to one hash, so Materialize reaches the
// worktree lifecycle without staging a repository.
type mlTStubRepo struct{ head plumbing.Hash }

func (r mlTStubRepo) Resolve(string) (plumbing.Hash, error)       { return r.head, nil }
func (r mlTStubRepo) Window(string, int) ([]plumbing.Hash, error) { return nil, nil }
func (r mlTStubRepo) Subject(plumbing.Hash) (string, error)       { return "", nil }
func (r mlTStubRepo) Changes(plumbing.Hash) ([]FileChange, error) { return nil, nil }

// A task that failed for its OWN reason keeps that diagnosis: a teardown
// failure on the way out must not overwrite why the comparison could not run,
// or every unavailable-bridge run would be reported as a locked worktree.
func TestMlTMaterializeKeepsTheTaskFailureWhenTeardownAlsoFails(t *testing.T) {
	worktrees := &mlTLateFailingWorktrees{}
	m := &WorktreeMaterializer{
		WorkDir: filepath.Join(t.TempDir(), "worktrees"), Release: testRelease(),
		Depth: DefaultDepth, MaxResults: DefaultMaxResults,
		repo: mlTStubRepo{head: hashOf("1")}, worktrees: worktrees,
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := m.Materialize(Task{Commit: hashOf("1").String()}); !errors.Is(err, ErrBridgeUnavailable) {
		t.Fatalf("Materialize error = %v, want the task's own ErrBridgeUnavailable to survive teardown", err)
	}
	if !worktrees.added {
		t.Fatal("the task failed before a worktree was ever created")
	}
}

// A task whose isolated worktree cannot be CHECKED OUT is reported as that, and
// the deferred teardown is not armed for a worktree that was never created —
// the gate never falls back to building whatever tree is on disk.
func TestMlTMaterializeStopsWhenTheWorktreeCannotBeCheckedOut(t *testing.T) {
	commit := hashOf("2")
	worktrees := &fakeWorktrees{fail: map[string]error{"add": errors.New("unknown revision")}}
	m := &WorktreeMaterializer{
		WorkDir: filepath.Join(t.TempDir(), "worktrees"), Release: testRelease(),
		Depth: DefaultDepth, MaxResults: DefaultMaxResults,
		repo: mlTStubRepo{head: commit}, worktrees: worktrees,
	}
	state, err := m.Materialize(Task{Commit: commit.String()})
	if err == nil || !strings.Contains(err.Error(), short(commit.String())) {
		t.Fatalf("Materialize error = %v, want it to name the commit it could not check out", err)
	}
	if state.Commit != "" {
		t.Fatalf("Materialize returned state %+v for a task it never checked out", state)
	}
	if len(worktrees.calls) != 2 {
		t.Fatalf("worktree calls = %v, want the stale clear and the failed add only", worktrees.calls)
	}
}

// A lifecycle observation is only meaningful AFTER the release's postprocess
// actually ran. A failed postprocess must not yield a zero observation, which
// would read as "the release rebuilt nothing" — a fabricated staleness verdict.
func TestMlTProbeLifecycleSurfacesAFailedPostprocess(t *testing.T) {
	bridge, root := mlTShimBridge(t)
	m := &WorktreeMaterializer{Release: testRelease()}
	got, err := m.probeLifecycle(bridge, FlowState{})
	if err == nil {
		t.Fatalf("probeLifecycle = %+v over a failed postprocess, want the failure surfaced", got)
	}
	if !strings.Contains(err.Error(), root) {
		t.Fatalf("probeLifecycle error = %q, want it to name the graph root %q", err, root)
	}
}

// mlTRebase moves the shared release-schema fixture's id space onto root, so a
// store opened at root normalizes it exactly as it normalizes a real build.
func mlTRebase(root string) string {
	return strings.ReplaceAll(twoFlowSeed, graphRoot, root)
}

// mlTSeedOutsideRoot leaves the fixture's id space where it was built, which is
// a graph whose paths the store's normalizer must refuse.
func mlTSeedOutsideRoot(string) string { return twoFlowSeed }

// mlTSeedStaleFTS rebases the fixture and then deletes a node WITHOUT touching
// the FTS index, leaving the release's own search index pointing at content
// that is gone — the staleness this package exists to observe.
func mlTSeedStaleFTS(root string) string {
	return mlTRebase(root) + "\nDELETE FROM nodes WHERE id=3;\n"
}

// mlTSeedGraphAt writes a pinned-release-shaped SQLite graph to the path the
// release itself builds to under root, and returns that path.
func mlTSeedGraphAt(t *testing.T, root, seed string) string {
	t.Helper()
	dbPath := graphstore.CRGDBPath(root)
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatalf("stage graph directory: %v", err)
	}
	db, err := sql.Open(sqliteDriver, dbPath)
	if err != nil {
		t.Fatalf("open graph: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(releaseSchemaSQL + seed); err != nil {
		t.Fatalf("seed graph: %v", err)
	}
	return dbPath
}

// mlTReadBuiltState runs the read half of materialization over a graph seeded at
// the release's canonical path under a fresh bridge root.
func mlTReadBuiltState(t *testing.T, seed func(string) string, task Task) (TaskState, error) {
	t.Helper()
	bridge, root := mlTShimBridge(t)
	mlTSeedGraphAt(t, root, seed(root))
	m := &WorktreeMaterializer{Release: testRelease(), Depth: DefaultDepth, MaxResults: DefaultMaxResults}
	return m.readBuiltState(bridge, task)
}

// mlTAssertFailedStage checks which stage's failure came back.
func mlTAssertFailedStage(t *testing.T, err error, wantErr error, wantMsg string) {
	t.Helper()
	if err == nil {
		t.Fatal("readBuiltState reported success over a graph it should have refused")
	}
	if wantErr != nil && !errors.Is(err, wantErr) {
		t.Fatalf("readBuiltState error = %v, want %v", err, wantErr)
	}
	if wantMsg != "" && !strings.Contains(err.Error(), wantMsg) {
		t.Fatalf("readBuiltState error = %q, want it to name %q", err, wantMsg)
	}
}

// readBuiltState reads the PERSISTED views out of the graph the release built,
// then searches the release's own index, and only then issues the live query.
// That order is the whole basis of "read first, probe in place": everything the
// comparison needs is taken from the freshly built graph BEFORE the lifecycle
// probe's postprocess mutates it.
//
// The order is pinned by giving each stage its own failure mode and checking
// which one comes back. If the live query moved ahead of the reads, all three
// cases would report an unavailable bridge instead.
func TestMlTReadBuiltStateReadsTheBuiltGraphBeforeQueryingTheRelease(t *testing.T) {
	task := Task{
		Commit:       hashOf("1").String(),
		ChangedFiles: []string{"pkg/a.go"},
		Identifiers:  []string{"Widget"},
	}
	for _, tc := range []struct {
		stage   string
		seed    func(string) string
		wantErr error
		wantMsg string
	}{
		{stage: "views decode first", seed: mlTSeedOutsideRoot,
			wantErr: ErrOutsideRoot, wantMsg: "is not under"},
		{stage: "then the release index is searched", seed: mlTSeedStaleFTS,
			wantMsg: "nodes_fts"},
		{stage: "and the live query is issued last", seed: mlTRebase,
			wantErr: ErrBridgeUnavailable},
	} {
		t.Run(tc.stage, func(t *testing.T) {
			state, err := mlTReadBuiltState(t, tc.seed, task)
			mlTAssertFailedStage(t, err, tc.wantErr, tc.wantMsg)
			if state.Commit != "" {
				t.Fatalf("readBuiltState returned state %+v alongside a failure", state)
			}
		})
	}
}

// The read half leaves the built graph BYTE-IDENTICAL. That is what lets the
// lifecycle probe run the release's postprocess in place and attribute every
// observed change to the release, with no second build and no database copy.
func TestMlTReadBuiltStateLeavesTheBuiltGraphUntouched(t *testing.T) {
	bridge, root := mlTShimBridge(t)
	dbPath := mlTSeedGraphAt(t, root, mlTRebase(root))
	before := mlTDigest(t, dbPath)
	m := &WorktreeMaterializer{Release: testRelease(), Depth: DefaultDepth, MaxResults: DefaultMaxResults}
	if _, err := m.readBuiltState(bridge, Task{Identifiers: []string{"Entry"}}); err == nil {
		t.Fatal("the live query against an unusable interpreter reported success")
	}
	if after := mlTDigest(t, dbPath); after != before {
		t.Fatal("the read half mutated the graph the lifecycle probe must observe in place")
	}
}

// mlTDigest fingerprints a file's exact bytes.
func mlTDigest(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// A root the release never BUILT has no graph to read. The read half refuses it
// up front, naming the graph it expected, rather than reporting an empty
// comparison that would read as agreement between the two sides.
func TestMlTReadBuiltStateRefusesARootWithNoBuiltGraph(t *testing.T) {
	bridge, root := mlTShimBridge(t)
	m := &WorktreeMaterializer{Release: testRelease(), Depth: DefaultDepth, MaxResults: DefaultMaxResults}
	state, err := m.readBuiltState(bridge, Task{Identifiers: []string{"Entry"}})
	if !errors.Is(err, ErrBridgeUnavailable) {
		t.Fatalf("readBuiltState error = %v, want ErrBridgeUnavailable", err)
	}
	if want := graphstore.CRGDBPath(root); !strings.Contains(err.Error(), want) {
		t.Fatalf("readBuiltState error = %q, want it to name the missing graph %q", err, want)
	}
	if state.Commit != "" {
		t.Fatalf("readBuiltState returned state %+v for a root with no graph", state)
	}
}
