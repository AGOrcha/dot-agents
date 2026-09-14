package codegraph

import (
	"errors"
	"fmt"
	"math"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/AGOrcha/dot-agents/internal/crgrelease"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// The query/search/stats quarter of the release's tool surface
// (code_review_graph/tools/query.py): `query_graph_tool`,
// `semantic_search_nodes_tool`, `find_large_functions_tool` and
// `list_graph_stats_tool`.
//
// Two release behaviours shape this file and are worth stating up front,
// because reproducing them is most of the work:
//
//  1. Search has TWO different orderings, and they are not interchangeable.
//     `hybrid_search` (semantic_search_nodes, traverse_graph) runs FTS5 with
//     `ORDER BY rank` and fuses the RANKS — so the score is a function of
//     position, not of BM25. `GraphStore.search_nodes` (query_graph's target
//     resolution, tests_for's naming-convention pass) runs the same MATCH with
//     NO `ORDER BY`, so FTS5 answers in ascending rowid order and the LIMIT
//     truncates BEFORE ordering. Using one where the release uses the other
//     silently reorders, and on a big graph silently re-SELECTS, every result.
//     They map onto two distinct store readers: SearchNodesFTS (rank) and
//     SearchNodesFTSWords (rowid).
//
//  2. query_graph's bounding is a streaming counter, not the shared Bounded
//     helper. The release counts every logical result but appends only the
//     bounded prefix, and it appends an edge only for a RETAINED result — so
//     `edges` is aligned with the visible `results`, not with the total. Its
//     summary fragment is " — showing N, M omitted", which is also not
//     ShownOf's ", showing N of M". ValidatePositiveInt does apply and is used;
//     Bounded/ShownOf deliberately are not.
//
// Embeddings are structurally absent from the native backend: `embed_graph_tool`
// is bridge-only and nothing in-process ever creates the `embeddings` table, so
// `search_mode` can only ever be "fts", "keyword" or "none" (never the
// release's "hybrid"/"semantic"), `semantic_search_nodes`' `model`/`provider`
// arguments have nothing to select, and `list_graph_stats`' `embeddings_count`
// is always 0 — which is why its summary always carries the release's
// "install sentence-transformers" line. On a natively-built graph the release
// answers identically, because its own embedding search short-circuits on an
// empty table.

func init() {
	RegisterTool("query_graph_tool", queryGraphTool)
	RegisterTool("semantic_search_nodes_tool", semanticSearchNodesTool)
	RegisterTool("find_large_functions_tool", findLargeFunctionsTool)
	RegisterTool("list_graph_stats_tool", listGraphStatsTool)
}

// ── query_graph_tool ─────────────────────────────────────────────────────────

// queryPatterns is the release's `_QUERY_PATTERNS`: the sixteen published
// patterns and the `description` each one echoes back.
var queryPatterns = map[string]string{
	"callers_of":    "Find all functions that call a given function",
	"references_to": "Find all nodes that reference a given symbol",
	"callees_of":    "Find all functions called by a given function",
	"imports_of":    "Find all imports of a given file or module",
	"importers_of":  "Find all files that import a given file or module",
	"children_of":   "Find all nodes contained in a file or class",
	"tests_for":     "Find all tests for a given function or class",
	"inheritors_of": "Find all classes that inherit from a given class",
	"triggers_of":   "Find methods invoked by a scheduler or other trigger",
	"triggered_by":  "Find schedulers or other triggers that invoke a method",
	"publishers_of": "Find methods that publish an event",
	"listeners_of":  "Find methods that listen for an event",
	"handlers_of":   "Find methods that handle an endpoint",
	"endpoints_for": "Find endpoints handled by a method",
	"consumers_of":  "Find classes that consume a Spring configuration property",
	"file_summary":  "Get a summary of all nodes in a file",
}

// queryPatternOrder is the DECLARATION order of `_QUERY_PATTERNS`. The
// unknown-pattern error echoes the dict's keys, and a Python dict renders in
// insertion order, so the order is part of the error string clients see.
var queryPatternOrder = []string{
	"callers_of", "references_to", "callees_of", "imports_of", "importers_of",
	"children_of", "tests_for", "inheritors_of", "triggers_of", "triggered_by",
	"publishers_of", "listeners_of", "handlers_of", "endpoints_for",
	"consumers_of", "file_summary",
}

// queryTargetSearchLimit / queryFQNCandidateLimit are the release's target
// resolution bounds (`store.search_nodes(target, limit=20)` and
// `_MAX_FQN_CANDIDATES`).
const (
	queryTargetSearchLimit = 20
	queryFQNCandidateLimit = 100
	queryMinimalLimit      = 5
	testNamingSearchLimit  = 10
)

// queryTypeKinds is the kind set `inheritors_of` narrows a bare target to, so
// `inheritors_of("Animal")` prefers the type named Animal over a same-named
// function.
var queryTypeKinds = map[string]bool{
	"Class": true, "Interface": true, "Type": true,
	"Struct": true, "Enum": true, "Trait": true,
}

// builtinCallNames is _common._BUILTIN_CALL_NAMES: the JS/TS builtin method
// names reverse call tracing refuses to answer for. They stay IN the graph —
// `callees_of` still reports them — and are excluded only from `callers_of`,
// because "who calls .map()?" is hundreds of hits and never the question the
// caller meant.
var builtinCallNames = map[string]bool{
	"map": true, "filter": true, "reduce": true, "reduceRight": true, "forEach": true, "find": true, "findIndex": true,
	"some": true, "every": true, "includes": true, "indexOf": true, "lastIndexOf": true,
	"push": true, "pop": true, "shift": true, "unshift": true, "splice": true, "slice": true,
	"concat": true, "join": true, "flat": true, "flatMap": true, "sort": true, "reverse": true, "fill": true,
	"keys": true, "values": true, "entries": true, "from": true, "isArray": true, "of": true, "at": true,
	"trim": true, "trimStart": true, "trimEnd": true, "split": true, "replace": true, "replaceAll": true,
	"match": true, "matchAll": true, "search": true, "substring": true, "substr": true,
	"toLowerCase": true, "toUpperCase": true, "startsWith": true, "endsWith": true,
	"padStart": true, "padEnd": true, "repeat": true, "charAt": true, "charCodeAt": true,
	"assign": true, "freeze": true, "defineProperty": true, "getOwnPropertyNames": true,
	"hasOwnProperty": true, "create": true, "is": true, "fromEntries": true,
	"log": true, "warn": true, "error": true, "info": true, "debug": true, "trace": true, "dir": true, "table": true,
	"time": true, "timeEnd": true, "assert": true, "clear": true, "count": true,
	"then": true, "catch": true, "finally": true, "resolve": true, "reject": true, "all": true, "allSettled": true, "race": true, "any": true,
	"parse": true, "stringify": true,
	"floor": true, "ceil": true, "round": true, "random": true, "max": true, "min": true, "abs": true, "pow": true, "sqrt": true,
	"addEventListener": true, "removeEventListener": true, "querySelector": true, "querySelectorAll": true,
	"getElementById": true, "createElement": true, "appendChild": true, "removeChild": true,
	"setAttribute": true, "getAttribute": true, "preventDefault": true, "stopPropagation": true,
	"setTimeout": true, "clearTimeout": true, "setInterval": true, "clearInterval": true,
	"toString": true, "valueOf": true, "toJSON": true, "toISOString": true,
	"getTime": true, "getFullYear": true, "now": true,
	"isNaN": true, "parseInt": true, "parseFloat": true, "toFixed": true,
	"encodeURIComponent": true, "decodeURIComponent": true,
	"call": true, "apply": true, "bind": true, "next": true,
	"emit": true, "on": true, "off": true, "once": true,
	"pipe": true, "write": true, "read": true, "end": true, "close": true, "destroy": true,
	"send": true, "status": true, "json": true, "redirect": true,
	"set": true, "get": true, "delete": true, "has": true,
	"findUnique": true, "findFirst": true, "findMany": true, "createMany": true,
	"update": true, "updateMany": true, "deleteMany": true, "upsert": true,
	"aggregate": true, "groupBy": true, "transaction": true,
	"describe": true, "it": true, "test": true, "expect": true, "beforeEach": true, "afterEach": true,
	"beforeAll": true, "afterAll": true, "mock": true, "spyOn": true,
	"require": true, "fetch": true,
}

func queryGraphTool(e *Engine, args crgrelease.Args) (any, error) {
	pattern := args.String("pattern")
	target := args.String("target")
	detailLevel := args.String("detail_level")
	maxResults := args.Int("max_results")

	// The release validates the bound BEFORE opening the store, outside its
	// try block, so the ValueError escapes to the MCP layer as a transport
	// error rather than becoming an in-band `{"status": "error"}` payload.
	// Returning a Go error is what reproduces that.
	if err := ValidatePositiveInt(maxResults, "max_results"); err != nil {
		return nil, err
	}
	if _, ok := queryPatterns[pattern]; !ok {
		// Not ErrorResponse: the release builds this dict literally, with no
		// `summary` key, so adding one would diverge.
		return map[string]any{
			"status": "error",
			"error": fmt.Sprintf("Unknown pattern '%s'. Available: %s",
				pattern, pythonStringListRepr(queryPatternOrder)),
		}, nil
	}

	store, err := e.readStore()
	if err != nil {
		return nil, err
	}

	// "Who calls .map()?" is hundreds of useless hits, so reverse call tracing
	// skips the common builtins — before target resolution, and only for a
	// BARE name: a qualified `utils.py::map` is a real symbol and bypasses it.
	if pattern == "callers_of" && builtinCallNames[target] && !strings.Contains(target, "::") {
		return map[string]any{
			"status": "ok", "pattern": pattern, "target": target,
			"description": queryPatterns[pattern],
			"summary": fmt.Sprintf(
				"'%s' is a common builtin — callers_of skipped to avoid noise.", target),
			"result_count": 0, "results_omitted": 0,
			"results": []any{}, "edges": []any{},
		}, nil
	}

	q := &graphQuery{
		engine: e, store: store, pattern: pattern, target: target,
		limit: maxResults, minimal: detailLevel == flowDetailMinimal,
	}
	if q.minimal && q.limit > queryMinimalLimit {
		q.limit = queryMinimalLimit
	}
	return q.run()
}

// graphQuery carries one query_graph invocation. It exists so the sixteen
// pattern bodies share the streaming result accumulator rather than each
// re-deriving the count/bound/edge-alignment rules.
type graphQuery struct {
	engine  *Engine
	store   graphstore.Store
	pattern string
	target  string
	limit   int
	minimal bool

	node    *graphstore.GraphNode
	results []map[string]any
	edges   []map[string]any
	total   int

	// ambiguous holds the disambiguation response when the target matched
	// more than one node. Target resolution and the pattern bodies are
	// separate steps, so the early answer has to be carried rather than
	// returned from inside resolution.
	ambiguous map[string]any
}

// add counts every logical result but retains only the bounded prefix, and
// records an edge only alongside a retained result. Passing a nil edge is the
// release's "result with no aligned edge" (children_of, tests_for,
// file_summary), which is why those patterns answer with an empty `edges`.
func (q *graphQuery) add(result map[string]any, edge *graphstore.GraphEdge) {
	q.total++
	if len(q.results) >= q.limit {
		return
	}
	q.results = append(q.results, result)
	if edge != nil {
		q.edges = append(q.edges, EdgeToDict(*edge))
	}
}

// addEdgeOnly appends an edge with no accompanying result. The event/trigger
// patterns use it for an edge whose endpoint has no node row: the relationship
// is real and reportable even though there is nothing to project.
func (q *graphQuery) addEdgeOnly(edge graphstore.GraphEdge) {
	q.edges = append(q.edges, EdgeToDict(edge))
}

func (q *graphQuery) run() (any, error) {
	if err := q.resolveTarget(); err != nil {
		return nil, err
	}
	if q.ambiguous != nil {
		return q.ambiguous, nil
	}
	if q.node == nil && q.pattern != "consumers_of" && q.pattern != "file_summary" {
		// An unresolved target lands HERE, not on the empty-result path below,
		// so the not-indexed marker has to be attached here too.
		unresolved := map[string]any{
			"status":  "not_found",
			"summary": fmt.Sprintf("No node found matching '%s'.", q.target),
		}
		if note := q.emptyConfidence(); note != "" {
			unresolved["confidence"] = note
		}
		return unresolved, nil
	}
	if err := q.dispatch(); err != nil {
		return nil, err
	}
	return q.payload(), nil
}

// resolveTarget reproduces the release's four-step target resolution: the
// target as given, the target anchored under the repository root, a Java-FQN
// specific lookup, then a keyword search whose outcome is either a unique node
// or the disambiguation response.
func (q *graphQuery) resolveTarget() error {
	// `file_summary` targets are paths and `consumers_of` bare targets are
	// config keys, so neither goes through node resolution.
	if q.pattern == "file_summary" {
		return nil
	}
	if q.pattern == "consumers_of" && !strings.Contains(q.target, "::") {
		return nil
	}
	if q.store == nil {
		return nil
	}

	node, err := q.store.GetNode(q.target)
	if err != nil {
		return err
	}
	if node == nil {
		if node, err = q.store.GetNode(anchorUnderRoot(q.engine.root, q.target)); err != nil {
			return err
		}
	}
	if node != nil {
		q.node = node
		return nil
	}

	candidates, isJavaFQN, err := q.candidates()
	if err != nil {
		return err
	}
	if q.pattern == "inheritors_of" && !strings.Contains(q.target, "::") {
		var exact []graphstore.GraphNode
		for _, candidate := range candidates {
			if candidate.Name == q.target && queryTypeKinds[candidate.Kind] {
				exact = append(exact, candidate)
			}
		}
		if len(exact) > 0 {
			candidates = exact
		}
	}
	switch {
	case len(candidates) == 1:
		q.node = &candidates[0]
		// The release rewrites `target` to the resolved qualified name, so the
		// summary and the echoed `target` name the node that was queried.
		q.target = q.node.QualifiedName
	case len(candidates) > 1:
		// A Java FQN's candidate list is already the complete evidence-backed
		// set, so its count is the list length; a keyword search's list was
		// capped at 20, so the real total needs a separate count.
		count := len(candidates)
		if !isJavaFQN {
			if count, err = releaseCountSearchNodes(q.store, q.target); err != nil {
				return err
			}
		}
		ranked := rankDisambiguationCandidates(candidates, q.target)
		q.ambiguous = map[string]any{
			"status": "ambiguous",
			"summary": fmt.Sprintf(
				"'%s' matches %d node(s). Re-run with a qualified_name from disambiguation.",
				q.target, count),
			// Both keys carry the same list: `candidates` is the established
			// name and `disambiguation` the clearer one #458 introduced.
			"candidates":           ranked,
			"disambiguation":       ranked,
			"candidate_count":      count,
			"candidates_truncated": count > len(candidates),
			"hint":                 "Use a qualified_name from disambiguation as the target parameter.",
		}
	}
	return nil
}

// candidates returns the resolution candidate set and whether it came from the
// Java-FQN path. A Java-shaped target that resolves to NOTHING deliberately
// yields an empty list rather than falling through to the keyword search: a
// globally unique method name is not evidence that it is the one named by the
// package-qualified target.
func (q *graphQuery) candidates() ([]graphstore.GraphNode, bool, error) {
	if javaCandidates, ok, err := q.javaFQNCandidates(); err != nil {
		return nil, false, err
	} else if ok {
		return javaCandidates, true, nil
	}
	nodes, err := releaseSearchNodes(q.store, q.target, queryTargetSearchLimit)
	return nodes, false, err
}

// javaFQNCandidates resolves a `pkg.Class.method` target using language plus
// class/file evidence. The second result reports whether the target is
// Java-FQN-shaped at all, which is what separates "no safe match" from "not
// this kind of target".
func (q *graphQuery) javaFQNCandidates() ([]graphstore.GraphNode, bool, error) {
	if !looksLikeJavaMethodFQN(q.target) {
		return nil, false, nil
	}
	parts := strings.Split(q.target, ".")
	className, methodName := parts[len(parts)-2], parts[len(parts)-1]
	found, err := releaseSearchNodes(q.store, methodName, queryFQNCandidateLimit)
	if err != nil {
		return nil, true, err
	}
	var matches []graphstore.GraphNode
	for _, candidate := range found {
		if !strings.EqualFold(candidate.Language, "java") || candidate.Name != methodName {
			continue
		}
		parentMatch := lastDotSegment(candidate.ParentName) == className
		fileMatch := pathStem(candidate.FilePath) == className
		qualifiedMatch := strings.HasSuffix(
			lastQualifiedSegment(candidate.QualifiedName), className+"."+methodName)
		if parentMatch || fileMatch || qualifiedMatch {
			matches = append(matches, candidate)
		}
	}
	return matches, true, nil
}

// rankDisambiguationCandidates orders the disambiguation list by match quality
// — exact qualified name, exact name, substring, then everything else — with
// the qualified name as the tie-break so the list is stable across calls.
func rankDisambiguationCandidates(candidates []graphstore.GraphNode, target string) []map[string]any {
	lowered := strings.ToLower(target)
	rank := func(node graphstore.GraphNode) int {
		switch {
		case node.QualifiedName == target:
			return 0
		case node.Name == target:
			return 1
		case strings.Contains(strings.ToLower(node.QualifiedName), lowered):
			return 2
		default:
			return 3
		}
	}
	ordered := make([]graphstore.GraphNode, len(candidates))
	copy(ordered, candidates)
	sort.Slice(ordered, func(i, j int) bool {
		if ri, rj := rank(ordered[i]), rank(ordered[j]); ri != rj {
			return ri < rj
		}
		return ordered[i].QualifiedName < ordered[j].QualifiedName
	})
	out := make([]map[string]any, 0, len(ordered))
	for _, node := range ordered {
		out = append(out, NodeToDict(node))
	}
	return out
}

// payload renders the standard or minimal response. The `confidence` marker is
// attached only when the result set is empty: a zero is the dangerous
// direction, because an agent reads it as "none exist" and either concludes
// wrongly or falls back to grepping the repository.
func (q *graphQuery) payload() map[string]any {
	omitted := q.total - len(q.results)
	if omitted < 0 {
		omitted = 0
	}
	summary := fmt.Sprintf("Found %d result(s) for %s('%s')", q.total, q.pattern, q.target)
	if omitted > 0 {
		summary += fmt.Sprintf(" — showing %d, %d omitted", len(q.results), omitted)
	}
	note := ""
	if q.total == 0 {
		note = q.emptyConfidence()
	}

	if q.minimal {
		minimal := make([]map[string]any, 0, len(q.results))
		for _, result := range q.results {
			minimal = append(minimal, projectKeys(result, "name", "kind", "file_path", "indirect"))
		}
		response := map[string]any{
			"status": "ok", "pattern": q.pattern, "target": q.target,
			"description": queryPatterns[q.pattern], "summary": summary,
			"result_count": q.total, "results_omitted": omitted,
			"results": emptyableMaps(minimal),
		}
		if note != "" {
			response["confidence"] = note
		}
		return response
	}

	response := map[string]any{
		"status": "ok", "pattern": q.pattern, "target": q.target,
		"description": queryPatterns[q.pattern], "summary": summary,
		"result_count": q.total, "results_omitted": omitted,
		"results": emptyableMaps(q.results),
		"edges":   emptyableMaps(q.edges),
	}
	if note != "" {
		response["confidence"] = note
	}
	return response
}

// emptyConfidence is uncertainty.empty_query_confidence. The priority order is
// the release's and is deliberate: an unresolved target makes the zero
// meaningless, a stale graph makes it untrustworthy, and a known language gap
// makes it incomplete. Only when none of those hold is the zero worth
// believing, and saying so is what stops an agent grepping anyway.
func (q *graphQuery) emptyConfidence() string {
	if q.node == nil {
		if q.store == nil {
			return ConfidenceNote(emptyGraphNote)
		}
		stats, err := q.store.GetStats()
		if err != nil {
			// An advisory marker must never turn a working call into an error.
			return ""
		}
		if stats.TotalNodes == 0 {
			return ConfidenceNote(emptyGraphNote)
		}
		if stale, _ := GraphStaleness(q.store, q.engine.root, ""); stale != "" {
			return ConfidenceNote(UnresolvedStaleNote(q.target))
		}
		return ConfidenceNote(NotIndexedNote(q.target))
	}
	stale, current := GraphStaleness(q.store, q.engine.root, q.node.FilePath)
	if stale != "" {
		return ConfidenceNote(stale)
	}
	if gap := LanguageGapNote(q.node.Language, q.pattern); gap != "" {
		return ConfidenceNote(gap)
	}
	return ConfidenceNote(confirmedAbsenceNote(q.target, current))
}

const emptyGraphNote = "graph is empty: nothing is indexed, so this 0 says " +
	"nothing about the code; run `code-review-graph build`"

// confirmedAbsenceNote is uncertainty._confirmed_note: the zero is a real
// absence, so the agent can stop searching. The strong wording is used only
// when currency was actually checked — when it could not be (no VCS, no build
// metadata) the weaker sentence still saves the fallback search without
// claiming something unverified.
func confirmedAbsenceNote(target string, current bool) string {
	if current {
		return InterpolatedTargetNote("'", target,
			"' is indexed and the graph is current, so this 0 is a real absence")
	}
	return InterpolatedTargetNote("'", target,
		"' is indexed and no such edge is recorded; graph currency unverified")
}

// ── query_graph pattern bodies ───────────────────────────────────────────────

func (q *graphQuery) dispatch() error {
	if q.store == nil {
		return nil
	}
	qualified := q.target
	if q.node != nil {
		qualified = q.node.QualifiedName
	}
	switch q.pattern {
	case "callers_of":
		return q.callersOf(qualified)
	case "references_to":
		return q.sourcesOfIncoming(qualified, "REFERENCES")
	case "callees_of":
		return q.calleesOf(qualified)
	case "imports_of":
		return q.importsOf(qualified)
	case "importers_of":
		return q.importersOf()
	case "children_of":
		return q.childrenOf(qualified)
	case "tests_for":
		return q.testsFor(qualified)
	case "inheritors_of":
		return q.inheritorsOf(qualified)
	case "triggers_of":
		return q.outgoingEndpoints(qualified, "TRIGGERS", "")
	case "triggered_by":
		return q.incomingEndpoints(qualified, "TRIGGERS")
	case "publishers_of":
		return q.incomingEndpoints(qualified, "PUBLISHES")
	case "listeners_of", "handlers_of":
		return q.incomingEndpoints(qualified, "HANDLES")
	case "endpoints_for":
		return q.outgoingEndpoints(qualified, "HANDLES", "Endpoint")
	case "consumers_of":
		return q.consumersOf()
	case "file_summary":
		return q.fileSummary()
	}
	return nil
}

// callersOf walks CALLS edges backwards. It runs TWO passes because a
// cross-package call keeps a BARE target name (`Login`) while the resolved node
// is fully qualified (`auth.go::Login`), so the qualified scan alone misses
// every call that crossed a file. Results from the bare pass are tagged
// `target_resolution: "unresolved"` rather than being presented as certain.
func (q *graphQuery) callersOf(qualified string) error {
	seen := map[string]bool{}
	incoming, err := q.store.GetEdgesByTarget(qualified)
	if err != nil {
		return err
	}
	for i := range incoming {
		edge := incoming[i]
		if edge.Kind != "CALLS" || seen[edge.SourceQualified] {
			continue
		}
		seen[edge.SourceQualified] = true
		caller, err := q.store.GetNode(edge.SourceQualified)
		if err != nil {
			return err
		}
		if caller != nil {
			q.add(NodeToDict(*caller), &edge)
		}
	}
	if q.node == nil {
		return nil
	}

	// A C++ overload set deliberately keeps its call targets bare. The
	// candidates support disambiguation but do not prove that any one exact
	// overload was called, so an ambiguous set contributes no callers at all.
	overloads := 0
	if q.node.Language == "cpp" {
		if overloads, err = releaseCountNodesByName(
			q.store, q.node.Name, "cpp", "Function", "Test"); err != nil {
			return err
		}
	}
	bare, err := releaseEdgesByTargetName(q.store, q.node.Name, "CALLS", q.node.Language)
	if err != nil {
		return err
	}
	for i := range bare {
		edge := bare[i]
		if hasUnresolvedTargets(edge.Extra) {
			continue
		}
		if q.node.Language == "cpp" && edge.Extra["receiver"] != nil {
			continue
		}
		if overloads > 1 || seen[edge.SourceQualified] {
			continue
		}
		seen[edge.SourceQualified] = true
		caller, err := q.store.GetNode(edge.SourceQualified)
		if err != nil {
			return err
		}
		if caller != nil {
			result := NodeToDict(*caller)
			result["target_resolution"] = "unresolved"
			q.add(result, &edge)
		}
	}
	return nil
}

// sourcesOfIncoming projects the SOURCE node of every incoming edge of one
// kind, skipping an edge whose source has no node row. `references_to` is the
// only pattern shaped exactly like this.
func (q *graphQuery) sourcesOfIncoming(qualified, kind string) error {
	seen := map[string]bool{}
	edges, err := q.store.GetEdgesByTarget(qualified)
	if err != nil {
		return err
	}
	for i := range edges {
		edge := edges[i]
		if edge.Kind != kind || seen[edge.SourceQualified] {
			continue
		}
		source, err := q.store.GetNode(edge.SourceQualified)
		if err != nil {
			return err
		}
		if source != nil {
			seen[edge.SourceQualified] = true
			q.add(NodeToDict(*source), &edge)
		}
	}
	return nil
}

// calleesOf walks CALLS edges forwards. A target with no node row is still
// reported — as a synthetic Function carrying the raw target name — when it is
// bare, or when the extractor recorded an ambiguous/unresolved candidate set:
// "this call goes somewhere we could not pin down" is information, and
// dropping it would read as "this function calls nothing".
func (q *graphQuery) calleesOf(qualified string) error {
	seen := map[string]bool{}
	edges, err := q.store.GetEdgesBySource(qualified)
	if err != nil {
		return err
	}
	for i := range edges {
		edge := edges[i]
		if edge.Kind != "CALLS" || seen[edge.TargetQualified] {
			continue
		}
		seen[edge.TargetQualified] = true
		callee, err := q.store.GetNode(edge.TargetQualified)
		if err != nil {
			return err
		}
		if callee != nil {
			q.add(NodeToDict(*callee), &edge)
			continue
		}
		ambiguous, hasAmbiguous := stringList(edge.Extra["ambiguous_targets"])
		unresolved, hasUnresolved := stringList(edge.Extra["unresolved_targets"])
		reportable := hasAmbiguous || hasUnresolved ||
			!strings.Contains(edge.TargetQualified, "::") ||
			(q.node != nil && q.node.Language == "cpp")
		if !reportable {
			continue
		}
		result := map[string]any{
			"kind":           "Function",
			"name":           edge.TargetQualified,
			"qualified_name": edge.TargetQualified,
		}
		// The release picks the candidate set with `ambiguous or unresolved`,
		// so an ambiguous list that is PRESENT BUT EMPTY is falsy and hands
		// over to the unresolved list — including the label, which is derived
		// from the same truthiness test rather than from which key exists.
		candidates, labelled := unresolved, hasUnresolved
		resolution := "unresolved"
		if len(ambiguous) > 0 {
			candidates, labelled, resolution = ambiguous, true, "ambiguous"
		}
		if labelled {
			shown := sanitizeAll(candidates, 20)
			count, ok := extraInt(edge.Extra[resolution+"_target_count"])
			if !ok {
				count = len(candidates)
			}
			result["resolution"] = resolution
			result["candidates"] = shown
			result["candidate_count"] = count
			result["candidates_truncated"] = truthy(edge.Extra[resolution+"_targets_truncated"]) ||
				count > len(shown)
		}
		q.add(result, &edge)
	}
	return nil
}

// importsOf reports what a file imports. The projection is the raw edge target,
// not a node: an import of a third-party module resolves to no node at all, so
// naming the target is the only honest answer.
func (q *graphQuery) importsOf(qualified string) error {
	edges, err := q.store.GetEdgesBySource(qualified)
	if err != nil {
		return err
	}
	for i := range edges {
		edge := edges[i]
		if edge.Kind == "IMPORTS_FROM" {
			q.add(map[string]any{"import_target": edge.TargetQualified}, &edge)
		}
	}
	return nil
}

// importersOf reports what imports a file. Edge targets are the canonical
// absolute path `_resolve_module_to_file` stored, which is exactly the
// resolved node's own file path — and `importers_of` is not one of the two
// patterns that tolerate an unresolved target, so run() has already answered
// `not_found` by the time this is reached and the node is always present.
func (q *graphQuery) importersOf() error {
	seen := map[string]bool{}
	if err := q.addImporters(q.node.FilePath, seen); err != nil {
		return err
	}
	// C# `using X.Y;` produces IMPORTS_FROM edges whose target is the raw
	// namespace string rather than a file path, so the path lookup above
	// cannot see them (#310). Resolve the file's declared namespaces and
	// search by those too.
	if q.node.Language != "csharp" {
		return nil
	}
	nodes, err := q.store.GetNodesByFile(q.node.FilePath)
	if err != nil {
		return err
	}
	for _, node := range nodes {
		if node.Kind != "File" {
			continue
		}
		namespaces, _ := stringList(node.Extra["csharp_namespaces"])
		for _, namespace := range namespaces {
			if err := q.addImporters(namespace, seen); err != nil {
				return err
			}
		}
		break
	}
	return nil
}

func (q *graphQuery) addImporters(target string, seen map[string]bool) error {
	edges, err := q.store.GetEdgesByTarget(target)
	if err != nil {
		return err
	}
	for i := range edges {
		edge := edges[i]
		if edge.Kind != "IMPORTS_FROM" || seen[edge.SourceQualified] {
			continue
		}
		seen[edge.SourceQualified] = true
		q.add(map[string]any{
			"importer": edge.SourceQualified,
			"file":     edge.FilePath,
		}, &edge)
	}
	return nil
}

// childrenOf lists what a file or class contains. The release passes NO edge to
// the accumulator here, so `edges` stays empty: containment is already implied
// by the parent that was asked about.
func (q *graphQuery) childrenOf(qualified string) error {
	edges, err := q.store.GetEdgesBySource(qualified)
	if err != nil {
		return err
	}
	for _, edge := range edges {
		if edge.Kind != "CONTAINS" {
			continue
		}
		child, err := q.store.GetNode(edge.TargetQualified)
		if err != nil {
			return err
		}
		if child != nil {
			q.add(NodeToDict(*child), nil)
		}
	}
	return nil
}

// testsFor answers from TESTED_BY coverage first, then from naming convention.
// The two are distinguishable in the payload: a coverage hit carries `indirect`
// (whether it was reached through a call), a convention hit additionally
// carries `inferred_by: "naming_convention"` so a caller can tell a recorded
// fact from a guess.
func (q *graphQuery) testsFor(qualified string) error {
	seen := map[string]bool{}
	matches, err := releaseTransitiveTests(q.store, qualified)
	if err != nil {
		return err
	}
	for _, match := range matches {
		if seen[match.qualifiedName] {
			continue
		}
		test, err := q.store.GetNode(match.qualifiedName)
		if err != nil {
			return err
		}
		if test == nil {
			continue
		}
		result := NodeToDict(*test)
		result["indirect"] = match.indirect
		q.add(result, nil)
		seen[match.qualifiedName] = true
	}

	name := q.target
	if q.node != nil {
		name = q.node.Name
	}
	// A C++ overload set has no single node the convention could name, so the
	// guess is suppressed rather than attributed to an arbitrary overload.
	if q.node != nil && q.node.Language == "cpp" {
		overloads, err := releaseCountNodesByName(q.store, q.node.Name, "cpp", "Function", "Test")
		if err != nil {
			return err
		}
		if overloads > 1 {
			return nil
		}
	}
	var byConvention []graphstore.GraphNode
	for _, prefix := range []string{"test_", "Test"} {
		found, err := releaseSearchNodes(q.store, prefix+name, testNamingSearchLimit)
		if err != nil {
			return err
		}
		byConvention = append(byConvention, found...)
	}
	for _, test := range byConvention {
		if seen[test.QualifiedName] || !test.IsTest {
			continue
		}
		result := NodeToDict(test)
		result["indirect"] = false
		result["inferred_by"] = "naming_convention"
		q.add(result, nil)
		seen[test.QualifiedName] = true
	}
	return nil
}

// inheritorsOf walks INHERITS/IMPLEMENTS backwards, with the same bare-name
// fallback callersOf needs: those edges store an unqualified base name
// (`Animal`) while the resolved node is qualified (#87). The fallback runs only
// when the qualified scan found nothing, so a graph with resolved edges never
// pays for it.
func (q *graphQuery) inheritorsOf(qualified string) error {
	edges, err := q.store.GetEdgesByTarget(qualified)
	if err != nil {
		return err
	}
	for i := range edges {
		edge := edges[i]
		if edge.Kind != "INHERITS" && edge.Kind != "IMPLEMENTS" {
			continue
		}
		child, err := q.store.GetNode(edge.SourceQualified)
		if err != nil {
			return err
		}
		if child != nil {
			q.add(NodeToDict(*child), &edge)
		}
	}
	if q.total != 0 || q.node == nil {
		return nil
	}
	for _, kind := range []string{"INHERITS", "IMPLEMENTS"} {
		bare, err := releaseEdgesByTargetName(q.store, q.node.Name, kind, q.node.Language)
		if err != nil {
			return err
		}
		for i := range bare {
			edge := bare[i]
			child, err := q.store.GetNode(edge.SourceQualified)
			if err != nil {
				return err
			}
			if child != nil {
				q.add(NodeToDict(*child), &edge)
			}
		}
	}
	return nil
}

// incomingEndpoints projects the source of every incoming edge of one kind and
// reports the bare edge when the source has no node row — the shape
// `triggered_by`, `publishers_of`, `listeners_of` and `handlers_of` share.
func (q *graphQuery) incomingEndpoints(qualified, kind string) error {
	edges, err := q.store.GetEdgesByTarget(qualified)
	if err != nil {
		return err
	}
	for i := range edges {
		edge := edges[i]
		if edge.Kind != kind {
			continue
		}
		source, err := q.store.GetNode(edge.SourceQualified)
		if err != nil {
			return err
		}
		if source != nil {
			q.add(NodeToDict(*source), &edge)
		} else {
			q.addEdgeOnly(edge)
		}
	}
	return nil
}

// outgoingEndpoints is the mirror image, with an optional kind requirement on
// the resolved target: `endpoints_for` accepts only an Endpoint node, and
// silently drops a HANDLES edge that points at anything else.
func (q *graphQuery) outgoingEndpoints(qualified, kind, wantTargetKind string) error {
	edges, err := q.store.GetEdgesBySource(qualified)
	if err != nil {
		return err
	}
	for i := range edges {
		edge := edges[i]
		if edge.Kind != kind {
			continue
		}
		target, err := q.store.GetNode(edge.TargetQualified)
		if err != nil {
			return err
		}
		switch {
		case target == nil:
			q.addEdgeOnly(edge)
		case wantTargetKind == "" || target.Kind == wantTargetKind:
			q.add(NodeToDict(*target), &edge)
		}
	}
	return nil
}

// consumersOf answers from DEPENDS_ON_CONFIG edges, matching the exact Spring
// property key and every `prefix.*` ancestor an @ConfigurationProperties class
// could have bound.
func (q *graphQuery) consumersOf() error {
	raw := q.target
	if q.node != nil {
		raw = q.node.Name
	} else {
		raw = strings.TrimPrefix(raw, "config:")
	}
	raw = strings.TrimSuffix(raw, ".*")
	edges, err := releaseConfigConsumers(q.store, normalizeSpringConfigKey(raw))
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for i := range edges {
		edge := edges[i]
		consumer, err := q.store.GetNode(edge.SourceQualified)
		if err != nil {
			return err
		}
		switch {
		case consumer == nil:
			q.addEdgeOnly(edge)
		case !seen[consumer.QualifiedName]:
			q.add(NodeToDict(*consumer), &edge)
			seen[consumer.QualifiedName] = true
		}
	}
	return nil
}

// fileSummary lists every node of a file. The target is resolved through the
// shared graph-path resolver because a graph may hold absolute, repo-relative
// or cwd-relative paths depending on how it was built, while the tool input is
// usually relative to the repository root.
func (q *graphQuery) fileSummary() error {
	for _, graphPath := range ResolveGraphFilePaths(q.store, q.engine.root, []string{q.target}) {
		nodes, err := q.store.GetNodesByFile(graphPath)
		if err != nil {
			return err
		}
		for _, node := range nodes {
			q.add(NodeToDict(node), nil)
		}
	}
	return nil
}

// ── semantic_search_nodes_tool ───────────────────────────────────────────────

// SearchHit is one ranked hybrid-search result: the node and its final fused,
// boosted score.
type SearchHit struct {
	Node  graphstore.GraphNode
	Score float64
}

// rrfK is search.rrf_merge's Reciprocal Rank Fusion constant. A higher value
// flattens the contribution of rank differences.
const rrfK = 60

// searchModeNone / searchModeFTS / searchModeKeyword are the reachable values
// of `search_mode`. The release also publishes "hybrid" and "semantic", but
// both require an embedding provider: see the file header.
const (
	searchModeNone    = "none"
	searchModeFTS     = "fts"
	searchModeKeyword = "keyword"
)

func semanticSearchNodesTool(e *Engine, args crgrelease.Args) (any, error) {
	query := args.String("query")
	kind := args.String("kind")
	limit := args.Int("limit")
	minimal := args.String("detail_level") == flowDetailMinimal

	store, err := e.readStore()
	if err != nil {
		return nil, err
	}
	hits, mode, err := HybridSearch(store, query, kind, limit, nil)
	if err != nil {
		return nil, err
	}
	results := make([]map[string]any, 0, len(hits))
	for _, hit := range hits {
		results = append(results, SearchHitDict(hit))
	}

	summary := fmt.Sprintf("Found %d node(s) matching '%s'", len(results), query)
	if kind != "" {
		summary += fmt.Sprintf(" (kind=%s)", kind)
	}
	// Zero hits can mean "no such symbol" or "never indexed"/"stale index";
	// only the marker distinguishes them.
	note := ""
	if len(results) == 0 {
		note = emptySearchConfidence(store, e.root, query)
	}

	if minimal {
		visible := results
		if len(visible) > queryMinimalLimit {
			visible = visible[:queryMinimalLimit]
		}
		projected := make([]map[string]any, 0, len(visible))
		for _, result := range visible {
			projected = append(projected, projectKeys(result, "name", "kind", "file_path", "score"))
		}
		response := map[string]any{
			"status": "ok", "query": query, "search_mode": mode,
			"summary": summary, "results": emptyableMaps(projected),
			"result_count": len(results), "results_omitted": len(results) - len(projected),
		}
		if note != "" {
			response["confidence"] = note
		}
		// The minimal branch returns BEFORE generate_hints, so a compact
		// response deliberately carries no `_hints`.
		return response, nil
	}

	response := map[string]any{
		"status": "ok", "query": query, "search_mode": mode,
		"summary": summary, "results": emptyableMaps(results),
	}
	if note != "" {
		response["confidence"] = note
	}
	return e.AttachHints("semantic_search_nodes", response), nil
}

// SearchHitDict is semantic_search_nodes' own result projection. It is NOT
// node_to_dict: it carries the signature and score a search result is judged
// by, and no node id. `language` falls back to the empty string while
// `params`/`return_type`/`signature` stay null, exactly as the release's
// `row["language"] or ""` versus raw column reads do.
func SearchHitDict(hit SearchHit) map[string]any {
	return map[string]any{
		"name":           SanitizeName(hit.Node.Name),
		"qualified_name": SanitizeName(hit.Node.QualifiedName),
		"kind":           hit.Node.Kind,
		"file_path":      hit.Node.FilePath,
		"line_start":     hit.Node.LineStart,
		"line_end":       hit.Node.LineEnd,
		"language":       hit.Node.Language,
		"params":         nullable(hit.Node.Params),
		"return_type":    nullable(hit.Node.ReturnType),
		"signature":      nullable(hit.Node.Signature),
		"score":          roundTo(hit.Score, 6),
	}
}

// HybridSearch is search.hybrid_search on the native backend: FTS5 BM25 ranks
// fused by Reciprocal Rank Fusion, with a keyword LIKE fallback, then query
// boosting, then the kind filter.
//
// Two details are load-bearing and easy to get wrong:
//   - the FTS list is RANK-ordered (unlike releaseSearchNodes, see the file
//     header), and the fused score is a function of rank only, so it is
//     identical for every backend that agrees on the ordering;
//   - the kind filter is applied INSIDE the output loop, AFTER the limit check,
//     so `limit` counts only kind-matching results while the boosted list is
//     consumed in full.
//
// A nil store is the never-built graph, which the release answers as mode
// "none" with no results rather than as an error.
func HybridSearch(
	store graphstore.Store, query, kind string, limit int, contextFiles []string,
) ([]SearchHit, string, error) {
	if store == nil || strings.TrimSpace(query) == "" {
		return nil, searchModeNone, nil
	}
	// `limit` is unguarded by the release's schema, so 0 reaches the SQL as a
	// literal `LIMIT 0`: every ranked list is empty and the mode is "none",
	// which is observably different from `limit: -1` (SQLite reads a negative
	// LIMIT as unbounded, so the mode is still "fts" even though the bounded
	// output is empty). The provider's bound chokepoint maps 0 to its default
	// page size, so the zero case has to be answered before the store is
	// asked anything.
	if limit == 0 {
		return nil, searchModeNone, nil
	}
	fetchLimit := limit * 3

	type ranked struct {
		id    int64
		score float64
	}
	var merged []ranked
	mode := searchModeFTS
	ids, err := store.SearchNodesFTS(query, fetchLimit)
	if err != nil && !errors.Is(err, graphstore.ErrFTSUnsupported) {
		return nil, "", err
	}
	if len(ids) > 0 {
		// Only one ranked list exists without an embedding provider, so RRF
		// reduces to 1/(k + rank + 1) and the list is already sorted.
		merged = make([]ranked, 0, len(ids))
		for rank, id := range ids {
			merged = append(merged, ranked{id: id, score: 1.0 / float64(rrfK+rank+1)})
		}
	} else {
		keyword, err := releaseKeywordSearch(store, query, fetchLimit)
		if err != nil {
			return nil, "", err
		}
		if len(keyword) == 0 {
			return nil, searchModeNone, nil
		}
		mode = searchModeKeyword
		merged = make([]ranked, 0, len(keyword))
		for _, scored := range keyword {
			merged = append(merged, ranked{id: scored.id, score: scored.score})
		}
	}

	candidateIDs := make([]int64, 0, len(merged))
	for _, item := range merged {
		candidateIDs = append(candidateIDs, item.id)
	}
	nodes, err := store.ReadNodesByID(candidateIDs)
	if err != nil {
		return nil, "", err
	}
	byID := make(map[int64]graphstore.GraphNode, len(nodes))
	for _, node := range nodes {
		byID[node.ID] = node
	}

	boosts := detectQueryKindBoost(query)
	context := map[string]bool{}
	for _, file := range contextFiles {
		context[NormalizeFilePath(file)] = true
	}
	lowered := strings.ToLower(query)

	boosted := make([]SearchHit, 0, len(merged))
	for _, item := range merged {
		node, ok := byID[item.id]
		if !ok {
			continue
		}
		boost := 1.0
		if kindBoost, ok := boosts.kinds[node.Kind]; ok {
			boost *= kindBoost
		}
		loweredQualified := strings.ToLower(node.QualifiedName)
		if boosts.qualified > 0 && strings.Contains(query, ".") &&
			strings.Contains(loweredQualified, lowered) {
			boost *= boosts.qualified
		}
		for _, identifier := range boosts.identifiers {
			if strings.Contains(loweredQualified, identifier) {
				boost *= 2.0
				break
			}
		}
		if len(context) > 0 && context[node.FilePath] {
			boost *= 1.5
		}
		boosted = append(boosted, SearchHit{Node: node, Score: item.score * boost})
	}
	// Stable: the release relies on Python's stable sort, so equal scores keep
	// the fused rank order rather than being permuted.
	sort.SliceStable(boosted, func(i, j int) bool { return boosted[i].Score > boosted[j].Score })

	hits := make([]SearchHit, 0, len(boosted))
	for _, hit := range boosted {
		if len(hits) >= limit {
			break
		}
		if kind != "" && hit.Node.Kind != kind {
			continue
		}
		hits = append(hits, hit)
	}
	return hits, mode, nil
}

// queryBoosts is detect_query_kind_boost's output: per-kind multipliers plus
// the two special keys the release stores in the same dict.
type queryBoosts struct {
	kinds       map[string]float64
	qualified   float64
	identifiers []string
}

// detectQueryKindBoost reads intent out of the query's shape: a PascalCase
// query is looking for a type, a snake_case one for a function, a dotted one
// for a qualified name, and any identifier-shaped token anywhere in a
// natural-language question boosts nodes whose qualified name contains it — so
// "Who advances the chain via Context.Next" lands on Context.Next rather than
// on the bare Context class.
func detectQueryKindBoost(query string) queryBoosts {
	boosts := queryBoosts{kinds: map[string]float64{}}
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return boosts
	}
	if startsPascalCase(trimmed) && trimmed != strings.ToUpper(trimmed) {
		boosts.kinds["Class"] = 1.5
		boosts.kinds["Type"] = 1.5
	}
	if strings.Contains(trimmed, "_") && containsASCIILetter(trimmed) {
		boosts.kinds["Function"] = 1.5
	}
	if strings.Contains(trimmed, ".") {
		boosts.qualified = 2.0
	}
	boosts.identifiers = extractQueryIdentifiers(trimmed)
	return boosts
}

