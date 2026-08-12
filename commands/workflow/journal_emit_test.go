package workflow

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/journal"
)

// TestMain isolates the per-repo journal under a throwaway XDG_STATE_HOME for the
// whole package run, so the real wiring (runner → emitWorkflow* → journal.Emit)
// exercised by every existing workflow test writes events.log into a temp dir
// instead of the developer's real ~/.local/state.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "wf-journal-state-")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("XDG_STATE_HOME", dir)
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// captureJournal swaps journalEmit for an in-memory recorder and restores it via
// t.Cleanup. The returned pointer accumulates every appended envelope so tests
// can assert the typed event a command produced without reading the log file.
func captureJournal(t *testing.T) *[]journal.Envelope {
	t.Helper()
	prev := journalEmit
	var got []journal.Envelope
	journalEmit = func(_ string, e journal.Envelope) error {
		got = append(got, e)
		return nil
	}
	t.Cleanup(func() { journalEmit = prev })
	return &got
}

// findEvent returns the first captured envelope for command, or fails.
func findEvent(t *testing.T, got []journal.Envelope, command string) journal.Envelope {
	t.Helper()
	for _, e := range got {
		if e.Command == command {
			return e
		}
	}
	t.Fatalf("no journal event for %q in %d captured events", command, len(got))
	return journal.Envelope{}
}

// ── journal_emit.go unit coverage ──────────────────────────────────────────────

func TestEmitWorkflowSuccess_AppendsTypedEvent(t *testing.T) {
	got := captureJournal(t)
	emitWorkflowSuccess("/repo", journal.CmdAdvance,
		&journal.AdvanceInput{Plan: "p", Task: "t", Status: "completed"},
		&journal.AdvanceObserved{FromStatus: "in_progress", ToStatus: "completed"})
	if len(*got) != 1 {
		t.Fatalf("want 1 event, got %d", len(*got))
	}
	e := (*got)[0]
	if e.Command != journal.CmdAdvance || e.EventType != journal.EventDurableDelta {
		t.Fatalf("unexpected envelope: %+v", e)
	}
	if e.Actor != journal.ActorMain {
		t.Fatalf("actor = %q, want main", e.Actor)
	}
	var obs journal.AdvanceObserved
	if err := json.Unmarshal(e.Observed, &obs); err != nil || obs.ToStatus != "completed" {
		t.Fatalf("observed decode: %v %+v", err, obs)
	}
}

func TestEmitWorkflowSuccess_BlankRepoSkips(t *testing.T) {
	got := captureJournal(t)
	emitWorkflowSuccess("", journal.CmdAdvance, &journal.AdvanceInput{}, &journal.AdvanceObserved{})
	if len(*got) != 0 {
		t.Fatalf("blank repoPath should skip emission, got %d", len(*got))
	}
}

func TestEmitWorkflowSuccess_BuildErrorWarnsNotFatal(t *testing.T) {
	got := captureJournal(t)
	// An unregistered command makes NewEvent fail; the helper must warn and not
	// append (and must never panic / propagate).
	emitWorkflowSuccess("/repo", "workflow not-a-command", nil, nil)
	if len(*got) != 0 {
		t.Fatalf("build error should not append, got %d", len(*got))
	}
}

func TestEmitWorkflowEvent_AppendErrorWarnsNotFatal(t *testing.T) {
	prev := journalEmit
	journalEmit = func(string, journal.Envelope) error { return errors.New("disk full") }
	t.Cleanup(func() { journalEmit = prev })
	// Must not panic or propagate — proves a journal append failure cannot fail
	// the underlying command.
	emitWorkflowSuccess("/repo", journal.CmdCheckpoint,
		&journal.CheckpointInput{Message: "m"}, &journal.CheckpointObserved{CheckpointID: "x"})
}

func TestEmitWorkflowFailure_DropsObserved(t *testing.T) {
	got := captureJournal(t)
	emitWorkflowFailure("/repo", journal.CmdAdvance, &journal.AdvanceInput{Plan: "p"})
	e := findEvent(t, *got, journal.CmdAdvance)
	if e.EventType != journal.EventFailed {
		t.Fatalf("event_type = %q, want failed", e.EventType)
	}
	if e.Observed != nil {
		t.Fatalf("failed event must drop observed, got %s", string(e.Observed))
	}
}

