package workflow

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/testutil"
)

const (
	testAgentsRCName  = ".agentsrc.json"
	testAgentsRCLocal = `{"version":1,"project":"p","sources":[{"type":"local"}]}`
	msgReconcileErr   = "reconcile: %v"
)

// tasksYAMLMulti renders a TASKS.yaml with one pending task per id.
func tasksYAMLMulti(planID string, taskIDs ...string) string {
	var b strings.Builder
	b.WriteString("schema_version: 1\nplan_id: \"" + planID + "\"\ntasks:\n")
	for _, id := range taskIDs {
		b.WriteString("  - id: \"" + id + "\"\n")
		b.WriteString("    title: \"" + id + "\"\n")
		b.WriteString("    status: \"pending\"\n")
		b.WriteString("    verification_required: true\n")
	}
	return b.String()
}

// seedReconcileRepo builds a git repo with a single active plan whose
// working-copy TASKS.yaml carries taskIDs and NO work_tracking block, so
// reconcile is exercised independent of any coordination backend.
func seedReconcileRepo(t *testing.T, planID string, taskIDs ...string) string {
	t.Helper()
	repo := t.TempDir()
	rel := ".agents/workflow/plans/" + planID + "/"
	testutil.InitGitRepo(t, repo, map[string]string{
		testAgentsRCName:   testAgentsRCLocal,
		rel + "PLAN.yaml":  planYAMLActive(planID),
		rel + "TASKS.yaml": tasksYAMLMulti(planID, taskIDs...),
	})
	return repo
}

