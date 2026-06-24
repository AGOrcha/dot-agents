package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// loadManifest writes body to <dir>/.agentsrc.json (via the shared
// writeManifest helper) and returns the loaded manifest, failing the test on a
// parse error.
func loadManifest(t *testing.T, dir, body string) *AgentsRC {
	t.Helper()
	writeManifest(t, dir, body)
	rc, err := LoadAgentsRC(dir)
	if err != nil {
		t.Fatalf("LoadAgentsRC: %v", err)
	}
	return rc
}

func TestDetectV1Deprecation_LegacyVersionFires(t *testing.T) {
	rc := loadManifest(t, t.TempDir(), `{
  "version": 1,
  "project": "demo",
  "sources": [{"type": "local"}]
}`)

	w := DetectV1Deprecation(rc)
	if !w.Detected {
		t.Fatal("expected deprecation detected for version 1 manifest")
	}
	if !w.LegacyVersion {
		t.Errorf("LegacyVersion: got false, want true (version=%d)", w.Version)
	}
	if msg := w.Message(); msg == "" || !strings.Contains(msg, "deprecated") {
		t.Errorf("Message: got %q, want a non-empty deprecation line", msg)
	}
}

func TestDetectV1Deprecation_LegacyKeysFire(t *testing.T) {
	// A v2 version number but with deprecated keys folded silently on load.
	rc := loadManifest(t, t.TempDir(), `{
  "version": 2,
  "project": "demo",
  "sources": [{"type": "local"}],
  "verifier_profiles": {"unit": {"label": "Unit"}},
  "app_type_verifier_map": {"go-cli": ["unit"]}
}`)

	w := DetectV1Deprecation(rc)
	if !w.Detected {
		t.Fatal("expected deprecation detected for manifest carrying legacy keys")
	}
	if w.LegacyVersion {
		t.Error("LegacyVersion: got true, want false (version=2)")
	}
	want := []string{"app_type_verifier_map", "verifier_profiles"}
	if strings.Join(w.LegacyKeys, ",") != strings.Join(want, ",") {
		t.Errorf("LegacyKeys: got %v, want %v", w.LegacyKeys, want)
	}
	if msg := w.Message(); !strings.Contains(msg, "verifier_profiles") {
		t.Errorf("Message: got %q, want it to name the deprecated keys", msg)
	}
}

func TestDetectV1Deprecation_CleanV2DoesNotFire(t *testing.T) {
	rc := loadManifest(t, t.TempDir(), `{
  "version": 2,
  "project": "demo",
  "repo_id": "github.com/acme/demo",
  "sources": [{"type": "local", "id": "self"}],
  "stage_profiles": {"verifier": {"unit": {"label": "Unit"}}}
}`)

	w := DetectV1Deprecation(rc)
	if w.Detected {
		t.Errorf("expected no deprecation for clean v2 manifest, got %+v", w)
	}
	if msg := w.Message(); msg != "" {
		t.Errorf("Message: got %q, want empty for clean v2", msg)
	}
}

func TestDetectV1Deprecation_NilManifest(t *testing.T) {
	if w := DetectV1Deprecation(nil); w.Detected {
		t.Errorf("nil manifest: got detected=%v, want false", w.Detected)
	}
}

// v1WithLegacyKeys is a legacy manifest: v1 schema version + a deprecated key
// that the loader folds into stage_profiles and never re-emits.
const v1WithLegacyKeys = `{
  "version": 1,
  "project": "demo",
  "sources": [{"type": "local"}],
  "verifier_profiles": {"unit": {"label": "Unit"}},
  "app_type_verifier_map": {"go-cli": ["unit"]}
}`

