package sync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"golang.org/x/sys/execabs"
)

func findSubcmd(t *testing.T, root *cobra.Command, name string) *cobra.Command {
	t.Helper()
	for _, c := range root.Commands() {
		if c.Name() == name {
			return c
		}
	}
	t.Fatalf("subcommand %q not found", name)
	return nil
}

func TestCommit_NewFileCreatesCommit(t *testing.T) {
	agentsHome := setupAgentsHomeRepo(t)
	initEmptyRepo(t, agentsHome)
	if err := os.WriteFile(filepath.Join(agentsHome, "hello.txt"), []byte("hi\n"), 0644); err != nil {
		t.Fatal(err)
	}

	deps := Deps{Flags: GlobalFlags{}, RunRefresh: func(string) error { return nil }}
	cmd := newCommitCmd(deps)
	if err := cmd.RunE(cmd, []string{"add", "hello"}); err != nil {
		t.Fatalf("commit RunE: %v", err)
	}

	out, err := execabs.Command("git", "-C", agentsHome, "log", "--oneline").CombinedOutput()
	if err != nil {
		t.Fatalf("git log: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "add hello") {
		t.Errorf("commit message missing from log: %s", out)
	}
}

func TestCommit_MessageFlag(t *testing.T) {
	agentsHome := setupAgentsHomeRepo(t)
	initEmptyRepo(t, agentsHome)
	if err := os.WriteFile(filepath.Join(agentsHome, "flag.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	deps := Deps{Flags: GlobalFlags{}, RunRefresh: func(string) error { return nil }}
	root := NewSyncCmd(deps)
	commitCmd := findSubcmd(t, root, "commit")
	if err := commitCmd.Flags().Set("message", "via-flag"); err != nil {
		t.Fatal(err)
	}
	if err := commitCmd.RunE(commitCmd, nil); err != nil {
		t.Fatalf("commit RunE: %v", err)
	}
	out, _ := execabs.Command("git", "-C", agentsHome, "log", "--oneline").CombinedOutput()
	if !strings.Contains(string(out), "via-flag") {
		t.Errorf("expected 'via-flag' in log:\n%s", out)
	}
}

func TestCommit_NothingToCommit(t *testing.T) {
	agentsHome := setupAgentsHomeRepo(t)
	initEmptyRepo(t, agentsHome)

	deps := Deps{Flags: GlobalFlags{}, RunRefresh: func(string) error { return nil }}
	cmd := newCommitCmd(deps)
	// Clean tree → should not error.
	if err := cmd.RunE(cmd, []string{"nothing"}); err != nil {
		t.Errorf("commit on clean tree should succeed, got %v", err)
	}
}

// TestCommit_GitCommitFailureSurfacesError ensures `da sync commit` returns a
// real (non-"nothing to commit") git commit failure instead of reporting
// success.
func TestCommit_GitCommitFailureSurfacesError(t *testing.T) {
	agentsHome := setupAgentsHomeRepo(t)
	initEmptyRepo(t, agentsHome)
	if err := os.WriteFile(filepath.Join(agentsHome, "change.txt"), []byte("x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	stripGitIdentity(t)

	deps := Deps{Flags: GlobalFlags{}, RunRefresh: func(string) error { return nil }}
	cmd := newCommitCmd(deps)
	err := cmd.RunE(cmd, []string{"should-fail"})
	if err == nil {
		t.Skip("git accepted the commit even with no identity (likely a CI git build with implicit defaults); cannot exercise the error path here")
	}
	if strings.Contains(err.Error(), "nothing to commit") {
		t.Fatalf("expected a real commit failure, got the nothing-to-commit sentinel: %v", err)
	}
}

// TestCommit_GitAddFailureSurfacesError ensures the previously-unchecked
// `git add -A` error in the commit RunE is now returned before git commit
// even runs.
func TestCommit_GitAddFailureSurfacesError(t *testing.T) {
	agentsHome := setupAgentsHomeRepo(t)
	initEmptyRepo(t, agentsHome)
	if err := os.WriteFile(filepath.Join(agentsHome, "change.txt"), []byte("x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Corrupt the index so `git add -A` fails before it ever reaches commit.
	if err := os.WriteFile(filepath.Join(agentsHome, ".git", "index"), []byte("not-a-real-index"), 0644); err != nil {
		t.Fatal(err)
	}

	deps := Deps{Flags: GlobalFlags{}, RunRefresh: func(string) error { return nil }}
	cmd := newCommitCmd(deps)
	err := cmd.RunE(cmd, []string{"should-fail"})
	if err == nil {
		t.Fatal("expected commit RunE to surface the git add failure, got nil")
	}
	if !strings.Contains(err.Error(), "git add") {
		t.Errorf("expected a git add error, got %q", err)
	}
}

func TestCommit_DryRunSkipsCommit(t *testing.T) {
	agentsHome := setupAgentsHomeRepo(t)
	initEmptyRepo(t, agentsHome)
	if err := os.WriteFile(filepath.Join(agentsHome, "dr.txt"), []byte("dr"), 0644); err != nil {
		t.Fatal(err)
	}

	deps := Deps{Flags: GlobalFlags{DryRun: true}, RunRefresh: func(string) error { return nil }}
	cmd := newCommitCmd(deps)
	if err := cmd.RunE(cmd, []string{"dry"}); err != nil {
		t.Fatalf("dry-run commit: %v", err)
	}
	out, _ := execabs.Command("git", "-C", agentsHome, "log", "--oneline").CombinedOutput()
	// Should only have the seed commit; "dry" should not appear.
	if strings.Contains(string(out), "dry") {
		t.Errorf("dry-run should not create commit:\n%s", out)
	}
}

func TestCommit_DefaultMessageWhenEmpty(t *testing.T) {
	agentsHome := setupAgentsHomeRepo(t)
	initEmptyRepo(t, agentsHome)
	if err := os.WriteFile(filepath.Join(agentsHome, "default.txt"), []byte("d"), 0644); err != nil {
		t.Fatal(err)
	}

	deps := Deps{Flags: GlobalFlags{}, RunRefresh: func(string) error { return nil }}
	cmd := newCommitCmd(deps)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("commit: %v", err)
	}
	out, _ := execabs.Command("git", "-C", agentsHome, "log", "--oneline").CombinedOutput()
	if !strings.Contains(string(out), "Update ~/.agents/ configuration") {
		t.Errorf("expected default message in log:\n%s", out)
	}
}

// TestCommit_DryRunDoesNotStage guards resolveCommitMessage's purity: a
// --dry-run with NO message must not stage anything. The prior version ran
// `git add -A` during message resolution — before runSyncCommit's dry-run
// guard — so `da sync commit --dry-run` silently staged the whole tree.
func TestCommit_DryRunDoesNotStage(t *testing.T) {
	agentsHome := setupAgentsHomeRepo(t)
	initEmptyRepo(t, agentsHome)
	if err := os.WriteFile(filepath.Join(agentsHome, "untracked.txt"), []byte("x\n"), 0644); err != nil {
		t.Fatal(err)
	}

	deps := Deps{Flags: GlobalFlags{DryRun: true}, RunRefresh: func(string) error { return nil }}
	cmd := newCommitCmd(deps)
	// No -m and no positional args -> exercises resolveCommitMessage's default path.
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("dry-run commit: %v", err)
	}

	out, err := execabs.Command("git", "-C", agentsHome, "status", "--porcelain").CombinedOutput()
	if err != nil {
		t.Fatalf("git status: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "?? untracked.txt") {
		t.Errorf("dry-run must not stage; untracked.txt should stay untracked, got:\n%s", out)
	}
}
