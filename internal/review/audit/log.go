package audit

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultLogName is the audit log filename under the review state directory.
// The full active path is .agents/active/review/audit.log.jsonl (design D5.4).
const DefaultLogName = "audit.log.jsonl"

// DefaultSizeCap is the size at which the active log rotates even before the
// year turns over (spec OQ4: yearly OR a size cap). 100 MiB matches the OQ4
// recommendation.
const DefaultSizeCap int64 = 100 * 1024 * 1024

// Package seams over the filesystem and clock so the error and rotation
// branches are coverable deterministically in tests, following the pattern in
// internal/fsops and internal/review/auth.
var (
	readFile   = os.ReadFile
	statFunc   = os.Stat
	renameFunc = os.Rename
	timeNow    = func() time.Time { return time.Now().UTC() }

	// openAppend opens path for appending, creating the file and its parent
	// directory if needed. It uses O_APPEND so concurrent appenders never
	// truncate each other — the log is append-only by construction, not by
	// convention. It is a seam so writeLine's write/close error branches are
	// coverable with an injected failing file.
	openAppend = func(path string) (appendFile, error) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("audit: create log dir: %w", err)
		}
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return nil, fmt.Errorf("audit: open log: %w", err)
		}
		return f, nil
	}

	// appendLine appends one JSON line (plus a trailing newline) to path. It is
	// a package var so Append's callers can stub the whole write in tests.
	appendLine = writeLine
)

// appendFile is the subset of *os.File that writeLine uses. Modeling it as an
// interface lets tests drive the write- and close-error paths deterministically
// without platform-specific filesystem tricks (mirroring internal/fsops).
type appendFile interface {
	Write([]byte) (int, error)
	Close() error
}

// writeLine appends line (plus a newline) to the file at path via the openAppend
// seam, closing the file and mapping each failure to a wrapped error.
func writeLine(path string, line []byte) error {
	f, err := openAppend(path)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		_ = f.Close()
		return fmt.Errorf("audit: write log: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("audit: close log: %w", err)
	}
	return nil
}

// Log is a handle to one audit log file. It is cheap to construct; all state
// lives on disk so out-of-band edits (rotation, prune) are observed on the next
// call.
type Log struct {
	path    string
	sizeCap int64
}

// Open returns a Log backed by the file at path (typically
// .agents/active/review/audit.log.jsonl). The file need not exist yet; the
// first Append creates it and its parent directory.
func Open(path string) *Log {
	return &Log{path: path, sizeCap: DefaultSizeCap}
}

// Path returns the active log file path.
func (l *Log) Path() string { return l.path }

// WithSizeCap returns a copy of the log whose size-based rotation threshold is
// cap bytes. A non-positive cap disables size-based rotation (yearly rotation
// still applies). It is used to exercise size rotation in tests and to let an
// operator tune the cap.
func (l *Log) WithSizeCap(cap int64) *Log {
	return &Log{path: l.path, sizeCap: cap}
}

