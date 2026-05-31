package none

import (
	"reflect"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/kg/registry"
)

// compile-time proof: the none adapter satisfies the Adapter interface.
var _ registry.Adapter = Adapter{}

func TestSchema(t *testing.T) {
	s := New().Schema()
	if s.Name != Name {
		t.Fatalf("Schema().Name = %q, want %q", s.Name, Name)
	}
	if s.Version != "1.0.0" {
		t.Fatalf("Schema().Version = %q, want 1.0.0", s.Version)
	}
	if len(s.NoteTypes) != 0 {
		t.Fatalf("Schema().NoteTypes = %+v, want empty", s.NoteTypes)
	}
	if len(s.EdgeTypes) != 0 {
		t.Fatalf("Schema().EdgeTypes = %+v, want empty", s.EdgeTypes)
	}
	if s.ImpactRadius.MaxDepth != 0 {
		t.Fatalf("Schema().ImpactRadius.MaxDepth = %d, want 0", s.ImpactRadius.MaxDepth)
	}
	if s.ImpactRadius.Query == "" {
		t.Fatal("Schema().ImpactRadius.Query empty")
	}
	if len(s.StalenessDrivers) != 0 {
		t.Fatalf("Schema().StalenessDrivers = %+v, want empty", s.StalenessDrivers)
	}
}

func TestName(t *testing.T) {
	if got := New().Name(); got != Name {
		t.Fatalf("Name() = %q, want %q", got, Name)
	}
}

func TestImpactRadius(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{name: "nil input", in: nil, want: []string{}},
		{name: "empty input", in: []string{}, want: []string{}},
		{name: "single id", in: []string{"a"}, want: []string{"a"}},
		{name: "multiple ids", in: []string{"a", "b", "c"}, want: []string{"a", "b", "c"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := New().ImpactRadius(registry.ImpactRequest{ChangedIDs: tt.in})
			if err != nil {
				t.Fatalf("ImpactRadius: %v", err)
			}
			if res.IDs == nil {
				t.Fatal("ImpactRadius returned nil IDs; want non-nil")
			}
			if !reflect.DeepEqual(res.IDs, tt.want) {
				t.Fatalf("ImpactRadius IDs = %v, want %v", res.IDs, tt.want)
			}
		})
	}
}

func TestImpactRadiusDoesNotAliasInput(t *testing.T) {
	in := []string{"a", "b"}
	res, _ := New().ImpactRadius(registry.ImpactRequest{ChangedIDs: in})
	res.IDs[0] = "mutated"
	if in[0] != "a" {
		t.Fatal("ImpactRadius aliased the input slice")
	}
}

func TestSchemaPanicsOnInvalidEmbed(t *testing.T) {
	orig := schemaYAML
	t.Cleanup(func() { schemaYAML = orig })
	schemaYAML = []byte("name: [unterminated")
	defer func() {
		if recover() == nil {
			t.Fatal("Schema() did not panic on invalid embedded schema")
		}
	}()
	_ = New().Schema()
}

func TestRegister(t *testing.T) {
	reg := registry.New()
	if err := Register(reg); err != nil {
		t.Fatalf("Register: %v", err)
	}
	a, err := reg.Resolve("dotagents-builtin:graph/none@^1.0")
	if err != nil {
		t.Fatalf("Resolve none: %v", err)
	}
	if a.Name() != Name {
		t.Fatalf("resolved adapter name = %q, want %q", a.Name(), Name)
	}
	// Double register must fail.
	if err := Register(reg); err == nil {
		t.Fatal("second Register want error")
	}
}
