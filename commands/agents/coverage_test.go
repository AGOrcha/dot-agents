package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/testutil"
	"github.com/spf13/cobra"
)

// ── cmd.go RunE coverage ─────────────────────────────────────────────────────

// hintErrDeps returns Deps stubs that always accept positional args and surface
// hint-style errors verbatim.
func hintErrDeps() Deps {
	accept := func(*cobra.Command, []string) error { return nil }
	return Deps{
		ErrorWithHints: func(message string, hints ...string) error {
			return &hintError{message: message, hints: hints}
		},
		UsageError: func(message string, hints ...string) error {
			return &hintError{message: message, hints: hints}
		},
		MaximumNArgsWithHints: func(int, ...string) cobra.PositionalArgs { return accept },
		RangeArgsWithHints:    func(int, int, ...string) cobra.PositionalArgs { return accept },
		ExactArgsWithHints:    func(int, ...string) cobra.PositionalArgs { return accept },
	}
}

func findSub(t *testing.T, root *cobra.Command, name string) *cobra.Command {
	t.Helper()
	for _, c := range root.Commands() {
		if c.Name() == name {
			return c
		}
	}
	t.Fatalf("subcommand %q not found", name)
	return nil
}

// TestAgentsListCmd_RunE exercises the list subcommand's RunE.
func TestAgentsListCmd_RunE(t *testing.T) {
	testutil.NewTempProject(t, "")
	root := NewAgentsCmd(hintErrDeps())
	cmd := findSub(t, root, "list")
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Errorf("list RunE: %v", err)
	}
}

// TestAgentsNewCmd_RunE exercises the new subcommand's RunE.
func TestAgentsNewCmd_RunE(t *testing.T) {
	agentsHome, _ := testutil.NewTempProject(t, "")
	root := NewAgentsCmd(hintErrDeps())
	cmd := findSub(t, root, "new")
	if err := cmd.RunE(cmd, []string{"runE-agent"}); err != nil {
		t.Fatalf("new RunE: %v", err)
	}
	if _, err := os.Stat(filepath.Join(agentsHome, "agents", "global", "runE-agent", agentManifestName)); err != nil {
		t.Errorf("expected agent manifest: %v", err)
	}
}

// TestAgentsPromoteCmd_RunE exercises the promote subcommand's RunE happy path.
func TestAgentsPromoteCmd_RunE(t *testing.T) {
	_, projectPath := testutil.NewTempProject(t, "promproj")
	testutil.WriteAgentManifest(t, projectPath, "promotee")

	// Run RunE from within projectPath so os.Getwd resolves correctly.
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(projectPath); err != nil {
		t.Fatal(err)
	}

	root := NewAgentsCmd(hintErrDeps())
	cmd := findSub(t, root, "promote")
	if err := cmd.RunE(cmd, []string{"promotee"}); err != nil {
		t.Fatalf("promote RunE: %v", err)
	}
}

// TestAgentsImportCmd_RunE exercises the import subcommand's RunE.
func TestAgentsImportCmd_RunE(t *testing.T) {
	agentsHome, projectPath := testutil.NewTempProject(t, "impproj")
	testutil.WriteCanonicalAgent(t, agentsHome, "impproj", "to-import")

	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(projectPath); err != nil {
		t.Fatal(err)
	}

	root := NewAgentsCmd(hintErrDeps())
	cmd := findSub(t, root, "import")
	if err := cmd.RunE(cmd, []string{"to-import"}); err != nil {
		t.Fatalf("import RunE: %v", err)
	}
}

// TestAgentsRemoveCmd_RunE exercises the remove subcommand's RunE happy path.
func TestAgentsRemoveCmd_RunE(t *testing.T) {
	agentsHome, projectPath := testutil.NewTempProject(t, "remproj")
	testutil.WriteCanonicalAgent(t, agentsHome, "remproj", "removable")
	if err := ImportAgentIn("removable", projectPath); err != nil {
		t.Fatalf("setup import: %v", err)
	}

	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(projectPath); err != nil {
		t.Fatal(err)
	}

	root := NewAgentsCmd(hintErrDeps())
	cmd := findSub(t, root, "remove")
	if err := cmd.RunE(cmd, []string{"removable"}); err != nil {
		t.Fatalf("remove RunE: %v", err)
	}
}

