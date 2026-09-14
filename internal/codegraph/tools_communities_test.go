package codegraph

// Community-tool parity against the pinned code-review-graph v2.3.8 release.
//
// Each case seeds the store DIRECTLY from a recorded graph — never by running
// the scanner or community detection — so a failure here is a failure of the
// three handlers in tools_communities.go and of nothing else. Two graphs are
// exercised:
//
//   - testdata/crg-release/v2.3.8/graph.json, the generated release contract.
//     Its repository's only inter-package calls resolve to bare target names,
//     so it pins the community listing, detail and member-bounding behaviour
//     but leaves `cross_community_edges` and the coupling warnings EMPTY.
//   - testdata/community-coupling/graph.json, a synthetic graph built to reach
//     exactly what the release fixture cannot: cross-community edges of two
//     kinds, the minimal-mode pair aggregation, a pair above the warning
//     threshold, a second pair suppressed because it is test-dominated, a
//     TESTED_BY pair excluded entirely, and a discriminating sort_by=name
//     order. Its expected answers are the RELEASE's, recorded by the oracle
//     script sitting next to it.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/crgrelease"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// commCouplingDir holds the synthetic cross-community graph and the release
// answers the oracle recorded for it.
const commCouplingDir = "testdata/community-coupling"

// commTools are the three tools this file owns. Cases are discovered by
// globbing, so a fixture added for any of them — a new behaviour case or a
// bound-violation case from the validation sweep — is covered automatically.
var commTools = []string{
	"list_communities_tool",
	"get_community_tool",
	"get_architecture_overview_tool",
}

func TestCommunityToolsMatchReleaseContract(t *testing.T) {
	for _, scenario := range []struct {
		name string
		// dir holds graph.json and calls/.
		dir string
		// copyRepo materializes the repository the graph's absolute paths
		// belong to, and returns its root.
		copyRepo func(*testing.T) string
		// standardCase is the case whose payload is the un-minimized
		// baseline for the other cases' context-savings estimate.
		standardCase string
	}{
		{
			name:         "release_contract",
			dir:          releaseContractDir,
			copyRepo:     copyReleaseRepo,
			standardCase: "get_architecture_overview_tool__standard",
		},
		{
			name:         "cross_community_coupling",
			dir:          commCouplingDir,
			copyRepo:     commCouplingRepo,
			standardCase: "get_architecture_overview_tool__coupling_standard",
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			root := scenario.copyRepo(t)
			seed := commLoadGraph(t, scenario.dir, root)
			engine := commEngineFromSeed(t, root, seed)
			baseline := commBaseline(t, scenario.dir, scenario.standardCase, root, seed)

			cases := commFixturePaths(t, scenario.dir)
			if len(cases) == 0 {
				t.Fatalf("no call fixtures for %v under %s", commTools, scenario.dir)
			}
			for _, path := range cases {
				name := strings.TrimSuffix(filepath.Base(path), ".json")
				t.Run(name, func(t *testing.T) {
					commAssertCase(t, engine, commLoadFixture(t, path, root), baseline)
				})
			}
		})
	}
}

