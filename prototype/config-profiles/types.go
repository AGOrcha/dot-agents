package main

// types.go — the data model for the config-profile prototype.
//
// A config "profile" is a SELECTOR-SCOPED config fragment. Resolution is a FLAT
// selector-merge cascade: there is deliberately NO `extends` inheritance between
// profiles (the anti-dependency-hell decision). The only graph edges that exist
// are profile -> leaf-unit (its own bundle/selector); there are zero
// profile -> profile edges. H1 asserts this structurally.

// Scope names the precedence layers of the config system. Higher authority
// scopes bind lower ones during policy resolution (Phase 1).
type Scope string

const (
	ScopeRepo    Scope = "repo"
	ScopeProject Scope = "project"
	ScopeUser    Scope = "user"
	ScopeTeam    Scope = "team"
	ScopeOrg     Scope = "org"
)

// Selector constrains the context in which a profile applies. An empty field is
// a wildcard (matches any context value).
type Selector struct {
	Role    string `json:"role,omitempty"`
	AppType string `json:"app_type,omitempty"`
	Stage   string `json:"stage,omitempty"`
	Harness string `json:"harness,omitempty"`
	Scope   Scope  `json:"scope,omitempty"`
}

// ToolSet is an additive allow set with a subtractive deny set.
type ToolSet struct {
	Allow []string `json:"allow,omitempty"`
	Deny  []string `json:"deny,omitempty"`
}

// SkillSet mirrors ToolSet but adds a preload list (additive, union-merged).
type SkillSet struct {
	Preload []string `json:"preload,omitempty"`
	Allow   []string `json:"allow,omitempty"`
	Deny    []string `json:"deny,omitempty"`
}

// Bundle is the config payload a profile contributes.
type Bundle struct {
	Tools  ToolSet  `json:"tools"`
	Skills SkillSet `json:"skills"`
	Hooks  []string `json:"hooks,omitempty"`
	MCP    []string `json:"mcp,omitempty"`
	Model  string   `json:"model,omitempty"`
}

// Profile is unit kind #1: a selector-scoped config fragment.
//
// SourceScope is the scope a profile is *contributed from* — the authority it
// carries for precedence and override_permissions. This is distinct from the
// selector (which constrains the context the profile applies to). The brief's
// model leaves this implicit: role/app_type/stage profiles have no selector
// scope. The prototype resolves the ambiguity by treating SourceScope as an
// explicit field; when unset it defaults to the selector's Scope, and when
// that is also unset it defaults to repo (the shared committed baseline).
// See the report's "judgment calls" section.
type Profile struct {
	Ref         string   `json:"ref"`
	Selector    Selector `json:"selector"`
	SourceScope Scope    `json:"source_scope,omitempty"`
	Bundle      Bundle   `json:"bundle"`
}

// scope returns the profile's effective source scope (see SourceScope doc).
func (p Profile) scope() Scope {
	if p.SourceScope != "" {
		return p.SourceScope
	}
	if p.Selector.Scope != "" {
		return p.Selector.Scope
	}
	return ScopeRepo
}

// LayeringPolicy is unit kind #2: attachable at any scope, it governs how
// profiles merge. Higher-authority scopes' policies bind lower scopes.
type LayeringPolicy struct {
	Scope Scope `json:"scope"`
	// Precedence lists scopes low-to-high authority for VALUE resolution:
	// later entries win on conflicting scalar fields ("local-wins" tail).
	Precedence []Scope `json:"precedence"`
	// Locks are absolute constraints expressed as "<field>:{vals}@<sel>".
	// e.g. "tools.deny:{Edit,Write}@role:reviewer" forces Edit/Write into the
	// effective deny set for any context whose role is reviewer, and forbids
	// any lower-scope profile from re-granting them.
	Locks []string `json:"locks,omitempty"`
	// OverridePermissions gates which bundle fields a scope may change. Keyed by
	// scope name; the value is the list of permitted field paths
	// (e.g. "tools.allow", "mcp", "model"). A scope absent from the map (when
	// the map is non-empty) may change nothing.
	OverridePermissions map[Scope][]string `json:"override_permissions,omitempty"`
}

// SourceSet is the parsed fixture set: all profiles and all policies.
type SourceSet struct {
	Profiles []Profile        `json:"profiles"`
	Policies []LayeringPolicy `json:"policies"`
}

// Context is the dispatch context resolution is performed against.
type Context struct {
	Role       string
	AppType    string
	Stage      string
	Harness    string
	ScopeChain []Scope // ordered low-authority -> high-authority
}

// Resolved is the output of the two-phase resolver.
type Resolved struct {
	Bundle       Bundle   `json:"bundle"`
	Contributing []string `json:"contributing_refs"`
	Digest       string   `json:"digest"`
	// EffectivePolicy is the merged Phase-1 policy (exposed for explain/tests).
	EffectivePolicy ResolvedPolicy `json:"-"`
}

// ResolvedPolicy is the Phase-1 merged effective layering policy.
type ResolvedPolicy struct {
	Precedence          []Scope
	Locks               []Lock
	OverridePermissions map[Scope][]string
	// lockAuthority records which scope owns each lock, so a higher scope's
	// lock is reported as the binding one.
	lockAuthority map[string]Scope
}

// Lock is a parsed lock directive.
type Lock struct {
	Raw      string
	Field    string   // e.g. "tools.deny"
	Values   []string // e.g. ["Edit","Write"]
	Selector Selector // the "@k:v" tail; empty => applies to all contexts
	Owner    Scope    // scope that declared the lock
}
