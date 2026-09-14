package crgbehavior

import (
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// fixtureViews reads the seeded release graph through the production reader.
func fixtureViews(t *testing.T, seed string) BridgeViews {
	t.Helper()
	views, err := openGraph(t, seed).Views()
	if err != nil {
		t.Fatalf("read views: %v", err)
	}
	return views
}

// Criterion 2 compares v2.3.8's EXACT output, so the reader has to surface the
// release's own rows — ordered flow paths, per-flow metrics, snapshots,
// summaries and full risk rows — not a lossy projection of them.
func TestViewsReadTheReleaseRowsExactly(t *testing.T) {
	views := fixtureViews(t, twoFlowSeed)
	entry, step, widget := repoFile("pkg/a.go", "Entry"), repoFile("pkg/a.go", "Step"), repoFile("pkg/b.go", "Widget")

	t.Run("flows keep identity, ordered path and metrics", func(t *testing.T) {
		vsTSame(t, "flows", views.Flows, []BridgeFlow{{
			Name: "Entry", EntryPoint: entry, Path: []string{entry, step},
			Depth: 1, NodeCount: 2, FileCount: 1, Criticality: 0.1875,
		}})
	})

	t.Run("snapshots are re-keyed off the autoincrement flow id", func(t *testing.T) {
		vsTSame(t, "flow_snapshots", views.FlowSnapshots, []BridgeFlowSnapshot{{
			EntryPoint: entry, Name: "Entry",
			CriticalPath: []string{"pkg/a.go::Entry", "pkg/a.go::Step"},
			Criticality:  0.1875, NodeCount: 2, FileCount: 1,
		}})
	})

	// The cluster key is the smallest member id, so a relabelled partition
	// still compares equal.
	t.Run("communities use a relabel-invariant cluster key", func(t *testing.T) {
		vsTSame(t, "communities", views.Communities,
			map[string]string{entry: entry, step: entry, widget: unassignedCluster})
		vsTSame(t, "communities assigned", views.CommunitiesAssigned, 2)
		vsTSame(t, "community_summaries", views.CommunitySummaries, []BridgeCommunitySummary{{
			Cluster: entry, Name: "pkg", Purpose: "pkg",
			KeySymbols: []string{"Entry", "Step"}, Risk: "unknown", Size: 2, DominantLanguage: "go",
		}})
	})

	t.Run("risk rows are read in full", func(t *testing.T) {
		vsTSame(t, "risk_index", views.RiskIndex, map[string]BridgeRisk{
			entry: {QualifiedName: "pkg/a.go::Entry", RiskScore: 0.3, CallerCount: 0, TestCoverage: "untested"},
			step:  {QualifiedName: "pkg/a.go::Step", RiskScore: 0.7, CallerCount: 4, TestCoverage: "untested", SecurityRelevant: true},
		})
	})

	t.Run("schema-v9 edge confidence is preserved with the upstream kind", func(t *testing.T) {
		vsTSame(t, "edges", views.Edges, []BridgeEdge{
			{Kind: "CALLS", From: "pkg/a.go::Entry", To: "pkg/a.go::Step", FilePath: "pkg/a.go", Line: 4,
				Confidence: 1.0, ConfidenceTier: "EXTRACTED"},
			{Kind: edgeKindImportsFrom, From: "pkg/a.go::Entry", To: "pkg/b.go::Widget", FilePath: "pkg/a.go", Line: 1,
				Confidence: 0.75, ConfidenceTier: "INFERRED"},
			{Kind: "CALLS", From: "pkg/a.go::Step", To: "append", FilePath: "pkg/a.go", Line: 11,
				Confidence: 0.5, ConfidenceTier: "HEURISTIC"},
		})
	})

	t.Run("the FTS index content is normalized and deduplicated", func(t *testing.T) {
		vsTSame(t, "fts index", views.FTSIndex, []string{"pkg/a.go::Entry", "pkg/a.go::Step", "pkg/b.go::Widget"})
	})
}

// vsTSame fails unless a read view equals the release rows it must reproduce.
// Every exactness assertion goes through it so each subtest above stays one
// statement of data plus one comparison.
func vsTSame[T any](t *testing.T, label string, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s = %+v, want %+v", label, got, want)
	}
}

