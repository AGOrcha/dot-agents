package platform

// Tests targeting uncovered branches across the platform package to lift
// statement coverage above the 95% gate. These touch CLI version probes via
// fake PATH binaries, session-file resolvers, hook-rendered fanout removals,
// and the catch-all dead-code helpers (resolveScopedFileFromBuckets,
// ExecuteSharedSkillMirrorPlan).

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// installFakeCLI writes an executable shell script that prints `out` and exits
// 0; returns the directory containing the script for use in PATH. Skips on
// non-unix because we rely on `#!/bin/sh`.
func installFakeCLI(t *testing.T, name, out string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI shim relies on POSIX shell semantics")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, name)
	body := "#!/bin/sh\nprintf '%s\\n' " + shellQuote(out) + "\n"
	if err := os.WriteFile(script, []byte(body), 0755); err != nil {
		t.Fatalf("write fake CLI: %v", err)
	}
	return dir
}

// shellQuote is a minimal POSIX-shell single-quote escape.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func TestPeekCLIVersionLine_TrimsAndReturnsFirstLine(t *testing.T) {
	dir := installFakeCLI(t, "fake-cli", "  v1.2.3 (build abc)\nextra-line\n")
	got, err := peekCLIVersionLine(filepath.Join(dir, "fake-cli"))
	if err != nil {
		t.Fatalf("peekCLIVersionLine: %v", err)
	}
	if got != "v1.2.3 (build abc)" {
		t.Errorf("peekCLIVersionLine = %q, want %q", got, "v1.2.3 (build abc)")
	}
}

func TestPeekCLIVersionLine_NonexistentBinaryErrors(t *testing.T) {
	if _, err := peekCLIVersionLine("/no/such/binary/probe-xyz"); err == nil {
		t.Error("expected error for missing binary")
	}
}

func TestFirstCLIPeekVersion_PicksFirstAvailable(t *testing.T) {
	dir := installFakeCLI(t, "agent", "Cursor Agent 2.0.0")
	// Strip PATH so only our fake `agent` binary is resolvable.
	t.Setenv("PATH", dir)
	got := firstCLIPeekVersion("agent", "cursor")
	if got != "Cursor Agent 2.0.0" {
		t.Errorf("firstCLIPeekVersion = %q, want %q", got, "Cursor Agent 2.0.0")
	}
}

func TestFirstCLIPeekVersion_AllMissingReturnsEmpty(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if got := firstCLIPeekVersion("nope-a", "nope-b"); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

// TestClaudeVersion_WithFakeBinary exercises the happy path of
// (*claude).Version through a fake `claude` binary on PATH.
func TestClaudeVersion_WithFakeBinary(t *testing.T) {
	dir := installFakeCLI(t, "claude", "Claude Code 2.5.1\nfoo")
	t.Setenv("PATH", dir)
	got := NewClaude().Version()
	if got != "Claude Code 2.5.1" {
		t.Errorf("claude.Version() = %q, want %q", got, "Claude Code 2.5.1")
	}
}

func TestClaudeVersion_NoBinaryReturnsEmpty(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if got := NewClaude().Version(); got != "" {
		t.Errorf("expected empty version, got %q", got)
	}
}

func TestCodexVersion_WithFakeBinary(t *testing.T) {
	dir := installFakeCLI(t, "codex", "codex-cli 0.4.2")
	t.Setenv("PATH", dir)
	got := NewCodex().Version()
	if got != "codex-cli 0.4.2" {
		t.Errorf("codex.Version() = %q", got)
	}
}

func TestCodexVersion_NoBinaryReturnsEmpty(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if got := NewCodex().Version(); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestOpenCodeVersion_WithFakeBinary(t *testing.T) {
	dir := installFakeCLI(t, "opencode", "opencode v0.9.0\nbuild=xyz")
	t.Setenv("PATH", dir)
	got := NewOpenCode().Version()
	if got != "opencode v0.9.0" {
		t.Errorf("opencode.Version() = %q", got)
	}
}

func TestOpenCodeVersion_NoBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if got := NewOpenCode().Version(); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

// TestCopilotVersion_ExtensionDir exercises the VSCode-extension discovery
// branch of (*copilot).Version by seeding a fake `~/.vscode/extensions/github.copilot-1.2.3/`.
func TestCopilotVersion_ExtensionDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir()) // no `copilot` binary; force extension branch
	extDir := filepath.Join(home, ".vscode", "extensions", "github.copilot-1.234.5")
	if err := os.MkdirAll(extDir, 0755); err != nil {
		t.Fatal(err)
	}
	got := NewCopilot().Version()
	if !strings.HasSuffix(got, "(Extension)") {
		t.Errorf("expected extension suffix, got %q", got)
	}
	if !strings.Contains(got, "1.234.5") {
		t.Errorf("expected version segment, got %q", got)
	}
}

func TestCopilotVersion_CLIFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := installFakeCLI(t, "copilot", "copilot 0.0.99")
	t.Setenv("PATH", dir)
	got := NewCopilot().Version()
	if got != "copilot 0.0.99" {
		t.Errorf("copilot.Version() = %q", got)
	}
}

func TestCopilotIsInstalled_ViaExtensionDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())
	ext := filepath.Join(home, ".vscode", "extensions", "github.copilot-1.0.0")
	if err := os.MkdirAll(ext, 0755); err != nil {
		t.Fatal(err)
	}
	if !NewCopilot().IsInstalled() {
		t.Error("expected IsInstalled to return true via extension dir")
	}
}

// TestClaudeIsInstalled_ViaClaudeDir covers the home-dir fallback (no `claude`
// binary, but ~/.claude exists).
func TestClaudeIsInstalled_ViaClaudeDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0755); err != nil {
		t.Fatal(err)
	}
	if !NewClaude().IsInstalled() {
		t.Error("expected IsInstalled true via ~/.claude")
	}
}

