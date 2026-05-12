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
	fixtureProject            = "proj"
	hookManifestName          = "HOOK.yaml"
	fixtureNoopScriptSh       = "#!/bin/sh\nexit 0\n"
	dirAgents                 = ".agents"
	dirClaude                 = ".claude"
	dirCursor                 = ".cursor"
	dirCodex                  = ".codex"
	dirGithub                 = ".github"
	fileSettingsJSON          = "settings.json"
	fileSettingsLocalJSON     = "settings.local.json"
	fileHooksJSON             = "hooks.json"
	fileCursorJSON            = "cursor.json"
	fileMCPJSON               = "mcp.json"
	fileClaudeCodeJSON        = "claude-code.json"
	filePreToolJSON           = "pre-tool.json"
	filePostSaveJSON          = "post-save.json"
	fileCopilotInstructionsMD = "copilot-instructions.md"
	filePromptLogJSON         = "prompt-log.json"
	hookNameFormatWrite       = "format-write"
	hookNameSessionBanner     = "session-banner"
	hookNameBashGuard         = "bash-guard"
	hookNamePromptLog         = "prompt-log"
	cmdBannerScript           = "./banner.sh"
	cmdGuardScript            = "./guard.sh"
	cmdPromptLogScript        = "./prompt-log.sh"
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

func TestClaudeCreateLinksDualSkillOutputs(t *testing.T) {
	paths := newPlatformTestPaths(t)
	agentsHome := paths.agentsHome
	repo := paths.repo

	skillDir := filepath.Join(agentsHome, "skills", "proj", "review")
	writeTextFile(t, filepath.Join(skillDir, "SKILL.md"), "---\nname: review\ndescription: review changes\n---\n")
	mkdirAll(t, repo)

	// Shared targets are now written by the command-layer plan before CreateLinks.
	if err := CollectAndExecuteSharedTargetPlan(fixtureProject, repo, []Platform{NewClaude()}); err != nil {
		t.Fatalf("CollectAndExecuteSharedTargetPlan: %v", err)
	}
	mustCreateLinks(t, "Claude", NewClaude(), fixtureProject, repo)

	assertSymlinkTarget(t, filepath.Join(repo, dirClaude, "skills", "review"), skillDir)
	assertSymlinkTarget(t, filepath.Join(repo, dirAgents, "skills", "review"), skillDir)
}

func TestClaudeCreateLinksReplacesImportedRepoSkillDirWithManagedSymlink(t *testing.T) {
	paths := newPlatformTestPaths(t)
	agentsHome := paths.agentsHome
	repo := paths.repo

	skillDir := filepath.Join(agentsHome, "skills", "proj", "review")
	writeTextFile(t, filepath.Join(skillDir, "SKILL.md"), "---\nname: review\ndescription: canonical review\n---\n")
	writeTextFile(t, filepath.Join(repo, dirAgents, "skills", "review", "SKILL.md"), "---\nname: review\ndescription: imported review\n---\n")

	// Shared targets are now written by the command-layer plan before CreateLinks.
	// The executor replaces the imported directory with a managed symlink.
	if err := CollectAndExecuteSharedTargetPlan(fixtureProject, repo, []Platform{NewClaude()}); err != nil {
		t.Fatalf("CollectAndExecuteSharedTargetPlan: %v", err)
	}
	mustCreateLinks(t, "Claude", NewClaude(), fixtureProject, repo)

	assertSymlinkTarget(t, filepath.Join(repo, dirAgents, "skills", "review"), skillDir)
	assertSymlinkTarget(t, filepath.Join(repo, dirClaude, "skills", "review"), skillDir)
}

func TestClaudeCreateLinksSymlinksGlobalAgentsIntoUserHome(t *testing.T) {
	paths := newPlatformTestPaths(t)
	agentsHome := paths.agentsHome
	home := paths.home
	repo := paths.repo

	globalAgentDir := filepath.Join(agentsHome, "agents", "global", "reviewer")
	writeTextFile(t, filepath.Join(globalAgentDir, "AGENT.md"), "# Reviewer\n")
	mkdirAll(t, repo)

	mustCreateLinks(t, "Claude", NewClaude(), fixtureProject, repo)

	assertSymlinkTarget(t, filepath.Join(home, dirClaude, "agents", "reviewer"), globalAgentDir)
}

