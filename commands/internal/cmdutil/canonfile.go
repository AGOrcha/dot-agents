// Package cmdutil holds shared helpers for the commands/* CLI tree.
//
// canonfile.go specifically powers the settings/mcp/rules subcommand
// families. The three families differ only in noun, dir segment, and
// a small number of message strings; the list/show/remove flows
// underneath are identical. Per
// .agents/workflow/specs/production-code-helper-extraction/design.md,
// extracting these into RunCanonical{List,Show,Remove} drains the
// three-way duplication SonarCloud flags at settings.go:106 ↔
// mcp.go:106 ↔ rules.go:122 (and the parallel show/remove blocks).
package cmdutil

import (
	"fmt"
	"os"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/ui"
	"github.com/spf13/cobra"
)

// CanonicalFileEntry is the projection of platform.{Settings,MCP,Rule}FileSpec
// that the List/Show/Remove helpers operate on. Per-subcommand specs
// convert their typed platform.* slices into []CanonicalFileEntry.
type CanonicalFileEntry struct {
	Scope      string
	BaseName   string
	SourcePath string
}

// CanonicalFileSpec parameterizes both the data-layer
// RunCanonicalList/Show/Remove helpers and the cobra-tree assembly in
// NewCanonicalResourceCmd. Settings/MCP/Rules each populate one of these
// — there is exactly one struct literal per resource family.
//
// The fields split into three groups:
//
//   - Resource identity (Kind, DirSegment, SingularRem)
//   - User-facing message hooks (EmptyHint, MissingDirHint)
//   - Data-layer callbacks (List, Resolve, EnsureScope)
//   - CLI-surface strings (Use, Short, Long, Example and the per-verb
//     SubCmdStrings + cobra.PositionalArgs)
//
// Leaves wire their per-verb runners through the package-level
// RunList/RunShow/RunRemove functions so this spec stays a pure data
// description with no behavior.
type CanonicalFileSpec struct {
	// Kind is the capitalized header label ("Settings" | "MCP" | "Rules").
	Kind string
	// DirSegment is the directory under ~/.agents/<DirSegment>/.
	// Used in user-facing path text, the remove header, and the
	// confirmation prompt.
	DirSegment string
	// SingularRem is the singular noun used in remove output strings
	// ("settings file" | "MCP file" | "rule file").
	SingularRem string
	// EmptyHint produces the message shown when no files exist for
	// the scope. Per-subcommand because the message lists the kind's
	// own valid file extensions or noun.
	EmptyHint func(scope string) string
	// MissingDirHint produces the message shown when the
	// agentsHome/<DirSegment>/<scope>/ directory itself doesn't exist.
	// Defaults if nil to "No ~/.agents/<DirSegment>/<scope>/ directory yet ...".
	MissingDirHint func(scope string) string

	// List enumerates entries for a scope. Returning an os.IsNotExist
	// error indicates the scope directory is missing — the helper
	// turns that into the MissingDirHint informational message.
	List func(agentsHome, scope string) ([]CanonicalFileEntry, error)
	// Resolve finds one entry by basename or stem; the returned error
	// is propagated as-is, so callers can wrap with errorWithHints.
	Resolve func(agentsHome, scope, name string) (CanonicalFileEntry, error)
	// EnsureScope verifies target is under <agentsHome>/<DirSegment>/<scope>/.
	EnsureScope func(agentsHome, scope, target string) error

	// Use / Short / Long / Example populate the parent cobra.Command
	// (the `da <kind>` node). Example is a pre-joined string — leaves
	// build multi-line examples with CanonicalCmdExampleBlock(...) or
	// a plain string literal.
	Use     string
	Short   string
	Long    string
	Example string

	// List / Show / Remove subcommand strings + Args validators.
	// Args is the pre-bound cobra.PositionalArgs validator from the
	// leaf's Deps (mcp uses MaxArgsWithHints, settings/rules use
	// MaximumNArgsWithHints, so binding stays at the leaf). Run is the
	// leaf-specific runner that receives unpacked scope/name args from
	// the canonical RunE wrapper.
	ListSub    SubCmdStrings
	ListArgs   cobra.PositionalArgs
	ListRun    func(scope string) error
	ShowSub    SubCmdStrings
	ShowArgs   cobra.PositionalArgs
	ShowRun    func(scope, name string) error
	RemoveSub  SubCmdStrings
	RemoveArgs cobra.PositionalArgs
	RemoveRun  func(scope, name string) error
}

