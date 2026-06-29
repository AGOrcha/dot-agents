package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/AGOrcha/dot-agents/internal/agentslock"
	"github.com/AGOrcha/dot-agents/internal/fsops"
	"github.com/AGOrcha/dot-agents/internal/journal"
	"github.com/spf13/cobra"
	"golang.org/x/sys/execabs"
)

// journal.go is the user-facing CLI surface for the session-handoff journal
// (internal/journal): `da workflow journal <subcommand>`. The journal engine —
// the append-only event log, the deterministic snapshot, and the verified
// recovery view — is all merged in internal/journal and frozen; this file only
// exposes it and supplies the REAL verification sources that the recovery layer
// (internal/journal/recover.go) left as injectable seams (Deps.Sources).
//
// Subcommands:
//
//   - snapshot — capture the deterministic live-state snapshot (journal.Snapshot).
//   - recover  — build the VERIFIED recovery view (journal.RecoveryView) with the
//     real gh/git verification sources wired in (the meaty part); render the
//     verified/changed/missing/unverified items, the trust gradient, the
//     quarantined conflicts, and the canonical-vs-in-PR locus with enriched coords.
//   - show     — display the current snapshot + the recent event log, bounded.
//   - prune    — drop old events past a bounded retention (safe, atomic).
//   - append   — low-level manual append entrypoint (reasoned-overlay / testing).
//
// # Verification sources (the p5-seam implementation)
//
// recover.go re-verifies each reconstructed item against an ORDERED list of
// VerificationSources (R7: authoritative store/service-backed first, local
// fallback last) and ENRICHES the snapshot's placeholder locus coordinates (PR 0,
// the "canonical" sentinel ref) from a source's resolved reality. This file wires
// the two production sources:
//
//   - ghSource (authoritative): resolves PR state/number + merge sha via gh. Open
//     PRs come through the EXISTING event.pr.* producer seam (defaultPRSourceLister,
//     reused from base-resolution); merged PRs + their merge sha come from a narrow
//     `gh pr list --state merged` read (the existing producer lists only OPEN PRs,
//     so a merged task's merge sha had no pre-existing seam). A gh/network failure
//     makes the source unavailable so recovery falls through to the local fallback.
//   - localSource (non-authoritative): confirms a task's existence + canonical
//     status from the live TASKS.yaml when gh is unavailable. It corroborates the
//     locus ARM and status but never resolves authoritative coords (medium trust).
//
// Task→PR matching reuses the existing bounded branchMatchesTask token rule, so a
// task id never resolves a sibling's PR.

const (
	// journalSourceGH / journalSourceLocal name the two verification sources in the
	// recovery view (RecoveredItem.VerifiedBy). Hoisted so each appears once.
	journalSourceGH    = "gh"
	journalSourceLocal = "local"

	// defaultJournalShowLimit bounds `journal show` to the most recent events so a
	// long log stays readable; --all lifts the cap.
	defaultJournalShowLimit = 20

	// defaultJournalKeep is the retention `journal prune` keeps by default: the
	// newest N events survive, older ones are dropped. The journal records short
	// workflow metadata, so a four-figure default spans many sessions.
	defaultJournalKeep = 1000
)

// --- cobra wiring ------------------------------------------------------------

func newWorkflowJournalCmd() *cobra.Command {
	journalCmd := &cobra.Command{
		Use:   "journal",
		Short: "Inspect and recover the session-handoff journal",
		Long: `The session-handoff journal is an append-only, crash-survivable event log
plus a deterministic live-state snapshot, kept off the git tree under the XDG
state directory. It lets a session resumed after a compaction or crash re-inject
state from durable file state, re-verified against current reality, rather than
re-grounding from scratch.`,
		Example: deps.ExampleBlock(
			"  da workflow journal snapshot",
			"  da workflow journal recover",
			"  da workflow journal show",
			"  da --json workflow journal recover",
		),
	}
	journalCmd.AddCommand(
		newWorkflowJournalSnapshotCmd(),
		newWorkflowJournalRecoverCmd(),
		newWorkflowJournalShowCmd(),
		newWorkflowJournalPruneCmd(),
		newWorkflowJournalAppendCmd(),
	)
	return journalCmd
}

func newWorkflowJournalSnapshotCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "snapshot",
		Short: "Capture the deterministic live-state snapshot for the current project",
		Example: deps.ExampleBlock(
			"  da workflow journal snapshot",
			"  da --json workflow journal snapshot",
		),
		Args: deps.NoArgsWithHints("`da workflow journal snapshot` works on the current repository."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowJournalSnapshot(cmd.OutOrStdout())
		},
	}
}

func newWorkflowJournalRecoverCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "recover",
		Short: "Build the verified recovery view (snapshot + replay, re-verified against reality)",
		Example: deps.ExampleBlock(
			"  da workflow journal recover",
			"  da --json workflow journal recover",
		),
		Args: deps.NoArgsWithHints("`da workflow journal recover` works on the current repository."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowJournalRecover(cmd.OutOrStdout())
		},
	}
}

func newWorkflowJournalShowCmd() *cobra.Command {
	var (
		limit int
		all   bool
	)
	showCmd := &cobra.Command{
		Use:   "show",
		Short: "Show the current snapshot and recent journal events",
		Example: deps.ExampleBlock(
			"  da workflow journal show",
			"  da workflow journal show --limit 50",
			"  da workflow journal show --all",
		),
		Args: deps.NoArgsWithHints("Use `--limit` or `--all` instead of positional arguments."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowJournalShow(cmd.OutOrStdout(), limit, all)
		},
	}
	showCmd.Flags().IntVar(&limit, "limit", defaultJournalShowLimit, "Show at most N most-recent events")
	showCmd.Flags().BoolVar(&all, "all", false, "Show every event (overrides --limit)")
	return showCmd
}

func newWorkflowJournalPruneCmd() *cobra.Command {
	var keep int
	pruneCmd := &cobra.Command{
		Use:   "prune",
		Short: "Drop journal events beyond a bounded retention (safe, atomic)",
		Example: deps.ExampleBlock(
			"  da workflow journal prune",
			"  da workflow journal prune --keep 200",
			"  da -n workflow journal prune --keep 200",
		),
		Args: deps.NoArgsWithHints("Use `--keep` instead of positional arguments."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowJournalPrune(cmd.OutOrStdout(), keep)
		},
	}
	pruneCmd.Flags().IntVar(&keep, "keep", defaultJournalKeep, "Keep the newest N events; drop the rest")
	return pruneCmd
}

func newWorkflowJournalAppendCmd() *cobra.Command {
	var in journalAppendInput
	appendCmd := &cobra.Command{
		Use:   "append",
		Short: "Low-level: append one event to the journal (reasoned-overlay / testing)",
		Long: `Appends a single raw event to the journal event log. This is a low-level
entrypoint that bypasses the typed per-command schemas (it writes through the raw
Emit path, which the systemic no-bodies size cap still guards); it is intended
for the reasoned-delta overlay and for testing, not routine workflow mutation.`,
		Example: deps.ExampleBlock(
			"  da workflow journal append --command \"workflow advance\" --actor main",
			"  da workflow journal append --command \"workflow advance\" --input '{\"plan\":\"p\",\"task\":\"t\"}'",
		),
		Args: deps.NoArgsWithHints("Use `--command` and related flags instead of positional arguments."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowJournalAppend(cmd.OutOrStdout(), in)
		},
	}
	appendCmd.Flags().StringVar(&in.Command, "command", "", "Canonical command name to record (required)")
	appendCmd.Flags().StringVar(&in.Actor, "actor", string(journal.ActorMain), "Actor role: main|loop-worker|orchestrator")
	appendCmd.Flags().StringVar(&in.EventType, "event-type", string(journal.EventDurableDelta), "Event type: durable_delta|input_only|failed")
	appendCmd.Flags().StringVar(&in.Input, "input", "", "Invoked-flags payload as a JSON object")
	appendCmd.Flags().StringVar(&in.Observed, "observed", "", "Observed-delta payload as a JSON object")
	_ = appendCmd.MarkFlagRequired("command")
	return appendCmd
}

// --- snapshot ----------------------------------------------------------------

// journalSnapshot is the capture seam (= journal.Snapshot), overridable in tests
// so they can drive the write-failure branch without staging filesystem faults.
var journalSnapshot = journal.Snapshot

func runWorkflowJournalSnapshot(out io.Writer) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	snap, err := journalSnapshot(project.Path)
	if err != nil {
		return fmt.Errorf("journal snapshot: %w", err)
	}
	if deps.Flags.JSON() {
		return emitJournalJSON(out, snap)
	}
	renderJournalSnapshot(out, project.Path, snap)
	return nil
}

