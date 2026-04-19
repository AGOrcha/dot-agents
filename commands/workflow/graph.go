package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/graphstore"
	"github.com/NikashPrakash/dot-agents/internal/ui"
	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
)

type ContextMapping struct {
	RepoScope  string `json:"repo_scope" yaml:"repo_scope"`
	GraphScope string `json:"graph_scope" yaml:"graph_scope"`
	Intent     string `json:"intent" yaml:"intent"`
}

type GraphBridgeConfig struct {
	SchemaVersion   int              `json:"schema_version" yaml:"schema_version"`
	Enabled         bool             `json:"enabled" yaml:"enabled"`
	GraphHome       string           `json:"graph_home" yaml:"graph_home"`
	AllowedIntents  []string         `json:"allowed_intents" yaml:"allowed_intents"`
	ContextMappings []ContextMapping `json:"context_mappings" yaml:"context_mappings"`
}

var validWorkflowBridgeIntents = map[string]bool{
	"plan_context":    true,
	"decision_lookup": true,
	"entity_context":  true,
	"workflow_memory": true,
	"contradictions":  true,
}

func isValidWorkflowBridgeIntent(intent string) bool { return validWorkflowBridgeIntents[intent] }

var workflowGraphCodeBridgeIntents = map[string]bool{
	"symbol_lookup":     true,
	"impact_radius":     true,
	"change_analysis":   true,
	"tests_for":         true,
	"callers_of":        true,
	"callees_of":        true,
	"community_context": true,
	"symbol_decisions":  true,
	"decision_symbols":  true,
}

func isWorkflowGraphCodeBridgeIntent(intent string) bool {
	return workflowGraphCodeBridgeIntents[intent]
}

var workflowDotAgentsExe = func() (string, error) {
	return os.Executable()
}

func runWorkflowGraphQueryViaKGBridge(projectPath, intent string, queryArgs []string) error {
	exe, err := workflowDotAgentsExe()
	if err != nil {
		return fmt.Errorf("resolve dot-agents executable: %w", err)
	}
	argv := []string{"kg", "bridge", "query", "--intent", intent}
	argv = append(argv, queryArgs...)
	if deps.Flags.JSON() {
		argv = append([]string{"--json"}, argv...)
	}
	cmd := exec.Command(exe, argv...)
	cmd.Dir = projectPath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("kg bridge query (via workflow graph query): %w", err)
	}
	return nil
}

// defaultGraphHome returns the default graph home path, preferring the
// agentsrc kg.graph_home field over the ~/.knowledge-graph default.
func defaultGraphHome(projectPath string) string {
	if rc, err := config.LoadAgentsRC(projectPath); err == nil && rc != nil && rc.KG != nil && rc.KG.GraphHome != "" {
		return rc.KG.GraphHome
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "knowledge-graph")
}

func loadGraphBridgeConfig(projectPath string) (*GraphBridgeConfig, error) {
	p := filepath.Join(projectPath, ".agents", "workflow", "graph-bridge.yaml")
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return &GraphBridgeConfig{Enabled: false}, nil
	}
	if err != nil {
		return nil, err
	}
	var cfg GraphBridgeConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse graph-bridge.yaml: %w", err)
	}
	// Resolve graph_home from agentsrc if not set in the file.
	if cfg.GraphHome == "" {
		cfg.GraphHome = defaultGraphHome(projectPath)
	}
	return &cfg, nil
}

// scaffoldGraphBridgeConfig creates a minimal .agents/workflow/graph-bridge.yaml
// with all defaults when the file is absent, so callers can proceed immediately.
func scaffoldGraphBridgeConfig(projectPath string) (*GraphBridgeConfig, error) {
	dir := filepath.Join(projectPath, ".agents", "workflow")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create .agents/workflow dir: %w", err)
	}
	graphHome := defaultGraphHome(projectPath)
	cfg := &GraphBridgeConfig{
		SchemaVersion: 1,
		Enabled:       true,
		GraphHome:     graphHome,
	}
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	p := filepath.Join(dir, "graph-bridge.yaml")
	if err := os.WriteFile(p, out, 0o644); err != nil {
		return nil, fmt.Errorf("write graph-bridge.yaml: %w", err)
	}
	return cfg, nil
}

