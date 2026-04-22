package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/config"
)

func TestNewInstallCmd_Long_DescribesMaterializeAndPlatformLinkPass(t *testing.T) {
	cmd := NewInstallCmd()
	if !strings.Contains(cmd.Long, "materializes") {
		t.Fatalf("install Long should mention materializing skills/agents: %s", cmd.Long)
	}
	if !strings.Contains(cmd.Long, "refresh") {
		t.Fatalf("install Long should tie platform link pass to refresh: %s", cmd.Long)
	}
}

func TestFirstResourceCandidatePrefersProjectScopeOverGlobal(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "agentshome")
	project := "myproj"
	name := "docbot"
	projDir := filepath.Join(home, "agents", project, name)
	if err := os.MkdirAll(projDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projDir, "AGENT.md"), []byte("# Docbot\n"), 0644); err != nil {
		t.Fatal(err)
	}

	candidate, srcRoot, found := firstResourceCandidate("agents", name, "AGENT.md", project, []string{home})
	if !found {
		t.Fatal("expected project-scoped agent to resolve")
	}
	if srcRoot != home {
		t.Fatalf("srcRoot = %q, want %q", srcRoot, home)
	}
	want := filepath.Join(home, "agents", project, name)
	if candidate != want {
		t.Fatalf("candidate = %q, want %q", candidate, want)
	}
}

func TestFirstResourceCandidateFallsBackToGlobal(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "agentshome")
	project := "myproj"
	name := "globalonly"
	globalDir := filepath.Join(home, "agents", "global", name)
	if err := os.MkdirAll(globalDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globalDir, "AGENT.md"), []byte("# G\n"), 0644); err != nil {
		t.Fatal(err)
	}

	candidate, _, found := firstResourceCandidate("agents", name, "AGENT.md", project, []string{home})
	if !found {
		t.Fatal("expected global agent to resolve")
	}
	want := filepath.Join(home, "agents", "global", name)
	if candidate != want {
		t.Fatalf("candidate = %q, want %q", candidate, want)
	}
}

// TestFirstResourceCandidatePrefersProjectScopeForSkills mirrors the agents test
// for the "skills" resource type, confirming SKILL.md lookup and project-scoped priority.
func TestFirstResourceCandidatePrefersProjectScopeForSkills(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "agentshome")
	project := "myproj"
	name := "formatter"

	// Both project-scoped and global exist; project-scoped must win.
	projDir := filepath.Join(home, "skills", project, name)
	globalDir := filepath.Join(home, "skills", "global", name)
	for _, d := range []string{projDir, globalDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "SKILL.md"), []byte("# Skill\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	candidate, srcRoot, found := firstResourceCandidate("skills", name, "SKILL.md", project, []string{home})
	if !found {
		t.Fatal("expected project-scoped skill to resolve")
	}
	if srcRoot != home {
		t.Fatalf("srcRoot = %q, want %q", srcRoot, home)
	}
	if candidate != projDir {
		t.Fatalf("candidate = %q, want project-scoped %q", candidate, projDir)
	}
}

// TestFirstResourceCandidateNotFoundReturnsEmpty verifies that a missing resource
// returns found=false regardless of resource type.
func TestFirstResourceCandidateNotFoundReturnsEmpty(t *testing.T) {
	home := t.TempDir()
	project := "p"

	for _, rt := range []struct {
		rtype  string
		marker string
	}{
		{"skills", "SKILL.md"},
		{"agents", "AGENT.md"},
	} {
		_, _, found := firstResourceCandidate(rt.rtype, "ghost", rt.marker, project, []string{home})
		if found {
			t.Errorf("resource type %q: expected not found, got found", rt.rtype)
		}
	}
}

// TestPromoteSkillThenInstallFindsResource verifies the end-to-end promote→install flow:
// after a skill is promoted into ~/.agents/skills/<project>/<name>/, running
// linkInstallResources with empty sources (agentsHome fallback) finds it without error.
// This is the regression case for the pre-fix behaviour where firstResourceCandidate
// only looked in global/ and emitted "not found in any source" for promoted skills.
func TestPromoteSkillThenInstallFindsResource(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agentshome")
	t.Setenv("AGENTS_HOME", agentsHome)

	project := "testproject"
	skillName := "my-promoted-skill"

	// Simulate what PromoteSkillIn does: place the skill at the canonical location.
	canonicalSkillDir := filepath.Join(agentsHome, "skills", project, skillName)
	if err := os.MkdirAll(canonicalSkillDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(canonicalSkillDir, "SKILL.md"), []byte("# MySkill\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// AgentsRC lists the promoted skill with no explicit sources — install must
	// fall back to agentsHome and find it via the project-scoped lookup.
	rc := &config.AgentsRC{Skills: []string{skillName}}
	err := linkInstallResources(project, rc, nil, false)
	if err != nil {
		t.Fatalf("linkInstallResources returned error after promote: %v\n"+
			"(pre-fix: firstResourceCandidate only looked in global/ and would not find the promoted skill)", err)
	}

	// The canonical path must still exist (no destructive side-effects).
	if _, err := os.Stat(canonicalSkillDir); err != nil {
		t.Errorf("canonical skill dir missing after install: %v", err)
	}
}

// TestPromoteAgentThenInstallFindsResource is the same as the skill test but for agents.
func TestPromoteAgentThenInstallFindsResource(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agentshome")
	t.Setenv("AGENTS_HOME", agentsHome)

	project := "testproject"
	agentName := "my-promoted-agent"

	canonicalAgentDir := filepath.Join(agentsHome, "agents", project, agentName)
	if err := os.MkdirAll(canonicalAgentDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(canonicalAgentDir, "AGENT.md"), []byte("# MyAgent\n"), 0644); err != nil {
		t.Fatal(err)
	}

	rc := &config.AgentsRC{Agents: []string{agentName}}
	err := linkInstallResources(project, rc, nil, false)
	if err != nil {
		t.Fatalf("linkInstallResources returned error after agent promote: %v", err)
	}
	if _, err := os.Stat(canonicalAgentDir); err != nil {
		t.Errorf("canonical agent dir missing after install: %v", err)
	}
}
