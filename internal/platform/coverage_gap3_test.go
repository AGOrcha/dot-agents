package platform

// Third-wave coverage tests focused on the smaller-but-numerous remaining
// gaps after coverage_gap{,2}_test.go: error branches in detectors, missing-
// file branches in remove helpers, and the leftover branches inside the
// per-platform CreateLinks helpers.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NikashPrakash/dot-agents/internal/linktest"
)

// TestIsLikelyRendered_BadJSON exercises the unmarshal-error branch of each
// detector.
func TestIsLikelyRendered_BadJSON(t *testing.T) {
	bad := []byte("not json at all")
	if isLikelyRenderedClaudeHookSettings(bad) {
		t.Error("claude detector should reject bad json")
	}
	if isLikelyRenderedCodexHookConfig(bad) {
		t.Error("codex detector should reject bad json")
	}
	if isLikelyRenderedCursorHookConfig(bad) {
		t.Error("cursor detector should reject bad json")
	}
	if isLikelyRenderedCopilotHookFile(bad) {
		t.Error("copilot detector should reject bad json")
	}

	// Empty-hooks JSON still parses but should not match.
	empty := []byte(`{"hooks":{}}`)
	if isLikelyRenderedClaudeHookSettings(empty) {
		t.Error("claude detector should reject empty hooks")
	}
	if isLikelyRenderedCodexHookConfig(empty) {
		t.Error("codex detector should reject empty hooks")
	}
	noVersion := []byte(`{"hooks":{"x":[]}}`)
	if isLikelyRenderedCursorHookConfig(noVersion) {
		t.Error("cursor detector should reject missing version")
	}
	if isLikelyRenderedCopilotHookFile(noVersion) {
		t.Error("copilot detector should reject missing version")
	}
}

