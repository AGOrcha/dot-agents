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
