package platform

// codex_coverage_test.go drives the remaining error/edge branches of codex.go
// that the primary codex_test.go suite leaves uncovered: provenance-read and
// stat faults, the prune ReadDir error handling, the diverged-collision
// preserve/alt-path/review-note failure branches, the hooks MkdirAll /
// resolveHookSpec faults, and the audit printers' empty/local-file branches.
// Helpers are prefixed cxCov to avoid collisions with the shared suite.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/testutil"
)

// cxCovWriteFile writes content at path (fatal on error).
func cxCovWriteFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
}

// cxCovManagedBytes returns a minimal managed-render byte body carrying the
// provenance marker as its first line.
func cxCovManagedBytes() []byte {
	return []byte(codexManagedTomlMarker + "\nname = \"x\"\n")
}

// --- isManagedCodexToml fault branches -------------------------------------

// TestCxCovIsManagedCodexToml_LstatRealErrorPropagates covers the non-ENOENT
// Lstat branch: a candidate under an unreadable parent dir must surface the
// permission fault, never be downgraded to (false, nil).
func TestCxCovIsManagedCodexToml_LstatRealErrorPropagates(t *testing.T) {
	tmp := t.TempDir()
	parent := filepath.Join(tmp, "locked")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(parent, "x.toml")
	cxCovWriteFile(t, child, cxCovManagedBytes())
	testutil.MakeDirUnreadable(t, parent)

	if _, err := isManagedCodexToml(child); err == nil {
		t.Fatal("expected a real Lstat error under an unreadable parent, got nil")
	}
}

// TestCxCovIsManagedCodexToml_ReadFileErrorPropagates covers the ReadFile
// fault after a successful regular-file Lstat.
func TestCxCovIsManagedCodexToml_ReadFileErrorPropagates(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "x.toml")
	cxCovWriteFile(t, f, cxCovManagedBytes())
	testutil.MakeFileUnreadable(t, f)

	if _, err := isManagedCodexToml(f); err == nil {
		t.Fatal("expected a ReadFile error on an unreadable regular file, got nil")
	}
}

// TestCxCovIsManagedCodexAgentTomlFile covers the exported wrapper used by
// commands/import.go.
func TestCxCovIsManagedCodexAgentTomlFile(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "x.toml")
	cxCovWriteFile(t, f, cxCovManagedBytes())
	ok, err := IsManagedCodexAgentTomlFile(f)
	if err != nil || !ok {
		t.Fatalf("IsManagedCodexAgentTomlFile = (%v,%v), want (true,nil)", ok, err)
	}
}

// --- codexReadUsageStats empty branch --------------------------------------

// TestCxCovCodexReadUsageStats_AllLinesInvalidReturnsNil covers the
// len(all)==0 branch when every line fails to unmarshal.
func TestCxCovCodexReadUsageStats_AllLinesInvalidReturnsNil(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cxCovWriteFile(t, filepath.Join(dir, "session_index.jsonl"), []byte("not json\n[[[\n"))
	if stats := codexReadUsageStats(home); stats != nil {
		t.Fatalf("expected nil stats when all lines are invalid, got %+v", stats)
	}
}

// --- hooks write faults ----------------------------------------------------

// TestCxCovWriteRepoHooks_MkdirAllErrorSurfaces covers the .codex MkdirAll
// fault in writeRepoHooks.
func TestCxCovWriteRepoHooks_MkdirAllErrorSurfaces(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	c := &codex{io: withMkdirAllError(t, codexDir)}
	if err := c.writeRepoHooks("proj", repo, agentsHome); err == nil {
		t.Fatal("expected writeRepoHooks to surface the MkdirAll fault")
	}
}

// TestCxCovWriteUserHomeHooks_ResolveHookSpecErrorSurfaces covers the
// resolveHookSpec fault branch: the global-scope canonical scan succeeds
// (absent bucket) but the project-scope hooks dir is unreadable, so the
// codex.json resolution aborts with a real Stat error.
func TestCxCovWriteUserHomeHooks_ResolveHookSpecErrorSurfaces(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	projHooks := filepath.Join(agentsHome, "hooks", "proj")
	if err := os.MkdirAll(projHooks, 0o755); err != nil {
		t.Fatal(err)
	}
	testutil.MakeDirUnreadable(t, projHooks)

	c := &codex{io: stdPlatformIO{}}
	if err := c.writeUserHomeHooks("proj", agentsHome); err == nil {
		t.Fatal("expected writeUserHomeHooks to surface the resolveHookSpec Stat error")
	}
}

