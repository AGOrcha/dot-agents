package sync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/execabs"
)

// setupPushPullPair creates a bare remote and a working agentsHome that
// has the remote configured + an initial commit pushed.
func setupPushPullPair(t *testing.T) (agentsHome, remote string) {
	t.Helper()
	agentsHome = setupAgentsHomeRepo(t)
	initEmptyRepo(t, agentsHome)

	tmp := t.TempDir()
	remote = filepath.Join(tmp, "remote.git")
	if err := os.MkdirAll(remote, 0755); err != nil {
		t.Fatal(err)
	}
	runGit(t, remote, "init", "--bare")
	runGit(t, agentsHome, "remote", "add", "origin", remote)
	// Push the seed commit so origin/HEAD exists for ahead/behind queries.
	runGit(t, agentsHome, "push", "-u", "origin", "HEAD")
	runGit(t, agentsHome, "remote", "set-head", "origin", "--auto")
	return agentsHome, remote
}

func TestRunSyncPush_DryRun(t *testing.T) {
	agentsHome := setupAgentsHomeRepo(t)
	initEmptyRepo(t, agentsHome)

	deps := Deps{Flags: GlobalFlags{DryRun: true}, RunRefresh: func(string) error { return nil }}
	if err := runSyncPush(deps, "test-msg"); err != nil {
		t.Errorf("dry-run push: %v", err)
	}
}

