package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// prunableProject sandboxes AGENTS_HOME, writes a project lock pinning the given
// units, and returns the project path. Every test in this file works exclusively
// against that sandbox — a prune must NEVER be exercised against a real home.
func prunableProject(t *testing.T, units map[string]LockedUnit) string {
	t.Helper()
	t.Setenv("AGENTS_HOME", t.TempDir())
	return projectWithUnits(t, units)
}

// projectWithUnits writes one more project (under the CURRENT sandboxed home)
// whose lock pins the given units.
func projectWithUnits(t *testing.T, units map[string]LockedUnit) string {
	t.Helper()
	repo := t.TempDir()
	if err := WriteUnitsLock(repo, UnitsLock{Units: units}); err != nil {
		t.Fatal(err)
	}
	return repo
}

// seedCacheEntry writes bytes into the shared config cache for a source+path at a
// digest, mirroring exactly what a fetcher writes.
func seedCacheEntry(t *testing.T, sourceID, unitPath, digest, fileName string, body []byte) string {
	t.Helper()
	target := FetchTarget{Dir: layerCacheDir(sourceID, unitPath), FileName: fileName}
	if err := writeCachedUnit(target, digest, body); err != nil {
		t.Fatal(err)
	}
	return filepath.Dir(target.pathFor(digest))
}

// entryFor finds the scanned entry for an absolute entry path.
func entryFor(t *testing.T, scan CacheScan, path string) CacheEntry {
	t.Helper()
	for _, e := range scan.Entries {
		if e.Path == path {
			return e
		}
	}
	t.Fatalf("entry %q not found in scan %#v", path, scan.Entries)
	return CacheEntry{}
}

// TestScanConfigCacheMarksReferencedEntries proves the liveness rule: a digest a
// project's lock pins (layer OR prompt) is referenced and therefore protected,
// while a superseded digest from the same source+path is prunable.
func TestScanConfigCacheMarksReferencedEntries(t *testing.T) {
	live := contentHash([]byte("live"))
	stale := contentHash([]byte("stale"))
	promptLive := contentHash([]byte("# prompt"))

	repo := prunableProject(t, map[string]LockedUnit{
		"team:team/base.json":         {Kind: UnitKindLayer, Digest: live},
		"team:verifiers/ts-lint.md":   {Kind: UnitKindPrompt, Digest: promptLive},
		"team:tools/fmt@1.0.0":        {Kind: UnitKindArtifact, Digest: "sha256:abc"},
		"team:digestless.json":        {Kind: UnitKindLayer},
		"repo-local:stage:verifier":   {Kind: UnitKindProfile, Digest: "sha256:def"},
		"malformed-key-without-colon": {Kind: UnitKindLayer, Digest: live},
	})
	liveDir := seedCacheEntry(t, "team", "team/base.json", live, layerCacheFileName, []byte("live"))
	staleDir := seedCacheEntry(t, "team", "team/base.json", stale, layerCacheFileName, []byte("stale"))
	promptDir := seedCacheEntry(t, "team", "verifiers/ts-lint.md", promptLive, "ts-lint.md", []byte("# prompt"))
	orphanDir := seedCacheEntry(t, "gone", "verifiers/old.md", contentHash([]byte("old")), "old.md", []byte("old"))

	scan, err := ScanConfigCache([]string{repo})
	if err != nil {
		t.Fatalf("ScanConfigCache: %v", err)
	}
	assertReferenced(t, scan, liveDir, true)
	assertReferenced(t, scan, promptDir, true)
	assertReferenced(t, scan, staleDir, false)
	assertReferenced(t, scan, orphanDir, false)

	if got := len(scan.Prunable()); got != 2 {
		t.Fatalf("prunable = %d, want 2 (%#v)", got, scan.Prunable())
	}
	if scan.PrunableBytes() != int64(len("stale")+len("old")) {
		t.Fatalf("prunable bytes = %d, want %d", scan.PrunableBytes(), len("stale")+len("old"))
	}
	entry := entryFor(t, scan, promptDir)
	if entry.SourceID != "team" || entry.UnitPath != "verifiers/ts-lint.md" || entry.Digest != promptLive {
		t.Fatalf("entry coordinates = %#v", entry)
	}
}

// assertReferenced asserts one entry's referenced flag.
func assertReferenced(t *testing.T, scan CacheScan, path string, want bool) {
	t.Helper()
	if got := entryFor(t, scan, path).Referenced; got != want {
		t.Fatalf("entry %q referenced = %t, want %t", path, got, want)
	}
}

