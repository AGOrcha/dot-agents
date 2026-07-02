package sandbox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"go.yaml.in/yaml/v3"

	"github.com/AGOrcha/dot-agents/internal/eval"
	"github.com/AGOrcha/dot-agents/internal/gitwt"
)

// fixture is a temp source repo with one commit plus a worktree sandbox
// rooted at a sibling runs dir.
type fixture struct {
	repoPath string
	runsRoot string
	base     plumbing.Hash
	sb       *worktreeSandbox
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	repoPath, base := initRepo(t, filepath.Join(root, "repo"), true)
	runsRoot := filepath.Join(root, "runs")
	sb, err := NewWorktreeSandbox(Config{RepoPath: repoPath, RunsRoot: runsRoot})
	if err != nil {
		t.Fatalf("NewWorktreeSandbox: %v", err)
	}
	return &fixture{
		repoPath: repoPath,
		runsRoot: runsRoot,
		base:     base,
		sb:       sb.(*worktreeSandbox),
	}
}

// initRepo creates a git repo at path; withCommit adds one README commit.
func initRepo(t *testing.T, path string, withCommit bool) (string, plumbing.Hash) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	repo, err := git.PlainInit(path, false)
	if err != nil {
		t.Fatalf("init repo: %v", err)
	}
	if !withCommit {
		return path, plumbing.ZeroHash
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	readme := filepath.Join(path, "README.md")
	if err := os.WriteFile(readme, []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	if _, err := wt.Add("README.md"); err != nil {
		t.Fatalf("add: %v", err)
	}
	base, err := wt.Commit("initial", &git.CommitOptions{
		Author: &object.Signature{Name: "Test", Email: "t@example.com", When: time.Now()},
	})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	return path, base
}

// testSpec returns a minimal valid TaskSpec with the given task id.
func testSpec(id string) *eval.TaskSpec {
	return &eval.TaskSpec{
		TaskSpecVersion: eval.CurrentTaskSpecVersion,
		TaskID:          id,
		Language:        eval.LanguageGo,
		Difficulty:      eval.DifficultyEasy,
		GeneratedFrom:   eval.GeneratedFrom{Kind: eval.KindKGTemplate},
		Prompt:          "implement the function",
		Verification:    eval.Verification{TestCmd: []string{"go", "test", "./..."}},
	}
}

func mustNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be gone, stat err=%v", path, err)
	}
}

func mustExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}

func TestNewWorktreeSandboxConfig(t *testing.T) {
	if _, err := NewWorktreeSandbox(Config{}); !errors.Is(err, ErrRepoPathRequired) {
		t.Fatalf("empty repo path: got %v, want ErrRepoPathRequired", err)
	}
	if _, err := NewWorktreeSandbox(Config{RepoPath: t.TempDir()}); err == nil {
		t.Fatal("non-repo path: expected error")
	}

	repoPath, _ := initRepo(t, filepath.Join(t.TempDir(), "repo"), true)
	sb, err := NewWorktreeSandbox(Config{RepoPath: repoPath})
	if err != nil {
		t.Fatalf("NewWorktreeSandbox: %v", err)
	}
	ws := sb.(*worktreeSandbox)
	wantRoot := filepath.Join(repoPath, ".agents", "eval", "runs")
	if ws.runsRoot != wantRoot {
		t.Errorf("default runs root = %q, want %q", ws.runsRoot, wantRoot)
	}
	if ws.retention != DefaultRetention {
		t.Errorf("default retention = %v, want %v", ws.retention, DefaultRetention)
	}
}

