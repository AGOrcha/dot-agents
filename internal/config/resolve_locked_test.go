package config

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// LayeredResolver must expose the read-only ResolveLocked seam used by
// `da config explain` (config-v2 p4e). This is a compile-time assertion that the
// method exists with the documented signature.
var _ func(*LayeredResolver, string) (*Snapshot, error) = (*LayeredResolver).ResolveLocked

// readLockState captures the on-disk bytes + mtime of .agentsrc.lock so a test
// can prove ResolveLocked never mutates the lockfile.
func readLockState(t *testing.T, repo string) ([]byte, int64) {
	t.Helper()
	path := AgentsLockPath(repo)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading lockfile: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat lockfile: %v", err)
	}
	return data, info.ModTime().UnixNano()
}

// TestResolveLockedTwoLayerReadOnlyFromCache is the positive case: a locked
// two-layer project resolves from the cache with no fetch, the resolved Snapshot
// matches the online Resolve result field-for-field, and the .agentsrc.lock file
// is byte-for-byte and mtime-for-mtime untouched.
func TestResolveLockedTwoLayerReadOnlyFromCache(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	src := localLayerSourcePath(t)
	writeManifest(t, repo, `{
		"version": 2,
		"repo_id": "github.com/acme/app",
		"sources": [{"id": "acme", "type": "local", "path": "`+jsonPath(src)+`", "cache_ttl": "4h"}],
		"extends": ["acme:org/base.json", "acme:team/frontend.json"],
		"skills": ["repo-skill"]
	}`)

	// First, an online resolve populates the lockfile + content-addressed cache.
	online, err := NewLayeredResolver().Resolve(repo)
	if err != nil {
		t.Fatalf("online Resolve: %v", err)
	}
	lockBytesBefore, lockMtimeBefore := readLockState(t, repo)

	// Now the read-only locked resolve. It must reconstruct the same stack from
	// the cache without fetching or writing.
	locked, err := NewLayeredResolver().ResolveLocked(repo)
	if err != nil {
		t.Fatalf("ResolveLocked: %v", err)
	}

	// Layer stack matches the online stack: product-defaults, two imports, repo.
	wantIDs := []string{LayerProductDefaults, "acme:org/base.json", "acme:team/frontend.json", LayerRepoLocal}
	if got := layerIDs(locked.Layers); !reflect.DeepEqual(got, wantIDs) {
		t.Errorf("layer ids = %v, want %v", got, wantIDs)
	}
	// Effective config matches the online resolution field-for-field.
	if !reflect.DeepEqual(locked.Effective, online.Effective) {
		t.Errorf("locked effective != online effective\nlocked=%+v\nonline=%+v", locked.Effective, online.Effective)
	}
	// Spot-check the set-union skills came through the cached layers.
	wantSkills := []string{"org-base-skill", "frontend-skill", "repo-skill"}
	if !reflect.DeepEqual(locked.Effective.Skills, wantSkills) {
		t.Errorf("skills = %v, want %v", locked.Effective.Skills, wantSkills)
	}

	// CRITICAL: the lockfile is byte-for-byte and mtime unchanged.
	lockBytesAfter, lockMtimeAfter := readLockState(t, repo)
	if !bytes.Equal(lockBytesBefore, lockBytesAfter) {
		t.Errorf("ResolveLocked mutated lockfile bytes:\nbefore=%s\nafter=%s", lockBytesBefore, lockBytesAfter)
	}
	if lockMtimeBefore != lockMtimeAfter {
		t.Errorf("ResolveLocked changed lockfile mtime: before=%d after=%d", lockMtimeBefore, lockMtimeAfter)
	}
}

