package workflow

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/testutil"
	"go.yaml.in/yaml/v3"
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

// ── error-path coverage for the per-task state-ref write / projection layer ──

// TestCollectPlanTaskStateRefWrite_MissingTasksIsEmpty proves a plan with no
// TASKS.yaml yields an empty (no-op) write rather than an error.
func TestCollectPlanTaskStateRefWrite_MissingTasksIsEmpty(t *testing.T) {
	repo := t.TempDir()
	ow, sd, err := collectPlanTaskStateRefWrite(repo, "nope", "t1")
	if err != nil {
		t.Fatalf("missing TASKS.yaml must be a no-op, got: %v", err)
	}
	if ow != nil || sd != nil {
		t.Fatalf("missing TASKS.yaml must yield empty write, got overwrite=%v seed=%v", ow, sd)
	}
}

// TestCollectPlanTaskStateRefWrite_UnreadableTasksErrors covers the
// non-IsNotExist read-error leg: a TASKS.yaml that is a directory makes
// os.ReadFile fail with an error that is NOT os.IsNotExist, which must surface.
func TestCollectPlanTaskStateRefWrite_UnreadableTasksErrors(t *testing.T) {
	repo := t.TempDir()
	dir := filepath.Join(plansBaseDir(repo), stateRefTestPlanID)
	if err := os.MkdirAll(filepath.Join(dir, workflowTasksFileName), 0755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := collectPlanTaskStateRefWrite(repo, stateRefTestPlanID, "t1"); err == nil {
		t.Fatal("expected read error when TASKS.yaml is a directory")
	}
}

// TestCollectPlanTaskStateRefWrite_MalformedTasksErrors covers the
// yaml.Unmarshal error leg.
func TestCollectPlanTaskStateRefWrite_MalformedTasksErrors(t *testing.T) {
	repo := t.TempDir()
	dir := filepath.Join(plansBaseDir(repo), stateRefTestPlanID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, workflowTasksFileName), []byte("this: : not valid ["), 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := collectPlanTaskStateRefWrite(repo, stateRefTestPlanID, "t1"); err == nil {
		t.Fatal("expected yaml unmarshal error for malformed TASKS.yaml")
	}
}

// TestCollectPlanTaskStateRefWrite_UnreadablePlanErrors covers the PLAN.yaml
// non-IsNotExist read-error leg (PLAN.yaml is a directory).
func TestCollectPlanTaskStateRefWrite_UnreadablePlanErrors(t *testing.T) {
	repo := t.TempDir()
	dir := filepath.Join(plansBaseDir(repo), stateRefTestPlanID)
	if err := os.MkdirAll(filepath.Join(dir, workflowPlanFileName), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, workflowTasksFileName), []byte(tasksYAMLWithStatus(stateRefTestPlanID, "pending")), 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := collectPlanTaskStateRefWrite(repo, stateRefTestPlanID, "t1"); err == nil {
		t.Fatal("expected read error when PLAN.yaml is a directory")
	}
}

// TestCollectPlanTaskStateRefWrite_MissingPlanOmitsPlanBlob covers the
// PLAN-IsNotExist leg: a missing PLAN.yaml leaves the plan blob out of the
// write set while the changed task's blob is still written.
func TestCollectPlanTaskStateRefWrite_MissingPlanOmitsPlanBlob(t *testing.T) {
	repo := t.TempDir()
	dir := filepath.Join(plansBaseDir(repo), stateRefTestPlanID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, workflowTasksFileName), []byte(tasksYAMLWithStatus(stateRefTestPlanID, "in_progress")), 0644); err != nil {
		t.Fatal(err)
	}
	ow, _, err := collectPlanTaskStateRefWrite(repo, stateRefTestPlanID, "t1")
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	hasTask := false
	for _, f := range ow {
		if strings.HasSuffix(f.relPath, "/"+workflowPlanFileName) {
			t.Fatalf("PLAN.yaml absent on disk must not be in the write set: %s", f.relPath)
		}
		if strings.HasSuffix(f.relPath, "/tasks/t1.yaml") {
			hasTask = true
		}
	}
	if !hasTask {
		t.Fatal("changed task blob missing from overwrite set")
	}
}

// TestBuildPlanTaskStateRefWrite_MarshalError covers the yamlMarshal error leg
// via the marshaller seam.
func TestBuildPlanTaskStateRefWrite_MarshalError(t *testing.T) {
	prev := yamlMarshal
	yamlMarshal = func(any) ([]byte, error) { return nil, errors.New("marshal boom") }
	t.Cleanup(func() { yamlMarshal = prev })
	tf := &CanonicalTaskFile{SchemaVersion: 1, PlanID: "p", Tasks: []CanonicalTask{{ID: "t1", Status: "pending"}}}
	if _, _, err := buildPlanTaskStateRefWrite(t.TempDir(), "p", "t1", tf, nil); err == nil {
		t.Fatal("expected marshal error to propagate")
	}
}

// TestBuildPlanTaskStateRefWrite_NoPlanContentOmitsPlanBlob covers the
// empty-planContent branch: no PLAN.yaml blob is added.
func TestBuildPlanTaskStateRefWrite_NoPlanContentOmitsPlanBlob(t *testing.T) {
	tf := &CanonicalTaskFile{SchemaVersion: 1, PlanID: "p", Tasks: []CanonicalTask{{ID: "t1", Status: "pending"}}}
	ow, _, err := buildPlanTaskStateRefWrite(t.TempDir(), "p", "t1", tf, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range ow {
		if strings.HasSuffix(f.relPath, "/"+workflowPlanFileName) {
			t.Fatal("nil planContent must not add a PLAN.yaml blob")
		}
	}
}

// TestWriteStateRefCAS_EmptyFilesIsNoop covers the empty-input guard.
func TestWriteStateRefCAS_EmptyFilesIsNoop(t *testing.T) {
	repo := seedStateRefRepo(t, config.WorkTrackingWriteToStateRef)
	if err := writeStateRefCAS(repo, nil); err != nil {
		t.Fatalf("empty write must be a no-op, got: %v", err)
	}
	if head := stateRefHead(repo); head != "" {
		t.Fatalf("empty write must not create the ref, got %s", head)
	}
}

// TestWritePlanStateRefCAS_EmptyIsNoop covers the per-task empty-input guard.
func TestWritePlanStateRefCAS_EmptyIsNoop(t *testing.T) {
	repo := seedStateRefRepo(t, config.WorkTrackingWriteToStateRef)
	if err := writePlanStateRefCAS(repo, nil, nil); err != nil {
		t.Fatalf("empty per-task write must be a no-op, got: %v", err)
	}
	if head := stateRefHead(repo); head != "" {
		t.Fatalf("empty per-task write must not create the ref, got %s", head)
	}
}

// TestCasWriteStateRef_ResolveErrorPropagates covers the resolver-error leg.
func TestCasWriteStateRef_ResolveErrorPropagates(t *testing.T) {
	repo := seedStateRefRepo(t, config.WorkTrackingWriteToStateRef)
	sentinel := errors.New("resolve boom")
	err := casWriteStateRef(repo, func(string) ([]stateRefFile, error) { return nil, sentinel })
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected resolve error to propagate, got %v", err)
	}
}

