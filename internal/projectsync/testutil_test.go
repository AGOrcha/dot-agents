package projectsync_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/projectsync"
)

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || indexOf(s, substr) >= 0)
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// duplicate of promoteEnv from promote_test.go scoped to this file to keep
// the helper local without exporting it.
func promoteEnvX(t *testing.T, projectName string) (agentsHome, projectPath string) {
	t.Helper()
	tmp := t.TempDir()
	agentsHome = filepath.Join(tmp, "agentshome")
	projectPath = filepath.Join(tmp, "repo")
	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)
	rc := &config.AgentsRC{
		Version: 1,
		Project: projectName,
		Sources: []config.Source{{Type: "local"}},
	}
	if err := rc.Save(projectPath); err != nil {
		t.Fatal(err)
	}
	return agentsHome, projectPath
}

func widgetSpecX(_ *testing.T) projectsync.PromoteSpec {
	return projectsync.PromoteSpec{
		BucketSpec: projectsync.BucketSpec{
			Bucket:       "widgets",
			ManifestName: "WIDGET.md",
			Singular:     "widget",
			Plural:       "Widgets",
		},
		ExistingRealDirHint: "; cannot promote",
		RegisterInRC: func(rc *config.AgentsRC, n string) int {
			rc.Skills = config.AppendUnique(rc.Skills, n)
			return len(rc.Skills)
		},
	}
}
