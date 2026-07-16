package platform

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/links"
	"github.com/AGOrcha/dot-agents/internal/testutil"
)

// stubPlatform implements Platform with fixed SharedTargetIntents for testing
// BuildSharedTargetPlan aggregation (collect → BuildResourcePlan) without
// real platform fixtures.
type stubPlatform struct {
	id      string
	intents []ResourceIntent
	err     error
}

func (s stubPlatform) ID() string                      { return s.id }
func (s stubPlatform) DisplayName() string             { return s.id }
func (s stubPlatform) IsInstalled() bool               { return true }
func (s stubPlatform) Version() string                 { return "" }
func (s stubPlatform) CreateLinks(_, _ string) error   { return nil }
func (s stubPlatform) RemoveLinks(_, _ string) error   { return nil }
func (s stubPlatform) HasDeprecatedFormat(string) bool { return false }
func (s stubPlatform) DeprecatedDetails(string) string { return "" }
func (s stubPlatform) SharedTargetIntents(string) ([]ResourceIntent, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.intents, nil
}

func TestBuildSharedTargetPlanDedupesIdenticalIntentsAcrossPlatforms(t *testing.T) {
	intents := []ResourceIntent{
		validSharedSkillIntent(".agents/skills/review", "stub-a"),
		validSharedSkillIntent(".agents/skills/review", "stub-b"),
	}
	plan, err := BuildSharedTargetPlan("proj", []Platform{
		stubPlatform{id: "stub-a", intents: intents[:1]},
		stubPlatform{id: "stub-b", intents: intents[1:]},
	})
	if err != nil {
		t.Fatalf("BuildSharedTargetPlan: %v", err)
	}
	if len(plan.Resources) != 1 {
		t.Fatalf("len(plan.Resources) = %d, want 1", len(plan.Resources))
	}
	if len(plan.Resources[0].Duplicates) != 1 {
		t.Fatalf("len(Duplicates) = %d, want 1", len(plan.Resources[0].Duplicates))
	}
}

func TestBuildSharedTargetPlanRejectsConflictingIntentsAcrossPlatforms(t *testing.T) {
	conflictB := validSharedSkillIntent(".agents/skills/review", "stub-b")
	conflictB.SourceRef.RelativePath = "lint"
	conflictB.IntentID = "skills.proj.lint.agents-skills"
	_, err := BuildSharedTargetPlan("proj", []Platform{
		stubPlatform{id: "stub-a", intents: []ResourceIntent{validSharedSkillIntent(".agents/skills/review", "stub-a")}},
		stubPlatform{id: "stub-b", intents: []ResourceIntent{conflictB}},
	})
	if err == nil {
		t.Fatal("BuildSharedTargetPlan returned nil error")
	}
	if !strings.Contains(err.Error(), "conflicting intents") {
		t.Fatalf("error = %q, want conflicting intents", err)
	}
}

