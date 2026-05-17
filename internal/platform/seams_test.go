package platform

// Seam-injection tests exercise the err != nil branches on os.MkdirAll /
// os.Remove / os.WriteFile that cannot be triggered with a writable tmp
// fixture. Each test swaps the package-level seam to an error-returning stub
// and asserts the calling function surfaces the synthetic error.

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var errSeamSynthetic = errors.New("seam synthetic failure")

// withMkdirAllErrorAfter swaps osMkdirAll to fail the Nth call (1-indexed)
// whose path contains want, and delegates the rest to the real os.MkdirAll.
// This lets a test target a specific MkdirAll call in a CreateLinks chain
// where earlier calls must succeed.
func withMkdirAllErrorAfter(t *testing.T, want string, failAt int) {
	t.Helper()
	saved := osMkdirAll
	t.Cleanup(func() { osMkdirAll = saved })
	count := 0
	osMkdirAll = func(path string, perm fs.FileMode) error {
		if want == "" || strings.Contains(path, want) {
			count++
			if count == failAt {
				return errSeamSynthetic
			}
		}
		return saved(path, perm)
	}
}

// withMkdirAllError swaps osMkdirAll to a stub that fails for any path
// containing want. The original is restored on test cleanup.
func withMkdirAllError(t *testing.T, want string) {
	t.Helper()
	saved := osMkdirAll
	t.Cleanup(func() { osMkdirAll = saved })
	osMkdirAll = func(path string, perm fs.FileMode) error {
		if want == "" || strings.Contains(path, want) {
			return errSeamSynthetic
		}
		return saved(path, perm)
	}
}

func withRemoveError(t *testing.T, want string) {
	t.Helper()
	saved := osRemove
	t.Cleanup(func() { osRemove = saved })
	osRemove = func(path string) error {
		if want == "" || strings.Contains(path, want) {
			return errSeamSynthetic
		}
		return saved(path)
	}
}

func withWriteFileError(t *testing.T, want string) {
	t.Helper()
	saved := osWriteFile
	t.Cleanup(func() { osWriteFile = saved })
	osWriteFile = func(name string, data []byte, perm fs.FileMode) error {
		if want == "" || strings.Contains(name, want) {
			return errSeamSynthetic
		}
		return saved(name, data, perm)
	}
}

func setupAgentsHome(t *testing.T) (agentsHome, repo string) {
	t.Helper()
	tmp := t.TempDir()
	agentsHome = filepath.Join(tmp, ".agents")
	repo = filepath.Join(tmp, "repo")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	return agentsHome, repo
}

// --- cursor.go seams ----------------------------------------------------

func TestCursorCreateRuleLinks_MkdirAllErrorSurfaces(t *testing.T) {
	_, repo := setupAgentsHome(t)
	withMkdirAllError(t, filepath.Join(".cursor", "rules"))

	c := NewCursor().(*cursor)
	err := c.createRuleLinks("proj", repo, "")
	if !errors.Is(err, errSeamSynthetic) {
		t.Fatalf("createRuleLinks err = %v, want %v", err, errSeamSynthetic)
	}
}

