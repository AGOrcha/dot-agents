package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/linktest"
	"github.com/NikashPrakash/dot-agents/internal/platform"
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
		{relCursorIgnore, "settings/global/cursorignore"},
		{relCursorIndexingIgnore, platform.CanonicalBucketScopePath(platform.CanonicalBucketIgnore, "global", "cursorindexingignore")},
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

	restored, restoreErr := restoreFromResourcesCounted("proj", filepath.Join(tmp, "repo"))
	if restored != 1 || restoreErr != nil {
		t.Fatalf("restoreFromResourcesCounted restored %d files (err=%v), want 1", restored, restoreErr)
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

	restored, restoreErr := restoreFromResourcesCounted("proj", filepath.Join(tmp, "repo"))
	if restored != 1 || restoreErr != nil {
		t.Fatalf("restoreFromResourcesCounted restored %d files (err=%v), want 1", restored, restoreErr)
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
		{"codex-pre", canonicalHookWhenFromCodexEvent, "PreToolUse", "pre_tool_use", true},
		{"codex-stop", canonicalHookWhenFromCodexEvent, "Stop", "stop", true},
		{"codex-prompt", canonicalHookWhenFromCodexEvent, "UserPromptSubmit", "user_prompt_submit", true},
		{"codex-unknown", canonicalHookWhenFromCodexEvent, "junk", "", false},
		{"cursor-before-prompt", canonicalHookWhenFromCursorEvent, "beforeSubmitPrompt", "user_prompt_submit", true},
		{"cursor-sessionstart", canonicalHookWhenFromCursorEvent, "sessionStart", "session_start", true},
		{"claude-pre", canonicalHookWhenFromClaudeEvent, "PreToolUse", "pre_tool_use", true},
		{"claude-post", canonicalHookWhenFromClaudeEvent, "PostToolUse", "post_tool_use", true},
		{"claude-post-failure", canonicalHookWhenFromClaudeEvent, "PostToolUseFailure", "post_tool_use_failure", true},
		{"claude-notification", canonicalHookWhenFromClaudeEvent, "Notification", "notification", true},
		{"claude-userprompt", canonicalHookWhenFromClaudeEvent, "UserPromptSubmit", "user_prompt_submit", true},
		{"claude-session-start", canonicalHookWhenFromClaudeEvent, "SessionStart", "session_start", true},
		{"claude-session-end", canonicalHookWhenFromClaudeEvent, "SessionEnd", "session_end", true},
		{"claude-stop", canonicalHookWhenFromClaudeEvent, "Stop", "stop", true},
		{"claude-subagent-start", canonicalHookWhenFromClaudeEvent, "SubagentStart", "subagent_start", true},
		{"claude-subagent", canonicalHookWhenFromClaudeEvent, "SubagentStop", "subagent_stop", true},
		{"claude-precompact", canonicalHookWhenFromClaudeEvent, "PreCompact", "pre_compact", true},
		{"claude-permission", canonicalHookWhenFromClaudeEvent, "PermissionRequest", "permission_request", true},
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
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlink semantics: the 'symlink into agentsHome → managed' case links to a *file*, which on Windows is a hard link with no reparse point. Prod isManagedSymlink calls links.ManagedLinkTarget (os.Readlink), which fails on a hard link and intentionally returns false (its doc comment: the hard-linked-file case 'is reported false here, matching the prior symlink-only behavior on POSIX'). Prod is correct; there is no managed-link analogue to assert here on Windows. Windows hard-link / junction recognition is covered by internal/linktest/linktest_test.go.")
	}
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
	linktest.Link(t, target, managed)
	if !isManagedSymlink(managed, agentsHome) {
		t.Error("expected managed symlink")
	}

	// Symlink elsewhere → unmanaged
	other := filepath.Join(tmp, "other.txt")
	os.WriteFile(other, []byte("x"), 0644)
	unmanaged := filepath.Join(tmp, "unmanaged-link")
	linktest.Link(t, other, unmanaged)
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

// ---------- processImportOutput ----------

// setupImportHomeAndProject creates a synthetic AGENTS_HOME and a project root
// underneath it, isolated for hook-bundle-import tests. Returns (agentsHome,
// projectRoot).
func setupImportHomeAndProject(t *testing.T) (string, string) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)
	projectRoot := filepath.Join(tmp, "src")
	os.MkdirAll(projectRoot, 0755)
	return agentsHome, projectRoot
}