func TestJournalTier1_BranchesOnOK(t *testing.T) {
	got := captureJournal(t)
	in := &journal.AdvanceInput{Plan: "p"}
	journalTier1("/repo", journal.CmdAdvance, in, &journal.AdvanceObserved{ToStatus: "x"}, true)
	journalTier1("/repo", journal.CmdAdvance, in, &journal.AdvanceObserved{ToStatus: "x"}, false)
	if len(*got) != 2 {
		t.Fatalf("want 2 events, got %d", len(*got))
	}
	if (*got)[0].EventType != journal.EventDurableDelta {
		t.Fatalf("ok=true should be durable_delta, got %q", (*got)[0].EventType)
	}
	if (*got)[1].EventType != journal.EventFailed {
		t.Fatalf("ok=false should be failed, got %q", (*got)[1].EventType)
	}
}

func TestEmitWorkflowDelta_RecordsChangedFields(t *testing.T) {
	got := captureJournal(t)
	emitWorkflowDelta("/repo", journal.CmdTaskUpdate, "plan-1", "t1",
		map[string]string{"notes": "new", "title": "T"})
	e := findEvent(t, *got, journal.CmdTaskUpdate)
	if e.EventType != journal.EventInputOnly {
		t.Fatalf("tier-2 should be input_only, got %q", e.EventType)
	}
	var obs journal.DeltaObserved
	if err := json.Unmarshal(e.Observed, &obs); err != nil {
		t.Fatal(err)
	}
	if len(obs.FieldsReplaced) != 2 || obs.FieldsReplaced[0] != "notes" || obs.FieldsReplaced[1] != "title" {
		t.Fatalf("fields_replaced = %v, want sorted [notes title]", obs.FieldsReplaced)
	}
	var in journal.DeltaInput
	if err := json.Unmarshal(e.Input, &in); err != nil {
		t.Fatal(err)
	}
	if in.Plan != "plan-1" || in.Task != "t1" || len(in.ChangedFields) != 2 {
		t.Fatalf("unexpected delta input: %+v", in)
	}
}

func TestEmitWorkflowDelta_EmptyAndBlankSkip(t *testing.T) {
	got := captureJournal(t)
	emitWorkflowDelta("/repo", journal.CmdTaskUpdate, "p", "t", nil)
	emitWorkflowDelta("", journal.CmdTaskUpdate, "p", "t", map[string]string{"a": "b"})
	if len(*got) != 0 {
		t.Fatalf("no-op and blank repo should skip, got %d", len(*got))
	}
}

func TestEmitWorkflowDelta_ChangedFieldsErrorWarns(t *testing.T) {
	got := captureJournal(t)
	longName := make([]byte, 200)
	for i := range longName {
		longName[i] = 'x'
	}
	// An over-long field name makes NewChangedFields fail; the helper warns and
	// emits nothing rather than corrupting the record.
	emitWorkflowDelta("/repo", journal.CmdTaskUpdate, "p", "t", map[string]string{string(longName): "v"})
	if len(*got) != 0 {
		t.Fatalf("changed-fields error should not append, got %d", len(*got))
	}
}

func TestJournalActor_IsMain(t *testing.T) {
	if journalActor() != journal.ActorMain {
		t.Fatalf("journalActor = %q, want main", journalActor())
	}
}

// ── per-command wiring: typed events from the real runners ─────────────────────

func TestRunWorkflowAdvance_EmitsTier1(t *testing.T) {
	repo := setupTestProject(t)
	chdirForCov(t, repo)
	got := captureJournal(t)
	if err := runWorkflowAdvance("plan-001", "task-001", "in_progress"); err != nil {
		t.Fatalf("advance: %v", err)
	}
	e := findEvent(t, *got, journal.CmdAdvance)
	if e.EventType != journal.EventDurableDelta {
		t.Fatalf("event_type = %q", e.EventType)
	}
	var obs journal.AdvanceObserved
	if err := json.Unmarshal(e.Observed, &obs); err != nil {
		t.Fatal(err)
	}
	if obs.FromStatus != "pending" || obs.ToStatus != "in_progress" {
		t.Fatalf("observed = %+v, want pending→in_progress", obs)
	}
}

func TestRunWorkflowAdvance_FailureEmitsFailedEvent(t *testing.T) {
	repo := setupTestProject(t)
	chdirForCov(t, repo)
	got := captureJournal(t)
	// Plan resolves the project but the task does not exist, so the mutation
	// fails after repoPath is known → a failed event with input only.
	if err := runWorkflowAdvance("plan-001", "ghost-task", "in_progress"); err == nil {
		t.Fatal("expected advance to fail for missing task")
	}
	e := findEvent(t, *got, journal.CmdAdvance)
	if e.EventType != journal.EventFailed || e.Observed != nil {
		t.Fatalf("want failed event with no observed, got type=%q observed=%s", e.EventType, string(e.Observed))
	}
}