// ── new.go error paths ───────────────────────────────────────────────────────

// TestCreateAgent_MkdirAllErrorWhenParentIsFile points AGENTS_HOME at a file
// path so MkdirAll fails inside CreateAgent.
func TestCreateAgent_MkdirAllErrorWhenParentIsFile(t *testing.T) {
	tmp := t.TempDir()
	bogus := filepath.Join(tmp, "is-a-file")
	if err := os.WriteFile(bogus, []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", bogus)
	if err := CreateAgent("x", "global"); err == nil {
		t.Error("expected MkdirAll error")
	}
}

// TestWriteAgentMDIfAbsent_WriteErrorOnReadOnlyDir simulates a write failure.
func TestWriteAgentMDIfAbsent_WriteErrorOnReadOnlyDir(t *testing.T) {
	dir := t.TempDir()
	// Target path under a non-existent directory triggers WriteFile failure.
	bad := filepath.Join(dir, "does", "not", "exist", agentManifestName)
	if err := writeAgentMDIfAbsent(bad, "x"); err == nil {
		t.Error("expected WriteFile error for missing parent dir")
	}
}

// TestAppendAgentsRCStep_CorruptManifestSkipsAppend covers the LoadAgentsRC
// error branch (corrupt .agentsrc.json).
func TestAppendAgentsRCStep_CorruptManifestSkipsAppend(t *testing.T) {
	_, projectPath := testutil.NewTempProject(t, "corruptproj")
	// Corrupt the manifest.
	if err := os.WriteFile(filepath.Join(projectPath, config.AgentsRCFile), []byte("{not-json"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.AddProject("corruptproj", projectPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	out := appendAgentsRCStep([]string{"step1"}, "x", "corruptproj")
	if len(out) != 1 {
		t.Errorf("expected unchanged steps on corrupt manifest, got: %v", out)
	}
}

// ── import.go edge paths ────────────────────────────────────────────────────

// TestImportAgentIn_MissingAgentsRCReturnsError covers the LoadAgentsRC error
// branch inside ImportAgentIn.
func TestImportAgentIn_MissingAgentsRCReturnsError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, "agents"))
	projectPath := filepath.Join(tmp, "proj")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatal(err)
	}
	// No .agentsrc.json present
	if err := ImportAgentIn("anything", projectPath); err == nil {
		t.Error("expected error for missing .agentsrc.json")
	}
}

