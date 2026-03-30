package commands

import (
	"os"
	"path/filepath"
	"testing"

	"go.yaml.in/yaml/v3"
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
	src := filepath.Join(dir, "prompt-log.json")
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
		t.Fatalf("yaml.Unmarshal failed: %v\n%s", err, string(content))
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

func TestCanonicalImportOutputs_FromCursorHooksJSON(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, relCursorHooksJSON)
	if err := os.MkdirAll(filepath.Dir(src), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte(`{
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
`), 0644); err != nil {
		t.Fatal(err)
	}

	outputs, ok, err := canonicalImportOutputs(importCandidate{
		project:    "proj",
		sourceRoot: dir,
		sourcePath: src,
	})
	if err != nil {
		t.Fatalf("canonicalImportOutputs failed: %v", err)
	}
	if !ok || len(outputs) != 1 {
		t.Fatalf("expected one canonical output, ok=%v len=%d", ok, len(outputs))
	}
	if got, want := outputs[0].destRel, "hooks/proj/pre-tool-use-guard/HOOK.yaml"; got != want {
		t.Fatalf("destRel = %q, want %q", got, want)
	}

	var manifest map[string]any
	if err := yaml.Unmarshal(outputs[0].content, &manifest); err != nil {
		t.Fatalf("yaml.Unmarshal failed: %v\n%s", err, string(outputs[0].content))
	}
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

func TestCanonicalImportOutputs_FromCodexHooksJSON(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, relCodexHooksJSON)
	if err := os.MkdirAll(filepath.Dir(src), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte(`{
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
`), 0644); err != nil {
		t.Fatal(err)
	}

	outputs, ok, err := canonicalImportOutputs(importCandidate{
		project:    "proj",
		sourceRoot: dir,
		sourcePath: src,
	})
	if err != nil {
		t.Fatalf("canonicalImportOutputs failed: %v", err)
	}
	if !ok || len(outputs) != 1 {
		t.Fatalf("expected one canonical output, ok=%v len=%d", ok, len(outputs))
	}
	if got, want := outputs[0].destRel, "hooks/proj/session-start-banner/HOOK.yaml"; got != want {
		t.Fatalf("destRel = %q, want %q", got, want)
	}
}

func TestCanonicalImportOutputs_FromClaudeCompatSettings(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, relClaudeSettingsLocal)
	if err := os.MkdirAll(filepath.Dir(src), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte(`{
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
`), 0644); err != nil {
		t.Fatal(err)
	}

	outputs, ok, err := canonicalImportOutputs(importCandidate{
		project:    "proj",
		sourceRoot: dir,
		sourcePath: src,
	})
	if err != nil {
		t.Fatalf("canonicalImportOutputs failed: %v", err)
	}
	if !ok || len(outputs) != 1 {
		t.Fatalf("expected one canonical output, ok=%v len=%d", ok, len(outputs))
	}

	var manifest map[string]any
	if err := yaml.Unmarshal(outputs[0].content, &manifest); err != nil {
		t.Fatalf("yaml.Unmarshal failed: %v\n%s", err, string(outputs[0].content))
	}
	if got := manifest["when"]; got != "session_start" {
		t.Fatalf("when = %#v, want session_start", got)
	}
	if got := manifest["enabled_on"]; got == nil {
		t.Fatalf("expected enabled_on in manifest")
	}
}

func TestCanonicalImportOutputs_AssignsDistinctNamesForGenericCommandsUsingMatchers(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, relCursorHooksJSON)
	if err := os.MkdirAll(filepath.Dir(src), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte(`{
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
`), 0644); err != nil {
		t.Fatal(err)
	}

	outputs, ok, err := canonicalImportOutputs(importCandidate{
		project:    "proj",
		sourceRoot: dir,
		sourcePath: src,
	})
	if err != nil {
		t.Fatalf("canonicalImportOutputs failed: %v", err)
	}
	if !ok || len(outputs) != 2 {
		t.Fatalf("expected two canonical outputs, ok=%v len=%d", ok, len(outputs))
	}
	if got, want := outputs[0].destRel, "hooks/proj/pre-tool-use-write-edit-run/HOOK.yaml"; got != want {
		t.Fatalf("first destRel = %q, want %q", got, want)
	}
	if got, want := outputs[1].destRel, "hooks/proj/pre-tool-use-bash-run/HOOK.yaml"; got != want {
		t.Fatalf("second destRel = %q, want %q", got, want)
	}
}

func TestCanonicalImportOutputs_AppendsStableSuffixForDuplicateIdentity(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, relCodexHooksJSON)
	if err := os.MkdirAll(filepath.Dir(src), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte(`{
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
`), 0644); err != nil {
		t.Fatal(err)
	}

	outputs, ok, err := canonicalImportOutputs(importCandidate{
		project:    "proj",
		sourceRoot: dir,
		sourcePath: src,
	})
	if err != nil {
		t.Fatalf("canonicalImportOutputs failed: %v", err)
	}
	if !ok || len(outputs) != 2 {
		t.Fatalf("expected two canonical outputs, ok=%v len=%d", ok, len(outputs))
	}
	if got, want := outputs[0].destRel, "hooks/proj/pre-tool-use-guard/HOOK.yaml"; got != want {
		t.Fatalf("first destRel = %q, want %q", got, want)
	}
	if got, want := outputs[1].destRel, "hooks/proj/pre-tool-use-guard-2/HOOK.yaml"; got != want {
		t.Fatalf("second destRel = %q, want %q", got, want)
	}
}

