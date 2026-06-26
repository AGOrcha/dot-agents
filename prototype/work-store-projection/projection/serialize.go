package projection

import (
	"fmt"

	yaml "go.yaml.in/yaml/v3"
)

// SerializePlan renders a Plan back to canonical PLAN.yaml bytes.
//
// CANONICAL FORM CONTRACT (what the spec must pin):
//   - Key order = struct-declaration order (deterministic, not map iteration).
//   - Indent = the library default (4 spaces) — this is what the shipped
//     da writer (plain yaml.Marshal) emits, so matching it = no churn vs files
//     da itself wrote.
//   - Quoting/block-scalar style = whatever go.yaml.in/yaml/v3 chooses for the
//     value (it auto-quotes when a plain scalar would be ambiguous, e.g. a
//     leading-`:` or a `: ` run, and uses `|-` for multi-line). This is
//     deterministic for a given value, which is what guarantees churn-freedom.
//
// ExtraKeys are intentionally NOT emitted: they are non-projectable residue. A
// caller that needs them must keep them git-canonical (see the report).
func SerializePlan(p *Plan) ([]byte, error) {
	out, err := yaml.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("serialize plan: %w", err)
	}
	return out, nil
}

// SerializeTasks renders a TaskFile back to canonical TASKS.yaml bytes.
func SerializeTasks(tf *TaskFile) ([]byte, error) {
	out, err := yaml.Marshal(tf)
	if err != nil {
		return nil, fmt.Errorf("serialize tasks: %w", err)
	}
	return out, nil
}

// SerializeSlices renders a SliceFile back to canonical SLICES.yaml bytes.
func SerializeSlices(sf *SliceFile) ([]byte, error) {
	out, err := yaml.Marshal(sf)
	if err != nil {
		return nil, fmt.Errorf("serialize slices: %w", err)
	}
	return out, nil
}

// Marshal is the single shared serialization entry point used by both Plan and
// TaskFile so the canonical settings (indent, encoder options) live in exactly
// one place. Hoisted to defeat any future drift between the two serializers.
func Marshal(v any) ([]byte, error) {
	return yaml.Marshal(v)
}
