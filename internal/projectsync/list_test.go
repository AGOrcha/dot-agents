package projectsync_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/projectsync"
	"github.com/AGOrcha/dot-agents/internal/testutil"
)

func TestReadFrontmatterDescription_QuotedAndUnquoted(t *testing.T) {
	tmp := t.TempDir()
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{"unquoted", "---\nname: x\ndescription: a clear sentence\n---\n", "a clear sentence"},
		{"double-quoted", "---\nname: x\ndescription: \"quoted desc\"\n---\n", "quoted desc"},
		{"single-quoted", "---\nname: x\ndescription: 'single desc'\n---\n", "single desc"},
		{"empty-description", "---\nname: x\ndescription:\n---\n", ""},
		{"no-frontmatter", "# heading only\n", ""},
		{"missing-description", "---\nname: x\n---\n", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(tmp, tc.name+".md")
			if err := os.WriteFile(p, []byte(tc.content), 0644); err != nil {
				t.Fatal(err)
			}
			got := projectsync.ReadFrontmatterDescription(p)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestReadFrontmatterDescription_MissingFile(t *testing.T) {
	got := projectsync.ReadFrontmatterDescription(filepath.Join(t.TempDir(), "nope.md"))
	if got != "" {
		t.Errorf("expected empty string for missing file, got %q", got)
	}
}

func TestListBucket_NoBucketDirIsNoOp(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))

	err := projectsync.ListBucket("global", projectsync.BucketSpec{
		Bucket:       "skills",
		ManifestName: "SKILL.md",
		Singular:     "skill",
		Plural:       "Skills",
	})
	if err != nil {
		t.Errorf("expected nil error when bucket dir missing, got %v", err)
	}
}

func TestListBucket_SkipsNonDirAndCountsValid(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	scopeDir := filepath.Join(agentsHome, "skills", "global")
	if err := os.MkdirAll(scopeDir, 0755); err != nil {
		t.Fatal(err)
	}

	good := filepath.Join(scopeDir, "good")
	if err := os.MkdirAll(good, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(good, "SKILL.md"),
		[]byte("---\nname: good\ndescription: a good skill\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}

	bare := filepath.Join(scopeDir, "bare")
	if err := os.MkdirAll(bare, 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(scopeDir, "stray.txt"), []byte("ignore"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := projectsync.ListBucket("global", projectsync.BucketSpec{
		Bucket:       "skills",
		ManifestName: "SKILL.md",
		Singular:     "skill",
		Plural:       "Skills",
	}); err != nil {
		t.Fatalf("ListBucket: %v", err)
	}
}

// TestListBucket_ReadDirRealErrorSurfaces exercises the should-be-LOUD fix:
// a real ReadDir failure on an existing bucket dir must not be reported to
// the user identically to "empty bucket."
func TestListBucket_ReadDirRealErrorSurfaces(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	scopeDir := filepath.Join(agentsHome, "skills", "global")
	if err := os.MkdirAll(scopeDir, 0755); err != nil {
		t.Fatal(err)
	}
	testutil.MakeDirUnreadable(t, scopeDir)

	err := projectsync.ListBucket("global", projectsync.BucketSpec{
		Bucket:       "skills",
		ManifestName: "SKILL.md",
		Singular:     "skill",
		Plural:       "Skills",
	})
	if err == nil {
		t.Fatal("want a surfaced error for a real ReadDir failure, not the 'No skills found' no-op")
	}
}
