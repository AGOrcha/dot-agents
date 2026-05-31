package registry

import (
	"strings"
	"testing"
)

func TestLoadSchemaValid(t *testing.T) {
	yaml := `
name: sample
version: 1.2.0
description: |-
  A sample adapter.
note_types:
  - name: control
    fields:
      - { name: status, type: enum, required: true, values: [open, closed] }
edge_types:
  - { name: derives_from, from: control, to: control, cardinality: many-to-many }
impact_radius:
  query: |-
    MATCH (c:control) RETURN c.status
  max_depth: 3
staleness_drivers:
  - source_mutation
`
	s, err := LoadSchema([]byte(yaml))
	if err != nil {
		t.Fatalf("LoadSchema: %v", err)
	}
	if s.Name != "sample" || s.Version != "1.2.0" {
		t.Fatalf("LoadSchema got name=%q version=%q", s.Name, s.Version)
	}
	if len(s.NoteTypes) != 1 || s.NoteTypes[0].Name != "control" {
		t.Fatalf("LoadSchema note types = %+v", s.NoteTypes)
	}
	if len(s.EdgeTypes) != 1 || s.ImpactRadius.MaxDepth != 3 {
		t.Fatalf("LoadSchema edges=%+v impact=%+v", s.EdgeTypes, s.ImpactRadius)
	}
}

func TestLoadSchemaParseError(t *testing.T) {
	if _, err := LoadSchema([]byte("name: [unclosed")); err == nil {
		t.Fatal("LoadSchema malformed yaml want error")
	}
}

func TestValidateSchema(t *testing.T) {
	base := func() Schema {
		return Schema{
			Name:         "x",
			Version:      "1.0.0",
			ImpactRadius: ImpactRadius{Query: "RETURN 1", MaxDepth: 0},
		}
	}
	tests := []struct {
		name    string
		mutate  func(s *Schema)
		wantErr string
	}{
		{name: "valid empty schema (none-like)", mutate: func(s *Schema) {}},
		{name: "missing name", mutate: func(s *Schema) { s.Name = "" }, wantErr: "missing name"},
		{name: "missing version", mutate: func(s *Schema) { s.Version = "" }, wantErr: "missing version"},
		{name: "bad version", mutate: func(s *Schema) { s.Version = "x.y" }, wantErr: "x.y"},
		{name: "missing query", mutate: func(s *Schema) { s.ImpactRadius.Query = "" }, wantErr: "impact_radius.query"},
		{name: "negative depth", mutate: func(s *Schema) { s.ImpactRadius.MaxDepth = -1 }, wantErr: "max_depth"},
		{
			name:    "empty note type name",
			mutate:  func(s *Schema) { s.NoteTypes = []NoteType{{Name: ""}} },
			wantErr: "empty name",
		},
		{
			name: "duplicate note type",
			mutate: func(s *Schema) {
				s.NoteTypes = []NoteType{{Name: "a"}, {Name: "a"}}
			},
			wantErr: "twice",
		},
		{
			name: "empty edge name",
			mutate: func(s *Schema) {
				s.NoteTypes = []NoteType{{Name: "a"}}
				s.EdgeTypes = []EdgeType{{Name: "", From: "a", To: "a"}}
			},
			wantErr: "empty name",
		},
		{
			name: "edge from undeclared",
			mutate: func(s *Schema) {
				s.NoteTypes = []NoteType{{Name: "a"}}
				s.EdgeTypes = []EdgeType{{Name: "e", From: "z", To: "a"}}
			},
			wantErr: "(from)",
		},
		{
			name: "edge to undeclared",
			mutate: func(s *Schema) {
				s.NoteTypes = []NoteType{{Name: "a"}}
				s.EdgeTypes = []EdgeType{{Name: "e", From: "a", To: "z"}}
			},
			wantErr: "(to)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := base()
			tt.mutate(&s)
			err := ValidateSchema(s)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateSchema unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateSchema want error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateSchema error = %q, want substring %q", err.Error(), tt.wantErr)
			}
		})
	}
}
