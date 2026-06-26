package graphproj

import (
	"strconv"

	"github.com/AGOrcha/dot-agents/prototype/work-store-projection/graphstore"
	"github.com/AGOrcha/dot-agents/prototype/work-store-projection/projection"
)

// Ingest writes a plan + its tasks + its slices into the graph under profile p.
// Returns the store so callers can read it back / mutate / re-ingest.
func Ingest(p Profile, plan *projection.Plan, tf *projection.TaskFile, sf *projection.SliceFile) *graphstore.Store {
	s := graphstore.New()
	IngestInto(s, p, plan, tf, sf)
	return s
}

// IngestInto writes into an existing store (the re-ingest update path: PutNode
// merges by id, RewriteEdgesFrom replaces a node's list edges). The loop index
// i is persisted as the node ORDINAL so reconstruction restores the original
// file order — graph containment edges are an unordered SET, so order must be
// stored explicitly (a real D1' requirement the shipped v4 graph omits).
func IngestInto(s *graphstore.Store, p Profile, plan *projection.Plan, tf *projection.TaskFile, sf *projection.SliceFile) {
	writePlan(s, p, plan)
	if tf != nil {
		for i := range tf.Tasks {
			writeTask(s, p, plan.ID, i, tf.Tasks[i])
		}
	}
	if sf != nil && p == Complete {
		for i := range sf.Slices {
			writeSlice(s, sf.PlanID, i, sf.Slices[i])
		}
	}
}

// ordinalField is the node field carrying a child's position in its parent's
// ordered list. Stored under both profiles for tasks (so even v4 keeps task
// order — see note: the shipped graph does NOT, but ordering is cheap and the
// complete-vs-v4 loss is about FIELDS, so we keep order in both to isolate the
// field-loss signal).
const ordinalField = "_ordinal"

// writePlan persists a plan node. v4 stores only plan_id/title/status/summary;
// complete stores every scalar field.
func writePlan(s *graphstore.Store, p Profile, plan *projection.Plan) {
	f := map[string]any{
		"plan_id": plan.ID,
		"title":   plan.Title,
		"status":  plan.Status,
		"summary": plan.Summary,
	}
	if p == Complete {
		f["schema_version"] = plan.SchemaVersion
		f["created_at"] = plan.CreatedAt
		f["updated_at"] = plan.UpdatedAt
		f["owner"] = plan.Owner
		f["success_criteria"] = plan.SuccessCriteria
		f["verification_strategy"] = plan.VerificationStrategy
		f["current_focus_task"] = plan.CurrentFocusTask
		f["default_app_type"] = plan.DefaultAppType
	}
	s.PutNode(graphstore.Node{ID: planNodeID(plan.ID), Type: typePlan, Fields: f})
}

// writeTask persists a task node + its edges. v4 stores id/title/status/app_type
// and depends_on EDGES (unordered, target normalized to a node id); blocks and
// the rest are dropped. complete stores every scalar and writes ordered list
// edges that carry the LITERAL value + ordinal so order + bare/qualified form
// reconstruct exactly.
func writeTask(s *graphstore.Store, p Profile, planID string, ord int, t projection.Task) {
	node := taskNodeID(planID, t.ID)
	f := map[string]any{
		"task_id":    t.ID,
		"plan_id":    planID,
		"title":      t.Title,
		"status":     t.Status,
		"app_type":   t.AppType,
		ordinalField: ord,
	}
	if p == Complete {
		f["owner"] = t.Owner
		f["verification_required"] = t.VerificationRequired
		f["notes"] = t.Notes
		// write_scope, depends_on, blocks are ordered lists -> ordered edges.
	}
	s.PutNode(graphstore.Node{ID: node, Type: typeTask, Fields: f})
	s.PutEdge(graphstore.Edge{Type: edgeContainsTask, From: planNodeID(planID), To: node})

	if p == Complete {
		writeOrderedList(s, edgeWriteScope, node, t.WriteScope)
		writeOrderedList(s, edgeDependsOn, node, t.DependsOn)
		writeOrderedList(s, edgeBlocks, node, t.Blocks)
		return
	}
	// v4: depends_on as plain edges (no ordinal, target normalized to node id).
	for _, dep := range t.DependsOn {
		s.PutEdge(graphstore.Edge{Type: edgeDependsOn, From: node, To: typeTask + ":" + normalizeDep(planID, dep)})
	}
}

// writeSlice persists a slice node (complete profile only). Every scalar is a
// node field; depends_on / write_scope are ordered list edges.
func writeSlice(s *graphstore.Store, planID string, ord int, sl projection.Slice) {
	node := sliceNodeID(planID, sl.ID)
	s.PutNode(graphstore.Node{ID: node, Type: typeSlice, Fields: map[string]any{
		"slice_id":           sl.ID,
		"parent_task_id":     sl.ParentTaskID,
		"title":              sl.Title,
		"summary":            sl.Summary,
		"status":             sl.Status,
		"verification_focus": sl.VerificationFocus,
		"owner":              sl.Owner,
		ordinalField:         ord,
	}})
	s.PutEdge(graphstore.Edge{Type: edgeContainsSlc, From: planNodeID(planID), To: node})
	s.PutEdge(graphstore.Edge{Type: edgeSliceParent, From: node, To: taskNodeID(planID, sl.ParentTaskID)})
	writeOrderedList(s, edgeWriteScope, node, sl.WriteScope)
	writeOrderedList(s, edgeSliceDepends, node, sl.DependsOn)
}

const edgeWriteScope = "write_scope"

// writeOrderedList rewrites the edges of edgeType from `from` as an ORDERED set:
// each element becomes an edge whose To encodes "<ordinal>|<literal>", so
// readback restores both the order and the exact string. Using RewriteEdgesFrom
// makes re-ingest replace the prior list wholesale.
func writeOrderedList(s *graphstore.Store, edgeType, from string, items []string) {
	repl := make([]graphstore.Edge, 0, len(items))
	for i, it := range items {
		repl = append(repl, graphstore.Edge{Type: edgeType, From: from, To: ordinalTarget(i, it)})
	}
	s.RewriteEdgesFrom(edgeType, from, repl)
}

func ordinalTarget(i int, literal string) string {
	return strconv.Itoa(i) + "|" + literal
}

// normalizeDep mirrors sdd-register: a bare dep is plan-local (keyed within the
// plan); a cross-plan dep already carries <plan>/<task>. The result is a node
// key — which is WHY v4 cannot recover the literal bare form on readback.
func normalizeDep(planID, dep string) string {
	for _, c := range dep {
		if c == '/' {
			return dep
		}
	}
	return planID + "/" + dep
}
