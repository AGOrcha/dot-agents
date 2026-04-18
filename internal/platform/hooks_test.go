package platform

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	hooksTestAgentsDir            = ".agents"
	hooksTestClaudeCompatFile     = "claude-code.json"
	hooksTestCanonicalHookName    = "format-write"
	hooksTestCanonicalMatcherExpr = "Write | Edit"
	hooksTestCanonicalRunCommand  = "/tmp/run.sh"
	hooksTestJSONUnmarshalFmt     = "json.Unmarshal failed: %v\n%s"
)

func TestResolveHookSpecPrefersProjectHooksOverSettingsAndGlobal(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, hooksTestAgentsDir)

	projectHook := filepath.Join(agentsHome, "hooks", "proj", hooksTestClaudeCompatFile)
	projectSettings := filepath.Join(agentsHome, "settings", "proj", hooksTestClaudeCompatFile)
	globalHook := filepath.Join(agentsHome, "hooks", "global", hooksTestClaudeCompatFile)

	writeTextFile(t, projectHook, "{\"source\":\"project-hook\"}\n")
	writeTextFile(t, projectSettings, "{\"source\":\"project-settings\"}\n")
	writeTextFile(t, globalHook, "{\"source\":\"global-hook\"}\n")

	spec := resolveHookSpec(agentsHome, []string{"hooks", "settings"}, "proj", hooksTestClaudeCompatFile)
	if spec == nil {
		t.Fatal("expected hook spec")
	}
	if spec.Scope != "proj" {
		t.Fatalf("expected scope proj, got %s", spec.Scope)
	}
	if spec.SourceBucket != "hooks" {
		t.Fatalf("expected hooks bucket, got %s", spec.SourceBucket)
	}
	if spec.SourcePath != projectHook {
		t.Fatalf("expected source %s, got %s", projectHook, spec.SourcePath)
	}
}

func TestEmitHookFanoutSymlinksSelectedHookFiles(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, hooksTestAgentsDir)
	dstRoot := filepath.Join(tmp, "repo", ".github", "hooks")

	preTool := filepath.Join(agentsHome, "hooks", "proj", "pre-tool.json")
	cursorHook := filepath.Join(agentsHome, "hooks", "proj", "cursor.json")
	writeTextFile(t, preTool, "{\"name\":\"pre-tool\"}\n")
	writeTextFile(t, cursorHook, "{\"name\":\"cursor\"}\n")

	specs, err := ListHookSpecs(agentsHome, "proj")
	if err != nil {
		t.Fatalf("listHookSpecs failed: %v", err)
	}

	err = emitHookFanout(specs, dstRoot, HookEmissionMode{
		Shape:     HookShapeRenderFanout,
		Transport: HookTransportSymlink,
	}, func(spec HookSpec) (string, bool) {
		if spec.Name == "cursor" {
			return "", false
		}
		return spec.Name + ".json", true
	})
	if err != nil {
		t.Fatalf("emitHookFanout failed: %v", err)
	}

	assertSymlinkTarget(t, filepath.Join(dstRoot, "pre-tool.json"), preTool)
	assertNoFile(t, filepath.Join(dstRoot, "cursor.json"))
	if _, err := os.Stat(dstRoot); err != nil {
		t.Fatalf("expected destination root to exist: %v", err)
	}
}

func TestListHookSpecsLoadsCanonicalBundleAndPreservesLegacyFiles(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, hooksTestAgentsDir)

	writeTextFile(t, filepath.Join(agentsHome, "hooks", "proj", hooksTestCanonicalHookName, "HOOK.yaml"), `name: format-write
when: pre_tool_use
match:
  tools: [Write, Edit]
  expression: Write | Edit
run:
  command: ./run.sh
  timeout_ms: 15000
enabled_on: [claude, cursor]
`)
	writeTextFile(t, filepath.Join(agentsHome, "hooks", "proj", hooksTestCanonicalHookName, "run.sh"), "#!/bin/sh\nexit 0\n")
	writeTextFile(t, filepath.Join(agentsHome, "hooks", "proj", "copilot-cli-policy.json"), "{\"version\":1}\n")

	specs, err := ListHookSpecs(agentsHome, "proj")
	if err != nil {
		t.Fatalf("listHookSpecs failed: %v", err)
	}
	if len(specs) != 2 {
		t.Fatalf("expected 2 hook specs, got %d", len(specs))
	}

	if specs[0].Name != "copilot-cli-policy" || specs[0].SourceKind != HookSourceLegacyFile {
		t.Fatalf("expected first spec to be legacy copilot hook, got %#v", specs[0])
	}
	if specs[1].Name != hooksTestCanonicalHookName || specs[1].SourceKind != HookSourceCanonicalBundle {
		t.Fatalf("expected second spec to be canonical bundle, got %#v", specs[1])
	}
	if specs[1].When != "pre_tool_use" {
		t.Fatalf("expected canonical when=pre_tool_use, got %q", specs[1].When)
	}
	if specs[1].Command != "./run.sh" {
		t.Fatalf("expected canonical command ./run.sh, got %q", specs[1].Command)
	}
	if specs[1].MatchExpression != hooksTestCanonicalMatcherExpr {
		t.Fatalf("expected canonical match expression %q, got %q", hooksTestCanonicalMatcherExpr, specs[1].MatchExpression)
	}
	if got, want := ResolveHookCommand(specs[1]), filepath.Join(agentsHome, "hooks", "proj", hooksTestCanonicalHookName, "run.sh"); got != want {
		t.Fatalf("resolved command = %q, want %q", got, want)
	}
}