// TestCommunityToolHardCeilingsMatchRelease pins the three ceilings that sit
// ABOVE whatever the caller asked for, and that no recorded fixture can reach
// because the fixture repositories are far too small.
//
// The expected numbers were OBSERVED, not read off the constants: a database
// with 250 communities, 40 members in the first and 480 cross-community edges
// was handed to the v2.3.8 wheel with max_results = max_members = 5000, and it
// answered 200 communities / 25 members / 200 edge rows with the untruncated
// totals 250 / 40 / 480 and `truncated` set.
//
// The fourth assertion is the one most easily got wrong by symmetry: the
// architecture overview does NOT bound its community list. `max_results`
// governs the cross-community rows and the warnings only, so all 250
// communities come back — each with its OWN member list bounded.
func TestCommunityToolHardCeilingsMatchRelease(t *testing.T) {
	const (
		communities    = 250
		membersInFirst = 40
		crossPairs     = 240
		crossEdges     = crossPairs * 2
		maxCommunities = 200
		maxMembers     = 25
		maxCrossEdges  = 200
		wayOverCeiling = 5000
	)

	root := filepath.Join(t.TempDir(), "ceilings")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", root, err)
	}
	seed := commSeed{}
	memberNames := make([][]string, communities)
	for community := range communities {
		size := 2
		if community == 0 {
			size = membersInFirst
		}
		file := fmt.Sprintf("%s/pkg%d/f.go", root, community)
		for member := range size {
			name := fmt.Sprintf("S%d_%d", community, member)
			seed.Nodes = append(seed.Nodes, commSeedNode{
				ID:            int64(len(seed.Nodes)) + 1,
				Kind:          "Function",
				Name:          name,
				QualifiedName: file + "::" + name,
				FilePath:      file,
				LineStart:     member,
				LineEnd:       member + 1,
				Language:      "go",
			})
			memberNames[community] = append(memberNames[community], file+"::"+name)
		}
		seed.Communities = append(seed.Communities, commSeedCommunity{
			ID: int64(community) + 1, Name: fmt.Sprintf("pkg%03d", community),
			Cohesion: 0.5, Size: size, DominantLanguage: "go",
			Description: fmt.Sprintf("probe %d", community),
			Members:     memberNames[community],
		})
	}
	for community := 1; community <= crossPairs; community++ {
		for _, target := range memberNames[community] {
			seed.Edges = append(seed.Edges, commSeedEdge{
				Kind:     "CALLS",
				Source:   memberNames[0][0],
				Target:   target,
				FilePath: fmt.Sprintf("%s/pkg0/f.go", root),
			})
		}
	}
	if len(seed.Edges) != crossEdges {
		t.Fatalf("seeded %d cross edges, want %d", len(seed.Edges), crossEdges)
	}
	engine := commEngineFromSeed(t, root, seed)

	listed := commCall(t, engine, "list_communities_tool", map[string]any{
		"max_results": wayOverCeiling, "max_members": wayOverCeiling,
	})
	commWant(t, listed, map[string]any{
		"total": float64(communities), "truncated": true,
		"summary": fmt.Sprintf("Found %d communities, showing %d of %d",
			communities, maxCommunities, communities),
	})
	rows, _ := listed["communities"].([]any)
	if len(rows) != maxCommunities {
		t.Fatalf("list_communities returned %d communities, want the %d ceiling",
			len(rows), maxCommunities)
	}
	first, _ := rows[0].(map[string]any)
	commWant(t, first, map[string]any{
		"size": float64(membersInFirst), "members_total": float64(membersInFirst),
		"members_truncated": true,
	})
	if members, _ := first["members"].([]any); len(members) != maxMembers {
		t.Fatalf("largest community listed %d members, want the %d ceiling",
			len(members), maxMembers)
	}

	detail := commCall(t, engine, "get_community_tool", map[string]any{
		"community_id": 1, "max_members": wayOverCeiling,
	})
	community, _ := detail["community"].(map[string]any)
	commWant(t, community, map[string]any{
		"size": float64(membersInFirst), "members_total": float64(membersInFirst),
		"members_truncated": true,
	})
	if members, _ := community["members"].([]any); len(members) != maxMembers {
		t.Fatalf("get_community returned %d members, want the %d ceiling",
			len(members), maxMembers)
	}

	overview := commCall(t, engine, "get_architecture_overview_tool", map[string]any{
		"detail_level": "standard", "max_results": wayOverCeiling,
		"max_members": wayOverCeiling,
	})
	commWant(t, overview, map[string]any{
		"cross_community_edges_total": float64(crossEdges), "truncated": true,
	})
	if edges, _ := overview["cross_community_edges"].([]any); len(edges) != maxCrossEdges {
		t.Fatalf("overview returned %d cross-community rows, want the %d ceiling",
			len(edges), maxCrossEdges)
	}
	listedCommunities, _ := overview["communities"].([]any)
	if len(listedCommunities) != communities {
		t.Fatalf("overview returned %d communities, want all %d — max_results bounds "+
			"the cross-community rows and warnings, not the community list",
			len(listedCommunities), communities)
	}
	largest, _ := listedCommunities[0].(map[string]any)
	if members, _ := largest["members"].([]any); len(members) != maxMembers {
		t.Fatalf("overview listed %d members for the largest community, want the %d ceiling",
			len(members), maxMembers)
	}
}

