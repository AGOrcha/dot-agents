package workflow

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/testutil"
)

// stateRefTestPlanID is the plan id the git-ref CAS write tests seed.
const stateRefTestPlanID = "srw"

// seedStateRefRepo builds a git repo with a single active plan (one pending
// task "t1") and an .agentsrc.json whose work_tracking.write_to is set to
// writeTo. writeTo == "" writes NO work_tracking block, exercising the default
// (working-copy-only) write path. Returns the repo root.
func seedStateRefRepo(t *testing.T, writeTo string) string {
	t.Helper()
	agentsrc := `{"version":1,"project":"p","sources":[{"type":"local"}]}`
	if writeTo != "" {
		agentsrc = `{"version":1,"project":"p","sources":[{"type":"local"}],"work_tracking":{"write_to":"` + writeTo + `"}}`
	}
	repo := t.TempDir()
	rel := ".agents/workflow/plans/" + stateRefTestPlanID + "/"
	testutil.InitGitRepo(t, repo, map[string]string{
		".agentsrc.json":   agentsrc,
		rel + "PLAN.yaml":  planYAMLActive(stateRefTestPlanID),
		rel + "TASKS.yaml": tasksYAMLWithStatus(stateRefTestPlanID, "pending"),
	})
	return repo
}

// gitAncestor reports whether commit a is an ancestor of commit b, tolerating
// `merge-base --is-ancestor`'s exit-1 ("not an ancestor") as a normal result
// rather than a test failure.
func gitAncestor(t *testing.T, repo, a, b string) bool {
	t.Helper()
	err := exec.Command("git", "-C", repo, "merge-base", "--is-ancestor", a, b).Run()
	if err == nil {
		return true
	}
	if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
		return false
	}
	t.Fatalf("merge-base --is-ancestor %s %s: %v", a, b, err)
	return false
}

// TestRunWorkflowAdvance_MirrorsToStateRefWhenEnabled proves the git-ref CAS
// write path (D9): with work_tracking.write_to=state-ref a status transition
// ALSO writes the changed state file(s) to refs/agents/state, the working copy
// is still written (additive, §3B), and the ref is orthogonal to the code
// branch — never merged into it (D10).
func TestRunWorkflowAdvance_MirrorsToStateRefWhenEnabled(t *testing.T) {
	repo := seedStateRefRepo(t, config.WorkTrackingWriteToStateRef)
	t.Setenv("AGENTS_HOME", t.TempDir())
	chdirRepo(t, repo)

	if err := runWorkflowAdvance(stateRefTestPlanID, "t1", "in_progress"); err != nil {
		t.Fatalf("advance: %v", err)
	}

	// The working copy is still written (the transition is additive, never
	// replaced by the ref).
	tf, err := loadCanonicalTasks(repo, stateRefTestPlanID)
	if err != nil {
		t.Fatalf("loadCanonicalTasks: %v", err)
	}
	if got := tf.Tasks[0].Status; got != "in_progress" {
		t.Fatalf("working copy not advanced: %q", got)
	}

	// The state ref now exists and its TASKS.yaml blob carries the new status.
	head := stateRefHead(repo)
	if head == "" {
		t.Fatal("refs/agents/state was not created by the transition")
	}
	if stateRefName != "refs/agents/state" {
		t.Fatalf("state ref name must be refs/agents/state, got %q", stateRefName)
	}
	relTasks := ".agents/workflow/plans/" + stateRefTestPlanID + "/TASKS.yaml"
	blob := gitOut(t, repo, "show", stateRefName+":"+relTasks)
	if !strings.Contains(blob, "in_progress") || strings.Contains(blob, "pending") {
		t.Fatalf("state ref TASKS.yaml missing new status; got:\n%s", blob)
	}
	// PLAN.yaml is mirrored too (runWorkflowAdvance rewrites it).
	relPlan := ".agents/workflow/plans/" + stateRefTestPlanID + "/PLAN.yaml"
	if _, err := exec.Command("git", "-C", repo, "cat-file", "-e", stateRefName+":"+relPlan).Output(); err != nil {
		t.Fatalf("state ref must carry PLAN.yaml: %v", err)
	}

	// Orthogonality (D10): the state ref is a parallel lineage — neither an
	// ancestor of the code branch nor a descendant of it, so it is never merged
	// into the default/code branch.
	codeHead := gitOut(t, repo, "rev-parse", "HEAD")
	if gitAncestor(t, repo, head, codeHead) {
		t.Fatal("state ref must NOT be an ancestor of the code branch (D10: never merged)")
	}
	if gitAncestor(t, repo, codeHead, head) {
		t.Fatal("code branch HEAD must NOT be an ancestor of the state ref (parallel lineage)")
	}
}

