package config

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"
)

// fixedFetchTime is a stable injected instant so locked timestamps are
// deterministic (the ReviewNudges DI convention, no package clock).
var fixedFetchTime = time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)

func capUnit(ref string, scope AuthorityScope, allow ...string) ConfigProfile {
	return capProfile(ref, scope, ProfileSelector{Role: "reviewer"}, allow...)
}

func TestProfileUnitDigestStableAndSensitive(t *testing.T) {
	base := capUnit("repo:cap", AuthRepo, "Edit")
	d1 := ProfileUnitDigest(base)
	if !strings.HasPrefix(d1, profileDigestPrefix) {
		t.Fatalf("digest %q missing %q prefix", d1, profileDigestPrefix)
	}
	if d1 != ProfileUnitDigest(base) {
		t.Fatal("digest must be stable across calls for the same fragment")
	}

	cases := map[string]ConfigProfile{
		"bundle":   capUnit("repo:cap", AuthRepo, "Edit", "Write"),
		"ref":      capUnit("repo:other", AuthRepo, "Edit"),
		"scope":    capUnit("repo:cap", AuthOrg, "Edit"),
		"order":    {Ref: "repo:cap", Kind: ProfileKindAgentCapability, Scope: AuthRepo, Order: 9, Selector: ProfileSelector{Role: "reviewer"}, Bundle: map[string]any{"tools_allow": []any{"Edit"}}},
		"selector": {Ref: "repo:cap", Kind: ProfileKindAgentCapability, Scope: AuthRepo, Selector: ProfileSelector{Role: "worker"}, Bundle: map[string]any{"tools_allow": []any{"Edit"}}},
		"authored": {Ref: "repo:cap", Kind: ProfileKindAgentCapability, Scope: AuthRepo, Selector: ProfileSelector{Role: "reviewer"}, Bundle: map[string]any{"tools_allow": []any{"Edit"}}, Authored: true},
		"kind":     {Ref: "repo:cap", Kind: ProfileKindAppType, Scope: AuthRepo, Selector: ProfileSelector{Role: "reviewer"}, Bundle: map[string]any{"tools_allow": []any{"Edit"}}},
	}
	for name, mutated := range cases {
		if ProfileUnitDigest(mutated) == d1 {
			t.Fatalf("mutating %s must change the unit digest", name)
		}
	}
}

func TestProfileUnitDigestNilBundle(t *testing.T) {
	withNil := ConfigProfile{Ref: "repo:x", Kind: ProfileKindStage, Scope: AuthRepo}
	withEmpty := ConfigProfile{Ref: "repo:x", Kind: ProfileKindStage, Scope: AuthRepo, Bundle: map[string]any{}}
	if ProfileUnitDigest(withNil) != ProfileUnitDigest(withEmpty) {
		t.Fatal("a nil bundle must digest identically to an empty bundle")
	}
}

func TestProfileLockUnits(t *testing.T) {
	set := ProfileSet{Profiles: []ConfigProfile{
		capUnit("repo:b", AuthRepo, "Edit"),
		capUnit("repo:a", AuthRepo, "Write"),
	}}
	units := ProfileLockUnits(set, fixedFetchTime)
	if len(units) != 2 {
		t.Fatalf("want 2 locked profile units, got %d", len(units))
	}
	want := fixedFetchTime.Format(time.RFC3339)
	for ref, u := range units {
		if u.Kind != UnitKindProfile {
			t.Fatalf("unit %q kind = %q, want %q", ref, u.Kind, UnitKindProfile)
		}
		if u.FetchedAt != want || u.LastCheckedAt != want {
			t.Fatalf("unit %q timestamps = %q/%q, want %q", ref, u.FetchedAt, u.LastCheckedAt, want)
		}
		if !strings.HasPrefix(u.Digest, profileDigestPrefix) {
			t.Fatalf("unit %q digest %q missing prefix", ref, u.Digest)
		}
	}
	// Deterministic: a re-run yields byte-identical entries regardless of input order.
	reordered := ProfileSet{Profiles: []ConfigProfile{set.Profiles[1], set.Profiles[0]}}
	if ProfileLockUnits(reordered, fixedFetchTime)["repo:a"] != units["repo:a"] {
		t.Fatal("lock units must be input-order-independent")
	}
}

