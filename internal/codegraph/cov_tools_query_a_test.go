package codegraph

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/crgrelease"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// query_graph_tool's sixteen patterns, its four-step target resolution and its
// streaming result accumulator, driven against a tabular store.
//
// The release's own recorded answers are pinned by TestQueryLaneMatchesRelease,
// which runs every `query_graph_tool__*` fixture against a graph seeded from
// the release's rows. That oracle covers the patterns the fixture repository
// can express; it cannot reach the five event/trigger/config patterns (the
// fixture repo has no Spring, no scheduler and no event bus), the Java-FQN
// resolution path (no Java sources), the language-specific carve-outs (C++
// overload sets, C# namespace imports), or any store failure.
//
// Those are what this file drives, through a seeded fake rather than SQLite so
// that a single edge can be given exactly the shape that selects one branch —
// an edge whose source has no node row, an edge carrying an ambiguous candidate
// set, a reader that fails only on its second call — none of which a real graph
// can be persuaded to contain.

// ── the fake store ───────────────────────────────────────────────────────────

// covQAStore is a graphstore.Store whose every reader is a lookup table.
//
// The contract interface is embedded as a nil value, exactly as fakeStore does
// it, so a reader these paths are not supposed to touch fails loudly instead of
// quietly answering zero.
type covQAStore struct {
	graphstore.Store

	allNodes []graphstore.GraphNode
	allEdges []graphstore.GraphEdge
	byName   map[string]graphstore.GraphNode
	byFile   map[string][]graphstore.GraphNode
	bySource map[string][]graphstore.GraphEdge
	byTarget map[string][]graphstore.GraphEdge
	files    []string
	stats    graphstore.GraphStats
	meta     map[string]string

	// missing names a row the bulk read still lists but the keyed lookup no
	// longer finds — a row dropped between a multi-pass query's first scan
	// and its projection.
	missing map[string]bool

	// fail injects a reader failure, keyed either by method name or by
	// "method:argument" so one call of a method can fail while another
	// succeeds. failAfter injects a failure that arrives only once a method
	// has already answered n times, which is the only way to reach a branch
	// guarded by an earlier call to the same reader.
	fail      map[string]error
	failAfter map[string]int
	calls     map[string]int
	// seen records every reader call as "method:argument", so a test can
	// assert that a lookup was NOT performed.
	seen []string
}

// covQASeed indexes a node/edge list into a store. Ids are assigned in
// declaration order, so both the payload's `id` fields and the edge ordering
// every pattern reports are deterministic.
func covQASeed(nodes []graphstore.GraphNode, edges []graphstore.GraphEdge) *covQAStore {
	s := &covQAStore{
		byName:    map[string]graphstore.GraphNode{},
		byFile:    map[string][]graphstore.GraphNode{},
		bySource:  map[string][]graphstore.GraphEdge{},
		byTarget:  map[string][]graphstore.GraphEdge{},
		meta:      map[string]string{},
		missing:   map[string]bool{},
		fail:      map[string]error{},
		failAfter: map[string]int{},
		calls:     map[string]int{},
	}
	for i := range nodes {
		node := nodes[i]
		node.ID = int64(i + 1)
		s.allNodes = append(s.allNodes, node)
		s.byName[node.QualifiedName] = node
		if _, ok := s.byFile[node.FilePath]; !ok {
			s.files = append(s.files, node.FilePath)
		}
		s.byFile[node.FilePath] = append(s.byFile[node.FilePath], node)
	}
	sort.Strings(s.files)
	for i := range edges {
		edge := edges[i]
		edge.ID = int64(i + 1)
		edge.Confidence = 1.0
		edge.ConfidenceTier = "EXTRACTED"
		s.allEdges = append(s.allEdges, edge)
		s.bySource[edge.SourceQualified] = append(s.bySource[edge.SourceQualified], edge)
		s.byTarget[edge.TargetQualified] = append(s.byTarget[edge.TargetQualified], edge)
	}
	s.stats = graphstore.GraphStats{
		TotalNodes: len(nodes), TotalEdges: len(edges), FilesCount: len(s.files),
	}
	return s
}

// gate records one reader call and returns the failure configured for it.
func (s *covQAStore) gate(method, arg string) error {
	s.seen = append(s.seen, method+":"+arg)
	s.calls[method]++
	if err, ok := s.fail[method+":"+arg]; ok {
		return err
	}
	if err, ok := s.fail[method]; ok {
		return err
	}
	if after, ok := s.failAfter[method]; ok && s.calls[method] > after {
		return errFake
	}
	return nil
}

// forget clears the call log, so an assertion about which lookups a query
// performed is not polluted by the test's own setup reads.
func (s *covQAStore) forget() {
	s.seen = nil
	s.calls = map[string]int{}
}

// consulted reports whether a "method:argument" lookup was ever performed.
func (s *covQAStore) consulted(call string) bool {
	for _, recorded := range s.seen {
		if recorded == call {
			return true
		}
	}
	return false
}

func (s *covQAStore) GetNode(qualified string) (*graphstore.GraphNode, error) {
	if err := s.gate("GetNode", qualified); err != nil {
		return nil, err
	}
	node, ok := s.byName[qualified]
	if !ok || s.missing[qualified] {
		return nil, nil
	}
	return &node, nil
}

func (s *covQAStore) GetNodesByFile(filePath string) ([]graphstore.GraphNode, error) {
	if err := s.gate("GetNodesByFile", filePath); err != nil {
		return nil, err
	}
	return s.byFile[filePath], nil
}

func (s *covQAStore) GetEdgesBySource(qualified string) ([]graphstore.GraphEdge, error) {
	if err := s.gate("GetEdgesBySource", qualified); err != nil {
		return nil, err
	}
	return s.bySource[qualified], nil
}

func (s *covQAStore) GetEdgesByTarget(qualified string) ([]graphstore.GraphEdge, error) {
	if err := s.gate("GetEdgesByTarget", qualified); err != nil {
		return nil, err
	}
	return s.byTarget[qualified], nil
}

func (s *covQAStore) GetAllFiles() ([]string, error) {
	if err := s.gate("GetAllFiles", ""); err != nil {
		return nil, err
	}
	return s.files, nil
}

func (s *covQAStore) GetStats() (graphstore.GraphStats, error) {
	if err := s.gate("GetStats", ""); err != nil {
		return graphstore.GraphStats{}, err
	}
	return s.stats, nil
}

func (s *covQAStore) GetMetadata(key string) (string, error) {
	if err := s.gate("GetMetadata", key); err != nil {
		return "", err
	}
	return s.meta[key], nil
}

func (s *covQAStore) ReadAllNodes() ([]graphstore.GraphNode, error) {
	if err := s.gate("ReadAllNodes", ""); err != nil {
		return nil, err
	}
	return s.allNodes, nil
}

func (s *covQAStore) ReadAllEdges() ([]graphstore.GraphEdge, error) {
	if err := s.gate("ReadAllEdges", ""); err != nil {
		return nil, err
	}
	return s.allEdges, nil
}

// SearchNodesFTSWords reports the backend's "no FTS5 index" answer, which is
// the successful outcome that sends search_nodes down its LIKE fallback. That
// keeps every resolution result a function of the seeded rows alone.
func (s *covQAStore) SearchNodesFTSWords(_ []string, _ int) ([]int64, error) {
	if err := s.gate("SearchNodesFTSWords", ""); err != nil {
		return nil, err
	}
	return nil, graphstore.ErrFTSUnsupported
}

func (s *covQAStore) CountNodesFTSWords(_ []string) (int, error) {
	if err := s.gate("CountNodesFTSWords", ""); err != nil {
		return 0, err
	}
	return 0, graphstore.ErrFTSUnsupported
}

func (s *covQAStore) Close() error { return nil }

// ── the seeded graph ─────────────────────────────────────────────────────────

// covQAPath is the graph's storage spelling of a repo-relative path: absolute
// and forward-slashed, which is what node identity is built from on every OS.
func covQAPath(root, rel string) string {
	return NormalizeFilePath(filepath.Join(root, rel))
}

