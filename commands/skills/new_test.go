package skills

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/config"
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

// setupProjectScopeSkill creates a registered project (global config.json +
// .agentsrc.json) under a fresh AGENTS_HOME/HOME pair and returns its path.
func setupProjectScopeSkill(t *testing.T, projectName string) string {
	t.Helper()
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", agentsHome)

	projPath := filepath.Join(tmp, projectName)
	if err := os.MkdirAll(projPath, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.AddProject(projectName, projPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	return projPath
}

// TestCreateSkill_CorruptAgentsRCWarnsInsteadOfSuccess is the acceptance test
// for se8: a corrupt .agentsrc.json must not silently make CreateSkill look
// like an unqualified success. SKILL.md is still written (no rollback);
// CreateSkill must still return nil (the skill WAS created), and the printed
// output must carry a registration-failed warning, not the plain "Created"
// success box.
func TestCreateSkill_CorruptAgentsRCWarnsInsteadOfSuccess(t *testing.T) {
	projPath := setupProjectScopeSkill(t, "warnproj")
	if err := os.WriteFile(filepath.Join(projPath, config.AgentsRCFile), []byte("{not-json"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout := captureStdout(t, func() {
		if err := CreateSkill("gadget", "warnproj"); err != nil {
			t.Fatalf("CreateSkill: %v", err)
		}
	})

	skillMD := filepath.Join(config.AgentsHome(), "skills", "warnproj", "gadget", "SKILL.md")
	if _, err := os.Stat(skillMD); err != nil {
		t.Fatalf("SKILL.md should still be written despite registration failure: %v", err)
	}

	if strings.Contains(stdout, "✓") {
		t.Fatalf("expected no unqualified success marker, got: %q", stdout)
	}
	if !strings.Contains(stdout, "registration did not fully complete") {
		t.Fatalf("expected a registration-failed warning, got: %q", stdout)
	}

	// The corrupt manifest must be left untouched, not rolled back/overwritten.
	raw, err := os.ReadFile(filepath.Join(projPath, config.AgentsRCFile))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "{not-json" {
		t.Fatalf("corrupt .agentsrc.json should be left untouched, got: %q", raw)
	}
}

// TestCreateSkill_MissingAgentsRCIsLegitimateAbsence covers the
// os.IsNotExist branch: a registered project that simply has no
// .agentsrc.json yet is a legitimate best-effort skip, not a loud warning —
// CreateSkill should still print an unqualified success.
func TestCreateSkill_MissingAgentsRCIsLegitimateAbsence(t *testing.T) {
	setupProjectScopeSkill(t, "freshproj")

	stdout := captureStdout(t, func() {
		if err := CreateSkill("widget", "freshproj"); err != nil {
			t.Fatalf("CreateSkill: %v", err)
		}
	})

	if !strings.Contains(stdout, "✓") {
		t.Fatalf("expected unqualified success marker, got: %q", stdout)
	}
	if strings.Contains(stdout, "registration did not fully complete") {
		t.Fatalf("missing .agentsrc.json should not trigger a registration warning, got: %q", stdout)
	}
}
