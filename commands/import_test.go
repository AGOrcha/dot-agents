package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"go.yaml.in/yaml/v3"
)

const (
	canonicalImportProject = "proj"
	promptLogJSON          = "prompt-log.json"
	yamlUnmarshalFailedFmt = "yaml.Unmarshal failed: %v\n%s"
)

func TestMapGlobalRelToDest(t *testing.T) {
	cases := []struct {
		rel  string
		want string
	}{
		{".claude/settings.json", "settings/global/claude-code.json"},
		{".cursor/settings.json", "settings/global/cursor.json"},
		{".cursor/mcp.json", "mcp/global/mcp.json"},
		{".claude/CLAUDE.md", "rules/global/agents.md"},
		{".codex/config.toml", "settings/global/codex.toml"},
		{".codex/hooks.json", "hooks/global/codex.json"},
		{".cursor/hooks.json", "hooks/global/cursor.json"},
		{".unknown", ""},
	}

	for _, c := range cases {
		got := mapGlobalRelToDest(c.rel)
		if got != c.want {
			t.Fatalf("mapGlobalRelToDest(%q)=%q, want %q", c.rel, got, c.want)
		}
	}
}

func TestMapResourceRelToDestHooks(t *testing.T) {
	project := "my-project"
	cases := []struct {
		rel  string
		want string
	}{
		{relCursorHooksJSON, agentsHooksPrefix + project + "/cursor.json"},
		{relCodexHooksJSON, agentsHooksPrefix + project + "/codex.json"},
		{".github/hooks/pre-tool.json", agentsHooksPrefix + project + "/pre-tool/HOOK.yaml"},
		{".github/hooks/post-save.json", agentsHooksPrefix + project + "/post-save/HOOK.yaml"},
	}

	for _, c := range cases {
		got := mapResourceRelToDest(project, c.rel)
		if got != c.want {
			t.Fatalf("mapResourceRelToDest(%q, %q)=%q, want %q", project, c.rel, got, c.want)
		}
	}
}

func TestCanonicalHookBundleContentFromCopilotFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, promptLogJSON)
	if err := os.WriteFile(src, []byte(`{
  "version": 1,
  "hooks": {
    "userPromptSubmitted": [
      {
        "type": "command",
        "bash": "./prompt-log.sh",
        "timeoutSec": 5
      }
    ]
  }
}
`), 0644); err != nil {
		t.Fatal(err)
	}

	content, err := canonicalHookBundleContentFromCopilotFile(src, "prompt-log")
	if err != nil {
		t.Fatalf("canonicalHookBundleContentFromCopilotFile failed: %v", err)
	}

	var manifest map[string]any
	if err := yaml.Unmarshal(content, &manifest); err != nil {
		t.Fatalf(yamlUnmarshalFailedFmt, err, string(content))
	}

	if got := manifest["name"]; got != "prompt-log" {
		t.Fatalf("name = %#v, want prompt-log", got)
	}
	if got := manifest["when"]; got != "user_prompt_submit" {
		t.Fatalf("when = %#v, want user_prompt_submit", got)
	}
	run, ok := manifest["run"].(map[string]any)
	if !ok {
		t.Fatalf("run missing from manifest: %#v", manifest)
	}
	if got := run["command"]; got != "./prompt-log.sh" {
		t.Fatalf("run.command = %#v, want ./prompt-log.sh", got)
	}
	if got := run["timeout_ms"]; got != 5000 {
		t.Fatalf("run.timeout_ms = %#v, want 5000", got)
	}
}

func TestCanonicalImportOutputsFromCursorHooksJSON(t *testing.T) {
	outputs, ok := canonicalImportFromJSON(t, relCursorHooksJSON, `{
  "version": 1,
  "hooks": {
    "preToolUse": [
      {
        "command": "./guard.sh",
        "matcher": "Bash",
        "timeout": 7
      }
    ]
  }
}
	`)
	assertSingleCanonicalOutput(t, outputs, ok, "hooks/proj/pre-tool-use-guard/HOOK.yaml")

	manifest := mustUnmarshalYAMLMap(t, outputs[0].content)
	if got := manifest["when"]; got != "pre_tool_use" {
		t.Fatalf("when = %#v, want pre_tool_use", got)
	}
	run := manifest["run"].(map[string]any)
	if got := run["command"]; got != "./guard.sh" {
		t.Fatalf("run.command = %#v, want ./guard.sh", got)
	}
	if got := run["timeout_ms"]; got != 7000 {
		t.Fatalf("run.timeout_ms = %#v, want 7000", got)
	}
}

