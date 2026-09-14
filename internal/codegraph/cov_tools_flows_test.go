package codegraph

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// The arms of the three execution-flow tools that the recorded release oracle
// in tools_flows_test.go does not reach: the sort columns no fixture asks
// for, the degraded reads of an unbuilt graph, the corrupted-row and
// store-failure branches, and the per-step source-inlining decisions.
//
// These cases have no recorded payload to compare against — the generator
// cannot make a release's SQLite read fail, and it never recorded a
// `sort_by=depth` call — so each one asserts the contract the handler's own
// documentation states instead. Where a fixture DOES exist for a case it is
// left to tools_flows_test.go, which compares the whole payload.

// covFlowMalformedPath is a stored path blob that is not a JSON id array,
// which is how a corrupted `flows.path_json` column reads. Every handler that
// meets one must report it rather than project the flow as having no steps.
const covFlowMalformedPath = `{"not": "an id array"}`

// covFlowChangedFile is the change set the affected-flow failure tables use.
// It is repository-relative, so the handler has to join it to the root before
// it can match a node.
const covFlowChangedFile = "pkg/auth/auth.go"

// ── list_flows ───────────────────────────────────────────────────────────────

// TestCovFlowListFlowsAppliesEverySortOrder pins the ORDER BY each sort column
// maps to, including the fallback an unrecognised column takes.
//
// The fixture repository's three flows separate the columns: only `main` is
// deeper and larger, every flow spans one file, and byte collation puts
// "ValidateToken" before "main". A handler that sorted ascending, or that
// silently ignored an unknown column instead of falling back to criticality,
// would diverge on a different row order for each case.
func TestCovFlowListFlowsAppliesEverySortOrder(t *testing.T) {
	for _, tc := range []struct {
		name      string
		sortBy    string
		wantOrder []string
	}{
		{"criticality descending", "criticality", []string{"Login", "main", "ValidateToken"}},
		{"depth descending", "depth", []string{"main", "Login", "ValidateToken"}},
		{"node_count descending", "node_count", []string{"main", "Login", "ValidateToken"}},
		{"file_count ties keep row order", "file_count", []string{"Login", "main", "ValidateToken"}},
		{"name ascending under byte collation", "name", []string{"Login", "ValidateToken", "main"}},
		{"an unknown column falls back to criticality", "invented", []string{"Login", "main", "ValidateToken"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine, _ := flowSeededEngine(t)
			payload := covFlowInvoke(t, engine, "list_flows_tool",
				fmt.Sprintf(`{"sort_by": %q}`, tc.sortBy))
			if got := covFlowNames(t, payload, "flows"); !slices.Equal(got, tc.wantOrder) {
				t.Fatalf("sort_by=%s: want %v, got %v", tc.sortBy, tc.wantOrder, got)
			}
		})
	}
}

// TestCovFlowListFlowsReportsStoreFailures pins the in-band failure shape of
// every read list_flows performs.
//
// The failure is a PAYLOAD, not a raised error: the release wraps its tool
// body in `except Exception`, so a client sees `{"status": "error", ...}` with
// no summary and no hints. A handler that returned a Go error instead would
// surface as a transport error and change what the client has to handle.
func TestCovFlowListFlowsReportsStoreFailures(t *testing.T) {
	goodRow := covFlowRow(1, "Login", "[1]", 0.45)
	for _, tc := range []struct {
		name      string
		engine    func(*testing.T) *Engine
		arguments string
		wantError string
	}{
		{
			"the database cannot be opened",
			unreadableEngine,
			`{}`,
			"codegraph: open",
		},
		{
			"the flow table cannot be read",
			covFlowFakeEngine(&fakeStore{flowsErr: errFake}),
			`{}`,
			"injected failure",
		},
		{
			"a stored path is not a JSON id array",
			covFlowFakeEngine(&fakeStore{
				flows: []graphstore.FlowRow{covFlowRow(1, "Login", covFlowMalformedPath, 0.45)},
			}),
			`{}`,
			"decode flow path",
		},
		{
			"the kind filter cannot read node kinds",
			covFlowFakeEngine(&fakeStore{
				flows:       []graphstore.FlowRow{goodRow},
				allNodesErr: errFake,
			}),
			`{"kind": "Function"}`,
			"injected failure",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := covFlowInvoke(t, tc.engine(t), "list_flows_tool", tc.arguments)
			covFlowAssertInBandError(t, payload, tc.wantError)
		})
	}
}

