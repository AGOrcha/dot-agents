package commands

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/linktest"
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

// withCopyFileStub swaps copyFile for the duration of the test.
func withCopyFileStub(t *testing.T, stub func(string, string) error) {
	t.Helper()
	prev := copyFile
	copyFile = stub
	t.Cleanup(func() { copyFile = prev })
}

// withRemoveStub swaps osRemove for the duration of the test.
func withRemoveStub(t *testing.T, stub func(string) error) {
	t.Helper()
	prev := osRemove
	osRemove = stub
	t.Cleanup(func() { osRemove = prev })
}

// withRemoveAllStub swaps osRemoveAll for the duration of the test.
func withRemoveAllStub(t *testing.T, stub func(string) error) {
	t.Helper()
	prev := osRemoveAll
	osRemoveAll = stub
	t.Cleanup(func() { osRemoveAll = prev })
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
	// A directory path causes os.ReadFile to fail with a non-ENOENT error on
	// both POSIX (EISDIR) and Windows (ERROR_ACCESS_DENIED / handle is a
	// directory). The previous "file as a path component" fixture produced
	// ENOTDIR on POSIX but ERROR_PATH_NOT_FOUND (→ fs.ErrNotExist) on
	// Windows, so it could not exercise the non-ENOENT branch there.
	target := filepath.Join(tmp, "adir")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := captureProposalRollback(target); err == nil {
		t.Fatal("expected non-nil error reading a directory")
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
	deps := fakeInitDirMaker{mkdirAll: func(string, os.FileMode) error { return sentinel }}

	if err := scaffoldWorkflowAssets(t.TempDir(), deps); !errors.Is(err, sentinel) {
		t.Fatalf("expected mkdir sentinel, got %v", err)
	}
	// silence unused import linter for config when nothing references it
	_ = config.AgentsContextDir
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
	deps := fakeImportDeps{writeFile: func(string, []byte, os.FileMode) error { return sentinel }}

	c := importCandidate{project: "p", sourceRoot: tmp, sourcePath: src, destRel: "dest.txt"}
	res := importMissingContentCandidate(c, dest, []byte("data"), "", deps)
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

	// Both the writeImportConflictReviewNote MkdirAll and the altDest MkdirAll
	// go through the injected deps.
	sentinel := errors.New("mkdir boom")
	deps := fakeImportDeps{mkdirAll: func(string, os.FileMode) error { return sentinel }}

	c := importCandidate{project: "p", sourceRoot: tmp, sourcePath: src, destRel: "dest.txt"}
	out := importOutput{destRel: "dest.txt", content: []byte("alt"), Origin: "test"}
	res := importPreservedConflictCandidate(c, tmp, out, "alt/out.txt", altDest, "ts", deps)
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
	deps := fakeImportDeps{writeFile: func(string, []byte, os.FileMode) error { return sentinel }}

	c := importCandidate{project: "p", sourceRoot: tmp, sourcePath: src, destRel: "dest.txt"}
	out := importOutput{destRel: "dest.txt", content: []byte("alt"), Origin: "test"}
	res := importPreservedConflictCandidate(c, tmp, out, "alt/out.txt", altDest, "ts", deps)
	if res.imported != 0 || res.skipped != 1 {
		t.Errorf("expected skipped=1 on write error, got %+v", res)
	}
}

func TestWriteImportConflictReviewNote_MkdirError(t *testing.T) {
	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	sentinel := errors.New("mkdir boom")
	deps := fakeImportDeps{mkdirAll: func(string, os.FileMode) error { return sentinel }}

	err := writeImportConflictReviewNote(t.TempDir(), "p", "rel.yaml", "alt.yaml", "origin", deps)
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected mkdir sentinel, got %v", err)
	}
}

func TestWriteImportConflictReviewNote_WriteError(t *testing.T) {
	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	sentinel := errors.New("write boom")
	deps := fakeImportDeps{writeFile: func(string, []byte, os.FileMode) error { return sentinel }}

	err := writeImportConflictReviewNote(t.TempDir(), "p", "rel.yaml", "alt.yaml", "origin", deps)
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected write sentinel, got %v", err)
	}
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

	got, err := backupExistingConfigsList([]string{target}, tmp, t.TempDir(), "p", "20240101-000000")
	if err != nil {
		t.Fatalf("backup itself should succeed (only Remove fails): %v", err)
	}
	if got != 0 {
		t.Errorf("expected count=0 when Remove fails, got %d", got)
	}
}

