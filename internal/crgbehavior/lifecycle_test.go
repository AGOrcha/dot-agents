package crgbehavior

import (
	"database/sql"
	"strings"
	"testing"
)

// v2.3.8's standalone postprocess rebuilds flows but does NOT recompute the
// summary tables, so `flows.id` (an AUTOINCREMENT rowid) advances while
// flow_snapshots keeps pointing at the old ids. Anything reading snapshots
// after a postprocess is reading stale data — this is the observable the gate
// asserts, per commit.
func TestObserveLifecycleRecognizesThePinnedStalenessContract(t *testing.T) {
	before := FlowState{flowIDs: []int64{41, 42}, snapshotFlowIDs: []int64{41, 42}}
	after := FlowState{flowIDs: []int64{43, 44}, snapshotFlowIDs: []int64{41, 42},
		postprocessedAt: "2026-01-01T00:00:00"}
	got := ObserveLifecycle(before, after)
	if !got.Observed || !got.SnapshotsConsistentAfterBuild || !got.PostprocessRebuiltFlows ||
		!got.SnapshotsStaleAfterPostprocess || !got.PostprocessStampedMetadata {
		t.Fatalf("observation = %+v, want every pinned expectation to hold", got)
	}
	if s := got.Surface(); s.Status != StatusAgree {
		t.Fatalf("lifecycle surface = %s %v, want agreement", s.Status, s.Detail)
	}
}

// A release that started refreshing the summaries in postprocess would be a
// real behavior change, and the gate must report it rather than absorb it.
func TestObserveLifecycleDetectsRefreshedSummaries(t *testing.T) {
	before := FlowState{flowIDs: []int64{41}, snapshotFlowIDs: []int64{41}}
	after := FlowState{flowIDs: []int64{42}, snapshotFlowIDs: []int64{42}, postprocessedAt: "now"}
	got := ObserveLifecycle(before, after)
	if got.SnapshotsStaleAfterPostprocess {
		t.Fatal("a refreshed flow_snapshots table was reported as stale")
	}
	s := got.Surface()
	if s.Status != StatusDiverge {
		t.Fatalf("lifecycle surface = %s, want a divergence", s.Status)
	}
	if !strings.Contains(strings.Join(s.Detail, "\n"), "does not recompute summaries") {
		t.Fatalf("the divergence must name the broken expectation: %v", s.Detail)
	}
}

// A build that left snapshots dangling was already inconsistent; the gate must
// not credit the pinned contract for it.
func TestObserveLifecycleDetectsAnInconsistentBuild(t *testing.T) {
	before := FlowState{flowIDs: []int64{41}, snapshotFlowIDs: []int64{7}}
	after := FlowState{flowIDs: []int64{42}, snapshotFlowIDs: []int64{7}, postprocessedAt: "now"}
	if got := ObserveLifecycle(before, after); got.SnapshotsConsistentAfterBuild {
		t.Fatal("a build that left flow_snapshots dangling was reported as consistent")
	}
}

// A commit whose graph has no flows cannot exercise the contract at all. That
// is NOT-EXERCISED with a reason, never a pass.
func TestObserveLifecycleReportsAnUnobservableCommit(t *testing.T) {
	for name, before := range map[string]FlowState{
		"no flows":     {},
		"no snapshots": {flowIDs: []int64{41}},
	} {
		t.Run(name, func(t *testing.T) {
			got := ObserveLifecycle(before, FlowState{})
			if got.Observed {
				t.Fatalf("%s was reported as observed", name)
			}
			s := got.Surface()
			if s.Status != StatusNotExercised || s.Reason == "" {
				t.Fatalf("surface = %s %q, want not_exercised with a reason", s.Status, s.Reason)
			}
		})
	}
}

// The probe samples the release's own tables, so a graph with no postprocess
// stamp reports an empty one rather than failing.
func TestReadFlowStateSamplesTheReleaseTables(t *testing.T) {
	db, err := sql.Open(sqliteDriver, newGraphDB(t, twoFlowSeed))
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	state, err := ReadFlowState(db)
	if err != nil {
		t.Fatalf("ReadFlowState: %v", err)
	}
	if len(state.flowIDs) != 1 || state.flowIDs[0] != 41 {
		t.Fatalf("flow ids = %v, want [41]", state.flowIDs)
	}
	if len(state.snapshotFlowIDs) != 1 || state.snapshotFlowIDs[0] != 41 {
		t.Fatalf("snapshot flow ids = %v, want [41]", state.snapshotFlowIDs)
	}
	if state.postprocessedAt != "" {
		t.Fatalf("postprocess stamp = %q, want empty for a graph that was never postprocessed", state.postprocessedAt)
	}
}

// nnTPostprocessedSeed is a release graph that WAS postprocessed: two flows,
// two snapshot rows and the release's own postprocess stamp.
const nnTPostprocessedSeed = `
INSERT INTO flows (id,name,entry_point_id,depth,node_count,file_count,path_json) VALUES
 (41,'Entry',1,1,2,1,'[1,2]'),
 (42,'Other',3,1,1,1,'[3]');
INSERT INTO flow_snapshots (flow_id,name,entry_point) VALUES
 (41,'Entry','pkg/a.go::Entry'),
 (42,'Other','pkg/b.go::Widget');
INSERT INTO metadata (key,value) VALUES ('last_postprocessed_at','2026-02-02T03:04:05');
`

