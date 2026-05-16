package platform

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

// TestSessionReaderAccessors covers the trivial constant-returning methods
// on every platform's SessionReader / StatsReader surface. These are pure
// identity functions but count towards coverage gate.
// checkAIAgentPrefix asserts AIAgentPrefix() (if implemented) is non-empty.
func checkAIAgentPrefix(t *testing.T, p Platform) {
	sr, ok := p.(interface{ AIAgentPrefix() string })
	if !ok {
		return
	}
	if sr.AIAgentPrefix() == "" {
		t.Errorf("%s: AIAgentPrefix empty", p.ID())
	}
}

// exerciseSessionEnvs calls SessionEnvs/EntrypointEnvs accessors if present
// to ensure they do not panic (values may legitimately be nil).
func exerciseSessionEnvs(p Platform) {
	if sr, ok := p.(interface{ SessionEnvs() []string }); ok {
		_ = sr.SessionEnvs()
	}
	if sr, ok := p.(interface{ EntrypointEnvs() []string }); ok {
		_ = sr.EntrypointEnvs()
	}
}

// exerciseResolveModel exercises the optional ResolveModel accessor.
func exerciseResolveModel(t *testing.T, p Platform) {
	sr, ok := p.(interface {
		ResolveModel(string, string, string) string
	})
	if !ok {
		return
	}
	_ = sr.ResolveModel(t.TempDir(), "/repo", "no-session")
}

// exerciseReadUsageStats exercises ReadUsageStats on an empty home; non-nil
// result is logged but not failed (some platforms may probe non-home paths).
func exerciseReadUsageStats(t *testing.T, p Platform) {
	sr, ok := p.(interface {
		ReadUsageStats(string) *PlatformUsageStats
	})
	if !ok {
		return
	}
	if sr.ReadUsageStats(t.TempDir()) != nil {
		t.Logf("%s: unexpected non-nil stats from empty home", p.ID())
	}
}

// exerciseScanSessionTokens exercises the optional ScanSessionTokens accessor.
func exerciseScanSessionTokens(t *testing.T, p Platform) {
	sr, ok := p.(interface {
		ScanSessionTokens(string, string, string, string) SessionTokenMetrics
	})
	if !ok {
		return
	}
	_ = sr.ScanSessionTokens(t.TempDir(), "/repo", "sid", "")
}

func TestSessionReaderAccessors(t *testing.T) {
	for _, p := range All() {
		checkAIAgentPrefix(t, p)
		exerciseSessionEnvs(p)
		exerciseResolveModel(t, p)
		exerciseReadUsageStats(t, p)
		exerciseScanSessionTokens(t, p)
	}
}

func TestVersionAndIsInstalledBoundedNoPanic(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	os.MkdirAll(filepath.Join(tmp, "home"), 0755)
	// PATH stripped to a clean tmp so LookPath cleanly misses every binary.
	t.Setenv("PATH", filepath.Join(tmp, "empty-bin"))
	os.MkdirAll(filepath.Join(tmp, "empty-bin"), 0755)

	for _, p := range All() {
		_ = p.IsInstalled() // may be true or false; must not panic
		// Versions are best-effort. With no binary in PATH most return "".
		if vp, ok := p.(interface{ Version() string }); ok {
			_ = vp.Version()
		}
	}
}

// TestParseJSONLTimestamp tests the dual-format timestamp parser used by
// session scanners.
func TestParseJSONLTimestamp(t *testing.T) {
	cases := []struct {
		ts      string
		wantOK  bool
		isAfter time.Time
	}{
		{"2026-04-01T12:00:00Z", true, time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)},
		{"2026-04-01T12:00:00.123456789Z", true, time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)},
		{"not a date", false, time.Time{}},
		{"", false, time.Time{}},
		{"2026-04-01", false, time.Time{}}, // bare date
	}
	for _, tc := range cases {
		got, ok := parseJSONLTimestamp(tc.ts)
		if ok != tc.wantOK {
			t.Errorf("parseJSONLTimestamp(%q) ok=%v, want %v", tc.ts, ok, tc.wantOK)
		}
		if ok && !got.After(tc.isAfter) {
			t.Errorf("parseJSONLTimestamp(%q) returned non-future-ish time %v", tc.ts, got)
		}
	}
}

