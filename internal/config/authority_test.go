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

// grantLayer builds an authorityLayer carrying a pre-parsed authority_grants set
// at the given authority scope (resolveAuthorityGrants reads the parsed grants).
func grantLayer(scope AuthorityScope, grants map[string]string) authorityLayer {
	g := map[string]AuthorityScope{}
	for k, v := range grants {
		g[k] = AuthorityScope(v)
	}
	return authorityLayer{scope: scope, grants: g}
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
			name:        "org may bless a source DOWNWARD to team (strictly higher)",
			layer:       grantLayer(AuthOrg, map[string]string{"acme": "team"}),
			wantHonored: map[string]AuthorityScope{"acme": AuthTeam},
		},
		{
			name:      "org granting its OWN rank (peer) is rejected — strictly-higher only",
			layer:     grantLayer(AuthOrg, map[string]string{"acme": "org"}),
			wantKind:  violSelfBlessing,
			wantFatal: true,
		},
		{
			name:      "team granting its OWN rank (peer) is rejected",
			layer:     grantLayer(AuthTeam, map[string]string{"acme": "team"}),
			wantKind:  violSelfBlessing,
			wantFatal: true,
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

// TestResolveAuthorityGrants_OverwriteRejected proves a same-or-lower-rank later
// layer cannot overwrite/downgrade a higher scope's grant (the downgrade vector).
func TestResolveAuthorityGrants_OverwriteRejected(t *testing.T) {
	// org grants acme→team first; a later repo layer tries to downgrade acme→user.
	layers := []authorityLayer{
		grantLayer(AuthOrg, map[string]string{"acme": "team"}),
		grantLayer(AuthRepo, map[string]string{"acme": "user"}),
	}
	grants, viols := resolveAuthorityGrants(layers)
	if grants["acme"] != AuthTeam {
		t.Fatalf("higher grant must survive: acme=%q, want team", grants["acme"])
	}
	if len(viols) != 1 || viols[0].Kind != violGrantOverwrite || !viols[0].Fatal {
		t.Fatalf("expected one fatal grant_overwrite violation, got %+v", viols)
	}

	// A PEER (same rank) later layer also cannot overwrite an incumbent grant.
	peer := []authorityLayer{
		grantLayer(AuthOrg, map[string]string{"acme": "team"}),
		grantLayer(AuthOrg, map[string]string{"acme": "repo"}),
	}
	g2, v2 := resolveAuthorityGrants(peer)
	if g2["acme"] != AuthTeam || len(v2) != 1 || v2[0].Kind != violGrantOverwrite {
		t.Fatalf("peer overwrite must be rejected, got grants=%v viols=%+v", g2, v2)
	}
}

func TestResolveAuthorityGrants_EmptyIsClean(t *testing.T) {
	grants, viols := resolveAuthorityGrants([]authorityLayer{{scope: AuthOrg}})
	if len(grants) != 0 || len(viols) != 0 {
		t.Fatalf("a layer with no grants must contribute nothing: grants=%v viols=%v", grants, viols)
	}
}

// TestParseGrants_FailClosed proves a malformed authority_grants block is a
// validation ERROR (fail-closed), never a silent skip.
func TestParseGrants_FailClosed(t *testing.T) {
	if g, err := parseGrants(map[string]any{}); err != nil || g != nil {
		t.Fatalf("absent grants: want (nil,nil), got (%v,%v)", g, err)
	}
	good, err := parseGrants(map[string]any{"authority_grants": map[string]any{"acme": "team"}})
	if err != nil || good["acme"] != AuthTeam {
		t.Fatalf("well-formed grant must parse, got (%v,%v)", good, err)
	}
	cases := []struct {
		name string
		raw  map[string]any
	}{
		{"non-object block", map[string]any{"authority_grants": "not-an-object"}},
		{"non-string value", map[string]any{"authority_grants": map[string]any{"acme": 42}}},
		{"unknown scope", map[string]any{"authority_grants": map[string]any{"acme": "superuser"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := parseGrants(c.raw); err == nil {
				t.Fatalf("malformed grants (%s) must error, got nil", c.name)
			}
		})
	}
}

func TestValidateLockSpec_FailClosed(t *testing.T) {
	if err := validateLockSpec(PolicyLockSpec{DenyLocks: []string{"skills:risky"}}); err != nil {
		t.Fatalf("well-formed deny_lock must validate, got %v", err)
	}
	if err := validateLockSpec(PolicyLockSpec{DenyLocks: []string{"no-colon"}}); err == nil {
		t.Fatal("malformed deny_lock must be a validation error")
	}
}

func TestValidScope(t *testing.T) {
	for _, s := range []AuthorityScope{AuthOrg, AuthTeam, AuthRepo, AuthUser, AuthProduct, AuthPublic, AuthRuntime, AuthProjectLocal} {
		if !validScope(s) {
			t.Errorf("%q must be a valid scope", s)
		}
	}
	if validScope(AuthorityScope("superuser")) {
		t.Error("unknown scope must be invalid")
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
