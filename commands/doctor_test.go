package commands

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/platform"
)

func TestDoctorPluginBrokenSymlink(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	projectPath := filepath.Join(tmp, "repo")
	t.Setenv("AGENTS_HOME", agentsHome)

	if err := os.MkdirAll(filepath.Join(agentsHome, "plugins", "global", "my-plugin"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentsHome, "plugins", "global", "my-plugin", platform.PluginManifestName), []byte(`schema_version: 1
kind: native
name: my-plugin
platforms: [opencode]
`), 0644); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(projectPath, ".opencode", "plugins"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(tmp, "missing-target"), filepath.Join(projectPath, ".opencode", "plugins", "my-plugin")); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Version: 1,
		Projects: map[string]config.Project{
			"repo": {Path: projectPath},
		},
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	if err := runDoctor(nil, nil); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	os.Stdout = oldStdout

	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	rendered := string(out)
	if !strings.Contains(rendered, "Plugins") {
		t.Fatalf("doctor output missing Plugins section:\n%s", rendered)
	}
	if !strings.Contains(rendered, "broken symlink") {
		t.Fatalf("doctor output missing broken symlink report:\n%s", rendered)
	}
	if !strings.Contains(rendered, "my-plugin") {
		t.Fatalf("doctor output missing plugin name:\n%s", rendered)
	}
}
