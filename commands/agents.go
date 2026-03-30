package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/ui"
	"github.com/spf13/cobra"
)

func NewAgentsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agents",
		Short: "Manage agents in ~/.agents/agents/",
	}
	cmd.AddCommand(newAgentsListCmd())
	cmd.AddCommand(newAgentsNewCmd())
	return cmd
}

func newAgentsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list [project]",
		Short: "List agents",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			scope := "global"
			if len(args) > 0 {
				scope = args[0]
			}
			return listAgents(scope)
		},
	}
}

func listAgents(scope string) error {
	agentsHome := config.AgentsHome()
	agentsDir := filepath.Join(agentsHome, "agents", scope)

	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		ui.Info("No agents found in ~/.agents/agents/" + scope + "/")
		return nil
	}

	ui.Header("Agents (" + scope + ")")
	count := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		agentPath := filepath.Join(agentsDir, e.Name())
		agentMD := filepath.Join(agentPath, "AGENT.md")
		if _, err := os.Stat(agentMD); err == nil {
			desc := readFrontmatterDescription(agentMD)
			if desc != "" {
				ui.Bullet("ok", fmt.Sprintf("%s  %s%s%s", e.Name(), ui.Dim, desc, ui.Reset))
			} else {
				ui.Bullet("ok", e.Name())
			}
		} else {
			ui.Bullet("warn", e.Name()+" (no AGENT.md)")
		}
		count++
	}
	fmt.Fprintf(os.Stdout, "\n  %s%d agent(s) in %s scope%s\n\n", ui.Dim, count, scope, ui.Reset)
	return nil
}

func newAgentsNewCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "new <name> [project]",
		Short: "Create a new agent",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			scope := "global"
			if len(args) > 1 {
				scope = args[1]
			}
			return createAgent(name, scope)
		},
	}
}

func createAgent(name, scope string) error {
	agentsHome := config.AgentsHome()
	agentDir := filepath.Join(agentsHome, "agents", scope, name)

	if err := os.MkdirAll(agentDir, 0755); err != nil {
		return fmt.Errorf("creating agent directory: %w", err)
	}

	agentMD := filepath.Join(agentDir, "AGENT.md")
	if _, err := os.Stat(agentMD); os.IsNotExist(err) {
		content := fmt.Sprintf("---\nname: %s\ndescription: \"\"\n---\n\n# %s\n\nAgent instructions here.\n", name, name)
		if err := os.WriteFile(agentMD, []byte(content), 0644); err != nil {
			return fmt.Errorf("creating AGENT.md: %w", err)
		}
	}

	ui.SuccessBox(fmt.Sprintf("Created agent '%s' in ~/.agents/agents/%s/%s/", name, scope, name),
		"Edit the agent: "+config.DisplayPath(agentMD),
	)
	return nil
}
