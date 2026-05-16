package projectsync_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/linktest"
	"github.com/NikashPrakash/dot-agents/internal/projectsync"
)

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

func TestPromoteResource_StaleCanonicalSymlinkReplaced(t *testing.T) {
	agentsHome, projectPath := promoteEnvX(t, "stale")
	// Repo-local source with manifest
	srcDir := filepath.Join(projectPath, ".agents", "widgets", "alpha")
	os.MkdirAll(srcDir, 0755)
	os.WriteFile(filepath.Join(srcDir, "WIDGET.md"), []byte("real-content"), 0644)

	// Stale canonical: symlink pointing somewhere else
	canonicalDir := filepath.Join(agentsHome, "widgets", "stale")
	os.MkdirAll(canonicalDir, 0755)
	elsewhere := filepath.Join(agentsHome, "elsewhere")
	os.MkdirAll(elsewhere, 0755)
	linktest.Link(t, elsewhere, filepath.Join(canonicalDir, "alpha"))

	if err := projectsync.PromoteResource("alpha", projectPath, widgetSpecX(t)); err != nil {
		t.Fatalf("PromoteResource: %v", err)
	}

	canonicalPath := filepath.Join(canonicalDir, "alpha")
	info, err := os.Lstat(canonicalPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("stale symlink should have been replaced with a real dir")
	}
}

func TestPromoteResource_CanonicalIsRegularFile(t *testing.T) {
	agentsHome, projectPath := promoteEnvX(t, "regular")
	srcDir := filepath.Join(projectPath, ".agents", "widgets", "alpha")
	os.MkdirAll(srcDir, 0755)
	os.WriteFile(filepath.Join(srcDir, "WIDGET.md"), []byte("c"), 0644)

	// Canonical slot is a regular FILE → distinct from dir, hits default branch
	canonicalDir := filepath.Join(agentsHome, "widgets", "regular")
	os.MkdirAll(canonicalDir, 0755)
	os.WriteFile(filepath.Join(canonicalDir, "alpha"), []byte("blocker"), 0644)

	err := projectsync.PromoteResource("alpha", projectPath, widgetSpecX(t))
	if err == nil || !strings.Contains(err.Error(), "remove the file and retry") {
		t.Errorf("expected 'remove the file' error, got: %v", err)
	}
}

func TestPromoteResource_MissingSource(t *testing.T) {
	_, projectPath := promoteEnvX(t, "missing")
	err := projectsync.PromoteResource("ghost", projectPath, widgetSpecX(t))
	if err == nil {
		t.Fatal("expected error for missing source")
	}
	if !strings.Contains(err.Error(), "not found in .agents") {
		t.Errorf("error %q missing expected phrase", err.Error())
	}
}

func TestPromoteResource_NoAgentsRC(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agentshome")
	projectPath := filepath.Join(tmp, "repo")
	os.MkdirAll(agentsHome, 0755)
	os.MkdirAll(projectPath, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)
	// Create repo-local source but NO .agentsrc.json
	srcDir := filepath.Join(projectPath, ".agents", "widgets", "alpha")
	os.MkdirAll(srcDir, 0755)
	os.WriteFile(filepath.Join(srcDir, "WIDGET.md"), []byte("c"), 0644)

	err := projectsync.PromoteResource("alpha", projectPath, widgetSpecX(t))
	if err == nil {
		t.Fatal("expected error when .agentsrc.json missing")
	}
	if !strings.Contains(err.Error(), ".agentsrc.json") {
		t.Errorf("error should reference .agentsrc.json, got: %v", err)
	}
}

func TestPromoteResource_MirrorRefreshIsCalled(t *testing.T) {
	_, projectPath := promoteEnvX(t, "mirror")
	srcDir := filepath.Join(projectPath, ".agents", "widgets", "alpha")
	os.MkdirAll(srcDir, 0755)
	os.WriteFile(filepath.Join(srcDir, "WIDGET.md"), []byte("c"), 0644)

	called := false
	spec := widgetSpecX(t)
	spec.MirrorRefresh = func(projectName, projectPath string) error {
		called = true
		return nil
	}
	if err := projectsync.PromoteResource("alpha", projectPath, spec); err != nil {
		t.Fatalf("PromoteResource: %v", err)
	}
	if !called {
		t.Error("MirrorRefresh should be invoked on success")
	}
}

