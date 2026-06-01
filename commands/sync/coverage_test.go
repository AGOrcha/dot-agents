package sync

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/config"
	"golang.org/x/sys/execabs"
)

// TestPrintGitSourcesHint smoke-covers the trivial hint printer.
func TestPrintGitSourcesHint(t *testing.T) {
	printGitSourcesHint()
}

// TestHasGitManifests_NoSources writes a config with a project that has no git
// sources, expecting hasGitManifests() to return false.
func TestHasGitManifests_NoSources(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, "agents"))
	if err := os.MkdirAll(filepath.Join(tmp, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}

	projectPath := filepath.Join(tmp, "proj")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatal(err)
	}
	rc := &config.AgentsRC{
		Version: 1,
		Project: "proj",
		Sources: []config.Source{{Type: "local"}},
	}
	if err := rc.Save(projectPath); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Version:  1,
		Projects: map[string]config.Project{},
		Agents:   map[string]config.Agent{},
	}
	cfg.AddProject("proj", projectPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	if hasGitManifests() {
		t.Error("hasGitManifests = true with only local source; want false")
	}
}

// TestHasGitManifests_WithGitSource writes a project manifest with a git
// source, expecting hasGitManifests() to return true.
func TestHasGitManifests_WithGitSource(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, "agents"))
	if err := os.MkdirAll(filepath.Join(tmp, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}

	projectPath := filepath.Join(tmp, "proj")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatal(err)
	}
	rc := &config.AgentsRC{
		Version: 1,
		Project: "proj",
		Sources: []config.Source{{Type: "git", URL: "https://example.com/x.git"}},
	}
	if err := rc.Save(projectPath); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Version:  1,
		Projects: map[string]config.Project{},
		Agents:   map[string]config.Agent{},
	}
	cfg.AddProject("proj", projectPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	if !hasGitManifests() {
		t.Error("hasGitManifests = false with git source; want true")
	}
}

// TestHasGitManifests_SkipsMissingPathsAndBadManifests covers the loop
// branches that skip projects whose path is empty / fail to load.
func TestHasGitManifests_SkipsMissingPathsAndBadManifests(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, "agents"))
	if err := os.MkdirAll(filepath.Join(tmp, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Version:  1,
		Projects: map[string]config.Project{},
		Agents:   map[string]config.Agent{},
	}
	// Project with no on-disk manifest -> LoadAgentsRC errors.
	cfg.AddProject("missing", filepath.Join(tmp, "missing"))
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	if hasGitManifests() {
		t.Error("hasGitManifests should be false when manifests fail to load")
	}
}

// TestPostPullRefresh_AutoYesWithManifests covers the Yes=true branch and
// the trailing hasManifests=true printGitSourcesHint call.
func TestPostPullRefresh_AutoYesWithManifests(t *testing.T) {
	called := 0
	deps := Deps{Flags: GlobalFlags{Yes: true}, RunRefresh: func(string) error {
		called++
		return nil
	}}
	if err := postPullRefresh(deps, true); err != nil {
		t.Fatalf("postPullRefresh: %v", err)
	}
	if called != 1 {
		t.Errorf("RunRefresh called %d times; want 1", called)
	}
}

// TestPostPullRefresh_RefreshErrorBubblesUp ensures the RunRefresh error
// is returned.
func TestPostPullRefresh_RefreshErrorBubblesUp(t *testing.T) {
	want := errors.New("refresh boom")
	deps := Deps{Flags: GlobalFlags{Yes: true}, RunRefresh: func(string) error {
		return want
	}}
	err := postPullRefresh(deps, false)
	if !errors.Is(err, want) {
		t.Errorf("postPullRefresh err = %v; want %v", err, want)
	}
}

// TestPostPullRefresh_DeclineSkipsRefresh covers the !ui.Confirm branch
// where the user declines the refresh prompt. We feed an "n\n" line into
// stdin via a pipe so the prompt receives a real negative answer.
func TestPostPullRefresh_DeclineSkipsRefresh(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteString("n\n"); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	oldStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = oldStdin
		_ = r.Close()
	})

	called := 0
	deps := Deps{Flags: GlobalFlags{}, RunRefresh: func(string) error {
		called++
		return nil
	}}
	if err := postPullRefresh(deps, true); err != nil {
		t.Fatalf("postPullRefresh: %v", err)
	}
	if called != 0 {
		t.Errorf("RunRefresh called %d times; want 0 after decline", called)
	}
}

// TestPostPullRefresh_AcceptedPromptCallsRefresh covers the prompted-accept
// branch (stdin returns "y").
func TestPostPullRefresh_AcceptedPromptCallsRefresh(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteString("y\n"); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	oldStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = oldStdin
		_ = r.Close()
	})

	called := 0
	deps := Deps{Flags: GlobalFlags{}, RunRefresh: func(string) error {
		called++
		return nil
	}}
	if err := postPullRefresh(deps, false); err != nil {
		t.Fatalf("postPullRefresh: %v", err)
	}
	if called != 1 {
		t.Errorf("RunRefresh called %d times; want 1 after accept", called)
	}
}