// ── get_flow ─────────────────────────────────────────────────────────────────

// TestCovFlowGetFlowByNameSkipsMoreCriticalNonMatches pins that the name
// search keeps scanning the criticality-ordered list instead of testing only
// its head.
//
// "validatetoken" matches the LEAST critical of the three flows and matches it
// only case-insensitively, so a handler that compared case-sensitively, or
// that gave up after the most critical candidate, reports not_found.
func TestCovFlowGetFlowByNameSkipsMoreCriticalNonMatches(t *testing.T) {
	engine, _ := flowSeededEngine(t)
	payload := covFlowInvoke(t, engine, "get_flow_tool", `{"flow_name": "validatetoken"}`)
	flow, ok := payload["flow"].(map[string]any)
	if !ok {
		t.Fatalf("want a flow, got status %v (keys %v)", payload["status"], flowSortedKeys(payload))
	}
	if flow["name"] != "ValidateToken" || flow["id"] != int64(3) {
		t.Fatalf("want flow 3 ValidateToken, got id %v name %v", flow["id"], flow["name"])
	}
}

// TestCovFlowGetFlowReportsStoreFailures pins get_flow's in-band failures,
// including the two a corrupted `path_json` column produces on each of the
// tool's selection paths.
func TestCovFlowGetFlowReportsStoreFailures(t *testing.T) {
	badRow := covFlowRow(1, "Login", covFlowMalformedPath, 0.45)
	for _, tc := range []struct {
		name      string
		engine    func(*testing.T) *Engine
		arguments string
		wantError string
	}{
		{
			"the database cannot be opened",
			unreadableEngine,
			`{"flow_id": 1}`,
			"codegraph: open",
		},
		{
			"the flow table cannot be read",
			covFlowFakeEngine(&fakeStore{flowsErr: errFake}),
			`{"flow_id": 1}`,
			"injected failure",
		},
		{
			"the name search meets a malformed stored path",
			covFlowFakeEngine(&fakeStore{flows: []graphstore.FlowRow{badRow}}),
			`{"flow_name": "Login"}`,
			"decode flow path",
		},
		{
			"the node index cannot be read",
			covFlowFakeEngine(&fakeStore{
				flows:       []graphstore.FlowRow{covFlowRow(1, "Login", "[1]", 0.45)},
				allNodesErr: errFake,
			}),
			`{"flow_id": 1}`,
			"injected failure",
		},
		{
			"the selected flow has a malformed stored path",
			covFlowFakeEngine(&fakeStore{flows: []graphstore.FlowRow{badRow}}),
			`{"flow_id": 1}`,
			"decode flow path",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := covFlowInvoke(t, tc.engine(t), "get_flow_tool", tc.arguments)
			covFlowAssertInBandError(t, payload, tc.wantError)
		})
	}
}

// TestCovFlowGetFlowStopsInliningSourceAtBudgetExhaustion pins what running
// out of the shared source-line budget does to the payload.
//
// `max_source_lines=1` lets the first of the Login flow's two steps inline its
// opening line and leaves nothing for the second. The second step must then
// carry NO `source` key — not an empty string — and the flow must report both
// `truncated` and `source_truncated`, which is how a client tells "the step
// list was cut" from "every step is here but some carry no source".
func TestCovFlowGetFlowStopsInliningSourceAtBudgetExhaustion(t *testing.T) {
	engine, _ := flowSeededEngine(t)
	payload := covFlowInvoke(t, engine, "get_flow_tool",
		`{"flow_id": 1, "include_source": true, "max_source_lines": 1}`)

	flow, ok := payload["flow"].(map[string]any)
	if !ok {
		t.Fatalf("want a flow, got status %v", payload["status"])
	}
	steps, ok := flow["steps"].([]map[string]any)
	if !ok || len(steps) != 2 {
		t.Fatalf("want the flow's two steps, got %#v", flow["steps"])
	}
	if want := "15: func Login(user, password string) *Session {"; steps[0]["source"] != want {
		t.Fatalf("want the single budgeted line %q, got %q", want, steps[0]["source"])
	}
	if source, present := steps[1]["source"]; present {
		t.Fatalf("the exhausted budget must leave the second step unsourced, got %q", source)
	}
	if flow["truncated"] != true || flow["source_truncated"] != true {
		t.Fatalf("want truncated and source_truncated set, got %v and %v",
			flow["truncated"], flow["source_truncated"])
	}
}

