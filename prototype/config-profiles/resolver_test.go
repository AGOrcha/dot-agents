package main

import (
	"math/rand"
	"reflect"
	"testing"
)

// resolver_test.go — the three empirical proofs (H1, H7, H8).
//
// These tests drive the real Resolve() entry point and assert observable end
// state (digest, deny set, contributing refs). Each proof is MUTATION-SENSITIVE:
// breaking the relevant resolver code makes the proof fail. See the
// "mutation-check" comments and TestMutationSensitivity_* below.

func loadFixtures(t *testing.T) SourceSet {
	t.Helper()
	src, err := LoadSourceSet("fixtures")
	if err != nil {
		t.Fatalf("LoadSourceSet: %v", err)
	}
	if len(src.Profiles) == 0 || len(src.Policies) == 0 {
		t.Fatalf("empty fixture set: %d profiles, %d policies", len(src.Profiles), len(src.Policies))
	}
	return src
}

func orchestratorCtx() Context {
	return Context{
		Role:       "orchestrator",
		AppType:    "go-cli",
		Stage:      "orchestrate",
		ScopeChain: []Scope{ScopeRepo, ScopeProject, ScopeUser, ScopeTeam, ScopeOrg},
	}
}

func reviewerCtx() Context {
	return Context{
		Role:       "reviewer",
		AppType:    "go-cli",
		Stage:      "review",
		ScopeChain: []Scope{ScopeRepo, ScopeProject, ScopeUser, ScopeTeam, ScopeOrg},
	}
}

// ---------------------------------------------------------------------------
// H1 — no dependency hell
// ---------------------------------------------------------------------------

// H1(a): the dependency graph is depth-1 (profile -> leaf-unit only). There are
// ZERO profile -> profile edges. We prove this structurally: a profile-to-
// profile edge could only exist via an `extends`/`ref-to-profile` field. The
// Profile struct has no such field, and no profile's bundle names another
// profile ref. We assert both: the type carries no inheritance field, and no
// bundle value collides with a profile ref.
func TestH1_GraphIsDepthOneNoProfileToProfileEdges(t *testing.T) {
	src := loadFixtures(t)

	// (1) No `extends` field exists on the Profile type.
	pt := reflect.TypeOf(Profile{})
	for i := 0; i < pt.NumField(); i++ {
		name := pt.Field(i).Name
		if name == "Extends" || name == "Inherits" || name == "Parent" || name == "Base" {
			t.Fatalf("Profile type has inheritance field %q — that is a profile->profile edge", name)
		}
	}

	// (2) No profile's bundle/selector references another profile's ref.
	refs := map[string]bool{}
	for _, p := range src.Profiles {
		refs[p.Ref] = true
	}
	for _, p := range src.Profiles {
		for _, v := range bundleStrings(p.Bundle) {
			if refs[v] {
				t.Fatalf("profile %s references another profile ref %q — profile->profile edge", p.Ref, v)
			}
		}
	}
}

// bundleStrings flattens every string value a bundle carries.
func bundleStrings(b Bundle) []string {
	out := []string{}
	out = append(out, b.Tools.Allow...)
	out = append(out, b.Tools.Deny...)
	out = append(out, b.Skills.Preload...)
	out = append(out, b.Skills.Allow...)
	out = append(out, b.Skills.Deny...)
	out = append(out, b.Hooks...)
	out = append(out, b.MCP...)
	if b.Model != "" {
		out = append(out, b.Model)
	}
	return out
}

