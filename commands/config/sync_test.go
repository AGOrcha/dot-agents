package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
		runContext: runContext{
			jsonOut: jsonOut,
			stdout:  &bytes.Buffer{},
			stderr:  &bytes.Buffer{},
			cwd:     project,
		},
		layer: layer,
		newResolver: func() forceResolver {
			return cfg.NewLayeredResolver().
				WithRefresh(true).
				WithClock(func() time.Time { return clock })
		},
	}
}

// readLockFetchedAt returns the fetched_at + resolved SHA recorded for ref in the
// project's authoritative §7A units section, failing the test if the entry is
// absent. (Post-cutover the SHA is the unit's content Digest.)
func readLockFetchedAt(t *testing.T, project, ref string) (string, string) {
	t.Helper()
	locked, err := readResolvedUnits(project)
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	entry, ok := locked[ref]
	if !ok {
		t.Fatalf("lock has no entry for %q (entries: %v)", ref, locked)
	}
	return entry.FetchedAt, entry.Digest
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

// TestRunSync_ProducesCoherentUnitsLock proves the §7A units-lock wiring reaches
// `da config sync` for free: because Fix #1 lives in LayeredResolver.Resolve (the
// path sync already drives via WithRefresh(true)), sync needs NO change to emit a
// coherent units+inputs_digest lock. After a sync the lockfile carries a units
// section for every extends ref AND a non-empty inputs_digest, and a staleness
// check over the project reports Fresh (no split-brain between what sync wrote
// and what staleness reads).
func TestRunSync_ProducesCoherentUnitsLock(t *testing.T) {
	project := withTwoLocalLayers(t)

	t1 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := runSync(syncOptions(project, "", false, t1), testDeps()); err != nil {
		t.Fatalf("sync: %v", err)
	}

	units, err := cfg.ReadUnits(project)
	if err != nil {
		t.Fatalf("ReadUnits: %v", err)
	}
	if units.InputsDigest == "" {
		t.Errorf("sync must write a non-empty inputs_digest, got empty")
	}
	for _, ref := range []string{"acme:org/base.json", "acme:team/frontend.json"} {
		u, ok := units.Units[ref]
		if !ok {
			t.Errorf("sync did not write a units entry for %q (config/units split-brain)", ref)
			continue
		}
		if u.Kind != cfg.UnitKindLayer || u.Digest == "" {
			t.Errorf("units entry for %q malformed: %+v", ref, u)
		}
	}

	// Staleness over what sync wrote must be Fresh — sync and staleness agree.
	stale, err := cfg.Staleness(project, "", nil)
	if err != nil {
		t.Fatalf("Staleness: %v", err)
	}
	if !stale.Fresh {
		t.Errorf("post-sync lock must be Fresh, got reasons %v", stale.Reasons)
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
			assertTargetedLayer(t, l)
			targeted++
		} else {
			assertUntargetedLayer(t, l)
			untargeted++
		}
	}
	if targeted != 1 || untargeted != 1 {
		t.Errorf("scoping: targeted=%d untargeted=%d, want 1/1", targeted, untargeted)
	}
}

// assertTargetedLayer checks the single --layer-scoped layer is flagged Targeted.
func assertTargetedLayer(t *testing.T, l SyncedLayer) {
	t.Helper()
	if !l.Targeted {
		t.Errorf("targeted layer not marked Targeted: %+v", l)
	}
}