// commCall binds arguments through the release's schema and returns the
// handler's payload rendered as transported JSON.
func commCall(t *testing.T, engine *Engine, name string, arguments map[string]any) map[string]any {
	t.Helper()

	tool, ok := crgrelease.Lookup(name)
	if !ok {
		t.Fatalf("%s is not published by code-review-graph %s", name, crgrelease.Version)
	}
	raw, err := json.Marshal(arguments)
	if err != nil {
		t.Fatalf("marshal arguments: %v", err)
	}
	args, bindErr := tool.Bind(raw)
	if bindErr != nil {
		t.Fatalf("bind %s%v: %v", name, arguments, bindErr)
	}
	engine.ResetSession()
	got, err := toolHandlers[name](engine, args)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	payload, ok := commCanonical(t, got).(map[string]any)
	if !ok {
		t.Fatalf("%s returned %T, want an object", name, got)
	}
	return payload
}

// commWant asserts the named fields of a payload, leaving the rest alone.
func commWant(t *testing.T, got map[string]any, want map[string]any) {
	t.Helper()
	for _, key := range commKeyUnion(want, nil) {
		if diff := commDiff(key, got[key], want[key]); diff != "" {
			t.Errorf("%s", diff)
		}
	}
}

// commAssertCase binds the recorded arguments through the release's own schema,
// runs the handler, and compares the WHOLE payload.
func commAssertCase(t *testing.T, engine *Engine, f commFixture, baseline commFixture) {
	t.Helper()

	tool, ok := crgrelease.Lookup(f.Tool)
	if !ok {
		t.Fatalf("%s is not published by code-review-graph %s", f.Tool, crgrelease.Version)
	}
	args, bindErr := tool.Bind(f.Arguments)
	if bindErr != nil {
		t.Fatalf("bind %s%s: %v", f.Tool, f.Arguments, bindErr)
	}
	handler, ok := toolHandlers[f.Tool]
	if !ok {
		t.Fatalf("%s has no native handler", f.Tool)
	}

	// The generator resets the hint session before every case, so each
	// recorded `_hints` block is the fresh-session value.
	engine.ResetSession()
	got, err := handler(engine, args)

	if f.IsError {
		// A bound violation escapes upstream's try block, so it is an MCP
		// transport error with no payload at all. The uniform
		// "Error calling tool '<tool>': " prefix is the server's, not the
		// handler's.
		want := strings.TrimPrefix(f.ErrorText, fmt.Sprintf("Error calling tool '%s': ", f.Tool))
		if err == nil {
			t.Fatalf("want error %q, got payload %v", want, got)
		}
		if err.Error() != want {
			t.Fatalf("error = %q, want %q", err.Error(), want)
		}
		return
	}
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	// `_graph` is Engine.CallTool's provenance envelope, attached around the
	// handler rather than by it.
	want := map[string]any{}
	for key, value := range f.Structured {
		if key != "_graph" {
			want[key] = value
		}
	}
	have := commCanonical(t, got)
	if diff := commDiff("", have, commCanonical(t, want)); diff != "" {
		t.Fatalf("payload mismatch at %s", diff)
	}
	if _, ok := want["context_savings"]; ok {
		commAssertSavings(t, f, baseline, have)
	}
}

// commAssertSavings checks the context-savings block against a value DERIVED
// from the release's own recorded payloads for this repository root.
//
// The recorded numbers themselves cannot be compared: they count tokens over
// payloads that embed absolute paths, so they scale with the length of the
// generating machine's temp root and the fixture normalizes them to
// `${SAVED_TOKENS}` / `${SAVED_PERCENT}`. What IS portable is the relation —
// the baseline is the FULL overview the minimal call avoided sending, and the
// returned side is the payload including `_hints`, which is why upstream
// attaches the block last. Both inputs below are release-recorded payloads, so
// a handler that measured the ratio over the wrong thing fails here, and a
// handler whose payload diverged at all fails the comparison above.
func commAssertSavings(t *testing.T, f, baseline commFixture, got any) {
	t.Helper()

	full := map[string]any{
		"communities":           baseline.Structured["communities"],
		"cross_community_edges": baseline.Structured["cross_community_edges"],
		"warnings":              baseline.Structured["warnings"],
	}
	returned := map[string]any{}
	for key, value := range f.Structured {
		if key != "_graph" && key != "context_savings" {
			returned[key] = value
		}
	}
	original := EstimateTokens(full)
	if original <= 0 {
		t.Fatalf("baseline estimate %d, want a positive token count", original)
	}
	saved := original - EstimateTokens(returned)
	if saved < 0 {
		saved = 0
	}
	want := map[string]any{
		"estimated":     true,
		"saved_tokens":  float64(saved),
		"saved_percent": math.Round(float64(saved) / float64(original) * 100),
	}
	payload, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("payload is %T, want an object", got)
	}
	if diff := commDiff("context_savings", payload["context_savings"], want); diff != "" {
		t.Fatalf("context_savings mismatch at %s", diff)
	}
}