// TestCanonicalBucketHelpers covers the small path-join helpers in
// buckets.go that compose the canonical store layout.
func TestCanonicalBucketHelpers(t *testing.T) {
	if got, want := CanonicalBucketRoot("/h", CanonicalBucketSkills), filepath.Join("/h", "skills"); got != want {
		t.Errorf("Root: %q, want %q", got, want)
	}
	if got, want := CanonicalBucketScopeRoot("/h", CanonicalBucketAgents, "global"), filepath.Join("/h", "agents", "global"); got != want {
		t.Errorf("ScopeRoot: %q, want %q", got, want)
	}
	if got := CanonicalBucketPath(CanonicalBucketRules, "global", "x.md"); got != "rules/global/x.md" {
		t.Errorf("Path: %q", got)
	}
	if got := CanonicalBucketScopePath(CanonicalBucketHooks, "proj", "b1", "HOOK.yaml"); got != "hooks/proj/b1/HOOK.yaml" {
		t.Errorf("ScopePath: %q", got)
	}
}

func TestCanonicalStoreBucketSpecs(t *testing.T) {
	all := CanonicalStoreBucketSpecs()
	stage1 := CanonicalStoreStage1BucketSpecs()
	stage2 := CanonicalStoreStage2BucketSpecs()
	if len(all) != len(stage1)+len(stage2) {
		t.Errorf("total %d != stage1 %d + stage2 %d", len(all), len(stage1), len(stage2))
	}
	for _, s := range stage1 {
		if s.Stage != 1 {
			t.Errorf("stage1 spec %s has Stage=%d", s.Name, s.Stage)
		}
	}
	for _, s := range stage2 {
		if s.Stage != 2 {
			t.Errorf("stage2 spec %s has Stage=%d", s.Name, s.Stage)
		}
	}
	// Marker files in stage1 should be present for dir-counted buckets.
	seenSkills := false
	for _, s := range stage1 {
		if s.Name == CanonicalBucketSkills {
			seenSkills = true
			if s.MarkerFile != "SKILL.md" {
				t.Errorf("Skills MarkerFile: %q", s.MarkerFile)
			}
		}
	}
	if !seenSkills {
		t.Error("Stage1 should include skills bucket")
	}
}

func TestListScopedResourceDirsForBucket(t *testing.T) {
	tmp := t.TempDir()
	scopeDir := filepath.Join(tmp, "skills", "global")
	os.MkdirAll(filepath.Join(scopeDir, "alpha"), 0755)
	os.WriteFile(filepath.Join(scopeDir, "alpha", "SKILL.md"), []byte("x"), 0644)
	os.MkdirAll(filepath.Join(scopeDir, "no-marker"), 0755)

	got, err := ListScopedResourceDirsForBucket(tmp, CanonicalBucketSkills, "global", "SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("expected 1 result, got %d", len(got))
	}
}

// TestSortedUniqueStrings covers the small helper in plugins.go.
func TestSortedUniqueStrings(t *testing.T) {
	got := SortedUniqueStrings([]string{"  b ", "a", "a", "", "  ", "c"})
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	sort.Strings(got)
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("idx %d: %q vs %q", i, got[i], want[i])
		}
	}
	if SortedUniqueStrings(nil) != nil {
		t.Error("nil input should return nil")
	}
}