func TestRunWorkflowPlanUpdate_DeltaOnlyChangedFields(t *testing.T) {
	repo := setupTestProject(t)
	chdirForCov(t, repo)
	got := captureJournal(t)
	// plan-001 starts active; setting status=active is a no-op, title changes.
	if err := runWorkflowPlanUpdate("plan-001", "active", "New Title", "", "", "", ""); err != nil {
		t.Fatalf("plan update: %v", err)
	}
	e := findEvent(t, *got, journal.CmdPlanUpdate)
	var obs journal.DeltaObserved
	if err := json.Unmarshal(e.Observed, &obs); err != nil {
		t.Fatal(err)
	}
	if len(obs.FieldsReplaced) != 1 || obs.FieldsReplaced[0] != "title" {
		t.Fatalf("fields_replaced = %v, want [title] (status unchanged)", obs.FieldsReplaced)
	}
}

func TestRunWorkflowPlanUpdate_NoChangeEmitsNothing(t *testing.T) {
	repo := setupTestProject(t)
	chdirForCov(t, repo)
	got := captureJournal(t)
	// Every override is empty or equal to the current value → no delta.
	if err := runWorkflowPlanUpdate("plan-001", "active", "", "", "", "", ""); err != nil {
		t.Fatalf("plan update: %v", err)
	}
	for _, e := range *got {
		if e.Command == journal.CmdPlanUpdate {
			t.Fatalf("no-op plan update should not journal, got %+v", e)
		}
	}
}

func TestRunWorkflowTaskAdd_EmitsAppended(t *testing.T) {
	repo := setupTestProject(t)
	chdirForCov(t, repo)
	got := captureJournal(t)
	err := runWorkflowTaskAdd(taskAddInputs{
		PlanID: "plan-001", TaskID: "task-new", Title: "New", AppType: "go-cli",
		WriteScope: "commands/,internal/", VerificationRequired: true,
	})
	if err != nil {
		t.Fatalf("task add: %v", err)
	}
	e := findEvent(t, *got, journal.CmdTaskAdd)
	if e.EventType != journal.EventInputOnly {
		t.Fatalf("tier-2 should be input_only, got %q", e.EventType)
	}
	var obs journal.TaskAddObserved
	if err := json.Unmarshal(e.Observed, &obs); err != nil || !obs.Appended {
		t.Fatalf("observed = %+v err=%v", obs, err)
	}
	var in journal.TaskAddInput
	if err := json.Unmarshal(e.Input, &in); err != nil {
		t.Fatal(err)
	}
	if in.AppType != "go-cli" || len(in.WriteScope) != 2 {
		t.Fatalf("input = %+v", in)
	}
}

func TestRunWorkflowTaskUpdate_DeltaChangedFields(t *testing.T) {
	repo := setupTestProject(t)
	chdirForCov(t, repo)
	got := captureJournal(t)
	if err := runWorkflowTaskUpdate(taskUpdateInputs{
		PlanID: "plan-001", TaskID: "task-001", Title: "Renamed", Notes: "fresh notes",
		WriteScope: "", DependsOn: "", Blocks: "",
	}); err != nil {
		t.Fatalf("task update: %v", err)
	}
	e := findEvent(t, *got, journal.CmdTaskUpdate)
	var obs journal.DeltaObserved
	if err := json.Unmarshal(e.Observed, &obs); err != nil {
		t.Fatal(err)
	}
	if len(obs.FieldsReplaced) != 2 {
		t.Fatalf("fields_replaced = %v, want [notes title]", obs.FieldsReplaced)
	}
}

func TestRunWorkflowPlanCreate_EmitsObservedFiles(t *testing.T) {
	repo := setupTestProject(t)
	chdirForCov(t, repo)
	got := captureJournal(t)
	if err := runWorkflowPlanCreate("brand-new", "Brand New", "", "dot-agents", "", ""); err != nil {
		t.Fatalf("plan create: %v", err)
	}
	e := findEvent(t, *got, journal.CmdPlanCreate)
	var obs journal.PlanCreateObserved
	if err := json.Unmarshal(e.Observed, &obs); err != nil {
		t.Fatal(err)
	}
	if len(obs.FilesCreated) != 2 || obs.PlanDir == "" {
		t.Fatalf("observed = %+v", obs)
	}
}

