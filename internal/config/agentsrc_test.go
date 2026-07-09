package config

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/go-git/go-git/v6"

	"github.com/AGOrcha/dot-agents/internal/events"
)

const (
	testProject         = "myproject"
	testSourceTypeLocal = "local"
	testMCPJSONFile     = "mcp.json"
	testHookPreToolUse  = "PreToolUse"
	testHookPostToolUse = "PostToolUse"
	errFmtGenerateRC    = "GenerateAgentsRC: %v"
	testSkillMarkerFile = "SKILL" + ".md"
	testRealSkillName   = "real" + "-skill"
)

// ── StringsOrBool ────────────────────────────────────────────────────────────

func assertStringsOrBoolMarshalJSON(t *testing.T, input StringsOrBool, want string) {
	t.Helper()
	got, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if string(got) != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestStringsOrBoolMarshalJSON(t *testing.T) {
	cases := []struct {
		name  string
		input StringsOrBool
		want  string
	}{
		{"zero value → false", StringsOrBool{}, "false"},
		{"All true → true", StringsOrBool{All: true}, "true"},
		{"All false, empty names → false", StringsOrBool{All: false, Names: []string{}}, "false"},
		{"names take priority over All=false", StringsOrBool{Names: []string{"a", "b"}}, `["a","b"]`},
		{"names with All=true still emit array", StringsOrBool{All: true, Names: []string{"x"}}, `["x"]`},
		{"single name", StringsOrBool{Names: []string{testHookPreToolUse}}, `["PreToolUse"]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertStringsOrBoolMarshalJSON(t, tc.input, tc.want)
		})
	}
}

func assertUnmarshalStringsOrBool(t *testing.T, input string, wantAll bool, wantN []string, wantErr bool) {
	t.Helper()
	var s StringsOrBool
	err := json.Unmarshal([]byte(input), &s)
	if wantErr {
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		return
	}
	if err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if s.All != wantAll {
		t.Errorf("All: got %v, want %v", s.All, wantAll)
	}
	if !reflect.DeepEqual(s.Names, wantN) {
		t.Errorf("Names: got %v, want %v", s.Names, wantN)
	}
}

func TestStringsOrBoolUnmarshalJSON(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantAll bool
		wantN   []string
		wantErr bool
	}{
		{"true", "true", true, nil, false},
		{"false", "false", false, nil, false},
		{"empty array", "[]", false, []string{}, false},
		{"string array", `["PreToolUse","PostToolUse"]`, false, []string{testHookPreToolUse, testHookPostToolUse}, false},
		{"single element", `["SessionStart"]`, false, []string{"SessionStart"}, false},
		{"number → error", "42", false, nil, true},
		{"object → error", "{}", false, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertUnmarshalStringsOrBool(t, tc.input, tc.wantAll, tc.wantN, tc.wantErr)
		})
	}
}

func TestStringsOrBoolRoundtrip(t *testing.T) {
	originals := []StringsOrBool{
		{All: true},
		{All: false},
		{Names: []string{"a", "b", "c"}},
	}
	for _, orig := range originals {
		data, err := json.Marshal(orig)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		var got StringsOrBool
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if orig.All != got.All || !reflect.DeepEqual(orig.Names, got.Names) {
			t.Errorf("roundtrip mismatch: orig=%+v got=%+v", orig, got)
		}
	}
}

func TestStringsOrBoolIsEnabled(t *testing.T) {
	cases := []struct {
		s    StringsOrBool
		want bool
	}{
		{StringsOrBool{}, false},
		{StringsOrBool{All: true}, true},
		{StringsOrBool{All: false}, false},
		{StringsOrBool{Names: []string{"x"}}, true},
		{StringsOrBool{Names: []string{}}, false},
	}
	for _, tc := range cases {
		if got := tc.s.IsEnabled(); got != tc.want {
			t.Errorf("IsEnabled(%+v) = %v, want %v", tc.s, got, tc.want)
		}
	}
}

func TestStringsOrBoolContains(t *testing.T) {
	allTrue := StringsOrBool{All: true}
	if !allTrue.Contains("anything") {
		t.Error("All=true should contain everything")
	}

	named := StringsOrBool{Names: []string{"foo", "bar"}}
	if !named.Contains("foo") {
		t.Error("should contain foo")
	}
	if named.Contains("baz") {
		t.Error("should not contain baz")
	}

	empty := StringsOrBool{}
	if empty.Contains("x") {
		t.Error("empty should contain nothing")
	}
}

func TestStringsOrBoolAdd(t *testing.T) {
	var s StringsOrBool

	s.Add("alpha")
	if !s.Contains("alpha") || len(s.Names) != 1 {
		t.Error("add alpha failed")
	}

	// Duplicate add is a no-op
	s.Add("alpha")
	if len(s.Names) != 1 {
		t.Error("duplicate add should be no-op")
	}

	s.Add("beta")
	if len(s.Names) != 2 {
		t.Error("expected 2 names")
	}

	// Add to All=true is a no-op
	allTrue := StringsOrBool{All: true}
	allTrue.Add("x")
	if len(allTrue.Names) != 0 {
		t.Error("Add on All=true should be no-op")
	}
}

func TestStringsOrBoolRemove(t *testing.T) {
	s := StringsOrBool{Names: []string{"a", "b", "c"}}

	s.Remove("b")
	if s.Contains("b") || len(s.Names) != 2 {
		t.Errorf("Remove b failed: %v", s.Names)
	}

	// Remove non-existent is a no-op
	s.Remove("z")
	if len(s.Names) != 2 {
		t.Error("remove of missing element should be no-op")
	}

	// Remove on All=true is a no-op
	allTrue := StringsOrBool{All: true}
	allTrue.Remove("x")
	if !allTrue.All {
		t.Error("Remove on All=true should leave All unchanged")
	}
}

// ── AppendUnique ─────────────────────────────────────────────────────────────

func TestAppendUnique(t *testing.T) {
	s := AppendUnique(nil, "a")
	s = AppendUnique(s, "b")
	s = AppendUnique(s, "a") // duplicate
	s = AppendUnique(s, "c")
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(s, want) {
		t.Errorf("got %v, want %v", s, want)
	}
}

// ── GitSourceCacheDir ────────────────────────────────────────────────────────

func TestGitSourceCacheDir(t *testing.T) {
	url := "https://github.com/example/repo.git"
	dir1 := GitSourceCacheDir(url)
	dir2 := GitSourceCacheDir(url)
	if dir1 != dir2 {
		t.Error("same URL must produce same cache dir")
	}
	// Different URLs → different dirs
	other := GitSourceCacheDir("https://github.com/other/repo.git")
	if dir1 == other {
		t.Error("different URLs should produce different cache dirs")
	}
	// Hash prefix is 12 hex chars in the base name
	base := filepath.Base(dir1)
	if len(base) != 12 {
		t.Errorf("expected 12-char hash prefix, got %q (len %d)", base, len(base))
	}
}

// ── LoadAgentsRC / Save ───────────────────────────────────────────────────────

func TestLoadAgentsRCMissing(t *testing.T) {
	tmp := t.TempDir()
	_, err := LoadAgentsRC(tmp)
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLoadAgentsRCCorruptJSON(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, AgentsRCFile), []byte("{bad json"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadAgentsRC(tmp)
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
}

func TestLoadAgentsRCDefaultSources(t *testing.T) {
	tmp := t.TempDir()
	// Write a manifest with no sources field
	payload := `{"version":1,"project":"p"}`
	os.WriteFile(filepath.Join(tmp, AgentsRCFile), []byte(payload), 0644)

	rc, err := LoadAgentsRC(tmp)
	if err != nil {
		t.Fatalf("LoadAgentsRC: %v", err)
	}
	if len(rc.Sources) != 1 || rc.Sources[0].Type != testSourceTypeLocal {
		t.Errorf("expected default local source, got %+v", rc.Sources)
	}
}

func TestAgentsRCSaveLoadRoundtrip(t *testing.T) {
	tmp := t.TempDir()

	orig := &AgentsRC{
		Schema:   "https://agorcha.dev/schemas/agentsrc.schema.json",
		Version:  1,
		Project:  testProject,
		Skills:   []string{"skill-a", "skill-b"},
		Agents:   []string{"agent-x"},
		Rules:    []string{"global", "project"},
		Hooks:    StringsOrBool{Names: []string{testHookPreToolUse, testHookPostToolUse}},
		MCP:      StringsOrBool{All: true},
		Settings: true,
		Sources: []Source{
			{Type: testSourceTypeLocal},
			{Type: "git", URL: "https://github.com/example/repo.git", Ref: "main"},
		},
	}

	if err := orig.Save(tmp); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := LoadAgentsRC(tmp)
	if err != nil {
		t.Fatalf("LoadAgentsRC: %v", err)
	}

	if got.Project != orig.Project {
		t.Errorf("Project: got %q, want %q", got.Project, orig.Project)
	}
	if !reflect.DeepEqual(got.Skills, orig.Skills) {
		t.Errorf("Skills: got %v, want %v", got.Skills, orig.Skills)
	}
	if !reflect.DeepEqual(got.Agents, orig.Agents) {
		t.Errorf("Agents: got %v, want %v", got.Agents, orig.Agents)
	}
	if !reflect.DeepEqual(got.Rules, orig.Rules) {
		t.Errorf("Rules: got %v, want %v", got.Rules, orig.Rules)
	}
	if !reflect.DeepEqual(got.Hooks.Names, orig.Hooks.Names) {
		t.Errorf("Hooks.Names: got %v, want %v", got.Hooks.Names, orig.Hooks.Names)
	}
	if got.MCP.All != orig.MCP.All {
		t.Errorf("MCP.All: got %v, want %v", got.MCP.All, orig.MCP.All)
	}
	if got.Settings != orig.Settings {
		t.Errorf("Settings: got %v, want %v", got.Settings, orig.Settings)
	}
	if len(got.Sources) != 2 || got.Sources[1].URL != orig.Sources[1].URL {
		t.Errorf("Sources: got %+v, want %+v", got.Sources, orig.Sources)
	}
}

// TestLoadAgentsRC_LegacyRefreshBlockIgnoredAndStripped is the back-compat
// guard for refresh-metadata-to-lock: a manifest stamped by a pre-fix da
// build still carries a top-level "refresh" object. LoadAgentsRC must load it
// without error (the key has no AgentsRC field to decode into, so it is
// silently dropped rather than erroring or leaking into ExtraFields), and a
// subsequent Save must not re-emit it — the legacy key is stripped on the
// next rewrite.
func TestLoadAgentsRC_LegacyRefreshBlockIgnoredAndStripped(t *testing.T) {
	tmp := t.TempDir()
	legacy := `{
  "version": 1,
  "project": "myproject",
  "sources": [{"type": "local"}],
  "refresh": {"version": "0.9.0", "commit": "deadbeef", "describe": "v0.9.0", "refreshedAt": "2026-01-01T00:00:00Z"}
}`
	if err := os.WriteFile(filepath.Join(tmp, AgentsRCFile), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	rc, err := LoadAgentsRC(tmp)
	if err != nil {
		t.Fatalf("LoadAgentsRC on legacy refresh manifest: %v", err)
	}
	if len(rc.ExtraFields) != 0 {
		t.Fatalf("legacy refresh block must not leak into ExtraFields: %+v", rc.ExtraFields)
	}

	if err := rc.Save(tmp); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(tmp, AgentsRCFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "refresh") {
		t.Fatalf("re-saved manifest must strip the legacy refresh key: %s", data)
	}
}

func TestAgentsRCSaveTrailingNewline(t *testing.T) {
	tmp := t.TempDir()
	rc := &AgentsRC{Version: 1, Project: "p", Sources: []Source{{Type: testSourceTypeLocal}}}
	if err := rc.Save(tmp); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(tmp, AgentsRCFile))
	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Error("saved file should end with newline")
	}
}

func TestMergeGenerateAgentsRCNilArgs(t *testing.T) {
	gen := &AgentsRC{Version: 1, Project: "g", Sources: []Source{{Type: testSourceTypeLocal}}}
	if MergeGenerateAgentsRC(nil, gen) != gen {
		t.Fatal("nil existing should return generated pointer")
	}
	ex := &AgentsRC{Version: 1, Project: "e"}
	if MergeGenerateAgentsRC(ex, nil) != ex {
		t.Fatal("nil generated should return existing pointer")
	}
}

func TestMergeGenerateAgentsRCPreservesGitSource(t *testing.T) {
	existing := &AgentsRC{
		Version: 1,
		Project: "keep-me",
		Sources: []Source{
			{Type: "git", URL: "https://example.com/skills.git", Ref: "main"},
		},
	}
	generated := &AgentsRC{
		Version: 1,
		Project: "scan-name",
		Skills:  []string{"a"},
		Sources: []Source{{Type: testSourceTypeLocal}},
	}
	out := MergeGenerateAgentsRC(existing, generated)
	if out.Project != "keep-me" {
		t.Errorf("Project: got %q, want preserved existing", out.Project)
	}
	if len(out.Sources) != 2 {
		t.Fatalf("Sources: got %d entries, want local + git", len(out.Sources))
	}
	if out.Sources[0].Type != testSourceTypeLocal {
		t.Errorf("first source should be generated local, got %+v", out.Sources[0])
	}
	if out.Sources[1].Type != "git" || out.Sources[1].URL != "https://example.com/skills.git" {
		t.Errorf("git source not preserved: %+v", out.Sources[1])
	}
	if len(out.Skills) != 1 || out.Skills[0] != "a" {
		t.Errorf("generated skills should win: %+v", out.Skills)
	}
}

func TestMergeGenerateAgentsRCDedupesLocalSources(t *testing.T) {
	existing := &AgentsRC{
		Version: 1,
		Sources: []Source{{Type: testSourceTypeLocal}},
	}
	generated := &AgentsRC{
		Version: 1,
		Sources: []Source{{Type: testSourceTypeLocal}},
	}
	out := MergeGenerateAgentsRC(existing, generated)
	if len(out.Sources) != 1 {
		t.Fatalf("Sources: got %v, want single local", out.Sources)
	}
}

func TestMergeGenerateAgentsRCDedupesGitSources(t *testing.T) {
	gitSrc := Source{Type: "git", URL: "https://example.com/r.git", Ref: "v1"}
	existing := &AgentsRC{Version: 1, Sources: []Source{gitSrc}}
	generated := &AgentsRC{Version: 1, Sources: []Source{{Type: testSourceTypeLocal}, gitSrc}}
	out := MergeGenerateAgentsRC(existing, generated)
	if len(out.Sources) != 2 {
		t.Fatalf("Sources: got %d entries %+v, want local + git only", len(out.Sources), out.Sources)
	}
}

func TestMergeGenerateAgentsRCPreservesExtraFields(t *testing.T) {
	legacy := json.RawMessage(`{"interval":"1h"}`)
	existing := &AgentsRC{
		Version:     1,
		Sources:     []Source{{Type: "git", URL: "https://x.test/r.git"}},
		ExtraFields: map[string]json.RawMessage{"customExtension": legacy},
	}
	generated := &AgentsRC{
		Version: 1,
		Sources: []Source{{Type: testSourceTypeLocal}},
		Skills:  []string{"s"},
	}
	out := MergeGenerateAgentsRC(existing, generated)
	if len(out.ExtraFields) != 1 || string(out.ExtraFields["customExtension"]) != string(legacy) {
		t.Errorf("ExtraFields not preserved: %#v", out.ExtraFields)
	}
}

// TestInstallGenerateOverStaleManifestDropsGlobalOverDeclarations proves
// install --generate's repo-facing contract: re-generating over an existing
// on-disk .agentsrc.json that carries pre-fix, over-captured global-scope
// declarations (skills/agents/rules/hooks/mcp/settings all folding in
// "global") REPLACES those scan-derived fields with the fresh project-only
// scan rather than unioning onto the stale set — so a stale committed
// manifest is cleaned up by the very next `da install --generate`, with no
// separate prune step needed.
func TestInstallGenerateOverStaleManifestDropsGlobalOverDeclarations(t *testing.T) {
	home := agentsHomeFixture(t)
	t.Setenv("AGENTS_HOME", home)

	// Simulate a stale, pre-fix-generated manifest: it declared the global
	// entries alongside the project ones for every scoped field.
	existing := &AgentsRC{
		Version:  1,
		Project:  testProject,
		Skills:   []string{"skill-global", "skill-proj"},
		Rules:    []string{"global", "project"},
		Agents:   []string{"agent-global", "agent-proj"},
		Hooks:    StringsOrBool{All: true},
		MCP:      StringsOrBool{Names: []string{"global-mcp-server", "server-a", "server-b"}},
		Settings: true,
		Sources:  []Source{{Type: testSourceTypeLocal}},
	}

	generated, err := GenerateAgentsRC(testProject, t.TempDir())
	if err != nil {
		t.Fatalf(errFmtGenerateRC, err)
	}
	out := MergeGenerateAgentsRC(existing, generated)

	if !reflect.DeepEqual(out.Skills, []string{"skill-proj"}) {
		t.Errorf("Skills: got %v, want [skill-proj] (stale global entry must be dropped)", out.Skills)
	}
	if !reflect.DeepEqual(out.Agents, []string{"agent-proj"}) {
		t.Errorf("Agents: got %v, want [agent-proj] (stale global entry must be dropped)", out.Agents)
	}
	if !reflect.DeepEqual(out.Rules, []string{"project"}) {
		t.Errorf("Rules: got %v, want [project] (stale global entry must be dropped)", out.Rules)
	}
	gotMCP := append([]string(nil), out.MCP.Names...)
	sort.Strings(gotMCP)
	wantMCP := []string{"server-a", "server-b"}
	if !reflect.DeepEqual(gotMCP, wantMCP) {
		t.Errorf("MCP.Names: got %v, want %v (stale global server must be dropped)", gotMCP, wantMCP)
	}
	if out.MCP.All {
		t.Error("MCP.All should be false after regenerate")
	}
	gotHooks := append([]string(nil), out.Hooks.Names...)
	sort.Strings(gotHooks)
	wantHooks := []string{testHookPostToolUse, testHookPreToolUse}
	if !reflect.DeepEqual(gotHooks, wantHooks) {
		t.Errorf("Hooks.Names: got %v, want %v (stale All:true must be replaced by the project scan)", gotHooks, wantHooks)
	}
	if out.Hooks.All {
		t.Error("Hooks.All should be false after regenerate — project scope only has named events")
	}
	if !out.Settings {
		t.Error("Settings should stay true — driven by the project-scoped cursor.json, not the stale global declaration")
	}
}

// ── GenerateAgentsRC ─────────────────────────────────────────────────────────

// agentsHomeFixture builds a minimal ~/.agents/ tree under tmp and returns its path.
func agentsHomeFixture(t *testing.T) string {
	t.Helper()
	home := t.TempDir()

	mkdirAll := func(parts ...string) string {
		p := filepath.Join(append([]string{home}, parts...)...)
		if err := os.MkdirAll(p, 0755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	writeFile := func(path, content string) {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Skills: global/skill-global (global-only, must NOT be captured),
	// myproject/skill-proj (project-scoped, must be captured)
	writeFile(filepath.Join(mkdirAll("skills", "global", "skill-global"), testSkillMarkerFile), "# skill")
	writeFile(filepath.Join(mkdirAll("skills", testProject, "skill-proj"), testSkillMarkerFile), "# skill")
	// File (not dir) in skills — should be ignored
	writeFile(filepath.Join(home, "skills", "global", "not-a-skill.txt"), "ignore me")

	// Agents: global/agent-global (global-only, must NOT be captured),
	// myproject/agent-proj (project-scoped, must be captured)
	writeFile(filepath.Join(mkdirAll("agents", "global", "agent-global"), "AGENT.md"), "# agent")
	writeFile(filepath.Join(mkdirAll("agents", testProject, "agent-proj"), "AGENT.md"), "# agent")

	// Rules: global file (must NOT be captured) + project file (must be captured)
	writeFile(filepath.Join(home, "rules", "global", "base.md"), "# rule")
	writeFile(filepath.Join(home, "rules", testProject, "custom.md"), "# rule")

	// Hooks: claude-code.json with two non-empty event types
	writeFile(filepath.Join(home, "settings", testProject, "claude-code.json"), `{
		"hooks": {
			"PreToolUse":  [{"command":"echo pre"}],
			"PostToolUse": [{"command":"echo post"}],
			"Notification": []
		}
	}`)

	// MCP: project-scoped mcp.json with two servers
	writeFile(filepath.Join(home, "mcp", testProject, testMCPJSONFile), `{
		"servers": {
			"server-a": {},
			"server-b": {}
		}
	}`)

	// Settings: cursor.json in BOTH global (must NOT be captured on its own)
	// and project (must be captured) scope, so TestGenerateAgentsRCSettings
	// proves the project file — not the global one — drives Settings=true.
	writeFile(filepath.Join(home, "settings", "global", "cursor.json"), "{}")
	writeFile(filepath.Join(home, "settings", testProject, "cursor.json"), "{}")

	return home
}

func TestGenerateAgentsRCSkills(t *testing.T) {
	home := agentsHomeFixture(t)
	t.Setenv("AGENTS_HOME", home)

	rc, err := GenerateAgentsRC(testProject, t.TempDir())
	if err != nil {
		t.Fatalf(errFmtGenerateRC, err)
	}

	// Project-scope only: skill-global exists on disk but must not appear —
	// it auto-resolves at the user level for every project regardless of
	// manifest declaration (see GenerateAgentsRC doc comment).
	want := []string{"skill-proj"}
	if !reflect.DeepEqual(rc.Skills, want) {
		t.Errorf("Skills: got %v, want %v", rc.Skills, want)
	}
}

// TestGenerateAgentsRCSkillsGlobalOnlyNotCaptured covers a project with NO
// project-scoped skill at all: a global-only skill must not leak into the
// generated manifest.
func TestGenerateAgentsRCSkillsGlobalOnlyNotCaptured(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)

	globalSkill := filepath.Join(home, "skills", "global", "skill-global")
	if err := os.MkdirAll(globalSkill, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globalSkill, testSkillMarkerFile), []byte("# skill"), 0644); err != nil {
		t.Fatal(err)
	}

	rc, err := GenerateAgentsRC(testProject, t.TempDir())
	if err != nil {
		t.Fatalf(errFmtGenerateRC, err)
	}

	if len(rc.Skills) != 0 {
		t.Errorf("Skills: got %v, want none (global-only skill must not be captured)", rc.Skills)
	}
}

func TestGenerateAgentsRCAgents(t *testing.T) {
	home := agentsHomeFixture(t)
	t.Setenv("AGENTS_HOME", home)

	rc, err := GenerateAgentsRC(testProject, t.TempDir())
	if err != nil {
		t.Fatalf(errFmtGenerateRC, err)
	}

	// Project-scope only: agent-global exists on disk but must not appear —
	// it auto-resolves at the user level for every project regardless of
	// manifest declaration (see GenerateAgentsRC doc comment).
	if !reflect.DeepEqual(rc.Agents, []string{"agent-proj"}) {
		t.Errorf("Agents: got %v, want [agent-proj]", rc.Agents)
	}
}

// TestGenerateAgentsRCAgentsGlobalOnlyNotCaptured covers a project with NO
// project-scoped agent at all: a global-only agent must not leak into the
// generated manifest.
func TestGenerateAgentsRCAgentsGlobalOnlyNotCaptured(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)

	globalAgent := filepath.Join(home, "agents", "global", "agent-global")
	if err := os.MkdirAll(globalAgent, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globalAgent, "AGENT.md"), []byte("# agent"), 0644); err != nil {
		t.Fatal(err)
	}

	rc, err := GenerateAgentsRC(testProject, t.TempDir())
	if err != nil {
		t.Fatalf(errFmtGenerateRC, err)
	}

	if len(rc.Agents) != 0 {
		t.Errorf("Agents: got %v, want none (global-only agent must not be captured)", rc.Agents)
	}
}

func TestGenerateAgentsRCRules(t *testing.T) {
	home := agentsHomeFixture(t)
	t.Setenv("AGENTS_HOME", home)

	rc, err := GenerateAgentsRC(testProject, t.TempDir())
	if err != nil {
		t.Fatalf(errFmtGenerateRC, err)
	}

	// Project-scope only: the global rule file must not appear.
	if !reflect.DeepEqual(rc.Rules, []string{"project"}) {
		t.Errorf("Rules: got %v, want [project]", rc.Rules)
	}
}

// TestGenerateAgentsRCRulesGlobalOnlyNotCaptured covers a project with NO
// project-scoped rule file: a global-only rule file must not leak into the
// generated manifest (it auto-resolves at the user level for every project).
func TestGenerateAgentsRCRulesGlobalOnlyNotCaptured(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)

	globalRules := filepath.Join(home, "rules", "global")
	if err := os.MkdirAll(globalRules, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globalRules, "base.md"), []byte("# rule"), 0644); err != nil {
		t.Fatal(err)
	}

	rc, err := GenerateAgentsRC(testProject, t.TempDir())
	if err != nil {
		t.Fatalf(errFmtGenerateRC, err)
	}

	if len(rc.Rules) != 0 {
		t.Errorf("Rules: got %v, want none (global-only rule must not be captured)", rc.Rules)
	}
}

func TestGenerateAgentsRCHooksNamedEvents(t *testing.T) {
	home := agentsHomeFixture(t)
	t.Setenv("AGENTS_HOME", home)

	rc, err := GenerateAgentsRC(testProject, t.TempDir())
	if err != nil {
		t.Fatalf(errFmtGenerateRC, err)
	}

	// Only non-empty event arrays should appear; Notification is empty → excluded
	got := rc.Hooks.Names
	sort.Strings(got)
	want := []string{testHookPostToolUse, testHookPreToolUse}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Hooks.Names: got %v, want %v", got, want)
	}
	if rc.Hooks.All {
		t.Error("Hooks.All should be false when specific events are listed")
	}
}

func TestGenerateAgentsRCHooksNoSettings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)

	rc, err := GenerateAgentsRC(testProject, t.TempDir())
	if err != nil {
		t.Fatalf(errFmtGenerateRC, err)
	}

	if rc.Hooks.IsEnabled() {
		t.Errorf("Hooks should be disabled when no settings file exists, got %+v", rc.Hooks)
	}
}

func TestGenerateAgentsRCHooksCanonicalBundlesEnableAll(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)

	bundleDir := filepath.Join(home, "hooks", testProject, "session-orient")
	if err := os.MkdirAll(bundleDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "HOOK.yaml"), []byte("name: session-orient\nwhen: session_start\nrun:\n  command: ./orient.sh\n"), 0644); err != nil {
		t.Fatal(err)
	}

	rc, err := GenerateAgentsRC(testProject, t.TempDir())
	if err != nil {
		t.Fatalf(errFmtGenerateRC, err)
	}
	if !rc.Hooks.All {
		t.Fatalf("Hooks.All = false, want true when project-scoped canonical hook bundles exist; got %+v", rc.Hooks)
	}
	if len(rc.Hooks.Names) != 0 {
		t.Fatalf("Hooks.Names = %v, want empty when Hooks.All is true", rc.Hooks.Names)
	}
}

// TestGenerateAgentsRCHooksCanonicalBundlesGlobalOnlyNotCaptured covers a
// project with NO project-scoped hook bundle: a global-only canonical hook
// bundle must not leak into the generated manifest.
func TestGenerateAgentsRCHooksCanonicalBundlesGlobalOnlyNotCaptured(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)

	bundleDir := filepath.Join(home, "hooks", "global", "session-orient")
	if err := os.MkdirAll(bundleDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "HOOK.yaml"), []byte("name: session-orient\nwhen: session_start\nrun:\n  command: ./orient.sh\n"), 0644); err != nil {
		t.Fatal(err)
	}

	rc, err := GenerateAgentsRC(testProject, t.TempDir())
	if err != nil {
		t.Fatalf(errFmtGenerateRC, err)
	}
	if rc.Hooks.IsEnabled() {
		t.Errorf("Hooks: got %+v, want disabled (global-only bundle must not be captured)", rc.Hooks)
	}
}

// TestGenerateAgentsRCHooksLegacySettingsGlobalOnlyNotCaptured covers a
// project with NO project-scoped settings hooks: global-only legacy
// claude-code.json hooks must not leak into the generated manifest.
func TestGenerateAgentsRCHooksLegacySettingsGlobalOnlyNotCaptured(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)

	settingsDir := filepath.Join(home, "settings", "global")
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatal(err)
	}
	content := `{
		"hooks": {
			"PreToolUse": [{"command":"echo pre"}],
			"PostToolUse": [],
			"Stop": [{"command":"echo stop"}]
		}
	}`
	if err := os.WriteFile(filepath.Join(settingsDir, "claude-code.json"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	rc, err := GenerateAgentsRC(testProject, t.TempDir())
	if err != nil {
		t.Fatalf(errFmtGenerateRC, err)
	}

	if rc.Hooks.IsEnabled() {
		t.Errorf("Hooks: got %+v, want disabled (global-only legacy settings must not be captured)", rc.Hooks)
	}
}

func TestGenerateAgentsRCMCPNamedServers(t *testing.T) {
	home := agentsHomeFixture(t)
	t.Setenv("AGENTS_HOME", home)

	rc, err := GenerateAgentsRC(testProject, t.TempDir())
	if err != nil {
		t.Fatalf(errFmtGenerateRC, err)
	}

	got := rc.MCP.Names
	sort.Strings(got)
	want := []string{"server-a", "server-b"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("MCP.Names: got %v, want %v", got, want)
	}
	if rc.MCP.All {
		t.Error("MCP.All should be false when specific servers are listed")
	}
}

// TestGenerateAgentsRCMCPGlobalOnlyNotCaptured covers a project with NO
// project-scoped MCP config: a global-only MCP server must not leak into
// the generated manifest.
func TestGenerateAgentsRCMCPGlobalOnlyNotCaptured(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)

	// Only global mcp, no project-scoped
	mcpPath := filepath.Join(home, "mcp", "global", testMCPJSONFile)
	os.MkdirAll(filepath.Dir(mcpPath), 0755)
	os.WriteFile(mcpPath, []byte(`{"servers":{"global-srv":{}}}`), 0644)

	rc, err := GenerateAgentsRC(testProject, t.TempDir())
	if err != nil {
		t.Fatalf(errFmtGenerateRC, err)
	}

	if rc.MCP.IsEnabled() {
		t.Errorf("MCP: got %+v, want disabled (global-only server must not be captured)", rc.MCP)
	}
}

func TestGenerateAgentsRCMCPReadsDocumentedMCPServersShape(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)

	mcpPath := filepath.Join(home, "mcp", testProject, testMCPJSONFile)
	if err := os.MkdirAll(filepath.Dir(mcpPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mcpPath, []byte(`{
		"mcpServers": {
			"code-review-graph": {},
			"sonarqube": {}
		}
	}`), 0644); err != nil {
		t.Fatal(err)
	}

	rc, err := GenerateAgentsRC(testProject, t.TempDir())
	if err != nil {
		t.Fatalf(errFmtGenerateRC, err)
	}

	got := rc.MCP.Names
	sort.Strings(got)
	want := []string{"code-review-graph", "sonarqube"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("MCP.Names: got %v, want %v", got, want)
	}
}

// TestGenerateAgentsRCMCPGlobalOnlyNotCapturedDocumentedMCPServersShape is
// the mcpServers-shape sibling of TestGenerateAgentsRCMCPGlobalOnlyNotCaptured.
func TestGenerateAgentsRCMCPGlobalOnlyNotCapturedDocumentedMCPServersShape(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)

	mcpPath := filepath.Join(home, "mcp", "global", testMCPJSONFile)
	if err := os.MkdirAll(filepath.Dir(mcpPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mcpPath, []byte(`{"mcpServers":{"global-srv":{}}}`), 0644); err != nil {
		t.Fatal(err)
	}

	rc, err := GenerateAgentsRC(testProject, t.TempDir())
	if err != nil {
		t.Fatalf(errFmtGenerateRC, err)
	}

	if rc.MCP.IsEnabled() {
		t.Errorf("MCP: got %+v, want disabled (global-only server must not be captured)", rc.MCP)
	}
}

func TestGenerateAgentsRCMCPReadsDotMCPJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)

	mcpPath := filepath.Join(home, "mcp", testProject, ".mcp.json")
	if err := os.MkdirAll(filepath.Dir(mcpPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mcpPath, []byte(`{"mcpServers":{"repo-srv":{}}}`), 0644); err != nil {
		t.Fatal(err)
	}

	rc, err := GenerateAgentsRC(testProject, t.TempDir())
	if err != nil {
		t.Fatalf(errFmtGenerateRC, err)
	}

	if !reflect.DeepEqual(rc.MCP.Names, []string{"repo-srv"}) {
		t.Errorf("MCP.Names: got %v, want [repo-srv]", rc.MCP.Names)
	}
}

func TestGenerateAgentsRCMCPNoConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)

	rc, err := GenerateAgentsRC(testProject, t.TempDir())
	if err != nil {
		t.Fatalf(errFmtGenerateRC, err)
	}

	if rc.MCP.IsEnabled() {
		t.Errorf("MCP should be disabled when no config exists, got %+v", rc.MCP)
	}
}

func TestGenerateAgentsRCSettings(t *testing.T) {
	home := agentsHomeFixture(t)
	t.Setenv("AGENTS_HOME", home)

	rc, err := GenerateAgentsRC(testProject, t.TempDir())
	if err != nil {
		t.Fatalf(errFmtGenerateRC, err)
	}

	if !rc.Settings {
		t.Error("Settings should be true when the project-scoped cursor.json exists")
	}
}

// TestGenerateAgentsRCSettingsGlobalOnlyNotCaptured covers a project with NO
// project-scoped cursor.json: a global-only cursor.json must not flip
// Settings to true.
func TestGenerateAgentsRCSettingsGlobalOnlyNotCaptured(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)

	globalSettings := filepath.Join(home, "settings", "global")
	if err := os.MkdirAll(globalSettings, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globalSettings, "cursor.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	rc, err := GenerateAgentsRC(testProject, t.TempDir())
	if err != nil {
		t.Fatalf(errFmtGenerateRC, err)
	}

	if rc.Settings {
		t.Error("Settings should be false — a global-only cursor.json must not be captured")
	}
}

func TestGenerateAgentsRCSettingsFalse(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)

	rc, err := GenerateAgentsRC(testProject, t.TempDir())
	if err != nil {
		t.Fatalf(errFmtGenerateRC, err)
	}

	if rc.Settings {
		t.Error("Settings should be false when no cursor.json exists")
	}
}

func TestGenerateAgentsRCDefaultFields(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)

	rc, err := GenerateAgentsRC(testProject, t.TempDir())
	if err != nil {
		t.Fatalf(errFmtGenerateRC, err)
	}

	if rc.Version != 1 {
		t.Errorf("Version: got %d, want 1", rc.Version)
	}
	if rc.Project != testProject {
		t.Errorf("Project: got %q, want myproject", rc.Project)
	}
	if len(rc.Sources) != 1 || rc.Sources[0].Type != testSourceTypeLocal {
		t.Errorf("Sources: got %+v, want [{Type:local}]", rc.Sources)
	}
}

func TestGenerateAgentsRCIgnoresNonDirectorySkills(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)

	// Plain file in skills/<project> — should be ignored
	skillsDir := filepath.Join(home, "skills", testProject)
	os.MkdirAll(skillsDir, 0755)
	os.WriteFile(filepath.Join(skillsDir, "not-a-skill"), []byte("text"), 0644)

	// Valid skill dir without the marker file should be ignored
	os.MkdirAll(filepath.Join(skillsDir, "no-marker"), 0755)

	// Valid skill dir WITH the marker file
	os.MkdirAll(filepath.Join(skillsDir, testRealSkillName), 0755)
	os.WriteFile(filepath.Join(skillsDir, testRealSkillName, testSkillMarkerFile), []byte("# s"), 0644)

	rc, err := GenerateAgentsRC(testProject, t.TempDir())
	if err != nil {
		t.Fatalf(errFmtGenerateRC, err)
	}

	if !reflect.DeepEqual(rc.Skills, []string{testRealSkillName}) {
		t.Errorf("Skills: got %v, want [%s]", rc.Skills, testRealSkillName)
	}
}

// TestGenerateAgentsRC_DetectorIOErrors covers the fail-or-full contract: a
// real I/O error (unreadable directory, not "not configured yet") on any
// scoped resource dir must abort GenerateAgentsRC with a non-nil error and a
// nil manifest, never a silently-empty field for that resource type.
func TestGenerateAgentsRC_DetectorIOErrors(t *testing.T) {
	cases := []struct {
		name string
		dir  func(home string) string
	}{
		{"skills", func(home string) string { return filepath.Join(home, "skills", testProject) }},
		{"agents", func(home string) string { return filepath.Join(home, "agents", testProject) }},
		{"hooks", func(home string) string { return filepath.Join(home, "hooks", testProject) }},
		{"mcp", func(home string) string { return filepath.Join(home, "mcp", testProject) }},
		{"settings", func(home string) string { return filepath.Join(home, "settings", testProject) }},
		{"rules", func(home string) string { return filepath.Join(home, "rules", testProject) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			chmodTestSkip(t)
			home := t.TempDir()
			t.Setenv("AGENTS_HOME", home)
			dir := c.dir(home)
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(dir, 0); err != nil {
				t.Fatalf("chmod dir unreadable: %v", err)
			}
			t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

			rc, err := GenerateAgentsRC(testProject, t.TempDir())
			if err == nil {
				t.Fatalf("GenerateAgentsRC with unreadable %s dir: expected non-nil error, got nil (rc=%+v)", c.name, rc)
			}
			if rc != nil {
				t.Errorf("GenerateAgentsRC with unreadable %s dir: expected nil manifest on error, got %+v", c.name, rc)
			}
		})
	}
}

// ── Unknown field round-trip ─────────────────────────────────────────────────

func TestAgentsRCUnknownFieldsRoundtrip(t *testing.T) {
	tmp := t.TempDir()

	// Write a manifest that contains custom keys that AgentsRC does not model.
	// ("refresh" is now a first-class field; use a different key for unknown JSON.)
	input := `{
  "version": 1,
  "project": "myproject",
  "sources": [{"type":"local"}],
  "hooks": false,
  "mcp": false,
  "settings": false,
  "customPolicy": {"interval": "daily", "auto": true},
  "myteam": "platform"
}`
	if err := os.WriteFile(filepath.Join(tmp, AgentsRCFile), []byte(input), 0644); err != nil {
		t.Fatal(err)
	}

	rc, err := LoadAgentsRC(tmp)
	if err != nil {
		t.Fatalf("LoadAgentsRC: %v", err)
	}

	// Known fields intact
	if rc.Project != "myproject" {
		t.Errorf("Project: got %q, want %q", rc.Project, "myproject")
	}
	// Extra fields captured
	if len(rc.ExtraFields) != 2 {
		t.Fatalf("ExtraFields: got %d keys, want 2; keys: %v", len(rc.ExtraFields), rc.ExtraFields)
	}
	if _, ok := rc.ExtraFields["customPolicy"]; !ok {
		t.Error("ExtraFields missing 'customPolicy'")
	}
	if _, ok := rc.ExtraFields["myteam"]; !ok {
		t.Error("ExtraFields missing 'myteam'")
	}

	// Mutate a known field and save
	rc.Project = "renamed"
	if err := rc.Save(tmp); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Reload and check both the mutation and the extra fields survived
	rc2, err := LoadAgentsRC(tmp)
	if err != nil {
		t.Fatalf("LoadAgentsRC after save: %v", err)
	}
	if rc2.Project != "renamed" {
		t.Errorf("Project after save: got %q, want %q", rc2.Project, "renamed")
	}
	if len(rc2.ExtraFields) != 2 {
		t.Fatalf("ExtraFields after save: got %d keys, want 2; keys: %v", len(rc2.ExtraFields), rc2.ExtraFields)
	}
	if _, ok := rc2.ExtraFields["customPolicy"]; !ok {
		t.Error("ExtraFields after save missing 'customPolicy'")
	}
	var policyVal map[string]any
	if err := json.Unmarshal(rc2.ExtraFields["customPolicy"], &policyVal); err != nil {
		t.Fatalf("unmarshal customPolicy extra field: %v", err)
	}
	if policyVal["interval"] != "daily" {
		t.Errorf("customPolicy.interval: got %v, want 'daily'", policyVal["interval"])
	}
}

func TestAgentsRCKnownFieldsNotDuplicated(t *testing.T) {
	// Known fields must not appear in ExtraFields even if the JSON has a key
	// collision (e.g. someone accidentally writes "version" twice — last wins
	// in Go's json.Unmarshal, and it should stay in the known slot only).
	tmp := t.TempDir()
	input := `{"version":1,"project":"p","sources":[{"type":"local"}],"hooks":false,"mcp":false,"settings":false}`
	if err := os.WriteFile(filepath.Join(tmp, AgentsRCFile), []byte(input), 0644); err != nil {
		t.Fatal(err)
	}
	rc, err := LoadAgentsRC(tmp)
	if err != nil {
		t.Fatalf("LoadAgentsRC: %v", err)
	}
	if len(rc.ExtraFields) != 0 {
		t.Errorf("ExtraFields should be empty for a manifest with only known keys, got: %v", rc.ExtraFields)
	}
}

// ── JSON shape produced by Save ───────────────────────────────────────────────

func TestAgentsRCJSONShape(t *testing.T) {
	tmp := t.TempDir()
	rc := &AgentsRC{
		Schema:  "https://agorcha.dev/schemas/agentsrc.schema.json",
		Version: 1,
		Project: "proj",
		Skills:  []string{"s1"},
		Hooks:   StringsOrBool{Names: []string{testHookPreToolUse}},
		MCP:     StringsOrBool{All: false},
		Sources: []Source{{Type: testSourceTypeLocal}},
	}
	if err := rc.Save(tmp); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(filepath.Join(tmp, AgentsRCFile))
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("saved file is not valid JSON: %v", err)
	}

	// hooks should be an array
	if _, ok := raw["hooks"].([]any); !ok {
		t.Errorf("hooks should be JSON array, got %T", raw["hooks"])
	}
	// mcp should be false (not enabled)
	if v, ok := raw["mcp"].(bool); !ok || v {
		t.Errorf("mcp should be JSON false, got %v", raw["mcp"])
	}
	// $schema present
	if raw["$schema"] == nil {
		t.Error("$schema should be present")
	}
}

// ── AgentsRCKG ────────────────────────────────────────────────────────────────

func TestAgentsRCKG_Unmarshal(t *testing.T) {
	raw := `{
		"version": 1,
		"project": "demo",
		"sources": [{"type": "local"}],
		"kg": {
			"graph_home": "/custom/kg",
			"backend": "sqlite",
			"bridge": {"enabled": true, "allowed_intents": ["symbol_lookup"]}
		}
	}`
	var rc AgentsRC
	if err := json.Unmarshal([]byte(raw), &rc); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if rc.KG == nil {
		t.Fatal("expected KG to be non-nil")
	}
	if rc.KG.GraphHome != "/custom/kg" {
		t.Errorf("GraphHome: got %q", rc.KG.GraphHome)
	}
	if rc.KG.Backend != "sqlite" {
		t.Errorf("Backend: got %q", rc.KG.Backend)
	}
	if !rc.KG.Bridge.Enabled {
		t.Error("expected Bridge.Enabled = true")
	}
	if len(rc.KG.Bridge.AllowedIntents) != 1 || rc.KG.Bridge.AllowedIntents[0] != "symbol_lookup" {
		t.Errorf("AllowedIntents: got %v", rc.KG.Bridge.AllowedIntents)
	}
}

func TestAgentsRCKG_NilWhenAbsent(t *testing.T) {
	raw := `{"version":1,"project":"demo","sources":[{"type":"local"}]}`
	var rc AgentsRC
	if err := json.Unmarshal([]byte(raw), &rc); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if rc.KG != nil {
		t.Errorf("expected KG nil when absent, got %+v", rc.KG)
	}
}

func TestAgentsRCKG_MarshalRoundTrip(t *testing.T) {
	rc := &AgentsRC{
		Version: 1,
		Project: "demo",
		Sources: []Source{{Type: testSourceTypeLocal}},
		KG: &AgentsRCKG{
			GraphHome: "/my/graph",
			Backend:   "sqlite",
			Bridge:    AgentsRCKGBridge{Enabled: true},
		},
	}
	data, err := json.Marshal(rc)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var rc2 AgentsRC
	if err := json.Unmarshal(data, &rc2); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if rc2.KG == nil {
		t.Fatal("KG nil after round-trip")
	}
	if rc2.KG.GraphHome != "/my/graph" {
		t.Errorf("GraphHome after round-trip: got %q", rc2.KG.GraphHome)
	}
	if !rc2.KG.Bridge.Enabled {
		t.Error("Bridge.Enabled false after round-trip")
	}
}

func TestUnmarshalJSON_InvalidCore(t *testing.T) {
	var rc AgentsRC
	if err := rc.UnmarshalJSON([]byte("not json")); err == nil {
		t.Error("expected error from json.Unmarshal core")
	}
}

func TestMarshalJSON_OverlapWithExtraFieldsDoesNotOverwriteKnown(t *testing.T) {
	rc := &AgentsRC{
		Version: 1,
		Project: "p",
		Sources: []Source{{Type: "local"}},
		ExtraFields: map[string]json.RawMessage{

			"$schema": json.RawMessage(`"OVERWRITE-ATTEMPT"`),
			"team":    json.RawMessage(`"platform"`),
		},
	}
	rc.Schema = "https://agorcha.dev/schemas/agentsrc.schema.json"
	data, err := json.Marshal(rc)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw["$schema"]), "agorcha.dev") {
		t.Errorf("$schema overwritten by ExtraFields: %s", raw["$schema"])
	}
	if string(raw["team"]) != `"platform"` {
		t.Errorf("custom field missing: %s", raw["team"])
	}
}

func TestSaveAgentsRC_BadPath(t *testing.T) {
	rc := &AgentsRC{Version: 1, Project: "p", Sources: []Source{{Type: "local"}}}

	tmp := t.TempDir()
	regular := filepath.Join(tmp, "regular")
	os.WriteFile(regular, []byte("x"), 0644)
	if err := rc.Save(filepath.Join(regular, "sub")); err == nil {
		t.Error("expected error saving under non-dir parent")
	}
}

func TestMergeGenerateAgentsRC_GenLocalSourceDeduplicatedAcrossSlices(t *testing.T) {
	g := &AgentsRC{Sources: []Source{{Type: "local"}, {Type: "local"}}}
	e := &AgentsRC{Sources: []Source{{Type: "local"}}}
	out := MergeGenerateAgentsRC(e, g)
	if len(out.Sources) != 1 {
		t.Errorf("expected dedupe to 1 local, got %v", out.Sources)
	}
}

func TestSourceMergeKeyUnknownType(t *testing.T) {
	a := Source{Type: "custom", Path: "/x", URL: "u", Ref: "r"}
	b := Source{Type: "custom", Path: "/x", URL: "u", Ref: "r"}
	c := Source{Type: "custom", Path: "/y"}
	in := []Source{a, b, c}
	out := mergeSourceSlices(in, nil)
	if len(out) != 2 {
		t.Errorf("expected 2 unique custom sources, got %d: %v", len(out), out)
	}
}

func TestDetectHookEvents_ProjectYAMLBundleEnablesAll(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "hooks", "myproj", "b1")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "HOOK.yaml"), []byte("name: b1\n"), 0644)
	got, err := detectHookEvents(home, "myproj")
	if err != nil {
		t.Fatalf("detectHookEvents: %v", err)
	}
	if !got.All {
		t.Errorf("expected All=true with project-scoped yaml bundle, got %+v", got)
	}
}

// TestDetectHookEvents_GlobalYAMLBundleNotCaptured proves a global-only
// bundle (no project-scoped bundle) does not enable Hooks — it auto-resolves
// at the user level for every project regardless of manifest declaration.
func TestDetectHookEvents_GlobalYAMLBundleNotCaptured(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "hooks", "global", "b1")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "HOOK.yaml"), []byte("name: b1\n"), 0644)
	got, err := detectHookEvents(home, "myproj")
	if err != nil {
		t.Fatalf("detectHookEvents: %v", err)
	}
	if got.IsEnabled() {
		t.Errorf("expected disabled with global-only yaml bundle, got %+v", got)
	}
}

func TestDetectHookEvents_None(t *testing.T) {
	home := t.TempDir()
	got, err := detectHookEvents(home, "p")
	if err != nil {
		t.Fatalf("detectHookEvents: %v", err)
	}
	if got.IsEnabled() {
		t.Errorf("expected disabled, got %+v", got)
	}
}

func TestDetectSettingsHookEvents_BadJSON(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "settings", "global")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "claude-code.json"), []byte("not json"), 0644)
	got, err := detectSettingsHookEvents(home, "global")
	if err != nil {
		t.Fatalf("detectSettingsHookEvents: %v", err)
	}
	if got.IsEnabled() {
		t.Errorf("bad json: got %+v", got)
	}
}

func TestDetectSettingsHookEvents_HooksNotMap(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "settings", "global")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "claude-code.json"), []byte(`{"hooks":"not a map"}`), 0644)
	got, err := detectSettingsHookEvents(home, "global")
	if err != nil {
		t.Fatalf("detectSettingsHookEvents: %v", err)
	}
	if got.IsEnabled() {
		t.Error("expected disabled when hooks is not a map")
	}
}

func TestDetectSettingsHookEvents_NoHooksKey(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "settings", "global")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "claude-code.json"), []byte(`{}`), 0644)
	got, err := detectSettingsHookEvents(home, "global")
	if err != nil {
		t.Fatalf("detectSettingsHookEvents: %v", err)
	}
	if got.IsEnabled() {
		t.Error("expected disabled when no hooks key")
	}
}

func TestReadMCPScope_BadJSON(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "mcp", "global")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "mcp.json"), []byte("not json"), 0644)
	got, err := readMCPScope(home, "global")
	if err != nil {
		t.Fatalf("readMCPScope: %v", err)
	}
	if got.IsEnabled() {
		t.Errorf("bad json: got %+v", got)
	}
}

func TestReadMCPScope_NoServersKey(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "mcp", "global")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "mcp.json"), []byte(`{"other":"value"}`), 0644)
	got, err := readMCPScope(home, "global")
	if err != nil {
		t.Fatalf("readMCPScope: %v", err)
	}
	if got.IsEnabled() {
		t.Error("expected disabled when no servers key")
	}
}

func TestReadMCPScope_EmptyServers(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "mcp", "global")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "mcp.json"), []byte(`{"servers":{}}`), 0644)
	got, err := readMCPScope(home, "global")
	if err != nil {
		t.Fatalf("readMCPScope: %v", err)
	}
	if got.IsEnabled() {
		t.Error("expected disabled when servers map is empty")
	}
}

func TestReadMCPScope_NoFiles(t *testing.T) {
	home := t.TempDir()
	got, err := readMCPScope(home, "global")
	if err != nil {
		t.Fatalf("readMCPScope: %v", err)
	}
	if got.IsEnabled() {
		t.Error("expected disabled when scope has no files")
	}
}

func TestDetectRuleScopes_OnlyOtherExt(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "rules", "proj")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "weird.json"), []byte("{}"), 0644)
	got, err := detectRuleScopes(home, "proj")
	if err != nil {
		t.Fatalf("detectRuleScopes: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected none (no .md/.mdc/.txt project rule file), got %v", got)
	}
}

func TestDetectRuleScopes_MissingDir(t *testing.T) {
	home := t.TempDir()
	got, err := detectRuleScopes(home, "missing-proj")
	if err != nil {
		t.Fatalf("detectRuleScopes: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected none (no project rules dir), got %v", got)
	}
}

func TestHasYAMLHooks_DirAbsent(t *testing.T) {
	got, err := hasYAMLHooks("/path/that/does/not/exist")
	if err != nil {
		t.Fatalf("hasYAMLHooks: %v", err)
	}
	if got {
		t.Error("missing dir should return false")
	}
}

func TestHasYAMLHooks_FileInDirIgnored(t *testing.T) {
	tmp := t.TempDir()

	os.WriteFile(filepath.Join(tmp, "stray.yaml"), []byte("x"), 0644)
	got, err := hasYAMLHooks(tmp)
	if err != nil {
		t.Fatalf("hasYAMLHooks: %v", err)
	}
	if got {
		t.Error("non-dir entry should be ignored")
	}
}

// ── v2 LayerRef / PackageRef unit tests ──────────────────────────────────────

func TestLayerRef_UnmarshalString(t *testing.T) {
	var r LayerRef
	if err := json.Unmarshal([]byte(`"acme:org/base"`), &r); err != nil {
		t.Fatalf("unmarshal string: %v", err)
	}
	if r.Ref != "acme:org/base" || r.Optional {
		t.Errorf("got %+v", r)
	}
}

func TestLayerRef_UnmarshalObject(t *testing.T) {
	var r LayerRef
	if err := json.Unmarshal([]byte(`{"ref":"acme:team/experimental","optional":true}`), &r); err != nil {
		t.Fatalf("unmarshal object: %v", err)
	}
	if r.Ref != "acme:team/experimental" || !r.Optional {
		t.Errorf("got %+v", r)
	}
}

func TestLayerRef_UnmarshalRejectsBadShape(t *testing.T) {
	var r LayerRef
	if err := json.Unmarshal([]byte(`123`), &r); err == nil {
		t.Error("expected error for numeric extends entry")
	}
	if err := json.Unmarshal([]byte(`{"optional":true}`), &r); err == nil {
		t.Error("expected error for object form missing ref")
	}
}

func TestLayerRef_MarshalStringForm(t *testing.T) {
	r := LayerRef{Ref: "acme:org/base"}
	out, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `"acme:org/base"` {
		t.Errorf("expected compact string form, got %s", out)
	}
}

func TestLayerRef_MarshalObjectFormWhenOptional(t *testing.T) {
	r := LayerRef{Ref: "acme:team/experimental", Optional: true}
	out, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"optional":true`) {
		t.Errorf("expected object form with optional flag, got %s", out)
	}
}

func TestLayerRef_RoundTripStability(t *testing.T) {
	cases := []LayerRef{
		{Ref: "acme:org/base"},
		{Ref: "acme:team/frontend@v1.2.3"},
		{Ref: "acme:team/experimental", Optional: true},
	}
	for _, orig := range cases {
		data, err := json.Marshal(orig)
		if err != nil {
			t.Fatalf("marshal %+v: %v", orig, err)
		}
		var rt LayerRef
		if err := json.Unmarshal(data, &rt); err != nil {
			t.Fatalf("unmarshal %s: %v", data, err)
		}
		if rt != orig {
			t.Errorf("round-trip drift: orig=%+v rt=%+v (data=%s)", orig, rt, data)
		}
	}
}

func TestPackageRef_UnmarshalString(t *testing.T) {
	var p PackageRef
	if err := json.Unmarshal([]byte(`"acme-pkgs:skill/review-pr@^1.2"`), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.Ref != "acme-pkgs:skill/review-pr@^1.2" {
		t.Errorf("got %+v", p)
	}
}

func TestPackageRef_UnmarshalObject(t *testing.T) {
	var p PackageRef
	if err := json.Unmarshal([]byte(`{"ref":"acme-pkgs:verifier/x@1.0.0"}`), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.Ref != "acme-pkgs:verifier/x@1.0.0" {
		t.Errorf("got %+v", p)
	}
}

func TestPackageRef_UnmarshalRejectsBadShape(t *testing.T) {
	var p PackageRef
	if err := json.Unmarshal([]byte(`true`), &p); err == nil {
		t.Error("expected error for boolean packages entry")
	}
	if err := json.Unmarshal([]byte(`{}`), &p); err == nil {
		t.Error("expected error for object form missing ref")
	}
}

func TestPackageRef_MarshalAlwaysString(t *testing.T) {
	p := PackageRef{Ref: "acme-pkgs:skill/review-pr@pinned:sha256:abc"}
	out, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `"acme-pkgs:skill/review-pr@pinned:sha256:abc"` {
		t.Errorf("expected string form, got %s", out)
	}
}

// ── PromptFileRef: source-aware prompt_files (config-v2 Q1, Option B) ─────────

func TestPromptFileRef_UnmarshalLegacyString(t *testing.T) {
	var r PromptFileRef
	if err := json.Unmarshal([]byte(`"verifiers/unit.md"`), &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if r.Path != "verifiers/unit.md" || r.Source != "" || r.Version != "" {
		t.Errorf("legacy string decode wrong: %+v", r)
	}
}

func TestPromptFileRef_UnmarshalTypedObject(t *testing.T) {
	var r PromptFileRef
	if err := json.Unmarshal([]byte(`{"source":"acme","path":"verifiers/cli-runner.md","version":"v3"}`), &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if r.Source != "acme" || r.Path != "verifiers/cli-runner.md" || r.Version != "v3" {
		t.Errorf("typed decode wrong: %+v", r)
	}
}

func TestPromptFileRef_UnmarshalRejectsBadShape(t *testing.T) {
	var r PromptFileRef
	if err := json.Unmarshal([]byte(`42`), &r); err == nil {
		t.Error("expected error for numeric prompt_files entry")
	}
	if err := json.Unmarshal([]byte(`""`), &r); err == nil {
		t.Error("expected error for empty-string entry")
	}
	if err := json.Unmarshal([]byte(`{"source":"acme"}`), &r); err == nil {
		t.Error("expected error for object form missing path")
	}
	if err := json.Unmarshal([]byte(`{`), &r); err == nil {
		t.Error("expected error for malformed JSON entry")
	}
}

func TestPromptFileRef_MarshalCompactWhenPathOnly(t *testing.T) {
	r := PromptFileRef{Path: "verifiers/unit.md"}
	out, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `"verifiers/unit.md"` {
		t.Errorf("path-only ref should marshal to bare string, got %s", out)
	}
}

func TestPromptFileRef_MarshalObjectWhenSourceOrVersion(t *testing.T) {
	for _, r := range []PromptFileRef{
		{Source: "acme", Path: "p.md"},
		{Path: "p.md", Version: "v2"},
		{Source: "acme", Path: "p.md", Version: "v2"},
	} {
		out, err := json.Marshal(r)
		if err != nil {
			t.Fatalf("marshal %+v: %v", r, err)
		}
		var back PromptFileRef
		if err := json.Unmarshal(out, &back); err != nil {
			t.Fatalf("round-trip unmarshal %s: %v", out, err)
		}
		if back != r {
			t.Errorf("round-trip drift: got %+v want %+v (wire %s)", back, r, out)
		}
	}
}

// TestStageProfiles_NewKeyRoundTrip proves the unified stage_profiles map routes
// to the typed AgentsRC field (not ExtraFields) across all four stages and
// preserves a mixed legacy/typed prompt_files list across a marshal/unmarshal
// cycle.
func TestStageProfiles_NewKeyRoundTrip(t *testing.T) {
	in := []byte(`{
  "version": 1,
  "sources": [{"type": "local"}],
  "stage_profiles": {
    "executor": {"default": {"label": "Executor", "prompt_files": ["executors/base.md"]}},
    "verifier": {
      "cli-runner": {
        "label": "CLI runner",
        "prompt_files": [
          "verifiers/verifier.base.md",
          {"source": "acme", "path": "verifiers/cli-runner.md", "version": "v2"}
        ]
      }
    },
    "reviewer": {"adversarial": {"label": "Adversarial", "prompt_files": ["reviewers/adversarial.md"]}},
    "orchestrator": {"default": {"label": "Orchestrator", "prompt_files": ["orchestrators/base.md"]}}
  }
}`)
	var rc AgentsRC
	if err := json.Unmarshal(in, &rc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rc.ExtraFields["stage_profiles"] != nil {
		t.Fatalf("stage_profiles leaked into ExtraFields instead of the typed field")
	}
	prof := rc.StageProfiles["verifier"]["cli-runner"]
	if prof.Label != "CLI runner" || len(prof.PromptFiles) != 2 {
		t.Fatalf("verifier profile decode wrong: %+v", prof)
	}
	if prof.PromptFiles[0].Source != "" || prof.PromptFiles[0].Path != "verifiers/verifier.base.md" {
		t.Errorf("legacy entry wrong: %+v", prof.PromptFiles[0])
	}
	if prof.PromptFiles[1].Source != "acme" || prof.PromptFiles[1].Version != "v2" {
		t.Errorf("typed entry wrong: %+v", prof.PromptFiles[1])
	}
	for _, stage := range []string{"executor", "reviewer", "orchestrator"} {
		if len(rc.StageProfiles[stage]) != 1 {
			t.Errorf("stage %q not decoded: %+v", stage, rc.StageProfiles[stage])
		}
	}

	out, err := json.Marshal(rc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var rc2 AgentsRC
	if err := json.Unmarshal(out, &rc2); err != nil {
		t.Fatalf("re-unmarshal: %v", err)
	}
	if rc2.StageProfiles["verifier"]["cli-runner"].PromptFiles[1].Source != "acme" {
		t.Fatalf("round-trip lost typed prompt provenance: %+v", rc2.StageProfiles)
	}
}

// TestStageProfiles_LegacyFold proves the three deprecated keys fold into the
// unified model on read, are never re-emitted, and that an explicit new-key value
// wins over a colliding legacy one.
func TestStageProfiles_LegacyFold(t *testing.T) {
	in := []byte(`{
  "version": 1,
  "sources": [{"type": "local"}],
  "verifier_profiles": {
    "unit": {"label": "legacy-unit", "prompt_files": ["verifiers/unit.md"]},
    "cli-runner": {"label": "CLI runner", "prompt_files": ["verifiers/cli-runner.md"]}
  },
  "reviewer_profiles": {
    "adversarial": {"label": "Adversarial", "prompt_files": ["reviewers/adversarial.md"]}
  },
  "app_type_verifier_map": {"go-cli": ["unit", "cli-runner"], "skip-me": []},
  "stage_profiles": {"verifier": {"unit": {"label": "new-unit", "prompt_files": ["verifiers/unit.new.md"]}}}
}`)
	var rc AgentsRC
	if err := json.Unmarshal(in, &rc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// New key wins on collision; legacy fills the gap.
	if rc.StageProfiles["verifier"]["unit"].Label != "new-unit" {
		t.Errorf("new-key value should win on collision: %+v", rc.StageProfiles["verifier"]["unit"])
	}
	if rc.StageProfiles["verifier"]["cli-runner"].Label != "CLI runner" {
		t.Errorf("legacy verifier profile not folded: %+v", rc.StageProfiles["verifier"])
	}
	if rc.StageProfiles["reviewer"]["adversarial"].PromptFiles[0].Path != "reviewers/adversarial.md" {
		t.Errorf("legacy reviewer profile not folded: %+v", rc.StageProfiles["reviewer"])
	}
	// app_type_verifier_map folds into execution_profile topology; empty seq is skipped.
	seq := rc.ExecutionProfile.ByAppType["go-cli"].Topology.VerifierSequence
	if len(seq) != 2 || seq[0] != "unit" || seq[1] != "cli-runner" {
		t.Errorf("app_type_verifier_map not folded into topology: %+v", seq)
	}
	if _, ok := rc.ExecutionProfile.ByAppType["skip-me"]; ok {
		t.Errorf("empty legacy verifier sequence should not create a by_app_type entry")
	}
	// Legacy keys are never re-emitted.
	out, err := json.Marshal(rc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, k := range []string{"verifier_profiles", "reviewer_profiles", "app_type_verifier_map"} {
		if bytes.Contains(out, []byte(`"`+k+`"`)) {
			t.Errorf("deprecated key %q re-emitted on marshal: %s", k, out)
		}
	}
}

// TestMergeGenerateAgentsRCPreservesStageProfiles guards the ExtraFields-guard
// breakage: now that stage profiles are a typed field, regeneration must carry a
// committed set over (it no longer rides along in ExtraFields).
func TestMergeGenerateAgentsRCPreservesStageProfiles(t *testing.T) {
	existing := &AgentsRC{
		Version: 1,
		Sources: []Source{{Type: testSourceTypeLocal}},
		StageProfiles: map[string]map[string]StageProfile{
			"verifier": {"unit": {Label: "Unit", PromptFiles: []PromptFileRef{{Path: "verifiers/unit.md"}}}},
			"reviewer": {"adversarial": {Label: "Adversarial", PromptFiles: []PromptFileRef{{Source: "acme", Path: "reviewers/adversarial.md", Version: "v1"}}}},
		},
	}
	generated := &AgentsRC{
		Version: 1,
		Sources: []Source{{Type: testSourceTypeLocal}},
		Skills:  []string{"s"},
	}
	out := MergeGenerateAgentsRC(existing, generated)
	if out.StageProfiles["verifier"]["unit"].PromptFiles[0].Path != "verifiers/unit.md" {
		t.Errorf("verifier stage profiles not preserved: %+v", out.StageProfiles)
	}
	if out.StageProfiles["reviewer"]["adversarial"].PromptFiles[0].Source != "acme" {
		t.Errorf("reviewer stage profiles not preserved: %+v", out.StageProfiles)
	}
	// Deep copy: mutating the source must not bleed into the merged result.
	existing.StageProfiles["verifier"]["unit"].PromptFiles[0].Path = "MUTATED"
	if out.StageProfiles["verifier"]["unit"].PromptFiles[0].Path == "MUTATED" {
		t.Error("cloneStageProfiles aliased the source slice")
	}
}

// TestCloneStageProfilesEmpty covers the empty/nil short-circuit.
func TestCloneStageProfilesEmpty(t *testing.T) {
	if got := cloneStageProfiles(nil); got != nil {
		t.Errorf("expected nil for empty map, got %+v", got)
	}
}

// preconditionPoliciesFixture is a sample manifest exercising the top-level
// precondition_policies registry plus a stage profile that references one by
// name (verifier-precondition-policy plan, Slice B).
const preconditionPoliciesFixture = `{
  "version": 1,
  "sources": [{"type": "local"}],
  "precondition_policies": {
    "default": {
      "predicates": [
        {"signal": "event.pr.open"},
        {"signal": "signal.ci.rollup", "args": {"equals": "green"}}
      ]
    }
  },
  "stage_profiles": {
    "verifier": {
      "cli-runner": {
        "label": "CLI runner",
        "prompt_files": ["verifiers/verifier.base.md"],
        "precondition_policy": "default"
      }
    }
  }
}`

// TestPreconditionPolicies_RoundTrip proves the registry + the stage-profile
// reference route to the typed fields and survive a marshal/unmarshal cycle.
func TestPreconditionPolicies_RoundTrip(t *testing.T) {
	var rc AgentsRC
	if err := json.Unmarshal([]byte(preconditionPoliciesFixture), &rc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	pol := rc.PreconditionPolicies["default"]
	if len(pol.Predicates) != 2 {
		t.Fatalf("predicates decode wrong: %+v", pol)
	}
	if pol.Predicates[0].Signal != "event.pr.open" {
		t.Errorf("predicate[0] signal wrong: %+v", pol.Predicates[0])
	}
	if pol.Predicates[1].Args["equals"] != "green" {
		t.Errorf("predicate[1] args wrong: %+v", pol.Predicates[1])
	}
	if got := rc.StageProfiles["verifier"]["cli-runner"].PreconditionPolicy; got != "default" {
		t.Errorf("stage-profile precondition_policy ref lost: %q", got)
	}

	out, err := json.Marshal(rc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var rc2 AgentsRC
	if err := json.Unmarshal(out, &rc2); err != nil {
		t.Fatalf("re-unmarshal: %v", err)
	}
	if !reflect.DeepEqual(rc.PreconditionPolicies, rc2.PreconditionPolicies) {
		t.Errorf("registry round-trip drift: %+v vs %+v", rc.PreconditionPolicies, rc2.PreconditionPolicies)
	}
	if rc2.StageProfiles["verifier"]["cli-runner"].PreconditionPolicy != "default" {
		t.Errorf("round-trip lost stage-profile precondition_policy ref: %+v", rc2.StageProfiles)
	}
}

// TestPreconditionPolicies_ExtraFieldsGuard confirms the registry key routes to
// the typed field, never ExtraFields ([[schema-usage]] guard).
func TestPreconditionPolicies_ExtraFieldsGuard(t *testing.T) {
	var rc AgentsRC
	if err := json.Unmarshal([]byte(preconditionPoliciesFixture), &rc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, leaked := rc.ExtraFields["precondition_policies"]; leaked {
		t.Errorf("precondition_policies leaked into ExtraFields instead of the typed field")
	}
	// An unknown sibling key still lands in ExtraFields (guard not over-broad).
	var rc2 AgentsRC
	withUnknown := `{"version":1,"sources":[{"type":"local"}],"made_up_key":{"x":1}}`
	if err := json.Unmarshal([]byte(withUnknown), &rc2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rc2.ExtraFields["made_up_key"] == nil {
		t.Errorf("unknown-key guard regressed: made_up_key not captured")
	}
}

// TestPreconditionPolicies_SchemaValidates confirms the sample manifest passes
// the compiled agentsrc.schema.json (the new property + $defs are well-formed).
func TestPreconditionPolicies_SchemaValidates(t *testing.T) {
	sch := compileAgentsRCSchema(t)
	var doc any
	if err := json.Unmarshal([]byte(preconditionPoliciesFixture), &doc); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	if err := sch.Validate(doc); err != nil {
		t.Fatalf("precondition_policies sample must validate: %v", err)
	}
}

// TestV2_AgentsRCMarshalUnmarshalRoundTrip ensures Marshal then Unmarshal
// of a fully-populated v2 AgentsRC preserves every additive field.
func TestV2_AgentsRCMarshalUnmarshalRoundTrip(t *testing.T) {
	orig := &AgentsRC{
		Version:  2,
		Project:  "p",
		RepoID:   "github.com/acme/p",
		Hooks:    StringsOrBool{All: false},
		MCP:      StringsOrBool{All: false},
		Settings: false,
		Sources: []Source{
			{ID: "local-src", Type: "local"},
			{ID: "acme-pkgs", Type: "oci", URL: "oci://example/repo",
				Auth: json.RawMessage(`{"provider":"credential-helper"}`)},
		},
		Extends: []LayerRef{
			{Ref: "acme:org/base"},
			{Ref: "acme:team/experimental", Optional: true},
		},
		Packages: []PackageRef{
			{Ref: "acme-pkgs:skill/review-pr@^1.2"},
		},
		Features: map[string]string{"graph_bridge": "preview"},
	}
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got AgentsRC
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.RepoID != orig.RepoID {
		t.Errorf("RepoID drift: %q vs %q", got.RepoID, orig.RepoID)
	}
	if len(got.Extends) != len(orig.Extends) {
		t.Fatalf("Extends len drift: %d vs %d", len(got.Extends), len(orig.Extends))
	}
	for i := range orig.Extends {
		if got.Extends[i] != orig.Extends[i] {
			t.Errorf("Extends[%d] drift: %+v vs %+v", i, got.Extends[i], orig.Extends[i])
		}
	}
	if len(got.Packages) != len(orig.Packages) || got.Packages[0] != orig.Packages[0] {
		t.Errorf("Packages drift: %+v vs %+v", got.Packages, orig.Packages)
	}
	if got.Features["graph_bridge"] != "preview" {
		t.Errorf("Features lost: %+v", got.Features)
	}
	// Source v2 fields
	if got.Sources[1].Type != "oci" || got.Sources[1].ID != "acme-pkgs" {
		t.Errorf("Source[1] v2 fields lost: %+v", got.Sources[1])
	}
	if string(got.Sources[1].Auth) != `{"provider":"credential-helper"}` {
		t.Errorf("Source[1].Auth not preserved verbatim: %s", got.Sources[1].Auth)
	}
}

// TestV1_LoadFromTestdataPreservesShape loads the v1 fixture via LoadAgentsRC
// and confirms that re-saving it does not introduce any v2 keys. This is the
// disk-level byte-stability guarantee for additive migration.
func TestV1_LoadFromTestdataPreservesShape(t *testing.T) {
	// Copy fixture into a TempDir as .agentsrc.json so LoadAgentsRC finds it.
	tmp := t.TempDir()
	src := filepath.Join("testdata", "v1", AgentsRCFile)
	srcData, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, AgentsRCFile), srcData, 0644); err != nil {
		t.Fatal(err)
	}
	rc, err := LoadAgentsRC(tmp)
	if err != nil {
		t.Fatalf("LoadAgentsRC: %v", err)
	}
	if rc.Version != 1 {
		t.Errorf("Version: got %d, want 1", rc.Version)
	}
	if rc.RepoID != "" || rc.Extends != nil || rc.Packages != nil || rc.Features != nil {
		t.Errorf("v1 load populated v2 fields: %+v", rc)
	}

	// Re-save and confirm output contains no v2 keys.
	if err := rc.Save(tmp); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out, err := os.ReadFile(filepath.Join(tmp, AgentsRCFile))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{`"repo_id"`, `"extends"`, `"packages"`, `"features"`,
		`"cache_ttl"`, `"id"`} {
		if strings.Contains(string(out), forbidden) {
			t.Errorf("v1 re-save leaked v2 key %s: %s", forbidden, out)
		}
	}
}

func TestSourceMergeKey_AllBranches(t *testing.T) {
	if k := sourceMergeKey(Source{Type: "local", Path: "/x"}); k != "local:/x" {
		t.Errorf("local: %q", k)
	}
	if k := sourceMergeKey(Source{Type: "git", URL: "u", Ref: "r"}); k != "git:u\x00r" {
		t.Errorf("git: %q", k)
	}
	if k := sourceMergeKey(Source{Type: "custom"}); !strings.HasPrefix(k, "type:custom") {
		t.Errorf("default: %q", k)
	}
}

// ── repo_id derivation (org-config-resolution §5) ────────────────────────────

// withGitRemoteOriginURL temporarily replaces the gitRemoteOriginURL seam
// for a single test and restores it on cleanup.
func withGitRemoteOriginURL(t *testing.T, fn func(string) (string, error)) {
	t.Helper()
	prev := gitRemoteOriginURL
	gitRemoteOriginURL = fn
	t.Cleanup(func() { gitRemoteOriginURL = prev })
}

// URL-parsing matrix coverage (SCP, ssh://, https://, http://, git://,
// embedded user[:token]@, ports, .git suffix, trailing slash, uppercase
// host, nested groups, empty/whitespace/junk/bare-path fallbacks) lives
// in internal/gitremote/gitremote_test.go — the canonical helper this
// package delegates to. The DeriveRepoIDFromGit_* tests below cover the
// seam end-to-end with a representative sample.

func TestDeriveRepoIDFromGit_SeamFormVariants(t *testing.T) {
	cases := []struct {
		name   string
		remote string
		want   string
	}{
		{"github ssh", "git@github.com:acme/repo.git", "github.com/acme/repo"},
		{"github https", "https://github.com/acme/repo.git", "github.com/acme/repo"},
		{"gitlab nested", "git@gitlab.acme.internal:payments/settlement-engine.git", "gitlab.acme.internal/payments/settlement-engine"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withGitRemoteOriginURL(t, func(string) (string, error) {
				return c.remote, nil
			})
			if got := DeriveRepoIDFromGit(t.TempDir()); got != c.want {
				t.Errorf("DeriveRepoIDFromGit = %q, want %q", got, c.want)
			}
		})
	}
}

func TestDeriveRepoIDFromGit_NoRemoteReturnsEmpty(t *testing.T) {
	// Simulate `git -C <dir> remote get-url origin` failing with exit 1
	// (the real CLI behavior when no origin remote is configured). The
	// helper must swallow it and return "" so callers leave repo_id blank
	// rather than fabricating a value (per spec §5.3 fallback contract).
	withGitRemoteOriginURL(t, func(string) (string, error) {
		return "", fmt.Errorf("fatal: No such remote 'origin'")
	})
	if got := DeriveRepoIDFromGit(t.TempDir()); got != "" {
		t.Errorf("DeriveRepoIDFromGit with no remote = %q, want empty", got)
	}
}

func TestDeriveRepoIDFromGit_BlankOriginReturnsEmpty(t *testing.T) {
	// Some edge cases (broken git config) can return a blank URL with no
	// error. Treat that the same as "no remote".
	withGitRemoteOriginURL(t, func(string) (string, error) {
		return "  \n", nil
	})
	if got := DeriveRepoIDFromGit(t.TempDir()); got != "" {
		t.Errorf("DeriveRepoIDFromGit with blank URL = %q, want empty", got)
	}
}

func TestDeriveRepoIDFromGit_WeirdFormReturnsEmpty(t *testing.T) {
	// A non-URL string flows through gitremote.CanonicalRepoID and falls out
	// as "". Caller (GenerateAgentsRC) leaves repo_id blank, doctor warns
	// later (p2+).
	withGitRemoteOriginURL(t, func(string) (string, error) {
		return "this is not a url", nil
	})
	if got := DeriveRepoIDFromGit(t.TempDir()); got != "" {
		t.Errorf("DeriveRepoIDFromGit with weird URL = %q, want empty", got)
	}
}

func TestGenerateAgentsRC_PopulatesRepoIDFromGitRemote(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	withGitRemoteOriginURL(t, func(string) (string, error) {
		return "git@github.com:AGOrcha/dot-agents.git", nil
	})

	rc, err := GenerateAgentsRC(testProject, t.TempDir())
	if err != nil {
		t.Fatalf(errFmtGenerateRC, err)
	}
	if rc.RepoID != "github.com/AGOrcha/dot-agents" {
		t.Errorf("RepoID: got %q, want %q", rc.RepoID, "github.com/AGOrcha/dot-agents")
	}
}

func TestGenerateAgentsRC_NoGitRemoteLeavesRepoIDEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	withGitRemoteOriginURL(t, func(string) (string, error) {
		return "", fmt.Errorf("not a git repository")
	})

	rc, err := GenerateAgentsRC(testProject, t.TempDir())
	if err != nil {
		t.Fatalf(errFmtGenerateRC, err)
	}
	if rc.RepoID != "" {
		t.Errorf("RepoID should be empty when no git remote, got %q", rc.RepoID)
	}
}

// TestDeriveRepoIDFromGit_CorruptConfigEmitsWarning covers the genuine-
// corruption branch (spec Slice 3): a real git checkout whose .git/config
// is malformed is neither ErrNoOrigin nor "not a checkout", so it must fire
// a structured warning via the emitConfigWarning seam while still returning
// "" (the documented spec §5.3 fallback is unchanged).
func TestDeriveRepoIDFromGit_CorruptConfigEmitsWarning(t *testing.T) {
	tmp := t.TempDir()
	if _, err := git.PlainInit(tmp, false); err != nil {
		t.Fatalf("PlainInit: %v", err)
	}
	cfgPath := filepath.Join(tmp, ".git", "config")
	if err := os.WriteFile(cfgPath, []byte("[core\nbroken = true"), 0644); err != nil {
		t.Fatal(err)
	}

	var fired []events.Envelope
	prev := emitConfigWarning
	emitConfigWarning = func(env events.Envelope) { fired = append(fired, env) }
	t.Cleanup(func() { emitConfigWarning = prev })

	if got := DeriveRepoIDFromGit(tmp); got != "" {
		t.Errorf("DeriveRepoIDFromGit with corrupt config = %q, want empty", got)
	}
	if len(fired) != 1 {
		t.Fatalf("expected exactly one warning event, got %d: %+v", len(fired), fired)
	}
	if fired[0].Type != "event.config.git_origin_unreadable" {
		t.Errorf("warning event type = %q", fired[0].Type)
	}
}

// TestDeriveRepoIDFromGit_NotACheckoutNoWarning regression-guards the
// legitimate-absence case: a plain directory that was never a git checkout
// must NOT fire a warning (git.ErrRepositoryNotExists is excluded).
func TestDeriveRepoIDFromGit_NotACheckoutNoWarning(t *testing.T) {
	tmp := t.TempDir()

	var fired []events.Envelope
	prev := emitConfigWarning
	emitConfigWarning = func(env events.Envelope) { fired = append(fired, env) }
	t.Cleanup(func() { emitConfigWarning = prev })

	if got := DeriveRepoIDFromGit(tmp); got != "" {
		t.Errorf("DeriveRepoIDFromGit on non-checkout = %q, want empty", got)
	}
	if len(fired) != 0 {
		t.Errorf("expected no warning for a plain non-checkout dir, got %d: %+v", len(fired), fired)
	}
}

// TestDeriveRepoIDFromGit_NoOriginRemoteNoWarning regression-guards the
// other legitimate-absence case: a real checkout with no `origin` remote
// configured (gitremote.ErrNoOrigin) must NOT fire a warning either.
func TestDeriveRepoIDFromGit_NoOriginRemoteNoWarning(t *testing.T) {
	tmp := t.TempDir()
	if _, err := git.PlainInit(tmp, false); err != nil {
		t.Fatalf("PlainInit: %v", err)
	}

	var fired []events.Envelope
	prev := emitConfigWarning
	emitConfigWarning = func(env events.Envelope) { fired = append(fired, env) }
	t.Cleanup(func() { emitConfigWarning = prev })

	if got := DeriveRepoIDFromGit(tmp); got != "" {
		t.Errorf("DeriveRepoIDFromGit with no origin = %q, want empty", got)
	}
	if len(fired) != 0 {
		t.Errorf("expected no warning when repo simply has no origin remote, got %d: %+v", len(fired), fired)
	}
}

func TestMergeGenerateAgentsRC_PreservesExplicitRepoID(t *testing.T) {
	// Explicit repo_id in the on-disk manifest must survive --generate
	// even when the git remote would normalize to a different value (per
	// spec §7.4 — repo_id is a protected scalar).
	existing := &AgentsRC{
		Version: 2,
		Project: "myproject",
		RepoID:  "github.com/acme/explicit-override",
		Sources: []Source{{Type: testSourceTypeLocal}},
	}
	generated := &AgentsRC{
		Version: 1,
		Project: "myproject",
		RepoID:  "github.com/AGOrcha/dot-agents", // would-be derived
		Sources: []Source{{Type: testSourceTypeLocal}},
		Skills:  []string{"s"},
	}
	out := MergeGenerateAgentsRC(existing, generated)
	if out.RepoID != "github.com/acme/explicit-override" {
		t.Errorf("RepoID: got %q, want preserved existing", out.RepoID)
	}
}

func TestMergeGenerateAgentsRC_FillsRepoIDWhenExistingEmpty(t *testing.T) {
	// A v1 manifest without repo_id must be upgraded in place: when
	// existing.RepoID is empty, the freshly derived value wins so the
	// next `da install --generate` writes the bootstrapped value.
	existing := &AgentsRC{
		Version: 1,
		Project: "myproject",
		Sources: []Source{{Type: testSourceTypeLocal}},
	}
	generated := &AgentsRC{
		Version: 1,
		Project: "myproject",
		RepoID:  "github.com/AGOrcha/dot-agents",
		Sources: []Source{{Type: testSourceTypeLocal}},
	}
	out := MergeGenerateAgentsRC(existing, generated)
	if out.RepoID != "github.com/AGOrcha/dot-agents" {
		t.Errorf("RepoID: got %q, want derived value to win", out.RepoID)
	}
}

func TestMergeGenerateAgentsRC_V1ManifestWithoutRepoIDRoundTripsByteForByte(t *testing.T) {
	// Acceptance criterion from p0b: "existing v1 .agentsrc.json without
	// repo_id is preserved unchanged on subsequent saves (round-trip
	// preserves omitempty)". Verify the marshalled bytes do not contain
	// "repo_id" when neither existing nor generated populated it.
	existing := &AgentsRC{
		Version: 1,
		Project: "myproject",
		Sources: []Source{{Type: testSourceTypeLocal}},
	}
	generated := &AgentsRC{
		Version: 1,
		Project: "myproject",
		Sources: []Source{{Type: testSourceTypeLocal}},
		// RepoID intentionally empty — simulates GenerateAgentsRC on a
		// non-git project directory.
	}
	out := MergeGenerateAgentsRC(existing, generated)
	if out.RepoID != "" {
		t.Fatalf("RepoID should stay empty, got %q", out.RepoID)
	}
	data, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(data), "repo_id") {
		t.Errorf("marshalled output should omit repo_id when empty: %s", data)
	}
}

func TestAgentsRCPRSourceRoundtrip(t *testing.T) {
	tmp := t.TempDir()
	input := `{
  "version": 2,
  "project": "myproject",
  "sources": [{"type":"local"}],
  "hooks": false,
  "mcp": false,
  "settings": false,
  "pr_source": {
    "producer": "gh",
    "list": {
      "argv": ["gh","pr","list","--json","number"],
      "each": ".",
      "map": {"number": ".number", "branch": ".headRefName"}
    },
    "comments": {
      "argv": ["gh","pr","view","{number}","--json","comments"],
      "each": ".comments",
      "map": {"author": ".author.login"}
    },
    "poll_interval_s": 270
  }
}`
	if err := os.WriteFile(filepath.Join(tmp, AgentsRCFile), []byte(input), 0644); err != nil {
		t.Fatal(err)
	}

	rc, err := LoadAgentsRC(tmp)
	if err != nil {
		t.Fatalf("LoadAgentsRC: %v", err)
	}

	// pr_source is a typed field, not captured as an unknown extra field.
	if _, ok := rc.ExtraFields["pr_source"]; ok {
		t.Fatal("pr_source leaked into ExtraFields; six-place sync incomplete")
	}
	assertRoundtripPRSource(t, "load", rc.PRSource)

	// Round-trip: save and reload preserves the typed field.
	if err := rc.Save(tmp); err != nil {
		t.Fatalf("Save: %v", err)
	}
	rc2, err := LoadAgentsRC(tmp)
	if err != nil {
		t.Fatalf("LoadAgentsRC after save: %v", err)
	}
	assertRoundtripPRSource(t, "reload", rc2.PRSource)
}

// assertRoundtripPRSource verifies the typed pr_source parsed from the roundtrip
// fixture. It is shared by the initial load and the post-save reload so the
// per-field assertion chains live in one flat helper (well under the cognitive
// complexity gate) instead of being duplicated in the test body. phase names the
// call site for clearer failure messages.
func assertRoundtripPRSource(t *testing.T, phase string, src *AgentsRCPRSource) {
	t.Helper()
	if src == nil {
		t.Fatalf("%s: PRSource is nil", phase)
		return
	}
	if src.Producer != "gh" {
		t.Errorf("%s: Producer = %q, want gh", phase, src.Producer)
	}
	if src.PollIntervalS != 270 {
		t.Errorf("%s: PollIntervalS = %d, want 270", phase, src.PollIntervalS)
	}
	assertRoundtripList(t, phase, src.List)
	assertRoundtripComments(t, phase, src.Comments)
}

// assertRoundtripList pins the List fetch block fields the fixture preserves.
func assertRoundtripList(t *testing.T, phase string, list *AgentsRCPRFetch) {
	t.Helper()
	if list == nil {
		t.Errorf("%s: List fetch block is nil", phase)
		return
	}
	if list.Map["number"] != ".number" {
		t.Errorf("%s: List.Map[number] mismatch: %+v", phase, list)
	}
	if len(list.Argv) == 0 || list.Argv[0] != "gh" {
		t.Errorf("%s: List.Argv not preserved: %+v", phase, list)
	}
}

// assertRoundtripComments pins the Comments fetch block field the fixture preserves.
func assertRoundtripComments(t *testing.T, phase string, comments *AgentsRCPRFetch) {
	t.Helper()
	if comments == nil {
		t.Errorf("%s: Comments fetch block is nil", phase)
		return
	}
	if comments.Each != ".comments" {
		t.Errorf("%s: Comments.Each mismatch: %+v", phase, comments)
	}
}

func TestAgentsRCPRSourceMarshalOmittedWhenNil(t *testing.T) {
	rc := AgentsRC{Version: 2, Project: "p", Sources: []Source{{Type: "local"}}}
	data, err := json.Marshal(rc)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(data), "pr_source") {
		t.Errorf("marshalled output should omit pr_source when nil: %s", data)
	}
}

// TestPRSourceToEngineConfigPreservesAllFields is the finding-1 regression: the
// config pr_source block must bridge to the engine type with every field carried
// across (no round-trip loss), so the configured source actually drives the
// producer instead of being inert.
func TestPRSourceToEngineConfigPreservesAllFields(t *testing.T) {
	src := &AgentsRCPRSource{
		Producer: "exec",
		List: &AgentsRCPRFetch{
			Argv:    []string{"glab", "mr", "list"},
			URL:     "https://api/mrs",
			Method:  "GET",
			Headers: map[string]string{"X-Token": "abc"},
			Each:    ".items",
			Map:     map[string]string{"number": ".iid", "branch": ".source_branch"},
		},
		Comments: &AgentsRCPRFetch{
			Argv: []string{"glab", "mr", "note", "list"},
			Each: ".notes",
			Map:  map[string]string{"author": ".author.username"},
		},
		PollIntervalS: 90,
	}
	got := src.ToEngineConfig()
	want := events.PRSourceConfig{
		Producer: "exec",
		List: events.FetchBlock{
			Argv:    []string{"glab", "mr", "list"},
			URL:     "https://api/mrs",
			Method:  "GET",
			Headers: map[string]string{"X-Token": "abc"},
			Each:    ".items",
			Map:     map[string]string{"number": ".iid", "branch": ".source_branch"},
		},
		Comments: events.FetchBlock{
			Argv: []string{"glab", "mr", "note", "list"},
			Each: ".notes",
			Map:  map[string]string{"author": ".author.username"},
		},
		PollIntervalS: 90,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ToEngineConfig() round-trip lost fields:\n got=%+v\nwant=%+v", got, want)
	}
}

// TestPRSourceToEngineConfigNilDefaultsToGH proves a nil/absent pr_source falls
// back to the gh default (silent-zero guard: an empty config must not yield an
// inert engine config).
func TestPRSourceToEngineConfigNilDefaultsToGH(t *testing.T) {
	var src *AgentsRCPRSource
	got := src.ToEngineConfig()
	if got.Producer != "gh" {
		t.Errorf("nil pr_source ToEngineConfig().Producer = %q, want gh default", got.Producer)
	}
	if len(got.List.Map) == 0 {
		t.Error("nil pr_source default has empty list map; expected the gh default map")
	}
}

// TestEffectivePRSourceFallsBackToGH covers the AgentsRC-level resolution: a
// config without pr_source still resolves to a usable gh source.
func TestEffectivePRSourceFallsBackToGH(t *testing.T) {
	rc := &AgentsRC{Version: 2}
	if got := rc.EffectivePRSource(); got.Producer != "gh" {
		t.Errorf("EffectivePRSource() = %q, want gh", got.Producer)
	}
	var nilRC *AgentsRC
	if got := nilRC.EffectivePRSource(); got.Producer != "gh" {
		t.Errorf("nil AgentsRC EffectivePRSource() = %q, want gh", got.Producer)
	}
}

// TestNewPRListProducerDrivesProducerFromConfig is the end-to-end finding-1+2
// regression: the configured pr_source must drive a real producer that emits
// event.pr.* envelopes with a derived rollup, and the registry must carry the
// control-plane PR kinds. This is the reachable production path that exercises
// ToEngineConfig + DeriveRollupState.
func TestNewPRListProducerDrivesProducerFromConfig(t *testing.T) {
	rc := &AgentsRC{
		Version: 2,
		PRSource: &AgentsRCPRSource{
			Producer: "exec",
			List: &AgentsRCPRFetch{
				Argv: []string{"gh", "pr", "list"},
				Each: ".",
				Map: map[string]string{
					"number": ".number",
					"rollup": ".statusCheckRollup",
				},
			},
		},
	}
	reg := events.NewRegistry()
	doc := []byte(`[{"number":5,"statusCheckRollup":[{"name":"lint","status":"COMPLETED","conclusion":"FAILURE"}]}]`)
	prod, err := rc.NewPRListProducer(reg, events.KindPRCIFailed, "github", &stubFetcher{out: doc})
	if err != nil {
		t.Fatalf("NewPRListProducer: %v", err)
	}
	// The PR kinds are registered control-plane on the supplied registry.
	if _, ok := reg.Lookup(events.KindPRCIFailed); !ok {
		t.Error("NewPRListProducer did not register PR kinds on the registry")
	}
	envs, err := prod.Cycle(context.Background())
	if err != nil {
		t.Fatalf("Cycle: %v", err)
	}
	if len(envs) != 1 {
		t.Fatalf("got %d envelopes, want 1", len(envs))
	}
	var pr events.PR
	if err := json.Unmarshal(envs[0].Payload, &pr); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if pr.Rollup.State != events.RollupFailing {
		t.Errorf("config-driven producer derived Rollup.State = %q, want FAILING", pr.Rollup.State)
	}
}

// TestNewPRListProducerNilRegistry guards the nil-registry path.
func TestNewPRListProducerNilRegistry(t *testing.T) {
	rc := &AgentsRC{Version: 2}
	if _, err := rc.NewPRListProducer(nil, events.KindPROpened, "github", nil); err == nil {
		t.Fatal("expected error for nil registry")
	}
}

// stubFetcher is the hermetic fetch seam for the config-side producer test.
type stubFetcher struct {
	out []byte
	err error
}

func (f *stubFetcher) Fetch(context.Context, events.FetchSpec) ([]byte, error) {
	return f.out, f.err
}
