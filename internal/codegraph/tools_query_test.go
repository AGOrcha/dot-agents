package codegraph

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/crgrelease"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// The query/search/stats lane is pinned against the pinned release's OWN
// recorded answers: testdata/crg-release/v2.3.8/calls/<tool>__<case>.json holds
// the real v2.3.8 response for each call, and graph.json holds the exact rows
// the fixture repository produces.
//
// The store is seeded straight from graph.json rather than by running the
// scanner, so a failure here is a failure of THIS lane's handlers and not of
// the scanner or of the derived-view computations. Seeding also re-derives each
// node's qualified name through the store's own makeQualified and asserts it
// against the recorded one, so a node-identity regression is reported as such
// instead of as a hundred payload diffs.
//
// Cases are discovered by GLOB, so a case added or renamed by any lane is
// picked up without editing this file.

// queryLaneTools are the four tools this file owns.
var queryLaneTools = map[string]bool{
	"query_graph_tool":           true,
	"semantic_search_nodes_tool": true,
	"find_large_functions_tool":  true,
	"list_graph_stats_tool":      true,
}

// releaseCall is one recorded tools/call.
type releaseCall struct {
	Tool              string          `json:"tool"`
	Case              string          `json:"case"`
	Arguments         json.RawMessage `json:"arguments"`
	IsError           bool            `json:"is_error"`
	StructuredContent map[string]any  `json:"structured_content"`
}

func TestQueryLaneMatchesRelease(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join(releaseFixtureDir, "calls", "*.json"))
	if err != nil {
		t.Fatalf("glob call fixtures: %v", err)
	}
	covered := map[string]int{}
	for _, path := range paths {
		call := readFixtureJSON[releaseCall](t, filepath.Join("calls", filepath.Base(path)))
		if !queryLaneTools[call.Tool] {
			continue
		}
		covered[call.Tool]++
		t.Run(call.Tool+"__"+call.Case, func(t *testing.T) {
			runReleaseCallCase(t, call)
		})
	}
	// A silently empty glob would make every assertion above vacuous.
	for tool := range queryLaneTools {
		if covered[tool] == 0 {
			t.Errorf("no call fixture found for %s; regenerate the release contract", tool)
		}
	}
}

func runReleaseCallCase(t *testing.T, call releaseCall) {
	t.Helper()
	fixture := newQueryFixture(t)

	tool, ok := crgrelease.Lookup(call.Tool)
	if !ok {
		t.Fatalf("%s is not published by code-review-graph %s", call.Tool, crgrelease.Version)
	}
	args, bindErr := tool.Bind(call.Arguments)
	if bindErr != nil {
		if !call.IsError {
			t.Fatalf("Bind(%s) rejected valid arguments: %v", call.Arguments, bindErr)
		}
		return
	}

	result, err := fixture.engine.CallTool(call.Tool, args)
	if call.IsError {
		if err == nil {
			t.Fatalf("want a tool error for %s, got payload %#v", call.Case, result)
		}
		return
	}
	if err != nil {
		t.Fatalf("CallTool(%s): %v", call.Tool, err)
	}

	got, ok := jsonValue(t, result).(map[string]any)
	if !ok {
		t.Fatalf("handler returned %T, want a JSON object", result)
	}
	// `_graph` is the provenance envelope CallTool attaches; it carries the
	// live head sha and the graph's age, neither of which a fixture can pin.
	delete(got, "_graph")
	assertReleaseValue(t, "$", got, fixture.expectPayload(call))
}

// ── fixture environment ──────────────────────────────────────────────────────

// queryFixture is one materialized repository whose graph was seeded from the
// release's recorded rows.
type queryFixture struct {
	engine *Engine
	root   string
	// builtAt is the `last_updated` metadata the seeded graph carries. It
	// resolves the ${TIMESTAMP} the `list_graph_stats` summary embeds.
	builtAt string
}

