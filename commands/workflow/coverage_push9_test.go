// Package workflow — final push tests using seam injection to drive
// secondary err-returns (e.g. saveCanonicalPlan after saveCanonicalTasks)
// without writing complex fixtures.
package workflow

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ─── plan_task.go: runWorkflowPlanCreate writeFile error on tasks ─────────

func TestRunWorkflowPlanCreate_WriteTasksError(t *testing.T) {
	repo := t.TempDir()
	chdirForCov(t, repo)
	// First write (plan) ok, second (tasks) failing requires the second
	// invocation of yamlMarshal to error. Use a stub that counts.
	calls := 0
	withYAMLMarshalStub(t, func(v any) ([]byte, error) {
		calls++
		if calls == 2 {
			return nil, errors.New("yaml-2 boom")
		}
		// Fallback to a real marshal for the first call.
		return realYAMLMarshal(v)
	})
	err := runWorkflowPlanCreate("new-plan", "Title", "", "", "", "")
	if err == nil {
		t.Fatal("expected error from tasks write")
	}
}

// realYAMLMarshal allows the stub to do a real marshal for selected calls.
func realYAMLMarshal(v any) ([]byte, error) {
	// Bypass the seam by calling the actual implementation directly. The
	// withYAMLMarshalStub helper restores yamlMarshal on cleanup, but
	// inside the closure we cannot call the unstubbed version. So we
	// emit minimal YAML for the structs we expect (CanonicalPlan).
	// Simplest: return a fixed valid YAML so writeFile succeeds and the
	// test reaches the second call.
	return []byte("schema_version: 1\nid: x\n"), nil
}

// ─── plan_task.go: runWorkflowTaskAdd save-tasks err propagates ──────────

func TestRunWorkflowTaskAdd_SaveErr(t *testing.T) {
	repo := setupTestProject(t)
	chdirForCov(t, repo)
	sentinel := errors.New("yaml boom")
	withYAMLMarshalStub(t, yamlMarshalErrStub(sentinel))
	err := runWorkflowTaskAdd(taskAddInputs{
		PlanID: "plan-001", TaskID: "task-new", Title: "x",
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel, got %v", err)
	}
}

// ─── plan_task.go: runWorkflowTaskUpdate save-tasks err ──────────────────

func TestRunWorkflowTaskUpdate_SaveErr(t *testing.T) {
	repo := setupTestProject(t)
	chdirForCov(t, repo)
	sentinel := errors.New("yaml boom")
	withYAMLMarshalStub(t, yamlMarshalErrStub(sentinel))
	err := runWorkflowTaskUpdate("plan-001", "task-001", "newtitle", "", "")
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel, got %v", err)
	}
}

// ─── plan_task.go: runWorkflowPlanUpdate save-plan err ───────────────────

func TestRunWorkflowPlanUpdate_SaveErr(t *testing.T) {
	repo := setupTestProject(t)
	chdirForCov(t, repo)
	sentinel := errors.New("yaml boom")
	withYAMLMarshalStub(t, yamlMarshalErrStub(sentinel))
	err := runWorkflowPlanUpdate("plan-001", "active", "", "", "", "", "")
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel, got %v", err)
	}
}

// ─── plan_task.go: runWorkflowAdvance save-tasks err ─────────────────────

func TestRunWorkflowAdvance_SaveErr(t *testing.T) {
	repo := setupTestProject(t)
	chdirForCov(t, repo)
	sentinel := errors.New("yaml boom")
	withYAMLMarshalStub(t, yamlMarshalErrStub(sentinel))
	err := runWorkflowAdvance("plan-001", "task-001", "in_progress")
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel, got %v", err)
	}
}

// ─── plan_task.go: appendPlanToWorkflowGraph corrupt slices file ─────────