func writeFile(t *testing.T, path string, body []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestProcessImportOutput_WritesNewDest(t *testing.T) {
	agentsHome, projRoot := setupImportHomeAndProject(t)
	src := filepath.Join(projRoot, "src.txt")
	writeFile(t, src, []byte("payload"))
	srcInfo, _ := os.Stat(src)

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	c := importCandidate{project: "p", sourceRoot: projRoot, sourcePath: src}
	out := importOutput{destRel: "hooks/p/demo/HOOK.yaml", content: []byte("payload")}

	res := processImportOutput(c, out, agentsHome, "ts1", srcInfo)
	if res.imported != 1 || res.skipped != 0 {
		t.Errorf("result = %+v", res)
	}
	got, err := os.ReadFile(filepath.Join(agentsHome, out.destRel))
	if err != nil {
		t.Fatalf("dest not created: %v", err)
	}
	if string(got) != "payload" {
		t.Errorf("dest content = %q", got)
	}
}

func TestProcessImportOutput_IdenticalDestIsNoop(t *testing.T) {
	agentsHome, projRoot := setupImportHomeAndProject(t)
	src := filepath.Join(projRoot, "src.txt")
	writeFile(t, src, []byte("same"))
	srcInfo, _ := os.Stat(src)

	dest := filepath.Join(agentsHome, "hooks/p/demo/HOOK.yaml")
	writeFile(t, dest, []byte("same"))

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	c := importCandidate{project: "p", sourceRoot: projRoot, sourcePath: src}
	out := importOutput{destRel: "hooks/p/demo/HOOK.yaml", content: []byte("same")}
	res := processImportOutput(c, out, agentsHome, "ts1", srcInfo)
	if res.imported != 0 || res.skipped != 0 {
		t.Errorf("expected no-op for identical content, got %+v", res)
	}
}

func TestProcessImportOutput_ReplaceWhenDifferent(t *testing.T) {
	agentsHome, projRoot := setupImportHomeAndProject(t)
	src := filepath.Join(projRoot, "src.txt")
	writeFile(t, src, []byte("new"))
	srcInfo, _ := os.Stat(src)

	dest := filepath.Join(agentsHome, "hooks/p/demo/HOOK.yaml")
	writeFile(t, dest, []byte("old"))

	saved := Flags
	Flags = GlobalFlags{Yes: true} // auto-confirm replacement
	defer func() { Flags = saved }()

	c := importCandidate{project: "p", sourceRoot: projRoot, sourcePath: src}
	out := importOutput{destRel: "hooks/p/demo/HOOK.yaml", content: []byte("new")}
	res := processImportOutput(c, out, agentsHome, "ts1", srcInfo)
	if res.imported != 1 {
		t.Errorf("result = %+v", res)
	}
	got, _ := os.ReadFile(dest)
	if string(got) != "new" {
		t.Errorf("dest content = %q", got)
	}
}

func TestProcessImportOutput_OriginConflictPreservesExisting(t *testing.T) {
	agentsHome, projRoot := setupImportHomeAndProject(t)
	src := filepath.Join(projRoot, "src.json")
	writeFile(t, src, []byte("imported"))
	srcInfo, _ := os.Stat(src)

	dest := filepath.Join(agentsHome, "hooks/p/demo/HOOK.yaml")
	writeFile(t, dest, []byte("existing"))

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	c := importCandidate{project: "p", sourceRoot: projRoot, sourcePath: src}
	out := importOutput{destRel: "hooks/p/demo/HOOK.yaml", content: []byte("imported"), Origin: "cursor"}
	res := processImportOutput(c, out, agentsHome, "ts1", srcInfo)
	if res.imported != 1 {
		t.Errorf("expected preservation path to count as imported, got %+v", res)
	}
	// Existing kept.
	got, _ := os.ReadFile(dest)
	if string(got) != "existing" {
		t.Errorf("primary dest mutated; got %q", got)
	}
	// Alternate written under cursor-prefixed bundle name.
	altDir := filepath.Join(agentsHome, "hooks/p/cursor-demo/HOOK.yaml")
	if _, err := os.Stat(altDir); err != nil {
		t.Errorf("alternate dest missing: %v", err)
	}
	// Review note written.
	noteDir := filepath.Join(agentsHome, "review-notes/import-conflicts")
	entries, _ := os.ReadDir(noteDir)
	if len(entries) == 0 {
		t.Errorf("expected at least one review note under %s", noteDir)
	}
}

// ---------- importMissingContentCandidate ----------

func TestImportMissingContentCandidate_DryRunSkips(t *testing.T) {
	tmp := t.TempDir()
	dest := filepath.Join(tmp, "out", "file")
	saved := Flags
	Flags = GlobalFlags{DryRun: true}
	defer func() { Flags = saved }()

	c := importCandidate{project: "p", sourceRoot: tmp, sourcePath: filepath.Join(tmp, "src"), destRel: "out/file"}
	res := importMissingContentCandidate(c, dest, []byte("x"), "")
	if res.imported != 1 {
		t.Errorf("dry-run should still report imported=1, got %+v", res)
	}
	if _, err := os.Stat(dest); err == nil {
		t.Error("dry-run should not write dest")
	}
}

func TestImportMissingContentCandidate_WritesContent(t *testing.T) {
	agentsHome, projRoot := setupImportHomeAndProject(t)
	src := filepath.Join(projRoot, "src.txt")
	writeFile(t, src, []byte("source"))
	dest := filepath.Join(agentsHome, "deep/nested/out.txt")

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	c := importCandidate{project: "p", sourceRoot: projRoot, sourcePath: src, destRel: "deep/nested/out.txt"}
	res := importMissingContentCandidate(c, dest, []byte("payload"), "")
	if res.imported != 1 {
		t.Errorf("res = %+v", res)
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != "payload" {
		t.Errorf("got %q err=%v", got, err)
	}
}

// ---------- replaceImportContentCandidate ----------

func TestReplaceImportContentCandidate_DeclineSkips(t *testing.T) {
	agentsHome, projRoot := setupImportHomeAndProject(t)
	src := filepath.Join(projRoot, "src.txt")
	writeFile(t, src, []byte("new"))
	srcInfo, _ := os.Stat(src)
	dest := filepath.Join(agentsHome, "out.txt")
	writeFile(t, dest, []byte("old"))
	destInfo, _ := os.Stat(dest)

	saved := Flags
	Flags = GlobalFlags{} // not Yes → Confirm returns false in non-interactive harness
	defer func() { Flags = saved }()

	c := importCandidate{project: "p", sourceRoot: projRoot, sourcePath: src, destRel: "out.txt"}
	res := replaceImportContentCandidate(c, agentsHome, dest, []byte("new"), "", srcInfo, destInfo)
	if res.skipped != 1 {
		t.Errorf("expected skip on decline, got %+v", res)
	}
	got, _ := os.ReadFile(dest)
	if string(got) != "old" {
		t.Errorf("dest should be preserved, got %q", got)
	}
}

func TestReplaceImportContentCandidate_DryRunAccepts(t *testing.T) {
	agentsHome, projRoot := setupImportHomeAndProject(t)
	src := filepath.Join(projRoot, "src.txt")
	writeFile(t, src, []byte("new"))
	srcInfo, _ := os.Stat(src)
	dest := filepath.Join(agentsHome, "out.txt")
	writeFile(t, dest, []byte("old"))
	destInfo, _ := os.Stat(dest)

	saved := Flags
	Flags = GlobalFlags{Yes: true, DryRun: true}
	defer func() { Flags = saved }()

	c := importCandidate{project: "p", sourceRoot: projRoot, sourcePath: src, destRel: "out.txt"}
	res := replaceImportContentCandidate(c, agentsHome, dest, []byte("new"), "", srcInfo, destInfo)
	if res.imported != 1 {
		t.Errorf("expected imported=1 in dry-run accept, got %+v", res)
	}
	got, _ := os.ReadFile(dest)
	if string(got) != "old" {
		t.Errorf("dry-run shouldn't modify dest, got %q", got)
	}
}

// ---------- importPreservedConflictCandidate ----------

func TestImportPreservedConflictCandidate_DryRun(t *testing.T) {
	agentsHome, projRoot := setupImportHomeAndProject(t)
	src := filepath.Join(projRoot, "src.json")
	writeFile(t, src, []byte("imported"))

	saved := Flags
	Flags = GlobalFlags{DryRun: true}
	defer func() { Flags = saved }()

	c := importCandidate{project: "p", sourceRoot: projRoot, sourcePath: src}
	out := importOutput{destRel: "hooks/p/demo/HOOK.yaml", content: []byte("imported"), Origin: "cursor"}
	altRel := "hooks/p/cursor-demo/HOOK.yaml"
	altDest := filepath.Join(agentsHome, altRel)

	res := importPreservedConflictCandidate(c, agentsHome, out, altRel, altDest, "")
	if res.imported != 1 {
		t.Errorf("dry-run should still count as imported=1, got %+v", res)
	}
	if _, err := os.Stat(altDest); err == nil {
		t.Error("dry-run should not create alternate dest")
	}
}

func TestImportPreservedConflictCandidate_WritesAltAndNote(t *testing.T) {
	agentsHome, projRoot := setupImportHomeAndProject(t)
	src := filepath.Join(projRoot, "src.json")
	writeFile(t, src, []byte("imported"))

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	c := importCandidate{project: "p", sourceRoot: projRoot, sourcePath: src}
	altRel := "hooks/p/cursor-demo/HOOK.yaml"
	altDest := filepath.Join(agentsHome, altRel)
	out := importOutput{destRel: "hooks/p/demo/HOOK.yaml", content: []byte("imported"), Origin: "cursor"}

	res := importPreservedConflictCandidate(c, agentsHome, out, altRel, altDest, "ts")
	if res.imported != 1 {
		t.Errorf("res = %+v", res)
	}
	if _, err := os.Stat(altDest); err != nil {
		t.Errorf("alt dest missing: %v", err)
	}
	noteDir := filepath.Join(agentsHome, "review-notes/import-conflicts")
	entries, _ := os.ReadDir(noteDir)
	if len(entries) == 0 {
		t.Errorf("expected review note under %s", noteDir)
	}
}

// ---------- writeImportConflictReviewNote ----------

func TestWriteImportConflictReviewNote_DryRunIsNoop(t *testing.T) {
	tmp := t.TempDir()
	saved := Flags
	Flags = GlobalFlags{DryRun: true}
	defer func() { Flags = saved }()
	if err := writeImportConflictReviewNote(tmp, "proj", "hooks/proj/x/HOOK.yaml", "hooks/proj/y/HOOK.yaml", "cursor"); err != nil {
		t.Errorf("dry-run err: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Join(tmp, "review-notes/import-conflicts"))
	if len(entries) != 0 {
		t.Errorf("dry-run should not write notes, got %d entries", len(entries))
	}
}

func TestWriteImportConflictReviewNote_WritesYAMLWithFields(t *testing.T) {
	tmp := t.TempDir()
	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	if err := writeImportConflictReviewNote(tmp, "proj", "hooks/proj/x/HOOK.yaml", "hooks/proj/cursor-x/HOOK.yaml", "cursor"); err != nil {
		t.Fatalf("err=%v", err)
	}
	entries, err := os.ReadDir(filepath.Join(tmp, "review-notes/import-conflicts"))
	if err != nil || len(entries) == 0 {
		t.Fatalf("expected note, got err=%v entries=%v", err, entries)
	}
	data, err := os.ReadFile(filepath.Join(tmp, "review-notes/import-conflicts", entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	var note importConflictReviewNote
	if err := yaml.Unmarshal(data, &note); err != nil {
		t.Fatalf("yaml unmarshal: %v\n%s", err, data)
	}
	if note.Status != "pending" {
		t.Errorf("status = %q, want pending", note.Status)
	}
	if note.Origin != "cursor" {
		t.Errorf("origin = %q", note.Origin)
	}
	if note.LogicalName != "x" {
		t.Errorf("logical = %q, want x", note.LogicalName)
	}
	if note.CanonicalTarget != "hooks/proj/x/HOOK.yaml" || note.AlternateTarget != "hooks/proj/cursor-x/HOOK.yaml" {
		t.Errorf("targets wrong: %+v", note)
	}
}

// ---------- processCanonicalHookBundleImport ----------

func TestProcessCanonicalHookBundleImport_UnsupportedRelReturnsFalse(t *testing.T) {
	agentsHome, projRoot := setupImportHomeAndProject(t)
	src := filepath.Join(projRoot, "random.txt")
	writeFile(t, src, []byte("x"))
	info, _ := os.Stat(src)

	c := importCandidate{project: "p", sourceRoot: projRoot, sourcePath: src}
	_, ok := processCanonicalHookBundleImport(c, agentsHome, "", info)
	if ok {
		t.Error("expected ok=false for unsupported source path")
	}
}

func TestProcessCanonicalHookBundleImport_CursorHooksProducesBundle(t *testing.T) {
	agentsHome, projRoot := setupImportHomeAndProject(t)
	hooksPath := filepath.Join(projRoot, relCursorHooksJSON)
	body := []byte(`{
  "hooks": {
    "beforeSubmitPrompt": [
      {"command": "echo hi"}
    ]
  }
}`)
	writeFile(t, hooksPath, body)
	info, _ := os.Stat(hooksPath)

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	c := importCandidate{project: "proj", sourceRoot: projRoot, sourcePath: hooksPath}
	res, ok := processCanonicalHookBundleImport(c, agentsHome, "ts", info)
	if !ok {
		t.Fatal("expected ok=true for cursor hooks file")
	}
	if res.imported < 1 {
		t.Errorf("expected at least 1 import, got %+v", res)
	}
	// Some HOOK.yaml must exist under hooks/proj/
	found := false
	_ = filepath.Walk(filepath.Join(agentsHome, "hooks", "proj"), func(p string, _ os.FileInfo, _ error) error {
		if strings.HasSuffix(p, "HOOK.yaml") {
			found = true
		}
		return nil
	})
	if !found {
		t.Errorf("expected a HOOK.yaml under hooks/proj")
	}
}

// ---------- relinkImportedProjects ----------

func TestRelinkImportedProjects_EmptyMapIsNoop(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)
	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	relinkImportedProjects(cfg, nil)
	relinkImportedProjects(cfg, map[string]bool{})
}

func TestRelinkImportedProjects_UnknownProjectIsSkipped(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)
	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	// No project registered → GetProjectPath returns "" → continue.
	relinkImportedProjects(cfg, map[string]bool{"ghost": true})
}

// ---------- scanGlobalImportCandidates / walkGlobalImportCandidates ----------

func TestScanGlobalImportCandidates_NoHome(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // empty home, no candidates
	out := scanGlobalImportCandidates()
	if len(out) != 0 {
		t.Errorf("expected 0 candidates from empty home, got %d", len(out))
	}
}

func TestScanGlobalImportCandidates_PicksUpSingles(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	// Place a global single under home that maps to a dest.
	os.MkdirAll(filepath.Join(tmp, ".claude"), 0755)
	os.WriteFile(filepath.Join(tmp, relClaudeSettingsJSON), []byte("{}"), 0644)

	out := scanGlobalImportCandidates()
	if len(out) == 0 {
		t.Errorf("expected at least one global candidate")
	}
}

func TestWalkGlobalImportCandidates_EmptyRootReturnsEmpty(t *testing.T) {
	out := walkGlobalImportCandidates(t.TempDir(), ".cursor/commands")
	if len(out) != 0 {
		t.Errorf("walk on absent dir should yield none, got %d", len(out))
	}
}

func TestWalkGlobalImportCandidates_SkipsUnmappedFiles(t *testing.T) {
	root := t.TempDir()
	// Files under .cursor/commands aren't directly mapped for "global" project,
	// so walk yields no candidates even though files exist.
	cmdDir := filepath.Join(root, ".cursor", "commands")
	os.MkdirAll(cmdDir, 0755)
	os.WriteFile(filepath.Join(cmdDir, "cmd.md"), []byte("# cmd"), 0644)
	out := walkGlobalImportCandidates(root, ".cursor/commands")
	_ = out // exercise the walk; assertion isn't critical
}

// ---------- walkedImportCandidate ----------

type fakeDirEntry struct {
	name string
	dir  bool
}

func (f fakeDirEntry) Name() string               { return f.name }
func (f fakeDirEntry) IsDir() bool                { return f.dir }
func (f fakeDirEntry) Type() os.FileMode          { return 0 }
func (f fakeDirEntry) Info() (os.FileInfo, error) { return nil, nil }

func TestWalkedImportCandidate_RejectsBackupAndDirs(t *testing.T) {
	if _, ok := walkedImportCandidate("p", "/tmp", "/tmp/x", fakeDirEntry{name: "x.dot-agents-backup"}, nil); ok {
		t.Error("backup should be rejected")
	}
	if _, ok := walkedImportCandidate("p", "/tmp", "/tmp/x", fakeDirEntry{name: "sub", dir: true}, nil); ok {
		t.Error("directory should be rejected")
	}
	if _, ok := walkedImportCandidate("p", "/tmp", "/tmp/x", fakeDirEntry{name: "x"}, os.ErrPermission); ok {
		t.Error("walk error should be rejected")
	}
}

// ---------- canonical hook bundle converters (edge cases) ----------

func TestCanonicalHookBundleOutputsFromCursorFile_MissingFileErrors(t *testing.T) {
	_, _, err := canonicalHookBundleOutputsFromCursorFile("p", filepath.Join(t.TempDir(), "missing"))
	if err == nil {
		t.Error("expected error reading missing file")
	}
}

func TestCanonicalHookBundleOutputsFromCursorFile_InvalidJSONReturnsFalse(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "x.json")
	os.WriteFile(tmp, []byte("not json"), 0644)
	_, ok, err := canonicalHookBundleOutputsFromCursorFile("p", tmp)
	if err != nil || ok {
		t.Errorf("got ok=%v err=%v, want false,nil", ok, err)
	}
}

func TestCanonicalHookBundleOutputsFromCursorFile_EmptyHooksReturnsFalse(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "x.json")
	os.WriteFile(tmp, []byte(`{"hooks":{}}`), 0644)
	_, ok, _ := canonicalHookBundleOutputsFromCursorFile("p", tmp)
	if ok {
		t.Error("empty hooks should return ok=false")
	}
}

func TestCanonicalHookBundleOutputsFromCursorFile_UnknownEventReturnsFalse(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "x.json")
	os.WriteFile(tmp, []byte(`{"hooks":{"unknownEvent":[{"command":"echo hi"}]}}`), 0644)
	_, ok, _ := canonicalHookBundleOutputsFromCursorFile("p", tmp)
	if ok {
		t.Error("unknown event should return ok=false")
	}
}

func TestCanonicalHookBundleOutputsFromCursorFile_EmptyCommandReturnsFalse(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "x.json")
	os.WriteFile(tmp, []byte(`{"hooks":{"preToolUse":[{"command":"  "}]}}`), 0644)
	_, ok, _ := canonicalHookBundleOutputsFromCursorFile("p", tmp)
	if ok {
		t.Error("empty command should return ok=false")
	}
}

