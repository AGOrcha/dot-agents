package workflow

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Each per-step error in runWorkflowStartTask wraps with "start-task:
// <step>: ..." so log triage maps to the chain position. Tests stub the
// startTask* function-var seams to trigger each branch.

func TestStartTaskErrorWrapsActivate(t *testing.T) {
	prior := startTaskPlanUpdate
	startTaskPlanUpdate = func(string, string, string, string, string, string, string) error {
		return errors.New("activate boom")
	}
	t.Cleanup(func() { startTaskPlanUpdate = prior })
	err := runWorkflowStartTask(&bytes.Buffer{}, startTaskOpts{planID: "p", taskID: "t"})
	if err == nil || !strings.Contains(err.Error(), "start-task: plan update --status active") {
		t.Errorf("want activate-wrap, got: %v", err)
	}
}

func TestStartTaskErrorWrapsFocus(t *testing.T) {
	// First call (activate) succeeds, second call (focus) errors.
	calls := 0
	prior := startTaskPlanUpdate
	startTaskPlanUpdate = func(string, string, string, string, string, string, string) error {
		calls++
		if calls == 2 {
			return errors.New("focus boom")
		}
		return nil
	}
	t.Cleanup(func() { startTaskPlanUpdate = prior })
	err := runWorkflowStartTask(&bytes.Buffer{}, startTaskOpts{planID: "p", taskID: "t"})
	if err == nil || !strings.Contains(err.Error(), "start-task: plan update --focus") {
		t.Errorf("want focus-wrap, got: %v", err)
	}
}

func TestStartTaskErrorWrapsDeriveScope(t *testing.T) {
	priorPU := startTaskPlanUpdate
	startTaskPlanUpdate = func(string, string, string, string, string, string, string) error { return nil }
	t.Cleanup(func() { startTaskPlanUpdate = priorPU })

	priorDS := startTaskDeriveScope
	startTaskDeriveScope = func(string, string, []string, []string) error {
		return errors.New("derive boom")
	}
	t.Cleanup(func() { startTaskDeriveScope = priorDS })

	err := runWorkflowStartTask(&bytes.Buffer{}, startTaskOpts{planID: "p", taskID: "t"})
	if err == nil || !strings.Contains(err.Error(), "start-task: plan derive-scope") {
		t.Errorf("want derive-scope-wrap, got: %v", err)
	}
}

// renderStartTaskSummary's branch table: derive=true + commit=true must
// land the corresponding "sidecar derived" + "committed" lines. The
// orchestration tests cover the (false, true) and (false, false)
// combinations; this pins the (true, true) row.
func TestRenderStartTaskSummaryDerivedAndCommitted(t *testing.T) {
	var buf bytes.Buffer
	renderStartTaskSummary(&buf, startTaskResult{
		PlanID: "p", TaskID: "t",
		ActivatedPlan: true, FocusedTask: true,
		DerivedScope: true, WorkflowCommit: true,
	})
	out := buf.String()
	for _, frag := range []string{"sidecar derived", "workflow state committed"} {
		if !strings.Contains(out, frag) {
			t.Errorf("missing %q in:\n%s", frag, out)
		}
	}
}

func TestStartTaskErrorWrapsCommit(t *testing.T) {
	priorPU := startTaskPlanUpdate
	startTaskPlanUpdate = func(string, string, string, string, string, string, string) error { return nil }
	t.Cleanup(func() { startTaskPlanUpdate = priorPU })

	priorCommit := iterationCloseCommit
	iterationCloseCommit = func(io.Writer) error { return errors.New("commit boom") }
	t.Cleanup(func() { iterationCloseCommit = priorCommit })

	// noDeriveScope=true so we skip the derive step that would also need stubbing.
	err := runWorkflowStartTask(&bytes.Buffer{}, startTaskOpts{
		planID: "p", taskID: "t", noDeriveScope: true,
	})
	if err == nil || !strings.Contains(err.Error(), "start-task: workflow commit") {
		t.Errorf("want commit-wrap, got: %v", err)
	}
}

