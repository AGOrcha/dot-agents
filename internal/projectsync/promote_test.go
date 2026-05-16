package projectsync_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/linktest"
	"github.com/NikashPrakash/dot-agents/internal/projectsync"
)

// promoteEnv constructs a self-contained AGENTS_HOME + repo with a
// .agentsrc.json so PromoteResource has the same prerequisites it would in a
// real CLI invocation.
func promoteEnv(t *testing.T, projectName string) (agentsHome, projectPath string) {
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
		t.Fatalf("rc.Save: %v", err)
	}
	return agentsHome, projectPath
}

func writeRepoLocal(t *testing.T, projectPath, bucket, name, manifest string) {
	t.Helper()
	dir := filepath.Join(projectPath, ".agents", bucket, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, manifest),
		[]byte("---\nname: "+name+"\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}
}

func widgetSpec(t *testing.T) projectsync.PromoteSpec {
	t.Helper()
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

func TestPromoteResource_ConvertsRepoLocalToManagedSymlink(t *testing.T) {
	agentsHome, projectPath := promoteEnv(t, "myproj")
	writeRepoLocal(t, projectPath, "widgets", "alpha", "WIDGET.md")

	if err := projectsync.PromoteResource("alpha", projectPath, widgetSpec(t)); err != nil {
		t.Fatalf("PromoteResource: %v", err)
	}

	canonical := filepath.Join(agentsHome, "widgets", "myproj", "alpha")
	if _, err := os.Stat(filepath.Join(canonical, "WIDGET.md")); err != nil {
		t.Errorf("canonical manifest missing: %v", err)
	}

	repoLocal := filepath.Join(projectPath, ".agents", "widgets", "alpha")
	target, err := os.Readlink(repoLocal)
	if err != nil {
		t.Fatalf("repo-local should be a symlink, got: %v", err)
	}
	if target != canonical {
		t.Errorf("repo-local points to %q; want %q", target, canonical)
	}
}

func TestPromoteResource_IdempotentOnExistingSymlink(t *testing.T) {
	agentsHome, projectPath := promoteEnv(t, "myproj")
	canonical := filepath.Join(agentsHome, "widgets", "myproj", "alpha")
	if err := os.MkdirAll(canonical, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(canonical, "WIDGET.md"),
		[]byte("---\nname: alpha\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}
	repoBucket := filepath.Join(projectPath, ".agents", "widgets")
	if err := os.MkdirAll(repoBucket, 0755); err != nil {
		t.Fatal(err)
	}
	repoLocal := filepath.Join(repoBucket, "alpha")
	linktest.Link(t, canonical, repoLocal)

	if err := projectsync.PromoteResource("alpha", projectPath, widgetSpec(t)); err != nil {
		t.Fatalf("expected idempotent success, got: %v", err)
	}
}

func TestPromoteResource_ErrorMissingManifest(t *testing.T) {
	_, projectPath := promoteEnv(t, "noman")
	dir := filepath.Join(projectPath, ".agents", "widgets", "empty")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}

	err := projectsync.PromoteResource("empty", projectPath, widgetSpec(t))
	if err == nil {
		t.Fatal("expected error when manifest missing")
	}
	if !strings.Contains(err.Error(), "WIDGET.md") {
		t.Errorf("error %q missing manifest name", err.Error())
	}
}

func TestPromoteResource_ErrorExistingRealDirIncludesHint(t *testing.T) {
	agentsHome, projectPath := promoteEnv(t, "clash")
	writeRepoLocal(t, projectPath, "widgets", "alpha", "WIDGET.md")

	dest := filepath.Join(agentsHome, "widgets", "clash", "alpha")
	if err := os.MkdirAll(dest, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "WIDGET.md"),
		[]byte("---\nname: x\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}

	spec := widgetSpec(t)
	spec.ExistingRealDirHint = "; use --force to overwrite"
	err := projectsync.PromoteResource("alpha", projectPath, spec)
	if err == nil {
		t.Fatal("expected error for real canonical dir without force")
	}
	if !strings.Contains(err.Error(), "real directory") {
		t.Errorf("error %q missing 'real directory' substring", err.Error())
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("error %q missing '--force' hint", err.Error())
	}
}

func TestPromoteResource_ForceOverwritesCanonicalDir(t *testing.T) {
	agentsHome, projectPath := promoteEnv(t, "force")
	writeRepoLocal(t, projectPath, "widgets", "alpha", "WIDGET.md")

	dest := filepath.Join(agentsHome, "widgets", "force", "alpha")
	if err := os.MkdirAll(dest, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "WIDGET.md"),
		[]byte("stale-canonical"), 0644); err != nil {
		t.Fatal(err)
	}

	spec := widgetSpec(t)
	spec.Force = true
	if err := projectsync.PromoteResource("alpha", projectPath, spec); err != nil {
		t.Fatalf("PromoteResource with force: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dest, "WIDGET.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "stale-canonical") {
		t.Errorf("expected stale canonical to be replaced, still got: %q", got)
	}
}

func TestPromoteResource_ErrorMispointedSymlink(t *testing.T) {
	agentsHome, projectPath := promoteEnv(t, "mispoint")
	repoBucket := filepath.Join(projectPath, ".agents", "widgets")
	if err := os.MkdirAll(repoBucket, 0755); err != nil {
		t.Fatal(err)
	}
	wrong := filepath.Join(agentsHome, "wrong-target")
	if err := os.MkdirAll(wrong, 0755); err != nil {
		t.Fatal(err)
	}
	linktest.Link(t, wrong, filepath.Join(repoBucket, "alpha"))

	err := projectsync.PromoteResource("alpha", projectPath, widgetSpec(t))
	if err == nil {
		t.Fatal("expected error for mispointed symlink")
	}
	if !strings.Contains(err.Error(), "already a symlink but points to") {
		t.Errorf("error %q missing 'already a symlink but points to'", err.Error())
	}
}

func TestPromoteResource_ErrorNoProjectName(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agentshome")
	projectPath := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	rc := &config.AgentsRC{Version: 1, Sources: []config.Source{{Type: "local"}}}
	if err := rc.Save(projectPath); err != nil {
		t.Fatal(err)
	}
	writeRepoLocal(t, projectPath, "widgets", "alpha", "WIDGET.md")

	err := projectsync.PromoteResource("alpha", projectPath, widgetSpec(t))
	if err == nil || !strings.Contains(err.Error(), "no project name set") {
		t.Errorf("expected 'no project name set' error, got %v", err)
	}
}

func TestCopyTree_CopiesFilesAndDirsSkipsSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("CopyTree skips symlinks via os.ModeSymlink; this fixture's managed link to a *file* is a Windows hard link with no reparse point and no mode bit, so it is indistinguishable from a.txt by the OS. Production promote sources are real trees with no internal hard links; the file symlink-skip contract is exercised on POSIX here, and the directory-junction skip path is covered by linktest.Link's junction fixture across internal/links and internal/linktest tests.")
	}
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "out")

	if err := os.MkdirAll(filepath.Join(src, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "nested", "b.txt"), []byte("b"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(src, "nested", "skipme")
	linktest.Link(t, filepath.Join(src, "a.txt"), link)

	if err := projectsync.CopyTree(src, dst); err != nil {
		t.Fatalf("CopyTree: %v", err)
	}

	if data, err := os.ReadFile(filepath.Join(dst, "a.txt")); err != nil || string(data) != "a" {
		t.Errorf("a.txt missing or wrong content: %q err=%v", data, err)
	}
	if data, err := os.ReadFile(filepath.Join(dst, "nested", "b.txt")); err != nil || string(data) != "b" {
		t.Errorf("nested/b.txt missing or wrong content: %q err=%v", data, err)
	}
	if _, err := os.Lstat(filepath.Join(dst, "nested", "skipme")); !os.IsNotExist(err) {
		t.Errorf("symlink should have been skipped, got err=%v", err)
	}
}
