package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/ui"
	"go.yaml.in/yaml/v3"
)

// ScopeEvidence is the Go representation of a .scope.yaml sidecar file located at
// .agents/workflow/plans/<plan_id>/evidence/<task_id>.scope.yaml.
// All slice fields use []string{} (not nil) so JSON marshals to [] not null.
type ScopeEvidence struct {
	SchemaVersion      int                    `json:"schema_version"          yaml:"schema_version"`
	PlanID             string                 `json:"plan_id"                 yaml:"plan_id"`
	TaskID             string                 `json:"task_id"                 yaml:"task_id"`
	Status             string                 `json:"status"                  yaml:"status"`
	Mode               string                 `json:"mode,omitempty"          yaml:"mode,omitempty"`
	Goal               string                 `json:"goal,omitempty"          yaml:"goal,omitempty"`
	Confidence         string                 `json:"confidence"              yaml:"confidence"`
	DecisionLocks      []string               `json:"decision_locks"          yaml:"decision_locks"`
	RequiredReads      []ScopeRequiredRead    `json:"required_reads"          yaml:"required_reads"`
	Seeds              *ScopeSeeds            `json:"seeds,omitempty"         yaml:"seeds,omitempty"`
	Queries            []ScopeQuery           `json:"queries"                 yaml:"queries"`
	RequiredPaths      []ScopePath            `json:"required_paths"          yaml:"required_paths"`
	OptionalPaths      []ScopePath            `json:"optional_paths"          yaml:"optional_paths"`
	ExcludedPaths      []ScopeExcludedPath    `json:"excluded_paths"          yaml:"excluded_paths"`
	Provides           []string               `json:"provides"                yaml:"provides"`
	Consumes           []string               `json:"consumes"                yaml:"consumes"`
	FinalWriteScope    []string               `json:"final_write_scope"       yaml:"final_write_scope"`
	VerificationFocus  []string               `json:"verification_focus"      yaml:"verification_focus"`
	AllowedLocalChoices []string              `json:"allowed_local_choices"   yaml:"allowed_local_choices"`
	StopConditions     []string               `json:"stop_conditions"         yaml:"stop_conditions"`
	OpenGaps           []string               `json:"open_gaps"               yaml:"open_gaps"`
}

// ScopeRequiredRead is an entry in ScopeEvidence.RequiredReads.
type ScopeRequiredRead struct {
	Path string `json:"path" yaml:"path"`
	Why  string `json:"why"  yaml:"why"`
}

// ScopeSeeds captures the starting symbols or paths the planner identified.
type ScopeSeeds struct {
	Symbols   []string `json:"symbols,omitempty"   yaml:"symbols,omitempty"`
	Paths     []string `json:"paths,omitempty"     yaml:"paths,omitempty"`
	Rationale []string `json:"rationale,omitempty" yaml:"rationale,omitempty"`
}

// ScopeQuerySummary holds the result files returned by a graph query.
type ScopeQuerySummary struct {
	Files []string `json:"files" yaml:"files"`
}

// ScopeQuery represents a single graph query run during scope derivation.
type ScopeQuery struct {
	Tool    string             `json:"tool"              yaml:"tool"`
	Kind    string             `json:"kind"              yaml:"kind"`
	Intent  string             `json:"intent"            yaml:"intent"`
	Subject string             `json:"subject"           yaml:"subject"`
	Summary *ScopeQuerySummary `json:"summary,omitempty" yaml:"summary,omitempty"`
}

// ScopePath is a required or optional path entry with explanatory reasons.
type ScopePath struct {
	Path    string   `json:"path"    yaml:"path"`
	Because []string `json:"because" yaml:"because"`
}

// ScopeExcludedPath is a path intentionally excluded from write_scope.
type ScopeExcludedPath struct {
	Path      string   `json:"path"      yaml:"path"`
	Rationale []string `json:"rationale" yaml:"rationale"`
}

// NewScopeEvidence returns a ScopeEvidence with all slice fields initialized to
// empty slices so they marshal to [] rather than null.
func NewScopeEvidence(planID, taskID string) *ScopeEvidence {
	return &ScopeEvidence{
		SchemaVersion:       1,
		PlanID:              planID,
		TaskID:              taskID,
		Status:              "draft",
		Confidence:          "low",
		DecisionLocks:       []string{},
		RequiredReads:       []ScopeRequiredRead{},
		Queries:             []ScopeQuery{},
		RequiredPaths:       []ScopePath{},
		OptionalPaths:       []ScopePath{},
		ExcludedPaths:       []ScopeExcludedPath{},
		Provides:            []string{},
		Consumes:            []string{},
		FinalWriteScope:     []string{},
		VerificationFocus:   []string{},
		AllowedLocalChoices: []string{},
		StopConditions:      []string{},
		OpenGaps:            []string{},
	}
}

// deriveScopeEvidencePath returns the canonical sidecar output path for a task.
func deriveScopeEvidencePath(projectPath, planID, taskID string) string {
	return filepath.Join(plansBaseDir(projectPath), planID, "evidence", taskID+".scope.yaml")
}

// runWorkflowPlanDeriveScope implements `workflow plan derive-scope <plan_id> <task_id>`.
// It runs scope-lane and context-lane query bundles against the KG/CRG graph and
// writes a candidate .scope.yaml sidecar. Degrades gracefully to confidence:low
// when the graph is not ready. Does NOT auto-edit TASKS.yaml.
func runWorkflowPlanDeriveScope(planID, taskID string, seedSymbols, seedPaths []string) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	projectPath := project.Path

	// Load the task to derive mode and goal from notes.
	tf, err := loadCanonicalTasks(projectPath, planID)
	if err != nil {
		return fmt.Errorf("tasks for plan %q not found: %w", planID, err)
	}
	var task *CanonicalTask
	for i := range tf.Tasks {
		if tf.Tasks[i].ID == taskID {
			task = &tf.Tasks[i]
			break
		}
	}
	if task == nil {
		return fmt.Errorf("task %q not found in plan %q", taskID, planID)
	}

	// Determine mode from task notes or app_type heuristic.
	mode := deriveScopeMode(task)

	// Check graph health to decide confidence and whether to run scope-lane queries.
	cfg, _ := loadGraphBridgeConfig(projectPath)
	if cfg == nil {
		cfg = &GraphBridgeConfig{Enabled: false}
	}
	graphHome := cfg.GraphHome
	if graphHome == "" {
		graphHome = defaultGraphHome(projectPath)
	}
	adapter := NewLocalGraphAdapter(graphHome)
	health, _ := adapter.Health()

	ev := NewScopeEvidence(planID, taskID)
	ev.Mode = mode
	ev.Goal = strings.TrimSpace(task.Notes)
	if len(ev.Goal) > 120 {
		ev.Goal = ev.Goal[:120] + "…"
	}

	// Populate seeds from flags.
	if len(seedSymbols) > 0 || len(seedPaths) > 0 {
		ev.Seeds = &ScopeSeeds{
			Symbols: append([]string{}, seedSymbols...),
			Paths:   append([]string{}, seedPaths...),
		}
	}

	// Populate write_scope from TASKS.yaml as a baseline for required_paths.
	for _, p := range task.WriteScope {
		ev.RequiredPaths = append(ev.RequiredPaths, ScopePath{
			Path:    p,
			Because: []string{"listed in TASKS.yaml write_scope"},
		})
	}

	// Determine confidence based on graph health and seeds.
	hasScopeInputs := len(seedSymbols) > 0 || len(seedPaths) > 0
	codeReady := health.CodeLaneReady
	contextReady := health.ContextLaneReady

	// Run scope-lane queries only for code mode when graph is ready and seeds exist.
	var scopeWarnings []string
	if mode == "code" && codeReady && hasScopeInputs {
		queryFiles := deriveScopeRunScopeLane(projectPath, seedSymbols, seedPaths, ev)
		_ = queryFiles
	} else if mode != "code" {
		scopeWarnings = append(scopeWarnings, "scope-lane graph queries skipped (mode: "+mode+")")
	} else if !codeReady {
		scopeWarnings = append(scopeWarnings, "scope-lane graph queries skipped (code-lane not ready; run 'kg build' then 'kg warm --include-code')")
	} else if !hasScopeInputs {
		scopeWarnings = append(scopeWarnings, "scope-lane graph queries skipped (no --seed-symbol or --seed-path provided)")
	}

	// Run context-lane queries for plan_context and decision_lookup.
	if contextReady {
		deriveScopeRunContextLane(planID, taskID, adapter, ev)
	} else {
		scopeWarnings = append(scopeWarnings, "context-lane queries skipped (context-lane not ready; run 'kg warm' after authoring notes)")
	}

	// Set confidence.
	ev.Confidence = deriveScopeConfidence(mode, codeReady, contextReady, hasScopeInputs, len(ev.Queries))
	if len(scopeWarnings) > 0 {
		ev.OpenGaps = append(ev.OpenGaps, scopeWarnings...)
	}

	// Write sidecar.
	outPath := deriveScopeEvidencePath(projectPath, planID, taskID)
	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return fmt.Errorf("create evidence dir: %w", err)
	}
	data, err := yaml.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal sidecar: %w", err)
	}
	if err := os.WriteFile(outPath, data, 0644); err != nil {
		return fmt.Errorf("write sidecar: %w", err)
	}

	if deps.Flags.JSON() {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(ev)
	}
	ui.Success(fmt.Sprintf("Wrote scope evidence sidecar: %s", config.DisplayPath(outPath)))
	fmt.Fprintf(os.Stdout, "  confidence: %s\n", ev.Confidence)
	fmt.Fprintf(os.Stdout, "  required_paths: %d  queries: %d\n", len(ev.RequiredPaths), len(ev.Queries))
	if len(scopeWarnings) > 0 {
		for _, w := range scopeWarnings {
			ui.Warn(w)
		}
	}
	fmt.Fprintln(os.Stdout)
	return nil
}

