package crgrelease

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// callFixture is one generated tools/call record.
type callFixture struct {
	Tool      string          `json:"tool"`
	Case      string          `json:"case"`
	Arguments json.RawMessage `json:"arguments"`
	IsError   bool            `json:"is_error"`
	Content   []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

func loadCallFixture(t *testing.T, name string) callFixture {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(fixtureDir(t), "calls", name+".json"))
	if err != nil {
		t.Fatalf("read call fixture %s: %v", name, err)
	}
	var fixture callFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("decode call fixture %s: %v", name, err)
	}
	return fixture
}

// The release reports invalid arguments as a successful response carrying
// isError plus a validator diagnostic. Reproducing that message exactly is
// the contract: clients surface it verbatim, and its structure (count line,
// field location, machine-readable kind, echoed input, docs URL) is what
// tooling parses.
func TestBindReproducesReleaseValidationErrors(t *testing.T) {
	for _, name := range []string{
		"list_graph_stats_tool__invalid_unknown_argument",
		"query_graph_tool__invalid_missing_required",
		"get_impact_radius_tool__invalid_wrong_type",
		"semantic_search_nodes_tool__invalid_wrong_type",
	} {
		t.Run(name, func(t *testing.T) {
			fixture := loadCallFixture(t, name)
			if !fixture.IsError {
				t.Fatalf("%s is not an error fixture", name)
			}
			tool, ok := Lookup(fixture.Tool)
			if !ok {
				t.Fatalf("tool %s not published", fixture.Tool)
			}
			_, toolErr := tool.Bind(fixture.Arguments)
			if toolErr == nil {
				t.Fatalf("expected a validation error for %s", fixture.Arguments)
			}
			want := fixture.Content[0].Text
			if toolErr.Message != want {
				t.Errorf("validation message differs from the release\n got: %q\nwant: %q",
					toolErr.Message, want)
			}
		})
	}
}

// Multi-error ordering is observable: the release reports declared parameters
// in declaration order and unexpected arguments last, and echoes the whole
// arguments object — in the order the client sent it — for a missing argument.
func TestBindErrorOrderingFollowsDeclarationOrder(t *testing.T) {
	tool, ok := Lookup("query_graph_tool")
	if !ok {
		t.Fatal("query_graph_tool not published")
	}
	_, toolErr := tool.Bind(json.RawMessage(
		`{"bogus":1,"target":"x","max_results":"nope"}`))
	if toolErr == nil {
		t.Fatal("expected validation errors")
	}
	want := "3 validation errors for call[query_graph_tool]\n" +
		"pattern\n  Missing required argument [type=missing_argument, " +
		"input_value={'bogus': 1, 'target': 'x', 'max_results': 'nope'}, input_type=dict]\n" +
		"    For further information visit https://errors.pydantic.dev/2.13/v/missing_argument\n" +
		"max_results\n  Input should be a valid integer, unable to parse string as an integer " +
		"[type=int_parsing, input_value='nope', input_type=str]\n" +
		"    For further information visit https://errors.pydantic.dev/2.13/v/int_parsing\n" +
		"bogus\n  Unexpected keyword argument [type=unexpected_keyword_argument, " +
		"input_value=1, input_type=int]\n" +
		"    For further information visit " +
		"https://errors.pydantic.dev/2.13/v/unexpected_keyword_argument"
	if toolErr.Message != want {
		t.Errorf("ordering differs from the release\n got: %q\nwant: %q", toolErr.Message, want)
	}
}

// Omitted parameters take the release's published defaults. A handler that
// re-implemented a default would drift the moment the release changed one.
func TestBindAppliesPublishedDefaults(t *testing.T) {
	tool, ok := Lookup("get_review_context_tool")
	if !ok {
		t.Fatal("get_review_context_tool not published")
	}
	args, toolErr := tool.Bind(nil)
	if toolErr != nil {
		t.Fatalf("unexpected error: %v", toolErr)
	}
	if got := args.Int("max_depth"); got != 2 {
		t.Errorf("max_depth default = %d, want 2", got)
	}
	if got := args.Int("max_lines_per_file"); got != 200 {
		t.Errorf("max_lines_per_file default = %d, want 200", got)
	}
	if got := args.Int("max_results"); got != 100 {
		t.Errorf("max_results default = %d, want 100", got)
	}
	if got := args.Int("max_files"); got != 25 {
		t.Errorf("max_files default = %d, want 25", got)
	}
	if got := args.String("base"); got != "HEAD~1" {
		t.Errorf("base default = %q, want HEAD~1", got)
	}
	if got := args.String("detail_level"); got != "standard" {
		t.Errorf("detail_level default = %q, want standard", got)
	}
	if !args.Bool("include_source") {
		t.Error("include_source default = false, want true")
	}
	// An optional parameter with a null default stays UNSET, which is how the
	// change-oriented tools tell "auto-detect from git" from "nothing changed".
	if _, ok := args.StringSlice("changed_files"); ok {
		t.Error("changed_files should be unset when omitted")
	}
	if _, ok := args.OptString("repo_root"); ok {
		t.Error("repo_root should be unset when omitted")
	}
}

