package codegraph

// Degradation and failure arms of the derived-view accessors in query.go and
// of the three community tools in tools_communities.go.
//
// The parity fixtures next door pin what these paths answer on a HEALTHY
// graph. What they cannot reach is the other half of each contract: a graph
// that was never built must degrade to an empty answer, and a persisted table
// that cannot be read must surface the failure instead of reporting a
// confident partial result — a change report missing its flows, or a community
// missing its members, is worse than an error.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/crgrelease"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// covViewChangedFile is the fixture file every change-detection case names.
const covViewChangedFile = "lib/lib.go"

// covViewMemberFailStore fails the member read after the first `ok` calls.
//
// get_community_tool reads members twice — once while listing the candidate
// communities and once for `include_members` — and only a store that answers
// the first and fails the second separates the two. A single always-failing
// read can never reach the second, because the listing would fail first and
// the tool would report not_found.
type covViewMemberFailStore struct {
	*fakeStore
	ok    int
	calls int
}

func (s *covViewMemberFailStore) ReadNodesByCommunity(id int64) ([]graphstore.GraphNode, error) {
	s.calls++
	if s.calls > s.ok {
		return nil, errFake
	}
	return s.fakeStore.ReadNodesByCommunity(id)
}

// covViewEngine wires a fake store into an engine over a fresh fixture and
// keys the store's file index on the absolute path the engine maps
// covViewChangedFile to, which is the spelling every graph row carries.
func covViewEngine(t *testing.T, store *fakeStore, changed ...graphstore.GraphNode) *Engine {
	t.Helper()
	e := engineWithStore(t, writeFixture(t), store)
	if changed != nil {
		store.nodes = map[string][]graphstore.GraphNode{e.absPath(covViewChangedFile): changed}
	}
	return e
}

// covViewDetect runs change detection over covViewChangedFile.
func covViewDetect(e *Engine) (*graphstore.CRGChangeReport, error) {
	return e.DetectChanges(graphstore.DetectChangesOptions{Files: []string{covViewChangedFile}})
}

// covViewWantFake asserts a call surfaced the injected store failure.
func covViewWantFake(t *testing.T, what string, err error) {
	t.Helper()
	if !errors.Is(err, errFake) {
		t.Fatalf("%s: err = %v, want the injected store failure", what, err)
	}
}

// covViewInvoke binds arguments through the pinned release's own schema and
// runs the tool, returning the handler's error rather than failing on it.
func covViewInvoke(t *testing.T, e *Engine, name string, arguments map[string]any) (any, error) {
	t.Helper()

	published, ok := crgrelease.Lookup(name)
	if !ok {
		t.Fatalf("%s is not published by code-review-graph %s", name, crgrelease.Version)
	}
	raw, err := json.Marshal(arguments)
	if err != nil {
		t.Fatalf("marshal %s arguments: %v", name, err)
	}
	args, bindErr := published.Bind(raw)
	if bindErr != nil {
		t.Fatalf("bind %s%v: %v", name, arguments, bindErr)
	}
	return e.CallTool(name, args)
}

// covViewWantToolFake asserts a tool call failed with the injected store
// failure and returned no payload at all — a half-built dict would be
// indistinguishable from a real answer once the error is logged and dropped.
func covViewWantToolFake(t *testing.T, e *Engine, name string, arguments map[string]any) {
	t.Helper()

	got, err := covViewInvoke(t, e, name, arguments)
	covViewWantFake(t, name, err)
	if got != nil {
		t.Fatalf("%s: payload = %#v, want none alongside the failure", name, got)
	}
}

// covViewPayload runs a tool that is expected to succeed and returns its dict.
func covViewPayload(t *testing.T, e *Engine, name string, arguments map[string]any) map[string]any {
	t.Helper()

	got, err := covViewInvoke(t, e, name, arguments)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	payload, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("%s returned %T, want an object", name, got)
	}
	return payload
}

// covViewList reads a payload key that must be a list.
func covViewList(t *testing.T, payload map[string]any, key string) []any {
	t.Helper()

	rows, ok := payload[key].([]any)
	if !ok {
		t.Fatalf("%s = %#v, want a list", key, payload[key])
	}
	return rows
}

