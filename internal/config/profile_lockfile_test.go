package config

import (
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
		got[d.Ref] = d.Status
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
	// Sorted by ref.
	for i := 1; i < len(deltas); i++ {
		if deltas[i-1].Ref > deltas[i].Ref {
			t.Fatalf("deltas not sorted by ref: %q before %q", deltas[i-1].Ref, deltas[i].Ref)
		}
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
