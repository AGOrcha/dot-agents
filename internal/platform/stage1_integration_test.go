package platform

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/links"
)

func TestClaudeCreateLinks_DualSkillOutputs(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	home := filepath.Join(tmp, "home")
	repo := filepath.Join(tmp, "repo")

	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)

	skillDir := filepath.Join(agentsHome, "skills", "proj", "review")
	writeTextFile(t, filepath.Join(skillDir, "SKILL.md"), "---\nname: review\ndescription: review changes\n---\n")
	mkdirAll(t, repo)

	if err := NewClaude().CreateLinks("proj", repo); err != nil {
		t.Fatalf("CreateLinks failed: %v", err)
	}

	assertSymlinkTarget(t, filepath.Join(repo, ".claude", "skills", "review"), skillDir)
	assertSymlinkTarget(t, filepath.Join(repo, ".agents", "skills", "review"), skillDir)
}

func TestCursorCreateLinks_HardlinksAndMCPSelection(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	home := filepath.Join(tmp, "home")
	repo := filepath.Join(tmp, "repo")

	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)

	globalRule := filepath.Join(agentsHome, "rules", "global", "rules.mdc")
	projectRule := filepath.Join(agentsHome, "rules", "proj", "lint.mdc")
	cursorSettings := filepath.Join(agentsHome, "settings", "proj", "cursor.json")
	cursorMCP := filepath.Join(agentsHome, "mcp", "proj", "cursor.json")
	fallbackMCP := filepath.Join(agentsHome, "mcp", "proj", "mcp.json")
	cursorIgnore := filepath.Join(agentsHome, "settings", "proj", "cursorignore")
	cursorHooks := filepath.Join(agentsHome, "hooks", "proj", "cursor.json")

	writeTextFile(t, globalRule, "---\ndescription: global rules\n---\n")
	writeTextFile(t, projectRule, "---\ndescription: lint\n---\n")
	writeTextFile(t, cursorSettings, "{}\n")
	writeTextFile(t, cursorMCP, "{\"cursor\":true}\n")
	writeTextFile(t, fallbackMCP, "{\"mcp\":true}\n")
	writeTextFile(t, cursorIgnore, "node_modules\n")
	writeTextFile(t, cursorHooks, "{\"hooks\":[]}\n")
	mkdirAll(t, repo)

	if err := NewCursor().CreateLinks("proj", repo); err != nil {
		t.Fatalf("CreateLinks failed: %v", err)
	}

	assertHardlinked(t, filepath.Join(repo, ".cursor", "rules", "global--rules.mdc"), globalRule)
	assertHardlinked(t, filepath.Join(repo, ".cursor", "rules", "proj--lint.mdc"), projectRule)
	assertHardlinked(t, filepath.Join(repo, ".cursor", "settings.json"), cursorSettings)
	assertHardlinked(t, filepath.Join(repo, ".cursor", "mcp.json"), cursorMCP)
	assertHardlinked(t, filepath.Join(repo, ".cursorignore"), cursorIgnore)
	assertHardlinked(t, filepath.Join(repo, ".cursor", "hooks.json"), cursorHooks)
}

func TestCursorCreateLinks_MCPFallsBackToProjectGenericBeforeGlobalPlatformFile(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	home := filepath.Join(tmp, "home")
	repo := filepath.Join(tmp, "repo")

	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)

	projectGenericMCP := filepath.Join(agentsHome, "mcp", "proj", "mcp.json")
	globalCursorMCP := filepath.Join(agentsHome, "mcp", "global", "cursor.json")

	writeTextFile(t, projectGenericMCP, "{\"source\":\"project-generic\"}\n")
	writeTextFile(t, globalCursorMCP, "{\"source\":\"global-cursor\"}\n")
	mkdirAll(t, repo)

	if err := NewCursor().CreateLinks("proj", repo); err != nil {
		t.Fatalf("CreateLinks failed: %v", err)
	}

	assertHardlinked(t, filepath.Join(repo, ".cursor", "mcp.json"), projectGenericMCP)
}

