package workflow

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
)

// ───────────────────────── shared fixtures ──────────────────────────────────

// orphanFixtureRepo builds a project tree with:
//   - active plan "active-plan" (one in_progress task)
//   - archived plan "archived-plan" living under history/ (one completed task)
//     plus an existing delegate-merge-back-archive entry for "case1-task" so
//     the duplicate-detection branch fires
//
// Each test then drops the specific active/merge-back/<task>.md or
// active/delegation/<task>.yaml file it needs and asserts the sweep result.
func orphanFixtureRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()

	// Active plan with one in_progress task (case-4 unknown variant when
	// classified as "not completed in history").
	activePlanDir := filepath.Join(repo, ".agents", "workflow", "plans", "active-plan")
	if err := os.MkdirAll(activePlanDir, 0755); err != nil {
		t.Fatal(err)
	}
	writeYAML(t, filepath.Join(activePlanDir, "PLAN.yaml"), CanonicalPlan{
		SchemaVersion: 1, ID: "active-plan", Title: "Active", Status: "active",
		CreatedAt: "2026-04-10T00:00:00Z", UpdatedAt: "2026-04-10T00:00:00Z",
	})
	writeYAML(t, filepath.Join(activePlanDir, "TASKS.yaml"), CanonicalTaskFile{
		SchemaVersion: 1, PlanID: "active-plan",
		Tasks: []CanonicalTask{
			{ID: "active-in-flight", Title: "In flight", Status: "in_progress"},
		},
	})

	// Archived plan: history/<plan>/PLAN.yaml + TASKS.yaml + an existing DMA entry.
	histPlanDir := filepath.Join(repo, ".agents", "history", "archived-plan")
	if err := os.MkdirAll(histPlanDir, 0755); err != nil {
		t.Fatal(err)
	}
	writeYAML(t, filepath.Join(histPlanDir, "PLAN.yaml"), CanonicalPlan{
		SchemaVersion: 1, ID: "archived-plan", Title: "Done", Status: "archived",
		CreatedAt: "2026-04-10T00:00:00Z", UpdatedAt: "2026-04-10T00:00:00Z",
	})
	writeYAML(t, filepath.Join(histPlanDir, "TASKS.yaml"), CanonicalTaskFile{
		SchemaVersion: 1, PlanID: "archived-plan",
		Tasks: []CanonicalTask{
			{ID: "case1-task", Title: "Already archived", Status: "completed"},
			{ID: "case2-task", Title: "Lone merge-back", Status: "completed"},
			{ID: "case3-task", Title: "Lone delegation", Status: "completed"},
		},
	})

	// Pre-existing delegate-merge-back-archive entry for case1-task so the
	// duplicate-detection branch can fire.
	dmaExisting := filepath.Join(histPlanDir, "delegate-merge-back-archive", "2026-04-10", "case1-task")
	if err := os.MkdirAll(dmaExisting, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dmaExisting, "merge-back.md"), []byte("---\ntask_id: case1-task\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}

	return repo
}

func writeYAML(t *testing.T, path string, v any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	data, err := yaml.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}

func writeActiveMergeBackFile(t *testing.T, repo, taskID, planID string) string {
	t.Helper()
	dir := filepath.Join(repo, ".agents", "active", "merge-back")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	mb := MergeBackSummary{SchemaVersion: 1, TaskID: taskID, ParentPlanID: planID, Title: "x", Summary: "s", IntegrationNotes: "n", CreatedAt: "2026-04-12T00:00:00Z"}
	if err := saveMergeBack(repo, &mb); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, taskID+".md")
}

func writeActiveDelegationFile(t *testing.T, repo, taskID, planID string) string {
	t.Helper()
	c := &DelegationContract{
		SchemaVersion: 1, ID: "del-" + taskID, ParentPlanID: planID,
		ParentTaskID: taskID, Title: "x", WriteScope: []string{"foo/"},
		Status: "active", CreatedAt: "2026-04-12T00:00:00Z", UpdatedAt: "2026-04-12T00:00:00Z",
	}
	if err := saveDelegationContract(repo, c); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(repo, ".agents", "active", "delegation", taskID+".yaml")
}

func newArchiveOrphansTestCmd() *cobra.Command {
	c := &cobra.Command{}
	var buf bytes.Buffer
	c.SetOut(&buf)
	c.SetErr(&buf)
	return c
}

// ─────────────────── case 1: duplicate merge-back removed ───────────────────

