// Package agentslock is the single shared writer for .agentsrc.lock — the
// resolved-state companion to .agentsrc.json (config-distribution-model §7).
//
// It is schema-agnostic: it owns the whole JSON document and treats top-level
// sections (config, packages, adapters, …) as opaque values, so the config/
// package resolver and the graph-adapter lifecycle share one file without
// either importing the other's schema (§7.4). A writer stages only its own
// section and flushes; sibling sections are preserved verbatim. Flush is
// atomic (temp file + rename, via fsops.WriteFileAtomic). A single Lockfile is
// safe for concurrent SetSection from parallel resolver goroutines. Flush also
// takes a portable sidecar-directory lock, rereads the latest on-disk document,
// and reapplies only this process's staged top-level keys before the atomic
// write. That keeps sibling sections written by another process from being lost
// while preserving the §7.4 "parallel resolution, serialized write" contract.
package agentslock

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AGOrcha/dot-agents/internal/fsops"
)

// isDeletePendingLockErr reports whether a failed lock-dir Mkdir is the Windows
// "delete pending" transient — a contender's os.Mkdir racing an in-place delete
// of the lock dir (an older da release, or trash churn in the parent) observes
// ERROR_ACCESS_DENIED (mapped to a permission error) while the directory is
// mid-delete, rather than the portable ERROR_ALREADY_EXISTS.
// It clears within a retry, so the acquisition loop treats it as retryable
// contention. Gated on goos == "windows" so a genuine permission error stays a
// fast failure on other platforms (where this race does not occur). goos is a
// parameter rather than a direct runtime.GOOS read so the predicate is unit-
// testable for the Windows branch from any host.
func isDeletePendingLockErr(err error, goos string) bool {
	return goos == "windows" && os.IsPermission(err)
}

// lockGOOS is the GOOS the acquire loop classifies Mkdir failures under. A
// package var (defaulting to runtime.GOOS) for the same reason
// isDeletePendingLockErr takes goos as a parameter: the Windows-only
// classification branches are unit-testable from any host.
var lockGOOS = runtime.GOOS

// LockVersion is the current .agentsrc.lock schema version.
const LockVersion = 1

// lockVersionKey is the reserved top-level key holding LockVersion. It is not a
// section and cannot be set via SetSection.
const lockVersionKey = "lock_version"

// inputsDigestKey is the reserved top-level key holding the resolver's
// whole-normalized hash of all local config scopes (config-distribution-model
// §7A.3). Like lockVersionKey it is a scalar top-level field, not a section, so
// it cannot be staged via SetSection; use SetInputsDigest / InputsDigest.
const inputsDigestKey = "inputs_digest"