// flow_memberships is derived from the release's ordered path_json, so position
// is the release's own step number rather than a re-derived one.
func TestFlowMembershipsFlattenTheOrderedPath(t *testing.T) {
	views := fixtureViews(t, twoFlowSeed)
	entry, step := repoFile("pkg/a.go", "Entry"), repoFile("pkg/a.go", "Step")
	want := []FlowMembershipRow{{entry, entry, 0}, {entry, step, 1}}
	got := make([]FlowMembershipRow, 0, 2)
	for _, m := range views.FlowMemberships() {
		got = append(got, FlowMembershipRow{m.FlowID, m.MemberID, m.Position})
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("flow memberships = %+v, want %+v", got, want)
	}
}

// FlowMembershipRow is a comparable projection of crg.FlowMembership.
type FlowMembershipRow struct {
	FlowID   string
	MemberID string
	Position int
}

// The ingestion corpus maps the release's IMPORTS_FROM spelling onto the
// kg-native IMPORTS one; everything else is carried through untouched.
func TestCorpusLowersTheReleaseGraph(t *testing.T) {
	views := fixtureViews(t, twoFlowSeed)
	corpus := views.Corpus("deadbeef")
	if corpus.Commit != "deadbeef" || len(corpus.Symbols) != 3 {
		t.Fatalf("corpus = %s with %d symbols, want deadbeef with 3", corpus.Commit, len(corpus.Symbols))
	}
	kinds := map[string]int{}
	for _, ref := range corpus.References {
		kinds[ref.Kind]++
	}
	if kinds["IMPORTS"] != 1 || kinds[edgeKindImportsFrom] != 0 {
		t.Fatalf("reference kinds = %v, want IMPORTS_FROM mapped onto IMPORTS", kinds)
	}
}

// The release's own FTS5 index answers the search, so the gate compares search
// RESULTS rather than a token dump it approximated itself.
func TestSearchFTSRunsTheReleasesOwnIndex(t *testing.T) {
	store := openGraph(t, twoFlowSeed)
	hits, err := store.SearchFTS("Entry")
	if err != nil {
		t.Fatalf("SearchFTS: %v", err)
	}
	if want := []string{"pkg/a.go::Entry"}; !reflect.DeepEqual(hits, want) {
		t.Fatalf("SearchFTS(Entry) = %v, want %v", hits, want)
	}
	// An identifier carrying FTS5 syntax must be searched literally, not parsed
	// as a query operator.
	if _, err := store.SearchFTS(`weird"-token*`); err != nil {
		t.Fatalf("SearchFTS with FTS5 syntax in the term: %v", err)
	}
}

// An off-release bridge is a hard failure: comparing against an unpinned build
// certifies nothing while looking like evidence.
func TestOpenBridgeStoreRejectsAnOffReleaseBridge(t *testing.T) {
	_, err := OpenBridgeStore(graphRoot, newGraphDB(t, twoFlowSeed), "2.2.0", testRelease())
	if !errors.Is(err, ErrReleaseMismatch) {
		t.Fatalf("OpenBridgeStore error = %v, want ErrReleaseMismatch", err)
	}
}

// A repository with no built graph cannot be compared; that is an environment
// fact, reported as an unavailable bridge rather than a divergence.
func TestOpenBridgeStoreReportsAMissingGraph(t *testing.T) {
	_, err := OpenBridgeStore(graphRoot, "/nonexistent/graph.db", PinnedVersion, testRelease())
	if !errors.Is(err, ErrBridgeUnavailable) {
		t.Fatalf("OpenBridgeStore error = %v, want ErrBridgeUnavailable", err)
	}
}

// A stored path outside the graph root cannot be normalized into the comparison
// id space. Rewriting it would invent an id; the reader refuses instead.
func TestViewsRejectPathsOutsideTheGraphRoot(t *testing.T) {
	seed := twoFlowSeed + `
INSERT INTO nodes (id,kind,name,qualified_name,file_path,line_start,language,file_hash) VALUES
 (4,'Function','Stray','/elsewhere/x.go::Stray','/elsewhere/x.go',1,'go','h3');
`
	if _, err := openGraph(t, seed).Views(); !errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("Views error = %v, want ErrOutsideRoot", err)
	}
}

