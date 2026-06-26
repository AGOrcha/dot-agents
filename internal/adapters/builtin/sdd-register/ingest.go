package sddregister

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/AGOrcha/dot-agents/commands/workflow"
	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"

	"go.yaml.in/yaml/v3"
)

const (
	planFileName  = "PLAN.yaml"
	tasksFileName = "TASKS.yaml"
	specFileName  = "design.md"

	// Field names on the projected notes (hoisted so a literal is declared once).
	fSpecID  = "spec_id"
	fTitle   = "title"
	fStatus  = "status"
	fSummary = "summary"
	fPlanID  = "plan_id"
	fTaskKey = "task_key"
	fTaskID  = "task_id"
	fAppType = "app_type"

	// Node types.
	typeSpec = "spec"
	typePlan = "plan"
	typeTask = "task"

	// Edge types.
	edgeContainsTask   = "contains_task"
	edgeBelongsToPlan  = "belongs_to_plan"
	edgeDependsOn      = "depends_on"
	edgePlanForSpec    = "plan_for_spec"
	edgeImplementsSpec = "implements_spec"
)

// IngestResult is the summary an Ingest call returns: the populated namespace
// view plus per-type node/edge counts so a caller (and the hard test) can
// assert plausible coverage without re-querying the store.
type IngestResult struct {
	View        sdk.NamespaceView
	SpecsParsed int
	PlansParsed int
	NodeCount   int
	EdgeCount   int
	NotesByType map[string]int
	EdgesByType map[string]int
}

// taskKey is the COMPOSITE business key for a task: plan-local task ids collide
// across plans, so the key is plan_id + "/" + task_id (the same <plan>/<task>
// form code already uses for cross-plan deps). It is stored as the task.task_key
// FIELD (what queries filter on); the graph NODE id is taskNodeID below.
func taskKey(planID, taskID string) string { return planID + "/" + taskID }

// Node-id namespacing. GAP the dogfood surfaced: a spec and a plan share a bare
// id (the dir-name convention means plan `docs-starlight-migration` and spec
// `docs-starlight-migration` collide), and the DSL evaluator indexes notes by
// id — so a bare-id spec node and plan node overwrite each other in the id map,
// silently breaking type-checked edge traversal. Every graph node id is
// therefore prefixed with its type so ids are unique ACROSS types. The
// unprefixed business keys live on the fields (spec_id/plan_id/task_key) which
// is what WHERE clauses match on.
func specNodeID(specID string) string { return typeSpec + ":" + specID }
func planNodeID(planID string) string { return typePlan + ":" + planID }
func taskNodeID(planID, taskID string) string {
	return typeTask + ":" + taskKey(planID, taskID)
}

// Ingest walks the real .agents/workflow/ tree rooted at workflowDir, projects
// every spec/plan/task into typed KG notes + edges, and writes them through the
// SDK into the adapter's own namespace. It returns the populated NamespaceView
// (read back through the SDK) plus the per-type counts. This is the production
// path the hard test drives end-to-end.
func (a *Adapter) Ingest(store sdk.Store, workflowDir string) (*IngestResult, error) {
	specIDs, err := listSubdirsWithFile(filepath.Join(workflowDir, "specs"), specFileName)
	if err != nil {
		return nil, fmt.Errorf("sdd-register ingest: list specs: %w", err)
	}
	planIDs, err := listSubdirsWithFile(filepath.Join(workflowDir, "plans"), planFileName)
	if err != nil {
		return nil, fmt.Errorf("sdd-register ingest: list plans: %w", err)
	}

	b := &builder{specSet: toSet(specIDs)}
	for _, id := range specIDs {
		b.addSpec(filepath.Join(workflowDir, "specs", id), id)
	}
	for _, id := range planIDs {
		if err := b.addPlan(filepath.Join(workflowDir, "plans", id), id); err != nil {
			return nil, err
		}
	}

	s := sdk.For(a.Name(), store)
	if err := s.WriteNotes(b.notes); err != nil {
		return nil, fmt.Errorf("sdd-register ingest: write notes: %w", err)
	}
	if err := s.WriteEdges(b.edges); err != nil {
		return nil, fmt.Errorf("sdd-register ingest: write edges: %w", err)
	}
	view, err := loadView(s)
	if err != nil {
		return nil, err
	}
	return b.result(view, len(specIDs), len(planIDs)), nil
}