// newQueryFixture materializes the fixture repository, seeds the store from
// graph.json and records the build metadata.
//
// The repository is a real git checkout because the release's empty-result
// `confidence` marker reports whether the graph's build commit still matches
// the checked-out one — without a repository every such marker would degrade to
// its "currency unverified" wording and stop matching the fixtures. That is the
// same git dependency the lifecycle contract test already declares.
//
// `last_updated` is deliberately set slightly AHEAD of the copied files'
// mtimes: the staleness check also compares a file's mtime against the build
// timestamp, so a graph recorded as older than its own sources would report
// "stale" and change those same markers.
func newQueryFixture(t *testing.T) *queryFixture {
	t.Helper()
	// The `list_graph_stats` summary names the repository directory, so the
	// leaf has to stay "repo".
	root := filepath.Join(t.TempDir(), "repo")
	copyFixtureTree(t, filepath.Join(releaseFixtureDir, "repo"), root)
	runFixtureGit(t, root, "init", "-q", "-b", "main")
	runFixtureGit(t, root, "config", "commit.gpgsign", "false")
	runFixtureGit(t, root, "add", "-A")
	runFixtureGit(t, root, "commit", "-q", "-m", "fixture: initial")
	head := runFixtureGit(t, root, "rev-parse", "--verify", "HEAD")

	engine := Open(root)
	t.Cleanup(func() { _ = engine.Close() })
	store, err := engine.writeStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	nodes, edges := loadReleaseGraphSeed(t, root)
	seedReleaseNodes(t, store, nodes)
	seedReleaseEdges(t, store, edges)
	// nodes_fts projects nodes(name, qualified_name, file_path, signature), so
	// it has to be rebuilt AFTER the signatures are written or every search
	// would miss the signature column — which is the only column that matches
	// a parameter name or a type.
	if _, err := store.RebuildFTS(); err != nil {
		t.Fatalf("RebuildFTS: %v", err)
	}

	builtAt := time.Now().Add(2 * time.Second).Format("2006-01-02T15:04:05")
	for key, value := range map[string]string{
		"last_updated": builtAt,
		"git_head_sha": head,
		"git_branch":   "main",
	} {
		if err := store.SetMetadata(key, value); err != nil {
			t.Fatalf("SetMetadata(%s): %v", key, err)
		}
	}
	if err := store.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	return &queryFixture{engine: engine, root: root, builtAt: builtAt}
}

// expectPayload is the recorded payload with `_graph` dropped and the two
// placeholders this lane can reproduce resolved. Every other token is left in
// place for assertReleaseValue to route — assertToken rejects an unknown one,
// so a newly normalized field cannot silently become a free pass.
func (f *queryFixture) expectPayload(call releaseCall) map[string]any {
	f.engine.Session()
	if call.StructuredContent == nil {
		panic(fmt.Sprintf("%s__%s records no structured_content", call.Tool, call.Case))
	}
	expected := map[string]any{}
	for key, value := range call.StructuredContent {
		if key == "_graph" {
			continue
		}
		expected[key] = value
	}
	return resolveQueryTokens(expected, map[string]string{
		"${REPO_ROOT}": filepath.ToSlash(f.root),
		"${TIMESTAMP}": f.builtAt,
	}).(map[string]any)
}

// resolveQueryTokens rewrites placeholders wherever they appear, including
// EMBEDDED in a longer string: `list_graph_stats`' summary carries the build
// timestamp mid-line and every qualified name carries the repository root, so a
// whole-value token match would miss both.
func resolveQueryTokens(value any, replacements map[string]string) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = resolveQueryTokens(item, replacements)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = resolveQueryTokens(item, replacements)
		}
		return out
	case string:
		for placeholder, actual := range replacements {
			typed = strings.ReplaceAll(typed, placeholder, actual)
		}
		return typed
	default:
		return value
	}
}

// releaseSeedNode / releaseSeedEdge are graph.json's row shapes AS THIS LANE
// NEEDS THEM. release_graph_test.go's loadReleaseGraph deliberately projects
// away `id`, `signature` and `community_id` because extraction does not own
// them — but seeding needs all three: the ids appear in every payload, the
// signature is an indexed FTS column, and the community id is a node column the
// derived views read. Hence a second, wider reader rather than a reuse.
type releaseSeedNode struct {
	ID            int64   `json:"id"`
	Kind          string  `json:"kind"`
	Name          string  `json:"name"`
	QualifiedName string  `json:"qualified_name"`
	FilePath      string  `json:"file_path"`
	LineStart     int     `json:"line_start"`
	LineEnd       int     `json:"line_end"`
	Language      *string `json:"language"`
	ParentName    *string `json:"parent_name"`
	Params        *string `json:"params"`
	ReturnType    *string `json:"return_type"`
	Modifiers     *string `json:"modifiers"`
	Signature     *string `json:"signature"`
	IsTest        int     `json:"is_test"`
	CommunityID   *int64  `json:"community_id"`
}

type releaseSeedEdge struct {
	ID       int64  `json:"id"`
	Kind     string `json:"kind"`
	Source   string `json:"source_qualified"`
	Target   string `json:"target_qualified"`
	FilePath string `json:"file_path"`
	Line     int    `json:"line"`
}

