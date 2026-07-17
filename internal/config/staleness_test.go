package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFileContent drops body at path (creating parent dirs), for seeding the
// local config scopes a staleness check hashes.
func writeFileContent(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// stalenessSeed sets up a project with a repo-local manifest and a separate
// user-local manifest path, returning both the project dir and user-local path
// so a test can mutate either scope and recompute.
func stalenessSeed(t *testing.T, repoManifest, userManifest string) (repo, userPath string) {
	t.Helper()
	repo = t.TempDir()
	writeManifest(t, repo, repoManifest)
	userPath = filepath.Join(t.TempDir(), AgentsRCFile)
	if userManifest != "" {
		writeFileContent(t, userPath, userManifest)
	}
	return repo, userPath
}

func TestComputeInputsDigest_StableAndPrefixed(t *testing.T) {
	repo, userPath := stalenessSeed(t, `{"version":2,"project":"x"}`, `{"version":2}`)

	d1, err := ComputeInputsDigest(repo, userPath)
	if err != nil {
		t.Fatalf("ComputeInputsDigest: %v", err)
	}
	if !strings.HasPrefix(d1, inputsDigestPrefix) {
		t.Errorf("expected %s prefix, got %q", inputsDigestPrefix, d1)
	}
	d2, err := ComputeInputsDigest(repo, userPath)
	if err != nil {
		t.Fatal(err)
	}
	if d1 != d2 {
		t.Errorf("digest not stable across calls: %q vs %q", d1, d2)
	}
}

func TestComputeInputsDigest_InsensitiveToWhitespaceAndKeyOrder(t *testing.T) {
	repoA, userA := stalenessSeed(t, `{"version":2,"project":"x"}`, "")
	repoB, userB := stalenessSeed(t, "{\n  \"project\":   \"x\",\n  \"version\": 2\n}\n", "")

	da, err := ComputeInputsDigest(repoA, userA)
	if err != nil {
		t.Fatal(err)
	}
	db, err := ComputeInputsDigest(repoB, userB)
	if err != nil {
		t.Fatal(err)
	}
	if da != db {
		t.Errorf("expected whitespace/key-order-insensitive digest, got %q vs %q", da, db)
	}
}

func TestComputeInputsDigest_ChangesWhenAnyScopeChanges(t *testing.T) {
	repo, userPath := stalenessSeed(t, `{"version":2}`, `{"version":2}`)
	base, err := ComputeInputsDigest(repo, userPath)
	if err != nil {
		t.Fatal(err)
	}

	// Mutate the project-local overlay scope only.
	writeFileContent(t, filepath.Join(repo, AgentsRCLocalFile), `{"project":"overlay"}`)
	withOverlay, err := ComputeInputsDigest(repo, userPath)
	if err != nil {
		t.Fatal(err)
	}
	if base == withOverlay {
		t.Error("expected digest to change when project-local overlay appears")
	}
}

func TestComputeInputsDigest_MissingScopesAreEmptyNotError(t *testing.T) {
	repo := t.TempDir()
	writeManifest(t, repo, `{"version":2}`)
	// No user-local file, no overlay.
	missingUser := filepath.Join(t.TempDir(), AgentsRCFile)

	if _, err := ComputeInputsDigest(repo, missingUser); err != nil {
		t.Fatalf("missing scopes should not error: %v", err)
	}
}

func TestComputeInputsDigest_MalformedManifestErrors(t *testing.T) {
	repo := t.TempDir()
	writeManifest(t, repo, `{not json`)
	if _, err := ComputeInputsDigest(repo, ""); err == nil {
		t.Fatal("expected error for malformed manifest")
	}
}

// TestComputeInputsDigest_PreservesLargeIntegerPrecision is the regression for
// the HIGH false-fresh defect: decoding into `any` coerces every number to
// float64, so two integers beyond 2^53 that differ by 1 would collapse to the
// same value and hash identically — a real config change going undetected. The
// canonicalizer must distinguish them.
func TestComputeInputsDigest_PreservesLargeIntegerPrecision(t *testing.T) {
	// 9007199254740993 = 2^53 + 1; as float64 it rounds to 2^53 (…992), so a
	// lossy normalizer would produce the same bytes for both manifests.
	repoA, userA := stalenessSeed(t, `{"id":9007199254740993}`, "")
	repoB, userB := stalenessSeed(t, `{"id":9007199254740992}`, "")

	da, err := ComputeInputsDigest(repoA, userA)
	if err != nil {
		t.Fatal(err)
	}
	db, err := ComputeInputsDigest(repoB, userB)
	if err != nil {
		t.Fatal(err)
	}
	if da == db {
		t.Errorf("large integers differing by 1 must produce different digests (float64 precision loss), got %q for both", da)
	}
}

// TestComputeInputsDigest_RejectsDuplicateKeys is the MEDIUM false-fresh
// regression: `{"a":1,"a":2}` is parseable but ambiguous; silently collapsing
// it (last-wins) hides a content change. The canonicalizer must error instead.
func TestComputeInputsDigest_RejectsDuplicateKeys(t *testing.T) {
	repo := t.TempDir()
	writeManifest(t, repo, `{"a":1,"a":2}`)
	if _, err := ComputeInputsDigest(repo, ""); err == nil {
		t.Fatal("expected error for duplicate object key")
	}
}

// TestComputeInputsDigest_RejectsDuplicateKeysNested ensures the duplicate-key
// check recurses into nested objects, not just the top level.
func TestComputeInputsDigest_RejectsDuplicateKeysNested(t *testing.T) {
	repo := t.TempDir()
	writeManifest(t, repo, `{"outer":{"k":1,"k":2}}`)
	if _, err := ComputeInputsDigest(repo, ""); err == nil {
		t.Fatal("expected error for duplicate key in nested object")
	}
}

// TestComputeInputsDigest_RejectsTrailingData ensures two concatenated JSON
// documents (a malformed manifest) are rejected rather than silently hashing
// only the first.
func TestComputeInputsDigest_RejectsTrailingData(t *testing.T) {
	repo := t.TempDir()
	writeManifest(t, repo, `{"a":1}{"b":2}`)
	if _, err := ComputeInputsDigest(repo, ""); err == nil {
		t.Fatal("expected error for trailing data after top-level value")
	}
}

// TestComputeInputsDigest_MalformedVariantsError covers the canonicalizer's
// error branches that are reachable with deliberately malformed manifests: a
// bare close-delimiter at a value position, an unterminated object, and a
// non-string object key are each rejected rather than silently normalized.
func TestComputeInputsDigest_MalformedVariantsError(t *testing.T) {
	cases := map[string]string{
		"bare close delim":    `}`,
		"unterminated object": `{"a":1`,
		"unterminated array":  `[1,2`,
		"truncated value":     `{"a":`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			repo := t.TempDir()
			writeManifest(t, repo, body)
			if _, err := ComputeInputsDigest(repo, ""); err == nil {
				t.Fatalf("expected error normalizing %q", body)
			}
		})
	}
}