// --- prune branches --------------------------------------------------------

// TestCxCovPruneUnwantedCodexTomls_AbsentDirNoop covers the ENOENT ReadDir
// branch (absent dstRoot is a converged no-op).
func TestCxCovPruneUnwantedCodexTomls_AbsentDirNoop(t *testing.T) {
	c := &codex{io: stdPlatformIO{}}
	if err := c.pruneUnwantedCodexTomls(filepath.Join(t.TempDir(), "missing"), map[string]bool{}); err != nil {
		t.Fatalf("absent dstRoot must be a no-op, got %v", err)
	}
}

// TestCxCovPruneUnwantedCodexTomls_UnlistableDirErrors covers the
// present-but-unlistable ReadDir fault.
func TestCxCovPruneUnwantedCodexTomls_UnlistableDirErrors(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	testutil.MakeDirUnreadable(t, dir)
	c := &codex{io: stdPlatformIO{}}
	if err := c.pruneUnwantedCodexTomls(dir, map[string]bool{}); err == nil {
		t.Fatal("expected an error for a present-but-unlistable dstRoot")
	}
}

// TestCxCovPruneManagedCodexTomlEntry_ProvenanceErrorSurfaces covers the
// provenance-read fault branch.
func TestCxCovPruneManagedCodexTomlEntry_ProvenanceErrorSurfaces(t *testing.T) {
	dir := t.TempDir()
	candidate := filepath.Join(dir, "bad.toml")
	cxCovWriteFile(t, candidate, cxCovManagedBytes())
	testutil.MakeFileUnreadable(t, candidate)
	c := &codex{io: stdPlatformIO{}}
	if err := c.pruneManagedCodexTomlEntry(candidate); err == nil {
		t.Fatal("expected a provenance-read error for an unreadable candidate")
	}
}

// TestCxCovPruneManagedCodexTomlEntry_RemoveErrorSurfaces covers the
// io.Remove fault on a proven-managed candidate.
func TestCxCovPruneManagedCodexTomlEntry_RemoveErrorSurfaces(t *testing.T) {
	dir := t.TempDir()
	candidate := filepath.Join(dir, "managed.toml")
	cxCovWriteFile(t, candidate, cxCovManagedBytes())
	c := &codex{io: withRemoveError(t, "managed.toml")}
	if err := c.pruneManagedCodexTomlEntry(candidate); err == nil {
		t.Fatal("expected the io.Remove fault to surface")
	}
}

