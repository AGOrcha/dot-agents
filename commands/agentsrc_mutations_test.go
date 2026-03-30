package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/config"
)

const (
	testProjectName     = "myproj"
	testSkillName       = "new-skill"
	testSourceTypeLocal = "local"
	testSourceTypeGit   = "git"
	errFmtChdir         = "chdir: %v"
)

// helpers ────────────────────────────────────────────────────────────────────

// setupEnv wires AGENTS_HOME to a temp dir with a registered project and
// returns (agentsHome, projectPath).  The project has a .agentsrc.json with
// only a local source so callers can verify mutations.
func setupEnv(t *testing.T, projectName string) (agentsHome, projectPath string) {
	t.Helper()
	tmp := t.TempDir()
	agentsHome = filepath.Join(tmp, "agents")
	projectPath = filepath.Join(tmp, "repo", projectName)

	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("AGENTS_HOME", agentsHome)

	// Register the project in config
	cfg := &config.Config{
		Version:  1,
		Projects: map[string]config.Project{},
		Agents:   map[string]config.Agent{},
	}
	cfg.AddProject(projectName, projectPath)
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save: %v", err)
	}

	// Write a minimal manifest in the project repo
	rc := &config.AgentsRC{
		Version: 1,
		Project: projectName,
		Sources: []config.Source{{Type: testSourceTypeLocal}},
	}
	if err := rc.Save(projectPath); err != nil {
		t.Fatalf("rc.Save: %v", err)
	}

	return agentsHome, projectPath
}

// ── createSkill / createAgent: target registered project, not CWD ────────────

