package forkbproto

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"testing"
)

// raw marshals a Go value to json.RawMessage for value-locks.
func raw(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

// permute3 returns all 6 orderings of a 3-element slice.
func permute3(in []ConfigProfile) [][]ConfigProfile {
	idx := [][]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}}
	var out [][]ConfigProfile
	for _, p := range idx {
		out = append(out, []ConfigProfile{in[p[0]], in[p[1]], in[p[2]]})
	}
	return out
}

func outcome(r Resolution) string {
	_, hasSentinel := r.Bundle["sentinel"]
	return fmt.Sprintf("family=%s sentinel=%v", r.Family, hasSentinel)
}

// ---------------------------------------------------------------------------
// TEST 1 — Hazard permutation fixture (DECISIVE)
// ---------------------------------------------------------------------------

func TestHazardPermutation(t *testing.T) {
	chain := []AuthorityScope{"repo"}
	// A: sets family=claude (low Order). B: selects family=claude + sentinel.
	// C: sets family=gpt (higher Order) — so the FROZEN phase-1 family is gpt.
	A := ConfigProfile{Ref: "A", Scope: "repo", Order: 0, Bundle: map[string]any{"model_family": "claude"}}
	B := ConfigProfile{Ref: "B", Scope: "repo", Order: 1, Selector: ProfileSelector{ModelFamily: "claude"}, Bundle: map[string]any{"sentinel": "B-applied"}}
	C := ConfigProfile{Ref: "C", Scope: "repo", Order: 2, Bundle: map[string]any{"model_family": "gpt"}}
	ctx := Context{ScopeChain: chain}

	twoPhaseSet := map[string]bool{}
	naiveSet := map[string]bool{}
	for _, perm := range permute3([]ConfigProfile{A, B, C}) {
		set := ProfileSet{Profiles: perm}
		tp := ResolveTwoPhase(set, ctx)
		nv := ResolveNaiveSinglePhase(set, ctx)
		twoPhaseSet[outcome(tp)] = true
		naiveSet[outcome(nv)] = true
		// Frozen phase-1 family must be gpt (C wins by Order) in EVERY permutation.
		if tp.Family != "gpt" {
			t.Errorf("two-phase perm %v: frozen family = %q, want gpt", refsOf(perm), tp.Family)
		}
		// B applies IFF frozen family == claude. Here frozen==gpt ⇒ NOT applied.
		if _, has := tp.Bundle["sentinel"]; has {
			t.Errorf("two-phase perm %v: sentinel applied though frozen family=gpt", refsOf(perm))
		}
	}
	if len(twoPhaseSet) != 1 {
		t.Errorf("two-phase NOT deterministic: %v", keysOf(twoPhaseSet))
	} else {
		t.Logf("two-phase DETERMINISTic across all 6 perms: %v", keysOf(twoPhaseSet))
	}
	if len(naiveSet) <= 1 {
		t.Errorf("naive expected order-dependent, but was deterministic: %v", keysOf(naiveSet))
	} else {
		t.Logf("naive ORDER-DEPENDENT (hazard shown): %d distinct outcomes %v", len(naiveSet), keysOf(naiveSet))
	}

	// Second scenario: flip Orders so A(claude) is highest ⇒ frozen==claude ⇒ B DOES apply.
	A2 := A
	A2.Order = 5
	for _, perm := range permute3([]ConfigProfile{A2, B, C}) {
		tp := ResolveTwoPhase(ProfileSet{Profiles: perm}, ctx)
		if tp.Family != "claude" {
			t.Errorf("flip perm %v: frozen=%q want claude", refsOf(perm), tp.Family)
		}
		if _, has := tp.Bundle["sentinel"]; !has {
			t.Errorf("flip perm %v: sentinel NOT applied though frozen family=claude", refsOf(perm))
		}
	}
	t.Logf("iff-condition confirmed: flip A.Order>C ⇒ frozen=claude ⇒ B applies, deterministically")
}

// ---------------------------------------------------------------------------
// TEST 2 — No-collision NEGATIVE control
// ---------------------------------------------------------------------------