// ── query.go: flows ──────────────────────────────────────────────────────────

// TestCovViewListFlowsPropagatesEntryPointReadFailure covers the batched
// entry-point name resolution. A flow whose entry point cannot be read must
// not be reported with the fallback row name, because `entry_point` is what a
// caller passes back to get_flow.
func TestCovViewListFlowsPropagatesEntryPointReadFailure(t *testing.T) {
	e := engineWithStore(t, writeFixture(t), &fakeStore{
		flows:       []graphstore.FlowRow{{ID: 1, Name: "Run", EntryPointID: 5, Criticality: 0.5}},
		allNodesErr: errFake,
	})
	_, err := e.ListFlows(0, "")
	covViewWantFake(t, "ListFlows", err)
}

// ── query.go: communities ────────────────────────────────────────────────────

// TestCovViewListCommunitiesUnbuiltGraphIsEmpty covers the never-built graph:
// a repository with no database answers "no communities", not an error.
func TestCovViewListCommunitiesUnbuiltGraphIsEmpty(t *testing.T) {
	e, _ := newEngine(t, nil)
	result, err := e.ListCommunities(0, "")
	if err != nil {
		t.Fatalf("ListCommunities: %v", err)
	}
	if len(result.Communities) != 0 {
		t.Fatalf("communities = %+v, want none", result.Communities)
	}
	if result.Status != statusOK || result.Summary != "0 community/communities" {
		t.Fatalf("status/summary = %q/%q", result.Status, result.Summary)
	}
}

// TestCovViewListCommunitiesPropagatesStoreOpenFailure covers the lazy open
// every community read starts from: a database that exists but cannot be
// opened is an error, not an empty partition.
func TestCovViewListCommunitiesPropagatesStoreOpenFailure(t *testing.T) {
	if _, err := unreadableEngine(t).ListCommunities(0, ""); err == nil {
		t.Fatal("ListCommunities: want the store-open failure")
	}
}

// TestCovViewListCommunitiesFiltersSizeAndFallsBackToSizeSort covers the
// min-size filter and the unknown-sort-key fallback together, because both are
// visible in one ordered answer: the sub-threshold community is absent and the
// rest come back largest first.
func TestCovViewListCommunitiesFiltersSizeAndFallsBackToSizeSort(t *testing.T) {
	e := engineWithStore(t, writeFixture(t), &fakeStore{communities: []graphstore.CommunityRow{
		{ID: 1, Name: "small", Size: 2},
		{ID: 2, Name: "medium", Size: 5},
		{ID: 3, Name: "big", Size: 9},
	}})
	result, err := e.ListCommunities(3, "no-such-column")
	if err != nil {
		t.Fatalf("ListCommunities: %v", err)
	}
	var got []string
	for _, community := range result.Communities {
		got = append(got, community.Name)
	}
	if len(got) != 2 || got[0] != "big" || got[1] != "medium" {
		t.Fatalf("communities = %v, want [big medium]: size 2 filtered out, unknown key sorted by size", got)
	}
	if result.Summary != "2 community/communities" {
		t.Fatalf("summary = %q, want the filtered count", result.Summary)
	}
}

// TestCovViewListCommunitiesPropagatesMemberReadFailure covers the per-community
// member read. A community reported without its members would look like an
// empty cluster.
func TestCovViewListCommunitiesPropagatesMemberReadFailure(t *testing.T) {
	e := engineWithStore(t, writeFixture(t), &fakeStore{
		communities: []graphstore.CommunityRow{{ID: 1, Name: "core", Size: 3}},
		allNodesErr: errFake,
	})
	_, err := e.ListCommunities(0, "")
	covViewWantFake(t, "ListCommunities", err)
}

// ── query.go: detect changes ─────────────────────────────────────────────────

// TestCovViewDetectChangesUnbuiltGraphIsEmptyReport covers the never-built
// graph: the report is the zero-count headline, not an error.
func TestCovViewDetectChangesUnbuiltGraphIsEmptyReport(t *testing.T) {
	e, _ := newEngine(t, nil)
	report, err := covViewDetect(e)
	if err != nil {
		t.Fatalf("DetectChanges: %v", err)
	}
	want := "0 changed symbol(s), 0 affected flow(s), 0 test gap(s); risk 0.00"
	if report.Summary != want {
		t.Fatalf("summary = %q, want %q", report.Summary, want)
	}
	if len(report.ChangedFunctions) != 0 {
		t.Fatalf("changed functions = %+v, want none", report.ChangedFunctions)
	}
}

