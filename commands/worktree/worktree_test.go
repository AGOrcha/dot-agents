package worktree

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/spf13/cobra"

	"github.com/AGOrcha/dot-agents/internal/gitwt"
)

func TestNewCmdWiring(t *testing.T) {
	cmd := NewCmd()
	if cmd.Name() != "worktree" {
		t.Fatalf("Use=%q, want worktree", cmd.Name())
	}
	subs := map[string]*cobra.Command{}
	for _, c := range cmd.Commands() {
		subs[c.Name()] = c
	}
	for _, want := range []struct {
		name  string
		flags []string
	}{
		{"create", []string{"name", "path", "base-branch", "purpose", "parent-pr"}},
		{"merge-back", []string{"name", "onto"}},
	} {
		sub, ok := subs[want.name]
		if !ok {
			t.Fatalf("missing subcommand %q", want.name)
		}
		for _, fl := range want.flags {
			if sub.Flags().Lookup(fl) == nil {
				t.Fatalf("subcommand %q missing flag --%s", want.name, fl)
			}
		}
	}
}

// TestRequiredFlagsEnforced proves the create/merge-back required flags are
// enforced before RunE (so no git repo is even touched).
func TestRequiredFlagsEnforced(t *testing.T) {
	for _, args := range [][]string{{"create"}, {"merge-back"}} {
		cmd := NewCmd()
		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)
		cmd.SetArgs(args)
		err := cmd.Execute()
		if err == nil {
			t.Fatalf("%v with no flags succeeded, want required-flag error", args)
		}
		if !strings.Contains(err.Error(), "required") {
			t.Fatalf("%v error=%q, want a required-flag error", args, err)
		}
	}
}

func TestJSONOut(t *testing.T) {
	withFlag := &cobra.Command{Use: "x"}
	withFlag.Flags().Bool("json", false, "")
	if jsonOut(withFlag) {
		t.Fatal("jsonOut=true with default flag, want false")
	}
	if err := withFlag.Flags().Set("json", "true"); err != nil {
		t.Fatalf("set json: %v", err)
	}
	if !jsonOut(withFlag) {
		t.Fatal("jsonOut=false after --json set, want true")
	}
	if jsonOut(&cobra.Command{Use: "y"}) {
		t.Fatal("jsonOut=true when no json flag present, want false")
	}
}

// TestCreateMergeBackE2E drives the full CLI path: create records the base,
// merge-back reads that recorded base and fast-forwards the parent to the sub
// tip.
func TestCreateMergeBackE2E(t *testing.T) {
	repoDir, base := initRepo(t)
	setBranchRef(t, repoDir, "parent", base)
	t.Chdir(repoDir)

	wtParent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve wt parent: %v", err)
	}
	wtDir := filepath.Join(wtParent, "sub")

	out := execWorktree(t, "create", "--name", "sub", "--path", wtDir,
		"--base-branch", "parent", "--purpose", "wt3 slice", "--parent-pr", "409")
	if !strings.Contains(out, "recorded base") {
		t.Fatalf("create output=%q, want 'recorded base'", out)
	}
	if got := branchRef(t, repoDir, "sub"); got != base {
		t.Fatalf("sub branch at %s, want base %s", got, base)
	}

	subHead := advanceWorktree(t, repoDir, wtDir)
	if subHead == base {
		t.Fatal("sub-branch did not advance")
	}

	out = execWorktree(t, "merge-back", "--name", "sub", "--onto", "parent")
	if !strings.Contains(out, "fast-forwarded") {
		t.Fatalf("merge-back output=%q, want 'fast-forwarded'", out)
	}
	if got := branchRef(t, repoDir, "parent"); got != subHead {
		t.Fatalf("parent tip after merge-back=%s, want sub tip %s", got, subHead)
	}
}

