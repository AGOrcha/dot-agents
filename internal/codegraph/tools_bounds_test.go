package codegraph

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/crgrelease"
)

// Parity of the release's result-bound rejection.
//
// Upstream guards a handful of caller-supplied bounds with
// `_validate_positive_int`, and every one of its 22 call sites runs OUTSIDE
// the tool's `try` block. So a bound violation is not an in-band
// `{"status": "error"}` payload like the rest of the release's failures — the
// ValueError escapes to FastMCP and the client sees a TRANSPORT error:
// `is_error` set, no structured content at all, and the message carrying
// FastMCP's uniform `Error calling tool '<name>': ` prefix.
//
// That distinction is the whole point of this file. A native handler that
// reached for the package's ErrorResponse helper here — the obvious thing to
// do, since every other release failure IS an error payload — would return
// `(payload, nil)`, and the server would answer `is_error: false` with a
// populated `structured_content`. Both halves of the envelope would be wrong
// while every field inside the payload looked plausible, and no
// whole-payload fixture comparison would catch it, because the release's
// fixture for that call has no payload to compare against.
//
// The arguments bind successfully before any of this: the published schemas
// carry no `minimum`, so 0 and negatives are valid input as far as
// crgrelease is concerned and the rejection belongs entirely to the handler.
const boundsFixtureDir = "../../testdata/crg-release/v2.3.8"

// boundsFixturePrefix is the case-name prefix the generator uses for these
// fixtures. The case name carries the argument under test, because a fixture
// is written to `calls/<tool>__<case>.json` and two cases sharing a
// (tool, case) pair would overwrite each other.
const boundsFixturePrefix = "bound_"

// boundsGuardedArgs is every NATIVE tool argument the release guards with
// `_validate_positive_int`, verified by calling the installed v2.3.8 wheel
// with 0 and -1 for each integer argument of each native tool.
//
// The negative half of that probe is why this table is written out rather
// than derived from the schemas: most integer arguments are NOT guarded, and
// a rule like "integer argument implies guarded" would assert an error for
// calls the release answers normally. Confirmed unguarded, each returning an
// ordinary payload at 0 and -1: `max_depth` on get_impact_radius and
// get_review_context, `limit` on semantic_search_nodes, `limit` and
// `min_lines` on find_large_functions, `max_flows` on get_affected_flows,
// `min_size` on list_communities, and `depth` and `token_budget` on
// traverse_graph.
var boundsGuardedArgs = map[string][]string{
	"query_graph_tool":                {"max_results"},
	"get_review_context_tool":         {"max_files", "max_lines_per_file", "max_results"},
	"list_flows_tool":                 {"limit"},
	"get_flow_tool":                   {"max_source_lines", "max_steps"},
	"list_communities_tool":           {"max_members", "max_results"},
	"get_community_tool":              {"max_members"},
	"get_architecture_overview_tool":  {"max_members", "max_results"},
	"get_hub_nodes_tool":              {"top_n"},
	"get_knowledge_gaps_tool":         {"max_per_category"},
	"get_surprising_connections_tool": {"top_n"},
}

// boundsFixture is the part of a recorded tools/call result this file asserts
// on. `StructuredContent` is a pointer because the generator omits the key
// entirely when the release returned none, and "absent" is exactly the
// property under test — a `{}` or `null` payload would be a different
// contract.
type boundsFixture struct {
	Tool      string          `json:"tool"`
	Case      string          `json:"case"`
	Arguments json.RawMessage `json:"arguments"`
	IsError   bool            `json:"is_error"`
	Content   []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StructuredContent *json.RawMessage `json:"structured_content"`
}

// TestBoundViolationsAreTransportErrors drives every recorded bound violation
// through the native handler and asserts the handler fails the way the
// release does.
func TestBoundViolationsAreTransportErrors(t *testing.T) {
	for _, name := range boundsExpectedFixtures(t) {
		t.Run(name, func(t *testing.T) {
			assertBoundViolationIsTransportError(t, name)
		})
	}
}

