package graphstore_test

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	tcwait "github.com/testcontainers/testcontainers-go/wait"

	"github.com/NikashPrakash/dot-agents/internal/graphstore"
)

// One Postgres container is shared by every test in this package: starting a
// fresh container per test would dominate the test budget (≈3–4 s of pull +
// boot per spin-up).  The existing postgres tests are designed for a shared
// schema — each test seeds rows under a unique prefix or unique qualified
// name, so they do not collide.
var (
	pgOnce       sync.Once
	pgSharedDSN  string
	pgSkipReason string
)

// lazyPostgresDSN returns the DSN of a Postgres container started on first
// use.  If Docker is unavailable (no daemon, no socket, etc.) the entire
// test calling this helper is skipped via t.Skipf — the package-level
// pgSkipReason memoises the failure so we only burn one attempt per process.
func lazyPostgresDSN(t *testing.T) string {
	t.Helper()
	pgOnce.Do(startSharedPostgres)
	if pgSkipReason != "" {
		t.Skipf("postgres testcontainer unavailable: %s", pgSkipReason)
	}
	return pgSharedDSN
}

// startSharedPostgres boots a single Postgres container for the lifetime of
// the test binary.  Termination is registered via TestMain in sqlite_test.go
// (best-effort: Docker will reap dangling testcontainers via Ryuk on exit).
func startSharedPostgres() {
	// Honour an external Postgres if the operator already set TEST_PG_URL —
	// this lets CI optionally use a service container instead of spinning
	// one via testcontainers.
	if dsn := os.Getenv("TEST_PG_URL"); dsn != "" {
		pgSharedDSN = dsn
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	container, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("graphstore"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			tcwait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		pgSkipReason = err.Error()
		return
	}

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = container.Terminate(context.Background())
		pgSkipReason = err.Error()
		return
	}

	// Reap on process exit — testcontainers-go's Ryuk sidecar will also
	// handle this if Terminate is never called, but explicit cleanup keeps
	// the local docker context tidy.  No t.Cleanup is possible from
	// startSharedPostgres (no *testing.T).
	pgRegisterTerminate(container)
	pgSharedDSN = dsn
}

var (
	pgTerminateMu sync.Mutex
	pgTerminate   []func()
)

func pgRegisterTerminate(c *postgres.PostgresContainer) {
	pgTerminateMu.Lock()
	defer pgTerminateMu.Unlock()
	pgTerminate = append(pgTerminate, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = c.Terminate(ctx)
	})
}

// pgTerminateAll runs every registered terminate hook. Invoked from
// TestMain (sqlite_test.go) before os.Exit.
func pgTerminateAll() {
	pgTerminateMu.Lock()
	hooks := pgTerminate
	pgTerminate = nil
	pgTerminateMu.Unlock()
	for _, h := range hooks {
		h()
	}
}

