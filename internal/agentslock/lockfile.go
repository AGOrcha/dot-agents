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
	lockDir := path + ".lock"
	deadline := time.Now().Add(lockAcquireTimeout)
	for {
		err := os.Mkdir(lockDir, 0o700)
		if err == nil {
			return func() { _ = os.Remove(lockDir) }, nil
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("agentslock: acquire lock %s: %w", lockDir, err)
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("agentslock: acquire lock %s: timed out", lockDir)
		}
		time.Sleep(lockRetryInterval)
	}
}