// TestCovViewDetectChangesPropagatesStoreOpenFailure covers the lazy open.
func TestCovViewDetectChangesPropagatesStoreOpenFailure(t *testing.T) {
	if _, err := covViewDetect(unreadableEngine(t)); err == nil {
		t.Fatal("DetectChanges: want the store-open failure")
	}
}

// covViewUnnameableDepth / covViewUnnameableSegment nest a working directory
// deep enough and long enough that the process can no longer name it:
//
//   - 350 * 17 characters exceeds PATH_MAX on every supported platform, so the
//     getcwd syscall cannot return the path;
//   - a depth over 341 exceeds the length os.Getwd's "../.." fallback walk
//     allows itself, so the slow path cannot reconstruct it either.
//
// Both conditions are needed: either alone still yields a working directory.
const (
	covViewUnnameableDepth   = 350
	covViewUnnameableSegment = "dddddddddddddddd"
)

// covViewEnterUnnameableDir makes the test's working directory one that
// filepath.Abs cannot resolve, restoring the original on cleanup.
func covViewEnterUnnameableDir(t *testing.T) {
	t.Helper()

	t.Chdir(t.TempDir())
	for depth := range covViewUnnameableDepth {
		if err := os.Mkdir(covViewUnnameableSegment, 0o755); err != nil {
			t.Fatalf("mkdir at depth %d: %v", depth, err)
		}
		if err := os.Chdir(covViewUnnameableSegment); err != nil {
			t.Fatalf("chdir at depth %d: %v", depth, err)
		}
	}
	if _, err := filepath.Abs("x"); err == nil {
		t.Fatal("working directory is still nameable; the fixture no longer sets up the failure")
	}
}

// TestCovViewDetectChangesRejectsUnresolvableRepoRoot covers the `--repo`
// scoping arm: a root the process cannot resolve to an absolute path must be
// reported, never silently answered from the current engine's own graph —
// which would be a different repository's report under the caller's root.
//
// Resolution fails only when the working directory a relative root is joined
// to cannot be named, so the test moves into one that cannot be.
func TestCovViewDetectChangesRejectsUnresolvableRepoRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		// SetCurrentDirectory is capped at MAX_PATH, so Windows cannot even
		// enter such a directory, and filepath.Abs there does not consult the
		// working directory at all. The POSIX matrix jobs cover this branch
		// in the merged multi-OS profile.
		return
	}
	e := engineWithStore(t, writeFixture(t), &fakeStore{})
	covViewEnterUnnameableDir(t)

	_, err := e.DetectChanges(graphstore.DetectChangesOptions{
		RepoRoot: "other-repo",
		Files:    []string{covViewChangedFile},
	})
	if err == nil {
		t.Fatal("DetectChanges: want the repo-root resolution failure")
	}
}

// TestCovViewDetectChangesPropagatesSymbolReadFailure covers the per-file
// symbol read the changed set is assembled from.
func TestCovViewDetectChangesPropagatesSymbolReadFailure(t *testing.T) {
	e := engineWithStore(t, writeFixture(t), &fakeStore{nodesErr: errFake})
	_, err := covViewDetect(e)
	covViewWantFake(t, "DetectChanges", err)
}

// TestCovViewDetectChangesPropagatesEdgeReadFailure covers the edge read the
// caller counts and the test-gap set are both derived from. Reporting zero
// callers and a universal test gap would be a confident lie.
func TestCovViewDetectChangesPropagatesEdgeReadFailure(t *testing.T) {
	e := engineWithStore(t, writeFixture(t), &fakeStore{allEdgesErr: errFake})
	_, err := covViewDetect(e)
	covViewWantFake(t, "DetectChanges", err)
}

