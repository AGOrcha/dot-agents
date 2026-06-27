package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// profile_resolver.go is the ONE shared two-phase selector-merge engine (R1) all
// profile kinds resolve through. It builds on the §15 authority substrate
// (AuthorityScope, AuthorityRankOf): authority is SOURCE-derived (Decision 1),
// locks are absolute deny/value pins with no force-allow (Decision 4), and the
// merge is order-independent (H1). Resolution is two phases (§2.3):
//
//	Phase 1 — resolveEffectivePolicy: merge every layering policy whose scope is
//	          in the context chain, low-authority first; higher narrows lower
//	          (Decision 3) unless it declares replace mode (Q4).
//	Phase 2 — ResolveProfile: selector-merge the matching fragments ordered by the
//	          Phase-1 precedence, governed by its permissions (Decision 2) and
//	          locks (Decisions 4/8).

// ProfileContext is the dispatch context resolution runs against. ScopeChain is
// the set of authority scopes present, low→high; a profile or policy whose scope
// is not in the chain does not contribute.
type ProfileContext struct {
	Role       string
	AppType    string
	Stage      string
	Harness    string
	ScopeChain []AuthorityScope
}

func (c ProfileContext) inChain(scope AuthorityScope) bool {
	for _, s := range c.ScopeChain {
		if s == scope {
			return true
		}
	}
	return false
}

// EffectivePolicy is the Phase-1 merged layering policy.
type EffectivePolicy struct {
	// Precedence is the effective scope ordering low→high (later wins scalars).
	Precedence []AuthorityScope
	// Locks are the effective absolute constraints, each tagged with its owner.
	Locks []ProfileLock
	// Permissions is the effective Decision-2 cap (nil ⇒ no restriction).
	Permissions *OverridePermissions
	// Replaced records the scope that issued a replace (Q4), or "" when none did.
	Replaced AuthorityScope
}

// ResolvedProfile is the Phase-2 output for a context.
type ResolvedProfile struct {
	// Bundle is the effective config fragment for the context.
	Bundle map[string]any `json:"bundle"`
	// Contributing is the sorted set of absolute refs that contributed.
	Contributing []string `json:"contributing_refs"`
	// Locks is the effective binding lock set (owner-tagged).
	Locks []ResolvedLockInfo `json:"locks"`
	// Permissions is the effective permission map (scope→fields), nil-omitted.
	Permissions map[AuthorityScope][]string `json:"permissions,omitempty"`
	// Conflicts records same-scope value conflicts: both contributors are shown,
	// not just the winner (Decision 6).
	Conflicts []ProfileConflict `json:"conflicts,omitempty"`
	// PolicyMode is "replace" when a scope superseded the inherited policy (Q4),
	// else "narrow".
	PolicyMode PolicyMode `json:"policy_mode"`
	// ReplacedBy names the scope that issued a replace, when PolicyMode==replace.
	ReplacedBy AuthorityScope `json:"replaced_by,omitempty"`
	// Digest hashes bundle + contributing refs + effective policy version
	// (Decision 7).
	Digest string `json:"digest"`
}

// ResolvedLockInfo is the explain-surfaced shape of a binding lock.
type ResolvedLockInfo struct {
	Field string         `json:"field"`
	Kind  string         `json:"kind"`
	Owner AuthorityScope `json:"owner"`
	Deny  []string       `json:"deny,omitempty"`
	Value any            `json:"value,omitempty"`
}

// ProfileConflict is a same-scope value conflict (Decision 6): two fragments at
// the same authority disagree on a field; both refs are surfaced.
type ProfileConflict struct {
	Field  string         `json:"field"`
	Scope  AuthorityScope `json:"scope"`
	Winner string         `json:"winner"`
	Refs   []string       `json:"refs"`
}

// ProfileSet is the parsed input: all profiles and all layering policies.
type ProfileSet struct {
	Profiles []ConfigProfile
	Policies []LayeringPolicy
}

// ---------------------------------------------------------------------------
// Phase 1 — effective layering policy
// ---------------------------------------------------------------------------

// resolveEffectivePolicy merges every policy whose scope is in the context
// chain, processed low-authority first (Decision 3 narrowing; Q4 replace).
func resolveEffectivePolicy(set ProfileSet, ctx ProfileContext) EffectivePolicy {
	policies := make([]LayeringPolicy, 0, len(set.Policies))
	for _, p := range set.Policies {
		if ctx.inChain(p.Scope) {
			policies = append(policies, p)
		}
	}
	sort.SliceStable(policies, func(i, j int) bool {
		return AuthorityRankOf(policies[i].Scope) < AuthorityRankOf(policies[j].Scope)
	})

	var eff EffectivePolicy
	for _, p := range policies {
		foldPolicy(&eff, p)
	}
	return eff
}