func renderJournalSnapshot(out io.Writer, repoPath string, snap journal.SnapshotState) {
	fmt.Fprintf(out, "journal snapshot captured at %s\n", snap.CapturedAt)
	fmt.Fprintf(out, "  path:        %s\n", journal.SnapshotPath(repoPath))
	fmt.Fprintf(out, "  identity:    %s\n", snap.Identity.Fingerprint)
	fmt.Fprintf(out, "  plans:       %d (%d task(s))\n", len(snap.Plans), countSnapshotTasks(snap))
	fmt.Fprintf(out, "  unblocked:   %d pending task(s) ready\n", len(snap.PendingUnblocked))
	fmt.Fprintf(out, "  delegations: %d in-flight\n", len(snap.Delegations))
	fmt.Fprintf(out, "  merge-backs: %d awaiting integration\n", len(snap.PendingMergeBacks))
}

func countSnapshotTasks(snap journal.SnapshotState) int {
	n := 0
	for _, p := range snap.Plans {
		n += len(p.Tasks)
	}
	return n
}

// --- recover -----------------------------------------------------------------

// journalRecoveryView and journalVerificationSources are the recover seams: the
// view builder (= journal.RecoveryView) and the ordered source list factory, both
// overridable so recover tests inject fake sources without a gh binary or network.
var (
	journalRecoveryView        = journal.RecoveryView
	journalVerificationSources = defaultJournalVerificationSources
)

func runWorkflowJournalRecover(out io.Writer) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	result, err := journalRecoveryView(project.Path, journal.Deps{
		Sources: journalVerificationSources(project.Path),
		// The reasoned-delta overlay writer (reasoned.log) is a separate, not-yet-
		// built task; until it lands, the freshest durable activity signal we have
		// is the newest event in the log, used here as the D10 freshness proxy so a
		// freshly-journaled session is not spuriously quarantined as orphaned.
		LastReasonedWrite: latestJournalEventTS(project.Path),
	})
	if err != nil {
		return fmt.Errorf("journal recover: %w", err)
	}
	if deps.Flags.JSON() {
		return emitJournalJSON(out, result)
	}
	renderRecover(out, result)
	return nil
}

// defaultJournalVerificationSources builds the ordered re-verify probe list:
// authoritative gh first (R7), local fallback last. When the gh source cannot be
// constructed (gh/network down), it is omitted and only the local fallback runs —
// the construction-level half of the R7 fallback (the per-item half is
// ErrSourceUnavailable inside VerifyTask).
func defaultJournalVerificationSources(repoPath string) []journal.VerificationSource {
	sources := make([]journal.VerificationSource, 0, 2)
	if gh, err := newGHSource(repoPath, defaultPRSourceLister, ghMergedPRLister{}); err == nil {
		sources = append(sources, gh)
	}
	sources = append(sources, &localSource{repoPath: repoPath, load: loadCanonicalTasks})
	return sources
}

func renderRecover(out io.Writer, r journal.RecoveryResult) {
	fmt.Fprintf(out, "recovery view for %s\n", r.Identity.Fingerprint)
	if r.SnapshotAt != "" {
		fmt.Fprintf(out, "  snapshot:  %s\n", r.SnapshotAt)
	} else {
		fmt.Fprintln(out, "  snapshot:  (none — replay-only, degraded)")
	}
	fmt.Fprintf(out, "  freshness: %s\n", r.Freshness.Label)
	if r.Quarantined {
		fmt.Fprintf(out, "  QUARANTINED: %s\n", r.QuarantineReason)
	}
	renderRecoverItems(out, r.Items)
	renderRecoverConflicts(out, r.Conflicts)
	for _, n := range r.Notes {
		fmt.Fprintf(out, "  note: %s\n", n)
	}
}

// recoverStatusOrder is the fixed render order of the verification tags so the
// most-trustworthy buckets read first and output is stable.
var recoverStatusOrder = []journal.VerificationStatus{
	journal.StatusVerified,
	journal.StatusChanged,
	journal.StatusMissing,
	journal.StatusUnverified,
}

