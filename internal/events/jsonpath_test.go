package events

import (
	"encoding/json"
	"testing"
)

func decode(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("decode %q: %v", s, err)
	}
	return v
}

func TestJSONPathSuccess(t *testing.T) {
	doc := decode(t, `{
		"a": {"b": "deep"},
		"items": [{"id": 1}, {"id": 2}],
		"checks": [
			{"name": "lint", "conclusion": "SUCCESS"},
			{"name": "test", "conclusion": "FAILURE"}
		],
		"flag": true,
		"score": 3.5
	}`)
	tests := []struct {
		name string
		expr string
		want any
	}{
		{"dotted", ".a.b", "deep"},
		{"dollar prefix", "$.a.b", "deep"},
		{"array index", ".items[1].id", float64(2)},
		{"filter string", ".checks[?(@.conclusion=='FAILURE')].name", "test"},
		{"filter double quote", `.checks[?(@.conclusion=="SUCCESS")].name`, "lint"},
		{"filter bool", ".checks[?(@.name=='lint')].conclusion", "SUCCESS"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := JSONPath(doc, tc.expr)
			if err != nil {
				t.Fatalf("JSONPath(%q): %v", tc.expr, err)
			}
			if got != tc.want {
				t.Fatalf("JSONPath(%q) = %v (%T), want %v", tc.expr, got, got, tc.want)
			}
		})
	}
}

func TestJSONPathScalarEqualsTypes(t *testing.T) {
	doc := decode(t, `{"rows":[{"v":true},{"v":3},{"v":"x"}]}`)
	tests := []struct {
		expr string
		want any
	}{
		{".rows[?(@.v==true)].v", true},
		{".rows[?(@.v==3)].v", float64(3)},
		{".rows[?(@.v=='x')].v", "x"},
	}
	for _, tc := range tests {
		got, err := JSONPath(doc, tc.expr)
		if err != nil {
			t.Fatalf("JSONPath(%q): %v", tc.expr, err)
		}
		if got != tc.want {
			t.Fatalf("JSONPath(%q) = %v, want %v", tc.expr, got, tc.want)
		}
	}
}

func TestJSONPathErrors(t *testing.T) {
	doc := decode(t, `{"items":[{"id":1}],"obj":{"k":"v"}}`)
	tests := []struct {
		name string
		expr string
	}{
		{"empty", ""},
		{"only dollar", "$"},
		{"only dots", "$.."},
		{"missing field", ".nope"},
		{"field on non-object", ".items.id"},
		{"index on non-array", ".obj[0]"},
		{"index out of range", ".items[9]"},
		{"negative index", ".items[-1]"},
		{"bad index", ".items[x]"},
		{"filter on non-array", ".obj[?(@.k=='v')]"},
		{"filter no match", ".items[?(@.id=='zzz')]"},
		{"filter missing eq", ".items[?(@.id)]"},
		{"filter empty field", ".items[?(@.=='1')]"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := JSONPath(doc, tc.expr); err == nil {
				t.Fatalf("expected error for %q", tc.expr)
			}
		})
	}
}

func TestScalarEqualsNonScalar(t *testing.T) {
	if scalarEquals(map[string]any{"a": 1}, "anything") {
		t.Fatalf("object value should never equal a scalar literal")
	}
	if scalarEquals(nil, "null") {
		t.Fatalf("nil value should never equal a scalar literal")
	}
}

func TestJSONPathFilterSkipsNonObjectElements(t *testing.T) {
	doc := decode(t, `{"items":["scalar",{"id":"match"}]}`)
	got, err := JSONPath(doc, ".items[?(@.id=='match')].id")
	if err != nil {
		t.Fatalf("JSONPath: %v", err)
	}
	if got != "match" {
		t.Fatalf("got %v, want match", got)
	}
}

func TestJSONPathUnclosedBracket(t *testing.T) {
	doc := decode(t, `{"items":[{"id":1}]}`)
	if _, err := JSONPath(doc, ".items[0"); err == nil {
		t.Fatalf("expected error for unclosed bracket")
	}
}

func TestUnquote(t *testing.T) {
	tests := []struct{ in, want string }{
		{"'x'", "x"},
		{`"x"`, "x"},
		{"x", "x"},
		{"'mismatch\"", "'mismatch\""},
		{"", ""},
	}
	for _, tc := range tests {
		if got := unquote(tc.in); got != tc.want {
			t.Fatalf("unquote(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