// deriveScopeMode derives the task mode from app_type and notes heuristics.
func deriveScopeMode(task *CanonicalTask) string {
	if task.AppType != "" {
		// Any declared app_type implies code mode.
		return "code"
	}
	notes := strings.ToLower(task.Notes)
	// Research/doc markers in notes.
	if strings.Contains(notes, "research task") || strings.Contains(notes, "no go code") ||
		strings.Contains(notes, "doc only") || strings.Contains(notes, "docs only") ||
		strings.Contains(notes, "skill instruction") {
		return "research"
	}
	// Check write_scope: if it only contains non-Go paths it's likely doc/research.
	allDocs := len(task.WriteScope) > 0
	for _, p := range task.WriteScope {
		if strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "/") ||
			strings.HasPrefix(p, "commands/") || strings.HasPrefix(p, "internal/") {
			allDocs = false
			break
		}
	}
	if allDocs && len(task.WriteScope) > 0 {
		return "doc"
	}
	return "code"
}

// deriveScopeConfidence calculates a confidence level string based on lane readiness.
func deriveScopeConfidence(mode string, codeReady, contextReady, hasScopeInputs bool, queryCount int) string {
	if mode != "code" {
		if contextReady {
			return "medium"
		}
		return "low"
	}
	// code mode
	switch {
	case codeReady && hasScopeInputs && queryCount > 0:
		return "medium"
	case codeReady && hasScopeInputs:
		return "medium"
	case codeReady || contextReady:
		return "low"
	default:
		return "low"
	}
}

// deriveScopeRunScopeLane runs symbol_lookup, callers_of, and impact_radius queries
// for all provided seed symbols and seed paths, populating ev.Queries and ev.RequiredPaths.
func deriveScopeRunScopeLane(projectPath string, seedSymbols, seedPaths []string, ev *ScopeEvidence) []string {
	var allFiles []string
	seen := make(map[string]bool)
	addFiles := func(files []string) {
		for _, f := range files {
			if !seen[f] {
				seen[f] = true
				allFiles = append(allFiles, f)
			}
		}
	}

	// symbol_lookup and callers_of for each seed symbol.
	for _, sym := range seedSymbols {
		for _, intent := range []string{"symbol_lookup", "callers_of"} {
			files := deriveScopeKGBridgeQuery(projectPath, intent, sym)
			q := ScopeQuery{
				Tool:    "kg",
				Kind:    "bridge_query",
				Intent:  intent,
				Subject: sym,
			}
			if len(files) > 0 {
				q.Summary = &ScopeQuerySummary{Files: files}
				addFiles(files)
			}
			ev.Queries = append(ev.Queries, q)
		}
	}

	// impact_radius for each seed path.
	for _, p := range seedPaths {
		files := deriveScopeKGBridgeQuery(projectPath, "impact_radius", p)
		q := ScopeQuery{
			Tool:    "kg",
			Kind:    "bridge_query",
			Intent:  "impact_radius",
			Subject: p,
		}
		if len(files) > 0 {
			q.Summary = &ScopeQuerySummary{Files: files}
			addFiles(files)
		}
		ev.Queries = append(ev.Queries, q)
	}

	// Merge query-discovered files into required_paths (dedup against existing).
	existingPaths := make(map[string]bool)
	for _, rp := range ev.RequiredPaths {
		existingPaths[rp.Path] = true
	}
	for _, f := range allFiles {
		if !existingPaths[f] {
			ev.RequiredPaths = append(ev.RequiredPaths, ScopePath{
				Path:    f,
				Because: []string{"discovered via scope-lane graph query"},
			})
			existingPaths[f] = true
		}
	}
	return allFiles
}