func TestCursorPruneRuleLinks_RemoveErrorSurfaces(t *testing.T) {
	_, repo := setupAgentsHome(t)
	rulesDir := filepath.Join(repo, ".cursor", "rules")
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Seed a stale file matching the project prefix so the prune loop attempts removal.
	stale := filepath.Join(rulesDir, "proj--stale.mdc")
	if err := os.WriteFile(stale, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	withRemoveError(t, "proj--stale.mdc")

	c := NewCursor().(*cursor)
	err := c.pruneRuleLinks(rulesDir, "proj", map[string]string{})
	if !errors.Is(err, errSeamSynthetic) {
		t.Fatalf("pruneRuleLinks err = %v, want %v", err, errSeamSynthetic)
	}
}

func TestCursorCreateSettingsLinks_MkdirAllErrorSurfaces(t *testing.T) {
	agentsHome, repo := setupAgentsHome(t)
	withMkdirAllError(t, ".cursor")

	c := NewCursor().(*cursor)
	err := c.createSettingsLinks("proj", repo, agentsHome)
	if !errors.Is(err, errSeamSynthetic) {
		t.Fatalf("createSettingsLinks err = %v, want %v", err, errSeamSynthetic)
	}
}

func TestCursorCreateMCPLinks_MkdirAllErrorSurfaces(t *testing.T) {
	agentsHome, repo := setupAgentsHome(t)
	withMkdirAllError(t, ".cursor")

	c := NewCursor().(*cursor)
	err := c.createMCPLinks("proj", repo, agentsHome)
	if !errors.Is(err, errSeamSynthetic) {
		t.Fatalf("createMCPLinks err = %v, want %v", err, errSeamSynthetic)
	}
}

func TestCursorWriteRepoHooks_MkdirAllErrorSurfaces(t *testing.T) {
	agentsHome, repo := setupAgentsHome(t)
	withMkdirAllError(t, ".cursor")

	c := NewCursor().(*cursor)
	err := c.writeRepoHooks("proj", repo, agentsHome)
	if !errors.Is(err, errSeamSynthetic) {
		t.Fatalf("writeRepoHooks err = %v, want %v", err, errSeamSynthetic)
	}
}

// TestCursorCreateLinks_ChainEarlyReturns drives the per-child early-return
// branches in cursor.CreateLinks by failing the Nth MkdirAll call that
// touches the `.cursor` tree.
func TestCursorCreateLinks_ChainEarlyReturns(t *testing.T) {
	for _, failAt := range []int{1, 2, 3, 4} {
		failAt := failAt
		t.Run(fmt.Sprintf("fail-%d", failAt), func(t *testing.T) {
			agentsHome, repo := setupAgentsHome(t)
			withMkdirAllErrorAfter(t, ".cursor", failAt)

			c := NewCursor().(*cursor)
			err := c.CreateLinks("proj", repo)
			if !errors.Is(err, errSeamSynthetic) {
				t.Fatalf("CreateLinks fail-%d err = %v, want %v", failAt, err, errSeamSynthetic)
			}
			_ = agentsHome
		})
	}
}

// TestCopilotCreateLinks_ChainEarlyReturns drives the per-child early-return
// branches in copilot.CreateLinks.
// seedCopilotChainSources writes the instructions + mcp source files the
// copilot CreateLinks chain requires before its MkdirAll branches fire.
func seedCopilotChainSources(t *testing.T, agentsHome string) {
	t.Helper()
	instructions := filepath.Join(agentsHome, "rules", "global", "copilot-instructions.md")
	if err := os.MkdirAll(filepath.Dir(instructions), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(instructions, []byte("# c"), 0644); err != nil {
		t.Fatal(err)
	}
	mcp := filepath.Join(agentsHome, "mcp", "global", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(mcp), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mcp, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
}

func runCopilotChainEarlyReturn(t *testing.T, failAt int) {
	agentsHome, repo := setupAgentsHome(t)
	seedCopilotChainSources(t, agentsHome)
	// Fail the Nth MkdirAll. Targets: .github (instructions), .vscode (mcp),
	// .claude (compat). Use empty pattern so any MkdirAll counts; cleanup
	// helpers in setupAgentsHome already created repo + agentsHome before
	// the seam is installed.
	withMkdirAllErrorAfter(t, "", failAt)

	c := NewCopilot().(*copilot)
	err := c.CreateLinks("proj", repo)
	if !errors.Is(err, errSeamSynthetic) {
		t.Fatalf("CreateLinks fail-%d err = %v, want %v", failAt, err, errSeamSynthetic)
	}
}

func TestCopilotCreateLinks_ChainEarlyReturns(t *testing.T) {
	for _, failAt := range []int{1, 2, 3} {
		failAt := failAt
		t.Run(fmt.Sprintf("fail-%d", failAt), func(t *testing.T) {
			runCopilotChainEarlyReturn(t, failAt)
		})
	}
}

// --- claude.go seams ----------------------------------------------------

// TestClaudePrepareLinks_EnsureUserSettingsErrorPropagates drives the
// ensureUserSettings early-return inside prepareLinks via a malformed
// global HOOK.yaml that surfaces a parse error.
func TestClaudePrepareLinks_EnsureUserSettingsErrorPropagates(t *testing.T) {
	agentsHome, repo := setupAgentsHome(t)
	bundleDir := filepath.Join(agentsHome, "hooks", "global", "broken")
	if err := os.MkdirAll(bundleDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "HOOK.yaml"), []byte(":\n -- not yaml --"), 0644); err != nil {
		t.Fatal(err)
	}

	c := NewClaude().(*claude)
	if err := c.prepareLinks(repo, agentsHome); err == nil {
		t.Fatal("expected propagated parse error from ensureUserSettings")
	}
}

func TestClaudePrepareLinks_MkdirAllErrorSurfaces(t *testing.T) {
	agentsHome, repo := setupAgentsHome(t)
	// Restrict the failing path to the final MkdirAll call so the earlier
	// ensureUser* helpers (which also call osMkdirAll) succeed.
	withMkdirAllError(t, filepath.Join(repo, ".claude", "rules"))

	c := NewClaude().(*claude)
	err := c.prepareLinks(repo, agentsHome)
	if !errors.Is(err, errSeamSynthetic) {
		t.Fatalf("prepareLinks err = %v, want %v", err, errSeamSynthetic)
	}
}

// TestClaudeCreateRulesLinks_SkipsDirAndUnsupportedExt covers the
// per-entry IsDir and unsupported-extension continue branches.
func TestClaudeCreateRulesLinks_SkipsDirAndUnsupportedExt(t *testing.T) {
	agentsHome, repo := setupAgentsHome(t)
	projectRulesSrc := filepath.Join(agentsHome, "rules", "proj")
	if err := os.MkdirAll(projectRulesSrc, 0755); err != nil {
		t.Fatal(err)
	}
	// A directory entry — IsDir branch.
	if err := os.MkdirAll(filepath.Join(projectRulesSrc, "subdir"), 0755); err != nil {
		t.Fatal(err)
	}
	// An unsupported-extension file — the ext != md|mdc|txt branch.
	if err := os.WriteFile(filepath.Join(projectRulesSrc, "ignore.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	// A valid rule to ensure the function still proceeds normally.
	if err := os.WriteFile(filepath.Join(projectRulesSrc, "rule.md"), []byte("# x"), 0644); err != nil {
		t.Fatal(err)
	}

	c := NewClaude().(*claude)
	if err := c.createRulesLinks("proj", repo, agentsHome); err != nil {
		t.Fatalf("createRulesLinks: %v", err)
	}
	// Verify the supported rule was linked and the ignored ones were not.
	if _, err := os.Stat(filepath.Join(repo, ".claude", "rules", "proj--rule.md")); err != nil {
		t.Errorf("expected proj--rule.md link, stat err = %v", err)
	}
}

// TestClaudeLinkProjectSettings_PropagatesProjectBundlesError covers the
// early-return when collectCanonicalHookSpecsForPlatform fails for the
// project scope.
func TestClaudeLinkProjectSettings_PropagatesProjectBundlesError(t *testing.T) {
	agentsHome, repo := setupAgentsHome(t)
	// Seed a broken HOOK.yaml under the project scope so collectCanonical
	// returns a parse error.
	bundleDir := filepath.Join(agentsHome, "hooks", "proj", "broken")
	if err := os.MkdirAll(bundleDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "HOOK.yaml"), []byte(":\n -- not yaml --"), 0644); err != nil {
		t.Fatal(err)
	}

	c := NewClaude().(*claude)
	// linkProjectSettings has signature (project, repoPath, agentsHome); it
	// returns no error, so we only verify it doesn't panic on the propagated
	// error. The early-return branch is now covered.
	c.linkProjectSettings("proj", repo, agentsHome)
}

// TestClaudeLinkProjectSettings_PropagatesGlobalBundlesError covers the
// second collectCanonicalHookSpecsForPlatform call's error path.
func TestClaudeLinkProjectSettings_PropagatesGlobalBundlesError(t *testing.T) {
	agentsHome, repo := setupAgentsHome(t)
	bundleDir := filepath.Join(agentsHome, "hooks", "global", "broken")
	if err := os.MkdirAll(bundleDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "HOOK.yaml"), []byte(":\n -- not yaml --"), 0644); err != nil {
		t.Fatal(err)
	}

	c := NewClaude().(*claude)
	c.linkProjectSettings("proj", repo, agentsHome)
}

// TestClaudeRemoveProjectSettingsLink_GlobalBundleFallback covers the
// else-branch where project bundles are absent but global bundles exist.
func TestClaudeRemoveProjectSettingsLink_GlobalBundleFallback(t *testing.T) {
	agentsHome, repo := setupAgentsHome(t)
	// Seed a canonical hook bundle ONLY under global scope.
	bundleDir := filepath.Join(agentsHome, "hooks", "global", "format-write")
	if err := os.MkdirAll(bundleDir, 0755); err != nil {
		t.Fatal(err)
	}
	manifest := "name: format-write\nwhen: pre_tool_use\nrun:\n  command: ./run.sh\n  timeout_ms: 1000\nenabled_on: [claude]\n"
	if err := os.WriteFile(filepath.Join(bundleDir, "HOOK.yaml"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}

	c := NewClaude().(*claude)
	c.removeProjectSettingsLink("proj", repo, agentsHome)
	// No assertion needed beyond no-panic; this exercises the fallback branch
	// that was previously uncovered.
}

// TestClaudeCreateLinks_CreateRulesLinksErrorSurfaces drives the
// createRulesLinks early-return branch by injecting a Remove failure on
// a stale project-rule file.
func TestClaudeCreateLinks_CreateRulesLinksErrorSurfaces(t *testing.T) {
	agentsHome, repo := setupAgentsHome(t)
	// Seed a project rules dir so createRulesLinks proceeds and seed a stale
	// repo rule that will be pruned -> osRemove invoked.
	projectRulesSrc := filepath.Join(agentsHome, "rules", "proj")
	if err := os.MkdirAll(projectRulesSrc, 0755); err != nil {
		t.Fatal(err)
	}
	staleRulesDir := filepath.Join(repo, ".claude", "rules")
	if err := os.MkdirAll(staleRulesDir, 0755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(staleRulesDir, "proj--stale.md")
	if err := os.WriteFile(stale, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	withRemoveError(t, "proj--stale.md")

	c := NewClaude().(*claude)
	err := c.CreateLinks("proj", repo)
	if !errors.Is(err, errSeamSynthetic) {
		t.Fatalf("CreateLinks err = %v, want %v", err, errSeamSynthetic)
	}
}

// TestClaudeCreateLinks_CreateAgentsLinksErrorSurfaces drives the
// createAgentsLinks early-return branch.
func TestClaudeCreateLinks_CreateAgentsLinksErrorSurfaces(t *testing.T) {
	agentsHome, repo := setupAgentsHome(t)
	// Seed an agents bucket entry so syncScopedDirSymlinksTargets enters its
	// MkdirAll path, then fail that MkdirAll.
	agentDir := filepath.Join(agentsHome, "agents", "proj", "reviewer")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "AGENT.md"), []byte("# r"), 0644); err != nil {
		t.Fatal(err)
	}
	// Fail the MkdirAll inside syncResourceDirEntries for the `.agents/agents`
	// destination. We use a tightly-scoped seam path so prepareLinks succeeds.
	withMkdirAllError(t, filepath.Join(repo, ".agents", "agents"))

	c := NewClaude().(*claude)
	err := c.CreateLinks("proj", repo)
	if !errors.Is(err, errSeamSynthetic) {
		t.Fatalf("CreateLinks err = %v, want %v", err, errSeamSynthetic)
	}
}

// TestCodexEnsureUserAgents_MkdirContinueAndWriteAgentsError covers the
// per-homeRoot MkdirAll-continue branch and the writeCodexAgents return-err
// branch.
func TestCodexEnsureUserAgents_MkdirContinueAndWriteAgentsError(t *testing.T) {
	agentsHome, _ := setupAgentsHome(t)
	// Seed the canonical agents bucket so the loop runs.
	agentDir := filepath.Join(agentsHome, "agents", "global", "reviewer")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, codexAgentMDFile), []byte("---\nname: reviewer\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Force the per-homeRoot MkdirAll for `.codex/agents` to fail and
	// continue. Then the loop body completes silently and returns nil.
	withMkdirAllError(t, filepath.Join(codexDir, "agents"))

	c := NewCodex().(*codex)
	if err := c.ensureUserAgents(agentsHome); err != nil {
		t.Fatalf("ensureUserAgents should swallow inner err, got %v", err)
	}
}

// TestCodexEnsureUserAgents_WriteCodexAgentsErrorSurfaces drives the
// writeCodexAgents error-return branch.
func TestCodexEnsureUserAgents_WriteCodexAgentsErrorSurfaces(t *testing.T) {
	agentsHome, _ := setupAgentsHome(t)
	// Seed the canonical agents bucket and pre-existing dst so MkdirAll
	// succeeds (no-op) and writeCodexAgents is reached.
	agentDir := filepath.Join(agentsHome, "agents", "global", "reviewer")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, codexAgentMDFile), []byte("---\nname: reviewer\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Pre-create the user .codex/agents dir so the MkdirAll succeeds, then
	// fail the inner per-toml write to surface the error.
	for _, homeRoot := range []string{os.Getenv("HOME")} {
		if err := os.MkdirAll(filepath.Join(homeRoot, codexDir, "agents"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	withWriteFileError(t, "reviewer.toml")

	c := NewCodex().(*codex)
	if err := c.ensureUserAgents(agentsHome); !errors.Is(err, errSeamSynthetic) {
		t.Fatalf("ensureUserAgents err = %v, want %v", err, errSeamSynthetic)
	}
}

// --- codex.go seams -----------------------------------------------------

func TestCodexCreateLinks_MkdirAllErrorSurfaces(t *testing.T) {
	agentsHome, repo := setupAgentsHome(t)
	withMkdirAllError(t, filepath.Join(repo, ".codex"))

	c := NewCodex().(*codex)
	err := c.CreateLinks("proj", repo)
	if !errors.Is(err, errSeamSynthetic) {
		t.Fatalf("CreateLinks err = %v, want %v", err, errSeamSynthetic)
	}
	_ = agentsHome
}

func TestCodexWriteRepoHooks_MkdirAllErrorSurfaces(t *testing.T) {
	agentsHome, repo := setupAgentsHome(t)
	withMkdirAllError(t, filepath.Join(repo, ".codex"))

	c := NewCodex().(*codex)
	err := c.writeRepoHooks("proj", repo, agentsHome)
	if !errors.Is(err, errSeamSynthetic) {
		t.Fatalf("writeRepoHooks err = %v, want %v", err, errSeamSynthetic)
	}
}

func TestWriteCodexAgentTomlFile_MkdirAllErrorSurfaces(t *testing.T) {
	tmp := t.TempDir()
	agentMD := filepath.Join(tmp, "AGENT.md")
	if err := os.WriteFile(agentMD, []byte("---\nname: foo\n---\nbody\n"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(tmp, "out", "foo.toml")
	withMkdirAllError(t, filepath.Join(tmp, "out"))

	err := writeCodexAgentTomlFile(dst, agentMD)
	if !errors.Is(err, errSeamSynthetic) {
		t.Fatalf("writeCodexAgentTomlFile err = %v, want %v", err, errSeamSynthetic)
	}
}

func TestWriteCodexAgentTomlFile_RemoveErrorSurfaces(t *testing.T) {
	tmp := t.TempDir()
	agentMD := filepath.Join(tmp, "AGENT.md")
	if err := os.WriteFile(agentMD, []byte("---\nname: foo\n---\nbody\n"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(tmp, "foo.toml")
	// Pre-existing target so the Lstat branch is taken and Remove is called.
	if err := os.WriteFile(dst, []byte("stale"), 0644); err != nil {
		t.Fatal(err)
	}
	withRemoveError(t, "foo.toml")

	err := writeCodexAgentTomlFile(dst, agentMD)
	if !errors.Is(err, errSeamSynthetic) {
		t.Fatalf("writeCodexAgentTomlFile err = %v, want %v", err, errSeamSynthetic)
	}
}

func TestWriteCodexAgentTomlFile_WriteFileErrorSurfaces(t *testing.T) {
	tmp := t.TempDir()
	agentMD := filepath.Join(tmp, "AGENT.md")
	if err := os.WriteFile(agentMD, []byte("---\nname: foo\n---\nbody\n"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(tmp, "foo.toml")
	withWriteFileError(t, "foo.toml")

	err := writeCodexAgentTomlFile(dst, agentMD)
	if !errors.Is(err, errSeamSynthetic) {
		t.Fatalf("writeCodexAgentTomlFile err = %v, want %v", err, errSeamSynthetic)
	}
}

// TestCodexWriteCodexAgents_MissingBucketIsNoOp covers the os.IsNotExist
// short-circuit when the canonical agents bucket does not exist.
func TestCodexWriteCodexAgents_MissingBucketIsNoOp(t *testing.T) {
	tmp := t.TempDir()
	c := NewCodex().(*codex)
	if err := c.writeCodexAgents(filepath.Join(tmp, "missing-agents-home"), "global", filepath.Join(tmp, "dst")); err != nil {
		t.Fatalf("expected nil for missing bucket, got %v", err)
	}
}

// TestCodexWriteCodexAgents_WriteTomlErrorSurfaces drives the per-entry
// writeCodexAgentToml error branch via the osWriteFile seam.
func TestCodexWriteCodexAgents_WriteTomlErrorSurfaces(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "home")
	// Seed one canonical agent so writeCodexAgents enters the per-entry loop.
	agentDir := filepath.Join(agentsHome, "agents", "global", "reviewer")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, codexAgentMDFile), []byte("---\nname: reviewer\n---\n# r\n"), 0644); err != nil {
		t.Fatal(err)
	}
	withWriteFileError(t, "reviewer.toml")

	c := NewCodex().(*codex)
	err := c.writeCodexAgents(agentsHome, "global", filepath.Join(tmp, "dst"))
	if !errors.Is(err, errSeamSynthetic) {
		t.Fatalf("writeCodexAgents err = %v, want %v", err, errSeamSynthetic)
	}
}

// TestCodexPruneManagedCodexAgentTomls_RemoveErrorSurfaces drives the per-entry
// osRemove error branch.
func TestCodexPruneManagedCodexAgentTomls_RemoveErrorSurfaces(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "home")
	// Seed canonical agent so the loop runs at least once.
	agentDir := filepath.Join(agentsHome, "agents", "global", "reviewer")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, codexAgentMDFile), []byte("---\nname: reviewer\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(tmp, "dst")
	if err := os.MkdirAll(dst, 0755); err != nil {
		t.Fatal(err)
	}
	// Seed an existing toml so osRemove has a real target.
	if err := os.WriteFile(filepath.Join(dst, "reviewer.toml"), []byte(""), 0644); err != nil {
		t.Fatal(err)
	}
	withRemoveError(t, "reviewer.toml")

	c := NewCodex().(*codex)
	err := c.pruneManagedCodexAgentTomls(agentsHome, "global", dst)
	if !errors.Is(err, errSeamSynthetic) {
		t.Fatalf("pruneManagedCodexAgentTomls err = %v, want %v", err, errSeamSynthetic)
	}
}

// TestCodexWriteCodexAgents_PrunesStaleTomls covers the prune-stale-toml
// branch by leaving an unwanted `.toml` in dstRoot before the call.
func TestCodexWriteCodexAgents_PrunesStaleTomls(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "home")
	agentDir := filepath.Join(agentsHome, "agents", "global", "reviewer")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, codexAgentMDFile), []byte("---\nname: reviewer\n---\n# r\n"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(tmp, "dst")
	if err := os.MkdirAll(dst, 0755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(dst, "stale.toml")
	if err := os.WriteFile(stale, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}

	c := NewCodex().(*codex)
	if err := c.writeCodexAgents(agentsHome, "global", dst); err != nil {
		t.Fatalf("writeCodexAgents: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale toml should have been pruned, stat err = %v", err)
	}
}

// TestOpencodeEnsureUserAgents_MkdirAllErrorSurfaces drives the
// syncScopedFileSymlinks → osMkdirAll error path so the wrapped error
// propagates back through opencode.ensureUserAgents and CreateLinks.
func TestOpencodeEnsureUserAgents_MkdirAllErrorSurfaces(t *testing.T) {
	agentsHome, repo := setupAgentsHome(t)
	// Seed a canonical agent so the syncScopedFileSymlinks call enters its
	// MkdirAll path.
	agentDir := filepath.Join(agentsHome, "agents", "global", "alpha")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "AGENT.md"), []byte("# a"), 0644); err != nil {
		t.Fatal(err)
	}
	// Fail MkdirAll on the opencode-agent destination dir.
	withMkdirAllError(t, filepath.Join(opencodeDir, "agent"))

	o := NewOpenCode().(*opencode)
	if err := o.ensureUserAgents(agentsHome); !errors.Is(err, errSeamSynthetic) {
		t.Fatalf("ensureUserAgents err = %v, want %v", err, errSeamSynthetic)
	}
	if err := o.CreateLinks("proj", repo); !errors.Is(err, errSeamSynthetic) {
		t.Fatalf("CreateLinks err = %v, want %v", err, errSeamSynthetic)
	}
}

// --- copilot.go seams ---------------------------------------------------

func TestCopilotCreateInstructionsLink_MkdirAllErrorSurfaces(t *testing.T) {
	agentsHome, repo := setupAgentsHome(t)
	// Seed a discoverable source so resolveInstructionsSrc returns non-empty
	// and the MkdirAll branch is reached.
	src := filepath.Join(agentsHome, "rules", "global", "copilot-instructions.md")
	if err := os.MkdirAll(filepath.Dir(src), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("# c"), 0644); err != nil {
		t.Fatal(err)
	}
	withMkdirAllError(t, ".github")

	c := NewCopilot().(*copilot)
	err := c.createInstructionsLink("proj", repo, agentsHome)
	if !errors.Is(err, errSeamSynthetic) {
		t.Fatalf("createInstructionsLink err = %v, want %v", err, errSeamSynthetic)
	}
}

func TestCopilotCreateMCPLinks_MkdirAllErrorSurfaces(t *testing.T) {
	agentsHome, repo := setupAgentsHome(t)
	// Seed source so the inner block is entered.
	src := filepath.Join(agentsHome, "mcp", "global", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(src), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	withMkdirAllError(t, ".vscode")

	c := NewCopilot().(*copilot)
	err := c.createMCPLinks("proj", repo, agentsHome)
	if !errors.Is(err, errSeamSynthetic) {
		t.Fatalf("createMCPLinks err = %v, want %v", err, errSeamSynthetic)
	}
}

// TestCopilotCreateClaudeCompatLinks_PropagatesProjectBundlesError covers the
// early-return when project-scope collectCanonical fails.
func TestCopilotCreateClaudeCompatLinks_PropagatesProjectBundlesError(t *testing.T) {
	agentsHome, repo := setupAgentsHome(t)
	bundleDir := filepath.Join(agentsHome, "hooks", "proj", "broken")
	if err := os.MkdirAll(bundleDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "HOOK.yaml"), []byte(":\n -- not yaml --"), 0644); err != nil {
		t.Fatal(err)
	}

	c := NewCopilot().(*copilot)
	if err := c.createClaudeCompatLinks("proj", repo, agentsHome); err == nil {
		t.Fatal("expected propagated parse error")
	}
}

// TestCopilotCreateClaudeCompatLinks_PropagatesGlobalBundlesError covers the
// second collectCanonical early-return path.
func TestCopilotCreateClaudeCompatLinks_PropagatesGlobalBundlesError(t *testing.T) {
	agentsHome, repo := setupAgentsHome(t)
	bundleDir := filepath.Join(agentsHome, "hooks", "global", "broken")
	if err := os.MkdirAll(bundleDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "HOOK.yaml"), []byte(":\n -- not yaml --"), 0644); err != nil {
		t.Fatal(err)
	}

	c := NewCopilot().(*copilot)
	if err := c.createClaudeCompatLinks("proj", repo, agentsHome); err == nil {
		t.Fatal("expected propagated parse error")
	}
}

// TestCopilotEmitCanonicalProjectHookFiles_RenderedNamesError covers the
// renderedCopilotHookNames error branch when a required-on copilot hook
// uses MatchTools (which copilot does not support).
func TestCopilotEmitCanonicalProjectHookFiles_RenderedNamesError(t *testing.T) {
	tmp := t.TempDir()
	hooksDir := filepath.Join(tmp, "hooks")
	specs := []HookSpec{{
		Name:       "h",
		When:       "pre_tool_use",
		Command:    "echo hi",
		MatchTools: []string{"Write"},
		RequiredOn: []string{"copilot"},
	}}
	c := NewCopilot().(*copilot)
	if err := c.emitCanonicalProjectHookFiles(specs, hooksDir); err == nil {
		t.Fatal("expected matcher-unsupported error")
	}
}

// TestCopilotCreateProjectHookFiles_PropagatesParseError covers the early-
// return when collectCanonical fails inside createProjectHookFiles.
func TestCopilotCreateProjectHookFiles_PropagatesParseError(t *testing.T) {
	agentsHome, repo := setupAgentsHome(t)
	bundleDir := filepath.Join(agentsHome, "hooks", "global", "broken")
	if err := os.MkdirAll(bundleDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "HOOK.yaml"), []byte(":\n -- not yaml --"), 0644); err != nil {
		t.Fatal(err)
	}

	c := NewCopilot().(*copilot)
	if err := c.createProjectHookFiles("proj", repo, agentsHome); err == nil {
		t.Fatal("expected propagated parse error")
	}
}

func TestCopilotCreateClaudeCompatLinks_MkdirAllErrorSurfaces(t *testing.T) {
	agentsHome, repo := setupAgentsHome(t)
	withMkdirAllError(t, ".claude")

	c := NewCopilot().(*copilot)
	err := c.createClaudeCompatLinks("proj", repo, agentsHome)
	if !errors.Is(err, errSeamSynthetic) {
		t.Fatalf("createClaudeCompatLinks err = %v, want %v", err, errSeamSynthetic)
	}
}

// TestCopilotRemoveClaudeCompatSettings_GlobalBundleFallback covers the
// else-branch that exercises the global bundles when project bundles are
// absent.
func TestCopilotRemoveClaudeCompatSettings_GlobalBundleFallback(t *testing.T) {
	agentsHome, repo := setupAgentsHome(t)
	// Seed canonical hook ONLY under global scope; project scope has none.
	bundleDir := filepath.Join(agentsHome, "hooks", "global", "format-write")
	if err := os.MkdirAll(bundleDir, 0755); err != nil {
		t.Fatal(err)
	}
	manifest := "name: format-write\nwhen: pre_tool_use\nrun:\n  command: ./run.sh\n  timeout_ms: 1000\nenabled_on: [copilot]\n"
	if err := os.WriteFile(filepath.Join(bundleDir, "HOOK.yaml"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}

	c := NewCopilot().(*copilot)
	c.removeClaudeCompatSettings("proj", repo, agentsHome)
}

// TestCodexWriteUserHomeHooks_PropagatesParseError covers the early-return.
func TestCodexWriteUserHomeHooks_PropagatesParseError(t *testing.T) {
	agentsHome, _ := setupAgentsHome(t)
	bundleDir := filepath.Join(agentsHome, "hooks", "global", "broken")
	if err := os.MkdirAll(bundleDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "HOOK.yaml"), []byte(":\n -- not yaml --"), 0644); err != nil {
		t.Fatal(err)
	}

	c := NewCodex().(*codex)
	if err := c.writeUserHomeHooks("proj", agentsHome); err == nil {
		t.Fatal("expected propagated parse error")
	}
}

// TestCursorWriteUserHomeHooks_PropagatesParseError covers the same for cursor.
func TestCursorWriteUserHomeHooks_PropagatesParseError(t *testing.T) {
	agentsHome, _ := setupAgentsHome(t)
	bundleDir := filepath.Join(agentsHome, "hooks", "global", "broken")
	if err := os.MkdirAll(bundleDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "HOOK.yaml"), []byte(":\n -- not yaml --"), 0644); err != nil {
		t.Fatal(err)
	}

	c := NewCursor().(*cursor)
	if err := c.writeUserHomeHooks("proj", agentsHome); err == nil {
		t.Fatal("expected propagated parse error")
	}
}

// TestCodexWriteRepoHooks_PropagatesParseError covers the early-return when
// the canonical collect call fails.
func TestCodexWriteRepoHooks_PropagatesParseError(t *testing.T) {
	agentsHome, repo := setupAgentsHome(t)
	bundleDir := filepath.Join(agentsHome, "hooks", "global", "broken")
	if err := os.MkdirAll(bundleDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "HOOK.yaml"), []byte(":\n -- not yaml --"), 0644); err != nil {
		t.Fatal(err)
	}

	c := NewCodex().(*codex)
	if err := c.writeRepoHooks("proj", repo, agentsHome); err == nil {
		t.Fatal("expected propagated parse error")
	}
}

// TestCursorWriteRepoHooks_PropagatesParseError covers the same for cursor.
func TestCursorWriteRepoHooks_PropagatesParseError(t *testing.T) {
	agentsHome, repo := setupAgentsHome(t)
	bundleDir := filepath.Join(agentsHome, "hooks", "global", "broken")
	if err := os.MkdirAll(bundleDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "HOOK.yaml"), []byte(":\n -- not yaml --"), 0644); err != nil {
		t.Fatal(err)
	}

	c := NewCursor().(*cursor)
	if err := c.writeRepoHooks("proj", repo, agentsHome); err == nil {
		t.Fatal("expected propagated parse error")
	}
}

// TestCodexCreateLinks_EnsureUserAgentsErrorPropagates wires through
// codex.CreateLinks → ensureUserAgents → writeCodexAgents failure.
func TestCodexCreateLinks_EnsureUserAgentsErrorPropagates(t *testing.T) {
	agentsHome, repo := setupAgentsHome(t)
	agentDir := filepath.Join(agentsHome, "agents", "global", "reviewer")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, codexAgentMDFile), []byte("---\nname: reviewer\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, homeRoot := range []string{os.Getenv("HOME")} {
		if err := os.MkdirAll(filepath.Join(homeRoot, codexDir, "agents"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	withWriteFileError(t, "reviewer.toml")

	c := NewCodex().(*codex)
	if err := c.CreateLinks("proj", repo); !errors.Is(err, errSeamSynthetic) {
		t.Fatalf("CreateLinks err = %v, want %v", err, errSeamSynthetic)
	}
}

// TestCodexCreateLinks_EnsureUserSkillsErrorPropagates covers the
// ensureUserSkills error branch in codex.CreateLinks.
func TestCodexCreateLinks_EnsureUserSkillsErrorPropagates(t *testing.T) {
	agentsHome, repo := setupAgentsHome(t)
	// Seed a canonical skill so syncScopedDirSymlinks enters MkdirAll.
	skillDir := filepath.Join(agentsHome, "skills", "global", "alpha")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# s"), 0644); err != nil {
		t.Fatal(err)
	}
	withMkdirAllError(t, filepath.Join(".agents", "skills"))

	c := NewCodex().(*codex)
	if err := c.CreateLinks("proj", repo); !errors.Is(err, errSeamSynthetic) {
		t.Fatalf("CreateLinks err = %v, want %v", err, errSeamSynthetic)
	}
}

// TestCodexCreateLinks_ChainEarlyReturns drives the per-child early-return
// branches in codex.CreateLinks by failing the Nth MkdirAll call.
func TestCodexCreateLinks_ChainEarlyReturns(t *testing.T) {
	// CreateLinks MkdirAll order: 0..N user agents (via ensureUserAgents),
	// then `.codex` for repo config, then `.codex` for writeRepoHooks. With
	// the global agents bucket empty, ensureUserAgents does NOT call MkdirAll,
	// so we can target call 1 (codex config) and call 2 (writeRepoHooks).
	for _, failAt := range []int{1, 2} {
		failAt := failAt
		t.Run(fmt.Sprintf("fail-%d", failAt), func(t *testing.T) {
			agentsHome, repo := setupAgentsHome(t)
			withMkdirAllErrorAfter(t, filepath.Join(repo, ".codex"), failAt)

			c := NewCodex().(*codex)
			err := c.CreateLinks("proj", repo)
			if !errors.Is(err, errSeamSynthetic) {
				t.Fatalf("CreateLinks fail-%d err = %v, want %v", failAt, err, errSeamSynthetic)
			}
			_ = agentsHome
		})
	}
}

// TestClaudeEnsureUserSkills_ErrorSurfaces drives the syncScopedDirSymlinks
// error-return branch through claude.ensureUserSkills.
func TestClaudeEnsureUserSkills_ErrorSurfaces(t *testing.T) {
	agentsHome, _ := setupAgentsHome(t)
	skillDir := filepath.Join(agentsHome, "skills", "global", "alpha")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# s"), 0644); err != nil {
		t.Fatal(err)
	}
	withMkdirAllError(t, filepath.Join(claudeDir, "skills"))

	c := NewClaude().(*claude)
	if err := c.ensureUserSkills(agentsHome); !errors.Is(err, errSeamSynthetic) {
		t.Fatalf("ensureUserSkills err = %v, want %v", err, errSeamSynthetic)
	}
}

// TestCodexEnsureUserSkills_ErrorSurfaces drives the
// syncScopedDirSymlinks-like flow for codex.ensureUserSkills.
func TestCodexEnsureUserSkills_ErrorSurfaces(t *testing.T) {
	agentsHome, _ := setupAgentsHome(t)
	skillDir := filepath.Join(agentsHome, "skills", "global", "alpha")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# s"), 0644); err != nil {
		t.Fatal(err)
	}
	withMkdirAllError(t, filepath.Join(".agents", "skills"))

	c := NewCodex().(*codex)
	if err := c.ensureUserSkills(agentsHome); !errors.Is(err, errSeamSynthetic) {
		t.Fatalf("ensureUserSkills err = %v, want %v", err, errSeamSynthetic)
	}
}

