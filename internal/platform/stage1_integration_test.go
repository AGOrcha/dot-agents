package platform

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/links"
)

const (
	fixtureProject      = "proj"
	hookManifestName    = "HOOK.yaml"
	fixtureNoopScriptSh = "#!/bin/sh\nexit 0\n"
)

type hookBundleFixture struct {
	Name            string
	When            string
	Command         string
	EnabledOn       []string
	MatchTools      []string
	MatchExpression string
}

type platformTestPaths struct {
	agentsHome string
	home       string
	repo       string
}

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

func TestHookTranslationAcrossPlatforms_UsesProjectHookSources(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	home := filepath.Join(tmp, "home")
	repo := filepath.Join(tmp, "repo")

	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)

	cursorHook := filepath.Join(agentsHome, "hooks", "proj", "cursor.json")
	codexHook := filepath.Join(agentsHome, "hooks", "proj", "codex.json")
	claudeCompatHook := filepath.Join(agentsHome, "hooks", "proj", "claude-code.json")
	copilotProjectHook := filepath.Join(agentsHome, "hooks", "proj", "pre-tool.json")

	writeTextFile(t, cursorHook, "{\"hooks\":[\"cursor\"]}\n")
	writeTextFile(t, codexHook, "{\"hooks\":[\"codex\"]}\n")
	writeTextFile(t, claudeCompatHook, "{\"hooks\":[\"claude\"]}\n")
	writeTextFile(t, copilotProjectHook, "{\"name\":\"pre-tool\"}\n")
	mkdirAll(t, repo)

	if err := NewCursor().CreateLinks("proj", repo); err != nil {
		t.Fatalf("Cursor CreateLinks failed: %v", err)
	}
	if err := NewCodex().CreateLinks("proj", repo); err != nil {
		t.Fatalf("Codex CreateLinks failed: %v", err)
	}
	if err := NewClaude().CreateLinks("proj", repo); err != nil {
		t.Fatalf("Claude CreateLinks failed: %v", err)
	}
	if err := NewCopilot().CreateLinks("proj", repo); err != nil {
		t.Fatalf("Copilot CreateLinks failed: %v", err)
	}

	assertHardlinked(t, filepath.Join(repo, ".cursor", "hooks.json"), cursorHook)
	assertSymlinkTarget(t, filepath.Join(repo, ".codex", "hooks.json"), codexHook)
	assertSymlinkTarget(t, filepath.Join(home, ".codex", "hooks.json"), codexHook)
	assertSymlinkTarget(t, filepath.Join(repo, ".claude", "settings.local.json"), claudeCompatHook)
	assertNoFile(t, filepath.Join(home, ".claude", "settings.json"))
	assertSymlinkTarget(t, filepath.Join(repo, ".github", "hooks", "pre-tool.json"), copilotProjectHook)
	assertNoFile(t, filepath.Join(repo, ".github", "hooks", "cursor.json"))
	assertNoFile(t, filepath.Join(repo, ".github", "hooks", "claude-code.json"))
}

func TestClaudeCompatTranslationFallsBackToSettingsBucket(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	home := filepath.Join(tmp, "home")
	repo := filepath.Join(tmp, "repo")

	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)

	projectSettings := filepath.Join(agentsHome, "settings", "proj", "claude-code.json")
	globalSettings := filepath.Join(agentsHome, "settings", "global", "claude-code.json")
	writeTextFile(t, projectSettings, "{\"scope\":\"project-settings\"}\n")
	writeTextFile(t, globalSettings, "{\"scope\":\"global-settings\"}\n")
	mkdirAll(t, repo)

	if err := NewClaude().CreateLinks("proj", repo); err != nil {
		t.Fatalf("Claude CreateLinks failed: %v", err)
	}
	if err := NewCopilot().CreateLinks("proj", repo); err != nil {
		t.Fatalf("Copilot CreateLinks failed: %v", err)
	}

	assertSymlinkTarget(t, filepath.Join(repo, ".claude", "settings.local.json"), projectSettings)
	assertSymlinkTarget(t, filepath.Join(home, ".claude", "settings.json"), globalSettings)
}