func TestProvisionCreatesIsolatedWorktree(t *testing.T) {
	f := newFixture(t)
	inst, err := f.sb.Provision(context.Background(), testSpec("kg-go-impl-001"))
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	t.Cleanup(func() { _ = inst.Cleanup() })

	if inst.RunDir != filepath.Join(f.runsRoot, inst.RunID) {
		t.Errorf("RunDir = %q, want under runs root with RunID", inst.RunDir)
	}
	if inst.Workdir != filepath.Join(inst.RunDir, worktreeDirName) {
		t.Errorf("Workdir = %q, want %q", inst.Workdir, filepath.Join(inst.RunDir, worktreeDirName))
	}
	if inst.BaseCommit != f.base.String() {
		t.Errorf("BaseCommit = %q, want %q", inst.BaseCommit, f.base.String())
	}
	mustExist(t, filepath.Join(inst.Workdir, "README.md"))

	home := filepath.Join(inst.RunDir, homeDirName)
	wantEnv := []string{"HOME=" + home, "USERPROFILE=" + home}
	for i, want := range wantEnv {
		if inst.Env[i] != want {
			t.Errorf("Env[%d] = %q, want %q", i, inst.Env[i], want)
		}
	}
	mustExist(t, home)

	data, err := os.ReadFile(filepath.Join(inst.RunDir, markerName))
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	var m marker
	if err := yaml.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal marker: %v", err)
	}
	if m.RunID != inst.RunID || m.WorktreeName != gitwt.SafeName(inst.RunID) || m.BaseCommit != f.base.String() {
		t.Errorf("marker = %+v, want run %q / worktree %q / base %q", m, inst.RunID, gitwt.SafeName(inst.RunID), f.base.String())
	}
	if _, err := time.Parse(time.RFC3339, m.ProvisionedAt); err != nil {
		t.Errorf("marker provisioned_at %q not RFC3339: %v", m.ProvisionedAt, err)
	}

	// Writes in the sandbox never appear in the operator's tree.
	if err := os.WriteFile(filepath.Join(inst.Workdir, "agent-output.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write in sandbox: %v", err)
	}
	mustNotExist(t, filepath.Join(f.repoPath, "agent-output.txt"))
}

func TestProvisionInputValidation(t *testing.T) {
	f := newFixture(t)
	if _, err := f.sb.Provision(context.Background(), nil); !errors.Is(err, ErrNilTaskSpec) {
		t.Fatalf("nil spec: got %v, want ErrNilTaskSpec", err)
	}
	if _, err := f.sb.Provision(context.Background(), testSpec("   ")); !errors.Is(err, ErrEmptyTaskID) {
		t.Fatalf("empty task id: got %v, want ErrEmptyTaskID", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.sb.Provision(ctx, testSpec("t")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled ctx: got %v, want context.Canceled", err)
	}
}

func TestProvisionHeadErrors(t *testing.T) {
	// A repo with no commits has no resolvable HEAD.
	repoPath, _ := initRepo(t, filepath.Join(t.TempDir(), "empty"), false)
	sb, err := NewWorktreeSandbox(Config{RepoPath: repoPath, RunsRoot: t.TempDir()})
	if err != nil {
		t.Fatalf("NewWorktreeSandbox: %v", err)
	}
	if _, err := sb.Provision(context.Background(), testSpec("t")); err == nil || !strings.Contains(err.Error(), "resolve HEAD") {
		t.Fatalf("no-commit repo: got %v, want resolve HEAD error", err)
	}

	// A repo deleted after construction fails at open time.
	f := newFixture(t)
	if err := os.RemoveAll(filepath.Join(f.repoPath, ".git")); err != nil {
		t.Fatalf("remove .git: %v", err)
	}
	if _, err := f.sb.Provision(context.Background(), testSpec("t")); err == nil || !strings.Contains(err.Error(), "open repo") {
		t.Fatalf("deleted repo: got %v, want open repo error", err)
	}
}

func TestProvisionRunIDRandFailure(t *testing.T) {
	f := newFixture(t)
	f.sb.randRead = func([]byte) (int, error) { return 0, errors.New("entropy down") }
	if _, err := f.sb.Provision(context.Background(), testSpec("t")); err == nil || !strings.Contains(err.Error(), "derive run id") {
		t.Fatalf("rand failure: got %v, want derive run id error", err)
	}
	// A short read without an error must fail loudly too — a silently
	// low-entropy suffix is what would make claim collisions real.
	f.sb.randRead = func(b []byte) (int, error) { return len(b) - 1, nil }
	if _, err := f.sb.Provision(context.Background(), testSpec("t")); err == nil || !strings.Contains(err.Error(), "short entropy read") {
		t.Fatalf("short read: got %v, want short entropy read error", err)
	}
}

// mustLeakNothing asserts the runs root holds no run dirs and the manager
// tracks no worktrees — the post-rollback invariant.
func mustLeakNothing(t *testing.T, f *fixture) {
	t.Helper()
	if entries, err := os.ReadDir(f.runsRoot); err == nil && len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("rollback leaked run dirs: %v", names)
	}
	names, err := f.sb.mgr.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(names) != 0 {
		t.Fatalf("rollback leaked worktrees: %v", names)
	}
}

func TestProvisionAddWorktreeFailureRollsBack(t *testing.T) {
	f := newFixture(t)
	f.sb.addWorktree = func(string, string, plumbing.Hash) error {
		return errors.New("checkout wedged")
	}
	if _, err := f.sb.Provision(context.Background(), testSpec("add-fails")); err == nil || !strings.Contains(err.Error(), "provision worktree") {
		t.Fatalf("add failure: got %v, want provision worktree error", err)
	}
	mustLeakNothing(t, f)
}

func TestProvisionMarkerFailureRollsBack(t *testing.T) {
	f := newFixture(t)
	// Let the worktree add succeed, then plant a directory at the marker
	// path so the atomic marker write fails and the full rollback runs.
	realAdd := f.sb.addWorktree
	f.sb.addWorktree = func(name, path string, base plumbing.Hash) error {
		if err := realAdd(name, path, base); err != nil {
			return err
		}
		return os.Mkdir(filepath.Join(filepath.Dir(path), markerName), 0o755)
	}
	if _, err := f.sb.Provision(context.Background(), testSpec("marker-fails")); err == nil || !strings.Contains(err.Error(), "write marker") {
		t.Fatalf("marker failure: got %v, want write marker error", err)
	}
	mustLeakNothing(t, f)
}

// pinIdentity fixes the sandbox's time and randomness seams so the next
// Provision derives exactly the returned run ID.
func pinIdentity(sb *worktreeSandbox, taskID string) string {
	sb.now = func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }
	sb.randRead = func(b []byte) (int, error) {
		for i := range b {
			b[i] = 0xcd
		}
		return len(b), nil
	}
	return sanitizeID(taskID) + "-20260102T030405-cdcdcdcd"
}

