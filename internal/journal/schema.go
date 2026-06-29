package journal

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// This file is the journal's command contract (spec D3/D4). It encodes, as typed
// Go structs, the per-command Input/Observed shapes from the spec's "Command
// Surface", and a single registry that maps each canonical command name to its
// Tier and its Input/Observed types. The registry is the single source of truth
// for WHICH commands are journaled and HOW: the emit wiring (a later task) looks a
// command up to learn its EventType and to marshal typed payloads, and the
// recovery view looks it up to decode an event's payloads back into the right
// type. A command absent from the registry is, by definition, not journaled
// (the spec's "Excluded" set — config, hook-sentinel/outcome, score).

// Tier classifies a journaled command by how much of its effect the record
// carries (spec "Command Surface" Tiers 1–2 + KG + review). The Tier selects the
// envelope EventType the emit wiring stamps.
type Tier string

const (
	// TierUnconditional (spec Tier 1) is a canonical state transition or
	// irreversible filesystem move: it journals every time with the full typed
	// observed delta. Maps to EventDurableDelta.
	TierUnconditional Tier = "tier-1"
	// TierDelta (spec Tier 2) journals only when it changed something; the full
	// content is re-readable from TASKS.yaml/PLAN.yaml/prefs, so observed carries
	// only the changed fields (R6). Maps to EventInputOnly.
	TierDelta Tier = "tier-2"
	// TierKG covers the KG commands: content deltas record counts + ids and
	// decision events record outcomes — never node/edge bodies (D4). These are
	// durable transitions, so they map to EventDurableDelta.
	TierKG Tier = "kg"
	// TierReview covers review approve/reject: the decision outcome is a durable
	// transition. Maps to EventDurableDelta.
	TierReview Tier = "review"
)

// Canonical command names — the registry keys and the value the emit wiring
// passes. Hoisted to consts so the name appears exactly once (no scattered string
// literals) and the same identifier is shared by registry, emit, and recovery.
const (
	CmdAdvance            = "workflow advance"
	CmdStartTask          = "workflow start-task"
	CmdCloseTask          = "workflow close-task"
	CmdPlanCreate         = "workflow plan create"
	CmdPlanArchive        = "workflow plan archive"
	CmdMergeBack          = "workflow merge-back"
	CmdFanout             = "workflow fanout"
	CmdFoldBackCreate     = "workflow fold-back create"
	CmdFoldBackUpdate     = "workflow fold-back update"
	CmdDelegationCloseout = "workflow delegation closeout"
	CmdContractCreate     = "workflow contract create"
	CmdPlanDeriveScope    = "workflow plan derive-scope"
	CmdVerifyRecord       = "workflow verify record"
	CmdCheckpoint         = "workflow checkpoint"
	CmdCommit             = "workflow commit"
	CmdArchiveOrphans     = "workflow archive-orphans"
	CmdSweepApply         = "workflow sweep --apply"

	CmdTaskAdd        = "workflow task add"
	CmdTaskUpdate     = "workflow task update"
	CmdPlanUpdate     = "workflow plan update"
	CmdPrefsSetLocal  = "workflow prefs set-local"
	CmdPrefsSetShared = "workflow prefs set-shared"

	CmdKGIngest          = "kg ingest"
	CmdKGLinkAdd         = "kg link add"
	CmdKGLinkRemove      = "kg link remove"
	CmdKGMaintainReweave = "kg maintain reweave"
	CmdKGMaintainStale   = "kg maintain mark-stale"
	CmdKGMaintainCompact = "kg maintain compact"
	CmdKGWarm            = "kg warm"

	CmdKGBuild       = "kg build"
	CmdKGUpdate      = "kg update"
	CmdKGPostprocess = "kg postprocess"

	CmdKGSync = "kg sync"

	CmdReviewApprove = "review approve"
	CmdReviewReject  = "review reject"
)

// Locus pins a tracked item to where its state currently lives (spec R8): a task
// can be "done in an open PR, not yet merged". Recovery must not conflate in-PR
// work with done-on-master or with fresh-eligible work, so the observed delta of a
// task-transition command carries the item's locus. Exactly one side is set.
type Locus struct {
	// Canonical is set when the change is landed on the canonical branch.
	Canonical *CanonicalRef `json:"canonical,omitempty"`
	// InOpenPR is set when the change lives only in an open, unmerged PR.
	InOpenPR *InOpenPRRef `json:"in_open_pr,omitempty"`
}

