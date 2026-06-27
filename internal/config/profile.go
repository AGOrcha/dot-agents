package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

// profile.go defines the kind:profile unit model for the unified-config-profiles
// spec (L1). A profile is a SELECTOR-SCOPED config fragment resolved by the
// shared two-phase selector-merge engine (profile_resolver.go) on top of the §15
// authority substrate (authority.go). The three profile KINDS (app_type, stage,
// agent-capability) are nameable, inspectable categories that share ONE engine;
// they differ only in (a) which selector keys are meaningful and (b) how their
// bundle fields merge (§2.2). There are deliberately ZERO profile→profile edges
// (no extends/inherits — the anti-dependency-hell decision, §2.1): a profile
// references only its own leaf bundle, and composition happens in the resolver,
// never by a profile pulling in another.

// ProfileKind labels a profile category for inspectability (§2.2). The kind
// governs which selector keys are meaningful and which bundle-merge discipline
// applies; it does NOT introduce a second resolution path.
type ProfileKind string

const (
	// ProfileKindAppType re-expresses today's execution_profile.by_app_type
	// entries: selector key app_type, bundle = the AppTypeProfile facets, merged
	// by deep map-merge (matching the legacy execution_profile merge for zero
	// behavioral diff, §5).
	ProfileKindAppType ProfileKind = "app_type"
	// ProfileKindStage re-expresses today's stage_profiles entries: selector keys
	// stage (and role where relevant), bundle = the StageProfile, deep map-merge.
	ProfileKindStage ProfileKind = "stage"
	// ProfileKindAgentCapability is the new kind for runtime-role capability
	// bundles (tools/skills/hooks/mcp): additive sets union, deny subtracts.
	ProfileKindAgentCapability ProfileKind = "agent-capability"
)

// UnitKindProfile is the §15 unit kind for a config profile (R2): a profile is a
// first-class unit identified by its absolute ref <source>:<name>, resolved and
// locked on the §15 substrate. It is recognized alongside layer/artifact/
// project-set so the resolver and lock model fail-closed on an unknown kind
// rather than mis-resolving a profile.
const UnitKindProfile = "profile"

// selectorKeys is the closed set of meaningful selector keys (Decision 5,
// exact-match v1). A selector carrying any other key is a validation error — a
// typo'd key must fail loudly, never silently match everything or nothing (R9).
var selectorKeys = map[string]bool{
	"role": true, "app_type": true, "stage": true, "harness": true,
}

// ProfileSelector constrains the dispatch context a fragment applies to. Each
// present key is matched EXACTLY; an absent (empty) key is a wildcard
// (Decision 5). The zero value matches every context.
type ProfileSelector struct {
	Role    string `json:"role,omitempty"`
	AppType string `json:"app_type,omitempty"`
	Stage   string `json:"stage,omitempty"`
	Harness string `json:"harness,omitempty"`
}

// decodeSelector decodes a selector from raw JSON fail-closed: an unknown key is
// rejected (Decision 5 / R9) rather than silently dropped. A nil/absent block
// yields the wildcard (zero) selector.
func decodeSelector(raw json.RawMessage) (ProfileSelector, error) {
	if len(raw) == 0 {
		return ProfileSelector{}, nil
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return ProfileSelector{}, fmt.Errorf("malformed selector: %w", err)
	}
	for k := range probe {
		if !selectorKeys[k] {
			return ProfileSelector{}, fmt.Errorf("unknown selector key %q (valid: app_type, harness, role, stage)", k)
		}
	}
	var sel ProfileSelector
	if err := json.Unmarshal(raw, &sel); err != nil {
		return ProfileSelector{}, fmt.Errorf("malformed selector: %w", err)
	}
	return sel, nil
}

// matches reports whether the selector applies to ctx: every present key matches
// exactly, an absent key is a wildcard (Decision 5).
func (s ProfileSelector) matches(ctx ProfileContext) bool {
	if s.Role != "" && s.Role != ctx.Role {
		return false
	}
	if s.AppType != "" && s.AppType != ctx.AppType {
		return false
	}
	if s.Stage != "" && s.Stage != ctx.Stage {
		return false
	}
	if s.Harness != "" && s.Harness != ctx.Harness {
		return false
	}
	return true
}

// specificity is the count of constrained (non-wildcard) selector keys. It is
// the deterministic, content-derived component of the same-scope tie-break
// (Decision 6): a more specific fragment is applied later (wins scalars), and
// the absolute ref breaks any remaining tie.
func (s ProfileSelector) specificity() int {
	n := 0
	for _, v := range []string{s.Role, s.AppType, s.Stage, s.Harness} {
		if v != "" {
			n++
		}
	}
	return n
}

