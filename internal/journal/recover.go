package journal

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// recover.go is the READ side of the session-handoff journal: the VERIFIED
// recovery view (spec D7/D8/D10, refined by R7/R8). It reconstructs the workflow
// state a crashed/compacted session left behind and — crucially — RE-VERIFIES each
// reconstructed item against current reality rather than trusting the journal
// blindly. The journal records what a command CLAIMED happened; reality may have
// moved (a PR merged, a branch deleted, a task re-statused) between the last
// durable append and the resume.
//
// # Reconstruct (locked decision)
//
// The view is built from two durable inputs that survive a compaction/kill: the
// deterministic snapshot (LoadSnapshot, the watermark base) plus the append-only
// event log (events.log, replayed via the typed registry factories). Snapshot task
// states seed the reconstruction; events with a timestamp AFTER the snapshot
// watermark replay the deltas the snapshot does not yet reflect.
//
// # Re-verify (D7/R7)
//
// Each reconstructed item is checked against an injectable, ordered list of
// VerificationSources. R7: PREFER an authoritative store/service-backed source
// (gh/remote, or — once the KG-as-SoT end-state lands — a store-backed da) that
// survives a locked/missing working tree; FALL BACK to a local git/da source. The
// concrete sources are injected by the caller (p6) so this layer never shells out
// and stays hermetically testable. Each item is tagged verified | changed | missing
// | unverified, with an explicit human-readable delta when reality differs, so the
// recovery NEVER injects a stale claim as a fact (Done criterion 2).
//
// # Trust gradient (R7/D10)
//
// Per item: evidence from an authoritative source is high-trust, evidence from a
// local fallback is medium, an item no source could re-verify is low (a hypothesis,
// not a fact). Per bundle: the reasoned overlay's freshness RELATIVE TO the
// snapshot timestamp (D10) labels the whole bundle fresh / stale / orphaned; an
// orphaned bundle is quarantined rather than auto-resumed.
//
// # Composite identity + quarantine (D8/R8)
//
// Items are keyed by a composite identity (kind, plan, task). When an event can
// only be pinned to an ambiguous identity — a merge-back names a task id present in
// several plans and cannot be disambiguated — the collision is QUARANTINED as a
// surfaced conflict rather than silently merged onto a guessed plan (D8: "a wrong
// handoff is worse than none"). A snapshot captured against a different repo
// identity than the resuming session quarantines the whole bundle. The
// canonical-vs-in-PR locus (R8) is honored throughout: a done-on-master item is
// never conflated with in-PR work; the snapshot's placeholder coordinates (PR 0,
// the "canonical" sentinel ref) are ENRICHED here from the verification source's
// resolved PR number / merge sha.

// VerificationStatus tags how a reconstructed item stands against current reality.
type VerificationStatus string

const (
	// StatusVerified — reality matches the journal's reconstructed claim.
	StatusVerified VerificationStatus = "verified"
	// StatusChanged — reality differs from the claim; both are recorded plus a delta.
	StatusChanged VerificationStatus = "changed"
	// StatusMissing — the journaled thing no longer exists in reality.
	StatusMissing VerificationStatus = "missing"
	// StatusUnverified — no source could re-verify the item; it is a hypothesis,
	// not a fact, and must never be injected as one (D7).
	StatusUnverified VerificationStatus = "unverified"
)

// TrustGrade weights an item by how fresh/authoritative its evidence is (R7/D10).
type TrustGrade string

const (
	// TrustHigh — re-verified against an authoritative store/service source that
	// survives a locked/missing working tree (R7).
	TrustHigh TrustGrade = "high"
	// TrustMedium — re-verified only against a local fallback source.
	TrustMedium TrustGrade = "medium"
	// TrustLow — could not be re-verified by any source.
	TrustLow TrustGrade = "low"
)

// FreshnessLabel is the D10 bundle-level trust signal derived from the reasoned
// overlay's recency relative to the snapshot timestamp.
type FreshnessLabel string

