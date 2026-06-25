package dsl_test

import (
	"testing"

	"github.com/AGOrcha/dot-agents/internal/kg/dsl"
)

// TestDeepParseErrors drives the deeply-nested expect* error returns that the
// first error-path test couldn't reach because an earlier check short-circuited.
// Each input is crafted to pass every earlier check and fail at exactly one
// nested point.
func TestDeepParseErrors(t *testing.T) {
	bad := []string{
		"MATCH (n:n)-[:]->(m:n) RETURN n.id",                // anonymous edge missing type after colon
		"MATCH (n:n)-[e:]->(m:n) RETURN n.id",               // aliased edge missing type after colon
		"MATCH (n:n)-[:e]-> RETURN n.id",                    // arrow then missing end node
		"MATCH (n:n)-[:e*1.. ]->(m:n) RETURN n.id",          // var-length upper bound not a number
		"MATCH (n:n)-[:e*1 2]->(m:n) RETURN n.id",           // var-length missing first dot
		"MATCH (n:n) WHERE n.. = $t RETURN n.id",            // field ref with empty part
		"MATCH (n:n) WHERE STARTS_WITH(n.v $p) RETURN n.id", // STARTS_WITH missing comma
		"MATCH (n:n) WHERE STARTS_WITH(n.v, $p RETURN n.id", // STARTS_WITH missing close paren
		"MATCH (n:n) RETURN coalesce($a RETURN",             // RETURN coalesce missing close paren
		"MATCH (n:n) RETURN min(n.v RETURN",                 // RETURN min missing close paren
		"MATCH (n:n) RETURN min(n.v AS",                     // RETURN AS missing alias
		"MATCH (n:n) WHERE n.v = coalesce($a RETURN n.id",   // value coalesce missing close paren
		"MATCH (n:n) WHERE n.v = coalesce(n.x) RETURN n.id", // value coalesce field arg (reject)
		"MATCH (n:n) RETURN count(*)x",                      // count(*) then non-comma garbage handled as trailing
	}
	for _, src := range bad {
		t.Run(src, func(t *testing.T) {
			if _, err := dsl.Parse(src); err == nil {
				t.Errorf("expected parse error for %q", src)
			}
		})
	}
}

// TestSchemaMidChainNonRef covers walkRefChain's non-ref-mid-chain rejection:
// traversing through a scalar field as if it were a ref.
func TestSchemaMidChainNonRef(t *testing.T) {
	info, err := dsl.NewSchemaInfo([]dsl.NoteTypeDecl{
		{Name: "a", Fields: []dsl.FieldDecl{{Name: "scalar", Type: "string"}}},
	}, nil, 2)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	// a.scalar.more traverses through a non-ref scalar field → reject.
	if _, err := dsl.ParseWithSchema("MATCH (a:a) WHERE a.scalar.more = $x RETURN a.id", info); err == nil {
		t.Fatal("expected non-ref mid-chain traversal to be rejected")
	}
}

// TestSchemaUnknownNoteTypeInChain covers walkRefChain's unknown-note-type
// branch: a ref whose target type is not declared.
func TestSchemaUnknownNoteTypeInChain(t *testing.T) {
	info, err := dsl.NewSchemaInfo([]dsl.NoteTypeDecl{
		{Name: "a", Fields: []dsl.FieldDecl{{Name: "r", Type: "ref<ghost>"}}},
	}, nil, 2)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	if _, err := dsl.ParseWithSchema("MATCH (a:a) WHERE a.r.k = $x RETURN a.id", info); err == nil {
		t.Fatal("expected ref to unknown note type to be rejected")
	}
}

// TestINDescribePath covers FieldRef.describe via the IN-on-non-id error, which
// renders the offending ref's full path.
func TestINDescribePath(t *testing.T) {
	_, err := dsl.Parse("MATCH (n:n) WHERE n.field.sub IN $x RETURN n.id")
	if err == nil {
		t.Fatal("expected IN on non-id ref to be rejected")
	}
}
