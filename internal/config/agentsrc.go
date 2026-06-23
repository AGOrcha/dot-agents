package config

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/AGOrcha/dot-agents/internal/events"
	"github.com/AGOrcha/dot-agents/internal/gitremote"
)

// gitRemoteOriginURL is the seam that returns the origin URL for repoPath.
// Defaults to gitremote.ReadOriginURL, which reads the on-disk git config
// in-process via go-git/v6 — no subprocess, no PATH lookup, no porcelain
// text parsing. Tests override the seam to inject SSH/HTTPS/edge-case URLs
// without standing up a real .git directory.
//
// The seam returns a wrapped error when the directory is not a git repo
// and gitremote.ErrNoOrigin when the repo exists but has no `origin`
// remote — DeriveRepoIDFromGit collapses both branches to "" so callers
// leave repo_id blank rather than fabricate one (spec §5.3 fallback).
var gitRemoteOriginURL = gitremote.ReadOriginURL

// DeriveRepoIDFromGit returns the canonical repo_id for the project at
// repoPath, derived from its `origin` git remote. The canonical form is
// `<host>/<path>` with the `.git` suffix stripped and the host lowercased,
// e.g. `github.com/acme/po-core-api-se` (per org-config-resolution §5.2).
//
// Accepted remote forms:
//   - SSH:    git@github.com:acme/repo.git              → github.com/acme/repo
//   - SCP-style with user: ssh://git@github.com/acme/repo.git → github.com/acme/repo
//   - HTTPS:  https://github.com/acme/repo.git          → github.com/acme/repo
//   - HTTP:   http://gitlab.acme.internal/g/r           → gitlab.acme.internal/g/r
//   - git://: git://github.com/acme/repo.git            → github.com/acme/repo
//
// Returns "" (no error) when:
//   - the directory is not a git checkout
//   - the repo has no `origin` remote (e.g. `git init` only)
//   - the remote URL cannot be parsed into a host+path pair
//
// Per spec §5.3 git derivation is a FALLBACK — callers must not overwrite
// an explicit repo_id set in the manifest. See MergeGenerateAgentsRC.
func DeriveRepoIDFromGit(repoPath string) string {
	raw, err := gitRemoteOriginURL(repoPath)
	if err != nil || raw == "" {
		return ""
	}
	return gitremote.CanonicalRepoID(raw)
}

// isDirEntry reports whether the path is a directory, following symlinks.
func isDirEntry(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// StringsOrBool holds either a boolean flag (all/none) or a named list.
// It marshals/unmarshals as either a JSON bool or a JSON string array:
//
//	true             → All resources of this type
//	false            → No resources
//	["name1","name2"] → Only the named resources
type StringsOrBool struct {
	All   bool
	Names []string
}

// IsEnabled returns true if any resources are enabled (All or at least one name).
func (s StringsOrBool) IsEnabled() bool {
	return s.All || len(s.Names) > 0
}

// Contains returns true if name is covered (either All=true or name is in Names).
func (s StringsOrBool) Contains(name string) bool {
	if s.All {
		return true
	}
	for _, n := range s.Names {
		if n == name {
			return true
		}
	}
	return false
}

// Add appends name to Names unless All is true (already covers everything).
func (s *StringsOrBool) Add(name string) {
	if s.All {
		return
	}
	for _, n := range s.Names {
		if n == name {
			return // already present
		}
	}
	s.Names = append(s.Names, name)
}

// Remove removes name from Names. No-op if All is true.
func (s *StringsOrBool) Remove(name string) {
	if s.All {
		return
	}
	out := s.Names[:0]
	for _, n := range s.Names {
		if n != name {
			out = append(out, n)
		}
	}
	s.Names = out
}

func (s StringsOrBool) MarshalJSON() ([]byte, error) {
	if len(s.Names) > 0 {
		return json.Marshal(s.Names)
	}
	return json.Marshal(s.All)
}

func (s *StringsOrBool) UnmarshalJSON(data []byte) error {
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		s.All = b
		s.Names = nil
		return nil
	}
	var names []string
	if err := json.Unmarshal(data, &names); err != nil {
		return fmt.Errorf("hooks/mcp field must be bool or string array: %w", err)
	}
	s.All = false
	s.Names = names
	return nil
}

// AgentsRCKGBridge is the bridge sub-config within the KG section.
type AgentsRCKGBridge struct {
	Enabled        bool     `json:"enabled"`
	AllowedIntents []string `json:"allowed_intents,omitempty"`
}

// AgentsRCKG is the knowledge-graph configuration block in agentsrc.json.
type AgentsRCKG struct {
	// GraphHome overrides KG_HOME env var for this project. Defaults to ~/.knowledge-graph.
	GraphHome string `json:"graph_home,omitempty"`
	// Backend selects the storage backend: "sqlite" (default) or "postgres".
	// Postgres requires KG_POSTGRES_URL.
	Backend string `json:"backend,omitempty"`
	// Bridge configures workflow/kg bridge query behaviour for this project.
	Bridge AgentsRCKGBridge `json:"bridge"`
}

