package crgrelease

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// covValLookup resolves a published tool, failing the test when the pinned
// release does not advertise it.
func covValLookup(t *testing.T, name string) Tool {
	t.Helper()
	tool, ok := Lookup(name)
	if !ok {
		t.Fatalf("tool %s not published", name)
	}
	return tool
}

// covValBind binds arguments to a published tool and requires success.
func covValBind(t *testing.T, name, arguments string) Args {
	t.Helper()
	args, toolErr := covValLookup(t, name).Bind(json.RawMessage(arguments))
	if toolErr != nil {
		t.Fatalf("bind %s(%s): unexpected error: %v", name, arguments, toolErr)
	}
	return args
}

// covValBindError binds arguments expected to fail and returns the release's
// diagnostic text.
func covValBindError(t *testing.T, name, arguments string) string {
	t.Helper()
	_, toolErr := covValLookup(t, name).Bind(json.RawMessage(arguments))
	if toolErr == nil {
		t.Fatalf("bind %s(%s): expected a validation error", name, arguments)
	}
	return toolErr.Message
}

// covValDiagnostic assembles one whole single-issue diagnostic exactly as the
// release renders it, so tests can pin the complete string rather than a
// fragment of it.
func covValDiagnostic(tool, loc, message, kind, annotation string) string {
	return "1 validation error for call[" + tool + "]\n" +
		loc + "\n  " + message + " [type=" + kind + annotation + "]\n" +
		"    For further information visit https://errors.pydantic.dev/" +
		PydanticVersion + "/v/" + kind
}

// A ToolError is surfaced through the error interface, and the release's
// unknown-tool answer is a ToolError too. The nil receiver matters because
// Bind returns a typed nil pointer on the success path.
func TestCovValToolErrorRendersThroughTheErrorInterface(t *testing.T) {
	var absent *ToolError
	if got := absent.Error(); got != "" {
		t.Errorf("nil ToolError.Error() = %q, want empty", got)
	}
	unknown := UnknownToolError("nope_tool")
	var asError error = unknown
	if got := asError.Error(); got != "Unknown tool: 'nope_tool'" {
		t.Errorf("ToolError.Error() = %q, want the release's unknown-tool text", got)
	}
	if asError.Error() != unknown.Message {
		t.Errorf("Error() = %q but Message = %q", asError.Error(), unknown.Message)
	}
}

// The accessors distinguish "defaulted" from "never set", which is what the
// tri-state parameters depend on, and Args remembers which tool it bound for.
func TestCovValArgsAccessorsReportWhetherAValueWasSet(t *testing.T) {
	supplied := covValBind(t, "build_or_update_graph_tool", `{"recurse_submodules":true}`)
	if got := supplied.Tool(); got != "build_or_update_graph_tool" {
		t.Errorf("Tool() = %q, want build_or_update_graph_tool", got)
	}
	if value, ok := supplied.OptBool("recurse_submodules"); !ok || !value {
		t.Errorf("OptBool(recurse_submodules) = (%v, %v), want (true, true)", value, ok)
	}

	omitted := covValBind(t, "build_or_update_graph_tool", `{}`)
	if value, ok := omitted.OptBool("recurse_submodules"); ok || value {
		t.Errorf("OptBool(recurse_submodules) = (%v, %v), want (false, false) when omitted", value, ok)
	}
	// A published default IS set, unlike a null-defaulted optional.
	if value, ok := omitted.OptBool("full_rebuild"); !ok || value {
		t.Errorf("OptBool(full_rebuild) = (%v, %v), want (false, true)", value, ok)
	}

	radius := covValBind(t, "get_impact_radius_tool", `{"max_depth":4}`)
	if value, ok := radius.OptInt("max_depth"); !ok || value != 4 {
		t.Errorf("OptInt(max_depth) = (%d, %v), want (4, true)", value, ok)
	}
	if value, ok := covValBind(t, "get_impact_radius_tool", `{}`).OptInt("max_depth"); !ok || value != 2 {
		t.Errorf("OptInt(max_depth) = (%d, %v), want the published default (2, true)", value, ok)
	}
	// Asking for the wrong kind, or for an unset optional, reports "not set"
	// rather than a zero that a handler could mistake for a real value.
	if value, ok := radius.OptInt("changed_files"); ok || value != 0 {
		t.Errorf("OptInt(changed_files) = (%d, %v), want (0, false)", value, ok)
	}
	if value, ok := radius.OptBool("max_depth"); ok || value {
		t.Errorf("OptBool(max_depth) = (%v, %v), want (false, false)", value, ok)
	}
}

