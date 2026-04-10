package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

const (
	testProject         = "myproject"
	testSourceTypeLocal = "local"
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
		Schema:   "https://dot-agents.dev/schemas/agentsrc.json",
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

	// Skills: global/skill-global, myproject/skill-proj
	writeFile(filepath.Join(mkdirAll("skills", "global", "skill-global"), testSkillMarkerFile), "# skill")
	writeFile(filepath.Join(mkdirAll("skills", testProject, "skill-proj"), testSkillMarkerFile), "# skill")
	// File (not dir) in skills — should be ignored
	writeFile(filepath.Join(home, "skills", "global", "not-a-skill.txt"), "ignore me")

	// Agents: global/agent-global
	writeFile(filepath.Join(mkdirAll("agents", "global", "agent-global"), "AGENT.md"), "# agent")

	// Rules: global file + project file
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
	writeFile(filepath.Join(home, "mcp", testProject, "mcp.json"), `{
		"servers": {
			"server-a": {},
			"server-b": {}
		}
	}`)

	// Settings: cursor.json in global scope
	writeFile(filepath.Join(home, "settings", "global", "cursor.json"), "{}")

	return home
}

func TestGenerateAgentsRCSkills(t *testing.T) {
	home := agentsHomeFixture(t)
	t.Setenv("AGENTS_HOME", home)

	rc, err := GenerateAgentsRC(testProject, t.TempDir())
	if err != nil {
		t.Fatalf(errFmtGenerateRC, err)
	}

	sort.Strings(rc.Skills)
	want := []string{"skill-global", "skill-proj"}
	if !reflect.DeepEqual(rc.Skills, want) {
		t.Errorf("Skills: got %v, want %v", rc.Skills, want)
	}
}

func TestGenerateAgentsRCAgents(t *testing.T) {
	home := agentsHomeFixture(t)
	t.Setenv("AGENTS_HOME", home)

	rc, err := GenerateAgentsRC(testProject, t.TempDir())
	if err != nil {
		t.Fatalf(errFmtGenerateRC, err)
	}

	if !reflect.DeepEqual(rc.Agents, []string{"agent-global"}) {
		t.Errorf("Agents: got %v, want [agent-global]", rc.Agents)
	}
}

func TestGenerateAgentsRCRules(t *testing.T) {
	home := agentsHomeFixture(t)
	t.Setenv("AGENTS_HOME", home)

	rc, err := GenerateAgentsRC(testProject, t.TempDir())
	if err != nil {
		t.Fatalf(errFmtGenerateRC, err)
	}

	sort.Strings(rc.Rules)
	want := []string{"global", "project"}
	if !reflect.DeepEqual(rc.Rules, want) {
		t.Errorf("Rules: got %v, want %v", rc.Rules, want)
	}
}

func TestGenerateAgentsRCRulesGlobalOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	// No project-scoped rules created

	rc, err := GenerateAgentsRC(testProject, t.TempDir())
	if err != nil {
		t.Fatalf(errFmtGenerateRC, err)
	}

	if !reflect.DeepEqual(rc.Rules, []string{"global"}) {
		t.Errorf("Rules: got %v, want [global]", rc.Rules)
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
	if !rc.Hooks.All {
		t.Fatalf("Hooks.All = false, want true when canonical hook bundles exist; got %+v", rc.Hooks)
	}
	if len(rc.Hooks.Names) != 0 {
		t.Fatalf("Hooks.Names = %v, want empty when Hooks.All is true", rc.Hooks.Names)
	}
}

func TestGenerateAgentsRCHooksLegacySettingsFallBackToGlobal(t *testing.T) {
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

	got := rc.Hooks.Names
	sort.Strings(got)
	want := []string{"PreToolUse", "Stop"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Hooks.Names: got %v, want %v", got, want)
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

func TestGenerateAgentsRCMCPFallsBackToGlobal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)

	// Only global mcp, no project-scoped
	mcpPath := filepath.Join(home, "mcp", "global", "mcp.json")
	os.MkdirAll(filepath.Dir(mcpPath), 0755)
	os.WriteFile(mcpPath, []byte(`{"servers":{"global-srv":{}}}`), 0644)

	rc, err := GenerateAgentsRC(testProject, t.TempDir())
	if err != nil {
		t.Fatalf(errFmtGenerateRC, err)
	}

	if !reflect.DeepEqual(rc.MCP.Names, []string{"global-srv"}) {
		t.Errorf("MCP.Names: got %v, want [global-srv]", rc.MCP.Names)
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
		t.Error("Settings should be true when cursor.json exists")
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

	// Plain file in skills/global — should be ignored
	skillsDir := filepath.Join(home, "skills", "global")
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

// ── JSON shape produced by Save ───────────────────────────────────────────────

func TestAgentsRCJSONShape(t *testing.T) {
	tmp := t.TempDir()
	rc := &AgentsRC{
		Schema:  "https://dot-agents.dev/schemas/agentsrc.json",
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
