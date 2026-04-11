package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildResourcePlanDedupesIdenticalSharedSkillIntents(t *testing.T) {
	intents := []ResourceIntent{
		validSharedSkillIntent(".agents/skills/review", "claude"),
		validSharedSkillIntent(".agents/skills/review", "codex"),
	}

	plan, err := BuildResourcePlan(intents)
	if err != nil {
		t.Fatalf("BuildResourcePlan returned error: %v", err)
	}
	if len(plan.Resources) != 1 {
		t.Fatalf("len(plan.Resources) = %d, want 1", len(plan.Resources))
	}
	if len(plan.Resources[0].Duplicates) != 1 {
		t.Fatalf("len(plan.Resources[0].Duplicates) = %d, want 1", len(plan.Resources[0].Duplicates))
	}
}

func TestBuildResourcePlanRejectsConflictingSharedSkillIntents(t *testing.T) {
	intents := []ResourceIntent{
		validSharedSkillIntent(".agents/skills/review", "claude"),
		func() ResourceIntent {
			intent := validSharedSkillIntent(".agents/skills/review", "codex")
			intent.SourceRef.RelativePath = "lint"
			intent.IntentID = "skills.proj.lint.agents-skills"
			return intent
		}(),
	}

	_, err := BuildResourcePlan(intents)
	if err == nil {
		t.Fatal("BuildResourcePlan returned nil error")
	}
	if !strings.Contains(err.Error(), "conflicting intents") {
		t.Fatalf("BuildResourcePlan error = %q, want conflict", err)
	}
}

