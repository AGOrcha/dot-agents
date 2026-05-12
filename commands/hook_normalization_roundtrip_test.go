package commands

import (
	"os"
	"path/filepath"
	"testing"

	"go.yaml.in/yaml/v3"
)

// TestHookNormalizationRoundTrip drives the canonical import flow for each
// supported platform hook shape (cursor.json, codex.json, claude
// settings.local.json, copilot .github/hooks/*.json) through
// canonicalImportOutputs and asserts the resulting HOOK.yaml encodes the
// input semantics: event mapping, matcher list, command text, and bundle
// name. Whitespace and ordering inside the HOOK.yaml are tolerated by
// unmarshaling to a generic map before comparison.
func TestHookNormalizationRoundTrip(t *testing.T) {
	type expected struct {
		bundleName string
		when       string
		command    string
		tools      []string // matcher list after normalization; nil means do not assert
	}

	type variant struct {
		name        string
		rel         string
		sourceJSON  string
		expectation expected
	}

	variants := []variant{
		{
			name: "cursor_pre_tool_use_bash",
			rel:  relCursorHooksJSON,
			sourceJSON: `{
  "version": 1,
  "hooks": {
    "preToolUse": [
      {
        "command": "./cursor-guard.sh",
        "matcher": "Bash"
      }
    ]
  }
}`,
			expectation: expected{
				// When the command stem is distinct (not a generic name like
				// "run"), the canonicalizer drops the matcher-derived suffix
				// from the bundle name; the matcher is still recorded in the
				// HOOK.yaml `match.tools` field.
				bundleName: "pre-tool-use-cursor-guard",
				when:       "pre_tool_use",
				command:    "./cursor-guard.sh",
				tools:      []string{"Bash"},
			},
		},
		{
			name: "codex_session_start_star_matcher",
			rel:  relCodexHooksJSON,
			sourceJSON: `{
  "hooks": {
    "SessionStart": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "./codex-banner.sh"
          }
        ]
      }
    ]
  }
}`,
			expectation: expected{
				bundleName: "session-start-codex-banner",
				when:       "session_start",
				command:    "./codex-banner.sh",
				tools:      nil, // "*" produces no canonical tools list
			},
		},
		{
			name: "claude_pre_tool_use_write_edit",
			rel:  relClaudeSettingsLocal,
			sourceJSON: `{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Write|Edit",
        "hooks": [
          {
            "type": "command",
            "command": "./claude-guard.sh"
          }
        ]
      }
    ]
  }
}`,
			expectation: expected{
				// Same naming rule as the cursor variant — the distinct
				// command stem suppresses the matcher suffix; tools are still
				// recorded under `match.tools`.
				bundleName: "pre-tool-use-claude-guard",
				when:       "pre_tool_use",
				command:    "./claude-guard.sh",
				tools:      []string{"Write", "Edit"},
			},
		},
	}

	for _, v := range variants {
		t.Run(v.name, func(t *testing.T) {
			sourceRoot := t.TempDir()
			sourcePath := filepath.Join(sourceRoot, v.rel)
			if err := os.MkdirAll(filepath.Dir(sourcePath), 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			if err := os.WriteFile(sourcePath, []byte(v.sourceJSON), 0o644); err != nil {
				t.Fatalf("write source: %v", err)
			}

			outputs, ok, err := canonicalImportOutputs(importCandidate{
				project:    canonicalImportProject,
				sourceRoot: sourceRoot,
				sourcePath: sourcePath,
			})
			if err != nil {
				t.Fatalf("canonicalImportOutputs: %v", err)
			}
			if !ok || len(outputs) != 1 {
				t.Fatalf("expected one canonical output, ok=%v len=%d", ok, len(outputs))
			}

			wantDest := "hooks/" + canonicalImportProject + "/" + v.expectation.bundleName + "/HOOK.yaml"
			if outputs[0].destRel != wantDest {
				t.Fatalf("destRel = %q, want %q", outputs[0].destRel, wantDest)
			}

			var manifest map[string]any
			if err := yaml.Unmarshal(outputs[0].content, &manifest); err != nil {
				t.Fatalf("yaml.Unmarshal: %v\n%s", err, string(outputs[0].content))
			}

			if got := manifest["when"]; got != v.expectation.when {
				t.Fatalf("when = %#v, want %q", got, v.expectation.when)
			}
			if got := manifest["name"]; got != v.expectation.bundleName {
				t.Fatalf("name = %#v, want %q", got, v.expectation.bundleName)
			}

			run, ok := manifest["run"].(map[string]any)
			if !ok {
				t.Fatalf("run section missing in manifest: %#v", manifest)
			}
			if got := run["command"]; got != v.expectation.command {
				t.Fatalf("run.command = %#v, want %q", got, v.expectation.command)
			}

			if v.expectation.tools != nil {
				match, ok := manifest["match"].(map[string]any)
				if !ok {
					t.Fatalf("match section missing in manifest: %#v", manifest)
				}
				toolsAny, ok := match["tools"].([]any)
				if !ok {
					t.Fatalf("match.tools missing or wrong type: %#v", match["tools"])
				}
				if len(toolsAny) != len(v.expectation.tools) {
					t.Fatalf("match.tools length = %d, want %d (%v)", len(toolsAny), len(v.expectation.tools), toolsAny)
				}
				for i, want := range v.expectation.tools {
					if toolsAny[i] != want {
						t.Fatalf("match.tools[%d] = %#v, want %q", i, toolsAny[i], want)
					}
				}
			}
		})
	}
}

// TestHookNormalizationRoundTrip_Copilot exercises the .github/hooks/<file>
// shape, which derives the bundle name from the source filename instead of
// the event+command/matcher combination used by the other variants.
func TestHookNormalizationRoundTrip_Copilot(t *testing.T) {
	sourceRoot := t.TempDir()
	sourcePath := filepath.Join(sourceRoot, relGitHubHooksDir, "prompt-log.json")
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := `{
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
}`
	if err := os.WriteFile(sourcePath, []byte(body), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	outputs, ok, err := canonicalImportOutputs(importCandidate{
		project:    canonicalImportProject,
		sourceRoot: sourceRoot,
		sourcePath: sourcePath,
	})
	if err != nil {
		t.Fatalf("canonicalImportOutputs: %v", err)
	}
	if !ok || len(outputs) != 1 {
		t.Fatalf("expected one canonical output, ok=%v len=%d", ok, len(outputs))
	}

	wantDest := "hooks/" + canonicalImportProject + "/prompt-log/HOOK.yaml"
	if outputs[0].destRel != wantDest {
		t.Fatalf("destRel = %q, want %q", outputs[0].destRel, wantDest)
	}

	var manifest map[string]any
	if err := yaml.Unmarshal(outputs[0].content, &manifest); err != nil {
		t.Fatalf("yaml.Unmarshal: %v\n%s", err, string(outputs[0].content))
	}
	if got := manifest["when"]; got != "user_prompt_submit" {
		t.Fatalf("when = %#v, want user_prompt_submit", got)
	}
	if got := manifest["name"]; got != "prompt-log" {
		t.Fatalf("name = %#v, want prompt-log", got)
	}
	run, ok := manifest["run"].(map[string]any)
	if !ok {
		t.Fatalf("run section missing in manifest: %#v", manifest)
	}
	if got := run["command"]; got != "./prompt-log.sh" {
		t.Fatalf("run.command = %#v, want ./prompt-log.sh", got)
	}
	if got := run["timeout_ms"]; got != 5000 {
		t.Fatalf("run.timeout_ms = %#v, want 5000", got)
	}
}

// TestHookNormalizationRoundTrip_PreservesExpressionForNonCanonicalMatcher
// ensures that matchers containing characters outside the canonical tool set
// (e.g. whitespace around the pipe) are preserved as a raw expression in
// addition to the parsed tools list — the round-trip must not silently drop
// non-canonical input semantics.
func TestHookNormalizationRoundTrip_PreservesExpressionForNonCanonicalMatcher(t *testing.T) {
	sourceRoot := t.TempDir()
	sourcePath := filepath.Join(sourceRoot, relClaudeSettingsLocal)
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := `{
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
}`
	if err := os.WriteFile(sourcePath, []byte(body), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	outputs, ok, err := canonicalImportOutputs(importCandidate{
		project:    canonicalImportProject,
		sourceRoot: sourceRoot,
		sourcePath: sourcePath,
	})
	if err != nil {
		t.Fatalf("canonicalImportOutputs: %v", err)
	}
	if !ok || len(outputs) != 1 {
		t.Fatalf("expected one canonical output, ok=%v len=%d", ok, len(outputs))
	}

	var manifest map[string]any
	if err := yaml.Unmarshal(outputs[0].content, &manifest); err != nil {
		t.Fatalf("yaml.Unmarshal: %v\n%s", err, string(outputs[0].content))
	}
	match, ok := manifest["match"].(map[string]any)
	if !ok {
		t.Fatalf("match section missing: %#v", manifest)
	}
	tools, ok := match["tools"].([]any)
	if !ok || len(tools) != 2 || tools[0] != "Write" || tools[1] != "Edit" {
		t.Fatalf("match.tools = %#v, want [Write Edit]", match["tools"])
	}
	if got := match["expression"]; got != "Write | Edit" {
		t.Fatalf("match.expression = %#v, want %q", got, "Write | Edit")
	}
}