func TestMigrateAgentsRC_RewritesV1ToV2(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, v1WithLegacyKeys)

	res, err := MigrateAgentsRC(dir, false)
	if err != nil {
		t.Fatalf("MigrateAgentsRC: %v", err)
	}
	if res.AlreadyV2 {
		t.Fatal("AlreadyV2: got true, want false for a v1 manifest")
	}
	if res.FromVersion != 1 || res.ToVersion != CurrentManifestVersion {
		t.Errorf("version: got %d->%d, want 1->%d", res.FromVersion, res.ToVersion, CurrentManifestVersion)
	}
	if !res.WroteFile || !res.WroteBackup {
		t.Errorf("wrote flags: file=%v backup=%v, want both true", res.WroteFile, res.WroteBackup)
	}
	wantKeys := "app_type_verifier_map,verifier_profiles"
	if strings.Join(res.FoldedKeys, ",") != wantKeys {
		t.Errorf("FoldedKeys: got %v, want %v", res.FoldedKeys, wantKeys)
	}

	// The rewritten manifest must be clean v2: current version, no legacy keys,
	// and the legacy data folded into stage_profiles.
	reloaded, err := LoadAgentsRC(dir)
	if err != nil {
		t.Fatalf("reload after migrate: %v", err)
	}
	if reloaded.Version != CurrentManifestVersion {
		t.Errorf("reloaded version: got %d, want %d", reloaded.Version, CurrentManifestVersion)
	}
	if w := DetectV1Deprecation(reloaded); w.Detected {
		t.Errorf("reloaded manifest still flags v1: %+v", w)
	}
	if _, ok := reloaded.StageProfiles["verifier"]["unit"]; !ok {
		t.Error("folded verifier profile missing from migrated stage_profiles")
	}

	// The on-disk bytes must not contain the dropped legacy key.
	onDisk := readFile(t, filepath.Join(dir, AgentsRCFile))
	if strings.Contains(onDisk, "verifier_profiles") {
		t.Error("migrated manifest still contains the legacy verifier_profiles key")
	}
}

func TestMigrateAgentsRC_BackupHoldsOriginal(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, v1WithLegacyKeys)

	if _, err := MigrateAgentsRC(dir, false); err != nil {
		t.Fatalf("MigrateAgentsRC: %v", err)
	}

	backup := readFile(t, filepath.Join(dir, AgentsRCFile+V1BackupSuffix))
	if backup != v1WithLegacyKeys {
		t.Errorf("backup is not the byte-identical original:\n got: %q", backup)
	}
}

func TestMigrateAgentsRC_IdempotentOnV2(t *testing.T) {
	dir := t.TempDir()
	v2 := `{
  "version": 2,
  "project": "demo",
  "repo_id": "github.com/acme/demo",
  "sources": [{"type": "local", "id": "self"}],
  "stage_profiles": {"verifier": {"unit": {"label": "Unit"}}}
}`
	writeManifest(t, dir, v2)

	res, err := MigrateAgentsRC(dir, false)
	if err != nil {
		t.Fatalf("MigrateAgentsRC: %v", err)
	}
	if !res.AlreadyV2 {
		t.Error("AlreadyV2: got false, want true for a clean v2 manifest")
	}
	if res.WroteFile || res.WroteBackup {
		t.Errorf("no-op must not write: file=%v backup=%v", res.WroteFile, res.WroteBackup)
	}
	if _, err := os.Stat(filepath.Join(dir, AgentsRCFile+V1BackupSuffix)); !os.IsNotExist(err) {
		t.Errorf("no-op created a backup sidecar (stat err=%v)", err)
	}
}

func TestMigrateAgentsRC_DryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, v1WithLegacyKeys)

	res, err := MigrateAgentsRC(dir, true)
	if err != nil {
		t.Fatalf("MigrateAgentsRC dry-run: %v", err)
	}
	if !res.DryRun {
		t.Error("DryRun flag not set on dry-run result")
	}
	if res.WroteFile || res.WroteBackup {
		t.Errorf("dry-run wrote: file=%v backup=%v, want both false", res.WroteFile, res.WroteBackup)
	}
	if len(res.V2JSON) == 0 || !strings.Contains(string(res.V2JSON), "\"version\": 2") {
		t.Error("dry-run did not produce a v2 preview")
	}

	// The manifest on disk must be untouched (still v1) and no backup created.
	onDisk := readFile(t, filepath.Join(dir, AgentsRCFile))
	if onDisk != v1WithLegacyKeys {
		t.Errorf("dry-run mutated the manifest on disk:\n got: %q", onDisk)
	}
	if _, err := os.Stat(filepath.Join(dir, AgentsRCFile+V1BackupSuffix)); !os.IsNotExist(err) {
		t.Errorf("dry-run created a backup sidecar (stat err=%v)", err)
	}
}

func TestMigrateAgentsRC_MissingManifest(t *testing.T) {
	if _, err := MigrateAgentsRC(t.TempDir(), false); err == nil {
		t.Error("expected an error migrating a directory with no .agentsrc.json")
	}
}