// TestRemoveManagedFile_SymlinkSkipped verifies a symlink at the target is
// left in place.
func TestRemoveManagedFile_SymlinkSkipped(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.json")
	if err := os.WriteFile(src, []byte("managed"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(tmp, "link.json")
	linktest.Link(t, src, link)
	if err := removeManagedFile(stdPlatformIO{}, link, []byte("managed")); err != nil {
		t.Fatalf("removeManagedFile symlink: %v", err)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Error("symlink should be preserved")
	}
}

func TestRemoveManagedFile_MissingTarget(t *testing.T) {
	if err := removeManagedFile(stdPlatformIO{}, filepath.Join(t.TempDir(), "no-such"), []byte("x")); err != nil {
		t.Errorf("missing file should no-op, got %v", err)
	}
}

func TestRemoveManagedFileIf_SymlinkSkipped(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.json")
	if err := os.WriteFile(src, []byte(`{"version":1,"hooks":{"x":[]}}`), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(tmp, "link.json")
	linktest.Link(t, src, link)
	if err := removeManagedFileIf(stdPlatformIO{}, link, isLikelyRenderedCursorHookConfig); err != nil {
		t.Errorf("symlink: %v", err)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Error("symlink should be preserved")
	}
}

// TestWriteManagedFile_ExistingSymlinkReplaced drives the Lstat→Remove branch.
func TestWriteManagedFile_ExistingSymlinkReplaced(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	if err := os.WriteFile(src, []byte("dummy"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(tmp, "dst")
	linktest.Link(t, src, dst)
	if err := writeManagedFile(stdPlatformIO{}, dst, []byte("real")); err != nil {
		t.Fatalf("writeManagedFile: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "real" {
		t.Errorf("got %q, want real", got)
	}
}

// TestWriteCodexAgentTomlFile_ExistingFileReplaced drives the Lstat→Remove branch.
func TestWriteCodexAgentTomlFile_ExistingFileReplaced(t *testing.T) {
	tmp := t.TempDir()
	agent := filepath.Join(tmp, "AGENT.md")
	if err := os.WriteFile(agent, []byte("---\nname: x\n---\nbody\n"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(tmp, "x.toml")
	if err := os.WriteFile(dst, []byte("stale\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := writeCodexAgentTomlFile(stdPlatformIO{}, dst, agent); err != nil {
		t.Fatalf("writeCodexAgentTomlFile: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `name = "x"`) {
		t.Errorf("file content not refreshed: %q", got)
	}
}

func TestWriteCodexAgentTomlFile_BadAgentMD(t *testing.T) {
	if err := writeCodexAgentTomlFile(stdPlatformIO{}, filepath.Join(t.TempDir(), "x.toml"), "/no/such/agent.md"); err == nil {
		t.Error("expected error for missing agent.md")
	}
}

// TestRemoveDirIfEmpty_EmptyString covers the early-return branch.
func TestRemoveDirIfEmpty_EmptyString(t *testing.T) {
	if err := removeDirIfEmpty(""); err != nil {
		t.Errorf("empty string should no-op, got %v", err)
	}
}

// TestCollectRuleEntry_NonRuleFile drives the isCursorRuleFile guard.
func TestCollectRuleEntry_NonRuleFile(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "ignore.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(tmp, "subdir"), 0755); err != nil {
		t.Fatal(err)
	}
	desired := map[string]string{}
	c := NewCursor().(*cursor)
	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		c.collectRuleEntry(entry, tmp, "prefix--", desired)
	}
	if len(desired) != 0 {
		t.Errorf("expected 0 entries, got %d (%+v)", len(desired), desired)
	}
}

// TestHasDeprecatedFormatAndDetails covers the matching branch of each
// platform's deprecation detector (the not-matching branch is exercised by the
// contract test).
func TestHasDeprecatedFormat_Detected(t *testing.T) {
	tmp := t.TempDir()
	// Claude deprecated marker.
	repoC := filepath.Join(tmp, "claude-repo")
	if err := os.MkdirAll(repoC, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoC, ".claude.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	cp := NewClaude()
	if !cp.HasDeprecatedFormat(repoC) {
		t.Error("expected claude deprecated detection")
	}
	if cp.DeprecatedDetails(repoC) == "" {
		t.Error("expected non-empty deprecated details")
	}

	// Cursor deprecated marker.
	repoCur := filepath.Join(tmp, "cursor-repo")
	if err := os.MkdirAll(repoCur, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoCur, ".cursorrules"), []byte("rules"), 0644); err != nil {
		t.Fatal(err)
	}
	curp := NewCursor()
	if !curp.HasDeprecatedFormat(repoCur) {
		t.Error("expected cursor deprecated detection")
	}
	if curp.DeprecatedDetails(repoCur) == "" {
		t.Error("expected non-empty deprecated details")
	}
}

// TestEmitRenderedHookFanout_MissingMkdirRoot drives error propagation when
// MkdirAll on the dst root fails (parent is a regular file).
func TestEmitRenderedHookFanout_MkdirFails(t *testing.T) {
	tmp := t.TempDir()
	blocker := filepath.Join(tmp, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	specs := []HookSpec{{Name: "p", When: "user_prompt_submit", Command: "/bin/true"}}
	err := emitRenderedHookFanout(stdPlatformIO{}, specs, filepath.Join(blocker, "sub"), renderCopilotHookFile)
	if err == nil {
		t.Error("expected mkdir error")
	}
}

// TestEmitHookFanout_MkdirFails drives error propagation in emitHookFanout.
func TestEmitHookFanout_MkdirFails(t *testing.T) {
	tmp := t.TempDir()
	blocker := filepath.Join(tmp, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	specs := []HookSpec{{Name: "p", SourcePath: filepath.Join(tmp, "src.json")}}
	if err := os.WriteFile(specs[0].SourcePath, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	err := emitHookFanout(stdPlatformIO{}, specs, filepath.Join(blocker, "sub"),
		HookEmissionMode{Shape: HookShapeRenderFanout, Transport: HookTransportSymlink},
		func(s HookSpec) (string, bool) { return s.Name + ".json", true })
	if err == nil {
		t.Error("expected mkdir error")
	}
}

// TestLoadHookSpecEntry_NonJSONFileIgnored exercises the "not directory, not
// .json" branch.
func TestLoadHookSpecEntry_NonJSONFileIgnored(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "readme.md"), []byte("# hi"), 0644); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		spec, ok, lerr := loadHookSpecEntry(tmp, "global", e)
		if lerr != nil {
			t.Errorf("expected no error, got %v", lerr)
		}
		if ok {
			t.Errorf("expected non-hook to skip, got %+v", spec)
		}
	}
}

// TestLoadHookBundleSpec_BadYaml drives the yaml.Unmarshal error branch.
func TestLoadHookBundleSpec_BadYaml(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "broken")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "HOOK.yaml"), []byte(":\n  -bad"), 0644); err != nil {
		t.Fatal(err)
	}
	_, _, err := loadHookBundleSpec(tmp, "global", "broken")
	if err == nil {
		t.Error("expected yaml error")
	}
}

func TestLoadHookBundleSpec_MissingManifest(t *testing.T) {
	tmp := t.TempDir()
	_, ok, err := loadHookBundleSpec(tmp, "global", "no-such")
	if err != nil {
		t.Errorf("missing manifest: %v", err)
	}
	if ok {
		t.Error("expected ok=false for missing manifest")
	}
}

// TestCollectCanonicalHookSpecsForPlatform_MissingScope covers the IsNotExist
// branch.
func TestCollectCanonicalHookSpecsForPlatform_MissingScope(t *testing.T) {
	tmp := t.TempDir()
	got, err := collectCanonicalHookSpecsForPlatform(tmp, "proj", "claude", "global", "proj")
	if err != nil {
		t.Errorf("missing scope: %v", err)
	}
	if got == nil {
		t.Error("expected empty slice, not nil") // function returns []HookSpec{}
	}
}

// TestEnsureUnderRulesScopeTree_DotPath exercises filepath.Rel with same dir.
func TestEnsureUnderRulesScopeTree_RootItself(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "rules", "global")
	// Same path == root → rel is "." → not "..", so accepted.
	if err := EnsureUnderRulesScopeTree(tmp, "global", root); err != nil {
		t.Errorf("expected root itself to validate, got %v", err)
	}
}

// TestResolveCanonicalSettingsFile_NotFound exercises the error branch.
func TestResolveCanonicalSettingsFile_NotFound(t *testing.T) {
	if _, err := ResolveCanonicalSettingsFile(t.TempDir(), "proj", "missing"); err == nil {
		t.Error("expected error for missing file")
	}
}

// TestRemoveSharedTargetPlanEmpty drives the no-platform branch.
func TestRemoveSharedTargetPlan_NoPlatforms(t *testing.T) {
	if err := RemoveSharedTargetPlan("proj", t.TempDir(), nil); err != nil {
		t.Errorf("RemoveSharedTargetPlan with no platforms: %v", err)
	}
}

// TestRemoveSharedTargets_RenderedTomlPath drives the codex-agent-toml remove
// path.
func TestRemoveManagedIntentTarget_CodexTomlPath(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "out.toml")
	if err := os.WriteFile(target, []byte("dummy"), 0644); err != nil {
		t.Fatal(err)
	}
	intent := ResourceIntent{
		Shape:        ResourceShapeRenderSingle,
		Transport:    ResourceTransportWrite,
		Materializer: codexAgentTomlMaterializer,
		TargetPath:   "out.toml",
	}
	if err := removeManagedIntentTarget(intent, tmp, t.TempDir()); err != nil {
		t.Fatalf("removeManagedIntentTarget: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Error("expected target removed")
	}
}

// TestRemoveManagedIntentTarget_UnknownShapeNoop drives the default (no-op)
// branch for unknown shape/transport combos.
func TestRemoveManagedIntentTarget_UnknownShape(t *testing.T) {
	intent := ResourceIntent{Shape: "weird", Transport: "weird"}
	if err := removeManagedIntentTarget(intent, t.TempDir(), t.TempDir()); err != nil {
		t.Errorf("unknown shape should no-op, got %v", err)
	}
}

// TestExecuteRenderSingleWrite_CodexAgentToml drives the codex-agent-toml
// happy-path branch via Execute.
func TestExecuteRenderSingleWrite_CodexAgentToml(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	src := filepath.Join(agentsHome, "agents", "proj", "reviewer", "AGENT.md")
	if err := os.MkdirAll(filepath.Dir(src), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("---\nname: reviewer\n---\nbody\n"), 0644); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	intent := ResourceIntent{
		IntentID:    "codex.proj.reviewer.toml",
		Project:     "proj",
		Bucket:      "agents",
		LogicalName: "reviewer",
		TargetPath:  filepath.Join(".codex/agents", "reviewer.toml"),
		Ownership:   ResourceOwnershipSharedRepo,
		SourceRef: ResourceSourceRef{
			Scope:        "proj",
			Bucket:       "agents",
			RelativePath: "reviewer/AGENT.md",
			Kind:         ResourceSourceCanonicalFile,
		},
		Shape:         ResourceShapeRenderSingle,
		Transport:     ResourceTransportWrite,
		Materializer:  codexAgentTomlMaterializer,
		ReplacePolicy: ResourceReplaceAllowlistedImportedDirOnly,
		PrunePolicy:   ResourcePruneTarget,
	}
	plan, err := BuildResourcePlan([]ResourceIntent{intent})
	if err != nil {
		t.Fatalf("BuildResourcePlan: %v", err)
	}
	if err := plan.Execute(repo, agentsHome); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".codex/agents/reviewer.toml")); err != nil {
		t.Errorf("expected toml file: %v", err)
	}
}

// TestSyncScopedFileSymlinks_ExistingTargetMaintained drives the link.Symlink
// idempotency branch.
func TestSyncScopedFileSymlinks_Idempotent(t *testing.T) {
	tmp := t.TempDir()
	agentDir := filepath.Join(tmp, "agents", "global", "x")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "AGENT.md"), []byte("y"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(tmp, "dst")
	if err := syncScopedFileSymlinks(stdPlatformIO{}, tmp, "agents", "global", "AGENT.md", dst, ".md"); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if err := syncScopedFileSymlinks(stdPlatformIO{}, tmp, "agents", "global", "AGENT.md", dst, ".md"); err != nil {
		t.Fatalf("second sync: %v", err)
	}
}

// TestEmitPreferredHookFile_LegacyBranch drives the case where there are no
// canonical bundles and a legacy spec is present (Shape=Direct, Transport=symlink).
func TestEmitPreferredHookFile_LegacyBranch(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.json")
	if err := os.WriteFile(src, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(tmp, "out", "settings.json")
	spec := &HookSpec{Name: "legacy", SourcePath: src}
	if err := emitPreferredHookFile(stdPlatformIO{}, target, renderClaudeHookSettings, spec, directSymlinkHookMode, nil); err != nil {
		t.Fatalf("emitPreferredHookFile legacy: %v", err)
	}
	if _, err := os.Lstat(target); err != nil {
		t.Errorf("expected legacy symlink: %v", err)
	}
}

// TestEmitPreferredHookFileToUserHomes_LegacyBranch drives the legacy spec
// path under user homes.
func TestEmitPreferredHookFileToUserHomes_LegacyBranch(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.json")
	if err := os.WriteFile(src, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(tmp, "home")
	t.Setenv("HOME", home)
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatal(err)
	}
	spec := &HookSpec{Name: "x", SourcePath: src}
	if err := emitPreferredHookFileToUserHomes(stdPlatformIO{}, ".claude/settings.json",
		renderClaudeHookSettings, spec, directSymlinkHookMode, nil); err != nil {
		t.Fatalf("user-home legacy: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(home, ".claude", "settings.json")); err != nil {
		t.Errorf("expected user-home symlink: %v", err)
	}
}

// TestEmitPreferredHookFile_AllNilNoOp drives the case where no bundles, no
// legacy, no removeRendered → returns nil.
func TestEmitPreferredHookFile_AllNilNoOp(t *testing.T) {
	if err := emitPreferredHookFile(stdPlatformIO{}, filepath.Join(t.TempDir(), "x"),
		renderClaudeHookSettings, nil, directSymlinkHookMode, nil); err != nil {
		t.Errorf("all-nil no-op: %v", err)
	}
}

func TestEmitPreferredHookFileToUserHomes_AllNilNoOp(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := emitPreferredHookFileToUserHomes(stdPlatformIO{}, ".x/y",
		renderClaudeHookSettings, nil, directSymlinkHookMode, nil); err != nil {
		t.Errorf("user-home all-nil no-op: %v", err)
	}
}

// TestClaudeLinkProjectMCPMissing drives the missing-source branch (just to
// hit the early-return).
func TestClaudeLinkProjectMCP_MissingSource(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	c := NewClaude().(*claude)
	c.linkProjectMCP("proj", repo, agentsHome)
	// no symlink created
	if _, err := os.Lstat(filepath.Join(repo, ".mcp.json")); !os.IsNotExist(err) {
		t.Errorf("expected no .mcp.json link: %v", err)
	}
}

// TestEnsureFileSymlinkIntent_TargetIsRegularFileBlocked drives the
// !info.IsDir + ReplaceNever rejection.
func TestEnsureFileSymlinkIntent_RegularFileNever(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	src := filepath.Join(agentsHome, "skills", "proj", "x", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(src), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("body"), 0644); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".agents/skills"), 0755); err != nil {
		t.Fatal(err)
	}
	// Pre-place a regular file at the target.
	target := filepath.Join(repo, ".agents/skills/x")
	if err := os.WriteFile(target, []byte("blocking"), 0644); err != nil {
		t.Fatal(err)
	}

	intent := validSharedSkillIntent(".agents/skills/x", "test")
	intent.ReplacePolicy = ResourceReplaceNever
	plan, err := BuildResourcePlan([]ResourceIntent{intent})
	if err != nil {
		t.Fatalf("BuildResourcePlan: %v", err)
	}
	if err := plan.Execute(repo, agentsHome); err == nil {
		t.Error("expected error when target is regular file with Never policy")
	}
}

// TestEnsureFileSymlinkIntent_TargetDirIfManagedRefused covers the dir+IfManaged refusal branch.
func TestEnsureFileSymlinkIntent_DirIfManagedRefused(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	src := filepath.Join(agentsHome, "skills", "proj", "x", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(src), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("body"), 0644); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(tmp, "repo")
	target := filepath.Join(repo, ".agents/skills/x")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	intent := validSharedSkillIntent(".agents/skills/x", "test")
	intent.ReplacePolicy = ResourceReplaceIfManaged
	plan, err := BuildResourcePlan([]ResourceIntent{intent})
	if err != nil {
		t.Fatalf("BuildResourcePlan: %v", err)
	}
	if err := plan.Execute(repo, agentsHome); err == nil {
		t.Error("expected refusal for IfManaged on directory")
	}
}

// TestEnsureFileSymlinkIntent_DirNeverRefused covers the dir+Never branch.
func TestEnsureFileSymlinkIntent_DirNeverRefused(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	src := filepath.Join(agentsHome, "skills", "proj", "x", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(src), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("body"), 0644); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(tmp, "repo")
	target := filepath.Join(repo, ".agents/skills/x")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	intent := validSharedSkillIntent(".agents/skills/x", "test")
	intent.ReplacePolicy = ResourceReplaceNever
	plan, err := BuildResourcePlan([]ResourceIntent{intent})
	if err != nil {
		t.Fatalf("BuildResourcePlan: %v", err)
	}
	if err := plan.Execute(repo, agentsHome); err == nil {
		t.Error("expected refusal for Never on directory")
	}
}

// TestPrepareIntentTargetForReplacement_UnknownReplaceForDir drives the
// "unsupported replace policy" default case.
func TestPrepareIntentTargetForReplacement_UnknownReplacePolicyForDir(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "d")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	intent := ResourceIntent{
		TargetPath:    ".agents/skills/x",
		ReplacePolicy: "weird-policy",
	}
	if err := prepareIntentTargetForReplacement(target, intent); err == nil {
		t.Error("expected error for unknown replace policy")
	}
}

// TestResolveCodexModelFromJSONL_NoResponseLine drives the empty-result branch
// (model in JSONL is "").
func TestResolveCodexModelFromJSONL_NoModel(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".codex", "sessions", "2026", "05", "11")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	sessID := "no-model"
	path := filepath.Join(dir, "rollout-"+sessID+".jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"event_msg","payload":{"type":"task_started"}}`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := resolveCodexModelFromJSONL(home, sessID); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

// TestClaudeAccumulateAssistantEntry_BadJSON drives the unmarshal-error branch.
func TestClaudeAccumulateAssistantEntry_BadJSON(t *testing.T) {
	var m SessionTokenMetrics
	claudeAccumulateAssistantEntry([]byte("garbage"), time.Time{}, &m)
	if m.InputTokens != 0 {
		t.Errorf("expected zero, got %+v", m)
	}
}

func TestClaudeAccumulateAssistantEntry_AfterCutoffSkipped(t *testing.T) {
	line := `{"type":"assistant","timestamp":"2026-05-11T10:00:00Z","message":{"usage":{"input_tokens":99}}}`
	var m SessionTokenMetrics
	claudeAccumulateAssistantEntry([]byte(line), time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC), &m)
	if m.InputTokens != 0 {
		t.Errorf("expected zero (before cutoff), got %+v", m)
	}
}

// TestPruneCodexRepoAgentTomls_NoEntries covers the early no-entries branch.
func TestPruneCodexRepoAgentTomls_NoEntries(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	// agentsHome with no agents bucket → listScopedResourceDirs errors → nil.
	if err := pruneCodexRepoAgentTomls("proj", repo, filepath.Join(tmp, "missing")); err != nil {
		t.Errorf("expected no error for missing agents bucket, got %v", err)
	}
}

// TestRulePrune_MissingDir covers the err==nil return-nil branch.
func TestCursorPruneRuleLinks_MissingDir(t *testing.T) {
	c := NewCursor().(*cursor)
	if err := c.pruneRuleLinks(filepath.Join(t.TempDir(), "no-such"), "proj", nil); err != nil {
		t.Errorf("missing dir should no-op, got %v", err)
	}
}

// TestCursorRemoveAgentLinks_MissingDir covers the err==nil return branch.
func TestCursorRemoveAgentLinks_MissingDir(t *testing.T) {
	c := NewCursor().(*cursor)
	c.removeAgentLinks(filepath.Join(t.TempDir(), "no-such"), filepath.Join(t.TempDir(), ".agents"))
	// no panic = pass
}