// TestCasWriteStateRef_EmptyResolveIsNoop covers the in-loop empty-files leg
// (resolve returns no files → nil, no ref).
func TestCasWriteStateRef_EmptyResolveIsNoop(t *testing.T) {
	repo := seedStateRefRepo(t, config.WorkTrackingWriteToStateRef)
	if err := casWriteStateRef(repo, func(string) ([]stateRefFile, error) { return nil, nil }); err != nil {
		t.Fatalf("empty resolve must be a no-op, got: %v", err)
	}
	if head := stateRefHead(repo); head != "" {
		t.Fatalf("empty resolve must not create the ref, got %s", head)
	}
}

// TestCasWriteStateRef_BuildErrorPropagates covers the buildStateRefCommit
// error leg: a non-git directory makes the commit plumbing fail.
func TestCasWriteStateRef_BuildErrorPropagates(t *testing.T) {
	dir := t.TempDir()
	err := casWriteStateRef(dir, func(string) ([]stateRefFile, error) {
		return []stateRefFile{{relPath: "x.txt", content: []byte("x")}}, nil
	})
	if err == nil {
		t.Fatal("expected buildStateRefCommit error in a non-git dir")
	}
}

// TestReadPlanTaskRecordsFromStateRef_NoRefIsEmpty covers the ref-absent leg.
func TestReadPlanTaskRecordsFromStateRef_NoRefIsEmpty(t *testing.T) {
	repo := seedStateRefRepo(t, config.WorkTrackingWriteToStateRef)
	recs, err := readPlanTaskRecordsFromStateRef(repo, stateRefTestPlanID)
	if err != nil {
		t.Fatalf("no ref must yield no error, got: %v", err)
	}
	if len(recs) != 0 {
		t.Fatalf("no ref must yield empty records, got %d", len(recs))
	}
}

// TestReadPlanTaskRecordsFromStateRef_NoTasksDirIsEmpty covers the ls-tree
// error leg: the ref exists but the plan's tasks/ dir does not.
func TestReadPlanTaskRecordsFromStateRef_NoTasksDirIsEmpty(t *testing.T) {
	repo := seedStateRefRepo(t, config.WorkTrackingWriteToStateRef)
	if err := writeStateRefCAS(repo, []stateRefFile{{relPath: ".agents/workflow/state/other.txt", content: []byte("x\n")}}); err != nil {
		t.Fatal(err)
	}
	recs, err := readPlanTaskRecordsFromStateRef(repo, stateRefTestPlanID)
	if err != nil {
		t.Fatalf("absent tasks dir must yield no error, got: %v", err)
	}
	if len(recs) != 0 {
		t.Fatalf("absent tasks dir must yield empty records, got %d", len(recs))
	}
}