const (
	lockAcquireTimeout = 5 * time.Second
	lockRetryInterval  = 10 * time.Millisecond
	// lockStaleTTL bounds how long an unreleased lock dir is tolerated before a
	// contending writer treats it as orphaned and reclaims it. The mkdir lock
	// has no kernel-backed auto-release, so a holder killed by SIGKILL/OOM/power
	// loss would otherwise wedge every future Flush permanently. The TTL is set
	// well above the expected sub-second hold time (a single read-merge-write)
	// so a live, slow holder is never reclaimed out from under itself, yet a
	// crashed holder's lock self-clears within the TTL rather than forever.
	lockStaleTTL = 30 * time.Second
	// lockNoHolderGrace bounds how long a lock dir with NO readable holder file
	// is tolerated before a contender treats it as a remnant and reclaims it.
	// Only two states look like this: the live mid-acquire window between
	// os.Mkdir and writeHolder (two local fs ops — microseconds, so 2s is a
	// >1000x margin), and a crash/partial-release remnant (holder gone, dir
	// stuck). The remnant case MUST clear well inside lockAcquireTimeout:
	// judging it by the full lockStaleTTL (as the old dir-mtime fallback did)
	// meant a 30s grace against a 5s acquire budget — every contender and every
	// fresh da process was stranded into a guaranteed timeout.
	// Ordering invariant: release rename retries (~300ms) < lockNoHolderGrace
	// < lockAcquireTimeout < lockStaleTTL.
	lockNoHolderGrace = 2 * time.Second
	// holderFile is the name of the sidecar metadata file written inside the
	// lock dir recording the acquiring PID and acquisition time, used to detect
	// and reclaim stale locks. Its contents double as the acquisition's identity
	// for the release-time ownership check.
	holderFile = "holder"
	// lockTrashInfix joins a lock-dir path to the unique suffix of its
	// renamed-away trash sibling: "<lockdir>.stale-<pid>-<nanos>". Release and
	// stale reclaim free the lock NAME with one atomic rename to such a sibling
	// and delete the trash out-of-band; the name itself is never deleted in
	// place (see releaseLockDir).
	lockTrashInfix = ".stale-"
	// releaseRenameAttempts/Backoff bound the holder's release retry of the
	// single atomic rename. On Windows a contender's probe handle or an AV scan
	// can transiently pin the dir against rename; the pin clears within
	// milliseconds, so a brief bounded retry (~275ms total, well under
	// lockNoHolderGrace) is the correct semantic. Retrying is safe here —
	// unlike reclaim — because the dir stays OURS until the rename lands.
	releaseRenameAttempts = 12
	releaseRenameBackoff  = 25 * time.Millisecond
	// deniedTransientAttempts bounds how many CONSECUTIVE permission-denied
	// Mkdir results the Windows delete-pending accommodation absorbs before the
	// denial is classified as real. The delete-pending transient clears within a
	// retry or two; 30 ticks (~300ms at lockRetryInterval) is orders of
	// magnitude beyond that, so a denial that survives it is an environment
	// problem — Controlled Folder Access / OneDrive-protected folders or AV
	// policy denying this binary (the field `da config explain` / `da install`
	// shape) — and must fail fast with the actual cause, not burn the remaining
	// acquire budget and misreport itself as a lock-contention timeout.
	deniedTransientAttempts = 30
)

// removeTrashFn deletes a renamed-away (trash) lock dir. A seam so tests can
// force "trash deletion failed" deterministically and prove the lock name is
// already free regardless; production always uses fsops.RemoveAll.
var removeTrashFn = fsops.RemoveAll

// renameLockDirFn is the seam over fsops.Rename for the release/reclaim
// lifecycle so tests can pin rename-failure interleavings deterministically;
// production always uses fsops.Rename.
var renameLockDirFn = fsops.Rename

// testHookAfterLockDirCreated, when non-nil, runs between the winning Mkdir
// and the holder claim in acquireLockDir. Tests use it to inject the
// mid-acquire stall interleaving (grace reclaim + successor acquisition)
// deterministically; production leaves it nil.
var testHookAfterLockDirCreated func(lockDir string)

// reservedKeys are the top-level scalar keys the writer manages itself. They are
// never valid section names — SetSection rejects them so a caller cannot
// accidentally overwrite a reserved field with an opaque section value.
var reservedKeys = map[string]bool{
	lockVersionKey:  true,
	inputsDigestKey: true,
}

// Lockfile is the in-memory view of a .agentsrc.lock document: open it, read or
// stage sections, then Flush. Safe for concurrent use.
type Lockfile struct {
	path  string
	mu    sync.Mutex
	doc   map[string]json.RawMessage // lock_version + one entry per section
	dirty map[string]bool
}

