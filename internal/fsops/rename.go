package fsops

import "os"

// Rename atomically renames (moves) oldpath to newpath on the same volume.
//
// Deliberately SINGLE-SHOT: no retry loop and no PowerShell fallback, unlike
// the other Windows variants in this package. Callers (notably the
// internal/agentslock release/reclaim lifecycle) build atomicity arguments on
// "one rename syscall, one observable transition" — the lock name either moved
// or it did not. A wrapper that silently retried would widen those callers'
// race windows (e.g. a retried rename against a lock name that a rival has
// since re-acquired would steal the rival's live lock). Callers for whom a
// bounded retry is the correct semantic loop over this primitive themselves.
func Rename(oldpath, newpath string) error {
	return os.Rename(oldpath, newpath)
}