// emptySearchConfidence is uncertainty.empty_search_confidence.
func emptySearchConfidence(store graphstore.Store, root, query string) string {
	if store == nil {
		return ConfidenceNote(emptyGraphNote)
	}
	stats, err := store.GetStats()
	if err != nil {
		return ""
	}
	if stats.TotalNodes == 0 {
		return ConfidenceNote(emptyGraphNote)
	}
	if stale, _ := GraphStaleness(store, root, ""); stale != "" {
		return ConfidenceNote(stale)
	}
	return ConfidenceNote(InterpolatedNote("no indexed node matches '", query,
		"'; search covers names, paths and signatures, not source text"))
}

// ── find_large_functions_tool ────────────────────────────────────────────────

func findLargeFunctionsTool(e *Engine, args crgrelease.Args) (any, error) {
	minLines := args.Int("min_lines")
	kind := args.String("kind")
	filePattern := args.String("file_path_pattern")
	limit := args.Int("limit")

	store, err := e.readStore()
	if err != nil {
		return nil, err
	}
	nodes, err := releaseNodesBySize(store, minLines, kind, filePattern, limit)
	if err != nil {
		return nil, err
	}

	root := NormalizeFilePath(e.root)
	results := make([]map[string]any, 0, len(nodes))
	for _, node := range nodes {
		result := NodeToDict(node)
		result["line_count"] = nodeLineCount(node)
		result["relative_path"] = relativeToRoot(root, node.FilePath)
		results = append(results, result)
	}

	head := fmt.Sprintf("Found %d node(s) with >= %d lines", len(results), minLines)
	if kind != "" {
		head += fmt.Sprintf(" (kind=%s)", kind)
	}
	if filePattern != "" {
		head += fmt.Sprintf(" matching '%s'", filePattern)
	}
	lines := []string{head + ":"}
	for i, result := range results {
		if i >= 10 {
			break
		}
		lines = append(lines, fmt.Sprintf("  %4d lines | %8s | %s (%s:%d)",
			result["line_count"], result["kind"], result["name"],
			result["relative_path"], result["line_start"]))
	}
	if len(results) > 10 {
		lines = append(lines, fmt.Sprintf("  ... and %d more", len(results)-10))
	}

	return map[string]any{
		"status":  "ok",
		"summary": strings.Join(lines, "\n"),
		// `total_found` counts the BOUNDED list, not the untruncated match
		// set: the release bounds with a SQL LIMIT and reports len(results).
		// That is why this tool does not use the shared Bounded/ShownOf
		// vocabulary — it has no `*_total` or `truncated` field to fill, and
		// inventing one would report a number the release never sends.
		"total_found": len(results),
		"min_lines":   minLines,
		"results":     emptyableMaps(results),
	}, nil
}