// assertUntargetedLayer checks an out-of-scope layer is not Targeted and carries
// the skip note.
func assertUntargetedLayer(t *testing.T, l SyncedLayer) {
	t.Helper()
	if l.Targeted {
		t.Errorf("non-targeted layer %q marked Targeted", l.Ref)
	}
	if l.Note == "" {
		t.Errorf("non-targeted layer %q should carry a note", l.Ref)
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

// failingResolver is a forceResolver that always returns a transport-style error,
// driving runSync's re-resolve failure branch without any network.
type failingResolver struct{ err error }

func (f failingResolver) Resolve(string) (*cfg.Snapshot, error) { return nil, f.err }

// passingResolver is a no-op forceResolver that reports success without writing
// the lock, so a test can drive the post-resolve report-build failure branch in
// runSync (the resolve succeeds but reading back the project then errors).
type passingResolver struct{}

func (passingResolver) Resolve(string) (*cfg.Snapshot, error) { return nil, nil }

// TestRunSync_ResolveFailureIsHinted injects a resolver whose Resolve errors and
// asserts runSync surfaces a non-nil, hinted error (non-zero exit) rather than
// proceeding to read back a never-written lock.
func TestRunSync_ResolveFailureIsHinted(t *testing.T) {
	project := withTwoLocalLayers(t)
	opts := syncOptions(project, "", false, time.Now().UTC())
	opts.newResolver = func() forceResolver {
		return failingResolver{err: errors.New("boom: remote unreachable")}
	}
	err := runSync(opts, testDeps())
	if err == nil {
		t.Fatal("expected error when the resolver fails")
	}
	if !strings.Contains(err.Error(), "could not re-resolve") {
		t.Errorf("error = %q, want it to mention the re-resolve failure", err.Error())
	}
}

// TestValidateLayerScope_BranchMatrix drives validateLayerScope through its three
// failure paths and the happy path: an unparseable ref, a manifest that cannot be
// loaded, a well-formed-but-undeclared ref, and a declared ref.
func TestValidateLayerScope_BranchMatrix(t *testing.T) {
	declared := withTwoLocalLayers(t)
	noManifest := t.TempDir() // no .agentsrc.json under here

	tests := []struct {
		name    string
		project string
		layer   string
		wantErr string
	}{
		{"unparseable ref", declared, "no-colon", "must be"},
		{"missing manifest", noManifest, "acme:org/base.json", "reading"},
		{"undeclared ref", declared, "acme:not/declared.json", "not declared"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateLayerScope(tt.project, tt.layer)
			if err == nil {
				t.Fatalf("expected error for %s", tt.name)
			}
			if tt.wantErr != "" && !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want substring %q", err.Error(), tt.wantErr)
			}
		})
	}

	if err := validateLayerScope(declared, "acme:org/base.json"); err != nil {
		t.Errorf("declared ref should validate, got %v", err)
	}
}

// TestPrintSyncHuman_NoLayers exercises the empty-Layers branch of
// printSyncHuman: a project with no declared external layers prints the
// "local stack re-resolved" line and the lockfile path, with no per-layer rows.
func TestPrintSyncHuman_NoLayers(t *testing.T) {
	report := SyncReport{OK: true, Lockfile: "/tmp/.agentsrc.lock", Layers: []SyncedLayer{}}
	buf := &bytes.Buffer{}
	printSyncHuman(buf, report)
	out := buf.String()
	if !strings.Contains(out, "no external layers declared") {
		t.Errorf("missing empty-layers line:\n%s", out)
	}
	if !strings.Contains(out, "/tmp/.agentsrc.lock") {
		t.Errorf("missing lockfile path:\n%s", out)
	}
	if strings.Contains(out, "Summary:") {
		t.Errorf("empty report should not print a per-layer summary:\n%s", out)
	}
}

// TestPrintSyncHuman_ScopedAndUntargeted covers the layer-scoped header plus the
// targeted/untargeted row marks and the note suffix in printSyncHuman.
func TestPrintSyncHuman_ScopedAndUntargeted(t *testing.T) {
	report := SyncReport{
		OK:       true,
		Lockfile: "/tmp/.agentsrc.lock",
		Layer:    "acme:org/base.json",
		Layers: []SyncedLayer{
			{Ref: "acme:org/base.json", SHA: "abc1234567", FetchedAt: "2024-01-01T00:00:00Z", Targeted: true},
			{Ref: "acme:team/frontend.json", Targeted: false, Note: "not targeted by --layer acme:org/base.json"},
		},
	}
	buf := &bytes.Buffer{}
	printSyncHuman(buf, report)
	out := buf.String()
	if !strings.Contains(out, "Config sync (layer acme:org/base.json):") {
		t.Errorf("missing scoped header:\n%s", out)
	}
	if !strings.Contains(out, "1 of 2 layer(s) targeted") {
		t.Errorf("missing targeted summary:\n%s", out)
	}
	if !strings.Contains(out, "not targeted by --layer") {
		t.Errorf("missing untargeted note:\n%s", out)
	}
}

