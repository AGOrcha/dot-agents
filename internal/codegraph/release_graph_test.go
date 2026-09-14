// Package codegraph — extraction parity against the pinned code-review-graph
// v2.3.8 release contract.
//
// testdata/crg-release/v2.3.8/graph.json holds the EXACT nodes and edges the
// release produces for testdata/crg-release/v2.3.8/repo, with the repository
// root normalized to ${REPO_ROOT}. These tests copy that repository into a
// temp root, run the native scanner and persistence, read the rows back out of
// the store in id order, and require them to equal the release's rows one for
// one. That is this lane's whole contract: if a node's line range, parameter
// text, kind or identity drifts, or a call target resolves differently, the
// graph is a different graph and every view derived from it diverges with it.
package codegraph

import (
	"database/sql"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/graphstore"
	_ "modernc.org/sqlite" // the driver graphstore opens the graph with
)

// releaseContractDir is the pinned v2.3.8 contract fixture directory.
const releaseContractDir = "../../testdata/crg-release/v2.3.8"

// repoRootPlaceholder is the token the fixture generator substitutes for the
// absolute repository root, which is a temp path on every machine.
const repoRootPlaceholder = "${REPO_ROOT}"

// nodeRow is the persisted node projection these tests compare. It omits the
// columns extraction does not own: `id` (its assignment order is asserted by
// comparing ordered slices instead), `signature` and `community_id` (both
// written by postprocess), and `file_hash`/`extra`/`updated_at`, which have no
// release counterpart.
type nodeRow struct {
	Kind       string
	Name       string
	Qualified  string
	FilePath   string
	LineStart  int
	LineEnd    int
	Language   string
	ParentName string
	Params     string
	ReturnType string
	Modifiers  string
	IsTest     int
}

// edgeRow is the persisted edge projection. `confidence`/`confidence_tier` are
// column defaults rather than extraction output, so they are not compared.
type edgeRow struct {
	Kind     string
	Source   string
	Target   string
	FilePath string
	Line     int
}

// ─── the parity assertion ────────────────────────────────────────────────────

func TestNativeScanReproducesReleaseGraphRows(t *testing.T) {
	root := copyReleaseRepo(t)
	wantNodes, wantEdges := loadReleaseGraph(t, root)

	gotNodes, gotEdges := persistedGraphRows(t, root)

	if len(gotNodes) != len(wantNodes) {
		t.Fatalf("node count = %d, want %d\n got: %s\nwant: %s",
			len(gotNodes), len(wantNodes), formatNodes(gotNodes), formatNodes(wantNodes))
	}
	for i := range wantNodes {
		if gotNodes[i] != wantNodes[i] {
			t.Errorf("node[%d] diverges from the release:\n got: %+v\nwant: %+v",
				i, gotNodes[i], wantNodes[i])
		}
	}
	if len(gotEdges) != len(wantEdges) {
		t.Fatalf("edge count = %d, want %d\n got: %s\nwant: %s",
			len(gotEdges), len(wantEdges), formatEdges(gotEdges), formatEdges(wantEdges))
	}
	for i := range wantEdges {
		if gotEdges[i] != wantEdges[i] {
			t.Errorf("edge[%d] diverges from the release:\n got: %+v\nwant: %+v",
				i, gotEdges[i], wantEdges[i])
		}
	}
}