// loadReleaseGraphSeed reads graph.json with ${REPO_ROOT} resolved to root. The
// substitution runs on the raw bytes because the placeholder appears inside
// names, qualified names, paths and signatures alike.
func loadReleaseGraphSeed(t *testing.T, root string) ([]releaseSeedNode, []releaseSeedEdge) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(releaseFixtureDir, "graph.json"))
	if err != nil {
		t.Fatalf("read graph.json: %v", err)
	}
	var graph struct {
		Nodes []releaseSeedNode `json:"nodes"`
		Edges []releaseSeedEdge `json:"edges"`
	}
	resolved := strings.ReplaceAll(string(raw), repoRootPlaceholder, filepath.ToSlash(root))
	if err := json.Unmarshal([]byte(resolved), &graph); err != nil {
		t.Fatalf("decode graph.json: %v", err)
	}
	if len(graph.Nodes) == 0 || len(graph.Edges) == 0 {
		t.Fatalf("graph.json is empty: %d nodes, %d edges", len(graph.Nodes), len(graph.Edges))
	}
	return graph.Nodes, graph.Edges
}

func seedReleaseNodes(t *testing.T, store graphstore.Store, nodes []releaseSeedNode) {
	t.Helper()
	for _, node := range nodes {
		id, err := store.UpsertNode(graphstore.NodeInfo{
			Kind:       node.Kind,
			Name:       node.Name,
			FilePath:   node.FilePath,
			LineStart:  node.LineStart,
			LineEnd:    node.LineEnd,
			Language:   orEmpty(node.Language),
			ParentName: orEmpty(node.ParentName),
			Params:     orEmpty(node.Params),
			ReturnType: orEmpty(node.ReturnType),
			Modifiers:  orEmpty(node.Modifiers),
			IsTest:     node.IsTest != 0,
		}, "")
		if err != nil {
			t.Fatalf("UpsertNode(%s): %v", node.QualifiedName, err)
		}
		// Seeding in recorded id order into a fresh database must reproduce the
		// recorded ids, because every payload reports them.
		if id != node.ID {
			t.Fatalf("UpsertNode(%s) = id %d, want %d", node.QualifiedName, id, node.ID)
		}
		// The store derives the qualified name from (kind, parent, name, path),
		// so asserting it here turns a node-identity regression into one clear
		// failure instead of a payload diff on every result.
		stored, err := store.GetNode(node.QualifiedName)
		if err != nil {
			t.Fatalf("GetNode(%s): %v", node.QualifiedName, err)
		}
		if stored == nil || stored.ID != node.ID {
			t.Fatalf("node %d is not stored under its recorded qualified name %q",
				node.ID, node.QualifiedName)
		}
		if node.Signature != nil {
			if err := store.SetNodeSignature(id, *node.Signature); err != nil {
				t.Fatalf("SetNodeSignature(%d): %v", id, err)
			}
		}
		if node.CommunityID != nil {
			if err := store.SetNodeCommunity(id, *node.CommunityID); err != nil {
				t.Fatalf("SetNodeCommunity(%d): %v", id, err)
			}
		}
	}
}

func seedReleaseEdges(t *testing.T, store graphstore.Store, edges []releaseSeedEdge) {
	t.Helper()
	for _, edge := range edges {
		id, err := store.UpsertEdge(graphstore.EdgeInfo{
			Kind:     edge.Kind,
			Source:   edge.Source,
			Target:   edge.Target,
			FilePath: edge.FilePath,
			Line:     edge.Line,
		})
		if err != nil {
			t.Fatalf("UpsertEdge(%s %s -> %s): %v", edge.Kind, edge.Source, edge.Target, err)
		}
		if id != edge.ID {
			t.Fatalf("UpsertEdge(%s %s -> %s) = id %d, want %d",
				edge.Kind, edge.Source, edge.Target, id, edge.ID)
		}
	}
}

func orEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// jsonValue round-trips a value through JSON so a handler's concrete Go types
// are compared on the same footing as the recorded fixture's decoded ones.
func jsonValue(t *testing.T, value any) any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal %T: %v", value, err)
	}
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}