// TestReadPlanTaskRecordsFromStateRef_SkipsNonYamlEntries covers the
// non-.yaml skip leg while still decoding the valid per-task blob.
func TestReadPlanTaskRecordsFromStateRef_SkipsNonYamlEntries(t *testing.T) {
	repo := seedStateRefRepo(t, config.WorkTrackingWriteToStateRef)
	base := ".agents/workflow/plans/" + stateRefTestPlanID + "/tasks/"
	recYAML, err := yamlMarshal(stateRefTaskRecord{SchemaVersion: 1, PlanID: stateRefTestPlanID, Order: 0, Task: CanonicalTask{ID: "t1", Status: "pending"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := writeStateRefCAS(repo, []stateRefFile{
		{relPath: base + "t1.yaml", content: recYAML},
		{relPath: base + "notes.txt", content: []byte("ignore me\n")},
	}); err != nil {
		t.Fatal(err)
	}
	recs, err := readPlanTaskRecordsFromStateRef(repo, stateRefTestPlanID)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].Task.ID != "t1" {
		t.Fatalf("non-.yaml entry must be skipped, got %+v", recs)
	}
}

// TestReadPlanTaskRecordsFromStateRef_OnlyNonYamlIsEmpty covers the leg where
// the tasks/ dir exists on the ref but holds no .yaml blob: the batch is
// skipped entirely and no records are returned.
func TestReadPlanTaskRecordsFromStateRef_OnlyNonYamlIsEmpty(t *testing.T) {
	repo := seedStateRefRepo(t, config.WorkTrackingWriteToStateRef)
	base := ".agents/workflow/plans/" + stateRefTestPlanID + "/tasks/"
	if err := writeStateRefCAS(repo, []stateRefFile{
		{relPath: base + "notes.txt", content: []byte("ignore me\n")},
	}); err != nil {
		t.Fatal(err)
	}
	recs, err := readPlanTaskRecordsFromStateRef(repo, stateRefTestPlanID)
	if err != nil {
		t.Fatalf("only non-.yaml entries must yield no error, got: %v", err)
	}
	if len(recs) != 0 {
		t.Fatalf("only non-.yaml entries must yield empty records, got %d", len(recs))
	}
}

// TestProjectPlanTasksFromStateRef_MalformedBlobErrors covers the unmarshal
// error leg in readPlanTaskRecordsFromStateRef and its propagation through
// projectPlanTasksFromStateRef.
func TestProjectPlanTasksFromStateRef_MalformedBlobErrors(t *testing.T) {
	repo := seedStateRefRepo(t, config.WorkTrackingWriteToStateRef)
	base := ".agents/workflow/plans/" + stateRefTestPlanID + "/tasks/"
	if err := writeStateRefCAS(repo, []stateRefFile{{relPath: base + "t1.yaml", content: []byte("this: : not valid [")}}); err != nil {
		t.Fatal(err)
	}
	if _, err := projectPlanTasksFromStateRef(repo, stateRefTestPlanID); err == nil {
		t.Fatal("expected unmarshal error from a malformed per-task blob")
	}
}

// TestReadPlanTaskRecordsFromStateRef_MultiTaskMatchesPerShow proves the
// batched cat-file read returns records byte-identical, and in the SAME order,
// as the prior per-blob `git show` path it replaced (H3 CAS-safety). It seeds
// three task blobs whose lexical name order differs from their Order field, so
// the assertion pins ls-tree name order (what both paths iterate), not Order.
func TestReadPlanTaskRecordsFromStateRef_MultiTaskMatchesPerShow(t *testing.T) {
	repo := seedStateRefRepo(t, config.WorkTrackingWriteToStateRef)
	base := ".agents/workflow/plans/" + stateRefTestPlanID + "/tasks/"
	names := []string{"t1.yaml", "t2.yaml", "t3.yaml"}
	orders := []int{2, 0, 1}
	var files []stateRefFile
	for i, name := range names {
		rec := stateRefTaskRecord{
			SchemaVersion: 1, PlanID: stateRefTestPlanID, Order: orders[i],
			Task: CanonicalTask{ID: strings.TrimSuffix(name, ".yaml"), Status: "pending"},
		}
		blob, err := yamlMarshal(rec)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, stateRefFile{relPath: base + name, content: blob})
	}
	if err := writeStateRefCAS(repo, files); err != nil {
		t.Fatal(err)
	}

	got, err := readPlanTaskRecordsFromStateRef(repo, stateRefTestPlanID)
	if err != nil {
		t.Fatalf("batched read errored: %v", err)
	}

	// Reconstruct via the old per-`git show` behavior directly from the ref.
	treeish := stateRefName + ":" + base
	var want []stateRefTaskRecord
	for _, name := range names {
		blob := gitOut(t, repo, "show", treeish+name)
		var rec stateRefTaskRecord
		if err := yaml.Unmarshal([]byte(blob), &rec); err != nil {
			t.Fatal(err)
		}
		want = append(want, rec)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("batched read diverged from per-show:\n got=%+v\nwant=%+v", got, want)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 records, got %d", len(got))
	}
}

// TestParseCatFileBatch_HappyPreservesOrderAndBinarySafety decodes a hand-built
// two-entry batch stream where the second blob embeds newlines: size-framing
// must return exact bytes (a naive line splitter would mangle it) in input
// order.
func TestParseCatFileBatch_HappyPreservesOrderAndBinarySafety(t *testing.T) {
	c0 := []byte("a: 1\n")
	c1 := []byte("multi\nline\nbody")
	var data []byte
	data = append(data, []byte(fmt.Sprintf("oid0 blob %d\n", len(c0)))...)
	data = append(data, c0...)
	data = append(data, '\n')
	data = append(data, []byte(fmt.Sprintf("oid1 blob %d\n", len(c1)))...)
	data = append(data, c1...)
	data = append(data, '\n')
	blobs, err := parseCatFileBatch(data, []string{"t0.yaml", "t1.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(blobs, [][]byte{c0, c1}) {
		t.Fatalf("blobs mismatch: %q", blobs)
	}
}

// TestParseCatFileBatch_ErrorLegs covers each framing-failure branch that the
// real ls-tree→cat-file path cannot reach (ls-tree only lists present blobs),
// mirroring the old per-show error behavior.
func TestParseCatFileBatch_ErrorLegs(t *testing.T) {
	cases := []struct {
		name string
		data string
	}{
		{"missing", "srw:tasks/t1.yaml missing\n"},
		{"truncatedHeader", "oid blob 5"},
		{"malformedHeader", "oid blob\n"},
		{"badSize", "oid blob xx\nbody\n"},
		{"truncatedContent", "oid blob 100\nshort"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseCatFileBatch([]byte(tc.data), []string{"t1.yaml"}); err == nil {
				t.Fatalf("%s: expected error", tc.name)
			}
		})
	}
}

// TestCatFileBatchBlobs_GitErrorPropagates covers the gitStateExec error leg:
// a non-git directory makes `cat-file --batch` fail, which must surface.
func TestCatFileBatchBlobs_GitErrorPropagates(t *testing.T) {
	if _, err := catFileBatchBlobs(t.TempDir(), "HEAD:tasks", []string{"t1.yaml"}); err == nil {
		t.Fatal("expected git error from a non-git dir")
	}
}

// TestMirrorTransitionToStateRef_CollectErrorPropagates covers the wrapped
// collect-error leg: the backend is active but the working-copy TASKS.yaml is
// malformed, so the mirror surfaces a "collect state files" error.
func TestMirrorTransitionToStateRef_CollectErrorPropagates(t *testing.T) {
	repo := seedStateRefRepo(t, config.WorkTrackingWriteToStateRef)
	dir := filepath.Join(plansBaseDir(repo), stateRefTestPlanID)
	if err := os.WriteFile(filepath.Join(dir, workflowTasksFileName), []byte("bad: : ["), 0644); err != nil {
		t.Fatal(err)
	}
	err := mirrorTransitionToStateRef(repo, stateRefTestPlanID, "t1")
	if err == nil || !strings.Contains(err.Error(), "collect state files") {
		t.Fatalf("expected wrapped collect error, got %v", err)
	}
}

// TestAdvanceFanoutTaskStatusIfPending_MirrorsWhenEnabled proves a fanout's
// pending→in_progress transition routes through the per-task mirror when the
// git-ref backend is active (the delegation.go wiring).
func TestAdvanceFanoutTaskStatusIfPending_MirrorsWhenEnabled(t *testing.T) {
	repo := seedStateRefRepo(t, config.WorkTrackingWriteToStateRef)
	tf, err := loadCanonicalTasks(repo, stateRefTestPlanID)
	if err != nil {
		t.Fatal(err)
	}
	advanceFanoutTaskStatusIfPending(repo, tf, &tf.Tasks[0])
	if tf.Tasks[0].Status != "in_progress" {
		t.Fatalf("status not advanced: %q", tf.Tasks[0].Status)
	}
	if stateRefHead(repo) == "" {
		t.Fatal("enabled fanout advance must mirror to the state ref")
	}
	blob := gitOut(t, repo, "show", stateRefName+":.agents/workflow/plans/"+stateRefTestPlanID+"/tasks/t1.yaml")
	if !strings.Contains(blob, "in_progress") {
		t.Fatalf("per-task blob missing new status:\n%s", blob)
	}
}

// TestAdvanceFanoutTaskStatusIfPending_SaveErrorSkipsMirror covers the
// save-failure leg: the warn-and-return short-circuits before the mirror, so no
// ref is written.
func TestAdvanceFanoutTaskStatusIfPending_SaveErrorSkipsMirror(t *testing.T) {
	repo := seedStateRefRepo(t, config.WorkTrackingWriteToStateRef)
	tf, err := loadCanonicalTasks(repo, stateRefTestPlanID)
	if err != nil {
		t.Fatal(err)
	}
	prev := osMkdirAll
	osMkdirAll = func(string, os.FileMode) error { return errors.New("mkdir boom") }
	t.Cleanup(func() { osMkdirAll = prev })
	advanceFanoutTaskStatusIfPending(repo, tf, &tf.Tasks[0])
	if head := stateRefHead(repo); head != "" {
		t.Fatalf("save failure must skip the mirror, but ref=%s", head)
	}
}

// TestAdvanceFanoutTaskStatusIfPending_MirrorErrorIsNonFatal covers the
// mirror-failure warn leg: a forced CAS failure makes the mirror error, but the
// working-copy save is already durable, so the fanout is not rolled back.
func TestAdvanceFanoutTaskStatusIfPending_MirrorErrorIsNonFatal(t *testing.T) {
	repo := seedStateRefRepo(t, config.WorkTrackingWriteToStateRef)
	tf, err := loadCanonicalTasks(repo, stateRefTestPlanID)
	if err != nil {
		t.Fatal(err)
	}
	prev := casSwapFn
	casSwapFn = func(string, string, string) error { return errors.New("forced CAS failure") }
	t.Cleanup(func() { casSwapFn = prev })
	advanceFanoutTaskStatusIfPending(repo, tf, &tf.Tasks[0])
	reloaded, err := loadCanonicalTasks(repo, stateRefTestPlanID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Tasks[0].Status != "in_progress" {
		t.Fatalf("save must persist despite mirror failure: %q", reloaded.Tasks[0].Status)
	}
}

// TestApplyCloseoutDecisionToTasks_MirrorErrorPropagates covers the closeout
// mirror-error return leg: a forced CAS failure surfaces as a wrapped error.
func TestApplyCloseoutDecisionToTasks_MirrorErrorPropagates(t *testing.T) {
	repo := seedStateRefRepo(t, config.WorkTrackingWriteToStateRef)
	prev := casSwapFn
	casSwapFn = func(string, string, string) error { return errors.New("forced CAS failure") }
	t.Cleanup(func() { casSwapFn = prev })
	closeout := workflowDelegationCloseoutRecord{Decision: "accept"}
	err := applyCloseoutDecisionToTasks(repo, stateRefTestPlanID, "t1", closeout)
	if err == nil || !strings.Contains(err.Error(), "mirror task status") {
		t.Fatalf("expected wrapped mirror error, got %v", err)
	}
}

// ── git-ref backend (work_tracking.backend) read path ──────────────────────────

// seedGitRefBackendRepo builds a git repo with a single active plan whose
// TASKS.yaml carries tasksYAML and an .agentsrc.json whose work_tracking block
// is wtJSON verbatim ("" ⇒ no work_tracking block, exercising the default
// local backend). Returns the repo root.
func seedGitRefBackendRepo(t *testing.T, wtJSON, tasksYAML string) string {
	t.Helper()
	agentsrc := `{"version":1,"project":"p","sources":[{"type":"local"}]}`
	if wtJSON != "" {
		agentsrc = `{"version":1,"project":"p","sources":[{"type":"local"}],"work_tracking":` + wtJSON + `}`
	}
	repo := t.TempDir()
	rel := ".agents/workflow/plans/" + stateRefTestPlanID + "/"
	testutil.InitGitRepo(t, repo, map[string]string{
		".agentsrc.json":   agentsrc,
		rel + "PLAN.yaml":  planYAMLActive(stateRefTestPlanID),
		rel + "TASKS.yaml": tasksYAML,
	})
	return repo
}

// writeWorktreeTasks overwrites the worktree TASKS.yaml for the state-ref test
// plan, diverging it from whatever is recorded on refs/agents/state.
func writeWorktreeTasks(t *testing.T, repo, content string) {
	t.Helper()
	path := filepath.Join(plansBaseDir(repo), stateRefTestPlanID, workflowTasksFileName)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// writeWorktreePlan overwrites the worktree PLAN.yaml for the state-ref test
// plan with the given status, diverging it from the ref copy.
func writeWorktreePlan(t *testing.T, repo, status string) {
	t.Helper()
	path := filepath.Join(plansBaseDir(repo), stateRefTestPlanID, workflowPlanFileName)
	content := "schema_version: 1\nid: \"" + stateRefTestPlanID + "\"\ntitle: \"" + stateRefTestPlanID + "\"\nstatus: \"" + status + "\"\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// TestLoadCanonicalTasks_GitRefBackendReadsRefProjection proves acceptance (a):
// under backend=git-ref, loadCanonicalTasks returns the per-task ref projection
// even when the worktree TASKS.yaml has diverged — the working copy is never
// consulted while the plan exists on the ref.
func TestLoadCanonicalTasks_GitRefBackendReadsRefProjection(t *testing.T) {
	repo := seedGitRefBackendRepo(t, `{"backend":"git-ref"}`, tasksYAMLWithStatus(stateRefTestPlanID, "in_progress"))
	if err := mirrorTransitionToStateRef(repo, stateRefTestPlanID, "t1"); err != nil {
		t.Fatalf("mirror: %v", err)
	}
	// Diverge the worktree copy AFTER mirroring the ref.
	writeWorktreeTasks(t, repo, tasksYAMLWithStatus(stateRefTestPlanID, "completed"))
	tf, err := loadCanonicalTasks(repo, stateRefTestPlanID)
	if err != nil {
		t.Fatalf("loadCanonicalTasks: %v", err)
	}
	if got := tf.Tasks[0].Status; got != "in_progress" {
		t.Fatalf("git-ref backend must return the ref projection (in_progress), got %q — worktree copy leaked", got)
	}
}

// TestLoadCanonicalPlan_GitRefBackendReadsRef proves loadCanonicalPlan resolves
// PLAN.yaml from the ref under backend=git-ref, ignoring a divergent worktree
// copy.
func TestLoadCanonicalPlan_GitRefBackendReadsRef(t *testing.T) {
	repo := seedGitRefBackendRepo(t, `{"backend":"git-ref"}`, tasksYAMLWithStatus(stateRefTestPlanID, "in_progress"))
	if err := mirrorTransitionToStateRef(repo, stateRefTestPlanID, "t1"); err != nil {
		t.Fatalf("mirror: %v", err)
	}
	writeWorktreePlan(t, repo, "paused")
	plan, err := loadCanonicalPlan(repo, stateRefTestPlanID)
	if err != nil {
		t.Fatalf("loadCanonicalPlan: %v", err)
	}
	if plan.Status != "active" {
		t.Fatalf("git-ref backend must return the ref PLAN.yaml (active), got %q", plan.Status)
	}
}

// TestLoadCanonical_GitRefBackendFallsBackWhenAbsentOnRef proves acceptance (b):
// when the plan has no blobs on refs/agents/state (a ref never written),
// loadCanonicalTasks and loadCanonicalPlan gracefully fall back to the
// per-worktree working copy.
func TestLoadCanonical_GitRefBackendFallsBackWhenAbsentOnRef(t *testing.T) {
	repo := seedGitRefBackendRepo(t, `{"backend":"git-ref"}`, tasksYAMLWithStatus(stateRefTestPlanID, "pending"))
	// No ref was ever written.
	if head := stateRefHead(repo); head != "" {
		t.Fatalf("precondition: state ref must be absent, got %q", head)
	}
	tf, err := loadCanonicalTasks(repo, stateRefTestPlanID)
	if err != nil {
		t.Fatalf("loadCanonicalTasks: %v", err)
	}
	if got := tf.Tasks[0].Status; got != "pending" {
		t.Fatalf("absent-on-ref must fall back to the worktree copy (pending), got %q", got)
	}
	plan, err := loadCanonicalPlan(repo, stateRefTestPlanID)
	if err != nil {
		t.Fatalf("loadCanonicalPlan: %v", err)
	}
	if plan.Status != "active" {
		t.Fatalf("absent-on-ref plan must fall back to the worktree copy (active), got %q", plan.Status)
	}
}

// TestRunWorkflowAdvance_GitRefBackendImpliesMirror proves acceptance (d): with
// backend=git-ref and write_to UNSET, a transition still mirrors to
// refs/agents/state (the backend implies the write mirror).
func TestRunWorkflowAdvance_GitRefBackendImpliesMirror(t *testing.T) {
	repo := seedGitRefBackendRepo(t, `{"backend":"git-ref"}`, tasksYAMLWithStatus(stateRefTestPlanID, "pending"))
	t.Setenv("AGENTS_HOME", t.TempDir())
	chdirRepo(t, repo)

	if err := runWorkflowAdvance(stateRefTestPlanID, "t1", "in_progress"); err != nil {
		t.Fatalf("advance: %v", err)
	}
	if stateRefHead(repo) == "" {
		t.Fatal("backend=git-ref must imply the state-ref mirror even with write_to unset")
	}
	relTask := ".agents/workflow/plans/" + stateRefTestPlanID + "/tasks/t1.yaml"
	blob := gitOut(t, repo, "show", stateRefName+":"+relTask)
	if !strings.Contains(blob, "in_progress") {
		t.Fatalf("per-task state ref blob missing new status; got:\n%s", blob)
	}
}

// TestCanonicalWriteToStateRef_GitRefBackendImplies is the focused unit for the
// implied-mirror guard: backend=git-ref activates canonicalWriteToStateRef (and
// useGitRefBackend) even without work_tracking.write_to.
func TestCanonicalWriteToStateRef_GitRefBackendImplies(t *testing.T) {
	repo := seedGitRefBackendRepo(t, `{"backend":"git-ref"}`, tasksYAMLWithStatus(stateRefTestPlanID, "pending"))
	if !canonicalWriteToStateRef(repo) {
		t.Fatal("backend=git-ref must imply the write mirror")
	}
	if !useGitRefBackend(repo) {
		t.Fatal("useGitRefBackend must be true for backend=git-ref")
	}
	// Default / local config activates neither.
	local := seedGitRefBackendRepo(t, "", tasksYAMLWithStatus(stateRefTestPlanID, "pending"))
	if canonicalWriteToStateRef(local) {
		t.Fatal("default config must not imply the write mirror")
	}
	if useGitRefBackend(local) {
		t.Fatal("default config must not use the git-ref backend")
	}
}

// TestRunWorkflowAdvance_GitRefBackendReadYourWrites proves acceptance (c): two
// sequential runWorkflowAdvance transitions to DIFFERENT tasks both persist. The
// worktree is deliberately reverted between the two advances to simulate the
// cross-worktree divergence read_from=master fell victim to; because the git-ref
// backend reads the LOCAL, in-process-updated state ref (read-your-writes safe),
// the 2nd advance sees t1's transition and does NOT clobber it.
func TestRunWorkflowAdvance_GitRefBackendReadYourWrites(t *testing.T) {
	repo := seedGitRefBackendRepo(t, `{"backend":"git-ref"}`, tasksYAMLTwoPending(stateRefTestPlanID))
	t.Setenv("AGENTS_HOME", t.TempDir())
	chdirRepo(t, repo)

	if err := runWorkflowAdvance(stateRefTestPlanID, "t1", "in_progress"); err != nil {
		t.Fatalf("advance t1: %v", err)
	}
	// Simulate a divergent worktree: a worktree-sourced read would lose t1's
	// transition here. The git-ref backend reads the state ref instead.
	writeWorktreeTasks(t, repo, tasksYAMLTwoPending(stateRefTestPlanID))
	if err := runWorkflowAdvance(stateRefTestPlanID, "t2", "in_progress"); err != nil {
		t.Fatalf("advance t2: %v", err)
	}

	tf, err := loadCanonicalTasks(repo, stateRefTestPlanID)
	if err != nil {
		t.Fatalf("loadCanonicalTasks: %v", err)
	}
	byID := map[string]string{}
	for _, task := range tf.Tasks {
		byID[task.ID] = task.Status
	}
	if byID["t1"] != "in_progress" {
		t.Fatalf("t1 transition clobbered by the t2 transition: %q (read_your_writes violated)", byID["t1"])
	}
	if byID["t2"] != "in_progress" {
		t.Fatalf("t2 transition lost: %q", byID["t2"])
	}
	projected, err := projectPlanTasksFromStateRef(repo, stateRefTestPlanID)
	if err != nil {
		t.Fatalf("project from ref: %v", err)
	}
	if len(projected.Tasks) != 2 {
		t.Fatalf("ref projection must carry both tasks, got %d", len(projected.Tasks))
	}
}

// TestLoadCanonicalTasks_GitRefBeatsReadFromMaster documents the precedence
// decision: when backend=git-ref AND read_from=master are both set, the git-ref
// backend wins for coordination reads (a LOCAL ref is read-your-writes safe,
// unlike the stale remote-tracking master ref). With no origin remote,
// read_from=master alone would fall back to the (divergent) worktree copy; the
// git-ref backend instead resolves the state ref.
func TestLoadCanonicalTasks_GitRefBeatsReadFromMaster(t *testing.T) {
	repo := seedGitRefBackendRepo(t, `{"backend":"git-ref","read_from":"master"}`, tasksYAMLWithStatus(stateRefTestPlanID, "in_progress"))
	if err := mirrorTransitionToStateRef(repo, stateRefTestPlanID, "t1"); err != nil {
		t.Fatalf("mirror: %v", err)
	}
	writeWorktreeTasks(t, repo, tasksYAMLWithStatus(stateRefTestPlanID, "completed"))
	tf, err := loadCanonicalTasks(repo, stateRefTestPlanID)
	if err != nil {
		t.Fatalf("loadCanonicalTasks: %v", err)
	}
	if got := tf.Tasks[0].Status; got != "in_progress" {
		t.Fatalf("git-ref must take precedence over read_from=master (want in_progress from the ref), got %q", got)
	}
}

// TestLoadCanonicalTasks_GitRefBackendMalformedBlobErrors covers the error-propagation
// leg of the git-ref read path: a malformed per-task blob on the ref surfaces as
// an error from loadCanonicalTasks rather than being silently swallowed.
func TestLoadCanonicalTasks_GitRefBackendMalformedBlobErrors(t *testing.T) {
	repo := seedGitRefBackendRepo(t, `{"backend":"git-ref"}`, tasksYAMLWithStatus(stateRefTestPlanID, "in_progress"))
	base := ".agents/workflow/plans/" + stateRefTestPlanID + "/tasks/"
	if err := writeStateRefCAS(repo, []stateRefFile{{relPath: base + "t1.yaml", content: []byte("this: : not valid [")}}); err != nil {
		t.Fatal(err)
	}
	if _, err := loadCanonicalTasks(repo, stateRefTestPlanID); err == nil {
		t.Fatal("malformed per-task blob must surface as an error under the git-ref backend")
	}
}

// TestLoadCanonicalPlan_GitRefBackendPlanAbsentFallsBack covers the leg where
// the state ref EXISTS (a per-task blob was written) but PLAN.yaml is NOT on it:
// loadCanonicalPlan gracefully falls back to the worktree copy.
func TestLoadCanonicalPlan_GitRefBackendPlanAbsentFallsBack(t *testing.T) {
	repo := seedGitRefBackendRepo(t, `{"backend":"git-ref"}`, tasksYAMLWithStatus(stateRefTestPlanID, "in_progress"))
	// Write only a task blob to the ref — PLAN.yaml is deliberately absent.
	base := ".agents/workflow/plans/" + stateRefTestPlanID + "/tasks/"
	blob := "schema_version: 1\nplan_id: \"" + stateRefTestPlanID + "\"\norder: 0\ntask:\n  id: \"t1\"\n  title: \"task one\"\n  status: \"in_progress\"\n  verification_required: true\n"
	if err := writeStateRefCAS(repo, []stateRefFile{{relPath: base + "t1.yaml", content: []byte(blob)}}); err != nil {
		t.Fatal(err)
	}
	if stateRefHead(repo) == "" {
		t.Fatal("precondition: state ref must exist")
	}
	plan, err := loadCanonicalPlan(repo, stateRefTestPlanID)
	if err != nil {
		t.Fatalf("loadCanonicalPlan: %v", err)
	}
	if plan.Status != "active" {
		t.Fatalf("PLAN.yaml absent on the ref must fall back to the worktree copy (active), got %q", plan.Status)
	}
}

// TestLoadCanonical_GitRefBackendMissingFileErrors covers the fallback os.ReadFile
// error legs: under backend=git-ref, a plan present on neither the ref nor the
// working copy surfaces the not-found error from loadCanonicalTasks and
// loadCanonicalPlan (rather than a silent empty result).
func TestLoadCanonical_GitRefBackendMissingFileErrors(t *testing.T) {
	repo := seedGitRefBackendRepo(t, `{"backend":"git-ref"}`, tasksYAMLWithStatus(stateRefTestPlanID, "pending"))
	if _, err := loadCanonicalTasks(repo, "ghost-plan"); err == nil {
		t.Fatal("missing plan (absent on ref and worktree) must error from loadCanonicalTasks")
	}
	if _, err := loadCanonicalPlan(repo, "ghost-plan"); err == nil {
		t.Fatal("missing plan (absent on ref and worktree) must error from loadCanonicalPlan")
	}
}

// ── canonical-write choke-point mirror (#433) ──────────────────────────────────

// refCommitCount returns the number of commits reachable from refs/agents/state,
// or 0 when the ref is absent. Used to assert exactly-one-commit-per-write.
func refCommitCount(t *testing.T, repo string) int {
	t.Helper()
	if stateRefHead(repo) == "" {
		return 0
	}
	out := gitOut(t, repo, "rev-list", "--count", stateRefName)
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		t.Fatalf("parse rev-list count %q: %v", out, err)
	}
	return n
}

// refHasTask reports whether taskID is present in planID's per-task projection on
// refs/agents/state.
func refHasTask(t *testing.T, repo, taskID string) bool {
	t.Helper()
	projected, err := projectPlanTasksFromStateRef(repo, stateRefTestPlanID)
	if err != nil {
		t.Fatalf("project from ref: %v", err)
	}
	for _, task := range projected.Tasks {
		if task.ID == taskID {
			return true
		}
	}
	return false
}

// TestRunWorkflowTaskAdd_GitRefBackendMirrorsAndReadYourWrites is THE regression
// that would have caught #433: under backend=git-ref a STRUCTURAL task add (not a
// status transition) must mirror to refs/agents/state at the canonical-write
// choke point, so a fresh read (which resolves from the ref) sees it — and two
// sequential adds both survive rather than the second clobbering the first via a
// stale ref. The ref is pre-established (a seed transition) to reproduce the
// exact precondition under which the clobber bit.
func TestRunWorkflowTaskAdd_GitRefBackendMirrorsAndReadYourWrites(t *testing.T) {
	repo := seedGitRefBackendRepo(t, `{"backend":"git-ref"}`, tasksYAMLWithStatus(stateRefTestPlanID, "pending"))
	t.Setenv("AGENTS_HOME", t.TempDir())
	chdirRepo(t, repo)

	// Establish the ref so subsequent reads resolve from it (the precondition
	// under which the stale-ref clobber bit).
	if err := runWorkflowAdvance(stateRefTestPlanID, "t1", "in_progress"); err != nil {
		t.Fatalf("seed advance: %v", err)
	}
	// First STRUCTURAL add (not a transition): must mirror to the ref.
	if err := runWorkflowTaskAdd(taskAddInputs{PlanID: stateRefTestPlanID, TaskID: "t2", Title: "second"}); err != nil {
		t.Fatalf("add t2: %v", err)
	}
	if !refHasTask(t, repo, "t2") {
		t.Fatal("structural add was not mirrored to the ref (stale-ref clobber bug)")
	}
	// Second sequential add loads from the FRESH ref (read-your-writes), so it
	// must NOT clobber t2. Both new tasks survive alongside t1.
	if err := runWorkflowTaskAdd(taskAddInputs{PlanID: stateRefTestPlanID, TaskID: "t3", Title: "third"}); err != nil {
		t.Fatalf("add t3: %v", err)
	}
	tf, err := loadCanonicalTasks(repo, stateRefTestPlanID)
	if err != nil {
		t.Fatalf("loadCanonicalTasks: %v", err)
	}
	got := map[string]bool{}
	for _, task := range tf.Tasks {
		got[task.ID] = true
	}
	for _, id := range []string{"t1", "t2", "t3"} {
		if !got[id] {
			t.Fatalf("task %q lost after two sequential adds (clobber); have %v", id, got)
		}
	}
	if len(tf.Tasks) != 3 {
		t.Fatalf("expected 3 tasks after two adds, got %d", len(tf.Tasks))
	}
}

// TestRunWorkflowTaskUpdate_GitRefBackendMirrorsToRef proves a STRUCTURAL task
// update (title + notes, not a transition) is mirrored to the ref so a fresh read
// returns the updated fields — the update leg of the #433 fix.
func TestRunWorkflowTaskUpdate_GitRefBackendMirrorsToRef(t *testing.T) {
	repo := seedGitRefBackendRepo(t, `{"backend":"git-ref"}`, tasksYAMLWithStatus(stateRefTestPlanID, "pending"))
	t.Setenv("AGENTS_HOME", t.TempDir())
	chdirRepo(t, repo)

	if err := runWorkflowAdvance(stateRefTestPlanID, "t1", "in_progress"); err != nil {
		t.Fatalf("seed advance: %v", err)
	}
	if err := runWorkflowTaskUpdate(stateRefTestPlanID, "t1", "renamed title", "fresh notes", "", "", ""); err != nil {
		t.Fatalf("update: %v", err)
	}
	tf, err := loadCanonicalTasks(repo, stateRefTestPlanID)
	if err != nil {
		t.Fatalf("loadCanonicalTasks: %v", err)
	}
	if tf.Tasks[0].Title != "renamed title" || tf.Tasks[0].Notes != "fresh notes" {
		t.Fatalf("update not ref-visible: title=%q notes=%q", tf.Tasks[0].Title, tf.Tasks[0].Notes)
	}
	// The per-task blob on the ref itself carries the update.
	relTask := ".agents/workflow/plans/" + stateRefTestPlanID + "/tasks/t1.yaml"
	blob := gitOut(t, repo, "show", stateRefName+":"+relTask)
	if !strings.Contains(blob, "renamed title") || !strings.Contains(blob, "fresh notes") {
		t.Fatalf("per-task ref blob missing the update:\n%s", blob)
	}
}

// TestRunWorkflowPlanUpdate_GitRefBackendMirrorsPlan proves a plan-only canonical
// write (plan update) mirrors PLAN.yaml to the ref via saveCanonicalPlanMirrored.
func TestRunWorkflowPlanUpdate_GitRefBackendMirrorsPlan(t *testing.T) {
	repo := seedGitRefBackendRepo(t, `{"backend":"git-ref"}`, tasksYAMLWithStatus(stateRefTestPlanID, "pending"))
	t.Setenv("AGENTS_HOME", t.TempDir())
	chdirRepo(t, repo)

	if err := runWorkflowAdvance(stateRefTestPlanID, "t1", "in_progress"); err != nil {
		t.Fatalf("seed advance: %v", err)
	}
	if err := runWorkflowPlanUpdate(stateRefTestPlanID, "paused", "", "", "", "", ""); err != nil {
		t.Fatalf("plan update: %v", err)
	}
	plan, err := loadCanonicalPlan(repo, stateRefTestPlanID)
	if err != nil {
		t.Fatalf("loadCanonicalPlan: %v", err)
	}
	if plan.Status != "paused" {
		t.Fatalf("plan status update not ref-visible: %q", plan.Status)
	}
}

// TestRunWorkflowAdvance_GitRefBackendSingleRefCommit guards the consolidation:
// with the choke-point mirror folded into saveCanonicalTasksMirrored and the
// old explicit mirror removed, ONE advance produces EXACTLY one new ref commit
// (no double CAS).
func TestRunWorkflowAdvance_GitRefBackendSingleRefCommit(t *testing.T) {
	repo := seedGitRefBackendRepo(t, `{"backend":"git-ref"}`, tasksYAMLTwoPending(stateRefTestPlanID))
	t.Setenv("AGENTS_HOME", t.TempDir())
	chdirRepo(t, repo)

	if err := runWorkflowAdvance(stateRefTestPlanID, "t1", "in_progress"); err != nil {
		t.Fatalf("advance t1: %v", err)
	}
	after1 := refCommitCount(t, repo)
	if after1 != 1 {
		t.Fatalf("first advance must produce exactly 1 ref commit, got %d", after1)
	}
	if err := runWorkflowAdvance(stateRefTestPlanID, "t2", "in_progress"); err != nil {
		t.Fatalf("advance t2: %v", err)
	}
	if delta := refCommitCount(t, repo) - after1; delta != 1 {
		t.Fatalf("second advance must produce exactly 1 more ref commit, got delta %d", delta)
	}
}

// TestSaveCanonicalTasksMirrored_MirrorFailureModeAware proves the mode-aware
// failure policy: under backend=git-ref (the ref IS the read source) a forced CAS
// failure PROPAGATES so the caller retries; under the additive write_to=state-ref
// mode it is WARNED and swallowed. Either way the working-copy write stays durable
// and no ref is left behind.
func TestSaveCanonicalTasksMirrored_MirrorFailureModeAware(t *testing.T) {
	prev := casSwapFn
	t.Cleanup(func() { casSwapFn = prev })
	casSwapFn = func(projectPath, newCommit, old string) error {
		return fmt.Errorf("simulated CAS failure")
	}
	newTF := func() *CanonicalTaskFile {
		return &CanonicalTaskFile{SchemaVersion: 1, PlanID: stateRefTestPlanID, Tasks: []CanonicalTask{
			{ID: "t1", Title: "one", Status: "in_progress", VerificationRequired: true},
		}}
	}

	// backend=git-ref: a failed mirror PROPAGATES.
	gitRef := seedGitRefBackendRepo(t, `{"backend":"git-ref"}`, tasksYAMLWithStatus(stateRefTestPlanID, "pending"))
	if err := saveCanonicalTasksMirrored(gitRef, newTF(), "t1"); err == nil {
		t.Fatal("backend=git-ref: a failed mirror must propagate (ref is the read source)")
	}
	assertTasksFileContains(t, gitRef, "in_progress")
	if stateRefHead(gitRef) != "" {
		t.Fatal("a failed mirror must leave no ref behind")
	}

	// additive write_to=state-ref: a failed mirror is WARNED, not propagated.
	additive := seedGitRefBackendRepo(t, `{"write_to":"state-ref"}`, tasksYAMLWithStatus(stateRefTestPlanID, "pending"))
	if err := saveCanonicalTasksMirrored(additive, newTF(), "t1"); err != nil {
		t.Fatalf("write_to=state-ref: a failed mirror must be warned, not fatal: %v", err)
	}
	assertTasksFileContains(t, additive, "in_progress")
	if stateRefHead(additive) != "" {
		t.Fatal("a failed mirror must leave no ref behind")
	}
}

// assertTasksFileContains fails unless the state-ref test plan's working-copy
// TASKS.yaml contains want (proving the working-copy write is durable).
func assertTasksFileContains(t *testing.T, repo, want string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(plansBaseDir(repo), stateRefTestPlanID, workflowTasksFileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), want) {
		t.Fatalf("working-copy TASKS.yaml must contain %q even when the mirror fails", want)
	}
}

// TestCasWriteStateRef_IdempotentTreeIsNoop proves the tree-equality idempotency
// guard: re-writing byte-identical content produces NO new commit (the ref does
// not advance), so a redundant mirror across the choke point is a no-op.
func TestCasWriteStateRef_IdempotentTreeIsNoop(t *testing.T) {
	repo := seedStateRefRepo(t, config.WorkTrackingWriteToStateRef)
	files := []stateRefFile{{relPath: ".agents/workflow/state/a.txt", content: []byte("a\n")}}
	if err := writeStateRefCAS(repo, files); err != nil {
		t.Fatalf("first write: %v", err)
	}
	first := stateRefHead(repo)
	if first == "" {
		t.Fatal("first write must create the ref")
	}
	if err := writeStateRefCAS(repo, files); err != nil {
		t.Fatalf("idempotent re-write: %v", err)
	}
	if got := stateRefHead(repo); got != first {
		t.Fatalf("identical re-write must not advance the ref: %s -> %s", first, got)
	}
}

// TestSaveCanonicalTasksMirrored_SaveErrorPropagates proves a working-copy save
// failure is propagated (and the mirror is never reached).
func TestSaveCanonicalTasksMirrored_SaveErrorPropagates(t *testing.T) {
	sentinel := errors.New("save boom")
	withMkdirAllStub(t, func(string, os.FileMode) error { return sentinel })
	err := saveCanonicalTasksMirrored(t.TempDir(), &CanonicalTaskFile{PlanID: "p1"}, "t1")
	if !errors.Is(err, sentinel) {
		t.Fatalf("save error must propagate before the mirror, got %v", err)
	}
}

// TestSaveCanonicalPlanMirrored_SaveErrorPropagates proves a working-copy plan
// save failure is propagated (and the mirror is never reached).
func TestSaveCanonicalPlanMirrored_SaveErrorPropagates(t *testing.T) {
	sentinel := errors.New("save boom")
	withMkdirAllStub(t, func(string, os.FileMode) error { return sentinel })
	err := saveCanonicalPlanMirrored(t.TempDir(), &CanonicalPlan{ID: "p1"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("save error must propagate before the mirror, got %v", err)
	}
}

// TestSaveCanonicalPlanMirrored_MirrorFailureModeAware proves the plan-only
// mirror follows the same mode-aware failure policy: propagate under
// backend=git-ref, warn under the additive write_to=state-ref mode.
func TestSaveCanonicalPlanMirrored_MirrorFailureModeAware(t *testing.T) {
	prev := casSwapFn
	t.Cleanup(func() { casSwapFn = prev })
	casSwapFn = func(projectPath, newCommit, old string) error {
		return fmt.Errorf("simulated CAS failure")
	}
	newPlan := func() *CanonicalPlan {
		return &CanonicalPlan{SchemaVersion: 1, ID: stateRefTestPlanID, Title: "t", Status: "paused"}
	}

	gitRef := seedGitRefBackendRepo(t, `{"backend":"git-ref"}`, tasksYAMLWithStatus(stateRefTestPlanID, "pending"))
	if err := saveCanonicalPlanMirrored(gitRef, newPlan()); err == nil {
		t.Fatal("backend=git-ref: a failed plan mirror must propagate")
	}
	if stateRefHead(gitRef) != "" {
		t.Fatal("a failed mirror must leave no ref behind")
	}

	additive := seedGitRefBackendRepo(t, `{"write_to":"state-ref"}`, tasksYAMLWithStatus(stateRefTestPlanID, "pending"))
	if err := saveCanonicalPlanMirrored(additive, newPlan()); err != nil {
		t.Fatalf("write_to=state-ref: a failed plan mirror must be warned, not fatal: %v", err)
	}
	if stateRefHead(additive) != "" {
		t.Fatal("a failed mirror must leave no ref behind")
	}
}

// TestRunWorkflowTaskAdd_DefaultConfigWritesNoStateRef is the HARD gate for the
// new choke-point path: with default / unset work_tracking config a structural
// task add writes ONLY the working copy and NO git ref (byte-identical to today).
func TestRunWorkflowTaskAdd_DefaultConfigWritesNoStateRef(t *testing.T) {
	repo := seedGitRefBackendRepo(t, "", tasksYAMLWithStatus(stateRefTestPlanID, "pending"))
	t.Setenv("AGENTS_HOME", t.TempDir())
	chdirRepo(t, repo)

	if err := runWorkflowTaskAdd(taskAddInputs{PlanID: stateRefTestPlanID, TaskID: "t2", Title: "second"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if head := stateRefHead(repo); head != "" {
		t.Fatalf("default config must write NO state ref, got %q", head)
	}
	tf, err := loadCanonicalTasks(repo, stateRefTestPlanID)
	if err != nil {
		t.Fatalf("loadCanonicalTasks: %v", err)
	}
	if len(tf.Tasks) != 2 {
		t.Fatalf("working copy must still carry both tasks, got %d", len(tf.Tasks))
	}
}

// TestCanonicalWriters_PlanLoadErrorPropagates covers the plan-load error leg
// inside each writer's lock (advance / task add / task update): a plan dir with
// TASKS.yaml but no PLAN.yaml surfaces the load error rather than proceeding.
func TestCanonicalWriters_PlanLoadErrorPropagates(t *testing.T) {
	newRepoNoPlan := func() string {
		repo := seedGitRefBackendRepo(t, "", tasksYAMLWithStatus(stateRefTestPlanID, "pending"))
		if err := os.Remove(filepath.Join(plansBaseDir(repo), stateRefTestPlanID, workflowPlanFileName)); err != nil {
			t.Fatal(err)
		}
		t.Setenv("AGENTS_HOME", t.TempDir())
		chdirRepo(t, repo)
		return repo
	}

	newRepoNoPlan()
	if err := runWorkflowAdvance(stateRefTestPlanID, "t1", "in_progress"); err == nil {
		t.Fatal("advance must surface the plan-load error")
	}
	newRepoNoPlan()
	if err := runWorkflowTaskAdd(taskAddInputs{PlanID: stateRefTestPlanID, TaskID: "t2", Title: "x"}); err == nil {
		t.Fatal("task add must surface the plan-load error")
	}
	newRepoNoPlan()
	if err := runWorkflowTaskUpdate(stateRefTestPlanID, "t1", "new", "", "", "", ""); err == nil {
		t.Fatal("task update must surface the plan-load error")
	}
}
