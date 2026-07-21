package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/go-git/go-git/v6"

	"github.com/AGOrcha/dot-agents/internal/events"
	"github.com/AGOrcha/dot-agents/internal/fsops"
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

// Returns "" (no error) when:
//   - the directory is not a git checkout
//   - the repo has no `origin` remote (e.g. `git init` only)
//   - the remote URL cannot be parsed into a host+path pair
//   - the repo exists but its `.git/config` is genuinely corrupt (a
//     structured warning is emitted in this case via warnOnCorruptGitConfig,
//     but the "" fallback contract below still holds)
//
// Per spec §5.3 git derivation is a FALLBACK — callers must not overwrite
// an explicit repo_id set in the manifest. See MergeGenerateAgentsRC.
func DeriveRepoIDFromGit(repoPath string) string {
	raw, err := gitRemoteOriginURL(repoPath)
	if err != nil {
		warnOnCorruptGitConfig(repoPath, err)
		return ""
	}
	if raw == "" {
		return ""
	}
	return gitremote.CanonicalRepoID(raw)
}

// warnOnCorruptGitConfig emits a structured warning when gitRemoteOriginURL
// fails for a reason other than the two documented, legitimately non-fatal
// cases: gitremote.ErrNoOrigin (repo exists, no `origin` remote configured)
// and git.ErrRepositoryNotExists (repoPath is not a git checkout at all).
// Anything else reaching here is most commonly a malformed .git/config that
// go-git's Config() parse rejects during Open — a genuine corruption that
// DeriveRepoIDFromGit's "" fallback (spec §5.3) was silently swallowing.
// DeriveRepoIDFromGit still returns "" either way; this only adds a signal.
func warnOnCorruptGitConfig(repoPath string, err error) {
	if errors.Is(err, gitremote.ErrNoOrigin) || errors.Is(err, git.ErrRepositoryNotExists) {
		return
	}
	env, envErr := events.NewEnvelope(
		"event.config.git_origin_unreadable",
		"config.DeriveRepoIDFromGit",
		repoPath,
		time.Time{},
		[]byte(fmt.Sprintf(`{"repo_path":%q,"error":%q}`, repoPath, err.Error())),
	)
	if envErr != nil {
		return
	}
	emitConfigWarning(env)
}

// emitConfigWarning is the seam that surfaces a structured config-warning
// Envelope produced by warnOnCorruptGitConfig. Defaults to a single stderr
// line; tests override the seam to assert a warning fired without depending
// on process output.
var emitConfigWarning = func(env events.Envelope) {
	fmt.Fprintf(os.Stderr, "warning: %s: %s\n", env.Type, string(env.Payload))
}

// gitRemoteAllURLs is the seam returning every configured remote's URLs for a
// repo path. Defaults to gitremote.ReadAllRemotes; tests override it to inject
// multi-remote / divergent-origin topologies without standing up real .git
// directories.
var gitRemoteAllURLs = gitremote.ReadAllRemotes

// DeriveTrustedRepoID returns the canonical repo_id for the project at repoPath
// ONLY when the git origin is unambiguous (FORK-1 hybrid / R12). The second
// return is true when origin cannot be trusted as a portable identity:
//
//   - origin has multiple configured URLs, OR
//   - another remote canonicalizes to a DIFFERENT repo_id than origin
//     (e.g. the AGOrcha case: origin=NikashPrakash fork vs org=AGOrcha).
//
// On a non-git path, a repo with no/empty origin, or an unparseable origin URL
// the result is ("", false): not ambiguous, just no portable key — the caller
// falls back to the logical id (the registry map key). Callers MUST NOT trust
// repoID when ambiguous is true.
func DeriveTrustedRepoID(repoPath string) (repoID string, ambiguous bool) {
	remotes, err := gitRemoteAllURLs(repoPath)
	if err != nil || len(remotes) == 0 {
		return "", false
	}
	origin := remotes["origin"]
	if len(origin) == 0 || origin[0] == "" {
		return "", false
	}
	if len(origin) > 1 {
		return "", true // multiple origin URLs — not a single trusted identity
	}
	originID := gitremote.CanonicalRepoID(origin[0])
	if originID == "" {
		return "", false
	}
	if hasDivergentRemote(remotes, originID) {
		return "", true
	}
	return originID, false
}

// hasDivergentRemote reports whether any non-origin remote canonicalizes to a
// repo_id different from origin's — the signal that origin is not a trustworthy
// portable identity (R12, e.g. AGOrcha origin=fork vs org=canonical).
func hasDivergentRemote(remotes map[string][]string, originID string) bool {
	for name, urls := range remotes {
		if name == "origin" {
			continue
		}
		for _, u := range urls {
			if id := gitremote.CanonicalRepoID(u); id != "" && id != originID {
				return true
			}
		}
	}
	return false
}