const (
	// FreshnessFresh — the reasoned overlay tracks the snapshot closely; trust it.
	FreshnessFresh FreshnessLabel = "fresh"
	// FreshnessStale — a gap opened; treat the bundle as a hypothesis.
	FreshnessStale FreshnessLabel = "stale"
	// FreshnessOrphaned — no/very old reasoned write vs the snapshot; quarantine.
	FreshnessOrphaned FreshnessLabel = "orphaned"
)

// Locus arm names, hoisted so the canonical-vs-in-PR distinction is one literal
// each (no scattered strings, no S1192) and shared by locusArm/describeLocus.
const (
	armNone      = "none"
	armCanonical = "canonical"
	armInOpenPR  = "in_open_pr"
)

// Freshness thresholds: a reasoned overlay within defaultStaleAfter of the snapshot
// is fresh; within defaultOrphanAfter it is stale; beyond that (or absent) it is
// orphaned. D6's backstop cadence is ~10 min, so a gap inside that window is fresh.
const (
	defaultStaleAfter  = 10 * time.Minute
	defaultOrphanAfter = time.Hour
)

// ErrSourceUnavailable is returned by a VerificationSource that cannot answer right
// now (e.g. the remote is unreachable, or a local source's working tree is locked).
// The recovery view treats it as "try the next source" — the R7 fallback hinge.
var ErrSourceUnavailable = errors.New("journal: verification source unavailable")

// ItemKind classifies a recovered item within its composite identity (D8).
type ItemKind string

// KindTask is the primary tracked item — a plan task with a locus.
const KindTask ItemKind = "task"

// ItemKey is the composite identity that pins a recovered item (D8). Two events
// that can only be resolved to the same ambiguous key are quarantined, never merged.
type ItemKey struct {
	Kind ItemKind `json:"kind"`
	Plan string   `json:"plan"`
	Task string   `json:"task"`
}

// ItemState is one item's bounded state: its workflow status and its locus
// (canonical vs in-open-PR). It is both what replay reconstructs and what a source
// reports as reality.
type ItemState struct {
	Status string `json:"status,omitempty"`
	Locus  *Locus `json:"locus,omitempty"`
}

// RealityCheck is what a VerificationSource observed for an item right now.
type RealityCheck struct {
	// Exists is false when the source confirms the journaled thing is gone (the
	// PR/branch/task no longer exists) → the item is tagged missing.
	Exists bool `json:"exists"`
	// Status is the item's current status as the source sees it (empty = no opinion;
	// an empty status never forces a "changed" tag).
	Status string `json:"status,omitempty"`
	// Locus is the locus the source resolved — the real PR number / merge sha the
	// snapshot recorded only as a placeholder. nil when no coordinates were resolved.
	Locus *Locus `json:"locus,omitempty"`
}

// VerificationSource re-verifies a reconstructed item against one backing source of
// truth. RecoveryView is handed an ordered list (R7: authoritative/store-backed
// first, local fallback last) and, per item, uses the first source that can answer;
// a source signals "I cannot answer" by returning ErrSourceUnavailable (or any
// error), and the view falls through to the next. Sources are an injected seam so
// production wiring (gh/git/da, in p6) stays out of this hermetically tested layer.
type VerificationSource interface {
	// Name identifies the source in the recovery view (which source confirmed it).
	Name() string
	// Authoritative reports whether this source is store/service-backed and so
	// survives a locked/missing working tree (R7). Drives the per-item trust grade.
	Authoritative() bool
	// VerifyTask re-verifies one reconstructed item against this source.
	VerifyTask(key ItemKey, reconstructed ItemState) (RealityCheck, error)
}

// RecoveredItem is one reconstructed-and-verified tracked item.
type RecoveredItem struct {
	Key ItemKey `json:"key"`
	// Status is the verification tag (verified|changed|missing|unverified).
	Status VerificationStatus `json:"status"`
	// Trust is the freshness/authority grade of the evidence (R7/D10).
	Trust TrustGrade `json:"trust"`
	// Reconstructed is the state replay rebuilt from snapshot+events (locus coords
	// enriched in place from reality on a verified item).
	Reconstructed ItemState `json:"reconstructed"`
	// Reality is what the source observed; nil when the item could not be verified.
	Reality *ItemState `json:"reality,omitempty"`
	// VerifiedBy names the source that answered ("" when unverified).
	VerifiedBy string `json:"verified_by,omitempty"`
	// Delta is the explicit "journal recorded X; <source> says Y" string, set only
	// when reality differs (changed/missing) so a stale claim is never a bare fact.
	Delta string `json:"delta,omitempty"`
}

