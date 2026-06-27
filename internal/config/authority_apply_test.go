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

// --- EXPLOIT-DIRECTION negative controls (the holes the audit found) --------

// EXPLOIT 1 (FIX B): a LOWER-scope deny must NOT erase a HIGHER/peer allow.
func TestExploit_LowerDenyCannotEraseHigherAllow(t *testing.T) {
	// user (rank 1) deny-locks skills:privileged; repo (rank 2) ALLOWS it.
	// deny binds only lower scopes, so the repo allow SURVIVES.
	snap := mustResolve(t,
		layer(LayerUserLocal, map[string]any{"locks": map[string]any{
			"deny_locks": []any{"skills:privileged"},
		}}),
		layer(LayerRepoLocal, map[string]any{"skills": []any{"privileged"}}),
	)
	if !containsStr(snap.Effective.Skills, "privileged") {
		t.Fatalf("a lower deny must NOT erase a higher allow: skills=%v", snap.Effective.Skills)
	}
	if len(snap.LockCollisions) != 0 {
		t.Fatalf("an ineffective lower deny must not record a collision, got %+v", snap.LockCollisions)
	}

	// Control: when the SAME deny owner outranks the contributor, it DOES bind.
	bound := mustResolve(t,
		layer(LayerUserLocal, map[string]any{"skills": []any{"privileged"}}),
		layer(LayerRepoLocal, map[string]any{"locks": map[string]any{
			"deny_locks": []any{"skills:privileged"},
		}}),
	)
	if containsStr(bound.Effective.Skills, "privileged") {
		t.Fatalf("a higher deny must bind a lower allow: skills=%v", bound.Effective.Skills)
	}
}

// EXPLOIT 2 (FIX D): a value-lock on a NESTED field path must pin the nested
// value, not a literal top-level key, and leave siblings intact.
func TestExploit_NestedFieldPathValueLock(t *testing.T) {
	snap := mustResolve(t,
		layer(LayerUserLocal, map[string]any{
			"features": map[string]any{"graph_bridge": "on", "other": "keep"},
		}),
		layer(LayerRepoLocal, map[string]any{"locks": map[string]any{
			"value_locks": map[string]any{"features.graph_bridge": "off"},
		}}),
	)
	m, err := snap.EffectiveRaw()
	if err != nil {
		t.Fatalf("EffectiveRaw: %v", err)
	}
	feats, ok := m["features"].(map[string]any)
	if !ok {
		t.Fatalf("features must remain a nested object, got %T", m["features"])
	}
	if feats["graph_bridge"] != "off" {
		t.Fatalf("nested value-lock must pin features.graph_bridge=off, got %v", feats["graph_bridge"])
	}
	if feats["other"] != "keep" {
		t.Fatalf("sibling nested key must be preserved, got %v", feats["other"])
	}
	if _, stray := m["features.graph_bridge"]; stray {
		t.Fatal("a nested lock must NOT create a literal top-level dotted key")
	}
	c := findCollision(t, snap.LockCollisions, "features.graph_bridge")
	if c.Attempted != "on" || c.Winning != "off" || c.Owner != AuthRepo {
		t.Fatalf("collision = %+v, want attempted=on winning=off owner=repo", c)
	}
}

// EXPLOIT 5 (HARDENING): a malformed policy is a resolve-time error (fail-closed).
func TestExploit_MalformedPolicyFailsClosed(t *testing.T) {
	cases := []struct {
		name string
		raw  map[string]any
	}{
		{"non-object grants", map[string]any{"authority_grants": "not-an-object"}},
		{"unknown grant scope", map[string]any{"authority_grants": map[string]any{"acme": "superuser"}}},
		{"non-object locks", map[string]any{"locks": "not-an-object"}},
		{"malformed deny_lock", map[string]any{"locks": map[string]any{"deny_locks": []any{"no-colon"}}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := resolve(t, layer(LayerRepoLocal, c.raw))
			if err == nil || !strings.Contains(err.Error(), "malformed") {
				t.Fatalf("malformed policy (%s) must fail closed, got err=%v", c.name, err)
			}
		})
	}
}