// assertBoundViolationIsTransportError drives one recorded bound violation
// through the native handler: the fixture must really record a transport
// failure, the arguments must still bind, and the handler must answer with a
// bare Go error carrying the release's message and no payload.
func assertBoundViolationIsTransportError(t *testing.T, name string) {
	t.Helper()

	fixture := readBoundsFixture(t, name)

	// The fixture is only a valid oracle for this test if the release
	// really did fail the call as a transport error. If a case is ever
	// added for an argument the release does not guard, this fails here
	// rather than blaming the handler for answering.
	if !fixture.IsError {
		t.Fatalf("fixture %s records is_error=false: %s is not a "+
			"bound-guarded argument, so this case does not belong in "+
			"the bound_ family", name, fixture.Case)
	}
	if fixture.StructuredContent != nil {
		t.Fatalf("fixture %s carries structured content %s; the "+
			"release returns none for an escaped exception",
			name, string(*fixture.StructuredContent))
	}
	want := boundsHandlerMessage(t, fixture)

	tool, ok := crgrelease.Lookup(fixture.Tool)
	if !ok {
		t.Fatalf("%s is not published by code-review-graph %s",
			fixture.Tool, crgrelease.Version)
	}
	args, bindErr := tool.Bind(fixture.Arguments)
	if bindErr != nil {
		// Binding must NOT reject the value: the bound is the handler's
		// to enforce, and a schema that rejected 0 would move the
		// failure to a different envelope.
		t.Fatalf("binding %s rejected the release's own arguments %s: %v",
			fixture.Tool, string(fixture.Arguments), bindErr)
	}

	handler, native := toolHandlers[fixture.Tool]
	if !native {
		t.Skipf("%s has no native handler yet; the call routes to the "+
			"retained bridge, which reproduces this envelope by "+
			"construction", fixture.Tool)
	}

	// A bare engine on an empty directory: the bound check runs before
	// the graph is read, so the rejection must not depend on a built
	// graph.
	result, err := handler(Open(t.TempDir()), args)

	if err == nil {
		t.Fatalf("handler returned (%#v, nil); the release raises here, "+
			"so the handler must return a Go error. %s",
			result, boundsErrorPayloadHint(result))
	}
	if result != nil {
		t.Errorf("handler returned a payload %#v alongside its error; "+
			"the release's result has no content but the error text",
			result)
	}
	if got := err.Error(); got != want {
		t.Errorf("error message\n got: %q\nwant: %q\n(want is the "+
			"fixture's content text minus FastMCP's uniform %q prefix, "+
			"which MCPServer.route adds)",
			got, want, boundsPrefix(fixture.Tool))
	}
}

// TestBoundViolationFixtureCoverage keeps the fixture family and the
// guarded-argument table in lockstep in BOTH directions.
//
// Without it the suite degrades silently in two ways: deleting a case would
// shrink the table test to the cases that remain, and adding a case for an
// argument nothing guards would pin an expectation the release does not have.
func TestBoundViolationFixtureCoverage(t *testing.T) {
	want := boundsExpectedFixtures(t)

	matches, err := filepath.Glob(filepath.Join(
		boundsFixtureDir, "calls", "*__"+boundsFixturePrefix+"*.json"))
	if err != nil {
		t.Fatalf("glob bound fixtures: %v", err)
	}
	got := make([]string, 0, len(matches))
	for _, match := range matches {
		got = append(got, strings.TrimSuffix(filepath.Base(match), ".json"))
	}
	sort.Strings(got)

	if len(got) == 0 {
		t.Fatal("no bound-violation fixtures found; regenerate with " +
			"tools/crgrelease/generate_release_contract.py")
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("bound-violation fixtures disagree with the guarded-argument "+
			"table\n on disk: %v\n expected: %v\nAdd the case to CALL_CASES, or "+
			"add the argument to boundsGuardedArgs once the release is "+
			"confirmed to guard it", got, want)
	}
}

