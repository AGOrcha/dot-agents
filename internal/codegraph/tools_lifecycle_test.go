package codegraph

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/crgrelease"
)

// Parity of the two MUTATING tools' response payloads against
// testdata/crg-release/v2.3.8/lifecycle.json.
//
// lifecycle_test.go already replays the generator's sequence at the REPORT
// level, constructing the option structs by hand. This suite replays the same
// sequence through the MCP tool surface instead, so it pins the two things
// only the adapter can get wrong:
//
//   - the ARGUMENT BINDING — `full_rebuild` choosing between BuildReport and
//     UpdateReport, `postprocess` reaching the option struct as the release's
//     literal level, the three post-process step flags arriving as PRESENT
//     booleans, and `base` keeping all three of its states apart;
//   - the PROJECTION — the report becoming the release's result dict, whose
//     field set differs per case and is asserted key-for-key.
//
// It deliberately reuses lifecycle_test.go's fixture repository, token
// resolution and value comparison: a second copy of the two-commit replay
// would be a second definition of what "the fixture repository" means.

// lifecycleToolStep is one call of dump_lifecycle()'s sequence, expressed as a
// tools/call rather than as a Go option struct.
type lifecycleToolStep struct {
	// fixture is the lifecycle.json key holding the expected result dict.
	fixture string
	tool    string
	// args are the tool arguments as an MCP client would send them, before
	// schema defaulting. A "${...}" value is resolved against the
	// materialized repository, so no SHA is hardcoded.
	args map[string]any
	// edited runs the call with the work-tree edit the one genuinely
	// incremental case needs.
	edited bool
}

// lifecycleToolSteps replays dump_lifecycle() in order.
//
// The order is load-bearing: an incremental update is only "no changes"
// because the build before it wrote the commit its base resolves to, and the
// standalone post-processes only report the counts they do because the full
// build before them populated the graph. The generator's SQLite-level
// truncate_summary_tables() step is absent because no build, update or
// postprocess RESULT DICT carries a summary-table counter — that split is
// asserted at the report level in lifecycle_test.go.
var lifecycleToolSteps = []lifecycleToolStep{
	{fixture: "clean_full_build", tool: "build_or_update_graph_tool", args: map[string]any{"full_rebuild": true}},
	{fixture: "postprocess_none_build", tool: "build_or_update_graph_tool", args: map[string]any{"full_rebuild": true, "postprocess": "none"}},
	{fixture: "postprocess_minimal_build", tool: "build_or_update_graph_tool", args: map[string]any{"full_rebuild": true, "postprocess": "minimal"}},
	{fixture: "full_build_again", tool: "build_or_update_graph_tool", args: map[string]any{"full_rebuild": true}},

	{fixture: "standalone_postprocess", tool: "run_postprocess_tool", args: map[string]any{}},
	{fixture: "standalone_postprocess_flows_only", tool: "run_postprocess_tool", args: map[string]any{"communities": false, "fts": false}},
	{fixture: "standalone_postprocess_no_flows", tool: "run_postprocess_tool", args: map[string]any{"flows": false, "communities": false, "fts": false}},

	{fixture: "full_build_restoring_summaries", tool: "build_or_update_graph_tool", args: map[string]any{"full_rebuild": true}},

	// An absent base is the release's automatic mode: the diff base
	// resolves to the commit the graph was last built at, so a clean tree
	// re-parses nothing.
	{fixture: "incremental_auto_base_no_changes", tool: "build_or_update_graph_tool", args: map[string]any{}},
	// An EXPLICIT base skips resolution: token.go is in the diff against
	// the first commit, yet its rows already match, so the update still
	// early-returns with files_updated 0.
	{fixture: "incremental_explicit_base", tool: "build_or_update_graph_tool", args: map[string]any{"base": "${BASE_SHA}"}},
	// An explicit base that resolves to nothing stays incremental; only an
	// absent base falls back to a full rebuild.
	{fixture: "incremental_unresolvable_base", tool: "build_or_update_graph_tool", args: map[string]any{"base": "0000000000000000000000000000000000000000"}},
	// An explicit EMPTY base is the third state, and the one this adapter
	// is most likely to lose: it is a ref that matches nothing, so
	// `base_resolved` echoes "" rather than the last-built commit an
	// ABSENT base would have resolved to. Binding it through OptString is
	// what keeps the two apart.
	{fixture: "incremental_empty_base", tool: "build_or_update_graph_tool", args: map[string]any{"base": ""}},

	{fixture: "incremental_auto_base_with_changes", tool: "build_or_update_graph_tool", args: map[string]any{}, edited: true},

	{fixture: "final_full_build", tool: "build_or_update_graph_tool", args: map[string]any{"full_rebuild": true}},
}