// TestCovViewDetectChangesPropagatesFlowMembershipFailure covers the
// membership read the affected-flow set is resolved through.
func TestCovViewDetectChangesPropagatesFlowMembershipFailure(t *testing.T) {
	e := covViewEngine(t, &fakeStore{membershipsErr: errFake},
		graphstore.GraphNode{ID: 1, Kind: kindFunction, QualifiedName: "lib.Greet"})
	_, err := covViewDetect(e)
	covViewWantFake(t, "DetectChanges", err)
}

// TestCovViewDetectChangesPropagatesFlowReadFailure covers the second read of
// the flow lookup: memberships resolved, but the flow rows themselves did not.
func TestCovViewDetectChangesPropagatesFlowReadFailure(t *testing.T) {
	e := covViewEngine(t, &fakeStore{
		memberships: []graphstore.FlowMembershipRow{{FlowID: 7, NodeID: 1}},
		flowsErr:    errFake,
	}, graphstore.GraphNode{ID: 1, Kind: kindFunction, QualifiedName: "lib.Greet"})
	_, err := covViewDetect(e)
	covViewWantFake(t, "DetectChanges", err)
}

// TestCovViewDetectChangesCollapsesRepeatedFiles covers the seen-file guard:
// two spellings of the same path are one file, so its symbols are reported
// once. A duplicated symbol would double-count into the review priorities.
func TestCovViewDetectChangesCollapsesRepeatedFiles(t *testing.T) {
	store := &fakeStore{}
	e := covViewEngine(t, store,
		graphstore.GraphNode{ID: 1, Kind: kindFunction, Name: "Greet", QualifiedName: "lib.Greet"},
		graphstore.GraphNode{ID: 2, Kind: nodeKindFile, QualifiedName: covViewChangedFile},
	)
	report, err := e.DetectChanges(graphstore.DetectChangesOptions{
		Files: []string{covViewChangedFile, "./" + covViewChangedFile},
	})
	if err != nil {
		t.Fatalf("DetectChanges: %v", err)
	}
	if len(report.ChangedFunctions) != 1 || report.ChangedFunctions[0].QualifiedName != "lib.Greet" {
		t.Fatalf("changed functions = %+v, want lib.Greet once (File node excluded)", report.ChangedFunctions)
	}
}

// TestCovViewDetectChangesEmptyChangeSetSkipsFlowLookup covers the short
// circuit: with no changed symbols there is nothing to match, so the
// membership table is never read — proven by an injected failure on it that
// the report never surfaces.
func TestCovViewDetectChangesEmptyChangeSetSkipsFlowLookup(t *testing.T) {
	e := engineWithStore(t, writeFixture(t), &fakeStore{
		membershipsErr: errFake,
		flows:          []graphstore.FlowRow{{ID: 7, Name: "Run"}},
	})
	report, err := covViewDetect(e)
	if err != nil {
		t.Fatalf("DetectChanges: %v", err)
	}
	if len(report.AffectedFlows) != 0 {
		t.Fatalf("affected flows = %+v, want none for an empty change set", report.AffectedFlows)
	}
}

// TestCovViewDetectChangesChangeOutsideEveryFlow covers the no-hit arm: flows
// exist and symbols changed, but none of the changed symbols is a member, so
// the flow rows are never read back and no flow is reported.
func TestCovViewDetectChangesChangeOutsideEveryFlow(t *testing.T) {
	e := covViewEngine(t, &fakeStore{
		memberships: []graphstore.FlowMembershipRow{{FlowID: 7, NodeID: 99}},
		flowsErr:    errFake,
	}, graphstore.GraphNode{ID: 1, Kind: kindFunction, Name: "Greet", QualifiedName: "lib.Greet"})
	report, err := covViewDetect(e)
	if err != nil {
		t.Fatalf("DetectChanges: %v", err)
	}
	if len(report.AffectedFlows) != 0 {
		t.Fatalf("affected flows = %+v, want none: no changed symbol is a flow member", report.AffectedFlows)
	}
	if len(report.ChangedFunctions) != 1 {
		t.Fatalf("changed functions = %+v, want the one changed symbol", report.ChangedFunctions)
	}
}

// ── tools_communities.go: failure arms ───────────────────────────────────────