func TestResourcePlanExecuteReplacesAllowlistedImportedSkillDir(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	agentsHome := filepath.Join(tmp, ".agents")

	if err := os.MkdirAll(filepath.Join(repo, ".agents", "skills", "review"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(agentsHome, "skills", "proj", "review"), 0755); err != nil {
		t.Fatal(err)
	}

	importedSkill := filepath.Join(repo, ".agents", "skills", "review", "SKILL.md")
	canonicalSkillDir := filepath.Join(agentsHome, "skills", "proj", "review")
	if err := os.WriteFile(importedSkill, []byte("---\nname: review\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(canonicalSkillDir, "SKILL.md"), []byte("---\nname: canonical-review\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}

	plan, err := BuildResourcePlan([]ResourceIntent{validSharedSkillIntent(".agents/skills/review", "claude")})
	if err != nil {
		t.Fatalf("BuildResourcePlan returned error: %v", err)
	}
	if err := plan.Execute(repo, agentsHome); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	assertSymlinkTarget(t, filepath.Join(repo, ".agents", "skills", "review"), canonicalSkillDir)
}

func TestBuildSharedTargetPlanEmptyPlatforms(t *testing.T) {
	plan, err := BuildSharedTargetPlan("proj", nil)
	if err != nil {
		t.Fatalf("BuildSharedTargetPlan: %v", err)
	}
	if len(plan.Resources) != 0 {
		t.Fatalf("len(plan.Resources) = %d, want 0", len(plan.Resources))
	}
}

func TestDryRunSharedTargetPlanLinesNone(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(filepath.Join(agentsHome, "skills", "proj"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)
	lines, err := DryRunSharedTargetPlanLines("proj", repo, []Platform{NewCodex()})
	if err != nil {
		t.Fatalf("DryRunSharedTargetPlanLines: %v", err)
	}
	if len(lines) != 1 || lines[0] != "shared targets: (none)" {
		t.Fatalf("got %v", lines)
	}
	plan, err := BuildSharedTargetPlan("proj", []Platform{NewCodex()})
	if err != nil {
		t.Fatalf("BuildSharedTargetPlan: %v", err)
	}
	if len(plan.Resources) != 0 {
		t.Fatalf("empty dry-run should match empty BuildSharedTargetPlan, got %d resources", len(plan.Resources))
	}
}

func TestDryRunSharedTargetPlanLinesDedupesCrossPlatform(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	agentsHome := filepath.Join(tmp, ".agents")

	skillDir := filepath.Join(agentsHome, "skills", "proj", "review")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: review\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("AGENTS_HOME", agentsHome)

	platforms := []Platform{NewCodex(), NewOpenCode(), NewCopilot()}
	lines, err := DryRunSharedTargetPlanLines("proj", repo, platforms)
	if err != nil {
		t.Fatalf("DryRunSharedTargetPlanLines: %v", err)
	}
	if len(lines) != 1 {
		t.Fatalf("want 1 merged shared row for codex+opencode+copilot -> .agents/skills/review, got %d: %v", len(lines), lines)
	}
	if !strings.Contains(lines[0], ".agents/skills/review") || !strings.Contains(lines[0], "2 duplicate intent(s) merged") {
		t.Fatalf("unexpected dry-run line: %q", lines[0])
	}
}

func TestBuildResourcePlanDedupesIdenticalSharedAgentIntents(t *testing.T) {
	intents := []ResourceIntent{
		validSharedAgentIntent(".claude/agents/reviewer", "claude"),
		validSharedAgentIntent(".claude/agents/reviewer", "cursor"),
	}

	plan, err := BuildResourcePlan(intents)
	if err != nil {
		t.Fatalf("BuildResourcePlan returned error: %v", err)
	}
	if len(plan.Resources) != 1 {
		t.Fatalf("len(plan.Resources) = %d, want 1", len(plan.Resources))
	}
	if len(plan.Resources[0].Duplicates) != 1 {
		t.Fatalf("len(plan.Resources[0].Duplicates) = %d, want 1", len(plan.Resources[0].Duplicates))
	}
}

func TestResourcePlanExecuteReplacesAllowlistedImportedAgentDir(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	agentsHome := filepath.Join(tmp, ".agents")

	if err := os.MkdirAll(filepath.Join(repo, ".claude", "agents", "reviewer"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(agentsHome, "agents", "proj", "reviewer"), 0755); err != nil {
		t.Fatal(err)
	}

	importedAgent := filepath.Join(repo, ".claude", "agents", "reviewer", "AGENT.md")
	canonicalAgentDir := filepath.Join(agentsHome, "agents", "proj", "reviewer")
	if err := os.WriteFile(importedAgent, []byte("# Imported\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(canonicalAgentDir, "AGENT.md"), []byte("# Canonical\n"), 0644); err != nil {
		t.Fatal(err)
	}

	plan, err := BuildResourcePlan([]ResourceIntent{validSharedAgentIntent(".claude/agents/reviewer", "claude")})
	if err != nil {
		t.Fatalf("BuildResourcePlan returned error: %v", err)
	}
	if err := plan.Execute(repo, agentsHome); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	assertSymlinkTarget(t, filepath.Join(repo, ".claude", "agents", "reviewer"), canonicalAgentDir)
}

func TestCollectAndExecuteSharedTargetPlanDedupesClaudeCursorAgents(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	agentsHome := filepath.Join(tmp, ".agents")

	agentDir := filepath.Join(agentsHome, "agents", "proj", "reviewer")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "AGENT.md"), []byte("# Reviewer\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".claude", "agents"), 0755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("AGENTS_HOME", agentsHome)

	platforms := []Platform{NewClaude(), NewCursor()}
	if err := CollectAndExecuteSharedTargetPlan("proj", repo, platforms); err != nil {
		t.Fatalf("CollectAndExecuteSharedTargetPlan: %v", err)
	}

	target := filepath.Join(repo, ".claude", "agents", "reviewer")
	info, err := os.Lstat(target)
	if err != nil {
		t.Fatalf("Lstat(%s): %v", target, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected symlink at %s, got mode %v", target, info.Mode())
	}
}

func TestCollectAndExecuteSharedTargetPlanWritesOpenCodeAndCopilotAgentFiles(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	agentsHome := filepath.Join(tmp, ".agents")

	agentDir := filepath.Join(agentsHome, "agents", "proj", "reviewer")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatal(err)
	}
	agentMD := filepath.Join(agentDir, "AGENT.md")
	if err := os.WriteFile(agentMD, []byte("# Reviewer\n"), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("AGENTS_HOME", agentsHome)

	if err := CollectAndExecuteSharedTargetPlan("proj", repo, []Platform{NewOpenCode(), NewCopilot()}); err != nil {
		t.Fatalf("CollectAndExecuteSharedTargetPlan: %v", err)
	}

	opencodeLink := filepath.Join(repo, ".opencode", "agent", "reviewer.md")
	copilotLink := filepath.Join(repo, ".github", "agents", "reviewer.agent.md")
	assertSymlinkTarget(t, opencodeLink, agentMD)
	assertSymlinkTarget(t, copilotLink, agentMD)
}

func TestCollectAndExecuteSharedTargetPlanWritesCodexAgentToml(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	agentsHome := filepath.Join(tmp, ".agents")

	agentDir := filepath.Join(agentsHome, "agents", "proj", "implementer")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatal(err)
	}
	content := `---
name: implementer
description: does work
---

# Body
Ship it.
`
	if err := os.WriteFile(filepath.Join(agentDir, "AGENT.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("AGENTS_HOME", agentsHome)

	if err := CollectAndExecuteSharedTargetPlan("proj", repo, []Platform{NewCodex()}); err != nil {
		t.Fatalf("CollectAndExecuteSharedTargetPlan: %v", err)
	}

	tomlPath := filepath.Join(repo, ".codex", "agents", "implementer.toml")
	b, err := os.ReadFile(tomlPath)
	if err != nil {
		t.Fatalf("read toml: %v", err)
	}
	if !strings.Contains(string(b), `name = "implementer"`) || !strings.Contains(string(b), "Ship it.") {
		t.Fatalf("unexpected toml: %s", b)
	}
}

func TestRemoveSharedTargetPlanRemovesSkillSymlink(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	agentsHome := filepath.Join(tmp, ".agents")

	if err := os.MkdirAll(filepath.Join(repo, ".agents", "skills", "review"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(agentsHome, "skills", "proj", "review"), 0755); err != nil {
		t.Fatal(err)
	}

	importedSkill := filepath.Join(repo, ".agents", "skills", "review", "SKILL.md")
	canonicalSkillDir := filepath.Join(agentsHome, "skills", "proj", "review")
	if err := os.WriteFile(importedSkill, []byte("---\nname: review\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(canonicalSkillDir, "SKILL.md"), []byte("---\nname: canonical-review\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("AGENTS_HOME", agentsHome)

	platforms := []Platform{NewClaude()}
	if err := CollectAndExecuteSharedTargetPlan("proj", repo, platforms); err != nil {
		t.Fatalf("CollectAndExecuteSharedTargetPlan: %v", err)
	}
	target := filepath.Join(repo, ".agents", "skills", "review")
	if err := RemoveSharedTargetPlan("proj", repo, platforms); err != nil {
		t.Fatalf("RemoveSharedTargetPlan: %v", err)
	}
	if _, err := os.Lstat(target); err == nil {
		t.Fatal("expected shared skill symlink removed")
	} else if !os.IsNotExist(err) {
		t.Fatalf("Lstat: %v", err)
	}
}

func TestRemoveSharedTargetPlanRemovesCodexAgentToml(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	agentsHome := filepath.Join(tmp, ".agents")

	agentDir := filepath.Join(agentsHome, "agents", "proj", "implementer")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatal(err)
	}
	content := `---
name: implementer
description: does work
---

# Body
Ship it.
`
	if err := os.WriteFile(filepath.Join(agentDir, "AGENT.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("AGENTS_HOME", agentsHome)

	platforms := []Platform{NewCodex()}
	if err := CollectAndExecuteSharedTargetPlan("proj", repo, platforms); err != nil {
		t.Fatalf("CollectAndExecuteSharedTargetPlan: %v", err)
	}
	tomlPath := filepath.Join(repo, ".codex", "agents", "implementer.toml")
	if err := RemoveSharedTargetPlan("proj", repo, platforms); err != nil {
		t.Fatalf("RemoveSharedTargetPlan: %v", err)
	}
	if _, err := os.Stat(tomlPath); !os.IsNotExist(err) {
		t.Fatalf("expected toml removed: %v", err)
	}
}

func TestEnsureFileSymlinkIntentRejectsUnmanagedFileOutsideAllowlist(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	agentsHome := filepath.Join(tmp, ".agents")

	agentDir := filepath.Join(agentsHome, "agents", "proj", "x")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatal(err)
	}
	agentMD := filepath.Join(agentDir, "AGENT.md")
	if err := os.WriteFile(agentMD, []byte("# X\n"), 0644); err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(repo, "blocked", "x.md")
	if err := os.MkdirAll(filepath.Dir(blocker), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blocker, []byte("user"), 0644); err != nil {
		t.Fatal(err)
	}

	intent := ResourceIntent{
		IntentID:    "agents.file.proj.x.test",
		Project:     "proj",
		Bucket:      "agents",
		LogicalName: "x",
		TargetPath:  "blocked/x.md",
		Ownership:   ResourceOwnershipSharedRepo,
		SourceRef: ResourceSourceRef{
			Scope:        "proj",
			Bucket:       "agents",
			RelativePath: filepath.Join("x", "AGENT.md"),
			Kind:         ResourceSourceCanonicalFile,
		},
		Shape:         ResourceShapeDirectFile,
		Transport:     ResourceTransportSymlink,
		Materializer:  "shared-agent-file-symlink",
		ReplacePolicy: ResourceReplaceAllowlistedImportedDirOnly,
		PrunePolicy:   ResourcePruneTarget,
	}
	plan, err := BuildResourcePlan([]ResourceIntent{intent})
	if err != nil {
		t.Fatalf("BuildResourcePlan: %v", err)
	}
	if err := plan.Execute(repo, agentsHome); err == nil {
		t.Fatal("expected error replacing unmanaged file outside allowlist")
	}
}

func TestCollectAndExecuteSharedTargetPlanDedupesCrossPlatform(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	agentsHome := filepath.Join(tmp, ".agents")

	// Set up a skill in agentsHome
	skillDir := filepath.Join(agentsHome, "skills", "proj", "review")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: review\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".agents", "skills"), 0755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("AGENTS_HOME", agentsHome)

	platforms := []Platform{NewCodex(), NewOpenCode(), NewCopilot()}
	if err := CollectAndExecuteSharedTargetPlan("proj", repo, platforms); err != nil {
		t.Fatalf("CollectAndExecuteSharedTargetPlan: %v", err)
	}

	// All three platforms target .agents/skills/review; it should be a single symlink
	target := filepath.Join(repo, ".agents", "skills", "review")
	info, err := os.Lstat(target)
	if err != nil {
		t.Fatalf("Lstat(%s): %v", target, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected symlink at %s, got mode %v", target, info.Mode())
	}
}

func validSharedSkillIntent(targetPath, emitter string) ResourceIntent {
	return ResourceIntent{
		IntentID:    "skills.proj.review." + emitter,
		Project:     "proj",
		Bucket:      "skills",
		LogicalName: "review",
		TargetPath:  targetPath,
		Ownership:   ResourceOwnershipSharedRepo,
		SourceRef: ResourceSourceRef{
			Scope:        "proj",
			Bucket:       "skills",
			RelativePath: "review",
			Kind:         ResourceSourceCanonicalDir,
			Origin:       "shared-skill-mirror",
		},
		Shape:         ResourceShapeDirectDir,
		Transport:     ResourceTransportSymlink,
		Materializer:  "shared-skill-dir-symlink",
		ReplacePolicy: ResourceReplaceAllowlistedImportedDirOnly,
		PrunePolicy:   ResourcePruneTarget,
		MarkerFiles:   []string{"SKILL.md"},
		Provenance: ResourceProvenance{
			Emitter: emitter,
		},
	}
}

func validSharedAgentIntent(targetPath, emitter string) ResourceIntent {
	return ResourceIntent{
		IntentID:    "agents.proj.reviewer." + emitter,
		Project:     "proj",
		Bucket:      "agents",
		LogicalName: "reviewer",
		TargetPath:  targetPath,
		Ownership:   ResourceOwnershipSharedRepo,
		SourceRef: ResourceSourceRef{
			Scope:        "proj",
			Bucket:       "agents",
			RelativePath: "reviewer",
			Kind:         ResourceSourceCanonicalDir,
			Origin:       "shared-agent-mirror",
		},
		Shape:         ResourceShapeDirectDir,
		Transport:     ResourceTransportSymlink,
		Materializer:  "shared-agent-dir-symlink",
		ReplacePolicy: ResourceReplaceAllowlistedImportedDirOnly,
		PrunePolicy:   ResourcePruneTarget,
		MarkerFiles:   []string{"AGENT.md"},
		Provenance: ResourceProvenance{
			Emitter: emitter,
		},
	}
}
