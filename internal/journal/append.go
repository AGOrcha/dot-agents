package journal

import (
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"github.com/AGOrcha/dot-agents/internal/agentslock"
	"github.com/AGOrcha/dot-agents/internal/fsops"
)

// seqCounter assigns each Emit a process-monotonic sequence number, the
// tiebreaker for records sharing a nanosecond timestamp. It resets per process;
// cross-session replay orders primarily by TS, with Seq disambiguating only
// within a single TS, so a per-process counter is sufficient and lock-free.
var seqCounter atomic.Int64

// now is the clock seam (overridable in tests) used to stamp the envelope TS.
var now = time.Now

// openFile and acquireLock are seams over the two failure-prone syscalls in the
// append path, so tests can drive the lock / open / write / release error
// branches deterministically without staging real filesystem faults.
var (
	openFile    = os.OpenFile
	acquireLock = agentslock.AcquireFileLock
)

// timeFormat is the envelope timestamp layout: RFC3339 with nanoseconds, in UTC,
// so records sort lexicographically in true chronological order.
const timeFormat = time.RFC3339Nano

// Emit appends one event to the journal for the repository at repoPath. It is the
// single append entrypoint commands call. The envelope's Schema, Version, TS,
// Seq, and (when empty) CwdRepo are stamped here; the caller supplies Actor,
// Command, EventType, and the payloads.
//
// Durability (D9 / R1): the event is written as a SINGLE os.Write of the full
// NDJSON line (record + trailing newline) under the package interprocess lock, so
// concurrent da processes never interleave or tear lines. The journal directory
// is created on first append. Per R1 a failed event never carries an observed
// delta: Emit drops Observed when EventType is EventFailed.
func Emit(repoPath string, e Envelope) error {
	e.Schema = Schema
	e.Version = Version
	e.Seq = seqCounter.Add(1)
	if e.TS == "" {
		e.TS = now().UTC().Format(timeFormat)
	}
	if e.CwdRepo == "" {
		e.CwdRepo = Fingerprint(repoPath)
	}
	if e.EventType == EventFailed {
		e.Observed = nil
	}

	line, err := e.MarshalLine()
	if err != nil {
		return fmt.Errorf("journal: marshal event: %w", err)
	}

	dir := RepoDir(repoPath)
	if err := fsops.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("journal: create dir %s: %w", dir, err)
	}
	return appendLine(EventsLogPath(repoPath), line)
}

// appendLine writes line to logPath as a single append under the interprocess
// lock. Acquire → open O_APPEND → one Write → close → release, with every error
// surfaced and the lock always released. A write/close failure is returned even
// though release still runs, so a partial filesystem state is never reported as
// success.
func appendLine(logPath string, line []byte) (err error) {
	release, err := acquireLock(logPath)
	if err != nil {
		return fmt.Errorf("journal: lock %s: %w", logPath, err)
	}
	defer func() {
		if relErr := release(); relErr != nil && err == nil {
			err = fmt.Errorf("journal: release lock %s: %w", logPath, relErr)
		}
	}()

	f, err := openFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, filePerm)
	if err != nil {
		return fmt.Errorf("journal: open %s: %w", logPath, err)
	}
	if _, err = f.Write(line); err != nil {
		_ = f.Close()
		return fmt.Errorf("journal: append %s: %w", logPath, err)
	}
	if err = f.Close(); err != nil {
		return fmt.Errorf("journal: close %s: %w", logPath, err)
	}
	return nil
}
