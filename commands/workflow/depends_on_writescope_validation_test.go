package workflow

import (
	"encoding/json"
	"strings"
	"testing"
)

// This file covers two paired validation gaps field-reported during rollout:
//
//  1. dangling-depends-on-silent: a depends_on ref naming a nonexistent task
//     used to pass task add/update with no diagnostic, and `workflow eligible`
//     left the task non-eligible with no explanation — indistinguishable from a
//     legitimately-blocked task. See validateDependsOnRefs (write-time) and
//     dependencyHoldReasons / HeldTask (eligible's "why not" output) in
//     plan_task.go.
//  2. empty-write-scope-silent: an implementation-type task with an empty
//     write_scope got no warning on the write path, though write scopes drive
//     delegation bounding/disjointness. See warnEmptyWriteScope in
//     plan_task.go.

// ── validateDependsOnRefs (write-time gate) ─────────────────────────────────

// TestValidateDependsOnRefs table-drives the write-time depends_on validator:
// dangling local refs are a hard error listing the plan's valid ids, valid
// local + valid cross-plan refs pass silently, a cross-plan ref whose plan
// isn't available locally degrades to a warning rather than blocking, and a
// cross-plan ref into an existing plan but an unknown task within it is also
// a hard error.
func TestValidateDependsOnRefs(t *testing.T) {
	proj := t.TempDir()
	writePlanFixture(t, proj, "other-plan", "active", []CanonicalTask{
		{ID: "ok-task", Status: "pending"},
	})

	localTasks := []CanonicalTask{
		{ID: "t1", Status: "pending"},
		{ID: "t2", Status: "completed"},
	}

	cases := []struct {
		name        string
		deps        []string
		wantErrSub  []string
		wantWarnLen int
		wantWarnSub string
	}{
		{
			name:       "dangling local ref errors listing valid ids",
			deps:       []string{"missing-task"},
			wantErrSub: []string{`unknown task "missing-task"`, "main-plan", "t1, t2"},
		},
		{
			name: "valid local ref and valid cross-plan ref pass with no warnings",
			deps: []string{"t1", "other-plan/ok-task"},
		},
		{
			name:        "cross-plan ref with an absent plan warns, does not block",
			deps:        []string{"ghost-plan/some-task"},
			wantWarnLen: 1,
			wantWarnSub: "ghost-plan",
		},
		{
			name:       "cross-plan ref into an existing plan but unknown task errors listing that plan's valid ids",
			deps:       []string{"other-plan/nonexistent"},
			wantErrSub: []string{`unknown task "nonexistent"`, "other-plan", "ok-task"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			warnings, err := validateDependsOnRefs(proj, "main-plan", localTasks, c.deps)
			if len(c.wantErrSub) > 0 {
				assertDependsOnRefsError(t, err, warnings, c.wantErrSub)
				return
			}
			assertDependsOnRefsWarnings(t, err, warnings, c.wantWarnLen, c.wantWarnSub)
		})
	}
}

// assertDependsOnRefsError fails t unless err is non-nil and its message
// contains every substring in wantSubs.
func assertDependsOnRefsError(t *testing.T, err error, warnings []string, wantSubs []string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error, got nil (warnings=%v)", warnings)
	}
	for _, sub := range wantSubs {
		if !strings.Contains(err.Error(), sub) {
			t.Errorf("error %q missing substring %q", err.Error(), sub)
		}
	}
}

// assertDependsOnRefsWarnings fails t unless err is nil, warnings has exactly
// wantLen entries, and (when wantSub is non-empty) at least one warning
// contains wantSub.
func assertDependsOnRefsWarnings(t *testing.T, err error, warnings []string, wantLen int, wantSub string) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(warnings) != wantLen {
		t.Fatalf("warnings = %v, want %d entries", warnings, wantLen)
	}
	if wantSub == "" {
		return
	}
	for _, w := range warnings {
		if strings.Contains(w, wantSub) {
			return
		}
	}
	t.Errorf("expected a warning containing %q, got %v", wantSub, warnings)
}

// ── runWorkflowTaskAdd / runWorkflowTaskUpdate depends_on integration ───────

