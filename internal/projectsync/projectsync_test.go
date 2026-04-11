package projectsync_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/projectsync"
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
