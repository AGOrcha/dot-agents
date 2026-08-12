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
type hookRoundTripExpected struct {
	bundleName string
	when       string
	command    string
	tools      []string // matcher list after normalization; nil means do not assert
}

type hookRoundTripVariant struct {
	name        string
	rel         string
	sourceJSON  string
	expectation hookRoundTripExpected
}

func hookRoundTripVariants() []hookRoundTripVariant {
	return []hookRoundTripVariant{
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
			expectation: hookRoundTripExpected{
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
			expectation: hookRoundTripExpected{
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
			expectation: hookRoundTripExpected{
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
}

// writeHookSource writes JSON source for a hook variant into a fresh temp
// directory and returns the source root + absolute source path.
func writeHookSource(t *testing.T, rel, body string) (sourceRoot, sourcePath string) {
	t.Helper()
	sourceRoot = t.TempDir()
	sourcePath = filepath.Join(sourceRoot, rel)
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(sourcePath, []byte(body), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	return sourceRoot, sourcePath
}

// runCanonicalImportSingle drives canonicalImportOutputs and asserts exactly
// one output was produced. Returns the single output for further inspection.
func runCanonicalImportSingle(t *testing.T, sourceRoot, sourcePath string) importOutput {
	t.Helper()
	outputs, ok, err := canonicalImportOutputs(importCandidate{
		project:    canonicalImportProject,
		sourceRoot: sourceRoot,
		sourcePath: sourcePath,
	}, "")
	if err != nil {
		t.Fatalf("canonicalImportOutputs: %v", err)
	}
	if !ok || len(outputs) != 1 {
		t.Fatalf("expected one canonical output, ok=%v len=%d", ok, len(outputs))
	}
	return outputs[0]
}

func unmarshalHookManifest(t *testing.T, content []byte) map[string]any {
	t.Helper()
	var manifest map[string]any
	if err := yaml.Unmarshal(content, &manifest); err != nil {
		t.Fatalf("yaml.Unmarshal: %v\n%s", err, string(content))
	}
	return manifest
}

func assertHookManifestTools(t *testing.T, manifest map[string]any, want []string) {
	t.Helper()
	match, ok := manifest["match"].(map[string]any)
	if !ok {
		t.Fatalf("match section missing in manifest: %#v", manifest)
	}
	toolsAny, ok := match["tools"].([]any)
	if !ok {
		t.Fatalf("match.tools missing or wrong type: %#v", match["tools"])
	}
	if len(toolsAny) != len(want) {
		t.Fatalf("match.tools length = %d, want %d (%v)", len(toolsAny), len(want), toolsAny)
	}
	for i, w := range want {
		if toolsAny[i] != w {
			t.Fatalf("match.tools[%d] = %#v, want %q", i, toolsAny[i], w)
		}
	}
}

func runHookRoundTripVariant(t *testing.T, v hookRoundTripVariant) {
	sourceRoot, sourcePath := writeHookSource(t, v.rel, v.sourceJSON)
	out := runCanonicalImportSingle(t, sourceRoot, sourcePath)

	wantDest := "hooks/" + canonicalImportProject + "/" + v.expectation.bundleName + "/HOOK.yaml"
	if out.destRel != wantDest {
		t.Fatalf("destRel = %q, want %q", out.destRel, wantDest)
	}

	manifest := unmarshalHookManifest(t, out.content)
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
		assertHookManifestTools(t, manifest, v.expectation.tools)
	}
}

func TestHookNormalizationRoundTrip(t *testing.T) {
	for _, v := range hookRoundTripVariants() {
		v := v
		t.Run(v.name, func(t *testing.T) {
			runHookRoundTripVariant(t, v)
		})
	}
}

// TestHookNormalizationRoundTrip_Copilot exercises the .github/hooks/<file>
// shape, which derives the bundle name from the source filename instead of
// the event+command/matcher combination used by the other variants.
func TestHookNormalizationRoundTrip_Copilot(t *testing.T) {
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
	sourceRoot, sourcePath := writeHookSource(t, filepath.Join(relGitHubHooksDir, "prompt-log.json"), body)
	out := runCanonicalImportSingle(t, sourceRoot, sourcePath)

	wantDest := "hooks/" + canonicalImportProject + "/prompt-log/HOOK.yaml"
	if out.destRel != wantDest {
		t.Fatalf("destRel = %q, want %q", out.destRel, wantDest)
	}

	manifest := unmarshalHookManifest(t, out.content)
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
	sourceRoot, sourcePath := writeHookSource(t, relClaudeSettingsLocal, body)
	out := runCanonicalImportSingle(t, sourceRoot, sourcePath)

	manifest := unmarshalHookManifest(t, out.content)
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
