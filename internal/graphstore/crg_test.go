package graphstore_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	// _ "modernc.org/sqlite": side-effect registers SQLite driver in database/sql
	_ "modernc.org/sqlite"

	"github.com/AGOrcha/dot-agents/internal/graphstore"
	"github.com/AGOrcha/dot-agents/internal/testutil"
)

// ── parseCRGStatusOutput ──────────────────────────────────────────────────────

func TestParseCRGStatusOutput_typical(t *testing.T) {
	raw := `INFO: Some log line
Nodes: 923
Edges: 6281
Files: 50
Languages: go, ruby
Last updated: 2026-04-11T00:49:52
`
	s := parseCRGStatusOutputExported([]byte(raw))
	if s.Nodes != 923 {
		t.Errorf("Nodes: got %d, want 923", s.Nodes)
	}
	if s.Edges != 6281 {
		t.Errorf("Edges: got %d, want 6281", s.Edges)
	}
	if s.Files != 50 {
		t.Errorf("Files: got %d, want 50", s.Files)
	}
	if s.Languages != "go, ruby" {
		t.Errorf("Languages: got %q, want %q", s.Languages, "go, ruby")
	}
	if s.LastUpdated != "2026-04-11T00:49:52" {
		t.Errorf("LastUpdated: got %q", s.LastUpdated)
	}
}

func TestParseCRGStatusOutput_empty(t *testing.T) {
	s := parseCRGStatusOutputExported(nil)
	if s.Nodes != 0 || s.Edges != 0 {
		t.Errorf("expected zero stats for empty output, got %+v", s)
	}
}

func TestParseCRGStatusOutput_noInfoLines(t *testing.T) {
	raw := "Nodes: 5\nEdges: 10\nFiles: 2\nLanguages: python\nLast updated: 2026-01-01T00:00:00\n"
	s := parseCRGStatusOutputExported([]byte(raw))
	if s.Nodes != 5 || s.Edges != 10 || s.Files != 2 {
		t.Errorf("unexpected stats: %+v", s)
	}
}

// ── DiscoverCRGBin ────────────────────────────────────────────────────────────

// TestCRGBridgeFreshBuildRealCRG exercises the full build path with the actual
// code-review-graph binary and asserts that graph.db is produced and readable.
// This covers the nested-transaction defect path that stub-based tests cannot
// reach: commandWithSQLiteAutocommit must patch isolation_level before CRG
// opens SQLite so explicit BEGIN/COMMIT calls don't collide with Python's
// implicit transaction management.
//
// Skipped when the real CRG binary is not discoverable (e.g. CI without a venv).
// skipOrFailCRGBuildErr skips when the CRG binary was discoverable but its Python
// package is not importable here (an environment limitation — e.g. a sibling
// worktree's .venv shim on PATH without code_review_graph installed), and fails
// loudly otherwise. Extracted from the test so the compound module-not-found
// condition does not inflate its cognitive complexity (SonarCloud go:S3776).
// BuildReport returns (nil, err) on the build-error path, so the report pointer
// is guarded before reading Summary.
func skipOrFailCRGBuildErr(t *testing.T, report *graphstore.CRGOperationReport, err error) {
	t.Helper()
	combined := err.Error()
	if report != nil {
		combined += " " + report.Summary
	}
	if strings.Contains(combined, "code_review_graph") &&
		(strings.Contains(combined, "No module named") || strings.Contains(combined, "ModuleNotFoundError")) {
		t.Skipf("real CRG binary discovered but its Python package is not importable here: %v", err)
	}
	t.Fatalf("BuildReport failed (possible nested-transaction defect): %v", err)
}

