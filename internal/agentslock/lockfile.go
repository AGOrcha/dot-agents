// Package agentslock is the single shared writer for .agentsrc.lock — the
// resolved-state companion to .agentsrc.json (config-distribution-model §7).
//
// It is schema-agnostic: it owns the whole JSON document and treats top-level
// sections (config, packages, adapters, …) as opaque values, so the config/
// package resolver and the graph-adapter lifecycle share one file without
// either importing the other's schema (§7.4). A writer stages only its own
// section and flushes; sibling sections are preserved verbatim. Flush is
// atomic (temp file + rename, via fsops.WriteFileAtomic). A single Lockfile is
// safe for concurrent SetSection from parallel resolver goroutines — the
// in-process mutex guards the document and the on-disk write is the one
// serialized step (§7.4 "parallel resolution, serialized write").
package agentslock

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/NikashPrakash/dot-agents/internal/fsops"
)

// LockVersion is the current .agentsrc.lock schema version.
const LockVersion = 1

// lockVersionKey is the reserved top-level key holding LockVersion. It is not a
// section and cannot be set via SetSection.
const lockVersionKey = "lock_version"

// Lockfile is the in-memory view of a .agentsrc.lock document: open it, read or
// stage sections, then Flush. Safe for concurrent use.
type Lockfile struct {
	path string
	mu   sync.Mutex
	doc  map[string]json.RawMessage // lock_version + one entry per section
}

// Open loads the lockfile at path. A missing file yields a fresh document
// (lock_version only); a present file is parsed, preserving every top-level key
// — including sections this process does not know about.
func Open(path string) (*Lockfile, error) {
	lf := &Lockfile{path: path, doc: map[string]json.RawMessage{}}
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

// SetSection marshals v and stages it as the named section, leaving every other
// section untouched. Safe to call concurrently from multiple goroutines.
func (lf *Lockfile) SetSection(name string, v any) error {
	if name == lockVersionKey {
		return fmt.Errorf("agentslock: %q is reserved, not a section", lockVersionKey)
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("agentslock: encode section %q: %w", name, err)
	}
	lf.mu.Lock()
	lf.doc[name] = raw
	lf.mu.Unlock()
	return nil
}

// Flush writes the whole document to path atomically, preserving every section.
// It is callable more than once (e.g. persist config before a slow adapter
// activation, then flush adapters after). The parent directory must exist.
func (lf *Lockfile) Flush() error {
	lf.mu.Lock()
	defer lf.mu.Unlock()
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
