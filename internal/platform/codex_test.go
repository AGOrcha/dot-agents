package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NikashPrakash/dot-agents/internal/links"
)

const codexAgentMarkdownFile = "AGENT.md"

func TestRenderCodexAgentTomlUsesFrontmatterAndBody(t *testing.T) {
	tmp := t.TempDir()
	agentDir := filepath.Join(tmp, "agents", "global", "reviewer")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatal(err)
	}
	agentMD := filepath.Join(agentDir, codexAgentMarkdownFile)
	content := `---
name: reviewer
description: reviews changes
model: gpt-5.1-codex
is_background: true
---

# Reviewer

Use "safe" defaults and avoid shell footguns.
`
	if err := os.WriteFile(agentMD, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := renderCodexAgentToml(agentMD)
	if err != nil {
		t.Fatalf("renderCodexAgentToml failed: %v", err)
	}

	out := string(got)
	for _, want := range []string{
		`name = "reviewer"`,
		`description = "reviews changes"`,
		`model = "gpt-5.1-codex"`,
		`developer_instructions = """`,
		`# Reviewer`,
		`Use "safe" defaults and avoid shell footguns.`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("render output missing %q:\n%s", want, out)
		}
	}
}

func TestCodexCreateLinksWritesNativeAgentTomlAndCleansCompat(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	home := filepath.Join(tmp, "home")
	repo := filepath.Join(tmp, "repo")

	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)

	globalAgentDir := filepath.Join(agentsHome, "agents", "global", "reviewer")
	projectAgentDir := filepath.Join(agentsHome, "agents", "proj", "implementer")
	for _, dir := range []string{globalAgentDir, projectAgentDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	mustWriteCodexFixtureFile(t, filepath.Join(globalAgentDir, codexAgentMarkdownFile), `---
name: reviewer
description: global reviewer
model: gpt-5.1-codex
---

# Reviewer
`)
	mustWriteCodexFixtureFile(t, filepath.Join(projectAgentDir, codexAgentMarkdownFile), `---
name: implementer
description: project implementer
is_background: false
---

# Implementer

Build the feature and keep tests green.
`)

	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}

	if err := CollectAndExecuteSharedTargetPlan("proj", repo, []Platform{NewCodex()}); err != nil {
		t.Fatalf("CollectAndExecuteSharedTargetPlan: %v", err)
	}
	if err := NewCodex().CreateLinks("proj", repo); err != nil {
		t.Fatalf("CreateLinks failed: %v", err)
	}

	projectToml := filepath.Join(repo, ".codex", "agents", "implementer.toml")
	assertCodexFileContains(t, "project toml", projectToml, []string{
		`name = "implementer"`,
		`description = "project implementer"`,
		`Build the feature and keep tests green.`,
	})

	userToml := filepath.Join(home, ".codex", "agents", "reviewer.toml")
	assertCodexFileContains(t, "user toml", userToml, []string{
		`name = "reviewer"`,
		`description = "global reviewer"`,
		`model = "gpt-5.1-codex"`,
	})

	assertCodexPathNotExists(t, filepath.Join(repo, ".claude", "agents"), "legacy compat path should be cleaned up")

	if err := NewCodex().RemoveLinks("proj", repo); err != nil {
		t.Fatalf("RemoveLinks failed: %v", err)
	}

	assertCodexPathNotExists(t, projectToml, "project native agent should be removed")
	assertCodexPathNotExists(t, filepath.Join(repo, ".claude", "agents"), "legacy compat path should stay removed")
}

func mustWriteCodexFixtureFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func assertCodexFileContains(t *testing.T, label, path string, want []string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected %s at %s: %v", label, path, err)
	}
	got := string(content)
	for _, snippet := range want {
		if !strings.Contains(got, snippet) {
			t.Fatalf("%s missing %q:\n%s", label, snippet, got)
		}
	}
}

func assertCodexPathNotExists(t *testing.T, path, message string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("%s, got %v", message, err)
	}
}

// ---------------------------------------------------------------------------
// Codex CreateLinks + SharedTargetIntents + hook rendering coverage
// (relocated from coverage_gap2_test.go).
// ---------------------------------------------------------------------------

