package commands

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/config"
)

func setupScaffoldedHookTestHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	if err := os.MkdirAll(filepath.Join(config.AgentsHome(), "hooks", "global"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := scaffoldWorkflowAssets(config.AgentsHome()); err != nil {
		t.Fatalf("scaffoldWorkflowAssets: %v", err)
	}
	return config.AgentsHome()
}

func initShellHookTestRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
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
	}
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(repo, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	run("init")
	run("config", "user.name", "Test")
	run("config", "user.email", "test@example.com")
	write(".agentsrc.json", `{"project":"shell-hook-proj","version":1,"sources":[{"type":"local"}]}`)
	write(".agents/active/sample.plan.md", "# Plan\n\n- [ ] First task\n")
	write(".agents/active/handoffs/next.md", "# Handoff\n")
	write(".agents/lessons.md", "- lesson one\n")
	write("README.md", "hello\n")
	run("add", ".")
	run("commit", "-m", "init")
	return repo
}

func runShellHook(t *testing.T, scriptPath, projectDir string, extraEnv ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("/bin/sh", scriptPath)
	cmd.Dir = projectDir
	cmd.Env = append(os.Environ(),
		"AGENTS_HOME="+config.AgentsHome(),
		"CLAUDE_PROJECT_DIR="+projectDir,
		"PATH=/usr/bin:/bin",
	)
	cmd.Env = append(cmd.Env, extraEnv...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestScaffoldedSessionOrientFallbackRendersExpectedSections(t *testing.T) {
	agentsHome := setupScaffoldedHookTestHome(t)
	repo := initShellHookTestRepo(t)

	script := filepath.Join(agentsHome, "hooks", "global", "session-orient", "orient.sh")
	output, err := runShellHook(t, script, repo)
	if err != nil {
		t.Fatalf("session-orient failed: %v\n%s", err, output)
	}
	for _, section := range []string{
		"# Project",
		"# Active Plans",
		"# Last Checkpoint",
		"# Pending Handoffs",
		"# Recent Lessons",
		"# Pending Proposals",
		"# Next Action",
	} {
		if !strings.Contains(output, section) {
			t.Fatalf("missing section %q in output:\n%s", section, output)
		}
	}
	if !strings.Contains(output, "- First task") {
		t.Fatalf("expected next task in orient output:\n%s", output)
	}
}

func TestScaffoldedSessionCaptureFallbackWritesCheckpointAndLog(t *testing.T) {
	agentsHome := setupScaffoldedHookTestHome(t)
	repo := initShellHookTestRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("hello world\n"), 0644); err != nil {
		t.Fatal(err)
	}

	script := filepath.Join(agentsHome, "hooks", "global", "session-capture", "capture.sh")
	output, err := runShellHook(t, script, repo)
	if err != nil {
		t.Fatalf("session-capture failed: %v\n%s", err, output)
	}

	checkpointPath := filepath.Join(config.ProjectContextDir("shell-hook-proj"), "checkpoint.yaml")
	checkpoint, err := os.ReadFile(checkpointPath)
	if err != nil {
		t.Fatal(err)
	}
	rendered := string(checkpoint)
	for _, expected := range []string{
		"schema_version: 1",
		`name: "shell-hook-proj"`,
		`verification:`,
		`status: "unknown"`,
		`next_action: "First task"`,
		`- "README.md"`,
	} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("checkpoint missing %q:\n%s", expected, rendered)
		}
	}

	sessionLog, err := os.ReadFile(filepath.Join(config.ProjectContextDir("shell-hook-proj"), "session-log.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(sessionLog), "next_action: First task") {
		t.Fatalf("unexpected session log:\n%s", string(sessionLog))
	}
}

func TestScaffoldedGuardCommandsBlocksForbiddenPattern(t *testing.T) {
	agentsHome := setupScaffoldedHookTestHome(t)
	script := filepath.Join(agentsHome, "hooks", "global", "guard-commands", "guard.sh")
	cmd := exec.Command("/bin/sh", script)
	cmd.Env = append(os.Environ(), "PATH=/usr/bin:/bin")
	cmd.Stdin = strings.NewReader(`{"tool_input":{"command":"git push --force origin main"}}`)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected guard command to block")
	}
	if !strings.Contains(string(out), "blocked by guard-commands") {
		t.Fatalf("unexpected output:\n%s", string(out))
	}
}

func TestScaffoldedSecretScanWarnsOnRealSecretEvenWithPlaceholder(t *testing.T) {
	agentsHome := setupScaffoldedHookTestHome(t)
	tmp := t.TempDir()
	target := filepath.Join(tmp, "test.txt")
	if err := os.WriteFile(target, []byte("REPLACE_ME\nreal sk_live_123456789\n"), 0644); err != nil {
		t.Fatal(err)
	}

	script := filepath.Join(agentsHome, "hooks", "global", "secret-scan", "scan.sh")
	cmd := exec.Command("/bin/sh", script)
	cmd.Env = append(os.Environ(), "PATH=/usr/bin:/bin")
	cmd.Stdin = strings.NewReader(`{"file_path":"` + target + `"}`)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("secret-scan should be non-blocking: %v\n%s", err, string(out))
	}
	if !strings.Contains(string(out), "secret-scan warning") {
		t.Fatalf("expected warning output, got:\n%s", string(out))
	}
}
