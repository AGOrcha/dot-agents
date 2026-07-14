package forkbproto

import "fmt"

// ResolutionCache is a memoizing wrapper over ResolveTwoPhase. Its correctness
// hinges entirely on the KEY function. Invariant 5 (pre-registration): the phase-2
// result is family-scoped, so the frozen phase-1 family is a HIDDEN input to the
// resolution — a cache key built only from the raw context dimensions can collide
// two contexts that resolve to different families and serve a stale, wrong-family
// bundle.
type ResolutionCache struct {
	key   func(Context, string) string // (ctx, frozenFamily) → key
	store map[string]Resolution
}

func NewResolutionCache(key func(Context, string) string) *ResolutionCache {
	return &ResolutionCache{key: key, store: map[string]Resolution{}}
}

// Get resolves via the two-phase engine, memoized under the cache's key. To model
// a real cache faithfully it computes the frozen family first (cheap phase-1),
// then consults the key — a BUGGY key that ignores that family collides.
func (c *ResolutionCache) Get(set ProfileSet, ctx Context) Resolution {
	frozen := freezeFamily(set, ctx)
	k := c.key(ctx, frozen)
	if r, ok := c.store[k]; ok {
		return r
	}
	r := ResolveTwoPhase(set, ctx)
	c.store[k] = r
	return r
}

// freezeFamily runs only phase-1 to obtain the frozen family for cache keying.
func freezeFamily(set ProfileSet, ctx Context) string {
	p1policy := resolveEffectivePolicy(set, ctx, "")
	var frags []ConfigProfile
	for _, pr := range set.Profiles {
		if !ctx.inChain(pr.Scope) || pr.Selector.familyScoped() {
			continue
		}
		if pr.Selector.matches(ctx, "") {
			frags = append(frags, pr)
		}
	}
	orderProfiles(frags, p1policy)
	b := map[string]any{}
	for _, pr := range frags {
		mergeInto(b, pr, p1policy)
	}
	applyLocks(b, p1policy.Locks, ctx, "")
	return familyOf(b)
}

// BuggyKey ignores the frozen family — it keys ONLY on the harness dimension (a
// plausible "family follows harness" shortcut). Two contexts sharing a harness but
// resolving to different families collide.
func BuggyKey(ctx Context, _ string) string { return "h=" + ctx.Harness }

// CorrectKey includes the frozen phase-1 family, so different-family resolutions
// never alias.
func CorrectKey(ctx Context, frozen string) string {
	return fmt.Sprintf("h=%s|fam=%s", ctx.Harness, frozen)
}