func TestCopilotCreateLinks_MCPSelectionAndHookFanout(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	home := filepath.Join(tmp, "home")
	repo := filepath.Join(tmp, "repo")

	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)

	copilotMCP := filepath.Join(agentsHome, "mcp", "proj", "copilot.json")
	fallbackMCP := filepath.Join(agentsHome, "mcp", "proj", "mcp.json")
	hooksDir := filepath.Join(agentsHome, "hooks", "proj")
	settingsCompat := filepath.Join(agentsHome, "settings", "proj", "claude-code.json")

	writeTextFile(t, copilotMCP, "{\"copilot\":true}\n")
	writeTextFile(t, fallbackMCP, "{\"mcp\":true}\n")
	writeTextFile(t, filepath.Join(hooksDir, "claude-code.json"), "{\"hooks\":[]}\n")
	writeTextFile(t, filepath.Join(hooksDir, "pre-tool.json"), "{\"name\":\"pre-tool\"}\n")
	writeTextFile(t, filepath.Join(hooksDir, "post-save.json"), "{\"name\":\"post-save\"}\n")
	writeTextFile(t, filepath.Join(hooksDir, "cursor.json"), "{\"name\":\"cursor\"}\n")
	writeTextFile(t, settingsCompat, "{\"settings\":true}\n")
	mkdirAll(t, repo)

	if err := NewCopilot().CreateLinks("proj", repo); err != nil {
		t.Fatalf("CreateLinks failed: %v", err)
	}

	assertSymlinkTarget(t, filepath.Join(repo, ".vscode", "mcp.json"), copilotMCP)
	assertSymlinkTarget(t, filepath.Join(repo, ".claude", "settings.local.json"), filepath.Join(hooksDir, "claude-code.json"))
	assertSymlinkTarget(t, filepath.Join(repo, ".github", "hooks", "pre-tool.json"), filepath.Join(hooksDir, "pre-tool.json"))
	assertSymlinkTarget(t, filepath.Join(repo, ".github", "hooks", "post-save.json"), filepath.Join(hooksDir, "post-save.json"))
	assertNoFile(t, filepath.Join(repo, ".github", "hooks", "cursor.json"))
	assertNoFile(t, filepath.Join(repo, ".github", "hooks", "claude-code.json"))
}

func TestClaudeCreateLinks_PrefersHooksOverSettingsAndUsesGlobalCompatForUser(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	home := filepath.Join(tmp, "home")
	repo := filepath.Join(tmp, "repo")

	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)

	projectHook := filepath.Join(agentsHome, "hooks", "proj", "claude-code.json")
	projectSettings := filepath.Join(agentsHome, "settings", "proj", "claude-code.json")
	globalHook := filepath.Join(agentsHome, "hooks", "global", "claude-code.json")
	globalSettings := filepath.Join(agentsHome, "settings", "global", "claude-code.json")

	writeTextFile(t, projectHook, "{\"source\":\"project-hook\"}\n")
	writeTextFile(t, projectSettings, "{\"source\":\"project-settings\"}\n")
	writeTextFile(t, globalHook, "{\"source\":\"global-hook\"}\n")
	writeTextFile(t, globalSettings, "{\"source\":\"global-settings\"}\n")
	mkdirAll(t, repo)

	if err := NewClaude().CreateLinks("proj", repo); err != nil {
		t.Fatalf("CreateLinks failed: %v", err)
	}

	assertSymlinkTarget(t, filepath.Join(repo, ".claude", "settings.local.json"), projectHook)
	assertSymlinkTarget(t, filepath.Join(home, ".claude", "settings.json"), globalHook)
}

func TestCursorCreateLinks_PrefersProjectHooksForRepoAndGlobalForUser(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	home := filepath.Join(tmp, "home")
	repo := filepath.Join(tmp, "repo")

	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)

	globalHook := filepath.Join(agentsHome, "hooks", "global", "cursor.json")
	projectHook := filepath.Join(agentsHome, "hooks", "proj", "cursor.json")
	writeTextFile(t, globalHook, "{\"scope\":\"global\"}\n")
	writeTextFile(t, projectHook, "{\"scope\":\"project\"}\n")
	mkdirAll(t, repo)

	if err := NewCursor().CreateLinks("proj", repo); err != nil {
		t.Fatalf("CreateLinks failed: %v", err)
	}

	assertHardlinked(t, filepath.Join(repo, ".cursor", "hooks.json"), projectHook)
	assertHardlinked(t, filepath.Join(home, ".cursor", "hooks.json"), globalHook)
}