func TestRunSyncPush_NewFileRoundTrip(t *testing.T) {
	agentsHome, remote := setupPushPullPair(t)

	if err := os.WriteFile(filepath.Join(agentsHome, "round.txt"), []byte("rt\n"), 0644); err != nil {
		t.Fatal(err)
	}

	deps := Deps{Flags: GlobalFlags{Yes: true}, RunRefresh: func(string) error { return nil }}
	if err := runSyncPush(deps, "round-trip"); err != nil {
		t.Fatalf("push: %v", err)
	}

	out, err := execabs.Command("git", "-C", remote, "log", "--oneline").CombinedOutput()
	if err != nil {
		t.Fatalf("git log on bare: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "round-trip") {
		t.Errorf("bare remote missing pushed commit:\n%s", out)
	}
}

func TestRunSyncPush_DefaultMessage(t *testing.T) {
	agentsHome, _ := setupPushPullPair(t)
	if err := os.WriteFile(filepath.Join(agentsHome, "d.txt"), []byte("d"), 0644); err != nil {
		t.Fatal(err)
	}

	deps := Deps{Flags: GlobalFlags{Yes: true}, RunRefresh: func(string) error { return nil }}
	if err := runSyncPush(deps, ""); err != nil {
		t.Fatalf("push: %v", err)
	}
}

// TestRunSyncPush_CommitFailureAbortsBeforePush ensures a real (non-"nothing
// to commit") git commit failure surfaces as an error and the command never
// reaches the confirm/push step. Before this fix, stageAndCommit discarded
// both the `git add -A` and `git commit` errors, so `da sync push` would
// report success (and could push a stale HEAD) even when the new changes
// were never actually committed.
func TestRunSyncPush_CommitFailureAbortsBeforePush(t *testing.T) {
	agentsHome, remote := setupPushPullPair(t)
	if err := os.WriteFile(filepath.Join(agentsHome, "should-not-push.txt"), []byte("x\n"), 0644); err != nil {
		t.Fatal(err)
	}

	beforeHead, err := execabs.Command("git", "-C", agentsHome, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}

	stripGitIdentity(t)

	deps := Deps{Flags: GlobalFlags{Yes: true}, RunRefresh: func(string) error { return nil }}
	pushErr := runSyncPush(deps, "should-not-push")
	if pushErr == nil {
		t.Skip("git accepted the commit even with no identity (likely a CI git build with implicit defaults); cannot exercise the error path here")
	}
	if strings.Contains(pushErr.Error(), "nothing to commit") {
		t.Fatalf("expected a real commit failure, got the nothing-to-commit sentinel: %v", pushErr)
	}

	afterHead, err := execabs.Command("git", "-C", agentsHome, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	if string(beforeHead) != string(afterHead) {
		t.Errorf("HEAD advanced despite the commit failure: before=%s after=%s", beforeHead, afterHead)
	}

	remoteOut, err := execabs.Command("git", "-C", remote, "log", "--oneline").CombinedOutput()
	if err != nil {
		t.Fatalf("git log on bare: %v\n%s", err, remoteOut)
	}
	if strings.Contains(string(remoteOut), "should-not-push") {
		t.Errorf("push step ran despite the aborted commit: remote log contains the message:\n%s", remoteOut)
	}
}

func TestRunSyncPush_NewPushCmdMetadata(t *testing.T) {
	deps := Deps{Flags: GlobalFlags{}, RunRefresh: func(string) error { return nil }}
	cmd := newPushCmd(deps)
	if cmd.Use != "push" {
		t.Errorf("Use = %q", cmd.Use)
	}
	if cmd.Flags().Lookup("message") == nil {
		t.Error("push command missing -m/--message flag")
	}
}

// printPendingPushCommits when there is an unpushed commit.
func TestPrintPendingPushCommits_WithPending(t *testing.T) {
	agentsHome, _ := setupPushPullPair(t)
	// Create a local commit not pushed.
	if err := os.WriteFile(filepath.Join(agentsHome, "pending.txt"), []byte("p"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, agentsHome, "add", ".")
	runGit(t, agentsHome, "commit", "-m", "pending-local")
	// Should not panic — output is to stdout.
	printPendingPushCommits(agentsHome)
}

func TestStageAndCommit_NothingToCommit(t *testing.T) {
	agentsHome := setupAgentsHomeRepo(t)
	initEmptyRepo(t, agentsHome)
	// No files staged — the "nothing to commit" sentinel must be treated as
	// non-fatal, not surfaced as an error.
	if err := stageAndCommit(agentsHome, "no-changes"); err != nil {
		t.Fatalf("stageAndCommit: %v", err)
	}
}

// TestStageAndCommit_CommitFailureSurfacesError ensures a real (non-"nothing
// to commit") git commit failure is returned instead of silently discarded.
// Before this fix, stageAndCommit had no error return at all.
func TestStageAndCommit_CommitFailureSurfacesError(t *testing.T) {
	agentsHome := setupAgentsHomeRepo(t)
	initEmptyRepo(t, agentsHome)
	if err := os.WriteFile(filepath.Join(agentsHome, "change.txt"), []byte("x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	stripGitIdentity(t)

	err := stageAndCommit(agentsHome, "should-fail")
	if err == nil {
		t.Skip("git accepted the commit even with no identity (likely a CI git build with implicit defaults); cannot exercise the error path here")
	}
	if strings.Contains(err.Error(), "nothing to commit") {
		t.Fatalf("expected a real commit failure, got the nothing-to-commit sentinel: %v", err)
	}
}

// TestStageAndCommit_GitAddFailureSurfacesError ensures the previously
// discarded `git add -A` error is now returned before git commit even runs.
func TestStageAndCommit_GitAddFailureSurfacesError(t *testing.T) {
	agentsHome := setupAgentsHomeRepo(t)
	initEmptyRepo(t, agentsHome)
	if err := os.WriteFile(filepath.Join(agentsHome, "change.txt"), []byte("x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Corrupt the index so `git add -A` fails before it ever reaches commit.
	if err := os.WriteFile(filepath.Join(agentsHome, ".git", "index"), []byte("not-a-real-index"), 0644); err != nil {
		t.Fatal(err)
	}

	err := stageAndCommit(agentsHome, "should-fail")
	if err == nil {
		t.Fatal("expected stageAndCommit to surface the git add failure, got nil")
	}
	if !strings.Contains(err.Error(), "git add") {
		t.Errorf("expected a git add error, got %q", err)
	}
}

// ── pull subcommand ─────────────────────────────────────────────────

func TestPull_RoundTrip(t *testing.T) {
	agentsHome, remote := setupPushPullPair(t)

	// Make a parallel clone, commit and push, then verify the original can pull.
	tmp := t.TempDir()
	other := filepath.Join(tmp, "other")
	runGit(t, tmp, "clone", remote, "other")
	if err := os.WriteFile(filepath.Join(other, "from-other.txt"), []byte("o"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, other, "add", ".")
	runGit(t, other, "config", "user.name", "Test")
	runGit(t, other, "config", "user.email", "test@example.com")
	runGit(t, other, "commit", "-m", "from-other")
	runGit(t, other, "push")

	deps := Deps{
		Flags:      GlobalFlags{Yes: true},
		RunRefresh: func(string) error { return nil },
	}
	cmd := newPullCmd(deps)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("pull: %v", err)
	}
	if _, err := os.Stat(filepath.Join(agentsHome, "from-other.txt")); err != nil {
		t.Errorf("pulled file missing: %v", err)
	}
}

// ── log + status subcommands ─────────────────────────────────────────

func TestLog_PrintsHistory(t *testing.T) {
	agentsHome := setupAgentsHomeRepo(t)
	initEmptyRepo(t, agentsHome)
	if err := os.WriteFile(filepath.Join(agentsHome, "loggable.txt"), []byte("L"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, agentsHome, "add", ".")
	runGit(t, agentsHome, "commit", "-m", "log entry")

	cmd := newLogCmd()
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("log: %v", err)
	}
}

func TestStatus_PrintsBranchAndCounts(t *testing.T) {
	agentsHome := setupAgentsHomeRepo(t)
	initEmptyRepo(t, agentsHome)
	if err := os.WriteFile(filepath.Join(agentsHome, "modded.txt"), []byte("m"), 0644); err != nil {
		t.Fatal(err)
	}

	cmd := newStatusCmd()
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("status: %v", err)
	}
}

// printAheadBehind when origin/HEAD exists and there's a local-only commit.
func TestPrintAheadBehind_WithRemote(t *testing.T) {
	agentsHome, _ := setupPushPullPair(t)
	if err := os.WriteFile(filepath.Join(agentsHome, "extra.txt"), []byte("e"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, agentsHome, "add", ".")
	runGit(t, agentsHome, "commit", "-m", "local-extra")
	printAheadBehind(agentsHome, true)
}
