package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/crgbehavior"
	"golang.org/x/sys/execabs"

	// _ "modernc.org/sqlite": registers the driver the staged legacy store uses.
	_ "modernc.org/sqlite"
)

// run drives mainRun and returns its exit code plus both streams.
func run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := mainRun(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestUsageErrorOnAnUnknownFlag(t *testing.T) {
	code, _, stderr := run(t, "-nope")
	if code != exitError || !strings.Contains(stderr, "flag provided but not defined") {
		t.Fatalf("code = %d, stderr = %q, want a usage error", code, stderr)
	}
}

func TestRegenPinsTheCorpusManifest(t *testing.T) {
	repo := newGitRepo(t)
	out := filepath.Join(t.TempDir(), "manifest.json")
	code, stdout, stderr := run(t, "-regen", "-repo", repo, "-ref", "main", "-commits", "5", "-manifest", out)
	if code != exitPass {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "review task(s) pinned from main") {
		t.Fatalf("stdout = %q, want the pinned-corpus summary", stdout)
	}
	m, err := crgbehavior.LoadManifest(out)
	if err != nil {
		t.Fatalf("the regenerated manifest must load: %v", err)
	}
	if len(m.Tasks) == 0 || m.Tasks[0].ChangedFiles[0] != "pkg/a.go" {
		t.Fatalf("manifest = %+v, want the code commit pinned", m)
	}
}

func TestRegenReportsAnUnknownRefAndAnUnwritableManifest(t *testing.T) {
	repo := newGitRepo(t)
	if code, _, stderr := run(t, "-regen", "-repo", repo, "-ref", "absent"); code != exitError ||
		!strings.Contains(stderr, "git rev-parse") {
		t.Fatalf("code = %d, stderr = %q, want the git failure", code, stderr)
	}
	dir := t.TempDir()
	if code, _, _ := run(t, "-regen", "-repo", repo, "-ref", "main", "-manifest", dir); code != exitError {
		t.Fatal("writing the manifest over a directory must be an error")
	}
}

func TestGateReportsAMissingManifest(t *testing.T) {
	code, _, stderr := run(t, "-manifest", filepath.Join(t.TempDir(), "absent.json"))
	if code != exitError || !strings.Contains(stderr, "read manifest") {
		t.Fatalf("code = %d, stderr = %q, want the manifest load failure", code, stderr)
	}
}

func TestGateSkipsWhenTheLegacyBridgeIsUnavailable(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir) // no code-review-graph anywhere
	code, stdout, _ := run(t, "-manifest", writeManifest(t, dir), "-repo", dir, "-graph-repo", dir)
	if code != exitPass {
		t.Fatalf("code = %d, want a SKIP (an absent legacy bridge is not a divergence)", code)
	}
	if !strings.Contains(stdout, "SKIP:") || !strings.Contains(stdout, "BEHAVIOR.md") {
		t.Fatalf("stdout = %q, want a skip notice pointing at the docs", stdout)
	}
}

func TestGateSurfacesAPlumbingFailure(t *testing.T) {
	dir := t.TempDir()
	stageBrokenGraph(t, dir)
	code, _, stderr := run(t, "-manifest", writeManifest(t, dir), "-repo", dir, "-graph-repo", dir)
	if code != exitError || stderr == "" {
		t.Fatalf("code = %d, stderr = %q, want a plumbing error", code, stderr)
	}
}

func TestGateRendersAReportOverAStagedBridge(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the staged bridge uses a POSIX shell interpreter; the merged multi-OS profile covers this elsewhere")
	}
	dir := t.TempDir()
	stageWorkingBridge(t, dir)
	code, stdout, stderr := run(t, "-manifest", writeManifest(t, dir), "-repo", dir, "-graph-repo", dir)
	if code != exitPass {
		t.Fatalf("code = %d, stderr = %q, want PASS over the staged bridge:\n%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "§11.4 criterion 2") || !strings.Contains(stdout, "GATE: PASS") {
		t.Fatalf("stdout = %q, want the rendered gate report", stdout)
	}
}