// Anything that is not a JSON object never reaches a field validator: the
// release answers with one top-level dict_type failure and no echoed input.
func TestCovValBindRejectsArgumentsThatAreNotAnObject(t *testing.T) {
	want := covValDiagnostic("list_graph_stats_tool", "arguments",
		"Input should be a valid dictionary", "dict_type", "")
	for _, arguments := range []string{
		`[1,2]`,
		`123`,
		`"detect_changes_tool"`,
		`true`,
		`tru`,
		`{"repo_path":1`,
		`{"repo_path":}`,
		`{"repo_path":1, "other`,
	} {
		t.Run(arguments, func(t *testing.T) {
			if got := covValBindError(t, "list_graph_stats_tool", arguments); got != want {
				t.Errorf("diagnostic for %s =\n%q\nwant\n%q", arguments, got, want)
			}
		})
	}
}

// A repeated key is one argument, not two: the last value wins and the key is
// reported once, in the position of its first appearance.
func TestCovValBindCollapsesRepeatedArgumentKeys(t *testing.T) {
	want := covValDiagnostic("list_graph_stats_tool", "bogus",
		"Unexpected keyword argument", "unexpected_keyword_argument",
		", input_value=2, input_type=int")
	if got := covValBindError(t, "list_graph_stats_tool", `{"bogus":1,"bogus":2}`); got != want {
		t.Errorf("diagnostic =\n%q\nwant\n%q", got, want)
	}
}

// A default that its own schema rejects is a contract bug in the embedded
// release surface, and must not be reported to the client as if the client
// had sent a bad value.
func TestCovValBindRejectsADefaultThatViolatesItsSchema(t *testing.T) {
	tool := Tool{
		Name: "covval_broken_tool",
		Params: map[string]Param{
			"limit": {Name: "limit", Types: []string{"integer"}, Default: "not-a-number", HasDefault: true},
		},
		order: []string{"limit"},
	}
	_, toolErr := tool.Bind(nil)
	if toolErr == nil {
		t.Fatal("expected a contract error for an unsatisfiable default")
	}
	want := "crgrelease: covval_broken_tool default for limit violates its schema"
	if toolErr.Message != want {
		t.Errorf("contract error = %q, want %q", toolErr.Message, want)
	}
}

// An explicit null on a non-optional parameter is reported with the vocabulary
// of the parameter's own declared type, not a generic "null" complaint.
func TestCovValNullIssueUsesThePrimaryTypeVocabulary(t *testing.T) {
	tests := []struct {
		name        string
		types       []string
		wantMessage string
		wantKind    string
	}{
		{"integer", []string{"integer"}, "Input should be a valid integer", "int_type"},
		{"number", []string{"number"}, "Input should be a valid number", "float_type"},
		{"boolean", []string{"boolean"}, "Input should be a valid boolean", "bool_type"},
		{"array", []string{"array", "null"}, "Input should be a valid list", "list_type"},
		{"string", []string{"string"}, "Input should be a valid string", "string_type"},
		{"undeclared type falls back to string", nil,
			"Input should be a valid string", "string_type"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			issue := nullIssue("field", Param{Name: "field", Types: test.types})
			covValCheckNullIssue(t, issue, test.wantMessage, test.wantKind)
		})
	}
}

