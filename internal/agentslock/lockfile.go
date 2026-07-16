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
// takes a portable single-file sidecar lock, rereads the latest on-disk document,
// and reapplies only this process's staged top-level keys before the atomic
// write. That keeps sibling sections written by another process from being lost
// while preserving the §7.4 "parallel resolution, serialized write" contract.
package agentslock

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AGOrcha/dot-agents/internal/fsops"
)

// isDeletePendingLockErr reports whether a failed lock-name create is the
// Windows "delete pending" transient — a contender's create racing an in-place
// delete of the lock name (an older da release, or trash churn in the parent)
// observes ERROR_ACCESS_DENIED (mapped to a permission error) while the object
// is mid-delete, rather than the portable ERROR_ALREADY_EXISTS.
// It clears within a retry, so the acquisition loop treats it as retryable
// contention. Gated on goos == "windows" so a genuine permission error stays a
// fast failure on other platforms (where this race does not occur). goos is a
// parameter rather than a direct runtime.GOOS read so the predicate is unit-
// testable for the Windows branch from any host.
func isDeletePendingLockErr(err error, goos string) bool {
	return goos == "windows" && os.IsPermission(err)
}

// lockGOOS is the GOOS the acquire loop classifies claim failures under. A
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

// lockAcquireTimeout bounds how long AcquireFileLock blocks before it returns a
// timeout error. It is a var, not a const, solely so the concurrent-churn test
// can widen the budget under the race detector: -race inflates every timing by
// 10-20x and, combined with 16-way contention, can push one scheduled hold past
// a 5s waiter deadline even though the primitive is correct. The acquire loop is
// pure polling (time.Sleep + retry, no channel/condvar/futex), so there is no
// wakeup to lose — a timeout under -race is scheduling latency, never a lost
// signal. Production always uses 5s.
var lockAcquireTimeout = 5 * time.Second

// SetAcquireTimeout overrides the acquire budget and returns a restore function.
// It is the cross-package seam for a concurrent-CONTENTION test that induces many
// real, slow, LIVE holds: under the race detector the runtime stretches every
// timing 10-20x, so on a loaded CI runner (the windows-latest leg especially,
// where -race runs alongside coverage) a single legitimate hold can outlast the
// 5s production budget even though the polling primitive is correct — no wakeup
// is ever lost, only latency added. Such a test widens the budget here to stay
// deterministic; production binaries (never built with -race) keep the 5s bound.
// The override MUST stay BELOW lockStaleTTL for a test that expects a timeout,
// else the held lock crosses the stale threshold and is reclaimed instead. Not
// for parallel tests — it mutates a process-global (callers here run serially).
func SetAcquireTimeout(d time.Duration) (restore func()) {
	prev := lockAcquireTimeout
	lockAcquireTimeout = d
	return func() { lockAcquireTimeout = prev }
}

