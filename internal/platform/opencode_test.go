package platform

import (
	"os"
	"path/filepath"
	"testing"
)

// TestOpencodeCreateLinks_FullFixture drives ensureUserAgents + settings link.
func TestOpencodeCreateLinks_FullFixture(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	home := filepath.Join(tmp, "home")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatal(err)
	}
	// Agent dir with marker (for ensureUserAgents).
	d := filepath.Join(agentsHome, "agents", "global", "reviewer")
	if err := os.MkdirAll(d, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "AGENT.md"), []byte("body"), 0644); err != nil {
		t.Fatal(err)
	}
	// Project opencode.json
	if err := os.MkdirAll(filepath.Join(agentsHome, "settings", "proj"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentsHome, "settings", "proj", "opencode.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	if err := NewOpenCode().CreateLinks("proj", repo); err != nil {
		t.Fatalf("CreateLinks: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(repo, "opencode.json")); err != nil {
		t.Errorf("opencode.json link missing: %v", err)
	}
	// User home should have agent symlink under .opencode/agent.
	if _, err := os.Lstat(filepath.Join(home, ".opencode", "agent", "reviewer.md")); err != nil {
		t.Errorf("expected user-home agent symlink: %v", err)
	}
}

// TestOpencodeSharedTargetIntents covers the all-branches code path
// (skills + plugins + agents) by seeding the buckets.
func TestOpencodeSharedTargetIntents_Populated(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	// Seed skill, plugin, agent buckets for proj.
	for _, p := range [][]string{
		{"skills", "proj", "alpha", "SKILL.md"},
		{"plugins", "proj", "rt-plugin", "PLUGIN.yaml"},
		{"agents", "proj", "reviewer", "AGENT.md"},
	} {
		dir := filepath.Join(append([]string{agentsHome}, p[:3]...)...)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		content := "name: x"
		if p[3] == "PLUGIN.yaml" {
			content = "schema_version: 1\nkind: native\nname: rt-plugin\nplatforms: [opencode]\n"
		}
		if err := os.WriteFile(filepath.Join(dir, p[3]), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	intents, err := NewOpenCode().SharedTargetIntents("proj")
	if err != nil {
		t.Fatalf("SharedTargetIntents: %v", err)
	}
	if len(intents) == 0 {
		t.Error("expected non-zero intents")
	}
}

// TestOpencodeScanSessionTokensMissingDB drives the missing-db path.
func TestOpencodeScanSessionTokensMissingDB(t *testing.T) {
	got := opencodeScanSessionTokens(t.TempDir(), "")
	if got.InputTokens != 0 {
		t.Errorf("expected zero for missing db, got %+v", got)
	}
}
