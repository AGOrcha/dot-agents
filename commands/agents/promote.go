package agents

import (
	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/platform"
	"github.com/AGOrcha/dot-agents/internal/projectsync"
	"github.com/AGOrcha/dot-agents/internal/ui"
	"path/filepath"
)

// PromoteAgentIn promotes a repo-local agent (.agents/agents/<name>/) into the
// shared agents store. The canonical location (~/.agents/agents/<project>/<name>/)
// becomes the real directory, and the repo-local path is converted to a managed
// symlink pointing at it.
func PromoteAgentIn(name, projectPath string, force bool) error {
	return projectsync.PromoteResource(name, projectPath, projectsync.PromoteSpec{
		BucketSpec: projectsync.BucketSpec{
			Bucket:       "agents",
			ManifestName: agentManifestName,
			Singular:     "agent",
			Plural:       "Agents",
		},
		Force:               force,
		ExistingRealDirHint: "; use --force to overwrite",
		RegisterInRC: func(rc *config.AgentsRC, n string) int {
			rc.Agents = config.AppendUnique(rc.Agents, n)
			return len(rc.Agents)
		},
		MirrorRefresh: refreshAgentMirror,
	})
}

func refreshAgentMirror(projectName, projectPath string) error {
	intents, err := platform.BuildSharedAgentMirrorIntents(projectName, filepath.Join(".claude", "agents"))
	if err != nil {
		ui.Bullet("warn", "building agent mirror intents: "+err.Error())
		return nil
	}
	plan, perr := platform.BuildResourcePlan(intents)
	if perr != nil {
		ui.Bullet("warn", "agent mirror plan: "+perr.Error())
		return nil
	}
	if err := plan.Execute(projectPath, config.AgentsHome()); err != nil {
		ui.Bullet("warn", "platform agent symlink refresh failed: "+err.Error())
	}
	return nil
}