func renderRecoverItems(out io.Writer, items []journal.RecoveredItem) {
	if len(items) == 0 {
		fmt.Fprintln(out, "  (no recovered items)")
		return
	}
	for _, status := range recoverStatusOrder {
		bucket := itemsWithStatus(items, status)
		if len(bucket) == 0 {
			continue
		}
		fmt.Fprintf(out, "  %s (%d):\n", status, len(bucket))
		for _, it := range bucket {
			renderRecoverItem(out, it)
		}
	}
}

func itemsWithStatus(items []journal.RecoveredItem, status journal.VerificationStatus) []journal.RecoveredItem {
	out := make([]journal.RecoveredItem, 0, len(items))
	for _, it := range items {
		if it.Status == status {
			out = append(out, it)
		}
	}
	return out
}

func renderRecoverItem(out io.Writer, it journal.RecoveredItem) {
	fmt.Fprintf(out, "    - %s/%s [trust=%s", it.Key.Plan, it.Key.Task, it.Trust)
	if it.VerifiedBy != "" {
		fmt.Fprintf(out, " via %s", it.VerifiedBy)
	}
	fmt.Fprint(out, "]")
	if loc := describeItemLocus(it.Reconstructed.Locus); loc != "" {
		fmt.Fprintf(out, " %s", loc)
	}
	fmt.Fprintln(out)
	if it.Delta != "" {
		fmt.Fprintf(out, "      delta: %s\n", it.Delta)
	}
}

// describeItemLocus renders the canonical-vs-in-PR locus (with the coords the
// verification layer enriched) for the text view.
func describeItemLocus(l *journal.Locus) string {
	switch {
	case l == nil:
		return ""
	case l.Canonical != nil:
		return "canonical " + l.Canonical.Ref
	case l.InOpenPR != nil:
		return fmt.Sprintf("in_open_pr #%d", l.InOpenPR.PR)
	default:
		return ""
	}
}

func renderRecoverConflicts(out io.Writer, conflicts []journal.IdentityConflict) {
	if len(conflicts) == 0 {
		return
	}
	fmt.Fprintf(out, "  quarantined conflicts (%d):\n", len(conflicts))
	for _, c := range conflicts {
		fmt.Fprintf(out, "    - task %s: %s\n", c.Task, c.Reason)
	}
}

// --- show --------------------------------------------------------------------

// journalShowResult is the JSON shape `journal show --json` emits.
type journalShowResult struct {
	SnapshotPath string                 `json:"snapshot_path"`
	Snapshot     *journal.SnapshotState `json:"snapshot,omitempty"`
	EventsPath   string                 `json:"events_path"`
	EventCount   int                    `json:"event_count"`
	Events       []journal.Envelope     `json:"events"`
}

func runWorkflowJournalShow(out io.Writer, limit int, all bool) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	events, err := readJournalEvents(project.Path)
	if err != nil {
		return fmt.Errorf("journal show: %w", err)
	}
	sortEventsChrono(events)
	if !all {
		events = tailEvents(events, limit)
	}
	result := journalShowResult{
		SnapshotPath: journal.SnapshotPath(project.Path),
		Snapshot:     loadSnapshotOrNil(project.Path),
		EventsPath:   journal.EventsLogPath(project.Path),
		EventCount:   len(events),
		Events:       events,
	}
	if deps.Flags.JSON() {
		return emitJournalJSON(out, result)
	}
	renderJournalShow(out, result)
	return nil
}

// loadSnapshotOrNil returns the persisted snapshot, or nil when none exists yet
// (a never-snapshotted repo is a normal, non-error state for `show`).
func loadSnapshotOrNil(repoPath string) *journal.SnapshotState {
	snap, err := journal.LoadSnapshot(repoPath)
	if err != nil {
		return nil
	}
	return &snap
}

func renderJournalShow(out io.Writer, r journalShowResult) {
	if r.Snapshot != nil {
		fmt.Fprintf(out, "snapshot %s — %d plan(s), %d delegation(s), %d merge-back(s)\n",
			r.Snapshot.CapturedAt, len(r.Snapshot.Plans), len(r.Snapshot.Delegations), len(r.Snapshot.PendingMergeBacks))
	} else {
		fmt.Fprintln(out, "snapshot: (none captured yet)")
	}
	fmt.Fprintf(out, "events (%d, newest last):\n", r.EventCount)
	if len(r.Events) == 0 {
		fmt.Fprintln(out, "  (no events)")
		return
	}
	for _, e := range r.Events {
		fmt.Fprintf(out, "  %s  %-13s  %s [%s]\n", e.TS, e.EventType, e.Command, e.Actor)
	}
}

