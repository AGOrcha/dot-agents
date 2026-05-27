package platform

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/linktest"
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

// TestOpencodeRemoveLinksFullPath exercises every branch of opencode.RemoveLinks
// (relocated from coverage_gap_test.go).
func TestOpencodeRemoveLinksFullPath(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	// Seed an agent file under the agents home, then symlink it into the repo.
	src := filepath.Join(agentsHome, "agents", "proj", "reviewer.md")
	if err := os.MkdirAll(filepath.Dir(src), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("body"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(repo, ".opencode", "agent", "reviewer.md")
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		t.Fatal(err)
	}
	linktest.Link(t, src, dst)
	// Skills symlink.
	skillSrc := filepath.Join(agentsHome, "skills", "proj", "x")
	if err := os.MkdirAll(skillSrc, 0755); err != nil {
		t.Fatal(err)
	}
	skillDst := filepath.Join(repo, ".agents", "skills", "x")
	if err := os.MkdirAll(filepath.Dir(skillDst), 0755); err != nil {
		t.Fatal(err)
	}
	linktest.Link(t, skillSrc, skillDst)

	if err := NewOpenCode().RemoveLinks("proj", repo); err != nil {
		t.Fatalf("RemoveLinks: %v", err)
	}
	if _, err := os.Lstat(dst); !os.IsNotExist(err) {
		t.Error("agent symlink should be removed")
	}
	if _, err := os.Lstat(skillDst); !os.IsNotExist(err) {
		t.Error("skill symlink should be removed")
	}
}

// ---------- P3: Badge + CountLinks (StatusBadger + LinkCounter) ----------

// TestOpenCodeBadge_EmptyProject pins the empty-project contract.
func TestOpenCodeBadge_EmptyProject(t *testing.T) {
	tmp := t.TempDir()
	got := NewOpenCode().(*opencode).Badge("proj", tmp, filepath.Join(tmp, ".agents"))
	if got.Name != "OpenCode" {
		t.Errorf("Badge.Name = %q, want %q", got.Name, "OpenCode")
	}
	if got.Present || got.Broken {
		t.Errorf("empty project: Badge = %+v, want Present=false Broken=false", got)
	}
}

// TestOpenCodeCountLinks_HealthyOpenCodeJSON covers the positive single-file
// branch: a present opencode.json counts as healthy and Badge surfaces
// Present=true.
func TestOpenCodeCountLinks_HealthyOpenCodeJSON(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "opencode.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	o := NewOpenCode().(*opencode)
	ok, broken := o.CountLinks("proj", tmp, filepath.Join(tmp, ".agents"))
	if ok < 1 || broken != 0 {
		t.Errorf("CountLinks = (%d,%d), want (>=1,0)", ok, broken)
	}
	b := o.Badge("proj", tmp, filepath.Join(tmp, ".agents"))
	if !b.Present || b.Broken {
		t.Errorf("Badge = %+v, want Present=true Broken=false", b)
	}
}