// releaseNodesBySize is GraphStore.get_nodes_by_size: nodes with a line span at
// or above the threshold, largest first, bounded by `limit`.
//
// The ordering is a STABLE sort on the line count, which is what SQLite's
// scan-then-sort produces: equal spans keep ascending id order, and that is
// observable whenever the limit cuts through a run of equally sized nodes.
//
// `limit` is unguarded by the release's schema and reaches SQL verbatim, so it
// carries SQLite's LIMIT semantics: 0 returns nothing at all and a negative
// value is unbounded. Neither is an error.
func releaseNodesBySize(
	store graphstore.Store, minLines int, kind, filePattern string, limit int,
) ([]graphstore.GraphNode, error) {
	if store == nil {
		return nil, nil
	}
	nodes, err := store.ReadAllNodes()
	if err != nil {
		return nil, err
	}
	pattern := ""
	if filePattern != "" {
		pattern = "%" + filePattern + "%"
	}
	matched := make([]graphstore.GraphNode, 0, len(nodes))
	for _, node := range nodes {
		if node.LineStart == 0 || node.LineEnd == 0 || isVerilogSignal(node) {
			continue
		}
		if nodeLineCount(node) < minLines {
			continue
		}
		if kind != "" && node.Kind != kind {
			continue
		}
		if pattern != "" && !sqlLike(node.FilePath, pattern) {
			continue
		}
		matched = append(matched, node)
	}
	sort.SliceStable(matched, func(i, j int) bool {
		return nodeLineCount(matched[i]) > nodeLineCount(matched[j])
	})
	if limit >= 0 && len(matched) > limit {
		matched = matched[:limit]
	}
	return matched, nil
}

