package projectsync

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// PromoteJournalDir is the relative directory (under ~/.agents/) holding
// promote-journal entries. Each in-flight promotion writes a single JSON file
// here; the file is removed once the promotion completes (or rollback finishes).
const PromoteJournalDir = ".promote-journal"

// Promote journal states. The pipeline writes one entry, then advances through
// these states as each destructive step succeeds.
const (
	PromoteStatePrepared        = "prepared"
	PromoteStateCanonicalCopied = "canonical-copied"
	PromoteStateSourceRemoved   = "source-removed"
	PromoteStateSymlinked       = "symlinked"
	PromoteStateRCSaved         = "rc-saved"
	PromoteStateRolledBack      = "rolled-back"
)

// PromoteJournalEntry is the JSON shape persisted under ~/.agents/.promote-journal/.
// One entry exists per in-flight PromoteResource call; entries are removed on
// success or on completed rollback.
type PromoteJournalEntry struct {
	ID            string    `json:"id"`
	Singular      string    `json:"singular"`
	Bucket        string    `json:"bucket"`
	Name          string    `json:"name"`
	SourcePath    string    `json:"source_path"`
	CanonicalPath string    `json:"canonical_path"`
	State         string    `json:"state"`
	StartedAt     time.Time `json:"started_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// Test seam: replaceable in tests to drive journal-write failure paths.
var osWriteFile = os.WriteFile

// promoteJournalDirPath returns the absolute directory holding journal entries
// for the given agents-home root.
func promoteJournalDirPath(agentsHome string) string {
	return filepath.Join(agentsHome, PromoteJournalDir)
}

// newPromoteJournalID returns a unique journal id (timestamp + random suffix).
// The timestamp keeps ids sortable for human inspection; random bytes prevent
// collisions when two promotions race.
func newPromoteJournalID() string {
	var buf [6]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// Extremely unlikely; fall back to nanosecond clock so we still produce
		// distinct ids under collision.
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%d-%s", time.Now().UnixNano(), hex.EncodeToString(buf[:]))
}

// BeginPromoteJournal writes a fresh journal entry under
// ~/.agents/.promote-journal/ with state="prepared". The returned path is the
// absolute journal file location; the caller passes it to AdvancePromoteJournal
// and RemovePromoteJournal as the promotion progresses.
//
// The caller must populate Singular, Bucket, Name, SourcePath, and
// CanonicalPath on the entry. ID/State/StartedAt/UpdatedAt are overwritten.
func BeginPromoteJournal(agentsHome string, entry PromoteJournalEntry) (string, error) {
	dir := promoteJournalDirPath(agentsHome)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("creating promote-journal dir %s: %w", dir, err)
	}
	entry.ID = newPromoteJournalID()
	entry.State = PromoteStatePrepared
	now := time.Now().UTC()
	entry.StartedAt = now
	entry.UpdatedAt = now

	path := filepath.Join(dir, entry.ID+".json")
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshaling journal entry: %w", err)
	}
	if err := osWriteFile(path, data, 0644); err != nil {
		return "", fmt.Errorf("writing journal entry %s: %w", path, err)
	}
	return path, nil
}

// AdvancePromoteJournal updates the State and UpdatedAt of an existing journal
// entry. The file is rewritten in place. A missing journal file is reported as
// an error so the caller can distinguish "lost-journal" failures from normal
// progression.
func AdvancePromoteJournal(path, newState string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading journal entry %s: %w", path, err)
	}
	var entry PromoteJournalEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return fmt.Errorf("parsing journal entry %s: %w", path, err)
	}
	entry.State = newState
	entry.UpdatedAt = time.Now().UTC()
	out, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling journal entry: %w", err)
	}
	if err := osWriteFile(path, out, 0644); err != nil {
		return fmt.Errorf("writing journal entry %s: %w", path, err)
	}
	return nil
}

// RemovePromoteJournal deletes the journal file. Safe to call on a missing
// path — a not-exist error is treated as success.
func RemovePromoteJournal(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing journal entry %s: %w", path, err)
	}
	return nil
}

// ListPendingPromoteJournals returns all journal entries under
// ~/.agents/.promote-journal/ whose state is not terminal (rc-saved or
// rolled-back). The returned slice is sorted by StartedAt ascending so callers
// can process oldest-first during recovery.
func ListPendingPromoteJournals(agentsHome string) ([]PromoteJournalEntry, error) {
	dir := promoteJournalDirPath(agentsHome)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return classifyJournalReadDirError(agentsHome, dir, err)
	}
	var pending []PromoteJournalEntry
	for _, e := range entries {
		if je, ok := readPendingJournalEntry(dir, e); ok {
			pending = append(pending, je)
		}
	}
	sort.Slice(pending, func(i, j int) bool {
		return pending[i].StartedAt.Before(pending[j].StartedAt)
	})
	return pending, nil
}

// classifyJournalReadDirError maps an os.ReadDir failure on the journal dir to
// either a benign no-op (dir simply absent) or a propagated fault.
//
// It distinguishes "journal dir simply absent" (legitimate no-op) from "a
// path component is not a directory" (e.g. agents-home is a regular file, so
// the .promote-journal subpath cannot exist). On Windows, os.ReadDir under a
// regular file maps to a NotExist-class error, so os.IsNotExist alone would
// silently hide the fault. The parent (agents-home) must be an existing
// *directory* for an absent journal dir to be benign; if the parent exists
// but is not a directory, the failure is real and must propagate. The %v (not
// %w) deliberately breaks the fs.ErrNotExist chain so callers cannot
// re-swallow it.
func classifyJournalReadDirError(agentsHome, dir string, err error) ([]PromoteJournalEntry, error) {
	if pi, statErr := os.Lstat(agentsHome); statErr == nil && !pi.IsDir() {
		return nil, fmt.Errorf("reading promote-journal dir %s (%v)", dir, err)
	}
	if os.IsNotExist(err) {
		return nil, nil
	}
	return nil, fmt.Errorf("reading promote-journal dir %s: %w", dir, err)
}

// readPendingJournalEntry decodes a single directory entry into a journal
// entry, returning ok=false for non-JSON files, unreadable/unparseable files,
// and entries already in a terminal state.
func readPendingJournalEntry(dir string, e os.DirEntry) (PromoteJournalEntry, bool) {
	if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
		return PromoteJournalEntry{}, false
	}
	data, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
	if rerr != nil {
		return PromoteJournalEntry{}, false
	}
	var je PromoteJournalEntry
	if json.Unmarshal(data, &je) != nil {
		return PromoteJournalEntry{}, false
	}
	if isTerminalPromoteState(je.State) {
		return PromoteJournalEntry{}, false
	}
	return je, true
}

// isTerminalPromoteState reports whether the journal state indicates the
// promotion has finished (either committed or rolled back). Terminal entries
// should not appear in pending-recovery scans.
func isTerminalPromoteState(state string) bool {
	return state == PromoteStateRCSaved || state == PromoteStateRolledBack
}

// RecoverPendingPromote inspects a journal entry and either rolls it forward
// (when the next step is safe) or surfaces a manual-cleanup notice for states
// past the recoverable line. Returns a short human-readable description of
// what was done; the journal file is removed only when recovery completes
// cleanly.
//
// State machine:
//   - "prepared":         no destructive change yet — delete the journal.
//   - "canonical-copied": canonical exists but source intact; remove canonical.
//   - "source-removed":   source gone, canonical present, no symlink yet —
//     recreate symlink (preferred) so the on-disk layout matches the steady
//     state of a successful promote.
//   - "symlinked":        canonical + symlink ok; only rc.Save was pending —
//     surface a manual rc update notice (we cannot guess the rc.Project field
//     from here).
//   - "rc-saved":         success — just delete the journal.
//   - "rolled-back":      rollback already attempted — just delete.
func RecoverPendingPromote(agentsHome string, e PromoteJournalEntry) (string, error) {
	path := filepath.Join(promoteJournalDirPath(agentsHome), e.ID+".json")
	switch e.State {
	case PromoteStatePrepared:
		if err := RemovePromoteJournal(path); err != nil {
			return "", err
		}
		return "prepared: nothing to undo, journal removed", nil
	case PromoteStateCanonicalCopied:
		if err := os.RemoveAll(e.CanonicalPath); err != nil {
			return "", fmt.Errorf("removing partial canonical %s: %w", e.CanonicalPath, err)
		}
		if err := RemovePromoteJournal(path); err != nil {
			return "", err
		}
		return fmt.Sprintf("canonical-copied: removed partial canonical %s", e.CanonicalPath), nil
	case PromoteStateSourceRemoved:
		// Source gone; canonical has the bytes. Best recovery is to recreate
		// the managed symlink so the on-disk shape matches the steady state
		// of a finished promote.
		if err := os.Symlink(e.CanonicalPath, e.SourcePath); err != nil {
			return "", fmt.Errorf("recreating symlink %s -> %s: %w", e.SourcePath, e.CanonicalPath, err)
		}
		if err := RemovePromoteJournal(path); err != nil {
			return "", err
		}
		return fmt.Sprintf("source-removed: recreated symlink %s -> %s", e.SourcePath, e.CanonicalPath), nil
	case PromoteStateSymlinked:
		// Canonical and symlink already exist; only the manifest update is
		// pending. We cannot infer the project rc without more context, so
		// surface this as a manual notice and keep the journal in place so
		// the user (or a downstream doctor pass) can see it.
		return fmt.Sprintf("symlinked: canonical and symlink are in place for %s %q; rerun `da %ss promote %s` to update .agentsrc.json",
			e.Singular, e.Name, e.Singular, e.Name), nil
	case PromoteStateRCSaved, PromoteStateRolledBack:
		if err := RemovePromoteJournal(path); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s: terminal state, journal removed", e.State), nil
	default:
		return "", fmt.Errorf("unknown promote-journal state %q for entry %s", e.State, e.ID)
	}
}
