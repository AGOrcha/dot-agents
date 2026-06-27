package config

import (
	"encoding/json"
	"reflect"
	"testing"
)

// denyLockPolicy builds a layering policy with a single deny-lock owned by scope.
func denyLockPolicy(scope AuthorityScope, field string, members ...string) LayeringPolicy {
	return LayeringPolicy{Scope: scope, Locks: []ProfileLock{
		{Field: field, Deny: members, Owner: scope},
	}}
}

// TestLockH8aOrgLockBeatsLowerGrant is H8(a): an org deny-lock wins over a
// lower-scope (team) grant of the same capability — the lower grant is dropped
// from the additive allow set and forced out of the effective bundle (Decision
// 4: a lower scope can never punch a capability through a higher deny).
func TestLockH8aOrgLockBeatsLowerGrant(t *testing.T) {
	set := ProfileSet{
		Profiles: []ConfigProfile{
			capProfile("team:grant", AuthTeam, ProfileSelector{}, "Edit", "Read"),
		},
		Policies: []LayeringPolicy{denyLockPolicy(AuthOrg, "tools.allow", "Edit")},
	}
	ctx := ProfileContext{ScopeChain: []AuthorityScope{AuthTeam, AuthOrg}}
	got := ResolveProfile(set, ctx)
	if reflect.DeepEqual(toolsAllow(got.Bundle), []string{"Edit", "Read"}) {
		t.Fatal("org deny-lock did not bind (H8a)")
	}
	if !reflect.DeepEqual(toolsAllow(got.Bundle), []string{"Read"}) {
		t.Fatalf("tools.allow = %v, want [Read] (Edit denied by org lock)", toolsAllow(got.Bundle))
	}
	if len(got.Locks) != 1 || got.Locks[0].Owner != AuthOrg {
		t.Fatalf("binding lock not surfaced with org owner: %+v", got.Locks)
	}
}

// TestLockH8bTeamLockHoldsWhereOrgSilent is H8(b): a team deny-lock binds the
// lower (repo) scope where org is silent.
func TestLockH8bTeamLockHoldsWhereOrgSilent(t *testing.T) {
	set := ProfileSet{
		Profiles: []ConfigProfile{
			capProfile("repo:grant", AuthRepo, ProfileSelector{}, "Edit", "Read"),
		},
		Policies: []LayeringPolicy{denyLockPolicy(AuthTeam, "tools.allow", "Edit")},
	}
	ctx := ProfileContext{ScopeChain: []AuthorityScope{AuthRepo, AuthTeam}}
	got := toolsAllow(ResolveProfile(set, ctx).Bundle)
	if !reflect.DeepEqual(got, []string{"Read"}) {
		t.Fatalf("tools.allow = %v, want [Read] (team lock holds where org silent, H8b)", got)
	}
}

// TestLockSelectorScoping proves a context-scoped lock binds only matching
// contexts (e.g. deny Edit/Write only @role:reviewer).
func TestLockSelectorScoping(t *testing.T) {
	policy := LayeringPolicy{Scope: AuthOrg, Locks: []ProfileLock{
		{Field: "tools.allow", Deny: []string{"Edit"}, Owner: AuthOrg, Selector: ProfileSelector{Role: "reviewer"}},
	}}
	set := ProfileSet{
		Profiles: []ConfigProfile{capProfile("a:1", AuthRepo, ProfileSelector{}, "Edit", "Read")},
		Policies: []LayeringPolicy{policy},
	}
	chain := []AuthorityScope{AuthRepo, AuthOrg}

	reviewer := toolsAllow(ResolveProfile(set, ProfileContext{Role: "reviewer", ScopeChain: chain}).Bundle)
	if !reflect.DeepEqual(reviewer, []string{"Read"}) {
		t.Fatalf("reviewer context: tools.allow = %v, want [Read]", reviewer)
	}
	worker := toolsAllow(ResolveProfile(set, ProfileContext{Role: "worker", ScopeChain: chain}).Bundle)
	if !reflect.DeepEqual(worker, []string{"Edit", "Read"}) {
		t.Fatalf("worker context: tools.allow = %v, want [Edit Read] (lock does not match)", worker)
	}
}

