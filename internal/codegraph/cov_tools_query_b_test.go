package codegraph

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/crgrelease"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// This file pins the search / size / stats half of tools_query.go plus the
// release store readers and pure helpers those handlers are built on.
//
// Two kinds of oracle are used, deliberately:
//
//   - the recorded release contract (testdata/crg-release/v2.3.8) for the
//     behaviours the fixture repository can express, via newQueryFixture;
//   - an injectable store for the ones it cannot: a backend without FTS5, a
//     handle that fails mid-query, a graph with a Verilog signal in it, and
//     the rank/boost arithmetic whose whole point is an ORDER that differs
//     from the order the index returned.
//
// The ordering assertions are the load-bearing ones. Every boosted list here
// is constructed so that a ranking which silently flattened — returning the
// index's own order, or losing one multiplier — produces a different sequence
// than the one asserted.

// ── injectable store ─────────────────────────────────────────────────────────

// covQBStore extends the package's fakeStore with the four readers the search,
// count and edge-name paths need and fakeStore does not implement, plus one
// fault seam fakeStore cannot express.
type covQBStore struct {
	*fakeStore

	// searchIDs / searchErr back SearchNodesFTS, the RANK-ordered read
	// hybrid search fuses.
	searchIDs []int64
	searchErr error
	// wordIDs / wordErr back SearchNodesFTSWords, the rowid-ordered read
	// search_nodes uses.
	wordIDs []int64
	wordErr error
	// wordCount / wordCountErr back CountNodesFTSWords.
	wordCount    int
	wordCountErr error
	// byQualified backs GetNode, which the edge-name language gate joins on.
	byQualified map[string]graphstore.GraphNode
	getNodeErr  error
	// failCallNo makes the failCallNo'th GetEdgesBySource call fail (1-based;
	// 0 disables). releaseTransitiveTests reads the SAME source in three
	// separate passes — the direct pass, the bare-name pass and the CALLS
	// walk — so a source-keyed failure cannot tell those error arms apart.
	// A handle that dies partway through a multi-pass query is the real
	// failure this models.
	failCallNo int
	edgeCalls  int
}

func covQBNewStore() *covQBStore { return &covQBStore{fakeStore: &fakeStore{}} }

func (s *covQBStore) SearchNodesFTS(string, int) ([]int64, error) {
	return s.searchIDs, s.searchErr
}

func (s *covQBStore) SearchNodesFTSWords([]string, int) ([]int64, error) {
	return s.wordIDs, s.wordErr
}

func (s *covQBStore) CountNodesFTSWords([]string) (int, error) {
	return s.wordCount, s.wordCountErr
}

func (s *covQBStore) GetNode(qualified string) (*graphstore.GraphNode, error) {
	if s.getNodeErr != nil {
		return nil, s.getNodeErr
	}
	node, ok := s.byQualified[qualified]
	if !ok {
		return nil, nil
	}
	return &node, nil
}

func (s *covQBStore) GetEdgesBySource(qualified string) ([]graphstore.GraphEdge, error) {
	s.edgeCalls++
	if s.failCallNo != 0 && s.edgeCalls == s.failCallNo {
		return nil, errFake
	}
	return s.fakeStore.GetEdgesBySource(qualified)
}

// ── fixture builders ─────────────────────────────────────────────────────────

// covQBNode is the node shape these readers look at: identity, kind, language.
func covQBNode(id int64, kind, name, file string) graphstore.GraphNode {
	return graphstore.GraphNode{
		ID:            id,
		Kind:          kind,
		Name:          name,
		QualifiedName: file + "::" + name,
		FilePath:      file,
		Language:      "go",
	}
}

// covQBSpanned is covQBNode with a line span, which is all find_large_functions
// filters and ranks on.
func covQBSpanned(id int64, kind, name, file string, start, end int) graphstore.GraphNode {
	node := covQBNode(id, kind, name, file)
	node.LineStart, node.LineEnd = start, end
	return node
}

func covQBEdge(id int64, kind, source, target string) graphstore.GraphEdge {
	return graphstore.GraphEdge{
		ID:              id,
		Kind:            kind,
		SourceQualified: source,
		TargetQualified: target,
	}
}

// covQBBySource groups edges the way fakeStore's GetEdgesBySource reads them.
func covQBBySource(edges ...graphstore.GraphEdge) map[string][]graphstore.GraphEdge {
	out := map[string][]graphstore.GraphEdge{}
	for _, edge := range edges {
		out[edge.SourceQualified] = append(out[edge.SourceQualified], edge)
	}
	return out
}

// covQBByTarget groups edges the way fakeStore's GetEdgesByTarget reads them.
func covQBByTarget(edges ...graphstore.GraphEdge) map[string][]graphstore.GraphEdge {
	out := map[string][]graphstore.GraphEdge{}
	for _, edge := range edges {
		out[edge.TargetQualified] = append(out[edge.TargetQualified], edge)
	}
	return out
}

// covQBBind binds raw arguments through the pinned release's own schema, so a
// handler test sees exactly the defaults and coercions a real call produces.
func covQBBind(t *testing.T, tool, arguments string) crgrelease.Args {
	t.Helper()
	published, ok := crgrelease.Lookup(tool)
	if !ok {
		t.Fatalf("%s is not published by code-review-graph %s", tool, crgrelease.Version)
	}
	args, bindErr := published.Bind(json.RawMessage(arguments))
	if bindErr != nil {
		t.Fatalf("bind %s %s: %v", tool, arguments, bindErr)
	}
	return args
}

// covQBPayload runs a native handler and returns its object payload.
func covQBPayload(t *testing.T, e *Engine, tool, arguments string,
	handler func(*Engine, crgrelease.Args) (any, error),
) map[string]any {
	t.Helper()
	result, err := handler(e, covQBBind(t, tool, arguments))
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	payload, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("%s returned %T, want an object", tool, result)
	}
	return payload
}

// covQBWantFake asserts a handler refused rather than degrading to an empty
// answer: a store failure the caller cannot see is a silently wrong result.
func covQBWantFake(t *testing.T, label string, result any, err error) {
	t.Helper()
	if !errors.Is(err, errFake) {
		t.Fatalf("%s: err = %v, want the injected failure (payload %#v)", label, err, result)
	}
}

// covQBHitNames renders a ranked hit list as `name` in rank order, which is
// what every ordering assertion below compares.
func covQBHitNames(hits []SearchHit) []string {
	out := make([]string, 0, len(hits))
	for _, hit := range hits {
		out = append(out, hit.Node.Name)
	}
	return out
}

// covQBEdgeIDs renders an edge list as its ids in order.
func covQBEdgeIDs(edges []graphstore.GraphEdge) []int64 {
	out := make([]int64, 0, len(edges))
	for _, edge := range edges {
		out = append(out, edge.ID)
	}
	return out
}

// covQBAssertClose compares a fused score exactly enough to catch a lost or
// duplicated boost multiplier while tolerating float reassociation.
func covQBAssertClose(t *testing.T, label string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-12 {
		t.Errorf("%s = %v, want %v", label, got, want)
	}
}

// ── pure helpers ─────────────────────────────────────────────────────────────

func TestCovQBLastSegmentHelpers(t *testing.T) {
	for _, tc := range []struct{ in, dot, qualified, stem string }{
		// pathStem is a FILE stem, so it drops only the last dotted suffix:
		// a dotted FQN is not a path and keeps everything but its tail.
		{in: "com.example.Session", dot: "Session", qualified: "com.example.Session", stem: "com.example"},
		{in: "pkg/auth/auth.go::Login", dot: "go::Login", qualified: "Login", stem: "auth"},
		{in: "Login", dot: "Login", qualified: "Login", stem: "Login"},
		{in: "", dot: "", qualified: "", stem: "."},
		// A trailing dot leaves an empty last segment rather than the value.
		{in: "trailing.", dot: "", qualified: "trailing.", stem: "trailing"},
		// A leading dot is an extension-less dotfile, so pathStem keeps it
		// whole: the release's Path.stem does the same (index > 0, not >= 0).
		{in: ".hidden", dot: "hidden", qualified: ".hidden", stem: ".hidden"},
		// Windows separators are normalized before the basename is taken.
		{in: `a\b\c.test.go`, dot: "go", qualified: `a\b\c.test.go`, stem: "c.test"},
	} {
		t.Run(tc.in, func(t *testing.T) {
			if got := lastDotSegment(tc.in); got != tc.dot {
				t.Errorf("lastDotSegment(%q) = %q, want %q", tc.in, got, tc.dot)
			}
			if got := lastQualifiedSegment(tc.in); got != tc.qualified {
				t.Errorf("lastQualifiedSegment(%q) = %q, want %q", tc.in, got, tc.qualified)
			}
			if got := pathStem(tc.in); got != tc.stem {
				t.Errorf("pathStem(%q) = %q, want %q", tc.in, got, tc.stem)
			}
		})
	}
}