// TestComputeInputsDigest_NormalizesNestedArraysAndScalars exercises the
// array/scalar canonicalization branches (order-preserving arrays, bool/null,
// nested objects) so the recursive walk is fully covered.
func TestComputeInputsDigest_NormalizesNestedArraysAndScalars(t *testing.T) {
	// Same semantic content, differing only in key order and whitespace inside a
	// nested object within an array, plus bool/null scalars.
	repoA, userA := stalenessSeed(t, `{"list":[{"b":true,"a":null},2,"s"]}`, "")
	repoB, userB := stalenessSeed(t, "{\n \"list\": [ {\"a\":null,\"b\":true}, 2, \"s\" ]\n}", "")

	da, err := ComputeInputsDigest(repoA, userA)
	if err != nil {
		t.Fatal(err)
	}
	db, err := ComputeInputsDigest(repoB, userB)
	if err != nil {
		t.Fatal(err)
	}
	if da != db {
		t.Errorf("nested array/scalar normalization should be order/whitespace-insensitive, got %q vs %q", da, db)
	}

	// Array element order IS significant — reordering must change the digest.
	repoC, userC := stalenessSeed(t, `{"list":["x","y"]}`, "")
	repoD, userD := stalenessSeed(t, `{"list":["y","x"]}`, "")
	dc, _ := ComputeInputsDigest(repoC, userC)
	dd, _ := ComputeInputsDigest(repoD, userD)
	if dc == dd {
		t.Error("array element order must be significant (digests should differ)")
	}
}

