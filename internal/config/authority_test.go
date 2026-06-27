package config

import (
	"encoding/json"
	"strings"
	"testing"
)

// authority_test.go covers the §15 D1a policy-authority pass core (authority.go):
// the AUTHORITY-RANK order, the source-authority registry write-guard, Phase-1
// lock folding, force-allow validation, and the small parse helpers.

func TestAuthorityRankOf(t *testing.T) {
	cases := []struct {
		scope AuthorityScope
		want  int
	}{
		{AuthOrg, 4}, {AuthTeam, 3}, {AuthRepo, 2}, {AuthUser, 1},
		{AuthProduct, 0}, {AuthPublic, 0}, {AuthRuntime, 0}, {AuthProjectLocal, 0},
		{AuthorityScope("bogus"), 0},
	}
	for _, c := range cases {
		if got := AuthorityRankOf(c.scope); got != c.want {
			t.Errorf("AuthorityRankOf(%q) = %d, want %d", c.scope, got, c.want)
		}
	}
	// The total order org > team > repo > user must hold strictly.
	if !(AuthorityRankOf(AuthOrg) > AuthorityRankOf(AuthTeam) &&
		AuthorityRankOf(AuthTeam) > AuthorityRankOf(AuthRepo) &&
		AuthorityRankOf(AuthRepo) > AuthorityRankOf(AuthUser) &&
		AuthorityRankOf(AuthUser) > AuthorityRankOf(AuthPublic)) {
		t.Fatal("AUTHORITY-RANK total order org>team>repo>user>(value-only) violated")
	}
}

func TestCanonicalScopeOrderingTwoNamedFields(t *testing.T) {
	o := CanonicalScopeOrdering()
	// Item 6 (F1.1): two explicitly-named, distinct orderings — NOT one reused chain.
	if len(o.AuthorityRank) == 0 || len(o.ValuePrecedence) == 0 {
		t.Fatal("both orderings must be populated")
	}
	if o.AuthorityRank[0] != AuthOrg {
		t.Errorf("authority_rank must lead with org, got %q", o.AuthorityRank[0])
	}
	if o.ValuePrecedence[0] != AuthProduct {
		t.Errorf("value_precedence must floor at product, got %q", o.ValuePrecedence[0])
	}
	// The two orderings are genuinely different (the bug D1a fixes).
	if sameOrder(o.AuthorityRank, o.ValuePrecedence) {
		t.Fatal("authority_rank and value_precedence must be distinct orderings")
	}
}

func sameOrder(a, b []AuthorityScope) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestPolicyLockSpecIsZero(t *testing.T) {
	var nilSpec *PolicyLockSpec
	if !nilSpec.IsZero() {
		t.Error("nil spec must be zero")
	}
	if !(&PolicyLockSpec{}).IsZero() {
		t.Error("empty spec must be zero")
	}
	if (&PolicyLockSpec{DenyLocks: []string{"skills:x"}}).IsZero() {
		t.Error("populated spec must not be zero")
	}
}

// grantLayer builds an authorityLayer that declares an authority_grants block.
func grantLayer(scope AuthorityScope, grants map[string]string) authorityLayer {
	g := map[string]any{}
	for k, v := range grants {
		g[k] = v
	}
	return authorityLayer{scope: scope, raw: map[string]any{"authority_grants": g}}
}