// ── source inlining ──────────────────────────────────────────────────────────

// TestCovFlowAttachSourceStepArms pins the per-step decisions source inlining
// makes, each of which either renders a numbered window or leaves the step
// without a `source` key at all.
//
// Leaving the key OFF rather than setting it empty is the contract: a client
// distinguishes "this step has no readable source" from "its source is the
// empty string" by the key's presence.
func TestCovFlowAttachSourceStepArms(t *testing.T) {
	root := covFlowSourceRoot(t)
	for _, tc := range []struct {
		name       string
		file       string
		lineStart  any
		lineEnd    any
		wantSource string
	}{
		{"a step with no recorded file", "", 1, 2, ""},
		{"a file the graph outlived", "src/absent.go", 1, 2, ""},
		{"a directory is not a step source", "src", 1, 2, ""},
		{
			"a relative step file resolves against the root",
			"src/sample.go", 3, 4,
			"3: func A() {}\n4: func B() {}",
		},
		{
			"a line_start below one starts at the first line",
			"src/sample.go", 0, 2,
			"1: package src\n2: ",
		},
		{
			"a line_end below one runs to the end of the file",
			"src/sample.go", 3, 0,
			"3: func A() {}\n4: func B() {}\n5: func C() {}",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			step := covFlowStep(tc.file, tc.lineStart, tc.lineEnd)
			flow := map[string]any{}
			attachFlowSource(root, flow, []map[string]any{step}, maxFlowSourceLines)
			covFlowAssertStepSource(t, step, flow, tc.wantSource)
		})
	}
}

// TestCovFlowAttachSourceMarksAnUnreadableStepFile pins that a file which
// exists but cannot be read yields the release's in-step marker rather than
// dropping the step or failing the whole tool.
func TestCovFlowAttachSourceMarksAnUnreadableStepFile(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("file permissions do not block reads here")
	}
	root := covFlowSourceRoot(t)
	locked := filepath.Join(root, "src", "sample.go")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o644) })

	step := covFlowStep("src/sample.go", 1, 2)
	flow := map[string]any{}
	attachFlowSource(root, flow, []map[string]any{step}, maxFlowSourceLines)
	if step["source"] != "(could not read file)" {
		t.Fatalf("want the unreadable-file marker, got %q", step["source"])
	}
	if _, truncated := flow["source_truncated"]; truncated {
		t.Fatal("an unreadable file is not a spent budget")
	}
}

// ── flow projection ──────────────────────────────────────────────────────────

// TestCovFlowDecodeStoredPath pins how the stored `path_json` column is read.
//
// The empty and null cases must produce a non-nil empty slice, because the
// value is handed to the JSON encoder and `null` in a payload's `path` is a
// different contract from `[]`. A malformed blob must be an ERROR, because
// reporting no steps would misrepresent the flow as trivial.
func TestCovFlowDecodeStoredPath(t *testing.T) {
	for _, tc := range []struct {
		name      string
		stored    string
		want      []int64
		wantError string
	}{
		{"an unset column is no steps", "", []int64{}, ""},
		{"a stored JSON null is no steps", "null", []int64{}, ""},
		{"an id array keeps its stored order", "[10,8,9]", []int64{10, 8, 9}, ""},
		{"a malformed blob is an error", covFlowMalformedPath, nil, "decode flow path"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decodeFlowPath(tc.stored)
			covFlowAssertPath(t, got, err, tc.want, tc.wantError)
		})
	}
}