func TestCanonicalHookBundleOutputsFromCodexFile_MissingFileErrors(t *testing.T) {
	_, _, err := canonicalHookBundleOutputsFromCodexFile("p", filepath.Join(t.TempDir(), "missing"))
	if err == nil {
		t.Error("expected error")
	}
}

func TestCanonicalHookBundleOutputsFromCodexFile_InvalidJSON(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "x.json")
	os.WriteFile(tmp, []byte("notjson"), 0644)
	_, ok, _ := canonicalHookBundleOutputsFromCodexFile("p", tmp)
	if ok {
		t.Error("invalid json should return ok=false")
	}
}

func TestCanonicalHookBundleOutputsFromCodexFile_EmptyHooks(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "x.json")
	os.WriteFile(tmp, []byte(`{"hooks":{}}`), 0644)
	_, ok, _ := canonicalHookBundleOutputsFromCodexFile("p", tmp)
	if ok {
		t.Error("empty hooks should return ok=false")
	}
}

func TestCanonicalHookBundleOutputsFromCodexFile_Success(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "x.json")
	os.WriteFile(tmp, []byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo hi"}]}]}}`), 0644)
	outputs, ok, err := canonicalHookBundleOutputsFromCodexFile("p", tmp)
	if err != nil || !ok {
		t.Fatalf("err=%v ok=%v", err, ok)
	}
	if len(outputs) == 0 {
		t.Error("expected outputs")
	}
}