// covQAGoGraph is the graph the pattern table queries. Every row exists to
// select one branch, and the comment beside it names which.
func covQAGoGraph(root string) ([]graphstore.GraphNode, []graphstore.GraphEdge) {
	a, b, c, d := covQAPath(root, "pkg/a.go"), covQAPath(root, "pkg/b.go"),
		covQAPath(root, "pkg/c.go"), covQAPath(root, "pkg/d.go")
	tf, routes := covQAPath(root, "pkg/a_test.go"), covQAPath(root, "app/routes.go")
	jav, yml := covQAPath(root, "app/App.java"), covQAPath(root, "app/app.yml")

	nodes := []graphstore.GraphNode{
		{Kind: "File", Name: a, QualifiedName: a, FilePath: a, Language: "go"},
		{Kind: "Function", Name: "Svc", QualifiedName: a + "::Svc", FilePath: a, Language: "go"},
		{Kind: "Class", Name: "Base", QualifiedName: a + "::Base", FilePath: a, Language: "go"},
		{Kind: "Class", Name: "Lonely", QualifiedName: a + "::Lonely", FilePath: a, Language: "go"},
		// A node whose language column is empty: the bare-name edge reader
		// applies no source-language filter for it, which is the only way an
		// edge reaches the fallback's own row lookup.
		{Kind: "Class", Name: "Blank", QualifiedName: a + "::Blank", FilePath: a},
		{Kind: "File", Name: b, QualifiedName: b, FilePath: b, Language: "go"},
		{Kind: "Function", Name: "Caller", QualifiedName: b + "::Caller", FilePath: b, Language: "go"},
		{Kind: "File", Name: c, QualifiedName: c, FilePath: c, Language: "go"},
		{Kind: "Class", Name: "Child", QualifiedName: c + "::Child", FilePath: c, Language: "go"},
		{Kind: "Class", Name: "Child2", QualifiedName: c + "::Child2", FilePath: c, Language: "go"},
		{Kind: "File", Name: d, QualifiedName: d, FilePath: d, Language: "go"},
		{Kind: "Function", Name: "Bare", QualifiedName: d + "::Bare", FilePath: d, Language: "go"},
		{Kind: "File", Name: tf, QualifiedName: tf, FilePath: tf, Language: "go"},
		{
			Kind: "Function", Name: "TestSvc", QualifiedName: tf + "::TestSvc",
			FilePath: tf, Language: "go", IsTest: true,
		},
		{
			Kind: "Function", Name: "TestCaller", QualifiedName: tf + "::TestCaller",
			FilePath: tf, Language: "go", IsTest: true,
		},
		// Matches the `Test<name>` convention search but is not a test, so
		// the naming-convention pass must reject it.
		{
			Kind: "Function", Name: "TestSvcHelper", QualifiedName: tf + "::TestSvcHelper",
			FilePath: tf, Language: "go",
		},
		{Kind: "File", Name: routes, QualifiedName: routes, FilePath: routes, Language: "go"},
		{
			Kind: "Endpoint", Name: "GET /x", QualifiedName: routes + "::GET /x",
			FilePath: routes, Language: "go",
		},
		{Kind: "File", Name: jav, QualifiedName: jav, FilePath: jav, Language: "java"},
		{Kind: "Class", Name: "Consumer", QualifiedName: jav + "::Consumer", FilePath: jav, Language: "java"},
		{
			Kind: "ConfigProperty", Name: "app.mail.host",
			QualifiedName: yml + "::app.mail.host", FilePath: yml, Language: "yaml",
		},
	}
	return nodes, covQAGoEdges(root)
}

// covQAGoEdges is covQAGoGraph's edge half, split out only for length.
//
// Every negative control here is targeted at a symbol NO tabled pattern
// queries. An edge's kind is not the only thing a pattern filters on, so an
// unwanted kind parked on a queried target is a real extra result somewhere:
// callers_of, for instance, filters incoming edges on kind alone and would
// report a File node as a caller.
func covQAGoEdges(root string) []graphstore.GraphEdge {
	a, b, c, d := covQAPath(root, "pkg/a.go"), covQAPath(root, "pkg/b.go"),
		covQAPath(root, "pkg/c.go"), covQAPath(root, "pkg/d.go")
	tf, routes, jav := covQAPath(root, "pkg/a_test.go"), covQAPath(root, "app/routes.go"),
		covQAPath(root, "app/App.java")
	svc, base, lone := a+"::Svc", a+"::Base", a+"::Lonely"
	caller, child, child2, bare := b+"::Caller", c+"::Child", c+"::Child2", d+"::Bare"
	tsvc, tcal, cons := tf+"::TestSvc", tf+"::TestCaller", jav+"::Consumer"
	gone, amb, unr, both := covQAPath(root, "pkg/z.go")+"::gone", d+"::amb", d+"::unr", d+"::both"

	incoming := []graphstore.GraphEdge{
		// callers_of / references_to / the four incoming event patterns are
		// all answered from one resolved node's incoming edges. "ghost" is a
		// source with no node row; the repeated edges pin de-duplication.
		{Kind: "CALLS", SourceQualified: caller, TargetQualified: svc, FilePath: b, Line: 11},
		{Kind: "CALLS", SourceQualified: caller, TargetQualified: svc, FilePath: b, Line: 12},
		{Kind: "CALLS", SourceQualified: "ghost", TargetQualified: svc, FilePath: b, Line: 13},
		{Kind: "REFERENCES", SourceQualified: caller, TargetQualified: svc, FilePath: b, Line: 14},
		{Kind: "REFERENCES", SourceQualified: caller, TargetQualified: svc, FilePath: b, Line: 15},
		{Kind: "REFERENCES", SourceQualified: "ghost", TargetQualified: svc, FilePath: b, Line: 16},
		{Kind: "TRIGGERS", SourceQualified: caller, TargetQualified: svc, FilePath: b, Line: 17},
		{Kind: "TRIGGERS", SourceQualified: "ghost", TargetQualified: svc, FilePath: b, Line: 18},
		{Kind: "PUBLISHES", SourceQualified: caller, TargetQualified: svc, FilePath: b, Line: 19},
		{Kind: "PUBLISHES", SourceQualified: "ghost", TargetQualified: svc, FilePath: b, Line: 20},
		{Kind: "HANDLES", SourceQualified: caller, TargetQualified: svc, FilePath: b, Line: 21},
		{Kind: "HANDLES", SourceQualified: "ghost", TargetQualified: svc, FilePath: b, Line: 22},

		// callers_of's bare-name second pass: a cross-file call keeps the
		// unqualified target name.
		{Kind: "CALLS", SourceQualified: bare, TargetQualified: "Svc", FilePath: d, Line: 3},
		{Kind: "CALLS", SourceQualified: bare, TargetQualified: "Svc", FilePath: d, Line: 4},
		{
			Kind: "CALLS", SourceQualified: caller, TargetQualified: "Svc", FilePath: b, Line: 5,
			Extra: map[string]any{"unresolved_targets": []any{"pkg/other.go::Svc"}},
		},
		{Kind: "CALLS", SourceQualified: "ghost", TargetQualified: "Svc", FilePath: d, Line: 6},
		{Kind: "INHERITS", SourceQualified: bare, TargetQualified: "Svc", FilePath: d, Line: 7},
	}

	outgoing := []graphstore.GraphEdge{
		{Kind: "CALLS", SourceQualified: svc, TargetQualified: caller, FilePath: a, Line: 31},
		{Kind: "CALLS", SourceQualified: svc, TargetQualified: caller, FilePath: a, Line: 32},
		{Kind: "REFERENCES", SourceQualified: svc, TargetQualified: base, FilePath: a, Line: 33},
		{Kind: "CALLS", SourceQualified: svc, TargetQualified: "helper", FilePath: a, Line: 34},
		{Kind: "CALLS", SourceQualified: svc, TargetQualified: gone, FilePath: a, Line: 35},
		{
			Kind: "CALLS", SourceQualified: svc, TargetQualified: amb, FilePath: a, Line: 36,
			Extra: map[string]any{
				"ambiguous_targets":           []any{"Alpha", "Beta"},
				"ambiguous_target_count":      float64(5),
				"ambiguous_targets_truncated": true,
			},
		},
		{
			Kind: "CALLS", SourceQualified: svc, TargetQualified: unr, FilePath: a, Line: 37,
			Extra: map[string]any{"unresolved_targets": []any{"Gamma"}},
		},
		{
			Kind: "CALLS", SourceQualified: svc, TargetQualified: both, FilePath: a, Line: 38,
			Extra: map[string]any{
				"ambiguous_targets":  []any{},
				"unresolved_targets": []any{"Delta"},
			},
		},
		{Kind: "TESTED_BY", SourceQualified: svc, TargetQualified: tsvc, FilePath: a, Line: 39},
		{Kind: "TESTED_BY", SourceQualified: svc, TargetQualified: "missingtest", FilePath: a, Line: 40},
		{Kind: "TRIGGERS", SourceQualified: svc, TargetQualified: caller, FilePath: a, Line: 41},
		{Kind: "TRIGGERS", SourceQualified: svc, TargetQualified: "nowhere", FilePath: a, Line: 42},
		{Kind: "HANDLES", SourceQualified: svc, TargetQualified: routes + "::GET /x", FilePath: a, Line: 43},
		{Kind: "HANDLES", SourceQualified: svc, TargetQualified: caller, FilePath: a, Line: 44},
		{Kind: "HANDLES", SourceQualified: svc, TargetQualified: "nowhere2", FilePath: a, Line: 45},
		// tests_for's one-hop CALLS pass reaches this from Svc via Caller.
		{Kind: "TESTED_BY", SourceQualified: caller, TargetQualified: tcal, FilePath: b, Line: 30},
	}

	fileLevel := []graphstore.GraphEdge{
		{Kind: "IMPORTS_FROM", SourceQualified: a, TargetQualified: "os", FilePath: a, Line: 3},
		{Kind: "IMPORTS_FROM", SourceQualified: a, TargetQualified: b, FilePath: a, Line: 4},
		{Kind: "CONTAINS", SourceQualified: a, TargetQualified: svc, FilePath: a, Line: 5},
		{Kind: "CONTAINS", SourceQualified: a, TargetQualified: base, FilePath: a, Line: 6},
		{Kind: "CONTAINS", SourceQualified: a, TargetQualified: lone, FilePath: a, Line: 7},
		{Kind: "CONTAINS", SourceQualified: a, TargetQualified: "gone", FilePath: a, Line: 8},
		// imports_of and children_of both filter this out by kind. It targets
		// Base, which no tabled pattern queries for callers.
		{Kind: "CALLS", SourceQualified: a, TargetQualified: base, FilePath: a, Line: 9},
		{Kind: "IMPORTS_FROM", SourceQualified: b, TargetQualified: a, FilePath: b, Line: 3},
		{Kind: "IMPORTS_FROM", SourceQualified: b, TargetQualified: a, FilePath: b, Line: 4},
		{Kind: "CALLS", SourceQualified: b, TargetQualified: a, FilePath: b, Line: 5},
	}

	structural := []graphstore.GraphEdge{
		// inheritors_of: resolved base names here, bare base names below.
		{Kind: "INHERITS", SourceQualified: child, TargetQualified: base, FilePath: c, Line: 3},
		{Kind: "IMPLEMENTS", SourceQualified: child2, TargetQualified: base, FilePath: c, Line: 4},
		{Kind: "CALLS", SourceQualified: caller, TargetQualified: base, FilePath: b, Line: 6},
		{Kind: "INHERITS", SourceQualified: "ghost", TargetQualified: base, FilePath: c, Line: 7},
		{Kind: "INHERITS", SourceQualified: child, TargetQualified: "Lonely", FilePath: c, Line: 8},
		{Kind: "IMPLEMENTS", SourceQualified: child2, TargetQualified: "Lonely", FilePath: c, Line: 9},
		// A bare CALLS edge here would make Caller a bare-name caller of
		// Lonely, which the empty-result cases query; REFERENCES is filtered
		// out by both patterns instead.
		{Kind: "REFERENCES", SourceQualified: caller, TargetQualified: "Lonely", FilePath: b, Line: 10},
		{Kind: "INHERITS", SourceQualified: "ghost", TargetQualified: "Lonely", FilePath: c, Line: 11},
		{Kind: "CALLS", SourceQualified: caller, TargetQualified: "Blank", FilePath: b, Line: 12},
		{Kind: "INHERITS", SourceQualified: child, TargetQualified: "Blank", FilePath: c, Line: 13},

		// consumers_of: the exact Spring key and one `prefix.*` ancestor.
		{Kind: "DEPENDS_ON_CONFIG", SourceQualified: cons, TargetQualified: "config:app.mail.host", FilePath: jav, Line: 3},
		{Kind: "DEPENDS_ON_CONFIG", SourceQualified: cons, TargetQualified: "config:app.mail.host", FilePath: jav, Line: 4},
		{Kind: "DEPENDS_ON_CONFIG", SourceQualified: "ghost", TargetQualified: "config:app.mail.host", FilePath: jav, Line: 5},
		{Kind: "CALLS", SourceQualified: cons, TargetQualified: "config:app.mail.host", FilePath: jav, Line: 6},
		{Kind: "DEPENDS_ON_CONFIG", SourceQualified: child, TargetQualified: "config:app.mail.*", FilePath: c, Line: 20},
	}

	edges := append(incoming, outgoing...)
	edges = append(edges, fileLevel...)
	return append(edges, structural...)
}