// IdentityConflict is a surfaced D8 collision: an event pinned to an ambiguous
// composite identity that could not be disambiguated, quarantined rather than
// merged onto a guessed plan.
type IdentityConflict struct {
	Task           string   `json:"task"`
	CandidatePlans []string `json:"candidate_plans"`
	Reason         string   `json:"reason"`
}

// BundleFreshness is the D10 bundle-level trust signal: the reasoned overlay's
// recency relative to the snapshot timestamp.
type BundleFreshness struct {
	Label             FreshnessLabel `json:"label"`
	SnapshotAt        time.Time      `json:"snapshot_at"`
	LastReasonedWrite time.Time      `json:"last_reasoned_write"`
	Gap               time.Duration  `json:"gap"`
}

// RecoveryResult is the verified, tagged, trust-graded view RecoveryView returns —
// the struct p6's `da workflow journal recover` renders.
type RecoveryResult struct {
	// Identity is the resuming session's composite identity.
	Identity Identity `json:"identity"`
	// SnapshotAt is the snapshot watermark (the snapshot's captured_at), "" when no
	// snapshot was present (replay-only, degraded).
	SnapshotAt string `json:"snapshot_at,omitempty"`
	// Freshness is the D10 bundle trust signal.
	Freshness BundleFreshness `json:"freshness"`
	// Quarantined is set when the whole bundle must not be auto-resumed (identity
	// mismatch or an orphaned overlay); QuarantineReason explains why.
	Quarantined      bool   `json:"quarantined"`
	QuarantineReason string `json:"quarantine_reason,omitempty"`
	// Items is the reconstructed-and-verified items, sorted by composite identity.
	Items []RecoveredItem `json:"items"`
	// Conflicts is the surfaced D8 identity collisions (never silently merged).
	Conflicts []IdentityConflict `json:"conflicts,omitempty"`
	// Notes carries degraded-mode observations (missing snapshot, skipped lines).
	Notes []string `json:"notes,omitempty"`
}

// Deps are RecoveryView's injectable inputs (R7 sources + D10 freshness clock).
type Deps struct {
	// Sources is the ordered re-verify probe list — authoritative/store-backed first
	// (R7), local fallback last. Nil/empty leaves every item unverified (low trust).
	Sources []VerificationSource
	// SessionIdentity is the resuming session's identity for the D8 mismatch check.
	// Zero value → ResolveIdentity(repoPath).
	SessionIdentity Identity
	// LastReasonedWrite is the reasoned overlay's last write time (D10 freshness).
	// Zero → the overlay is treated as orphaned.
	LastReasonedWrite time.Time
	// StaleAfter / OrphanAfter bound the freshness gradient; zero → defaults.
	StaleAfter  time.Duration
	OrphanAfter time.Duration
}

// loadSnapshotForRecovery and readEventsLog are seams over the two durable reads so
// tests can drive the missing-snapshot (degraded) and read-error branches without
// staging filesystem faults.
var (
	loadSnapshotForRecovery = LoadSnapshot
	readEventsLog           = func(repoPath string) ([]byte, error) {
		data, err := os.ReadFile(EventsLogPath(repoPath))
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return data, err
	}
)

