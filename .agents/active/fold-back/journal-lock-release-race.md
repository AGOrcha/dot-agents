# Fold-back: agentslock lock-RELEASE lifecycle race (windows) — RESOLVED

**Source**: `TestFlushSucceedsAfterContendingReleaseMidWait` low-rate flake on
windows-latest (most recently run 28554198278 attempt 1: 5.01s timeout — the
contender never acquired within its budget) + owner-machine runtime failures
(`da config explain` / `da install` lock errors, see
`.agents/history/provadm-windows-da-lock-observation.md`, PR #254).
**Observed**: recurring; deferred for a careful root-cause fix (integrity was
intact — atomic merge-write meant no data loss, only stranded acquirers).
**Status**: RESOLVED — PR `fix/agentslock-release-race` (agentslock: fix
lock-release lifecycle race stranding contenders).

## Root cause (one line)

In-place `RemoveAll` release is non-atomic (holder file deleted, then dir), and
a holderless lock dir was judged by the full 30s TTL via dir mtime — 6x the 5s
acquire budget — so any partially-failed release stranded every contender and
every fresh `da` process into a guaranteed timeout.

## Fix shape

- Release/reclaim free the lock NAME only via a single atomic rename to a
  unique trash sibling (`<lockdir>.stale-<pid>-<nanos>`), never by deleting in
  place; trash is deleted out-of-band and swept on later releases.
- A lock dir with no readable holder file is reclaimable after a 2s
  `lockNoHolderGrace` instead of the 30s TTL (mid-acquire window is two local
  fs ops; remnants must clear inside the 5s acquire budget).
- Release verifies holder identity before touching the name, so an overdue
  release (e.g. post-suspend, after a TTL reclaim) cannot steal a successor's
  live lock.
- The acquire-timeout error now names the blocking holder (pid, hold age, TTL).

## Owner work-PC note (separate cause, related fix)

The ProvAdm trace (PR #254) shows PLAIN file writes also failing (`Access is
denied` on TASKS.yaml, plan-create mkdir) — an ENVIRONMENTAL denial (Controlled
Folder Access / OneDrive-protected Documents / AV), not (only) the release
race. The same PR fixes the adjacent misdiagnosis: a persistent
ERROR_ACCESS_DENIED on the lock-dir Mkdir was classified as the delete-pending
transient and retried into a generic "timed out"; it now fails fast (~300ms)
with an actionable error naming the protected parent and likely causes.
Follow-up (not implemented here): a `da doctor` probe-write check (write+delete
a temp file in the repo and the lock parent, naming CFA/OneDrive/AV on denial).