func TestNoCollisionControl(t *testing.T) {
	ctx := Context{ScopeChain: []AuthorityScope{"repo"}}
	// Family-as-SELECTOR fragments, but NO family-as-VALUE fragment anywhere.
	base := ConfigProfile{Ref: "base", Scope: "repo", Order: 0, Bundle: map[string]any{"base": "x"}}
	selC := ConfigProfile{Ref: "selC", Scope: "repo", Order: 1, Selector: ProfileSelector{ModelFamily: "claude"}, Bundle: map[string]any{"sentinel": "c"}}
	selG := ConfigProfile{Ref: "selG", Scope: "repo", Order: 2, Selector: ProfileSelector{ModelFamily: "gpt"}, Bundle: map[string]any{"sentinel": "g"}}

	var tpFirst, nvFirst map[string]any
	for i, perm := range permute3([]ConfigProfile{base, selC, selG}) {
		set := ProfileSet{Profiles: perm}
		tp := ResolveTwoPhase(set, ctx)
		nv := ResolveNaiveSinglePhase(set, ctx)
		if i == 0 {
			tpFirst, nvFirst = tp.Bundle, nv.Bundle
		}
		if !reflect.DeepEqual(tp.Bundle, tpFirst) {
			t.Errorf("two-phase NOT order-invariant in control: %v vs %v", tp.Bundle, tpFirst)
		}
		if !reflect.DeepEqual(nv.Bundle, nvFirst) {
			t.Errorf("naive NOT order-invariant in control: %v vs %v", nv.Bundle, nvFirst)
		}
		if !reflect.DeepEqual(tp.Bundle, nv.Bundle) {
			t.Errorf("resolvers DISAGREE in no-collision control: two-phase=%v naive=%v", tp.Bundle, nv.Bundle)
		}
	}
	if _, has := tpFirst["sentinel"]; has {
		t.Errorf("no family-value fragment ⇒ family unresolved ⇒ no selector should fire; got %v", tpFirst)
	}
	t.Logf("control: both resolvers deterministic AND identical (no family-value ⇒ no hazard): %v", tpFirst)
}

// ---------------------------------------------------------------------------
// TEST 3 — Invariant 1: locks are absolute under two-phase
// ---------------------------------------------------------------------------

func TestInvariant1Locks(t *testing.T) {
	ctx := Context{Role: "reviewer", ScopeChain: []AuthorityScope{"repo", "runtime"}}
	// A tries family=gpt; a runtime value-lock pins family=claude regardless.
	A := ConfigProfile{Ref: "A", Scope: "repo", Order: 5, Bundle: map[string]any{"model_family": "gpt"}}
	// A family-scoped-claude fragment: fires ONLY if frozen==claude. Also tries to
	// re-pin family=gpt (must be dropped by no-self-reference) and add Write back.
	B := ConfigProfile{Ref: "B", Scope: "repo", Order: 6, Selector: ProfileSelector{ModelFamily: "claude"},
		Bundle: map[string]any{"sentinel": "B", "model_family": "gpt", "tools": map[string]any{"allow": []any{"Read", "Write"}}}}
	base := ConfigProfile{Ref: "base", Scope: "repo", Order: 0, Bundle: map[string]any{"tools": map[string]any{"allow": []any{"Read", "Write"}}}}

	policy := LayeringPolicy{Scope: "runtime", Locks: []ProfileLock{
		{Field: "model_family", Value: raw("claude"), Owner: "runtime"},
		{Field: "tools.allow", Deny: []string{"Write"}, Selector: ProfileSelector{Role: "reviewer"}, Owner: "runtime"},
	}}
	set := ProfileSet{Profiles: []ConfigProfile{A, B, base}, Policies: []LayeringPolicy{policy}}
	r := ResolveTwoPhase(set, ctx)

	if r.Family != "claude" {
		t.Errorf("value-lock: frozen family = %q, want claude (lock must beat A's gpt)", r.Family)
	}
	if _, has := r.Bundle["sentinel"]; !has {
		t.Errorf("frozen==claude via lock ⇒ family-scoped B should apply; bundle=%v", r.Bundle)
	}
	allow := toStrings(getPath(r.Bundle, "tools.allow"))
	if contains(allow, "Write") {
		t.Errorf("deny-lock: Write must be removed, got %v", allow)
	}
	if !contains(allow, "Read") {
		t.Errorf("deny-lock over-removed; Read should survive, got %v", allow)
	}
	t.Logf("locks absolute across both phases: family pinned=%s, tools.allow=%v, applied=%v", r.Family, allow, r.AppliedLocks)
}