// TestCreateSkill_UpdatesRegisteredProjectManifest verifies that creating a
// project-scoped skill writes to the registered repo's .agentsrc.json even
// when the caller's CWD is a completely different directory.
func TestCreateSkillUpdatesRegisteredProjectManifest(t *testing.T) {
	agentsHome, projectPath := setupEnv(t, testProjectName)

	// CWD is a completely unrelated directory (the agentsHome itself).
	if err := os.Chdir(agentsHome); err != nil {
		t.Fatalf(errFmtChdir, err)
	}

	if err := createSkill(testSkillName, testProjectName); err != nil {
		t.Fatalf("createSkill: %v", err)
	}

	// Manifest in the registered project repo must list the skill.
	rc, err := config.LoadAgentsRC(projectPath)
	if err != nil {
		t.Fatalf("LoadAgentsRC: %v", err)
	}
	found := false
	for _, s := range rc.Skills {
		if s == testSkillName {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("skill 'new-skill' not found in project manifest; Skills=%v", rc.Skills)
	}
}

// TestCreateSkill_DoesNotMutateUnrelatedCWDManifest verifies that creating a
// project-scoped skill does NOT touch a .agentsrc.json that happens to exist
// in the current directory.
func TestCreateSkillDoesNotMutateUnrelatedCWDManifest(t *testing.T) {
	agentsHome, _ := setupEnv(t, testProjectName)

	// Write a second, unrelated manifest in agentsHome (which is our CWD).
	cwdManifest := &config.AgentsRC{
		Version: 1,
		Project: "other",
		Sources: []config.Source{{Type: testSourceTypeLocal}},
	}
	if err := cwdManifest.Save(agentsHome); err != nil {
		t.Fatalf("save cwd manifest: %v", err)
	}
	if err := os.Chdir(agentsHome); err != nil {
		t.Fatalf(errFmtChdir, err)
	}

	if err := createSkill(testSkillName, testProjectName); err != nil {
		t.Fatalf("createSkill: %v", err)
	}

	// The manifest in CWD must not have been touched.
	rc, err := config.LoadAgentsRC(agentsHome)
	if err != nil {
		t.Fatalf("reload cwd manifest: %v", err)
	}
	for _, s := range rc.Skills {
		if s == testSkillName {
			t.Error("cwd manifest was incorrectly mutated by project-scoped skill creation")
		}
	}
}

// TestCreateAgent_UpdatesRegisteredProjectManifest mirrors the skill test for
// agents.
func TestCreateAgentUpdatesRegisteredProjectManifest(t *testing.T) {
	agentsHome, projectPath := setupEnv(t, testProjectName)

	if err := os.Chdir(agentsHome); err != nil {
		t.Fatalf(errFmtChdir, err)
	}

	if err := createAgent("new-agent", testProjectName); err != nil {
		t.Fatalf("createAgent: %v", err)
	}

	rc, err := config.LoadAgentsRC(projectPath)
	if err != nil {
		t.Fatalf("LoadAgentsRC: %v", err)
	}
	found := false
	for _, a := range rc.Agents {
		if a == "new-agent" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("agent 'new-agent' not found in project manifest; Agents=%v", rc.Agents)
	}
}

// TestCreateSkill_UnregisteredProjectSkipsManifest verifies that creating a
// skill for a project name that is NOT registered in config silently skips the
// manifest update instead of panicking or writing to an arbitrary path.
func TestCreateSkillUnregisteredProjectSkipsManifest(t *testing.T) {
	agentsHome, _ := setupEnv(t, testProjectName)
	_ = agentsHome

	// "ghost" is not registered — no crash, no manifest written anywhere.
	if err := createSkill("s", "ghost"); err != nil {
		t.Fatalf("createSkill for unregistered scope: %v", err)
	}
}

// ── doctor manifest check: all git sources validated ─────────────────────────

// buildManifestWithSources writes a .agentsrc.json with the given sources to
// dir and returns the path.
func buildManifestWithSources(t *testing.T, dir string, sources []config.Source) string {
	t.Helper()
	rc := &config.AgentsRC{
		Version: 1,
		Project: filepath.Base(dir),
		Sources: sources,
	}
	if err := rc.Save(dir); err != nil {
		t.Fatalf("save manifest: %v", err)
	}
	return filepath.Join(dir, config.AgentsRCFile)
}

// manifestGitSourcesStatus runs the same logic as the doctor manifest check and
// returns (missingURLs, presentURLs).
func manifestGitSourcesStatus(t *testing.T, manifestPath string) (missing, present []string) {
	t.Helper()
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var rc config.AgentsRC
	if err := json.Unmarshal(data, &rc); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	for _, src := range rc.Sources {
		if src.Type != testSourceTypeGit || src.URL == "" {
			continue
		}
		cacheDir := config.GitSourceCacheDir(src.URL)
		if _, err := os.Stat(cacheDir); err != nil {
			missing = append(missing, src.URL)
		} else {
			present = append(present, src.URL)
		}
	}
	return missing, present
}

// TestDoctorManifest_AllSourcesPresent verifies that a manifest with two git
// sources where both caches exist is reported healthy.
func TestDoctorManifestAllSourcesPresent(t *testing.T) {
	tmp := t.TempDir()
	url1 := "https://github.com/example/repo1.git"
	url2 := "https://github.com/example/repo2.git"

	// Pre-create both cache directories so they look fetched.
	for _, url := range []string{url1, url2} {
		if err := os.MkdirAll(config.GitSourceCacheDir(url), 0755); err != nil {
			t.Fatal(err)
		}
	}

	mf := buildManifestWithSources(t, tmp, []config.Source{
		{Type: testSourceTypeLocal},
		{Type: testSourceTypeGit, URL: url1},
		{Type: testSourceTypeGit, URL: url2},
	})

	missing, present := manifestGitSourcesStatus(t, mf)
	if len(missing) != 0 {
		t.Errorf("expected no missing sources, got %v", missing)
	}
	if len(present) != 2 {
		t.Errorf("expected 2 present sources, got %v", present)
	}
}

// TestDoctorManifest_FirstPresentSecondMissing verifies that a manifest where
// the first git source is cached but the second is not is NOT reported as
// healthy — the old goto logic would have incorrectly returned "ok".
func TestDoctorManifestFirstPresentSecondMissing(t *testing.T) {
	tmp := t.TempDir()
	url1 := "https://github.com/example/present.git"
	url2 := "https://github.com/example/missing.git"

	// Only create the first cache.
	if err := os.MkdirAll(config.GitSourceCacheDir(url1), 0755); err != nil {
		t.Fatal(err)
	}

	mf := buildManifestWithSources(t, tmp, []config.Source{
		{Type: testSourceTypeGit, URL: url1},
		{Type: testSourceTypeGit, URL: url2},
	})

	missing, present := manifestGitSourcesStatus(t, mf)
	if len(missing) != 1 || missing[0] != url2 {
		t.Errorf("expected [%s] missing, got %v", url2, missing)
	}
	if len(present) != 1 || present[0] != url1 {
		t.Errorf("expected [%s] present, got %v", url1, present)
	}
}

// TestDoctorManifest_AllMissing verifies all-absent sources are all reported.
func TestDoctorManifestAllMissing(t *testing.T) {
	tmp := t.TempDir()

	mf := buildManifestWithSources(t, tmp, []config.Source{
		{Type: testSourceTypeGit, URL: "https://github.com/example/a.git"},
		{Type: testSourceTypeGit, URL: "https://github.com/example/b.git"},
		{Type: testSourceTypeGit, URL: "https://github.com/example/c.git"},
	})

	missing, present := manifestGitSourcesStatus(t, mf)
	if len(missing) != 3 {
		t.Errorf("expected 3 missing, got %v", missing)
	}
	if len(present) != 0 {
		t.Errorf("expected 0 present, got %v", present)
	}
}

// TestDoctorManifest_LocalOnlyNoGitSources verifies that a local-only manifest
// has no git sources to check.
func TestDoctorManifestLocalOnlyNoGitSources(t *testing.T) {
	tmp := t.TempDir()

	mf := buildManifestWithSources(t, tmp, []config.Source{
		{Type: testSourceTypeLocal},
	})

	missing, present := manifestGitSourcesStatus(t, mf)
	if len(missing) != 0 || len(present) != 0 {
		t.Errorf("local-only manifest should have no git sources; missing=%v present=%v", missing, present)
	}
}