func TestClaudeCreateLinksSymlinksProjectAgentsIntoRepoMirrors(t *testing.T) {
	paths := newPlatformTestPaths(t)
	agentsHome := paths.agentsHome
	repo := paths.repo

	projectAgentDir := filepath.Join(agentsHome, "agents", fixtureProject, "docbot")
	writeTextFile(t, filepath.Join(projectAgentDir, "AGENT.md"), "# Docbot\n")
	mkdirAll(t, repo)

	if err := CollectAndExecuteSharedTargetPlan(fixtureProject, repo, []Platform{NewClaude()}); err != nil {
		t.Fatalf("CollectAndExecuteSharedTargetPlan: %v", err)
	}
	mustCreateLinks(t, "Claude", NewClaude(), fixtureProject, repo)

	assertSymlinkTarget(t, filepath.Join(repo, dirClaude, "agents", "docbot"), projectAgentDir)
	assertSymlinkTarget(t, filepath.Join(repo, dirAgents, "agents", "docbot"), projectAgentDir)
}

func TestCursorCreateLinksHardlinksAndMCPSelection(t *testing.T) {
	paths := newPlatformTestPaths(t)
	agentsHome := paths.agentsHome
	repo := paths.repo

	globalRule := filepath.Join(agentsHome, "rules", "global", "rules.mdc")
	projectRule := filepath.Join(agentsHome, "rules", "proj", "lint.mdc")
	cursorSettings := filepath.Join(agentsHome, "settings", "proj", fileCursorJSON)
	cursorMCP := filepath.Join(agentsHome, "mcp", "proj", fileCursorJSON)
	fallbackMCP := filepath.Join(agentsHome, "mcp", "proj", fileMCPJSON)
	cursorIgnore := filepath.Join(agentsHome, "settings", "proj", "cursorignore")
	cursorHooks := filepath.Join(agentsHome, "hooks", "proj", fileCursorJSON)

	writeTextFile(t, globalRule, "---\ndescription: global rules\n---\n")
	writeTextFile(t, projectRule, "---\ndescription: lint\n---\n")
	writeTextFile(t, cursorSettings, "{}\n")
	writeTextFile(t, cursorMCP, "{\"cursor\":true}\n")
	writeTextFile(t, fallbackMCP, "{\"mcp\":true}\n")
	writeTextFile(t, cursorIgnore, "node_modules\n")
	writeTextFile(t, cursorHooks, "{\"hooks\":[]}\n")
	mkdirAll(t, repo)

	mustCreateLinks(t, "Cursor", NewCursor(), fixtureProject, repo)

	assertHardlinked(t, filepath.Join(repo, dirCursor, "rules", "global--rules.mdc"), globalRule)
	assertHardlinked(t, filepath.Join(repo, dirCursor, "rules", "proj--lint.mdc"), projectRule)
	assertHardlinked(t, filepath.Join(repo, dirCursor, fileSettingsJSON), cursorSettings)
	assertHardlinked(t, filepath.Join(repo, dirCursor, fileMCPJSON), cursorMCP)
	assertHardlinked(t, filepath.Join(repo, ".cursorignore"), cursorIgnore)
	assertHardlinked(t, filepath.Join(repo, dirCursor, fileHooksJSON), cursorHooks)
}

func TestCursorRemoveLinksRemovesManagedRuleHardlinks(t *testing.T) {
	paths := newPlatformTestPaths(t)
	agentsHome := paths.agentsHome
	repo := paths.repo

	globalRule := filepath.Join(agentsHome, "rules", "global", "rules.md")
	projectRule := filepath.Join(agentsHome, "rules", "proj", "lint.mdc")
	writeTextFile(t, globalRule, "---\ndescription: global rules\n---\n")
	writeTextFile(t, projectRule, "---\ndescription: lint\n---\n")
	mkdirAll(t, repo)

	mustCreateLinks(t, "Cursor", NewCursor(), fixtureProject, repo)
	mustRemoveLinks(t, "Cursor", NewCursor(), fixtureProject, repo)

	assertNoFile(t, filepath.Join(repo, dirCursor, "rules", "global--rules.mdc"))
	assertNoFile(t, filepath.Join(repo, dirCursor, "rules", "proj--lint.mdc"))
}

func TestCursorCreateLinksMCPFallsBackToProjectGenericBeforeGlobalPlatformFile(t *testing.T) {
	paths := newPlatformTestPaths(t)
	agentsHome := paths.agentsHome
	repo := paths.repo

	projectGenericMCP := filepath.Join(agentsHome, "mcp", "proj", fileMCPJSON)
	globalCursorMCP := filepath.Join(agentsHome, "mcp", "global", fileCursorJSON)

	writeTextFile(t, projectGenericMCP, "{\"source\":\"project-generic\"}\n")
	writeTextFile(t, globalCursorMCP, "{\"source\":\"global-cursor\"}\n")
	mkdirAll(t, repo)

	mustCreateLinks(t, "Cursor", NewCursor(), fixtureProject, repo)

	assertHardlinked(t, filepath.Join(repo, dirCursor, fileMCPJSON), projectGenericMCP)
}