// TestResolveLockedFlatDegrade covers the no-extends path: ResolveLocked falls
// back to the FLAT layer set so explain still works on a flat project, and it
// does not require (or write) a lockfile.
func TestResolveLockedFlatDegrade(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{"version": 2, "skills": ["only-repo"]}`)

	snap, err := NewLayeredResolver().ResolveLocked(repo)
	if err != nil {
		t.Fatalf("ResolveLocked: %v", err)
	}
	if got := activeValue(findProvenance(snap, "skills")); !reflect.DeepEqual(got, []any{"only-repo"}) {
		t.Errorf("skills = %v, want [only-repo]", got)
	}
	// Flat layer stack: product-defaults, user-local, repo-local.
	wantIDs := []string{LayerProductDefaults, LayerUserLocal, LayerRepoLocal}
	if got := layerIDs(snap.Layers); !reflect.DeepEqual(got, wantIDs) {
		t.Errorf("layer ids = %v, want %v", got, wantIDs)
	}
	// Read-only: no lockfile is created by the flat-degrade path.
	if _, err := os.Stat(AgentsLockPath(repo)); !os.IsNotExist(err) {
		t.Errorf("ResolveLocked flat-degrade should not write a lockfile, stat err = %v", err)
	}
}

// TestResolveLockedIncludesUserLocalLayer: a present user-local manifest is
// included in the locked stack between product-defaults and the imports.
func TestResolveLockedIncludesUserLocalLayer(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	repo := t.TempDir()
	src := localLayerSourcePath(t)
	// User-local manifest contributes a skill via set-union.
	if err := os.WriteFile(filepath.Join(home, AgentsRCFile), []byte(`{"skills":["user-skill"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	writeManifest(t, repo, `{
		"version": 2,
		"sources": [{"id": "acme", "type": "local", "path": "`+jsonPath(src)+`"}],
		"extends": ["acme:org/base.json"],
		"skills": ["repo-skill"]
	}`)
	if _, err := NewLayeredResolver().Resolve(repo); err != nil {
		t.Fatalf("online Resolve: %v", err)
	}

	snap, err := NewLayeredResolver().ResolveLocked(repo)
	if err != nil {
		t.Fatalf("ResolveLocked: %v", err)
	}
	wantIDs := []string{LayerProductDefaults, LayerUserLocal, "acme:org/base.json", LayerRepoLocal}
	if got := layerIDs(snap.Layers); !reflect.DeepEqual(got, wantIDs) {
		t.Errorf("layer ids = %v, want %v", got, wantIDs)
	}
	// user-skill (user-local) unions ahead of the imported + repo skills.
	if got := snap.Effective.Skills; len(got) == 0 || got[0] != "user-skill" {
		t.Errorf("skills = %v, want user-skill first", got)
	}
}

// TestResolveLockedInvalidUserLocalFatal: a malformed user-local manifest is a
// fatal error from the user-layer load step (it parses as a file but not JSON).
func TestResolveLockedInvalidUserLocalFatal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	repo := t.TempDir()
	src := localLayerSourcePath(t)
	if err := os.WriteFile(filepath.Join(home, AgentsRCFile), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeManifest(t, repo, `{
		"version": 2,
		"sources": [{"id": "acme", "type": "local", "path": "`+jsonPath(src)+`"}],
		"extends": ["acme:org/base.json"]
	}`)
	// Lock entry need not exist; the user-local parse fails first.
	if _, err := NewLayeredResolver().ResolveLocked(repo); err == nil {
		t.Fatal("expected fatal error for malformed user-local manifest")
	}
}

// TestResolveLockedCorruptLockfileErrors: a malformed .agentsrc.lock surfaces as
// an error from readLockedLayers rather than a fetch or a panic.
func TestResolveLockedCorruptLockfileErrors(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{
		"version": 2,
		"sources": [{"id": "acme", "type": "local", "path": "/tmp/x"}],
		"extends": ["acme:org/base.json"]
	}`)
	if err := os.WriteFile(AgentsLockPath(repo), []byte("{not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewLayeredResolver().ResolveLocked(repo); err == nil {
		t.Fatal("expected error from corrupt lockfile")
	}
}

// TestResolveLockedMissingCachedBytesErrors is the negative case: when a locked
// layer's cached bytes are gone, ResolveLocked returns a clear transport
// ImportError and never fetches.
func TestResolveLockedMissingCachedBytesErrors(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	src := localLayerSourcePath(t)
	writeManifest(t, repo, `{
		"version": 2,
		"sources": [{"id": "acme", "type": "local", "path": "`+jsonPath(src)+`", "cache_ttl": "4h"}],
		"extends": ["acme:org/base.json"]
	}`)
	if _, err := NewLayeredResolver().Resolve(repo); err != nil {
		t.Fatalf("online Resolve: %v", err)
	}

	// Wipe the content-addressed cache so the locked SHA's bytes are missing.
	if err := os.RemoveAll(configCacheRoot()); err != nil {
		t.Fatalf("clearing cache: %v", err)
	}

	_, err := NewLayeredResolver().ResolveLocked(repo)
	var ie *ImportError
	if !errors.As(err, &ie) {
		t.Fatalf("expected *ImportError, got %v", err)
	}
	if ie.Reason != ReasonTransport {
		t.Errorf("reason = %q, want %q", ie.Reason, ReasonTransport)
	}
	if ie.Ref != "acme:org/base.json" {
		t.Errorf("ref = %q, want acme:org/base.json", ie.Ref)
	}
}

// TestResolveLockedMissingLockEntryErrors covers a required extends entry that
// has no lockfile entry at all (lock never written / out of sync) — a transport
// ImportError, not a fetch.
func TestResolveLockedMissingLockEntryErrors(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	src := localLayerSourcePath(t)
	writeManifest(t, repo, `{
		"version": 2,
		"sources": [{"id": "acme", "type": "local", "path": "`+jsonPath(src)+`"}],
		"extends": ["acme:org/base.json"]
	}`)
	// No prior online Resolve: there is no .agentsrc.lock, so readLockedLayers
	// returns an empty map and the required entry has no resolved SHA.

	_, err := NewLayeredResolver().ResolveLocked(repo)
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonTransport {
		t.Fatalf("expected transport ImportError, got %v", err)
	}
}

// TestResolveLockedOptionalMissingSkipped: an optional extends entry whose lock
// entry is missing is skipped with a warning rather than failing the resolve.
func TestResolveLockedOptionalMissingSkipped(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	src := localLayerSourcePath(t)
	writeManifest(t, repo, `{
		"version": 2,
		"sources": [{"id": "acme", "type": "local", "path": "`+jsonPath(src)+`"}],
		"extends": [{"ref": "acme:org/base.json", "optional": true}],
		"skills": ["only-repo"]
	}`)
	// No lockfile written: the optional entry cannot be reconstructed and is
	// skipped with a warning; the resolve still succeeds on the flat layers.

	snap, err := NewLayeredResolver().ResolveLocked(repo)
	if err != nil {
		t.Fatalf("ResolveLocked: %v", err)
	}
	if !hasWarningPrefix(snap.Warnings, "acme:org/base.json", "optional_skipped") {
		t.Errorf("expected optional_skipped warning, got %+v", snap.Warnings)
	}
	wantIDs := []string{LayerProductDefaults, LayerRepoLocal}
	if got := layerIDs(snap.Layers); !reflect.DeepEqual(got, wantIDs) {
		t.Errorf("layer ids = %v, want %v", got, wantIDs)
	}
}

// TestResolveLockedLayerProtectedFieldDropped: a cached layer that sets a
// protected field (repo_id) has it dropped with a warning during validation,
// exercising the validate path's non-fatal warning return.
func TestResolveLockedLayerProtectedFieldDropped(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	src := localLayerSourcePath(t)
	writeManifest(t, repo, `{
		"version": 2,
		"sources": [{"id": "acme", "type": "local", "path": "`+jsonPath(src)+`"}],
		"extends": ["acme:org/base.json"]
	}`)
	if _, err := NewLayeredResolver().Resolve(repo); err != nil {
		t.Fatalf("online Resolve: %v", err)
	}
	// Rewrite the cached layer bytes to attempt a protected field.
	locked, err := readLockedLayersFromUnits(repo)
	if err != nil {
		t.Fatal(err)
	}
	sha := locked["acme:org/base.json"].ResolvedSHA
	cacheDir := layerCacheDir("acme", "org/base.json")
	if err := os.WriteFile(filepath.Join(cacheDir, sha, "layer.json"), []byte(`{"repo_id":"evil","skills":["base"]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	snap, err := NewLayeredResolver().ResolveLocked(repo)
	if err != nil {
		t.Fatalf("ResolveLocked: %v", err)
	}
	if !hasWarning(snap.Warnings, "repo_id", "dropped") {
		t.Errorf("expected repo_id dropped warning, got %+v", snap.Warnings)
	}
}

// TestResolveLockedUnknownSourceErrors: an extends ref to an undeclared source
// is a not_found ImportError (caught before any cache read).
func TestResolveLockedUnknownSourceErrors(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{
		"version": 2,
		"sources": [{"id": "acme", "type": "local", "path": "/tmp/x"}],
		"extends": ["ghost:org/base.json"]
	}`)
	_, err := NewLayeredResolver().ResolveLocked(repo)
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonNotFound {
		t.Fatalf("expected not_found ImportError, got %v", err)
	}
}

// TestResolveLockedBadRefErrors: a malformed extends ref is a schema ImportError.
func TestResolveLockedBadRefErrors(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{
		"version": 2,
		"sources": [{"id": "acme", "type": "local", "path": "/tmp/x"}],
		"extends": ["no-colon-ref"]
	}`)
	_, err := NewLayeredResolver().ResolveLocked(repo)
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonSchema {
		t.Fatalf("expected schema ImportError, got %v", err)
	}
}

// TestResolveLockedUndecodableManifestErrors: a repo manifest that parses as a
// generic object but is not a valid AgentsRC (extends is the wrong type) fails at
// the typed-decode step.
func TestResolveLockedUndecodableManifestErrors(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	// extends must be an array of refs; a number is a typed-decode error.
	writeManifest(t, repo, `{"version": 2, "extends": 42}`)
	if _, err := NewLayeredResolver().ResolveLocked(repo); err == nil {
		t.Fatal("expected error decoding malformed manifest")
	}
}

// TestResolveLockedMissingRepoManifestFatal: ResolveLocked surfaces the missing
// repo-local manifest as a fatal error (it cannot degrade without a manifest).
func TestResolveLockedMissingRepoManifestFatal(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir() // no .agentsrc.json written
	if _, err := NewLayeredResolver().ResolveLocked(repo); err == nil {
		t.Fatal("expected error for missing repo manifest")
	}
}

// TestResolveLockedInvalidLayerCacheIsSchemaError: cached bytes that are not
// valid JSON decode to a schema ImportError (no fetch).
func TestResolveLockedInvalidLayerCacheIsSchemaError(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	src := localLayerSourcePath(t)
	writeManifest(t, repo, `{
		"version": 2,
		"sources": [{"id": "acme", "type": "local", "path": "`+jsonPath(src)+`"}],
		"extends": ["acme:org/base.json"]
	}`)
	if _, err := NewLayeredResolver().Resolve(repo); err != nil {
		t.Fatalf("online Resolve: %v", err)
	}
	// Corrupt the cached layer bytes at the locked SHA in place.
	locked, err := readLockedLayersFromUnits(repo)
	if err != nil {
		t.Fatal(err)
	}
	sha := locked["acme:org/base.json"].ResolvedSHA
	cacheDir := layerCacheDir("acme", "org/base.json")
	if err := os.WriteFile(filepath.Join(cacheDir, sha, "layer.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = NewLayeredResolver().ResolveLocked(repo)
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonSchema {
		t.Fatalf("expected schema ImportError, got %v", err)
	}
}

// TestResolveLockedVersionPinnedRef covers the version-pinned dedupe key in the
// transitive walk: an extends ref carrying `@<version>` keys the dedupe/cycle
// map by ref+version, and reconstructs from the cache keyed by source+path.
func TestResolveLockedVersionPinnedRef(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{
		"version": 2,
		"sources": [{"id": "s", "type": "git", "url": "https://example/s.git", "ref": "main"}],
		"extends": ["s:org.json@v1"]
	}`)
	if err := WriteConfigLock(repo, map[string]LockedLayer{
		"s:org.json@v1": {ResolvedSHA: "shaorg", FetchedAt: "2026-06-02T00:00:00Z"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := writeCachedLayer(layerCacheDir("s", "org.json"), "shaorg", []byte(`{"version":2,"skills":["org"]}`)); err != nil {
		t.Fatal(err)
	}
	snap, err := NewLayeredResolver().ResolveLocked(repo)
	if err != nil {
		t.Fatalf("ResolveLocked: %v", err)
	}
	wantIDs := []string{LayerProductDefaults, "s:org.json@v1", LayerRepoLocal}
	if got := layerIDs(snap.Layers); !reflect.DeepEqual(got, wantIDs) {
		t.Errorf("layer ids = %v, want %v", got, wantIDs)
	}
}

// TestResolveLockedCycleDetected: a cyclic cached extends graph (org->team->org)
// is a fatal cycle ImportError in the offline replay, never an infinite walk.
// Seeded manually because the online resolve rejects the cycle before it could
// write the lock.
func TestResolveLockedCycleDetected(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{
		"version": 2,
		"sources": [{"id": "s", "type": "git", "url": "https://example/s.git", "ref": "main"}],
		"extends": ["s:org.json"]
	}`)
	if err := WriteConfigLock(repo, map[string]LockedLayer{
		"s:org.json":  {ResolvedSHA: "shaorg", FetchedAt: "t"},
		"s:team.json": {ResolvedSHA: "shateam", FetchedAt: "t"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := writeCachedLayer(layerCacheDir("s", "org.json"), "shaorg", []byte(`{"version":2,"extends":["s:team.json"],"skills":["org"]}`)); err != nil {
		t.Fatal(err)
	}
	if err := writeCachedLayer(layerCacheDir("s", "team.json"), "shateam", []byte(`{"version":2,"extends":["s:org.json"],"skills":["team"]}`)); err != nil {
		t.Fatal(err)
	}
	_, err := NewLayeredResolver().ResolveLocked(repo)
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonCycle {
		t.Fatalf("expected cycle ImportError, got %v", err)
	}
}

// TestResolveLockedOptionalChildSkipped: a nested (transitive) optional extends
// whose lock/cache is missing is skipped with a warning, and its parent layer is
// still admitted — the optional-skip semantics apply at every depth, not just at
// the repo root.
func TestResolveLockedOptionalChildSkipped(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{
		"version": 2,
		"sources": [{"id": "s", "type": "git", "url": "https://example/s.git", "ref": "main"}],
		"extends": ["s:org.json"]
	}`)
	if err := WriteConfigLock(repo, map[string]LockedLayer{
		"s:org.json": {ResolvedSHA: "shaorg", FetchedAt: "t"},
	}); err != nil {
		t.Fatal(err)
	}
	// org.json transitively extends an OPTIONAL team layer that was never locked.
	if err := writeCachedLayer(layerCacheDir("s", "org.json"), "shaorg", []byte(`{"version":2,"extends":[{"ref":"s:team.json","optional":true}],"skills":["org"]}`)); err != nil {
		t.Fatal(err)
	}
	snap, err := NewLayeredResolver().ResolveLocked(repo)
	if err != nil {
		t.Fatalf("ResolveLocked: %v", err)
	}
	if !hasWarningPrefix(snap.Warnings, "s:team.json", "optional_skipped") {
		t.Errorf("expected optional_skipped warning for the nested child, got %+v", snap.Warnings)
	}
	wantIDs := []string{LayerProductDefaults, "s:org.json", LayerRepoLocal}
	if got := layerIDs(snap.Layers); !reflect.DeepEqual(got, wantIDs) {
		t.Errorf("layer ids = %v, want %v (parent still admitted)", got, wantIDs)
	}
}

// TestResolveLockedNullLayerCacheIsSchemaError: cached bytes of `null` decode to
// a nil payload that validateLayer rejects as a schema ImportError (an empty
// fetch must be loud), distinct from non-JSON bytes that fail at decode.
func TestResolveLockedNullLayerCacheIsSchemaError(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{
		"version": 2,
		"sources": [{"id": "s", "type": "git", "url": "https://example/s.git", "ref": "main"}],
		"extends": ["s:org.json"]
	}`)
	if err := WriteConfigLock(repo, map[string]LockedLayer{
		"s:org.json": {ResolvedSHA: "shaorg", FetchedAt: "t"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := writeCachedLayer(layerCacheDir("s", "org.json"), "shaorg", []byte(`null`)); err != nil {
		t.Fatal(err)
	}
	_, err := NewLayeredResolver().ResolveLocked(repo)
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonSchema {
		t.Fatalf("expected schema ImportError, got %v", err)
	}
}

// TestResolveLockedChildExtendsWrongTypeErrors: a cached layer whose `extends` is
// the wrong JSON type passes the layer schema (which does not type-check extends)
// but fails the typed decode of the child's own sources/extends in the walk — a
// schema ImportError rather than a silent drop of the transitive edge.
func TestResolveLockedChildExtendsWrongTypeErrors(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{
		"version": 2,
		"sources": [{"id": "s", "type": "git", "url": "https://example/s.git", "ref": "main"}],
		"extends": ["s:org.json"]
	}`)
	if err := WriteConfigLock(repo, map[string]LockedLayer{
		"s:org.json": {ResolvedSHA: "shaorg", FetchedAt: "t"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := writeCachedLayer(layerCacheDir("s", "org.json"), "shaorg", []byte(`{"version":2,"extends":{"bad":"object"}}`)); err != nil {
		t.Fatal(err)
	}
	_, err := NewLayeredResolver().ResolveLocked(repo)
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonSchema {
		t.Fatalf("expected schema ImportError, got %v", err)
	}
}