func TestClaudeCreateLinks_RendersCanonicalHookBundles(t *testing.T) {
	paths := newPlatformTestPaths(t)
	agentsHome := paths.agentsHome
	home := paths.home
	repo := paths.repo

	projectHookDir := filepath.Join(agentsHome, "hooks", "proj", "format-write")
	globalHookDir := filepath.Join(agentsHome, "hooks", "global", "session-banner")
	projectRunScript := writeHookBundleFixture(t, projectHookDir, hookBundleFixture{
		Name:            "format-write",
		When:            "pre_tool_use",
		MatchTools:      []string{"Write", "Edit"},
		MatchExpression: "Write | Edit",
		Command:         "./run.sh",
		EnabledOn:       []string{"claude"},
	})
	globalBannerScript := writeHookBundleFixture(t, globalHookDir, hookBundleFixture{
		Name:      "session-banner",
		When:      "session_start",
		Command:   "./banner.sh",
		EnabledOn: []string{"claude"},
	})
	mkdirAll(t, repo)

	mustCreateLinks(t, "Claude", NewClaude(), fixtureProject, repo)

	projectJSON := readJSONFile(t, filepath.Join(repo, ".claude", "settings.local.json"))
	userJSON := readJSONFile(t, filepath.Join(home, ".claude", "settings.json"))

	assertJSONPathEquals(t, projectJSON, "hooks.PreToolUse.0.matcher", "Write | Edit")
	assertJSONPathEquals(t, projectJSON, "hooks.PreToolUse.0.hooks.0.type", "command")
	assertJSONPathEquals(t, projectJSON, "hooks.PreToolUse.0.hooks.0.command", projectRunScript)
	assertJSONPathEquals(t, userJSON, "hooks.SessionStart.0.hooks.0.command", globalBannerScript)
}

func TestClaudeRemoveLinks_RemovesRenderedCanonicalHookSettings(t *testing.T) {
	paths := newPlatformTestPaths(t)
	agentsHome := paths.agentsHome
	repo := paths.repo

	projectHookDir := filepath.Join(agentsHome, "hooks", "proj", "format-write")
	writeHookBundleFixture(t, projectHookDir, hookBundleFixture{
		Name:      "format-write",
		When:      "pre_tool_use",
		Command:   "./run.sh",
		EnabledOn: []string{"claude"},
	})
	mkdirAll(t, repo)

	mustCreateLinks(t, "Claude", NewClaude(), fixtureProject, repo)
	mustRemoveLinks(t, "Claude", NewClaude(), fixtureProject, repo)

	assertNoFile(t, filepath.Join(repo, ".claude", "settings.local.json"))
}

func TestClaudeCreateLinks_PrunesGlobalRenderedUserSettingsWhenCanonicalHooksDisappear(t *testing.T) {
	paths := newPlatformTestPaths(t)
	agentsHome := paths.agentsHome
	home := paths.home
	repo := paths.repo

	globalHookDir := filepath.Join(agentsHome, "hooks", "global", "session-banner")
	manifestPath := filepath.Join(globalHookDir, hookManifestName)
	writeHookBundleFixture(t, globalHookDir, hookBundleFixture{
		Name:      "session-banner",
		When:      "session_start",
		Command:   "./banner.sh",
		EnabledOn: []string{"claude"},
	})
	mkdirAll(t, repo)

	mustCreateLinks(t, "Claude", NewClaude(), fixtureProject, repo)
	if err := os.Remove(manifestPath); err != nil {
		t.Fatalf("remove manifest: %v", err)
	}
	mustCreateLinks(t, "Claude", NewClaude(), fixtureProject, repo)

	assertNoFile(t, filepath.Join(home, ".claude", "settings.json"))
	assertNoFile(t, filepath.Join(repo, ".claude", "settings.local.json"))
}