// AgentsRC represents the .agentsrc.json manifest committed to a project repo.
//
// Schema versions:
//   - version=1 (legacy): only the original field surface (project, sources, …) is
//     meaningful. The v2 additive fields below remain absent/empty on a v1 file.
//   - version=2: the v2 additive fields (RepoID, Extends, Packages, Features and the
//     extended Source fields ID/CacheTTL/Auth, plus the http+oci source types)
//     become first-class. All v2 fields use `omitempty` so a v1 manifest round-trips
//     byte-for-byte when these fields are absent.
//
// See specs config-distribution-model §3-§5 + org-config-resolution §15.2.
type AgentsRC struct {
	Schema   string           `json:"$schema,omitempty"`
	Version  int              `json:"version"`
	Project  string           `json:"project,omitempty"`
	Skills   []string         `json:"skills,omitempty"`
	Rules    []string         `json:"rules,omitempty"`
	Agents   []string         `json:"agents,omitempty"`
	Hooks    StringsOrBool    `json:"hooks"`
	MCP      StringsOrBool    `json:"mcp"`
	Settings bool             `json:"settings"`
	Sources  []Source         `json:"sources"`
	KG       *AgentsRCKG      `json:"kg,omitempty"`
	Refresh  *RefreshMetadata `json:"refresh,omitempty"`

	// --- v2 additive fields (config-distribution-model §3) ---

	// RepoID is the canonical repository identity (e.g. "github.com/acme/manager-ui").
	// Protected: imported layers cannot override it. See org-config-resolution §5.
	RepoID string `json:"repo_id,omitempty"`
	// Extends references config layers in the form "source-id:layer-path[@version]".
	// Each entry may be a plain string or an object form `{"ref": "...", "optional": true}`.
	// Tier constraint (enforced at schema validation): extends entries must reference
	// git|http|local sources — see config-distribution-model §4.
	Extends []LayerRef `json:"extends,omitempty"`
	// Packages references executable artifact bundles in the form
	// `source-id:artifact-path@version-spec`. Any source kind (git|local|http|oci)
	// may supply an artifact (config-distribution-model §15 D8).
	Packages []PackageRef `json:"packages,omitempty"`
	// Features overrides feature-flag defaults (config-distribution-model §3.6).
	Features map[string]string `json:"features,omitempty"`
	// ExecutionProfile is the config-v2 §15-shaped, scope-mergeable layer that
	// routes a task's workflow execution shape (unit relevance + topology +
	// lenses) by app_type. It is a kind=layer policy fragment — see
	// internal/config/execution_profile.go and the
	// skill-relevance-filter design.
	ExecutionProfile *ExecutionProfile `json:"execution_profile,omitempty"`
	// PRSource is the config-driven pull-request event producer config
	// (pr-event-source design D4). It configures the generic internal/events
	// producer engine — a fetch+field-map per platform — so PR state becomes
	// event.pr.* events with no per-platform Go. `gh` is the zero-config default;
	// an org layer can override the default via the config-v2 scope layers.
	PRSource *AgentsRCPRSource `json:"pr_source,omitempty"`

	// StageProfiles are named per-stage prompt-composition profiles, keyed by
	// stage (executor | verifier | reviewer | orchestrator) then by slug. Each
	// profile's prompt_files is source-aware (config-v2 Q1 ruling, Option B): an
	// entry is a typed {source, path, version} object, with the bare-string legacy
	// form still accepted on read (see PromptFileRef). The verifier and reviewer
	// stages supersede the deprecated verifier_profiles / reviewer_profiles maps,
	// and topology.verifier_sequence supersedes app_type_verifier_map; all three
	// legacy keys are still read and folded in here for back-compat (see
	// UnmarshalJSON / foldLegacyProfiles) and are never re-emitted.
	StageProfiles map[string]map[string]StageProfile `json:"stage_profiles,omitempty"`

	// PreconditionPolicies is the top-level named registry of verifier
	// precondition policies (verifier-precondition-policy plan, Slice B). Each
	// entry is a named policy = an ordered list of predicates over the unified
	// event/signal contract. A stage profile references a policy by name via
	// StageProfile.PreconditionPolicy; the verifier reads the resolved policy
	// from the lockfile. Absent ⇒ the built-in `default` gate applies.
	PreconditionPolicies map[string]PreconditionPolicySpec `json:"precondition_policies,omitempty"`

	// ExtraFields captures unknown JSON keys so Save() can round-trip them
	// instead of silently dropping legacy or custom fields.
	ExtraFields map[string]json.RawMessage `json:"-"`
}

// PredicateSpec is the config-facing mirror of the commands/workflow Predicate:
// one predicate over a single registered event/signal kind. Signal is the
// registered kind (e.g. "event.pr.open", "gate.quality.sonar"); Args are
// kind-specific (e.g. {"equals":"green"}).
type PredicateSpec struct {
	Signal string            `json:"signal"`
	Args   map[string]string `json:"args,omitempty"`
}

// PreconditionPolicySpec is one named entry in the precondition_policies
// registry: an ordered list of predicates all of which must hold for the
// in_progress → awaiting_agent_review gate to open.
type PreconditionPolicySpec struct {
	Predicates []PredicateSpec `json:"predicates"`
}

// AgentsRCPRSource is the .agentsrc.json `pr_source` block (pr-event-source D4).
// It is the config-facing mirror of the internal/events engine's PRSourceConfig:
// it owns JSON (de)serialization and config-v2 scope merging, while ToEngineConfig
// bridges it to the engine that actually drives the producer. A platform is added
// by writing this block — no Go change.
type AgentsRCPRSource struct {
	// Producer selects the named producer: "gh" (default), "exec", "http", or a
	// registered code producer.
	Producer string `json:"producer,omitempty"`
	// List fetches the open PR list and maps it onto canonical PR fields.
	List *AgentsRCPRFetch `json:"list,omitempty"`
	// Comments fetches one PR's comments. Optional.
	Comments *AgentsRCPRFetch `json:"comments,omitempty"`
	// PollIntervalS is the producer poll cadence in seconds.
	PollIntervalS int `json:"poll_interval_s,omitempty"`
}