// builder accumulates the projected notes + edges during a walk.
type builder struct {
	notes   []sdk.Note
	edges   []sdk.Edge
	specSet map[string]bool // spec ids that exist on disk (gate spec edges)
}

// addSpec projects one spec dir into a spec node. The spec id is the directory
// name (the only reliable identity — frontmatter has no schema'd id field).
func (b *builder) addSpec(dir, specID string) {
	title, status := parseSpecFrontmatter(filepath.Join(dir, specFileName))
	b.notes = append(b.notes, sdk.Note{
		ID:   specNodeID(specID),
		Type: typeSpec,
		Fields: map[string]any{
			fSpecID: specID,
			fTitle:  title,
			fStatus: status,
		},
	})
}

// addPlan projects one plan dir: the plan node, its tasks (composite-keyed),
// the contains_task/belongs_to_plan/depends_on edges, and the plan_for_spec /
// implements_spec trace edges when a same-named spec exists on disk.
func (b *builder) addPlan(dir, planID string) error {
	plan, err := loadPlan(filepath.Join(dir, planFileName))
	if err != nil {
		return err
	}
	tf, err := loadTasks(filepath.Join(dir, tasksFileName))
	if err != nil {
		return err
	}
	id := effectivePlanID(plan, planID)
	b.notes = append(b.notes, planNote(plan, id))
	specID := b.linkedSpec(id)
	if specID != "" {
		b.edges = append(b.edges, sdk.Edge{Type: edgePlanForSpec, From: planNodeID(id), To: specNodeID(specID)})
	}
	for i := range tf.Tasks {
		b.addTask(id, specID, tf.Tasks[i])
	}
	return nil
}

// linkedSpec returns the spec id a plan implements, by the strong dir-name
// convention (plan_id == spec_id). Returns "" when no same-named spec exists.
func (b *builder) linkedSpec(planID string) string {
	if b.specSet[planID] {
		return planID
	}
	return ""
}

// addTask projects one task into a composite-keyed task node plus its
// contains_task (plan->task), belongs_to_plan (task->plan), depends_on, and —
// when the plan implements a spec — implements_spec edges.
func (b *builder) addTask(planID, specID string, t workflow.CanonicalTask) {
	key := taskKey(planID, t.ID)
	node := taskNodeID(planID, t.ID)
	b.notes = append(b.notes, sdk.Note{
		ID:   node,
		Type: typeTask,
		Fields: map[string]any{
			fTaskKey: key,
			fTaskID:  t.ID,
			fPlanID:  planID,
			fTitle:   t.Title,
			fStatus:  t.Status,
			fAppType: t.AppType,
		},
	})
	b.edges = append(b.edges,
		sdk.Edge{Type: edgeContainsTask, From: planNodeID(planID), To: node},
		sdk.Edge{Type: edgeBelongsToPlan, From: node, To: planNodeID(planID)},
	)
	if specID != "" {
		b.edges = append(b.edges, sdk.Edge{Type: edgeImplementsSpec, From: node, To: specNodeID(specID)})
	}
	b.addDeps(planID, t)
}

// addDeps emits a depends_on edge per dependency. A bare dep id is plan-local
// (keyed within the same plan); a cross-plan dep already carries the
// <plan>/<task> form and is used verbatim. The to-node may not exist as a
// projected node (a stale/cross-plan ref) — the edge still records the
// declared dependency, which is what the real corpus carries.
func (b *builder) addDeps(planID string, t workflow.CanonicalTask) {
	from := taskNodeID(planID, t.ID)
	for _, dep := range t.DependsOn {
		// A bare dep is plan-local (key it within this plan); a cross-plan dep
		// already carries the <plan>/<task> form. Either way the dep is a task
		// business key, so namespace it to the task node id.
		depKey := dep
		if !strings.Contains(dep, "/") {
			depKey = taskKey(planID, dep)
		}
		b.edges = append(b.edges, sdk.Edge{Type: edgeDependsOn, From: from, To: typeTask + ":" + depKey})
	}
}