func TestMergeProfileUnitsPreservesSiblings(t *testing.T) {
	existing := map[string]LockedUnit{
		"src:layer@1": {Kind: UnitKindLayer, Digest: "sha256:layer"},
	}
	set := ProfileSet{Profiles: []ConfigProfile{capUnit("repo:cap", AuthRepo, "Edit")}}
	merged := MergeProfileUnits(existing, set, fixedFetchTime)
	if _, ok := merged["src:layer@1"]; !ok {
		t.Fatal("merge must preserve the sibling layer unit")
	}
	if merged["repo:cap"].Kind != UnitKindProfile {
		t.Fatal("merge must add the profile unit")
	}
	// The source map is not mutated.
	if _, ok := existing["repo:cap"]; ok {
		t.Fatal("merge must not mutate the input units map")
	}
}

func TestProfileLockReproducibility(t *testing.T) {
	set := ProfileSet{Profiles: []ConfigProfile{
		capUnit("repo:ok", AuthRepo, "Edit"),
		capUnit("repo:stale", AuthRepo, "Edit"),
		capUnit("repo:missing", AuthRepo, "Edit"),
	}}
	locked := ProfileLockUnits(ProfileSet{Profiles: []ConfigProfile{
		capUnit("repo:ok", AuthRepo, "Edit"),
		capUnit("repo:stale", AuthRepo, "Write"), // different bundle ⇒ stale
		capUnit("repo:extra", AuthRepo, "Edit"),  // no longer in the set ⇒ extra
	}}, fixedFetchTime)
	// A non-profile lock entry must be ignored.
	locked["src:layer@1"] = LockedUnit{Kind: UnitKindLayer, Digest: "sha256:layer"}

	deltas := ProfileLockReproducibility(locked, set)
	got := map[string]ProfileLockStatus{}
	for _, d := range deltas {
		got[d.Key] = d.Status
	}
	want := map[string]ProfileLockStatus{
		"repo:ok":      ProfileLockOK,
		"repo:stale":   ProfileLockStale,
		"repo:missing": ProfileLockMissing,
		"repo:extra":   ProfileLockExtra,
	}
	for ref, status := range want {
		if got[ref] != status {
			t.Fatalf("ref %q status = %q, want %q", ref, got[ref], status)
		}
	}
	if _, ok := got["src:layer@1"]; ok {
		t.Fatal("a non-profile lock entry must not appear in the reproducibility report")
	}
	if ProfileLockReproducible(deltas) {
		t.Fatal("a set with stale/missing/extra units is not reproducible")
	}
	// Sorted by key.
	for i := 1; i < len(deltas); i++ {
		if deltas[i-1].Key > deltas[i].Key {
			t.Fatalf("deltas not sorted by key: %q before %q", deltas[i-1].Key, deltas[i].Key)
		}
	}
}

// TestWriteUnitsLockEmitsProfileUnits drives the PRODUCTION lock IO funnel: derive
// profile units from a resolved snapshot, fold them into a UnitsLock alongside a
// layer unit, write the real .agentsrc.lock via WriteUnitsLock, and read it back —
// asserting the kind:profile units actually landed in the written lock (R2). This
// exercises the writer/serializer/reader, not just the in-memory map builder.
func TestWriteUnitsLockEmitsProfileUnits(t *testing.T) {
	dir := t.TempDir()
	snap := mustResolveLayers(t, migrationLayers())
	profileUnits, err := ProfileUnitsForSnapshot(snap, fixedFetchTime)
	if err != nil {
		t.Fatal(err)
	}
	if len(profileUnits) == 0 {
		t.Fatal("expected the snapshot to derive at least one profile unit")
	}

	// Assemble the lock the way the production caller (resolver.writeUnitsLock) does:
	// the layer units + inputs_digest, PLUS the profile contribution.
	base := map[string]LockedUnit{"acme:org/base@v1": {Kind: UnitKindLayer, Digest: "sha256:layer"}}
	lock := UnitsLock{Units: base, InputsDigest: "sha256:inputs", ProfileUnits: profileUnits}
	if err := WriteUnitsLock(dir, lock); err != nil {
		t.Fatal(err)
	}

	// Read the REAL on-disk lock back through the production reader.
	locked, err := ReadLockedUnits(dir)
	if err != nil {
		t.Fatal(err)
	}
	if locked["acme:org/base@v1"].Kind != UnitKindLayer {
		t.Fatal("the sibling layer unit must survive in the written lock")
	}
	landed := 0
	for key, want := range profileUnits {
		got, ok := locked[key]
		if !ok {
			t.Fatalf("profile unit %q missing from the written lock", key)
		}
		if got.Kind != UnitKindProfile {
			t.Fatalf("unit %q landed with kind %q, want %q", key, got.Kind, UnitKindProfile)
		}
		if got.Digest != want.Digest {
			t.Fatalf("unit %q digest = %q, want %q", key, got.Digest, want.Digest)
		}
		landed++
	}
	if landed != len(profileUnits) {
		t.Fatalf("only %d/%d profile units landed in the lock", landed, len(profileUnits))
	}
	// inputs_digest round-trips through the same write.
	ul, err := ReadUnits(dir)
	if err != nil {
		t.Fatal(err)
	}
	if ul.InputsDigest != "sha256:inputs" {
		t.Fatalf("inputs digest = %q, want sha256:inputs", ul.InputsDigest)
	}
}

