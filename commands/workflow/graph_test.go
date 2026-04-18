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
	if strings.Contains(err.Error(), "Use `dot-agents kg bridge query") {
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

// ── Wave 6: Delegation & Merge-back ─────────────────────────────���────────────