// covQAGoFiles are the fixture's source files. They exist on disk because the
// empty-result confidence marker stats a node's file to compare its mtime
// against the recorded build timestamp.
var covQAGoFiles = []string{
	"pkg/a.go", "pkg/b.go", "pkg/c.go", "pkg/d.go", "pkg/a_test.go",
	"app/routes.go", "app/App.java", "app/app.yml",
}

// covQARoot materializes the fixture's working tree.
func covQARoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, rel := range covQAGoFiles {
		writeExtra(t, root, rel, "// fixture\n")
	}
	return root
}

// covQAEnvAt seeds a fresh store over an existing working tree. Tests whose
// expectations name absolute paths share ONE root and re-seed per case, because
// t.TempDir() answers differently in a subtest than in its parent.
func covQAEnvAt(t *testing.T, root string) (*Engine, *covQAStore) {
	t.Helper()
	store := covQASeed(covQAGoGraph(root))
	return engineWithStore(t, root, store), store
}

func covQAEnv(t *testing.T) (*Engine, *covQAStore, string) {
	t.Helper()
	root := covQARoot(t)
	engine, store := covQAEnvAt(t, root)
	return engine, store, root
}

// ── invocation helpers ───────────────────────────────────────────────────────

// covQACall runs query_graph_tool with arguments bound through the pinned
// release's schema, so the handler sees every published default.
func covQACall(t *testing.T, e *Engine, arguments map[string]any) (any, error) {
	t.Helper()
	definition, ok := crgrelease.Lookup("query_graph_tool")
	if !ok {
		t.Fatalf("query_graph_tool is not published by code-review-graph %s", crgrelease.Version)
	}
	raw, err := json.Marshal(arguments)
	if err != nil {
		t.Fatalf("marshal arguments: %v", err)
	}
	bound, bindErr := definition.Bind(raw)
	if bindErr != nil {
		t.Fatalf("bind %s: %v", raw, bindErr)
	}
	return queryGraphTool(e, bound)
}

// covQAPayload runs a query that must succeed and returns its payload.
func covQAPayload(t *testing.T, e *Engine, arguments map[string]any) map[string]any {
	t.Helper()
	result, err := covQACall(t, e, arguments)
	if err != nil {
		t.Fatalf("query_graph_tool(%v): %v", arguments, err)
	}
	payload, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("handler returned %T, want an object", result)
	}
	return payload
}

// covQAArgs is `{"pattern": ..., "target": ...}` plus any overrides.
func covQAArgs(pattern, target string, extra map[string]any) map[string]any {
	args := map[string]any{"pattern": pattern, "target": target}
	for key, value := range extra {
		args[key] = value
	}
	return args
}

// covQAField projects one key out of every entry of a payload list.
func covQAField(t *testing.T, payload map[string]any, list, key string) []string {
	t.Helper()
	raw, ok := payload[list]
	if !ok {
		t.Fatalf("payload has no %q key: %v", list, payload)
	}
	items, ok := raw.([]map[string]any)
	if !ok {
		t.Fatalf("%s is %T, want a list of objects", list, raw)
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, fmt.Sprint(item[key]))
	}
	return out
}

func covQAEqualStrings(t *testing.T, label string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = %v (%d entries), want %v (%d)", label, got, len(got), want, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s[%d] = %q, want %q", label, i, got[i], want[i])
		}
	}
}

func covQAEqual(t *testing.T, label string, got, want any) {
	t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("%s = %v, want %v", label, got, want)
	}
}

// covQASummary is the release's result-count summary line.
func covQASummary(total int, pattern, target string) string {
	return fmt.Sprintf("Found %d result(s) for %s('%s')", total, pattern, target)
}

// ── the sixteen patterns ─────────────────────────────────────────────────────

// covQAPatternCase is one pattern invocation and the payload it must produce.
//
// resultKey names the field that identifies a result, because the projections
// differ by pattern: a node-shaped result is named by `name`, imports_of
// projects the raw edge target as `import_target`, and importers_of projects
// the importing file as `importer`.
type covQAPatternCase struct {
	name        string
	pattern     string
	target      string
	resultKey   string
	wantResults []string
	// wantEdges are the edge kinds, in order. They are deliberately NOT
	// one-per-result: the containment patterns pass no edge at all, and the
	// event patterns report an edge whose far endpoint has no node row with
	// no result beside it.
	wantEdges []string
}

// covQACallPatternCases are the patterns answered from CALLS/REFERENCES edges.
func covQACallPatternCases(root string) []covQAPatternCase {
	a, d := covQAPath(root, "pkg/a.go"), covQAPath(root, "pkg/d.go")
	return []covQAPatternCase{
		{
			name: "callers_of adds the bare-name pass", pattern: "callers_of",
			target: a + "::Svc", resultKey: "name",
			// Caller is found by the qualified scan and Bare only by the
			// bare-name pass; the duplicate edge, the edge carrying an
			// unresolved candidate set and the source with no node row all
			// contribute nothing.
			wantResults: []string{"Caller", "Bare"},
			wantEdges:   []string{"CALLS", "CALLS"},
		},
		{
			name: "references_to projects edge sources", pattern: "references_to",
			target: a + "::Svc", resultKey: "name",
			wantResults: []string{"Caller"}, wantEdges: []string{"REFERENCES"},
		},
		{
			name: "callees_of reports unresolved targets too", pattern: "callees_of",
			target: a + "::Svc", resultKey: "name",
			// `helper` is bare and amb/unr/both carry candidate sets, so all
			// four are reportable without a node row; `pkg/z.go::gone` is
			// qualified with no candidate set and is dropped.
			wantResults: []string{"Caller", "helper", d + "::amb", d + "::unr", d + "::both"},
			wantEdges:   []string{"CALLS", "CALLS", "CALLS", "CALLS", "CALLS"},
		},
		{
			name: "tests_for reports coverage then convention", pattern: "tests_for",
			target: a + "::Svc", resultKey: "name",
			// TestSvc is a direct TESTED_BY edge and TestCaller is reached one
			// CALLS hop away; TestSvcHelper matches the convention search but
			// is not a test. Coverage implies no edge, so `edges` is empty.
			wantResults: []string{"TestSvc", "TestCaller"}, wantEdges: nil,
		},
	}
}

// covQAFilePatternCases are the patterns keyed on a file or a container.
func covQAFilePatternCases(root string) []covQAPatternCase {
	a, b := covQAPath(root, "pkg/a.go"), covQAPath(root, "pkg/b.go")
	return []covQAPatternCase{
		{
			name: "imports_of names the raw edge target", pattern: "imports_of",
			target: a, resultKey: "import_target",
			// `os` resolves to no node at all, so naming the target is the
			// only honest projection.
			wantResults: []string{"os", b}, wantEdges: []string{"IMPORTS_FROM", "IMPORTS_FROM"},
		},
		{
			name: "importers_of names the importing file", pattern: "importers_of",
			target: a, resultKey: "importer",
			wantResults: []string{b}, wantEdges: []string{"IMPORTS_FROM"},
		},
		{
			name: "children_of passes no edge", pattern: "children_of",
			target: a, resultKey: "name",
			wantResults: []string{"Svc", "Base", "Lonely"}, wantEdges: nil,
		},
		{
			name: "file_summary lists every node of the file", pattern: "file_summary",
			target: "pkg/a.go", resultKey: "name",
			wantResults: []string{a, "Svc", "Base", "Lonely", "Blank"}, wantEdges: nil,
		},
		{
			name: "inheritors_of walks resolved base names", pattern: "inheritors_of",
			target: a + "::Base", resultKey: "name",
			wantResults: []string{"Child", "Child2"}, wantEdges: []string{"INHERITS", "IMPLEMENTS"},
		},
		{
			name: "inheritors_of falls back to the bare base name", pattern: "inheritors_of",
			target: a + "::Lonely", resultKey: "name",
			wantResults: []string{"Child", "Child2"}, wantEdges: []string{"INHERITS", "IMPLEMENTS"},
		},
	}
}

