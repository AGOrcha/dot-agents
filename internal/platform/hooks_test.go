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

// assertManyHookConfigRenders unmarshals the rendered bytes and asserts
// every (jsonPath, value) pair. Shared by codex/cursor/claude render-path
// subtests so the marshal+unmarshal+assert boilerplate exists once.
func assertManyHookConfigRenders(t *testing.T, content []byte, pairs map[string]string) {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(content, &payload); err != nil {
		t.Fatalf(hooksTestJSONUnmarshalFmt, err, string(content))
	}
	for path, want := range pairs {
		assertHookJSONPathEquals(t, payload, path, want)
	}
}

// assertCodexConfigRenders renders specs through renderCodexHookConfig and
// asserts every (jsonPath → want) pair on the parsed JSON payload.
func assertCodexConfigRenders(t *testing.T, specs []HookSpec, pairs map[string]string) {
	t.Helper()
	content, err := renderCodexHookConfig(specs)
	if err != nil {
		t.Fatalf("renderCodexHookConfig: %v", err)
	}
	assertManyHookConfigRenders(t, content, pairs)
}

// assertCursorConfigRenders mirrors assertCodexConfigRenders for cursor.
func assertCursorConfigRenders(t *testing.T, specs []HookSpec, pairs map[string]string) {
	t.Helper()
	content, err := renderCursorHookConfig(specs)
	if err != nil {
		t.Fatalf("renderCursorHookConfig: %v", err)
	}
	assertManyHookConfigRenders(t, content, pairs)
}

// assertClaudeSettingsRenders mirrors assertCodexConfigRenders for claude.
func assertClaudeSettingsRenders(t *testing.T, specs []HookSpec, pairs map[string]string) {
	t.Helper()
	content, err := renderClaudeHookSettings(specs)
	if err != nil {
		t.Fatalf("renderClaudeHookSettings: %v", err)
	}
	assertManyHookConfigRenders(t, content, pairs)
}

// assertCopilotHookFileRenders calls renderCopilotHookFile and asserts the
// resulting (name, JSON path) shape. wantPairs are matched on the parsed
// JSON payload; wantName is matched on the file basename.
func assertCopilotHookFileRenders(t *testing.T, spec HookSpec, wantName string, wantPairs map[string]string) {
	t.Helper()
	name, content, ok, err := renderCopilotHookFile(spec)
	if err != nil {
		t.Fatalf("renderCopilotHookFile: %v", err)
	}
	if !ok {
		t.Fatal("expected copilot render to include the spec")
	}
	if name != wantName {
		t.Fatalf("file name = %q, want %q", name, wantName)
	}
	assertManyHookConfigRenders(t, content, wantPairs)
}

