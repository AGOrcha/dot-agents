package workflow

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
)

func TestLoadGraphBridgeConfig_Absent(t *testing.T) {
	dir := t.TempDir()
	cfg, err := loadGraphBridgeConfig(dir)
	if err != nil {
		t.Fatalf("loadGraphBridgeConfig absent: %v", err)
	}
	if cfg.Enabled {
		t.Error("expected bridge disabled when config absent")
	}
}

func TestLoadGraphBridgeConfig_Present(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, ".agents", "workflow")
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := `schema_version: 1
enabled: true
graph_home: /tmp/my-graph
allowed_intents:
  - plan_context
  - decision_lookup
`
	if err := os.WriteFile(filepath.Join(cfgDir, "graph-bridge.yaml"), []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := loadGraphBridgeConfig(dir)
	if err != nil {
		t.Fatalf("loadGraphBridgeConfig: %v", err)
	}
	if !cfg.Enabled {
		t.Error("expected bridge enabled")
	}
	if cfg.GraphHome != "/tmp/my-graph" {
		t.Errorf("graph_home: got %s", cfg.GraphHome)
	}
	if len(cfg.AllowedIntents) != 2 {
		t.Errorf("allowed_intents: expected 2, got %d", len(cfg.AllowedIntents))
	}
}

func TestIsValidWorkflowBridgeIntent(t *testing.T) {
	valid := []string{"plan_context", "decision_lookup", "entity_context", "workflow_memory", "contradictions"}
	for _, intent := range valid {
		if !isValidWorkflowBridgeIntent(intent) {
			t.Errorf("expected %s to be valid", intent)
		}
	}
	if isValidWorkflowBridgeIntent("unknown") {
		t.Error("'unknown' should not be valid")
	}
}

func TestRunWorkflowGraphQueryAllowsWorkflowBridgeIntent(t *testing.T) {
	project := t.TempDir()
	kgHome := t.TempDir()
	agentsHome := t.TempDir()
	t.Setenv("KG_HOME", kgHome)
	t.Setenv("AGENTS_HOME", agentsHome)

	runKGSetupViaCLI(t)

	cfgDir := filepath.Join(project, ".agents", "workflow")
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		t.Fatal(err)
	}
	cfg := []byte("schema_version: 1\nenabled: true\ngraph_home: \"" + kgHome + "\"\n")
	if err := os.WriteFile(filepath.Join(cfgDir, "graph-bridge.yaml"), cfg, 0644); err != nil {
		t.Fatal(err)
	}

	cmd := &cobra.Command{}
	cmd.Flags().String("intent", "decision_lookup", "")
	cmd.Flags().String("scope", "", "")

	oldwd, _ := os.Getwd()
	defer os.Chdir(oldwd)
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}

	if err := runWorkflowGraphQuery(cmd, nil); err != nil {
		t.Fatalf("runWorkflowGraphQuery: %v", err)
	}
}

func TestWorkflowGraphQueryCodeStructureRoutesToKGBridge(t *testing.T) {
	oldExe := workflowDotAgentsExe
	t.Cleanup(func() { workflowDotAgentsExe = oldExe })

	repoRoot := dotAgentsRepoRoot(t)
	bin := filepath.Join(t.TempDir(), "dot-agents")
	build := exec.Command("go", "build", "-buildvcs=false", "-o", bin, "./cmd/dot-agents")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build dot-agents: %v\n%s", err, out)
	}
	workflowDotAgentsExe = func() (string, error) { return bin, nil }

	project := t.TempDir()
	t.Setenv("KG_HOME", t.TempDir())

	cmd := &cobra.Command{}
	cmd.Flags().String("intent", "symbol_lookup", "")
	cmd.Flags().String("scope", "", "")

	oldwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(oldwd) })
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}

	err := runWorkflowGraphQuery(cmd, []string{"SomeQuery"})
	if err == nil {
		t.Fatal("expected error from kg bridge when graph is not initialized")
	}
	if strings.Contains(err.Error(), "workflow graph query does not handle") {
		t.Fatalf("expected route to kg bridge, got old guard: %v", err)
	}
	if strings.Contains(err.Error(), "Use `da kg bridge query") {
		t.Fatalf("expected route to kg bridge, got manual-use hint: %v", err)
	}
}

