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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AGOrcha/dot-agents/internal/fsops"
)

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
	// holderFile is the name of the sidecar metadata file written inside the
	// lock dir recording the acquiring PID and acquisition time, used to detect
	// and reclaim stale locks.
	holderFile = "holder"
)

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

func acquireFileLock(path string) (func(), error) {
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
	lockDir := filepath.Clean(path) + ".lock"
	if parent := filepath.Dir(lockDir); parent != "." && parent != "" {
		if err := os.MkdirAll(parent, 0o700); err != nil {
			return nil, fmt.Errorf("agentslock: ensure lock parent %s: %w", parent, err)
		}
	}
	deadline := time.Now().Add(lockAcquireTimeout)
	reclaimed := false
	for {
		err := os.Mkdir(lockDir, 0o700)
		if err == nil {
			writeHolder(lockDir)
			return func() { unlockFileLock(lockDir) }, nil
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("agentslock: acquire lock %s: %w", lockDir, err)
		}
		// Contention: the lock dir already exists. Before waiting, decide whether
		// the current holder is alive or stale (crashed without releasing). A
		// stale lock is reclaimed at most once per call so a live holder racing
		// us cannot be repeatedly torn down.
		if !reclaimed && lockIsStale(lockDir) {
			reclaimed = true
			// Remove the whole lock dir (holder file included) so the next Mkdir
			// can succeed. Ignore the error: if removal fails (e.g. another
			// writer reclaimed first), we simply fall through to the retry/timeout
			// path and try again.
			_ = os.RemoveAll(lockDir)
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("agentslock: acquire lock %s: timed out", lockDir)
		}
		time.Sleep(lockRetryInterval)
	}
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

// writeHolder records the acquiring PID and acquisition time inside the lock dir
// so a later contender can detect a crashed holder. Best-effort: a write failure
// only forfeits stale detection (the lock still functions), so the error is not
// fatal to acquisition.
func writeHolder(lockDir string) {
	contents := fmt.Sprintf("%d\n%d\n", os.Getpid(), time.Now().UnixNano())
	_ = os.WriteFile(filepath.Join(lockDir, holderFile), []byte(contents), 0o600)
}

// lockIsStale reports whether the existing lock dir should be treated as
// orphaned. A lock is stale when its recorded acquisition time is older than
// lockStaleTTL. The TTL alone is enough to guarantee no permanent deadlock; PID
// liveness is deliberately not consulted because it cannot be checked portably
// (notably on Windows).
//
// When the holder file is missing or unparseable the lock dir's own mtime is
// used as the fallback age source. This avoids a reclaim race: os.Mkdir and the
// subsequent writeHolder are two steps, so a live holder is momentarily visible
// with no holder file yet — judging that instantly stale would let a contender
// delete a healthy lock. Falling back to the dir mtime gives that window the
// same TTL grace as a recorded acquisition.
func lockIsStale(lockDir string) bool {
	holderPath := filepath.Join(lockDir, holderFile)
	data, err := os.ReadFile(holderPath)
	if err != nil {
		return dirOlderThanTTL(lockDir)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 2 {
		return dirOlderThanTTL(lockDir) // unparseable
	}
	acquiredNanos, err := strconv.ParseInt(strings.TrimSpace(lines[1]), 10, 64)
	if err != nil {
		return dirOlderThanTTL(lockDir) // unparseable timestamp
	}
	age := time.Now().UnixNano() - acquiredNanos
	return age > int64(lockStaleTTL)
}

// dirOlderThanTTL reports whether the lock dir's last-modified time is older than
// lockStaleTTL. If the dir can't be stat'd it is treated as stale (it likely no
// longer exists, in which case the retry will simply re-acquire).
func dirOlderThanTTL(lockDir string) bool {
	info, err := os.Stat(lockDir)
	if err != nil {
		return true
	}
	return time.Since(info.ModTime()) > lockStaleTTL
}

// unlockFileLock releases the lock by removing the dir and its holder file. A
// failed removal would leave a stale lock with no signal until the TTL elapses,
// so the failure is surfaced via the package debug channel rather than silently
// dropped. RemoveAll clears the holder sidecar in the same call.
func unlockFileLock(lockDir string) {
	if err := os.RemoveAll(lockDir); err != nil {
		debugf("agentslock: release lock %s: %v", lockDir, err)
	}
}