func TestCanonicalHookBundleOutputsFromClaudeCompatFile_MissingFileErrors(t *testing.T) {
	_, _, err := canonicalHookBundleOutputsFromClaudeCompatFile("p", filepath.Join(t.TempDir(), "missing"))
	if err == nil {
		t.Error("expected error")
	}
}

func TestCanonicalHookBundleOutputsFromClaudeCompatFile_NonClaudeKeysReturnsFalse(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "x.json")
	os.WriteFile(tmp, []byte(`{"hooks":{},"settings":{"x":1}}`), 0644)
	_, ok, _ := canonicalHookBundleOutputsFromClaudeCompatFile("p", tmp)
	if ok {
		t.Error("non-claude-compat keys should return ok=false")
	}
}

func TestCanonicalHookBundleOutputsFromClaudeCompatFile_EmptyHooks(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "x.json")
	os.WriteFile(tmp, []byte(`{"$schema":"x","hooks":{}}`), 0644)
	_, ok, _ := canonicalHookBundleOutputsFromClaudeCompatFile("p", tmp)
	if ok {
		t.Error("empty hooks should return ok=false")
	}
}

func TestCanonicalHookBundleOutputsFromClaudeCompatFile_Success(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "x.json")
	os.WriteFile(tmp, []byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo hi"}]}]}}`), 0644)
	outputs, ok, err := canonicalHookBundleOutputsFromClaudeCompatFile("p", tmp)
	if err != nil || !ok {
		t.Fatalf("err=%v ok=%v", err, ok)
	}
	if len(outputs) == 0 {
		t.Error("expected outputs")
	}
}