// TestNativeScanEdgeEndpointsResolveToPersistedNodes is the invariant tying the
// scanner's own qualified-name construction to graphstore's makeQualified: an
// endpoint carrying the `::` separator is an identity claim, and it must match
// a persisted node's qualified_name exactly. The endpoints the release
// deliberately leaves bare — an unresolved call target, its TESTED_BY mirror,
// and an import path — are exactly the ones without that separator.
func TestNativeScanEdgeEndpointsResolveToPersistedNodes(t *testing.T) {
	root := copyReleaseRepo(t)
	nodes, edges := persistedGraphRows(t, root)

	known := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		known[n.Qualified] = true
	}
	checked := 0
	for _, e := range edges {
		for _, endpoint := range []string{e.Source, e.Target} {
			if !strings.Contains(endpoint, "::") {
				continue // a deliberately bare target or a raw import path
			}
			checked++
			if !known[endpoint] {
				t.Errorf("edge %s %s -> %s names %q, which is no persisted node",
					e.Kind, e.Source, e.Target, endpoint)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no qualified endpoint was checked; the fixture or the scanner produced none")
	}
}

// ─── call-target resolution ──────────────────────────────────────────────────

// TestScanCallTargetResolutionMatchesRelease pins the resolution rule the whole
// call graph rests on, including the parts that look like bugs and are not
// ours to fix. sameFile and crossFile are the regression pair: the release
// resolves a call only against the CALLING FILE's own declarations, so
// `helper()` in the same file is fully qualified while `crossFile()` one file
// away in the SAME PACKAGE stays a bare name. A resolver that "improved" on
// that would silently rewrite every cross-file call target in the graph.
func TestScanCallTargetResolutionMatchesRelease(t *testing.T) {
	root := writeGoTree(t, map[string]string{
		"go.mod": "module example.com/probe\n\ngo 1.22\n",
		"a.go": `package probe

import "fmt"

type Holder struct{ n int }

func (h *Holder) Outer() int {
	h.inner()
	fmt.Println(h.n)
	helper()
	crossFile()
	if false {
		neverCalled()
	}
	shadowed := func(h *Other) { h.other() }
	_ = shadowed
	return h.n
}

func (h *Holder) inner() int { return h.n }

func helper() {}

func neverCalled() {}

type Other struct{}

func (o *Other) other() {}
`,
		"b.go": "package probe\n\nfunc crossFile() {}\n",
	})
	abs := filepath.ToSlash(root)
	outer := abs + "/a.go::Holder.Outer"

	_, edges := persistedGraphRows(t, root)
	calls := map[string]bool{}
	for _, e := range edges {
		if e.Kind == "CALLS" && e.Source == outer {
			calls[e.Target] = true
		}
	}

	cases := map[string]struct {
		target  string
		present bool
	}{
		// A call through the enclosing method's own receiver resolves to that
		// receiver's method in the same file.
		"receiverSelfCall": {abs + "/a.go::Holder.inner", true},
		// A package selector is never resolved: the member name stays bare.
		"packageSelector": {"Println", true},
		// A bare name declared in this file resolves.
		"sameFile": {abs + "/a.go::helper", true},
		// A bare name declared in ANOTHER FILE of the same package does not.
		"crossFile": {"crossFile", true},
		// `if false { ... }` is statically dead, so its call is not extracted
		// under either spelling.
		"deadGuardBare":     {"neverCalled", false},
		"deadGuardResolved": {abs + "/a.go::neverCalled", false},
		// The func literal's parameter shadows the receiver name, so the call
		// is not a receiver self-call and stays bare.
		"shadowedReceiver": {"other", true},
		"shadowedResolved": {abs + "/a.go::Other.other", false},
	}
	for name, want := range cases {
		if calls[want.target] != want.present {
			t.Errorf("%s: CALLS target %q present = %v, want %v (targets: %v)",
				name, want.target, calls[want.target], want.present, sortedBoolKeys(calls))
		}
	}
}

// ─── node shape ──────────────────────────────────────────────────────────────

// TestScanNodeShapeMatchesRelease pins the declaration shapes the release
// fixture repository does not exercise. Each was settled by running the v2.3.8
// parser over this same source.
func TestScanNodeShapeMatchesRelease(t *testing.T) {
	root := writeGoTree(t, map[string]string{
		"go.mod": "module example.com/probe\n\ngo 1.22\n",
		"a.go": `package probe

type Alias = int

type (
	AliasFirst = int
	RealSecond struct{ A int }
)

type Stack[T any] struct{ items []T }

func (s *Stack[T]) Push(v T) {}

func (*Stack[T]) Anon() {}

func Variadic(prefix string, rest ...int) (sum int, err error) { return 0, nil }

func Generic[T any, U comparable](in T, key U) T { return in }

const Version = "1"

var Global = 2

func nest() {
	type Inner struct{ X int }
	_ = Inner{}
}

func BenchmarkThing() {}

func test_lower() {}
`,
	})
	abs := filepath.ToSlash(root)
	file := abs + "/a.go"
	nodes, edges := persistedGraphRows(t, root)
	byQualified := make(map[string]nodeRow, len(nodes))
	for _, n := range nodes {
		byQualified[n.Qualified] = n
	}

	// A type alias declares nothing upstream, so neither a lone alias nor the
	// alias member of a group becomes a node — and `const`/`var` never do.
	for _, absent := range []string{
		file + "::Alias", file + "::AliasFirst",
		file + "::Version", file + "::Global",
	} {
		if _, ok := byQualified[absent]; ok {
			t.Errorf("%q must not produce a node", absent)
		}
	}

	for _, want := range []nodeRow{
		// One node for the whole `type (...)` group, named after its first
		// non-alias spec and spanning keyword through closing paren.
		{Kind: "Class", Name: "RealSecond", Qualified: file + "::RealSecond",
			FilePath: file, LineStart: 5, LineEnd: 8, Language: "go"},
		// A generic type keeps its bare name.
		{Kind: "Class", Name: "Stack", Qualified: file + "::Stack",
			FilePath: file, LineStart: 10, LineEnd: 10, Language: "go"},
		// A method's params are its RECEIVER list, verbatim, and its parent is
		// the receiver type with type arguments stripped.
		{Kind: "Function", Name: "Push", Qualified: file + "::Stack.Push",
			FilePath: file, LineStart: 12, LineEnd: 12, Language: "go",
			ParentName: "Stack", Params: "(s *Stack[T])"},
		// An anonymous receiver still attaches the method to its type.
		{Kind: "Function", Name: "Anon", Qualified: file + "::Stack.Anon",
			FilePath: file, LineStart: 14, LineEnd: 14, Language: "go",
			ParentName: "Stack", Params: "(*Stack[T])"},
		// Variadic parameters are verbatim; named results are not params, and
		// return_type stays empty because a Go result is not one of the node
		// types upstream reads a return type from.
		{Kind: "Function", Name: "Variadic", Qualified: file + "::Variadic",
			FilePath: file, LineStart: 16, LineEnd: 16, Language: "go",
			Params: "(prefix string, rest ...int)"},
		// A type parameter list is skipped: params is the VALUE parameter list.
		{Kind: "Function", Name: "Generic", Qualified: file + "::Generic",
			FilePath: file, LineStart: 18, LineEnd: 18, Language: "go",
			Params: "(in T, key U)"},
		// A type declared inside a function body is still a file-level node.
		{Kind: "Class", Name: "Inner", Qualified: file + "::Inner",
			FilePath: file, LineStart: 25, LineEnd: 25, Language: "go"},
		// `Benchmark*` is not a test; a `test_`-prefixed name is one even in a
		// production file, because the predicate keys on the NAME first.
		{Kind: "Function", Name: "BenchmarkThing", Qualified: file + "::BenchmarkThing",
			FilePath: file, LineStart: 29, LineEnd: 29, Language: "go", Params: "()"},
		{Kind: "Test", Name: "test_lower", Qualified: file + "::test_lower",
			FilePath: file, LineStart: 31, LineEnd: 31, Language: "go",
			Params: "()", IsTest: 1},
	} {
		got, ok := byQualified[want.Qualified]
		if !ok {
			t.Errorf("no node %q; persisted: %v", want.Qualified, sortedNodeKeys(byQualified))
			continue
		}
		if got != want {
			t.Errorf("node %q:\n got: %+v\nwant: %+v", want.Qualified, got, want)
		}
	}

	// A nested type is contained by its FILE, not by the function it sits in.
	if !hasEdge(edges, edgeRow{
		Kind: "CONTAINS", Source: file, Target: file + "::Inner", FilePath: file, Line: 25,
	}) {
		t.Errorf("nested type is not contained by its file; edges: %s", formatEdges(edges))
	}
	// A method is contained by its receiver type, not by the file.
	if !hasEdge(edges, edgeRow{
		Kind: "CONTAINS", Source: file + "::Stack", Target: file + "::Stack.Push",
		FilePath: file, Line: 12,
	}) {
		t.Errorf("method is not contained by its receiver type; edges: %s", formatEdges(edges))
	}
}

// TestScanFileNodeSpansWholeFile pins the File node's shape: its name, its
// identity and its path are one string, and its line_end is the newline count
// plus one rather than the number of lines of text.
func TestScanFileNodeSpansWholeFile(t *testing.T) {
	root := writeGoTree(t, map[string]string{
		"a.go":          "package probe\n\nfunc A() {}\n",
		"sub/a_test.go": "package sub\n\nfunc TestA() {}\n",
		"noeol/b.go":    "package noeol\n\nfunc B() {}",
	})
	abs := filepath.ToSlash(root)
	nodes, _ := persistedGraphRows(t, root)
	byQualified := make(map[string]nodeRow, len(nodes))
	for _, n := range nodes {
		byQualified[n.Qualified] = n
	}
	for _, want := range []nodeRow{
		{Kind: "File", Name: abs + "/a.go", Qualified: abs + "/a.go",
			FilePath: abs + "/a.go", LineStart: 1, LineEnd: 4, Language: "go"},
		// A `_test.go` path makes the File node itself a test node.
		{Kind: "File", Name: abs + "/sub/a_test.go", Qualified: abs + "/sub/a_test.go",
			FilePath: abs + "/sub/a_test.go", LineStart: 1, LineEnd: 4, Language: "go",
			IsTest: 1},
		// No trailing newline: three lines of text but only two newlines, so
		// the count-plus-one rule reports 3 rather than 4.
		{Kind: "File", Name: abs + "/noeol/b.go", Qualified: abs + "/noeol/b.go",
			FilePath: abs + "/noeol/b.go", LineStart: 1, LineEnd: 3, Language: "go"},
	} {
		got, ok := byQualified[want.Qualified]
		if !ok {
			t.Errorf("no File node %q; persisted: %v", want.Qualified, sortedNodeKeys(byQualified))
			continue
		}
		if got != want {
			t.Errorf("File node %q:\n got: %+v\nwant: %+v", want.Qualified, got, want)
		}
	}
}

// ─── test mirroring ──────────────────────────────────────────────────────────

// TestScanMirrorsTestCallsAsTestedBy pins TESTED_BY generation: every CALLS
// edge made by a test declaration is mirrored back from callee to test,
// including a bare callee, and the mirrors are NOT deduplicated across call
// sites. A helper that is not named like a test is not a test, so its calls
// are not mirrored at all.
func TestScanMirrorsTestCallsAsTestedBy(t *testing.T) {
	root := writeGoTree(t, map[string]string{
		"a_test.go": `package probe

func helper() {}

func TestTwice() {
	helper()
	helper()
	external()
}

func notATest() {
	helper()
}
`,
	})
	file := filepath.ToSlash(root) + "/a_test.go"
	_, edges := persistedGraphRows(t, root)

	var mirrors []edgeRow
	for _, e := range edges {
		if e.Kind == "TESTED_BY" {
			mirrors = append(mirrors, e)
		}
	}
	want := []edgeRow{
		{Kind: "TESTED_BY", Source: file + "::helper", Target: file + "::TestTwice", FilePath: file, Line: 6},
		{Kind: "TESTED_BY", Source: file + "::helper", Target: file + "::TestTwice", FilePath: file, Line: 7},
		// A bare callee is mirrored exactly as the CALLS edge left it.
		{Kind: "TESTED_BY", Source: "external", Target: file + "::TestTwice", FilePath: file, Line: 8},
	}
	if len(mirrors) != len(want) {
		t.Fatalf("TESTED_BY edges = %s, want %s", formatEdges(mirrors), formatEdges(want))
	}
	for i := range want {
		if mirrors[i] != want[i] {
			t.Errorf("TESTED_BY[%d]:\n got: %+v\nwant: %+v", i, mirrors[i], want[i])
		}
	}
}

// ─── persistence ─────────────────────────────────────────────────────────────

// TestPersistFileCollapsesDuplicateEdgeRows pins the two-sided behaviour of the
// release's edge write: two calls to the same unresolved name on ONE line are
// two extracted edges — which is what the build report counts — and a single
// persisted row, because the write upserts on
// (kind, source, target, file, line). Collapsing before counting would
// under-report the build; storing both would double every such call site in
// the graph's own edge total.
func TestPersistFileCollapsesDuplicateEdgeRows(t *testing.T) {
	root := writeGoTree(t, map[string]string{
		"a.go": "package probe\n\nfunc twice() { external(); external() }\n",
	})
	files, _, err := Scan(root, "")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("scanned %d files, want 1", len(files))
	}
	extracted := 0
	for _, e := range files[0].Edges {
		if e.Kind == "CALLS" && e.Target == "external" {
			extracted++
		}
	}
	if extracted != 2 {
		t.Errorf("extracted CALLS edges to `external` = %d, want 2 (one per call site)", extracted)
	}

	store, dbPath := openGraphStore(t)
	nodes, edges, err := persistFile(store, files[0])
	if err != nil {
		t.Fatalf("persistFile: %v", err)
	}
	if nodes != 2 || edges != 3 {
		t.Errorf("persistFile counts = (%d nodes, %d edges), want (2, 3): the report counts "+
			"what was extracted, not what survived the upsert", nodes, edges)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	rows := 0
	for _, e := range readEdgeRows(t, dbPath) {
		if e.Kind == "CALLS" && e.Target == "external" {
			rows++
		}
	}
	if rows != 1 {
		t.Errorf("persisted CALLS rows to `external` = %d, want 1 (the upsert collapses them)", rows)
	}
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// copyReleaseRepo materializes the pinned fixture repository under a temp root.
func copyReleaseRepo(t *testing.T) string {
	t.Helper()
	src := filepath.Join(releaseContractDir, "repo")
	root := filepath.Join(t.TempDir(), "fixture")
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, relErr := filepath.Rel(src, path)
		if relErr != nil {
			return relErr
		}
		dst := filepath.Join(root, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		return os.WriteFile(dst, body, 0o644)
	})
	if err != nil {
		t.Fatalf("copy release fixture repo: %v", err)
	}
	requireNonTestRoot(t, root)
	return root
}

// writeGoTree materializes an ad-hoc source tree under a temp root.
func writeGoTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "tree")
	for rel, body := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	requireNonTestRoot(t, root)
	return root
}

