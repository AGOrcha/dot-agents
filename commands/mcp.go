package commands

import (
	"fmt"
	"os"
	"strings"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/platform"
	"github.com/NikashPrakash/dot-agents/internal/ui"
	"github.com/spf13/cobra"
)

type mcpDeps struct {
	Flags              rulesGlobalFlags
	maxArgsWithHints   func(n int, hints ...string) cobra.PositionalArgs
	exactArgsWithHints func(n int, hints ...string) cobra.PositionalArgs
}

func mcpCommandDeps() mcpDeps {
	return mcpDeps{
		Flags: rulesGlobalFlags{
			DryRun: Flags.DryRun,
			Yes:    Flags.Yes,
			Force:  Flags.Force,
		},
		maxArgsWithHints:   MaximumNArgsWithHints,
		exactArgsWithHints: ExactArgsWithHints,
	}
}

// NewMCPCmd builds the `dot-agents mcp` command tree.
func NewMCPCmd() *cobra.Command {
	deps := mcpCommandDeps()
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Inspect and manage canonical ~/.agents/mcp config files",
		Long: `Commands for MCP server configs stored under ~/.agents/mcp/<scope>/.

Scopes are either global (~/.agents/mcp/global/) or a managed project name
(~/.agents/mcp/<project>/), matching dot-agents status.

These files are what add, import, refresh, install, and remove wire into
Cursor, Claude Code, Copilot, and related projections. Prefer editing canonical
paths here, then run refresh or install for the project.`,
		Example: rulesExampleBlock(
			"  dot-agents mcp list",
			"  dot-agents mcp list my-app",
			"  dot-agents mcp show global mcp.json",
			"  dot-agents mcp remove global stale.json",
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
		Example: rulesExampleBlock(
			"  dot-agents mcp list",
			"  dot-agents mcp list billing-api",
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
run dot-agents refresh or install for the relevant project so platform MCP
links stay consistent.`,
		Args: deps.exactArgsWithHints(2, "`scope` is `global` or a managed project name; `name` matches list/show."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMCPRemove(deps, args[0], args[1])
		},
	}
}

func runMCPList(scope string) error {
	agentsHome := config.AgentsHome()
	specs, err := platform.ListCanonicalMCPFiles(agentsHome, scope)
	if err != nil {
		if os.IsNotExist(err) {
			ui.Info("No ~/.agents/mcp/" + scope + "/ directory yet (no canonical MCP files for this scope).")
			return nil
		}
		return err
	}
	if len(specs) == 0 {
		ui.Info("No MCP config files (.json/.yaml/.yml/.toml) under ~/.agents/mcp/" + scope + "/")
		return nil
	}
	ui.Header("MCP (" + scope + ")")
	for _, spec := range specs {
		fmt.Fprintf(os.Stdout, "\n  %s%s%s\n", ui.Cyan, spec.BaseName, ui.Reset)
		fmt.Fprintf(os.Stdout, "    %spath:%s %s\n", ui.Dim, ui.Reset, config.DisplayPath(spec.SourcePath))
	}
	fmt.Fprintln(os.Stdout)
	return nil
}

func runMCPShow(scope, name string) error {
	agentsHome := config.AgentsHome()
	spec, err := findMCPSpec(agentsHome, scope, name)
	if err != nil {
		return err
	}
	info, statErr := os.Stat(spec.SourcePath)
	ui.Header("MCP " + spec.BaseName + " (" + scope + ")")
	fmt.Fprintf(os.Stdout, "  %spath:%s %s\n", ui.Dim, ui.Reset, config.DisplayPath(spec.SourcePath))
	if statErr == nil {
		fmt.Fprintf(os.Stdout, "  %ssize:%s %d bytes\n", ui.Dim, ui.Reset, info.Size())
	}
	fmt.Fprintln(os.Stdout)
	return nil
}

func runMCPRemove(deps mcpDeps, scope, name string) error {
	agentsHome := config.AgentsHome()
	spec, err := findMCPSpec(agentsHome, scope, name)
	if err != nil {
		return err
	}
	if err := platform.EnsureUnderMCPScopeTree(agentsHome, scope, spec.SourcePath); err != nil {
		return err
	}

	ui.Header("dot-agents mcp remove")
	fmt.Fprintf(os.Stdout, "Remove MCP file %q from scope %s\n", name, ui.BoldText(scope))
	fmt.Fprintf(os.Stdout, "  %s\n", config.DisplayPath(spec.SourcePath))

	if deps.Flags.DryRun {
		fmt.Fprintln(os.Stdout, "\nDRY RUN - no changes made")
		return nil
	}
	if !deps.Flags.Yes && !deps.Flags.Force {
		if !ui.Confirm("Remove this file from ~/.agents/mcp/?", false) {
			ui.Info("Cancelled.")
			return nil
		}
	}

	if err := os.Remove(spec.SourcePath); err != nil {
		return fmt.Errorf("removing MCP file: %w", err)
	}
	ui.Success(fmt.Sprintf("Removed MCP file %q from scope %s.", spec.BaseName, scope))
	return nil
}

func findMCPSpec(agentsHome, scope, name string) (*platform.MCPFileSpec, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, UsageError("MCP file name is empty", "Pass the file name or stem shown by `dot-agents mcp list`.")
	}
	spec, err := platform.ResolveCanonicalMCPFile(agentsHome, scope, name)
	if err != nil {
		return nil, ErrorWithHints(
			fmt.Sprintf("MCP file not found: %s / %s", scope, name),
			"Run `dot-agents mcp list "+scope+"` to see available files.",
		)
	}
	return spec, nil
}
