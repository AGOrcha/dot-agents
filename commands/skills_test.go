package commands

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewSkillsCmd_Structure(t *testing.T) {
	cmd := NewSkillsCmd()
	if cmd == nil {
		t.Fatal("NewSkillsCmd returned nil")
	}
	if cmd.Use != "skills" {
		t.Errorf("Use = %q, want skills", cmd.Use)
	}
	want := map[string]bool{"list": false, "new": false, "promote": false}
	for _, c := range cmd.Commands() {
		want[c.Name()] = true
	}
	for name, ok := range want {
		if !ok {
			t.Errorf("missing subcommand %q", name)
		}
	}
}

func TestCreateSkill_GlobalScopeCreatesDir(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		t.Fatal(err)
	}
	fakeHome := filepath.Join(tmp, "home")
	if err := os.MkdirAll(fakeHome, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", fakeHome)

	if err := createSkill("my-skill", "global"); err != nil {
		t.Fatalf("createSkill: %v", err)
	}
	skillDir := filepath.Join(agentsHome, "skills", "global", "my-skill")
	if _, err := os.Stat(filepath.Join(skillDir, "SKILL.md")); err != nil {
		t.Errorf("SKILL.md not created: %v", err)
	}
}

func TestCreateSkill_GlobalScopeCreatesUserSymlinks(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	fakeHome := filepath.Join(tmp, "home")
	for _, d := range []string{agentsHome, fakeHome} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", fakeHome)

	if err := createSkill("link-skill", "global"); err != nil {
		t.Fatalf("createSkill: %v", err)
	}

	expectedTarget := filepath.Join(agentsHome, "skills", "global", "link-skill")
	for _, target := range []string{
		filepath.Join(fakeHome, ".agents", "skills", "link-skill"),
		filepath.Join(fakeHome, ".claude", "skills", "link-skill"),
	} {
		fi, err := os.Lstat(target)
		if err != nil {
			t.Errorf("expected symlink at %s: %v", target, err)
			continue
		}
		if fi.Mode()&os.ModeSymlink == 0 {
			t.Errorf("%s should be a symlink, got %v", target, fi.Mode())
			continue
		}
		if got, _ := os.Readlink(target); got != expectedTarget {
			t.Errorf("symlink target = %q, want %q", got, expectedTarget)
		}
	}
}

func TestCreateSkill_ProjectScopeDoesNotCreateSymlinks(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	fakeHome := filepath.Join(tmp, "home")
	for _, d := range []string{agentsHome, fakeHome} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", fakeHome)

	if err := createSkill("proj-skill", "billing-api"); err != nil {
		t.Fatalf("createSkill: %v", err)
	}

	if _, err := os.Lstat(filepath.Join(fakeHome, ".claude", "skills", "proj-skill")); !os.IsNotExist(err) {
		t.Errorf("project-scoped skill should not create ~/.claude/skills symlink: err=%v", err)
	}
}

func TestEnsureSkillMarkdown_DoesNotOverwrite(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "SKILL.md")
	if err := os.WriteFile(path, []byte("preexisting"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := ensureSkillMarkdown(path, "skill-x"); err != nil {
		t.Fatalf("ensureSkillMarkdown: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "preexisting" {
		t.Errorf("existing file modified: %q", data)
	}
}

func TestSkillCreationNextSteps_GlobalSingleEntry(t *testing.T) {
	steps := skillCreationNextSteps("name", "global", "/tmp/SKILL.md")
	if len(steps) != 1 {
		t.Errorf("global next steps = %v; want 1 entry", steps)
	}
}

func TestSkillsListCmd_Args(t *testing.T) {
	cmd := newSkillsListCmd()
	// Ensure listing command has the expected metadata and accepts 0..1 args.
	if cmd.Use == "" || cmd.Args == nil {
		t.Fatalf("skills list metadata incomplete: %+v", cmd)
	}
	if err := cmd.Args(cmd, []string{}); err != nil {
		t.Errorf("0 args should be valid: %v", err)
	}
	if err := cmd.Args(cmd, []string{"proj"}); err != nil {
		t.Errorf("1 arg should be valid: %v", err)
	}
}

func TestSkillsPromoteCmd_RejectsZeroArgs(t *testing.T) {
	cmd := newSkillsPromoteCmd()
	if err := cmd.Args(cmd, []string{}); err == nil {
		t.Error("promote should require exactly 1 arg")
	}
}

func TestEnsureUserSkillLinks_PreservesExistingTarget(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	fakeHome := filepath.Join(tmp, "home")
	for _, d := range []string{agentsHome, fakeHome} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", fakeHome)

	preExisting := filepath.Join(fakeHome, ".agents", "skills", "kept")
	if err := os.MkdirAll(filepath.Dir(preExisting), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(preExisting, []byte("real"), 0644); err != nil {
		t.Fatal(err)
	}

	// Should NOT clobber the pre-existing file.
	ensureUserSkillLinks(agentsHome, "kept", filepath.Join(agentsHome, "skills", "global", "kept"))

	fi, err := os.Lstat(preExisting)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Errorf("pre-existing target was clobbered into a symlink")
	}
}