// TestResolveWritesProfileUnitsToLock is bug 1's END-TO-END production proof: a
// real project with execution_profile + stage_profiles is resolved through the
// actual resolver entry (LayeredResolver.Resolve, which calls writeUnitsLock), and
// the GENERATED .agentsrc.lock is read back and asserted to carry kind:profile
// units with the right kind + content digest. Unlike the WriteUnitsLock-in-isolation
// test, this drives the resolver's own lock-generation assembly site.
// collectWrittenProfileUnits returns the kind:profile units written into the
// lock, failing the test if none were written or any lacks a content digest.
func collectWrittenProfileUnits(t *testing.T, units UnitsLock) map[string]LockedUnit {
	t.Helper()
	written := map[string]LockedUnit{}
	for key, u := range units.Units {
		if u.Kind == UnitKindProfile {
			written[key] = u
		}
	}
	if len(written) == 0 {
		t.Fatal("Resolve must write kind:profile units into .agentsrc.lock (R2)")
	}
	for key, u := range written {
		if u.Digest == "" || !strings.HasPrefix(u.Digest, profileDigestPrefix) {
			t.Fatalf("profile unit %q must carry a content digest, got %q", key, u.Digest)
		}
	}
	return written
}

func TestResolveWritesProfileUnitsToLock(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{
		"version": 2,
		"repo_id": "github.com/acme/profiled",
		"execution_profile": {
			"by_app_type": {
				"go-cli": {
					"topology": {"executors": 2},
					"relevance": {"verify": {"core": ["go-test"]}}
				}
			}
		},
		"stage_profiles": {
			"verify": {"default": {"label": "Default verify"}}
		}
	}`)

	snap, err := NewLayeredResolver().Resolve(repo)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	units, err := ReadUnits(repo)
	if err != nil {
		t.Fatalf("ReadUnits: %v", err)
	}
	written := collectWrittenProfileUnits(t, units)

	// Every written profile unit's digest matches an independent derivation from the
	// SAME snapshot Resolve returned — proving the lock records exactly the fragments
	// the engine resolves, not a re-derived divergence. (Digests exclude timestamps,
	// so the clock used here is irrelevant.)
	want, err := ProfileUnitsForSnapshot(snap, time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(want) != len(written) {
		t.Fatalf("written profile units (%d) != derived (%d)", len(written), len(want))
	}
	for key, w := range want {
		got, ok := written[key]
		if !ok {
			t.Fatalf("derived profile unit %q missing from the written lock", key)
		}
		if got.Digest != w.Digest {
			t.Fatalf("profile unit %q digest = %q, want %q", key, got.Digest, w.Digest)
		}
	}
}

// TestResolveLockProfileUnitsDeterministic confirms the generated lock has no
// map-ordering (or clock) nondeterminism: two resolves of the same project under a
// fixed clock produce byte-identical .agentsrc.lock files (the units section is a
// map, and a profile contribution must not perturb its key-sorted serialization).
func TestResolveLockProfileUnitsDeterministic(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{
		"version": 2,
		"repo_id": "github.com/acme/profiled",
		"execution_profile": {"by_app_type": {
			"go-cli": {"topology": {"executors": 2}},
			"ideation": {"topology": {"executors": 1}}
		}},
		"stage_profiles": {"verify": {"default": {"label": "D"}, "strict": {"label": "S"}}}
	}`)
	fixed := func() time.Time { return time.Date(2026, 6, 28, 9, 0, 0, 0, time.UTC) }

	if _, err := NewLayeredResolver().WithClock(fixed).Resolve(repo); err != nil {
		t.Fatalf("first Resolve: %v", err)
	}
	first, err := os.ReadFile(AgentsLockPath(repo))
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	if _, err := NewLayeredResolver().WithClock(fixed).Resolve(repo); err != nil {
		t.Fatalf("second Resolve: %v", err)
	}
	second, err := os.ReadFile(AgentsLockPath(repo))
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("lock output is nondeterministic across resolves:\nfirst=%s\nsecond=%s", first, second)
	}
}