func TestBuildSharedTargetPlanWrapsSharedIntentCollectionError(t *testing.T) {
	wrapped := errors.New("boom")
	_, err := BuildSharedTargetPlan("proj", []Platform{
		stubPlatform{id: "bad", err: wrapped},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, wrapped) {
		t.Fatalf("errors.Is: got %v, want %v", err, wrapped)
	}
	if !strings.Contains(err.Error(), "bad shared intents") {
		t.Fatalf("error = %q, want platform id in message", err)
	}
}

func TestDryRunSharedTargetPlanLinesPropagatesBuildPlanError(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(filepath.Join(agentsHome, "skills", "proj"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)
	conflictB := validSharedSkillIntent(".agents/skills/review", "stub-b")
	conflictB.SourceRef.RelativePath = "other"
	conflictB.IntentID = "skills.proj.other.agents-skills"
	_, err := DryRunSharedTargetPlanLines("proj", repo, []Platform{
		stubPlatform{id: "stub-a", intents: []ResourceIntent{validSharedSkillIntent(".agents/skills/review", "stub-a")}},
		stubPlatform{id: "stub-b", intents: []ResourceIntent{conflictB}},
	})
	if err == nil {
		t.Fatal("DryRunSharedTargetPlanLines returned nil error")
	}
	if !strings.Contains(err.Error(), "conflicting intents") {
		t.Fatalf("error = %q", err)
	}
}

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
	repo, agentsHome := setupRepoAgentsHome(t)
	_, canonicalSkillDir := writeFixtureImportedSkillPair(t, repo, agentsHome, "proj", "review")

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

func TestRunSharedTargetProjectionDryRunMatchesDryRunLines(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(filepath.Join(agentsHome, "skills", "proj"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)
	plats := []Platform{NewCodex()}
	want, err := DryRunSharedTargetPlanLines("proj", repo, plats)
	if err != nil {
		t.Fatalf("DryRunSharedTargetPlanLines: %v", err)
	}
	got, err := RunSharedTargetProjection("proj", repo, plats, true)
	if err != nil {
		t.Fatalf("RunSharedTargetProjection dry-run: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("len: got %d want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line %d: got %q want %q", i, got[i], want[i])
		}
	}
}

func TestRunSharedTargetProjectionApplyReturnsNilLines(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(filepath.Join(agentsHome, "skills", "proj"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)
	lines, err := RunSharedTargetProjection("proj", repo, []Platform{NewCodex()}, false)
	if err != nil {
		t.Fatalf("RunSharedTargetProjection apply: %v", err)
	}
	if lines != nil {
		t.Fatalf("apply mode should return nil lines, got %#v", lines)
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
	repo, agentsHome := setupRepoAgentsHome(t)
	writeFixtureSkill(t, agentsHome, "proj", "review")
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
	repo, agentsHome := setupRepoAgentsHome(t)

	// Imported (repo-side) agent: a .claude/agents/<name>/AGENT.md the
	// executor must overwrite with a managed link. Use the testutil
	// nested-path helper to absorb the MkdirAll+WriteFile pair.
	testutil.WriteScopeFilePath(t, repo, ".claude", "agents",
		filepath.Join("reviewer", "AGENT.md"), []byte("# Imported\n"))
	canonicalAgentDir := writeFixtureAgent(t, agentsHome, "proj", "reviewer", "# Canonical\n")

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
	repo, agentsHome := setupRepoAgentsHome(t)
	agentDir := writeFixtureAgent(t, agentsHome, "proj", "reviewer", "# Reviewer\n")
	if err := os.MkdirAll(filepath.Join(repo, ".claude", "agents"), 0755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("AGENTS_HOME", agentsHome)

	platforms := []Platform{NewClaude(), NewCursor()}
	if err := CollectAndExecuteSharedTargetPlan("proj", repo, platforms); err != nil {
		t.Fatalf("CollectAndExecuteSharedTargetPlan: %v", err)
	}

	target := filepath.Join(repo, ".claude", "agents", "reviewer")
	if !links.IsManagedLink(target, agentDir) {
		t.Fatalf("expected managed link at %s -> %s", target, agentDir)
	}
}

func TestCollectAndExecuteSharedTargetPlanWritesOpenCodeAndCopilotAgentFiles(t *testing.T) {
	repo, agentsHome := setupRepoAgentsHome(t)
	agentDir := writeFixtureAgent(t, agentsHome, "proj", "reviewer", "# Reviewer\n")
	agentMD := filepath.Join(agentDir, "AGENT.md")
	t.Setenv("AGENTS_HOME", agentsHome)

	if err := CollectAndExecuteSharedTargetPlan("proj", repo, []Platform{NewOpenCode(), NewCopilot()}); err != nil {
		t.Fatalf("CollectAndExecuteSharedTargetPlan: %v", err)
	}

	opencodeLink := filepath.Join(repo, ".opencode", "agent", "reviewer.md")
	copilotLink := filepath.Join(repo, ".github", "agents", "reviewer.agent.md")
	assertSymlinkTarget(t, opencodeLink, agentMD)
	assertSymlinkTarget(t, copilotLink, agentMD)
}

func TestCollectAndExecuteSharedTargetPlanWritesOpenCodePluginBundles(t *testing.T) {
	repo, agentsHome := setupRepoAgentsHome(t)
	pluginDir := writeFixturePlugin(t, agentsHome, "proj", "runtime-plugin", true)
	t.Setenv("AGENTS_HOME", agentsHome)

	if err := CollectAndExecuteSharedTargetPlan("proj", repo, []Platform{NewOpenCode()}); err != nil {
		t.Fatalf("CollectAndExecuteSharedTargetPlan: %v", err)
	}

	assertSymlinkTarget(t, filepath.Join(repo, ".opencode", "plugins", "runtime-plugin"), pluginDir)
}

func TestCollectAndExecuteSharedTargetPlanWritesCodexAgentToml(t *testing.T) {
	repo, agentsHome := setupRepoAgentsHome(t)
	writeFixtureCodexAgent(t, agentsHome)
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

func TestExecutePluginBundleIntentReplacesAllowlistedImportedPluginDir(t *testing.T) {
	repo, agentsHome := setupRepoAgentsHome(t)
	pluginDir := writeFixturePlugin(t, agentsHome, "proj", "runtime-plugin", false)

	target := filepath.Join(repo, ".opencode", "plugins", "runtime-plugin")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "PLUGIN.yaml"), []byte("schema_version: 1\nname: imported-runtime-plugin\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := executePluginPlan(t, repo, agentsHome); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	assertSymlinkTarget(t, target, pluginDir)
}

func TestExecutePluginBundleIntentRejectsAllowlistedDirectoryWithoutImportedMarker(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	agentsHome := filepath.Join(tmp, ".agents")

	pluginDir := filepath.Join(agentsHome, "plugins", "proj", "runtime-plugin")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "PLUGIN.yaml"), []byte("schema_version: 1\nname: runtime-plugin\n"), 0644); err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(repo, ".opencode", "plugins", "runtime-plugin")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "notes.txt"), []byte("imported"), 0644); err != nil {
		t.Fatal(err)
	}

	err := executePluginPlan(t, repo, agentsHome)
	if err == nil {
		t.Fatal("expected error when allowlisted plugin dir lacks imported marker files")
	}
	if !strings.Contains(err.Error(), "without imported markers") {
		t.Fatalf("error = %q, want marker refusal", err)
	}
}

func TestRemoveSharedTargetPlanRemovesSkillSymlink(t *testing.T) {
	repo, agentsHome := setupRepoAgentsHome(t)
	writeFixtureImportedSkillPair(t, repo, agentsHome, "proj", "review")
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
	repo, agentsHome := setupRepoAgentsHome(t)
	writeFixtureCodexAgent(t, agentsHome)
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

// A failed rendered-file removal must NOT be swallowed: da remove relies on
// this error to avoid reporting a clean unlink while a managed output is
// still live on disk. os.Remove of a non-empty directory fails with a
// non-IsNotExist error, exercising the propagation branch.
func TestRemoveManagedIntentTarget_RenderedRemoveErrorPropagates(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "out.toml")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "child"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	intent := ResourceIntent{
		Shape:        ResourceShapeRenderSingle,
		Transport:    ResourceTransportWrite,
		Materializer: codexAgentTomlMaterializer,
		TargetPath:   "out.toml",
	}
	err := removeManagedIntentTarget(intent, tmp, t.TempDir())
	if err == nil {
		t.Fatal("non-IsNotExist remove failure must propagate, got nil")
	}
	if !strings.Contains(err.Error(), "remove rendered file") {
		t.Fatalf("error must identify the failing op, got %v", err)
	}
}

// A missing rendered target is a successful no-op (IsNotExist swallowed).
func TestRemoveManagedIntentTarget_MissingRenderedTargetIsNoop(t *testing.T) {
	intent := ResourceIntent{
		Shape:        ResourceShapeRenderSingle,
		Transport:    ResourceTransportWrite,
		Materializer: codexAgentTomlMaterializer,
		TargetPath:   "does-not-exist.toml",
	}
	if err := removeManagedIntentTarget(intent, t.TempDir(), t.TempDir()); err != nil {
		t.Fatalf("missing target must be a no-op, got %v", err)
	}
}

// RemoveSharedTargets must aggregate per-resource failures (errors.Join)
// rather than short-circuiting, so one stuck target cannot mask the rest.
func TestRemoveSharedTargets_AggregatesFailures(t *testing.T) {
	tmp := t.TempDir()
	mkBlockedToml := func(name string) string {
		d := filepath.Join(tmp, name)
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "child"), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		return name
	}
	plan := ResourcePlan{Resources: []plannedResource{
		{Intent: ResourceIntent{
			IntentID:     "first",
			Shape:        ResourceShapeRenderSingle,
			Transport:    ResourceTransportWrite,
			Materializer: codexAgentTomlMaterializer,
			TargetPath:   mkBlockedToml("a.toml"),
		}},
		{Intent: ResourceIntent{
			IntentID:     "second",
			Shape:        ResourceShapeRenderSingle,
			Transport:    ResourceTransportWrite,
			Materializer: codexAgentTomlMaterializer,
			TargetPath:   mkBlockedToml("b.toml"),
		}},
	}}
	err := plan.RemoveSharedTargets(tmp, t.TempDir())
	if err == nil {
		t.Fatal("aggregated removal failures must surface, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "first") || !strings.Contains(msg, "second") {
		t.Fatalf("both failures must be reported (errors.Join), got %v", err)
	}
}

// A DirectFile shared target materialized as a hard link to the canonical
// source (the Windows file-link model) must be removed. The symlink/junction
// removal is a no-op for a hard link, so without the hard-link branch da
// remove would report success while .github/agents/*.agent.md stays live.
func TestRemoveManagedIntentTarget_DirectFileHardLinkRemoved(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	agentsHome := filepath.Join(tmp, ".agents")

	srcDir := filepath.Join(agentsHome, "agents", "proj", "x")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(srcDir, "AGENT.md")
	if err := os.WriteFile(src, []byte("# X\n"), 0644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(repo, ".github", "agents", "x.agent.md")
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		t.Fatal(err)
	}
	// Materialize the file-link model: a hard link, not a symlink.
	if err := os.Link(src, target); err != nil {
		t.Fatalf("hard link: %v", err)
	}

	intent := ResourceIntent{
		IntentID:   "agents.file.proj.x",
		TargetPath: filepath.Join(".github", "agents", "x.agent.md"),
		SourceRef: ResourceSourceRef{
			Scope:        "proj",
			Bucket:       "agents",
			RelativePath: filepath.Join("x", "AGENT.md"),
			Kind:         ResourceSourceCanonicalFile,
		},
		Shape:     ResourceShapeDirectFile,
		Transport: ResourceTransportSymlink,
	}
	if err := removeManagedIntentTarget(intent, repo, agentsHome); err != nil {
		t.Fatalf("hard-linked DirectFile target must be removed cleanly, got %v", err)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("managed hard link must be gone, lstat err=%v", err)
	}
}

// A removal failure on the hard-link path must surface, not be swallowed
// (otherwise da remove reports success while the managed file is still live).
func TestRemoveManagedIntentTarget_DirectFileHardLinkRemovalFailureSurfaces(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	agentsHome := filepath.Join(tmp, ".agents")

	srcDir := filepath.Join(agentsHome, "agents", "proj", "x")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(srcDir, "AGENT.md")
	if err := os.WriteFile(src, []byte("# X\n"), 0644); err != nil {
		t.Fatal(err)
	}
	targetDir := filepath.Join(repo, ".github", "agents")
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(targetDir, "x.agent.md")
	if err := os.Link(src, target); err != nil {
		t.Fatalf("hard link: %v", err)
	}
	// Deny child writes/deletes on the parent dir so the hard-link removal
	// fails. MakeDirWriteDenied installs POSIX chmod 0o500 / Windows deny-ACE
	// on FILE_DELETE_CHILD, covering both platforms; it also handles the
	// root-bypass and elevated-Windows cases internally.
	testutil.MakeDirWriteDenied(t, targetDir)

	intent := ResourceIntent{
		IntentID:   "agents.file.proj.x",
		TargetPath: filepath.Join(".github", "agents", "x.agent.md"),
		SourceRef: ResourceSourceRef{
			Scope:        "proj",
			Bucket:       "agents",
			RelativePath: filepath.Join("x", "AGENT.md"),
			Kind:         ResourceSourceCanonicalFile,
		},
		Shape:     ResourceShapeDirectFile,
		Transport: ResourceTransportSymlink,
	}
	err := removeManagedIntentTarget(intent, repo, agentsHome)
	if err == nil {
		t.Fatal("hard-link removal failure must surface, got nil")
	}
	if !strings.Contains(err.Error(), "remove managed hard link") {
		t.Fatalf("error must identify the failing op, got %v", err)
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

// TestEnsureFileSymlinkIntentPreservesUserFileAtAllowlistedTarget is the
// regression for task item 2: a user-authored regular file at an ALLOWLISTED
// DirectFile target (.opencode/agent/*.md) must NOT be silently deleted by
// prepareIntentTargetForReplacement. The file must survive (links.Symlink
// refuses an unmanaged file → execute errors), proving no silent data loss.
func TestEnsureFileSymlinkIntentPreservesUserFileAtAllowlistedTarget(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	agentsHome := filepath.Join(tmp, ".agents")

	agentDir := filepath.Join(agentsHome, "agents", "proj", "x")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "AGENT.md"), []byte("# X\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// User-authored file at an allowlisted DirectFile target.
	userFile := filepath.Join(repo, ".opencode", "agent", "x.md")
	if err := os.MkdirAll(filepath.Dir(userFile), 0755); err != nil {
		t.Fatal(err)
	}
	const userContent = "hand-written by the user"
	if err := os.WriteFile(userFile, []byte(userContent), 0644); err != nil {
		t.Fatal(err)
	}

	intent := ResourceIntent{
		IntentID:    "agents.file.proj.x.opencode",
		Project:     "proj",
		Bucket:      "agents",
		LogicalName: "x",
		TargetPath:  ".opencode/agent/x.md",
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
		t.Fatal("expected refusal: a user file at an allowlisted DirectFile target must not be replaced")
	}
	got, err := os.ReadFile(userFile)
	if err != nil {
		t.Fatalf("user file must be preserved, got: %v", err)
	}
	if string(got) != userContent {
		t.Errorf("user file content mutated: got %q want %q", got, userContent)
	}
}

func TestExecuteDirSymlinkIntentRejectsNonAllowlistedImportedDirectory(t *testing.T) {
	repo, agentsHome := setupRepoAgentsHome(t)
	writeFixtureSkill(t, agentsHome, "proj", "review")

	// Directory blocks symlink creation; path is not under shared-mirror allowlist prefixes.
	blocked := filepath.Join(repo, "vendor", "skills", "review")
	if err := os.MkdirAll(blocked, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blocked, "SKILL.md"), []byte("imported"), 0644); err != nil {
		t.Fatal(err)
	}

	intent := ResourceIntent{
		IntentID:    "skills.proj.review.non-allowlisted",
		Project:     "proj",
		Bucket:      "skills",
		LogicalName: "review",
		TargetPath:  filepath.Join("vendor", "skills", "review"),
		Ownership:   ResourceOwnershipSharedRepo,
		SourceRef: ResourceSourceRef{
			Scope:        "proj",
			Bucket:       "skills",
			RelativePath: "review",
			Kind:         ResourceSourceCanonicalDir,
		},
		Shape:         ResourceShapeDirectDir,
		Transport:     ResourceTransportSymlink,
		Materializer:  "shared-skill-dir-symlink",
		ReplacePolicy: ResourceReplaceAllowlistedImportedDirOnly,
		PrunePolicy:   ResourcePruneTarget,
		MarkerFiles:   []string{"SKILL.md"},
	}
	plan, err := BuildResourcePlan([]ResourceIntent{intent})
	if err != nil {
		t.Fatalf("BuildResourcePlan: %v", err)
	}
	err = plan.Execute(repo, agentsHome)
	if err == nil {
		t.Fatal("expected error for non-allowlisted directory replacement")
	}
	if !strings.Contains(err.Error(), "not allowlisted for imported directory replacement") {
		t.Fatalf("error = %q, want allowlisted refusal", err)
	}
}

func TestExecuteDirSymlinkIntentRejectsAllowlistedDirectoryWithoutImportedMarkers(t *testing.T) {
	repo, agentsHome := setupRepoAgentsHome(t)
	writeFixtureSkill(t, agentsHome, "proj", "review")

	target := filepath.Join(repo, ".agents", "skills", "review")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	// Non-marker content only — executor must refuse (no imported SKILL.md to bless removal).
	if err := os.WriteFile(filepath.Join(target, "notes.txt"), []byte("user"), 0644); err != nil {
		t.Fatal(err)
	}

	err := executeSkillPlan(t, repo, agentsHome)
	if err == nil {
		t.Fatal("expected error when allowlisted dir lacks imported marker files")
	}
	if !strings.Contains(err.Error(), "without imported markers") {
		t.Fatalf("error = %q, want marker refusal", err)
	}
}

func TestExecuteDirSymlinkIntentReplacesAllowlistedDirectoryWhenImportedMarkerPresent(t *testing.T) {
	repo, agentsHome := setupRepoAgentsHome(t)
	writeFixtureSkill(t, agentsHome, "proj", "review")

	target := filepath.Join(repo, ".agents", "skills", "review")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "SKILL.md"), []byte("imported-body"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := executeSkillPlan(t, repo, agentsHome); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !links.IsManagedLink(target, filepath.Join(agentsHome, "skills", "proj", "review")) {
		t.Fatalf("expected managed link at %s after imported-dir replacement", target)
	}
}

func TestCollectAndExecuteSharedTargetPlanDedupesCrossPlatform(t *testing.T) {
	repo, agentsHome := setupRepoAgentsHome(t)
	writeFixtureSkill(t, agentsHome, "proj", "review")
	if err := os.MkdirAll(filepath.Join(repo, ".agents", "skills"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	platforms := []Platform{NewCodex(), NewOpenCode(), NewCopilot()}
	if err := CollectAndExecuteSharedTargetPlan("proj", repo, platforms); err != nil {
		t.Fatalf("CollectAndExecuteSharedTargetPlan: %v", err)
	}

	// All three platforms target .agents/skills/review; it should be a single managed link
	target := filepath.Join(repo, ".agents", "skills", "review")
	if !links.IsManagedLink(target, filepath.Join(agentsHome, "skills", "proj", "review")) {
		t.Fatalf("expected managed link at %s", target)
	}
}

// ── Shared fixture helpers (package-private) ────────────────────────────────
//
// These collapse the recurring tmp/repo/agentsHome boilerplate and the
// canonical bucket-fixture writes that the resource_plan tests use.
// Helper bodies delegate to testutil.WriteScopeFilePath for the on-disk
// write; the wrappers exist to (a) preserve the project-specific frontmatter
// bodies these tests assert on (e.g. "Ship it." in the codex toml test) and
// (b) avoid the testutil → platform import cycle that lifting
// validShared{Skill,Agent,Plugin}Intent into testutil would create.

// setupRepoAgentsHome returns (repo, agentsHome) under t.TempDir().
// Replaces the 3-line tmp/repo/agentsHome boilerplate at the start of
// most resource_plan tests.
func setupRepoAgentsHome(t *testing.T) (repo, agentsHome string) {
	t.Helper()
	tmp := t.TempDir()
	return filepath.Join(tmp, "repo"), filepath.Join(tmp, ".agents")
}

// writeFixtureSkill creates ~/.agents/skills/<project>/<name>/SKILL.md
// with frontmatter `name: <name>`. Returns the directory path.
func writeFixtureSkill(t *testing.T, agentsHome, project, name string) string {
	t.Helper()
	testutil.WriteScopeFilePath(t, agentsHome, "skills", project,
		filepath.Join(name, "SKILL.md"), []byte("---\nname: "+name+"\n---\n"))
	return filepath.Join(agentsHome, "skills", project, name)
}

// writeFixtureImportedSkillPair creates BOTH the repo-imported skill at
// repo/.agents/skills/<name>/SKILL.md AND the canonical agentsHome skill
// at <agentsHome>/skills/<project>/<name>/SKILL.md (with a "canonical-"
// prefix in the canonical name field so tests can distinguish). Returns
// (importedSkillPath, canonicalSkillDir).
//
// The repo-side write uses repo as the agentsHome-equivalent root and
// ".agents" as the bucket, mirroring the on-disk layout the executor
// inspects under .agents/skills/<name>/.
func writeFixtureImportedSkillPair(t *testing.T, repo, agentsHome, project, name string) (importedSkill, canonicalSkillDir string) {
	t.Helper()
	testutil.WriteScopeFilePath(t, repo, ".agents", "skills",
		filepath.Join(name, "SKILL.md"), []byte("---\nname: "+name+"\n---\n"))
	testutil.WriteScopeFilePath(t, agentsHome, "skills", project,
		filepath.Join(name, "SKILL.md"), []byte("---\nname: canonical-"+name+"\n---\n"))
	importedSkill = filepath.Join(repo, ".agents", "skills", name, "SKILL.md")
	canonicalSkillDir = filepath.Join(agentsHome, "skills", project, name)
	return importedSkill, canonicalSkillDir
}

// writeFixtureAgent creates ~/.agents/agents/<project>/<name>/AGENT.md
// with the supplied body. Returns the directory path.
func writeFixtureAgent(t *testing.T, agentsHome, project, name, body string) string {
	t.Helper()
	testutil.WriteScopeFilePath(t, agentsHome, "agents", project,
		filepath.Join(name, "AGENT.md"), []byte(body))
	return filepath.Join(agentsHome, "agents", project, name)
}

// writeFixtureCodexAgent creates the canonical "implementer" agent
// fixture used by both Codex toml emit + remove tests. Returns the
// directory path.
func writeFixtureCodexAgent(t *testing.T, agentsHome string) string {
	t.Helper()
	body := `---
name: implementer
description: does work
---

# Body
Ship it.
`
	return writeFixtureAgent(t, agentsHome, "proj", "implementer", body)
}

// writeFixturePlugin creates ~/.agents/plugins/<project>/<name>/PLUGIN.yaml.
// When withManifest is true, also writes manifest.json with `{"name":"<name>"}`.
// Returns the directory path.
func writeFixturePlugin(t *testing.T, agentsHome, project, name string, withManifest bool) string {
	t.Helper()
	testutil.WriteScopeFilePath(t, agentsHome, "plugins", project,
		filepath.Join(name, "PLUGIN.yaml"),
		[]byte("schema_version: 1\nname: "+name+"\n"))
	if withManifest {
		testutil.WriteScopeFilePath(t, agentsHome, "plugins", project,
			filepath.Join(name, "manifest.json"),
			[]byte(`{"name":"`+name+`"}`))
	}
	return filepath.Join(agentsHome, "plugins", project, name)
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

func validSharedPluginIntent(targetPath, emitter string) ResourceIntent {
	return ResourceIntent{
		IntentID:    "plugins.proj.runtime-plugin." + emitter,
		Project:     "proj",
		Bucket:      "plugins",
		LogicalName: "runtime-plugin",
		TargetPath:  targetPath,
		Ownership:   ResourceOwnershipSharedRepo,
		SourceRef: ResourceSourceRef{
			Scope:        "proj",
			Bucket:       "plugins",
			RelativePath: "runtime-plugin",
			Kind:         ResourceSourceCanonicalBundle,
			Origin:       "shared-plugin-bundle",
		},
		Shape:         ResourceShapeDirectDir,
		Transport:     ResourceTransportSymlink,
		Materializer:  "shared-plugin-dir-symlink",
		ReplacePolicy: ResourceReplaceAllowlistedImportedDirOnly,
		PrunePolicy:   ResourcePruneTarget,
		MarkerFiles:   []string{"PLUGIN.yaml"},
		Provenance: ResourceProvenance{
			Emitter: emitter,
		},
	}
}

// executePluginPlan builds and executes a shared plugin intent for the
// standard test path (.opencode/plugins/runtime-plugin / opencode).
func executePluginPlan(t *testing.T, repo, agentsHome string) error {
	t.Helper()
	intent := validSharedPluginIntent(".opencode/plugins/runtime-plugin", "opencode")
	plan, err := BuildResourcePlan([]ResourceIntent{intent})
	if err != nil {
		t.Fatalf("BuildResourcePlan: %v", err)
	}
	return plan.Execute(repo, agentsHome)
}

// executeSkillPlan builds and executes a shared skill intent for the
// standard test path (.agents/skills/review / test).
func executeSkillPlan(t *testing.T, repo, agentsHome string) error {
	t.Helper()
	intent := validSharedSkillIntent(".agents/skills/review", "test")
	plan, err := BuildResourcePlan([]ResourceIntent{intent})
	if err != nil {
		t.Fatalf("BuildResourcePlan: %v", err)
	}
	return plan.Execute(repo, agentsHome)
}

// ---------------------------------------------------------------------------
// ResourceIntent / ResourceSourceRef validate-enum coverage (relocated from
// coverage_gap2_test.go).
// ---------------------------------------------------------------------------

// TestResourceIntentValidateEnums_AllBadVariants drives each enum-mismatch
// branch in validateEnums.
func TestResourceIntentValidateEnums_AllBadVariants(t *testing.T) {
	good := validSharedSkillIntent(".agents/skills/review", "test")
	if err := good.Validate(); err != nil {
		t.Fatalf("baseline valid: %v", err)
	}
	cases := []struct {
		name string
		bad  func(ResourceIntent) ResourceIntent
		want string
	}{
		{"empty-ownership", func(r ResourceIntent) ResourceIntent { r.Ownership = ""; return r }, "ownership"},
		{"bad-ownership", func(r ResourceIntent) ResourceIntent { r.Ownership = "weird"; return r }, "ownership"},
		{"empty-shape", func(r ResourceIntent) ResourceIntent { r.Shape = ""; return r }, "shape"},
		{"bad-shape", func(r ResourceIntent) ResourceIntent { r.Shape = "weird"; return r }, "shape"},
		{"empty-transport", func(r ResourceIntent) ResourceIntent { r.Transport = ""; return r }, "transport"},
		{"bad-transport", func(r ResourceIntent) ResourceIntent { r.Transport = "weird"; return r }, "transport"},
		{"empty-replace", func(r ResourceIntent) ResourceIntent { r.ReplacePolicy = ""; return r }, "replace_policy"},
		{"bad-replace", func(r ResourceIntent) ResourceIntent { r.ReplacePolicy = "weird"; return r }, "replace_policy"},
		{"empty-prune", func(r ResourceIntent) ResourceIntent { r.PrunePolicy = ""; return r }, "prune_policy"},
		{"bad-prune", func(r ResourceIntent) ResourceIntent { r.PrunePolicy = "weird"; return r }, "prune_policy"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			intent := tc.bad(good)
			err := intent.Validate()
			if err == nil {
				t.Fatalf("expected error mentioning %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err %v missing %q", err, tc.want)
			}
		})
	}
}

// TestResourceSourceRefValidate_AllCases exhausts the kind switch.
func TestResourceSourceRefValidate_AllCases(t *testing.T) {
	base := ResourceSourceRef{
		Scope:        "proj",
		Bucket:       "skills",
		RelativePath: "x",
		Kind:         ResourceSourceCanonicalDir,
	}
	for _, kind := range []ResourceSourceKind{
		ResourceSourceCanonicalFile, ResourceSourceCanonicalDir, ResourceSourceCanonicalBundle,
	} {
		ref := base
		ref.Kind = kind
		if err := ref.Validate(); err != nil {
			t.Errorf("kind %q: %v", kind, err)
		}
	}
	bad := base
	bad.Kind = "weird"
	if err := bad.Validate(); err == nil {
		t.Error("expected error for unknown kind")
	}
	bad.Kind = ""
	if err := bad.Validate(); err == nil {
		t.Error("expected error for empty kind")
	}
	for _, missing := range []ResourceSourceRef{
		{Bucket: "x", RelativePath: "y", Kind: ResourceSourceCanonicalDir},
		{Scope: "x", RelativePath: "y", Kind: ResourceSourceCanonicalDir},
		{Scope: "x", Bucket: "y", Kind: ResourceSourceCanonicalDir},
	} {
		if err := missing.Validate(); err == nil {
			t.Errorf("expected error for missing field in %+v", missing)
		}
	}
}

// TestResourceIntentValidate_MissingMaterializer covers the empty-materializer branch.
func TestResourceIntentValidate_MissingMaterializer(t *testing.T) {
	intent := validSharedSkillIntent(".agents/skills/review", "test")
	intent.Materializer = ""
	if err := intent.Validate(); err == nil || !strings.Contains(err.Error(), "materializer") {
		t.Errorf("expected materializer error, got %v", err)
	}
}

// TestResourceIntentValidate_BadSourceRef propagates from SourceRef.Validate.
func TestResourceIntentValidate_BadSourceRef(t *testing.T) {
	intent := validSharedSkillIntent(".agents/skills/review", "test")
	intent.SourceRef.Kind = ""
	if err := intent.Validate(); err == nil {
		t.Error("expected propagated source_ref error")
	}
}

// TestValidateEnum_Direct exercises the helper with both success and failure paths.
func TestValidateEnum_Direct(t *testing.T) {
	if err := validateEnum("color", "red", []string{"red", "blue"}); err != nil {
		t.Errorf("valid: %v", err)
	}
	if err := validateEnum("color", "", []string{"red"}); err == nil {
		t.Error("expected required error for empty value")
	}
	if err := validateEnum("color", "green", []string{"red", "blue"}); err == nil {
		t.Error("expected unsupported error for unknown value")
	}
}

// TestSameStrings_Differences ensures the helper is symmetric on shuffles and
// rejects mismatched slices.
func TestSameStrings_Differences(t *testing.T) {
	cases := []struct {
		a, b []string
		want bool
	}{
		{nil, nil, true},
		{[]string{"a", "b"}, []string{"b", "a"}, true},
		{[]string{"a"}, []string{}, false},
		{[]string{"a", "b"}, []string{"a", "c"}, false},
	}
	for i, tc := range cases {
		if got := sameStrings(tc.a, tc.b); got != tc.want {
			t.Errorf("[%d] sameStrings(%v, %v) = %v, want %v", i, tc.a, tc.b, got, tc.want)
		}
	}
}

// TestSyncResourceDirEntries_HardError drives the mkdir error branch.
func TestSyncResourceDirEntries_MkdirError(t *testing.T) {
	// Try to use a path that contains a regular file as parent dir.
	tmp := t.TempDir()
	blocker := filepath.Join(tmp, "file")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	// dstRoot below a regular file → MkdirAll errors.
	dst := filepath.Join(blocker, "child")
	err := syncResourceDirEntries(stdPlatformIO{}, []resourceDir{{Name: "x", Dir: "/no/where"}}, dst)
	if err == nil {
		t.Error("expected mkdir error")
	}
}

// ---------------------------------------------------------------------------
// Resource plan + intent execute coverage (relocated from coverage_gap3).
// ---------------------------------------------------------------------------

// TestRemoveSharedTargetPlanEmpty drives the no-platform branch.
func TestRemoveSharedTargetPlan_NoPlatforms(t *testing.T) {
	if err := RemoveSharedTargetPlan("proj", t.TempDir(), nil); err != nil {
		t.Errorf("RemoveSharedTargetPlan with no platforms: %v", err)
	}
}

// TestRemoveManagedIntentTarget_UnknownShapeNoop drives the default (no-op)
// branch for unknown shape/transport combos.
func TestRemoveManagedIntentTarget_UnknownShape(t *testing.T) {
	intent := ResourceIntent{Shape: "weird", Transport: "weird"}
	if err := removeManagedIntentTarget(intent, t.TempDir(), t.TempDir()); err != nil {
		t.Errorf("unknown shape should no-op, got %v", err)
	}
}

// TestSyncScopedFileSymlinks_ExistingTargetMaintained drives the link.Symlink
// idempotency branch.
func TestSyncScopedFileSymlinks_Idempotent(t *testing.T) {
	tmp := t.TempDir()
	agentDir := filepath.Join(tmp, "agents", "global", "x")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "AGENT.md"), []byte("y"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(tmp, "dst")
	if err := syncScopedFileSymlinks(stdPlatformIO{}, tmp, "agents", "global", "AGENT.md", dst, ".md"); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if err := syncScopedFileSymlinks(stdPlatformIO{}, tmp, "agents", "global", "AGENT.md", dst, ".md"); err != nil {
		t.Fatalf("second sync: %v", err)
	}
}

// TestEnsureFileSymlinkIntent_TargetIsRegularFileBlocked drives the
// !info.IsDir + ReplaceNever rejection.
func TestEnsureFileSymlinkIntent_RegularFileNever(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	src := filepath.Join(agentsHome, "skills", "proj", "x", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(src), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("body"), 0644); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".agents/skills"), 0755); err != nil {
		t.Fatal(err)
	}
	// Pre-place a regular file at the target.
	target := filepath.Join(repo, ".agents/skills/x")
	if err := os.WriteFile(target, []byte("blocking"), 0644); err != nil {
		t.Fatal(err)
	}

	intent := validSharedSkillIntent(".agents/skills/x", "test")
	intent.ReplacePolicy = ResourceReplaceNever
	plan, err := BuildResourcePlan([]ResourceIntent{intent})
	if err != nil {
		t.Fatalf("BuildResourcePlan: %v", err)
	}
	if err := plan.Execute(repo, agentsHome); err == nil {
		t.Error("expected error when target is regular file with Never policy")
	}
}

// TestEnsureFileSymlinkIntent_TargetDirIfManagedRefused covers the dir+IfManaged refusal branch.
func TestEnsureFileSymlinkIntent_DirIfManagedRefused(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	src := filepath.Join(agentsHome, "skills", "proj", "x", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(src), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("body"), 0644); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(tmp, "repo")
	target := filepath.Join(repo, ".agents/skills/x")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	intent := validSharedSkillIntent(".agents/skills/x", "test")
	intent.ReplacePolicy = ResourceReplaceIfManaged
	plan, err := BuildResourcePlan([]ResourceIntent{intent})
	if err != nil {
		t.Fatalf("BuildResourcePlan: %v", err)
	}
	if err := plan.Execute(repo, agentsHome); err == nil {
		t.Error("expected refusal for IfManaged on directory")
	}
}

// TestEnsureFileSymlinkIntent_DirNeverRefused covers the dir+Never branch.
func TestEnsureFileSymlinkIntent_DirNeverRefused(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	src := filepath.Join(agentsHome, "skills", "proj", "x", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(src), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("body"), 0644); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(tmp, "repo")
	target := filepath.Join(repo, ".agents/skills/x")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	intent := validSharedSkillIntent(".agents/skills/x", "test")
	intent.ReplacePolicy = ResourceReplaceNever
	plan, err := BuildResourcePlan([]ResourceIntent{intent})
	if err != nil {
		t.Fatalf("BuildResourcePlan: %v", err)
	}
	if err := plan.Execute(repo, agentsHome); err == nil {
		t.Error("expected refusal for Never on directory")
	}
}

// TestPrepareIntentTargetForReplacement_UnknownReplaceForDir drives the
// "unsupported replace policy" default case.
func TestPrepareIntentTargetForReplacement_UnknownReplacePolicyForDir(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "d")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	intent := ResourceIntent{
		TargetPath:    ".agents/skills/x",
		ReplacePolicy: "weird-policy",
	}
	if err := prepareIntentTargetForReplacement(target, intent); err == nil {
		t.Error("expected error for unknown replace policy")
	}
}

// ---------------------------------------------------------------------------
// Resource intent prepare-replace coverage (relocated from coverage_gap5).
// ---------------------------------------------------------------------------

// TestPrepareIntentTargetForReplacement_AllowlistedFilePreserved drives the
// AllowlistedImportedDirOnly + regular-file branch when target IS allowlisted.
// The ownership contract authorizes this policy to replace only a proven
// imported/managed DIRECTORY; a regular file must be left in place (not
// pre-removed) so links.Symlink can apply the unmanaged-file contract instead
// of this code silently deleting user data.
func TestPrepareIntentTargetForReplacement_AllowlistedFilePreserved(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "blocking")
	if err := os.WriteFile(target, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	intent := ResourceIntent{
		TargetPath:    ".agents/skills/x",
		ReplacePolicy: ResourceReplaceAllowlistedImportedDirOnly,
	}
	if err := prepareIntentTargetForReplacement(target, intent); err != nil {
		t.Fatalf("allowlisted file prepare: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("expected regular file preserved (links.Symlink applies the contract), got: %v", err)
	}
}

// TestPrepareIntentTargetForReplacement_DefaultReplaceForFile drives the
// "default → os.Remove" branch (e.g. IfManaged on file).
func TestPrepareIntentTargetForReplacement_IfManagedFile(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "f")
	if err := os.WriteFile(target, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	intent := ResourceIntent{
		TargetPath:    ".agents/skills/x",
		ReplacePolicy: ResourceReplaceIfManaged,
	}
	if err := prepareIntentTargetForReplacement(target, intent); err != nil {
		t.Fatalf("IfManaged file replace: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Error("expected file removed")
	}
}

// TestSyncResourceDirEntries_NoEntries handles the empty-input branch.
func TestSyncResourceDirEntries_Empty(t *testing.T) {
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "out")
	if err := syncResourceDirEntries(stdPlatformIO{}, nil, dst); err != nil {
		t.Errorf("empty entries: %v", err)
	}
	if _, err := os.Stat(dst); err != nil {
		t.Error("expected dst dir created")
	}
}

// ---------------------------------------------------------------------------
// Resource intent helpers + allowlist + scoped-symlinks coverage (relocated
// from coverage_gap_test.go).
// ---------------------------------------------------------------------------

// TestResolveScopedFileFromBuckets covers the otherwise-dead-code multi-bucket
// resolver in resources.go.
func TestResolveScopedFileFromBuckets(t *testing.T) {
	tmp := t.TempDir()
	mkfile := func(parts ...string) string {
		p := filepath.Join(append([]string{tmp}, parts...)...)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	// Project scope wins over global.
	projFile := mkfile("settings", "proj", "thing.json")
	mkfile("settings", "global", "thing.json")

	got := resolveScopedFileFromBuckets(tmp, []string{"settings"}, "proj", "thing.json")
	if got != projFile {
		t.Errorf("got %q, want %q", got, projFile)
	}

	// Falls back to global when project is missing.
	globalOnly := mkfile("hooks", "global", "other.json")
	got = resolveScopedFileFromBuckets(tmp, []string{"hooks"}, "noproj", "other.json")
	if got != globalOnly {
		t.Errorf("got %q, want %q", got, globalOnly)
	}

	// Cross-bucket search.
	got = resolveScopedFileFromBuckets(tmp, []string{"missing-bucket", "hooks"}, "noproj", "other.json")
	if got != globalOnly {
		t.Errorf("cross-bucket got %q, want %q", got, globalOnly)
	}

	// No match → empty.
	if got := resolveScopedFileFromBuckets(tmp, []string{"hooks"}, "proj", "nope.json"); got != "" {
		t.Errorf("expected empty for no match, got %q", got)
	}
}

// TestExecuteSharedSkillMirrorPlan drives the helper that wraps
// BuildSharedSkillMirrorIntents + BuildResourcePlan + Execute.
func TestExecuteSharedSkillMirrorPlan(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	repo := filepath.Join(tmp, "repo")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	// Seed one project-scope skill.
	skillDir := filepath.Join(agentsHome, "skills", "proj", "my-skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: my-skill\n---\nbody\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := ExecuteSharedSkillMirrorPlan("proj", repo, ".agents/skills"); err != nil {
		t.Fatalf("ExecuteSharedSkillMirrorPlan: %v", err)
	}

	link := filepath.Join(repo, ".agents/skills", "my-skill")
	if !links.IsManagedLink(link, skillDir) {
		t.Errorf("expected managed link at %s -> %s", link, skillDir)
	}

	// Empty target roots → no-op.
	if err := ExecuteSharedSkillMirrorPlan("proj", repo); err != nil {
		t.Errorf("empty target roots should be a no-op, got %v", err)
	}
}

// TestEnsureFileSymlinkIntent_AlreadyCorrect drives the symlink-in-place
// branch.
func TestEnsureFileSymlinkIntent_ExistingSymlinkReplaced(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	src := filepath.Join(agentsHome, "skills", "proj", "review", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(src), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("body"), 0644); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".agents", "skills"), 0755); err != nil {
		t.Fatal(err)
	}

	intent := validSharedSkillIntent(".agents/skills/review", "test")
	plan, err := BuildResourcePlan([]ResourceIntent{intent})
	if err != nil {
		t.Fatalf("BuildResourcePlan: %v", err)
	}
	if err := plan.Execute(repo, agentsHome); err != nil {
		t.Fatalf("first execute: %v", err)
	}
	// Run again; should be idempotent (already-symlink branch).
	if err := plan.Execute(repo, agentsHome); err != nil {
		t.Fatalf("second execute: %v", err)
	}
}

// TestPrepareIntentTargetForReplacement_RefusesUnmanagedFile drives the
// no-replace branch (ResourceReplaceNever) and asserts the ownership contract
// for the AllowlistedImportedDirOnly + regular-file case: prepare must NOT
// remove a regular file (allowlisted or not) — it defers to links.Symlink so
// user data is never silently deleted here.
func TestPrepareIntentTargetForReplacement_RefusesUnmanagedFile(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "f")
	if err := os.WriteFile(target, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	intent := ResourceIntent{
		TargetPath:    "anywhere/f",
		ReplacePolicy: ResourceReplaceNever,
	}
	if err := prepareIntentTargetForReplacement(target, intent); err == nil {
		t.Error("expected refusal for never-replace policy")
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("never-replace must preserve the file, got: %v", err)
	}

	// AllowlistedImportedDirOnly on a regular file: prepare returns nil
	// without removing the file, regardless of allowlist membership, so
	// links.Symlink applies the unmanaged-file contract.
	intent.ReplacePolicy = ResourceReplaceAllowlistedImportedDirOnly
	if err := prepareIntentTargetForReplacement(target, intent); err != nil {
		t.Errorf("non-allowlisted file prepare must defer (nil), got: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("non-allowlisted file must be preserved, got: %v", err)
	}

	intent.TargetPath = ".agents/skills/review"
	if err := prepareIntentTargetForReplacement(target, intent); err != nil {
		t.Errorf("allowlisted file prepare must defer (nil), got: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("allowlisted regular file must be preserved (links.Symlink applies the contract), got: %v", err)
	}
}

func TestPrepareIntentTargetForReplacement_DirPolicies(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "d")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	// IfManaged → refuse.
	if err := prepareIntentTargetForReplacement(dir, ResourceIntent{
		TargetPath:    ".agents/skills/x",
		ReplacePolicy: ResourceReplaceIfManaged,
	}); err == nil {
		t.Error("expected refusal IfManaged on dir")
	}
	// Never → refuse.
	if err := prepareIntentTargetForReplacement(dir, ResourceIntent{
		TargetPath:    ".agents/skills/x",
		ReplacePolicy: ResourceReplaceNever,
	}); err == nil {
		t.Error("expected refusal Never on dir")
	}
	// Allowlisted with marker → removed.
	marker := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(marker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := prepareIntentTargetForReplacement(dir, ResourceIntent{
		TargetPath:    ".agents/skills/x",
		ReplacePolicy: ResourceReplaceAllowlistedImportedDirOnly,
		MarkerFiles:   []string{"SKILL.md"},
	}); err != nil {
		t.Errorf("allowlisted dir replace: %v", err)
	}
}

// TestExecuteResourceIntent_UnsupportedShapeErrors drives the default branch
// in the switch.
func TestExecuteResourceIntent_UnsupportedShapeErrors(t *testing.T) {
	intent := ResourceIntent{
		Shape:     "weird",
		Transport: ResourceTransportSymlink,
	}
	if err := executeResourceIntent(intent, t.TempDir(), t.TempDir()); err == nil {
		t.Error("expected error for unsupported shape")
	}
}

func TestRemoveManagedIntentTargetUnknownMaterializerErrors(t *testing.T) {
	intent := ResourceIntent{
		Shape:        ResourceShapeRenderSingle,
		Transport:    ResourceTransportWrite,
		Materializer: "no-such-materializer",
		TargetPath:   "x",
	}
	if err := removeManagedIntentTarget(intent, t.TempDir(), t.TempDir()); err == nil {
		t.Error("expected error for unknown materializer in remove path")
	}
}

func TestCanonicalIntentSourcePath_EmptyErrors(t *testing.T) {
	if _, err := canonicalIntentSourcePath(ResourceIntent{}, ""); err == nil {
		t.Error("expected error for empty agentsHome")
	}
}

func TestResolveIntentTargetPath_Absolute(t *testing.T) {
	abs, err := filepath.Abs(filepath.Join("abs"))
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}
	got := resolveIntentTargetPath(abs, filepath.FromSlash("/repo"))
	if got != abs {
		t.Errorf("got %q, want %q", got, abs)
	}
}

// TestExecuteRenderSingleWrite_UnknownMaterializer covers the default branch.
func TestExecuteRenderSingleWrite_UnknownMaterializer(t *testing.T) {
	if err := executeRenderSingleWrite(ResourceIntent{
		Materializer: "unsupported",
	}, t.TempDir(), t.TempDir()); err == nil {
		t.Error("expected error for unknown materializer")
	}
}

// TestRemoveImportedDirIfAllowlisted_NoMarkers covers refusal branch.
func TestRemoveImportedDirIfAllowlisted_NoMarkers(t *testing.T) {
	tmp := t.TempDir()
	intent := ResourceIntent{TargetPath: ".agents/skills/x"}
	if err := removeImportedDirIfAllowlisted(tmp, intent); err == nil {
		t.Error("expected error for no-marker dir")
	}
}

func TestRemoveImportedDirIfAllowlisted_NotAllowlisted(t *testing.T) {
	intent := ResourceIntent{TargetPath: "other/path"}
	if err := removeImportedDirIfAllowlisted(t.TempDir(), intent); err == nil {
		t.Error("expected error for non-allowlisted target")
	}
}

// TestIsAllowlistedSharedMirrorTarget covers each branch of the allowlist.
func TestIsAllowlistedSharedMirrorTarget(t *testing.T) {
	for _, ok := range []string{
		".agents/skills/x", ".claude/skills/x", ".claude/agents/x",
		".codex/agents/x", ".opencode/plugins/x", ".opencode/agent/x",
		".github/agents/x",
	} {
		if !isAllowlistedSharedMirrorTarget(ok) {
			t.Errorf("expected %q allowlisted", ok)
		}
	}
	if isAllowlistedSharedMirrorTarget("random/path") {
		t.Error("random path should not be allowlisted")
	}
}

// TestSyncScopedFileSymlinks drives the opencode-style file fanout helper.
func TestSyncScopedFileSymlinks(t *testing.T) {
	tmp := t.TempDir()
	agentDir := filepath.Join(tmp, "agents", "global", "reviewer")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "AGENT.md"), []byte("body"), 0644); err != nil {
		t.Fatal(err)
	}
	dstRoot := filepath.Join(tmp, "out")
	if err := syncScopedFileSymlinks(stdPlatformIO{}, tmp, "agents", "global", "AGENT.md", dstRoot, ".md"); err != nil {
		t.Fatalf("syncScopedFileSymlinks: %v", err)
	}
	link := filepath.Join(dstRoot, "reviewer.md")
	if !links.IsManagedLink(link, filepath.Join(agentDir, "AGENT.md")) {
		t.Errorf("expected managed link at %s", link)
	}

	// Missing source → no-op (no error).
	if err := syncScopedFileSymlinks(stdPlatformIO{}, tmp, "no-such-bucket", "global", "AGENT.md", t.TempDir(), ".md"); err != nil {
		t.Errorf("missing bucket should be no-op, got %v", err)
	}
}

// makeAgentsBucketARegularFile writes a regular file at
// <agentsHome>/agents/<scope> so listScopedResourceDirs surfaces the
// non-ENOENT "exists but is not a listable directory" error that
// listCanonicalAgentEntries must wrap (rather than swallow as empty).
func makeAgentsBucketARegularFile(t *testing.T, agentsHome, scope string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(agentsHome, "agents"), 0o755); err != nil {
		t.Fatalf("mkdir agents: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentsHome, "agents", scope), []byte("not a dir"), 0o644); err != nil {
		t.Fatalf("write bucket file: %v", err)
	}
}

// testSharedAgentIntentBuilderCase runs one test case (has-entries, missing-bucket, or error)
// for a named builder function with optional error fragment text.
func testSharedAgentIntentBuilderCase(t *testing.T, name string, caseType string,
	fn func(agentsHome string) ([]ResourceIntent, error), errFragment string) {
	switch caseType {
	case "has-entries":
		_, agentsHome := setupRepoAgentsHome(t)
		writeFixtureAgent(t, agentsHome, "proj", "reviewer", "# Reviewer\n")
		t.Setenv("AGENTS_HOME", agentsHome)

		intents, err := fn(agentsHome)
		if err != nil {
			t.Fatalf("builder returned error: %v", err)
		}
		if len(intents) != 1 || intents[0].LogicalName != "reviewer" {
			t.Fatalf("expected one reviewer intent, got %+v", intents)
		}

	case "missing-bucket-is-empty":
		_, agentsHome := setupRepoAgentsHome(t)
		t.Setenv("AGENTS_HOME", agentsHome)

		intents, err := fn(agentsHome)
		if err != nil {
			t.Fatalf("missing bucket should be a nil-error no-op, got %v", err)
		}
		if len(intents) != 0 {
			t.Fatalf("missing bucket should yield no intents, got %+v", intents)
		}

	case "non-enoent-error-wrapped":
		_, agentsHome := setupRepoAgentsHome(t)
		makeAgentsBucketARegularFile(t, agentsHome, "proj")
		t.Setenv("AGENTS_HOME", agentsHome)

		_, err := fn(agentsHome)
		if err == nil {
			t.Fatal("expected wrapped error for non-listable bucket, got nil")
		}
		if !strings.Contains(err.Error(), "listing canonical agents for "+errFragment) {
			t.Fatalf("error %q missing caller context %q", err, errFragment)
		}
	}
}

// TestSharedAgentIntentBuilders_CanonicalEntryPreamble exercises the
// listCanonicalAgentEntries helper extracted from
// BuildSharedAgentFileSymlinkIntents and BuildSharedCodexAgentTomlIntents.
// Each builder must: (a) build intents when the bucket has entries,
// (b) return an empty set when the bucket is absent (ENOENT), and
// (c) propagate a wrapped error for a non-ENOENT failure with the
// caller-specific error context preserved byte-for-byte.
func TestSharedAgentIntentBuilders_CanonicalEntryPreamble(t *testing.T) {
	build := map[string]func(agentsHome string) ([]ResourceIntent, error){
		"file-symlink": func(string) ([]ResourceIntent, error) {
			return BuildSharedAgentFileSymlinkIntents("proj", ".opencode/agent", ".md")
		},
		"codex-toml": func(string) ([]ResourceIntent, error) {
			return BuildSharedCodexAgentTomlIntents("proj")
		},
	}
	// Per-builder fragment that must appear verbatim in the wrapped error.
	errFragment := map[string]string{
		"file-symlink": `project "proj" under .opencode/agent:`,
		"codex-toml":   `project "proj" (codex toml intents):`,
	}

	for name, fn := range build {
		t.Run(name+"/has-entries", func(t *testing.T) {
			testSharedAgentIntentBuilderCase(t, name, "has-entries", fn, "")
		})

		t.Run(name+"/missing-bucket-is-empty", func(t *testing.T) {
			testSharedAgentIntentBuilderCase(t, name, "missing-bucket-is-empty", fn, "")
		})

		t.Run(name+"/non-enoent-error-wrapped", func(t *testing.T) {
			testSharedAgentIntentBuilderCase(t, name, "non-enoent-error-wrapped", fn, errFragment[name])
		})
	}
}

// invalidSharedSkillIntent returns a skill intent that fails Validate
// (empty Materializer), used to drive the BuildResourcePlan validate-error
// branch that several plan entry points share.
func invalidSharedSkillIntent() ResourceIntent {
	intent := validSharedSkillIntent(".agents/skills/review", "claude")
	intent.Materializer = ""
	return intent
}

// TestSharedPlanEntryPointsPropagateBuildError covers the validate-error
// branch in BuildResourcePlan as reached through each high-level entry point.
func TestSharedPlanEntryPointsPropagateBuildError(t *testing.T) {
	repo, agentsHome := setupRepoAgentsHome(t)
	t.Setenv("AGENTS_HOME", agentsHome)
	platforms := []Platform{stubPlatform{id: "stub", intents: []ResourceIntent{invalidSharedSkillIntent()}}}

	t.Run("CollectAndExecute", func(t *testing.T) {
		if err := CollectAndExecuteSharedTargetPlan("proj", repo, platforms); err == nil {
			t.Fatal("expected build error, got nil")
		}
	})
	t.Run("RemoveSharedTargetPlan", func(t *testing.T) {
		if err := RemoveSharedTargetPlan("proj", repo, platforms); err == nil {
			t.Fatal("expected build error, got nil")
		}
	})
	t.Run("ExecuteSharedSkillMirrorPlan", func(t *testing.T) {
		// Seed a skill so intents exist; a real (non-".") target root drives
		// the build → plan → Execute body to completion.
		canonical := writeFixtureSkill(t, agentsHome, "proj", "review")
		if err := ExecuteSharedSkillMirrorPlan("proj", repo, ".agents/skills"); err != nil {
			t.Fatalf("ExecuteSharedSkillMirrorPlan: %v", err)
		}
		assertSymlinkTarget(t, filepath.Join(repo, ".agents", "skills", "review"), canonical)
	})
}

// --- config-v2-coherence §7A.5: EXACT/PRUNE outputs projection ---

// planWithSkillTarget builds a one-intent ResourcePlan whose single target is
// repo/<relDir>/review, a ResourcePruneTarget skill intent — the canonical
// "wanted" set the prune scan keeps. The parent dir (repo/<relDir>) is the only
// directory the prune will scan.
// swapOSReadDir replaces the osReadDir seam with fn and returns a restore func.
func swapOSReadDir(fn func(string) ([]os.DirEntry, error)) func() {
	prev := osReadDir
	osReadDir = fn
	return func() { osReadDir = prev }
}

// swapRemoveIfSymlinkUnder replaces the removeIfSymlinkUnder seam with fn and
// returns a restore func.
func swapRemoveIfSymlinkUnder(fn func(string, string) error) func() {
	prev := removeIfSymlinkUnder
	removeIfSymlinkUnder = fn
	return func() { removeIfSymlinkUnder = prev }
}

// swapExecuteResourcePlan replaces the executeResourcePlan seam with fn and
// returns a restore func.
func swapExecuteResourcePlan(fn func(ResourcePlan, string, string) error) func() {
	prev := executeResourcePlan
	executeResourcePlan = fn
	return func() { executeResourcePlan = prev }
}

func planWithSkillTarget(t *testing.T, relDir string) ResourcePlan {
	t.Helper()
	intent := validSharedSkillIntent(filepath.Join(relDir, "review"), "claude")
	plan, err := BuildResourcePlan([]ResourceIntent{intent})
	if err != nil {
		t.Fatalf("BuildResourcePlan: %v", err)
	}
	return plan
}

// seedManagedLink creates a managed symlink at linkPath pointing into agentsHome
// (so links.IsManagedLinkUnder reports it managed). The canonical target dir is
// created first so the link resolves.
func seedManagedLink(t *testing.T, agentsHome, name, linkPath string) {
	t.Helper()
	testutil.SymlinkOrSkip(t)
	canonical := filepath.Join(agentsHome, "skills", "proj", name)
	if err := os.MkdirAll(canonical, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(linkPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := links.Symlink(canonical, linkPath); err != nil {
		t.Fatalf("seed managed link: %v", err)
	}
	if !links.IsManagedLinkUnder(linkPath, agentsHome) {
		t.Fatalf("seeded link %s is not detected as managed under %s", linkPath, agentsHome)
	}
}

// PruneStaleSharedTargets must delete a managed output that is no longer in the
// resolved set while preserving the wanted target — the core exact/prune
// contract.
func TestPruneStaleSharedTargets_RemovesStaleKeepsWanted(t *testing.T) {
	repo, agentsHome := setupRepoAgentsHome(t)
	relDir := filepath.Join(".agents", "skills")
	dir := filepath.Join(repo, relDir)

	wanted := filepath.Join(dir, "review")
	stale := filepath.Join(dir, "obsolete")
	seedManagedLink(t, agentsHome, "review", wanted)
	seedManagedLink(t, agentsHome, "obsolete", stale)

	plan := planWithSkillTarget(t, relDir)
	pruned, err := plan.PruneStaleSharedTargets(repo, agentsHome)
	if err != nil {
		t.Fatalf("PruneStaleSharedTargets: %v", err)
	}
	if len(pruned) != 1 || pruned[0] != stale {
		t.Fatalf("pruned = %v, want [%s]", pruned, stale)
	}
	if _, err := os.Lstat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale managed link must be removed, lstat err = %v", err)
	}
	if _, err := os.Lstat(wanted); err != nil {
		t.Fatalf("wanted target must be preserved, lstat err = %v", err)
	}
}

// The prune must NEVER touch a user-authored file or a link that does not point
// into agentsHome — only managed outputs under agentsHome are eligible.
func TestPruneStaleSharedTargets_PreservesUserContent(t *testing.T) {
	repo, agentsHome := setupRepoAgentsHome(t)
	relDir := filepath.Join(".agents", "skills")
	dir := filepath.Join(repo, relDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	// A plain user file living next to managed outputs.
	userFile := filepath.Join(dir, "user-notes.md")
	if err := os.WriteFile(userFile, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A symlink the user owns, pointing OUTSIDE agentsHome — not managed.
	outside := filepath.Join(repo, "elsewhere")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	userLink := filepath.Join(dir, "user-link")
	testutil.SymlinkOrSkip(t)
	if err := links.Symlink(outside, userLink); err != nil {
		t.Fatalf("user link: %v", err)
	}

	plan := planWithSkillTarget(t, relDir)
	pruned, err := plan.PruneStaleSharedTargets(repo, agentsHome)
	if err != nil {
		t.Fatalf("PruneStaleSharedTargets: %v", err)
	}
	if len(pruned) != 0 {
		t.Fatalf("pruned = %v, want none (user content must be preserved)", pruned)
	}
	if _, err := os.Lstat(userFile); err != nil {
		t.Fatalf("user file must survive, lstat err = %v", err)
	}
	if _, err := os.Lstat(userLink); err != nil {
		t.Fatalf("user-owned non-managed link must survive, lstat err = %v", err)
	}
}

// An intent with PrunePolicy=none must NOT make its parent dir a prune scope —
// the exact projection only sibling-prunes directories da actively projects
// ResourcePruneTarget intents into.
func TestPruneStaleSharedTargets_PruneNoneIntentDoesNotScanDir(t *testing.T) {
	repo, agentsHome := setupRepoAgentsHome(t)
	relDir := filepath.Join(".codex", "agents")
	dir := filepath.Join(repo, relDir)

	stale := filepath.Join(dir, "obsolete")
	seedManagedLink(t, agentsHome, "obsolete", stale)

	// A render intent with PrunePolicy=none in that same dir.
	intent := ResourceIntent{
		IntentID:      "agents.codex-toml.proj.keep",
		Project:       "proj",
		Bucket:        "agents",
		LogicalName:   "keep",
		TargetPath:    filepath.Join(relDir, "keep.toml"),
		Ownership:     ResourceOwnershipSharedRepo,
		SourceRef:     ResourceSourceRef{Scope: "proj", Bucket: "agents", RelativePath: "keep/AGENT.md", Kind: ResourceSourceCanonicalFile, Origin: "shared-codex-agent-toml"},
		Shape:         ResourceShapeRenderSingle,
		Transport:     ResourceTransportWrite,
		Materializer:  codexAgentTomlMaterializer,
		ReplacePolicy: ResourceReplaceIfManaged,
		PrunePolicy:   ResourcePruneNone,
	}
	plan, err := BuildResourcePlan([]ResourceIntent{intent})
	if err != nil {
		t.Fatalf("BuildResourcePlan: %v", err)
	}

	pruned, err := plan.PruneStaleSharedTargets(repo, agentsHome)
	if err != nil {
		t.Fatalf("PruneStaleSharedTargets: %v", err)
	}
	if len(pruned) != 0 {
		t.Fatalf("pruned = %v, want none (prune_none dir must not be scanned)", pruned)
	}
	if _, err := os.Lstat(stale); err != nil {
		t.Fatalf("link in a prune_none dir must survive, lstat err = %v", err)
	}
}

// A non-listable managed dir must surface as an aggregated error rather than a
// silent clean-prune. The ReadDir failure is injected through the osReadDir seam
// so the branch is exercised deterministically on every OS (a file-where-a-dir
// is-expected trick does not fail ReadDir uniformly on Windows).
func TestPruneStaleSharedTargets_ReadDirErrorPropagates(t *testing.T) {
	repo, agentsHome := setupRepoAgentsHome(t)
	relDir := filepath.Join(".agents", "skills")

	sentinel := errors.New("injected readdir failure")
	defer swapOSReadDir(func(string) ([]os.DirEntry, error) { return nil, sentinel })()

	plan := planWithSkillTarget(t, relDir)
	pruned, err := plan.PruneStaleSharedTargets(repo, agentsHome)
	if err == nil {
		t.Fatal("expected a ReadDir error to propagate, got nil")
	}
	if len(pruned) != 0 {
		t.Fatalf("pruned = %v, want none on error", pruned)
	}
	if !strings.Contains(err.Error(), "listing managed dir") {
		t.Fatalf("error = %q, want listing-managed-dir context", err)
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want wrapped injected failure", err)
	}
}

// RunSharedTargetProjectionExact apply path: write the wanted set AND prune a
// pre-existing stale managed output in one call.
func TestRunSharedTargetProjectionExact_ApplyWritesAndPrunes(t *testing.T) {
	repo, agentsHome := setupRepoAgentsHome(t)
	t.Setenv("AGENTS_HOME", agentsHome)
	testutil.SymlinkOrSkip(t)

	// Canonical wanted skill + its imported pair so Claude emits the intent.
	writeFixtureImportedSkillPair(t, repo, agentsHome, "proj", "review")

	// A pre-existing stale managed link in the same target dir.
	stale := filepath.Join(repo, ".agents", "skills", "gone")
	seedManagedLink(t, agentsHome, "gone", stale)

	platforms := []Platform{NewClaude()}
	lines, err := RunSharedTargetProjectionExact("proj", repo, platforms, false, true)
	if err != nil {
		t.Fatalf("RunSharedTargetProjectionExact apply: %v", err)
	}
	if lines != nil {
		t.Fatalf("apply lines = %v, want nil", lines)
	}
	wanted := filepath.Join(repo, ".agents", "skills", "review")
	if !links.IsManagedLink(wanted, filepath.Join(agentsHome, "skills", "proj", "review")) {
		t.Fatalf("wanted target %s not projected as managed link", wanted)
	}
	if _, err := os.Lstat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale managed link must be pruned, lstat err = %v", err)
	}
}

// exact=false (`--inexact`) must keep the additive RunSharedTargetProjection
// behavior: the stale managed output is left in place.
func TestRunSharedTargetProjectionExact_InexactKeepsStale(t *testing.T) {
	repo, agentsHome := setupRepoAgentsHome(t)
	t.Setenv("AGENTS_HOME", agentsHome)
	testutil.SymlinkOrSkip(t)

	writeFixtureImportedSkillPair(t, repo, agentsHome, "proj", "review")
	stale := filepath.Join(repo, ".agents", "skills", "gone")
	seedManagedLink(t, agentsHome, "gone", stale)

	platforms := []Platform{NewClaude()}
	if _, err := RunSharedTargetProjectionExact("proj", repo, platforms, false, false); err != nil {
		t.Fatalf("RunSharedTargetProjectionExact inexact: %v", err)
	}
	if _, err := os.Lstat(stale); err != nil {
		t.Fatalf("inexact must keep stale managed output, lstat err = %v", err)
	}
}

// Dry-run exact preview lists the additive write lines AND a "prune managed"
// line for every stale managed output, without mutating the filesystem.
func TestRunSharedTargetProjectionExact_DryRunPreviewsPrune(t *testing.T) {
	repo, agentsHome := setupRepoAgentsHome(t)
	t.Setenv("AGENTS_HOME", agentsHome)
	testutil.SymlinkOrSkip(t)

	writeFixtureImportedSkillPair(t, repo, agentsHome, "proj", "review")
	stale := filepath.Join(repo, ".agents", "skills", "gone")
	seedManagedLink(t, agentsHome, "gone", stale)

	platforms := []Platform{NewClaude()}
	lines, err := RunSharedTargetProjectionExact("proj", repo, platforms, true, true)
	if err != nil {
		t.Fatalf("RunSharedTargetProjectionExact dry-run: %v", err)
	}
	var sawWrite, sawPrune bool
	for _, l := range lines {
		if strings.Contains(l, "symlink") && strings.Contains(l, "review") {
			sawWrite = true
		}
		if strings.Contains(l, "prune managed") && strings.Contains(l, "gone") {
			sawPrune = true
		}
	}
	if !sawWrite {
		t.Fatalf("dry-run lines missing the write preview: %v", lines)
	}
	if !sawPrune {
		t.Fatalf("dry-run lines missing the prune preview: %v", lines)
	}
	// Dry-run must not mutate: the stale link is still present.
	if _, err := os.Lstat(stale); err != nil {
		t.Fatalf("dry-run must not prune, lstat err = %v", err)
	}
}

// exact=false delegates to RunSharedTargetProjection's dry-run, so the preview
// carries NO prune line even when a stale managed output exists.
func TestRunSharedTargetProjectionExact_InexactDryRunNoPruneLine(t *testing.T) {
	repo, agentsHome := setupRepoAgentsHome(t)
	t.Setenv("AGENTS_HOME", agentsHome)
	testutil.SymlinkOrSkip(t)

	writeFixtureImportedSkillPair(t, repo, agentsHome, "proj", "review")
	stale := filepath.Join(repo, ".agents", "skills", "gone")
	seedManagedLink(t, agentsHome, "gone", stale)

	platforms := []Platform{NewClaude()}
	lines, err := RunSharedTargetProjectionExact("proj", repo, platforms, true, false)
	if err != nil {
		t.Fatalf("RunSharedTargetProjectionExact inexact dry-run: %v", err)
	}
	for _, l := range lines {
		if strings.Contains(l, "prune managed") {
			t.Fatalf("inexact dry-run must not preview prune, got %q", l)
		}
	}
}

// An empty resolved set in dry-run exact mode reports the "(none)" sentinel,
// matching RunSharedTargetProjection's empty-plan behavior.
func TestRunSharedTargetProjectionExact_DryRunEmptyPlanSentinel(t *testing.T) {
	repo, _ := setupRepoAgentsHome(t)
	lines, err := RunSharedTargetProjectionExact("proj", repo, []Platform{}, true, true)
	if err != nil {
		t.Fatalf("RunSharedTargetProjectionExact empty dry-run: %v", err)
	}
	if len(lines) != 1 || lines[0] != "shared targets: (none)" {
		t.Fatalf("empty dry-run lines = %v, want [shared targets: (none)]", lines)
	}
}

// A BuildSharedTargetPlan failure (a platform that errors on intent collection)
// must propagate out of RunSharedTargetProjectionExact rather than being
// swallowed by the prune step.
func TestRunSharedTargetProjectionExact_PropagatesBuildError(t *testing.T) {
	repo, _ := setupRepoAgentsHome(t)
	boom := stubPlatform{id: "boom", err: errors.New("collect failed")}
	if _, err := RunSharedTargetProjectionExact("proj", repo, []Platform{boom}, false, true); err == nil {
		t.Fatal("expected build error to propagate, got nil")
	}
}

// PruneStaleSharedTargets on a plan whose prune dir does not exist on disk is a
// clean no-op (the ENOENT-continue branch) — a freshly-resolved project that
// has never been projected has nothing to prune.
func TestPruneStaleSharedTargets_MissingDirIsNoop(t *testing.T) {
	repo, agentsHome := setupRepoAgentsHome(t)
	plan := planWithSkillTarget(t, filepath.Join(".agents", "skills"))
	pruned, err := plan.PruneStaleSharedTargets(repo, agentsHome)
	if err != nil {
		t.Fatalf("PruneStaleSharedTargets on missing dir: %v", err)
	}
	if len(pruned) != 0 {
		t.Fatalf("pruned = %v, want none for a never-projected tree", pruned)
	}
}

// RunSharedTargetProjectionExact apply path with an empty resolved plan writes
// nothing and prunes nothing — the no-resources guard.
func TestRunSharedTargetProjectionExact_ApplyEmptyPlanNoop(t *testing.T) {
	repo, _ := setupRepoAgentsHome(t)
	lines, err := RunSharedTargetProjectionExact("proj", repo, []Platform{}, false, true)
	if err != nil {
		t.Fatalf("RunSharedTargetProjectionExact empty apply: %v", err)
	}
	if lines != nil {
		t.Fatalf("empty apply lines = %v, want nil", lines)
	}
}

// PruneStaleSharedTargets must aggregate a removal failure rather than report a
// clean prune: when RemoveIfSymlinkUnder cannot delete a managed link, the error
// surfaces and the link is NOT counted as pruned. The unlink failure is injected
// through the removeIfSymlinkUnder seam so the branch runs deterministically on
// every OS (a chmod-denied directory does not block the owner's unlink on
// Windows).
func TestPruneStaleSharedTargets_RemovalFailureAggregates(t *testing.T) {
	repo, agentsHome := setupRepoAgentsHome(t)
	relDir := filepath.Join(".agents", "skills")
	stale := filepath.Join(repo, relDir, "obsolete")
	seedManagedLink(t, agentsHome, "obsolete", stale)

	sentinel := errors.New("injected unlink failure")
	defer swapRemoveIfSymlinkUnder(func(string, string) error { return sentinel })()

	plan := planWithSkillTarget(t, relDir)
	pruned, err := plan.PruneStaleSharedTargets(repo, agentsHome)
	if err == nil {
		t.Fatal("expected a removal failure to aggregate, got nil")
	}
	if len(pruned) != 0 {
		t.Fatalf("pruned = %v, want none when removal failed", pruned)
	}
	if !strings.Contains(err.Error(), "prune managed target") {
		t.Fatalf("error = %q, want prune-managed-target context", err)
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want wrapped injected failure", err)
	}
}

// RunSharedTargetProjectionExact apply must propagate an Execute failure (the
// plan.Execute call) rather than proceeding to prune. The failure is injected
// through the executeResourcePlan seam so the branch runs deterministically on
// every OS (a chmod read-only repo dir does not deny the owner's write on
// Windows).
func TestRunSharedTargetProjectionExact_ApplyExecuteErrorPropagates(t *testing.T) {
	repo, agentsHome := setupRepoAgentsHome(t)
	t.Setenv("AGENTS_HOME", agentsHome)
	testutil.SymlinkOrSkip(t)

	// Resolve a non-empty plan so the len(plan.Resources) > 0 guard passes and
	// Execute is actually reached.
	writeFixtureImportedSkillPair(t, repo, agentsHome, "proj", "review")

	sentinel := errors.New("injected execute failure")
	defer swapExecuteResourcePlan(func(ResourcePlan, string, string) error { return sentinel })()

	platforms := []Platform{NewClaude()}
	_, err := RunSharedTargetProjectionExact("proj", repo, platforms, false, true)
	if err == nil {
		t.Fatal("expected Execute failure to propagate, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want injected execute failure", err)
	}
}

// ---------------------------------------------------------------------------
// t2b: CAS-aware projection for the FILE-shaped agents surfaces (Codex TOML
// render, OpenCode/Copilot agent-file symlink).
// ---------------------------------------------------------------------------

// sourcedAgentBundle is a minimal AGENT.md bundle materializeUnit can turn
// into a "agents"-family ResolvedUnit for the file-shaped projection tests.
func sourcedAgentBundle(name, description string) map[string]string {
	return map[string]string{
		agentManifestName: "---\nname: " + name + "\ndescription: " + description + "\n---\n" + description + " body\n",
	}
}

// TestProjectResolvedUnits_SourcedAgentReachesFileShapedPlatforms is the t2b
// positive acceptance test: a sourced "agents" unit must reach Codex
// (rendered `.toml`), OpenCode, and Copilot (symlinked `.md`/`.agent.md`) —
// not only Claude's dir-mirror bucket, which t2 already covered.
func TestProjectResolvedUnits_SourcedAgentReachesFileShapedPlatforms(t *testing.T) {
	testutil.SymlinkOrSkip(t)
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	repo := filepath.Join(t.TempDir(), "repo")

	u := materializeUnit(t, home, "agents", "da-agc", "platform-dirs-change-analyst", sourcedAgentBundle("platform-dirs-change-analyst", "reviews platform dir changes"))

	platforms := []Platform{NewClaude(), NewCodex(), NewOpenCode(), NewCopilot()}
	if _, err := ProjectResolvedUnits("proj", repo, []ResolvedUnit{u}, platforms, false, true, "proj"); err != nil {
		t.Fatalf("ProjectResolvedUnits: %v", err)
	}

	// Codex: a RENDERED .toml, not a symlink — content is derived from AGENT.md.
	tomlPath := filepath.Join(repo, codexDir, "agents", "platform-dirs-change-analyst.toml")
	tomlBytes, err := os.ReadFile(tomlPath)
	if err != nil {
		t.Fatalf("expected codex toml at %s: %v", tomlPath, err)
	}
	if !strings.Contains(string(tomlBytes), `name = "platform-dirs-change-analyst"`) {
		t.Fatalf("codex toml missing rendered name: %q", tomlBytes)
	}
	if !strings.Contains(string(tomlBytes), "reviews platform dir changes") {
		t.Fatalf("codex toml missing rendered description: %q", tomlBytes)
	}

	// OpenCode: a managed SYMLINK straight to the unit's CAS AGENT.md.
	opencodeLink := filepath.Join(repo, opencodeDir, "agent", "platform-dirs-change-analyst.md")
	wantSrc := filepath.Join(u.CASPath, agentManifestName)
	if !links.IsManagedLink(opencodeLink, wantSrc) {
		t.Fatalf("expected opencode agent link %s -> %s", opencodeLink, wantSrc)
	}

	// Copilot: same shape, .agent.md suffix.
	copilotLink := filepath.Join(repo, copilotGitHubDir, "agents", "platform-dirs-change-analyst.agent.md")
	if !links.IsManagedLink(copilotLink, wantSrc) {
		t.Fatalf("expected copilot agent link %s -> %s", copilotLink, wantSrc)
	}

	// Claude (t2, sanity: the dir-mirror path must still work alongside t2b).
	claudeDirLink := filepath.Join(repo, claudeDir, "agents", "platform-dirs-change-analyst")
	if !links.IsManagedLink(claudeDirLink, u.CASPath) {
		t.Fatalf("expected claude agent dir link %s -> %s", claudeDirLink, u.CASPath)
	}
}

// TestProjectResolvedUnits_SourcedAgentExactPruneRemovesFileShapedOutputs is
// the t2b negative/removal acceptance test: dropping the resolved unit must
// prune the Codex/OpenCode/Copilot outputs it created, exactly like the
// dir-mirror shapes already prune (materialize_test.go's
// TestProjectResolvedUnits_ExactPruneRemovesDroppedUnit).
func TestProjectResolvedUnits_SourcedAgentExactPruneRemovesFileShapedOutputs(t *testing.T) {
	testutil.SymlinkOrSkip(t)
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	repo := filepath.Join(t.TempDir(), "repo")

	u := materializeUnit(t, home, "agents", "da-agc", "platform-dirs-change-analyst", sourcedAgentBundle("platform-dirs-change-analyst", "reviews platform dir changes"))
	platforms := []Platform{NewCodex(), NewOpenCode(), NewCopilot()}
	if _, err := ProjectResolvedUnits("proj", repo, []ResolvedUnit{u}, platforms, false, true, "proj"); err != nil {
		t.Fatalf("project {unit}: %v", err)
	}

	tomlPath := filepath.Join(repo, codexDir, "agents", "platform-dirs-change-analyst.toml")
	opencodeLink := filepath.Join(repo, opencodeDir, "agent", "platform-dirs-change-analyst.md")
	copilotLink := filepath.Join(repo, copilotGitHubDir, "agents", "platform-dirs-change-analyst.agent.md")
	for _, p := range []string{tomlPath, opencodeLink, copilotLink} {
		if _, err := os.Lstat(p); err != nil {
			t.Fatalf("expected %s to exist before removal: %v", p, err)
		}
	}

	// Re-project with zero units → exact/prune must remove the SYMLINK-shaped
	// outputs (OpenCode/Copilot) directly, via the one-to-zero prune floor.
	if _, err := ProjectResolvedUnits("proj", repo, nil, platforms, false, true, "proj"); err != nil {
		t.Fatalf("project {}: %v", err)
	}
	if _, err := os.Lstat(opencodeLink); !os.IsNotExist(err) {
		t.Fatalf("expected opencode agent link pruned, err=%v", err)
	}
	if _, err := os.Lstat(copilotLink); !os.IsNotExist(err) {
		t.Fatalf("expected copilot agent link pruned, err=%v", err)
	}

	// Codex's rendered .toml is a plain file, invisible to the generic
	// symlink-only prune scan regardless of forced dirs — its removal is the
	// dedicated pruneCodexRepoAgentTomls complement, which the real
	// install/refresh flow runs via CreateLinks AFTER ProjectResolvedUnits
	// (see commands/internal/lifecycle/install.go's runInstallSharedTargetsFor
	// -> createInstallPlatformLink ordering). With no lock entry for the
	// removed unit (this test never wrote one), pruneCodexRepoAgentTomls sees
	// no sourced names to protect and removes the orphaned toml.
	if err := NewCodex().CreateLinks("proj", repo); err != nil {
		t.Fatalf("codex CreateLinks (prune pass): %v", err)
	}
	if _, err := os.Lstat(tomlPath); !os.IsNotExist(err) {
		t.Fatalf("expected codex toml pruned via pruneCodexRepoAgentTomls, err=%v", err)
	}
}

// TestPruneCodexRepoAgentTomls_PreservesSourcedAgentToml is the fail-before-
// fix guard for the legacy-prune interaction t2b had to close: codex's own
// CreateLinks-time prune step (pruneCodexRepoAgentTomls) only knew about
// LOCAL canonical agents, so it would delete a sourced agent's rendered
// `.toml` moments after ProjectResolvedUnits created it. A `.toml` whose name
// matches a locked kind:artifact "agent/<name>" unit must survive; an
// unrelated stale `.toml` must still be pruned.
func TestPruneCodexRepoAgentTomls_PreservesSourcedAgentToml(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	repo := filepath.Join(tmp, "repo")
	dstRoot := filepath.Join(repo, codexDir, "agents")
	if err := os.MkdirAll(dstRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dstRoot, "platform-dirs-change-analyst.toml"), []byte("name = \"platform-dirs-change-analyst\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dstRoot, "unrelated-stale.toml"), []byte("name = \"unrelated-stale\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// No local canonical agents bucket at all — the sourced name is the ONLY
	// reason platform-dirs-change-analyst.toml should survive.
	digest := "sha256:" + strings.Repeat("1", 64)
	if err := config.WriteUnitsLock(repo, config.UnitsLock{
		Units: map[string]config.LockedUnit{
			"da-agc:agent/platform-dirs-change-analyst@main": {Kind: config.UnitKindArtifact, Digest: digest, FetchedAt: "2026-07-15T00:00:00Z"},
		},
	}); err != nil {
		t.Fatalf("seed lock: %v", err)
	}

	if err := pruneCodexRepoAgentTomls("proj", repo, agentsHome); err != nil {
		t.Fatalf("pruneCodexRepoAgentTomls: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dstRoot, "platform-dirs-change-analyst.toml")); err != nil {
		t.Fatalf("sourced agent toml was pruned: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dstRoot, "unrelated-stale.toml")); !os.IsNotExist(err) {
		t.Fatalf("expected the unrelated stale toml to be pruned, err=%v", err)
	}
}