// covQAEventPatternCases are the endpoint/event/config patterns. Each reports
// an edge whose far endpoint has no node row with no result beside it — the
// relationship is real even though there is nothing to project.
func covQAEventPatternCases(root string) []covQAPatternCase {
	svc := covQAPath(root, "pkg/a.go") + "::Svc"
	return []covQAPatternCase{
		{
			name: "triggers_of", pattern: "triggers_of", target: svc, resultKey: "name",
			wantResults: []string{"Caller"}, wantEdges: []string{"TRIGGERS", "TRIGGERS"},
		},
		{
			name: "triggered_by", pattern: "triggered_by", target: svc, resultKey: "name",
			wantResults: []string{"Caller"}, wantEdges: []string{"TRIGGERS", "TRIGGERS"},
		},
		{
			name: "publishers_of", pattern: "publishers_of", target: svc, resultKey: "name",
			wantResults: []string{"Caller"}, wantEdges: []string{"PUBLISHES", "PUBLISHES"},
		},
		{
			name: "listeners_of", pattern: "listeners_of", target: svc, resultKey: "name",
			wantResults: []string{"Caller"}, wantEdges: []string{"HANDLES", "HANDLES"},
		},
		{
			name: "handlers_of", pattern: "handlers_of", target: svc, resultKey: "name",
			wantResults: []string{"Caller"}, wantEdges: []string{"HANDLES", "HANDLES"},
		},
		{
			name: "endpoints_for keeps only Endpoint targets", pattern: "endpoints_for",
			target: svc, resultKey: "name",
			// The HANDLES edge pointing at a Function is dropped entirely —
			// neither a result nor an edge.
			wantResults: []string{"GET /x"}, wantEdges: []string{"HANDLES", "HANDLES"},
		},
		{
			name:    "consumers_of matches the key and its prefix ancestors",
			pattern: "consumers_of", target: "app.mail.host", resultKey: "name",
			wantResults: []string{"Consumer", "Child"},
			wantEdges: []string{
				"DEPENDS_ON_CONFIG", "DEPENDS_ON_CONFIG", "DEPENDS_ON_CONFIG",
			},
		},
	}
}

func TestCovQAQueryGraphAnswersEveryPattern(t *testing.T) {
	engine, _, root := covQAEnv(t)
	cases := covQACallPatternCases(root)
	cases = append(cases, covQAFilePatternCases(root)...)
	cases = append(cases, covQAEventPatternCases(root)...)
	covered := map[string]bool{}
	for _, tc := range cases {
		covered[tc.pattern] = true
		t.Run(tc.name, func(t *testing.T) {
			covQAAssertPattern(t, engine, tc)
		})
	}
	// A pattern the table forgets would be a silently unverified dispatch arm.
	for _, pattern := range queryPatternOrder {
		if !covered[pattern] {
			t.Errorf("published pattern %q has no case in this table", pattern)
		}
	}
}

func covQAAssertPattern(t *testing.T, engine *Engine, tc covQAPatternCase) {
	t.Helper()
	payload := covQAPayload(t, engine, covQAArgs(tc.pattern, tc.target, nil))
	covQAEqual(t, "status", payload["status"], "ok")
	covQAEqual(t, "pattern", payload["pattern"], tc.pattern)
	covQAEqual(t, "description", payload["description"], queryPatterns[tc.pattern])
	covQAEqual(t, "result_count", payload["result_count"], len(tc.wantResults))
	covQAEqual(t, "results_omitted", payload["results_omitted"], 0)
	covQAEqual(t, "summary", payload["summary"],
		covQASummary(len(tc.wantResults), tc.pattern, fmt.Sprint(payload["target"])))
	covQAEqualStrings(t, "results",
		covQAField(t, payload, "results", tc.resultKey), tc.wantResults)
	covQAEqualStrings(t, "edges", covQAField(t, payload, "edges", "kind"), tc.wantEdges)
	// A non-empty answer must never carry an empty-result advisory.
	if note, ok := payload["confidence"]; ok && len(tc.wantResults) > 0 {
		t.Errorf("confidence = %v on a non-empty result set", note)
	}
}

// TestCovQACalleesOfLabelsCandidateSets pins the candidate-set projection for a
// call whose target has no node row.
//
// The subtle rule is which list wins: the release selects with
// `ambiguous or unresolved`, so an ambiguous key that is PRESENT BUT EMPTY is
// falsy and hands over to the unresolved list — label included. Choosing on key
// presence instead would label that edge "ambiguous" with no candidates.
func TestCovQACalleesOfLabelsCandidateSets(t *testing.T) {
	engine, _, root := covQAEnv(t)
	d := covQAPath(root, "pkg/d.go")
	payload := covQAPayload(t, engine,
		covQAArgs("callees_of", covQAPath(root, "pkg/a.go")+"::Svc", nil))
	results, ok := payload["results"].([]map[string]any)
	if !ok {
		t.Fatalf("results is %T, want a list of objects", payload["results"])
	}
	byName := map[string]map[string]any{}
	for _, result := range results {
		byName[fmt.Sprint(result["name"])] = result
	}

	for _, tc := range []struct {
		target     string
		resolution any
		candidates []string
		count      int
		truncated  bool
	}{
		{
			target: d + "::amb", resolution: "ambiguous",
			candidates: []string{"Alpha", "Beta"}, count: 5, truncated: true,
		},
		{target: d + "::unr", resolution: "unresolved", candidates: []string{"Gamma"}, count: 1},
		{target: d + "::both", resolution: "unresolved", candidates: []string{"Delta"}, count: 1},
		// A bare target carries no candidate set at all, so it must stay
		// unlabelled rather than gain an empty one.
		{target: "helper", resolution: nil},
	} {
		t.Run(tc.target, func(t *testing.T) {
			result, found := byName[tc.target]
			if !found {
				t.Fatalf("callees_of omitted %q; reported %v", tc.target, byName)
			}
			covQAEqual(t, "resolution", result["resolution"], tc.resolution)
			if tc.resolution == nil {
				covQAEqual(t, "candidates", result["candidates"], nil)
				return
			}
			covQAEqualStrings(t, "candidates", covQAStringsOf(t, result["candidates"]), tc.candidates)
			covQAEqual(t, "candidate_count", result["candidate_count"], tc.count)
			covQAEqual(t, "candidates_truncated", result["candidates_truncated"], tc.truncated)
		})
	}
}

func covQAStringsOf(t *testing.T, value any) []string {
	t.Helper()
	items, ok := value.([]string)
	if !ok {
		t.Fatalf("candidates is %T, want []string", value)
	}
	return items
}

// TestCovQAImportersOfFollowsCSharpNamespaces pins #310: a C# `using X.Y;`
// records an IMPORTS_FROM edge whose target is the namespace TEXT rather than a
// file path, so the file-path lookup alone reports no importers for a file that
// has them. Resolving the file's declared namespaces and searching by those too
// is the fix, and the two passes must merge without reporting an importer that
// both of them found twice.
func TestCovQAImportersOfFollowsCSharpNamespaces(t *testing.T) {
	root := t.TempDir()
	store := covQASeed(covQACSharpGraph(root))
	engine := engineWithStore(t, root, store)

	payload := covQAPayload(t, engine, covQAArgs("importers_of", covQAPath(root, "src/x.cs"), nil))
	covQAEqualStrings(t, "results", covQAField(t, payload, "results", "importer"),
		[]string{covQAPath(root, "src/y.cs"), covQAPath(root, "src/z.cs")})
	covQAEqual(t, "result_count", payload["result_count"], 2)
}

// covQACSharpGraph is a C# file imported once by path and once by namespace.
func covQACSharpGraph(root string) ([]graphstore.GraphNode, []graphstore.GraphEdge) {
	x, y, z := covQAPath(root, "src/x.cs"), covQAPath(root, "src/y.cs"), covQAPath(root, "src/z.cs")
	return []graphstore.GraphNode{
			// Declared before the File node so the namespace scan has a
			// non-File row of the same file to skip.
			{Kind: "Method", Name: "Helper", QualifiedName: x + "::Helper", FilePath: x, Language: "csharp"},
			{
				Kind: "File", Name: x, QualifiedName: x, FilePath: x, Language: "csharp",
				Extra: map[string]any{"csharp_namespaces": []any{"My.Ns", "Empty.Ns"}},
			},
			{Kind: "File", Name: y, QualifiedName: y, FilePath: y, Language: "csharp"},
			{Kind: "File", Name: z, QualifiedName: z, FilePath: z, Language: "csharp"},
		}, []graphstore.GraphEdge{
			{Kind: "IMPORTS_FROM", SourceQualified: y, TargetQualified: x, FilePath: y, Line: 1},
			{Kind: "IMPORTS_FROM", SourceQualified: y, TargetQualified: "My.Ns", FilePath: y, Line: 2},
			{Kind: "IMPORTS_FROM", SourceQualified: z, TargetQualified: "My.Ns", FilePath: z, Line: 1},
		}
}

// ── target resolution ────────────────────────────────────────────────────────