const (
	lockRetryInterval = 10 * time.Millisecond
	// lockStaleTTL bounds how long an unreleased lock is tolerated before a
	// contending writer treats it as orphaned and reclaims it. The lock has no
	// kernel-backed auto-release, so a holder killed by SIGKILL/OOM/power
	// loss would otherwise wedge every future Flush permanently. The TTL is set
	// well above the expected sub-second hold time (a single read-merge-write)
	// so a live, slow holder is never reclaimed out from under itself, yet a
	// crashed holder's lock self-clears within the TTL rather than forever.
	lockStaleTTL = 30 * time.Second
	// lockNoHolderGrace bounds how long an UNIDENTIFIED occupant of the lock
	// name is tolerated before a contender treats it as a remnant and reclaims
	// it. With the hardlink claim this state cannot arise for new locks (the
	// name is only ever created already carrying its full identity); it covers
	// the degraded O_EXCL claim's create→write gap on hardlink-less
	// filesystems, torn/corrupt records, and legacy holderless lock DIRS from
	// pre-single-file binaries. A remnant MUST clear well inside
	// lockAcquireTimeout: judging it by the full lockStaleTTL (as the original
	// dir-mtime fallback did) meant a 30s grace against a 5s acquire budget —
	// every contender and every fresh da process was stranded into a
	// guaranteed timeout.
	// Ordering invariant: release rename retries (~300ms) < lockNoHolderGrace
	// < lockAcquireTimeout < lockStaleTTL.
	lockNoHolderGrace = 2 * time.Second
	// holderFile is the metadata file a LEGACY (pre-single-file) da binary
	// wrote inside its lock directory. Kept so upgraded binaries can judge and
	// describe legacy lock dirs during the transition.
	holderFile = "holder"
	// lockTrashInfix joins a lock path to the unique suffix of its renamed-away
	// trash sibling: "<lockpath>.stale-<pid>-<nanos>". Release and stale
	// reclaim free the lock NAME with one atomic rename to such a sibling and
	// delete the trash out-of-band; the name itself is never deleted in place
	// (see releaseLock). Claim temps share this namespace so crashed claims
	// are swept too.
	lockTrashInfix = ".stale-"
	// releaseRenameAttempts/Backoff bound the holder's release retry of the
	// single atomic rename. On Windows a contender's probe handle or an AV scan
	// can transiently pin the file against rename; the pin clears within
	// milliseconds, so a brief bounded retry (~275ms total, well under
	// lockNoHolderGrace) is the correct semantic. Retrying is safe here —
	// unlike reclaim — because the lock stays OURS until the rename lands.
	releaseRenameAttempts = 12
	releaseRenameBackoff  = 25 * time.Millisecond
	// deniedTransientAttempts bounds how many CONSECUTIVE permission-denied
	// claim results the Windows delete-pending accommodation absorbs before the
	// denial is classified as real. The delete-pending transient clears within a
	// retry or two; 30 ticks (~300ms at lockRetryInterval) is orders of
	// magnitude beyond that, so a denial that survives it is an environment
	// problem — Controlled Folder Access / OneDrive-protected folders or AV
	// policy denying this binary (the field `da config explain` / `da install`
	// shape) — and must fail fast with the actual cause, not burn the remaining
	// acquire budget and misreport itself as a lock-contention timeout.
	deniedTransientAttempts = 30
	// linkDegradeAttempts bounds how many consecutive hardlink-claim failures
	// (other than exists / not-exists) ONE acquire call absorbs as transients
	// before concluding the filesystem cannot do hardlinks and degrading to
	// the O_EXCL two-step claim. On NTFS a just-written claim temp can be
	// transiently pinned by an AV scan, failing CreateHardLinkW with a
	// sharing violation that clears within a tick — degrading on the FIRST
	// failure routed healthy Windows machines onto the weaker two-step path,
	// which is exactly how TestClaimNeverExposesPartialIdentity caught an
	// O_EXCL empty-file window on windows-latest (master run 28570275221). A
	// genuinely hardlink-less filesystem (FAT/exFAT) fails every attempt
	// identically, so degradation still engages after ~3 ticks (~30ms), once
	// per acquire call.
	linkDegradeAttempts = 3
)

// readLockFile reads the sidecar lock name (or a trash sibling) and returns its
// bytes. It exists because the lock name is freed by an atomic rename-away
// (displaceLock) while contenders concurrently read it to judge the occupant —
// and on Windows a rename cannot proceed while ANY other handle on the source
// was opened WITHOUT FILE_SHARE_DELETE, which is exactly what Go's os.ReadFile /
// os.Open omit (syscall.Open uses FILE_SHARE_READ|FILE_SHARE_WRITE only). A
// plain read therefore intermittently blocks a holder's release rename with
// ERROR_SHARING_VIOLATION ("the process cannot access the file because it is
// being used by another process"). The Windows build overrides this var (see
// readlock_windows.go) with a reader that shares DELETE, so a concurrent read
// never blocks the displace rename; every other platform renames open files
// freely and uses os.ReadFile unchanged. Read semantics (bytes returned, and an
// os.IsNotExist-classifiable error for a missing name) match os.ReadFile.
var readLockFile = os.ReadFile

// removeTrashFn deletes a renamed-away (trash) lock object. A seam so tests
// can force "trash deletion failed" deterministically and prove the lock name
// is already free regardless; production always uses fsops.RemoveAll.
var removeTrashFn = fsops.RemoveAll

// renameLockDirFn is the seam over fsops.Rename for the release/reclaim
// lifecycle so tests can pin rename-failure interleavings deterministically;
// production always uses fsops.Rename.
var renameLockDirFn = fsops.Rename

// linkLockFn is the seam over os.Link, the atomic claim primitive (hardlink
// the pre-written identity temp to the lock name; CreateHardLinkW on Windows,
// link(2) on unix — both atomic fail-if-exists). A seam so tests can force
// "filesystem without hardlinks" (the FAT/exFAT degraded path) and pin the
// reclaim restore interleaving deterministically. os.Link is not an
// fsguard-policed mutator; the atomic link IS the lock primitive, exactly like
// the old mkdir-as-lock.
var linkLockFn = os.Link

// testHookBeforeClaimVerify, when non-nil, runs between the degraded O_EXCL
// claim's identity write and its read-back verify. Tests use it to inject the
// mid-write loss interleaving on the hardlink-less fallback path
// deterministically; production leaves it nil. The primary (hardlink) claim
// has no hook point because it has no intermediate state to interleave with.
var testHookBeforeClaimVerify func(lockPath string)

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
//
// Flush is NOT a serialized read-modify-write: it acquires the advisory lock
// only for the write. A caller that must READ a shared section, compute a new
// value from it, and WRITE it back atomically — with no concurrent writer
// slipping in between the read and the write (the classic lost-update on a
// section BOTH processes write, e.g. the config/packages "units" section) —
// must use Update instead, which holds the lock across the whole cycle.
func (lf *Lockfile) Flush() error {
	unlock, err := acquireFileLock(lf.path)
	if err != nil {
		return err
	}
	defer unlock()
	return lf.writeLocked()
}

