// Package lockfile implements the adapter lockfile state
// (graph-backend-adapter-contract §10.1).
//
// The lockfile pins each activated adapter's source and schema digests and
// tracks a per-materialized-view state machine for cross-adapter cutover.
// This package provides the value types, the view_status enumeration
// (§10.1.1), state-machine init on activation, and fail-closed reconciliation
// (§10.1.3). Cross-adapter transitions beyond init/reconcile are owned by
// later tasks in this plan.
//
// Persistence is NOT hand-rolled here. Adapter state is the "adapters" section
// of the shared .agentsrc.lock document (config-distribution-model §7.4):
// Load/Save route through internal/agentslock, which owns the whole JSON
// document, the atomic write, and the preservation of sibling sections
// (config, packages, …). The path argument is always the canonical
// config.AgentsLockPath — never a standalone *.lock file.
package lockfile

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"time"

	"github.com/AGOrcha/dot-agents/internal/agentslock"
)

// lockSectionAdapters is the agentslock section name this package owns. It is
// a sibling of the config resolver's "config" and the package resolver's
// "packages" sections in the same .agentsrc.lock document (§7.4).
const lockSectionAdapters = "adapters"

// ViewStatus is the normative four-value enum from §10.1.1.
type ViewStatus string

const (
	// StatusReady — view tables present and digest matches.
	StatusReady ViewStatus = "ready"
	// StatusPendingRecompatCheck — a dependee bumped; re-validate the view DSL.
	StatusPendingRecompatCheck ViewStatus = "pending-recompat-check"
	// StatusPendingRebuild — validation passed; bootstrap must rebuild.
	StatusPendingRebuild ViewStatus = "pending-rebuild"
	// StatusDSLUpdateRequired — incompatible dependee; dependent must ship a new query.
	StatusDSLUpdateRequired ViewStatus = "dsl-update-required"
)

// validStatuses is the set of legal view_status values.
var validStatuses = map[ViewStatus]bool{
	StatusReady:                true,
	StatusPendingRecompatCheck: true,
	StatusPendingRebuild:       true,
	StatusDSLUpdateRequired:    true,
}

// Valid reports whether s is one of the four normative statuses.
func (s ViewStatus) Valid() bool { return validStatuses[s] }

// maxStateHistory bounds the per-view audit log (§10.1.1).
const maxStateHistory = 20

// StateTransition is one entry in a view's bounded audit log (§10.1.1).
type StateTransition struct {
	At      string     `json:"at"`
	From    ViewStatus `json:"from,omitempty"`
	To      ViewStatus `json:"to"`
	Trigger string     `json:"trigger"`
}

// ViewDependency records a dependee's schema digest at the time the view was
// last rebuilt and validated (§10.1).
type ViewDependency struct {
	Adapter      string `json:"adapter"`
	SchemaDigest string `json:"schema_digest"`
	Version      string `json:"version"`
}

// View is the per-materialized-view lockfile state (§10.1).
type View struct {
	ViewDigest       string            `json:"view_digest,omitempty"`
	ViewStatus       ViewStatus        `json:"view_status"`
	DependsOn        []ViewDependency  `json:"depends_on,omitempty"`
	LastRebuiltAt    string            `json:"last_rebuilt_at,omitempty"`
	LastValidationAt string            `json:"last_validation_at,omitempty"`
	StateHistory     []StateTransition `json:"state_history,omitempty"`
}

// recordTransition appends a transition and truncates to the last
// maxStateHistory entries (§10.1.1).
func (v *View) recordTransition(to ViewStatus, trigger string, now time.Time) {
	from := v.ViewStatus
	v.ViewStatus = to
	v.StateHistory = append(v.StateHistory, StateTransition{
		At:      now.UTC().Format(time.RFC3339),
		From:    from,
		To:      to,
		Trigger: trigger,
	})
	if len(v.StateHistory) > maxStateHistory {
		v.StateHistory = v.StateHistory[len(v.StateHistory)-maxStateHistory:]
	}
}

// Adapter is the per-adapter lockfile state (§10.1).
type Adapter struct {
	SourceDigest      string           `json:"source_digest"`
	SchemaDigest      string           `json:"schema_digest"`
	ActivatedAt       string           `json:"activated_at"`
	MaterializedViews map[string]*View `json:"materialized_views,omitempty"`
}

// Lockfile is the in-memory view of the "adapters" section of .agentsrc.lock
// (§10.1). It is a typed projection of one section; the surrounding document
// (config, packages, lock_version) is owned and preserved by agentslock.
type Lockfile struct {
	Adapters map[string]*Adapter `json:"adapters"`
}