// TestCovViewCommunityToolsPropagateCommunityReadFailure covers the stored
// community read all three tools begin with.
func TestCovViewCommunityToolsPropagateCommunityReadFailure(t *testing.T) {
	for _, tool := range commTools {
		t.Run(tool, func(t *testing.T) {
			e := engineWithStore(t, writeFixture(t), &fakeStore{communitiesErr: errFake})
			covViewWantToolFake(t, e, tool, map[string]any{})
		})
	}
}

// TestCovViewListCommunitiesToolPropagatesStoreOpenFailure covers the lazy
// open behind the tool surface.
func TestCovViewListCommunitiesToolPropagatesStoreOpenFailure(t *testing.T) {
	_, err := covViewInvoke(t, unreadableEngine(t), "list_communities_tool", map[string]any{})
	if err == nil {
		t.Fatal("list_communities_tool: want the store-open failure")
	}
}

// TestCovViewListCommunitiesToolPropagatesMemberReadFailure covers the member
// read the listing performs per community.
func TestCovViewListCommunitiesToolPropagatesMemberReadFailure(t *testing.T) {
	e := engineWithStore(t, writeFixture(t), &fakeStore{
		communities: []graphstore.CommunityRow{{ID: 1, Name: "core", Size: 3}},
		allNodesErr: errFake,
	})
	covViewWantToolFake(t, e, "list_communities_tool", map[string]any{"min_size": 1})
}

// TestCovViewGetCommunityToolPropagatesMemberDetailFailure covers the SECOND
// member read, the one `include_members` adds: the community was found and
// listed, and only the detail expansion failed. Returning the community
// without `member_details` would answer a different question than the one the
// caller asked.
func TestCovViewGetCommunityToolPropagatesMemberDetailFailure(t *testing.T) {
	base := &fakeStore{
		communities: []graphstore.CommunityRow{{ID: 1, Name: "core", Size: 1}},
		allNodes: []graphstore.GraphNode{
			{ID: 1, Kind: kindFunction, Name: "Greet", QualifiedName: "lib.Greet", CommunityID: 1},
		},
	}
	store := &covViewMemberFailStore{fakeStore: base, ok: 1}
	e := engineWithStore(t, writeFixture(t), store)
	covViewWantToolFake(t, e, "get_community_tool", map[string]any{
		"community_id": 1, "include_members": true,
	})
	if store.calls != 2 {
		t.Fatalf("member reads = %d, want 2: the listing read and the detail read", store.calls)
	}
}

// TestCovViewArchitectureOverviewPropagatesEdgeReadFailure covers the edge
// read the coupling analysis is built from. An overview reporting zero
// cross-community coupling because the edges could not be read would read as
// a perfectly decoupled architecture.
func TestCovViewArchitectureOverviewPropagatesEdgeReadFailure(t *testing.T) {
	e := engineWithStore(t, writeFixture(t), &fakeStore{allEdgesErr: errFake})
	covViewWantToolFake(t, e, "get_architecture_overview_tool", map[string]any{})
}

// TestCovViewArchitectureOverviewUnbuiltGraphIsEmpty covers the never-built
// graph across both of the overview's reads: no communities and no edges
// degrade to an empty overview with the release's summary shape.
func TestCovViewArchitectureOverviewUnbuiltGraphIsEmpty(t *testing.T) {
	e, _ := newEngine(t, nil)
	payload := covViewPayload(t, e, "get_architecture_overview_tool", map[string]any{})

	want := "Architecture: 0 communities, 0 community pairs, 0 warning(s)"
	if payload["summary"] != want {
		t.Fatalf("summary = %#v, want %q", payload["summary"], want)
	}
	for _, key := range []string{"communities", "cross_community_edges", "warnings"} {
		if rows := covViewList(t, payload, key); len(rows) != 0 {
			t.Fatalf("%s = %#v, want an empty list", key, rows)
		}
	}
	if payload["truncated"] != false {
		t.Fatalf("truncated = %#v, want false", payload["truncated"])
	}
}

// TestCovViewMemberDetailsUnbuiltGraphIsEmptyList covers the never-built graph
// under `include_members`. The empty list has to be non-nil: the key is
// transported as JSON and `null` is a different answer from `[]`.
func TestCovViewMemberDetailsUnbuiltGraphIsEmptyList(t *testing.T) {
	e, _ := newEngine(t, nil)
	details, err := commMemberDetails(e, 1)
	if err != nil {
		t.Fatalf("commMemberDetails: %v", err)
	}
	if details == nil || len(details) != 0 {
		t.Fatalf("details = %#v, want an empty non-nil list", details)
	}
}

