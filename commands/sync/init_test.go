package sync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/execabs"
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
	gi, err := os.ReadFile(filepath.Join(agentsHome, ".gitignore"))
	if err != nil {
		t.Fatalf("expected .gitignore created: %v", err)
	}
	// The machine-local sync boundary must exclude both the binding table
	// (local/) and the materialized caches (cache/) — defects 2 & 5, R7.
	for _, want := range []string{"local/", "cache/"} {
		if !strings.Contains(string(gi), want) {
			t.Errorf(".gitignore missing machine-local entry %q:\n%s", want, gi)
		}
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
	// An already-initialized home is upgraded with the machine-local boundary.
	gi, err := os.ReadFile(filepath.Join(agentsHome, ".gitignore"))
	if err != nil {
		t.Fatalf("expected .gitignore on existing repo: %v", err)
	}
	for _, want := range []string{"local/", "cache/"} {
		if !strings.Contains(string(gi), want) {
			t.Errorf("existing-home .gitignore missing %q:\n%s", want, gi)
		}
	}
}

// TestRunSyncInit_ExistingRepoGitignoreError covers runSyncInit's error branch
// when the in-place .gitignore repair fails (here: .gitignore is a directory).
func TestRunSyncInit_ExistingRepoGitignoreError(t *testing.T) {
	agentsHome := setupAgentsHomeRepo(t)
	initEmptyRepo(t, agentsHome)
	if err := os.Mkdir(filepath.Join(agentsHome, ".gitignore"), 0755); err != nil {
		t.Fatal(err)
	}
	deps := Deps{Flags: GlobalFlags{}, RunRefresh: func(string) error { return nil }}
	if err := runSyncInit(deps); err == nil {
		t.Error("expected error when .gitignore cannot be written")
	}
}

// TestRunSyncInit_ExistingRepoDryRunSkipsRepair covers the existing-repo dry-run
// branch: the in-place gitignore/untrack repair must be skipped, leaving the
// repo untouched.
func TestRunSyncInit_ExistingRepoDryRunSkipsRepair(t *testing.T) {
	agentsHome := setupAgentsHomeRepo(t)
	initEmptyRepo(t, agentsHome)

	deps := Deps{Flags: GlobalFlags{DryRun: true}, RunRefresh: func(string) error { return nil }}
	if err := runSyncInit(deps); err != nil {
		t.Fatalf("runSyncInit dry-run on existing repo: %v", err)
	}
	if _, err := os.Stat(filepath.Join(agentsHome, ".gitignore")); !os.IsNotExist(err) {
		t.Errorf("dry-run must not write .gitignore on existing repo: err=%v", err)
	}
}

// TestRunSyncInit_UntracksAlreadyTrackedMachineLocal proves Fix 3: a home that
// already committed local/ and cache/ (before the gitignore fix) must have those
// paths UNTRACKED by `sync init` — the gitignore alone only stops new tracking,
// so an in-place repair must `git rm --cached` the machine-local state. The
// working-tree files are preserved; only the index entries are removed.
func TestRunSyncInit_UntracksAlreadyTrackedMachineLocal(t *testing.T) {
	agentsHome := setupAgentsHomeRepo(t)
	initEmptyRepo(t, agentsHome)

	// Simulate the pre-fix state: machine-local files committed into the repo.
	for _, rel := range []string{"local/bindings.json", "cache/config/foo"} {
		full := filepath.Join(agentsHome, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("machine-local"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	runGit(t, agentsHome, "add", "-A")
	runGit(t, agentsHome, "commit", "-m", "pre-fix: tracked machine-local state")

	deps := Deps{Flags: GlobalFlags{}, RunRefresh: func(string) error { return nil }}
	if err := runSyncInit(deps); err != nil {
		t.Fatalf("runSyncInit: %v", err)
	}

	tracked, err := execabs.Command("git", "-C", agentsHome, "ls-files").Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"local/", "cache/"} {
		if strings.Contains(string(tracked), leak) {
			t.Errorf("%s still tracked after repair:\n%s", leak, tracked)
		}
	}
	// The working-tree files must survive the untrack (--cached, not -f).
	for _, rel := range []string{"local/bindings.json", "cache/config/foo"} {
		if _, err := os.Stat(filepath.Join(agentsHome, rel)); err != nil {
			t.Errorf("untrack deleted the working-tree file %s: %v", rel, err)
		}
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

// TestRunSyncInit_GitCommitFailureSurfacesError ensures the friendly error
// path fires when git refuses to commit because user.email/user.name are
// unset. The previous code swallowed the .Run() error and lied to the user
// with ui.Success.
func TestRunSyncInit_GitCommitFailureSurfacesError(t *testing.T) {
	requireGit(t)

	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	// Strip every git identity source so the commit must fail. HOME +
	// XDG_CONFIG_HOME point at empty dirs (no ~/.gitconfig). GIT_CONFIG_*
	// vars are unset. GIT_AUTHOR_*/GIT_COMMITTER_* are unset so git falls
	// back to user.email/user.name (which is also unset).
	emptyHome := filepath.Join(tmp, "empty-home")
	if err := os.MkdirAll(emptyHome, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", emptyHome)
	t.Setenv("XDG_CONFIG_HOME", emptyHome)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(emptyHome, ".no-such-config"))
	t.Setenv("GIT_CONFIG_SYSTEM", filepath.Join(emptyHome, ".no-such-system"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, k := range []string{"GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL"} {
		t.Setenv(k, "")
	}

	deps := Deps{Flags: GlobalFlags{}, RunRefresh: func(string) error { return nil }}
	err := runSyncInit(deps)
	if err == nil {
		t.Skip("git accepted the commit even with no identity (likely a CI git build with implicit defaults); cannot exercise the error path here")
	}
	msg := err.Error()
	if !(strings.Contains(msg, "user.email") || strings.Contains(msg, "user.name") || strings.Contains(msg, "git commit")) {
		t.Errorf("expected git-commit/user-config error, got %q", msg)
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