func covValCheckNullIssue(t *testing.T, issue validationIssue, wantMessage, wantKind string) {
	t.Helper()
	if issue.loc != "field" {
		t.Errorf("loc = %q, want field", issue.loc)
	}
	if issue.message != wantMessage {
		t.Errorf("message = %q, want %q", issue.message, wantMessage)
	}
	if issue.kind != wantKind {
		t.Errorf("kind = %q, want %q", issue.kind, wantKind)
	}
	if !issue.valueSet || issue.value != nil {
		t.Errorf("issue should echo an explicit None, got value=%v set=%v", issue.value, issue.valueSet)
	}
}

// The whole null diagnostic reaches the client for a real published boolean.
func TestCovValBindRejectsNullOnANonOptionalBoolean(t *testing.T) {
	want := covValDiagnostic("get_review_context_tool", "include_source",
		"Input should be a valid boolean", "bool_type",
		", input_value=None, input_type=NoneType")
	got := covValBindError(t, "get_review_context_tool", `{"include_source":null}`)
	if got != want {
		t.Errorf("diagnostic =\n%q\nwant\n%q", got, want)
	}
}

// covValCoercion is one lax-coercion rule: either an accepted value with the
// value it coerces to, or a rejection with the validator's own error kind.
type covValCoercion struct {
	name     string
	types    []string
	value    any
	want     any
	wantLoc  string
	wantMsg  string
	wantKind string
}

func covValRunCoercions(t *testing.T, tests []covValCoercion) {
	t.Helper()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, issue := coerce(Param{Name: "field", Types: test.types}, test.value)
			if test.wantKind == "" {
				covValCheckAccepted(t, got, issue, test.want)
				return
			}
			covValCheckRejected(t, got, issue, test)
		})
	}
}

func covValCheckAccepted(t *testing.T, got any, issue *validationIssue, want any) {
	t.Helper()
	if issue != nil {
		t.Fatalf("unexpected rejection: %s [type=%s]", issue.message, issue.kind)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("coerced to %#v, want %#v", got, want)
	}
}

func covValCheckRejected(t *testing.T, got any, issue *validationIssue, test covValCoercion) {
	t.Helper()
	if issue == nil {
		t.Fatalf("expected rejection %s, got value %#v", test.wantKind, got)
	}
	if issue.kind != test.wantKind {
		t.Errorf("kind = %q, want %q", issue.kind, test.wantKind)
	}
	if issue.message != test.wantMsg {
		t.Errorf("message = %q, want %q", issue.message, test.wantMsg)
	}
	if issue.loc != test.wantLoc {
		t.Errorf("loc = %q, want %q", issue.loc, test.wantLoc)
	}
	if !issue.valueSet || !reflect.DeepEqual(issue.value, test.value) {
		t.Errorf("issue should echo the rejected input, got value=%#v set=%v",
			issue.value, issue.valueSet)
	}
}

const (
	covValIntTypeMsg    = "Input should be a valid integer"
	covValFloatTypeMsg  = "Input should be a valid number"
	covValBoolTypeMsg   = "Input should be a valid boolean"
	covValStringTypeMsg = "Input should be a valid string"
)

// A string parameter accepts only strings; every other JSON kind is a
// string_type failure echoing the value the client sent.
func TestCovValCoerceStringParameter(t *testing.T) {
	covValRunCoercions(t, []covValCoercion{
		{name: "string passes through", types: []string{"string"}, value: "x", want: "x"},
		{
			name: "integer is not a string", types: []string{"string"}, value: int64(3),
			wantMsg: covValStringTypeMsg, wantKind: "string_type",
		},
		{
			name: "list is not a string", types: []string{"string"},
			value:   []any{"x"},
			wantMsg: covValStringTypeMsg, wantKind: "string_type",
		},
	})
}