// CanonicalRef is a landed-on-canonical-branch reference, e.g. ref "master@<sha>".
type CanonicalRef struct {
	Ref string `json:"ref"`
}

// InOpenPRRef is a lives-in-an-open-PR reference: the PR number plus the task's
// status within that PR.
type InOpenPRRef struct {
	PR     int    `json:"pr"`
	Status string `json:"status"`
}

// Validate enforces Locus as a sum type: exactly one arm must be set. An absent
// (nil) Locus is valid — many transitions carry no locus — but a present Locus
// with both or neither arm set is rejected, so a caller can never produce a
// record that conflates in-PR with done-on-master (spec R8). NewEvent calls this
// at construction time, failing loudly rather than persisting an ambiguous locus.
func (l *Locus) Validate() error {
	if l == nil {
		return nil
	}
	set := 0
	if l.Canonical != nil {
		set++
	}
	if l.InOpenPR != nil {
		set++
	}
	if set != 1 {
		return fmt.Errorf("journal: locus must set exactly one of {canonical, in_open_pr}, got %d", set)
	}
	return nil
}

// locusCarrier is implemented by every Observed struct that carries a Locus, so
// NewEvent can validate the locus of any observed payload uniformly without a
// type switch over the concrete observed types.
type locusCarrier interface {
	ValidateLocus() error
}

// ValidateLocus implements locusCarrier for the task-transition observed deltas.
// A nil receiver (typed-nil observed) is valid; the embedded Locus is validated
// otherwise.
func (o *AdvanceObserved) ValidateLocus() error {
	if o == nil {
		return nil
	}
	return o.Locus.Validate()
}

// ValidateLocus implements locusCarrier for CloseTaskObserved.
func (o *CloseTaskObserved) ValidateLocus() error {
	if o == nil {
		return nil
	}
	return o.Locus.Validate()
}

// ValidateLocus implements locusCarrier for MergeBackObserved.
func (o *MergeBackObserved) ValidateLocus() error {
	if o == nil {
		return nil
	}
	return o.Locus.Validate()
}

// --- Tier 1: canonical transitions + irreversible FS moves -------------------

// AdvanceInput is `workflow advance`'s invoked flags.
type AdvanceInput struct {
	Plan        string `json:"plan"`
	Task        string `json:"task"`
	Status      string `json:"status,omitempty"`
	CommitState string `json:"commit_state,omitempty"`
}

// AdvanceObserved is `workflow advance`'s durable delta.
type AdvanceObserved struct {
	FromStatus string `json:"from_status"`
	ToStatus   string `json:"to_status"`
	Committed  bool   `json:"committed"`
	HeadSHA    string `json:"head_sha,omitempty"`
	Locus      *Locus `json:"locus,omitempty"`
}

// StartTaskInput is `workflow start-task`'s invoked flags.
type StartTaskInput struct {
	Plan        string   `json:"plan"`
	Task        string   `json:"task"`
	SeedSymbols []string `json:"seed_symbols,omitempty"`
	SeedPaths   []string `json:"seed_paths,omitempty"`
}

// StartTaskObserved is `workflow start-task`'s durable delta.
type StartTaskObserved struct {
	ToStatus    string `json:"to_status"`
	SidecarPath string `json:"sidecar_path,omitempty"`
	Committed   bool   `json:"committed"`
	HeadSHA     string `json:"head_sha,omitempty"`
}

// CloseTaskInput is `workflow close-task`'s invoked flags.
type CloseTaskInput struct {
	Plan      string `json:"plan"`
	Task      string `json:"task"`
	NextFocus string `json:"next_focus,omitempty"`
}

// CloseTaskObserved is `workflow close-task`'s durable delta.
type CloseTaskObserved struct {
	ToStatus     string `json:"to_status"`
	NextFocusSet bool   `json:"next_focus_set"`
	Committed    bool   `json:"committed"`
	HeadSHA      string `json:"head_sha,omitempty"`
	Locus        *Locus `json:"locus,omitempty"`
}

// PlanCreateInput is `workflow plan create`'s invoked flags.
type PlanCreateInput struct {
	Plan  string `json:"plan"`
	Title string `json:"title,omitempty"`
	Owner string `json:"owner,omitempty"`
}

