package config

import "errors"

// ensure_resolved.go implements the auto-sync seam every config-consuming
// command calls (config-distribution-model §7A.5). EnsureResolved makes one
// cheap, local, clock-free staleness decision (via staleness.go) and then
// chooses exactly one resolution path:
//
//   - Frozen   → use the lock as-is, skip the staleness check entirely.
//   - Locked   → assert the lock is fresh; if it WOULD change, error and write
//     NOTHING (the CI gate — caller maps the sentinel to a non-zero exit).
//   - Offline  → resolve from lock/cache only, never the network.
//   - Default  → fresh ⇒ no-op (return the resolved snapshot, no write);
//     stale ⇒ re-resolve (rewrites the lock) and return.
//
// This seam owns the LOCK half of §7A.5 only. The outputs/projection (sync)
// half is the caller's concern; NoSync is carried through on the result so the
// caller can honor `--no-sync` without re-deriving it.

// ErrLockWouldChange is the sentinel returned by EnsureResolved in Locked mode
// when the committed lock is stale (re-resolving WOULD rewrite it). It is the
// `--locked` CI assertion: the caller maps it to a non-zero exit. When this is
// returned, EnsureResolved has written nothing.
var ErrLockWouldChange = errors.New("config: lock would change (--locked assertion failed)")

// EnsureOpts selects the resolution mode for EnsureResolved. The four bools map
// 1:1 to the §7A.5 flags (--locked / --frozen / --no-sync / --offline). The
// remaining fields are the existing test seams threaded through so the seam
// stays hermetic: no network, no real clock.
type EnsureOpts struct {
	// Locked asserts the lock is fresh: a stale lock yields ErrLockWouldChange
	// and no write (CI gate).
	Locked bool
	// Frozen uses the lock as-is and skips the staleness check entirely.
	Frozen bool
	// NoSync skips the caller's outputs/projection step. It does not affect the
	// lock decision here; it is recorded on the result for the caller.
	NoSync bool
	// Offline resolves from lock/cache only and never contacts the network.
	Offline bool

	// UserLocalPath is the resolver's user-local manifest seam, threaded into the
	// staleness inputs_digest computation so it honors the same override the
	// resolver uses. Empty ⇒ default <AgentsHome>/.agentsrc.json.
	UserLocalPath string
	// UnitDigest is the staleness per-unit digest seam (staleness.go). Nil skips
	// the per-unit digest driver event (inputs_digest + declared-set only).
	UnitDigest UnitDigestFunc
	// Resolver overrides the resolver seam (test injection of a fake). Nil ⇒ a
	// default NewLayeredResolver(). Both Resolve (rewrites the lock) and
	// ResolveLocked (read-only) stay behind this interface so tests never touch
	// the network.
	Resolver EnsureResolverSeam
}

// EnsureResolverSeam is the resolution surface EnsureResolved drives: the
// rewriting Resolve path and the read-only ResolveLocked path. *LayeredResolver
// satisfies it; tests inject a fake so no resolution touches the network.
type EnsureResolverSeam interface {
	// Resolve rebuilds the layer stack and REWRITES the lock.
	Resolve(projectPath string) (*Snapshot, error)
	// ResolveLocked rebuilds the snapshot from the lock/cache WITHOUT any fetch
	// or write.
	ResolveLocked(projectPath string) (*Snapshot, error)
}

// *LayeredResolver is the production EnsureResolverSeam.
var _ EnsureResolverSeam = (*LayeredResolver)(nil)

// EnsureResult is the outcome of EnsureResolved: the effective-config Snapshot
// plus the metadata the caller needs to decide what to do next (whether the
// lock was rewritten, whether the lock was already fresh, and the carried-over
// NoSync flag for the outputs step).
type EnsureResult struct {
	// Snapshot is the resolved effective config. Always non-nil on a nil error.
	Snapshot *Snapshot
	// ReResolved is true when the default-mode stale path ran Resolve and
	// rewrote the lock. It is false for the Frozen, Offline, fresh, and (error)
	// Locked paths — none of which write.
	ReResolved bool
	// Fresh is true when the staleness check reported no driver event. It is
	// false in Frozen mode (the check is skipped) and on any stale path.
	Fresh bool
	// NoSync echoes EnsureOpts.NoSync so the caller can gate its outputs step
	// without re-deriving it.
	NoSync bool
	// Reasons lists the driver events that made the lock stale (empty when fresh
	// or Frozen). Surfaced so the caller can explain why a re-resolve happened.
	Reasons []StalenessReason
}