// --- prune -------------------------------------------------------------------

// journalPruneResult is the JSON shape `journal prune --json` emits.
type journalPruneResult struct {
	Path    string `json:"path"`
	Total   int    `json:"total"`
	Kept    int    `json:"kept"`
	Removed int    `json:"removed"`
	DryRun  bool   `json:"dry_run"`
}

func runWorkflowJournalPrune(out io.Writer, keep int) error {
	if keep < 0 {
		return deps.UsageError("--keep must be >= 0", "Pass --keep N to keep the newest N events.")
	}
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	result, err := pruneJournal(project.Path, keep, safeDryRun())
	if err != nil {
		return err
	}
	if deps.Flags.JSON() {
		return emitJournalJSON(out, result)
	}
	renderJournalPrune(out, result)
	return nil
}

// pruneJournal drops all but the newest keep events from the event log. The
// rewrite is atomic (temp-then-rename) under the journal's interprocess advisory
// lock — the same durability contract the appender uses — so a concurrent da
// process can never observe a torn log or race the rewrite. A dry run computes the
// result without touching the file.
func pruneJournal(repoPath string, keep int, dryRun bool) (journalPruneResult, error) {
	path := journal.EventsLogPath(repoPath)
	events, err := readJournalEvents(repoPath)
	if err != nil {
		return journalPruneResult{}, fmt.Errorf("journal prune: read events: %w", err)
	}
	sortEventsChrono(events)
	removed := 0
	kept := events
	if len(events) > keep {
		removed = len(events) - keep
		kept = events[removed:]
	}
	result := journalPruneResult{Path: path, Total: len(events), Kept: len(kept), Removed: removed, DryRun: dryRun}
	if removed == 0 || dryRun {
		return result, nil
	}
	if err := rewriteJournalEvents(path, kept); err != nil {
		return journalPruneResult{}, err
	}
	return result, nil
}

// rewriteJournalEvents replaces the event log with exactly events, atomically and
// under the package advisory lock (mirrors internal/journal's write discipline).
func rewriteJournalEvents(path string, events []journal.Envelope) (err error) {
	release, err := agentslock.AcquireFileLock(path)
	if err != nil {
		return fmt.Errorf("journal prune: lock %s: %w", path, err)
	}
	defer func() {
		if relErr := release(); relErr != nil && err == nil {
			err = fmt.Errorf("journal prune: release lock %s: %w", path, relErr)
		}
	}()
	var buf bytes.Buffer
	for _, e := range events {
		line, merr := e.MarshalLine()
		if merr != nil {
			return fmt.Errorf("journal prune: marshal event: %w", merr)
		}
		buf.Write(line)
	}
	if err := fsops.WriteFileAtomic(path, buf.Bytes()); err != nil {
		return fmt.Errorf("journal prune: write %s: %w", path, err)
	}
	return nil
}

func renderJournalPrune(out io.Writer, r journalPruneResult) {
	verb := "removed"
	if r.DryRun {
		verb = "would remove"
	}
	fmt.Fprintf(out, "journal prune: %d total, kept %d, %s %d\n", r.Total, r.Kept, verb, r.Removed)
	fmt.Fprintf(out, "  path: %s\n", r.Path)
}

// --- append ------------------------------------------------------------------

// journalAppendInput bundles the `journal append` flags.
type journalAppendInput struct {
	Command   string
	Actor     string
	EventType string
	Input     string
	Observed  string
}

// journalAppendResult is the JSON shape `journal append --json` emits.
type journalAppendResult struct {
	Appended  bool   `json:"appended"`
	Command   string `json:"command"`
	Actor     string `json:"actor"`
	EventType string `json:"event_type"`
	EventsLog string `json:"events_log"`
}

func runWorkflowJournalAppend(out io.Writer, in journalAppendInput) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	env, err := buildAppendEnvelope(in)
	if err != nil {
		return err
	}
	if err := journalEmit(project.Path, env); err != nil {
		return fmt.Errorf("journal append: %w", err)
	}
	result := journalAppendResult{
		Appended:  true,
		Command:   env.Command,
		Actor:     string(env.Actor),
		EventType: string(env.EventType),
		EventsLog: journal.EventsLogPath(project.Path),
	}
	if deps.Flags.JSON() {
		return emitJournalJSON(out, result)
	}
	fmt.Fprintf(out, "journal append: recorded %q [%s/%s] to %s\n",
		result.Command, result.Actor, result.EventType, result.EventsLog)
	return nil
}