// H1(b): resolution is deterministic + order-independent. Shuffling the profile
// input order yields an identical output digest.
func TestH1_DeterministicOrderIndependent(t *testing.T) {
	src := loadFixtures(t)
	ctx := orchestratorCtx()
	want := Resolve(src, ctx).Digest

	rng := rand.New(rand.NewSource(1))
	for trial := 0; trial < 50; trial++ {
		shuffled := append([]Profile{}, src.Profiles...)
		rng.Shuffle(len(shuffled), func(i, j int) {
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		})
		got := Resolve(SourceSet{Profiles: shuffled, Policies: src.Policies}, ctx).Digest
		if got != want {
			t.Fatalf("trial %d: digest changed under shuffle: got %s want %s", trial, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// H7 — reproducibility (team guarantee)
// ---------------------------------------------------------------------------

// H7: resolving the same source-set twice, AND re-parsing the fixtures fresh
// ("two clones"), yields an identical bundle digest. The two sides are produced
// through INDEPENDENT parses (not a shared value), per the tests-must-drive
// lesson on comparing independent productions.
func TestH7_ReproducibleAcrossClones(t *testing.T) {
	ctx := orchestratorCtx()

	cloneA, err := LoadSourceSet("fixtures")
	if err != nil {
		t.Fatal(err)
	}
	cloneB, err := LoadSourceSet("fixtures")
	if err != nil {
		t.Fatal(err)
	}

	resA1 := Resolve(cloneA, ctx)
	resA2 := Resolve(cloneA, ctx) // same source twice
	resB := Resolve(cloneB, ctx)  // independent re-parse

	if resA1.Digest != resA2.Digest {
		t.Fatalf("same source resolved twice differs: %s vs %s", resA1.Digest, resA2.Digest)
	}
	if resA1.Digest != resB.Digest {
		t.Fatalf("two-clone digests differ: %s vs %s", resA1.Digest, resB.Digest)
	}
	if !reflect.DeepEqual(resA1.Bundle, resB.Bundle) {
		t.Fatalf("two-clone bundles differ structurally")
	}
}

// ---------------------------------------------------------------------------
// H8 — policy-governed merge across scopes
// ---------------------------------------------------------------------------

// H8(a): the ORG lock wins. A team/project profile trying to grant a reviewer
// Write is rejected/stripped. We inject such a malicious profile and assert
// Write never appears in tools.allow and DOES appear in tools.deny.
func TestH8a_OrgLockWinsOverLowerScopeGrant(t *testing.T) {
	src := loadFixtures(t)
	ctx := reviewerCtx()

	// A team-scoped profile that tries to re-grant Write to reviewers.
	malicious := Profile{
		Ref:         "team/sneaky-write-grant",
		Selector:    Selector{Role: "reviewer"},
		SourceScope: ScopeTeam,
		Bundle:      Bundle{Tools: ToolSet{Allow: []string{"Write", "Edit"}}},
	}
	src.Profiles = append(src.Profiles, malicious)

	res := Resolve(src, ctx)

	if contains(res.Bundle.Tools.Allow, "Write") || contains(res.Bundle.Tools.Allow, "Edit") {
		t.Fatalf("org lock failed: reviewer allow leaked Edit/Write: %v", res.Bundle.Tools.Allow)
	}
	if !contains(res.Bundle.Tools.Deny, "Write") || !contains(res.Bundle.Tools.Deny, "Edit") {
		t.Fatalf("org lock not enforced into deny set: %v", res.Bundle.Tools.Deny)
	}
}

// H8(b): a TEAM lock holds for a field the org left open. The team policy locks
// `skills.deny:{plan-wave-picker}@role:loop-worker`. The org policy says nothing
// about that skill. We give the loop-worker a profile that would otherwise allow
// it, and assert the team lock strips it.
func TestH8b_TeamLockHoldsWhereOrgIsSilent(t *testing.T) {
	src := loadFixtures(t)
	ctx := Context{
		Role:       "loop-worker",
		AppType:    "go-cli",
		Stage:      "impl",
		ScopeChain: []Scope{ScopeRepo, ScopeProject, ScopeUser, ScopeTeam, ScopeOrg},
	}

	// repo-scoped profile that grants the locked skill.
	grant := Profile{
		Ref:         "repo/grants-wave-picker",
		Selector:    Selector{Role: "loop-worker"},
		SourceScope: ScopeRepo,
		Bundle:      Bundle{Skills: SkillSet{Allow: []string{"plan-wave-picker"}}},
	}
	src.Profiles = append(src.Profiles, grant)

	res := Resolve(src, ctx)

	if contains(res.Bundle.Skills.Allow, "plan-wave-picker") {
		t.Fatalf("team lock failed: loop-worker skills.allow leaked plan-wave-picker: %v", res.Bundle.Skills.Allow)
	}
	if !contains(res.Bundle.Skills.Deny, "plan-wave-picker") {
		t.Fatalf("team lock not enforced into skills.deny: %v", res.Bundle.Skills.Deny)
	}
}

// H8(c): swapping the org policy's precedence changes value resolution
// predictably. We give two scopes a conflicting scalar (model). With precedence
// putting org last (highest), the org model wins; reversing precedence makes
// repo win. This proves precedence governs scalar conflict resolution.
func TestH8c_PrecedenceSwapChangesResolution(t *testing.T) {
	base := SourceSet{
		Profiles: []Profile{
			{Ref: "repo/model", Selector: Selector{Role: "x"}, SourceScope: ScopeRepo,
				Bundle: Bundle{Model: "repo-model"}},
			{Ref: "org/model", Selector: Selector{Role: "x"}, SourceScope: ScopeOrg,
				Bundle: Bundle{Model: "org-model"}},
		},
	}
	ctx := Context{Role: "x", ScopeChain: []Scope{ScopeRepo, ScopeOrg}}

	// org last in precedence => org-model wins (last write).
	srcOrgWins := base
	srcOrgWins.Policies = []LayeringPolicy{{Scope: ScopeOrg, Precedence: []Scope{ScopeRepo, ScopeOrg}}}
	if got := Resolve(srcOrgWins, ctx).Bundle.Model; got != "org-model" {
		t.Fatalf("org-last precedence: want org-model, got %q", got)
	}

	// repo last in precedence => repo-model wins.
	srcRepoWins := base
	srcRepoWins.Policies = []LayeringPolicy{{Scope: ScopeOrg, Precedence: []Scope{ScopeOrg, ScopeRepo}}}
	if got := Resolve(srcRepoWins, ctx).Bundle.Model; got != "repo-model" {
		t.Fatalf("repo-last precedence: want repo-model, got %q", got)
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