func TestStaleness_FreshWhenLockMatches(t *testing.T) {
	repo, userPath := stalenessSeed(t, `{"extends":["acme:org/base.json"]}`, "")
	digest, err := ComputeInputsDigest(repo, userPath)
	if err != nil {
		t.Fatal(err)
	}
	seedUnits(t, repo, digest, map[string]LockedUnit{
		"acme:org/base.json@a1": {Kind: UnitKindLayer, Digest: "sha256:d1"},
	})

	res, err := Staleness(repo, userPath, nil)
	if err != nil {
		t.Fatalf("Staleness: %v", err)
	}
	if !res.Fresh || res.IsStale() {
		t.Fatalf("expected fresh lock, got %+v", res)
	}
	if len(res.Reasons) != 0 {
		t.Errorf("expected no reasons, got %+v", res.Reasons)
	}
}

func TestStaleness_InputsDigestMismatch(t *testing.T) {
	repo, userPath := stalenessSeed(t, `{"extends":["acme:org/base.json"]}`, "")
	seedUnits(t, repo, "sha256:stale-digest", map[string]LockedUnit{
		"acme:org/base.json@a1": {Kind: UnitKindLayer, Digest: "sha256:d1"},
	})

	res, err := Staleness(repo, userPath, nil)
	if err != nil {
		t.Fatalf("Staleness: %v", err)
	}
	if !res.IsStale() || !hasReason(res, ReasonInputsDigest) {
		t.Fatalf("expected inputs-digest-mismatch, got %+v", res)
	}
	if res.ExpectedInputsDigest == "" || res.ExpectedInputsDigest == "sha256:stale-digest" {
		t.Errorf("expected a freshly-computed digest, got %q", res.ExpectedInputsDigest)
	}
}

func TestStaleness_DeclaredSetChanged(t *testing.T) {
	repo, userPath := stalenessSeed(t, `{"extends":["acme:org/base.json","acme:org/new.json"]}`, "")
	digest, err := ComputeInputsDigest(repo, userPath)
	if err != nil {
		t.Fatal(err)
	}
	// Lock only has one of the two declared layers.
	seedUnits(t, repo, digest, map[string]LockedUnit{
		"acme:org/base.json@a1": {Kind: UnitKindLayer, Digest: "sha256:d1"},
	})

	res, err := Staleness(repo, userPath, nil)
	if err != nil {
		t.Fatalf("Staleness: %v", err)
	}
	if !res.IsStale() || !hasReason(res, ReasonDeclaredSet) {
		t.Fatalf("expected declared-set-changed, got %+v", res)
	}
}

func TestStaleness_UnitDigestMismatch(t *testing.T) {
	repo, userPath := stalenessSeed(t, `{"extends":["acme:org/base.json"]}`, "")
	digest, err := ComputeInputsDigest(repo, userPath)
	if err != nil {
		t.Fatal(err)
	}
	seedUnits(t, repo, digest, map[string]LockedUnit{
		"acme:org/base.json@a1": {Kind: UnitKindLayer, Digest: "sha256:recorded"},
	})

	// recompute reports a different current digest for the locked unit.
	recompute := func(ref string) (string, bool) {
		if ref == "acme:org/base.json@a1" {
			return "sha256:drifted", true
		}
		return "", false
	}
	res, err := Staleness(repo, userPath, recompute)
	if err != nil {
		t.Fatalf("Staleness: %v", err)
	}
	if !res.IsStale() || !hasReason(res, ReasonUnitDigest) {
		t.Fatalf("expected unit-digest-mismatch, got %+v", res)
	}
}

