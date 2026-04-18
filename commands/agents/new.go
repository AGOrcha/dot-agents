package agents

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/ui"
)

// CreateAgent creates a new agent directory under ~/.agents/agents/<scope>/<name>/.
func CreateAgent(name, scope string) error {
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