// New returns an empty lockfile.
func New() *Lockfile {
	return &Lockfile{Adapters: map[string]*Adapter{}}
}

// Digest returns the canonical sha256 digest of data, prefixed `sha256:`.
func Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// lockOpener is the narrow collaborator Load/Save need from the shared writer:
// open a .agentsrc.lock document for reading/staging its sections. It is the
// interface-DI test seam (docs/TEST_SEAMS.md) over agentslock.Open, letting the
// open-error branch be faulted without crafting a malformed lock on disk.
type lockOpener interface {
	Open(path string) (*agentslock.Lockfile, error)
}

// stdLockOpener is the production lockOpener, backed by the real shared writer.
type stdLockOpener struct{}

func (stdLockOpener) Open(path string) (*agentslock.Lockfile, error) {
	return agentslock.Open(path)
}

// Load reads the "adapters" section of the .agentsrc.lock document at path. A
// missing file or absent section yields an empty lockfile and no error (a
// not-yet-initialized graph is valid). Sibling sections (config, packages) are
// not loaded here — they belong to other writers (§7.4).
func Load(path string) (*Lockfile, error) {
	return loadWith(stdLockOpener{}, path)
}

// loadWith is Load's injection point: production passes stdLockOpener{}; tests
// pass a fake to exercise the open/decode error branches.
func loadWith(opener lockOpener, path string) (*Lockfile, error) {
	doc, err := opener.Open(path)
	if err != nil {
		return nil, fmt.Errorf("lockfile: open %s: %w", path, err)
	}
	lf := New()
	if _, err := doc.Section(lockSectionAdapters, &lf.Adapters); err != nil {
		return nil, fmt.Errorf("lockfile: decode adapters %s: %w", path, err)
	}
	if lf.Adapters == nil {
		lf.Adapters = map[string]*Adapter{}
	}
	return lf, nil
}

// Save stages the lockfile as the "adapters" section of the .agentsrc.lock
// document at path and flushes it atomically via agentslock (§7.4). Any
// sibling sections already present (config, packages, …) are preserved
// verbatim: Save opens the live document, replaces only "adapters", and writes
// the whole thing back. The shared writer owns the atomic temp-file+rename.
func Save(path string, lf *Lockfile) error {
	return saveWith(stdLockOpener{}, path, lf)
}

// saveWith is Save's injection point: production passes stdLockOpener{}; tests
// pass a fake to exercise the open-error branch.
func saveWith(opener lockOpener, path string, lf *Lockfile) error {
	if lf == nil {
		return fmt.Errorf("lockfile: cannot save nil lockfile")
	}
	doc, err := opener.Open(path)
	if err != nil {
		return fmt.Errorf("lockfile: open %s: %w", path, err)
	}
	if err := doc.SetSection(lockSectionAdapters, lf.Adapters); err != nil {
		return fmt.Errorf("lockfile: stage adapters: %w", err)
	}
	if err := doc.Flush(); err != nil {
		return fmt.Errorf("lockfile: flush %s: %w", path, err)
	}
	return nil
}

// dependsOn reports whether the view records a dependency on dependee.
func (v *View) dependsOn(dependee string) bool {
	for _, d := range v.DependsOn {
		if d.Adapter == dependee {
			return true
		}
	}
	return false
}

// RegisterView records a materialized view's initial lockfile state on the
// consumer adapter as ready: the view was just compiled and (re)built against
// the recorded dependency digests (§10.1). It creates the consumer adapter entry
// if absent. Re-registering replaces the view's recorded dependencies and digest
// and resets it to ready (a fresh build).
func (lf *Lockfile) RegisterView(adapter, view, viewDigest string, deps []ViewDependency, now time.Time) {
	if lf.Adapters == nil {
		lf.Adapters = map[string]*Adapter{}
	}
	ad := lf.Adapters[adapter]
	if ad == nil {
		ad = &Adapter{}
		lf.Adapters[adapter] = ad
	}
	if ad.MaterializedViews == nil {
		ad.MaterializedViews = map[string]*View{}
	}
	v := &View{ViewDigest: viewDigest, DependsOn: append([]ViewDependency(nil), deps...)}
	v.recordTransition(StatusReady, "registered", now)
	v.LastRebuiltAt = now.UTC().Format(time.RFC3339)
	ad.MaterializedViews[view] = v
}

// viewKey is the stable "adapter/view" identity used in cutover result slices.
func viewKey(adapter, view string) string { return adapter + "/" + view }

