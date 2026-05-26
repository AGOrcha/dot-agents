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

	err = emitHookFanout(stdPlatformIO{}, specs, dstRoot, HookEmissionMode{
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

// TestCanonicalWhenEventMapping is the table-driven verification for P1b:
// each new canonical HookSpec.When value introduced in p1a/p1b must map to
// the documented vendor event name on every supported platform, and must
// fall through (return false) on platforms whose vendor surface does not
// document the event. The expected-empty string column documents
// fall-through omissions per D2 (one canonical When → at most one
// documented event per platform).
func TestCanonicalWhenEventMapping(t *testing.T) {
	cases := []struct {
		name    string
		when    string
		claude  string // "" means: mapper must return ok=false
		codex   string
		cursor  string
		copilot string
	}{
		// Baseline coverage (p1a + pre-existing) for regression protection.
		{
			name:    "pre_tool_use",
			when:    "pre_tool_use",
			claude:  "PreToolUse",
			codex:   "PreToolUse",
			cursor:  "preToolUse",
			copilot: "preToolUse",
		},
		{
			name:    "stop",
			when:    "stop",
			claude:  "Stop",
			codex:   "Stop",
			cursor:  "stop",
			copilot: "agentStop", // P1a footgun
		},
		{
			name:    "subagent_stop",
			when:    "subagent_stop",
			claude:  "SubagentStop",
			codex:   "SubagentStop",
			cursor:  "subagentStop",
			copilot: "subagentStop",
		},
		// P1b additions: Codex parity (R6.1).
		{
			name:    "subagent_start",
			when:    "subagent_start",
			claude:  "SubagentStart",
			codex:   "SubagentStart",
			cursor:  "subagentStart",
			copilot: "subagentStart",
		},
		{
			name:    "pre_compact",
			when:    "pre_compact",
			claude:  "PreCompact",
			codex:   "PreCompact",
			cursor:  "preCompact",
			copilot: "preCompact",
		},
		{
			name:    "post_compact",
			when:    "post_compact",
			claude:  "PostCompact",
			codex:   "PostCompact",
			cursor:  "", // not in Cursor wider surface for this plan
			copilot: "", // not on Copilot's published surface
		},
		{
			name:    "permission_request",
			when:    "permission_request",
			claude:  "PermissionRequest",
			codex:   "PermissionRequest",
			cursor:  "", // not on Cursor's published surface
			copilot: "permissionRequest",
		},
		// P1b additions: Copilot parity (R6.2) + Cursor parity (R6.3).
		{
			name:    "session_end",
			when:    "session_end",
			claude:  "SessionEnd",
			codex:   "", // Codex does not document SessionEnd
			cursor:  "sessionEnd",
			copilot: "sessionEnd",
		},
		{
			name:    "post_tool_use",
			when:    "post_tool_use",
			claude:  "PostToolUse",
			codex:   "PostToolUse",
			cursor:  "postToolUse",
			copilot: "postToolUse",
		},
		{
			name:    "post_tool_use_failure",
			when:    "post_tool_use_failure",
			claude:  "PostToolUseFailure",
			codex:   "", // not on Codex's published surface
			cursor:  "postToolUseFailure",
			copilot: "postToolUseFailure",
		},
		{
			name:    "notification",
			when:    "notification",
			claude:  "Notification",
			codex:   "", // not on Codex's published surface
			cursor:  "", // not on Cursor's published surface
			copilot: "notification",
		},
		// P1b new canonical When values (R6.4).
		{
			name:    "error_occurred",
			when:    "error_occurred",
			claude:  "", // Claude does not document ErrorOccurred
			codex:   "", // Codex does not document this event
			cursor:  "", // Cursor does not document this event
			copilot: "errorOccurred",
		},
		// Cursor-wider surface (D3 + R6.3): canonical values that today
		// resolve only for Cursor. Other platforms fall through per D2.
		{
			name:   "before_shell_execution",
			when:   "before_shell_execution",
			cursor: "beforeShellExecution",
		},
		{
			name:   "after_shell_execution",
			when:   "after_shell_execution",
			cursor: "afterShellExecution",
		},
		{
			name:   "before_mcp_execution",
			when:   "before_mcp_execution",
			cursor: "beforeMCPExecution",
		},
		{
			name:   "after_mcp_execution",
			when:   "after_mcp_execution",
			cursor: "afterMCPExecution",
		},
		{
			name:   "before_read_file",
			when:   "before_read_file",
			cursor: "beforeReadFile",
		},
		{
			name:   "after_file_edit",
			when:   "after_file_edit",
			cursor: "afterFileEdit",
		},
		{
			name:   "after_agent_response",
			when:   "after_agent_response",
			cursor: "afterAgentResponse",
		},
		{
			name:   "after_agent_thought",
			when:   "after_agent_thought",
			cursor: "afterAgentThought",
		},
		{
			name:   "workspace_open",
			when:   "workspace_open",
			cursor: "workspaceOpen",
		},
		{
			name:   "before_tab_file_read",
			when:   "before_tab_file_read",
			cursor: "beforeTabFileRead",
		},
		{
			name:   "after_tab_file_edit",
			when:   "after_tab_file_edit",
			cursor: "afterTabFileEdit",
		},
		// Negative case: an entirely unknown canonical value must fall
		// through on every platform (D2 fall-through behavior).
		{
			name: "unsupported_value_falls_through",
			when: "this_event_is_not_documented_by_any_vendor",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			spec := HookSpec{Name: "t", When: tc.when, Command: "/tmp/x.sh"}
			claudeGot, claudeOK := claudeEventName(spec)
			assertWhenMaps(t, "claude", claudeGot, claudeOK, tc.claude)
			codexGot, codexOK := codexEventName(spec)
			assertWhenMaps(t, "codex", codexGot, codexOK, tc.codex)
			cursorGot, cursorOK := cursorEventName(spec)
			assertWhenMaps(t, "cursor", cursorGot, cursorOK, tc.cursor)
			copilotGot, copilotOK := copilotEventName(spec)
			assertWhenMaps(t, "copilot", copilotGot, copilotOK, tc.copilot)
		})
	}
}

// TestCanonicalWhenEventMappingRenders is the render-path assertion: each
// new mapped canonical event must produce a rendered entry under the
// expected vendor key in the per-platform configuration shape, and each
// unsupported value must be omitted entirely without raising an error
// when the hook is not RequiredOn that platform (fall-through per D2).
func TestCanonicalWhenEventMappingRenders(t *testing.T) {
	t.Run("codex subagent_start renders under SubagentStart", func(t *testing.T) {
		specs := []HookSpec{{
			Name:    "bootstrap",
			When:    "subagent_start",
			Command: "/tmp/bootstrap.sh",
		}}
		content, err := renderCodexHookConfig(specs)
		if err != nil {
			t.Fatalf("renderCodexHookConfig: %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(content, &payload); err != nil {
			t.Fatalf(hooksTestJSONUnmarshalFmt, err, string(content))
		}
		assertHookJSONPathEquals(t, payload, "hooks.SubagentStart.0.hooks.0.command", "/tmp/bootstrap.sh")
		// Per R6.5 the Codex matcher whitelist for SubagentStart is NOT
		// extended in p1b — verify the rendered matcher is empty.
		assertHookJSONPathEquals(t, payload, "hooks.SubagentStart.0.matcher", "")
	})

	t.Run("cursor before_shell_execution renders under beforeShellExecution", func(t *testing.T) {
		specs := []HookSpec{{
			Name:    "bash-guard",
			When:    "before_shell_execution",
			Command: "/tmp/guard.sh",
		}}
		content, err := renderCursorHookConfig(specs)
		if err != nil {
			t.Fatalf("renderCursorHookConfig: %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(content, &payload); err != nil {
			t.Fatalf(hooksTestJSONUnmarshalFmt, err, string(content))
		}
		assertHookJSONPathEquals(t, payload, "hooks.beforeShellExecution.0.command", "/tmp/guard.sh")
	})

	t.Run("copilot error_occurred renders under errorOccurred", func(t *testing.T) {
		name, content, ok, err := renderCopilotHookFile(HookSpec{
			Name:    "error-log",
			When:    "error_occurred",
			Command: "/tmp/log.sh",
		})
		if err != nil {
			t.Fatalf("renderCopilotHookFile: %v", err)
		}
		if !ok {
			t.Fatal("expected copilot render to include error_occurred")
		}
		if name != "error-log.json" {
			t.Fatalf("file name = %q, want error-log.json", name)
		}
		var payload map[string]any
		if err := json.Unmarshal(content, &payload); err != nil {
			t.Fatalf(hooksTestJSONUnmarshalFmt, err, string(content))
		}
		assertHookJSONPathEquals(t, payload, "hooks.errorOccurred.0.bash", "/tmp/log.sh")
	})

	t.Run("claude post_compact renders under PostCompact", func(t *testing.T) {
		specs := []HookSpec{{
			Name:    "post-compact-log",
			When:    "post_compact",
			Command: "/tmp/post.sh",
		}}
		content, err := renderClaudeHookSettings(specs)
		if err != nil {
			t.Fatalf("renderClaudeHookSettings: %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(content, &payload); err != nil {
			t.Fatalf(hooksTestJSONUnmarshalFmt, err, string(content))
		}
		assertHookJSONPathEquals(t, payload, "hooks.PostCompact.0.hooks.0.command", "/tmp/post.sh")
	})

	t.Run("cursor wider-surface value is omitted from codex and copilot renders", func(t *testing.T) {
		// after_file_edit is Cursor-only; renders for other platforms
		// must omit the spec entirely (no fall-through forced because
		// nothing in RequiredOn names them).
		spec := HookSpec{
			Name:    "edit-watch",
			When:    "after_file_edit",
			Command: "/tmp/watch.sh",
		}
		codex, err := renderCodexHookConfig([]HookSpec{spec})
		if err != nil {
			t.Fatalf("renderCodexHookConfig: %v", err)
		}
		var codexPayload codexRenderedHooks
		if err := json.Unmarshal(codex, &codexPayload); err != nil {
			t.Fatalf(hooksTestJSONUnmarshalFmt, err, string(codex))
		}
		if len(codexPayload.Hooks) != 0 {
			t.Fatalf("expected codex render to be empty for cursor-only event, got %v", codexPayload.Hooks)
		}
		_, _, ok, err := renderCopilotHookFile(spec)
		if err != nil {
			t.Fatalf("renderCopilotHookFile: %v", err)
		}
		if ok {
			t.Fatal("expected copilot render to omit cursor-only event after_file_edit")
		}
	})

	t.Run("required-on platform errors when canonical value unsupported", func(t *testing.T) {
		// Sanity: if an operator marks a Cursor-only event RequiredOn
		// codex, the render must error per D2's explicit-opt-in posture.
		spec := HookSpec{
			Name:       "edit-watch",
			When:       "after_file_edit",
			Command:    "/tmp/watch.sh",
			RequiredOn: []string{"codex"},
		}
		_, err := renderCodexHookConfig([]HookSpec{spec})
		if err == nil {
			t.Fatal("expected error when cursor-only event is required on codex")
		}
		if !strings.Contains(err.Error(), "not representable for codex") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

// assertWhenMaps validates a single mapper result against the expected
// vendor event name. Empty expected string means the mapper must return
// ok=false (fall-through per D2).
func assertWhenMaps(t *testing.T, platform string, gotName string, gotOK bool, expected string) {
	t.Helper()
	if expected == "" {
		if gotOK {
			t.Fatalf("%s mapper: expected fall-through (ok=false), got %q", platform, gotName)
		}
		return
	}
	if !gotOK {
		t.Fatalf("%s mapper: expected %q, got fall-through (ok=false)", platform, expected)
	}
	if gotName != expected {
		t.Fatalf("%s mapper: got %q, want %q", platform, gotName, expected)
	}
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
  command: da kg update --skip-flows
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