// ConfigProfile is one kind:profile unit — a selector-scoped config fragment.
// Its identity is the absolute ref <source>:<name> (§15 D2). Its authority for
// precedence and permission-gating is SOURCE-derived (Decision 1): Scope is set
// from ref.source → source-registry → scope by the loader, NEVER self-declared
// by the unit. The selector governs WHERE the fragment applies; the scope
// governs HOW MUCH it is trusted — the two are independent (Decision 1).
type ConfigProfile struct {
	// Ref is the absolute unit ref <source>:<name> — the identity and the
	// deterministic same-scope tie-break key (Decision 6).
	Ref string
	// Kind selects the bundle-merge discipline (deep-map vs capability-union).
	Kind ProfileKind
	// Scope is the SOURCE-derived authority scope (Decision 1). It is not
	// authored on the unit; the loader stamps it from the source registry.
	Scope AuthorityScope
	// Selector constrains the context this fragment applies to (Decision 5).
	Selector ProfileSelector
	// Bundle is the config payload the profile contributes — a kind-agnostic
	// object so one engine carries app_type facets, stage composition, and
	// capability sets without a per-kind resolver.
	Bundle map[string]any
}

// PolicyMode is the Q4 named replace-mode marker (Decision 3). The default is
// narrow (merge that may only tighten); replace supersedes the inherited
// precedence + permissions as a visible, named act.
type PolicyMode string

const (
	// PolicyModeNarrow (the default, also the zero value when authored absent) is
	// monotone-narrowing: a higher scope may tighten precedence/permissions and
	// add locks, never broaden.
	PolicyModeNarrow PolicyMode = "narrow"
	// PolicyModeReplace declares "this policy supersedes, not merges": the
	// replacing scope's precedence + override_permissions wholly replace the
	// inherited ones. Locks still accumulate (they are absolute invariants and a
	// replace can never drop a lower scope's lock — that would broaden).
	PolicyModeReplace PolicyMode = "replace"
)

// ProfileLock is a layering-policy lock in the profile engine. It reuses the §15
// lock vocabulary — deny-lock and value-lock ONLY, no force-allow (Decision 4) —
// extended with an optional selector tail so a lock can be context-scoped
// (e.g. deny Edit/Write only @role:reviewer). A lock is absolute: permission
// never beats it (Decision 4).
type ProfileLock struct {
	// Field is the dot-path the lock governs (e.g. "tools.allow", "model").
	Field string `json:"field"`
	// Deny lists set members forced OUT of Field and forbidden from lower-scope
	// re-grant (the deny-lock). Mutually exclusive with Value.
	Deny []string `json:"deny,omitempty"`
	// Value pins Field to a fixed scalar (the value-lock). A lower scope writing
	// a different value is rejected.
	Value json.RawMessage `json:"value,omitempty"`
	// Selector optionally scopes the lock to a context (empty = all contexts).
	Selector ProfileSelector `json:"selector,omitempty"`
	// Owner is the authority scope that declared the lock (set by the resolver,
	// not authored). Higher owner binds lower scopes.
	Owner AuthorityScope `json:"-"`
}

// kind reports whether the lock is a value-lock or a deny-lock for collision
// reporting.
func (l ProfileLock) kind() string {
	if len(l.Value) > 0 {
		return collisionValueLock
	}
	return collisionDenyLock
}

// OverridePermissions is the Decision-2 three-state permission map. The pointer
// itself distinguishes the three intents:
//
//   - a nil *OverridePermissions ⇒ OMITTED (no opinion; inherit-higher).
//   - a non-nil value with an empty byScope map ⇒ LOCKDOWN ({}): no scope may
//     change anything.
//   - a non-nil value with a non-empty map ⇒ ALLOWLIST: a present scope may
//     change exactly its listed field-paths ("*" = all); a scope absent from the
//     map may change nothing.
//
// The presence check (absent key vs present-empty-map) is preserved through a
// custom JSON round-trip so the schema and resolver both honor it.
type OverridePermissions struct {
	byScope map[AuthorityScope][]string
}

// NewOverridePermissions builds an allowlist/lockdown permission map. A nil or
// empty map is the lockdown state ({}); a non-empty map is an allowlist.
func NewOverridePermissions(m map[AuthorityScope][]string) *OverridePermissions {
	if m == nil {
		m = map[AuthorityScope][]string{}
	}
	return &OverridePermissions{byScope: m}
}

// mayChange reports whether scope may write field under this permission state.
// A nil receiver is the OMITTED state ⇒ no restriction (allow). A non-nil map
// is an allowlist: a scope absent from the map may change nothing; "*" grants
// every field.
func (o *OverridePermissions) mayChange(scope AuthorityScope, field string) bool {
	if o == nil {
		return true
	}
	fields, ok := o.byScope[scope]
	if !ok {
		return false
	}
	for _, f := range fields {
		if f == "*" || f == field {
			return true
		}
	}
	return false
}

// MarshalJSON emits the bare scope→fields map (or {} for lockdown), so a
// round-trip preserves the present-but-empty (lockdown) state distinctly from an
// omitted field (the omitempty on the pointer handles omitted).
func (o OverridePermissions) MarshalJSON() ([]byte, error) {
	if o.byScope == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(o.byScope)
}

// UnmarshalJSON decodes the scope→fields map. A present `{}` decodes to a
// non-nil empty map (lockdown); the omitted case never reaches here because the
// field is a pointer.
func (o *OverridePermissions) UnmarshalJSON(data []byte) error {
	m := map[AuthorityScope][]string{}
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	o.byScope = m
	return nil
}

