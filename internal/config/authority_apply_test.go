package config

import (
	"strings"
	"testing"
)

// authority_apply_test.go is the dogfood gate for §15.9 items 1-5: the
// NEGATIVE-CONTROL suite that PROVES the attacks fail end-to-end through the
// shared resolveSnapshot merge path, not just that the happy path works.

// layer builds a ResolvedLayer with the given id and raw object.
func layer(id string, raw map[string]any) ResolvedLayer {
	return ResolvedLayer{ID: id, Present: true, Raw: raw}
}

// resolve runs the shared merge + authority pass over an ordered layer stack.
func resolve(t *testing.T, layers ...ResolvedLayer) (*Snapshot, error) {
	t.Helper()
	return resolveSnapshot(layers)
}

func mustResolve(t *testing.T, layers ...ResolvedLayer) *Snapshot {
	t.Helper()
	snap, err := resolveSnapshot(layers)
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	return snap
}

func rawModel(t *testing.T, snap *Snapshot) any {
	t.Helper()
	m, err := snap.EffectiveRaw()
	if err != nil {
		t.Fatalf("EffectiveRaw: %v", err)
	}
	return m["model"]
}

// --- NEGATIVE CONTROL 1: self-blessing rejected -----------------------------

func TestNegativeControl_SelfBlessingRejected(t *testing.T) {
	// A user-scope layer that grants ITSELF (a source it controls) org authority
	// is a resolve-time rejection, not a silent no-op.
	t.Run("user grants org is a fatal error", func(t *testing.T) {
		_, err := resolve(t,
			layer(LayerUserLocal, map[string]any{
				"authority_grants": map[string]any{"evil": "org"},
			}),
			layer(LayerRepoLocal, map[string]any{}),
		)
		if err == nil || !strings.Contains(err.Error(), "self-elevation") {
			t.Fatalf("expected self-elevation rejection, got err=%v", err)
		}
	})

	t.Run("repo grants org is a fatal error", func(t *testing.T) {
		_, err := resolve(t,
			layer(LayerRepoLocal, map[string]any{
				"authority_grants": map[string]any{"evil": "org"},
			}),
		)
		if err == nil || !strings.Contains(err.Error(), "self-elevation") {
			t.Fatalf("expected self-elevation rejection, got err=%v", err)
		}
	})

	// A public/foreign extends source shipping its OWN authority_grants claiming
	// org is INERT: the grant is not honored, so a lock it then tries to emit
	// does NOT bind a real repo value. The attack produces no elevation.
	t.Run("public source self-grant is inert and cannot bind", func(t *testing.T) {
		snap := mustResolve(t,
			layer("evil:layer", map[string]any{
				"authority_grants": map[string]any{"evil": "org"},
				"locks":            map[string]any{"value_locks": map[string]any{"model": "EVIL"}},
				"model":            "evil-value",
			}),
			layer(LayerRepoLocal, map[string]any{"model": "REPO"}),
		)
		if got := rawModel(t, snap); got != "REPO" {
			t.Fatalf("public self-grant must not bind: model=%v, want REPO", got)
		}
		if !hasViolation(snap.AuthorityViolations, violInertGrant) {
			t.Fatalf("expected an inert_grant violation, got %+v", snap.AuthorityViolations)
		}
		if len(snap.LockCollisions) != 0 {
			t.Fatalf("an inert grant must not produce a binding collision, got %+v", snap.LockCollisions)
		}
	})
}

func hasViolation(viols []AuthorityViolation, kind string) bool {
	for _, v := range viols {
		if v.Kind == kind {
			return true
		}
	}
	return false
}

// --- NEGATIVE CONTROL 2: deny-overrides holds -------------------------------

func TestNegativeControl_DenyOverridesHolds(t *testing.T) {
	// repo (rank 2) deny-locks skills:risky; user (rank 1) tries to allow it.
	// A lower-scope allow does NOT beat a higher-scope deny-lock.
	snap := mustResolve(t,
		layer(LayerUserLocal, map[string]any{"skills": []any{"risky", "safe"}}),
		layer(LayerRepoLocal, map[string]any{"locks": map[string]any{
			"deny_locks": []any{"skills:risky"},
		}}),
	)
	if containsStr(snap.Effective.Skills, "risky") {
		t.Fatalf("deny-overrides failed: risky still present in %v", snap.Effective.Skills)
	}
	if !containsStr(snap.Effective.Skills, "safe") {
		t.Fatalf("deny-lock over-subtracted: safe missing from %v", snap.Effective.Skills)
	}
	if !hasCollision(snap.LockCollisions, "skills:risky", collisionDenyLock) {
		t.Fatalf("expected a deny_lock collision for skills:risky, got %+v", snap.LockCollisions)
	}
}

