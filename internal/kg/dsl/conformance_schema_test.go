package dsl_test

import (
	"testing"

	"github.com/AGOrcha/dot-agents/internal/kg/dsl"
)

// testSchema is the SchemaInfo the conformance tests parse against. It models a
// small TTRPG-shaped schema (the §5 conformance examples use character/location
// with ref fields) plus a CALLS edge for the edge-alias tests (T28–T30).
func testSchema(t *testing.T) dsl.SchemaInfo {
	t.Helper()
	notes := []dsl.NoteTypeDecl{
		{Name: tCharacter, Fields: []dsl.FieldDecl{
			{Name: "name", Type: tString},
			{Name: fRegion, Type: tString},
			{Name: fStatus, Type: tString},
			{Name: fStatedLocation, Type: "ref<location>"},
			{Name: "home_region", Type: "ref<region_node>"},
		}},
		{Name: tLocation, Fields: []dsl.FieldDecl{
			{Name: fRegion, Type: tString},
			{Name: "kind", Type: tString},
			{Name: "ruler", Type: "ref<character>"},
		}},
		{Name: "region_node", Fields: []dsl.FieldDecl{
			{Name: "name", Type: tString},
			{Name: "ruler", Type: "ref<character>"},
		}},
		{Name: tFunction, Fields: []dsl.FieldDecl{
			{Name: "qualified_name", Type: tString},
			{Name: "path", Type: tString},
		}},
		{Name: "event", Fields: []dsl.FieldDecl{{Name: "session", Type: "int"}}},
	}
	edges := []dsl.EdgeTypeDecl{
		{Name: "home", From: tCharacter, To: tLocation},
		{Name: "connects_to", From: tLocation, To: tLocation},
		{Name: "CALLS", From: tFunction, To: tFunction},
	}
	info, err := dsl.NewSchemaInfo(notes, edges, 5)
	if err != nil {
		t.Fatalf("NewSchemaInfo: %v", err)
	}
	return info
}

// mustParse parses src with the test schema, failing the test on error.
func mustParse(t *testing.T, src string) *dsl.Query {
	t.Helper()
	q, err := dsl.ParseWithSchema(src, testSchema(t))
	if err != nil {
		t.Fatalf("ParseWithSchema(%q): unexpected error %v", src, err)
	}
	return q
}

// mustReject asserts that parsing src fails (a forbidden construct, §5.2).
func mustReject(t *testing.T, name, src string) {
	t.Helper()
	if _, err := dsl.ParseWithSchema(src, testSchema(t)); err == nil {
		t.Fatalf("%s: expected ParseWithSchema(%q) to reject, got nil error", name, src)
	}
}

// --- §5.5.1 Basic query forms (T1–T5) ---

func TestConformanceBasicForms(t *testing.T) {
	cases := map[string]string{
		"T1_scalar_where":   "MATCH (c:character) WHERE c.status = 'alive' RETURN c.id",
		"T2_single_edge":    "MATCH (c:character)-[:home]->(l:location) RETURN l.id",
		"T3_varlength":      "MATCH (a:location)-[:connects_to*1..3]->(b:location) RETURN b.id, hop_count",
		"T4_optional_match": "MATCH (c:character) OPTIONAL MATCH (c)-[:home]->(l:location) RETURN c.id, l.id",
		"T5_bare_ref":       "MATCH (c:character) RETURN c.id, c.stated_location",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) { mustParse(t, src) })
	}
}

// --- §5.5.2 Ref-join forms (T6–T12) ---