// foldPolicy folds one policy (higher authority than any prior) into eff. In
// narrow mode precedence is taken from the most-authoritative policy that sets
// it and permissions intersect (monotone-narrowing, Decision 3); in replace mode
// precedence + permissions are wholly replaced (Q4). Locks always accumulate —
// they are absolute and a replace can never drop a lower lock.
func foldPolicy(eff *EffectivePolicy, p LayeringPolicy) {
	eff.Locks = append(eff.Locks, p.Locks...)
	if p.Mode == PolicyModeReplace {
		eff.Precedence = append([]AuthorityScope{}, p.Precedence...)
		eff.Permissions = p.OverridePermissions
		eff.Replaced = p.Scope
		return
	}
	if len(p.Precedence) > 0 {
		eff.Precedence = append([]AuthorityScope{}, p.Precedence...)
	}
	eff.Permissions = narrowPermissions(eff.Permissions, p.OverridePermissions)
}

// narrowPermissions intersects an accumulated permission cap with a higher
// scope's cap (Decision 3 monotone-narrowing). An OMITTED (nil) higher cap adds
// no constraint; a nil accumulator means "universe" so the higher cap governs
// directly. When both are present, the result keeps only scopes in BOTH and, per
// scope, only the field-paths both allow — so a higher scope can tighten but
// never broaden.
func narrowPermissions(acc, next *OverridePermissions) *OverridePermissions {
	if next == nil {
		return acc
	}
	if acc == nil {
		return next
	}
	out := map[AuthorityScope][]string{}
	for _, scope := range sortedScopes(acc.byScope) {
		if nextFields, ok := next.byScope[scope]; ok {
			out[scope] = intersectFields(acc.byScope[scope], nextFields)
		}
	}
	return &OverridePermissions{byScope: out}
}