func TestCursorCreateLinksPrunesStaleManagedRuleFiles(t *testing.T) {
	paths := newPlatformTestPaths(t)
	agentsHome := paths.agentsHome
	repo := paths.repo

	globalRule := filepath.Join(agentsHome, "rules", "global", "rules.mdc")
	writeTextFile(t, globalRule, "---\ndescription: global rules\n---\n")
	mkdirAll(t, filepath.Join(repo, dirCursor, "rules"))
	writeTextFile(t, filepath.Join(repo, dirCursor, "rules", "global--agents.mdc"), "stale\n")
	writeTextFile(t, filepath.Join(repo, dirCursor, "rules", "proj--agents.mdc"), "stale\n")
	writeTextFile(t, filepath.Join(repo, dirCursor, "rules", "user-local.mdc"), "keep\n")

	mustCreateLinks(t, "Cursor", NewCursor(), fixtureProject, repo)

	assertNoFile(t, filepath.Join(repo, dirCursor, "rules", "global--agents.mdc"))
	assertNoFile(t, filepath.Join(repo, dirCursor, "rules", "proj--agents.mdc"))
	assertFileContains(t, filepath.Join(repo, dirCursor, "rules", "user-local.mdc"), "keep\n")
	assertHardlinked(t, filepath.Join(repo, dirCursor, "rules", "global--rules.mdc"), globalRule)
}

func TestCopilotCreateLinksMCPSelectionAndHookFanout(t *testing.T) {
	paths := newPlatformTestPaths(t)
	agentsHome := paths.agentsHome
	repo := paths.repo

	copilotMCP := filepath.Join(agentsHome, "mcp", "proj", "copilot.json")
	fallbackMCP := filepath.Join(agentsHome, "mcp", "proj", fileMCPJSON)
	hooksDir := filepath.Join(agentsHome, "hooks", "proj")
	settingsCompat := filepath.Join(agentsHome, "settings", "proj", fileClaudeCodeJSON)

	writeTextFile(t, copilotMCP, "{\"copilot\":true}\n")
	writeTextFile(t, fallbackMCP, "{\"mcp\":true}\n")
	writeTextFile(t, filepath.Join(hooksDir, fileClaudeCodeJSON), "{\"hooks\":[]}\n")
	writeTextFile(t, filepath.Join(hooksDir, filePreToolJSON), "{\"name\":\"pre-tool\"}\n")
	writeTextFile(t, filepath.Join(hooksDir, filePostSaveJSON), "{\"name\":\"post-save\"}\n")
	writeTextFile(t, filepath.Join(hooksDir, fileCursorJSON), "{\"name\":\"cursor\"}\n")
	writeTextFile(t, settingsCompat, "{\"settings\":true}\n")
	mkdirAll(t, repo)

	mustCreateLinks(t, "Copilot", NewCopilot(), fixtureProject, repo)

	assertSymlinkTarget(t, filepath.Join(repo, ".vscode", fileMCPJSON), copilotMCP)
	assertSymlinkTarget(t, filepath.Join(repo, dirClaude, fileSettingsLocalJSON), filepath.Join(hooksDir, fileClaudeCodeJSON))
	assertSymlinkTarget(t, filepath.Join(repo, dirGithub, "hooks", filePreToolJSON), filepath.Join(hooksDir, filePreToolJSON))
	assertSymlinkTarget(t, filepath.Join(repo, dirGithub, "hooks", filePostSaveJSON), filepath.Join(hooksDir, filePostSaveJSON))
	assertNoFile(t, filepath.Join(repo, dirGithub, "hooks", fileCursorJSON))
	assertNoFile(t, filepath.Join(repo, dirGithub, "hooks", fileClaudeCodeJSON))
}