// writeLocked merges the latest on-disk document with this Lockfile's staged
// keys and writes atomically. The caller MUST already hold the advisory file
// lock (Flush and Update both do). Split out of Flush so Update can perform a
// read-open + mutate + write entirely inside ONE lock hold.
func (lf *Lockfile) writeLocked() error {
	lf.mu.Lock()
	defer lf.mu.Unlock()
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

// Update runs a serialized read-modify-write against the lockfile at path: it
// acquires the advisory file lock, opens the CURRENT on-disk document UNDER
// that lock, invokes fn to stage changes (SetSection / SetInputsDigest against
// the just-read state), then writes atomically and releases — all inside one
// lock hold. This closes the lost-update window that an unsynchronized
// Open→read→…→Flush leaves: when two processes both read-modify-write the SAME
// shared section (the config/packages "units" section), a plain Flush reapplies
// each process's stale whole-section snapshot and silently drops the other's
// keys. Update makes the read happen under the same lock as the write, so each
// writer observes the other's committed keys and preserves them.
//
// fn returns a non-nil error to ABORT with no write (the lock is released and
// the document is left untouched). fn must not call Flush/Update on the same
// Lockfile (the lock is not reentrant); it only stages via SetSection /
// SetInputsDigest and reads via Section.
func Update(path string, fn func(*Lockfile) error) error {
	unlock, err := acquireFileLock(path)
	if err != nil {
		return err
	}
	defer unlock()
	lf, err := Open(path)
	if err != nil {
		return err
	}
	if err := fn(lf); err != nil {
		return err
	}
	return lf.writeLocked()
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
// The lock is a SINGLE sidecar file at "<path>.lock" whose contents are the
// holder's identity (pid, acquisition time, random token). Acquisition is one
// atomic name-creation: the identity is fully written to a unique temp sibling
// first and then hard-linked to the lock name (os.Link → CreateHardLinkW, both
// atomic fail-if-exists). The lock name therefore NEVER holds a partial or
// identity-less object — there is no observable two-step state, which is what
// eliminated the mid-acquire interleavings of the previous dir+holder design
// (a contender could judge the dir between the Mkdir and the holder write).
// On filesystems without hardlinks (FAT/exFAT, some network mounts) the claim
// degrades to O_CREATE|O_EXCL + write + read-back verify; that path reopens a
// microseconds-wide create→write gap, guarded by lockNoHolderGrace and the
// verify, and is documented as the only residual two-step surface.
//
// The lock is ADVISORY — it excludes only other callers of this function that
// name the same path, not arbitrary writers of the underlying file. The parent
// directory of path is created if it does not yet exist.
//
// Acquisition blocks up to lockAcquireTimeout, retrying every lockRetryInterval,
// and returns a timeout error if a live holder never releases. Because the lock
// has no kernel-backed auto-release, a holder that crashed without releasing
// (SIGKILL/OOM/power loss) is detected as stale once its recorded age exceeds
// lockStaleTTL and is reclaimed at most once per call — so a slow but live
// holder is never torn down out from under itself. A legacy lock DIRECTORY at
// the same name (left by a pre-single-file da binary) is judged by the old
// dir+holder staleness rules and reclaimed through the same rename-away path,
// so upgraded binaries never wedge on old remnants.
//
// The returned release frees the lock name exactly once (identity-verified
// atomic rename-away, see releaseLock) and reports an error only when the name
// could not be freed; it is once-guarded (a second call returns the first
// call's cached error without touching the filesystem). The once-guard plus
// the identity check guarantee a duplicate or overdue release can never touch
// a lock this caller no longer owns.
func AcquireFileLock(path string) (release func() error, err error) {
	lockPath, identity, err := acquireLockPath(path)
	if err != nil {
		return nil, err
	}
	var (
		once   sync.Once
		relErr error
	)
	return func() error {
		once.Do(func() { relErr = releaseLock(lockPath, identity) })
		return relErr
	}, nil
}

// acquireFileLock is the internal release-returning-nothing form used by Flush.
// It shares acquireLockPath with the exported AcquireFileLock; the only
// difference is that a release error is surfaced via the package debug channel
// (Flush has no error path for the deferred unlock) rather than returned.
func acquireFileLock(path string) (func(), error) {
	lockPath, identity, err := acquireLockPath(path)
	if err != nil {
		return nil, err
	}
	return func() { unlockLock(lockPath, identity) }, nil
}

// acquireLockPath is the shared acquisition core. It returns the sidecar lock
// path it now holds plus the identity recorded inside it; both AcquireFileLock
// and acquireFileLock wrap it with their respective release shapes.
func acquireLockPath(path string) (lockPath, identity string, err error) {
	// Build the sidecar lock path through filepath so it carries the platform
	// separator, and ensure its parent exists before the first claim: the
	// single-component create does NOT make intermediate components, and an
	// absent parent was the original Windows field failure (#148).
	lockPath = filepath.Clean(path) + ".lock"
	if parent := filepath.Dir(lockPath); parent != "." && parent != "" {
		if err := fsops.MkdirAll(parent, 0o700); err != nil {
			return "", "", fmt.Errorf("agentslock: ensure lock parent %s: %w", parent, err)
		}
	}
	deadline := time.Now().Add(lockAcquireTimeout)
	st := acquireLoopState{}
	for {
		identity, acquired, err := acquireTick(lockPath, &st)
		if err != nil {
			return "", "", err
		}
		if acquired {
			return lockPath, identity, nil
		}
		if st.retryNow {
			st.retryNow = false
			continue
		}
		if time.Now().After(deadline) {
			return "", "", lockTimeoutError(lockPath)
		}
		time.Sleep(lockRetryInterval)
	}
}

// acquireLoopState carries the per-call bookkeeping across acquireTick
// iterations: the once-per-call stale-reclaim budget, the consecutive
// permission-denied streak, and whether the next retry should skip the sleep
// (a reclaim just freed the name).
type acquireLoopState struct {
	reclaimed    bool
	denied       int
	retryNow     bool
	linkFailures int
}

// acquireTick performs one iteration of the acquisition loop. When the lock
// name is occupied it evaluates the occupant (live wait / stale reclaim /
// legacy dir); when free it attempts the atomic claim and classifies any
// failure. It returns the claimed identity with acquired=true, a fatal error,
// or neither (the caller waits and retries).
func acquireTick(lockPath string, st *acquireLoopState) (identity string, acquired bool, err error) {
	if _, statErr := os.Lstat(lockPath); statErr == nil {
		// Occupied: any prior denials were the delete-pending transient. Judge
		// the occupant; a stale one is reclaimed at most once per call, and the
		// budget is only spent when the reclaim rename actually lands.
		st.denied = 0
		if !st.reclaimed {
			if stale, judged := lockOccupantStale(lockPath); stale && reclaimStaleLock(lockPath, judged) {
				st.reclaimed = true
				st.retryNow = true
			}
		}
		return "", false, nil
	}
	id, claimErr := claimLock(lockPath, st)
	if claimErr == nil {
		return id, true, nil
	}
	return "", false, classifyClaimError(lockPath, claimErr, st)
}

// errClaimTempVanished marks the one benign not-found shape inside a claim:
// the pre-written identity temp was deleted between write and link (it lives
// in the trash namespace, so a concurrent release's sweep may collect it).
// Deliberately does NOT wrap os.ErrNotExist so it cannot be confused with a
// create-level not-found, which is a distinct, environmental failure.
var errClaimTempVanished = fmt.Errorf("agentslock: claim temp swept concurrently")

// errClaimLinkTransient marks a hardlink-claim failure being absorbed as a
// transient (see linkDegradeAttempts): the acquire loop retries on the strong
// hardlink path instead of degrading to the two-step O_EXCL claim.
var errClaimLinkTransient = fmt.Errorf("agentslock: hardlink claim transiently failed")

// classifyClaimError maps a failed claim to either a fatal error or nil
// (retryable contention/transient; the acquire loop waits and retries).
func classifyClaimError(lockPath string, claimErr error, st *acquireLoopState) error {
	switch {
	case os.IsExist(claimErr):
		st.denied = 0
		return nil // lost the claim race; next tick judges the winner
	case errors.Is(claimErr, errClaimTempVanished):
		return nil // transient: retry with a fresh temp
	case errors.Is(claimErr, errClaimLinkTransient):
		return nil // transient link pin: retry on the strong hardlink path
	case os.IsNotExist(claimErr):
		// A CREATE failing not-found. With a missing parent it is the plain
		// #148 shape (surface it raw). With a parent that demonstrably EXISTS
		// it is impossible from userland filesystem semantics — a filter
		// driver is intercepting creates — and must fail fast with the
		// environmental diagnosis instead of burning the acquire budget.
		if _, statErr := os.Stat(filepath.Dir(lockPath)); statErr == nil {
			return notFoundLockError(lockPath, claimErr)
		}
		return fmt.Errorf("agentslock: acquire lock %s: %w", lockPath, claimErr)
	case isDeletePendingLockErr(claimErr, lockGOOS):
		// Windows-only transient: creating at a name that is mid-delete
		// (delete-pending) reports ERROR_ACCESS_DENIED. It clears within a
		// retry or two. A denial that SURVIVES the transient window is not this
		// race at all — it is a real permission denial (folder protection / AV)
		// and must fail fast with the actual cause instead of a misleading
		// contention timeout.
		st.denied++
		if st.denied >= deniedTransientAttempts {
			return deniedLockError(lockPath, claimErr)
		}
		return nil
	case os.IsPermission(claimErr):
		// Non-Windows permission denial: never a delete-pending transient, so
		// classify immediately with the actionable message.
		return deniedLockError(lockPath, claimErr)
	default:
		return fmt.Errorf("agentslock: acquire lock %s: %w", lockPath, claimErr)
	}
}

// newLockIdentity renders this acquisition's identity record: pid, acquisition
// time, and a random token. The token makes the identity unforgeable in
// practice, so "contents at the lock name == our identity" is proof of
// ownership even across grace reclaims and re-claims.
func newLockIdentity() string {
	var token [8]byte
	_, _ = rand.Read(token[:]) // best-effort; pid+nanos already disambiguate
	return fmt.Sprintf("%d\n%d\n%x\n", os.Getpid(), time.Now().UnixNano(), token)
}

// claimLock atomically claims the free lock name. The identity is FULLY
// written to a unique temp sibling first, then hard-linked to the lock name:
// one atomic syscall either publishes the complete identity at the name or
// fails. No contender can ever observe a claimed-but-identity-less lock, which
// is the structural property that retired the old design's mid-acquire races.
//
// The temp lives in the trash namespace so a crash between write and link is
// swept by later releases; the price is that a concurrent sweep can delete the
// temp first, surfacing as an ENOENT the acquire loop simply retries.
//
// A link failure other than exists/not-exists means the filesystem cannot do
// hardlinks (FAT/exFAT, some network mounts) — or is transiently pinning the
// name — and the claim degrades to the O_EXCL two-step. If the real cause was
// a permission denial, the degraded path fails with the same denial and the
// caller's classification still applies.
func claimLock(lockPath string, st *acquireLoopState) (string, error) {
	identity := newLockIdentity()
	temp := lockTrashName(lockPath)
	if err := writeIdentityFileFn(temp, identity); err != nil {
		return "", err
	}
	defer func() { _ = fsops.Remove(temp) }()
	if err := linkLockFn(temp, lockPath); err != nil {
		if os.IsExist(err) {
			return "", err
		}
		if os.IsNotExist(err) {
			// The temp we just wrote is gone: a concurrent release swept the
			// trash namespace. Distinct from a create-level not-found (which
			// is environmental; see classifyClaimError).
			return "", fmt.Errorf("%w: %v", errClaimTempVanished, err)
		}
		// Any other failure is EITHER a transient pin on the just-written
		// temp (NTFS: AV scanning it blocks CreateHardLinkW with a sharing
		// violation for a tick) OR a filesystem without hardlinks. Absorb a
		// few as transients — degrading to the two-step O_EXCL claim on the
		// first failure put Windows machines on the weaker path and exposed
		// its empty-file window to the structural-impossibility test.
		st.linkFailures++
		if st.linkFailures < linkDegradeAttempts {
			return "", fmt.Errorf("%w: %v", errClaimLinkTransient, err)
		}
		return claimLockExclusive(lockPath, identity)
	}
	return identity, nil
}

// writeIdentityFile writes an identity record with raw file syscalls on
// purpose (os.OpenFile is not an fsguard-policed mutator): fsops.WriteFile's
// Windows fallback MkdirAll-then-PowerShell "heals" missing parents and takes
// hundreds of milliseconds per attempt — the claim must observe the
// filesystem's true state (and fail fast on a real denial), never repair it.
var writeIdentityFileFn = writeIdentityFile

func writeIdentityFile(path, identity string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, werr := f.WriteString(identity)
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	return cerr
}

// claimLockExclusive is the degraded claim for hardlink-less filesystems:
// O_CREATE|O_EXCL (CREATE_NEW on Windows — atomic name creation), then write,
// then a read-back verify. The create→write gap is the one residual two-step
// surface in the design: a contender that reads the just-created empty file
// judges it by lockNoHolderGrace, so only a holder that stalls >2s inside a
// two-syscall window is ever at risk, and the verify converts that loss into
// a clean retry instead of an unverified acquisition.
func claimLockExclusive(lockPath, identity string) (string, error) {
	f, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	_, werr := f.WriteString(identity)
	cerr := f.Close()
	if werr != nil || cerr != nil {
		// Retract the half-written token (provably ours — the O_EXCL create
		// succeeded microseconds ago) so the name reads as unidentified and
		// clears via the grace instead of lingering as garbage under the TTL.
		_ = fsops.Remove(lockPath)
		return "", fmt.Errorf("agentslock: write lock identity %s: %w", lockPath, werr)
	}
	if testHookBeforeClaimVerify != nil {
		testHookBeforeClaimVerify(lockPath)
	}
	data, rerr := readLockFile(lockPath)
	if rerr != nil || string(data) != identity {
		// Lost mid-write to a grace reclaim (the name now holds a successor's
		// identity, or nothing). Never proceed unverified, and never touch the
		// name — it is not provably ours anymore.
		return "", &os.PathError{Op: "claim", Path: lockPath, Err: os.ErrExist}
	}
	return identity, nil
}

// lockOccupantStale judges the object currently at the lock name. It returns
// whether the occupant is reclaimable plus the exact bytes that were judged
// (nil for a legacy dir), so the reclaim can verify it renamed the same
// occupant it judged.
//
// A parseable identity is judged by its recorded acquisition age against
// lockStaleTTL — PID liveness is deliberately not consulted because it cannot
// be checked portably (notably on Windows). An unidentified occupant (the
// degraded O_EXCL claim's create→write gap, a torn write, or corruption) gets
// the SHORT lockNoHolderGrace by mtime: with the hardlink claim this state
// cannot arise at all, and a remnant must clear well inside a contender's
// acquire budget. A legacy DIRECTORY (pre-single-file binary) is judged by the
// old dir+holder rules.
func lockOccupantStale(lockPath string) (bool, []byte) {
	fi, err := os.Lstat(lockPath)
	if err != nil {
		return false, nil // vanished: the next tick simply claims
	}
	if fi.IsDir() {
		return legacyLockDirStale(lockPath), nil
	}
	data, err := readLockFile(lockPath)
	if err != nil {
		// Unreadable file (mid-rename, delete-pending, foreign perms): give it
		// the grace rather than wedging every acquirer until timeout.
		return pathOlderThan(lockPath, lockNoHolderGrace), nil
	}
	if age, ok := identityAge(data); ok {
		return age > lockStaleTTL, data
	}
	return pathOlderThan(lockPath, lockNoHolderGrace), data
}

// identityAge parses an identity record's recorded acquisition time and
// returns its age. ok=false means the record is not a parseable identity.
func identityAge(data []byte) (time.Duration, bool) {
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 2 {
		return 0, false
	}
	acquiredNanos, err := strconv.ParseInt(strings.TrimSpace(lines[1]), 10, 64)
	if err != nil {
		return 0, false
	}
	return time.Duration(time.Now().UnixNano() - acquiredNanos), true
}

// legacyLockDirStale judges a lock DIRECTORY left by a pre-single-file da
// binary, preserving the old rules so mixed-version machines behave: a
// readable holder file inside the dir is judged by its recorded age against
// the TTL; a garbage holder falls back to dir mtime against the TTL; a
// holderless dir gets the short grace (it is either the old design's
// mid-acquire window or a partial-release remnant, and remnants must clear
// inside a contender's acquire budget).
func legacyLockDirStale(lockDir string) bool {
	data, err := os.ReadFile(filepath.Join(lockDir, holderFile))
	if err != nil {
		return pathOlderThan(lockDir, lockNoHolderGrace)
	}
	if age, ok := identityAge(data); ok {
		return age > lockStaleTTL
	}
	return pathOlderThan(lockDir, lockStaleTTL)
}

// pathOlderThan reports whether the path's last-modified time is older than
// limit. If it can't be stat'd it is treated as stale (it likely no longer
// exists, in which case the retry will simply re-claim).
func pathOlderThan(path string, limit time.Duration) bool {
	info, err := os.Stat(path)
	if err != nil {
		return true
	}
	return time.Since(info.ModTime()) > limit
}

// lockTrashSeq disambiguates trash names minted within the same nanosecond by
// the same process. pid+nanos alone is NOT unique in-process: dozens of
// concurrent claimants on a coarse-clock CI runner can mint the same
// pid+nanos, and a collided CLAIM TEMP is catastrophic — two claimants write
// the same temp path (O_TRUNC), one links it to the lock name after the other
// overwrote it, and the "winner" holds a lock recording the LOSER's identity;
// its own release then self-refuses on the identity check and the lock wedges
// until the TTL. That was the windows/macos-latest
// TestEmitConcurrentNoTornLines wedge ("held by pid <self> for 5s").
var lockTrashSeq atomic.Int64

// lockTrashName returns a trash sibling for lockPath that is unique across
// processes (pid) and within a process (atomic sequence; nanos kept for
// human-readable ordering). Trash names are never contended or re-acquired:
// deleting trash can only ever race other deleters of the same garbage, never
// a live lock — and claim temps minted here can never collide.
func lockTrashName(lockPath string) string {
	return fmt.Sprintf("%s%s%d-%d-%d",
		lockPath, lockTrashInfix, os.Getpid(), time.Now().UnixNano(), lockTrashSeq.Add(1))
}

// displaceOutcome classifies what displaceLock actually took off the name.
type displaceOutcome int

const (
	// displacedMatch: the renamed object carried the expected identity — it
	// was ours (release) or the judged-stale occupant (reclaim); disposed.
	displacedMatch displaceOutcome = iota
	// displacedGone: nothing at the name (already released or reclaimed).
	displacedGone
	// displacedWrongObject: the rename took a DIFFERENT, live object — a
	// successor claimed the name between the caller's judgment and the
	// rename. The object was restored in place via an atomic same-inode link
	// (or, if a third claimant had already re-taken the name, disposed with a
	// loud debugf — the shared three-actor residual).
	displacedWrongObject
	// displaceFailed: the rename never landed (transient pin / denial); the
	// name is untouched.
	displaceFailed
)

// displaceLock is the ONE primitive that ever frees the lock name, shared by
// release and stale reclaim. It renames the name away FIRST and verifies
// AFTERWARD, on the renamed object itself, that it took what the caller
// expected — so there is no window between judgment and action: judgment IS
// performed on the very object the action displaced. (The old shape — verify
// by name, then rename — left a TOCTOU on both paths: between the read and
// the rename a TTL reclaim plus a successor claim could swap the object, and
// the rename displaced the successor's live lock.)
//
// On mismatch the wrongly-taken object is restored by atomically hard-linking
// the SAME inode back to the name; the displaced holder never notices. Only
// if a third claimant took the name inside that window is the displacement
// permanent (loud debugf; the victim's own release then refuses on identity
// mismatch) — the single documented residual, requiring three independent
// actors inside a two-syscall window, shared by release and reclaim alike. A
// nil expected skips verification (legacy lock DIRS cannot be identity-read
// or link-restored; they keep single-rename semantics for the transition).
func displaceLock(lockPath string, expected []byte) (displaceOutcome, error) {
	trash := lockTrashName(lockPath)
	if err := renameLockDirFn(lockPath, trash); err != nil {
		if os.IsNotExist(err) {
			return displacedGone, nil
		}
		return displaceFailed, err
	}
	outcome := displacedMatch
	if expected != nil && !trashCarries(trash, expected) {
		outcome = displacedWrongObject
		if err := linkLockFn(trash, lockPath); err == nil {
			_ = fsops.Remove(trash) // drop the extra name; the inode lives on at lockPath
			return outcome, nil
		}
		debugf("agentslock: displaced a live lock at %s and could not restore it (a third claimant holds the name)", lockPath)
	}
	if err := removeTrashFn(trash); err != nil {
		debugf("agentslock: remove displaced lock remnant %s: %v", trash, err)
	}
	return outcome, nil
}

// trashCarries reports whether the renamed-away trash carries exactly the
// expected identity bytes.
func trashCarries(trash string, expected []byte) bool {
	data, err := readLockFile(trash)
	return err == nil && bytes.Equal(data, expected)
}

// reclaimStaleLock takes a stale occupant off the name via displaceLock and
// reports whether the reclaim landed (the caller may spend its once-per-call
// budget). A wrong-object displacement (rival reclaimed + successor claimed
// between judgment and rename) restores the successor and reports false — the
// caller simply keeps waiting on the live lock it nearly displaced.
func reclaimStaleLock(lockPath string, judged []byte) bool {
	outcome, _ := displaceLock(lockPath, judged)
	return outcome == displacedMatch
}

// releaseLock frees the lock at lockPath. Lifecycle contract: the lock name is
// only ever freed by displaceLock's atomic rename-away — never deleted in
// place — and the ownership verification happens on the renamed object itself,
// so a release that raced a TTL reclaim (e.g. this process resumed after a
// 30s+ suspend) restores the successor's live lock and reports the honest
// outcome (already reclaimed, nil) instead of stealing it. An empty identity
// never releases. The initial read is a fast-path OPTIMIZATION only (skip the
// rename when we are provably no longer the holder); correctness never rests
// on it.
//
// The displace is retried briefly (releaseRenameAttempts x
// releaseRenameBackoff) for transient Windows pins; retrying is safe because
// every attempt re-verifies on whatever it actually renamed. If the rename
// never lands the remnant keeps its full identity and self-clears via the
// TTL; returning the error is the only remaining honest option.
func releaseLock(lockPath, identity string) error {
	if identity == "" {
		debugf("agentslock: skip release of %s: no identity to prove ownership", lockPath)
		return nil
	}
	if data, err := readLockFile(lockPath); err == nil && string(data) != identity {
		debugf("agentslock: skip release of %s: no longer the holder", lockPath)
		return nil
	}
	var (
		outcome displaceOutcome
		err     error
	)
	for attempt := 0; attempt < releaseRenameAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(releaseRenameBackoff)
		}
		outcome, err = displaceLock(lockPath, []byte(identity))
		if outcome != displaceFailed {
			break
		}
	}
	switch outcome {
	case displacedMatch:
		sweepLockTrash(lockPath)
		return nil
	case displacedWrongObject:
		debugf("agentslock: release of %s: lock was TTL-reclaimed by a successor; nothing of ours to free", lockPath)
		return nil
	case displacedGone:
		return nil
	default:
		return fmt.Errorf("agentslock: release lock %s: %w", lockPath, err)
	}
}

