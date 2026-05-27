package mcp

import (
	"fmt"
	"strings"

	"github.com/NikashPrakash/dot-agents/commands/internal/cmdutil"
	"github.com/NikashPrakash/dot-agents/internal/platform"
	"github.com/spf13/cobra"
)

// canonicalSpec is the single source of truth for the `da mcp` resource
// family. It populates both the data-layer fields cmdutil.RunCanonical{
// List,Show,Remove} consume (Kind/DirSegment/List/Resolve/...) and the
// CLI-surface fields cmdutil.NewCanonicalResourceCmd consumes
// (Use/Short/Long/Example and the per-verb SubCmdStrings + Args + Run).
// One struct literal per resource — there is no parallel ResourceCmdSpec
// to keep in sync.
//
// deps is threaded through so the Resolve callback can wrap
// platform.ResolveCanonicalMCPFile errors via findMCPSpec, which prefers
// deps.ErrorWithHints / deps.UsageError when provided (matching the parent
// commands package's user-facing error shape). mcp uses
// MaxArgsWithHints (not MaximumNArgsWithHints like settings/rules), so
// the ListArgs binding happens here, not in cmdutil.
func canonicalSpec(deps Deps) cmdutil.CanonicalFileSpec {
	return cmdutil.CanonicalFileSpec{
		Kind:        "MCP",
		DirSegment:  "mcp",
		SingularRem: "MCP file",
		EmptyHint: func(scope string) string {
			return "No MCP config files (.json/.yaml/.yml/.toml) under ~/.agents/mcp/" + scope + "/"
		},
		MissingDirHint: func(scope string) string {
			return "No ~/.agents/mcp/" + scope + "/ directory yet (no canonical MCP files for this scope)."
		},
		List: func(agentsHome, scope string) ([]cmdutil.CanonicalFileEntry, error) {
			specs, err := platform.ListCanonicalMCPFiles(agentsHome, scope)
			if err != nil {
				return nil, err
			}
			out := make([]cmdutil.CanonicalFileEntry, len(specs))
			for i, sp := range specs {
				out[i] = cmdutil.CanonicalFileEntry{Scope: sp.Scope, BaseName: sp.BaseName, SourcePath: sp.SourcePath}
			}
			return out, nil
		},
		Resolve: func(agentsHome, scope, name string) (cmdutil.CanonicalFileEntry, error) {
			sp, err := findMCPSpec(deps, agentsHome, scope, name)
			if err != nil {
				return cmdutil.CanonicalFileEntry{}, err
			}
			return cmdutil.CanonicalFileEntry{Scope: sp.Scope, BaseName: sp.BaseName, SourcePath: sp.SourcePath}, nil
		},
		EnsureScope: platform.EnsureUnderMCPScopeTree,

		Use:   "mcp",
		Short: "Inspect and manage canonical ~/.agents/mcp config files",
		Long: `Commands for MCP server configs stored under ~/.agents/mcp/<scope>/.

Scopes are either global (~/.agents/mcp/global/) or a managed project name
(~/.agents/mcp/<project>/), matching da status.

These files are what add, import, refresh, install, and remove wire into
Cursor, Claude Code, Copilot, and related projections. Prefer editing canonical
paths here, then run refresh or install for the project.`,
		Example: cmdutil.CanonicalCmdExampleBlock(
			"  da mcp list",
			"  da mcp list my-app",
			"  da mcp show global mcp.json",
			"  da mcp remove global stale.json",
		),
		ListSub: cmdutil.SubCmdStrings{
			Use:   "list [scope]",
			Short: "List canonical MCP config files for a scope",
			Example: cmdutil.CanonicalCmdExampleBlock(
				"  da mcp list",
				"  da mcp list billing-api",
			),
		},
		ListArgs: maxArgs(deps, 1, "Optionally pass a project scope (or `global`) to inspect that MCP tree."),
		ListRun:  func(scope string) error { return RunList(scope) },
		ShowSub: cmdutil.SubCmdStrings{
			Use:   "show <scope> <name>",
			Short: "Show metadata for one MCP file under ~/.agents/mcp/",
		},
		ShowArgs: exactArgs(deps, 2, "`scope` is `global` or a managed project name; `name` is the file (e.g. mcp.json) or stem (mcp)."),
		ShowRun:  func(scope, name string) error { return RunShow(deps, scope, name) },
		RemoveSub: cmdutil.SubCmdStrings{
			Use:   "remove <scope> <name>",
			Short: "Remove an MCP file from ~/.agents/mcp/ (canonical storage only)",
			Long: `Deletes the file from managed MCP storage only (not repo links). After removal,
run da refresh or install for the relevant project so platform MCP
links stay consistent.`,
		},
		RemoveArgs: exactArgs(deps, 2, "`scope` is `global` or a managed project name; `name` matches list/show."),
		RemoveRun:  func(scope, name string) error { return RunRemove(deps, scope, name) },
	}
}

// maxArgs / exactArgs guard against the zero-value Deps used by the
// data-layer RunList/RunShow paths. The CLI wiring in NewCmd always
// supplies real helpers via Deps; the data path only needs the data-
// layer spec fields and never invokes Args, so nil-returning fallbacks
// are safe.
func maxArgs(deps Deps, n int, hints ...string) cobra.PositionalArgs {
	if deps.MaxArgsWithHints == nil {
		return nil
	}
	return deps.MaxArgsWithHints(n, hints...)
}

func exactArgs(deps Deps, n int, hints ...string) cobra.PositionalArgs {
	if deps.ExactArgsWithHints == nil {
		return nil
	}
	return deps.ExactArgsWithHints(n, hints...)
}

// findMCPSpec looks up an MCP file by basename or stem. Kept package-
// private because the focused tests in mcp_test.go / seams_test.go call
// it directly and the parent commands shim has no need for it.
//
// Errors are produced via deps.UsageError / deps.ErrorWithHints when
// supplied, matching the user-facing shape commands.UsageError /
// commands.ErrorWithHints emit. When deps is the zero value (e.g.
// RunList paths that never invoke Resolve) the helper falls back to
// fmt.Errorf so it remains usable without wiring.
func findMCPSpec(deps Deps, agentsHome, scope, name string) (*platform.MCPFileSpec, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, usageErr(deps,
			"MCP file name is empty",
			"Pass the file name or stem shown by `da mcp list`.",
		)
	}
	spec, err := platform.ResolveCanonicalMCPFile(agentsHome, scope, name)
	if err != nil {
		return nil, hintErr(deps,
			fmt.Sprintf("MCP file not found: %s / %s", scope, name),
			"Run `da mcp list "+scope+"` to see available files.",
		)
	}
	return spec, nil
}

// usageErr / hintErr centralise the deps-nil fallback so each call site
// stays a one-liner.
func usageErr(deps Deps, message string, hints ...string) error {
	if deps.UsageError != nil {
		return deps.UsageError(message, hints...)
	}
	return formatFallback(message, hints)
}

func hintErr(deps Deps, message string, hints ...string) error {
	if deps.ErrorWithHints != nil {
		return deps.ErrorWithHints(message, hints...)
	}
	return formatFallback(message, hints)
}

func formatFallback(message string, hints []string) error {
	if len(hints) == 0 {
		return fmt.Errorf("%s", message)
	}
	return fmt.Errorf("%s: %s", message, hints[0])
}
