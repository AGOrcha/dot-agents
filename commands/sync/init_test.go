package sync

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunSyncInit_FreshRepo(t *testing.T) {
	agentsHome := setupAgentsHomeRepo(t)

	deps := Deps{Flags: GlobalFlags{}, RunRefresh: func(string) error { return nil }}
	if err := runSyncInit(deps); err != nil {
		t.Fatalf("runSyncInit: %v", err)
	}

	if _, err := os.Stat(filepath.Join(agentsHome, ".git")); err != nil {
		t.Errorf("expected .git directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(agentsHome, ".gitignore")); err != nil {
		t.Errorf("expected .gitignore created: %v", err)
	}
}

func TestRunSyncInit_DryRunSkipsInit(t *testing.T) {
	agentsHome := setupAgentsHomeRepo(t)
	deps := Deps{Flags: GlobalFlags{DryRun: true}, RunRefresh: func(string) error { return nil }}
	if err := runSyncInit(deps); err != nil {
		t.Fatalf("dry run init: %v", err)
	}
	if _, err := os.Stat(filepath.Join(agentsHome, ".git")); !os.IsNotExist(err) {
		t.Errorf(".git should not exist on dry-run: err=%v", err)
	}
}

func TestRunSyncInit_ExistingRepoNoRemote(t *testing.T) {
	agentsHome := setupAgentsHomeRepo(t)
	initEmptyRepo(t, agentsHome)
	deps := Deps{Flags: GlobalFlags{}, RunRefresh: func(string) error { return nil }}
	if err := runSyncInit(deps); err != nil {
		t.Fatalf("runSyncInit on existing repo: %v", err)
	}
}

func TestRunSyncInit_ExistingRepoWithRemote(t *testing.T) {
	agentsHome := setupAgentsHomeRepo(t)
	initEmptyRepo(t, agentsHome)

	tmp := t.TempDir()
	bare := filepath.Join(tmp, "remote.git")
	if err := os.MkdirAll(bare, 0755); err != nil {
		t.Fatal(err)
	}
	runGit(t, bare, "init", "--bare")
	runGit(t, agentsHome, "remote", "add", "origin", bare)

	deps := Deps{Flags: GlobalFlags{}, RunRefresh: func(string) error { return nil }}
	if err := runSyncInit(deps); err != nil {
		t.Fatalf("runSyncInit on existing+remote repo: %v", err)
	}
}

func TestNewInitCmd_RunE(t *testing.T) {
	agentsHome := setupAgentsHomeRepo(t)
	deps := Deps{Flags: GlobalFlags{}, RunRefresh: func(string) error { return nil }}
	cmd := newInitCmd(deps)
	if cmd.Use != "init" {
		t.Errorf("Use = %q, want init", cmd.Use)
	}
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if _, err := os.Stat(filepath.Join(agentsHome, ".git")); err != nil {
		t.Errorf("expected .git after RunE: %v", err)
	}
}