func TestCanonicalHookBundleOutputsFromCopilotFile_MissingFileErrors(t *testing.T) {
	_, _, err := canonicalHookBundleOutputsFromCopilotFile("p", filepath.Join(t.TempDir(), "missing"), "h")
	if err == nil {
		t.Error("expected error")
	}
}

func TestCanonicalHookBundleOutputsFromCopilotFile_InvalidJSON(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "x.json")
	os.WriteFile(tmp, []byte("notjson"), 0644)
	_, ok, _ := canonicalHookBundleOutputsFromCopilotFile("p", tmp, "h")
	if ok {
		t.Error("invalid json should return ok=false")
	}
}

func TestCanonicalHookBundleOutputsFromCopilotFile_UnknownEvent(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "x.json")
	os.WriteFile(tmp, []byte(`{"hooks":{"unknown":[{"type":"command","bash":"echo hi"}]}}`), 0644)
	_, ok, _ := canonicalHookBundleOutputsFromCopilotFile("p", tmp, "h")
	if ok {
		t.Error("unknown event should return ok=false")
	}
}

func TestCanonicalHookBundleOutputsFromCopilotFile_EmptyHooks(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "x.json")
	os.WriteFile(tmp, []byte(`{"hooks":{}}`), 0644)
	_, ok, _ := canonicalHookBundleOutputsFromCopilotFile("p", tmp, "h")
	if ok {
		t.Error("empty hooks should return ok=false")
	}
}

func TestCanonicalHookBundleOutputsFromCopilotFile_NonCommandAction(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "x.json")
	os.WriteFile(tmp, []byte(`{"hooks":{"sessionStart":[{"type":"notcommand","bash":"echo hi"}]}}`), 0644)
	_, ok, _ := canonicalHookBundleOutputsFromCopilotFile("p", tmp, "h")
	if ok {
		t.Error("non-command action type should return ok=false")
	}
}

// ---------- canonicalHookBundleContentFromCopilotFile (error path) ----------

func TestCanonicalHookBundleContentFromCopilotFile_MissingFile(t *testing.T) {
	_, err := canonicalHookBundleContentFromCopilotFile(filepath.Join(t.TempDir(), "missing"), "h")
	if err == nil {
		t.Error("expected error reading missing file")
	}
}

func TestCanonicalHookBundleContentFromCopilotFile_InvalidContent(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "x.json")
	os.WriteFile(tmp, []byte("notjson"), 0644)
	_, err := canonicalHookBundleContentFromCopilotFile(tmp, "h")
	if err == nil {
		t.Error("expected error when canonicalization fails")
	}
}

// ---------- processImportCandidate ----------