// PlanCreateObserved is `workflow plan create`'s durable delta.
type PlanCreateObserved struct {
	PlanDir      string   `json:"plan_dir"`
	FilesCreated []string `json:"files_created,omitempty"`
}

// PlanArchiveInput is `workflow plan archive`'s invoked flags.
type PlanArchiveInput struct {
	Plans []string `json:"plans"`
	Force bool     `json:"force,omitempty"`
}

// PlanArchiveObserved is `workflow plan archive`'s durable delta.
type PlanArchiveObserved struct {
	ArchivePaths      []string `json:"archive_paths,omitempty"`
	ActiveDirsRemoved []string `json:"active_dirs_removed,omitempty"`
}

// FanoutInput is `workflow fanout`'s invoked flags.
type FanoutInput struct {
	Plan             string   `json:"plan"`
	Task             string   `json:"task"`
	WriteScope       []string `json:"write_scope,omitempty"`
	DelegateProfile  string   `json:"delegate_profile,omitempty"`
	BaseBranch       string   `json:"base_branch,omitempty"`
	VerifierSequence []string `json:"verifier_sequence,omitempty"`
}

// FanoutObserved is `workflow fanout`'s durable delta.
type FanoutObserved struct {
	DelegationPath     string   `json:"delegation_path"`
	BundlePath         string   `json:"bundle_path"`
	ResolvedBaseBranch string   `json:"resolved_base_branch,omitempty"`
	ResolvedWriteScope []string `json:"resolved_write_scope,omitempty"`
}

// MergeBackInput is `workflow merge-back`'s invoked flags.
type MergeBackInput struct {
	Task               string `json:"task"`
	Summary            string `json:"summary,omitempty"`
	VerificationStatus string `json:"verification_status,omitempty"`
	CommitState        string `json:"commit_state,omitempty"`
}

// MergeBackObserved is `workflow merge-back`'s durable delta.
type MergeBackObserved struct {
	ArtifactPath string   `json:"artifact_path"`
	FilesChanged []string `json:"files_changed,omitempty"`
	Verdict      string   `json:"verdict,omitempty"`
	Committed    bool     `json:"committed"`
	HeadSHA      string   `json:"head_sha,omitempty"`
	Locus        *Locus   `json:"locus,omitempty"`
}

// FoldBackInput is `workflow fold-back create`/`update`'s invoked flags.
type FoldBackInput struct {
	Plan        string `json:"plan"`
	Task        string `json:"task,omitempty"`
	Observation string `json:"observation"`
	Slug        string `json:"slug,omitempty"`
	Propose     bool   `json:"propose,omitempty"`
}

// FoldBackObserved is `workflow fold-back create`/`update`'s durable delta.
type FoldBackObserved struct {
	ArtifactID string   `json:"artifact_id"`
	RoutedTo   []string `json:"routed_to,omitempty"`
	Action     string   `json:"action"`
}

// DelegationCloseoutInput is `workflow delegation closeout`'s invoked flags.
type DelegationCloseoutInput struct {
	Plan     string `json:"plan"`
	Task     string `json:"task"`
	Decision string `json:"decision"`
	Note     string `json:"note,omitempty"`
}

// DelegationCloseoutObserved is `workflow delegation closeout`'s durable delta.
type DelegationCloseoutObserved struct {
	ArchivedPaths        []string `json:"archived_paths,omitempty"`
	ReconciledTaskStatus string   `json:"reconciled_task_status"`
}

// ContractCreateInput is `workflow contract create`'s invoked flags. Sibling of
// fanout: it persists a delegation contract for direct/delegated orchestrator
// work.
type ContractCreateInput struct {
	Plan       string   `json:"plan"`
	Task       string   `json:"task"`
	Owner      string   `json:"owner,omitempty"`
	WriteScope []string `json:"write_scope,omitempty"`
	Mode       string   `json:"mode,omitempty"`
	Force      bool     `json:"force,omitempty"`
}

// ContractCreateObserved is `workflow contract create`'s durable delta: the
// persisted contract's id/mode/status/path and the resolved write scope.
type ContractCreateObserved struct {
	ContractID         string   `json:"contract_id"`
	Mode               string   `json:"mode"`
	Status             string   `json:"status,omitempty"`
	ContractPath       string   `json:"contract_path,omitempty"`
	ResolvedWriteScope []string `json:"resolved_write_scope,omitempty"`
}

