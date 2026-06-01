package agents

import (
	"github.com/AGOrcha/dot-agents/internal/projectsync"
)

func listAgents(scope string) error {
	return projectsync.ListBucket(scope, projectsync.BucketSpec{
		Bucket:       "agents",
		ManifestName: agentManifestName,
		Singular:     "agent",
		Plural:       "Agents",
	})
}