func TestGateExitsNonZeroOnAGatingDivergence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the staged bridge uses a POSIX shell interpreter; the merged multi-OS profile covers this elsewhere")
	}
	dir := t.TempDir()
	stageWorkingBridge(t, dir)
	// The legacy side claims a symbol the kg-native namespace does not hold.
	divergent := `{"changed_nodes":[{"qualified_name":"` + dir + `/a.go::Ghost","file_path":"` + dir + `/a.go"}],` +
		`"impacted_nodes":[],"truncated":false}`
	writeExe(t, filepath.Join(dir, ".venv", "bin", "python3"), "#!/bin/sh\ncat <<'EOF'\n"+divergent+"\nEOF\n")
	code, stdout, _ := run(t, "-manifest", writeManifest(t, dir), "-repo", dir, "-graph-repo", dir)
	if code != exitFail {
		t.Fatalf("code = %d, want a gating-divergence failure:\n%s", code, stdout)
	}
	if !strings.Contains(stdout, "FAIL  changed_nodes") || !strings.Contains(stdout, "Ghost") {
		t.Fatalf("the report must name the diverging query and symbol:\n%s", stdout)
	}
}

// stageWorkingBridge stages a fake code-review-graph install whose query
// interpreter returns a recorded answer, plus a legacy-shaped graph store that
// agrees with it.
func stageWorkingBridge(t *testing.T, dir string) {
	t.Helper()
	bin := filepath.Join(dir, ".venv", "bin")
	if err := os.MkdirAll(bin, 0o750); err != nil {
		t.Fatal(err)
	}
	impact := `{"changed_nodes":[{"qualified_name":"` + dir + `/a.go::A","file_path":"` + dir + `/a.go"}],` +
		`"impacted_nodes":[],"truncated":false}`
	writeExe(t, filepath.Join(bin, "code-review-graph"), "#!/bin/sh\nexit 0\n")
	writeExe(t, filepath.Join(bin, "python3"), "#!/bin/sh\ncat <<'EOF'\n"+impact+"\nEOF\n")
	stageGraphDB(t, dir)
}

func writeExe(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}
}

// stageGraphDB writes a minimal legacy graph store containing one symbol.
func stageGraphDB(t *testing.T, dir string) {
	t.Helper()
	graph := filepath.Join(dir, ".code-review-graph")
	if err := os.MkdirAll(graph, 0o750); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(graph, "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	schema := `
CREATE TABLE nodes (id INTEGER PRIMARY KEY, kind TEXT, name TEXT, qualified_name TEXT,
  file_path TEXT, line_start INTEGER, language TEXT, file_hash TEXT, community_id INTEGER);
CREATE TABLE edges (id INTEGER PRIMARY KEY, kind TEXT, source_qualified TEXT, target_qualified TEXT);
CREATE TABLE flow_memberships (flow_id INTEGER, node_id INTEGER, position INTEGER);
CREATE TABLE risk_index (node_id INTEGER, risk_score REAL);
CREATE TABLE nodes_fts (qualified_name TEXT);
INSERT INTO nodes VALUES (1,'Function','A','` + dir + `/a.go::A','` + dir + `/a.go',1,'go','h',3);
INSERT INTO nodes_fts VALUES ('` + dir + `/a.go::A');
`
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
}

// writeManifest stages a minimal pinned corpus and returns its path.
func writeManifest(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "manifest.json")
	data, err := json.Marshal(crgbehavior.Manifest{
		SchemaVersion: crgbehavior.ManifestSchemaVersion,
		Head:          "deadbeef",
		Tasks:         []crgbehavior.Task{{Commit: "deadbeefcafe", ChangedFiles: []string{"a.go"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// stageBrokenGraph puts a fake CRG CLI and an unreadable graph in dir, so the
// gate gets past bridge discovery and fails while reading legacy state.
func stageBrokenGraph(t *testing.T, dir string) {
	t.Helper()
	bin := filepath.Join(dir, ".venv", "bin")
	if err := os.MkdirAll(bin, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "code-review-graph"), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}
	graph := filepath.Join(dir, ".code-review-graph")
	if err := os.MkdirAll(graph, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(graph, "graph.db"), []byte("not a database"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// newGitRepo stages a repository with one code commit.
func newGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "gate@example.com"},
		{"config", "user.name", "Gate Test"},
	} {
		gitOrFail(t, dir, args...)
	}
	if err := os.MkdirAll(filepath.Join(dir, "pkg"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pkg", "a.go"), []byte("package pkg\n\nfunc A() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitOrFail(t, dir, "add", ".")
	gitOrFail(t, dir, "commit", "-m", "feat: add A")
	return dir
}

func gitOrFail(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := execabs.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
}