// A derived row naming a node the graph does not hold means the readback is
// inconsistent. That is a failure, not a quietly dropped row.
func TestViewsRejectDanglingDerivedRows(t *testing.T) {
	cases := map[string]string{
		"flow path":   "\nUPDATE flows SET path_json='[1,99]' WHERE id=41;\n",
		"flow entry":  "\nUPDATE flows SET entry_point_id=99 WHERE id=41;\n",
		"risk row":    "\nUPDATE risk_index SET node_id=99 WHERE node_id=1;\n",
		"snapshot id": "\nUPDATE flow_snapshots SET flow_id=99 WHERE flow_id=41;\n",
	}
	for name, mutation := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := openGraph(t, twoFlowSeed+mutation).Views(); err == nil {
				t.Fatalf("a dangling %s was accepted", name)
			}
		})
	}
}

// An empty optional view is readable but contributes nothing; the reader must
// not invent rows for it.
func TestViewsSkipUncomputedOptionalTables(t *testing.T) {
	seed := twoFlowSeed + "\nDELETE FROM flow_snapshots;\nDELETE FROM community_summaries;\n"
	views := fixtureViews(t, seed)
	if len(views.FlowSnapshots) != 0 || len(views.CommunitySummaries) != 0 {
		t.Fatalf("uncomputed views produced rows: %+v / %+v", views.FlowSnapshots, views.CommunitySummaries)
	}
	if len(views.Flows) != 1 {
		t.Fatalf("an uncomputed summary table must not disturb the flows read: %+v", views.Flows)
	}
}

// --- vsT ---------------------------------------------------------------
// Seeds below are REAL pinned-release stores; each is named for the single
// fact it proves about the reader.

// vsTSecondFlowSeed adds a second traced flow, a second community and their
// summaries, so every ordering the reader promises has at least two rows to
// order — and it stores the second community's members in the order the
// canonical cluster key must NOT inherit (Zeta first, Alpha second).
const vsTSecondFlowSeed = twoFlowSeed + `
INSERT INTO nodes (id,kind,name,qualified_name,file_path,line_start,language,file_hash,signature,community_id) VALUES
 (10,'Function','Zeta','/abs/repo/pkg/c.go::Zeta','/abs/repo/pkg/c.go',1,'go','h3','def Zeta()',3),
 (11,'Function','Alpha','/abs/repo/pkg/c.go::Alpha','/abs/repo/pkg/c.go',5,'go','h3','def Alpha()',3);
INSERT INTO communities (id,name,size,dominant_language) VALUES (3,'pkgc',2,'go');
INSERT INTO community_summaries (community_id,name,purpose,key_symbols,risk,size,dominant_language) VALUES
 (3,'pkgc','pkgc','["Alpha","Zeta"]','low',2,'go');
INSERT INTO flows (id,name,entry_point_id,depth,node_count,file_count,criticality,path_json) VALUES
 (40,'Alpha',11,1,2,1,0.5,'[11,10]');
INSERT INTO flow_snapshots (flow_id,name,entry_point,critical_path,criticality,node_count,file_count) VALUES
 (40,'Alpha','/abs/repo/pkg/c.go::Alpha','["/abs/repo/pkg/c.go::Alpha","/abs/repo/pkg/c.go::Zeta"]',0.5,2,1);
`

// vsTSharedEntrySeed adds a second flow traced from the SAME entry point, so
// the membership order has to tie-break on something other than the flow id.
const vsTSharedEntrySeed = twoFlowSeed + `
INSERT INTO flows (id,name,entry_point_id,depth,node_count,file_count,criticality,path_json) VALUES
 (42,'Entry',1,2,2,2,0.25,'[1,3]');
`

// vsTBlankPathSeed is a flow and a snapshot the release persisted with their
// metrics but with nothing in the path columns.
const vsTBlankPathSeed = `
UPDATE flows SET path_json='   ' WHERE id=41;
UPDATE flow_snapshots SET critical_path='' WHERE flow_id=41;
`

// vsTTextCommunityIDSeed stores a community assignment that is not an id.
const vsTTextCommunityIDSeed = `
UPDATE nodes SET community_id='cluster-7' WHERE id=1;
`

