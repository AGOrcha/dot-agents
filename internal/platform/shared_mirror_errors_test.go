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