// TestCovFlowStepsSkipNodesTheGraphNoLongerHas pins that a flow outliving one
// of its nodes projects the surviving steps instead of failing or emitting a
// hole, which is what an incremental update that removed a symbol leaves
// behind.
func TestCovFlowStepsSkipNodesTheGraphNoLongerHas(t *testing.T) {
	nodes := map[int64]graphstore.GraphNode{
		8: {ID: 8, Kind: "Function", Name: "Login", FilePath: covFlowChangedFile, LineStart: 15, LineEnd: 17},
		9: {ID: 9, Kind: "Function", Name: "hashPassword", FilePath: covFlowChangedFile, LineStart: 19, LineEnd: 21},
	}
	steps := flowSteps([]int64{8, 404, 9}, nodes)
	got := make([]int64, 0, len(steps))
	for _, step := range steps {
		got = append(got, step["node_id"].(int64))
	}
	if want := []int64{8, 9}; !slices.Equal(got, want) {
		t.Fatalf("want the surviving nodes %v in path order, got %v", want, got)
	}
}

// TestCovFlowRowToMapRejectsAMalformedStoredPath pins that projecting a flow
// refuses a corrupted path rather than reporting a deep flow as trivial. The
// decode happens once, in flowRowToMap, and its path feeds flowSteps — so
// this is the only place the corruption can surface.
func TestCovFlowRowToMapRejectsAMalformedStoredPath(t *testing.T) {
	flow, path, err := flowRowToMap(covFlowRow(1, "Login", covFlowMalformedPath, 0.45))
	if err == nil {
		t.Fatalf("want a decode failure, got flow %v with path %v", flow, path)
	}
}

// TestCovFlowReleaseFlowsCutsToLimit pins that the limit is applied AFTER the
// sort, so asking for fewer flows returns the most critical ones rather than
// the first rows the table happened to yield.
func TestCovFlowReleaseFlowsCutsToLimit(t *testing.T) {
	rows := []graphstore.FlowRow{
		covFlowRow(1, "ValidateToken", "[10,11]", 0.41),
		covFlowRow(2, "Login", "[8,9]", 0.45),
		covFlowRow(3, "main", "[2,3,4]", 0.4167),
	}
	flows, err := releaseFlows(rows, "criticality", 2)
	if err != nil {
		t.Fatalf("releaseFlows: %v", err)
	}
	got := make([]string, 0, len(flows))
	for _, flow := range flows {
		got = append(got, flow["name"].(string))
	}
	if want := []string{"Login", "main"}; !slices.Equal(got, want) {
		t.Fatalf("want the two most critical flows %v, got %v", want, got)
	}
}

// TestCovFlowReleaseFlowsPropagatesADecodeFailure pins that one corrupted row
// fails the whole listing instead of being dropped, so a partial answer is
// never presented as complete.
func TestCovFlowReleaseFlowsPropagatesADecodeFailure(t *testing.T) {
	rows := []graphstore.FlowRow{
		covFlowRow(1, "Login", "[8,9]", 0.45),
		covFlowRow(2, "main", covFlowMalformedPath, 0.4167),
	}
	if _, err := releaseFlows(rows, "criticality", fetchAllFlows); err == nil {
		t.Fatal("want the corrupted row to fail the listing")
	}
}

// TestCovFlowBoundStepsSpendsOneBudgetAcrossFlows pins that the step budget is
// SHARED rather than per-flow: the most critical flow spends what it needs and
// the tail keeps its metadata with an empty step list.
//
// Both flows must report their untruncated `total_steps` and be marked
// `steps_omitted`, so the depth a reviewer lost is still visible.
func TestCovFlowBoundStepsSpendsOneBudgetAcrossFlows(t *testing.T) {
	flows := []map[string]any{
		{"name": "deep", "steps": covFlowStepList(maxAffectedFlowSteps + 1)},
		{"name": "tail", "steps": covFlowStepList(3)},
	}
	bounded, truncated := boundFlowSteps(flows)
	if !truncated {
		t.Fatal("want the shared budget to report truncation")
	}
	if len(bounded) != 2 {
		t.Fatalf("want both flows kept, got %d", len(bounded))
	}
	covFlowAssertBounded(t, bounded[0], maxAffectedFlowSteps, maxAffectedFlowSteps+1)
	covFlowAssertBounded(t, bounded[1], 0, 3)
}