// TestCovQAResolvesTargetsByJavaFQN pins the Java-FQN resolution path, which
// resolves `pkg.Class.method` from language plus class/file evidence.
//
// The negative direction is the load-bearing one: a Java-shaped target that
// matches nothing yields NO candidates rather than falling through to the
// keyword search, because a globally unique method name is not evidence that it
// is the one the package-qualified target names.
func TestCovQAResolvesTargetsByJavaFQN(t *testing.T) {
	root := t.TempDir()
	engine := engineWithStore(t, root, covQASeed(covQAJavaGraph(root)))

	t.Run("one evidence-backed match resolves and rewrites the target", func(t *testing.T) {
		payload := covQAPayload(t, engine, covQAArgs("callers_of", "com.example.Only.run", nil))
		covQAEqual(t, "status", payload["status"], "ok")
		// The echoed target is the resolved qualified name, not the input, so
		// the summary names the node that was actually queried.
		covQAEqual(t, "target", payload["target"], covQAPath(root, "java/Only.java")+"::run")
		covQAEqualStrings(t, "results",
			covQAField(t, payload, "results", "name"), []string{"lonelyMethod"})
	})

	t.Run("several matches disambiguate over the evidence set", func(t *testing.T) {
		payload := covQAPayload(t, engine, covQAArgs("callers_of", "com.example.Service.handle", nil))
		covQAEqual(t, "status", payload["status"], "ambiguous")
		// A Java FQN's candidate list IS the complete evidence-backed set, so
		// its count is the list length and it is never truncated — unlike a
		// keyword search, whose list is capped at twenty and needs a separate
		// unbounded count.
		covQAEqual(t, "candidate_count", payload["candidate_count"], 3)
		covQAEqual(t, "candidates_truncated", payload["candidates_truncated"], false)
		covQAEqualStrings(t, "candidates",
			covQAField(t, payload, "candidates", "qualified_name"), []string{
				covQAPath(root, "java/S.java") + "::handle",
				covQAPath(root, "java/Service.java") + "::handle",
				covQAPath(root, "java/Z.java") + "::Service.handle",
			})
		covQAEqual(t, "disambiguation", fmt.Sprint(payload["disambiguation"]),
			fmt.Sprint(payload["candidates"]))
	})

	t.Run("no evidence-backed match refuses the keyword fallback", func(t *testing.T) {
		// `lonelyMethod` is globally unique, so a keyword search WOULD find
		// it; the Java path must not accept it for a class it does not live in.
		payload := covQAPayload(t, engine, covQAArgs("callers_of", "com.example.Nope.lonelyMethod", nil))
		covQAEqual(t, "status", payload["status"], "not_found")
		covQAEqual(t, "summary", payload["summary"],
			"No node found matching 'com.example.Nope.lonelyMethod'.")
	})
}

// covQAJavaGraph holds one method name spread across several classes, plus the
// rows each evidence rule must accept or reject.
func covQAJavaGraph(root string) ([]graphstore.GraphNode, []graphstore.GraphEdge) {
	s, svc := covQAPath(root, "java/S.java"), covQAPath(root, "java/Service.java")
	z, q := covQAPath(root, "java/Z.java"), covQAPath(root, "java/Q.java")
	only, other := covQAPath(root, "java/Only.java"), covQAPath(root, "java/Other.java")
	return []graphstore.GraphNode{
			// Accepted on the parent class name.
			{
				Kind: "Method", Name: "handle", QualifiedName: s + "::handle", FilePath: s,
				Language: "Java", ParentName: "com.example.Service",
			},
			// Accepted on the file stem.
			{
				Kind: "Method", Name: "handle", QualifiedName: svc + "::handle", FilePath: svc,
				Language: "java", ParentName: "Other",
			},
			// Accepted on the qualified-name suffix.
			{
				Kind: "Method", Name: "handle", QualifiedName: z + "::Service.handle", FilePath: z,
				Language: "java", ParentName: "Nope",
			},
			// Rejected: no class evidence of any kind.
			{
				Kind: "Method", Name: "handle", QualifiedName: q + "::handle", FilePath: q,
				Language: "java", ParentName: "Nope",
			},
			// Rejected: right name and right class, wrong language.
			{
				Kind: "Method", Name: "handle", QualifiedName: other + "::handle", FilePath: other,
				Language: "go", ParentName: "com.example.Service",
			},
			// Rejected: the keyword search matches it as a substring, but the
			// exact method name does not.
			{
				Kind: "Method", Name: "handleAll", QualifiedName: other + "::handleAll", FilePath: other,
				Language: "java", ParentName: "com.example.Service",
			},
			{
				Kind: "Method", Name: "run", QualifiedName: only + "::run", FilePath: only,
				Language: "java", ParentName: "com.example.Only",
			},
			{
				Kind: "Method", Name: "lonelyMethod", QualifiedName: other + "::lonelyMethod",
				FilePath: other, Language: "java", ParentName: "com.example.Other",
			},
		}, []graphstore.GraphEdge{
			{
				Kind: "CALLS", SourceQualified: other + "::lonelyMethod",
				TargetQualified: only + "::run", FilePath: other, Line: 9,
			},
		}
}

// TestCovQADisambiguatesKeywordMatches pins the keyword-search branch of target
// resolution: an agent re-runs with a qualified name from the list, so the
// response has to say how many nodes matched and whether the list it shows is
// the whole set.
func TestCovQADisambiguatesKeywordMatches(t *testing.T) {
	engine, _, root := covQAEnv(t)
	c := covQAPath(root, "pkg/c.go")
	payload := covQAPayload(t, engine, covQAArgs("callers_of", "Child", nil))
	covQAEqual(t, "status", payload["status"], "ambiguous")
	covQAEqual(t, "summary", payload["summary"],
		"'Child' matches 2 node(s). Re-run with a qualified_name from disambiguation.")
	covQAEqual(t, "candidate_count", payload["candidate_count"], 2)
	covQAEqual(t, "candidates_truncated", payload["candidates_truncated"], false)
	covQAEqual(t, "hint", payload["hint"],
		"Use a qualified_name from disambiguation as the target parameter.")
	covQAEqualStrings(t, "candidates", covQAField(t, payload, "candidates", "qualified_name"),
		[]string{c + "::Child", c + "::Child2"})
}

// TestCovQARankDisambiguationCandidatesOrdersByMatchQuality pins the
// disambiguation ordering, which is the whole value of the response: an agent
// re-runs with the FIRST qualified name, so putting a substring match ahead of
// an exact one sends it to the wrong node.
func TestCovQARankDisambiguationCandidatesOrdersByMatchQuality(t *testing.T) {
	candidates := []graphstore.GraphNode{
		// Neither the name nor the qualified name relates to the target.
		{Name: "zzz", QualifiedName: "pkg/z.go::zzz"},
		// The qualified name contains the target, case-insensitively.
		{Name: "doLogin", QualifiedName: "pkg/b.go::doLOGINnow"},
		// Exact bare name.
		{Name: "Login", QualifiedName: "pkg/a.go::Login"},
		// Exact qualified name.
		{Name: "other", QualifiedName: "Login"},
		// Also a substring match, so the qualified-name tie-break decides.
		{Name: "alsoLogin", QualifiedName: "pkg/a.go::Login2"},
	}
	ranked := rankDisambiguationCandidates(candidates, "Login")
	got := make([]string, 0, len(ranked))
	for _, candidate := range ranked {
		got = append(got, fmt.Sprint(candidate["qualified_name"]))
	}
	covQAEqualStrings(t, "ranked", got, []string{
		"Login",
		"pkg/a.go::Login",
		"pkg/a.go::Login2",
		"pkg/b.go::doLOGINnow",
		"pkg/z.go::zzz",
	})
	// Ranking must not reorder the caller's own slice.
	covQAEqual(t, "candidates[0] after ranking", candidates[0].QualifiedName, "pkg/z.go::zzz")
}

// ── empty-result confidence ──────────────────────────────────────────────────