func TestResolveAuthorityGrants_WriteGuard(t *testing.T) {
	cases := []struct {
		name        string
		layer       authorityLayer
		wantHonored map[string]AuthorityScope
		wantKind    string // "" => no violation
		wantFatal   bool
	}{
		{
			name:        "org may bless a source to carry org authority",
			layer:       grantLayer(AuthOrg, map[string]string{"acme": "org"}),
			wantHonored: map[string]AuthorityScope{"acme": AuthOrg},
		},
		{
			name:        "org may bless a source downward to team",
			layer:       grantLayer(AuthOrg, map[string]string{"acme": "team"}),
			wantHonored: map[string]AuthorityScope{"acme": AuthTeam},
		},
		{
			name:      "user self-elevating to org is a fatal rejection",
			layer:     grantLayer(AuthUser, map[string]string{"evil": "org"}),
			wantKind:  violSelfBlessing,
			wantFatal: true,
		},
		{
			name:      "repo self-elevating to org is a fatal rejection",
			layer:     grantLayer(AuthRepo, map[string]string{"evil": "org"}),
			wantKind:  violSelfBlessing,
			wantFatal: true,
		},
		{
			name:     "public source claiming org is inert, not fatal",
			layer:    grantLayer(AuthPublic, map[string]string{"foreign": "org"}),
			wantKind: violInertGrant,
		},
		{
			name:     "conferring a zero-authority scope is inert",
			layer:    grantLayer(AuthOrg, map[string]string{"acme": "runtime"}),
			wantKind: violInertGrant,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			grants, viols := resolveAuthorityGrants([]authorityLayer{c.layer})
			assertGrants(t, grants, c.wantHonored)
			assertViolation(t, viols, c.wantKind, c.wantFatal)
		})
	}
}

func assertGrants(t *testing.T, got, want map[string]AuthorityScope) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("honored grants = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("grant[%q] = %q, want %q", k, got[k], v)
		}
	}
}

func assertViolation(t *testing.T, viols []AuthorityViolation, wantKind string, wantFatal bool) {
	t.Helper()
	if wantKind == "" {
		if len(viols) != 0 {
			t.Fatalf("expected no violation, got %+v", viols)
		}
		return
	}
	if len(viols) != 1 {
		t.Fatalf("expected one %q violation, got %+v", wantKind, viols)
	}
	if viols[0].Kind != wantKind {
		t.Fatalf("violation kind = %q, want %q", viols[0].Kind, wantKind)
	}
	if viols[0].Fatal != wantFatal {
		t.Fatalf("violation fatal = %v, want %v (reason: %s)", viols[0].Fatal, wantFatal, viols[0].Reason)
	}
	if viols[0].Reason == "" {
		t.Fatal("violation must carry a reason")
	}
}

func TestResolveAuthorityGrants_DeterministicOrder(t *testing.T) {
	// Multiple rejected grants in one block must surface in sorted source order.
	l := grantLayer(AuthUser, map[string]string{"zeta": "org", "alpha": "team"})
	_, viols := resolveAuthorityGrants([]authorityLayer{l})
	if len(viols) != 2 {
		t.Fatalf("want 2 violations, got %d", len(viols))
	}
	if viols[0].Source != "alpha" || viols[1].Source != "zeta" {
		t.Fatalf("violations not in sorted source order: %q, %q", viols[0].Source, viols[1].Source)
	}
}

func TestResolveAuthorityGrants_Skips(t *testing.T) {
	// A layer with no grants block, and a malformed (non-object) block.
	layers := []authorityLayer{
		{scope: AuthOrg, raw: map[string]any{}},
		{scope: AuthOrg, raw: map[string]any{"authority_grants": "not-an-object"}},
		{scope: AuthOrg, raw: map[string]any{"authority_grants": map[string]any{"acme": 42}}},
	}
	grants, viols := resolveAuthorityGrants(layers)
	if len(grants) != 0 || len(viols) != 0 {
		t.Fatalf("malformed/absent grants must contribute nothing: grants=%v viols=%v", grants, viols)
	}
}

func lockLayer(scope AuthorityScope, spec PolicyLockSpec) authorityLayer {
	return authorityLayer{scope: scope, locks: spec}
}

func vl(field, val string) map[string]json.RawMessage {
	return map[string]json.RawMessage{field: json.RawMessage(`"` + val + `"`)}
}