// vsTOldSchemaSeed is a graph written by an off-release build.
const vsTOldSchemaSeed = `
UPDATE metadata SET value='8' WHERE key='schema_version';
`

// vsTNoDerivedViewsSeed is a release build that materialized none of the
// optional derived views at all.
const vsTNoDerivedViewsSeed = `
DROP TABLE flows;
DROP TABLE flow_snapshots;
DROP TABLE community_summaries;
DROP TABLE risk_index;
DROP TABLE nodes_fts;
`

// vsTNullFTSIndexSeed holds one index entry with no qualified name at all.
const vsTNullFTSIndexSeed = `
DROP TABLE nodes_fts;
CREATE VIRTUAL TABLE nodes_fts USING fts5(name, qualified_name, file_path, signature, tokenize='porter unicode61');
INSERT INTO nodes_fts (name,qualified_name,file_path,signature) VALUES
 ('Ghost',NULL,'/abs/repo/pkg/a.go','def Ghost()');
`

// vsTStrayFTSIndexSeed holds one index entry naming a file outside the root.
const vsTStrayFTSIndexSeed = `
DROP TABLE nodes_fts;
CREATE VIRTUAL TABLE nodes_fts USING fts5(name, qualified_name, file_path, signature, tokenize='porter unicode61');
INSERT INTO nodes_fts (name,qualified_name,file_path,signature) VALUES
 ('Stray','/elsewhere/x.go::Stray','/elsewhere/x.go','def Stray()');
`

// vsTDriftedFTSIndexSeed keeps the release's own external-content index but
// adds an entry whose content row does not exist — the drift a half-finished
// rebuild leaves behind, which only a real MATCH discovers.
const vsTDriftedFTSIndexSeed = `
INSERT INTO nodes_fts (rowid,name,qualified_name,file_path,signature) VALUES
 (999,'Ghost','/abs/repo/pkg/g.go::Ghost','/abs/repo/pkg/g.go','def Ghost()');
`

// vsTIteratingFTSFailureSeed makes the index read fail PART WAY through: the
// first row reads cleanly and the second raises a SQLite error while stepping.
const vsTIteratingFTSFailureSeed = `
DROP TABLE nodes_fts;
CREATE VIEW nodes_fts AS
  SELECT 'Entry' AS name, '/abs/repo/pkg/a.go::Entry' AS qualified_name, 'pkg/a.go' AS file_path, 'def Entry()' AS signature
  UNION ALL
  SELECT 'Boom', abs(-9223372036854775808), 'pkg/a.go', 'def Boom()';
`

// vsTStore opens a seeded graph through the production reader and also hands
// back its path, so a test can change the file underneath the handle.
func vsTStore(t *testing.T, seed string) (*BridgeStore, string) {
	t.Helper()
	path := newGraphDB(t, seed)
	store, err := OpenBridgeStore(graphRoot, path, PinnedVersion, testRelease())
	if err != nil {
		t.Fatalf("open bridge store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store, path
}

// vsTMutatedStore opens the two-flow graph and then changes the SAME file
// through a second, writable connection. The capability probe is a snapshot
// taken at open time, so the readers must report what the graph holds NOW.
func vsTMutatedStore(t *testing.T, mutation string) *BridgeStore {
	t.Helper()
	store, path := vsTStore(t, twoFlowSeed)
	writer, err := sql.Open(sqliteDriver, path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open fixture writer: %v", err)
	}
	defer writer.Close()
	if _, err := writer.Exec(mutation); err != nil {
		t.Fatalf("mutate fixture: %v", err)
	}
	return store
}

// vsTViewsError requires reading a seeded graph to fail.
func vsTViewsError(t *testing.T, seed string) error {
	t.Helper()
	views, err := openGraph(t, seed).Views()
	if err == nil {
		t.Fatalf("a broken graph was read as %d usable symbol(s)", len(views.Symbols))
	}
	return err
}

// vsTWantErr requires err to name what the reader choked on.
func vsTWantErr(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want one naming %q", err, want)
	}
}

// vsTKeys projects the ordering key out of a row slice.
func vsTKeys[T any](rows []T, key func(T) string) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, key(row))
	}
	return out
}