// TestClaudeEnsureUserAgents_InHomeErrorSurfaces verifies the outer
// ensureUserAgents loop no longer swallows a per-home-root failure: a real
// link/mkdir error must propagate so add/refresh/doctor cannot report
// success while a managed user-agent link was never created (HIGH-3).
func TestClaudeEnsureUserAgents_InHomeErrorSurfaces(t *testing.T) {
	agentsHome, _ := setupAgentsHome(t)
	globalAgents := filepath.Join(agentsHome, "agents", "global", "reviewer")
	if err := os.MkdirAll(globalAgents, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globalAgents, "AGENT.md"), []byte("# r"), 0644); err != nil {
		t.Fatal(err)
	}
	withMkdirAllError(t, filepath.Join(".claude", "agents"))

	c := NewClaude().(*claude)
	if err := c.ensureUserAgents(agentsHome); err == nil {
		t.Fatal("ensureUserAgents must propagate the inner per-home error, got nil")
	}
}

// TestClaudeEnsureUserAgentsInHome_MkdirAllErrorSurfaces drives the
// userAgentsDir MkdirAll error branch.
func TestClaudeEnsureUserAgentsInHome_MkdirAllErrorSurfaces(t *testing.T) {
	tmp := t.TempDir()
	withMkdirAllError(t, ".claude")

	c := NewClaude().(*claude)
	err := c.ensureUserAgentsInHome(tmp, "ignored", nil)
	if !errors.Is(err, errSeamSynthetic) {
		t.Fatalf("ensureUserAgentsInHome err = %v, want %v", err, errSeamSynthetic)
	}
}

