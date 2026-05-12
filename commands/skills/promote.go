package skills

import (
	"os"
	"path/filepath"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/platform"
	"github.com/NikashPrakash/dot-agents/internal/projectsync"
	"github.com/NikashPrakash/dot-agents/internal/ui"
)

// PromoteSkillIn promotes a repo-local skill (.agents/skills/<name>/) into the
// shared agents store. The canonical location (~/.agents/skills/<project>/<name>/)
// becomes the real directory, and the repo-local path is converted to a managed
// symlink pointing at it.
func PromoteSkillIn(name, projectPath string) error {
	return projectsync.PromoteResource(name, projectPath, projectsync.PromoteSpec{
		BucketSpec: projectsync.BucketSpec{
			Bucket:       "skills",
			ManifestName: "SKILL.md",
			Singular:     "skill",
			Plural:       "Skills",
		},
		Force:               false,
		ExistingRealDirHint: "; cannot promote",
		RegisterInRC: func(rc *config.AgentsRC, n string) int {
			rc.Skills = config.AppendUnique(rc.Skills, n)
			return len(rc.Skills)
		},
		MirrorRefresh: refreshSkillMirror,
	})
}

// refreshSkillMirror executes the shared skill mirror plan with relative
// target roots so allowlist prefix checks pass even when an existing
// .claude/skills directory needs to be replaced.
func refreshSkillMirror(projectName, _ string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		ui.Bullet("warn", "could not determine home directory; skipping platform mirrors: "+err.Error())
		return nil
	}
	if err := platform.ExecuteSharedSkillMirrorPlan(projectName, homeDir,
		filepath.Join(".agents", "skills"),
		filepath.Join(".claude", "skills"),
	); err != nil {
		ui.Bullet("warn", "platform mirror refresh failed: "+err.Error())
	}
	return nil
}