// DeriveScopeInput is `workflow plan derive-scope`'s invoked flags.
type DeriveScopeInput struct {
	Plan        string   `json:"plan"`
	Task        string   `json:"task"`
	SeedSymbols []string `json:"seed_symbols,omitempty"`
	SeedPaths   []string `json:"seed_paths,omitempty"`
}

// DeriveScopeObserved is `workflow plan derive-scope`'s durable delta: where the
// scope-evidence sidecar was written and a summary of what was derived — counts
// and the confidence label, not the evidence bodies (D4).
type DeriveScopeObserved struct {
	SidecarPath   string `json:"sidecar_path"`
	Mode          string `json:"mode,omitempty"`
	Confidence    string `json:"confidence,omitempty"`
	RequiredPaths int    `json:"required_paths"`
	Queries       int    `json:"queries"`
}

// VerifyRecordInput is `workflow verify record`'s invoked flags.
type VerifyRecordInput struct {
	Kind    string `json:"kind"`
	Status  string `json:"status"`
	Scope   string `json:"scope,omitempty"`
	Summary string `json:"summary,omitempty"`
	Task    string `json:"task,omitempty"`
}

// VerifyRecordObserved is `workflow verify record`'s durable delta.
type VerifyRecordObserved struct {
	VerificationLogID  string `json:"verification_log_id"`
	ResultArtifactPath string `json:"result_artifact_path,omitempty"`
}

// CheckpointInput is `workflow checkpoint`'s invoked flags.
type CheckpointInput struct {
	Message            string `json:"message"`
	VerificationStatus string `json:"verification_status,omitempty"`
	LogToIter          bool   `json:"log_to_iter,omitempty"`
}

// CheckpointObserved is `workflow checkpoint`'s durable delta.
type CheckpointObserved struct {
	CheckpointID string `json:"checkpoint_id"`
	IterStubPath string `json:"iter_stub_path,omitempty"`
}

// CommitInput is `workflow commit`'s invoked flags.
type CommitInput struct {
	Includes []string `json:"includes,omitempty"`
	DryRun   bool     `json:"dry_run,omitempty"`
}

// CommitObserved is `workflow commit`'s durable delta.
type CommitObserved struct {
	StagedPaths []string `json:"staged_paths,omitempty"`
	HeadSHA     string   `json:"head_sha,omitempty"`
	Noop        bool     `json:"noop"`
}

// OrphanAction is one resolution within an `archive-orphans` run.
type OrphanAction struct {
	Artifact   string `json:"artifact"`
	Class      string `json:"class"`
	Resolution string `json:"resolution"`
	DestPath   string `json:"dest_path,omitempty"`
}

// ArchiveOrphansInput is `workflow archive-orphans`'s invoked flags.
type ArchiveOrphansInput struct {
	DryRun bool `json:"dry_run,omitempty"`
}

// ArchiveOrphansObserved is `workflow archive-orphans`'s durable delta.
type ArchiveOrphansObserved struct {
	Actions []OrphanAction `json:"actions,omitempty"`
}

// SweepFix is one applied fix within a `sweep --apply` run.
type SweepFix struct {
	Project string `json:"project"`
	Action  string `json:"action"`
}

// SweepApplyInput is `workflow sweep --apply`'s invoked flags.
type SweepApplyInput struct {
	StaleDays    int `json:"stale_days,omitempty"`
	ProposalDays int `json:"proposal_days,omitempty"`
}

// SweepApplyObserved is `workflow sweep --apply`'s durable delta.
type SweepApplyObserved struct {
	FixesApplied []SweepFix `json:"fixes_applied,omitempty"`
}

// --- Tier 2: delta only (snapshot covers the full content) -------------------

// TaskAddInput is `workflow task add`'s invoked flags.
type TaskAddInput struct {
	Plan       string   `json:"plan"`
	TaskID     string   `json:"task_id"`
	Title      string   `json:"title,omitempty"`
	DependsOn  []string `json:"depends_on,omitempty"`
	WriteScope []string `json:"write_scope,omitempty"`
	AppType    string   `json:"app_type,omitempty"`
}

// TaskAddObserved is `workflow task add`'s durable delta.
type TaskAddObserved struct {
	Appended bool `json:"appended"`
}

