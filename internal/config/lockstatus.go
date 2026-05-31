package config

import (
	"os"
	"sort"
	"time"
)

// lockstatus.go is the READ-ONLY lock inspection surface consumed by
// `da doctor` and `da config explain` (config-v2 p2, reshaped for §7A).
//
// Under the §7A unified model staleness is content-hash driven, not clock
// driven (see staleness.go). This file owns the two read-only surfaces that
// build on that:
//
//   - LockDrift — the declared-vs-locked *driver-event* drift: a declared
//     `extends`/`packages` unit missing from the lock, or a locked unit no
//     longer declared. There is NO ttl-expired drift status: cache_ttl never
//     auto-invalidates the lock (§7A.3).
//   - ReviewNudge — the demoted-TTL surface: cache_ttl is reframed as a
//     review-nudge window, and last_checked_at (per unit) powers a "last
//     re-checked N ago — da config sync" reminder. It NEVER invalidates the
//     lock; it is advisory only.
//
// Nothing here writes the lockfile — drift and nudges are reported, never
// repaired. The resolver remains the sole writer.

// ReadLockedUnits loads the §7A "units" section of a project's .agentsrc.lock
// (migrating a legacy config/packages lockfile in memory when needed), returning
// an empty map when the file or section is absent. It is the exported,
// read-only companion doctor and config explain call to surface resolved unit
// digests / last-checked state without invoking any fetch or resolve. The map
// key is the resolved unit ref ("source:path@version").
func ReadLockedUnits(projectPath string) (map[string]LockedUnit, error) {
	lock, err := ReadUnits(projectPath)
	if err != nil {
		return nil, err
	}
	return lock.Units, nil
}

// LockDriftStatus classifies a single declared/locked unit's drift state.
type LockDriftStatus string

const (
	// LockStatusOK means the declared unit has a matching lock entry. (Content
	// staleness is a separate, clock-free axis — see staleness.go — not a drift
	// status, because it does not mean the lock is structurally wrong.)
	LockStatusOK LockDriftStatus = "ok"
	// LockStatusMissingFromLock means the unit is declared in .agentsrc.json
	// (`extends` or `packages`) but has no entry in the lockfile — the project
	// was never resolved, or the lock predates the declaration (run
	// `da install` / `da config sync`).
	LockStatusMissingFromLock LockDriftStatus = "missing-from-lock"
	// LockStatusExtraInLock means the lockfile carries an entry for a unit that
	// is no longer declared — a stale leftover after the declaration was removed
	// from the manifest.
	LockStatusExtraInLock LockDriftStatus = "extra-in-lock"
)

// LockUnitDrift is one unit's drift record: its declared ref, the
// classification, and (when present) the locked digest / kind so doctor and
// config explain can render a one-line diagnostic without re-reading the lock.
type LockUnitDrift struct {
	// Ref is the declared unit reference ("source-id:path[@version]").
	Ref string
	// Status classifies this unit's drift.
	Status LockDriftStatus
	// Digest is the locked content digest, empty for missing-from-lock entries.
	Digest string
	// Kind is the locked unit kind (layer|artifact), empty for
	// missing-from-lock entries.
	Kind string
}

// LockDriftResult is the renderable outcome of comparing a project's
// .agentsrc.lock against its declared `extends`/`packages` units. Callers branch
// on a few booleans and iterate a single sorted slice:
//
//   - LockPresent false  → no lockfile at all (only meaningful when units are
//     declared; a project with no extends/packages and no lock is simply local).
//   - HasDeclaredUnits false → the manifest declares no units, so lock drift is
//     not applicable.
//   - Units → per-unit drift records, sorted by Ref, covering every declared and
//     every locked ref (the union).
//
// IsClean reports the common "nothing to surface" case.
type LockDriftResult struct {
	// LockPresent is true when a .agentsrc.lock file exists for the project.
	LockPresent bool
	// HasDeclaredUnits is true when the manifest declares at least one `extends`
	// or `packages` unit (lock drift is only applicable to such manifests).
	HasDeclaredUnits bool
	// Units holds one record per ref in the union of declared and locked units,
	// sorted by Ref. Records with LockStatusOK are included so callers can render
	// a healthy summary count.
	Units []LockUnitDrift
}

// IsClean reports whether the result has no drift to surface: either the
// manifest declares no units, or every declared unit is locked and the lock
// carries no extra entries.
func (r LockDriftResult) IsClean() bool {
	if !r.HasDeclaredUnits {
		return true
	}
	for _, u := range r.Units {
		if u.Status != LockStatusOK {
			return false
		}
	}
	return true
}

// Problems returns the subset of Units whose status is not OK, preserving the
// sorted order. Convenience for doctor / config explain drift-only rendering.
func (r LockDriftResult) Problems() []LockUnitDrift {
	var out []LockUnitDrift
	for _, u := range r.Units {
		if u.Status != LockStatusOK {
			out = append(out, u)
		}
	}
	return out
}

