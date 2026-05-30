package config

import (
	"os"
	"sort"
	"time"
)

// lockstatus.go is the READ-ONLY lock inspection surface consumed by
// `da doctor` and `da status` (config-v2 p2). It wraps the resolver's
// unexported readLockedLayers and adds a drift comparator that diffs the
// committed .agentsrc.lock against the project's declared `extends` layers.
//
// Nothing in this file writes the lockfile — drift is reported, never
// repaired. The resolver (internal/config/resolver.go) remains the sole
// writer (WriteConfigLock). Keep all p2 lock-inspection code here so the
// resolver stays owned by its concurrent worker.

// ReadLockedLayers loads the "config" section of a project's .agentsrc.lock,
// returning an empty map when the file or section is absent. It is the
// exported, read-only companion to the resolver's WriteConfigLock: doctor and
// status call it to surface the last-resolved SHA / TTL state without invoking
// any fetch or resolve. The map key is the layer ref ("acme:org/base").
func ReadLockedLayers(projectPath string) (map[string]LockedLayer, error) {
	return readLockedLayers(projectPath)
}

// LockDriftStatus classifies a single declared/locked layer's drift state.
type LockDriftStatus string

const (
	// LockStatusOK means the declared extends entry has a matching lock entry
	// whose TTL (if any) has not expired.
	LockStatusOK LockDriftStatus = "ok"
	// LockStatusMissingFromLock means the layer is declared in .agentsrc.json
	// `extends` but has no entry in the lockfile — the project was never
	// resolved, or the lock predates the layer (run `da install` / sync).
	LockStatusMissingFromLock LockDriftStatus = "missing-from-lock"
	// LockStatusExtraInLock means the lockfile carries an entry for a ref that
	// is no longer declared in `extends` — a stale leftover after the layer was
	// removed from the manifest.
	LockStatusExtraInLock LockDriftStatus = "extra-in-lock"
	// LockStatusTTLExpired means the locked layer's TTLExpiresAt is in the past
	// — the cached SHA is due for a re-check (`da config sync`).
	LockStatusTTLExpired LockDriftStatus = "ttl-expired"
)

// LockLayerDrift is one layer's drift record: its ref, the classification, and
// (when present) the locked SHA / TTL so doctor/status can render a one-line
// diagnostic without re-reading the lockfile.
type LockLayerDrift struct {
	// Ref is the layer reference ("source-id:layer-path[@version]").
	Ref string
	// Status classifies this layer's drift.
	Status LockDriftStatus
	// ResolvedSHA is the locked SHA, empty for missing-from-lock entries.
	ResolvedSHA string
	// TTLExpiresAt is the locked TTL expiry (RFC3339), empty when the layer has
	// no TTL or no lock entry.
	TTLExpiresAt string
}

// LockDriftResult is the renderable outcome of comparing a project's
// .agentsrc.lock against its declared `extends`. It is designed so doctor and
// status can branch on a few booleans and iterate a single sorted slice:
//
//   - LockPresent false  → no lockfile at all (only meaningful when extends are
//     declared; a project with no extends and no lock is simply local).
//   - HasExtends false   → the manifest declares no extends layers, so lock
//     drift is not applicable.
//   - Layers             → per-layer drift records, sorted by Ref, covering
//     every declared and every locked ref (the union).
//
// IsClean reports the common "nothing to surface" case.
type LockDriftResult struct {
	// LockPresent is true when a .agentsrc.lock file exists for the project.
	LockPresent bool
	// HasExtends is true when the manifest declares at least one `extends`
	// layer (lock drift is only applicable to extends-bearing manifests).
	HasExtends bool
	// Layers holds one record per ref in the union of declared extends and
	// locked entries, sorted by Ref. Records with LockStatusOK are included so
	// callers can render a healthy summary count.
	Layers []LockLayerDrift
}

// IsClean reports whether the result has no drift to surface: either the
// manifest declares no extends, or every declared layer is locked, TTL-fresh,
// and the lock carries no extra entries.
func (r LockDriftResult) IsClean() bool {
	if !r.HasExtends {
		return true
	}
	for _, l := range r.Layers {
		if l.Status != LockStatusOK {
			return false
		}
	}
	return true
}

