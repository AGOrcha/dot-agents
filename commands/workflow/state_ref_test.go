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
	// Status now lives in a PER-TASK blob (§9 D5), not the monolithic TASKS.yaml.
	relTask := ".agents/workflow/plans/" + stateRefTestPlanID + "/tasks/t1.yaml"
	blob := gitOut(t, repo, "show", stateRefName+":"+relTask)
	if !strings.Contains(blob, "in_progress") {
		t.Fatalf("per-task state ref blob missing new status; got:\n%s", blob)
	}
	// The monolithic TASKS.yaml is a projection, never written to the ref.
	relTasks := ".agents/workflow/plans/" + stateRefTestPlanID + "/TASKS.yaml"
	if err := exec.Command("git", "-C", repo, "cat-file", "-e", stateRefName+":"+relTasks).Run(); err == nil {
		t.Fatal("monolithic TASKS.yaml must NOT be written to the ref (per-task split)")
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
	if err := mirrorTransitionToStateRef(repo, stateRefTestPlanID, "t1"); err != nil {
		t.Fatalf("disabled mirror must be a no-op, got: %v", err)
	}
	if head := stateRefHead(repo); head != "" {
		t.Fatalf("disabled mirror must write no ref, got %s", head)
	}
}

// tasksYAMLTwoPending renders a TASKS.yaml with two pending tasks (t1, t2) in a
// deterministic order, used by the per-task split / projection tests.
func tasksYAMLTwoPending(planID string) string {
	return "schema_version: 1\nplan_id: \"" + planID + "\"\ntasks:\n" +
		"  - id: \"t1\"\n    title: \"task one\"\n    status: \"pending\"\n    verification_required: true\n" +
		"  - id: \"t2\"\n    title: \"task two\"\n    status: \"pending\"\n    verification_required: true\n"
}