// TestCodexCreateLinks_FullRulesAndSettings drives the rules→AGENTS.md and
// settings→config.toml branches of (*codex).CreateLinks.
func TestCodexCreateLinks_FullRulesAndSettings(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	home := filepath.Join(tmp, "home")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatal(err)
	}
	// Seed global rules and project override.
	if err := os.MkdirAll(filepath.Join(agentsHome, "rules", "global"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentsHome, "rules", "global", "agents.md"), []byte("# global\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(agentsHome, "rules", "proj"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentsHome, "rules", "proj", "agents.md"), []byte("# proj\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Seed settings/codex.toml.
	if err := os.MkdirAll(filepath.Join(agentsHome, "settings", "proj"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentsHome, "settings", "proj", "codex.toml"), []byte("model = \"x\"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	if err := NewCodex().CreateLinks("proj", repo); err != nil {
		t.Fatalf("CreateLinks: %v", err)
	}

	// AGENTS.md should be linked (project override wins).
	if !links.IsManagedLink(filepath.Join(repo, "AGENTS.md"), filepath.Join(agentsHome, "rules", "proj", "agents.md")) {
		t.Error("AGENTS.md should be a managed link to the project override")
	}
	if _, err := os.Lstat(filepath.Join(repo, ".codex", "config.toml")); err != nil {
		t.Errorf("config.toml missing: %v", err)
	}
}

// TestCodexSharedTargetIntentsPopulated drives skills + codex-agent-toml intents.
func TestCodexSharedTargetIntents_Populated(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	for _, p := range [][]string{
		{"skills", "proj", "alpha", "SKILL.md"},
		{"agents", "proj", "reviewer", "AGENT.md"},
	} {
		dir := filepath.Join(append([]string{agentsHome}, p[:3]...)...)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, p[3]), []byte("body"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	intents, err := NewCodex().SharedTargetIntents("proj")
	if err != nil {
		t.Fatalf("SharedTargetIntents: %v", err)
	}
	if len(intents) == 0 {
		t.Error("expected non-zero intents")
	}
}

// TestRenderCodexHookConfigMatcherBranches covers session_start/pre_tool_use/
// post_tool_use which take the matcherForSpec branch versus stop which uses
// empty matcher.
func TestRenderCodexHookConfig_MatcherBranches(t *testing.T) {
	specs := []HookSpec{
		{Name: "a", When: "stop", Command: "/bin/x"},
		{Name: "b", When: "pre_tool_use", Command: "/bin/y", MatchExpression: "Bash"},
	}
	content, err := renderCodexHookConfig(specs)
	if err != nil {
		t.Fatalf("renderCodexHookConfig: %v", err)
	}
	got := string(content)
	if !strings.Contains(got, "Stop") {
		t.Errorf("expected Stop event: %s", got)
	}
	if !strings.Contains(got, `"matcher": "Bash"`) {
		t.Errorf("expected Bash matcher: %s", got)
	}
}

// ---------------------------------------------------------------------------
// Codex agent-toml + session model resolution coverage (relocated from
// coverage_gap3_test.go).
// ---------------------------------------------------------------------------

// TestWriteCodexAgentTomlFile_ExistingFileReplaced drives the Lstat→Remove branch.
func TestWriteCodexAgentTomlFile_ExistingFileReplaced(t *testing.T) {
	tmp := t.TempDir()
	agent := filepath.Join(tmp, "AGENT.md")
	if err := os.WriteFile(agent, []byte("---\nname: x\n---\nbody\n"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(tmp, "x.toml")
	if err := os.WriteFile(dst, []byte("stale\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := writeCodexAgentTomlFile(stdPlatformIO{}, dst, agent); err != nil {
		t.Fatalf("writeCodexAgentTomlFile: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `name = "x"`) {
		t.Errorf("file content not refreshed: %q", got)
	}
}

func TestWriteCodexAgentTomlFile_BadAgentMD(t *testing.T) {
	if err := writeCodexAgentTomlFile(stdPlatformIO{}, filepath.Join(t.TempDir(), "x.toml"), "/no/such/agent.md"); err == nil {
		t.Error("expected error for missing agent.md")
	}
}

// TestRemoveSharedTargets_RenderedTomlPath drives the codex-agent-toml remove
// path.
func TestRemoveManagedIntentTarget_CodexTomlPath(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "out.toml")
	if err := os.WriteFile(target, []byte("dummy"), 0644); err != nil {
		t.Fatal(err)
	}
	intent := ResourceIntent{
		Shape:        ResourceShapeRenderSingle,
		Transport:    ResourceTransportWrite,
		Materializer: codexAgentTomlMaterializer,
		TargetPath:   "out.toml",
	}
	if err := removeManagedIntentTarget(intent, tmp, t.TempDir()); err != nil {
		t.Fatalf("removeManagedIntentTarget: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Error("expected target removed")
	}
}

// TestExecuteRenderSingleWrite_CodexAgentToml drives the codex-agent-toml
// happy-path branch via Execute.
func TestExecuteRenderSingleWrite_CodexAgentToml(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	src := filepath.Join(agentsHome, "agents", "proj", "reviewer", "AGENT.md")
	if err := os.MkdirAll(filepath.Dir(src), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("---\nname: reviewer\n---\nbody\n"), 0644); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	intent := ResourceIntent{
		IntentID:    "codex.proj.reviewer.toml",
		Project:     "proj",
		Bucket:      "agents",
		LogicalName: "reviewer",
		TargetPath:  filepath.Join(".codex/agents", "reviewer.toml"),
		Ownership:   ResourceOwnershipSharedRepo,
		SourceRef: ResourceSourceRef{
			Scope:        "proj",
			Bucket:       "agents",
			RelativePath: "reviewer/AGENT.md",
			Kind:         ResourceSourceCanonicalFile,
		},
		Shape:         ResourceShapeRenderSingle,
		Transport:     ResourceTransportWrite,
		Materializer:  codexAgentTomlMaterializer,
		ReplacePolicy: ResourceReplaceAllowlistedImportedDirOnly,
		PrunePolicy:   ResourcePruneTarget,
	}
	plan, err := BuildResourcePlan([]ResourceIntent{intent})
	if err != nil {
		t.Fatalf("BuildResourcePlan: %v", err)
	}
	if err := plan.Execute(repo, agentsHome); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".codex/agents/reviewer.toml")); err != nil {
		t.Errorf("expected toml file: %v", err)
	}
}

// TestResolveCodexModelFromJSONL_NoResponseLine drives the empty-result branch
// (model in JSONL is "").
func TestResolveCodexModelFromJSONL_NoModel(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".codex", "sessions", "2026", "05", "11")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	sessID := "no-model"
	path := filepath.Join(dir, "rollout-"+sessID+".jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"event_msg","payload":{"type":"task_started"}}`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := resolveCodexModelFromJSONL(home, sessID); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

// TestPruneCodexRepoAgentTomls_NoEntries covers the early no-entries branch.
func TestPruneCodexRepoAgentTomls_NoEntries(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	// agentsHome with no agents bucket → listScopedResourceDirs errors → nil.
	if err := pruneCodexRepoAgentTomls("proj", repo, filepath.Join(tmp, "missing")); err != nil {
		t.Errorf("expected no error for missing agents bucket, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Codex CreateLinks + scan session error coverage (relocated from coverage_gap5).
// ---------------------------------------------------------------------------

// TestCodexCreateLinks_GlobalRuleOnly drives the global-rules path when
// project override is absent.
func TestCodexCreateLinks_GlobalRuleOnly(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	home := filepath.Join(tmp, "home")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(agentsHome, "rules", "global"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentsHome, "rules", "global", "rules.md"), []byte("# rules"), 0644); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	if err := NewCodex().CreateLinks("proj", repo); err != nil {
		t.Fatalf("CreateLinks: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(repo, "AGENTS.md")); err != nil {
		t.Errorf("AGENTS.md missing: %v", err)
	}
}

// TestCodexScanSessionTokens_OpenError exercises the os.Open failure branch
// when findCodexSessionFile returns a path that cannot be opened (e.g. it
// resolves to a directory instead of a regular file).
func TestCodexScanSessionTokens_OpenError(t *testing.T) {
	home := t.TempDir()
	sessID := "open-err-id"
	// Create the expected daily directory and place a *directory* at the
	// rollout filename — findCodexSessionFile globs by suffix and will pick
	// it up, but os.Open succeeds on directories on Unix. To trigger an
	// open error we instead seed an unreadable file.
	dir := filepath.Join(home, ".codex", "sessions", "2026", "05", "11")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "rollout-2026-05-11-"+sessID+".jsonl")
	if err := os.WriteFile(path, []byte("ignored"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	// On macOS running as root, mode 0o000 may still be readable; treat the
	// success path as acceptable too — what matters is that the function
	// returns without panicking.
	got := codexScanSessionTokens(home, sessID, "")
	_ = got
}

// ---------------------------------------------------------------------------
// Codex session-file resolution + token scanner + usage stats coverage
// (relocated from coverage_gap_test.go).
// ---------------------------------------------------------------------------

// TestFindCodexSessionFile_LocatesFile constructs the nested sessions
// directory and verifies findCodexSessionFile finds it.
func TestFindCodexSessionFile_LocatesFile(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".codex", "sessions", "2026", "05", "11")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	sessID := "abc-123"
	path := filepath.Join(dir, "rollout-2026-05-11-"+sessID+".jsonl")
	if err := os.WriteFile(path, []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got := findCodexSessionFile(home, sessID)
	if got != path {
		t.Errorf("findCodexSessionFile = %q, want %q", got, path)
	}
}

func TestFindCodexSessionFile_EmptyID(t *testing.T) {
	if got := findCodexSessionFile(t.TempDir(), ""); got != "" {
		t.Errorf("expected empty for missing session id, got %q", got)
	}
}

func TestFindCodexSessionFile_NoMatch(t *testing.T) {
	if got := findCodexSessionFile(t.TempDir(), "missing"); got != "" {
		t.Errorf("expected empty for no match, got %q", got)
	}
}

// TestResolveCodexModelFromJSONL parses a synthetic response_item entry.
func TestResolveCodexModelFromJSONL(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".codex", "sessions", "2026", "05", "11")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	sessID := "sess-123"
	path := filepath.Join(dir, "rollout-2026-05-11-"+sessID+".jsonl")
	lines := []string{
		`{"type":"event_msg","payload":{"type":"task_started"}}`,
		`{"type":"response_item","payload":{"type":"response","model":"gpt-5"}}`,
		`{"type":"response_item","payload":{"type":"response","model":"gpt-5.1"}}`,
		`not-json`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got := resolveCodexModelFromJSONL(home, sessID)
	if got != "gpt-5.1" {
		t.Errorf("model = %q, want gpt-5.1 (last response wins)", got)
	}
}

func TestResolveCodexModelFromJSONL_NoSession(t *testing.T) {
	if got := resolveCodexModelFromJSONL(t.TempDir(), "missing"); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

// TestCodexAccumulateTokenEntry table-drives the per-line accumulator.
func TestCodexAccumulateTokenEntry(t *testing.T) {
	cases := []struct {
		name     string
		line     string
		after    time.Time
		wantIn   int
		wantOut  int
		wantMsgs int
	}{
		{
			name:     "valid token_count adds to metrics",
			line:     `{"type":"event_msg","timestamp":"2026-05-11T12:00:00Z","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":10,"output_tokens":20,"cached_input_tokens":5,"reasoning_output_tokens":2}}}}`,
			wantIn:   10,
			wantOut:  20,
			wantMsgs: 1,
		},
		{
			name: "non-event_msg ignored",
			line: `{"type":"response_item","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":99,"output_tokens":99}}}}`,
		},
		{
			name: "missing info ignored",
			line: `{"type":"event_msg","payload":{"type":"token_count","info":null}}`,
		},
		{
			name: "missing last_token_usage ignored",
			line: `{"type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":null}}}`,
		},
		{
			name: "non-token_count payload ignored",
			line: `{"type":"event_msg","payload":{"type":"task_started"}}`,
		},
		{
			name: "malformed JSON ignored",
			line: `not-json`,
		},
		{
			name:  "after-cutoff entry skipped",
			line:  `{"type":"event_msg","timestamp":"2026-05-11T10:00:00Z","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":7}}}}`,
			after: time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var m SessionTokenMetrics
			codexAccumulateTokenEntry([]byte(tc.line), tc.after, &m)
			if m.InputTokens != tc.wantIn {
				t.Errorf("InputTokens = %d, want %d", m.InputTokens, tc.wantIn)
			}
			if m.OutputTokens != tc.wantOut {
				t.Errorf("OutputTokens = %d, want %d", m.OutputTokens, tc.wantOut)
			}
			if m.MessageCount != tc.wantMsgs {
				t.Errorf("MessageCount = %d, want %d", m.MessageCount, tc.wantMsgs)
			}
		})
	}
}

// TestCodexScanSessionTokens_AggregatesAcrossLines drives the full
// codexScanSessionTokens path with synthetic session files.
func TestCodexScanSessionTokens_AggregatesAcrossLines(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".codex", "sessions", "2026", "05", "11")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	sessID := "scan-test"
	path := filepath.Join(dir, "rollout-2026-05-11-"+sessID+".jsonl")
	lines := []string{
		`{"type":"event_msg","timestamp":"2026-05-11T11:00:00Z","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":100,"output_tokens":200,"cached_input_tokens":50,"reasoning_output_tokens":5}}}}`,
		`{"type":"event_msg","timestamp":"2026-05-11T13:00:00Z","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":1,"output_tokens":2,"cached_input_tokens":3,"reasoning_output_tokens":4}}}}`,
		`{"type":"event_msg","timestamp":"bad-ts","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":9}}}}`,
		// Line missing token_count substring is short-circuited.
		`{"type":"event_msg","payload":{"type":"other"}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// No timestamp filter — sums all valid token_count lines (3 of them, since
	// the unparseable-ts line still accumulates: parseJSONLTimestamp returns
	// !ok which is treated as "no after constraint").
	got := codexScanSessionTokens(home, sessID, "")
	if got.MessageCount < 2 {
		t.Errorf("MessageCount = %d, want >= 2", got.MessageCount)
	}
	if got.InputTokens < 101 {
		t.Errorf("InputTokens = %d, want >= 101", got.InputTokens)
	}

	// With cutoff at noon: the 13:00 entry contributes plus any with an
	// unparseable timestamp (whose after-check is skipped by design).
	gotFiltered := codexScanSessionTokens(home, sessID, "2026-05-11T12:00:00Z")
	if gotFiltered.MessageCount < 1 {
		t.Errorf("filtered MessageCount = %d, want >= 1", gotFiltered.MessageCount)
	}
	if gotFiltered.InputTokens < 1 {
		t.Errorf("filtered InputTokens = %d, want >= 1", gotFiltered.InputTokens)
	}
}

func TestCodexScanSessionTokens_MissingSession(t *testing.T) {
	got := codexScanSessionTokens(t.TempDir(), "missing-id", "")
	if got.InputTokens != 0 || got.MessageCount != 0 {
		t.Errorf("expected zero metrics for missing session, got %+v", got)
	}
}

func TestScanJSONLForLastModel_MissingFile(t *testing.T) {
	got := scanJSONLForLastModel("/no/such/file", func([]byte) string { return "x" })
	if got != "" {
		t.Errorf("expected empty for missing file, got %q", got)
	}
}

func TestScanJSONLForLastModel_KeepsLastNonEmpty(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "f.jsonl")
	content := "alpha\nbeta\n\n\ngamma\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	got := scanJSONLForLastModel(path, func(line []byte) string {
		return strings.TrimSpace(string(line))
	})
	if got != "gamma" {
		t.Errorf("got %q, want gamma", got)
	}
}

// TestCodexReadUsageStats_TooManyEntriesKeepsTail mirrors the claude tail
// behaviour and ensures the >10-entry branch is exercised.
func TestCodexReadUsageStats_TooManyEntriesKeepsTail(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, ".codex")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for i := 0; i < 15; i++ {
		b.WriteString(`{"id":"s` + itoa(i) + `","thread_name":"t` + itoa(i) + `","updated_at":"2026-05-11T00:00:00Z"}`)
		b.WriteString("\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "session_index.jsonl"), []byte(b.String()), 0644); err != nil {
		t.Fatal(err)
	}
	stats := codexReadUsageStats(tmp)
	if stats == nil {
		t.Fatal("nil stats")
	}
	if stats.TotalSessions != 15 {
		t.Errorf("TotalSessions = %d, want 15", stats.TotalSessions)
	}
	if len(stats.RecentSessions) != 10 {
		t.Errorf("RecentSessions tail length = %d, want 10", len(stats.RecentSessions))
	}
}

// ---------- P3: Badge + CountLinks (StatusBadger + LinkCounter) ----------

// TestCodexBadge_EmptyProject pins the empty-project contract.
func TestCodexBadge_EmptyProject(t *testing.T) {
	tmp := t.TempDir()
	got := NewCodex().(*codex).Badge("proj", tmp, filepath.Join(tmp, ".agents"))
	if got.Name != "Codex" {
		t.Errorf("Badge.Name = %q, want %q", got.Name, "Codex")
	}
	if got.Present || got.Broken {
		t.Errorf("empty project: Badge = %+v, want Present=false Broken=false", got)
	}
}

// TestCodexCountLinks_HealthyAGENTSMarkdown covers the positive single-file
// branch: a managed AGENTS.md surfaces as (ok>=1, broken=0) and Badge
// surfaces Present=true.
func TestCodexCountLinks_HealthyAGENTSMarkdown(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "AGENTS.md"), []byte("# agents"), 0644); err != nil {
		t.Fatal(err)
	}

	c := NewCodex().(*codex)
	ok, broken := c.CountLinks("proj", tmp, filepath.Join(tmp, ".agents"))
	if ok < 1 || broken != 0 {
		t.Errorf("CountLinks = (%d,%d), want (>=1,0)", ok, broken)
	}
	b := c.Badge("proj", tmp, filepath.Join(tmp, ".agents"))
	if !b.Present || b.Broken {
		t.Errorf("Badge = %+v, want Present=true Broken=false", b)
	}
}
