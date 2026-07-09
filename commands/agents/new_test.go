package agents

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/testutil"
)

// captureStdout redirects os.Stdout for the duration of fn and returns
// everything written to it.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	fn()
	_ = w.Close()
	os.Stdout = old
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

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

	out, err := appendAgentsRCStep(in, "any-name", "global")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 1 || out[0] != "step1" {
		t.Fatalf("expected unchanged steps for global scope, got: %v", out)
	}
}

func TestAppendAgentsRCStep_ProjectNotRegistered(t *testing.T) {
	testutil.NewTempProject(t, "")

	out, err := appendAgentsRCStep([]string{"step1"}, "any-name", "unregistered-scope")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected unchanged steps when project not registered, got: %v", out)
	}
}

// TestAppendAgentsRCStep_MissingAgentsRCIsLegitimateAbsence covers the
// os.IsNotExist branch: a registered project that simply has no
// .agentsrc.json yet is a legitimate best-effort skip, not a loud error.
func TestAppendAgentsRCStep_MissingAgentsRCIsLegitimateAbsence(t *testing.T) {
	agentsHome, projectPath := testutil.NewTempProject(t, "myproj")
	_ = agentsHome
	if err := os.Remove(filepath.Join(projectPath, config.AgentsRCFile)); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.AddProject("myproj", projectPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	out, err := appendAgentsRCStep([]string{"step1"}, "agent-x", "myproj")
	if err != nil {
		t.Fatalf("missing .agentsrc.json should not be a loud error, got: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected unchanged steps when .agentsrc.json absent, got: %v", out)
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

	out, err := appendAgentsRCStep([]string{"step1"}, "agent-x", "myproj")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
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
	steps, err := createAgentNextSteps("/tmp/agent/AGENT.md", "n", "global")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("expected 1 step for global, got: %v", steps)
	}
	if !strings.Contains(steps[0], "AGENT.md") {
		t.Fatalf("step should mention AGENT.md, got: %v", steps[0])
	}
}

// TestCreateAgent_CorruptAgentsRCWarnsInsteadOfSuccess is the acceptance test
// for se8: a corrupt .agentsrc.json must not silently make CreateAgent look
// like an unqualified success. AGENT.md is still written (no rollback);
// CreateAgent must still return nil (the agent WAS created), and the printed
// output must carry a registration-failed warning, not the plain "Created"
// success box.
func TestCreateAgent_CorruptAgentsRCWarnsInsteadOfSuccess(t *testing.T) {
	_, projectPath := testutil.NewTempProject(t, "warnproj")
	if err := os.WriteFile(filepath.Join(projectPath, config.AgentsRCFile), []byte("{not-json"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.AddProject("warnproj", projectPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	stdout := captureStdout(t, func() {
		if err := CreateAgent("gadget", "warnproj"); err != nil {
			t.Fatalf("CreateAgent: %v", err)
		}
	})

	manifest := filepath.Join(config.AgentsHome(), "agents", "warnproj", "gadget", agentManifestName)
	if _, err := os.Stat(manifest); err != nil {
		t.Fatalf("AGENT.md should still be written despite registration failure: %v", err)
	}

	if strings.Contains(stdout, "✓") {
		t.Fatalf("expected no unqualified success marker, got: %q", stdout)
	}
	if !strings.Contains(stdout, "registration did not fully complete") {
		t.Fatalf("expected a registration-failed warning, got: %q", stdout)
	}

	// The corrupt manifest must be left untouched, not rolled back/overwritten.
	raw, err := os.ReadFile(filepath.Join(projectPath, config.AgentsRCFile))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "{not-json" {
		t.Fatalf("corrupt .agentsrc.json should be left untouched, got: %q", raw)
	}
}
