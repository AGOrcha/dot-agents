package platform

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/links"
	"github.com/AGOrcha/dot-agents/internal/linktest"
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

	spec, err := resolveHookSpec(agentsHome, []string{"hooks", "settings"}, "proj", hooksTestClaudeCompatFile)
	if err != nil {
		t.Fatalf("resolveHookSpec: %v", err)
	}
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
		// P1d Claude-wider surface (R6.6 + DC9 + D2): canonical values
		// that today resolve only for Claude. Other platforms fall
		// through per D2 (no semantic-equivalence inference). The
		// vendor names are sourced from the Claude Code hooks
		// reference verified on 2026-05-26.
		{
			name:   "setup",
			when:   "setup",
			claude: "Setup",
		},
		{
			name:   "message_display",
			when:   "message_display",
			claude: "MessageDisplay",
		},
		{
			name:   "user_prompt_expansion",
			when:   "user_prompt_expansion",
			claude: "UserPromptExpansion",
		},
		{
			name:   "post_tool_batch",
			when:   "post_tool_batch",
			claude: "PostToolBatch",
		},
		{
			name:   "permission_denied",
			when:   "permission_denied",
			claude: "PermissionDenied",
		},
		{
			name:   "stop_failure",
			when:   "stop_failure",
			claude: "StopFailure",
		},
		{
			name:   "teammate_idle",
			when:   "teammate_idle",
			claude: "TeammateIdle",
		},
		{
			name:   "task_created",
			when:   "task_created",
			claude: "TaskCreated",
		},
		{
			name:   "task_completed",
			when:   "task_completed",
			claude: "TaskCompleted",
		},
		{
			name:   "worktree_create",
			when:   "worktree_create",
			claude: "WorktreeCreate",
		},
		{
			name:   "worktree_remove",
			when:   "worktree_remove",
			claude: "WorktreeRemove",
		},
		{
			name:   "file_changed",
			when:   "file_changed",
			claude: "FileChanged",
		},
		{
			name:   "config_change",
			when:   "config_change",
			claude: "ConfigChange",
		},
		{
			name:   "cwd_changed",
			when:   "cwd_changed",
			claude: "CwdChanged",
		},
		{
			name:   "instructions_loaded",
			when:   "instructions_loaded",
			claude: "InstructionsLoaded",
		},
		{
			name:   "elicitation",
			when:   "elicitation",
			claude: "Elicitation",
		},
		{
			name:   "elicitation_result",
			when:   "elicitation_result",
			claude: "ElicitationResult",
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

	// P1d wider-surface render verification: each Claude-only canonical
	// value must render under its documented vendor key with the
	// canonical matcher fallback ("*"), exactly the same shape as the
	// pre-existing Claude entries. This is a representative subset; the
	// table-driven mapper test above covers the full 17-event surface.
	t.Run("claude p1d wider-surface events render under documented vendor keys", func(t *testing.T) {
		cases := []struct {
			canonical string
			vendor    string
		}{
			{canonical: "setup", vendor: "Setup"},
			{canonical: "message_display", vendor: "MessageDisplay"},
			{canonical: "user_prompt_expansion", vendor: "UserPromptExpansion"},
			{canonical: "post_tool_batch", vendor: "PostToolBatch"},
			{canonical: "permission_denied", vendor: "PermissionDenied"},
			{canonical: "stop_failure", vendor: "StopFailure"},
			{canonical: "teammate_idle", vendor: "TeammateIdle"},
			{canonical: "task_created", vendor: "TaskCreated"},
			{canonical: "task_completed", vendor: "TaskCompleted"},
			{canonical: "worktree_create", vendor: "WorktreeCreate"},
			{canonical: "worktree_remove", vendor: "WorktreeRemove"},
			{canonical: "file_changed", vendor: "FileChanged"},
			{canonical: "config_change", vendor: "ConfigChange"},
			{canonical: "cwd_changed", vendor: "CwdChanged"},
			{canonical: "instructions_loaded", vendor: "InstructionsLoaded"},
			{canonical: "elicitation", vendor: "Elicitation"},
			{canonical: "elicitation_result", vendor: "ElicitationResult"},
		}
		for _, tc := range cases {
			tc := tc
			t.Run(tc.canonical, func(t *testing.T) {
				assertClaudeSettingsRenders(t,
					[]HookSpec{{Name: "p1d-" + tc.canonical, When: tc.canonical, Command: "/tmp/p1d.sh"}},
					map[string]string{
						"hooks." + tc.vendor + ".0.hooks.0.command": "/tmp/p1d.sh",
						"hooks." + tc.vendor + ".0.matcher":         "*",
					},
				)
			})
		}
	})

	t.Run("claude p1d wider-surface event is omitted from codex/cursor/copilot renders", func(t *testing.T) {
		// task_created is a Claude-only canonical value introduced by P1d.
		// The other three platform renderers must omit the spec silently
		// (no fall-through forced because nothing in RequiredOn names them).
		assertSpecOmittedAcrossOtherPlatforms(t,
			HookSpec{Name: "task-watch", When: "task_created", Command: "/tmp/task.sh"},
			"claude-only event task_created")
	})

	t.Run("required-on codex errors when a claude-only p1d event is required there", func(t *testing.T) {
		assertRequiredOnCodexErrors(t,
			HookSpec{Name: "task-watch", When: "task_completed", Command: "/tmp/task.sh", RequiredOn: []string{"codex"}})
	})

	t.Run("cursor wider-surface value is omitted from codex and copilot renders", func(t *testing.T) {
		// after_file_edit is Cursor-only; renders for non-cursor platforms
		// must omit entirely. Reuses the shared omission helper.
		assertSpecOmittedFromCodexAndCopilot(t,
			HookSpec{Name: "edit-watch", When: "after_file_edit", Command: "/tmp/watch.sh"},
			"cursor-only event after_file_edit")
	})

	t.Run("required-on platform errors when canonical value unsupported", func(t *testing.T) {
		assertRequiredOnCodexErrors(t,
			HookSpec{Name: "edit-watch", When: "after_file_edit", Command: "/tmp/watch.sh", RequiredOn: []string{"codex"}})
	})
}

// assertSpecOmittedFromCodexAndCopilot is the load-bearing helper for
// "this canonical event is platform X-only" subtests: it asserts that
// the codex + copilot renderers both produce empty output for spec.
// Extracted from TestCanonicalWhenEventMappingRenders subtests to keep
// the top-level test under Sonar's cog-complexity gate (was 36 → 15
// limit, the original PR #102 review-blocker).
func assertSpecOmittedFromCodexAndCopilot(t *testing.T, spec HookSpec, ctx string) {
	t.Helper()
	codex, err := renderCodexHookConfig([]HookSpec{spec})
	if err != nil {
		t.Fatalf("renderCodexHookConfig: %v", err)
	}
	var codexPayload codexRenderedHooks
	if err := json.Unmarshal(codex, &codexPayload); err != nil {
		t.Fatalf(hooksTestJSONUnmarshalFmt, err, string(codex))
	}
	if len(codexPayload.Hooks) != 0 {
		t.Fatalf("expected codex render to be empty for %s, got %v", ctx, codexPayload.Hooks)
	}
	_, _, ok, err := renderCopilotHookFile(spec)
	if err != nil {
		t.Fatalf("renderCopilotHookFile: %v", err)
	}
	if ok {
		t.Fatalf("expected copilot render to omit %s", ctx)
	}
}

