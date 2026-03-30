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
			return listAgents(scopeFromArgs(args))
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
			return createAgent(args[0], scopeFromArgs(args[1:]))
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
	if err := writeAgentMDIfAbsent(agentMD, name); err != nil {
		return err
	}

	ui.SuccessBox(
		fmt.Sprintf("Created agent '%s' in ~/.agents/agents/%s/%s/", name, scope, name),
		createAgentNextSteps(agentMD, name, scope)...,
	)
	return nil
}

func scopeFromArgs(args []string) string {
	if len(args) == 0 {
		return "global"
	}
	return args[0]
}

func createAgentNextSteps(agentMD, name, scope string) []string {
	nextSteps := []string{"Edit the agent: " + config.DisplayPath(agentMD)}
	return appendAgentsRCStep(nextSteps, name, scope)
}

// writeAgentMDIfAbsent creates AGENT.md with default content when it does not yet exist.
func writeAgentMDIfAbsent(agentMD, name string) error {
	if _, err := os.Stat(agentMD); !os.IsNotExist(err) {
		return nil
	}
	content := fmt.Sprintf("---\nname: %s\ndescription: \"\"\n---\n\n# %s\n\nAgent instructions here.\n", name, name)
	if err := os.WriteFile(agentMD, []byte(content), 0644); err != nil {
		return fmt.Errorf("creating AGENT.md: %w", err)
	}
	return nil
}

// appendAgentsRCStep auto-updates .agentsrc.json for project-scoped agents and
// returns nextSteps with an optional confirmation message appended.
func appendAgentsRCStep(nextSteps []string, name, scope string) []string {
	if scope == "global" {
		return nextSteps
	}
	cfg, err := config.Load()
	if err != nil {
		return nextSteps
	}
	projPath := cfg.GetProjectPath(scope)
	if projPath == "" {
		return nextSteps
	}
	rc, err := config.LoadAgentsRC(projPath)
	if err != nil {
		return nextSteps
	}
	rc.Agents = config.AppendUnique(rc.Agents, name)
	if err := rc.Save(projPath); err == nil {
		nextSteps = append(nextSteps, "Updated .agentsrc.json with agent '"+name+"'")
	}
	return nextSteps
}