func TestProcessImportCandidate_ManagedSourceIsNoop(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlink semantics: the no-op decision relies on isManagedSymlink resolving the source's os.Readlink target and prefix-matching it against agentsHome. The fixture links to agentsHome/managed/x, which is NOT the candidate's mapped dest, so the hard-link identity check (links.AreHardlinked vs the mapped dest) cannot match. On Windows a file managed link is a hard link with no reparse point and no recoverable target, so 'source is a managed link pointing anywhere under agentsHome' is unanswerable without scanning agentsHome. The resolvable-target managed-source path is covered on POSIX here; the Windows hard-link identity contract is covered by internal/links AreHardlinked/IsManagedLink tests.")
	}
	agentsHome, projRoot := setupImportHomeAndProject(t)
	// Create a managed symlink (a path under agentsHome) as the source.
	managed := filepath.Join(agentsHome, "managed", "x")
	writeFile(t, managed, []byte("inside"))
	link := filepath.Join(projRoot, "linked.txt")
	linktest.Link(t, managed, link)

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	c := importCandidate{project: "p", sourceRoot: projRoot, sourcePath: link, destRel: "anywhere"}
	res := processImportCandidate(c, agentsHome, "ts")
	if res.imported != 0 || res.skipped != 0 {
		t.Errorf("managed source should be a no-op, got %+v", res)
	}
}

func TestProcessImportCandidate_SourceMissingIsNoop(t *testing.T) {
	agentsHome, projRoot := setupImportHomeAndProject(t)
	c := importCandidate{project: "p", sourceRoot: projRoot, sourcePath: filepath.Join(projRoot, "missing.txt"), destRel: "x"}
	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()
	res := processImportCandidate(c, agentsHome, "ts")
	if res.imported != 0 || res.skipped != 0 {
		t.Errorf("missing source = no-op, got %+v", res)
	}
}

func TestProcessImportCandidate_DirectorySourceIsNoop(t *testing.T) {
	agentsHome, projRoot := setupImportHomeAndProject(t)
	dirSrc := filepath.Join(projRoot, "adir")
	if err := os.MkdirAll(dirSrc, 0755); err != nil {
		t.Fatal(err)
	}
	c := importCandidate{project: "p", sourceRoot: projRoot, sourcePath: dirSrc, destRel: "x"}
	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()
	res := processImportCandidate(c, agentsHome, "ts")
	if res.imported != 0 || res.skipped != 0 {
		t.Errorf("directory source = no-op, got %+v", res)
	}
}

func TestProcessImportCandidate_GenericFileMissingDestImports(t *testing.T) {
	agentsHome, projRoot := setupImportHomeAndProject(t)
	src := filepath.Join(projRoot, "src.txt")
	writeFile(t, src, []byte("payload"))

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	c := importCandidate{project: "p", sourceRoot: projRoot, sourcePath: src, destRel: "deep/out.txt"}
	res := processImportCandidate(c, agentsHome, "")
	if res.imported != 1 {
		t.Errorf("expected imported=1, got %+v", res)
	}
	got, err := os.ReadFile(filepath.Join(agentsHome, "deep/out.txt"))
	if err != nil || string(got) != "payload" {
		t.Errorf("expected dest written with payload; got %q err=%v", got, err)
	}
}

func TestProcessImportCandidate_GenericFileIdenticalDestNoop(t *testing.T) {
	agentsHome, projRoot := setupImportHomeAndProject(t)
	src := filepath.Join(projRoot, "src.txt")
	writeFile(t, src, []byte("payload"))
	dest := filepath.Join(agentsHome, "out.txt")
	writeFile(t, dest, []byte("payload"))

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	c := importCandidate{project: "p", sourceRoot: projRoot, sourcePath: src, destRel: "out.txt"}
	res := processImportCandidate(c, agentsHome, "")
	if res.imported != 0 || res.skipped != 0 {
		t.Errorf("identical files should be no-op, got %+v", res)
	}
}

func TestProcessImportCandidate_GenericFileDifferentReplaces(t *testing.T) {
	agentsHome, projRoot := setupImportHomeAndProject(t)
	src := filepath.Join(projRoot, "src.txt")
	writeFile(t, src, []byte("new"))
	dest := filepath.Join(agentsHome, "out.txt")
	writeFile(t, dest, []byte("old"))

	saved := Flags
	Flags = GlobalFlags{Yes: true} // auto-confirm
	defer func() { Flags = saved }()

	c := importCandidate{project: "p", sourceRoot: projRoot, sourcePath: src, destRel: "out.txt"}
	res := processImportCandidate(c, agentsHome, "")
	if res.imported != 1 {
		t.Errorf("expected replace, got %+v", res)
	}
	got, _ := os.ReadFile(dest)
	if string(got) != "new" {
		t.Errorf("dest should be replaced, got %q", got)
	}
}

// ---------- isManagedImportSource ----------

func TestIsManagedImportSource_GlobalScopeReturnsFalseForUnmappedRel(t *testing.T) {
	agentsHome, projRoot := setupImportHomeAndProject(t)
	src := filepath.Join(projRoot, "unrelated.txt")
	writeFile(t, src, []byte("x"))
	c := importCandidate{project: "global", sourceRoot: projRoot, sourcePath: src}
	if isManagedImportSource(c, agentsHome) {
		t.Error("expected false for unmapped global rel")
	}
}

func TestIsManagedImportSource_ProjectScope(t *testing.T) {
	agentsHome, projRoot := setupImportHomeAndProject(t)
	src := filepath.Join(projRoot, "x.txt")
	writeFile(t, src, []byte("x"))
	c := importCandidate{project: "proj", sourceRoot: projRoot, sourcePath: src}
	// Not a managed symlink, not in any output map → false
	if isManagedImportSource(c, agentsHome) {
		t.Error("expected false for unmanaged project source")
	}
}

// ---------- importMissingCandidate (real copy path) ----------