func TestCanonicalImportOutputsFromCodexHooksJSON(t *testing.T) {
	outputs, ok := canonicalImportFromJSON(t, relCodexHooksJSON, `{
  "hooks": {
    "SessionStart": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "./banner.sh"
          }
        ]
      }
    ]
  }
}
	`)
	assertSingleCanonicalOutput(t, outputs, ok, "hooks/proj/session-start-banner/HOOK.yaml")
}

func TestCanonicalImportOutputsFromClaudeCompatSettings(t *testing.T) {
	outputs, ok := canonicalImportFromJSON(t, relClaudeSettingsLocal, `{
  "$schema": "https://json.schemastore.org/claude-code-settings.json",
  "hooks": {
    "SessionStart": [
      {
        "matcher": "Write|Edit",
        "hooks": [
          {
            "type": "command",
            "command": "./banner.sh"
          }
        ]
      }
    ]
  }
}
	`)
	assertSingleCanonicalOutput(t, outputs, ok, "hooks/proj/session-start-banner/HOOK.yaml")

	manifest := mustUnmarshalYAMLMap(t, outputs[0].content)
	if got := manifest["when"]; got != "session_start" {
		t.Fatalf("when = %#v, want session_start", got)
	}
	if manifest["enabled_on"] == nil {
		t.Fatalf("expected enabled_on in manifest")
	}
}

func TestCanonicalImportOutputsAssignsDistinctNamesForGenericCommandsUsingMatchers(t *testing.T) {
	outputs, ok := canonicalImportFromJSON(t, relCursorHooksJSON, `{
  "version": 1,
  "hooks": {
    "preToolUse": [
      {
        "command": "./run.sh",
        "matcher": "Write|Edit"
      },
      {
        "command": "./run.sh",
        "matcher": "Bash"
      }
    ]
  }
}
	`)
	assertTwoCanonicalOutputs(t, outputs, ok,
		"hooks/proj/pre-tool-use-write-edit-run/HOOK.yaml",
		"hooks/proj/pre-tool-use-bash-run/HOOK.yaml",
	)
}

func TestCanonicalImportOutputsAppendsStableSuffixForDuplicateIdentity(t *testing.T) {
	outputs, ok := canonicalImportFromJSON(t, relCodexHooksJSON, `{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Write",
        "hooks": [
          {
            "type": "command",
            "command": "./guard.sh"
          }
        ]
      },
      {
        "matcher": "Write",
        "hooks": [
          {
            "type": "command",
            "command": "./guard.sh"
          }
        ]
      }
    ]
  }
}
	`)
	assertTwoCanonicalOutputs(t, outputs, ok,
		"hooks/proj/pre-tool-use-guard/HOOK.yaml",
		"hooks/proj/pre-tool-use-guard-2/HOOK.yaml",
	)
}

func TestCanonicalImportOutputsSplitsMultipleActionsIntoDistinctCanonicalHooks(t *testing.T) {
	outputs, ok := canonicalImportFromJSON(t, relClaudeSettingsLocal, `{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Write|Edit",
        "hooks": [
          {
            "type": "command",
            "command": "./lint.sh"
          },
          {
            "type": "command",
            "command": "./format.sh"
          }
        ]
      }
    ]
  }
}
	`)
	assertTwoCanonicalOutputs(t, outputs, ok,
		"hooks/proj/pre-tool-use-lint/HOOK.yaml",
		"hooks/proj/pre-tool-use-format/HOOK.yaml",
	)
}

func TestCanonicalImportOutputsCanonicalizesMultiActionCopilotFanoutUsingFilenameHint(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, relGitHubHooksDir, promptLogJSON)
	if err := os.MkdirAll(filepath.Dir(src), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte(`{
  "version": 1,
  "hooks": {
    "userPromptSubmitted": [
      {
        "type": "command",
        "bash": "./prompt-log.sh"
      },
      {
        "type": "command",
        "bash": "./second.sh"
      }
    ]
  }
}
`), 0644); err != nil {
		t.Fatal(err)
	}

	outputs, ok := canonicalImportFromPath(t, dir, src)
	assertTwoCanonicalOutputs(t, outputs, ok,
		"hooks/proj/prompt-log/HOOK.yaml",
		"hooks/proj/prompt-log-second/HOOK.yaml",
	)
}