func TestClaudeCreateLinksPrefersHooksOverSettingsAndUsesGlobalCompatForUser(t *testing.T) {
	paths := newPlatformTestPaths(t)
	agentsHome := paths.agentsHome
	home := paths.home
	repo := paths.repo

	projectHook := filepath.Join(agentsHome, "hooks", "proj", fileClaudeCodeJSON)
	projectSettings := filepath.Join(agentsHome, "settings", "proj", fileClaudeCodeJSON)
	globalHook := filepath.Join(agentsHome, "hooks", "global", fileClaudeCodeJSON)
	globalSettings := filepath.Join(agentsHome, "settings", "global", fileClaudeCodeJSON)

	writeTextFile(t, projectHook, "{\"source\":\"project-hook\"}\n")
	writeTextFile(t, projectSettings, "{\"source\":\"project-settings\"}\n")
	writeTextFile(t, globalHook, "{\"source\":\"global-hook\"}\n")
	writeTextFile(t, globalSettings, "{\"source\":\"global-settings\"}\n")
	mkdirAll(t, repo)

	mustCreateLinks(t, "Claude", NewClaude(), fixtureProject, repo)

	assertSymlinkTarget(t, filepath.Join(repo, dirClaude, fileSettingsLocalJSON), projectHook)
	assertSymlinkTarget(t, filepath.Join(home, dirClaude, fileSettingsJSON), globalHook)
}

func TestClaudeCreateLinksPrunesStaleProjectRuleSymlinks(t *testing.T) {
	paths := newPlatformTestPaths(t)
	agentsHome := paths.agentsHome
	repo := paths.repo

	projectRule := filepath.Join(agentsHome, "rules", "proj", "lint.mdc")
	writeTextFile(t, projectRule, "---\ndescription: lint\n---\n")
	mkdirAll(t, filepath.Join(repo, dirClaude, "rules"))
	writeTextFile(t, filepath.Join(repo, dirClaude, "rules", "proj--legacy.md"), "stale\n")
	writeTextFile(t, filepath.Join(repo, dirClaude, "rules", "user-local.md"), "keep\n")

	mustCreateLinks(t, "Claude", NewClaude(), fixtureProject, repo)

	assertNoFile(t, filepath.Join(repo, dirClaude, "rules", "proj--legacy.md"))
	assertFileContains(t, filepath.Join(repo, dirClaude, "rules", "user-local.md"), "keep\n")
	assertSymlinkTarget(t, filepath.Join(repo, dirClaude, "rules", "proj--lint.md"), projectRule)
}

func TestCursorCreateLinksPrefersProjectHooksForRepoAndGlobalForUser(t *testing.T) {
	paths := newPlatformTestPaths(t)
	agentsHome := paths.agentsHome
	home := paths.home
	repo := paths.repo

	globalHook := filepath.Join(agentsHome, "hooks", "global", fileCursorJSON)
	projectHook := filepath.Join(agentsHome, "hooks", "proj", fileCursorJSON)
	writeTextFile(t, globalHook, "{\"scope\":\"global\"}\n")
	writeTextFile(t, projectHook, "{\"scope\":\"project\"}\n")
	mkdirAll(t, repo)

	mustCreateLinks(t, "Cursor", NewCursor(), fixtureProject, repo)

	assertHardlinked(t, filepath.Join(repo, dirCursor, fileHooksJSON), projectHook)
	assertHardlinked(t, filepath.Join(home, dirCursor, fileHooksJSON), globalHook)
}

func TestCopilotCreateLinksClaudeCompatFallsBackToProjectSettingsBeforeGlobalHooks(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, dirAgents)
	home := filepath.Join(tmp, "home")
	repo := filepath.Join(tmp, "repo")

	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)

	projectSettings := filepath.Join(agentsHome, "settings", "proj", fileClaudeCodeJSON)
	globalHook := filepath.Join(agentsHome, "hooks", "global", fileClaudeCodeJSON)
	writeTextFile(t, projectSettings, "{\"source\":\"project-settings\"}\n")
	writeTextFile(t, globalHook, "{\"source\":\"global-hook\"}\n")
	mkdirAll(t, repo)

	mustCreateLinks(t, "Copilot", NewCopilot(), fixtureProject, repo)

	assertSymlinkTarget(t, filepath.Join(repo, dirClaude, fileSettingsLocalJSON), projectSettings)
}

func TestCodexCreateLinksPrefersProjectFallbackHookOverGlobalPrimary(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, dirAgents)
	home := filepath.Join(tmp, "home")
	repo := filepath.Join(tmp, "repo")

	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)

	globalPrimary := filepath.Join(agentsHome, "hooks", "global", "codex.json")
	projectFallback := filepath.Join(agentsHome, "hooks", "proj", "codex-hooks.json")
	writeTextFile(t, globalPrimary, "{\"source\":\"global-primary\"}\n")
	writeTextFile(t, projectFallback, "{\"source\":\"project-fallback\"}\n")
	mkdirAll(t, repo)

	mustCreateLinks(t, "Codex", NewCodex(), fixtureProject, repo)

	assertSymlinkTarget(t, filepath.Join(repo, dirCodex, fileHooksJSON), projectFallback)
	assertSymlinkTarget(t, filepath.Join(home, dirCodex, fileHooksJSON), projectFallback)
}

