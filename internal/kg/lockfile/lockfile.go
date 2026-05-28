// Package lockfile implements the adapter lockfile state
// (graph-backend-adapter-contract §10.1).
//
// The lockfile pins each activated adapter's source and schema digests and
// tracks a per-materialized-view state machine for cross-adapter cutover.
// This package provides the value types, atomic read/write (§10.1.2), the
// view_status enumeration (§10.1.1), state-machine init on activation, and
// fail-closed reconciliation (§10.1.3). Cross-adapter transitions beyond
// init/reconcile are owned by later tasks in this plan.
package lockfile

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"go.yaml.in/yaml/v3"
)

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
	At      string     `yaml:"at" json:"at"`
	From    ViewStatus `yaml:"from,omitempty" json:"from,omitempty"`
	To      ViewStatus `yaml:"to" json:"to"`
	Trigger string     `yaml:"trigger" json:"trigger"`
}

// ViewDependency records a dependee's schema digest at the time the view was
// last rebuilt and validated (§10.1).
type ViewDependency struct {
	Adapter      string `yaml:"adapter" json:"adapter"`
	SchemaDigest string `yaml:"schema_digest" json:"schema_digest"`
	Version      string `yaml:"version" json:"version"`
}

// View is the per-materialized-view lockfile state (§10.1).
type View struct {
	ViewDigest       string            `yaml:"view_digest,omitempty" json:"view_digest,omitempty"`
	ViewStatus       ViewStatus        `yaml:"view_status" json:"view_status"`
	DependsOn        []ViewDependency  `yaml:"depends_on,omitempty" json:"depends_on,omitempty"`
	LastRebuiltAt    string            `yaml:"last_rebuilt_at,omitempty" json:"last_rebuilt_at,omitempty"`
	LastValidationAt string            `yaml:"last_validation_at,omitempty" json:"last_validation_at,omitempty"`
	StateHistory     []StateTransition `yaml:"state_history,omitempty" json:"state_history,omitempty"`
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
	SourceDigest      string           `yaml:"source_digest" json:"source_digest"`
	SchemaDigest      string           `yaml:"schema_digest" json:"schema_digest"`
	ActivatedAt       string           `yaml:"activated_at" json:"activated_at"`
	MaterializedViews map[string]*View `yaml:"materialized_views,omitempty" json:"materialized_views,omitempty"`
}

// Lockfile is the top-level adapter lockfile document (§10.1).
type Lockfile struct {
	Adapters map[string]*Adapter `yaml:"adapters" json:"adapters"`
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

// Load reads and parses the lockfile at path. A missing file yields an empty
// lockfile and no error (a not-yet-initialized graph is valid).
func Load(path string) (*Lockfile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return New(), nil
		}
		return nil, fmt.Errorf("lockfile: read %s: %w", path, err)
	}
	lf := New()
	if err := yaml.Unmarshal(data, lf); err != nil {
		return nil, fmt.Errorf("lockfile: parse %s: %w", path, err)
	}
	if lf.Adapters == nil {
		lf.Adapters = map[string]*Adapter{}
	}
	return lf, nil
}

// tempFile is the subset of *os.File the atomic-write protocol uses. It is a
// seam so Save's write/fsync/close error branches are testable without a
// fault-injecting filesystem.
type tempFile interface {
	Name() string
	Write(p []byte) (int, error)
	Sync() error
	Close() error
}

// Save's filesystem collaborators, overridable in tests. Production wires the
// os package.
var (
	saveMkdirAll   = os.MkdirAll
	saveCreateTemp = func(dir, pattern string) (tempFile, error) {
		return os.CreateTemp(dir, pattern)
	}
	saveRename = os.Rename
	saveRemove = os.Remove
)

// Save writes the lockfile atomically (§10.1.2): marshal, write to a temp
// file alongside the target, fsync, rename. The parent directory is created
// if absent.
func Save(path string, lf *Lockfile) error {
	if lf == nil {
		return fmt.Errorf("lockfile: cannot save nil lockfile")
	}
	data, err := yaml.Marshal(lf)
	if err != nil {
		return fmt.Errorf("lockfile: marshal: %w", err)
	}
	dir := filepath.Dir(path)
	if err := saveMkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("lockfile: mkdir %s: %w", dir, err)
	}
	tmp, err := saveCreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("lockfile: create temp: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = saveRemove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("lockfile: write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("lockfile: fsync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("lockfile: close temp: %w", err)
	}
	if err := saveRename(tmpName, path); err != nil {
		return fmt.Errorf("lockfile: rename temp: %w", err)
	}
	cleanup = false
	return nil
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
		viewNames := make([]string, 0, len(ad.MaterializedViews))
		for vn := range ad.MaterializedViews {
			viewNames = append(viewNames, vn)
		}
		sort.Strings(viewNames)
		for _, vn := range viewNames {
			v := ad.MaterializedViews[vn]
			switch v.ViewStatus {
			case StatusReady:
				ok := false
				if present != nil {
					p, digest := present(an, vn)
					ok = p && digest == v.ViewDigest
				}
				if !ok {
					reason := "view tables absent"
					if present != nil {
						if p, _ := present(an, vn); p {
							reason = "view digest mismatch"
						}
					}
					from := v.ViewStatus
					v.recordTransition(StatusPendingRebuild, "reconcile:"+reason, now)
					found = append(found, Inconsistency{
						Adapter: an, View: vn, From: from, To: StatusPendingRebuild, Reason: reason,
					})
				}
			case StatusPendingRecompatCheck, StatusPendingRebuild, StatusDSLUpdateRequired:
				// No reconcile action: pending-rebuild and dsl-update-required
				// are handled by their own flows; pending-recompat-check is
				// re-validated by `da kg view validate`, not the reconcile pass.
			default:
				// Unknown/empty status: force pending-rebuild fail-closed.
				from := v.ViewStatus
				v.recordTransition(StatusPendingRebuild, "reconcile:invalid-status", now)
				found = append(found, Inconsistency{
					Adapter: an, View: vn, From: from, To: StatusPendingRebuild, Reason: "invalid view_status",
				})
			}
		}
	}
	return found
}