// deriveScopeKGBridgeQuery runs one kg bridge query subcommand and returns the
// list of file paths extracted from the JSON response. Returns nil on any error
// (graceful degradation).
func deriveScopeKGBridgeQuery(projectPath, intent, subject string) []string {
	exe, err := workflowDotAgentsExe()
	if err != nil {
		return nil
	}
	argv := []string{"--json", "kg", "bridge", "query", "--intent", intent, subject}
	cmd := exec.Command(exe, argv...)
	cmd.Dir = projectPath
	cmd.Env = os.Environ()
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	// Parse the JSON response to extract file paths from results.
	var resp struct {
		Results []struct {
			Path     string `json:"path"`
			FilePath string `json:"file_path"`
		} `json:"results"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil
	}
	seen := make(map[string]bool)
	var files []string
	for _, r := range resp.Results {
		p := r.FilePath
		if p == "" {
			p = r.Path
		}
		if p != "" && !seen[p] {
			seen[p] = true
			files = append(files, p)
		}
	}
	return files
}

// deriveScopeRunContextLane runs plan_context and decision_lookup queries against
// the context-lane and populates ev.RequiredReads with the results.
func deriveScopeRunContextLane(planID, taskID string, adapter *LocalGraphAdapter, ev *ScopeEvidence) {
	for _, intent := range []string{"plan_context", "decision_lookup"} {
		q := strings.TrimSpace(planID + " " + taskID)
		resp, err := adapter.Query(GraphBridgeQuery{
			Intent: intent,
			Query:  q,
		})
		if err != nil {
			continue
		}
		sq := ScopeQuery{
			Tool:    "kg",
			Kind:    "bridge_query",
			Intent:  intent,
			Subject: q,
		}
		if len(resp.Results) > 0 {
			var files []string
			for _, r := range resp.Results {
				if r.Path != "" {
					files = append(files, r.Path)
				}
			}
			if len(files) > 0 {
				sq.Summary = &ScopeQuerySummary{Files: files}
			}
			// Add as required_reads entries.
			for _, r := range resp.Results {
				if r.Path != "" {
					ev.RequiredReads = append(ev.RequiredReads, ScopeRequiredRead{
						Path: r.Path,
						Why:  r.Title + " — " + r.Summary,
					})
				}
			}
		}
		ev.Queries = append(ev.Queries, sq)
	}
}

func loadCanonicalPlan(projectPath, planID string) (*CanonicalPlan, error) {
	path := filepath.Join(plansBaseDir(projectPath), planID, "PLAN.yaml")
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var plan CanonicalPlan
	if err := yaml.Unmarshal(content, &plan); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &plan, nil
}

func saveCanonicalPlan(projectPath string, plan *CanonicalPlan) error {
	dir := filepath.Join(plansBaseDir(projectPath), plan.ID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	content, err := yaml.Marshal(plan)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "PLAN.yaml"), content, 0644)
}

func loadCanonicalTasks(projectPath, planID string) (*CanonicalTaskFile, error) {
	path := filepath.Join(plansBaseDir(projectPath), planID, "TASKS.yaml")
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var tf CanonicalTaskFile
	if err := yaml.Unmarshal(content, &tf); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &tf, nil
}

func loadCanonicalSlices(projectPath, planID string) (*CanonicalSliceFile, error) {
	path := filepath.Join(plansBaseDir(projectPath), planID, "SLICES.yaml")
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var sf CanonicalSliceFile
	if err := yaml.Unmarshal(content, &sf); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &sf, nil
}

func saveCanonicalTasks(projectPath string, tf *CanonicalTaskFile) error {
	dir := filepath.Join(plansBaseDir(projectPath), tf.PlanID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	content, err := yaml.Marshal(tf)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "TASKS.yaml"), content, 0644)
}

func collectCanonicalPlans(projectPath string) ([]workflowCanonicalPlanSummary, []string) {
	ids, err := listCanonicalPlanIDs(projectPath)
	if err != nil {
		return nil, []string{"canonical plans unreadable: " + err.Error()}
	}
	var summaries []workflowCanonicalPlanSummary
	var warnings []string
	for _, id := range ids {
		plan, err := loadCanonicalPlan(projectPath, id)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("plan %s unreadable: %v", id, err))
			continue
		}
		summary := workflowCanonicalPlanSummary{
			ID:               plan.ID,
			Title:            plan.Title,
			Status:           plan.Status,
			CurrentFocusTask: plan.CurrentFocusTask,
		}
		if tf, err := loadCanonicalTasks(projectPath, id); err == nil {
			summary.CurrentFocusTask = effectivePlanFocusTask(tf.Tasks)
			for _, t := range tf.Tasks {
				switch t.Status {
				case "pending", "in_progress":
					summary.PendingCount++
				case "blocked":
					summary.BlockedCount++
				case "completed":
					summary.CompletedCount++
				}
			}
		}
		summaries = append(summaries, summary)
	}
	if summaries == nil {
		summaries = []workflowCanonicalPlanSummary{}
	}
	return summaries, warnings
}

func isValidPlanStatus(s string) bool {
	switch s {
	case "draft", "active", "paused", "completed", "archived":
		return true
	default:
		return false
	}
}

func isValidTaskStatus(s string) bool {
	switch s {
	case "pending", "in_progress", "blocked", "completed", "cancelled":
		return true
	default:
		return false
	}
}

func runWorkflowPlanList() error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	ids, err := listCanonicalPlanIDs(project.Path)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		fmt.Fprintln(os.Stdout, "No canonical plans found.")
		fmt.Fprintf(os.Stdout, "  Create one at: %s\n", config.DisplayPath(filepath.Join(plansBaseDir(project.Path), "<plan-id>", "PLAN.yaml")))
		return nil
	}
	if deps.Flags.JSON() {
		summaries, _ := collectCanonicalPlans(project.Path)
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(summaries)
	}
	ui.Header("Canonical Plans")
	for _, id := range ids {
		plan, err := loadCanonicalPlan(project.Path, id)
		if err != nil {
			fmt.Fprintf(os.Stdout, "  %s (unreadable: %v)\n", id, err)
			continue
		}
		focus := ""
		if plan.CurrentFocusTask != "" {
			focus = "  focus: " + plan.CurrentFocusTask
		}
		fmt.Fprintf(os.Stdout, "  [%s] %s (%s)%s\n", plan.ID, plan.Title, plan.Status, focus)
	}
	fmt.Fprintln(os.Stdout)
	return nil
}

func runWorkflowPlanShow(planID string) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	plan, err := loadCanonicalPlan(project.Path, planID)
	if err != nil {
		return fmt.Errorf("plan %q not found: %w", planID, err)
	}
	tf, tasksErr := loadCanonicalTasks(project.Path, planID)
	sf, slicesErr := loadCanonicalSlices(project.Path, planID)

	if deps.Flags.JSON() {
		out := map[string]interface{}{"plan": plan}
		if tasksErr == nil {
			out["tasks"] = tf
		}
		if slicesErr == nil {
			out["slices"] = sf
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}

	ui.Header(plan.Title)
	ui.Section("Plan")
	fmt.Fprintf(os.Stdout, "  id: %s\n", plan.ID)
	fmt.Fprintf(os.Stdout, "  status: %s\n", plan.Status)
	fmt.Fprintf(os.Stdout, "  created: %s\n", plan.CreatedAt)
	fmt.Fprintf(os.Stdout, "  updated: %s\n", plan.UpdatedAt)
	if plan.Owner != "" {
		fmt.Fprintf(os.Stdout, "  owner: %s\n", plan.Owner)
	}
	if plan.Summary != "" {
		fmt.Fprintf(os.Stdout, "  summary: %s\n", plan.Summary)
	}
	if plan.SuccessCriteria != "" {
		fmt.Fprintf(os.Stdout, "  success criteria: %s\n", plan.SuccessCriteria)
	}
	if plan.CurrentFocusTask != "" {
		fmt.Fprintf(os.Stdout, "  focus task: %s\n", plan.CurrentFocusTask)
	}
	fmt.Fprintln(os.Stdout)

	if tasksErr != nil {
		fmt.Fprintln(os.Stdout, "  (no TASKS.yaml found)")
		return nil
	}

	var pending, blocked, completed, total int
	for _, t := range tf.Tasks {
		total++
		switch t.Status {
		case "pending", "in_progress":
			pending++
		case "blocked":
			blocked++
		case "completed":
			completed++
		}
	}
	ui.Section("Tasks")
	fmt.Fprintf(os.Stdout, "  total: %d   pending: %d   blocked: %d   completed: %d\n\n", total, pending, blocked, completed)
	for _, t := range tf.Tasks {
		marker := " "
		switch t.Status {
		case "completed":
			marker = "✓"
		case "in_progress":
			marker = "▶"
		case "blocked":
			marker = "✗"
		}
		fmt.Fprintf(os.Stdout, "  [%s] %s  %s\n", marker, t.ID, t.Title)
	}
	fmt.Fprintln(os.Stdout)
	if slicesErr == nil {
		ui.Section("Slices")
		fmt.Fprintf(os.Stdout, "  total: %d\n\n", len(sf.Slices))
		for _, slice := range sf.Slices {
			fmt.Fprintf(os.Stdout, "  [%s] %s  (%s)  task: %s\n", slice.ID, slice.Title, slice.Status, slice.ParentTaskID)
		}
	}
	fmt.Fprintln(os.Stdout)
	return nil
}

type workflowPlanGraphNode struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	PlanID string `json:"plan_id,omitempty"`
	TaskID string `json:"task_id,omitempty"`
	Label  string `json:"label"`
	Status string `json:"status,omitempty"`
}

type workflowPlanGraphEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Type string `json:"type"`
}

type workflowPlanGraph struct {
	PlanFilter string                  `json:"plan_filter,omitempty"`
	Nodes      []workflowPlanGraphNode `json:"nodes"`
	Edges      []workflowPlanGraphEdge `json:"edges"`
	Warnings   []string                `json:"warnings,omitempty"`
}

func runWorkflowPlanGraph(planID string) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}

	graph, err := buildWorkflowPlanGraph(project.Path, planID)
	if err != nil {
		return err
	}

	if deps.Flags.JSON() {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(graph)
	}

	title := "Canonical Plan Graph"
	if planID != "" {
		title += ": " + planID
	}
	ui.Header(title)

	nodeByID := make(map[string]workflowPlanGraphNode, len(graph.Nodes))
	for _, node := range graph.Nodes {
		nodeByID[node.ID] = node
	}

	for _, node := range graph.Nodes {
		if node.Kind != "plan" {
			continue
		}
		fmt.Fprintf(os.Stdout, "  [%s] %s (%s)\n", strings.TrimPrefix(node.ID, "plan:"), node.Label, node.Status)
		for _, edge := range graph.Edges {
			if edge.Type != "contains" || edge.From != node.ID {
				continue
			}
			taskNode := workflowPlanGraphNode{}
			found := false
			for _, candidate := range graph.Nodes {
				if candidate.ID == edge.To {
					taskNode = candidate
					found = true
					break
				}
			}
			if !found {
				continue
			}
			fmt.Fprintf(os.Stdout, "      -> [%s] %s (%s)\n", strings.TrimPrefix(strings.TrimPrefix(taskNode.ID, "task:"+taskNode.PlanID+"/"), "task:"), taskNode.Label, taskNode.Status)
			for _, taskEdge := range graph.Edges {
				if taskEdge.From == taskNode.ID && taskEdge.Type == "contains" {
					sliceNode, ok := nodeByID[taskEdge.To]
					if ok && sliceNode.Kind == "slice" {
						fmt.Fprintf(os.Stdout, "         => [%s] %s (%s)\n", strings.TrimPrefix(strings.TrimPrefix(sliceNode.ID, "slice:"+sliceNode.PlanID+"/"), "slice:"), sliceNode.Label, sliceNode.Status)
						for _, sliceEdge := range graph.Edges {
							if sliceEdge.From != sliceNode.ID || sliceEdge.Type != "depends_on" {
								continue
							}
							targetLabel := sliceEdge.To
							if targetNode, ok := nodeByID[sliceEdge.To]; ok {
								targetLabel = targetNode.Label
							}
							fmt.Fprintf(os.Stdout, "            depends_on: %s\n", targetLabel)
						}
					}
				}
				if taskEdge.From != taskNode.ID || (taskEdge.Type != "depends_on" && taskEdge.Type != "blocks") {
					continue
				}
				targetLabel := taskEdge.To
				if targetNode, ok := nodeByID[taskEdge.To]; ok {
					targetLabel = targetNode.Label
				}
				fmt.Fprintf(os.Stdout, "         %s: %s\n", taskEdge.Type, targetLabel)
			}
		}
	}

	for _, warning := range graph.Warnings {
		ui.Warn(warning)
	}
	fmt.Fprintln(os.Stdout)
	return nil
}

func buildWorkflowPlanGraph(projectPath, planID string) (*workflowPlanGraph, error) {
	ids, err := listCanonicalPlanIDs(projectPath)
	if err != nil {
		return nil, err
	}
	if planID != "" {
		found := false
		for _, id := range ids {
			if id == planID {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("plan %q not found", planID)
		}
		ids = []string{planID}
	}

	graph := &workflowPlanGraph{
		PlanFilter: planID,
		Nodes:      []workflowPlanGraphNode{},
		Edges:      []workflowPlanGraphEdge{},
		Warnings:   []string{},
	}

	for _, id := range ids {
		plan, err := loadCanonicalPlan(projectPath, id)
		if err != nil {
			return nil, fmt.Errorf("load plan %q: %w", id, err)
		}
		tf, err := loadCanonicalTasks(projectPath, id)
		if err != nil {
			return nil, fmt.Errorf("load tasks for plan %q: %w", id, err)
		}
		sf, slicesErr := loadCanonicalSlices(projectPath, id)
		if slicesErr != nil && !os.IsNotExist(slicesErr) {
			return nil, fmt.Errorf("load slices for plan %q: %w", id, slicesErr)
		}

		planNodeID := "plan:" + plan.ID
		graph.Nodes = append(graph.Nodes, workflowPlanGraphNode{
			ID:     planNodeID,
			Kind:   "plan",
			Label:  plan.Title,
			Status: plan.Status,
		})

		taskIDs := make(map[string]string, len(tf.Tasks))
		for _, task := range tf.Tasks {
			taskNodeID := "task:" + plan.ID + "/" + task.ID
			taskIDs[task.ID] = taskNodeID
			graph.Nodes = append(graph.Nodes, workflowPlanGraphNode{
				ID:     taskNodeID,
				Kind:   "task",
				PlanID: plan.ID,
				Label:  task.Title,
				Status: task.Status,
			})
			graph.Edges = append(graph.Edges, workflowPlanGraphEdge{
				From: planNodeID,
				To:   taskNodeID,
				Type: "contains",
			})
		}

		if slicesErr == nil {
			sliceIDs := make(map[string]string, len(sf.Slices))
			for _, slice := range sf.Slices {
				parentTaskNodeID, ok := taskIDs[slice.ParentTaskID]
				if !ok {
					graph.Warnings = append(graph.Warnings, fmt.Sprintf("plan %s slice %s references unknown parent task %s", plan.ID, slice.ID, slice.ParentTaskID))
					continue
				}
				sliceNodeID := "slice:" + plan.ID + "/" + slice.ID
				sliceIDs[slice.ID] = sliceNodeID
				graph.Nodes = append(graph.Nodes, workflowPlanGraphNode{
					ID:     sliceNodeID,
					Kind:   "slice",
					PlanID: plan.ID,
					TaskID: slice.ParentTaskID,
					Label:  slice.Title,
					Status: slice.Status,
				})
				graph.Edges = append(graph.Edges, workflowPlanGraphEdge{
					From: parentTaskNodeID,
					To:   sliceNodeID,
					Type: "contains",
				})
			}
			for _, slice := range sf.Slices {
				fromID, ok := sliceIDs[slice.ID]
				if !ok {
					continue
				}
				for _, dep := range slice.DependsOn {
					toID, ok := sliceIDs[dep]
					if !ok {
						graph.Warnings = append(graph.Warnings, fmt.Sprintf("plan %s slice %s depends on unknown slice %s", plan.ID, slice.ID, dep))
						continue
					}
					graph.Edges = append(graph.Edges, workflowPlanGraphEdge{
						From: fromID,
						To:   toID,
						Type: "depends_on",
					})
				}
			}
		}

		for _, task := range tf.Tasks {
			fromID := taskIDs[task.ID]
			for _, dep := range task.DependsOn {
				toID, ok := taskIDs[dep]
				if !ok {
					graph.Warnings = append(graph.Warnings, fmt.Sprintf("plan %s task %s depends on unknown task %s", plan.ID, task.ID, dep))
					continue
				}
				graph.Edges = append(graph.Edges, workflowPlanGraphEdge{
					From: fromID,
					To:   toID,
					Type: "depends_on",
				})
			}
			for _, blocked := range task.Blocks {
				toID, ok := taskIDs[blocked]
				if !ok {
					graph.Warnings = append(graph.Warnings, fmt.Sprintf("plan %s task %s blocks unknown task %s", plan.ID, task.ID, blocked))
					continue
				}
				graph.Edges = append(graph.Edges, workflowPlanGraphEdge{
					From: fromID,
					To:   toID,
					Type: "blocks",
				})
			}
		}
	}

	return graph, nil
}

func runWorkflowTasks(planID string) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	if _, err := loadCanonicalPlan(project.Path, planID); err != nil {
		return fmt.Errorf("plan %q not found: %w", planID, err)
	}
	tf, err := loadCanonicalTasks(project.Path, planID)
	if err != nil {
		return fmt.Errorf("tasks for plan %q not found: %w", planID, err)
	}
	if deps.Flags.JSON() {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(tf)
	}
	ui.Header("Tasks: " + planID)
	for _, t := range tf.Tasks {
		depsLabel := ""
		if len(t.DependsOn) > 0 {
			depsLabel = "  depends: " + strings.Join(t.DependsOn, ", ")
		}
		fmt.Fprintf(os.Stdout, "  [%s] %s  (%s)%s\n", t.ID, t.Title, t.Status, depsLabel)
		if t.Notes != "" {
			fmt.Fprintf(os.Stdout, "      note: %s\n", t.Notes)
		}
	}
	fmt.Fprintln(os.Stdout)
	return nil
}

func runWorkflowSlices(planID string) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	if _, err := loadCanonicalPlan(project.Path, planID); err != nil {
		return fmt.Errorf("plan %q not found: %w", planID, err)
	}
	sf, err := loadCanonicalSlices(project.Path, planID)
	if err != nil {
		return fmt.Errorf("slices for plan %q not found: %w", planID, err)
	}
	if deps.Flags.JSON() {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(sf)
	}
	ui.Header("Slices: " + planID)
	for _, slice := range sf.Slices {
		depsLabel := ""
		if len(slice.DependsOn) > 0 {
			depsLabel = "  depends: " + strings.Join(slice.DependsOn, ", ")
		}
		fmt.Fprintf(os.Stdout, "  [%s] %s  (%s)  task: %s%s\n", slice.ID, slice.Title, slice.Status, slice.ParentTaskID, depsLabel)
		if slice.Summary != "" {
			fmt.Fprintf(os.Stdout, "      summary: %s\n", slice.Summary)
		}
		if len(slice.WriteScope) > 0 {
			fmt.Fprintf(os.Stdout, "      write scope: %s\n", strings.Join(slice.WriteScope, ", "))
		}
		if slice.VerificationFocus != "" {
			fmt.Fprintf(os.Stdout, "      verification: %s\n", slice.VerificationFocus)
		}
	}
	fmt.Fprintln(os.Stdout)
	return nil
}

type workflowNextTaskSuggestion struct {
	PlanID               string   `json:"plan_id"`
	PlanTitle            string   `json:"plan_title"`
	TaskID               string   `json:"task_id"`
	TaskTitle            string   `json:"task_title"`
	Status               string   `json:"status"`
	Reason               string   `json:"reason"`
	WriteScope           []string `json:"write_scope,omitempty"`
	VerificationRequired bool     `json:"verification_required"`
	DependsOn            []string `json:"depends_on,omitempty"`
	AppType              string   `json:"app_type,omitempty"`
	ConflictsWith        []string `json:"conflicts_with"`
}

// AnnotatedTask enriches a workflowNextTaskSuggestion with conflict detection
// and evidence fields populated by computeWriteScopeConflicts and the eligible command.
// All slice fields are initialized to []string{} (not nil) so they marshal to [] not null.
type AnnotatedTask struct {
	workflowNextTaskSuggestion
	ConflictsWith      []string `json:"conflicts_with"`
	HasEvidence        bool     `json:"has_evidence"`
	EvidenceConfidence string   `json:"evidence_confidence"`
	WriteScopeDeclared bool     `json:"write_scope_declared"`
}

// eligibleOutput is the full JSON output of `workflow eligible`.
type eligibleOutput struct {
	EligibleTasks []AnnotatedTask     `json:"eligible_tasks"`
	MaxBatch      []string            `json:"max_batch"`
	ConflictGraph map[string][]string `json:"conflict_graph"`
	TotalEligible int                 `json:"total_eligible"`
	MaxParallel   int                 `json:"max_parallel"`
}

// writeScopeConflictResult is the output of computeWriteScopeConflicts.
// All slice/map fields use non-nil zero values per the additive struct pattern.
type writeScopeConflictResult struct {
	EligibleTasks []workflowNextTaskSuggestion `json:"eligible_tasks"`
	MaxBatch      []string                     `json:"max_batch"`
	ConflictGraph map[string][]string          `json:"conflict_graph"`
}

// writeScopesConflict reports whether two write_scope lists overlap.
// Two scopes conflict when any path in one is a prefix of (or equal to) any path in the other.
// Prefix matching is directory-aware: "commands/workflow/" conflicts with
// "commands/workflow/plan_task.go", and exact matches also conflict.
// Uses the package-level scopePathsOverlap (defined in delegation.go).
func writeScopesConflict(a, b []string) bool {
	for _, pa := range a {
		for _, pb := range b {
			if scopePathsOverlap(pa, pb) {
				return true
			}
		}
	}
	return false
}

// computeWriteScopeConflicts annotates each task in the input slice with
// ConflictsWith (the IDs of other tasks whose write_scope overlaps its own),
// then computes the MaxNonConflictingBatch (the largest subset of tasks with
// zero pairwise conflicts, greedy by input order) and builds a ConflictGraph.
//
// Schema rule: ConflictsWith per task is []string{} not nil. MaxBatch is []string{} not nil.
// ConflictGraph values are []string{} not nil for every key.
func computeWriteScopeConflicts(tasks []workflowNextTaskSuggestion) writeScopeConflictResult {
	n := len(tasks)

	// Initialize ConflictsWith on every task to []string{} per schema rule.
	for i := range tasks {
		tasks[i].ConflictsWith = []string{}
	}

	// Build conflict graph: for each pair (i,j) check scope overlap.
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if writeScopesConflict(tasks[i].WriteScope, tasks[j].WriteScope) {
				tasks[i].ConflictsWith = append(tasks[i].ConflictsWith, tasks[j].TaskID)
				tasks[j].ConflictsWith = append(tasks[j].ConflictsWith, tasks[i].TaskID)
			}
		}
	}

	// Build ConflictGraph map from populated ConflictsWith slices.
	conflictGraph := make(map[string][]string, n)
	for _, t := range tasks {
		if t.ConflictsWith == nil {
			conflictGraph[t.TaskID] = []string{}
		} else {
			conflictGraph[t.TaskID] = t.ConflictsWith
		}
	}

	// Greedy MaxNonConflictingBatch: iterate in input order, add a task if it
	// does not conflict with any already-included task.
	inBatch := make(map[string]bool, n)
	maxBatch := []string{}
	for _, t := range tasks {
		canAdd := true
		for _, conflictID := range t.ConflictsWith {
			if inBatch[conflictID] {
				canAdd = false
				break
			}
		}
		if canAdd {
			inBatch[t.TaskID] = true
			maxBatch = append(maxBatch, t.TaskID)
		}
	}

	return writeScopeConflictResult{
		EligibleTasks: tasks,
		MaxBatch:      maxBatch,
		ConflictGraph: conflictGraph,
	}
}

type workflowCompletionScopeState struct {
	Scope       []string                    `json:"scope"`
	State       string                      `json:"state"`
	Next        *workflowNextTaskSuggestion `json:"next,omitempty"`
	PausedPlans []string                    `json:"paused_plans,omitempty"`
	LockedPlans []string                    `json:"locked_plans,omitempty"`
}

func runWorkflowNext(explicitPlanID string) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}

	suggestion, err := selectNextCanonicalTask(project.Path, explicitPlanID)
	if err != nil {
		return err
	}
	if suggestion == nil {
		fmt.Fprintln(os.Stdout, "No actionable canonical task found.")
		fmt.Fprintln(os.Stdout, "  Active plans are completed, blocked by dependencies, already delegated, or missing TASKS.yaml.")
		return nil
	}

	if deps.Flags.JSON() {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(suggestion)
	}

	ui.Header("Next Canonical Task")
	fmt.Fprintf(os.Stdout, "  plan: %s  [%s]\n", suggestion.PlanTitle, suggestion.PlanID)
	fmt.Fprintf(os.Stdout, "  task: %s  [%s]\n", suggestion.TaskTitle, suggestion.TaskID)
	fmt.Fprintf(os.Stdout, "  status: %s\n", suggestion.Status)
	fmt.Fprintf(os.Stdout, "  reason: %s\n", suggestion.Reason)
	if len(suggestion.DependsOn) > 0 {
		fmt.Fprintf(os.Stdout, "  depends on: %s\n", strings.Join(suggestion.DependsOn, ", "))
	}
	if len(suggestion.WriteScope) > 0 {
		fmt.Fprintf(os.Stdout, "  write scope: %s\n", strings.Join(suggestion.WriteScope, ", "))
	}
	if suggestion.VerificationRequired {
		fmt.Fprintln(os.Stdout, "  verification: required")
	} else {
		fmt.Fprintln(os.Stdout, "  verification: optional")
	}
	fmt.Fprintln(os.Stdout)
	return nil
}

// annotateEligibleTasks enriches a slice of workflowNextTaskSuggestion with
// conflict detection (ConflictsWith), evidence sidecar data (HasEvidence,
// EvidenceConfidence), and write_scope_declared.
func annotateEligibleTasks(projectPath string, tasks []workflowNextTaskSuggestion) []AnnotatedTask {
	// Run conflict detection first (populates ConflictsWith on each task in-place).
	conflictResult := computeWriteScopeConflicts(tasks)

	annotated := make([]AnnotatedTask, len(conflictResult.EligibleTasks))
	for i, t := range conflictResult.EligibleTasks {
		at := AnnotatedTask{
			workflowNextTaskSuggestion: t,
			ConflictsWith:              t.ConflictsWith,
			WriteScopeDeclared:         len(t.WriteScope) > 0,
		}

		// Check for evidence sidecar.
		sidecarPath := deriveScopeEvidencePath(projectPath, t.PlanID, t.TaskID)
		data, err := os.ReadFile(sidecarPath)
		if err == nil {
			at.HasEvidence = true
			// Parse confidence from sidecar.
			var ev struct {
				Confidence string `yaml:"confidence"`
			}
			if parseErr := yaml.Unmarshal(data, &ev); parseErr == nil && ev.Confidence != "" {
				at.EvidenceConfidence = ev.Confidence
			} else {
				at.EvidenceConfidence = "none"
			}
		} else {
			at.HasEvidence = false
			at.EvidenceConfidence = "none"
		}

		annotated[i] = at
	}
	return annotated
}

// runWorkflowEligible implements `workflow eligible`: lists all unblocked tasks
// across active plans, annotated with conflict detection and evidence data.
func runWorkflowEligible(planFilter string, limit int) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}

	planIDs := parsePlanIDFilter(planFilter)
	tasks, err := selectAllEligibleTasks(project.Path, planIDs)
	if err != nil {
		return err
	}

	// Apply --limit if set.
	if limit > 0 && len(tasks) > limit {
		tasks = tasks[:limit]
	}

	annotated := annotateEligibleTasks(project.Path, tasks)

	// Re-derive conflict graph and max batch from the annotated slice.
	// We need to re-run conflict detection on the (possibly truncated) set.
	taskSuggestions := make([]workflowNextTaskSuggestion, len(annotated))
	for i, at := range annotated {
		taskSuggestions[i] = at.workflowNextTaskSuggestion
	}
	conflictResult := computeWriteScopeConflicts(taskSuggestions)

	// Sync ConflictsWith back to annotated (truncated slice may differ).
	for i := range annotated {
		if i < len(conflictResult.EligibleTasks) {
			annotated[i].ConflictsWith = conflictResult.EligibleTasks[i].ConflictsWith
		}
	}

	out := eligibleOutput{
		EligibleTasks: annotated,
		MaxBatch:      conflictResult.MaxBatch,
		ConflictGraph: conflictResult.ConflictGraph,
		TotalEligible: len(annotated),
		MaxParallel:   len(conflictResult.MaxBatch),
	}
	if out.EligibleTasks == nil {
		out.EligibleTasks = []AnnotatedTask{}
	}

	if deps.Flags.JSON() {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}

	// Human-readable output.
	ui.Header("Eligible Tasks")
	for _, at := range out.EligibleTasks {
		scopeStr := strings.Join(at.WriteScope, ", ")
		if !at.WriteScopeDeclared {
			scopeStr = "(none) [no write_scope declared]"
		}
		conflictsStr := ""
		if len(at.ConflictsWith) > 0 {
			conflictsStr = "  conflicts: " + strings.Join(at.ConflictsWith, ", ")
		}
		evidenceStr := fmt.Sprintf("  evidence: %s (confidence: %s)", fmt.Sprintf("%v", at.HasEvidence), at.EvidenceConfidence)
		fmt.Fprintf(os.Stdout, "  [%s/%s] %s  (%s)\n", at.PlanID, at.TaskID, at.TaskTitle, at.Status)
		fmt.Fprintf(os.Stdout, "      scope: %s\n", scopeStr)
		fmt.Fprintf(os.Stdout, "     %s\n", evidenceStr)
		if conflictsStr != "" {
			fmt.Fprintf(os.Stdout, "     %s\n", conflictsStr)
		}
	}
	fmt.Fprintln(os.Stdout)
	maxWorkers := 1 // default until p4 wires the pref
	fmt.Fprintf(os.Stdout, "%d tasks eligible, %d can run in parallel (limited by max_parallel_workers=%d)\n",
		out.TotalEligible, out.MaxParallel, maxWorkers)
	if len(out.MaxBatch) > 0 {
		fmt.Fprintf(os.Stdout, "  max batch: %s\n", strings.Join(out.MaxBatch, ", "))
	}
	fmt.Fprintln(os.Stdout)
	return nil
}

func runWorkflowComplete(explicitPlanID string) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	if strings.TrimSpace(explicitPlanID) == "" {
		return fmt.Errorf("--plan must not be empty")
	}
	completion, err := collectWorkflowCompletionState(project.Path, explicitPlanID)
	if err != nil {
		return err
	}
	if deps.Flags.JSON() {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(completion)
	}

	ui.Header("Scoped Plan Completion")
	fmt.Fprintf(os.Stdout, "  scope: %s\n", strings.Join(completion.Scope, ", "))
	fmt.Fprintf(os.Stdout, "  state: %s\n", completion.State)
	if completion.Next != nil {
		fmt.Fprintf(os.Stdout, "  next: %s  [%s]\n", completion.Next.TaskTitle, completion.Next.TaskID)
		fmt.Fprintf(os.Stdout, "  plan: %s  [%s]\n", completion.Next.PlanTitle, completion.Next.PlanID)
		fmt.Fprintf(os.Stdout, "  reason: %s\n", completion.Next.Reason)
	}
	if len(completion.PausedPlans) > 0 {
		fmt.Fprintf(os.Stdout, "  paused plans: %s\n", strings.Join(completion.PausedPlans, ", "))
	}
	if len(completion.LockedPlans) > 0 {
		fmt.Fprintf(os.Stdout, "  locked plans: %s\n", strings.Join(completion.LockedPlans, ", "))
	}
	fmt.Fprintln(os.Stdout)
	return nil
}

func parsePlanIDFilter(planFilter string) []string {
	planFilter = strings.TrimSpace(planFilter)
	if planFilter == "" {
		return nil
	}

	seen := make(map[string]bool)
	ids := make([]string, 0, 4)
	for _, raw := range strings.Split(planFilter, ",") {
		id := strings.TrimSpace(raw)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids
}

func filterPlanIDsUnlocked(ids []string, locked map[string]bool) []string {
	if len(locked) == 0 {
		return ids
	}
	var out []string
	for _, id := range ids {
		if !locked[id] {
			out = append(out, id)
		}
	}
	return out
}

// activeDelegationPlanIDs returns plan ids that currently have a pending or active delegation.
func activeDelegationPlanIDs(delegations []DelegationContract) map[string]bool {
	m := make(map[string]bool)
	for _, c := range delegations {
		if c.Status != "pending" && c.Status != "active" {
			continue
		}
		if c.ParentPlanID != "" {
			m[c.ParentPlanID] = true
		}
	}
	return m
}

func filterPlanIDsLocked(ids []string, locked map[string]bool) []string {
	if len(locked) == 0 {
		return ids
	}
	var out []string
	for _, id := range ids {
		if locked[id] {
			out = append(out, id)
		}
	}
	return out
}

func collectWorkflowCompletionState(projectPath string, explicitPlanID string) (*workflowCompletionScopeState, error) {
	ids, err := listCanonicalPlanIDs(projectPath)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return &workflowCompletionScopeState{
			Scope: []string{},
			State: "drained",
		}, nil
	}

	scopeIDs := parsePlanIDFilter(explicitPlanID)
	if len(scopeIDs) > 0 {
		available := make(map[string]bool, len(ids))
		for _, id := range ids {
			available[id] = true
		}
		filtered := make([]string, 0, len(scopeIDs))
		for _, id := range scopeIDs {
			if !available[id] {
				return nil, fmt.Errorf("plan %q not found", id)
			}
			filtered = append(filtered, id)
		}
		scopeIDs = filtered
	}

	delegations, err := listDelegationContracts(projectPath)
	if err != nil {
		return nil, err
	}
	lockedPlans := activeDelegationPlanIDs(delegations)
	pausedPlans := make([]string, 0, len(scopeIDs))
	lockedScopePlans := make([]string, 0, len(scopeIDs))
	for _, id := range scopeIDs {
		plan, err := loadCanonicalPlan(projectPath, id)
		if err != nil {
			return nil, fmt.Errorf("load plan %q: %w", id, err)
		}
		switch plan.Status {
		case "paused":
			pausedPlans = append(pausedPlans, id)
		case "active":
			if lockedPlans[id] {
				lockedScopePlans = append(lockedScopePlans, id)
			}
		}
	}
	sort.Strings(pausedPlans)
	sort.Strings(lockedScopePlans)

	suggestion, err := selectNextCanonicalTask(projectPath, explicitPlanID)
	if err != nil {
		return nil, err
	}

	state := "drained"
	switch {
	case suggestion != nil:
		state = "actionable"
	case len(pausedPlans) > 0:
		state = "paused"
	case len(lockedScopePlans) > 0:
		state = "locked"
	}

	return &workflowCompletionScopeState{
		Scope:       scopeIDs,
		State:       state,
		Next:        suggestion,
		PausedPlans: pausedPlans,
		LockedPlans: lockedScopePlans,
	}, nil
}

func selectNextCanonicalTask(projectPath string, explicitPlanID string) (*workflowNextTaskSuggestion, error) {
	ids, err := listCanonicalPlanIDs(projectPath)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}

	explicitPlanID = strings.TrimSpace(explicitPlanID)
	planFilter := parsePlanIDFilter(explicitPlanID)
	if explicitPlanID != "" && len(planFilter) == 0 {
		return nil, nil
	}
	if len(planFilter) > 0 {
		available := make(map[string]bool, len(ids))
		for _, id := range ids {
			available[id] = true
		}
		for _, id := range planFilter {
			if !available[id] {
				return nil, fmt.Errorf("plan %q not found", id)
			}
		}
	}

	// Apply delegation-based plan locking: when no explicit plan is given, only
	// consider plans that are locked (have active delegations). When explicit, only
	// consider plans that are NOT locked.
	delegations, err := listDelegationContracts(projectPath)
	if err != nil {
		return nil, err
	}
	lockedPlans := activeDelegationPlanIDs(delegations)

	effectiveIDs := ids
	if len(planFilter) > 0 {
		effectiveIDs = planFilter
		effectiveIDs = filterPlanIDsUnlocked(effectiveIDs, lockedPlans)
	} else {
		effectiveIDs = filterPlanIDsLocked(effectiveIDs, lockedPlans)
	}
	if len(effectiveIDs) == 0 {
		return nil, nil
	}

	// Use selectAllEligibleTasks scoped to effectiveIDs to get the candidate pool.
	candidates, err := selectAllEligibleTasks(projectPath, effectiveIDs)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, nil
	}

	// Re-rank candidates by priority using focus-task information.
	type ranked struct {
		s        workflowNextTaskSuggestion
		priority int
	}
	var best *ranked
	for _, sug := range candidates {
		plan, loadErr := loadCanonicalPlan(projectPath, sug.PlanID)
		r := ranked{s: sug, priority: 3}
		if loadErr == nil {
			switch {
			case sug.Status == "in_progress" && plan.CurrentFocusTask == sug.TaskTitle:
				r.priority = 0
				r.s.Reason = "current focus task is already in progress"
			case sug.Status == "in_progress":
				r.priority = 1
				r.s.Reason = "task is already in progress and unblocked"
			case plan.CurrentFocusTask == sug.TaskTitle:
				r.priority = 2
				r.s.Reason = "current focus task is pending and all dependencies are complete"
			default:
				r.priority = 3
				r.s.Reason = "first pending unblocked task in an active canonical plan"
			}
		}
		if best == nil || r.priority < best.priority {
			tmp := r
			best = &tmp
		}
	}

	if best == nil {
		return nil, nil
	}
	return &best.s, nil
}

func incompleteCanonicalDependencies(tasks []CanonicalTask, deps []string) []string {
	if len(deps) == 0 {
		return nil
	}

	statusByID := make(map[string]string, len(tasks))
	for _, task := range tasks {
		statusByID[task.ID] = task.Status
	}

	var incomplete []string
	for _, dep := range deps {
		if statusByID[dep] != "completed" {
			incomplete = append(incomplete, dep)
		}
	}
	return incomplete
}

// incompleteCanonicalDependenciesCrossplan checks whether all deps are satisfied,
// resolving cross-plan references (entries containing "/") by loading the referenced
// plan's TASKS.yaml. Intra-plan deps are checked against localTasks.
// If a cross-plan reference cannot be loaded, it is treated as unsatisfied and a
// warning is appended to warnings.
func incompleteCanonicalDependenciesCrossplan(projectPath string, localTasks []CanonicalTask, deps []string, warnings *[]string) []string {
	if len(deps) == 0 {
		return nil
	}

	statusByID := make(map[string]string, len(localTasks))
	for _, task := range localTasks {
		statusByID[task.ID] = task.Status
	}

	// Cache loaded cross-plan task files to avoid redundant IO.
	crossPlanCache := make(map[string]*CanonicalTaskFile)

	var incomplete []string
	for _, dep := range deps {
		if !strings.Contains(dep, "/") {
			// Intra-plan dependency.
			if statusByID[dep] != "completed" {
				incomplete = append(incomplete, dep)
			}
			continue
		}

		// Cross-plan dependency: format is "<planID>/<taskID>".
		slashIdx := strings.Index(dep, "/")
		refPlanID := dep[:slashIdx]
		refTaskID := dep[slashIdx+1:]

		tf, ok := crossPlanCache[refPlanID]
		if !ok {
			var loadErr error
			tf, loadErr = loadCanonicalTasks(projectPath, refPlanID)
			if loadErr != nil {
				if warnings != nil {
					*warnings = append(*warnings, fmt.Sprintf("cross-plan dep %q: cannot load plan %q tasks: %v", dep, refPlanID, loadErr))
				}
				incomplete = append(incomplete, dep)
				crossPlanCache[refPlanID] = nil // cache miss marker
				continue
			}
			crossPlanCache[refPlanID] = tf
		}
		if tf == nil {
			// Already failed to load; treat as unsatisfied.
			incomplete = append(incomplete, dep)
			continue
		}

		found := false
		satisfied := false
		for _, t := range tf.Tasks {
			if t.ID == refTaskID {
				found = true
				satisfied = t.Status == "completed"
				break
			}
		}
		if !found {
			if warnings != nil {
				*warnings = append(*warnings, fmt.Sprintf("cross-plan dep %q: task %q not found in plan %q", dep, refTaskID, refPlanID))
			}
			incomplete = append(incomplete, dep)
			continue
		}
		if !satisfied {
			incomplete = append(incomplete, dep)
		}
	}
	return incomplete
}

// selectAllEligibleTasks returns ALL unblocked pending/in_progress tasks across active
// plans, optionally filtered to the plans named in planFilter (comma-separated IDs or
// a pre-split []string passed directly). Tasks are excluded when:
//   - their plan has status != "active"
//   - the task has an active delegation lock (pending or active DelegationContract)
//   - any dependency is incomplete (intra-plan or cross-plan)
//
// The returned slice is ordered: in_progress before pending, then by plan order and
// task order within each plan. Each entry carries the same workflowNextTaskSuggestion
// shape that selectNextCanonicalTask uses. Returns an empty slice (not nil) when no
// eligible tasks exist.
func selectAllEligibleTasks(projectPath string, planFilter []string) ([]workflowNextTaskSuggestion, error) {
	ids, err := listCanonicalPlanIDs(projectPath)
	if err != nil {
		return []workflowNextTaskSuggestion{}, err
	}
	if len(ids) == 0 {
		return []workflowNextTaskSuggestion{}, nil
	}

	// Apply plan filter if provided.
	if len(planFilter) > 0 {
		filterSet := make(map[string]bool, len(planFilter))
		for _, id := range planFilter {
			filterSet[id] = true
		}
		filtered := ids[:0]
		for _, id := range ids {
			if filterSet[id] {
				filtered = append(filtered, id)
			}
		}
		ids = filtered
		// Validate that every requested plan exists.
		for _, wantID := range planFilter {
			found := false
			for _, id := range ids {
				if id == wantID {
					found = true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("plan %q not found", wantID)
			}
		}
	}

	// Build active-delegation lookup keyed by task ID.
	delegations, err := listDelegationContracts(projectPath)
	if err != nil {
		return []workflowNextTaskSuggestion{}, err
	}
	activeDelegations := make(map[string]bool, len(delegations))
	for _, c := range delegations {
		if c.Status == "pending" || c.Status == "active" {
			activeDelegations[c.ParentTaskID] = true
		}
	}

	var eligible []workflowNextTaskSuggestion
	for _, id := range ids {
		plan, err := loadCanonicalPlan(projectPath, id)
		if err != nil || plan.Status != "active" {
			continue
		}
		tf, err := loadCanonicalTasks(projectPath, id)
		if err != nil {
			continue
		}
		for _, task := range tf.Tasks {
			if task.Status != "pending" && task.Status != "in_progress" {
				continue
			}
			if activeDelegations[task.ID] {
				continue
			}
			var depWarnings []string
			if len(incompleteCanonicalDependenciesCrossplan(projectPath, tf.Tasks, task.DependsOn, &depWarnings)) > 0 {
				continue
			}
			ws := task.WriteScope
			if ws == nil {
				ws = []string{}
			}
			eligible = append(eligible, workflowNextTaskSuggestion{
				PlanID:               plan.ID,
				PlanTitle:            plan.Title,
				TaskID:               task.ID,
				TaskTitle:            task.Title,
				Status:               task.Status,
				Reason:               "eligible: active plan, unblocked, no active delegation",
				WriteScope:           append([]string(nil), ws...),
				VerificationRequired: task.VerificationRequired,
				DependsOn:            append([]string(nil), task.DependsOn...),
				AppType:              task.AppType,
			})
		}
	}

	// Stable sort: in_progress before pending, preserve declaration order otherwise.
	sort.SliceStable(eligible, func(i, j int) bool {
		si, sj := eligible[i].Status, eligible[j].Status
		if si == sj {
			return false
		}
		return si == "in_progress"
	})

	if eligible == nil {
		eligible = []workflowNextTaskSuggestion{}
	}
	return eligible, nil
}

// effectivePlanFocusTask returns the title that should represent plan focus for orient/status.
func effectivePlanFocusTask(tasks []CanonicalTask) string {
	var lastInProgress string
	for _, t := range tasks {
		if t.Status == "in_progress" {
			lastInProgress = strings.TrimSpace(t.Title)
		}
	}
	if lastInProgress != "" {
		return lastInProgress
	}
	for _, t := range tasks {
		if t.Status != "pending" {
			continue
		}
		if len(incompleteCanonicalDependencies(tasks, t.DependsOn)) > 0 {
			continue
		}
		return strings.TrimSpace(t.Title)
	}
	return ""
}

func runWorkflowAdvance(planID, taskID, newStatus string) error {
	if !isValidTaskStatus(newStatus) {
		return deps.ErrorWithHints(
			fmt.Sprintf("invalid task status %q", newStatus),
			"Valid values: `pending`, `in_progress`, `blocked`, `completed`, `cancelled`.",
		)
	}
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	tf, err := loadCanonicalTasks(project.Path, planID)
	if err != nil {
		return fmt.Errorf("tasks for plan %q not found: %w", planID, err)
	}
	found := false
	var taskTitle string
	for i, t := range tf.Tasks {
		if t.ID == taskID {
			tf.Tasks[i].Status = newStatus
			taskTitle = t.Title
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("task %q not found in plan %q", taskID, planID)
	}
	if err := saveCanonicalTasks(project.Path, tf); err != nil {
		return err
	}
	plan, err := loadCanonicalPlan(project.Path, planID)
	if err != nil {
		return err
	}
	plan.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if newStatus == "in_progress" {
		plan.CurrentFocusTask = strings.TrimSpace(taskTitle)
	} else {
		plan.CurrentFocusTask = effectivePlanFocusTask(tf.Tasks)
	}
	if err := saveCanonicalPlan(project.Path, plan); err != nil {
		return err
	}
	ui.Success(fmt.Sprintf("Task %q advanced to %q", taskTitle, newStatus))
	return nil
}

func runWorkflowPlanCreate(planID, title, summary, owner, successCriteria, verificationStrategy string) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	dir := filepath.Join(plansBaseDir(project.Path), planID)
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("plan %q already exists at %s", planID, config.DisplayPath(dir))
	}
	now := time.Now().UTC().Format(time.RFC3339)
	plan := &CanonicalPlan{
		SchemaVersion:        1,
		ID:                   planID,
		Title:                title,
		Status:               "draft",
		Summary:              summary,
		Owner:                owner,
		SuccessCriteria:      successCriteria,
		VerificationStrategy: verificationStrategy,
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	if err := saveCanonicalPlan(project.Path, plan); err != nil {
		return err
	}
	tf := &CanonicalTaskFile{
		SchemaVersion: 1,
		PlanID:        planID,
		Tasks:         []CanonicalTask{},
	}
	if err := saveCanonicalTasks(project.Path, tf); err != nil {
		return err
	}
	ui.Success(fmt.Sprintf("Created plan %q at %s", planID, config.DisplayPath(dir)))
	return nil
}

// runWorkflowPlanArchive archives one or more plans (comma-separated planIDs) by
// merging each plan source directory into .agents/history/<planID>/ and stamping
// the PLAN.yaml status=archived before the move.
//
// Guard: plan status must be "completed" unless --force is set.
// Bulk: each plan is archived in sequence; a failure for one plan is logged and
// iteration continues.
func runWorkflowPlanArchive(projectPath string, planIDs []string, force, dryRun bool) error {
	var firstErr error
	for _, planID := range planIDs {
		if err := archiveSinglePlan(projectPath, planID, force, dryRun); err != nil {
			fmt.Fprintf(os.Stderr, "archive plan %q: %v\n", planID, err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func archiveSinglePlan(projectPath, planID string, force, dryRun bool) error {
	plan, err := loadCanonicalPlan(projectPath, planID)
	if err != nil {
		return fmt.Errorf("plan %q not found: %w", planID, err)
	}

	// Guard: status must be completed (or --force).
	if plan.Status != "completed" && !force {
		return deps.ErrorWithHints(
			fmt.Sprintf("plan %q has status %q (expected completed)", planID, plan.Status),
			"Use --force to archive regardless of status.",
		)
	}

	srcDir := filepath.Join(plansBaseDir(projectPath), planID)
	dstDir := filepath.Join(historyBaseDir(projectPath), planID)

	// Stamp status=archived + updated_at BEFORE move.
	if !dryRun {
		plan.Status = "archived"
		plan.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		if err := saveCanonicalPlan(projectPath, plan); err != nil {
			return fmt.Errorf("stamp archived status: %w", err)
		}
	} else {
		fmt.Printf("  [dry-run] stamp %s status=archived\n", planID)
	}

	// Merge or rename into history.
	if err := mergeWorkflowPlanDir(planID, srcDir, dstDir, dryRun); err != nil {
		return fmt.Errorf("merge plan dir: %w", err)
	}

	// Remove the source directory after a successful merge.
	if !dryRun {
		if err := removeAllWithRetry(srcDir); err != nil {
			return fmt.Errorf("remove source dir %s: %w", srcDir, err)
		}
		ui.Success(fmt.Sprintf("Archived plan %q to %s", planID, config.DisplayPath(dstDir)))
	} else {
		fmt.Printf("  [dry-run] remove source dir %s\n", srcDir)
	}

	return nil
}

func runWorkflowPlanUpdate(planID, status, title, summary, focus, successCriteria, verificationStrategy string) error {
	if status != "" && !isValidPlanStatus(status) {
		return deps.ErrorWithHints(
			fmt.Sprintf("invalid plan status %q", status),
			"Valid values: `draft`, `active`, `paused`, `completed`, `archived`.",
		)
	}
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	plan, err := loadCanonicalPlan(project.Path, planID)
	if err != nil {
		return fmt.Errorf("plan %q not found: %w", planID, err)
	}
	if status != "" {
		plan.Status = status
	}
	if title != "" {
		plan.Title = title
	}
	if summary != "" {
		plan.Summary = summary
	}
	if successCriteria != "" {
		plan.SuccessCriteria = successCriteria
	}
	if verificationStrategy != "" {
		plan.VerificationStrategy = verificationStrategy
	}
	if focus != "" {
		plan.CurrentFocusTask = focus
	}
	plan.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := saveCanonicalPlan(project.Path, plan); err != nil {
		return err
	}
	ui.Success(fmt.Sprintf("Updated plan %q", planID))
	return nil
}

func runWorkflowTaskAdd(planID, taskID, title, notes, owner, dependsOn, blocks, writeScope, appType string, verificationRequired bool) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	tf, err := loadCanonicalTasks(project.Path, planID)
	if err != nil {
		return fmt.Errorf("tasks for plan %q not found: %w", planID, err)
	}
	for _, t := range tf.Tasks {
		if t.ID == taskID {
			return fmt.Errorf("task %q already exists in plan %q", taskID, planID)
		}
	}
	task := CanonicalTask{
		ID:                   taskID,
		Title:                title,
		Status:               "pending",
		Owner:                owner,
		Notes:                notes,
		AppType:              appType,
		VerificationRequired: verificationRequired,
	}
	if dependsOn != "" {
		for _, id := range strings.Split(dependsOn, ",") {
			if id = strings.TrimSpace(id); id != "" {
				task.DependsOn = append(task.DependsOn, id)
			}
		}
	}
	if blocks != "" {
		for _, id := range strings.Split(blocks, ",") {
			if id = strings.TrimSpace(id); id != "" {
				task.Blocks = append(task.Blocks, id)
			}
		}
	}
	if writeScope != "" {
		for _, p := range strings.Split(writeScope, ",") {
			if p = strings.TrimSpace(p); p != "" {
				task.WriteScope = append(task.WriteScope, p)
			}
		}
	}
	tf.Tasks = append(tf.Tasks, task)
	if err := saveCanonicalTasks(project.Path, tf); err != nil {
		return err
	}
	plan, err := loadCanonicalPlan(project.Path, planID)
	if err != nil {
		return err
	}
	plan.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	_ = saveCanonicalPlan(project.Path, plan)
	ui.Success(fmt.Sprintf("Added task %q to plan %q", taskID, planID))
	return nil
}

func runWorkflowTaskUpdate(planID, taskID, title, notes, writeScope string) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	tf, err := loadCanonicalTasks(project.Path, planID)
	if err != nil {
		return fmt.Errorf("tasks for plan %q not found: %w", planID, err)
	}
	found := false
	for i, t := range tf.Tasks {
		if t.ID != taskID {
			continue
		}
		if title != "" {
			tf.Tasks[i].Title = title
		}
		if notes != "" {
			tf.Tasks[i].Notes = notes
		}
		if writeScope != "" {
			var scope []string
			for _, p := range strings.Split(writeScope, ",") {
				if p = strings.TrimSpace(p); p != "" {
					scope = append(scope, p)
				}
			}
			tf.Tasks[i].WriteScope = scope
		}
		found = true
		break
	}
	if !found {
		return fmt.Errorf("task %q not found in plan %q", taskID, planID)
	}
	if err := saveCanonicalTasks(project.Path, tf); err != nil {
		return err
	}
	plan, err := loadCanonicalPlan(project.Path, planID)
	if err != nil {
		return err
	}
	plan.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	_ = saveCanonicalPlan(project.Path, plan)
	ui.Success(fmt.Sprintf("Updated task %q in plan %q", taskID, planID))
	return nil
}

// PlanScheduleTask is a task entry in the schedule output.
type PlanScheduleTask struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Status     string   `json:"status"`
	WriteScope []string `json:"write_scope"`
}

// PlanScheduleWave is a single wave (parallel group) in the schedule.
type PlanScheduleWave struct {
	Wave  int                `json:"wave"`
	Tasks []PlanScheduleTask `json:"tasks"`
}

// PlanScheduleResult is the full schedule output.
type PlanScheduleResult struct {
	PlanID              string             `json:"plan_id"`
	Waves               []PlanScheduleWave `json:"waves"`
	CriticalPathLength  int                `json:"critical_path_length"`
	MaxIntraParallelism int                `json:"max_intra_plan_parallelism"`
}

// computePlanSchedule runs Kahn's BFS topological sort on the tasks in tf,
// assigning each task a wave number. Cross-plan dep entries (containing "/")
// are ignored for intra-plan scheduling. Returns a PlanScheduleResult.
func computePlanSchedule(tf *CanonicalTaskFile) (*PlanScheduleResult, error) {
	// Build id → task index map.
	idxByID := make(map[string]int, len(tf.Tasks))
	for i, t := range tf.Tasks {
		idxByID[t.ID] = i
	}

	// Build in-degree and adjacency list: adj[i] = list of task indices that depend on task i.
	inDegree := make([]int, len(tf.Tasks))
	adj := make([][]int, len(tf.Tasks))

	for i, t := range tf.Tasks {
		for _, dep := range t.DependsOn {
			// Skip cross-plan dependencies.
			if strings.Contains(dep, "/") {
				continue
			}
			j, ok := idxByID[dep]
			if !ok {
				continue
			}
			adj[j] = append(adj[j], i)
			inDegree[i]++
		}
	}

	// wave[i] holds the 0-based wave index for task i (wave 1 = index 0).
	waveIdx := make([]int, len(tf.Tasks))
	processed := 0

	// Seed with all zero in-degree tasks.
	queue := []int{}
	for i, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, i)
		}
	}

	// waveSlots groups task indices by their wave index.
	waveSlots := map[int][]int{}

	for len(queue) > 0 {
		var nextQueue []int
		for _, idx := range queue {
			w := waveIdx[idx]
			waveSlots[w] = append(waveSlots[w], idx)
			processed++
			for _, dep := range adj[idx] {
				inDegree[dep]--
				if waveIdx[dep] < w+1 {
					waveIdx[dep] = w + 1
				}
				if inDegree[dep] == 0 {
					nextQueue = append(nextQueue, dep)
				}
			}
		}
		queue = nextQueue
	}

	if processed != len(tf.Tasks) {
		return nil, fmt.Errorf("plan %q has a dependency cycle (processed %d of %d tasks)", tf.PlanID, processed, len(tf.Tasks))
	}

	// Build sorted wave list.
	waveNums := make([]int, 0, len(waveSlots))
	for w := range waveSlots {
		waveNums = append(waveNums, w)
	}
	sort.Ints(waveNums)

	waves := make([]PlanScheduleWave, 0, len(waveNums))
	maxPar := 0
	for _, w := range waveNums {
		indices := waveSlots[w]
		waveTasks := make([]PlanScheduleTask, 0, len(indices))
		for _, idx := range indices {
			t := tf.Tasks[idx]
			ws := t.WriteScope
			if ws == nil {
				ws = []string{}
			}
			waveTasks = append(waveTasks, PlanScheduleTask{
				ID:         t.ID,
				Title:      t.Title,
				Status:     t.Status,
				WriteScope: ws,
			})
		}
		sort.Slice(waveTasks, func(a, b int) bool { return waveTasks[a].ID < waveTasks[b].ID })
		waves = append(waves, PlanScheduleWave{Wave: w + 1, Tasks: waveTasks})
		if len(waveTasks) > maxPar {
			maxPar = len(waveTasks)
		}
	}

	return &PlanScheduleResult{
		PlanID:              tf.PlanID,
		Waves:               waves,
		CriticalPathLength:  len(waves),
		MaxIntraParallelism: maxPar,
	}, nil
}

// checkScopeResult holds the output of a check-scope run.
type checkScopeResult struct {
	PlanID              string   `json:"plan_id"`
	TaskID              string   `json:"task_id"`
	SidecarPath         string   `json:"sidecar_path"`
	ChangedFiles        []string `json:"changed_files"`
	InsideScope         []string `json:"inside_scope"`
	OutsideScope        []string `json:"outside_scope"`
	UntouchedRequired   []string `json:"untouched_required"`
	TouchedExcluded     []string `json:"touched_excluded"`
	Clean               bool     `json:"clean"`
}

// runWorkflowPlanCheckScope implements `workflow plan check-scope <plan_id> <task_id>`.
// It reads the .scope.yaml sidecar, collects changed files from flags or git diff, and
// reports which files are inside/outside final_write_scope, which required_paths were
// untouched, and which excluded_paths were touched.
// Exit code: 0=clean, 1=warning (outside-scope or excluded touched), 2=no-sidecar.
func runWorkflowPlanCheckScope(planID, taskID string, changedFiles []string, fromGitDiff bool) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	projectPath := project.Path

	sidecarPath := deriveScopeEvidencePath(projectPath, planID, taskID)
	data, err := os.ReadFile(sidecarPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "no scope sidecar found at %s\n", config.DisplayPath(sidecarPath))
			fmt.Fprintln(os.Stderr, "Run 'dot-agents workflow plan derive-scope' to generate one.")
			os.Exit(2)
		}
		return fmt.Errorf("read sidecar: %w", err)
	}
	var ev ScopeEvidence
	if err := yaml.Unmarshal(data, &ev); err != nil {
		return fmt.Errorf("parse sidecar: %w", err)
	}

	// Collect changed files.
	if fromGitDiff {
		gitFiles, err := checkScopeGitDiffFiles(projectPath)
		if err != nil {
			ui.Warn("--from-git-diff: " + err.Error())
		} else {
			changedFiles = append(changedFiles, gitFiles...)
		}
	}
	// Dedup changed files.
	seen := make(map[string]bool, len(changedFiles))
	deduped := make([]string, 0, len(changedFiles))
	for _, f := range changedFiles {
		if !seen[f] {
			seen[f] = true
			deduped = append(deduped, f)
		}
	}
	changedFiles = deduped

	// Build lookup sets.
	finalScopeSet := make(map[string]bool, len(ev.FinalWriteScope))
	for _, p := range ev.FinalWriteScope {
		finalScopeSet[p] = true
	}
	requiredSet := make(map[string]bool, len(ev.RequiredPaths))
	for _, rp := range ev.RequiredPaths {
		requiredSet[rp.Path] = true
	}
	excludedSet := make(map[string]bool, len(ev.ExcludedPaths))
	for _, ep := range ev.ExcludedPaths {
		excludedSet[ep.Path] = true
	}

	var insideScope, outsideScope, touchedExcluded []string
	touchedFiles := make(map[string]bool, len(changedFiles))
	for _, f := range changedFiles {
		touchedFiles[f] = true
		if finalScopeSet[f] || requiredSet[f] {
			insideScope = append(insideScope, f)
		} else if excludedSet[f] {
			touchedExcluded = append(touchedExcluded, f)
		} else {
			outsideScope = append(outsideScope, f)
		}
	}

	var untouchedRequired []string
	for _, rp := range ev.RequiredPaths {
		if !touchedFiles[rp.Path] {
			untouchedRequired = append(untouchedRequired, rp.Path)
		}
	}

	sort.Strings(insideScope)
	sort.Strings(outsideScope)
	sort.Strings(untouchedRequired)
	sort.Strings(touchedExcluded)

	clean := len(outsideScope) == 0 && len(touchedExcluded) == 0

	result := checkScopeResult{
		PlanID:            planID,
		TaskID:            taskID,
		SidecarPath:       config.DisplayPath(sidecarPath),
		ChangedFiles:      changedFiles,
		InsideScope:       insideScope,
		OutsideScope:      outsideScope,
		UntouchedRequired: untouchedRequired,
		TouchedExcluded:   touchedExcluded,
		Clean:             clean,
	}

	if deps.Flags.JSON() {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(result)
		if !clean {
			os.Exit(1)
		}
		return nil
	}

	ui.Header(fmt.Sprintf("Scope Check: %s / %s", planID, taskID))
	fmt.Fprintf(os.Stdout, "  sidecar: %s\n", config.DisplayPath(sidecarPath))
	fmt.Fprintf(os.Stdout, "  changed files: %d\n\n", len(changedFiles))

	if len(insideScope) > 0 {
		ui.Section("Inside Scope")
		for _, f := range insideScope {
			fmt.Fprintf(os.Stdout, "  + %s\n", f)
		}
		fmt.Fprintln(os.Stdout)
	}

	if len(outsideScope) > 0 {
		ui.Section("Outside Scope (warning)")
		for _, f := range outsideScope {
			fmt.Fprintf(os.Stdout, "  ! %s\n", f)
		}
		fmt.Fprintln(os.Stdout)
	}

	if len(untouchedRequired) > 0 {
		ui.Section("Untouched Required Paths")
		for _, f := range untouchedRequired {
			fmt.Fprintf(os.Stdout, "  - %s\n", f)
		}
		fmt.Fprintln(os.Stdout)
	}

	if len(touchedExcluded) > 0 {
		ui.Section("Touched Excluded Paths (warning)")
		for _, f := range touchedExcluded {
			fmt.Fprintf(os.Stdout, "  x %s\n", f)
		}
		fmt.Fprintln(os.Stdout)
	}

	if clean {
		ui.Success("clean — all changes are within scope, no excluded paths touched")
	} else {
		ui.Warn("scope warnings present (outside-scope or excluded paths touched)")
		fmt.Fprintln(os.Stdout)
		os.Exit(1)
	}
	return nil
}

// checkScopeGitDiffFiles returns the list of files with uncommitted changes using
// `git diff --name-only HEAD`. Returns an error on failure (used for graceful degradation).
func checkScopeGitDiffFiles(projectPath string) ([]string, error) {
	cmd := exec.Command("git", "diff", "--name-only", "HEAD")
	cmd.Dir = projectPath
	cmd.Env = os.Environ()
	out, err := cmd.Output()
	if err != nil {
		// Also try index-only (staged but not committed).
		cmd2 := exec.Command("git", "diff", "--name-only", "--cached")
		cmd2.Dir = projectPath
		cmd2.Env = os.Environ()
		out2, err2 := cmd2.Output()
		if err2 != nil {
			return nil, fmt.Errorf("git diff HEAD: %v; git diff --cached: %v", err, err2)
		}
		out = out2
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

func runWorkflowPlanSchedule(planID string) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	tf, err := loadCanonicalTasks(project.Path, planID)
	if err != nil {
		return fmt.Errorf("load tasks for plan %q: %w", planID, err)
	}

	result, err := computePlanSchedule(tf)
	if err != nil {
		return err
	}

	if deps.Flags.JSON() {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	ui.Header(fmt.Sprintf("Plan Schedule: %s", planID))
	for _, w := range result.Waves {
		fmt.Fprintf(os.Stdout, "\nWave %d (%d task(s)):\n", w.Wave, len(w.Tasks))
		for _, t := range w.Tasks {
			scope := strings.Join(t.WriteScope, ", ")
			if scope == "" {
				scope = "(none)"
			}
			fmt.Fprintf(os.Stdout, "  [%s] %s — %s\n    write_scope: %s\n", t.Status, t.ID, t.Title, scope)
		}
	}
	fmt.Fprintln(os.Stdout)
	fmt.Fprintf(os.Stdout, "Critical path length : %d wave(s)\n", result.CriticalPathLength)
	fmt.Fprintf(os.Stdout, "Max intra-plan parallelism: %d task(s)\n", result.MaxIntraParallelism)
	fmt.Fprintln(os.Stdout)
	return nil
}