// Open loads the lockfile at path. A missing file yields a fresh document
// (lock_version only); a present file is parsed, preserving every top-level key
// — including sections this process does not know about.
func Open(path string) (*Lockfile, error) {
	lf := &Lockfile{path: path, doc: map[string]json.RawMessage{}, dirty: map[string]bool{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			lf.setVersion()
			return lf, nil
		}
		return nil, fmt.Errorf("agentslock: read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &lf.doc); err != nil {
		return nil, fmt.Errorf("agentslock: parse %s: %w", path, err)
	}
	if _, ok := lf.doc[lockVersionKey]; !ok {
		lf.setVersion()
	}
	return lf, nil
}

func (lf *Lockfile) setVersion() {
	v, _ := json.Marshal(LockVersion) // an int never fails to marshal
	lf.doc[lockVersionKey] = v
}

// Section decodes the named section into v and reports whether it was present.
// An absent section returns (false, nil) so callers can treat "no section yet"
// and "section exists" uniformly.
func (lf *Lockfile) Section(name string, v any) (bool, error) {
	lf.mu.Lock()
	raw, ok := lf.doc[name]
	lf.mu.Unlock()
	if !ok {
		return false, nil
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return false, fmt.Errorf("agentslock: decode section %q: %w", name, err)
	}
	return true, nil
}

// SetInputsDigest stages the top-level inputs_digest field (§7A.3): the
// whole-normalized hash of all local config scopes that drives staleness. An
// empty digest clears the field. Safe for concurrent use.
func (lf *Lockfile) SetInputsDigest(digest string) {
	lf.mu.Lock()
	defer lf.mu.Unlock()
	if digest == "" {
		delete(lf.doc, inputsDigestKey)
		lf.dirty[inputsDigestKey] = true
		return
	}
	raw, _ := json.Marshal(digest) // a string never fails to marshal
	lf.doc[inputsDigestKey] = raw
	lf.dirty[inputsDigestKey] = true
}

// InputsDigest returns the top-level inputs_digest and whether it was present.
// An absent or empty field reports ("", false).
func (lf *Lockfile) InputsDigest() (string, bool) {
	lf.mu.Lock()
	raw, ok := lf.doc[inputsDigestKey]
	lf.mu.Unlock()
	if !ok {
		return "", false
	}
	var digest string
	if err := json.Unmarshal(raw, &digest); err != nil || digest == "" {
		return "", false
	}
	return digest, true
}

// SetSection marshals v and stages it as the named section, leaving every other
// section untouched. Safe to call concurrently from multiple goroutines.
func (lf *Lockfile) SetSection(name string, v any) error {
	if reservedKeys[name] {
		return fmt.Errorf("agentslock: %q is reserved, not a section", name)
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("agentslock: encode section %q: %w", name, err)
	}
	lf.mu.Lock()
	lf.doc[name] = raw
	lf.dirty[name] = true
	lf.mu.Unlock()
	return nil
}

// Flush writes the whole document to path atomically, preserving every section.
// It is callable more than once (e.g. persist config before a slow adapter
// activation, then flush adapters after). The parent directory must exist.
func (lf *Lockfile) Flush() error {
	lf.mu.Lock()
	defer lf.mu.Unlock()
	unlock, err := acquireFileLock(lf.path)
	if err != nil {
		return err
	}
	defer unlock()
	if err := lf.mergeDiskLocked(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(lf.doc, "", "  ")
	if err != nil {
		return fmt.Errorf("agentslock: encode document: %w", err)
	}
	data = append(data, '\n')
	if err := fsops.WriteFileAtomic(lf.path, data); err != nil {
		return fmt.Errorf("agentslock: write %s: %w", lf.path, err)
	}
	return nil
}

func (lf *Lockfile) mergeDiskLocked() error {
	latest, err := readDocument(lf.path)
	if err != nil {
		return err
	}
	if latest == nil {
		latest = map[string]json.RawMessage{}
	}
	if _, ok := latest[lockVersionKey]; !ok {
		latest[lockVersionKey] = lf.doc[lockVersionKey]
	}
	for key := range lf.dirty {
		raw, ok := lf.doc[key]
		if !ok {
			delete(latest, key)
			continue
		}
		latest[key] = raw
	}
	lf.doc = latest
	return nil
}

func readDocument(path string) (map[string]json.RawMessage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("agentslock: read %s: %w", path, err)
	}
	doc := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("agentslock: parse %s: %w", path, err)
	}
	return doc, nil
}

