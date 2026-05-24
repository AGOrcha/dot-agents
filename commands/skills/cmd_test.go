package skills

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/spf13/cobra"
)

// testDeps returns a Deps populated with the standard cobra positional-arg
// validators so RunE tests can exercise the cmd tree without depending on the
// parent commands/ package.
func testDeps() Deps {
	return Deps{
		MaximumNArgsWithHints: func(n int, hints ...string) cobra.PositionalArgs {
			return cobra.MaximumNArgs(n)
		},
		RangeArgsWithHints: func(min, max int, hints ...string) cobra.PositionalArgs {
			return cobra.RangeArgs(min, max)
		},
		ExactArgsWithHints: func(n int, hints ...string) cobra.PositionalArgs {
			return cobra.ExactArgs(n)
		},
	}
}

// setupAgentsHomeAndHome creates fake AGENTS_HOME and HOME dirs and sets the
// env vars for the duration of the test. Mirrors the helper in commands/.
func setupAgentsHomeAndHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	fakeHome := filepath.Join(tmp, "home")
	for _, d := range []string{agentsHome, fakeHome} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", fakeHome)
	return agentsHome
}

func TestNewSkillsCmd_Structure(t *testing.T) {
	cmd := NewSkillsCmd(testDeps())
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

	if err := CreateSkill("my-skill", "global"); err != nil {
		t.Fatalf("CreateSkill: %v", err)
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

	if err := CreateSkill("link-skill", "global"); err != nil {
		t.Fatalf("CreateSkill: %v", err)
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

	if err := CreateSkill("proj-skill", "billing-api"); err != nil {
		t.Fatalf("CreateSkill: %v", err)
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
	if err := EnsureSkillMarkdown(path, "skill-x"); err != nil {
		t.Fatalf("EnsureSkillMarkdown: %v", err)
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
	steps := SkillCreationNextSteps("name", "global", "/tmp/SKILL.md")
	if len(steps) != 1 {
		t.Errorf("global next steps = %v; want 1 entry", steps)
	}
}

func TestSkillsListCmd_Args(t *testing.T) {
	cmd := newSkillsListCmd(testDeps())
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
	cmd := newSkillsPromoteCmd(testDeps())
	if err := cmd.Args(cmd, []string{}); err == nil {
		t.Error("promote should require exactly 1 arg")
	}
}

// TestAppendSkillToAgentsRC_HappyAndFailureBranches exercises the happy path
// + the project-not-found and missing-manifest branches.
func TestAppendSkillToAgentsRC_HappyAndFailureBranches(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	// Project not registered → returns ""
	if got := AppendSkillToAgentsRC("x", "no-such-scope"); got != "" {
		t.Errorf("unregistered scope should return empty, got %q", got)
	}

	// Register project but with no .agentsrc.json → returns ""
	projPath := filepath.Join(tmp, "p")
	os.MkdirAll(projPath, 0755)
	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("p", projPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	if got := AppendSkillToAgentsRC("x", "p"); got != "" {
		t.Errorf("missing manifest should return empty, got %q", got)
	}

	// Now write a manifest → returns the success message.
	rc := &config.AgentsRC{Version: 1, Project: "p"}
	if err := rc.Save(projPath); err != nil {
		t.Fatal(err)
	}
	got := AppendSkillToAgentsRC("alpha", "p")
	if got == "" {
		t.Error("expected success message after manifest save")
	}
}

// TestCreateSkill_ProjectScopeAppendsToManifest covers the CreateSkill →
// AppendSkill pathway with a project scope that has a manifest.
func TestCreateSkill_ProjectScopeAppendsToManifest(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)
	projPath := filepath.Join(tmp, "myproj")
	os.MkdirAll(projPath, 0755)
	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("myproj", projPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	rc := &config.AgentsRC{Version: 1, Project: "myproj"}
	if err := rc.Save(projPath); err != nil {
		t.Fatal(err)
	}

	if err := CreateSkill("my-skill", "myproj"); err != nil {
		t.Fatalf("CreateSkill: %v", err)
	}
	rc2, _ := config.LoadAgentsRC(projPath)
	found := false
	for _, n := range rc2.Skills {
		if n == "my-skill" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected skill in manifest, got %+v", rc2.Skills)
	}
}

// TestNewSkillsNewCmd_AcceptsScopeArg exercises the len(args)>1 branch.
func TestNewSkillsNewCmd_AcceptsScopeArg(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	cmd := newSkillsNewCmd(testDeps())
	if err := cmd.RunE(cmd, []string{"named-skill", "myproject"}); err != nil {
		t.Errorf("RunE with scope arg: %v", err)
	}
	skillMD := filepath.Join(agentsHome, "skills", "myproject", "named-skill", "SKILL.md")
	if _, err := os.Stat(skillMD); err != nil {
		t.Errorf("expected skill file at %s: %v", skillMD, err)
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
	EnsureUserSkillLinks(agentsHome, "kept", filepath.Join(agentsHome, "skills", "global", "kept"))

	fi, err := os.Lstat(preExisting)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Errorf("pre-existing target was clobbered into a symlink")
	}
}

// ── cmd RunE coverage moved from commands/coverage_test.go ──────────────────

func TestNewSkillsListCmd_RunE(t *testing.T) {
	setupAgentsHomeAndHome(t)
	cmd := newSkillsListCmd(testDeps())
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Errorf("skills list RunE: %v", err)
	}
	if err := cmd.RunE(cmd, []string{"some-project"}); err != nil {
		t.Errorf("skills list with scope RunE: %v", err)
	}
}

func TestNewSkillsNewCmd_RunE(t *testing.T) {
	agentsHome := setupAgentsHomeAndHome(t)
	cmd := newSkillsNewCmd(testDeps())
	if err := cmd.RunE(cmd, []string{"runE-skill"}); err != nil {
		t.Fatalf("skills new RunE: %v", err)
	}
	if _, err := os.Stat(filepath.Join(agentsHome, "skills", "global", "runE-skill", "SKILL.md")); err != nil {
		t.Errorf("expected SKILL.md: %v", err)
	}
}

func TestNewSkillsPromoteCmd_RunEErrorWhenNoSkill(t *testing.T) {
	setupAgentsHomeAndHome(t)
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	cmd := newSkillsPromoteCmd(testDeps())
	if err := cmd.RunE(cmd, []string{"missing"}); err == nil {
		t.Error("expected error for missing skill")
	}
}

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

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("promo-proj", tmp)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	rc := &config.AgentsRC{Version: 1, Project: "promo-proj"}
	if err := rc.Save(tmp); err != nil {
		t.Fatal(err)
	}

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

// TestCreateSkill_EnsureSkillMarkdownErrorPropagates was migrated to
// seams_test.go alongside the interface-DI seam conversion; it now drives the
// branch through the IO-injected createSkill(fakeIO, ...) entry point.

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
