package config

import (
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
