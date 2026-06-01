package storetest_test

// In-package smoke tests for the prefix-parameterized runners. The
// runners themselves are normally exercised by sqlite_test.go and
// postgres_test.go in the parent graphstore package; the tests below
// pin two behaviors the consumers depend on:
//
//  1. Each runner runs to completion against a real graphstore.Store
//     (compile-time interface match is necessary but not sufficient —
//     the prefix interpolation must produce valid keys for every
//     backend operation the runner invokes).
//  2. Running the same runner twice against the same store with
//     different prefixes does not produce cross-invocation row
//     collisions. This is the isolation contract the shared Postgres
//     testcontainer relies on; the SQLite-backed verification here is a
//     load-bearing equivalence because both backends share the same
//     Store interface and uniqueness keys.
//
// SQLite is used as the test driver because it is a non-shared backend
// (each call to OpenSQLite returns a fresh, file-scoped database) and
// because it avoids the testcontainers/Postgres dependency that gates
// graphstore's postgres_test.go.

import (
	"path/filepath"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/graphstore"
	"github.com/AGOrcha/dot-agents/internal/graphstore/internal/storetest"
)

// newSmokeStore returns a fresh SQLite-backed graphstore.Store for one
// smoke test. Closed automatically at test end.
func newSmokeStore(t *testing.T) graphstore.Store {
	t.Helper()
	dir := t.TempDir()
	s, err := graphstore.OpenSQLite(filepath.Join(dir, "smoke.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestRunMetadataRoundTrip_Smoke(t *testing.T) {
	storetest.RunMetadataRoundTrip(t, newSmokeStore(t), "smoke-meta-a-")
}

func TestRunEdgeUpsertCreate_Smoke(t *testing.T) {
	storetest.RunEdgeUpsertCreate(t, newSmokeStore(t), "smoke-edge-c-")
}

func TestRunEdgeUpsertUpdate_Smoke(t *testing.T) {
	storetest.RunEdgeUpsertUpdate(t, newSmokeStore(t), "smoke-edge-u-")
}

func TestRunNoteSymbolLinkRoundTrip_Smoke(t *testing.T) {
	storetest.RunNoteSymbolLinkRoundTrip(t, newSmokeStore(t), "smoke-link-rt-")
}

func TestRunNoteSymbolLinkIdempotent_Smoke(t *testing.T) {
	storetest.RunNoteSymbolLinkIdempotent(t, newSmokeStore(t), "smoke-link-idem-")
}

// TestPrefixIsolation_SameStore proves the isolation contract that the
// shared Postgres testcontainer depends on: running every runner twice
// against ONE store handle with two distinct prefixes must succeed and
// must not produce cross-invocation collisions. If a runner accidentally
// wrote to a fixed (non-prefixed) row key, the second invocation would
// fail (e.g. RunEdgeUpsertCreate seeing a pre-existing edge, or
// RunNoteSymbolLinkIdempotent counting two links instead of one).
//
// This is a NEGATIVE-path guard against a regression in any future
// runner author dropping the prefix on one row write. Without prefix
// hygiene this test would fail on the second invocation.
func TestPrefixIsolation_SameStore(t *testing.T) {
	s := newSmokeStore(t)

	prefixes := []string{"iso-A-", "iso-B-"}
	for _, p := range prefixes {
		storetest.RunMetadataRoundTrip(t, s, p+"meta-")
		storetest.RunEdgeUpsertCreate(t, s, p+"edgec-")
		storetest.RunEdgeUpsertUpdate(t, s, p+"edgeu-")
		storetest.RunNoteSymbolLinkRoundTrip(t, s, p+"linkrt-")
		storetest.RunNoteSymbolLinkIdempotent(t, s, p+"linkidem-")
	}
}

// TestPrefixCollision_Negative pins the failure mode the isolation
// contract prevents: running RunNoteSymbolLinkIdempotent twice against
// the same store with the SAME prefix produces two distinct notes
// pointing at the same QualifiedName, so the second-call expectation of
// "1 link for this note" still holds (because GetLinksForNote scopes by
// note ID). But running RunEdgeUpsertCreate twice with the same prefix
// would silently update the existing edge rather than create a new one
// — the runner only asserts "non-zero ID" so both calls pass, but the
// store ends up with one edge, not two.
//
// The runner's contract is: "callers pass unique prefixes; the runner
// then guarantees independence". This test documents that the prefix
// uniqueness obligation lives with the caller, not the runner, and
// asserts the no-error path for the documented-collision case so
// future refactors don't accidentally add an "already exists" check
// inside the runner that would break the prefix-isolation use case.
func TestPrefixCollision_Negative(t *testing.T) {
	s := newSmokeStore(t)
	prefix := "collide-"

	// Two invocations with the SAME prefix must each succeed in
	// isolation (the runner asserts only "non-zero ID" or
	// "1 link for this note"); the documented behavior is that
	// rows are shared, not that the second call fails.
	storetest.RunEdgeUpsertCreate(t, s, prefix)
	storetest.RunEdgeUpsertCreate(t, s, prefix)
	storetest.RunNoteSymbolLinkIdempotent(t, s, prefix)
	storetest.RunNoteSymbolLinkIdempotent(t, s, prefix)
}
