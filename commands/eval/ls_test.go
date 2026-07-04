package eval

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/eval/store"
	"github.com/spf13/cobra"
)

// writeRunSidecar creates <root>/.agents/eval/runs/<runID>/eval-run.yaml with
// body, materialising a listable run.
func writeRunSidecar(t *testing.T, root, runID, body string) {
	t.Helper()
	dir := store.RunDir(root, runID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir run dir: %v", err)
	}
	writeFile(t, filepath.Join(dir, evalRunFile), body)
}

func goRunSidecar(runID, band string, value float64, scored, verified bool) string {
	return strings.Join([]string{
		"run_id: " + runID,
		"language: go",
		"difficulty: easy",
		"agent:",
		"  harness: fake-harness",
		"verify:",
		"  passed: " + strconv.FormatBool(verified),
		"score:",
		"  value: " + strconv.FormatFloat(value, 'g', -1, 64),
		"  band: " + band,
		"  scored: " + strconv.FormatBool(scored),
		"",
	}, "\n")
}

func TestEvalRunsRoot(t *testing.T) {
	got := evalRunsRoot("/repo")
	want := filepath.Join("/repo", ".agents", "eval", "runs")
	if got != want {
		t.Errorf("evalRunsRoot = %q, want %q", got, want)
	}
}

// No runs root yet renders the friendly first-use notice, not an error.
func TestRunLsMissingRoot(t *testing.T) {
	var buf bytes.Buffer
	if err := runLs(&buf, t.TempDir(), false); err != nil {
		t.Fatalf("runLs: %v", err)
	}
	if !strings.Contains(buf.String(), "no runs found") {
		t.Errorf("missing empty-state notice: %q", buf.String())
	}
}

// An empty runs root (present but with no runs) also renders the notice.
func TestRunLsEmptyRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(evalRunsRoot(root), 0o755); err != nil {
		t.Fatalf("mkdir runs root: %v", err)
	}
	var buf bytes.Buffer
	if err := runLs(&buf, root, false); err != nil {
		t.Fatalf("runLs: %v", err)
	}
	if !strings.Contains(buf.String(), "no runs found") {
		t.Errorf("missing empty-state notice: %q", buf.String())
	}
}

func TestRunLsListsRunsSorted(t *testing.T) {
	root := t.TempDir()
	writeRunSidecar(t, root, "eval-zzz", goRunSidecar("eval-zzz", "good", 0.812, true, true))
	writeRunSidecar(t, root, "eval-aaa", goRunSidecar("eval-aaa", "fair", 0.5, true, false))

	var buf bytes.Buffer
	if err := runLs(&buf, root, false); err != nil {
		t.Fatalf("runLs: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"eval-aaa", "eval-zzz", "go", "good", "fair", "0.812", "pass", "fail"} {
		if !strings.Contains(out, want) {
			t.Errorf("listing missing %q in:\n%s", want, out)
		}
	}
	// Sorted ascending: eval-aaa must precede eval-zzz.
	if strings.Index(out, "eval-aaa") > strings.Index(out, "eval-zzz") {
		t.Errorf("runs not sorted by id:\n%s", out)
	}
}

// Non-directories, dirs without a sidecar, and malformed sidecars are skipped.
func TestRunLsSkipsInvalidEntries(t *testing.T) {
	root := t.TempDir()
	runsDir := evalRunsRoot(root)
	writeRunSidecar(t, root, "eval-good", goRunSidecar("eval-good", "good", 0.812, true, true))
	// a bare file at the runs root
	writeFile(t, filepath.Join(runsDir, "stray.txt"), "ignore me")
	// a dir with no sidecar
	if err := os.MkdirAll(filepath.Join(runsDir, "eval-nosidecar"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// a dir with a malformed sidecar
	writeRunSidecar(t, root, "eval-bad", "this: is: not: valid: yaml")

	var buf bytes.Buffer
	if err := runLs(&buf, root, false); err != nil {
		t.Fatalf("runLs: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "eval-good") {
		t.Errorf("valid run missing:\n%s", out)
	}
	for _, skip := range []string{"eval-nosidecar", "stray", "eval-bad"} {
		if strings.Contains(out, skip) {
			t.Errorf("invalid entry %q leaked into listing:\n%s", skip, out)
		}
	}
}

func TestRunLsJSON(t *testing.T) {
	root := t.TempDir()
	writeRunSidecar(t, root, "eval-json", goRunSidecar("eval-json", "good", 0.812, true, true))

	var buf bytes.Buffer
	if err := runLs(&buf, root, true); err != nil {
		t.Fatalf("runLs json: %v", err)
	}
	var got []lsRecordJSON
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("decode json: %v\n%s", err, buf.String())
	}
	if len(got) != 1 || got[0].RunID != "eval-json" || !got[0].Score.Scored {
		t.Fatalf("json listing = %+v", got)
	}
}

// Empty JSON listing marshals to [] (never null).
func TestRunLsJSONEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := runLs(&buf, t.TempDir(), true); err != nil {
		t.Fatalf("runLs json: %v", err)
	}
	if strings.TrimSpace(buf.String()) != "[]" {
		t.Errorf("empty json listing = %q, want []", buf.String())
	}
}

func TestLsScoreCol(t *testing.T) {
	if got := lsScoreCol(lsScore{Scored: false}); got != "-" {
		t.Errorf("unscored col = %q, want -", got)
	}
	if got := lsScoreCol(lsScore{Scored: true, Value: 0.5}); got != "0.500" {
		t.Errorf("scored col = %q, want 0.500", got)
	}
}

func TestReadRunRecordMissing(t *testing.T) {
	if _, ok := readRunRecord(t.TempDir()); ok {
		t.Error("readRunRecord should report false for a dir without a sidecar")
	}
}

// The ls command Execute path via the real RunLs entry point covers RunLs +
// flag wiring. A JSON pass also exercises the asJSON thread-through.
func TestLsCommandExecute(t *testing.T) {
	root := t.TempDir()
	writeRunSidecar(t, root, "eval-exec", goRunSidecar("eval-exec", "good", 0.812, true, true))
	cmd := newLsCmd(func(c *cobra.Command, _ []string) error { return RunLs(c, false) })
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--repo-dir", root})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("ls Execute: %v", err)
	}
	if !strings.Contains(buf.String(), "eval-exec") {
		t.Errorf("ls Execute output missing run:\n%s", buf.String())
	}
}

// RunLs with asJSON=true routes through the JSON renderer.
func TestRunLsEntryJSON(t *testing.T) {
	root := t.TempDir()
	writeRunSidecar(t, root, "eval-jsonentry", goRunSidecar("eval-jsonentry", "good", 0.812, true, true))
	cmd := newLsCmd(func(c *cobra.Command, _ []string) error { return RunLs(c, true) })
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--repo-dir", root})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("ls Execute json: %v", err)
	}
	if !strings.Contains(buf.String(), "\"run_id\": \"eval-jsonentry\"") {
		t.Errorf("ls json output missing run_id:\n%s", buf.String())
	}
}