// AcquireFileLock takes the package's advisory, inter-process lock guarding the
// file at path and returns a release function the caller MUST invoke to free it.
// It is the reusable form of the primitive that serializes .agentsrc.lock writes
// (see Flush); other cooperating da processes (e.g. an append-only NDJSON
// writer) call it to serialize their own writes to a shared file.
//
// The lock is a sidecar directory at "<path>.lock". Acquisition is a single
// atomic os.Mkdir: success is the mutual-exclusion signal, and EEXIST means
// another holder currently owns it. The lock is therefore ADVISORY — it excludes
// only other callers of this function that name the same path, not arbitrary
// writers of the underlying file. The parent directory of path is created if it
// does not yet exist.
//
// Acquisition blocks up to lockAcquireTimeout, retrying every lockRetryInterval,
// and returns a timeout error if a live holder never releases. Because the mkdir
// lock has no kernel-backed auto-release, a holder that crashed without releasing
// (SIGKILL/OOM/power loss) is detected as stale once its recorded age exceeds
// lockStaleTTL and is reclaimed at most once per call — so a slow but live holder
// is never torn down out from under itself. The returned release frees the lock
// name exactly once (atomic rename-away, see releaseLockDir) and reports an
// error only when the name could not be freed; it is once-guarded (a second
// call returns the first call's cached error without touching the filesystem).
//
// The once-guard is a correctness requirement, not a convenience: an unguarded
// release on every call would, after this caller released and another caller
// re-acquired the same path, tear down the new holder's live lock dir on a stray
// second release — silently breaking its mutual exclusion. Releasing at most once
// (plus the holder-identity check inside releaseLockDir) guarantees a duplicate
// or overdue release can never touch a dir this caller no longer owns.
func AcquireFileLock(path string) (release func() error, err error) {
	lockDir, holderID, err := acquireLockDir(path)
	if err != nil {
		return nil, err
	}
	var (
		once   sync.Once
		relErr error
	)
	return func() error {
		once.Do(func() { relErr = releaseLockDir(lockDir, holderID) })
		return relErr
	}, nil
}

// acquireFileLock is the internal release-returning-nothing form used by Flush.
// It shares acquireLockDir with the exported AcquireFileLock; the only
// difference is that a release error is surfaced via the package debug channel
// (Flush has no error path for the deferred unlock) rather than returned.
func acquireFileLock(path string) (func(), error) {
	lockDir, holderID, err := acquireLockDir(path)
	if err != nil {
		return nil, err
	}
	return func() { unlockFileLock(lockDir, holderID) }, nil
}

// acquireLockDir is the shared mkdir-as-lock acquisition core. It returns the
// sidecar lock-dir path it created (held) plus the holder identity it recorded
// (empty when the best-effort holder write failed); both AcquireFileLock and
// acquireFileLock wrap it with their respective release shapes.
func acquireLockDir(path string) (lockDir, holderID string, err error) {
	// Build the sidecar lock-dir path through filepath so it carries the
	// platform separator (backslashes on Windows) rather than whatever the
	// caller's `path` happened to use, and ensure its parent exists before the
	// first Mkdir. os.Mkdir (unlike MkdirAll) does NOT create intermediate
	// components: if the parent directory is absent it fails with ENOENT on unix
	// and ERROR_FILE_NOT_FOUND ("The system cannot find the file specified") on
	// Windows — the exact failure seen in the field, where the lock is taken
	// before any sibling writer (or fsops.WriteFileAtomic) has materialized the
	// directory. MkdirAll-ing only the parent keeps the lock-dir Mkdir itself a
	// single, atomic, EEXIST-distinguishable create — preserving the
	// contention/stale-reclaim semantics below — while removing the
	// missing-parent failure mode. A nil/empty parent (".") MkdirAll is a no-op.
	lockDir = filepath.Clean(path) + ".lock"
	if parent := filepath.Dir(lockDir); parent != "." && parent != "" {
		if err := fsops.MkdirAll(parent, 0o700); err != nil {
			return "", "", fmt.Errorf("agentslock: ensure lock parent %s: %w", parent, err)
		}
	}
	deadline := time.Now().Add(lockAcquireTimeout)
	st := acquireLoopState{}
	for {
		holderID, acquired, err := acquireTick(lockDir, &st)
		if err != nil {
			return "", "", err
		}
		if acquired {
			return lockDir, holderID, nil
		}
		if st.retryNow {
			st.retryNow = false
			continue
		}
		if time.Now().After(deadline) {
			return "", "", lockTimeoutError(lockDir)
		}
		time.Sleep(lockRetryInterval)
	}
}