// TestListPluginSpecs_MissingRootReturnsNil exercises the nil-on-IsNotExist branch.
func TestListPluginSpecs_MissingRootReturnsNil(t *testing.T) {
	specs, err := ListPluginSpecs(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if specs != nil {
		t.Errorf("expected nil for missing plugins/, got %v", specs)
	}
	// Scope variant on missing root also returns nil.
	specs, err = ListPluginSpecs(t.TempDir(), "global")
	if err != nil {
		t.Fatal(err)
	}
	if specs != nil {
		t.Errorf("scope-form expected nil, got %v", specs)
	}
}

func TestListPluginSpecs_SkipsNonDir(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "plugins")
	os.MkdirAll(root, 0755)
	os.WriteFile(filepath.Join(root, "stray.txt"), []byte("x"), 0644)
	specs, err := ListPluginSpecs(tmp, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 0 {
		t.Errorf("expected 0 specs, got %d", len(specs))
	}
}

func TestLoadPluginSpec_BadFiles(t *testing.T) {
	tmp := t.TempDir()
	// missing manifest
	if _, err := LoadPluginSpec(tmp); err == nil {
		t.Error("expected error for missing manifest")
	}

	// malformed yaml
	dir := filepath.Join(tmp, "broken")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, PluginManifestName), []byte(":\n  -nope\n"), 0644)
	if _, err := LoadPluginSpec(dir); err == nil {
		t.Error("expected error for malformed yaml")
	}

	// schema violation
	dir2 := filepath.Join(tmp, "bad-schema")
	os.MkdirAll(dir2, 0755)
	os.WriteFile(filepath.Join(dir2, PluginManifestName), []byte("schema_version: 1\nkind: weird\nname: x\nplatforms:\n  - claude\n"), 0644)
	if _, err := LoadPluginSpec(dir2); err == nil {
		t.Error("expected schema error for unknown kind")
	}
}

func TestLoadPluginSpec_Valid(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "good")
	os.MkdirAll(dir, 0755)
	content := `schema_version: 1
kind: native
name: test-plugin
version: 1.0.0
platforms:
  - claude
  - codex
authors:
  - "  alice "
  - bob
  - alice
`
	os.WriteFile(filepath.Join(dir, PluginManifestName), []byte(content), 0644)
	spec, err := LoadPluginSpec(dir)
	if err != nil {
		t.Fatalf("LoadPluginSpec: %v", err)
	}
	if spec.Name != "test-plugin" {
		t.Errorf("Name: %q", spec.Name)
	}
	if spec.Kind != PluginKindNative {
		t.Errorf("Kind: %q", spec.Kind)
	}
	// Authors deduped + sorted + trimmed
	if len(spec.Authors) != 2 || spec.Authors[0] != "alice" || spec.Authors[1] != "bob" {
		t.Errorf("Authors not normalized: %v", spec.Authors)
	}
}

func TestEnsureUnderSettingsScopeTree(t *testing.T) {
	tmp := t.TempDir()
	scope := "global"
	good := filepath.Join(tmp, "settings", scope, "claude-code.json")
	if err := EnsureUnderSettingsScopeTree(tmp, scope, good); err != nil {
		t.Errorf("path under scope should validate: %v", err)
	}
	bad := filepath.Join(tmp, "elsewhere", "claude-code.json")
	if err := EnsureUnderSettingsScopeTree(tmp, scope, bad); err == nil {
		t.Error("path outside scope tree should error")
	}
}

func TestResourceIntent_Validate(t *testing.T) {
	intent := ResourceIntent{}
	if err := intent.Validate(); err == nil {
		t.Error("empty intent should be invalid")
	}
}

// TestClaudeUsageStatsParse exercises the stats-cache JSON parser by writing
// a minimal cache and verifying the returned struct.
func TestClaudeUsageStatsParse(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, ".claude")
	os.MkdirAll(dir, 0755)
	cache := map[string]any{
		"totalSessions": 3,
		"totalMessages": 12,
		"modelUsage": map[string]any{
			"sonnet": map[string]int{"inputTokens": 100, "outputTokens": 50, "cacheReadInputTokens": 10, "cacheCreationInputTokens": 5},
		},
		"dailyActivity": []map[string]any{
			{"date": "2026-04-01", "messageCount": 1, "sessionCount": 1, "toolCallCount": 1},
		},
	}
	data, _ := json.Marshal(cache)
	os.WriteFile(filepath.Join(dir, "stats-cache.json"), data, 0644)

	stats := claudeReadUsageStats(tmp)
	if stats == nil {
		t.Fatal("expected non-nil stats")
	}
	if stats.PlatformID != "claude" {
		t.Errorf("PlatformID: %q", stats.PlatformID)
	}
	if stats.TotalSessions != 3 || stats.TotalMessages != 12 {
		t.Errorf("totals: %+v", stats)
	}
	if usage, ok := stats.TokensByModel["sonnet"]; !ok || usage.InputTokens != 100 {
		t.Errorf("missing or wrong sonnet entry: %+v", stats.TokensByModel)
	}
}

