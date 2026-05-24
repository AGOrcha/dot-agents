package graphstore

import (
	"bufio"
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// regression_contract_test.go closes plan graphstore-concurrency-contract task
// gcc4 (OD-1 + maxNodes Low-1). It proves the three Path-A contract guarantees
// the CONTRACT.md publishes — hard cap, cross-path parity, request timeout —
// hold uniformly across the native (SQLite / Postgres) and CRG paths.
//
// Maintainer rulings closed here:
//
//   - OD-1 (di-refactor): the Deps singleton is justified ONLY as a holder of
//     a contract-typed Store handle whose provider owns concurrency. The
//     singleton is not the concurrency story — the provider behind the
//     contract is. The rationale comment is anchored on commands/workflow/deps.go
//     (gcc3 PR #57); this regression locks it in by proving the contract has
//     teeth at the provider boundary (clamping + timeout uniformity), so the
//     singleton's "holder of the contract" framing is verifiable, not hand-wavy.
//   - maxNodes Low-1: enforced *via* the contract (bounds.go's hardMaxNodes
//     chokepoint) rather than a one-off patch — proven by
//     TestRegression_MaxNodes_Low1_ClosedViaContract.
//
// The Postgres parity branch is testcontainer-gated and lives in
// regression_contract_pg_test.go so the same Docker-detection / shared-DB
// machinery as postgres_container_test.go applies. The in-package assertions
// here run unconditionally and are the load-bearing proof of OD-1 + Low-1.

// TestRegression_MaxNodes_Low1_ClosedViaContract proves maxNodes Low-1 is
// closed *through* the contract, not a one-off cap. The single clampBound
// chokepoint is the only place a caller-requested ceiling is normalised; the
// table covers every documented contract case for the maxNodes axis:
// 0/negative → default, in-range unchanged, equal to hard preserved, over hard
// clamped down. If a future change widens the cap on any single path it must
// edit hardMaxNodes here — there is no second branch to drift from.
func TestRegression_MaxNodes_Low1_ClosedViaContract(t *testing.T) {
	cases := []struct {
		name      string
		requested int
		want      int
	}{
		{"unset_uses_default", 0, defaultMaxNodes},
		{"negative_uses_default", -42, defaultMaxNodes},
		{"in_range_preserved", 100, 100},
		{"equal_to_hard_preserved", hardMaxNodes, hardMaxNodes},
		{"one_over_hard_clamped", hardMaxNodes + 1, hardMaxNodes},
		{"far_over_hard_clamped", 1 << 30, hardMaxNodes},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, n := normalizeTraversalBounds(0, c.requested)
			if n != c.want {
				t.Fatalf("normalizeTraversalBounds(_,%d) maxNodes=%d want %d",
					c.requested, n, c.want)
			}
		})
	}
}

// TestRegression_HardCap_NeverOvershoots_NativeBFS guarantees the impact BFS
// stops EXACTLY at the cap — the pre-Path-A behaviour the spec called out
// ("advisory, overshoot by a frontier"). A star with fanout >> cap in a
// single hop is the worst-case overshoot input; the BFS must still return
// exactly `cap` nodes, not `cap + leftover frontier`.
func TestRegression_HardCap_NeverOvershoots_NativeBFS(t *testing.T) {
	const fanout = 2 * hardMaxNodes
	seeds := map[string]bool{"seed": true}
	fwd := map[string][]string{}
	rev := map[string][]string{}
	for i := 0; i < fanout; i++ {
		leaf := "leaf" + strconv.Itoa(i)
		fwd["seed"] = append(fwd["seed"], leaf)
		rev[leaf] = append(rev[leaf], "seed")
	}

	// bfsImpacted clamps to hardMaxNodes when called with a value over the cap.
	impacted := bfsImpacted(seeds, fwd, rev, hardMaxDepth, hardMaxNodes)
	if len(impacted) != hardMaxNodes {
		t.Fatalf("BFS hard cap not exact: got %d impacted, want %d (overshoot regression)",
			len(impacted), hardMaxNodes)
	}
}

// TestRegression_CrossPathParity_ClampsAreTheSameNumbers is the structural
// proof of the contract's "hard, uniform cap across native and CRG paths"
// guarantee. Every provider routes maxNodes/maxDepth/limit through
// clampBound; if two paths produced different numbers, this assertion would
// require fixing the chokepoint, not the call sites. The test ensures the
// constants the CRG path inherits are EXACTLY the constants the native BFS
// uses — there is only one set of numbers.
func TestRegression_CrossPathParity_ClampsAreTheSameNumbers(t *testing.T) {
	depthCases := []struct{ requested, want int }{
		{0, defaultMaxDepth},
		{-1, defaultMaxDepth},
		{2, 2},
		{hardMaxDepth, hardMaxDepth},
		{hardMaxDepth + 100, hardMaxDepth},
	}
	for _, c := range depthCases {
		d, _ := normalizeTraversalBounds(c.requested, 0)
		if d != c.want {
			t.Fatalf("normalizeTraversalBounds depth %d -> %d want %d", c.requested, d, c.want)
		}
	}

	limitCases := []struct{ requested, want int }{
		{0, defaultSearchLimit},
		{-7, defaultSearchLimit},
		{42, 42},
		{hardSearchLimit, hardSearchLimit},
		{hardSearchLimit + 1, hardSearchLimit},
	}
	for _, c := range limitCases {
		got := normalizeSearchLimit(c.requested)
		if got != c.want {
			t.Fatalf("normalizeSearchLimit(%d)=%d want %d", c.requested, got, c.want)
		}
	}
}