// Happy-path start-task: flip status to active, set focus, derive scope
// (the derive-scope call writes a placeholder sidecar since the test
// project has no real source for the derivation), fire workflow commit.
// iterationCloseCommit is stubbed; the existing TestExecGitEndToEnd
// covers the real-git surface.
func TestStartTaskOrchestratesChain(t *testing.T) {
	repo, planID, taskID := closeTaskTestRepo(t)
	t.Setenv("AGENTS_HOME", t.TempDir())
	t.Chdir(repo)

	commitCalls := 0
	prior := iterationCloseCommit
	iterationCloseCommit = func(out io.Writer) error { commitCalls++; return nil }
	t.Cleanup(func() { iterationCloseCommit = prior })

	var buf bytes.Buffer
	err := runWorkflowStartTask(&buf, startTaskOpts{
		planID:        planID,
		taskID:        taskID,
		noDeriveScope: true, // derive-scope needs real code-graph fixtures; skip in unit test
	})
	if err != nil {
		t.Fatalf("runWorkflowStartTask: %v\n%s", err, buf.String())
	}
	if commitCalls != 1 {
		t.Errorf("commit calls = %d, want 1", commitCalls)
	}
	out := buf.String()
	for _, frag := range []string{"start-task " + planID + "/" + taskID, "plan activated", "workflow state committed"} {
		if !strings.Contains(out, frag) {
			t.Errorf("summary missing %q in:\n%s", frag, out)
		}
	}
}

// --no-commit skips the workflow commit step. --no-derive-scope skips
// the sidecar derivation. Both flags reflected in the rendered summary.
func TestStartTaskNoCommitNoDeriveScope(t *testing.T) {
	repo, planID, taskID := closeTaskTestRepo(t)
	t.Setenv("AGENTS_HOME", t.TempDir())
	t.Chdir(repo)

	commitCalls := 0
	prior := iterationCloseCommit
	iterationCloseCommit = func(out io.Writer) error { commitCalls++; return nil }
	t.Cleanup(func() { iterationCloseCommit = prior })

	var buf bytes.Buffer
	if err := runWorkflowStartTask(&buf, startTaskOpts{
		planID: planID, taskID: taskID, noDeriveScope: true, noCommit: true,
	}); err != nil {
		t.Fatalf("runWorkflowStartTask: %v", err)
	}
	if commitCalls != 0 {
		t.Errorf("commit fired despite --no-commit: calls=%d", commitCalls)
	}
	out := buf.String()
	for _, frag := range []string{"scope derivation skipped", "NOT committed"} {
		if !strings.Contains(out, frag) {
			t.Errorf("missing %q in summary:\n%s", frag, out)
		}
	}
}

// --json emits the structured startTaskResult with every step's outcome
// surfaced as a boolean field. Callers parse this directly.
func TestStartTaskJSONOutput(t *testing.T) {
	repo, planID, taskID := closeTaskTestRepo(t)
	t.Setenv("AGENTS_HOME", t.TempDir())
	t.Chdir(repo)

	prior := iterationCloseCommit
	iterationCloseCommit = func(out io.Writer) error { return nil }
	t.Cleanup(func() { iterationCloseCommit = prior })
	priorJSON := deps.Flags.JSON
	jsonOn := true
	deps.Flags.JSON = func() bool { return jsonOn }
	t.Cleanup(func() { deps.Flags.JSON = priorJSON })

	var buf bytes.Buffer
	if err := runWorkflowStartTask(&buf, startTaskOpts{
		planID: planID, taskID: taskID, noDeriveScope: true,
	}); err != nil {
		t.Fatalf("runWorkflowStartTask: %v\n%s", err, buf.String())
	}
	var got startTaskResult
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("decode JSON: %v\n%s", err, buf.String())
	}
	if got.PlanID != planID || got.TaskID != taskID {
		t.Errorf("plan/task wrong: %+v", got)
	}
	if !got.ActivatedPlan || !got.FocusedTask {
		t.Error("ActivatedPlan / FocusedTask should both be true")
	}
	if got.DerivedScope {
		t.Error("DerivedScope = true with --no-derive-scope, want false")
	}
	if !got.WorkflowCommit {
		t.Error("WorkflowCommit = false, want true (default no-commit=false)")
	}
}