// SubCmdStrings carries the per-leaf strings for one canonical
// subcommand. Use/Short are required; Long/Example are optional.
// Mirrors what the old commands/internal/canonical package exposed so
// the leaf spec literals retain a familiar shape.
type SubCmdStrings struct {
	Use     string
	Short   string
	Long    string
	Example string
}

// RemoveDeps carries the user-facing flags consumed by RunCanonicalRemove.
type RemoveDeps struct {
	DryRun bool
	Yes    bool
	Force  bool
}

// RunCanonicalList prints the canonical entries for a scope. Emits an
// info message for missing scope directories (via spec.MissingDirHint)
// and empty scope directories (via spec.EmptyHint).
func RunCanonicalList(scope string, spec CanonicalFileSpec) error {
	agentsHome := config.AgentsHome()
	entries, err := spec.List(agentsHome, scope)
	if err != nil {
		if os.IsNotExist(err) {
			ui.Info(missingDirMessage(scope, spec))
			return nil
		}
		return err
	}
	if len(entries) == 0 {
		ui.Info(spec.EmptyHint(scope))
		return nil
	}
	ui.Header(fmt.Sprintf("%s (%s)", spec.Kind, scope))
	for _, e := range entries {
		fmt.Fprintf(os.Stdout, "\n  %s%s%s\n", ui.Cyan, e.BaseName, ui.Reset)
		fmt.Fprintf(os.Stdout, "    %spath:%s %s\n", ui.Dim, ui.Reset, config.DisplayPath(e.SourcePath))
	}
	fmt.Fprintln(os.Stdout)
	return nil
}

// RunCanonicalShow prints metadata for one entry. Each `extra` callback
// receives the resolved source path and may print additional lines
// (e.g. rules append a frontmatter description).
func RunCanonicalShow(scope, name string, spec CanonicalFileSpec, extras ...func(srcPath string)) error {
	agentsHome := config.AgentsHome()
	entry, err := spec.Resolve(agentsHome, scope, name)
	if err != nil {
		return err
	}
	info, statErr := os.Stat(entry.SourcePath)
	ui.Header(fmt.Sprintf("%s %s (%s)", spec.Kind, entry.BaseName, scope))
	fmt.Fprintf(os.Stdout, "  %spath:%s %s\n", ui.Dim, ui.Reset, config.DisplayPath(entry.SourcePath))
	if statErr == nil {
		fmt.Fprintf(os.Stdout, "  %ssize:%s %d bytes\n", ui.Dim, ui.Reset, info.Size())
	}
	for _, fn := range extras {
		fn(entry.SourcePath)
	}
	fmt.Fprintln(os.Stdout)
	return nil
}

// RunCanonicalRemove removes one entry, with dry-run + confirm gates
// matching the original per-subcommand handlers.
func RunCanonicalRemove(deps RemoveDeps, scope, name string, spec CanonicalFileSpec) error {
	agentsHome := config.AgentsHome()
	entry, err := spec.Resolve(agentsHome, scope, name)
	if err != nil {
		return err
	}
	if err := spec.EnsureScope(agentsHome, scope, entry.SourcePath); err != nil {
		return err
	}

	ui.Header(fmt.Sprintf("da %s remove", spec.DirSegment))
	fmt.Fprintf(os.Stdout, "Remove %s %q from scope %s\n", spec.SingularRem, name, ui.BoldText(scope))
	fmt.Fprintf(os.Stdout, "  %s\n", config.DisplayPath(entry.SourcePath))

	if deps.DryRun {
		fmt.Fprintln(os.Stdout, "\nDRY RUN - no changes made")
		return nil
	}
	if !deps.Yes && !deps.Force {
		if !ui.Confirm(fmt.Sprintf("Remove this file from ~/.agents/%s/?", spec.DirSegment), false) {
			ui.Info("Cancelled.")
			return nil
		}
	}

	if err := os.Remove(entry.SourcePath); err != nil {
		return fmt.Errorf("removing %s: %w", spec.SingularRem, err)
	}
	ui.Success(fmt.Sprintf("Removed %s %q from scope %s.", spec.SingularRem, entry.BaseName, scope))
	return nil
}

func missingDirMessage(scope string, spec CanonicalFileSpec) string {
	if spec.MissingDirHint != nil {
		return spec.MissingDirHint(scope)
	}
	return fmt.Sprintf("No ~/.agents/%s/%s/ directory yet (no canonical %s files for this scope).",
		spec.DirSegment, scope, spec.DirSegment)
}
