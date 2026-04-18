package agents

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// NewAgentsCmd builds the `dot-agents agents` command tree from injected dependencies.
func NewAgentsCmd(deps Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agents",
		Short: "Manage agents in ~/.agents/agents/",
		Long: `Lists and creates reusable agent definitions inside the canonical
~/.agents/agents tree. These definitions can then be distributed into projects
through refresh or install flows.`,
		Example: exampleBlock(
			"  dot-agents agents list",
			"  dot-agents agents new reviewer",
			"  dot-agents agents promote reviewer",
			"  dot-agents agents import reviewer",
			"  dot-agents agents remove reviewer",
			"  dot-agents agents new repo-owner billing-api",
		),
	}
	cmd.AddCommand(newAgentsListCmd(deps))
	cmd.AddCommand(newAgentsNewCmd(deps))
	cmd.AddCommand(newAgentsPromoteCmd(deps))
	cmd.AddCommand(newAgentsImportCmd(deps))
	cmd.AddCommand(newAgentsRemoveCmd(deps))
	return cmd
}

func newAgentsListCmd(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "list [project]",
		Short: "List agents",
		Example: exampleBlock(
			"  dot-agents agents list",
			"  dot-agents agents list billing-api",
		),
		Args: deps.MaximumNArgsWithHints(1, "Optionally pass a project scope to list project-local agents."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return listAgents(scopeFromArgs(args))
		},
	}
}

func newAgentsNewCmd(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "new <name> [project]",
		Short: "Create a new agent",
		Example: exampleBlock(
			"  dot-agents agents new reviewer",
			"  dot-agents agents new doc-writer billing-api",
		),
		Args: deps.RangeArgsWithHints(1, 2, "Pass an agent name and optionally a project scope."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return CreateAgent(args[0], scopeFromArgs(args[1:]))
		},
	}
}

func newAgentsPromoteCmd(deps Deps) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "promote <name>",
		Short: "Promote a repo-local agent to shared storage",
		Long: `Promotes an agent from .agents/agents/<name>/ in the current repo to
~/.agents/agents/<project>/<name>/, registers it in .agentsrc.json, and
ensures repo symlinks under .claude/agents/.`,
		Example: exampleBlock(
			"  dot-agents agents promote reviewer",
			"  dot-agents agents promote reviewer --force",
		),
		Args: deps.ExactArgsWithHints(1, "Run this from the project repository that owns `.agents/agents/<name>/`."),
		RunE: func(cmd *cobra.Command, args []string) error {
			projectPath, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("resolving project path: %w", err)
			}
			return PromoteAgentIn(args[0], projectPath, force)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Replace an existing real directory at the canonical path (destructive)")
	return cmd
}

func newAgentsRemoveCmd(deps Deps) *cobra.Command {
	var purge bool
	cmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "Unlink agent symlinks from this repo and drop the manifest entry",
		Long: `Removes managed symlinks under .agents/agents/<name>/ and .claude/agents/<name>/
and removes the name from .agentsrc.json agents[]. The canonical directory under
~/.agents/agents/<project>/<name>/ is left intact unless --purge is set.`,
		Example: exampleBlock(
			"  dot-agents agents remove reviewer",
			"  dot-agents agents remove reviewer --purge",
		),
		Args: deps.ExactArgsWithHints(1, "Pass the agent name as registered in .agentsrc.json."),
		RunE: func(cmd *cobra.Command, args []string) error {
			projectPath, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("resolving project path: %w", err)
			}
			return RemoveAgentIn(deps, args[0], projectPath, purge)
		},
	}
	cmd.Flags().BoolVar(&purge, "purge", false, "Also delete ~/.agents/agents/<project>/<name>/ (prompts unless --yes)")
	return cmd
}

func newAgentsImportCmd(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "import <name>",
		Short: "Link a canonical agent from ~/.agents/agents/ into this repo",
		Long: `Imports an agent that already exists under ~/.agents/agents/<project>/<name>/
into the current repository: creates managed symlinks under .agents/agents/ and
.claude/agents/, and registers the name in .agentsrc.json when absent.

This is the reverse of promote: the canonical directory remains the source of truth.`,
		Example: exampleBlock(
			"  dot-agents agents import reviewer",
		),
		Args: deps.ExactArgsWithHints(1, "Pass the agent name as it appears under ~/.agents/agents/<project>/."),
		RunE: func(cmd *cobra.Command, args []string) error {
			projectPath, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("resolving project path: %w", err)
			}
			return ImportAgentIn(args[0], projectPath)
		},
	}
}
