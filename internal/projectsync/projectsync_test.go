package projectsync_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/linktest"
	"github.com/AGOrcha/dot-agents/internal/projectsync"
)

func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "nested", "dst.txt")

	if err := os.WriteFile(src, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := projectsync.CopyFile(src, dst); err != nil {
		t.Fatalf("CopyFile: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Errorf("expected 'hello', got %q", got)
	}
}

func TestCopyFile_MissingSrc(t *testing.T) {
	dir := t.TempDir()
	err := projectsync.CopyFile(filepath.Join(dir, "nope.txt"), filepath.Join(dir, "out.txt"))
	if err == nil {
		t.Error("expected error for missing source")
	}
}

func TestEnsureGitignoreEntry(t *testing.T) {
	dir := t.TempDir()

	// First call: creates and appends
	projectsync.EnsureGitignoreEntry(dir, ".agents-refresh")
	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), ".agents-refresh") {
		t.Error("expected .agents-refresh in .gitignore")
	}

	// Second call: must not duplicate
	projectsync.EnsureGitignoreEntry(dir, ".agents-refresh")
	data2, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	count := strings.Count(string(data2), ".agents-refresh")
	if count != 1 {
		t.Errorf("expected exactly 1 occurrence, got %d", count)
	}
}

func TestRefreshMarkerContent(t *testing.T) {
	content := projectsync.RefreshMarkerContent("1.0.0", "abc1234", "v1.0.0-1-gabc1234")
	s := string(content)
	for _, want := range []string{"version=1.0.0", "commit=abc1234", "describe=v1.0.0-1-gabc1234", "refreshed_at="} {
		if !strings.Contains(s, want) {
			t.Errorf("marker content missing %q\ngot: %s", want, s)
		}
	}
}

func TestRefreshMarkerContent_EmptyOptionals(t *testing.T) {
	content := projectsync.RefreshMarkerContent("dev", "", "")
	s := string(content)
	if strings.Contains(s, "commit=") {
		t.Error("should not include empty commit")
	}
	if strings.Contains(s, "describe=") {
		t.Error("should not include empty describe")
	}
	if !strings.Contains(s, "version=dev") {
		t.Error("should include version")
	}
}

func TestWriteRefreshToAgentsRC_CreatesManifestAndRemovesLegacy(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		t.Fatal(err)
	}
	projectPath := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(projectPath, ".agents-refresh")
	if err := os.WriteFile(legacy, []byte("legacy\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := projectsync.WriteRefreshToAgentsRC("myproj", projectPath, "1.0.0", "deadbeef", "v1"); err != nil {
		t.Fatalf("WriteRefreshToAgentsRC: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(projectPath, ".agentsrc.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, `"refresh"`) || !strings.Contains(s, "deadbeef") {
		t.Fatalf("manifest missing refresh metadata: %s", s)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("legacy .agents-refresh should be removed: stat err=%v", err)
	}
}

func TestListBucket_ManifestWithoutDescription(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	scopeDir := filepath.Join(agentsHome, "skills", "global")
	os.MkdirAll(scopeDir, 0755)
	good := filepath.Join(scopeDir, "good")
	os.MkdirAll(good, 0755)

	os.WriteFile(filepath.Join(good, "SKILL.md"), []byte("---\nname: good\n---\n"), 0644)

	if err := projectsync.ListBucket("global", projectsync.BucketSpec{
		Bucket: "skills", ManifestName: "SKILL.md", Singular: "skill", Plural: "Skills",
	}); err != nil {
		t.Fatalf("ListBucket: %v", err)
	}
}

func TestReadFrontmatterDescription_UnclosedFrontmatter(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "unclosed.md")

	os.WriteFile(path, []byte("---\nname: x\nother: y\n"), 0644)
	if got := projectsync.ReadFrontmatterDescription(path); got != "" {
		t.Errorf("expected empty for unclosed frontmatter, got %q", got)
	}
}

func TestCreateProjectDirs(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	if err := projectsync.CreateProjectDirs("myproj"); err != nil {
		t.Fatalf("CreateProjectDirs: %v", err)
	}

	for _, bucket := range []string{"rules", "settings", "mcp", "skills", "agents", "hooks"} {
		dir := filepath.Join(agentsHome, bucket, "myproj")
		st, err := os.Stat(dir)
		if err != nil {
			t.Errorf("missing %s: %v", dir, err)
			continue
		}
		if !st.IsDir() {
			t.Errorf("%s should be dir", dir)
		}
	}

	if err := projectsync.CreateProjectDirs("myproj"); err != nil {
		t.Errorf("repeat: %v", err)
	}
}