func TestCopilotCreateLinksPrefersProjectInstructions(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, dirAgents)
	home := filepath.Join(tmp, "home")
	repo := filepath.Join(tmp, "repo")

	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)

	globalFallbackRules := filepath.Join(agentsHome, "rules", "global", "rules.md")
	globalInstructions := filepath.Join(agentsHome, "rules", "global", fileCopilotInstructionsMD)
	projectInstructions := filepath.Join(agentsHome, "rules", "proj", fileCopilotInstructionsMD)

	writeTextFile(t, globalFallbackRules, "# Global Rules\n")
	writeTextFile(t, globalInstructions, "# Global Copilot Instructions\n")
	writeTextFile(t, projectInstructions, "# Project Copilot Instructions\n")
	mkdirAll(t, repo)

	mustCreateLinks(t, "Copilot", NewCopilot(), fixtureProject, repo)

	assertSymlinkTarget(t, filepath.Join(repo, dirGithub, fileCopilotInstructionsMD), projectInstructions)
}

func TestHookTranslationAcrossPlatformsUsesProjectHookSources(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, dirAgents)
	home := filepath.Join(tmp, "home")
	repo := filepath.Join(tmp, "repo")

	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)

	cursorHook := filepath.Join(agentsHome, "hooks", "proj", fileCursorJSON)
	codexHook := filepath.Join(agentsHome, "hooks", "proj", "codex.json")
	claudeCompatHook := filepath.Join(agentsHome, "hooks", "proj", fileClaudeCodeJSON)
	copilotProjectHook := filepath.Join(agentsHome, "hooks", "proj", filePreToolJSON)

	writeTextFile(t, cursorHook, "{\"hooks\":[\"cursor\"]}\n")
	writeTextFile(t, codexHook, "{\"hooks\":[\"codex\"]}\n")
	writeTextFile(t, claudeCompatHook, "{\"hooks\":[\"claude\"]}\n")
	writeTextFile(t, copilotProjectHook, "{\"name\":\"pre-tool\"}\n")
	mkdirAll(t, repo)

	platforms := []Platform{NewCursor(), NewCodex(), NewClaude(), NewCopilot()}
	if err := CollectAndExecuteSharedTargetPlan("proj", repo, platforms); err != nil {
		t.Fatalf("CollectAndExecuteSharedTargetPlan: %v", err)
	}
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

	assertHardlinked(t, filepath.Join(repo, dirCursor, fileHooksJSON), cursorHook)
	assertSymlinkTarget(t, filepath.Join(repo, dirCodex, fileHooksJSON), codexHook)
	assertSymlinkTarget(t, filepath.Join(home, dirCodex, fileHooksJSON), codexHook)
	assertSymlinkTarget(t, filepath.Join(repo, dirClaude, fileSettingsLocalJSON), claudeCompatHook)
	assertNoFile(t, filepath.Join(home, dirClaude, fileSettingsJSON))
	assertSymlinkTarget(t, filepath.Join(repo, dirGithub, "hooks", filePreToolJSON), copilotProjectHook)
	assertNoFile(t, filepath.Join(repo, dirGithub, "hooks", fileCursorJSON))
	assertNoFile(t, filepath.Join(repo, dirGithub, "hooks", fileClaudeCodeJSON))
}

func TestClaudeCompatTranslationFallsBackToSettingsBucket(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, dirAgents)
	home := filepath.Join(tmp, "home")
	repo := filepath.Join(tmp, "repo")

	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)

	projectSettings := filepath.Join(agentsHome, "settings", "proj", fileClaudeCodeJSON)
	globalSettings := filepath.Join(agentsHome, "settings", "global", fileClaudeCodeJSON)
	writeTextFile(t, projectSettings, "{\"scope\":\"project-settings\"}\n")
	writeTextFile(t, globalSettings, "{\"scope\":\"global-settings\"}\n")
	mkdirAll(t, repo)

	if err := CollectAndExecuteSharedTargetPlan("proj", repo, []Platform{NewClaude(), NewCopilot()}); err != nil {
		t.Fatalf("CollectAndExecuteSharedTargetPlan: %v", err)
	}
	if err := NewClaude().CreateLinks("proj", repo); err != nil {
		t.Fatalf("Claude CreateLinks failed: %v", err)
	}
	if err := NewCopilot().CreateLinks("proj", repo); err != nil {
		t.Fatalf("Copilot CreateLinks failed: %v", err)
	}

	assertSymlinkTarget(t, filepath.Join(repo, dirClaude, fileSettingsLocalJSON), projectSettings)
	assertSymlinkTarget(t, filepath.Join(home, dirClaude, fileSettingsJSON), globalSettings)
}