// nodeLineCount is the inclusive span the release reports as `line_count`. A
// node missing either bound counts as 0 rather than as a negative span.
func nodeLineCount(node graphstore.GraphNode) int {
	if node.LineStart == 0 || node.LineEnd == 0 {
		return 0
	}
	return node.LineEnd - node.LineStart + 1
}

// relativeToRoot makes a stored path readable, falling back to the absolute
// path when the file is not under the root — the release's ValueError arm.
// Purely lexical, like pathlib's relative_to: no symlink resolution, so a
// stored path and the root must already agree.
func relativeToRoot(root, filePath string) string {
	normalized := NormalizeFilePath(filePath)
	root = strings.TrimSuffix(root, "/")
	switch {
	case normalized == root:
		return "."
	case root != "" && strings.HasPrefix(normalized, root+"/"):
		return normalized[len(root)+1:]
	default:
		return filePath
	}
}

// ── list_graph_stats_tool ────────────────────────────────────────────────────

func listGraphStatsTool(e *Engine, _ crgrelease.Args) (any, error) {
	store, err := e.readStore()
	if err != nil {
		return nil, err
	}

	// Most fields come straight from the store's own aggregate — its SQL is
	// the release's, one COUNT per field. Only the two node-shaped ones need
	// deriving (see statsFromNodes). An unbuilt graph is the release's empty
	// database, so every count is zero rather than an error.
	var stats graphstore.GraphStats
	nodesByKind, edgesByKind := map[string]int{}, map[string]int{}
	languages := []string{}
	if store != nil {
		if stats, err = store.GetStats(); err != nil {
			return nil, err
		}
		if stats.EdgesByKind != nil {
			edgesByKind = stats.EdgesByKind
		}
		if nodesByKind, languages, err = statsFromNodes(store); err != nil {
			return nil, err
		}
	}

	languageList := "none"
	if len(languages) > 0 {
		languageList = strings.Join(languages, ", ")
	}
	lastUpdated := stats.LastUpdated
	lastUpdatedText := lastUpdated
	if lastUpdatedText == "" {
		lastUpdatedText = "never"
	}
	lines := []string{
		fmt.Sprintf("Graph statistics for %s:", path.Base(NormalizeFilePath(e.root))),
		fmt.Sprintf("  Files: %d", stats.FilesCount),
		fmt.Sprintf("  Total nodes: %d", stats.TotalNodes),
		fmt.Sprintf("  Total edges: %d", stats.TotalEdges),
		fmt.Sprintf("  Languages: %s", languageList),
		fmt.Sprintf("  Last updated: %s", lastUpdatedText),
		"",
		"Nodes by kind:",
	}
	lines = append(lines, kindCountLines(nodesByKind)...)
	lines = append(lines, "", "Edges by kind:")
	lines = append(lines, kindCountLines(edgesByKind)...)
	// There is no embedding provider and no `embeddings` table in-process, so
	// the count is structurally 0 and the release's unavailable-provider hint
	// is always the honest line to print.
	lines = append(lines, "", "Embeddings: 0 nodes embedded",
		"  (install sentence-transformers for semantic search)")

	return map[string]any{
		"status":           "ok",
		"summary":          strings.Join(lines, "\n"),
		"total_nodes":      stats.TotalNodes,
		"total_edges":      stats.TotalEdges,
		"nodes_by_kind":    nodesByKind,
		"edges_by_kind":    edgesByKind,
		"languages":        languages,
		"files_count":      stats.FilesCount,
		"last_updated":     nullable(lastUpdated),
		"embeddings_count": 0,
	}, nil
}