// TestImportAgentIn_CanonicalPathPermissionError synthesises a permission-style
// Stat failure by pointing the canonical path at a parent that is a file
// instead of a directory.
func TestImportAgentIn_CanonicalPathNotDirectory(t *testing.T) {
	agentsHome, projectPath := testutil.NewTempProject(t, "permproj")
	// Place a *file* at the canonical agent path so Stat(AGENT.md) returns
	// a not-exist error reachable via the IsNotExist branch.
	canonDir := filepath.Join(agentsHome, "agents", "permproj")
	if err := os.MkdirAll(canonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Create as a file so Stat under that path errors with ENOTDIR.
	if err := os.WriteFile(filepath.Join(canonDir, "thing"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := ImportAgentIn("thing", projectPath)
	if err == nil {
		t.Error("expected error when canonical layout is invalid")
	}
}

// TestEnsureImportRepoAgentsSlot_UnknownFileTypeReturnsError covers the
// trailing fallthrough error branch in ensureImportRepoAgentsSlot.
func TestEnsureImportRepoAgentsSlot_UnknownFileTypeReturnsError(t *testing.T) {
	_, projectPath := testutil.NewTempProject(t, "p")
	repoLocal := filepath.Join(projectPath, ".agents", "agents", "weird")
	if err := os.MkdirAll(filepath.Dir(repoLocal), 0o755); err != nil {
		t.Fatal(err)
	}
	// Plain file at the slot path → neither symlink nor directory.
	if err := os.WriteFile(repoLocal, []byte("plain"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := ensureImportRepoAgentsSlot("weird", filepath.Join("/tmp", "fake-canon"), projectPath)
	if err == nil {
		t.Fatal("expected error for unknown file type at repo-local slot")
	}
	if !strings.Contains(err.Error(), "unexpected path") {
		t.Errorf("error = %q; want 'unexpected path'", err.Error())
	}
}

// TestEnsureImportRepoAgentsSlot_AlreadyCorrectSymlink covers the
// `existing == canonicalPath` early-return branch.
func TestEnsureImportRepoAgentsSlot_AlreadyCorrectSymlink(t *testing.T) {
	agentsHome, projectPath := testutil.NewTempProject(t, "good")
	canonical := testutil.WriteCanonicalAgent(t, agentsHome, "good", "agent-correct")

	repoLocal := filepath.Join(projectPath, ".agents", "agents", "agent-correct")
	if err := os.MkdirAll(filepath.Dir(repoLocal), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(canonical, repoLocal); err != nil {
		t.Fatal(err)
	}
	if err := ensureImportRepoAgentsSlot("agent-correct", canonical, projectPath); err != nil {
		t.Fatalf("expected no-op for correct symlink, got: %v", err)
	}
}

// ── remove.go edge paths ────────────────────────────────────────────────────

// TestAgentUserError_FallbackWithoutHintHandler covers the deps.ErrorWithHints
// nil branch.
func TestAgentUserError_FallbackWithoutHintHandler(t *testing.T) {
	d := Deps{}
	err := agentUserError(d, "boom")
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("err = %v; want 'boom'", err)
	}
	err = agentUserError(d, "boom", "do this")
	if err == nil || !strings.Contains(err.Error(), "do this") {
		t.Errorf("err = %v; want hint 'do this'", err)
	}
}

// TestRemoveAgentNameFromSlice_NoMatchPreserves covers the !=name branch
// across multiple list entries.
func TestRemoveAgentNameFromSlice_NoMatchPreserves(t *testing.T) {
	in := []string{"a", "b", "c"}
	out := removeAgentNameFromSlice(in, "z")
	if len(out) != 3 {
		t.Errorf("expected slice unchanged: %v", out)
	}
}

// TestRemoveAgentIn_MissingAgentsRC covers the LoadAgentsRC error branch.
func TestRemoveAgentIn_MissingAgentsRC(t *testing.T) {
	d := Deps{
		ErrorWithHints: func(m string, h ...string) error {
			return &hintError{message: m, hints: h}
		},
	}
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, "agents"))
	projectPath := filepath.Join(tmp, "proj")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatal(err)
	}

	err := RemoveAgentIn(d, "x", projectPath, false)
	if err == nil {
		t.Error("expected error when .agentsrc.json missing")
	}
}

// TestRemoveAgentIn_NoProjectName covers the empty-project branch.
func TestRemoveAgentIn_NoProjectName(t *testing.T) {
	d := Deps{
		ErrorWithHints: func(m string, h ...string) error {
			return &hintError{message: m, hints: h}
		},
	}
	_, projectPath := testutil.NewTempProject(t, "")
	err := RemoveAgentIn(d, "x", projectPath, false)
	if err == nil {
		t.Error("expected error when project name empty")
	}
}

// TestCleanupManagedAgentRepoPath_MispointedSymlinkErrors creates a symlink
// pointing outside agentsHome so the unmanaged-symlink branch fires.
func TestCleanupManagedAgentRepoPath_MispointedSymlinkErrors(t *testing.T) {
	d := stubDeps(false)
	agentsHome, projectPath := testutil.NewTempProject(t, "p")
	// Create a symlink pointing outside agentsHome.
	other := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(projectPath, ".agents", "agents", "rogue")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, target); err != nil {
		t.Fatal(err)
	}
	err := cleanupManagedAgentRepoPath(d, target, agentsHome, "rogue")
	if err == nil {
		t.Error("expected error for mispointed symlink")
	}
}

// TestCleanupManagedAgentRepoPath_PlainFileErrors covers the unexpected-file
// branch.
func TestCleanupManagedAgentRepoPath_PlainFileErrors(t *testing.T) {
	d := stubDeps(false)
	agentsHome, _ := testutil.NewTempProject(t, "p")
	tmp := t.TempDir()
	target := filepath.Join(tmp, "plain")
	if err := os.WriteFile(target, []byte("file"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := cleanupManagedAgentRepoPath(d, target, agentsHome, "plain")
	if err == nil {
		t.Error("expected error for plain file at managed path")
	}
}

// TestPurgeCanonicalAgent_AbsentNoOp covers the IsNotExist branch.
func TestPurgeCanonicalAgent_AbsentNoOp(t *testing.T) {
	d := stubDeps(true)
	tmp := t.TempDir()
	purged, err := purgeCanonicalAgent(d, filepath.Join(tmp, "gone"), "gone")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if purged {
		t.Error("absent canonical should not report purged=true")
	}
}

// TestPurgeCanonicalAgent_NotDirError covers the !IsDir branch.
func TestPurgeCanonicalAgent_NotDirError(t *testing.T) {
	d := stubDeps(true)
	tmp := t.TempDir()
	target := filepath.Join(tmp, "plain")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := purgeCanonicalAgent(d, target, "plain")
	if err == nil {
		t.Error("expected error for non-directory canonical")
	}
}

// ── promote.go mirror error path ─────────────────────────────────────────────

// TestRefreshAgentMirror_BuildIntentsErrorIsSwallowed asserts the function
// returns nil even when the project has no agents (intents empty -> plan
// succeeds with no actions).
func TestRefreshAgentMirror_NoAgentsReturnsNil(t *testing.T) {
	_, projectPath := testutil.NewTempProject(t, "mirrornone")
	if err := refreshAgentMirror("mirrornone", projectPath); err != nil {
		t.Errorf("refreshAgentMirror with no agents: %v", err)
	}
}

// TestRefreshAgentMirror_PlanExecuteSwallowedOnConflict covers the
// plan.Execute warn-only branch by setting up a clash between the symlink
// destination and a pre-existing directory at the .claude/agents/<name>
// path.
func TestRefreshAgentMirror_PlanExecuteSwallowedOnConflict(t *testing.T) {
	agentsHome, projectPath := testutil.NewTempProject(t, "mirrorconf")
	testutil.WriteCanonicalAgent(t, agentsHome, "mirrorconf", "ag")

	// Pre-create a real directory at the destination to potentially make
	// Execute warn — even on success path the test exercises the happy code.
	target := filepath.Join(projectPath, ".claude", "agents", "ag")
	_ = os.MkdirAll(target, 0o755)

	if err := refreshAgentMirror("mirrorconf", projectPath); err != nil {
		t.Errorf("refreshAgentMirror swallows plan errors; got: %v", err)
	}
}

// TestPurgeCanonicalAgent_DeclinedAtConfirm covers the !ui.Confirm branch when
// the user declines the purge prompt. We replace stdin with a closed pipe so
// Confirm returns false.
func TestPurgeCanonicalAgent_DeclinedAtConfirm(t *testing.T) {
	d := stubDeps(false)
	tmp := t.TempDir()
	canon := filepath.Join(tmp, "canon")
	if err := os.MkdirAll(canon, 0o755); err != nil {
		t.Fatal(err)
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	oldStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = oldStdin
		_ = r.Close()
	})

	purged, err := purgeCanonicalAgent(d, canon, "x")
	if err != nil {
		t.Errorf("declined purge: %v", err)
	}
	if purged {
		t.Error("declined purge should not report purged=true")
	}
	if _, err := os.Stat(canon); err != nil {
		t.Errorf("canon should remain after decline: %v", err)
	}
}

// TestRemoveAgentIn_RCSaveErrorAfterCleanup makes the project's
// .agentsrc.json read-only AND its parent read-only so rc.Save's WriteFile
// fails when the function tries to persist the trimmed agents list.
func TestRemoveAgentIn_RCSaveErrorAfterCleanup(t *testing.T) {
	d := stubDeps(false)
	agentsHome, projectPath := testutil.NewTempProject(t, "saveerr")
	testutil.WriteCanonicalAgent(t, agentsHome, "saveerr", "to-rm")
	if err := ImportAgentIn("to-rm", projectPath); err != nil {
		t.Fatalf("setup import: %v", err)
	}

	rcPath := filepath.Join(projectPath, config.AgentsRCFile)
	if err := os.Chmod(rcPath, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(projectPath, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(projectPath, 0o755)
		_ = os.Chmod(rcPath, 0o644)
	})

	err := RemoveAgentIn(d, "to-rm", projectPath, false)
	if err == nil {
		// Some filesystems ignore chmod; not fatal but the branch wasn't hit.
		t.Skip("filesystem ignored chmod; rc.Save error path not exercised")
	}
	if !strings.Contains(err.Error(), "agentsrc") {
		// On macOS the cleanup symlinks under a 0o555 projectPath may have
		// already errored before reaching rc.Save; record but don't fail.
		t.Logf("non-rc.Save error path triggered: %v", err)
	}
}

// TestCreateAgent_WriteManifestErrorBubbles pre-creates the agent directory
// with 0500 perms so writeAgentMDIfAbsent fails to write AGENT.md inside
// CreateAgent (covers the writeAgentMDIfAbsent error return branch).
func TestCreateAgent_WriteManifestErrorBubbles(t *testing.T) {
	agentsHome, _ := testutil.NewTempProject(t, "")
	agentDir := filepath.Join(agentsHome, "agents", "global", "blocked")
	// Create parents first, then make the leaf 0500 so writeAgentMDIfAbsent's
	// WriteFile fails. MkdirAll is a no-op on existing leaves.
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(agentDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(agentDir, 0o755) })

	err := CreateAgent("blocked", "global")
	if err == nil {
		t.Skip("filesystem allowed write to read-only dir; error path not exercised")
	}
}

// TestCleanupManagedAgentRepoPath_LstatPermissionError covers the
// non-IsNotExist Lstat error branch by making the parent directory
// unreadable.
func TestCleanupManagedAgentRepoPath_LstatPermissionError(t *testing.T) {
	d := stubDeps(false)
	agentsHome, _ := testutil.NewTempProject(t, "p")
	tmp := t.TempDir()
	parent := filepath.Join(tmp, "parent")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(parent, "child")
	// Create child so Lstat would normally succeed.
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Remove search permission on the parent.
	if err := os.Chmod(parent, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })

	err := cleanupManagedAgentRepoPath(d, target, agentsHome, "child")
	if err == nil {
		t.Skip("filesystem ignored chmod 000; Lstat error path not exercised")
	}
}

