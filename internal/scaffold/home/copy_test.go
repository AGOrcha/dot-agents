package home

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyMissingStarterAssetsCopiesStarterBundle(t *testing.T) {
	tmp := t.TempDir()
	if err := CopyMissingStarterAssets(tmp); err != nil {
		t.Fatalf("CopyMissingStarterAssets: %v", err)
	}
	for _, rel := range []string{
		".gitignore",
		"README.md",
		"rules/global/rules.mdc",
		"settings/global/claude-code.json",
		"skills/global/agent-start/SKILL.md",
		"skills/global/review-pr/templates/review-output.md",
	} {
		if _, err := os.Stat(filepath.Join(tmp, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("expected %s: %v", rel, err)
		}
	}
}

func TestCopyMissingStarterAssetsPreservesExistingFiles(t *testing.T) {
	tmp := t.TempDir()
	skill := filepath.Join(tmp, "skills", "global", "agent-start", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skill), 0755); err != nil {
		t.Fatal(err)
	}
	want := "# custom\n"
	if err := os.WriteFile(skill, []byte(want), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CopyMissingStarterAssets(tmp); err != nil {
		t.Fatalf("CopyMissingStarterAssets: %v", err)
	}
	got, err := os.ReadFile(skill)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("starter skill overwritten:\n got: %s\nwant: %s", string(got), want)
	}
}