func TestConformanceRefJoins(t *testing.T) {
	t.Run("T6_field_traversal", func(t *testing.T) {
		mustParse(t, "MATCH (c:character) WHERE c.stated_location.region = $region RETURN c.id")
	})
	t.Run("T7_multi_use_dedup", func(t *testing.T) {
		mustParse(t, "MATCH (c:character) WHERE c.stated_location.region = $region RETURN c.id, c.stated_location.kind")
	})
	t.Run("T8_bare_plus_traversal", func(t *testing.T) {
		mustParse(t, "MATCH (c:character) RETURN c.stated_location, c.stated_location.kind")
	})
	t.Run("T9_depth2_chain_ok", func(t *testing.T) {
		mustParse(t, "MATCH (c:character) RETURN c.stated_location.ruler")
	})
	t.Run("T10_depth3_chain_reject", func(t *testing.T) {
		// Three ref hops (stated_location→location, ruler→character,
		// stated_location→location) exceeds the §5.4.1 depth cap of 2.
		mustReject(t, "T10", "MATCH (c:character) WHERE c.stated_location.ruler.stated_location.region = $n RETURN c.id")
	})
	t.Run("T11_untyped_ref_reject", func(t *testing.T) {
		_, err := dsl.NewSchemaInfo([]dsl.NoteTypeDecl{
			{Name: "x", Fields: []dsl.FieldDecl{{Name: "bad", Type: "ref"}}},
		}, nil, 2)
		if err == nil {
			t.Fatal("T11: expected untyped ref to be rejected at schema build")
		}
	})
	t.Run("T12_null_ref_short_circuit", func(t *testing.T) {
		// Behavioral; exercised in eval tests. Parse must accept the form.
		mustParse(t, "MATCH (c:character) RETURN c.id, c.stated_location.region")
	})
}

// --- §5.5.5 Allowed functions in WHERE (T23–T27) ---

func TestConformanceWhereFunctions(t *testing.T) {
	t.Run("T23_coalesce_param_accept", func(t *testing.T) {
		mustParse(t, "MATCH (e:event) WHERE e.session >= coalesce($min, 0) RETURN e.id")
	})
	t.Run("T24_coalesce_on_field_reject", func(t *testing.T) {
		mustReject(t, "T24", "MATCH (c:character) WHERE coalesce(c.region, 'x') = $r RETURN c.id")
	})
	t.Run("T25_unknown_func_reject", func(t *testing.T) {
		mustReject(t, "T25", "MATCH (c:character) WHERE c.region = upper($r) RETURN c.id")
	})
	t.Run("T26_starts_with_accept", func(t *testing.T) {
		mustParse(t, "MATCH (f:Function) WHERE STARTS_WITH(f.path, $app_root) RETURN f.qualified_name")
	})
	t.Run("T27_starts_with_arg_order_reject", func(t *testing.T) {
		mustReject(t, "T27", "MATCH (f:Function) WHERE STARTS_WITH($app_root, f.path) RETURN f.qualified_name")
	})
}

// --- §5.5.6 Edge-alias returns and filters (T28–T31) ---

func TestConformanceEdgeAliases(t *testing.T) {
	t.Run("T28_edge_intrinsic_return", func(t *testing.T) {
		mustParse(t, "MATCH (a:Function)-[e:CALLS]->(b:Function) RETURN e.id, e.kind")
	})
	t.Run("T29_edge_kind_where", func(t *testing.T) {
		mustParse(t, "MATCH (a:Function)-[e:CALLS]->(b:Function) WHERE e.kind = $kind RETURN b.qualified_name")
	})
	t.Run("T30_edge_metadata_reject", func(t *testing.T) {
		mustReject(t, "T30", "MATCH (a:Function)-[e:CALLS]->(b:Function) RETURN e.weight")
	})
	t.Run("T31_paths_as_objects_reject", func(t *testing.T) {
		mustReject(t, "T31", "MATCH p = (a:Function)-[:CALLS*1..3]->(b:Function) RETURN p")
	})
}

// --- §5.5.7 Forbidden constructs (T32–T39) ---

func TestConformanceForbidden(t *testing.T) {
	cases := map[string]string{
		"T32_string_concat":   "MATCH (c:character) WHERE c.name = $a + $b RETURN c.id",
		"T33_subquery":        "MATCH (c:character) WHERE c.id IN (MATCH (x:character) RETURN x.id) RETURN c.id",
		"T34_ddl_set":         "MATCH (c:character) SET c.status = 'dead' RETURN c.id",
		"T35_func_outside":    "MATCH (c:character) WHERE c.region = lower($r) RETURN c.id",
		"T36_varlen_no_upper": "MATCH (a:location)-[:connects_to*1..]->(b:location) RETURN b.id",
		"T37_varlen_exceeds":  "MATCH (a:location)-[:connects_to*1..9]->(b:location) RETURN b.id",
		"T38_like_reject":     "MATCH (c:character) WHERE c.name LIKE '%foo%' RETURN c.id",
		"T39_ltgt_reject":     "MATCH (c:character) WHERE c.region <> $r RETURN c.id",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) { mustReject(t, name, src) })
	}
}
