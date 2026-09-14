package codegraph

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// The `_hints` block is session-stateful: a next-step suggestion disappears
// once its tool has been called on the connection, `related` only offers files
// the session has not seen, and both the history and the node set are bounded.
// The fixture-driven tests in this package all reset the session first, so they
// only ever observe the FIRST call of a connection. Everything below drives the
// session across calls and past its bounds, which is where a dropped cap or a
// forgotten `recordFiles` would hide.

// covHintEngine returns an engine with a fresh hint session. Hint generation
// touches no store, so a bare root is enough.
func covHintEngine(t *testing.T) *Engine {
	t.Helper()
	e := Open(t.TempDir())
	e.ResetSession()
	return e
}

// covHintBlock attaches hints to result and returns the `_hints` block.
func covHintBlock(t *testing.T, e *Engine, hintName string, result map[string]any) map[string]any {
	t.Helper()
	out := e.AttachHints(hintName, result)
	block, ok := out["_hints"].(map[string]any)
	if !ok {
		t.Fatalf("AttachHints(%q) attached no _hints block: %#v", hintName, out)
	}
	return block
}

// covHintNextTools is the tool names of a block's next_steps, in order.
func covHintNextTools(t *testing.T, block map[string]any) []string {
	t.Helper()
	steps, ok := block["next_steps"].([]hintSuggestion)
	if !ok {
		t.Fatalf("next_steps = %#v, want []hintSuggestion", block["next_steps"])
	}
	tools := make([]string, 0, len(steps))
	for _, step := range steps {
		tools = append(tools, step.Tool)
	}
	return tools
}

// covHintList is a block's string-valued category.
func covHintList(t *testing.T, block map[string]any, key string) []string {
	t.Helper()
	items, ok := block[key].([]string)
	if !ok {
		t.Fatalf("%s = %#v, want []string", key, block[key])
	}
	return items
}

// covHintSame compares two string lists, treating nil and empty as equal
// because the caps normalise an absent category to an empty array.
func covHintSame(got, want []string) bool {
	return strings.Join(got, "\x00") == strings.Join(want, "\x00")
}

// covHintRepeat is n copies of a tool name, for driving the intent window.
func covHintRepeat(tool string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = tool
	}
	return out
}

// covHintFillHistory makes n throwaway calls, none of which appears in any
// workflow adjacency list, so only the history bound can be responsible for
// what falls out of the session.
func covHintFillHistory(t *testing.T, e *Engine, n int) {
	t.Helper()
	for i := range n {
		covHintBlock(t, e, fmt.Sprintf("cov_hint_filler_%d", i), map[string]any{})
	}
}

func TestCovHintAttachHintsIgnoresNilResult(t *testing.T) {
	e := covHintEngine(t)
	if got := e.AttachHints("list_flows", nil); got != nil {
		t.Fatalf("AttachHints with a nil result = %#v, want nil", got)
	}
	if called := e.Session().toolsCalled; len(called) != 0 {
		t.Fatalf("a nil result advanced the session to %v, want it untouched", called)
	}
}

func TestCovHintNextStepsForAFreshSession(t *testing.T) {
	for _, tc := range []struct {
		name string
		hint string
		want []string
	}{
		{"a known tool offers its adjacency list", "list_flows",
			[]string{"get_flow", "get_affected_flows", "get_architecture_overview"}},
		{"a fourth candidate is dropped by the per-category cap", "detect_changes",
			[]string{"get_review_context", "get_affected_flows", "get_impact_radius"}},
		{"a tool with no adjacency offers nothing", "cov_hint_unknown_tool", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			block := covHintBlock(t, covHintEngine(t), tc.hint, map[string]any{})
			if got := covHintNextTools(t, block); !covHintSame(got, tc.want) {
				t.Fatalf("next_steps for %q = %v, want %v", tc.hint, got, tc.want)
			}
		})
	}
}

func TestCovHintNextStepsDropAlreadyCalledTools(t *testing.T) {
	e := covHintEngine(t)
	covHintBlock(t, e, "get_flow", map[string]any{})
	block := covHintBlock(t, e, "list_flows", map[string]any{})
	want := []string{"get_affected_flows", "get_architecture_overview"}
	if got := covHintNextTools(t, block); !covHintSame(got, want) {
		t.Fatalf("next_steps = %v, want %v (get_flow was already called)", got, want)
	}
}