func TestProvisionRunsRootFailure(t *testing.T) {
	f := newFixture(t)
	// A file where the runs root must go makes the claim's MkdirAll fail.
	if err := os.WriteFile(f.runsRoot, []byte("x"), 0o644); err != nil {
		t.Fatalf("plant runs-root file: %v", err)
	}
	if _, err := f.sb.Provision(context.Background(), testSpec("root-blocked")); err == nil || !strings.Contains(err.Error(), "create runs root") {
		t.Fatalf("blocked root: got %v, want create runs root error", err)
	}
}

func TestProvisionClaimContention(t *testing.T) {
	f := newFixture(t)
	pinIdentity(f.sb, "collide")
	inst, err := f.sb.Provision(context.Background(), testSpec("collide"))
	if err != nil {
		t.Fatalf("first Provision: %v", err)
	}
	t.Cleanup(func() { _ = inst.Cleanup() })
	// The pinned seams re-derive the same run ID on every retry, so the
	// atomic claim rejects the twin after the bounded attempts — without
	// ever touching the live first run.
	_, err = f.sb.Provision(context.Background(), testSpec("collide"))
	if err == nil || !strings.Contains(err.Error(), "no unique run id") || !errors.Is(err, os.ErrExist) {
		t.Fatalf("contention: got %v, want bounded-claim failure wrapping os.ErrExist", err)
	}
	mustExist(t, filepath.Join(inst.Workdir, "README.md"))
	mustExist(t, filepath.Join(inst.RunDir, homeDirName))
	mustExist(t, filepath.Join(inst.RunDir, markerName))
}

