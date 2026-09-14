// Derived-view computations — a port of upstream code-review-graph v2.3.8's
// flow detection (code_review_graph/flows.py), community detection
// (communities.py) and summary-table computation
// (tools/build.py::_compute_summaries).
//
// These are ports, not reimplementations. Every scoring weight, regex,
// keyword set, iteration order, rounding mode and tie-break below is
// reproduced from upstream because the output is a PARITY SURFACE: the values
// land in the schema-v9 derived tables and flow out through the MCP tools, so
// a "cleaner" formula is a behaviour change. The reference values are checked
// in at testdata/crg-release/v2.3.8/graph.json, generated from a real v2.3.8
// install, and postprocess_test.go compares every produced row against them
// field for field.
//
// Five upstream properties are load-bearing and easy to "fix" by accident:
//
//   - Iteration order is observable. Entry-point candidates come back in
//     (kind, id) order because upstream's `kind IN (...)` query is served by
//     idx_nodes_kind; flows are then sorted by criticality with a STABLE
//     sort, so candidate order decides the winner between two equally
//     critical flows. Community grouping iterates nodes in id order, which
//     is what fixes community ids 1..N.
//   - Scores are rounded to 4 decimals with round-HALF-TO-EVEN (Python's
//     round), not half-away-from-zero. See roundTo4.
//   - The JSON text columns are compared byte for byte, so they are written
//     by pyJSONStringArray rather than encoding/json. See that function.
//   - Truncation is by CODE POINT, not byte: Python's len() and slicing are
//     code-point based, so a byte-based cut would differ on any non-ASCII
//     identifier.
//   - CALLS targets that do not resolve to a node are not errors. They are
//     upstream's "external call" signal (they raise criticality) and they are
//     precisely the bare cross-package / method-selector targets the native
//     scanner deliberately leaves unresolved.
//
// Community detection ports upstream's FILE/DIRECTORY-BASED detector — the
// fallback it uses when the optional igraph package is absent. That is the
// path the release fixtures were generated on, so it is the parity target.
// Leiden clustering and _split_oversized (which returns its input unchanged
// without igraph) are not reachable natively and are not emulated.
package crg