// TestProjectCanonicalTaskFile_RoundTrip proves the per-task split projection is
// byte-faithful (§3B / D1'): splitting a canonical TASKS.yaml into per-task
// records and projecting them back regenerates the EXACT serializer output, so
// the monolithic view is losslessly reconstructable from the split files.
func TestProjectCanonicalTaskFile_RoundTrip(t *testing.T) {
	tf := &CanonicalTaskFile{
		SchemaVersion: 1,
		PlanID:        "rt",
		Tasks: []CanonicalTask{
			{ID: "t1", Title: "one", Status: "in_progress", DependsOn: []string{}, Blocks: []string{}, WriteScope: []string{"a.go"}, VerificationRequired: true, Notes: "n1"},
			{ID: "t2", Title: "two", Status: "pending", DependsOn: []string{"t1"}, Blocks: []string{}, WriteScope: []string{}, VerificationRequired: false},
			{ID: "t3", Title: "three", Status: "completed", DependsOn: []string{}, Blocks: []string{"t2"}, WriteScope: []string{}, AppType: "go-cli"},
		},
	}
	want, err := yamlMarshal(tf)
	if err != nil {
		t.Fatalf("marshal canonical: %v", err)
	}

	projected := projectCanonicalTaskFile(splitCanonicalTaskFile(tf))
	got, err := yamlMarshal(projected)
	if err != nil {
		t.Fatalf("marshal projected: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("projection not byte-identical to canonical:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

// TestProjectCanonicalTaskFile_ReordersByOrder proves the projection restores
// the canonical task order from the Order field regardless of the record slice
// order (per-task blobs come back from the ref in ls-tree/alphabetical order).
func TestProjectCanonicalTaskFile_ReordersByOrder(t *testing.T) {
	records := []stateRefTaskRecord{
		{SchemaVersion: 1, PlanID: "p", Order: 2, Task: CanonicalTask{ID: "c"}},
		{SchemaVersion: 1, PlanID: "p", Order: 0, Task: CanonicalTask{ID: "a"}},
		{SchemaVersion: 1, PlanID: "p", Order: 1, Task: CanonicalTask{ID: "b"}},
	}
	tf := projectCanonicalTaskFile(records)
	if tf.SchemaVersion != 1 || tf.PlanID != "p" {
		t.Fatalf("plan scoping lost: %+v", tf)
	}
	got := []string{tf.Tasks[0].ID, tf.Tasks[1].ID, tf.Tasks[2].ID}
	if got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("projection did not restore order: %v", got)
	}
}

// TestWritePlanStateRefCAS_ConcurrentDifferentTasksNoLostUpdate is the core D5
// proof: two workers transitioning DIFFERENT tasks of the SAME plan race to
// commit to refs/agents/state. Each overwrites only its own per-task blob and
// seeds siblings only-if-absent, so the two writes touch DISJOINT blobs and
// BOTH survive the concurrent read-modify-write — no lost update on the
// monolithic TASKS.yaml that the old whole-file mirror suffered. CAS-retry
// remains the fallback for the shared PLAN.yaml blob.
func TestWritePlanStateRefCAS_ConcurrentDifferentTasksNoLostUpdate(t *testing.T) {
	repo := t.TempDir()
	rel := ".agents/workflow/plans/" + stateRefTestPlanID + "/"
	testutil.InitGitRepo(t, repo, map[string]string{
		rel + "PLAN.yaml":  planYAMLActive(stateRefTestPlanID),
		rel + "TASKS.yaml": tasksYAMLTwoPending(stateRefTestPlanID),
	})

	// Each worker owns a private view of TASKS.yaml (its own worktree copy):
	// worker A saw t1 advance, worker B saw t2 advance. Neither has seen the
	// other's transition — exactly the cross-worktree divergence D5 addresses.
	viewA := &CanonicalTaskFile{SchemaVersion: 1, PlanID: stateRefTestPlanID, Tasks: []CanonicalTask{
		{ID: "t1", Title: "task one", Status: "in_progress", VerificationRequired: true},
		{ID: "t2", Title: "task two", Status: "pending", VerificationRequired: true},
	}}
	viewB := &CanonicalTaskFile{SchemaVersion: 1, PlanID: stateRefTestPlanID, Tasks: []CanonicalTask{
		{ID: "t1", Title: "task one", Status: "pending", VerificationRequired: true},
		{ID: "t2", Title: "task two", Status: "in_progress", VerificationRequired: true},
	}}
	planContent := []byte(planYAMLActive(stateRefTestPlanID))

	writers := []struct {
		view   *CanonicalTaskFile
		taskID string
	}{{viewA, "t1"}, {viewB, "t2"}}

	var wg sync.WaitGroup
	errs := make([]error, len(writers))
	start := make(chan struct{})
	for i := range writers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			overwrite, seed, err := buildPlanTaskStateRefWrite(repo, stateRefTestPlanID, writers[i].taskID, writers[i].view, planContent)
			if err != nil {
				errs[i] = err
				return
			}
			<-start // release together to maximize CAS contention
			errs[i] = writePlanStateRefCAS(repo, overwrite, seed)
		}(i)
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("writer %d: %v", i, err)
		}
	}

	// Disjoint blobs: t1 and t2 are separate ref paths.
	t1 := gitOut(t, repo, "show", stateRefName+":"+rel+"tasks/t1.yaml")
	t2 := gitOut(t, repo, "show", stateRefName+":"+rel+"tasks/t2.yaml")
	if t1 == t2 {
		t.Fatal("per-task blobs must be disjoint, got identical content")
	}

	// No lost update: the projection reconstructs BOTH transitions.
	projected, err := projectPlanTasksFromStateRef(repo, stateRefTestPlanID)
	if err != nil {
		t.Fatalf("project from ref: %v", err)
	}
	byID := map[string]string{}
	for _, task := range projected.Tasks {
		byID[task.ID] = task.Status
	}
	if byID["t1"] != "in_progress" {
		t.Fatalf("worker A's t1 transition lost: %q", byID["t1"])
	}
	if byID["t2"] != "in_progress" {
		t.Fatalf("worker B's t2 transition lost: %q", byID["t2"])
	}
	if len(projected.Tasks) != 2 {
		t.Fatalf("projection must carry both tasks, got %d", len(projected.Tasks))
	}
}
