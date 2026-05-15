package commands

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFetchGitSourceRefreshesMovingBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	remote, work := initInstallGitRemote(t, "develop")
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
	oldFlags := Flags
	defer func() { Flags = oldFlags }()
	Flags = GlobalFlags{}

	cacheDir, err := fetchGitSource(remote, "develop")
	if err != nil {
		t.Fatalf("first fetchGitSource: %v", err)
	}
	if got := readInstallGitFile(t, cacheDir, "payload.txt"); got != "v1\n" {
		t.Fatalf("initial payload = %q, want v1", got)
	}

	writeInstallGitFile(t, filepath.Join(work, "payload.txt"), "v2\n")
	runInstallGit(t, work, "add", "payload.txt")
	runInstallGit(t, work, "commit", "-m", "update payload")
	runInstallGit(t, work, "push", "origin", "develop")

	cacheDir2, err := fetchGitSource(remote, "develop")
	if err != nil {
		t.Fatalf("second fetchGitSource: %v", err)
	}
	if cacheDir2 != cacheDir {
		t.Fatalf("cache dir changed across refresh: %q vs %q", cacheDir, cacheDir2)
	}
	if got := readInstallGitFile(t, cacheDir2, "payload.txt"); got != "v2\n" {
		t.Fatalf("refreshed payload = %q, want v2", got)
	}
}

func TestFetchGitSourceKeepsPinnedTagStable(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	remote, work := initInstallGitRemote(t, "develop")
	runInstallGit(t, work, "tag", "v1")
	runInstallGit(t, work, "push", "origin", "v1")

	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
	oldFlags := Flags
	defer func() { Flags = oldFlags }()
	Flags = GlobalFlags{}

	cacheDir, err := fetchGitSource(remote, "v1")
	if err != nil {
		t.Fatalf("fetchGitSource tag: %v", err)
	}
	if got := readInstallGitFile(t, cacheDir, "payload.txt"); got != "v1\n" {
		t.Fatalf("tag payload = %q, want v1", got)
	}

	writeInstallGitFile(t, filepath.Join(work, "payload.txt"), "v2\n")
	runInstallGit(t, work, "add", "payload.txt")
	runInstallGit(t, work, "commit", "-m", "branch moved")
	runInstallGit(t, work, "push", "origin", "develop")

	cacheDir2, err := fetchGitSource(remote, "v1")
	if err != nil {
		t.Fatalf("second fetchGitSource tag: %v", err)
	}
	if cacheDir2 != cacheDir {
		t.Fatalf("tag cache dir changed across fetch: %q vs %q", cacheDir, cacheDir2)
	}
	if got := readInstallGitFile(t, cacheDir2, "payload.txt"); got != "v1\n" {
		t.Fatalf("tag payload drifted = %q, want v1", got)
	}
}

func initInstallGitRemote(t *testing.T, branch string) (string, string) {
	t.Helper()
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	work := filepath.Join(root, "work")

	runInstallGitNoDir(t, "init", "--bare", remote)
	runInstallGitNoDir(t, "clone", remote, work)
	runInstallGit(t, work, "checkout", "-b", branch)
	writeInstallGitFile(t, filepath.Join(work, "payload.txt"), "v1\n")
	runInstallGit(t, work, "add", "payload.txt")
	runInstallGit(t, work, "commit", "-m", "initial commit")
	runInstallGit(t, work, "push", "-u", "origin", branch)

	return remote, work
}

func runInstallGitNoDir(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
	return strings.TrimSpace(string(out))
}

func runInstallGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed in %s: %v\n%s", args, dir, err, string(out))
	}
	return strings.TrimSpace(string(out))
}

func writeInstallGitFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func readInstallGitFile(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read %s from %s: %v", name, dir, err)
	}
	return string(data)
}