// Drive the cobra subcommand through Execute so newWorkflowStartTaskCmd's
// RunE closure is covered. --task required-flag check + positional plan
// ID validation come along for free.
func TestStartTaskSubcommandExecute(t *testing.T) {
	repo, planID, taskID := closeTaskTestRepo(t)
	t.Setenv("AGENTS_HOME", t.TempDir())
	t.Chdir(repo)

	prior := iterationCloseCommit
	iterationCloseCommit = func(out io.Writer) error { return nil }
	t.Cleanup(func() { iterationCloseCommit = prior })

	cmd := newWorkflowStartTaskCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{planID, "--task", taskID, "--no-derive-scope", "--no-commit"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\n%s", err, buf.String())
	}
	if !strings.Contains(buf.String(), "start-task") {
		t.Errorf("summary not rendered: %s", buf.String())
	}
}

// startTaskTreeSnapshot walks root (skipping .git) and returns a sorted
// "path|size|mtime" fingerprint per entry, letting dry-run tests assert
// zero on-disk mutation with a single before/after string compare.
//
// Every entry is re-stat'd directly via os.Lstat rather than trusting
// fs.DirEntry.Info(): per the io/fs docs, Info() "may be from the time of
// the original directory read" — on Windows, os.ReadDir populates each
// entry's FileInfo from the FindNextFile data captured at the READDIR call,
// not a live query. A parent directory's own on-disk LastWriteTime can lag
// microseconds behind a child file write it should already reflect (NTFS
// directory-index commit is not synchronous with the child's MFT update),
// so a "before" walk taken moments after setup can observe a stale parent
// mtime that a later "after" walk (same real state, no new mutation) then
// observes correctly — a false-positive "mutation" with zero code involved.
// Forcing a live stat closes that window without weakening what is
// compared: same path|size|mtime triple, just sourced authoritatively.
func startTaskTreeSnapshot(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if rel == "." {
			return nil
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		info, infoErr := os.Lstat(path)
		if infoErr != nil {
			return infoErr
		}
		out = append(out, fmt.Sprintf("%s|%d|%d", rel, info.Size(), info.ModTime().UnixNano()))
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", root, err)
	}
	sort.Strings(out)
	return out
}