// statsFromNodes derives the two node-shaped statistics the store's own
// GetStats cannot express:
//
//   - `nodes_by_kind` re-labels a Verilog signal as kind "Signal", which the
//     release does in SQL over the raw `extra` text;
//   - `languages` is the LIVE FILE inventory, sorted — not every node's
//     language. A virtual or leftover row with no backing File node (a
//     synthetic Spring Event node) must not keep a language alive after its
//     last real file left the graph (#474).
func statsFromNodes(store graphstore.Store) (map[string]int, []string, error) {
	nodes, err := store.ReadAllNodes()
	if err != nil {
		return nil, nil, err
	}
	byKind := map[string]int{}
	seen := map[string]bool{}
	languages := []string{}
	for _, node := range nodes {
		kind := node.Kind
		if isVerilogSignal(node) {
			kind = "Signal"
		}
		byKind[kind]++
		if node.Kind == "File" && node.Language != "" && !seen[node.Language] {
			seen[node.Language] = true
			languages = append(languages, node.Language)
		}
	}
	sort.Strings(languages)
	return byKind, languages, nil
}

// kindCountLines renders a kind→count map as the summary's sorted lines.
func kindCountLines(counts map[string]int) []string {
	kinds := make([]string, 0, len(counts))
	for kind := range counts {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	lines := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		lines = append(lines, fmt.Sprintf("  %s: %d", kind, counts[kind]))
	}
	return lines
}

