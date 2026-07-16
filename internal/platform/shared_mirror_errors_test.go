package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBuildSharedAgentMirrorIntents_MissingBucketIsEmpty asserts that a
// missing canonical agents/<project>/ directory is treated as "no
// resources yet" — empty slice, no error. Projects that have not been
// promoted/imported anything are legitimate.
func TestBuildSharedAgentMirrorIntents_MissingBucketIsEmpty(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))

	intents, err := BuildSharedAgentMirrorIntents("never-promoted", filepath.Join(".claude", "agents"))
	if err != nil {
		t.Fatalf("ENOENT must not surface as error: %v", err)
	}
	if len(intents) != 0 {
		t.Errorf("want empty intents for missing bucket, got %d", len(intents))
	}
}

// TestBuildSharedAgentMirrorIntents_NonENOENTErrorPropagates asserts that
// a real listScopedResourceDirs failure (here: bucket path is a regular
// file, not a directory — os.ReadDir errors with ENOTDIR) now propagates
// up instead of being silently swallowed. Regression for the
// `return nil` swallow previously inside buildSharedMirrorIntentsForRoot.
func TestBuildSharedAgentMirrorIntents_NonENOENTErrorPropagates(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	// Create agents/<project> as a FILE, not a directory.
	bucketParent := filepath.Join(agentsHome, "agents")
	if err := os.MkdirAll(bucketParent, 0o755); err != nil {
		t.Fatal(err)
	}
	bucketPath := filepath.Join(bucketParent, "proj-not-a-dir")
	if err := os.WriteFile(bucketPath, []byte("masquerading as a dir"), 0o644); err != nil {
		t.Fatal(err)
	}

	intents, err := BuildSharedAgentMirrorIntents("proj-not-a-dir", filepath.Join(".claude", "agents"))
	if err == nil {
		t.Fatalf("expected error from non-ENOENT readdir; got intents=%v", intents)
	}
	if !strings.Contains(err.Error(), "listing canonical agents") {
		t.Errorf("error should mention the failing bucket; got %q", err)
	}
	if intents != nil {
		t.Errorf("error path must return nil intents, got %v", intents)
	}
}

// TestBuildSharedSkillMirrorIntents_NonENOENTErrorPropagates mirrors the
// agent test for the skills code path (same helper, different bucket).
func TestBuildSharedSkillMirrorIntents_NonENOENTErrorPropagates(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	bucketParent := filepath.Join(agentsHome, "skills")
	if err := os.MkdirAll(bucketParent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bucketParent, "proj"), []byte("file masquerade"), 0o644); err != nil {
		t.Fatal(err)
	}

	intents, err := BuildSharedSkillMirrorIntents("proj", filepath.Join(".claude", "skills"))
	if err == nil {
		t.Fatalf("expected error; got intents=%v", intents)
	}
	if !strings.Contains(err.Error(), "listing canonical skills") {
		t.Errorf("error should mention failing bucket; got %q", err)
	}
}

// TestBuildSharedPluginBundleIntents_NonENOENTErrorPropagates same for
// plugins path.
func TestBuildSharedPluginBundleIntents_NonENOENTErrorPropagates(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	bucketParent := filepath.Join(agentsHome, "plugins")
	if err := os.MkdirAll(bucketParent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bucketParent, "proj"), []byte("file masquerade"), 0o644); err != nil {
		t.Fatal(err)
	}

	intents, err := BuildSharedPluginBundleIntents("proj", filepath.Join(".cursor-plugin"))
	if err == nil {
		t.Fatalf("expected error; got intents=%v", intents)
	}
	if !strings.Contains(err.Error(), "listing canonical plugins") {
		t.Errorf("error should mention failing bucket; got %q", err)
	}
}

