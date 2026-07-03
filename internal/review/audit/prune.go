package audit

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ErrUnprunableArchive is returned by PruneArchivesBefore when one or more
// eligible archives are not safe to compact: they are left in place (never
// silently deleted) and named — with a per-archive reason — in the error, while
// intact eligible archives are still compacted. Callers can match it with
// errors.Is. An archive is unprunable when it fails chain/anchor verification
// (tamper) OR reads as an unresolved torn append (see archiveSkipReason).
var ErrUnprunableArchive = errors.New("audit: refusing to prune archive(s) that are not a fully-clean, fully-anchored chain")

// archiveRef is a rotated year-archive file discovered next to the active log,
// paired with the calendar year encoded in its rotation-assigned name.
type archiveRef struct {
	path string
	year int
}

// PruneArchivesBefore compacts the audit log's retained history by removing the
// rotated year-archive files whose year is strictly less than the given year
// (design D5.4: "an admin-only prune command can compact older years"). It
// returns the archive paths it removed (each removed archive's .head sidecar is
// deleted alongside it but not listed).
//
// Prune is deliberately NOT an R6-audited mutation and writes NO audit record:
// R6's mutating-action set (spec line 111 = validActions) covers live review
// state (labels, users, roles), whereas each rotated archive is a frozen,
// self-contained chain with its own genesis and head anchor. Deleting a whole
// such file is retention maintenance, not a live-chain edit — so it needs
// neither an action value (none exists in the closed set) nor a fail-closed
// audit wrapper, and the active log is never touched.
//
// Tamper-safety: an eligible archive is compacted ONLY when it verifies as a
// fully-clean, fully-anchored chain (OK && !TornAppend). A chain/anchor break
// (tamper) and an unresolved torn append are both left in place and reported via
// ErrUnprunableArchive — the torn-append case matters because Verify reports it
// as OK (chain intact) yet it is byte-indistinguishable from a FORGED
// out-of-band append (see VerifyResult.TornAppend), so deleting it could destroy
// tamper evidence. Intact eligible archives are still compacted, so the returned
// slice records real progress even on the error path. The whole pass runs under
// the same in-process mutex and inter-process file lock Append takes on the
// active log, so a concurrent rotation cannot race the enumeration.
func (l *Log) PruneArchivesBefore(year int) ([]string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	release, err := acquireFileLock(l.path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = release() }()

	refs, err := l.archiveRefs()
	if err != nil {
		return nil, err
	}
	removed := []string{}
	var skipped []string
	var cruftErrs []error
	for _, ref := range refs {
		if ref.year >= year {
			continue
		}
		reason, err := archiveSkipReason(ref.path)
		if err != nil {
			return removed, err
		}
		if reason != "" {
			skipped = append(skipped, fmt.Sprintf("%s (%s)", ref.path, reason))
			continue
		}
		if err := removeArchive(ref.path); err != nil {
			if errors.Is(err, errTombstoneCruft) {
				// The archive is fully pruned (both-or-neither holds); only an
				// inert ".pruning" tombstone could not be unlinked. Count it and
				// record the note, then keep pruning the remaining archives.
				removed = append(removed, ref.path)
				cruftErrs = append(cruftErrs, err)
				continue
			}
			return removed, err
		}
		removed = append(removed, ref.path)
	}
	if len(skipped) > 0 {
		return removed, fmt.Errorf("%w: %s", ErrUnprunableArchive, strings.Join(skipped, "; "))
	}
	if len(cruftErrs) > 0 {
		// Each entry wraps errTombstoneCruft; the archives themselves were
		// pruned (they are in removed). errors.Join keeps errors.Is matching.
		return removed, errors.Join(cruftErrs...)
	}
	return removed, nil
}

// archiveRefs enumerates the rotated year-archive files that sit next to the
// active log, pairing each with the year in its name. The active log and any
// non-archive sibling are excluded. A missing log directory yields no refs (an
// unrotated log simply has nothing to prune).
func (l *Log) archiveRefs() ([]archiveRef, error) {
	dir := filepath.Dir(l.path)
	entries, err := readDirFunc(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("audit: read log dir: %w", err)
	}
	activeName := filepath.Base(l.path)
	stem := filepath.Base(archiveBase(l.path)) + "."
	refs := []archiveRef{}
	for _, e := range entries {
		if ref, ok := archiveRefFor(e, dir, activeName, stem); ok {
			refs = append(refs, ref)
		}
	}
	return refs, nil
}

// archiveRefFor classifies one directory entry: it is a prunable archive iff it
// is a file named "<stem><year>[.<n>].jsonl" other than the active log whose
// leading segment parses as a year.
func archiveRefFor(e os.DirEntry, dir, activeName, stem string) (archiveRef, bool) {
	if e.IsDir() {
		return archiveRef{}, false
	}
	name := e.Name()
	if name == activeName || !strings.HasPrefix(name, stem) || !strings.HasSuffix(name, ".jsonl") {
		return archiveRef{}, false
	}
	seg := strings.TrimSuffix(strings.TrimPrefix(name, stem), ".jsonl")
	year, ok := leadingYear(seg)
	if !ok {
		return archiveRef{}, false
	}
	return archiveRef{path: filepath.Join(dir, name), year: year}, true
}