// TestRunWorkflowAdvance_DefaultConfigWritesNoStateRef is the HARD gate: with
// default / unset work_tracking config a transition writes ONLY the working
// copy and NO git ref, so the default write path is byte-for-byte identical to
// today. The state-ref mechanism ships here but stays inert until opted in.
func TestRunWorkflowAdvance_DefaultConfigWritesNoStateRef(t *testing.T) {
	repo := seedStateRefRepo(t, "") // no work_tracking block at all
	t.Setenv("AGENTS_HOME", t.TempDir())
	chdirRepo(t, repo)

	if err := runWorkflowAdvance(stateRefTestPlanID, "t1", "in_progress"); err != nil {
		t.Fatalf("advance: %v", err)
	}

	// Working copy IS updated — the default path is preserved.
	tf, err := loadCanonicalTasks(repo, stateRefTestPlanID)
	if err != nil {
		t.Fatalf("loadCanonicalTasks: %v", err)
	}
	if got := tf.Tasks[0].Status; got != "in_progress" {
		t.Fatalf("default path must still write the working copy: got %q", got)
	}

	// NO state ref written.
	if head := stateRefHead(repo); head != "" {
		t.Fatalf("default config must write NO ref, but refs/agents/state=%s", head)
	}
	// Belt-and-suspenders: nothing under refs/agents/* exists.
	if refs := strings.TrimSpace(gitOutput(repo, "for-each-ref", "refs/agents/")); refs != "" {
		t.Fatalf("default config wrote unexpected agent refs:\n%s", refs)
	}
}

// TestRunWorkflowAdvance_ExplicitWorktreeWritesNoStateRef proves write_to set
// explicitly to "worktree" behaves like the default (no ref written).
func TestRunWorkflowAdvance_ExplicitWorktreeWritesNoStateRef(t *testing.T) {
	repo := seedStateRefRepo(t, config.WorkTrackingWriteToWorktree)
	t.Setenv("AGENTS_HOME", t.TempDir())
	chdirRepo(t, repo)

	if err := runWorkflowAdvance(stateRefTestPlanID, "t1", "in_progress"); err != nil {
		t.Fatalf("advance: %v", err)
	}
	if head := stateRefHead(repo); head != "" {
		t.Fatalf("write_to=worktree must write NO ref, but refs/agents/state=%s", head)
	}
}

// TestWriteStateRefCAS_ConcurrentRMWPreservesAllWriters is the interprocess-safe
// read-modify-write proof: many writers race to commit DISTINCT files to the
// single refs/agents/state via separate git processes. Because each CAS re-reads
// the (possibly concurrently-advanced) current tree and re-applies on top before
// swapping, every writer's file must survive — no addition is lost to a racing
// update. This is the RMW the file lock alone cannot provide across worktrees.
func TestWriteStateRefCAS_ConcurrentRMWPreservesAllWriters(t *testing.T) {
	repo := seedStateRefRepo(t, config.WorkTrackingWriteToStateRef)

	const writers = 8
	var wg sync.WaitGroup
	errs := make([]error, writers)
	start := make(chan struct{})
	for i := range writers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start // release all at once to maximize CAS contention
			f := stateRefFile{
				relPath: fmt.Sprintf(".agents/workflow/state/w%d.txt", i),
				content: []byte(fmt.Sprintf("writer %d\n", i)),
			}
			errs[i] = writeStateRefCAS(repo, []stateRefFile{f})
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("writer %d: CAS failed (retry did not converge): %v", i, err)
		}
	}
	// Every writer's addition survives on the ref → no lost update.
	for i := range writers {
		path := fmt.Sprintf(".agents/workflow/state/w%d.txt", i)
		blob := gitOut(t, repo, "show", stateRefName+":"+path)
		if strings.TrimSpace(blob) != fmt.Sprintf("writer %d", i) {
			t.Fatalf("writer %d addition lost/corrupted on ref: %q", i, blob)
		}
	}
}