func TestClaudeCreateLinksRendersCanonicalHookBundles(t *testing.T) {
	paths := newPlatformTestPaths(t)
	agentsHome := paths.agentsHome
	home := paths.home
	repo := paths.repo

	projectHookDir := filepath.Join(agentsHome, "hooks", "proj", hookNameFormatWrite)
	globalHookDir := filepath.Join(agentsHome, "hooks", "global", hookNameSessionBanner)
	projectRunScript := writeHookBundleFixture(t, projectHookDir, hookBundleFixture{
		Name:            hookNameFormatWrite,
		When:            "pre_tool_use",
		MatchTools:      []string{"Write", "Edit"},
		MatchExpression: "Write | Edit",
		Command:         "./run.sh",
		EnabledOn:       []string{"claude"},
	})
	globalBannerScript := writeHookBundleFixture(t, globalHookDir, hookBundleFixture{
		Name:      hookNameSessionBanner,
		When:      "session_start",
		Command:   cmdBannerScript,
		EnabledOn: []string{"claude"},
	})
	mkdirAll(t, repo)

	mustCreateLinks(t, "Claude", NewClaude(), fixtureProject, repo)

	projectJSON := readJSONFile(t, filepath.Join(repo, dirClaude, fileSettingsLocalJSON))
	userJSON := readJSONFile(t, filepath.Join(home, dirClaude, fileSettingsJSON))

	assertJSONPathEquals(t, projectJSON, "hooks.PreToolUse.0.matcher", "Write | Edit")
	assertJSONPathEquals(t, projectJSON, "hooks.PreToolUse.0.hooks.0.type", "command")
	assertJSONPathEquals(t, projectJSON, "hooks.PreToolUse.0.hooks.0.command", projectRunScript)
	assertJSONPathEquals(t, userJSON, "hooks.SessionStart.0.hooks.0.command", globalBannerScript)
}

func TestClaudeRemoveLinksRemovesRenderedCanonicalHookSettings(t *testing.T) {
	paths := newPlatformTestPaths(t)
	agentsHome := paths.agentsHome
	repo := paths.repo

	projectHookDir := filepath.Join(agentsHome, "hooks", "proj", hookNameFormatWrite)
	writeHookBundleFixture(t, projectHookDir, hookBundleFixture{
		Name:      hookNameFormatWrite,
		When:      "pre_tool_use",
		Command:   "./run.sh",
		EnabledOn: []string{"claude"},
	})
	mkdirAll(t, repo)

	mustCreateLinks(t, "Claude", NewClaude(), fixtureProject, repo)
	mustRemoveLinks(t, "Claude", NewClaude(), fixtureProject, repo)

	assertNoFile(t, filepath.Join(repo, dirClaude, fileSettingsLocalJSON))
}

func TestClaudeCreateLinksPrunesGlobalRenderedUserSettingsWhenCanonicalHooksDisappear(t *testing.T) {
	paths := newPlatformTestPaths(t)
	agentsHome := paths.agentsHome
	home := paths.home
	repo := paths.repo

	globalHookDir := filepath.Join(agentsHome, "hooks", "global", hookNameSessionBanner)
	manifestPath := filepath.Join(globalHookDir, hookManifestName)
	writeHookBundleFixture(t, globalHookDir, hookBundleFixture{
		Name:      hookNameSessionBanner,
		When:      "session_start",
		Command:   cmdBannerScript,
		EnabledOn: []string{"claude"},
	})
	mkdirAll(t, repo)

	mustCreateLinks(t, "Claude", NewClaude(), fixtureProject, repo)
	if err := os.Remove(manifestPath); err != nil {
		t.Fatalf("remove manifest: %v", err)
	}
	mustCreateLinks(t, "Claude", NewClaude(), fixtureProject, repo)

	assertNoFile(t, filepath.Join(home, dirClaude, fileSettingsJSON))
	assertNoFile(t, filepath.Join(repo, dirClaude, fileSettingsLocalJSON))
}

