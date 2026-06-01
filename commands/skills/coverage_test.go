package skills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/testutil"
)

// TestRefreshSkillMirror_UserHomeDirErrorIsSwallowed covers the os.UserHomeDir
// error branch on platforms where unsetting HOME causes UserHomeDir to fail.
// On macOS UserHomeDir falls back to getpwuid so this branch is not always
// reachable; in that case the test still completes successfully via the
// happy path.
func TestRefreshSkillMirror_UserHomeDirErrorIsSwallowed(t *testing.T) {
	// Unset HOME to attempt to make UserHomeDir fail (mostly effective on
	// Linux test environments). On macOS this is a no-op because getpwuid
	// supplies the home.
	t.Setenv("HOME", "")
	if err := refreshSkillMirror("nope", t.TempDir()); err != nil {
		t.Errorf("refreshSkillMirror swallows errors; got: %v", err)
	}
}

// TestRefreshSkillMirror_ExecutePlanWarnOnlyWithSkills exercises the
// ExecuteSharedSkillMirrorPlan call path with a project that has skills, so
// BuildSharedSkillMirrorIntents produces at least one intent.
func TestRefreshSkillMirror_ExecutePlanWarnOnlyWithSkills(t *testing.T) {
	agentsHome, _ := testutil.NewTempProject(t, "skillproj")
	testutil.WriteCanonicalSkill(t, agentsHome, "skillproj", "alpha")

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	// refreshSkillMirror should never return error; it warns on failure.
	if err := refreshSkillMirror("skillproj", t.TempDir()); err != nil {
		t.Errorf("refreshSkillMirror should swallow plan errors; got: %v", err)
	}
}

// TestRefreshSkillMirror_PlanFailsWarnOnly forces ExecuteSharedSkillMirrorPlan
// to return an error by planting a regular file at the .claude/skills parent
// directory location, which prevents MkdirAll/symlink creation. The function
// must still return nil (warn-only swallow).
func TestRefreshSkillMirror_PlanFailsWarnOnly(t *testing.T) {
	agentsHome, _ := testutil.NewTempProject(t, "skillfail")
	testutil.WriteCanonicalSkill(t, agentsHome, "skillfail", "blocker")

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	// Make `.claude` itself a regular file so MkdirAll(.claude/skills) fails.
	if err := os.WriteFile(filepath.Join(homeDir, ".claude"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := refreshSkillMirror("skillfail", t.TempDir()); err != nil {
		t.Errorf("refreshSkillMirror should swallow plan errors; got: %v", err)
	}
}