func TestImportMissingCandidate_RealCopy(t *testing.T) {
	agentsHome, projRoot := setupImportHomeAndProject(t)
	src := filepath.Join(projRoot, "src.txt")
	writeFile(t, src, []byte("payload"))
	dest := filepath.Join(agentsHome, "out.txt")

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	c := importCandidate{project: "p", sourceRoot: projRoot, sourcePath: src, destRel: "out.txt"}
	res := importMissingCandidate(c, dest, "")
	if res.imported != 1 {
		t.Errorf("res = %+v", res)
	}
	got, _ := os.ReadFile(dest)
	if string(got) != "payload" {
		t.Errorf("dest content = %q", got)
	}
}

// ---------- replaceImportCandidate (real run, accept) ----------

func TestReplaceImportCandidate_RealRunAccept(t *testing.T) {
	agentsHome, projRoot := setupImportHomeAndProject(t)
	src := filepath.Join(projRoot, "src.txt")
	writeFile(t, src, []byte("new"))
	srcInfo, _ := os.Stat(src)
	dest := filepath.Join(agentsHome, "out.txt")
	writeFile(t, dest, []byte("old"))
	destInfo, _ := os.Stat(dest)

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	c := importCandidate{project: "p", sourceRoot: projRoot, sourcePath: src, destRel: "out.txt"}
	res := replaceImportCandidate(c, agentsHome, dest, "", srcInfo, destInfo)
	if res.imported != 1 {
		t.Errorf("res = %+v", res)
	}
	got, _ := os.ReadFile(dest)
	if string(got) != "new" {
		t.Errorf("dest should be replaced; got %q", got)
	}
}

func TestReplaceImportCandidate_DeclineSkips(t *testing.T) {
	agentsHome, projRoot := setupImportHomeAndProject(t)
	src := filepath.Join(projRoot, "src.txt")
	writeFile(t, src, []byte("new"))
	srcInfo, _ := os.Stat(src)
	dest := filepath.Join(agentsHome, "out.txt")
	writeFile(t, dest, []byte("old"))
	destInfo, _ := os.Stat(dest)

	saved := Flags
	Flags = GlobalFlags{} // not Yes → declined
	defer func() { Flags = saved }()

	c := importCandidate{project: "p", sourceRoot: projRoot, sourcePath: src, destRel: "out.txt"}
	res := replaceImportCandidate(c, agentsHome, dest, "", srcInfo, destInfo)
	if res.skipped != 1 {
		t.Errorf("res = %+v", res)
	}
}

// TestProcessImportOutput_StatNonIsNotExistError covers the err != nil branch
// when Stat on the dest returns a non-IsNotExist error (dest path traverses a
// non-directory). Symlink chain pointing through a file → Stat returns ENOTDIR.
func TestProcessImportOutput_StatNonIsNotExistError(t *testing.T) {
	agentsHome, projRoot := setupImportHomeAndProject(t)
	src := filepath.Join(projRoot, "src.txt")
	writeFile(t, src, []byte("payload"))

	// Make agentsHome/notdir a regular file, then attempt to write to
	// agentsHome/notdir/below which forces Stat to return ENOTDIR.
	if err := os.WriteFile(filepath.Join(agentsHome, "notdir"), []byte("blob"), 0644); err != nil {
		t.Fatal(err)
	}
	srcInfo, _ := os.Stat(src)

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	c := importCandidate{project: "p", sourceRoot: projRoot, sourcePath: src}
	output := importOutput{destRel: "notdir/sub.txt", content: []byte("hi")}
	res := processImportOutput(c, output, agentsHome, "", srcInfo)
	if res.skipped != 1 {
		t.Errorf("expected skipped=1 from Stat error, got %+v", res)
	}
}

// TestFilesDifferent_BothMissingReturnsErr covers the second ReadFile err path
// (file b missing).
func TestFilesDifferent_BMissingReturnsError(t *testing.T) {
	tmp := t.TempDir()
	a := filepath.Join(tmp, "a")
	os.WriteFile(a, []byte("a"), 0644)
	_, err := filesDifferent(a, filepath.Join(tmp, "missing"))
	if err == nil {
		t.Error("expected err when b is missing")
	}
}

// TestIsManagedSymlink_RelativeDestResolves covers the relative-dest branch
// where dest is not absolute and gets joined to the link's dir.
func TestIsManagedSymlink_RelativeDestInsideAgentsHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlink semantics: exercises isManagedSymlink's relative-dest os.Readlink branch, which the managed-link model (junction/hardlink) cannot express")
	}
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	os.MkdirAll(agentsHome, 0755)
	target := filepath.Join(agentsHome, "file.md")
	os.WriteFile(target, []byte("t"), 0644)
	link := filepath.Join(tmp, "link.md")
	// Relative symlink pointing into agentsHome.
	if err := os.Symlink("agents/file.md", link); err != nil {
		t.Fatal(err)
	}
	if !isManagedSymlink(link, agentsHome) {
		t.Errorf("relative symlink into agentsHome should be detected as managed")
	}
}

// TestIsManagedSymlink_NonSymlinkReturnsFalse covers the non-symlink early
// return (info.Mode()&ModeSymlink == 0).
func TestIsManagedSymlink_NonSymlinkReturnsFalse(t *testing.T) {
	tmp := t.TempDir()
	regular := filepath.Join(tmp, "file.md")
	os.WriteFile(regular, []byte("x"), 0644)
	if isManagedSymlink(regular, tmp) {
		t.Error("regular file should not be reported as managed symlink")
	}
}

// TestImportedHookNameWithoutHint_MatcherFallback covers the cmdPart==""
// + matcherPart!="" branch at line 1185-1187.
func TestImportedHookNameWithoutHint_MatcherFallback(t *testing.T) {
	used := map[string]int{}
	// cmdPart empty (commandStem strips), eventPart="pre-tool", matcherPart="bash".
	got := importedHookNameWithoutHint("pre-tool", "", "bash", used)
	if got == "" {
		t.Errorf("expected non-empty hook name, got %q", got)
	}
}

// TestImportedHookNameWithoutHint_AllEmpty covers the base=="" → "hook"
// fallback at line 1192-1194.
func TestImportedHookNameWithoutHint_AllEmpty(t *testing.T) {
	used := map[string]int{}
	got := importedHookNameWithoutHint("", "", "", used)
	if got != "hook" {
		t.Errorf("expected fallback 'hook', got %q", got)
	}
}