// TestRunWorkflowTaskAdd_DanglingDependsOnRejected verifies `task add` refuses
// to write a task whose depends_on names a nonexistent local task, and that
// the error lists the plan's valid ids.
func TestRunWorkflowTaskAdd_DanglingDependsOnRejected(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	chdirRepo(t, repo)

	err := runWorkflowTaskAdd(taskAddInputs{
		PlanID:    "wave-2",
		TaskID:    "t4",
		Title:     "new task",
		DependsOn: "not-a-real-task",
	})
	if err == nil {
		t.Fatal("expected error for dangling depends_on ref, got nil")
	}
	if !strings.Contains(err.Error(), `unknown task "not-a-real-task"`) {
		t.Errorf("error should name the dangling ref, got: %v", err)
	}
	for _, id := range []string{"t1", "t2", "t3"} {
		if !strings.Contains(err.Error(), id) {
			t.Errorf("error should list the plan's valid task ids (missing %q): %v", id, err)
		}
	}

	tf, loadErr := loadCanonicalTasks(repo, "wave-2")
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if taskIndexByID(tf, "t4") != -1 {
		t.Error("task should not have been written after depends_on validation failed")
	}
}

// TestRunWorkflowTaskAdd_ValidLocalAndCrossPlanDependsOnPersists verifies a
// task add with a valid local ref and a valid cross-plan ref succeeds with no
// warnings and both refs land in TASKS.yaml.
func TestRunWorkflowTaskAdd_ValidLocalAndCrossPlanDependsOnPersists(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	writePlanFixture(t, repo, "other-plan", "active", []CanonicalTask{
		{ID: "ok-task", Status: "completed"},
	})
	chdirRepo(t, repo)

	var runErr error
	stdout := captureStdoutToString(t, func() {
		runErr = runWorkflowTaskAdd(taskAddInputs{
			PlanID:     "wave-2",
			TaskID:     "t4",
			Title:      "new task",
			DependsOn:  "t1,other-plan/ok-task",
			WriteScope: "commands/workflow/",
		})
	})
	if runErr != nil {
		t.Fatalf("runWorkflowTaskAdd: %v", runErr)
	}
	if strings.Contains(stdout, "!") {
		t.Errorf("valid local + cross-plan refs should not warn, got: %q", stdout)
	}

	tf, loadErr := loadCanonicalTasks(repo, "wave-2")
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	idx := taskIndexByID(tf, "t4")
	if idx == -1 {
		t.Fatal("t4 missing after add")
	}
	got := strings.Join(tf.Tasks[idx].DependsOn, ",")
	if got != "t1,other-plan/ok-task" {
		t.Errorf("depends_on = %q, want \"t1,other-plan/ok-task\"", got)
	}
}

// TestRunWorkflowTaskAdd_CrossPlanDependsOnAbsentPlanWarnsNotBlocks verifies a
// cross-plan depends_on ref whose plan doesn't exist locally yet warns rather
// than blocking the write — the referenced plan may arrive later.
func TestRunWorkflowTaskAdd_CrossPlanDependsOnAbsentPlanWarnsNotBlocks(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	chdirRepo(t, repo)

	var runErr error
	stdout := captureStdoutToString(t, func() {
		runErr = runWorkflowTaskAdd(taskAddInputs{
			PlanID:    "wave-2",
			TaskID:    "t4",
			Title:     "new task",
			DependsOn: "ghost-plan/some-task",
		})
	})
	if runErr != nil {
		t.Fatalf("expected cross-plan dep on an absent plan to warn, not block: %v", runErr)
	}
	if !strings.Contains(stdout, "ghost-plan") {
		t.Errorf("expected a warning mentioning the absent plan, got: %q", stdout)
	}

	tf, loadErr := loadCanonicalTasks(repo, "wave-2")
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if taskIndexByID(tf, "t4") == -1 {
		t.Error("task should have been written despite the cross-plan warning")
	}
}

// TestRunWorkflowTaskUpdate_DanglingDependsOnRejected mirrors the add-side
// dangling-ref test for `task update`: the existing depends_on must be left
// untouched when validation rejects the new value.
func TestRunWorkflowTaskUpdate_DanglingDependsOnRejected(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	chdirRepo(t, repo)

	err := runWorkflowTaskUpdate(taskUpdateInputs{
		PlanID:    "wave-2",
		TaskID:    "t2",
		DependsOn: "not-a-real-task",
	})
	if err == nil {
		t.Fatal("expected error for dangling depends_on ref, got nil")
	}
	if !strings.Contains(err.Error(), `unknown task "not-a-real-task"`) {
		t.Errorf("error should name the dangling ref, got: %v", err)
	}

	tf, loadErr := loadCanonicalTasks(repo, "wave-2")
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	idx := taskIndexByID(tf, "t2")
	if idx == -1 {
		t.Fatal("t2 missing")
	}
	if len(tf.Tasks[idx].DependsOn) != 1 || tf.Tasks[idx].DependsOn[0] != "t1" {
		t.Errorf("depends_on should be unchanged after a rejected update, got %v", tf.Tasks[idx].DependsOn)
	}
}