// RecoveryView builds the verified recovery view for the repository at repoPath. It
// loads the snapshot watermark, replays events.log over it, re-verifies each
// reconstructed item against the injected sources, applies the trust gradient and
// the D8/D10 quarantine rules, and returns the tagged result. The only hard error
// is an unreadable event log; a missing snapshot degrades to a replay-only view
// with a note (never a failure), mirroring the snapshot layer's "never fails" reads.
func RecoveryView(repoPath string, deps Deps) (RecoveryResult, error) {
	deps = deps.withDefaults(repoPath)

	snap, snapErr := loadSnapshotForRecovery(repoPath)
	if snapErr != nil {
		snap = SnapshotState{}
	}
	events, skipped, err := replayEvents(repoPath)
	if err != nil {
		return RecoveryResult{}, err
	}

	recon := reconstruct(snap, events)

	result := RecoveryResult{
		Identity:   deps.SessionIdentity,
		SnapshotAt: snap.CapturedAt,
		Conflicts:  recon.conflicts,
		Items:      recon.verify(deps.Sources),
		Freshness:  computeFreshness(parseTS(snap.CapturedAt), deps.LastReasonedWrite, deps.StaleAfter, deps.OrphanAfter),
	}
	applyBundleQuarantine(&result, snap.Identity, deps.SessionIdentity)
	result.Notes = recoveryNotes(snapErr, skipped)
	return result, nil
}

// withDefaults fills the unset Deps fields: the session identity from the checkout
// and the freshness thresholds from the package defaults.
func (d Deps) withDefaults(repoPath string) Deps {
	if d.SessionIdentity.Fingerprint == "" {
		d.SessionIdentity = ResolveIdentity(repoPath)
	}
	if d.StaleAfter == 0 {
		d.StaleAfter = defaultStaleAfter
	}
	if d.OrphanAfter == 0 {
		d.OrphanAfter = defaultOrphanAfter
	}
	return d
}

// replayEvents reads events.log and decodes each NDJSON line into an Envelope,
// sorted by (ts, seq) — the spec's replay ordering. A crash can leave a torn final
// line; malformed lines are skipped and counted (returned as skipped) rather than
// failing the whole recovery. A missing log is an empty replay, not an error.
func replayEvents(repoPath string) (events []Envelope, skipped int, err error) {
	data, err := readEventsLog(repoPath)
	if err != nil {
		return nil, 0, fmt.Errorf("journal: read events: %w", err)
	}
	for _, line := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var e Envelope
		if json.Unmarshal(line, &e) != nil {
			skipped++
			continue
		}
		events = append(events, e)
	}
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].TS != events[j].TS {
			return events[i].TS < events[j].TS
		}
		return events[i].Seq < events[j].Seq
	})
	return events, skipped, nil
}

// reconstruction is the in-progress recovery state: items keyed by composite
// identity, the plans each task id appears in (for D8 ambiguity resolution), and
// the surfaced collisions.
type reconstruction struct {
	items       map[ItemKey]*RecoveredItem
	order       []ItemKey
	plansByTask map[string][]string
	conflicts   []IdentityConflict
}

// reconstruct seeds the recovery from the snapshot watermark and replays the events
// recorded AFTER it, so the result reflects deltas the snapshot does not yet carry.
func reconstruct(snap SnapshotState, events []Envelope) *reconstruction {
	r := seedFromSnapshot(snap)
	for _, e := range events {
		if e.EventType == EventFailed || !afterWatermark(e.TS, snap.CapturedAt) {
			continue
		}
		if tr, ok := eventTransition(e); ok {
			r.apply(tr)
		}
	}
	return r
}

// seedFromSnapshot builds the base reconstruction from the snapshot's task states.
func seedFromSnapshot(snap SnapshotState) *reconstruction {
	r := &reconstruction{items: map[ItemKey]*RecoveredItem{}, plansByTask: map[string][]string{}}
	for _, p := range snap.Plans {
		for _, t := range p.Tasks {
			r.put(ItemKey{Kind: KindTask, Plan: p.ID, Task: t.ID}, ItemState{Status: t.Status, Locus: t.Locus})
		}
	}
	return r
}

// transition is the bounded effect a replayed task-transition command had: the
// item it targeted (plan may be empty for a merge-back) and its new status/locus.
type transition struct {
	plan   string
	task   string
	status string
	locus  *Locus
}