// result assembles the IngestResult with per-type counts.
func (b *builder) result(view sdk.NamespaceView, specs, plans int) *IngestResult {
	notesByType := map[string]int{}
	for _, n := range b.notes {
		notesByType[n.Type]++
	}
	edgesByType := map[string]int{}
	for _, e := range b.edges {
		edgesByType[e.Type]++
	}
	return &IngestResult{
		View:        view,
		SpecsParsed: specs,
		PlansParsed: plans,
		NodeCount:   len(b.notes),
		EdgeCount:   len(b.edges),
		NotesByType: notesByType,
		EdgesByType: edgesByType,
	}
}

// planNote builds a plan node from the canonical PLAN.yaml struct.
func planNote(plan *workflow.CanonicalPlan, id string) sdk.Note {
	return sdk.Note{
		ID:   planNodeID(id),
		Type: typePlan,
		Fields: map[string]any{
			fPlanID:  id,
			fTitle:   plan.Title,
			fStatus:  plan.Status,
			fSummary: plan.Summary,
		},
	}
}

// effectivePlanID prefers the PLAN.yaml `id` field; falls back to the directory
// name when the file omits it (a real-corpus gap the dogfood tolerates).
func effectivePlanID(plan *workflow.CanonicalPlan, dirName string) string {
	if plan.ID != "" {
		return plan.ID
	}
	return dirName
}

// loadPlan reads + unmarshals a PLAN.yaml into the canonical struct (reusing
// commands/workflow's CanonicalPlan rather than re-deriving the shape).
func loadPlan(path string) (*workflow.CanonicalPlan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read plan %s: %w", path, err)
	}
	var plan workflow.CanonicalPlan
	if err := yaml.Unmarshal(data, &plan); err != nil {
		return nil, fmt.Errorf("parse plan %s: %w", path, err)
	}
	return &plan, nil
}

// loadTasks reads + unmarshals a TASKS.yaml into the canonical struct.
func loadTasks(path string) (*workflow.CanonicalTaskFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read tasks %s: %w", path, err)
	}
	var tf workflow.CanonicalTaskFile
	if err := yaml.Unmarshal(data, &tf); err != nil {
		return nil, fmt.Errorf("parse tasks %s: %w", path, err)
	}
	return &tf, nil
}

// parseSpecFrontmatter extracts the H1 title and the **Status:** line from a
// design.md, best-effort. The frontmatter is freeform markdown (no schema), so
// a missing line yields an empty value rather than an error — a deliberate
// dogfood gap (see PR GAPS). Returns (title, status).
func parseSpecFrontmatter(path string) (string, string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()
	var title, status string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if title == "" && strings.HasPrefix(line, "# ") {
			title = strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
		if status == "" && strings.HasPrefix(line, "**Status:**") {
			status = strings.TrimSpace(strings.TrimPrefix(line, "**Status:**"))
		}
		if title != "" && status != "" {
			break
		}
	}
	return title, status
}

// loadView reads the adapter namespace back through the SDK as a NamespaceView
// (own-namespace read), the same shape the DSL evaluator runs over.
func loadView(s *sdk.SDK) (sdk.NamespaceView, error) {
	var view sdk.NamespaceView
	_, err := s.Query(func(v sdk.NamespaceView) []sdk.Row {
		view = v
		return nil
	})
	return view, err
}

// listSubdirsWithFile returns the names of immediate subdirectories of dir that
// contain a file named fileName, sorted for deterministic ingest order.
func listSubdirsWithFile(dir, fileName string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if fileExists(filepath.Join(dir, e.Name(), fileName)) {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

// fileExists reports whether path is an existing regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// toSet builds a presence set from a slice.
func toSet(ids []string) map[string]bool {
	m := make(map[string]bool, len(ids))
	for _, id := range ids {
		m[id] = true
	}
	return m
}