func TestCanonicalImportOutputs_SplitsMultipleActionsIntoDistinctCanonicalHooks(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, relClaudeSettingsLocal)
	if err := os.MkdirAll(filepath.Dir(src), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte(`{
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
`), 0644); err != nil {
		t.Fatal(err)
	}

	outputs, ok, err := canonicalImportOutputs(importCandidate{
		project:    "proj",
		sourceRoot: dir,
		sourcePath: src,
	})
	if err != nil {
		t.Fatalf("canonicalImportOutputs failed: %v", err)
	}
	if !ok || len(outputs) != 2 {
		t.Fatalf("expected two canonical outputs, ok=%v len=%d", ok, len(outputs))
	}
	if got, want := outputs[0].destRel, "hooks/proj/pre-tool-use-lint/HOOK.yaml"; got != want {
		t.Fatalf("first destRel = %q, want %q", got, want)
	}
	if got, want := outputs[1].destRel, "hooks/proj/pre-tool-use-format/HOOK.yaml"; got != want {
		t.Fatalf("second destRel = %q, want %q", got, want)
	}
}

func TestCanonicalImportOutputs_CanonicalizesMultiActionCopilotFanoutUsingFilenameHint(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, relGitHubHooksDir, "prompt-log.json")
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

	outputs, ok, err := canonicalImportOutputs(importCandidate{
		project:    "proj",
		sourceRoot: dir,
		sourcePath: src,
	})
	if err != nil {
		t.Fatalf("canonicalImportOutputs failed: %v", err)
	}
	if !ok || len(outputs) != 2 {
		t.Fatalf("expected two canonical outputs, ok=%v len=%d", ok, len(outputs))
	}
	if got, want := outputs[0].destRel, "hooks/proj/prompt-log/HOOK.yaml"; got != want {
		t.Fatalf("first destRel = %q, want %q", got, want)
	}
	if got, want := outputs[1].destRel, "hooks/proj/prompt-log-second/HOOK.yaml"; got != want {
		t.Fatalf("second destRel = %q, want %q", got, want)
	}
}

func TestCanonicalImportOutputs_FallsBackToLegacyWhenCopilotEventIsUnknown(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, relGitHubHooksDir, "prompt-log.json")
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

	outputs, ok, err := canonicalImportOutputs(importCandidate{
		project:    "proj",
		sourceRoot: dir,
		sourcePath: src,
	})
	if err != nil {
		t.Fatalf("canonicalImportOutputs failed: %v", err)
	}
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

func TestCanonicalImportOutputs_UsesMatcherHintForGenericCommandName(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, relCursorHooksJSON)
	if err := os.MkdirAll(filepath.Dir(src), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte(`{
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
`), 0644); err != nil {
		t.Fatal(err)
	}

	outputs, ok, err := canonicalImportOutputs(importCandidate{
		project:    "proj",
		sourceRoot: dir,
		sourcePath: src,
	})
	if err != nil {
		t.Fatalf("canonicalImportOutputs failed: %v", err)
	}
	if !ok || len(outputs) != 1 {
		t.Fatalf("expected one canonical output, ok=%v len=%d", ok, len(outputs))
	}
	if got, want := outputs[0].destRel, "hooks/proj/pre-tool-use-bash-run/HOOK.yaml"; got != want {
		t.Fatalf("destRel = %q, want %q", got, want)
	}
}

func TestCanonicalImportOutputs_PreservesRawMatcherOverrideWhenNormalized(t *testing.T) {
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

	outputs, ok, err := canonicalImportOutputs(importCandidate{
		project:    "proj",
		sourceRoot: dir,
		sourcePath: src,
	})
	if err != nil {
		t.Fatalf("canonicalImportOutputs failed: %v", err)
	}
	if !ok || len(outputs) != 1 {
		t.Fatalf("expected one canonical output, ok=%v len=%d", ok, len(outputs))
	}

	var manifest map[string]any
	if err := yaml.Unmarshal(outputs[0].content, &manifest); err != nil {
		t.Fatalf("yaml.Unmarshal failed: %v\n%s", err, string(outputs[0].content))
	}
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

func TestRestoreFromResourcesCounted_CanonicalizesGitHubHooks(t *testing.T) {
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
		t.Fatalf("yaml.Unmarshal failed: %v\n%s", err, string(content))
	}
	if got := manifest["name"]; got != "pre-tool" {
		t.Fatalf("name = %#v, want pre-tool", got)
	}
	if got := manifest["when"]; got != "pre_tool_use" {
		t.Fatalf("when = %#v, want pre_tool_use", got)
	}
}

func TestRestoreFromResourcesCounted_CanonicalizesCursorHooks(t *testing.T) {
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
