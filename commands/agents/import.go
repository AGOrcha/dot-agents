package agents

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/links"
	"github.com/AGOrcha/dot-agents/internal/platform"
	"github.com/AGOrcha/dot-agents/internal/ui"
)

// ImportAgentIn links ~/.agents/agents/<project>/<name>/ into the repo as symlinks
// and ensures .agentsrc.json lists the agent.
func ImportAgentIn(deps Deps, name, projectPath string) error {
	rc, err := config.LoadAgentsRC(projectPath)
	if err != nil {
		return fmt.Errorf("loading .agentsrc.json for agent %q: %w", name, err)
	}
	projectName := rc.Project
	if projectName == "" {
		return agentUserError(deps, ".agentsrc.json has no project name set", "Run `da install --generate` or `da add .` to repair the manifest.")
	}

	agentsHome := config.AgentsHome()
	canonicalPath := filepath.Join(agentsHome, "agents", projectName, name)
	agentMD := filepath.Join(canonicalPath, agentManifestName)
	if _, err := os.Stat(agentMD); err != nil {
		if os.IsNotExist(err) {
			return agentUserError(deps, fmt.Sprintf("agent %q not found at canonical path %s (expected %s)", name, config.DisplayPath(canonicalPath), agentManifestName), "Create the canonical agent first, or run `da agents list` to confirm the name.")
		}
		return fmt.Errorf("agent %q: %w", name, err)
	}

	if err := ensureImportRepoAgentsSlot(deps, stdReadlinker{}, name, canonicalPath, projectPath); err != nil {
		return err
	}

	intents := []platform.ResourceIntent{buildSingleAgentMirrorIntent(projectName, name, filepath.Join(".claude", "agents"))}
	plan, err := platform.BuildResourcePlan(intents)
	if err != nil {
		return fmt.Errorf("building import plan for agent %q: %w", name, err)
	}
	if err := plan.Execute(projectPath, agentsHome); err != nil {
		return fmt.Errorf("importing agent %q: %w", name, err)
	}

	rc.Agents = config.AppendUnique(rc.Agents, name)
	if err := rc.Save(projectPath); err != nil {
		return fmt.Errorf("updating .agentsrc.json for agent %q: %w", name, err)
	}

	ui.SuccessBox(
		fmt.Sprintf("Imported agent '%s' for project '%s'", name, projectName),
		fmt.Sprintf("Canonical: %s", config.DisplayPath(canonicalPath)),
		fmt.Sprintf("Registered in .agentsrc.json (%d agent(s) total)", len(rc.Agents)),
		"Run 'da refresh' to sync across all platforms",
	)
	return nil
}

func ensureImportRepoAgentsSlot(deps Deps, rl readlinker, name, canonicalPath, projectPath string) error {
	repoLocal := filepath.Join(projectPath, ".agents", "agents", name)
	fi, err := os.Lstat(repoLocal)
	if err != nil {
		if os.IsNotExist(err) {
			return links.Symlink(canonicalPath, repoLocal)
		}
		return err
	}
	// Idempotency must use the managed-link abstraction, not raw string
	// equality on os.Readlink: a directory managed link is a junction on
	// Windows whose stored target is cleaned/absolute/extended-length and
	// never byte-equal to canonicalPath. POSIX behavior is unchanged.
	if links.IsManagedLink(repoLocal, canonicalPath) {
		return nil
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		existing, err := rl.Readlink(repoLocal)
		if err != nil {
			return fmt.Errorf("reading symlink for agent %q: %w", name, err)
		}
		return agentUserError(deps, fmt.Sprintf("agent %q: .agents/agents/%s is a symlink pointing to %q, not the canonical path %s", name, name, existing, canonicalPath), "Remove the stale link and retry.")
	}
	if fi.IsDir() {
		if _, err := os.Stat(filepath.Join(repoLocal, agentManifestName)); err == nil {
			return agentUserError(deps, fmt.Sprintf("agent %q already exists as a real directory at %s", name, repoLocal), "Remove it, or use `da agents promote` first.")
		}
	}
	return agentUserError(deps, fmt.Sprintf("agent %q: unexpected path at %s", name, repoLocal), "Remove it before importing.")
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
		MarkerFiles:   []string{agentManifestName},
	}
}