func TestRunWorkflowPrefsSetLocal_EmitsDelta(t *testing.T) {
	repo := setupTestProject(t)
	t.Setenv("AGENTS_HOME", t.TempDir())
	chdirForCov(t, repo)
	got := captureJournal(t)
	if err := runWorkflowPrefsSetLocal("verification.test_command", "go test ./..."); err != nil {
		t.Fatalf("prefs set-local: %v", err)
	}
	e := findEvent(t, *got, journal.CmdPrefsSetLocal)
	if e.EventType != journal.EventInputOnly {
		t.Fatalf("event_type = %q", e.EventType)
	}
	var obs journal.DeltaObserved
	if err := json.Unmarshal(e.Observed, &obs); err != nil {
		t.Fatal(err)
	}
	if len(obs.FieldsReplaced) != 1 || obs.FieldsReplaced[0] != "verification.test_command" {
		t.Fatalf("fields_replaced = %v", obs.FieldsReplaced)
	}
}

func TestRunWorkflowCheckpoint_EmitsTier1(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	t.Setenv("AGENTS_HOME", t.TempDir())
	chdirForCov(t, repo)
	got := captureJournal(t)
	if err := runWorkflowCheckpoint("resume slice", "pass", "go test ./..."); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	e := findEvent(t, *got, journal.CmdCheckpoint)
	var obs journal.CheckpointObserved
	if err := json.Unmarshal(e.Observed, &obs); err != nil {
		t.Fatal(err)
	}
	if obs.CheckpointID == "" {
		t.Fatalf("checkpoint id empty: %+v", obs)
	}
}

func TestRunWorkflowPlanDeriveScope_EmitsObserved(t *testing.T) {
	repo := setupTestProject(t)
	chdirForCov(t, repo)
	got := captureJournal(t)
	if err := runWorkflowPlanDeriveScope("plan-001", "task-001", nil, nil); err != nil {
		t.Fatalf("derive-scope: %v", err)
	}
	e := findEvent(t, *got, journal.CmdPlanDeriveScope)
	var obs journal.DeriveScopeObserved
	if err := json.Unmarshal(e.Observed, &obs); err != nil {
		t.Fatal(err)
	}
	if obs.SidecarPath == "" || obs.Mode == "" || obs.Confidence == "" {
		t.Fatalf("observed = %+v", obs)
	}
}

func TestRunWorkflowFoldBack_CreateThenUpdateEmit(t *testing.T) {
	repo := setupFoldBackProject(t)
	chdirForCov(t, repo)
	got := captureJournal(t)
	createCmd := newFoldBackTestCmd("first observation", "slug-x-p1", "p1")
	if err := runWorkflowFoldBackCreate(createCmd, nil); err != nil {
		t.Fatalf("fold-back create: %v", err)
	}
	ce := findEvent(t, *got, journal.CmdFoldBackCreate)
	var cObs journal.FoldBackObserved
	if err := json.Unmarshal(ce.Observed, &cObs); err != nil {
		t.Fatal(err)
	}
	if cObs.Action != "create" || cObs.ArtifactID == "" {
		t.Fatalf("create observed = %+v", cObs)
	}
	updateCmd := newFoldBackTestCmd("refined observation", "slug-x-p1", "p1")
	if err := runWorkflowFoldBackUpdate(updateCmd, nil); err != nil {
		t.Fatalf("fold-back update: %v", err)
	}
	ue := findEvent(t, *got, journal.CmdFoldBackUpdate)
	var uObs journal.FoldBackObserved
	if err := json.Unmarshal(ue.Observed, &uObs); err != nil {
		t.Fatal(err)
	}
	if uObs.Action != "update" {
		t.Fatalf("update observed = %+v", uObs)
	}
}

func TestRunWorkflowVerifyRecord_EmitsTier1(t *testing.T) {
	repo := setupTestProject(t)
	t.Setenv("AGENTS_HOME", t.TempDir())
	chdirForCov(t, repo)
	got := captureJournal(t)
	err := runWorkflowVerifyRecord(verifyRecordInputs{
		Kind: "test", Status: "pass", Scope: "repo", Summary: "go test ./...",
	})
	if err != nil {
		t.Fatalf("verify record: %v", err)
	}
	e := findEvent(t, *got, journal.CmdVerifyRecord)
	if e.EventType != journal.EventDurableDelta {
		t.Fatalf("event_type = %q", e.EventType)
	}
	var obs journal.VerifyRecordObserved
	if err := json.Unmarshal(e.Observed, &obs); err != nil {
		t.Fatal(err)
	}
	if obs.VerificationLogID == "" {
		t.Fatalf("verification_log_id empty: %+v", obs)
	}
}