func TestWorkflowGraphQueryKGBridgeIntentsNotRouted(t *testing.T) {
	kgIntents := []string{"plan_context", "decision_lookup", "entity_context", "workflow_memory", "contradictions"}
	for _, intent := range kgIntents {
		if isWorkflowGraphCodeBridgeIntent(intent) {
			t.Errorf("intent %q must not be classified as workflow code-bridge intent (should use local graph bridge path)", intent)
		}
	}
}

// ── Wave 5: GraphBridgeHealth write/read ─────────────────────────────────────

func TestWriteReadGraphBridgeHealth(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	h := GraphBridgeHealth{
		SchemaVersion:    1,
		Timestamp:        "2026-01-01T00:00:00Z",
		AdapterAvailable: true,
		NoteCount:        5,
		Status:           "healthy",
	}
	if err := writeGraphBridgeHealth("test-project", h); err != nil {
		t.Fatalf("writeGraphBridgeHealth: %v", err)
	}
	got, err := readGraphBridgeHealth("test-project")
	if err != nil {
		t.Fatalf("readGraphBridgeHealth: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil health")
	}
	if got.NoteCount != 5 {
		t.Errorf("NoteCount: got %d, want 5", got.NoteCount)
	}
}

// ── Wave 5: LocalGraphAdapter ─────────────────────────────────────────────────

func TestLocalGraphAdapter_Health_NotInitialized(t *testing.T) {
	dir := t.TempDir()
	adapter := NewLocalGraphAdapter(dir)
	h, err := adapter.Health()
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if h.AdapterAvailable {
		t.Error("expected unavailable before setup")
	}
	if h.Status == "healthy" {
		t.Error("expected non-healthy status")
	}
}

func TestLocalGraphAdapter_Query_ReturnsResults(t *testing.T) {
	home := newTempKGForWorkflow(t)
	runKGSetupViaCLI(t)
	now := "2026-01-01T00:00:00Z"
	notePath := filepath.Join(home, "notes", "decisions", "dec-workflow-test.md")
	if err := os.MkdirAll(filepath.Dir(notePath), 0755); err != nil {
		t.Fatal(err)
	}
	noteBody := "---\n" +
		"id: dec-workflow-test\n" +
		"type: decision\n" +
		"title: \"Use cobra for CLI\"\n" +
		"summary: \"We chose cobra.\"\n" +
		"status: active\n" +
		"created_at: " + now + "\n" +
		"updated_at: " + now + "\n" +
		"---\n\n" +
		"body content about cobra CLI framework\n"
	if err := os.WriteFile(notePath, []byte(noteBody), 0644); err != nil {
		t.Fatalf("write note: %v", err)
	}

	adapter := NewLocalGraphAdapter(home)
	resp, err := adapter.Query(GraphBridgeQuery{
		Intent: "decision_lookup",
		Query:  "cobra",
	})
	if err != nil {
		t.Fatalf("adapter.Query: %v", err)
	}
	if len(resp.Results) == 0 {
		t.Error("expected at least one result for 'cobra'")
	}
	if resp.Results[0].Type != "decision" {
		t.Errorf("expected type=decision, got %s", resp.Results[0].Type)
	}
}

func TestLocalGraphAdapter_Query_UnknownIntent(t *testing.T) {
	dir := t.TempDir()
	adapter := NewLocalGraphAdapter(dir)
	_, err := adapter.Query(GraphBridgeQuery{Intent: "bad_intent", Query: "x"})
	if err == nil {
		t.Error("expected error for unknown intent")
	}
}

// ── Wave 6: Delegation & Merge-back ─────────────────────────────────────────

// ── pr3b coverage: graph helpers not previously exercised ────────────────────

func TestParseNoteMetadata(t *testing.T) {
	t.Run("full-frontmatter", func(t *testing.T) {
		content := "---\nid: dec-1\ntitle: \"My Title\"\nsummary: 'short text'\n---\nbody\n"
		id, title, summary, _ := parseNoteMetadata(content)
		if id != "dec-1" {
			t.Errorf("id = %q, want dec-1", id)
		}
		if title != "My Title" {
			t.Errorf("title = %q, want My Title", title)
		}
		if summary != "short text" {
			t.Errorf("summary = %q, want short text", summary)
		}
	})

	t.Run("no-frontmatter", func(t *testing.T) {
		id, title, summary, refs := parseNoteMetadata("just body text\n")
		if id != "" || title != "" || summary != "" || refs != nil {
			t.Errorf("expected zero values, got id=%q title=%q summary=%q refs=%v", id, title, summary, refs)
		}
	})

	t.Run("unterminated-frontmatter", func(t *testing.T) {
		id, _, _, _ := parseNoteMetadata("---\nid: x\nbody only\n")
		if id != "" {
			t.Errorf("expected empty id for unterminated fm, got %q", id)
		}
	})
}