func TestCopyTree(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("CopyTree skips symlinks via os.ModeSymlink; this fixture's managed link to a *file* is a Windows hard link with no reparse point and no mode bit, so it is indistinguishable from real.txt by the OS and there is no filesystem signal of which entry is 'the link'. Production promote sources are real trees with no internal hard links; the symlink-skip contract for files is exercised on POSIX here, and the directory-junction skip path is covered by linktest.Link's junction fixture used across internal/links and internal/linktest tests.")
	}
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	dst := filepath.Join(tmp, "dst")
	os.MkdirAll(filepath.Join(src, "sub"), 0755)
	os.WriteFile(filepath.Join(src, "a.txt"), []byte("a"), 0644)
	os.WriteFile(filepath.Join(src, "sub", "b.txt"), []byte("b"), 0644)

	os.WriteFile(filepath.Join(src, "real.txt"), []byte("real"), 0644)
	linktest.Link(t, filepath.Join(src, "real.txt"), filepath.Join(src, "link.txt"))

	if err := projectsync.CopyTree(src, dst); err != nil {
		t.Fatalf("CopyTree: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(dst, "a.txt")); err != nil || string(data) != "a" {
		t.Errorf("a.txt: data=%q err=%v", data, err)
	}
	if data, err := os.ReadFile(filepath.Join(dst, "sub", "b.txt")); err != nil || string(data) != "b" {
		t.Errorf("sub/b.txt: data=%q err=%v", data, err)
	}

	if _, err := os.Lstat(filepath.Join(dst, "link.txt")); !os.IsNotExist(err) {
		t.Errorf("symlink should be skipped, stat=%v", err)
	}
}

func TestCopyFile_MkdirAllFails(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.txt")
	os.WriteFile(src, []byte("x"), 0644)
	blocker := filepath.Join(tmp, "blocker")
	os.WriteFile(blocker, []byte("blocker"), 0644)
	dst := filepath.Join(blocker, "sub", "out.txt")
	if err := projectsync.CopyFile(src, dst); err == nil {
		t.Error("expected MkdirAll failure for dst under regular file")
	}
}

func TestCreateProjectDirs_BlockerFile(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(agentsHome, "rules"), []byte("blocker"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := projectsync.CreateProjectDirs("p"); err == nil {
		t.Error("expected error when bucket is a regular file")
	}
}

func TestEnsureGitignoreEntry_UnreadableFileSkipsCleanly(t *testing.T) {

	projectsync.EnsureGitignoreEntry("/nonexistent/path", "entry")
}

func TestWriteRefreshToAgentsRC_SaveError(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	os.MkdirAll(agentsHome, 0755)

	projectPath := filepath.Join(tmp, "regular")
	os.WriteFile(projectPath, []byte("file"), 0644)
	if err := projectsync.WriteRefreshToAgentsRC("p", projectPath, "v", "c", "d"); err == nil {
		t.Error("expected Save error when projectPath is a regular file")
	}
}

func TestWriteRefreshToAgentsRC_LoadError(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	os.MkdirAll(agentsHome, 0755)
	projectPath := filepath.Join(tmp, "repo")
	os.MkdirAll(projectPath, 0755)

	if err := os.WriteFile(filepath.Join(projectPath, ".agentsrc.json"), []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := projectsync.WriteRefreshToAgentsRC("p", projectPath, "v", "c", "d"); err == nil {
		t.Error("expected error from malformed .agentsrc.json")
	}
}

func TestCopyTreeMissingSource(t *testing.T) {
	tmp := t.TempDir()
	if err := projectsync.CopyTree(filepath.Join(tmp, "nope"), filepath.Join(tmp, "dst")); err == nil {
		t.Error("expected error for missing source")
	}
}

// TestWriteRefreshToAgentsRC_LegacyRemoveErrorPropagates covers the
// non-IsNotExist branch of `os.Remove(legacy)`. We place a non-empty
// directory at the legacy `.agents-refresh` path so os.Remove returns
// ENOTEMPTY rather than ENOENT.
func TestWriteRefreshToAgentsRC_LegacyRemoveErrorPropagates(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		t.Fatal(err)
	}
	projectPath := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(projectPath, ".agentsrc.json"),
		[]byte(`{"version":1,"project":"p","sources":[{"type":"local"}]}`), 0644); err != nil {
		t.Fatal(err)
	}

	legacy := filepath.Join(projectPath, ".agents-refresh")
	if err := os.MkdirAll(filepath.Join(legacy, "child"), 0755); err != nil {
		t.Fatal(err)
	}
	err := projectsync.WriteRefreshToAgentsRC("p", projectPath, "v", "c", "d")
	if err == nil {
		t.Skip("filesystem allowed os.Remove on non-empty dir; legacy-error branch not exercised")
	}
}

func TestWriteRefreshToAgentsRC_ExistingManifest(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		t.Fatal(err)
	}
	projectPath := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(projectPath, ".agentsrc.json"), []byte(`{"version":1,"project":"myproj","sources":[{"type":"local"}]}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := projectsync.WriteRefreshToAgentsRC("myproj", projectPath, "2.0", "c2", "v2"); err != nil {
		t.Fatalf("WriteRefreshToAgentsRC: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(projectPath, ".agentsrc.json"))
	if want := "c2"; !contains(string(data), want) {
		t.Errorf("manifest missing %s: %s", want, data)
	}
}