// ViewStatusOf returns a view's current status and whether it is recorded.
func (lf *Lockfile) ViewStatusOf(adapter, view string) (ViewStatus, bool) {
	v, err := lf.findView(adapter, view)
	if err != nil {
		return "", false
	}
	return v.ViewStatus, true
}

// findView resolves a view's state, erroring when the adapter or view is absent.
func (lf *Lockfile) findView(adapter, view string) (*View, error) {
	ad := lf.Adapters[adapter]
	if ad == nil || ad.MaterializedViews[view] == nil {
		return nil, fmt.Errorf("lockfile: no materialized view %s", viewKey(adapter, view))
	}
	return ad.MaterializedViews[view], nil
}

// MarkDependeeBumped freezes every ready view that depends_on dependee into
// pending-recompat-check (§10.3 step 1: a dependee schema bump suspends its
// dependents until each view's DSL is re-validated). It returns the affected
// "adapter/view" keys in sorted order. Views not currently ready are left as-is
// (a view already mid-cutover is not re-frozen).
func (lf *Lockfile) MarkDependeeBumped(dependee string, now time.Time) []string {
	var affected []string
	for _, an := range lf.AdapterNames() {
		ad := lf.Adapters[an]
		for _, vn := range sortedViewNames(ad) {
			v := ad.MaterializedViews[vn]
			if v.ViewStatus == StatusReady && v.dependsOn(dependee) {
				v.recordTransition(StatusPendingRecompatCheck, "dependee-bump:"+dependee, now)
				affected = append(affected, viewKey(an, vn))
			}
		}
	}
	return affected
}

// ResolveRecompat applies the mechanical cutover gate result (§10.3) to a view
// in pending-recompat-check: compatible → pending-rebuild (recording the new
// dependency digests the view will rebuild against); incompatible →
// dsl-update-required (the dependent must ship a new query; O1 — no
// accepts_breaking_changes opt-out). It errors if the view is not in
// pending-recompat-check (the only state from which recompat resolves).
func (lf *Lockfile) ResolveRecompat(adapter, view string, compatible bool, deps []ViewDependency, now time.Time) error {
	v, err := lf.findView(adapter, view)
	if err != nil {
		return err
	}
	if v.ViewStatus != StatusPendingRecompatCheck {
		return fmt.Errorf("lockfile: view %s is %q, not pending-recompat-check", viewKey(adapter, view), v.ViewStatus)
	}
	v.LastValidationAt = now.UTC().Format(time.RFC3339)
	if compatible {
		v.DependsOn = append([]ViewDependency(nil), deps...)
		v.recordTransition(StatusPendingRebuild, "recompat-pass", now)
		return nil
	}
	v.recordTransition(StatusDSLUpdateRequired, "recompat-fail", now)
	return nil
}

// MarkViewRebuilt transitions a pending-rebuild view to ready after the
// bootstrap recomputes its rows (§10.3), recording the rebuilt view digest. It
// errors if the view is not in pending-rebuild.
func (lf *Lockfile) MarkViewRebuilt(adapter, view, viewDigest string, now time.Time) error {
	v, err := lf.findView(adapter, view)
	if err != nil {
		return err
	}
	if v.ViewStatus != StatusPendingRebuild {
		return fmt.Errorf("lockfile: view %s is %q, not pending-rebuild", viewKey(adapter, view), v.ViewStatus)
	}
	v.ViewDigest = viewDigest
	v.LastRebuiltAt = now.UTC().Format(time.RFC3339)
	v.recordTransition(StatusReady, "rebuilt", now)
	return nil
}

// ActivationBlockers returns the dependent "adapter/view" keys in
// dsl-update-required that depend_on dependee, in sorted order. A non-empty
// result BLOCKS dependee (re)activation (§10.3 mechanical gate): the dependee
// cannot go live until every incompatible dependent ships an updated query that
// clears the block.
func (lf *Lockfile) ActivationBlockers(dependee string) []string {
	var blockers []string
	for _, an := range lf.AdapterNames() {
		ad := lf.Adapters[an]
		for _, vn := range sortedViewNames(ad) {
			v := ad.MaterializedViews[vn]
			if v.ViewStatus == StatusDSLUpdateRequired && v.dependsOn(dependee) {
				blockers = append(blockers, viewKey(an, vn))
			}
		}
	}
	return blockers
}