// TestResolveFailsClosedOnMalformedPolicyAtLockGen covers the new fail-closed
// branch the activation adds to writeUnitsLock: a project whose config carries a
// malformed layering_policy resolves its layers fine but fails when the lock
// generation derives profile units (R9) — the resolve aborts rather than writing a
// lock that silently omits the unit.
func TestResolveFailsClosedOnMalformedPolicyAtLockGen(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{
		"version": 2,
		"repo_id": "github.com/acme/bad",
		"layering_policy": {"mode": "merge"}
	}`)
	if _, err := NewLayeredResolver().Resolve(repo); err == nil {
		t.Fatal("a malformed layering_policy must fail the resolve closed at lock generation (R9)")
	}
}

func TestProfileUnitsForSnapshotFailsClosed(t *testing.T) {
	snap := mustResolveLayers(t, []ResolvedLayer{
		{ID: LayerRepoLocal, Present: true, Raw: map[string]any{
			"layering_policy": map[string]any{"mode": "merge"}, // invalid
		}},
	})
	if _, err := ProfileUnitsForSnapshot(snap, fixedFetchTime); err == nil {
		t.Fatal("a malformed policy must fail closed through the lock-unit derivation")
	}
}

// TestSharedRefAuthoredSynthesizedDoNotCollapse is bug 2's identity guard: an
// authored and a synthesized fragment sharing a raw ref must survive as TWO
// distinct units BOTH through resolution (contributing keys) AND in the generated
// lock (two map entries, distinct digests) — not collapse into one.
func TestSharedRefAuthoredSynthesizedDoNotCollapse(t *testing.T) {
	const ref = "repo:thing"
	syn := ConfigProfile{
		Ref: ref, Kind: ProfileKindAgentCapability, Scope: AuthRepo,
		Selector: ProfileSelector{Role: "r"}, Bundle: map[string]any{"tools_allow": []any{"Edit"}},
	}
	auth := ConfigProfile{
		Ref: ref, Kind: ProfileKindAgentCapability, Scope: AuthRepo,
		Selector: ProfileSelector{Role: "r"}, Bundle: map[string]any{"skills_allow": []any{"plan-wave-picker"}},
		Authored: true,
	}
	set := ProfileSet{Profiles: []ConfigProfile{syn, auth}}

	// Resolution: both contribute, under DISTINCT namespaced keys.
	got := mustResolveProfile(t, set, ProfileContext{Role: "r", ScopeChain: []AuthorityScope{AuthRepo}})
	wantKeys := map[string]bool{ref: true, authoredKeyPrefix + ref: true}
	if len(got.Contributing) != 2 {
		t.Fatalf("want 2 distinct contributing keys, got %v", got.Contributing)
	}
	for _, k := range got.Contributing {
		if !wantKeys[k] {
			t.Fatalf("unexpected contributing key %q (want %v)", k, wantKeys)
		}
	}

	// Lock: two distinct entries, not one collapsing the other.
	units := ProfileLockUnits(set, fixedFetchTime)
	if len(units) != 2 {
		t.Fatalf("authored+synthesized sharing a ref must yield 2 lock units, got %d (%v)", len(units), units)
	}
	bare, hasBare := units[ref]
	namespaced, hasNS := units[authoredKeyPrefix+ref]
	if !hasBare || !hasNS {
		t.Fatalf("both keys must be present; bare=%v namespaced=%v", hasBare, hasNS)
	}
	if bare.Digest == namespaced.Digest {
		t.Fatal("the two shared-ref units must carry distinct content digests")
	}
}

func TestProfileLockReproducibleClean(t *testing.T) {
	set := ProfileSet{Profiles: []ConfigProfile{capUnit("repo:cap", AuthRepo, "Edit")}}
	locked := ProfileLockUnits(set, fixedFetchTime)
	deltas := ProfileLockReproducibility(locked, set)
	if !ProfileLockReproducible(deltas) {
		t.Fatal("a set whose units all match the lock must be reproducible")
	}
	if len(deltas) != 1 || deltas[0].Status != ProfileLockOK {
		t.Fatalf("want one OK delta, got %+v", deltas)
	}
}