// TestScanConfigCacheAcrossProjects proves the live set is the UNION over every
// supplied project, so one project's pin protects an entry another never names,
// and a project with no lockfile is reported as skipped rather than silently
// contributing nothing.
func TestScanConfigCacheAcrossProjects(t *testing.T) {
	a := contentHash([]byte("a"))
	b := contentHash([]byte("b"))
	repoA := prunableProject(t, map[string]LockedUnit{"team:team/base.json": {Kind: UnitKindLayer, Digest: a}})
	repoB := projectWithUnits(t, map[string]LockedUnit{"team:team/base.json": {Kind: UnitKindLayer, Digest: b}})
	unlocked := t.TempDir()

	dirA := seedCacheEntry(t, "team", "team/base.json", a, layerCacheFileName, []byte("a"))
	dirB := seedCacheEntry(t, "team", "team/base.json", b, layerCacheFileName, []byte("b"))

	scan, err := ScanConfigCache([]string{repoA, repoB, unlocked, repoA})
	if err != nil {
		t.Fatalf("ScanConfigCache: %v", err)
	}
	assertReferenced(t, scan, dirA, true)
	assertReferenced(t, scan, dirB, true)
	if len(scan.Projects) != 2 {
		t.Fatalf("projects = %#v, want the two locked projects (deduped)", scan.Projects)
	}
	if len(scan.Skipped) != 1 || scan.Skipped[0] != filepath.Clean(unlocked) {
		t.Fatalf("skipped = %#v, want the lockless project", scan.Skipped)
	}
}

// TestScanConfigCacheFailsClosedOnBadLock proves an unparseable lock blocks the
// prune instead of shrinking the live set.
func TestScanConfigCacheFailsClosedOnBadLock(t *testing.T) {
	repo := prunableProject(t, nil)
	if err := os.WriteFile(AgentsLockPath(repo), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ScanConfigCache([]string{repo}); err == nil {
		t.Fatal("an unparseable lock must fail the scan, not shrink the live set")
	}
}

// TestScanConfigCacheEmptyRoot proves a machine with no cache yet scans cleanly.
func TestScanConfigCacheEmptyRoot(t *testing.T) {
	repo := prunableProject(t, map[string]LockedUnit{})
	scan, err := ScanConfigCache([]string{repo})
	if err != nil {
		t.Fatalf("ScanConfigCache: %v", err)
	}
	if len(scan.Entries) != 0 || len(scan.Prunable()) != 0 || scan.PrunableBytes() != 0 {
		t.Fatalf("empty cache must scan to nothing, got %#v", scan)
	}
}

// TestPruneCacheEntriesRemovesAndCleansParents proves --apply deletes exactly the
// prunable entries, leaves the referenced ones intact, reclaims their bytes, and
// cleans up the now-empty parent directories without touching the cache root.
func TestPruneCacheEntriesRemovesAndCleansParents(t *testing.T) {
	live := contentHash([]byte("live"))
	stale := contentHash([]byte("stale"))
	repo := prunableProject(t, map[string]LockedUnit{
		"team:team/base.json": {Kind: UnitKindLayer, Digest: live},
	})
	liveDir := seedCacheEntry(t, "team", "team/base.json", live, layerCacheFileName, []byte("live"))
	staleDir := seedCacheEntry(t, "team", "team/base.json", stale, layerCacheFileName, []byte("stale"))
	orphanDir := seedCacheEntry(t, "gone", "verifiers/old.md", contentHash([]byte("old")), "old.md", []byte("old"))

	scan, err := ScanConfigCache([]string{repo})
	if err != nil {
		t.Fatalf("ScanConfigCache: %v", err)
	}
	removed, bytes, err := PruneCacheEntries(scan.Root, scan.Prunable())
	if err != nil {
		t.Fatalf("PruneCacheEntries: %v", err)
	}
	if removed != 2 || bytes != int64(len("stale")+len("old")) {
		t.Fatalf("removed %d entries / %d bytes, want 2 / %d", removed, bytes, len("stale")+len("old"))
	}
	assertPathGone(t, staleDir)
	assertPathGone(t, orphanDir)
	// The whole "gone" source tree is empty now and must be cleaned up, while the
	// cache root itself and the still-referenced entry survive.
	assertPathGone(t, filepath.Join(scan.Root, "gone"))
	if _, err := os.Stat(liveDir); err != nil {
		t.Fatalf("a referenced entry must survive: %v", err)
	}
	if _, err := os.Stat(scan.Root); err != nil {
		t.Fatalf("the cache root must survive: %v", err)
	}
}

// assertPathGone asserts a path no longer exists.
func assertPathGone(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %q to be removed, stat err = %v", path, err)
	}
}

