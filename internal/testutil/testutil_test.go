package testutil_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/testutil"
)

func TestNewTempProject_WritesAgentsRCAndSetsEnv(t *testing.T) {
	agentsHome, projectPath := testutil.NewTempProject(t, "myproj")

	if got := os.Getenv("AGENTS_HOME"); got != agentsHome {
		t.Errorf("AGENTS_HOME = %q; want %q", got, agentsHome)
	}
	if _, err := os.Stat(agentsHome); err != nil {
		t.Errorf("agentsHome not created: %v", err)
	}
	rcPath := filepath.Join(projectPath, ".agentsrc.json")
	data, err := os.ReadFile(rcPath)
	if err != nil {
		t.Fatalf("reading .agentsrc.json: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed["project"] != "myproj" {
		t.Errorf("project = %v; want myproj", parsed["project"])
	}
	if parsed["version"].(float64) != 1 {
		t.Errorf("version = %v; want 1", parsed["version"])
	}
}

func TestWriteAgentsRC_NilUsesDefaults(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, "ah"))

	testutil.WriteAgentsRC(t, tmp, nil)
	rc, err := config.LoadAgentsRC(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if rc.Version != 1 {
		t.Errorf("version = %d; want 1", rc.Version)
	}
	if len(rc.Sources) != 1 || rc.Sources[0].Type != "local" {
		t.Errorf("sources = %+v; want one local source", rc.Sources)
	}
}

func TestWriteAgentsRC_PreservesProvidedRC(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, "ah"))

	custom := &config.AgentsRC{
		Version: 1,
		Project: "x",
		Skills:  []string{"alpha", "beta"},
		Sources: []config.Source{{Type: "local"}},
	}
	testutil.WriteAgentsRC(t, tmp, custom)
	rc, err := config.LoadAgentsRC(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if rc.Project != "x" {
		t.Errorf("project = %q; want x", rc.Project)
	}
	if len(rc.Skills) != 2 {
		t.Errorf("skills = %v; want 2 entries", rc.Skills)
	}
}

func TestWriteAgentManifest_LayoutAndFrontmatter(t *testing.T) {
	_, projectPath := testutil.NewTempProject(t, "p1")
	testutil.WriteAgentManifest(t, projectPath, "alpha")

	manifestPath := filepath.Join(projectPath, ".agents", "agents", "alpha", "AGENT.md")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("reading AGENT.md: %v", err)
	}
	body := string(data)
	if !strings.Contains(body, "name: alpha") {
		t.Errorf("frontmatter missing name; got: %s", body)
	}
	if !strings.Contains(body, "description: test agent") {
		t.Errorf("frontmatter missing description; got: %s", body)
	}
}

func TestWriteSkillManifest_LayoutAndFrontmatter(t *testing.T) {
	_, projectPath := testutil.NewTempProject(t, "p1")
	testutil.WriteSkillManifest(t, projectPath, "beta")

	manifestPath := filepath.Join(projectPath, ".agents", "skills", "beta", "SKILL.md")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("reading SKILL.md: %v", err)
	}
	body := string(data)
	if !strings.Contains(body, "name: beta") {
		t.Errorf("frontmatter missing name; got: %s", body)
	}
	if !strings.Contains(body, "description: test skill") {
		t.Errorf("frontmatter missing description; got: %s", body)
	}
}

func TestWriteCanonicalAgent_ReturnsDirAndWritesManifest(t *testing.T) {
	agentsHome, _ := testutil.NewTempProject(t, "p1")
	dir := testutil.WriteCanonicalAgent(t, agentsHome, "p1", "alpha")

	want := filepath.Join(agentsHome, "agents", "p1", "alpha")
	if dir != want {
		t.Errorf("dir = %q; want %q", dir, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "AGENT.md")); err != nil {
		t.Errorf("canonical AGENT.md missing: %v", err)
	}
}

func TestWriteCanonicalSkill_ReturnsDirAndWritesManifest(t *testing.T) {
	agentsHome, _ := testutil.NewTempProject(t, "p1")
	dir := testutil.WriteCanonicalSkill(t, agentsHome, "p1", "beta")

	want := filepath.Join(agentsHome, "skills", "p1", "beta")
	if dir != want {
		t.Errorf("dir = %q; want %q", dir, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err != nil {
		t.Errorf("canonical SKILL.md missing: %v", err)
	}
}

func TestWriteScopeFile_CreatesParentsAndWritesContent(t *testing.T) {
	tmp := t.TempDir()

	testutil.WriteScopeFile(t, tmp, "settings", "global", "claude.json", []byte(`{"x":1}`))
	got, err := os.ReadFile(filepath.Join(tmp, "settings", "global", "claude.json"))
	if err != nil {
		t.Fatalf("reading scope file: %v", err)
	}
	if string(got) != `{"x":1}` {
		t.Errorf("content = %q; want {\"x\":1}", got)
	}
}

func TestInitGitRepo_CreatesCommittedTreeWithIdentity(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := t.TempDir()

	testutil.InitGitRepo(t, repo, map[string]string{
		"README.md":           "hello\n",
		".agents/handoff.md":  "# handoff\n",
		"a/b/c/deep.txt":      "deep\n",
	})

	if _, err := os.Stat(filepath.Join(repo, ".git")); err != nil {
		t.Errorf("expected .git to exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, "a/b/c/deep.txt")); err != nil {
		t.Errorf("nested file missing: %v", err)
	}

	cmd := exec.Command("git", "-C", repo, "log", "--format=%s")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	if !strings.Contains(string(out), "init") {
		t.Errorf("expected commit subject 'init'; got: %q", out)
	}

	cmd = exec.Command("git", "-C", repo, "log", "--format=%an %ae")
	out, err = cmd.Output()
	if err != nil {
		t.Fatalf("git log identity: %v", err)
	}
	if !strings.Contains(string(out), "Test test@example.com") {
		t.Errorf("expected Test author identity; got: %q", out)
	}
}
