package workflow

import (
	"encoding/json"
	"fmt"
	"os"
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
	if explicitPlanID != "" {
		filteredIDs := parsePlanIDFilter(explicitPlanID)
		if len(filteredIDs) == 0 {
			return nil, nil
		}
		available := make(map[string]bool, len(ids))
		for _, id := range ids {
			available[id] = true
		}
		ids = ids[:0]
		for _, id := range filteredIDs {
			if !available[id] {
				return nil, fmt.Errorf("plan %q not found", id)
			}
			ids = append(ids, id)
		}
	}

	delegations, err := listDelegationContracts(projectPath)
	if err != nil {
		return nil, err
	}

	lockedPlans := activeDelegationPlanIDs(delegations)
	if explicitPlanID == "" {
		ids = filterPlanIDsLocked(ids, lockedPlans)
	} else {
		ids = filterPlanIDsUnlocked(ids, lockedPlans)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	activeDelegations := make(map[string]bool, len(delegations))
	for _, c := range delegations {
		if c.Status == "pending" || c.Status == "active" {
			activeDelegations[c.ParentTaskID] = true
		}
	}

	type candidate struct {
		suggestion workflowNextTaskSuggestion
		priority   int
	}

	var best *candidate
	for _, id := range ids {
		plan, err := loadCanonicalPlan(projectPath, id)
		if err != nil || plan.Status != "active" {
			continue
		}
		tf, err := loadCanonicalTasks(projectPath, id)
		if err != nil {
			return nil, fmt.Errorf("load tasks for plan %q: %w", id, err)
		}
		for _, task := range tf.Tasks {
			if activeDelegations[task.ID] {
				continue
			}
			if task.Status != "in_progress" && task.Status != "pending" {
				continue
			}
			if len(incompleteCanonicalDependencies(tf.Tasks, task.DependsOn)) > 0 {
				continue
			}

			c := candidate{
				suggestion: workflowNextTaskSuggestion{
					PlanID:               plan.ID,
					PlanTitle:            plan.Title,
					TaskID:               task.ID,
					TaskTitle:            task.Title,
					Status:               task.Status,
					WriteScope:           append([]string(nil), task.WriteScope...),
					VerificationRequired: task.VerificationRequired,
					DependsOn:            append([]string(nil), task.DependsOn...),
					AppType:              task.AppType,
				},
				priority: 3,
			}

			switch {
			case task.Status == "in_progress" && plan.CurrentFocusTask == task.Title:
				c.priority = 0
				c.suggestion.Reason = "current focus task is already in progress"
			case task.Status == "in_progress":
				c.priority = 1
				c.suggestion.Reason = "task is already in progress and unblocked"
			case plan.CurrentFocusTask == task.Title:
				c.priority = 2
				c.suggestion.Reason = "current focus task is pending and all dependencies are complete"
			default:
				c.priority = 3
				c.suggestion.Reason = "first pending unblocked task in an active canonical plan"
			}

			if best == nil || c.priority < best.priority {
				tmp := c
				best = &tmp
			}
		}
	}

	if best == nil {
		return nil, nil
	}
	return &best.suggestion, nil
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
