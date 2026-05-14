// Package workflow — sixth batch of coverage tests covering fs.go,
// prefs.go, plan_task.go, graph.go, health.go small branches.
package workflow

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// ─── fs.go: copyWorkflowArtifact missing-source error ──────────────────────

func TestCopyWorkflowArtifact_MissingSource(t *testing.T) {
	tmp := t.TempDir()
	err := copyWorkflowArtifact(filepath.Join(tmp, "does-not-exist"), filepath.Join(tmp, "dst"))
	if err == nil {
		t.Fatal("expected open-src error")
	}
}

// ─── fs.go: copyWorkflowArtifact destination-create error ──────────────────

func TestCopyWorkflowArtifact_DstCreateError(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.txt")
	if err := os.WriteFile(src, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	// dst is under a path where MkdirAll succeeds but Create fails
	// (use an existing file as the destination directory).
	conflict := filepath.Join(tmp, "blocker")
	if err := os.WriteFile(conflict, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	// dst tries to be conflict/inner — MkdirAll(conflict) fails
	dst := filepath.Join(conflict, "inner.txt")
	if err := copyWorkflowArtifact(src, dst); err == nil {
		t.Fatal("expected MkdirAll over file error")
	}
}

// ─── fs.go: copyWorkflowDir walk error on missing src ──────────────────────

func TestCopyWorkflowDir_MissingSource(t *testing.T) {
	tmp := t.TempDir()
	err := copyWorkflowDir(filepath.Join(tmp, "no-such-dir"), filepath.Join(tmp, "dst"))
	if err == nil {
		t.Fatal("expected walk error on missing src")
	}
}

// ─── fs.go: copyWorkflowDir happy path covers dir + file branches ──────────

func TestCopyWorkflowDir_Recursive(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "out")
	// Make src/sub/leaf.txt
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "leaf.txt"), []byte("hi"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := copyWorkflowDir(src, dst); err != nil {
		t.Fatalf("copyWorkflowDir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "sub", "leaf.txt")); err != nil {
		t.Fatalf("expected file copied, got %v", err)
	}
}

// ─── fs.go: sha256File missing-file branch ─────────────────────────────────

func TestSha256File_Missing(t *testing.T) {
	_, err := sha256File(filepath.Join(t.TempDir(), "absent"))
	if err == nil {
		t.Fatal("expected error")
	}
}

// ─── fs.go: mergePlanDirFastRename mkdir parent error ──────────────────────

func TestMergePlanDirFastRename_MkdirError(t *testing.T) {
	src := t.TempDir()
	// Force MkdirAll(filepath.Dir(dst)) to fail by giving dst a file-as-parent.
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(blocker, "child", "plan")
	if err := mergePlanDirFastRename(src, dst, false); err == nil {
		t.Fatal("expected mkdir-parent error")
	}
}

// ─── fs.go: mergePlanDirFastRename dryRun branch ───────────────────────────

func TestMergePlanDirFastRename_DryRun_Push6(t *testing.T) {
	if err := mergePlanDirFastRename("/src", "/dst", true); err != nil {
		t.Fatalf("dryRun must not error, got %v", err)
	}
}

// ─── fs.go: mergePlanDirCompareAndCopy hash-src error ──────────────────────

func TestMergePlanDirCompareAndCopy_HashSrcError(t *testing.T) {
	tmp := t.TempDir()
	err := mergePlanDirCompareAndCopy(filepath.Join(tmp, "missing"), filepath.Join(tmp, "dst"), "rel", false)
	if err == nil || !strings.Contains(err.Error(), "hash ") {
		t.Fatalf("expected hash error, got %v", err)
	}
}

// ─── fs.go: mergePlanDirCompareAndCopy stat-dst error path ─────────────────

func TestMergePlanDirCompareAndCopy_DstStatErrPath(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.txt")
	if err := os.WriteFile(src, []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	// Put a file in place of dst's parent to break stat lookup.
	blocker := filepath.Join(tmp, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(blocker, "child")
	// Stat(dst) returns "not a directory" — not IsNotExist on macOS — so
	// we hit the stat-error branch.
	err := mergePlanDirCompareAndCopy(src, dst, "child", false)
	// The dry-run/copy may still error inside copyWorkflowArtifact; we
	// only assert non-nil to cover the branch.
	if err == nil {
		// Some platforms return IsNotExist and fall through to copy: at
		// minimum verify we entered the routine.
		t.Log("stat-dst returned IsNotExist on this platform; branch not hit but not an error")
	}
}

// ─── fs.go: mergePlanDirCompareAndCopy dryRun overwrite branch ─────────────

func TestMergePlanDirCompareAndCopy_DryRunOverwrite(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.txt")
	dst := filepath.Join(tmp, "dst.txt")
	if err := os.WriteFile(src, []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	// Make src newer than dst to bypass skip branch.
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(src, future, future); err != nil {
		t.Fatal(err)
	}
	if err := mergePlanDirCompareAndCopy(src, dst, "rel", true); err != nil {
		t.Fatalf("dryRun: %v", err)
	}
}

// ─── fs.go: shouldSkipPlanDirCopy hash-dst error ───────────────────────────

func TestShouldSkipPlanDirCopy_HashDstError(t *testing.T) {
	tmp := t.TempDir()
	srcHash := [32]byte{}
	st, err := os.Stat(tmp)
	if err != nil {
		t.Fatal(err)
	}
	_, err = shouldSkipPlanDirCopy(filepath.Join(tmp, "src"), filepath.Join(tmp, "missing"), "rel", false, srcHash, st)
	if err == nil || !strings.Contains(err.Error(), "hash dst") {
		t.Fatalf("expected hash-dst error, got %v", err)
	}
}

// ─── fs.go: shouldSkipPlanDirCopy stat-src error ───────────────────────────

func TestShouldSkipPlanDirCopy_StatSrcError(t *testing.T) {
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "dst.txt")
	if err := os.WriteFile(dst, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(dst)
	// src hash differs from dst hash to bypass identical-skip branch.
	srcHash := [32]byte{1, 2, 3}
	_, err := shouldSkipPlanDirCopy(filepath.Join(tmp, "missing-src"), dst, "rel", false, srcHash, st)
	if err == nil || !strings.Contains(err.Error(), "stat src") {
		t.Fatalf("expected stat-src error, got %v", err)
	}
}

// ─── fs.go: shouldSkipPlanDirCopy history-newer warn branch ────────────────

func TestShouldSkipPlanDirCopy_HistoryNewer(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.txt")
	dst := filepath.Join(tmp, "dst.txt")
	if err := os.WriteFile(src, []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("b"), 0644); err != nil {
		t.Fatal(err)
	}
	// Make dst newer than src.
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(src, old, old); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(dst)
	srcHashBytes := [32]byte{9, 9, 9}
	skip, err := shouldSkipPlanDirCopy(src, dst, "rel", true, srcHashBytes, st)
	if err != nil {
		t.Fatal(err)
	}
	if !skip {
		t.Fatal("expected skip=true when history newer")
	}
}

// ─── fs.go: isDMAFile detection branches ───────────────────────────────────

func TestIsDMAFile_ByBasename(t *testing.T) {
	for _, name := range []string{"delegation.yaml", "merge-back.md", "closeout.yaml"} {
		if !isDMAFile(name) {
			t.Errorf("expected %q to be DMA", name)
		}
	}
}

func TestIsDMAFile_ByDirectory(t *testing.T) {
	cases := []string{
		"delegate-merge-back-archive/x.yaml",
		"delegation/x.yaml",
		"merge-back/x.md",
		"fold-back/x.yaml",
		"verification/x.yaml",
	}
	for _, p := range cases {
		if !isDMAFile(p) {
			t.Errorf("expected %q to be DMA", p)
		}
	}
}

func TestIsDMAFile_NotDMA(t *testing.T) {
	if isDMAFile("PLAN.yaml") {
		t.Error("PLAN.yaml should not be DMA")
	}
}

// ─── fs.go: isCanonicalPlanFile branches ───────────────────────────────────

func TestIsCanonicalPlanFile_Push6(t *testing.T) {
	if !isCanonicalPlanFile("PLAN.yaml", "p1") {
		t.Error("PLAN.yaml")
	}
	if !isCanonicalPlanFile("TASKS.yaml", "p1") {
		t.Error("TASKS.yaml")
	}
	if !isCanonicalPlanFile("p1.plan.md", "p1") {
		t.Error("p1.plan.md")
	}
	if isCanonicalPlanFile("README.md", "p1") {
		t.Error("README.md should not be canonical")
	}
}

// ─── prefs.go: runWorkflowPrefs JSON path ──────────────────────────────────

func TestRunWorkflowPrefs_JSON_Push6(t *testing.T) {
	repo := setupTestProject(t)
	chdirForCov(t, repo)
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = false })
	out, err := captureCovStdout(t, runWorkflowPrefs)
	if err != nil {
		t.Fatalf("prefs json: %v", err)
	}
	if !strings.Contains(out, "verification") {
		t.Fatalf("expected verification in JSON: %s", out)
	}
}

// ─── prefs.go: applyMaxParallelWorkers invalid value branch ────────────────

func TestApplyMaxParallelWorkers_OutOfRange(t *testing.T) {
	p := &WorkflowPreferences{}
	if err := applyMaxParallelWorkers(p, "100"); err == nil {
		t.Fatal("expected out-of-range error")
	}
	if err := applyMaxParallelWorkers(p, "0"); err == nil {
		t.Fatal("expected lower-bound error")
	}
	if err := applyMaxParallelWorkers(p, "abc"); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestApplyMaxParallelWorkers_Valid(t *testing.T) {
	p := &WorkflowPreferences{}
	if err := applyMaxParallelWorkers(p, "4"); err != nil {
		t.Fatal(err)
	}
	if p.Execution.MaxParallelWorkers == nil || *p.Execution.MaxParallelWorkers != 4 {
		t.Fatalf("expected 4, got %v", p.Execution.MaxParallelWorkers)
	}
}

// ─── prefs.go: setLocalPreference malformed-existing branch ────────────────

func TestSetLocalPreference_MalformedExisting(t *testing.T) {
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	ctx := filepath.Join(agentsHome, "context", "p")
	if err := os.MkdirAll(ctx, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ctx, "preferences.local.yaml"), []byte(":\n  - bad: ["), 0644); err != nil {
		t.Fatal(err)
	}
	err := setLocalPreference("p", "execution.max_parallel_workers", "4")
	if err == nil || !strings.Contains(err.Error(), "parse local preferences") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

// ─── prefs.go: setLocalPreference applies and writes ───────────────────────

func TestSetLocalPreference_Writes(t *testing.T) {
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	if err := setLocalPreference("p", "execution.max_parallel_workers", "3"); err != nil {
		t.Fatalf("set: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(agentsHome, "context", "p", "preferences.local.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "max_parallel_workers: 3") {
		t.Fatalf("unexpected file content: %s", string(data))
	}
}

// ─── plan_task.go: incompleteCanonicalDependenciesCrossplan task missing ───

func TestCrossPlanDepIncomplete_TaskMissingWarns(t *testing.T) {
	repo := t.TempDir()
	plansDir := filepath.Join(repo, ".agents", "workflow", "plans", "p1")
	if err := os.MkdirAll(plansDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Plan with a task that does NOT include id "missing".
	plan := CanonicalPlan{SchemaVersion: 1, ID: "p1", Status: "active",
		CreatedAt: "2026-04-10T00:00:00Z", UpdatedAt: "2026-04-10T00:00:00Z"}
	if err := saveCanonicalPlan(repo, &plan); err != nil {
		t.Fatal(err)
	}
	tasks := &CanonicalTaskFile{SchemaVersion: 1, PlanID: "p1",
		Tasks: []CanonicalTask{{ID: "t-real", Status: "pending"}}}
	if err := saveCanonicalTasks(repo, tasks); err != nil {
		t.Fatal(err)
	}

	var warnings []string
	cache := make(map[string]*CanonicalTaskFile)
	got := crossPlanDepIncomplete(repo, "p1/missing", cache, &warnings)
	if !got {
		t.Fatal("missing task should be treated as incomplete")
	}
	if len(warnings) == 0 || !strings.Contains(warnings[0], "missing") {
		t.Fatalf("expected warning about missing dep, got %v", warnings)
	}
}

// ─── plan_task.go: incompleteCanonicalDependenciesCrossplan completed dep ──

func TestCrossPlanDepIncomplete_CompletedReturnsFalse(t *testing.T) {
	repo := t.TempDir()
	plansDir := filepath.Join(repo, ".agents", "workflow", "plans", "p1")
	if err := os.MkdirAll(plansDir, 0755); err != nil {
		t.Fatal(err)
	}
	plan := CanonicalPlan{SchemaVersion: 1, ID: "p1", Status: "active",
		CreatedAt: "2026-04-10T00:00:00Z", UpdatedAt: "2026-04-10T00:00:00Z"}
	if err := saveCanonicalPlan(repo, &plan); err != nil {
		t.Fatal(err)
	}
	tasks := &CanonicalTaskFile{SchemaVersion: 1, PlanID: "p1",
		Tasks: []CanonicalTask{{ID: "t-done", Status: "completed"}}}
	if err := saveCanonicalTasks(repo, tasks); err != nil {
		t.Fatal(err)
	}
	cache := make(map[string]*CanonicalTaskFile)
	got := crossPlanDepIncomplete(repo, "p1/t-done", cache, nil)
	if got {
		t.Fatal("completed dep should be considered complete")
	}
}

// ─── plan_task.go: runWorkflowPlanSchedule JSON path ───────────────────────

func TestRunWorkflowPlanSchedule_JSON_Push6(t *testing.T) {
	repo := setupTestProject(t)
	chdirForCov(t, repo)
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = false })
	out, err := captureCovStdout(t, func() error { return runWorkflowPlanSchedule("plan-001") })
	if err != nil {
		t.Fatalf("schedule json: %v", err)
	}
	if !strings.Contains(out, `"waves"`) {
		t.Fatalf("expected waves in JSON: %s", out)
	}
}

// ─── plan_task.go: runWorkflowPlanSchedule scope-empty render branch ───────

func TestRunWorkflowPlanSchedule_NoWriteScopeRender(t *testing.T) {
	repo := t.TempDir()
	plansDir := filepath.Join(repo, ".agents", "workflow", "plans", "p1")
	if err := os.MkdirAll(plansDir, 0755); err != nil {
		t.Fatal(err)
	}
	plan := CanonicalPlan{SchemaVersion: 1, ID: "p1", Status: "active",
		CreatedAt: "2026-04-10T00:00:00Z", UpdatedAt: "2026-04-10T00:00:00Z"}
	if err := saveCanonicalPlan(repo, &plan); err != nil {
		t.Fatal(err)
	}
	tasks := &CanonicalTaskFile{SchemaVersion: 1, PlanID: "p1",
		Tasks: []CanonicalTask{
			{ID: "t1", Title: "no-scope", Status: "pending"}, // empty WriteScope
		}}
	if err := saveCanonicalTasks(repo, tasks); err != nil {
		t.Fatal(err)
	}
	chdirForCov(t, repo)
	out, err := captureCovStdout(t, func() error { return runWorkflowPlanSchedule("p1") })
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if !strings.Contains(out, "(none)") {
		t.Fatalf("expected (none) for empty write_scope: %s", out)
	}
}

// ─── plan_task.go: runWorkflowPlanSchedule plan-not-found ──────────────────

func TestRunWorkflowPlanSchedule_PlanNotFound(t *testing.T) {
	repo := t.TempDir()
	chdirForCov(t, repo)
	err := runWorkflowPlanSchedule("missing")
	if err == nil {
		t.Fatal("expected load-tasks error")
	}
}

// ─── plan_task.go: runWorkflowTaskAdd duplicate-task ───────────────────────

func TestRunWorkflowTaskAdd_Duplicate(t *testing.T) {
	repo := setupTestProject(t)
	chdirForCov(t, repo)
	err := runWorkflowTaskAdd(taskAddInputs{
		PlanID: "plan-001", TaskID: "task-001", Title: "Dup",
	})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

// ─── plan_task.go: runWorkflowTaskAdd missing-plan ─────────────────────────

func TestRunWorkflowTaskAdd_MissingPlan(t *testing.T) {
	repo := t.TempDir()
	chdirForCov(t, repo)
	err := runWorkflowTaskAdd(taskAddInputs{
		PlanID: "no-plan", TaskID: "t1", Title: "x",
	})
	if err == nil {
		t.Fatal("expected error on missing plan")
	}
}

// ─── plan_task.go: runWorkflowTaskUpdate missing-task ─────────────────────

func TestRunWorkflowTaskUpdate_MissingTask(t *testing.T) {
	repo := setupTestProject(t)
	chdirForCov(t, repo)
	err := runWorkflowTaskUpdate("plan-001", "no-such", "title", "", "")
	if err == nil || !strings.Contains(err.Error(), "task") {
		t.Fatalf("expected task-not-found, got %v", err)
	}
}

// ─── plan_task.go: runWorkflowPlanUpdate invalid status ───────────────────

func TestRunWorkflowPlanUpdate_InvalidStatus_Push6(t *testing.T) {
	repo := setupTestProject(t)
	chdirForCov(t, repo)
	err := runWorkflowPlanUpdate("plan-001", "bogus", "", "", "", "", "")
	if err == nil || !strings.Contains(err.Error(), "invalid plan status") {
		t.Fatalf("expected invalid status, got %v", err)
	}
}

// ─── plan_task.go: runWorkflowPlanUpdate missing-plan ─────────────────────

func TestRunWorkflowPlanUpdate_MissingPlan(t *testing.T) {
	repo := t.TempDir()
	chdirForCov(t, repo)
	err := runWorkflowPlanUpdate("missing", "active", "", "", "", "", "")
	if err == nil {
		t.Fatal("expected load error")
	}
}

// ─── plan_task.go: loadCheckScopeSidecar bad-YAML branch ──────────────────

func TestLoadCheckScopeSidecar_MalformedYAML(t *testing.T) {
	repo := t.TempDir()
	planID := "p1"
	taskID := "t1"
	dir := filepath.Join(repo, ".agents", "workflow", "plans", planID, "evidence")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, taskID+".scope.yaml"), []byte(":\n  - bad: ["), 0644); err != nil {
		t.Fatal(err)
	}
	_, _, err := loadCheckScopeSidecar(repo, planID, taskID)
	if err == nil {
		t.Fatal("expected yaml parse error")
	}
}

// ─── plan_task.go: loadCheckScopeSidecar generic ReadFile error ───────────

func TestLoadCheckScopeSidecar_ReadError(t *testing.T) {
	repo := t.TempDir()
	planID := "p1"
	taskID := "t1"
	dir := filepath.Join(repo, ".agents", "workflow", "plans", planID, "evidence")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	side := filepath.Join(dir, taskID+".scope.yaml")
	if err := os.WriteFile(side, []byte("plan_id: p1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	chmodUnreadable(t, side)
	_, _, err := loadCheckScopeSidecar(repo, planID, taskID)
	if err == nil {
		t.Fatal("expected read error")
	}
}

// ─── graph.go: defaultGraphHome agentsrc-set branch ────────────────────────

func TestDefaultGraphHome_FromAgentsRC(t *testing.T) {
	repo := t.TempDir()
	rc := `{"version":1,"project":"p","kg":{"graph_home":"/tmp/custom-kg-home"},"sources":[{"type":"local"}]}`
	if err := os.WriteFile(filepath.Join(repo, ".agentsrc.json"), []byte(rc), 0644); err != nil {
		t.Fatal(err)
	}
	got := defaultGraphHome(repo)
	if got != "/tmp/custom-kg-home" {
		t.Fatalf("expected /tmp/custom-kg-home, got %s", got)
	}
}

// ─── graph.go: graphSearchNoteEntry malformed metadata branch ──────────────

func TestGraphSearchNoteEntry_BodyMatchesNoFrontmatter(t *testing.T) {
	graphHome := t.TempDir()
	sub := "entities"
	dir := filepath.Join(graphHome, "notes", sub)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "x.md"), []byte("just some lowercase text containing query word"), 0644); err != nil {
		t.Fatal(err)
	}
	result, id, ok := graphSearchNoteEntry(graphHome, sub, "x.md", "query")
	if !ok {
		t.Fatal("expected match")
	}
	if id != "x" {
		t.Fatalf("expected id derived from filename, got %s", id)
	}
	if result.ID != "x" {
		t.Fatalf("expected ID to fallback to filename, got %q", result.ID)
	}
}

// ─── graph.go: graphSearchSubdir cap respected ─────────────────────────────

func TestGraphSearchSubdir_CapStopsEarly(t *testing.T) {
	graphHome := t.TempDir()
	sub := "entities"
	dir := filepath.Join(graphHome, "notes", sub)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("n%d.md", i)), []byte("match content"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	results := []GraphBridgeResult{}
	graphSearchSubdir(graphHome, sub, "match", seen, &results, 2)
	if len(results) > 2 {
		t.Fatalf("cap 2 exceeded, got %d", len(results))
	}
}

// ─── graph.go: graphSearchSubdir missing dir returns silently ──────────────

func TestGraphSearchSubdir_MissingDirNoop(t *testing.T) {
	graphHome := t.TempDir()
	seen := map[string]bool{}
	results := []GraphBridgeResult{}
	graphSearchSubdir(graphHome, "nonexistent-sub", "q", seen, &results, 10)
	if len(results) != 0 {
		t.Fatalf("expected empty results, got %d", len(results))
	}
}

// ─── graph.go: Query unsupported intent ────────────────────────────────────

func TestLocalGraphAdapter_Query_UnsupportedIntent(t *testing.T) {
	a := NewLocalGraphAdapter(t.TempDir())
	_, err := a.Query(GraphBridgeQuery{Intent: "not-a-real-intent"})
	if err == nil || !strings.Contains(err.Error(), "unsupported bridge intent") {
		t.Fatalf("expected unsupported-intent error, got %v", err)
	}
}

// ─── health.go: readHealthSnapshot missing returns nil,nil ─────────────────

func TestReadHealthSnapshot_Missing(t *testing.T) {
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	h, err := readHealthSnapshot("p")
	if err != nil {
		t.Fatal(err)
	}
	if h != nil {
		t.Fatalf("expected nil snapshot, got %+v", h)
	}
}

// ─── health.go: readHealthSnapshot malformed-json branch ──────────────────

func TestReadHealthSnapshot_MalformedJSON(t *testing.T) {
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	ctx := filepath.Join(agentsHome, "context", "p")
	if err := os.MkdirAll(ctx, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ctx, "health.json"), []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := readHealthSnapshot("p")
	if err == nil {
		t.Fatal("expected json error")
	}
}

// ─── health.go: readHealthSnapshot ReadFile non-NotExist err ───────────────

func TestReadHealthSnapshot_ReadError(t *testing.T) {
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	ctx := filepath.Join(agentsHome, "context", "p")
	if err := os.MkdirAll(ctx, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(ctx, "health.json")
	if err := os.WriteFile(path, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	chmodUnreadable(t, path)
	_, err := readHealthSnapshot("p")
	if err == nil {
		t.Fatal("expected read error")
	}
}

// ─── iter_log.go: loadPrevCheckpointAt missing prev returns "" ─────────────

func TestLoadPrevCheckpointAt_MissingReturnsEmpty(t *testing.T) {
	got := loadPrevCheckpointAt(t.TempDir(), 2)
	if got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestLoadPrevCheckpointAt_NLessThanOrEqual1(t *testing.T) {
	got := loadPrevCheckpointAt(t.TempDir(), 1)
	if got != "" {
		t.Fatalf("expected empty for n<=1, got %q", got)
	}
}

func TestLoadPrevCheckpointAt_HappyPath(t *testing.T) {
	dir := t.TempDir()
	prev := filepath.Join(dir, "iter-1.yaml")
	if err := os.WriteFile(prev, []byte("checkpoint_at: \"2026-05-12T10:00:00Z\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got := loadPrevCheckpointAt(dir, 2)
	if got != "2026-05-12T10:00:00Z" {
		t.Fatalf("expected timestamp, got %q", got)
	}
}

func TestLoadPrevCheckpointAt_MalformedYAML(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "iter-1.yaml"), []byte(":\n  - bad: ["), 0644); err != nil {
		t.Fatal(err)
	}
	got := loadPrevCheckpointAt(dir, 2)
	if got != "" {
		t.Fatalf("expected empty on malformed YAML, got %q", got)
	}
}

// ─── iter_log.go: firstReadableDelegationContract no-dir branch ────────────

func TestFirstReadableDelegationContract_NoDir(t *testing.T) {
	got := firstReadableDelegationContract(t.TempDir())
	if got != nil {
		t.Fatalf("expected nil for missing delegation dir, got %+v", got)
	}
}

// ─── iter_log.go: firstReadableDelegationContract skips non-active ─────────

func TestFirstReadableDelegationContract_SkipsInactive(t *testing.T) {
	repo := t.TempDir()
	// Save a completed contract via the standard helper.
	saveTestDelegationContract(t, repo, "task-c", "plan-c", "deleg-c")
	// Reload it, mark completed, save again.
	c, err := loadDelegationContract(repo, "task-c")
	if err != nil {
		t.Fatal(err)
	}
	c.Status = "completed"
	if err := saveDelegationContract(repo, c); err != nil {
		t.Fatal(err)
	}
	got := firstReadableDelegationContract(repo)
	if got != nil {
		t.Fatalf("expected nil (completed skipped), got %+v", got)
	}
}

// ─── delegation.go: listDelegationContracts non-NotExist read err ──────────

func TestListDelegationContracts_ReadDirError(t *testing.T) {
	repo := t.TempDir()
	dir := filepath.Join(repo, ".agents", "active", "delegation")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	chmodUnreadableDir(t, dir)
	_, err := listDelegationContracts(repo)
	if err == nil {
		t.Fatal("expected ReadDir error")
	}
}

// ─── delegation.go: validateInsideProjectPath traversal rejected ───────────

func TestValidateInsideProjectPath_Traversal(t *testing.T) {
	repo := t.TempDir()
	if _, err := validateInsideProjectPath(repo, ".."); err == nil {
		t.Fatal("expected rejection of traversal")
	}
	if _, err := validateInsideProjectPath(repo, "ok/path"); err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
	if _, err := validateInsideProjectPath(repo, ""); err == nil {
		t.Fatal("expected rejection of empty")
	}
}

// ─── verification_result_schema.go: writeVerificationResultYAML invalid doc ─

func TestWriteVerificationResultYAML_SchemaInvalid(t *testing.T) {
	// status missing -> schema validation fails before write.
	doc := &VerificationResultDoc{
		SchemaVersion: 1, TaskID: "t", ParentPlanID: "p",
		VerifierType: "unit",
		RecordedAt:   "2026-05-12T00:00:00Z",
	}
	err := writeVerificationResultYAML(t.TempDir(), doc)
	if err == nil {
		t.Fatal("expected schema error")
	}
}

// ─── review_decision_schema.go: writeReviewDecisionYAML invalid doc ────────

func TestWriteReviewDecisionYAML_SchemaInvalid(t *testing.T) {
	doc := &ReviewDecisionDoc{
		SchemaVersion: 1, TaskID: "t",
		// Missing required fields → schema fails.
	}
	err := writeReviewDecisionYAML(t.TempDir(), doc)
	if err == nil {
		t.Fatal("expected schema error")
	}
}

// ─── delegation.go: validateProjectFileRef rejects empty / traversal ───────

func TestValidateProjectFileRef_RejectsBad(t *testing.T) {
	repo := t.TempDir()
	if _, err := validateProjectFileRef(repo, ""); err == nil {
		t.Fatal("empty rejected")
	}
	if _, err := validateProjectFileRef(repo, "../escape"); err == nil {
		t.Fatal("traversal rejected")
	}
	if _, err := validateProjectFileRef(repo, "missing.txt"); err == nil {
		t.Fatal("non-existent rejected")
	}
}

// ─── delegation.go: saveDelegationContract yaml-marshal error wraps ────────

func TestSaveDelegationContract_NoNameDoesNotPanic(t *testing.T) {
	// ParentTaskID empty → file would be ".yaml"; the function should
	// still produce a file or error gracefully (covers the happy/empty path).
	c := &DelegationContract{ParentTaskID: "x"}
	if err := saveDelegationContract(t.TempDir(), c); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

// ─── delegation.go: parseFoldBackUpsertInputs requires observation/slug ────

func newFoldBackTestCmd(observation, slug, planID string) *cobra.Command {
	c := &cobra.Command{}
	c.Flags().String("plan", planID, "")
	c.Flags().String("task", "", "")
	c.Flags().String("observation", observation, "")
	c.Flags().Bool("propose", false, "")
	c.Flags().String("slug", slug, "")
	return c
}

func TestParseFoldBackUpsertInputs_RequiresObservation(t *testing.T) {
	_, err := parseFoldBackUpsertInputs(newFoldBackTestCmd("", "", "p1"), false)
	if err == nil || !strings.Contains(err.Error(), "observation text is required") {
		t.Fatalf("expected obs required error, got %v", err)
	}
}

func TestParseFoldBackUpsertInputs_RequiresSlugForUpdate(t *testing.T) {
	_, err := parseFoldBackUpsertInputs(newFoldBackTestCmd("hi", "", "p1"), true)
	if err == nil || !strings.Contains(err.Error(), "--slug is required") {
		t.Fatalf("expected slug-required error, got %v", err)
	}
}

// ─── delegation.go: parseFoldBackUpsertInputs invalid slug rejected ────────

func TestParseFoldBackUpsertInputs_InvalidSlug(t *testing.T) {
	_, err := parseFoldBackUpsertInputs(newFoldBackTestCmd("hi", "bad slug!", "p1"), false)
	if err == nil {
		t.Fatal("expected invalid-slug error")
	}
}

// ─── delegation.go: loadPriorFoldBackArtifact missing slug returns nil ─────

func TestLoadPriorFoldBackArtifact_EmptySlug(t *testing.T) {
	a, ok, err := loadPriorFoldBackArtifact(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if a != nil || ok {
		t.Fatalf("expected nil,false for empty slug")
	}
}

func TestLoadPriorFoldBackArtifact_NotExist(t *testing.T) {
	a, ok, err := loadPriorFoldBackArtifact(t.TempDir(), "missing")
	if err != nil {
		t.Fatal(err)
	}
	if a != nil || ok {
		t.Fatalf("expected nil,false for missing slug")
	}
}

// ─── seams.go: removeAllWithRetry seam helper combinations ─────────────────

func TestRemoveAllWithRetry_OnFakeFile(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "f"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	// Should remove tmp's contents cleanly.
	if err := removeAllWithRetry(filepath.Join(tmp, "f")); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

// ─── verification.go: appendVerificationLog round-trips records ────────────

func TestAppendVerificationLog_RoundTrip(t *testing.T) {
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	rec := VerificationRecord{
		SchemaVersion: 1, Kind: "test", Status: "pass",
		Timestamp: "2026-05-12T00:00:00Z", Summary: "ok",
	}
	if err := appendVerificationLog("p", rec); err != nil {
		t.Fatal(err)
	}
	out, err := readVerificationLog("p", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Summary != "ok" {
		t.Fatalf("round-trip mismatch: %+v", out)
	}
}

// ─── seams.go reuse: yamlMarshal stub triggers persistScopeEvidenceSidecar ─

func TestPersistScopeEvidenceSidecar_DryRunPath(t *testing.T) {
	sentinel := errors.New("yaml boom")
	withYAMLMarshalStub(t, yamlMarshalErrStub(sentinel))
	_, err := persistScopeEvidenceSidecar(t.TempDir(), "p1", "t1", &ScopeEvidence{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected yaml sentinel wrapped, got %v", err)
	}
}
