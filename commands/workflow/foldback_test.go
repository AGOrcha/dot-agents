package workflow

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

// loadOnlyFoldBackArtifact globs the repo's fold-back staging dir,
// asserts exactly one fold-*.yaml file exists, reads + unmarshals it,
// and returns the parsed artifact. Replaces the recurring 16-17 line
// glob+read+unmarshal block in TestFoldBackCreate* tests.
func loadOnlyFoldBackArtifact(t *testing.T, repo string) foldBackArtifact {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(repo, ".agents", "active", "fold-back", "fold-*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected one fold-back artifact, got %d", len(matches))
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	var a foldBackArtifact
	if err := yaml.Unmarshal(data, &a); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestFoldBackCreateSmall(t *testing.T) {
	repo := setupFoldBackProject(t)
	if err := executeWorkflowCommand(t, repo, "fold-back", "create", "--plan", "p1", "--task", "t1", "--observation", "new obs"); err != nil {
		t.Fatal(err)
	}
	tf, err := loadCanonicalTasks(repo, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tf.Tasks[0].Notes, "existing") || !strings.Contains(tf.Tasks[0].Notes, "new obs") {
		t.Fatalf("task notes = %q", tf.Tasks[0].Notes)
	}
	a := loadOnlyFoldBackArtifact(t, repo)
	if a.Classification != "small" || a.RoutedTo != "task_note:p1/t1" {
		t.Fatalf("artifact: %+v", a)
	}
}

func TestFoldBackCreateNoTask(t *testing.T) {
	repo := setupFoldBackProject(t)
	if err := executeWorkflowCommand(t, repo, "fold-back", "create", "--plan", "p1", "--observation", "plan-level obs"); err != nil {
		t.Fatal(err)
	}
	plan, err := loadCanonicalPlan(repo, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.Summary, "start") || !strings.Contains(plan.Summary, "plan-level obs") {
		t.Fatalf("plan summary = %q", plan.Summary)
	}
	a := loadOnlyFoldBackArtifact(t, repo)
	if a.Classification != "small" || a.RoutedTo != "plan_summary:p1" || a.TaskID != "" {
		t.Fatalf("artifact: %+v", a)
	}
}

func TestFoldBackCreatePropose(t *testing.T) {
	repo := setupFoldBackProject(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	tfBefore, err := loadCanonicalTasks(repo, "p1")
	if err != nil {
		t.Fatal(err)
	}
	beforeNotes := tfBefore.Tasks[0].Notes

	if err := executeWorkflowCommand(t, repo, "fold-back", "create", "--plan", "p1", "--task", "t1", "--observation", "big change", "--propose"); err != nil {
		t.Fatal(err)
	}

	tfAfter, err := loadCanonicalTasks(repo, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if tfAfter.Tasks[0].Notes != beforeNotes {
		t.Fatalf("TASKS.yaml notes changed under --propose: %q -> %q", beforeNotes, tfAfter.Tasks[0].Notes)
	}

	propMatches, err := filepath.Glob(filepath.Join(agentsHome, "proposals", "obs-*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(propMatches) != 1 {
		t.Fatalf("expected one proposal, got %d", len(propMatches))
	}

	a := loadOnlyFoldBackArtifact(t, repo)
	if a.Classification != "proposal" || !strings.HasPrefix(a.RoutedTo, "proposal:obs-") {
		t.Fatalf("artifact: %+v", a)
	}
}

func TestFoldBackSlugTaskDedupesNotes(t *testing.T) {
	repo := setupFoldBackProject(t)
	slug := "schema-drift-p1-t1"
	if err := executeWorkflowCommand(t, repo, "fold-back", "create", "--plan", "p1", "--task", "t1", "--slug", slug, "--observation", "first"); err != nil {
		t.Fatal(err)
	}
	if err := executeWorkflowCommand(t, repo, "fold-back", "create", "--plan", "p1", "--task", "t1", "--slug", slug, "--observation", "second"); err != nil {
		t.Fatal(err)
	}
	tf, err := loadCanonicalTasks(repo, "p1")
	if err != nil {
		t.Fatal(err)
	}
	notes := tf.Tasks[0].Notes
	if strings.Count(notes, "(fb:"+slug+")") != 1 {
		t.Fatalf("expected exactly one tagged line for slug, got notes:\n%s", notes)
	}
	if !strings.Contains(notes, "second") || strings.Contains(notes, "first") {
		t.Fatalf("expected latest observation only, got:\n%s", notes)
	}
}

func TestFoldBackUpdateMissingSlug(t *testing.T) {
	repo := setupFoldBackProject(t)
	oldwd, _ := os.Getwd()
	defer func() { _ = os.Chdir(oldwd) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	cmd := NewCmdForTest()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"fold-back", "update", "--plan", "p1", "--slug", "missing-slug", "--observation", "x"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for missing slug")
	}
}

func TestFoldBackUpdatePlanScoped(t *testing.T) {
	repo := setupFoldBackProject(t)
	slug := "fold-back-triage-p1"
	if err := executeWorkflowCommand(t, repo, "fold-back", "create", "--plan", "p1", "--slug", slug, "--observation", "v1"); err != nil {
		t.Fatal(err)
	}
	if err := executeWorkflowCommand(t, repo, "fold-back", "update", "--plan", "p1", "--slug", slug, "--observation", "v2"); err != nil {
		t.Fatal(err)
	}
	plan, err := loadCanonicalPlan(repo, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(plan.Summary, "(fb:"+slug+")") != 1 || !strings.Contains(plan.Summary, "v2") {
		t.Fatalf("plan summary = %q", plan.Summary)
	}
}

func TestFoldBackUpdateTaskScoped(t *testing.T) {
	repo := setupFoldBackProject(t)
	slug := "coverage-regression-p1-t1"
	if err := executeWorkflowCommand(t, repo, "fold-back", "create", "--plan", "p1", "--task", "t1", "--slug", slug, "--observation", "round-a"); err != nil {
		t.Fatal(err)
	}
	if err := executeWorkflowCommand(t, repo, "fold-back", "update", "--plan", "p1", "--slug", slug, "--task", "t1", "--observation", "round-b"); err != nil {
		t.Fatal(err)
	}
	tf, err := loadCanonicalTasks(repo, "p1")
	if err != nil {
		t.Fatal(err)
	}
	notes := tf.Tasks[0].Notes
	if strings.Count(notes, "(fb:"+slug+")") != 1 || !strings.Contains(notes, "round-b") || strings.Contains(notes, "round-a") {
		t.Fatalf("task notes = %q", notes)
	}
}

func TestFoldBackUpdateTaskScopedRequiresTaskFlag(t *testing.T) {
	repo := setupFoldBackProject(t)
	slug := "tool-bug-p1-t1"
	if err := executeWorkflowCommand(t, repo, "fold-back", "create", "--plan", "p1", "--task", "t1", "--slug", slug, "--observation", "first"); err != nil {
		t.Fatal(err)
	}
	oldwd, _ := os.Getwd()
	defer func() { _ = os.Chdir(oldwd) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	cmd := NewCmdForTest()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"fold-back", "update", "--plan", "p1", "--slug", slug, "--observation", "x"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when updating task-scoped fold-back without --task")
	}
}

func TestFoldBackSlugInvalid(t *testing.T) {
	repo := setupFoldBackProject(t)
	oldwd, _ := os.Getwd()
	defer func() { _ = os.Chdir(oldwd) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	cmd := NewCmdForTest()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"fold-back", "create", "--plan", "p1", "--task", "t1", "--slug", "bad slug", "--observation", "x"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for invalid slug")
	}
}

func TestDelegationCloseoutAcceptHandwrittenMergeBack(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	if err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--slice", "s1", "--owner", "w"); err != nil {
		t.Fatal(err)
	}
	c, err := loadDelegationContract(repo, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if c.Status != "active" {
		t.Fatalf("delegation status = %q, want active", c.Status)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if err := saveMergeBack(repo, &MergeBackSummary{
		SchemaVersion:      1,
		TaskID:             "t1",
		ParentPlanID:       "p1",
		Title:              c.Title,
		Summary:            "worker merge-back without CLI",
		VerificationResult: MergeBackVerification{Status: "pass", Summary: "ok"},
		IntegrationNotes:   "ok",
		CreatedAt:          now,
	}); err != nil {
		t.Fatal(err)
	}
	c2, err := loadDelegationContract(repo, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if c2.Status != "active" {
		t.Fatalf("after handwritten merge-back, delegation status = %q, want active (closeout reconciles)", c2.Status)
	}
	if err := executeWorkflowCommand(t, repo, "delegation", "closeout", "--plan", "p1", "--task", "t1", "--decision", "accept"); err != nil {
		t.Fatal(err)
	}
	tf, err := loadCanonicalTasks(repo, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if tf.Tasks[0].Status != "completed" {
		t.Fatalf("task status = %q, want completed", tf.Tasks[0].Status)
	}
}

func TestDelegationCloseoutAccept(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	if err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--slice", "s1", "--owner", "w"); err != nil {
		t.Fatal(err)
	}
	if err := executeWorkflowCommand(t, repo, "merge-back", "--task", "t1", "--summary", "done", "--verification-status", "pass"); err != nil {
		t.Fatal(err)
	}
	if err := executeWorkflowCommand(t, repo, "delegation", "closeout", "--plan", "p1", "--task", "t1", "--decision", "accept"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".agents", "active", "delegation", "t1.yaml")); !os.IsNotExist(err) {
		t.Fatal("expected active delegation removed")
	}
	if _, err := os.Stat(filepath.Join(repo, ".agents", "active", "merge-back", "t1.md")); !os.IsNotExist(err) {
		t.Fatal("expected active merge-back removed")
	}
	if _, err := os.Stat(filepath.Join(repo, ".agents", "active", "verification", "t1")); !os.IsNotExist(err) {
		t.Fatal("expected active verification dir removed")
	}
	tf, err := loadCanonicalTasks(repo, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if tf.Tasks[0].Status != "completed" {
		t.Fatalf("task status = %q, want completed", tf.Tasks[0].Status)
	}
	plan, err := loadCanonicalPlan(repo, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != "completed" {
		t.Fatalf("plan status = %q, want completed", plan.Status)
	}
	matches, _ := filepath.Glob(filepath.Join(repo, ".agents", "history", "p1", "delegate-merge-back-archive", "*", "t1", "closeout.yaml"))
	if len(matches) != 1 {
		t.Fatalf("expected one closeout record, got %v", matches)
	}
	verificationArchive := filepath.Join(filepath.Dir(matches[0]), "verification", "merge-back.result.yaml")
	if _, err := os.Stat(verificationArchive); err != nil {
		t.Fatalf("expected archived verification artifact at %s: %v", verificationArchive, err)
	}
}

func TestDelegationCloseoutReject(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	if err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--slice", "s1", "--owner", "w"); err != nil {
		t.Fatal(err)
	}
	if err := executeWorkflowCommand(t, repo, "merge-back", "--task", "t1", "--summary", "try", "--verification-status", "fail"); err != nil {
		t.Fatal(err)
	}
	if err := executeWorkflowCommand(t, repo, "delegation", "closeout", "--plan", "p1", "--task", "t1", "--decision", "reject", "--note", "fix tests"); err != nil {
		t.Fatal(err)
	}
	tf, err := loadCanonicalTasks(repo, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if tf.Tasks[0].Status != "blocked" {
		t.Fatalf("task status = %q, want blocked", tf.Tasks[0].Status)
	}
	if !strings.Contains(tf.Tasks[0].Notes, "delegation closeout reject: fix tests") {
		t.Fatalf("expected reject note in task notes: %q", tf.Tasks[0].Notes)
	}
	plan, err := loadCanonicalPlan(repo, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != "active" {
		t.Fatalf("plan status = %q, want active", plan.Status)
	}
}

func TestFoldBackList(t *testing.T) {
	repo := setupFoldBackTwoPlanProject(t)
	if err := executeWorkflowCommand(t, repo, "fold-back", "create", "--plan", "p1", "--task", "t1", "--observation", "a1"); err != nil {
		t.Fatal(err)
	}
	if err := executeWorkflowCommand(t, repo, "fold-back", "create", "--plan", "p2", "--task", "t1", "--observation", "a2"); err != nil {
		t.Fatal(err)
	}

	workflowTestJSON = true
	defer func() { workflowTestJSON = false }()

	outAll := executeWorkflowCommandOutput(t, repo, "fold-back", "list")
	if !strings.Contains(outAll, `"plan_id": "p1"`) || !strings.Contains(outAll, `"plan_id": "p2"`) {
		t.Fatalf("list all should include both plans: %s", outAll)
	}

	outP1 := executeWorkflowCommandOutput(t, repo, "fold-back", "list", "--plan", "p1")
	if !strings.Contains(outP1, `"plan_id": "p1"`) || strings.Contains(outP1, `"plan_id": "p2"`) {
		t.Fatalf("filtered list: %s", outP1)
	}
}

func TestFoldBackCreate_ProposeWritesProposal(t *testing.T) {
	repo := setupFoldBackProject(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	if err := executeWorkflowCommand(t, repo, "fold-back", "create",
		"--plan", "p1", "--task", "t1", "--observation", "needs design change", "--propose"); err != nil {
		t.Fatalf("expected propose flow to succeed: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(agentsHome, "proposals", "obs-*.md"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("expected one proposal, err=%v matches=%v", err, matches)
	}
}

func TestFoldBackCreate_DispatchWriteError(t *testing.T) {
	repo := setupFoldBackProject(t)

	prev := osWriteFile
	calls := 0
	osWriteFile = func(name string, data []byte, perm os.FileMode) error {
		calls++
		if calls >= 2 {
			return errors.New("write boom")
		}
		return prev(name, data, perm)
	}
	t.Cleanup(func() { osWriteFile = prev })

	err := executeWorkflowCommand(t, repo, "fold-back", "create",
		"--plan", "p1", "--task", "t1", "--observation", "x")
	if err == nil {
		t.Fatal("expected write error somewhere in dispatch+writeArtifact")
	}
}

func TestFoldBackCreate_MissingObservation(t *testing.T) {
	repo := setupFoldBackProject(t)
	err := executeWorkflowCommand(t, repo, "fold-back", "create",
		"--plan", "p1", "--task", "t1", "--observation", "   ")
	if err == nil || !strings.Contains(err.Error(), "observation") {
		t.Fatalf("expected observation-required, got %v", err)
	}
}

func TestFoldBackUpdate_MissingSlug(t *testing.T) {
	repo := setupFoldBackProject(t)
	err := executeWorkflowCommand(t, repo, "fold-back", "update",
		"--plan", "p1", "--observation", "x")
	if err == nil || !strings.Contains(err.Error(), "slug") {
		t.Fatalf("expected slug-required-for-update, got %v", err)
	}
}

func TestFoldBackUpdate_RejectsCrossPlan(t *testing.T) {
	repo := setupFoldBackTwoPlanProject(t)
	slug := "shared-slug"
	if err := executeWorkflowCommand(t, repo, "fold-back", "create",
		"--plan", "p1", "--task", "t1", "--slug", slug, "--observation", "v1"); err != nil {
		t.Fatal(err)
	}
	err := executeWorkflowCommand(t, repo, "fold-back", "update",
		"--plan", "p2", "--task", "t1", "--slug", slug, "--observation", "v2")
	if err == nil {
		t.Fatal("expected cross-plan rejection")
	}
}

func TestFoldBackUpdate_ProposeInvalid(t *testing.T) {
	repo := setupFoldBackProject(t)
	slug := "small-slug"
	if err := executeWorkflowCommand(t, repo, "fold-back", "create",
		"--plan", "p1", "--task", "t1", "--slug", slug, "--observation", "v1"); err != nil {
		t.Fatal(err)
	}
	err := executeWorkflowCommand(t, repo, "fold-back", "update",
		"--plan", "p1", "--task", "t1", "--slug", slug, "--observation", "v2", "--propose")
	if err == nil {
		t.Fatal("expected --propose-not-valid-for-update")
	}
}

func TestFoldBackCreate_PlanNotFound(t *testing.T) {
	repo := setupFoldBackProject(t)
	err := executeWorkflowCommand(t, repo, "fold-back", "create",
		"--plan", "no-such", "--observation", "x")
	if err == nil || !strings.Contains(err.Error(), "no-such") {
		t.Fatalf("expected plan-not-found, got %v", err)
	}
}

func TestFoldBackCreate_JSON(t *testing.T) {
	repo := setupFoldBackProject(t)
	workflowTestJSON = true
	defer func() { workflowTestJSON = false }()

	out := executeWorkflowCommandOutput(t, repo, "fold-back",
		"create", "--plan", "p1", "--task", "t1", "--observation", "test obs")
	if !strings.Contains(out, `"plan_id"`) || !strings.Contains(out, `"observation"`) {
		t.Fatalf("missing JSON fields: %s", out)
	}
}

func TestFoldBackList_RendersHuman(t *testing.T) {
	repo := setupFoldBackProject(t)
	if err := executeWorkflowCommand(t, repo, "fold-back", "create",
		"--plan", "p1", "--task", "t1", "--observation", "x"); err != nil {
		t.Fatal(err)
	}
	out := executeWorkflowCommandOutput(t, repo, "fold-back", "list")
	if !strings.Contains(out, "p1") {
		t.Fatalf("expected p1 in human render: %s", out)
	}
}

func TestFoldBackList_NoArtifactsHuman(t *testing.T) {
	repo := setupTestProject(t)
	out := executeWorkflowCommandOutput(t, repo, "fold-back", "list")
	if !strings.Contains(out, "No fold-back observations") {
		t.Fatalf("expected empty list message, got %s", out)
	}
}

func TestFoldBackCreate_RewritesFromBlocks(t *testing.T) {
	repo := setupFoldBackProject(t)

	if err := executeWorkflowCommand(t, repo, "fold-back", "create",
		"--plan", "p1", "--task", "t1", "--slug", "first-slug", "--observation", "v1"); err != nil {
		t.Fatal(err)
	}

	err := executeWorkflowCommand(t, repo, "fold-back", "create",
		"--plan", "p1", "--slug", "first-slug", "--observation", "v2")
	if err == nil {
		t.Fatal("expected prior-validation error for task-scoped slug without --task")
	}
}

// fbPlanFileRel returns the on-disk path to a p1 plan artifact
// (TASKS.yaml / PLAN.yaml) inside repo.
func fbPlanFileRel(repo, name string) string {
	return filepath.Join(repo, ".agents", "workflow", "plans", "p1", name)
}

// fbDryRunInlineTaskNoteWritesNothing exercises the inline task-note route:
// the preview reports task_note:p1/t1 and leaves TASKS.yaml plus the fold-back
// staging dir untouched.
func fbDryRunInlineTaskNoteWritesNothing(t *testing.T) {
	repo := setupFoldBackProject(t)
	tasksPath := fbPlanFileRel(repo, "TASKS.yaml")
	before := mustReadFileString(t, tasksPath)

	out := dryRunFoldBackCreate(t, repo,
		"--plan", "p1", "--task", "t1", "--observation", "dry obs")

	if got := foldBackArtifactPaths(repo); len(got) != 0 {
		t.Fatalf("dry-run wrote fold-back artifact(s): %v", got)
	}
	if after := mustReadFileString(t, tasksPath); after != before {
		t.Fatalf("dry-run mutated TASKS.yaml:\nbefore=%q\nafter=%q", before, after)
	}
	requireContainsAll(t, out, "dry-run", "task_note:p1/t1", "TASKS.yaml")
}

// fbDryRunPlanSummaryWritesNothing exercises the plan-summary route: the
// preview reports plan_summary:p1 and leaves PLAN.yaml plus the staging dir
// untouched.
func fbDryRunPlanSummaryWritesNothing(t *testing.T) {
	repo := setupFoldBackProject(t)
	planPath := fbPlanFileRel(repo, "PLAN.yaml")
	before := mustReadFileString(t, planPath)

	out := dryRunFoldBackCreate(t, repo,
		"--plan", "p1", "--observation", "plan dry obs")

	if got := foldBackArtifactPaths(repo); len(got) != 0 {
		t.Fatalf("dry-run wrote fold-back artifact(s): %v", got)
	}
	if after := mustReadFileString(t, planPath); after != before {
		t.Fatalf("dry-run mutated PLAN.yaml:\nbefore=%q\nafter=%q", before, after)
	}
	requireContainsAll(t, out, "dry-run", "plan_summary:p1", "PLAN.yaml")
}

// fbDryRunProposeWritesNothing exercises the --propose route: the preview
// reports the obs- proposal but creates neither the proposal file nor the
// proposals dir, and writes no fold-back artifact.
func fbDryRunProposeWritesNothing(t *testing.T) {
	repo := setupFoldBackProject(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	out := dryRunFoldBackCreate(t, repo,
		"--plan", "p1", "--task", "t1", "--observation", "big dry", "--propose")

	if props, _ := filepath.Glob(filepath.Join(agentsHome, "proposals", "obs-*.md")); len(props) != 0 {
		t.Fatalf("dry-run wrote proposal file(s): %v", props)
	}
	if _, err := os.Stat(filepath.Join(agentsHome, "proposals")); !os.IsNotExist(err) {
		t.Fatalf("dry-run created the proposals dir (want none): %v", err)
	}
	if got := foldBackArtifactPaths(repo); len(got) != 0 {
		t.Fatalf("dry-run wrote fold-back artifact(s): %v", got)
	}
	requireContainsAll(t, out, "dry-run", "proposal:obs-", "create proposal")
}

// fbDryRunWetInlineWrites is the companion positive case: the same inline
// invocation WITHOUT --dry-run does mutate TASKS.yaml and persist the artifact,
// so the gate is the only difference between preview and write.
func fbDryRunWetInlineWrites(t *testing.T) {
	repo := setupFoldBackProject(t)
	seedFoldBack(t, repo, "--plan", "p1", "--task", "t1", "--observation", "wet obs")
	if got := foldBackArtifactPaths(repo); len(got) != 1 {
		t.Fatalf("expected one fold-back artifact without dry-run, got %v", got)
	}
	tf, err := loadCanonicalTasks(repo, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tf.Tasks[0].Notes, "wet obs") {
		t.Fatalf("task notes not mutated: %q", tf.Tasks[0].Notes)
	}
}

// fbDryRunGlobalFlagGuards proves the global -n/--dry-run flag
// (deps.Flags.DryRun, OR-merged in foldBackDryRun) triggers the same
// side-effect-free guard as the local --dry-run flag — no artifact, no
// TASKS.yaml edit — and queues zero journal events.
func fbDryRunGlobalFlagGuards(t *testing.T) {
	repo := setupFoldBackProject(t)
	tasksPath := fbPlanFileRel(repo, "TASKS.yaml")
	before := mustReadFileString(t, tasksPath)
	events := captureJournal(t)
	prev := deps.Flags.DryRun
	deps.Flags.DryRun = func() bool { return true }
	t.Cleanup(func() { deps.Flags.DryRun = prev })

	// No local --dry-run in the args: only the global flag gates here.
	out := executeWorkflowCommandOutput(t, repo, "fold-back", "create",
		"--plan", "p1", "--task", "t1", "--observation", "global dry")

	if got := foldBackArtifactPaths(repo); len(got) != 0 {
		t.Fatalf("global dry-run wrote fold-back artifact(s): %v", got)
	}
	if after := mustReadFileString(t, tasksPath); after != before {
		t.Fatalf("global dry-run mutated TASKS.yaml:\nbefore=%q\nafter=%q", before, after)
	}
	if len(*events) != 0 {
		t.Fatalf("global dry-run queued %d journal event(s), want 0", len(*events))
	}
	if !strings.Contains(out, "dry-run") {
		t.Fatalf("output missing dry-run marker:\n%s", out)
	}
}

// fbDryRunQueuesNoJournal proves a dry-run records nothing durable, so it must
// queue zero journal events (the write path defers a journalTier1 delta; the
// dry-run path returns first).
func fbDryRunQueuesNoJournal(t *testing.T) {
	repo := setupFoldBackProject(t)
	events := captureJournal(t)

	_ = dryRunFoldBackCreate(t, repo,
		"--plan", "p1", "--task", "t1", "--observation", "no journal")

	if len(*events) != 0 {
		t.Fatalf("dry-run queued %d journal event(s), want 0", len(*events))
	}
}

// fbDryRunMissingTaskErrors proves an accurate preview: a task-note dry-run
// against a NON-EXISTENT task must return the same missing-task error the real
// create path hits at updateTaskFoldBackNote, not a false-green preview — and
// must still write nothing / journal nothing.
func fbDryRunMissingTaskErrors(t *testing.T) {
	repo := setupFoldBackProject(t)
	tasksPath := fbPlanFileRel(repo, "TASKS.yaml")
	before := mustReadFileString(t, tasksPath)
	events := captureJournal(t)

	err := executeWorkflowCommand(t, repo, "fold-back", "create",
		"--plan", "p1", "--task", "ghost", "--observation", "no such task", "--dry-run")
	if err == nil {
		t.Fatal("dry-run of a task-note route against a missing task should error")
	}
	if !strings.Contains(err.Error(), "task ghost not found in plan p1") {
		t.Fatalf("want missing-task error, got %v", err)
	}
	if got := foldBackArtifactPaths(repo); len(got) != 0 {
		t.Fatalf("failed dry-run wrote fold-back artifact(s): %v", got)
	}
	if after := mustReadFileString(t, tasksPath); after != before {
		t.Fatalf("failed dry-run mutated TASKS.yaml:\nbefore=%q\nafter=%q", before, after)
	}
	if len(*events) != 0 {
		t.Fatalf("failed dry-run queued %d journal event(s), want 0", len(*events))
	}
}

// fbDryRunUnreadableTasksErrors proves the read-only precondition surfaces a
// TASKS.yaml load failure too (not just a missing task): a corrupt tasks file
// makes the task-note dry-run report the same load error the write path would,
// still writing nothing.
func fbDryRunUnreadableTasksErrors(t *testing.T) {
	repo := setupFoldBackProject(t)
	tasksPath := fbPlanFileRel(repo, "TASKS.yaml")
	if err := os.WriteFile(tasksPath, []byte("[unterminated\n"), 0644); err != nil {
		t.Fatal(err)
	}

	err := executeWorkflowCommand(t, repo, "fold-back", "create",
		"--plan", "p1", "--task", "t1", "--observation", "corrupt", "--dry-run")
	if err == nil {
		t.Fatal("dry-run should surface the TASKS.yaml load error")
	}
	if !strings.Contains(err.Error(), "load tasks for plan p1") {
		t.Fatalf("want load-tasks error, got %v", err)
	}
	if got := foldBackArtifactPaths(repo); len(got) != 0 {
		t.Fatalf("failed dry-run wrote fold-back artifact(s): %v", got)
	}
}

// TestFoldBackCreate_DryRunNoSideEffects proves `fold-back create --dry-run`
// previews the routing decision and writes NOTHING to disk: no obs-*.md
// proposal, no fold-back artifact YAML, and the target TASKS.yaml/PLAN.yaml
// bytes stay identical. It covers the inline task-note route, the plan-summary
// route, and the --propose route.
func TestFoldBackCreate_DryRunNoSideEffects(t *testing.T) {
	t.Run("inline task-note route", fbDryRunInlineTaskNoteWritesNothing)
	t.Run("plan-summary route", fbDryRunPlanSummaryWritesNothing)
	t.Run("propose route", fbDryRunProposeWritesNothing)
	t.Run("writes without dry-run", fbDryRunWetInlineWrites)
	t.Run("global -n flag guards without local --dry-run", fbDryRunGlobalFlagGuards)
	t.Run("queues no journal event", fbDryRunQueuesNoJournal)
	t.Run("missing task surfaces error and writes nothing", fbDryRunMissingTaskErrors)
	t.Run("unreadable tasks file surfaces load error", fbDryRunUnreadableTasksErrors)
}

// fbDryRunUpdateVerb is the preview verb the dry-run render emits when a prior
// artifact already exists (priorExists → "update", first-time → "record").
const fbDryRunUpdateVerb = "would update"

// foldBackArtifactPaths globs the repo's fold-back staging dir for *.yaml
// (slug-scoped IDs are <slug>.yaml, defaults are fold-<ts>.yaml).
func foldBackArtifactPaths(repo string) []string {
	m, _ := filepath.Glob(filepath.Join(repo, ".agents", "active", "fold-back", "*.yaml"))
	return m
}

// seedFoldBack runs a real (non-dry-run) `fold-back create`, failing on error,
// to establish a prior artifact the update-route dry-run tests re-enter.
func seedFoldBack(t *testing.T, repo string, args ...string) {
	t.Helper()
	full := append([]string{"fold-back", "create"}, args...)
	if err := executeWorkflowCommand(t, repo, full...); err != nil {
		t.Fatalf("seed fold-back create %v: %v", args, err)
	}
}

// dryRunFoldBackCreate runs `fold-back create ... --dry-run` and returns stdout,
// failing on error (the happy-path preview cases).
func dryRunFoldBackCreate(t *testing.T, repo string, args ...string) string {
	t.Helper()
	full := append([]string{"fold-back", "create"}, args...)
	full = append(full, "--dry-run")
	return executeWorkflowCommandOutput(t, repo, full...)
}

// mustReadFileString reads path or fails, returning its bytes as a string.
func mustReadFileString(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// requireContainsAll fails unless out contains every wanted substring.
func requireContainsAll(t *testing.T, out string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Fatalf("output missing %q:\n%s", w, out)
		}
	}
}

// TestFoldBackCreate_DryRunUpdateRoutes covers the priorExists ("update")
// branches of planFoldBackRouting / runFoldBackUpsertDryRun. Re-entering
// `fold-back create` with an existing --slug routes as an update: the preview
// reports "would update", targets the same route the seeded prior owns, and
// still writes nothing (no new artifact, no TASKS.yaml/PLAN.yaml/proposal edit).
func TestFoldBackCreate_DryRunUpdateRoutes(t *testing.T) {
	tasksRel := filepath.Join(".agents", "workflow", "plans", "p1", "TASKS.yaml")
	planRel := filepath.Join(".agents", "workflow", "plans", "p1", "PLAN.yaml")

	t.Run("existing small task-note route previews update", func(t *testing.T) {
		repo := setupFoldBackProject(t)
		seedFoldBack(t, repo, "--plan", "p1", "--task", "t1", "--slug", "s1", "--observation", "first")
		if got := foldBackArtifactPaths(repo); len(got) != 1 {
			t.Fatalf("seed should write exactly one artifact, got %v", got)
		}
		tasksBefore := mustReadFileString(t, filepath.Join(repo, tasksRel))

		out := dryRunFoldBackCreate(t, repo,
			"--plan", "p1", "--task", "t1", "--slug", "s1", "--observation", "second")

		requireContainsAll(t, out, fbDryRunUpdateVerb, "task_note:p1/t1", "TASKS.yaml")
		if got := foldBackArtifactPaths(repo); len(got) != 1 {
			t.Fatalf("dry-run update changed artifact count: %v", got)
		}
		if after := mustReadFileString(t, filepath.Join(repo, tasksRel)); after != tasksBefore {
			t.Fatalf("dry-run update mutated TASKS.yaml:\nbefore=%q\nafter=%q", tasksBefore, after)
		}
	})

	t.Run("existing small plan-summary route previews update", func(t *testing.T) {
		repo := setupFoldBackProject(t)
		seedFoldBack(t, repo, "--plan", "p1", "--slug", "s2", "--observation", "first")
		planBefore := mustReadFileString(t, filepath.Join(repo, planRel))

		out := dryRunFoldBackCreate(t, repo,
			"--plan", "p1", "--slug", "s2", "--observation", "second")

		requireContainsAll(t, out, fbDryRunUpdateVerb, "plan_summary:p1", "PLAN.yaml")
		if after := mustReadFileString(t, filepath.Join(repo, planRel)); after != planBefore {
			t.Fatalf("dry-run update mutated PLAN.yaml:\nbefore=%q\nafter=%q", planBefore, after)
		}
	})

	t.Run("existing proposal route previews update", func(t *testing.T) {
		repo := setupFoldBackProject(t)
		agentsHome := t.TempDir()
		t.Setenv("AGENTS_HOME", agentsHome)
		seedFoldBack(t, repo, "--plan", "p1", "--task", "t1", "--slug", "s3", "--observation", "first", "--propose")
		if props, _ := filepath.Glob(filepath.Join(agentsHome, "proposals", "obs-s3.md")); len(props) != 1 {
			t.Fatalf("seed --propose should write one proposal, got %v", props)
		}

		// Re-enter WITHOUT --propose: an existing proposal-classified slug routes
		// as a proposal update, keeping the prior route rather than reclassifying.
		out := dryRunFoldBackCreate(t, repo,
			"--plan", "p1", "--slug", "s3", "--observation", "second")

		requireContainsAll(t, out, fbDryRunUpdateVerb, "proposal:obs-s3", "update proposal")
		if props, _ := filepath.Glob(filepath.Join(agentsHome, "proposals", "obs-*.md")); len(props) != 1 {
			t.Fatalf("dry-run update changed proposal files: %v", props)
		}
	})
}

// TestFoldBackCreate_DryRunJSONEnvelope covers the deps.Flags.JSON() branch of
// runFoldBackUpsertDryRun: the machine-readable foldBackDryRunResult envelope
// (dry_run + artifact routing + would_write targets), emitted with zero writes.
func TestFoldBackCreate_DryRunJSONEnvelope(t *testing.T) {
	repo := setupFoldBackProject(t)
	priorJSON := deps.Flags.JSON
	deps.Flags.JSON = func() bool { return true }
	t.Cleanup(func() { deps.Flags.JSON = priorJSON })

	out := dryRunFoldBackCreate(t, repo,
		"--plan", "p1", "--task", "t1", "--observation", "json obs")

	var res foldBackDryRunResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("dry-run JSON did not decode: %v\n%s", err, out)
	}
	if !res.DryRun {
		t.Fatalf("dry_run should be true: %+v", res)
	}
	if res.Artifact.Classification != "small" || res.Artifact.RoutedTo != "task_note:p1/t1" || res.Artifact.TaskID != "t1" {
		t.Fatalf("unexpected artifact routing in JSON envelope: %+v", res.Artifact)
	}
	if len(res.WouldWrite) != 2 {
		t.Fatalf("expected two would_write targets, got %v", res.WouldWrite)
	}
	requireContainsAll(t, strings.Join(res.WouldWrite, "\n"), "edit task note t1", "write fold-back artifact")
	if got := foldBackArtifactPaths(repo); len(got) != 0 {
		t.Fatalf("JSON dry-run wrote artifact(s): %v", got)
	}
}

// fbDryRunProposeSlugNamedProposal covers the slug-named proposal name
// (planFoldBackRouting propose branch when a --slug is set): the preview names
// obs-s4 without creating the proposals dir or any fold-back artifact.
func fbDryRunProposeSlugNamedProposal(t *testing.T) {
	repo := setupFoldBackProject(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	out := dryRunFoldBackCreate(t, repo,
		"--plan", "p1", "--task", "t1", "--slug", "s4", "--observation", "big", "--propose")

	requireContainsAll(t, out, "proposal:obs-s4", "create proposal", "obs-s4.md")
	if _, err := os.Stat(filepath.Join(agentsHome, "proposals")); !os.IsNotExist(err) {
		t.Fatalf("propose dry-run created proposals dir: %v", err)
	}
	if got := foldBackArtifactPaths(repo); len(got) != 0 {
		t.Fatalf("propose dry-run wrote artifact(s): %v", got)
	}
}

// fbDryRunMissingPlanErrors covers the read-only preamble error path: a missing
// plan surfaces the same "plan not found" error the write path would, before
// any preview render.
func fbDryRunMissingPlanErrors(t *testing.T) {
	repo := setupFoldBackProject(t)
	err := executeWorkflowCommand(t, repo, "fold-back", "create",
		"--plan", "ghost", "--task", "t1", "--observation", "x", "--dry-run")
	if err == nil {
		t.Fatal("dry-run against a missing plan should error")
	}
	if !strings.Contains(err.Error(), "plan ghost not found") {
		t.Fatalf("want plan-not-found error, got %v", err)
	}
}

// fbDryRunCorruptPriorArtifactErrors covers the shared read-only preamble
// (prepareFoldBackUpsert): a prior artifact that exists but fails to load must
// surface the load error, not a false-green preview — the write path would hit
// the same error.
func fbDryRunCorruptPriorArtifactErrors(t *testing.T) {
	repo := setupFoldBackProject(t)
	fbDir := filepath.Join(repo, ".agents", "active", "fold-back")
	if err := os.MkdirAll(fbDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fbDir, "s5.yaml"), []byte(": not: valid: yaml\n"), 0644); err != nil {
		t.Fatal(err)
	}

	err := executeWorkflowCommand(t, repo, "fold-back", "create",
		"--plan", "p1", "--task", "t1", "--slug", "s5", "--observation", "x", "--dry-run")
	if err == nil {
		t.Fatal("dry-run against a corrupt prior artifact should error")
	}
	if !strings.Contains(err.Error(), "load fold-back") {
		t.Fatalf("want load-fold-back error, got %v", err)
	}
}

// TestFoldBackCreate_DryRunProposeSlugAndMissingPlan covers three remaining
// dry-run branches: the slug-named proposal name, the read-only preamble error
// path (missing plan), and the corrupt prior-artifact load error.
func TestFoldBackCreate_DryRunProposeSlugAndMissingPlan(t *testing.T) {
	t.Run("propose with slug previews slug-named proposal", fbDryRunProposeSlugNamedProposal)
	t.Run("missing plan surfaces error before preview", fbDryRunMissingPlanErrors)
	t.Run("corrupt prior artifact surfaces load error", fbDryRunCorruptPriorArtifactErrors)
}

func TestFoldBackCreate_WriteError(t *testing.T) {
	repo := setupFoldBackProject(t)
	chdirRepo(t, repo)
	sentinel := errors.New("write boom")
	withWriteFileStub(t, func(string, []byte, os.FileMode) error { return sentinel })

	err := executeWorkflowCommand(t, repo, "fold-back", "create",
		"--plan", "p1", "--task", "t1", "--observation", "x")
	if err == nil {
		t.Fatal("expected write fault")
	}
}