// acquireLoopState carries the per-call bookkeeping across acquireTick
// iterations: the once-per-call stale-reclaim budget, the consecutive
// permission-denied streak, and whether the next retry should skip the sleep
// (a reclaim just freed the name).
type acquireLoopState struct {
	reclaimed bool
	denied    int
	retryNow  bool
}

// acquireTick performs one iteration of the acquisition loop: attempt the
// atomic Mkdir, then either complete the two-step acquisition (holder claim)
// or classify the failure. It returns the claimed holder identity with
// acquired=true, a fatal error, or neither (the caller waits and retries).
func acquireTick(lockDir string, st *acquireLoopState) (holderID string, acquired bool, err error) {
	// fsguard:allow os.Mkdir — atomic mkdir-as-lock primitive; see allowlist.go.
	// The single-component create here is the lock acquisition itself: its
	// success/EEXIST result is the mutual-exclusion signal. fsops has no
	// atomic-mkdir-lock equivalent, so this one call stays on raw os.Mkdir.
	mkdirErr := os.Mkdir(lockDir, 0o700)
	switch {
	case mkdirErr == nil:
		st.denied = 0
		if testHookAfterLockDirCreated != nil {
			testHookAfterLockDirCreated(lockDir)
		}
		if id := writeHolder(lockDir); id != "" {
			return id, true, nil
		}
		// Winning the Mkdir is only HALF the acquisition: the O_EXCL holder
		// claim is the other half, and it just failed. Either we lost the dir
		// mid-stall (a contender grace-reclaimed it and a successor already
		// claimed it, or it was renamed away), or the ownership token could not
		// be recorded. NEVER proceed identity-less — an unverifiable holder
		// could later release a successor's live lock. Rejoin the contention
		// loop (the deadline still bounds the caller); an abandoned holderless
		// dir of ours self-clears via lockNoHolderGrace.
		return "", false, nil
	case os.IsExist(mkdirErr):
		st.denied = 0 // the name exists; any prior denials were the transient
		// Contention: the lock dir already exists and is held. Before waiting,
		// decide whether the current holder is alive or stale (crashed without
		// releasing). A stale lock is reclaimed at most once per call — the
		// flag is set only when the reclaim RENAME actually lands, so a rename
		// lost to a rival reclaimer (or a transient Windows pin) does not burn
		// the budget, while a live holder racing us can still be torn down at
		// most once.
		if !st.reclaimed && lockIsStale(lockDir) && reclaimStaleLockDir(lockDir) {
			st.reclaimed = true
			st.retryNow = true
		}
		return "", false, nil
	case isDeletePendingLockErr(mkdirErr, lockGOOS):
		// Windows-only transient: a contender's Mkdir can observe
		// ERROR_ACCESS_DENIED while the dir is in the "delete pending" state
		// (an older release deleting in place, or trash churn in the parent).
		// It clears within a retry or two, so wait and retry rather than
		// failing the whole acquisition. Skip the stale-reclaim path: there is
		// no live holder to judge, just a dir mid-delete. A denial that
		// SURVIVES the transient window is not this race at all — it is a real
		// permission denial (folder protection / AV) and must fail fast with
		// the actual cause instead of a misleading contention timeout.
		st.denied++
		if st.denied >= deniedTransientAttempts {
			return "", false, deniedLockDirError(lockDir, mkdirErr)
		}
		return "", false, nil
	case os.IsPermission(mkdirErr):
		// Non-Windows permission denial: never a delete-pending transient, so
		// classify immediately with the actionable message.
		return "", false, deniedLockDirError(lockDir, mkdirErr)
	default:
		return "", false, fmt.Errorf("agentslock: acquire lock %s: %w", lockDir, mkdirErr)
	}
}

// lockTimeoutError renders the acquire-timeout failure, naming the blocking
// holder when its record is readable so a stranded invocation reports WHO
// holds the lock and when the TTL will self-heal it.
func lockTimeoutError(lockDir string) error {
	msg := "timed out"
	if held := describeHolder(lockDir); held != "" {
		msg += " (" + held + ")"
	}
	return fmt.Errorf("agentslock: acquire lock %s: %s", lockDir, msg)
}