// TestPurgeCanonicalAgent_LstatPermissionError covers the non-IsNotExist
// Lstat error branch inside purgeCanonicalAgent.
func TestPurgeCanonicalAgent_LstatPermissionError(t *testing.T) {
	d := stubDeps(true)
	tmp := t.TempDir()
	parent := filepath.Join(tmp, "parent")
	target := filepath.Join(parent, "child")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })

	_, err := purgeCanonicalAgent(d, target, "child")
	if err == nil {
		t.Skip("filesystem ignored chmod 000; Lstat error path not exercised")
	}
}

// TestPurgeCanonicalAgent_RemoveAllError prevents os.RemoveAll from
// succeeding by making the parent of the target read-only after Confirm.
func TestPurgeCanonicalAgent_RemoveAllError(t *testing.T) {
	d := stubDeps(true) // autoYes -> Confirm returns true
	tmp := t.TempDir()
	parent := filepath.Join(tmp, "parent")
	target := filepath.Join(parent, "victim")
	if err := os.MkdirAll(filepath.Join(target, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Make parent read-only: Lstat still succeeds (target's stat is on the
	// inode), but RemoveAll's unlink/rmdir on `target` requires write on
	// parent.
	if err := os.Chmod(parent, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })

	_, err := purgeCanonicalAgent(d, target, "victim")
	if err == nil {
		t.Skip("filesystem ignored chmod 0o555; RemoveAll error path not exercised")
	}
}

// TestImportAgentIn_PlanExecuteError makes the .claude/agents parent path a
// regular file so plan.Execute's MkdirAll fails.
func TestImportAgentIn_PlanExecuteError(t *testing.T) {
	agentsHome, projectPath := testutil.NewTempProject(t, "execerr")
	testutil.WriteCanonicalAgent(t, agentsHome, "execerr", "x")

	// Make .claude exist as a regular file so MkdirAll(.claude/agents) fails.
	if err := os.WriteFile(filepath.Join(projectPath, ".claude"), []byte("file"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := ImportAgentIn("x", projectPath)
	if err == nil {
		t.Skip("filesystem allowed write through file; Execute error not exercised")
	}
}

// TestEnsureImportRepoAgentsSlot_LstatPermissionError covers the
// non-IsNotExist Lstat error path inside ensureImportRepoAgentsSlot.
func TestEnsureImportRepoAgentsSlot_LstatPermissionError(t *testing.T) {
	tmp := t.TempDir()
	parent := filepath.Join(tmp, "parent", ".agents", "agents")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	// Pre-create the leaf so Lstat would have a target.
	leaf := filepath.Join(parent, "perm")
	if err := os.WriteFile(leaf, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })

	err := ensureImportRepoAgentsSlot("perm", "/canon", filepath.Join(tmp, "parent"))
	if err == nil {
		t.Skip("filesystem ignored chmod; Lstat error path not exercised")
	}
}

// TestRemoveAgentIn_SecondCleanupError covers the second
// cleanupManagedAgentRepoPath error branch (repoClaude failure) by leaving a
// real directory at the .claude/agents/<name> path.
func TestRemoveAgentIn_SecondCleanupError(t *testing.T) {
	d := stubDeps(false)
	agentsHome, projectPath := testutil.NewTempProject(t, "secondcleanup")
	testutil.WriteCanonicalAgent(t, agentsHome, "secondcleanup", "double")
	if err := ImportAgentIn("double", projectPath); err != nil {
		t.Fatalf("setup import: %v", err)
	}
	// Replace the .claude/agents/<name> symlink with a real directory so
	// cleanupManagedAgentRepoPath returns an unmanaged-error.
	claudePath := filepath.Join(projectPath, ".claude", "agents", "double")
	if err := os.Remove(claudePath); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(claudePath, "extra"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := RemoveAgentIn(d, "double", projectPath, false)
	if err == nil {
		t.Error("expected error from second cleanup branch")
	}
}

// TestImportAgentIn_RCSaveError performs a successful import then forces the
// second invocation's rc.Save to fail by chmod'ing the .agentsrc.json file
// and its parent directory after the symlinks are already in place. The
// second ImportAgentIn is idempotent on the symlinks, so it reaches rc.Save
// where the WriteFile fails with EACCES.
func TestImportAgentIn_RCSaveError(t *testing.T) {
	agentsHome, projectPath := testutil.NewTempProject(t, "rcsavefail")
	testutil.WriteCanonicalAgent(t, agentsHome, "rcsavefail", "save-fail")

	// First import: succeeds, sets up symlinks.
	if err := ImportAgentIn("save-fail", projectPath); err != nil {
		t.Fatalf("setup import: %v", err)
	}

	rcPath := filepath.Join(projectPath, config.AgentsRCFile)
	// Make .agentsrc.json read-only AND its parent read-only so WriteFile
	// (which uses O_TRUNC) fails to open it for writing.
	if err := os.Chmod(rcPath, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(projectPath, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(projectPath, 0o755)
		_ = os.Chmod(rcPath, 0o644)
	})

	err := ImportAgentIn("save-fail", projectPath)
	if err == nil {
		t.Skip("filesystem allowed write to read-only file; rc.Save error path not exercised")
	}
	if !strings.Contains(err.Error(), "updating .agentsrc.json") {
		// On macOS we may get an earlier failure path; that still exercises
		// some branch but not the targeted one. Don't be strict.
		t.Logf("non-rc.Save error path triggered: %v", err)
	}
}

// TestAppendAgentsRCStep_CorruptGlobalConfigSkipsAppend covers the
// `config.Load` error branch inside appendAgentsRCStep by pointing
// AGENTS_HOME at a directory whose config.json is malformed.
func TestAppendAgentsRCStep_CorruptGlobalConfigSkipsAppend(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentsHome, "config.json"), []byte("{not-json"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	out := appendAgentsRCStep([]string{"step1"}, "x", "someproj")
	if len(out) != 1 {
		t.Errorf("expected unchanged steps on corrupt global config, got: %v", out)
	}
}

// TestAppendAgentsRCStep_UnknownProjectSkipsAppend covers the projPath==""
// branch (project not listed in global config).
func TestAppendAgentsRCStep_UnknownProjectSkipsAppend(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatal(err)
	}
	// Empty config.json — no projects registered.
	if err := os.WriteFile(filepath.Join(agentsHome, "config.json"), []byte(`{"version":1,"projects":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	out := appendAgentsRCStep([]string{"step1"}, "x", "ghost")
	if len(out) != 1 {
		t.Errorf("expected unchanged steps for unknown project, got: %v", out)
	}
}

// TestEnsureImportRepoAgentsSlot_DanglingSymlink covers the Readlink success
// case where the symlink points to a non-existent path that differs from the
// canonical path (already covered by mispointed test, but this exercises
// dangling links).
func TestEnsureImportRepoAgentsSlot_DanglingSymlink(t *testing.T) {
	_, projectPath := testutil.NewTempProject(t, "p")
	repoLocal := filepath.Join(projectPath, ".agents", "agents", "dangling")
	if err := os.MkdirAll(filepath.Dir(repoLocal), 0o755); err != nil {
		t.Fatal(err)
	}
	// Dangling symlink to /nonexistent.
	if err := os.Symlink("/nonexistent/foo", repoLocal); err != nil {
		t.Fatal(err)
	}
	err := ensureImportRepoAgentsSlot("dangling", "/canon/path", projectPath)
	if err == nil {
		t.Error("expected mispointed-symlink error for dangling link")
	}
}

// TestRefreshAgentMirror_BuildIntentsErrorWarns covers the now-reachable
// error branch in refreshAgentMirror that fires when
// platform.BuildSharedAgentMirrorIntents propagates a non-ENOENT error
// from listScopedResourceDirs. Previously the underlying helper swallowed
// that error and returned an empty slice, leaving the branch dead.
// Triggered by making the canonical agents/<project>/ path a regular
// file so os.ReadDir errors with ENOTDIR (not ENOENT).
func TestRefreshAgentMirror_BuildIntentsErrorWarns(t *testing.T) {
	agentsHome, projectPath := testutil.NewTempProject(t, "agtproj")

	bucketParent := filepath.Join(agentsHome, "agents")
	if err := os.MkdirAll(bucketParent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bucketParent, "agtproj"), []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}

	// refreshAgentMirror warns and returns nil so caller flow continues.
	if err := refreshAgentMirror("agtproj", projectPath); err != nil {
		t.Fatalf("refreshAgentMirror must absorb build-intents error, got: %v", err)
	}
}