// writeWCTasks overwrites planID's working-copy TASKS.yaml.
func writeWCTasks(t *testing.T, repo, planID, content string) {
	t.Helper()
	path := filepath.Join(plansBaseDir(repo), planID, workflowTasksFileName)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// seedRefSubset mirrors ONLY taskIDs onto refs/agents/state, reproducing the
// pre-#434 stale-subset state where the ref holds a strict subset of the plan's
// working-copy tasks. It temporarily narrows the working-copy TASKS.yaml to the
// subset, runs the shipped seed path, then restores the full working copy.
func seedRefSubset(t *testing.T, repo, planID string, taskIDs ...string) {
	t.Helper()
	path := filepath.Join(plansBaseDir(repo), planID, workflowTasksFileName)
	full, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	writeWCTasks(t, repo, planID, tasksYAMLMulti(planID, taskIDs...))
	overwrite, seed, err := collectPlanTaskStateRefWrite(repo, planID, "")
	if err != nil {
		t.Fatalf("seed subset collect: %v", err)
	}
	if err := writePlanStateRefCAS(repo, overwrite, seed); err != nil {
		t.Fatalf("seed subset write: %v", err)
	}
	if err := os.WriteFile(path, full, 0644); err != nil {
		t.Fatal(err)
	}
}

// refTaskSet returns the sorted task ids currently blobbed for planID on the ref.
func refTaskSet(t *testing.T, repo, planID string) []string {
	t.Helper()
	set, err := stateRefPlanTaskIDs(repo, planID)
	if err != nil {
		t.Fatalf("stateRefPlanTaskIDs: %v", err)
	}
	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// TestStateRefReconcile_StaleSubsetSeedsMissingTask proves acceptance (a): a
// plan whose ref holds a stale SUBSET (missing t2, t3) is reconciled so the
// ref's task-set equals the working-copy task-set.
func TestStateRefReconcile_StaleSubsetSeedsMissingTask(t *testing.T) {
	repo := seedReconcileRepo(t, "p1", "t1", "t2", "t3")
	chdirRepo(t, repo)
	seedRefSubset(t, repo, "p1", "t1")

	if got := refTaskSet(t, repo, "p1"); len(got) != 1 || got[0] != "t1" {
		t.Fatalf("precondition: ref should hold only t1, got %v", got)
	}

	if err := runWorkflowStateRefReconcile(io.Discard, stateRefReconcileOpts{}); err != nil {
		t.Fatalf(msgReconcileErr, err)
	}

	want := []string{"t1", "t2", "t3"}
	if got := refTaskSet(t, repo, "p1"); !equalStrings(got, want) {
		t.Fatalf("ref task-set != working-copy task-set: got %v want %v", got, want)
	}
}

// TestStateRefReconcile_AbsentPlanFullySeeded proves acceptance (b): a plan
// absent from the ref (the ref does not exist at all) is fully seeded and the
// ref is created.
func TestStateRefReconcile_AbsentPlanFullySeeded(t *testing.T) {
	repo := seedReconcileRepo(t, "p1", "t1", "t2")
	chdirRepo(t, repo)

	if head := stateRefHead(repo); head != "" {
		t.Fatalf("precondition: no ref expected, got %s", head)
	}

	var buf bytes.Buffer
	if err := runWorkflowStateRefReconcile(&buf, stateRefReconcileOpts{}); err != nil {
		t.Fatalf(msgReconcileErr, err)
	}

	if stateRefHead(repo) == "" {
		t.Fatal("reconcile must create refs/agents/state from the working copy")
	}
	if got := refTaskSet(t, repo, "p1"); !equalStrings(got, []string{"t1", "t2"}) {
		t.Fatalf("plan not fully seeded: got %v", got)
	}
	if !strings.Contains(buf.String(), "ref created") {
		t.Fatalf("summary should note ref creation; got:\n%s", buf.String())
	}
}

// TestStateRefReconcile_IdempotentSecondRunNoNewCommit proves acceptance (c): a
// second reconcile with nothing to change makes NO new ref commit (the ref OID
// is unchanged) via the tree-equality guard.
func TestStateRefReconcile_IdempotentSecondRunNoNewCommit(t *testing.T) {
	repo := seedReconcileRepo(t, "p1", "t1", "t2")
	chdirRepo(t, repo)

	if err := runWorkflowStateRefReconcile(io.Discard, stateRefReconcileOpts{}); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	oid1 := stateRefHead(repo)
	if oid1 == "" {
		t.Fatal("first reconcile must create the ref")
	}

	if err := runWorkflowStateRefReconcile(io.Discard, stateRefReconcileOpts{}); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	oid2 := stateRefHead(repo)
	if oid1 != oid2 {
		t.Fatalf("second reconcile must not commit: oid1=%s oid2=%s", oid1, oid2)
	}
}

// TestStateRefReconcile_DryRunWritesNothing proves acceptance (d): --dry-run on
// a stale-subset plan reports the tasks it WOULD seed but leaves the ref OID
// unchanged and seeds nothing.
func TestStateRefReconcile_DryRunWritesNothing(t *testing.T) {
	repo := seedReconcileRepo(t, "p1", "t1", "t2", "t3")
	chdirRepo(t, repo)
	seedRefSubset(t, repo, "p1", "t1")
	before := stateRefHead(repo)

	var buf bytes.Buffer
	if err := runWorkflowStateRefReconcile(&buf, stateRefReconcileOpts{dryRun: true}); err != nil {
		t.Fatalf("dry-run reconcile: %v", err)
	}

	if after := stateRefHead(repo); after != before {
		t.Fatalf("dry-run must not move the ref: before=%s after=%s", before, after)
	}
	if got := refTaskSet(t, repo, "p1"); len(got) != 1 {
		t.Fatalf("dry-run must not seed: ref task-set=%v", got)
	}
	out := buf.String()
	if !strings.Contains(out, "[dry-run]") || !strings.Contains(out, "would reconcile") {
		t.Fatalf("dry-run report missing markers; got:\n%s", out)
	}
	if !strings.Contains(out, "t2") || !strings.Contains(out, "t3") {
		t.Fatalf("dry-run should list would-seed tasks t2,t3; got:\n%s", out)
	}
}

// TestStateRefReconcile_DryRunAbsentRefStaysAbsent proves --dry-run never
// creates the ref when it does not yet exist.
func TestStateRefReconcile_DryRunAbsentRefStaysAbsent(t *testing.T) {
	repo := seedReconcileRepo(t, "p1", "t1")
	chdirRepo(t, repo)

	if err := runWorkflowStateRefReconcile(io.Discard, stateRefReconcileOpts{dryRun: true}); err != nil {
		t.Fatalf("dry-run reconcile: %v", err)
	}
	if head := stateRefHead(repo); head != "" {
		t.Fatalf("dry-run must not create the ref, got %s", head)
	}
}

// TestStateRefReconcile_AlreadyConsistentIsNoop proves acceptance (e): a plan
// already fully mirrored is a no-op — the per-plan result reports no change and
// no seeded tasks.
func TestStateRefReconcile_AlreadyConsistentIsNoop(t *testing.T) {
	repo := seedReconcileRepo(t, "p1", "t1", "t2")
	chdirRepo(t, repo)
	seedRefSubset(t, repo, "p1", "t1", "t2") // ref already holds the full set

	pr, err := reconcilePlanToStateRef(repo, "p1", false)
	if err != nil {
		t.Fatalf("reconcile plan: %v", err)
	}
	if pr.Changed {
		t.Fatalf("already-consistent plan must not change: %+v", pr)
	}
	if len(pr.Seeded) != 0 {
		t.Fatalf("already-consistent plan must seed nothing: %v", pr.Seeded)
	}
	if pr.TasksInWC != 2 || pr.TasksOnRef != 2 {
		t.Fatalf("unexpected counts: %+v", pr)
	}
}

// TestStateRefReconcile_JSONReport exercises the machine-readable envelope via
// the local --json flag: the report enumerates plans and the reconcile delta.
func TestStateRefReconcile_JSONReport(t *testing.T) {
	repo := seedReconcileRepo(t, "p1", "t1", "t2")
	chdirRepo(t, repo)
	seedRefSubset(t, repo, "p1", "t1")

	var buf bytes.Buffer
	if err := runWorkflowStateRefReconcile(&buf, stateRefReconcileOpts{json: true}); err != nil {
		t.Fatalf(msgReconcileErr, err)
	}

	var result stateRefReconcileResult
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("decode json: %v\n%s", err, buf.String())
	}
	if result.StateRef != stateRefName {
		t.Fatalf("state_ref = %q", result.StateRef)
	}
	if result.PlansScanned != 1 || result.PlansChanged != 1 {
		t.Fatalf("counts: scanned=%d changed=%d", result.PlansScanned, result.PlansChanged)
	}
	if len(result.Plans) != 1 || result.Plans[0].Plan != "p1" {
		t.Fatalf("plans: %+v", result.Plans)
	}
	if !equalStrings(result.Plans[0].Seeded, []string{"t2"}) {
		t.Fatalf("seeded should be [t2], got %v", result.Plans[0].Seeded)
	}
}

// TestStateRefReconcile_JSONHonorsGlobalFlag proves the global --json flag path
// (deps.Flags.JSON) also selects the JSON envelope.
func TestStateRefReconcile_JSONHonorsGlobalFlag(t *testing.T) {
	repo := seedReconcileRepo(t, "p1", "t1")
	chdirRepo(t, repo)

	prev := workflowTestJSON
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = prev })

	var buf bytes.Buffer
	if err := runWorkflowStateRefReconcile(&buf, stateRefReconcileOpts{}); err != nil {
		t.Fatalf(msgReconcileErr, err)
	}
	var result stateRefReconcileResult
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("global --json must emit JSON: %v\n%s", err, buf.String())
	}
}