// TestCovQAEmptyResultConfidence pins the advisory attached to a zero.
//
// A zero is the dangerous direction: an agent reads "0 callers" as "none exist"
// and either concludes wrongly or falls back to grepping the repository. The
// outcomes below must stay distinguishable — nothing is indexed, this target is
// not indexed, the graph is stale, or the absence is real.
func TestCovQAEmptyResultConfidence(t *testing.T) {
	t.Run("an unbuilt graph says nothing is indexed", func(t *testing.T) {
		// A directory with no database: every read degrades to an empty graph
		// rather than an error, including for the two patterns that tolerate
		// an unresolved target and therefore reach their pattern body.
		engine := Open(t.TempDir())
		t.Cleanup(func() { _ = engine.Close() })
		for _, pattern := range []string{"callers_of", "file_summary", "consumers_of"} {
			payload := covQAPayload(t, engine, covQAArgs(pattern, "whatever", nil))
			covQAEqual(t, pattern+" confidence", payload["confidence"],
				ConfidenceNote(emptyGraphNote))
		}
	})

	t.Run("an empty graph says nothing is indexed", func(t *testing.T) {
		engine := engineWithStore(t, t.TempDir(), covQASeed(nil, nil))
		payload := covQAPayload(t, engine, covQAArgs("callers_of", "Nope", nil))
		covQAEqual(t, "status", payload["status"], "not_found")
		covQAEqual(t, "confidence", payload["confidence"], ConfidenceNote(emptyGraphNote))
	})

	t.Run("a populated graph names the unindexed target", func(t *testing.T) {
		engine, _, _ := covQAEnv(t)
		payload := covQAPayload(t, engine, covQAArgs("callers_of", "nosuchsymbol", nil))
		covQAEqual(t, "confidence", payload["confidence"],
			ConfidenceNote(NotIndexedNote("nosuchsymbol")))
	})

	t.Run("a failed stats read drops the advisory, not the answer", func(t *testing.T) {
		engine, store, _ := covQAEnv(t)
		store.fail["GetStats"] = errFake
		payload := covQAPayload(t, engine, covQAArgs("callers_of", "nosuchsymbol", nil))
		covQAEqual(t, "status", payload["status"], "not_found")
		if note, ok := payload["confidence"]; ok {
			t.Errorf("confidence = %v; an unreadable advisory must be omitted", note)
		}
	})

	t.Run("a graph built at another commit explains the miss", func(t *testing.T) {
		engine, store, root := covQAEnv(t)
		covQAInitGitRepo(t, root)
		// A build sha that is not HEAD is independent proof of staleness, and
		// it outranks the flat "not indexed" wording because it has a remedy.
		store.meta["git_head_sha"] = "0000000000000000000000000000000000000000"
		payload := covQAPayload(t, engine, covQAArgs("callers_of", "nosuchsymbol", nil))
		covQAEqual(t, "confidence", payload["confidence"],
			ConfidenceNote(UnresolvedStaleNote("nosuchsymbol")))
	})

	t.Run("a file changed after the build reports staleness", func(t *testing.T) {
		engine, store, root := covQAEnv(t)
		// A commit match says nothing about uncommitted edits, so the file's
		// mtime is an independent staleness signal.
		store.meta["last_updated"] = "2000-01-01T00:00:00"
		payload := covQAPayload(t, engine,
			covQAArgs("callers_of", covQAPath(root, "pkg/a.go")+"::Lonely", nil))
		covQAEqual(t, "result_count", payload["result_count"], 0)
		covQAEqual(t, "confidence", payload["confidence"],
			ConfidenceNote("graph is stale: a.go changed after the last build; "+updateHint))
	})

	t.Run("an indexed target with unverifiable currency hedges", func(t *testing.T) {
		engine, _, root := covQAEnv(t)
		target := covQAPath(root, "pkg/a.go") + "::Lonely"
		// detail_level minimal drops `edges` and projects each result, but the
		// advisory rides along unchanged.
		payload := covQAPayload(t, engine,
			covQAArgs("callers_of", target, map[string]any{"detail_level": "minimal"}))
		covQAEqual(t, "result_count", payload["result_count"], 0)
		if _, ok := payload["edges"]; ok {
			t.Errorf("minimal detail must not carry edges: %v", payload)
		}
		// The graph carries no build metadata, so currency could not be
		// checked and the marker must not claim that it was. The qualified
		// name does not fit the marker's budget, and the half that gets kept
		// is the SYMBOL rather than the directory prefix.
		covQAEqual(t, "confidence", payload["confidence"],
			"'Lonely' is indexed and no such edge is recorded; graph currency unverified")
		covQAEqual(t, "confidence", payload["confidence"],
			ConfidenceNote(confirmedAbsenceNote(target, false)))
	})
}

// covQAInitGitRepo makes root a repository with one commit, so the staleness
// check has a live HEAD to compare the recorded build sha against.
//
// Automatic maintenance is disabled before the commit: `git commit` otherwise
// spawns a detached `git maintenance run`, which keeps mutating .git after the
// commit returns and races the temp directory's own cleanup.
func covQAInitGitRepo(t *testing.T, root string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "commit.gpgsign", "false"},
		{"config", "maintenance.auto", "false"},
		{"config", "gc.auto", "0"},
		{"add", "-A"},
		{"commit", "-q", "-m", "fixture: initial"},
	} {
		runFixtureGit(t, root, args...)
	}
}

// ── bounding ─────────────────────────────────────────────────────────────────

// TestCovQABoundedResultsKeepEdgesAligned pins query_graph's streaming bound.
//
// It counts every logical result but appends only the bounded prefix, and it
// records an edge only alongside a RETAINED result — so `edges` describes the
// visible `results` rather than the total. Routing this through the shared
// Bounded helper would emit edges for results the caller cannot see, and would
// also produce ShownOf's summary fragment instead of this one.
func TestCovQABoundedResultsKeepEdgesAligned(t *testing.T) {
	engine, _, root := covQAEnv(t)
	payload := covQAPayload(t, engine, covQAArgs(
		"callers_of", covQAPath(root, "pkg/a.go")+"::Svc", map[string]any{"max_results": 1}))
	covQAEqual(t, "result_count", payload["result_count"], 2)
	covQAEqual(t, "results_omitted", payload["results_omitted"], 1)
	covQAEqualStrings(t, "results", covQAField(t, payload, "results", "name"), []string{"Caller"})
	covQAEqualStrings(t, "edges", covQAField(t, payload, "edges", "kind"), []string{"CALLS"})
	covQAEqual(t, "summary", payload["summary"],
		covQASummary(2, "callers_of", fmt.Sprint(payload["target"]))+" — showing 1, 1 omitted")
}

// TestCovQABoundIsRejectedBeforeTheStoreOpens pins that a non-positive bound is
// a transport error rather than an in-band error payload: the release validates
// it outside its try block, so the ValueError escapes to the MCP layer.
func TestCovQABoundIsRejectedBeforeTheStoreOpens(t *testing.T) {
	engine, store, root := covQAEnv(t)
	_, err := covQACall(t, engine,
		covQAArgs("callers_of", covQAPath(root, "pkg/a.go")+"::Svc", map[string]any{"max_results": 0}))
	if err == nil {
		t.Fatal("max_results 0 must be rejected as an error, not answered")
	}
	if len(store.seen) != 0 {
		t.Errorf("the graph was read before the bound was validated: %v", store.seen)
	}
}

// TestCovQAUnknownPatternIsAnInBandError pins the shape of the unknown-pattern
// answer: a bare status/error dict with no `summary`, echoing the published
// pattern names in their DECLARATION order, because a Python dict renders in
// insertion order and that string is what clients see.
func TestCovQAUnknownPatternIsAnInBandError(t *testing.T) {
	engine, store, _ := covQAEnv(t)
	payload := covQAPayload(t, engine, covQAArgs("no_such_pattern", "Login", nil))
	covQAEqual(t, "status", payload["status"], "error")
	covQAEqual(t, "error", payload["error"],
		"Unknown pattern 'no_such_pattern'. Available: "+pythonStringListRepr(queryPatternOrder))
	if _, ok := payload["summary"]; ok {
		t.Errorf("the unknown-pattern dict must carry no summary: %v", payload)
	}
	if len(store.seen) != 0 {
		t.Errorf("an unknown pattern must not reach the graph: %v", store.seen)
	}
}

// TestCovQAPayloadNeverReportsNegativeOmitted pins the omitted-count floor.
// `results_omitted` is a count of hidden results, and a negative one would read
// to a client as a nonsensical total.
func TestCovQAPayloadNeverReportsNegativeOmitted(t *testing.T) {
	q := &graphQuery{
		pattern: "callers_of",
		target:  "pkg/a.go::Svc",
		results: []map[string]any{{"name": "Caller"}},
	}
	payload := q.payload()
	covQAEqual(t, "result_count", payload["result_count"], 0)
	covQAEqual(t, "results_omitted", payload["results_omitted"], 0)
}

// ── dispatch ─────────────────────────────────────────────────────────────────

// TestCovQADispatchAnswersOnlyPublishedPatterns pins the dispatch table against
// the published pattern list: every published pattern must reach the graph, and
// a pattern that is not published must be a silent no-op rather than an error —
// the tool-level gate rejects it first, so dispatch is never asked for one.
//
// The query is built the way run() builds it for a resolved target: a File node
// satisfies every arm, because importers_of reads the resolved node's path and
// consumers_of reads its name.
func TestCovQADispatchAnswersOnlyPublishedPatterns(t *testing.T) {
	engine, _, root := covQAEnv(t)
	file := covQAPath(root, "pkg/a.go")
	for _, pattern := range append(queryPatternOrder, "not_a_published_pattern") {
		t.Run(pattern, func(t *testing.T) {
			store := covQASeed(covQAGoGraph(root))
			node, err := store.GetNode(file)
			if err != nil || node == nil {
				t.Fatalf("GetNode(%s) = %v, %v", file, node, err)
			}
			store.forget()
			q := &graphQuery{
				engine: engine, store: store, pattern: pattern,
				target: file, node: node, limit: 100,
			}
			if err := q.dispatch(); err != nil {
				t.Fatalf("dispatch(%s): %v", pattern, err)
			}
			published := queryPatterns[pattern] != ""
			if read := len(store.seen) > 0; read != published {
				t.Errorf("dispatch(%s) read the graph = %t, want %t", pattern, read, published)
			}
		})
	}
}

// TestCovQADispatchIsSkippedWithoutAStore pins that an unbuilt graph degrades to
// an empty OK answer for the two patterns whose targets are not node names and
// which therefore reach their pattern body with nothing resolved.
func TestCovQADispatchIsSkippedWithoutAStore(t *testing.T) {
	engine := Open(t.TempDir())
	t.Cleanup(func() { _ = engine.Close() })
	for _, tc := range []struct{ pattern, target string }{
		{"file_summary", "pkg/a.go"},
		{"consumers_of", "app.mail.host"},
	} {
		payload := covQAPayload(t, engine, covQAArgs(tc.pattern, tc.target, nil))
		covQAEqual(t, tc.pattern+" status", payload["status"], "ok")
		covQAEqual(t, tc.pattern+" result_count", payload["result_count"], 0)
	}
}

