package registry

import (
	"fmt"

	"go.yaml.in/yaml/v3"
)

// LoadSchema parses an adapter schema from its YAML form (spec §4) and
// validates the fields the contract requires. It is the loader + schema
// validator the `none` adapter and later adapters share.
func LoadSchema(data []byte) (Schema, error) {
	var s Schema
	if err := yaml.Unmarshal(data, &s); err != nil {
		return Schema{}, fmt.Errorf("adapter schema: parse: %w", err)
	}
	if err := ValidateSchema(s); err != nil {
		return Schema{}, err
	}
	return s, nil
}

// ValidateSchema enforces the §4 invariants checkable without a corpus:
//   - name and version are present and version parses
//   - impact_radius.query is present and max_depth is non-negative
//   - every edge_type references declared note types (or none, when the
//     schema declares no note types — the `none` adapter case)
func ValidateSchema(s Schema) error {
	if s.Name == "" {
		return fmt.Errorf("adapter schema: missing name")
	}
	if s.Version == "" {
		return fmt.Errorf("adapter schema: %q missing version", s.Name)
	}
	if _, err := ParseVersion(s.Version); err != nil {
		return fmt.Errorf("adapter schema: %q: %w", s.Name, err)
	}
	if s.ImpactRadius.Query == "" {
		return fmt.Errorf("adapter schema: %q missing impact_radius.query", s.Name)
	}
	if s.ImpactRadius.MaxDepth < 0 {
		return fmt.Errorf("adapter schema: %q impact_radius.max_depth is negative", s.Name)
	}
	declared := make(map[string]bool, len(s.NoteTypes))
	for _, nt := range s.NoteTypes {
		if nt.Name == "" {
			return fmt.Errorf("adapter schema: %q has a note_type with empty name", s.Name)
		}
		if declared[nt.Name] {
			return fmt.Errorf("adapter schema: %q declares note_type %q twice", s.Name, nt.Name)
		}
		declared[nt.Name] = true
	}
	for _, et := range s.EdgeTypes {
		if et.Name == "" {
			return fmt.Errorf("adapter schema: %q has an edge_type with empty name", s.Name)
		}
		if !declared[et.From] {
			return fmt.Errorf("adapter schema: %q edge_type %q references undeclared note_type %q (from)", s.Name, et.Name, et.From)
		}
		if !declared[et.To] {
			return fmt.Errorf("adapter schema: %q edge_type %q references undeclared note_type %q (to)", s.Name, et.Name, et.To)
		}
	}
	return nil
}
