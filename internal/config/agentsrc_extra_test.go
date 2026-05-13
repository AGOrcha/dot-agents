package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSetRefreshMetadata_NilSafe(t *testing.T) {
	var a *AgentsRC
	a.SetRefreshMetadata("v", "c", "d", time.Now()) // must not panic
}

func TestSetRefreshMetadata_UTC(t *testing.T) {
	a := &AgentsRC{}
	ts := time.Date(2026, 4, 1, 12, 0, 0, 0, time.FixedZone("EST", -5*3600))
	a.SetRefreshMetadata("1.0", "abc", "v1", ts)
	if a.Refresh == nil {
		t.Fatal("Refresh nil")
	}
	if !strings.HasSuffix(a.Refresh.RefreshedAt, "Z") {
		t.Errorf("RefreshedAt should be UTC RFC3339 (Z), got %q", a.Refresh.RefreshedAt)
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
			// $schema is a known field — ExtraFields entry should not overwrite
			"$schema": json.RawMessage(`"OVERWRITE-ATTEMPT"`),
			"team":    json.RawMessage(`"platform"`),
		},
	}
	rc.Schema = "https://dot-agents.dev/schemas/agentsrc.json"
	data, err := json.Marshal(rc)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw["$schema"]), "dot-agents.dev") {
		t.Errorf("$schema overwritten by ExtraFields: %s", raw["$schema"])
	}
	if string(raw["team"]) != `"platform"` {
		t.Errorf("custom field missing: %s", raw["team"])
	}
}

func TestSaveAgentsRC_BadPath(t *testing.T) {
	rc := &AgentsRC{Version: 1, Project: "p", Sources: []Source{{Type: "local"}}}
	// Save to a path under a regular file (cannot be parent dir)
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

func TestDetectHookEvents_GlobalYAMLBundleEnablesAll(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "hooks", "global", "b1")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "HOOK.yaml"), []byte("name: b1\n"), 0644)
	got := detectHookEvents(home, "myproj")
	if !got.All {
		t.Errorf("expected All=true with global yaml bundle, got %+v", got)
	}
}

func TestDetectHookEvents_None(t *testing.T) {
	home := t.TempDir()
	got := detectHookEvents(home, "p")
	if got.IsEnabled() {
		t.Errorf("expected disabled, got %+v", got)
	}
}

func TestDetectSettingsHookEvents_BadJSON(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "settings", "global")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "claude-code.json"), []byte("not json"), 0644)
	got := detectSettingsHookEvents(home, "global")
	if got.IsEnabled() {
		t.Errorf("bad json: got %+v", got)
	}
}

func TestDetectSettingsHookEvents_HooksNotMap(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "settings", "global")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "claude-code.json"), []byte(`{"hooks":"not a map"}`), 0644)
	got := detectSettingsHookEvents(home, "global")
	if got.IsEnabled() {
		t.Error("expected disabled when hooks is not a map")
	}
}

func TestDetectSettingsHookEvents_NoHooksKey(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "settings", "global")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "claude-code.json"), []byte(`{}`), 0644)
	got := detectSettingsHookEvents(home, "global")
	if got.IsEnabled() {
		t.Error("expected disabled when no hooks key")
	}
}

func TestReadMCPScope_BadJSON(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "mcp", "global")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "mcp.json"), []byte("not json"), 0644)
	got := readMCPScope(home, "global")
	if got.IsEnabled() {
		t.Errorf("bad json: got %+v", got)
	}
}

func TestReadMCPScope_NoServersKey(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "mcp", "global")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "mcp.json"), []byte(`{"other":"value"}`), 0644)
	got := readMCPScope(home, "global")
	if got.IsEnabled() {
		t.Error("expected disabled when no servers key")
	}
}

func TestReadMCPScope_EmptyServers(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "mcp", "global")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "mcp.json"), []byte(`{"servers":{}}`), 0644)
	got := readMCPScope(home, "global")
	if got.IsEnabled() {
		t.Error("expected disabled when servers map is empty")
	}
}

func TestReadMCPScope_NoFiles(t *testing.T) {
	home := t.TempDir()
	got := readMCPScope(home, "global")
	if got.IsEnabled() {
		t.Error("expected disabled when scope has no files")
	}
}

func TestDetectRuleScopes_OnlyOtherExt(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "rules", "proj")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "weird.json"), []byte("{}"), 0644)
	got := detectRuleScopes(home, "proj")
	if len(got) != 1 || got[0] != "global" {
		t.Errorf("expected only [global], got %v", got)
	}
}

func TestDetectRuleScopes_MissingDir(t *testing.T) {
	home := t.TempDir()
	got := detectRuleScopes(home, "missing-proj")
	if len(got) != 1 || got[0] != "global" {
		t.Errorf("expected only [global], got %v", got)
	}
}

func TestHasYAMLHooks_DirAbsent(t *testing.T) {
	if hasYAMLHooks("/path/that/does/not/exist") {
		t.Error("missing dir should return false")
	}
}

func TestHasYAMLHooks_FileInDirIgnored(t *testing.T) {
	tmp := t.TempDir()
	// A regular file (not a dir entry) should be ignored
	os.WriteFile(filepath.Join(tmp, "stray.yaml"), []byte("x"), 0644)
	if hasYAMLHooks(tmp) {
		t.Error("non-dir entry should be ignored")
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