func containsStr(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// --- NEGATIVE CONTROL 3: locked-field collision -----------------------------

func TestNegativeControl_LockedFieldCollision(t *testing.T) {
	// user sets model=Y; repo value-locks model=X. Effective is X, the lower
	// write is rejected, and explain surfaces attempted=Y / winning=X / owner=repo.
	snap := mustResolve(t,
		layer(LayerUserLocal, map[string]any{"model": "Y"}),
		layer(LayerRepoLocal, map[string]any{"locks": map[string]any{
			"value_locks": map[string]any{"model": "X"},
		}}),
	)
	if got := rawModel(t, snap); got != "X" {
		t.Fatalf("locked value must win: model=%v, want X", got)
	}
	c := findCollision(t, snap.LockCollisions, "model")
	if c.Attempted != "Y" || c.Winning != "X" || c.Owner != AuthRepo || c.Kind != collisionValueLock {
		t.Fatalf("collision = %+v, want attempted=Y winning=X owner=repo kind=value_lock", c)
	}
}

// --- NEGATIVE CONTROL 4: no force-allow --------------------------------------

func TestNegativeControl_NoForceAllow(t *testing.T) {
	_, err := resolve(t,
		layer(LayerRepoLocal, map[string]any{"locks": map[string]any{
			"force_allow": []any{"Edit"},
		}}),
	)
	if err == nil || !strings.Contains(err.Error(), "force_allow") {
		t.Fatalf("force_allow must be a validation error, got %v", err)
	}
}

// --- NEGATIVE CONTROL 5: user cannot out-rank repo --------------------------

func TestNegativeControl_UserCannotOutrankRepo(t *testing.T) {
	// A user-scope value-lock does NOT constrain a repo-scope value: user (rank 1)
	// is below repo (rank 2), so the repo value wins and there is no rejection.
	snap := mustResolve(t,
		layer(LayerUserLocal, map[string]any{"locks": map[string]any{
			"value_locks": map[string]any{"model": "USER"},
		}}),
		layer(LayerRepoLocal, map[string]any{"model": "REPO"}),
	)
	if got := rawModel(t, snap); got != "REPO" {
		t.Fatalf("user lock must not out-rank repo: model=%v, want REPO", got)
	}
	if len(snap.LockCollisions) != 0 {
		t.Fatalf("repo value is not a rejected write; got collisions %+v", snap.LockCollisions)
	}

	// Converse: repo value-locks, user attempts a different value → rejected.
	snap2 := mustResolve(t,
		layer(LayerUserLocal, map[string]any{"model": "USER"}),
		layer(LayerRepoLocal, map[string]any{"locks": map[string]any{
			"value_locks": map[string]any{"model": "REPO"},
		}}),
	)
	if got := rawModel(t, snap2); got != "REPO" {
		t.Fatalf("repo lock must bind user: model=%v, want REPO", got)
	}
	c := findCollision(t, snap2.LockCollisions, "model")
	if c.Attempted != "USER" || c.Owner != AuthRepo {
		t.Fatalf("collision = %+v, want attempted=USER owner=repo", c)
	}
}

// --- additive / no-op guarantee ---------------------------------------------

func TestAuthorityPass_NoOpWhenNoPolicy(t *testing.T) {
	// A plain layer stack with no locks/grants resolves exactly as before: no
	// collisions, no violations, value-merge unchanged.
	snap := mustResolve(t,
		layer(LayerUserLocal, map[string]any{"model": "U", "skills": []any{"a"}}),
		layer(LayerRepoLocal, map[string]any{"model": "R", "skills": []any{"b"}}),
	)
	if got := rawModel(t, snap); got != "R" {
		t.Fatalf("most-local value must win: model=%v, want R", got)
	}
	if len(snap.LockCollisions) != 0 || len(snap.AuthorityViolations) != 0 {
		t.Fatalf("no-policy resolve must be clean: collisions=%v viols=%v", snap.LockCollisions, snap.AuthorityViolations)
	}
	if snap.LockCollisions == nil || snap.AuthorityViolations == nil {
		t.Fatal("collision/violation slices must be non-nil for stable JSON")
	}
}

// --- direct helper coverage -------------------------------------------------

func TestBaseLayerScope(t *testing.T) {
	cases := map[string]AuthorityScope{
		LayerProductDefaults: AuthProduct,
		LayerUserLocal:       AuthUser,
		LayerRepoLocal:       AuthRepo,
		LayerProjectLocal:    AuthProjectLocal,
		"acme:org/base":      AuthPublic,
	}
	for id, want := range cases {
		if got := baseLayerScope(id); got != want {
			t.Errorf("baseLayerScope(%q) = %q, want %q", id, got, want)
		}
	}
}

func TestApplyGrantsToScopesUpgradesImported(t *testing.T) {
	layers := []authorityLayer{
		{id: LayerRepoLocal, scope: AuthRepo},
		{id: "acme:org/base", scope: AuthPublic},
	}
	applyGrantsToScopes(layers, map[string]AuthorityScope{"acme": AuthOrg})
	if layers[0].scope != AuthRepo {
		t.Errorf("built-in layer scope must not change, got %q", layers[0].scope)
	}
	if layers[1].scope != AuthOrg {
		t.Errorf("imported layer must be upgraded to org, got %q", layers[1].scope)
	}
	// Empty grant map is a no-op.
	applyGrantsToScopes(layers, nil)
}

func TestSourceIDOf(t *testing.T) {
	if sourceIDOf("acme:org/base") != "acme" {
		t.Error("source id before colon")
	}
	if sourceIDOf("noprefix") != "noprefix" {
		t.Error("no colon returns whole ref")
	}
}

func TestExtractLocksMalformed(t *testing.T) {
	if absent := extractLocks(map[string]any{}); !absent.IsZero() {
		t.Error("absent locks block must be zero")
	}
	if bad := extractLocks(map[string]any{"locks": "not-an-object"}); !bad.IsZero() {
		t.Error("malformed locks block must be zero")
	}
	spec := extractLocks(map[string]any{"locks": map[string]any{"deny_locks": []any{"a:b"}}})
	if len(spec.DenyLocks) != 1 {
		t.Errorf("well-formed locks block must decode, got %+v", spec)
	}
}

func TestRemoveSetMemberAndSetHasMember(t *testing.T) {
	merged := map[string]any{"skills": []any{"a", "b"}, "scalar": "x"}
	removeSetMember(merged, "skills", "a")
	if setHasMember(merged["skills"], "a") {
		t.Error("member a should be removed")
	}
	if !setHasMember(merged["skills"], "b") {
		t.Error("member b should remain")
	}
	// Non-array field is a no-op (no panic).
	removeSetMember(merged, "scalar", "x")
	if !setHasMember([]any{"keep"}, "keep") || setHasMember("not-array", "x") {
		t.Error("setHasMember array/non-array handling wrong")
	}
}

func TestValuesEqual(t *testing.T) {
	if !valuesEqual([]any{"a", "b"}, []any{"a", "b"}) {
		t.Error("equal slices")
	}
	if valuesEqual("x", "y") {
		t.Error("unequal scalars")
	}
}

// TestRejectedWrite_LowerMatchingValueNotFlagged proves a lower-scope layer that
// happens to write the SAME value as the winning lock is not recorded as a
// rejected collision.
func TestRejectedWrite_LowerMatchingValueNotFlagged(t *testing.T) {
	layers := []authorityLayer{
		{id: LayerUserLocal, scope: AuthUser, raw: map[string]any{"model": "X"}},
		{id: LayerRepoLocal, scope: AuthRepo},
	}
	attempted, found := rejectedWrite(layers, "model", "X", AuthorityRankOf(AuthRepo))
	if found {
		t.Fatalf("a lower write equal to the winning value must not be flagged, got %v", attempted)
	}
}

// --- collision helpers ------------------------------------------------------

func hasCollision(cs []LockCollision, field, kind string) bool {
	for _, c := range cs {
		if c.Field == field && c.Kind == kind {
			return true
		}
	}
	return false
}

func findCollision(t *testing.T, cs []LockCollision, field string) LockCollision {
	t.Helper()
	for _, c := range cs {
		if c.Field == field {
			return c
		}
	}
	t.Fatalf("no collision for field %q in %+v", field, cs)
	return LockCollision{}
}