// Integer coercion is deliberately lax — a numeric string, a bool and an
// integral float all bind — but a fractional float and an unparseable or
// out-of-range string are rejected with distinct error kinds.
func TestCovValCoerceIntegerParameter(t *testing.T) {
	const fromFloat = "Input should be a valid integer, got a number with a fractional part"
	const parsing = "Input should be a valid integer, unable to parse string as an integer"
	covValRunCoercions(t, []covValCoercion{
		{name: "integer", types: []string{"integer"}, value: int64(7), want: int64(7)},
		{name: "integral float", types: []string{"integer"}, value: float64(7), want: int64(7)},
		{name: "negative integral float", types: []string{"integer"}, value: -7.0, want: int64(-7)},
		{name: "true is one", types: []string{"integer"}, value: true, want: int64(1)},
		{name: "false is zero", types: []string{"integer"}, value: false, want: int64(0)},
		{name: "padded numeric string", types: []string{"integer"}, value: "  12\t", want: int64(12)},
		{
			name: "fractional float", types: []string{"integer"}, value: 7.5,
			wantMsg: fromFloat, wantKind: "int_from_float",
		},
		{
			name: "unparseable string", types: []string{"integer"}, value: "seven",
			wantMsg: parsing, wantKind: "int_parsing",
		},
		{
			name: "out of range string", types: []string{"integer"}, value: "99999999999999999999",
			wantMsg: parsing, wantKind: "int_parsing",
		},
		{
			name: "list", types: []string{"integer"}, value: []any{int64(1)},
			wantMsg: covValIntTypeMsg, wantKind: "int_type",
		},
		{
			name: "null-typed absence", types: []string{"integer"}, value: nil,
			wantMsg: covValIntTypeMsg, wantKind: "int_type",
		},
	})
}

// Number parameters coerce like integers but keep the fractional part, and an
// unparseable string is a float_parsing failure, not int_parsing.
func TestCovValCoerceNumberParameter(t *testing.T) {
	const parsing = "Input should be a valid number, unable to parse string as a number"
	covValRunCoercions(t, []covValCoercion{
		{name: "float", types: []string{"number"}, value: 2.5, want: 2.5},
		{name: "integer widens", types: []string{"number"}, value: int64(3), want: float64(3)},
		{name: "true is one", types: []string{"number"}, value: true, want: float64(1)},
		{name: "false is zero", types: []string{"number"}, value: false, want: float64(0)},
		{name: "numeric string", types: []string{"number"}, value: "2.5", want: 2.5},
		{
			name: "unparseable string", types: []string{"number"}, value: "two",
			wantMsg: parsing, wantKind: "float_parsing",
		},
		{
			name: "list", types: []string{"number"}, value: []any{2.5},
			wantMsg: covValFloatTypeMsg, wantKind: "float_type",
		},
	})
}

// Boolean coercion accepts the validator's whole vocabulary of truthy and
// falsey spellings, case-insensitively, and rejects everything else.
func TestCovValCoerceBooleanParameter(t *testing.T) {
	covValRunCoercions(t, append(covValBoolWordCases(), covValBoolRejectionCases()...))
}

func covValBoolWordCases() []covValCoercion {
	cases := []covValCoercion{
		{name: "bool true", types: []string{"boolean"}, value: true, want: true},
		{name: "bool false", types: []string{"boolean"}, value: false, want: false},
		{name: "integer one", types: []string{"boolean"}, value: int64(1), want: true},
		{name: "integer zero", types: []string{"boolean"}, value: int64(0), want: false},
		{name: "float one", types: []string{"boolean"}, value: float64(1), want: true},
		{name: "float zero", types: []string{"boolean"}, value: float64(0), want: false},
	}
	for _, word := range []string{"true", "T", "yes", "Y", "on", "1"} {
		cases = append(cases, covValCoercion{
			name: "string " + word, types: []string{"boolean"}, value: word, want: true,
		})
	}
	for _, word := range []string{"false", "f", "No", "n", "OFF", " 0 "} {
		cases = append(cases, covValCoercion{
			name: "string " + word, types: []string{"boolean"}, value: word, want: false,
		})
	}
	return cases
}