// TestMergeBackStaleBaseE2E proves the CLI surfaces the stale-base guard: after
// the parent advances, merge-back errors instead of silently re-deriving.
func TestMergeBackStaleBaseE2E(t *testing.T) {
	repoDir, base := initRepo(t)
	setBranchRef(t, repoDir, "parent", base)
	t.Chdir(repoDir)

	wtParent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve wt parent: %v", err)
	}
	wtDir := filepath.Join(wtParent, "sub")

	execWorktree(t, "create", "--name", "sub", "--path", wtDir, "--base-branch", "parent")
	advanceWorktree(t, repoDir, wtDir)

	// Parent advances after the base was recorded.
	advanced := commitOnMain(t, repoDir, "adv.txt", "moved\n", "advance parent")
	setBranchRef(t, repoDir, "parent", advanced)

	cmd := NewCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"merge-back", "--name", "sub", "--onto", "parent"})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("merge-back onto an advanced parent succeeded, want stale-base error (out=%q)", buf.String())
	}
	if got := branchRef(t, repoDir, "parent"); got != advanced {
		t.Fatalf("parent moved to %s despite stale-base failure, want %s", got, advanced)
	}
}

// goCliAgentsRC is a minimal .agentsrc.json whose execution_profile routes the
// go-cli app_type to a full execution shape (topology + lenses + graph backend).
const goCliAgentsRC = `{
  "execution_profile": {
    "by_app_type": {
      "go-cli": {
        "topology": { "verifier_sequence": ["unit", "cli-runner"] },
        "lenses": { "lens_set": ["architecture-standards", "adversarial"], "lens_concurrency": "gated" },
        "graph_backend": "dotagents-builtin:graph/none@^1.0"
      }
    }
  }
}`

// docsGoCliAgentsRC routes both go-cli and docs to distinct execution shapes so
// a test can prove which app_type won resolution by the shape recorded.
const docsGoCliAgentsRC = `{
  "execution_profile": {
    "by_app_type": {
      "go-cli": {
        "topology": { "verifier_sequence": ["unit", "cli-runner"] },
        "lenses": { "lens_set": ["architecture-standards", "adversarial"], "lens_concurrency": "gated" },
        "graph_backend": "dotagents-builtin:graph/none@^1.0"
      },
      "docs": {
        "topology": { "verifier_sequence": ["docs-build"] },
        "lenses": { "lens_set": ["doc-quality"], "lens_concurrency": "serial" },
        "graph_backend": "dotagents-builtin:graph/none@^1.0"
      }
    }
  }
}`

// tasksGoCliYAML is a canonical TASKS.yaml with a decoy task (docs) ahead of the
// target (go-cli) so resolution must select by id, not by position.
const tasksGoCliYAML = `schema_version: 1
plan_id: plan-wt
tasks:
  - id: task-other
    title: Other slice
    app_type: docs
  - id: task-go
    title: Go slice
    app_type: go-cli
`

// planGoCliYAML is a canonical PLAN.yaml whose default_app_type is go-cli.
const planGoCliYAML = `id: plan-wt
title: WT plan
default_app_type: go-cli
`