func TestAppendPlanToWorkflowGraph_CorruptSlices(t *testing.T) {
	repo := setupTestProject(t)
	// Write a corrupt SLICES.yaml so loadCanonicalSlices returns an err
	// that is NOT IsNotExist.
	slicesPath := filepath.Join(repo, ".agents", "workflow", "plans", "plan-001", "SLICES.yaml")
	if err := os.WriteFile(slicesPath, []byte(":\n  - bad: ["), 0644); err != nil {
		t.Fatal(err)
	}
	graph := &workflowPlanGraph{}
	err := appendPlanToWorkflowGraph(graph, repo, "plan-001")
	if err == nil || !strings.Contains(err.Error(), "load slices") {
		t.Fatalf("expected load-slices error, got %v", err)
	}
}

// ─── plan_task.go: appendPlanToWorkflowGraph corrupt tasks ───────────────

func TestAppendPlanToWorkflowGraph_CorruptTasks(t *testing.T) {
	repo := setupTestProject(t)
	tasksPath := filepath.Join(repo, ".agents", "workflow", "plans", "plan-001", "TASKS.yaml")
	if err := os.WriteFile(tasksPath, []byte(":\n  - bad: ["), 0644); err != nil {
		t.Fatal(err)
	}
	graph := &workflowPlanGraph{}
	err := appendPlanToWorkflowGraph(graph, repo, "plan-001")
	if err == nil || !strings.Contains(err.Error(), "load tasks") {
		t.Fatalf("expected load-tasks error, got %v", err)
	}
}

// ─── plan_task.go: appendPlanToWorkflowGraph corrupt plan ────────────────

func TestAppendPlanToWorkflowGraph_CorruptPlan(t *testing.T) {
	repo := setupTestProject(t)
	planPath := filepath.Join(repo, ".agents", "workflow", "plans", "plan-001", "PLAN.yaml")
	if err := os.WriteFile(planPath, []byte(":\n  - bad: ["), 0644); err != nil {
		t.Fatal(err)
	}
	graph := &workflowPlanGraph{}
	err := appendPlanToWorkflowGraph(graph, repo, "plan-001")
	if err == nil || !strings.Contains(err.Error(), "load plan") {
		t.Fatalf("expected load-plan error, got %v", err)
	}
}

// ─── plan_task.go: runWorkflowTasks plan-not-found ───────────────────────

func TestRunWorkflowTasks_PlanNotFound_Push9(t *testing.T) {
	repo := t.TempDir()
	chdirForCov(t, repo)
	if err := runWorkflowTasks("no-such"); err == nil {
		t.Fatal("expected error")
	}
}

// ─── plan_task.go: runWorkflowSlices plan-not-found ──────────────────────

func TestRunWorkflowSlices_PlanNotFound_Push9(t *testing.T) {
	repo := t.TempDir()
	chdirForCov(t, repo)
	if err := runWorkflowSlices("no-such"); err == nil {
		t.Fatal("expected error")
	}
}

// ─── plan_task.go: loadActiveDelegationTaskSet read error propagates ─────

func TestLoadActiveDelegationTaskSet_ReadError(t *testing.T) {
	repo := t.TempDir()
	dir := filepath.Join(repo, ".agents", "active", "delegation")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	chmodUnreadableDir(t, dir)
	_, err := loadActiveDelegationTaskSet(repo)
	if err == nil {
		t.Fatal("expected read error")
	}
}

// ─── delegation.go: writeFoldBackArtifact load -> happy ──────────────────

func TestWriteFoldBackArtifact_Happy(t *testing.T) {
	repo := t.TempDir()
	if err := writeFoldBackArtifact(repo, foldBackArtifact{
		ID: "good", Classification: "small", PlanID: "p1",
		CreatedAt: "2026-05-12T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
}

// ─── delegation.go: scanActiveDelegationContract with active contract ────

func TestScanActiveDelegationContract_Active(t *testing.T) {
	repo := t.TempDir()
	saveTestDelegationContract(t, repo, "task-act", "plan-act", "d-act")
	wave, tid := scanActiveDelegationContract(repo)
	if wave != "plan-act" || tid != "task-act" {
		t.Fatalf("expected plan-act/task-act, got %q/%q", wave, tid)
	}
}