// TestCovQBHasWindowsDrive pins the absolute-path probe anchorUnderRoot gates
// on. A false negative turns `C:/repo/a.go` into `<root>/C:/repo/a.go` and
// looks up a path that cannot exist.
func TestCovQBHasWindowsDrive(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{value: "C:/repo/a.go", want: true},
		{value: "z:/repo/a.go", want: true},
		// The slash is required: a bare `C:` is relative to the drive's CWD.
		{value: "C:repo/a.go", want: false},
		// Paths reach this already normalized, so a backslash is not a
		// separator any more.
		{value: `C:\repo\a.go`, want: false},
		{value: "1:/repo", want: false},
		{value: "CC:/repo", want: false},
		{value: "C:", want: false},
		{value: "", want: false},
		{value: "/repo/a.go", want: false},
	} {
		t.Run(tc.value, func(t *testing.T) {
			if got := hasWindowsDrive(tc.value); got != tc.want {
				t.Errorf("hasWindowsDrive(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

// TestCovQBAnchorUnderRoot pins pathlib's `root / target`: an absolute target
// DISCARDS the root instead of being appended to it.
func TestCovQBAnchorUnderRoot(t *testing.T) {
	for _, tc := range []struct{ root, target, want string }{
		{root: "/repo", target: "pkg/auth/auth.go", want: "/repo/pkg/auth/auth.go"},
		{root: "/repo/", target: "auth.go", want: "/repo/auth.go"},
		{root: "/repo", target: "/elsewhere/a.go", want: "/elsewhere/a.go"},
		{root: "/repo", target: "C:/other/a.go", want: "C:/other/a.go"},
		{root: `C:\repo`, target: `pkg\a.go`, want: "C:/repo/pkg/a.go"},
	} {
		t.Run(tc.root+"|"+tc.target, func(t *testing.T) {
			if got := anchorUnderRoot(tc.root, tc.target); got != tc.want {
				t.Errorf("anchorUnderRoot(%q, %q) = %q, want %q", tc.root, tc.target, got, tc.want)
			}
		})
	}
}

// TestCovQBRelativeToRoot pins find_large_functions' `relative_path`. The
// fallback arm is the release's ValueError branch: a path that is not under the
// root is reported RAW, so a reader can still tell which file was meant.
func TestCovQBRelativeToRoot(t *testing.T) {
	for _, tc := range []struct{ root, filePath, want string }{
		{root: "/repo", filePath: "/repo/pkg/auth/auth.go", want: "pkg/auth/auth.go"},
		{root: "/repo/", filePath: "/repo/auth.go", want: "auth.go"},
		{root: "/repo", filePath: "/repo", want: "."},
		{root: "/repo", filePath: "/repository/auth.go", want: "/repository/auth.go"},
		{root: "/repo", filePath: "/other/a.go", want: "/other/a.go"},
		{root: "C:/repo", filePath: "C:/repo/a.go", want: "a.go"},
		{root: "/repo", filePath: `C:\other\a.go`, want: `C:\other\a.go`},
		{root: "", filePath: "a.go", want: "a.go"},
		// Separators are normalized before the prefix test, so a stored
		// Windows path under a slash-form root still relativizes.
		{root: "/repo", filePath: `/repo\pkg\a.go`, want: "pkg/a.go"},
	} {
		t.Run(tc.root+"|"+tc.filePath, func(t *testing.T) {
			if got := relativeToRoot(tc.root, tc.filePath); got != tc.want {
				t.Errorf("relativeToRoot(%q, %q) = %q, want %q", tc.root, tc.filePath, got, tc.want)
			}
		})
	}
}

// TestCovQBNodeLineCount pins the inclusive span. A node missing either bound
// counts as 0 rather than as a negative span, which would sort ahead of every
// real function under a descending size order.
func TestCovQBNodeLineCount(t *testing.T) {
	for _, tc := range []struct {
		name       string
		start, end int
		want       int
	}{
		{name: "no_range", want: 0},
		{name: "no_start", end: 12, want: 0},
		{name: "no_end", start: 12, want: 0},
		{name: "single_line", start: 7, end: 7, want: 1},
		{name: "inclusive_span", start: 10, end: 20, want: 11},
	} {
		t.Run(tc.name, func(t *testing.T) {
			node := covQBSpanned(1, "Function", "f", "/repo/a.go", tc.start, tc.end)
			if got := nodeLineCount(node); got != tc.want {
				t.Errorf("nodeLineCount(%d..%d) = %d, want %d", tc.start, tc.end, got, tc.want)
			}
		})
	}
}

func TestCovQBIdentifierShapeHelpers(t *testing.T) {
	for _, tc := range []struct {
		in     string
		java   bool
		pascal bool
		letter bool
	}{
		{in: "", java: false, pascal: false, letter: false},
		{in: "a", java: true, pascal: false, letter: true},
		{in: "Ab", java: true, pascal: true, letter: true},
		{in: "AB", java: true, pascal: false, letter: true},
		{in: "ab", java: true, pascal: false, letter: true},
		// A digit is legal after the first character only.
		{in: "A1", java: true, pascal: false, letter: true},
		{in: "a9b", java: true, pascal: false, letter: true},
		{in: "9a", java: false, pascal: false, letter: true},
		{in: "9", java: false, pascal: false, letter: false},
		{in: "_$x", java: true, pascal: false, letter: true},
		{in: "a-b", java: false, pascal: false, letter: true},
		{in: "a.b", java: false, pascal: false, letter: true},
		{in: "123", java: false, pascal: false, letter: false},
		{in: "___", java: true, pascal: false, letter: false},
		// Non-ASCII letters are not ASCII letters, and not Java identifier
		// characters as this probe spells them.
		{in: "Ωμ", java: false, pascal: false, letter: false},
		{in: "Ωx", java: false, pascal: false, letter: true},
	} {
		t.Run(tc.in, func(t *testing.T) {
			if got := isJavaIdentifier(tc.in); got != tc.java {
				t.Errorf("isJavaIdentifier(%q) = %v, want %v", tc.in, got, tc.java)
			}
			if got := startsPascalCase(tc.in); got != tc.pascal {
				t.Errorf("startsPascalCase(%q) = %v, want %v", tc.in, got, tc.pascal)
			}
			if got := containsASCIILetter(tc.in); got != tc.letter {
				t.Errorf("containsASCIILetter(%q) = %v, want %v", tc.in, got, tc.letter)
			}
		})
	}
}

// TestCovQBStringList pins the `isinstance(..., list)` distinction: an absent
// key and a non-list value are the same thing, but a PRESENT empty list is
// not — it means "the extractor recorded no candidates", which is why the
// second result exists at all.
func TestCovQBStringList(t *testing.T) {
	for _, tc := range []struct {
		name   string
		value  any
		want   []string
		isList bool
	}{
		{name: "string_slice", value: []string{"a", "b"}, want: []string{"a", "b"}, isList: true},
		{name: "decoded_json_list", value: []any{"a", "b"}, want: []string{"a", "b"}, isList: true},
		{name: "drops_non_strings", value: []any{"a", 1.0, nil, "b"}, want: []string{"a", "b"}, isList: true},
		{name: "present_empty_list", value: []any{}, want: []string{}, isList: true},
		{name: "absent", value: nil, want: nil, isList: false},
		{name: "scalar", value: "a", want: nil, isList: false},
		{name: "object", value: map[string]any{"a": 1}, want: nil, isList: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, isList := stringList(tc.value)
			if isList != tc.isList {
				t.Fatalf("stringList(%#v) list = %v, want %v", tc.value, isList, tc.isList)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("stringList(%#v) = %#v, want %#v", tc.value, got, tc.want)
			}
		})
	}
}

// TestCovQBSanitizeAll pins the candidate-list projection: the cap is applied
// BEFORE sanitizing, and a control character is stripped rather than being
// allowed to forge structure in a rendered response.
func TestCovQBSanitizeAll(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values []string
		limit  int
		want   []string
	}{
		{name: "under_limit", values: []string{"a", "b"}, limit: 5, want: []string{"a", "b"}},
		{name: "at_limit", values: []string{"a", "b"}, limit: 2, want: []string{"a", "b"}},
		{name: "over_limit", values: []string{"a", "b", "c"}, limit: 2, want: []string{"a", "b"}},
		{name: "zero_limit", values: []string{"a"}, limit: 0, want: []string{}},
		{name: "empty", values: nil, limit: 3, want: []string{}},
		{name: "strips_controls", values: []string{"a\x00b\x07"}, limit: 3, want: []string{"ab"}},
		// Tab and newline survive sanitizing, as upstream keeps them.
		{name: "keeps_tab", values: []string{"a\tb"}, limit: 3, want: []string{"a\tb"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeAll(tc.values, tc.limit)
			if !slices.Equal(got, tc.want) {
				t.Errorf("sanitizeAll(%#v, %d) = %#v, want %#v", tc.values, tc.limit, got, tc.want)
			}
		})
	}
}

// TestCovQBExtraInt pins the deliberate rejection of a fractional value:
// truncating 2.5 to 2 would invent a count the release never reported.
func TestCovQBExtraInt(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
		want  int
		ok    bool
	}{
		{name: "int", value: 3, want: 3, ok: true},
		{name: "int64", value: int64(4), want: 4, ok: true},
		{name: "integral_float", value: 5.0, want: 5, ok: true},
		{name: "negative_integral_float", value: -6.0, want: -6, ok: true},
		{name: "fractional_float", value: 2.5, ok: false},
		{name: "string", value: "7", ok: false},
		{name: "absent", value: nil, ok: false},
		{name: "bool", value: true, ok: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := extraInt(tc.value)
			if ok != tc.ok || got != tc.want {
				t.Errorf("extraInt(%#v) = (%d, %v), want (%d, %v)", tc.value, got, ok, tc.want, tc.ok)
			}
		})
	}
}

// TestCovQBTruthy pins Python's `bool(value)` over every type a decoded `extra`
// can hold, plus the object arm: anything else is an object, and `bool(obj)` is
// True.
func TestCovQBTruthy(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
		want  bool
	}{
		{name: "none", value: nil, want: false},
		{name: "true", value: true, want: true},
		{name: "false", value: false, want: false},
		{name: "empty_string", value: "", want: false},
		{name: "string", value: "x", want: true},
		{name: "zero_float", value: 0.0, want: false},
		{name: "float", value: 2.5, want: true},
		{name: "zero_int", value: 0, want: false},
		{name: "int", value: 1, want: true},
		{name: "zero_int64", value: int64(0), want: false},
		{name: "negative_int64", value: int64(-1), want: true},
		{name: "empty_list", value: []any{}, want: false},
		{name: "list", value: []any{nil}, want: true},
		{name: "empty_object", value: map[string]any{}, want: false},
		{name: "object", value: map[string]any{"k": nil}, want: true},
		{name: "other_value_is_an_object", value: struct{}{}, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := truthy(tc.value); got != tc.want {
				t.Errorf("truthy(%#v) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

// TestCovQBHasUnresolvedTargets pins how a recorded relationship is told from a
// guess. Either marker disqualifies the edge, and the marker's VALUE is never
// consulted — an empty candidate list still means "unresolved".
func TestCovQBHasUnresolvedTargets(t *testing.T) {
	for _, tc := range []struct {
		name  string
		extra map[string]any
		want  bool
	}{
		{name: "no_extra", extra: nil, want: false},
		{name: "empty_extra", extra: map[string]any{}, want: false},
		{name: "ambiguous", extra: map[string]any{"ambiguous_targets": []any{}}, want: true},
		{name: "unresolved", extra: map[string]any{"unresolved_targets": nil}, want: true},
		{name: "unrelated_key", extra: map[string]any{"content_hash": "abc"}, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasUnresolvedTargets(tc.extra); got != tc.want {
				t.Errorf("hasUnresolvedTargets(%#v) = %v, want %v", tc.extra, got, tc.want)
			}
		})
	}
}

func TestCovQBUniqueStrings(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values []string
		want   []string
	}{
		{name: "empty", values: nil, want: []string{}},
		{name: "already_unique", values: []string{"a", "b"}, want: []string{"a", "b"}},
		{name: "keeps_first_seen_order", values: []string{"b", "a", "b", "c", "a"}, want: []string{"b", "a", "c"}},
		{name: "empty_string_is_a_value", values: []string{"", ""}, want: []string{""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := uniqueStrings(tc.values)
			if !slices.Equal(got, tc.want) {
				t.Errorf("uniqueStrings(%#v) = %#v, want %#v", tc.values, got, tc.want)
			}
		})
	}
}

// TestCovQBIsJSFamily pins the one language family whose bare edge targets may
// be shared. Case folding matters because `language` is whatever the parser
// recorded, and widening the family would re-open #708: a common method name
// matching an unrelated same-named method in another language.
func TestCovQBIsJSFamily(t *testing.T) {
	for _, tc := range []struct {
		language string
		want     bool
	}{
		{language: "javascript", want: true},
		{language: "TypeScript", want: true},
		{language: "tsx", want: true},
		{language: "jsx", want: false},
		{language: "go", want: false},
		{language: "java", want: false},
		{language: "", want: false},
	} {
		t.Run("lang_"+tc.language, func(t *testing.T) {
			if got := isJSFamily(tc.language); got != tc.want {
				t.Errorf("isJSFamily(%q) = %v, want %v", tc.language, got, tc.want)
			}
		})
	}
}

// TestCovQBSQLLikeIsAnchored pins why the release wraps a caller's
// `file_path_pattern` in `%...%`: sqlLike itself is anchored at BOTH ends, so
// an unwrapped substring must not match. A matcher that silently treated every
// pattern as a substring would make `file_path_pattern: "main.go"` match every
// path containing it even where the release reports nothing.
func TestCovQBSQLLikeIsAnchored(t *testing.T) {
	for _, tc := range []struct {
		value   string
		pattern string
		want    bool
	}{
		{value: "/r/pkg/auth/auth.go", pattern: "pkg/auth", want: false},
		{value: "abc", pattern: "x", want: false},
		{value: "abc", pattern: "ab", want: false},
		{value: "abc", pattern: "abc", want: true},
		{value: "abc", pattern: "abcd", want: false},
		{value: "", pattern: "abc", want: false},
		{value: "", pattern: "", want: true},
		{value: "", pattern: "%", want: true},
		{value: "abc", pattern: "a_c", want: true},
		{value: "ac", pattern: "a_c", want: false},
	} {
		t.Run(tc.pattern+"_vs_"+tc.value, func(t *testing.T) {
			if got := sqlLike(tc.value, tc.pattern); got != tc.want {
				t.Errorf("sqlLike(%q, %q) = %v, want %v", tc.value, tc.pattern, got, tc.want)
			}
		})
	}
}

// TestCovQBSplitSpringIndex pins the `[N]` peel. A malformed index must stay
// part of the NAME rather than being silently dropped, because dropping it
// would fold two distinct keys onto one.
func TestCovQBSplitSpringIndex(t *testing.T) {
	for _, tc := range []struct{ segment, base, index string }{
		{segment: "host", base: "host", index: ""},
		{segment: "servers[2]", base: "servers", index: "[2]"},
		{segment: "servers[12]", base: "servers", index: "[12]"},
		{segment: "[0]", base: "", index: "[0]"},
		// Malformed: an empty index, a non-numeric one, and a missing bracket.
		{segment: "servers[]", base: "servers[]", index: ""},
		{segment: "servers[x]", base: "servers[x]", index: ""},
		{segment: "servers[1a]", base: "servers[1a]", index: ""},
		{segment: "servers2]", base: "servers2]", index: ""},
		{segment: "]", base: "]", index: ""},
		{segment: "servers[2", base: "servers[2", index: ""},
		{segment: "", base: "", index: ""},
	} {
		t.Run("seg_"+tc.segment, func(t *testing.T) {
			base, index := splitSpringIndex(tc.segment)
			if base != tc.base || index != tc.index {
				t.Errorf("splitSpringIndex(%q) = (%q, %q), want (%q, %q)",
					tc.segment, base, index, tc.base, tc.index)
			}
		})
	}
}

// TestCovQBNormalizeSpringConfigKeyPreservesIndexOnlySegments covers the fold's
// remaining arm: a segment that is NOTHING but a list index carries no name to
// re-case, so it passes through verbatim instead of collapsing to "".
func TestCovQBNormalizeSpringConfigKeyPreservesIndexOnlySegments(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{in: "my-app.[0].data-source", want: "myApp.[0].dataSource"},
		{in: "[3]", want: "[3]"},
		// A separator-only segment has no tokens either, so it folds to empty
		// and the dot structure survives — the key still names one property.
		{in: "my-app._.host", want: "myApp..host"},
		{in: "servers[10].host-name", want: "servers[10].hostName"},
	} {
		t.Run(tc.in, func(t *testing.T) {
			if got := normalizeSpringConfigKey(tc.in); got != tc.want {
				t.Errorf("normalizeSpringConfigKey(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestCovQBDetectQueryKindBoost pins the intent read out of a query's SHAPE.
// Each arm changes which nodes win a search, so a lost arm is a silently
// different ranking rather than an error.
func TestCovQBDetectQueryKindBoost(t *testing.T) {
	for _, tc := range []struct {
		query       string
		kinds       map[string]float64
		qualified   float64
		identifiers []string
	}{
		{query: "", kinds: map[string]float64{}},
		{query: "   ", kinds: map[string]float64{}},
		{query: "Session", kinds: map[string]float64{"Class": 1.5, "Type": 1.5}},
		// An all-caps query is an acronym, not a type name.
		{query: "SESSION", kinds: map[string]float64{}},
		{
			query:       "get_token",
			kinds:       map[string]float64{"Function": 1.5},
			identifiers: []string{"get_token"},
		},
		// Underscores alone are not a snake_case identifier.
		{query: "___", kinds: map[string]float64{}},
		{
			query:       "auth.Login",
			kinds:       map[string]float64{},
			qualified:   2.0,
			identifiers: []string{"auth.login"},
		},
		{
			query:       "Who calls Session.expire via get_token",
			kinds:       map[string]float64{"Class": 1.5, "Type": 1.5, "Function": 1.5},
			qualified:   2.0,
			identifiers: []string{"session.expire", "get_token"},
		},
	} {
		t.Run("q_"+tc.query, func(t *testing.T) {
			got := detectQueryKindBoost(tc.query)
			if fmt.Sprint(got.kinds) != fmt.Sprint(tc.kinds) {
				t.Errorf("kinds = %v, want %v", got.kinds, tc.kinds)
			}
			if got.qualified != tc.qualified {
				t.Errorf("qualified = %v, want %v", got.qualified, tc.qualified)
			}
			if !slices.Equal(got.identifiers, tc.identifiers) {
				t.Errorf("identifiers = %v, want %v", got.identifiers, tc.identifiers)
			}
		})
	}
}

// ── hybrid search ────────────────────────────────────────────────────────────

// covQBSearchStore is a three-node graph whose FTS rank order is 1, 2, 3. Every
// ranking test below re-ranks exactly this list, so the asserted order is only
// reachable by applying the boosts — the index's own order is 1, 2, 3.
func covQBSearchStore() *covQBStore {
	store := covQBNewStore()
	store.allNodes = []graphstore.GraphNode{
		covQBNode(1, "Function", "handleSession", "cmd/main.go"),
		covQBNode(2, "Class", "Session", "pkg/auth/auth.go"),
		covQBNode(3, "Function", "sessionKey", "pkg/auth/token.go"),
	}
	store.searchIDs = []int64{1, 2, 3}
	return store
}

// TestCovQBHybridSearchRanksByBoostNotByIndexOrder is the ordering oracle: a
// fusion that forwarded the index's ranks, or dropped the kind multiplier,
// returns handleSession first instead of Session.
func TestCovQBHybridSearchRanksByBoostNotByIndexOrder(t *testing.T) {
	store := covQBSearchStore()

	hits, mode, err := HybridSearch(store, "Session", "", 20, nil)
	if err != nil {
		t.Fatalf("HybridSearch: %v", err)
	}
	if mode != searchModeFTS {
		t.Fatalf("search_mode = %q, want %q", mode, searchModeFTS)
	}
	want := []string{"Session", "handleSession", "sessionKey"}
	if got := covQBHitNames(hits); !slices.Equal(got, want) {
		t.Fatalf("ranked order = %v, want %v", got, want)
	}
	// 1/(k + rank + 1) for rank 1, times the Class multiplier.
	covQBAssertClose(t, "Session score", hits[0].Score, 1.5/62.0)
	covQBAssertClose(t, "handleSession score", hits[1].Score, 1.0/61.0)
}

// TestCovQBHybridSearchContextFilesBoost pins the caller-supplied context
// multiplier. It is the only boost that can lift a node the index ranked LAST
// above one it ranked second, which is exactly what a reviewer editing that
// file wants.
func TestCovQBHybridSearchContextFilesBoost(t *testing.T) {
	store := covQBSearchStore()

	hits, _, err := HybridSearch(store, "Session", "", 20, []string{`pkg\auth\token.go`})
	if err != nil {
		t.Fatalf("HybridSearch: %v", err)
	}
	want := []string{"Session", "sessionKey", "handleSession"}
	if got := covQBHitNames(hits); !slices.Equal(got, want) {
		t.Fatalf("ranked order = %v, want %v", got, want)
	}
	// The context path was given in Windows form; it is normalized before the
	// membership test, so the boost still lands.
	covQBAssertClose(t, "sessionKey score", hits[1].Score, 1.5/63.0)
}

// covQBQualifiedStore is two same-named Java-style symbols. Only the first
// shares the query's dotted prefix, and the index ranks it SECOND.
func covQBQualifiedStore() *covQBStore {
	store := covQBNewStore()
	matching := covQBNode(1, "Function", "Session", "src/Session.java")
	matching.QualifiedName = "com.example.Session"
	other := covQBNode(2, "Function", "Session", "src/other/Session.java")
	other.QualifiedName = "com.other.Session"
	store.allNodes = []graphstore.GraphNode{matching, other}
	store.searchIDs = []int64{2, 1}
	return store
}

// TestCovQBHybridSearchQualifiedAndIdentifierBoostsCompound pins both dotted
// multipliers at once, by their PRODUCT. A dotted query that contains the
// node's qualified name earns the qualified boost (2.0) and the dotted
// identifier boost (2.0); losing either one changes the score and, here, the
// order.
func TestCovQBHybridSearchQualifiedAndIdentifierBoostsCompound(t *testing.T) {
	hits, _, err := HybridSearch(covQBQualifiedStore(), "example.Session", "", 20, nil)
	if err != nil {
		t.Fatalf("HybridSearch: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("got %d hit(s), want 2", len(hits))
	}
	if hits[0].Node.QualifiedName != "com.example.Session" {
		t.Fatalf("ranked first = %q, want com.example.Session", hits[0].Node.QualifiedName)
	}
	covQBAssertClose(t, "boosted score", hits[0].Score, 4.0/62.0)
	covQBAssertClose(t, "unboosted score", hits[1].Score, 1.0/61.0)
}

// TestCovQBHybridSearchIdentifierBoostWithoutQualifiedBoost isolates the
// identifier scan: a natural-language question is not a prefix of any qualified
// name, so the qualified boost cannot fire, yet the dotted token inside the
// sentence still finds Context.Next rather than the bare Context.
func TestCovQBHybridSearchIdentifierBoostWithoutQualifiedBoost(t *testing.T) {
	hits, _, err := HybridSearch(
		covQBQualifiedStore(), "Who advances the chain via example.Session", "", 20, nil)
	if err != nil {
		t.Fatalf("HybridSearch: %v", err)
	}
	if hits[0].Node.QualifiedName != "com.example.Session" {
		t.Fatalf("ranked first = %q, want com.example.Session", hits[0].Node.QualifiedName)
	}
	covQBAssertClose(t, "identifier-boosted score", hits[0].Score, 2.0/62.0)
}

// TestCovQBHybridSearchKindFilterDoesNotConsumeTheLimit pins where the filter
// runs: INSIDE the output loop, after the limit check. `limit` therefore counts
// only kind-matching results, so a filtered search still fills its page instead
// of returning whatever survived the first `limit` boosted rows.
func TestCovQBHybridSearchKindFilterDoesNotConsumeTheLimit(t *testing.T) {
	store := covQBSearchStore()

	hits, _, err := HybridSearch(store, "session", "Class", 1, nil)
	if err != nil {
		t.Fatalf("HybridSearch: %v", err)
	}
	want := []string{"Session"}
	if got := covQBHitNames(hits); !slices.Equal(got, want) {
		t.Fatalf("hits = %v, want %v", got, want)
	}
}

// TestCovQBHybridSearchDropsIDsWithNoNodeRow pins the join: an id the index
// still carries for a deleted row contributes nothing rather than an empty hit.
func TestCovQBHybridSearchDropsIDsWithNoNodeRow(t *testing.T) {
	store := covQBSearchStore()
	store.searchIDs = []int64{99, 2}

	hits, mode, err := HybridSearch(store, "session", "", 20, nil)
	if err != nil {
		t.Fatalf("HybridSearch: %v", err)
	}
	if mode != searchModeFTS {
		t.Errorf("search_mode = %q, want %q", mode, searchModeFTS)
	}
	want := []string{"Session"}
	if got := covQBHitNames(hits); !slices.Equal(got, want) {
		t.Fatalf("hits = %v, want %v", got, want)
	}
}

// TestCovQBHybridSearchFallsBackWhenFTSIsUnsupported pins the capability
// degradation: a backend with no FTS5 module reports ErrFTSUnsupported, and the
// search must still answer from the keyword scan rather than failing.
func TestCovQBHybridSearchFallsBackWhenFTSIsUnsupported(t *testing.T) {
	store := covQBSearchStore()
	store.searchIDs = nil
	store.searchErr = graphstore.ErrFTSUnsupported

	hits, mode, err := HybridSearch(store, "Session", "", 20, nil)
	if err != nil {
		t.Fatalf("HybridSearch: %v", err)
	}
	if mode != searchModeKeyword {
		t.Fatalf("search_mode = %q, want %q", mode, searchModeKeyword)
	}
	// The fallback's own scores replace the RRF ranks, so the order is
	// keyword band times kind boost: exact+Class 4.5, prefix 2.0, contains
	// 1.0. Note it differs from the FTS path's order for the same query,
	// which is what makes the mode worth reporting.
	want := []string{"Session", "sessionKey", "handleSession"}
	if got := covQBHitNames(hits); !slices.Equal(got, want) {
		t.Fatalf("hits = %v, want %v", got, want)
	}
}

// TestCovQBHybridSearchNilStoreIsAnEmptyGraph pins the never-built graph: the
// release answers mode "none" with no results rather than an error.
func TestCovQBHybridSearchNilStoreIsAnEmptyGraph(t *testing.T) {
	hits, mode, err := HybridSearch(nil, "session", "", 20, nil)
	if err != nil || mode != searchModeNone || len(hits) != 0 {
		t.Fatalf("HybridSearch(nil) = (%d hits, %q, %v), want (0, %q, nil)",
			len(hits), mode, err, searchModeNone)
	}
}

// TestCovQBHybridSearchSurfacesStoreFailures pins that a failed read is
// reported, not degraded to "no matches" — the one answer a caller cannot tell
// apart from a real absence.
func TestCovQBHybridSearchSurfacesStoreFailures(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*covQBStore)
	}{
		{
			name:  "index_read_fails",
			setup: func(s *covQBStore) { s.searchErr = errFake },
		},
		{
			name: "keyword_fallback_read_fails",
			setup: func(s *covQBStore) {
				s.searchIDs = nil
				s.allNodesErr = errFake
			},
		},
		{
			name:  "node_hydration_fails",
			setup: func(s *covQBStore) { s.allNodesErr = errFake },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := covQBSearchStore()
			tc.setup(store)
			hits, mode, err := HybridSearch(store, "session", "", 20, nil)
			covQBWantFake(t, "HybridSearch", hits, err)
			if mode != "" {
				t.Errorf("search_mode = %q, want \"\" on failure", mode)
			}
		})
	}
}

// ── semantic_search_nodes_tool ───────────────────────────────────────────────

// TestCovQBSemanticSearchSurfacesStoreFailures pins that neither an unopenable
// database nor a failing index read is answered as "found 0 nodes".
func TestCovQBSemanticSearchSurfacesStoreFailures(t *testing.T) {
	t.Run("database_cannot_be_opened", func(t *testing.T) {
		result, err := semanticSearchNodesTool(
			unreadableEngine(t), covQBBind(t, "semantic_search_nodes_tool", `{"query":"token"}`))
		if err == nil {
			t.Fatalf("want an error for an unopenable graph, got %#v", result)
		}
	})
	t.Run("index_read_fails", func(t *testing.T) {
		store := covQBSearchStore()
		store.searchErr = errFake
		engine := engineWithStore(t, t.TempDir(), store)
		result, err := semanticSearchNodesTool(
			engine, covQBBind(t, "semantic_search_nodes_tool", `{"query":"token"}`))
		covQBWantFake(t, "semantic_search_nodes_tool", result, err)
	})
}

// covQBManyHitsStore is a graph with more hits than minimal mode shows, so the
// projection's cap and its omitted count are both observable.
func covQBManyHitsStore(count int) *covQBStore {
	store := covQBNewStore()
	for i := 1; i <= count; i++ {
		store.allNodes = append(store.allNodes,
			covQBNode(int64(i), "Function", fmt.Sprintf("sessionPart%d", i), "pkg/auth/auth.go"))
		store.searchIDs = append(store.searchIDs, int64(i))
	}
	return store
}

// TestCovQBSemanticSearchMinimalCapsResultsAndReportsOmitted pins the compact
// projection: it shows at most queryMinimalLimit results, still reports the
// FULL count, and says how many it withheld. A cap that also shrank
// `result_count` would tell an agent the graph has fewer matches than it does.
func TestCovQBSemanticSearchMinimalCapsResultsAndReportsOmitted(t *testing.T) {
	engine := engineWithStore(t, t.TempDir(), covQBManyHitsStore(queryMinimalLimit+2))
	payload := covQBPayload(t, engine, "semantic_search_nodes_tool",
		`{"query":"session","detail_level":"minimal"}`, semanticSearchNodesTool)

	if payload["result_count"] != queryMinimalLimit+2 {
		t.Errorf("result_count = %v, want %d", payload["result_count"], queryMinimalLimit+2)
	}
	if payload["results_omitted"] != 2 {
		t.Errorf("results_omitted = %v, want 2", payload["results_omitted"])
	}
	results, ok := payload["results"].([]map[string]any)
	if !ok {
		t.Fatalf("results is %T, want a list of objects", payload["results"])
	}
	if len(results) != queryMinimalLimit {
		t.Fatalf("got %d visible result(s), want %d", len(results), queryMinimalLimit)
	}
	wantKeys := []string{"file_path", "kind", "name", "score"}
	if got := slices.Sorted(maps.Keys(results[0])); !slices.Equal(got, wantKeys) {
		t.Errorf("minimal projection keys = %v, want %v", got, wantKeys)
	}
	// The compact branch returns before generate_hints, so it deliberately
	// carries no `_hints` block.
	if _, present := payload["_hints"]; present {
		t.Error("minimal mode must not attach _hints")
	}
}

// TestCovQBSemanticSearchMinimalKeepsTheEmptyResultMarker pins the marker on
// the compact path too. Zero hits can mean "no such symbol" or "never
// indexed"; without the marker an agent reads an unbuilt graph as a confirmed
// absence and stops looking.
func TestCovQBSemanticSearchMinimalKeepsTheEmptyResultMarker(t *testing.T) {
	engine := engineWithStore(t, t.TempDir(), covQBNewStore())
	payload := covQBPayload(t, engine, "semantic_search_nodes_tool",
		`{"query":"zzqqxx","detail_level":"minimal"}`, semanticSearchNodesTool)

	note, _ := payload["confidence"].(string)
	if !strings.Contains(note, "graph is empty") {
		t.Errorf("confidence = %q, want the empty-graph marker", note)
	}
	if payload["search_mode"] != searchModeNone {
		t.Errorf("search_mode = %v, want %q", payload["search_mode"], searchModeNone)
	}
	if payload["result_count"] != 0 || payload["results_omitted"] != 0 {
		t.Errorf("counts = (%v, %v), want (0, 0)",
			payload["result_count"], payload["results_omitted"])
	}
}

// TestCovQBEmptySearchConfidence pins which of the three "why is this 0"
// stories a zero-result search tells. They are not interchangeable: an empty
// graph, an unreadable aggregate and a confirmed absence call for different
// next steps.
func TestCovQBEmptySearchConfidence(t *testing.T) {
	for _, tc := range []struct {
		name  string
		store graphstore.Store
		want  string
	}{
		{name: "never_built", store: nil, want: "graph is empty"},
		{
			name:  "stats_unreadable_says_nothing",
			store: &fakeStore{statsErr: errFake},
			want:  "",
		},
		{
			name:  "built_but_empty",
			store: &fakeStore{stats: graphstore.GraphStats{}},
			want:  "graph is empty",
		},
		{
			name:  "confirmed_absence_names_what_search_covers",
			store: &fakeStore{stats: graphstore.GraphStats{TotalNodes: 3}},
			want:  "no indexed node matches 'zzqqxx'",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := emptySearchConfidence(tc.store, t.TempDir(), "zzqqxx")
			if tc.want == "" {
				if got != "" {
					t.Fatalf("confidence = %q, want no marker", got)
				}
				return
			}
			if !strings.Contains(got, tc.want) {
				t.Errorf("confidence = %q, want it to contain %q", got, tc.want)
			}
		})
	}
}

// TestCovQBEmptySearchConfidenceReportsAStaleGraph pins the fourth story, which
// needs a real repository: the graph was built at a commit that is no longer
// HEAD, so the 0 may simply be out of date. Reporting the confirmed-absence
// wording here would be an outright false negative.
func TestCovQBEmptySearchConfidenceReportsAStaleGraph(t *testing.T) {
	fixture := newQueryFixture(t)
	store, err := fixture.engine.readStore()
	if err != nil {
		t.Fatalf("readStore: %v", err)
	}
	if err := store.SetMetadata("git_head_sha", strings.Repeat("0", 40)); err != nil {
		t.Fatalf("SetMetadata: %v", err)
	}
	if err := store.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	got := emptySearchConfidence(store, fixture.root, "zzqqxx")
	if !strings.Contains(got, "graph is stale") {
		t.Errorf("confidence = %q, want the stale-graph marker", got)
	}
}

// ── find_large_functions_tool ────────────────────────────────────────────────

// TestCovQBFindLargeFunctionsSurfacesStoreFailures pins that a decomposition
// audit fails loudly rather than reporting a clean bill of health.
func TestCovQBFindLargeFunctionsSurfacesStoreFailures(t *testing.T) {
	t.Run("database_cannot_be_opened", func(t *testing.T) {
		result, err := findLargeFunctionsTool(
			unreadableEngine(t), covQBBind(t, "find_large_functions_tool", `{}`))
		if err == nil {
			t.Fatalf("want an error for an unopenable graph, got %#v", result)
		}
	})
	t.Run("node_scan_fails", func(t *testing.T) {
		store := covQBNewStore()
		store.allNodesErr = errFake
		result, err := findLargeFunctionsTool(
			engineWithStore(t, t.TempDir(), store), covQBBind(t, "find_large_functions_tool", `{}`))
		covQBWantFake(t, "find_large_functions_tool", result, err)
	})
}

// TestCovQBFindLargeFunctionsProjectsLineCountAndRelativePath pins the two
// fields this tool adds on top of node_to_dict. The relative path is what makes
// the summary readable, and a file outside the root keeps its absolute path
// rather than being reported under a root it does not live in.
func TestCovQBFindLargeFunctionsProjectsLineCountAndRelativePath(t *testing.T) {
	store := covQBNewStore()
	store.allNodes = []graphstore.GraphNode{
		covQBSpanned(1, "Function", "inside", "/repo/pkg/auth/auth.go", 10, 40),
		covQBSpanned(2, "Function", "outside", "/vendor/lib/lib.go", 1, 20),
	}
	payload := covQBPayload(t, engineWithStore(t, "/repo", store), "find_large_functions_tool",
		`{"min_lines":5}`, findLargeFunctionsTool)

	if payload["total_found"] != 2 || payload["min_lines"] != 5 {
		t.Fatalf("total_found/min_lines = (%v, %v), want (2, 5)",
			payload["total_found"], payload["min_lines"])
	}
	results, ok := payload["results"].([]map[string]any)
	if !ok {
		t.Fatalf("results is %T, want a list of objects", payload["results"])
	}
	// Descending line count: `inside` spans 31 lines, `outside` 20.
	if results[0]["line_count"] != 31 || results[0]["relative_path"] != "pkg/auth/auth.go" {
		t.Errorf("inside projection = (%v, %v), want (31, pkg/auth/auth.go)",
			results[0]["line_count"], results[0]["relative_path"])
	}
	if results[1]["line_count"] != 20 || results[1]["relative_path"] != "/vendor/lib/lib.go" {
		t.Errorf("outside projection = (%v, %v), want (20, /vendor/lib/lib.go)",
			results[1]["line_count"], results[1]["relative_path"])
	}
	if !strings.Contains(payload["summary"].(string), "auth.go:10") {
		t.Errorf("summary = %q, want it to cite the relative path and start line",
			payload["summary"])
	}
}

// covQBSizeStore is four equally sized nodes in ascending id order plus one
// larger node, a signal, and a node with no line span. It exists to make the
// tie-break, the exclusions and the limit cut all observable at once.
func covQBSizeStore() *covQBStore {
	store := covQBNewStore()
	signal := covQBSpanned(5, "Variable", "clk", "/repo/rtl/top.v", 3, 9)
	signal.Extra = map[string]any{"verilog_kind": "wire"}
	store.allNodes = []graphstore.GraphNode{
		covQBSpanned(1, "Function", "aaa", "/repo/pkg/auth/a.go", 1, 10),
		covQBSpanned(2, "Function", "bbb", "/repo/pkg/auth/b.go", 1, 10),
		covQBSpanned(3, "Class", "Ccc", "/repo/pkg/auth/c.go", 1, 10),
		covQBSpanned(4, "Function", "ddd", "/repo/cmd/d.go", 1, 20),
		signal,
		covQBNode(6, "Function", "unspanned", "/repo/pkg/auth/e.go"),
	}
	return store
}

func covQBNodeNames(nodes []graphstore.GraphNode) []string {
	out := make([]string, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, node.Name)
	}
	return out
}

// TestCovQBReleaseNodesBySize pins the size reader: a STABLE descending sort,
// so a limit cutting through a run of equally sized nodes keeps ascending id
// order; the three exclusions; and SQLite's LIMIT semantics, which the
// release's schema forwards verbatim.
func TestCovQBReleaseNodesBySize(t *testing.T) {
	for _, tc := range []struct {
		name     string
		minLines int
		kind     string
		pattern  string
		limit    int
		want     []string
	}{
		{name: "stable_tie_break_under_limit", minLines: 1, limit: 3, want: []string{"ddd", "aaa", "bbb"}},
		{name: "threshold_excludes_smaller", minLines: 11, limit: 50, want: []string{"ddd"}},
		{name: "threshold_above_everything", minLines: 100, limit: 50, want: []string{}},
		{
			name: "kind_and_pattern", minLines: 1, kind: "Function", pattern: "pkg/auth",
			limit: 50, want: []string{"aaa", "bbb"},
		},
		{name: "pattern_alone", minLines: 1, pattern: "cmd/", limit: 50, want: []string{"ddd"}},
		// A wildcard inside the caller's pattern is a real wildcard: the
		// release wraps it in `%...%` and hands it to SQL.
		{name: "pattern_wildcard", minLines: 1, pattern: "pkg/%/c.go", limit: 50, want: []string{"Ccc"}},
		{name: "zero_limit_returns_nothing", minLines: 1, limit: 0, want: []string{}},
		{
			name: "negative_limit_is_unbounded", minLines: 1, limit: -1,
			want: []string{"ddd", "aaa", "bbb", "Ccc"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nodes, err := releaseNodesBySize(covQBSizeStore(), tc.minLines, tc.kind, tc.pattern, tc.limit)
			if err != nil {
				t.Fatalf("releaseNodesBySize: %v", err)
			}
			if got := covQBNodeNames(nodes); !slices.Equal(got, tc.want) {
				t.Errorf("nodes = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestCovQBReleaseNodesBySizeDegradesAndFails pins the two non-result answers:
// an unbuilt graph is empty, a failing scan is an error.
func TestCovQBReleaseNodesBySizeDegradesAndFails(t *testing.T) {
	t.Run("nil_store_is_an_empty_graph", func(t *testing.T) {
		nodes, err := releaseNodesBySize(nil, 1, "", "", 50)
		if err != nil || len(nodes) != 0 {
			t.Fatalf("releaseNodesBySize(nil) = (%d nodes, %v), want (0, nil)", len(nodes), err)
		}
	})
	t.Run("scan_failure_is_reported", func(t *testing.T) {
		store := covQBNewStore()
		store.allNodesErr = errFake
		nodes, err := releaseNodesBySize(store, 1, "", "", 50)
		covQBWantFake(t, "releaseNodesBySize", nodes, err)
	})
}

// ── list_graph_stats_tool ────────────────────────────────────────────────────

// TestCovQBListGraphStatsSurfacesStoreFailures pins that a health check cannot
// report a healthy zero when the store refused to answer.
func TestCovQBListGraphStatsSurfacesStoreFailures(t *testing.T) {
	args := covQBBind(t, "list_graph_stats_tool", `{}`)
	t.Run("database_cannot_be_opened", func(t *testing.T) {
		result, err := listGraphStatsTool(unreadableEngine(t), args)
		if err == nil {
			t.Fatalf("want an error for an unopenable graph, got %#v", result)
		}
	})
	t.Run("aggregate_read_fails", func(t *testing.T) {
		store := covQBNewStore()
		store.statsErr = errFake
		result, err := listGraphStatsTool(engineWithStore(t, t.TempDir(), store), args)
		covQBWantFake(t, "list_graph_stats_tool", result, err)
	})
	t.Run("node_derivation_fails", func(t *testing.T) {
		store := covQBNewStore()
		store.allNodesErr = errFake
		result, err := listGraphStatsTool(engineWithStore(t, t.TempDir(), store), args)
		covQBWantFake(t, "list_graph_stats_tool", result, err)
	})
}

// TestCovQBListGraphStatsOnANeverBuiltGraph pins the graceful degradation an
// unbuilt repository relies on: every count is zero and `last_updated` is null,
// rather than an error telling the caller to build a graph it was asking about.
func TestCovQBListGraphStatsOnANeverBuiltGraph(t *testing.T) {
	payload := covQBPayload(t, Open(t.TempDir()), "list_graph_stats_tool", `{}`, listGraphStatsTool)

	if payload["total_nodes"] != 0 || payload["total_edges"] != 0 || payload["files_count"] != 0 {
		t.Errorf("counts = (%v, %v, %v), want all zero",
			payload["total_nodes"], payload["total_edges"], payload["files_count"])
	}
	if payload["last_updated"] != nil {
		t.Errorf("last_updated = %v, want null", payload["last_updated"])
	}
	summary, _ := payload["summary"].(string)
	for _, want := range []string{"Languages: none", "Last updated: never", "Embeddings: 0 nodes embedded"} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary = %q, want it to contain %q", summary, want)
		}
	}
}

// TestCovQBListGraphStatsDerivesSignalsAndLiveLanguages pins the two
// node-shaped statistics the store's own aggregate cannot express: a Verilog
// signal is counted under kind "Signal", and `languages` is the LIVE FILE
// inventory — a synthetic node with no backing File row must not keep a
// language alive (#474).
func TestCovQBListGraphStatsDerivesSignalsAndLiveLanguages(t *testing.T) {
	store := covQBNewStore()
	store.stats = graphstore.GraphStats{
		TotalNodes: 4, TotalEdges: 1, FilesCount: 2,
		EdgesByKind: map[string]int{"CALLS": 1},
	}
	signal := covQBSpanned(3, "Variable", "clk", "/repo/rtl/top.v", 3, 9)
	signal.Extra = map[string]any{"verilog_kind": "wire"}
	goFile := covQBNode(1, "File", "/repo/pkg/a.go", "/repo/pkg/a.go")
	verilogFile := covQBNode(2, "File", "/repo/rtl/top.v", "/repo/rtl/top.v")
	verilogFile.Language = "verilog"
	orphan := covQBNode(4, "Event", "OrderPlaced", "")
	orphan.Language = "java"
	store.allNodes = []graphstore.GraphNode{goFile, verilogFile, signal, orphan}

	payload := covQBPayload(t, engineWithStore(t, "/repo", store),
		"list_graph_stats_tool", `{}`, listGraphStatsTool)

	byKind, _ := payload["nodes_by_kind"].(map[string]int)
	want := map[string]int{"File": 2, "Signal": 1, "Event": 1}
	if fmt.Sprint(byKind) != fmt.Sprint(want) {
		t.Errorf("nodes_by_kind = %v, want %v", byKind, want)
	}
	languages, _ := payload["languages"].([]string)
	if !slices.Equal(languages, []string{"go", "verilog"}) {
		t.Errorf("languages = %v, want [go verilog]", languages)
	}
	if payload["embeddings_count"] != 0 {
		t.Errorf("embeddings_count = %v, want 0", payload["embeddings_count"])
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "  Signal: 1") || !strings.Contains(summary, "  CALLS: 1") {
		t.Errorf("summary = %q, want the derived kind lines", summary)
	}
}

// ── release store readers ────────────────────────────────────────────────────

// covQBWordStore is the graph search_nodes' LIKE fallback runs over: three
// nodes whose NAMES do not all contain the query words, so the qualified-name
// column has to be searched too.
func covQBWordStore() *covQBStore {
	store := covQBNewStore()
	store.allNodes = []graphstore.GraphNode{
		covQBNode(1, "Function", "handleLogin", "cmd/main.go"),
		covQBNode(2, "Function", "loginRetry", "cmd/main.go"),
		covQBNode(3, "Function", "Login", "pkg/auth/auth.go"),
		covQBNode(4, "Function", "logout", "cmd/main.go"),
	}
	return store
}

// TestCovQBReleaseSearchNodes pins search_nodes: FTS5 first, then a LIKE
// fallback that requires EVERY word, in either the name or the qualified name,
// case-insensitively — and truncated by the limit as it scans, in id order.
func TestCovQBReleaseSearchNodes(t *testing.T) {
	t.Run("fts_hit_is_hydrated_in_index_order", func(t *testing.T) {
		store := covQBWordStore()
		store.wordIDs = []int64{3, 1}
		nodes, err := releaseSearchNodes(store, "login", 20)
		if err != nil {
			t.Fatalf("releaseSearchNodes: %v", err)
		}
		if got := covQBNodeNames(nodes); !slices.Equal(got, []string{"handleLogin", "Login"}) {
			t.Errorf("nodes = %v, want [handleLogin Login]", got)
		}
	})
	t.Run("fallback_requires_every_word_across_both_columns", func(t *testing.T) {
		nodes, err := releaseSearchNodes(covQBWordStore(), "login MAIN", 20)
		if err != nil {
			t.Fatalf("releaseSearchNodes: %v", err)
		}
		// "main" only ever appears in the qualified name; "login" only in the
		// name. A scan over either column alone would find nothing.
		if got := covQBNodeNames(nodes); !slices.Equal(got, []string{"handleLogin", "loginRetry"}) {
			t.Errorf("nodes = %v, want [handleLogin loginRetry]", got)
		}
	})
	t.Run("fallback_truncates_in_id_order", func(t *testing.T) {
		nodes, err := releaseSearchNodes(covQBWordStore(), "login", 2)
		if err != nil {
			t.Fatalf("releaseSearchNodes: %v", err)
		}
		if got := covQBNodeNames(nodes); !slices.Equal(got, []string{"handleLogin", "loginRetry"}) {
			t.Errorf("nodes = %v, want [handleLogin loginRetry]", got)
		}
	})
	t.Run("fallback_finds_nothing", func(t *testing.T) {
		nodes, err := releaseSearchNodes(covQBWordStore(), "zzqqxx", 20)
		if err != nil || len(nodes) != 0 {
			t.Fatalf("releaseSearchNodes = (%d nodes, %v), want (0, nil)", len(nodes), err)
		}
	})
	t.Run("unsupported_fts_falls_back", func(t *testing.T) {
		store := covQBWordStore()
		store.wordErr = graphstore.ErrFTSUnsupported
		nodes, err := releaseSearchNodes(store, "logout", 20)
		if err != nil {
			t.Fatalf("releaseSearchNodes: %v", err)
		}
		if got := covQBNodeNames(nodes); !slices.Equal(got, []string{"logout"}) {
			t.Errorf("nodes = %v, want [logout]", got)
		}
	})
}

// TestCovQBReleaseSearchNodesDegradesAndFails pins the non-result answers: an
// unbuilt graph and a query with no words are empty, a failing read is an
// error.
func TestCovQBReleaseSearchNodesDegradesAndFails(t *testing.T) {
	t.Run("nil_store", func(t *testing.T) {
		nodes, err := releaseSearchNodes(nil, "login", 20)
		if err != nil || nodes != nil {
			t.Fatalf("releaseSearchNodes(nil) = (%v, %v), want (nil, nil)", nodes, err)
		}
	})
	t.Run("blank_query_has_no_words", func(t *testing.T) {
		nodes, err := releaseSearchNodes(covQBWordStore(), "   ", 20)
		if err != nil || nodes != nil {
			t.Fatalf("releaseSearchNodes = (%v, %v), want (nil, nil)", nodes, err)
		}
	})
	t.Run("index_read_fails", func(t *testing.T) {
		store := covQBWordStore()
		store.wordErr = errFake
		nodes, err := releaseSearchNodes(store, "login", 20)
		covQBWantFake(t, "releaseSearchNodes", nodes, err)
	})
	t.Run("fallback_scan_fails", func(t *testing.T) {
		store := covQBWordStore()
		store.allNodesErr = errFake
		nodes, err := releaseSearchNodes(store, "login", 20)
		covQBWantFake(t, "releaseSearchNodes", nodes, err)
	})
}

// TestCovQBReleaseCountSearchNodes pins the UNBOUNDED count under
// search_nodes' semantics. It is what lets a disambiguation response report a
// real total next to a capped list, so a count that inherited the list's limit
// would understate how ambiguous a target is.
func TestCovQBReleaseCountSearchNodes(t *testing.T) {
	t.Run("fts_count_is_used_when_nonzero", func(t *testing.T) {
		store := covQBWordStore()
		store.wordCount = 7
		got, err := releaseCountSearchNodes(store, "login")
		if err != nil || got != 7 {
			t.Fatalf("releaseCountSearchNodes = (%d, %v), want (7, nil)", got, err)
		}
	})
	t.Run("fallback_count_is_unbounded", func(t *testing.T) {
		got, err := releaseCountSearchNodes(covQBWordStore(), "login")
		if err != nil || got != 3 {
			t.Fatalf("releaseCountSearchNodes = (%d, %v), want (3, nil)", got, err)
		}
	})
	t.Run("unsupported_fts_falls_back", func(t *testing.T) {
		store := covQBWordStore()
		store.wordCountErr = graphstore.ErrFTSUnsupported
		got, err := releaseCountSearchNodes(store, "logout")
		if err != nil || got != 1 {
			t.Fatalf("releaseCountSearchNodes = (%d, %v), want (1, nil)", got, err)
		}
	})
	t.Run("nil_store", func(t *testing.T) {
		got, err := releaseCountSearchNodes(nil, "login")
		if err != nil || got != 0 {
			t.Fatalf("releaseCountSearchNodes(nil) = (%d, %v), want (0, nil)", got, err)
		}
	})
	t.Run("blank_query_has_no_words", func(t *testing.T) {
		got, err := releaseCountSearchNodes(covQBWordStore(), "  ")
		if err != nil || got != 0 {
			t.Fatalf("releaseCountSearchNodes = (%d, %v), want (0, nil)", got, err)
		}
	})
	t.Run("index_count_fails", func(t *testing.T) {
		store := covQBWordStore()
		store.wordCountErr = errFake
		got, err := releaseCountSearchNodes(store, "login")
		covQBWantFake(t, "releaseCountSearchNodes", got, err)
	})
	t.Run("fallback_scan_fails", func(t *testing.T) {
		store := covQBWordStore()
		store.allNodesErr = errFake
		got, err := releaseCountSearchNodes(store, "login")
		covQBWantFake(t, "releaseCountSearchNodes", got, err)
	})
}

// covQBKeywordStore holds one node per keyword-score band, in ascending id
// order so the limit's interaction with the scoring is observable.
func covQBKeywordStore() *covQBStore {
	store := covQBNewStore()
	store.allNodes = []graphstore.GraphNode{
		covQBNode(1, "Function", "getSession", "pkg/a.go"),
		covQBNode(2, "Class", "Session", "pkg/b.go"),
		covQBNode(3, "Class", "SessionStore", "pkg/c.go"),
		covQBNode(4, "Function", "mySessionCache", "pkg/d.go"),
	}
	return store
}

func covQBScored(results []scoredNode) string {
	parts := make([]string, 0, len(results))
	for _, result := range results {
		parts = append(parts, fmt.Sprintf("%d:%.1f", result.id, result.score))
	}
	return strings.Join(parts, " ")
}

// TestCovQBReleaseKeywordSearch pins the LIKE fallback's scoring AND the order
// the two operations happen in. The limit truncates the id-ordered scan BEFORE
// anything is scored, so a small limit can exclude a better-scoring node — a
// score-then-truncate implementation returns a different list.
func TestCovQBReleaseKeywordSearch(t *testing.T) {
	for _, tc := range []struct {
		name  string
		query string
		limit int
		want  string
	}{
		{name: "exact_then_prefix_then_contains", query: "session", limit: 20, want: "2:3.0 3:2.0 1:1.0 4:1.0"},
		{name: "case_insensitive", query: "SESSION", limit: 20, want: "2:3.0 3:2.0 1:1.0 4:1.0"},
		{name: "limit_truncates_before_scoring", query: "session", limit: 2, want: "2:3.0 1:1.0"},
		{name: "negative_limit_is_unbounded", query: "session", limit: -1, want: "2:3.0 3:2.0 1:1.0 4:1.0"},
		{name: "zero_limit_returns_nothing", query: "session", limit: 0, want: ""},
		{name: "blank_query_has_no_words", query: "  ", limit: 20, want: ""},
		{name: "no_match", query: "zzqqxx", limit: 20, want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			results, err := releaseKeywordSearch(covQBKeywordStore(), tc.query, tc.limit)
			if err != nil {
				t.Fatalf("releaseKeywordSearch: %v", err)
			}
			if got := covQBScored(results); got != tc.want {
				t.Errorf("scored = %q, want %q", got, tc.want)
			}
		})
	}
	t.Run("scan_failure_is_reported", func(t *testing.T) {
		store := covQBKeywordStore()
		store.allNodesErr = errFake
		results, err := releaseKeywordSearch(store, "session", 20)
		covQBWantFake(t, "releaseKeywordSearch", results, err)
	})
}

// covQBOverloadStore is one name carried by several signature-bearing nodes
// across two languages, which is the C++ overload probe's whole subject.
func covQBOverloadStore() *covQBStore {
	store := covQBNewStore()
	cppFunc := covQBNode(1, "Function", "clone", "src/a.cpp")
	cppFunc.Language = "cpp"
	cppMethod := covQBNode(2, "Method", "clone", "src/b.cpp")
	cppMethod.Language = "cpp"
	javaFunc := covQBNode(3, "Function", "clone", "src/C.java")
	javaFunc.Language = "java"
	store.allNodes = []graphstore.GraphNode{
		cppFunc, cppMethod, javaFunc, covQBNode(4, "Function", "other", "src/d.cpp"),
	}
	return store
}

// TestCovQBReleaseCountNodesByName pins the overload probe: more than one
// same-named signature-bearing node means the bare call targets around it
// cannot be attributed to a single overload. Each filter narrows what counts as
// "the same symbol".
func TestCovQBReleaseCountNodesByName(t *testing.T) {
	for _, tc := range []struct {
		name     string
		symbol   string
		language string
		kinds    []string
		want     int
	}{
		{name: "every_language_and_kind", symbol: "clone", want: 3},
		{name: "language_filter", symbol: "clone", language: "cpp", want: 2},
		{name: "language_and_kind_filter", symbol: "clone", language: "cpp", kinds: []string{"Function"}, want: 1},
		{name: "several_kinds", symbol: "clone", kinds: []string{"Function", "Method"}, want: 3},
		{name: "kind_excludes_everything", symbol: "clone", kinds: []string{"Class"}, want: 0},
		{name: "unknown_name", symbol: "missing", want: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := releaseCountNodesByName(covQBOverloadStore(), tc.symbol, tc.language, tc.kinds...)
			if err != nil {
				t.Fatalf("releaseCountNodesByName: %v", err)
			}
			if got != tc.want {
				t.Errorf("releaseCountNodesByName(%q, %q, %v) = %d, want %d",
					tc.symbol, tc.language, tc.kinds, got, tc.want)
			}
		})
	}
	t.Run("scan_failure_is_reported", func(t *testing.T) {
		store := covQBOverloadStore()
		store.allNodesErr = errFake
		got, err := releaseCountNodesByName(store, "clone", "")
		covQBWantFake(t, "releaseCountNodesByName", got, err)
	})
}

// covQBTargetNameStore holds edges onto one BARE target name from sources in
// four languages, one of which has no node row at all.
func covQBTargetNameStore() *covQBStore {
	store := covQBNewStore()
	store.edgesByTarget = covQBByTarget(
		covQBEdge(1, "CALLS", "app/main.js::handle", "Login"),
		covQBEdge(2, "CALLS", "app/svc.py::run", "Login"),
		covQBEdge(3, "CALLS", "app/ghost.go::gone", "Login"),
		covQBEdge(4, "CONTAINS", "app/main.js::handle", "Login"),
		covQBEdge(5, "CALLS", "app/lib.ts::call", "Login"),
	)
	js := covQBNode(1, "Function", "handle", "app/main.js")
	js.Language = "javascript"
	js.QualifiedName = "app/main.js::handle"
	py := covQBNode(2, "Function", "run", "app/svc.py")
	py.Language = "python"
	py.QualifiedName = "app/svc.py::run"
	ts := covQBNode(3, "Function", "call", "app/lib.ts")
	ts.Language = "typescript"
	ts.QualifiedName = "app/lib.ts::call"
	store.byQualified = map[string]graphstore.GraphNode{
		js.QualifiedName: js, py.QualifiedName: py, ts.QualifiedName: ts,
	}
	return store
}

// TestCovQBReleaseEdgesByTargetName pins the language gate behind #708. With no
// language the whole graph is fair game and an edge whose source has no node
// row survives; with a language the release's INNER JOIN drops it, and only the
// JS family is treated as one interchangeable set.
func TestCovQBReleaseEdgesByTargetName(t *testing.T) {
	for _, tc := range []struct {
		name     string
		target   string
		kind     string
		language string
		want     []int64
	}{
		{name: "no_language_keeps_unbacked_sources", target: "Login", kind: "CALLS", want: []int64{1, 2, 3, 5}},
		{
			name: "js_family_is_interchangeable", target: "Login", kind: "CALLS",
			language: "typescript", want: []int64{1, 5},
		},
		{
			name: "non_js_language_is_exact", target: "Login", kind: "CALLS",
			language: "python", want: []int64{2},
		},
		{
			name: "language_with_no_source_matches", target: "Login", kind: "CALLS",
			language: "go", want: []int64{},
		},
		{name: "kind_filter", target: "Login", kind: "CONTAINS", want: []int64{4}},
		{name: "unknown_target", target: "Missing", kind: "CALLS", want: []int64{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			edges, err := releaseEdgesByTargetName(
				covQBTargetNameStore(), tc.target, tc.kind, tc.language)
			if err != nil {
				t.Fatalf("releaseEdgesByTargetName: %v", err)
			}
			if got := covQBEdgeIDs(edges); !slices.Equal(got, tc.want) {
				t.Errorf("edge ids = %v, want %v", got, tc.want)
			}
		})
	}
	t.Run("edge_read_fails", func(t *testing.T) {
		store := covQBTargetNameStore()
		store.edgesErr = errFake
		edges, err := releaseEdgesByTargetName(store, "Login", "CALLS", "")
		covQBWantFake(t, "releaseEdgesByTargetName", edges, err)
	})
	t.Run("source_node_read_fails", func(t *testing.T) {
		store := covQBTargetNameStore()
		store.getNodeErr = errFake
		edges, err := releaseEdgesByTargetName(store, "Login", "CALLS", "python")
		covQBWantFake(t, "releaseEdgesByTargetName", edges, err)
	})
}

// covQBConfigStore holds config consumers under the exact key, an ancestor
// wildcard, and a wildcard carrying an edge of the wrong kind.
func covQBConfigStore() *covQBStore {
	store := covQBNewStore()
	store.edgesByTarget = covQBByTarget(
		covQBEdge(2, "DEPENDS_ON_CONFIG", "app/Mailer.java::Mailer", "config:app.mail.host"),
		covQBEdge(1, "DEPENDS_ON_CONFIG", "app/Smtp.java::Smtp", "config:app.mail.host"),
		covQBEdge(3, "DEPENDS_ON_CONFIG", "app/MailProps.java::MailProps", "config:app.mail.*"),
		covQBEdge(4, "CALLS", "app/Other.java::Other", "config:app.*"),
	)
	return store
}

// TestCovQBReleaseConfigConsumers pins the property-key fan-out: the exact key
// plus every `prefix.*` ancestor, so a class bound to `app.mail.*` is reported
// as a consumer of `app.mail.host`. Ordering is part of the contract — targets
// in fan-out order, edges in edge-id order within each target — and a duplicate
// target is read once.
func TestCovQBReleaseConfigConsumers(t *testing.T) {
	t.Run("exact_key_then_ancestor_wildcards", func(t *testing.T) {
		edges, err := releaseConfigConsumers(covQBConfigStore(), "app.mail.host")
		if err != nil {
			t.Fatalf("releaseConfigConsumers: %v", err)
		}
		if got := covQBEdgeIDs(edges); !slices.Equal(got, []int64{1, 2, 3}) {
			t.Errorf("edge ids = %v, want [1 2 3]", got)
		}
	})
	t.Run("repeated_target_is_read_once", func(t *testing.T) {
		// `app.mail.*` generates itself twice: once as the exact key and once
		// as its own ancestor wildcard.
		edges, err := releaseConfigConsumers(covQBConfigStore(), "app.mail.*")
		if err != nil {
			t.Fatalf("releaseConfigConsumers: %v", err)
		}
		if got := covQBEdgeIDs(edges); !slices.Equal(got, []int64{3}) {
			t.Errorf("edge ids = %v, want [3]", got)
		}
	})
	t.Run("unconsumed_key", func(t *testing.T) {
		edges, err := releaseConfigConsumers(covQBConfigStore(), "app.other")
		if err != nil || len(edges) != 0 {
			t.Fatalf("releaseConfigConsumers = (%d edges, %v), want (0, nil)", len(edges), err)
		}
	})
	t.Run("edge_read_fails", func(t *testing.T) {
		store := covQBConfigStore()
		store.edgesErr = errFake
		edges, err := releaseConfigConsumers(store, "app.mail.host")
		covQBWantFake(t, "releaseConfigConsumers", edges, err)
	})
}

// ── get_transitive_tests ─────────────────────────────────────────────────────

const (
	covQBSrcFile  = "/repo/svc.go"
	covQBTestFile = "/repo/svc_test.go"
)

// covQBTestNames renders a coverage row list as `qualified name` plus the
// indirect marker, in result order.
func covQBTestNames(tests []transitiveTest) []string {
	out := make([]string, 0, len(tests))
	for _, test := range tests {
		name := test.qualifiedName
		if test.indirect {
			name += " (indirect)"
		}
		out = append(out, name)
	}
	return out
}

// covQBCoverageStore is a small service whose tests are reachable three
// different ways: through a class's contained methods, through the file that
// contains them, and through an evidence-gated bare TESTED_BY source.
func covQBCoverageStore() *covQBStore {
	store := covQBNewStore()
	service := covQBNode(2, "Class", "Service", covQBSrcFile)
	run := covQBNode(3, "Method", "Service.Run", covQBSrcFile)
	helper := covQBNode(4, "Function", "helper", covQBSrcFile)
	orphan := covQBNode(7, "Function", "orphan", covQBSrcFile)
	version := covQBNode(9, "Variable", "version", covQBSrcFile)
	file := covQBNode(1, "File", covQBSrcFile, covQBSrcFile)
	file.QualifiedName = covQBSrcFile
	store.allNodes = []graphstore.GraphNode{
		file, service, run, helper, orphan, version,
		covQBNode(5, "Test", "TestRun", covQBTestFile),
		covQBNode(6, "Test", "TestHelper", covQBTestFile),
		covQBNode(8, "Test", "TestOrphan", covQBTestFile),
	}
	store.nodes = map[string][]graphstore.GraphNode{
		covQBSrcFile: {file, service, run, helper, orphan, version},
	}

	unresolved := covQBEdge(12, "TESTED_BY", run.QualifiedName, covQBTestFile+"::TestHelper")
	unresolved.Extra = map[string]any{"unresolved_targets": []any{"a", "b"}}
	bare := covQBEdge(20, "TESTED_BY", "orphan", covQBTestFile+"::TestOrphan")
	bare.FilePath = covQBTestFile
	// Two disqualifiers sharing the bare source: a relationship of another
	// kind, and a TESTED_BY that is only a guess. Neither may be credited.
	bareGuess := covQBEdge(21, "TESTED_BY", "orphan", covQBTestFile+"::TestRun")
	bareGuess.FilePath = covQBTestFile
	bareGuess.Extra = map[string]any{"ambiguous_targets": []any{"a", "b"}}
	store.edges = covQBBySource(
		covQBEdge(10, "CONTAINS", service.QualifiedName, run.QualifiedName),
		covQBEdge(11, "TESTED_BY", run.QualifiedName, covQBTestFile+"::TestRun"),
		unresolved,
		covQBEdge(13, "TESTED_BY", run.QualifiedName, covQBTestFile+"::Ghost"),
		covQBEdge(14, "TESTED_BY", helper.QualifiedName, covQBTestFile+"::TestHelper"),
		bare,
		bareGuess,
		covQBEdge(22, "CALLS", "orphan", helper.QualifiedName),
	)
	// The bare-name gate accepts `orphan` only because svc_test.go imports the
	// file it lives in.
	store.allEdges = []graphstore.GraphEdge{
		{
			ID: 30, Kind: "IMPORTS_FROM", SourceQualified: covQBTestFile,
			TargetQualified: covQBSrcFile + "::Service", FilePath: covQBTestFile,
		},
	}
	return store
}

// TestCovQBReleaseTransitiveTestsExpandsContainers pins the first pass. A class
// or a file is covered by the tests of the symbols it CONTAINS, and the two
// disqualifiers hold: an edge carrying an unresolved-candidate marker is a
// guess, and a target with no node row is not a test that exists.
func TestCovQBReleaseTransitiveTestsExpandsContainers(t *testing.T) {
	for _, tc := range []struct {
		name   string
		target string
		want   []string
	}{
		{
			name:   "class_expands_to_contained_methods",
			target: covQBSrcFile + "::Service",
			want:   []string{covQBTestFile + "::TestRun"},
		},
		{
			name:   "file_expands_to_nested_symbols",
			target: covQBSrcFile,
			want:   []string{covQBTestFile + "::TestRun", covQBTestFile + "::TestHelper"},
		},
		{
			name:   "bare_source_is_accepted_on_unique_evidence",
			target: covQBSrcFile + "::orphan",
			want:   []string{covQBTestFile + "::TestOrphan"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tests, err := releaseTransitiveTests(covQBCoverageStore(), tc.target)
			if err != nil {
				t.Fatalf("releaseTransitiveTests: %v", err)
			}
			if got := covQBTestNames(tests); !slices.Equal(got, tc.want) {
				t.Errorf("tests = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestCovQBReleaseTransitiveTestsRejectsAmbiguousBareSources pins the gate in
// the NEGATIVE direction. A matching name alone is not evidence: with two
// same-named candidates in scope the bare TESTED_BY source cannot be
// attributed, and attributing it anyway would credit this symbol with every
// same-named test in the repository.
func TestCovQBReleaseTransitiveTestsRejectsAmbiguousBareSources(t *testing.T) {
	store := covQBCoverageStore()
	rival := covQBNode(40, "Function", "orphan", covQBTestFile)
	store.allNodes = append(store.allNodes, rival)

	tests, err := releaseTransitiveTests(store, covQBSrcFile+"::orphan")
	if err != nil {
		t.Fatalf("releaseTransitiveTests: %v", err)
	}
	if len(tests) != 0 {
		t.Errorf("tests = %v, want none for an ambiguous bare source", covQBTestNames(tests))
	}
}

// TestCovQBReleaseTransitiveTestsDedupesRepeatedTargets pins the record gate:
// two contained methods covered by the same test yield one row, not two.
func TestCovQBReleaseTransitiveTestsDedupesRepeatedTargets(t *testing.T) {
	store := covQBCoverageStore()
	store.edges[covQBSrcFile+"::Service"] = append(
		store.edges[covQBSrcFile+"::Service"],
		covQBEdge(15, "CONTAINS", covQBSrcFile+"::Service", covQBSrcFile+"::helper"),
	)
	store.edges[covQBSrcFile+"::helper"] = append(
		store.edges[covQBSrcFile+"::helper"],
		covQBEdge(16, "TESTED_BY", covQBSrcFile+"::helper", covQBTestFile+"::TestRun"),
	)

	tests, err := releaseTransitiveTests(store, covQBSrcFile+"::Service")
	if err != nil {
		t.Fatalf("releaseTransitiveTests: %v", err)
	}
	want := []string{covQBTestFile + "::TestRun", covQBTestFile + "::TestHelper"}
	if got := covQBTestNames(tests); !slices.Equal(got, want) {
		t.Errorf("tests = %v, want %v", got, want)
	}
}

// covQBCallHopStore is one caller reading its coverage through a single CALLS
// hop. `bare` has no stable identity, so following TESTED_BY from it would
// attribute every same-named test; the unresolved CALLS edge is a guess.
func covQBCallHopStore() *covQBStore {
	store := covQBNewStore()
	caller := covQBNode(1, "Function", "caller", covQBSrcFile)
	callee := covQBNode(2, "Function", "callee", covQBSrcFile)
	store.allNodes = []graphstore.GraphNode{
		caller, callee, covQBNode(3, "Test", "TestCallee", covQBTestFile),
	}
	guess := covQBEdge(52, "CALLS", caller.QualifiedName, covQBSrcFile+"::guessed")
	guess.Extra = map[string]any{"ambiguous_targets": []any{"x"}}
	store.edges = covQBBySource(
		covQBEdge(50, "CALLS", caller.QualifiedName, callee.QualifiedName),
		covQBEdge(51, "CALLS", caller.QualifiedName, "bare"),
		guess,
		covQBEdge(53, "TESTED_BY", callee.QualifiedName, covQBTestFile+"::TestCallee"),
	)
	return store
}

// TestCovQBReleaseTransitiveTestsFollowsOneCallsHop pins the third pass and
// its marker: coverage reached through a call is reported as INDIRECT, which is
// the difference between "this function is tested" and "something that calls it
// is".
func TestCovQBReleaseTransitiveTestsFollowsOneCallsHop(t *testing.T) {
	tests, err := releaseTransitiveTests(covQBCallHopStore(), covQBSrcFile+"::caller")
	if err != nil {
		t.Fatalf("releaseTransitiveTests: %v", err)
	}
	want := []string{covQBTestFile + "::TestCallee (indirect)"}
	if got := covQBTestNames(tests); !slices.Equal(got, want) {
		t.Errorf("tests = %v, want %v", got, want)
	}
}

// TestCovQBReleaseTransitiveTestsCapsTheFrontier pins the fan-out bound that
// keeps a hub function from turning one query into an O(N*M) walk: only the
// first transitiveTestFrontier callees are followed, in first-seen order.
func TestCovQBReleaseTransitiveTestsCapsTheFrontier(t *testing.T) {
	store := covQBNewStore()
	caller := covQBNode(1, "Function", "caller", covQBSrcFile)
	store.allNodes = []graphstore.GraphNode{caller}
	var edges []graphstore.GraphEdge
	total := transitiveTestFrontier + 10
	for i := range total {
		callee := covQBNode(int64(100+i), "Function", fmt.Sprintf("f%d", i), "/repo/gen.go")
		test := covQBNode(int64(500+i), "Test", fmt.Sprintf("TestF%d", i), "/repo/gen_test.go")
		store.allNodes = append(store.allNodes, callee, test)
		edges = append(edges,
			covQBEdge(int64(1000+i), "CALLS", caller.QualifiedName, callee.QualifiedName),
			covQBEdge(int64(2000+i), "TESTED_BY", callee.QualifiedName, test.QualifiedName),
		)
	}
	store.edges = covQBBySource(edges...)

	tests, err := releaseTransitiveTests(store, covQBSrcFile+"::caller")
	if err != nil {
		t.Fatalf("releaseTransitiveTests: %v", err)
	}
	if len(tests) != transitiveTestFrontier {
		t.Fatalf("got %d test(s), want the frontier cap of %d", len(tests), transitiveTestFrontier)
	}
	names := covQBTestNames(tests)
	if names[0] != "/repo/gen_test.go::TestF0 (indirect)" {
		t.Errorf("first row = %q, want the first-seen callee's test", names[0])
	}
	last := fmt.Sprintf("/repo/gen_test.go::TestF%d (indirect)", transitiveTestFrontier-1)
	if names[len(names)-1] != last {
		t.Errorf("last row = %q, want %q", names[len(names)-1], last)
	}
}

// covQBHopStore is the minimal shape that reads edges from ONE source in all
// three passes plus the callee read, so a call-ordinal failure names exactly
// one of them.
func covQBHopStore() *covQBStore {
	store := covQBNewStore()
	helper := covQBNode(1, "Function", "helper", covQBSrcFile)
	callee := covQBNode(2, "Function", "callee", covQBSrcFile)
	store.allNodes = []graphstore.GraphNode{
		helper, callee, covQBNode(3, "Test", "TestCallee", covQBTestFile),
	}
	store.edges = covQBBySource(
		covQBEdge(60, "CALLS", helper.QualifiedName, callee.QualifiedName),
		covQBEdge(61, "TESTED_BY", callee.QualifiedName, covQBTestFile+"::TestCallee"),
		// The callee's own outgoing calls are not coverage; only its
		// TESTED_BY edges are.
		covQBEdge(62, "CALLS", callee.QualifiedName, "/repo/other.go::deeper"),
	)
	return store
}

// TestCovQBReleaseTransitiveTestsSurfacesStoreFailures pins that a failed read
// in ANY of the four edge passes is reported. Silently swallowing one would
// under-report coverage, which reads as "this code is untested" — the most
// expensive wrong answer this reader can give.
func TestCovQBReleaseTransitiveTestsSurfacesStoreFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		callNo int
	}{
		{name: "direct_pass", callNo: 1},
		{name: "bare_name_pass", callNo: 2},
		{name: "calls_walk", callNo: 3},
		{name: "callee_read", callNo: 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := covQBHopStore()
			store.failCallNo = tc.callNo
			tests, err := releaseTransitiveTests(store, covQBSrcFile+"::helper")
			covQBWantFake(t, "releaseTransitiveTests", tests, err)
		})
	}
	t.Run("node_scan_fails", func(t *testing.T) {
		store := covQBHopStore()
		store.allNodesErr = errFake
		tests, err := releaseTransitiveTests(store, covQBSrcFile+"::helper")
		covQBWantFake(t, "releaseTransitiveTests", tests, err)
	})
	t.Run("file_expansion_fails", func(t *testing.T) {
		store := covQBCoverageStore()
		store.nodesErr = errFake
		tests, err := releaseTransitiveTests(store, covQBSrcFile)
		covQBWantFake(t, "releaseTransitiveTests", tests, err)
	})
	t.Run("class_expansion_fails", func(t *testing.T) {
		// A Class target reads its CONTAINS edges first, so the first call is
		// the expansion rather than the direct pass.
		store := covQBCoverageStore()
		store.failCallNo = 1
		tests, err := releaseTransitiveTests(store, covQBSrcFile+"::Service")
		covQBWantFake(t, "releaseTransitiveTests", tests, err)
	})
	t.Run("evidence_read_fails", func(t *testing.T) {
		store := covQBCoverageStore()
		store.allEdgesErr = errFake
		tests, err := releaseTransitiveTests(store, covQBSrcFile+"::orphan")
		covQBWantFake(t, "releaseTransitiveTests", tests, err)
	})
}

// TestCovQBReleaseTransitiveTestsHappyHopPath pins that covQBHopStore's four
// edge reads do produce a result when none of them fails, so the failure
// ordinals above name real passes rather than unreachable ones.
func TestCovQBReleaseTransitiveTestsHappyHopPath(t *testing.T) {
	tests, err := releaseTransitiveTests(covQBHopStore(), covQBSrcFile+"::helper")
	if err != nil {
		t.Fatalf("releaseTransitiveTests: %v", err)
	}
	want := []string{covQBTestFile + "::TestCallee (indirect)"}
	if got := covQBTestNames(tests); !slices.Equal(got, want) {
		t.Errorf("tests = %v, want %v", got, want)
	}
}

// ── the bare-name evidence gate ──────────────────────────────────────────────

// covQBEvidenceNodes are three same-named symbols in three files, so each
// scoping rule can be isolated.
func covQBEvidenceNodes() []graphstore.GraphNode {
	return []graphstore.GraphNode{
		covQBNode(1, "Function", "handle", "/repo/a.go"),
		covQBNode(2, "Function", "handle", "/repo/b.go"),
		covQBNode(3, "Class", "handle", "/repo/c.go"),
		// A kind the gate does not consider a candidate at all.
		covQBNode(4, "Variable", "handle", "/repo/d.go"),
	}
}

func covQBEvidenceStore(imports ...graphstore.GraphEdge) *covQBStore {
	store := covQBNewStore()
	store.allEdges = imports
	return store
}

func covQBImportEdge(id int64, from, target string) graphstore.GraphEdge {
	edge := covQBEdge(id, "IMPORTS_FROM", from, target)
	edge.FilePath = from
	return edge
}

// TestCovQBCandidateFor pins the evidence gate: the SOLE same-named symbol in
// the call-site file or in a file that file imports. Returning "" on ambiguity
// is the point — the caller compares the answer against the symbol it is
// resolving, so ambiguity has to read as "no match".
func TestCovQBCandidateFor(t *testing.T) {
	for _, tc := range []struct {
		name    string
		context string
		imports []graphstore.GraphEdge
		want    string
	}{
		{name: "sole_candidate_in_the_call_site_file", context: "/repo/a.go", want: "/repo/a.go::handle"},
		{
			name: "sole_candidate_in_an_imported_file", context: "/repo/x.go",
			// The import target is a qualified name; only the file part scopes.
			imports: []graphstore.GraphEdge{covQBImportEdge(10, "/repo/x.go", "/repo/b.go::other")},
			want:    "/repo/b.go::handle",
		},
		{
			name: "ambiguous_between_two_files_in_scope", context: "/repo/a.go",
			imports: []graphstore.GraphEdge{covQBImportEdge(11, "/repo/a.go", "/repo/b.go")},
			want:    "",
		},
		{name: "no_candidate_in_scope", context: "/repo/x.go", want: ""},
		{
			name: "an_import_from_another_file_does_not_widen_scope", context: "/repo/x.go",
			imports: []graphstore.GraphEdge{covQBImportEdge(12, "/repo/y.go", "/repo/b.go")},
			want:    "",
		},
		{
			name: "non_candidate_kinds_are_ignored", context: "/repo/d.go",
			want: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resolver := newEvidenceResolver(covQBEvidenceStore(tc.imports...), covQBEvidenceNodes())
			got, err := resolver.candidateFor("handle", tc.context)
			if err != nil {
				t.Fatalf("candidateFor: %v", err)
			}
			if got != tc.want {
				t.Errorf("candidateFor(handle, %q) = %q, want %q", tc.context, got, tc.want)
			}
		})
	}
}

// TestCovQBCandidateForCachesTheImportScan pins the cache. The gate is
// consulted once per bare TESTED_BY edge, so re-reading every edge each time
// would turn one query into an O(edges * edges) scan.
func TestCovQBCandidateForCachesTheImportScan(t *testing.T) {
	store := covQBEvidenceStore(covQBImportEdge(10, "/repo/x.go", "/repo/b.go::other"))
	resolver := newEvidenceResolver(store, covQBEvidenceNodes())

	first, err := resolver.candidateFor("handle", "/repo/x.go")
	if err != nil {
		t.Fatalf("candidateFor: %v", err)
	}
	// Any further edge read now fails, so a second answer can only come from
	// the cache.
	store.allEdgesErr = errFake
	second, err := resolver.candidateFor("handle", "/repo/x.go")
	if err != nil {
		t.Fatalf("candidateFor re-queried the store: %v", err)
	}
	if first != second || first != "/repo/b.go::handle" {
		t.Errorf("candidateFor = (%q, %q), want both /repo/b.go::handle", first, second)
	}
}

// TestCovQBCandidateForSurfacesEdgeReadFailures pins that an unreadable import
// set is an error rather than an empty scope, which would silently reject every
// bare-name candidate.
func TestCovQBCandidateForSurfacesEdgeReadFailures(t *testing.T) {
	store := covQBEvidenceStore()
	store.allEdgesErr = errFake
	resolver := newEvidenceResolver(store, covQBEvidenceNodes())

	got, err := resolver.candidateFor("handle", "/repo/a.go")
	covQBWantFake(t, "candidateFor", got, err)
}
