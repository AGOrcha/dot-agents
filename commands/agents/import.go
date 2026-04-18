package agents

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/links"
	"github.com/NikashPrakash/dot-agents/internal/platform"
	"github.com/NikashPrakash/dot-agents/internal/ui"
)

// ImportAgentIn links ~/.agents/agents/<project>/<name>/ into the repo as symlinks
// and ensures .agentsrc.json lists the agent.
func ImportAgentIn(name, projectPath string) error {
	rc, err := config.LoadAgentsRC(projectPath)
	if err != nil {
		return fmt.Errorf("loading .agentsrc.json: %w", err)
	}
	projectName := rc.Project
	if projectName == "" {
		return fmt.Errorf(".agentsrc.json has no project name set")
	}

	agentsHome := config.AgentsHome()
	canonicalPath := filepath.Join(agentsHome, "agents", projectName, name)
	agentMD := filepath.Join(canonicalPath, "AGENT.md")
	if _, err := os.Stat(agentMD); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("agent %q not found at canonical path %s (expected AGENT.md)", name, config.DisplayPath(canonicalPath))
		}
		return fmt.Errorf("agent %q: %w", name, err)
	}

	if err := ensureImportRepoAgentsSlot(name, canonicalPath, projectPath); err != nil {
		return err
	}

	intents := []platform.ResourceIntent{buildSingleAgentMirrorIntent(projectName, name, filepath.Join(".claude", "agents"))}
	plan, err := platform.BuildResourcePlan(intents)
	if err != nil {
		return fmt.Errorf("building import plan: %w", err)
	}
	if err := plan.Execute(projectPath, agentsHome); err != nil {
		return fmt.Errorf("importing agent %q: %w", name, err)
	}

	rc.Agents = config.AppendUnique(rc.Agents, name)
	if err := rc.Save(projectPath); err != nil {
		return fmt.Errorf("updating .agentsrc.json: %w", err)
	}

	ui.SuccessBox(
		fmt.Sprintf("Imported agent '%s' for project '%s'", name, projectName),
		fmt.Sprintf("Canonical: %s", config.DisplayPath(canonicalPath)),
		fmt.Sprintf("Registered in .agentsrc.json (%d agent(s) total)", len(rc.Agents)),
		"Run 'dot-agents refresh' to sync across all platforms",
	)
	return nil
}

func ensureImportRepoAgentsSlot(name, canonicalPath, projectPath string) error {
	repoLocal := filepath.Join(projectPath, ".agents", "agents", name)
	fi, err := os.Lstat(repoLocal)
	if err != nil {
		if os.IsNotExist(err) {
			return links.Symlink(canonicalPath, repoLocal)
		}
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		existing, err := os.Readlink(repoLocal)
		if err != nil {
			return fmt.Errorf("reading symlink for agent %q: %w", name, err)
		}
		if existing == canonicalPath {
			return nil
		}
		return fmt.Errorf("agent %q: .agents/agents/%s is a symlink pointing to %q, not the canonical path %s", name, name, existing, canonicalPath)
	}
	if fi.IsDir() {
		if _, err := os.Stat(filepath.Join(repoLocal, "AGENT.md")); err == nil {
			return fmt.Errorf("agent %q already exists as a real directory at %s; remove it or use 'agents promote' first", name, repoLocal)
		}
	}
	return fmt.Errorf("agent %q: unexpected path at %s", name, repoLocal)
}

func buildSingleAgentMirrorIntent(project, name, targetRoot string) platform.ResourceIntent {
	root := filepath.Clean(targetRoot)
	return platform.ResourceIntent{
		IntentID:    fmt.Sprintf("agents.import.%s.%s.%s", project, name, strings.ReplaceAll(filepath.ToSlash(root), "/", "-")),
		Project:     project,
		Bucket:      "agents",
		LogicalName: name,
		TargetPath:  filepath.Join(root, name),
		Ownership:   platform.ResourceOwnershipSharedRepo,
		SourceRef: platform.ResourceSourceRef{
			Scope:        project,
			Bucket:       "agents",
			RelativePath: name,
			Kind:         platform.ResourceSourceCanonicalDir,
			Origin:       "agents-import",
		},
		Shape:         platform.ResourceShapeDirectDir,
		Transport:     platform.ResourceTransportSymlink,
		Materializer:  "shared-agent-dir-symlink",
		ReplacePolicy: platform.ResourceReplaceAllowlistedImportedDirOnly,
		PrunePolicy:   platform.ResourcePruneTarget,
		MarkerFiles:   []string{"AGENT.md"},
	}
}
