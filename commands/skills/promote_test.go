package skills

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/links"
	"github.com/AGOrcha/dot-agents/internal/testutil"
)

// TestMain is a package-wide hermeticity guard: it fails the whole test
// binary if any test in this package writes into the developer's real
// ~/.claude/skills or ~/.agents/skills (e.g. a promote/mirror test that
// resolves os.UserHomeDir() without isolating HOME via t.Setenv first). See
// testutil.RunPackageTestsWithHomeGuard and
// .agents/lessons/hermetic-home-for-state-resolving-tests/LESSON.md.
func TestMain(m *testing.M) {
	os.Exit(testutil.RunPackageTestsWithHomeGuard(m))
}

// ── PromoteSkillIn success ────────────────────────────────────────────────────

func TestPromoteSkillIn_ConvergesRepoLocalToManagedSymlink(t *testing.T) {
	agentsHome, projectPath := testutil.NewTempProject(t, "myprojtest")
	testutil.WriteSkillManifest(t, projectPath, "my-skill")

	if err := PromoteSkillIn("my-skill", projectPath); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	canonicalPath := filepath.Join(agentsHome, "skills", "myprojtest", "my-skill")
	repoLocalPath := filepath.Join(projectPath, ".agents", "skills", "my-skill")

	// Canonical path must be a real directory containing SKILL.md.
	cfi, err := os.Lstat(canonicalPath)
	if err != nil {
		t.Fatalf("canonical path not created at %s: %v", canonicalPath, err)
	}
	if cfi.Mode()&os.ModeSymlink != 0 {
		t.Errorf("canonical path %s should be a real directory, got symlink", canonicalPath)
	}
	if !cfi.IsDir() {
		t.Errorf("canonical path %s should be a directory, got %v", canonicalPath, cfi.Mode())
	}
	if _, err := os.Stat(filepath.Join(canonicalPath, "SKILL.md")); err != nil {
		t.Errorf("canonical SKILL.md missing: %v", err)
	}

	// Repo-local path must now be a managed symlink pointing to canonical.
	rfi, err := os.Lstat(repoLocalPath)
	if err != nil {
		t.Fatalf("repo-local path missing after promote: %v", err)
	}
	if rfi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("repo-local path %s should be a symlink after promote, got %v", repoLocalPath, rfi.Mode())
	}
	target, err := os.Readlink(repoLocalPath)
	if err != nil {
		t.Fatalf("readlink repo-local: %v", err)
	}
	if target != canonicalPath {
		t.Errorf("repo-local symlink target = %q, want %q", target, canonicalPath)
	}

	// .agentsrc.json should have the skill registered.
	rc, err := config.LoadAgentsRC(projectPath)
	if err != nil {
		t.Fatalf("LoadAgentsRC: %v", err)
	}
	found := false
	for _, s := range rc.Skills {
		if s == "my-skill" {
			found = true
		}
	}
	if !found {
		t.Errorf(".agentsrc.json Skills = %v; want 'my-skill' to be present", rc.Skills)
	}
}

// TestPromoteSkillIn_PreservesManifestUnknownFields regression-tests that promote's
// LoadAgentsRC → append Skills → Save path keeps ExtraFields (custom keys) and known
// multi-source declarations — not only the isolated marshal tests.
func TestPromoteSkillIn_PreservesManifestUnknownFields(t *testing.T) {
	_, projectPath := testutil.WritePreservationManifest(t)
	testutil.WriteSkillManifest(t, projectPath, "extra-skill")

	if err := PromoteSkillIn("extra-skill", projectPath); err != nil {
		t.Fatalf("PromoteSkillIn: %v", err)
	}

	rc, err := config.LoadAgentsRC(projectPath)
	if err != nil {
		t.Fatalf("LoadAgentsRC: %v", err)
	}
	testutil.AssertExtraFieldsPreserved(t, rc)
	found := false
	for _, s := range rc.Skills {
		if s == "extra-skill" {
			found = true
		}
	}
	if !found {
		t.Errorf("Skills should include extra-skill, got %v", rc.Skills)
	}
}