// isDirEntry reports whether the path is a directory, following symlinks. A
// legitimately absent path returns (false, nil); a real Stat error (e.g.
// permission denied) is surfaced instead of being silently treated as
// "not a directory".
func isDirEntry(path string) (bool, error) {
	info, found, err := fsops.StatAllowMissing(path)
	if err != nil {
		return false, err
	}
	if !found {
		return false, nil
	}
	return info.IsDir(), nil
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

const observabilityCredentialRefKind = "credential-ref"

var observabilityCredentialIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// AgentsRCObservabilityAuth references one atomic credential in the shared
// credential store. The resolved secret shape is intentionally not part of
// AgentsRC; only the credential reference is committed to the repository.
type AgentsRCObservabilityAuth struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// UnmarshalJSON rejects unknown fields and validates the committed
// credential-reference shape before it can enter the typed AgentsRC surface.
func (a *AgentsRCObservabilityAuth) UnmarshalJSON(data []byte) error {
	type wire AgentsRCObservabilityAuth
	var decoded wire
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return fmt.Errorf("observability auth: %w", err)
	}
	if decoded.Kind != observabilityCredentialRefKind {
		return fmt.Errorf("observability auth kind must be %q", observabilityCredentialRefKind)
	}
	if !observabilityCredentialIDPattern.MatchString(decoded.ID) {
		return fmt.Errorf("observability auth id must match %s", observabilityCredentialIDPattern)
	}
	*a = AgentsRCObservabilityAuth(decoded)
	return nil
}

// AgentsRCObservability configures publication to an observability backend.
// PushThrottleSeconds defaults to zero, which publishes immediately.
type AgentsRCObservability struct {
	Enabled             bool                       `json:"enabled"`
	Endpoint            string                     `json:"endpoint"`
	PushThrottleSeconds int                        `json:"push_throttle_seconds"`
	Auth                *AgentsRCObservabilityAuth `json:"auth,omitempty"`
}