// TestCreateResolvesAgentConfigE2E proves create loads the project's AgentsRC,
// resolves the app_type's execution shape, and records it (plus app_type +
// profile) onto the worktree's registry metadata, round-tripping via Get.
func TestCreateResolvesAgentConfigE2E(t *testing.T) {
	repoDir, base := initRepo(t)
	setBranchRef(t, repoDir, "parent", base)
	writeAgentsRC(t, repoDir, goCliAgentsRC)
	t.Chdir(repoDir)

	wtParent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve wt parent: %v", err)
	}
	wtDir := filepath.Join(wtParent, "sub")

	out := execWorktree(t, "create", "--name", "sub", "--path", wtDir,
		"--base-branch", "parent", "--app-type", "go-cli", "--profile", "loop-worker")
	for _, want := range []string{"go-cli", "loop-worker", "cli-runner", "gated"} {
		if !strings.Contains(out, want) {
			t.Fatalf("create output=%q, want to surface %q", out, want)
		}
	}

	meta := getMetadata(t, repoDir, "sub")
	if meta.AppType != "go-cli" || meta.Profile != "loop-worker" {
		t.Fatalf("app_type/profile=%q/%q, want go-cli/loop-worker", meta.AppType, meta.Profile)
	}
	if !reflect.DeepEqual(meta.VerifierSequence, []string{"unit", "cli-runner"}) {
		t.Fatalf("verifier_sequence=%v, want [unit cli-runner]", meta.VerifierSequence)
	}
	if !reflect.DeepEqual(meta.LensSet, []string{"architecture-standards", "adversarial"}) {
		t.Fatalf("lens_set=%v, want [architecture-standards adversarial]", meta.LensSet)
	}
	if meta.LensConcurrency != "gated" {
		t.Fatalf("lens_concurrency=%q, want gated", meta.LensConcurrency)
	}
	if meta.GraphBackend != "dotagents-builtin:graph/none@^1.0" {
		t.Fatalf("graph_backend=%q, want dotagents-builtin:graph/none@^1.0", meta.GraphBackend)
	}
}

// TestCreateUnknownAppTypeWarnsE2E proves an app_type with no execution_profile
// entry warns loudly (a typo invoked directly must be visible) yet still records
// app_type/profile with empty resolved fields and never errors.
func TestCreateUnknownAppTypeWarnsE2E(t *testing.T) {
	repoDir, base := initRepo(t)
	setBranchRef(t, repoDir, "parent", base)
	writeAgentsRC(t, repoDir, goCliAgentsRC) // execution_profile present, but not for this app_type
	t.Chdir(repoDir)

	wtParent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve wt parent: %v", err)
	}
	wtDir := filepath.Join(wtParent, "sub")

	out := execWorktree(t, "create", "--name", "sub", "--path", wtDir,
		"--base-branch", "parent", "--app-type", "typo-cli", "--profile", "loop-worker")
	if !strings.Contains(out, `warning: no execution_profile entry for app_type "typo-cli"`) {
		t.Fatalf("create output=%q, want unknown-app_type warning", out)
	}

	meta := getMetadata(t, repoDir, "sub")
	if meta.AppType != "typo-cli" || meta.Profile != "loop-worker" {
		t.Fatalf("app_type/profile=%q/%q, want typo-cli/loop-worker recorded", meta.AppType, meta.Profile)
	}
	if len(meta.VerifierSequence) != 0 || len(meta.LensSet) != 0 || meta.LensConcurrency != "" || meta.GraphBackend != "" {
		t.Fatalf("resolved fields should be empty for unknown app_type, got %+v", meta)
	}
}

// TestCreateResolvesTaskAppTypeE2E covers acceptance: --plan P --task T where
// TASKS.yaml task T has app_type=go-cli resolves go-cli and records the go-cli
// execution shape (verifier_sequence/lens_set/graph_backend) into Metadata.
// Profile defaults to loop-worker (no --profile passed).
func TestCreateResolvesTaskAppTypeE2E(t *testing.T) {
	repoDir, base := initRepo(t)
	setBranchRef(t, repoDir, "parent", base)
	writeAgentsRC(t, repoDir, goCliAgentsRC)
	writeTasksYAML(t, repoDir, "plan-wt", tasksGoCliYAML)
	t.Chdir(repoDir)

	wtParent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve wt parent: %v", err)
	}
	wtDir := filepath.Join(wtParent, "sub")

	out := execWorktree(t, "create", "--name", "sub", "--path", wtDir,
		"--base-branch", "parent", "--plan", "plan-wt", "--task", "task-go")
	for _, want := range []string{"go-cli", "cli-runner", "gated"} {
		if !strings.Contains(out, want) {
			t.Fatalf("create output=%q, want to surface %q", out, want)
		}
	}

	meta := getMetadata(t, repoDir, "sub")
	if meta.AppType != "go-cli" {
		t.Fatalf("app_type=%q, want go-cli resolved from task", meta.AppType)
	}
	if meta.Profile != "loop-worker" {
		t.Fatalf("profile=%q, want loop-worker default", meta.Profile)
	}
	if !reflect.DeepEqual(meta.VerifierSequence, []string{"unit", "cli-runner"}) {
		t.Fatalf("verifier_sequence=%v, want [unit cli-runner]", meta.VerifierSequence)
	}
	if !reflect.DeepEqual(meta.LensSet, []string{"architecture-standards", "adversarial"}) {
		t.Fatalf("lens_set=%v, want [architecture-standards adversarial]", meta.LensSet)
	}
	if meta.LensConcurrency != "gated" {
		t.Fatalf("lens_concurrency=%q, want gated", meta.LensConcurrency)
	}
	if meta.GraphBackend != "dotagents-builtin:graph/none@^1.0" {
		t.Fatalf("graph_backend=%q, want dotagents-builtin:graph/none@^1.0", meta.GraphBackend)
	}
}