func TestPromoteSkillIn_IdempotentOnExistingSymlink(t *testing.T) {
	agentsHome, projectPath := testutil.NewTempProject(t, "myprojtest2")
	testutil.WriteSkillManifest(t, projectPath, "idem-skill")

	// First promote: copies content, repo-local becomes managed symlink.
	if err := PromoteSkillIn("idem-skill", projectPath); err != nil {
		t.Fatalf("first promote: %v", err)
	}
	// Second promote: repo-local is already a symlink to canonical — idempotent.
	if err := PromoteSkillIn("idem-skill", projectPath); err != nil {
		t.Fatalf("second promote: %v", err)
	}

	// Canonical still a real directory.
	canonical := filepath.Join(agentsHome, "skills", "myprojtest2", "idem-skill")
	fi, err := os.Lstat(canonical)
	if err != nil {
		t.Fatalf("canonical missing after second promote: %v", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Errorf("canonical should be a real directory after second promote, got symlink")
	}

	// Skills list should not contain duplicates.
	rc, err := config.LoadAgentsRC(projectPath)
	if err != nil {
		t.Fatalf("LoadAgentsRC: %v", err)
	}
	count := 0
	for _, s := range rc.Skills {
		if s == "idem-skill" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("Skills list has %d occurrences of 'idem-skill'; want 1. list=%v", count, rc.Skills)
	}
}

// ── PromoteSkillIn error paths ────────────────────────────────────────────────

func TestPromoteSkillIn_ErrorSkillNotFound(t *testing.T) {
	_, projectPath := testutil.NewTempProject(t, "myprojtest3")
	// Do NOT write skill files — skill directory does not exist.

	err := PromoteSkillIn("nonexistent", projectPath)
	if err == nil {
		t.Fatal("expected error for missing skill, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error message = %q; want 'not found' substring", err.Error())
	}
}

func TestPromoteSkillIn_ErrorNoProjectName(t *testing.T) {
	_, projectPath := testutil.NewTempProject(t, "")
	testutil.WriteSkillManifest(t, projectPath, "some-skill")

	err := PromoteSkillIn("some-skill", projectPath)
	if err == nil {
		t.Fatal("expected error for empty project name, got nil")
	}
	if !strings.Contains(err.Error(), "project name") {
		t.Errorf("error message = %q; want 'project name' substring", err.Error())
	}
}

func TestPromoteSkillIn_ErrorRepoLocalSymlinkMispoints(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlink semantics: os.ModeSymlink + os.Readlink mispoint detection has no managed-link analogue")
	}
	agentsHome, projectPath := testutil.NewTempProject(t, "myprojtest5")
	testutil.WriteSkillManifest(t, projectPath, "mis-skill")

	// Manually create repo-local as a symlink pointing somewhere else.
	repoLocalPath := filepath.Join(projectPath, ".agents", "skills", "mis-skill")
	// Remove the real dir first, create symlink to a different location.
	if err := os.RemoveAll(repoLocalPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(agentsHome, "other"), repoLocalPath); err != nil {
		t.Fatal(err)
	}

	err := PromoteSkillIn("mis-skill", projectPath)
	if err == nil {
		t.Fatal("expected error when repo-local symlink points elsewhere, got nil")
	}
	if !strings.Contains(err.Error(), "already a symlink but points to") {
		t.Errorf("error message = %q; want 'already a symlink but points to' substring", err.Error())
	}
}

func TestPromoteSkillIn_ErrorExistingNonSymlink(t *testing.T) {
	agentsHome, projectPath := testutil.NewTempProject(t, "myprojtest4")
	testutil.WriteSkillManifest(t, projectPath, "clash-skill")

	// Pre-create a real directory at the destination (not a symlink).
	destPath := filepath.Join(agentsHome, "skills", "myprojtest4", "clash-skill")
	if err := os.MkdirAll(destPath, 0755); err != nil {
		t.Fatal(err)
	}

	err := PromoteSkillIn("clash-skill", projectPath)
	if err == nil {
		t.Fatal("expected error when canonical path is a real directory, got nil")
	}
	if !strings.Contains(err.Error(), "real directory") {
		t.Errorf("error message = %q; want 'real directory' substring", err.Error())
	}
}

// TestPromoteSkillIn_MirrorSucceedsWhenClaudeSkillsDirAlreadyExists is the regression
// test for the "absolute target root → allowlist miss" bug.
//
// Scenario: a user manually created (or a previous run left) a real directory at
// homeDir/.claude/skills/<name>/ with SKILL.md.  When PromoteSkillIn runs its mirror
// step it must replace that directory with a managed symlink.
//
// Pre-fix the mirror step passed absolute target roots to ExecuteSharedSkillMirrorPlan,
// producing an intent.TargetPath of "/abs/path/.claude/skills/name".
// isAllowlistedSharedMirrorTarget only recognises ".claude/skills/" prefixes, so the
// allowlist check failed and the mirror step warned "target … is not allowlisted".
//
// Post-fix: relative roots + homeDir as repoPath → intent.TargetPath = ".claude/skills/name"
// → allowlist passes → existing directory is replaced with the managed symlink.
func TestPromoteSkillIn_MirrorSucceedsWhenClaudeSkillsDirAlreadyExists(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agentshome")
	fakeHome := filepath.Join(tmp, "home")
	projectPath := filepath.Join(tmp, "repo")
	for _, d := range []string{agentsHome, fakeHome, projectPath} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", fakeHome)

	projectName := "mirrortest"
	skillName := "article-extract"

	// Write .agentsrc.json
	rc := &config.AgentsRC{Version: 1, Project: projectName, Sources: []config.Source{{Type: "local"}}}
	if err := rc.Save(projectPath); err != nil {
		t.Fatalf("rc.Save: %v", err)
	}
	testutil.WriteSkillManifest(t, projectPath, skillName)

	// Pre-create a real directory at the Claude skills target — this is the
	// scenario that triggered the bug: skill was placed directly in ~/.claude/skills/
	// before the promote command was run.
	claudeSkillsDir := filepath.Join(fakeHome, ".claude", "skills", skillName)
	if err := os.MkdirAll(claudeSkillsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claudeSkillsDir, "SKILL.md"), []byte("# old\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := PromoteSkillIn(skillName, projectPath); err != nil {
		t.Fatalf("PromoteSkillIn: %v", err)
	}

	// The canonical path must be a real directory.
	canonicalPath := filepath.Join(agentsHome, "skills", projectName, skillName)
	if fi, err := os.Lstat(canonicalPath); err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("canonical path should be a real directory: err=%v", err)
	}

	// ~/.claude/skills/<name> must now be a managed link → canonical. Assert
	// via the OS-aware links contract rather than raw os.Readlink ==: on
	// Windows this managed link to a directory is a junction whose stored
	// target is an absolute, cleaned (and possibly extended-length) path that
	// is never byte-identical to canonicalPath even though it resolves there.
	claudeLink := filepath.Join(fakeHome, ".claude", "skills", skillName)
	if _, err := os.Lstat(claudeLink); err != nil {
		t.Fatalf("~/.claude/skills/%s missing after promote: %v", skillName, err)
	}
	if !links.IsManagedLink(claudeLink, canonicalPath) {
		t.Errorf("~/.claude/skills/%s should be a managed link to %q", skillName, canonicalPath)
	}
}