// TestCovHintToolHistoryForgetsCallsBeyondTheCap pins the bounded history
// through its one observable consequence: a suggestion suppressed by an early
// call comes BACK once that call has been evicted. Without the bound the
// session would grow forever and get_flow would stay suppressed.
func TestCovHintToolHistoryForgetsCallsBeyondTheCap(t *testing.T) {
	e := covHintEngine(t)
	covHintBlock(t, e, "get_flow", map[string]any{})
	covHintFillHistory(t, e, hintsMaxToolsHistory)
	if got := len(e.Session().toolsCalled); got != hintsMaxToolsHistory {
		t.Fatalf("history length = %d, want it bounded at %d", got, hintsMaxToolsHistory)
	}
	block := covHintBlock(t, e, "list_flows", map[string]any{})
	want := []string{"get_flow", "get_affected_flows", "get_architecture_overview"}
	if got := covHintNextTools(t, block); !covHintSame(got, want) {
		t.Fatalf("next_steps after the history rolled over = %v, want %v", got, want)
	}
}

func TestCovHintInferIntentClassifiesRecentCalls(t *testing.T) {
	for _, tc := range []struct {
		name    string
		history []string
		want    string
	}{
		{"an empty session is exploring", nil, "exploring"},
		{"survey tools are exploring", []string{"list_flows", "list_graph_stats"}, "exploring"},
		{"change tools are reviewing", []string{"detect_changes", "get_impact_radius"}, "reviewing"},
		{"traversal tools are debugging",
			[]string{"query_graph", "get_flow", "semantic_search_nodes"}, "debugging"},
		{"rewrite tools are refactoring", []string{"refactor", "find_dead_code"}, "refactoring"},
		{"unclassified tools fall back to exploring",
			[]string{"build_or_update_graph", "cov_hint_unknown_tool"}, "exploring"},
		{"a tie goes to the first declared intent",
			[]string{"query_graph", "detect_changes"}, "reviewing"},
		{"only the last ten calls count",
			append(covHintRepeat("detect_changes", 11), covHintRepeat("query_graph", 10)...),
			"debugging"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewSessionState()
			s.toolsCalled = tc.history
			if got := s.inferIntent(); got != tc.want {
				t.Fatalf("inferIntent over %d calls = %q, want %q", len(tc.history), got, tc.want)
			}
		})
	}
}

func TestCovHintSessionRecordsInferredIntent(t *testing.T) {
	e := covHintEngine(t)
	covHintBlock(t, e, "detect_changes", map[string]any{})
	if got := e.Session().intent; got != "reviewing" {
		t.Fatalf("session intent after detect_changes = %q, want %q", got, "reviewing")
	}
}