// TestPruneCodexRepoAgentTomls_NonENOENTErrorPropagates exercises the
// previously-dead non-ENOENT error path. Regression for `return nil` on
// listScopedResourceDirs failures.
func TestPruneCodexRepoAgentTomls_NonENOENTErrorPropagates(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	bucketParent := filepath.Join(agentsHome, "agents")
	if err := os.MkdirAll(bucketParent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bucketParent, "proj"), []byte("masquerade"), 0o644); err != nil {
		t.Fatal(err)
	}

	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := pruneCodexRepoAgentTomls("proj", repo, agentsHome); err == nil {
		t.Fatal("expected ENOTDIR-style error to propagate")
	}
}

// TestPruneCodexRepoAgentTomls_DstReadErrorPropagates exercises the
// previously-dead non-ENOENT path of the inner os.ReadDir(dstRoot) error.
func TestPruneCodexRepoAgentTomls_DstReadErrorPropagates(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	// Make a valid agents bucket so listScopedResourceDirs succeeds with no
	// entries, then make .codex/agents a file (not a dir) → ReadDir ENOTDIR.
	if err := os.MkdirAll(filepath.Join(agentsHome, "agents", "proj"), 0o755); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(tmp, "repo")
	codexDirAgents := filepath.Join(repo, ".codex")
	if err := os.MkdirAll(codexDirAgents, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexDirAgents, "agents"), []byte("masquerade"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := pruneCodexRepoAgentTomls("proj", repo, agentsHome); err == nil {
		t.Fatal("expected ENOTDIR-style error from ReadDir(dstRoot) to propagate")
	}
}

// TestWriteCodexAgents_NonENOENTErrorPropagates exercises the dead branch in
// writeCodexAgents on listScopedResourceDirs failure.
func TestWriteCodexAgents_NonENOENTErrorPropagates(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	bucketParent := filepath.Join(agentsHome, "agents")
	if err := os.MkdirAll(bucketParent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bucketParent, "scope-x"), []byte("masquerade"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := NewCodex().(*codex)
	if err := c.writeCodexAgents(agentsHome, "scope-x", filepath.Join(tmp, "dst")); err == nil {
		t.Fatal("expected ENOTDIR-style error to propagate")
	}
}

// TestPruneManagedCodexAgentTomls_NonENOENTErrorPropagates exercises the dead
// branch in pruneManagedCodexAgentTomls.
func TestPruneManagedCodexAgentTomls_NonENOENTErrorPropagates(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	bucketParent := filepath.Join(agentsHome, "agents")
	if err := os.MkdirAll(bucketParent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bucketParent, "scope-y"), []byte("masquerade"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := NewCodex().(*codex)
	if err := c.pruneManagedCodexAgentTomls(agentsHome, "scope-y", filepath.Join(tmp, "repo"), filepath.Join(tmp, "dst")); err == nil {
		t.Fatal("expected ENOTDIR-style error to propagate")
	}
}

// TestSyncScopedDirSymlinks_MissingBucketIsNoop asserts ENOENT on the source
// bucket is a clean no-op (no error, no destination touched). Mirrors the
// "no resources yet" semantics for the sync-side helpers.
func TestSyncScopedDirSymlinks_MissingBucketIsNoop(t *testing.T) {
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "dst")
	if err := syncScopedDirSymlinks(stdPlatformIO{}, filepath.Join(tmp, "agents-home-missing"), "skills", "global", "SKILL.md", dst); err != nil {
		t.Fatalf("ENOENT bucket must be a no-op: %v", err)
	}
}

// TestSyncScopedDirSymlinks_NonENOENTErrorPropagates asserts a real
// listScopedResourceDirs failure (bucket path is a regular file → ENOTDIR)
// now propagates. Regression for the previous `return nil` swallow.
func TestSyncScopedDirSymlinks_NonENOENTErrorPropagates(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	bucketParent := filepath.Join(agentsHome, "skills")
	if err := os.MkdirAll(bucketParent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bucketParent, "global"), []byte("masquerade"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := syncScopedDirSymlinks(stdPlatformIO{}, agentsHome, "skills", "global", "SKILL.md", filepath.Join(tmp, "dst")); err == nil {
		t.Fatal("expected ENOTDIR-style error to propagate")
	}
}

// TestSyncScopedDirSymlinksTargets_NonENOENTErrorPropagates same regression
// for the multi-target variant.
func TestSyncScopedDirSymlinksTargets_NonENOENTErrorPropagates(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	bucketParent := filepath.Join(agentsHome, "agents")
	if err := os.MkdirAll(bucketParent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bucketParent, "proj"), []byte("masquerade"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := syncScopedDirSymlinksTargets(stdPlatformIO{}, agentsHome, "agents", "proj", "AGENT.md", filepath.Join(tmp, "dst-a"), filepath.Join(tmp, "dst-b"))
	if err == nil {
		t.Fatal("expected ENOTDIR-style error to propagate")
	}
}

// TestSyncScopedFileSymlinks_NonENOENTErrorPropagates same regression for the
// file-symlink variant (used by opencode user-home agents).
func TestSyncScopedFileSymlinks_NonENOENTErrorPropagates(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	bucketParent := filepath.Join(agentsHome, "agents")
	if err := os.MkdirAll(bucketParent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bucketParent, "global"), []byte("masquerade"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := syncScopedFileSymlinks(stdPlatformIO{}, agentsHome, "agents", "global", "AGENT.md", filepath.Join(tmp, "dst"), ".md")
	if err == nil {
		t.Fatal("expected ENOTDIR-style error to propagate")
	}
}

// TestSyncScopedFileSymlinks_MissingBucketIsNoop asserts ENOENT no-ops cleanly.
func TestSyncScopedFileSymlinks_MissingBucketIsNoop(t *testing.T) {
	tmp := t.TempDir()
	if err := syncScopedFileSymlinks(stdPlatformIO{}, filepath.Join(tmp, "no-home"), "agents", "global", "AGENT.md", filepath.Join(tmp, "dst"), ".md"); err != nil {
		t.Fatalf("ENOENT bucket must be a no-op: %v", err)
	}
}

// TestBuildSharedAgentFileSymlinkIntents_MissingBucketIsEmpty asserts ENOENT
// surfaces as empty intents (no error) — projects without agents yet.
func TestBuildSharedAgentFileSymlinkIntents_MissingBucketIsEmpty(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))

	intents, err := BuildSharedAgentFileSymlinkIntents("never-promoted", ".github/agents", ".agent.md")
	if err != nil {
		t.Fatalf("ENOENT must not surface as error: %v", err)
	}
	if len(intents) != 0 {
		t.Errorf("want empty intents for missing bucket, got %d", len(intents))
	}
}

// TestBuildSharedAgentFileSymlinkIntents_NonENOENTErrorPropagates asserts a
// real listScopedResourceDirs failure (bucket path is a regular file, not a
// directory — os.ReadDir errors with ENOTDIR) propagates up. Regression for
// the previous `return nil, nil` swallow.
func TestBuildSharedAgentFileSymlinkIntents_NonENOENTErrorPropagates(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	bucketParent := filepath.Join(agentsHome, "agents")
	if err := os.MkdirAll(bucketParent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bucketParent, "proj-file"), []byte("masquerade"), 0o644); err != nil {
		t.Fatal(err)
	}

	intents, err := BuildSharedAgentFileSymlinkIntents("proj-file", ".github/agents", ".agent.md")
	if err == nil {
		t.Fatalf("expected error from non-ENOENT readdir; got intents=%v", intents)
	}
	if !strings.Contains(err.Error(), "listing canonical agents") {
		t.Errorf("error should mention failing bucket; got %q", err)
	}
	if intents != nil {
		t.Errorf("error path must return nil intents, got %v", intents)
	}
}

// TestBuildSharedCodexAgentTomlIntents_MissingBucketIsEmpty asserts ENOENT
// surfaces as empty intents (no error).
func TestBuildSharedCodexAgentTomlIntents_MissingBucketIsEmpty(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))

	intents, err := BuildSharedCodexAgentTomlIntents("never-promoted")
	if err != nil {
		t.Fatalf("ENOENT must not surface as error: %v", err)
	}
	if len(intents) != 0 {
		t.Errorf("want empty intents for missing bucket, got %d", len(intents))
	}
}

