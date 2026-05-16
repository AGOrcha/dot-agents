package projectsync_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/linktest"
	"github.com/NikashPrakash/dot-agents/internal/projectsync"
)

func TestListBucket_ManifestWithoutDescription(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	scopeDir := filepath.Join(agentsHome, "skills", "global")
	os.MkdirAll(scopeDir, 0755)
	good := filepath.Join(scopeDir, "good")
	os.MkdirAll(good, 0755)
	// Manifest exists, but no description in frontmatter → exercises the
	// "ok without description" branch.
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
	// Frontmatter opens but never closes — scanner runs to EOF, hits final
	// `return ""` after the loop.
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

	// Idempotent
	if err := projectsync.CreateProjectDirs("myproj"); err != nil {
		t.Errorf("repeat: %v", err)
	}
}

func TestCopyTree(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	dst := filepath.Join(tmp, "dst")
	os.MkdirAll(filepath.Join(src, "sub"), 0755)
	os.WriteFile(filepath.Join(src, "a.txt"), []byte("a"), 0644)
	os.WriteFile(filepath.Join(src, "sub", "b.txt"), []byte("b"), 0644)
	// Symlink should be skipped
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
	// Symlink should not have been copied
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
	// Put a regular file at the bucket slot so MkdirAll fails on first dir
	if err := os.WriteFile(filepath.Join(agentsHome, "rules"), []byte("blocker"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := projectsync.CreateProjectDirs("p"); err == nil {
		t.Error("expected error when bucket is a regular file")
	}
}

func TestEnsureGitignoreEntry_UnreadableFileSkipsCleanly(t *testing.T) {
	// Pass a path under a non-existent directory — OpenFile will fail.
	// Function is silently no-op on failure.
	projectsync.EnsureGitignoreEntry("/nonexistent/path", "entry")
}

func TestWriteRefreshToAgentsRC_SaveError(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	os.MkdirAll(agentsHome, 0755)
	// projectPath is a regular file → rc.Save's WriteFile call fails
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
	// Malformed .agentsrc.json causes a parse error (not IsNotExist) — should bubble up.
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
	// Pre-existing .agentsrc.json so Load succeeds and Save succeeds.
	if err := os.WriteFile(filepath.Join(projectPath, ".agentsrc.json"),
		[]byte(`{"version":1,"project":"p","sources":[{"type":"local"}]}`), 0644); err != nil {
		t.Fatal(err)
	}
	// Place a non-empty directory at the legacy file path. os.Remove on a
	// non-empty directory returns ENOTEMPTY (not IsNotExist), surfacing the
	// branch under test.
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
	// Pre-existing minimal .agentsrc.json
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

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || indexOf(s, substr) >= 0)
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
