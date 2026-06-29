package views

import (
	"testing"

	"github.com/AGOrcha/dot-agents/internal/kg/registry"
)

// badSchema is a registry.Schema whose untyped `ref` field makes
// adapterkit.BuildSchemaInfo fail (§5.4 forbids untyped refs) — the only way to
// reach the namespaceFromSchema error / mustNamespace panic branches, which the
// shipped CRG embed never triggers.
func badSchema() registry.Schema {
	return registry.Schema{
		Name:    "bad",
		Version: "1.0.0",
		NoteTypes: []registry.NoteType{
			{Name: "x", Fields: []registry.FieldSpec{{Name: "f", Type: "ref"}}},
		},
	}
}

func TestNamespaceFromSchemaError(t *testing.T) {
	if _, err := namespaceFromSchema("bad", badSchema()); err == nil {
		t.Fatal("namespaceFromSchema: expected error on an untyped ref field")
	}
}

func TestMustNamespacePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("mustNamespace did not panic on an invalid schema")
		}
	}()
	mustNamespace("bad", badSchema())
}