// ── get_affected_flows ───────────────────────────────────────────────────────

// TestCovFlowAffectedFlowsChangeSetWithNoGraphNodes pins the difference
// between "nothing changed" and "the change set touched no graph node".
//
// The second case QUERIES the store and therefore answers with the full
// payload — `changed_files`, `truncated` and a `_hints` block — where the
// no-changes early return carries none of them. Collapsing the two would hide
// from a client whether its change set was even looked at.
func TestCovFlowAffectedFlowsChangeSetWithNoGraphNodes(t *testing.T) {
	engine, _ := flowSeededEngine(t)
	payload := covFlowInvoke(t, engine, "get_affected_flows_tool",
		`{"changed_files": ["docs/guide.md"]}`)

	if payload["total"] != 0 {
		t.Fatalf("want total 0, got %v", payload["total"])
	}
	want := "0 flow(s) affected by changes in 1 file(s)"
	if payload["summary"] != want {
		t.Fatalf("want summary %q, got %q", want, payload["summary"])
	}
	for _, key := range []string{"changed_files", "truncated", "_hints"} {
		if _, present := payload[key]; !present {
			t.Errorf("an answered payload must carry %q (keys %v)", key, flowSortedKeys(payload))
		}
	}
}

// TestCovFlowAffectedFlowsFallsBackToTheWorkingTree pins the second half of
// auto-detection: an empty diff against the base falls through to the staged
// and unstaged paths, so uncommitted work is still reviewed.
func TestCovFlowAffectedFlowsFallsBackToTheWorkingTree(t *testing.T) {
	root := gitOracleRepo(t)
	gitOracleWrite(t, root, covFlowChangedFile, "package auth\n")
	engine := engineWithStore(t, root, &fakeStore{})
	engine.changedFiles = func(string, string) ([]string, error) { return nil, nil }

	payload := covFlowInvoke(t, engine, "get_affected_flows_tool", `{}`)
	files, _ := payload["changed_files"].([]string)
	if !slices.Equal(files, []string{covFlowChangedFile}) {
		t.Fatalf("want the untracked working-tree file, got %#v", payload["changed_files"])
	}
}

// TestCovFlowAffectedFlowsReportsDiscoveryFailures pins that neither half of
// auto-detection is allowed to escape as a raised error.
//
// A Subversion working copy is the discovery failure the native port
// deliberately refuses rather than answering an empty, confidently wrong
// change set.
func TestCovFlowAffectedFlowsReportsDiscoveryFailures(t *testing.T) {
	for _, tc := range []struct {
		name      string
		engine    func(*testing.T) *Engine
		wantError string
	}{
		{"the base diff fails", covFlowDetectFailureEngine, "injected failure"},
		{"the working tree is not git", covFlowSubversionEngine, "Subversion"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := covFlowInvoke(t, tc.engine(t), "get_affected_flows_tool", `{}`)
			covFlowAssertInBandError(t, payload, tc.wantError)
		})
	}
}

