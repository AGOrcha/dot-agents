package commands

import (
	"os"
	"path/filepath"
	"testing"

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

func TestFoldImportCandidates_EmptyIsNoOp(t *testing.T) {
	r := foldImportCandidates(nil, filepath.Join(t.TempDir(), ".agents"), "20260101-000000")
	if r.imported != 0 || r.skipped != 0 {
		t.Fatalf("expected zero aggregate, got %#v", r)
	}
}

func TestSupportsCanonicalImportPathNonPlugin_Table(t *testing.T) {
	cases := []struct {
		rel  string
		want bool
	}{
		{relCursorHooksJSON, true},
		{".github/hooks/x.json", true},
		{".opencode/plugins/foo", true},
		{"misc/unknown.txt", false},
	}
	for _, c := range cases {
		if got := supportsCanonicalImportPathNonPlugin(c.rel); got != c.want {
			t.Fatalf("supportsCanonicalImportPathNonPlugin(%q)=%v want %v", c.rel, got, c.want)
		}
	}
}

func TestCanonicalImportOutputsNonPlugin_UnknownRel(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "notes.txt")
	if err := os.WriteFile(src, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	c := importCandidate{project: canonicalImportProject, sourceRoot: tmp, sourcePath: src}
	outputs, ok, err := canonicalImportOutputsNonPlugin(c, "notes.txt")
	if err != nil {
		t.Fatal(err)
	}
	if ok || len(outputs) != 0 {
		t.Fatalf("expected no outputs, ok=%v len=%d", ok, len(outputs))
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

func TestProcessImportCandidate_SkipsManagedCursorHardlink(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	repo := filepath.Join(tmp, "repo")
	project := canonicalImportProject
	t.Setenv("AGENTS_HOME", agentsHome)

	source := filepath.Join(agentsHome, "rules", "global", "rules.mdc")
	if err := os.MkdirAll(filepath.Dir(source), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("rule"), 0644); err != nil {
		t.Fatal(err)
	}

	rulePath := filepath.Join(repo, ".cursor", "rules", "global--rules.mdc")
	if err := os.MkdirAll(filepath.Dir(rulePath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(source, rulePath); err != nil {
		t.Fatal(err)
	}

	candidate := importCandidate{
		project:    project,
		sourceRoot: repo,
		sourcePath: rulePath,
		destRel:    mapResourceRelToDest(project, ".cursor/rules/global--rules.mdc"),
	}
	result := processImportCandidate(candidate, agentsHome, "20260410-120000")
	if result.imported != 0 || result.skipped != 0 {
		t.Fatalf("managed hardlink should have been ignored, got %+v", result)
	}

	if _, err := os.Stat(filepath.Join(agentsHome, "resources", project)); !os.IsNotExist(err) {
		t.Fatalf("managed hardlink should not create resources backup")
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

func TestImportConflictStableBundleName(t *testing.T) {
	cases := []struct {
		logical, origin string
		occupied        []string
		want            string
	}{
		{"foo", "cursor", nil, "cursor-foo"},
		{"foo", "cursor", []string{"cursor-foo"}, "cursor-foo-2"},
		{"foo", "cursor", []string{"cursor-foo", "cursor-foo-2"}, "cursor-foo-3"},
		{"", "cursor", nil, "cursor-hook"},
	}
	for _, tc := range cases {
		occ := map[string]bool{}
		for _, o := range tc.occupied {
			occ[o] = true
		}
		got := importConflictStableBundleName(tc.logical, tc.origin, func(n string) bool { return occ[n] })
		if got != tc.want {
			t.Fatalf("importConflictStableBundleName(%q, %q, occupied=%v)=%q, want %q", tc.logical, tc.origin, tc.occupied, got, tc.want)
		}
	}
}

func TestLogicalNameFromHooksDest(t *testing.T) {
	cases := []struct {
		rel  string
		want string
	}{
		{"hooks/proj/foo/HOOK.yaml", "foo"},
		{"hooks/proj/bar.json", "bar"},
		{"settings/x", ""},
	}
	for _, tc := range cases {
		if got := logicalNameFromHooksDest(tc.rel); got != tc.want {
			t.Fatalf("logicalNameFromHooksDest(%q)=%q, want %q", tc.rel, got, tc.want)
		}
	}
}

func TestImportConflictFirstFreeAlternateDestRel(t *testing.T) {
	agentsHome := t.TempDir()
	primary := filepath.Join(agentsHome, "hooks", "proj", "foo", "HOOK.yaml")
	if err := os.MkdirAll(filepath.Dir(primary), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(primary, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	got, ok := importConflictFirstFreeAlternateDestRel(agentsHome, "hooks/proj/foo/HOOK.yaml", "cursor")
	if !ok || got != "hooks/proj/cursor-foo/HOOK.yaml" {
		t.Fatalf("got ok=%v rel=%q", ok, got)
	}

	// Second file occupies cursor-foo; expect -2 suffix.
	alt1 := filepath.Join(agentsHome, "hooks", "proj", "cursor-foo", "HOOK.yaml")
	if err := os.MkdirAll(filepath.Dir(alt1), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(alt1, []byte("y"), 0644); err != nil {
		t.Fatal(err)
	}
	got2, ok2 := importConflictFirstFreeAlternateDestRel(agentsHome, "hooks/proj/foo/HOOK.yaml", "cursor")
	if !ok2 || got2 != "hooks/proj/cursor-foo-2/HOOK.yaml" {
		t.Fatalf("got2 ok=%v rel=%q", ok2, got2)
	}

	d := t.TempDir()
	if _, ok := importConflictFirstFreeAlternateDestRel(d, "settings/foo.json", "cursor"); ok {
		t.Fatal("expected unsupported path")
	}
}

func TestImportConflictFirstFreeAlternateDestRel_flatJSON(t *testing.T) {
	agentsHome := t.TempDir()
	p := filepath.Join(agentsHome, "hooks", "proj", "bar.json")
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	got, ok := importConflictFirstFreeAlternateDestRel(agentsHome, "hooks/proj/bar.json", "github")
	if !ok || got != "hooks/proj/github-bar.json" {
		t.Fatalf("got ok=%v rel=%q", ok, got)
	}
}

func TestBuildCanonicalHookOutputs_setsOrigin(t *testing.T) {
	specs := []importedHookSpec{{
		when:      "pre_tool_use",
		command:   "echo",
		timeoutMS: 1,
		enabledOn: []string{"cursor"},
		platform:  "cursor",
	}}
	outs := buildCanonicalHookOutputs("p", specs)
	if len(outs) != 1 {
		t.Fatalf("len=%d", len(outs))
	}
	if outs[0].Origin != "cursor" {
		t.Fatalf("Origin=%q", outs[0].Origin)
	}
}

func TestProcessImportOutput_preservesHookConflict(t *testing.T) {
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	srcRoot := t.TempDir()
	srcFile := filepath.Join(srcRoot, ".cursor", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(srcFile), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(srcFile, []byte(`{"hooks":{"preToolUse":[{"command":"x","matcher":""}]}}`), 0644); err != nil {
		t.Fatal(err)
	}

	primary := filepath.Join(agentsHome, "hooks", "proj", "pre-tool-use-echo", "HOOK.yaml")
	if err := os.MkdirAll(filepath.Dir(primary), 0755); err != nil {
		t.Fatal(err)
	}
	oldYAML := []byte("name: old\nwhen: pre_tool_use\nrun:\n  command: old\n")
	if err := os.WriteFile(primary, oldYAML, 0644); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(primary)
	if err != nil {
		t.Fatal(err)
	}

	newYAML := []byte("name: new\nwhen: pre_tool_use\nrun:\n  command: new\n")
	out := importOutput{
		destRel: "hooks/proj/pre-tool-use-echo/HOOK.yaml",
		content: newYAML,
		Origin:  "cursor",
	}
	c := importCandidate{
		project:    "proj",
		sourceRoot: srcRoot,
		sourcePath: srcFile,
	}
	res := processImportOutput(c, out, agentsHome, "20260101120000", fi)
	if res.imported != 1 || res.skipped != 0 {
		t.Fatalf("imported=%d skipped=%d", res.imported, res.skipped)
	}
	primaryBytes, err := os.ReadFile(primary)
	if err != nil {
		t.Fatal(err)
	}
	if string(primaryBytes) != string(oldYAML) {
		t.Fatal("primary file should be preserved")
	}
	alt := filepath.Join(agentsHome, "hooks", "proj", "cursor-pre-tool-use-echo", "HOOK.yaml")
	altBytes, err := os.ReadFile(alt)
	if err != nil {
		t.Fatal(err)
	}
	if string(altBytes) != string(newYAML) {
		t.Fatalf("alternate content mismatch: %s", altBytes)
	}

	matches, _ := filepath.Glob(filepath.Join(agentsHome, "review-notes", "import-conflicts", "ic-*.yaml"))
	if len(matches) != 1 {
		t.Fatalf("expected one review note, got %d", len(matches))
	}
	var note importConflictReviewNote
	raw, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(raw, &note); err != nil {
		t.Fatal(err)
	}
	if note.Kind != "duplicate_name" || note.Origin != "cursor" {
		t.Fatalf("note kind=%q origin=%q", note.Kind, note.Origin)
	}
	if note.CanonicalTarget != out.destRel || note.AlternateTarget != "hooks/proj/cursor-pre-tool-use-echo/HOOK.yaml" {
		t.Fatalf("note targets: %+v", note)
	}
}

func TestProcessImportOutput_hookConflictDryRun(t *testing.T) {
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	oldFlags := Flags
	t.Cleanup(func() { Flags = oldFlags })
	Flags.DryRun = true

	srcRoot := t.TempDir()
	srcFile := filepath.Join(srcRoot, "h.json")
	if err := os.WriteFile(srcFile, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	primary := filepath.Join(agentsHome, "hooks", "proj", "x", "HOOK.yaml")
	if err := os.MkdirAll(filepath.Dir(primary), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(primary, []byte("a: 1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(primary)
	if err != nil {
		t.Fatal(err)
	}
	res := processImportOutput(importCandidate{project: "proj", sourceRoot: srcRoot, sourcePath: srcFile}, importOutput{
		destRel: "hooks/proj/x/HOOK.yaml",
		content: []byte("b: 2\n"),
		Origin:  "cursor",
	}, agentsHome, "", fi)
	if res.imported != 1 {
		t.Fatalf("imported=%d", res.imported)
	}
	if _, err := os.Stat(filepath.Join(agentsHome, "hooks", "proj", "cursor-x", "HOOK.yaml")); !os.IsNotExist(err) {
		t.Fatal("dry-run should not write alternate")
	}
}

func TestImportFromOpencodePluginDir(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	sourceRoot := filepath.Join(tmp, "repo")
	sourcePath := filepath.Join(sourceRoot, relOpenCodePluginsDir, "my-plugin", "index.js")
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, []byte("console.log('hello')\n"), 0644); err != nil {
		t.Fatal(err)
	}

	result := processImportCandidate(importCandidate{
		project:    "proj",
		sourceRoot: sourceRoot,
		sourcePath: sourcePath,
	}, agentsHome, "20260412-120000")
	if result.imported != 2 {
		t.Fatalf("expected 2 imported outputs, got %+v", result)
	}

	manifestPath := filepath.Join(agentsHome, "plugins", "proj", "my-plugin", platform.PluginManifestName)
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	manifest := mustUnmarshalYAMLMap(t, content)
	if got := manifest["kind"]; got != "native" {
		t.Fatalf("kind = %#v, want native", got)
	}
	if got := manifest["name"]; got != "my-plugin" {
		t.Fatalf("name = %#v, want my-plugin", got)
	}
	if got := manifest["schema_version"]; got != 1 {
		t.Fatalf("schema_version = %#v, want 1", got)
	}
	filesPath := filepath.Join(agentsHome, "plugins", "proj", "my-plugin", "files", "index.js")
	fileContent, err := os.ReadFile(filesPath)
	if err != nil {
		t.Fatalf("read plugin file: %v", err)
	}
	if string(fileContent) != "console.log('hello')\n" {
		t.Fatalf("plugin file content mismatch: %q", string(fileContent))
	}
}

func TestImportFromCursorPluginManifest(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	sourceRoot := filepath.Join(tmp, "repo")
	sourcePath := filepath.Join(sourceRoot, relCursorPluginDir, "plugin.json")
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, []byte(`{"name":"my-plugin","version":"1.0.0"}`), 0644); err != nil {
		t.Fatal(err)
	}

	result := processImportCandidate(importCandidate{
		project:    "proj",
		sourceRoot: sourceRoot,
		sourcePath: sourcePath,
	}, agentsHome, "20260412-120000")
	if result.imported != 2 {
		t.Fatalf("expected 2 imported outputs, got %+v", result)
	}

	manifestPath := filepath.Join(agentsHome, "plugins", "proj", "my-plugin", platform.PluginManifestName)
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	manifest := mustUnmarshalYAMLMap(t, content)
	if got := manifest["kind"]; got != "package" {
		t.Fatalf("kind = %#v, want package", got)
	}
	if got := manifest["version"]; got != "1.0.0" {
		t.Fatalf("version = %#v, want 1.0.0", got)
	}
	platforms, ok := manifest["platforms"].([]any)
	if !ok || len(platforms) != 1 || platforms[0] != "cursor" {
		t.Fatalf("platforms = %#v, want [cursor]", manifest["platforms"])
	}
	platformJSONPath := filepath.Join(agentsHome, "plugins", "proj", "my-plugin", "platforms", "cursor", "plugin.json")
	platformJSON, err := os.ReadFile(platformJSONPath)
	if err != nil {
		t.Fatalf("read platform plugin.json: %v", err)
	}
	if string(platformJSON) != `{"name":"my-plugin","version":"1.0.0"}` {
		t.Fatalf("platform plugin.json mismatch: %s", string(platformJSON))
	}
}
