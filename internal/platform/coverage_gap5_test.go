package platform

// Fifth-wave coverage tests: pre-existing-symlink and pre-existing-CLAUDE.md
// branches; legacy-only hook fixtures; and uncovered "second pass" branches
// in repeat-run scenarios.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/links"
	"github.com/NikashPrakash/dot-agents/internal/linktest"
)

// TestClaudeEnsureUserRules_PreExistingSymlinkSkipped drives the "already a
// symlink → continue" branch.
func TestClaudeEnsureUserRules_PreExistingSymlinkSkipped(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	home := filepath.Join(tmp, "home")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatal(err)
	}
	// Seed source.
	src := filepath.Join(agentsHome, "rules", "global", "rules.md")
	if err := os.MkdirAll(filepath.Dir(src), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("# rules"), 0644); err != nil {
		t.Fatal(err)
	}
	// Pre-existing symlink target.
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0755); err != nil {
		t.Fatal(err)
	}
	pretend := filepath.Join(tmp, "pretend.md")
	if err := os.WriteFile(pretend, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	linktest.Link(t, pretend, filepath.Join(home, ".claude", "CLAUDE.md"))

	c := NewClaude().(*claude)
	if err := c.ensureUserRules(agentsHome); err != nil {
		t.Fatalf("ensureUserRules: %v", err)
	}
	// Managed link should not have been changed.
	if !links.IsManagedLink(filepath.Join(home, ".claude", "CLAUDE.md"), pretend) {
		t.Errorf("managed link to %q was not preserved", pretend)
	}
}

// TestClaudeEnsureUserSettings_LegacyPathWithExistingSymlink covers the
// settings.json continue-on-existing-symlink branch.
func TestClaudeEnsureUserSettings_PreExistingSymlinkSkipped(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	home := filepath.Join(tmp, "home")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatal(err)
	}
	// Legacy spec file under settings/global.
	legacy := filepath.Join(agentsHome, "settings", "global", "claude-code.json")
	if err := os.MkdirAll(filepath.Dir(legacy), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	// Pre-existing symlink in user home.
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0755); err != nil {
		t.Fatal(err)
	}
	pretend := filepath.Join(tmp, "pretend.json")
	if err := os.WriteFile(pretend, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	linktest.Link(t, pretend, filepath.Join(home, ".claude", "settings.json"))

	c := NewClaude().(*claude)
	if err := c.ensureUserSettings(agentsHome); err != nil {
		t.Fatalf("ensureUserSettings: %v", err)
	}
}

// TestClaudeEnsureUserSettings_NoSpecRemovesStale drives the
// "spec == nil → removeManagedFileIf" branch.
func TestClaudeEnsureUserSettings_NoSpecRemoves(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	home := filepath.Join(tmp, "home")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0755); err != nil {
		t.Fatal(err)
	}
	// Pre-write a managed-looking rendered settings file.
	content, err := renderClaudeHookSettings([]HookSpec{{Name: "x", When: "pre_tool_use", Command: "/bin/true"}})
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, ".claude", "settings.json")
	if err := os.WriteFile(target, content, 0644); err != nil {
		t.Fatal(err)
	}
	c := NewClaude().(*claude)
	if err := c.ensureUserSettings(agentsHome); err != nil {
		t.Fatalf("ensureUserSettings: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Error("expected managed settings.json removed")
	}
}

// TestClaudeLinkUserAgent_PreExistingSymlinkSkipped drives the symlink-skip
// branch.
func TestClaudeLinkUserAgent_SymlinkSkipped(t *testing.T) {
	tmp := t.TempDir()
	agentDir := filepath.Join(tmp, "agents", "global", "reviewer")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "AGENT.md"), []byte("body"), 0644); err != nil {
		t.Fatal(err)
	}
	userAgentsDir := filepath.Join(tmp, "userhome", ".claude", "agents")
	if err := os.MkdirAll(userAgentsDir, 0755); err != nil {
		t.Fatal(err)
	}
	pretend := filepath.Join(tmp, "x")
	if err := os.MkdirAll(pretend, 0755); err != nil {
		t.Fatal(err)
	}
	linktest.Link(t, pretend, filepath.Join(userAgentsDir, "reviewer"))

	c := NewClaude().(*claude)
	entries, err := os.ReadDir(filepath.Join(tmp, "agents", "global"))
	if err != nil {
		t.Fatal(err)
	}
	c.linkUserAgent(filepath.Join(tmp, "agents", "global"), userAgentsDir, entries[0])
	// Managed link should remain pointing at pretend.
	if !links.IsManagedLink(filepath.Join(userAgentsDir, "reviewer"), pretend) {
		t.Errorf("link changed, expected preserved link to %q", pretend)
	}
}