// buildAppendEnvelope validates the flags and assembles a raw Envelope. It writes
// through the raw Emit path on purpose (the low-level entrypoint), so the typed
// per-command schemas are bypassed; the payloads are only checked to be valid JSON
// objects, and Emit's size cap remains the no-bodies backstop.
func buildAppendEnvelope(in journalAppendInput) (journal.Envelope, error) {
	if strings.TrimSpace(in.Command) == "" {
		return journal.Envelope{}, deps.UsageError("--command is required", "Pass --command with the canonical command name to record.")
	}
	actor, err := parseJournalActor(in.Actor)
	if err != nil {
		return journal.Envelope{}, err
	}
	eventType, err := parseJournalEventType(in.EventType)
	if err != nil {
		return journal.Envelope{}, err
	}
	rawInput, err := rawJSONPayload("input", in.Input)
	if err != nil {
		return journal.Envelope{}, err
	}
	rawObserved, err := rawJSONPayload("observed", in.Observed)
	if err != nil {
		return journal.Envelope{}, err
	}
	return journal.Envelope{
		Actor:     actor,
		Command:   in.Command,
		EventType: eventType,
		Input:     rawInput,
		Observed:  rawObserved,
	}, nil
}

func parseJournalActor(s string) (journal.Actor, error) {
	switch journal.Actor(s) {
	case journal.ActorMain, journal.ActorLoopWorker, journal.ActorOrchestrator:
		return journal.Actor(s), nil
	default:
		return "", deps.UsageError(
			fmt.Sprintf("invalid --actor %q", s),
			"Use one of: main, loop-worker, orchestrator.",
		)
	}
}

func parseJournalEventType(s string) (journal.EventType, error) {
	switch journal.EventType(s) {
	case journal.EventDurableDelta, journal.EventInputOnly, journal.EventFailed:
		return journal.EventType(s), nil
	default:
		return "", deps.UsageError(
			fmt.Sprintf("invalid --event-type %q", s),
			"Use one of: durable_delta, input_only, failed.",
		)
	}
}

// rawJSONPayload validates that an optional flag value is a JSON object and
// returns it as a RawMessage (nil when the flag was empty).
func rawJSONPayload(field, value string) (json.RawMessage, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	if !json.Valid([]byte(value)) {
		return nil, deps.UsageError(
			fmt.Sprintf("--%s must be valid JSON", field),
			fmt.Sprintf("Pass --%s a JSON object, for example '{\"plan\":\"p\"}'.", field),
		)
	}
	return json.RawMessage(value), nil
}

// --- gh verification source (authoritative) ----------------------------------

// mergedPR is a merged pull request's identity + merge commit sha — the canonical
// locus coordinate a completed task's placeholder ref is enriched with.
type mergedPR struct {
	Number   int
	Branch   string
	MergeSHA string
}

// mergedPRLister enumerates a project's merged PRs. This is a NEW seam: the
// existing event.pr.* producer (reused for OPEN PRs) lists only open PRs, so a
// merged task's merge sha has no pre-existing seam to reuse.
type mergedPRLister interface {
	ListMergedPRs(repoPath string) ([]mergedPR, error)
}

// ghJSON runs a gh CLI command in repoPath and returns stdout. It is the single
// seam through which the merged-PR read shells out, so tests never invoke a real
// gh binary — they override ghJSON (or inject a fake lister) instead.
var ghJSON = func(repoPath string, args ...string) ([]byte, error) {
	cmd := execabs.Command("gh", args...)
	cmd.Dir = repoPath
	return cmd.Output()
}

// ghMergedPRLister is the production mergedPRLister: one `gh pr list --state
// merged` read, mapping headRefName + mergeCommit.oid onto mergedPR.
type ghMergedPRLister struct{}

func (ghMergedPRLister) ListMergedPRs(repoPath string) ([]mergedPR, error) {
	out, err := ghJSON(repoPath, "pr", "list", "--state", "merged",
		"--json", "number,headRefName,mergeCommit", "--limit", "100")
	if err != nil {
		return nil, err
	}
	return parseMergedPRs(out)
}

