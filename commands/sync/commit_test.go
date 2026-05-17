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