// --dry-run must not call any of the four mutating primitives, must not
// write a journal event, and must leave the project tree byte-for-byte
// identical — the same "prints the plan, makes no changes" contract as
// TestCommitDryRunMakesNoMutations.
func TestStartTaskDryRunMakesNoMutations(t *testing.T) {
	repo, planID, taskID := closeTaskTestRepo(t)
	t.Setenv("AGENTS_HOME", t.TempDir())
	t.Chdir(repo)

	puCalls := 0
	priorPU := startTaskPlanUpdate
	startTaskPlanUpdate = func(string, string, string, string, string, string, string) error { puCalls++; return nil }
	t.Cleanup(func() { startTaskPlanUpdate = priorPU })

	dsCalls := 0
	priorDS := startTaskDeriveScope
	startTaskDeriveScope = func(string, string, []string, []string) error { dsCalls++; return nil }
	t.Cleanup(func() { startTaskDeriveScope = priorDS })

	commitCalls := 0
	priorCommit := iterationCloseCommit
	iterationCloseCommit = func(io.Writer) error { commitCalls++; return nil }
	t.Cleanup(func() { iterationCloseCommit = priorCommit })

	before := startTaskTreeSnapshot(t, repo)

	var buf bytes.Buffer
	if err := runWorkflowStartTask(&buf, startTaskOpts{
		planID: planID, taskID: taskID, dryRun: true,
	}); err != nil {
		t.Fatalf("runWorkflowStartTask dry-run: %v\n%s", err, buf.String())
	}

	if puCalls != 0 || dsCalls != 0 || commitCalls != 0 {
		t.Errorf("dry-run invoked primitives: planUpdate=%d deriveScope=%d commit=%d", puCalls, dsCalls, commitCalls)
	}

	after := startTaskTreeSnapshot(t, repo)
	if strings.Join(before, "\n") != strings.Join(after, "\n") {
		t.Errorf("dry-run mutated the project tree:\nbefore=%v\nafter=%v", before, after)
	}

	out := buf.String()
	for _, frag := range []string{"[dry-run]", "would activate plan", "would focus task " + taskID, "would derive scope-evidence sidecar", "would commit workflow state"} {
		if !strings.Contains(out, frag) {
			t.Errorf("dry-run preview missing %q:\n%s", frag, out)
		}
	}
}

// Regression guard: with dryRun left false, all four sub-steps still fire
// exactly once each — the pre-existing chain behavior is unchanged by
// adding the dry-run gate.
func TestStartTaskRunsAllFourStepsWhenNotDryRun(t *testing.T) {
	puCalls := 0
	priorPU := startTaskPlanUpdate
	startTaskPlanUpdate = func(string, string, string, string, string, string, string) error { puCalls++; return nil }
	t.Cleanup(func() { startTaskPlanUpdate = priorPU })

	dsCalls := 0
	priorDS := startTaskDeriveScope
	startTaskDeriveScope = func(string, string, []string, []string) error { dsCalls++; return nil }
	t.Cleanup(func() { startTaskDeriveScope = priorDS })

	commitCalls := 0
	priorCommit := iterationCloseCommit
	iterationCloseCommit = func(io.Writer) error { commitCalls++; return nil }
	t.Cleanup(func() { iterationCloseCommit = priorCommit })

	var buf bytes.Buffer
	if err := runWorkflowStartTask(&buf, startTaskOpts{planID: "p", taskID: "t1"}); err != nil {
		t.Fatalf("runWorkflowStartTask: %v\n%s", err, buf.String())
	}
	if puCalls != 2 {
		t.Errorf("plan update calls = %d, want 2 (activate + focus)", puCalls)
	}
	if dsCalls != 1 {
		t.Errorf("derive-scope calls = %d, want 1", dsCalls)
	}
	if commitCalls != 1 {
		t.Errorf("commit calls = %d, want 1", commitCalls)
	}
	out := buf.String()
	if strings.Contains(out, "[dry-run]") {
		t.Errorf("real run should not carry the dry-run marker:\n%s", out)
	}
	for _, frag := range []string{"plan activated + focused", "scope-evidence sidecar derived", "workflow state committed"} {
		if !strings.Contains(out, frag) {
			t.Errorf("summary missing %q in:\n%s", frag, out)
		}
	}
}

// --dry-run combined with --no-derive-scope/--no-commit must report fewer
// planned steps: the preview omits the "would derive"/"would commit" lines
// for the steps the operator opted out of.
func TestStartTaskDryRunFewerStepsWithSkipFlags(t *testing.T) {
	var buf bytes.Buffer
	if err := runWorkflowStartTask(&buf, startTaskOpts{
		planID: "p", taskID: "t1", dryRun: true, noDeriveScope: true, noCommit: true,
	}); err != nil {
		t.Fatalf("runWorkflowStartTask dry-run: %v\n%s", err, buf.String())
	}
	out := buf.String()
	for _, frag := range []string{
		"[dry-run]",
		"would activate plan",
		"would focus task t1",
		"scope derivation skipped (--no-derive-scope)",
		"workflow state NOT committed (--no-commit)",
	} {
		if !strings.Contains(out, frag) {
			t.Errorf("missing %q in:\n%s", frag, out)
		}
	}
	for _, frag := range []string{"would derive scope-evidence sidecar", "would commit workflow state"} {
		if strings.Contains(out, frag) {
			t.Errorf("preview should not list the skipped step %q:\n%s", frag, out)
		}
	}
}

