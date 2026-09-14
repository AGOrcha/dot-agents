package codegraph

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/crgrelease"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// Parity of the three execution-flow tools against the pinned release's
// tools/call oracle, testdata/crg-release/v2.3.8/calls/*.json.
//
// Every case is a WHOLE-PAYLOAD comparison, key set included. That is the
// point: a projection that emits `steps_omitted: false` where the release
// omits the key, or `total_steps` where the release does not add it, has the
// right values and the wrong shape — and a spot check of two or three keys
// would pass.
//
// The store is seeded DIRECTLY from graph.json rather than by running the
// scanner and the flow derivation, so a failure here is a failure of these
// handlers. The scanner's node identity is pinned by the native-scanner lane
// and the flow rows by the derived-view lane; if this test built the graph it
// would fail for their reasons too and stop being a projection test.

// flowFixtureTools are the tools this file owns. Naming them is what makes
// the coverage test able to fail when a regenerated oracle adds a case
// nothing here exercises.
var flowFixtureTools = []string{
	"list_flows_tool",
	"get_flow_tool",
	"get_affected_flows_tool",
}

// flowRequiredFixtures are the cases whose disappearance from a regenerated
// oracle would silently drop a behaviour this lane is responsible for.
var flowRequiredFixtures = []string{
	"list_flows_tool__defaults",
	"list_flows_tool__minimal",
	"list_flows_tool__sort_by_name",
	"list_flows_tool__sort_by_node_count",
	"list_flows_tool__sort_by_unknown",
	"list_flows_tool__kind_filter",
	"list_flows_tool__kind_filter_empty",
	"list_flows_tool__bound_limit",
	"get_flow_tool__by_id",
	"get_flow_tool__by_name",
	"get_flow_tool__empty_name",
	"get_flow_tool__include_source",
	"get_flow_tool__missing_selector",
	"get_flow_tool__unknown_id",
	"get_flow_tool__bound_max_steps",
	"get_flow_tool__bound_max_source_lines",
	"get_affected_flows_tool__defaults",
	"get_affected_flows_tool__changed_files",
	"get_affected_flows_tool__max_flows",
	"get_affected_flows_tool__no_changes",
}

// flowAutoDetectedFiles is what the fixture repository's second commit
// touched — the generator's SECOND_COMMIT_FILES — and therefore what
// `git diff HEAD~1` reports for the auto-detecting cases.
//
// It is injected through the engine's changed-file seam rather than by
// building a git history here, so this test pins the handler's use of the
// detected list. ReleaseChangedFiles itself is oracled against real git in
// tools_git_test.go.
var flowAutoDetectedFiles = []string{"pkg/auth/token.go"}

// flowErrorPrefix is the wrapper FastMCP puts on an exception that escapes a
// tool, which is why the fixture text is not the bare message. It is uniform
// across every tool, so it belongs to the transport layer and a handler's
// error carries only the message.
const flowErrorPrefix = "Error calling tool '%s': "

// TestFlowToolsMatchReleaseFixtures replays every recorded call for the three
// flow tools against a store seeded from the same graph the release answered
// from.
func TestFlowToolsMatchReleaseFixtures(t *testing.T) {
	fixtures := flowLoadFixtures(t)
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			engine, root := flowSeededEngine(t)
			args := flowBind(t, fixture.Tool, fixture.Arguments)

			handler, ok := toolHandlers[fixture.Tool]
			if !ok {
				t.Fatalf("%s has no native handler", fixture.Tool)
			}
			result, err := handler(engine, args)

			if fixture.IsError {
				flowAssertToolError(t, fixture, result, err)
				return
			}
			if err != nil {
				t.Fatalf("%s: %v", fixture.name, err)
			}
			payload, ok := result.(map[string]any)
			if !ok {
				t.Fatalf("%s: want a dict result, got %T", fixture.name, result)
			}
			want := flowExpectedPayload(t, fixture, root)
			got := flowJSONRoundTrip(t, payload)
			if msg := flowDiff(fixture.name, want, got); msg != "" {
				t.Fatal(msg)
			}
		})
	}
}

// TestFlowToolsFixtureCoverage fails when the oracle loses a case this lane
// is responsible for, which a regeneration could otherwise do silently.
func TestFlowToolsFixtureCoverage(t *testing.T) {
	fixtures := flowLoadFixtures(t)
	present := make([]string, 0, len(fixtures))
	for _, fixture := range fixtures {
		present = append(present, fixture.name)
	}
	for _, required := range flowRequiredFixtures {
		if !slices.Contains(present, required) {
			t.Errorf("fixture %s.json is missing; present: %v", required, present)
		}
	}
}