// vsTMembershipRows projects the flattened memberships for comparison.
func vsTMembershipRows(views BridgeViews) []FlowMembershipRow {
	rows := views.FlowMemberships()
	out := make([]FlowMembershipRow, 0, len(rows))
	for _, m := range rows {
		out = append(out, FlowMembershipRow{m.FlowID, m.MemberID, m.Position})
	}
	return out
}

// A row the reader cannot decode is a BROKEN readback, not an empty view: the
// two demand opposite verdicts, so the failure has to name the row it choked
// on instead of leaving the view quietly short.
func TestVsTViewsRefuseUndecodableRows(t *testing.T) {
	cases := map[string]struct{ mutation, want string }{
		"node line is not a number": {
			"\nUPDATE nodes SET line_start='third' WHERE id=1;\n", "scan bridge node"},
		"edge line is not a number": {
			"\nUPDATE edges SET line='fourth' WHERE line=4;\n", "scan bridge edge"},
		"flow depth is not a number": {
			"\nUPDATE flows SET depth='deep' WHERE id=41;\n", "scan bridge flow"},
		"snapshot count is not a number": {
			"\nUPDATE flow_snapshots SET node_count='many' WHERE flow_id=41;\n", "scan bridge flow_snapshot"},
		"summary size is not a number": {
			"\nUPDATE community_summaries SET size='big' WHERE community_id=7;\n", "scan bridge community_summary"},
		"risk caller count is not a number": {
			"\nUPDATE risk_index SET caller_count='many' WHERE node_id=1;\n", "scan bridge risk_index"},
		"flow path is not a json id list": {
			"\nUPDATE flows SET path_json='[1,2' WHERE id=41;\n", "decode bridge flow 41 path_json"},
		"critical path is not json": {
			"\nUPDATE flow_snapshots SET critical_path='[\"Entry\"' WHERE flow_id=41;\n",
			"decode bridge flow_snapshot critical_path"},
		"key symbols are not json": {
			"\nUPDATE community_summaries SET key_symbols='Entry,Step' WHERE community_id=7;\n",
			"decode bridge community_summary key_symbols"},
		"an index entry has no qualified name": {
			vsTNullFTSIndexSeed, "scan bridge nodes_fts row"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			vsTWantErr(t, vsTViewsError(t, twoFlowSeed+tc.mutation), tc.want)
		})
	}
}

// Every id the gate compares lives in ONE space. A stored value that cannot be
// trimmed into it is refused wherever it appears, because rewriting it would
// invent an id and make a divergence look like agreement.
func TestVsTViewsRefuseIdsOutsideTheGraphRoot(t *testing.T) {
	cases := map[string]string{
		"node file path": "\nINSERT INTO nodes (id,kind,name,qualified_name,file_path,line_start,language,file_hash)" +
			" VALUES (4,'Function','Ok','/abs/repo/pkg/c.go::Ok','/elsewhere/c.go',1,'go','h4');\n",
		"edge source":            "\nUPDATE edges SET source_qualified='/elsewhere/x.go::Stray' WHERE line=4;\n",
		"edge target":            "\nUPDATE edges SET target_qualified='/elsewhere/x.go::Stray' WHERE line=4;\n",
		"edge file path":         "\nUPDATE edges SET file_path='/elsewhere/x.go' WHERE line=4;\n",
		"snapshot critical path": "\nUPDATE flow_snapshots SET critical_path='[\"/elsewhere/x.go::Stray\"]' WHERE flow_id=41;\n",
		"risk qualified name":    "\nUPDATE risk_index SET qualified_name='/elsewhere/x.go::Stray' WHERE node_id=1;\n",
		"index entry":            vsTStrayFTSIndexSeed,
	}
	for name, mutation := range cases {
		t.Run(name, func(t *testing.T) {
			if err := vsTViewsError(t, twoFlowSeed+mutation); !errors.Is(err, ErrOutsideRoot) {
				t.Fatalf("error = %v, want ErrOutsideRoot", err)
			}
		})
	}
}

