package workflow

import (
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

// TestFoldBackCreate_DryRunNoSideEffects proves `fold-back create --dry-run`
// previews the routing decision and writes NOTHING to disk: no obs-*.md
// proposal, no fold-back artifact YAML, and the target TASKS.yaml/PLAN.yaml
// bytes stay identical. It covers the inline task-note route, the plan-summary
// route, and the --propose route.
func TestFoldBackCreate_DryRunNoSideEffects(t *testing.T) {
	fbArtifacts := func(repo string) []string {
		m, _ := filepath.Glob(filepath.Join(repo, ".agents", "active", "fold-back", "*.yaml"))
		return m
	}

	t.Run("inline task-note route", func(t *testing.T) {
		repo := setupFoldBackProject(t)
		tasksPath := filepath.Join(repo, ".agents", "workflow", "plans", "p1", "TASKS.yaml")
		before, err := os.ReadFile(tasksPath)
		if err != nil {
			t.Fatal(err)
		}

		out := executeWorkflowCommandOutput(t, repo, "fold-back", "create",
			"--plan", "p1", "--task", "t1", "--observation", "dry obs", "--dry-run")

		if got := fbArtifacts(repo); len(got) != 0 {
			t.Fatalf("dry-run wrote fold-back artifact(s): %v", got)
		}
		after, err := os.ReadFile(tasksPath)
		if err != nil {
			t.Fatal(err)
		}
		if string(before) != string(after) {
			t.Fatalf("dry-run mutated TASKS.yaml:\nbefore=%q\nafter=%q", before, after)
		}
		for _, want := range []string{"dry-run", "task_note:p1/t1", "TASKS.yaml"} {
			if !strings.Contains(out, want) {
				t.Fatalf("output missing %q:\n%s", want, out)
			}
		}
	})

	t.Run("plan-summary route", func(t *testing.T) {
		repo := setupFoldBackProject(t)
		planPath := filepath.Join(repo, ".agents", "workflow", "plans", "p1", "PLAN.yaml")
		before, err := os.ReadFile(planPath)
		if err != nil {
			t.Fatal(err)
		}

		out := executeWorkflowCommandOutput(t, repo, "fold-back", "create",
			"--plan", "p1", "--observation", "plan dry obs", "--dry-run")

		if got := fbArtifacts(repo); len(got) != 0 {
			t.Fatalf("dry-run wrote fold-back artifact(s): %v", got)
		}
		after, err := os.ReadFile(planPath)
		if err != nil {
			t.Fatal(err)
		}
		if string(before) != string(after) {
			t.Fatalf("dry-run mutated PLAN.yaml:\nbefore=%q\nafter=%q", before, after)
		}
		for _, want := range []string{"dry-run", "plan_summary:p1", "PLAN.yaml"} {
			if !strings.Contains(out, want) {
				t.Fatalf("output missing %q:\n%s", want, out)
			}
		}
	})

	t.Run("propose route", func(t *testing.T) {
		repo := setupFoldBackProject(t)
		agentsHome := t.TempDir()
		t.Setenv("AGENTS_HOME", agentsHome)

		out := executeWorkflowCommandOutput(t, repo, "fold-back", "create",
			"--plan", "p1", "--task", "t1", "--observation", "big dry", "--propose", "--dry-run")

		if props, _ := filepath.Glob(filepath.Join(agentsHome, "proposals", "obs-*.md")); len(props) != 0 {
			t.Fatalf("dry-run wrote proposal file(s): %v", props)
		}
		if _, err := os.Stat(filepath.Join(agentsHome, "proposals")); !os.IsNotExist(err) {
			t.Fatalf("dry-run created the proposals dir (want none): %v", err)
		}
		if got := fbArtifacts(repo); len(got) != 0 {
			t.Fatalf("dry-run wrote fold-back artifact(s): %v", got)
		}
		for _, want := range []string{"dry-run", "proposal:obs-", "create proposal"} {
			if !strings.Contains(out, want) {
				t.Fatalf("output missing %q:\n%s", want, out)
			}
		}
	})

	// Companion positive case: the same inline invocation WITHOUT --dry-run
	// does mutate TASKS.yaml and persist the artifact, so the gate is the only
	// difference between preview and write.
	t.Run("writes without dry-run", func(t *testing.T) {
		repo := setupFoldBackProject(t)
		if err := executeWorkflowCommand(t, repo, "fold-back", "create",
			"--plan", "p1", "--task", "t1", "--observation", "wet obs"); err != nil {
			t.Fatal(err)
		}
		if got := fbArtifacts(repo); len(got) != 1 {
			t.Fatalf("expected one fold-back artifact without dry-run, got %v", got)
		}
		tf, err := loadCanonicalTasks(repo, "p1")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(tf.Tasks[0].Notes, "wet obs") {
			t.Fatalf("task notes not mutated: %q", tf.Tasks[0].Notes)
		}
	})
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
