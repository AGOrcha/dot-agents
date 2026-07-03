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
			return removed, err
		}
		removed = append(removed, ref.path)
	}
	if len(skipped) > 0 {
		return removed, fmt.Errorf("%w: %s", ErrUnprunableArchive, strings.Join(skipped, "; "))
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

// removeArchive deletes an archive file and its head-anchor sidecar (an absent
// sidecar is not an error). Removal routes through fsops per the FS-helpers guard.
//
// Delete order: the .head sidecar is removed FIRST, then the archive. This
// ensures that a mid-failure (head removed but archive deletion fails) leaves
// the archive file itself intact and recoverable — it can be re-pruned once the
// underlying I/O problem is resolved. The reverse order (archive first) would
// orphan the .head sidecar if head deletion subsequently failed, destroying
// tamper evidence without any compensating recovery path.
func removeArchive(path string) error {
	head := headPathFor(path)
	if err := removeFunc(head); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("audit: remove archive head %s: %w", head, err)
	}
	if err := removeFunc(path); err != nil {
		return fmt.Errorf("audit: remove archive %s: %w", path, err)
	}
	return nil
}
