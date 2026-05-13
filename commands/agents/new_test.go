package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/testutil"
)

func TestCreateAgent_GlobalScope_WritesManifest(t *testing.T) {
	agentsHome, _ := testutil.NewTempProject(t, "")

	if err := CreateAgent("brand-new", "global"); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}

	manifest := filepath.Join(agentsHome, "agents", "global", "brand-new", agentManifestName)
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatalf("manifest missing: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("manifest should not be empty")
	}
}

func TestCreateAgent_DoesNotOverwriteExisting(t *testing.T) {
	agentsHome, _ := testutil.NewTempProject(t, "")

	manifestDir := filepath.Join(agentsHome, "agents", "global", "preexisting")
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(manifestDir, agentManifestName)
	if err := os.WriteFile(manifest, []byte("ORIGINAL"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := CreateAgent("preexisting", "global"); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}

	got, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "ORIGINAL" {
		t.Fatalf("manifest was overwritten, got: %q", got)
	}
}

func TestWriteAgentMDIfAbsent_NoOpWhenPresent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, agentManifestName)
	if err := os.WriteFile(path, []byte("KEEP"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := writeAgentMDIfAbsent(path, "ignored"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "KEEP" {
		t.Fatalf("file content was changed, got: %q", got)
	}
}

func TestWriteAgentMDIfAbsent_CreatesNew(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, agentManifestName)

	if err := writeAgentMDIfAbsent(path, "fresh-agent"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("file should exist: %v", err)
	}
	if !strings.Contains(string(got), "fresh-agent") {
		t.Fatalf("expected manifest to include agent name, got: %q", got)
	}
}

func TestAppendAgentsRCStep_GlobalScopeReturnsUnchanged(t *testing.T) {
	testutil.NewTempProject(t, "")
	in := []string{"step1"}

	out := appendAgentsRCStep(in, "any-name", "global")
	if len(out) != 1 || out[0] != "step1" {
		t.Fatalf("expected unchanged steps for global scope, got: %v", out)
	}
}

func TestAppendAgentsRCStep_ProjectNotRegistered(t *testing.T) {
	testutil.NewTempProject(t, "")

	out := appendAgentsRCStep([]string{"step1"}, "any-name", "unregistered-scope")
	if len(out) != 1 {
		t.Fatalf("expected unchanged steps when project not registered, got: %v", out)
	}
}

func TestAppendAgentsRCStep_UpdatesAgentsRC(t *testing.T) {
	agentsHome, projectPath := testutil.NewTempProject(t, "myproj")
	_ = agentsHome

	// Register project so GetProjectPath resolves.
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.AddProject("myproj", projectPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	out := appendAgentsRCStep([]string{"step1"}, "agent-x", "myproj")
	if len(out) != 2 {
		t.Fatalf("expected appended step, got: %v", out)
	}
	if !strings.Contains(out[1], "agent-x") {
		t.Fatalf("appended step missing agent name: %v", out)
	}

	rc, err := config.LoadAgentsRC(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range rc.Agents {
		if a == "agent-x" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected agent-x in rc.Agents, got: %v", rc.Agents)
	}
}

func TestCreateAgentNextSteps_GlobalIncludesDisplayPath(t *testing.T) {
	testutil.NewTempProject(t, "")
	steps := createAgentNextSteps("/tmp/agent/AGENT.md", "n", "global")
	if len(steps) != 1 {
		t.Fatalf("expected 1 step for global, got: %v", steps)
	}
	if !strings.Contains(steps[0], "AGENT.md") {
		t.Fatalf("step should mention AGENT.md, got: %v", steps[0])
	}
}