// TestCovQAConsumersOfCanonicalizesTheTarget pins the three target spellings
// `consumers_of` accepts. The graph stores one canonical `config:<key>` edge
// target, so a spelling that is not folded onto it looks up a key no edge
// references and reports "no consumers" for a property that has them.
func TestCovQAConsumersOfCanonicalizesTheTarget(t *testing.T) {
	engine, _, root := covQAEnv(t)
	for _, tc := range []struct {
		name    string
		target  string
		results []string
	}{
		{
			name: "the config: prefix is stripped", target: "config:app.mail.host",
			results: []string{"Consumer", "Child"},
		},
		{
			// `app.mail.*` names the prefix itself, so only the class bound to
			// the whole prefix consumes it.
			name: "a wildcard suffix is trimmed", target: "app.mail.*",
			results: []string{"Child"},
		},
		{
			name:    "a resolved ConfigProperty node names the key",
			target:  covQAPath(root, "app/app.yml") + "::app.mail.host",
			results: []string{"Consumer", "Child"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := covQAPayload(t, engine, covQAArgs("consumers_of", tc.target, nil))
			covQAEqualStrings(t, "results", covQAField(t, payload, "results", "name"), tc.results)
		})
	}
}

// ── language carve-outs ──────────────────────────────────────────────────────

// TestCovQABuiltinCallTargetsAreRefused pins the reverse-call carve-out: "who
// calls .map()?" is hundreds of useless hits and never the question the caller
// meant, so callers_of answers zero for a bare builtin name WITHOUT resolving
// it. The carve-out is scoped twice over — a qualified `utils.py::map` is a real
// symbol, and forward tracing still reports the builtins.
func TestCovQABuiltinCallTargetsAreRefused(t *testing.T) {
	engine, store, _ := covQAEnv(t)
	payload := covQAPayload(t, engine, covQAArgs("callers_of", "map", nil))
	covQAEqual(t, "status", payload["status"], "ok")
	covQAEqual(t, "result_count", payload["result_count"], 0)
	covQAEqual(t, "results_omitted", payload["results_omitted"], 0)
	covQAEqual(t, "summary", payload["summary"],
		"'map' is a common builtin — callers_of skipped to avoid noise.")
	if len(store.seen) != 0 {
		t.Errorf("a builtin target must be refused before resolution: %v", store.seen)
	}

	store.forget()
	covQAPayload(t, engine, covQAArgs("callers_of", "utils.py::map", nil))
	if !store.consulted("GetNode:utils.py::map") {
		t.Error("a qualified builtin name is a real symbol and must be resolved")
	}

	store.forget()
	covQAPayload(t, engine, covQAArgs("callees_of", "map", nil))
	if !store.consulted("GetNode:map") {
		t.Error("forward tracing must not inherit the reverse-tracing carve-out")
	}
}

// TestCovQACppOverloadsSuppressBareAttribution pins the C++ carve-out. An
// overload set deliberately keeps its call targets bare, and the candidates
// support disambiguation without proving that any one overload was called — so
// an ambiguous set must contribute no callers and no inferred test rather than
// attributing either to an arbitrary overload.
func TestCovQACppOverloadsSuppressBareAttribution(t *testing.T) {
	root := t.TempDir()
	engine := engineWithStore(t, root, covQASeed(covQACppGraph(root)))
	o := covQAPath(root, "src/o.cpp")

	t.Run("an overloaded name drops every bare caller", func(t *testing.T) {
		payload := covQAPayload(t, engine, covQAArgs("callers_of", o+"::Over1", nil))
		covQAEqualStrings(t, "results",
			covQAField(t, payload, "results", "name"), []string{"Qualified"})
	})

	t.Run("a unique name keeps bare callers but drops method calls", func(t *testing.T) {
		// A receiver at the call site means `obj.Single()`, which is not
		// provably this free function.
		payload := covQAPayload(t, engine, covQAArgs("callers_of", o+"::Single", nil))
		covQAEqualStrings(t, "results",
			covQAField(t, payload, "results", "name"), []string{"CppCaller"})
		covQAEqualStrings(t, "target_resolution",
			covQAField(t, payload, "results", "target_resolution"), []string{"unresolved"})
	})

	t.Run("an overloaded name suppresses the naming-convention guess", func(t *testing.T) {
		payload := covQAPayload(t, engine, covQAArgs("tests_for", o+"::Over1", nil))
		covQAEqual(t, "result_count", payload["result_count"], 0)
	})

	t.Run("a unique name still allows the guess", func(t *testing.T) {
		payload := covQAPayload(t, engine, covQAArgs("tests_for", o+"::Single", nil))
		covQAEqualStrings(t, "results",
			covQAField(t, payload, "results", "name"), []string{"TestSingle"})
		covQAEqualStrings(t, "inferred_by",
			covQAField(t, payload, "results", "inferred_by"), []string{"naming_convention"})
	})
}

// covQACppGraph holds one overloaded function name and one unique one.
func covQACppGraph(root string) ([]graphstore.GraphNode, []graphstore.GraphEdge) {
	o, tf := covQAPath(root, "src/o.cpp"), covQAPath(root, "src/o_test.cpp")
	return []graphstore.GraphNode{
			{Kind: "Function", Name: "Over", QualifiedName: o + "::Over1", FilePath: o, Language: "cpp"},
			{Kind: "Function", Name: "Over", QualifiedName: o + "::Over2", FilePath: o, Language: "cpp"},
			{Kind: "Function", Name: "Single", QualifiedName: o + "::Single", FilePath: o, Language: "cpp"},
			{Kind: "Function", Name: "CppCaller", QualifiedName: o + "::CppCaller", FilePath: o, Language: "cpp"},
			{Kind: "Function", Name: "Qualified", QualifiedName: o + "::Qualified", FilePath: o, Language: "cpp"},
			{
				Kind: "Function", Name: "TestSingle", QualifiedName: tf + "::TestSingle",
				FilePath: tf, Language: "cpp", IsTest: true,
			},
		}, []graphstore.GraphEdge{
			{Kind: "CALLS", SourceQualified: o + "::Qualified", TargetQualified: o + "::Over1", FilePath: o, Line: 3},
			{Kind: "CALLS", SourceQualified: o + "::CppCaller", TargetQualified: "Over", FilePath: o, Line: 4},
			{Kind: "CALLS", SourceQualified: o + "::CppCaller", TargetQualified: "Single", FilePath: o, Line: 5},
			{
				Kind: "CALLS", SourceQualified: o + "::Qualified", TargetQualified: "Single",
				FilePath: o, Line: 6, Extra: map[string]any{"receiver": "obj"},
			},
		}
}

// TestCovQACallersOfSkipsTheBareFallbackWithoutANode pins the guard in front of
// callers_of's second pass. The pass takes its search name and its language
// from the RESOLVED node, so without one there is nothing to search by —
// searching by the raw target instead would attribute every same-named call in
// the repository to a symbol that was never identified.
func TestCovQACallersOfSkipsTheBareFallbackWithoutANode(t *testing.T) {
	engine, store, root := covQAEnv(t)
	svc := covQAPath(root, "pkg/a.go") + "::Svc"
	q := &graphQuery{engine: engine, store: store, pattern: "callers_of", target: svc, limit: 100}
	if err := q.callersOf(svc); err != nil {
		t.Fatalf("callersOf: %v", err)
	}
	covQAEqual(t, "total", q.total, 1)
	covQAEqual(t, "results[0].name", q.results[0]["name"], "Caller")
	if store.consulted("GetEdgesByTarget:Svc") {
		t.Error("the bare-name pass ran without a resolved node")
	}
}

// TestCovQATestsForSkipsAVanishedTestRow pins that a covering test whose node
// row is gone by the time the payload is projected is omitted, rather than
// reported as a result full of nulls.
func TestCovQATestsForSkipsAVanishedTestRow(t *testing.T) {
	engine, store, root := covQAEnv(t)
	store.missing[covQAPath(root, "pkg/a_test.go")+"::TestCaller"] = true
	payload := covQAPayload(t, engine,
		covQAArgs("tests_for", covQAPath(root, "pkg/a.go")+"::Svc", nil))
	covQAEqualStrings(t, "results",
		covQAField(t, payload, "results", "name"), []string{"TestSvc"})
}

// TestCovQATestsForDistinguishesEvidenceFromAGuess pins the two provenance
// markers: a coverage hit carries `indirect` (whether it was reached through a
// call) and a convention hit additionally carries `inferred_by`, so a caller
// can tell a recorded fact from a name-shaped guess.
func TestCovQATestsForDistinguishesEvidenceFromAGuess(t *testing.T) {
	engine, _, root := covQAEnv(t)
	payload := covQAPayload(t, engine,
		covQAArgs("tests_for", covQAPath(root, "pkg/a.go")+"::Svc", nil))
	results, ok := payload["results"].([]map[string]any)
	if !ok || len(results) != 2 {
		t.Fatalf("results = %v, want two entries", payload["results"])
	}
	covQAEqual(t, "direct indirect", results[0]["indirect"], false)
	covQAEqual(t, "direct inferred_by", results[0]["inferred_by"], nil)
	covQAEqual(t, "one-hop indirect", results[1]["indirect"], true)
	covQAEqual(t, "one-hop inferred_by", results[1]["inferred_by"], nil)
}

// ── store failures ───────────────────────────────────────────────────────────

// TestCovQAStoreFailuresPropagate pins that a failed graph read becomes a tool
// error rather than a partial answer. Every read on every pattern's path is
// listed, because a swallowed failure here reports "0 callers" for a query that
// never ran — exactly the false negative the confidence markers exist to stop.
func TestCovQAStoreFailuresPropagate(t *testing.T) {
	root := covQARoot(t)
	for _, tc := range covQAFailureCases(root) {
		t.Run(tc.name, func(t *testing.T) {
			engine, store := covQAEnvAt(t, root)
			for key, err := range tc.fail {
				store.fail[key] = err
			}
			for key, after := range tc.failAfter {
				store.failAfter[key] = after
			}
			_, err := covQACall(t, engine, covQAArgs(tc.pattern, tc.target, nil))
			if !errors.Is(err, errFake) {
				t.Fatalf("query_graph_tool(%s, %s) error = %v, want the injected failure",
					tc.pattern, tc.target, err)
			}
		})
	}
}