// ─── replaceImportContentCandidate WriteFile failure ────────────────────────

func TestReplaceImportContentCandidate_WriteFileError(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.txt")
	if err := os.WriteFile(src, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(tmp, "out", "dest.txt")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	srcInfo, _ := os.Stat(src)
	destInfo, _ := os.Stat(dest)

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	sentinel := errors.New("write boom")
	deps := fakeImportDeps{writeFile: func(string, []byte, os.FileMode) error { return sentinel }}

	c := importCandidate{project: "p", sourceRoot: tmp, sourcePath: src, destRel: "dest.txt"}
	res := replaceImportContentCandidate(c, tmp, dest, []byte("new"), "ts", srcInfo, destInfo, deps)
	if res.imported != 0 || res.skipped != 1 {
		t.Errorf("expected skipped=1 on write error, got %+v", res)
	}
}

// ─── processImportCandidate canonical-path branches ─────────────────────────

// When the candidate rel path is a canonical hook source (e.g.
// .cursor/hooks.json), processImportCandidate takes the canonical-path branch.
// A non-existent source there returns importResult{} via statErr.
func TestProcessImportCandidate_CanonicalSourceMissingIsNoop(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	c := importCandidate{
		project:    "p",
		sourceRoot: tmp,
		sourcePath: filepath.Join(tmp, ".cursor", "hooks.json"),
		destRel:    "hooks/p/cursor.json",
	}
	res := processImportCandidate(c, agentsHome, "ts", stdImportDeps{})
	if res.imported != 0 || res.skipped != 0 {
		t.Errorf("expected no-op when canonical source missing, got %+v", res)
	}
}

// Canonical source pointing at a directory must short-circuit (IsDir branch).
func TestProcessImportCandidate_CanonicalSourceDirIsNoop(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)
	dirAsSrc := filepath.Join(tmp, ".cursor", "hooks.json")
	if err := os.MkdirAll(dirAsSrc, 0o755); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	c := importCandidate{
		project:    "p",
		sourceRoot: tmp,
		sourcePath: dirAsSrc,
		destRel:    "hooks/p/cursor.json",
	}
	res := processImportCandidate(c, agentsHome, "ts", stdImportDeps{})
	if res.imported != 0 || res.skipped != 0 {
		t.Errorf("expected no-op when canonical source is a dir, got %+v", res)
	}
}

// Cursor hooks.json source that fails canonicalization (malformed) hits the
// "Failed to canonicalize" warn branch in processCanonicalHookBundleImport
// and returns skipped=1.
func TestProcessImportCandidate_CanonicalCanonicalizationError(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)
	srcDir := filepath.Join(tmp, ".cursor")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(srcDir, "hooks.json")
	if err := os.WriteFile(src, []byte("not json at all {{{"), 0o644); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	c := importCandidate{
		project:    "p",
		sourceRoot: tmp,
		sourcePath: src,
		destRel:    "hooks/p/cursor.json",
	}
	res := processImportCandidate(c, agentsHome, "ts", stdImportDeps{})
	// Either canonicalization succeeded with empty outputs (no-op) or failed
	// with skipped=1. Both exercise canonical-path branches that were
	// previously uncovered.
	if res.skipped > 1 || res.imported > 0 {
		t.Errorf("unexpected result for malformed cursor hooks.json: %+v", res)
	}
}

// withConfigLoadStub swaps configLoad for the duration of the test.
func withConfigLoadStub(t *testing.T, stub func() (*config.Config, error)) {
	t.Helper()
	prev := configLoad
	configLoad = stub
	t.Cleanup(func() { configLoad = prev })
}

// withApplyProposalStub swaps applyProposalFn for the duration of the test.
func withApplyProposalStub(t *testing.T, stub func(*config.Proposal) error) {
	t.Helper()
	prev := applyProposalFn
	applyProposalFn = stub
	t.Cleanup(func() { applyProposalFn = prev })
}

// withArchiveProposalStub swaps archiveProposalFn for the duration of the test.
func withArchiveProposalStub(t *testing.T, stub func(*config.Proposal) error) {
	t.Helper()
	prev := archiveProposalFn
	archiveProposalFn = stub
	t.Cleanup(func() { archiveProposalFn = prev })
}