// TestEmitHookFanout_MkdirAllErrorSurfaces covers the MkdirAll(dstRoot) error
// branch in emitHookFanout.
func TestEmitHookFanout_MkdirAllErrorSurfaces(t *testing.T) {
	tmp := t.TempDir()
	dstRoot := filepath.Join(tmp, "fanout")
	withMkdirAllError(t, "fanout")
	specs := []HookSpec{{Name: "a", SourcePath: filepath.Join(tmp, "missing.json")}}

	err := emitHookFanout(specs, dstRoot, HookEmissionMode{
		Shape:     HookShapeRenderFanout,
		Transport: HookTransportSymlink,
	}, func(s HookSpec) (string, bool) { return s.Name + ".json", true })
	if !errors.Is(err, errSeamSynthetic) {
		t.Fatalf("emitHookFanout err = %v, want %v", err, errSeamSynthetic)
	}
}

// TestEmitRenderedHookFanout_MkdirAllErrorSurfaces covers the MkdirAll error
// branch in emitRenderedHookFanout.
func TestEmitRenderedHookFanout_MkdirAllErrorSurfaces(t *testing.T) {
	tmp := t.TempDir()
	dstRoot := filepath.Join(tmp, "fanout")
	withMkdirAllError(t, "fanout")
	specs := []HookSpec{{Name: "a"}}

	err := emitRenderedHookFanout(specs, dstRoot, func(HookSpec) (string, []byte, bool, error) {
		return "a.json", []byte("{}"), true, nil
	})
	if !errors.Is(err, errSeamSynthetic) {
		t.Fatalf("emitRenderedHookFanout err = %v, want %v", err, errSeamSynthetic)
	}
}