// nnTRawGraph opens a seeded fixture graph read-write so a test can damage one
// release table and drive a REAL query failure through the production reader.
func nnTRawGraph(t *testing.T, seed string, damage ...string) *sql.DB {
	t.Helper()
	db, err := sql.Open(sqliteDriver, newGraphDB(t, seed))
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	for _, stmt := range damage {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("apply %q: %v", stmt, err)
		}
	}
	return db
}

// nnTAssertSampleFailed checks an unreadable graph is reported, attributed to
// the table that failed, and yields NO partial sample.
func nnTAssertSampleFailed(t *testing.T, st FlowState, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("ReadFlowState succeeded against an unreadable graph: %+v", st)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want it to name %q", err, want)
	}
	if st.flowIDs != nil || st.snapshotFlowIDs != nil || st.postprocessedAt != "" {
		t.Fatalf("a failed sample returned partial state: %+v", st)
	}
}

// A release graph the probe cannot read is NOT an empty graph: an empty sample
// would be folded into a not-exercised verdict, quietly excusing a broken
// store. Each of the three tables ReadFlowState touches must fail loudly and
// name itself.
func TestNnTReadFlowStateFailsOnAnUnreadableTable(t *testing.T) {
	cases := []struct{ name, damage, want string }{
		{"flows unreadable", "DROP TABLE flows", "bridge " + tableFlows + " ids"},
		{"flow_snapshots unreadable", "DROP TABLE flow_snapshots", "bridge " + tableFlowSnapshots + " ids"},
		{"metadata unreadable", "DROP TABLE metadata", metadataLastPostprocessed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st, err := ReadFlowState(nnTRawGraph(t, twoFlowSeed, c.damage))
			nnTAssertSampleFailed(t, st, err, c.want)
		})
	}
}

// The lifecycle probe samples the same handle twice (before and after the
// release's postprocess). A handle closed in between must fail rather than
// report an empty id space that ObserveLifecycle would read as "no flows".
func TestNnTReadFlowStateFailsOnAClosedStore(t *testing.T) {
	db := nnTRawGraph(t, twoFlowSeed)
	if err := db.Close(); err != nil {
		t.Fatalf("close fixture: %v", err)
	}
	st, err := ReadFlowState(db)
	nnTAssertSampleFailed(t, st, err, "bridge "+tableFlows+" ids")
}

// A graph the release already postprocessed reports its stamp verbatim — that
// stamp is the only evidence the postprocess ran at all.
func TestNnTReadFlowStateReadsThePostprocessStamp(t *testing.T) {
	st, err := ReadFlowState(nnTRawGraph(t, nnTPostprocessedSeed))
	if err != nil {
		t.Fatalf("ReadFlowState: %v", err)
	}
	if !sameIDs(st.flowIDs, []int64{41, 42}) || !sameIDs(st.snapshotFlowIDs, []int64{41, 42}) {
		t.Fatalf("sample = %v / %v, want both id spaces fully read", st.flowIDs, st.snapshotFlowIDs)
	}
	if want := "2026-02-02T03:04:05"; st.postprocessedAt != want {
		t.Fatalf("postprocess stamp = %q, want %q", st.postprocessedAt, want)
	}
}

// The samples are compared element-wise by sameIDs, so readIDs owes its caller
// a SORTED column regardless of the order the release's store hands rows back.
func TestNnTReadIDsSortsTheColumn(t *testing.T) {
	db := nnTRawGraph(t, nnTPostprocessedSeed)
	got, err := readIDs(db, `SELECT id FROM flows ORDER BY id DESC`, tableFlows)
	if err != nil {
		t.Fatalf("readIDs: %v", err)
	}
	if !sameIDs(got, []int64{41, 42}) {
		t.Fatalf("readIDs = %v, want it sorted ascending", got)
	}
}

// A NULL id must be a hard failure. Skipping the row instead would shrink the
// sampled id space, and a shrunken "after" sample makes a rebuilt flow table
// look untouched. The shared fixture seeds nodes.community_id with a real
// NULL, so the failure is driven by stored data, not by a stub.
func TestNnTReadIDsRejectsANullID(t *testing.T) {
	db := nnTRawGraph(t, twoFlowSeed)
	got, err := readIDs(db, `SELECT community_id FROM nodes`, "nodes")
	if err == nil {
		t.Fatalf("readIDs accepted a NULL id and returned %v", got)
	}
	if !strings.Contains(err.Error(), "scan bridge nodes id") {
		t.Fatalf("error = %q, want a scan failure naming the table", err)
	}
	if got != nil {
		t.Fatalf("readIDs returned %v alongside an error, want nil", got)
	}
}

