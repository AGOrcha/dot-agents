package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	cfg "github.com/AGOrcha/dot-agents/internal/config"
)

// writeLocalLayer writes a layer.json fixture under a local source root and
// returns the source root dir, so a manifest can point a `local` source at it.
func writeLocalLayer(t *testing.T, layerPath, body string) string {
	t.Helper()
	root := t.TempDir()
	full := filepath.Join(root, filepath.FromSlash(layerPath))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir layer dir: %v", err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatalf("write layer: %v", err)
	}
	return root
}

// syncOptions builds a runSyncOptions wired to a force-refresh resolver with a
// fixed clock, so the re-resolve is hermetic (local source, no network) and the
// lock's fetched_at timestamp is deterministic.
func syncOptions(project, layer string, jsonOut bool, clock time.Time) *runSyncOptions {
	return &runSyncOptions{
		jsonOut: jsonOut,
		layer:   layer,
		stdout:  &bytes.Buffer{},
		stderr:  &bytes.Buffer{},
		cwd:     project,
		newResolver: func() resolverSeam {
			return cfg.NewLayeredResolver().
				WithRefresh(true).
				WithClock(func() time.Time { return clock })
		},
	}
}

// readLockFetchedAt returns the fetched_at + resolved_sha recorded for ref in the
// project's lockfile config section, failing the test if the entry is absent.
func readLockFetchedAt(t *testing.T, project, ref string) (string, string) {
	t.Helper()
	locked, err := readLockedConfigSection(project)
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	entry, ok := locked[ref]
	if !ok {
		t.Fatalf("lock has no entry for %q (entries: %v)", ref, locked)
	}
	return entry.FetchedAt, entry.ResolvedSHA
}

const syncManifestTwoLayers = `{
	"version": 2,
	"repo_id": "github.com/acme/app",
	"sources": [{"id": "acme", "type": "local", "path": "%PATH%", "cache_ttl": "4h"}],
	"extends": ["acme:org/base.json", "acme:team/frontend.json"]
}`