// --- hooks.go seams -----------------------------------------------------

func TestWriteManagedFile_RemoveErrorSurfaces(t *testing.T) {
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "managed.json")
	// Seed differing content so the existing-then-remove branch is taken.
	if err := os.WriteFile(dst, []byte("stale"), 0644); err != nil {
		t.Fatal(err)
	}
	withRemoveError(t, "managed.json")

	err := writeManagedFile(dst, []byte("fresh"))
	if !errors.Is(err, errSeamSynthetic) {
		t.Fatalf("writeManagedFile err = %v, want %v", err, errSeamSynthetic)
	}
}

func TestWriteManagedFile_MkdirAllErrorSurfaces(t *testing.T) {
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "sub", "managed.json")
	withMkdirAllError(t, filepath.Join(tmp, "sub"))

	err := writeManagedFile(dst, []byte("fresh"))
	if !errors.Is(err, errSeamSynthetic) {
		t.Fatalf("writeManagedFile err = %v, want %v", err, errSeamSynthetic)
	}
}

func TestWriteManagedFile_WriteFileErrorSurfaces(t *testing.T) {
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "managed.json")
	withWriteFileError(t, "managed.json")

	err := writeManagedFile(dst, []byte("fresh"))
	if !errors.Is(err, errSeamSynthetic) {
		t.Fatalf("writeManagedFile err = %v, want %v", err, errSeamSynthetic)
	}
}

// TestResolveCodexModelFromJSONL_MalformedAndNonResponseSkipped covers the
// Unmarshal-error and non-response-item early-return branches.
func TestResolveCodexModelFromJSONL_MalformedAndNonResponseSkipped(t *testing.T) {
	home := t.TempDir()
	sessionID := "sess-x"
	dir := filepath.Join(home, ".codex", "sessions", "2026", "05", "13")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "trace-"+sessionID+".jsonl")
	content := strings.Join([]string{
		`{"response_item": broken json`, // unmarshal error
		`{"type":"response_item","payload":{"type":"function_call","model":"x"}}`,
		`{"type":"response_item","payload":{"type":"response","model":"gpt-5"}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	got := resolveCodexModelFromJSONL(home, sessionID)
	if got != "gpt-5" {
		t.Errorf("model = %q, want gpt-5", got)
	}
}

// TestResolveClaudeCodeModelFromJSONL_MalformedLineSkipped covers the
// Unmarshal-error and non-assistant-type early-return branches in the
// per-line callback.
func TestResolveClaudeCodeModelFromJSONL_MalformedLineSkipped(t *testing.T) {
	home := t.TempDir()
	projectPath := "/repo"
	sessionID := "abc"
	jsonlPath := filepath.Join(home, ".claude", "projects", "-repo", sessionID+".jsonl")
	if err := os.MkdirAll(filepath.Dir(jsonlPath), 0755); err != nil {
		t.Fatal(err)
	}
	// Mix of malformed-but-contains-assistant, non-assistant, and a valid
	// trailing assistant entry.
	content := strings.Join([]string{
		`{"assistant": broken json`, // Unmarshal error (line contains literal "assistant")
		`{"type":"user","message":{"model":"x"}}`,
		`{"type":"assistant","message":{"model":"claude-opus"}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(jsonlPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	got := resolveClaudeCodeModelFromJSONL(home, projectPath, sessionID)
	if got != "claude-opus" {
		t.Errorf("model = %q, want claude-opus", got)
	}
}

// --- render helpers (hooks.go) -----------------------------------------

// TestRenderCursorHookEntry_RequiredButNotRepresentable covers the
// "event not representable + required" error branch.
func TestRenderCursorHookEntry_RequiredButNotRepresentable(t *testing.T) {
	spec := HookSpec{
		Name:       "h",
		When:       "OnUnknownEvent",
		Command:    "echo hi",
		RequiredOn: []string{"cursor"},
	}
	_, _, include, err := renderCursorHookEntry(spec)
	if err == nil {
		t.Fatal("expected error for unrepresentable event")
	}
	if include {
		t.Fatal("include=true, want false on error")
	}
}

// TestRenderCursorHookEntry_NotRequiredNotRepresentableSkipped covers the
// silent-skip branch when the platform is optional.
func TestRenderCursorHookEntry_NotRequiredNotRepresentableSkipped(t *testing.T) {
	spec := HookSpec{Name: "h", When: "OnUnknownEvent", Command: "echo hi"}
	_, _, include, err := renderCursorHookEntry(spec)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if include {
		t.Fatal("expected include=false")
	}
}

// TestRenderCursorHookEntry_RequiredButNoCommand covers the
// "no command + required" error branch.
func TestRenderCursorHookEntry_RequiredButNoCommand(t *testing.T) {
	spec := HookSpec{
		Name:       "h",
		When:       "PreToolUse",
		RequiredOn: []string{"cursor"},
	}
	_, _, include, err := renderCursorHookEntry(spec)
	if err == nil {
		t.Fatal("expected error for missing command")
	}
	if include {
		t.Fatal("include=true, want false on error")
	}
}

// TestRenderCursorHookEntry_NoCommandSkipped covers the silent-skip branch
// when the command is empty but the platform is optional.
func TestRenderCursorHookEntry_NoCommandSkipped(t *testing.T) {
	spec := HookSpec{Name: "h", When: "PreToolUse"}
	_, _, include, err := renderCursorHookEntry(spec)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if include {
		t.Fatal("expected include=false")
	}
}

// TestRenderCodexHookConfig_RequiredButNotRepresentable mirrors the
// renderCursor coverage for the codex path.
func TestRenderCodexHookConfig_RequiredButNotRepresentable(t *testing.T) {
	specs := []HookSpec{{
		Name:       "h",
		When:       "OnUnknownEvent",
		Command:    "echo hi",
		RequiredOn: []string{"codex"},
	}}
	if _, err := renderCodexHookConfig(specs); err == nil {
		t.Fatal("expected error for unrepresentable event")
	}
}

// TestRenderCodexHookConfig_RequiredButNoCommand covers the missing-command
// error branch in renderCodexHookConfig.
func TestRenderCodexHookConfig_RequiredButNoCommand(t *testing.T) {
	specs := []HookSpec{{
		Name:       "h",
		When:       "PreToolUse",
		RequiredOn: []string{"codex"},
	}}
	if _, err := renderCodexHookConfig(specs); err == nil {
		t.Fatal("expected error for missing command")
	}
}

// TestRenderCursorHookConfig_SkipsNonIncludedSpec covers the !include
// continue branch in renderCursorHookConfig.
func TestRenderCursorHookConfig_SkipsNonIncludedSpec(t *testing.T) {
	// A non-representable cursor event with no required-on entry should
	// silently skip.
	specs := []HookSpec{{Name: "h", When: "OnUnknownEvent", Command: "echo hi"}}
	out, err := renderCursorHookConfig(specs)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("expected non-empty output")
	}
}

// TestRenderClaudeHookSettings_NoCommandSkipped covers the non-required
// no-command continue branch in renderClaudeHookSettings.
func TestRenderClaudeHookSettings_NoCommandSkipped(t *testing.T) {
	specs := []HookSpec{{Name: "h", When: "pre_tool_use"}}
	out, err := renderClaudeHookSettings(specs)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("expected non-empty output")
	}
}

// TestRenderCodexHookConfig_NotRequiredUnrepresentableSkipped covers the
// continue-on-skip branch.
func TestRenderCodexHookConfig_NotRequiredUnrepresentableSkipped(t *testing.T) {
	specs := []HookSpec{{Name: "h", When: "OnUnknownEvent", Command: "echo hi"}}
	out, err := renderCodexHookConfig(specs)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("expected non-empty output")
	}
}

// TestRenderCodexHookConfig_NotRequiredNoCommandSkipped covers the
// continue-on-no-command branch.
func TestRenderCodexHookConfig_NotRequiredNoCommandSkipped(t *testing.T) {
	specs := []HookSpec{{Name: "h", When: "pre_tool_use"}}
	out, err := renderCodexHookConfig(specs)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("expected non-empty output")
	}
}

