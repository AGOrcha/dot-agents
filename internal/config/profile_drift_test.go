package config

import (
	"testing"
)

func resolvedFixture() ResolvedProfile {
	return ResolvedProfile{
		Digest:       "digest-v1",
		Contributing: []string{"repo:cap"},
		Bundle: map[string]any{
			"tools":  map[string]any{"allow": []any{"Edit"}},
			"model":  "claude",
			"dropme": "old",
		},
	}
}

func TestDetectProfileDriftNone(t *testing.T) {
	resolved := resolvedFixture()
	proj := ProjectProfile(resolved)
	res := DetectProfileDrift(proj, resolved)
	if res.HasDrift {
		t.Fatalf("a projection of the same resolution must not drift: %+v", res)
	}
	if !res.DigestMatch {
		t.Fatal("digests must match for a fresh projection")
	}
	if len(res.Changes) != 0 {
		t.Fatalf("no field changes expected, got %v", res.Changes)
	}
}

func TestDetectProfileDriftDigestMoved(t *testing.T) {
	proj := ProjectProfile(resolvedFixture())
	// Same bundle values but the engine now reports a different digest (a policy /
	// provenance change, Decision 7) — drift even though no leaf changed.
	resolved := resolvedFixture()
	resolved.Digest = "digest-v2"
	res := DetectProfileDrift(proj, resolved)
	if !res.HasDrift || res.DigestMatch {
		t.Fatalf("a moved source digest must register as drift: %+v", res)
	}
	if res.ProjectedDigest != "digest-v1" || res.ResolvedDigest != "digest-v2" {
		t.Fatalf("digests = %q/%q, want digest-v1/digest-v2", res.ProjectedDigest, res.ResolvedDigest)
	}
	if len(res.Changes) != 0 {
		t.Fatalf("no leaf changes expected when only the digest moved, got %v", res.Changes)
	}
}

func TestDetectProfileDriftFieldLevel(t *testing.T) {
	proj := ProjectProfile(resolvedFixture())
	resolved := resolvedFixture()
	resolved.Digest = "digest-v1"                                       // keep digests equal to prove the value diff alone catches drift
	resolved.Bundle["tools"].(map[string]any)["allow"] = []any{"Write"} // changed
	resolved.Bundle["added"] = "new"                                    // added
	delete(resolved.Bundle, "dropme")                                   // removed

	res := DetectProfileDrift(proj, resolved)
	if !res.HasDrift {
		t.Fatal("field-level differences must register as drift even with equal digests")
	}
	if !res.DigestMatch {
		t.Fatal("digests were left equal in this case")
	}
	byPath := map[string]ProfileFieldDrift{}
	for _, c := range res.Changes {
		byPath[c.Path] = c
	}
	want := map[string]ProfileDriftKind{
		"tools.allow": DriftChanged,
		"added":       DriftAdded,
		"dropme":      DriftRemoved,
	}
	for path, kind := range want {
		if byPath[path].Kind != kind {
			t.Fatalf("path %q kind = %q, want %q (changes=%+v)", path, byPath[path].Kind, kind, res.Changes)
		}
	}
	// Deterministic: sorted by path.
	for i := 1; i < len(res.Changes); i++ {
		if res.Changes[i-1].Path > res.Changes[i].Path {
			t.Fatalf("changes not sorted: %q before %q", res.Changes[i-1].Path, res.Changes[i].Path)
		}
	}
}

func TestDetectProfileDriftFromSnapshot(t *testing.T) {
	snap := mustResolveLayers(t, migrationLayers())
	resolved, err := ResolveProfileContext(snap, "", "go-cli", "", "")
	if err != nil {
		t.Fatal(err)
	}
	proj := ProjectProfile(resolved)

	// A projection of the live resolution does not drift against the same snapshot.
	res, err := DetectProfileDriftFromSnapshot(proj, snap, "", "go-cli", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.HasDrift {
		t.Fatalf("a current projection must not drift against its snapshot: %+v", res)
	}

	// A projection captured for a DIFFERENT context drifts.
	stale := ProjectProfile(ResolvedProfile{Digest: "stale", Bundle: map[string]any{"x": "y"}})
	drifted, err := DetectProfileDriftFromSnapshot(stale, snap, "", "go-cli", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !drifted.HasDrift {
		t.Fatal("a stale projection must drift against the live snapshot")
	}
}

func TestDetectProfileDriftFromSnapshotFailsClosed(t *testing.T) {
	// A malformed layering_policy makes ResolveProfileContext fail; drift detection
	// must propagate that fail-closed rather than reporting a false "no drift".
	snap := mustResolveLayers(t, []ResolvedLayer{
		{ID: LayerRepoLocal, Present: true, Raw: map[string]any{
			"layering_policy": map[string]any{"mode": "merge"},
		}},
	})
	if _, err := DetectProfileDriftFromSnapshot(ProfileProjection{}, snap, "", "", "", ""); err == nil {
		t.Fatal("expected a malformed policy to fail closed through drift detection")
	}
}
