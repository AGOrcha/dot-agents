package config

import (
	"os"
	"path/filepath"
	"testing"
)

// layerProject writes a manifest + (optional) lockfile into an isolated temp
// tree (AGENTS_HOME under it so the layer cache resolves locally) and returns
// the project path. Cached layers are seeded separately via seedCachedLayer.
func layerProject(t *testing.T, manifest string, locks map[string]LockedLayer) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("AGENTS_HOME", filepath.Join(root, "home", ".agents"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))

	project := filepath.Join(root, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	if manifest != "" {
		if err := os.WriteFile(filepath.Join(project, AgentsRCFile), []byte(manifest), 0o644); err != nil {
			t.Fatalf("write manifest: %v", err)
		}
	}
	if locks != nil {
		if err := WriteConfigLock(project, locks); err != nil {
			t.Fatalf("write lock: %v", err)
		}
	}
	return project
}

func seedCachedLayer(t *testing.T, sourceID, layerPath, sha string) {
	t.Helper()
	if err := writeCachedUnit(layerTarget(sourceID, layerPath), sha, []byte(`{"skills":[]}`)); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
}

func statusByRef(sts []LayerLockStatus, ref string) (LayerLockStatus, bool) {
	for _, s := range sts {
		if s.Ref == ref {
			return s, true
		}
	}
	return LayerLockStatus{}, false
}

func TestVerifyLayerLocks_NoManifestOrNoExtends(t *testing.T) {
	// no manifest at all
	root := t.TempDir()
	t.Setenv("AGENTS_HOME", filepath.Join(root, "home", ".agents"))
	got, err := VerifyLayerLocks(filepath.Join(root, "project"))
	if err != nil || got != nil {
		t.Fatalf("no manifest: want (nil,nil), got (%v,%v)", got, err)
	}

	// manifest with no extends
	p := layerProject(t, `{"sources":[{"type":"local"}]}`, nil)
	got, err = VerifyLayerLocks(p)
	if err != nil || len(got) != 0 {
		t.Fatalf("no extends: want empty, got (%v,%v)", got, err)
	}
}

func TestVerifyLayerLocks_UnparseableManifest(t *testing.T) {
	p := layerProject(t, `{not-json`, nil)
	if _, err := VerifyLayerLocks(p); err == nil {
		t.Fatalf("expected error for unparseable manifest")
	}
}

func TestVerifyLayerLocks_RemoteCachedAndMissing(t *testing.T) {
	manifest := `{
	  "sources": [{"type":"git","id":"acme","url":"https://example.com/a.git"}],
	  "extends": ["acme:org/base", "acme:org/extra"]
	}`
	locks := map[string]LockedLayer{
		"acme:org/base":  {ResolvedSHA: "abcdef1234567890", FetchedAt: "2026-06-02T00:00:00Z"},
		"acme:org/extra": {ResolvedSHA: "deadbeefcafef00d", FetchedAt: "2026-06-02T00:00:00Z"},
	}
	p := layerProject(t, manifest, locks)
	// Only base is present in the cache; extra is locked but its assets are gone.
	seedCachedLayer(t, "acme", "org/base", "abcdef1234567890")

	got, err := VerifyLayerLocks(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	base, _ := statusByRef(got, "acme:org/base")
	if !base.Locked || !base.Cached || !base.OK() || base.SourceType != "git" {
		t.Fatalf("base should be locked+cached+ok git, got %+v", base)
	}
	extra, _ := statusByRef(got, "acme:org/extra")
	if !extra.Locked || extra.Cached || extra.OK() {
		t.Fatalf("extra should be locked but not cached, got %+v", extra)
	}
	if extra.Problem == "" || extra.CachePath == "" {
		t.Fatalf("extra should report a cache-miss problem + path, got %+v", extra)
	}
}

func TestVerifyLayerLocks_NotLockedLocalAndUnknownSourceAndBadRef(t *testing.T) {
	manifest := `{
	  "sources": [
	    {"type":"git","id":"acme","url":"https://example.com/a.git"},
	    {"type":"local","id":"loc","path":"./layers"}
	  ],
	  "extends": [
	    "acme:org/unlocked",
	    "loc:team/x",
	    "ghost:org/y",
	    {"ref":"acme:org/opt","optional":true},
	    "no-colon-ref"
	  ]
	}`
	// loc layer is locked (local needs only a lock entry); acme:org/unlocked and
	// the optional acme:org/opt have no lock entries.
	locks := map[string]LockedLayer{
		"loc:team/x": {ResolvedSHA: "11112222", FetchedAt: "2026-06-02T00:00:00Z"},
	}
	p := layerProject(t, manifest, locks)
	seedCachedLayer(t, "loc", "team/x", "11112222") // local layers are cached like remote

	got, err := VerifyLayerLocks(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	loc, _ := statusByRef(got, "loc:team/x")
	if !loc.Locked || !loc.Cached || !loc.OK() || loc.SourceType != "local" {
		t.Fatalf("local locked layer should be ok, got %+v", loc)
	}
	unlocked, _ := statusByRef(got, "acme:org/unlocked")
	if unlocked.Locked || unlocked.OK() {
		t.Fatalf("acme:org/unlocked should be unlocked+problem, got %+v", unlocked)
	}
	ghost, _ := statusByRef(got, "ghost:org/y")
	if ghost.OK() || ghost.SourceType != "" {
		t.Fatalf("ghost source should report undeclared-source problem, got %+v", ghost)
	}
	opt, _ := statusByRef(got, "acme:org/opt")
	if !opt.Optional || opt.OK() {
		t.Fatalf("optional unlocked layer should carry Optional+problem, got %+v", opt)
	}
	bad, _ := statusByRef(got, "no-colon-ref")
	if bad.OK() || bad.Problem == "" {
		t.Fatalf("bad ref should report a parse problem, got %+v", bad)
	}
}

func TestVerifyLayerLocks_CorruptLockfileErrors(t *testing.T) {
	manifest := `{"sources":[{"type":"git","id":"acme","url":"u"}],"extends":["acme:org/base"]}`
	p := layerProject(t, manifest, nil)
	if err := os.WriteFile(AgentsLockPath(p), []byte("{not-json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyLayerLocks(p); err == nil {
		t.Fatalf("expected error reading corrupt lockfile")
	}
}

func TestShortSHA(t *testing.T) {
	if shortSHA("abcdef0123456789") != "abcdef012345" {
		t.Fatalf("long sha not truncated: %q", shortSHA("abcdef0123456789"))
	}
	if shortSHA("short") != "short" {
		t.Fatalf("short sha changed: %q", shortSHA("short"))
	}
}