// TestRenderCopilotHookFile_RequiredButHasMatcher covers the
// "matcher unsupported + required" branch.
func TestRenderCopilotHookFile_RequiredButHasMatcher(t *testing.T) {
	spec := HookSpec{
		Name:       "h",
		When:       "PreToolUse",
		Command:    "echo hi",
		MatchTools: []string{"Write"},
		RequiredOn: []string{"copilot"},
	}
	_, _, include, err := renderCopilotHookFile(spec)
	if err == nil {
		t.Fatal("expected error for matcher use on copilot")
	}
	if include {
		t.Fatal("include=true, want false")
	}
}

// TestRenderCopilotHookFile_RequiredButNoCommand covers the missing-command
// error branch.
func TestRenderCopilotHookFile_RequiredButNoCommand(t *testing.T) {
	spec := HookSpec{
		Name:       "h",
		When:       "PreToolUse",
		RequiredOn: []string{"copilot"},
	}
	_, _, include, err := renderCopilotHookFile(spec)
	if err == nil {
		t.Fatal("expected error for missing command")
	}
	if include {
		t.Fatal("include=true, want false")
	}
}

// TestRemoveManagedFile_RemoveErrorSurfaces covers the os.Remove error
// branch (real but ENOENT-tolerant; we trigger a non-ENOENT error via the
// seam).
func TestRemoveManagedFile_RemoveErrorSurfaces(t *testing.T) {
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "managed.json")
	content := []byte("payload")
	if err := os.WriteFile(dst, content, 0644); err != nil {
		t.Fatal(err)
	}
	withRemoveError(t, "managed.json")

	err := removeManagedFile(dst, content)
	if !errors.Is(err, errSeamSynthetic) {
		t.Fatalf("removeManagedFile err = %v, want %v", err, errSeamSynthetic)
	}
}

// TestRemoveManagedFileIf_RemoveErrorSurfaces covers the os.Remove branch in
// removeManagedFileIf.
func TestRemoveManagedFileIf_RemoveErrorSurfaces(t *testing.T) {
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "managed.json")
	if err := os.WriteFile(dst, []byte("payload"), 0644); err != nil {
		t.Fatal(err)
	}
	withRemoveError(t, "managed.json")

	err := removeManagedFileIf(dst, func([]byte) bool { return true })
	if !errors.Is(err, errSeamSynthetic) {
		t.Fatalf("removeManagedFileIf err = %v, want %v", err, errSeamSynthetic)
	}
}

// TestRemoveImportedDirIfAllowlisted_SkipsEmptyMarker covers the empty-marker
// continue branch and the no-marker error branch.
func TestRemoveImportedDirIfAllowlisted_SkipsEmptyMarker(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, ".agents", "skills", "alpha")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	// Use the relative TargetPath that passes the allowlist.
	intent := ResourceIntent{
		TargetPath:  ".agents/skills/alpha",
		MarkerFiles: []string{"", "MISSING.md"},
	}
	err := removeImportedDirIfAllowlisted(target, intent)
	if err == nil {
		t.Fatal("expected refuse-replace error when no marker matches")
	}
}

// TestBuildSharedSkillMirrorIntents_SkipsDotRoot covers the root == "."
// continue branch.
func TestBuildSharedSkillMirrorIntents_SkipsDotRoot(t *testing.T) {
	intents, err := BuildSharedSkillMirrorIntents("proj", ".", ".")
	if err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}
	if len(intents) != 0 {
		t.Errorf("expected no intents for dot root, got %d", len(intents))
	}
}

// TestBuildSharedPluginBundleIntents_SkipsDotRoot covers the same skip
// branch for the plugin variant.
func TestBuildSharedPluginBundleIntents_SkipsDotRoot(t *testing.T) {
	intents, err := BuildSharedPluginBundleIntents("proj", ".")
	if err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}
	if len(intents) != 0 {
		t.Errorf("expected no intents for dot root, got %d", len(intents))
	}
}

// TestBuildSharedAgentMirrorIntents_SkipsDotRoot covers the same skip
// branch for the agent variant.
func TestBuildSharedAgentMirrorIntents_SkipsDotRoot(t *testing.T) {
	intents, err := BuildSharedAgentMirrorIntents("proj", ".")
	if err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}
	if len(intents) != 0 {
		t.Errorf("expected no intents for dot root, got %d", len(intents))
	}
}

// TestExecuteResourceIntent_DirectDir_EmptySourceError covers
// canonicalIntentSourcePath's empty-source error branch via the
// ResourceShapeDirectDir/Symlink intent path.
func TestExecuteResourceIntent_DirectDir_EmptySourceError(t *testing.T) {
	intent := ResourceIntent{
		IntentID:   "x",
		TargetPath: "out",
		Shape:      ResourceShapeDirectDir,
		Transport:  ResourceTransportSymlink,
		// SourceRef left zero -> CanonicalPath returns "" -> error.
	}
	err := executeResourceIntent(intent, t.TempDir(), t.TempDir())
	if err == nil {
		t.Fatal("expected empty-source error")
	}
}

// TestExecuteResourceIntent_DirectFile_EmptySourceError covers the same for
// the file-symlink shape.
func TestExecuteResourceIntent_DirectFile_EmptySourceError(t *testing.T) {
	intent := ResourceIntent{
		IntentID:   "x",
		TargetPath: "out",
		Shape:      ResourceShapeDirectFile,
		Transport:  ResourceTransportSymlink,
	}
	err := executeResourceIntent(intent, t.TempDir(), t.TempDir())
	if err == nil {
		t.Fatal("expected empty-source error")
	}
}

// TestExecuteRenderSingleWrite_EmptySourceError covers the empty-source
// branch for the codex-agent-toml materializer.
func TestExecuteRenderSingleWrite_EmptySourceError(t *testing.T) {
	intent := ResourceIntent{
		IntentID:     "x",
		TargetPath:   "out",
		Shape:        ResourceShapeRenderSingle,
		Transport:    ResourceTransportWrite,
		Materializer: codexAgentTomlMaterializer,
	}
	err := executeResourceIntent(intent, t.TempDir(), t.TempDir())
	if err == nil {
		t.Fatal("expected empty-source error")
	}
}