// eventTransition decodes an envelope's typed input+observed via the registry
// factories and extracts the task-transition effect. A command that is not a task
// transition (or is unregistered) yields ok=false and no effect.
func eventTransition(e Envelope) (transition, bool) {
	spec, ok := Lookup(e.Command)
	if !ok {
		return transition{}, false
	}
	in := spec.NewInput()
	if len(e.Input) > 0 && json.Unmarshal(e.Input, in) != nil {
		return transition{}, false
	}
	obs := spec.NewObserved()
	if len(e.Observed) > 0 && json.Unmarshal(e.Observed, obs) != nil {
		return transition{}, false
	}
	return extractTransition(in, obs)
}

// extractTransition maps the decoded typed payloads to a transition. Only the
// task-status commands move reconstructed task state; merge-back carries a locus but
// no plan (the source of the D8 ambiguity case).
func extractTransition(in, obs any) (transition, bool) {
	switch i := in.(type) {
	case *AdvanceInput:
		o := obs.(*AdvanceObserved)
		return transition{plan: i.Plan, task: i.Task, status: o.ToStatus, locus: o.Locus}, true
	case *StartTaskInput:
		o := obs.(*StartTaskObserved)
		return transition{plan: i.Plan, task: i.Task, status: o.ToStatus}, true
	case *CloseTaskInput:
		o := obs.(*CloseTaskObserved)
		return transition{plan: i.Plan, task: i.Task, status: o.ToStatus, locus: o.Locus}, true
	case *MergeBackInput:
		o := obs.(*MergeBackObserved)
		return transition{task: i.Task, locus: o.Locus}, true
	default:
		return transition{}, false
	}
}

// apply resolves the transition's target identity and mutates the item, or records
// a D8 collision when the identity is ambiguous.
func (r *reconstruction) apply(tr transition) {
	plan, ok := r.resolveTarget(tr)
	if !ok {
		return
	}
	key := ItemKey{Kind: KindTask, Plan: plan, Task: tr.task}
	item := r.items[key]
	if item == nil {
		item = r.put(key, ItemState{})
	}
	if tr.status != "" {
		item.Reconstructed.Status = tr.status
	}
	if tr.locus != nil {
		item.Reconstructed.Locus = tr.locus
	}
}

// resolveTarget pins a transition to a plan. A transition that already names its
// plan is unambiguous. A plan-less transition (merge-back) resolves to the single
// plan that owns the task id; zero known plans yields a bare (plan-less) item; two
// or more is an unresolvable D8 collision — quarantined as a conflict, not guessed.
func (r *reconstruction) resolveTarget(tr transition) (string, bool) {
	if tr.plan != "" {
		return tr.plan, true
	}
	plans := distinct(r.plansByTask[tr.task])
	switch len(plans) {
	case 1:
		return plans[0], true
	case 0:
		return "", true
	default:
		r.conflicts = append(r.conflicts, IdentityConflict{
			Task:           tr.task,
			CandidatePlans: plans,
			Reason:         "task id present in multiple plans; merge-back identity is ambiguous and cannot be disambiguated (D8)",
		})
		return "", false
	}
}

// put registers a new item under key with the given seed state, recording it in the
// deterministic order list and the task→plans index.
func (r *reconstruction) put(key ItemKey, state ItemState) *RecoveredItem {
	item := &RecoveredItem{Key: key, Reconstructed: state}
	r.items[key] = item
	r.order = append(r.order, key)
	r.plansByTask[key.Task] = append(r.plansByTask[key.Task], key.Plan)
	return item
}

// verify re-verifies every reconstructed item against the ordered sources and
// returns the tagged, trust-graded items sorted by composite identity.
func (r *reconstruction) verify(sources []VerificationSource) []RecoveredItem {
	keys := append([]ItemKey(nil), r.order...)
	sort.Slice(keys, func(i, j int) bool { return lessKey(keys[i], keys[j]) })
	out := make([]RecoveredItem, 0, len(keys))
	for _, key := range keys {
		item := r.items[key]
		verifyItem(item, sources)
		out = append(out, *item)
	}
	return out
}