func TestProvisionClaimRetriesToFreshID(t *testing.T) {
	f := newFixture(t)
	f.sb.now = func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }
	calls := 0
	f.sb.randRead = func(b []byte) (int, error) {
		calls++
		fill := byte(0xaa)
		if calls >= 3 {
			fill = 0xbb // entropy recovers on the second provision's retry
		}
		for i := range b {
			b[i] = fill
		}
		return len(b), nil
	}
	first, err := f.sb.Provision(context.Background(), testSpec("retry"))
	if err != nil {
		t.Fatalf("first Provision: %v", err)
	}
	t.Cleanup(func() { _ = first.Cleanup() })
	second, err := f.sb.Provision(context.Background(), testSpec("retry"))
	if err != nil {
		t.Fatalf("second Provision (should retry to a fresh id): %v", err)
	}
	t.Cleanup(func() { _ = second.Cleanup() })
	if first.RunID == second.RunID {
		t.Fatalf("retry reused run id %q", first.RunID)
	}
	mustExist(t, filepath.Join(first.Workdir, "README.md"))
	mustExist(t, filepath.Join(second.Workdir, "README.md"))
}

func TestProvisionRunDirCollision(t *testing.T) {
	f := newFixture(t)
	runID := pinIdentity(f.sb, "retained")
	// A retained run dir (sidecars only, claim long released) matching a
	// newly claimed id — only reachable via clock rewind — must never be
	// built into or rolled back over.
	retained := filepath.Join(f.runsRoot, runID, "eval-run.yaml")
	if err := os.MkdirAll(filepath.Dir(retained), 0o755); err != nil {
		t.Fatalf("mkdir retained: %v", err)
	}
	if err := os.WriteFile(retained, []byte("run: old\n"), 0o644); err != nil {
		t.Fatalf("write retained sidecar: %v", err)
	}
	if _, err := f.sb.Provision(context.Background(), testSpec("retained")); err == nil || !strings.Contains(err.Error(), "run id collision") {
		t.Fatalf("collision: got %v, want run id collision error", err)
	}
	mustExist(t, retained)                                        // retained data untouched
	mustNotExist(t, filepath.Join(f.runsRoot, runID+claimSuffix)) // claim released
}

func TestCleanupPreservesSidecars(t *testing.T) {
	f := newFixture(t)
	inst, err := f.sb.Provision(context.Background(), testSpec("cleanup-task"))
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	sidecar := filepath.Join(inst.RunDir, "taskspec.yaml")
	if err := os.WriteFile(sidecar, []byte("task_spec_version: 1\n"), 0o644); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}
	if err := inst.Cleanup(); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	mustNotExist(t, inst.Workdir)
	mustNotExist(t, filepath.Join(inst.RunDir, homeDirName))
	mustNotExist(t, filepath.Join(inst.RunDir, markerName))
	mustNotExist(t, filepath.Join(f.runsRoot, inst.RunID+claimSuffix))
	mustExist(t, sidecar)
	if err := inst.Cleanup(); err != nil {
		t.Fatalf("second Cleanup: %v", err)
	}
	// A zero-value instance's Cleanup is a safe no-op.
	if err := (&Instance{}).Cleanup(); err != nil {
		t.Fatalf("zero-value Cleanup: %v", err)
	}
}

// TestConcurrentProvisionIsolation is the R4 requirement-R4 gate: two
// simultaneous Provisions cannot see each other's writes, and neither
// touches the operator's tree.
func TestConcurrentProvisionIsolation(t *testing.T) {
	f := newFixture(t)
	var (
		wg    sync.WaitGroup
		insts [2]*Instance
		errs  [2]error
	)
	for i := range insts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			insts[i], errs[i] = f.sb.Provision(context.Background(), testSpec(fmt.Sprintf("concurrent-%d", i)))
		}()
	}
	wg.Wait()
	for i := range insts {
		if errs[i] != nil {
			t.Fatalf("Provision %d: %v", i, errs[i])
		}
		t.Cleanup(func() { _ = insts[i].Cleanup() })
		name := fmt.Sprintf("only-in-%d.txt", i)
		if err := os.WriteFile(filepath.Join(insts[i].Workdir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("write in sandbox %d: %v", i, err)
		}
	}
	if insts[0].RunID == insts[1].RunID {
		t.Fatalf("run IDs collide: %q", insts[0].RunID)
	}
	for i := range insts {
		other := 1 - i
		mustExist(t, filepath.Join(insts[i].Workdir, fmt.Sprintf("only-in-%d.txt", i)))
		mustNotExist(t, filepath.Join(insts[i].Workdir, fmt.Sprintf("only-in-%d.txt", other)))
		mustNotExist(t, filepath.Join(f.repoPath, fmt.Sprintf("only-in-%d.txt", i)))
	}
}