// --dry-run --json emits startTaskResult with dry_run:true and every step
// marked as it WOULD run by default (no skip flags set).
func TestStartTaskDryRunJSONOutput(t *testing.T) {
	priorJSON := deps.Flags.JSON
	jsonOn := true
	deps.Flags.JSON = func() bool { return jsonOn }
	t.Cleanup(func() { deps.Flags.JSON = priorJSON })

	var buf bytes.Buffer
	if err := runWorkflowStartTask(&buf, startTaskOpts{
		planID: "p", taskID: "t1", dryRun: true,
	}); err != nil {
		t.Fatalf("runWorkflowStartTask dry-run JSON: %v\n%s", err, buf.String())
	}
	var got startTaskResult
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("decode JSON: %v\n%s", err, buf.String())
	}
	if !got.DryRun {
		t.Error("DryRun = false, want true")
	}
	if !got.ActivatedPlan || !got.FocusedTask || !got.DerivedScope || !got.WorkflowCommit {
		t.Errorf("dry-run preview fields should default to true (nothing skipped): %+v", got)
	}
}

// The local --dry-run flag on the cobra subcommand short-circuits the
// chain: none of the primitive seams fire and the rendered output carries
// the [dry-run] marker.
func TestStartTaskSubcommandDryRunFlag(t *testing.T) {
	puCalls := 0
	priorPU := startTaskPlanUpdate
	startTaskPlanUpdate = func(string, string, string, string, string, string, string) error { puCalls++; return nil }
	t.Cleanup(func() { startTaskPlanUpdate = priorPU })

	commitCalls := 0
	priorCommit := iterationCloseCommit
	iterationCloseCommit = func(io.Writer) error { commitCalls++; return nil }
	t.Cleanup(func() { iterationCloseCommit = priorCommit })

	cmd := newWorkflowStartTaskCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"p", "--task", "t1", "--dry-run"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\n%s", err, buf.String())
	}
	if puCalls != 0 || commitCalls != 0 {
		t.Errorf("--dry-run invoked primitives: planUpdate=%d commit=%d", puCalls, commitCalls)
	}
	if !strings.Contains(buf.String(), "[dry-run]") {
		t.Errorf("dry-run marker missing: %s", buf.String())
	}
}

// The global -n/--dry-run flag (deps.Flags.DryRun) is OR-merged into the
// local --dry-run so `da -n workflow start-task ...` is honored without the
// subcommand flag, exactly like commit's global dry-run wiring.
func TestStartTaskHonorsGlobalDryRun(t *testing.T) {
	puCalls := 0
	priorPU := startTaskPlanUpdate
	startTaskPlanUpdate = func(string, string, string, string, string, string, string) error { puCalls++; return nil }
	t.Cleanup(func() { startTaskPlanUpdate = priorPU })

	saved := deps.Flags.DryRun
	deps.Flags.DryRun = func() bool { return true }
	t.Cleanup(func() { deps.Flags.DryRun = saved })

	cmd := newWorkflowStartTaskCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"p", "--task", "t1"}) // no local --dry-run; the global -n must carry it
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\n%s", err, buf.String())
	}
	if puCalls != 0 {
		t.Errorf("global dry-run invoked plan update: calls=%d", puCalls)
	}
	if !strings.Contains(buf.String(), "[dry-run]") {
		t.Errorf("global dry-run marker missing: %s", buf.String())
	}
}