func TestArchiveOrphans_Case1_DuplicateMergeBackRemoved(t *testing.T) {
	repo := orphanFixtureRepo(t)
	mbPath := writeActiveMergeBackFile(t, repo, "case1-task", "archived-plan")

	cmd := newArchiveOrphansTestCmd()
	if err := runWorkflowArchiveOrphans(cmd, repo, false); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if _, err := os.Stat(mbPath); !os.IsNotExist(err) {
		t.Fatalf("duplicate merge-back not removed: stat err=%v", err)
	}
}

func TestArchiveOrphans_Case1_DryRunKeepsFile(t *testing.T) {
	repo := orphanFixtureRepo(t)
	mbPath := writeActiveMergeBackFile(t, repo, "case1-task", "archived-plan")

	cmd := newArchiveOrphansTestCmd()
	if err := runWorkflowArchiveOrphans(cmd, repo, true); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if _, err := os.Stat(mbPath); err != nil {
		t.Fatalf("dry-run removed file: %v", err)
	}
}

// ──────────────── case 2: lone merge-back promoted to history ───────────────

func TestArchiveOrphans_Case2_MergeBackMovedToHistory(t *testing.T) {
	repo := orphanFixtureRepo(t)
	mbPath := writeActiveMergeBackFile(t, repo, "case2-task", "archived-plan")

	cmd := newArchiveOrphansTestCmd()
	if err := runWorkflowArchiveOrphans(cmd, repo, false); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if _, err := os.Stat(mbPath); !os.IsNotExist(err) {
		t.Fatalf("source not removed after move: %v", err)
	}
	// Check the destination exists somewhere under DMA/<date>/<task>/merge-back.md.
	matches, _ := filepath.Glob(filepath.Join(repo, ".agents", "history", "archived-plan", "delegate-merge-back-archive", "*", "case2-task", "merge-back.md"))
	if len(matches) != 1 {
		t.Fatalf("expected 1 archived merge-back, got %d: %v", len(matches), matches)
	}
}

func TestArchiveOrphans_Case2_DryRunReportsTargetButDoesNotMove(t *testing.T) {
	repo := orphanFixtureRepo(t)
	mbPath := writeActiveMergeBackFile(t, repo, "case2-task", "archived-plan")

	cmd := newArchiveOrphansTestCmd()
	if err := runWorkflowArchiveOrphans(cmd, repo, true); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if _, err := os.Stat(mbPath); err != nil {
		t.Fatalf("dry-run mutated source: %v", err)
	}
	matches, _ := filepath.Glob(filepath.Join(repo, ".agents", "history", "archived-plan", "delegate-merge-back-archive", "*", "case2-task", "merge-back.md"))
	if len(matches) != 0 {
		t.Fatalf("dry-run created destination: %v", matches)
	}
}

// ───────────── case 3: lone delegation for completed task moved ─────────────

func TestArchiveOrphans_Case3_DelegationMovedToHistory(t *testing.T) {
	repo := orphanFixtureRepo(t)
	delPath := writeActiveDelegationFile(t, repo, "case3-task", "archived-plan")

	cmd := newArchiveOrphansTestCmd()
	if err := runWorkflowArchiveOrphans(cmd, repo, false); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if _, err := os.Stat(delPath); !os.IsNotExist(err) {
		t.Fatalf("delegation contract not removed: %v", err)
	}
	matches, _ := filepath.Glob(filepath.Join(repo, ".agents", "history", "archived-plan", "delegate-merge-back-archive", "*", "case3-task", "delegation.yaml"))
	if len(matches) != 1 {
		t.Fatalf("expected delegation archived under DMA, got %d", len(matches))
	}
}

func TestArchiveOrphans_Case3_NotCompleted_LeavesAlone(t *testing.T) {
	repo := orphanFixtureRepo(t)
	// active-in-flight is in_progress under active-plan; sweep must not move it.
	delPath := writeActiveDelegationFile(t, repo, "active-in-flight", "active-plan")

	cmd := newArchiveOrphansTestCmd()
	if err := runWorkflowArchiveOrphans(cmd, repo, false); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if _, err := os.Stat(delPath); err != nil {
		t.Fatalf("active in-flight contract was disturbed: %v", err)
	}
}

// ─────────── case 4: unknown task id → fold-back artifact + leave ───────────