// parseMergedPRs decodes the `gh pr list --state merged --json …` array.
func parseMergedPRs(data []byte) ([]mergedPR, error) {
	var raw []struct {
		Number      int    `json:"number"`
		HeadRefName string `json:"headRefName"`
		MergeCommit struct {
			OID string `json:"oid"`
		} `json:"mergeCommit"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("journal: decode merged PRs: %w", err)
	}
	out := make([]mergedPR, 0, len(raw))
	for _, r := range raw {
		out = append(out, mergedPR{Number: r.Number, Branch: r.HeadRefName, MergeSHA: r.MergeCommit.OID})
	}
	return out, nil
}

// ghSource is the authoritative VerificationSource: it re-verifies an item's
// locus against the live open + merged PR sets and enriches the placeholder coords
// (PR 0, the "canonical" sentinel ref) with the resolved PR number / merge sha.
type ghSource struct {
	open   []openPR
	merged []mergedPR
}

// newGHSource loads the open + merged PR sets once. A failure on either read makes
// the source unavailable (R7) — newGHSource returns the error so the caller omits
// it and recovery falls through to the local fallback.
func newGHSource(repoPath string, open prSourceLister, merged mergedPRLister) (*ghSource, error) {
	openPRs, err := open.ListOpenPRs(repoPath)
	if err != nil {
		return nil, err
	}
	mergedPRs, err := merged.ListMergedPRs(repoPath)
	if err != nil {
		return nil, err
	}
	return &ghSource{open: openPRs, merged: mergedPRs}, nil
}

func (*ghSource) Name() string        { return journalSourceGH }
func (*ghSource) Authoritative() bool { return true }

// VerifyTask resolves an item against gh by its reconstructed locus arm. A
// locus-less item (gh has only PR/locus truth, not workflow status) is deferred to
// the local fallback via ErrSourceUnavailable.
func (g *ghSource) VerifyTask(key journal.ItemKey, recon journal.ItemState) (journal.RealityCheck, error) {
	switch {
	case recon.Locus == nil:
		return journal.RealityCheck{}, journal.ErrSourceUnavailable
	case recon.Locus.InOpenPR != nil:
		return g.verifyInOpenPR(key), nil
	case recon.Locus.Canonical != nil:
		return g.verifyCanonical(key)
	default:
		return journal.RealityCheck{}, journal.ErrSourceUnavailable
	}
}

// verifyInOpenPR confirms an in-PR item: a matching open PR enriches the real PR
// number (verified); a matching merged PR means it landed since (the item changed
// to a canonical locus carrying the merge sha); neither means the PR is gone.
func (g *ghSource) verifyInOpenPR(key journal.ItemKey) journal.RealityCheck {
	if pr, ok := g.matchOpen(key.Task); ok {
		return journal.RealityCheck{Exists: true, Locus: &journal.Locus{InOpenPR: &journal.InOpenPRRef{PR: pr.Number}}}
	}
	if m, ok := g.matchMerged(key.Task); ok {
		return journal.RealityCheck{Exists: true, Locus: &journal.Locus{Canonical: &journal.CanonicalRef{Ref: m.MergeSHA}}}
	}
	return journal.RealityCheck{Exists: false}
}

// verifyCanonical confirms a done-on-canonical item by resolving its merge sha
// from the merged PR set. A merge that has aged out of the gh window is not
// "missing" — gh simply cannot see it, so the item is deferred to the local
// fallback via ErrSourceUnavailable.
func (g *ghSource) verifyCanonical(key journal.ItemKey) (journal.RealityCheck, error) {
	if m, ok := g.matchMerged(key.Task); ok {
		return journal.RealityCheck{Exists: true, Locus: &journal.Locus{Canonical: &journal.CanonicalRef{Ref: m.MergeSHA}}}, nil
	}
	return journal.RealityCheck{}, journal.ErrSourceUnavailable
}

func (g *ghSource) matchOpen(task string) (openPR, bool) {
	for _, pr := range g.open {
		if pr.Branch != "" && branchMatchesTask(pr.Branch, task) {
			return pr, true
		}
	}
	return openPR{}, false
}

func (g *ghSource) matchMerged(task string) (mergedPR, bool) {
	for _, pr := range g.merged {
		if pr.Branch != "" && branchMatchesTask(pr.Branch, task) {
			return pr, true
		}
	}
	return mergedPR{}, false
}

// --- local verification source (non-authoritative fallback) ------------------

// canonicalTaskLoader loads a plan's live TASKS.yaml; seam (= loadCanonicalTasks).
type canonicalTaskLoader func(repoPath, planID string) (*CanonicalTaskFile, error)

// localSource is the non-authoritative fallback: when gh is unavailable it
// confirms a task's existence + canonical status from the live TASKS.yaml. It
// corroborates the locus ARM and status (medium trust) but never resolves the
// authoritative PR/merge coordinates.
type localSource struct {
	repoPath string
	load     canonicalTaskLoader
}

func (*localSource) Name() string        { return journalSourceLocal }
func (*localSource) Authoritative() bool { return false }

func (s *localSource) VerifyTask(key journal.ItemKey, _ journal.ItemState) (journal.RealityCheck, error) {
	tf, err := s.load(s.repoPath, key.Plan)
	if err != nil {
		if os.IsNotExist(err) {
			return journal.RealityCheck{Exists: false}, nil // plan gone → task missing
		}
		return journal.RealityCheck{}, journal.ErrSourceUnavailable
	}
	for _, t := range tf.Tasks {
		if t.ID == key.Task {
			return journal.RealityCheck{Exists: true, Status: t.Status, Locus: localLocusForStatus(t.Status)}, nil
		}
	}
	return journal.RealityCheck{Exists: false}, nil
}

// localLocusForStatus mirrors the snapshot's status→locus arm mapping but with
// placeholder coords: the local source confirms the locus ARM from the live status
// (so canonical-vs-in-PR is honored) without resolving authoritative coordinates.
func localLocusForStatus(status string) *journal.Locus {
	switch status {
	case TaskStatusCompleted:
		return &journal.Locus{Canonical: &journal.CanonicalRef{}}
	case TaskStatusAwaitingOwnerReview:
		return &journal.Locus{InOpenPR: &journal.InOpenPRRef{}}
	default:
		return nil
	}
}

// --- shared helpers ----------------------------------------------------------

// readJournalEvents reads and decodes every well-formed line of the event log. A
// missing log is an empty (non-error) result; a torn/malformed line is skipped,
// mirroring the recovery layer's tolerant replay.
func readJournalEvents(repoPath string) ([]journal.Envelope, error) {
	data, err := os.ReadFile(journal.EventsLogPath(repoPath))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var events []journal.Envelope
	for _, line := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var e journal.Envelope
		if json.Unmarshal(line, &e) == nil {
			events = append(events, e)
		}
	}
	return events, nil
}

// sortEventsChrono orders events for display/retention by parsed timestamp, then
// the monotonic Seq — the same chronological order the recovery replay uses (an
// unparseable timestamp sorts last). The sort is stable for reproducibility.
func sortEventsChrono(events []journal.Envelope) {
	sort.SliceStable(events, func(i, j int) bool {
		ti, tj := parseEventTS(events[i].TS), parseEventTS(events[j].TS)
		if ti.IsZero() != tj.IsZero() {
			return tj.IsZero()
		}
		if !ti.IsZero() && !ti.Equal(tj) {
			return ti.Before(tj)
		}
		return events[i].Seq < events[j].Seq
	})
}

func parseEventTS(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// tailEvents returns the newest limit events (the input must already be in
// chronological order). A non-positive limit yields an empty slice.
func tailEvents(events []journal.Envelope, limit int) []journal.Envelope {
	if limit <= 0 {
		return []journal.Envelope{}
	}
	if len(events) <= limit {
		return events
	}
	return events[len(events)-limit:]
}

// latestJournalEventTS returns the newest event timestamp in the log, or the zero
// time when the log is empty/unreadable. It is the D10 freshness proxy used until
// the reasoned-overlay writer (a separate task) lands.
func latestJournalEventTS(repoPath string) time.Time {
	events, err := readJournalEvents(repoPath)
	if err != nil {
		return time.Time{}
	}
	var latest time.Time
	for _, e := range events {
		if t := parseEventTS(e.TS); t.After(latest) {
			latest = t
		}
	}
	return latest
}

// safeDryRun reports the global dry-run flag, tolerating an unset seam (so a
// test-wired Deps without DryRun never panics).
func safeDryRun() bool {
	return deps.Flags.DryRun != nil && deps.Flags.DryRun()
}

// emitJournalJSON writes v as indented JSON — the structured-output path shared by
// every journal subcommand.
func emitJournalJSON(out io.Writer, v any) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
