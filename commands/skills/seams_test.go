package skills

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/config"
)

// withMkdirAllStub swaps osMkdirAll for the duration of the test.
func withMkdirAllStub(t *testing.T, stub func(string, os.FileMode) error) {
	t.Helper()
	orig := osMkdirAll
	osMkdirAll = stub
	t.Cleanup(func() { osMkdirAll = orig })
}

// withWriteFileStub swaps osWriteFile for the duration of the test.
func withWriteFileStub(t *testing.T, stub func(string, []byte, os.FileMode) error) {
	t.Helper()
	orig := osWriteFile
	osWriteFile = stub
	t.Cleanup(func() { osWriteFile = orig })
}

// withSymlinkStub swaps osSymlink for the duration of the test.
func withSymlinkStub(t *testing.T, stub func(string, string) error) {
	t.Helper()
	orig := osSymlink
	osSymlink = stub
	t.Cleanup(func() { osSymlink = orig })
}

// withConfigLoadStub swaps configLoad for the duration of the test.
func withConfigLoadStub(t *testing.T, stub func() (*config.Config, error)) {
	t.Helper()
	orig := configLoad
	configLoad = stub
	t.Cleanup(func() { configLoad = orig })
}

// ─── EnsureSkillMarkdown WriteFile branch ────────────────────────────────────

func TestEnsureSkillMarkdown_WriteError(t *testing.T) {
	dir := t.TempDir()
	skillMD := filepath.Join(dir, "SKILL.md")

	sentinel := errors.New("write boom")
	withWriteFileStub(t, func(string, []byte, os.FileMode) error { return sentinel })

	err := EnsureSkillMarkdown(skillMD, "demo")
	if err == nil || !strings.Contains(err.Error(), "creating SKILL.md") {
		t.Fatalf("expected creating SKILL.md error, got %v", err)
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected wrapped sentinel, got %v", err)
	}
}

// When SKILL.md already exists, EnsureSkillMarkdown is a no-op and never
// touches osWriteFile. Verify this by installing a fatal stub.
func TestEnsureSkillMarkdown_NoopWhenPresent(t *testing.T) {
	dir := t.TempDir()
	skillMD := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(skillMD, []byte("# existing\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	withWriteFileStub(t, func(string, []byte, os.FileMode) error {
		t.Fatal("osWriteFile must not be called when SKILL.md already exists")
		return nil
	})

	if err := EnsureSkillMarkdown(skillMD, "demo"); err != nil {
		t.Fatalf("EnsureSkillMarkdown: %v", err)
	}
}

// ─── CreateSkill MkdirAll branch ─────────────────────────────────────────────

func TestCreateSkill_MkdirError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))

	sentinel := errors.New("mkdir boom")
	withMkdirAllStub(t, func(string, os.FileMode) error { return sentinel })

	err := CreateSkill("demo", "global")
	if err == nil || !strings.Contains(err.Error(), "creating skill directory") {
		t.Fatalf("expected wrapped mkdir error, got %v", err)
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel to wrap, got %v", err)
	}
}

// ─── EnsureUserSkillLinks MkdirAll branch (continue path) ────────────────────

// When MkdirAll fails for both targets, EnsureUserSkillLinks silently moves on.
// Verify by also installing a fatal osSymlink stub — it must not be reached.
func TestEnsureUserSkillLinks_MkdirAllFailsContinue(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	withMkdirAllStub(t, func(string, os.FileMode) error { return errors.New("mkdir boom") })
	withSymlinkStub(t, func(string, string) error {
		t.Fatal("osSymlink must not be called when osMkdirAll returns an error")
		return nil
	})

	EnsureUserSkillLinks(filepath.Join(tmp, ".agents"), "demo", filepath.Join(tmp, ".agents", "skills", "global", "demo"))
}

// When the link already exists, symlink must not be re-attempted.
func TestEnsureUserSkillLinks_SkipsExisting(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	for _, dir := range []string{".agents/skills", ".claude/skills"} {
		full := filepath.Join(tmp, dir, "demo")
		if err := os.MkdirAll(full, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	withSymlinkStub(t, func(string, string) error {
		t.Fatal("osSymlink must not be called when target already exists")
		return nil
	})

	EnsureUserSkillLinks(filepath.Join(tmp, ".agents"), "demo", filepath.Join(tmp, ".agents", "skills", "global", "demo"))
}

// ─── AppendSkillToAgentsRC configLoad branch ─────────────────────────────────

func TestAppendSkillToAgentsRC_ConfigLoadError(t *testing.T) {
	withConfigLoadStub(t, func() (*config.Config, error) { return nil, errors.New("load boom") })
	if got := AppendSkillToAgentsRC("demo", "missing-proj"); got != "" {
		t.Errorf("expected empty string on load error, got %q", got)
	}
}
