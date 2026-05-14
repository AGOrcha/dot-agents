package commands

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/config"
)

// withMkdirAllStub swaps osMkdirAll for the duration of the test.
func withMkdirAllStub(t *testing.T, stub func(string, os.FileMode) error) {
	t.Helper()
	prev := osMkdirAll
	osMkdirAll = stub
	t.Cleanup(func() { osMkdirAll = prev })
}

// withWriteFileStub swaps osWriteFile for the duration of the test.
func withWriteFileStub(t *testing.T, stub func(string, []byte, os.FileMode) error) {
	t.Helper()
	prev := osWriteFile
	osWriteFile = stub
	t.Cleanup(func() { osWriteFile = prev })
}

// withRemoveStub swaps osRemove for the duration of the test.
func withRemoveStub(t *testing.T, stub func(string) error) {
	t.Helper()
	prev := osRemove
	osRemove = stub
	t.Cleanup(func() { osRemove = prev })
}

// withExecutableStub swaps osExecutable for the duration of the test.
func withExecutableStub(t *testing.T, stub func() (string, error)) {
	t.Helper()
	prev := osExecutable
	osExecutable = stub
	t.Cleanup(func() { osExecutable = prev })
}

// withGetwdStub swaps osGetwd for the duration of the test.
func withGetwdStub(t *testing.T, stub func() (string, error)) {
	t.Helper()
	prev := osGetwd
	osGetwd = stub
	t.Cleanup(func() { osGetwd = prev })
}

// withSymlinkStub swaps osSymlink for the duration of the test.
func withSymlinkStub(t *testing.T, stub func(string, string) error) {
	t.Helper()
	prev := osSymlink
	osSymlink = stub
	t.Cleanup(func() { osSymlink = prev })
}

// ─── writeKGMCPConfigFile seam paths ─────────────────────────────────────────

// MkdirAll failure inside writeKGMCPConfigFile must propagate.
func TestWriteKGMCPConfigFile_MkdirAllError(t *testing.T) {
	sentinel := errors.New("mkdir boom")
	withMkdirAllStub(t, func(string, os.FileMode) error { return sentinel })

	target := filepath.Join(t.TempDir(), "nested", "claude.json")
	err := writeKGMCPConfigFile(target, map[string]any{"command": "x"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected mkdir sentinel, got %v", err)
	}
}

// WriteFile failure inside writeKGMCPConfigFile must propagate.
func TestWriteKGMCPConfigFile_WriteFileError(t *testing.T) {
	sentinel := errors.New("write boom")
	withWriteFileStub(t, func(string, []byte, os.FileMode) error { return sentinel })

	target := filepath.Join(t.TempDir(), "claude.json")
	err := writeKGMCPConfigFile(target, map[string]any{"command": "x"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected write sentinel, got %v", err)
	}
}

// ─── writeKGMCPConfigs seam paths ────────────────────────────────────────────

// Executable() failure must propagate before any files are touched.
func TestWriteKGMCPConfigs_ExecutableError(t *testing.T) {
	sentinel := errors.New("no exe")
	withExecutableStub(t, func() (string, error) { return "", sentinel })

	err := writeKGMCPConfigs(t.TempDir())
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected executable sentinel, got %v", err)
	}
}

// Per-file error inside the iteration must surface as the first failure.
func TestWriteKGMCPConfigs_PerFileError(t *testing.T) {
	sentinel := errors.New("write boom")
	withWriteFileStub(t, func(string, []byte, os.FileMode) error { return sentinel })

	err := writeKGMCPConfigs(t.TempDir())
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected per-file sentinel, got %v", err)
	}
}

// ─── captureProposalRollback closures ────────────────────────────────────────

// When target file exists, rollback restores it. Stub MkdirAll to force the
// "restore: mkdir failed" branch.
func TestCaptureProposalRollback_RestoreMkdirError(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "f.txt")
	if err := os.WriteFile(target, []byte("orig"), 0o644); err != nil {
		t.Fatal(err)
	}

	rollback, err := captureProposalRollback(target)
	if err != nil {
		t.Fatalf("captureProposalRollback: %v", err)
	}

	sentinel := errors.New("mkdir boom")
	withMkdirAllStub(t, func(string, os.FileMode) error { return sentinel })
	if got := rollback(); !errors.Is(got, sentinel) {
		t.Fatalf("expected mkdir sentinel, got %v", got)
	}
}

