package dsl_test

import (
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
	"github.com/AGOrcha/dot-agents/internal/kg/dsl"
)

// numSchema is a tiny schema with a numeric field for operator/aggregate cover.
func numSchema(t *testing.T) dsl.SchemaInfo {
	t.Helper()
	info, err := dsl.NewSchemaInfo([]dsl.NoteTypeDecl{
		{Name: "n", Fields: []dsl.FieldDecl{
			{Name: "v", Type: "int"},
			{Name: "tag", Type: tString},
			{Name: fActive, Type: "bool"},
		}},
	}, nil, 3)
	if err != nil {
		t.Fatalf("numSchema: %v", err)
	}
	return info
}

func numView() sdk.NamespaceView {
	return sdk.NamespaceView{Notes: []sdk.Note{
		{ID: "n1", Type: "n", Fields: map[string]any{"v": float64(1), "tag": "x", fActive: true}},
		{ID: "n2", Type: "n", Fields: map[string]any{"v": float64(5), "tag": "y", fActive: false}},
		{ID: "n3", Type: "n", Fields: map[string]any{"v": float64(3), "tag": "x", fActive: true}},
	}}
}

func runNum(t *testing.T, src string, params map[string]any) []sdk.Row {
	t.Helper()
	q, err := dsl.ParseWithSchema(src, numSchema(t))
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	rows, err := dsl.Eval(q, numView(), params)
	if err != nil {
		t.Fatalf("eval %q: %v", src, err)
	}
	return rows
}

// TestOperators exercises every operator in the closed §5.1 set so
// compareNumbers / compareNonNumeric are fully covered.
func TestOperators(t *testing.T) {
	cases := []struct {
		op   string
		want int
	}{
		{"=", 1}, {"!=", 2}, {"<", 1}, {"<=", 2}, {">", 1}, {">=", 2},
	}
	for _, c := range cases {
		t.Run(c.op, func(t *testing.T) {
			rows := runNum(t, "MATCH (n:n) WHERE n.v "+c.op+" $t RETURN n.id", map[string]any{"t": float64(3)})
			if len(rows) != c.want {
				t.Fatalf("op %s: got %d rows, want %d", c.op, len(rows), c.want)
			}
		})
	}
}

// TestStringAndBoolEquality covers compareNonNumeric on string/bool fields.
func TestStringAndBoolEquality(t *testing.T) {
	rows := runNum(t, "MATCH (n:n) WHERE n.tag = $t RETURN n.id", map[string]any{"t": "x"})
	if len(rows) != 2 {
		t.Fatalf("string eq: got %d, want 2", len(rows))
	}
	rows = runNum(t, "MATCH (n:n) WHERE n.active != $a RETURN n.id", map[string]any{"a": true})
	if len(rows) != 1 {
		t.Fatalf("bool neq: got %d, want 1", len(rows))
	}
}

// TestMinMaxAggregate covers minMax + aggregateValue for both functions.
func TestMinMaxAggregate(t *testing.T) {
	rows := runNum(t, "MATCH (n:n) RETURN min(n.v), max(n.v)", nil)
	if len(rows) != 1 {
		t.Fatalf("min/max: expected one grouped row, got %d", len(rows))
	}
	if rows[0]["min"] != float64(1) || rows[0]["max"] != float64(5) {
		t.Fatalf("min/max values wrong: %v", rows[0])
	}
}

// TestMinMaxEmpty covers the no-rows minMax branch (returns nil).
func TestMinMaxEmpty(t *testing.T) {
	q, err := dsl.ParseWithSchema("MATCH (n:n) WHERE n.v > $t RETURN min(n.v)", numSchema(t))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	rows, err := dsl.Eval(q, numView(), map[string]any{"t": float64(100)})
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if rows[0]["min"] != nil {
		t.Fatalf("empty min should be nil, got %v", rows[0]["min"])
	}
}

// TestReturnCoalesce covers evalReturnCoalesce + literalFromArg with both a
// resolved field and a literal default fallback.
func TestReturnCoalesce(t *testing.T) {
	view := sdk.NamespaceView{Notes: []sdk.Note{
		{ID: "n1", Type: "n", Fields: map[string]any{"tag": "present"}},
		{ID: "n2", Type: "n", Fields: map[string]any{}}, // tag absent → default
	}}
	q, err := dsl.ParseWithSchema("MATCH (n:n) RETURN n.id, coalesce(n.tag, 'fallback')", numSchema(t))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	rows, err := dsl.Eval(q, view, nil)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	got := map[string]any{}
	for _, r := range rows {
		got[r["n.id"].(string)] = r["coalesce"]
	}
	if got["n1"] != "present" || got["n2"] != "fallback" {
		t.Fatalf("return coalesce: %v", got)
	}
}

// TestMultiNodeMatch covers applyNodeMatch (a second bare MATCH cross-product).
func TestMultiNodeMatch(t *testing.T) {
	info, err := dsl.NewSchemaInfo([]dsl.NoteTypeDecl{
		{Name: "a", Fields: []dsl.FieldDecl{{Name: "k", Type: tString}}},
		{Name: "b", Fields: []dsl.FieldDecl{{Name: "k", Type: tString}}},
	}, nil, 2)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	q, err := dsl.ParseWithSchema("MATCH (x:a) MATCH (y:b) WHERE x.k = $k RETURN x.id, y.id", info)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	view := sdk.NamespaceView{Notes: []sdk.Note{
		{ID: "a1", Type: "a", Fields: map[string]any{"k": "yes"}},
		{ID: "b1", Type: "b", Fields: map[string]any{"k": "z"}},
		{ID: "b2", Type: "b", Fields: map[string]any{"k": "z"}},
	}}
	rows, err := dsl.Eval(q, view, map[string]any{"k": "yes"})
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if len(rows) != 2 { // a1 × {b1, b2}
		t.Fatalf("multi-node: expected 2 cross-product rows, got %d", len(rows))
	}
}