// TestCopilotSharedTargetIntents_AgentFileErrorPropagates exercises the
// previously-dead `if err != nil { return nil, err }` branch at the
// BuildSharedAgentFileSymlinkIntents callsite in copilot.SharedTargetIntents.
func TestCopilotSharedTargetIntents_AgentFileErrorPropagates(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	// skills bucket exists empty (so BuildSharedSkillMirrorIntents succeeds),
	// agents bucket has a file masquerading as a project dir → ReadDir errors.
	if err := os.MkdirAll(filepath.Join(agentsHome, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentsHome, "agents", "broken-proj"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	intents, err := NewCopilot().SharedTargetIntents("broken-proj")
	if err == nil {
		t.Fatalf("expected propagated error from agent file intents; got intents=%v", intents)
	}
	if !strings.Contains(err.Error(), "listing canonical agents") {
		t.Errorf("error should mention failing bucket; got %q", err)
	}
}

// TestOpencodeSharedTargetIntents_AgentFileErrorPropagates exercises the same
// previously-dead branch in opencode.SharedTargetIntents.
func TestOpencodeSharedTargetIntents_AgentFileErrorPropagates(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	if err := os.MkdirAll(filepath.Join(agentsHome, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentsHome, "agents", "broken-proj"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	intents, err := NewOpenCode().SharedTargetIntents("broken-proj")
	if err == nil {
		t.Fatalf("expected propagated error from agent file intents; got intents=%v", intents)
	}
	if !strings.Contains(err.Error(), "listing canonical agents") {
		t.Errorf("error should mention failing bucket; got %q", err)
	}
}

// TestCodexSharedTargetIntents_TomlErrorPropagates exercises the
// previously-dead branch in codex.SharedTargetIntents on the
// BuildSharedCodexAgentTomlIntents callsite.
func TestCodexSharedTargetIntents_TomlErrorPropagates(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	if err := os.MkdirAll(filepath.Join(agentsHome, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentsHome, "agents", "broken-proj"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	intents, err := NewCodex().SharedTargetIntents("broken-proj")
	if err == nil {
		t.Fatalf("expected propagated error from codex toml intents; got intents=%v", intents)
	}
	if !strings.Contains(err.Error(), "listing canonical agents") {
		t.Errorf("error should mention failing bucket; got %q", err)
	}
}

// TestBuildSharedCodexAgentTomlIntents_NonENOENTErrorPropagates asserts the
// same non-ENOENT failure mode propagates for the codex toml builder.
// Regression for the previous `return nil, nil` swallow.
func TestBuildSharedCodexAgentTomlIntents_NonENOENTErrorPropagates(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	bucketParent := filepath.Join(agentsHome, "agents")
	if err := os.MkdirAll(bucketParent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bucketParent, "proj-codex"), []byte("masquerade"), 0o644); err != nil {
		t.Fatal(err)
	}

	intents, err := BuildSharedCodexAgentTomlIntents("proj-codex")
	if err == nil {
		t.Fatalf("expected error from non-ENOENT readdir; got intents=%v", intents)
	}
	if !strings.Contains(err.Error(), "listing canonical agents") {
		t.Errorf("error should mention failing bucket; got %q", err)
	}
	if !strings.Contains(err.Error(), "codex toml") {
		t.Errorf("error should mention codex toml context; got %q", err)
	}
	if intents != nil {
		t.Errorf("error path must return nil intents, got %v", intents)
	}
}