// Lax coercion is part of the release's accepted-input contract: rejecting a
// numeric string that the release accepts would break working clients.
func TestBindCoercionMatchesRelease(t *testing.T) {
	tests := []struct {
		name    string
		tool    string
		args    string
		wantErr string
		check   func(t *testing.T, args Args)
	}{
		{
			name: "numeric string becomes an integer",
			tool: "semantic_search_nodes_tool",
			args: `{"query":"x","limit":"5"}`,
			check: func(t *testing.T, args Args) {
				if got := args.Int("limit"); got != 5 {
					t.Errorf("limit = %d, want 5", got)
				}
			},
		},
		{
			name: "integral float becomes an integer",
			tool: "semantic_search_nodes_tool",
			args: `{"query":"x","limit":5.0}`,
			check: func(t *testing.T, args Args) {
				if got := args.Int("limit"); got != 5 {
					t.Errorf("limit = %d, want 5", got)
				}
			},
		},
		{
			name:    "fractional float is rejected",
			tool:    "semantic_search_nodes_tool",
			args:    `{"query":"x","limit":5.5}`,
			wantErr: "got a number with a fractional part [type=int_from_float",
		},
		{
			name: "bool counts as an integer",
			tool: "semantic_search_nodes_tool",
			args: `{"query":"x","limit":true}`,
			check: func(t *testing.T, args Args) {
				if got := args.Int("limit"); got != 1 {
					t.Errorf("limit = %d, want 1", got)
				}
			},
		},
		{
			name:    "a number is not a string",
			tool:    "semantic_search_nodes_tool",
			args:    `{"query":123}`,
			wantErr: "Input should be a valid string [type=string_type, input_value=123, input_type=int]",
		},
		{
			name:    "null on a required string is rejected",
			tool:    "semantic_search_nodes_tool",
			args:    `{"query":null}`,
			wantErr: "Input should be a valid string [type=string_type, input_value=None, input_type=NoneType]",
		},
		{
			name: "null on an optional parameter means unset",
			tool: "semantic_search_nodes_tool",
			args: `{"query":"x","kind":null}`,
			check: func(t *testing.T, args Args) {
				if _, ok := args.OptString("kind"); ok {
					t.Error("kind should be unset")
				}
			},
		},
		{
			name:    "a string is not a list",
			tool:    "get_impact_radius_tool",
			args:    `{"changed_files":"pkg/auth/auth.go"}`,
			wantErr: "Input should be a valid list [type=list_type, input_value='pkg/auth/auth.go', input_type=str]",
		},
		{
			name:    "list elements are validated positionally",
			tool:    "get_impact_radius_tool",
			args:    `{"changed_files":[1,2]}`,
			wantErr: "changed_files.0\n  Input should be a valid string",
		},
		{
			name:    "null on a defaulted integer is rejected",
			tool:    "get_impact_radius_tool",
			args:    `{"max_depth":null}`,
			wantErr: "Input should be a valid integer [type=int_type, input_value=None, input_type=NoneType]",
		},
		{
			name: "an explicitly empty list is not an unset list",
			tool: "get_impact_radius_tool",
			args: `{"changed_files":[]}`,
			check: func(t *testing.T, args Args) {
				files, ok := args.StringSlice("changed_files")
				if !ok {
					t.Fatal("changed_files should be set")
				}
				if len(files) != 0 {
					t.Errorf("changed_files = %v, want empty", files)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tool, ok := Lookup(test.tool)
			if !ok {
				t.Fatalf("tool %s not published", test.tool)
			}
			args, toolErr := tool.Bind(json.RawMessage(test.args))
			if test.wantErr != "" {
				if toolErr == nil {
					t.Fatalf("expected an error containing %q", test.wantErr)
				}
				if !strings.Contains(toolErr.Message, test.wantErr) {
					t.Fatalf("error %q does not contain %q", toolErr.Message, test.wantErr)
				}
				return
			}
			if toolErr != nil {
				t.Fatalf("unexpected error: %v", toolErr)
			}
			test.check(t, args)
		})
	}
}

// A bound argument set is what a bridge-routed call forwards, so the defaulted
// values must round-trip as the release's own argument values.
func TestArgsRawCarriesDefaultedValues(t *testing.T) {
	tool, ok := Lookup("list_flows_tool")
	if !ok {
		t.Fatal("list_flows_tool not published")
	}
	args, toolErr := tool.Bind(json.RawMessage(`{"limit":3}`))
	if toolErr != nil {
		t.Fatalf("unexpected error: %v", toolErr)
	}
	want := map[string]any{
		"sort_by":      "criticality",
		"limit":        int64(3),
		"detail_level": "standard",
	}
	if got := args.Raw(); !reflect.DeepEqual(got, want) {
		t.Errorf("raw args = %#v, want %#v", got, want)
	}
}

// An unknown tool has its own release-defined answer, distinct from a schema
// failure.
func TestUnknownToolMessage(t *testing.T) {
	fixture := loadCallFixture(t, "list_graph_stats_tool__invalid_unknown_argument")
	_ = fixture // the unknown-tool case has no fixture; the release string is pinned here.
	if got := UnknownToolError("nope_tool").Message; got != "Unknown tool: 'nope_tool'" {
		t.Errorf("unknown tool message = %q", got)
	}
}