// commBaseline loads the un-minimized standard overview whose token count is
// the baseline every minimal call's saving is measured against, and restores
// the member lists the recording itself bounded.
//
// The restoration is necessary, not cosmetic: a standard overview caps each
// community's member list at `max_members` (default 10, ceiling 25), so a
// community with more members than that has NO recorded payload carrying its
// full list — the release cannot emit one. The un-capped list is what
// `get_architecture_overview(store)` returned internally, and therefore what
// the release measured. Field set, field values and community order still come
// from the recording; only the bounded lists are put back.
func commBaseline(t *testing.T, dir, standardCase, root string, seed commSeed) commFixture {
	t.Helper()

	baseline := commLoadFixture(t, filepath.Join(dir, "calls", standardCase+".json"), root)
	if truncated, _ := baseline.Structured["truncated"].(bool); truncated {
		t.Fatalf("%s is itself truncated, so it cannot be the un-minimized baseline",
			standardCase)
	}
	// Keyed and valued by the SANITIZED spellings, because that is what a
	// recorded payload carries: a name or qualified name reaching a response
	// has had its ASCII control characters stripped.
	members := make(map[string][]any, len(seed.Communities))
	for _, community := range seed.Communities {
		names := make([]any, 0, len(community.Members))
		for _, name := range community.Members {
			names = append(names, SanitizeName(name))
		}
		members[SanitizeName(community.Name)] = names
	}
	recorded, _ := baseline.Structured["communities"].([]any)
	restored := make([]any, 0, len(recorded))
	for _, entry := range recorded {
		community, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("%s records a %T in communities", standardCase, entry)
		}
		full := make(map[string]any, len(community))
		for key, value := range community {
			if key == "members_total" || key == "members_truncated" {
				continue
			}
			full[key] = value
		}
		name, _ := community["name"].(string)
		names, ok := members[name]
		if !ok {
			t.Fatalf("%s records community %q, which the seeded graph does not have",
				standardCase, name)
		}
		full["members"] = names
		restored = append(restored, full)
	}
	baseline.Structured["communities"] = restored
	return baseline
}

// ─── seeding ─────────────────────────────────────────────────────────────────

// commEngineFromSeed materializes a graph into a real store under root.
//
// Nodes and edges go through the published writer so their persisted identities
// are built by graphstore, not asserted by the test; the recorded ids are then
// required to match, which is what makes `member_details[].id` meaningful.
// Communities go through ReplaceCommunities, which also re-points
// nodes.community_id — the column every member list is read back through.
func commEngineFromSeed(t *testing.T, root string, graph commSeed) *Engine {
	t.Helper()

	engine := Open(root)
	t.Cleanup(func() { _ = engine.Close() })
	store, err := engine.writeStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	for _, node := range graph.Nodes {
		id, err := store.UpsertNode(graphstore.NodeInfo{
			Kind:       node.Kind,
			Name:       node.Name,
			FilePath:   node.FilePath,
			LineStart:  node.LineStart,
			LineEnd:    node.LineEnd,
			Language:   node.Language,
			ParentName: node.ParentName,
			Params:     node.Params,
			ReturnType: node.ReturnType,
			Modifiers:  node.Modifiers,
			IsTest:     node.IsTest,
		}, "")
		if err != nil {
			t.Fatalf("upsert node %s: %v", node.QualifiedName, err)
		}
		if id != node.ID {
			t.Fatalf("node %s got id %d, recorded id %d", node.QualifiedName, id, node.ID)
		}
		stored, err := store.GetNode(node.QualifiedName)
		if err != nil || stored == nil {
			t.Fatalf("node %s is not retrievable by its recorded qualified name (err %v)",
				node.QualifiedName, err)
		}
	}
	for _, edge := range graph.Edges {
		if _, err := store.UpsertEdge(graphstore.EdgeInfo{
			Kind:     edge.Kind,
			Source:   edge.Source,
			Target:   edge.Target,
			FilePath: edge.FilePath,
			Line:     edge.Line,
		}); err != nil {
			t.Fatalf("upsert edge %s -> %s: %v", edge.Source, edge.Target, err)
		}
	}

	rows := make([]graphstore.CommunityRow, 0, len(graph.Communities))
	members := make([][]string, 0, len(graph.Communities))
	for _, community := range graph.Communities {
		rows = append(rows, graphstore.CommunityRow{
			Name:             community.Name,
			Level:            community.Level,
			Cohesion:         community.Cohesion,
			Size:             community.Size,
			DominantLanguage: community.DominantLanguage,
			Description:      community.Description,
		})
		members = append(members, community.Members)
	}
	if _, err := store.ReplaceCommunities(rows, members); err != nil {
		t.Fatalf("replace communities: %v", err)
	}
	stored, err := store.ReadCommunities()
	if err != nil {
		t.Fatalf("read communities: %v", err)
	}
	if len(stored) != len(graph.Communities) {
		t.Fatalf("stored %d communities, recorded %d", len(stored), len(graph.Communities))
	}
	for i, community := range graph.Communities {
		if stored[i].ID != community.ID {
			t.Fatalf("community %q got id %d, recorded id %d",
				community.Name, stored[i].ID, community.ID)
		}
	}
	if err := store.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return engine
}

