package skills

// Seam-injection tests exercise the err != nil branches on os.MkdirAll /
// os.WriteFile / os.Symlink / config.Load that cannot be triggered with a
// writable tmp fixture. Each test constructs a fakeSkillsIO that fault-injects
// exactly one operation and passes it explicitly to the IO-injected unit under
// test (the lowercase createSkill / ensureSkillMarkdown / ensureUserSkillLinks
// / appendSkillToAgentsRC variants in new.go).
//
// This replaces the legacy `var osMkdirAll = os.MkdirAll`-style func-var seams
// formerly defined in seams.go (see docs/TEST_SEAMS.md and the
// seam-interface-di-migration plan / PR #59 platform-pkg reference impl).

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/config"
)

// fakeSkillsIO implements skillsIO with per-operation overrides. A nil
// override delegates to the real impl so a test only has to define the failure
// point it wants to exercise (the nil-delegates-to-real convention from
// docs/TEST_SEAMS.md).
type fakeSkillsIO struct {
	mkdirAll   func(path string, perm fs.FileMode) error
	writeFile  func(name string, data []byte, perm fs.FileMode) error
	symlink    func(oldname, newname string) error
	configLoad func() (*config.Config, error)
}

func (f *fakeSkillsIO) MkdirAll(path string, perm os.FileMode) error {
	if f.mkdirAll != nil {
		return f.mkdirAll(path, perm)
	}
	return os.MkdirAll(path, perm)
}

func (f *fakeSkillsIO) WriteFile(name string, data []byte, perm os.FileMode) error {
	if f.writeFile != nil {
		return f.writeFile(name, data, perm)
	}
	return os.WriteFile(name, data, perm)
}

func (f *fakeSkillsIO) Symlink(oldname, newname string) error {
	if f.symlink != nil {
		return f.symlink(oldname, newname)
	}
	return os.Symlink(oldname, newname)
}

func (f *fakeSkillsIO) ConfigLoad() (*config.Config, error) {
	if f.configLoad != nil {
		return f.configLoad()
	}
	return config.Load()
}

// newFakeIOWriteFileStub returns a fake whose WriteFile delegates to stub and
// whose other ops delegate to the real impl.
func newFakeIOWriteFileStub(stub func(string, []byte, fs.FileMode) error) *fakeSkillsIO {
	return &fakeSkillsIO{writeFile: stub}
}

// newFakeIOMkdirAllStub returns a fake whose MkdirAll delegates to stub.
func newFakeIOMkdirAllStub(stub func(string, fs.FileMode) error) *fakeSkillsIO {
	return &fakeSkillsIO{mkdirAll: stub}
}

// newFakeIOSymlinkStub returns a fake whose Symlink delegates to stub.
func newFakeIOSymlinkStub(stub func(string, string) error) *fakeSkillsIO {
	return &fakeSkillsIO{symlink: stub}
}

// newFakeIOMkdirAndSymlinkStub returns a fake with both MkdirAll and Symlink
// overridden (used for ensureUserSkillLinks tests that pair a MkdirAll-fails
// stub with a fatal Symlink stub).
func newFakeIOMkdirAndSymlinkStub(mkdir func(string, fs.FileMode) error, sym func(string, string) error) *fakeSkillsIO {
	return &fakeSkillsIO{mkdirAll: mkdir, symlink: sym}
}

// newFakeIOConfigLoadStub returns a fake whose ConfigLoad delegates to stub.
func newFakeIOConfigLoadStub(stub func() (*config.Config, error)) *fakeSkillsIO {
	return &fakeSkillsIO{configLoad: stub}
}

// ─── ensureSkillMarkdown WriteFile branch ────────────────────────────────────

func TestEnsureSkillMarkdown_WriteError(t *testing.T) {
	dir := t.TempDir()
	skillMD := filepath.Join(dir, "SKILL.md")

	sentinel := errors.New("write boom")
	fakeIO := newFakeIOWriteFileStub(func(string, []byte, fs.FileMode) error { return sentinel })

	err := ensureSkillMarkdown(fakeIO, skillMD, "demo")
	if err == nil || !strings.Contains(err.Error(), "creating SKILL.md") {
		t.Fatalf("expected creating SKILL.md error, got %v", err)
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected wrapped sentinel, got %v", err)
	}
}