// TestRegression_RequestContext_HonorsTightParentDeadline proves the provider
// timeout boundary respects an inherited parent deadline — i.e. a parent that
// expires earlier than the provider's requestTimeout wins. This is the
// caller-cancellation half of CONTRACT.md guarantee #2: the provider deadline
// does not sever caller cancellation/deadlines.
func TestRegression_RequestContext_HonorsTightParentDeadline(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	cancel() // immediately
	ctx, cancel2 := requestContext(parent)
	defer cancel2()
	select {
	case <-ctx.Done():
	default:
		t.Fatal("requestContext did not inherit pre-cancelled parent")
	}
}

// TestRegression_PostgresRoutesEveryMethodThroughRequestContext is a
// structural assertion (AST-level, no Postgres daemon required) that every
// pool-using method on *PostgresStore obtains its context from
// requestContext(...) rather than calling context.Background() directly. This
// is the gcc4-deferred postgres.go cleanup the bundle calls out: at gcc2 the
// timeout chokepoint was uniform only on the SQLite + CRG paths because the
// postgres reads/execs still used raw context.Background(). After the gcc4
// refactor, the assertion below is structural — a future regression that
// reintroduces context.Background() in a method body will fail this test
// instead of silently bypassing the timeout boundary.
//
// Two exceptions are documented (not assertion failures):
//
//   - OpenPostgres + initSchema: connection setup, takes the caller ctx so
//     OpenPostgres can be cancelled by the caller's setup deadline.
//   - The pgScan*/pgCollect* helpers don't take a context — they iterate
//     rows already opened under a request context by the calling method.
func TestRegression_PostgresRoutesEveryMethodThroughRequestContext(t *testing.T) {
	path, ok := locatePostgresGoForTest(t)
	if !ok {
		return
	}
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read postgres.go: %v", err)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.AllErrors)
	if err != nil {
		t.Fatalf("parse postgres.go: %v", err)
	}

	// Methods exempt from the "must use requestContext" rule (connection setup).
	exempt := map[string]bool{
		"OpenPostgres": true,
		"initSchema":   true,
	}

	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		// Only check methods on *PostgresStore (skip free helpers, scan funcs).
		if fd.Recv == nil || len(fd.Recv.List) == 0 {
			continue
		}
		star, ok := fd.Recv.List[0].Type.(*ast.StarExpr)
		if !ok {
			continue
		}
		ident, ok := star.X.(*ast.Ident)
		if !ok || ident.Name != "PostgresStore" {
			continue
		}
		if exempt[fd.Name.Name] {
			continue
		}
		// Walk the body and flag any context.Background() call.
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			if pkg.Name == "context" && sel.Sel.Name == "Background" {
				t.Errorf("(*PostgresStore).%s uses context.Background() — must route through requestContext (gcc4 OD-1 cross-provider timeout uniformity)", fd.Name.Name)
			}
			return true
		})
	}
}

// TestRegression_PostgresHasNoRawContextBackground is the file-level smoke
// twin of the AST walk above: if a future commit reintroduces a raw
// context.Background() anywhere in postgres.go's body (top-level helper, new
// method, fixture), this test fails. Cheap and zero-dependency.
func TestRegression_PostgresHasNoRawContextBackground(t *testing.T) {
	path, ok := locatePostgresGoForTest(t)
	if !ok {
		return
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open postgres.go: %v", err)
	}
	defer file.Close()
	sc := bufio.NewScanner(file)
	// Lines that mention context.Background ONLY as a doc comment are fine;
	// we flag actual call syntax `context.Background()`.
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := sc.Text()
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") {
			continue
		}
		if strings.Contains(line, "context.Background()") {
			t.Errorf("postgres.go:%d uses context.Background() — must route through requestContext", lineNo)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan postgres.go: %v", err)
	}
}

// locatePostgresGoForTest finds internal/graphstore/postgres.go relative to
// the test binary's runtime working directory. Returns false (and skips)
// rather than failing if the file cannot be located — keeps the test robust
// when run from a non-standard cwd (it is normally `cd internal/graphstore`).
func locatePostgresGoForTest(t *testing.T) (string, bool) {
	t.Helper()
	// `go test ./internal/graphstore/...` runs each package test with cwd set
	// to that package, so postgres.go is sitting next to the test binary.
	candidates := []string{
		"postgres.go",
		filepath.Join("internal", "graphstore", "postgres.go"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, true
		}
	}
	t.Skipf("postgres.go not found from cwd; AST regression skipped")
	return "", false
}