// commCouplingRepo materializes the synthetic graph's repository. The graph is
// recorded rather than scanned, so the tree only has to exist: the files carry
// the line ranges the recorded nodes claim, and nothing reads their contents.
func commCouplingRepo(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "coupling")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", root, err)
	}
	for _, name := range []string{"a.go", "b.go", "c_test.go", "d.go", "e.go"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("package synthetic\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return root
}

// ─── fixture decoding ────────────────────────────────────────────────────────

type commGraph struct {
	Nodes []struct {
		ID            int64    `json:"id"`
		Kind          string   `json:"kind"`
		Name          string   `json:"name"`
		QualifiedName string   `json:"qualified_name"`
		FilePath      string   `json:"file_path"`
		LineStart     int      `json:"line_start"`
		LineEnd       int      `json:"line_end"`
		Language      string   `json:"language"`
		ParentName    string   `json:"parent_name"`
		Params        string   `json:"params"`
		ReturnType    string   `json:"return_type"`
		Modifiers     string   `json:"modifiers"`
		IsTest        commBool `json:"is_test"`
		CommunityID   *int64   `json:"community_id"`
	} `json:"nodes"`
	Edges []struct {
		ID   int64  `json:"id"`
		Kind string `json:"kind"`
		// The release's dump names the endpoints after its columns; the
		// synthetic graph uses the short names the tool payloads use.
		SourceQualified string `json:"source_qualified"`
		TargetQualified string `json:"target_qualified"`
		ShortSource     string `json:"source"`
		ShortTarget     string `json:"target"`
		FilePath        string `json:"file_path"`
		Line            int    `json:"line"`
	} `json:"edges"`
	Communities []struct {
		ID               int64    `json:"id"`
		Name             string   `json:"name"`
		Level            int      `json:"level"`
		Cohesion         float64  `json:"cohesion"`
		Size             int      `json:"size"`
		DominantLanguage string   `json:"dominant_language"`
		Description      string   `json:"description"`
		Members          []string `json:"members"`
	} `json:"communities"`
}

// commBool accepts both spellings of a boolean column: the release dumps
// SQLite's 0/1 integer, the synthetic graph writes JSON true/false.
type commBool bool

func (b *commBool) UnmarshalJSON(raw []byte) error {
	var number float64
	if err := json.Unmarshal(raw, &number); err == nil {
		*b = number != 0
		return nil
	}
	var flag bool
	if err := json.Unmarshal(raw, &flag); err != nil {
		return err
	}
	*b = commBool(flag)
	return nil
}

type commSeedNode struct {
	ID            int64
	Kind          string
	Name          string
	QualifiedName string
	FilePath      string
	LineStart     int
	LineEnd       int
	Language      string
	ParentName    string
	Params        string
	ReturnType    string
	Modifiers     string
	IsTest        bool
}

type commSeedEdge struct {
	Kind     string
	Source   string
	Target   string
	FilePath string
	Line     int
}

type commSeedCommunity struct {
	ID               int64
	Name             string
	Level            int
	Cohesion         float64
	Size             int
	DominantLanguage string
	Description      string
	Members          []string
}

type commSeed struct {
	Nodes       []commSeedNode
	Edges       []commSeedEdge
	Communities []commSeedCommunity
}

// commLoadGraph reads graph.json, substitutes the placeholder root, and
// normalizes the two recorded shapes into one seed. Community membership comes
// from an explicit member list when the graph has one and from the nodes'
// community_id column otherwise; both end up ordered by node id, which is the
// order the release reads members back in.
func commLoadGraph(t *testing.T, dir, root string) commSeed {
	t.Helper()

	var raw commGraph
	commReadJSON(t, filepath.Join(dir, "graph.json"), root, &raw)

	seed := commSeed{}
	byCommunity := map[int64][]string{}
	for _, node := range raw.Nodes {
		seed.Nodes = append(seed.Nodes, commSeedNode{
			ID: node.ID, Kind: node.Kind, Name: node.Name,
			QualifiedName: node.QualifiedName, FilePath: node.FilePath,
			LineStart: node.LineStart, LineEnd: node.LineEnd,
			Language: node.Language, ParentName: node.ParentName,
			Params: node.Params, ReturnType: node.ReturnType,
			Modifiers: node.Modifiers, IsTest: bool(node.IsTest),
		})
		if node.CommunityID != nil {
			byCommunity[*node.CommunityID] = append(byCommunity[*node.CommunityID], node.QualifiedName)
		}
	}
	for _, edge := range raw.Edges {
		source, target := edge.SourceQualified, edge.TargetQualified
		if source == "" && target == "" {
			source, target = edge.ShortSource, edge.ShortTarget
		}
		seed.Edges = append(seed.Edges, commSeedEdge{
			Kind: edge.Kind, Source: source, Target: target,
			FilePath: edge.FilePath, Line: edge.Line,
		})
	}
	for _, community := range raw.Communities {
		members := community.Members
		if len(members) == 0 {
			members = byCommunity[community.ID]
		}
		seed.Communities = append(seed.Communities, commSeedCommunity{
			ID: community.ID, Name: community.Name, Level: community.Level,
			Cohesion: community.Cohesion, Size: community.Size,
			DominantLanguage: community.DominantLanguage,
			Description:      community.Description, Members: members,
		})
	}
	if len(seed.Nodes) == 0 || len(seed.Communities) == 0 {
		t.Fatalf("%s/graph.json has %d nodes and %d communities",
			dir, len(seed.Nodes), len(seed.Communities))
	}
	return seed
}

type commFixture struct {
	Tool      string
	Arguments json.RawMessage
	IsError   bool
	// ErrorText is the transport error text the release reported, including
	// the server's uniform prefix.
	ErrorText string
	// Structured is the recorded payload with integers kept integral, so the
	// token estimate over it matches the release's byte count.
	Structured map[string]any
}

func commLoadFixture(t *testing.T, path, root string) commFixture {
	t.Helper()

	var raw struct {
		Tool      string          `json:"tool"`
		Arguments json.RawMessage `json:"arguments"`
		IsError   bool            `json:"is_error"`
		Content   []struct {
			Text string `json:"text"`
		} `json:"content"`
		Structured json.RawMessage `json:"structured_content"`
	}
	commReadJSON(t, path, root, &raw)

	f := commFixture{
		Tool:       raw.Tool,
		Arguments:  raw.Arguments,
		IsError:    raw.IsError,
		Structured: commPayload(t, path, raw.Structured),
	}
	if len(raw.Content) > 0 {
		f.ErrorText = raw.Content[0].Text
	}
	if f.Tool == "" {
		t.Fatalf("%s records no tool name", path)
	}
	return f
}

// commReadJSON decodes a recorded file with the placeholder root substituted
// into every string, so an absolute path it records points at this test's
// repository copy.
//
// The substituted root is SLASH-NORMALIZED, because the graph's node identity
// is: scanFile stores filepath.ToSlash(filepath.Join(absRoot, rel)), so a
// stored absolute path is `C:/.../001/pkg/auth/auth.go` on Windows too, never
// `C:\...`. Substituting the native spelling would build a hybrid expectation
// (native-separator root, forward-slash tail) that matches nothing the product
// ever produces. On POSIX ToSlash is the identity, so the substituted bytes
// are unchanged there.
func commReadJSON(t *testing.T, path, root string, into any) {
	t.Helper()

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	body = bytes.ReplaceAll(body,
		[]byte(commJSONQuoted(t, repoRootPlaceholder)),
		[]byte(commJSONQuoted(t, filepath.ToSlash(root))))
	if err := json.Unmarshal(body, into); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}

// commPayload decodes a recorded payload with every number re-typed from its
// LITERAL: an integer without a decimal point stays integral, a float keeps
// its fractional spelling.
//
// That distinction is load-bearing for the context-savings check, because
// EstimateTokens counts the BYTES of Python's spelling. A plain decode into
// `any` makes every number a float64, which would re-spell `"size":9` as
// `"size":9.0` and inflate the baseline by two bytes per integer field.
func commPayload(t *testing.T, path string, raw json.RawMessage) map[string]any {
	t.Helper()

	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	var payload map[string]any
	if err := decoder.Decode(&payload); err != nil {
		t.Fatalf("decode %s structured_content: %v", path, err)
	}
	commRetypeMap(payload)
	return payload
}

// commJSONQuoted renders a string the way it appears inside a JSON document,
// without the surrounding quotes, so a substitution on the raw bytes cannot
// break escaping whatever the temp root's spelling contains.
func commJSONQuoted(t *testing.T, value string) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("quote %q: %v", value, err)
	}
	return string(encoded[1 : len(encoded)-1])
}