// TestReturnOnlyQuery covers the no-MATCH path (the none-adapter shape).
func TestReturnOnlyQuery(t *testing.T) {
	q, err := dsl.Parse("RETURN $changed_ids")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	rows, err := dsl.Eval(q, sdk.NamespaceView{}, map[string]any{"changed_ids": []string{"a"}})
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("return-only: expected 1 row, got %d", len(rows))
	}
}

// TestFloatLiteral covers the decimal-number lexer branch and parseNumber float.
func TestFloatLiteral(t *testing.T) {
	rows := runNum(t, "MATCH (n:n) WHERE n.v >= $t RETURN n.id", map[string]any{"t": float64(3)})
	_ = rows
	q, err := dsl.ParseWithSchema("MATCH (n:n) WHERE n.v > 2.5 RETURN n.id", numSchema(t))
	if err != nil {
		t.Fatalf("float parse: %v", err)
	}
	rows, err = dsl.Eval(q, numView(), nil)
	if err != nil {
		t.Fatalf("float eval: %v", err)
	}
	if len(rows) != 2 { // v=3 and v=5
		t.Fatalf("float literal: got %d, want 2", len(rows))
	}
}

// TestInListAny covers the []any branch of inList.
func TestInListAny(t *testing.T) {
	q, err := dsl.ParseWithSchema("MATCH (n:n) WHERE n.id IN $ids RETURN n.id", numSchema(t))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	rows, err := dsl.Eval(q, numView(), map[string]any{"ids": []any{"n2"}})
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if len(rows) != 1 || rows[0]["n.id"] != "n2" {
		t.Fatalf("IN []any: got %v", rows)
	}
}

// TestStartsWithNonString covers the non-string operand branch (no match).
func TestStartsWithNonString(t *testing.T) {
	rows := runNum(t, "MATCH (n:n) WHERE STARTS_WITH(n.v, $p) RETURN n.id", map[string]any{"p": "1"})
	if len(rows) != 0 {
		t.Fatalf("STARTS_WITH on non-string: expected 0 rows, got %d", len(rows))
	}
}

// TestParseErrors covers assorted lexer/parser error branches.
func TestParseErrors(t *testing.T) {
	bad := []string{
		"MATCH (n:n) WHERE n.v = 'unterminated RETURN n.id", // unterminated string
		"MATCH (n:n) WHERE n.v = $ RETURN n.id @",           // unexpected char
		"MATCH (n:n)",                   // missing RETURN
		"MATCH (n:n) RETURN n.id extra", // trailing tokens
		"MATCH (n:n) WHERE n.id IN $x AND n.v IN $y RETURN n.id", // IN on non-id
	}
	for _, src := range bad {
		if _, err := dsl.Parse(src); err == nil {
			t.Errorf("expected parse error for %q", src)
		}
	}
}

// TestSchemaValidationErrors covers schema-aware rejection branches.
func TestSchemaValidationErrors(t *testing.T) {
	info := numSchema(t)
	bad := []string{
		"MATCH (n:n) WHERE n.missing = $t RETURN n.id", // unknown field
		"MATCH (n:n) RETURN z.id",                      // unbound alias in RETURN
	}
	for _, src := range bad {
		if _, err := dsl.ParseWithSchema(src, info); err == nil {
			t.Errorf("expected schema error for %q", src)
		}
	}
}

// TestNewSchemaInfoRefParse covers ref<type> parsing for a valid typed ref.
func TestNewSchemaInfoRefParse(t *testing.T) {
	info, err := dsl.NewSchemaInfo([]dsl.NoteTypeDecl{
		{Name: "a", Fields: []dsl.FieldDecl{{Name: "r", Type: "ref<b>"}}},
		{Name: "b", Fields: []dsl.FieldDecl{{Name: "k", Type: tString}}},
	}, nil, 2)
	if err != nil {
		t.Fatalf("ref parse: %v", err)
	}
	if _, err := dsl.ParseWithSchema("MATCH (x:a) RETURN x.r.k", info); err != nil {
		t.Fatalf("ref traversal should validate: %v", err)
	}
}

// TestEnvTimeAfterNonDate covers timeAfterFires branches: missing field and a
// non-date string (both must not fire).
func TestEnvTimeAfterNonDate(t *testing.T) {
	preds := []dsl.EnvPredicate{{NoteType: tEvidence, Kind: dsl.KindTimeAfter, Field: fExpiresAt}}
	notes := []sdk.Note{
		{ID: "e1", Type: tEvidence, Fields: map[string]any{}},                         // missing field
		{ID: "e2", Type: tEvidence, Fields: map[string]any{fExpiresAt: "not-a-date"}}, // unparseable
		{ID: "e3", Type: "other", Fields: map[string]any{fExpiresAt: "2020-01-01"}},   // wrong type
	}
	tagged, err := dsl.ApplyEnvTrigger(preds, notes, dsl.EnvTrigger{Kind: dsl.KindTimeAfter})
	if err != nil {
		t.Fatalf("env: %v", err)
	}
	if len(tagged) != 0 {
		t.Fatalf("expected no notes tagged, got %v", tagged)
	}
}

// TestParseErrorMessages sanity-checks a couple of error strings so the precise
// messages stay stable for adapter authors.
func TestParseErrorMessages(t *testing.T) {
	_, err := dsl.Parse("MATCH (n:n) WHERE n.v <> $t RETURN n.id")
	if err == nil || !strings.Contains(err.Error(), "'<>'") {
		t.Fatalf("expected <> rejection message, got %v", err)
	}
}