// TestCxCovPruneManagedCodexAgentTomls_StatRealErrorSurfaces covers the
// non-ENOENT Stat branch (dstRoot under an unreadable parent).
func TestCxCovPruneManagedCodexAgentTomls_StatRealErrorSurfaces(t *testing.T) {
	tmp := t.TempDir()
	parent := filepath.Join(tmp, "locked")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	dstRoot := filepath.Join(parent, "agents")
	if err := os.MkdirAll(dstRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	testutil.MakeDirUnreadable(t, parent)
	c := &codex{io: stdPlatformIO{}}
	if err := c.pruneManagedCodexAgentTomls(dstRoot); err == nil {
		t.Fatal("expected a real Stat error under an unreadable parent")
	}
}

// TestCxCovPruneManagedCodexAgentTomls_UnlistableDirErrors covers the ReadDir
// fault after a successful IsDir stat.
func TestCxCovPruneManagedCodexAgentTomls_UnlistableDirErrors(t *testing.T) {
	dstRoot := filepath.Join(t.TempDir(), "agents")
	if err := os.MkdirAll(dstRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	testutil.MakeDirUnreadable(t, dstRoot)
	c := &codex{io: stdPlatformIO{}}
	if err := c.pruneManagedCodexAgentTomls(dstRoot); err == nil {
		t.Fatal("expected a ReadDir error for a present-but-unlistable dstRoot")
	}
}

// TestCxCovPruneManagedCodexAgentTomls_SkipsNonTomlAggregatesProvErr covers the
// non-.toml continue branch AND the provenance-read aggregation branch in the
// same pass: a plain .txt is skipped, an unreadable .toml surfaces a
// provenance fault, and a managed .toml is removed.
func TestCxCovPruneManagedCodexAgentTomls_SkipsNonTomlAggregatesProvErr(t *testing.T) {
	dstRoot := t.TempDir()
	cxCovWriteFile(t, filepath.Join(dstRoot, "note.txt"), []byte("ignore me"))
	cxCovWriteFile(t, filepath.Join(dstRoot, "good.toml"), cxCovManagedBytes())
	bad := filepath.Join(dstRoot, "bad.toml")
	cxCovWriteFile(t, bad, cxCovManagedBytes())
	testutil.MakeFileUnreadable(t, bad)

	c := &codex{io: stdPlatformIO{}}
	if err := c.pruneManagedCodexAgentTomls(dstRoot); err == nil {
		t.Fatal("expected the unreadable .toml provenance fault to be aggregated")
	}
	if _, err := os.Stat(filepath.Join(dstRoot, "good.toml")); !os.IsNotExist(err) {
		t.Errorf("managed good.toml should have been pruned, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dstRoot, "note.txt")); err != nil {
		t.Errorf("non-.toml note.txt must be left intact, got %v", err)
	}
}

// --- writeCodexAgentTomlFile provenance + skip branches ---------------------

// TestCxCovWriteCodexAgentTomlFile_ProvenanceCheckErrorSurfaces covers the
// isManagedCodexToml fault branch inside writeCodexAgentTomlFile: an
// unreadable regular occupant makes the provenance check error out.
func TestCxCovWriteCodexAgentTomlFile_ProvenanceCheckErrorSurfaces(t *testing.T) {
	tmp := t.TempDir()
	agent := filepath.Join(tmp, "AGENT.md")
	cxCovWriteFile(t, agent, []byte("---\nname: x\n---\nbody\n"))
	dst := filepath.Join(tmp, "x.toml")
	cxCovWriteFile(t, dst, cxCovManagedBytes())
	testutil.MakeFileUnreadable(t, dst)

	if err := writeCodexAgentTomlFile(stdPlatformIO{}, dst, agent); err == nil {
		t.Fatal("expected a provenance-check error for an unreadable occupant")
	}
}

// TestCxCovWriteCodexAgentTomlFile_ManagedIdenticalSkipsRewrite covers the
// steady-state skip=true path: a managed render already byte-equal to what
// would be produced is a no-op rewrite.
func TestCxCovWriteCodexAgentTomlFile_ManagedIdenticalSkipsRewrite(t *testing.T) {
	tmp := t.TempDir()
	agent := filepath.Join(tmp, "AGENT.md")
	cxCovWriteFile(t, agent, []byte("---\nname: x\n---\nbody\n"))
	body, err := renderCodexAgentToml(agent)
	if err != nil {
		t.Fatal(err)
	}
	managed := append([]byte(codexManagedTomlMarker+"\n"), body...)
	dst := filepath.Join(tmp, "x.toml")
	cxCovWriteFile(t, dst, managed)
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	before := info.ModTime()

	if err := writeCodexAgentTomlFile(stdPlatformIO{}, dst, agent); err != nil {
		t.Fatalf("writeCodexAgentTomlFile: %v", err)
	}
	after, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before) {
		t.Errorf("steady-state managed render should not be rewritten (mtime changed)")
	}
}

// --- handleUnmanagedCodexTomlOccupant Lstat fault --------------------------

// TestCxCovHandleUnmanagedCodexTomlOccupant_LstatRealErrorSurfaces covers the
// non-ENOENT Lstat branch by calling the helper directly with a dst under an
// unreadable parent (this branch is unreachable through
// writeCodexAgentTomlFile because the provenance Lstat runs first).
func TestCxCovHandleUnmanagedCodexTomlOccupant_LstatRealErrorSurfaces(t *testing.T) {
	tmp := t.TempDir()
	parent := filepath.Join(tmp, "locked")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(parent, "x.toml")
	cxCovWriteFile(t, dst, []byte("occupant"))
	testutil.MakeDirUnreadable(t, parent)

	if err := handleUnmanagedCodexTomlOccupant(stdPlatformIO{}, dst, []byte("render")); err == nil {
		t.Fatal("expected a real Lstat error under an unreadable parent")
	}
}

// --- prepareManagedCodexTomlRewrite branches -------------------------------

// TestCxCovPrepareManagedCodexTomlRewrite_IdenticalSkips covers skip=true.
func TestCxCovPrepareManagedCodexTomlRewrite_IdenticalSkips(t *testing.T) {
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "x.toml")
	managed := cxCovManagedBytes()
	cxCovWriteFile(t, dst, managed)
	skip, err := prepareManagedCodexTomlRewrite(stdPlatformIO{}, dst, managed)
	if err != nil || !skip {
		t.Fatalf("prepareManagedCodexTomlRewrite = (%v,%v), want (true,nil)", skip, err)
	}
}

// TestCxCovPrepareManagedCodexTomlRewrite_ReadErrorSurfaces covers the
// non-ENOENT ReadFile fault branch.
func TestCxCovPrepareManagedCodexTomlRewrite_ReadErrorSurfaces(t *testing.T) {
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "x.toml")
	cxCovWriteFile(t, dst, cxCovManagedBytes())
	testutil.MakeFileUnreadable(t, dst)
	if _, err := prepareManagedCodexTomlRewrite(stdPlatformIO{}, dst, []byte("new")); err == nil {
		t.Fatal("expected a ReadFile error for an unreadable prior render")
	}
}

