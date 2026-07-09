package agents

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/AGOrcha/dot-agents/internal/config"
	scaffoldtemplates "github.com/AGOrcha/dot-agents/internal/scaffold/templates"
	"github.com/AGOrcha/dot-agents/internal/ui"
)

// CreateAgent creates a new agent directory under ~/.agents/agents/<scope>/<name>/.
func CreateAgent(name, scope string) error {
	agentsHome := config.AgentsHome()
	agentDir := filepath.Join(agentsHome, "agents", scope, name)

	if err := os.MkdirAll(agentDir, 0755); err != nil {
		return fmt.Errorf("creating agent directory: %w", err)
	}

	agentMD := filepath.Join(agentDir, agentManifestName)
	if err := writeAgentMDIfAbsent(agentMD, name); err != nil {
		return err
	}

	nextSteps, rcErr := createAgentNextSteps(agentMD, name, scope)
	if rcErr != nil {
		ui.WarnBox(
			fmt.Sprintf("Created agent '%s' in ~/.agents/agents/%s/%s/, but registration did not fully complete", name, scope, name),
			append(nextSteps, "did not update .agentsrc.json: "+rcErr.Error())...,
		)
		return nil
	}

	ui.SuccessBox(
		fmt.Sprintf("Created agent '%s' in ~/.agents/agents/%s/%s/", name, scope, name),
		nextSteps...,
	)
	return nil
}

func scopeFromArgs(args []string) string {
	if len(args) == 0 {
		return "global"
	}
	return args[0]
}

func createAgentNextSteps(agentMD, name, scope string) ([]string, error) {
	nextSteps := []string{"Edit the agent: " + config.DisplayPath(agentMD)}
	return appendAgentsRCStep(nextSteps, name, scope)
}

// writeAgentMDIfAbsent creates AGENT.md with default content when it does not yet exist.
func writeAgentMDIfAbsent(agentMD, name string) error {
	if _, err := os.Stat(agentMD); !os.IsNotExist(err) {
		return nil
	}
	content, err := scaffoldtemplates.RenderAgentManifest(name)
	if err != nil {
		return fmt.Errorf("rendering %s: %w", agentManifestName, err)
	}
	if err := os.WriteFile(agentMD, []byte(content), 0644); err != nil {
		return fmt.Errorf("creating %s: %w", agentManifestName, err)
	}
	return nil
}

// appendAgentsRCStep auto-updates .agentsrc.json for project-scoped agents and
// returns nextSteps with an optional confirmation message appended. A non-nil
// error means the project IS registered but config.Load/LoadAgentsRC/rc.Save
// failed on a real error (corrupt JSON, permission denied, disk full) — the
// caller (CreateAgent) has already written AGENT.md, so it downgrades its
// success message to a warning naming the failure instead of rolling back.
// A missing .agentsrc.json (os.IsNotExist) is legitimate absence, not an
// error: the project simply hasn't been initialized with one yet.
func appendAgentsRCStep(nextSteps []string, name, scope string) ([]string, error) {
	if scope == "global" {
		return nextSteps, nil
	}
	cfg, err := config.Load()
	if err != nil {
		return nextSteps, fmt.Errorf("loading config.json: %w", err)
	}
	projPath := cfg.GetProjectPath(scope)
	if projPath == "" {
		return nextSteps, nil
	}
	rc, err := config.LoadAgentsRC(projPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nextSteps, nil
		}
		return nextSteps, fmt.Errorf("loading .agentsrc.json: %w", err)
	}
	rc.Agents = config.AppendUnique(rc.Agents, name)
	if err := rc.Save(projPath); err != nil {
		return nextSteps, fmt.Errorf("saving .agentsrc.json: %w", err)
	}
	return append(nextSteps, "Updated .agentsrc.json with agent '"+name+"'"), nil
}