func TestCountMarkdownNotes(t *testing.T) {
	home := t.TempDir()
	notesRoot := filepath.Join(home, "notes")
	for _, sub := range []string{"decisions", "entities", "sources"} {
		dir := filepath.Join(notesRoot, sub)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("---\nid: a\n---\nbody"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "ignored.txt"), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	got := countMarkdownNotes(home)
	if got != 3 {
		t.Errorf("countMarkdownNotes = %d, want 3", got)
	}

	t.Run("empty-home", func(t *testing.T) {
		if c := countMarkdownNotes(t.TempDir()); c != 0 {
			t.Errorf("empty home count = %d, want 0", c)
		}
	})
}

func TestSetLaneReadyStatus(t *testing.T) {
	cases := []struct {
		name   string
		code   bool
		ctx    bool
		status string
	}{
		{"both-ready", true, true, "healthy"},
		{"code-only", true, false, "partial"},
		{"ctx-only", false, true, "partial"},
		{"none", false, false, "degraded"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := &GraphBridgeHealth{CodeLaneReady: tc.code, ContextLaneReady: tc.ctx}
			setLaneReadyStatus(h)
			if h.Status != tc.status {
				t.Errorf("status = %q, want %q", h.Status, tc.status)
			}
			if tc.status != "healthy" && h.Note == "" {
				t.Errorf("expected note for status %q", tc.status)
			}
		})
	}
}

func TestValidateGraphBridgeIntent(t *testing.T) {
	t.Run("invalid-intent", func(t *testing.T) {
		if err := validateGraphBridgeIntent("not_a_real_intent", nil); err == nil {
			t.Error("expected error for invalid intent")
		}
	})
	t.Run("valid-intent-no-allowed-list", func(t *testing.T) {
		if err := validateGraphBridgeIntent("decision_lookup", nil); err != nil {
			t.Errorf("expected nil error, got %v", err)
		}
	})
	t.Run("valid-intent-in-allowed", func(t *testing.T) {
		if err := validateGraphBridgeIntent("plan_context", []string{"plan_context", "decision_lookup"}); err != nil {
			t.Errorf("expected nil error, got %v", err)
		}
	})
	t.Run("valid-intent-not-allowed", func(t *testing.T) {
		err := validateGraphBridgeIntent("plan_context", []string{"decision_lookup"})
		if err == nil {
			t.Error("expected error when intent not in allowed list")
		}
	})
}

func TestScaffoldGraphBridgeConfig(t *testing.T) {
	dir := t.TempDir()
	cfg, err := scaffoldGraphBridgeConfig(dir)
	if err != nil {
		t.Fatalf("scaffoldGraphBridgeConfig: %v", err)
	}
	if !cfg.Enabled {
		t.Error("scaffold should produce enabled config")
	}
	if cfg.SchemaVersion != 1 {
		t.Errorf("schema_version = %d, want 1", cfg.SchemaVersion)
	}
	path := filepath.Join(dir, ".agents", "workflow", "graph-bridge.yaml")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("scaffolded file not written: %v", err)
	}
}

func TestDefaultGraphHome(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	got := defaultGraphHome(t.TempDir())
	if got == "" {
		t.Error("defaultGraphHome returned empty string")
	}
	if !strings.HasSuffix(got, "knowledge-graph") {
		t.Errorf("expected knowledge-graph suffix, got %s", got)
	}
}

func TestLoadGraphBridgeConfig_MissingGraphHomeFallsBack(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, ".agents", "workflow")
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		t.Fatal(err)
	}
	// graph_home omitted in YAML — should fall back to defaultGraphHome.
	yamlBody := "schema_version: 1\nenabled: true\n"
	if err := os.WriteFile(filepath.Join(cfgDir, "graph-bridge.yaml"), []byte(yamlBody), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadGraphBridgeConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GraphHome == "" {
		t.Error("expected GraphHome to be populated via fallback")
	}
}

func TestLoadGraphBridgeConfig_Malformed(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, ".agents", "workflow")
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "graph-bridge.yaml"), []byte("::not yaml::\n  ["), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadGraphBridgeConfig(dir); err == nil {
		t.Error("expected error on malformed graph-bridge.yaml")
	}
}

