package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestConfigProfileKeyDistinguishesAuthored(t *testing.T) {
	derived := ConfigProfile{Ref: "execution-profile:go-cli"}
	authored := ConfigProfile{Ref: "repo:profile:caps", Authored: true}

	if derived.Key() != "execution-profile:go-cli" {
		t.Fatalf("a synthesized profile's key is its bare ref, got %q", derived.Key())
	}
	if derived.IsAuthored() {
		t.Fatal("a zero-value profile is not authored")
	}
	if !authored.IsAuthored() {
		t.Fatal("Authored=true must report IsAuthored()")
	}
	if !strings.HasPrefix(authored.Key(), authoredKeyPrefix) {
		t.Fatalf("an authored profile's key must be namespaced under %q, got %q", authoredKeyPrefix, authored.Key())
	}
	// Same underlying ref, different provenance ⇒ different addressable keys.
	collide := ConfigProfile{Ref: "repo:profile:caps"}
	if collide.Key() == authored.Key() {
		t.Fatal("authored and derived profiles sharing a ref must not alias in the key namespace")
	}
}

func TestDecodeAuthoredProfilesValid(t *testing.T) {
	raw := map[string]any{
		"caps": map[string]any{
			"kind":     "agent-capability",
			"selector": map[string]any{"role": "orchestrator"},
			"bundle":   map[string]any{"tools_allow": []any{"Skill"}},
		},
		"shape": map[string]any{
			"kind":   "app_type",
			"bundle": map[string]any{"topology": map[string]any{"executors": float64(2)}},
		},
	}
	got, err := decodeAuthoredProfiles(raw, AuthRepo, "repo", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 authored profiles, got %d", len(got))
	}
	// Sorted by name: caps before shape.
	if got[0].Ref != derivedRef("repo", AuthoredProfileRefPrefix, "caps") {
		t.Fatalf("ref = %q, want the authored-namespaced ref", got[0].Ref)
	}
	for _, p := range got {
		if !p.Authored {
			t.Fatalf("profile %q must be Authored=true", p.Ref)
		}
		if p.Scope != AuthRepo || p.Order != 3 {
			t.Fatalf("profile %q carries loader-stamped scope/order, got %q/%d", p.Ref, p.Scope, p.Order)
		}
	}
	if got[0].Selector.Role != "orchestrator" {
		t.Fatalf("selector not decoded: %+v", got[0].Selector)
	}
}

func TestDecodeAuthoredProfilesAbsent(t *testing.T) {
	got, err := decodeAuthoredProfiles(nil, AuthRepo, "repo", 0)
	if err != nil || got != nil {
		t.Fatalf("a nil profiles block yields no profiles, got %v / %v", got, err)
	}
}

func TestDecodeAuthoredProfilesEmptyBundleDefaults(t *testing.T) {
	raw := map[string]any{"bare": map[string]any{"kind": "stage"}}
	got, err := decodeAuthoredProfiles(raw, AuthRepo, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Bundle == nil {
		t.Fatal("a kind-only profile must default to a non-nil empty bundle")
	}
	// Source-less layer ⇒ bare prefixed ref.
	if got[0].Ref != "profile:bare" {
		t.Fatalf("source-less ref = %q, want profile:bare", got[0].Ref)
	}
}

func TestDecodeAuthoredProfilesFailClosed(t *testing.T) {
	cases := map[string]any{
		"not-an-object":       []any{"x"},
		"self-declared-scope": map[string]any{"p": map[string]any{"kind": "stage", "scope": "org"}},
		"inheritance-edge":    map[string]any{"p": map[string]any{"kind": "stage", "extends": "other"}},
		"unknown-field":       map[string]any{"p": map[string]any{"kind": "stage", "bogus": 1}},
		"unknown-kind":        map[string]any{"p": map[string]any{"kind": "mystery"}},
		"missing-kind":        map[string]any{"p": map[string]any{"bundle": map[string]any{}}},
		"unknown-selector":    map[string]any{"p": map[string]any{"kind": "stage", "selector": map[string]any{"bogus": "x"}}},
	}
	for name, raw := range cases {
		if _, err := decodeAuthoredProfiles(raw, AuthRepo, "repo", 0); err == nil {
			t.Fatalf("%s: expected a fail-closed error (R9)", name)
		}
	}
}

func TestDecodeAuthoredProfileMalformedJSON(t *testing.T) {
	if _, err := decodeAuthoredProfile(json.RawMessage(`{`), "repo:p", AuthRepo, 0); err == nil {
		t.Fatal("malformed JSON must error")
	}
	// A non-object payload (probe decode error path).
	if _, err := decodeAuthoredProfile(json.RawMessage(`42`), "repo:p", AuthRepo, 0); err == nil {
		t.Fatal("a non-object profile payload must error")
	}
}

func TestValidateProfileKind(t *testing.T) {
	for _, k := range []ProfileKind{ProfileKindAppType, ProfileKindStage, ProfileKindAgentCapability} {
		if err := validateProfileKind(k); err != nil {
			t.Fatalf("kind %q should be valid: %v", k, err)
		}
	}
	for _, k := range []ProfileKind{"", "bogus"} {
		if err := validateProfileKind(k); err == nil {
			t.Fatalf("kind %q should be rejected", k)
		}
	}
}

func TestProfileSetFromSnapshotDerivesAuthored(t *testing.T) {
	layers := []ResolvedLayer{
		{ID: LayerRepoLocal, Present: true, Raw: map[string]any{
			"profiles": map[string]any{
				"orchestrator-caps": map[string]any{
					"kind":     "agent-capability",
					"selector": map[string]any{"role": "orchestrator"},
					"bundle":   map[string]any{"skills_allow": []any{"orchestrator-session-start"}},
				},
			},
		}},
	}
	snap := mustResolveLayers(t, layers)
	set, err := ProfileSetFromSnapshot(snap)
	if err != nil {
		t.Fatal(err)
	}
	var authored *ConfigProfile
	for i := range set.Profiles {
		if set.Profiles[i].Authored {
			authored = &set.Profiles[i]
		}
	}
	if authored == nil {
		t.Fatal("expected an authored profile derived from the layer's profiles block")
	}
	if authored.Scope != AuthRepo {
		t.Fatalf("authored profile scope = %q, want repo (source-derived)", authored.Scope)
	}
	// It resolves through the same engine.
	got := mustResolveProfile(t, set, ProfileContext{Role: "orchestrator", ScopeChain: []AuthorityScope{AuthRepo}})
	if len(got.Contributing) == 0 {
		t.Fatal("the authored profile must contribute to the orchestrator context")
	}
}

func TestProfileSetFromSnapshotAuthoredFailsClosed(t *testing.T) {
	snap := mustResolveLayers(t, []ResolvedLayer{
		{ID: LayerRepoLocal, Present: true, Raw: map[string]any{
			"profiles": map[string]any{"bad": map[string]any{"kind": "nope"}},
		}},
	})
	if _, err := ProfileSetFromSnapshot(snap); err == nil {
		t.Fatal("an invalid authored profile kind must fail closed through the bridge")
	}
}