// TestLifecycleToolsProjectReleaseResults replays the oracle's lifecycle
// sequence through the two native handlers and compares each projected result
// dict, field set included, against the result the release recorded.
func TestLifecycleToolsProjectReleaseResults(t *testing.T) {
	fix := newLifecycleFixture(t)

	for _, step := range lifecycleToolSteps {
		want := fix.expect(step.fixture)
		passed := t.Run(step.fixture, func(t *testing.T) {
			call := func() {
				args := bindLifecycleToolArgs(t, fix, step.tool, step.args)
				got := callLifecycleTool(t, fix.engine, step.tool, args)
				assertValue(t, step.fixture, got, want)
			}
			if step.edited {
				fix.withEditedAuthFile(call)
				return
			}
			call()
		})
		if !passed {
			// Every later case reads the graph this one left behind,
			// so continuing would report cascading failures that say
			// nothing about their own contract.
			t.Fatalf("lifecycle sequence aborted at %q", step.fixture)
		}
	}
}

// TestLifecycleToolFixtureCoverage fails when lifecycle.json carries a result
// dict this table neither drives nor classifies, so a regenerated oracle
// cannot quietly introduce a payload shape nothing asserts.
func TestLifecycleToolFixtureCoverage(t *testing.T) {
	cases := readFixtureJSON[map[string]map[string]any](t, "lifecycle.json")

	driven := make([]string, 0, len(lifecycleToolSteps))
	for _, step := range lifecycleToolSteps {
		driven = append(driven, step.fixture)
	}
	for name := range cases {
		if slices.Contains(driven, name) ||
			slices.Contains(lifecycleFullBuildAliases, name) ||
			slices.Contains(lifecycleSummaryTableCases, name) {
			continue
		}
		t.Errorf("lifecycle.json case %q is neither driven by lifecycleToolSteps nor classified", name)
	}
}

// lifecycleFullBuildAliases are full-build result dicts the step table does
// not call separately because they are the same payload — a full rebuild of
// the fixture repository is idempotent, which
// TestLifecycleFullBuildResultsAreIdempotent pins.
var lifecycleFullBuildAliases = []string{"full_build"}

// lifecycleSummaryTableCases are the lifecycle.json keys that are not a tool
// result dict at all: summary_counts() snapshots of the derived tables, whose
// refresh split is asserted at the report level in lifecycle_test.go.
var lifecycleSummaryTableCases = []string{
	"summary_tables_after_full_build",
	"summary_tables_after_selective_postprocess",
	"summary_tables_after_standalone_postprocess",
	"summary_tables_before_standalone_postprocess",
	"summary_tables_restored",
}

// TestLifecycleFullBuildResultsAreIdempotent justifies the alias list above:
// every full-build result dict the oracle recorded is the same payload, so
// driving four of them covers all five.
func TestLifecycleFullBuildResultsAreIdempotent(t *testing.T) {
	cases := readFixtureJSON[map[string]map[string]any](t, "lifecycle.json")
	reference := "clean_full_build"
	want, err := json.Marshal(cases[reference])
	if err != nil {
		t.Fatalf("encode %s: %v", reference, err)
	}
	for _, name := range append([]string{
		"full_build_again", "full_build_restoring_summaries", "final_full_build",
	}, lifecycleFullBuildAliases...) {
		got, err := json.Marshal(cases[name])
		if err != nil {
			t.Fatalf("encode %s: %v", name, err)
		}
		// Durations are normalized to the same token on both sides, so
		// the two payloads compare byte for byte.
		if string(got) != string(want) {
			t.Errorf("%s is not identical to %s; the alias list assumes a full rebuild is idempotent", name, reference)
		}
	}
}