func TestEmitSweepApplyJournal_EmitsFixes(t *testing.T) {
	repo := setupTestProject(t)
	chdirForCov(t, repo)
	got := captureJournal(t)
	applied := []SweepActionItem{{Project: ManagedProject{Name: "proj-a"}, Action: SweepActionType("checkpoint_stale")}}
	emitSweepApplyJournal(7, 14, applied)
	e := findEvent(t, *got, journal.CmdSweepApply)
	var obs journal.SweepApplyObserved
	if err := json.Unmarshal(e.Observed, &obs); err != nil {
		t.Fatal(err)
	}
	if len(obs.FixesApplied) != 1 || obs.FixesApplied[0].Project != "proj-a" || obs.FixesApplied[0].Action != "checkpoint_stale" {
		t.Fatalf("fixes = %+v", obs.FixesApplied)
	}
	var in journal.SweepApplyInput
	if err := json.Unmarshal(e.Input, &in); err != nil {
		t.Fatal(err)
	}
	if in.StaleDays != 7 || in.ProposalDays != 14 {
		t.Fatalf("input = %+v", in)
	}
}

func TestEmitSweepApplyJournal_EmptyAndUnresolvedSkip(t *testing.T) {
	got := captureJournal(t)
	emitSweepApplyJournal(1, 1, nil) // nothing applied → skip
	prev := osGetwd
	osGetwd = func() (string, error) { return "", errors.New("no cwd") }
	t.Cleanup(func() { osGetwd = prev })
	emitSweepApplyJournal(1, 1, []SweepActionItem{{Project: ManagedProject{Name: "x"}, Action: "y"}})
	if len(*got) != 0 {
		t.Fatalf("empty/unresolved should skip, got %d", len(*got))
	}
}

func TestEmitArchiveOrphansJournal_AppliedAndSkipped(t *testing.T) {
	got := captureJournal(t)
	result := archiveOrphansResult{DryRun: false, Actions: []archiveOrphanAction{
		{TaskID: "t-applied", Class: orphanClassMoveMergeBack, Target: "dest/x.md", Applied: true},
		{TaskID: "t-skipped", Class: orphanClassUnknownTask, Applied: false},
	}}
	emitArchiveOrphansJournal("/repo", result)
	e := findEvent(t, *got, journal.CmdArchiveOrphans)
	var obs journal.ArchiveOrphansObserved
	if err := json.Unmarshal(e.Observed, &obs); err != nil {
		t.Fatal(err)
	}
	if len(obs.Actions) != 2 || obs.Actions[0].Resolution != "applied" || obs.Actions[1].Resolution != "skipped" {
		t.Fatalf("actions = %+v", obs.Actions)
	}
	if obs.Actions[0].DestPath != "dest/x.md" {
		t.Fatalf("dest path = %q", obs.Actions[0].DestPath)
	}
}

func TestEmitArchiveOrphansJournal_DryRunAndEmptySkip(t *testing.T) {
	got := captureJournal(t)
	emitArchiveOrphansJournal("/repo", archiveOrphansResult{DryRun: true, Actions: []archiveOrphanAction{{TaskID: "x"}}})
	emitArchiveOrphansJournal("/repo", archiveOrphansResult{DryRun: false})
	if len(*got) != 0 {
		t.Fatalf("dry-run/empty should skip, got %d", len(*got))
	}
}

func TestRunWorkflowCommit_EmitsOnRealCommit(t *testing.T) {
	got := captureJournal(t)
	g := &fakeGit{status: []StatusEntry{{Path: ".agents/workflow/plans/x/PLAN.yaml", XY: ".M"}}}
	prev := commitDisabled
	commitDisabled = func() (bool, string) { return false, "" }
	t.Cleanup(func() { commitDisabled = prev })
	repo := setupTestProject(t)
	chdirForCov(t, repo)
	if err := runWorkflowCommit(&bytes.Buffer{}, g, false, []string{".agents/y.yaml"}); err != nil {
		t.Fatalf("commit: %v", err)
	}
	e := findEvent(t, *got, journal.CmdCommit)
	var obs journal.CommitObserved
	if err := json.Unmarshal(e.Observed, &obs); err != nil {
		t.Fatal(err)
	}
	if len(obs.StagedPaths) == 0 || obs.Noop {
		t.Fatalf("observed = %+v, want staged paths, noop=false", obs)
	}
}