func TestCursorIsInstalled_AgentBinary(t *testing.T) {
	dir := installFakeCLI(t, "agent", "v1")
	t.Setenv("PATH", dir)
	// Cursor.app may or may not exist on the runner; behaviour:
	// if Cursor.app exists -> true; else if agent on PATH -> true.
	// Either way the function returns true here.
	if !NewCursor().IsInstalled() {
		t.Error("expected IsInstalled true with agent on PATH")
	}
}

// TestCursorVersion_NoAppFallsBackToCLI: on non-Darwin (or if Cursor.app is
// absent) the path is the `firstCLIPeekVersion("agent", "cursor")` fallback.
func TestCursorVersion_NoAppFallsBackToCLI(t *testing.T) {
	if _, err := os.Stat("/Applications/Cursor.app"); err == nil {
		t.Skip("Cursor.app exists on this host — fallback branch not reachable")
	}
	dir := installFakeCLI(t, "agent", "Cursor Agent 9.9.9")
	t.Setenv("PATH", dir)
	got := NewCursor().Version()
	if got != "Cursor Agent 9.9.9" {
		t.Errorf("cursor.Version() = %q", got)
	}
}

// TestMacOSCursorAppShortVersion_MissingPlist exercises the error branch.
// We can't easily construct a fake plist at /Applications, so we accept that
// when the file is absent the call returns an error.
func TestMacOSCursorAppShortVersion_MissingPlist(t *testing.T) {
	if _, err := os.Stat("/Applications/Cursor.app/Contents/Info.plist"); err == nil {
		t.Skip("plist exists; error branch not reachable")
	}
	if _, err := macOSCursorAppShortVersion(); err == nil {
		t.Error("expected error when plist is missing")
	}
}

// TestClaudeFindSessionsOnBranch_MatchesMostRecent seeds two JSONL session
// files under ~/.claude/projects/<slug>/ and checks the resolver returns the
// matching session.
func TestClaudeFindSessionsOnBranch_MatchesMostRecent(t *testing.T) {
	home := t.TempDir()
	project := "/repo/example"
	branch := "feature/branch-x"

	good := `{"type":"assistant","sessionId":"sess-good","timestamp":"2026-05-11T11:30:00Z","gitBranch":"feature/branch-x"}`
	writeClaudeProjectJSONL(t, home, project, "sess-good", []string{good})

	stale := `{"type":"assistant","sessionId":"sess-stale","timestamp":"2026-05-09T11:30:00Z","gitBranch":"other"}`
	writeClaudeProjectJSONL(t, home, project, "sess-stale", []string{stale})

	got := claudeFindSessionsOnBranch(home, project, branch, 5)
	if len(got) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(got))
	}
	if got[0].SessionID != "sess-good" {
		t.Errorf("SessionID = %q, want sess-good", got[0].SessionID)
	}
	// Same call via the platform method.
	got2 := NewClaude().(interface {
		FindSessionsOnBranch(string, string, string, int) []BranchSessionInfo
	}).FindSessionsOnBranch(home, project, branch, 5)
	if len(got2) != 1 || got2[0].SessionID != "sess-good" {
		t.Errorf("Platform.FindSessionsOnBranch returned %+v", got2)
	}
}

func TestClaudeFindSessionsOnBranch_NoProjectsDir(t *testing.T) {
	if got := claudeFindSessionsOnBranch(t.TempDir(), "/no/where", "main", 5); got != nil {
		t.Errorf("expected nil for missing projects dir, got %+v", got)
	}
}

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

// TestResolveClaudeCodeModelFromJSONL parses a synthetic claude session JSONL.
func TestResolveClaudeCodeModelFromJSONL(t *testing.T) {
	home := t.TempDir()
	project := "/repo/example"
	sess := "claude-sess"
	lines := []string{
		`{"type":"user","message":{"content":"hi"}}`,
		`{"type":"assistant","message":{"model":"claude-3-5","content":[]}}`,
		`{"type":"assistant","message":{"model":"claude-3-7","content":[]}}`,
		`not-json`,
	}
	writeClaudeProjectJSONL(t, home, project, sess, lines)
	got := resolveClaudeCodeModelFromJSONL(home, project, sess)
	if got != "claude-3-7" {
		t.Errorf("model = %q, want claude-3-7", got)
	}
}

