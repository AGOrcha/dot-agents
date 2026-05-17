package commands

import (
	"fmt"
	"strings"

	"github.com/NikashPrakash/dot-agents/commands/internal/cmdutil"
	"github.com/NikashPrakash/dot-agents/internal/platform"
	"github.com/spf13/cobra"
)

type mcpDeps struct {
	Flags              canonicalCmdFlags
	maxArgsWithHints   func(n int, hints ...string) cobra.PositionalArgs
	exactArgsWithHints func(n int, hints ...string) cobra.PositionalArgs
}

func mcpCommandDeps() mcpDeps {
	return mcpDeps{
		Flags: canonicalCmdFlags{
			DryRun: Flags.DryRun,
			Yes:    Flags.Yes,
			Force:  Flags.Force,
		},
		maxArgsWithHints:   MaximumNArgsWithHints,
		exactArgsWithHints: ExactArgsWithHints,
	}
}

// NewMCPCmd builds the `da mcp` command tree.
func NewMCPCmd() *cobra.Command {
	deps := mcpCommandDeps()
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Inspect and manage canonical ~/.agents/mcp config files",
		Long: `Commands for MCP server configs stored under ~/.agents/mcp/<scope>/.

Scopes are either global (~/.agents/mcp/global/) or a managed project name
(~/.agents/mcp/<project>/), matching da status.

These files are what add, import, refresh, install, and remove wire into
Cursor, Claude Code, Copilot, and related projections. Prefer editing canonical
paths here, then run refresh or install for the project.`,
		Example: canonicalCmdExampleBlock(
			"  da mcp list",
			"  da mcp list my-app",
			"  da mcp show global mcp.json",
			"  da mcp remove global stale.json",
		),
	}
	cmd.AddCommand(newMCPListCmd(deps))
	cmd.AddCommand(newMCPShowCmd(deps))
	cmd.AddCommand(newMCPRemoveCmd(deps))
	return cmd
}

func newMCPListCmd(deps mcpDeps) *cobra.Command {
	return &cobra.Command{
		Use:   "list [scope]",
		Short: "List canonical MCP config files for a scope",
		Example: canonicalCmdExampleBlock(
			"  da mcp list",
			"  da mcp list billing-api",
		),
		Args: deps.maxArgsWithHints(1, "Optionally pass a project scope (or `global`) to inspect that MCP tree."),
		RunE: func(cmd *cobra.Command, args []string) error {
			scope := "global"
			if len(args) > 0 {
				scope = args[0]
			}
			return runMCPList(scope)
		},
	}
}

func newMCPShowCmd(deps mcpDeps) *cobra.Command {
	return &cobra.Command{
		Use:   "show <scope> <name>",
		Short: "Show metadata for one MCP file under ~/.agents/mcp/",
		Args:  deps.exactArgsWithHints(2, "`scope` is `global` or a managed project name; `name` is the file (e.g. mcp.json) or stem (mcp)."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMCPShow(args[0], args[1])
		},
	}
}

func newMCPRemoveCmd(deps mcpDeps) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <scope> <name>",
		Short: "Remove an MCP file from ~/.agents/mcp/ (canonical storage only)",
		Long: `Deletes the file from managed MCP storage only (not repo links). After removal,
run da refresh or install for the relevant project so platform MCP
links stay consistent.`,
		Args: deps.exactArgsWithHints(2, "`scope` is `global` or a managed project name; `name` matches list/show."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMCPRemove(deps, args[0], args[1])
		},
	}
}

// mcpCanonicalSpec wires mcp.go's runMCP* helpers into
// cmdutil.RunCanonical{List,Show,Remove}.
func mcpCanonicalSpec() cmdutil.CanonicalFileSpec {
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
			sp, err := findMCPSpec(agentsHome, scope, name)
			if err != nil {
				return cmdutil.CanonicalFileEntry{}, err
			}
			return cmdutil.CanonicalFileEntry{Scope: sp.Scope, BaseName: sp.BaseName, SourcePath: sp.SourcePath}, nil
		},
		EnsureScope: platform.EnsureUnderMCPScopeTree,
	}
}

func runMCPList(scope string) error {
	return cmdutil.RunCanonicalList(scope, mcpCanonicalSpec())
}

func runMCPShow(scope, name string) error {
	return cmdutil.RunCanonicalShow(scope, name, mcpCanonicalSpec())
}

func runMCPRemove(deps mcpDeps, scope, name string) error {
	return cmdutil.RunCanonicalRemove(cmdutil.RemoveDeps{
		DryRun: deps.Flags.DryRun, Yes: deps.Flags.Yes, Force: deps.Flags.Force,
	}, scope, name, mcpCanonicalSpec())
}

// findMCPSpec looks up an MCP file by basename or stem. Kept package-
// private because TestFindMCPSpecNotFound calls it directly.
func findMCPSpec(agentsHome, scope, name string) (*platform.MCPFileSpec, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, UsageError("MCP file name is empty", "Pass the file name or stem shown by `da mcp list`.")
	}
	spec, err := platform.ResolveCanonicalMCPFile(agentsHome, scope, name)
	if err != nil {
		return nil, ErrorWithHints(
			fmt.Sprintf("MCP file not found: %s / %s", scope, name),
			"Run `da mcp list "+scope+"` to see available files.",
		)
	}
	return spec, nil
}