// The capability probe is a snapshot taken at open time. If a table goes away
// afterwards, the reader must surface the failure and name the table — never
// downgrade it to "the release computed nothing for that view".
func TestVsTViewsSurfaceATableThatVanishedAfterTheProbe(t *testing.T) {
	cases := map[string]string{
		"nodes":                 "query bridge nodes",
		"edges":                 "query bridge edges",
		tableFlows:              "query bridge flows",
		tableFlowSnapshots:      "query bridge flow_snapshots",
		tableCommunitySummaries: "query bridge community_summaries",
		tableRiskIndex:          "query bridge risk_index",
		tableNodesFTS:           "query bridge nodes_fts",
	}
	for table, want := range cases {
		t.Run(table, func(t *testing.T) {
			_, err := vsTMutatedStore(t, "DROP TABLE "+table).Views()
			vsTWantErr(t, err, want)
		})
	}
}

// A read that dies PART WAY through is not a short answer: the rows already
// scanned would look like the whole view. The reader reports the iteration
// failure and names the table it was reading.
func TestVsTViewsSurfaceARowIterationFailure(t *testing.T) {
	vsTWantErr(t, vsTViewsError(t, twoFlowSeed+vsTIteratingFTSFailureSeed), "iterate bridge nodes_fts")
}

// A graph whose nodes are gone has nothing to compare. Reporting that as an
// empty-but-successful read is exactly the green run this gate exists to stop.
func TestVsTViewsReportAnEmptiedGraphAsUnavailable(t *testing.T) {
	_, err := vsTMutatedStore(t, "DELETE FROM nodes").Views()
	if !errors.Is(err, ErrBridgeUnavailable) {
		t.Fatalf("Views error = %v, want ErrBridgeUnavailable", err)
	}
}

// Summaries are re-keyed by reading the community assignment back off `nodes`.
// A readback that cannot be completed must fail loudly: a partial map would
// silently re-key a summary onto the wrong cluster.
func TestVsTCommunitySummariesRefuseABrokenAssignmentReadback(t *testing.T) {
	t.Run("the nodes table vanished", func(t *testing.T) {
		_, err := vsTMutatedStore(t, "DROP TABLE nodes").readCommunitySummaries(nil, nil)
		vsTWantErr(t, err, "query bridge community assignment")
	})
	t.Run("an assignment is not a community id", func(t *testing.T) {
		store := openGraph(t, twoFlowSeed+vsTTextCommunityIDSeed)
		_, err := store.readCommunitySummaries(nil, nil)
		vsTWantErr(t, err, "scan bridge community assignment")
		_, viewsErr := store.Views()
		vsTWantErr(t, viewsErr, "scan bridge node")
	})
}

// Comparison is row-by-row, so every derived view is rendered in a fixed order
// and in the repo-relative id space — no matter what order the release stored
// its rows in, and no matter which absolute root it was built under.
func TestVsTViewsRenderRowsCanonically(t *testing.T) {
	views := fixtureViews(t, vsTSecondFlowSeed)
	entry, alpha, zeta := repoFile("pkg/a.go", "Entry"), repoFile("pkg/c.go", "Alpha"), repoFile("pkg/c.go", "Zeta")
	ordered := []string{entry, alpha}

	t.Run("flows and snapshots are ordered by entry point", func(t *testing.T) {
		flows := vsTKeys(views.Flows, func(f BridgeFlow) string { return f.EntryPoint })
		if !reflect.DeepEqual(flows, ordered) {
			t.Fatalf("flow order = %v, want %v", flows, ordered)
		}
		snapshots := vsTKeys(views.FlowSnapshots, func(s BridgeFlowSnapshot) string { return s.EntryPoint })
		if !reflect.DeepEqual(snapshots, ordered) {
			t.Fatalf("snapshot order = %v, want %v", snapshots, ordered)
		}
	})

	t.Run("summaries are ordered by cluster key", func(t *testing.T) {
		clusters := vsTKeys(views.CommunitySummaries, func(c BridgeCommunitySummary) string { return c.Cluster })
		if !reflect.DeepEqual(clusters, ordered) {
			t.Fatalf("summary order = %v, want %v (community 3 is stored first)", clusters, ordered)
		}
	})

	t.Run("a cluster is keyed by its smallest member, not its first stored one", func(t *testing.T) {
		if views.Communities[zeta] != alpha || views.Communities[alpha] != alpha {
			t.Fatalf("cluster of {Zeta,Alpha} = %q/%q, want %q for both",
				views.Communities[zeta], views.Communities[alpha], alpha)
		}
	})

	t.Run("no rendered row keeps the absolute graph root", func(t *testing.T) {
		rendered := fmt.Sprintf("%+v", views)
		if !strings.Contains(rendered, "pkg/a.go::Entry") {
			t.Fatalf("the rendered views do not carry the rows under test: %s", rendered)
		}
		if strings.Contains(rendered, graphRoot) {
			t.Fatalf("a rendered row still carries %q: %s", graphRoot, rendered)
		}
	})
}