// changedFieldHashLen is how many hex chars of the value SHA-256 a FieldDelta
// keeps. 12 hex chars (48 bits) is enough for the recovery view to detect whether
// a recorded change still matches the current file value, while never storing the
// value itself.
const changedFieldHashLen = 12

// FieldDelta is the bounded record of one changed Tier-2 field: its name plus
// metadata about the new value — its byte length and a short SHA-256 prefix —
// but NEVER the value itself (spec D4: Tier-2 journals the delta, not bodies).
//
// Its fields are UNEXPORTED so the no-bodies invariant is structural, not by
// convention: no caller in any other package can construct a FieldDelta literal
// and stuff a raw value into it. The only populating path is NewChangedFields,
// which hashes and discards the raw value. The wire format (name/len/sha256) is
// preserved by the custom Marshal/UnmarshalJSON, and the values are exposed
// read-only via Name/Len/SHA256 so the recovery view can re-verify against the
// canonical file (TASKS.yaml/PLAN.yaml/prefs).
type FieldDelta struct {
	name   string
	length int
	sha    string
}

// Name is the changed field's name.
func (f FieldDelta) Name() string { return f.name }

// Len is the byte length of the new value.
func (f FieldDelta) Len() int { return f.length }

// SHA256 is the short SHA-256 prefix of the new value (changedFieldHashLen hex
// chars), used to detect whether the journaled change is still the current value.
func (f FieldDelta) SHA256() string { return f.sha }

// fieldDeltaWire is the stable JSON shape for FieldDelta. It mirrors the (now
// unexported) struct fields so the wire format is unchanged while the in-memory
// type stays un-stuffable.
type fieldDeltaWire struct {
	Name   string `json:"name"`
	Len    int    `json:"len"`
	SHA256 string `json:"sha256,omitempty"`
}

// MarshalJSON emits the stable name/len/sha256 wire shape.
func (f FieldDelta) MarshalJSON() ([]byte, error) {
	return json.Marshal(fieldDeltaWire{Name: f.name, Len: f.length, SHA256: f.sha})
}

// UnmarshalJSON decodes the wire shape back into the unexported fields, so the
// recovery view can read a journaled FieldDelta without a re-export.
func (f *FieldDelta) UnmarshalJSON(data []byte) error {
	var w fieldDeltaWire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	f.name = w.Name
	f.length = w.Len
	f.sha = w.SHA256
	return nil
}

// ChangedFields is the bounded set of fields a Tier-2 command changed. It is a
// slice of FieldDelta (metadata only) rather than a name→value map, and FieldDelta
// has no exported field, so a body cannot be smuggled into the journal even by a
// careless emitter (R6/D4). Build it only via NewChangedFields.
type ChangedFields []FieldDelta

