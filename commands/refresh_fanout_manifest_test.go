package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/config"
)

// TestRunRefresh_FanOutLeavesEveryLinkedManifestUntouched is the regression
// guard for the field report: ONE `da refresh` invoked at a workspace root
// iterated every linked project and re-injected `"hooks": false, "mcp": false,
// "settings": false` into each sub-repo's local .agentsrc.json.
//
// The fan-out is what made the blast radius workspace-wide, and it is also what
// produced the observed asymmetry: runRefresh iterates cfg.ListProjects() — the
// managed-project registry — so registered MAIN checkouts were rewritten while
// unregistered scratch git worktrees never were. A single-project test cannot
// catch a regression in the loop, so this asserts the property across THREE
// registered projects at once, on raw bytes.
func TestRunRefresh_FanOutLeavesEveryLinkedManifestUntouched(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	seedClaudeInstalledSignal(t, tmp)

	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	// Three linked projects, each with a manifest that DECLARES NOTHING about
	// hooks/mcp/settings — the exact shape that got corrupted in the field.
	const manifest = `{
  "version": 2,
  "repo_id": "github.com/acme/%s",
  "sources": [
    { "type": "local" }
  ]
}
`
	projects := []string{"alpha", "beta", "gamma"}
	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	want := map[string]string{}

	for _, name := range projects {
		projectPath := filepath.Join(tmp, name)
		body := strings.Replace(manifest, "%s", name, 1)
		seedLinkedProject(t, projectPath, body)
		want[name] = body
		cfg.AddProject(name, projectPath)
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	// No project filter: this is the workspace-root fan-out over ALL projects.
	if err := runRefresh("", stdRefreshConfigLoader{}, stdImportDeps{}, stdAddDeps{}); err != nil {
		t.Errorf("runRefresh: %v", err)
	}

	for _, name := range projects {
		assertLinkedManifestUntouched(t, name, filepath.Join(tmp, name), want[name])
	}
}

// seedLinkedProject creates a project dir holding body as its manifest, with a
// backdated mtime so a later write is unambiguous.
func seedLinkedProject(t *testing.T, projectPath, body string) {
	t.Helper()
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(projectPath, config.AgentsRCFile)
	if err := os.WriteFile(manifestPath, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(manifestPath, old, old); err != nil {
		t.Fatal(err)
	}
}

// assertLinkedManifestUntouched checks one project in the fan-out: its manifest
// is byte-identical, carries none of the injected keys, and its lock was still
// stamped.
func assertLinkedManifestUntouched(t *testing.T, name, projectPath, want string) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(projectPath, config.AgentsRCFile))
	if err != nil {
		t.Errorf("%s: reading manifest: %v", name, err)
		return
	}
	if string(got) != want {
		t.Errorf("%s: refresh fan-out rewrote the manifest\n--- before ---\n%s\n--- after ---\n%s",
			name, want, got)
	}
	for _, key := range []string{`"hooks"`, `"mcp"`, `"settings"`} {
		if strings.Contains(string(got), key) {
			t.Errorf("%s: refresh fan-out injected %s:\n%s", name, key, got)
		}
	}
	// The lock is the machine-written artifact and must still be stamped for
	// every project in the fan-out.
	if _, err := os.Stat(config.AgentsLockPath(projectPath)); err != nil {
		t.Errorf("%s: refresh must still write the lock: %v", name, err)
	}
}
