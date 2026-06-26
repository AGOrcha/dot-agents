// Package projection holds the typed YAML model + the canonical (de)serializer.
//
// SCOPE NOTE (read this): this package proves only the SERIALIZER half — that a
// typed model marshals to canonical, deterministic, churn-free YAML. Because the
// model mirrors the YAML schema 1:1, "lossless for typed fields" here is
// TAUTOLOGICAL and does NOT validate the D1' claim that a GRAPH can losslessly
// project the YAML. The real D1' experiment lives in package graphproj, which
// routes through an actual node+edge graph and measures what the graph DROPS.
// These struct-level proofs are kept because the serializer IS the YAML-write
// half of the graph round-trip (graphproj reconstructs a model, then this
// package serializes it).
//
// The typed model mirrors commands/workflow.CanonicalPlan / CanonicalTask /
// CanonicalSlice (the shipped PLAN/TASKS/SLICES schema) but is independent so
// the prototype has its own go.mod and does not couple to the main module.
package projection

// Plan is the typed projection of a PLAN.yaml. Field order here defines the
// canonical key order in the regenerated YAML — the serializer emits keys in
// struct-declaration order, which is the no-churn contract.
//
// IMPORTANT: only fields declared here are graph-projectable. Any key present in
// the on-disk file but absent from this struct (e.g. spec_ref, coherence_note)
// is NOT representable in the graph and is dropped on regeneration. ExtraKeys
// captures those so we can MEASURE the loss precisely rather than hide it.
type Plan struct {
	SchemaVersion        int    `yaml:"schema_version"`
	ID                   string `yaml:"id"`
	Title                string `yaml:"title"`
	Status               string `yaml:"status"`
	Summary              string `yaml:"summary"`
	CreatedAt            string `yaml:"created_at"`
	UpdatedAt            string `yaml:"updated_at"`
	Owner                string `yaml:"owner"`
	SuccessCriteria      string `yaml:"success_criteria"`
	VerificationStrategy string `yaml:"verification_strategy"`
	CurrentFocusTask     string `yaml:"current_focus_task"`
	DefaultAppType       string `yaml:"default_app_type,omitempty"`

	// ExtraKeys holds top-level keys found in the file that are not part of the
	// canonical schema (the "git-canonical-only" residue). Not serialized back
	// in the canonical projection; tracked for fidelity reporting.
	ExtraKeys map[string]any `yaml:"-"`
}

// TaskFile is the typed projection of a TASKS.yaml.
type TaskFile struct {
	SchemaVersion int    `yaml:"schema_version"`
	PlanID        string `yaml:"plan_id"`
	Tasks         []Task `yaml:"tasks"`
}

// Task is one entry in a TASKS.yaml. Field order = canonical key order.
type Task struct {
	ID                   string   `yaml:"id"`
	Title                string   `yaml:"title"`
	Status               string   `yaml:"status"`
	DependsOn            []string `yaml:"depends_on"`
	Blocks               []string `yaml:"blocks"`
	Owner                string   `yaml:"owner"`
	WriteScope           []string `yaml:"write_scope"`
	VerificationRequired bool     `yaml:"verification_required"`
	Notes                string   `yaml:"notes"`
	AppType              string   `yaml:"app_type,omitempty"`
}

// SliceFile is the typed projection of a SLICES.yaml.
type SliceFile struct {
	SchemaVersion int     `yaml:"schema_version"`
	PlanID        string  `yaml:"plan_id"`
	Slices        []Slice `yaml:"slices"`
}

// Slice is one entry in a SLICES.yaml (mirrors CanonicalSlice). Field order =
// canonical key order.
type Slice struct {
	ID                string   `yaml:"id"`
	ParentTaskID      string   `yaml:"parent_task_id"`
	Title             string   `yaml:"title"`
	Summary           string   `yaml:"summary"`
	Status            string   `yaml:"status"`
	DependsOn         []string `yaml:"depends_on"`
	WriteScope        []string `yaml:"write_scope"`
	VerificationFocus string   `yaml:"verification_focus"`
	Owner             string   `yaml:"owner"`
}