// TestValueLockPinsScalar proves a value-lock pins a scalar absolutely.
func TestValueLockPinsScalar(t *testing.T) {
	policy := LayeringPolicy{Scope: AuthOrg, Locks: []ProfileLock{
		{Field: "model", Value: json.RawMessage(`"sonnet"`), Owner: AuthOrg},
	}}
	set := ProfileSet{
		Profiles: []ConfigProfile{
			{Ref: "r:1", Kind: ProfileKindAgentCapability, Scope: AuthRepo, Bundle: map[string]any{"model": "haiku"}},
		},
		Policies: []LayeringPolicy{policy},
	}
	got := ResolveProfile(set, ProfileContext{ScopeChain: []AuthorityScope{AuthRepo, AuthOrg}})
	if got.Bundle["model"] != "sonnet" {
		t.Fatalf("model = %v, want sonnet (value-lock pin)", got.Bundle["model"])
	}
}

// TestZeroAuthorityLockInert proves a value-only scope's lock cannot bind.
func TestZeroAuthorityLockInert(t *testing.T) {
	policy := LayeringPolicy{Scope: AuthProjectLocal, Locks: []ProfileLock{
		{Field: "tools.allow", Deny: []string{"Edit"}, Owner: AuthProjectLocal},
	}}
	set := ProfileSet{
		Profiles: []ConfigProfile{capProfile("a:1", AuthRepo, ProfileSelector{}, "Edit")},
		Policies: []LayeringPolicy{policy},
	}
	got := ResolveProfile(set, ProfileContext{ScopeChain: []AuthorityScope{AuthProjectLocal, AuthRepo}})
	if !reflect.DeepEqual(toolsAllow(got.Bundle), []string{"Edit"}) {
		t.Fatal("a zero-authority scope's deny-lock must be inert")
	}
	if len(got.Locks) != 0 {
		t.Fatalf("zero-authority lock should not be a binding lock: %+v", got.Locks)
	}
}

// TestSameScopeConflictShowsBoth is the Decision-6 proof: two fragments at the
// same scope disagreeing on a scalar are reported with BOTH contributors.
func TestSameScopeConflictShowsBoth(t *testing.T) {
	set := ProfileSet{Profiles: []ConfigProfile{
		{Ref: "b:model", Kind: ProfileKindAgentCapability, Scope: AuthRepo, Bundle: map[string]any{"model": "haiku"}},
		{Ref: "a:model", Kind: ProfileKindAgentCapability, Scope: AuthRepo, Bundle: map[string]any{"model": "sonnet"}},
	}}
	got := ResolveProfile(set, ProfileContext{ScopeChain: []AuthorityScope{AuthRepo}})
	if len(got.Conflicts) != 1 {
		t.Fatalf("expected 1 same-scope conflict, got %d: %+v", len(got.Conflicts), got.Conflicts)
	}
	c := got.Conflicts[0]
	if c.Field != "model" || c.Scope != AuthRepo {
		t.Fatalf("conflict cell wrong: %+v", c)
	}
	if !reflect.DeepEqual(c.Refs, []string{"a:model", "b:model"}) {
		t.Fatalf("conflict refs = %v, want both contributors (Decision 6)", c.Refs)
	}
}

func TestNoConflictWhenValuesAgree(t *testing.T) {
	set := ProfileSet{Profiles: []ConfigProfile{
		{Ref: "a:1", Kind: ProfileKindAgentCapability, Scope: AuthRepo, Bundle: map[string]any{"model": "x"}},
		{Ref: "a:2", Kind: ProfileKindAgentCapability, Scope: AuthRepo, Bundle: map[string]any{"model": "x"}},
	}}
	got := ResolveProfile(set, ProfileContext{ScopeChain: []AuthorityScope{AuthRepo}})
	if len(got.Conflicts) != 0 {
		t.Fatalf("agreeing same-scope values should not conflict: %+v", got.Conflicts)
	}
}

// TestPermissionGatesWrite proves an allowlist permission blocks a disallowed
// scope's write (Decision 2 enforcement in the merge).
func TestPermissionGatesWrite(t *testing.T) {
	policy := LayeringPolicy{
		Scope:               AuthOrg,
		OverridePermissions: NewOverridePermissions(map[AuthorityScope][]string{AuthOrg: {"tools.allow"}}),
	}
	set := ProfileSet{
		Profiles: []ConfigProfile{
			capProfile("repo:1", AuthRepo, ProfileSelector{}, "Edit"), // repo not permitted
			capProfile("org:1", AuthOrg, ProfileSelector{}, "Read"),
		},
		Policies: []LayeringPolicy{policy},
	}
	got := toolsAllow(ResolveProfile(set, ProfileContext{ScopeChain: []AuthorityScope{AuthRepo, AuthOrg}}).Bundle)
	if !reflect.DeepEqual(got, []string{"Read"}) {
		t.Fatalf("tools.allow = %v, want [Read] (repo write gated out by allowlist)", got)
	}
}