func TestCursorAndCodexCreateLinksRenderCanonicalHookBundles(t *testing.T) {
	paths := newPlatformTestPaths(t)
	agentsHome := paths.agentsHome
	home := paths.home
	repo := paths.repo

	projectHookDir := filepath.Join(agentsHome, "hooks", "proj", hookNameBashGuard)
	globalHookDir := filepath.Join(agentsHome, "hooks", "global", hookNameSessionBanner)
	projectGuardScript := writeHookBundleFixture(t, projectHookDir, hookBundleFixture{
		Name:       hookNameBashGuard,
		When:       "pre_tool_use",
		MatchTools: []string{"Bash"},
		Command:    cmdGuardScript,
		EnabledOn:  []string{"cursor", "codex"},
	})
	globalBannerScript := writeHookBundleFixture(t, globalHookDir, hookBundleFixture{
		Name:      hookNameSessionBanner,
		When:      "session_start",
		Command:   cmdBannerScript,
		EnabledOn: []string{"cursor", "codex"},
	})
	mkdirAll(t, repo)

	mustCreateLinks(t, "Cursor", NewCursor(), fixtureProject, repo)
	mustCreateLinks(t, "Codex", NewCodex(), fixtureProject, repo)

	cursorProject := readJSONFile(t, filepath.Join(repo, dirCursor, fileHooksJSON))
	cursorUser := readJSONFile(t, filepath.Join(home, dirCursor, fileHooksJSON))
	codexProject := readJSONFile(t, filepath.Join(repo, dirCodex, fileHooksJSON))
	codexUser := readJSONFile(t, filepath.Join(home, dirCodex, fileHooksJSON))

	assertJSONPathEquals(t, cursorProject, "version", float64(1))
	assertJSONPathEquals(t, cursorProject, "hooks.preToolUse.0.command", projectGuardScript)
	assertJSONPathEquals(t, cursorProject, "hooks.preToolUse.0.matcher", "Bash")
	assertJSONPathEquals(t, cursorUser, "hooks.sessionStart.0.command", globalBannerScript)

	assertJSONPathEquals(t, codexProject, "hooks.PreToolUse.0.matcher", "Bash")
	assertJSONPathEquals(t, codexProject, "hooks.PreToolUse.0.hooks.0.command", projectGuardScript)
	assertJSONPathEquals(t, codexUser, "hooks.SessionStart.0.hooks.0.command", globalBannerScript)
}

func TestCursorAndCodexRemoveLinksRemoveRenderedCanonicalHookFiles(t *testing.T) {
	paths := newPlatformTestPaths(t)
	agentsHome := paths.agentsHome
	repo := paths.repo

	projectHookDir := filepath.Join(agentsHome, "hooks", "proj", hookNameBashGuard)
	writeHookBundleFixture(t, projectHookDir, hookBundleFixture{
		Name:       hookNameBashGuard,
		When:       "pre_tool_use",
		MatchTools: []string{"Bash"},
		Command:    cmdGuardScript,
		EnabledOn:  []string{"cursor", "codex"},
	})
	mkdirAll(t, repo)

	mustCreateLinks(t, "Cursor", NewCursor(), fixtureProject, repo)
	mustCreateLinks(t, "Codex", NewCodex(), fixtureProject, repo)
	mustRemoveLinks(t, "Cursor", NewCursor(), fixtureProject, repo)
	mustRemoveLinks(t, "Codex", NewCodex(), fixtureProject, repo)

	assertNoFile(t, filepath.Join(repo, dirCursor, fileHooksJSON))
	assertNoFile(t, filepath.Join(repo, dirCodex, fileHooksJSON))
}

