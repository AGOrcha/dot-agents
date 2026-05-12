package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/config"
)

func withTestFlags(t *testing.T, flags GlobalFlags) {
	t.Helper()
	previous := Flags
	Flags = flags
	t.Cleanup(func() {
		Flags = previous
	})
}

func TestScaffoldWorkflowAssetsCreatesStarterHooksAndContextDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	if err := os.MkdirAll(filepath.Join(config.AgentsHome(), "hooks", "global"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := scaffoldWorkflowAssets(config.AgentsHome()); err != nil {
		t.Fatalf("scaffoldWorkflowAssets: %v", err)
	}

	for _, rel := range []string{
		"context",
		"hooks/global/session-orient/HOOK.yaml",
		"hooks/global/session-orient/orient.sh",
		"hooks/global/session-capture/HOOK.yaml",
		"hooks/global/guard-commands/HOOK.yaml",
		"hooks/global/secret-scan/HOOK.yaml",
		"hooks/global/auto-format/HOOK.yaml",
	} {
		path := filepath.Join(config.AgentsHome(), filepath.FromSlash(rel))
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s to exist: %v", path, err)
		}
	}
}

func TestScaffoldWorkflowAssetsPreservesExistingHookBundle(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))

	customBundleDir := filepath.Join(config.AgentsHome(), "hooks", "global", "session-orient")
	if err := os.MkdirAll(customBundleDir, 0755); err != nil {
		t.Fatal(err)
	}
	customContent := "name: session-orient\ndescription: custom\n"
	if err := os.WriteFile(filepath.Join(customBundleDir, "HOOK.yaml"), []byte(customContent), 0644); err != nil {
		t.Fatal(err)
	}

	if err := scaffoldWorkflowAssets(config.AgentsHome()); err != nil {
		t.Fatalf("scaffoldWorkflowAssets: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(customBundleDir, "HOOK.yaml"))
	if err != nil {
		t.Fatalf("reading preserved hook bundle: %v", err)
	}
	if string(got) != customContent {
		t.Fatalf("existing hook bundle was overwritten:\n got: %s\nwant: %s", string(got), customContent)
	}
}

func TestScaffoldStarterHomeAssetsSeedsStarterSkillsAndGitignore(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))

	if err := scaffoldStarterHomeAssets(config.AgentsHome()); err != nil {
		t.Fatalf("scaffoldStarterHomeAssets: %v", err)
	}

	gitignore, err := os.ReadFile(filepath.Join(config.AgentsHome(), ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(gitignore), "context/") {
		t.Fatalf(".gitignore missing context/:\n%s", string(gitignore))
	}
	for _, rel := range []string{
		"README.md",
		"rules/global/rules.mdc",
		"settings/global/claude-code.json",
		"skills/global/agent-start/SKILL.md",
		"skills/global/review-delta/instructions/workflow.md",
	} {
		if _, err := os.Stat(filepath.Join(config.AgentsHome(), filepath.FromSlash(rel))); err != nil {
			t.Fatalf("expected %s to exist: %v", rel, err)
		}
	}
}