// When SKILL.md already exists, ensureSkillMarkdown is a no-op and never
// touches WriteFile. Verify this by installing a fatal stub.
func TestEnsureSkillMarkdown_NoopWhenPresent(t *testing.T) {
	dir := t.TempDir()
	skillMD := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(skillMD, []byte("# existing\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	fakeIO := newFakeIOWriteFileStub(func(string, []byte, fs.FileMode) error {
		t.Fatal("WriteFile must not be called when SKILL.md already exists")
		return nil
	})

	if err := ensureSkillMarkdown(fakeIO, skillMD, "demo"); err != nil {
		t.Fatalf("ensureSkillMarkdown: %v", err)
	}
}

// ─── createSkill MkdirAll branch ─────────────────────────────────────────────

func TestCreateSkill_MkdirError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))

	sentinel := errors.New("mkdir boom")
	fakeIO := newFakeIOMkdirAllStub(func(string, fs.FileMode) error { return sentinel })

	err := createSkill(fakeIO, "demo", "global")
	if err == nil || !strings.Contains(err.Error(), "creating skill directory") {
		t.Fatalf("expected wrapped mkdir error, got %v", err)
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel to wrap, got %v", err)
	}
}

// ─── ensureUserSkillLinks MkdirAll branch (continue path) ────────────────────

// When MkdirAll fails for both targets, ensureUserSkillLinks silently moves
// on. Verify by also installing a fatal symlink stub — it must not be reached.
func TestEnsureUserSkillLinks_MkdirAllFailsContinue(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	fakeIO := newFakeIOMkdirAndSymlinkStub(
		func(string, fs.FileMode) error { return errors.New("mkdir boom") },
		func(string, string) error {
			t.Fatal("Symlink must not be called when MkdirAll returns an error")
			return nil
		},
	)

	ensureUserSkillLinks(fakeIO, filepath.Join(tmp, ".agents"), "demo", filepath.Join(tmp, ".agents", "skills", "global", "demo"))
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
	fakeIO := newFakeIOSymlinkStub(func(string, string) error {
		t.Fatal("Symlink must not be called when target already exists")
		return nil
	})

	ensureUserSkillLinks(fakeIO, filepath.Join(tmp, ".agents"), "demo", filepath.Join(tmp, ".agents", "skills", "global", "demo"))
}

// ─── appendSkillToAgentsRC ConfigLoad branch ─────────────────────────────────

func TestAppendSkillToAgentsRC_ConfigLoadError(t *testing.T) {
	fakeIO := newFakeIOConfigLoadStub(func() (*config.Config, error) { return nil, errors.New("load boom") })
	if got := appendSkillToAgentsRC(fakeIO, "demo", "missing-proj"); got != "" {
		t.Errorf("expected empty string on load error, got %q", got)
	}
}

// ─── createSkill → ensureSkillMarkdown propagation ───────────────────────────

// TestCreateSkill_EnsureSkillMarkdownErrorPropagates covers the
// `if err := ensureSkillMarkdown(...); err != nil { return err }` branch
// inside createSkill. MkdirAll succeeds (default delegation) so we reach
// ensureSkillMarkdown; WriteFile then fails so ensureSkillMarkdown errors.
func TestCreateSkill_EnsureSkillMarkdownErrorPropagates(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))

	sentinel := errors.New("write boom")
	fakeIO := newFakeIOWriteFileStub(func(string, []byte, fs.FileMode) error { return sentinel })

	err := createSkill(fakeIO, "err-skill", "global")
	if err == nil || !strings.Contains(err.Error(), "creating SKILL.md") {
		t.Fatalf("expected creating SKILL.md error from createSkill, got %v", err)
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("expected wrapped sentinel, got %v", err)
	}
}
