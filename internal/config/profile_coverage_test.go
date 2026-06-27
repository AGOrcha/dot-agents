package config

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestOverridePermissionsUnmarshalError(t *testing.T) {
	var o OverridePermissions
	if err := o.UnmarshalJSON([]byte(`["not","a","map"]`)); err == nil {
		t.Fatal("expected error decoding a non-object override_permissions")
	}
}

func TestOverridePermissionsMarshalAllowlist(t *testing.T) {
	o := NewOverridePermissions(map[AuthorityScope][]string{AuthRepo: {"mcp"}})
	data, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"repo":["mcp"]}` {
		t.Fatalf("marshal = %s, want the allowlist map", data)
	}
}

func TestNarrowPermissionsNextWildcardAndMultiScope(t *testing.T) {
	acc := NewOverridePermissions(map[AuthorityScope][]string{
		AuthRepo: {"mcp", "tools.allow"},
		AuthUser: {"model"},
	})
	next := NewOverridePermissions(map[AuthorityScope][]string{
		AuthRepo: {"*"},     // next wildcard ∩ acc = acc's repo set
		AuthUser: {"model"}, // shared
	})
	got := narrowPermissions(acc, next)
	if !reflect.DeepEqual(got.byScope[AuthRepo], []string{"mcp", "tools.allow"}) {
		t.Fatalf("repo narrow = %v, want acc set (next '*')", got.byScope[AuthRepo])
	}
	if !got.mayChange(AuthUser, "model") {
		t.Fatal("user model should survive the intersection")
	}
}

func TestResolveProfileReplaceModeEndToEnd(t *testing.T) {
	profiles := []ConfigProfile{
		{Ref: "r:1", Kind: ProfileKindAgentCapability, Scope: AuthRepo, Bundle: map[string]any{"model": "x"}},
	}
	policies := []LayeringPolicy{{Scope: AuthOrg, Mode: PolicyModeReplace}}
	got := ResolveProfile(ProfileSet{Profiles: profiles, Policies: policies},
		ProfileContext{ScopeChain: []AuthorityScope{AuthRepo, AuthOrg}})
	if got.PolicyMode != PolicyModeReplace || got.ReplacedBy != AuthOrg {
		t.Fatalf("expected replace mode by org, got %q/%q", got.PolicyMode, got.ReplacedBy)
	}
}

func TestPrecedenceRankerAbsentScopeSortsFirst(t *testing.T) {
	profiles := []ConfigProfile{
		{Ref: "a:1", Kind: ProfileKindAgentCapability, Scope: AuthUser, Bundle: map[string]any{"model": "user"}},
		{Ref: "o:1", Kind: ProfileKindAgentCapability, Scope: AuthOrg, Bundle: map[string]any{"model": "org"}},
	}
	// Precedence lists only org; user is absent → ranks lowest (sorts first), so
	// org is the local-wins tail.
	policies := []LayeringPolicy{{Scope: AuthOrg, Precedence: []AuthorityScope{AuthOrg}}}
	got := ResolveProfile(ProfileSet{Profiles: profiles, Policies: policies},
		ProfileContext{ScopeChain: []AuthorityScope{AuthUser, AuthOrg}})
	if got.Bundle["model"] != "org" {
		t.Fatalf("model = %v, want org (user absent from precedence sorts first)", got.Bundle["model"])
	}
}

func TestDeepBundlePermissionSkip(t *testing.T) {
	// An app_type (deep-merge) profile whose scope is not permitted to change the
	// top-level field is skipped (Decision 2 on the deep path).
	policy := LayeringPolicy{
		Scope:               AuthOrg,
		OverridePermissions: NewOverridePermissions(map[AuthorityScope][]string{AuthOrg: {"relevance"}}),
	}
	profiles := []ConfigProfile{
		{Ref: "repo:go", Kind: ProfileKindAppType, Scope: AuthRepo, Selector: ProfileSelector{AppType: "go"},
			Bundle: map[string]any{"topology": map[string]any{"executors": float64(9)}}},
	}
	got := ResolveProfile(ProfileSet{Profiles: profiles, Policies: []LayeringPolicy{policy}},
		ProfileContext{AppType: "go", ScopeChain: []AuthorityScope{AuthRepo, AuthOrg}})
	if _, ok := got.Bundle["topology"]; ok {
		t.Fatal("repo topology write should be gated out (repo not in allowlist)")
	}
}

func TestApplyDenyMembersNonSetField(t *testing.T) {
	// A deny-lock on a scalar / absent field is a no-op, not a panic.
	policy := LayeringPolicy{Scope: AuthOrg, Locks: []ProfileLock{
		{Field: "model", Deny: []string{"x"}, Owner: AuthOrg},
		{Field: "absent.path", Deny: []string{"y"}, Owner: AuthOrg},
	}}
	profiles := []ConfigProfile{
		{Ref: "r:1", Kind: ProfileKindAgentCapability, Scope: AuthRepo, Bundle: map[string]any{"model": "keep"}},
	}
	got := ResolveProfile(ProfileSet{Profiles: profiles, Policies: []LayeringPolicy{policy}},
		ProfileContext{ScopeChain: []AuthorityScope{AuthRepo, AuthOrg}})
	if got.Bundle["model"] != "keep" {
		t.Fatalf("scalar deny-lock should be a no-op, model = %v", got.Bundle["model"])
	}
}

func TestLockInfosTieBreakByOwner(t *testing.T) {
	locks := []ProfileLock{
		{Field: "tools.allow", Deny: []string{"Edit"}, Owner: AuthTeam},
		{Field: "tools.allow", Deny: []string{"Write"}, Owner: AuthOrg},
	}
	infos := lockInfos(locks)
	if len(infos) != 2 || infos[0].Owner != AuthOrg || infos[1].Owner != AuthTeam {
		t.Fatalf("same-field locks should sort by owner: %+v", infos)
	}
}

func TestSnapshotSkipsNilRawLayer(t *testing.T) {
	snap := &Snapshot{Layers: []ResolvedLayer{
		{ID: LayerProductDefaults, Present: false, Raw: nil},
		{ID: LayerRepoLocal, Present: true, Raw: map[string]any{"project": "x"}},
	}}
	if _, err := ProfileSetFromSnapshot(snap); err != nil {
		t.Fatalf("a nil-raw layer should be skipped, not error: %v", err)
	}
	chain := SnapshotScopeChain(snap)
	if len(chain) != 1 || chain[0] != AuthRepo {
		t.Fatalf("scope chain should only include present layers: %v", chain)
	}
}

func TestSnapshotLayerDecodeError(t *testing.T) {
	// A layer whose typed field has the wrong shape fails the manifest decode.
	snap := &Snapshot{Layers: []ResolvedLayer{
		{ID: LayerRepoLocal, Present: true, Raw: map[string]any{"skills": "not-an-array"}},
	}}
	if _, err := ProfileSetFromSnapshot(snap); err == nil {
		t.Fatal("expected a layer manifest decode error")
	}
}

func TestResolveProfileContextPropagatesError(t *testing.T) {
	snap := &Snapshot{Layers: []ResolvedLayer{
		{ID: LayerRepoLocal, Present: true, Raw: map[string]any{
			"layering_policy": map[string]any{"mode": "bogus"},
		}},
	}}
	if _, err := ResolveProfileContext(snap, "", "go", "", ""); err == nil {
		t.Fatal("malformed policy must propagate through ResolveProfileContext")
	}
}