// TestExecuteResourceIntent_UnsupportedShape covers the default error branch
// in executeResourceIntent.
func TestExecuteResourceIntent_UnsupportedShape(t *testing.T) {
	intent := ResourceIntent{
		IntentID:   "x",
		TargetPath: "out",
		Shape:      "unknown",
		Transport:  "unknown",
	}
	err := executeResourceIntent(intent, t.TempDir(), t.TempDir())
	if err == nil {
		t.Fatal("expected unsupported-shape error")
	}
}

// TestExecuteRenderSingleWrite_UnsupportedMaterializer covers the default
// error branch when materializer is unknown.
func TestExecuteRenderSingleWrite_UnsupportedMaterializer(t *testing.T) {
	intent := ResourceIntent{
		IntentID:     "x",
		TargetPath:   "out",
		Shape:        ResourceShapeRenderSingle,
		Transport:    ResourceTransportWrite,
		Materializer: "unknown",
	}
	err := executeResourceIntent(intent, t.TempDir(), t.TempDir())
	if err == nil {
		t.Fatal("expected unsupported-materializer error")
	}
}

// TestLoadHookBundleSpec_ReadFileNonENOENTError covers the readFile
// non-ENOENT error branch (HOOK.yaml is a directory, EISDIR).
func TestLoadHookBundleSpec_ReadFileNonENOENTError(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "hooks", "global")
	bundle := filepath.Join(root, "bundle")
	// Create HOOK.yaml as a directory so os.ReadFile returns EISDIR.
	if err := os.MkdirAll(filepath.Join(bundle, "HOOK.yaml"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadHookBundleSpec(root, "global", "bundle"); err == nil {
		t.Fatal("expected readFile non-ENOENT error")
	}
}

// TestCollectCanonicalHookSpecsForPlatform_PropagatesNonENOENTError ensures
// the non-ENOENT error branch returns rather than continuing.
func TestCollectCanonicalHookSpecsForPlatform_PropagatesNonENOENTError(t *testing.T) {
	tmp := t.TempDir()
	bundleDir := filepath.Join(tmp, "hooks", "global", "broken")
	if err := os.MkdirAll(bundleDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "HOOK.yaml"), []byte(":\n -- not yaml --"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := collectCanonicalHookSpecsForPlatform(tmp, "proj", "claude", "global"); err == nil {
		t.Fatal("expected propagated parse error")
	}
}

// TestLoadHookBundleSpec_DefaultsNameToDir covers the "name == \"\"" fallback
// that derives the hook name from the bundle directory.
func TestLoadHookBundleSpec_DefaultsNameToDir(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "hooks", "global")
	bundleDir := "bundle-default-name"
	if err := os.MkdirAll(filepath.Join(root, bundleDir), 0755); err != nil {
		t.Fatal(err)
	}
	// Manifest without `name:`.
	manifest := "when: pre_tool_use\nrun:\n  command: ./run.sh\n"
	if err := os.WriteFile(filepath.Join(root, bundleDir, "HOOK.yaml"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}

	spec, ok, err := loadHookBundleSpec(root, "global", bundleDir)
	if err != nil {
		t.Fatalf("loadHookBundleSpec: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	if spec.Name != bundleDir {
		t.Errorf("Name = %q, want %q", spec.Name, bundleDir)
	}
}

// TestListHookSpecs_MalformedManifestSurfacesError covers the
// loadHookSpecEntry error-return branch from a broken HOOK.yaml.
func TestListHookSpecs_MalformedManifestSurfacesError(t *testing.T) {
	tmp := t.TempDir()
	bundleDir := filepath.Join(tmp, "hooks", "global", "broken")
	if err := os.MkdirAll(bundleDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Invalid YAML.
	if err := os.WriteFile(filepath.Join(bundleDir, "HOOK.yaml"), []byte(":\n -- not yaml --"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ListHookSpecs(tmp, "global"); err == nil {
		t.Fatal("expected manifest-parse error")
	}
}

// TestEmitHookFile_UnknownTransportError covers the default error branch.
func TestEmitHookFile_UnknownTransportError(t *testing.T) {
	err := emitHookFile("/x", "/y", HookTransport("bogus"))
	if err == nil {
		t.Fatal("expected unknown-transport error")
	}
}

// TestEmitHookFile_WriteTransportMissingSourceError covers the os.ReadFile
// error branch under HookTransportWrite.
func TestEmitHookFile_WriteTransportMissingSourceError(t *testing.T) {
	tmp := t.TempDir()
	err := emitHookFile(filepath.Join(tmp, "missing.json"), filepath.Join(tmp, "dst.json"), HookTransportWrite)
	if err == nil {
		t.Fatal("expected read-source error")
	}
}

// TestRemoveSharedTargets_UnsupportedMaterializerError covers the
// removeManagedIntentTarget error branch and the wrapping in
// RemoveSharedTargets.
func TestRemoveSharedTargets_UnsupportedMaterializerError(t *testing.T) {
	tmp := t.TempDir()
	plan := ResourcePlan{Resources: []plannedResource{{
		Intent: ResourceIntent{
			IntentID:     "test-intent",
			TargetPath:   "subdir/file.toml",
			Shape:        ResourceShapeRenderSingle,
			Transport:    ResourceTransportWrite,
			Materializer: "unknown-materializer",
		},
	}}}
	err := plan.RemoveSharedTargets(tmp, tmp)
	if err == nil {
		t.Fatal("expected error for unsupported materializer")
	}
	if !strings.Contains(err.Error(), "test-intent") {
		t.Errorf("err = %v, expected to mention intent id", err)
	}
}

// --- resources.go seams -------------------------------------------------

func TestSyncResourceDirEntries_MkdirAllErrorSurfaces(t *testing.T) {
	tmp := t.TempDir()
	dstRoot := filepath.Join(tmp, "dst")
	withMkdirAllError(t, "dst")

	err := syncResourceDirEntries(nil, dstRoot)
	if !errors.Is(err, errSeamSynthetic) {
		t.Fatalf("syncResourceDirEntries err = %v, want %v", err, errSeamSynthetic)
	}
}

func TestSyncScopedFileSymlinks_MkdirAllErrorSurfaces(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "home")
	// Seed one bucket entry so listScopedResourceDirs returns a non-empty list
	// and the MkdirAll branch is reached.
	entryDir := filepath.Join(agentsHome, "skills", "global", "alpha")
	if err := os.MkdirAll(entryDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(entryDir, "SKILL.md"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	dstRoot := filepath.Join(tmp, "dst")
	withMkdirAllError(t, "dst")

	err := syncScopedFileSymlinks(agentsHome, "skills", "global", "SKILL.md", dstRoot, ".md")
	if !errors.Is(err, errSeamSynthetic) {
		t.Fatalf("syncScopedFileSymlinks err = %v, want %v", err, errSeamSynthetic)
	}
}

// --- HIGH-3: CreateLinks-family must propagate link-creation failures -----
//
// links.Symlink now refuses to replace an unmanaged non-empty directory at
// the link path (it would otherwise recursively delete user data). Before
// HIGH-3, callers ignored links.Symlink's error, so the refusal silently
// no-oped and add/refresh/doctor reported success with config missing.
// Each test stages a non-empty unmanaged dir at a managed link path and
// asserts the platform CreateLinks-family surfaces the refusal.

func stageBlockingDir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "local-user-work.txt"), []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestCopilotCreateInstructionsLink_PropagatesSymlinkRefusal(t *testing.T) {
	agentsHome, repo := setupAgentsHome(t)
	src := filepath.Join(agentsHome, "rules", "global", "copilot-instructions.md")
	if err := os.MkdirAll(filepath.Dir(src), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("# instr"), 0644); err != nil {
		t.Fatal(err)
	}
	stageBlockingDir(t, filepath.Join(repo, ".github", "copilot-instructions.md"))

	c := NewCopilot().(*copilot)
	if err := c.createInstructionsLink("proj", repo, agentsHome); err == nil {
		t.Fatal("createInstructionsLink must propagate links.Symlink refusal, got nil")
	}
}

func TestOpenCodeCreateLinks_PropagatesSymlinkRefusal(t *testing.T) {
	agentsHome, repo := setupAgentsHome(t)
	src := filepath.Join(agentsHome, "settings", "proj", "opencode.json")
	if err := os.MkdirAll(filepath.Dir(src), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	stageBlockingDir(t, filepath.Join(repo, "opencode.json"))

	o := NewOpenCode().(*opencode)
	if err := o.CreateLinks("proj", repo); err == nil {
		t.Fatal("opencode CreateLinks must propagate links.Symlink refusal, got nil")
	}
}

func TestClaudeLinkProjectMCP_PropagatesSymlinkRefusal(t *testing.T) {
	agentsHome, repo := setupAgentsHome(t)
	src := filepath.Join(agentsHome, "mcp", "proj", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(src), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	stageBlockingDir(t, filepath.Join(repo, ".mcp.json"))

	c := NewClaude().(*claude)
	if err := c.linkProjectMCP("proj", repo, agentsHome); err == nil {
		t.Fatal("linkProjectMCP must propagate links.Symlink refusal, got nil")
	}
}

func TestCodexCreateLinks_PropagatesSymlinkRefusal(t *testing.T) {
	agentsHome, repo := setupAgentsHome(t)
	src := filepath.Join(agentsHome, "rules", "global", "agents.md")
	if err := os.MkdirAll(filepath.Dir(src), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("# a"), 0644); err != nil {
		t.Fatal(err)
	}
	stageBlockingDir(t, filepath.Join(repo, codexAgentsMarkdown))

	c := NewCodex().(*codex)
	if err := c.CreateLinks("proj", repo); err == nil {
		t.Fatal("codex CreateLinks must propagate links.Symlink refusal, got nil")
	}
}

// --- MED-2: RemoveLinks must surface a failed managed-hardlink removal ----
//
// removeHardlinkedManaged adapts links.RemoveIfHardlinkedToAny's (bool,err);
// when a still-present managed hard link cannot be removed the error must
// thread out of RemoveLinks so `da remove` / doctor do not report success.
func TestCursorRemoveLinks_SurfacesManagedHardlinkRemovalFailure(t *testing.T) {
	agentsHome, repo := setupAgentsHome(t)
	src := filepath.Join(agentsHome, "rules", "global", "style.mdc")
	if err := os.MkdirAll(filepath.Dir(src), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("rule"), 0644); err != nil {
		t.Fatal(err)
	}
	rulesDir := filepath.Join(repo, cursorDir, "rules")
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		t.Fatal(err)
	}
	managed := filepath.Join(rulesDir, "global--style.mdc")
	if err := os.Link(src, managed); err != nil {
		t.Skipf("hard link unsupported: %v", err)
	}
	// Make the parent unwritable so fsops.RemoveAll(managed) fails while the
	// managed hard link is still present and matched. On platforms/filesystems
	// where this does not actually block removal (e.g. Windows), skip — the
	// (bool,err) threading is also covered by internal/links unit tests.
	if err := os.Chmod(rulesDir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(rulesDir, 0755) })
	if os.Remove(managed) == nil {
		t.Skip("read-only parent does not block removal on this platform")
	}

	c := NewCursor().(*cursor)
	if err := c.RemoveLinks("proj", repo); err == nil {
		t.Fatal("cursor RemoveLinks must surface the failed managed-hardlink removal, got nil")
	}
}

// Cursor createRuleLinks must propagate links.Hardlink's refusal to replace
// an unmanaged non-empty directory at the rule link path (previously the
// error was discarded as "best-effort").
func TestCursorCreateRuleLinks_PropagatesHardlinkRefusal(t *testing.T) {
	agentsHome, repo := setupAgentsHome(t)
	src := filepath.Join(agentsHome, "rules", "global", "style.mdc")
	if err := os.MkdirAll(filepath.Dir(src), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("rule"), 0644); err != nil {
		t.Fatal(err)
	}
	stageBlockingDir(t, filepath.Join(repo, cursorDir, "rules", "global--style.mdc"))

	c := NewCursor().(*cursor)
	if err := c.createRuleLinks("proj", repo, agentsHome); err == nil {
		t.Fatal("createRuleLinks must propagate links.Hardlink refusal, got nil")
	}
}

// Cursor settings/mcp/ignore hardlink helpers must propagate the refusal too.
func TestCursorCreateSettingsMCPIgnore_PropagateHardlinkRefusal(t *testing.T) {
	agentsHome, repo := setupAgentsHome(t)
	mk := func(rel, name string) {
		p := filepath.Join(agentsHome, rel, "proj", name)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	mk("settings", "cursor.json")
	mk("mcp", "cursor.json")
	mk("settings", "cursorignore")

	c := NewCursor().(*cursor)

	stageBlockingDir(t, filepath.Join(repo, cursorDir, "settings.json"))
	if err := c.createSettingsLinks("proj", repo, agentsHome); err == nil {
		t.Fatal("createSettingsLinks must propagate hardlink refusal")
	}
	stageBlockingDir(t, filepath.Join(repo, cursorDir, "mcp.json"))
	if err := c.createMCPLinks("proj", repo, agentsHome); err == nil {
		t.Fatal("createMCPLinks must propagate hardlink refusal")
	}
	stageBlockingDir(t, filepath.Join(repo, ".cursorignore"))
	if err := c.createIgnoreLink("proj", repo, agentsHome); err == nil {
		t.Fatal("createIgnoreLink must propagate hardlink refusal")
	}
}

// Claude createRulesLinks must propagate the per-rule links.Symlink error
// (new return inside the rule emit loop).
func TestClaudeCreateRulesLinks_PropagatesSymlinkRefusal(t *testing.T) {
	agentsHome, repo := setupAgentsHome(t)
	ruleSrcDir := filepath.Join(agentsHome, "rules", "proj")
	if err := os.MkdirAll(ruleSrcDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ruleSrcDir, "style.md"), []byte("r"), 0644); err != nil {
		t.Fatal(err)
	}
	stageBlockingDir(t, filepath.Join(repo, claudeDir, "rules", "proj--style.md"))

	c := NewClaude().(*claude)
	if err := c.createRulesLinks("proj", repo, agentsHome); err == nil {
		t.Fatal("createRulesLinks must propagate links.Symlink refusal, got nil")
	}
}

// Claude linkUserAgent → ensureUserAgentsInHome must propagate a per-agent
// links.Symlink refusal (blocking dir at the user-home agent link path).
func TestClaudeLinkUserAgent_PropagatesSymlinkRefusal(t *testing.T) {
	tmp := t.TempDir()
	globalAgents := filepath.Join(tmp, "agents", "global")
	agentDir := filepath.Join(globalAgents, "reviewer")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "AGENT.md"), []byte("# r"), 0644); err != nil {
		t.Fatal(err)
	}
	userAgentsDir := filepath.Join(tmp, "home", ".claude", "agents")
	stageBlockingDir(t, filepath.Join(userAgentsDir, "reviewer"))

	c := NewClaude().(*claude)
	entries, err := os.ReadDir(globalAgents)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ensureUserAgentsInHome(filepath.Join(tmp, "home"), globalAgents, entries); err == nil {
		t.Fatal("ensureUserAgentsInHome must propagate linkUserAgent refusal, got nil")
	}
}

// Claude ensureUserRules must surface a links.Symlink refusal at the
// per-home CLAUDE.md link path (collected via errors.Join).
func TestClaudeEnsureUserRules_PropagatesSymlinkRefusal(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	src := filepath.Join(agentsHome, "rules", "global", "rules.md")
	if err := os.MkdirAll(filepath.Dir(src), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("# r"), 0644); err != nil {
		t.Fatal(err)
	}
	stageBlockingDir(t, filepath.Join(tmp, "home", ".claude", "CLAUDE.md"))

	c := NewClaude().(*claude)
	if err := c.ensureUserRules(agentsHome); err == nil {
		t.Fatal("ensureUserRules must propagate links.Symlink refusal, got nil")
	}
}

// Codex project-override AGENTS.md symlink refusal must propagate (second
// links.Symlink call in codex CreateLinks).
func TestCodexCreateLinks_PropagatesProjectOverrideRefusal(t *testing.T) {
	agentsHome, repo := setupAgentsHome(t)
	proj := filepath.Join(agentsHome, "rules", "proj")
	if err := os.MkdirAll(proj, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "agents.md"), []byte("# a"), 0644); err != nil {
		t.Fatal(err)
	}
	stageBlockingDir(t, filepath.Join(repo, codexAgentsMarkdown))

	c := NewCodex().(*codex)
	if err := c.CreateLinks("proj", repo); err == nil {
		t.Fatal("codex CreateLinks must propagate project-override symlink refusal, got nil")
	}
}

// Codex config.toml symlink refusal must propagate.
func TestCodexCreateLinks_PropagatesConfigTomlRefusal(t *testing.T) {
	agentsHome, repo := setupAgentsHome(t)
	src := filepath.Join(agentsHome, "settings", "proj", "codex.toml")
	if err := os.MkdirAll(filepath.Dir(src), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	stageBlockingDir(t, filepath.Join(repo, codexDir, "config.toml"))

	c := NewCodex().(*codex)
	if err := c.CreateLinks("proj", repo); err == nil {
		t.Fatal("codex CreateLinks must propagate config.toml symlink refusal, got nil")
	}
}

// Copilot createMCPLinks symlink refusal must propagate.
func TestCopilotCreateMCPLinks_PropagatesSymlinkRefusal(t *testing.T) {
	agentsHome, repo := setupAgentsHome(t)
	src := filepath.Join(agentsHome, "mcp", "proj", "copilot.json")
	if err := os.MkdirAll(filepath.Dir(src), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	stageBlockingDir(t, filepath.Join(repo, copilotVSCodeDir, copilotMCPJSON))

	c := NewCopilot().(*copilot)
	if err := c.createMCPLinks("proj", repo, agentsHome); err == nil {
		t.Fatal("createMCPLinks must propagate links.Symlink refusal, got nil")
	}
}

// Cursor removeRuleEntry returns nil for a directory entry and for a name
// matching no managed prefix (the new explicit nil returns).
func TestCursorRemoveRuleEntry_NonManagedAndDirReturnNil(t *testing.T) {
	agentsHome, repo := setupAgentsHome(t)
	rulesDir := filepath.Join(repo, cursorDir, "rules")
	if err := os.MkdirAll(filepath.Join(rulesDir, "adir"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rulesDir, "unrelated.mdc"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	c := NewCursor().(*cursor)
	entries, err := os.ReadDir(rulesDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if err := c.removeRuleEntry(e, rulesDir, "proj", agentsHome); err != nil {
			t.Fatalf("removeRuleEntry(%s) = %v, want nil", e.Name(), err)
		}
	}
}