type GraphBridgeQuery struct {
	Intent  string `json:"intent"`
	Project string `json:"project"`
	Scope   string `json:"scope,omitempty"`
	Query   string `json:"query"`
}

type GraphBridgeResult struct {
	ID         string   `json:"id"`
	Type       string   `json:"type"`
	Title      string   `json:"title"`
	Summary    string   `json:"summary"`
	Path       string   `json:"path"`
	SourceRefs []string `json:"source_refs,omitempty"`
}

type GraphBridgeResponse struct {
	SchemaVersion int                 `json:"schema_version"`
	Intent        string              `json:"intent"`
	Query         string              `json:"query"`
	Results       []GraphBridgeResult `json:"results"`
	Warnings      []string            `json:"warnings"`
	Provider      string              `json:"provider"`
	Timestamp     string              `json:"timestamp"`
}

type GraphBridgeAdapter interface {
	Query(query GraphBridgeQuery) (GraphBridgeResponse, error)
	Health() (GraphBridgeHealth, error)
}

type GraphBridgeHealth struct {
	SchemaVersion      int      `json:"schema_version"`
	Timestamp          string   `json:"timestamp"`
	AdapterAvailable   bool     `json:"adapter_available"`
	GraphHomeExists    bool     `json:"graph_home_exists"`
	NoteCount          int      `json:"note_count"`
	WarmStoreNodeCount int      `json:"warm_store_node_count"`
	WarmStoreNoteCount int      `json:"warm_store_note_count"`
	CodeLaneReady      bool     `json:"code_lane_ready"`
	ContextLaneReady   bool     `json:"context_lane_ready"`
	LastQueryTime      string   `json:"last_query_time,omitempty"`
	LastQueryStatus    string   `json:"last_query_status,omitempty"`
	Status             string   `json:"status"`
	Note               string   `json:"note,omitempty"`
	Warnings           []string `json:"warnings,omitempty"`
}

func writeGraphBridgeHealth(project string, health GraphBridgeHealth) error {
	dir := config.ProjectContextDir(project)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(health, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "graph-bridge-health.json"), data, 0644)
}

func readGraphBridgeHealth(project string) (*GraphBridgeHealth, error) {
	p := filepath.Join(config.ProjectContextDir(project), "graph-bridge-health.json")
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var h GraphBridgeHealth
	if err := json.Unmarshal(data, &h); err != nil {
		return nil, err
	}
	return &h, nil
}

type LocalGraphAdapter struct {
	graphHome  string
	lastQuery  string
	lastStatus string
}

func NewLocalGraphAdapter(graphHome string) *LocalGraphAdapter {
	return &LocalGraphAdapter{graphHome: graphHome}
}

func (a *LocalGraphAdapter) Health() (GraphBridgeHealth, error) {
	h := GraphBridgeHealth{
		SchemaVersion: 1,
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
	}
	info, err := os.Stat(a.graphHome)
	h.GraphHomeExists = err == nil && info.IsDir()
	configExists := false
	if _, err := os.Stat(filepath.Join(a.graphHome, "self", "config.yaml")); err == nil {
		configExists = true
	}
	h.AdapterAvailable = h.GraphHomeExists && configExists
	if !h.AdapterAvailable {
		h.Status = "degraded"
		h.Warnings = append(h.Warnings, fmt.Sprintf("graph not initialized at %s", a.graphHome))
		return h, nil
	}
	noteDirs := []string{"sources", "entities", "concepts", "synthesis", "decisions", "repos", "sessions"}
	for _, sub := range noteDirs {
		entries, err := os.ReadDir(filepath.Join(a.graphHome, "notes", sub))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
				h.NoteCount++
			}
		}
	}

	// Query warm store for node/note counts to report code-lane and context-lane readiness.
	warmDBPath := filepath.Join(a.graphHome, "ops", "graphstore.db")
	if store, err := graphstore.OpenSQLite(warmDBPath); err == nil {
		defer store.Close()
		h.WarmStoreNodeCount = store.CountNodes()
		h.WarmStoreNoteCount = store.CountKGNotes()
	}
	h.CodeLaneReady = h.WarmStoreNodeCount > 0
	h.ContextLaneReady = h.WarmStoreNoteCount > 0

	h.LastQueryTime = a.lastQuery
	h.LastQueryStatus = a.lastStatus
	switch {
	case h.CodeLaneReady && h.ContextLaneReady:
		h.Status = "healthy"
	case h.CodeLaneReady:
		h.Status = "partial"
		h.Note = "code-lane ready; context-lane needs KG notes (run 'kg warm' after authoring notes)"
	case h.ContextLaneReady:
		h.Status = "partial"
		h.Note = "context-lane ready; code-lane needs ETL (run 'kg warm --include-code' after 'kg build')"
	default:
		h.Status = "degraded"
		h.Note = "neither lane has data — run 'kg build' then 'kg warm --include-code' to populate code-lane"
	}
	return h, nil
}