// leadingYear parses the year from an archive's variable segment, which is
// "<year>" or "<year>.<n>" (a second same-year, size-triggered rotation). A
// segment whose dot-suffix is not a plain non-negative integer (e.g.
// "2024.backup"), or whose year portion is not exactly four decimal digits
// (rejecting signed values like "-2024" and non-four-digit spans like "999"
// or "10000"), yields ok=false so the file is left untouched. This tightens
// matching to ONLY the exact rotation-archive formats the writer produces.
func leadingYear(seg string) (int, bool) {
	if i := strings.IndexByte(seg, '.'); i >= 0 {
		// The suffix after the dot must be a plain non-negative integer
		// (the size-rotation sequence number). Anything else — e.g.
		// "backup", "tmp" — means this is not a rotation archive and
		// must be ignored.
		if _, err := strconv.Atoi(seg[i+1:]); err != nil {
			return 0, false
		}
		seg = seg[:i]
	}
	// Accept ONLY a plain 4-decimal-digit segment (no sign prefix, no fewer
	// or more digits). Signed inputs like "-024" and non-4-digit spans like
	// "999" or "10000" are not valid rotation-archive years.
	if len(seg) != 4 {
		return 0, false
	}
	for _, c := range seg {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	year, _ := strconv.Atoi(seg)
	return year, true
}

// archiveSkipReason reports why the archive at path may NOT be pruned, or "" when
// it is a fully-clean, fully-anchored chain safe to compact. An operational read
// error is returned as err. Two states block pruning:
//   - a chain or head-anchor break (OK=false): tamper — never delete the evidence;
//   - an unresolved torn append (OK=true but TornAppend=true): the head anchor is
//     one behind, which Verify cannot distinguish from a forged out-of-band
//     append, so the archive must be repaired (`da review audit repair`) before it
//     is safe to delete.
func archiveSkipReason(path string) (string, error) {
	res, err := Open(path).Verify()
	if err != nil {
		return "", fmt.Errorf("audit: verify archive %s: %w", path, err)
	}
	if !res.OK {
		return "corrupt chain: " + res.Reason, nil
	}
	if res.TornAppend {
		return "unresolved torn-append / head-anchor mismatch; run `da review audit repair` before pruning", nil
	}
	return "", nil
}

// pruningSuffix tombstones an archive or its head sidecar mid-prune. Renaming a
// file to this suffix atomically moves it off its attestable path; a leftover
// tombstone (from a benign post-commit unlink failure or a crash) is inert —
// archiveRefFor ignores any name not ending in ".jsonl", and Verify never
// consults a ".pruning" sidecar.
const pruningSuffix = ".pruning"

// errTombstoneCruft signals that an archive was fully pruned — both the archive
// and its head sidecar left their attestable paths, so the both-or-neither
// invariant holds — but an inert ".pruning" tombstone could not be unlinked
// afterward. The prune of that archive is COMPLETE (it is still counted in
// removed); the leftover is reported for visibility, never treated as a failure
// that could corrupt attestation. Callers can match it with errors.Is.
var errTombstoneCruft = errors.New("audit: archive pruned but tombstone cleanup left inert cruft")

// removeArchive deletes an archive file and its head-anchor sidecar as one
// BOTH-OR-NEITHER transaction. No partial failure may leave a valid archive
// WITHOUT its head attestation (the round-3 hazard) or an orphaned/misreported
// head with its archive gone (the round-2 hazard): neither delete order is safe
// on its own, so the operation is made transactional rather than reordered. An
// absent head sidecar is not an error — a torn-first archive may never have had
// one.
//
// Rename-based tombstones make the commit atomic (a rename is one observable
// transition on a single filesystem):
//
//  1. Tombstone the head sidecar (if present), then the archive, by renaming
//     each to "<path>.pruning". A tombstoned file has left its attestable path,
//     so an observer sees neither the archive nor its head at their real paths.
//  2. If EITHER tombstone rename fails, roll the already-tombstoned file BACK to
//     its real path — restoring the untouched both-present state — and return
//     the error. The caller never counts this archive. A rollback that itself
//     fails is the one unavoidable inconsistent state; it is surfaced as a LOUD
//     explicit error naming both paths, never silently.
//  3. Once BOTH are tombstoned the transaction has COMMITTED (neither remains at
//     an attestable path). Unlink both tombstones; an unlink failure here cannot
//     corrupt attestation — the file is already off its real path — so it is
//     reported as errTombstoneCruft while the prune of that archive is treated
//     as done.
//
// All renames and unlinks route through fsops per the FS-helpers guard.
func removeArchive(path string) error {
	head := headPathFor(path)
	headTomb := head + pruningSuffix
	archiveTomb := path + pruningSuffix

	// Step 1a: tombstone the head sidecar, tolerating an absent one.
	headTombstoned := false
	if err := renameArchiveFunc(head, headTomb); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("audit: tombstone archive head %s: %w", head, err)
		}
	} else {
		headTombstoned = true
	}

	// Step 1b: tombstone the archive. On failure, roll the head back so the
	// caller observes the untouched both-present state.
	if err := renameArchiveFunc(path, archiveTomb); err != nil {
		if headTombstoned {
			if rbErr := renameArchiveFunc(headTomb, head); rbErr != nil {
				return fmt.Errorf(
					"audit: CRITICAL: archive %s tombstone failed (%v) AND head rollback to %s failed (%w): the archive is present but its head anchor is stranded at %s — restore the head manually before trusting this archive",
					path, err, head, rbErr, headTomb)
			}
		}
		return fmt.Errorf("audit: tombstone archive %s: %w", path, err)
	}

	// Step 2: committed. Unlink both tombstones; any leftover is inert cruft.
	var cruft []string
	if headTombstoned {
		if err := removeFunc(headTomb); err != nil {
			cruft = append(cruft, fmt.Sprintf("%s (%v)", headTomb, err))
		}
	}
	if err := removeFunc(archiveTomb); err != nil {
		cruft = append(cruft, fmt.Sprintf("%s (%v)", archiveTomb, err))
	}
	if len(cruft) > 0 {
		return fmt.Errorf("%w: %s", errTombstoneCruft, strings.Join(cruft, "; "))
	}
	return nil
}
