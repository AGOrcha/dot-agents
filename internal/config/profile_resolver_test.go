package config

import (
	"encoding/json"
	"reflect"
	"testing"
)

// capProfile builds an agent-capability profile with a tools.allow set.
func capProfile(ref string, scope AuthorityScope, sel ProfileSelector, allow ...string) ConfigProfile {
	items := make([]any, len(allow))
	for i, a := range allow {
		items[i] = a
	}
	return ConfigProfile{
		Ref: ref, Kind: ProfileKindAgentCapability, Scope: scope, Selector: sel,
		Bundle: map[string]any{"tools": map[string]any{"allow": items}},
	}
}

func toolsAllow(b map[string]any) []string {
	tools, _ := b["tools"].(map[string]any)
	arr, _ := tools["allow"].([]any)
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
	got := ResolveProfile(set, ctx)
	if !reflect.DeepEqual(got.Contributing, []string{"a:1"}) {
		t.Fatalf("contributing = %v, want [a:1] (selector + scope-chain filter)", got.Contributing)
	}
	if !reflect.DeepEqual(toolsAllow(got.Bundle), []string{"Skill"}) {
		t.Fatalf("tools.allow = %v, want [Skill]", toolsAllow(got.Bundle))
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
	want := ResolveProfile(ProfileSet{Profiles: base}, ctx).Digest
	perms := [][]int{{0, 1, 2}, {2, 1, 0}, {1, 2, 0}, {2, 0, 1}}
	for _, p := range perms {
		shuffled := []ConfigProfile{base[p[0]], base[p[1]], base[p[2]]}
		if got := ResolveProfile(ProfileSet{Profiles: shuffled}, ctx).Digest; got != want {
			t.Fatalf("digest changed under input reorder %v: %s != %s (H1 broken)", p, got, want)
		}
	}
}

// TestResolveProfilePrecedence proves a policy precedence reorders scalar
// resolution predictably (H8 precedence-swap).
func TestResolveProfilePrecedence(t *testing.T) {
	profiles := []ConfigProfile{
		{Ref: "r:1", Kind: ProfileKindAgentCapability, Scope: AuthRepo, Bundle: map[string]any{"model": "repo-model"}},
		{Ref: "o:1", Kind: ProfileKindAgentCapability, Scope: AuthOrg, Bundle: map[string]any{"model": "org-model"}},
	}
	ctx := ProfileContext{ScopeChain: []AuthorityScope{AuthRepo, AuthOrg}}

	// Default precedence (AUTHORITY-RANK): org outranks repo, org wins.
	got := ResolveProfile(ProfileSet{Profiles: profiles}, ctx)
	if got.Bundle["model"] != "org-model" {
		t.Fatalf("default precedence: model = %v, want org-model", got.Bundle["model"])
	}
	// Swap precedence so repo is the local-wins tail.
	policy := LayeringPolicy{Scope: AuthOrg, Precedence: []AuthorityScope{AuthOrg, AuthRepo}}
	got = ResolveProfile(ProfileSet{Profiles: profiles, Policies: []LayeringPolicy{policy}}, ctx)
	if got.Bundle["model"] != "repo-model" {
		t.Fatalf("swapped precedence: model = %v, want repo-model", got.Bundle["model"])
	}
}

func TestResolvePolicyReplaceMode(t *testing.T) {
	low := LayeringPolicy{
		Scope:               AuthRepo,
		OverridePermissions: NewOverridePermissions(map[AuthorityScope][]string{AuthRepo: {"tools.allow"}}),
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
	// Replace wholly supersedes: the repo grant is gone, only org's map governs.
	if eff.Permissions.mayChange(AuthRepo, "tools.allow") {
		t.Fatal("replace mode must discard the lower-scope grant")
	}
	if !eff.Permissions.mayChange(AuthOrg, "model") {
		t.Fatal("replace mode keeps the replacing scope's grant")
	}
}

func TestResolvePolicyNarrowIntersects(t *testing.T) {
	low := LayeringPolicy{
		Scope:               AuthRepo,
		OverridePermissions: NewOverridePermissions(map[AuthorityScope][]string{AuthRepo: {"tools.allow", "mcp", "model"}}),
	}
	high := LayeringPolicy{
		Scope:               AuthOrg,
		OverridePermissions: NewOverridePermissions(map[AuthorityScope][]string{AuthRepo: {"tools.allow"}}),
	}
	eff := resolveEffectivePolicy(ProfileSet{Policies: []LayeringPolicy{low, high}},
		ProfileContext{ScopeChain: []AuthorityScope{AuthRepo, AuthOrg}})
	// Narrowing: org tightened repo to only tools.allow (Decision 3 monotone).
	if !eff.Permissions.mayChange(AuthRepo, "tools.allow") {
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
	if !eff.Permissions.mayChange(AuthRepo, "mcp") || eff.Permissions.mayChange(AuthRepo, "tools.allow") {
		t.Fatal("'*' ∩ {mcp} should narrow to exactly {mcp}")
	}
	// An omitted higher cap adds no constraint.
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
	plain := ResolveProfile(ProfileSet{Profiles: profiles}, ctx)

	// Add an org value-lock on an unrelated field that does not alter the bundle
	// values for this context — only the policy version should move the digest.
	policy := LayeringPolicy{Scope: AuthOrg, Locks: []ProfileLock{
		{Field: "unrelated", Value: json.RawMessage(`"x"`), Owner: AuthOrg, Selector: ProfileSelector{Stage: "never"}},
	}}
	withPolicy := ResolveProfile(ProfileSet{Profiles: profiles, Policies: []LayeringPolicy{policy}}, ctx)
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
	got := toolsAllow(ResolveProfile(set, ctx).Bundle)
	// Stable union in precedence order (repo Read,Write then org adds Bash); the
	// engine does NOT re-sort sets, which is what keeps the app_type/stage merge
	// byte-identical to the legacy map-merge (zero-diff).
	want := []string{"Read", "Write", "Bash"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("union tools.allow = %v, want %v", got, want)
	}
}