// --- resolveUnmanagedCodexTomlCollision ReadFile fault ---------------------

// TestCxCovResolveUnmanagedCodexTomlCollision_ReadErrorSurfaces covers the
// os.ReadFile fault on a regular occupant.
func TestCxCovResolveUnmanagedCodexTomlCollision_ReadErrorSurfaces(t *testing.T) {
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "x.toml")
	cxCovWriteFile(t, dst, []byte("occupant"))
	info, err := os.Lstat(dst)
	if err != nil {
		t.Fatal(err)
	}
	testutil.MakeFileUnreadable(t, dst)
	if err := resolveUnmanagedCodexTomlCollision(stdPlatformIO{}, dst, []byte("render"), info); err == nil {
		t.Fatal("expected a ReadFile error for an unreadable regular occupant")
	}
}

// --- preserveDivergedCodexToml failure branches ----------------------------

// TestCxCovPreserveDivergedCodexToml_AltWriteErrorSurfaces covers the alt-path
// WriteFile fault.
func TestCxCovPreserveDivergedCodexToml_AltWriteErrorSurfaces(t *testing.T) {
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "x.toml")
	io := withWriteFileError(t, "codex-preexisting")
	if err := preserveDivergedCodexToml(io, dst, []byte("diverged")); err == nil {
		t.Fatal("expected the alt-path WriteFile fault to surface")
	}
}

// TestCxCovPreserveDivergedCodexToml_VacateRemoveErrorSurfaces covers the
// io.Remove(dst) fault after the alt write succeeds.
func TestCxCovPreserveDivergedCodexToml_VacateRemoveErrorSurfaces(t *testing.T) {
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "x.toml")
	cxCovWriteFile(t, dst, []byte("diverged"))
	// Remove fails only for the canonical dst (its basename "x.toml" is not a
	// substring of the alt "x.codex-preexisting.toml"), so the alt write lands
	// and the cleanup Remove(alt) still succeeds.
	io := withRemoveError(t, "x.toml")
	if err := preserveDivergedCodexToml(io, dst, []byte("diverged")); err == nil {
		t.Fatal("expected the vacate Remove fault to surface")
	}
}