// TestBuildSyncReport_ReadBackErrorOnBadLock covers buildSyncReport's read-back
// error branch: a malformed .agentsrc.lock makes readLockedConfigSection fail,
// which buildSyncReport must propagate.
func TestBuildSyncReport_ReadBackErrorOnBadLock(t *testing.T) {
	project := withTwoLocalLayers(t)
	lockPath := cfg.AgentsLockPath(project)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, []byte("{ this is not valid lock json"), 0o644); err != nil {
		t.Fatalf("write bad lock: %v", err)
	}
	if _, err := buildSyncReport(project, ""); err == nil {
		t.Fatal("expected buildSyncReport to error on a malformed lockfile")
	}
}

// TestReadResolvedUnits_BadUnitsSection covers the Section-decode error branch of
// readResolvedUnits: a lockfile that is valid top-level JSON but whose "units"
// section is the wrong shape (a string, not a map) makes ReadUnits fail after
// Open succeeds.
func TestReadResolvedUnits_BadUnitsSection(t *testing.T) {
	project := withTwoLocalLayers(t)
	lockPath := cfg.AgentsLockPath(project)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatal(err)
	}
	// Top-level JSON parses, but "units" is a string so decoding it into
	// map[string]LockedUnit errors inside Section (not in Open).
	if err := os.WriteFile(lockPath, []byte(`{"version":1,"units":"not-a-map"}`), 0o644); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	if _, err := readResolvedUnits(project); err == nil {
		t.Fatal("expected readResolvedUnits to error on a wrong-shaped units section")
	}
}

// TestBuildSyncReport_VerifyLayerLocksError covers buildSyncReport's
// VerifyLayerLocks failure branch: the lock read-back succeeds (no lock present →
// empty config section), but the manifest is malformed JSON so VerifyLayerLocks
// cannot parse it and buildSyncReport propagates the error.
func TestBuildSyncReport_VerifyLayerLocksError(t *testing.T) {
	project := withRepoLayer(t, `{"version":2,"extends":[ this is not valid json`, "")
	if _, err := buildSyncReport(project, ""); err == nil {
		t.Fatal("expected buildSyncReport to error when the manifest cannot be parsed")
	}
}

// TestRunSync_ReportBuildFailureAfterResolve covers runSync's post-resolve
// report-build error branch: the resolver reports success but the manifest is
// unparseable, so buildSyncReport fails and runSync returns the hinted error.
func TestRunSync_ReportBuildFailureAfterResolve(t *testing.T) {
	project := withRepoLayer(t, `{"version":2,"extends":[ this is not valid json`, "")
	opts := &runSyncOptions{
		runContext: runContext{
			stdout: &bytes.Buffer{},
			stderr: &bytes.Buffer{},
			cwd:    project,
		},
		newResolver: func() forceResolver { return passingResolver{} },
	}
	err := runSync(opts, testDeps())
	if err == nil {
		t.Fatal("expected runSync to error when the post-resolve report build fails")
	}
	if !strings.Contains(err.Error(), "could not read back the lock") {
		t.Errorf("error = %q, want it to mention the read-back failure", err.Error())
	}
}

// TestSyncCmd_RunE_RoutesThroughCwd drives newSyncCmd end-to-end through cobra so
// the RunE wrapper (which resolves cwd via os.Getwd and falls back to the default
// real resolver) is exercised against a real two-layer project. Both a plain
// `sync` and a `--layer`-scoped `sync` are executed.
func TestSyncCmd_RunE_RoutesThroughCwd(t *testing.T) {
	project := withTwoLocalLayers(t)
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
	if err := os.Chdir(project); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	cases := [][]string{
		{"sync"},
		{"sync", "--layer", "acme:org/base.json"},
	}
	for _, args := range cases {
		cmd := newSyncCmd(testDeps())
		cmd.SetArgs(args[1:]) // newSyncCmd is already the `sync` command
		out := &bytes.Buffer{}
		cmd.SetOut(out)
		cmd.SetErr(&bytes.Buffer{})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("Execute %v: %v", args, err)
		}
		if out.Len() == 0 {
			t.Errorf("Execute %v produced no output", args)
		}
	}
}