func TestCursorAndCodexCreateLinks_RenderCanonicalHookBundles(t *testing.T) {
	paths := newPlatformTestPaths(t)
	agentsHome := paths.agentsHome
	home := paths.home
	repo := paths.repo

	projectHookDir := filepath.Join(agentsHome, "hooks", "proj", "bash-guard")
	globalHookDir := filepath.Join(agentsHome, "hooks", "global", "session-banner")
	projectGuardScript := writeHookBundleFixture(t, projectHookDir, hookBundleFixture{
		Name:       "bash-guard",
		When:       "pre_tool_use",
		MatchTools: []string{"Bash"},
		Command:    "./guard.sh",
		EnabledOn:  []string{"cursor", "codex"},
	})
	globalBannerScript := writeHookBundleFixture(t, globalHookDir, hookBundleFixture{
		Name:      "session-banner",
		When:      "session_start",
		Command:   "./banner.sh",
		EnabledOn: []string{"cursor", "codex"},
	})
	mkdirAll(t, repo)

	mustCreateLinks(t, "Cursor", NewCursor(), fixtureProject, repo)
	mustCreateLinks(t, "Codex", NewCodex(), fixtureProject, repo)

	cursorProject := readJSONFile(t, filepath.Join(repo, ".cursor", "hooks.json"))
	cursorUser := readJSONFile(t, filepath.Join(home, ".cursor", "hooks.json"))
	codexProject := readJSONFile(t, filepath.Join(repo, ".codex", "hooks.json"))
	codexUser := readJSONFile(t, filepath.Join(home, ".codex", "hooks.json"))

	assertJSONPathEquals(t, cursorProject, "version", float64(1))
	assertJSONPathEquals(t, cursorProject, "hooks.preToolUse.0.command", projectGuardScript)
	assertJSONPathEquals(t, cursorProject, "hooks.preToolUse.0.matcher", "Bash")
	assertJSONPathEquals(t, cursorUser, "hooks.sessionStart.0.command", globalBannerScript)

	assertJSONPathEquals(t, codexProject, "hooks.PreToolUse.0.matcher", "Bash")
	assertJSONPathEquals(t, codexProject, "hooks.PreToolUse.0.hooks.0.command", projectGuardScript)
	assertJSONPathEquals(t, codexUser, "hooks.SessionStart.0.hooks.0.command", globalBannerScript)
}

func TestCursorAndCodexRemoveLinks_RemoveRenderedCanonicalHookFiles(t *testing.T) {
	paths := newPlatformTestPaths(t)
	agentsHome := paths.agentsHome
	repo := paths.repo

	projectHookDir := filepath.Join(agentsHome, "hooks", "proj", "bash-guard")
	writeHookBundleFixture(t, projectHookDir, hookBundleFixture{
		Name:       "bash-guard",
		When:       "pre_tool_use",
		MatchTools: []string{"Bash"},
		Command:    "./guard.sh",
		EnabledOn:  []string{"cursor", "codex"},
	})
	mkdirAll(t, repo)

	mustCreateLinks(t, "Cursor", NewCursor(), fixtureProject, repo)
	mustCreateLinks(t, "Codex", NewCodex(), fixtureProject, repo)
	mustRemoveLinks(t, "Cursor", NewCursor(), fixtureProject, repo)
	mustRemoveLinks(t, "Codex", NewCodex(), fixtureProject, repo)

	assertNoFile(t, filepath.Join(repo, ".cursor", "hooks.json"))
	assertNoFile(t, filepath.Join(repo, ".codex", "hooks.json"))
}

func TestCursorAndCodexCreateLinks_PruneRenderedFilesWhenCanonicalHooksDisappear(t *testing.T) {
	paths := newPlatformTestPaths(t)
	agentsHome := paths.agentsHome
	repo := paths.repo

	projectHookDir := filepath.Join(agentsHome, "hooks", "proj", "bash-guard")
	manifestPath := filepath.Join(projectHookDir, hookManifestName)
	writeHookBundleFixture(t, projectHookDir, hookBundleFixture{
		Name:       "bash-guard",
		When:       "pre_tool_use",
		MatchTools: []string{"Bash"},
		Command:    "./guard.sh",
		EnabledOn:  []string{"cursor", "codex"},
	})
	mkdirAll(t, repo)

	mustCreateLinks(t, "Cursor", NewCursor(), fixtureProject, repo)
	mustCreateLinks(t, "Codex", NewCodex(), fixtureProject, repo)
	if err := os.Remove(manifestPath); err != nil {
		t.Fatalf("remove manifest: %v", err)
	}
	mustCreateLinks(t, "Cursor", NewCursor(), fixtureProject, repo)
	mustCreateLinks(t, "Codex", NewCodex(), fixtureProject, repo)

	assertNoFile(t, filepath.Join(repo, ".cursor", "hooks.json"))
	assertNoFile(t, filepath.Join(repo, ".codex", "hooks.json"))
}