// TestCxCovPreserveDivergedCodexToml_ReviewNoteFailureIsNonFatal covers the
// ui.Warn branch: a review-note write failure (bad AGENTS_HOME) is logged but
// does not fail the non-destructive preserve-and-adopt.
func TestCxCovPreserveDivergedCodexToml_ReviewNoteFailureIsNonFatal(t *testing.T) {
	tmp := t.TempDir()
	// Point AGENTS_HOME at a path under a regular file so the review-note
	// MkdirAll fails with ENOTDIR.
	blocker := filepath.Join(tmp, "afile")
	cxCovWriteFile(t, blocker, []byte("x"))
	t.Setenv("AGENTS_HOME", filepath.Join(blocker, "nested"))

	dst := filepath.Join(tmp, "x.toml")
	cxCovWriteFile(t, dst, []byte("diverged"))
	if err := preserveDivergedCodexToml(stdPlatformIO{}, dst, []byte("diverged")); err != nil {
		t.Fatalf("review-note failure must be non-fatal, got %v", err)
	}
	alt := filepath.Join(tmp, "x.codex-preexisting.toml")
	if got, err := os.ReadFile(alt); err != nil || string(got) != "diverged" {
		t.Fatalf("diverged bytes must survive at %s, got=%q err=%v", alt, got, err)
	}
}

// --- nextCodexPreexistingAltPath sequencing --------------------------------

// TestCxCovNextCodexPreexistingAltPath_SequencesWhenFirstTaken covers the
// numbered-suffix loop when the base .codex-preexisting.toml already exists.
func TestCxCovNextCodexPreexistingAltPath_SequencesWhenFirstTaken(t *testing.T) {
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "x.toml")
	cxCovWriteFile(t, filepath.Join(tmp, "x.codex-preexisting.toml"), []byte("taken"))
	got := nextCodexPreexistingAltPath(dst)
	want := filepath.Join(tmp, "x.codex-preexisting-2.toml")
	if got != want {
		t.Fatalf("nextCodexPreexistingAltPath = %q, want %q", got, want)
	}
}

// --- writeCodexImportConflictReviewNote MkdirAll fault ----------------------

// TestCxCovWriteCodexImportConflictReviewNote_MkdirAllErrorSurfaces covers the
// review-notes MkdirAll fault directly.
func TestCxCovWriteCodexImportConflictReviewNote_MkdirAllErrorSurfaces(t *testing.T) {
	tmp := t.TempDir()
	blocker := filepath.Join(tmp, "afile")
	cxCovWriteFile(t, blocker, []byte("x"))
	t.Setenv("AGENTS_HOME", filepath.Join(blocker, "nested"))
	if err := writeCodexImportConflictReviewNote(filepath.Join(tmp, "x.toml"), filepath.Join(tmp, "x.alt.toml")); err == nil {
		t.Fatal("expected the review-notes MkdirAll fault to surface")
	}
}

// --- renderCodexAgentToml name fallback ------------------------------------

// TestCxCovRenderCodexAgentToml_NameFallbackFromDir covers the name-fallback
// branch: an AGENT.md without a name frontmatter derives the name from its
// parent directory.
func TestCxCovRenderCodexAgentToml_NameFallbackFromDir(t *testing.T) {
	tmp := t.TempDir()
	agentDir := filepath.Join(tmp, "myagent")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	agentMD := filepath.Join(agentDir, "AGENT.md")
	cxCovWriteFile(t, agentMD, []byte("---\ndescription: no name here\n---\nbody\n"))
	got, err := renderCodexAgentToml(agentMD)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `name = "myagent"`) {
		t.Fatalf("expected name fallback to the dir name, got:\n%s", got)
	}
}

// --- audit printers empty / local-file branches ----------------------------

// TestCxCovCodexPrintSymlinkAudit_LocalFileBranch covers the
// present-but-not-a-link (rendered/managed file on disk) branch.
func TestCxCovCodexPrintSymlinkAudit_LocalFileBranch(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "hooks.json")
	cxCovWriteFile(t, path, []byte("{}"))
	var buf bytes.Buffer
	codexPrintSymlinkAudit(&buf, path, ".codex/hooks.json")
	if !strings.Contains(buf.String(), ".codex/hooks.json") {
		t.Fatalf("expected the local-file audit line, got:\n%s", buf.String())
	}
}