func covValBoolRejectionCases() []covValCoercion {
	const parsing = "Input should be a valid boolean, unable to interpret input"
	return []covValCoercion{
		{
			name: "other word", types: []string{"boolean"}, value: "maybe",
			wantMsg: parsing, wantKind: "bool_parsing",
		},
		{
			name: "integer two", types: []string{"boolean"}, value: int64(2),
			wantMsg: covValBoolTypeMsg, wantKind: "bool_type",
		},
		{
			name: "fractional float", types: []string{"boolean"}, value: 0.5,
			wantMsg: covValBoolTypeMsg, wantKind: "bool_type",
		},
		{
			name: "list", types: []string{"boolean"}, value: []any{true},
			wantMsg: covValBoolTypeMsg, wantKind: "bool_type",
		},
		{
			name: "absent", types: []string{"boolean"}, value: nil,
			wantMsg: covValBoolTypeMsg, wantKind: "bool_type",
		},
	}
}

// List parameters validate elements positionally, and the failing element's
// index becomes part of the reported location.
func TestCovValCoerceArrayParameter(t *testing.T) {
	covValRunCoercions(t, []covValCoercion{
		{
			name: "list of strings", types: []string{"array", "null"},
			value: []any{"a.go", "b.go"}, want: []string{"a.go", "b.go"},
		},
		{
			name: "empty list", types: []string{"array", "null"},
			value: []any{}, want: []string{},
		},
		{
			name: "string is not a list", types: []string{"array", "null"}, value: "a.go",
			wantMsg: "Input should be a valid list", wantKind: "list_type",
		},
	})
	// A rejected element reports its own index and value, not the whole list.
	_, issue := coerce(Param{Name: "changed_files", Types: []string{"array"}},
		[]any{"a.go", int64(2)})
	if issue == nil {
		t.Fatal("expected the non-string element to be rejected")
	}
	if issue.loc != ".1" || issue.kind != "string_type" || issue.value != int64(2) {
		t.Errorf("element issue = %+v, want loc .1 / string_type / 2", *issue)
	}
}

// A declared type the validator has no coercion rule for is passed through
// untouched rather than silently rejected.
func TestCovValCoercePassesThroughUnhandledTypes(t *testing.T) {
	value := map[string]any{"a": int64(1)}
	got, issue := coerce(Param{Name: "payload", Types: []string{"object"}}, value)
	if issue != nil {
		t.Fatalf("unexpected rejection: %s", issue.message)
	}
	if !reflect.DeepEqual(got, value) {
		t.Errorf("coerced to %#v, want the input unchanged", got)
	}
}

// A well-formed list argument binds end to end, which is what a change-oriented
// tool reads back through StringSlice.
func TestCovValBindAcceptsAListArgument(t *testing.T) {
	args := covValBind(t, "get_impact_radius_tool", `{"changed_files":["pkg/a.go","pkg/b.go"]}`)
	files, ok := args.StringSlice("changed_files")
	if !ok {
		t.Fatal("changed_files should be set")
	}
	if !reflect.DeepEqual(files, []string{"pkg/a.go", "pkg/b.go"}) {
		t.Errorf("changed_files = %#v", files)
	}
}

// Decoding keeps integers distinct from floats at every depth, because the
// distinction decides whether an integer parameter accepts the value.
func TestCovValDecodeAnyNormalizesNumbersAtEveryDepth(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want any
	}{
		{"null", `null`, nil},
		{"malformed json", `nope`, nil},
		{"empty payload", ``, nil},
		{"integer", `5`, int64(5)},
		{"integral float literal", `5.0`, float64(5)},
		{"fractional float", `5.5`, 5.5},
		{"string passes through", `"x"`, "x"},
		{"bool passes through", `true`, true},
		{"number beyond float range stays textual", `1e999`, "1e999"},
		{"list", `[1, 2.5, null]`, []any{int64(1), 2.5, nil}},
		{"nested object", `{"a":{"b":[7]}}`,
			map[string]any{"a": map[string]any{"b": []any{int64(7)}}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := decodeAny(json.RawMessage(test.raw)); !reflect.DeepEqual(got, test.want) {
				t.Errorf("decodeAny(%s) = %#v, want %#v", test.raw, got, test.want)
			}
		})
	}
}

