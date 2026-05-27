// Package-level diagnostics types and sister interfaces.
//
// This file introduces the data shapes and reader interfaces that doctor and
// status will iterate over once consumers migrate (Phases 1+). Per the
// proposal at .agents/proposals/platform-driven-diagnostics.md (D1), these are
// sister interfaces — not extensions to the core Platform interface — so a
// platform may implement only the readers that apply to it (e.g. cursor and
// copilot have no user-config layer; only claude and codex expose orphan
// canonicals). Doctor/status type-assert at the use site:
//
//	if r, ok := p.(BrokenLinkReporter); ok {
//	    broken = append(broken, r.BrokenLinks(name, repo, agentsHome)...)
//	}
//
// Adding a new platform from this phase forward becomes a single
// internal/platform/<name>.go change: implement the relevant readers and the
// lifecycle layer surfaces it automatically (proved in Phase 6).

package platform

import "io"

// BrokenLink describes a single managed link that exists but does not
// resolve to the expected canonical source. PlatformID is intentionally
// carried on the value so JSON reports can self-describe per-entry (Q1 in
// the proposal — kept).
type BrokenLink struct {
	// PlatformID identifies which platform owns this link (e.g. "cursor").
	PlatformID string
	// LinkPath is the display-friendly path to the broken link — typically
	// repo-relative for project links or home-relative for user-config links.
	LinkPath string
	// Dest is the raw target string returned by the OS (os.Readlink output
	// for symlinks/junctions; empty for hard links whose canonical source
	// could not be matched).
	Dest string
	// DisplayDest is Dest after path resolution and ~/-prefix formatting,
	// suitable for direct rendering by doctor/status.
	DisplayDest string
}

// PlatformBadge is the per-platform summary consumed by both the text
// (badge row) and JSON (statusJSONPlatform) status output. Per D5 of the
// proposal, this is the single source of truth that replaces the parallel
// `*TextBadge` and `collectProjectPlatforms` paths.
type PlatformBadge struct {
	// Name is the human-readable platform name shown in the badge row
	// (e.g. "Cursor", "Claude Code").
	Name string
	// Present is true when this platform has any managed state for the
	// inspected scope (project or user-home).
	Present bool
	// Broken is true when any of the platform's managed links are broken.
	Broken bool
}

// OrphanCanonical describes a canonical entry under ~/.agents/ that has no
// corresponding managed link in the project (orphan from the project's
// perspective). DisplayNote is empty for plain orphans; a mis-pointed link
// surfaces as a non-empty note formatted as
// "  (mis-pointed: <target>)".
type OrphanCanonical struct {
	// Name is the entry basename as it appears under ~/.agents/<bucket>/.
	Name string
	// DisplayNote is either "" (plain orphan) or a pre-formatted suffix
	// describing the mis-pointed target. Callers concatenate it directly
	// when rendering.
	DisplayNote string
}

// SingleFileLinkSpec describes one managed file link a platform owns: a
// fixed link path and the ordered list of canonical source files any of
// which would satisfy the link (the ordered list supports the .mdc → .md
// fallback already used by cursor rules).
type SingleFileLinkSpec struct {
	// LinkPath is the absolute path to the managed file link.
	LinkPath string
	// CanonicalPaths is the ordered list of acceptable sources. The first
	// hard-linked match wins; if none match and the link exists, the link
	// is reported as broken.
	CanonicalPaths []string
}

// BrokenLinkReporter is implemented by platforms that can enumerate their
// own broken project-scope links. Consumer: doctor.collectBrokenLinks.
type BrokenLinkReporter interface {
	BrokenLinks(project, repoPath, agentsHome string) []BrokenLink
}

// LinkCounter is implemented by platforms that can count their healthy and
// broken managed links for a project. Consumer: doctor.countProjectLinks
// and the badge-math feeding status.
type LinkCounter interface {
	CountLinks(project, repoPath, agentsHome string) (ok, broken int)
}

// StatusBadger is implemented by platforms that can produce a one-line
// status badge for the project. Consumer: status.collectProjectTextBadges
// and status.collectProjectPlatforms (text + JSON share the same value per
// D5).
type StatusBadger interface {
	Badge(project, repoPath, agentsHome string) PlatformBadge
}

// UserConfigReporter is implemented by platforms that maintain a user-home
// configuration layer (e.g. claude/codex/opencode). cursor and copilot do
// not implement this.
type UserConfigReporter interface {
	// UserBrokenLinks returns broken managed links under the user's home
	// directory for this platform. Consumer: doctor.collectBrokenUserLinks.
	UserBrokenLinks(home string) []BrokenLink
	// UserBadge returns the user-config badge for the platform. Consumer:
	// status.collectUserConfigPlatforms.
	UserBadge(home string) PlatformBadge
}

// OrphanCanonicalReporter is implemented by platforms that maintain a
// canonical store under ~/.agents/<bucket>/ that can be inspected for
// orphans (entries with no matching project link). Consumer:
// doctor.collectOrphanCanonicals.
type OrphanCanonicalReporter interface {
	OrphanCanonicals(project, projectPath, agentsHome, bucket string) []OrphanCanonical
}

// AuditPrinter is implemented by platforms that render the per-platform
// audit block shown by `da status --audit`. Q2 of the proposal asks
// whether this should take a *ui.Sink instead of io.Writer — that
// decision is deferred to Phase 5 when the helpers actually move. Until
// then io.Writer keeps the interface dependency-free at the platform
// layer.
type AuditPrinter interface {
	PrintAudit(w io.Writer, project, repoPath, agentsHome string)
}