// TestCanonicalWhenEventMappingRenders is the render-path assertion: each
// new mapped canonical event must produce a rendered entry under the
// expected vendor key in the per-platform configuration shape, and each
// unsupported value must be omitted entirely without raising an error
// when the hook is not RequiredOn that platform (fall-through per D2).
func TestCanonicalWhenEventMappingRenders(t *testing.T) {
	t.Run("codex subagent_start renders under SubagentStart with matcher", func(t *testing.T) {
		// Codex docs document SubagentStart matcher narrowing on subagent
		// type — codexMatcherWhitelist includes it post-PR-#98 review.
		assertCodexConfigRenders(t,
			[]HookSpec{{Name: "bootstrap", When: "subagent_start", Command: "/tmp/bootstrap.sh"}},
			map[string]string{
				"hooks.SubagentStart.0.hooks.0.command": "/tmp/bootstrap.sh",
				"hooks.SubagentStart.0.matcher":         "*",
			},
		)
	})

	t.Run("cursor before_shell_execution renders under beforeShellExecution", func(t *testing.T) {
		assertCursorConfigRenders(t,
			[]HookSpec{{Name: "bash-guard", When: "before_shell_execution", Command: "/tmp/guard.sh"}},
			map[string]string{"hooks.beforeShellExecution.0.command": "/tmp/guard.sh"},
		)
	})

	t.Run("copilot error_occurred renders under errorOccurred", func(t *testing.T) {
		assertCopilotHookFileRenders(t,
			HookSpec{Name: "error-log", When: "error_occurred", Command: "/tmp/log.sh"},
			"error-log.json",
			map[string]string{"hooks.errorOccurred.0.bash": "/tmp/log.sh"},
		)
	})

	t.Run("claude post_compact renders under PostCompact", func(t *testing.T) {
		assertClaudeSettingsRenders(t,
			[]HookSpec{{Name: "post-compact-log", When: "post_compact", Command: "/tmp/post.sh"}},
			map[string]string{"hooks.PostCompact.0.hooks.0.command": "/tmp/post.sh"},
		)
	})

	t.Run("cursor wider-surface value is omitted from codex and copilot renders", func(t *testing.T) {
		// after_file_edit is Cursor-only; renders for other platforms
		// must omit the spec entirely (no fall-through forced because
		// nothing in RequiredOn names them).
		spec := HookSpec{Name: "edit-watch", When: "after_file_edit", Command: "/tmp/watch.sh"}
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
		spec := HookSpec{Name: "edit-watch", When: "after_file_edit", Command: "/tmp/watch.sh", RequiredOn: []string{"codex"}}
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

// --- P1c: when_events schema extension + matcher boundary tests ---

// loadSingleHookBundle is a P1c helper that materializes a single
// canonical bundle on disk and returns the loaded HookSpec (or a load
// error). It removes the boilerplate from each table-driven case below.
func loadSingleHookBundle(t *testing.T, manifest string) (HookSpec, error) {
	t.Helper()
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, hooksTestAgentsDir)
	writeTextFile(t, filepath.Join(agentsHome, "hooks", "global", "p1c-bundle", "HOOK.yaml"), manifest)
	specs, err := ListHookSpecs(agentsHome, "global")
	if err != nil {
		return HookSpec{}, err
	}
	if len(specs) != 1 {
		t.Fatalf("expected 1 spec, got %d", len(specs))
	}
	return specs[0], nil
}

// TestLoadHookBundleAcceptsScalarWhenForBackwardCompatibility pins the
// P1c contract rule that pre-existing scalar `when` manifests load
// unchanged: WhenEvents stays empty and When carries the canonical
// event so every renderer behaves as it did before P1c.
func TestLoadHookBundleAcceptsScalarWhenForBackwardCompatibility(t *testing.T) {
	spec, err := loadSingleHookBundle(t, `name: legacy
when: pre_tool_use
run:
  command: /tmp/run.sh
`)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if spec.When != "pre_tool_use" {
		t.Fatalf("When = %q, want pre_tool_use", spec.When)
	}
	if len(spec.WhenEvents) != 0 {
		t.Fatalf("WhenEvents = %v, want empty for scalar `when`", spec.WhenEvents)
	}
}

// TestLoadHookBundleAcceptsWhenEventsArray covers the new multi-event
// path: WhenEvents is preserved verbatim, scalar When stays empty, and
// expandHookSpecEvents reports the per-event view shape downstream
// renderers consume.
func TestLoadHookBundleAcceptsWhenEventsArray(t *testing.T) {
	spec, err := loadSingleHookBundle(t, `name: multi
when_events:
  - pre_tool_use
  - stop
  - subagent_stop
run:
  command: /tmp/run.sh
`)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if spec.When != "" {
		t.Fatalf("When = %q, want empty when when_events is set", spec.When)
	}
	if got := spec.WhenEvents; len(got) != 3 || got[0] != "pre_tool_use" || got[1] != "stop" || got[2] != "subagent_stop" {
		t.Fatalf("WhenEvents = %v, want [pre_tool_use stop subagent_stop]", got)
	}
	views := expandHookSpecEvents(spec)
	if len(views) != 3 {
		t.Fatalf("expandHookSpecEvents returned %d views, want 3", len(views))
	}
	for i, want := range []string{"pre_tool_use", "stop", "subagent_stop"} {
		if views[i].When != want {
			t.Fatalf("view[%d].When = %q, want %q", i, views[i].When, want)
		}
		if len(views[i].WhenEvents) != 0 {
			t.Fatalf("view[%d].WhenEvents should be cleared after expansion", i)
		}
	}
}

// TestLoadHookBundleRejectsBothWhenAndWhenEvents enforces the
// mutual-exclusion clause of the when_events contract: setting both
// fields is a misconfiguration that must fail loudly at load time.
func TestLoadHookBundleRejectsBothWhenAndWhenEvents(t *testing.T) {
	_, err := loadSingleHookBundle(t, `name: bad
when: stop
when_events:
  - stop
  - subagent_stop
run:
  command: /tmp/run.sh
`)
	if err == nil {
		t.Fatal("expected error when both `when` and `when_events` are set")
	}
	if !strings.Contains(err.Error(), "both") {
		t.Fatalf("error %v should mention `both`", err)
	}
}

// TestLoadHookBundleRejectsDuplicateWhenEvents protects against a typo
// silently doubling the rendered actions: duplicate canonical events
// inside `when_events` are rejected.
func TestLoadHookBundleRejectsDuplicateWhenEvents(t *testing.T) {
	_, err := loadSingleHookBundle(t, `name: dup
when_events:
  - stop
  - stop
run:
  command: /tmp/run.sh
`)
	if err == nil {
		t.Fatal("expected error for duplicate canonical event in when_events")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("error %v should mention `duplicate`", err)
	}
}

// TestLoadHookBundleRejectsUnknownWhenEvent ensures unknown canonical
// values (e.g. typos like `stoop`) are caught at load time rather than
// silently no-op'ing on every platform mapper.
func TestLoadHookBundleRejectsUnknownWhenEvent(t *testing.T) {
	_, err := loadSingleHookBundle(t, `name: typo
when_events:
  - stop
  - stoop
run:
  command: /tmp/run.sh
`)
	if err == nil {
		t.Fatal("expected error for unknown canonical event in when_events")
	}
	if !strings.Contains(err.Error(), "unknown canonical event") {
		t.Fatalf("error %v should mention `unknown canonical event`", err)
	}
}

// TestRenderClaudeHookSettingsExpandsWhenEvents asserts that a single
// multi-event HookSpec fans out into one Claude entry per documented
// event, preserving the canonical command, matcher, and ordering. This
// is the Claude-side render contract.
func TestRenderClaudeHookSettingsExpandsWhenEvents(t *testing.T) {
	spec := HookSpec{
		Name:       "gate",
		WhenEvents: []string{"pre_tool_use", "stop", "subagent_stop"},
		Command:    "/tmp/gate.sh",
	}
	assertClaudeSettingsRenders(t, []HookSpec{spec}, map[string]string{
		"hooks.PreToolUse.0.hooks.0.command":   "/tmp/gate.sh",
		"hooks.Stop.0.hooks.0.command":         "/tmp/gate.sh",
		"hooks.SubagentStop.0.hooks.0.command": "/tmp/gate.sh",
		"hooks.PreToolUse.0.matcher":           "*",
		"hooks.Stop.0.matcher":                 "*",
		"hooks.SubagentStop.0.matcher":         "*",
	})
}

// TestRenderCodexHookConfigExpandsWhenEventsAndHonorsMatcherWhitelist
// is the load-bearing matcher-boundary regression: a multi-event hook
// containing matcher-supported events (PreToolUse, SubagentStart,
// SubagentStop per Codex docs) and matcher-unsupported events (Stop,
// UserPromptSubmit per Codex docs) must render with matcher="*" for the
// supported set and matcher="" for the unsupported set. This guards the
// initial P1b worker note (PR #95) that committed an incomplete
// whitelist of {SessionStart, PreToolUse, PostToolUse} and the broader
// P1c verification surface from the Codex hooks reference.
func TestRenderCodexHookConfigExpandsWhenEventsAndHonorsMatcherWhitelist(t *testing.T) {
	spec := HookSpec{
		Name:       "gate",
		WhenEvents: []string{"pre_tool_use", "subagent_start", "stop", "subagent_stop", "user_prompt_submit"},
		Command:    "/tmp/gate.sh",
	}
	assertCodexConfigRenders(t, []HookSpec{spec}, map[string]string{
		"hooks.PreToolUse.0.hooks.0.command":       "/tmp/gate.sh",
		"hooks.PreToolUse.0.matcher":               "*",
		"hooks.SubagentStart.0.hooks.0.command":    "/tmp/gate.sh",
		"hooks.SubagentStart.0.matcher":            "*",
		"hooks.SubagentStop.0.hooks.0.command":     "/tmp/gate.sh",
		"hooks.SubagentStop.0.matcher":             "*",
		"hooks.Stop.0.hooks.0.command":             "/tmp/gate.sh",
		"hooks.Stop.0.matcher":                     "",
		"hooks.UserPromptSubmit.0.hooks.0.command": "/tmp/gate.sh",
		"hooks.UserPromptSubmit.0.matcher":         "",
	})
}

// TestRenderCursorHookConfigExpandsWhenEvents asserts the Cursor render
// path expands a multi-event spec across the documented Cursor surface
// and omits the events Cursor does not document (e.g. `error_occurred`).
func TestRenderCursorHookConfigExpandsWhenEventsAndSkipsUnmappedEvents(t *testing.T) {
	spec := HookSpec{
		Name:       "gate",
		WhenEvents: []string{"pre_tool_use", "stop", "error_occurred"},
		Command:    "/tmp/gate.sh",
	}
	content, err := renderCursorHookConfig([]HookSpec{spec})
	if err != nil {
		t.Fatalf("renderCursorHookConfig: %v", err)
	}
	var payload cursorRenderedHooks
	if err := json.Unmarshal(content, &payload); err != nil {
		t.Fatalf(hooksTestJSONUnmarshalFmt, err, string(content))
	}
	if _, ok := payload.Hooks["preToolUse"]; !ok {
		t.Fatalf("expected preToolUse entry in cursor render, got %v", payload.Hooks)
	}
	if _, ok := payload.Hooks["stop"]; !ok {
		t.Fatalf("expected stop entry in cursor render, got %v", payload.Hooks)
	}
	// error_occurred is Copilot-only; Cursor mapper must omit it
	// silently because the spec did not name cursor in RequiredOn.
	if _, ok := payload.Hooks["errorOccurred"]; ok {
		t.Fatalf("expected errorOccurred to be omitted from cursor render")
	}
}

// TestRenderCopilotHookFileFanoutExpandsWhenEvents drives the Copilot
// per-file fanout: a multi-event HookSpec must produce one file per
// documented event with a deterministic suffix (no clobbering). Events
// Copilot does not document are omitted silently.
func TestRenderCopilotHookFileFanoutExpandsWhenEvents(t *testing.T) {
	tmp := t.TempDir()
	dstRoot := filepath.Join(tmp, "out")
	spec := HookSpec{
		Name:       "gate",
		WhenEvents: []string{"stop", "subagent_stop", "post_compact"},
		Command:    "/tmp/gate.sh",
	}
	if err := emitRenderedHookFanout(stdPlatformIO{}, []HookSpec{spec}, dstRoot, renderCopilotHookFile); err != nil {
		t.Fatalf("emitRenderedHookFanout: %v", err)
	}
	// `stop` -> agentStop and `subagent_stop` -> subagentStop on
	// Copilot; `post_compact` is undocumented on Copilot and must be
	// omitted silently (no file).
	for _, want := range []string{"gate-stop.json", "gate-subagent_stop.json"} {
		if _, err := os.Stat(filepath.Join(dstRoot, want)); err != nil {
			t.Fatalf("expected %s to be emitted: %v", want, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dstRoot, "gate-post_compact.json")); err == nil {
		t.Fatal("expected gate-post_compact.json to be omitted (Copilot does not document PostCompact)")
	}
}

// TestRenderCopilotHookFileFanoutPreservesSingleEventFilename ensures
// the per-event filename suffix is only applied to multi-event hooks;
// pre-P1c single-event hooks keep their `<name>.json` layout so
// downstream wiring (settings.json patches, fanout removal) is unchanged.
func TestRenderCopilotHookFileFanoutPreservesSingleEventFilename(t *testing.T) {
	tmp := t.TempDir()
	dstRoot := filepath.Join(tmp, "out")
	spec := HookSpec{
		Name:    "gate",
		When:    "stop",
		Command: "/tmp/gate.sh",
	}
	if err := emitRenderedHookFanout(stdPlatformIO{}, []HookSpec{spec}, dstRoot, renderCopilotHookFile); err != nil {
		t.Fatalf("emitRenderedHookFanout: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dstRoot, "gate.json")); err != nil {
		t.Fatalf("expected gate.json for scalar `when` hook: %v", err)
	}
}

// TestCodexMatcherWhitelistIsConstrainedToDocumentedEvents pins the
// Codex matcher boundary per the Codex hooks reference:
// PermissionRequest / PostCompact / PostToolUse / PreCompact /
// PreToolUse / SessionStart / SubagentStart / SubagentStop are in;
// Stop and UserPromptSubmit are explicitly matcher-ignored by Codex.
// Locally-mirrors codexMatcherWhitelist so a future drift between the
// two surfaces fails this test before the renderer fails the docs.
func TestCodexMatcherWhitelistIsConstrainedToDocumentedEvents(t *testing.T) {
	whitelisted := map[string]bool{
		"PermissionRequest": true,
		"PostCompact":       true,
		"PostToolUse":       true,
		"PreCompact":        true,
		"PreToolUse":        true,
		"SessionStart":      true,
		"SubagentStart":     true,
		"SubagentStop":      true,
	}
	for canonical, codexEvent := range codexEventTable {
		spec := HookSpec{Name: "probe", When: canonical, Command: "/tmp/probe.sh"}
		content, err := renderCodexHookConfig([]HookSpec{spec})
		if err != nil {
			t.Fatalf("renderCodexHookConfig %q: %v", canonical, err)
		}
		var payload codexRenderedHooks
		if err := json.Unmarshal(content, &payload); err != nil {
			t.Fatalf(hooksTestJSONUnmarshalFmt, err, string(content))
		}
		entries, ok := payload.Hooks[codexEvent]
		if !ok || len(entries) != 1 {
			t.Fatalf("%s -> %s: expected one entry, got %v", canonical, codexEvent, payload.Hooks)
		}
		gotMatcher := entries[0].Matcher
		wantMatcher := ""
		if whitelisted[codexEvent] {
			wantMatcher = "*"
		}
		if gotMatcher != wantMatcher {
			t.Fatalf("codex event %s matcher = %q, want %q (whitelist=%v)", codexEvent, gotMatcher, wantMatcher, whitelisted[codexEvent])
		}
	}
}

// TestRenderCodexHookEntry_NoCommand covers the two empty-command
// branches in renderCodexHookEntry: RequiredOn codex → error; not
// RequiredOn → silent fall-through (include=false). These are the
// PR #98 review-fix new lines that pushed file coverage below 95%.
func TestRenderCodexHookEntry_NoCommand(t *testing.T) {
	t.Run("required-on codex errors", func(t *testing.T) {
		_, _, include, err := renderCodexHookEntry(HookSpec{
			Name: "gate", When: "pre_tool_use", Command: "", RequiredOn: []string{"codex"},
		})
		if err == nil || !strings.Contains(err.Error(), "no command for codex") {
			t.Fatalf("expected 'no command for codex' error, got err=%v include=%v", err, include)
		}
	})
	t.Run("not required-on falls through", func(t *testing.T) {
		_, _, include, err := renderCodexHookEntry(HookSpec{
			Name: "gate", When: "pre_tool_use", Command: "",
		})
		if err != nil {
			t.Fatalf("expected no error on fall-through, got %v", err)
		}
		if include {
			t.Fatal("expected include=false on empty-command fall-through")
		}
	})
}

// TestRenderCursorHookEntry_NoCommand mirrors TestRenderCodexHookEntry_NoCommand
// for the Cursor renderer (PR #98 review-fix new lines).
func TestRenderCursorHookEntry_NoCommand(t *testing.T) {
	t.Run("required-on cursor errors", func(t *testing.T) {
		_, _, include, err := renderCursorHookEntry(HookSpec{
			Name: "gate", When: "pre_tool_use", Command: "", RequiredOn: []string{"cursor"},
		})
		if err == nil || !strings.Contains(err.Error(), "no command for cursor") {
			t.Fatalf("expected 'no command for cursor' error, got err=%v include=%v", err, include)
		}
	})
	t.Run("not required-on falls through", func(t *testing.T) {
		_, _, include, err := renderCursorHookEntry(HookSpec{
			Name: "gate", When: "pre_tool_use", Command: "",
		})
		if err != nil {
			t.Fatalf("expected no error on fall-through, got %v", err)
		}
		if include {
			t.Fatal("expected include=false on empty-command fall-through")
		}
	})
}

// TestRenderClaudeHookEntry_NoCommand mirrors the same shape for
// renderClaudeHookEntry — extracted helper from the PR #98 Sonar
// cog-reduction commit; cover its empty-command branches too.
func TestRenderClaudeHookEntry_NoCommand(t *testing.T) {
	t.Run("required-on claude errors", func(t *testing.T) {
		_, _, include, err := renderClaudeHookEntry(HookSpec{
			Name: "gate", When: "pre_tool_use", Command: "", RequiredOn: []string{"claude"},
		})
		if err == nil || !strings.Contains(err.Error(), "no command for claude") {
			t.Fatalf("expected 'no command for claude' error, got err=%v include=%v", err, include)
		}
	})
	t.Run("not required-on falls through", func(t *testing.T) {
		_, _, include, err := renderClaudeHookEntry(HookSpec{
			Name: "gate", When: "pre_tool_use", Command: "",
		})
		if err != nil {
			t.Fatalf("expected no error on fall-through, got %v", err)
		}
		if include {
			t.Fatal("expected include=false on empty-command fall-through")
		}
	})
}

// TestValidateHookWhenEvents_BackwardCompat covers the L587-592 branch:
// a manifest with neither `when` nor `when_events` is accepted (returns
// nil, nil) because many existing hooks rely on platform_overrides.event
// rather than canonical When. This branch was previously uncovered.
func TestValidateHookWhenEvents_BackwardCompat(t *testing.T) {
	events, err := validateHookWhenEvents("HOOK.yaml", hookManifest{})
	if err != nil {
		t.Fatalf("expected nil error for empty when + empty when_events, got %v", err)
	}
	if events != nil {
		t.Errorf("expected nil events on backward-compat path, got %v", events)
	}
}