// LockDrift compares a project's committed .agentsrc.lock against the
// `extends`/`packages` units declared in its .agentsrc.json and reports the
// per-unit *driver-event* drift. It is strictly read-only: it never writes or
// repairs the lockfile, and it never consults a clock (§7A.3 moves staleness off
// the TTL axis entirely).
//
// Drift dimensions reported:
//   - a declared unit absent from the lock (missing-from-lock),
//   - a locked unit no longer declared (extra-in-lock).
//
// A manifest with no declared units yields HasDeclaredUnits=false and an empty
// Units slice. A missing manifest surfaces as the LoadAgentsRC error; a missing
// lockfile is not an error (LockPresent=false), since the absence is itself the
// drift to report against declared units.
func LockDrift(projectPath string) (LockDriftResult, error) {
	rc, err := LoadAgentsRC(projectPath)
	if err != nil {
		return LockDriftResult{}, err
	}

	locked, err := ReadLockedUnits(projectPath)
	if err != nil {
		return LockDriftResult{}, err
	}

	declared := declaredUnitRefs(*rc)
	res := LockDriftResult{
		LockPresent:      lockFileExists(projectPath),
		HasDeclaredUnits: len(declared) > 0,
	}

	// Index locked entries by their declared-ref form so a declared ref (which
	// may omit or pin a version) matches its resolved lock key.
	lockedByRef := indexLockedByRef(locked)

	for ref := range declared {
		entry, ok := lockedByRef[ref]
		if !ok {
			res.Units = append(res.Units, LockUnitDrift{Ref: ref, Status: LockStatusMissingFromLock})
			continue
		}
		res.Units = append(res.Units, LockUnitDrift{
			Ref:    ref,
			Status: LockStatusOK,
			Digest: entry.Digest,
			Kind:   entry.Kind,
		})
	}

	for ref, entry := range lockedByRef {
		if declared[ref] {
			continue
		}
		res.Units = append(res.Units, LockUnitDrift{
			Ref:    ref,
			Status: LockStatusExtraInLock,
			Digest: entry.Digest,
			Kind:   entry.Kind,
		})
	}

	sort.Slice(res.Units, func(i, j int) bool { return res.Units[i].Ref < res.Units[j].Ref })
	return res, nil
}

// indexLockedByRef keys locked units by their declared-ref form (the resolved
// key with its "@version" suffix trimmed) so LockDrift can match manifest refs
// against resolved lock keys. When two resolved keys collapse to the same
// declared ref (a version change for the same unit), the last wins — drift only
// cares about presence of the declared ref, and the digest/kind shown is
// representative.
func indexLockedByRef(units map[string]LockedUnit) map[string]LockedUnit {
	out := make(map[string]LockedUnit, len(units))
	for key, entry := range units {
		out[declaredRefOf(key)] = entry
	}
	return out
}

// reviewNudgeClock is the time seam for the "last re-checked N ago" computation.
// Tests override it for determinism; production uses time.Now.
var reviewNudgeClock = time.Now

// ReviewNudge is the demoted-TTL advisory for one unit (§7A.3): how long since
// its last upstream re-check. It NEVER invalidates the lock — it only drives a
// "last re-checked N ago — da config sync" reminder in doctor / config explain.
type ReviewNudge struct {
	// Ref is the resolved unit ref ("source:path@version").
	Ref string
	// LastCheckedAt is the RFC3339 timestamp of the last upstream re-check
	// (falls back to fetched_at when never re-checked since fetch). Empty when
	// the unit records neither.
	LastCheckedAt string
	// SinceLastCheck is how long ago the last re-check was, relative to the
	// review-nudge clock. Zero when LastCheckedAt is empty or unparseable.
	SinceLastCheck time.Duration
}

// ReviewNudges returns the per-unit review-nudge advisories for a project's
// locked units, sorted by ref. Each reports how long since the unit was last
// re-checked upstream (last_checked_at, or fetched_at as the basis when never
// re-checked). This is the demoted-TTL surface: it is advisory only and never
// affects staleness or the lock. Units with no timestamp basis are still
// returned (with a zero duration) so the surface lists every locked unit.
func ReviewNudges(projectPath string) ([]ReviewNudge, error) {
	locked, err := ReadLockedUnits(projectPath)
	if err != nil {
		return nil, err
	}
	now := reviewNudgeClock()
	nudges := make([]ReviewNudge, 0, len(locked))
	for _, ref := range sortedUnitKeys(locked) {
		nudges = append(nudges, reviewNudgeFor(ref, locked[ref], now))
	}
	return nudges, nil
}

// reviewNudgeFor builds a single unit's review-nudge: it prefers last_checked_at
// as the basis, falling back to fetched_at when the unit was never re-checked,
// and computes the elapsed duration. An empty or unparseable basis yields a
// zero duration (the surface still lists the unit).
func reviewNudgeFor(ref string, unit LockedUnit, now time.Time) ReviewNudge {
	basis := unit.LastCheckedAt
	if basis == "" {
		basis = unit.FetchedAt
	}
	nudge := ReviewNudge{Ref: ref, LastCheckedAt: basis}
	if basis == "" {
		return nudge
	}
	t, err := time.Parse(time.RFC3339, basis)
	if err != nil {
		return nudge
	}
	if d := now.Sub(t); d > 0 {
		nudge.SinceLastCheck = d
	}
	return nudge
}

// lockFileExists reports whether the project has a .agentsrc.lock on disk,
// resolved through the shared AgentsLockPath helper so the location can never
// drift from the resolver's writer.
func lockFileExists(projectPath string) bool {
	_, err := os.Stat(AgentsLockPath(projectPath))
	return err == nil
}