// TestCovFlowAffectedFlowsReportsStoreFailures pins the in-band failure shape
// of every read the affected-flow query performs, in the order it performs
// them.
func TestCovFlowAffectedFlowsReportsStoreFailures(t *testing.T) {
	arguments := fmt.Sprintf(`{"changed_files": [%q]}`, covFlowChangedFile)
	for _, tc := range []struct {
		name      string
		engine    func(*testing.T) *Engine
		wantError string
	}{
		{
			"the database cannot be opened",
			unreadableEngine,
			"codegraph: open",
		},
		{
			"the node table cannot be read",
			covFlowChangedNodeEngine(func(s *fakeStore) { s.allNodesErr = errFake }),
			"injected failure",
		},
		{
			"the membership table cannot be read",
			covFlowChangedNodeEngine(func(s *fakeStore) { s.membershipsErr = errFake }),
			"injected failure",
		},
		{
			"the flow table cannot be read",
			covFlowChangedNodeEngine(func(s *fakeStore) {
				s.memberships = []graphstore.FlowMembershipRow{{FlowID: 1, NodeID: 1}}
				s.flowsErr = errFake
			}),
			"injected failure",
		},
		{
			"an affected flow has a malformed stored path",
			covFlowChangedNodeEngine(func(s *fakeStore) {
				s.memberships = []graphstore.FlowMembershipRow{{FlowID: 1, NodeID: 1}}
				s.flows = []graphstore.FlowRow{covFlowRow(1, "Login", covFlowMalformedPath, 0.45)}
			}),
			"decode flow path",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := covFlowInvoke(t, tc.engine(t), "get_affected_flows_tool", arguments)
			covFlowAssertInBandError(t, payload, tc.wantError)
		})
	}
}

// TestCovFlowAffectedFlowsIgnoresAMembershipWithoutAFlowRow pins that a
// membership row whose flow was deleted is skipped rather than projected as an
// empty flow or dereferenced as a missing row.
func TestCovFlowAffectedFlowsIgnoresAMembershipWithoutAFlowRow(t *testing.T) {
	engine := covFlowChangedNodeEngine(func(s *fakeStore) {
		s.memberships = []graphstore.FlowMembershipRow{{FlowID: 99, NodeID: 1}}
	})(t)
	payload := covFlowInvoke(t, engine, "get_affected_flows_tool",
		fmt.Sprintf(`{"changed_files": [%q]}`, covFlowChangedFile))

	if payload["status"] != "ok" || payload["total"] != 0 {
		t.Fatalf("want an ok payload with no flows, got status %v total %v",
			payload["status"], payload["total"])
	}
}

// ── unbuilt graph ────────────────────────────────────────────────────────────

// TestCovFlowToolsDegradeOnUnbuiltGraph pins that a repository whose graph was
// never built answers with zero flows instead of failing, which is what the
// release's on-demand store creation produces.
func TestCovFlowToolsDegradeOnUnbuiltGraph(t *testing.T) {
	for _, tc := range []struct {
		name      string
		tool      string
		arguments string
		listKey   string
	}{
		{"list_flows with a kind filter", "list_flows_tool", `{"kind": "Function"}`, "flows"},
		{
			"get_affected_flows with an explicit change set",
			"get_affected_flows_tool",
			fmt.Sprintf(`{"changed_files": [%q]}`, covFlowChangedFile),
			"affected_flows",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := covFlowInvoke(t, covFlowUnbuiltEngine(t), tc.tool, tc.arguments)
			if payload["status"] != "ok" || payload["total"] != 0 {
				t.Fatalf("want an ok payload with no flows, got status %v total %v",
					payload["status"], payload["total"])
			}
			if names := covFlowNames(t, payload, tc.listKey); len(names) != 0 {
				t.Fatalf("want no flows, got %v", names)
			}
		})
	}
}

// TestCovFlowNodeIndexOnAnUnbuiltGraph pins that the node index degrades to an
// empty, NON-NIL map: a flow's step expansion indexes into it, and a nil map
// would be indistinguishable from a failed read to its caller.
func TestCovFlowNodeIndexOnAnUnbuiltGraph(t *testing.T) {
	index, err := nodesByID(nil)
	if err != nil {
		t.Fatalf("nodesByID(nil): %v", err)
	}
	if index == nil {
		t.Fatal("want an empty index, got nil")
	}
	if len(index) != 0 {
		t.Fatalf("want an empty index, got %d nodes", len(index))
	}
}

// ── step records ─────────────────────────────────────────────────────────────