// assertReleaseValue walks a payload reporting every difference at its JSON
// path, and hands every LEAF to the package's assertValue so the `${...}`
// token vocabulary stays in one place.
//
// It exists because assertValue has no array arm: it compares a slice by its
// rendered form, which is a correct equality test but reports one whole-payload
// string diff instead of naming `results[3].kind`. These payloads are almost
// entirely list-shaped — `results`, `edges`, `candidates`, `disambiguation` —
// so the containers have to be walked here; assertValue's own map arm recurses
// into itself, which is why delegating the top level alone is not enough.
func assertReleaseValue(t *testing.T, path string, got, want any) {
	t.Helper()
	switch expected := want.(type) {
	case []any:
		actual, ok := got.([]any)
		if !ok {
			t.Errorf("%s: got %T (%v), want an array", path, got, got)
			return
		}
		if len(actual) != len(expected) {
			t.Errorf("%s: got %d element(s), want %d", path, len(actual), len(expected))
			return
		}
		for i := range expected {
			assertReleaseValue(t, fmt.Sprintf("%s[%d]", path, i), actual[i], expected[i])
		}
	case map[string]any:
		actual, ok := got.(map[string]any)
		if !ok {
			t.Errorf("%s: got %T (%v), want an object", path, got, got)
			return
		}
		gotKeys, wantKeys := slices.Sorted(maps.Keys(actual)), slices.Sorted(maps.Keys(expected))
		if !slices.Equal(gotKeys, wantKeys) {
			t.Errorf("%s: key set mismatch\n got: %v\nwant: %v", path, gotKeys, wantKeys)
			return
		}
		for _, key := range wantKeys {
			assertReleaseValue(t, path+"."+key, actual[key], expected[key])
		}
	default:
		assertValue(t, path, got, want)
	}
}

// ── behaviours the fixture repository cannot reach ───────────────────────────

// TestHybridSearchLimitFollowsSQLLimitSemantics pins the difference between
// `limit: 0` and `limit: -1`, both of which the release's schema accepts and
// neither of which is an error.
//
// It matters because the two are NOT the same answer: 0 reaches SQLite as
// `LIMIT 0`, so every ranked list is empty and the mode is "none", while a
// negative LIMIT is unbounded, so the mode still reports the path that found
// rows even though the bounded output is empty. The provider's bound chokepoint
// rewrites 0 to a default page size, so a handler that simply forwards the
// caller's limit reports "fts" for both.
func TestHybridSearchLimitFollowsSQLLimitSemantics(t *testing.T) {
	fixture := newQueryFixture(t)
	store, err := fixture.engine.readStore()
	if err != nil {
		t.Fatalf("readStore: %v", err)
	}

	for _, tc := range []struct {
		name     string
		query    string
		limit    int
		wantMode string
		wantHits int
	}{
		{name: "zero_limit_finds_nothing", query: "token", limit: 0, wantMode: searchModeNone},
		{name: "negative_limit_is_unbounded", query: "token", limit: -1, wantMode: searchModeFTS},
		{name: "fts_path", query: "token", limit: 20, wantMode: searchModeFTS, wantHits: 4},
		// "andle" is a substring of handleLogin that no tokenizer emits, so it
		// is only reachable through the keyword fallback.
		{name: "keyword_fallback", query: "andle", limit: 20, wantMode: searchModeKeyword, wantHits: 1},
		{name: "no_match_anywhere", query: "zzqqxx", limit: 20, wantMode: searchModeNone},
		{name: "blank_query", query: "   ", limit: 20, wantMode: searchModeNone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hits, mode, err := HybridSearch(store, tc.query, "", tc.limit, nil)
			if err != nil {
				t.Fatalf("HybridSearch: %v", err)
			}
			if mode != tc.wantMode {
				t.Errorf("search_mode = %q, want %q", mode, tc.wantMode)
			}
			if len(hits) != tc.wantHits {
				t.Errorf("got %d hit(s), want %d", len(hits), tc.wantHits)
			}
		})
	}
}

// TestSQLLikeMatchesSQLiteSemantics pins the matcher behind
// `find_large_functions_tool`'s `file_path_pattern`, which the release hands to
// SQL as `'%' || pattern || '%'`. Two consequences are observable and easy to
// get wrong: matching is case-insensitive for ASCII, and a `_` or `%` INSIDE a
// caller's pattern is a real wildcard rather than a literal.
func TestSQLLikeMatchesSQLiteSemantics(t *testing.T) {
	for _, tc := range []struct {
		value   string
		pattern string
		want    bool
	}{
		{value: "/r/pkg/auth/auth.go", pattern: "%pkg/auth%", want: true},
		{value: "/r/cmd/main.go", pattern: "%pkg/auth%", want: false},
		{value: "/r/PKG/Auth/auth.go", pattern: "%pkg/auth%", want: true},
		// `_` matches exactly one character, so it spans the "a" of "atest".
		{value: "/r/pkg/atest.go", pattern: "%_test.go%", want: true},
		{value: "/r/pkg/test.go", pattern: "%_test.go%", want: true},
		{value: "/r/test.go", pattern: "%x_test.go%", want: false},
		// A `%` inside the pattern absorbs a whole run.
		{value: "/r/a/b/c.go", pattern: "%a%c.go%", want: true},
		{value: "/r/a/b/c.go", pattern: "%c.go%a%", want: false},
		{value: "anything", pattern: "%%", want: true},
	} {
		t.Run(tc.pattern+"_vs_"+tc.value, func(t *testing.T) {
			if got := sqlLike(tc.value, tc.pattern); got != tc.want {
				t.Errorf("sqlLike(%q, %q) = %v, want %v", tc.value, tc.pattern, got, tc.want)
			}
		})
	}
}

