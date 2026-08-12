package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-billy/v6"
	gogit "github.com/go-git/go-git/v6"
)

// promptCacheManifest declares one git source that supplies BOTH an extends
// layer and two source-qualified prompt files, so a single resolve exercises the
// layer tier and the prompt tier against the same source.
const promptCacheManifest = `{
  "project": "p", "version": 1,
  "sources": [{"id": "team", "type": "git", "url": "file:///team", "ref": "master"}],
  "extends": ["team:team/base.json"],
  "stage_profiles": {"verifier": {"ts-lint": {"prompt_files": [
    {"source": "team", "path": "verifiers/verifier.base.md"},
    "team:verifiers/ts-lint.md"
  ]}}}
}`

// promptCacheSourceFiles is the fixture source tree promptCacheManifest consumes.
func promptCacheSourceFiles() map[string][]byte {
	return map[string][]byte{
		"team/base.json":             []byte(`{"version":2}`),
		"verifiers/verifier.base.md": []byte("# base\n"),
		"verifiers/ts-lint.md":       []byte("# ts-lint\n"),
	}
}

// countingGitFetcher wires a gitFetcher to the committed-repo cloner seam and
// counts how many clones actually reach it, so a test can assert the per-resolve
// (url, ref) memo collapses N units from one source into ONE clone.
func countingGitFetcher(t *testing.T, files map[string][]byte) (*gitFetcher, *int) {
	t.Helper()
	dir := t.TempDir()
	makeGitFixtureAt(t, dir, "seed.json", []byte("{}"))
	inner := committedRepoFS(t, dir, files)
	clones := 0
	fetcher := &gitFetcher{cloner: func(ctx context.Context, url, ref string) (*gogit.Repository, billy.Filesystem, error) {
		clones++
		return inner(ctx, url, ref)
	}}
	return fetcher, &clones
}