// TestCovFlowLineNumberReadsOnlyStoredIntegers pins which dynamic types a step
// record's line numbers are read from.
//
// The values are `any` because they go straight to the JSON encoder, so the
// reader has to be explicit: a float — what a re-decoded payload would carry —
// is NOT a stored line number, and treating it as one would let a round-tripped
// step silently change which lines get inlined.
func TestCovFlowLineNumberReadsOnlyStoredIntegers(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
		want  int
	}{
		{"a stored int", 17, 17},
		{"a stored int64", int64(21), 21},
		{"a re-decoded float is not a stored line", float64(12), 0},
		{"an absent key", nil, 0},
		{"a string", "12", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := flowLineNumber(tc.value); got != tc.want {
				t.Fatalf("flowLineNumber(%#v) = %d, want %d", tc.value, got, tc.want)
			}
		})
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

// covFlowInvoke binds arguments through the pinned release schema and runs the
// tool's native handler, failing on a raised error so every caller can assume
// a payload.
func covFlowInvoke(t *testing.T, engine *Engine, tool, arguments string) map[string]any {
	t.Helper()
	args := flowBind(t, tool, json.RawMessage(arguments))
	result, err := toolHandlers[tool](engine, args)
	if err != nil {
		t.Fatalf("%s %s: %v", tool, arguments, err)
	}
	payload, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("%s: want a dict result, got %T", tool, result)
	}
	return payload
}

// covFlowAssertInBandError checks the failure shape the three flow tools share:
// a status/error pair with NO summary and NO hints, unlike the tools that go
// through the shared error-response helper.
func covFlowAssertInBandError(t *testing.T, payload map[string]any, want string) {
	t.Helper()
	if payload["status"] != "error" {
		t.Fatalf("want status error, got %v (keys %v)", payload["status"], flowSortedKeys(payload))
	}
	message, _ := payload["error"].(string)
	if !strings.Contains(message, want) {
		t.Fatalf("want an error containing %q, got %q", want, message)
	}
	for _, key := range []string{"summary", "_hints"} {
		if _, present := payload[key]; present {
			t.Errorf("a flow-tool failure must not carry %q (keys %v)", key, flowSortedKeys(payload))
		}
	}
}

// covFlowNames lists the flow names under a payload's flow-list key.
func covFlowNames(t *testing.T, payload map[string]any, key string) []string {
	t.Helper()
	flows, ok := payload[key].([]map[string]any)
	if !ok {
		t.Fatalf("payload[%q] is %T, want a flow list", key, payload[key])
	}
	names := make([]string, 0, len(flows))
	for _, flow := range flows {
		name, _ := flow["name"].(string)
		names = append(names, name)
	}
	return names
}

// covFlowAssertPath checks a decoded stored path, including that an empty
// result is a non-nil slice so it encodes as `[]` rather than `null`.
func covFlowAssertPath(t *testing.T, got []int64, err error, want []int64, wantError string) {
	t.Helper()
	if wantError != "" {
		if err == nil || !strings.Contains(err.Error(), wantError) {
			t.Fatalf("want an error containing %q, got %v (%v)", wantError, err, got)
		}
		return
	}
	if err != nil {
		t.Fatalf("decodeFlowPath: %v", err)
	}
	if got == nil {
		t.Fatal("want a non-nil path so the payload encodes []")
	}
	if !slices.Equal(got, want) {
		t.Fatalf("want path %v, got %v", want, got)
	}
}

// covFlowAssertStepSource checks one step's inlined source. An empty want
// means the step must carry no `source` key at all.
func covFlowAssertStepSource(t *testing.T, step, flow map[string]any, want string) {
	t.Helper()
	source, present := step["source"]
	switch {
	case want == "" && present:
		t.Fatalf("want no source key, got %q", source)
	case want != "" && source != want:
		t.Fatalf("want source %q, got %q", want, source)
	}
	if _, truncated := flow["source_truncated"]; truncated {
		t.Fatal("a step within budget must not mark the flow source-truncated")
	}
}