func TestCopilotCreateLinks_RendersCanonicalHookBundles(t *testing.T) {
	paths := newPlatformTestPaths(t)
	agentsHome := paths.agentsHome
	repo := paths.repo

	projectHookDir := filepath.Join(agentsHome, "hooks", "proj", "prompt-log")
	globalHookDir := filepath.Join(agentsHome, "hooks", "global", "session-banner")
	projectPromptScript := writeHookBundleFixture(t, projectHookDir, hookBundleFixture{
		Name:      "prompt-log",
		When:      "user_prompt_submit",
		Command:   "./prompt-log.sh",
		EnabledOn: []string{"copilot"},
	})
	globalBannerScript := writeHookBundleFixture(t, globalHookDir, hookBundleFixture{
		Name:      "session-banner",
		When:      "session_start",
		Command:   "./banner.sh",
		EnabledOn: []string{"copilot"},
	})
	mkdirAll(t, repo)

	mustCreateLinks(t, "Copilot", NewCopilot(), fixtureProject, repo)

	sessionFile := readJSONFile(t, filepath.Join(repo, ".github", "hooks", "session-banner.json"))
	promptFile := readJSONFile(t, filepath.Join(repo, ".github", "hooks", "prompt-log.json"))
	compatFile := readJSONFile(t, filepath.Join(repo, ".claude", "settings.local.json"))

	assertJSONPathEquals(t, sessionFile, "version", float64(1))
	assertJSONPathEquals(t, sessionFile, "hooks.sessionStart.0.type", "command")
	assertJSONPathEquals(t, sessionFile, "hooks.sessionStart.0.bash", globalBannerScript)
	assertJSONPathEquals(t, promptFile, "hooks.userPromptSubmitted.0.bash", projectPromptScript)
	assertJSONPathEquals(t, compatFile, "hooks.UserPromptSubmit.0.hooks.0.command", projectPromptScript)
}

func TestCopilotRemoveLinks_RemovesRenderedCanonicalHookFiles(t *testing.T) {
	paths := newPlatformTestPaths(t)
	agentsHome := paths.agentsHome
	repo := paths.repo

	projectHookDir := filepath.Join(agentsHome, "hooks", "proj", "prompt-log")
	writeHookBundleFixture(t, projectHookDir, hookBundleFixture{
		Name:      "prompt-log",
		When:      "user_prompt_submit",
		Command:   "./prompt-log.sh",
		EnabledOn: []string{"copilot"},
	})
	mkdirAll(t, repo)

	mustCreateLinks(t, "Copilot", NewCopilot(), fixtureProject, repo)
	mustRemoveLinks(t, "Copilot", NewCopilot(), fixtureProject, repo)

	assertNoFile(t, filepath.Join(repo, ".claude", "settings.local.json"))
	assertNoFile(t, filepath.Join(repo, ".github", "hooks", "prompt-log.json"))
}

func TestCopilotCreateLinks_PrunesStaleRenderedHookFanout(t *testing.T) {
	paths := newPlatformTestPaths(t)
	agentsHome := paths.agentsHome
	repo := paths.repo

	projectHookDir := filepath.Join(agentsHome, "hooks", "proj", "prompt-log")
	projectPromptScript := writeHookBundleFixture(t, projectHookDir, hookBundleFixture{
		Name:      "prompt-log",
		When:      "user_prompt_submit",
		Command:   "./prompt-log.sh",
		EnabledOn: []string{"copilot"},
	})
	writeTextFile(t, filepath.Join(repo, ".github", "hooks", "stale.json"), `{
  "version": 1,
  "hooks": {
    "sessionStart": [
      {
        "type": "command",
        "bash": "./stale.sh"
      }
    ]
	}
}
`)
	mkdirAll(t, repo)

	mustCreateLinks(t, "Copilot", NewCopilot(), fixtureProject, repo)

	assertNoFile(t, filepath.Join(repo, ".github", "hooks", "stale.json"))
	assertJSONPathEquals(t, readJSONFile(t, filepath.Join(repo, ".github", "hooks", "prompt-log.json")), "hooks.userPromptSubmitted.0.bash", projectPromptScript)
}