func TestCursorAndCodexCreateLinksPruneRenderedFilesWhenCanonicalHooksDisappear(t *testing.T) {
	paths := newPlatformTestPaths(t)
	agentsHome := paths.agentsHome
	repo := paths.repo

	projectHookDir := filepath.Join(agentsHome, "hooks", "proj", hookNameBashGuard)
	manifestPath := filepath.Join(projectHookDir, hookManifestName)
	writeHookBundleFixture(t, projectHookDir, hookBundleFixture{
		Name:       hookNameBashGuard,
		When:       "pre_tool_use",
		MatchTools: []string{"Bash"},
		Command:    cmdGuardScript,
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

	assertNoFile(t, filepath.Join(repo, dirCursor, fileHooksJSON))
	assertNoFile(t, filepath.Join(repo, dirCodex, fileHooksJSON))
}

func TestCopilotCreateLinksRendersCanonicalHookBundles(t *testing.T) {
	paths := newPlatformTestPaths(t)
	agentsHome := paths.agentsHome
	repo := paths.repo

	projectHookDir := filepath.Join(agentsHome, "hooks", "proj", hookNamePromptLog)
	globalHookDir := filepath.Join(agentsHome, "hooks", "global", hookNameSessionBanner)
	projectPromptScript := writeHookBundleFixture(t, projectHookDir, hookBundleFixture{
		Name:      hookNamePromptLog,
		When:      "user_prompt_submit",
		Command:   cmdPromptLogScript,
		EnabledOn: []string{"copilot"},
	})
	globalBannerScript := writeHookBundleFixture(t, globalHookDir, hookBundleFixture{
		Name:      hookNameSessionBanner,
		When:      "session_start",
		Command:   cmdBannerScript,
		EnabledOn: []string{"copilot"},
	})
	mkdirAll(t, repo)

	mustCreateLinks(t, "Copilot", NewCopilot(), fixtureProject, repo)

	sessionFile := readJSONFile(t, filepath.Join(repo, dirGithub, "hooks", hookNameSessionBanner+".json"))
	promptFile := readJSONFile(t, filepath.Join(repo, dirGithub, "hooks", filePromptLogJSON))
	compatFile := readJSONFile(t, filepath.Join(repo, dirClaude, fileSettingsLocalJSON))

	assertJSONPathEquals(t, sessionFile, "version", float64(1))
	assertJSONPathEquals(t, sessionFile, "hooks.sessionStart.0.type", "command")
	assertJSONPathEquals(t, sessionFile, "hooks.sessionStart.0.bash", globalBannerScript)
	assertJSONPathEquals(t, promptFile, "hooks.userPromptSubmitted.0.bash", projectPromptScript)
	assertJSONPathEquals(t, compatFile, "hooks.UserPromptSubmit.0.hooks.0.command", projectPromptScript)
}

func TestCopilotRemoveLinksRemovesRenderedCanonicalHookFiles(t *testing.T) {
	paths := newPlatformTestPaths(t)
	agentsHome := paths.agentsHome
	repo := paths.repo

	projectHookDir := filepath.Join(agentsHome, "hooks", "proj", hookNamePromptLog)
	writeHookBundleFixture(t, projectHookDir, hookBundleFixture{
		Name:      hookNamePromptLog,
		When:      "user_prompt_submit",
		Command:   cmdPromptLogScript,
		EnabledOn: []string{"copilot"},
	})
	mkdirAll(t, repo)

	mustCreateLinks(t, "Copilot", NewCopilot(), fixtureProject, repo)
	mustRemoveLinks(t, "Copilot", NewCopilot(), fixtureProject, repo)

	assertNoFile(t, filepath.Join(repo, dirClaude, fileSettingsLocalJSON))
	assertNoFile(t, filepath.Join(repo, dirGithub, "hooks", filePromptLogJSON))
}

func TestCopilotCreateLinksPrunesStaleRenderedHookFanout(t *testing.T) {
	paths := newPlatformTestPaths(t)
	agentsHome := paths.agentsHome
	repo := paths.repo

	projectHookDir := filepath.Join(agentsHome, "hooks", "proj", hookNamePromptLog)
	projectPromptScript := writeHookBundleFixture(t, projectHookDir, hookBundleFixture{
		Name:      hookNamePromptLog,
		When:      "user_prompt_submit",
		Command:   cmdPromptLogScript,
		EnabledOn: []string{"copilot"},
	})
	writeTextFile(t, filepath.Join(repo, dirGithub, "hooks", "stale.json"), `{
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

	assertNoFile(t, filepath.Join(repo, dirGithub, "hooks", "stale.json"))
	assertJSONPathEquals(t, readJSONFile(t, filepath.Join(repo, dirGithub, "hooks", filePromptLogJSON)), "hooks.userPromptSubmitted.0.bash", projectPromptScript)
}

func newPlatformTestPaths(t *testing.T) platformTestPaths {
	t.Helper()
	tmp := t.TempDir()
	paths := platformTestPaths{
		agentsHome: filepath.Join(tmp, dirAgents),
		home:       filepath.Join(tmp, "home"),
		repo:       filepath.Join(tmp, "repo"),
	}
	t.Setenv("AGENTS_HOME", paths.agentsHome)
	t.Setenv("HOME", paths.home)
	return paths
}

func mustCreateLinks(t *testing.T, label string, p Platform, project, repo string) {
	t.Helper()
	if err := CollectAndExecuteSharedTargetPlan(project, repo, []Platform{p}); err != nil {
		t.Fatalf("%s CollectAndExecuteSharedTargetPlan: %v", label, err)
	}
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

func assertFileContains(t *testing.T, path, want string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(content) != want {
		t.Fatalf("expected %s to contain %q, got %q", path, want, string(content))
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