// ── store readers reproduced from the release ────────────────────────────────
//
// These are GraphStore methods the published Store contract does not expose,
// rebuilt on the readers it does. Each one notes the ordering it depends on,
// because in every case the ordering — not the row set — is the part a
// plausible-looking reimplementation gets wrong.

// releaseSearchNodes is GraphStore.search_nodes: FTS5 first, then a LIKE
// substring fallback, with EVERY word required.
//
// The FTS phase has no ORDER BY, so rows come back in ascending node id and the
// limit truncates BEFORE ordering. The fallback is a table scan in id order,
// also truncated by the limit. See the file header for why this must not be
// confused with hybrid search's rank-ordered read.
func releaseSearchNodes(store graphstore.Store, query string, limit int) ([]graphstore.GraphNode, error) {
	if store == nil {
		return nil, nil
	}
	words := strings.Fields(query)
	if len(words) == 0 {
		return nil, nil
	}
	ids, err := store.SearchNodesFTSWords(words, limit)
	if err != nil && !errors.Is(err, graphstore.ErrFTSUnsupported) {
		return nil, err
	}
	if len(ids) > 0 {
		return store.ReadNodesByID(ids)
	}
	nodes, err := store.ReadAllNodes()
	if err != nil {
		return nil, err
	}
	matched := make([]graphstore.GraphNode, 0, limit)
	for _, node := range nodes {
		if !matchesAllWords(node, words) {
			continue
		}
		matched = append(matched, node)
		if len(matched) >= limit {
			break
		}
	}
	return matched, nil
}

// releaseCountSearchNodes is GraphStore.count_search_nodes: the UNBOUNDED count
// under search_nodes' semantics, which is how the disambiguation response can
// report a real total next to a list capped at twenty.
func releaseCountSearchNodes(store graphstore.Store, query string) (int, error) {
	if store == nil {
		return 0, nil
	}
	words := strings.Fields(query)
	if len(words) == 0 {
		return 0, nil
	}
	count, err := store.CountNodesFTSWords(words)
	if err != nil && !errors.Is(err, graphstore.ErrFTSUnsupported) {
		return 0, err
	}
	if count > 0 {
		return count, nil
	}
	nodes, err := store.ReadAllNodes()
	if err != nil {
		return 0, err
	}
	for _, node := range nodes {
		if matchesAllWords(node, words) {
			count++
		}
	}
	return count, nil
}

// matchesAllWords is search_nodes' LIKE fallback predicate: every word must
// appear, case-insensitively, in the name or the qualified name.
func matchesAllWords(node graphstore.GraphNode, words []string) bool {
	name := strings.ToLower(node.Name)
	qualified := strings.ToLower(node.QualifiedName)
	for _, word := range words {
		lowered := strings.ToLower(word)
		if !strings.Contains(name, lowered) && !strings.Contains(qualified, lowered) {
			return false
		}
	}
	return true
}

// scoredNode is one keyword-fallback hit.
type scoredNode struct {
	id    int64
	score float64
}

// releaseKeywordSearch is search._keyword_search: the LIKE fallback used when
// FTS5 answers nothing, scored exact-name > prefix > contains. Rows are
// collected in id order and the limit is applied BEFORE scoring, so the sort
// is stable over that prefix — the same shape the SQL has. `limit` carries
// SQLite's LIMIT semantics, so a negative value is unbounded.
func releaseKeywordSearch(store graphstore.Store, query string, limit int) ([]scoredNode, error) {
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 || limit == 0 {
		return nil, nil
	}
	nodes, err := store.ReadAllNodes()
	if err != nil {
		return nil, err
	}
	lowered := strings.ToLower(query)
	var results []scoredNode
	for _, node := range nodes {
		if !matchesAllWords(node, words) {
			continue
		}
		name := strings.ToLower(node.Name)
		score := 1.0
		switch {
		case name == lowered:
			score = 3.0
		case strings.HasPrefix(name, lowered):
			score = 2.0
		}
		results = append(results, scoredNode{id: node.ID, score: score})
		if limit > 0 && len(results) >= limit {
			break
		}
	}
	sort.SliceStable(results, func(i, j int) bool { return results[i].score > results[j].score })
	return results, nil
}

// releaseCountNodesByName is GraphStore.count_nodes_by_name, the C++ overload
// probe: more than one same-named signature-bearing node means the bare call
// targets around it cannot be attributed to any single overload.
func releaseCountNodesByName(
	store graphstore.Store, name, language string, kinds ...string,
) (int, error) {
	nodes, err := store.ReadAllNodes()
	if err != nil {
		return 0, err
	}
	wanted := map[string]bool{}
	for _, kind := range kinds {
		wanted[kind] = true
	}
	count := 0
	for _, node := range nodes {
		if node.Name != name {
			continue
		}
		if language != "" && node.Language != language {
			continue
		}
		if len(wanted) > 0 && !wanted[node.Kind] {
			continue
		}
		count++
	}
	return count, nil
}

// jsLanguageFamily is the one language family whose bare edge targets may be
// shared: calls and inheritance routinely cross these source types (JSX is
// stored as JavaScript, Astro as TypeScript). Every other language requires an
// exact match, because a bare name is ambiguous across the whole graph and
// without the filter a common method name like `clone` matches an unrelated
// same-named method in another language (#708).
var jsLanguageFamily = []string{"javascript", "typescript", "tsx"}

// releaseEdgesByTargetName is GraphStore.iter_edges_by_target_name: edges whose
// target is an exact BARE name, optionally restricted to source nodes in a
// compatible language. With a language given, an edge whose source has no node
// row is dropped (the release's INNER JOIN); without one, it is kept.
func releaseEdgesByTargetName(
	store graphstore.Store, name, kind, language string,
) ([]graphstore.GraphEdge, error) {
	edges, err := store.GetEdgesByTarget(name)
	if err != nil {
		return nil, err
	}
	var compatible map[string]bool
	if language != "" {
		compatible = map[string]bool{}
		if isJSFamily(language) {
			for _, family := range jsLanguageFamily {
				compatible[family] = true
			}
		} else {
			compatible[language] = true
		}
	}
	out := make([]graphstore.GraphEdge, 0, len(edges))
	for _, edge := range edges {
		if edge.Kind != kind {
			continue
		}
		if compatible != nil {
			source, err := store.GetNode(edge.SourceQualified)
			if err != nil {
				return nil, err
			}
			if source == nil || !compatible[source.Language] {
				continue
			}
		}
		out = append(out, edge)
	}
	return out, nil
}