func TestCovHintWarningsFromTestGaps(t *testing.T) {
	for _, tc := range []struct {
		name string
		gaps any
		want []string
	}{
		{"named gaps are comma-joined", []any{
			map[string]any{"name": "pkg.A"}, map[string]any{"name": "pkg.B"}},
			[]string{"Test coverage gaps: pkg.A, pkg.B"}},
		{"a single gap carries no separator",
			[]any{map[string]any{"name": "pkg.A"}}, []string{"Test coverage gaps: pkg.A"}},
		{"an entry without a name string falls back to its rendering",
			[]any{map[string]any{"qualified_name": "pkg.C"}, "pkg.D", 7},
			[]string{"Test coverage gaps: map[qualified_name:pkg.C], pkg.D, 7"}},
		{"at most five gaps are named",
			[]any{"a", "b", "c", "d", "e", "f", "g"}, []string{"Test coverage gaps: a, b, c, d, e"}},
		{"an empty gap list is not a warning", []any{}, nil},
		{"a non-list test_gaps is ignored", []string{"pkg.A"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := extractHintWarnings(map[string]any{"test_gaps": tc.gaps})
			if !covHintSame(got, tc.want) {
				t.Fatalf("warnings = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCovHintWarningsFromRiskScore(t *testing.T) {
	for _, tc := range []struct {
		name string
		risk any
		want []string
	}{
		{"a float64 above the threshold warns", 0.9,
			[]string{"High risk score (0.90) — review carefully"}},
		{"a float32 above the threshold warns", float32(0.75),
			[]string{"High risk score (0.75) — review carefully"}},
		{"an int above the threshold warns", 1,
			[]string{"High risk score (1.00) — review carefully"}},
		{"an int64 above the threshold warns", int64(2),
			[]string{"High risk score (2.00) — review carefully"}},
		{"exactly at the threshold does not warn", 0.7, nil},
		{"below the threshold does not warn", 0.1, nil},
		{"a non-numeric score is ignored", "high", nil},
		{"a nil score is ignored", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := extractHintWarnings(map[string]any{"risk_score": tc.risk})
			if !covHintSame(got, tc.want) {
				t.Fatalf("warnings for risk_score %#v = %v, want %v", tc.risk, got, tc.want)
			}
		})
	}
}

func TestCovHintWarningsFromPayloadWarnings(t *testing.T) {
	for _, tc := range []struct {
		name     string
		existing any
		want     []string
	}{
		{"strings pass through", []any{"a", "b"}, []string{"a", "b"}},
		{"an object contributes its message",
			[]any{map[string]any{"message": "boom"}}, []string{"boom"}},
		{"the entry budget counts entries that contribute nothing",
			[]any{"a", map[string]any{"message": 7}, 42, "d"}, []string{"a"}},
		{"at most three entries are read", []any{"a", "b", "c", "d"}, []string{"a", "b", "c"}},
		{"a non-list warnings field is ignored", "boom", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := extractHintWarnings(map[string]any{"warnings": tc.existing})
			if !covHintSame(got, tc.want) {
				t.Fatalf("warnings = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestCovHintWarningsCappedAndOrdered drives all three warning sources at once,
// which is the only way to observe the category cap trimming warnings.
func TestCovHintWarningsCappedAndOrdered(t *testing.T) {
	block := covHintBlock(t, covHintEngine(t), "detect_changes", map[string]any{
		"test_gaps":  []any{map[string]any{"name": "pkg.A"}},
		"risk_score": 0.9,
		"warnings":   []any{"w1", "w2", "w3"},
	})
	want := []string{
		"Test coverage gaps: pkg.A",
		"High risk score (0.90) — review carefully",
		"w1",
	}
	if got := covHintList(t, block, "warnings"); !covHintSame(got, want) {
		t.Fatalf("warnings = %v, want the first %d of gaps, risk, payload", got, hintsMaxPerCategory)
	}
}

func TestCovHintRelatedFiles(t *testing.T) {
	for _, tc := range []struct {
		name     string
		impacted any
		want     []string
	}{
		{"a non-list impacted_files is ignored", "a.go", nil},
		{"distinct paths are capped per category",
			[]any{"a.go", "b.go", "c.go", "d.go"}, []string{"a.go", "b.go", "c.go"}},
		{"duplicates and non-strings are skipped",
			[]any{"a.go", 7, "a.go", "b.go"}, []string{"a.go", "b.go"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			block := covHintBlock(t, covHintEngine(t), "detect_changes",
				map[string]any{"impacted_files": tc.impacted})
			if got := covHintList(t, block, "related"); !covHintSame(got, tc.want) {
				t.Fatalf("related = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCovHintRelatedIsEmptyWithoutImpactedFiles(t *testing.T) {
	block := covHintBlock(t, covHintEngine(t), "list_flows", map[string]any{})
	if got := covHintList(t, block, "related"); len(got) != 0 {
		t.Fatalf("related = %v, want empty for a payload with no impacted_files", got)
	}
}

// TestCovHintRelatedSkipsFilesTheSessionAlreadySaw pins two coupled contracts:
// a result's own impacted files are still suggested (related is built before
// the result is tracked), and on the NEXT call every file the session has seen —
// whether it arrived as changed_files or impacted_files — is withheld.
func TestCovHintRelatedSkipsFilesTheSessionAlreadySaw(t *testing.T) {
	e := covHintEngine(t)
	first := covHintBlock(t, e, "detect_changes", map[string]any{
		"changed_files":  []any{"a.go", 7},
		"impacted_files": []any{"b.go"},
	})
	if got := covHintList(t, first, "related"); !covHintSame(got, []string{"b.go"}) {
		t.Fatalf("first related = %v, want this call's own impacted file", got)
	}
	second := covHintBlock(t, e, "get_impact_radius", map[string]any{
		"impacted_files": []any{"a.go", "b.go", "c.go"},
	})
	if got := covHintList(t, second, "related"); !covHintSame(got, []string{"c.go"}) {
		t.Fatalf("second related = %v, want only the file the session has not seen", got)
	}
}

func TestCovHintTrackResultRecordsQualifiedNames(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result map[string]any
		want   map[string]bool
	}{
		{"every node-bearing key contributes its qualified names", map[string]any{
			"results": []any{map[string]any{"qualified_name": "pkg.A"}, "not-a-map"},
			"changed_nodes": []any{
				map[string]any{"qualified_name": ""},
				map[string]any{"name": "pkg.X"},
			},
			"impacted_nodes": []any{map[string]any{"qualified_name": "pkg.B"}},
			"other_nodes":    []any{map[string]any{"qualified_name": "pkg.C"}},
		}, map[string]bool{"pkg.A": true, "pkg.B": true}},
		{"a non-list node key is skipped",
			map[string]any{"results": "pkg.A"}, map[string]bool{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := covHintEngine(t)
			covHintBlock(t, e, "semantic_search_nodes", tc.result)
			if got := e.Session().nodesQueried; !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("nodesQueried = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCovHintNodeTrackingIsBounded(t *testing.T) {
	entries := make([]any, hintsMaxNodesTracked+1)
	for i := range entries {
		entries[i] = map[string]any{"qualified_name": fmt.Sprintf("pkg.N%d", i)}
	}
	e := covHintEngine(t)
	covHintBlock(t, e, "semantic_search_nodes", map[string]any{"results": entries})
	if got := len(e.Session().nodesQueried); got != hintsMaxNodesTracked {
		t.Fatalf("tracked nodes = %d, want it bounded at %d", got, hintsMaxNodesTracked)
	}
}

func TestCovHintCapsBoundEachCategory(t *testing.T) {
	t.Run("an empty category marshals as an array, not null", func(t *testing.T) {
		encoded, err := json.Marshal(map[string]any{
			"next_steps": capSuggestions(nil),
			"related":    capStrings(nil),
		})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		const want = `{"next_steps":[],"related":[]}`
		if string(encoded) != want {
			t.Fatalf("encoded = %s, want %s", encoded, want)
		}
	})

	t.Run("an overflowing category keeps its leading entries", func(t *testing.T) {
		suggestions := capSuggestions([]hintSuggestion{
			{Tool: "a"}, {Tool: "b"}, {Tool: "c"}, {Tool: "d"},
		})
		tools := make([]string, 0, len(suggestions))
		for _, suggestion := range suggestions {
			tools = append(tools, suggestion.Tool)
		}
		if !covHintSame(tools, []string{"a", "b", "c"}) {
			t.Fatalf("capSuggestions = %v, want the first %d", tools, hintsMaxPerCategory)
		}
		if got := capStrings([]string{"a", "b", "c", "d"}); !covHintSame(got, []string{"a", "b", "c"}) {
			t.Fatalf("capStrings = %v, want the first %d", got, hintsMaxPerCategory)
		}
	})
}

func TestCovHintToFloatCoercesNumericKinds(t *testing.T) {
	for _, tc := range []struct {
		name   string
		value  any
		want   float64
		wantOK bool
	}{
		{"float64", 1.5, 1.5, true},
		{"float32", float32(2.5), 2.5, true},
		{"int", 3, 3, true},
		{"int64", int64(4), 4, true},
		{"a numeric string is rejected", "5", 0, false},
		{"a bool is rejected", true, 0, false},
		{"nil is rejected", nil, 0, false},
		{"an unsigned int is rejected", uint(6), 0, false},
		{"a json.Number is rejected", json.Number("7"), 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := toFloat(tc.value)
			if got != tc.want || ok != tc.wantOK {
				t.Fatalf("toFloat(%#v) = (%v, %v), want (%v, %v)",
					tc.value, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestCovHintJoinCommaSeparatesItems(t *testing.T) {
	for _, tc := range []struct {
		name  string
		items []string
		want  string
	}{
		{"no items", nil, ""},
		{"one item", []string{"a"}, "a"},
		{"many items", []string{"a", "b", "c"}, "a, b, c"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := joinComma(tc.items); got != tc.want {
				t.Fatalf("joinComma(%v) = %q, want %q", tc.items, got, tc.want)
			}
		})
	}
}