// nnTAssertBrokenExpectations checks the observation matches exactly and that
// the surface names every pinned expectation that failed, in order.
func nnTAssertBrokenExpectations(t *testing.T, got, want LifecycleObservation, details []string, metric string) {
	t.Helper()
	if got != want {
		t.Fatalf("observation = %+v, want %+v", got, want)
	}
	s := got.Surface()
	if s.Status != StatusDiverge {
		t.Fatalf("surface = %s, want a divergence", s.Status)
	}
	if s.Metric != metric {
		t.Fatalf("metric = %q, want %q", s.Metric, metric)
	}
	if len(s.Detail) != len(details) {
		t.Fatalf("detail = %v, want %d item(s)", s.Detail, len(details))
	}
	for i, want := range details {
		if !strings.Contains(s.Detail[i], want) {
			t.Fatalf("detail[%d] = %q, want it to name %q", i, s.Detail[i], want)
		}
	}
}

// Each of the four pinned expectations is independently falsifiable, and a
// release that broke one must be reported as a divergence naming exactly that
// expectation — never absorbed into the others.
func TestNnTObserveLifecycleReportsEachBrokenExpectation(t *testing.T) {
	cases := []struct {
		name    string
		before  FlowState
		after   FlowState
		want    LifecycleObservation
		details []string
		metric  string
	}{
		{
			name:   "postprocess reused the flow id space",
			before: FlowState{flowIDs: []int64{41}, snapshotFlowIDs: []int64{41}},
			after:  FlowState{flowIDs: []int64{41}, snapshotFlowIDs: []int64{41}, postprocessedAt: "now"},
			want: LifecycleObservation{
				Observed:                      true,
				SnapshotsConsistentAfterBuild: true,
				PostprocessStampedMetadata:    true,
			},
			details: []string{"rebuilds the flows rows", "does not recompute summaries"},
			metric:  "2 of 4 pinned lifecycle expectation(s) held",
		},
		{
			name:   "postprocess recomputed part of the snapshots",
			before: FlowState{flowIDs: []int64{41, 42}, snapshotFlowIDs: []int64{41, 42}},
			after:  FlowState{flowIDs: []int64{43, 44}, snapshotFlowIDs: []int64{43}, postprocessedAt: "now"},
			want: LifecycleObservation{
				Observed:                      true,
				SnapshotsConsistentAfterBuild: true,
				PostprocessRebuiltFlows:       true,
				PostprocessStampedMetadata:    true,
			},
			details: []string{"does not recompute summaries"},
			metric:  "3 of 4 pinned lifecycle expectation(s) held",
		},
		{
			name:   "postprocess left no stamp",
			before: FlowState{flowIDs: []int64{41}, snapshotFlowIDs: []int64{41}},
			after:  FlowState{flowIDs: []int64{42}, snapshotFlowIDs: []int64{41}},
			want: LifecycleObservation{
				Observed:                       true,
				SnapshotsConsistentAfterBuild:  true,
				PostprocessRebuiltFlows:        true,
				SnapshotsStaleAfterPostprocess: true,
			},
			details: []string{metadataLastPostprocessed},
			metric:  "3 of 4 pinned lifecycle expectation(s) held",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ObserveLifecycle(c.before, c.after)
			nnTAssertBrokenExpectations(t, got, c.want, c.details, c.metric)
		})
	}
}

// noneIn decides whether the rebuilt flow table shares ANY id with the old one,
// so a single surviving id must flip it — a "mostly rebuilt" table is not a
// rebuilt table.
func TestNnTNoneInDetectsASingleSharedID(t *testing.T) {
	cases := []struct {
		name string
		ids  []int64
		set  []int64
		want bool
	}{
		{"no ids", nil, []int64{1, 2}, true},
		{"empty set", []int64{1, 2}, nil, true},
		{"both empty", nil, nil, true},
		{"identical", []int64{1, 2}, []int64{1, 2}, false},
		{"one shared id", []int64{2, 3}, []int64{1, 2}, false},
		{"disjoint", []int64{3, 4}, []int64{1, 2}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := noneIn(c.ids, int64Set(c.set)); got != c.want {
				t.Fatalf("noneIn(%v, %v) = %v, want %v", c.ids, c.set, got, c.want)
			}
		})
	}
}

// sameIDs decides whether the snapshot table was left untouched, so it must
// compare position by position: same length with one different id, or the same
// ids with a row added or dropped, are both changes.
func TestNnTSameIDsComparesPositionByPosition(t *testing.T) {
	cases := []struct {
		name string
		a    []int64
		b    []int64
		want bool
	}{
		{"both empty", nil, nil, true},
		{"identical", []int64{1, 2}, []int64{1, 2}, true},
		{"one differing id", []int64{1, 2}, []int64{1, 3}, false},
		{"disjoint", []int64{1, 2}, []int64{3, 4}, false},
		{"a row was dropped", []int64{1, 2}, []int64{1}, false},
		{"a row was added", nil, []int64{1}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := sameIDs(c.a, c.b); got != c.want {
				t.Fatalf("sameIDs(%v, %v) = %v, want %v", c.a, c.b, got, c.want)
			}
		})
	}
}