func commRetypeMap(m map[string]any) {
	for key, value := range m {
		m[key] = commRetypeValue(value)
	}
}

func commRetypeValue(value any) any {
	switch typed := value.(type) {
	case json.Number:
		text := typed.String()
		if !strings.ContainsAny(text, ".eE") {
			if number, err := typed.Int64(); err == nil {
				return number
			}
		}
		number, err := typed.Float64()
		if err != nil {
			return text
		}
		return number
	case []any:
		for i, item := range typed {
			typed[i] = commRetypeValue(item)
		}
		return typed
	case map[string]any:
		commRetypeMap(typed)
		return typed
	}
	return value
}

func commFixturePaths(t *testing.T, dir string) []string {
	t.Helper()

	var paths []string
	for _, tool := range commTools {
		matches, err := filepath.Glob(filepath.Join(dir, "calls", tool+"__*.json"))
		if err != nil {
			t.Fatalf("glob %s: %v", tool, err)
		}
		paths = append(paths, matches...)
	}
	sort.Strings(paths)
	return paths
}

// ─── comparison ──────────────────────────────────────────────────────────────

// commCanonical renders a value the way the MCP transport would, so a typed
// Go struct in the payload (the hint suggestions) compares against the
// recorded JSON on equal terms.
func commCanonical(t *testing.T, value any) any {
	t.Helper()

	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out any
	if err := json.Unmarshal(encoded, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}

// commDiff reports the first differing JSON path, or "" when equal. A recorded
// `${...}` value is a normalized volatile field and matches anything.
func commDiff(path string, got, want any) string {
	if text, ok := want.(string); ok && strings.HasPrefix(text, "${") {
		return ""
	}
	switch wanted := want.(type) {
	case map[string]any:
		gotMap, ok := got.(map[string]any)
		if !ok {
			return fmt.Sprintf("%s: got %T, want an object", commRoot(path), got)
		}
		for _, key := range commKeyUnion(gotMap, wanted) {
			gotValue, inGot := gotMap[key]
			wantValue, inWant := wanted[key]
			switch {
			case !inGot:
				return fmt.Sprintf("%s.%s: missing, want %v", commRoot(path), key, wantValue)
			case !inWant:
				return fmt.Sprintf("%s.%s: unexpected %v", commRoot(path), key, gotValue)
			}
			if diff := commDiff(path+"."+key, gotValue, wantValue); diff != "" {
				return diff
			}
		}
		return ""
	case []any:
		gotList, ok := got.([]any)
		if !ok {
			return fmt.Sprintf("%s: got %T, want an array", commRoot(path), got)
		}
		if len(gotList) != len(wanted) {
			return fmt.Sprintf("%s: length %d, want %d", commRoot(path), len(gotList), len(wanted))
		}
		for i := range wanted {
			if diff := commDiff(fmt.Sprintf("%s[%d]", path, i), gotList[i], wanted[i]); diff != "" {
				return diff
			}
		}
		return ""
	}
	if got != want {
		return fmt.Sprintf("%s: %#v, want %#v", commRoot(path), got, want)
	}
	return ""
}

func commRoot(path string) string {
	if path == "" {
		return "(root)"
	}
	return path
}

func commKeyUnion(a, b map[string]any) []string {
	seen := make(map[string]bool, len(a)+len(b))
	for key := range a {
		seen[key] = true
	}
	for key := range b {
		seen[key] = true
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