// Stub WriteFile to force the "restore: write failed" branch.
func TestCaptureProposalRollback_RestoreWriteError(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "f.txt")
	if err := os.WriteFile(target, []byte("orig"), 0o644); err != nil {
		t.Fatal(err)
	}

	rollback, err := captureProposalRollback(target)
	if err != nil {
		t.Fatalf("captureProposalRollback: %v", err)
	}

	sentinel := errors.New("write boom")
	withWriteFileStub(t, func(string, []byte, os.FileMode) error { return sentinel })
	if got := rollback(); !errors.Is(got, sentinel) {
		t.Fatalf("expected write sentinel, got %v", got)
	}
}

// Happy-path restore branch — exercise the closure end-to-end after seams are
// reset so MkdirAll/WriteFile succeed.
func TestCaptureProposalRollback_RestoreSuccess(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "nested", "f.txt")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("orig"), 0o644); err != nil {
		t.Fatal(err)
	}

	rollback, err := captureProposalRollback(target)
	if err != nil {
		t.Fatalf("captureProposalRollback: %v", err)
	}

	// Mutate the file to verify restore overwrites it.
	if err := os.WriteFile(target, []byte("mutated"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "orig" {
		t.Errorf("expected restored content 'orig', got %q", got)
	}
}

// When the target does NOT exist, captureProposalRollback returns a removal
// closure. Stub Remove to a non-ENOENT error and confirm it propagates.
func TestCaptureProposalRollback_RemoveError(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "absent.txt")

	rollback, err := captureProposalRollback(target)
	if err != nil {
		t.Fatalf("captureProposalRollback: %v", err)
	}

	sentinel := errors.New("remove boom")
	withRemoveStub(t, func(string) error { return sentinel })
	if got := rollback(); !errors.Is(got, sentinel) {
		t.Fatalf("expected remove sentinel, got %v", got)
	}
}

// Removal closure must swallow ENOENT.
func TestCaptureProposalRollback_RemoveENOENTSwallowed(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "absent.txt")

	rollback, err := captureProposalRollback(target)
	if err != nil {
		t.Fatalf("captureProposalRollback: %v", err)
	}

	withRemoveStub(t, func(string) error {
		return &fs.PathError{Op: "remove", Path: target, Err: fs.ErrNotExist}
	})
	if got := rollback(); got != nil {
		t.Fatalf("expected ENOENT to be swallowed, got %v", got)
	}
}

// captureProposalRollback returns a propagated error when ReadFile returns a
// non-ENOENT error. Trigger via ENOTDIR (target lives "under" a regular file).
func TestCaptureProposalRollback_ReadFileError(t *testing.T) {
	tmp := t.TempDir()
	blocker := filepath.Join(tmp, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(blocker, "child.txt")

	if _, err := captureProposalRollback(target); err == nil {
		t.Fatal("expected non-nil error from ENOTDIR read")
	} else if errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expected non-ENOENT error, got %v", err)
	}
}

// ─── scaffoldWorkflowAssets MkdirAll branch ──────────────────────────────────

func TestScaffoldWorkflowAssets_MkdirError(t *testing.T) {
	// AgentsContextDir() resolves from $HOME/.agents/active — point HOME at
	// the tmp dir to keep the call site harmless under stubs.
	t.Setenv("HOME", t.TempDir())

	sentinel := errors.New("mkdir boom")
	withMkdirAllStub(t, func(string, os.FileMode) error { return sentinel })

	if err := scaffoldWorkflowAssets(t.TempDir()); !errors.Is(err, sentinel) {
		t.Fatalf("expected mkdir sentinel, got %v", err)
	}
	// silence unused import linter for config when nothing references it
	_ = config.AgentsContextDir
}

// ─── ensureSkillMarkdown WriteFile branch ────────────────────────────────────

func TestEnsureSkillMarkdown_WriteError(t *testing.T) {
	dir := t.TempDir()
	skillMD := filepath.Join(dir, "SKILL.md")

	sentinel := errors.New("write boom")
	withWriteFileStub(t, func(string, []byte, os.FileMode) error { return sentinel })

	err := ensureSkillMarkdown(skillMD, "demo")
	if err == nil || !strings.Contains(err.Error(), "creating SKILL.md") {
		t.Fatalf("expected creating SKILL.md error, got %v", err)
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected wrapped sentinel, got %v", err)
	}
}