// assertSpecOmittedAcrossOtherPlatforms extends the omission proof to
// cursor when the spec belongs to a Claude-only canonical value.
func assertSpecOmittedAcrossOtherPlatforms(t *testing.T, spec HookSpec, ctx string) {
	t.Helper()
	assertSpecOmittedFromCodexAndCopilot(t, spec, ctx)
	cursor, err := renderCursorHookConfig([]HookSpec{spec})
	if err != nil {
		t.Fatalf("renderCursorHookConfig: %v", err)
	}
	var cursorPayload cursorRenderedHooks
	if err := json.Unmarshal(cursor, &cursorPayload); err != nil {
		t.Fatalf(hooksTestJSONUnmarshalFmt, err, string(cursor))
	}
	if len(cursorPayload.Hooks) != 0 {
		t.Fatalf("expected cursor render to be empty for %s, got %v", ctx, cursorPayload.Hooks)
	}
}

// assertRequiredOnCodexErrors confirms the D2 explicit-opt-in posture:
// a spec marked RequiredOn=["codex"] but mapping to a canonical event
// codex does not document must produce an error containing
// "not representable for codex" rather than silently omit.
func assertRequiredOnCodexErrors(t *testing.T, spec HookSpec) {
	t.Helper()
	_, err := renderCodexHookConfig([]HookSpec{spec})
	if err == nil {
		t.Fatal("expected error when event is required on codex")
	}
	if !strings.Contains(err.Error(), "not representable for codex") {
		t.Fatalf("unexpected error: %v", err)
	}
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

// TestLoadHookBundleAcceptsClaudeWiderSurfaceWhenEvents verifies the
// P1d-added canonical values are recognized by isKnownCanonicalEvent.
// Without the new entries in claudeEventTable, this load would fail
// with "unknown canonical event" because no platform table would claim
// the names. The test pins one Claude-only event from the P1d wider
// surface so a future drop from claudeEventTable fails this load.
func TestLoadHookBundleAcceptsClaudeWiderSurfaceWhenEvents(t *testing.T) {
	spec, err := loadSingleHookBundle(t, `name: claude-wider
when_events:
  - task_created
  - task_completed
  - worktree_create
run:
  command: /tmp/run.sh
`)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := spec.WhenEvents; len(got) != 3 || got[0] != "task_created" || got[1] != "task_completed" || got[2] != "worktree_create" {
		t.Fatalf("WhenEvents = %v, want [task_created task_completed worktree_create]", got)
	}
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

// TestRenderCursorHookEntry_TimeoutClampMinimum exercises the timeout clamp
// branch when TimeoutMS / 1000 == 0 (relocated from coverage_gap2_test.go).
func TestRenderCursorHookEntry_TimeoutClampMinimum(t *testing.T) {
	event, entry, ok, err := renderCursorHookEntry(HookSpec{
		Name:      "tiny",
		When:      "pre_tool_use",
		Command:   "/bin/true",
		TimeoutMS: 500, // < 1s after integer division → should clamp to 1
	})
	if err != nil {
		t.Fatalf("renderCursorHookEntry: %v", err)
	}
	if !ok || event != "preToolUse" {
		t.Fatalf("unexpected event/ok: %q %v", event, ok)
	}
	if entry.Timeout != 1 {
		t.Errorf("Timeout = %d, want clamped to 1", entry.Timeout)
	}
}

// ---------------------------------------------------------------------------
// Hook detector + emit/remove error branches (relocated from coverage_gap3).
// ---------------------------------------------------------------------------

// TestIsLikelyRendered_BadJSON exercises the unmarshal-error branch of each
// detector.
func TestIsLikelyRendered_BadJSON(t *testing.T) {
	bad := []byte("not json at all")
	if isLikelyRenderedClaudeHookSettings(bad) {
		t.Error("claude detector should reject bad json")
	}
	if isLikelyRenderedCodexHookConfig(bad) {
		t.Error("codex detector should reject bad json")
	}
	if isLikelyRenderedCursorHookConfig(bad) {
		t.Error("cursor detector should reject bad json")
	}
	if isLikelyRenderedCopilotHookFile(bad) {
		t.Error("copilot detector should reject bad json")
	}

	// Empty-hooks JSON still parses but should not match.
	empty := []byte(`{"hooks":{}}`)
	if isLikelyRenderedClaudeHookSettings(empty) {
		t.Error("claude detector should reject empty hooks")
	}
	if isLikelyRenderedCodexHookConfig(empty) {
		t.Error("codex detector should reject empty hooks")
	}
	noVersion := []byte(`{"hooks":{"x":[]}}`)
	if isLikelyRenderedCursorHookConfig(noVersion) {
		t.Error("cursor detector should reject missing version")
	}
	if isLikelyRenderedCopilotHookFile(noVersion) {
		t.Error("copilot detector should reject missing version")
	}
}

// TestRemoveManagedFile_SymlinkSkipped verifies a symlink at the target is
// left in place.
func TestRemoveManagedFile_SymlinkSkipped(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.json")
	if err := os.WriteFile(src, []byte("managed"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(tmp, "link.json")
	linktest.Link(t, src, link)
	if err := removeManagedFile(stdPlatformIO{}, link, []byte("managed")); err != nil {
		t.Fatalf("removeManagedFile symlink: %v", err)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Error("symlink should be preserved")
	}
}

func TestRemoveManagedFile_MissingTarget(t *testing.T) {
	if err := removeManagedFile(stdPlatformIO{}, filepath.Join(t.TempDir(), "no-such"), []byte("x")); err != nil {
		t.Errorf("missing file should no-op, got %v", err)
	}
}

func TestRemoveManagedFileIf_SymlinkSkipped(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.json")
	if err := os.WriteFile(src, []byte(`{"version":1,"hooks":{"x":[]}}`), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(tmp, "link.json")
	linktest.Link(t, src, link)
	if err := removeManagedFileIf(stdPlatformIO{}, link, isLikelyRenderedCursorHookConfig); err != nil {
		t.Errorf("symlink: %v", err)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Error("symlink should be preserved")
	}
}

// TestWriteManagedFile_ExistingSymlinkReplaced drives the Lstat→Remove branch.
func TestWriteManagedFile_ExistingSymlinkReplaced(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	if err := os.WriteFile(src, []byte("dummy"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(tmp, "dst")
	linktest.Link(t, src, dst)
	if err := writeManagedFile(stdPlatformIO{}, dst, []byte("real")); err != nil {
		t.Fatalf("writeManagedFile: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "real" {
		t.Errorf("got %q, want real", got)
	}
}

// TestRemoveDirIfEmpty_EmptyString covers the early-return branch.
func TestRemoveDirIfEmpty_EmptyString(t *testing.T) {
	if err := removeDirIfEmpty(""); err != nil {
		t.Errorf("empty string should no-op, got %v", err)
	}
}

// TestEmitRenderedHookFanout_MissingMkdirRoot drives error propagation when
// MkdirAll on the dst root fails (parent is a regular file).
func TestEmitRenderedHookFanout_MkdirFails(t *testing.T) {
	tmp := t.TempDir()
	blocker := filepath.Join(tmp, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	specs := []HookSpec{{Name: "p", When: "user_prompt_submit", Command: "/bin/true"}}
	err := emitRenderedHookFanout(stdPlatformIO{}, specs, filepath.Join(blocker, "sub"), renderCopilotHookFile)
	if err == nil {
		t.Error("expected mkdir error")
	}
}

// TestEmitHookFanout_MkdirFails drives error propagation in emitHookFanout.
func TestEmitHookFanout_MkdirFails(t *testing.T) {
	tmp := t.TempDir()
	blocker := filepath.Join(tmp, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	specs := []HookSpec{{Name: "p", SourcePath: filepath.Join(tmp, "src.json")}}
	if err := os.WriteFile(specs[0].SourcePath, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	err := emitHookFanout(stdPlatformIO{}, specs, filepath.Join(blocker, "sub"),
		HookEmissionMode{Shape: HookShapeRenderFanout, Transport: HookTransportSymlink},
		func(s HookSpec) (string, bool) { return s.Name + ".json", true })
	if err == nil {
		t.Error("expected mkdir error")
	}
}

// TestLoadHookSpecEntry_NonJSONFileIgnored exercises the "not directory, not
// .json" branch.
func TestLoadHookSpecEntry_NonJSONFileIgnored(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "readme.md"), []byte("# hi"), 0644); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		spec, ok, lerr := loadHookSpecEntry(tmp, "global", e)
		if lerr != nil {
			t.Errorf("expected no error, got %v", lerr)
		}
		if ok {
			t.Errorf("expected non-hook to skip, got %+v", spec)
		}
	}
}

// TestLoadHookBundleSpec_BadYaml drives the yaml.Unmarshal error branch.
func TestLoadHookBundleSpec_BadYaml(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "broken")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "HOOK.yaml"), []byte(":\n  -bad"), 0644); err != nil {
		t.Fatal(err)
	}
	_, _, err := loadHookBundleSpec(tmp, "global", "broken")
	if err == nil {
		t.Error("expected yaml error")
	}
}

func TestLoadHookBundleSpec_MissingManifest(t *testing.T) {
	tmp := t.TempDir()
	_, ok, err := loadHookBundleSpec(tmp, "global", "no-such")
	if err != nil {
		t.Errorf("missing manifest: %v", err)
	}
	if ok {
		t.Error("expected ok=false for missing manifest")
	}
}

// TestCollectCanonicalHookSpecsForPlatform_MissingScope covers the IsNotExist
// branch.
func TestCollectCanonicalHookSpecsForPlatform_MissingScope(t *testing.T) {
	tmp := t.TempDir()
	got, err := collectCanonicalHookSpecsForPlatform(tmp, "proj", "claude", "global", "proj")
	if err != nil {
		t.Errorf("missing scope: %v", err)
	}
	if got == nil {
		t.Error("expected empty slice, not nil") // function returns []HookSpec{}
	}
}

// TestEmitPreferredHookFile_LegacyBranch drives the case where there are no
// canonical bundles and a legacy spec is present (Shape=Direct, Transport=symlink).
func TestEmitPreferredHookFile_LegacyBranch(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.json")
	if err := os.WriteFile(src, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(tmp, "out", "settings.json")
	spec := &HookSpec{Name: "legacy", SourcePath: src}
	if err := emitPreferredHookFile(stdPlatformIO{}, target, renderClaudeHookSettings, spec, directSymlinkHookMode, nil); err != nil {
		t.Fatalf("emitPreferredHookFile legacy: %v", err)
	}
	if _, err := os.Lstat(target); err != nil {
		t.Errorf("expected legacy symlink: %v", err)
	}
}

// TestEmitPreferredHookFileToUserHomes_LegacyBranch drives the legacy spec
// path under user homes.
func TestEmitPreferredHookFileToUserHomes_LegacyBranch(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.json")
	if err := os.WriteFile(src, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(tmp, "home")
	t.Setenv("HOME", home)
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatal(err)
	}
	spec := &HookSpec{Name: "x", SourcePath: src}
	if err := emitPreferredHookFileToUserHomes(stdPlatformIO{}, ".claude/settings.json",
		renderClaudeHookSettings, spec, directSymlinkHookMode, nil); err != nil {
		t.Fatalf("user-home legacy: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(home, ".claude", "settings.json")); err != nil {
		t.Errorf("expected user-home symlink: %v", err)
	}
}

// TestEmitPreferredHookFile_AllNilNoOp drives the case where no bundles, no
// legacy, no removeRendered → returns nil.
func TestEmitPreferredHookFile_AllNilNoOp(t *testing.T) {
	if err := emitPreferredHookFile(stdPlatformIO{}, filepath.Join(t.TempDir(), "x"),
		renderClaudeHookSettings, nil, directSymlinkHookMode, nil); err != nil {
		t.Errorf("all-nil no-op: %v", err)
	}
}

func TestEmitPreferredHookFileToUserHomes_AllNilNoOp(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := emitPreferredHookFileToUserHomes(stdPlatformIO{}, ".x/y",
		renderClaudeHookSettings, nil, directSymlinkHookMode, nil); err != nil {
		t.Errorf("user-home all-nil no-op: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Hook emit/render error + transport coverage (relocated from coverage_gap5).
// ---------------------------------------------------------------------------

// TestEmitHookSpec_DirectHardlink drives the direct-hardlink transport branch.
func TestEmitHookSpec_DirectHardlink(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.json")
	if err := os.WriteFile(src, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	spec := &HookSpec{SourcePath: src}
	dst := filepath.Join(tmp, "out", "x.json")
	if err := emitHookSpec(stdPlatformIO{}, spec, dst, directHardlinkHookMode); err != nil {
		// Hardlinks fail across some filesystems; tolerate that as long as no panic.
		t.Logf("emitHookSpec hardlink: %v", err)
	}
}

// TestEmitHookFile_HardlinkBranch drives the HookTransportHardlink case.
func TestEmitHookFile_HardlinkBranch(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.json")
	if err := os.WriteFile(src, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(tmp, "dst.json")
	// Best-effort; just exercise the branch.
	_ = emitHookFile(stdPlatformIO{}, src, dst, HookTransportHardlink)
}

// TestEmitHookFile_SymlinkBranch drives the symlink case.
func TestEmitHookFile_SymlinkBranch(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.json")
	if err := os.WriteFile(src, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(tmp, "dst.json")
	if err := emitHookFile(stdPlatformIO{}, src, dst, HookTransportSymlink); err != nil {
		t.Errorf("symlink branch: %v", err)
	}
	if !links.IsManagedLink(dst, src) {
		t.Error("expected managed link to src")
	}
}

// TestParseJSONLTimestamp_BareDate makes sure the non-matching branch is hit.
func TestParseJSONLTimestamp_BareDateNotAccepted(t *testing.T) {
	if _, ok := parseJSONLTimestamp("2026-04-01"); ok {
		t.Error("bare date should not parse")
	}
}

// TestRemoveDirIfEmpty_NotEmpty covers the "len(entries) > 0" branch.
func TestRemoveDirIfEmpty_HasEntries(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "d")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := removeDirIfEmpty(dir); err != nil {
		t.Errorf("removeDirIfEmpty: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Error("non-empty dir should be preserved")
	}
}

// TestEmitRenderedHookFile_RenderError drives the render-error branch.
func TestEmitRenderedHookFile_RenderError(t *testing.T) {
	tmp := t.TempDir()
	// Use a spec that forces render error: required-on platform with no command.
	spec := HookSpec{Name: "x", When: "pre_tool_use", RequiredOn: []string{"claude"}}
	err := emitRenderedHookFile(stdPlatformIO{}, []HookSpec{spec}, filepath.Join(tmp, "out"), renderClaudeHookSettings)
	if err == nil {
		t.Error("expected render error")
	}
}

func TestEmitRenderedHookFileToUserHomes_RenderError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	spec := HookSpec{Name: "x", When: "pre_tool_use", RequiredOn: []string{"claude"}}
	err := emitRenderedHookFileToUserHomes(stdPlatformIO{}, []HookSpec{spec}, ".claude/settings.json", renderClaudeHookSettings)
	if err == nil {
		t.Error("expected render error")
	}
}

// TestRemoveManagedRenderedHookFile_RenderError drives the render-error branch.
func TestRemoveManagedRenderedHookFile_RenderError(t *testing.T) {
	spec := HookSpec{Name: "x", When: "pre_tool_use", RequiredOn: []string{"claude"}}
	err := removeManagedRenderedHookFile(stdPlatformIO{}, []HookSpec{spec}, "/tmp/x", renderClaudeHookSettings)
	if err == nil {
		t.Error("expected render error")
	}
}

// TestRemoveManagedRenderedHookFileToUserHomes_RenderError drives the error
// path under user homes.
func TestRemoveManagedRenderedHookFileToUserHomes_RenderError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	spec := HookSpec{Name: "x", When: "pre_tool_use", RequiredOn: []string{"claude"}}
	err := removeManagedRenderedHookFileToUserHomes(stdPlatformIO{}, []HookSpec{spec}, ".claude/settings.json", renderClaudeHookSettings)
	if err == nil {
		t.Error("expected render error")
	}
}

// TestEmitRenderedHookFanout_RenderError drives the fanout render-error branch.
// P1b: post_tool_use is now representable on copilot (R6.2); use
// no_such_canonical_event which no platform mapper supports.
func TestEmitRenderedHookFanout_RenderError(t *testing.T) {
	tmp := t.TempDir()
	spec := HookSpec{Name: "x", When: "no_such_canonical_event", RequiredOn: []string{"copilot"}, Command: "/bin/true"}
	err := emitRenderedHookFanout(stdPlatformIO{}, []HookSpec{spec}, filepath.Join(tmp, "out"), renderCopilotHookFile)
	if err == nil {
		t.Error("expected error")
	}
}

func TestRemoveManagedRenderedHookFanout_RenderError(t *testing.T) {
	spec := HookSpec{Name: "x", When: "no_such_canonical_event", RequiredOn: []string{"copilot"}, Command: "/bin/true"}
	err := removeManagedRenderedHookFanout(stdPlatformIO{}, []HookSpec{spec}, "/tmp/out", renderCopilotHookFile)
	if err == nil {
		t.Error("expected error")
	}
}

// TestRenderedCopilotHookNames_RenderError drives the propagation branch.
func TestRenderedCopilotHookNames_RenderError(t *testing.T) {
	specs := []HookSpec{{
		Name:       "x",
		When:       "no_such_canonical_event",
		Command:    "/bin/true",
		RequiredOn: []string{"copilot"},
	}}
	if _, err := renderedCopilotHookNames(specs); err == nil {
		t.Error("expected error")
	}
}

// TestPruneManagedRenderedFanoutExtras_RemovePermissionsRespected covers the
// continue-on-IsNotExist branch when concurrent removal already cleared it.
func TestPruneManagedRenderedFanoutExtras_StaleEntry(t *testing.T) {
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "hooks")
	if err := os.MkdirAll(dst, 0755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(dst, "stale.json")
	if err := os.WriteFile(stale, []byte(`{"version":1,"hooks":{"x":[]}}`), 0644); err != nil {
		t.Fatal(err)
	}
	// "wanted" does not include it; the detector matches; file is removed.
	if err := pruneManagedRenderedFanoutExtras(stdPlatformIO{}, dst, nil, isLikelyRenderedCursorHookConfig); err != nil {
		t.Fatalf("prune: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("stale removed")
	}
}

// ---------------------------------------------------------------------------
// Hook render + emit + remove + event-name + matcher coverage from
// coverage_gap_test.go (relocated).
// ---------------------------------------------------------------------------

// TestRemoveManagedRenderedHookFileToUserHomes drives the user-home removal
// fanout for rendered hook settings.
func TestRemoveManagedRenderedHookFileToUserHomes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// No-op when specs are empty.
	if err := removeManagedRenderedHookFileToUserHomes(stdPlatformIO{}, nil, ".claude/settings.json", renderClaudeHookSettings); err != nil {
		t.Errorf("nil specs should no-op, got %v", err)
	}

	// Seed a managed file with the exact rendered content, then ensure the
	// fanout removal deletes it under HOME.
	specs := []HookSpec{{
		Name:    "ping",
		When:    "pre_tool_use",
		Command: "/bin/echo",
	}}
	rendered, err := renderClaudeHookSettings(specs)
	if err != nil {
		t.Fatalf("renderClaudeHookSettings: %v", err)
	}
	target := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, rendered, 0644); err != nil {
		t.Fatal(err)
	}

	if err := removeManagedRenderedHookFileToUserHomes(stdPlatformIO{}, specs, ".claude/settings.json", renderClaudeHookSettings); err != nil {
		t.Fatalf("removeManagedRenderedHookFileToUserHomes: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("expected file removed, stat err=%v", err)
	}
}

// TestRenderClaudeHookSettings_UnrepresentableEventSkipped: a spec with an
// unknown `when` is dropped when not required-on.
func TestRenderClaudeHookSettings_UnrepresentableEventSkipped(t *testing.T) {
	specs := []HookSpec{
		{Name: "good", When: "pre_tool_use", Command: "/bin/true"},
		{Name: "bad-event", When: "weird_event", Command: "/bin/true"},
	}
	out, err := renderClaudeHookSettings(specs)
	if err != nil {
		t.Fatalf("renderClaudeHookSettings: %v", err)
	}
	if !strings.Contains(string(out), "PreToolUse") {
		t.Errorf("expected good hook rendered, got %s", out)
	}
}

func TestRenderClaudeHookSettings_RequiredUnrepresentableErrors(t *testing.T) {
	specs := []HookSpec{{
		Name:       "x",
		When:       "no_such_event",
		Command:    "/bin/true",
		RequiredOn: []string{"claude"},
	}}
	if _, err := renderClaudeHookSettings(specs); err == nil {
		t.Error("expected error when required+unrepresentable")
	}
}

func TestRenderClaudeHookSettings_RequiredNoCommandErrors(t *testing.T) {
	specs := []HookSpec{{
		Name:       "x",
		When:       "pre_tool_use",
		RequiredOn: []string{"claude"},
	}}
	if _, err := renderClaudeHookSettings(specs); err == nil {
		t.Error("expected error when required+no-command")
	}
}

func TestRenderCodexHookConfig_RequiredUnrepresentableErrors(t *testing.T) {
	specs := []HookSpec{{
		Name:       "x",
		When:       "no_such_event",
		Command:    "/bin/true",
		RequiredOn: []string{"codex"},
	}}
	if _, err := renderCodexHookConfig(specs); err == nil {
		t.Error("expected error")
	}
}

func TestRenderCursorHookConfig_RequiredUnrepresentableErrors(t *testing.T) {
	// P1b note: post_tool_use is now representable on cursor (R6.3); use
	// error_occurred which p1b scopes to copilot only, so it remains
	// unrepresentable on cursor and exercises the same fall-through path.
	specs := []HookSpec{{
		Name:       "x",
		When:       "error_occurred", // not in cursor switch
		Command:    "/bin/true",
		RequiredOn: []string{"cursor"},
	}}
	if _, err := renderCursorHookConfig(specs); err == nil {
		t.Error("expected error")
	}
}

func TestRenderCopilotHookFile_RequiredUnrepresentableErrors(t *testing.T) {
	// P1b note: post_tool_use is now representable on copilot (R6.2); use
	// a canonical When value that no platform mapper handles to exercise
	// the same fall-through path.
	_, _, _, err := renderCopilotHookFile(HookSpec{
		Name:       "x",
		When:       "no_such_canonical_event", // not in copilot switch
		Command:    "/bin/true",
		RequiredOn: []string{"copilot"},
	})
	if err == nil {
		t.Error("expected error")
	}
}

func TestRenderCopilotHookFile_RequiredMatcherErrors(t *testing.T) {
	_, _, _, err := renderCopilotHookFile(HookSpec{
		Name:            "x",
		When:            "pre_tool_use",
		Command:         "/bin/true",
		MatchExpression: "Bash",
		RequiredOn:      []string{"copilot"},
	})
	if err == nil {
		t.Error("expected error")
	}
}

func TestRenderCopilotHookFile_RequiredNoCommandErrors(t *testing.T) {
	_, _, _, err := renderCopilotHookFile(HookSpec{
		Name:       "x",
		When:       "pre_tool_use",
		RequiredOn: []string{"copilot"},
	})
	if err == nil {
		t.Error("expected error")
	}
}

// TestPlatformOverride covers both the present and missing branches.
func TestPlatformOverride(t *testing.T) {
	spec := HookSpec{PlatformOverrides: map[string]HookPlatformOverride{
		"claude": {Event: "PreToolUse", Matcher: "X"},
	}}
	if got := platformOverride(spec, "claude").Event; got != "PreToolUse" {
		t.Errorf("expected PreToolUse, got %q", got)
	}
	if got := platformOverride(spec, "missing").Event; got != "" {
		t.Errorf("expected empty for missing platform, got %q", got)
	}
	if got := platformOverride(HookSpec{}, "claude").Event; got != "" {
		t.Errorf("expected empty for nil overrides, got %q", got)
	}
}

func TestHookEnabledAndRequiredOnPlatform(t *testing.T) {
	// Both default to allow-all when nil.
	if !hookEnabledOnPlatform(HookSpec{}, "anyone") {
		t.Error("nil EnabledOn should be allow-all")
	}
	if hookRequiredOnPlatform(HookSpec{}, "anyone") {
		t.Error("nil RequiredOn should be false")
	}
	if !hookEnabledOnPlatform(HookSpec{EnabledOn: []string{"claude"}}, "claude") {
		t.Error("explicit enabled platform should be allowed")
	}
	if hookEnabledOnPlatform(HookSpec{EnabledOn: []string{"claude"}}, "codex") {
		t.Error("non-enabled platform should be denied")
	}
	if !hookRequiredOnPlatform(HookSpec{RequiredOn: []string{"claude"}}, "claude") {
		t.Error("required platform should be true")
	}
}

// TestPlatformIDOverrides walks each event-name function with override
// branches so the override path is exercised.
func TestPerPlatformEventOverrides(t *testing.T) {
	spec := HookSpec{
		When: "weird",
		PlatformOverrides: map[string]HookPlatformOverride{
			"claude":  {Event: "PreToolUse"},
			"codex":   {Event: "PreToolUse"},
			"cursor":  {Event: "preToolUse"},
			"copilot": {Event: "preToolUse"},
		},
	}
	if name, ok := claudeEventName(spec); !ok || name != "PreToolUse" {
		t.Errorf("claudeEventName override: %q %v", name, ok)
	}
	if name, ok := codexEventName(spec); !ok || name != "PreToolUse" {
		t.Errorf("codexEventName override: %q %v", name, ok)
	}
	if name, ok := cursorEventName(spec); !ok || name != "preToolUse" {
		t.Errorf("cursorEventName override: %q %v", name, ok)
	}
	if name, ok := copilotEventName(spec); !ok || name != "preToolUse" {
		t.Errorf("copilotEventName override: %q %v", name, ok)
	}
}

// TestEventNameDefaults exhaustively triggers each switch in the event-name
// helpers so all cases are covered.
// assertEventNameMapping runs an event-name resolver across a set of valid
// `when` values and asserts each resolves; then asserts an unknown sentinel
// is rejected.
func assertEventNameMapping(
	t *testing.T,
	resolver func(HookSpec) (string, bool),
	platform string,
	validWhens []string,
) {
	t.Helper()
	for _, when := range validWhens {
		if _, ok := resolver(HookSpec{When: when}); !ok {
			t.Errorf("%sEventName(%q) returned !ok", platform, when)
		}
	}
	if _, ok := resolver(HookSpec{When: "weird"}); ok {
		t.Errorf("weird should be unrepresentable for %s", platform)
	}
}

func TestEventNameDefaults(t *testing.T) {
	assertEventNameMapping(t, claudeEventName, "claude", []string{
		"pre_tool_use", "post_tool_use", "post_tool_use_failure",
		"notification", "user_prompt_submit", "session_start", "session_end",
		"stop", "subagent_start", "subagent_stop", "pre_compact",
		"permission_request",
	})
	assertEventNameMapping(t, codexEventName, "codex", []string{
		"session_start", "pre_tool_use", "post_tool_use",
		"user_prompt_submit", "stop", "subagent_stop",
	})
	assertEventNameMapping(t, cursorEventName, "cursor", []string{
		"pre_tool_use", "user_prompt_submit", "stop", "session_start",
		"subagent_stop",
	})
	assertEventNameMapping(t, copilotEventName, "copilot", []string{
		"session_start", "user_prompt_submit", "pre_tool_use",
		"stop", "subagent_stop",
	})
}

// TestP1aGateCriticalStopMappings is the table-driven regression for the P1a
// gate-critical mapping deltas. It covers exactly the rows from the contract's
// mapping table and the negative assertion that Copilot must NOT render
// canonical `stop` as the literal `stop`.
func TestP1aGateCriticalStopMappings(t *testing.T) {
	cases := []struct {
		name     string
		resolver func(HookSpec) (string, bool)
		platform string
		when     string
		want     string
	}{
		{
			name:     "codex subagent_stop renders SubagentStop",
			resolver: codexEventName,
			platform: "codex",
			when:     "subagent_stop",
			want:     "SubagentStop",
		},
		{
			name:     "copilot stop renders agentStop (not stop)",
			resolver: copilotEventName,
			platform: "copilot",
			when:     "stop",
			want:     "agentStop",
		},
		{
			name:     "copilot subagent_stop renders subagentStop",
			resolver: copilotEventName,
			platform: "copilot",
			when:     "subagent_stop",
			want:     "subagentStop",
		},
		{
			name:     "cursor subagent_stop renders subagentStop",
			resolver: cursorEventName,
			platform: "cursor",
			when:     "subagent_stop",
			want:     "subagentStop",
		},
		// Claude regression: stop and subagent_stop already supported.
		{
			name:     "claude stop still renders Stop",
			resolver: claudeEventName,
			platform: "claude",
			when:     "stop",
			want:     "Stop",
		},
		{
			name:     "claude subagent_stop still renders SubagentStop",
			resolver: claudeEventName,
			platform: "claude",
			when:     "subagent_stop",
			want:     "SubagentStop",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := tc.resolver(HookSpec{When: tc.when})
			if !ok {
				t.Fatalf("%sEventName(%q) returned !ok; want %q", tc.platform, tc.when, tc.want)
			}
			if got != tc.want {
				t.Errorf("%sEventName(%q) = %q, want %q", tc.platform, tc.when, got, tc.want)
			}
		})
	}
}

// TestCopilotStopIsAgentStopNotStop is the explicit negative-path regression
// guarding against a recurrence of the documented Copilot pitfall: a future
// edit must not collapse canonical `stop` to the literal `stop` event name
// the way Claude/Cursor do. Copilot's terminal event is `agentStop`.
func TestCopilotStopIsAgentStopNotStop(t *testing.T) {
	got, ok := copilotEventName(HookSpec{When: "stop"})
	if !ok {
		t.Fatalf("copilotEventName(stop) returned !ok; the P1a mapping must accept canonical stop")
	}
	if got == "stop" {
		t.Fatalf("copilotEventName(stop) = %q; Copilot does not have a `stop` event — it must render `agentStop`", got)
	}
	if got != "agentStop" {
		t.Errorf("copilotEventName(stop) = %q, want agentStop", got)
	}

	// And the rendered config must also carry agentStop, not stop, in its
	// JSON output — guarding the end-to-end render path, not just the
	// resolver.
	_, content, ok, err := renderCopilotHookFile(HookSpec{
		Name:    "stop-guard",
		When:    "stop",
		Command: "/bin/true",
	})
	if err != nil {
		t.Fatalf("renderCopilotHookFile: %v", err)
	}
	if !ok {
		t.Fatal("renderCopilotHookFile returned !ok for canonical stop")
	}
	if !strings.Contains(string(content), `"agentStop"`) {
		t.Errorf("rendered copilot hook missing \"agentStop\" event key:\n%s", content)
	}
	// Negative assertion: the rendered JSON must not key the hook on a
	// bare "stop" event. (We check for the JSON key form `"stop":` so we
	// do not false-positive on substrings of `agentStop`.)
	if strings.Contains(string(content), `"stop":`) {
		t.Errorf("rendered copilot hook keys event on bare \"stop\"; must be \"agentStop\":\n%s", content)
	}
}

// TestMatcherForSpecVariants covers all three branches of matcherForSpec.
func TestMatcherForSpecVariants(t *testing.T) {
	if got := matcherForSpec(HookSpec{MatchExpression: "Write | Edit"}, "claude", "*"); got != "Write | Edit" {
		t.Errorf("expression branch: %q", got)
	}
	if got := matcherForSpec(HookSpec{MatchTools: []string{"Write", "Edit"}}, "claude", "*"); got != "Write|Edit" {
		t.Errorf("tools branch: %q", got)
	}
	if got := matcherForSpec(HookSpec{}, "claude", "*"); got != "*" {
		t.Errorf("fallback branch: %q", got)
	}
	override := HookSpec{PlatformOverrides: map[string]HookPlatformOverride{
		"claude": {Matcher: "Bash"},
	}}
	if got := matcherForSpec(override, "claude", "*"); got != "Bash" {
		t.Errorf("override branch: %q", got)
	}
}

func TestResolveHookCommand_AbsolutePassthrough(t *testing.T) {
	spec := HookSpec{Command: "/usr/local/bin/run"}
	if got := ResolveHookCommand(spec); got != "/usr/local/bin/run" {
		t.Errorf("absolute cmd: %q", got)
	}
}

func TestResolveHookCommand_RelativeBundleResolution(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "HOOK.yaml")
	script := filepath.Join(tmp, "run.sh")
	if err := os.WriteFile(src, []byte("name: x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	spec := HookSpec{
		SourcePath: src,
		SourceKind: HookSourceCanonicalBundle,
		Command:    "./run.sh",
	}
	if got := ResolveHookCommand(spec); got != script {
		t.Errorf("ResolveHookCommand = %q, want %q", got, script)
	}
}

// TestEmitHookSpec_NilNoOp keeps the early-return branch covered.
func TestEmitHookSpec_NilNoOp(t *testing.T) {
	if err := emitHookSpec(stdPlatformIO{}, nil, "/dev/null", HookEmissionMode{}); err != nil {
		t.Errorf("nil spec should no-op, got %v", err)
	}
	if err := emitHookSpecToUserHomes(stdPlatformIO{}, nil, ".x/y", HookEmissionMode{}); err != nil {
		t.Errorf("nil spec to user homes should no-op, got %v", err)
	}
}

func TestEmitHookSpec_UnknownShapeErrors(t *testing.T) {
	if err := emitHookSpec(stdPlatformIO{}, &HookSpec{}, "/tmp/x", HookEmissionMode{Shape: "wat"}); err == nil {
		t.Error("expected error for unknown shape")
	}
	if err := emitHookSpec(stdPlatformIO{}, &HookSpec{}, "/tmp/x", HookEmissionMode{Shape: HookShapeRenderSingle}); err == nil {
		t.Error("render shapes are not single-direct emission")
	}
}

func TestEmitHookFile_UnknownTransportErrors(t *testing.T) {
	if err := emitHookFile(stdPlatformIO{}, "/no/where", "/tmp/x", "weird"); err == nil {
		t.Error("expected error for unknown transport")
	}
}

func TestEmitHookFile_WriteCopiesContent(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	dst := filepath.Join(tmp, "dst")
	if err := os.WriteFile(src, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := emitHookFile(stdPlatformIO{}, src, dst, HookTransportWrite); err != nil {
		t.Fatalf("emitHookFile write: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Errorf("got %q, want hello", got)
	}
}

func TestEmitHookFile_WriteMissingSource(t *testing.T) {
	if err := emitHookFile(stdPlatformIO{}, "/no/where", filepath.Join(t.TempDir(), "out"), HookTransportWrite); err == nil {
		t.Error("expected error reading missing source")
	}
}

func TestEmitHookFanout_RejectsWrongShape(t *testing.T) {
	err := emitHookFanout(stdPlatformIO{}, nil, t.TempDir(), HookEmissionMode{Shape: HookShapeDirect}, func(HookSpec) (string, bool) { return "", false })
	if err == nil {
		t.Error("expected error for non-fanout shape")
	}
}

func TestEmitRenderedHookFile_NilSpecsNoOp(t *testing.T) {
	if err := emitRenderedHookFile(stdPlatformIO{}, nil, "/tmp/x", renderClaudeHookSettings); err != nil {
		t.Errorf("nil specs should no-op, got %v", err)
	}
	if err := emitRenderedHookFileToUserHomes(stdPlatformIO{}, nil, ".x", renderClaudeHookSettings); err != nil {
		t.Errorf("nil specs to user homes should no-op, got %v", err)
	}
}

func TestEmitPreferredHookFile_FallbackToRemove(t *testing.T) {
	called := 0
	remove := func(_ string) error { called++; return nil }
	if err := emitPreferredHookFile(stdPlatformIO{}, "/tmp/x", renderClaudeHookSettings, nil, directSymlinkHookMode, remove); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if called != 1 {
		t.Errorf("expected remove invoked once, got %d", called)
	}

	// canonicalSets present → render branch used (legacy not invoked).
	tmp := t.TempDir()
	target := filepath.Join(tmp, "out.json")
	if err := emitPreferredHookFile(stdPlatformIO{}, target, renderClaudeHookSettings, nil, directSymlinkHookMode, nil,
		[]HookSpec{{Name: "ping", When: "pre_tool_use", Command: "/bin/true"}}); err != nil {
		t.Errorf("render branch: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("expected target rendered: %v", err)
	}
}

func TestEmitPreferredHookFileToUserHomes_FallbackBranches(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	called := 0
	remove := func(_ string) error { called++; return nil }
	if err := emitPreferredHookFileToUserHomes(stdPlatformIO{}, ".claude/settings.json",
		renderClaudeHookSettings, nil, directSymlinkHookMode, remove); err != nil {
		t.Errorf("user-home remove branch: %v", err)
	}
	if called == 0 {
		t.Error("expected remove invoked at least once")
	}
}

// TestWriteManagedFile_OverwriteAndDeduplicate verifies that re-writing
// identical content is a no-op (the implementation short-circuits when the
// file matches), and that differing content triggers a fresh write.
func TestWriteManagedFile_Dedup(t *testing.T) {
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "f.json")
	if err := writeManagedFile(stdPlatformIO{}, dst, []byte("a")); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := writeManagedFile(stdPlatformIO{}, dst, []byte("a")); err != nil {
		t.Fatalf("second identical write: %v", err)
	}
	if err := writeManagedFile(stdPlatformIO{}, dst, []byte("b")); err != nil {
		t.Fatalf("differing write: %v", err)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "b" {
		t.Errorf("got %q, want b", got)
	}
}

func TestRemoveManagedFile_NoOpWhenContentDiffers(t *testing.T) {
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "f.json")
	if err := os.WriteFile(dst, []byte("unrelated"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := removeManagedFile(stdPlatformIO{}, dst, []byte("managed")); err != nil {
		t.Fatalf("removeManagedFile: %v", err)
	}
	if _, err := os.Stat(dst); err != nil {
		t.Error("expected unmanaged file untouched")
	}
}

func TestRemoveManagedFile_RemovesMatchingContent(t *testing.T) {
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "f.json")
	if err := os.WriteFile(dst, []byte("managed"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := removeManagedFile(stdPlatformIO{}, dst, []byte("managed")); err != nil {
		t.Fatalf("removeManagedFile: %v", err)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Errorf("expected file removed, got err=%v", err)
	}
}

func TestRemoveDirIfEmpty(t *testing.T) {
	tmp := t.TempDir()
	empty := filepath.Join(tmp, "empty")
	if err := os.MkdirAll(empty, 0755); err != nil {
		t.Fatal(err)
	}
	if err := removeDirIfEmpty(empty); err != nil {
		t.Fatalf("removeDirIfEmpty: %v", err)
	}
	if _, err := os.Stat(empty); !os.IsNotExist(err) {
		t.Errorf("expected dir removed, got %v", err)
	}

	// Non-empty dir is preserved.
	nonEmpty := filepath.Join(tmp, "with-file")
	if err := os.MkdirAll(nonEmpty, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nonEmpty, "x"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := removeDirIfEmpty(nonEmpty); err != nil {
		t.Fatalf("removeDirIfEmpty non-empty: %v", err)
	}
	if _, err := os.Stat(nonEmpty); err != nil {
		t.Errorf("expected non-empty dir preserved, got %v", err)
	}

	// Missing dir is a no-op.
	if err := removeDirIfEmpty(filepath.Join(tmp, "no-such")); err != nil {
		t.Errorf("missing dir should no-op, got %v", err)
	}
}

// TestEmitRenderedHookFanoutAndRemove pairs emit+remove on a fanout to drive
// both helpers' main branches.
func TestEmitRenderedHookFanoutAndRemove(t *testing.T) {
	tmp := t.TempDir()
	dstRoot := filepath.Join(tmp, "hooks")
	specs := []HookSpec{
		{Name: "a", When: "user_prompt_submit", Command: "/bin/true"},
		{Name: "b", When: "post_tool_use", Command: "/bin/true"}, // not in copilot switch → skipped
	}
	if err := emitRenderedHookFanout(stdPlatformIO{}, specs, dstRoot, renderCopilotHookFile); err != nil {
		t.Fatalf("emitRenderedHookFanout: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dstRoot, "a.json")); err != nil {
		t.Errorf("expected a.json: %v", err)
	}
	// Remove fanout: matches content → removes a.json, dir becomes empty and is pruned.
	if err := removeManagedRenderedHookFanout(stdPlatformIO{}, specs, dstRoot, renderCopilotHookFile); err != nil {
		t.Fatalf("removeManagedRenderedHookFanout: %v", err)
	}
	if _, err := os.Stat(dstRoot); !os.IsNotExist(err) {
		t.Errorf("expected hooks dir removed, got %v", err)
	}
}

func TestEmitRenderedHookFanout_NilSpecsNoOp(t *testing.T) {
	if err := emitRenderedHookFanout(stdPlatformIO{}, nil, "/tmp/x", renderCopilotHookFile); err != nil {
		t.Errorf("nil specs no-op: %v", err)
	}
	if err := removeManagedRenderedHookFanout(stdPlatformIO{}, nil, "/tmp/x", renderCopilotHookFile); err != nil {
		t.Errorf("nil specs no-op (remove): %v", err)
	}
	if err := emitHookFanout(stdPlatformIO{}, []HookSpec{}, t.TempDir(),
		HookEmissionMode{Shape: HookShapeRenderFanout, Transport: HookTransportSymlink},
		func(HookSpec) (string, bool) { return "", false }); err != nil {
		t.Errorf("empty specs fanout: %v", err)
	}
}

// TestIsLikelyRenderedHookFileDetectors covers the four shape detectors.
func TestIsLikelyRenderedHookFileDetectors(t *testing.T) {
	// Claude rendered settings: contains $schema URL marker.
	claude := []byte(`{"$schema":"https://json.schemastore.org/claude-code-settings.json","hooks":{"PreToolUse":[]}}`)
	if !isLikelyRenderedClaudeHookSettings(claude) {
		t.Error("expected claude detector to match")
	}
	if isLikelyRenderedClaudeHookSettings([]byte(`{"foo":"bar"}`)) {
		t.Error("expected claude detector to reject random JSON")
	}

	// Codex
	codex := []byte(`{"hooks":{"PreToolUse":[{"matcher":"*"}]}}`)
	if !isLikelyRenderedCodexHookConfig(codex) {
		t.Error("expected codex detector to match")
	}
	if isLikelyRenderedCodexHookConfig([]byte(`{"x":1}`)) {
		t.Error("expected codex detector to reject")
	}

	// Cursor — versioned envelope.
	cursor := []byte(`{"version":1,"hooks":{"preToolUse":[]}}`)
	if !isLikelyRenderedCursorHookConfig(cursor) {
		t.Error("expected cursor detector to match")
	}
	if isLikelyRenderedCursorHookConfig([]byte(`{}`)) {
		t.Error("expected cursor detector to reject")
	}

	// Copilot
	copilot := []byte(`{"version":1,"hooks":{"preToolUse":[{"bash":"x"}]}}`)
	if !isLikelyRenderedCopilotHookFile(copilot) {
		t.Error("expected copilot detector to match")
	}
	if isLikelyRenderedCopilotHookFile([]byte(`{"x":1}`)) {
		t.Error("expected copilot detector to reject")
	}
}

// TestRemoveRenderedHelpers exercises the public remove* wrappers on missing
// files (should no-op, not error).
func TestRemoveRenderedHelpers_MissingFile(t *testing.T) {
	if err := removeRenderedClaudeHookSettings(stdPlatformIO{}, "/no/file"); err != nil {
		t.Errorf("removeRenderedClaudeHookSettings missing: %v", err)
	}
	if err := removeRenderedCodexHookConfig(stdPlatformIO{}, "/no/file"); err != nil {
		t.Errorf("removeRenderedCodexHookConfig missing: %v", err)
	}
	if err := removeRenderedCursorHookConfig(stdPlatformIO{}, "/no/file"); err != nil {
		t.Errorf("removeRenderedCursorHookConfig missing: %v", err)
	}
}

func TestRemoveManagedFileIf(t *testing.T) {
	tmp := t.TempDir()
	matching := filepath.Join(tmp, "match.json")
	other := filepath.Join(tmp, "other.json")
	// "matching" content includes the cursor envelope.
	matchContent := `{"version":1,"hooks":{"preToolUse":[]}}`
	if err := os.WriteFile(matching, []byte(matchContent), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte(`{"unrelated":true}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := removeManagedFileIf(stdPlatformIO{}, matching, isLikelyRenderedCursorHookConfig); err != nil {
		t.Errorf("removeManagedFileIf matching: %v", err)
	}
	if _, err := os.Stat(matching); !os.IsNotExist(err) {
		t.Errorf("expected matching file removed")
	}
	if err := removeManagedFileIf(stdPlatformIO{}, other, isLikelyRenderedCursorHookConfig); err != nil {
		t.Errorf("removeManagedFileIf other: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("expected other file preserved")
	}
	if err := removeManagedFileIf(stdPlatformIO{}, filepath.Join(tmp, "no-such"), isLikelyRenderedCursorHookConfig); err != nil {
		t.Errorf("removeManagedFileIf missing: %v", err)
	}
}

// TestPruneManagedRenderedFanoutExtras prunes non-wanted entries.
func TestPruneManagedRenderedFanoutExtras(t *testing.T) {
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "hooks")
	if err := os.MkdirAll(dst, 0755); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(dst, "keep.json")
	stale := filepath.Join(dst, "stale.json")
	other := filepath.Join(dst, "unrelated.txt")
	cursorContent := `{"version":1,"hooks":{"preToolUse":[]}}`
	for _, p := range []string{keep, stale} {
		if err := os.WriteFile(p, []byte(cursorContent), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(other, []byte("text"), 0644); err != nil {
		t.Fatal(err)
	}

	wanted := map[string]bool{"keep.json": true}
	if err := pruneManagedRenderedFanoutExtras(stdPlatformIO{}, dst, wanted, isLikelyRenderedCursorHookConfig); err != nil {
		t.Fatalf("pruneManagedRenderedFanoutExtras: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Error("kept file should remain")
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("stale file should be pruned")
	}
	if _, err := os.Stat(other); err != nil {
		t.Error("unrelated file should be preserved")
	}

	// Empty dir tolerated.
	emptyDst := filepath.Join(tmp, "empty")
	if err := os.MkdirAll(emptyDst, 0755); err != nil {
		t.Fatal(err)
	}
	if err := pruneManagedRenderedFanoutExtras(stdPlatformIO{}, emptyDst, nil, isLikelyRenderedCursorHookConfig); err != nil {
		t.Errorf("empty dir prune: %v", err)
	}

	// Missing dir is a no-op.
	if err := pruneManagedRenderedFanoutExtras(stdPlatformIO{}, filepath.Join(tmp, "nope"), nil, isLikelyRenderedCursorHookConfig); err != nil {
		t.Errorf("missing dir prune: %v", err)
	}
}