// covFlowAssertBounded checks one bounded flow's visible step count against
// the untruncated total it has to keep reporting.
func covFlowAssertBounded(t *testing.T, flow map[string]any, wantVisible, wantTotal int) {
	t.Helper()
	steps, ok := flow["steps"].([]map[string]any)
	if !ok {
		t.Fatalf("flow %v: steps are %T", flow["name"], flow["steps"])
	}
	if len(steps) != wantVisible {
		t.Errorf("flow %v: want %d visible steps, got %d", flow["name"], wantVisible, len(steps))
	}
	if flow["total_steps"] != wantTotal {
		t.Errorf("flow %v: want total_steps %d, got %v", flow["name"], wantTotal, flow["total_steps"])
	}
	if flow["steps_omitted"] != true {
		t.Errorf("flow %v: want steps_omitted set, got %v", flow["name"], flow["steps_omitted"])
	}
}

// covFlowRow builds one stored flow row. Criticality drives the default sort,
// and pathJSON is what every decode arm is keyed on.
func covFlowRow(id int64, name, pathJSON string, criticality float64) graphstore.FlowRow {
	return graphstore.FlowRow{
		ID:           id,
		Name:         name,
		EntryPointID: id,
		Depth:        1,
		NodeCount:    2,
		FileCount:    1,
		Criticality:  criticality,
		PathJSON:     pathJSON,
	}
}

// covFlowStep is one step record carrying only the fields source inlining
// reads.
func covFlowStep(file string, lineStart, lineEnd any) map[string]any {
	return map[string]any{"file": file, "line_start": lineStart, "line_end": lineEnd}
}

// covFlowStepList is a step list of the given length. The step budget counts
// records, so each one carries just its position.
func covFlowStepList(n int) []map[string]any {
	steps := make([]map[string]any, 0, n)
	for i := range n {
		steps = append(steps, map[string]any{"node_id": int64(i + 1)})
	}
	return steps
}

// covFlowSourceRoot writes the five-line file the source-inlining arms read.
func covFlowSourceRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeExtra(t, root, "src/sample.go", "package src\n\nfunc A() {}\nfunc B() {}\nfunc C() {}\n")
	return root
}

// covFlowUnbuiltEngine returns an engine over a repository whose graph was
// never built, so every read degrades through a nil store.
func covFlowUnbuiltEngine(t *testing.T) *Engine {
	t.Helper()
	e := Open(t.TempDir())
	e.changedFiles = func(string, string) ([]string, error) { return nil, nil }
	t.Cleanup(func() { _ = e.Close() })
	return e
}

// covFlowFakeEngine adapts a prepared fake store into the engine factory the
// failure tables use.
func covFlowFakeEngine(store graphstore.Store) func(*testing.T) *Engine {
	return func(t *testing.T) *Engine {
		t.Helper()
		return engineWithStore(t, writeFixture(t), store)
	}
}

// covFlowChangedNodeEngine returns an engine whose store reports exactly one
// node inside covFlowChangedFile, so the affected-flow query gets past its
// changed-node lookup and reaches the read prepare injects a failure into.
//
// The node's path is built from the run's root because graph identity is the
// absolute POSIX path: a node recorded under any other root would never match
// the relative change set.
func covFlowChangedNodeEngine(prepare func(*fakeStore)) func(*testing.T) *Engine {
	return func(t *testing.T) *Engine {
		t.Helper()
		root := writeFixture(t)
		store := &fakeStore{allNodes: []graphstore.GraphNode{{
			ID:       1,
			Kind:     "Function",
			Name:     "Login",
			FilePath: filepath.ToSlash(filepath.Join(root, covFlowChangedFile)),
		}}}
		prepare(store)
		return engineWithStore(t, root, store)
	}
}

// covFlowDetectFailureEngine returns an engine whose base-diff discovery
// fails.
func covFlowDetectFailureEngine(t *testing.T) *Engine {
	t.Helper()
	e := engineWithStore(t, writeFixture(t), &fakeStore{})
	e.changedFiles = func(string, string) ([]string, error) { return nil, errFake }
	return e
}

// covFlowSubversionEngine returns an engine rooted at a Subversion working
// copy, whose working-tree discovery the native port refuses.
func covFlowSubversionEngine(t *testing.T) *Engine {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".svn"), 0o755); err != nil {
		t.Fatalf("mkdir .svn: %v", err)
	}
	e := engineWithStore(t, root, &fakeStore{})
	e.changedFiles = func(string, string) ([]string, error) { return nil, nil }
	return e
}