// When SKILL.md already exists, ensureSkillMarkdown is a no-op and never
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

	if err := ensureSkillMarkdown(skillMD, "demo"); err != nil {
		t.Fatalf("ensureSkillMarkdown: %v", err)
	}
}

// ─── runInstall / runInstallGenerate Getwd failure ───────────────────────────

func TestRunInstall_GetwdError(t *testing.T) {
	sentinel := errors.New("getwd boom")
	withGetwdStub(t, func() (string, error) { return "", sentinel })

	err := runInstall(false)
	if err == nil || !errors.Is(err, sentinel) {
		t.Fatalf("expected wrapped getwd error, got %v", err)
	}
}

func TestRunInstallGenerate_GetwdError(t *testing.T) {
	sentinel := errors.New("getwd boom")
	withGetwdStub(t, func() (string, error) { return "", sentinel })

	err := runInstallGenerate()
	if err == nil || !errors.Is(err, sentinel) {
		t.Fatalf("expected wrapped getwd error, got %v", err)
	}
}

// ─── linkResourceFromSources MkdirAll / Symlink branches ─────────────────────

func TestLinkResourceFromSources_MkdirError(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(tmp, "src")
	skillDir := filepath.Join(src, "skills", "proj", "demo")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	sentinel := errors.New("mkdir boom")
	withMkdirAllStub(t, func(string, os.FileMode) error { return sentinel })

	err := linkResourceFromSources("skills", "demo", "proj", []string{src})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected mkdir sentinel, got %v", err)
	}
}

func TestLinkResourceFromSources_SymlinkError(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(tmp, "src")
	skillDir := filepath.Join(src, "skills", "proj", "demo")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	sentinel := errors.New("symlink boom")
	withSymlinkStub(t, func(string, string) error { return sentinel })

	err := linkResourceFromSources("skills", "demo", "proj", []string{src})
	if err == nil || !strings.Contains(err.Error(), "symlinking demo") {
		t.Fatalf("expected wrapped symlink error, got %v", err)
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel to wrap, got %v", err)
	}
}

// ─── cloneGitSource MkdirAll branch ──────────────────────────────────────────

func TestCloneGitSource_MkdirError(t *testing.T) {
	sentinel := errors.New("mkdir boom")
	withMkdirAllStub(t, func(string, os.FileMode) error { return sentinel })

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	_, err := cloneGitSource("git", "https://example.invalid/repo.git", "main", t.TempDir())
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected mkdir sentinel, got %v", err)
	}
}

// ─── import.go content-import seams ──────────────────────────────────────────