// TestPruneCacheEntriesAbsentEntryIsNoOp proves an entry that is already gone
// (e.g. removed by a concurrent prune) does not fail the run.
func TestPruneCacheEntriesAbsentEntryIsNoOp(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	root := configCacheRoot()
	removed, bytes, err := PruneCacheEntries(root, []CacheEntry{{Path: filepath.Join(root, "team", "nope", "sha")}})
	if err != nil || removed != 1 || bytes != 0 {
		t.Fatalf("removing an absent entry = (%d, %d, %v), want (1, 0, nil)", removed, bytes, err)
	}
}

// TestPruneCacheEntriesSurfacesRemovalError proves a failed removal stops the
// prune and reports how many entries had already been removed, rather than
// reporting a clean run over a cache it could not actually change.
func TestPruneCacheEntriesSurfacesRemovalError(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("directory permissions do not gate removal for this platform/user")
	}
	t.Setenv("AGENTS_HOME", t.TempDir())
	root := configCacheRoot()
	dir := seedCacheEntry(t, "team", "base.json", "shalocked", layerCacheFileName, []byte("x"))
	parent := filepath.Dir(dir)
	if err := os.Chmod(parent, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })

	removed, _, err := PruneCacheEntries(root, []CacheEntry{{Path: dir}})
	if err == nil {
		t.Fatal("expected the blocked removal to surface an error")
	}
	if removed != 0 {
		t.Fatalf("removed = %d, want 0 before the failure", removed)
	}
}

// skipWithoutPermissionGating skips a test on platforms/users where directory
// permissions do not actually gate reads or removals.
func skipWithoutPermissionGating(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("directory permissions do not gate access for this platform/user")
	}
}

// TestScanConfigCacheFailsOnUnreadableEntry proves the scan refuses to report a
// PARTIAL view of the cache: an unreadable directory is an error, because a
// partial view is exactly what must never drive a delete.
func TestScanConfigCacheFailsOnUnreadableEntry(t *testing.T) {
	skipWithoutPermissionGating(t)
	repo := prunableProject(t, map[string]LockedUnit{})
	dir := seedCacheEntry(t, "team", "base.json", "shahidden", layerCacheFileName, []byte("x"))
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	if _, err := ScanConfigCache([]string{repo}); err == nil {
		t.Fatal("an unreadable cache directory must fail the scan")
	}
}

// TestPruneCacheEntriesSurfacesParentCleanupError proves a failure while cleaning
// up an emptied parent is reported (with the entry already counted as removed)
// rather than swallowed.
func TestPruneCacheEntriesSurfacesParentCleanupError(t *testing.T) {
	skipWithoutPermissionGating(t)
	t.Setenv("AGENTS_HOME", t.TempDir())
	root := configCacheRoot()
	dir := seedCacheEntry(t, "team", "base.json", "shacleanup", layerCacheFileName, []byte("x"))
	// The entry's own removal succeeds (its parent is writable), but removing the
	// then-empty parent needs write access on the source directory.
	source := filepath.Join(root, "team")
	if err := os.Chmod(source, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(source, 0o755) })

	removed, _, err := PruneCacheEntries(root, []CacheEntry{{Path: dir}})
	if err == nil {
		t.Fatal("expected the blocked parent cleanup to surface an error")
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want the entry counted before the cleanup failure", removed)
	}
}

// TestDedupeSortedProjectPaths covers the path normalization the scan relies on:
// blanks dropped, duplicates (including uncleaned spellings) collapsed, output
// sorted.
func TestDedupeSortedProjectPaths(t *testing.T) {
	// Built through filepath so the expectations hold on Windows too, where a
	// cleaned path uses backslash separators.
	a, b := filepath.FromSlash("/a"), filepath.FromSlash("/b")
	got := dedupeSorted([]string{b + string(filepath.Separator), "  ", filepath.Join(a, "x", ".."), a, b})
	want := []string{a, b}
	if len(got) != len(want) {
		t.Fatalf("dedupeSorted = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("dedupeSorted = %#v, want %#v", got, want)
		}
	}
}

// TestNewCacheEntryHandlesShallowPaths covers the defensive branches of the
// coordinate split: a directory directly under the root carries no unit path.
func TestNewCacheEntryHandlesShallowPaths(t *testing.T) {
	root := filepath.Join("/tmp", "cache", "config")
	loose := filepath.Join(root, "loose-file-dir")
	shallow := newCacheEntry(cacheEntryRel(root, loose), loose, 7)
	if shallow.SourceID != "" || shallow.UnitPath != "" || shallow.Digest != "loose-file-dir" {
		t.Fatalf("shallow entry = %#v", shallow)
	}
	deep := filepath.Join(root, "team", "a", "b", "sha")
	nested := newCacheEntry(cacheEntryRel(root, deep), deep, 9)
	if nested.SourceID != "team" || nested.UnitPath != "a/b" || nested.Digest != "sha" {
		t.Fatalf("nested entry = %#v", nested)
	}
}