func TestStaleness_UnitDigestSkippedWhenUnavailable(t *testing.T) {
	repo, userPath := stalenessSeed(t, `{"extends":["acme:org/base.json"]}`, "")
	digest, err := ComputeInputsDigest(repo, userPath)
	if err != nil {
		t.Fatal(err)
	}
	seedUnits(t, repo, digest, map[string]LockedUnit{
		"acme:org/base.json@a1": {Kind: UnitKindLayer, Digest: "sha256:recorded"},
	})

	// recompute always reports "not available" — the digest check is skipped, so
	// the lock stays fresh.
	recompute := func(string) (string, bool) { return "", false }
	res, err := Staleness(repo, userPath, recompute)
	if err != nil {
		t.Fatalf("Staleness: %v", err)
	}
	if !res.Fresh {
		t.Fatalf("expected fresh when digests unavailable, got %+v", res)
	}
}

func TestStaleness_MissingManifestErrors(t *testing.T) {
	repo := t.TempDir()
	if _, err := Staleness(repo, "", nil); err == nil {
		t.Fatal("expected error for missing manifest")
	}
}

func TestStaleness_MalformedOverlayScopeErrors(t *testing.T) {
	// Repo manifest is valid (LoadAgentsRC succeeds) but the project-local
	// overlay scope is malformed, so the inputs-digest computation fails inside
	// Staleness rather than at manifest load.
	repo, userPath := stalenessSeed(t, `{"extends":["acme:org/base.json"]}`, "")
	writeFileContent(t, filepath.Join(repo, AgentsRCLocalFile), `{bad json`)
	if _, err := Staleness(repo, userPath, nil); err == nil {
		t.Fatal("expected error when an overlay scope is malformed")
	}
}

func TestStaleness_CorruptLockfileErrors(t *testing.T) {
	repo, userPath := stalenessSeed(t, `{"extends":["acme:org/base.json"]}`, "")
	writeFileContent(t, AgentsLockPath(repo), `{not valid json`)
	if _, err := Staleness(repo, userPath, nil); err == nil {
		t.Fatal("expected error when the lockfile is corrupt")
	}
}

func TestDeclaredSetChanged_EqualLengthDifferentRef(t *testing.T) {
	rc := AgentsRC{Extends: []LayerRef{{Ref: "acme:org/base"}}}
	// Same cardinality, different identity → changed.
	units := map[string]LockedUnit{"acme:org/other@a1": {Kind: UnitKindLayer}}
	if !declaredSetChanged(rc, units) {
		t.Error("expected change when an equal-sized lock set holds a different ref")
	}
}

func TestDeclaredSetChanged_PackagesCounted(t *testing.T) {
	rc := AgentsRC{
		Extends:  []LayerRef{{Ref: "acme:org/base"}},
		Packages: []PackageRef{{Ref: "acme:skill/x@^1"}},
	}
	// Lock has the layer but not the package → changed.
	units := map[string]LockedUnit{"acme:org/base@a1": {Kind: UnitKindLayer}}
	if !declaredSetChanged(rc, units) {
		t.Error("expected declared-set change when a declared package is unlocked")
	}
	// Lock has both → unchanged.
	units["acme:skill/x@1.0.0"] = LockedUnit{Kind: UnitKindArtifact}
	if declaredSetChanged(rc, units) {
		t.Error("expected no change when every declared unit is locked")
	}
}

// TestDeclaredSetChanged_IgnoresDerivedUnits is the regression guard for the H9
// frozen-no-op break: the resolver folds DERIVED units (kind:profile stage
// profiles, project-set, descriptor) into the lock's units section, but those
// are never declared in the manifest's extends/packages. Counting them in the
// declared-set comparison made declaredSetChanged fire on every run — forcing a
// re-resolve that re-stamped fetched_at/last_checked_at and churned the lock on a
// lock-clean re-install. Only kind:layer and kind:artifact units are declared, so
// derived units must not trip the comparison.
func TestDeclaredSetChanged_IgnoresDerivedUnits(t *testing.T) {
	rc := AgentsRC{Packages: []PackageRef{{Ref: "da-agc:skill/x@main"}}}
	units := map[string]LockedUnit{
		"da-agc:skill/x@main":             {Kind: UnitKindArtifact},
		"user-local:stage-profile:review": {Kind: "profile"},
	}
	if declaredSetChanged(rc, units) {
		t.Error("a kind:profile unit folded into the lock must not trip declared-set-changed")
	}
	// Removing the actually-declared artifact still registers as changed.
	delete(units, "da-agc:skill/x@main")
	if !declaredSetChanged(rc, units) {
		t.Error("expected declared-set change when the declared package is absent from the lock")
	}
}