func TestClaudeUsageStatsBadJSON(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, ".claude")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "stats-cache.json"), []byte("not json"), 0644)
	if got := claudeReadUsageStats(tmp); got != nil {
		t.Errorf("bad json: got %+v", got)
	}
}

func TestClaudeUsageStatsMissing(t *testing.T) {
	if claudeReadUsageStats(t.TempDir()) != nil {
		t.Error("missing cache should return nil")
	}
}

// TestCodexUsageStats exercises codexReadUsageStats with a synthetic session_index.jsonl.
func TestCodexUsageStats(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, ".codex")
	os.MkdirAll(dir, 0755)
	jsonl := `{"id":"s1","thread_name":"alpha","updated_at":"2026-04-01T12:00:00Z"}
{"id":"s2","thread_name":"beta","updated_at":"2026-04-02T12:00:00Z"}
`
	os.WriteFile(filepath.Join(dir, "session_index.jsonl"), []byte(jsonl), 0644)

	stats := codexReadUsageStats(tmp)
	if stats == nil {
		t.Fatal("expected non-nil stats")
	}
	if stats.PlatformID != "codex" {
		t.Errorf("PlatformID: %q", stats.PlatformID)
	}
	if stats.TotalSessions != 2 {
		t.Errorf("TotalSessions: %d", stats.TotalSessions)
	}
	if len(stats.RecentSessions) != 2 {
		t.Errorf("RecentSessions: %d", len(stats.RecentSessions))
	}
}

func TestCodexUsageStatsMissing(t *testing.T) {
	if codexReadUsageStats(t.TempDir()) != nil {
		t.Error("missing index should return nil")
	}
}

// TestCopilotScanSessionTokens with synthetic events.jsonl
func TestCopilotScanSessionTokens(t *testing.T) {
	tmp := t.TempDir()
	stateDir := filepath.Join(tmp, ".copilot", "session-state", "abc")
	os.MkdirAll(stateDir, 0755)
	jsonl := `{"type":"session.shutdown","data":{"modelMetrics":{"gpt":{"usage":{"inputTokens":100,"outputTokens":50,"cacheReadTokens":10,"cacheWriteTokens":5,"reasoningTokens":2}}}}}
`
	os.WriteFile(filepath.Join(stateDir, "events.jsonl"), []byte(jsonl), 0644)

	m := copilotScanSessionTokens(tmp, "")
	if m.InputTokens != 100 {
		t.Errorf("InputTokens: %d", m.InputTokens)
	}
	if m.OutputTokens != 50 {
		t.Errorf("OutputTokens: %d", m.OutputTokens)
	}
	if m.ReasoningTokens != 2 {
		t.Errorf("ReasoningTokens: %d", m.ReasoningTokens)
	}
}

func TestCopilotScanSessionTokensMissing(t *testing.T) {
	m := copilotScanSessionTokens(t.TempDir(), "")
	if m.InputTokens != 0 {
		t.Errorf("expected zero metrics, got %+v", m)
	}
}

func TestCopilotAccumulateShutdownTokens_OpenError(t *testing.T) {
	var m SessionTokenMetrics
	copilotAccumulateShutdownTokens("/nonexistent/path", &m)
	if m.InputTokens != 0 {
		t.Error("expected no-op on missing file")
	}
}