// TestCovViewMemberDetailsPropagatesStoreOpenFailure covers the lazy open in
// the member-detail read.
func TestCovViewMemberDetailsPropagatesStoreOpenFailure(t *testing.T) {
	if _, err := commMemberDetails(unreadableEngine(t), 1); err == nil {
		t.Fatal("commMemberDetails: want the store-open failure")
	}
}

// TestCovViewAllEdgesDegradesAndPropagates covers the overview's edge read on
// its own: a never-built graph has no edges, an unopenable one is an error.
func TestCovViewAllEdgesDegradesAndPropagates(t *testing.T) {
	e, _ := newEngine(t, nil)
	edges, err := commAllEdges(e)
	if err != nil {
		t.Fatalf("commAllEdges: %v", err)
	}
	if len(edges) != 0 {
		t.Fatalf("edges = %+v, want none for a never-built graph", edges)
	}
	if _, err := commAllEdges(unreadableEngine(t)); err == nil {
		t.Fatal("commAllEdges: want the store-open failure")
	}
}

// ── tools_communities.go: minimal overview aggregation ───────────────────────

// covViewCrossEdge is one entry of the full overview's cross-edge list.
func covViewCrossEdge(source, target int64, kind string) map[string]any {
	return map[string]any{
		"source_community": source,
		"target_community": target,
		"edge_kind":        kind,
		"source":           "lib.Greet",
		"target":           "app.Run",
	}
}

// covViewPairField reads one field of the single aggregated pair the minimal
// overview is expected to produce.
func covViewPairField(t *testing.T, pairs []any, field string) any {
	t.Helper()

	if len(pairs) != 1 {
		t.Fatalf("cross pairs = %#v, want exactly one aggregated pair", pairs)
	}
	pair, ok := pairs[0].(map[string]any)
	if !ok {
		t.Fatalf("pair = %#v, want an object", pairs[0])
	}
	return pair[field]
}

// TestCovViewMinimalOverviewIgnoresForeignEdgeEntries covers the minimal
// aggregation's entry guard: anything in the cross-edge list that is not an
// edge dict contributes nothing, rather than aggregating as a (0,0) pair whose
// count would inflate the real pair's neighbour.
func TestCovViewMinimalOverviewIgnoresForeignEdgeEntries(t *testing.T) {
	full := commOverview{
		communities: []map[string]any{
			{"id": int64(1), "name": "core", "size": 2, "cohesion": 0.5},
			{"id": int64(2), "name": "api", "size": 3, "cohesion": 0.25},
		},
		crossEdges: []any{
			"not-an-edge",
			covViewCrossEdge(1, 2, "CALLS"),
		},
	}
	communities, pairs := commMinimalOverview(full)
	if len(communities) != 2 {
		t.Fatalf("communities = %#v, want both reduced rows", communities)
	}
	if got := covViewPairField(t, pairs, "edge_count"); got != 1 {
		t.Fatalf("edge_count = %#v, want 1: the foreign entry must not aggregate", got)
	}
	if got := covViewPairField(t, pairs, "source_community"); got != "core" {
		t.Fatalf("source_community = %#v, want the community name", got)
	}
}

// TestCovViewMinimalOverviewLabelsCommunityOutsidePartition covers the label
// fallback: an edge endpoint naming a community the overview does not carry is
// rendered as the release's `community-<id>` placeholder, so the pair is still
// attributable instead of appearing under an empty name.
func TestCovViewMinimalOverviewLabelsCommunityOutsidePartition(t *testing.T) {
	full := commOverview{
		communities: []map[string]any{{"id": int64(1), "name": "core", "size": 2, "cohesion": 0.5}},
		crossEdges:  []any{covViewCrossEdge(1, 9, "IMPORTS")},
	}
	_, pairs := commMinimalOverview(full)
	if got := covViewPairField(t, pairs, "target_community"); got != "community-9" {
		t.Fatalf("target_community = %#v, want the community-9 placeholder", got)
	}
}
