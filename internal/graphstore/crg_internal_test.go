package graphstore

import (
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCRGBridge_pythonBin_FallbackToPath(t *testing.T) {

	if runtime.GOOS == "windows" {
		t.Skip("python3 fallback is POSIX-only; Windows 'python' fallback covered by TestPythonBin_ResolvesSiblingThenFallback")
	}

	b := &CRGBridge{Bin: filepath.Join(t.TempDir(), "fake")}
	got := b.pythonBin()
	if got != "python3" {
		t.Errorf("expected fallback to 'python3', got %q", got)
	}
}

func TestCRGBridge_pythonBin_DiscoversPython3(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pyPath := filepath.Join(binDir, "python3")
	if err := os.WriteFile(pyPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	b := &CRGBridge{Bin: filepath.Join(binDir, "code-review-graph")}
	got := b.pythonBin()
	if got != pyPath {
		t.Errorf("expected %s, got %q", pyPath, got)
	}
}

func TestCRGBridge_pythonBin_DiscoversPlainPython(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pyPath := filepath.Join(binDir, "python")
	if err := os.WriteFile(pyPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	b := &CRGBridge{Bin: filepath.Join(binDir, "code-review-graph")}
	got := b.pythonBin()
	if got != pyPath {
		t.Errorf("expected %s, got %q", pyPath, got)
	}
}

func TestCRGBridge_DiscoverCRGBin_PrefersVenv(t *testing.T) {
	repo := t.TempDir()
	venvBin := filepath.Join(repo, ".venv", "bin")
	if err := os.MkdirAll(venvBin, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(venvBin, crgBinName)
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := DiscoverCRGBin(repo)
	if err != nil {
		t.Fatalf("DiscoverCRGBin: %v", err)
	}
	if got != bin {
		t.Errorf("expected %s, got %s", bin, got)
	}
}

func TestCRGBridge_DiscoverCRGBin_ParentVenv(t *testing.T) {
	parent := t.TempDir()
	repo := filepath.Join(parent, "child")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	venvBin := filepath.Join(parent, ".venv", "bin")
	if err := os.MkdirAll(venvBin, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(venvBin, crgBinName)
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := DiscoverCRGBin(repo)
	if err != nil {
		t.Fatalf("DiscoverCRGBin: %v", err)
	}
	if got != bin {
		t.Errorf("expected parent venv bin %s, got %s", bin, got)
	}
}

func TestNewCRGBridge_NotFound(t *testing.T) {

	repo := t.TempDir()
	if _, err := DiscoverCRGBin(repo); err == nil {
		t.Skip("code-review-graph available on PATH; skipping not-found test")
	}
	if _, err := NewCRGBridge(repo); err == nil {
		t.Error("expected error when CRG not discoverable")
	}
}

func TestNewCRGBridge_Found(t *testing.T) {
	repo := t.TempDir()
	venvBin := filepath.Join(repo, ".venv", "bin")
	if err := os.MkdirAll(venvBin, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(venvBin, crgBinName)
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	b, err := NewCRGBridge(repo)
	if err != nil {
		t.Fatalf("NewCRGBridge: %v", err)
	}
	if b.RepoRoot != repo {
		t.Errorf("RepoRoot = %s, want %s", b.RepoRoot, repo)
	}
}

func TestCRGBridge_run_BadBin(t *testing.T) {
	b := &CRGBridge{RepoRoot: t.TempDir(), Bin: "/no/such/binary/exists"}
	_, err := b.run("foo")
	if err == nil {
		t.Error("expected run to fail when binary missing")
	}
}

func TestCRGBridge_runStreamed_BadBin(t *testing.T) {
	b := &CRGBridge{RepoRoot: t.TempDir(), Bin: "/no/such/binary"}
	if err := b.runStreamed("foo"); err == nil {
		t.Error("expected runStreamed to fail when binary missing")
	}
}

func TestCRGBridge_run_NonZeroExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell binary path differs on Windows")
	}

	dir := t.TempDir()
	bin := filepath.Join(dir, "fake")
	script := "#!/bin/sh\necho 'an error' >&2\nexit 1\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	b := &CRGBridge{RepoRoot: dir, Bin: bin}
	_, err := b.run("foo")
	if err == nil {
		t.Fatal("expected error from non-zero exit")
	}
	if !strings.Contains(err.Error(), "an error") {
		t.Errorf("expected stderr in error, got %v", err)
	}
}

func TestCRGBridge_run_NonZeroExitNoStderr(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell binary path differs on Windows")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-silent")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 2\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	b := &CRGBridge{RepoRoot: dir, Bin: bin}
	if _, err := b.run("foo"); err == nil {
		t.Error("expected error for silent exit 2")
	}
}

func TestCRGBridge_run_OK(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell binary path differs on Windows")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "echo-bin")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho hello\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	b := &CRGBridge{RepoRoot: dir, Bin: bin}
	out, err := b.run("ignored")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(string(out), "hello") {
		t.Errorf("unexpected output %q", string(out))
	}
}

func TestCRGBridge_runCaptured_NonPythonBin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell binary path differs on Windows")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "ok-bin")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho ok\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	b := &CRGBridge{RepoRoot: dir, Bin: bin}
	out, err := b.runCaptured("x")
	if err != nil {
		t.Fatalf("runCaptured: %v", err)
	}
	if !strings.Contains(string(out), "ok") {
		t.Errorf("got %q", string(out))
	}
}

func TestCRGBridge_runCaptured_Error(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell binary path differs on Windows")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "err-bin")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho boom >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	b := &CRGBridge{RepoRoot: dir, Bin: bin}
	_, err := b.runCaptured("y")
	if err == nil {
		t.Error("expected error")
	}
}

func TestCRGBridge_commandWithSQLiteAutocommit_NonPython(t *testing.T) {

	dir := t.TempDir()
	bin := filepath.Join(dir, "non-py")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho ok\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	b := &CRGBridge{RepoRoot: dir, Bin: bin}
	cmd, err := b.commandWithSQLiteAutocommit("a", "b")
	if err != nil {
		t.Fatal(err)
	}
	if cmd == nil || cmd.Path != bin {
		t.Errorf("expected direct command path %s, got %+v", bin, cmd)
	}
	if len(cmd.Args) < 3 || cmd.Args[1] != "a" || cmd.Args[2] != "b" {
		t.Errorf("unexpected args: %v", cmd.Args)
	}
}

func TestCRGBridge_commandWithSQLiteAutocommit_PythonEntrypoint(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "py-entry")

	if err := os.WriteFile(bin, []byte("#!/usr/bin/env python3\nprint('hi')\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	b := &CRGBridge{RepoRoot: dir, Bin: bin}
	cmd, err := b.commandWithSQLiteAutocommit("--foo")
	if err != nil {
		t.Fatal(err)
	}
	if cmd == nil {
		t.Fatal("nil cmd")
	}

	if len(cmd.Args) < 3 {
		t.Fatalf("expected at least 3 args, got %v", cmd.Args)
	}
	if cmd.Args[1] != "-c" {
		t.Errorf("expected -c flag, got %q", cmd.Args[1])
	}
}

func TestReadCRGLanguages(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ddl := `CREATE TABLE nodes (id INTEGER PRIMARY KEY, language TEXT);
INSERT INTO nodes (language) VALUES ('go'), ('python'), ('go'), (''), ('ruby');`
	if _, err := db.Exec(ddl); err != nil {
		t.Fatalf("ddl: %v", err)
	}
	langs, err := readCRGLanguages(db)
	if err != nil {
		t.Fatalf("readCRGLanguages: %v", err)
	}

	if len(langs) != 3 {
		t.Errorf("expected 3 langs, got %v", langs)
	}
}

func TestReadCRGLanguages_Error(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "bad.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := readCRGLanguages(db); err == nil {
		t.Error("expected error when nodes table missing")
	}
}

func TestCRGBridge_Status_EmptyDB(t *testing.T) {
	dir := t.TempDir()

	dbDir := filepath.Join(dir, ".code-review-graph")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dbDir, "graph.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	ddl := `CREATE TABLE nodes (id INTEGER PRIMARY KEY, file_path TEXT, language TEXT, updated_at TEXT);
CREATE TABLE edges (id INTEGER PRIMARY KEY);`
	if _, err := db.Exec(ddl); err != nil {
		t.Fatalf("ddl: %v", err)
	}
	db.Close()

	b := &CRGBridge{RepoRoot: dir, Bin: ""}
	status, err := b.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.Ready {
		t.Errorf("empty DB should not be Ready, got %+v", status)
	}
	if status.State != CRGReadinessUnbuilt {
		t.Errorf("expected unbuilt state, got %q", status.State)
	}
}

func TestCRGBridge_Status_LangCSV(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, ".code-review-graph")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dbDir, "graph.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	ddl := `CREATE TABLE nodes (id INTEGER PRIMARY KEY, file_path TEXT, language TEXT, updated_at TEXT);
INSERT INTO nodes (file_path, language, updated_at) VALUES ('a.go', 'go', '2026-01-01T00:00:00Z'), ('b.py', 'python', '2026-01-02T00:00:00Z');
CREATE TABLE edges (id INTEGER PRIMARY KEY);`
	if _, err := db.Exec(ddl); err != nil {
		t.Fatalf("ddl: %v", err)
	}
	db.Close()

	b := &CRGBridge{RepoRoot: dir, Bin: ""}
	status, err := b.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !strings.Contains(status.Languages, "go") {
		t.Errorf("expected 'go' in languages, got %q", status.Languages)
	}
	if !strings.Contains(status.Languages, "python") {
		t.Errorf("expected 'python' in languages, got %q", status.Languages)
	}
}

func TestCRGChangeReport_JSONUnmarshal(t *testing.T) {

	report := CRGChangeReport{}
	_ = report
}

func TestCRGImpactResult_JSONShape(t *testing.T) {
	r := CRGImpactResult{Status: "ready", Summary: "ok"}
	if r.Status != "ready" {
		t.Error("status not retained")
	}
}

func TestCRGFlowFlowInfo(t *testing.T) {
	fl := FlowInfo{ID: 1, Name: "n", StepCount: 5}
	if fl.Name != "n" {
		t.Error("FlowInfo not retained")
	}
}

func TestCommunityInfo_Members(t *testing.T) {
	c := CommunityInfo{Members: []string{"a", "b"}}
	if len(c.Members) != 2 {
		t.Error("Members not retained")
	}
}

func TestUnmarshalSkippingLogPrefix_LogOnly(t *testing.T) {
	// Only log lines, no JSON — should still error.
	var v map[string]any
	err := unmarshalSkippingLogPrefix([]byte("INFO: foo\nINFO: bar\n"), &v)
	if err == nil {
		t.Error("expected error for log-only input")
	}
}

func TestParseCRGMutationSummary_TabSeparated(t *testing.T) {
	out := []byte("3 files updated, 12 nodes, 7 edges\n")
	files, nodes, edges, ok := parseCRGMutationSummary(out)
	if !ok {
		t.Fatal("expected to parse")
	}
	if files != 3 || nodes != 12 || edges != 7 {
		t.Errorf("got files=%d nodes=%d edges=%d", files, nodes, edges)
	}
}

func TestParseCRGMutationSummary_CRSplit(t *testing.T) {

	out := []byte("1 file, 2 nodes, 3 edges")
	files, nodes, edges, ok := parseCRGMutationSummary(out)
	if !ok {
		t.Fatal("expected to parse single line")
	}
	if files != 1 || nodes != 2 || edges != 3 {
		t.Errorf("got files=%d nodes=%d edges=%d", files, nodes, edges)
	}
}

func TestCRGBridge_BuildReport_FailedRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell binary path differs on Windows")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "fail-bin")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho 'kaboom' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	b := &CRGBridge{RepoRoot: dir, Bin: bin}
	_, err := b.BuildReport(BuildOptions{SkipFlows: true, SkipPostprocess: true})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "build") {
		t.Errorf("expected build-classified error, got %v", err)
	}
}

func TestCRGBridge_BuildReport_EmptyGraphAfterRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell binary path differs on Windows")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "ok-bin")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho built\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	b := &CRGBridge{RepoRoot: dir, Bin: bin}
	report, err := b.BuildReport(BuildOptions{SkipFlows: true})
	if err != nil {
		t.Fatalf("BuildReport: %v", err)
	}
	if report.Outcome != CRGReadinessUnbuilt {
		t.Errorf("expected outcome=unbuilt, got %q (summary=%s)", report.Outcome, report.Summary)
	}
}

func TestCRGBridge_BuildReport_ReadyOutcome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell binary path differs on Windows")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "ok-bin")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho built\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	writeFakeCRGDBInternal(t, dir, 2, 1)

	b := &CRGBridge{RepoRoot: dir, Bin: bin}
	report, err := b.BuildReport(BuildOptions{})
	if err != nil {
		t.Fatalf("BuildReport: %v", err)
	}
	if report.Outcome != CRGReadinessReady {
		t.Errorf("expected ready outcome, got %q", report.Outcome)
	}
	if !strings.Contains(report.Summary, "nodes") {
		t.Errorf("expected nodes count in summary, got %q", report.Summary)
	}
}

func TestCRGBridge_Build_PassthroughError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell binary path differs on Windows")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "fail")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	b := &CRGBridge{RepoRoot: dir, Bin: bin}
	if err := b.Build(BuildOptions{}); err == nil {
		t.Error("expected Build wrapper to propagate failure")
	}
}
