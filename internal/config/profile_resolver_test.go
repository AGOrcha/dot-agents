package config

import (
	"encoding/json"
	"reflect"
	"testing"
)

// mustResolveProfile resolves and fails the test on a fatal authority error.
func mustResolveProfile(t *testing.T, set ProfileSet, ctx ProfileContext) ResolvedProfile {
	t.Helper()
	got, err := ResolveProfile(set, ctx)
	if err != nil {
		t.Fatalf("ResolveProfile: %v", err)
	}
	return got
}

// capProfile builds an agent-capability profile with a FLAT top-level tools_allow
// set (the §15 lock/deny pass operates on top-level set fields).
func capProfile(ref string, scope AuthorityScope, sel ProfileSelector, allow ...string) ConfigProfile {
	items := make([]any, len(allow))
	for i, a := range allow {
		items[i] = a
	}
	return ConfigProfile{
		Ref: ref, Kind: ProfileKindAgentCapability, Scope: scope, Selector: sel,
		Bundle: map[string]any{"tools_allow": items},
	}
}

func toolsAllow(b map[string]any) []string {
	arr, _ := b["tools_allow"].([]any)
	out := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func TestResolveProfileSelectorAndScopeChain(t *testing.T) {
	set := ProfileSet{Profiles: []ConfigProfile{
		capProfile("a:1", AuthRepo, ProfileSelector{Role: "orchestrator"}, "Skill"),
		capProfile("a:2", AuthRepo, ProfileSelector{Role: "reviewer"}, "Read"),
		capProfile("a:3", AuthTeam, ProfileSelector{}, "Bash"), // team not in chain
	}}
	ctx := ProfileContext{Role: "orchestrator", ScopeChain: []AuthorityScope{AuthRepo}}
	got := mustResolveProfile(t, set, ctx)
	if !reflect.DeepEqual(got.Contributing, []string{"a:1"}) {
		t.Fatalf("contributing = %v, want [a:1] (selector + scope-chain filter)", got.Contributing)
	}
	if !reflect.DeepEqual(toolsAllow(got.Bundle), []string{"Skill"}) {
		t.Fatalf("tools_allow = %v, want [Skill]", toolsAllow(got.Bundle))
	}
}

// TestResolveProfileOrderIndependent is the H1 proof: shuffling the input order
// yields an identical digest (§2.1, done-criterion 3).
func TestResolveProfileOrderIndependent(t *testing.T) {
	base := []ConfigProfile{
		capProfile("z:1", AuthUser, ProfileSelector{}, "Read"),
		capProfile("a:2", AuthRepo, ProfileSelector{}, "Write"),
		capProfile("m:3", AuthOrg, ProfileSelector{}, "Bash"),
	}
	ctx := ProfileContext{ScopeChain: []AuthorityScope{AuthUser, AuthRepo, AuthOrg}}
	want := mustResolveProfile(t, ProfileSet{Profiles: base}, ctx).Digest
	perms := [][]int{{0, 1, 2}, {2, 1, 0}, {1, 2, 0}, {2, 0, 1}}
	for _, p := range perms {
		shuffled := []ConfigProfile{base[p[0]], base[p[1]], base[p[2]]}
		if got := mustResolveProfile(t, ProfileSet{Profiles: shuffled}, ctx).Digest; got != want {
			t.Fatalf("digest changed under input reorder %v: %s != %s (H1 broken)", p, got, want)
		}
	}
}

// TestResolveProfileValueAxisRepoBeatsOrg is the FIX-1 proof: on the VALUE axis a
// repo fragment wins over an org fragment (value-precedence: repo is a HIGHER
// layer than a granted/imported org source), even though org OUTRANKS repo on the
// AUTHORITY axis. The old engine ordered values by authority-rank and wrongly let
// org win — this fails on that code and passes on the value-precedence fix.
func TestResolveProfileValueAxisRepoBeatsOrg(t *testing.T) {
	profiles := []ConfigProfile{
		{Ref: "r:1", Kind: ProfileKindAgentCapability, Scope: AuthRepo, Bundle: map[string]any{"model": "repo-model"}},
		{Ref: "o:1", Kind: ProfileKindAgentCapability, Scope: AuthOrg, Bundle: map[string]any{"model": "org-model"}},
	}
	ctx := ProfileContext{ScopeChain: []AuthorityScope{AuthOrg, AuthRepo}}
	got := mustResolveProfile(t, ProfileSet{Profiles: profiles}, ctx)
	if got.Bundle["model"] != "repo-model" {
		t.Fatalf("value axis: model = %v, want repo-model (value-precedence: repo > org)", got.Bundle["model"])
	}

	// A policy precedence override can still reorder the value axis explicitly.
	policy := LayeringPolicy{Scope: AuthOrg, Precedence: []AuthorityScope{AuthRepo, AuthOrg}}
	got = mustResolveProfile(t, ProfileSet{Profiles: profiles, Policies: []LayeringPolicy{policy}}, ctx)
	if got.Bundle["model"] != "org-model" {
		t.Fatalf("explicit precedence [repo,org]: model = %v, want org-model (org is the tail)", got.Bundle["model"])
	}
}

func TestResolvePolicyReplaceMode(t *testing.T) {
	low := LayeringPolicy{
		Scope:               AuthRepo,
		OverridePermissions: NewOverridePermissions(map[AuthorityScope][]string{AuthRepo: {"tools_allow"}}),
	}
	high := LayeringPolicy{
		Scope:               AuthOrg,
		Mode:                PolicyModeReplace,
		OverridePermissions: NewOverridePermissions(map[AuthorityScope][]string{AuthOrg: {"*"}}),
	}
	eff := resolveEffectivePolicy(ProfileSet{Policies: []LayeringPolicy{low, high}},
		ProfileContext{ScopeChain: []AuthorityScope{AuthRepo, AuthOrg}})
	if eff.Replaced != AuthOrg {
		t.Fatalf("replaced-by = %q, want org (Q4 replace marker)", eff.Replaced)
	}
	if eff.Permissions.mayChange(AuthRepo, "tools_allow") {
		t.Fatal("replace mode must discard the lower-scope grant")
	}
	if !eff.Permissions.mayChange(AuthOrg, "model") {
		t.Fatal("replace mode keeps the replacing scope's grant")
	}
}

func TestResolvePolicyNarrowIntersects(t *testing.T) {
	low := LayeringPolicy{
		Scope:               AuthRepo,
		OverridePermissions: NewOverridePermissions(map[AuthorityScope][]string{AuthRepo: {"tools_allow", "mcp", "model"}}),
	}
	high := LayeringPolicy{
		Scope:               AuthOrg,
		OverridePermissions: NewOverridePermissions(map[AuthorityScope][]string{AuthRepo: {"tools_allow"}}),
	}
	eff := resolveEffectivePolicy(ProfileSet{Policies: []LayeringPolicy{low, high}},
		ProfileContext{ScopeChain: []AuthorityScope{AuthRepo, AuthOrg}})
	if !eff.Permissions.mayChange(AuthRepo, "tools_allow") {
		t.Fatal("intersection should keep the shared grant")
	}
	if eff.Permissions.mayChange(AuthRepo, "mcp") {
		t.Fatal("higher scope must narrow away mcp (Decision 3: never broaden)")
	}
}

func TestResolvePolicyNarrowWildcardAndOmitted(t *testing.T) {
	withWild := LayeringPolicy{Scope: AuthRepo, OverridePermissions: NewOverridePermissions(map[AuthorityScope][]string{AuthRepo: {"*"}})}
	concrete := LayeringPolicy{Scope: AuthOrg, OverridePermissions: NewOverridePermissions(map[AuthorityScope][]string{AuthRepo: {"mcp"}})}
	eff := resolveEffectivePolicy(ProfileSet{Policies: []LayeringPolicy{withWild, concrete}},
		ProfileContext{ScopeChain: []AuthorityScope{AuthRepo, AuthOrg}})
	if !eff.Permissions.mayChange(AuthRepo, "mcp") || eff.Permissions.mayChange(AuthRepo, "tools_allow") {
		t.Fatal("'*' ∩ {mcp} should narrow to exactly {mcp}")
	}
	onlyLow := resolveEffectivePolicy(ProfileSet{Policies: []LayeringPolicy{withWild}},
		ProfileContext{ScopeChain: []AuthorityScope{AuthRepo}})
	if !onlyLow.Permissions.mayChange(AuthRepo, "anything") {
		t.Fatal("a single '*' grant should permit everything")
	}
}

// TestDigestIncludesPolicyVersion is the Decision-7 proof: the digest changes
// when the effective policy changes even if the bundle VALUES are identical.
func TestDigestIncludesPolicyVersion(t *testing.T) {
	profiles := []ConfigProfile{capProfile("a:1", AuthRepo, ProfileSelector{}, "Read")}
	ctx := ProfileContext{ScopeChain: []AuthorityScope{AuthRepo, AuthOrg}}
	plain := mustResolveProfile(t, ProfileSet{Profiles: profiles}, ctx)

	// An org value-lock on an unrelated field that never matches this context: it
	// does not alter the bundle values, only the policy version.
	policy := LayeringPolicy{Scope: AuthOrg, Locks: []ProfileLock{
		{Field: "unrelated", Value: json.RawMessage(`"x"`), Owner: AuthOrg, Selector: ProfileSelector{Stage: "never"}},
	}}
	withPolicy := mustResolveProfile(t, ProfileSet{Profiles: profiles, Policies: []LayeringPolicy{policy}}, ctx)
	if reflect.DeepEqual(plain.Bundle, withPolicy.Bundle) && plain.Digest == withPolicy.Digest {
		t.Fatal("digest must change with policy version even when bundle values match (Decision 7)")
	}
}

func TestResolveProfileCapabilityUnion(t *testing.T) {
	set := ProfileSet{Profiles: []ConfigProfile{
		capProfile("a:1", AuthRepo, ProfileSelector{}, "Read", "Write"),
		capProfile("a:2", AuthOrg, ProfileSelector{}, "Write", "Bash"),
	}}
	ctx := ProfileContext{ScopeChain: []AuthorityScope{AuthRepo, AuthOrg}}
	got := toolsAllow(mustResolveProfile(t, set, ctx).Bundle)
	// Value-precedence order merges org (idx2) BEFORE repo (idx4): org Write,Bash
	// then repo adds Read.
	want := []string{"Write", "Bash", "Read"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("union tools_allow = %v, want %v", got, want)
	}
}