// TestLooksLikeJavaMethodFQNRejectsDottedPaths pins the gate in front of the
// Java-FQN resolution path. It is load-bearing in the NEGATIVE direction: a
// target this accepts and cannot resolve deliberately yields no candidates
// rather than falling through to a keyword search, so a dotted module path or
// filename misclassified as a Java FQN would stop resolving altogether.
func TestLooksLikeJavaMethodFQNRejectsDottedPaths(t *testing.T) {
	for _, tc := range []struct {
		target string
		want   bool
	}{
		{target: "com.example.service.handle", want: true},
		{target: "Session.expired", want: true},
		// Two segments are accepted only for Class.method: a lowercase first
		// segment is an ordinary dotted filename or module path.
		{target: "utils.helper", want: false},
		{target: "auth.go", want: false},
		{target: "hashPassword", want: false},
		// A qualified name is already resolved and never takes this path.
		{target: "auth.go::Login", want: false},
		{target: "pkg/auth.go", want: false},
		{target: "", want: false},
	} {
		t.Run(tc.target, func(t *testing.T) {
			if got := looksLikeJavaMethodFQN(tc.target); got != tc.want {
				t.Errorf("looksLikeJavaMethodFQN(%q) = %v, want %v", tc.target, got, tc.want)
			}
		})
	}
}

// TestNormalizeSpringConfigKeyFoldsRelaxedBinding pins `consumers_of`'s target
// canonicalization. Spring's relaxed binding means one property has many
// spellings while the graph stores exactly one, so a wrong fold looks up a key
// no edge references and reports "no consumers" for a property that has them.
func TestNormalizeSpringConfigKeyFoldsRelaxedBinding(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{in: "my-app.data-source-url", want: "myApp.dataSourceUrl"},
		{in: "my_app.data_source_url", want: "myApp.dataSourceUrl"},
		{in: "MY_APP.DATA_SOURCE_URL", want: "myApp.dataSourceUrl"},
		// A single token keeps its case unless the whole segment is uppercase,
		// so an already-camelCase spelling survives untouched.
		{in: "myApp.dataSource", want: "myApp.dataSource"},
		{in: "DATASOURCE.url", want: "datasource.url"},
		// A list index identifies an element, not a name, so it is preserved.
		{in: "my-app.servers[2].host", want: "myApp.servers[2].host"},
		{in: "  spaced.key  ", want: "spaced.key"},
	} {
		t.Run(tc.in, func(t *testing.T) {
			if got := normalizeSpringConfigKey(tc.in); got != tc.want {
				t.Errorf("normalizeSpringConfigKey(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestExtractQueryIdentifiersKeepsReleaseOrder pins the boost-token scan. The
// ORDER is part of the behaviour — the release scans dotted, then snake_case,
// then CamelCase, de-duplicating case-insensitively on first appearance.
//
// Note what the CamelCase pattern actually is: `[A-Z][a-z0-9]+([A-Z][a-z0-9]+)+`
// needs a lowercase-or-digit run after EVERY capital, so `ValidateToken`
// matches but an all-caps prefix like `APIRoute` does not, despite the
// release's docstring claiming otherwise. This pins the regex, which is what
// actually runs.
func TestExtractQueryIdentifiersKeepsReleaseOrder(t *testing.T) {
	got := extractQueryIdentifiers("Who calls ValidateToken and get_dependant via Context.Next?")
	want := []string{"context.next", "get_dependant", "validatetoken"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("extractQueryIdentifiers = %v, want %v", got, want)
	}
	if extracted := extractQueryIdentifiers("APIRoute and HTTPServer"); len(extracted) != 0 {
		t.Fatalf("an all-caps prefix must not match the CamelCase pattern, got %v", extracted)
	}
}