// TestBoundViolationArgumentValuesCoverBothSides checks the recorded cases
// exercise 0 AND a negative.
//
// `value < 1` and `value <= 0` are the same predicate for 0 and differ for
// nothing, but a handler that wrote `value == 0` would pass a suite that only
// ever passed 0. Pinning both sides costs two fixtures.
func TestBoundViolationArgumentValuesCoverBothSides(t *testing.T) {
	var zeros, negatives []string
	for _, name := range boundsExpectedFixtures(t) {
		fixture := readBoundsFixture(t, name)
		arg := strings.TrimPrefix(fixture.Case, boundsFixturePrefix)
		var decoded map[string]any
		if err := json.Unmarshal(fixture.Arguments, &decoded); err != nil {
			t.Fatalf("%s: decode arguments: %v", name, err)
		}
		value, ok := decoded[arg].(float64)
		if !ok {
			t.Fatalf("%s: case names argument %q but the arguments %s carry no "+
				"such number", name, arg, string(fixture.Arguments))
		}
		switch {
		case value == 0:
			zeros = append(zeros, name)
		case value < 0:
			negatives = append(negatives, name)
		default:
			t.Errorf("%s: %s=%v is not a bound violation", name, arg, value)
		}
	}
	if len(zeros) == 0 {
		t.Error("no bound-violation fixture passes 0")
	}
	if len(negatives) < 2 {
		t.Errorf("only %d bound-violation fixture(s) pass a negative (%v); "+
			"at least two are needed for the negative side to be pinned "+
			"across more than one upstream module", len(negatives), negatives)
	}
}

// boundsExpectedFixtures is the sorted fixture base name for every
// (native tool, guarded argument) pair.
func boundsExpectedFixtures(t *testing.T) []string {
	t.Helper()
	var names []string
	for tool, args := range boundsGuardedArgs {
		capability, ok := crgrelease.Capability(tool)
		if !ok {
			t.Fatalf("%s has no routing decision in the pinned contract", tool)
		}
		if !capability.NativeBackend {
			t.Fatalf("%s is bridge-only (%s); this table covers the native "+
				"surface, so either the tool moved or the table is stale",
				tool, capability.BridgeOnlyReason)
		}
		for _, arg := range args {
			names = append(names, tool+"__"+boundsFixturePrefix+arg)
		}
	}
	sort.Strings(names)
	return names
}

// readBoundsFixture loads one recorded tools/call result.
func readBoundsFixture(t *testing.T, name string) boundsFixture {
	t.Helper()
	path := filepath.Join(boundsFixtureDir, "calls", name+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v (regenerate with "+
			"tools/crgrelease/generate_release_contract.py)", path, err)
	}
	var fixture boundsFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return fixture
}

// boundsPrefix is the prefix FastMCP prepends to every exception that escapes
// a tool. It is uniform across all 30 tools, so it belongs to the transport
// layer (MCPServer.route) and NOT to a handler's error text.
func boundsPrefix(tool string) string {
	return fmt.Sprintf("Error calling tool '%s': ", tool)
}

// boundsHandlerMessage strips the transport prefix, leaving exactly what the
// handler's Go error must say.
func boundsHandlerMessage(t *testing.T, fixture boundsFixture) string {
	t.Helper()
	if len(fixture.Content) != 1 {
		t.Fatalf("%s: expected one content block, got %d",
			fixture.Tool, len(fixture.Content))
	}
	text := fixture.Content[0].Text
	prefix := boundsPrefix(fixture.Tool)
	if !strings.HasPrefix(text, prefix) {
		t.Fatalf("%s: content text %q does not carry the %q prefix",
			fixture.Tool, text, prefix)
	}
	return strings.TrimPrefix(text, prefix)
}

// boundsErrorPayloadHint names the specific mistake when a handler returned
// the release's in-band error payload instead of failing.
func boundsErrorPayloadHint(result any) string {
	payload, ok := result.(map[string]any)
	if !ok {
		return "Return the error from ValidatePositiveInt."
	}
	if payload["status"] == "error" {
		return "That payload is an ErrorResponse. ErrorResponse is for " +
			"upstream's _error_response, which is an in-band failure the " +
			"release returns from INSIDE its try block. A bound violation is " +
			"raised BEFORE it, so return the error from ValidatePositiveInt."
	}
	return "Return the error from ValidatePositiveInt."
}
