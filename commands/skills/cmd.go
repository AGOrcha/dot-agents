package skills

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// NewSkillsCmd builds the `da skills` command tree from injected dependencies.
// Mirrors agents.NewAgentsCmd: helpers come from Deps so the subpackage stays
// independent of the parent commands/ package.
func NewSkillsCmd(deps Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skills",
		Short: "Manage skills in ~/.agents/skills/",
		Long: `Lists, creates, and promotes reusable skills stored in the canonical
~/.agents/skills tree. Skills created here can be linked into projects and consumed
by supported AI platforms through refresh or install.`,
		Example: exampleBlock(
			"  da skills list",
			"  da skills new agent-start",
			"  da skills promote session-start",
		),
	}
	cmd.AddCommand(newSkillsListCmd(deps))
	cmd.AddCommand(newSkillsNewCmd(deps))
	cmd.AddCommand(newSkillsPromoteCmd(deps))
	return cmd
}

func newSkillsListCmd(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "list [project]",
		Short: "List skills",
		Example: exampleBlock(
			"  da skills list",
			"  da skills list billing-api",
		),
		Args: deps.MaximumNArgsWithHints(1, "Optionally pass a project scope to list project-local skills."),
		RunE: func(cmd *cobra.Command, args []string) error {
			scope := "global"
			if len(args) > 0 {
				scope = args[0]
			}
			return List(scope)
		},
	}
}

func newSkillsNewCmd(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "new <name> [project]",
		Short: "Create a new skill",
		Example: exampleBlock(
			"  da skills new self-review",
			"  da skills new repo-bootstrap billing-api",
		),
		Args: deps.RangeArgsWithHints(1, 2, "Pass a skill name and optionally a project scope."),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			scope := "global"
			if len(args) > 1 {
				scope = args[1]
			}
			return CreateSkill(name, scope)
		},
	}
}

func newSkillsPromoteCmd(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "promote <name>",
		Short: "Promote a repo-local skill to shared storage",
		Long: `Promotes a skill from .agents/skills/<name>/ in the current repo to
~/.agents/skills/<project>/<name>/, registers it in .agentsrc.json, and
refreshes shared skill mirrors for all platforms.`,
		Example: exampleBlock(
			"  da skills promote session-start",
			"  da status --audit",
		),
		Args: deps.ExactArgsWithHints(1, "Run this from the project repository that owns `.agents/skills/<name>/`."),
		RunE: func(cmd *cobra.Command, args []string) error {
			projectPath, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("resolving project path: %w", err)
			}
			return PromoteSkillIn(args[0], projectPath)
		},
	}
}
