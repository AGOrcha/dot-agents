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
func TestCopilotCreateLinks_ChainEarlyReturns(t *testing.T) {
	for _, failAt := range []int{1, 2, 3} {
		failAt := failAt
		t.Run(fmt.Sprintf("fail-%d", failAt), func(t *testing.T) {
			agentsHome, repo := setupAgentsHome(t)
			// Seed sources so MkdirAll branches are reached for instructions/MCP/claude-compat.
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
		})
	}
}

// --- claude.go seams ----------------------------------------------------

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

func TestCopilotCreateClaudeCompatLinks_MkdirAllErrorSurfaces(t *testing.T) {
	agentsHome, repo := setupAgentsHome(t)
	withMkdirAllError(t, ".claude")

	c := NewCopilot().(*copilot)
	err := c.createClaudeCompatLinks("proj", repo, agentsHome)
	if !errors.Is(err, errSeamSynthetic) {
		t.Fatalf("createClaudeCompatLinks err = %v, want %v", err, errSeamSynthetic)
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