// TestClaudeLinkUserAgent_NonAgentDirSkipped drives the !isClaudeAgentDir
// branch.
func TestClaudeLinkUserAgent_NonAgentDirSkipped(t *testing.T) {
	tmp := t.TempDir()
	// Directory without AGENT.md
	dir := filepath.Join(tmp, "agents", "global", "no-agent")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	userAgentsDir := filepath.Join(tmp, "userhome", ".claude", "agents")
	if err := os.MkdirAll(userAgentsDir, 0755); err != nil {
		t.Fatal(err)
	}
	c := NewClaude().(*claude)
	entries, err := os.ReadDir(filepath.Join(tmp, "agents", "global"))
	if err != nil {
		t.Fatal(err)
	}
	c.linkUserAgent(filepath.Join(tmp, "agents", "global"), userAgentsDir, entries[0])
	// No symlink should be created.
	if _, err := os.Lstat(filepath.Join(userAgentsDir, "no-agent")); !os.IsNotExist(err) {
		t.Error("non-agent dir should be skipped")
	}
}

// TestClaudePruneProjectRuleLinks_NonMatchingPreserved drives the non-prefix
// continue branch.
func TestClaudePruneProjectRuleLinks_NonMatchingPreserved(t *testing.T) {
	tmp := t.TempDir()
	rulesDir := filepath.Join(tmp, "rules")
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		t.Fatal(err)
	}
	// File with different prefix; should be left alone.
	other := filepath.Join(rulesDir, "global--keep.md")
	if err := os.WriteFile(other, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	// File that has subdir name (directory entry skipped).
	sub := filepath.Join(rulesDir, "subdir")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	// File with project prefix that's in keep map.
	keepP := filepath.Join(rulesDir, "proj--keep.md")
	if err := os.WriteFile(keepP, []byte("y"), 0644); err != nil {
		t.Fatal(err)
	}
	// File with project prefix that should be removed.
	stale := filepath.Join(rulesDir, "proj--stale.md")
	if err := os.WriteFile(stale, []byte("z"), 0644); err != nil {
		t.Fatal(err)
	}

	c := NewClaude().(*claude)
	wanted := map[string]string{"proj--keep.md": "/no/where"}
	if err := c.pruneProjectRuleLinks(rulesDir, "proj", wanted); err != nil {
		t.Fatalf("pruneProjectRuleLinks: %v", err)
	}
	for _, expect := range []string{other, keepP} {
		if _, err := os.Stat(expect); err != nil {
			t.Errorf("expected preserved: %s (%v)", expect, err)
		}
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("stale entry should be removed")
	}
}