// TestRunWorkflowTaskUpdate_ValidDependsOnPersists verifies a valid depends_on
// update (no "/" — local ref that exists) is applied normally.
func TestRunWorkflowTaskUpdate_ValidDependsOnPersists(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	chdirRepo(t, repo)

	if err := runWorkflowTaskUpdate(taskUpdateInputs{
		PlanID:    "wave-2",
		TaskID:    "t2",
		DependsOn: "t3",
	}); err != nil {
		t.Fatalf("runWorkflowTaskUpdate: %v", err)
	}

	tf, loadErr := loadCanonicalTasks(repo, "wave-2")
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	idx := taskIndexByID(tf, "t2")
	if idx == -1 || len(tf.Tasks[idx].DependsOn) != 1 || tf.Tasks[idx].DependsOn[0] != "t3" {
		t.Errorf("depends_on should update to [t3], got %v", tf.Tasks[idx].DependsOn)
	}
}

// ── dependencyHoldReasons / eligible "why not" output ───────────────────────

// TestDependencyHoldReasons table-drives the classifier eligible's held_tasks
// output is built from: a known task with an unsatisfying status is an
// "unmet dependency" naming the id and its status; an unknown id (local or
// cross-plan) is a "dangling reference" naming the missing id; a satisfied
// dep produces no reason at all.
func TestDependencyHoldReasons(t *testing.T) {
	proj := t.TempDir()
	writePlanFixture(t, proj, "other-plan", "active", []CanonicalTask{
		{ID: "x", Status: "pending"},
	})
	localTasks := []CanonicalTask{
		{ID: "t1", Status: "in_progress"},
		{ID: "t2", Status: "completed"},
	}

	cases := []struct {
		name       string
		deps       []string
		wantRef    string
		wantReason string // substring; empty means "no hold reasons at all"
	}{
		{name: "unmet dependency names the id and status", deps: []string{"t1"}, wantRef: "t1", wantReason: "unmet dependency"},
		{name: "dangling local ref names the missing id", deps: []string{"ghost"}, wantRef: "ghost", wantReason: "dangling reference"},
		{name: "satisfied dependency produces no reason", deps: []string{"t2"}},
		{name: "cross-plan unmet dependency", deps: []string{"other-plan/x"}, wantRef: "other-plan/x", wantReason: "unmet dependency"},
		{name: "cross-plan dangling plan", deps: []string{"ghost-plan/x"}, wantRef: "ghost-plan/x", wantReason: "dangling reference"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reasons := dependencyHoldReasons(proj, localTasks, c.deps)
			assertHeldReasons(t, reasons, c.wantRef, c.wantReason)
		})
	}
}

// assertHeldReasons fails t unless reasons matches the expected shape: zero
// entries when wantReason is empty, otherwise exactly one entry whose Ref
// equals wantRef and whose Reason contains wantReason.
func assertHeldReasons(t *testing.T, reasons []HeldReason, wantRef, wantReason string) {
	t.Helper()
	if wantReason == "" {
		if len(reasons) != 0 {
			t.Fatalf("expected no hold reasons, got %v", reasons)
		}
		return
	}
	if len(reasons) != 1 {
		t.Fatalf("expected exactly one hold reason, got %v", reasons)
	}
	if reasons[0].Ref != wantRef {
		t.Errorf("ref = %q, want %q", reasons[0].Ref, wantRef)
	}
	if !strings.Contains(reasons[0].Reason, wantReason) {
		t.Errorf("reason %q missing %q", reasons[0].Reason, wantReason)
	}
}