func TestCopilotCreateLinks_ClaudeCompatFallsBackToProjectSettingsBeforeGlobalHooks(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	home := filepath.Join(tmp, "home")
	repo := filepath.Join(tmp, "repo")

	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)

	projectSettings := filepath.Join(agentsHome, "settings", "proj", "claude-code.json")
	globalHook := filepath.Join(agentsHome, "hooks", "global", "claude-code.json")
	writeTextFile(t, projectSettings, "{\"source\":\"project-settings\"}\n")
	writeTextFile(t, globalHook, "{\"source\":\"global-hook\"}\n")
	mkdirAll(t, repo)

	if err := NewCopilot().CreateLinks("proj", repo); err != nil {
		t.Fatalf("CreateLinks failed: %v", err)
	}

	assertSymlinkTarget(t, filepath.Join(repo, ".claude", "settings.local.json"), projectSettings)
}

func TestCodexCreateLinks_PrefersProjectFallbackHookOverGlobalPrimary(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	home := filepath.Join(tmp, "home")
	repo := filepath.Join(tmp, "repo")

	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)

	globalPrimary := filepath.Join(agentsHome, "hooks", "global", "codex.json")
	projectFallback := filepath.Join(agentsHome, "hooks", "proj", "codex-hooks.json")
	writeTextFile(t, globalPrimary, "{\"source\":\"global-primary\"}\n")
	writeTextFile(t, projectFallback, "{\"source\":\"project-fallback\"}\n")
	mkdirAll(t, repo)

	if err := NewCodex().CreateLinks("proj", repo); err != nil {
		t.Fatalf("CreateLinks failed: %v", err)
	}

	assertSymlinkTarget(t, filepath.Join(repo, ".codex", "hooks.json"), projectFallback)
	assertSymlinkTarget(t, filepath.Join(home, ".codex", "hooks.json"), projectFallback)
}

func TestCopilotCreateLinks_PrefersProjectInstructions(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	home := filepath.Join(tmp, "home")
	repo := filepath.Join(tmp, "repo")

	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)

	globalFallbackRules := filepath.Join(agentsHome, "rules", "global", "rules.md")
	globalInstructions := filepath.Join(agentsHome, "rules", "global", "copilot-instructions.md")
	projectInstructions := filepath.Join(agentsHome, "rules", "proj", "copilot-instructions.md")

	writeTextFile(t, globalFallbackRules, "# Global Rules\n")
	writeTextFile(t, globalInstructions, "# Global Copilot Instructions\n")
	writeTextFile(t, projectInstructions, "# Project Copilot Instructions\n")
	mkdirAll(t, repo)

	if err := NewCopilot().CreateLinks("proj", repo); err != nil {
		t.Fatalf("CreateLinks failed: %v", err)
	}

	assertSymlinkTarget(t, filepath.Join(repo, ".github", "copilot-instructions.md"), projectInstructions)
}

func writeTextFile(t *testing.T, path, content string) {
	t.Helper()
	mkdirAll(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func assertSymlinkTarget(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.Readlink(path)
	if err != nil {
		t.Fatalf("expected symlink at %s: %v", path, err)
	}
	if got != want {
		t.Fatalf("expected %s to point to %s, got %s", path, want, got)
	}
}

func assertHardlinked(t *testing.T, path, src string) {
	t.Helper()
	linked, err := links.AreHardlinked(path, src)
	if err != nil {
		t.Fatalf("AreHardlinked(%s, %s): %v", path, src, err)
	}
	if !linked {
		t.Fatalf("expected %s to be hard-linked to %s", path, src)
	}
}

func assertNoFile(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be absent, got err=%v", path, err)
	}
}