func (a *LocalGraphAdapter) Query(query GraphBridgeQuery) (GraphBridgeResponse, error) {
	resp := GraphBridgeResponse{
		SchemaVersion: 1,
		Intent:        query.Intent,
		Query:         query.Query,
		Provider:      "local-graph",
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		Results:       []GraphBridgeResult{},
	}

	noteTypes := map[string][]string{
		"plan_context":    {"decisions", "synthesis"},
		"decision_lookup": {"decisions"},
		"entity_context":  {"entities"},
		"workflow_memory": {"sources", "sessions"},
		"contradictions":  {"decisions"},
	}
	subdirs, ok := noteTypes[query.Intent]
	if !ok {
		return resp, fmt.Errorf("unsupported bridge intent: %s", query.Intent)
	}

	seen := make(map[string]bool)
	q := strings.ToLower(query.Query)
	for _, sub := range subdirs {
		dir := filepath.Join(a.graphHome, "notes", sub)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			content := strings.ToLower(string(data))
			if q == "" || strings.Contains(content, q) {
				id, title, summary, srcRefs := parseNoteMetadata(string(data))
				if id == "" {
					id = strings.TrimSuffix(e.Name(), ".md")
				}
				if seen[id] {
					continue
				}
				seen[id] = true
				resp.Results = append(resp.Results, GraphBridgeResult{
					ID:         id,
					Type:       strings.TrimSuffix(sub, "s"),
					Title:      title,
					Summary:    summary,
					Path:       filepath.Join("notes", sub, e.Name()),
					SourceRefs: srcRefs,
				})
				if len(resp.Results) >= 10 {
					break
				}
			}
		}
	}

	a.lastQuery = time.Now().UTC().Format(time.RFC3339)
	a.lastStatus = "ok"
	return resp, nil
}

func parseNoteMetadata(content string) (id, title, summary string, sourceRefs []string) {
	if !strings.HasPrefix(content, "---") {
		return
	}
	rest := content[3:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return
	}
	fm := rest[:idx]
	for _, line := range strings.Split(fm, "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "id: "); ok {
			id = strings.Trim(after, "\"'")
		} else if after, ok := strings.CutPrefix(line, "title: "); ok {
			title = strings.Trim(after, "\"'")
		} else if after, ok := strings.CutPrefix(line, "summary: "); ok {
			summary = strings.Trim(after, "\"'")
		} else if after, ok := strings.CutPrefix(line, "- "); ok && strings.Contains(fm, "source_refs:") {
			sourceRefs = append(sourceRefs, strings.Trim(after, "\"'"))
		}
	}
	return
}