// TestCreateAppTypeFlagOverridesTaskE2E covers acceptance: an explicit
// --app-type wins over the --task's app_type. Task says go-cli, flag says docs,
// so the docs execution shape is what gets recorded.
func TestCreateAppTypeFlagOverridesTaskE2E(t *testing.T) {
	repoDir, base := initRepo(t)
	setBranchRef(t, repoDir, "parent", base)
	writeAgentsRC(t, repoDir, docsGoCliAgentsRC)
	writeTasksYAML(t, repoDir, "plan-wt", tasksGoCliYAML)
	t.Chdir(repoDir)

	wtParent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve wt parent: %v", err)
	}
	wtDir := filepath.Join(wtParent, "sub")

	execWorktree(t, "create", "--name", "sub", "--path", wtDir,
		"--base-branch", "parent", "--app-type", "docs", "--plan", "plan-wt", "--task", "task-go")

	meta := getMetadata(t, repoDir, "sub")
	if meta.AppType != "docs" {
		t.Fatalf("app_type=%q, want docs (explicit --app-type flag wins over task)", meta.AppType)
	}
	if !reflect.DeepEqual(meta.VerifierSequence, []string{"docs-build"}) {
		t.Fatalf("verifier_sequence=%v, want [docs-build] (docs shape, not go-cli's)", meta.VerifierSequence)
	}
}

// TestCreateResolvesPlanDefaultAppTypeE2E covers acceptance: --plan P with no
// --task resolves app_type from PLAN.yaml default_app_type.
func TestCreateResolvesPlanDefaultAppTypeE2E(t *testing.T) {
	repoDir, base := initRepo(t)
	setBranchRef(t, repoDir, "parent", base)
	writeAgentsRC(t, repoDir, goCliAgentsRC)
	writePlanYAML(t, repoDir, "plan-wt", planGoCliYAML)
	t.Chdir(repoDir)

	wtParent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve wt parent: %v", err)
	}
	wtDir := filepath.Join(wtParent, "sub")

	execWorktree(t, "create", "--name", "sub", "--path", wtDir,
		"--base-branch", "parent", "--plan", "plan-wt")

	meta := getMetadata(t, repoDir, "sub")
	if meta.AppType != "go-cli" {
		t.Fatalf("app_type=%q, want go-cli resolved from plan default_app_type", meta.AppType)
	}
	if !reflect.DeepEqual(meta.VerifierSequence, []string{"unit", "cli-runner"}) {
		t.Fatalf("verifier_sequence=%v, want [unit cli-runner]", meta.VerifierSequence)
	}
}