// TestStateRefReconcile_RefResidentPlanWithoutWorkingCopySkipped proves a plan
// present on the ref but with no working-copy TASKS.yaml is enumerated and
// reported as skipped (it cannot be reconciled FROM a working copy that is gone).
func TestStateRefReconcile_RefResidentPlanWithoutWorkingCopySkipped(t *testing.T) {
	repo := seedReconcileRepo(t, "gone", "t1", "t2")
	chdirRepo(t, repo)
	seedRefSubset(t, repo, "gone", "t1", "t2") // now on the ref

	// Remove the plan's working-copy directory entirely.
	if err := os.RemoveAll(filepath.Join(plansBaseDir(repo), "gone")); err != nil {
		t.Fatal(err)
	}

	// The ref still lists the plan.
	refPlans, err := stateRefResidentPlanIDs(repo)
	if err != nil {
		t.Fatalf("stateRefResidentPlanIDs: %v", err)
	}
	if !contains(refPlans, "gone") {
		t.Fatalf("ref-resident plans should include 'gone', got %v", refPlans)
	}

	var buf bytes.Buffer
	if err := runWorkflowStateRefReconcile(&buf, stateRefReconcileOpts{json: true}); err != nil {
		t.Fatalf(msgReconcileErr, err)
	}
	var result stateRefReconcileResult
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if len(result.Plans) != 1 || result.Plans[0].Plan != "gone" {
		t.Fatalf("plans: %+v", result.Plans)
	}
	if result.Plans[0].Skipped == "" || result.Plans[0].Changed {
		t.Fatalf("plan without working copy must be skipped, unchanged: %+v", result.Plans[0])
	}
}