// TestRunSyncPush_DeclinedAtConfirm covers the !ui.Confirm branch where the
// user declines the push.
func TestRunSyncPush_DeclinedAtConfirm(t *testing.T) {
	agentsHome, _ := setupPushPullPair(t)
	if err := os.WriteFile(filepath.Join(agentsHome, "decl.txt"), []byte("d"), 0o644); err != nil {
		t.Fatal(err)
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	oldStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = oldStdin
		_ = r.Close()
	})

	deps := Deps{Flags: GlobalFlags{}, RunRefresh: func(string) error { return nil }}
	if err := runSyncPush(deps, "decline"); err != nil {
		t.Fatalf("declined push: %v", err)
	}
	// The commit should still have happened locally.
	out, _ := execabs.Command("git", "-C", agentsHome, "log", "--oneline").CombinedOutput()
	if string(out) == "" {
		t.Error("expected local log after declined push")
	}
}

// TestRunSyncPush_PushErrorPropagates points push at a non-existent remote
// so `git push` fails. runSyncPush should wrap the error.
func TestRunSyncPush_PushErrorPropagates(t *testing.T) {
	agentsHome := setupAgentsHomeRepo(t)
	initEmptyRepo(t, agentsHome)
	// Add a remote that doesn't exist so push fails.
	runGit(t, agentsHome, "remote", "add", "origin", filepath.Join(t.TempDir(), "does-not-exist.git"))

	if err := os.WriteFile(filepath.Join(agentsHome, "p.txt"), []byte("p"), 0o644); err != nil {
		t.Fatal(err)
	}

	deps := Deps{Flags: GlobalFlags{Yes: true}, RunRefresh: func(string) error { return nil }}
	if err := runSyncPush(deps, "bad-remote"); err == nil {
		t.Fatal("expected push error when remote is missing")
	}
}

// TestPull_DryRunRejectedViaRunE covers the early-return error branch when
// --dry-run is passed to pull.
func TestPull_DryRunRejectedViaRunE(t *testing.T) {
	deps := Deps{Flags: GlobalFlags{DryRun: true}, RunRefresh: func(string) error { return nil }}
	cmd := newPullCmd(deps)
	if err := cmd.RunE(cmd, nil); err == nil {
		t.Fatal("expected dry-run error from pull")
	}
}

// TestNewLogCmd_ErrorOnNonRepo runs the log command against a non-git dir to
// exercise the error-return branch.
func TestNewLogCmd_ErrorOnNonRepo(t *testing.T) {
	requireGit(t)
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", tmp)
	cmd := newLogCmd()
	if err := cmd.RunE(cmd, nil); err == nil {
		t.Error("expected git log error on non-repo dir")
	}
}

// TestInitSyncRepo_GitInitErrorReports drives the `git init` error path by
// pointing AGENTS_HOME at a path that cannot exist (a file).
func TestInitSyncRepo_GitInitErrorReports(t *testing.T) {
	requireGit(t)
	tmp := t.TempDir()
	// Create a file at the path we'll try to init — git refuses.
	target := filepath.Join(tmp, "notdir")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := initSyncRepo(target); err == nil {
		t.Error("expected git init error on file path")
	}
}

// TestInitSyncRepo_PreservesExistingGitignore covers the branch where the
// .gitignore already exists (so the write path is skipped).
func TestInitSyncRepo_PreservesExistingGitignore(t *testing.T) {
	agentsHome := setupAgentsHomeRepo(t)
	gi := filepath.Join(agentsHome, ".gitignore")
	if err := os.WriteFile(gi, []byte("custom\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := initSyncRepo(agentsHome); err != nil {
		t.Fatalf("initSyncRepo: %v", err)
	}
	got, err := os.ReadFile(gi)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "custom\n" {
		t.Errorf("existing .gitignore overwritten: %q", got)
	}
}

// TestReportExistingSyncRepo_LongRemoteListTruncates covers the i>=2 break
// branch when `git remote -v` returns more than 2 lines.
func TestReportExistingSyncRepo_LongRemoteListTruncates(t *testing.T) {
	agentsHome := setupAgentsHomeRepo(t)
	initEmptyRepo(t, agentsHome)
	// Two remotes -> `remote -v` yields 4 lines (fetch+push each).
	tmp := t.TempDir()
	r1 := filepath.Join(tmp, "r1.git")
	r2 := filepath.Join(tmp, "r2.git")
	for _, r := range []string{r1, r2} {
		if err := os.MkdirAll(r, 0o755); err != nil {
			t.Fatal(err)
		}
		runGit(t, r, "init", "--bare")
	}
	runGit(t, agentsHome, "remote", "add", "origin", r1)
	runGit(t, agentsHome, "remote", "add", "second", r2)

	if err := reportExistingSyncRepo(agentsHome); err != nil {
		t.Fatalf("reportExistingSyncRepo: %v", err)
	}
}

// TestPrintAheadBehind_NoOriginHeadShortCircuits covers the branch where
// `git rev-list` returns malformed/empty output.
func TestPrintAheadBehind_NoOriginHeadShortCircuits(t *testing.T) {
	agentsHome := setupAgentsHomeRepo(t)
	initEmptyRepo(t, agentsHome)
	// No remote -> rev-list will fail and return empty; len(ab) != 2 path.
	printAheadBehind(agentsHome, true)
}