// deniedLockDirError classifies a PERSISTENT access-denied on the lock-dir
// Mkdir. Field shape: Windows Controlled Folder Access / OneDrive-protected
// Documents or antivirus policy denying an unrecognized binary — observed as
// `da config explain --all` and `da install` failing on `.agentsrc.lock.lock`
// creation. Before this classification the Windows delete-pending
// accommodation retried the denial for the whole acquire budget and then
// reported a generic "timed out", misdiagnosing an environment problem as lock
// contention. Per the error-message contract the primary line says what
// failed; the follow-up names the likely causes and the concrete next step.
func deniedLockDirError(lockDir string, err error) error {
	return fmt.Errorf("agentslock: acquire lock %s: creating the lock directory is persistently denied (%w); "+
		"this is not lock contention — the parent folder %s is likely write-protected "+
		"(Windows Controlled Folder Access, OneDrive-protected Documents, or antivirus policy blocking this binary); "+
		"allow the da binary for that folder or move the repo outside the protected path, then retry",
		lockDir, err, filepath.Dir(lockDir))
}

// lockTrashName returns a unique trash sibling for lockDir. Uniqueness (pid +
// nanos) means trash names are never contended or re-acquired: deleting trash
// can only ever race other deleters of the same garbage, never a live lock.
func lockTrashName(lockDir string) string {
	return fmt.Sprintf("%s%s%d-%d", lockDir, lockTrashInfix, os.Getpid(), time.Now().UnixNano())
}

// reclaimStaleLockDir takes a stale lock dir out of the way with a SINGLE
// atomic rename to a unique trash name, then deletes the trash out-of-band.
// It reports whether the reclaim landed.
//
// Single-shot on purpose. The judge-then-reclaim sequence has an unavoidable
// TOCTOU window (staleness check and removal cannot be one atomic step over a
// directory), so the window is kept to exactly one rename syscall. The previous
// shape — fsops.RemoveAll(lockDir) — retried a delete against the lock NAME for
// up to ~300ms plus a PowerShell fallback: long enough for a rival contender to
// have reclaimed and re-acquired, at which point the still-running delete
// destroyed the rival's LIVE lock. A rename that loses that race fails with
// ENOENT (source gone) or finds a fresh dir that the caller's next lockIsStale
// pass will refuse to judge stale; either way the caller just keeps waiting.
func reclaimStaleLockDir(lockDir string) bool {
	trash := lockTrashName(lockDir)
	if err := renameLockDirFn(lockDir, trash); err != nil {
		return false
	}
	if err := removeTrashFn(trash); err != nil {
		debugf("agentslock: remove reclaimed lock remnant %s: %v", trash, err)
	}
	return true
}

// releaseLockDir frees the sidecar lock held at lockDir. Lifecycle contract
// (the fix for the lock-release lifecycle race): the lock NAME is only ever
// freed by a single atomic rename to a unique trash sibling — never by deleting
// in place. In-place deletion (the old fsops.RemoveAll) is not atomic: it
// removes the holder file, then the dir, with Windows retries in between, so
// contenders could observe a holderless "fresh" dir under the live lock name —
// a state the old staleness policy granted the full 30s TTL, stranding every
// 5s-budget acquirer into a guaranteed timeout — or an ACCESS_DENIED
// delete-pending name. After a successful rename the name is free no matter
// what happens to the trash: a failed trash deletion costs a uniquely-named
// leftover (swept by later releases), never lock availability.
//
// Before touching anything the release verifies identity: if the holder file no
// longer carries the contents this acquisition claimed (a contender
// TTL-reclaimed the lock — e.g. after this process sat suspended past the TTL —
// and re-acquired), the release is a no-op instead of stealing the new holder's
// lock. An empty holderID never passes the check: acquisition fails rather
// than proceed identity-less, so a legitimate release always carries the
// O_EXCL-claimed identity.
//
// The rename is retried briefly (releaseRenameAttempts x releaseRenameBackoff)
// because on Windows a contender's probe handle or an AV scan can transiently
// pin the dir; the dir remains ours until the rename lands, so retrying cannot
// touch anyone else's lock. If the rename never lands the remnant is left
// intact — holder file and all — and self-clears via the TTL; returning the
// error is the only remaining honest option.
func releaseLockDir(lockDir, holderID string) error {
	if !holderStillOurs(lockDir, holderID) {
		debugf("agentslock: skip release of %s: no longer the holder", lockDir)
		return nil
	}
	var err error
	for attempt := 0; attempt < releaseRenameAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(releaseRenameBackoff)
		}
		err = renameLockDirFn(lockDir, lockTrashName(lockDir))
		if err == nil {
			sweepLockTrash(lockDir)
			return nil
		}
		if os.IsNotExist(err) {
			return nil // already released or reclaimed out from under us
		}
	}
	return fmt.Errorf("agentslock: release lock %s: %w", lockDir, err)
}