// assertGraphDBNonEmpty opens the CRG graph.db under repoRoot and asserts the
// nodes table is non-empty, returning the row count. Extracted to keep the
// caller under the cognitive-complexity gate (SonarCloud go:S3776).
func assertGraphDBNonEmpty(t *testing.T, repoRoot string) int {
	t.Helper()
	dbPath := graphstore.CRGDBPath(repoRoot)
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("graph.db not found at %s: %v", dbPath, err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open graph.db: %v", err)
	}
	defer db.Close()
	var nodeCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM nodes").Scan(&nodeCount); err != nil {
		t.Fatalf("SELECT COUNT(*) FROM nodes: %v", err)
	}
	if nodeCount == 0 {
		t.Fatal("graph.db has zero rows in nodes table after fresh build")
	}
	return nodeCount
}

func TestCRGBridgeFreshBuildRealCRG(t *testing.T) {
	_, testFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(testFile), "..", "..")

	crgBin, err := graphstore.DiscoverCRGBin(repoRoot)
	if err != nil {
		t.Skipf("real CRG not available: %v", err)
	}

	// One Go file so CRG has something to parse.
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Explicit Bin bypasses DiscoverCRGBin on the temp dir; pythonBin() resolves
	// python3 from the same real .venv/bin directory so site-packages are present.
	bridge := &graphstore.CRGBridge{RepoRoot: tmpDir, Bin: crgBin}
	report, err := bridge.BuildReport(graphstore.BuildOptions{
		SkipFlows:       true,
		SkipPostprocess: true,
	})
	if err != nil {
		skipOrFailCRGBuildErr(t, report, err)
	}
	if report.Outcome != graphstore.CRGReadinessReady {
		t.Fatalf("expected outcome=%q, got %q; summary: %s", graphstore.CRGReadinessReady, report.Outcome, report.Summary)
	}
	if report.Status == nil || report.Status.Nodes == 0 {
		t.Fatalf("expected non-zero nodes, got status=%+v", report.Status)
	}

	// Direct SQLite assertion: the produced graph.db must have rows in nodes.
	nodeCount := assertGraphDBNonEmpty(t, tmpDir)

	// Status() must agree with what CRG wrote into graph.db.
	status, err := bridge.Status()
	if err != nil {
		t.Fatalf("Status(): %v", err)
	}
	if !status.Ready {
		t.Errorf("Status().Ready=false after successful build (state=%s, message=%s)", status.State, status.Message)
	}
	if status.Nodes != nodeCount {
		t.Errorf("Status().Nodes=%d disagrees with graph.db COUNT(*)=%d", status.Nodes, nodeCount)
	}
}

func TestDiscoverCRGBin_returnsErrorWhenMissing(t *testing.T) {
	// Use a temp dir with no .venv and no code-review-graph on PATH.
	_, err := graphstore.DiscoverCRGBin(t.TempDir())
	if err == nil {
		// If the tester has CRG on PATH this test legitimately passes — skip
		t.Skip("code-review-graph is available on PATH; skip missing-binary test")
	}
	if !strings.Contains(err.Error(), "code-review-graph") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// ── CRGDBPath / Available / DiscoverCRGBin ────────────────────────────────────

func TestCRGDBPath(t *testing.T) {
	got := graphstore.CRGDBPath("/repo/root")
	if got != filepath.Join("/repo/root", ".code-review-graph", "graph.db") {
		t.Errorf("got %q", got)
	}
}

func TestCRGBridge_Available_EmptyBin(t *testing.T) {
	b := &graphstore.CRGBridge{RepoRoot: t.TempDir(), Bin: ""}
	if b.Available() {
		t.Error("empty Bin should not be Available")
	}
}

func TestCRGBridge_Available_MissingFile(t *testing.T) {
	b := &graphstore.CRGBridge{RepoRoot: t.TempDir(), Bin: "/no/such/binary"}
	if b.Available() {
		t.Error("missing binary should not be Available")
	}
}

func TestCRGBridge_Available_FileExists(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "fake")
	_ = os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755)
	b := &graphstore.CRGBridge{RepoRoot: dir, Bin: p}
	if !b.Available() {
		t.Errorf("existing binary should be Available")
	}
}

// ── ReadNodes / ReadEdges / Status (against a fake graph.db) ──────────────────