func TestResolveGraphBridgeConfig_AutoScaffolds(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	cfg, err := resolveGraphBridgeConfig(dir)
	if err != nil {
		t.Fatalf("resolveGraphBridgeConfig: %v", err)
	}
	if !cfg.Enabled {
		t.Error("expected scaffolded config to be enabled")
	}
	// File should now exist on disk.
	if _, err := os.Stat(filepath.Join(dir, ".agents", "workflow", "graph-bridge.yaml")); err != nil {
		t.Errorf("scaffold file missing after resolve: %v", err)
	}
}

func TestRunWorkflowGraphQuery_MissingIntent(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("intent", "", "")
	cmd.Flags().String("scope", "", "")
	err := runWorkflowGraphQuery(cmd, nil)
	if err == nil {
		t.Fatal("expected error when --intent is empty")
	}
	if !strings.Contains(err.Error(), "intent") {
		t.Errorf("error should mention intent, got %v", err)
	}
}

func TestGraphSearchSubdir_CapAndDedup(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "notes", "decisions")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	// Two notes with same id (dedup test) and a third with unique id.
	dup := "---\nid: shared\ntitle: t\n---\nfoobar\n"
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte(dup), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.md"), []byte(dup), 0644); err != nil {
		t.Fatal(err)
	}
	unique := "---\nid: unique\ntitle: u\n---\nfoobar\n"
	if err := os.WriteFile(filepath.Join(dir, "c.md"), []byte(unique), 0644); err != nil {
		t.Fatal(err)
	}

	seen := map[string]bool{}
	var results []GraphBridgeResult
	graphSearchSubdir(home, "decisions", "foobar", seen, &results, 10)
	if len(results) != 2 {
		t.Errorf("dedup: expected 2 unique results, got %d", len(results))
	}

	t.Run("cap-enforced", func(t *testing.T) {
		seen2 := map[string]bool{}
		var capped []GraphBridgeResult
		graphSearchSubdir(home, "decisions", "foobar", seen2, &capped, 1)
		if len(capped) != 1 {
			t.Errorf("cap=1: got %d", len(capped))
		}
	})

	t.Run("missing-subdir-noop", func(t *testing.T) {
		seen3 := map[string]bool{}
		var none []GraphBridgeResult
		graphSearchSubdir(home, "no_such_sub", "x", seen3, &none, 10)
		if len(none) != 0 {
			t.Errorf("expected zero results for missing subdir, got %d", len(none))
		}
	})
}

func TestRunWorkflowGraphQuery_UnknownIntent(t *testing.T) {
	dir := t.TempDir()
	chdirForCov(t, dir)

	cfgDir := filepath.Join(dir, ".agents", "workflow")
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "graph-bridge.yaml"), []byte("schema_version: 1\nenabled: true\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := newGraphQueryTestCommand("totally_bogus_intent", "")
	err := runWorkflowGraphQuery(cmd, nil)
	if err == nil {
		t.Error("expected unknown-intent error")
	}
}

func TestGraphSearchSubdir_MissingDir(t *testing.T) {
	seen := map[string]bool{}
	var results []GraphBridgeResult

	graphSearchSubdir(t.TempDir(), "nope", "x", seen, &results, 10)
	if len(results) != 0 {
		t.Errorf("expected zero results from missing dir, got %d", len(results))
	}
}

func TestRunWorkflowGraphQuery_SuccessLocalAdapter(t *testing.T) {
	project := t.TempDir()
	kgHome := t.TempDir()
	agentsHome := t.TempDir()
	t.Setenv("KG_HOME", kgHome)
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", t.TempDir())

	if err := os.MkdirAll(filepath.Join(kgHome, "self"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(kgHome, "self", "config.yaml"), []byte("version: 1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	notesDir := filepath.Join(kgHome, "notes", "decisions")
	if err := os.MkdirAll(notesDir, 0755); err != nil {
		t.Fatal(err)
	}
	note := "---\nid: dec-1\ntitle: Decision\nsummary: chosen\n---\n\nbody about loops\n"
	if err := os.WriteFile(filepath.Join(notesDir, "dec-1.md"), []byte(note), 0644); err != nil {
		t.Fatal(err)
	}

	cfgDir := filepath.Join(project, ".agents", "workflow")
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		t.Fatal(err)
	}

	cfg, err := yaml.Marshal(GraphBridgeConfig{SchemaVersion: 1, Enabled: true, GraphHome: kgHome})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "graph-bridge.yaml"), cfg, 0644); err != nil {
		t.Fatal(err)
	}

	chdirForCov(t, project)
	cmd := newGraphQueryTestCommand("decision_lookup", "")
	out, _ := captureCovStdout(t, func() error {
		return runWorkflowGraphQuery(cmd, []string{"loops"})
	})
	if !strings.Contains(out, "Graph Query") {
		t.Errorf("expected graph query header, got: %s", out)
	}
}

