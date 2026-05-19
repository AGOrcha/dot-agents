package graphstore

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sqlited "modernc.org/sqlite"
)

// Regression: Windows + modernc.org/sqlite whole-store deadlock.
//
// Root cause (architectural, after two non-converging Windows patch
// passes): modernc.org/sqlite's _sqlite3Step is a non-preemptible
// translated-C VM loop. On Windows a ctx deadline — or an out-of-band
// rows.Close() from a watchdog goroutine — CANNOT interrupt an
// in-progress step. Combined with the former db.SetMaxOpenConns(1),
// ANY single wedged/slow step held the only connection and the next
// operation could never acquire it -> whole-store deadlock and the
// windows-latest "test timed out after 5m" panic. The single-conn cap
// was the disease; the watchdog had nowhere to fail over.
//
// Fix: relax the Go connection-pool cap (OpenSQLite uses a small bounded
// pool, not 1) and make the SQLite request timeout abandon-and-fail
// instead of trying to interrupt the non-interruptible step. WAL +
// busy_timeout still deliver the documented cross-process write
// serialization (independent of the Go pool size).
//
// These tests are fast and deterministic. The "wedged step" is injected
// via a wrapping database/sql driver returned through the sanctioned
// sqlOpen seam — a designated query blocks on a channel exactly the way
// a non-preemptible modernc step would block, with NO real multi-minute
// wait, so the test can never itself hang CI.

func openInternalTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	dir := t.TempDir()
	s, err := OpenSQLite(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// withGuardedQueryTimeoutHook swaps the onGuardedQueryTimeout seam for the
// duration of the test (mirrors withSQLOpen/withDBExec in seams_test.go).
func withGuardedQueryTimeoutHook(t *testing.T, fn func()) {
	t.Helper()
	prev := onGuardedQueryTimeout
	onGuardedQueryTimeout = fn
	t.Cleanup(func() { onGuardedQueryTimeout = prev })
}

// --- wedging driver ---------------------------------------------------------
//
// wedgeDriver wraps the real modernc "sqlite" driver. Any query whose text
// contains the wedge marker blocks inside Query() until release is closed —
// a faithful, deterministic stand-in for modernc's non-preemptible
// _sqlite3Step (which on Windows neither ctx nor Close can interrupt). All
// other statements pass straight through to modernc.

const wedgeMarker = "/*__WEDGE__*/"

type wedgeDriver struct {
	inner    driver.Driver
	release  chan struct{}
	wedged   chan struct{} // closed once the wedged query is in-flight
	wedgeOne sync.Once
}

func (d *wedgeDriver) Open(name string) (driver.Conn, error) {
	c, err := d.inner.Open(name)
	if err != nil {
		return nil, err
	}
	return &wedgeConn{Conn: c, d: d}, nil
}

type wedgeConn struct {
	driver.Conn
	d *wedgeDriver
}

func (c *wedgeConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if containsMarker(query) {
		c.d.wedgeOne.Do(func() { close(c.d.wedged) })
		<-c.d.release // block like a non-preemptible modernc step
	}
	if qc, ok := c.Conn.(driver.QueryerContext); ok {
		return qc.QueryContext(ctx, stripMarker(query), args)
	}
	return nil, driver.ErrSkip
}

func (c *wedgeConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if ec, ok := c.Conn.(driver.ExecerContext); ok {
		return ec.ExecContext(ctx, query, args)
	}
	return nil, driver.ErrSkip
}

func (c *wedgeConn) Prepare(query string) (driver.Stmt, error) { return c.Conn.Prepare(query) }

func (c *wedgeConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	if pc, ok := c.Conn.(driver.ConnPrepareContext); ok {
		return pc.PrepareContext(ctx, query)
	}
	return c.Conn.Prepare(query)
}

func (c *wedgeConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if bt, ok := c.Conn.(driver.ConnBeginTx); ok {
		return bt.BeginTx(ctx, opts)
	}
	return c.Conn.Begin() //nolint:staticcheck
}

func containsMarker(q string) bool { return len(q) >= len(wedgeMarker) && indexOf(q, wedgeMarker) >= 0 }

func stripMarker(q string) string {
	i := indexOf(q, wedgeMarker)
	if i < 0 {
		return q
	}
	return q[:i] + q[i+len(wedgeMarker):]
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

var wedgeDriverRegistered atomic.Bool

// newWedgeStore opens a SQLiteStore whose driver can wedge one query. The
// caller releases the wedge by closing the returned channel. maxOpen lets a
// test reproduce the old single-conn disease (1) vs. the production relaxed
// pool (0 = leave OpenSQLite's setting untouched).
func newWedgeStore(t *testing.T, maxOpen int) (*SQLiteStore, chan struct{}, chan struct{}) {
	t.Helper()
	release := make(chan struct{})
	wedged := make(chan struct{})
	wd := &wedgeDriver{inner: &sqlited.Driver{}, release: release, wedged: wedged}

	name := "wedge-" + t.Name()
	// database/sql panics on duplicate Register; each test name is unique
	// and tests in a package run in one process, so a per-name driver is
	// safe and isolated.
	sql.Register(name, wd)
	wedgeDriverRegistered.Store(true)

	prev := sqlOpen
	sqlOpen = func(_ string, dsn string) (*sql.DB, error) { return sql.Open(name, dsn) }
	t.Cleanup(func() { sqlOpen = prev })

	dir := t.TempDir()
	s, err := OpenSQLite(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("OpenSQLite (wedge): %v", err)
	}
	if maxOpen > 0 {
		s.db.SetMaxOpenConns(maxOpen) // reproduce the old disease
	}
	t.Cleanup(func() { s.Close() })
	return s, release, wedged
}

// TestSQLiteStore_WedgedStepDoesNotDeadlockStore is THE disease/cure test.
// One operation's step is wedged (non-preemptible, like modernc/Windows);
// a second, independent operation must still complete. Under the production
// relaxed pool it does (different conn). The companion sub-test pins the
// fail-on-old direction: with the connection pool forced back to 1, the
// second operation deadlocks exactly as windows-latest did.
func TestSQLiteStore_WedgedStepDoesNotDeadlockStore(t *testing.T) {
	// --- CURE: production relaxed pool, second op completes -----------------
	t.Run("relaxed_pool_no_deadlock", func(t *testing.T) {
		s, release, wedged := newWedgeStore(t, 0) // 0 = keep OpenSQLite's pool
		defer close(release)

		seedNodes(t, s, 5)

		// Op A: a guarded read whose step wedges and never returns until we
		// release it. requestContext gives it the provider timeout; on the
		// abandon-and-fail path queryContextGuarded returns promptly.
		stuck := make(chan struct{})
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
			defer cancel()
			_, _ = s.queryContextGuarded(ctx, wedgeMarker+"SELECT * FROM nodes")
			close(stuck)
		}()
		<-wedged // Op A's step is now wedged holding a conn

		// Op B: an independent operation. It MUST complete while A is wedged.
		bDone := make(chan error, 1)
		go func() {
			_, err := s.GetMetadata("last_updated")
			bDone <- err
		}()
		select {
		case err := <-bDone:
			if err != nil {
				t.Fatalf("second op failed: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("second op deadlocked while one step was wedged — the windows-latest whole-store deadlock is NOT cured")
		}

		// Op A itself returns via abandon-and-fail (deadline path), not by
		// waiting for the wedged step.
		select {
		case <-stuck:
		case <-time.After(5 * time.Second):
			t.Fatal("guarded read did not abandon-and-fail on deadline")
		}
	})

	// --- DISEASE: pool forced to 1 reproduces the deadlock -----------------
	t.Run("capped_pool_deadlocks_fail_on_old", func(t *testing.T) {
		s, release, wedged := newWedgeStore(t, 1) // old SetMaxOpenConns(1)
		defer close(release)

		seedNodes(t, s, 5)

		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
			defer cancel()
			_, _ = s.queryContextGuarded(ctx, wedgeMarker+"SELECT * FROM nodes")
		}()
		<-wedged

		bDone := make(chan error, 1)
		go func() {
			_, err := s.GetMetadata("last_updated")
			bDone <- err
		}()
		select {
		case <-bDone:
			t.Fatal("expected the cap=1 pool to deadlock the second op (proves the test discriminates old vs new); it did not")
		case <-time.After(1500 * time.Millisecond):
			// Correct: with the pool capped at 1 the wedged step holds the
			// only conn and the second op cannot proceed — exactly the old
			// windows-latest failure. Releasing the wedge (deferred) lets
			// the goroutines unwind cleanly so the test does not leak.
		}
	})
}

func seedNodes(t *testing.T, s *SQLiteStore, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if _, err := s.UpsertNode(NodeInfo{
			Kind: "Function", Name: "fn" + itoa(i), FilePath: "f.go", Language: "go",
		}, "h"); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
}

// TestQueryContextGuarded_AbandonAndFailOnDeadline asserts the SQLite
// request-timeout mechanism: when the provider deadline fires before the
// step yields a result set, queryContextGuarded returns a deadline-bounded
// error (errRequestTimeout) WITHOUT waiting for the non-preemptible step,
// and the abandoned conn's step is reaped via onGuardedQueryTimeout once it
// finishes. On the pre-fix code this path did not exist (the watchdog
// waited on *sql.Rows that QueryContext never returned), so this fails
// deterministically on every OS against the old implementation.
func TestQueryContextGuarded_AbandonAndFailOnDeadline(t *testing.T) {
	s, release, wedged := newWedgeStore(t, 0)

	reaped := make(chan struct{}, 1)
	var once sync.Once
	withGuardedQueryTimeoutHook(t, func() { once.Do(func() { close(reaped) }) })

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()

	start := time.Now()
	got := make(chan error, 1)
	go func() {
		_, err := s.queryContextGuarded(ctx, wedgeMarker+"SELECT * FROM nodes")
		got <- err
	}()
	<-wedged

	select {
	case err := <-got:
		if err != errRequestTimeout {
			t.Fatalf("want errRequestTimeout, got %v", err)
		}
		if d := time.Since(start); d > 3*time.Second {
			t.Fatalf("guarded read blocked on the wedged step for %s — abandon-and-fail did not engage", d)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("queryContextGuarded blocked on the non-preemptible step instead of abandoning + failing")
	}

	// Release the wedge: the orphaned step finishes and the reaper runs.
	close(release)
	select {
	case <-reaped:
	case <-time.After(5 * time.Second):
		t.Fatal("abandoned conn's result set was never reaped after the step finished")
	}
}

// TestGetImpactRadius_NoSelfDeadlock proves the full Path-A read path
// completes well under the CI budget. computeImpactRadius re-enters the
// store (GetEdgesAmong / resolveImpactNodes); loadEdgeAdjacency releases its
// edge result set via deferred Close before the re-entrant call. Under the
// relaxed pool a stranded result set no longer deadlocks the store, but this
// guards against a regression that strands conns AND re-caps the pool.
func TestGetImpactRadius_NoSelfDeadlock(t *testing.T) {
	s := openInternalTestStore(t)

	const n = 50
	for i := 0; i < n; i++ {
		fp := filepath.Join("pkg", "f"+itoa(i)+".go")
		if _, err := s.UpsertNode(NodeInfo{
			Kind: "Function", Name: "fn" + itoa(i), FilePath: fp, Language: "go",
		}, "h"); err != nil {
			t.Fatalf("seed node: %v", err)
		}
	}
	for i := 0; i < n-1; i++ {
		if _, err := s.UpsertEdge(EdgeInfo{
			Kind:     "CALLS",
			Source:   filepath.Join("pkg", "f"+itoa(i)+".go") + "::fn" + itoa(i),
			Target:   filepath.Join("pkg", "f"+itoa(i+1)+".go") + "::fn" + itoa(i+1),
			FilePath: filepath.Join("pkg", "f"+itoa(i)+".go"),
		}); err != nil {
			t.Fatalf("seed edge: %v", err)
		}
	}

	done := make(chan error, 1)
	go func() {
		_, err := s.GetImpactRadius([]string{filepath.Join("pkg", "f0.go")}, 10, 1000)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("GetImpactRadius: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("GetImpactRadius deadlocked (windows-latest regression)")
	}
}

// TestOpenSQLite_SyncNormalForWindowsBulkWrite asserts the durability tuning
// that makes the bulk write path tractable on modernc/Windows is actually
// applied (WAL + synchronous=NORMAL). Unrelated to the pool change, but the
// pragma must not silently regress to the slow modernc default.
func TestOpenSQLite_SyncNormalForWindowsBulkWrite(t *testing.T) {
	s := openInternalTestStore(t)
	var mode int
	// PRAGMA synchronous returns 0=OFF, 1=NORMAL, 2=FULL, 3=EXTRA.
	if err := s.db.QueryRow("PRAGMA synchronous").Scan(&mode); err != nil {
		t.Fatalf("PRAGMA synchronous: %v", err)
	}
	if mode != 1 {
		t.Fatalf("synchronous mode = %d, want 1 (NORMAL); FULL/default reintroduces the modernc/Windows 5m-timeout bulk-write regression", mode)
	}
	var jmode string
	if err := s.db.QueryRow("PRAGMA journal_mode").Scan(&jmode); err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if jmode != "wal" {
		t.Fatalf("journal_mode = %q, want wal (NORMAL is only crash-safe paired with WAL)", jmode)
	}
}

// TestOpenSQLite_RelaxedPoolNotCappedAtOne pins the core architectural fix:
// the store must NOT cap the connection pool at 1 (the disease). A small
// bounded pool with >1 max-open is required so a wedged non-preemptible
// modernc step cannot chokepoint the whole store.
func TestOpenSQLite_RelaxedPoolNotCappedAtOne(t *testing.T) {
	s := openInternalTestStore(t)
	stats := s.db.Stats()
	if stats.MaxOpenConnections == 1 {
		t.Fatal("connection pool is capped at 1 — reintroduces the modernc/Windows whole-store deadlock (SetMaxOpenConns(1) is the disease)")
	}
	if stats.MaxOpenConnections <= 0 {
		t.Fatalf("expected a bounded (>1) pool, got MaxOpenConnections=%d", stats.MaxOpenConnections)
	}
}

// itoa is a tiny dependency-free int->string for stable test fixture names.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(b[pos:])
}