// TestFlowToolsAttachHintsOnlyOnAnsweredPayloads pins the placement of the
// `_hints` block, which the release generates only after its early returns.
//
// The two early returns are the whole reason this is asserted separately:
// hints are appended by a shared helper, so a handler that called it at the
// top of the function would produce a `_hints` block on a not-found result
// and every fixture with a payload would still pass.
func TestFlowToolsAttachHintsOnlyOnAnsweredPayloads(t *testing.T) {
	for _, tc := range []struct {
		name      string
		tool      string
		arguments string
		wantHints bool
	}{
		{"get_flow no selector", "get_flow_tool", `{}`, false},
		{"get_flow unknown id", "get_flow_tool", `{"flow_id": 999}`, false},
		{"get_flow answered", "get_flow_tool", `{"flow_id": 1}`, true},
		{"affected flows none", "get_affected_flows_tool", `{"changed_files": []}`, false},
		{
			"affected flows answered",
			"get_affected_flows_tool",
			`{"changed_files": ["pkg/auth/auth.go"]}`,
			true,
		},
		{"list flows always", "list_flows_tool", `{}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine, _ := flowSeededEngine(t)
			args := flowBind(t, tc.tool, json.RawMessage(tc.arguments))
			result, err := toolHandlers[tc.tool](engine, args)
			if err != nil {
				t.Fatalf("%s: %v", tc.tool, err)
			}
			payload := result.(map[string]any)
			_, hasHints := payload["_hints"]
			if hasHints != tc.wantHints {
				t.Fatalf("%s: want _hints present=%v, got %v (keys %v)",
					tc.tool, tc.wantHints, hasHints, flowSortedKeys(payload))
			}
		})
	}
}

// TestGetAffectedFlowsUnsetVersusEmptyChangedFiles pins the distinction the
// release's `changed_files is None` check makes.
//
// An UNSET list means "auto-detect from git"; an EMPTY list means "nothing
// changed". Collapsing them — the natural mistake when a list argument is
// read as a plain slice — would make an explicit empty list run a git diff
// and report changes the caller said were not there.
func TestGetAffectedFlowsUnsetVersusEmptyChangedFiles(t *testing.T) {
	for _, tc := range []struct {
		name        string
		arguments   string
		wantDetect  bool
		wantSummary string
	}{
		{"unset auto-detects", `{}`, true, "0 flow(s) affected by changes in 1 file(s)"},
		{"empty means nothing changed", `{"changed_files": []}`, false, "No changed files detected."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine, _ := flowSeededEngine(t)
			detected := false
			engine.changedFiles = func(string, string) ([]string, error) {
				detected = true
				return flowAutoDetectedFiles, nil
			}
			args := flowBind(t, "get_affected_flows_tool", json.RawMessage(tc.arguments))
			result, err := toolHandlers["get_affected_flows_tool"](engine, args)
			if err != nil {
				t.Fatalf("get_affected_flows_tool: %v", err)
			}
			if detected != tc.wantDetect {
				t.Errorf("want git detection called=%v, got %v", tc.wantDetect, detected)
			}
			if summary := result.(map[string]any)["summary"]; summary != tc.wantSummary {
				t.Errorf("want summary %q, got %q", tc.wantSummary, summary)
			}
		})
	}
}

// ── fixtures ─────────────────────────────────────────────────────────────────

// flowCallFixture is one recorded tools/call result.
type flowCallFixture struct {
	name              string
	Tool              string          `json:"tool"`
	Case              string          `json:"case"`
	Arguments         json.RawMessage `json:"arguments"`
	IsError           bool            `json:"is_error"`
	StructuredContent json.RawMessage `json:"structured_content"`
	Content           []struct {
		Text string `json:"text"`
	} `json:"content"`
}

// flowLoadFixtures reads every recorded call for the three flow tools, in a
// deterministic order.
func flowLoadFixtures(t *testing.T) []flowCallFixture {
	t.Helper()
	var fixtures []flowCallFixture
	for _, tool := range flowFixtureTools {
		pattern := filepath.Join(releaseFixtureDir, "calls", tool+"__*.json")
		paths, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatalf("glob %s: %v", pattern, err)
		}
		if len(paths) == 0 {
			t.Fatalf("no recorded calls for %s at %s", tool, pattern)
		}
		sort.Strings(paths)
		for _, path := range paths {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			var fixture flowCallFixture
			if err := json.Unmarshal(raw, &fixture); err != nil {
				t.Fatalf("decode %s: %v", path, err)
			}
			fixture.name = strings.TrimSuffix(filepath.Base(path), ".json")
			fixtures = append(fixtures, fixture)
		}
	}
	return fixtures
}

// flowBind validates the recorded arguments through the pinned release's
// schema, so the handler sees exactly the values a real tools/call delivers,
// every published default included.
func flowBind(t *testing.T, tool string, arguments json.RawMessage) crgrelease.Args {
	t.Helper()
	definition, ok := crgrelease.Lookup(tool)
	if !ok {
		t.Fatalf("%s is not published by code-review-graph %s", tool, crgrelease.Version)
	}
	bound, bindErr := definition.Bind(arguments)
	if bindErr != nil {
		t.Fatalf("bind %s arguments %s: %v", tool, arguments, bindErr)
	}
	return bound
}

// flowExpectedPayload is the recorded payload with ${REPO_ROOT} resolved to
// this run's root and the provenance envelope removed, because
// Engine.CallTool attaches that envelope around a handler rather than inside
// it.
//
// The substitution is done on the recorded JSON TEXT rather than on decoded
// leaves: the token appears embedded inside longer strings — a step's `file`
// and its `<abs path>::<symbol>` qualified name — so a leaf-level token
// comparison would never match it.
func flowExpectedPayload(t *testing.T, fixture flowCallFixture, root string) map[string]any {
	t.Helper()
	if len(fixture.StructuredContent) == 0 {
		t.Fatalf("%s recorded no structured content", fixture.name)
	}
	resolved := strings.ReplaceAll(
		string(fixture.StructuredContent), "${REPO_ROOT}", filepath.ToSlash(root))
	var want map[string]any
	if err := json.Unmarshal([]byte(resolved), &want); err != nil {
		t.Fatalf("%s: decode structured content: %v", fixture.name, err)
	}
	delete(want, "_graph")
	return want
}

// flowDiff reports the first JSON path at which got diverges from want, or ""
// when they agree.
//
// It is a message-returning comparator rather than the package's
// t.Errorf-based assertValue because these payloads are ARRAYS of flow and
// step objects: assertValue compares an array by formatting both sides, which
// answers "they differ" without saying where, and these fixtures are large
// enough that the where is the whole value of the test.
//
// Object comparison checks the KEY SET before any value. Which fields the
// release emits is exactly what these fixtures pin: `steps_omitted: false`
// where the release omits the key has the right values and the wrong shape.
func flowDiff(path string, want, got any) string {
	if token, ok := want.(string); ok && strings.HasPrefix(token, "${") {
		return flowTokenDiff(path, token, got)
	}
	switch expected := want.(type) {
	case map[string]any:
		actual, ok := got.(map[string]any)
		if !ok {
			return fmt.Sprintf("%s: want an object, got %T (%v)", path, got, got)
		}
		if missing, extra := flowKeyDelta(expected, actual); len(missing)+len(extra) > 0 {
			return fmt.Sprintf("%s: key set mismatch; missing %v, unexpected %v", path, missing, extra)
		}
		for _, key := range slices.Sorted(maps.Keys(expected)) {
			if msg := flowDiff(path+"."+key, expected[key], actual[key]); msg != "" {
				return msg
			}
		}
		return ""
	case []any:
		actual, ok := got.([]any)
		if !ok {
			return fmt.Sprintf("%s: want an array, got %T (%v)", path, got, got)
		}
		if len(expected) != len(actual) {
			return fmt.Sprintf("%s: want %d elements, got %d", path, len(expected), len(actual))
		}
		for i := range expected {
			if msg := flowDiff(fmt.Sprintf("%s[%d]", path, i), expected[i], actual[i]); msg != "" {
				return msg
			}
		}
		return ""
	default:
		if !reflect.DeepEqual(want, got) {
			return fmt.Sprintf("%s: want %#v, got %#v", path, want, got)
		}
		return ""
	}
}

// flowTokenDiff checks a value the generator normalized away. A normalized
// field is still asserted to be PRESENT and of the right type and sign, so
// the normalization does not turn it into a free pass, and an unrecognised
// token fails rather than silently comparing against the literal text.
func flowTokenDiff(path, token string, got any) string {
	switch token {
	case "${TIMESTAMP}":
		if text, ok := got.(string); !ok || strings.TrimSpace(text) == "" {
			return fmt.Sprintf("%s: want a timestamp for %s, got %#v", path, token, got)
		}
		return ""
	case "${AGE_SECONDS}", "${DURATION}", "${SAVED_TOKENS}", "${SAVED_PERCENT}":
		number, ok := got.(float64)
		if !ok {
			return fmt.Sprintf("%s: want a number for %s, got %#v", path, token, got)
		}
		if number < 0 {
			return fmt.Sprintf("%s: want a non-negative %s, got %v", path, token, number)
		}
		return ""
	default:
		return fmt.Sprintf("%s: the comparator does not know fixture token %s", path, token)
	}
}

// flowKeyDelta names the fields the payload is missing and the fields it
// should not have emitted at all.
func flowKeyDelta(want, got map[string]any) (missing, extra []string) {
	for _, key := range slices.Sorted(maps.Keys(want)) {
		if _, ok := got[key]; !ok {
			missing = append(missing, key)
		}
	}
	for _, key := range slices.Sorted(maps.Keys(got)) {
		if _, ok := want[key]; !ok {
			extra = append(extra, key)
		}
	}
	return missing, extra
}

// flowAssertToolError checks a bound-violation case, which the release
// surfaces as a TRANSPORT error: `isError: true`, one text block, and no
// payload at all. A handler that answered with an in-band `{"status":
// "error"}` dict would diverge on both.
func flowAssertToolError(t *testing.T, fixture flowCallFixture, result any, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: want an error result, got payload %#v", fixture.name, result)
	}
	if result != nil {
		t.Errorf("%s: want no payload alongside the error, got %#v", fixture.name, result)
	}
	if len(fixture.Content) != 1 {
		t.Fatalf("%s: want exactly one recorded text block, got %d", fixture.name, len(fixture.Content))
	}
	want := strings.TrimPrefix(fixture.Content[0].Text, fmt.Sprintf(flowErrorPrefix, fixture.Tool))
	if err.Error() != want {
		t.Fatalf("%s: want error %q, got %q", fixture.name, want, err.Error())
	}
}

// flowJSONRoundTrip renders a handler payload through JSON, which is how a
// client sees it: the comparison then runs on the same value domain as the
// decoded fixture instead of on Go's int64/float64/[]map distinctions.
func flowJSONRoundTrip(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode payload: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	return decoded
}

func flowSortedKeys(payload map[string]any) []string {
	keys := make([]string, 0, len(payload))
	for key := range payload {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// ── seeded store ─────────────────────────────────────────────────────────────

// flowGraphFixture is the subset of graph.json these tools read: the nodes a
// flow's step list resolves against, and the flow rows themselves.
//
// The nullable text columns are pointers because graph.json records them as
// JSON null, and collapsing null to "" before it reaches the store would hide
// whether the store round-trips the distinction.
type flowGraphFixture struct {
	Nodes []struct {
		ID            int64   `json:"id"`
		Kind          string  `json:"kind"`
		Name          string  `json:"name"`
		QualifiedName string  `json:"qualified_name"`
		FilePath      string  `json:"file_path"`
		LineStart     int     `json:"line_start"`
		LineEnd       int     `json:"line_end"`
		Language      string  `json:"language"`
		ParentName    *string `json:"parent_name"`
		Params        *string `json:"params"`
		ReturnType    *string `json:"return_type"`
		Modifiers     *string `json:"modifiers"`
		IsTest        int     `json:"is_test"`
	} `json:"nodes"`
	Flows []struct {
		ID           int64   `json:"id"`
		Name         string  `json:"name"`
		EntryPointID int64   `json:"entry_point_id"`
		Depth        int     `json:"depth"`
		NodeCount    int     `json:"node_count"`
		FileCount    int     `json:"file_count"`
		Criticality  float64 `json:"criticality"`
		PathJSON     string  `json:"path_json"`
	} `json:"flows"`
}

// flowSeededEngine returns an engine over a copy of the fixture repository
// whose store holds exactly the recorded nodes and flows.
//
// The sources are copied too, not just the rows: `include_source` reads the
// real files, so the snippets it renders have to come from the same bytes the
// release read.
func flowSeededEngine(t *testing.T) (*Engine, string) {
	t.Helper()
	root := t.TempDir()
	copyFixtureTree(t, filepath.Join(releaseFixtureDir, "repo"), root)

	engine := Open(root)
	t.Cleanup(func() { _ = engine.Close() })
	// No test may reach git through the seam by accident: the auto-detecting
	// cases get the fixture repository's second-commit file list.
	engine.changedFiles = func(string, string) ([]string, error) {
		return flowAutoDetectedFiles, nil
	}

	store, err := engine.writeStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	fixture := flowLoadGraph(t, root)
	flowSeedNodes(t, store, fixture)
	flowSeedFlows(t, store, fixture)
	return engine, root
}

// flowLoadGraph decodes graph.json with ${REPO_ROOT} resolved to root.
func flowLoadGraph(t *testing.T, root string) flowGraphFixture {
	t.Helper()
	path := filepath.Join(releaseFixtureDir, "graph.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	// Graph identity is the absolute POSIX path of the file, so the recorded
	// rows are rewritten onto this run's root before they are stored.
	resolved := strings.ReplaceAll(string(raw), "${REPO_ROOT}", filepath.ToSlash(root))
	var fixture flowGraphFixture
	if err := json.Unmarshal([]byte(resolved), &fixture); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	if len(fixture.Nodes) == 0 || len(fixture.Flows) == 0 {
		t.Fatalf("%s carries %d nodes and %d flows", path, len(fixture.Nodes), len(fixture.Flows))
	}
	return fixture
}

// flowSeedNodes inserts the recorded nodes in primary-key order and checks
// that both halves of node identity came out right.
//
// The id check matters because a flow's stored path is a list of node IDS: if
// the seeded ids drifted, every step would resolve to the wrong node and the
// payloads would be wrong in a way that still looked structurally valid. The
// qualified-name check pins that the store derives the release's identity
// from the same inputs, so a step's `qualified_name` is reproduced rather
// than copied from the fixture.
func flowSeedNodes(t *testing.T, store graphstore.Store, fixture flowGraphFixture) {
	t.Helper()
	nodes := fixture.Nodes
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	for _, node := range nodes {
		id, err := store.UpsertNode(graphstore.NodeInfo{
			Kind:       node.Kind,
			Name:       node.Name,
			FilePath:   node.FilePath,
			LineStart:  node.LineStart,
			LineEnd:    node.LineEnd,
			Language:   node.Language,
			ParentName: flowText(node.ParentName),
			Params:     flowText(node.Params),
			ReturnType: flowText(node.ReturnType),
			Modifiers:  flowText(node.Modifiers),
			IsTest:     node.IsTest == 1,
		}, "")
		if err != nil {
			t.Fatalf("seed node %d (%s): %v", node.ID, node.Name, err)
		}
		if id != node.ID {
			t.Fatalf("seed node %s: want id %d, got %d", node.Name, node.ID, id)
		}
	}
	stored, err := store.ReadAllNodes()
	if err != nil {
		t.Fatalf("read back nodes: %v", err)
	}
	if len(stored) != len(nodes) {
		t.Fatalf("want %d stored nodes, got %d", len(nodes), len(stored))
	}
	for i, node := range stored {
		if node.QualifiedName != nodes[i].QualifiedName {
			t.Fatalf("node %d: want qualified name %q, got %q",
				node.ID, nodes[i].QualifiedName, node.QualifiedName)
		}
	}
}

// flowSeedFlows writes the recorded flow rows. The memberships are derived by
// ReplaceFlows from the same paths, which is exactly how the release's
// store_flows keeps the two tables from drifting apart.
func flowSeedFlows(t *testing.T, store graphstore.Store, fixture flowGraphFixture) {
	t.Helper()
	flows := fixture.Flows
	sort.Slice(flows, func(i, j int) bool { return flows[i].ID < flows[j].ID })
	rows := make([]graphstore.FlowRow, 0, len(flows))
	paths := make([][]int64, 0, len(flows))
	for _, flow := range flows {
		var path []int64
		if err := json.Unmarshal([]byte(flow.PathJSON), &path); err != nil {
			t.Fatalf("decode flow %d path %q: %v", flow.ID, flow.PathJSON, err)
		}
		rows = append(rows, graphstore.FlowRow{
			Name:         flow.Name,
			EntryPointID: flow.EntryPointID,
			Depth:        flow.Depth,
			NodeCount:    flow.NodeCount,
			FileCount:    flow.FileCount,
			Criticality:  flow.Criticality,
		})
		paths = append(paths, path)
	}
	written, err := store.ReplaceFlows(rows, paths)
	if err != nil {
		t.Fatalf("seed flows: %v", err)
	}
	if written != len(rows) {
		t.Fatalf("want %d flows written, got %d", len(rows), written)
	}
	stored, err := store.ReadFlows()
	if err != nil {
		t.Fatalf("read back flows: %v", err)
	}
	if len(stored) != len(flows) {
		t.Fatalf("want %d stored flows, got %d", len(flows), len(stored))
	}
	for i, row := range stored {
		if row.ID != flows[i].ID {
			t.Fatalf("flow %q: want id %d, got %d", row.Name, flows[i].ID, row.ID)
		}
		if row.PathJSON == "" {
			t.Fatalf("flow %d stored an empty path", row.ID)
		}
	}
}

// flowText flattens a nullable fixture column to the store's string field.
func flowText(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