func TestCanonicalImportOutputsFallsBackToLegacyWhenCopilotEventIsUnknown(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, relGitHubHooksDir, promptLogJSON)
	if err := os.MkdirAll(filepath.Dir(src), 0755); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{
  "version": 1,
  "hooks": {
    "unknownEvent": [
      {
        "type": "command",
        "bash": "./prompt-log.sh"
      }
    ]
  }
}
`)
	if err := os.WriteFile(src, raw, 0644); err != nil {
		t.Fatal(err)
	}

	outputs, ok := canonicalImportFromPath(t, dir, src)
	if !ok || len(outputs) != 1 {
		t.Fatalf("expected one fallback output, ok=%v len=%d", ok, len(outputs))
	}
	if got, want := outputs[0].destRel, "hooks/proj/prompt-log.json"; got != want {
		t.Fatalf("destRel = %q, want %q", got, want)
	}
	if string(outputs[0].content) != string(raw) {
		t.Fatalf("expected raw legacy fallback content to be preserved")
	}
}

func TestCanonicalImportOutputsUsesMatcherHintForGenericCommandName(t *testing.T) {
	outputs, ok := canonicalImportFromJSON(t, relCursorHooksJSON, `{
  "version": 1,
  "hooks": {
    "preToolUse": [
      {
        "command": "./run.sh",
        "matcher": "Bash"
      }
    ]
  }
}
	`)
	assertSingleCanonicalOutput(t, outputs, ok, "hooks/proj/pre-tool-use-bash-run/HOOK.yaml")
}

func TestCanonicalImportOutputsPreservesRawMatcherOverrideWhenNormalized(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, relClaudeSettingsLocal)
	if err := os.MkdirAll(filepath.Dir(src), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte(`{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Write | Edit",
        "hooks": [
          {
            "type": "command",
            "command": "./guard.sh"
          }
        ]
      }
    ]
  }
}
`), 0644); err != nil {
		t.Fatal(err)
	}

	outputs, ok := canonicalImportFromPath(t, dir, src)
	if !ok || len(outputs) != 1 {
		t.Fatalf("expected one canonical output, ok=%v len=%d", ok, len(outputs))
	}

	manifest := mustUnmarshalYAMLMap(t, outputs[0].content)
	match, ok := manifest["match"].(map[string]any)
	if !ok {
		t.Fatalf("expected match section in manifest: %#v", manifest)
	}
	tools, ok := match["tools"].([]any)
	if !ok || len(tools) != 2 || tools[0] != "Write" || tools[1] != "Edit" {
		t.Fatalf("match.tools = %#v, want [Write Edit]", match["tools"])
	}
	if got := match["expression"]; got != "Write | Edit" {
		t.Fatalf("match.expression = %#v, want %q", got, "Write | Edit")
	}
}

func TestRestoreFromResourcesCountedCanonicalizesGitHubHooks(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	resourceFile := filepath.Join(agentsHome, "resources", "proj", ".github", "hooks", "pre-tool.json")
	if err := os.MkdirAll(filepath.Dir(resourceFile), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(resourceFile, []byte(`{
  "version": 1,
  "hooks": {
    "preToolUse": [
      {
        "type": "command",
        "bash": "./guard.sh"
      }
    ]
  }
}
`), 0644); err != nil {
		t.Fatal(err)
	}

	restored := restoreFromResourcesCounted("proj", filepath.Join(tmp, "repo"))
	if restored != 1 {
		t.Fatalf("restoreFromResourcesCounted restored %d files, want 1", restored)
	}

	dest := filepath.Join(agentsHome, "hooks", "proj", "pre-tool", "HOOK.yaml")
	content, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("expected canonical hook bundle at %s: %v", dest, err)
	}
	var manifest map[string]any
	if err := yaml.Unmarshal(content, &manifest); err != nil {
		t.Fatalf(yamlUnmarshalFailedFmt, err, string(content))
	}
	if got := manifest["name"]; got != "pre-tool" {
		t.Fatalf("name = %#v, want pre-tool", got)
	}
	if got := manifest["when"]; got != "pre_tool_use" {
		t.Fatalf("when = %#v, want pre_tool_use", got)
	}
}

func TestRestoreFromResourcesCountedCanonicalizesCursorHooks(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	resourceFile := filepath.Join(agentsHome, "resources", "proj", relCursorHooksJSON)
	if err := os.MkdirAll(filepath.Dir(resourceFile), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(resourceFile, []byte(`{
  "version": 1,
  "hooks": {
    "preToolUse": [
      {
        "command": "./guard.sh",
        "matcher": "Bash"
      }
    ]
  }
}
`), 0644); err != nil {
		t.Fatal(err)
	}

	restored := restoreFromResourcesCounted("proj", filepath.Join(tmp, "repo"))
	if restored != 1 {
		t.Fatalf("restoreFromResourcesCounted restored %d files, want 1", restored)
	}

	dest := filepath.Join(agentsHome, "hooks", "proj", "pre-tool-use-guard", "HOOK.yaml")
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("expected canonical hook bundle at %s: %v", dest, err)
	}
}

func TestFilesDifferent(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	c := filepath.Join(dir, "c.txt")
	if err := os.WriteFile(a, []byte("same"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("same"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c, []byte("different"), 0644); err != nil {
		t.Fatal(err)
	}

	same, err := filesDifferent(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if same {
		t.Fatalf("expected equal files")
	}

	diff, err := filesDifferent(a, c)
	if err != nil {
		t.Fatal(err)
	}
	if !diff {
		t.Fatalf("expected different files")
	}
}

func canonicalImportFromJSON(t *testing.T, relPath, content string) ([]importOutput, bool) {
	t.Helper()
	sourceRoot := t.TempDir()
	sourcePath := filepath.Join(sourceRoot, relPath)
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return canonicalImportFromPath(t, sourceRoot, sourcePath)
}

func canonicalImportFromPath(t *testing.T, sourceRoot, sourcePath string) ([]importOutput, bool) {
	t.Helper()
	outputs, ok, err := canonicalImportOutputs(importCandidate{
		project:    canonicalImportProject,
		sourceRoot: sourceRoot,
		sourcePath: sourcePath,
	})
	if err != nil {
		t.Fatalf("canonicalImportOutputs failed: %v", err)
	}
	return outputs, ok
}

func assertSingleCanonicalOutput(t *testing.T, outputs []importOutput, ok bool, wantDest string) {
	t.Helper()
	if !ok || len(outputs) != 1 {
		t.Fatalf("expected one canonical output, ok=%v len=%d", ok, len(outputs))
	}
	if got := outputs[0].destRel; got != wantDest {
		t.Fatalf("destRel = %q, want %q", got, wantDest)
	}
}

func assertTwoCanonicalOutputs(t *testing.T, outputs []importOutput, ok bool, wantFirst, wantSecond string) {
	t.Helper()
	if !ok || len(outputs) != 2 {
		t.Fatalf("expected two canonical outputs, ok=%v len=%d", ok, len(outputs))
	}
	if got := outputs[0].destRel; got != wantFirst {
		t.Fatalf("first destRel = %q, want %q", got, wantFirst)
	}
	if got := outputs[1].destRel; got != wantSecond {
		t.Fatalf("second destRel = %q, want %q", got, wantSecond)
	}
}

func mustUnmarshalYAMLMap(t *testing.T, content []byte) map[string]any {
	t.Helper()
	var manifest map[string]any
	if err := yaml.Unmarshal(content, &manifest); err != nil {
		t.Fatalf(yamlUnmarshalFailedFmt, err, string(content))
	}
	return manifest
}

// ---------- NewImportCmd metadata ----------

func TestNewImportCmd_FlagsAndArgs(t *testing.T) {
	cmd := NewImportCmd()
	if cmd.Flags().Lookup("scope") == nil {
		t.Error("missing --scope flag")
	}
	if err := cmd.Args(cmd, nil); err != nil {
		t.Errorf("import should accept zero args, got: %v", err)
	}
	if err := cmd.Args(cmd, []string{"only"}); err != nil {
		t.Errorf("import should accept one arg, got: %v", err)
	}
	if err := cmd.Args(cmd, []string{"a", "b"}); err == nil {
		t.Error("import should reject multiple args")
	}
}

// ---------- normalizeImportScope ----------

func TestNormalizeImportScope(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"project", importScopeProject, false},
		{"global", importScopeGlobal, false},
		{"all", importScopeAll, false},
		{"  Project  ", importScopeProject, false},
		{"ALL", importScopeAll, false},
		{"bogus", "", true},
		{"", "", true},
	}
	for _, c := range cases {
		got, err := normalizeImportScope(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("normalizeImportScope(%q): expected error", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("normalizeImportScope(%q): unexpected error: %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("normalizeImportScope(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// ---------- sortImportCandidates ----------

func TestSortImportCandidates_StableByProjectAndSource(t *testing.T) {
	candidates := []importCandidate{
		{project: "b", sourcePath: "/a/x"},
		{project: "a", sourcePath: "/b/y"},
		{project: "a", sourcePath: "/a/z"},
	}
	sortImportCandidates(candidates)
	if candidates[0].project != "a" || candidates[0].sourcePath != "/a/z" {
		t.Errorf("first = %+v, want a /a/z", candidates[0])
	}
	if candidates[1].project != "a" || candidates[1].sourcePath != "/b/y" {
		t.Errorf("second = %+v, want a /b/y", candidates[1])
	}
	if candidates[2].project != "b" {
		t.Errorf("third = %+v, want b", candidates[2])
	}
}

// ---------- collectImportCandidates: scope filtering ----------

func TestCollectImportCandidates_ScopeFiltering(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projPath := filepath.Join(tmp, "proj")
	os.MkdirAll(projPath, 0755)
	// Project file: AGENTS.md
	os.WriteFile(filepath.Join(projPath, relAgentsMD), []byte("# rules"), 0644)

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("proj", projPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	// Scope=global → no project candidates
	candidates, projectSet, err := collectImportCandidates(cfg, "", importScopeGlobal)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	for _, c := range candidates {
		if c.project == "proj" {
			t.Errorf("global scope should not include project candidate: %+v", c)
		}
	}
	if projectSet["proj"] {
		t.Error("global scope: projectSet should not include proj")
	}

	// Scope=project → must include proj
	candidates, projectSet, err = collectImportCandidates(cfg, "", importScopeProject)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if len(candidates) == 0 {
		t.Fatal("expected project candidates")
	}
	if !projectSet["proj"] {
		t.Error("projectSet should include proj")
	}

	// Scope=project with filter for unknown project → error
	if _, _, err := collectImportCandidates(cfg, "ghost", importScopeProject); err == nil {
		t.Error("expected error for unknown project filter")
	}
}

// ---------- scanProjectImportCandidates: filter unknown ----------

func TestScanProjectImportCandidates_UnknownFilter(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	_, err := scanProjectImportCandidates(cfg, "ghost")
	if err == nil {
		t.Error("expected error for unknown project filter")
	}
}

// ---------- projectImportCandidate ----------

func TestProjectImportCandidate(t *testing.T) {
	projPath := t.TempDir()
	// Missing source
	if _, ok := projectImportCandidate("proj", projPath, relAgentsMD); ok {
		t.Error("missing source should not yield candidate")
	}

	// Existing source maps cleanly
	os.WriteFile(filepath.Join(projPath, relAgentsMD), []byte("# rules"), 0644)
	c, ok := projectImportCandidate("proj", projPath, relAgentsMD)
	if !ok {
		t.Fatal("expected candidate")
	}
	if c.destRel != "rules/proj/agents.md" {
		t.Errorf("destRel = %q", c.destRel)
	}

	// Backup artifact filename excluded
	backup := relAgentsMD + ".dot-agents-backup-20260101-000000"
	os.WriteFile(filepath.Join(projPath, backup), []byte("x"), 0644)
	if _, ok := projectImportCandidate("proj", projPath, backup); ok {
		t.Error("backup artifact should be excluded")
	}
}

// ---------- githubHookBundleName ----------

func TestGitHubHookBundleName(t *testing.T) {
	cases := []struct {
		rel  string
		name string
		ok   bool
	}{
		{".github/hooks/pre.json", "pre", true},
		{".github/hooks/sub/post.json", "post", true},
		{".github/hooks/x.yaml", "", false},
		{"other/path.json", "", false},
	}
	for _, c := range cases {
		name, ok := githubHookBundleName(c.rel)
		if name != c.name || ok != c.ok {
			t.Errorf("githubHookBundleName(%q) = (%q,%v), want (%q,%v)",
				c.rel, name, ok, c.name, c.ok)
		}
	}
}

// ---------- canonicalHookWhen* event mappers ----------

func TestCanonicalHookWhenMappers(t *testing.T) {
	cases := []struct {
		name string
		fn   func(string) (string, bool)
		in   string
		want string
		ok   bool
	}{
		{"copilot-known", canonicalHookWhenFromCopilotEvent, "sessionStart", "session_start", true},
		{"copilot-prompt", canonicalHookWhenFromCopilotEvent, "userPromptSubmitted", "user_prompt_submit", true},
		{"copilot-unknown", canonicalHookWhenFromCopilotEvent, "unknown", "", false},
		{"cursor-pre", canonicalHookWhenFromCursorEvent, "preToolUse", "pre_tool_use", true},
		{"cursor-stop", canonicalHookWhenFromCursorEvent, "stop", "stop", true},
		{"cursor-unknown", canonicalHookWhenFromCursorEvent, "junk", "", false},
		{"codex-session", canonicalHookWhenFromCodexEvent, "SessionStart", "session_start", true},
		{"codex-post", canonicalHookWhenFromCodexEvent, "PostToolUse", "post_tool_use", true},
		{"codex-unknown", canonicalHookWhenFromCodexEvent, "junk", "", false},
		{"claude-session-end", canonicalHookWhenFromClaudeEvent, "SessionEnd", "session_end", true},
		{"claude-subagent", canonicalHookWhenFromClaudeEvent, "SubagentStop", "subagent_stop", true},
		{"claude-precompact", canonicalHookWhenFromClaudeEvent, "PreCompact", "pre_compact", true},
		{"claude-unknown", canonicalHookWhenFromClaudeEvent, "junk", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := c.fn(c.in)
			if got != c.want || ok != c.ok {
				t.Errorf("got (%q,%v), want (%q,%v)", got, ok, c.want, c.ok)
			}
		})
	}
}

// ---------- sanitizeHookNamePart ----------

func TestSanitizeHookNamePart(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"hello-world", "hello-world"},
		{" HelloWorld ", "helloworld"},
		{"a__b!!c", "a-b-c"},
		{"---x---", "x"},
		{"", ""},
	}
	for _, c := range cases {
		got := sanitizeHookNamePart(c.in)
		if got != c.want {
			t.Errorf("sanitizeHookNamePart(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// ---------- commandStem ----------

func TestCommandStem(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"./scripts/run.sh", "run"},
		{"bash -c 'echo hi'", "bash"},
		{"/usr/bin/python3", "python3"},
	}
	for _, c := range cases {
		got := commandStem(c.in)
		if got != c.want {
			t.Errorf("commandStem(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// ---------- trimRedundantPrefix ----------

func TestTrimRedundantPrefix(t *testing.T) {
	cases := []struct {
		v, p, want string
	}{
		{"foo-bar", "foo", "bar"},
		{"foo", "foo", ""},
		{"foo-bar", "baz", "foo-bar"},
		{"", "x", ""},
		{"x", "", "x"},
	}
	for _, c := range cases {
		got := trimRedundantPrefix(c.v, c.p)
		if got != c.want {
			t.Errorf("trimRedundantPrefix(%q,%q) = %q, want %q", c.v, c.p, got, c.want)
		}
	}
}

// ---------- canonicalMatchToolsFromMatcher / shouldSetCanonicalMatchExpression ----------

func TestCanonicalMatchToolsFromMatcher(t *testing.T) {
	cases := []struct {
		matcher string
		want    []string
	}{
		{"", nil},
		{"*", nil},
		{"Bash", []string{"Bash"}},
		{"Write|Edit", []string{"Write", "Edit"}},
		{"Write | Edit", []string{"Write", "Edit"}}, // spaces are trimmed per part
		{"Write|with-dash", []string{"Write", "with-dash"}},
		{"with spaces", nil},
		{"a|", nil},
	}
	for _, c := range cases {
		got := canonicalMatchToolsFromMatcher(c.matcher)
		if len(got) != len(c.want) {
			t.Errorf("matcher=%q got=%v want=%v", c.matcher, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("matcher=%q got=%v want=%v", c.matcher, got, c.want)
				break
			}
		}
	}
}

func TestShouldSetCanonicalMatchExpression(t *testing.T) {
	cases := []struct {
		matcher string
		want    bool
	}{
		{"", false},
		{"*", false},
		{"Bash", false},        // pure single tool
		{"Write|Edit", false},  // pure simple tokens with no spaces
		{"Write | Edit", true}, // spaces — needs raw expression
		{"complex.regex", true},
	}
	for _, c := range cases {
		got := shouldSetCanonicalMatchExpression(c.matcher)
		if got != c.want {
			t.Errorf("shouldSetCanonicalMatchExpression(%q) = %v, want %v", c.matcher, got, c.want)
		}
	}
}

// ---------- hasOnlyClaudeCompatKeys ----------

func TestHasOnlyClaudeCompatKeys(t *testing.T) {
	allowed := map[string]json.RawMessage{
		"hooks":   nil,
		"$schema": nil,
	}
	if !hasOnlyClaudeCompatKeys(allowed) {
		t.Error("expected allowed keys to pass")
	}
	denied := map[string]json.RawMessage{
		"hooks":   nil,
		"plugins": nil,
	}
	if hasOnlyClaudeCompatKeys(denied) {
		t.Error("expected unknown keys to fail")
	}
}

// ---------- importConflictStableBundleName ----------

func TestImportConflictStableBundleName(t *testing.T) {
	taken := func(name string) bool { return name == "cursor-pre" }
	got := importConflictStableBundleName("pre", "cursor", taken)
	if got != "cursor-pre-2" {
		t.Errorf("first-conflict: got %q, want cursor-pre-2", got)
	}

	taken2 := func(name string) bool { return false }
	got = importConflictStableBundleName("pre", "cursor", taken2)
	if got != "cursor-pre" {
		t.Errorf("free: got %q, want cursor-pre", got)
	}

	taken3 := func(name string) bool { return name == "import-hook" }
	got = importConflictStableBundleName("", "", taken3)
	if got != "import-hook-2" {
		t.Errorf("blank parts: got %q, want import-hook-2", got)
	}
}

// ---------- importConflictFirstFreeAlternateDestRel ----------

func TestImportConflictFirstFreeAlternateDestRel(t *testing.T) {
	agentsHome := t.TempDir()

	// Non-hooks path
	if _, ok := importConflictFirstFreeAlternateDestRel(agentsHome, "rules/proj/x.md", "cursor"); ok {
		t.Error("non-hooks path should not produce alternate")
	}

	// HOOK.yaml shape free
	alt, ok := importConflictFirstFreeAlternateDestRel(agentsHome, "hooks/proj/myhook/HOOK.yaml", "cursor")
	if !ok || alt != "hooks/proj/cursor-myhook/HOOK.yaml" {
		t.Errorf("HOOK.yaml: got (%q,%v)", alt, ok)
	}

	// .json shape
	alt, ok = importConflictFirstFreeAlternateDestRel(agentsHome, "hooks/proj/legacy.json", "github")
	if !ok || alt != "hooks/proj/github-legacy.json" {
		t.Errorf(".json: got (%q,%v)", alt, ok)
	}
}

// ---------- logicalNameFromHooksDest ----------

func TestLogicalNameFromHooksDest(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"hooks/proj/myhook/HOOK.yaml", "myhook"},
		{"hooks/proj/legacy.json", "legacy"},
		{"hooks/proj/something/else.yaml", ""},
		{"unrelated/path", ""},
	}
	for _, c := range cases {
		got := logicalNameFromHooksDest(c.in)
		if got != c.want {
			t.Errorf("logicalNameFromHooksDest(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// ---------- isSimpleHookToken ----------

func TestIsSimpleHookToken(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"Bash", true},
		{"with-dash", true},
		{"with_underscore", true},
		{"AlphaNum123", true},
		{"with space", false},
		{"with.dot", false},
		{"with/slash", false},
	}
	for _, c := range cases {
		got := isSimpleHookToken(c.in)
		if got != c.want {
			t.Errorf("isSimpleHookToken(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// ---------- matcherNameHint ----------

func TestMatcherNameHint(t *testing.T) {
	cases := []struct {
		matcher string
		want    string
	}{
		{"", ""},
		{"*", ""},
		{"Bash", "Bash"},
		{"Write|Edit", "Write-Edit"},
		{"Write|Edit|Read", "Write-Edit"},
		{"with space", ""},
	}
	for _, c := range cases {
		got := matcherNameHint(c.matcher)
		if got != c.want {
			t.Errorf("matcherNameHint(%q) = %q, want %q", c.matcher, got, c.want)
		}
	}
}

// ---------- isManagedSymlink ----------

func TestIsManagedSymlink(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)

	// Plain file → not a symlink
	plain := filepath.Join(tmp, "plain")
	os.WriteFile(plain, []byte("x"), 0644)
	if isManagedSymlink(plain, agentsHome) {
		t.Error("plain file is not a managed symlink")
	}

	// Symlink pointing into agentsHome → managed
	target := filepath.Join(agentsHome, "managed.txt")
	os.WriteFile(target, []byte("x"), 0644)
	managed := filepath.Join(tmp, "managed-link")
	if err := os.Symlink(target, managed); err != nil {
		t.Fatal(err)
	}
	if !isManagedSymlink(managed, agentsHome) {
		t.Error("expected managed symlink")
	}

	// Symlink elsewhere → unmanaged
	other := filepath.Join(tmp, "other.txt")
	os.WriteFile(other, []byte("x"), 0644)
	unmanaged := filepath.Join(tmp, "unmanaged-link")
	if err := os.Symlink(other, unmanaged); err != nil {
		t.Fatal(err)
	}
	if isManagedSymlink(unmanaged, agentsHome) {
		t.Error("unmanaged symlink should not be reported as managed")
	}

	// Missing path
	if isManagedSymlink(filepath.Join(tmp, "nope"), agentsHome) {
		t.Error("missing path should not be managed")
	}
}

// ---------- runImport: scope=global with no candidates is a no-op ----------

func TestRunImport_GlobalScopeNoCandidatesIsNoop(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true, DryRun: true}
	defer func() { Flags = saved }()

	if err := runImport("", "global"); err != nil {
		t.Errorf("runImport global: %v", err)
	}
}

func TestRunImport_InvalidScopeErrors(t *testing.T) {
	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	if err := runImport("", "bogus"); err == nil {
		t.Error("expected error for invalid scope")
	} else if !strings.Contains(err.Error(), "scope") {
		t.Errorf("expected scope error, got: %v", err)
	}
}

// ---------- foldImportCandidates: skips backup destinations ----------

func TestFoldImportCandidates_EmptyList(t *testing.T) {
	r := foldImportCandidates(nil, t.TempDir(), "20260101-000000")
	if r.imported != 0 || r.skipped != 0 {
		t.Errorf("expected zero result, got %+v", r)
	}
}

// runImport with project scope when there are no candidates ends successfully.
func TestRunImport_ProjectScopeNoCandidates(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	if err := runImport("", "project"); err != nil {
		t.Errorf("runImport project scope: %v", err)
	}
}

// runImportFromRefresh forces Yes=true; verify no error on empty config.
func TestRunImportFromRefresh_EmptyConfig(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	defer func() { Flags = saved }()
	Flags = GlobalFlags{}

	if err := runImportFromRefresh("", "all"); err != nil {
		t.Errorf("runImportFromRefresh: %v", err)
	}
}

// runImportInternal with invalid scope returns usage error.
func TestRunImportInternal_InvalidScope(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	saved := Flags
	defer func() { Flags = saved }()
	Flags = GlobalFlags{}

	if err := runImportInternal("", "bogus", false); err == nil {
		t.Error("expected invalid-scope error")
	}
}

// collectImportCandidates with a managed project that has no AI configs is empty.
func TestCollectImportCandidates_RegisteredProjectEmpty(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "empty-proj")
	os.MkdirAll(projectPath, 0755)
	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("empty-proj", projectPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	candidates, _, err := collectImportCandidates(cfg, "", importScopeProject)
	if err != nil {
		t.Errorf("collectImportCandidates: %v", err)
	}
	if len(candidates) != 0 {
		t.Errorf("expected no candidates, got %+v", candidates)
	}
}