// requireNonTestRoot guards the one way the host's temp directory can change
// what these tests observe: the release's test-file predicate is an unanchored
// substring match over the WHOLE absolute path, so a root containing a `test/`
// or `tests/` segment would mark every file in the tree as a test.
func requireNonTestRoot(t *testing.T, root string) {
	t.Helper()
	if isTestFile(filepath.ToSlash(filepath.Join(root, "probe.go"))) {
		t.Fatalf("temp root %q contains a test/ path segment, which the release's "+
			"test-file predicate matches — every node would be marked is_test", root)
	}
}

// openGraphStore opens a fresh graph database and returns it with its path, so
// a test can close the store and read the rows back in id order.
func openGraphStore(t *testing.T) (graphstore.Store, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "graph", "code-graph.db")
	store, err := graphstore.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	return store, dbPath
}

// persistedGraphRows scans root, persists every file, and returns the stored
// rows in insertion order.
func persistedGraphRows(t *testing.T, root string) ([]nodeRow, []edgeRow) {
	t.Helper()
	files, _, err := Scan(root, "")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	store, dbPath := openGraphStore(t)
	if _, _, err := persistFiles(store, files); err != nil {
		t.Fatalf("persistFiles: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	return readNodeRows(t, dbPath), readEdgeRows(t, dbPath)
}

func openGraphDB(t *testing.T, dbPath string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open graph db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func readNodeRows(t *testing.T, dbPath string) []nodeRow {
	t.Helper()
	rows, err := openGraphDB(t, dbPath).Query(
		`SELECT kind, name, qualified_name, file_path, line_start, line_end,
		        language, parent_name, params, return_type, modifiers, is_test
		 FROM nodes ORDER BY id`)
	if err != nil {
		t.Fatalf("query nodes: %v", err)
	}
	defer func() { _ = rows.Close() }()

	var out []nodeRow
	for rows.Next() {
		var n nodeRow
		var lineStart, lineEnd sql.NullInt64
		var language, parent, params, ret, modifiers sql.NullString
		if err := rows.Scan(&n.Kind, &n.Name, &n.Qualified, &n.FilePath,
			&lineStart, &lineEnd, &language, &parent, &params, &ret,
			&modifiers, &n.IsTest); err != nil {
			t.Fatalf("scan node: %v", err)
		}
		n.LineStart, n.LineEnd = int(lineStart.Int64), int(lineEnd.Int64)
		n.Language, n.ParentName = language.String, parent.String
		n.Params, n.ReturnType, n.Modifiers = params.String, ret.String, modifiers.String
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate nodes: %v", err)
	}
	return out
}

func readEdgeRows(t *testing.T, dbPath string) []edgeRow {
	t.Helper()
	rows, err := openGraphDB(t, dbPath).Query(
		`SELECT kind, source_qualified, target_qualified, file_path, line
		 FROM edges ORDER BY id`)
	if err != nil {
		t.Fatalf("query edges: %v", err)
	}
	defer func() { _ = rows.Close() }()

	var out []edgeRow
	for rows.Next() {
		var e edgeRow
		var line sql.NullInt64
		if err := rows.Scan(&e.Kind, &e.Source, &e.Target, &e.FilePath, &line); err != nil {
			t.Fatalf("scan edge: %v", err)
		}
		e.Line = int(line.Int64)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate edges: %v", err)
	}
	return out
}

// releaseGraph mirrors graph.json's nodes and edges. Nullable columns are
// pointers so a JSON null stays distinguishable from an empty string, then are
// flattened to "" — which is how graphstore reads a NULL column back.
type releaseGraph struct {
	Nodes []struct {
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
		IsTest        int     `json:"is_test"`
	} `json:"nodes"`
	Edges []struct {
		ID       int64  `json:"id"`
		Kind     string `json:"kind"`
		Source   string `json:"source_qualified"`
		Target   string `json:"target_qualified"`
		FilePath string `json:"file_path"`
		Line     int    `json:"line"`
	} `json:"edges"`
}

// loadReleaseGraph reads graph.json, substitutes the placeholder root, and
// returns the release's rows in id order.
func loadReleaseGraph(t *testing.T, root string) ([]nodeRow, []edgeRow) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(releaseContractDir, "graph.json"))
	if err != nil {
		t.Fatalf("read release graph: %v", err)
	}
	var graph releaseGraph
	if err := json.Unmarshal(raw, &graph); err != nil {
		t.Fatalf("parse release graph: %v", err)
	}
	if len(graph.Nodes) == 0 || len(graph.Edges) == 0 {
		t.Fatalf("release graph fixture is empty: %d nodes, %d edges",
			len(graph.Nodes), len(graph.Edges))
	}
	sort.Slice(graph.Nodes, func(i, j int) bool { return graph.Nodes[i].ID < graph.Nodes[j].ID })
	sort.Slice(graph.Edges, func(i, j int) bool { return graph.Edges[i].ID < graph.Edges[j].ID })

	slashRoot := filepath.ToSlash(root)
	subst := func(s string) string {
		return strings.ReplaceAll(s, repoRootPlaceholder, slashRoot)
	}
	deref := func(p *string) string {
		if p == nil {
			return ""
		}
		return subst(*p)
	}

	nodes := make([]nodeRow, 0, len(graph.Nodes))
	for _, n := range graph.Nodes {
		nodes = append(nodes, nodeRow{
			Kind: n.Kind, Name: subst(n.Name), Qualified: subst(n.QualifiedName),
			FilePath: subst(n.FilePath), LineStart: n.LineStart, LineEnd: n.LineEnd,
			Language: deref(n.Language), ParentName: deref(n.ParentName),
			Params: deref(n.Params), ReturnType: deref(n.ReturnType),
			Modifiers: deref(n.Modifiers), IsTest: n.IsTest,
		})
	}
	edges := make([]edgeRow, 0, len(graph.Edges))
	for _, e := range graph.Edges {
		edges = append(edges, edgeRow{
			Kind: e.Kind, Source: subst(e.Source), Target: subst(e.Target),
			FilePath: subst(e.FilePath), Line: e.Line,
		})
	}
	return nodes, edges
}

func hasEdge(edges []edgeRow, want edgeRow) bool {
	for _, e := range edges {
		if e == want {
			return true
		}
	}
	return false
}

func formatNodes(nodes []nodeRow) string {
	parts := make([]string, 0, len(nodes))
	for _, n := range nodes {
		parts = append(parts, n.Kind+" "+n.Qualified)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func formatEdges(edges []edgeRow) string {
	parts := make([]string, 0, len(edges))
	for _, e := range edges {
		parts = append(parts, e.Kind+" "+e.Source+"->"+e.Target+"@"+strconv.Itoa(e.Line))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func sortedBoolKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedNodeKeys(m map[string]nodeRow) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
