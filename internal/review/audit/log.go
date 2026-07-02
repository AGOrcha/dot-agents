package audit

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/AGOrcha/dot-agents/internal/agentslock"
	"github.com/AGOrcha/dot-agents/internal/fsops"
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

	// readDirFunc lists a log directory's entries; a seam so
	// PruneArchivesBefore's directory-read error branch is coverable.
	readDirFunc = os.ReadDir
	// removeFunc deletes a pruned archive (and its head sidecar) through fsops so
	// the FS-helpers guard is satisfied; a seam so the remove-error branch is
	// coverable.
	removeFunc = fsops.Remove

	// openAppend opens path for appending, creating the file and its parent
	// directory if needed. It uses O_APPEND so concurrent appenders never
	// truncate each other — the log is append-only by construction, not by
	// convention. It is a seam so writeLine's write/close error branches are
	// coverable with an injected failing file.
	openAppend = func(path string) (appendFile, error) {
		// fsops.MkdirAll (not os.MkdirAll) so the Windows fallback + hardening
		// apply uniformly — the FS-helpers guard forbids raw os.* mutators
		// outside internal/fsops.
		if err := fsops.MkdirAll(filepath.Dir(path), 0o755); err != nil {
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

	// acquireFileLock takes the advisory inter-process lock guarding the log
	// file and returns a release func the caller must invoke. It defaults to the
	// shared agentslock primitive (a "<path>.lock" mkdir lock with stale
	// reclaim) so concurrent da processes serialize their read-prev+append
	// critical sections against each other, not just goroutines in one process.
	acquireFileLock = agentslock.AcquireFileLock

	// writeHeadFile persists the head-anchor sidecar atomically. It is a seam so
	// the head write-error branch is coverable.
	writeHeadFile = fsops.WriteFileAtomic
)

// headSuffix names the head-anchor sidecar that pins the tail record's hash so
// tail modification, tail truncation, and out-of-band appends are detectable
// (the bare hash chain alone cannot attest its own last link).
const headSuffix = ".head"

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

// Log is a handle to one audit log file. It is cheap to construct; all durable
// state lives on disk so out-of-band edits (rotation, prune) are observed on the
// next call. The mutex serializes Append against other goroutines sharing this
// handle; cross-process serialization is the file lock Append takes.
type Log struct {
	path    string
	sizeCap int64
	mu      sync.Mutex
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
	recs, _, err := l.readStored()
	return recs, err
}

// readStored reads the active log and returns both the decoded records and, in
// lockstep, the exact stored line bytes each was parsed from. The raw lines are
// what the chain and head anchor attest (see hashBytes); the decoded structs
// exist for validation and for callers that consume record fields.
func (l *Log) readStored() ([]Record, [][]byte, error) {
	data, err := readFile(l.path)
	if errors.Is(err, os.ErrNotExist) {
		return []Record{}, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("audit: read log: %w", err)
	}
	return parseStored(data)
}

// parseStored decodes JSON-lines content into records plus their raw stored
// line bytes (post-trim), skipping blank lines. Each raw slice is copied out of
// the scanner's reused buffer.
func parseStored(data []byte) ([]Record, [][]byte, error) {
	out := []Record{}
	raws := [][]byte{}
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
			return nil, nil, fmt.Errorf("audit: parse log line %d: %w", line, err)
		}
		out = append(out, r)
		raws = append(raws, append([]byte(nil), raw...))
	}
	if err := sc.Err(); err != nil {
		return nil, nil, fmt.Errorf("audit: scan log: %w", err)
	}
	return out, raws, nil
}