func TestRenderClaudeHookSettingsPrefersCanonicalMatchExpression(t *testing.T) {
	specs := []HookSpec{{
		Name:            hooksTestCanonicalHookName,
		When:            "pre_tool_use",
		MatchTools:      []string{"Write", "Edit"},
		MatchExpression: hooksTestCanonicalMatcherExpr,
		Command:         hooksTestCanonicalRunCommand,
	}}

	content, err := renderClaudeHookSettings(specs)
	if err != nil {
		t.Fatalf("renderClaudeHookSettings failed: %v", err)
	}
	if got := string(content); !strings.Contains(got, `"matcher": "Write | Edit"`) {
		t.Fatalf("expected rendered matcher to use canonical expression, got:\n%s", got)
	}
}

func TestRenderClaudeHookSettingsMatchesClaudeCodeSchema(t *testing.T) {
	specs := []HookSpec{{
		Name:            hooksTestCanonicalHookName,
		When:            "pre_tool_use",
		MatchTools:      []string{"Write", "Edit"},
		MatchExpression: hooksTestCanonicalMatcherExpr,
		Command:         hooksTestCanonicalRunCommand,
		TimeoutMS:       15000,
	}}

	content, err := renderClaudeHookSettings(specs)
	if err != nil {
		t.Fatalf("renderClaudeHookSettings failed: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(content, &payload); err != nil {
		t.Fatalf(hooksTestJSONUnmarshalFmt, err, string(content))
	}
	assertHookJSONPathEquals(t, payload, "$schema", "https://json.schemastore.org/claude-code-settings.json")
	assertHookJSONPathEquals(t, payload, "hooks.PreToolUse.0.matcher", hooksTestCanonicalMatcherExpr)
	assertHookJSONPathEquals(t, payload, "hooks.PreToolUse.0.hooks.0.type", "command")
	assertHookJSONPathEquals(t, payload, "hooks.PreToolUse.0.hooks.0.command", hooksTestCanonicalRunCommand)
}

func TestRenderCursorHookConfigPrefersCanonicalMatchExpression(t *testing.T) {
	specs := []HookSpec{{
		Name:            hooksTestCanonicalHookName,
		When:            "pre_tool_use",
		MatchTools:      []string{"Write", "Edit"},
		MatchExpression: hooksTestCanonicalMatcherExpr,
		Command:         hooksTestCanonicalRunCommand,
	}}

	content, err := renderCursorHookConfig(specs)
	if err != nil {
		t.Fatalf("renderCursorHookConfig failed: %v", err)
	}
	if got := string(content); !strings.Contains(got, `"matcher": "Write | Edit"`) {
		t.Fatalf("expected rendered matcher to use canonical expression, got:\n%s", got)
	}
}

func TestRenderCursorHookConfigMatchesCursorDocsShape(t *testing.T) {
	specs := []HookSpec{{
		Name:            hooksTestCanonicalHookName,
		When:            "pre_tool_use",
		MatchTools:      []string{"Bash"},
		MatchExpression: "Bash",
		Command:         hooksTestCanonicalRunCommand,
		TimeoutMS:       7000,
	}}

	content, err := renderCursorHookConfig(specs)
	if err != nil {
		t.Fatalf("renderCursorHookConfig failed: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(content, &payload); err != nil {
		t.Fatalf(hooksTestJSONUnmarshalFmt, err, string(content))
	}
	assertHookJSONPathEquals(t, payload, "version", float64(1))
	assertHookJSONPathEquals(t, payload, "hooks.preToolUse.0.command", hooksTestCanonicalRunCommand)
	assertHookJSONPathEquals(t, payload, "hooks.preToolUse.0.matcher", "Bash")
	assertHookJSONPathEquals(t, payload, "hooks.preToolUse.0.timeout", float64(7))
}

func TestRenderCodexHookConfigMatchesCodexHookShape(t *testing.T) {
	specs := []HookSpec{{
		Name:       "session-banner",
		When:       "session_start",
		Command:    hooksTestCanonicalRunCommand,
		EnabledOn:  []string{"codex"},
		RequiredOn: []string{"codex"},
	}}

	content, err := renderCodexHookConfig(specs)
	if err != nil {
		t.Fatalf("renderCodexHookConfig failed: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(content, &payload); err != nil {
		t.Fatalf(hooksTestJSONUnmarshalFmt, err, string(content))
	}
	assertHookJSONPathEquals(t, payload, "hooks.SessionStart.0.matcher", "*")
	assertHookJSONPathEquals(t, payload, "hooks.SessionStart.0.hooks.0.type", "command")
	assertHookJSONPathEquals(t, payload, "hooks.SessionStart.0.hooks.0.command", hooksTestCanonicalRunCommand)
}

func TestRenderCopilotHookFileMatchesCopilotCLIShape(t *testing.T) {
	name, content, ok, err := renderCopilotHookFile(HookSpec{
		Name:      "prompt-log",
		When:      "user_prompt_submit",
		Command:   hooksTestCanonicalRunCommand,
		TimeoutMS: 5000,
	})
	if err != nil {
		t.Fatalf("renderCopilotHookFile returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected copilot hook render to be included")
	}
	if name != "prompt-log.json" {
		t.Fatalf("file name = %q, want prompt-log.json", name)
	}

	var payload map[string]any
	if err := json.Unmarshal(content, &payload); err != nil {
		t.Fatalf(hooksTestJSONUnmarshalFmt, err, string(content))
	}
	assertHookJSONPathEquals(t, payload, "version", float64(1))
	assertHookJSONPathEquals(t, payload, "hooks.userPromptSubmitted.0.type", "command")
	assertHookJSONPathEquals(t, payload, "hooks.userPromptSubmitted.0.bash", hooksTestCanonicalRunCommand)
	assertHookJSONPathEquals(t, payload, "hooks.userPromptSubmitted.0.timeoutSec", float64(5))
}

func TestRenderCopilotHookFileSkipsWhenCanonicalMatchExpressionPresent(t *testing.T) {
	_, _, ok, err := renderCopilotHookFile(HookSpec{
		Name:            "prompt-log",
		When:            "user_prompt_submit",
		MatchExpression: hooksTestCanonicalMatcherExpr,
		Command:         hooksTestCanonicalRunCommand,
	})
	if err != nil {
		t.Fatalf("renderCopilotHookFile returned error: %v", err)
	}
	if ok {
		t.Fatalf("expected copilot hook render to skip matcher-constrained hook")
	}
}

func assertHookJSONPathEquals(t *testing.T, doc map[string]any, path string, want any) {
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
			idx := int(mustParseHookIndex(t, part))
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

func mustParseHookIndex(t *testing.T, s string) int64 {
	t.Helper()
	var n int64
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			t.Fatalf("invalid array index %q", s)
		}
		n = n*10 + int64(ch-'0')
	}
	return n
}

func TestListHookSpecsGraphIntegrationBundles(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, hooksTestAgentsDir)
	globalRoot := filepath.Join(agentsHome, "hooks", "global")
	writeTextFile(t, filepath.Join(globalRoot, "graph-update", "HOOK.yaml"), `name: graph-update
description: test
when: post_tool_use
match:
  expression: "Edit|Write|Bash"
run:
  command: dot-agents kg update --skip-flows
  timeout_ms: 5000
enabled_on:
  - claude
`)
	writeTextFile(t, filepath.Join(globalRoot, "graph-precommit", "HOOK.yaml"), `name: graph-precommit
description: test
when: pre_tool_use
match:
  tools:
    - Bash
run:
  command: ./graph-precommit.sh
  timeout_ms: 10000
`)
	writeTextFile(t, filepath.Join(globalRoot, "graph-precommit", "graph-precommit.sh"), "#!/bin/sh\nexit 0\n")

	specs, err := ListHookSpecs(agentsHome, "global")
	if err != nil {
		t.Fatalf("ListHookSpecs: %v", err)
	}
	var gotUpdate, gotPre *HookSpec
	for i := range specs {
		switch specs[i].Name {
		case "graph-update":
			gotUpdate = &specs[i]
		case "graph-precommit":
			gotPre = &specs[i]
		}
	}
	if gotUpdate == nil {
		t.Fatal("expected graph-update spec")
	}
	if gotUpdate.When != "post_tool_use" || !strings.Contains(gotUpdate.Command, "kg update") {
		t.Fatalf("graph-update: %#v", gotUpdate)
	}
	if gotPre == nil {
		t.Fatal("expected graph-precommit spec")
	}
	if gotPre.When != "pre_tool_use" || gotPre.Command != "./graph-precommit.sh" {
		t.Fatalf("graph-precommit: %#v", gotPre)
	}
	wantResolved := filepath.Join(globalRoot, "graph-precommit", "graph-precommit.sh")
	if got := ResolveHookCommand(*gotPre); got != wantResolved {
		t.Fatalf("ResolveHookCommand = %q, want %q", got, wantResolved)
	}
}