// ---------------------------------------------------------------------------
// TEST 4 — Invariant 2: PolicyModeReplace, locks still accumulate, no phase leak
// ---------------------------------------------------------------------------

func TestInvariant2Replace(t *testing.T) {
	ctx := Context{Role: "reviewer", ScopeChain: []AuthorityScope{"repo", "runtime"}}
	A := ConfigProfile{Ref: "A", Scope: "repo", Order: 0, Bundle: map[string]any{"model_family": "claude"}}
	B := ConfigProfile{Ref: "B", Scope: "runtime", Order: 1, Selector: ProfileSelector{ModelFamily: "claude"},
		Bundle: map[string]any{"tools": map[string]any{"allow": []any{"Read", "Delete"}}}}

	// Lower policy: deny-lock on Delete (must survive a replace). Narrow.
	pLow := LayeringPolicy{Scope: "repo", Mode: PolicyModeNarrow,
		Locks: []ProfileLock{{Field: "tools.allow", Deny: []string{"Delete"}, Owner: "repo"}},
		Permissions: map[AuthorityScope][]string{"repo": {"model_family"}, "runtime": {"tools"}}}
	// Higher, FAMILY-SCOPED policy: replace resets precedence + permissions.
	pHigh := LayeringPolicy{Scope: "runtime", Mode: PolicyModeReplace, Selector: ProfileSelector{ModelFamily: "claude"},
		Permissions: map[AuthorityScope][]string{"repo": {"*"}, "runtime": {"*"}}}

	set := ProfileSet{Profiles: []ConfigProfile{A, B}, Policies: []LayeringPolicy{pLow, pHigh}}
	r := ResolveTwoPhase(set, ctx)

	if r.Mode != PolicyModeReplace || r.ReplacedBy != "runtime" {
		t.Errorf("expected replace by runtime, got mode=%s by=%s", r.Mode, r.ReplacedBy)
	}
	if r.Family != "claude" {
		t.Errorf("phase leak: frozen family changed to %q under replace (phase-1 must be unaffected)", r.Family)
	}
	allow := toStrings(getPath(r.Bundle, "tools.allow"))
	if contains(allow, "Delete") {
		t.Errorf("lock must ACCUMULATE across replace: Delete should be denied, got %v", allow)
	}
	if !contains(allow, "Read") {
		t.Errorf("replace reset permissions to allow runtime tools write; Read expected, got %v", allow)
	}
	t.Logf("replace resets precedence/perms but deny-lock accumulates; no phase leak (family=%s, allow=%v)", r.Family, allow)
}

// ---------------------------------------------------------------------------
// TEST 5 — Invariant 3: adding ModelFamily to specificity() is a no-op for
// family-free profiles (tie-break regression).
// ---------------------------------------------------------------------------