// The membership rows are the flow oracle's comparison input, so their order
// must be TOTAL: flow, then position, then member.
func TestVsTFlowMembershipsAreTotallyOrdered(t *testing.T) {
	entry, step := repoFile("pkg/a.go", "Entry"), repoFile("pkg/a.go", "Step")
	widget, alpha, zeta := repoFile("pkg/b.go", "Widget"), repoFile("pkg/c.go", "Alpha"), repoFile("pkg/c.go", "Zeta")

	t.Run("rows group by flow", func(t *testing.T) {
		want := []FlowMembershipRow{{entry, entry, 0}, {entry, step, 1}, {alpha, alpha, 0}, {alpha, zeta, 1}}
		if got := vsTMembershipRows(fixtureViews(t, vsTSecondFlowSeed)); !reflect.DeepEqual(got, want) {
			t.Fatalf("memberships = %+v, want %+v", got, want)
		}
	})

	t.Run("two flows from one entry point tie-break by member", func(t *testing.T) {
		want := []FlowMembershipRow{{entry, entry, 0}, {entry, entry, 0}, {entry, step, 1}, {entry, widget, 1}}
		if got := vsTMembershipRows(fixtureViews(t, vsTSharedEntrySeed)); !reflect.DeepEqual(got, want) {
			t.Fatalf("memberships = %+v, want %+v", got, want)
		}
	})
}

// A flow the release persisted with no path, and a snapshot with no critical
// path, are legitimate rows with nothing to decode: they keep their metrics and
// contribute no members.
func TestVsTViewsAcceptRowsWithNoPersistedPath(t *testing.T) {
	views := fixtureViews(t, twoFlowSeed+vsTBlankPathSeed)
	if len(views.Flows) != 1 || views.Flows[0].Path != nil || views.Flows[0].NodeCount != 2 {
		t.Fatalf("flows = %+v, want one pathless flow that kept its metrics", views.Flows)
	}
	if len(views.FlowSnapshots) != 1 || views.FlowSnapshots[0].CriticalPath != nil {
		t.Fatalf("snapshots = %+v, want one snapshot with no critical path", views.FlowSnapshots)
	}
	if rows := views.FlowMemberships(); len(rows) != 0 {
		t.Fatalf("a pathless flow produced memberships: %+v", rows)
	}
}

// A release build that materialized none of the derived views is still a
// readable graph: the base tables compare, and every derived surface is
// reported as unmaterialized rather than invented, read, or searched.
func TestVsTViewsSkipDerivedTablesTheReleaseNeverMaterialized(t *testing.T) {
	store := openGraph(t, twoFlowSeed+vsTNoDerivedViewsSeed)
	views, err := store.Views()
	if err != nil {
		t.Fatalf("read views: %v", err)
	}
	if len(views.Symbols) != 3 || len(views.Edges) != 3 {
		t.Fatalf("the base graph did not survive: %d symbols, %d edges", len(views.Symbols), len(views.Edges))
	}
	if views.Flows != nil || views.FlowSnapshots != nil || views.CommunitySummaries != nil ||
		views.FTSIndex != nil || len(views.RiskIndex) != 0 {
		t.Fatalf("an unmaterialized view produced rows: %+v", views)
	}
	if hits, err := store.SearchFTS("Entry"); err != nil || hits != nil {
		t.Fatalf("search without an index = %v, %v; want no hits and no error", hits, err)
	}
	for _, table := range []string{tableFlows, tableFlowSnapshots, tableCommunitySummaries,
		tableRiskIndex, tableNodesFTS} {
		if c, ok := views.Schema.Of(table); !ok || c.State != CapTableMissing {
			t.Fatalf("%s = %+v, want table_missing", table, c)
		}
	}
}