func newPlatformTestPaths(t *testing.T) platformTestPaths {
	t.Helper()
	tmp := t.TempDir()
	paths := platformTestPaths{
		agentsHome: filepath.Join(tmp, ".agents"),
		home:       filepath.Join(tmp, "home"),
		repo:       filepath.Join(tmp, "repo"),
	}
	t.Setenv("AGENTS_HOME", paths.agentsHome)
	t.Setenv("HOME", paths.home)
	return paths
}

func mustCreateLinks(t *testing.T, label string, p Platform, project, repo string) {
	t.Helper()
	if err := p.CreateLinks(project, repo); err != nil {
		t.Fatalf("%s CreateLinks failed: %v", label, err)
	}
}

func mustRemoveLinks(t *testing.T, label string, p Platform, project, repo string) {
	t.Helper()
	if err := p.RemoveLinks(project, repo); err != nil {
		t.Fatalf("%s RemoveLinks failed: %v", label, err)
	}
}

func writeHookBundleFixture(t *testing.T, hookDir string, fixture hookBundleFixture) string {
	t.Helper()
	var b strings.Builder
	b.WriteString("name: " + fixture.Name + "\n")
	b.WriteString("when: " + fixture.When + "\n")
	if len(fixture.MatchTools) > 0 || fixture.MatchExpression != "" {
		b.WriteString("match:\n")
		if len(fixture.MatchTools) > 0 {
			b.WriteString("  tools: [" + strings.Join(fixture.MatchTools, ", ") + "]\n")
		}
		if fixture.MatchExpression != "" {
			b.WriteString("  expression: " + fixture.MatchExpression + "\n")
		}
	}
	b.WriteString("run:\n")
	b.WriteString("  command: " + fixture.Command + "\n")
	if len(fixture.EnabledOn) > 0 {
		b.WriteString("enabled_on: [" + strings.Join(fixture.EnabledOn, ", ") + "]\n")
	}
	writeTextFile(t, filepath.Join(hookDir, hookManifestName), b.String())

	if !strings.HasPrefix(fixture.Command, "./") {
		return fixture.Command
	}
	scriptPath := filepath.Join(hookDir, strings.TrimPrefix(fixture.Command, "./"))
	writeTextFile(t, scriptPath, fixtureNoopScriptSh)
	return scriptPath
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

func readJSONFile(t *testing.T, path string) map[string]any {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var out map[string]any
	if err := json.Unmarshal(content, &out); err != nil {
		t.Fatalf("parse json %s: %v\n%s", path, err, string(content))
	}
	return out
}

func assertJSONPathEquals(t *testing.T, doc map[string]any, path string, want any) {
	t.Helper()
	parts := strings.Split(path, ".")
	var cur any = doc
	for _, part := range parts {
		switch node := cur.(type) {
		case map[string]any:
			next, ok := node[part]
			if !ok {
				t.Fatalf("json path %q missing segment %q", path, part)
			}
			cur = next
		case []any:
			idx := int(mustParseInt(t, part))
			if idx < 0 || idx >= len(node) {
				t.Fatalf("json path %q index %d out of range", path, idx)
			}
			cur = node[idx]
		default:
			t.Fatalf("json path %q hit non-container at segment %q", path, part)
		}
	}
	if cur != want {
		t.Fatalf("json path %q = %#v, want %#v", path, cur, want)
	}
}

func mustParseInt(t *testing.T, s string) int64 {
	t.Helper()
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		t.Fatalf("parse int %q: %v", s, err)
	}
	return n
}