func TestInvariant3TieBreakRegression(t *testing.T) {
	// Property: for ANY family-free selector, specificity()==legacySpecificity().
	familyFree := []ProfileSelector{
		{}, {Role: "reviewer"}, {Stage: "review"}, {Role: "reviewer", Stage: "review"},
		{AppType: "go-cli", Harness: "claude-code"}, {Role: "r", AppType: "a", Stage: "s", Harness: "h"},
	}
	for _, s := range familyFree {
		if s.specificity() != s.legacySpecificity() {
			t.Errorf("specificity drift on family-free selector %+v: new=%d legacy=%d", s, s.specificity(), s.legacySpecificity())
		}
	}
	// Concrete ordering fixture pair: same Order+Scope, differ only by ref & a
	// role-vs-none specificity difference. Winner (last) must be identical whether
	// specificity counts 4 or 5 keys.
	p1 := ConfigProfile{Ref: "zzz", Scope: "repo", Order: 0, Selector: ProfileSelector{}}
	p2 := ConfigProfile{Ref: "aaa", Scope: "repo", Order: 0, Selector: ProfileSelector{Role: "reviewer"}}
	profs := []ConfigProfile{p1, p2}
	orderProfiles(profs, EffectivePolicy{})
	// p2 is more specific (role set) ⇒ sorts LAST (wins). ModelFamily unused ⇒ same
	// as legacy. Assert the winner is p2 regardless.
	if profs[len(profs)-1].Ref != "aaa" {
		t.Errorf("tie-break changed: winner=%s, want aaa (more-specific role)", profs[len(profs)-1].Ref)
	}
	// Pure ref tie (equal specificity): lexicographic, unaffected by ModelFamily.
	q := []ConfigProfile{{Ref: "b", Scope: "repo"}, {Ref: "a", Scope: "repo"}}
	orderProfiles(q, EffectivePolicy{})
	if q[0].Ref != "a" || q[1].Ref != "b" {
		t.Errorf("ref tie-break changed: %v", refsOf(q))
	}
	t.Logf("ModelFamily added to specificity() is a no-op for family-free profiles (ties unchanged)")
}

// ---------------------------------------------------------------------------
// TEST 6 — Invariant 5: cache key must include the frozen phase-1 family
// ---------------------------------------------------------------------------

func TestInvariant5CacheKey(t *testing.T) {
	// Two contexts share a harness but resolve to DIFFERENT families (role-scoped
	// family-value fragments). A cache key that omits the frozen family collides.
	Fa := ConfigProfile{Ref: "Fa", Scope: "repo", Order: 0, Selector: ProfileSelector{Role: "r1"}, Bundle: map[string]any{"model_family": "claude"}}
	Fb := ConfigProfile{Ref: "Fb", Scope: "repo", Order: 0, Selector: ProfileSelector{Role: "r2"}, Bundle: map[string]any{"model_family": "gpt"}}
	set := ProfileSet{Profiles: []ConfigProfile{Fa, Fb}}
	ctx1 := Context{Role: "r1", Harness: "H", ScopeChain: []AuthorityScope{"repo"}}
	ctx2 := Context{Role: "r2", Harness: "H", ScopeChain: []AuthorityScope{"repo"}}

	// Sanity: direct resolution gives distinct families.
	if got := ResolveTwoPhase(set, ctx1).Family; got != "claude" {
		t.Fatalf("ctx1 family = %q, want claude", got)
	}
	if got := ResolveTwoPhase(set, ctx2).Family; got != "gpt" {
		t.Fatalf("ctx2 family = %q, want gpt", got)
	}

	// BUGGY key (harness only): ctx1 primes the cache, ctx2 collides ⇒ stale claude.
	buggy := NewResolutionCache(BuggyKey)
	buggy.Get(set, ctx1)
	if got := buggy.Get(set, ctx2).Family; got == "gpt" {
		t.Errorf("expected BUGGY cache to serve STALE claude for ctx2, but got correct gpt")
	} else {
		t.Logf("buggy key (no frozen family) serves STALE %q for ctx2 (should be gpt) — hazard reproduced", got)
	}

	// CORRECT key (includes frozen family): ctx2 misses ⇒ resolves gpt.
	good := NewResolutionCache(CorrectKey)
	good.Get(set, ctx1)
	if got := good.Get(set, ctx2).Family; got != "gpt" {
		t.Errorf("correct-key cache still stale: ctx2 family = %q, want gpt", got)
	} else {
		t.Logf("correct key (frozen family in key) fixes it: ctx2 → gpt")
	}
}

// ---- small helpers ----

func refsOf(ps []ConfigProfile) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.Ref
	}
	return out
}
func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
func toStrings(v any) []string {
	xs, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