// TestCreateTaskWithoutPlanWarnsE2E covers acceptance: --task without --plan
// cannot locate a TASKS.yaml, so it warns clearly that --plan is required and
// proceeds with an empty resolved app_type (the no-app_type-resolved warning
// then also fires). An explicit --profile override is recorded.
func TestCreateTaskWithoutPlanWarnsE2E(t *testing.T) {
	repoDir, base := initRepo(t)
	setBranchRef(t, repoDir, "parent", base)
	writeAgentsRC(t, repoDir, goCliAgentsRC)
	t.Chdir(repoDir)

	wtParent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve wt parent: %v", err)
	}
	wtDir := filepath.Join(wtParent, "sub")

	out := execWorktree(t, "create", "--name", "sub", "--path", wtDir,
		"--base-branch", "parent", "--task", "task-go", "--profile", "my-worker")
	if !strings.Contains(out, "--task") || !strings.Contains(out, "needs --plan") {
		t.Fatalf("create output=%q, want a clear --plan-required warning", out)
	}
	if !strings.Contains(out, "warning: no app_type resolved") {
		t.Fatalf("create output=%q, want no-app_type-resolved warning after fall-through", out)
	}

	meta := getMetadata(t, repoDir, "sub")
	if meta.AppType != "" {
		t.Fatalf("app_type=%q, want empty (task unresolvable without plan)", meta.AppType)
	}
	if meta.Profile != "my-worker" {
		t.Fatalf("profile=%q, want my-worker (explicit --profile override)", meta.Profile)
	}
}

// TestCreateNoAppTypeResolvedWarnsE2E covers acceptance: with no
// --app-type/--plan/--task nothing resolves, the "no app_type resolved" warning
// fires, the resolved agent-config fields stay empty (no error), and --profile
// defaults to loop-worker.
func TestCreateNoAppTypeResolvedWarnsE2E(t *testing.T) {
	repoDir, base := initRepo(t)
	setBranchRef(t, repoDir, "parent", base)
	writeAgentsRC(t, repoDir, goCliAgentsRC)
	t.Chdir(repoDir)

	wtParent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve wt parent: %v", err)
	}
	wtDir := filepath.Join(wtParent, "sub")

	out := execWorktree(t, "create", "--name", "sub", "--path", wtDir, "--base-branch", "parent")
	if !strings.Contains(out, "warning: no app_type resolved") {
		t.Fatalf("create output=%q, want no-app_type-resolved warning", out)
	}

	meta := getMetadata(t, repoDir, "sub")
	if meta.AppType != "" {
		t.Fatalf("app_type=%q, want empty (nothing resolved)", meta.AppType)
	}
	if meta.Profile != "loop-worker" {
		t.Fatalf("profile=%q, want loop-worker default", meta.Profile)
	}
	if len(meta.VerifierSequence) != 0 || len(meta.LensSet) != 0 || meta.LensConcurrency != "" || meta.GraphBackend != "" {
		t.Fatalf("resolved fields should be empty, got %+v", meta)
	}
}

// TestRenderCreateSurfacesAgentConfig covers requirement 4: the resolved config
// appears in both the human render and the --json output.
func TestRenderCreateSurfacesAgentConfig(t *testing.T) {
	res := gitwt.CreateResult{
		Base: plumbing.ZeroHash,
		Metadata: gitwt.Metadata{
			Name:             "sub",
			AppType:          "go-cli",
			Profile:          "loop-worker",
			VerifierSequence: []string{"unit", "cli-runner"},
			LensSet:          []string{"architecture-standards"},
			LensConcurrency:  "gated",
			GraphBackend:     "dotagents-builtin:graph/none@^1.0",
		},
	}
	var human bytes.Buffer
	if err := renderCreate(&human, false, res); err != nil {
		t.Fatalf("renderCreate human: %v", err)
	}
	for _, want := range []string{"go-cli", "loop-worker", "cli-runner", "gated"} {
		if !strings.Contains(human.String(), want) {
			t.Errorf("human render missing %q: %s", want, human.String())
		}
	}
	var js bytes.Buffer
	if err := renderCreate(&js, true, res); err != nil {
		t.Fatalf("renderCreate json: %v", err)
	}
	for _, want := range []string{`"app_type": "go-cli"`, `"profile": "loop-worker"`,
		`"verifier_sequence"`, `"lens_concurrency": "gated"`, `"graph_backend"`} {
		if !strings.Contains(js.String(), want) {
			t.Errorf("json render missing %q: %s", want, js.String())
		}
	}
}