// TestRunWorkflowEligible_JSON_HeldTasksCarryUnmetDependencyReason verifies
// the eligible JSON output's held_tasks field carries an unmet-dependency
// reason (naming the blocking task and its status) for a task blocked on an
// in-progress dependency, and that the same task is correctly absent from
// eligible_tasks.
func TestRunWorkflowEligible_JSON_HeldTasksCarryUnmetDependencyReason(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo) // t1 in_progress; t2 pending depends_on t1 (unmet); t3 completed
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	chdirForCov(t, repo)
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = false })

	out, err := captureCovStdout(t, func() error { return runWorkflowEligible("", 0) })
	if err != nil {
		t.Fatalf("runWorkflowEligible: %v", err)
	}

	var decoded eligibleOutput
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}

	var held *HeldTask
	for i := range decoded.HeldTasks {
		if decoded.HeldTasks[i].TaskID == "t2" {
			held = &decoded.HeldTasks[i]
		}
	}
	if held == nil {
		t.Fatalf("expected t2 in held_tasks, got %+v", decoded.HeldTasks)
	}
	if len(held.HeldBy) != 1 {
		t.Fatalf("held_by = %+v, want exactly one reason", held.HeldBy)
	}
	if held.HeldBy[0].Ref != "t1" {
		t.Errorf("held_by[0].ref = %q, want %q", held.HeldBy[0].Ref, "t1")
	}
	if !strings.Contains(held.HeldBy[0].Reason, "unmet dependency") || !strings.Contains(held.HeldBy[0].Reason, "in_progress") {
		t.Errorf("held_by[0].reason = %q, want it to name unmet dependency + status=in_progress", held.HeldBy[0].Reason)
	}

	for _, at := range decoded.EligibleTasks {
		if at.TaskID == "t2" {
			t.Error("t2 should not appear in eligible_tasks while its dependency is unmet")
		}
	}
}

// TestRunWorkflowEligible_JSON_HeldTasksCarryDanglingRefReason verifies a task
// whose depends_on names a task id that doesn't exist anywhere in its plan
// gets a dangling-reference reason naming the missing id — this is the exact
// silent-gap scenario the dangling-depends-on-silent fold-back reported (the
// task existed in a legacy TASKS.yaml written before write-time validation).
func TestRunWorkflowEligible_JSON_HeldTasksCarryDanglingRefReason(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	writePlanFixture(t, repo, "dangling-plan", "active", []CanonicalTask{
		{ID: "orphan", Title: "Orphan", Status: "pending", DependsOn: []string{"ghost-task"}},
	})
	chdirForCov(t, repo)
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = false })

	out, err := captureCovStdout(t, func() error { return runWorkflowEligible("", 0) })
	if err != nil {
		t.Fatalf("runWorkflowEligible: %v", err)
	}

	var decoded eligibleOutput
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}

	var held *HeldTask
	for i := range decoded.HeldTasks {
		if decoded.HeldTasks[i].TaskID == "orphan" {
			held = &decoded.HeldTasks[i]
		}
	}
	if held == nil {
		t.Fatalf("expected orphan in held_tasks, got %+v", decoded.HeldTasks)
	}
	if len(held.HeldBy) != 1 {
		t.Fatalf("held_by = %+v, want exactly one reason", held.HeldBy)
	}
	if held.HeldBy[0].Ref != "ghost-task" {
		t.Errorf("held_by[0].ref = %q, want %q", held.HeldBy[0].Ref, "ghost-task")
	}
	if !strings.Contains(held.HeldBy[0].Reason, "dangling reference") {
		t.Errorf("held_by[0].reason = %q, want it to name a dangling reference", held.HeldBy[0].Reason)
	}
}

// TestRunWorkflowEligible_JSON_ReadyTaskEligibleFieldsUnchanged proves the
// held_tasks addition is purely additive: a ready task's existing
// eligible_tasks fields are untouched, and a task with no depends_on never
// shows up in held_tasks.
func TestRunWorkflowEligible_JSON_ReadyTaskEligibleFieldsUnchanged(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	chdirForCov(t, repo)
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = false })

	out, err := captureCovStdout(t, func() error { return runWorkflowEligible("", 0) })
	if err != nil {
		t.Fatalf("runWorkflowEligible: %v", err)
	}

	var decoded eligibleOutput
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}

	var t1 *AnnotatedTask
	for i := range decoded.EligibleTasks {
		if decoded.EligibleTasks[i].TaskID == "t1" {
			t1 = &decoded.EligibleTasks[i]
		}
	}
	if t1 == nil {
		t.Fatalf("expected t1 eligible, got %+v", decoded.EligibleTasks)
	}
	if t1.PlanID != "wave-2" || t1.Status != "in_progress" || !t1.WriteScopeDeclared {
		t.Errorf("t1's existing eligible fields changed unexpectedly: %+v", t1)
	}
	for _, h := range decoded.HeldTasks {
		if h.TaskID == "t1" {
			t.Error("t1 has no depends_on and should not appear in held_tasks")
		}
	}
}

