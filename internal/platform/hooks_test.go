package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveHookSpec_PrefersProjectHooksOverSettingsAndGlobal(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")

	projectHook := filepath.Join(agentsHome, "hooks", "proj", "claude-code.json")
	projectSettings := filepath.Join(agentsHome, "settings", "proj", "claude-code.json")
	globalHook := filepath.Join(agentsHome, "hooks", "global", "claude-code.json")

	writeTextFile(t, projectHook, "{\"source\":\"project-hook\"}\n")
	writeTextFile(t, projectSettings, "{\"source\":\"project-settings\"}\n")
	writeTextFile(t, globalHook, "{\"source\":\"global-hook\"}\n")

	spec := resolveHookSpec(agentsHome, []string{"hooks", "settings"}, "proj", "claude-code.json")
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

func TestEmitHookFanout_SymlinksSelectedHookFiles(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	dstRoot := filepath.Join(tmp, "repo", ".github", "hooks")

	preTool := filepath.Join(agentsHome, "hooks", "proj", "pre-tool.json")
	cursorHook := filepath.Join(agentsHome, "hooks", "proj", "cursor.json")
	writeTextFile(t, preTool, "{\"name\":\"pre-tool\"}\n")
	writeTextFile(t, cursorHook, "{\"name\":\"cursor\"}\n")

	specs, err := listHookSpecs(agentsHome, "proj")
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

func TestListHookSpecs_LoadsCanonicalBundleAndPreservesLegacyFiles(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")

	writeTextFile(t, filepath.Join(agentsHome, "hooks", "proj", "format-write", "HOOK.yaml"), `name: format-write
when: pre_tool_use
match:
  tools: [Write, Edit]
  expression: Write | Edit
run:
  command: ./run.sh
  timeout_ms: 15000
enabled_on: [claude, cursor]
`)
	writeTextFile(t, filepath.Join(agentsHome, "hooks", "proj", "format-write", "run.sh"), "#!/bin/sh\nexit 0\n")
	writeTextFile(t, filepath.Join(agentsHome, "hooks", "proj", "copilot-cli-policy.json"), "{\"version\":1}\n")

	specs, err := listHookSpecs(agentsHome, "proj")
	if err != nil {
		t.Fatalf("listHookSpecs failed: %v", err)
	}
	if len(specs) != 2 {
		t.Fatalf("expected 2 hook specs, got %d", len(specs))
	}

	if specs[0].Name != "copilot-cli-policy" || specs[0].SourceKind != HookSourceLegacyFile {
		t.Fatalf("expected first spec to be legacy copilot hook, got %#v", specs[0])
	}
	if specs[1].Name != "format-write" || specs[1].SourceKind != HookSourceCanonicalBundle {
		t.Fatalf("expected second spec to be canonical bundle, got %#v", specs[1])
	}
	if specs[1].When != "pre_tool_use" {
		t.Fatalf("expected canonical when=pre_tool_use, got %q", specs[1].When)
	}
	if specs[1].Command != "./run.sh" {
		t.Fatalf("expected canonical command ./run.sh, got %q", specs[1].Command)
	}
	if specs[1].MatchExpression != "Write | Edit" {
		t.Fatalf("expected canonical match expression %q, got %q", "Write | Edit", specs[1].MatchExpression)
	}
	if got, want := resolveHookCommand(specs[1]), filepath.Join(agentsHome, "hooks", "proj", "format-write", "run.sh"); got != want {
		t.Fatalf("resolved command = %q, want %q", got, want)
	}
}

func TestRenderClaudeHookSettings_PrefersCanonicalMatchExpression(t *testing.T) {
	specs := []HookSpec{{
		Name:            "format-write",
		When:            "pre_tool_use",
		MatchTools:      []string{"Write", "Edit"},
		MatchExpression: "Write | Edit",
		Command:         "/tmp/run.sh",
	}}

	content, err := renderClaudeHookSettings(specs)
	if err != nil {
		t.Fatalf("renderClaudeHookSettings failed: %v", err)
	}
	if got := string(content); !strings.Contains(got, `"matcher": "Write | Edit"`) {
		t.Fatalf("expected rendered matcher to use canonical expression, got:\n%s", got)
	}
}

func TestRenderCursorHookConfig_PrefersCanonicalMatchExpression(t *testing.T) {
	specs := []HookSpec{{
		Name:            "format-write",
		When:            "pre_tool_use",
		MatchTools:      []string{"Write", "Edit"},
		MatchExpression: "Write | Edit",
		Command:         "/tmp/run.sh",
	}}

	content, err := renderCursorHookConfig(specs)
	if err != nil {
		t.Fatalf("renderCursorHookConfig failed: %v", err)
	}
	if got := string(content); !strings.Contains(got, `"matcher": "Write | Edit"`) {
		t.Fatalf("expected rendered matcher to use canonical expression, got:\n%s", got)
	}
}

func TestRenderCopilotHookFile_SkipsWhenCanonicalMatchExpressionPresent(t *testing.T) {
	_, _, ok, err := renderCopilotHookFile(HookSpec{
		Name:            "prompt-log",
		When:            "user_prompt_submit",
		MatchExpression: "Write | Edit",
		Command:         "/tmp/run.sh",
	})
	if err != nil {
		t.Fatalf("renderCopilotHookFile returned error: %v", err)
	}
	if ok {
		t.Fatalf("expected copilot hook render to skip matcher-constrained hook")
	}
}