// withTwoLocalLayers sets up an isolated project whose manifest extends two
// local-source layers, returning the project root.
func withTwoLocalLayers(t *testing.T) string {
	t.Helper()
	srcRoot := writeLocalLayer(t, "org/base.json", `{"version":2,"skills":["org-skill"]}`)
	// frontend.json lives under the SAME source root.
	if err := os.MkdirAll(filepath.Join(srcRoot, "team"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcRoot, "team", "frontend.json"), []byte(`{"version":2,"skills":["frontend-skill"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	body := replacePath(syncManifestTwoLayers, srcRoot)
	return withRepoLayer(t, body, "")
}

// replacePath substitutes the %PATH% placeholder with a JSON-escaped path.
func replacePath(template, path string) string {
	b, _ := json.Marshal(path)
	escaped := string(b[1 : len(b)-1])
	out := make([]byte, 0, len(template))
	marker := "%PATH%"
	for i := 0; i < len(template); {
		if i+len(marker) <= len(template) && template[i:i+len(marker)] == marker {
			out = append(out, escaped...)
			i += len(marker)
			continue
		}
		out = append(out, template[i])
		i++
	}
	return string(out)
}

func TestRunSync_ReResolvesAndUpdatesLockTimestamps(t *testing.T) {
	project := withTwoLocalLayers(t)

	t1 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := runSync(syncOptions(project, "", false, t1), testDeps()); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	ts1, sha1 := readLockFetchedAt(t, project, "acme:org/base.json")
	if ts1 == "" || sha1 == "" {
		t.Fatalf("expected lock entry populated after first sync, got ts=%q sha=%q", ts1, sha1)
	}
	if want := t1.Format(time.RFC3339); ts1 != want {
		t.Errorf("fetched_at = %q, want %q", ts1, want)
	}

	// A second sync with a LATER clock must rewrite the lock — the timestamp
	// advances and the SHA re-resolves (same content => same SHA, proving the
	// re-resolve actually ran and re-derived it rather than leaving it stale).
	t2 := t1.Add(48 * time.Hour)
	if err := runSync(syncOptions(project, "", false, t2), testDeps()); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	ts2, sha2 := readLockFetchedAt(t, project, "acme:org/base.json")
	if ts2 == ts1 {
		t.Errorf("fetched_at did not advance: still %q after second sync", ts2)
	}
	if want := t2.Format(time.RFC3339); ts2 != want {
		t.Errorf("second fetched_at = %q, want %q", ts2, want)
	}
	if sha2 != sha1 {
		t.Errorf("resolved sha changed for identical content: %q -> %q", sha1, sha2)
	}
}

func TestRunSync_ReportListsAllLayers(t *testing.T) {
	project := withTwoLocalLayers(t)
	report, err := syncReportAfterRun(t, project, "")
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if !report.OK {
		t.Fatalf("expected OK report, got %+v", report)
	}
	if len(report.Layers) != 2 {
		t.Fatalf("expected 2 layers, got %d: %+v", len(report.Layers), report.Layers)
	}
	for _, l := range report.Layers {
		if !l.Targeted {
			t.Errorf("layer %q should be targeted when --layer is empty", l.Ref)
		}
		if l.SHA == "" || l.FetchedAt == "" {
			t.Errorf("layer %q missing sha/fetched_at: %+v", l.Ref, l)
		}
	}
}

func TestRunSync_LayerScopesToOneLayer(t *testing.T) {
	project := withTwoLocalLayers(t)
	report, err := syncReportAfterRun(t, project, "acme:org/base.json")
	if err != nil {
		t.Fatalf("scoped sync: %v", err)
	}
	if report.Layer != "acme:org/base.json" {
		t.Errorf("report.Layer = %q, want acme:org/base.json", report.Layer)
	}
	var targeted, untargeted int
	for _, l := range report.Layers {
		if l.Ref == "acme:org/base.json" {
			if !l.Targeted {
				t.Errorf("targeted layer not marked Targeted: %+v", l)
			}
			targeted++
		} else {
			if l.Targeted {
				t.Errorf("non-targeted layer %q marked Targeted", l.Ref)
			}
			if l.Note == "" {
				t.Errorf("non-targeted layer %q should carry a note", l.Ref)
			}
			untargeted++
		}
	}
	if targeted != 1 || untargeted != 1 {
		t.Errorf("scoping: targeted=%d untargeted=%d, want 1/1", targeted, untargeted)
	}
}

func TestRunSync_UnknownLayerFailsBeforeResolve(t *testing.T) {
	project := withTwoLocalLayers(t)
	err := runSync(syncOptions(project, "acme:does/not-exist.json", false, time.Now()), testDeps())
	if err == nil {
		t.Fatal("expected error for undeclared --layer ref")
	}
}

func TestRunSync_JSONOutputIsStable(t *testing.T) {
	project := withTwoLocalLayers(t)
	opts := syncOptions(project, "", true, time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC))
	buf := &bytes.Buffer{}
	opts.stdout = buf
	if err := runSync(opts, testDeps()); err != nil {
		t.Fatalf("json sync: %v", err)
	}
	var decoded SyncReport
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("sync --json did not emit valid JSON: %v\n%s", err, buf.String())
	}
	if !decoded.OK || len(decoded.Layers) != 2 {
		t.Errorf("decoded report unexpected: %+v", decoded)
	}
}

// syncReportAfterRun runs a sync (human mode) for its side effect of rewriting
// the lock, then rebuilds the report from the project so a test can assert on the
// structured outcome without parsing stdout.
func syncReportAfterRun(t *testing.T, project, layer string) (SyncReport, error) {
	t.Helper()
	if err := runSync(syncOptions(project, layer, false, time.Now().UTC()), testDeps()); err != nil {
		return SyncReport{}, err
	}
	return buildSyncReport(project, layer)
}