func TestImportMissingContentCandidate_WriteError(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.txt")
	if err := os.WriteFile(src, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(tmp, "out", "dest.txt")

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	sentinel := errors.New("write boom")
	withWriteFileStub(t, func(string, []byte, os.FileMode) error { return sentinel })

	c := importCandidate{project: "p", sourceRoot: tmp, sourcePath: src, destRel: "dest.txt"}
	res := importMissingContentCandidate(c, dest, []byte("data"), "")
	if res.imported != 0 || res.skipped != 1 {
		t.Errorf("expected skipped=1 on write error, got %+v", res)
	}
}

func TestImportPreservedConflictCandidate_MkdirError(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.txt")
	if err := os.WriteFile(src, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	altDest := filepath.Join(tmp, "alt", "out.txt")

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	// First call (writeImportConflictReviewNote) and subsequent altDest must
	// both fail — both call osMkdirAll.
	sentinel := errors.New("mkdir boom")
	withMkdirAllStub(t, func(string, os.FileMode) error { return sentinel })

	c := importCandidate{project: "p", sourceRoot: tmp, sourcePath: src, destRel: "dest.txt"}
	out := importOutput{destRel: "dest.txt", content: []byte("alt"), Origin: "test"}
	res := importPreservedConflictCandidate(c, tmp, out, "alt/out.txt", altDest, "ts")
	if res.imported != 0 || res.skipped != 1 {
		t.Errorf("expected skipped=1 on mkdir error, got %+v", res)
	}
}

func TestImportPreservedConflictCandidate_WriteError(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.txt")
	if err := os.WriteFile(src, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	altDest := filepath.Join(tmp, "alt", "out.txt")

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	sentinel := errors.New("write boom")
	withWriteFileStub(t, func(string, []byte, os.FileMode) error { return sentinel })

	c := importCandidate{project: "p", sourceRoot: tmp, sourcePath: src, destRel: "dest.txt"}
	out := importOutput{destRel: "dest.txt", content: []byte("alt"), Origin: "test"}
	res := importPreservedConflictCandidate(c, tmp, out, "alt/out.txt", altDest, "ts")
	if res.imported != 0 || res.skipped != 1 {
		t.Errorf("expected skipped=1 on write error, got %+v", res)
	}
}

func TestWriteImportConflictReviewNote_MkdirError(t *testing.T) {
	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	sentinel := errors.New("mkdir boom")
	withMkdirAllStub(t, func(string, os.FileMode) error { return sentinel })

	err := writeImportConflictReviewNote(t.TempDir(), "p", "rel.yaml", "alt.yaml", "origin")
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected mkdir sentinel, got %v", err)
	}
}

func TestWriteImportConflictReviewNote_WriteError(t *testing.T) {
	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	sentinel := errors.New("write boom")
	withWriteFileStub(t, func(string, []byte, os.FileMode) error { return sentinel })

	err := writeImportConflictReviewNote(t.TempDir(), "p", "rel.yaml", "alt.yaml", "origin")
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected write sentinel, got %v", err)
	}
}

// ─── createSkill MkdirAll branch ─────────────────────────────────────────────

func TestCreateSkill_MkdirError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))

	sentinel := errors.New("mkdir boom")
	withMkdirAllStub(t, func(string, os.FileMode) error { return sentinel })

	err := createSkill("demo", "global")
	if err == nil || !strings.Contains(err.Error(), "creating skill directory") {
		t.Fatalf("expected wrapped mkdir error, got %v", err)
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel to wrap, got %v", err)
	}
}

// ─── ensureUserSkillLinks MkdirAll branch (continue path) ────────────────────

// When MkdirAll fails for both targets, ensureUserSkillLinks silently moves on.
// Verify by also installing a fatal osSymlink stub — it must not be reached.
func TestEnsureUserSkillLinks_MkdirAllFailsContinue(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	withMkdirAllStub(t, func(string, os.FileMode) error { return errors.New("mkdir boom") })
	withSymlinkStub(t, func(string, string) error {
		t.Fatal("osSymlink must not be called when osMkdirAll returns an error")
		return nil
	})

	ensureUserSkillLinks(filepath.Join(tmp, ".agents"), "demo", filepath.Join(tmp, ".agents", "skills", "global", "demo"))
}

// When the link already exists, symlink must not be re-attempted.
func TestEnsureUserSkillLinks_SkipsExisting(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	// Pre-create the targets so Lstat finds something.
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

	ensureUserSkillLinks(filepath.Join(tmp, ".agents"), "demo", filepath.Join(tmp, ".agents", "skills", "global", "demo"))
}

// ─── backupExistingConfigsList: Remove failure skips count ───────────────────

func TestBackupExistingConfigsList_RemoveError(t *testing.T) {
	tmp := t.TempDir()
	// A managed-looking regular file
	target := filepath.Join(tmp, "config.txt")
	if err := os.WriteFile(target, []byte("v"), 0o644); err != nil {
		t.Fatal(err)
	}

	withRemoveStub(t, func(string) error { return errors.New("remove boom") })

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	got := backupExistingConfigsList([]string{target}, tmp, t.TempDir(), "p", "20240101-000000")
	if got != 0 {
		t.Errorf("expected count=0 when Remove fails, got %d", got)
	}
}

// ─── runInit early MkdirAll error ────────────────────────────────────────────

func TestRunInit_MkdirError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	sentinel := errors.New("mkdir boom")
	withMkdirAllStub(t, func(string, os.FileMode) error { return sentinel })

	err := runInit(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "creating ") {
		t.Fatalf("expected wrapped creating error, got %v", err)
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel to wrap, got %v", err)
	}
}
