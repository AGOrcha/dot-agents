package graphstore

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// Regression: Windows + modernc.org/sqlite single-connection deadlock.
//
// gcc2 routes SQLite reads through QueryContext with the Path-A
// request-timeout context. On modernc.org/sqlite + Windows the driver does
// NOT honor a ctx deadline mid-query, so database/sql cannot reclaim the
// connection on ctx expiry. With SetMaxOpenConns(1) the in-flight query
// holds the only connection and the next acquisition blocks forever — CI
// panicked with "test timed out after 5m" on windows-latest while ubuntu
// and macos stayed green (their builds DO honor the ctx, which is exactly
// why a same-OS reproduction is impossible here).
//
// The fix attaches a deadline watchdog to queryContextGuarded that calls
// rows.Close() when the request-timeout ctx fires — an explicit driver
// abort that returns the lone conn even on the modernc/Windows path that
// ignores the ctx. These tests are fast and deterministic (no real timeout
// wait) and assert the conn-release behaviour the fix guarantees.

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

// TestQueryContextGuarded_WatchdogFiresOnDeadline is the platform-independent
// fixed-vs-broken assertion. The fix's correctness hinges on a watchdog that
// force-closes the result set when the request-timeout ctx expires — that is
// the ONLY thing that frees the lone conn on the modernc/Windows path where
// the driver ignores the ctx. This test observes the watchdog firing via the
// onGuardedQueryTimeout seam. Without the watchdog (the pre-fix code) the
// hook is never invoked and this test fails on every OS, deterministically.
func TestQueryContextGuarded_WatchdogFiresOnDeadline(t *testing.T) {
	s := openInternalTestStore(t)
	for i := 0; i < 5; i++ {
		if _, err := s.UpsertNode(NodeInfo{
			Kind: "Function", Name: "fn" + itoa(i), FilePath: "f.go", Language: "go",
		}, "h"); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	fired := make(chan struct{}, 1)
	var once sync.Once
	withGuardedQueryTimeoutHook(t, func() {
		once.Do(func() { close(fired) })
	})

	// Deadline long enough that QueryContext succeeds and takes the lone
	// conn, short enough the watchdog fires sub-second.
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()

	rows, err := s.queryContextGuarded(ctx, "SELECT * FROM nodes")
	if err != nil {
		t.Fatalf("queryContextGuarded: %v", err)
	}
	// Abandon the result set WITHOUT draining or Close — the modernc/Windows
	// shape where normal flow never returns the conn. Only the watchdog can.
	_ = rows

	select {
	case <-fired:
		// Watchdog force-closed the timed-out result set: the conn-release
		// path the Windows deadlock needs is present and triggered.
	case <-time.After(5 * time.Second):
		t.Fatal("deadline watchdog never fired: timed-out result set would strand the lone conn on modernc/Windows (5m-panic regression)")
	}
}

// TestQueryContextGuarded_ConnReusableAfterDeadline asserts the observable
// behavioural consequence: after a guarded read's ctx expires, the single
// connection is reusable (a follow-up query succeeds promptly) instead of
// being stranded until the 5m harness panic.
//
// Windows-path reasoning: on the CI macos/ubuntu modernc builds database/sql
// also reclaims the conn on ctx cancel, so this passes there even without the
// watchdog — that is why the Windows failure could not be reproduced same-OS.
// On modernc/Windows the driver does NOT abort on ctx, so database/sql cannot
// reclaim it; the watchdog's explicit rows.Close() (proven to fire by the
// test above) is then the sole mechanism that frees the conn. The two tests
// together cover the Windows path without a Windows runner.
func TestQueryContextGuarded_ConnReusableAfterDeadline(t *testing.T) {
	s := openInternalTestStore(t)
	for i := 0; i < 5; i++ {
		if _, err := s.UpsertNode(NodeInfo{
			Kind: "Function", Name: "fn" + itoa(i), FilePath: "f.go", Language: "go",
		}, "h"); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	rows, err := s.queryContextGuarded(ctx, "SELECT * FROM nodes")
	if err != nil {
		t.Fatalf("queryContextGuarded: %v", err)
	}
	_ = rows // abandoned: only ctx-expiry conn release can rescue this

	acq := make(chan error, 1)
	go func() {
		_, err := s.GetMetadata("last_updated")
		acq <- err
	}()
	select {
	case err := <-acq:
		if err != nil {
			t.Fatalf("follow-up single-conn query failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("connection stranded by abandoned timed-out result set (windows-latest deadlock regression)")
	}
}

// TestGetImpactRadius_NoSelfDeadlockUnderSingleConn proves the full Path-A
// read path completes well under the CI 5m budget on the single-connection
// pool. computeImpactRadius re-enters the store (GetEdgesAmong /
// resolveImpactNodes); loadEdgeAdjacency MUST release the lone conn (via its
// own deferred Close) before that re-entrant call or it deadlocks on itself —
// the exact windows-latest failure mode the bare, non-deferred ~467
// rows.Close() created.
func TestGetImpactRadius_NoSelfDeadlockUnderSingleConn(t *testing.T) {
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
		t.Fatal("GetImpactRadius deadlocked on the single connection (windows-latest regression)")
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