// TestCxCovCodexPrintSkillsAudit_EmptyWhenOnlyNonLinks covers the empty-tally
// branch when the dir holds only non-link entries.
func TestCxCovCodexPrintSkillsAudit_EmptyWhenOnlyNonLinks(t *testing.T) {
	tmp := t.TempDir()
	cxCovWriteFile(t, filepath.Join(tmp, "plain-file"), []byte("x"))
	var buf bytes.Buffer
	codexPrintSkillsAudit(&buf, tmp)
	if !strings.Contains(buf.String(), "(empty)") {
		t.Fatalf("expected the (empty) skills line, got:\n%s", buf.String())
	}
}

// TestCxCovCodexPrintAgentsAudit_EmptyDir covers the empty-tally branch when
// the agents dir has no entries.
func TestCxCovCodexPrintAgentsAudit_EmptyDir(t *testing.T) {
	tmp := t.TempDir()
	empty := filepath.Join(tmp, "agents")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	codexPrintAgentsAudit(&buf, empty)
	if !strings.Contains(buf.String(), "(empty)") {
		t.Fatalf("expected the (empty) agents line, got:\n%s", buf.String())
	}
}

// --- SharedTargetIntents error propagation ---------------------------------

// TestCxCovSharedTargetIntents_CodexTomlErrorSurfaces covers the
// BuildSharedCodexAgentTomlIntents fault branch: the skills mirror scan
// succeeds but the agents bucket scope dir is unreadable, so the codex-toml
// intent build aborts and SharedTargetIntents propagates the error.
func TestCxCovSharedTargetIntents_CodexTomlErrorSurfaces(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	// A readable skills scope so BuildSharedSkillMirrorIntents succeeds.
	skillDir := filepath.Join(agentsHome, "skills", "proj", "alpha")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cxCovWriteFile(t, filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: alpha\n---\n"))
	// An unreadable agents scope dir so listCanonicalAgentEntries errors.
	agentsProj := filepath.Join(agentsHome, "agents", "proj")
	if err := os.MkdirAll(agentsProj, 0o755); err != nil {
		t.Fatal(err)
	}
	testutil.MakeDirUnreadable(t, agentsProj)
	t.Setenv("AGENTS_HOME", agentsHome)

	if _, err := NewCodex().(*codex).SharedTargetIntents("proj"); err == nil {
		t.Fatal("expected SharedTargetIntents to surface the codex-toml build error")
	}
}

// TestCxCovSharedTargetIntents_SkillMirrorErrorSurfaces covers the
// BuildSharedSkillMirrorIntents fault branch (the first error check in
// SharedTargetIntents): an unreadable skills scope dir aborts before the
// codex-toml build runs.
func TestCxCovSharedTargetIntents_SkillMirrorErrorSurfaces(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	skillsProj := filepath.Join(agentsHome, "skills", "proj")
	if err := os.MkdirAll(skillsProj, 0o755); err != nil {
		t.Fatal(err)
	}
	testutil.MakeDirUnreadable(t, skillsProj)
	t.Setenv("AGENTS_HOME", agentsHome)

	if _, err := NewCodex().(*codex).SharedTargetIntents("proj"); err == nil {
		t.Fatal("expected SharedTargetIntents to surface the skill-mirror build error")
	}
}

// --- SourcedAgentFileIntents non-agents skip -------------------------------

// TestCxCovSourcedAgentFileIntents_SkipsNonAgentsFamily covers the
// Family != "agents" continue branch while still emitting an intent for a
// genuine agents unit.
func TestCxCovSourcedAgentFileIntents_SkipsNonAgentsFamily(t *testing.T) {
	c := &codex{io: stdPlatformIO{}}
	units := []ResolvedUnit{
		{Family: "skills", Name: "skip-me", SourceID: "src", Digest: "sha256:abc"},
		{Family: "agents", Name: "keep-me", SourceID: "src", Digest: "sha256:def"},
	}
	intents := c.SourcedAgentFileIntents("proj", units)
	if len(intents) != 1 {
		t.Fatalf("expected exactly one agents intent, got %d", len(intents))
	}
	if intents[0].LogicalName != "keep-me" {
		t.Fatalf("expected the agents unit intent, got %q", intents[0].LogicalName)
	}
}
