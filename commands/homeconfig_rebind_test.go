package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/config"
)

// TestRegisterAddedProject_RebindPreservesIdentity models the machine-B rebind:
// the project's portable identity (repo_id) is already in the SYNCED registry
// but has no local binding. `da add` must bind the machine-local path WITHOUT
// recomputing/overwriting the synced repo_id — re-deriving it from this
// machine's git remotes would corrupt the synced identity registry (Fix 1).
func TestRegisterAddedProject_RebindPreservesIdentity(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)

	// A synced identity-only config.json (no binding table on this machine).
	synced := `{"version":2,"projects":{"svc":{"repo_id":"github.com/acme/svc"}}}`
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(synced), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ProjectRepoID("svc") != "github.com/acme/svc" {
		t.Fatalf("precondition: synced repo_id not loaded")
	}

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	projPath := filepath.Join(home, "checkout", "svc")
	if err := registerAddedProject(cfg, "svc", projPath); err != nil {
		t.Fatalf("registerAddedProject: %v", err)
	}

	reloaded, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.ProjectRepoID("svc"); got != "github.com/acme/svc" {
		t.Errorf("rebind OVERWROTE synced repo_id: got %q, want github.com/acme/svc", got)
	}
	if got := reloaded.GetProjectPath("svc"); got != filepath.Clean(projPath) {
		t.Errorf("rebind did not set machine-local path: got %q", got)
	}
}

// TestRegisterAddedProject_NewProjectDerivesIdentity asserts a genuinely new
// project (absent from the registry) still goes through AddProject so its
// identity is derived — the rebind branch must not swallow the new-project case.
func TestRegisterAddedProject_NewProjectDerivesIdentity(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	if err := registerAddedProject(cfg, "fresh", filepath.Join(home, "fresh")); err != nil {
		t.Fatalf("registerAddedProject: %v", err)
	}
	reloaded, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.IsProjectKnown("fresh") {
		t.Error("new project not registered in identity registry")
	}
	if !reloaded.IsProjectBound("fresh") {
		t.Error("new project not bound on this machine")
	}
}