// lockStillOurs reports whether the lock file still carries exactly the
// identity this acquisition claimed. An empty identity can NEVER prove
// ownership (acquisition fails rather than hand out empty identities), and an
// unreadable or mismatched record means the lock was reclaimed or is
// mid-teardown by someone else — without proof of ownership the release must
// not touch the name (a wrongly-skipped release only leaves a remnant that the
// grace/TTL reclaim clears; a wrongly-performed release steals a live lock).
func lockStillOurs(lockPath, identity string) bool {
	if identity == "" {
		return false
	}
	data, err := readLockFile(lockPath)
	return err == nil && string(data) == identity
}

// unlockLock is Flush's release shape: same releaseLock lifecycle, with a
// failure surfaced via the package debug channel (Flush has no error path for
// the deferred unlock). A rename that never lands leaves the identity intact,
// so the remnant self-clears via the TTL rather than wedging forever.
func unlockLock(lockPath, identity string) {
	if err := releaseLock(lockPath, identity); err != nil {
		debugf("%v", err)
	}
}

// sweepLockTrash best-effort deletes every trash sibling of lockPath: the one
// the caller just renamed away plus any leftovers from earlier releases whose
// trash deletion failed (and any crashed claim temps, which share the trash
// namespace). Runs only after the lock name is already free, so a sweep
// failure costs nothing but disk garbage (retried on the next release).
func sweepLockTrash(lockPath string) {
	parent := filepath.Dir(lockPath)
	prefix := filepath.Base(lockPath) + lockTrashInfix
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

// lockTimeoutError renders the acquire-timeout failure, naming the blocking
// holder when its record is readable so a stranded invocation reports WHO
// holds the lock and when the TTL will self-heal it.
func lockTimeoutError(lockPath string) error {
	msg := "timed out"
	if held := describeHolder(lockPath); held != "" {
		msg += " (" + held + ")"
	}
	return fmt.Errorf("agentslock: acquire lock %s: %s", lockPath, msg)
}

// deniedLockError classifies a PERSISTENT access-denied creating the lock.
// Field shape: Windows Controlled Folder Access / OneDrive-protected Documents
// or antivirus policy denying an unrecognized binary — observed as `da config
// explain --all` and `da install` failing on `.agentsrc.lock.lock` creation.
// Without this classification the Windows delete-pending accommodation retried
// the denial for the whole acquire budget and then reported a generic "timed
// out", misdiagnosing an environment problem as lock contention. Per the
// error-message contract the primary line says what failed; the follow-up
// names the likely causes and the concrete next step.
func deniedLockError(lockPath string, err error) error {
	return fmt.Errorf("agentslock: acquire lock %s: creating the lock file is persistently denied (%w); "+
		"this is not lock contention — the parent folder %s is likely write-protected "+
		"(Windows Controlled Folder Access, OneDrive-protected Documents, or antivirus policy blocking this binary); "+
		"allow the da binary for that folder or move the repo outside the protected path, then retry",
		lockPath, err, filepath.Dir(lockPath))
}

// notFoundLockError classifies a lock create failing ENOENT /
// ERROR_FILE_NOT_FOUND while the parent directory demonstrably EXISTS —
// impossible from plain userland filesystem semantics, so something is
// intercepting creates in that tree: OneDrive Files-On-Demand / a
// sync-redirected Documents folder, or a corporate DLP/antivirus filter
// driver. This is the exact work-PC field shape (`mkdir
// C:\...\.agentsrc.lock.lock: The system cannot find the file specified`
// with git operating normally in the same directory — see
// .agents/history/provadm-windows-da-lock-observation.md). Classified
// immediately, not retried: the interference is persistent and burning the
// acquire budget would misreport it as a lock timeout.
func notFoundLockError(lockPath string, err error) error {
	return fmt.Errorf("agentslock: acquire lock %s: create failed with not-found although the parent directory exists (%w); "+
		"this indicates filesystem interference in %s — a OneDrive-redirected or cloud-synced folder, or a DLP/antivirus filter driver intercepting file creation; "+
		"move the repo outside the synced/protected path or exempt the da binary, then run `da doctor` and retry",
		lockPath, err, filepath.Dir(lockPath))
}

// describeHolder renders a short "held by pid P for D; stale after TTL"
// summary of the current lock holder for the acquire-timeout error. It reads
// the single-file identity first and falls back to a legacy dir's holder file;
// an absent or unparseable record yields "" (the bare timeout message).
func describeHolder(lockPath string) string {
	data, err := readLockFile(lockPath)
	if err != nil {
		if data, err = os.ReadFile(filepath.Join(lockPath, holderFile)); err != nil {
			return ""
		}
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
