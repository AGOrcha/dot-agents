package dsl_test

import (
	"sort"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
	"github.com/AGOrcha/dot-agents/internal/kg/dsl"
)

// evalView builds the character/location view the lowering tests evaluate
// against. It mirrors the §5.4.2 conformance fixtures: characters with and
// without a stated_location ref / home edge, locations with regions.
func evalView() sdk.NamespaceView {
	return sdk.NamespaceView{
		Notes: []sdk.Note{
			{ID: idLocEU, Type: tLocation, Fields: map[string]any{fRegion: "eu", "kind": "city"}},
			{ID: idLocUS, Type: tLocation, Fields: map[string]any{fRegion: "us", "kind": "city"}},
			{ID: idCharA, Type: tCharacter, Fields: map[string]any{fStatus: vAlive, fStatedLocation: idLocEU}},
			{ID: "char-b", Type: tCharacter, Fields: map[string]any{fStatus: vAlive, fStatedLocation: idLocUS}},
			{ID: "char-c", Type: tCharacter, Fields: map[string]any{fStatus: vAlive}}, // NULL ref
		},
		Edges: []sdk.Edge{
			{Type: "home", From: idCharA, To: idLocEU},
			{Type: "home", From: "char-b", To: idLocUS},
		},
	}
}

// runIDs parses+evals a query and returns the "id" column values, sorted.
func runIDs(t *testing.T, src string, params map[string]any) []string {
	t.Helper()
	q := mustParse(t, src)
	rows, err := dsl.Eval(q, evalView(), params)
	if err != nil {
		t.Fatalf("Eval(%q): %v", src, err)
	}
	var out []string
	for _, r := range rows {
		if id, ok := r["c.id"].(string); ok {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

func eq(t *testing.T, name string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %v, want %v", name, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s: got %v, want %v", name, got, want)
		}
	}
}

// T13 — required source, ref-traversal predicate stays in WHERE; NULL-ref rows
// excluded (§5.4.2 test 1).
func TestConformanceT13RequiredStaysInWhere(t *testing.T) {
	got := runIDs(t, "MATCH (c:character) WHERE c.stated_location.region = $region RETURN c.id", map[string]any{fRegion: "eu"})
	eq(t, "T13", got, []string{idCharA})
}

// T14 — OPTIONAL source, predicate hoists to ON; all source rows preserved, the
// joined loc nulled when it fails (§5.4.2 test 2). All living characters appear.
func TestConformanceT14OptionalHoistsToOn(t *testing.T) {
	q := mustParse(t, "MATCH (c:character) OPTIONAL MATCH (c)-[:home]->(loc:location) WHERE loc.region = $region RETURN c.id, loc.id")
	rows, err := dsl.Eval(q, evalView(), map[string]any{fRegion: "eu"})
	if err != nil {
		t.Fatalf("T14 eval: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("T14: expected all 3 characters preserved, got %d rows", len(rows))
	}
	var withLoc int
	for _, r := range rows {
		if r["loc.id"] != nil {
			withLoc++
		}
	}
	if withLoc != 1 {
		t.Fatalf("T14: expected exactly 1 row with a non-NULL loc (char-a/eu), got %d", withLoc)
	}
}

// T15 — required source, ref in RETURN only (no WHERE filter); NULL refs
// preserved (no filter to reject them).
func TestConformanceT15RefInReturnOnly(t *testing.T) {
	q := mustParse(t, "MATCH (c:character) RETURN c.id, c.stated_location.region")
	rows, err := dsl.Eval(q, evalView(), nil)
	if err != nil {
		t.Fatalf("T15 eval: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("T15: expected 3 rows (all characters, NULL refs preserved), got %d", len(rows))
	}
}

// T16 — OPTIONAL source, no WHERE; all source rows preserved.
func TestConformanceT16OptionalNoWhere(t *testing.T) {
	q := mustParse(t, "MATCH (c:character) OPTIONAL MATCH (c)-[:home]->(loc:location) RETURN c.id, loc.id")
	rows, err := dsl.Eval(q, evalView(), nil)
	if err != nil {
		t.Fatalf("T16 eval: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("T16: expected 3 rows, got %d", len(rows))
	}
}

// T17 — required source with multiple WHERE predicates on the same ref; row
// excluded if any fail.
func TestConformanceT17MultipleRequiredPredicates(t *testing.T) {
	got := runIDs(t,
		"MATCH (c:character) WHERE c.stated_location.region = $region AND c.stated_location.kind = $kind RETURN c.id",
		map[string]any{fRegion: "eu", "kind": "city"})
	eq(t, "T17", got, []string{idCharA})
	none := runIDs(t,
		"MATCH (c:character) WHERE c.stated_location.region = $region AND c.stated_location.kind = $kind RETURN c.id",
		map[string]any{fRegion: "eu", "kind": "village"})
	if len(none) != 0 {
		t.Fatalf("T17: expected no rows when one predicate fails, got %v", none)
	}
}