// Activate registers (or refreshes) an adapter's lockfile entry on
// activation, initializing the per-adapter state machine. sourceDigest is
// the digest of the adapter YAML; schemaDigest is the canonical schema hash.
// Re-activating an existing adapter updates its digests and activation time
// while preserving its materialized-view state.
func (lf *Lockfile) Activate(name, sourceDigest, schemaDigest string, now time.Time) {
	if lf.Adapters == nil {
		lf.Adapters = map[string]*Adapter{}
	}
	existing := lf.Adapters[name]
	if existing == nil {
		existing = &Adapter{}
		lf.Adapters[name] = existing
	}
	existing.SourceDigest = sourceDigest
	existing.SchemaDigest = schemaDigest
	existing.ActivatedAt = now.UTC().Format(time.RFC3339)
}

// AdapterNames returns the activated adapter names in sorted order.
func (lf *Lockfile) AdapterNames() []string {
	names := make([]string, 0, len(lf.Adapters))
	for n := range lf.Adapters {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// ViewPresenceFunc reports, for an adapter and view, whether the view tables
// are present on disk and (if present) the on-disk schema digest. Reconcile
// uses it to compare against the lockfile's recorded view_digest.
type ViewPresenceFunc func(adapter, view string) (present bool, onDiskDigest string)

// Inconsistency is one reconciliation finding (§10.1.3).
type Inconsistency struct {
	Adapter string
	View    string
	From    ViewStatus
	To      ViewStatus
	Reason  string
}

// Reconcile performs the fail-closed reconciliation pass (§10.1.3). It
// cross-checks each ready view against on-disk presence via present and
// flips lockfile state per the §10.1.3 table. It never deletes view rows; it
// only mutates lockfile state. The returned slice lists every state change.
// A nil presence func treats every view as absent (the conservative case).
func (lf *Lockfile) Reconcile(present ViewPresenceFunc, now time.Time) []Inconsistency {
	var found []Inconsistency
	for _, an := range lf.AdapterNames() {
		ad := lf.Adapters[an]
		for _, vn := range sortedViewNames(ad) {
			if inc := reconcileView(an, vn, ad.MaterializedViews[vn], present, now); inc != nil {
				found = append(found, *inc)
			}
		}
	}
	return found
}

// sortedViewNames returns an adapter's materialized-view names in sorted order.
func sortedViewNames(ad *Adapter) []string {
	viewNames := make([]string, 0, len(ad.MaterializedViews))
	for vn := range ad.MaterializedViews {
		viewNames = append(viewNames, vn)
	}
	sort.Strings(viewNames)
	return viewNames
}

// reconcileView applies the §10.1.3 reconcile rule to a single view, mutating
// its state when a transition is required. It returns the resulting
// Inconsistency, or nil when the view needs no action.
func reconcileView(adapter, view string, v *View, present ViewPresenceFunc, now time.Time) *Inconsistency {
	switch v.ViewStatus {
	case StatusReady:
		return reconcileReadyView(adapter, view, v, present, now)
	case StatusPendingRecompatCheck, StatusPendingRebuild, StatusDSLUpdateRequired:
		// No reconcile action: pending-rebuild and dsl-update-required
		// are handled by their own flows; pending-recompat-check is
		// re-validated by `da kg view validate`, not the reconcile pass.
		return nil
	default:
		// Unknown/empty status: force pending-rebuild fail-closed.
		return transitionView(adapter, view, v, "invalid-status", "invalid view_status", now)
	}
}

// reconcileReadyView cross-checks a ready view against on-disk presence and
// flips it to pending-rebuild when absent or digest-mismatched. It returns nil
// when the view is consistent on disk.
func reconcileReadyView(adapter, view string, v *View, present ViewPresenceFunc, now time.Time) *Inconsistency {
	if present != nil {
		if p, digest := present(adapter, view); p && digest == v.ViewDigest {
			return nil
		}
	}
	reason := "view tables absent"
	if present != nil {
		if p, _ := present(adapter, view); p {
			reason = "view digest mismatch"
		}
	}
	return transitionView(adapter, view, v, reason, reason, now)
}

// transitionView records a pending-rebuild transition on v and returns the
// matching Inconsistency. transitionReason labels the recorded transition;
// inconsistencyReason is reported to the caller.
func transitionView(adapter, view string, v *View, transitionReason, inconsistencyReason string, now time.Time) *Inconsistency {
	from := v.ViewStatus
	v.recordTransition(StatusPendingRebuild, "reconcile:"+transitionReason, now)
	return &Inconsistency{
		Adapter: adapter, View: view, From: from, To: StatusPendingRebuild, Reason: inconsistencyReason,
	}
}