// ── renderEligibleOutput held-tasks text suffix ─────────────────────────────

// TestRenderEligibleOutput_HeldTasksSuffix verifies the text renderer appends
// a "Held Tasks" section naming each ref and its reason, without disturbing
// existing output when there ARE held tasks.
func TestRenderEligibleOutput_HeldTasksSuffix(t *testing.T) {
	out := eligibleOutput{
		EligibleTasks: []AnnotatedTask{},
		MaxBatch:      []string{},
		ConflictGraph: map[string][]string{},
		DraftPlans:    []string{},
		HeldTasks: []HeldTask{
			{PlanID: "p", TaskID: "t2", TaskTitle: "T2", Status: "pending", HeldBy: []HeldReason{
				{Ref: "t1", Reason: `unmet dependency: task "t1" status=in_progress`},
			}},
		},
	}
	stdout := captureStdoutToString(t, func() { renderEligibleOutput(out, 1, 0) })
	if !strings.Contains(stdout, "Held Tasks") {
		t.Errorf("expected a Held Tasks section, got: %q", stdout)
	}
	if !strings.Contains(stdout, "held by t1: unmet dependency") {
		t.Errorf("expected the held reason rendered, got: %q", stdout)
	}
}

// TestRenderEligibleOutput_NoHeldTasksNoSuffix verifies the renderer is a
// no-op for the held-tasks section when there are none, keeping existing
// output byte-identical to before this field existed.
func TestRenderEligibleOutput_NoHeldTasksNoSuffix(t *testing.T) {
	out := eligibleOutput{
		EligibleTasks: []AnnotatedTask{},
		MaxBatch:      []string{},
		ConflictGraph: map[string][]string{},
		DraftPlans:    []string{},
		HeldTasks:     []HeldTask{},
	}
	stdout := captureStdoutToString(t, func() { renderEligibleOutput(out, 1, 0) })
	if strings.Contains(stdout, "Held Tasks") {
		t.Errorf("did not expect a Held Tasks section when there are none, got: %q", stdout)
	}
}

// ── warnEmptyWriteScope (Gap 2) ──────────────────────────────────────────────

// TestWarnEmptyWriteScope table-drives the empty-write_scope warning helper
// directly: an empty write_scope warns unless deriveScopeMode classifies the
// task as "research" (the existing app_type/notes/write_scope heuristic
// already used by `plan derive-scope`); a populated write_scope is always
// silent regardless of mode.
func TestWarnEmptyWriteScope(t *testing.T) {
	cases := []struct {
		name     string
		task     CanonicalTask
		wantWarn bool
	}{
		{
			name:     "empty write_scope with no other signal warns (defaults to code mode)",
			task:     CanonicalTask{ID: "a"},
			wantWarn: true,
		},
		{
			name:     "populated write_scope is silent",
			task:     CanonicalTask{ID: "b", WriteScope: []string{"commands/workflow/"}},
			wantWarn: false,
		},
		{
			name:     "research-mode via notes marker is exempted",
			task:     CanonicalTask{ID: "c", Notes: "research task, no go code"},
			wantWarn: false,
		},
		{
			name:     "app_type set but write_scope empty still warns (app_type implies code mode)",
			task:     CanonicalTask{ID: "d", AppType: "go-cli"},
			wantWarn: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			task := c.task
			out := captureStdoutToString(t, func() { warnEmptyWriteScope(&task) })
			gotWarn := strings.Contains(out, "no write_scope declared")
			if gotWarn != c.wantWarn {
				t.Errorf("warnEmptyWriteScope warned=%v, want %v (out=%q)", gotWarn, c.wantWarn, out)
			}
		})
	}
}