// --- helpers ---

func initRepo(t *testing.T) (string, plumbing.Hash) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve temp dir: %v", err)
	}
	dir := filepath.Join(root, "repo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("init repo: %v", err)
	}
	cfg, err := repo.Config()
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	cfg.User.Name = "Test"
	cfg.User.Email = "t@example.com"
	if err := repo.SetConfig(cfg); err != nil {
		t.Fatalf("set config: %v", err)
	}
	h := commitOnMain(t, dir, "README.md", "hello\n", "initial")
	return dir, h
}

func commitOnMain(t *testing.T, repoDir, name, content, msg string) plumbing.Hash {
	t.Helper()
	repo, err := git.PlainOpen(repoDir)
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	if _, err := wt.Add(name); err != nil {
		t.Fatalf("add %s: %v", name, err)
	}
	h, err := wt.Commit(msg, &git.CommitOptions{
		Author: &object.Signature{Name: "Test", Email: "t@example.com", When: time.Now()},
	})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	return h
}

func setBranchRef(t *testing.T, repoDir, name string, h plumbing.Hash) {
	t.Helper()
	repo, err := git.PlainOpen(repoDir)
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	ref := plumbing.NewHashReference(plumbing.NewBranchReferenceName(name), h)
	if err := repo.Storer.SetReference(ref); err != nil {
		t.Fatalf("set branch %s: %v", name, err)
	}
}

func branchRef(t *testing.T, repoDir, name string) plumbing.Hash {
	t.Helper()
	repo, err := git.PlainOpen(repoDir)
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	ref, err := repo.Reference(plumbing.NewBranchReferenceName(name), true)
	if err != nil {
		t.Fatalf("resolve branch %s: %v", name, err)
	}
	return ref.Hash()
}

func advanceWorktree(t *testing.T, repoDir, wtDir string) plumbing.Hash {
	t.Helper()
	mgr, err := gitwt.NewManager(repoDir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	wt, err := mgr.Open(wtDir)
	if err != nil {
		t.Fatalf("Open worktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wtDir, "work.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatalf("write work.txt: %v", err)
	}
	if err := wt.Stage("work.txt"); err != nil {
		t.Fatalf("Stage: %v", err)
	}
	h, err := wt.Commit("wip", &gitwt.CommitOptions{AuthorName: "W", AuthorEmail: "w@x"})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	return h
}

func execWorktree(t *testing.T, args ...string) string {
	t.Helper()
	cmd := NewCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute %v: %v (out=%q)", args, err, buf.String())
	}
	return buf.String()
}

func writeAgentsRC(t *testing.T, repoDir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repoDir, ".agentsrc.json"), []byte(content), 0o644); err != nil {
		t.Fatalf("write .agentsrc.json: %v", err)
	}
}

func writeTasksYAML(t *testing.T, repoDir, plan, body string) {
	t.Helper()
	writePlanFile(t, repoDir, plan, "TASKS.yaml", body)
}

func writePlanYAML(t *testing.T, repoDir, plan, body string) {
	t.Helper()
	writePlanFile(t, repoDir, plan, "PLAN.yaml", body)
}

func writePlanFile(t *testing.T, repoDir, plan, name, body string) {
	t.Helper()
	dir := filepath.Join(repoDir, ".agents", "workflow", "plans", plan)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir plan dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func getMetadata(t *testing.T, repoDir, name string) gitwt.Metadata {
	t.Helper()
	mgr, err := gitwt.NewManager(repoDir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	reg, err := gitwt.NewRegistry(mgr, registryIdleTTL)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	meta, err := reg.Get(name)
	if err != nil {
		t.Fatalf("registry Get %q: %v", name, err)
	}
	return meta
}