// TestStateRefReconcile_MultiPlanUnionAndTextSummary proves the plan universe is
// the union of ref-resident and working-copy plans and the text summary counts
// scanned/changed plans.
func TestStateRefReconcile_MultiPlanUnionAndTextSummary(t *testing.T) {
	repo := seedReconcileRepo(t, "alpha", "t1", "t2")
	chdirRepo(t, repo)
	// A second working-copy plan absent from the ref.
	writeExtraPlan(t, repo, "beta", "b1")
	seedRefSubset(t, repo, "alpha", "t1") // alpha ref subset

	var buf bytes.Buffer
	if err := runWorkflowStateRefReconcile(&buf, stateRefReconcileOpts{}); err != nil {
		t.Fatalf(msgReconcileErr, err)
	}
	out := buf.String()
	if !strings.Contains(out, "alpha:") || !strings.Contains(out, "beta:") {
		t.Fatalf("both plans should appear in report; got:\n%s", out)
	}
	if !strings.Contains(out, "2 plan(s) scanned, 2 changed") {
		t.Fatalf("summary counts wrong; got:\n%s", out)
	}
	if got := refTaskSet(t, repo, "alpha"); !equalStrings(got, []string{"t1", "t2"}) {
		t.Fatalf("alpha not reconciled: %v", got)
	}
	if got := refTaskSet(t, repo, "beta"); !equalStrings(got, []string{"b1"}) {
		t.Fatalf("beta not seeded: %v", got)
	}
}

// TestStateRefReconcile_MalformedWorkingCopyTasksErrors proves a malformed
// working-copy TASKS.yaml surfaces as an error rather than silently mirroring
// garbage.
func TestStateRefReconcile_MalformedWorkingCopyTasksErrors(t *testing.T) {
	repo := seedReconcileRepo(t, "p1", "t1")
	chdirRepo(t, repo)
	writeWCTasks(t, repo, "p1", "bad: : [")

	if err := runWorkflowStateRefReconcile(io.Discard, stateRefReconcileOpts{}); err == nil {
		t.Fatal("malformed TASKS.yaml must surface an error")
	}
}

// TestStateRefReconcile_NoPlansReportsEmpty proves an empty plans directory
// reconciles cleanly with an empty report.
func TestStateRefReconcile_NoPlansReportsEmpty(t *testing.T) {
	repo := t.TempDir()
	testutil.InitGitRepo(t, repo, map[string]string{
		testAgentsRCName: testAgentsRCLocal,
	})
	chdirRepo(t, repo)

	var buf bytes.Buffer
	if err := runWorkflowStateRefReconcile(&buf, stateRefReconcileOpts{}); err != nil {
		t.Fatalf(msgReconcileErr, err)
	}
	if !strings.Contains(buf.String(), "no plans found") {
		t.Fatalf("empty repo should report no plans; got:\n%s", buf.String())
	}
	if head := stateRefHead(repo); head != "" {
		t.Fatalf("no plans must write no ref, got %s", head)
	}
}