// TestRunWorkflowTaskAdd_EmptyWriteScopeWarns verifies `task add` warns (but
// still succeeds) when no --write-scope is given and nothing marks the task
// as research-mode.
func TestRunWorkflowTaskAdd_EmptyWriteScopeWarns(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	chdirRepo(t, repo)

	var runErr error
	stdout := captureStdoutToString(t, func() {
		runErr = runWorkflowTaskAdd(taskAddInputs{
			PlanID: "wave-2",
			TaskID: "t4",
			Title:  "implementation task with no scope",
		})
	})
	if runErr != nil {
		t.Fatalf("runWorkflowTaskAdd: %v", runErr)
	}
	if !strings.Contains(stdout, "no write_scope declared") {
		t.Errorf("expected an empty-write_scope warning, got: %q", stdout)
	}

	tf, loadErr := loadCanonicalTasks(repo, "wave-2")
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if taskIndexByID(tf, "t4") == -1 {
		t.Error("task should still be written despite the warning (WARN never blocks)")
	}
}

// TestRunWorkflowTaskAdd_PopulatedWriteScopeSilent verifies a task add with a
// non-empty --write-scope produces no warning.
func TestRunWorkflowTaskAdd_PopulatedWriteScopeSilent(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	chdirRepo(t, repo)

	var runErr error
	stdout := captureStdoutToString(t, func() {
		runErr = runWorkflowTaskAdd(taskAddInputs{
			PlanID:     "wave-2",
			TaskID:     "t4",
			Title:      "implementation task with scope",
			WriteScope: "commands/workflow/",
		})
	})
	if runErr != nil {
		t.Fatalf("runWorkflowTaskAdd: %v", runErr)
	}
	if strings.Contains(stdout, "no write_scope declared") {
		t.Errorf("did not expect an empty-write_scope warning, got: %q", stdout)
	}
}

// TestRunWorkflowTaskAdd_ResearchModeEmptyWriteScopeSilent verifies a task
// whose notes mark it as research/doc-only is exempted from the warning even
// with an empty write_scope.
func TestRunWorkflowTaskAdd_ResearchModeEmptyWriteScopeSilent(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	chdirRepo(t, repo)

	var runErr error
	stdout := captureStdoutToString(t, func() {
		runErr = runWorkflowTaskAdd(taskAddInputs{
			PlanID: "wave-2",
			TaskID: "t4",
			Title:  "write up findings",
			Notes:  "research task: survey prior art, no go code",
		})
	})
	if runErr != nil {
		t.Fatalf("runWorkflowTaskAdd: %v", runErr)
	}
	if strings.Contains(stdout, "no write_scope declared") {
		t.Errorf("research-mode task should be exempt from the empty write_scope warning, got: %q", stdout)
	}
}

// TestRunWorkflowTaskUpdate_EmptyWriteScopeWarnsOnUnrelatedFieldUpdate proves
// the warning evaluates the task's FINAL state, not just the field being
// touched: updating notes on a task that has always had an empty write_scope
// still warns.
func TestRunWorkflowTaskUpdate_EmptyWriteScopeWarnsOnUnrelatedFieldUpdate(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	chdirRepo(t, repo)

	if err := runWorkflowTaskAdd(taskAddInputs{PlanID: "wave-2", TaskID: "t4", Title: "no scope"}); err != nil {
		t.Fatalf("seed task add: %v", err)
	}

	var runErr error
	stdout := captureStdoutToString(t, func() {
		runErr = runWorkflowTaskUpdate(taskUpdateInputs{PlanID: "wave-2", TaskID: "t4", Notes: "still no scope"})
	})
	if runErr != nil {
		t.Fatalf("runWorkflowTaskUpdate: %v", runErr)
	}
	if !strings.Contains(stdout, "no write_scope declared") {
		t.Errorf("expected the empty-write_scope warning on the final task state, got: %q", stdout)
	}
}

// TestRunWorkflowTaskUpdate_PopulatedWriteScopeSilent verifies updating an
// unrelated field on a task that already has a populated write_scope produces
// no warning.
func TestRunWorkflowTaskUpdate_PopulatedWriteScopeSilent(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo) // t1 already has write_scope populated
	chdirRepo(t, repo)

	var runErr error
	stdout := captureStdoutToString(t, func() {
		runErr = runWorkflowTaskUpdate(taskUpdateInputs{PlanID: "wave-2", TaskID: "t1", Notes: "unrelated note update"})
	})
	if runErr != nil {
		t.Fatalf("runWorkflowTaskUpdate: %v", runErr)
	}
	if strings.Contains(stdout, "no write_scope declared") {
		t.Errorf("did not expect a warning for a task with populated write_scope, got: %q", stdout)
	}
}
