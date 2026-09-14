package config

// Contract coverage for LoadEffectiveAgentsRC — the single layered-config read
// seam observability and the workflow publication hook admit on.

import (
	"os"
	"path/filepath"
	"testing"
)

const effectiveObsBlock = `{"enabled":true,"endpoint":"https://obs.example","push_throttle_seconds":30,` +
	`"auth":{"kind":"credential-ref","id":"obs-layer"}}`

// TestLoadEffectiveAgentsRC_MergesUserLocalLayerOnAFlatProject: a project with
// no `extends` still resolves through the layer stack, so a user-local key the
// flat repo manifest never mentions is visible. LoadAgentsRC could not see it.
func TestLoadEffectiveAgentsRC_MergesUserLocalLayerOnAFlatProject(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	writeManifest(t, home, `{"version":2,"observability":`+effectiveObsBlock+`}`)

	repo := t.TempDir()
	writeManifest(t, repo, `{"version":2,"project":"p"}`)

	rc, err := LoadEffectiveAgentsRC(repo)
	if err != nil {
		t.Fatalf("LoadEffectiveAgentsRC: %v", err)
	}
	if rc.Observability == nil || !rc.Observability.Enabled {
		t.Fatalf("observability = %+v, want the user-local layer's enabled block", rc.Observability)
	}
	if rc.Observability.Endpoint != "https://obs.example" {
		t.Errorf("endpoint = %q, want the layer's", rc.Observability.Endpoint)
	}

	// A flat project needs no lock, so the read must not have produced one.
	if _, err := os.Stat(AgentsLockPath(repo)); !os.IsNotExist(err) {
		t.Errorf("a read-only effective load must not write %s (stat err=%v)", AgentsLockFile, err)
	}
}

// TestLoadEffectiveAgentsRC_ReplacesObservabilityWholesale pins the SCALAR
// merge contract for `observability`: it is not in fieldCategories, so the
// highest-precedence layer's object replaces the lower one entirely. A
// fieldwise deep merge would leave the lower layer's endpoint/throttle behind.
func TestLoadEffectiveAgentsRC_ReplacesObservabilityWholesale(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	writeManifest(t, home, `{"version":2,"observability":`+effectiveObsBlock+`}`)

	repo := t.TempDir()
	writeManifest(t, repo, `{"version":2,"project":"p","observability":{"enabled":true,"endpoint":"https://repo.example",`+
		`"auth":{"kind":"credential-ref","id":"obs-repo"}}}`)

	rc, err := LoadEffectiveAgentsRC(repo)
	if err != nil {
		t.Fatalf("LoadEffectiveAgentsRC: %v", err)
	}
	if rc.Observability.Endpoint != "https://repo.example" {
		t.Errorf("endpoint = %q, want the repo-local object to win", rc.Observability.Endpoint)
	}
	if rc.Observability.PushThrottleSeconds != 0 {
		t.Errorf("push_throttle_seconds = %d, want 0: the repo object REPLACES the layer object, it does not merge field-by-field",
			rc.Observability.PushThrottleSeconds)
	}
}

// TestLoadEffectiveAgentsRC_ReadsImportedLayerWithoutWritingTheLock: the
// canonical case — an org/team layer supplies the block, the repo declares
// nothing — plus the access-mode guarantee that makes this safe to call from
// best-effort hook paths.
func TestLoadEffectiveAgentsRC_ReadsImportedLayerWithoutWritingTheLock(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	layerRoot := t.TempDir()
	writeLayerFile(t, filepath.Join(layerRoot, "org", "base.json"), `{"observability":`+effectiveObsBlock+`}`)

	repo := t.TempDir()
	writeManifest(t, repo, `{
		"version": 2,
		"project": "p",
		"sources": [{"id":"org","type":"local","path":`+quoteJSONPath(layerRoot)+`}],
		"extends": ["org:org/base.json"]
	}`)
	if _, err := EnsureResolved(repo, EnsureOpts{}); err != nil {
		t.Fatalf("seed lock: %v", err)
	}
	before, err := os.ReadFile(AgentsLockPath(repo))
	if err != nil {
		t.Fatal(err)
	}

	rc, err := LoadEffectiveAgentsRC(repo)
	if err != nil {
		t.Fatalf("LoadEffectiveAgentsRC: %v", err)
	}
	if rc.Observability == nil || rc.Observability.Endpoint != "https://obs.example" {
		t.Fatalf("observability = %+v, want the imported layer's block", rc.Observability)
	}

	after, err := os.ReadFile(AgentsLockPath(repo))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Error("a Frozen effective read must not rewrite the lock")
	}
}

// TestLoadEffectiveAgentsRC_UnresolvableExtendsFailsInsteadOfFlatFallback: an
// `extends` project with no lock has no defensible answer. Falling back to the
// flat repo manifest would silently answer with a DIFFERENT layer precedence
// than the caller asked for — the exact bug this seam removes.
func TestLoadEffectiveAgentsRC_UnresolvableExtendsFailsInsteadOfFlatFallback(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{
		"version": 2,
		"project": "p",
		"observability": `+effectiveObsBlock+`,
		"sources": [{"id":"org","type":"local","path":"/nonexistent"}],
		"extends": ["org:org/base.json"]
	}`)

	if _, err := LoadEffectiveAgentsRC(repo); err == nil {
		t.Fatal("an unlocked extends project must fail rather than silently degrade to the flat manifest")
	}
}

func writeLayerFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// quoteJSONPath renders a filesystem path as a quoted JSON string literal
// (Windows backslashes escaped).
func quoteJSONPath(p string) string {
	return `"` + jsonPath(p) + `"`
}