// verifyItem runs the item through the sources in order, applying the first one
// that answers; if none can, the item is left unverified at low trust (a hypothesis,
// never injected as a fact).
func verifyItem(item *RecoveredItem, sources []VerificationSource) {
	for _, src := range sources {
		check, err := src.VerifyTask(item.Key, item.Reconstructed)
		if err != nil {
			continue
		}
		applyCheck(item, src, check)
		return
	}
	item.Status = StatusUnverified
	item.Trust = TrustLow
}

// applyCheck tags an item from a source's RealityCheck and grades its trust by the
// source's authority. A non-existent thing is missing; a matching reality is
// verified (with the locus coords enriched in place); a differing reality is changed
// with an explicit delta — so the canonical-vs-in-PR locus is honored and never
// conflated, and no stale claim is presented as a bare fact.
func applyCheck(item *RecoveredItem, src VerificationSource, check RealityCheck) {
	item.VerifiedBy = src.Name()
	item.Trust = trustFor(src)
	if !check.Exists {
		item.Status = StatusMissing
		item.Delta = fmt.Sprintf("journal recorded %s; %s reports it no longer exists", describe(item.Reconstructed), src.Name())
		return
	}
	reality := ItemState{Status: check.Status, Locus: check.Locus}
	item.Reality = &reality
	if statesMatch(item.Reconstructed, reality) {
		item.Status = StatusVerified
		enrichLocus(&item.Reconstructed, check.Locus)
		return
	}
	item.Status = StatusChanged
	item.Delta = fmt.Sprintf("journal recorded %s; %s says %s", describe(item.Reconstructed), src.Name(), describe(reality))
}

// trustFor grades evidence by source authority (R7): a store/service-backed source
// is high-trust, a local fallback is medium.
func trustFor(src VerificationSource) TrustGrade {
	if src.Authoritative() {
		return TrustHigh
	}
	return TrustMedium
}

// statesMatch reports whether reality matches the reconstructed claim. The locus
// ARM is the load-bearing comparison (R8): an item that moved from in_open_pr to
// canonical has MERGED — a change, not a match — even though both are "done". A
// status mismatch is a change too; an empty reality status is "no opinion" and never
// forces a change (it lets a placeholder-coord-only enrichment count as verified).
func statesMatch(recon, reality ItemState) bool {
	return locusArm(recon.Locus) == locusArm(reality.Locus) && statusMatches(recon.Status, reality.Status)
}

// statusMatches treats an empty reality status as agreement (the source had no
// status opinion, only locus coords to enrich).
func statusMatches(recon, reality string) bool {
	return reality == "" || reality == recon
}

// locusArm names which arm of the locus sum type is set (R8). A nil or malformed
// locus is armNone.
func locusArm(l *Locus) string {
	switch {
	case l == nil:
		return armNone
	case l.Canonical != nil:
		return armCanonical
	case l.InOpenPR != nil:
		return armInOpenPR
	default:
		return armNone
	}
}

// enrichLocus fills the snapshot's PLACEHOLDER locus coordinates from the source's
// resolved reality, strictly within the matching arm so canonical and in-PR are
// never conflated (R8). The snapshot records a sentinel canonical ref and PR 0; p5
// is where the real merge sha / PR number lands.
func enrichLocus(recon *ItemState, reality *Locus) {
	if reality == nil || recon.Locus == nil {
		return
	}
	switch {
	case recon.Locus.Canonical != nil && reality.Canonical != nil:
		if isPlaceholderRef(recon.Locus.Canonical.Ref) && reality.Canonical.Ref != "" {
			recon.Locus.Canonical.Ref = reality.Canonical.Ref
		}
	case recon.Locus.InOpenPR != nil && reality.InOpenPR != nil:
		if recon.Locus.InOpenPR.PR == 0 {
			recon.Locus.InOpenPR.PR = reality.InOpenPR.PR
		}
		if recon.Locus.InOpenPR.Status == "" {
			recon.Locus.InOpenPR.Status = reality.InOpenPR.Status
		}
	}
}