// holderStillOurs reports whether the lock dir's holder file still carries the
// identity this acquisition claimed. An empty holderID can NEVER prove
// ownership: acquireLockDir no longer hands out empty identities (a failed
// O_EXCL claim fails the acquisition), so refusing here keeps any stray legacy
// or test caller from releasing a lock it cannot show it holds. An unreadable
// or mismatched holder means the lock was reclaimed or is mid-teardown by
// someone else — without proof of ownership the release must not touch the
// name (a wrongly-skipped release only leaves a remnant that the grace/TTL
// reclaim clears; a wrongly-performed release steals a live lock).
func holderStillOurs(lockDir, holderID string) bool {
	if holderID == "" {
		return false
	}
	data, err := os.ReadFile(filepath.Join(lockDir, holderFile))
	if err != nil {
		return false
	}
	return string(data) == holderID
}

// sweepLockTrash best-effort deletes every trash sibling of lockDir: the one
// the caller just renamed away plus any leftovers from earlier releases whose
// trash deletion failed. Runs only after the lock name is already free, so a
// sweep failure costs nothing but disk garbage (retried on the next release).
func sweepLockTrash(lockDir string) {
	parent := filepath.Dir(lockDir)
	prefix := filepath.Base(lockDir) + lockTrashInfix
	entries, err := os.ReadDir(parent)
	if err != nil {
		debugf("agentslock: sweep lock trash in %s: %v", parent, err)
		return
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), prefix) {
			continue
		}
		if err := removeTrashFn(filepath.Join(parent, e.Name())); err != nil {
			debugf("agentslock: remove lock trash %s: %v", e.Name(), err)
		}
	}
}

// describeHolder renders a short "held by pid P for D; stale after TTL" summary
// of the current lock holder for the acquire-timeout error, so a stranded `da`
// invocation reports WHO is blocking it and when self-healing will kick in. An
// absent or unparseable holder yields "" (the bare timeout message).
func describeHolder(lockDir string) string {
	data, err := os.ReadFile(filepath.Join(lockDir, holderFile))
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 2 {
		return ""
	}
	acquiredNanos, err := strconv.ParseInt(strings.TrimSpace(lines[1]), 10, 64)
	if err != nil {
		return ""
	}
	held := time.Since(time.Unix(0, acquiredNanos)).Round(time.Second)
	return fmt.Sprintf("held by pid %s for %s; stale after %s",
		strings.TrimSpace(lines[0]), held, lockStaleTTL)
}

// debugf surfaces non-fatal lock diagnostics (notably a failed unlock that would
// otherwise leave a stale lock silently). It is gated on DA_DEBUG so it stays
// quiet in normal operation without pulling in a logging dependency.
func debugf(format string, args ...any) {
	if os.Getenv("DA_DEBUG") == "" {
		return
	}
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}