// Problems returns the subset of Layers whose status is not OK, preserving the
// sorted order. Convenience for doctor/status drift-only rendering.
func (r LockDriftResult) Problems() []LockLayerDrift {
	var out []LockLayerDrift
	for _, l := range r.Layers {
		if l.Status != LockStatusOK {
			out = append(out, l)
		}
	}
	return out
}

// lockDriftClock is the time seam for TTL-expiry comparison. Tests override it
// to make ttl-expired classification deterministic; production uses time.Now.
var lockDriftClock = time.Now

// LockDrift compares a project's committed .agentsrc.lock against the `extends`
// layers declared in its .agentsrc.json and reports the per-layer drift. It is
// strictly read-only: it never writes or repairs the lockfile.
//
// Drift dimensions reported (spec §7):
//   - a declared extends layer absent from the lock (missing-from-lock),
//   - a locked entry no longer declared in extends (extra-in-lock),
//   - a locked entry whose TTLExpiresAt is in the past (ttl-expired).
//
// A manifest with no `extends` yields HasExtends=false and an empty Layers
// slice (lock drift is not applicable). A missing manifest surfaces as the
// LoadAgentsRC error; a missing lockfile is not an error (LockPresent=false),
// since the absence is itself the drift to report against declared extends.
func LockDrift(projectPath string) (LockDriftResult, error) {
	rc, err := LoadAgentsRC(projectPath)
	if err != nil {
		return LockDriftResult{}, err
	}

	locked, err := readLockedLayers(projectPath)
	if err != nil {
		return LockDriftResult{}, err
	}

	res := LockDriftResult{
		LockPresent: lockFileExists(projectPath),
		HasExtends:  len(rc.Extends) > 0,
	}

	declared := make(map[string]bool, len(rc.Extends))
	now := lockDriftClock()

	// Walk declared extends first: each is either locked (OK / ttl-expired) or
	// missing-from-lock.
	for _, ext := range rc.Extends {
		declared[ext.Ref] = true
		entry, ok := locked[ext.Ref]
		if !ok {
			res.Layers = append(res.Layers, LockLayerDrift{
				Ref:    ext.Ref,
				Status: LockStatusMissingFromLock,
			})
			continue
		}
		res.Layers = append(res.Layers, LockLayerDrift{
			Ref:          ext.Ref,
			Status:       classifyLockedLayer(entry, now),
			ResolvedSHA:  entry.ResolvedSHA,
			TTLExpiresAt: entry.TTLExpiresAt,
		})
	}

	// Locked entries with no matching declared extends are stale leftovers.
	for ref, entry := range locked {
		if declared[ref] {
			continue
		}
		res.Layers = append(res.Layers, LockLayerDrift{
			Ref:          ref,
			Status:       LockStatusExtraInLock,
			ResolvedSHA:  entry.ResolvedSHA,
			TTLExpiresAt: entry.TTLExpiresAt,
		})
	}

	sort.Slice(res.Layers, func(i, j int) bool {
		return res.Layers[i].Ref < res.Layers[j].Ref
	})
	return res, nil
}

// classifyLockedLayer returns the drift status for a layer that exists in both
// the manifest and the lock: ttl-expired when its TTLExpiresAt is a parseable
// timestamp in the past, otherwise ok. An empty TTL means "never auto-re-check"
// (spec §7) and is treated as ok. An unparseable TTL is conservatively treated
// as ok rather than forcing a spurious re-check.
func classifyLockedLayer(entry LockedLayer, now time.Time) LockDriftStatus {
	if entry.TTLExpiresAt == "" {
		return LockStatusOK
	}
	expiry, err := time.Parse(time.RFC3339, entry.TTLExpiresAt)
	if err != nil {
		return LockStatusOK
	}
	if now.After(expiry) {
		return LockStatusTTLExpired
	}
	return LockStatusOK
}

// lockFileExists reports whether the project has a .agentsrc.lock on disk,
// resolved through the shared AgentsLockPath helper so the location can never
// drift from the resolver's writer.
func lockFileExists(projectPath string) bool {
	_, err := os.Stat(AgentsLockPath(projectPath))
	return err == nil
}