// LayeringPolicy is a scope-attached policy unit (§2.3 Phase 1): it governs how
// profile fragments merge across scopes. A higher-authority scope's policy binds
// lower ones — its precedence wins, its locks are absolute, and its
// override_permissions cap what lower scopes may set. Scope is SOURCE-derived
// (set by the loader from the owning layer/source), never authored on the unit.
type LayeringPolicy struct {
	// Scope is the authority scope that owns this policy (set by the loader).
	Scope AuthorityScope `json:"-"`
	// Precedence orders scopes low→high for Phase-2 value resolution (later wins
	// scalar conflicts). Empty ⇒ fall back to the canonical AUTHORITY-RANK order.
	Precedence []AuthorityScope `json:"precedence,omitempty"`
	// Locks are the absolute constraints this scope emits (Decision 4).
	Locks []ProfileLock `json:"locks,omitempty"`
	// OverridePermissions is the Decision-2 three-state cap. A nil pointer is the
	// OMITTED state; presence is preserved (see OverridePermissions).
	OverridePermissions *OverridePermissions `json:"override_permissions,omitempty"`
	// Mode is the Q4 replace-mode marker (Decision 3). Empty ⇒ narrow.
	Mode PolicyMode `json:"mode,omitempty"`
}

// layeringPolicyWire is the strict decode shape for a LayeringPolicy: it rejects
// unknown keys and, critically, surfaces force_allow as a validation error
// (Decision 4 / R9) rather than silently dropping it.
type layeringPolicyWire struct {
	Precedence          []AuthorityScope     `json:"precedence,omitempty"`
	Locks               []json.RawMessage    `json:"locks,omitempty"`
	OverridePermissions *OverridePermissions `json:"override_permissions,omitempty"`
	Mode                PolicyMode           `json:"mode,omitempty"`
}

// lockWire strict-decodes one ProfileLock, rejecting a force_allow key fail-closed
// (Decision 4): there is no force-allow lock, so its presence is an authoring
// error, not a no-op.
type lockWire struct {
	Field    string          `json:"field"`
	Deny     []string        `json:"deny,omitempty"`
	Value    json.RawMessage `json:"value,omitempty"`
	Selector json.RawMessage `json:"selector,omitempty"`
}

// decodeLayeringPolicy decodes and validates a scope's authored layering policy
// fail-closed. Unknown keys, a force_allow lock, an unknown selector key, or a
// malformed mode are all errors (R9). scope stamps the source-derived authority.
func decodeLayeringPolicy(raw json.RawMessage, scope AuthorityScope) (LayeringPolicy, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var wire layeringPolicyWire
	if err := dec.Decode(&wire); err != nil {
		return LayeringPolicy{}, fmt.Errorf("malformed layering_policy: %w", err)
	}
	if err := validatePolicyMode(wire.Mode); err != nil {
		return LayeringPolicy{}, err
	}
	locks, err := decodeLocks(wire.Locks, scope)
	if err != nil {
		return LayeringPolicy{}, err
	}
	return LayeringPolicy{
		Scope:               scope,
		Precedence:          wire.Precedence,
		Locks:               locks,
		OverridePermissions: wire.OverridePermissions,
		Mode:                wire.Mode,
	}, nil
}

// validatePolicyMode rejects any mode other than the two named values.
func validatePolicyMode(m PolicyMode) error {
	switch m {
	case "", PolicyModeNarrow, PolicyModeReplace:
		return nil
	default:
		return fmt.Errorf("unknown layering_policy mode %q (valid: narrow, replace)", m)
	}
}

// decodeLocks strict-decodes every authored lock, rejecting force_allow and an
// unknown selector key, and stamping the owning scope on each.
func decodeLocks(raws []json.RawMessage, scope AuthorityScope) ([]ProfileLock, error) {
	out := make([]ProfileLock, 0, len(raws))
	for i, raw := range raws {
		lock, err := decodeOneLock(raw, scope)
		if err != nil {
			return nil, fmt.Errorf("lock[%d]: %w", i, err)
		}
		out = append(out, lock)
	}
	return out, nil
}

func decodeOneLock(raw json.RawMessage, scope AuthorityScope) (ProfileLock, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var wire lockWire
	if err := dec.Decode(&wire); err != nil {
		return ProfileLock{}, fmt.Errorf("malformed/unknown lock field (force_allow is invalid — no force-allow): %w", err)
	}
	if wire.Field == "" {
		return ProfileLock{}, fmt.Errorf("lock missing field")
	}
	if len(wire.Deny) > 0 && len(wire.Value) > 0 {
		return ProfileLock{}, fmt.Errorf("lock %q sets both deny and value (mutually exclusive)", wire.Field)
	}
	sel, err := decodeSelector(wire.Selector)
	if err != nil {
		return ProfileLock{}, err
	}
	return ProfileLock{
		Field: wire.Field, Deny: wire.Deny, Value: wire.Value,
		Selector: sel, Owner: scope,
	}, nil
}

// sortedScopes returns the keys of an authority-scope map in deterministic order.
func sortedScopes[V any](m map[AuthorityScope]V) []AuthorityScope {
	out := make([]AuthorityScope, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
