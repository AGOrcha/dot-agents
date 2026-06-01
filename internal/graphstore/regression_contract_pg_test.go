package graphstore_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// regression_contract_pg_test.go is the testcontainer-gated cross-provider
// half of the gcc4 regression: it proves the same hard-cap + request-timeout
// semantics hold against a real Postgres backend, not just SQLite. Skips
// cleanly if Docker is unavailable via lazyPostgresDSN's shared mechanism.

// TestRegression_PostgresSearchNodesEnforcesHardLimit is the Postgres twin of
// TestSQLiteSearchNodesEnforcesHardLimit (bounds_enforcement_test.go). It
// proves the hard SearchNodes row cap is enforced on the Postgres path too:
// seed more rows than the cap, ask for "everything", get back at most the
// hard cap. With both backends asserting the SAME ceiling we have the
// cross-path parity the gcc4 charter demands.
func TestRegression_PostgresSearchNodesEnforcesHardLimit(t *testing.T) {
	s := openPGContainerStore(t)

	// Unique prefix per test (the container is shared — see
	// postgres_container_test.go header) so previously-seeded rows from
	// other tests don't trip the cap assertion.
	const prefix = "pg_reg_hardcap_"
	const hardLimit = 2000 // mirrors graphstore.hardSearchLimit (package-private)
	total := hardLimit + 50

	// Batch the seed into one StoreFileNodesEdges call (one Tx) — per-row
	// upserts would burn the suite budget for ~2k rows over a network
	// roundtrip per row.
	nodes := make([]graphstore.NodeInfo, 0, total)
	for i := 0; i < total; i++ {
		nodes = append(nodes, graphstore.NodeInfo{
			Kind:     graphstore.NodeKindFunction,
			Name:     fmt.Sprintf("%sfn%d", prefix, i),
			FilePath: "pg_reg_hardcap.go",
		})
	}
	if err := s.StoreFileNodesEdges("pg_reg_hardcap.go", nodes, nil, ""); err != nil {
		t.Fatalf("seed: %v", err)
	}

	got, err := s.SearchNodes(prefix+"fn", 10_000_000) // caller asks for "everything"
	if err != nil {
		t.Fatalf("SearchNodes: %v", err)
	}
	if len(got) > hardLimit {
		t.Fatalf("Postgres SearchNodes returned %d rows, hard cap is %d — cross-path parity broken",
			len(got), hardLimit)
	}
	if len(got) != hardLimit {
		t.Fatalf("expected exactly the hard cap %d rows, got %d", hardLimit, len(got))
	}
}

// TestRegression_PostgresSearchNodesZeroLimitUsesDefault is the Postgres twin
// of TestSQLiteSearchNodesZeroLimitUsesDefault: an unset (0) limit is the
// provider default, not "unbounded". Same number on both providers.
func TestRegression_PostgresSearchNodesZeroLimitUsesDefault(t *testing.T) {
	s := openPGContainerStore(t)

	const prefix = "pg_reg_default_"
	const defaultLimit = 100 // mirrors graphstore.defaultSearchLimit
	total := defaultLimit + 25

	nodes := make([]graphstore.NodeInfo, 0, total)
	for i := 0; i < total; i++ {
		nodes = append(nodes, graphstore.NodeInfo{
			Kind:     graphstore.NodeKindFunction,
			Name:     fmt.Sprintf("%sfn%d", prefix, i),
			FilePath: "pg_reg_default.go",
		})
	}
	if err := s.StoreFileNodesEdges("pg_reg_default.go", nodes, nil, ""); err != nil {
		t.Fatalf("seed: %v", err)
	}

	got, err := s.SearchNodes(prefix+"fn", 0)
	if err != nil {
		t.Fatalf("SearchNodes: %v", err)
	}
	if len(got) != defaultLimit {
		t.Fatalf("Postgres 0 limit should yield default %d rows, got %d (cross-path parity broken)",
			defaultLimit, len(got))
	}
}

// TestRegression_PostgresOpenHonorsCallerDeadline is the request-timeout
// regression for the Postgres connection setup boundary: a caller passing a
// pre-cancelled context to OpenPostgres must fail fast, not block on pool
// initialisation. This is the deepest "timeout uniformity across providers"
// proof we can make at the OpenPostgres seam — every per-request method
// derives its ctx from requestContext (proven by the AST regression in
// regression_contract_test.go).
func TestRegression_PostgresOpenHonorsCallerDeadline(t *testing.T) {
	// We deliberately don't reuse the shared container; this test only needs
	// to prove the OpenPostgres ctx is honoured. Use the shared DSN if Docker
	// is available so the test still proves a real network behavior.
	dsn := lazyPostgresDSN(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel

	start := time.Now()
	s, err := graphstore.OpenPostgres(ctx, dsn)
	elapsed := time.Since(start)

	if s != nil {
		_ = s.Close()
	}
	if err == nil {
		t.Fatal("OpenPostgres with pre-cancelled ctx returned no error — caller deadline not honoured")
	}
	// Should fail fast — not hang for the full Postgres connect timeout.
	if elapsed > 5*time.Second {
		t.Errorf("OpenPostgres took %v with pre-cancelled ctx — should fail fast", elapsed)
	}
}