func TestDeclaredRefOf(t *testing.T) {
	cases := map[string]string{
		"acme:org/base@a1":  "acme:org/base",
		"acme:org/base":     "acme:org/base",
		"local:a/b@c@1.2.3": "local:a/b@c",
	}
	for in, want := range cases {
		if got := declaredRefOf(in); got != want {
			t.Errorf("declaredRefOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func hasReason(r StalenessResult, want StalenessReason) bool {
	for _, got := range r.Reasons {
		if got == want {
			return true
		}
	}
	return false
}

// --- cache-key staleness axis (cl-cache-keys-consume) ----------------------

// TestCacheKeyStale covers the pure cache-key staleness classifier: the
// AlwaysRevalidate sentinel is always stale, a matching key is fresh, a changed
// key is stale, and an empty recorded key is stale against any concrete key.
func TestCacheKeyStale(t *testing.T) {
	const key = cacheKeyPrefix + "git:abc"
	cases := []struct {
		name              string
		recorded, derived string
		wantStale         bool
	}{
		{"matching key is fresh", key, key, false},
		{"changed key is stale", key, cacheKeyPrefix + "git:def", true},
		{"always-revalidate is always stale", key, AlwaysRevalidate, true},
		{"empty recorded vs concrete is stale", "", key, true},
		{"both empty is fresh", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CacheKeyStale(tc.recorded, tc.derived); got != tc.wantStale {
				t.Errorf("CacheKeyStale(%q,%q) = %v, want %v", tc.recorded, tc.derived, got, tc.wantStale)
			}
		})
	}
}

// TestCacheKeyStaleForLayerEnvSelector proves the resolver-side consumer detects
// a cache_keys override edit that changes the key shape from the same SHA facts:
// adding an {env} selector switches the source from the kind default to a
// composite override key, so a lock recorded under the default reads back stale,
// while a lock recorded under the same override reads back fresh.
func TestCacheKeyStaleForLayerEnvSelector(t *testing.T) {
	t.Setenv("DA_CACHE_TOKEN", "v1")
	const sha = "deadbeef00000000000000000000000000000000"
	r := NewLayeredResolver()

	defaultSrc := Source{ID: "acme", Type: "git"}
	overrideSrc := Source{ID: "acme", Type: "git", CacheKeys: &CacheKeys{Env: []string{"DA_CACHE_TOKEN"}}}

	// A lock recorded under the kind default, then the source declares an env
	// selector: the recomputed key no longer matches -> stale (negative path: the
	// override was a silent no-op before, now it flips the decision).
	defaultLock := r.lockEntry(defaultSrc, FetchedLayer{ResolvedSHA: sha, KeyInputs: CacheKeyInputs{ResolvedCommit: sha}})
	if !r.CacheKeyStaleForLayer(overrideSrc, defaultLock) {
		t.Error("adding an env selector should make a default-keyed lock stale")
	}

	// A lock recorded under the override with the env value unchanged reads fresh
	// (positive path: identical override + value -> not stale).
	overrideLock := r.lockEntry(overrideSrc, FetchedLayer{ResolvedSHA: sha, KeyInputs: CacheKeyInputs{ResolvedCommit: sha}})
	if r.CacheKeyStaleForLayer(overrideSrc, overrideLock) {
		t.Error("unchanged override + env value should be fresh")
	}

	// Changing the env value makes the override-keyed lock stale.
	t.Setenv("DA_CACHE_TOKEN", "v2")
	if !r.CacheKeyStaleForLayer(overrideSrc, overrideLock) {
		t.Error("changed env value should make the override-keyed lock stale")
	}
}