// covQAFailureCase is one injected reader failure and the query that must
// surface it.
type covQAFailureCase struct {
	name      string
	pattern   string
	target    string
	fail      map[string]error
	failAfter map[string]int
}

func covQAFailureCases(root string) []covQAFailureCase {
	cases := covQAResolutionFailureCases(root)
	return append(cases, covQAWalkFailureCases(root)...)
}

// covQAResolutionFailureCases are failures inside the four-step target
// resolution, which runs before any pattern body.
func covQAResolutionFailureCases(root string) []covQAFailureCase {
	a := covQAPath(root, "pkg/a.go")
	return []covQAFailureCase{
		{
			name: "the target lookup", pattern: "callers_of", target: a + "::Svc",
			fail: map[string]error{"GetNode:" + a + "::Svc": errFake},
		},
		{
			name: "the root-anchored retry", pattern: "callers_of", target: "pkg/zz.go",
			// The target as given misses, so resolution retries it anchored
			// under the repository root; only that second read fails.
			fail: map[string]error{"GetNode:" + covQAPath(root, "pkg/zz.go"): errFake},
		},
		{
			name: "the keyword search", pattern: "callers_of", target: "nosuchsymbol",
			fail: map[string]error{"SearchNodesFTSWords": errFake},
		},
		{
			name: "the Java-FQN search", pattern: "callers_of", target: "com.example.Service.handle",
			fail: map[string]error{"SearchNodesFTSWords": errFake},
		},
		{
			name: "the disambiguation count", pattern: "callers_of", target: "Child",
			// Two nodes match, so the ambiguous answer needs the unbounded
			// count that the capped candidate list cannot provide.
			fail: map[string]error{"CountNodesFTSWords": errFake},
		},
	}
}

// covQAWalkFailureCases are failures inside a pattern body.
func covQAWalkFailureCases(root string) []covQAFailureCase {
	a, b, c := covQAPath(root, "pkg/a.go"), covQAPath(root, "pkg/b.go"), covQAPath(root, "pkg/c.go")
	tf, jav := covQAPath(root, "pkg/a_test.go"), covQAPath(root, "app/App.java")
	svc, caller, child := a+"::Svc", b+"::Caller", c+"::Child"
	return []covQAFailureCase{
		{
			name: "callers_of incoming edges", pattern: "callers_of", target: svc,
			fail: map[string]error{"GetEdgesByTarget:" + svc: errFake},
		},
		{
			name: "callers_of caller row", pattern: "callers_of", target: svc,
			fail: map[string]error{"GetNode:" + caller: errFake},
		},
		{
			name: "callers_of bare-name edges", pattern: "callers_of", target: svc,
			fail: map[string]error{"GetEdgesByTarget:Svc": errFake},
		},
		{
			name: "callers_of bare caller row", pattern: "callers_of", target: a + "::Blank",
			// The resolved node has no language, so the bare-name reader
			// applies no source filter and the row is read here instead.
			fail: map[string]error{"GetNode:" + caller: errFake},
		},
		{
			name: "references_to incoming edges", pattern: "references_to", target: svc,
			fail: map[string]error{"GetEdgesByTarget:" + svc: errFake},
		},
		{
			name: "references_to source row", pattern: "references_to", target: svc,
			fail: map[string]error{"GetNode:" + caller: errFake},
		},
		{
			name: "callees_of outgoing edges", pattern: "callees_of", target: svc,
			fail: map[string]error{"GetEdgesBySource:" + svc: errFake},
		},
		{
			name: "callees_of callee row", pattern: "callees_of", target: svc,
			fail: map[string]error{"GetNode:" + caller: errFake},
		},
		{
			name: "imports_of outgoing edges", pattern: "imports_of", target: a,
			fail: map[string]error{"GetEdgesBySource:" + a: errFake},
		},
		{
			name: "importers_of incoming edges", pattern: "importers_of", target: a,
			fail: map[string]error{"GetEdgesByTarget:" + a: errFake},
		},
		{
			name: "children_of outgoing edges", pattern: "children_of", target: a,
			fail: map[string]error{"GetEdgesBySource:" + a: errFake},
		},
		{
			name: "children_of child row", pattern: "children_of", target: a,
			fail: map[string]error{"GetNode:" + svc: errFake},
		},
		{
			name: "tests_for coverage walk", pattern: "tests_for", target: svc,
			fail: map[string]error{"ReadAllNodes": errFake},
		},
		{
			name: "tests_for test row", pattern: "tests_for", target: svc,
			fail: map[string]error{"GetNode:" + tf + "::TestSvc": errFake},
		},
		{
			name: "tests_for convention search", pattern: "tests_for", target: svc,
			fail: map[string]error{"SearchNodesFTSWords": errFake},
		},
		{
			name: "inheritors_of incoming edges", pattern: "inheritors_of", target: a + "::Base",
			fail: map[string]error{"GetEdgesByTarget:" + a + "::Base": errFake},
		},
		{
			name: "inheritors_of inheritor row", pattern: "inheritors_of", target: a + "::Base",
			fail: map[string]error{"GetNode:" + child: errFake},
		},
		{
			name: "inheritors_of bare-name edges", pattern: "inheritors_of", target: a + "::Lonely",
			fail: map[string]error{"GetEdgesByTarget:Lonely": errFake},
		},
		{
			name: "inheritors_of bare inheritor row", pattern: "inheritors_of", target: a + "::Blank",
			fail: map[string]error{"GetNode:" + child: errFake},
		},
		{
			name: "triggered_by incoming edges", pattern: "triggered_by", target: svc,
			fail: map[string]error{"GetEdgesByTarget:" + svc: errFake},
		},
		{
			name: "triggered_by source row", pattern: "triggered_by", target: svc,
			fail: map[string]error{"GetNode:" + caller: errFake},
		},
		{
			name: "triggers_of outgoing edges", pattern: "triggers_of", target: svc,
			fail: map[string]error{"GetEdgesBySource:" + svc: errFake},
		},
		{
			name: "triggers_of target row", pattern: "triggers_of", target: svc,
			fail: map[string]error{"GetNode:" + caller: errFake},
		},
		{
			name: "consumers_of config edges", pattern: "consumers_of", target: "app.mail.host",
			fail: map[string]error{"GetEdgesByTarget:config:app.mail.host": errFake},
		},
		{
			name: "consumers_of consumer row", pattern: "consumers_of", target: "app.mail.host",
			fail: map[string]error{"GetNode:" + jav + "::Consumer": errFake},
		},
		{
			name: "file_summary node read", pattern: "file_summary", target: "pkg/a.go",
			// Path resolution tolerates a failed lookup — it is probing
			// spellings — so the failure surfaces on the read that answers.
			fail: map[string]error{"GetNodesByFile:" + a: errFake},
		},
	}
}

// TestCovQAImportersOfSurfacesNamespaceFailures pins the two reads the C#
// namespace pass adds, which exist only on that path.
func TestCovQAImportersOfSurfacesNamespaceFailures(t *testing.T) {
	root := t.TempDir()
	nodes, edges := covQACSharpGraph(root)
	x := covQAPath(root, "src/x.cs")
	for _, tc := range []struct{ name, key string }{
		{name: "the declaring file's rows", key: "GetNodesByFile:" + x},
		{name: "the namespace's importers", key: "GetEdgesByTarget:My.Ns"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := covQASeed(nodes, edges)
			store.fail[tc.key] = errFake
			engine := engineWithStore(t, root, store)
			if _, err := covQACall(t, engine, covQAArgs("importers_of", x, nil)); !errors.Is(err, errFake) {
				t.Fatalf("error = %v, want the injected failure", err)
			}
		})
	}
}

// TestCovQACppOverloadProbeFailuresPropagate pins the overload probe, which is
// an extra bulk read layered on top of a walk that already performed one — so
// it can only be observed failing after that first read has succeeded.
func TestCovQACppOverloadProbeFailuresPropagate(t *testing.T) {
	root := t.TempDir()
	nodes, edges := covQACppGraph(root)
	o := covQAPath(root, "src/o.cpp")
	for _, tc := range []struct {
		name      string
		pattern   string
		failAfter int
	}{
		// callers_of probes overloads before any other bulk node read.
		{name: "callers_of", pattern: "callers_of", failAfter: 0},
		// tests_for walks coverage first, which reads every node, so the
		// probe is the second bulk read.
		{name: "tests_for", pattern: "tests_for", failAfter: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := covQASeed(nodes, edges)
			store.failAfter["ReadAllNodes"] = tc.failAfter
			engine := engineWithStore(t, root, store)
			_, err := covQACall(t, engine, covQAArgs(tc.pattern, o+"::Over1", nil))
			if !errors.Is(err, errFake) {
				t.Fatalf("error = %v, want the injected failure", err)
			}
		})
	}
}

// TestCovQAUnopenableGraphIsAnError pins that a database which exists but
// cannot be opened is reported. It is the one read failure NOT degraded to an
// empty graph: "never built" and "broken" are different answers.
func TestCovQAUnopenableGraphIsAnError(t *testing.T) {
	engine := unreadableEngine(t)
	if _, err := covQACall(t, engine, covQAArgs("callers_of", "Login", nil)); err == nil {
		t.Fatal("an unopenable graph must be reported, not answered as empty")
	}
}