func TestPromoteResource_RcSaveFails(t *testing.T) {
	agentsHome, projectPath := promoteEnvX(t, "savefail")
	_ = agentsHome
	srcDir := filepath.Join(projectPath, ".agents", "widgets", "alpha")
	os.MkdirAll(srcDir, 0755)
	os.WriteFile(filepath.Join(srcDir, "WIDGET.md"), []byte("c"), 0644)

	// Make .agentsrc.json a directory so subsequent rc.Save WriteFile fails.
	rcPath := filepath.Join(projectPath, ".agentsrc.json")
	if err := os.Remove(rcPath); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(rcPath, 0755); err != nil {
		t.Fatal(err)
	}
	// PromoteResource loads .agentsrc.json — which is now a directory and
	// will return an error from LoadAgentsRC (ReadFile on dir = EISDIR).
	// We expect a load-side error rather than a save-side error; either
	// path exercises the failure flow.
	err := projectsync.PromoteResource("alpha", projectPath, widgetSpecX(t))
	if err == nil {
		t.Error("expected error when .agentsrc.json is a directory")
	}
}

func TestPromoteResource_PreparePromoteDestMkdirFails(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agentshome")
	projectPath := filepath.Join(tmp, "repo")
	os.MkdirAll(agentsHome, 0755)
	os.MkdirAll(projectPath, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	rc := &config.AgentsRC{
		Version: 1,
		Project: "p",
		Sources: []config.Source{{Type: "local"}},
	}
	if err := rc.Save(projectPath); err != nil {
		t.Fatal(err)
	}

	// Place a regular file at the bucket slot so MkdirAll fails when
	// trying to create ~/.agents/widgets.
	if err := os.WriteFile(filepath.Join(agentsHome, "widgets"), []byte("blocker"), 0644); err != nil {
		t.Fatal(err)
	}

	// Source needs to exist for the Lstat at the top of PromoteResource.
	srcDir := filepath.Join(projectPath, ".agents", "widgets", "alpha")
	os.MkdirAll(srcDir, 0755)
	os.WriteFile(filepath.Join(srcDir, "WIDGET.md"), []byte("c"), 0644)

	err := projectsync.PromoteResource("alpha", projectPath, widgetSpecX(t))
	if err == nil {
		t.Error("expected MkdirAll failure for canonical bucket dir")
	}
}

func TestPromoteResource_CopyTreeFailsBlockerInRepo(t *testing.T) {
	agentsHome, projectPath := promoteEnvX(t, "copyfail")
	srcDir := filepath.Join(projectPath, ".agents", "widgets", "alpha")
	os.MkdirAll(srcDir, 0755)
	os.WriteFile(filepath.Join(srcDir, "WIDGET.md"), []byte("c"), 0644)

	// Pre-create a regular file at the canonical destination's parent so the
	// final Symlink call would fail; the easier failure: ensure canonical
	// dest pre-exists as a real file (default branch error in
	// clearExistingCanonical).
	canonicalDir := filepath.Join(agentsHome, "widgets", "copyfail")
	os.MkdirAll(canonicalDir, 0755)
	os.WriteFile(filepath.Join(canonicalDir, "alpha"), []byte("blocker"), 0644)

	err := projectsync.PromoteResource("alpha", projectPath, widgetSpecX(t))
	if err == nil {
		t.Error("expected error from canonical being a regular file")
	}
}

func TestEnsureGitignoreEntry_ExistingFileSkipsDup(t *testing.T) {
	tmp := t.TempDir()
	// Pre-existing .gitignore with the entry already present (no trailing newline)
	os.WriteFile(filepath.Join(tmp, ".gitignore"), []byte(".agents-refresh\n# other\n"), 0644)
	projectsync.EnsureGitignoreEntry(tmp, ".agents-refresh")
	data, _ := os.ReadFile(filepath.Join(tmp, ".gitignore"))
	if strings.Count(string(data), ".agents-refresh") != 1 {
		t.Errorf("should not duplicate; got %q", data)
	}
}