// Records reads and parses every record in the active log file, in file order.
// A missing file yields an empty slice and no error (an unwritten log is a
// valid empty chain). A malformed line returns an error naming the 1-based line
// number.
func (l *Log) Records() ([]Record, error) {
	data, err := readFile(l.path)
	if errors.Is(err, os.ErrNotExist) {
		return []Record{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("audit: read log: %w", err)
	}
	return parseRecords(data)
}

// parseRecords decodes JSON-lines content into records, skipping blank lines.
func parseRecords(data []byte) ([]Record, error) {
	out := []Record{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	// Audit lines are single JSON objects but may grow with free-form targets;
	// raise the scanner's token cap well above the default 64 KiB.
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	line := 0
	for sc.Scan() {
		line++
		raw := bytes.TrimSpace(sc.Bytes())
		if len(raw) == 0 {
			continue
		}
		var r Record
		if err := decode(raw, &r); err != nil {
			return nil, fmt.Errorf("audit: parse log line %d: %w", line, err)
		}
		out = append(out, r)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("audit: scan log: %w", err)
	}
	return out, nil
}

// Append chains and writes one record for the given event and returns the
// written record. Before writing it applies the rotation policy (yearly or
// size cap), so a year boundary or an oversized file starts a fresh chain in a
// dated archive. The new record's prev_hash is the SHA-256 of the last record
// in the (post-rotation) active file, or GenesisPrevHash when the file is
// empty.
func (l *Log) Append(e Event) (Record, error) {
	if err := e.validate(); err != nil {
		return Record{}, err
	}
	now := e.Now
	if now.IsZero() {
		now = timeNow()
	}
	recs, err := l.Records()
	if err != nil {
		return Record{}, err
	}
	rotated, err := l.maybeRotate(recs, now)
	if err != nil {
		return Record{}, err
	}
	if rotated {
		recs = nil
	}
	prev := GenesisPrevHash
	if n := len(recs); n > 0 {
		prev, err = hashRecord(recs[n-1])
		if err != nil {
			return Record{}, err
		}
	}
	rec := Record{
		SchemaVersion: SchemaVersion,
		Ts:            now.UTC(),
		Actor:         e.Actor,
		Role:          e.Role,
		Action:        e.Action,
		Target:        e.Target,
		BeforeHash:    e.BeforeHash,
		AfterHash:     e.AfterHash,
		PrevHash:      prev,
		RequestID:     e.RequestID,
	}
	line, err := canonicalBytes(rec)
	if err != nil {
		return Record{}, err
	}
	if err := appendLine(l.path, line); err != nil {
		return Record{}, err
	}
	return rec, nil
}

// shouldRotate reports whether the active file must be rotated before appending
// a record stamped at now. Rotation fires when the newest existing record is
// from an earlier calendar year (yearly rotation) or when the file has reached
// the size cap (OQ4). An empty log never rotates.
func (l *Log) shouldRotate(recs []Record, now time.Time) (bool, error) {
	if len(recs) == 0 {
		return false, nil
	}
	if recs[len(recs)-1].Ts.UTC().Year() != now.UTC().Year() {
		return true, nil
	}
	if l.sizeCap > 0 {
		info, err := statFunc(l.path)
		if err != nil {
			return false, fmt.Errorf("audit: stat log: %w", err)
		}
		if info.Size() >= l.sizeCap {
			return true, nil
		}
	}
	return false, nil
}

// maybeRotate rotates the active file when shouldRotate says so and reports
// whether a rotation happened. The closed file is renamed to a dated archive
// keyed on the year of its last record; the active path is then free for a
// fresh genesis chain.
func (l *Log) maybeRotate(recs []Record, now time.Time) (bool, error) {
	do, err := l.shouldRotate(recs, now)
	if err != nil || !do {
		return false, err
	}
	year := recs[len(recs)-1].Ts.UTC().Year()
	dest, err := l.nextArchivePath(year)
	if err != nil {
		return false, err
	}
	if err := renameFunc(l.path, dest); err != nil {
		return false, fmt.Errorf("audit: rotate log: %w", err)
	}
	return true, nil
}

// nextArchivePath returns the archive destination for a rotation closing the
// given year: audit.log.<year>.jsonl, or audit.log.<year>.<n>.jsonl when an
// archive for that year already exists (a size-triggered second rotation within
// the same year).
func (l *Log) nextArchivePath(year int) (string, error) {
	base := archiveBase(l.path)
	candidate := fmt.Sprintf("%s.%d.jsonl", base, year)
	if _, err := statFunc(candidate); errors.Is(err, os.ErrNotExist) {
		return candidate, nil
	} else if err != nil {
		return "", fmt.Errorf("audit: stat archive: %w", err)
	}
	for n := 1; ; n++ {
		candidate = fmt.Sprintf("%s.%d.%d.jsonl", base, year, n)
		_, err := statFunc(candidate)
		if errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		}
		if err != nil {
			return "", fmt.Errorf("audit: stat archive: %w", err)
		}
	}
}

// archiveBase strips the trailing ".jsonl" (or, defensively, any final
// extension) from the active path so archives share its stem: for
// ".../audit.log.jsonl" it returns ".../audit.log".
func archiveBase(path string) string {
	if strings.HasSuffix(path, ".jsonl") {
		return strings.TrimSuffix(path, ".jsonl")
	}
	return strings.TrimSuffix(path, filepath.Ext(path))
}