// intersectFields intersects two field-path allowlists, honoring the "*"
// wildcard: "*" ∩ X = X, and X ∩ X keeps shared entries. Order is deterministic.
func intersectFields(a, b []string) []string {
	if hasWildcard(a) {
		return append([]string{}, b...)
	}
	if hasWildcard(b) {
		return append([]string{}, a...)
	}
	bset := map[string]bool{}
	for _, f := range b {
		bset[f] = true
	}
	var out []string
	for _, f := range a {
		if bset[f] {
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out
}

func hasWildcard(fields []string) bool {
	for _, f := range fields {
		if f == "*" {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Phase 2 — effective config, governed by the policy
// ---------------------------------------------------------------------------

// ResolveProfile runs both phases for a context. Input order of profiles and
// policies does NOT affect the output (H1): policies sort by authority and
// profiles sort by precedence then specificity then ref before merging.
func ResolveProfile(set ProfileSet, ctx ProfileContext) ResolvedProfile {
	policy := resolveEffectivePolicy(set, ctx)
	matched := matchingProfiles(set, ctx)
	orderProfiles(matched, policy)

	bundle := map[string]any{}
	refs := make([]string, 0, len(matched))
	conflicts := detectConflicts(matched, policy)
	for _, pr := range matched {
		mergeProfileInto(bundle, pr, policy, ctx)
		refs = append(refs, pr.Ref)
	}
	applyEffectiveLocks(bundle, policy, ctx)

	sort.Strings(refs)
	return ResolvedProfile{
		Bundle:       bundle,
		Contributing: refs,
		Locks:        bindingLocks(policy, ctx),
		Permissions:  permissionMap(policy.Permissions),
		Conflicts:    conflicts,
		PolicyMode:   effectiveMode(policy),
		ReplacedBy:   policy.Replaced,
		Digest:       digestProfile(bundle, refs, policy),
	}
}

func effectiveMode(policy EffectivePolicy) PolicyMode {
	if policy.Replaced != "" {
		return PolicyModeReplace
	}
	return PolicyModeNarrow
}

// matchingProfiles returns profiles whose scope is in the chain and whose
// selector matches the context (Decision 5).
func matchingProfiles(set ProfileSet, ctx ProfileContext) []ConfigProfile {
	out := make([]ConfigProfile, 0, len(set.Profiles))
	for _, p := range set.Profiles {
		if !ctx.inChain(p.Scope) {
			continue
		}
		if p.Selector.matches(ctx) {
			out = append(out, p)
		}
	}
	return out
}

// orderProfiles sorts matched profiles low→high precedence so later entries win
// scalar conflicts (local-wins tail). Ties break by selector specificity (a more
// specific fragment wins) then by absolute ref (Decision 6 determinism).
func orderProfiles(profiles []ConfigProfile, policy EffectivePolicy) {
	rank := precedenceRanker(policy)
	sort.SliceStable(profiles, func(i, j int) bool {
		ri, rj := rank(profiles[i].Scope), rank(profiles[j].Scope)
		if ri != rj {
			return ri < rj
		}
		si, sj := profiles[i].Selector.specificity(), profiles[j].Selector.specificity()
		if si != sj {
			return si < sj
		}
		return profiles[i].Ref < profiles[j].Ref
	})
}

// precedenceRanker returns a scope→rank function from the policy precedence,
// falling back to the §15 AUTHORITY-RANK when the policy is silent.
func precedenceRanker(policy EffectivePolicy) func(AuthorityScope) int {
	if len(policy.Precedence) == 0 {
		return AuthorityRankOf
	}
	idx := map[AuthorityScope]int{}
	for i, s := range policy.Precedence {
		idx[s] = i
	}
	return func(s AuthorityScope) int {
		if r, ok := idx[s]; ok {
			return r
		}
		return -1 // scopes absent from precedence sort first (lowest)
	}
}

// mergeProfileInto merges one profile's bundle into the accumulator, gated by
// override permissions and locks. The merge discipline is kind-specific: app_type
// and stage deep-map-merge (matching the legacy execution_profile/stage_profiles
// merge for zero behavioral diff); agent-capability unions additive sets and
// subtracts deny. A field the profile's scope may not change (Decision 2) is
// skipped; a value a lock forbids re-granting (Decision 4) is dropped.
func mergeProfileInto(acc map[string]any, pr ConfigProfile, policy EffectivePolicy, ctx ProfileContext) {
	if pr.Kind == ProfileKindAgentCapability {
		mergeCapabilityBundle(acc, pr, policy, ctx)
		return
	}
	mergeDeepBundle(acc, pr, policy)
}

// mergeDeepBundle deep-map-merges a fragment's bundle per top-level field,
// honoring the Decision-2 permission cap. It uses the §15 recursive object merge
// (mergeMaps) — NOT the top-level-key-categorized mergeField — because the legacy
// execution_profile / stage_profiles surfaces merge as ONE CategoryMapMerge that
// recurses through the whole nested object (relevance → stage → class; slug →
// profile). The bundle's own keys (relevance, topology, slug names) are not
// registered merge categories, so mergeField would scalar-REPLACE them and drop
// the lower layer's contribution; mergeMaps deep-merges objects and replaces only
// leaf arrays/scalars — byte-identical to the legacy merge (the zero-diff
// guarantee, §5).
func mergeDeepBundle(acc map[string]any, pr ConfigProfile, policy EffectivePolicy) {
	for _, key := range sortedAnyKeys(pr.Bundle) {
		if !policy.Permissions.mayChange(pr.Scope, key) {
			continue
		}
		acc[key] = mergeMaps(acc[key], pr.Bundle[key])
	}
}

// digestProfile produces the reproducibility digest over the canonical bundle,
// the sorted contributing refs, AND the effective policy version (Decision 7),
// so the digest changes whenever HOW the bundle was produced changes — even if
// the values coincide.
func digestProfile(bundle map[string]any, refs []string, policy EffectivePolicy) string {
	payload := struct {
		Bundle        map[string]any `json:"bundle"`
		Refs          []string       `json:"refs"`
		PolicyVersion string         `json:"policy_version"`
	}{Bundle: bundle, Refs: refs, PolicyVersion: policyVersion(policy)}
	raw, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])[:16]
}

// policyVersion is a stable hash of the effective policy (precedence + locks +
// permissions + replace marker) so the digest registers a policy/provenance
// change even when bundle values are unchanged (Decision 7).
func policyVersion(policy EffectivePolicy) string {
	raw, err := json.Marshal(struct {
		Precedence  []AuthorityScope            `json:"precedence"`
		Locks       []ResolvedLockInfo          `json:"locks"`
		Permissions map[AuthorityScope][]string `json:"permissions"`
		Replaced    AuthorityScope              `json:"replaced"`
	}{
		Precedence:  policy.Precedence,
		Locks:       lockInfos(policy.Locks),
		Permissions: permissionMap(policy.Permissions),
		Replaced:    policy.Replaced,
	})
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])[:16]
}

// permissionMap projects the effective permission cap into a plain, sorted-key
// map for explain + digest, or nil when the cap is omitted.
func permissionMap(perm *OverridePermissions) map[AuthorityScope][]string {
	if perm == nil {
		return nil
	}
	out := make(map[AuthorityScope][]string, len(perm.byScope))
	for scope, fields := range perm.byScope {
		cp := append([]string{}, fields...)
		sort.Strings(cp)
		out[scope] = cp
	}
	return out
}
