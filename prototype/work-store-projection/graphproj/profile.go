// Package graphproj is the REAL-graph round-trip: it ingests parsed PLAN/TASKS/
// SLICES into a node+edge graphstore under a chosen schema PROFILE, then
// reconstructs the YAML model PURELY from graph readback (nodes + edges) — never
// from a retained parse struct. This is what makes the loss real: a field the
// profile does not persist is gone at readback.
//
// Two profiles are compared:
//   - SchemaV4: the SHIPPED sdd-register field set (plan: plan_id/title/status/
//     summary ; task: task_id/plan_id/title/status/app_type ; deps as edges ;
//     blocks/notes/write_scope/owner/verification_required/timestamps/criteria
//     DROPPED ; slices NOT ingested). This is the field-dropping negative
//     control — it LOSES data on the real tree.
//   - Complete: the minimal extension that persists every typed field (as node
//     fields, with ordered list edges) so reconstruction is lossless. The delta
//     between the two IS the schema-completeness gap D1' requires.
package graphproj

// Profile selects how much of the typed schema the graph persists.
type Profile int

const (
	// SchemaV4 mirrors the shipped sdd-register adapter's field set.
	SchemaV4 Profile = iota
	// Complete persists every typed PLAN/TASKS/SLICES field losslessly.
	Complete
)

// Node types and edge types used by both profiles.
const (
	typePlan  = "plan"
	typeTask  = "task"
	typeSlice = "slice"

	edgeContainsTask = "contains_task"
	edgeDependsOn    = "depends_on"
	edgeBlocks       = "blocks"
	edgeContainsSlc  = "contains_slice"
	edgeSliceParent  = "slice_parent"
	edgeSliceDepends = "slice_depends_on"
)

func planNodeID(id string) string           { return typePlan + ":" + id }
func taskNodeID(plan, task string) string   { return typeTask + ":" + plan + "/" + task }
func sliceNodeID(plan, slice string) string { return typeSlice + ":" + plan + "/" + slice }