// Append chains and writes one record for the given event and returns the
// written record.
//
// Concurrency: reading the current tail to compute prev_hash and writing the new
// record MUST be one indivisible step — two callers that both read tail N and
// both chain to it would fork the chain and later fail Verify. Append therefore
// holds the handle's in-process mutex AND the inter-process file lock across the
// whole read-prev → rotate → append → head-update section, so concurrent
// appenders (goroutines or separate da processes) are strictly serialized.
//
// Before writing it applies the rotation policy (yearly or size cap), so a year
// boundary or an oversized file starts a fresh chain in a dated archive. The new
// record's prev_hash is the SHA-256 of the last stored LINE (its exact bytes on
// disk, not a re-marshal) in the (post-rotation) active file, or GenesisPrevHash
// when the file is empty. After the line lands, the head anchor is advanced to
// the hash of the newly written bytes so the tail record stays attestable.
//
// Durability is AT-LEAST-ONCE: the record line and the head anchor are two
// writes, so on an error return (or a crash mid-call) the record MAY already be
// durable with the anchor one behind. Callers must not blind-retry — a retry
// would append a duplicate record; use the Event's RequestID to detect an
// already-landed record before retrying. The stale-anchor state is benign:
// Verify classifies it as TornAppend (not tamper) and it heals on the next
// successful Append here (the anchor is recomputed from the on-disk tail) or
// explicitly via RepairHead.
func (l *Log) Append(e Event) (Record, error) {
	if err := e.validate(); err != nil {
		return Record{}, err
	}
	now := e.Now
	if now.IsZero() {
		now = timeNow()
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	release, err := acquireFileLock(l.path)
	if err != nil {
		return Record{}, err
	}
	defer func() { _ = release() }()

	recs, raws, err := l.readStored()
	if err != nil {
		return Record{}, err
	}
	rotated, err := l.maybeRotate(recs, now)
	if err != nil {
		return Record{}, err
	}
	if rotated {
		recs, raws = nil, nil
	}
	prev := GenesisPrevHash
	if n := len(raws); n > 0 {
		prev = hashBytes(raws[n-1])
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
	// Marshal exactly once: the bytes hashed into the head anchor are the
	// bytes written to disk, so the stored line and its attestation can never
	// diverge.
	line, err := marshal(rec)
	if err != nil {
		return Record{}, fmt.Errorf("%w: %v", ErrMarshal, err)
	}
	if err := appendLine(l.path, line); err != nil {
		return Record{}, err
	}
	if err := l.writeHead(len(recs)+1, hashBytes(line)); err != nil {
		return Record{}, err
	}
	return rec, nil
}

// RepairHead heals the one benign inconsistency an interrupted Append leaves
// behind: a clean, fully-chained log whose head anchor is exactly one record
// behind (VerifyResult.TornAppend). It re-verifies under the same in-process
// and inter-process locks Append takes, and only when the state is exactly the
// torn-append shape does it advance the anchor to the on-disk tail. Every other
// state is left untouched: a clean log is a no-op, and a tamper finding is
// returned as-is so repair can never be used to paper over a real integrity
// break. The returned result reflects the post-repair state.
func (l *Log) RepairHead() (VerifyResult, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	release, err := acquireFileLock(l.path)
	if err != nil {
		return VerifyResult{}, err
	}
	defer func() { _ = release() }()

	res, raws, err := l.verifyState()
	if err != nil || !res.OK || !res.TornAppend {
		return res, err
	}
	n := len(raws)
	if err := l.writeHead(n, hashBytes(raws[n-1])); err != nil {
		return VerifyResult{}, err
	}
	return VerifyResult{OK: true, Count: n}, nil
}

// headAnchor pins the active log's tail: the count of records and the hash of
// the last one. Verify recomputes the on-disk tail and compares, catching tail
// modification, tail truncation, and forged out-of-band appends — none of which
// the forward hash chain can detect on its own last link.
type headAnchor struct {
	Count    int    `json:"count"`
	TailHash string `json:"tail_hash"`
}

// headPathFor returns the head-anchor sidecar path for a log file.
func headPathFor(logPath string) string { return logPath + headSuffix }

// writeHead persists the head anchor for the active log atomically. The tiny
// JSON is built by hand (count is an int, tail hash is fixed hex) so there is no
// marshal error path to smuggle in.
func (l *Log) writeHead(count int, tailHash string) error {
	data := []byte(fmt.Sprintf("{\"count\":%d,\"tail_hash\":%q}\n", count, tailHash))
	if err := writeHeadFile(headPathFor(l.path), data); err != nil {
		return fmt.Errorf("audit: write head: %w", err)
	}
	return nil
}

// readHead loads the head anchor. A missing sidecar reports (_, false, nil) so
// Verify can distinguish "never anchored" from "anchor removed with records
// still present".
func (l *Log) readHead() (headAnchor, bool, error) {
	data, err := readFile(headPathFor(l.path))
	if errors.Is(err, os.ErrNotExist) {
		return headAnchor{}, false, nil
	}
	if err != nil {
		return headAnchor{}, false, fmt.Errorf("audit: read head: %w", err)
	}
	var h headAnchor
	if err := json.Unmarshal(data, &h); err != nil {
		return headAnchor{}, false, fmt.Errorf("audit: parse head: %w", err)
	}
	return h, true, nil
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
	if err := l.rotateHead(dest); err != nil {
		return false, err
	}
	return true, nil
}

// rotateHead moves the active log's head anchor alongside its archived file so
// the frozen archive keeps its tail attestation; the active head path is then
// free for the fresh chain's first Append to recreate. A log that has no head
// yet (rotation before any successful head write) is a no-op.
func (l *Log) rotateHead(dest string) error {
	src := headPathFor(l.path)
	if _, err := statFunc(src); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("audit: stat head: %w", err)
	}
	if err := renameFunc(src, headPathFor(dest)); err != nil {
		return fmt.Errorf("audit: rotate head: %w", err)
	}
	return nil
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