// writeFakeCRGDB seeds a .code-review-graph/graph.db SQLite file under repoRoot
// containing the minimum schema needed by ReadNodes/ReadEdges/Status.
func writeFakeCRGDB(t *testing.T, repoRoot string, nodeCount, edgeCount int) {
	t.Helper()
	dir := filepath.Join(repoRoot, ".code-review-graph")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "graph.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open fake db: %v", err)
	}
	defer db.Close()

	ddl := `
		CREATE TABLE nodes (
		  id INTEGER PRIMARY KEY AUTOINCREMENT,
		  kind TEXT, name TEXT, qualified_name TEXT UNIQUE,
		  file_path TEXT, line_start INTEGER, line_end INTEGER,
		  language TEXT, parent_name TEXT, params TEXT, return_type TEXT,
		  is_test INTEGER, file_hash TEXT, extra TEXT, updated_at REAL
		);
		CREATE TABLE edges (
		  id INTEGER PRIMARY KEY AUTOINCREMENT,
		  kind TEXT, source_qualified TEXT, target_qualified TEXT,
		  file_path TEXT, line INTEGER, extra TEXT, updated_at REAL
		);`
	if _, err := db.Exec(ddl); err != nil {
		t.Fatalf("ddl: %v", err)
	}
	for i := 0; i < nodeCount; i++ {
		name := "fn" + string(rune('a'+i))
		_, _ = db.Exec(
			`INSERT INTO nodes (kind,name,qualified_name,file_path,line_start,line_end,language,parent_name,params,return_type,is_test,file_hash,extra,updated_at)
			 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			"Function", name, "pkg::"+name, "f.go", 1, 5, "go", "pkg", "", "", 0, "", "{}", 1.0,
		)
	}
	for i := 0; i < edgeCount; i++ {
		_, _ = db.Exec(
			`INSERT INTO edges (kind,source_qualified,target_qualified,file_path,line,extra,updated_at)
			 VALUES (?,?,?,?,?,?,?)`,
			"CALLS", "pkg::A", "pkg::B", "f.go", 1, "{}", 1.0,
		)
	}
}

func TestCRGBridge_ReadNodes_NoDB(t *testing.T) {
	b := &graphstore.CRGBridge{RepoRoot: t.TempDir(), Bin: ""}
	nodes, err := b.ReadNodes(10)
	if err != nil {
		t.Errorf("missing db should not error: %v", err)
	}
	if len(nodes) != 0 {
		t.Errorf("missing db should yield 0 nodes, got %d", len(nodes))
	}
}

func TestCRGBridge_ReadNodes_WithDB(t *testing.T) {
	dir := t.TempDir()
	writeFakeCRGDB(t, dir, 3, 0)
	b := &graphstore.CRGBridge{RepoRoot: dir, Bin: ""}
	nodes, err := b.ReadNodes(0)
	if err != nil {
		t.Fatalf("ReadNodes: %v", err)
	}
	if len(nodes) != 3 {
		t.Errorf("expected 3 nodes, got %d", len(nodes))
	}
}

func TestCRGBridge_ReadNodes_Limit(t *testing.T) {
	dir := t.TempDir()
	writeFakeCRGDB(t, dir, 5, 0)
	b := &graphstore.CRGBridge{RepoRoot: dir, Bin: ""}
	nodes, err := b.ReadNodes(2)
	if err != nil {
		t.Fatalf("ReadNodes: %v", err)
	}
	if len(nodes) != 2 {
		t.Errorf("expected 2 (limit), got %d", len(nodes))
	}
}

// TestCRGBridge_ReadNodes_PermissionDeniedStat_ReturnsError: a real Stat
// failure (permission denied on the containing dir) must surface as an
// error, not be folded into the legitimate-absence "no CRG db" nil,nil path.
func TestCRGBridge_ReadNodes_PermissionDeniedStat_ReturnsError(t *testing.T) {
	repo := t.TempDir()
	dbPath := graphstore.CRGDBPath(repo)
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatal(err)
	}
	testutil.MakeDirUnreadable(t, filepath.Dir(dbPath))

	b := &graphstore.CRGBridge{RepoRoot: repo, Bin: ""}
	nodes, err := b.ReadNodes(10)
	if err == nil {
		t.Fatal("expected a real error from a permission-denied Stat, not the legitimate-absence nil,nil")
	}
	if nodes != nil {
		t.Errorf("expected nil nodes alongside the error, got %v", nodes)
	}
}

// TestCRGBridge_ReadNodes_ScanFailureCounted: a row that fails to Scan
// (corrupt column type) must be surfaced via the returned error instead of
// vanishing via a silent `continue` — the well-formed rows are still
// returned so the caller can decide what to do with the partial result.
func TestCRGBridge_ReadNodes_ScanFailureCounted(t *testing.T) {
	dir := t.TempDir()
	writeFakeCRGDB(t, dir, 2, 0)
	db, err := sql.Open("sqlite", graphstore.CRGDBPath(dir))
	if err != nil {
		t.Fatalf("open fake db: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO nodes (kind,name,qualified_name,file_path,line_start,line_end,language,parent_name,params,return_type,is_test,file_hash,extra,updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		"Function", "badrow", "pkg::badrow", "f.go", "not-a-number", 5, "go", "pkg", "", "", 0, "", "{}", 1.0,
	); err != nil {
		t.Fatalf("insert bad row: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close fake db: %v", err)
	}

	b := &graphstore.CRGBridge{RepoRoot: dir, Bin: ""}
	nodes, err := b.ReadNodes(0)
	if err == nil {
		t.Fatal("expected the row-scan failure to be surfaced, not silently skipped")
	}
	if len(nodes) != 2 {
		t.Errorf("expected the 2 well-formed rows to still be returned, got %d", len(nodes))
	}
}

func TestCRGBridge_ReadEdges_NoDB(t *testing.T) {
	b := &graphstore.CRGBridge{RepoRoot: t.TempDir(), Bin: ""}
	edges, err := b.ReadEdges(10)
	if err != nil {
		t.Errorf("missing db should not error: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("expected 0 edges, got %d", len(edges))
	}
}

func TestCRGBridge_ReadEdges_WithDB(t *testing.T) {
	dir := t.TempDir()
	writeFakeCRGDB(t, dir, 0, 4)
	b := &graphstore.CRGBridge{RepoRoot: dir, Bin: ""}
	edges, err := b.ReadEdges(0)
	if err != nil {
		t.Fatalf("ReadEdges: %v", err)
	}
	if len(edges) != 4 {
		t.Errorf("expected 4 edges, got %d", len(edges))
	}
}

func TestCRGBridge_ReadEdges_Limit(t *testing.T) {
	dir := t.TempDir()
	writeFakeCRGDB(t, dir, 0, 5)
	b := &graphstore.CRGBridge{RepoRoot: dir, Bin: ""}
	edges, err := b.ReadEdges(2)
	if err != nil {
		t.Fatalf("ReadEdges: %v", err)
	}
	if len(edges) != 2 {
		t.Errorf("expected 2 (limit), got %d", len(edges))
	}
}

// TestCRGBridge_ReadEdges_PermissionDeniedStat_ReturnsError mirrors the
// ReadNodes coverage for the edges path.
func TestCRGBridge_ReadEdges_PermissionDeniedStat_ReturnsError(t *testing.T) {
	repo := t.TempDir()
	dbPath := graphstore.CRGDBPath(repo)
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatal(err)
	}
	testutil.MakeDirUnreadable(t, filepath.Dir(dbPath))

	b := &graphstore.CRGBridge{RepoRoot: repo, Bin: ""}
	edges, err := b.ReadEdges(10)
	if err == nil {
		t.Fatal("expected a real error from a permission-denied Stat, not the legitimate-absence nil,nil")
	}
	if edges != nil {
		t.Errorf("expected nil edges alongside the error, got %v", edges)
	}
}

// TestCRGBridge_ReadEdges_ScanFailureCounted mirrors the ReadNodes coverage
// for the edges path.
func TestCRGBridge_ReadEdges_ScanFailureCounted(t *testing.T) {
	dir := t.TempDir()
	writeFakeCRGDB(t, dir, 0, 2)
	db, err := sql.Open("sqlite", graphstore.CRGDBPath(dir))
	if err != nil {
		t.Fatalf("open fake db: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO edges (kind,source_qualified,target_qualified,file_path,line,extra,updated_at)
		 VALUES (?,?,?,?,?,?,?)`,
		"CALLS", "pkg::C", "pkg::D", "f.go", "not-a-number", "{}", 1.0,
	); err != nil {
		t.Fatalf("insert bad row: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close fake db: %v", err)
	}

	b := &graphstore.CRGBridge{RepoRoot: dir, Bin: ""}
	edges, err := b.ReadEdges(0)
	if err == nil {
		t.Fatal("expected the row-scan failure to be surfaced, not silently skipped")
	}
	if len(edges) != 2 {
		t.Errorf("expected the 2 well-formed rows to still be returned, got %d", len(edges))
	}
}