// openPGContainerStore opens a *PostgresStore against the shared container.
// Returns nil + skipped test if Docker is unavailable.
func openPGContainerStore(t *testing.T) *graphstore.PostgresStore {
	t.Helper()
	dsn := lazyPostgresDSN(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := graphstore.OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatalf("OpenPostgres against shared container: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// openPGContainerStoreInterface adapts openPGContainerStore to
// storetest.OpenStore.
//
// Kept package-local so it can replace the existing TEST_PG_URL-gated
// adapter in postgres_test.go without exporting any symbol.
func openPGContainerStoreInterface(t *testing.T) graphstore.Store {
	return openPGContainerStore(t)
}

// ---------------------------------------------------------------------------
// Additional coverage — these exercise the Postgres-only branches that the
// shared storetest harness and the original postgres_test.go did not touch.
// ---------------------------------------------------------------------------

// TestPG_Commit_NoOp exercises Commit (always returns nil for Postgres).
func TestPG_Commit_NoOp(t *testing.T) {
	s := openPGContainerStore(t)
	if err := s.Commit(); err != nil {
		t.Errorf("Commit should be no-op, got %v", err)
	}
}

// TestPG_GetAllFiles exercises GetAllFiles after seeding File-kind nodes.
func TestPG_GetAllFiles(t *testing.T) {
	s := openPGContainerStore(t)

	for _, path := range []string{"pg_files_a.go", "pg_files_b.go", "pg_files_c.go"} {
		if _, err := s.UpsertNode(graphstore.NodeInfo{
			Kind:     graphstore.NodeKindFile,
			Name:     path,
			FilePath: path,
			Language: "go",
		}, ""); err != nil {
			t.Fatalf("UpsertNode %s: %v", path, err)
		}
	}

	files, err := s.GetAllFiles()
	if err != nil {
		t.Fatalf("GetAllFiles: %v", err)
	}

	// At least our three seeded paths must be present.
	want := map[string]bool{"pg_files_a.go": false, "pg_files_b.go": false, "pg_files_c.go": false}
	for _, f := range files {
		if _, ok := want[f]; ok {
			want[f] = true
		}
	}
	for f, seen := range want {
		if !seen {
			t.Errorf("GetAllFiles missing %s", f)
		}
	}
}

// TestPG_GetEdgesAmong_Filtered exercises the array-membership query plus
// the in-memory filter that drops edges whose target isn't in the supplied
// set.
func TestPG_GetEdgesAmong_Filtered(t *testing.T) {
	s := openPGContainerStore(t)

	// Seed: A→B (both in set), A→outside (target dropped), C→B (in set).
	_, _ = s.UpsertEdge(graphstore.EdgeInfo{
		Kind: graphstore.EdgeKindCalls, Source: "pg_among::A", Target: "pg_among::B",
		FilePath: "pg_among.go", Line: 1,
	})
	_, _ = s.UpsertEdge(graphstore.EdgeInfo{
		Kind: graphstore.EdgeKindCalls, Source: "pg_among::A", Target: "pg_among::outside",
		FilePath: "pg_among.go", Line: 2,
	})
	_, _ = s.UpsertEdge(graphstore.EdgeInfo{
		Kind: graphstore.EdgeKindCalls, Source: "pg_among::C", Target: "pg_among::B",
		FilePath: "pg_among.go", Line: 3,
	})

	edges, err := s.GetEdgesAmong([]string{"pg_among::A", "pg_among::B", "pg_among::C"})
	if err != nil {
		t.Fatalf("GetEdgesAmong: %v", err)
	}

	gotPairs := map[string]bool{}
	for _, e := range edges {
		gotPairs[e.SourceQualified+"->"+e.TargetQualified] = true
	}
	if !gotPairs["pg_among::A->pg_among::B"] {
		t.Error("missing A->B in GetEdgesAmong")
	}
	if !gotPairs["pg_among::C->pg_among::B"] {
		t.Error("missing C->B in GetEdgesAmong")
	}
	if gotPairs["pg_among::A->pg_among::outside"] {
		t.Error("A->outside should have been filtered out")
	}
}

// TestPG_GetStats_KindsAndLanguages exercises the kinds/languages
// aggregation branches in scanStatsCounts, collectKindCounts, and
// collectLanguages.
func TestPG_GetStats_KindsAndLanguages(t *testing.T) {
	s := openPGContainerStore(t)

	// Seed multiple kinds + multiple languages so the aggregation has rows.
	seed := []graphstore.NodeInfo{
		{Kind: graphstore.NodeKindFunction, Name: "pgStatsFnGo", FilePath: "pg_stats_go.go", Language: "go"},
		{Kind: graphstore.NodeKindClass, Name: "PGStatsClassPy", FilePath: "pg_stats_py.py", Language: "python"},
		{Kind: graphstore.NodeKindFile, Name: "pg_stats_go.go", FilePath: "pg_stats_go.go", Language: "go"},
		{Kind: graphstore.NodeKindFile, Name: "pg_stats_py.py", FilePath: "pg_stats_py.py", Language: "python"},
	}
	for _, n := range seed {
		if _, err := s.UpsertNode(n, ""); err != nil {
			t.Fatalf("UpsertNode %s: %v", n.Name, err)
		}
	}
	_ = s.SetMetadata("last_updated", "2026-05-12T00:00:00Z")

	stats, err := s.GetStats()
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.TotalNodes < 4 {
		t.Errorf("want at least 4 nodes, got %d", stats.TotalNodes)
	}
	if stats.NodesByKind["Function"] < 1 {
		t.Errorf("expected Function in NodesByKind, got %v", stats.NodesByKind)
	}
	hasGo, hasPy := false, false
	for _, l := range stats.Languages {
		if l == "go" {
			hasGo = true
		}
		if l == "python" {
			hasPy = true
		}
	}
	if !hasGo || !hasPy {
		t.Errorf("expected go+python in languages, got %v", stats.Languages)
	}
	if stats.LastUpdated != "2026-05-12T00:00:00Z" {
		t.Errorf("expected LastUpdated metadata to be returned, got %q", stats.LastUpdated)
	}
}

// TestPG_GetImpactRadius exercises the BFS path: seed a small graph, then
// query impact starting from a changed file and assert affected nodes.
// seedPGImpactGraph builds a tiny call graph (seed + external caller + external
// callee) used by TestPG_GetImpactRadius and returns the three file names.
func seedPGImpactGraph(t *testing.T, s *graphstore.PostgresStore) (file, callerFile, calleeFile string) {
	t.Helper()
	file = "pg_impact_unique.go"
	callerFile = "pg_impact_caller.go"
	calleeFile = "pg_impact_callee.go"

	seedNode := graphstore.NodeInfo{
		Kind: graphstore.NodeKindFunction, Name: "pgImpactSeed", FilePath: file, Language: "go",
	}
	if err := s.StoreFileNodesEdges(file,
		[]graphstore.NodeInfo{seedNode},
		[]graphstore.EdgeInfo{
			// Seed calls external callee → forward BFS picks callee up.
			{
				Kind: graphstore.EdgeKindCalls, Source: file + "::pgImpactSeed",
				Target: calleeFile + "::pgImpactCallee", FilePath: file, Line: 1,
			},
		}, "pg_impact_hash"); err != nil {
		t.Fatalf("StoreFileNodesEdges: %v", err)
	}
	// External caller in a separate file, with reverse edge → seed.
	if _, err := s.UpsertNode(graphstore.NodeInfo{
		Kind: graphstore.NodeKindFunction, Name: "pgImpactCaller",
		FilePath: callerFile, Language: "go",
	}, ""); err != nil {
		t.Fatalf("UpsertNode caller: %v", err)
	}
	if _, err := s.UpsertEdge(graphstore.EdgeInfo{
		Kind: graphstore.EdgeKindCalls, Source: callerFile + "::pgImpactCaller",
		Target: file + "::pgImpactSeed", FilePath: callerFile, Line: 1,
	}); err != nil {
		t.Fatalf("UpsertEdge caller→seed: %v", err)
	}
	// External callee node so resolveImpactNodes returns it.
	if _, err := s.UpsertNode(graphstore.NodeInfo{
		Kind: graphstore.NodeKindFunction, Name: "pgImpactCallee",
		FilePath: calleeFile, Language: "go",
	}, ""); err != nil {
		t.Fatalf("UpsertNode callee: %v", err)
	}
	return file, callerFile, calleeFile
}

// hasQualified reports whether any node in nodes has the given qualified name.
func hasQualified(nodes []graphstore.GraphNode, qualified string) bool {
	for _, n := range nodes {
		if n.QualifiedName == qualified {
			return true
		}
	}
	return false
}

func TestPG_GetImpactRadius(t *testing.T) {
	s := openPGContainerStore(t)

	// Build a tiny call graph: a SEED in the changed file, plus an external
	// caller and an external callee. Both directions of BFS should add the
	// external nodes to ImpactedNodes; the seed itself goes to ChangedNodes.
	file, callerFile, calleeFile := seedPGImpactGraph(t, s)

	res, err := s.GetImpactRadius([]string{file}, 2, 100)
	if err != nil {
		t.Fatalf("GetImpactRadius: %v", err)
	}

	if !hasQualified(res.ImpactedNodes, calleeFile+"::pgImpactCallee") {
		t.Errorf("expected forward-reachable callee in impact set, got %+v", res.ImpactedNodes)
	}
	if !hasQualified(res.ImpactedNodes, callerFile+"::pgImpactCaller") {
		t.Errorf("expected reverse-reachable caller in impact set, got %+v", res.ImpactedNodes)
	}
	if !hasQualified(res.ChangedNodes, file+"::pgImpactSeed") {
		t.Errorf("expected seed in ChangedNodes, got %+v", res.ChangedNodes)
	}
}

// TestPG_GetImpactRadius_EmptySeeds covers the no-seed branch (changedFiles
// produces no node hits → BFS over an empty seed set → empty result).
func TestPG_GetImpactRadius_EmptySeeds(t *testing.T) {
	s := openPGContainerStore(t)

	res, err := s.GetImpactRadius([]string{"pg_no_such_file_xyz.go"}, 3, 50)
	if err != nil {
		t.Fatalf("GetImpactRadius empty: %v", err)
	}
	if len(res.ImpactedNodes) != 0 {
		t.Errorf("expected empty ImpactedNodes for unknown file, got %+v", res.ImpactedNodes)
	}
}

// TestPG_StoreFileNodesEdges_TxRollback_OnBadEdge exercises the
// rollback-on-error path in StoreFileNodesEdges by forcing an invalid extra
// payload via … no, encodeExtra never fails for valid maps.  The realistic
// failure mode is a bad node.Kind that violates a NOT NULL — but Postgres
// allows empty strings on TEXT.  Skip this branch; rollback semantics are
// exercised indirectly by every other StoreFileNodesEdges call.
//
// Kept as a documentation stub so future tests can pick it up if a hard
// error path becomes available.

// TestPG_ErrorPathsAfterClose exercises the "DB error" branch of every
// PostgresStore method by opening a fresh store, closing the pool, and
// asserting that subsequent calls return an error rather than panicking.
// This drives coverage on the uniform `if err != nil { return err }`
// statements that follow each pool.Exec / pool.Query.
//
// The store is created against the shared container but its own pool is
// closed immediately — so this does not impact other tests sharing the
// container.
func TestPG_ErrorPathsAfterClose(t *testing.T) {
	dsn := lazyPostgresDSN(t)
	ctx := context.Background()
	s, err := graphstore.OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatalf("OpenPostgres: %v", err)
	}
	_ = s.Close()

	// Every call below should return an error of some kind; we don't care
	// what kind, only that the error branch executes. Each case wraps one
	// PostgresStore method so the closed-pool error path is driven uniformly.
	cases := []struct {
		name string
		call func() error
	}{
		{"UpsertNode", func() error {
			_, err := s.UpsertNode(graphstore.NodeInfo{Kind: graphstore.NodeKindFunction, Name: "x", FilePath: "x.go"}, "")
			return err
		}},
		{"UpsertEdge", func() error {
			_, err := s.UpsertEdge(graphstore.EdgeInfo{Kind: graphstore.EdgeKindCalls, Source: "x", Target: "y", FilePath: "f"})
			return err
		}},
		{"RemoveFileData", func() error { return s.RemoveFileData("x.go") }},
		{"StoreFileNodesEdges", func() error { return s.StoreFileNodesEdges("x.go", nil, nil, "") }},
		{"GetNode", func() error { _, err := s.GetNode("x"); return err }},
		{"GetNodesByFile", func() error { _, err := s.GetNodesByFile("x.go"); return err }},
		{"GetEdgesBySource", func() error { _, err := s.GetEdgesBySource("x"); return err }},
		{"GetEdgesByTarget", func() error { _, err := s.GetEdgesByTarget("x"); return err }},
		{"GetEdgesAmong", func() error { _, err := s.GetEdgesAmong([]string{"x"}); return err }},
		{"GetAllFiles", func() error { _, err := s.GetAllFiles(); return err }},
		{"SearchNodes", func() error { _, err := s.SearchNodes("x", 10); return err }},
		{"GetStats", func() error { _, err := s.GetStats(); return err }},
		{"GetImpactRadius", func() error { _, err := s.GetImpactRadius([]string{"x.go"}, 1, 10); return err }},
		{"SetMetadata", func() error { return s.SetMetadata("k", "v") }},
		{"GetMetadata", func() error { _, err := s.GetMetadata("k"); return err }},
		{"UpsertKGNote", func() error {
			return s.UpsertKGNote(graphstore.KGNote{ID: "x", Title: "x", NoteType: "concept", Status: "active", FilePath: "x.md"})
		}},
		{"GetKGNote", func() error { _, err := s.GetKGNote("x"); return err }},
		{"SearchKGNotes", func() error { _, err := s.SearchKGNotes("x", 10); return err }},
		{"ListArchivedKGNotes", func() error { _, err := s.ListArchivedKGNotes(); return err }},
		{"UpsertNoteSymbolLink", func() error {
			_, err := s.UpsertNoteSymbolLink(graphstore.NoteSymbolLink{NoteID: "x", QualifiedName: "y", LinkKind: "mentions"})
			return err
		}},
		{"GetLinksForNote", func() error { _, err := s.GetLinksForNote("x"); return err }},
		{"GetLinksForSymbol", func() error { _, err := s.GetLinksForSymbol("x"); return err }},
		{"DeleteNoteSymbolLink", func() error { return s.DeleteNoteSymbolLink(1) }},
	}
	for _, tc := range cases {
		if err := tc.call(); err == nil {
			t.Errorf("%s after Close: expected error", tc.name)
		}
	}
}