// TestStateRefReconcile_GitRefBackendReadsWorkingCopyNotRef proves the CRITICAL
// contract: under backend=git-ref, reconcile reads the WORKING COPY (not the
// backend-aware ref projection), so a working-copy task ABSENT from the ref is
// still seeded rather than being masked by the ref read path.
func TestStateRefReconcile_GitRefBackendReadsWorkingCopyNotRef(t *testing.T) {
	repo := t.TempDir()
	rel := ".agents/workflow/plans/p1/"
	testutil.InitGitRepo(t, repo, map[string]string{
		testAgentsRCName:   `{"version":1,"project":"p","sources":[{"type":"local"}],"work_tracking":{"backend":"git-ref"}}`,
		rel + "PLAN.yaml":  planYAMLActive("p1"),
		rel + "TASKS.yaml": tasksYAMLMulti("p1", "t1", "t2"),
	})
	chdirRepo(t, repo)
	seedRefSubset(t, repo, "p1", "t1") // ref holds only t1 under git-ref backend

	if err := runWorkflowStateRefReconcile(io.Discard, stateRefReconcileOpts{}); err != nil {
		t.Fatalf(msgReconcileErr, err)
	}
	if got := refTaskSet(t, repo, "p1"); !equalStrings(got, []string{"t1", "t2"}) {
		t.Fatalf("git-ref backend reconcile must seed from working copy: got %v", got)
	}
}

// TestStateRefReconcile_WriteErrorPropagates proves a CAS write failure surfaces
// as an error (wrapped with the plan id) rather than being swallowed.
func TestStateRefReconcile_WriteErrorPropagates(t *testing.T) {
	repo := seedReconcileRepo(t, "p1", "t1", "t2")
	chdirRepo(t, repo)

	prev := casSwapFn
	casSwapFn = func(string, string, string) error { return errAlwaysConflict }
	t.Cleanup(func() { casSwapFn = prev })

	err := runWorkflowStateRefReconcile(io.Discard, stateRefReconcileOpts{})
	if err == nil || !strings.Contains(err.Error(), "p1") {
		t.Fatalf("write failure must propagate with plan id, got: %v", err)
	}
}

// TestReconcilePlanStatusLine_Branches covers every status phrase the text
// report can render for a single plan.
func TestReconcilePlanStatusLine_Branches(t *testing.T) {
	cases := []struct {
		name   string
		pr     stateRefReconcilePlanResult
		dryRun bool
		want   string
	}{
		{"skipped", stateRefReconcilePlanResult{Skipped: "no working-copy TASKS.yaml"}, false, "skipped (no working-copy TASKS.yaml)"},
		{"seeded", stateRefReconcilePlanResult{Seeded: []string{"t2", "t3"}, Changed: true}, false, "reconciled (2 task(s) seeded: t2, t3)"},
		{"would_seed", stateRefReconcilePlanResult{Seeded: []string{"t2"}}, true, "would reconcile (1 task(s) seeded: t2)"},
		{"metadata", stateRefReconcilePlanResult{Changed: true}, false, "reconciled (plan metadata refreshed)"},
		{"consistent", stateRefReconcilePlanResult{TasksInWC: 3}, false, "already-consistent (3 task(s))"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := reconcilePlanStatusLine(c.pr, c.dryRun); got != c.want {
				t.Fatalf("got %q want %q", got, c.want)
			}
		})
	}
}

var errAlwaysConflict = errors.New("simulated conflict")

// writeExtraPlan adds a second active plan directory with the given task ids.
func writeExtraPlan(t *testing.T, repo, planID string, taskIDs ...string) {
	t.Helper()
	dir := filepath.Join(plansBaseDir(repo), planID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, workflowPlanFileName), []byte(planYAMLActive(planID)), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, workflowTasksFileName), []byte(tasksYAMLMulti(planID, taskIDs...)), 0644); err != nil {
		t.Fatal(err)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func contains(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
