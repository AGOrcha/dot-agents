package graphstore

import (
	"database/sql"
)

// Test seams — function variables that wrap external calls so unit tests
// can inject failures without touching real I/O. These are exported only at
// the package level (lowercase), so tests in this package can swap them via
// the helper functions in seams_test.go.
//
// Production code routes through these variables; the defaults are the
// real implementations and zero-overhead.

// sqlOpen is the seam wrapping sql.Open. Tests can replace it to simulate
// driver-level failures.
var sqlOpen = sql.Open

// dbExec is the seam wrapping (*sql.DB).Exec. Tests can replace it to
// simulate PRAGMA or schema Exec failures.
var dbExec = func(db *sql.DB, query string, args ...any) (sql.Result, error) {
	return db.Exec(query, args...)
}

// onGuardedQueryTimeout is invoked by queryContextGuarded's deadline
// watchdog immediately after it force-closes a timed-out result set to
// release the single SQLite connection. Default is a no-op (zero overhead
// in production). The regression test swaps it to deterministically observe
// that the conn-release path fires on ctx expiry — the behaviour modernc on
// Windows would otherwise never trigger, causing the single-conn deadlock.
var onGuardedQueryTimeout = func() {}
