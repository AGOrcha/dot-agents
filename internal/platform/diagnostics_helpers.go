// Package-level diagnostics helpers — composition primitives shared by per-
// platform readers introduced in Phase 1+. These helpers are PURE: they take
// path inputs, perform read-only filesystem queries via the existing
// internal/links primitives, and return diagnostic structs. They never
// mutate state and they never log.

package platform

import (
	"os"
	"path/filepath"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/links"
)

// ScanSingleFileLinks evaluates each spec and returns a BrokenLink for every
// entry that exists at LinkPath but does not satisfy any CanonicalPaths
// match. The rules mirror what cursor + the single-file managed paths in
// doctor.go already encode:
//
//  1. If LinkPath does not exist on disk, nothing is reported — an absent
//     link is "not present" rather than "broken".
//  2. If LinkPath is hard-linked to ANY of CanonicalPaths, it is healthy
//     and nothing is reported.
//  3. If LinkPath is a resolvable managed link (POSIX symlink / Windows
//     junction) and its target is missing, it is reported broken with the
//     raw target in Dest.
//  4. If LinkPath is a resolvable managed link and its target exists but
//     does not match any canonical (mis-pointed), it is also reported
//     broken — the canonical contract is "link matches a known source",
//     not "link resolves to anything".
//
// Note that platforms supply the PlatformID at the call site by post-
// processing the returned slice (this keeps the helper platform-agnostic
// and avoids threading platform identity through every spec).
func ScanSingleFileLinks(specs []SingleFileLinkSpec) []BrokenLink {
	var broken []BrokenLink
	for _, spec := range specs {
		if bl, ok := classifySingleFileLink(spec); ok {
			broken = append(broken, bl)
		}
	}
	return broken
}

// classifySingleFileLink encapsulates the per-spec branching so
// ScanSingleFileLinks remains a simple loop with a low cognitive-complexity
// score; per the no-lazy-allowlist rule we extract rather than allow-list.
func classifySingleFileLink(spec SingleFileLinkSpec) (BrokenLink, bool) {
	if _, err := os.Lstat(spec.LinkPath); err != nil {
		return BrokenLink{}, false
	}
	for _, canonical := range spec.CanonicalPaths {
		if linked, _ := links.AreHardlinked(spec.LinkPath, canonical); linked {
			return BrokenLink{}, false
		}
	}
	raw, isLink := links.ManagedLinkTarget(spec.LinkPath)
	if !isLink {
		// Not a managed link and not hard-linked to a canonical: from the
		// helper's perspective this is unmanaged user content (a plain
		// file). Report nothing — per-platform readers decide whether to
		// flag this elsewhere.
		return BrokenLink{}, false
	}
	resolved := resolveLinkDest(spec.LinkPath, raw)
	if _, err := os.Stat(resolved); err != nil {
		// Resolvable but target missing — classic broken symlink.
		return BrokenLink{
			LinkPath:    spec.LinkPath,
			Dest:        raw,
			DisplayDest: config.DisplayPath(resolved),
		}, true
	}
	// Resolvable AND target exists but matches no canonical — mis-pointed.
	return BrokenLink{
		LinkPath:    spec.LinkPath,
		Dest:        raw,
		DisplayDest: config.DisplayPath(resolved),
	}, true
}

// ScanSymlinkDir reads dir and classifies each entry as healthy or broken
// via the managed-link semantics used by status/doctor today. Returns the
// healthy count, the broken count, and the broken entries with raw +
// display target populated. Hidden files and subdirectories are skipped
// (the existing per-platform dirs are flat — rules/, agents/, skills/,
// agent/).
//
// dir may not exist; in that case (0, 0, nil) is returned without error —
// "absent dir" is "not present" rather than "broken", matching the
// existing doctor/status behavior. Symlinks to directories (which on
// Windows show up as junctions and on POSIX as plain symlinks) are
// classified using managedLinkBroken so their broken state is reported
// even though they are not files.
func ScanSymlinkDir(dir string) (ok, broken int, brokenLinks []BrokenLink) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0, nil
	}
	for _, e := range entries {
		linkPath := filepath.Join(dir, e.Name())
		raw, isLink, isBroken := managedLinkBroken(linkPath)
		if !isLink {
			continue
		}
		if isBroken {
			resolved := resolveLinkDest(linkPath, raw)
			brokenLinks = append(brokenLinks, BrokenLink{
				LinkPath:    linkPath,
				Dest:        raw,
				DisplayDest: config.DisplayPath(resolved),
			})
			broken++
			continue
		}
		ok++
	}
	return ok, broken, brokenLinks
}

// resolveLinkDest mirrors commands/internal/lifecycle/status.go.
// A POSIX symlink target may be relative to the link's own directory; resolve
// it before any stat or display.
//
// TODO(Phase 5): consolidate this with the lifecycle copy when the audit
// printers move into AuditPrinter implementations. Until consumers migrate
// the helper is kept package-private here to keep diagnostics self-contained.
func resolveLinkDest(linkPath, dest string) string {
	if dest == "" || filepath.IsAbs(dest) {
		return dest
	}
	return filepath.Clean(filepath.Join(filepath.Dir(linkPath), dest))
}

// managedLinkBroken mirrors commands/internal/lifecycle/status.go.
// For a single managed link path, reports whether it is a resolvable managed
// link (POSIX symlink / Windows junction), its resolved target for display,
// and whether that target is missing (the link is broken).
//
// A Windows hard-linked managed *file* has no reparse point and therefore no
// resolvable target — ManagedLinkTarget returns ("", false). Such a file
// cannot dangle (its target inode must exist), so it is reported isLink=false
// and broken=false here.
//
// TODO(Phase 5): consolidate this with the lifecycle copy. Per the proposal
// the per-platform AuditPrinter migration is when the lifecycle helper goes
// away; until then both copies coexist intentionally to keep this PR's
// write-scope confined to internal/platform/.
func managedLinkBroken(linkPath string) (dest string, isLink, broken bool) {
	raw, ok := links.ManagedLinkTarget(linkPath)
	if !ok {
		return "", false, false
	}
	resolved := resolveLinkDest(linkPath, raw)
	if _, err := os.Stat(resolved); err != nil {
		return raw, true, true
	}
	return raw, true, false
}
