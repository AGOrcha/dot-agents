package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPromptUnitRefFor(t *testing.T) {
	sources := SourceIndex([]Source{{ID: "team", Type: "git", URL: "file:///team"}})
	cases := []struct {
		name      string
		entry     PromptFileRef
		wantOK    bool
		wantKey   string
		wantPath  string
		wantOwner string
	}{
		{
			name:      "typed object is canonical",
			entry:     PromptFileRef{Source: "team", Path: "verifiers/ts-lint.md"},
			wantOK:    true,
			wantKey:   "team:verifiers/ts-lint.md",
			wantPath:  "verifiers/ts-lint.md",
			wantOwner: "team",
		},
		{
			name:      "typed object keeps its version pin",
			entry:     PromptFileRef{Source: "team", Path: "verifiers/base.md", Version: "v2"},
			wantOK:    true,
			wantKey:   "team:verifiers/base.md@v2",
			wantPath:  "verifiers/base.md",
			wantOwner: "team",
		},
		{
			// An undeclared source on the TYPED form is honored: the author was
			// explicit, so the gap surfaces as an unresolved prompt rather than a
			// silent downgrade to a local path.
			name:      "typed object with undeclared source stays source-qualified",
			entry:     PromptFileRef{Source: "ghost", Path: "verifiers/x.md"},
			wantOK:    true,
			wantKey:   "ghost:verifiers/x.md",
			wantPath:  "verifiers/x.md",
			wantOwner: "ghost",
		},
		{
			name:      "colon string with declared prefix",
			entry:     PromptFileRef{Path: "team:verifiers/ts-lint.md"},
			wantOK:    true,
			wantKey:   "team:verifiers/ts-lint.md",
			wantPath:  "verifiers/ts-lint.md",
			wantOwner: "team",
		},
		{
			name:      "colon string carries a version pin",
			entry:     PromptFileRef{Path: "team:verifiers/ts-lint.md@abc123"},
			wantOK:    true,
			wantKey:   "team:verifiers/ts-lint.md@abc123",
			wantPath:  "verifiers/ts-lint.md",
			wantOwner: "team",
		},
		{name: "colon string with undeclared prefix stays local", entry: PromptFileRef{Path: "nope:verifiers/x.md"}},
		{name: "plain local path", entry: PromptFileRef{Path: "verifiers/unit.md"}},
		{name: "windows drive letter stays local", entry: PromptFileRef{Path: `C:\prompts\unit.md`}},
		{name: "empty path", entry: PromptFileRef{Source: "team"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ref, ok := PromptUnitRefFor(tc.entry, sources)
			if ok != tc.wantOK {
				t.Fatalf("qualified = %t, want %t (ref %#v)", ok, tc.wantOK, ref)
			}
			if !ok {
				return
			}
			if ref.Key() != tc.wantKey {
				t.Fatalf("key = %q, want %q", ref.Key(), tc.wantKey)
			}
			if ref.Path != tc.wantPath || ref.SourceID != tc.wantOwner {
				t.Fatalf("ref = %#v, want path %q source %q", ref, tc.wantPath, tc.wantOwner)
			}
			if ref.String() != ref.Key() {
				t.Fatalf("String() = %q, want %q", ref.String(), ref.Key())
			}
		})
	}
}

func TestPromptUnitRefsDedupesAndSorts(t *testing.T) {
	rc := AgentsRC{
		Sources: []Source{{ID: "team", Type: "git", URL: "file:///team"}},
		StageProfiles: map[string]map[string]StageProfile{
			"verifier": {
				"ts-lint": {PromptFiles: []PromptFileRef{
					{Path: "team:verifiers/verifier.base.md"},
					{Source: "team", Path: "verifiers/ts-lint.md"},
					{Path: "verifiers/local.md"},
				}},
				"unit": {PromptFiles: []PromptFileRef{
					// same unit as ts-lint's base entry, declared the other way
					{Source: "team", Path: "verifiers/verifier.base.md"},
				}},
			},
			"reviewer": {
				"arch": {PromptFiles: []PromptFileRef{{Source: "team", Path: "reviewers/arch.md"}}},
			},
		},
	}
	got := PromptUnitRefs(rc)
	want := []string{
		"team:reviewers/arch.md",
		"team:verifiers/ts-lint.md",
		"team:verifiers/verifier.base.md",
	}
	if len(got) != len(want) {
		t.Fatalf("refs = %#v, want %v", got, want)
	}
	for i, key := range want {
		if got[i].Key() != key {
			t.Fatalf("ref[%d] = %q, want %q", i, got[i].Key(), key)
		}
	}
	if len(PromptUnitRefs(AgentsRC{})) != 0 {
		t.Fatal("a config with no stage_profiles must declare no prompt units")
	}
}

// promptProject writes a repo manifest declaring a git source and a stage
// profile whose prompt_files are source-qualified, plus an isolated AGENTS_HOME
// so the prompt cache is hermetic. It returns the project path.
func promptProject(t *testing.T, manifest string) string {
	t.Helper()
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, AgentsRCFile), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return repo
}