func TestValidateGraphBridgeIntent_NoAllowlist(t *testing.T) {
	if err := validateGraphBridgeIntent("decision_lookup", nil); err != nil {
		t.Errorf("nil allowlist should be passthrough, got: %v", err)
	}
}

func TestValidateGraphBridgeIntent_NotInAllowlist(t *testing.T) {
	err := validateGraphBridgeIntent("decision_lookup", []string{"plan_context"})
	if err == nil || !strings.Contains(err.Error(), "not in allowed_intents") {
		t.Errorf("expected 'not in allowed_intents' error, got: %v", err)
	}
}

func TestValidateGraphBridgeIntent_Unknown(t *testing.T) {
	err := validateGraphBridgeIntent("totally_invalid", nil)
	if err == nil || !strings.Contains(err.Error(), "unknown intent") {
		t.Errorf("expected unknown intent error, got: %v", err)
	}
}

func TestLoadGraphBridgeConfig_ReadError(t *testing.T) {
	repo := t.TempDir()
	dir := filepath.Join(repo, ".agents", "workflow")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "graph-bridge.yaml")
	if err := os.WriteFile(cfgPath, []byte("enabled: true\n"), 0644); err != nil {
		t.Fatal(err)
	}
	chmodUnreadable(t, cfgPath)

	_, err := loadGraphBridgeConfig(repo)
	if err == nil {
		t.Fatal("expected read error")
	}
}

func TestLoadGraphBridgeConfig_MalformedYAML(t *testing.T) {
	repo := t.TempDir()
	dir := filepath.Join(repo, ".agents", "workflow")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "graph-bridge.yaml"), []byte(":\n  - bad: ["), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := loadGraphBridgeConfig(repo)
	if err == nil || !strings.Contains(err.Error(), "parse graph-bridge.yaml") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

func TestDefaultGraphHome_FromAgentsRC(t *testing.T) {
	repo := t.TempDir()
	rc := `{"version":1,"project":"p","kg":{"graph_home":"/tmp/custom-kg-home"},"sources":[{"type":"local"}]}`
	if err := os.WriteFile(filepath.Join(repo, ".agentsrc.json"), []byte(rc), 0644); err != nil {
		t.Fatal(err)
	}
	got := defaultGraphHome(repo)
	if got != "/tmp/custom-kg-home" {
		t.Fatalf("expected /tmp/custom-kg-home, got %s", got)
	}
}

func TestGraphSearchNoteEntry_BodyMatchesNoFrontmatter(t *testing.T) {
	graphHome := t.TempDir()
	sub := "entities"
	dir := filepath.Join(graphHome, "notes", sub)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "x.md"), []byte("just some lowercase text containing query word"), 0644); err != nil {
		t.Fatal(err)
	}
	result, id, ok := graphSearchNoteEntry(graphHome, sub, "x.md", "query")
	if !ok {
		t.Fatal("expected match")
	}
	if id != "x" {
		t.Fatalf("expected id derived from filename, got %s", id)
	}
	if result.ID != "x" {
		t.Fatalf("expected ID to fallback to filename, got %q", result.ID)
	}
}

func TestGraphSearchSubdir_CapStopsEarly(t *testing.T) {
	graphHome := t.TempDir()
	sub := "entities"
	dir := filepath.Join(graphHome, "notes", sub)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("n%d.md", i)), []byte("match content"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	results := []GraphBridgeResult{}
	graphSearchSubdir(graphHome, sub, "match", seen, &results, 2)
	if len(results) > 2 {
		t.Fatalf("cap 2 exceeded, got %d", len(results))
	}
}