// TestLifecycleToolAttachesProvenanceEnvelope pins the division of labour
// between a handler and Engine.CallTool: the handler returns the release's
// result dict and nothing else, and the `_graph` provenance envelope the
// release's MCP wrapper adds (main.py -> tools/_common.with_provenance) is
// attached AROUND it. A handler that assembled its own envelope would produce
// two, and one that assembled it instead of returning the dict would lose the
// payload.
func TestLifecycleToolAttachesProvenanceEnvelope(t *testing.T) {
	fix := newLifecycleFixture(t)
	want := fix.expect("clean_full_build")

	args := bindLifecycleToolArgs(t, fix, "build_or_update_graph_tool", map[string]any{"full_rebuild": true})
	result, err := fix.engine.CallTool("build_or_update_graph_tool", args)
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	payload, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("want a dict result, got %T", result)
	}
	if _, ok := payload["_graph"]; !ok {
		t.Error("want the release's _graph provenance envelope on a native lifecycle result")
	}
	delete(payload, "_graph")
	assertValue(t, "clean_full_build", payload, want)
}

// TestLifecycleRecurseSubmodulesStaysTriState guards the one bound argument
// whose null is not false.
//
// The fixture repository has no submodules, so no recorded payload can pin
// this: an unset recurse_submodules must reach the option struct as nil so
// CRG_RECURSE_SUBMODULES stays in charge, and an explicit false must reach it
// as a PRESENT false that overrides that environment. Collapsing the two would
// silently disable a submodule scan the environment had enabled.
func TestLifecycleRecurseSubmodulesStaysTriState(t *testing.T) {
	for _, tc := range []struct {
		name string
		args map[string]any
		want *bool
	}{
		{name: "unset", args: map[string]any{}, want: nil},
		{name: "explicit_false", args: map[string]any{"recurse_submodules": false}, want: new(false)},
		{name: "explicit_true", args: map[string]any{"recurse_submodules": true}, want: new(true)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := bindLifecycleToolArgs(t, nil, "build_or_update_graph_tool", tc.args)
			got := lifecycleTriStateBool(args, "recurse_submodules")
			switch {
			case tc.want == nil && got != nil:
				t.Fatalf("want nil (defer to CRG_RECURSE_SUBMODULES), got %v", *got)
			case tc.want != nil && got == nil:
				t.Fatalf("want a present %v, got nil", *tc.want)
			case tc.want != nil && *got != *tc.want:
				t.Fatalf("want %v, got %v", *tc.want, *got)
			}
		})
	}
}

// bindLifecycleToolArgs validates an argument map through the pinned release's
// schema, so a handler is exercised with exactly the values a real tools/call
// delivers — including every default the schema fills in, which is where the
// post-process step flags and the "full" level come from.
//
// A nil fixture means "no repository": the arguments are bound for their own
// sake rather than to drive a call. Otherwise `repo_root` is supplied and
// every "${...}" value is resolved, because dump_lifecycle() passes repo_root
// explicitly on every call and the fixtures are that shape.
func bindLifecycleToolArgs(t *testing.T, fix *lifecycleFixture, tool string, args map[string]any) crgrelease.Args {
	t.Helper()
	supplied := make(map[string]any, len(args)+1)
	for key, value := range args {
		if fix != nil {
			value = fix.resolveToken(value)
		}
		supplied[key] = value
	}
	if fix != nil {
		supplied["repo_root"] = fix.root
	}
	raw, err := json.Marshal(supplied)
	if err != nil {
		t.Fatalf("encode %s arguments: %v", tool, err)
	}
	definition, ok := crgrelease.Lookup(tool)
	if !ok {
		t.Fatalf("%s is not published by code-review-graph %s", tool, crgrelease.Version)
	}
	bound, bindErr := definition.Bind(raw)
	if bindErr != nil {
		t.Fatalf("bind %s arguments %s: %v", tool, raw, bindErr)
	}
	return bound
}

// callLifecycleTool invokes the handler directly — not through
// Engine.CallTool — so the comparison sees the handler's own payload with
// nothing added to it. The envelope is asserted separately.
func callLifecycleTool(t *testing.T, engine *Engine, tool string, args crgrelease.Args) map[string]any {
	t.Helper()
	handler, ok := toolHandlers[tool]
	if !ok {
		t.Fatalf("%s has no native handler", tool)
	}
	result, err := handler(engine, args)
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	payload, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("%s: want a dict result, got %T", tool, result)
	}
	return payload
}
