package audit

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ErrCorruptArchive is returned by PruneArchivesBefore when one or more eligible
// archives fail chain verification: they are left in place (never silently
// deleted) and named in the error, while intact eligible archives are still
// compacted. Callers can match it with errors.Is.
var ErrCorruptArchive = errors.New("audit: refusing to prune corrupt archive(s)")

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
// Safety: an eligible archive is verified as an intact self-contained chain
// before removal; a corrupt one is left in place and reported via
// ErrCorruptArchive (intact eligible archives are still compacted, so the
// returned slice records real progress even on the error path). The whole pass
// runs under the same in-process mutex and inter-process file lock Append takes
// on the active log, so a concurrent rotation cannot race the enumeration.
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
	var corrupt []string
	for _, ref := range refs {
		if ref.year >= year {
			continue
		}
		intact, err := archiveIntact(ref.path)
		if err != nil {
			return removed, err
		}
		if !intact {
			corrupt = append(corrupt, ref.path)
			continue
		}
		if err := removeArchive(ref.path); err != nil {
			return removed, err
		}
		removed = append(removed, ref.path)
	}
	if len(corrupt) > 0 {
		return removed, fmt.Errorf("%w: %s", ErrCorruptArchive, strings.Join(corrupt, ", "))
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
// segment that is not a plain integer (a stray sibling sharing the stem) yields
// ok=false so it is left untouched.
func leadingYear(seg string) (int, bool) {
	if i := strings.IndexByte(seg, '.'); i >= 0 {
		seg = seg[:i]
	}
	year, err := strconv.Atoi(seg)
	if err != nil {
		return 0, false
	}
	return year, true
}

// archiveIntact reports whether the archive at path is a fully intact,
// self-contained chain (safe to compact). An operational read error is returned
// as err; a chain or head-anchor break yields ok=false (corrupt — do not delete).
func archiveIntact(path string) (bool, error) {
	res, err := Open(path).Verify()
	if err != nil {
		return false, fmt.Errorf("audit: verify archive %s: %w", path, err)
	}
	return res.OK, nil
}

// removeArchive deletes an archive file and its head-anchor sidecar (an absent
// sidecar is not an error). Removal routes through fsops per the FS-helpers guard.
func removeArchive(path string) error {
	if err := removeFunc(path); err != nil {
		return fmt.Errorf("audit: remove archive %s: %w", path, err)
	}
	head := headPathFor(path)
	if err := removeFunc(head); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("audit: remove archive head %s: %w", head, err)
	}
	return nil
}