func TestArchiveOrphans_Case4_UnknownTaskWritesFoldBack(t *testing.T) {
	repo := orphanFixtureRepo(t)
	// Task id not present in any TASKS.yaml.
	delPath := writeActiveDelegationFile(t, repo, "ghost-task", "ghost-plan")

	cmd := newArchiveOrphansTestCmd()
	if err := runWorkflowArchiveOrphans(cmd, repo, false); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if _, err := os.Stat(delPath); err != nil {
		t.Fatalf("unknown-task delegation must be left in place, got err=%v", err)
	}
	entries, err := os.ReadDir(filepath.Join(repo, ".agents", "active", "fold-back"))
	if err != nil {
		t.Fatalf("fold-back dir missing: %v", err)
	}
	found := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "archive-orphan-ghost-task-") {
			found = true
		}
	}
	if !found {
		t.Fatalf("fold-back artifact for ghost-task not written: %v", entries)
	}
}

func TestArchiveOrphans_Case4_MergeBackMissingPlanID(t *testing.T) {
	repo := orphanFixtureRepo(t)
	// Write a merge-back file with empty parent_plan_id.
	dir := filepath.Join(repo, ".agents", "active", "merge-back")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	mb := MergeBackSummary{SchemaVersion: 1, TaskID: "noplan-task", Title: "x", Summary: "s", IntegrationNotes: "n", CreatedAt: "2026-04-12T00:00:00Z"}
	if err := saveMergeBack(repo, &mb); err != nil {
		t.Fatal(err)
	}

	cmd := newArchiveOrphansTestCmd()
	if err := runWorkflowArchiveOrphans(cmd, repo, false); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	// Should land in unknown_task and produce a fold-back artifact while
	// leaving the source intact.
	if _, err := os.Stat(filepath.Join(dir, "noplan-task.md")); err != nil {
		t.Fatalf("source merge-back removed despite unknown plan: %v", err)
	}
}

func TestArchiveOrphans_NoOrphans_Success(t *testing.T) {
	repo := orphanFixtureRepo(t)
	cmd := newArchiveOrphansTestCmd()
	if err := runWorkflowArchiveOrphans(cmd, repo, false); err != nil {
		t.Fatalf("sweep: %v", err)
	}
}

// ─────────────── render / output coverage ──────────────────────────────────

func TestArchiveOrphans_JSONOutput(t *testing.T) {
	repo := orphanFixtureRepo(t)
	_ = writeActiveMergeBackFile(t, repo, "case2-task", "archived-plan")

	prev := workflowTestJSON
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = prev })

	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	if err := runWorkflowArchiveOrphans(cmd, repo, false); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	var result archiveOrphansResult
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("json output not parseable: %v\nraw: %s", err, buf.String())
	}
	if len(result.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(result.Actions))
	}
	if result.Actions[0].Class != orphanClassMoveMergeBack || !result.Actions[0].Applied {
		t.Fatalf("unexpected action: %+v", result.Actions[0])
	}
}