func TestRunAuthorityPass_HighestAuthorityLockWins(t *testing.T) {
	// repo locks model=R, org locks model=X — org outranks repo.
	layers := []authorityLayer{
		lockLayer(AuthRepo, PolicyLockSpec{ValueLocks: vl("model", "R")}),
		lockLayer(AuthOrg, PolicyLockSpec{ValueLocks: vl("model", "X")}),
	}
	res := runAuthorityPass(layers)
	got, ok := res.valueLocks["model"]
	if !ok {
		t.Fatal("model must be value-locked")
	}
	if got.owner != AuthOrg || got.value != "X" {
		t.Fatalf("winning lock owner=%q value=%v, want org/X", got.owner, got.value)
	}

	// Reverse fold order (org first, then repo) must keep org — a lower-rank
	// lock arriving later does not displace a higher-rank one.
	rev := runAuthorityPass([]authorityLayer{
		lockLayer(AuthOrg, PolicyLockSpec{ValueLocks: vl("model", "X")}),
		lockLayer(AuthRepo, PolicyLockSpec{ValueLocks: vl("model", "R")}),
	})
	if rev.valueLocks["model"].owner != AuthOrg {
		t.Fatalf("lower-rank later lock must not displace org, got %q", rev.valueLocks["model"].owner)
	}
}

func TestRunAuthorityPass_ForceAllowIsFatal(t *testing.T) {
	res := runAuthorityPass([]authorityLayer{
		lockLayer(AuthRepo, PolicyLockSpec{ForceAllow: []string{"Edit"}}),
	})
	fatal := fatalViolations(res.violations)
	if len(fatal) != 1 || fatal[0].Kind != violForceAllow {
		t.Fatalf("force_allow must yield one fatal violation, got %+v", res.violations)
	}
}

func TestRunAuthorityPass_ZeroAuthorityLockIgnored(t *testing.T) {
	// A runtime (zero-authority) scope cannot emit a binding lock.
	res := runAuthorityPass([]authorityLayer{
		lockLayer(AuthRuntime, PolicyLockSpec{ValueLocks: vl("model", "Z")}),
	})
	if len(res.valueLocks) != 0 {
		t.Fatalf("zero-authority lock must not bind, got %v", res.valueLocks)
	}
	if len(res.violations) != 1 || res.violations[0].Kind != violZeroAuthLock {
		t.Fatalf("expected one zero_authority_lock violation, got %+v", res.violations)
	}
}

func TestRunAuthorityPass_DenyLockFolds(t *testing.T) {
	res := runAuthorityPass([]authorityLayer{
		lockLayer(AuthRepo, PolicyLockSpec{DenyLocks: []string{"skills:risky", "malformed-no-colon", "trailing:"}}),
	})
	if len(res.denyLocks) != 1 {
		t.Fatalf("only the well-formed deny-lock should fold, got %+v", res.denyLocks)
	}
	d := res.denyLocks[0]
	if d.field != "skills" || d.member != "risky" || d.owner != AuthRepo {
		t.Fatalf("deny-lock parsed wrong: %+v", d)
	}
}

func TestAuthorityErrorJoinsReasons(t *testing.T) {
	err := authorityError([]AuthorityViolation{
		{Reason: "first"}, {Reason: "second"},
	})
	if !strings.Contains(err.Error(), "first") || !strings.Contains(err.Error(), "second") {
		t.Fatalf("authorityError must join reasons, got %v", err)
	}
}

func TestSplitDenyLock(t *testing.T) {
	cases := []struct {
		raw           string
		field, member string
		ok            bool
	}{
		{"skills:risky", "skills", "risky", true},
		{"a:b:c", "a", "b:c", true},
		{"nocolon", "", "", false},
		{":leading", "", "", false},
		{"trailing:", "", "", false},
	}
	for _, c := range cases {
		f, m, ok := splitDenyLock(c.raw)
		if ok != c.ok || f != c.field || m != c.member {
			t.Errorf("splitDenyLock(%q) = (%q,%q,%v), want (%q,%q,%v)", c.raw, f, m, ok, c.field, c.member, c.ok)
		}
	}
}

func TestDecodeLockValue(t *testing.T) {
	if v := decodeLockValue(json.RawMessage(`"hi"`)); v != "hi" {
		t.Errorf("string lock value = %v, want hi", v)
	}
	if v := decodeLockValue(json.RawMessage(`not json`)); v != "not json" {
		t.Errorf("invalid JSON should fall back to raw string, got %v", v)
	}
}