// EnsureResolved is the §7A.5 auto-sync seam. It computes staleness once (unless
// Frozen) and dispatches to exactly one resolution path. It owns the lock half
// of §7A.5 only; the caller owns the outputs/projection half (gated by NoSync,
// echoed on the result).
//
// It is hermetic: every resolution goes through opts.Resolver (default
// LayeredResolver) and staleness through the injected UnitDigest seam, so no
// path here contacts the network or a clock directly.
func EnsureResolved(projectPath string, opts EnsureOpts) (*EnsureResult, error) {
	resolver := opts.resolver()

	// Frozen short-circuits before any staleness work: the lock is authoritative.
	if opts.Frozen {
		return readOnlyResult(resolver, projectPath, opts.NoSync)
	}

	stale, err := Staleness(projectPath, opts.UserLocalPath, opts.UnitDigest)
	if err != nil {
		return nil, err
	}

	switch {
	case opts.Locked:
		return lockedResult(resolver, projectPath, opts.NoSync, stale)
	case opts.Offline:
		return offlineResult(resolver, projectPath, opts.NoSync, stale)
	default:
		return defaultResult(resolver, projectPath, opts.NoSync, stale)
	}
}

// resolver returns the configured resolver seam, defaulting to a real
// LayeredResolver when the test seam is unset.
func (o EnsureOpts) resolver() EnsureResolverSeam {
	if o.Resolver != nil {
		return o.Resolver
	}
	return NewLayeredResolver()
}

// readOnlyResult resolves from the lock/cache without any staleness metadata.
// It backs the Frozen path: the lock is used as-is, so the result reports
// neither Fresh nor stale reasons.
func readOnlyResult(r EnsureResolverSeam, projectPath string, noSync bool) (*EnsureResult, error) {
	snap, err := r.ResolveLocked(projectPath)
	if err != nil {
		return nil, err
	}
	return &EnsureResult{Snapshot: snap, NoSync: noSync}, nil
}

// lockedResult is the --locked CI assertion path. A stale lock WOULD change, so
// it returns ErrLockWouldChange and writes nothing; a fresh lock resolves
// read-only.
func lockedResult(r EnsureResolverSeam, projectPath string, noSync bool, stale StalenessResult) (*EnsureResult, error) {
	if stale.IsStale() {
		return nil, ErrLockWouldChange
	}
	return freshResult(r, projectPath, noSync, stale)
}

// offlineResult resolves from the lock/cache only, never the network, carrying
// the staleness verdict through for the caller without re-resolving.
func offlineResult(r EnsureResolverSeam, projectPath string, noSync bool, stale StalenessResult) (*EnsureResult, error) {
	snap, err := r.ResolveLocked(projectPath)
	if err != nil {
		return nil, err
	}
	return staleResult(snap, false, noSync, stale), nil
}

// defaultResult is the no-flag path: a fresh lock is a read-only no-op; a stale
// lock triggers a re-resolve that rewrites the lock.
func defaultResult(r EnsureResolverSeam, projectPath string, noSync bool, stale StalenessResult) (*EnsureResult, error) {
	if stale.Fresh {
		return freshResult(r, projectPath, noSync, stale)
	}
	snap, err := r.Resolve(projectPath)
	if err != nil {
		return nil, err
	}
	return staleResult(snap, true, noSync, stale), nil
}

// freshResult resolves read-only and tags the result Fresh — the shared "lock
// matches, no write" outcome of the Locked and default paths.
func freshResult(r EnsureResolverSeam, projectPath string, noSync bool, stale StalenessResult) (*EnsureResult, error) {
	snap, err := r.ResolveLocked(projectPath)
	if err != nil {
		return nil, err
	}
	return &EnsureResult{Snapshot: snap, Fresh: true, NoSync: noSync, Reasons: stale.Reasons}, nil
}

// staleResult assembles a result for a stale-lock path, recording whether the
// lock was rewritten (reResolved) and the driver events that fired.
func staleResult(snap *Snapshot, reResolved, noSync bool, stale StalenessResult) *EnsureResult {
	return &EnsureResult{
		Snapshot:   snap,
		ReResolved: reResolved,
		Fresh:      false,
		NoSync:     noSync,
		Reasons:    stale.Reasons,
	}
}