// rewriteMarkerAge rewrites a run's marker with the given provisioned_at.
func rewriteMarkerAge(t *testing.T, runDir string, at time.Time) {
	t.Helper()
	path := filepath.Join(runDir, markerName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	var m marker
	if err := yaml.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal marker: %v", err)
	}
	m.ProvisionedAt = at.UTC().Format(time.RFC3339)
	out, err := yaml.Marshal(&m)
	if err != nil {
		t.Fatalf("marshal marker: %v", err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
}

func TestPruneStaleRemovesOldTrees(t *testing.T) {
	f := newFixture(t)
	stale, err := f.sb.Provision(context.Background(), testSpec("stale-task"))
	if err != nil {
		t.Fatalf("Provision stale: %v", err)
	}
	fresh, err := f.sb.Provision(context.Background(), testSpec("fresh-task"))
	if err != nil {
		t.Fatalf("Provision fresh: %v", err)
	}
	t.Cleanup(func() { _ = fresh.Cleanup() })
	sidecar := filepath.Join(stale.RunDir, "eval-run.yaml")
	if err := os.WriteFile(sidecar, []byte("run: stale\n"), 0o644); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}
	rewriteMarkerAge(t, stale.RunDir, time.Now().Add(-8*24*time.Hour))

	pruned, err := f.sb.PruneStale(context.Background())
	if err != nil {
		t.Fatalf("PruneStale: %v", err)
	}
	if len(pruned) != 1 || pruned[0] != stale.RunID {
		t.Fatalf("pruned = %v, want [%s]", pruned, stale.RunID)
	}
	mustNotExist(t, stale.Workdir)
	mustNotExist(t, filepath.Join(stale.RunDir, homeDirName))
	mustNotExist(t, filepath.Join(stale.RunDir, markerName))
	mustNotExist(t, filepath.Join(f.runsRoot, stale.RunID+claimSuffix))
	mustExist(t, sidecar) // sidecars retained indefinitely (OQ6)
	mustExist(t, fresh.Workdir)
	mustExist(t, filepath.Join(f.runsRoot, fresh.RunID+claimSuffix))

	again, err := f.sb.PruneStale(context.Background())
	if err != nil {
		t.Fatalf("second PruneStale: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("second prune = %v, want empty", again)
	}
}

// TestPruneStaleSweepsMarkerlessLeak is the defense layer for provisions
// that crashed before the marker was written (or whose rollback died): an
// aged markerless run dir is reclaimed by mtime, while an aged sidecar-only
// run dir (a normally-swept run) keeps its sidecars.
func TestPruneStaleSweepsMarkerlessLeak(t *testing.T) {
	f := newFixture(t)
	old := time.Now().Add(-8 * 24 * time.Hour)

	// A leaked partial provision: home dir, no marker.
	leakDir := filepath.Join(f.runsRoot, "leaked-run")
	if err := os.MkdirAll(filepath.Join(leakDir, homeDirName), 0o755); err != nil {
		t.Fatalf("mkdir leak: %v", err)
	}
	// An already-swept run: sidecar only, no marker, no trees.
	sweptDir := filepath.Join(f.runsRoot, "swept-run")
	if err := os.MkdirAll(sweptDir, 0o755); err != nil {
		t.Fatalf("mkdir swept: %v", err)
	}
	sidecar := filepath.Join(sweptDir, "eval-run.yaml")
	if err := os.WriteFile(sidecar, []byte("run: swept\n"), 0o644); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}
	for _, dir := range []string{leakDir, sweptDir} {
		if err := os.Chtimes(dir, old, old); err != nil {
			t.Fatalf("age %s: %v", dir, err)
		}
	}

	pruned, err := f.sb.PruneStale(context.Background())
	if err != nil {
		t.Fatalf("PruneStale: %v", err)
	}
	if len(pruned) != 1 || pruned[0] != "leaked-run" {
		t.Fatalf("pruned = %v, want [leaked-run]", pruned)
	}
	mustNotExist(t, leakDir) // emptied leak dir is litter, dropped entirely
	mustExist(t, sidecar)    // swept run's sidecars retained indefinitely
}

// TestPruneStaleSweepsOrphanClaims covers the claim-file leg of the
// markerless defense: an aged claim with no run dir is a leaked partial
// provision and is reclaimed; fresh claims, claims with a live run dir, and
// non-claim files are left alone.
func TestPruneStaleSweepsOrphanClaims(t *testing.T) {
	f := newFixture(t)
	if err := os.MkdirAll(filepath.Join(f.runsRoot, "live-run"), 0o755); err != nil {
		t.Fatalf("mkdir live run dir: %v", err)
	}
	orphan := filepath.Join(f.runsRoot, "ghost-run"+claimSuffix)
	young := filepath.Join(f.runsRoot, "young-run"+claimSuffix)
	owned := filepath.Join(f.runsRoot, "live-run"+claimSuffix)
	stray := filepath.Join(f.runsRoot, "stray.txt")
	for _, path := range []string{orphan, young, owned, stray} {
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	old := time.Now().Add(-8 * 24 * time.Hour)
	for _, path := range []string{orphan, owned, stray} {
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatalf("age %s: %v", path, err)
		}
	}

	pruned, err := f.sb.PruneStale(context.Background())
	if err != nil {
		t.Fatalf("PruneStale: %v", err)
	}
	if len(pruned) != 0 {
		t.Fatalf("pruned = %v, want empty (claim sweep reports no run ids)", pruned)
	}
	mustNotExist(t, orphan) // aged + no run dir = leaked claim, reclaimed
	mustExist(t, young)     // fresh claim: a provision may be in flight
	mustExist(t, owned)     // live run dir owns it; the dir path releases it
	mustExist(t, stray)     // foreign file, never touched
}

func TestPruneStaleEdgeCases(t *testing.T) {
	f := newFixture(t)

	// Missing runs root is not an error — nothing has been provisioned yet.
	pruned, err := f.sb.PruneStale(context.Background())
	if err != nil || len(pruned) != 0 {
		t.Fatalf("missing root: pruned=%v err=%v, want empty/nil", pruned, err)
	}

	// Foreign or malformed entries are skipped, never deleted.
	if err := os.MkdirAll(f.runsRoot, 0o755); err != nil {
		t.Fatalf("mkdir runs root: %v", err)
	}
	plainFile := filepath.Join(f.runsRoot, "stray.txt")
	if err := os.WriteFile(plainFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("write stray: %v", err)
	}
	noMarker := filepath.Join(f.runsRoot, "no-marker")
	corrupt := filepath.Join(f.runsRoot, "corrupt")
	badTime := filepath.Join(f.runsRoot, "bad-time")
	for dir, content := range map[string]string{
		noMarker: "",
		corrupt:  ":\tnot yaml [",
		badTime:  "run_id: bad-time\nworktree_name: wt-x\nprovisioned_at: yesterday\n",
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		if content == "" {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, markerName), []byte(content), 0o644); err != nil {
			t.Fatalf("write marker in %s: %v", dir, err)
		}
	}
	pruned, err = f.sb.PruneStale(context.Background())
	if err != nil || len(pruned) != 0 {
		t.Fatalf("foreign entries: pruned=%v err=%v, want empty/nil", pruned, err)
	}
	mustExist(t, plainFile)
	mustExist(t, noMarker)

	// A cancelled context aborts the sweep.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.sb.PruneStale(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled ctx: got %v, want context.Canceled", err)
	}

	// An unreadable runs root is an error. Injected through the readDir
	// seam because no fs-shape trick stages this portably: planting a file
	// where the dir should be forces ENOTDIR on Unix, but Windows maps that
	// condition to "not exist" and the sweep legitimately reports nothing
	// to prune.
	f.sb.readDir = func(string) ([]os.DirEntry, error) {
		return nil, errors.New("io wedged")
	}
	if _, err := f.sb.PruneStale(context.Background()); err == nil || !strings.Contains(err.Error(), "read runs root") {
		t.Fatalf("unreadable root: got %v, want read runs root error", err)
	}
}