// The echoed input in a diagnostic is a Python literal, so a nested argument
// has to render as Python renders it — including sorted dict keys, None, and
// True/False.
func TestCovValPyReprRendersPythonLiterals(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{"none", nil, "None"},
		{"true", true, "True"},
		{"false", false, "False"},
		{"string", "plain", "'plain'"},
		{"string with a quote", "it's", `'it\'s'`},
		{"integer", int64(-7), "-7"},
		{"float", 2.5, "2.5"},
		{"integral float", float64(3), "3"},
		{"list", []any{int64(1), nil, true, "x"}, "[1, None, True, 'x']"},
		{"string list", []string{"a", "b"}, "['a', 'b']"},
		{"empty list", []any{}, "[]"},
		{"empty dict", map[string]any{}, "{}"},
		{"dict sorts its keys", map[string]any{"b": int64(2), "a": 1.5}, "{'a': 1.5, 'b': 2}"},
		{"empty ordered dict", orderedMap{}, "{}"},
		{
			name:  "ordered dict keeps insertion order",
			value: orderedMap{keys: []string{"z", "a"}, values: map[string]any{"z": int64(1), "a": nil}},
			want:  "{'z': 1, 'a': None}",
		},
		{"unmodelled value", 5, "5"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := pyRepr(test.value); got != test.want {
				t.Errorf("pyRepr(%#v) = %q, want %q", test.value, got, test.want)
			}
		})
	}
}

// input_type names the Python type the validator saw.
func TestCovValPyTypeNamesThePythonType(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{"none", nil, "NoneType"},
		{"bool", true, "bool"},
		{"string", "x", "str"},
		{"integer", int64(1), "int"},
		{"float", 1.5, "float"},
		{"list", []any{}, "list"},
		{"string list", []string{}, "list"},
		{"dict", map[string]any{}, "dict"},
		{"ordered dict", orderedMap{}, "dict"},
		{"unmodelled value", 1, "object"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := pyType(test.value); got != test.want {
				t.Errorf("pyType(%#v) = %q, want %q", test.value, got, test.want)
			}
		})
	}
}

// End to end: an unexpected argument holding a nested structure echoes that
// structure as a Python literal, which is the rendering clients display.
func TestCovValBindEchoesNestedArgumentsAsPythonLiterals(t *testing.T) {
	want := covValDiagnostic("list_graph_stats_tool", "bogus",
		"Unexpected keyword argument", "unexpected_keyword_argument",
		", input_value={'flag': True, 'items': [1, 2.5, None], 'name': 'it\\'s'}, input_type=dict")
	got := covValBindError(t, "list_graph_stats_tool",
		`{"bogus":{"name":"it's","items":[1,2.5,null],"flag":true}}`)
	if got != want {
		t.Errorf("diagnostic =\n%q\nwant\n%q", got, want)
	}
}

// A missing required argument with nothing supplied echoes an empty dict.
func TestCovValMissingArgumentEchoesAnEmptyDict(t *testing.T) {
	want := covValDiagnostic("semantic_search_nodes_tool", "query",
		"Missing required argument", "missing_argument",
		", input_value={}, input_type=dict")
	if got := covValBindError(t, "semantic_search_nodes_tool", ``); got != want {
		t.Errorf("diagnostic =\n%q\nwant\n%q", got, want)
	}
	if got := covValBindError(t, "semantic_search_nodes_tool", `null`); got != want {
		t.Errorf("null arguments should behave as no arguments, got\n%q", got)
	}
}

// The pluralised header distinguishes one failure from several.
func TestCovValDiagnosticHeaderCountsIssues(t *testing.T) {
	single := covValBindError(t, "semantic_search_nodes_tool", `{"query":"x","limit":"nope"}`)
	if !strings.HasPrefix(single, "1 validation error for call[semantic_search_nodes_tool]\n") {
		t.Errorf("single-issue header = %q", strings.SplitN(single, "\n", 2)[0])
	}
	multiple := covValBindError(t, "semantic_search_nodes_tool", `{"limit":"nope"}`)
	if !strings.HasPrefix(multiple, "2 validation errors for call[semantic_search_nodes_tool]\n") {
		t.Errorf("multi-issue header = %q", strings.SplitN(multiple, "\n", 2)[0])
	}
}