// TestCacheFileName covers the basename derivation that gives a prompt unit a
// real cached file name: an ordinary source-relative path yields its basename,
// while any path whose basename could not safely name a file inside the <sha>/
// entry falls back to the layer default.
func TestCacheFileName(t *testing.T) {
	cases := []struct {
		name string
		path string
		want string
	}{
		{"nested prompt", "verifiers/ts-lint.md", "ts-lint.md"},
		{"bare file", "ts-lint.md", "ts-lint.md"},
		{"surrounding space", "  verifiers/ts-lint.md  ", "ts-lint.md"},
		{"windows separators", `verifiers\ts-lint.md`, "ts-lint.md"},
		{"layer path keeps json name", "team/base.json", "base.json"},
		{"empty", "", layerCacheFileName},
		{"dot", ".", layerCacheFileName},
		{"parent", "..", layerCacheFileName},
		{"root", "/", layerCacheFileName},
		{"colon in basename", "verifiers/a:b.md", layerCacheFileName},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := cacheFileName(tc.path); got != tc.want {
				t.Fatalf("cacheFileName(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

// TestFetchTargetDefaultsToLayerFileName proves a zero-value target still writes
// (and reads) the historical layer.json name, so the layer cache layout is
// unchanged by the file-name threading.
func TestFetchTargetDefaultsToLayerFileName(t *testing.T) {
	target := FetchTarget{Dir: t.TempDir()}
	if got := filepath.Base(target.pathFor("abc")); got != layerCacheFileName {
		t.Fatalf("default cached file = %q, want %q", got, layerCacheFileName)
	}
	if err := writeCachedUnit(target, "abc", []byte("{}")); err != nil {
		t.Fatalf("writeCachedUnit: %v", err)
	}
	if _, ok := readCachedUnit(target, "abc"); !ok {
		t.Fatal("bytes written under the default name must read back")
	}
}

// TestPromptUnitCachesUnderRealBasename is the item-1 contract: a fetched prompt
// caches as <sha>/<its own basename> (not <sha>/layer.json), the bytes are the
// prompt's own, and the layer tier keeps caching as <sha>/layer.json.
func TestPromptUnitCachesUnderRealBasename(t *testing.T) {
	repo := promptProject(t, promptCacheManifest)
	fetcher, _ := countingGitFetcher(t, promptCacheSourceFiles())
	if _, err := NewLayeredResolver().WithFetcher("git", fetcher).Resolve(repo); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	units, err := ReadUnits(repo)
	if err != nil {
		t.Fatal(err)
	}

	ref := PromptUnitRef{SourceID: "team", Path: "verifiers/ts-lint.md"}
	path := assertCachedPrompt(t, units.Units, ref, "# ts-lint\n")
	if base := filepath.Base(path); base != "ts-lint.md" {
		t.Fatalf("cached prompt file = %q, want its real basename %q (path %s)", base, "ts-lint.md", path)
	}

	layerUnit, ok := units.Units["team:team/base.json"]
	if !ok {
		t.Fatalf("expected a locked layer unit, got %#v", units.Units)
	}
	layerPath := layerTarget("team", "team/base.json").pathFor(layerUnit.Digest)
	if filepath.Base(layerPath) != layerCacheFileName {
		t.Fatalf("layer cache file = %q, want unchanged %q", filepath.Base(layerPath), layerCacheFileName)
	}
	if _, err := os.Stat(layerPath); err != nil {
		t.Fatalf("layer bytes must stay cached at %s: %v", layerPath, err)
	}
}

// assertCachedPrompt resolves a pinned prompt unit offline and asserts its cached
// bytes match want, returning the resolved cache path.
func assertCachedPrompt(t *testing.T, units map[string]LockedUnit, ref PromptUnitRef, want string) string {
	t.Helper()
	path, ok := LockedPromptFile(units, ref)
	if !ok {
		t.Fatalf("prompt %q did not resolve offline; units = %#v", ref.Key(), units)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading cached prompt %s: %v", path, err)
	}
	if string(data) != want {
		t.Fatalf("cached prompt bytes = %q, want %q", data, want)
	}
	return path
}

// TestPromptUnitCacheKeyIsCommitPinned is the item-2 contract: the recorded
// prompt cache key is the commit-pinned CONTENT key even on a `--refresh` run
// (which `da config sync` always is), so a transient runtime escape is not frozen
// into the lock — while a source that DECLARES always_revalidate still records
// the sentinel.
func TestPromptUnitCacheKeyIsCommitPinned(t *testing.T) {
	cases := []struct {
		name         string
		cacheKeys    string
		refresh      bool
		wantSentinel bool
	}{
		{name: "plain resolve"},
		{name: "refresh run", refresh: true},
		{name: "refresh run with a cache_ttl", cacheKeys: `, "cache_ttl": "1h"`, refresh: true},
		{
			name:         "source declares always_revalidate",
			cacheKeys:    `, "cache_keys": {"always_revalidate": true}`,
			wantSentinel: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			unit := resolvePromptUnit(t, tc.cacheKeys, tc.refresh)
			assertPromptCacheKey(t, unit, tc.wantSentinel)
		})
	}
}

// resolvePromptUnit resolves a one-source project (with the given extra source
// JSON fields) and returns the pinned ts-lint prompt unit.
func resolvePromptUnit(t *testing.T, sourceExtra string, refresh bool) LockedUnit {
	t.Helper()
	manifest := `{
  "project": "p", "version": 1,
  "sources": [{"id": "team", "type": "git", "url": "file:///team", "ref": "master"` + sourceExtra + `}],
  "stage_profiles": {"verifier": {"ts-lint": {"prompt_files": ["team:verifiers/ts-lint.md"]}}}
}`
	repo := promptProject(t, manifest)
	fetcher, _ := countingGitFetcher(t, promptCacheSourceFiles())
	if _, err := NewLayeredResolver().WithFetcher("git", fetcher).WithRefresh(refresh).Resolve(repo); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	units, err := ReadUnits(repo)
	if err != nil {
		t.Fatal(err)
	}
	unit, ok := units.Units["team:verifiers/ts-lint.md"]
	if !ok {
		t.Fatalf("prompt unit missing from the lock: %#v", units.Units)
	}
	return unit
}

// assertPromptCacheKey checks the recorded key against the commit-pinned default
// (or the always-revalidate sentinel when the source declares it).
func assertPromptCacheKey(t *testing.T, unit LockedUnit, wantSentinel bool) {
	t.Helper()
	if wantSentinel {
		if unit.CacheKey != AlwaysRevalidate {
			t.Fatalf("cache_key = %q, want the declared always-revalidate sentinel", unit.CacheKey)
		}
		return
	}
	want := DefaultCacheKey(SourceKindGit, CacheKeyInputs{ResolvedCommit: unit.Digest})
	if unit.CacheKey != want {
		t.Fatalf("cache_key = %q, want the commit-pinned %q", unit.CacheKey, want)
	}
	if strings.Contains(unit.CacheKey, "always-revalidate") {
		t.Fatalf("a per-resolve --refresh must not be recorded in the lock: %q", unit.CacheKey)
	}
}

// TestGitFetcherMemoizesClonePerResolve is the item-3 contract: one source that
// supplies a layer plus two prompt files costs ONE clone per resolve, and the
// memo does not survive into the next resolve.
func TestGitFetcherMemoizesClonePerResolve(t *testing.T) {
	repo := promptProject(t, promptCacheManifest)
	fetcher, clones := countingGitFetcher(t, promptCacheSourceFiles())
	resolver := NewLayeredResolver().WithFetcher("git", fetcher)

	if _, err := resolver.Resolve(repo); err != nil {
		t.Fatalf("first resolve: %v", err)
	}
	if *clones != 1 {
		t.Fatalf("first resolve made %d clones for 1 source (1 layer + 2 prompts), want 1", *clones)
	}

	if _, err := resolver.Resolve(repo); err != nil {
		t.Fatalf("second resolve: %v", err)
	}
	if *clones != 2 {
		t.Fatalf("clones after a second resolve = %d, want 2 (the memo must not outlive a resolve)", *clones)
	}
}

// TestGitFetcherMemoizesCloneFailure proves a failing source is cloned once per
// resolve too: the memoized error is replayed for the remaining units instead of
// each unit paying its own failed clone.
func TestGitFetcherMemoizesCloneFailure(t *testing.T) {
	repo := promptProject(t, promptCacheManifest)
	attempts := 0
	fetcher := &gitFetcher{cloner: func(context.Context, string, string) (*gogit.Repository, billy.Filesystem, error) {
		attempts++
		return nil, nil, os.ErrPermission
	}}
	// The extends layer fails the resolve (non-optional), but the clone must have
	// been attempted exactly once regardless of how many units wanted the source.
	if _, err := NewLayeredResolver().WithFetcher("git", fetcher).Resolve(repo); err == nil {
		t.Fatal("expected the failing source to fail the resolve")
	}
	if attempts != 1 {
		t.Fatalf("clone attempts = %d, want 1 memoized failure", attempts)
	}
}

// TestDefaultFetcherIsSharedWithinOneResolve proves fetcherFor hands the SAME
// default fetcher instance to every unit of a type within a resolve (the sharing
// the clone memo rides on) and a fresh instance after beginResolve.
func TestDefaultFetcherIsSharedWithinOneResolve(t *testing.T) {
	r := NewLayeredResolver()
	r.beginResolve()
	first, err := r.fetcherFor("git")
	if err != nil {
		t.Fatal(err)
	}
	again, err := r.fetcherFor("git")
	if err != nil {
		t.Fatal(err)
	}
	if first != again {
		t.Fatal("units within one resolve must share a default fetcher instance")
	}
	r.beginResolve()
	next, err := r.fetcherFor("git")
	if err != nil {
		t.Fatal(err)
	}
	if next == first {
		t.Fatal("a new resolve must start from a fresh default fetcher")
	}
	if _, err := r.fetcherFor("carrier-pigeon"); err == nil {
		t.Fatal("an unsupported source type must still error")
	}
}

// TestBeginResolveResetsInjectedFetcherMemo proves the reset reaches an INJECTED
// fetcher too, so a test seam (or any future memoizing fetcher) cannot serve a
// tree cloned during an earlier resolve.
func TestBeginResolveResetsInjectedFetcherMemo(t *testing.T) {
	fetcher, clones := countingGitFetcher(t, promptCacheSourceFiles())
	ctx := context.Background()
	if _, _, err := fetcher.clone(ctx, "file:///team", "refs/heads/master"); err != nil {
		t.Fatalf("clone: %v", err)
	}
	if _, _, err := fetcher.clone(ctx, "file:///team", "refs/heads/master"); err != nil {
		t.Fatalf("memoized clone: %v", err)
	}
	if *clones != 1 {
		t.Fatalf("clones = %d, want 1 (second call must hit the memo)", *clones)
	}
	NewLayeredResolver().WithFetcher("git", fetcher).beginResolve()
	if _, _, err := fetcher.clone(ctx, "file:///team", "refs/heads/master"); err != nil {
		t.Fatalf("post-reset clone: %v", err)
	}
	if *clones != 2 {
		t.Fatalf("clones = %d, want 2 after the memo reset", *clones)
	}
}
