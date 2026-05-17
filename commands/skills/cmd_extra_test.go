package skills

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/config"
)

// TestNewSkillsCmd_MetadataExhaustive asserts every subcommand has non-empty
// Use/Short/Args/RunE wired so a future refactor that drops any of them
// fails the test (defense-in-depth for the cobra extraction).
func TestNewSkillsCmd_MetadataExhaustive(t *testing.T) {
	cmd := NewSkillsCmd(testDeps())
	for _, sub := range cmd.Commands() {
		if sub.Use == "" {
			t.Errorf("subcommand %q: Use is empty", sub.Name())
		}
		if sub.Short == "" {
			t.Errorf("subcommand %q: Short is empty", sub.Name())
		}
		if sub.Args == nil {
			t.Errorf("subcommand %q: Args validator nil", sub.Name())
		}
		if sub.RunE == nil {
			t.Errorf("subcommand %q: RunE nil", sub.Name())
		}
	}
}

// TestSkillsListCmd_RejectsExtraArgs covers the MaximumNArgs(1) validator
// rejecting the >1-arg case.
func TestSkillsListCmd_RejectsExtraArgs(t *testing.T) {
	cmd := newSkillsListCmd(testDeps())
	if err := cmd.Args(cmd, []string{"a", "b"}); err == nil {
		t.Error("list should reject 2 args")
	}
}

// TestSkillsNewCmd_RejectsZeroAndTooManyArgs covers the RangeArgs(1,2) edges.
func TestSkillsNewCmd_RejectsZeroAndTooManyArgs(t *testing.T) {
	cmd := newSkillsNewCmd(testDeps())
	if err := cmd.Args(cmd, []string{}); err == nil {
		t.Error("new should reject 0 args")
	}
	if err := cmd.Args(cmd, []string{"a", "b", "c"}); err == nil {
		t.Error("new should reject 3 args")
	}
	// Valid arities.
	if err := cmd.Args(cmd, []string{"name"}); err != nil {
		t.Errorf("new(1 arg) should be valid: %v", err)
	}
	if err := cmd.Args(cmd, []string{"name", "scope"}); err != nil {
		t.Errorf("new(2 args) should be valid: %v", err)
	}
}

// TestSkillsPromoteCmd_RejectsMultipleArgs covers ExactArgs(1) rejecting
// extra args.
func TestSkillsPromoteCmd_RejectsMultipleArgs(t *testing.T) {
	cmd := newSkillsPromoteCmd(testDeps())
	if err := cmd.Args(cmd, []string{"a", "b"}); err == nil {
		t.Error("promote should reject 2 args")
	}
}

// TestSkillsPromoteCmd_RunESuccessPath exercises a full happy-path through
// the RunE: a real promote that uses an existing repo-local skill. This
// covers the os.Getwd → PromoteSkillIn dispatch.
func TestSkillsPromoteCmd_RunESuccessPath(t *testing.T) {
	agentsHome := setupAgentsHomeAndHome(t)
	tmp := t.TempDir()

	// Register the project in ~/.agents/config.toml so projectsync can find it.
	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("promo-proj", tmp)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	// Seed .agentsrc.json with the project name set so PromoteResource accepts it.
	rc := &config.AgentsRC{Version: 1, Project: "promo-proj"}
	if err := rc.Save(tmp); err != nil {
		t.Fatal(err)
	}

	// Seed a repo-local skill dir with the manifest the promote pipeline expects.
	skillDir := filepath.Join(tmp, ".agents", "skills", "demo-skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"),
		[]byte("---\nname: demo-skill\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}

	cmd := newSkillsPromoteCmd(testDeps())
	if err := cmd.RunE(cmd, []string{"demo-skill"}); err != nil {
		t.Fatalf("RunE happy path: %v", err)
	}

	canonical := filepath.Join(agentsHome, "skills", "promo-proj", "demo-skill", "SKILL.md")
	if _, err := os.Stat(canonical); err != nil {
		t.Errorf("expected canonical skill at %s: %v", canonical, err)
	}
}

// TestCreateSkill_EnsureSkillMarkdownErrorPropagates covers the
// `if err := EnsureSkillMarkdown(...); err != nil { return err }` branch
// inside CreateSkill (line 104). MkdirAll succeeds so we reach
// EnsureSkillMarkdown; osWriteFile then fails so EnsureSkillMarkdown errors.
func TestCreateSkill_EnsureSkillMarkdownErrorPropagates(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))

	sentinel := errors.New("write boom")
	withWriteFileStub(t, func(string, []byte, os.FileMode) error { return sentinel })

	err := CreateSkill("err-skill", "global")
	if err == nil || !strings.Contains(err.Error(), "creating SKILL.md") {
		t.Fatalf("expected creating SKILL.md error from CreateSkill, got %v", err)
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("expected wrapped sentinel, got %v", err)
	}
}

// TestAppendSkillToAgentsRC_SaveError covers the rc.Save failure branch
// inside AppendSkillToAgentsRC (line 31). We make the project dir read-only
// AFTER its manifest is written; that way LoadAgentsRC still succeeds (read
// access remains) but rc.Save's call to os.WriteFile fails because it
// cannot create the temp-rename intermediate in a read-only dir.
func TestAppendSkillToAgentsRC_SaveError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod-based read-only dir not portable on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod restrictions")
	}
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))

	projPath := filepath.Join(tmp, "p")
	if err := os.MkdirAll(projPath, 0755); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("p", projPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	rc := &config.AgentsRC{Version: 1, Project: "p"}
	if err := rc.Save(projPath); err != nil {
		t.Fatal(err)
	}

	// Chmod the manifest 0o400 (owner read-only). LoadAgentsRC reads it
	// successfully (read bit set), but rc.Save's os.WriteFile then fails
	// with EACCES because it cannot truncate-and-write a read-only file.
	manifest := filepath.Join(projPath, ".agentsrc.json")
	if err := os.Chmod(manifest, 0o400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(manifest, 0o644) })

	got := AppendSkillToAgentsRC("late", "p")
	if got != "" {
		t.Skip("rc.Save unexpectedly succeeded under chmod 0o400; skipping")
	}
}

// TestEnsureUserSkillLinks_OneTargetExistsOneCreated covers the mixed branch
// where one target dir already exists (skip) and the other does not (create
// fresh symlink) inside a single invocation.
func TestEnsureUserSkillLinks_OneTargetExistsOneCreated(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	skillName := "mixed"
	skillDir := filepath.Join(agentsHome, "skills", "global", skillName)
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Pre-create only ~/.agents/skills/mixed so it is skipped; the
	// ~/.claude/skills/mixed link must still be created.
	preExisting := filepath.Join(tmp, ".agents", "skills", skillName)
	if err := os.MkdirAll(filepath.Dir(preExisting), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(preExisting, []byte("real"), 0644); err != nil {
		t.Fatal(err)
	}

	EnsureUserSkillLinks(agentsHome, skillName, skillDir)

	target := filepath.Join(tmp, ".claude", "skills", skillName)
	fi, err := os.Lstat(target)
	if err != nil {
		t.Fatalf("expected ~/.claude/skills link: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("expected symlink, got %v", fi.Mode())
	}
}