// AgentsRCPRFetch is one named fetch block (the "list" or "comments" block) of a
// pr_source config: how to fetch (exec argv or http url) and how to project each
// item onto canonical fields (each + map).
type AgentsRCPRFetch struct {
	Argv    []string          `json:"argv,omitempty"`
	URL     string            `json:"url,omitempty"`
	Method  string            `json:"method,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Each    string            `json:"each,omitempty"`
	Map     map[string]string `json:"map,omitempty"`
}

// ToEngineConfig bridges the config-facing pr_source block to the internal/events
// engine type that actually drives the producer (pr-event-source D4). Without this
// converter the configured pr_source would never reach the engine — the gh/exec/
// http source the user configured would be inert. Every field is carried across so
// the round-trip config -> engine -> producer preserves the configured fetch and
// field map exactly (no round-trip loss).
//
// A nil receiver returns the built-in gh default, so an absent pr_source still
// yields a working producer config rather than an empty (inert) one.
func (s *AgentsRCPRSource) ToEngineConfig() events.PRSourceConfig {
	if s == nil {
		return events.DefaultGHPRSource()
	}
	return events.PRSourceConfig{
		Producer:      s.Producer,
		List:          s.List.toEngineFetch(),
		Comments:      s.Comments.toEngineFetch(),
		PollIntervalS: s.PollIntervalS,
	}
}

// toEngineFetch converts one config fetch block to the engine FetchBlock,
// preserving every field. A nil block (the "comments" block is optional) yields
// the zero FetchBlock so the engine sees an empty, clearly-absent block rather
// than a panic.
func (b *AgentsRCPRFetch) toEngineFetch() events.FetchBlock {
	if b == nil {
		return events.FetchBlock{}
	}
	return events.FetchBlock{
		Argv:    append([]string(nil), b.Argv...),
		URL:     b.URL,
		Method:  b.Method,
		Headers: cloneStringStringMap(b.Headers),
		Each:    b.Each,
		Map:     cloneStringStringMap(b.Map),
	}
}

// cloneStringStringMap returns a shallow copy of m, or nil when m is empty, so
// the engine config does not alias the caller's config maps.
func cloneStringStringMap(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// EffectivePRSource resolves the pr_source config that drives the producer: the
// configured block when present, otherwise the built-in gh default. This is the
// single resolution point so a missing pr_source field falls back to a working
// gh source rather than an empty (inert) one (pr-event-source D4).
func (a *AgentsRC) EffectivePRSource() events.PRSourceConfig {
	if a == nil || a.PRSource == nil {
		return events.DefaultGHPRSource()
	}
	return a.PRSource.ToEngineConfig()
}

// NewPRListProducer builds the live PR list producer from this config's resolved
// pr_source (pr-event-source D4/R3). It is the production entry point consumers
// (the layered-pr-fanout poll-detector, the pr-ci verifier) call to turn PR state
// into event.pr.* events: it registers the control-plane PR kinds on reg and
// returns a producer that derives each PR's Rollup.State on every cycle. This is
// the reachable path that exercises the config -> engine bridge (ToEngineConfig)
// and the DeriveRollupState rule — without it both would be dead code.
//
// typ is the event.pr.* kind to emit (e.g. events.KindPROpened); a nil fetcher
// uses the real exec/http fetcher.
func (a *AgentsRC) NewPRListProducer(reg *events.Registry, typ, source string, fetcher events.Fetcher) (*events.PRProducer, error) {
	if reg == nil {
		return nil, fmt.Errorf("config: NewPRListProducer: nil registry")
	}
	if err := events.RegisterPRKinds(reg); err != nil {
		return nil, fmt.Errorf("config: NewPRListProducer: register kinds: %w", err)
	}
	return a.EffectivePRSource().NewListProducer(typ, source, fetcher)
}

// LayerRef is a single entry in AgentsRC.Extends. It accepts either a bare
// reference string ("acme:org/base") or an object form with an optional flag:
//
//	{"ref": "acme:team/experimental", "optional": true}
//
// Per config-distribution-model §11.
type LayerRef struct {
	// Ref is the layer reference string "source-id:layer-path[@version]".
	Ref string `json:"ref"`
	// Optional marks the layer as non-fatal on fetch failure.
	Optional bool `json:"optional,omitempty"`
}

// MarshalJSON emits the compact string form when Optional is false, otherwise
// emits the object form. Round-trip is stable under repeated marshal/unmarshal.
func (l LayerRef) MarshalJSON() ([]byte, error) {
	if !l.Optional {
		return json.Marshal(l.Ref)
	}
	type wire struct {
		Ref      string `json:"ref"`
		Optional bool   `json:"optional,omitempty"`
	}
	return json.Marshal(wire{Ref: l.Ref, Optional: l.Optional})
}

// UnmarshalJSON accepts either a plain string or the object form.
func (l *LayerRef) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		l.Ref = s
		l.Optional = false
		return nil
	}
	type wire struct {
		Ref      string `json:"ref"`
		Optional bool   `json:"optional,omitempty"`
	}
	var w wire
	if err := json.Unmarshal(data, &w); err != nil {
		return fmt.Errorf("extends entry must be string or {ref,optional?}: %w", err)
	}
	if w.Ref == "" {
		return fmt.Errorf("extends entry object form requires non-empty ref")
	}
	l.Ref = w.Ref
	l.Optional = w.Optional
	return nil
}

// PackageRef is a single entry in AgentsRC.Packages. The string form is the
// canonical wire form per config-distribution-model §5; the object form is
// accepted for forward compatibility with future per-entry options.
type PackageRef struct {
	// Ref is the package reference string "source-id:artifact-path@version-spec".
	Ref string `json:"ref"`
}

func (p PackageRef) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.Ref)
}

func (p *PackageRef) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		p.Ref = s
		return nil
	}
	type wire struct {
		Ref string `json:"ref"`
	}
	var w wire
	if err := json.Unmarshal(data, &w); err != nil {
		return fmt.Errorf("packages entry must be string or {ref}: %w", err)
	}
	if w.Ref == "" {
		return fmt.Errorf("packages entry object form requires non-empty ref")
	}
	p.Ref = w.Ref
	return nil
}

// PromptFileRef is a single entry in a verifier/reviewer profile's prompt_files
// list. Per the config-v2 Q1 ruling (Option B — force typed objects everywhere),
// an entry resolves to a typed object {source, path, version}. The bare-string
// legacy form ("verifiers/unit.md") is still accepted on read and is equivalent
// to {path: "<string>"} with an empty source/version; this is the same dual-form
// pattern as LayerRef/PackageRef so legacy .agentsrc.json files round-trip
// byte-for-byte while new source-aware entries gain the object form.
//
// Source names the config source the prompt file is fetched from (e.g. a
// source `id` declared under `sources`); an empty source means the prompt is
// resolved relative to the local repo/home search path (the historical
// behavior). Version pins a source-relative revision; empty means "as resolved".
type PromptFileRef struct {
	// Source is the source id the prompt file is fetched from. Empty means
	// local resolution across the repo/home search path.
	Source string `json:"source,omitempty"`
	// Path is the prompt file path, relative to the source (or to the local
	// prompt search path when Source is empty). Required.
	Path string `json:"path"`
	// Version pins a source-relative revision. Empty means "as resolved".
	Version string `json:"version,omitempty"`
}

// MarshalJSON emits the compact string form when only Path is set (legacy
// compatibility, so existing string-list profiles round-trip unchanged) and the
// typed-object form once Source or Version is populated. Round-trip is stable
// under repeated marshal/unmarshal.
func (r PromptFileRef) MarshalJSON() ([]byte, error) {
	if r.Source == "" && r.Version == "" {
		return json.Marshal(r.Path)
	}
	type wire struct {
		Source  string `json:"source,omitempty"`
		Path    string `json:"path"`
		Version string `json:"version,omitempty"`
	}
	return json.Marshal(wire{Source: r.Source, Path: r.Path, Version: r.Version})
}

// UnmarshalJSON accepts either a bare string (legacy form) or the typed-object
// {source, path, version} form.
func (r *PromptFileRef) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		if s == "" {
			return fmt.Errorf("prompt_files entry string form must be non-empty")
		}
		r.Source = ""
		r.Path = s
		r.Version = ""
		return nil
	}
	type wire struct {
		Source  string `json:"source,omitempty"`
		Path    string `json:"path"`
		Version string `json:"version,omitempty"`
	}
	var w wire
	if err := json.Unmarshal(data, &w); err != nil {
		return fmt.Errorf("prompt_files entry must be string or {source?,path,version?}: %w", err)
	}
	if w.Path == "" {
		return fmt.Errorf("prompt_files entry object form requires non-empty path")
	}
	r.Source = w.Source
	r.Path = w.Path
	r.Version = w.Version
	return nil
}

// StageProfile is one named entry in an AgentsRC.StageProfiles stage map
// (executor | verifier | reviewer | orchestrator). A profile is a label for
// display plus a base-first ordered prompt_files composition. The same type
// serves every stage — the stage is the outer map key — so the four agentic
// stages are uniform, composable primitives. PromptFiles is source-aware (see
// PromptFileRef) so an org layer can pin a prompt to a remote config source
// while a repo keeps the legacy local string form.
type StageProfile struct {
	// Label is the human-readable profile name shown in fanout/explain output.
	Label string `json:"label,omitempty"`
	// PromptFiles is the base-first ordered prompt composition for the profile.
	PromptFiles []PromptFileRef `json:"prompt_files,omitempty"`
	// PreconditionPolicy names the verifier precondition policy (a key in the
	// top-level precondition_policies registry) that gates this profile's
	// in_progress → awaiting_agent_review transition. Unset ⇒ the built-in
	// `default` gate (verifier-precondition-policy plan, Slice B).
	PreconditionPolicy string `json:"precondition_policy,omitempty"`
}

// RefreshMetadata records the latest da install/refresh that updated a project.
type RefreshMetadata struct {
	Version     string `json:"version,omitempty"`
	Commit      string `json:"commit,omitempty"`
	Describe    string `json:"describe,omitempty"`
	RefreshedAt string `json:"refreshedAt,omitempty"`
}

// SetRefreshMetadata stores the latest refresh details in the manifest.
func (a *AgentsRC) SetRefreshMetadata(version, commit, describe string, refreshedAt time.Time) {
	if a == nil {
		return
	}
	a.Refresh = &RefreshMetadata{
		Version:     version,
		Commit:      commit,
		Describe:    describe,
		RefreshedAt: refreshedAt.UTC().Format(time.RFC3339),
	}
}

// agentsRCKnown lists all JSON keys owned by AgentsRC's known fields.
// Per [[schema-usage]]: this MUST stay in sync with the struct, agentsRCCore,
// MarshalJSON, UnmarshalJSON, and schemas/agentsrc.schema.json — any drift
// here silently routes the key into ExtraFields instead of the typed field.
var agentsRCKnown = map[string]bool{
	"$schema": true, "version": true, "project": true,
	"skills": true, "rules": true, "agents": true,
	"hooks": true, "mcp": true, "settings": true, "sources": true,
	"kg": true, "refresh": true,
	// v2 additive fields (config-distribution-model §3)
	"repo_id": true, "extends": true, "packages": true, "features": true,
	// execution-profile layer (config relevance / skill-relevance-filter)
	"execution_profile": true,
	// pr_source: config-driven PR event producer (pr-event-source design)
	"pr_source": true,
	// stage_profiles: unified per-stage named prompt-composition primitive
	// (config-v2 Q1; supersedes verifier_profiles/reviewer_profiles)
	"stage_profiles": true,
	// precondition_policies: top-level named registry of verifier precondition
	// policies (verifier-precondition-policy plan, Slice B)
	"precondition_policies": true,
	// deprecated legacy keys — read and folded into stage_profiles /
	// execution_profile for back-compat, never re-emitted (see foldLegacyProfiles).
	// Listed as "known" so they are not captured into ExtraFields (which would
	// re-emit them); they are decoded explicitly in UnmarshalJSON instead.
	"verifier_profiles":     true,
	"reviewer_profiles":     true,
	"app_type_verifier_map": true,
}

// agentsRCCore is an alias used in custom marshal/unmarshal to avoid
// infinite recursion while still using the standard json encoder.
// Per [[schema-usage]]: this MUST mirror AgentsRC's typed fields exactly.
type agentsRCCore struct {
	Schema   string           `json:"$schema,omitempty"`
	Version  int              `json:"version"`
	Project  string           `json:"project,omitempty"`
	Skills   []string         `json:"skills,omitempty"`
	Rules    []string         `json:"rules,omitempty"`
	Agents   []string         `json:"agents,omitempty"`
	Hooks    StringsOrBool    `json:"hooks"`
	MCP      StringsOrBool    `json:"mcp"`
	Settings bool             `json:"settings"`
	Sources  []Source         `json:"sources"`
	KG       *AgentsRCKG      `json:"kg,omitempty"`
	Refresh  *RefreshMetadata `json:"refresh,omitempty"`

	// v2 additive fields (config-distribution-model §3)
	RepoID           string            `json:"repo_id,omitempty"`
	Extends          []LayerRef        `json:"extends,omitempty"`
	Packages         []PackageRef      `json:"packages,omitempty"`
	Features         map[string]string `json:"features,omitempty"`
	ExecutionProfile *ExecutionProfile `json:"execution_profile,omitempty"`
	PRSource         *AgentsRCPRSource `json:"pr_source,omitempty"`

	StageProfiles        map[string]map[string]StageProfile `json:"stage_profiles,omitempty"`
	PreconditionPolicies map[string]PreconditionPolicySpec  `json:"precondition_policies,omitempty"`
}

func (a *AgentsRC) UnmarshalJSON(data []byte) error {
	var core agentsRCCore
	if err := json.Unmarshal(data, &core); err != nil {
		return err
	}
	a.Schema = core.Schema
	a.Version = core.Version
	a.Project = core.Project
	a.Skills = core.Skills
	a.Rules = core.Rules
	a.Agents = core.Agents
	a.Hooks = core.Hooks
	a.MCP = core.MCP
	a.Settings = core.Settings
	a.Sources = core.Sources
	a.KG = core.KG
	a.Refresh = core.Refresh
	a.RepoID = core.RepoID
	a.Extends = core.Extends
	a.Packages = core.Packages
	a.Features = core.Features
	a.ExecutionProfile = core.ExecutionProfile
	a.PRSource = core.PRSource
	a.StageProfiles = core.StageProfiles
	a.PreconditionPolicies = core.PreconditionPolicies

	// Back-compat: read the deprecated verifier_profiles / reviewer_profiles /
	// app_type_verifier_map keys and fold them into the unified stage_profiles +
	// execution_profile model. New-key values always win; legacy only fills gaps.
	// The legacy keys are never re-emitted (agentsRCCore has no legacy fields).
	var legacy struct {
		VerifierProfiles   map[string]StageProfile `json:"verifier_profiles"`
		ReviewerProfiles   map[string]StageProfile `json:"reviewer_profiles"`
		AppTypeVerifierMap map[string][]string     `json:"app_type_verifier_map"`
	}
	if err := json.Unmarshal(data, &legacy); err != nil {
		return err
	}
	a.foldLegacyProfiles(legacy.VerifierProfiles, legacy.ReviewerProfiles, legacy.AppTypeVerifierMap)

	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		return err
	}
	for k, v := range all {
		if !agentsRCKnown[k] {
			if a.ExtraFields == nil {
				a.ExtraFields = make(map[string]json.RawMessage)
			}
			a.ExtraFields[k] = v
		}
	}
	return nil
}

func (a AgentsRC) MarshalJSON() ([]byte, error) {
	core := agentsRCCore{
		Schema:               a.Schema,
		Version:              a.Version,
		Project:              a.Project,
		Skills:               a.Skills,
		Rules:                a.Rules,
		Agents:               a.Agents,
		Hooks:                a.Hooks,
		MCP:                  a.MCP,
		Settings:             a.Settings,
		Sources:              a.Sources,
		KG:                   a.KG,
		Refresh:              a.Refresh,
		RepoID:               a.RepoID,
		Extends:              a.Extends,
		Packages:             a.Packages,
		Features:             a.Features,
		ExecutionProfile:     a.ExecutionProfile,
		PRSource:             a.PRSource,
		StageProfiles:        a.StageProfiles,
		PreconditionPolicies: a.PreconditionPolicies,
	}
	data, err := json.Marshal(core)
	if err != nil {
		return nil, err
	}
	if len(a.ExtraFields) == 0 {
		return data, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	for k, v := range a.ExtraFields {
		if _, exists := m[k]; !exists {
			m[k] = v
		}
	}
	return json.Marshal(m)
}

// Source describes where to find agent resources. The v1 surface accepts
// `local` and `git` types; v2 adds `http` and `oci` per config-distribution-model
// §4. The v2 additive fields (ID, CacheTTL, Auth) all use omitempty so a v1
// Source round-trips byte-for-byte when those fields are absent.
type Source struct {
	Type string `json:"type"`           // "local" | "git" | "http" | "oci"
	Path string `json:"path,omitempty"` // override path for "local"
	URL  string `json:"url,omitempty"`  // repository URL for "git" / "http" / "oci"
	Ref  string `json:"ref,omitempty"`  // branch/tag for "git", or OCI tag

	// --- v2 additive fields (config-distribution-model §3.2) ---

	// ID is the stable local identifier used in extends/packages refs.
	// Required for v2 sources referenced by extends or packages; optional for
	// bare v1-style sources that exist only for legacy compatibility.
	ID string `json:"id,omitempty"`
	// CacheTTL is a duration string (e.g. "4h") governing tier-1 layer TTL.
	// Ignored for oci sources (which are strictly content-addressed per spec §8).
	CacheTTL string `json:"cache_ttl,omitempty"`
	// Auth is an opaque pass-through block whose schema is owned by the
	// external-agent-sources spec. The config layer treats it as an arbitrary
	// JSON object and does not introspect it.
	Auth json.RawMessage `json:"auth,omitempty"`
	// CacheKeys overrides the kind-default content cache key for this source
	// (config-distribution-model §7A.4, the uv `cache-keys` analog). Absent ⇒
	// the source uses its kind default (git→commit, local→commit+worktree,
	// http→ETag/Last-Modified else digest, oci→digest). See cache_keys.go for
	// the derivation; a *CacheKeys pointer keeps a defaulting source byte-stable
	// (no cache_keys object emitted). Omitted on a v1 source.
	CacheKeys *CacheKeys `json:"cache_keys,omitempty"`
}

const AgentsRCFile = ".agentsrc.json"

// LoadAgentsRC reads .agentsrc.json from the given project directory.
func LoadAgentsRC(projectPath string) (*AgentsRC, error) {
	path := filepath.Join(projectPath, AgentsRCFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rc AgentsRC
	if err := json.Unmarshal(data, &rc); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", AgentsRCFile, err)
	}
	// Default to a local source if none declared
	if len(rc.Sources) == 0 {
		rc.Sources = []Source{{Type: "local"}}
	}
	return &rc, nil
}

// Save writes the manifest to .agentsrc.json in projectPath.
func (a *AgentsRC) Save(projectPath string) error {
	path := filepath.Join(projectPath, AgentsRCFile)
	data, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling %s: %w", AgentsRCFile, err)
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0644)
}

// AgentsCacheDir returns the root directory for cached remote sources.
func AgentsCacheDir() string {
	cacheHome := os.Getenv("XDG_CACHE_HOME")
	if cacheHome == "" {
		home, _ := UserHomeDir()
		cacheHome = filepath.Join(home, ".cache")
	}
	return filepath.Join(cacheHome, "dot-agents")
}

// GitSourceCacheDir returns the cache directory for a given git URL.
func GitSourceCacheDir(url string) string {
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(url)))[:12]
	return filepath.Join(AgentsCacheDir(), "sources", hash)
}

// AppendUnique appends s to slice only if not already present.
func AppendUnique(slice []string, s string) []string {
	for _, v := range slice {
		if v == s {
			return slice
		}
	}
	return append(slice, s)
}

// GenerateAgentsRC inspects ~/.agents/ and builds a manifest for the given project.
func GenerateAgentsRC(projectName, projectPath string) (*AgentsRC, error) {
	agentsHome := AgentsHome()

	rc := &AgentsRC{
		Schema:  "https://dot-agents.dev/schemas/agentsrc.json",
		Version: 1,
		Project: projectName,
		Sources: []Source{{Type: "local"}},
	}

	// Auto-derive repo_id from the project's git remote (org-config-resolution §5).
	// Empty string when there is no git checkout / no origin remote — left
	// blank rather than fabricated so `da doctor` can warn (p2+ scope).
	rc.RepoID = DeriveRepoIDFromGit(projectPath)

	scopes := []string{"global", projectName}
	rc.Skills = collectScopedDirs(agentsHome, "skills", scopes, "SKILL.md")
	rc.Agents = collectScopedDirs(agentsHome, "agents", scopes, "AGENT.md")
	rc.Rules = detectRuleScopes(agentsHome, projectName)
	rc.Hooks = detectHookEvents(agentsHome, projectName)
	rc.MCP = detectMCPServers(agentsHome, projectName)
	rc.Settings = detectPlatformSettings(agentsHome, projectName)

	return rc, nil
}

// MergeGenerateAgentsRC overlays a freshly generated manifest onto an existing
// on-disk manifest. Scan-derived lists (skills, rules, agents, hooks, mcp,
// settings) come from generated; an existing non-empty project name, unknown
// JSON keys (ExtraFields), and supplemental sources (e.g. git remotes not
// produced by GenerateAgentsRC) are preserved. Source entries are unioned with
// deduplication so the default local source is not duplicated when merging.
func MergeGenerateAgentsRC(existing, generated *AgentsRC) *AgentsRC {
	if existing == nil {
		return generated
	}
	if generated == nil {
		return existing
	}
	out := *generated
	out.Sources = mergeSourceSlices(generated.Sources, existing.Sources)
	if existing.Project != "" {
		out.Project = existing.Project
	}
	// repo_id is a PROTECTED scalar per org-config-resolution §7.4 — an
	// explicit value committed in the manifest must survive regeneration.
	// Falling back to the generated (derived-from-git) value when existing
	// is empty preserves the bootstrap behaviour for v1 manifests being
	// upgraded in place.
	if existing.RepoID != "" {
		out.RepoID = existing.RepoID
	}
	if len(existing.ExtraFields) > 0 {
		out.ExtraFields = cloneExtraFieldsMap(existing.ExtraFields)
	}
	// stage_profiles are author-owned config, not scan-derived, so a committed set
	// must survive regeneration. Before these were typed fields the profiles rode
	// along in ExtraFields and were preserved by the clause above; now they are
	// first-class and must be carried over explicitly or `da install`/refresh
	// would silently drop a project's registered profiles.
	if len(existing.StageProfiles) > 0 {
		out.StageProfiles = cloneStageProfiles(existing.StageProfiles)
	}
	if existing.Refresh != nil {
		out.Refresh = existing.Refresh
	}
	return &out
}

// cloneStageProfiles deep-copies a stage_profiles map (stage → slug → profile,
// including each profile's prompt_files slice) so the merged manifest does not
// alias the existing manifest's profile data.
func cloneStageProfiles(m map[string]map[string]StageProfile) map[string]map[string]StageProfile {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]map[string]StageProfile, len(m))
	for stage, profiles := range m {
		inner := make(map[string]StageProfile, len(profiles))
		for slug, p := range profiles {
			inner[slug] = StageProfile{
				Label:              p.Label,
				PromptFiles:        append([]PromptFileRef(nil), p.PromptFiles...),
				PreconditionPolicy: p.PreconditionPolicy,
			}
		}
		out[stage] = inner
	}
	return out
}

// foldLegacyProfiles folds the deprecated verifier_profiles / reviewer_profiles /
// app_type_verifier_map keys into the unified stage_profiles + execution_profile
// model so legacy manifests keep loading. New-key values always win — legacy only
// fills gaps — and the folded keys are never re-emitted (agentsRCCore carries no
// legacy fields). See the stage-profile-and-routing-consolidation spec.
func (a *AgentsRC) foldLegacyProfiles(verifier, reviewer map[string]StageProfile, appTypeVerifierMap map[string][]string) {
	a.foldStageProfiles("verifier", verifier)
	a.foldStageProfiles("reviewer", reviewer)
	for appType, seq := range appTypeVerifierMap {
		a.foldAppTypeVerifierSequence(appType, seq)
	}
}

// foldStageProfiles fills stage_profiles[stage] with legacy profiles, leaving any
// slug already set by the new key untouched (new key wins).
func (a *AgentsRC) foldStageProfiles(stage string, profiles map[string]StageProfile) {
	if len(profiles) == 0 {
		return
	}
	if a.StageProfiles == nil {
		a.StageProfiles = map[string]map[string]StageProfile{}
	}
	if a.StageProfiles[stage] == nil {
		a.StageProfiles[stage] = map[string]StageProfile{}
	}
	for slug, p := range profiles {
		if _, exists := a.StageProfiles[stage][slug]; !exists {
			a.StageProfiles[stage][slug] = p
		}
	}
}

// foldAppTypeVerifierSequence folds one legacy app_type_verifier_map entry into
// execution_profile.by_app_type[appType].topology.verifier_sequence, creating the
// entry when absent and only when the new field is unset (new value wins).
func (a *AgentsRC) foldAppTypeVerifierSequence(appType string, seq []string) {
	if appType == "" || len(seq) == 0 {
		return
	}
	if a.ExecutionProfile == nil {
		a.ExecutionProfile = &ExecutionProfile{}
	}
	if a.ExecutionProfile.ByAppType == nil {
		a.ExecutionProfile.ByAppType = map[string]AppTypeProfile{}
	}
	prof := a.ExecutionProfile.ByAppType[appType]
	if len(prof.Topology.VerifierSequence) == 0 {
		prof.Topology.VerifierSequence = append([]string(nil), seq...)
		a.ExecutionProfile.ByAppType[appType] = prof
	}
}

func cloneExtraFieldsMap(m map[string]json.RawMessage) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func mergeSourceSlices(generated, existing []Source) []Source {
	seen := make(map[string]bool)
	var out []Source
	for _, s := range generated {
		k := sourceMergeKey(s)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, s)
	}
	for _, s := range existing {
		k := sourceMergeKey(s)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, s)
	}
	return out
}

func sourceMergeKey(s Source) string {
	switch s.Type {
	case "local":
		return "local:" + s.Path
	case "git":
		return "git:" + s.URL + "\x00" + s.Ref
	default:
		return "type:" + s.Type + "\x00" + s.Path + "\x00" + s.URL + "\x00" + s.Ref
	}
}

// collectScopedDirs returns unique entry names from resource subdirs that contain markerFile.
func collectScopedDirs(agentsHome, resourceType string, scopes []string, markerFile string) []string {
	var names []string
	for _, scope := range scopes {
		dir := filepath.Join(agentsHome, resourceType, scope)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			entryPath := filepath.Join(dir, e.Name())
			if !isDirEntry(entryPath) {
				continue
			}
			if _, err := os.Stat(filepath.Join(entryPath, markerFile)); err == nil {
				names = AppendUnique(names, e.Name())
			}
		}
	}
	return names
}

// detectHookEvents reads the project claude-code.json and returns a StringsOrBool
// listing hook event names that have at least one entry.
func detectHookEvents(agentsHome, projectName string) StringsOrBool {
	for _, scope := range []string{projectName, "global"} {
		if hasYAMLHooks(filepath.Join(agentsHome, "hooks", scope)) {
			return StringsOrBool{All: true}
		}
	}
	for _, scope := range []string{projectName, "global"} {
		if result := detectSettingsHookEvents(agentsHome, scope); result.IsEnabled() {
			return result
		}
	}
	return StringsOrBool{}
}

func hasYAMLHooks(hooksDir string) bool {
	entries, err := os.ReadDir(hooksDir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(hooksDir, entry.Name(), "HOOK.yaml")); err == nil {
			return true
		}
	}
	return false
}

func detectSettingsHookEvents(agentsHome, scope string) StringsOrBool {
	settingsPath := filepath.Join(agentsHome, "settings", scope, "claude-code.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return StringsOrBool{}
	}
	var settings map[string]any
	if json.Unmarshal(data, &settings) != nil {
		return StringsOrBool{}
	}
	hooksVal, ok := settings["hooks"]
	if !ok {
		return StringsOrBool{}
	}
	hooksMap, ok := hooksVal.(map[string]any)
	if !ok {
		return StringsOrBool{}
	}
	var hookEvents []string
	for event, val := range hooksMap {
		if list, ok := val.([]any); ok && len(list) > 0 {
			hookEvents = append(hookEvents, event)
		}
	}
	if len(hookEvents) == 0 {
		return StringsOrBool{}
	}
	sort.Strings(hookEvents)
	return StringsOrBool{Names: hookEvents}
}

// detectMCPServers scans MCP config files for the project and global scopes
// and returns a StringsOrBool listing named server entries.
func detectMCPServers(agentsHome, projectName string) StringsOrBool {
	for _, scope := range []string{projectName, "global"} {
		if result := readMCPScope(agentsHome, scope); result.IsEnabled() {
			return result
		}
	}
	return StringsOrBool{}
}

// readMCPScope tries claude.json, mcp.json, then .mcp.json for a single scope directory
// and returns the server list from the first readable file.
func readMCPScope(agentsHome, scope string) StringsOrBool {
	for _, fname := range []string{"claude.json", "mcp.json", ".mcp.json"} {
		mcpPath := filepath.Join(agentsHome, "mcp", scope, fname)
		data, err := os.ReadFile(mcpPath)
		if err != nil {
			continue
		}
		var mcpConfig map[string]any
		if json.Unmarshal(data, &mcpConfig) != nil {
			continue
		}
		servers, ok := mcpConfig["servers"].(map[string]any)
		if !ok {
			servers, ok = mcpConfig["mcpServers"].(map[string]any)
		}
		if !ok {
			break
		}
		var names []string
		for name := range servers {
			names = append(names, name)
		}
		if len(names) > 0 {
			sort.Strings(names)
			return StringsOrBool{Names: names}
		}
		break // found a file, stop trying filenames
	}
	return StringsOrBool{}
}

// detectPlatformSettings returns true if a cursor.json settings file exists
// for the project or global scope.
func detectPlatformSettings(agentsHome, projectName string) bool {
	for _, scope := range []string{projectName, "global"} {
		if _, err := os.Stat(filepath.Join(agentsHome, "settings", scope, "cursor.json")); err == nil {
			return true
		}
	}
	return false
}

func detectRuleScopes(agentsHome, projectName string) []string {
	scopes := []string{"global"}
	projectRulesDir := filepath.Join(agentsHome, "rules", projectName)
	entries, err := os.ReadDir(projectRulesDir)
	if err != nil {
		return scopes
	}
	for _, entry := range entries {
		ext := filepath.Ext(entry.Name())
		if ext == ".md" || ext == ".mdc" || ext == ".txt" {
			return append(scopes, "project")
		}
	}
	return scopes
}