func TestGraphSearchSubdir_MissingDirNoop(t *testing.T) {
	graphHome := t.TempDir()
	seen := map[string]bool{}
	results := []GraphBridgeResult{}
	graphSearchSubdir(graphHome, "nonexistent-sub", "q", seen, &results, 10)
	if len(results) != 0 {
		t.Fatalf("expected empty results, got %d", len(results))
	}
}

func TestLocalGraphAdapter_Query_UnsupportedIntent(t *testing.T) {
	a := NewLocalGraphAdapter(t.TempDir())
	_, err := a.Query(GraphBridgeQuery{Intent: "not-a-real-intent"})
	if err == nil || !strings.Contains(err.Error(), "unsupported bridge intent") {
		t.Fatalf("expected unsupported-intent error, got %v", err)
	}
}

func TestRunWorkflowGraphQuery_MissingIntent_Push8(t *testing.T) {
	repo := t.TempDir()
	chdirForCov(t, repo)
	cmd := &cobra.Command{}
	cmd.Flags().String("intent", "", "")
	cmd.Flags().String("scope", "", "")
	err := runWorkflowGraphQuery(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "--intent") {
		t.Fatalf("expected intent-required, got %v", err)
	}
}

func TestRunWorkflowGraphQuery_CodeBridgeIntent(t *testing.T) {
	repo := t.TempDir()
	chdirForCov(t, repo)
	cmd := &cobra.Command{}
	cmd.Flags().String("intent", "symbol_lookup", "")
	cmd.Flags().String("scope", "", "")

	saved := workflowDotAgentsExe
	workflowDotAgentsExe = func() (string, error) { return "", errors.New("synthetic") }
	t.Cleanup(func() { workflowDotAgentsExe = saved })
	err := runWorkflowGraphQuery(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "resolve da executable") {
		t.Fatalf("expected resolve-exe error, got %v", err)
	}
}

func TestRunWorkflowGraphHealth_JSON_Push8(t *testing.T) {
	repo := t.TempDir()
	chdirForCov(t, repo)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = false })
	out, err := captureCovStdout(t, func() error { return runWorkflowGraphHealth(nil, nil) })
	if err != nil {
		t.Fatalf("graph health: %v", err)
	}
	if !strings.Contains(out, `"status"`) {
		t.Fatalf("expected status field in JSON, got: %s", out)
	}
}

func TestRenderGraphQueryResults_TextMode(t *testing.T) {
	resp := GraphBridgeResponse{
		Results: []GraphBridgeResult{
			{Type: "decision", Title: "T1", Summary: "S1"},
		},
		Warnings: []string{"sparse"},
	}
	out, _ := captureCovStdout(t, func() error {
		renderGraphQueryResults("decision_lookup", "q", resp)
		return nil
	})
	if !strings.Contains(out, "decision") || !strings.Contains(out, "T1") {
		t.Errorf("expected results in output, got %s", out)
	}
}

func TestRenderGraphQueryResults_NoResults(t *testing.T) {
	out, _ := captureCovStdout(t, func() error {
		renderGraphQueryResults("plan_context", "q", GraphBridgeResponse{})
		return nil
	})
	if !strings.Contains(out, "No results") {
		t.Errorf("expected 'No results' in output, got %s", out)
	}
}

func TestRenderGraphQueryResults_JSON(t *testing.T) {
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = false })
	resp := GraphBridgeResponse{Intent: "plan_context", Query: "x"}
	out, _ := captureCovStdout(t, func() error {
		renderGraphQueryResults("plan_context", "x", resp)
		return nil
	})
	if !strings.Contains(out, "\"intent\"") {
		t.Errorf("expected JSON, got %s", out)
	}
}

func TestRunWorkflowGraphHealth(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	chdirForCov(t, dir)
	cmd := &cobra.Command{}
	out, err := captureCovStdout(t, func() error { return runWorkflowGraphHealth(cmd, nil) })
	if err != nil {
		t.Fatalf("runWorkflowGraphHealth: %v", err)
	}
	if !strings.Contains(out, "Graph Bridge Health") {
		t.Errorf("expected health header, got %s", out)
	}
}

func TestRunWorkflowGraphHealth_JSON(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	chdirForCov(t, dir)
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = false })
	cmd := &cobra.Command{}
	out, err := captureCovStdout(t, func() error { return runWorkflowGraphHealth(cmd, nil) })
	if err != nil {
		t.Fatalf("runWorkflowGraphHealth json: %v", err)
	}
	if !strings.Contains(out, "\"status\"") {
		t.Errorf("expected JSON status field, got %s", out)
	}
}