func runWorkflowGraphQuery(cmd *cobra.Command, args []string) error {
	projectPath, err := os.Getwd()
	if err != nil {
		return err
	}
	intent, _ := cmd.Flags().GetString("intent")
	if intent == "" {
		return deps.UsageError(
			"`--intent` is required",
			"Workflow graph queries require a bridge intent such as `plan_context` or `decision_lookup`.",
		)
	}
	scope, _ := cmd.Flags().GetString("scope")
	if isWorkflowGraphCodeBridgeIntent(intent) {
		return runWorkflowGraphQueryViaKGBridge(projectPath, intent, args)
	}
	cfg, err := loadGraphBridgeConfig(projectPath)
	if err != nil {
		return fmt.Errorf("load bridge config: %w", err)
	}
	if !cfg.Enabled {
		// Auto-scaffold a default config and continue rather than hard-failing.
		scaffolded, serr := scaffoldGraphBridgeConfig(projectPath)
		if serr != nil {
			return deps.ErrorWithHints(
				"graph bridge not configured",
				"Create `.agents/workflow/graph-bridge.yaml` with `enabled: true` to enable workflow graph queries.",
			)
		}
		cfg = scaffolded
		fmt.Fprintln(os.Stderr, "graph-bridge.yaml created with defaults — results may be sparse until the KG is populated")
	}

	if !isValidWorkflowBridgeIntent(intent) {
		return deps.ErrorWithHints(
			fmt.Sprintf("unknown intent %q", intent),
			"Valid workflow bridge intents: `plan_context`, `decision_lookup`, `entity_context`, `workflow_memory`, `contradictions`.",
		)
	}
	allowed := cfg.AllowedIntents
	if len(allowed) > 0 {
		ok := false
		for _, a := range allowed {
			if a == intent {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("intent %q not in allowed_intents for this repo", intent)
		}
	}

	query := strings.Join(args, " ")
	graphHome := cfg.GraphHome
	if graphHome == "" {
		home, _ := os.UserHomeDir()
		graphHome = filepath.Join(home, "knowledge-graph")
	}
	adapter := NewLocalGraphAdapter(graphHome)
	resp, err := adapter.Query(GraphBridgeQuery{
		Intent:  intent,
		Project: filepath.Base(projectPath),
		Scope:   scope,
		Query:   query,
	})
	if err != nil {
		return err
	}

	health, _ := adapter.Health()
	health.LastQueryTime = time.Now().UTC().Format(time.RFC3339)
	health.LastQueryStatus = "ok"
	_ = writeGraphBridgeHealth(filepath.Base(projectPath), health)

	if deps.Flags.JSON() {
		data, _ := json.MarshalIndent(resp, "", "  ")
		fmt.Println(string(data))
		return nil
	}
	ui.Header(fmt.Sprintf("Graph Query: %s  [%s]", intent, query))
	if len(resp.Results) == 0 {
		ui.Info("No results found.")
	} else {
		for _, r := range resp.Results {
			ui.Bullet("found", fmt.Sprintf("[%s] %s — %s", r.Type, r.Title, r.Summary))
		}
	}
	for _, w := range resp.Warnings {
		ui.Warn(w)
	}
	return nil
}

func runWorkflowGraphHealth(_ *cobra.Command, _ []string) error {
	projectPath, err := os.Getwd()
	if err != nil {
		return err
	}
	cfg, err := loadGraphBridgeConfig(projectPath)
	if err != nil {
		return fmt.Errorf("load bridge config: %w", err)
	}

	graphHome := cfg.GraphHome
	if graphHome == "" {
		graphHome = defaultGraphHome(projectPath)
	}
	adapter := NewLocalGraphAdapter(graphHome)
	health, err := adapter.Health()
	if err != nil {
		return err
	}
	_ = writeGraphBridgeHealth(filepath.Base(projectPath), health)

	if deps.Flags.JSON() {
		data, _ := json.MarshalIndent(health, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	statusColor := ui.Green
	if health.Status == "partial" {
		statusColor = ui.Yellow
	} else if health.Status == "degraded" {
		statusColor = ui.Red
	}
	badge := ui.ColorText(statusColor, health.Status)
	ui.Header(fmt.Sprintf("Graph Bridge Health  [%s]", badge))
	ui.Info(fmt.Sprintf("  Graph home:        %s", graphHome))
	ui.Info(fmt.Sprintf("  Adapter available: %v", health.AdapterAvailable))
	ui.Info(fmt.Sprintf("  Code-lane ready:   %v  (%d nodes in warm store)", health.CodeLaneReady, health.WarmStoreNodeCount))
	ui.Info(fmt.Sprintf("  Context-lane ready:%v  (%d notes in warm store)", health.ContextLaneReady, health.WarmStoreNoteCount))
	if health.Note != "" {
		ui.Warn(health.Note)
	}
	for _, w := range health.Warnings {
		ui.Warn(w)
	}
	return nil
}