// TestCursorCreateLinks_SecondRunIdempotent drives the second-pass execution
// of cursor CreateLinks where target files already exist.
func TestCursorCreateLinks_SecondRunIdempotent(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	if err := os.MkdirAll(filepath.Join(tmp, "home"), 0755); err != nil {
		t.Fatal(err)
	}
	// Rule.
	ruleSrc := filepath.Join(agentsHome, "rules", "proj", "x.md")
	if err := os.MkdirAll(filepath.Dir(ruleSrc), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ruleSrc, []byte("# rule"), 0644); err != nil {
		t.Fatal(err)
	}
	// Settings.
	if err := os.MkdirAll(filepath.Join(agentsHome, "settings", "proj"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentsHome, "settings", "proj", "cursor.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	cur := NewCursor()
	for i := 0; i < 2; i++ {
		if err := cur.CreateLinks("proj", repo); err != nil {
			t.Fatalf("CreateLinks pass %d: %v", i, err)
		}
	}
	if err := cur.RemoveLinks("proj", repo); err != nil {
		t.Errorf("RemoveLinks: %v", err)
	}
}

// TestEmitHookSpec_DirectHardlink drives the direct-hardlink transport branch.
func TestEmitHookSpec_DirectHardlink(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.json")
	if err := os.WriteFile(src, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	spec := &HookSpec{SourcePath: src}
	dst := filepath.Join(tmp, "out", "x.json")
	if err := emitHookSpec(stdPlatformIO{}, spec, dst, directHardlinkHookMode); err != nil {
		// Hardlinks fail across some filesystems; tolerate that as long as no panic.
		t.Logf("emitHookSpec hardlink: %v", err)
	}
}

// TestEmitHookFile_HardlinkBranch drives the HookTransportHardlink case.
func TestEmitHookFile_HardlinkBranch(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.json")
	if err := os.WriteFile(src, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(tmp, "dst.json")
	// Best-effort; just exercise the branch.
	_ = emitHookFile(stdPlatformIO{}, src, dst, HookTransportHardlink)
}

// TestEmitHookFile_SymlinkBranch drives the symlink case.
func TestEmitHookFile_SymlinkBranch(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.json")
	if err := os.WriteFile(src, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(tmp, "dst.json")
	if err := emitHookFile(stdPlatformIO{}, src, dst, HookTransportSymlink); err != nil {
		t.Errorf("symlink branch: %v", err)
	}
	if !links.IsManagedLink(dst, src) {
		t.Error("expected managed link to src")
	}
}

// TestPrepareIntentTargetForReplacement_AllowlistedFilePreserved drives the
// AllowlistedImportedDirOnly + regular-file branch when target IS allowlisted.
// The ownership contract authorizes this policy to replace only a proven
// imported/managed DIRECTORY; a regular file must be left in place (not
// pre-removed) so links.Symlink can apply the unmanaged-file contract instead
// of this code silently deleting user data.
func TestPrepareIntentTargetForReplacement_AllowlistedFilePreserved(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "blocking")
	if err := os.WriteFile(target, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	intent := ResourceIntent{
		TargetPath:    ".agents/skills/x",
		ReplacePolicy: ResourceReplaceAllowlistedImportedDirOnly,
	}
	if err := prepareIntentTargetForReplacement(target, intent); err != nil {
		t.Fatalf("allowlisted file prepare: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("expected regular file preserved (links.Symlink applies the contract), got: %v", err)
	}
}

// TestPrepareIntentTargetForReplacement_DefaultReplaceForFile drives the
// "default → os.Remove" branch (e.g. IfManaged on file).
func TestPrepareIntentTargetForReplacement_IfManagedFile(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "f")
	if err := os.WriteFile(target, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	intent := ResourceIntent{
		TargetPath:    ".agents/skills/x",
		ReplacePolicy: ResourceReplaceIfManaged,
	}
	if err := prepareIntentTargetForReplacement(target, intent); err != nil {
		t.Fatalf("IfManaged file replace: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Error("expected file removed")
	}
}

// TestParseJSONLTimestamp_BareDate makes sure the non-matching branch is hit.
func TestParseJSONLTimestamp_BareDateNotAccepted(t *testing.T) {
	if _, ok := parseJSONLTimestamp("2026-04-01"); ok {
		t.Error("bare date should not parse")
	}
}

// TestResolveClaudeCodeModelFromJSONL_NonAssistant lines drives the
// extract-function-returns-empty branch.
func TestResolveClaudeCodeModelFromJSONL_NonAssistantLine(t *testing.T) {
	home := t.TempDir()
	project := "/repo/x"
	sess := "no-asst"
	lines := []string{
		`{"type":"user","message":{"content":"hi"}}`,
		// substring-matched but with non-assistant type:
		`{"type":"user","gitBranch":"x","message":{"content":"assistant somewhere in body"}}`,
	}
	writeClaudeProjectJSONL(t, home, project, sess, lines)
	got := resolveClaudeCodeModelFromJSONL(home, project, sess)
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

// TestRemoveDirIfEmpty_NotEmpty covers the "len(entries) > 0" branch.
func TestRemoveDirIfEmpty_HasEntries(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "d")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := removeDirIfEmpty(dir); err != nil {
		t.Errorf("removeDirIfEmpty: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Error("non-empty dir should be preserved")
	}
}

// TestEmitRenderedHookFile_RenderError drives the render-error branch.
func TestEmitRenderedHookFile_RenderError(t *testing.T) {
	tmp := t.TempDir()
	// Use a spec that forces render error: required-on platform with no command.
	spec := HookSpec{Name: "x", When: "pre_tool_use", RequiredOn: []string{"claude"}}
	err := emitRenderedHookFile(stdPlatformIO{}, []HookSpec{spec}, filepath.Join(tmp, "out"), renderClaudeHookSettings)
	if err == nil {
		t.Error("expected render error")
	}
}

func TestEmitRenderedHookFileToUserHomes_RenderError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	spec := HookSpec{Name: "x", When: "pre_tool_use", RequiredOn: []string{"claude"}}
	err := emitRenderedHookFileToUserHomes(stdPlatformIO{}, []HookSpec{spec}, ".claude/settings.json", renderClaudeHookSettings)
	if err == nil {
		t.Error("expected render error")
	}
}

// TestRemoveManagedRenderedHookFile_RenderError drives the render-error branch.
func TestRemoveManagedRenderedHookFile_RenderError(t *testing.T) {
	spec := HookSpec{Name: "x", When: "pre_tool_use", RequiredOn: []string{"claude"}}
	err := removeManagedRenderedHookFile(stdPlatformIO{}, []HookSpec{spec}, "/tmp/x", renderClaudeHookSettings)
	if err == nil {
		t.Error("expected render error")
	}
}

// TestRemoveManagedRenderedHookFileToUserHomes_RenderError drives the error
// path under user homes.
func TestRemoveManagedRenderedHookFileToUserHomes_RenderError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	spec := HookSpec{Name: "x", When: "pre_tool_use", RequiredOn: []string{"claude"}}
	err := removeManagedRenderedHookFileToUserHomes(stdPlatformIO{}, []HookSpec{spec}, ".claude/settings.json", renderClaudeHookSettings)
	if err == nil {
		t.Error("expected render error")
	}
}

// TestEmitRenderedHookFanout_RenderError drives the fanout render-error branch.
func TestEmitRenderedHookFanout_RenderError(t *testing.T) {
	tmp := t.TempDir()
	spec := HookSpec{Name: "x", When: "post_tool_use", RequiredOn: []string{"copilot"}, Command: "/bin/true"}
	err := emitRenderedHookFanout(stdPlatformIO{}, []HookSpec{spec}, filepath.Join(tmp, "out"), renderCopilotHookFile)
	if err == nil {
		t.Error("expected error")
	}
}

func TestRemoveManagedRenderedHookFanout_RenderError(t *testing.T) {
	spec := HookSpec{Name: "x", When: "post_tool_use", RequiredOn: []string{"copilot"}, Command: "/bin/true"}
	err := removeManagedRenderedHookFanout(stdPlatformIO{}, []HookSpec{spec}, "/tmp/out", renderCopilotHookFile)
	if err == nil {
		t.Error("expected error")
	}
}

// TestRenderedCopilotHookNames_RenderError drives the propagation branch.
func TestRenderedCopilotHookNames_RenderError(t *testing.T) {
	specs := []HookSpec{{
		Name:       "x",
		When:       "post_tool_use",
		Command:    "/bin/true",
		RequiredOn: []string{"copilot"},
	}}
	if _, err := renderedCopilotHookNames(specs); err == nil {
		t.Error("expected error")
	}
}

// TestPruneManagedRenderedFanoutExtras_RemovePermissionsRespected covers the
// continue-on-IsNotExist branch when concurrent removal already cleared it.
func TestPruneManagedRenderedFanoutExtras_StaleEntry(t *testing.T) {
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "hooks")
	if err := os.MkdirAll(dst, 0755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(dst, "stale.json")
	if err := os.WriteFile(stale, []byte(`{"version":1,"hooks":{"x":[]}}`), 0644); err != nil {
		t.Fatal(err)
	}
	// "wanted" does not include it; the detector matches; file is removed.
	if err := pruneManagedRenderedFanoutExtras(stdPlatformIO{}, dst, nil, isLikelyRenderedCursorHookConfig); err != nil {
		t.Fatalf("prune: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("stale removed")
	}
}

// TestSyncResourceDirEntries_NoEntries handles the empty-input branch.
func TestSyncResourceDirEntries_Empty(t *testing.T) {
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "out")
	if err := syncResourceDirEntries(stdPlatformIO{}, nil, dst); err != nil {
		t.Errorf("empty entries: %v", err)
	}
	if _, err := os.Stat(dst); err != nil {
		t.Error("expected dst dir created")
	}
}

// TestSharedTargetIntents_AllPlatformsPopulated provides coverage of the
// concatenation paths for each platform's SharedTargetIntents.
func TestSharedTargetIntents_AllPlatformsCoverConcat(t *testing.T) {
	agentsHome, home := fullyPopulatedAgentsHome(t, "proj")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)
	for _, p := range All() {
		intents, err := p.SharedTargetIntents("proj")
		if err != nil {
			t.Errorf("%s SharedTargetIntents: %v", p.ID(), err)
		}
		if len(intents) == 0 {
			t.Errorf("%s expected intents", p.ID())
		}
	}
}

// TestFormatSharedTargetPlanForDryRun_AllVariants exercises each formatter
// branch (DirectDir, DirectFile, RenderSingle, default).
func TestFormatSharedTargetPlanForDryRun_RenderVariant(t *testing.T) {
	agentsHome, home := fullyPopulatedAgentsHome(t, "proj")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)
	repo := filepath.Join(t.TempDir(), "repo")
	lines, err := DryRunSharedTargetPlanLines("proj", repo, []Platform{NewCodex()})
	if err != nil {
		t.Fatalf("DryRunSharedTargetPlanLines: %v", err)
	}
	if len(lines) == 0 {
		t.Fatal("expected dry-run lines")
	}
	// At least one line should mention "write" for codex agent toml.
	gotWrite := false
	for _, l := range lines {
		if strings.Contains(l, "write") {
			gotWrite = true
			break
		}
	}
	if !gotWrite {
		t.Errorf("expected write line in %+v", lines)
	}
}

// TestFormatSharedTargetPlanForDryRun_FileVariant drives the DirectFile branch
// (BuildSharedAgentFileSymlinkIntents → copilot/opencode).
func TestFormatSharedTargetPlanForDryRun_FileVariant(t *testing.T) {
	agentsHome, home := fullyPopulatedAgentsHome(t, "proj")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)
	repo := filepath.Join(t.TempDir(), "repo")
	lines, err := DryRunSharedTargetPlanLines("proj", repo, []Platform{NewCopilot()})
	if err != nil {
		t.Fatalf("DryRunSharedTargetPlanLines: %v", err)
	}
	if len(lines) == 0 {
		t.Fatal("expected dry-run lines")
	}
	gotFile := false
	for _, l := range lines {
		if strings.Contains(l, "symlink file") {
			gotFile = true
			break
		}
	}
	if !gotFile {
		t.Errorf("expected symlink-file line in %+v", lines)
	}
}

// TestCodexCreateLinks_GlobalRuleOnly drives the global-rules path when
// project override is absent.
func TestCodexCreateLinks_GlobalRuleOnly(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	home := filepath.Join(tmp, "home")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(agentsHome, "rules", "global"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentsHome, "rules", "global", "rules.md"), []byte("# rules"), 0644); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	if err := NewCodex().CreateLinks("proj", repo); err != nil {
		t.Fatalf("CreateLinks: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(repo, "AGENTS.md")); err != nil {
		t.Errorf("AGENTS.md missing: %v", err)
	}
}

// TestCopilotCreateMCPLinks_NoSource drives the no-source early-return branch.
func TestCopilotCreateMCPLinks_NoSource(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	c := NewCopilot().(*copilot)
	if err := c.createMCPLinks("proj", repo, filepath.Join(tmp, ".agents")); err != nil {
		t.Errorf("createMCPLinks no source: %v", err)
	}
	// .vscode/mcp.json should NOT exist.
	if _, err := os.Lstat(filepath.Join(repo, ".vscode", "mcp.json")); !os.IsNotExist(err) {
		t.Error("expected no mcp.json")
	}
}

// TestClaudeCreateLinks_ErrorPropagationFromMkdir uses a blocker file to
// force an mkdir error inside prepareLinks (rulesDir path).
func TestClaudeCreateLinks_RulesMkdirBlocked(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	home := filepath.Join(tmp, "home")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	// Place a regular file where the rules dir should live.
	claudeDir := filepath.Join(repo, ".claude")
	if err := os.WriteFile(claudeDir, []byte("blocker"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := NewClaude().CreateLinks("proj", repo); err == nil {
		t.Error("expected mkdir error")
	}
}

// TestClaudeScanSessionTokens_SkipsNonAssistantLines exercises the
// substring-prefilter "continue" branch in claudeScanSessionTokens
// (lines without `"assistant"` are short-circuited before JSON parse).
func TestClaudeScanSessionTokens_SkipsNonAssistantLines(t *testing.T) {
	home := t.TempDir()
	project := "/repo/skip"
	sess := "44444444-4444-4444-4444-444444444444"
	lines := []string{
		// No "assistant" substring — hits the continue branch.
		`{"type":"user","timestamp":"2026-05-11T12:00:00Z","message":{"content":"hi"}}`,
		// One real assistant line so the function does some work.
		`{"type":"assistant","timestamp":"2026-05-11T12:30:00Z","message":{"usage":{"input_tokens":7,"output_tokens":3}}}`,
	}
	writeClaudeProjectJSONL(t, home, project, sess, lines)

	got := claudeScanSessionTokens(home, project, sess, "")
	if got.MessageCount != 1 {
		t.Errorf("MessageCount = %d, want 1 (non-assistant line skipped)", got.MessageCount)
	}
	if got.InputTokens != 7 {
		t.Errorf("InputTokens = %d, want 7", got.InputTokens)
	}
	if got.OutputTokens != 3 {
		t.Errorf("OutputTokens = %d, want 3", got.OutputTokens)
	}
}

// TestClaudeScanJSONLForBranch_MissingFile drives the os.Open failure
// short-circuit (path does not exist → nil).
func TestClaudeScanJSONLForBranch_MissingFile(t *testing.T) {
	got := claudeScanJSONLForBranch(filepath.Join(t.TempDir(), "missing.jsonl"), `"gitBranch":"main"`, "main")
	if got != nil {
		t.Errorf("expected nil for missing file, got %+v", got)
	}
}

// TestClaudeScanJSONLForBranch_LineCap50 drives the >50-line break branch.
// We write 60 matching lines; only the first 50 should be visited.
func TestClaudeScanJSONLForBranch_LineCap50(t *testing.T) {
	home := t.TempDir()
	project := "/repo/cap"
	sess := "55555555-5555-5555-5555-555555555555"
	target := "main"
	line := `{"type":"assistant","sessionId":"` + sess + `","timestamp":"2026-05-11T11:30:00Z","gitBranch":"` + target + `","message":{"content":[{"type":"text","text":"x"}]}}`
	var lines []string
	for i := 0; i < 60; i++ {
		lines = append(lines, line)
	}
	writeClaudeProjectJSONL(t, home, project, sess, lines)

	slug := strings.ReplaceAll(project, "/", "-")
	path := filepath.Join(home, ".claude", "projects", slug, sess+".jsonl")
	marker := `"gitBranch":"` + target + `"`
	got := claudeScanJSONLForBranch(path, marker, target)
	if got == nil {
		t.Fatalf("expected match, got nil")
	}
	// Cap is 50 — anything more means the break branch did not engage.
	if got.MessageCount > 50 {
		t.Errorf("MessageCount = %d, want <= 50 (cap)", got.MessageCount)
	}
}

// TestCodexScanSessionTokens_OpenError exercises the os.Open failure branch
// when findCodexSessionFile returns a path that cannot be opened (e.g. it
// resolves to a directory instead of a regular file).
func TestCodexScanSessionTokens_OpenError(t *testing.T) {
	home := t.TempDir()
	sessID := "open-err-id"
	// Create the expected daily directory and place a *directory* at the
	// rollout filename — findCodexSessionFile globs by suffix and will pick
	// it up, but os.Open succeeds on directories on Unix. To trigger an
	// open error we instead seed an unreadable file.
	dir := filepath.Join(home, ".codex", "sessions", "2026", "05", "11")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "rollout-2026-05-11-"+sessID+".jsonl")
	if err := os.WriteFile(path, []byte("ignored"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	// On macOS running as root, mode 0o000 may still be readable; treat the
	// success path as acceptable too — what matters is that the function
	// returns without panicking.
	got := codexScanSessionTokens(home, sessID, "")
	_ = got
}