func TestArchiveOrphans_HumanRenderIncludesAllFields(t *testing.T) {
	repo := orphanFixtureRepo(t)
	_ = writeActiveMergeBackFile(t, repo, "case2-task", "archived-plan")
	_ = writeActiveDelegationFile(t, repo, "ghost-task", "ghost-plan")

	prev := workflowTestJSON
	workflowTestJSON = false
	t.Cleanup(func() { workflowTestJSON = prev })

	out := captureStdoutToString(t, func() {
		cmd := &cobra.Command{}
		if err := runWorkflowArchiveOrphans(cmd, repo, false); err != nil {
			t.Fatalf("sweep: %v", err)
		}
	})
	for _, want := range []string{"Archive Orphan Sweep", "case2-task", "ghost-task", "src:", "dst:"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

func TestArchiveOrphans_HumanRenderDryRunHint(t *testing.T) {
	repo := orphanFixtureRepo(t)
	_ = writeActiveMergeBackFile(t, repo, "case2-task", "archived-plan")

	prev := workflowTestJSON
	workflowTestJSON = false
	t.Cleanup(func() { workflowTestJSON = prev })

	out := captureStdoutToString(t, func() {
		cmd := &cobra.Command{}
		if err := runWorkflowArchiveOrphans(cmd, repo, true); err != nil {
			t.Fatalf("sweep: %v", err)
		}
	})
	for _, want := range []string{"dry-run", "Re-run without"} {
		if !strings.Contains(out, want) {
			t.Fatalf("dry-run hint missing %q:\n%s", want, out)
		}
	}
}

func TestArchiveOrphans_HumanRenderNoOrphans(t *testing.T) {
	repo := orphanFixtureRepo(t)

	prev := workflowTestJSON
	workflowTestJSON = false
	t.Cleanup(func() { workflowTestJSON = prev })

	out := captureStdoutToString(t, func() {
		cmd := &cobra.Command{}
		if err := runWorkflowArchiveOrphans(cmd, repo, false); err != nil {
			t.Fatalf("sweep: %v", err)
		}
	})
	if !strings.Contains(out, "No archive orphans") {
		t.Fatalf("empty-result message missing:\n%s", out)
	}
}

// ───────────────── helper / classifier unit tests ──────────────────────────

func TestClassifyOrphan_BothFilesPresent_NotAnOrphan(t *testing.T) {
	repo := orphanFixtureRepo(t)
	cand := orphanCandidate{
		taskID:           "case1-task",
		mergeBackPath:    "/a/case1-task.md",
		delegationPath:   "/b/case1-task.yaml",
		mergeBackPlanID:  "archived-plan",
		delegationPlanID: "archived-plan",
	}
	idx, err := buildTaskPlanIndex(repo)
	if err != nil {
		t.Fatal(err)
	}
	class, notes := classifyOrphan(repo, cand, idx)
	if class != orphanClassUnknownTask || !strings.Contains(notes, "both delegation and merge-back") {
		t.Fatalf("expected both-files unknown branch, got %s / %q", class, notes)
	}
}

func TestClassifyOrphan_MoveMergeBack_RequiresHistoryDir(t *testing.T) {
	repo := orphanFixtureRepo(t)
	cand := orphanCandidate{
		taskID:          "ghost",
		mergeBackPath:   "/a/ghost.md",
		mergeBackPlanID: "no-such-plan",
	}
	idx, err := buildTaskPlanIndex(repo)
	if err != nil {
		t.Fatal(err)
	}
	class, notes := classifyOrphan(repo, cand, idx)
	if class != orphanClassUnknownTask {
		t.Fatalf("expected unknown for missing history dir, got %s", class)
	}
	if !strings.Contains(notes, "no history dir") {
		t.Fatalf("expected note about history dir, got %q", notes)
	}
}

func TestClassifyOrphan_Case3_NoHistoryDir(t *testing.T) {
	repo := t.TempDir()
	// Active plan only (no history dir) with completed task.
	dir := filepath.Join(repo, ".agents", "workflow", "plans", "p1")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	writeYAML(t, filepath.Join(dir, "PLAN.yaml"), CanonicalPlan{SchemaVersion: 1, ID: "p1", Status: "active"})
	writeYAML(t, filepath.Join(dir, "TASKS.yaml"), CanonicalTaskFile{
		SchemaVersion: 1, PlanID: "p1",
		Tasks: []CanonicalTask{{ID: "done-here", Status: "completed"}},
	})
	cand := orphanCandidate{taskID: "done-here", delegationPath: "/x/done-here.yaml", delegationPlanID: "p1"}
	idx, err := buildTaskPlanIndex(repo)
	if err != nil {
		t.Fatal(err)
	}
	class, _ := classifyOrphan(repo, cand, idx)
	// Task is "active" source (TASKS.yaml under workflow/plans/), so this
	// hits the not-history branch, not the no-history-dir branch.
	if class != orphanClassUnknownTask {
		t.Fatalf("expected unknown-task class, got %s", class)
	}
}

func TestSanitizeFoldBackID(t *testing.T) {
	cases := []struct{ in, want string }{
		{"abc-123", "abc-123"},
		{"abc/123", "abc-123"},
		{"a:b:c", "a-b-c"},
		{"---abc---", "abc"},
		{"////", "unknown"},
		{"", "unknown"},
	}
	for _, c := range cases {
		if got := sanitizeFoldBackID(c.in); got != c.want {
			t.Errorf("sanitizeFoldBackID(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestEffectivePlanID(t *testing.T) {
	if got := (orphanCandidate{mergeBackPlanID: "A", delegationPlanID: "B"}).effectivePlanID(); got != "A" {
		t.Errorf("merge-back should win, got %q", got)
	}
	if got := (orphanCandidate{delegationPlanID: "B"}).effectivePlanID(); got != "B" {
		t.Errorf("delegation fallback, got %q", got)
	}
	if got := (orphanCandidate{}).effectivePlanID(); got != "" {
		t.Errorf("empty expected, got %q", got)
	}
}

func TestBuildTaskPlanIndex_ActiveWinsOverHistory(t *testing.T) {
	repo := t.TempDir()
	// Same task id in both an active and a history plan.
	act := filepath.Join(repo, ".agents", "workflow", "plans", "active-p")
	if err := os.MkdirAll(act, 0755); err != nil {
		t.Fatal(err)
	}
	writeYAML(t, filepath.Join(act, "TASKS.yaml"), CanonicalTaskFile{
		SchemaVersion: 1, PlanID: "active-p",
		Tasks: []CanonicalTask{{ID: "dup", Status: "in_progress"}},
	})
	hist := filepath.Join(repo, ".agents", "history", "old-p")
	if err := os.MkdirAll(hist, 0755); err != nil {
		t.Fatal(err)
	}
	writeYAML(t, filepath.Join(hist, "TASKS.yaml"), CanonicalTaskFile{
		SchemaVersion: 1, PlanID: "old-p",
		Tasks: []CanonicalTask{{ID: "dup", Status: "completed"}},
	})

	idx, err := buildTaskPlanIndex(repo)
	if err != nil {
		t.Fatal(err)
	}
	info, ok := idx["dup"]
	if !ok {
		t.Fatalf("dup missing from index")
	}
	if info.Source != "active" || info.PlanID != "active-p" {
		t.Fatalf("expected active source winner, got %+v", info)
	}
}

func TestBuildTaskPlanIndex_MissingDirsAreFine(t *testing.T) {
	repo := t.TempDir()
	idx, err := buildTaskPlanIndex(repo)
	if err != nil {
		t.Fatalf("missing-dirs case errored: %v", err)
	}
	if len(idx) != 0 {
		t.Fatalf("expected empty index, got %d entries", len(idx))
	}
}

func TestCollectArchiveOrphanCandidates_MissingDirs(t *testing.T) {
	repo := t.TempDir()
	cands, err := collectArchiveOrphanCandidates(repo)
	if err != nil {
		t.Fatalf("missing-dirs errored: %v", err)
	}
	if len(cands) != 0 {
		t.Fatalf("expected zero candidates, got %d", len(cands))
	}
}

func TestCollectArchiveOrphanCandidates_DedupesAcrossDirs(t *testing.T) {
	repo := orphanFixtureRepo(t)
	_ = writeActiveMergeBackFile(t, repo, "shared", "archived-plan")
	_ = writeActiveDelegationFile(t, repo, "shared", "archived-plan")

	cands, err := collectArchiveOrphanCandidates(repo)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, c := range cands {
		if c.taskID == "shared" {
			count++
			if c.mergeBackPath == "" || c.delegationPath == "" {
				t.Fatalf("shared candidate missing path: %+v", c)
			}
		}
	}
	if count != 1 {
		t.Fatalf("expected 1 deduped shared candidate, got %d", count)
	}
}

func TestArchivedMergeBackExists_NoDMABase(t *testing.T) {
	repo := t.TempDir()
	if archivedMergeBackExists(repo, "p", "t") {
		t.Fatal("expected false when DMA base missing")
	}
}

func TestArchivedMergeBackExists_EmptyDate(t *testing.T) {
	repo := t.TempDir()
	dir := filepath.Join(repo, ".agents", "history", "p", "delegate-merge-back-archive", "2026-04-10", "other-task")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if archivedMergeBackExists(repo, "p", "missing-task") {
		t.Fatal("expected false when no merge-back.md under task dir")
	}
}

func TestPlanForOrphanAction_FallsBackToIndex(t *testing.T) {
	idx := taskPlanIndex{"t": {PlanID: "from-idx", Status: "completed", Source: "history"}}
	got := planForOrphanAction(orphanCandidate{taskID: "t"}, idx)
	if got != "from-idx" {
		t.Fatalf("expected idx fallback, got %q", got)
	}
}

func TestMovePath_MkdirFailureBubbles(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src.txt")
	if err := os.WriteFile(src, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	prev := osMkdirAll
	t.Cleanup(func() { osMkdirAll = prev })
	osMkdirAll = func(string, os.FileMode) error { return errMockMkdir }

	if err := movePath(src, filepath.Join(t.TempDir(), "nested", "dst.txt")); err == nil {
		t.Fatal("expected mkdir failure")
	}
}

func TestMovePath_CopyFailureBubbles(t *testing.T) {
	// Source does not exist → copyWorkflowArtifact returns an error.
	if err := movePath(filepath.Join(t.TempDir(), "missing.txt"), filepath.Join(t.TempDir(), "dst.txt")); err == nil {
		t.Fatal("expected copy failure for missing source")
	}
}

var errMockMkdir = &mockErr{"mkdir blocked"}

type mockErr struct{ msg string }

func (e *mockErr) Error() string { return e.msg }

// ────────────────────── cobra wiring + end-to-end ──────────────────────────

func TestArchiveOrphansCmd_Wired(t *testing.T) {
	root := newWorkflowArchiveOrphansCmd()
	if root.Use != "archive-orphans" {
		t.Fatalf("expected Use=archive-orphans, got %q", root.Use)
	}
	if root.Short == "" {
		t.Fatal("expected non-empty Short")
	}
}

func TestArchiveOrphansCmd_EndToEndViaCobra(t *testing.T) {
	repo := orphanFixtureRepo(t)
	mbPath := writeActiveMergeBackFile(t, repo, "case1-task", "archived-plan")

	if err := executeWorkflowCommand(t, repo, "archive-orphans"); err != nil {
		t.Fatalf("workflow archive-orphans: %v", err)
	}
	if _, err := os.Stat(mbPath); !os.IsNotExist(err) {
		t.Fatalf("end-to-end did not remove duplicate orphan: %v", err)
	}
}

// ──────────────── error-path branch coverage ───────────────────────────────

func TestRunWorkflowArchiveOrphans_CollectCandidatesError(t *testing.T) {
	repo := t.TempDir()
	// Make the merge-back dir an unreadable dir to force os.ReadDir failure.
	mbDir := filepath.Join(repo, ".agents", "active", "merge-back")
	if err := os.MkdirAll(mbDir, 0755); err != nil {
		t.Fatal(err)
	}
	chmodUnreadableDir(t, mbDir)

	cmd := newArchiveOrphansTestCmd()
	err := runWorkflowArchiveOrphans(cmd, repo, false)
	if err == nil {
		t.Fatal("expected collect-candidates error")
	}
	if !strings.Contains(err.Error(), "collect orphan candidates") {
		t.Fatalf("error not wrapped: %v", err)
	}
}

func TestRunWorkflowArchiveOrphans_IndexError(t *testing.T) {
	repo := t.TempDir()
	// Active orphans dir must be empty/readable but the plans dir unreadable
	// so the second step fails. Make .agents/workflow/plans unenumerable.
	plans := filepath.Join(repo, ".agents", "workflow", "plans")
	if err := os.MkdirAll(plans, 0755); err != nil {
		t.Fatal(err)
	}
	chmodUnreadableDir(t, plans)

	cmd := newArchiveOrphansTestCmd()
	err := runWorkflowArchiveOrphans(cmd, repo, false)
	if err == nil {
		t.Fatal("expected index error")
	}
	if !strings.Contains(err.Error(), "index tasks") {
		t.Fatalf("error not wrapped: %v", err)
	}
}

func TestResolveArchiveOrphan_RemoveError(t *testing.T) {
	repo := orphanFixtureRepo(t)
	mbPath := writeActiveMergeBackFile(t, repo, "case1-task", "archived-plan")
	// Remove the file out from under the sweep so os.Remove fails with
	// ErrNotExist — that is the IsNotExist branch (not the error branch).
	// To hit the actual error branch, replace the file with a directory.
	if err := os.Remove(mbPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(mbPath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mbPath, "blocker"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	cand := orphanCandidate{taskID: "case1-task", mergeBackPath: mbPath, mergeBackPlanID: "archived-plan"}
	idx, _ := buildTaskPlanIndex(repo)
	_, err := resolveArchiveOrphan(repo, cand, idx, "2026-04-12", "2026-04-12T00:00:00Z", 12345, false)
	if err == nil {
		t.Fatal("expected remove error for non-empty dir")
	}
}

func TestResolveArchiveOrphan_MoveMergeBackCopyError(t *testing.T) {
	repo := orphanFixtureRepo(t)
	cand := orphanCandidate{
		taskID:          "case2-task",
		mergeBackPath:   filepath.Join(repo, "does-not-exist.md"),
		mergeBackPlanID: "archived-plan",
	}
	idx, _ := buildTaskPlanIndex(repo)
	_, err := resolveArchiveOrphan(repo, cand, idx, "2026-04-12", "2026-04-12T00:00:00Z", 1, false)
	if err == nil {
		t.Fatal("expected copy error from missing source")
	}
}

func TestResolveArchiveOrphan_MoveDelegationCopyError(t *testing.T) {
	repo := orphanFixtureRepo(t)
	cand := orphanCandidate{
		taskID:           "case3-task",
		delegationPath:   filepath.Join(repo, "does-not-exist.yaml"),
		delegationPlanID: "archived-plan",
	}
	idx, _ := buildTaskPlanIndex(repo)
	_, err := resolveArchiveOrphan(repo, cand, idx, "2026-04-12", "2026-04-12T00:00:00Z", 1, false)
	if err == nil {
		t.Fatal("expected copy error from missing source")
	}
}

func TestRecordUnknownTaskFoldBack_WriteError(t *testing.T) {
	repo := t.TempDir()
	prev := osMkdirAll
	t.Cleanup(func() { osMkdirAll = prev })
	osMkdirAll = func(string, os.FileMode) error { return errMockMkdir }

	cand := orphanCandidate{taskID: "boom", delegationPath: "/x/boom.yaml"}
	action := archiveOrphanAction{TaskID: "boom", Class: orphanClassUnknownTask}
	_, err := recordUnknownTaskFoldBack(repo, cand, "", action, "2026-04-12T00:00:00Z", 1, false)
	if err == nil {
		t.Fatal("expected write-fold-back error")
	}
}

func TestRecordUnknownTaskFoldBack_DryRunNoOp(t *testing.T) {
	repo := t.TempDir()
	cand := orphanCandidate{taskID: "tquiet", mergeBackPath: "/x/tquiet.md"}
	action := archiveOrphanAction{TaskID: "tquiet", Class: orphanClassUnknownTask}
	got, err := recordUnknownTaskFoldBack(repo, cand, "", action, "2026-04-12T00:00:00Z", 1, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Applied {
		t.Fatal("dry-run must not apply")
	}
	if got.Source != "/x/tquiet.md" {
		t.Fatalf("dry-run still populates source, got %q", got.Source)
	}
}

func TestIndexTasksFromDir_MalformedTASKSYAML(t *testing.T) {
	repo := t.TempDir()
	plansDir := filepath.Join(repo, ".agents", "workflow", "plans", "bad")
	if err := os.MkdirAll(plansDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plansDir, "TASKS.yaml"), []byte("::: not yaml :::"), 0644); err != nil {
		t.Fatal(err)
	}
	idx, err := buildTaskPlanIndex(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx) != 0 {
		t.Fatalf("malformed YAML should contribute zero tasks, got %d", len(idx))
	}
}

func TestIndexTasksFromDir_NoTASKSFile(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".agents", "workflow", "plans", "empty"), 0755); err != nil {
		t.Fatal(err)
	}
	idx, err := buildTaskPlanIndex(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx) != 0 {
		t.Fatalf("plan without TASKS.yaml should be skipped, got %d", len(idx))
	}
}

func TestIndexTasksFromDir_EmptyTaskIDSkipped(t *testing.T) {
	repo := t.TempDir()
	dir := filepath.Join(repo, ".agents", "workflow", "plans", "p")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	writeYAML(t, filepath.Join(dir, "TASKS.yaml"), CanonicalTaskFile{
		SchemaVersion: 1, PlanID: "p",
		Tasks: []CanonicalTask{{ID: "", Status: "pending"}, {ID: "real", Status: "pending"}},
	})
	idx, err := buildTaskPlanIndex(repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := idx[""]; ok {
		t.Fatal("empty task id must be skipped")
	}
	if _, ok := idx["real"]; !ok {
		t.Fatal("real task id missing")
	}
}

func TestIndexTasksFromDir_SkipsFilesInBase(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".agents", "workflow", "plans"), 0755); err != nil {
		t.Fatal(err)
	}
	// A stray file (not a dir) inside the plans base — must be skipped.
	if err := os.WriteFile(filepath.Join(repo, ".agents", "workflow", "plans", "stray.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	idx, err := buildTaskPlanIndex(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx) != 0 {
		t.Fatalf("stray file should not contribute tasks, got %d", len(idx))
	}
}

func TestCollectOrphanFiles_SkipsSubdirsAndNonMatchingExts(t *testing.T) {
	repo := t.TempDir()
	mbDir := filepath.Join(repo, ".agents", "active", "merge-back")
	if err := os.MkdirAll(filepath.Join(mbDir, "subdir"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mbDir, "notes.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	delDir := filepath.Join(repo, ".agents", "active", "delegation")
	if err := os.MkdirAll(filepath.Join(delDir, "subdir"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(delDir, "stray.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	cands, err := collectArchiveOrphanCandidates(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 0 {
		t.Fatalf("expected zero candidates from subdirs/non-matching, got %d", len(cands))
	}
}

func TestCollectOrphanFiles_UnreadableMergeBackDir(t *testing.T) {
	repo := t.TempDir()
	mbDir := filepath.Join(repo, ".agents", "active", "merge-back")
	if err := os.MkdirAll(mbDir, 0755); err != nil {
		t.Fatal(err)
	}
	chmodUnreadableDir(t, mbDir)
	if _, err := collectArchiveOrphanCandidates(repo); err == nil {
		t.Fatal("expected ReadDir error")
	}
}

func TestCollectOrphanFiles_UnreadableDelegationDir(t *testing.T) {
	repo := t.TempDir()
	delDir := filepath.Join(repo, ".agents", "active", "delegation")
	if err := os.MkdirAll(delDir, 0755); err != nil {
		t.Fatal(err)
	}
	chmodUnreadableDir(t, delDir)
	if _, err := collectArchiveOrphanCandidates(repo); err == nil {
		t.Fatal("expected ReadDir error")
	}
}

func TestCollectMergeBackOrphanFiles_ParseErrorStillRegistersCandidate(t *testing.T) {
	repo := t.TempDir()
	mbDir := filepath.Join(repo, ".agents", "active", "merge-back")
	if err := os.MkdirAll(mbDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Write a malformed merge-back file (no frontmatter terminator).
	if err := os.WriteFile(filepath.Join(mbDir, "broken.md"), []byte("no frontmatter here"), 0644); err != nil {
		t.Fatal(err)
	}
	cands, err := collectArchiveOrphanCandidates(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 1 || cands[0].taskID != "broken" {
		t.Fatalf("expected one candidate for broken.md, got %+v", cands)
	}
	if cands[0].mergeBackPlanID != "" {
		t.Fatal("malformed file should leave plan id empty")
	}
}

func TestCollectDelegationOrphanFiles_ParseErrorStillRegistersCandidate(t *testing.T) {
	repo := t.TempDir()
	delDir := filepath.Join(repo, ".agents", "active", "delegation")
	if err := os.MkdirAll(delDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(delDir, "weird.yaml"), []byte("::: not yaml :::"), 0644); err != nil {
		t.Fatal(err)
	}
	cands, err := collectArchiveOrphanCandidates(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 1 || cands[0].taskID != "weird" {
		t.Fatalf("expected one candidate for weird.yaml, got %+v", cands)
	}
	if cands[0].delegationPlanID != "" {
		t.Fatal("malformed file should leave plan id empty")
	}
}

func TestArchivedMergeBackExists_SkipsFileEntries(t *testing.T) {
	repo := t.TempDir()
	dmaBase := filepath.Join(repo, ".agents", "history", "p", "delegate-merge-back-archive")
	if err := os.MkdirAll(dmaBase, 0755); err != nil {
		t.Fatal(err)
	}
	// Stray non-dir entry in DMA base.
	if err := os.WriteFile(filepath.Join(dmaBase, "stray.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if archivedMergeBackExists(repo, "p", "t") {
		t.Fatal("file entries must be skipped")
	}
}

func TestArchiveOrphans_CobraCommandReturnsProjectError(t *testing.T) {
	// Drive the currentWorkflowProject() error path by faulting osGetwd.
	prev := osGetwd
	t.Cleanup(func() { osGetwd = prev })
	osGetwd = func() (string, error) { return "", errMockMkdir }

	cmd := newWorkflowArchiveOrphansCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected currentWorkflowProject error to bubble")
	}
}

func TestArchiveOrphans_CobraDryRunFlagWired(t *testing.T) {
	// Drive the `deps.Flags.DryRun != nil` branch in the cobra wrapper by
	// installing a stub. Without an active workflow project the call still
	// proceeds because the cwd is a writable temp dir; we assert the dry-run
	// signal reaches runWorkflowArchiveOrphans via the rendered output mode.
	repo := orphanFixtureRepo(t)
	_ = writeActiveMergeBackFile(t, repo, "case2-task", "archived-plan")

	prevDryRun := deps.Flags.DryRun
	deps.Flags.DryRun = func() bool { return true }
	t.Cleanup(func() { deps.Flags.DryRun = prevDryRun })

	out := executeWorkflowCommandOutput(t, repo, "archive-orphans")
	// Marker "○" indicates !Applied (dry-run); "✓" would mean it ran.
	if !strings.Contains(out, "○ [move_merge_back]") {
		t.Fatalf("expected dry-run pending marker, got:\n%s", out)
	}
	// Source still on disk confirms dry-run was honored.
	if _, err := os.Stat(filepath.Join(repo, ".agents", "active", "merge-back", "case2-task.md")); err != nil {
		t.Fatalf("dry-run mutated source: %v", err)
	}
}

func TestArchiveOrphansCmd_HelpRendersWithoutProject(t *testing.T) {
	// `--help` should not require a workflow project.
	cmd := newWorkflowArchiveOrphansCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("--help: %v", err)
	}
	if !strings.Contains(buf.String(), "archive-orphans") {
		t.Fatalf("help text missing command name:\n%s", buf.String())
	}
}
