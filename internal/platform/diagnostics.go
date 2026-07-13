// Package-level diagnostics types and sister interfaces.
//
// This file introduces the data shapes and reader interfaces that doctor and
// status will iterate over once consumers migrate (Phases 1+). Per the
// proposal at .agents/proposals/platform-driven-diagnostics.md (D1), these are
// sister interfaces — not extensions to the core Platform interface — so a
// platform may implement only the readers that apply to it (e.g. only claude
// and codex expose orphan canonicals). Doctor/status type-assert at the use
// site:
//
//	if r, ok := p.(BrokenLinkReporter); ok {
//	    broken = append(broken, r.BrokenLinks(name, repo, agentsHome)...)
//	}
//
// Adding a new platform from this phase forward becomes a single
// internal/platform/<name>.go change: implement the relevant readers and the
// lifecycle layer surfaces it automatically (proved in Phase 6).

package platform

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/ui"
)

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

// UserConfigReporter is implemented by every platform that participates in the
// user-home diagnostics path. claude/codex/opencode/cursor report real managed
// user-home links; copilot implements it too but reports an empty/clean surface
// because its documented user-config layer is not yet wired by dot-agents (the
// user-scope wiring gap is tracked in PLATFORM_DIRS_DOCS). No platform lacks a
// user-config layer — the distinction is whether dot-agents wires it yet.
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
// audit block shown by `da status --audit`. Resolved in Phase 5 (Q2): the
// printer takes an io.Writer rather than a *ui.Sink — the platform layer
// only needs the ANSI colour constants from internal/ui (plain strings),
// so io.Writer keeps the dependency surface minimal while the lifecycle
// layer supplies os.Stdout at the call site.
type AuditPrinter interface {
	PrintAudit(w io.Writer, project, repoPath, agentsHome string)
}

// Filter returns the subset of all whose ID() matches agentFilter, in the
// original order. An empty agentFilter selects every platform (the
// unfiltered `da status`/`da doctor --verbose` audit). This is the single
// platform-selection primitive the lifecycle audit loop dispatches over,
// replacing the per-platform `agentFilter == "" || agentFilter == "<id>"`
// chain that previously lived in status.go's printAudit.
func Filter(all []Platform, agentFilter string) []Platform {
	if agentFilter == "" {
		return all
	}
	var out []Platform
	for _, p := range all {
		if p.ID() == agentFilter {
			out = append(out, p)
		}
	}
	return out
}

// The audit-rendering primitives below were moved verbatim (byte-for-byte
// output preserved) from commands/internal/lifecycle/status.go in Phase 5.
// They render to an io.Writer using the ANSI colour constants from
// internal/ui and the package-local link classification helpers
// (classifyManagedLink, absolutizeDest) so the per-platform PrintAudit
// implementations no longer reach back into the lifecycle package.

const (
	auditLocalFileIndentedFmt = "      %s○%s %s %s(local file)%s\n"
	auditLinkOKFmt            = "      %s✓%s %s %s→ %s%s\n"
	auditLinkBrokenFmt        = "      %s✗%s %s %s→ %s (broken)%s\n"
)

// displayDest formats a managed link's raw target for audit display:
// absolutized then ~/-collapsed via config.DisplayPath.
func displayDest(linkPath, raw string) string {
	return config.DisplayPath(absolutizeDest(linkPath, raw))
}

// printSymlinkDirAudit reads dir for symlink entries and writes each entry's
// status to w. nameFormat is a printf format applied to the entry name (e.g.
// "%s" or ".opencode/agent/%s"). emptyLabel is shown after the ○ marker when
// no symlinks were found. Returns the number of OK and broken entries.
func printSymlinkDirAudit(w io.Writer, dir, emptyLabel, nameFormat string) (ok, broken int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0
	}
	for _, e := range entries {
		linkPath := filepath.Join(dir, e.Name())
		state, raw := classifyManagedLink(linkPath)
		if state == linkStateNotALink {
			continue
		}
		display := fmt.Sprintf(nameFormat, e.Name())
		if state == linkStateBroken {
			fmt.Fprintf(w, auditLinkBrokenFmt, ui.Red, ui.Reset, display, ui.Dim, displayDest(linkPath, raw), ui.Reset)
			broken++
		} else {
			fmt.Fprintf(w, auditLinkOKFmt, ui.Green, ui.Reset, display, ui.Dim, displayDest(linkPath, raw), ui.Reset)
			ok++
		}
	}
	if ok == 0 && broken == 0 {
		fmt.Fprintf(w, "      %s○%s %s %s(empty)%s\n", ui.Dim, ui.Reset, emptyLabel, ui.Dim, ui.Reset)
	}
	return ok, broken
}

// printSymlinkAudit reads a single symlink and writes its ✓/✗/(local file)/
// (not linked) status with the supplied display label. A present path that is
// not a managed link is a rendered/managed file on disk, reported as
// "(local file)"; "(not linked)" is reserved for a path that is truly absent.
func printSymlinkAudit(w io.Writer, linkPath, label string) {
	if state, raw := classifyManagedLink(linkPath); state != linkStateNotALink {
		if state == linkStateBroken {
			fmt.Fprintf(w, auditLinkBrokenFmt, ui.Red, ui.Reset, label, ui.Dim, displayDest(linkPath, raw), ui.Reset)
		} else {
			fmt.Fprintf(w, auditLinkOKFmt, ui.Green, ui.Reset, label, ui.Dim, displayDest(linkPath, raw), ui.Reset)
		}
		return
	}
	if _, err := os.Lstat(linkPath); err == nil {
		fmt.Fprintf(w, auditLocalFileIndentedFmt, ui.Dim, ui.Reset, label, ui.Dim, ui.Reset)
		return
	}
	fmt.Fprintf(w, "      %s-%s %s %s(not linked)%s\n", ui.Dim, ui.Reset, label, ui.Dim, ui.Reset)
}

// printLinkedStatusLine writes a managed link's ✓/✗ status line for label and
// returns true when the link is healthy. Callers have already established that
// linkPath is a managed link.
func printLinkedStatusLine(w io.Writer, label, linkPath string) bool {
	state, raw := classifyManagedLink(linkPath)
	if state != linkStateBroken {
		fmt.Fprintf(w, auditLinkOKFmt, ui.Green, ui.Reset, label, ui.Dim, displayDest(linkPath, raw), ui.Reset)
		return true
	}
	fmt.Fprintf(w, auditLinkBrokenFmt, ui.Red, ui.Reset, label, ui.Dim, displayDest(linkPath, raw), ui.Reset)
	return false
}