func TestPruneStaleSurfacesRemoveError(t *testing.T) {
	f := newFixture(t)
	inst, err := f.sb.Provision(context.Background(), testSpec("bad-marker-task"))
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	t.Cleanup(func() { _ = inst.Cleanup() })
	// A stale marker naming an invalid worktree makes the removal fail
	// mid-sweep.
	data := "run_id: " + inst.RunID + "\nworktree_name: bad_name!\nprovisioned_at: 2020-01-01T00:00:00Z\n"
	if err := os.WriteFile(filepath.Join(inst.RunDir, markerName), []byte(data), 0o644); err != nil {
		t.Fatalf("rewrite marker: %v", err)
	}
	if _, err := f.sb.PruneStale(context.Background()); !errors.Is(err, gitwt.ErrInvalidName) {
		t.Fatalf("bad worktree name: got %v, want gitwt.ErrInvalidName", err)
	}
}

func TestPruneStaleSurfacesMetadataPruneError(t *testing.T) {
	f := newFixture(t)
	if err := os.MkdirAll(f.runsRoot, 0o755); err != nil {
		t.Fatalf("mkdir runs root: %v", err)
	}
	// Injected through the pruneMeta seam: the natural trigger (a non-
	// NotExist stat error on a recorded worktree path, e.g. a run dir
	// replaced by a file) is Unix-only — Windows maps ERROR_PATH_NOT_FOUND
	// to "not exist" and gitwt prunes the metadata cleanly instead.
	f.sb.pruneMeta = func() ([]string, error) {
		return nil, errors.New("admin dir wedged")
	}
	if _, err := f.sb.PruneStale(context.Background()); err == nil || !strings.Contains(err.Error(), "prune worktree metadata") {
		t.Fatalf("wedged metadata prune: got %v, want prune worktree metadata error", err)
	}
}