func isJSFamily(language string) bool {
	folded := strings.ToLower(language)
	for _, family := range jsLanguageFamily {
		if folded == family {
			return true
		}
	}
	return false
}

// releaseConfigConsumers is GraphStore.get_config_consumers: the exact Spring
// property key plus every `prefix.*` ancestor, so a class bound to
// `app.mail.*` is reported as a consumer of `app.mail.host`. Targets are
// de-duplicated in first-seen order and each is read in edge-id order.
func releaseConfigConsumers(store graphstore.Store, key string) ([]graphstore.GraphEdge, error) {
	parts := strings.Split(key, ".")
	targets := []string{"config:" + key, "config:" + key + ".*"}
	for i := 1; i < len(parts); i++ {
		targets = append(targets, "config:"+strings.Join(parts[:i], ".")+".*")
	}
	seen := map[string]bool{}
	var out []graphstore.GraphEdge
	for _, target := range targets {
		if seen[target] {
			continue
		}
		seen[target] = true
		edges, err := store.GetEdgesByTarget(target)
		if err != nil {
			return nil, err
		}
		matching := make([]graphstore.GraphEdge, 0, len(edges))
		for _, edge := range edges {
			if edge.Kind == "DEPENDS_ON_CONFIG" {
				matching = append(matching, edge)
			}
		}
		sort.SliceStable(matching, func(i, j int) bool { return matching[i].ID < matching[j].ID })
		out = append(out, matching...)
	}
	return out, nil
}

// normalizeSpringConfigKey is config_keys.normalize_spring_config_key: it folds
// a relaxed-binding property name to the lowerCamelCase spelling the graph
// stores, so `my-app.data-source_url`, `MY_APP.DATA_SOURCE_URL` and
// `myApp.dataSourceUrl` all name one property. A `[N]` list index is preserved
// verbatim, because it identifies an element rather than spelling a name.
//
// Note the single-token rule: a segment of one token keeps its case UNLESS the
// whole segment is uppercase. `dataSource` therefore survives intact while
// `DATASOURCE` folds to `datasource` — a segment is only re-cased when its
// original spelling carried no case information to preserve.
func normalizeSpringConfigKey(key string) string {
	segments := strings.Split(strings.TrimSpace(key), ".")
	normalized := make([]string, 0, len(segments))
	for _, segment := range segments {
		base, index := splitSpringIndex(segment)
		tokens := splitSpringTokens(base)
		if len(tokens) == 0 {
			normalized = append(normalized, index)
			continue
		}
		var out strings.Builder
		out.Grow(len(segment))
		if len(tokens) > 1 || isUpperCased(base) {
			out.WriteString(strings.ToLower(tokens[0]))
		} else {
			out.WriteString(tokens[0])
		}
		for _, token := range tokens[1:] {
			runes := []rune(token)
			out.WriteString(strings.ToUpper(string(runes[0])))
			out.WriteString(strings.ToLower(string(runes[1:])))
		}
		out.WriteString(index)
		normalized = append(normalized, out.String())
	}
	return strings.Join(normalized, ".")
}

// splitSpringIndex peels a trailing `[N]` list index off one key segment.
func splitSpringIndex(segment string) (base, index string) {
	if !strings.HasSuffix(segment, "]") {
		return segment, ""
	}
	open := strings.LastIndexByte(segment, '[')
	if open < 0 || open == len(segment)-2 {
		return segment, ""
	}
	for i := open + 1; i < len(segment)-1; i++ {
		if segment[i] < '0' || segment[i] > '9' {
			return segment, ""
		}
	}
	return segment[:open], segment[open:]
}

// splitSpringTokens splits a segment on runs of `-` and `_`, dropping empties.
func splitSpringTokens(base string) []string {
	return strings.FieldsFunc(base, func(r rune) bool { return r == '-' || r == '_' })
}

// isUpperCased is Python's str.isupper(): at least one cased character, and
// every cased character uppercase.
func isUpperCased(value string) bool {
	return strings.ToUpper(value) == value && strings.ToLower(value) != value
}

// transitiveTest is one row of get_transitive_tests: a covering test plus
// whether it was reached through a call rather than directly.
type transitiveTest struct {
	qualifiedName string
	indirect      bool
}

// transitiveTestDepth / transitiveTestFrontier are the release's defaults for
// the CALLS walk. The frontier cap keeps a hub function from turning one query
// into an O(N*M) fan-out.
const (
	transitiveTestDepth    = 1
	transitiveTestFrontier = 50
)

// releaseTransitiveTests is GraphStore.get_transitive_tests.
//
// TESTED_BY is stored source=production, target=test (#515), so coverage is
// found by walking OUT of the node under test. Three passes contribute, in this
// order: direct edges on the node (and, for a class or file, on the symbols it
// contains), an EVIDENCE-GATED bare-name fallback, then one hop along CALLS.
//
// The bare-name gate is the subtle one. A matching name alone is not enough: a
// bare TESTED_BY source is only accepted when exactly one same-named candidate
// lives in the call-site file or in a file that file imports. Without the gate,
// every same-named test in the repository would be attributed to this symbol.
func releaseTransitiveTests(store graphstore.Store, qualifiedName string) ([]transitiveTest, error) {
	nodes, err := store.ReadAllNodes()
	if err != nil {
		return nil, err
	}
	byQualified := make(map[string]graphstore.GraphNode, len(nodes))
	for _, node := range nodes {
		byQualified[node.QualifiedName] = node
	}

	inputs := []string{qualifiedName}
	if node, ok := byQualified[qualifiedName]; ok {
		switch node.Kind {
		case "Class":
			edges, err := store.GetEdgesBySource(qualifiedName)
			if err != nil {
				return nil, err
			}
			for _, edge := range edges {
				if edge.Kind == "CONTAINS" {
					inputs = append(inputs, edge.TargetQualified)
				}
			}
		case "File":
			// A file target must cover methods nested under classes as well as
			// top-level functions, because the public tool accepts a path.
			contained, err := store.GetNodesByFile(node.FilePath)
			if err != nil {
				return nil, err
			}
			for _, symbol := range contained {
				if symbol.QualifiedName == qualifiedName {
					continue
				}
				if symbol.Kind == "Class" || symbol.Kind == "Function" || symbol.Kind == "Method" {
					inputs = append(inputs, symbol.QualifiedName)
				}
			}
		}
	}

	seen := map[string]bool{}
	var results []transitiveTest
	record := func(qualified string, indirect bool) {
		if seen[qualified] {
			return
		}
		if _, ok := byQualified[qualified]; !ok {
			seen[qualified] = true
			return
		}
		seen[qualified] = true
		results = append(results, transitiveTest{qualifiedName: qualified, indirect: indirect})
	}

	for _, input := range inputs {
		edges, err := store.GetEdgesBySource(input)
		if err != nil {
			return nil, err
		}
		for _, edge := range edges {
			if edge.Kind != "TESTED_BY" || hasUnresolvedTargets(edge.Extra) {
				continue
			}
			record(edge.TargetQualified, false)
		}
	}

	bare := qualifiedName
	if index := strings.LastIndex(qualifiedName, "::"); index >= 0 {
		bare = qualifiedName[index+2:]
	}
	bareEdges, err := store.GetEdgesBySource(bare)
	if err != nil {
		return nil, err
	}
	if len(bareEdges) > 0 {
		resolver := newEvidenceResolver(store, nodes)
		for _, edge := range bareEdges {
			if edge.Kind != "TESTED_BY" || hasUnresolvedTargets(edge.Extra) {
				continue
			}
			backed, err := resolver.candidateFor(bare, edge.FilePath)
			if err != nil {
				return nil, err
			}
			if backed != qualifiedName {
				continue
			}
			record(edge.TargetQualified, false)
		}
	}

	frontier := uniqueStrings(inputs)
	for range transitiveTestDepth {
		var next []string
		nextSeen := map[string]bool{}
		for _, current := range frontier {
			edges, err := store.GetEdgesBySource(current)
			if err != nil {
				return nil, err
			}
			for _, edge := range edges {
				if edge.Kind != "CALLS" || hasUnresolvedTargets(edge.Extra) {
					continue
				}
				if !nextSeen[edge.TargetQualified] {
					nextSeen[edge.TargetQualified] = true
					next = append(next, edge.TargetQualified)
				}
			}
		}
		if len(next) > transitiveTestFrontier {
			next = next[:transitiveTestFrontier]
		}
		for _, callee := range next {
			// A bare callee has no stable identity; following TESTED_BY from it
			// would attribute every same-named test.
			if !strings.Contains(callee, "::") {
				continue
			}
			edges, err := store.GetEdgesBySource(callee)
			if err != nil {
				return nil, err
			}
			for _, edge := range edges {
				if edge.Kind != "TESTED_BY" || hasUnresolvedTargets(edge.Extra) {
					continue
				}
				record(edge.TargetQualified, true)
			}
		}
		frontier = next
	}
	return results, nil
}

// evidenceResolver caches the two lookups the bare-name gate needs: the
// same-named candidate symbols, and the set of files a given file imports.
type evidenceResolver struct {
	store      graphstore.Store
	nodes      []graphstore.GraphNode
	candidates map[string][]graphstore.GraphNode
	imports    map[string]map[string]bool
}

func newEvidenceResolver(store graphstore.Store, nodes []graphstore.GraphNode) *evidenceResolver {
	return &evidenceResolver{
		store:      store,
		nodes:      nodes,
		candidates: map[string][]graphstore.GraphNode{},
		imports:    map[string]map[string]bool{},
	}
}