// EXPLOIT D2 (round 3): a TYPO'd or unknown lock key must FAIL CLOSED — strict
// decoding, so a mistyped admin deny can never silently bind nothing.
func TestExploit_UnknownLockFieldFailsClosed(t *testing.T) {
	cases := []struct {
		name  string
		locks map[string]any
	}{
		{"deny_lock typo (missing s)", map[string]any{"deny_lock": []any{"skills:risky"}}},
		{"force_alow typo", map[string]any{"force_alow": []any{"Edit"}}},
		{"unknown lock key", map[string]any{"bogus_lock": []any{"x"}}},
		{"array-index value_lock path", map[string]any{"value_locks": map[string]any{"skills.0": "x"}}},
		{"empty-segment value_lock path", map[string]any{"value_locks": map[string]any{"a..b": "x"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := resolve(t, layer(LayerRepoLocal, map[string]any{"locks": c.locks}))
			if err == nil || !strings.Contains(err.Error(), "malformed") {
				t.Fatalf("typo'd/unknown lock (%s) must fail closed, got err=%v", c.name, err)
			}
		})
	}

	// A VALID lock spec still resolves (no false-positive regression).
	snap := mustResolve(t, layer(LayerRepoLocal, map[string]any{"locks": map[string]any{
		"value_locks": map[string]any{"model": "X"},
		"deny_locks":  []any{"skills:risky"},
	}}))
	if got := rawModel(t, snap); got != "X" {
		t.Fatalf("valid lock spec must resolve, model=%v want X", got)
	}
}

// EXPLOIT C (round 3): overlapping value-lock paths are ambiguous and rejected
// deterministically (fail-closed), not resolved in Go map-iteration order.
func TestExploit_OverlappingLockPathsRejected(t *testing.T) {
	// repo locks the broad "features" AND the nested "features.graph_bridge".
	_, err := resolve(t, layer(LayerRepoLocal, map[string]any{"locks": map[string]any{
		"value_locks": map[string]any{
			"features":              map[string]any{"graph_bridge": "a"},
			"features.graph_bridge": "b",
		},
	}}))
	if err == nil || !strings.Contains(err.Error(), "overlapping") {
		t.Fatalf("overlapping value-locks must be rejected, got err=%v", err)
	}

	// Deterministic across runs: same error every time (map order must not leak).
	for i := 0; i < 8; i++ {
		_, e := resolve(t, layer(LayerRepoLocal, map[string]any{"locks": map[string]any{
			"value_locks": map[string]any{
				"features":              map[string]any{"graph_bridge": "a"},
				"features.graph_bridge": "b",
			},
		}}))
		if e == nil || !strings.Contains(e.Error(), `"features" is a prefix of "features.graph_bridge"`) {
			t.Fatalf("overlap rejection must be deterministic, run %d got %v", i, e)
		}
	}

	// Non-overlapping sibling paths are fine.
	snap := mustResolve(t, layer(LayerRepoLocal, map[string]any{"locks": map[string]any{
		"value_locks": map[string]any{"features.a": "1", "features.b": "2"},
	}}))
	m, _ := snap.EffectiveRaw()
	feats := m["features"].(map[string]any)
	if feats["a"] != "1" || feats["b"] != "2" {
		t.Fatalf("disjoint nested locks must both apply, got %v", feats)
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

func TestExtractLocks_FailClosed(t *testing.T) {
	absent, err := extractLocks(map[string]any{})
	if err != nil || !absent.IsZero() {
		t.Fatalf("absent locks block: want (zero,nil), got (%+v,%v)", absent, err)
	}
	good, err := extractLocks(map[string]any{"locks": map[string]any{"deny_locks": []any{"a:b"}}})
	if err != nil || len(good.DenyLocks) != 1 {
		t.Fatalf("well-formed locks block must decode, got (%+v,%v)", good, err)
	}
	// Malformed block (not an object) and a malformed deny_lock both error.
	if _, err := extractLocks(map[string]any{"locks": "not-an-object"}); err == nil {
		t.Error("non-object locks block must be a validation error")
	}
	if _, err := extractLocks(map[string]any{"locks": map[string]any{"deny_locks": []any{"no-colon"}}}); err == nil {
		t.Error("malformed deny_lock must be a validation error")
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
	attempted, found := rejectedWrite(layers, []string{"model"}, "X", AuthorityRankOf(AuthRepo))
	if found {
		t.Fatalf("a lower write equal to the winning value must not be flagged, got %v", attempted)
	}
}

// TestSetPath covers the nested-path setter used by value-lock pinning.
func TestSetPath(t *testing.T) {
	m := map[string]any{"features": map[string]any{"other": "Z"}}
	setPath(m, []string{"features", "flag"}, "X")
	feats := m["features"].(map[string]any)
	if feats["flag"] != "X" || feats["other"] != "Z" {
		t.Fatalf("nested set must pin leaf and preserve siblings, got %v", feats)
	}
	// A non-object intermediate is replaced so the pin still lands.
	m2 := map[string]any{"a": "scalar"}
	setPath(m2, []string{"a", "b"}, 1)
	if m2["a"].(map[string]any)["b"] != 1 {
		t.Fatalf("non-object intermediate must be replaced, got %v", m2)
	}
	// Empty path is a no-op (no panic).
	setPath(m2, nil, "ignored")
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
