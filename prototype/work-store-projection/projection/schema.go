package projection

// This file is the FIELD CATALOG — the deliverable enumeration. It lists every
// typed field of PLAN.yaml / TASKS.yaml / SLICES.yaml (the M denominator) and,
// for each, whether the SHIPPED schema-v4 graph (internal/adapters/builtin/
// sdd-register/ingest.go) persists it. The "lost" set is the schema-completeness
// gap: what a graph must additionally store for D1' (lossless YAML projection)
// to be TRUE.
//
// Source of truth for "v4 persists": sdd-register ingest.go as of master —
//   planNote  fields: plan_id, title, status, summary
//   addTask   fields: task_key, task_id, plan_id, title, status, app_type
//   deps: depends_on becomes depends_on EDGES (no ordinal, bare/qualified form
//         normalized to a node-id) — value present as edges but ORDER + literal
//         form are lossy; blocks is NOT ingested at all.
//   SLICES.yaml: NOT ingested by sdd-register at all (zero coverage).

// FieldCov describes one typed field's coverage under the shipped v4 graph.
type FieldCov struct {
	Field    string
	V4Stored bool   // does the shipped schema-v4 graph persist it (recoverably)?
	How      string // mechanism / why it is lossy if not stored
}

// PlanFields is the M-field catalog for PLAN.yaml.
func PlanFields() []FieldCov {
	return []FieldCov{
		{"schema_version", false, "constant; not written as a plan-node field"},
		{"id", true, "plan_id node field"},
		{"title", true, "title node field"},
		{"status", true, "status node field"},
		{"summary", true, "summary node field"},
		{"created_at", false, "timestamp dropped — no node field"},
		{"updated_at", false, "timestamp dropped — no node field"},
		{"owner", false, "owner dropped — no node field"},
		{"success_criteria", false, "dropped — no node field"},
		{"verification_strategy", false, "dropped — no node field"},
		{"current_focus_task", false, "dropped — no node field"},
		{"default_app_type", false, "dropped — no node field (omitempty)"},
	}
}

// TaskFields is the M-field catalog for TASKS.yaml task entries.
func TaskFields() []FieldCov {
	return []FieldCov{
		{"id", true, "task_id node field"},
		{"title", true, "title node field"},
		{"status", true, "status node field"},
		{"depends_on", false, "becomes depends_on edges: ORDER lost, bare/qualified literal form normalized to node-id — not byte-recoverable"},
		{"blocks", false, "NOT ingested at all — no edge, no field"},
		{"owner", false, "dropped — no node field"},
		{"write_scope", false, "dropped — no node field (the agent's file-scope contract)"},
		{"verification_required", false, "dropped — no node field"},
		{"notes", false, "dropped — no node field (the substantive per-task record)"},
		{"app_type", true, "app_type node field"},
	}
}

// SliceFields is the M-field catalog for SLICES.yaml slice entries. The shipped
// v4 graph ingests NONE of these (sdd-register has no slice node type).
func SliceFields() []FieldCov {
	return []FieldCov{
		{"id", false, "no slice node type in v4"},
		{"parent_task_id", false, "no slice node type in v4"},
		{"title", false, "no slice node type in v4"},
		{"summary", false, "no slice node type in v4"},
		{"status", false, "no slice node type in v4"},
		{"depends_on", false, "no slice node type in v4"},
		{"write_scope", false, "no slice node type in v4"},
		{"verification_focus", false, "no slice node type in v4"},
		{"owner", false, "no slice node type in v4"},
	}
}

// CoverageGap returns (storedCount, totalCount, lostFieldNames) for a catalog.
func CoverageGap(cat []FieldCov) (stored, total int, lost []string) {
	for _, f := range cat {
		total++
		if f.V4Stored {
			stored++
		} else {
			lost = append(lost, f.Field)
		}
	}
	return stored, total, lost
}