// NewChangedFields reduces a name→raw-value map to bounded per-field metadata
// (length + SHA-256 prefix), discarding the raw values. Output is sorted by field
// name for a deterministic record. An empty input yields a nil ChangedFields.
func NewChangedFields(raw map[string]string) ChangedFields {
	if len(raw) == 0 {
		return nil
	}
	names := make([]string, 0, len(raw))
	for name := range raw {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make(ChangedFields, 0, len(names))
	for _, name := range names {
		sum := sha256.Sum256([]byte(raw[name]))
		out = append(out, FieldDelta{
			name:   name,
			length: len(raw[name]),
			sha:    hex.EncodeToString(sum[:])[:changedFieldHashLen],
		})
	}
	return out
}

// DeltaInput is the shared invoked-flags shape for the field-replacing Tier-2
// commands (task update, plan update, prefs set-local/set-shared).
type DeltaInput struct {
	Plan          string        `json:"plan,omitempty"`
	Task          string        `json:"task,omitempty"`
	ChangedFields ChangedFields `json:"changed_fields,omitempty"`
}

// DeltaObserved is the shared durable delta for the field-replacing Tier-2
// commands: the names of the fields actually replaced (no-ops record nothing).
type DeltaObserved struct {
	FieldsReplaced []string `json:"fields_replaced,omitempty"`
}

// --- KG: counts / ids / decisions, never bodies (D4) -------------------------

// KGIngestInput is `kg ingest`'s invoked flags.
type KGIngestInput struct {
	File string `json:"file,omitempty"`
	All  bool   `json:"all,omitempty"`
	Type string `json:"type,omitempty"`
}

// KGIngestObserved is `kg ingest`'s durable delta — counts + ids, not bodies.
type KGIngestObserved struct {
	NotesCreated int      `json:"notes_created"`
	NotesUpdated int      `json:"notes_updated"`
	NoteIDs      []string `json:"note_ids,omitempty"`
}

// KGContentDeltaInput is the shared invoked-flags shape for the KG content-delta
// commands (link add/remove, maintain reweave|mark-stale|compact, warm).
// Operation names the concrete sub-command; Targets are the file/note/link ids
// the invocation named.
type KGContentDeltaInput struct {
	Operation string   `json:"operation"`
	Targets   []string `json:"targets,omitempty"`
}

// KGContentDeltaObserved is the shared durable delta for KG content-delta
// commands: counts keyed by what changed plus the affected ids — ids not bodies.
type KGContentDeltaObserved struct {
	Counts map[string]int `json:"counts,omitempty"`
	IDs    []string       `json:"ids,omitempty"`
}

// KGDecisionInput is the shared invoked-flags shape for the KG decision-event
// commands (build, update, postprocess).
type KGDecisionInput struct {
	Repo string `json:"repo,omitempty"`
	Base string `json:"base,omitempty"`
}

// KGDecisionObserved is the shared durable delta for KG decision-event commands:
// the outcome plus optional graph counts — "rebuilt at base X", never node/edge
// dumps.
type KGDecisionObserved struct {
	Outcome string `json:"outcome"`
	Nodes   *int   `json:"nodes,omitempty"`
	Edges   *int   `json:"edges,omitempty"`
	Files   *int   `json:"files,omitempty"`
}

// KGSyncInput is `kg sync`'s invoked flags.
type KGSyncInput struct {
	Push bool `json:"push,omitempty"`
}

// KGSyncObserved is `kg sync`'s durable delta. kg sync moves a git remote and is
// not locally snapshot-recoverable, so it is journaled fully.
type KGSyncObserved struct {
	PullStatus string `json:"pull_status,omitempty"`
	PushStatus string `json:"push_status,omitempty"`
	HeadSHA    string `json:"head_sha,omitempty"`
}

// --- Review: decision outcomes -----------------------------------------------

// ReviewInput is `review approve`/`reject`'s invoked flags.
type ReviewInput struct {
	ProposalID string `json:"proposal_id"`
	Reason     string `json:"reason,omitempty"`
}

// ReviewObserved is `review approve`/`reject`'s durable delta.
type ReviewObserved struct {
	Decision         string `json:"decision"`
	Applied          bool   `json:"applied"`
	ArchivedPath     string `json:"archived_path,omitempty"`
	RefreshTriggered bool   `json:"refresh_triggered"`
}

// CommandSpec is one registry entry: the canonical command, its Tier, and
// factories that allocate fresh typed Input/Observed values. The factories let
// the recovery view decode an event's opaque payloads back into the right type
// without a type switch, and let tests round-trip every schema generically.
type CommandSpec struct {
	command     string
	tier        Tier
	newInput    func() any
	newObserved func() any
}

// Command is the canonical command name.
func (s CommandSpec) Command() string { return s.command }

// Tier is the command's tier classification.
func (s CommandSpec) Tier() Tier { return s.tier }

// EventType is the envelope event_type the emit wiring stamps for this command:
// a Tier-2 delta is input_only (observed carries only changed fields); every
// other tier records a durable_delta. A failed invocation overrides this with
// EventFailed via NewFailedEvent.
func (s CommandSpec) EventType() EventType {
	if s.tier == TierDelta {
		return EventInputOnly
	}
	return EventDurableDelta
}

// NewInput allocates a fresh, zero-valued pointer to this command's Input type.
func (s CommandSpec) NewInput() any { return s.newInput() }

// NewObserved allocates a fresh, zero-valued pointer to this command's Observed
// type.
func (s CommandSpec) NewObserved() any { return s.newObserved() }

// newSpec builds a CommandSpec whose factories allocate the generic Input/Observed
// types I and O. One generic constructor keeps every registry entry to a single
// line and the factory closures defined in exactly one place.
func newSpec[I, O any](command string, tier Tier) CommandSpec {
	return CommandSpec{
		command:     command,
		tier:        tier,
		newInput:    func() any { return new(I) },
		newObserved: func() any { return new(O) },
	}
}

// registry is the single source of truth for the journaled command set. A command
// absent here is not journaled — that is how the spec's Excluded set is expressed:
// by omission, asserted in tests. The exclusions are intentional, not oversights:
//
//   - config (`refresh`, `config explain|verify|relevance`) — D4: the deterministic
//     snapshot re-reads the `.agentsrc.lock` (inputs_digest + per-layer resolved
//     sha), so config state is snapshot-redundant; recovery is "re-run da refresh".
//   - hook-sentinel / hook-outcome and `workflow delegation gate` — intra-turn gate
//     plumbing/telemetry with their own stores; not canonical workflow state.
//   - score — recomputable from iter-logs.
//   - `workflow drift` — writes only drift-report.json, a recomputable DERIVED
//     cache (re-run drift to regenerate), not canonical workflow state; excluded
//     for the same reason as score, not because it mutates nothing.
//   - `kg setup` — first-run provisioning, skipped after; not a routine mutation.
//   - read-only commands (status, orient, log, plan show|graph|schedule|
//     check-scope, complete, bundle stages, kg lint|stats|query|…) mutate nothing.
//
// Two state-mutating commands beyond the spec's Representative block are journaled
// here after a re-scan of the command trees: `workflow contract create` (persists a
// delegation contract; Tier-1 sibling of fanout) and `workflow plan derive-scope`
// (writes a scope-evidence sidecar; Tier-1, recording counts not bodies).
var registry = buildRegistry()

func buildRegistry() map[string]CommandSpec {
	specs := []CommandSpec{
		// Tier 1 — unconditional durable deltas.
		newSpec[AdvanceInput, AdvanceObserved](CmdAdvance, TierUnconditional),
		newSpec[StartTaskInput, StartTaskObserved](CmdStartTask, TierUnconditional),
		newSpec[CloseTaskInput, CloseTaskObserved](CmdCloseTask, TierUnconditional),
		newSpec[PlanCreateInput, PlanCreateObserved](CmdPlanCreate, TierUnconditional),
		newSpec[PlanArchiveInput, PlanArchiveObserved](CmdPlanArchive, TierUnconditional),
		newSpec[MergeBackInput, MergeBackObserved](CmdMergeBack, TierUnconditional),
		newSpec[FanoutInput, FanoutObserved](CmdFanout, TierUnconditional),
		newSpec[FoldBackInput, FoldBackObserved](CmdFoldBackCreate, TierUnconditional),
		newSpec[FoldBackInput, FoldBackObserved](CmdFoldBackUpdate, TierUnconditional),
		newSpec[DelegationCloseoutInput, DelegationCloseoutObserved](CmdDelegationCloseout, TierUnconditional),
		newSpec[ContractCreateInput, ContractCreateObserved](CmdContractCreate, TierUnconditional),
		newSpec[DeriveScopeInput, DeriveScopeObserved](CmdPlanDeriveScope, TierUnconditional),
		newSpec[VerifyRecordInput, VerifyRecordObserved](CmdVerifyRecord, TierUnconditional),
		newSpec[CheckpointInput, CheckpointObserved](CmdCheckpoint, TierUnconditional),
		newSpec[CommitInput, CommitObserved](CmdCommit, TierUnconditional),
		newSpec[ArchiveOrphansInput, ArchiveOrphansObserved](CmdArchiveOrphans, TierUnconditional),
		newSpec[SweepApplyInput, SweepApplyObserved](CmdSweepApply, TierUnconditional),

		// Tier 2 — changed-fields-only deltas.
		newSpec[TaskAddInput, TaskAddObserved](CmdTaskAdd, TierDelta),
		newSpec[DeltaInput, DeltaObserved](CmdTaskUpdate, TierDelta),
		newSpec[DeltaInput, DeltaObserved](CmdPlanUpdate, TierDelta),
		newSpec[DeltaInput, DeltaObserved](CmdPrefsSetLocal, TierDelta),
		newSpec[DeltaInput, DeltaObserved](CmdPrefsSetShared, TierDelta),

		// KG — counts / ids / decisions, never bodies.
		newSpec[KGIngestInput, KGIngestObserved](CmdKGIngest, TierKG),
		newSpec[KGContentDeltaInput, KGContentDeltaObserved](CmdKGLinkAdd, TierKG),
		newSpec[KGContentDeltaInput, KGContentDeltaObserved](CmdKGLinkRemove, TierKG),
		newSpec[KGContentDeltaInput, KGContentDeltaObserved](CmdKGMaintainReweave, TierKG),
		newSpec[KGContentDeltaInput, KGContentDeltaObserved](CmdKGMaintainStale, TierKG),
		newSpec[KGContentDeltaInput, KGContentDeltaObserved](CmdKGMaintainCompact, TierKG),
		newSpec[KGContentDeltaInput, KGContentDeltaObserved](CmdKGWarm, TierKG),
		newSpec[KGDecisionInput, KGDecisionObserved](CmdKGBuild, TierKG),
		newSpec[KGDecisionInput, KGDecisionObserved](CmdKGUpdate, TierKG),
		newSpec[KGDecisionInput, KGDecisionObserved](CmdKGPostprocess, TierKG),
		newSpec[KGSyncInput, KGSyncObserved](CmdKGSync, TierKG),

		// Review — decision outcomes.
		newSpec[ReviewInput, ReviewObserved](CmdReviewApprove, TierReview),
		newSpec[ReviewInput, ReviewObserved](CmdReviewReject, TierReview),
	}
	m := make(map[string]CommandSpec, len(specs))
	for _, s := range specs {
		m[s.command] = s
	}
	return m
}

// Lookup returns the registry entry for a canonical command name. The second
// return is false when the command is not journaled (the spec's Excluded set).
func Lookup(command string) (CommandSpec, bool) {
	spec, ok := registry[command]
	return spec, ok
}

// IsJournaled reports whether a command appends a journal event.
func IsJournaled(command string) bool {
	_, ok := registry[command]
	return ok
}

// NewEvent builds an Envelope for a journaled command from its typed input and
// observed payloads, stamping the EventType from the command's tier. It is the
// bridge the emit wiring uses to carry typed payloads through the opaque envelope:
// the typed values are marshaled into the envelope's RawMessage fields and tagged
// with the command. A nil (or typed-nil) payload is omitted. The remaining
// envelope fields (schema, version, ts, seq, actor, cwd_repo) are stamped by Emit.
//
// It returns an error for a command that is not journaled, and for an observed
// payload whose Locus is invalid (R8 sum-type: exactly one arm), so a miswired
// caller fails loudly at construction rather than silently dropping the event or
// persisting an ambiguous locus.
func NewEvent(command string, actor Actor, input, observed any) (Envelope, error) {
	spec, ok := Lookup(command)
	if !ok {
		return Envelope{}, fmt.Errorf("journal: command %q is not journaled", command)
	}
	if lc, ok := observed.(locusCarrier); ok {
		if err := lc.ValidateLocus(); err != nil {
			return Envelope{}, fmt.Errorf("journal: invalid observed for %q: %w", command, err)
		}
	}
	rawInput, err := marshalPayload(input)
	if err != nil {
		return Envelope{}, fmt.Errorf("journal: marshal input for %q: %w", command, err)
	}
	rawObserved, err := marshalPayload(observed)
	if err != nil {
		return Envelope{}, fmt.Errorf("journal: marshal observed for %q: %w", command, err)
	}
	return Envelope{
		Actor:     actor,
		Command:   command,
		EventType: spec.EventType(),
		Input:     rawInput,
		Observed:  rawObserved,
	}, nil
}

// NewFailedEvent builds an EventFailed envelope for a journaled command that did
// NOT succeed. Per R1 a failure never carries a fabricated observed delta, so
// only the invoked input is recorded; observed is always absent.
func NewFailedEvent(command string, actor Actor, input any) (Envelope, error) {
	if !IsJournaled(command) {
		return Envelope{}, fmt.Errorf("journal: command %q is not journaled", command)
	}
	rawInput, err := marshalPayload(input)
	if err != nil {
		return Envelope{}, fmt.Errorf("journal: marshal input for %q: %w", command, err)
	}
	return Envelope{
		Actor:     actor,
		Command:   command,
		EventType: EventFailed,
		Input:     rawInput,
	}, nil
}

// marshalPayload marshals a typed payload to a RawMessage, collapsing both a nil
// interface and a typed-nil pointer (which json renders as "null") to an absent
// (nil) payload so the omitempty envelope field drops out cleanly.
func marshalPayload(v any) (json.RawMessage, error) {
	if v == nil {
		return nil, nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if string(data) == "null" {
		return nil, nil
	}
	return json.RawMessage(data), nil
}