const promptManifest = `{
  "project": "p", "version": 1,
  "sources": [{"id": "team", "type": "git", "url": "file:///team", "ref": "master"}],
  "stage_profiles": {"verifier": {"ts-lint": {"prompt_files": [
    {"source": "team", "path": "verifiers/verifier.base.md"},
    "team:verifiers/ts-lint.md",
    "verifiers/local.md"
  ]}}}
}`

// gitPromptFetcher builds a gitFetcher wired to the cloner test seam over a real
// committed fixture repo, so the prompt fetch exercises the SAME git plumbing an
// extends layer uses without touching the network.
func gitPromptFetcher(t *testing.T, files map[string][]byte) *gitFetcher {
	t.Helper()
	dir := t.TempDir()
	makeGitFixtureAt(t, dir, "seed.json", []byte("{}"))
	return &gitFetcher{cloner: committedRepoFS(t, dir, files)}
}

// TestResolveFetchesAndPinsPromptUnits is the sync-time half of the contract: a
// resolve fetches every source-qualified prompt file, caches it
// content-addressed, and pins it in the lock as a kind:prompt unit whose digest
// locates the cached bytes offline.
func TestResolveFetchesAndPinsPromptUnits(t *testing.T) {
	repo := promptProject(t, promptManifest)
	fetcher := gitPromptFetcher(t, map[string][]byte{
		"verifiers/verifier.base.md": []byte("# base\n"),
		"verifiers/ts-lint.md":       []byte("# ts-lint\n"),
	})
	if _, err := NewLayeredResolver().WithFetcher("git", fetcher).Resolve(repo); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	units, err := ReadUnits(repo)
	if err != nil {
		t.Fatalf("read units: %v", err)
	}
	for _, ref := range []PromptUnitRef{
		{SourceID: "team", Path: "verifiers/verifier.base.md"},
		{SourceID: "team", Path: "verifiers/ts-lint.md"},
	} {
		unit, ok := units.Units[ref.Key()]
		if !ok {
			t.Fatalf("lock is missing prompt unit %q; units = %#v", ref.Key(), units.Units)
		}
		if unit.Kind != UnitKindPrompt || unit.Digest == "" {
			t.Fatalf("unit %q = %#v, want kind %q with a digest", ref.Key(), unit, UnitKindPrompt)
		}
		path, ok := LockedPromptFile(units.Units, ref)
		if !ok {
			t.Fatalf("cached bytes for %q not resolvable offline", ref.Key())
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("cached prompt %q: %v", path, err)
		}
	}
	// The local (non source-qualified) entry is never turned into a unit.
	if _, ok := units.Units["verifiers/local.md"]; ok {
		t.Fatal("a local prompt path must not be pinned as a unit")
	}
	// Pruning the cache makes the offline probe fail closed (the caller then
	// emits the `da config sync` hint) even though the pin is still recorded.
	if err := os.RemoveAll(filepath.Join(AgentsHome(), "cache")); err != nil {
		t.Fatal(err)
	}
	if _, ok := LockedPromptFile(units.Units, PromptUnitRef{SourceID: "team", Path: "verifiers/ts-lint.md"}); ok {
		t.Fatal("a pruned cache must not resolve")
	}
}

// TestResolveOfflineCarriesPromptUnits proves the offline resolve serves the
// pinned prompt units from the lock with ZERO fetches — the R5 offline replay
// property extended to prompt content.
func TestResolveOfflineCarriesPromptUnits(t *testing.T) {
	repo := promptProject(t, promptManifest)
	fetcher := gitPromptFetcher(t, map[string][]byte{
		"verifiers/verifier.base.md": []byte("# base\n"),
		"verifiers/ts-lint.md":       []byte("# ts-lint\n"),
	})
	if _, err := NewLayeredResolver().WithFetcher("git", fetcher).Resolve(repo); err != nil {
		t.Fatalf("seed resolve: %v", err)
	}
	before, err := ReadUnits(repo)
	if err != nil {
		t.Fatal(err)
	}

	// An offline resolve with a fetcher that would fail on any call.
	boom := &fakeFetcher{fetchErr: os.ErrPermission}
	if _, err := NewLayeredResolver().WithFetcher("git", boom).WithOffline(true).Resolve(repo); err != nil {
		t.Fatalf("offline resolve: %v", err)
	}
	if boom.calls != 0 {
		t.Fatalf("offline resolve made %d fetches, want 0", boom.calls)
	}
	after, err := ReadUnits(repo)
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range before.Units {
		if want.Kind != UnitKindPrompt {
			continue
		}
		got, ok := after.Units[key]
		if !ok || got.Digest != want.Digest {
			t.Fatalf("offline resolve dropped prompt pin %q: %#v", key, after.Units)
		}
	}
}