// UnmarshalJSON keeps the nested block strict and enforces the transport
// boundary before any credential-ref can be consumed by a client.
func (o *AgentsRCObservability) UnmarshalJSON(data []byte) error {
	var decoded struct {
		Enabled             bool            `json:"enabled"`
		Endpoint            string          `json:"endpoint"`
		PushThrottleSeconds int             `json:"push_throttle_seconds"`
		Auth                json.RawMessage `json:"auth"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return fmt.Errorf("observability: %w", err)
	}

	var auth *AgentsRCObservabilityAuth
	if len(decoded.Auth) > 0 {
		if bytes.Equal(bytes.TrimSpace(decoded.Auth), []byte("null")) {
			return fmt.Errorf("observability auth must be an object")
		}
		var parsed AgentsRCObservabilityAuth
		if err := json.Unmarshal(decoded.Auth, &parsed); err != nil {
			return err
		}
		auth = &parsed
	}

	candidate := AgentsRCObservability{
		Enabled:             decoded.Enabled,
		Endpoint:            decoded.Endpoint,
		PushThrottleSeconds: decoded.PushThrottleSeconds,
		Auth:                auth,
	}
	if candidate.PushThrottleSeconds < 0 {
		return fmt.Errorf("observability push_throttle_seconds must be non-negative")
	}
	if candidate.Auth != nil {
		endpoint, err := url.Parse(candidate.Endpoint)
		if err != nil || !endpoint.IsAbs() || endpoint.Host == "" || !strings.EqualFold(endpoint.Scheme, "https") {
			return fmt.Errorf("observability credential-ref endpoint must be an absolute https URL")
		}
	}
	if candidate.Enabled && candidate.Auth == nil && !observabilityEndpointIsLoopback(candidate.Endpoint) {
		return fmt.Errorf("enabled non-loopback observability endpoint requires auth")
	}
	*o = candidate
	return nil
}

func observabilityEndpointIsLoopback(raw string) bool {
	endpoint, err := url.Parse(raw)
	if err != nil || !endpoint.IsAbs() || endpoint.Host == "" {
		return false
	}
	host := endpoint.Hostname()
	return strings.EqualFold(host, "localhost") || host == "127.0.0.1" || host == "::1"
}

// WorkTracking read_from values (work-tracking-storage-abstraction §9 read-from-master shim).
const (
	// WorkTrackingReadFromWorktree reads coordination state (TASKS.yaml /
	// PLAN.yaml) from the per-worktree working copy — today's behaviour and the
	// default when work_tracking.read_from is unset.
	WorkTrackingReadFromWorktree = "worktree"
	// WorkTrackingReadFromMaster reads coordination state from the canonical ref
	// (origin/<default-branch>) via `git show`, so worktree isolation cannot make
	// the orchestrator/scout see a stale status and re-dispatch in-flight work.
	// Writes are unchanged — they still land in the working copy.
	WorkTrackingReadFromMaster = "master"
)

// WorkTracking write_to values (work-tracking-storage-abstraction §9 D9
// git-ref backend CAS write path). ADDITIVE: the per-worktree working copy is
// ALWAYS written; write_to only selects whether the transition is ALSO
// mirrored to the shared state ref.
const (
	// WorkTrackingWriteToWorktree writes coordination state ONLY to the
	// per-worktree working copy — today's behaviour and the default when
	// work_tracking.write_to is unset. No git ref is written.
	WorkTrackingWriteToWorktree = "worktree"
	// WorkTrackingWriteToStateRef ADDITIONALLY mirrors each status transition
	// to refs/agents/state via atomic compare-and-swap (git update-ref
	// <new> <old>, retry-on-mismatch) — the git-ref shared SOT (D9). The
	// working copy is still written; the ref is an ADDITIONAL source of truth,
	// orthogonal to the code branch and never merged into the default branch
	// (D10).
	WorkTrackingWriteToStateRef = "state-ref"
)

// WorkTracking backend values (work-tracking-storage-abstraction §D7/D8/D9
// scope ladder). backend selects the coordination-state STORAGE PLANE. Only
// "local" (default) and "git-ref" are IMPLEMENTED; the remaining values are
// reserved ladder rungs that validate against the schema but are treated as
// unimplemented (fall back to local behaviour) until their backend lands.
const (
	// WorkTrackingBackendLocal stores coordination state in the per-worktree
	// working copy (TASKS.yaml / PLAN.yaml). This is today's behaviour and the
	// default when work_tracking.backend is unset/blank.
	WorkTrackingBackendLocal = "local"
	// WorkTrackingBackendGitRef serves coordination-state READS from the shared
	// state ref (refs/agents/state) per-task projection and IMPLIES the state-ref
	// write mirror (transitions CAS-update the ref even when write_to is unset).
	// Because the ref is a LOCAL ref updated in-process, reads from it are
	// read-your-writes safe (unlike the read_from=master remote-tracking shim).
	WorkTrackingBackendGitRef = "git-ref"
	// WorkTrackingBackendKG is a RESERVED ladder value (knowledge-graph backend);
	// not implemented — treated as local.
	WorkTrackingBackendKG = "kg"
	// WorkTrackingBackendCloudflareDO is a RESERVED ladder value (Cloudflare
	// Durable Objects backend); not implemented — treated as local.
	WorkTrackingBackendCloudflareDO = "cloudflare-do"
	// WorkTrackingBackendJira is a RESERVED ladder value (Jira backend); not
	// implemented — treated as local.
	WorkTrackingBackendJira = "jira"
	// WorkTrackingBackendLinear is a RESERVED ladder value (Linear backend); not
	// implemented — treated as local.
	WorkTrackingBackendLinear = "linear"
)

// AgentsRCWorkTracking is the work_tracking configuration block in
// agentsrc.json — the coordination-state storage plane
// (work-tracking-storage-abstraction spec, D8 scope ladder). Only the
// read-from-master shim (§9) is wired today; backend selection lands with the
// git-ref WorkStore backend.
type AgentsRCWorkTracking struct {
	// ReadFrom selects where coordination state is READ from:
	//   "worktree" (default / empty) — the per-worktree working copy (today's behaviour)
	//   "master"                     — the canonical ref (origin/<default-branch>) via `git show`
	// The write side is unchanged regardless of this value (read-side-only shim).
	ReadFrom string `json:"read_from,omitempty"`
	// WriteTo selects where a status transition is WRITTEN:
	//   "worktree" (default / empty) — only the per-worktree working copy (today's behaviour)
	//   "state-ref"                  — ALSO mirror the transition to refs/agents/state via CAS (additive)
	// The working-copy write happens regardless of this value; "state-ref"
	// only adds the git-ref mirror. Backend selection via the D8 scope ladder
	// (work_tracking.backend) supersedes this focused gate in a later task.
	WriteTo string `json:"write_to,omitempty"`
	// Backend selects the coordination-state STORAGE PLANE (D8 scope ladder):
	//   "local" (default / empty) — the per-worktree working copy (today's behaviour)
	//   "git-ref"                 — reads project from refs/agents/state and the
	//                               state-ref write mirror is implied (see below)
	// Reserved (validate but treated as local until implemented): "kg",
	// "cloudflare-do", "jira", "linear". Under "git-ref", coordination reads
	// resolve from the LOCAL state ref (read-your-writes safe) and take
	// precedence over the read_from=master shim; the write mirror is implied
	// even when write_to is unset.
	Backend string `json:"backend,omitempty"`
}

// ReadFromMaster reports whether coordination state (TASKS.yaml / PLAN.yaml)
// should be READ from the canonical ref (origin/<default-branch>) rather than
// the per-worktree working copy — i.e. work_tracking.read_from == "master".
// A nil receiver or absent/blank/"worktree" config yields false, preserving
// today's byte-for-byte worktree read behaviour by default.
func (a *AgentsRC) ReadFromMaster() bool {
	return a != nil && a.WorkTracking != nil && a.WorkTracking.ReadFrom == WorkTrackingReadFromMaster
}

// WriteToStateRef reports whether a status transition should ALSO be mirrored
// to the git-ref shared SOT (refs/agents/state) via compare-and-swap — i.e.
// work_tracking.write_to == "state-ref". A nil receiver or absent/blank/
// "worktree" config yields false, preserving today's byte-for-byte
// working-copy-only write behaviour by default (no ref is written).
func (a *AgentsRC) WriteToStateRef() bool {
	return a != nil && a.WorkTracking != nil && a.WorkTracking.WriteTo == WorkTrackingWriteToStateRef
}

// WorkStoreBackend returns the configured coordination-state backend, defaulting
// to "local" when the receiver, work_tracking block, or backend value is
// nil/absent/blank — so an unset config resolves to today's working-copy plane.
func (a *AgentsRC) WorkStoreBackend() string {
	if a == nil || a.WorkTracking == nil || a.WorkTracking.Backend == "" {
		return WorkTrackingBackendLocal
	}
	return a.WorkTracking.Backend
}

// UseGitRefBackend reports whether the git-ref WorkStore backend is active —
// work_tracking.backend == "git-ref". A nil receiver or absent/blank/"local"
// config yields false, preserving today's byte-for-byte working-copy behaviour
// by default. When true, coordination reads project from refs/agents/state and
// the state-ref write mirror is implied.
func (a *AgentsRC) UseGitRefBackend() bool {
	return a.WorkStoreBackend() == WorkTrackingBackendGitRef
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
	Schema        string                 `json:"$schema,omitempty"`
	Version       int                    `json:"version"`
	Project       string                 `json:"project,omitempty"`
	Skills        []string               `json:"skills,omitempty"`
	Rules         []string               `json:"rules,omitempty"`
	Agents        []string               `json:"agents,omitempty"`
	Hooks         StringsOrBool          `json:"hooks"`
	MCP           StringsOrBool          `json:"mcp"`
	Settings      bool                   `json:"settings"`
	Sources       []Source               `json:"sources"`
	KG            *AgentsRCKG            `json:"kg,omitempty"`
	Observability *AgentsRCObservability `json:"observability,omitempty"`

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

	// WorkTracking is the coordination-state storage plane config
	// (work-tracking-storage-abstraction spec). Today it carries the §9
	// read-from-master shim (read_from); backend selection lands with the
	// git-ref WorkStore backend. Scope-mergeable like execution_profile (D8).
	WorkTracking *AgentsRCWorkTracking `json:"work_tracking,omitempty"`

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

	// Locks is the §15 D1a authority-axis lock block a layer emits in the
	// policy-authority pass (Phase 1): value_locks (pin a field, rejecting lower
	// writes) and deny_locks (subtract a set member, deny-overrides). force_allow
	// is invalid and aborts the resolve. A lock binds only scopes ranked below
	// its owner's AUTHORITY-RANK (org > team > repo > user). See authority.go.
	Locks *PolicyLockSpec `json:"locks,omitempty"`

	// AuthorityGrants is the §15 D1a source-authority registry block: a per-source
	// allowlist "source-id → scope it may carry." It is honored only when written
	// by a layer whose own authority is at least the conferred scope — a lower
	// scope cannot self-bless authority (a resolve-time rejection). See
	// resolveAuthorityGrants in authority.go.
	AuthorityGrants map[string]AuthorityScope `json:"authority_grants,omitempty"`

	// LayeringPolicy is the scope-attached unified-config-profiles (L1) policy
	// unit: the Phase-1 layering governance a scope emits — precedence, absolute
	// locks (deny/value, no force-allow), the Decision-2 three-state
	// override_permissions, and the Q4 replace-mode marker. Its owning authority
	// scope is SOURCE-derived (set by the resolver from the owning layer), never
	// authored on the unit. See internal/config/profile.go and the
	// unified-config-profiles design (§2.3).
	LayeringPolicy *LayeringPolicy `json:"layering_policy,omitempty"`

	// Manifests are the scope-attached distributable config manifest units (L2),
	// keyed by name; each manifest's absolute ref is <owning-source>:<name>. A
	// manifest REFERENCES (by ref) the sources to pull, the layering policy/profiles
	// that bind, and (optionally) the project-set to manage — it never inlines
	// copies (distributable-config-manifest D1/D2/R2). This lenient typed shape is
	// what round-trips; the strict fail-closed decode (no manifest->manifest edge,
	// no self-declared authority, no force-allow) runs at resolve time in
	// manifest.go. Its owning authority is SOURCE-derived, never authored here (D4).
	Manifests map[string]ManifestSpec `json:"manifests,omitempty"`

	// ExtraFields captures unknown JSON keys so Save() can round-trip them
	// instead of silently dropping legacy or custom fields.
	ExtraFields map[string]json.RawMessage `json:"-"`

	// LegacyKeys lists the deprecated v1 JSON keys observed in the source
	// manifest during UnmarshalJSON (verifier_profiles / reviewer_profiles /
	// app_type_verifier_map). These keys are silently folded into the unified
	// stage_profiles / execution_profile model (see foldLegacyProfiles) and
	// never re-emitted; recording them here lets `da init` / `da doctor`
	// surface a deprecation warning without re-parsing the file (config-v2
	// §15.3 deprecation cadence). Not serialized.
	LegacyKeys []string `json:"-"`
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
// (executor | verifier | reviewer | orchestrator). A profile is a label,
// explicit OMP model route, and base-first ordered prompt_files composition.
// The same type serves every stage — the stage is the outer map key — so the
// four agentic stages are uniform, composable primitives. PromptFiles is
// source-aware (see PromptFileRef) so an org layer can pin a prompt to a remote
// config source while a repo keeps the legacy local string form.
type StageProfile struct {
	// Label is the human-readable profile name shown in fanout/explain output.
	Label string `json:"label,omitempty"`
	// Model is the concrete OMP model identifier used to run this stage.
	Model string `json:"model,omitempty"`
	// ModelFamily is the semantic family used for cross-family diversity gates.
	// It is intentionally open-ended; diversity requires inequality, not a
	// closed vendor list.
	ModelFamily string `json:"model_family,omitempty"`
	// PromptFiles is the base-first ordered prompt composition for the profile.
	PromptFiles []PromptFileRef `json:"prompt_files,omitempty"`
	// PreconditionPolicy names the verifier precondition policy (a key in the
	// top-level precondition_policies registry) that gates this profile's
	// in_progress → awaiting_agent_review transition. Unset ⇒ the built-in
	// `default` gate (verifier-precondition-policy plan, Slice B).
	PreconditionPolicy string `json:"precondition_policy,omitempty"`
}

// RefreshMetadata records the latest da install/refresh that updated a
// project. It is a LOCK type (config-distribution-model §7A /
// refresh-metadata-to-lock): the payload for .agentsrc.lock's "refresh"
// section, written via WriteRefreshLock and read via ReadRefreshLock — never
// a field of the committed .agentsrc.json manifest. Refresh metadata is
// resolved STATE about the project (what was last refreshed, with which da
// build), not manifest content, so it does not participate in schema
// validation of the manifest and is not carried through
// MergeGenerateAgentsRC.
type RefreshMetadata struct {
	Version     string `json:"version,omitempty"`
	Commit      string `json:"commit,omitempty"`
	Describe    string `json:"describe,omitempty"`
	RefreshedAt string `json:"refreshedAt,omitempty"`
}

// agentsRCKnown lists all JSON keys owned by AgentsRC's known fields.
// Per [[schema-usage]]: this MUST stay in sync with the struct, agentsRCCore,
// MarshalJSON, UnmarshalJSON, and schemas/agentsrc.schema.json — any drift
// here silently routes the key into ExtraFields instead of the typed field.
var agentsRCKnown = map[string]bool{
	"$schema": true, "version": true, "project": true,
	"skills": true, "rules": true, "agents": true,
	"hooks": true, "mcp": true, "settings": true, "sources": true,
	"kg": true, "observability": true,
	// refresh is a legacy pre-refresh-metadata-to-lock manifest key: refresh
	// metadata now lives in .agentsrc.lock's "refresh" section (RefreshMetadata /
	// WriteRefreshLock), never the committed manifest. It stays "known" here so a
	// legacy value is silently ignored during UnmarshalJSON — never captured into
	// ExtraFields (which would re-emit it) — instead of erroring; AgentsRC has no
	// Refresh field to decode it into, so it is dropped and the next
	// `da refresh`/Save naturally strips it from the manifest.
	"refresh": true,
	// v2 additive fields (config-distribution-model §3)
	"repo_id": true, "extends": true, "packages": true, "features": true,
	// execution-profile layer (config relevance / skill-relevance-filter)
	"execution_profile": true,
	// pr_source: config-driven PR event producer (pr-event-source design)
	"pr_source": true,
	// work_tracking: coordination-state storage plane (read-from-master shim §9)
	"work_tracking": true,
	// stage_profiles: unified per-stage named prompt-composition primitive
	// (config-v2 Q1; supersedes verifier_profiles/reviewer_profiles)
	"stage_profiles": true,
	// precondition_policies: top-level named registry of verifier precondition
	// policies (verifier-precondition-policy plan, Slice B)
	"precondition_policies": true,
	// §15 D1a authority/value two-axis resolver fields (config-distribution-model §15.9)
	"locks": true, "authority_grants": true,
	// unified-config-profiles (L1): scope-attached layering policy unit
	"layering_policy": true,
	// distributable-config-manifest (L2): scope-attached manifest units
	"manifests": true,
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
	Schema        string                 `json:"$schema,omitempty"`
	Version       int                    `json:"version"`
	Project       string                 `json:"project,omitempty"`
	Skills        []string               `json:"skills,omitempty"`
	Rules         []string               `json:"rules,omitempty"`
	Agents        []string               `json:"agents,omitempty"`
	Hooks         StringsOrBool          `json:"hooks"`
	MCP           StringsOrBool          `json:"mcp"`
	Settings      bool                   `json:"settings"`
	Sources       []Source               `json:"sources"`
	KG            *AgentsRCKG            `json:"kg,omitempty"`
	Observability *AgentsRCObservability `json:"observability,omitempty"`

	// v2 additive fields (config-distribution-model §3)
	RepoID           string                `json:"repo_id,omitempty"`
	Extends          []LayerRef            `json:"extends,omitempty"`
	Packages         []PackageRef          `json:"packages,omitempty"`
	Features         map[string]string     `json:"features,omitempty"`
	ExecutionProfile *ExecutionProfile     `json:"execution_profile,omitempty"`
	PRSource         *AgentsRCPRSource     `json:"pr_source,omitempty"`
	WorkTracking     *AgentsRCWorkTracking `json:"work_tracking,omitempty"`

	StageProfiles        map[string]map[string]StageProfile `json:"stage_profiles,omitempty"`
	PreconditionPolicies map[string]PreconditionPolicySpec  `json:"precondition_policies,omitempty"`

	Locks           *PolicyLockSpec           `json:"locks,omitempty"`
	AuthorityGrants map[string]AuthorityScope `json:"authority_grants,omitempty"`
	LayeringPolicy  *LayeringPolicy           `json:"layering_policy,omitempty"`
	Manifests       map[string]ManifestSpec   `json:"manifests,omitempty"`
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
	a.Observability = core.Observability
	a.RepoID = core.RepoID
	a.Extends = core.Extends
	a.Packages = core.Packages
	a.Features = core.Features
	a.ExecutionProfile = core.ExecutionProfile
	a.PRSource = core.PRSource
	a.WorkTracking = core.WorkTracking
	a.StageProfiles = core.StageProfiles
	a.PreconditionPolicies = core.PreconditionPolicies
	a.Locks = core.Locks
	a.AuthorityGrants = core.AuthorityGrants
	a.LayeringPolicy = core.LayeringPolicy
	a.Manifests = core.Manifests

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
	a.recordLegacyKeys(all)
	return nil
}

// recordLegacyKeys captures any deprecated v1 keys present in the raw manifest
// into a.LegacyKeys (sorted for stable output). The keys are decoded and folded
// elsewhere; this is purely the bookkeeping that lets init/doctor warn.
func (a *AgentsRC) recordLegacyKeys(all map[string]json.RawMessage) {
	var seen []string
	for _, k := range deprecatedV1Keys {
		if _, ok := all[k]; ok {
			seen = append(seen, k)
		}
	}
	sort.Strings(seen)
	a.LegacyKeys = seen
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
		Observability:        a.Observability,
		RepoID:               a.RepoID,
		Extends:              a.Extends,
		Packages:             a.Packages,
		Features:             a.Features,
		ExecutionProfile:     a.ExecutionProfile,
		PRSource:             a.PRSource,
		WorkTracking:         a.WorkTracking,
		StageProfiles:        a.StageProfiles,
		PreconditionPolicies: a.PreconditionPolicies,
		Locks:                a.Locks,
		AuthorityGrants:      a.AuthorityGrants,
		LayeringPolicy:       a.LayeringPolicy,
		Manifests:            a.Manifests,
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

// GenerateAgentsRC inspects ~/.agents/ and builds a manifest for the given
// project. It is fail-or-full: every scan it performs (skills/agents/rules/
// hooks/mcp/settings dirs) distinguishes a legitimately absent resource from
// a real I/O error (permission denied, unreadable directory, ...) via the
// internal/fsops allow-missing helpers, and any real failure anywhere
// aggregates (errors.Join) into a single (nil, err) return instead of
// silently degrading to an empty/zero manifest. Git origin lookup
// (DeriveRepoIDFromGit) keeps its own documented "" fallback per spec §5.3
// and is not part of this aggregation.
//
// Every scan below is PROJECT-SCOPE ONLY. A "global" scoped resource
// (skills/agents/rules/hooks/mcp/settings living under
// ~/.agents/<bucket>/global/) auto-resolves at the user level for every
// project via each platform adapter's scopedNames(project) = [project,
// "global"] link pass (internal/platform/resources.go) — it materializes to
// ~/.claude (or the platform-equivalent user home) regardless of whether any
// project declares it. Recording it in a project's committed .agentsrc.json
// would therefore be redundant at best and, worse, goes stale the moment the
// user's global set changes on another machine that lacks it. Declaring a
// project-scope resource is the only thing a project manifest can
// meaningfully own.
func GenerateAgentsRC(projectName, projectPath string) (*AgentsRC, error) {
	agentsHome := AgentsHome()

	rc := &AgentsRC{
		Schema:  "https://agorcha.dev/schemas/agentsrc.schema.json",
		Version: 1,
		Project: projectName,
		Sources: []Source{{Type: "local"}},
	}

	// Auto-derive repo_id from the project's git remote (org-config-resolution §5).
	// Empty string when there is no git checkout / no origin remote — left
	// blank rather than fabricated so `da doctor` can warn (p2+ scope).
	rc.RepoID = DeriveRepoIDFromGit(projectPath)

	// Default a new git-repo project to the git-ref work-tracking backend — the
	// shipped, recommended coordination plane (a durable refs/agents/state audit
	// trail + cross-worktree coordination, written ADDITIVELY over the working
	// copy). A non-git project has no ref to write, so it stays on the local
	// default. Existing manifests are untouched (this is generate-time only).
	if NewGoGitRepo().IsRepo(projectPath) {
		rc.WorkTracking = &AgentsRCWorkTracking{Backend: WorkTrackingBackendGitRef}
	}

	scopes := []string{projectName}

	var errs []error

	skills, err := collectScopedDirs(agentsHome, "skills", scopes, "SKILL.md")
	errs = append(errs, err)
	rc.Skills = skills

	agentsList, err := collectScopedDirs(agentsHome, "agents", scopes, "AGENT.md")
	errs = append(errs, err)
	rc.Agents = agentsList

	rules, err := detectRuleScopes(agentsHome, projectName)
	errs = append(errs, err)
	rc.Rules = rules

	hooks, err := detectHookEvents(agentsHome, projectName)
	errs = append(errs, err)
	rc.Hooks = hooks

	mcp, err := detectMCPServers(agentsHome, projectName)
	errs = append(errs, err)
	rc.MCP = mcp

	settings, err := detectPlatformSettings(agentsHome, projectName)
	errs = append(errs, err)
	rc.Settings = settings

	if joined := errors.Join(errs...); joined != nil {
		return nil, joined
	}

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
	// observability is author-owned endpoint/auth configuration. Preserve it
	// across generated-manifest rewrites now that it no longer rides in
	// ExtraFields, and clone the nested auth pointer to avoid aliasing.
	if existing.Observability != nil {
		observability := *existing.Observability
		if existing.Observability.Auth != nil {
			auth := *existing.Observability.Auth
			observability.Auth = &auth
		}
		out.Observability = &observability
	}
	// stage_profiles are author-owned config, not scan-derived, so a committed set
	// must survive regeneration. Before these were typed fields the profiles rode
	// along in ExtraFields and were preserved by the clause above; now they are
	// first-class and must be carried over explicitly or `da install`/refresh
	// would silently drop a project's registered profiles.
	if len(existing.StageProfiles) > 0 {
		out.StageProfiles = cloneStageProfiles(existing.StageProfiles)
	}
	// manifests are author-owned config like stage_profiles: now that they are a
	// typed field (L2) they no longer ride along in ExtraFields, so a generate /
	// refresh rewrite must carry a committed set over explicitly or `da install` /
	// refresh would silently drop a project's authored manifests (the
	// schema-usage.md typed-field/ExtraFields breakage rule).
	if len(existing.Manifests) > 0 {
		out.Manifests = cloneManifests(existing.Manifests)
	}
	return &out
}

// cloneManifests deep-copies a manifests map (name → spec, including each spec's
// source/bind ref slices) so the merged manifest does not alias the existing
// manifest's data.
func cloneManifests(m map[string]ManifestSpec) map[string]ManifestSpec {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]ManifestSpec, len(m))
	for name, spec := range m {
		out[name] = ManifestSpec{
			Sources:    append([]string(nil), spec.Sources...),
			Binds:      append([]string(nil), spec.Binds...),
			ProjectSet: spec.ProjectSet,
		}
	}
	return out
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
				Model:              p.Model,
				ModelFamily:        p.ModelFamily,
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

// collectScopedDirs returns unique entry names from resource subdirs that
// contain markerFile. A real I/O error (as opposed to a scope dir simply not
// existing yet) is aggregated and returned alongside whatever entries were
// found before the failure, so callers on the fail-or-full path (see
// GenerateAgentsRC) can distinguish "no skills configured" from "couldn't
// read the skills directory".
func collectScopedDirs(agentsHome, resourceType string, scopes []string, markerFile string) ([]string, error) {
	var names []string
	var errs []error
	for _, scope := range scopes {
		dir := filepath.Join(agentsHome, resourceType, scope)
		found, scopeErrs := markerDirsIn(dir, markerFile)
		errs = append(errs, scopeErrs...)
		for _, name := range found {
			names = AppendUnique(names, name)
		}
	}
	return names, errors.Join(errs...)
}

// markerDirsIn returns the names of immediate subdirectories of dir that
// contain markerFile. A missing dir yields no names and no error; real I/O
// errors (unreadable dir, un-stattable entry) are aggregated so the caller can
// surface them rather than silently reporting "nothing configured".
func markerDirsIn(dir, markerFile string) ([]string, []error) {
	entries, found, err := fsops.ReadDirAllowMissing(dir)
	if err != nil {
		return nil, []error{err}
	}
	if !found {
		return nil, nil
	}
	var names []string
	var errs []error
	for _, e := range entries {
		entryPath := filepath.Join(dir, e.Name())
		isDir, err := isDirEntry(entryPath)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !isDir {
			continue
		}
		_, markerFound, err := fsops.StatAllowMissing(filepath.Join(entryPath, markerFile))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if markerFound {
			names = append(names, e.Name())
		}
	}
	return names, errs
}

// detectHookEvents reads the project claude-code.json and returns a
// StringsOrBool listing hook event names that have at least one entry.
// PROJECT-SCOPE ONLY — see the GenerateAgentsRC doc comment: a global hook
// bundle/settings file auto-resolves to every project via each platform
// adapter's scopedNames(project) = [project, "global"] link pass regardless
// of manifest declaration, so folding "global" in here would only make the
// generated manifest misrepresent global state as project-owned. Real I/O
// errors from either underlying check are aggregated and returned so a
// chmod'd hooks or settings scope directory surfaces as a failure instead of
// "no hooks configured".
func detectHookEvents(agentsHome, projectName string) (StringsOrBool, error) {
	var errs []error
	hasYAML, err := hasYAMLHooks(filepath.Join(agentsHome, "hooks", projectName))
	if err != nil {
		errs = append(errs, err)
	} else if hasYAML {
		return StringsOrBool{All: true}, errors.Join(errs...)
	}
	result, err := detectSettingsHookEvents(agentsHome, projectName)
	if err != nil {
		errs = append(errs, err)
	} else if result.IsEnabled() {
		return result, errors.Join(errs...)
	}
	return StringsOrBool{}, errors.Join(errs...)
}

func hasYAMLHooks(hooksDir string) (bool, error) {
	entries, found, err := fsops.ReadDirAllowMissing(hooksDir)
	if err != nil {
		return false, err
	}
	if !found {
		return false, nil
	}
	var errs []error
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		_, markerFound, err := fsops.StatAllowMissing(filepath.Join(hooksDir, entry.Name(), "HOOK.yaml"))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if markerFound {
			return true, errors.Join(errs...)
		}
	}
	return false, errors.Join(errs...)
}

func detectSettingsHookEvents(agentsHome, scope string) (StringsOrBool, error) {
	settingsPath := filepath.Join(agentsHome, "settings", scope, "claude-code.json")
	data, found, err := fsops.ReadFileAllowMissing(settingsPath)
	if err != nil {
		return StringsOrBool{}, err
	}
	if !found {
		return StringsOrBool{}, nil
	}
	var settings map[string]any
	if json.Unmarshal(data, &settings) != nil {
		return StringsOrBool{}, nil
	}
	hooksVal, ok := settings["hooks"]
	if !ok {
		return StringsOrBool{}, nil
	}
	hooksMap, ok := hooksVal.(map[string]any)
	if !ok {
		return StringsOrBool{}, nil
	}
	var hookEvents []string
	for event, val := range hooksMap {
		if list, ok := val.([]any); ok && len(list) > 0 {
			hookEvents = append(hookEvents, event)
		}
	}
	if len(hookEvents) == 0 {
		return StringsOrBool{}, nil
	}
	sort.Strings(hookEvents)
	return StringsOrBool{Names: hookEvents}, nil
}

// detectMCPServers scans the project-scope MCP config file and returns a
// StringsOrBool listing named server entries. PROJECT-SCOPE ONLY — see the
// GenerateAgentsRC doc comment: a global MCP server config auto-resolves to
// every project via scopedNames(project) = [project, "global"] regardless of
// manifest declaration.
func detectMCPServers(agentsHome, projectName string) (StringsOrBool, error) {
	return readMCPScope(agentsHome, projectName)
}

// readMCPScope tries claude.json, mcp.json, then .mcp.json for a single scope
// directory and returns the server list from the first readable file. A real
// read error on any candidate file aborts immediately (rather than masking it
// by falling through to the next filename) so a chmod'd mcp scope directory
// surfaces as a failure.
func readMCPScope(agentsHome, scope string) (StringsOrBool, error) {
	for _, fname := range []string{"claude.json", "mcp.json", ".mcp.json"} {
		mcpPath := filepath.Join(agentsHome, "mcp", scope, fname)
		data, found, err := fsops.ReadFileAllowMissing(mcpPath)
		if err != nil {
			return StringsOrBool{}, err
		}
		if !found {
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
			return StringsOrBool{Names: names}, nil
		}
		break // found a file, stop trying filenames
	}
	return StringsOrBool{}, nil
}

// detectPlatformSettings returns true if a cursor.json settings file exists
// for the project scope. PROJECT-SCOPE ONLY — see the GenerateAgentsRC doc
// comment: a global cursor.json auto-resolves to every project via
// scopedNames(project) = [project, "global"] regardless of manifest
// declaration.
func detectPlatformSettings(agentsHome, projectName string) (bool, error) {
	_, found, err := fsops.StatAllowMissing(filepath.Join(agentsHome, "settings", projectName, "cursor.json"))
	return found, err
}

// detectRuleScopes returns ["project"] when the project has at least one
// project-scoped rule file, else nil. PROJECT-SCOPE ONLY — see the
// GenerateAgentsRC doc comment: a global rule file auto-resolves to every
// project via scopedNames(project) = [project, "global"] regardless of
// manifest declaration, so it is never folded in here (the prior
// unconditional "global" entry didn't even check the global file existed).
func detectRuleScopes(agentsHome, projectName string) ([]string, error) {
	projectRulesDir := filepath.Join(agentsHome, "rules", projectName)
	entries, found, err := fsops.ReadDirAllowMissing(projectRulesDir)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	for _, entry := range entries {
		ext := filepath.Ext(entry.Name())
		if ext == ".md" || ext == ".mdc" || ext == ".txt" {
			return []string{"project"}, nil
		}
	}
	return nil, nil
}