import (
	"encoding/json"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// Tuning constants, all upstream defaults.
const (
	// DefaultFlowMaxDepth is upstream trace_flows' max_depth: the BFS stops
	// expanding past this many hops from the entry point.
	DefaultFlowMaxDepth = 15
	// DefaultCommunityMinSize is upstream detect_communities' min_size: a
	// directory group smaller than this is not a community.
	DefaultCommunityMinSize = 2

	// maxSignatureLen is upstream's signature truncation bound, in code
	// points.
	maxSignatureLen = 512
	// maxSanitizedNameLen is graph._sanitize_name's truncation bound, in
	// code points.
	maxSanitizedNameLen = 256
	// slugMaxLen is communities._SLUG_MAX_LEN.
	slugMaxLen = 30
	// keySymbolCount is how many top symbols a community summary keeps, and
	// how many keywords the community namer considers.
	keySymbolCount = 5
	// purposePathSample is how many member file paths the purpose prefix is
	// derived from.
	purposePathSample = 20
	// criticalPathTail bounds flow_snapshots' intermediate node slice: the
	// entry point plus path[1:criticalPathTail].
	criticalPathTail = 4
	// communityGroupTarget is the number of qualifying directory groups the
	// grouping-depth search aims for before it stops deepening.
	communityGroupTarget = 10
	// dominantClassShare is the share of a community's members a single
	// class must exceed to name the community after it.
	dominantClassShare = 0.4
)

// Criticality weights (flows.compute_criticality). They sum to 1.0.
const (
	weightFileSpread = 0.30
	weightExternal   = 0.20
	weightSecurity   = 0.25
	weightTestGap    = 0.15
	weightDepth      = 0.10
)

// Normalisation ceilings for the criticality factors: each factor saturates
// at 1.0 once it reaches these values.
const (
	fileSpreadSaturation = 4.0
	externalSaturation   = 5.0
	depthSaturation      = 10.0
)

// Risk-index scoring constants (tools/build.py's risk block).
const (
	riskManyCallers  = 0.3
	riskSomeCallers  = 0.15
	riskUntested     = 0.3
	riskSecurity     = 0.4
	manyCallers      = 10
	someCallers      = 3
	coverageTested   = "tested"
	coverageUntested = "untested"
)

// securityKeywords is constants.SECURITY_KEYWORDS — the 25 substrings that
// mark a symbol security-sensitive for CRITICALITY scoring. A node counts at
// most once no matter how many keywords it hits.
var securityKeywords = []string{
	"auth", "login", "password", "token", "session", "crypt", "secret",
	"credential", "permission", "sql", "query", "execute", "connect",
	"socket", "request", "http", "sanitize", "validate", "encrypt",
	"decrypt", "hash", "sign", "verify", "admin", "privilege",
}

// riskSecurityKeywords is the SHORTER, separate keyword set inlined in
// tools/build.py's risk_index block. It is deliberately not
// securityKeywords: it omits query/connect/socket/request/http/sanitize/
// validate/encrypt/decrypt/hash/sign/verify/admin/privilege, and it is
// matched against the symbol NAME only (never the qualified name), so a
// symbol in a package called `auth` is NOT security-relevant for risk
// scoring while it IS for criticality. Unifying the two sets — or the two
// match targets — changes both tables' values.
var riskSecurityKeywords = []string{
	"auth", "login", "password", "token", "session", "crypt", "secret",
	"credential", "permission", "sql", "execute",
}

// frameworkDecoratorPatterns is flows._FRAMEWORK_DECORATOR_PATTERNS: a
// decorator matching any of these marks its function a framework-invoked
// entry point (nothing in the graph calls it, but the runtime does).
var frameworkDecoratorPatterns = []*regexp.Regexp{
	// Python web frameworks
	regexp.MustCompile(`(?i)app\.(get|post|put|delete|patch|route|websocket|on_event)`),
	regexp.MustCompile(`(?i)router\.(get|post|put|delete|patch|route)`),
	regexp.MustCompile(`(?i)blueprint\.(route|before_request|after_request)`),
	regexp.MustCompile(`(?i)(before|after)_(request|response)`),
	// CLI frameworks
	regexp.MustCompile(`(?i)click\.(command|group)`),
	regexp.MustCompile(`(?i)\w+\.(command|group)\b`), // Click subgroups: @mygroup.command()
	// Pydantic validators/serializers
	regexp.MustCompile(`(?i)(field|model)_(serializer|validator)`),
	// Task queues
	regexp.MustCompile(`(?i)(celery\.)?(task|shared_task|periodic_task)`),
	// Django
	regexp.MustCompile(`(?i)receiver`),
	regexp.MustCompile(`(?i)api_view`),
	regexp.MustCompile(`(?i)\baction\b`),
	// Testing
	regexp.MustCompile(`pytest\.(fixture|mark)`),
	regexp.MustCompile(`(?i)(override_settings|modify_settings)`),
	// SQLAlchemy / event systems
	regexp.MustCompile(`(?i)(event\.)?listens_for`),
	// Java Spring
	regexp.MustCompile(`(?i)(Get|Post|Put|Delete|Patch|RequestMapping)Mapping`),
	regexp.MustCompile(`(?i)(Scheduled|EventListener|Bean|Configuration)`),
	regexp.MustCompile(`(?i)KafkaListener`),
	// Temporal Java callbacks are invoked by the workflow runtime.
	regexp.MustCompile(`(?i)(WorkflowMethod|ActivityMethod)`),
	// JS/TS frameworks
	regexp.MustCompile(`(?i)(Component|Injectable|Controller|Module|Guard|Pipe)`),
	regexp.MustCompile(`(?i)(Subscribe|Mutation|Query|Resolver)`),
	// Express / Koa / Hono route handlers
	regexp.MustCompile(`(app|router)\.(get|post|put|delete|patch|use|all)\b`),
	// Android lifecycle
	regexp.MustCompile(`(?i)@(Override|OnLifecycleEvent|Composable)`),
	// Kotlin coroutines / Android ViewModel
	regexp.MustCompile(`(?i)(HiltViewModel|AndroidEntryPoint|Inject)`),
	// AI/agent frameworks (pydantic-ai, langchain, etc.)
	regexp.MustCompile(`(?i)\w+\.(tool|tool_plain|system_prompt|result_validator)\b`),
	regexp.MustCompile(`^tool\b`), // bare @tool (LangChain, etc.)
	// Middleware and exception handlers (Starlette, FastAPI, Sanic)
	regexp.MustCompile(`(?i)\w+\.(middleware|exception_handler|on_exception)\b`),
	// Generic route decorator (Flask blueprints: @bp.route, @auth_bp.route)
	regexp.MustCompile(`(?i)\w+\.route\b`),
}

// entryNamePatterns is flows._ENTRY_NAME_PATTERNS: naming conventions that
// mark a function an entry point even when something in the graph calls it.
var entryNamePatterns = []*regexp.Regexp{
	regexp.MustCompile(`^main$`),
	regexp.MustCompile(`^__main__$`),
	regexp.MustCompile(`^test_`),
	regexp.MustCompile(`^Test[A-Z]`),
	regexp.MustCompile(`^on_`),
	regexp.MustCompile(`^handle_`),
	// Lambda / serverless handlers (wired via config, not code calls)
	regexp.MustCompile(`^handler$`),
	regexp.MustCompile(`^handle$`),
	regexp.MustCompile(`^lambda_handler$`),
	// Alembic migration entry points
	regexp.MustCompile(`^upgrade$`),
	regexp.MustCompile(`^downgrade$`),
	// FastAPI lifecycle / dependency injection
	regexp.MustCompile(`^lifespan$`),
	regexp.MustCompile(`^get_db$`),
	// Android Activity/Fragment lifecycle
	regexp.MustCompile(`^on(Create|Start|Resume|Pause|Stop|Destroy|Bind|Receive)`),
	// Servlet / JAX-RS
	regexp.MustCompile(`^do(Get|Post|Put|Delete)$`),
	// Python BaseHTTPRequestHandler
	regexp.MustCompile(`^do_(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)$`),
	regexp.MustCompile(`^log_message$`),
	// Express middleware signature
	regexp.MustCompile(`^(middleware|errorHandler)$`),
	// Angular lifecycle hooks
	regexp.MustCompile(`^ng(OnInit|OnChanges|OnDestroy|DoCheck` +
		`|AfterContentInit|AfterContentChecked|AfterViewInit|AfterViewChecked)$`),
	// Angular Pipe / ControlValueAccessor / Guards / Resolvers
	regexp.MustCompile(`^(transform|writeValue|registerOnChange|registerOnTouched|setDisabledState)$`),
	regexp.MustCompile(`^(canActivate|canDeactivate|canActivateChild|canLoad|canMatch|resolve)$`),
	// React class component lifecycle
	regexp.MustCompile(`^(componentDidMount|componentDidUpdate|componentWillUnmount` +
		`|shouldComponentUpdate|render)$`),
}

// languageEntryNamePatterns is flows._LANGUAGE_ENTRY_NAME_PATTERNS: naming
// conventions scoped to one language so they cannot pollute other parsers.
var languageEntryNamePatterns = map[string][]*regexp.Regexp{
	"php": {
		regexp.MustCompile(`^(boot|register)$`),
		regexp.MustCompile(`^__invoke$`),
	},
}

// testFileRE is flows._TEST_FILE_RE. It is a SECOND, path-based test check
// applied on top of the node's is_test flag, so a production-looking symbol
// living in __tests__/ or a .spec.ts file is still excluded from flow
// analysis.
var testFileRE = regexp.MustCompile(
	`([\\/]__tests__[\\/]|\.spec\.[jt]sx?$|\.test\.[jt]sx?$|[\\/]test_[^/\\]*\.py$)`)

// Identifier-splitting regexes for communities._split_name / _to_slug. The
// split class carries an explicit \v because Go's \s omits vertical tab while
// Python's includes it.
var (
	camelBoundaryRE = regexp.MustCompile(`([a-z])([A-Z])`)
	nameSplitRE     = regexp.MustCompile(`[_\-.\s\v]+`)
	nonAlnumRE      = regexp.MustCompile(`[^A-Za-z0-9]+`)
)

// commonWords is communities._COMMON_WORDS: vocabulary too generic to name a
// community after.
var commonWords = map[string]bool{
	"get": true, "set": true, "self": true, "init": true, "new": true,
	"create": true, "update": true, "delete": true, "add": true,
	"remove": true, "make": true, "build": true, "from": true, "to": true,
	"for": true, "with": true, "the": true, "and": true, "test": true,
	"main": true, "run": true, "do": true, "is": true, "has": true,
	"on": true, "of": true, "in": true, "at": true, "by": true, "my": true,
	"this": true, "that": true, "all": true, "none": true, "should": true,
	"when": true, "then": true, "given": true, "return": true,
	"returns": true, "raise": true, "raises": true, "expect": true,
	"expected": true, "assert": true, "tests": true, "be": true,
	"it": true, "if": true, "not": true,
}

// Summaries is the output of ComputeSummaries: the three pre-computed summary
// tables upstream's _compute_summaries fills. They are grouped into one
// struct because they are computed together from one graph pass and,
// critically, are refreshed together by ONE lifecycle step — a standalone
// postprocess refreshes none of them. See graphstore.CodeGraphDerived.
type Summaries struct {
	CommunitySummaries []graphstore.CommunitySummaryRow
	FlowSnapshots      []graphstore.FlowSnapshotRow
	RiskIndex          []graphstore.RiskIndexRow
}

// graphIndex is upstream's FlowAdjacency plus the entry-point lookup, built
// once per computation so the algorithms below do map lookups instead of
// rescanning the edge slice. Holding node POINTERS into the caller's slice is
// deliberate: the slices are read-only here and a large graph must not be
// copied again.
type graphIndex struct {
	nodes []graphstore.GraphNode

	byQN map[string]*graphstore.GraphNode
	byID map[int64]*graphstore.GraphNode

	// callsOut maps a source qualified name to its CALLS targets in edge
	// order. Targets that are not graph nodes are KEPT: they are the
	// external-call signal criticality scores.
	callsOut map[string][]string
	// hasTestedBy holds the SOURCE of every TESTED_BY edge — the production
	// symbol under test, since upstream stores that edge production -> test.
	hasTestedBy map[string]bool
	// calledQNames holds every CALLS target whose source is not a File node.
	// Excluding file-sourced calls is what keeps a symbol invoked only from
	// module scope visible as an entry point.
	calledQNames map[string]bool
}

// newGraphIndex builds the index in one pass over nodes and one over edges.
func newGraphIndex(nodes []graphstore.GraphNode, edges []graphstore.GraphEdge) *graphIndex {
	g := &graphIndex{
		nodes:        nodes,
		byQN:         make(map[string]*graphstore.GraphNode, len(nodes)),
		byID:         make(map[int64]*graphstore.GraphNode, len(nodes)),
		callsOut:     make(map[string][]string),
		hasTestedBy:  make(map[string]bool),
		calledQNames: make(map[string]bool),
	}
	for i := range nodes {
		n := &nodes[i]
		g.byQN[n.QualifiedName] = n
		g.byID[n.ID] = n
	}
	for _, e := range edges {
		switch e.Kind {
		case graphstore.EdgeKindCalls:
			g.callsOut[e.SourceQualified] = append(g.callsOut[e.SourceQualified], e.TargetQualified)
			if src, ok := g.byQN[e.SourceQualified]; !ok || src.Kind != graphstore.NodeKindFile {
				g.calledQNames[e.TargetQualified] = true
			}
		case graphstore.EdgeKindTestedBy:
			g.hasTestedBy[e.SourceQualified] = true
		}
	}
	return g
}

// ---------------------------------------------------------------------------
// Entry-point detection
// ---------------------------------------------------------------------------

// DetectEntryPoints returns the Function/Test nodes that start an execution
// flow, porting flows.detect_entry_points. A candidate qualifies when ANY of
// three independent signals holds:
//
//  1. nothing in the graph calls it (a true root),
//  2. it carries a framework decorator (the runtime calls it),
//  3. its name matches a conventional entry-point pattern.
//
// They are OR-ed, not tried in priority order: `main` is an entry point even
// when something calls it, which is how a handler that tests also call still
// anchors its own flow.
//
// When includeTests is false (upstream's default, and what the build
// lifecycle uses) Test nodes and nodes in test files are skipped so flow
// analysis describes production paths. Candidates are considered in
// (kind, id) order — see the package comment on why that is observable.
func DetectEntryPoints(nodes []graphstore.GraphNode, edges []graphstore.GraphEdge, includeTests bool) []graphstore.GraphNode {
	return newGraphIndex(nodes, edges).detectEntryPoints(includeTests)
}

func (g *graphIndex) detectEntryPoints(includeTests bool) []graphstore.GraphNode {
	candidates := nodesByKind(g.nodes, graphstore.NodeKindFunction, graphstore.NodeKindTest)

	var entryPoints []graphstore.GraphNode
	seen := make(map[string]bool, len(candidates))
	for _, node := range candidates {
		if !includeTests && (node.IsTest || isTestFile(node.FilePath)) {
			continue
		}
		if _, ok := node.Extra["verilog_kind"]; ok {
			// Upstream skips Verilog constructs: they are hardware
			// declarations rather than callable code, so every one of them
			// would otherwise register as an uncalled "root".
			continue
		}
		isEntry := !g.calledQNames[node.QualifiedName] ||
			hasFrameworkDecorator(node) ||
			matchesEntryName(node)
		if isEntry && !seen[node.QualifiedName] {
			entryPoints = append(entryPoints, node)
			seen[node.QualifiedName] = true
		}
	}
	return entryPoints
}

// nodesByKind filters nodes to the given kinds and orders them by (kind, id),
// reproducing the order upstream's get_nodes_by_kind observably returns (its
// `kind IN (...)` predicate is served by idx_nodes_kind, so SQLite walks the
// index key-first and rowid-second).
func nodesByKind(nodes []graphstore.GraphNode, kinds ...string) []graphstore.GraphNode {
	want := make(map[string]bool, len(kinds))
	for _, k := range kinds {
		want[k] = true
	}
	var out []graphstore.GraphNode
	for _, n := range nodes {
		if want[n.Kind] {
			out = append(out, n)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// isTestFile ports flows._is_test_file.
func isTestFile(filePath string) bool { return testFileRE.MatchString(filePath) }

// hasFrameworkDecorator ports flows._has_framework_decorator.
func hasFrameworkDecorator(node graphstore.GraphNode) bool {
	for _, dec := range decoratorsOf(node) {
		for _, pat := range frameworkDecoratorPatterns {
			if pat.MatchString(dec) {
				return true
			}
		}
	}
	return false
}

// decoratorsOf normalises node.Extra["decorators"] to a string slice.
// Upstream accepts either a single string or a list; the extra map also
// round-trips through JSON on its way out of the store, so a list arrives as
// []any of strings rather than []string.
func decoratorsOf(node graphstore.GraphNode) []string {
	switch v := node.Extra["decorators"].(type) {
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// matchesEntryName ports flows._matches_entry_name, including the
// per-language table.
func matchesEntryName(node graphstore.GraphNode) bool {
	for _, pat := range entryNamePatterns {
		if pat.MatchString(node.Name) {
			return true
		}
	}
	for _, pat := range languageEntryNamePatterns[node.Language] {
		if pat.MatchString(node.Name) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Flow tracing
// ---------------------------------------------------------------------------

// TraceFlows traces one execution flow forward from every entry point and
// returns the `flows` rows plus their node-id paths, ready for
// graphstore.ReplaceFlows. It ports flows.trace_flows.
//
// maxDepth <= 0 uses DefaultFlowMaxDepth. Flows shorter than two nodes are
// dropped: a function whose calls all leave the graph is not a flow. The
// result is sorted by criticality DESCENDING with a stable sort, so equally
// critical flows keep entry-point order.
//
// FlowRow.ID is left zero — the store assigns flow ids on insert.
func TraceFlows(
	nodes []graphstore.GraphNode,
	edges []graphstore.GraphEdge,
	maxDepth int,
	includeTests bool,
) ([]graphstore.FlowRow, [][]int64) {
	g := newGraphIndex(nodes, edges)
	return g.traceFlows(g.detectEntryPoints(includeTests), maxDepth)
}

// traceFlows traces the given entry points and sorts the result.
func (g *graphIndex) traceFlows(entryPoints []graphstore.GraphNode, maxDepth int) ([]graphstore.FlowRow, [][]int64) {
	if maxDepth <= 0 {
		maxDepth = DefaultFlowMaxDepth
	}
	rows := make([]graphstore.FlowRow, 0, len(entryPoints))
	paths := make([][]int64, 0, len(entryPoints))
	for _, ep := range entryPoints {
		row, path, ok := g.traceSingleFlow(ep, maxDepth)
		if !ok {
			continue
		}
		rows = append(rows, row)
		paths = append(paths, path)
	}
	sortFlowsByCriticality(rows, paths)
	return rows, paths
}

// sortFlowsByCriticality sorts rows descending by criticality, keeping paths
// aligned. It is a STABLE sort so equally critical flows keep entry-point
// order, matching Python's list.sort.
func sortFlowsByCriticality(rows []graphstore.FlowRow, paths [][]int64) {
	order := make([]int, len(rows))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		return rows[order[a]].Criticality > rows[order[b]].Criticality
	})
	sortedRows := make([]graphstore.FlowRow, len(rows))
	sortedPaths := make([][]int64, len(paths))
	for newIdx, oldIdx := range order {
		sortedRows[newIdx] = rows[oldIdx]
		sortedPaths[newIdx] = paths[oldIdx]
	}
	copy(rows, sortedRows)
	copy(paths, sortedPaths)
}

// traceSingleFlow ports flows._trace_single_flow: a forward BFS over CALLS
// edges from ep, visiting each reachable node once.
//
// The path is FIRST-SEEN order, not sorted: position 0 is the entry point and
// later positions follow BFS discovery, which is the order
// flow_memberships.position records and flow_snapshots' critical path slices.
// Depth is the maximum depth actually REACHED (so a two-node flow has depth
// 1), never maxDepth. A target that does not resolve to a node is skipped
// WITHOUT being marked visited — it is an external call, counted by
// computeCriticality rather than walked.
func (g *graphIndex) traceSingleFlow(ep graphstore.GraphNode, maxDepth int) (graphstore.FlowRow, []int64, bool) {
	type frontierNode struct {
		qn    string
		depth int
	}

	pathIDs := []int64{ep.ID}
	pathQNames := []string{ep.QualifiedName}
	visited := map[string]bool{ep.QualifiedName: true}
	queue := []frontierNode{{ep.QualifiedName, 0}}
	actualDepth := 0

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur.depth > actualDepth {
			actualDepth = cur.depth
		}
		if cur.depth >= maxDepth {
			continue
		}
		for _, targetQN := range g.callsOut[cur.qn] {
			if visited[targetQN] {
				continue
			}
			target, ok := g.byQN[targetQN]
			if !ok {
				continue
			}
			visited[targetQN] = true
			pathIDs = append(pathIDs, target.ID)
			pathQNames = append(pathQNames, targetQN)
			queue = append(queue, frontierNode{targetQN, cur.depth + 1})
		}
	}

	if len(pathIDs) < 2 {
		return graphstore.FlowRow{}, nil, false
	}

	files := make(map[string]bool, len(pathQNames))
	for _, qn := range pathQNames {
		if n, ok := g.byQN[qn]; ok {
			files[n.FilePath] = true
		}
	}

	row := graphstore.FlowRow{
		Name:         sanitizeName(ep.Name),
		EntryPointID: ep.ID,
		Depth:        actualDepth,
		NodeCount:    len(pathIDs),
		FileCount:    len(files),
	}
	row.Criticality = g.computeCriticality(pathIDs, actualDepth)
	return row, pathIDs, true
}

// sanitizeName ports graph._sanitize_name: strip ASCII control characters
// (except tab and newline) and truncate, so an adversarial symbol name
// extracted from source cannot inject instructions into an MCP tool response.
//
// This is a package-local copy rather than a call into the shared codegraph
// helper because internal/codegraph imports this package — reaching back the
// other way would be an import cycle.
func sanitizeName(s string) string {
	cleaned := strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r >= 0x20 {
			return r
		}
		return -1
	}, s)
	return truncateRunes(cleaned, maxSanitizedNameLen)
}

// truncateRunes cuts s to at most limit CODE POINTS. Python's len() and
// slicing are code-point based, so a byte-based cut would both differ from
// upstream and risk splitting a multi-byte rune.
func truncateRunes(s string, limit int) string {
	count := 0
	for i := range s {
		if count == limit {
			return s[:i]
		}
		count++
	}
	return s
}

// computeCriticality ports flows.compute_criticality: a weighted 0..1 score
// over five factors, rounded to 4 decimals.
//
//	file spread    0.30  how far the flow reaches across files
//	external calls 0.20  calls leaving the indexed graph
//	security       0.25  share of path nodes with a security-ish identifier
//	test-coverage  0.15  share of path nodes with NO test edge
//	depth          0.10  how deep the call chain goes
//
// Each factor is normalised by its saturation constant, so scores are
// comparable across repositories of different sizes.
func (g *graphIndex) computeCriticality(nodeIDs []int64, depth int) float64 {
	if len(nodeIDs) == 0 {
		return 0.0
	}
	nodes := make([]*graphstore.GraphNode, 0, len(nodeIDs))
	for _, id := range nodeIDs {
		if n, ok := g.byID[id]; ok {
			nodes = append(nodes, n)
		}
	}
	if len(nodes) == 0 {
		return 0.0
	}

	// File spread: one file scores 0, fileSpreadSaturation+1 files score 1.
	files := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		files[n.FilePath] = true
	}
	fileSpread := 0.0
	if len(files) > 1 {
		fileSpread = math.Min(float64(len(files)-1)/fileSpreadSaturation, 1.0)
	}

	// External calls: CALLS targets that are not nodes in this graph.
	externalCount := 0
	for _, n := range nodes {
		for _, targetQN := range g.callsOut[n.QualifiedName] {
			if _, ok := g.byQN[targetQN]; !ok {
				externalCount++
			}
		}
	}
	externalScore := math.Min(float64(externalCount)/externalSaturation, 1.0)

	// Security sensitivity: a node hits at most once, however many keywords
	// its name or qualified name contains.
	securityHits := 0
	for _, n := range nodes {
		if containsAnyKeyword(strings.ToLower(n.Name), strings.ToLower(n.QualifiedName), securityKeywords) {
			securityHits++
		}
	}
	securityScore := math.Min(float64(securityHits)/float64(len(nodes)), 1.0)

	// Test-coverage gap: the share of path nodes with no TESTED_BY edge.
	testedCount := 0
	for _, n := range nodes {
		if g.hasTestedBy[n.QualifiedName] {
			testedCount++
		}
	}
	testGap := 1.0 - float64(testedCount)/float64(len(nodes))

	depthScore := math.Min(float64(depth)/depthSaturation, 1.0)

	criticality := fileSpread*weightFileSpread +
		externalScore*weightExternal +
		securityScore*weightSecurity +
		testGap*weightTestGap +
		depthScore*weightDepth
	return roundTo4(math.Min(math.Max(criticality, 0.0), 1.0))
}

// containsAnyKeyword reports whether either lowered string contains any
// keyword. Pass an empty qnLower to match against the name alone.
func containsAnyKeyword(nameLower, qnLower string, keywords []string) bool {
	for _, kw := range keywords {
		if strings.Contains(nameLower, kw) {
			return true
		}
		if qnLower != "" && strings.Contains(qnLower, kw) {
			return true
		}
	}
	return false
}

// roundTo4 rounds to 4 decimal places the way Python's round(x, 4) does:
// round-to-nearest with ties broken to EVEN, over the exact decimal expansion
// of the binary double.
//
// math.Round cannot be used even with scaling — it breaks ties away from
// zero, and the scale/round/unscale dance adds its own representation error.
// strconv.FormatFloat at fixed precision performs exactly Python's rounding
// (strconv's shouldRoundUp is ties-to-even on the exact decimal digits), so
// formatting and re-parsing is both the shortest and the only exactly
// equivalent route. These values are compared field for field against
// upstream's, so "close enough" is not enough.
func roundTo4(x float64) float64 {
	rounded, err := strconv.ParseFloat(strconv.FormatFloat(x, 'f', 4, 64), 64)
	if err != nil {
		return x
	}
	return rounded
}

// IncrementalTraceFlows ports flows.incremental_trace_flows: re-trace only
// the flows a changed-file set can have affected, instead of the whole graph.
//
// A flow is AFFECTED when any of its member nodes lives in a changed file.
// Affected flows are dropped, and the entry points re-traced are those whose
// file changed OR which anchored a dropped flow — the second condition is
// what keeps a flow alive when the change was in its tail rather than at its
// head.
//
// It returns the re-traced flows and paths first (that count is what upstream
// reports as flows_detected) and the untouched flows and paths second. A
// caller persists append(keep, retraced...) through graphstore.ReplaceFlows,
// whose full-table swap is the atomic equivalent of upstream's
// delete-the-affected-rows-then-insert. An empty changedFiles re-traces
// nothing and keeps everything.
func IncrementalTraceFlows(
	nodes []graphstore.GraphNode,
	edges []graphstore.GraphEdge,
	existing []graphstore.FlowRow,
	changedFiles []string,
	maxDepth int,
) (retraced []graphstore.FlowRow, retracedPaths [][]int64, keep []graphstore.FlowRow, keepPaths [][]int64) {
	if len(changedFiles) == 0 {
		return nil, nil, existing, flowPaths(existing)
	}
	changed := make(map[string]bool, len(changedFiles))
	for _, f := range changedFiles {
		changed[f] = true
	}

	g := newGraphIndex(nodes, edges)
	affectedEntryIDs := make(map[int64]bool)
	for _, f := range existing {
		if g.flowTouchesChangedFiles(f, changed) {
			affectedEntryIDs[f.EntryPointID] = true
			continue
		}
		keep = append(keep, f)
		keepPaths = append(keepPaths, decodePath(f.PathJSON))
	}

	var relevant []graphstore.GraphNode
	for _, ep := range g.detectEntryPoints(false) {
		if changed[ep.FilePath] || affectedEntryIDs[ep.ID] {
			relevant = append(relevant, ep)
		}
	}
	retraced, retracedPaths = g.traceFlows(relevant, maxDepth)
	return retraced, retracedPaths, keep, keepPaths
}

// flowTouchesChangedFiles reports whether any node on the flow's path lives
// in a changed file.
func (g *graphIndex) flowTouchesChangedFiles(f graphstore.FlowRow, changed map[string]bool) bool {
	for _, id := range decodePath(f.PathJSON) {
		if n, ok := g.byID[id]; ok && changed[n.FilePath] {
			return true
		}
	}
	return false
}

// decodePath parses a flows.path_json value. A malformed or empty value
// yields no ids rather than an error: path_json is store-owned data, and a
// flow whose path we cannot read is a flow that must be re-traced — which is
// exactly what an empty path causes.
func decodePath(pathJSON string) []int64 {
	if pathJSON == "" {
		return nil
	}
	var ids []int64
	if err := json.Unmarshal([]byte(pathJSON), &ids); err != nil {
		return nil
	}
	return ids
}

// flowPaths decodes each row's stored path, for the no-op incremental case.
func flowPaths(rows []graphstore.FlowRow) [][]int64 {
	if len(rows) == 0 {
		return nil
	}
	paths := make([][]int64, len(rows))
	for i, r := range rows {
		paths[i] = decodePath(r.PathJSON)
	}
	return paths
}

// ---------------------------------------------------------------------------
// Community detection
// ---------------------------------------------------------------------------

// DetectCommunities groups the graph's code nodes into communities and
// returns the `communities` rows plus each community's member qualified
// names, ready for graphstore.ReplaceCommunities.
//
// It ports upstream's DIRECTORY-BASED detector (communities._detect_file_based
// plus _generate_community_name, _compute_cohesion_batch and
// _dedupe_community_names) — the path upstream takes when the optional igraph
// package is absent, which is how the release fixtures were generated and
// therefore the parity target. Leiden clustering is not reachable natively,
// and _split_oversized returns its input unchanged without igraph, so there
// is nothing to port for it.
//
// File nodes are excluded (a file is a container, not a community member), so
// callers pass the FULL node set and must not pre-filter. minSize <= 0 uses
// DefaultCommunityMinSize. CommunityRow.ID is left zero — the store assigns
// community ids on insert, in the returned order.
func DetectCommunities(
	nodes []graphstore.GraphNode,
	edges []graphstore.GraphEdge,
	minSize int,
) ([]graphstore.CommunityRow, [][]string) {
	if minSize <= 0 {
		minSize = DefaultCommunityMinSize
	}
	codeNodes := make([]graphstore.GraphNode, 0, len(nodes))
	for _, n := range nodes {
		if n.Kind != graphstore.NodeKindFile {
			codeNodes = append(codeNodes, n)
		}
	}
	return detectFileBased(codeNodes, edges, minSize)
}

// dirGroup is one directory bucket of nodes. It carries its key so group
// iteration order is explicit rather than Go map order — that order fixes
// community ids and is a parity surface.
type dirGroup struct {
	key     string
	members []graphstore.GraphNode
}

// detectFileBased ports communities._detect_file_based: strip the longest
// common directory prefix from every node's path, then pick a grouping depth.
//
// The depth search aims for at least communityGroupTarget qualifying groups,
// and its exit behaviour is subtle enough to state: when no depth reaches
// that target — the case for any small repository — the loop runs to the
// maximum depth and keeps the DEEPEST grouping, not the first one tried.
func detectFileBased(
	nodes []graphstore.GraphNode,
	edges []graphstore.GraphEdge,
	minSize int,
) ([]graphstore.CommunityRow, [][]string) {
	dirParts := make([][]string, len(nodes))
	for i, n := range nodes {
		dirParts[i] = dirSegments(n.FilePath)
	}
	prefixLen := commonPrefixLen(dirParts)

	maxDepth := 0
	for _, p := range dirParts {
		if d := len(p) - prefixLen; d > maxDepth {
			maxDepth = d
		}
	}

	best := groupAtDepth(nodes, prefixLen, 1)
	for depth := 1; depth <= maxDepth; depth++ {
		groups := groupAtDepth(nodes, prefixLen, depth)
		qualifying := 0
		for _, grp := range groups {
			if len(grp.members) >= minSize {
				qualifying++
			}
		}
		best = groups
		if qualifying >= communityGroupTarget {
			break
		}
	}

	pending := make([]dirGroup, 0, len(best))
	memberSets := make([]map[string]bool, 0, len(best))
	for _, grp := range best {
		if len(grp.members) < minSize {
			continue
		}
		set := make(map[string]bool, len(grp.members))
		for _, m := range grp.members {
			set[m.QualifiedName] = true
		}
		pending = append(pending, grp)
		memberSets = append(memberSets, set)
	}

	cohesions := computeCohesionBatch(memberSets, edges)
	rows := make([]graphstore.CommunityRow, 0, len(pending))
	members := make([][]string, 0, len(pending))
	for i, grp := range pending {
		rows = append(rows, graphstore.CommunityRow{
			Name:             generateCommunityName(grp.members),
			Level:            0,
			Size:             len(grp.members),
			Cohesion:         roundTo4(cohesions[i]),
			DominantLanguage: dominantLanguage(grp.members),
			Description:      "Directory-based community: " + grp.key,
		})
		members = append(members, memberQualifiedNames(grp.members))
	}
	dedupeCommunityNames(rows, members, nodes)
	return rows, members
}

// dirSegments splits a file path into its non-empty DIRECTORY segments,
// dropping the file name. Backslashes are normalised first so a
// Windows-authored path groups with its POSIX twin.
func dirSegments(filePath string) []string {
	segments := strings.Split(strings.ReplaceAll(filePath, `\`, "/"), "/")
	out := make([]string, 0, len(segments))
	for _, p := range segments[:len(segments)-1] {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// commonPrefixLen returns how many leading directory segments every path
// shares. Stripping it is what makes community names repo-relative instead of
// all starting at the filesystem root.
func commonPrefixLen(dirParts [][]string) int {
	if len(dirParts) == 0 {
		return 0
	}
	shortest := len(dirParts[0])
	for _, p := range dirParts {
		if len(p) < shortest {
			shortest = len(p)
		}
	}
	prefixLen := 0
	for i := 0; i < shortest; i++ {
		seg := dirParts[0][i]
		same := true
		for _, p := range dirParts {
			if p[i] != seg {
				same = false
				break
			}
		}
		if !same {
			break
		}
		prefixLen = i + 1
	}
	return prefixLen
}

// groupAtDepth buckets nodes by the first `depth` directory segments after
// the common prefix. Groups come back in FIRST-SEEN node order, which is what
// fixes community ids.
func groupAtDepth(nodes []graphstore.GraphNode, prefixLen, depth int) []dirGroup {
	index := make(map[string]int, len(nodes))
	var groups []dirGroup
	for _, n := range nodes {
		key := groupKey(n.FilePath, prefixLen, depth)
		if i, seen := index[key]; seen {
			groups[i].members = append(groups[i].members, n)
			continue
		}
		index[key] = len(groups)
		groups = append(groups, dirGroup{key: key, members: []graphstore.GraphNode{n}})
	}
	return groups
}

// groupKey is one node's directory bucket at the given depth. A node with
// nothing left after the common prefix — a file at the repo root — falls back
// to its file stem, so root-level files still group together by file.
func groupKey(filePath string, prefixLen, depth int) string {
	dirs := dirSegments(filePath)
	if remainder := dirs[min(prefixLen, len(dirs)):]; len(remainder) > 0 {
		return strings.Join(remainder[:min(depth, len(remainder))], "/")
	}
	segments := strings.Split(strings.ReplaceAll(filePath, `\`, "/"), "/")
	return stripExtension(segments[len(segments)-1])
}

// stripExtension drops the last dot-suffix of a file name, matching Python's
// rsplit(".", 1)[0] — including for a dotfile, whose stem is empty.
func stripExtension(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 {
		return name[:i]
	}
	return name
}

// computeCohesionBatch ports communities._compute_cohesion_batch: cohesion is
// internal / (internal + external) edge count per community, computed for
// every community in ONE pass over the edges.
//
// The single pass is the point. Per-community cohesion is O(edges) each, so
// the naive form is O(edges * communities) and effectively hangs on a large
// repository. Because every caller produces a PARTITION — a node belongs to
// at most one community — a reverse qualified-name -> community index map
// lets one edge walk bucket every community at once. An edge spanning two
// different communities counts as external for BOTH.
func computeCohesionBatch(memberSets []map[string]bool, edges []graphstore.GraphEdge) []float64 {
	qnToIdx := make(map[string]int)
	for idx, members := range memberSets {
		for qn := range members {
			qnToIdx[qn] = idx
		}
	}
	internal := make([]int, len(memberSets))
	external := make([]int, len(memberSets))
	for _, e := range edges {
		src, srcOK := qnToIdx[e.SourceQualified]
		tgt, tgtOK := qnToIdx[e.TargetQualified]
		if !srcOK && !tgtOK {
			continue
		}
		if srcOK && tgtOK && src == tgt {
			internal[src]++
			continue
		}
		if srcOK {
			external[src]++
		}
		if tgtOK {
			external[tgt]++
		}
	}
	out := make([]float64, len(memberSets))
	for i := range memberSets {
		if total := internal[i] + external[i]; total > 0 {
			out[i] = float64(internal[i]) / float64(total)
		}
	}
	return out
}

// memberQualifiedNames returns the members' qualified names in member order.
func memberQualifiedNames(members []graphstore.GraphNode) []string {
	out := make([]string, len(members))
	for i, m := range members {
		out[i] = m.QualifiedName
	}
	return out
}

// dominantLanguage returns the most common non-empty member language,
// breaking ties by first-seen order (Python Counter.most_common(1)).
func dominantLanguage(members []graphstore.GraphNode) string {
	langs := make([]string, 0, len(members))
	for _, m := range members {
		if m.Language != "" {
			langs = append(langs, m.Language)
		}
	}
	top := topCounted(langs, 1)
	if len(top) == 0 {
		return ""
	}
	return top[0]
}

// generateCommunityName ports communities._generate_community_name:
//
//  1. the most common parent-directory name becomes the prefix,
//  2. a class present in more than dominantClassShare of members names the
//     community,
//  3. otherwise the most frequent non-generic keyword does,
//
// joined as "<prefix>-<keyword>". Test nodes are dropped from the naming
// vocabulary whenever any production member exists, so a package is not named
// after its tests.
func generateCommunityName(members []graphstore.GraphNode) string {
	if len(members) == 0 {
		return "empty"
	}
	naming := namingMembers(members)

	filePaths := make([]string, len(naming))
	for i, m := range naming {
		filePaths[i] = m.FilePath
	}
	prefix := extractFilePrefix(filePaths)

	classNames := make([]string, 0, len(naming))
	for _, m := range naming {
		if m.Kind == graphstore.NodeKindClass {
			classNames = append(classNames, m.Name)
		}
	}
	if len(classNames) > 0 {
		topClass := topCounted(classNames, 1)[0]
		if float64(countOf(classNames, topClass)) > float64(len(naming))*dominantClassShare {
			if prefix != "" {
				return prefix + "-" + toSlug(topClass)
			}
			return toSlug(topClass)
		}
	}

	keyword := ""
	if keywords := extractKeywords(naming); len(keywords) > 0 {
		keyword = keywords[0]
	}
	switch {
	case prefix != "" && keyword != "":
		return prefix + "-" + keyword
	case prefix != "":
		return prefix
	case keyword != "":
		return keyword
	default:
		return "cluster"
	}
}

// namingMembers ports communities._naming_members.
func namingMembers(members []graphstore.GraphNode) []graphstore.GraphNode {
	production := make([]graphstore.GraphNode, 0, len(members))
	for _, m := range members {
		if m.Kind != graphstore.NodeKindTest && !m.IsTest {
			production = append(production, m)
		}
	}
	if len(production) > 0 {
		return production
	}
	return members
}

// extractFilePrefix ports communities._extract_file_prefix: the most common
// parent-directory name across the paths, or the file stem for a root-level
// file.
func extractFilePrefix(filePaths []string) string {
	if len(filePaths) == 0 {
		return ""
	}
	parts := make([]string, 0, len(filePaths))
	for _, fp := range filePaths {
		segments := strings.Split(strings.ReplaceAll(fp, `\`, "/"), "/")
		if len(segments) >= 2 {
			parts = append(parts, segments[len(segments)-2])
		} else {
			parts = append(parts, stripExtension(segments[len(segments)-1]))
		}
	}
	return toSlug(topCounted(parts, 1)[0])
}

// extractKeywords ports communities._extract_keywords: the most frequent
// non-generic words in member identifiers, ties broken by first-seen order.
func extractKeywords(members []graphstore.GraphNode) []string {
	var words []string
	for _, m := range members {
		switch m.Kind {
		case graphstore.NodeKindFunction, graphstore.NodeKindClass,
			graphstore.NodeKindTest, graphstore.NodeKindType:
		default:
			continue
		}
		for _, w := range splitName(m.Name) {
			wl := strings.ToLower(w)
			if !commonWords[wl] && len(wl) > 1 {
				words = append(words, wl)
			}
		}
	}
	return topCounted(words, keySymbolCount)
}

// splitName ports communities._split_name: break camelCase, then split on
// underscores, hyphens, dots and whitespace.
func splitName(name string) []string {
	s := camelBoundaryRE.ReplaceAllString(name, "${1}_${2}")
	var out []string
	for _, p := range nameSplitRE.Split(s, -1) {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// toSlug ports communities._to_slug: lowercase hyphenated words, truncated at
// a word boundary inside slugMaxLen where one exists.
//
// Byte and code-point length coincide here: nonAlnumRE has already replaced
// everything outside [A-Za-z0-9] — including every non-ASCII rune — with a
// space, so the slug is ASCII by construction.
func toSlug(s string) string {
	words := splitName(nonAlnumRE.ReplaceAllString(s, " "))
	lowered := make([]string, len(words))
	for i, w := range words {
		lowered[i] = strings.ToLower(w)
	}
	slug := strings.Join(lowered, "-")
	if len(slug) <= slugMaxLen {
		return slug
	}
	if boundary := strings.LastIndex(slug[:min(slugMaxLen+1, len(slug))], "-"); boundary > 0 {
		return slug[:boundary]
	}
	return slug[:slugMaxLen]
}

// topCounted returns the n most frequent values in xs, most frequent first,
// ties broken by FIRST-SEEN order.
//
// That is Python's Counter.most_common(n), which is stable in exactly this
// way — heapq.nlargest decorates each item with a descending index, so equal
// counts come back in insertion order — and the tie-break is what fixes
// community names and community_summaries.key_symbols.
func topCounted(xs []string, n int) []string {
	counts := make(map[string]int, len(xs))
	var order []string
	for _, x := range xs {
		if _, seen := counts[x]; !seen {
			order = append(order, x)
		}
		counts[x]++
	}
	sort.SliceStable(order, func(i, j int) bool { return counts[order[i]] > counts[order[j]] })
	if len(order) > n {
		return order[:n]
	}
	return order
}

// countOf counts occurrences of want in xs.
func countOf(xs []string, want string) int {
	n := 0
	for _, x := range xs {
		if x == want {
			n++
		}
	}
	return n
}

// dedupeCommunityNames ports communities._dedupe_community_names: when two
// communities generate the same name, the LARGEST keeps it and the others
// take a distinguishing keyword suffix (or a numeric one when no unused
// keyword is available). Without it, two `util` directories in different
// trees would be indistinguishable in every tool response.
//
// rows is mutated in place; members[i] supplies rows[i]'s vocabulary.
func dedupeCommunityNames(rows []graphstore.CommunityRow, members [][]string, nodes []graphstore.GraphNode) {
	byName := make(map[string][]int)
	taken := make(map[string]bool, len(rows))
	for i, r := range rows {
		if r.Name == "" {
			continue
		}
		byName[r.Name] = append(byName[r.Name], i)
		taken[r.Name] = true
	}

	nodesByQN := make(map[string]graphstore.GraphNode, len(nodes))
	for _, n := range nodes {
		nodesByQN[n.QualifiedName] = n
	}

	for _, name := range sortedKeys(byName) {
		duplicates := byName[name]
		if len(duplicates) <= 1 {
			continue
		}
		// Largest first; equal sizes keep original position order, so which
		// community keeps the base name is deterministic.
		ordered := append([]int(nil), duplicates...)
		sort.SliceStable(ordered, func(a, b int) bool {
			return rows[ordered[a]].Size > rows[ordered[b]].Size
		})
		baseWords := make(map[string]bool)
		for _, w := range strings.Split(name, "-") {
			baseWords[w] = true
		}
		for _, idx := range ordered[1:] {
			rows[idx].Name = disambiguatedName(name, baseWords, taken, memberNodes(members[idx], nodesByQN))
			taken[rows[idx].Name] = true
		}
	}
}

// disambiguatedName picks the first unused "<base>-<keyword>" from the
// community's own vocabulary, falling back to "<base>-2", "-3", ...
func disambiguatedName(base string, baseWords, taken map[string]bool, members []graphstore.GraphNode) string {
	for _, keyword := range extractKeywords(namingMembers(members)) {
		suffix := toSlug(keyword)
		if suffix == "" || baseWords[suffix] {
			continue
		}
		if candidate := base + "-" + suffix; !taken[candidate] {
			return candidate
		}
	}
	for n := 2; ; n++ {
		if candidate := base + "-" + strconv.Itoa(n); !taken[candidate] {
			return candidate
		}
	}
}

// memberNodes resolves a community's member qualified names back to nodes,
// skipping any that no longer resolve.
func memberNodes(qns []string, nodesByQN map[string]graphstore.GraphNode) []graphstore.GraphNode {
	out := make([]graphstore.GraphNode, 0, len(qns))
	for _, qn := range qns {
		if n, ok := nodesByQN[qn]; ok {
			out = append(out, n)
		}
	}
	return out
}

// sortedKeys returns a map's keys in sorted order, so a rename pass driven by
// map iteration is deterministic.
func sortedKeys(m map[string][]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// CommunitiesAffected reports whether any changed file contains a node that
// already belongs to a community. It ports the guard in
// communities.incremental_detect_communities: when nothing changed inside an
// existing community, upstream SKIPS re-detection entirely and reports 0
// communities — so a caller must not read that 0 as "detection found
// nothing".
func CommunitiesAffected(nodes []graphstore.GraphNode, changedFiles []string) bool {
	if len(changedFiles) == 0 {
		return false
	}
	changed := make(map[string]bool, len(changedFiles))
	for _, f := range changedFiles {
		changed[f] = true
	}
	for _, n := range nodes {
		if n.CommunityID != 0 && changed[n.FilePath] {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Summary tables
// ---------------------------------------------------------------------------

// ComputeSummaries ports tools/build.py::_compute_summaries: the three
// pre-computed summary tables that let review tooling answer questions
// without walking the graph.
//
// It needs nodes whose CommunityID is already ASSIGNED and flows/communities
// whose ids are already ASSIGNED, because the summary rows are keyed by them.
// The caller therefore persists flows and communities first, then RE-READS
// nodes (graphstore.ReplaceCommunities rewrites nodes.community_id) and calls
// this — the same order upstream runs in.
//
// lastComputed is the stamp written to risk_index.last_computed (upstream
// uses SQLite's datetime('now')); passing it in keeps the computation pure
// and the tests deterministic.
func ComputeSummaries(
	nodes []graphstore.GraphNode,
	edges []graphstore.GraphEdge,
	flows []graphstore.FlowRow,
	communities []graphstore.CommunityRow,
	lastComputed string,
) Summaries {
	return Summaries{
		CommunitySummaries: computeCommunitySummaries(nodes, edges, communities),
		FlowSnapshots:      computeFlowSnapshots(nodes, flows),
		RiskIndex:          computeRiskIndex(nodes, edges, lastComputed),
	}
}

// symbolDegree pairs a member name with its total edge degree, for key-symbol
// selection.
type symbolDegree struct {
	name   string
	degree int
}

// computeCommunitySummaries builds one row per community: its top symbols by
// edge degree plus a purpose inferred from the members' file paths.
func computeCommunitySummaries(
	nodes []graphstore.GraphNode,
	edges []graphstore.GraphEdge,
	communities []graphstore.CommunityRow,
) []graphstore.CommunitySummaryRow {
	// Total (in + out) edge degree per qualified name, computed once.
	// Upstream replaced a per-community triple-JOIN with this because the
	// JOIN was its second-worst hang on large graphs.
	degree := make(map[string]int, len(nodes))
	for _, e := range edges {
		degree[e.SourceQualified]++
		degree[e.TargetQualified]++
	}

	byCommunity := make(map[int64][]symbolDegree)
	filesByCommunity := make(map[int64][]string)
	seenFiles := make(map[int64]map[string]bool)
	for _, n := range nodes {
		if n.CommunityID == 0 {
			continue
		}
		if n.Kind != graphstore.NodeKindFile {
			byCommunity[n.CommunityID] = append(byCommunity[n.CommunityID],
				symbolDegree{name: n.Name, degree: degree[n.QualifiedName]})
		}
		if seenFiles[n.CommunityID] == nil {
			seenFiles[n.CommunityID] = map[string]bool{}
		}
		if !seenFiles[n.CommunityID][n.FilePath] {
			seenFiles[n.CommunityID][n.FilePath] = true
			filesByCommunity[n.CommunityID] = append(filesByCommunity[n.CommunityID], n.FilePath)
		}
	}

	out := make([]graphstore.CommunitySummaryRow, 0, len(communities))
	for _, c := range communities {
		out = append(out, graphstore.CommunitySummaryRow{
			CommunityID: c.ID,
			Name:        c.Name,
			Purpose:     purposeFromPaths(filesByCommunity[c.ID]),
			KeySymbols:  pyJSONStringArray(keySymbols(byCommunity[c.ID])),
			// Upstream's INSERT omits `risk`, so the column default
			// applies. Writing anything else here would diverge from the
			// stored row.
			Risk:             "unknown",
			Size:             c.Size,
			DominantLanguage: c.DominantLanguage,
		})
	}
	return out
}

// keySymbols picks the top members by total edge degree. The sort is STABLE,
// so equal degrees keep node order — that tie-break is what makes
// key_symbols reproducible.
func keySymbols(symbols []symbolDegree) []string {
	ordered := append([]symbolDegree(nil), symbols...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].degree > ordered[j].degree })
	names := make([]string, 0, keySymbolCount)
	for _, s := range ordered[:min(keySymbolCount, len(ordered))] {
		names = append(names, s.name)
	}
	return names
}

// purposeFromPaths derives a community's purpose from the last directory
// component its member paths share.
//
// The prefix is CHARACTER-wise, not path-component-wise (upstream uses
// os.path.commonprefix), so ["a/auth.go", "a/auth_test.go"] share "a/auth"
// and the purpose comes from "a" — dropping that partial component is exactly
// what the rsplit does. Only the first purposePathSample paths are
// considered, matching upstream's slice.
func purposeFromPaths(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	prefix := commonStringPrefix(paths[:min(purposePathSample, len(paths))])
	i := strings.LastIndex(prefix, "/")
	if i < 0 {
		return ""
	}
	head := prefix[:i]
	if j := strings.LastIndex(head, "/"); j >= 0 {
		return head[j+1:]
	}
	return head
}

// commonStringPrefix is os.path.commonprefix: the longest common leading
// CHARACTER sequence, with no path awareness.
func commonStringPrefix(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	prefix := paths[0]
	for _, p := range paths[1:] {
		limit := min(len(prefix), len(p))
		i := 0
		for i < limit && prefix[i] == p[i] {
			i++
		}
		prefix = prefix[:i]
		if prefix == "" {
			break
		}
	}
	return prefix
}

// computeFlowSnapshots builds one flattened row per flow, resolving node ids
// to qualified names.
func computeFlowSnapshots(nodes []graphstore.GraphNode, flows []graphstore.FlowRow) []graphstore.FlowSnapshotRow {
	qnByID := make(map[int64]string, len(nodes))
	for _, n := range nodes {
		qnByID[n.ID] = n.QualifiedName
	}
	out := make([]graphstore.FlowSnapshotRow, 0, len(flows))
	for _, f := range flows {
		path := decodePath(f.PathJSON)
		entryName, ok := qnByID[f.EntryPointID]
		if !ok {
			// Upstream falls back to the stringified id so a snapshot row
			// still exists for a flow whose entry node has been deleted.
			entryName = strconv.FormatInt(f.EntryPointID, 10)
		}
		out = append(out, graphstore.FlowSnapshotRow{
			FlowID:       f.ID,
			Name:         f.Name,
			EntryPoint:   entryName,
			CriticalPath: pyJSONStringArray(criticalPath(path, entryName, qnByID)),
			Criticality:  f.Criticality,
			NodeCount:    f.NodeCount,
			FileCount:    f.FileCount,
		})
	}
	return out
}

// criticalPath ports the flow_snapshots critical-path rule: the entry point's
// QUALIFIED name (v2.3.8; v2.2.0 used bare names here), then up to three
// intermediate nodes, then the final node when it is not already listed.
//
// A two-node path therefore yields [entry, last]: the intermediate slice is
// skipped entirely, which is why the two length guards are not redundant.
func criticalPath(path []int64, entryName string, qnByID map[int64]string) []string {
	if len(path) == 0 {
		return nil
	}
	out := []string{entryName}
	if len(path) > 2 {
		for _, id := range path[1:min(criticalPathTail, len(path))] {
			if qn, ok := qnByID[id]; ok {
				out = append(out, qn)
			}
		}
	}
	if len(path) > 1 {
		if last, ok := qnByID[path[len(path)-1]]; ok && !containsString(out, last) {
			out = append(out, last)
		}
	}
	return out
}

// containsString reports whether xs contains want.
func containsString(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

// computeRiskIndex builds the per-symbol review-risk table over Function,
// Class and Test nodes.
//
// Scoring is upstream's, and deliberately coarse — it orders a review queue,
// it is not a metric:
//
//	callers > 10      +0.30   widely depended on
//	callers >  3      +0.15   (an else branch: the two are exclusive)
//	untested          +0.30
//	security-relevant +0.40
//
// capped at 1.0. security_relevant matches the symbol NAME against the
// SHORTER riskSecurityKeywords set — see that variable for why it is not
// securityKeywords.
func computeRiskIndex(
	nodes []graphstore.GraphNode,
	edges []graphstore.GraphEdge,
	lastComputed string,
) []graphstore.RiskIndexRow {
	callerCounts := make(map[string]int)
	testedCounts := make(map[string]int)
	for _, e := range edges {
		switch e.Kind {
		case graphstore.EdgeKindCalls:
			callerCounts[e.TargetQualified]++
		case graphstore.EdgeKindTestedBy:
			testedCounts[e.SourceQualified]++
		}
	}

	candidates := nodesByKind(nodes,
		graphstore.NodeKindFunction, graphstore.NodeKindClass, graphstore.NodeKindTest)
	out := make([]graphstore.RiskIndexRow, 0, len(candidates))
	for _, n := range candidates {
		callerCount := callerCounts[n.QualifiedName]
		coverage := coverageUntested
		if testedCounts[n.QualifiedName] > 0 {
			coverage = coverageTested
		}
		securityRelevant := containsAnyKeyword(strings.ToLower(n.Name), "", riskSecurityKeywords)

		risk := 0.0
		if callerCount > manyCallers {
			risk += riskManyCallers
		} else if callerCount > someCallers {
			risk += riskSomeCallers
		}
		if coverage == coverageUntested {
			risk += riskUntested
		}
		if securityRelevant {
			risk += riskSecurity
		}

		out = append(out, graphstore.RiskIndexRow{
			NodeID:           n.ID,
			QualifiedName:    n.QualifiedName,
			RiskScore:        math.Min(risk, 1.0),
			CallerCount:      callerCount,
			TestCoverage:     coverage,
			SecurityRelevant: securityRelevant,
			LastComputed:     lastComputed,
		})
	}
	return out
}

// pyJSONStringArray renders a string slice exactly as Python's
// json.dumps(list) does, because these values are stored as TEXT in
// community_summaries.key_symbols / flow_snapshots.critical_path and compared
// BYTE FOR BYTE against upstream's rows.
//
// encoding/json cannot be used for either half of that:
//
//   - Separator. json.dumps defaults to ", " between elements; Marshal emits
//     ",".
//   - Escaping. json.dumps defaults to ensure_ascii=True, so every non-ASCII
//     rune becomes \uXXXX (a surrogate pair above the BMP); Marshal emits raw
//     UTF-8. Conversely Marshal HTML-escapes <, > and & into
//     \u003c/\u003e/\u0026 while json.dumps leaves them literal — and those
//     three characters occur in ordinary generic/template symbol names
//     (vector<int>, Map<K,V>), so this is not a theoretical case.
//
// An empty slice renders "[]", never "null".
func pyJSONStringArray(values []string) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, v := range values {
		if i > 0 {
			b.WriteString(", ")
		}
		writePyJSONString(&b, v)
	}
	b.WriteByte(']')
	return b.String()
}

// writePyJSONString writes one Python-json.dumps-escaped string literal:
// backslash and double quote escaped, the short escapes for the control
// characters that have them, and \uXXXX (lowercase hex, surrogate pairs above
// the BMP) for everything else outside printable ASCII.
func writePyJSONString(b *strings.Builder, s string) {
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			switch {
			case r >= 0x20 && r <= 0x7E:
				b.WriteRune(r)
			case r > 0xFFFF:
				hi, lo := utf16.EncodeRune(r)
				writeUnicodeEscape(b, hi)
				writeUnicodeEscape(b, lo)
			default:
				writeUnicodeEscape(b, r)
			}
		}
	}
	b.WriteByte('"')
}

// writeUnicodeEscape writes a single \uXXXX escape with lowercase hex, the
// form Python's json module emits.
func writeUnicodeEscape(b *strings.Builder, r rune) {
	const hexDigits = "0123456789abcdef"
	b.WriteString(`\u`)
	b.WriteByte(hexDigits[(r>>12)&0xF])
	b.WriteByte(hexDigits[(r>>8)&0xF])
	b.WriteByte(hexDigits[(r>>4)&0xF])
	b.WriteByte(hexDigits[r&0xF])
}

// ---------------------------------------------------------------------------
// Signatures
// ---------------------------------------------------------------------------

// NodeSignature renders the declaration string stored in nodes.signature and
// indexed by nodes_fts, porting the signature block of tools/build.py:
//
//	Function / Test -> "def <name>(<params>)", plus " -> <return>" when known
//	Class           -> "class <name>"
//	anything else   -> the node's name (a File node's name is its path)
//
// The "def"/"class" spelling is Python's even for Go symbols: it is what
// upstream writes for every language, it is what the FTS index tokenizes, and
// the release fixtures pin it — e.g. "def Login((user, password string))",
// where the doubled parentheses are upstream's, because params already
// carries its own.
//
// Truncated to maxSignatureLen code points, matching upstream's sig[:512].
func NodeSignature(node graphstore.GraphNode) string {
	var sig string
	switch node.Kind {
	case graphstore.NodeKindFunction, graphstore.NodeKindTest:
		sig = "def " + node.Name + "(" + node.Params + ")"
		if node.ReturnType != "" {
			sig += " -> " + node.ReturnType
		}
	case graphstore.NodeKindClass:
		sig = "class " + node.Name
	default:
		sig = node.Name
	}
	return truncateRunes(sig, maxSignatureLen)
}

// ---------------------------------------------------------------------------
// Bare-endpoint resolution
// ---------------------------------------------------------------------------
//
// A port of GraphStore.resolve_bare_call_targets /
// resolve_bare_tested_by_sources and their shared _resolve_bare_endpoints
// (graph.py). Upstream runs both on every build AND on every standalone
// postprocess, before flow and community detection.
//
// What it is for: the parser resolves a call to a qualified name only when it
// can see the declaration. Everything else is emitted BARE — a name with no
// "::" — which is why testdata/crg-release/v2.3.8/graph.json has CALLS edges
// targeting plain "Login" and "Fatal". This pass promotes a bare endpoint to a
// real node identity, but only on EVIDENCE.
//
// The evidence rule is the whole point, and it is deliberately stricter than
// "the name is unique in the repo": a globally unique match is a coincidence
// waiting to happen, since unrelated packages routinely contain one
// same-named helper. A candidate counts only when it is declared in the
// CALL-SITE FILE, or in a file that call-site file imports; and the endpoint
// is rewritten only when EXACTLY ONE candidate survives that filter. Two
// surviving candidates are recorded as ambiguous and left bare — a wrong edge
// is worse than a missing one, because it silently redirects every flow,
// community and impact query that crosses it.
//
// For Go only the same-file arm can fire: a Go IMPORTS_FROM target is a module
// path, never a file path, so no candidate's defining file can ever appear in
// the import evidence. The arm still fires, and is how a call to a helper
// declared exactly once in the same file resolves. Two upstream evidence
// EXPANSIONS are not ported because nothing in this repo's languages can
// produce their inputs: Python's ambiguous-import candidate list
// (extra.import_resolution == "ambiguous") and C#'s namespace-to-file mapping
// (File node extra.csharp_namespaces). Both widen `imported_files` only.
//
// This pass is a NO-OP on the release fixture repository — every bare target
// there is either declared in another file (Login, ValidateToken, mintToken)
// or not in the graph at all (Fatal) — which is exactly why it has dedicated
// tests rather than relying on the fixture comparison to cover it.

// Extra keys this pass reads and writes. bareCallTargetKey /
// bareTestedBySourceKey record the ORIGINAL bare name, which is what makes
// the rewrite reversible and makes a second pass idempotent (it recognises an
// edge it already managed instead of treating the now-qualified endpoint as
// unresolvable).
const (
	bareCallTargetKey      = "bare_call_target"
	bareTestedBySourceKey  = "bare_tested_by_source"
	ambiguousTargetsKey    = "ambiguous_targets"
	unresolvedTargetsKey   = "unresolved_targets"
	resolutionAmbiguous    = "ambiguous"
	resolutionUnresolved   = "unresolved"
	maxRecordedCandidates  = 20
	qualifiedNameSeparator = "::"
)

// candidateSuffixes are the extra-key suffixes each resolution state writes.
var candidateSuffixes = []string{"_targets", "_target_count", "_targets_truncated"}

// ResolveBareCallTargets resolves bare CALLS targets that have same-file or
// import evidence, porting GraphStore.resolve_bare_call_targets.
//
// It returns the edge rewrites to persist through
// graphstore.ApplyEdgeRewrites, and the count of endpoints it genuinely
// RESOLVED — which is smaller than len(rewrites), because a rewrite is also
// emitted to record ambiguity on an endpoint that stays bare. The count is
// the lifecycle report's bare_edges_resolved.
func ResolveBareCallTargets(
	nodes []graphstore.GraphNode,
	edges []graphstore.GraphEdge,
) ([]graphstore.EdgeRewrite, int) {
	return resolveBareEndpoints(nodes, edges, graphstore.EdgeKindCalls, false)
}

// ResolveBareTestedBySources resolves bare TESTED_BY sources the same way,
// porting GraphStore.resolve_bare_tested_by_sources.
//
// TESTED_BY edges copy the target of a test's CALLS edge, so an unresolved
// cross-file call leaves a bare PRODUCTION source here too — the mirror image
// of the CALLS case, which is why upstream shares one implementation.
func ResolveBareTestedBySources(
	nodes []graphstore.GraphNode,
	edges []graphstore.GraphEdge,
) ([]graphstore.EdgeRewrite, int) {
	return resolveBareEndpoints(nodes, edges, graphstore.EdgeKindTestedBy, true)
}

// nodeCandidate is one declaration a bare name could refer to.
type nodeCandidate struct {
	qualifiedName string
	filePath      string
}

// resolveBareEndpoints is upstream's shared _resolve_bare_endpoints. When
// onSource is true it resolves the edge's source endpoint (TESTED_BY),
// otherwise its target (CALLS).
func resolveBareEndpoints(
	nodes []graphstore.GraphNode,
	edges []graphstore.GraphEdge,
	kind string,
	onSource bool,
) ([]graphstore.EdgeRewrite, int) {
	rawKey := bareCallTargetKey
	if onSource {
		rawKey = bareTestedBySourceKey
	}

	bare := bareEdgesOf(edges, kind, rawKey, onSource)
	if len(bare) == 0 {
		return nil, 0
	}
	byName := declarationsByName(nodes)
	importEvidence := importEvidenceByFile(edges)

	var rewrites []graphstore.EdgeRewrite
	resolved := 0
	for _, e := range bare {
		rewrite, didResolve, ok := resolveOneEndpoint(e, rawKey, onSource, byName, importEvidence)
		if !ok {
			continue
		}
		rewrites = append(rewrites, rewrite)
		if didResolve {
			resolved++
		}
	}
	return rewrites, resolved
}

// bareEdgesOf selects the edges this pass considers: edges of the given kind
// whose relevant endpoint has no "::", plus edges this pass has already
// MANAGED (their extra carries rawKey). Re-selecting managed edges is what
// makes the pass idempotent and lets it revise an earlier verdict when the
// graph has since grown the missing declaration.
func bareEdgesOf(edges []graphstore.GraphEdge, kind, rawKey string, onSource bool) []graphstore.GraphEdge {
	var out []graphstore.GraphEdge
	for _, e := range edges {
		if e.Kind != kind {
			continue
		}
		if _, managed := e.Extra[rawKey]; managed ||
			!strings.Contains(endpointOf(e, onSource), qualifiedNameSeparator) {
			out = append(out, e)
		}
	}
	return out
}

// endpointOf returns the endpoint this pass operates on.
func endpointOf(e graphstore.GraphEdge, onSource bool) string {
	if onSource {
		return e.SourceQualified
	}
	return e.TargetQualified
}

// declarationsByName indexes every declaration a bare name could refer to,
// in node order. Only Function, Test and Class nodes are candidates: a File
// node's identity is a path and can never be a bare call target.
func declarationsByName(nodes []graphstore.GraphNode) map[string][]nodeCandidate {
	out := make(map[string][]nodeCandidate)
	for _, n := range nodes {
		switch n.Kind {
		case graphstore.NodeKindFunction, graphstore.NodeKindTest, graphstore.NodeKindClass:
			out[n.Name] = append(out[n.Name], nodeCandidate{n.QualifiedName, n.FilePath})
		}
	}
	return out
}

// importEvidenceByFile maps each file to the files it imports, derived from
// IMPORTS_FROM targets. A target carrying "::" is a symbol import, so only its
// file part is evidence.
//
// For Go every target is a module path and therefore matches no candidate's
// defining file — the map is built anyway because the rule is shared across
// languages and a language whose imports ARE paths must keep working.
func importEvidenceByFile(edges []graphstore.GraphEdge) map[string]map[string]bool {
	out := make(map[string]map[string]bool)
	for _, e := range edges {
		if e.Kind != graphstore.EdgeKindImportsFrom {
			continue
		}
		targetFile := e.TargetQualified
		if i := strings.Index(targetFile, qualifiedNameSeparator); i >= 0 {
			targetFile = targetFile[:i]
		}
		if out[e.FilePath] == nil {
			out[e.FilePath] = map[string]bool{}
		}
		out[e.FilePath][targetFile] = true
	}
	return out
}

// resolveOneEndpoint decides one edge's fate. It returns the rewrite to
// persist, whether that rewrite is a genuine RESOLUTION (as opposed to an
// ambiguity record), and whether anything needs writing at all.
func resolveOneEndpoint(
	e graphstore.GraphEdge,
	rawKey string,
	onSource bool,
	byName map[string][]nodeCandidate,
	importEvidence map[string]map[string]bool,
) (graphstore.EdgeRewrite, bool, bool) {
	extra := e.Extra
	_, managed := extra[rawKey]
	if !managed && (hasKey(extra, ambiguousTargetsKey) || hasKey(extra, unresolvedTargetsKey)) {
		// Another resolver already declared this endpoint unresolvable and
		// owns its metadata; overwriting it would discard stronger evidence.
		return graphstore.EdgeRewrite{}, false, false
	}

	bareName, ok := bareNameOf(extra, rawKey, endpointOf(e, onSource))
	if !ok {
		return graphstore.EdgeRewrite{}, false, false
	}
	candidates := byName[bareName]
	supported := supportedCandidates(candidates, e.FilePath, importEvidence[e.FilePath])

	// Nothing to say: no evidence-backed candidate, no ambiguity to record,
	// and no prior verdict of ours to revise.
	if len(supported) != 1 && !managed && len(supported) < 2 {
		return graphstore.EdgeRewrite{}, false, false
	}

	desiredExtra := cloneExtra(extra)
	desiredExtra[rawKey] = bareName
	desiredEndpoint := bareName
	if len(supported) == 1 {
		desiredEndpoint = supported[0]
		// A resolved endpoint has no ambiguity left to describe.
		dropCandidateKeys(desiredExtra, resolutionAmbiguous)
		dropCandidateKeys(desiredExtra, resolutionUnresolved)
	} else {
		recordCandidates(desiredExtra, supported, candidates)
	}

	if endpointOf(e, onSource) == desiredEndpoint && sameExtra(extra, desiredExtra) {
		return graphstore.EdgeRewrite{}, false, false
	}

	rewrite := graphstore.EdgeRewrite{
		EdgeID:          e.ID,
		SourceQualified: e.SourceQualified,
		TargetQualified: e.TargetQualified,
		Extra:           desiredExtra,
	}
	if onSource {
		rewrite.SourceQualified = desiredEndpoint
	} else {
		rewrite.TargetQualified = desiredEndpoint
	}
	didResolve := len(supported) == 1 && endpointOf(e, onSource) != desiredEndpoint
	return rewrite, didResolve, true
}

// bareNameOf returns the original bare name: the recorded one for an edge this
// pass already managed, otherwise the endpoint's current value. Reading the
// recorded value is what lets a managed (already-qualified) edge be
// re-evaluated against a changed graph.
func bareNameOf(extra map[string]any, rawKey, endpoint string) (string, bool) {
	recorded, ok := extra[rawKey]
	if !ok {
		return endpoint, true
	}
	name, isString := recorded.(string)
	return name, isString
}

// supportedCandidates filters candidates to those with same-file or
// import-backed evidence, preserving candidate order.
func supportedCandidates(candidates []nodeCandidate, contextFile string, imported map[string]bool) []string {
	var out []string
	for _, c := range candidates {
		if c.filePath == contextFile || imported[c.filePath] {
			out = append(out, c.qualifiedName)
		}
	}
	return out
}

// recordCandidates writes the ambiguity metadata for an endpoint that stays
// bare, and clears the opposite state's keys so the two can never both
// describe the same edge.
//
// Two or more evidence-backed candidates is AMBIGUOUS — the graph genuinely
// cannot choose. Zero is UNRESOLVED, and the recorded list is then every
// same-named declaration anywhere, which is diagnostic rather than
// authoritative: it tells a reader what the name could have meant without
// claiming any of them.
func recordCandidates(extra map[string]any, supported []string, candidates []nodeCandidate) {
	resolution, other := resolutionUnresolved, resolutionAmbiguous
	recorded := supported
	if len(supported) > 1 {
		resolution, other = resolutionAmbiguous, resolutionUnresolved
	} else {
		recorded = make([]string, 0, len(candidates))
		for _, c := range candidates {
			recorded = append(recorded, c.qualifiedName)
		}
	}
	dropCandidateKeys(extra, other)
	total := len(recorded)
	if total > maxRecordedCandidates {
		recorded = recorded[:maxRecordedCandidates]
	}
	extra[resolution+"_targets"] = recorded
	extra[resolution+"_target_count"] = total
	extra[resolution+"_targets_truncated"] = total > maxRecordedCandidates
}

// dropCandidateKeys removes one resolution state's three extra keys.
func dropCandidateKeys(extra map[string]any, resolution string) {
	for _, suffix := range candidateSuffixes {
		delete(extra, resolution+suffix)
	}
}

// hasKey reports whether extra carries key.
func hasKey(extra map[string]any, key string) bool {
	_, ok := extra[key]
	return ok
}

// cloneExtra copies an edge's extra map so a computed verdict never mutates
// the caller's edge slice.
func cloneExtra(extra map[string]any) map[string]any {
	out := make(map[string]any, len(extra)+1)
	for k, v := range extra {
		out[k] = v
	}
	return out
}

// sameExtra compares two extra maps for equality, so an edge whose verdict has
// not changed produces no write.
//
// reflect.DeepEqual is right here rather than a hand-rolled walk: extra is
// decoded from arbitrary JSON, so its values are any-typed nested maps and
// slices with no fixed shape to walk.
func sameExtra(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	return reflect.DeepEqual(a, b)
}