// TestPromptUnitFetchFailureIsNonFatal proves a prompt that cannot be fetched
// does not fail the resolve: it warns, keeps the previous pin when there is one,
// and leaves the rest of the effective config intact.
func TestPromptUnitFetchFailureIsNonFatal(t *testing.T) {
	repo := promptProject(t, promptManifest)
	good := gitPromptFetcher(t, map[string][]byte{
		"verifiers/verifier.base.md": []byte("# base\n"),
		"verifiers/ts-lint.md":       []byte("# ts-lint\n"),
	})
	if _, err := NewLayeredResolver().WithFetcher("git", good).Resolve(repo); err != nil {
		t.Fatalf("seed resolve: %v", err)
	}

	// Second resolve: the source no longer serves ts-lint.md.
	partial := &fakeFetcher{files: map[string]string{"verifiers/verifier.base.md": "# base\n"}}
	snap, err := NewLayeredResolver().WithFetcher("git", partial).Resolve(repo)
	if err != nil {
		t.Fatalf("resolve with a missing prompt must not fail: %v", err)
	}
	var warned bool
	for _, w := range snap.Warnings {
		if w.AttemptedByLayer == "team:verifiers/ts-lint.md" {
			warned = true
		}
	}
	if !warned {
		t.Fatalf("expected a prompt_skipped warning, got %#v", snap.Warnings)
	}
	units, err := ReadUnits(repo)
	if err != nil {
		t.Fatal(err)
	}
	if u, ok := units.Units["team:verifiers/ts-lint.md"]; !ok || u.Kind != UnitKindPrompt {
		t.Fatalf("a failed re-fetch must keep the previous pin, got %#v", units.Units)
	}
}

// TestPromptUnitsDroppedWhenUndeclared proves the prompt set is pass 1's own: a
// prompt_files entry removed from the config drops out of the lock on the next
// resolve (no permanently stale pins), while sibling kinds survive.
func TestPromptUnitsDroppedWhenUndeclared(t *testing.T) {
	repo := promptProject(t, promptManifest)
	fetcher := gitPromptFetcher(t, map[string][]byte{
		"verifiers/verifier.base.md": []byte("# base\n"),
		"verifiers/ts-lint.md":       []byte("# ts-lint\n"),
	})
	if _, err := NewLayeredResolver().WithFetcher("git", fetcher).Resolve(repo); err != nil {
		t.Fatalf("seed resolve: %v", err)
	}
	trimmed := `{
  "project": "p", "version": 1,
  "sources": [{"id": "team", "type": "git", "url": "file:///team", "ref": "master"}],
  "stage_profiles": {"verifier": {"ts-lint": {"prompt_files": [{"source": "team", "path": "verifiers/verifier.base.md"}]}}}
}`
	if err := os.WriteFile(filepath.Join(repo, AgentsRCFile), []byte(trimmed), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewLayeredResolver().WithFetcher("git", fetcher).Resolve(repo); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	units, err := ReadUnits(repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := units.Units["team:verifiers/ts-lint.md"]; ok {
		t.Fatalf("an undeclared prompt must drop out of the lock, got %#v", units.Units)
	}
	if _, ok := units.Units["team:verifiers/verifier.base.md"]; !ok {
		t.Fatalf("the still-declared prompt must stay pinned, got %#v", units.Units)
	}
}

// TestPromptUnitLockShapeIsAdditive proves an existing lock stays valid: the
// prompt entries land in the SAME units section as layer/artifact/profile units,
// under the same "source:path" key grammar, and a lock written before prompt
// units existed still reads.
func TestPromptUnitLockShapeIsAdditive(t *testing.T) {
	repo := promptProject(t, promptManifest)
	// A pre-existing lock with only an artifact unit (as a packages pass wrote it).
	if err := WriteUnitsLock(repo, UnitsLock{Units: map[string]LockedUnit{
		"team:tools/fmt@1.0.0": {Kind: UnitKindArtifact, Digest: "sha256:abc"},
	}}); err != nil {
		t.Fatal(err)
	}
	fetcher := gitPromptFetcher(t, map[string][]byte{
		"verifiers/verifier.base.md": []byte("# base\n"),
		"verifiers/ts-lint.md":       []byte("# ts-lint\n"),
	})
	if _, err := NewLayeredResolver().WithFetcher("git", fetcher).Resolve(repo); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	raw, err := os.ReadFile(AgentsLockPath(repo))
	if err != nil {
		t.Fatal(err)
	}
	var lock struct {
		Units map[string]LockedUnit `json:"units"`
	}
	if err := json.Unmarshal(raw, &lock); err != nil {
		t.Fatalf("lock must stay valid JSON: %v\n%s", err, raw)
	}
	if u, ok := lock.Units["team:tools/fmt@1.0.0"]; !ok || u.Kind != UnitKindArtifact {
		t.Fatalf("the pre-existing artifact unit must survive, got %#v", lock.Units)
	}
	if u, ok := lock.Units["team:verifiers/ts-lint.md"]; !ok || u.Kind != UnitKindPrompt {
		t.Fatalf("prompt unit missing from the units section, got %#v", lock.Units)
	}
	if !KnownUnitKind(UnitKindPrompt) || !IsProjectableKind(UnitKindPrompt) || !IsSyncedUnitKind(UnitKindPrompt) {
		t.Fatal("the prompt kind must be a known, projectable, synced unit kind")
	}
	if err := ValidateUnitKind(UnitKindPrompt); err != nil {
		t.Fatalf("prompt kind must validate: %v", err)
	}
}