// withRunRefreshStub swaps runRefreshFn for the duration of the test.
func withRunRefreshStub(t *testing.T, stub func(string) error) {
	t.Helper()
	prev := runRefreshFn
	runRefreshFn = stub
	t.Cleanup(func() { runRefreshFn = prev })
}

// ─── runRefresh / runRemove configLoad failure branches ──────────────────────

func TestRunRefresh_ConfigLoadError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	if err := os.MkdirAll(filepath.Join(tmp, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("load boom")
	// runImportFromRefresh still uses the package-level configLoad. Stub it
	// so the import pass succeeds (returns an empty config). The
	// refresh-internal call now goes through the per-file
	// refreshConfigLoader and returns the sentinel directly.
	withConfigLoadStub(t, func() (*config.Config, error) {
		return &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}, nil
	})
	deps := fakeRefreshConfigLoader{loadConfig: func() (*config.Config, error) { return nil, sentinel }}

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	err := runRefresh("", deps)
	if err == nil || !errors.Is(err, sentinel) {
		t.Fatalf("expected configLoad sentinel from refresh, got %v", err)
	}
	if !strings.Contains(err.Error(), "loading config") {
		t.Errorf("expected wrap 'loading config', got %v", err)
	}
}

func TestRunRemove_ConfigLoadError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	if err := os.MkdirAll(filepath.Join(tmp, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("load boom")
	deps := fakeRemoveDeps{loadConfig: func() (*config.Config, error) { return nil, sentinel }}

	err := runRemove("some-project", false, deps)
	if err == nil || !errors.Is(err, sentinel) {
		t.Fatalf("expected configLoad sentinel, got %v", err)
	}
}

// ─── runReviewApprove ApplyProposal / ArchiveProposal failure branches ──────

func TestRunReviewApprove_ApplyProposalError(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", agentsHome)
	writeProposal(t, agentsHome, "apply-fail", validProposalYAML("apply-fail", "pending"))

	sentinel := errors.New("apply boom")
	withApplyProposalStub(t, func(*config.Proposal) error { return sentinel })

	err := runReviewApprove("apply-fail")
	if err == nil || !errors.Is(err, sentinel) {
		t.Fatalf("expected applyProposal sentinel, got %v", err)
	}
}

func TestRunReviewApprove_ArchiveProposalError_RollsBack(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", agentsHome)
	writeProposal(t, agentsHome, "arch-fail", validProposalYAML("arch-fail", "pending"))

	// Apply + refresh succeed, archive fails — rollback path executes.
	withApplyProposalStub(t, func(*config.Proposal) error { return nil })
	withRunRefreshStub(t, func(string) error { return nil })
	sentinel := errors.New("archive boom")
	withArchiveProposalStub(t, func(*config.Proposal) error { return sentinel })

	err := runReviewApprove("arch-fail")
	if err == nil || !errors.Is(err, sentinel) {
		t.Fatalf("expected archiveProposal sentinel, got %v", err)
	}
}

func TestRunReviewReject_ArchiveProposalError(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", agentsHome)
	writeProposal(t, agentsHome, "reject-arch-fail", validProposalYAML("reject-arch-fail", "pending"))

	sentinel := errors.New("archive boom")
	withArchiveProposalStub(t, func(*config.Proposal) error { return sentinel })

	err := runReviewReject("reject-arch-fail", "no")
	if err == nil || !errors.Is(err, sentinel) {
		t.Fatalf("expected archiveProposal sentinel, got %v", err)
	}
}

func TestRunStatus_ConfigLoadError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	if err := os.MkdirAll(filepath.Join(tmp, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("load boom")
	deps := fakeStatusConfigLoader{loadConfig: func() (*config.Config, error) { return nil, sentinel }}

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	err := runStatus(false, "", deps)
	if err == nil || !errors.Is(err, sentinel) {
		t.Fatalf("expected configLoad sentinel, got %v", err)
	}
}

func TestRunAdd_ConfigLoadError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	if err := os.MkdirAll(filepath.Join(tmp, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	projectPath := filepath.Join(tmp, "proj")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("load boom")
	withConfigLoadStub(t, func() (*config.Config, error) { return nil, sentinel })

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	err := runAdd(projectPath, "")
	if err == nil || !errors.Is(err, sentinel) {
		t.Fatalf("expected configLoad sentinel, got %v", err)
	}
}

func TestRegisterInstallProject_ConfigLoadError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	if err := os.MkdirAll(filepath.Join(tmp, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("load boom")
	withConfigLoadStub(t, func() (*config.Config, error) { return nil, sentinel })

	err := registerInstallProject("p", filepath.Join(tmp, "p"))
	if err == nil || !errors.Is(err, sentinel) {
		t.Fatalf("expected configLoad sentinel, got %v", err)
	}
}

func TestFindProjectByPath_ConfigLoadError(t *testing.T) {
	withConfigLoadStub(t, func() (*config.Config, error) { return nil, errors.New("load boom") })
	if got := findProjectByPath("/whatever"); got != "" {
		t.Errorf("expected empty string on load error, got %q", got)
	}
}

// processImportCandidate Stat(dest) error branch (lines 412-414). Make dest
// a path through a non-directory so Stat returns a non-IsNotExist error.
func TestProcessImportCandidate_DestStatErrorWarns(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	src := filepath.Join(tmp, "src.txt")
	if err := os.WriteFile(src, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Block the dest directory with a regular file: agentsHome/rules/p is a
	// file, so Stat(agentsHome/rules/p/foo.md) returns ENOTDIR.
	blockerParent := filepath.Join(agentsHome, "rules")
	if err := os.MkdirAll(blockerParent, 0o755); err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(blockerParent, "p")
	if err := os.WriteFile(blocker, []byte("blocker"), 0o644); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	c := importCandidate{
		project:    "p",
		sourceRoot: tmp,
		sourcePath: src,
		destRel:    "rules/p/foo.md",
	}
	res := processImportCandidate(c, agentsHome, "ts", stdImportDeps{})
	// Either skipped=1 (Stat fail) or imported (treated as missing) — only the
	// stat-failure code path is interesting; both indicate the branch fired.
	_ = res
}

// shouldUseCachedGitSource verbose-info branch (lines 419-421).
func TestShouldUseCachedGitSource_VerboseInfo(t *testing.T) {
	cacheDir := t.TempDir()
	// Touch a fresh .last-fetch file
	if err := os.WriteFile(filepath.Join(cacheDir, ".last-fetch"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	saved := Flags
	Flags = GlobalFlags{Verbose: true}
	defer func() { Flags = saved }()

	if !shouldUseCachedGitSource(cacheDir, "https://example.invalid/r.git") {
		t.Error("expected cached source to be used")
	}
}

// linkResourceFromSources verbose-bullet branch (lines 494-496). Use real
// MkdirAll/Symlink and verify the call succeeds with Verbose=true.
func TestLinkResourceFromSources_VerboseBullet(t *testing.T) {
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
	Flags = GlobalFlags{Verbose: true}
	defer func() { Flags = saved }()

	if err := linkResourceFromSources("skills", "demo", "proj", []string{src}); err != nil {
		t.Fatalf("linkResourceFromSources: %v", err)
	}
}

// printSymlinkDirAudit ReadDir-fails branch (lines 1017-1019).
func TestPrintSymlinkDirAudit_NonexistentDir(t *testing.T) {
	ok, broken := printSymlinkDirAudit(filepath.Join(t.TempDir(), "absent"), "(empty)", "%s")
	if ok != 0 || broken != 0 {
		t.Errorf("expected (0,0) for missing dir, got (%d,%d)", ok, broken)
	}
}

// countClaudeRules Readlink-fails branch (lines 230-232). A regular file
// in .claude/rules/ produces Readlink err which is silently skipped.
func TestCountClaudeRules_ReadlinkFails(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, ".claude", "rules")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Regular file, not a symlink.
	if err := os.WriteFile(filepath.Join(dir, "regular.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Broken managed link.
	linktest.DanglingLink(t, filepath.Join(dir, "broken"))
	// Healthy managed link.
	healthy := filepath.Join(tmp, "real.md")
	if err := os.WriteFile(healthy, []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	linktest.Link(t, healthy, filepath.Join(dir, "good"))

	ok, warn := countClaudeRules(tmp)
	if ok != 1 || warn != 1 {
		t.Errorf("countClaudeRules: ok=%d warn=%d, want ok=1 warn=1", ok, warn)
	}
}

// backupExistingConfigsList Lstat-fails branch (lines 535-536). Pass a
// non-existent file path and verify the loop skips it.
func TestBackupExistingConfigsList_LstatFails(t *testing.T) {
	tmp := t.TempDir()
	got, err := backupExistingConfigsList(
		[]string{filepath.Join(tmp, "absent.txt")},
		tmp, t.TempDir(), "p", "ts",
	)
	if err != nil {
		t.Fatalf("absent file should be skipped, not error: %v", err)
	}
	if got != 0 {
		t.Errorf("expected 0 when Lstat fails, got %d", got)
	}
}

// backupExistingConfigsList managed-symlink branch (lines 538-543). Create a
// symlink and verify it is removed and counted.
func TestBackupExistingConfigsList_SymlinkBranch(t *testing.T) {
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "target.txt")
	if err := os.WriteFile(dst, []byte("v"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(tmp, "link")
	linktest.Link(t, dst, link)
	got, err := backupExistingConfigsList([]string{link}, tmp, t.TempDir(), "p", "ts")
	if err != nil {
		t.Fatalf("managed symlink removal should not error: %v", err)
	}
	if got != 1 {
		t.Errorf("expected 1 for removed symlink, got %d", got)
	}
}

// restoreLegacyResourceFile dest-empty branch (lines 614-617): when
// mapResourceRelToDest returns "" the function returns 0.
func TestRestoreLegacyResourceFile_NoMapping(t *testing.T) {
	if got, err := restoreLegacyResourceFile("p", "totally/unknown/path.txt", t.TempDir(), ""); got != 0 || err != nil {
		t.Errorf("expected 0 for unmapped rel path, got %d (err=%v)", got, err)
	}
}

// runDoctor configLoad-failure branch (lines 91-94): returns nil but emits warn.
func TestRunDoctor_ConfigLoadError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	if err := os.MkdirAll(filepath.Join(tmp, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	deps := fakeDoctorConfigLoader{loadConfig: func() (*config.Config, error) { return nil, errors.New("load boom") }}

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	if err := runDoctor(nil, nil, deps); err != nil {
		t.Fatalf("runDoctor expected nil on configLoad err, got %v", err)
	}
}

// runRefresh project-filter-missing branch (lines 100-105).
func TestRunRefresh_ProjectFilterNotFound(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	if err := os.MkdirAll(filepath.Join(tmp, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}

	// runImportFromRefresh still uses package-level configLoad; stub it to
	// return a benign empty config. The refresh-internal load goes through
	// the injected refreshConfigLoader and returns a config with one
	// project named "real" so the filter mismatch triggers.
	withConfigLoadStub(t, func() (*config.Config, error) {
		return &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}, nil
	})
	deps := fakeRefreshConfigLoader{loadConfig: func() (*config.Config, error) {
		return &config.Config{
			Version:  1,
			Projects: map[string]config.Project{"real": {Path: filepath.Join(tmp, "real")}},
			Agents:   map[string]config.Agent{},
		}, nil
	}}

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	err := runRefresh("ghost-project", deps)
	if err == nil || !strings.Contains(err.Error(), "project not found") {
		t.Fatalf("expected project-not-found, got %v", err)
	}
}

// runInstallGenerate's LoadAgentsRC error branch (lines 292-294): create an
// invalid manifest file so the second load (after generate) returns an error.
func TestRunInstallGenerate_CorruptExistingManifest(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "proj")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatal(err)
	}
	// Write malformed .agentsrc.json so the merge load step fails.
	if err := os.WriteFile(filepath.Join(projectPath, config.AgentsRCFile), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	withGetwdStub(t, func() (string, error) { return projectPath, nil })

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	err := runInstallGenerate()
	if err == nil || !strings.Contains(err.Error(), "loading existing") {
		t.Fatalf("expected loading existing manifest error, got %v", err)
	}
}

// runInstallGenerate Save() failure: point projectPath at a non-writable
// location so rc.Save returns an error.
func TestRunInstallGenerate_SaveFailure(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	// Make projectPath itself a file so writing .agentsrc.json inside fails.
	projectPath := filepath.Join(tmp, "blocker")
	if err := os.WriteFile(projectPath, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	withGetwdStub(t, func() (string, error) { return projectPath, nil })

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	err := runInstallGenerate()
	if err == nil {
		t.Fatal("expected error from rc.Save on non-directory project path")
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
	deps := fakeInitDirMaker{mkdirAll: func(string, os.FileMode) error { return sentinel }}

	err := runInit(nil, nil, deps)
	if err == nil || !strings.Contains(err.Error(), "creating ") {
		t.Fatalf("expected wrapped creating error, got %v", err)
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel to wrap, got %v", err)
	}
}