// candidateFor returns the SOLE same-file-or-imported candidate, or "" when
// there is no unique one. Returning "" on ambiguity is the point: the caller
// compares against the symbol it is resolving, so ambiguity means "no match".
func (r *evidenceResolver) candidateFor(name, contextFile string) (string, error) {
	if _, ok := r.candidates[name]; !ok {
		var found []graphstore.GraphNode
		for _, node := range r.nodes {
			if node.Name != name {
				continue
			}
			if node.Kind == "Function" || node.Kind == "Test" || node.Kind == "Class" {
				found = append(found, node)
			}
		}
		r.candidates[name] = found
	}
	if _, ok := r.imports[contextFile]; !ok {
		edges, err := r.store.ReadAllEdges()
		if err != nil {
			return "", err
		}
		imported := map[string]bool{}
		for _, edge := range edges {
			if edge.Kind != "IMPORTS_FROM" || edge.FilePath != contextFile {
				continue
			}
			target := edge.TargetQualified
			if index := strings.Index(target, "::"); index >= 0 {
				target = target[:index]
			}
			imported[target] = true
		}
		r.imports[contextFile] = imported
	}
	supported := ""
	count := 0
	for _, candidate := range r.candidates[name] {
		if candidate.FilePath != contextFile && !r.imports[contextFile][candidate.FilePath] {
			continue
		}
		count++
		if count == 1 {
			supported = candidate.QualifiedName
		}
	}
	if count != 1 {
		return "", nil
	}
	return supported, nil
}

// ── small shared helpers ─────────────────────────────────────────────────────

// hasUnresolvedTargets reports whether an edge carries either candidate-set
// marker, which is how the release tells a recorded relationship from a guess.
func hasUnresolvedTargets(extra map[string]any) bool {
	if extra == nil {
		return false
	}
	if _, ok := extra["ambiguous_targets"]; ok {
		return true
	}
	_, ok := extra["unresolved_targets"]
	return ok
}

// isVerilogSignal reports whether a node is a Verilog signal, which
// `list_graph_stats` re-labels and `find_large_functions` excludes.
//
// The release tests the raw `extra` JSON text with `LIKE '%"verilog_kind"%'`;
// this tests the decoded key, which is where the Verilog parser writes it. The
// two differ only for a node whose extra merely CONTAINS that string in some
// nested value, which no parser produces.
func isVerilogSignal(node graphstore.GraphNode) bool {
	_, ok := node.Extra["verilog_kind"]
	return ok
}

// stringList extracts a list-shaped `extra` value, reporting whether the key
// held a list at all — the release branches on `isinstance(..., list)`, so an
// absent key and a non-list value are the same thing but a present empty list
// is not.
func stringList(value any) ([]string, bool) {
	switch typed := value.(type) {
	case []string:
		return typed, true
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return out, true
	default:
		return nil, false
	}
}

// sanitizeAll sanitizes and caps a candidate list, dropping non-strings the way
// the release's comprehension does.
func sanitizeAll(values []string, limit int) []string {
	if len(values) > limit {
		values = values[:limit]
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, SanitizeName(value))
	}
	return out
}

// extraInt reads an integer out of decoded JSON, which may hold it as a float.
// It deliberately rejects a fractional value: the release requires an `int`,
// and silently truncating would invent a count.
func extraInt(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		if typed == math.Trunc(typed) {
			return int(typed), true
		}
	}
	return 0, false
}

// truthy is Python's `bool(value)` for the decoded `extra` values the release
// passes through it.
func truthy(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case bool:
		return typed
	case string:
		return typed != ""
	case float64:
		return typed != 0
	case int:
		return typed != 0
	case int64:
		return typed != 0
	case []any:
		return len(typed) > 0
	case map[string]any:
		return len(typed) > 0
	default:
		return true
	}
}

// projectKeys is the minimal-mode projection: the listed keys, but only the
// ones actually present, so an absent optional key stays absent rather than
// becoming a null.
func projectKeys(source map[string]any, keys ...string) map[string]any {
	out := make(map[string]any, len(keys))
	for _, key := range keys {
		if value, ok := source[key]; ok {
			out[key] = value
		}
	}
	return out
}

// emptyableMaps guarantees a JSON array rather than a null for an empty list.
func emptyableMaps(items []map[string]any) []map[string]any {
	if items == nil {
		return []map[string]any{}
	}
	return items
}

// nullable renders an empty string as JSON null, which is how the store's
// flattened nullable columns map back onto the release's payloads.
func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// roundTo rounds to a fixed number of decimals, matching Python's round() for
// the score precision the release publishes.
func roundTo(value float64, decimals int) float64 {
	scale := math.Pow(10, float64(decimals))
	return math.Round(value*scale) / scale
}

// uniqueStrings de-duplicates while preserving first-seen order.
func uniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

// lastDotSegment / lastQualifiedSegment / pathStem are the Python one-liners
// the Java-FQN resolver uses, spelled out.
func lastDotSegment(value string) string {
	if index := strings.LastIndex(value, "."); index >= 0 {
		return value[index+1:]
	}
	return value
}

func lastQualifiedSegment(value string) string {
	if index := strings.LastIndex(value, "::"); index >= 0 {
		return value[index+2:]
	}
	return value
}

func pathStem(filePath string) string {
	base := path.Base(NormalizeFilePath(filePath))
	if index := strings.LastIndex(base, "."); index > 0 {
		return base[:index]
	}
	return base
}

// anchorUnderRoot is pathlib's `root / target`: it joins target under the
// repository root, EXCEPT when target is already absolute, in which case
// pathlib discards the root and returns target unchanged. Plain concatenation
// would turn an absolute `/elsewhere/a.go` into `<root>/elsewhere/a.go` and
// look up a path that cannot exist.
func anchorUnderRoot(root, target string) string {
	normalized := NormalizeFilePath(target)
	if strings.HasPrefix(normalized, "/") || hasWindowsDrive(normalized) {
		return normalized
	}
	return path.Join(NormalizeFilePath(root), normalized)
}

// hasWindowsDrive reports a `C:/...` style absolute path, which pathlib treats
// as absolute on Windows and which the graph stores with forward slashes.
func hasWindowsDrive(value string) bool {
	if len(value) < 3 || value[1] != ':' || value[2] != '/' {
		return false
	}
	c := value[0]
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}

// looksLikeJavaMethodFQN reports whether a target has a package/Class/method
// shape. Two segments are accepted only for the conventional `Class.method`
// form, which keeps ordinary dotted filenames and module paths on the legacy
// resolution path.
func looksLikeJavaMethodFQN(target string) bool {
	if strings.Contains(target, "::") {
		return false
	}
	parts := strings.Split(target, ".")
	if len(parts) < 2 {
		return false
	}
	for _, part := range parts {
		if !isJavaIdentifier(part) {
			return false
		}
	}
	if len(parts) >= 3 {
		return true
	}
	first := parts[len(parts)-2]
	return first != "" && first[0] >= 'A' && first[0] <= 'Z'
}

func isJavaIdentifier(part string) bool {
	if part == "" {
		return false
	}
	for i := range len(part) {
		c := part[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c == '_', c == '$':
		case c >= '0' && c <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// startsPascalCase / containsASCIILetter are detect_query_kind_boost's two
// query-shape probes, as character tests rather than regexps.
func startsPascalCase(query string) bool {
	if len(query) < 2 {
		return false
	}
	return query[0] >= 'A' && query[0] <= 'Z' && query[1] >= 'a' && query[1] <= 'z'
}

func containsASCIILetter(query string) bool {
	for i := range len(query) {
		c := query[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
			return true
		}
	}
	return false
}

// queryIdentifierPatterns are search.py's three identifier regexps, VERBATIM
// and in declaration order — the order matters because it is the tie-break on
// the returned token order.
//
// One documented narrowing: Go's `\w` and `\b` are ASCII, Python's are
// Unicode-aware. All three patterns only ever MATCH ASCII, so the difference
// is confined to whether a non-ASCII letter immediately adjacent to an
// otherwise-matching run suppresses the match. That can only change a boost
// multiplier, never which nodes are searched.
var queryIdentifierPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\b[A-Za-z_][\w]*(?:\.[A-Za-z_][\w]*)+\b`),
	regexp.MustCompile(`\b[a-z][a-z0-9]*(?:_[a-z0-9]+)+\b`),
	regexp.MustCompile(`\b[A-Z][a-z0-9]+(?:[A-Z][a-z0-9]+)+\b`),
}

// extractQueryIdentifiers pulls identifier-shaped tokens out of anywhere in a
// query — dotted (`Context.Next`), snake_case (`get_dependant`) and CamelCase
// (`APIRoute`) — even when embedded in a natural-language sentence. That is
// what makes "Who advances the middleware chain via Context.Next" land on
// `Context.Next` rather than on the bare `Context` class. Tokens shorter than
// three characters are dropped as noise.
func extractQueryIdentifiers(query string) []string {
	var found []string
	seen := map[string]bool{}
	for _, pattern := range queryIdentifierPatterns {
		for _, match := range pattern.FindAllString(query, -1) {
			lowered := strings.ToLower(match)
			if len(lowered) >= 3 && !seen[lowered] {
				seen[lowered] = true
				found = append(found, lowered)
			}
		}
	}
	return found
}

// sqlLike evaluates SQLite's default LIKE: `%` matches any run, `_` matches one
// character, and comparison is case-insensitive for ASCII. The release builds
// `'%' + file_path_pattern + '%'` and hands it to SQL, so a pattern containing
// a wildcard genuinely behaves as one.
func sqlLike(value, pattern string) bool {
	value = strings.ToLower(value)
	pattern = strings.ToLower(pattern)
	// Classic linear wildcard match: remember the last `%` and the position it
	// was matched at, so a failure can resume by letting that `%` absorb one
	// more character.
	v, p := 0, 0
	star, mark := -1, 0
	for v < len(value) {
		switch {
		case p < len(pattern) && (pattern[p] == '_' || pattern[p] == value[v]):
			v++
			p++
		case p < len(pattern) && pattern[p] == '%':
			star = p
			p++
			mark = v
		case star >= 0:
			p = star + 1
			mark++
			v = mark
		default:
			return false
		}
	}
	for p < len(pattern) && pattern[p] == '%' {
		p++
	}
	return p == len(pattern)
}

// pythonStringListRepr renders a []string the way `repr(list)` does, because
// the unknown-pattern error embeds the available-pattern list verbatim.
func pythonStringListRepr(values []string) string {
	var out strings.Builder
	out.WriteByte('[')
	for i, value := range values {
		if i > 0 {
			out.WriteString(", ")
		}
		out.WriteString(pythonStringRepr(value))
	}
	out.WriteByte(']')
	return out.String()
}