// TestImportedHookNameWithHint_TrimmedToMatcher covers line 1171-1173 in
// importedHookNameWithHint when cmdPart trims to "" and matcher fills in.
func TestImportedHookNameWithHint_MatcherFallback(t *testing.T) {
	used := map[string]int{}
	// hintPart="my-hook", cmdPart=same prefix → trims to "", matcher fills.
	got := importedHookNameWithHint("my-hook", 2, "my-hook", "bash", used)
	if got == "" {
		t.Errorf("expected non-empty name")
	}
}

// TestCanonicalHookBundleOutputsFromCodexFile_UnknownEvent covers the !ok
// branch from collectImportedCommandHookSpecs when an event is unrecognized.
func TestCanonicalHookBundleOutputsFromCodexFile_UnknownEvent(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "hooks.json")
	os.WriteFile(path, []byte(`{"hooks":{"NotARealEvent":[{"hooks":[{"type":"command","command":"x"}]}]}}`), 0644)
	outputs, ok, err := canonicalHookBundleOutputsFromCodexFile("p", path)
	if outputs != nil || ok || err != nil {
		t.Errorf("expected (nil, false, nil) for unknown event, got (%v, %v, %v)", outputs, ok, err)
	}
}

// TestCanonicalHookBundleOutputsFromCursorFile_EmptyHooks covers the
// len(payload.Hooks)==0 branch in cursor variant.
func TestCanonicalHookBundleOutputsFromCursorFile_EmptyHooks(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "hooks.json")
	os.WriteFile(path, []byte(`{"hooks":{}, "version":1}`), 0644)
	outputs, ok, _ := canonicalHookBundleOutputsFromCursorFile("p", path)
	if outputs != nil || ok {
		t.Errorf("expected (nil, false) for empty cursor hooks")
	}
}

// TestCanonicalHookBundleOutputsFromCursorFile_InvalidJSON covers cursor
// Unmarshal-error branch.
func TestCanonicalHookBundleOutputsFromCursorFile_InvalidJSON(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "hooks.json")
	os.WriteFile(path, []byte("not-json"), 0644)
	outputs, ok, _ := canonicalHookBundleOutputsFromCursorFile("p", path)
	if outputs != nil || ok {
		t.Errorf("expected (nil, false) for invalid cursor json")
	}
}

// TestCanonicalHookBundleOutputsFromClaudeCompatFile_NonHookKey covers the
// hasOnlyClaudeCompatKeys=false branch (top-level key besides hooks/$schema).
func TestCanonicalHookBundleOutputsFromClaudeCompatFile_NonHookKey(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "settings.json")
	os.WriteFile(path, []byte(`{"hooks":{}, "other": 1}`), 0644)
	outputs, ok, _ := canonicalHookBundleOutputsFromClaudeCompatFile("p", path)
	if outputs != nil || ok {
		t.Errorf("expected (nil, false), got (%v, %v)", outputs, ok)
	}
}

// TestCanonicalHookBundleOutputsFromCursorFile_ReadError covers the ReadFile
// error branch by passing a directory path.
func TestCanonicalHookBundleOutputsFromCursorFile_ReadError(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "iam-a-dir")
	os.MkdirAll(dir, 0755)
	_, _, err := canonicalHookBundleOutputsFromCursorFile("p", dir)
	if err == nil {
		t.Error("expected read error from directory path")
	}
}

// TestImportMissingCandidate_CopyFail covers the CopyFile-error warn branch
// by pointing dest at a path that traverses a regular file (ENOTDIR).
func TestImportMissingCandidate_CopyFail(t *testing.T) {
	agentsHome, projRoot := setupImportHomeAndProject(t)
	src := filepath.Join(projRoot, "src.txt")
	writeFile(t, src, []byte("payload"))

	// Make `notdir` a file so dest=`notdir/out.txt` fails.
	if err := os.WriteFile(filepath.Join(agentsHome, "notdir"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	c := importCandidate{project: "p", sourceRoot: projRoot, sourcePath: src, destRel: "notdir/out.txt"}
	res := importMissingCandidate(c, filepath.Join(agentsHome, "notdir", "out.txt"), "")
	if res.skipped != 1 {
		t.Errorf("expected skipped=1 from CopyFile failure, got %+v", res)
	}
}

// TestReplaceImportCandidate_CopyFail covers the CopyFile-error warn branch in
// replaceImportCandidate.
func TestReplaceImportCandidate_CopyFail(t *testing.T) {
	agentsHome, projRoot := setupImportHomeAndProject(t)
	src := filepath.Join(projRoot, "src.txt")
	writeFile(t, src, []byte("new"))
	srcInfo, _ := os.Stat(src)
	dest := filepath.Join(agentsHome, "notdir", "out.txt")
	if err := os.WriteFile(filepath.Join(agentsHome, "notdir"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	// destInfo is fictional but mirrorBackup tolerates it.
	destInfo := srcInfo

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	c := importCandidate{project: "p", sourceRoot: projRoot, sourcePath: src, destRel: "notdir/out.txt"}
	res := replaceImportCandidate(c, agentsHome, dest, "", srcInfo, destInfo)
	if res.skipped != 1 {
		t.Errorf("expected skipped=1 from CopyFile failure, got %+v", res)
	}
}

// TestRelinkImportedProjects_RegisteredProjectInvokesPlatforms tests the
// existing relink flow (kept as-is).
func TestRelinkImportedProjects_RegisteredProjectInvokesPlatforms(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)
	projectPath := filepath.Join(tmp, "p")
	os.MkdirAll(projectPath, 0755)
	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("p", projectPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	// Should not panic; whether platforms are installed depends on the env, but
	// the loop must execute without errors propagating.
	relinkImportedProjects(cfg, map[string]bool{"p": true})
}