func TestReadGraphBridgeHealth_Missing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	h, err := readGraphBridgeHealth("nope-project")
	if err != nil {
		t.Fatalf("readGraphBridgeHealth: %v", err)
	}
	if h != nil {
		t.Errorf("expected nil for missing file, got %+v", h)
	}
}

func TestReadGraphBridgeHealth_Malformed(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", tmp)
	dir := filepath.Join(tmp, "context", "p")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "graph-bridge-health.json"), []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := readGraphBridgeHealth("p")
	if err == nil {
		t.Error("expected error for malformed JSON")
	}
}

func TestIsWorkflowGraphCodeBridgeIntent(t *testing.T) {
	for _, i := range []string{"symbol_lookup", "impact_radius", "callers_of", "callees_of",
		"community_context", "symbol_decisions", "decision_symbols", "change_analysis", "tests_for"} {
		if !isWorkflowGraphCodeBridgeIntent(i) {
			t.Errorf("expected %q to be code-bridge intent", i)
		}
	}
	if isWorkflowGraphCodeBridgeIntent("plan_context") {
		t.Error("plan_context should not be a code-bridge intent")
	}
}

func TestRunWorkflowGraphHealth_JSON_FromRepo(t *testing.T) {
	repo := setupTestProject(t)
	setupGraphHome(t, repo)
	chdirRepo(t, repo)

	workflowTestJSON = true
	defer func() { workflowTestJSON = false }()

	captureStdoutWhileRunning(t, repo, func() error {
		return runWorkflowGraphHealth(nil, nil)
	}, `"status"`)
}

func TestRunWorkflowGraphHealth_DegradedHuman(t *testing.T) {
	repo := setupTestProject(t)

	bridgeDir := filepath.Join(repo, ".agents", "workflow")
	if err := os.MkdirAll(bridgeDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bridgeDir, "graph-bridge.yaml"),
		[]byte("schema_version: 1\nenabled: true\ngraph_home: /no/such/path\n"), 0644); err != nil {
		t.Fatal(err)
	}
	chdirRepo(t, repo)
	captureStdoutWhileRunning(t, repo, func() error {
		return runWorkflowGraphHealth(nil, nil)
	}, "Graph Bridge Health")
}

func TestRunWorkflowGraphHealth_BadBridgeConfig(t *testing.T) {
	repo := setupTestProject(t)
	bridgeDir := filepath.Join(repo, ".agents", "workflow")
	if err := os.MkdirAll(bridgeDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(bridgeDir, "graph-bridge.yaml"),
		[]byte("not: valid: yaml:\nfoo bar:"), 0644); err != nil {
		t.Fatal(err)
	}
	chdirRepo(t, repo)
	err := runWorkflowGraphHealth(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "bridge config") {
		t.Fatalf("expected bridge-config error, got %v", err)
	}
}

func TestRunWorkflowGraphHealth_PartialStatusHuman(t *testing.T) {
	repo := setupTestProject(t)

	bridgeDir := filepath.Join(repo, ".agents", "workflow")
	if err := os.MkdirAll(bridgeDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bridgeDir, "graph-bridge.yaml"),
		[]byte("schema_version: 1\nenabled: true\ngraph_home: /no/such\n"), 0644); err != nil {
		t.Fatal(err)
	}
	chdirRepo(t, repo)
	out := captureStdoutToString(t, func() {
		_ = runWorkflowGraphHealth(nil, nil)
	})
	if !strings.Contains(out, "Graph Bridge Health") {
		t.Fatalf("expected header in output: %s", out)
	}
}

func TestRunWorkflowGraphQuery_BadBridgeConfig(t *testing.T) {
	repo := setupTestProject(t)
	bridgeDir := filepath.Join(repo, ".agents", "workflow")
	if err := os.MkdirAll(bridgeDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bridgeDir, "graph-bridge.yaml"),
		[]byte("not: valid: yaml:"), 0644); err != nil {
		t.Fatal(err)
	}
	err := executeWorkflowCommand(t, repo, "graph", "query",
		"--intent", "plan_context", "loop")
	if err == nil {
		t.Fatal("expected bridge config error")
	}
}

func TestRunWorkflowGraphHealth_GreenStatus(t *testing.T) {
	repo := setupTestProject(t)
	setupGraphHome(t, repo)
	chdirRepo(t, repo)

	if err := runWorkflowGraphHealth(nil, nil); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}