// TestCompareAndSwapStateRef_RejectsStaleOld proves the CAS primitive detects a
// conflict: once the ref has advanced past <old>, an update-ref that still
// believes it points at <old> is rejected and the ref is left untouched — the
// signal that drives the retry loop.
func TestCompareAndSwapStateRef_RejectsStaleOld(t *testing.T) {
	repo := seedStateRefRepo(t, config.WorkTrackingWriteToStateRef)

	if err := writeStateRefCAS(repo, []stateRefFile{{relPath: ".agents/workflow/state/a.txt", content: []byte("a\n")}}); err != nil {
		t.Fatalf("initial write: %v", err)
	}
	c0 := stateRefHead(repo)

	// A concurrent writer advances the ref past c0.
	if err := writeStateRefCAS(repo, []stateRefFile{{relPath: ".agents/workflow/state/b.txt", content: []byte("b\n")}}); err != nil {
		t.Fatalf("second write: %v", err)
	}
	c1 := stateRefHead(repo)
	if c0 == c1 {
		t.Fatal("ref did not advance between the two writes")
	}

	// A swap that still names the stale c0 as <old> must be rejected.
	commit, err := buildStateRefCommit(repo, c0, []stateRefFile{{relPath: ".agents/workflow/state/c.txt", content: []byte("c\n")}})
	if err != nil {
		t.Fatalf("build commit: %v", err)
	}
	if err := compareAndSwapStateRef(repo, commit, c0); err == nil {
		t.Fatal("expected CAS to reject a stale <old>, but it succeeded")
	}
	if stateRefHead(repo) != c1 {
		t.Fatal("a rejected CAS must not move the ref")
	}
}

// TestWriteStateRefCAS_ErrorsAfterBoundedRetries proves the retry loop is
// bounded: when every swap conflicts (simulated via the casSwapFn seam), the
// write gives up after exactly stateRefCASAttempts and returns a wrapped
// contention error rather than spinning forever.
func TestWriteStateRefCAS_ErrorsAfterBoundedRetries(t *testing.T) {
	repo := seedStateRefRepo(t, config.WorkTrackingWriteToStateRef)

	prev := casSwapFn
	t.Cleanup(func() { casSwapFn = prev })
	calls := 0
	casSwapFn = func(projectPath, newCommit, old string) error {
		calls++
		return fmt.Errorf("simulated concurrent ref move")
	}

	err := writeStateRefCAS(repo, []stateRefFile{{relPath: ".agents/workflow/state/x.txt", content: []byte("x\n")}})
	if err == nil {
		t.Fatal("expected bounded-retry exhaustion error")
	}
	if !strings.Contains(err.Error(), "compare-and-swap lost after") {
		t.Fatalf("error should name the retry exhaustion: %v", err)
	}
	if calls != stateRefCASAttempts {
		t.Fatalf("expected exactly %d CAS attempts, got %d", stateRefCASAttempts, calls)
	}
}

// TestMirrorTransitionToStateRef_DisabledIsNoop proves the gate short-circuits
// entirely when the backend is inactive: calling the mirror directly on a
// default-config repo writes no ref and returns nil.
func TestMirrorTransitionToStateRef_DisabledIsNoop(t *testing.T) {
	repo := seedStateRefRepo(t, "")
	if err := mirrorTransitionToStateRef(repo, stateRefTestPlanID); err != nil {
		t.Fatalf("disabled mirror must be a no-op, got: %v", err)
	}
	if head := stateRefHead(repo); head != "" {
		t.Fatalf("disabled mirror must write no ref, got %s", head)
	}
}
