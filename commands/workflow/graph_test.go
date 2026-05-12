package workflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
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