// The gate reproduces the release's own search RESULTS, so every answer the
// index can give — a hit, a miss, and a failure — has to come back as itself.
func TestVsTSearchFTSReportsHitsMissesAndFailures(t *testing.T) {
	t.Run("a term the index does not hold is a miss, not a failure", func(t *testing.T) {
		vsTWantNoHits(t, openGraph(t, twoFlowSeed), "Nonexistent")
	})

	t.Run("a term made of FTS5 syntax is searched literally", func(t *testing.T) {
		store := openGraph(t, twoFlowSeed)
		for _, term := range []string{"", "-", "*", "NEAR", "AND"} {
			vsTWantNoHits(t, store, term)
		}
		// '^' opens FTS5's first-token operator. Quoted, it is punctuation
		// around the identifier, so the search still answers for Entry
		// instead of being parsed as a query.
		hits, err := store.SearchFTS("^Entry")
		if err != nil || !reflect.DeepEqual(hits, []string{"pkg/a.go::Entry"}) {
			t.Fatalf(`SearchFTS("^Entry") = %v, %v; want the Entry hit`, hits, err)
		}
	})

	t.Run("an index that drifted from its content is a failure", func(t *testing.T) {
		_, err := openGraph(t, twoFlowSeed+vsTDriftedFTSIndexSeed).SearchFTS("Ghost")
		vsTWantErr(t, err, `search bridge nodes_fts for "Ghost"`)
	})

	t.Run("a hit with no qualified name is refused", func(t *testing.T) {
		_, err := openGraph(t, twoFlowSeed+vsTNullFTSIndexSeed).SearchFTS("Ghost")
		vsTWantErr(t, err, "scan bridge nodes_fts hit")
	})

	t.Run("a hit outside the graph root is refused", func(t *testing.T) {
		_, err := openGraph(t, twoFlowSeed+vsTStrayFTSIndexSeed).SearchFTS("Stray")
		if !errors.Is(err, ErrOutsideRoot) {
			t.Fatalf("SearchFTS error = %v, want ErrOutsideRoot", err)
		}
	})
}

// vsTWantNoHits requires a search to answer with no hits and no failure.
func vsTWantNoHits(t *testing.T, store *BridgeStore, term string) {
	t.Helper()
	hits, err := store.SearchFTS(term)
	if err != nil {
		t.Fatalf("SearchFTS(%q): %v", term, err)
	}
	if len(hits) != 0 {
		t.Fatalf("SearchFTS(%q) = %v, want no hits", term, hits)
	}
}

// A graph at the wrong schema version is refused at open time and no handle is
// handed back: nothing may read a store the probe rejected.
func TestVsTOpenBridgeStoreRefusesAnIncompatibleGraph(t *testing.T) {
	store, err := OpenBridgeStore(graphRoot, newGraphDB(t, twoFlowSeed+vsTOldSchemaSeed), PinnedVersion, testRelease())
	if !errors.Is(err, ErrSchemaIncompatible) {
		t.Fatalf("OpenBridgeStore error = %v, want ErrSchemaIncompatible", err)
	}
	if store != nil {
		t.Fatal("a rejected graph still handed back a readable store")
	}
}

// The handle the gate keeps open answers live questions about the SAME graph,
// in the SAME id space, as the persisted views it already read.
func TestVsTStoreExposesTheGraphItOpened(t *testing.T) {
	store := openGraph(t, twoFlowSeed)

	t.Run("the normalizer keys live answers like the persisted views", func(t *testing.T) {
		if root := store.Normalizer().Root(); root != graphRoot {
			t.Fatalf("normalizer root = %q, want %q", root, graphRoot)
		}
		qualified, err := store.Normalizer().Qualified(graphRoot + "/pkg/a.go::Entry")
		if err != nil || qualified != "pkg/a.go::Entry" {
			t.Fatalf("normalized live answer = %q, %v; want pkg/a.go::Entry", qualified, err)
		}
	})

	t.Run("the flow state samples the graph's own id space", func(t *testing.T) {
		state, err := store.FlowState()
		if err != nil {
			t.Fatalf("FlowState: %v", err)
		}
		want := FlowState{flowIDs: []int64{41}, snapshotFlowIDs: []int64{41}}
		if !reflect.DeepEqual(state, want) {
			t.Fatalf("flow state = %+v, want %+v", state, want)
		}
	})
}