// writeHolder atomically claims ownership of the lock dir at lockDir by
// creating the holder file with O_CREATE|O_EXCL and returns the recorded
// contents (PID + acquisition time) as this acquisition's identity. "" means
// the claim was LOST or unprovable — the caller must treat the acquisition as
// failed and must never proceed identity-less.
//
// The exclusive create is the second half of the two-step acquisition
// (mkdir-as-lock, then holder-as-claim) and closes the mid-acquire steal
// window: if the Mkdir winner stalls past lockNoHolderGrace, a contender may
// legitimately grace-reclaim (rename away) the holderless dir and a successor
// may re-create and claim it. O_EXCL guarantees at most ONE process ever
// records ownership of whatever dir sits at the lock name — the stalled
// winner's late claim hits EEXIST (a successor already holds) or ENOENT (the
// dir was renamed away) and correctly reports the loss instead of overwriting
// the successor's token or proceeding unverified.
//
// Raw os.OpenFile on purpose (os.OpenFile is not an fsguard-policed mutator,
// and no fsops helper fits): fsops.WriteFile is actively UNSAFE here — its
// Windows fallback MkdirAll-then-PowerShell "heals" a missing parent, which
// would RESURRECT a lock dir that a reclaimer just renamed away and silently
// convert a lost claim into a phantom acquisition. The claim must observe the
// dir's true state, never repair it.
func writeHolder(lockDir string) string {
	contents := fmt.Sprintf("%d\n%d\n", os.Getpid(), time.Now().UnixNano())
	holderPath := filepath.Join(lockDir, holderFile)
	f, err := os.OpenFile(holderPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		debugf("agentslock: claim holder in %s: %v", lockDir, err)
		return ""
	}
	_, werr := f.WriteString(contents)
	cerr := f.Close()
	if werr != nil || cerr != nil {
		// A half-written token is worse than none: retract it (provably ours —
		// the O_EXCL create succeeded) so the dir reads as holderless and clears
		// via the short grace instead of lingering as garbage under the TTL.
		_ = fsops.Remove(holderPath)
		debugf("agentslock: write holder in %s: %v / %v", lockDir, werr, cerr)
		return ""
	}
	return contents
}

// lockIsStale reports whether the existing lock dir should be treated as
// orphaned. A lock is stale when its recorded acquisition time is older than
// lockStaleTTL. The TTL alone is enough to guarantee no permanent deadlock; PID
// liveness is deliberately not consulted because it cannot be checked portably
// (notably on Windows).
//
// A dir with NO readable holder file gets the SHORT lockNoHolderGrace, judged
// by dir mtime, not the full TTL. That state is either the mid-acquire window
// between os.Mkdir and writeHolder (two local fs ops; the grace is a >1000x
// margin, so a healthy just-acquired lock is never torn down) or a
// crash/partial-release remnant — and a remnant must be reclaimable within a
// contender's lockAcquireTimeout budget. The old fallback granted such dirs the
// full 30s TTL: any release that removed the holder file but failed to remove
// the dir (Windows sharing violation / delete-pending) stranded every acquirer
// — including fresh `da config explain` / `da install` processes — into a
// guaranteed 5s timeout until the remnant aged out.
//
// A holder file that is readable but unparseable keeps the conservative
// TTL-by-mtime fallback: something wrote it, so it is judged like a recorded
// acquisition rather than a remnant.
func lockIsStale(lockDir string) bool {
	holderPath := filepath.Join(lockDir, holderFile)
	data, err := os.ReadFile(holderPath)
	if err != nil {
		return dirOlderThan(lockDir, lockNoHolderGrace)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 2 {
		return dirOlderThan(lockDir, lockStaleTTL) // unparseable
	}
	acquiredNanos, err := strconv.ParseInt(strings.TrimSpace(lines[1]), 10, 64)
	if err != nil {
		return dirOlderThan(lockDir, lockStaleTTL) // unparseable timestamp
	}
	age := time.Now().UnixNano() - acquiredNanos
	return age > int64(lockStaleTTL)
}

// dirOlderThan reports whether the lock dir's last-modified time is older than
// limit. If the dir can't be stat'd it is treated as stale (it likely no
// longer exists, in which case the retry will simply re-acquire).
func dirOlderThan(lockDir string, limit time.Duration) bool {
	info, err := os.Stat(lockDir)
	if err != nil {
		return true
	}
	return time.Since(info.ModTime()) > limit
}

// unlockFileLock is Flush's release shape: same releaseLockDir lifecycle, with
// a failure surfaced via the package debug channel (Flush has no error path for
// the deferred unlock). A rename that never lands leaves the holder file intact,
// so the remnant self-clears via the TTL rather than wedging forever.
func unlockFileLock(lockDir, holderID string) {
	if err := releaseLockDir(lockDir, holderID); err != nil {
		debugf("%v", err)
	}
}
