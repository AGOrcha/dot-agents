// Package journal is the append-only, crash-survivable event log that backs the
// session-handoff feature — the "episodic" typed view of agent state
// (events/history/decisions). Every state-mutating da command appends one typed
// Envelope (a single NDJSON line) on success, so a session killed mid-flight can
// be reconstructed from durable file state rather than re-grounded by replaying
// git/gh/da-status bursts.
//
// # Layout (D9)
//
// The log lives OFF the git-tracked tree, under the XDG state directory, keyed by
// a stable per-repo fingerprint so one journal can span the several repos a
// single session touches (sweep/drift) without ever polluting a working tree:
//
//	<config.AgentsStateDir()>/journal/<repo-fingerprint>/
//	    events.log     append-only NDJSON, one Envelope per line  (this task)
//	    snapshot.json  PreCompact deterministic live-state snapshot (later task)
//	    reasoned.log   append-only reasoned-delta overlay          (later task)
//
// This package owns the directory layout; later tasks add snapshot.json and
// reasoned.log writers alongside events.log.
//
// # Durability (D9 / R1)
//
// A line append is a SINGLE os.Write of the full marshaled Envelope plus its
// trailing newline, performed under the package's interprocess advisory lock
// (agentslock.AcquireFileLock) so concurrent da processes can never interleave or
// tear each other's lines. Appends record what HAPPENED — a failed command emits
// a "failed" event, never a fabricated observed delta.
package journal

import (
	"path/filepath"

	"github.com/AGOrcha/dot-agents/internal/config"
)

// Schema is the envelope schema name stamped on every event. It namespaces the
// journal's records within the broader event ecosystem and lets a reader reject
// foreign lines without guessing.
const Schema = "session-handoff-journal/event"

// Version is the envelope schema version. Bump it when the envelope's required
// shape changes incompatibly; readers gate on it.
const Version = 1

// dirPerm/filePerm are the journal's create modes: owner-only, because the log
// can carry repo identity and command inputs that are not meant to be
// world-readable.
const (
	dirPerm  = 0o700
	filePerm = 0o600
)

// eventsLogName is the NDJSON append log within a repo's journal directory.
const eventsLogName = "events.log"

// RepoDir returns the journal directory for the repository at repoPath:
// <AgentsStateDir>/journal/<fingerprint>. The directory is not created here; the
// appender MkdirAlls it before the first write.
func RepoDir(repoPath string) string {
	return filepath.Join(config.AgentsStateDir(), "journal", Fingerprint(repoPath))
}

// EventsLogPath returns the events.log path for the repository at repoPath.
func EventsLogPath(repoPath string) string {
	return filepath.Join(RepoDir(repoPath), eventsLogName)
}
