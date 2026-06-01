package skills

import (
	"github.com/AGOrcha/dot-agents/internal/projectsync"
)

// List prints skills under ~/.agents/skills/<scope>/.
func List(scope string) error {
	return projectsync.ListBucket(scope, projectsync.BucketSpec{
		Bucket:       "skills",
		ManifestName: "SKILL.md",
		Singular:     "skill",
		Plural:       "Skills",
	})
}
