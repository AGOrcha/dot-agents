package graphproj

import (
	"fmt"

	"github.com/AGOrcha/dot-agents/prototype/work-store-projection/projection"
)

// FieldLoss records one field that differed between the original parsed value
// and the graph-reconstructed value (i.e. the graph could not hold it).
type FieldLoss struct {
	Entity string // "plan:<id>" / "task:<plan>/<id>" / "slice:<plan>/<id>"
	Field  string
	Reason string
}

// LossReport is the per-round-trip loss accounting through the graph.
type LossReport struct {
	Profile       Profile
	Losses        []FieldLoss
	TasksChecked  int
	SlicesPresent int
}

// AnalyzePlan ingests plan+tasks+slices into a graph under p, reconstructs from
// readback, and diffs every typed field. The returned losses are the fields the
// graph DROPPED — the real D1' loss (not the tautological struct mirror).
func AnalyzePlan(p Profile, plan *projection.Plan, tf *projection.TaskFile, sf *projection.SliceFile) LossReport {
	s := Ingest(p, plan, tf, sf)
	rp := ReconstructPlan(s, plan.ID)
	rt := ReconstructTasks(s, plan.ID, tf.SchemaVersion)
	var rs *projection.SliceFile
	if sf != nil {
		rs = ReconstructSlices(s, sf.PlanID, sf.SchemaVersion)
	}

	rep := LossReport{Profile: p, TasksChecked: len(tf.Tasks)}
	rep.Losses = append(rep.Losses, diffPlan(plan, rp)...)
	rep.Losses = append(rep.Losses, diffTasks(plan.ID, tf, rt)...)
	if sf != nil && len(sf.Slices) > 0 {
		rep.SlicesPresent = len(sf.Slices)
		rep.Losses = append(rep.Losses, diffSlices(sf, rs)...)
	}
	return rep
}

// diffPlan compares each plan field; mismatch => the graph lost it.
func diffPlan(orig, got *projection.Plan) []FieldLoss {
	ent := "plan:" + orig.ID
	if got == nil {
		return []FieldLoss{{ent, "*", "plan node absent in graph"}}
	}
	var out []FieldLoss
	chk := func(field, a, b string) {
		if a != b {
			out = append(out, FieldLoss{ent, field, fmt.Sprintf("orig=%q got=%q", trunc(a), trunc(b))})
		}
	}
	chk("title", orig.Title, got.Title)
	chk("status", orig.Status, got.Status)
	chk("summary", orig.Summary, got.Summary)
	chk("created_at", orig.CreatedAt, got.CreatedAt)
	chk("updated_at", orig.UpdatedAt, got.UpdatedAt)
	chk("owner", orig.Owner, got.Owner)
	chk("success_criteria", orig.SuccessCriteria, got.SuccessCriteria)
	chk("verification_strategy", orig.VerificationStrategy, got.VerificationStrategy)
	chk("current_focus_task", orig.CurrentFocusTask, got.CurrentFocusTask)
	chk("default_app_type", orig.DefaultAppType, got.DefaultAppType)
	if orig.SchemaVersion != got.SchemaVersion {
		out = append(out, FieldLoss{ent, "schema_version", fmt.Sprintf("orig=%d got=%d", orig.SchemaVersion, got.SchemaVersion)})
	}
	return out
}

func diffTasks(planID string, orig, got *projection.TaskFile) []FieldLoss {
	var out []FieldLoss
	byID := map[string]projection.Task{}
	for _, t := range got.Tasks {
		byID[t.ID] = t
	}
	for _, t := range orig.Tasks {
		ent := "task:" + planID + "/" + t.ID
		g, ok := byID[t.ID]
		if !ok {
			out = append(out, FieldLoss{ent, "*", "task node absent in graph"})
			continue
		}
		out = append(out, diffTaskFields(ent, t, g)...)
	}
	return out
}

func diffTaskFields(ent string, a, b projection.Task) []FieldLoss {
	var out []FieldLoss
	scalar := func(field, x, y string) {
		if x != y {
			out = append(out, FieldLoss{ent, field, fmt.Sprintf("orig=%q got=%q", trunc(x), trunc(y))})
		}
	}
	scalar("title", a.Title, b.Title)
	scalar("status", a.Status, b.Status)
	scalar("app_type", a.AppType, b.AppType)
	scalar("owner", a.Owner, b.Owner)
	scalar("notes", a.Notes, b.Notes)
	if a.VerificationRequired != b.VerificationRequired {
		out = append(out, FieldLoss{ent, "verification_required", fmt.Sprintf("orig=%v got=%v", a.VerificationRequired, b.VerificationRequired)})
	}
	out = append(out, diffList(ent, "depends_on", a.DependsOn, b.DependsOn)...)
	out = append(out, diffList(ent, "blocks", a.Blocks, b.Blocks)...)
	out = append(out, diffList(ent, "write_scope", a.WriteScope, b.WriteScope)...)
	return out
}

func diffSlices(orig, got *projection.SliceFile) []FieldLoss {
	var out []FieldLoss
	byID := map[string]projection.Slice{}
	if got != nil {
		for _, s := range got.Slices {
			byID[s.ID] = s
		}
	}
	for _, s := range orig.Slices {
		ent := "slice:" + orig.PlanID + "/" + s.ID
		g, ok := byID[s.ID]
		if !ok {
			out = append(out, FieldLoss{ent, "*", "slice node absent in graph (v4 has no slice type)"})
			continue
		}
		out = append(out, diffSliceFields(ent, s, g)...)
	}
	return out
}

func diffSliceFields(ent string, a, b projection.Slice) []FieldLoss {
	var out []FieldLoss
	scalar := func(field, x, y string) {
		if x != y {
			out = append(out, FieldLoss{ent, field, fmt.Sprintf("orig=%q got=%q", trunc(x), trunc(y))})
		}
	}
	scalar("parent_task_id", a.ParentTaskID, b.ParentTaskID)
	scalar("title", a.Title, b.Title)
	scalar("summary", a.Summary, b.Summary)
	scalar("status", a.Status, b.Status)
	scalar("verification_focus", a.VerificationFocus, b.VerificationFocus)
	scalar("owner", a.Owner, b.Owner)
	out = append(out, diffList(ent, "depends_on", a.DependsOn, b.DependsOn)...)
	out = append(out, diffList(ent, "write_scope", a.WriteScope, b.WriteScope)...)
	return out
}

// diffList reports a loss when two ordered string lists differ (length, order,
// or literal). Treats nil and empty as equal.
func diffList(ent, field string, a, b []string) []FieldLoss {
	if len(a) != len(b) {
		return []FieldLoss{{ent, field, fmt.Sprintf("len orig=%d got=%d", len(a), len(b))}}
	}
	for i := range a {
		if a[i] != b[i] {
			return []FieldLoss{{ent, field, fmt.Sprintf("at[%d] orig=%q got=%q", i, a[i], b[i])}}
		}
	}
	return nil
}

func trunc(s string) string {
	if len(s) > 40 {
		return s[:37] + "..."
	}
	return s
}