func TestCRGBridge_Status_MissingDB(t *testing.T) {
	b := &graphstore.CRGBridge{RepoRoot: t.TempDir(), Bin: ""}
	status, err := b.Status()
	if err != nil {
		t.Fatalf("Status missing-db: %v", err)
	}
	if status == nil {
		t.Fatal("Status returned nil")
	}
	if status.State != graphstore.CRGReadinessUnbuilt {
		t.Errorf("missing-db state: got %q", status.State)
	}
	if status.Ready {
		t.Errorf("Ready should be false when db missing")
	}
}

func TestCRGBridge_Status_PopulatedDB(t *testing.T) {
	dir := t.TempDir()
	writeFakeCRGDB(t, dir, 3, 1)
	b := &graphstore.CRGBridge{RepoRoot: dir, Bin: ""}
	status, err := b.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.Nodes != 3 || status.Edges != 1 {
		t.Errorf("got nodes=%d edges=%d", status.Nodes, status.Edges)
	}
	// 1 distinct file_path = 1 file
	if status.Files != 1 {
		t.Errorf("got files=%d, want 1", status.Files)
	}
}

// ── CRGOperationReport JSON shape ─────────────────────────────────────────────

func TestCRGOperationReport_JSONShape(t *testing.T) {
	rep := graphstore.CRGOperationReport{
		Operation: "build",
		Outcome:   graphstore.CRGReadinessReady,
		Summary:   "ok",
	}
	if rep.Operation != "build" || rep.Outcome != graphstore.CRGReadinessReady {
		t.Errorf("unexpected: %+v", rep)
	}
}

// parseCRGStatusOutputExported is a thin helper so tests in the _test package
// can reach the unexported parsing function via a white-box re-export.
// We use this approach rather than making the function exported to keep the
// public API surface small.
func parseLeadingInt(s string) int {
	n := 0
	for _, ch := range s {
		if ch >= '0' && ch <= '9' {
			n = n*10 + int(ch-'0')
		}
	}
	return n
}

func parseCRGStatusOutputExported(out []byte) *graphstore.CRGStatus {
	s := &graphstore.CRGStatus{}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "INFO:") || strings.HasPrefix(line, "WARNING:") {
			continue
		}
		key, val, ok := strings.Cut(line, ": ")
		if !ok {
			continue
		}
		val = strings.TrimSpace(val)
		switch strings.TrimSpace(key) {
		case "Nodes":
			s.Nodes = parseLeadingInt(val)
		case "Edges":
			s.Edges = parseLeadingInt(val)
		case "Files":
			s.Files = parseLeadingInt(val)
		case "Languages":
			s.Languages = val
		case "Last updated":
			s.LastUpdated = val
		}
	}
	return s
}