func TestPruneStaleClearsOrphanedMetadata(t *testing.T) {
	f := newFixture(t)
	inst, err := f.sb.Provision(context.Background(), testSpec("orphan-task"))
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	// The working tree vanishes out-of-band (manual rm); admin metadata stays.
	if err := os.RemoveAll(inst.Workdir); err != nil {
		t.Fatalf("remove workdir: %v", err)
	}
	pruned, err := f.sb.PruneStale(context.Background())
	if err != nil {
		t.Fatalf("PruneStale: %v", err)
	}
	if len(pruned) != 0 {
		t.Fatalf("pruned = %v, want empty (run is fresh)", pruned)
	}
	names, err := f.sb.mgr.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(names) != 0 {
		t.Fatalf("orphaned metadata not cleared: %v", names)
	}
}

func TestRemoveRunTreesErrors(t *testing.T) {
	f := newFixture(t)

	// An invalid worktree name surfaces the non-NotFound manager error.
	if err := f.sb.removeRunTrees("x", "bad_name!"); err == nil || !errors.Is(err, gitwt.ErrInvalidName) {
		t.Fatalf("invalid name: got %v, want gitwt.ErrInvalidName", err)
	}

	// A marker that cannot be removed (a non-empty directory in its place)
	// surfaces a remove-marker error.
	markerDir := filepath.Join(f.runsRoot, "mk", markerName)
	if err := os.MkdirAll(markerDir, 0o755); err != nil {
		t.Fatalf("mkdir marker dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(markerDir, "child.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write child: %v", err)
	}
	if err := f.sb.removeRunTrees("mk", "wt-0000000000000000"); err == nil || !strings.Contains(err.Error(), "remove marker") {
		t.Fatalf("marker dir: got %v, want remove marker error", err)
	}
}

func TestRunIDDerivation(t *testing.T) {
	sanitizeCases := map[string]string{
		"KG Go_Impl.001!": "kg-go-impl-001",
		"  ":              "task",
		"---":             "task",
		"already-clean-9": "already-clean-9",
	}
	for in, want := range sanitizeCases {
		if got := sanitizeID(in); got != want {
			t.Errorf("sanitizeID(%q) = %q, want %q", in, got, want)
		}
	}

	f := newFixture(t)
	runID, err := f.sb.newRunID("My Task/07")
	if err != nil {
		t.Fatalf("newRunID: %v", err)
	}
	pattern := regexp.MustCompile(`^my-task-07-\d{8}T\d{6}-[0-9a-f]{8}$`)
	if !pattern.MatchString(runID) {
		t.Errorf("runID %q does not match %s", runID, pattern)
	}
}
