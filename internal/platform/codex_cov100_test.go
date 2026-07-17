package platform

// codex_cov100_test.go drives the last uncovered error-propagation branches of
// codex.go that a healthy filesystem cannot reach on its own: the two
// now-inert CreateLinks sub-step returns, the writeRepoHooks resolveHookSpec
// Stat fault, the writeCodexAgentTomlFile atomic-rename failure, and the
// writeCodexImportConflictReviewNote yaml.Marshal failure. Each uses the
// minimal codex.go seam (or a POSIX read-without-exec dir) rather than a
// process-global clamp. Helpers are prefixed cx100 to avoid collisions.

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// --- seam swap helpers ------------------------------------------------------

func swapCodexCreateAgentsLinks(fn func(*codex, string, string, string) error) func() {
	prev := codexCreateAgentsLinks
	codexCreateAgentsLinks = fn
	return func() { codexCreateAgentsLinks = prev }
}

func swapCodexCreateSkillsLinks(fn func(*codex, string, string, string) error) func() {
	prev := codexCreateSkillsLinks
	codexCreateSkillsLinks = fn
	return func() { codexCreateSkillsLinks = prev }
}

func swapCodexRenameFn(fn func(string, string) error) func() {
	prev := codexRenameFn
	codexRenameFn = fn
	return func() { codexRenameFn = prev }
}

func swapCodexYAMLMarshal(fn func(any) ([]byte, error)) func() {
	prev := codexYAMLMarshal
	codexYAMLMarshal = fn
	return func() { codexYAMLMarshal = prev }
}

// cx100CleanEnv points AGENTS_HOME + HOME at fresh empty temp dirs so every
// CreateLinks pre-step (ensureUserAgents/Skills, linkCodexAgentsMD/ConfigToml)
// is a clean no-op and control reaches the create-links sub-steps.
func cx100CleanEnv(t *testing.T) (repo string) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	repo = filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	return repo
}

// --- CreateLinks sub-step error legs (codex.go:192 / :197) ------------------

func TestCx100CreateLinks_CreateAgentsLinksErrorSurfaces(t *testing.T) {
	repo := cx100CleanEnv(t)
	sentinel := errors.New("injected create-agents-links failure")
	defer swapCodexCreateAgentsLinks(func(*codex, string, string, string) error { return sentinel })()

	c := NewCodex().(*codex)
	if err := c.CreateLinks("proj", repo); !errors.Is(err, sentinel) {
		t.Fatalf("CreateLinks err = %v, want %v", err, sentinel)
	}
}

func TestCx100CreateLinks_CreateSkillsLinksErrorSurfaces(t *testing.T) {
	repo := cx100CleanEnv(t)
	sentinel := errors.New("injected create-skills-links failure")
	// createAgentsLinks must succeed (real, returns nil) so control reaches the
	// skills sub-step.
	defer swapCodexCreateSkillsLinks(func(*codex, string, string, string) error { return sentinel })()

	c := NewCodex().(*codex)
	if err := c.CreateLinks("proj", repo); !errors.Is(err, sentinel) {
		t.Fatalf("CreateLinks err = %v, want %v", err, sentinel)
	}
}

// --- writeRepoHooks resolveHookSpec Stat fault (codex.go:337) ---------------

// TestCx100WriteRepoHooks_ResolveHookSpecErrorSurfaces covers the repo-hooks
// resolveHookSpec fault leg (the symmetric writeUserHomeHooks leg is already
// covered). The project-scope hooks dir is made readable-but-not-searchable
// (mode 0400): collectCanonicalHookSpecsForPlatform's ReadDir over the empty
// dir still succeeds, but resolveHookSpec's Stat of a child (codex.json) fails
// EACCES. POSIX-only fault (read-without-exec has no Windows analogue); the
// merged multi-OS ratchet takes this line from the unix legs.
func TestCx100WriteRepoHooks_ResolveHookSpecErrorSurfaces(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX read-without-execute directory permissions")
	}
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	projHooks := filepath.Join(agentsHome, "hooks", "proj")
	if err := os.MkdirAll(projHooks, 0o755); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(projHooks, 0o400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(projHooks, 0o755) })

	c := NewCodex().(*codex)
	if err := c.writeRepoHooks("proj", repo, agentsHome); err == nil {
		t.Fatal("expected writeRepoHooks to surface the resolveHookSpec Stat error")
	}
}

// --- writeCodexAgentTomlFile atomic-rename failure (codex.go:565) -----------

// TestCx100WriteCodexAgentTomlFile_RenameErrorSurfaces covers the atomic-rename
// failure leg: the same-dir temp file is written, the rename fails, and the
// caller both cleans up the temp and surfaces a wrapped error. A rename of a
// freshly-written same-dir temp cannot fail on a healthy fs, so the codexRenameFn
// seam injects the fault.
func TestCx100WriteCodexAgentTomlFile_RenameErrorSurfaces(t *testing.T) {
	tmp := t.TempDir()
	agentMD := filepath.Join(tmp, "AGENT.md")
	if err := os.WriteFile(agentMD, []byte("---\nname: foo\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(tmp, "foo.toml")
	sentinel := errors.New("injected rename failure")
	defer swapCodexRenameFn(func(string, string) error { return sentinel })()

	err := writeCodexAgentTomlFile(stdPlatformIO{}, dst, agentMD)
	if !errors.Is(err, sentinel) {
		t.Fatalf("writeCodexAgentTomlFile err = %v, want %v", err, sentinel)
	}
	if _, statErr := os.Stat(dst + ".da-toml-tmp"); !os.IsNotExist(statErr) {
		t.Fatalf("temp file must be removed after a rename failure, stat err=%v", statErr)
	}
}

// --- writeCodexImportConflictReviewNote yaml.Marshal failure (codex.go:752) --

// TestCx100WriteImportConflictReviewNote_MarshalErrorSurfaces covers the
// yaml.Marshal error leg of the review-note writer. Marshalling the fixed
// struct never fails in practice, so the codexYAMLMarshal seam injects it.
func TestCx100WriteImportConflictReviewNote_MarshalErrorSurfaces(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	sentinel := errors.New("injected yaml marshal failure")
	defer swapCodexYAMLMarshal(func(any) ([]byte, error) { return nil, sentinel })()

	dst := filepath.Join(tmp, "repo", ".codex", "agents", "x.toml")
	alt := filepath.Join(tmp, "repo", ".codex", "agents", "x.codex-preexisting.toml")
	if err := writeCodexImportConflictReviewNote(dst, alt); !errors.Is(err, sentinel) {
		t.Fatalf("writeCodexImportConflictReviewNote err = %v, want %v", err, sentinel)
	}
}