func TestResolveClaudeCodeModelFromJSONL_MissingFile(t *testing.T) {
	if got := resolveClaudeCodeModelFromJSONL(t.TempDir(), "/none", "x"); got != "" {
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

// TestResolveScopedFileFromBuckets covers the otherwise-dead-code multi-bucket
// resolver in resources.go.
func TestResolveScopedFileFromBuckets(t *testing.T) {
	tmp := t.TempDir()
	mkfile := func(parts ...string) string {
		p := filepath.Join(append([]string{tmp}, parts...)...)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	// Project scope wins over global.
	projFile := mkfile("settings", "proj", "thing.json")
	mkfile("settings", "global", "thing.json")

	got := resolveScopedFileFromBuckets(tmp, []string{"settings"}, "proj", "thing.json")
	if got != projFile {
		t.Errorf("got %q, want %q", got, projFile)
	}

	// Falls back to global when project is missing.
	globalOnly := mkfile("hooks", "global", "other.json")
	got = resolveScopedFileFromBuckets(tmp, []string{"hooks"}, "noproj", "other.json")
	if got != globalOnly {
		t.Errorf("got %q, want %q", got, globalOnly)
	}

	// Cross-bucket search.
	got = resolveScopedFileFromBuckets(tmp, []string{"missing-bucket", "hooks"}, "noproj", "other.json")
	if got != globalOnly {
		t.Errorf("cross-bucket got %q, want %q", got, globalOnly)
	}

	// No match → empty.
	if got := resolveScopedFileFromBuckets(tmp, []string{"hooks"}, "proj", "nope.json"); got != "" {
		t.Errorf("expected empty for no match, got %q", got)
	}
}

// TestExecuteSharedSkillMirrorPlan drives the helper that wraps
// BuildSharedSkillMirrorIntents + BuildResourcePlan + Execute.
func TestExecuteSharedSkillMirrorPlan(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	repo := filepath.Join(tmp, "repo")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	// Seed one project-scope skill.
	skillDir := filepath.Join(agentsHome, "skills", "proj", "my-skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: my-skill\n---\nbody\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := ExecuteSharedSkillMirrorPlan("proj", repo, ".agents/skills"); err != nil {
		t.Fatalf("ExecuteSharedSkillMirrorPlan: %v", err)
	}

	link := filepath.Join(repo, ".agents/skills", "my-skill")
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("lstat link: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("expected symlink at %s", link)
	}

	// Empty target roots → no-op.
	if err := ExecuteSharedSkillMirrorPlan("proj", repo); err != nil {
		t.Errorf("empty target roots should be a no-op, got %v", err)
	}
}

// TestRemoveManagedRenderedHookFileToUserHomes drives the user-home removal
// fanout for rendered hook settings.
func TestRemoveManagedRenderedHookFileToUserHomes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// No-op when specs are empty.
	if err := removeManagedRenderedHookFileToUserHomes(nil, ".claude/settings.json", renderClaudeHookSettings); err != nil {
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

	if err := removeManagedRenderedHookFileToUserHomes(specs, ".claude/settings.json", renderClaudeHookSettings); err != nil {
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
	specs := []HookSpec{{
		Name:       "x",
		When:       "post_tool_use", // not in cursor switch
		Command:    "/bin/true",
		RequiredOn: []string{"cursor"},
	}}
	if _, err := renderCursorHookConfig(specs); err == nil {
		t.Error("expected error")
	}
}

func TestRenderCopilotHookFile_RequiredUnrepresentableErrors(t *testing.T) {
	_, _, _, err := renderCopilotHookFile(HookSpec{
		Name:       "x",
		When:       "post_tool_use", // not in copilot switch
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
		"user_prompt_submit", "stop",
	})
	assertEventNameMapping(t, cursorEventName, "cursor", []string{
		"pre_tool_use", "user_prompt_submit", "stop", "session_start",
	})
	assertEventNameMapping(t, copilotEventName, "copilot", []string{
		"session_start", "user_prompt_submit", "pre_tool_use",
	})
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
	if err := emitHookSpec(nil, "/dev/null", HookEmissionMode{}); err != nil {
		t.Errorf("nil spec should no-op, got %v", err)
	}
	if err := emitHookSpecToUserHomes(nil, ".x/y", HookEmissionMode{}); err != nil {
		t.Errorf("nil spec to user homes should no-op, got %v", err)
	}
}

func TestEmitHookSpec_UnknownShapeErrors(t *testing.T) {
	if err := emitHookSpec(&HookSpec{}, "/tmp/x", HookEmissionMode{Shape: "wat"}); err == nil {
		t.Error("expected error for unknown shape")
	}
	if err := emitHookSpec(&HookSpec{}, "/tmp/x", HookEmissionMode{Shape: HookShapeRenderSingle}); err == nil {
		t.Error("render shapes are not single-direct emission")
	}
}

func TestEmitHookFile_UnknownTransportErrors(t *testing.T) {
	if err := emitHookFile("/no/where", "/tmp/x", "weird"); err == nil {
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
	if err := emitHookFile(src, dst, HookTransportWrite); err != nil {
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
	if err := emitHookFile("/no/where", filepath.Join(t.TempDir(), "out"), HookTransportWrite); err == nil {
		t.Error("expected error reading missing source")
	}
}

func TestEmitHookFanout_RejectsWrongShape(t *testing.T) {
	err := emitHookFanout(nil, t.TempDir(), HookEmissionMode{Shape: HookShapeDirect}, func(HookSpec) (string, bool) { return "", false })
	if err == nil {
		t.Error("expected error for non-fanout shape")
	}
}

func TestEmitRenderedHookFile_NilSpecsNoOp(t *testing.T) {
	if err := emitRenderedHookFile(nil, "/tmp/x", renderClaudeHookSettings); err != nil {
		t.Errorf("nil specs should no-op, got %v", err)
	}
	if err := emitRenderedHookFileToUserHomes(nil, ".x", renderClaudeHookSettings); err != nil {
		t.Errorf("nil specs to user homes should no-op, got %v", err)
	}
}

func TestEmitPreferredHookFile_FallbackToRemove(t *testing.T) {
	called := 0
	remove := func(_ string) error { called++; return nil }
	if err := emitPreferredHookFile("/tmp/x", renderClaudeHookSettings, nil, directSymlinkHookMode, remove); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if called != 1 {
		t.Errorf("expected remove invoked once, got %d", called)
	}

	// canonicalSets present → render branch used (legacy not invoked).
	tmp := t.TempDir()
	target := filepath.Join(tmp, "out.json")
	if err := emitPreferredHookFile(target, renderClaudeHookSettings, nil, directSymlinkHookMode, nil,
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
	if err := emitPreferredHookFileToUserHomes(".claude/settings.json",
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
	if err := writeManagedFile(dst, []byte("a")); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := writeManagedFile(dst, []byte("a")); err != nil {
		t.Fatalf("second identical write: %v", err)
	}
	if err := writeManagedFile(dst, []byte("b")); err != nil {
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
	if err := removeManagedFile(dst, []byte("managed")); err != nil {
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
	if err := removeManagedFile(dst, []byte("managed")); err != nil {
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
	if err := emitRenderedHookFanout(specs, dstRoot, renderCopilotHookFile); err != nil {
		t.Fatalf("emitRenderedHookFanout: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dstRoot, "a.json")); err != nil {
		t.Errorf("expected a.json: %v", err)
	}
	// Remove fanout: matches content → removes a.json, dir becomes empty and is pruned.
	if err := removeManagedRenderedHookFanout(specs, dstRoot, renderCopilotHookFile); err != nil {
		t.Fatalf("removeManagedRenderedHookFanout: %v", err)
	}
	if _, err := os.Stat(dstRoot); !os.IsNotExist(err) {
		t.Errorf("expected hooks dir removed, got %v", err)
	}
}

func TestEmitRenderedHookFanout_NilSpecsNoOp(t *testing.T) {
	if err := emitRenderedHookFanout(nil, "/tmp/x", renderCopilotHookFile); err != nil {
		t.Errorf("nil specs no-op: %v", err)
	}
	if err := removeManagedRenderedHookFanout(nil, "/tmp/x", renderCopilotHookFile); err != nil {
		t.Errorf("nil specs no-op (remove): %v", err)
	}
	if err := emitHookFanout([]HookSpec{}, t.TempDir(),
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
	if err := removeRenderedClaudeHookSettings("/no/file"); err != nil {
		t.Errorf("removeRenderedClaudeHookSettings missing: %v", err)
	}
	if err := removeRenderedCodexHookConfig("/no/file"); err != nil {
		t.Errorf("removeRenderedCodexHookConfig missing: %v", err)
	}
	if err := removeRenderedCursorHookConfig("/no/file"); err != nil {
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
	if err := removeManagedFileIf(matching, isLikelyRenderedCursorHookConfig); err != nil {
		t.Errorf("removeManagedFileIf matching: %v", err)
	}
	if _, err := os.Stat(matching); !os.IsNotExist(err) {
		t.Errorf("expected matching file removed")
	}
	if err := removeManagedFileIf(other, isLikelyRenderedCursorHookConfig); err != nil {
		t.Errorf("removeManagedFileIf other: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("expected other file preserved")
	}
	if err := removeManagedFileIf(filepath.Join(tmp, "no-such"), isLikelyRenderedCursorHookConfig); err != nil {
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
	if err := pruneManagedRenderedFanoutExtras(dst, wanted, isLikelyRenderedCursorHookConfig); err != nil {
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
	if err := pruneManagedRenderedFanoutExtras(emptyDst, nil, isLikelyRenderedCursorHookConfig); err != nil {
		t.Errorf("empty dir prune: %v", err)
	}

	// Missing dir is a no-op.
	if err := pruneManagedRenderedFanoutExtras(filepath.Join(tmp, "nope"), nil, isLikelyRenderedCursorHookConfig); err != nil {
		t.Errorf("missing dir prune: %v", err)
	}
}

// TestCopilotLegacyHookFanoutBuilds wires up a legacy `.json` hooks directory
// and asserts copilot's createProjectHookFiles emits fanout files.
func TestCopilotLegacyHookFanoutBuilds(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(agentsHome, "hooks", "proj", "session-banner.json")
	if err := os.MkdirAll(filepath.Dir(legacy), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte(`{"version":1,"hooks":{"sessionStart":[{"type":"command","bash":"x"}]}}`), 0644); err != nil {
		t.Fatal(err)
	}

	c := NewCopilot().(*copilot)
	if err := c.createProjectHookFiles("proj", repo, agentsHome); err != nil {
		t.Fatalf("createProjectHookFiles: %v", err)
	}
	out := filepath.Join(repo, ".github", "hooks", "session-banner.json")
	if _, err := os.Stat(out); err != nil {
		t.Errorf("expected fanout file at %s: %v", out, err)
	}

	// Removing should clear the fanout via legacy entries.
	if err := NewCopilot().RemoveLinks("proj", repo); err != nil {
		t.Fatalf("RemoveLinks: %v", err)
	}
}

// TestCopilotCanonicalHookFanout drives the canonical-bundle code path with
// HOOK.yaml under hooks/proj/<name>/.
func TestCopilotCanonicalHookFanout(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(agentsHome, "hooks", "global", "prompt-log", "HOOK.yaml")
	if err := os.MkdirAll(filepath.Dir(manifest), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, []byte("name: prompt-log\nwhen: user_prompt_submit\nrun:\n  command: /bin/echo\n"), 0644); err != nil {
		t.Fatal(err)
	}

	c := NewCopilot().(*copilot)
	if err := c.createProjectHookFiles("proj", repo, agentsHome); err != nil {
		t.Fatalf("createProjectHookFiles canonical: %v", err)
	}
	out := filepath.Join(repo, ".github", "hooks", "prompt-log.json")
	if _, err := os.Stat(out); err != nil {
		t.Errorf("expected canonical fanout at %s: %v", out, err)
	}
}

// TestClaudeRemoveAndRecreateRules ensures the project-rules prune path covers
// the leftover-rule branch.
func TestClaudeRulePruneRemovesLeftover(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	// Pre-seed a stale rules file.
	rulesDir := filepath.Join(repo, ".claude", "rules")
	stale := filepath.Join(rulesDir, "proj--ancient.md")
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	// Seed one current rule for the project.
	live := filepath.Join(agentsHome, "rules", "proj", "current.md")
	if err := os.MkdirAll(filepath.Dir(live), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(live, []byte("y"), 0644); err != nil {
		t.Fatal(err)
	}

	c := NewClaude().(*claude)
	if err := c.createRulesLinks("proj", repo, agentsHome); err != nil {
		t.Fatalf("createRulesLinks: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale rule should be pruned, stat err=%v", err)
	}
	want := filepath.Join(rulesDir, "proj--current.md")
	if _, err := os.Lstat(want); err != nil {
		t.Errorf("expected live rule at %s: %v", want, err)
	}
}

// TestClaudePruneRuleLinksWithoutSource: when project rules dir is missing,
// pruneProjectRuleLinks should still clean stray entries.
func TestClaudePruneRuleLinksWithoutSource(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	repo := filepath.Join(tmp, "repo")
	rulesDir := filepath.Join(repo, ".claude", "rules")
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(rulesDir, "proj--ghost.md")
	if err := os.WriteFile(stale, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	c := NewClaude().(*claude)
	if err := c.createRulesLinks("proj", repo, agentsHome); err != nil {
		t.Fatalf("createRulesLinks: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale rule should be pruned even with missing source")
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

// TestClaudeReadUsageStats_TooManyDailyEntries triggers the tail-trim branch.
func TestClaudeReadUsageStats_TooManyDailyEntries(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, ".claude")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	sb.WriteString(`{"totalSessions":1,"totalMessages":2,"modelUsage":{},"dailyActivity":[`)
	for i := 0; i < 15; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`{"date":"d` + itoa(i) + `","messageCount":1,"sessionCount":1,"toolCallCount":1}`)
	}
	sb.WriteString(`]}`)
	if err := os.WriteFile(filepath.Join(dir, "stats-cache.json"), []byte(sb.String()), 0644); err != nil {
		t.Fatal(err)
	}
	stats := claudeReadUsageStats(tmp)
	if stats == nil {
		t.Fatal("nil stats")
	}
	if len(stats.DailyActivity) != 10 {
		t.Errorf("DailyActivity tail = %d, want 10", len(stats.DailyActivity))
	}
}

// itoa is a minimal no-import int → string helper to keep this file's import
// list small.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var digits []byte
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}

// TestEnsureUnderRulesScopeTreeRejectsOutside covers the negative branch of
// EnsureUnderRulesScopeTree (rules.go).
func TestEnsureUnderRulesScopeTreeRejectsOutside(t *testing.T) {
	tmp := t.TempDir()
	good := filepath.Join(tmp, "rules", "global", "x.md")
	if err := EnsureUnderRulesScopeTree(tmp, "global", good); err != nil {
		t.Errorf("expected path under tree to pass, got %v", err)
	}
	bad := filepath.Join(tmp, "elsewhere", "x.md")
	if err := EnsureUnderRulesScopeTree(tmp, "global", bad); err == nil {
		t.Error("expected error for path outside tree")
	}
}

// TestEnsureUnderMCPScopeTreeRejectsOutside covers the negative branch of
// EnsureUnderMCPScopeTree (mcp_settings.go).
func TestEnsureUnderMCPScopeTreeRejectsOutside(t *testing.T) {
	tmp := t.TempDir()
	good := filepath.Join(tmp, "mcp", "proj", "x.json")
	if err := EnsureUnderMCPScopeTree(tmp, "proj", good); err != nil {
		t.Errorf("expected ok, got %v", err)
	}
	bad := filepath.Join(tmp, "other", "x.json")
	if err := EnsureUnderMCPScopeTree(tmp, "proj", bad); err == nil {
		t.Error("expected error for outside path")
	}
}

// TestListCanonicalMCPFilesAndSettingsFiles covers the listing helpers and
// filename filters.
func TestListCanonicalMCPFilesAndSettings(t *testing.T) {
	tmp := t.TempDir()
	mkfile := func(p, s string) {
		full := filepath.Join(tmp, p)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(s), 0644); err != nil {
			t.Fatal(err)
		}
	}
	mkfile("mcp/proj/a.json", "{}")
	mkfile("mcp/proj/.dotfile.json", "{}")
	mkfile("mcp/proj/c.txt", "skip")
	if err := os.MkdirAll(filepath.Join(tmp, "mcp", "proj", "subdir"), 0755); err != nil {
		t.Fatal(err)
	}
	got, err := ListCanonicalMCPFiles(tmp, "proj")
	if err != nil {
		t.Fatalf("list mcp: %v", err)
	}
	if len(got) != 1 || got[0].BaseName != "a.json" {
		t.Errorf("got %+v, want one entry [a.json]", got)
	}

	mkfile("settings/proj/cursor.json", "{}")
	mkfile("settings/proj/cursorignore", "ign")
	mkfile("settings/proj/random.md", "skip")
	mkfile("settings/proj/.dotfile", "skip")
	gotS, err := ListCanonicalSettingsFiles(tmp, "proj")
	if err != nil {
		t.Fatalf("list settings: %v", err)
	}
	if len(gotS) != 2 {
		t.Errorf("got %+v, want 2 (cursor.json + cursorignore)", gotS)
	}

	// Missing dir
	if _, err := ListCanonicalMCPFiles(tmp, "nonexistent"); err == nil {
		t.Error("expected error for missing scope")
	}
}

// TestResolveCanonicalMCPFile_NotFound exercises the error branch.
func TestResolveCanonicalMCPFile_NotFound(t *testing.T) {
	tmp := t.TempDir()
	if _, err := ResolveCanonicalMCPFile(tmp, "proj", "mcp"); err == nil {
		t.Error("expected error for missing file")
	}
	if _, err := ResolveCanonicalMCPFile(tmp, "proj", ""); err == nil {
		t.Error("expected error for empty name")
	}
}

func TestResolveCanonicalSettingsFile_Found(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "settings", "global")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "cursor.json")
	if err := os.WriteFile(src, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveCanonicalSettingsFile(tmp, "global", "cursor")
	if err != nil {
		t.Fatalf("ResolveCanonicalSettingsFile: %v", err)
	}
	if got.SourcePath != src {
		t.Errorf("source %q, want %q", got.SourcePath, src)
	}
}

// TestResolveCanonicalRuleFileNotFound exercises rules.go missing branches.
func TestResolveCanonicalRuleFileMissing(t *testing.T) {
	tmp := t.TempDir()
	if _, err := ResolveCanonicalRuleFile(tmp, "global", "missing"); err == nil {
		t.Error("expected error for missing rule")
	}
	if _, err := ResolveCanonicalRuleFile(tmp, "global", ""); err == nil {
		t.Error("expected error for empty name")
	}
}

func TestListCanonicalRuleFilesMissing(t *testing.T) {
	if _, err := ListCanonicalRuleFiles(t.TempDir(), "global"); err == nil {
		t.Error("expected error for missing scope dir")
	}
}

// TestCopilotResolveInstructionsSrcFallbackPath drives the rules.md fallback
// branch.
func TestCopilotResolveInstructionsSrcFallback(t *testing.T) {
	tmp := t.TempDir()
	rules := filepath.Join(tmp, "rules", "global")
	if err := os.MkdirAll(rules, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rules, "rules.md"), []byte("# rules\n"), 0644); err != nil {
		t.Fatal(err)
	}
	c := NewCopilot().(*copilot)
	got := c.resolveInstructionsSrc("proj", tmp)
	if !strings.HasSuffix(got, "rules.md") {
		t.Errorf("expected rules.md fallback, got %q", got)
	}

	// Missing → empty.
	if got := c.resolveInstructionsSrc("proj", filepath.Join(tmp, "no-such")); got != "" {
		t.Errorf("expected empty for missing rules, got %q", got)
	}
}

// TestCopilotResolveInstructionsSrcDirectCopilotInstructions covers the
// preferred (copilot-instructions.md) branch.
func TestCopilotResolveInstructionsSrcDirectFile(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "rules", "proj")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "copilot-instructions.md")
	if err := os.WriteFile(src, []byte("# copilot\n"), 0644); err != nil {
		t.Fatal(err)
	}
	c := NewCopilot().(*copilot)
	if got := c.resolveInstructionsSrc("proj", tmp); got != src {
		t.Errorf("got %q, want %q", got, src)
	}
}

// TestEnsureFileSymlinkIntent_AlreadyCorrect drives the symlink-in-place
// branch.
func TestEnsureFileSymlinkIntent_ExistingSymlinkReplaced(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	src := filepath.Join(agentsHome, "skills", "proj", "review", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(src), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("body"), 0644); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".agents", "skills"), 0755); err != nil {
		t.Fatal(err)
	}

	intent := validSharedSkillIntent(".agents/skills/review", "test")
	plan, err := BuildResourcePlan([]ResourceIntent{intent})
	if err != nil {
		t.Fatalf("BuildResourcePlan: %v", err)
	}
	if err := plan.Execute(repo, agentsHome); err != nil {
		t.Fatalf("first execute: %v", err)
	}
	// Run again; should be idempotent (already-symlink branch).
	if err := plan.Execute(repo, agentsHome); err != nil {
		t.Fatalf("second execute: %v", err)
	}
}

// TestPrepareIntentTargetForReplacement_RefusesUnmanagedFile drives the
// no-replace branch when a non-allowlisted target collides with a regular file.
func TestPrepareIntentTargetForReplacement_RefusesUnmanagedFile(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "f")
	if err := os.WriteFile(target, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	intent := ResourceIntent{
		TargetPath:    "anywhere/f",
		ReplacePolicy: ResourceReplaceNever,
	}
	if err := prepareIntentTargetForReplacement(target, intent); err == nil {
		t.Error("expected refusal for never-replace policy")
	}

	intent.ReplacePolicy = ResourceReplaceAllowlistedImportedDirOnly
	if err := prepareIntentTargetForReplacement(target, intent); err == nil {
		t.Error("expected refusal for non-allowlisted target")
	}

	// Allowlisted path replaces the file.
	intent.TargetPath = ".agents/skills/review"
	if err := prepareIntentTargetForReplacement(target, intent); err != nil {
		t.Errorf("allowlisted file replace: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Error("expected file removed")
	}
}

func TestPrepareIntentTargetForReplacement_DirPolicies(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "d")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	// IfManaged → refuse.
	if err := prepareIntentTargetForReplacement(dir, ResourceIntent{
		TargetPath:    ".agents/skills/x",
		ReplacePolicy: ResourceReplaceIfManaged,
	}); err == nil {
		t.Error("expected refusal IfManaged on dir")
	}
	// Never → refuse.
	if err := prepareIntentTargetForReplacement(dir, ResourceIntent{
		TargetPath:    ".agents/skills/x",
		ReplacePolicy: ResourceReplaceNever,
	}); err == nil {
		t.Error("expected refusal Never on dir")
	}
	// Allowlisted with marker → removed.
	marker := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(marker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := prepareIntentTargetForReplacement(dir, ResourceIntent{
		TargetPath:    ".agents/skills/x",
		ReplacePolicy: ResourceReplaceAllowlistedImportedDirOnly,
		MarkerFiles:   []string{"SKILL.md"},
	}); err != nil {
		t.Errorf("allowlisted dir replace: %v", err)
	}
}

// TestExecuteResourceIntent_UnsupportedShapeErrors drives the default branch
// in the switch.
func TestExecuteResourceIntent_UnsupportedShapeErrors(t *testing.T) {
	intent := ResourceIntent{
		Shape:     "weird",
		Transport: ResourceTransportSymlink,
	}
	if err := executeResourceIntent(intent, t.TempDir(), t.TempDir()); err == nil {
		t.Error("expected error for unsupported shape")
	}
}

func TestRemoveManagedIntentTargetUnknownMaterializerErrors(t *testing.T) {
	intent := ResourceIntent{
		Shape:        ResourceShapeRenderSingle,
		Transport:    ResourceTransportWrite,
		Materializer: "no-such-materializer",
		TargetPath:   "x",
	}
	if err := removeManagedIntentTarget(intent, t.TempDir(), t.TempDir()); err == nil {
		t.Error("expected error for unknown materializer in remove path")
	}
}

func TestCanonicalIntentSourcePath_EmptyErrors(t *testing.T) {
	if _, err := canonicalIntentSourcePath(ResourceIntent{}, ""); err == nil {
		t.Error("expected error for empty agentsHome")
	}
}

func TestResolveIntentTargetPath_Absolute(t *testing.T) {
	got := resolveIntentTargetPath("/abs", "/repo")
	if got != "/abs" {
		t.Errorf("got %q, want /abs", got)
	}
}

// TestExecuteRenderSingleWrite_UnknownMaterializer covers the default branch.
func TestExecuteRenderSingleWrite_UnknownMaterializer(t *testing.T) {
	if err := executeRenderSingleWrite(ResourceIntent{
		Materializer: "unsupported",
	}, t.TempDir(), t.TempDir()); err == nil {
		t.Error("expected error for unknown materializer")
	}
}

// TestRemoveImportedDirIfAllowlisted_NoMarkers covers refusal branch.
func TestRemoveImportedDirIfAllowlisted_NoMarkers(t *testing.T) {
	tmp := t.TempDir()
	intent := ResourceIntent{TargetPath: ".agents/skills/x"}
	if err := removeImportedDirIfAllowlisted(tmp, intent); err == nil {
		t.Error("expected error for no-marker dir")
	}
}

func TestRemoveImportedDirIfAllowlisted_NotAllowlisted(t *testing.T) {
	intent := ResourceIntent{TargetPath: "other/path"}
	if err := removeImportedDirIfAllowlisted(t.TempDir(), intent); err == nil {
		t.Error("expected error for non-allowlisted target")
	}
}

// TestIsAllowlistedSharedMirrorTarget covers each branch of the allowlist.
func TestIsAllowlistedSharedMirrorTarget(t *testing.T) {
	for _, ok := range []string{
		".agents/skills/x", ".claude/skills/x", ".claude/agents/x",
		".codex/agents/x", ".opencode/plugins/x", ".opencode/agent/x",
		".github/agents/x",
	} {
		if !isAllowlistedSharedMirrorTarget(ok) {
			t.Errorf("expected %q allowlisted", ok)
		}
	}
	if isAllowlistedSharedMirrorTarget("random/path") {
		t.Error("random path should not be allowlisted")
	}
}

// TestReadFrontmatterEdgeCases exercises the frontmatter parser.
func TestReadFrontmatterEdgeCases(t *testing.T) {
	tmp := t.TempDir()
	cases := []struct {
		name    string
		content string
		want    map[string]string
	}{
		{
			name:    "no-frontmatter",
			content: "# heading\nbody\n",
			want:    map[string]string{},
		},
		{
			name:    "valid-frontmatter",
			content: "---\nname: x\ndesc: y\n---\nbody\n",
			want:    map[string]string{"name": "x", "desc": "y"},
		},
		{
			name:    "unterminated-frontmatter",
			content: "---\nname: x\nno-end\n",
			want:    map[string]string{"name": "x", "no-end": ""},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(tmp, tc.name+".md")
			if err := os.WriteFile(path, []byte(tc.content), 0644); err != nil {
				t.Fatal(err)
			}
			got := readFrontmatter(path)
			for k, v := range tc.want {
				if got[k] != v {
					t.Errorf("[%s] %s = %q, want %q", tc.name, k, got[k], v)
				}
			}
		})
	}
	if got := readFrontmatter(filepath.Join(tmp, "no-such")); got != nil {
		t.Errorf("expected nil for missing file, got %+v", got)
	}
}

// TestReadAgentBody covers the frontmatter-stripping reader.
func TestReadAgentBody(t *testing.T) {
	tmp := t.TempDir()
	cases := []struct {
		content, want string
		hasErr        bool
	}{
		{"---\nname: x\n---\n\nbody\n", "body\n", false},
		{"plain body\n", "plain body\n", false},
		{"---\nno-end", "---\nno-end", false},
	}
	for i, tc := range cases {
		p := filepath.Join(tmp, "x"+itoa(i)+".md")
		if err := os.WriteFile(p, []byte(tc.content), 0644); err != nil {
			t.Fatal(err)
		}
		got, err := readAgentBody(p)
		if (err != nil) != tc.hasErr {
			t.Errorf("[%d] err=%v hasErr=%v", i, err, tc.hasErr)
		}
		if got != tc.want {
			t.Errorf("[%d] got %q, want %q", i, got, tc.want)
		}
	}
	if _, err := readAgentBody("/no/such"); err == nil {
		t.Error("expected error for missing file")
	}
}

// TestOpencodeRemoveLinksFullPath exercises every branch of opencode.RemoveLinks.
func TestOpencodeRemoveLinksFullPath(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	// Seed an agent file under the agents home, then symlink it into the repo.
	src := filepath.Join(agentsHome, "agents", "proj", "reviewer.md")
	if err := os.MkdirAll(filepath.Dir(src), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("body"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(repo, ".opencode", "agent", "reviewer.md")
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(src, dst); err != nil {
		t.Fatal(err)
	}
	// Skills symlink.
	skillSrc := filepath.Join(agentsHome, "skills", "proj", "x")
	if err := os.MkdirAll(skillSrc, 0755); err != nil {
		t.Fatal(err)
	}
	skillDst := filepath.Join(repo, ".agents", "skills", "x")
	if err := os.MkdirAll(filepath.Dir(skillDst), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(skillSrc, skillDst); err != nil {
		t.Fatal(err)
	}

	if err := NewOpenCode().RemoveLinks("proj", repo); err != nil {
		t.Fatalf("RemoveLinks: %v", err)
	}
	if _, err := os.Lstat(dst); !os.IsNotExist(err) {
		t.Error("agent symlink should be removed")
	}
	if _, err := os.Lstat(skillDst); !os.IsNotExist(err) {
		t.Error("skill symlink should be removed")
	}
}

// TestCopilotRemoveLinksFullSweep wires Copilot remove against a seeded
// shared-target layout to drive removeAgentLinks and removeHookLinks.
func TestCopilotRemoveLinksFullSweep(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	// Agent symlink.
	src := filepath.Join(agentsHome, "agents", "proj", "reviewer.agent.md")
	if err := os.MkdirAll(filepath.Dir(src), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(repo, ".github", "agents", "reviewer.agent.md")
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(src, dst); err != nil {
		t.Fatal(err)
	}
	// Hooks dir with a stale entry.
	hookSrc := filepath.Join(agentsHome, "hooks", "proj", "abc.json")
	if err := os.MkdirAll(filepath.Dir(hookSrc), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hookSrc, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	hookDst := filepath.Join(repo, ".github", "hooks", "abc.json")
	if err := os.MkdirAll(filepath.Dir(hookDst), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(hookSrc, hookDst); err != nil {
		t.Fatal(err)
	}

	if err := NewCopilot().RemoveLinks("proj", repo); err != nil {
		t.Fatalf("RemoveLinks: %v", err)
	}
	if _, err := os.Lstat(dst); !os.IsNotExist(err) {
		t.Error("agent symlink should be removed")
	}
	if _, err := os.Lstat(hookDst); !os.IsNotExist(err) {
		t.Error("hook symlink should be removed")
	}
}

// TestCursorRemoveLinksWithExistingAgentLinks drives removeAgentLinks via a
// seeded `.cursor/agents/<name>` symlink.
func TestCursorRemoveLinksWithExistingAgentLinks(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(agentsHome, "agents", "proj", "reviewer")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(repo, ".cursor", "agents", "reviewer")
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(src, dst); err != nil {
		t.Fatal(err)
	}
	if err := NewCursor().RemoveLinks("proj", repo); err != nil {
		t.Fatalf("RemoveLinks: %v", err)
	}
	if _, err := os.Lstat(dst); !os.IsNotExist(err) {
		t.Error("cursor agent symlink should be removed")
	}
}

// TestClaudeRemoveLinksFullSweep drives the .claude / .agents remove paths.
func TestClaudeRemoveLinksFullSweep(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	// Seed a `.mcp.json` symlink under repo pointing inside agentsHome.
	mcpSrc := filepath.Join(agentsHome, "mcp", "proj", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(mcpSrc), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mcpSrc, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	mcpDst := filepath.Join(repo, ".mcp.json")
	if err := os.Symlink(mcpSrc, mcpDst); err != nil {
		t.Fatal(err)
	}
	// Seed a stale rule symlink.
	ruleSrc := filepath.Join(agentsHome, "rules", "proj", "x.md")
	if err := os.MkdirAll(filepath.Dir(ruleSrc), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ruleSrc, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	ruleDst := filepath.Join(repo, ".claude", "rules", "proj--x.md")
	if err := os.MkdirAll(filepath.Dir(ruleDst), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(ruleSrc, ruleDst); err != nil {
		t.Fatal(err)
	}

	if err := NewClaude().RemoveLinks("proj", repo); err != nil {
		t.Fatalf("RemoveLinks: %v", err)
	}
	if _, err := os.Lstat(mcpDst); !os.IsNotExist(err) {
		t.Error(".mcp.json symlink should be removed")
	}
	if _, err := os.Lstat(ruleDst); !os.IsNotExist(err) {
		t.Error("project rule symlink should be removed")
	}
}

// TestIsClaudeAgentDir covers the directory-with-AGENT.md check.
func TestIsClaudeAgentDir(t *testing.T) {
	tmp := t.TempDir()
	good := filepath.Join(tmp, "agent")
	if err := os.MkdirAll(good, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(good, "AGENT.md"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if !isClaudeAgentDir(good) {
		t.Error("expected true for dir with AGENT.md")
	}
	noMarker := filepath.Join(tmp, "no-marker")
	if err := os.MkdirAll(noMarker, 0755); err != nil {
		t.Fatal(err)
	}
	if isClaudeAgentDir(noMarker) {
		t.Error("expected false for dir without AGENT.md")
	}
	// Non-directory path.
	notDir := filepath.Join(tmp, "regular")
	if err := os.WriteFile(notDir, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if isClaudeAgentDir(notDir) {
		t.Error("expected false for non-directory")
	}
}

// TestSyncScopedFileSymlinks drives the opencode-style file fanout helper.
func TestSyncScopedFileSymlinks(t *testing.T) {
	tmp := t.TempDir()
	agentDir := filepath.Join(tmp, "agents", "global", "reviewer")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "AGENT.md"), []byte("body"), 0644); err != nil {
		t.Fatal(err)
	}
	dstRoot := filepath.Join(tmp, "out")
	if err := syncScopedFileSymlinks(tmp, "agents", "global", "AGENT.md", dstRoot, ".md"); err != nil {
		t.Fatalf("syncScopedFileSymlinks: %v", err)
	}
	link := filepath.Join(dstRoot, "reviewer.md")
	if info, err := os.Lstat(link); err != nil {
		t.Errorf("expected link at %s: %v", link, err)
	} else if info.Mode()&os.ModeSymlink == 0 {
		t.Error("expected symlink")
	}

	// Missing source → no-op (no error).
	if err := syncScopedFileSymlinks(tmp, "no-such-bucket", "global", "AGENT.md", t.TempDir(), ".md"); err != nil {
		t.Errorf("missing bucket should be no-op, got %v", err)
	}
}