// isPlaceholderRef reports whether a canonical ref is still the snapshot's sentinel
// (or empty) and so awaits enrichment with a real merge sha.
func isPlaceholderRef(ref string) bool {
	return ref == "" || ref == canonicalLocusRef
}

// describe renders an item state for a delta string: "status=X canonical Y" /
// "status=X in_open_pr #N" / "status=X".
func describe(s ItemState) string {
	parts := make([]string, 0, 2)
	if s.Status != "" {
		parts = append(parts, "status="+s.Status)
	}
	if loc := describeLocus(s.Locus); loc != "" {
		parts = append(parts, loc)
	}
	if len(parts) == 0 {
		return "(no state)"
	}
	return strings.Join(parts, " ")
}

// describeLocus renders a locus arm for a delta string.
func describeLocus(l *Locus) string {
	switch locusArm(l) {
	case armCanonical:
		return armCanonical + " " + l.Canonical.Ref
	case armInOpenPR:
		return fmt.Sprintf("%s #%d", armInOpenPR, l.InOpenPR.PR)
	default:
		return ""
	}
}

// computeFreshness derives the D10 bundle trust label from the reasoned overlay's
// recency relative to the snapshot timestamp. No snapshot baseline or no reasoned
// write is orphaned; otherwise the absolute gap selects fresh/stale/orphaned.
func computeFreshness(snapshotAt, lastReasoned time.Time, staleAfter, orphanAfter time.Duration) BundleFreshness {
	f := BundleFreshness{SnapshotAt: snapshotAt, LastReasonedWrite: lastReasoned}
	if snapshotAt.IsZero() || lastReasoned.IsZero() {
		f.Label = FreshnessOrphaned
		return f
	}
	gap := snapshotAt.Sub(lastReasoned)
	if gap < 0 {
		gap = -gap
	}
	f.Gap = gap
	switch {
	case gap <= staleAfter:
		f.Label = FreshnessFresh
	case gap <= orphanAfter:
		f.Label = FreshnessStale
	default:
		f.Label = FreshnessOrphaned
	}
	return f
}

// applyBundleQuarantine sets the whole-bundle quarantine (D8 identity mismatch takes
// precedence over D10 orphaned freshness) so a stale or wrong-identity crash-bundle
// is never auto-resumed.
func applyBundleQuarantine(res *RecoveryResult, snapID, sessionID Identity) {
	if snapID.Fingerprint != "" && snapID.Fingerprint != sessionID.Fingerprint {
		res.Quarantined = true
		res.QuarantineReason = fmt.Sprintf(
			"snapshot identity %s does not match resuming session %s; quarantined (D8)",
			snapID.Fingerprint, sessionID.Fingerprint)
		return
	}
	if res.Freshness.Label == FreshnessOrphaned {
		res.Quarantined = true
		res.QuarantineReason = "reasoned overlay orphaned relative to snapshot; quarantined (D10)"
	}
}

// recoveryNotes records degraded-mode observations so the renderer can surface them.
func recoveryNotes(snapErr error, skipped int) []string {
	var notes []string
	if snapErr != nil {
		notes = append(notes, "snapshot unavailable; recovered from event replay only: "+snapErr.Error())
	}
	if skipped > 0 {
		notes = append(notes, fmt.Sprintf("%d malformed event line(s) skipped during replay", skipped))
	}
	return notes
}

// afterWatermark reports whether an event timestamp falls strictly after the
// snapshot watermark. RFC3339Nano UTC sorts lexicographically in chronological
// order, so a string compare is correct. An empty watermark (no snapshot) admits
// every event.
func afterWatermark(ts, watermark string) bool {
	return watermark == "" || ts > watermark
}

// parseTS parses an RFC3339Nano timestamp, returning the zero time on any error.
func parseTS(s string) time.Time {
	t, err := time.Parse(timeFormat, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// distinct returns the unique values of in, preserving first-seen order.
func distinct(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

// lessKey orders composite identities deterministically by kind, then plan, then
// task.
func lessKey(a, b ItemKey) bool {
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	if a.Plan != b.Plan {
		return a.Plan < b.Plan
	}
	return a.Task < b.Task
}
