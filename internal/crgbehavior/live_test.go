package crgbehavior

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeCRGRepo stages a repository whose .venv looks like a code-review-graph
// install: DiscoverCRGBin finds the CLI there and the bridge's query path runs
// the sibling python3. pyBody is that interpreter's canned behavior, so the
// real subprocess plumbing (not a stub) is exercised.
func fakeCRGRepo(t *testing.T, pyBody string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake venv uses a POSIX shell interpreter; the merged multi-OS profile covers this on Linux/macOS")
	}
	repo := t.TempDir()
	bin := filepath.Join(repo, ".venv", "bin")
	if err := os.MkdirAll(bin, 0o750); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o700); err != nil { //nolint:gosec // test fixture
			t.Fatal(err)
		}
	}
	write("code-review-graph", "#!/bin/sh\nexit 0\n")
	write("python3", pyBody)
	return repo
}

// impactJSON is a recorded legacy impact-radius answer in the bridge's own
// absolute-path spelling.
const impactJSON = `{"status":"ok","changed_nodes":[` +
	`{"qualified_name":"REPO/a.go::Entry","file_path":"REPO/a.go"}],` +
	`"impacted_nodes":[{"qualified_name":"REPO/b.go::Widget","file_path":"REPO/b.go"}],` +
	`"truncated":true}`

func TestNewLiveBridgeReportsAMissingLegacyCLI(t *testing.T) {
	empty := t.TempDir()
	t.Setenv("PATH", empty)
	if _, err := NewLiveBridge(empty); !errors.Is(err, ErrBridgeUnavailable) {
		t.Fatal("no code-review-graph anywhere must report ErrBridgeUnavailable so the gate SKIPS")
	}
}

func TestLiveBridgeNormalizesTheLegacyImpactAnswer(t *testing.T) {
	repo := fakeCRGRepo(t, "#!/bin/sh\ncat <<'EOF'\n"+strings.ReplaceAll(impactJSON, "REPO", "/abs/repo")+"\nEOF\n")
	live, err := NewLiveBridge(repo)
	if err != nil {
		t.Fatalf("bind bridge: %v", err)
	}
	live.repoRoot = "/abs/repo" // the graph was built under this root
	got, err := live.ImpactRadius([]string{"a.go"}, 2, 100)
	if err != nil {
		t.Fatalf("impact: %v", err)
	}
	if len(got.ChangedIDs) != 1 || got.ChangedIDs[0] != "a.go::Entry@a.go" {
		t.Fatalf("changed ids = %v, want the kg-native id space", got.ChangedIDs)
	}
	if len(got.ImpactedIDs) != 1 || got.ImpactedIDs[0] != "b.go::Widget@b.go" {
		t.Fatalf("impacted ids = %v, want the kg-native id space", got.ImpactedIDs)
	}
	if !got.Truncated {
		t.Fatal("the bridge's own truncation flag must be carried into the report")
	}
}

func TestLiveBridgePropagatesAQueryFailure(t *testing.T) {
	repo := fakeCRGRepo(t, "#!/bin/sh\necho 'boom' >&2\nexit 1\n")
	live, err := NewLiveBridge(repo)
	if err != nil {
		t.Fatalf("bind bridge: %v", err)
	}
	if _, err := live.ImpactRadius([]string{"a.go"}, 2, 100); err == nil {
		t.Fatal("a failing legacy query must be an error, not an empty answer")
	}
}

func TestLiveBridgeViewsRequireABuiltGraph(t *testing.T) {
	repo := fakeCRGRepo(t, "#!/bin/sh\nexit 0\n")
	live, err := NewLiveBridge(repo)
	if err != nil {
		t.Fatalf("bind bridge: %v", err)
	}
	if _, err := live.Views(); !errors.Is(err, ErrBridgeUnavailable) {
		t.Fatal("a repository with no built graph must report ErrBridgeUnavailable")
	}
}

func TestRunLiveEndToEndOverAStagedBridge(t *testing.T) {
	repo := fakeCRGRepo(t, "#!/bin/sh\ncat <<'EOF'\n"+strings.ReplaceAll(impactJSON, "REPO", "REPOROOT")+"\nEOF\n")
	stageGraph(t, repo)
	// The staged graph's absolute prefix is the temp repo itself, so the
	// recorded answer is rewritten to match.
	body, err := os.ReadFile(filepath.Join(repo, ".venv", "bin", "python3"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".venv", "bin", "python3"),
		[]byte(strings.ReplaceAll(string(body), "REPOROOT", repo)), 0o700); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}

	cfg := Config{
		RepoRoot: repo,
		Manifest: Manifest{
			SchemaVersion: ManifestSchemaVersion,
			Head:          "headsha0",
			Tasks:         []Task{{Commit: "6666666666666666", ChangedFiles: []string{"a.go"}}},
		},
	}
	report, err := RunLive(cfg, repo)
	if err != nil {
		t.Fatalf("run live: %v", err)
	}
	if report.ExecutedTasks() != 1 {
		t.Fatalf("the staged task must execute: %+v", report.Tasks)
	}
	if s := surfaceNamed(t, report, SurfaceChangedNodes); !s.Pass {
		t.Fatalf("both sides resolved the same seed symbol, want PASS: %+v", s)
	}
}

func TestRunLiveReportsAnUnavailableBridge(t *testing.T) {
	empty := t.TempDir()
	t.Setenv("PATH", empty)
	if _, err := RunLive(Config{}, empty); !errors.Is(err, ErrBridgeUnavailable) {
		t.Fatal("RunLive must surface ErrBridgeUnavailable so callers SKIP")
	}
}

// stageGraph writes a legacy-shaped graph.db under repo's .code-review-graph.
func stageGraph(t *testing.T, repo string) {
	t.Helper()
	dir := filepath.Join(repo, ".code-review-graph")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	seed := strings.ReplaceAll(`
INSERT INTO nodes VALUES
 (1,'Function','Entry','ROOT/a.go::Entry','ROOT/a.go',1,'go','h',5),
 (2,'Type','Widget','ROOT/b.go::Widget','ROOT/b.go',1,'go','h',5);
INSERT INTO edges VALUES (1,'CALLS','ROOT/a.go::Entry','ROOT/b.go::Widget');
INSERT INTO nodes_fts VALUES ('ROOT/a.go::Entry'),('ROOT/b.go::Widget');
`, "ROOT", repo)
	src := newBridgeDB(t, seed)
	data, err := os.ReadFile(src) //nolint:gosec // test fixture
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "graph.db"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}
