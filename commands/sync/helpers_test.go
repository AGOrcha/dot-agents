package sync

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"golang.org/x/sys/execabs"
)

// requireGit skips the test when the git binary is not available in PATH.
func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not available: %v", err)
	}
}

// setupAgentsHomeRepo creates a fresh git repo at tmp/agents and points
// AGENTS_HOME at it. Returns the agentsHome path. Test author/committer
// identity is set via env so commits work in CI sandboxes.
func setupAgentsHomeRepo(t *testing.T) string {
	t.Helper()
	requireGit(t)

	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("GIT_AUTHOR_NAME", "Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
	return agentsHome
}

// runGit runs a git command rooted at dir and fails the test on error.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{"-C", dir}, args...)
	out, err := execabs.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// initEmptyRepo runs `git init` and a config dance so commits succeed.
func initEmptyRepo(t *testing.T, dir string) {
	t.Helper()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.name", "Test")
	runGit(t, dir, "config", "user.email", "test@example.com")
	// Initial empty commit so HEAD exists.
	runGit(t, dir, "commit", "--allow-empty", "-m", "seed")
}

// TestHasGitManifests_NoConfig returns false without a config.
func TestHasGitManifests_NoConfig(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, "agents"))
	// No global config exists -> ListProjects empty -> returns false.
	if hasGitManifests() {
		t.Error("hasGitManifests = true with no config; want false")
	}
}

// TestPrintHelpers_NoPanic exercises the small status printers.
func TestPrintHelpers_NoPanic(t *testing.T) {
	requireGit(t)
	agentsHome := setupAgentsHomeRepo(t)
	initEmptyRepo(t, agentsHome)

	// branch + remote + ahead/behind paths.
	printBranchStatus(agentsHome)
	hasRemote := printRemoteStatus(agentsHome)
	if hasRemote {
		t.Error("fresh repo should not have a remote")
	}
	printAheadBehind(agentsHome, hasRemote) // no-op when !hasRemote

	staged, unstaged, untracked := countPorcelainStatus(agentsHome)
	if staged != 0 || unstaged != 0 || untracked != 0 {
		t.Errorf("expected clean repo: staged=%d unstaged=%d untracked=%d", staged, unstaged, untracked)
	}
}

// TestPrintRemoteStatus_WithRemote covers the hasRemote==true branch.
func TestPrintRemoteStatus_WithRemote(t *testing.T) {
	agentsHome := setupAgentsHomeRepo(t)
	initEmptyRepo(t, agentsHome)

	tmp := t.TempDir()
	remoteBare := filepath.Join(tmp, "remote.git")
	if err := os.MkdirAll(remoteBare, 0755); err != nil {
		t.Fatal(err)
	}
	runGit(t, remoteBare, "init", "--bare")
	runGit(t, agentsHome, "remote", "add", "origin", remoteBare)

	if !printRemoteStatus(agentsHome) {
		t.Error("printRemoteStatus = false; want true after remote add")
	}
}

// TestPrintHelpers_NoGitRepo verifies the helpers don't panic when the
// path is not a git repository (commands silently fail to noop).
func TestPrintHelpers_NoGitRepo(t *testing.T) {
	tmp := t.TempDir()
	printBranchStatus(tmp)
	if printRemoteStatus(tmp) {
		t.Error("printRemoteStatus on non-repo should report no remote")
	}
}
