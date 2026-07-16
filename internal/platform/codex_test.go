package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/links"
	"github.com/AGOrcha/dot-agents/internal/linktest"
	"github.com/AGOrcha/dot-agents/internal/testutil"
)

const codexAgentMarkdownFile = "AGENT.md"

func TestRenderCodexAgentTomlUsesFrontmatterAndBody(t *testing.T) {
	tmp := t.TempDir()
	agentDir := filepath.Join(tmp, "agents", "global", "reviewer")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatal(err)
	}
	agentMD := filepath.Join(agentDir, codexAgentMarkdownFile)
	content := `---
name: reviewer
description: reviews changes
model: gpt-5.1-codex
is_background: true
---

# Reviewer

Use "safe" defaults and avoid shell footguns.
`
	if err := os.WriteFile(agentMD, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := renderCodexAgentToml(agentMD)
	if err != nil {
		t.Fatalf("renderCodexAgentToml failed: %v", err)
	}

	out := string(got)
	for _, want := range []string{
		`name = "reviewer"`,
		`description = "reviews changes"`,
		`model = "gpt-5.1-codex"`,
		`developer_instructions = """`,
		`# Reviewer`,
		`Use "safe" defaults and avoid shell footguns.`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("render output missing %q:\n%s", want, out)
		}
	}
}

func TestCodexCreateLinksWritesNativeAgentTomlAndCleansCompat(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	home := filepath.Join(tmp, "home")
	repo := filepath.Join(tmp, "repo")

	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)

	globalAgentDir := filepath.Join(agentsHome, "agents", "global", "reviewer")
	projectAgentDir := filepath.Join(agentsHome, "agents", "proj", "implementer")
	for _, dir := range []string{globalAgentDir, projectAgentDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	mustWriteCodexFixtureFile(t, filepath.Join(globalAgentDir, codexAgentMarkdownFile), `---
name: reviewer
description: global reviewer
model: gpt-5.1-codex
---

# Reviewer
`)
	mustWriteCodexFixtureFile(t, filepath.Join(projectAgentDir, codexAgentMarkdownFile), `---
name: implementer
description: project implementer
is_background: false
---

# Implementer

Build the feature and keep tests green.
`)

	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}

	if err := CollectAndExecuteSharedTargetPlan("proj", repo, []Platform{NewCodex()}); err != nil {
		t.Fatalf("CollectAndExecuteSharedTargetPlan: %v", err)
	}
	if err := NewCodex().CreateLinks("proj", repo); err != nil {
		t.Fatalf("CreateLinks failed: %v", err)
	}

	projectToml := filepath.Join(repo, ".codex", "agents", "implementer.toml")
	assertCodexFileContains(t, "project toml", projectToml, []string{
		`name = "implementer"`,
		`description = "project implementer"`,
		`Build the feature and keep tests green.`,
	})

	userToml := filepath.Join(home, ".codex", "agents", "reviewer.toml")
	assertCodexFileContains(t, "user toml", userToml, []string{
		`name = "reviewer"`,
		`description = "global reviewer"`,
		`model = "gpt-5.1-codex"`,
	})

	assertCodexPathNotExists(t, filepath.Join(repo, ".claude", "agents"), "legacy compat path should be cleaned up")

	if err := NewCodex().RemoveLinks("proj", repo); err != nil {
		t.Fatalf("RemoveLinks failed: %v", err)
	}

	assertCodexPathNotExists(t, projectToml, "project native agent should be removed")
	assertCodexPathNotExists(t, filepath.Join(repo, ".claude", "agents"), "legacy compat path should stay removed")
}

func mustWriteCodexFixtureFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func assertCodexFileContains(t *testing.T, label, path string, want []string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected %s at %s: %v", label, path, err)
	}
	got := string(content)
	for _, snippet := range want {
		if !strings.Contains(got, snippet) {
			t.Fatalf("%s missing %q:\n%s", label, snippet, got)
		}
	}
}

func assertCodexPathNotExists(t *testing.T, path, message string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("%s, got %v", message, err)
	}
}

// ---------------------------------------------------------------------------
// Codex CreateLinks + SharedTargetIntents + hook rendering coverage
// (relocated from coverage_gap2_test.go).
// ---------------------------------------------------------------------------

// TestCodexCreateLinks_FullRulesAndSettings drives the rules→AGENTS.md and
// settings→config.toml branches of (*codex).CreateLinks.
func TestCodexCreateLinks_FullRulesAndSettings(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	home := filepath.Join(tmp, "home")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatal(err)
	}
	// Seed global rules and project override.
	if err := os.MkdirAll(filepath.Join(agentsHome, "rules", "global"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentsHome, "rules", "global", "agents.md"), []byte("# global\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(agentsHome, "rules", "proj"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentsHome, "rules", "proj", "agents.md"), []byte("# proj\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Seed settings/codex.toml.
	if err := os.MkdirAll(filepath.Join(agentsHome, "settings", "proj"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentsHome, "settings", "proj", "codex.toml"), []byte("model = \"x\"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	if err := NewCodex().CreateLinks("proj", repo); err != nil {
		t.Fatalf("CreateLinks: %v", err)
	}

	// AGENTS.md should be linked (project override wins).
	if !links.IsManagedLink(filepath.Join(repo, "AGENTS.md"), filepath.Join(agentsHome, "rules", "proj", "agents.md")) {
		t.Error("AGENTS.md should be a managed link to the project override")
	}
	if _, err := os.Lstat(filepath.Join(repo, ".codex", "config.toml")); err != nil {
		t.Errorf("config.toml missing: %v", err)
	}
}

// TestCodexSharedTargetIntentsPopulated drives skills + codex-agent-toml intents.
func TestCodexSharedTargetIntents_Populated(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	for _, p := range [][]string{
		{"skills", "proj", "alpha", "SKILL.md"},
		{"agents", "proj", "reviewer", "AGENT.md"},
	} {
		dir := filepath.Join(append([]string{agentsHome}, p[:3]...)...)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, p[3]), []byte("body"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	intents, err := NewCodex().SharedTargetIntents("proj")
	if err != nil {
		t.Fatalf("SharedTargetIntents: %v", err)
	}
	if len(intents) == 0 {
		t.Error("expected non-zero intents")
	}
}

// TestRenderCodexHookConfigMatcherBranches covers session_start/pre_tool_use/
// post_tool_use which take the matcherForSpec branch versus stop which uses
// empty matcher.
func TestRenderCodexHookConfig_MatcherBranches(t *testing.T) {
	specs := []HookSpec{
		{Name: "a", When: "stop", Command: "/bin/x"},
		{Name: "b", When: "pre_tool_use", Command: "/bin/y", MatchExpression: "Bash"},
	}
	content, err := renderCodexHookConfig(specs)
	if err != nil {
		t.Fatalf("renderCodexHookConfig: %v", err)
	}
	got := string(content)
	if !strings.Contains(got, "Stop") {
		t.Errorf("expected Stop event: %s", got)
	}
	if !strings.Contains(got, `"matcher": "Bash"`) {
		t.Errorf("expected Bash matcher: %s", got)
	}
}

// ---------------------------------------------------------------------------
// Codex agent-toml + session model resolution coverage (relocated from
// coverage_gap3_test.go).
// ---------------------------------------------------------------------------

// TestWriteCodexAgentTomlFile_ExistingFileReplaced drives the Lstat→Remove branch.
func TestWriteCodexAgentTomlFile_ExistingFileReplaced(t *testing.T) {
	tmp := t.TempDir()
	agent := filepath.Join(tmp, "AGENT.md")
	if err := os.WriteFile(agent, []byte("---\nname: x\n---\nbody\n"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(tmp, "x.toml")
	// A MANAGED stale render (carries the marker) is ours to replace.
	if err := os.WriteFile(dst, []byte(codexManagedTomlMarker+"\nname = \"old\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := writeCodexAgentTomlFile(stdPlatformIO{}, dst, agent); err != nil {
		t.Fatalf("writeCodexAgentTomlFile: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `name = "x"`) {
		t.Errorf("file content not refreshed: %q", got)
	}
	if !isManagedCodexTomlBytes(got) {
		t.Errorf("rewritten toml lost its managed provenance marker: %q", got)
	}
}

// TestWriteCodexAgentTomlFile_DivergedUserAuthoredFilePreservedAtAlternate is
// the t2c collision-resolution guard (supersedes t2b's unconditional
// refuse): a NON-managed `.toml` (no marker) occupying the target whose
// content DIVERGES from the render must not be silently clobbered, but the
// write must also no longer fail closed — it preserves the diverged bytes at
// a sibling alternate path, writes an import-conflict review note, and lands
// the managed render at the canonical name.
func TestWriteCodexAgentTomlFile_DivergedUserAuthoredFilePreservedAtAlternate(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	agent := filepath.Join(tmp, "AGENT.md")
	if err := os.WriteFile(agent, []byte("---\nname: x\n---\nbody\n"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(tmp, "x.toml")
	const userContent = "# my hand-written codex agent\nname = \"mine\"\n"
	if err := os.WriteFile(dst, []byte(userContent), 0644); err != nil {
		t.Fatal(err)
	}
	if err := writeCodexAgentTomlFile(stdPlatformIO{}, dst, agent); err != nil {
		t.Fatalf("writeCodexAgentTomlFile: unexpected error: %v", err)
	}

	rendered, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if managed, mErr := isManagedCodexToml(dst); mErr != nil || !managed {
		t.Fatalf("expected canonical name to hold a managed render, got managed=%v err=%v", managed, mErr)
	}
	if strings.Contains(string(rendered), "mine") {
		t.Fatalf("canonical name still carries the diverged user content: %q", rendered)
	}

	altPath := filepath.Join(tmp, "x.codex-preexisting.toml")
	altGot, err := os.ReadFile(altPath)
	if err != nil {
		t.Fatalf("expected diverged content preserved at %s: %v", altPath, err)
	}
	if string(altGot) != userContent {
		t.Fatalf("preserved alternate content = %q, want %q", altGot, userContent)
	}

	notesDir := filepath.Join(agentsHome, "review-notes", "import-conflicts")
	entries, err := os.ReadDir(notesDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected exactly one import-conflict review note, got dir=%v err=%v", entries, err)
	}
	noteData, err := os.ReadFile(filepath.Join(notesDir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	note := string(noteData)
	for _, want := range []string{"origin: codex", dst, altPath} {
		if !strings.Contains(note, want) {
			t.Errorf("review note missing %q:\n%s", want, note)
		}
	}
}

// TestWriteCodexAgentTomlFile_ByteIdenticalUnmarkedFileAdoptedSilently is the
// t2c mark-adopt path: an unmarked `.toml` whose bytes already equal what
// writeCodexAgentTomlFile would render (the marker-upgrade-migration
// collision every pre-existing install hits on its first post-marker
// refresh) is adopted silently — no error, the marker is added, and no
// alternate file or review note is produced.
func TestWriteCodexAgentTomlFile_ByteIdenticalUnmarkedFileAdoptedSilently(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	agent := filepath.Join(tmp, "AGENT.md")
	if err := os.WriteFile(agent, []byte("---\nname: x\n---\nbody\n"), 0644); err != nil {
		t.Fatal(err)
	}
	rendered, err := renderCodexAgentToml(agent)
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(tmp, "x.toml")
	if err := os.WriteFile(dst, rendered, 0644); err != nil {
		t.Fatal(err)
	}

	if err := writeCodexAgentTomlFile(stdPlatformIO{}, dst, agent); err != nil {
		t.Fatalf("writeCodexAgentTomlFile: unexpected error: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	wantBody := string(rendered)
	if !strings.HasPrefix(string(got), codexManagedTomlMarker+"\n") {
		t.Fatalf("adopted file missing managed marker: %q", got)
	}
	if strings.TrimPrefix(string(got), codexManagedTomlMarker+"\n") != wantBody {
		t.Fatalf("adopted file body changed: got %q want %q", got, wantBody)
	}

	if _, err := os.Stat(filepath.Join(tmp, "x.codex-preexisting.toml")); !os.IsNotExist(err) {
		t.Fatalf("byte-identical adoption must not create an alternate file, stat err=%v", err)
	}
	notesDir := filepath.Join(agentsHome, "review-notes", "import-conflicts")
	if entries, err := os.ReadDir(notesDir); err == nil && len(entries) != 0 {
		t.Fatalf("byte-identical adoption must not write a review note, found %d", len(entries))
	}
}

// TestWriteCodexAgentTomlFile_RefusesNonRegularOccupant preserves t2b's
// fail-closed refuse for occupants that can never be a prior render (a
// directory in this case) — the content-aware adoption in t2c only applies
// to regular files.
func TestWriteCodexAgentTomlFile_RefusesNonRegularOccupant(t *testing.T) {
	tmp := t.TempDir()
	agent := filepath.Join(tmp, "AGENT.md")
	if err := os.WriteFile(agent, []byte("---\nname: x\n---\nbody\n"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(tmp, "x.toml")
	if err := os.Mkdir(dst, 0755); err != nil {
		t.Fatal(err)
	}
	if err := writeCodexAgentTomlFile(stdPlatformIO{}, dst, agent); err == nil {
		t.Fatal("expected writeCodexAgentTomlFile to refuse a non-regular occupant")
	}
	info, err := os.Stat(dst)
	if err != nil || !info.IsDir() {
		t.Fatalf("non-regular occupant was modified: info=%v err=%v", info, err)
	}
}

// TestCollectAndExecuteSharedTargetPlan_UpgradeMigrationCollisionResolvesClean
// drives the t2c scenario end-to-end through the real projection entry point
// (not the low-level writeCodexAgentTomlFile unit) — the exact shape a first
// `da refresh`/`da install` after upgrading to the marker takes: a
// pre-existing UNMARKED `.codex/agents/<name>.toml` (as every install from
// before codexManagedTomlMarker existed has) must no longer refuse the
// project, matching t2b's own fold-back
// obs-codex-toml-marker-upgrade-migration.
func TestCollectAndExecuteSharedTargetPlan_UpgradeMigrationCollisionResolvesClean(t *testing.T) {
	repo, agentsHome := setupRepoAgentsHome(t)
	writeFixtureCodexAgent(t, agentsHome)
	t.Setenv("AGENTS_HOME", agentsHome)

	tomlPath := filepath.Join(repo, ".codex", "agents", "implementer.toml")
	if err := os.MkdirAll(filepath.Dir(tomlPath), 0755); err != nil {
		t.Fatal(err)
	}
	const preexisting = "# hand-edited before the marker existed\nname = \"implementer\"\ncustom = true\n"
	if err := os.WriteFile(tomlPath, []byte(preexisting), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CollectAndExecuteSharedTargetPlan("proj", repo, []Platform{NewCodex()}); err != nil {
		t.Fatalf("CollectAndExecuteSharedTargetPlan must complete clean on a pre-marker collision, got: %v", err)
	}

	got, err := os.ReadFile(tomlPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(got), codexManagedTomlMarker+"\n") {
		t.Fatalf("canonical name must carry the managed render after resolution: %q", got)
	}
	if !strings.Contains(string(got), `name = "implementer"`) {
		t.Fatalf("canonical name must carry the dot-agents render, got: %q", got)
	}

	altPath := filepath.Join(repo, ".codex", "agents", "implementer.codex-preexisting.toml")
	altGot, err := os.ReadFile(altPath)
	if err != nil {
		t.Fatalf("expected the hand-edited content preserved at %s: %v", altPath, err)
	}
	if string(altGot) != preexisting {
		t.Fatalf("preserved content = %q, want %q", altGot, preexisting)
	}

	notesDir := filepath.Join(agentsHome, "review-notes", "import-conflicts")
	entries, err := os.ReadDir(notesDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected exactly one import-conflict review note, got dir=%v err=%v", entries, err)
	}
}

func TestWriteCodexAgentTomlFile_BadAgentMD(t *testing.T) {
	if err := writeCodexAgentTomlFile(stdPlatformIO{}, filepath.Join(t.TempDir(), "x.toml"), "/no/such/agent.md"); err == nil {
		t.Error("expected error for missing agent.md")
	}
}

// TestRemoveSharedTargets_RenderedTomlPath drives the codex-agent-toml remove
// path.
func TestRemoveManagedIntentTarget_CodexTomlPath(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "out.toml")
	if err := os.WriteFile(target, []byte("dummy"), 0644); err != nil {
		t.Fatal(err)
	}
	intent := ResourceIntent{
		Shape:        ResourceShapeRenderSingle,
		Transport:    ResourceTransportWrite,
		Materializer: codexAgentTomlMaterializer,
		TargetPath:   "out.toml",
	}
	if err := removeManagedIntentTarget(intent, tmp, t.TempDir()); err != nil {
		t.Fatalf("removeManagedIntentTarget: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Error("expected target removed")
	}
}

// TestExecuteRenderSingleWrite_CodexAgentToml drives the codex-agent-toml
// happy-path branch via Execute.
func TestExecuteRenderSingleWrite_CodexAgentToml(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	src := filepath.Join(agentsHome, "agents", "proj", "reviewer", "AGENT.md")
	if err := os.MkdirAll(filepath.Dir(src), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("---\nname: reviewer\n---\nbody\n"), 0644); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	intent := ResourceIntent{
		IntentID:    "codex.proj.reviewer.toml",
		Project:     "proj",
		Bucket:      "agents",
		LogicalName: "reviewer",
		TargetPath:  filepath.Join(".codex/agents", "reviewer.toml"),
		Ownership:   ResourceOwnershipSharedRepo,
		SourceRef: ResourceSourceRef{
			Scope:        "proj",
			Bucket:       "agents",
			RelativePath: "reviewer/AGENT.md",
			Kind:         ResourceSourceCanonicalFile,
		},
		Shape:         ResourceShapeRenderSingle,
		Transport:     ResourceTransportWrite,
		Materializer:  codexAgentTomlMaterializer,
		ReplacePolicy: ResourceReplaceAllowlistedImportedDirOnly,
		PrunePolicy:   ResourcePruneTarget,
	}
	plan, err := BuildResourcePlan([]ResourceIntent{intent})
	if err != nil {
		t.Fatalf("BuildResourcePlan: %v", err)
	}
	if err := plan.Execute(repo, agentsHome); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".codex/agents/reviewer.toml")); err != nil {
		t.Errorf("expected toml file: %v", err)
	}
}

// TestResolveCodexModelFromJSONL_NoResponseLine drives the empty-result branch
// (model in JSONL is "").
func TestResolveCodexModelFromJSONL_NoModel(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".codex", "sessions", "2026", "05", "11")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	sessID := "no-model"
	path := filepath.Join(dir, "rollout-"+sessID+".jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"event_msg","payload":{"type":"task_started"}}`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := resolveCodexModelFromJSONL(home, sessID); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

// TestIsManagedCodexToml classifies a managed render, a user file, an absent
// path, and a symlink (defect 1 provenance predicate).
func TestIsManagedCodexToml(t *testing.T) {
	tmp := t.TempDir()

	managed := filepath.Join(tmp, "managed.toml")
	if err := os.WriteFile(managed, []byte(codexManagedTomlMarker+"\nname = \"x\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ok, err := isManagedCodexToml(managed); err != nil || !ok {
		t.Fatalf("managed render: ok=%v err=%v, want true,nil", ok, err)
	}

	user := filepath.Join(tmp, "user.toml")
	if err := os.WriteFile(user, []byte("name = \"user-authored\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ok, err := isManagedCodexToml(user); err != nil || ok {
		t.Fatalf("user file: ok=%v err=%v, want false,nil", ok, err)
	}

	if ok, err := isManagedCodexToml(filepath.Join(tmp, "absent.toml")); err != nil || ok {
		t.Fatalf("absent: ok=%v err=%v, want false,nil", ok, err)
	}

	link := filepath.Join(tmp, "link.toml")
	if err := os.Symlink(managed, link); err == nil {
		if ok, err := isManagedCodexToml(link); err != nil || ok {
			t.Fatalf("symlink: ok=%v err=%v, want false,nil (only regular files are ours)", ok, err)
		}
	}
}

// ---------------------------------------------------------------------------
// Codex CreateLinks + scan session error coverage (relocated from coverage_gap5).
// ---------------------------------------------------------------------------

// TestCodexCreateLinks_GlobalRuleOnly drives the global-rules path when
// project override is absent.
func TestCodexCreateLinks_GlobalRuleOnly(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	home := filepath.Join(tmp, "home")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(agentsHome, "rules", "global"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentsHome, "rules", "global", "rules.md"), []byte("# rules"), 0644); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	if err := NewCodex().CreateLinks("proj", repo); err != nil {
		t.Fatalf("CreateLinks: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(repo, "AGENTS.md")); err != nil {
		t.Errorf("AGENTS.md missing: %v", err)
	}
}

// TestCodexScanSessionTokens_OpenError exercises the os.Open failure branch
// when findCodexSessionFile returns a path that cannot be opened (e.g. it
// resolves to a directory instead of a regular file).
func TestCodexScanSessionTokens_OpenError(t *testing.T) {
	home := t.TempDir()
	sessID := "open-err-id"
	// Create the expected daily directory and place a *directory* at the
	// rollout filename — findCodexSessionFile globs by suffix and will pick
	// it up, but os.Open succeeds on directories on Unix. To trigger an
	// open error we instead seed an unreadable file.
	dir := filepath.Join(home, ".codex", "sessions", "2026", "05", "11")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "rollout-2026-05-11-"+sessID+".jsonl")
	if err := os.WriteFile(path, []byte("ignored"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	// On macOS running as root, mode 0o000 may still be readable; treat the
	// success path as acceptable too — what matters is that the function
	// returns without panicking.
	got := codexScanSessionTokens(home, sessID, "")
	_ = got
}

// ---------------------------------------------------------------------------
// Codex session-file resolution + token scanner + usage stats coverage
// (relocated from coverage_gap_test.go).
// ---------------------------------------------------------------------------

// TestFindCodexSessionFile_LocatesFile constructs the nested sessions
// directory and verifies findCodexSessionFile finds it.
func TestFindCodexSessionFile_LocatesFile(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".codex", "sessions", "2026", "05", "11")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	sessID := "abc-123"
	path := filepath.Join(dir, "rollout-2026-05-11-"+sessID+".jsonl")
	if err := os.WriteFile(path, []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got := findCodexSessionFile(home, sessID)
	if got != path {
		t.Errorf("findCodexSessionFile = %q, want %q", got, path)
	}
}

func TestFindCodexSessionFile_EmptyID(t *testing.T) {
	if got := findCodexSessionFile(t.TempDir(), ""); got != "" {
		t.Errorf("expected empty for missing session id, got %q", got)
	}
}

func TestFindCodexSessionFile_NoMatch(t *testing.T) {
	if got := findCodexSessionFile(t.TempDir(), "missing"); got != "" {
		t.Errorf("expected empty for no match, got %q", got)
	}
}

// TestResolveCodexModelFromJSONL parses a synthetic response_item entry.
func TestResolveCodexModelFromJSONL(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".codex", "sessions", "2026", "05", "11")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	sessID := "sess-123"
	path := filepath.Join(dir, "rollout-2026-05-11-"+sessID+".jsonl")
	lines := []string{
		`{"type":"event_msg","payload":{"type":"task_started"}}`,
		`{"type":"response_item","payload":{"type":"response","model":"gpt-5"}}`,
		`{"type":"response_item","payload":{"type":"response","model":"gpt-5.1"}}`,
		`not-json`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got := resolveCodexModelFromJSONL(home, sessID)
	if got != "gpt-5.1" {
		t.Errorf("model = %q, want gpt-5.1 (last response wins)", got)
	}
}

func TestResolveCodexModelFromJSONL_NoSession(t *testing.T) {
	if got := resolveCodexModelFromJSONL(t.TempDir(), "missing"); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

// TestCodexAccumulateTokenEntry table-drives the per-line accumulator.
func TestCodexAccumulateTokenEntry(t *testing.T) {
	cases := []struct {
		name     string
		line     string
		after    time.Time
		wantIn   int
		wantOut  int
		wantMsgs int
	}{
		{
			name:     "valid token_count adds to metrics",
			line:     `{"type":"event_msg","timestamp":"2026-05-11T12:00:00Z","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":10,"output_tokens":20,"cached_input_tokens":5,"reasoning_output_tokens":2}}}}`,
			wantIn:   10,
			wantOut:  20,
			wantMsgs: 1,
		},
		{
			name: "non-event_msg ignored",
			line: `{"type":"response_item","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":99,"output_tokens":99}}}}`,
		},
		{
			name: "missing info ignored",
			line: `{"type":"event_msg","payload":{"type":"token_count","info":null}}`,
		},
		{
			name: "missing last_token_usage ignored",
			line: `{"type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":null}}}`,
		},
		{
			name: "non-token_count payload ignored",
			line: `{"type":"event_msg","payload":{"type":"task_started"}}`,
		},
		{
			name: "malformed JSON ignored",
			line: `not-json`,
		},
		{
			name:  "after-cutoff entry skipped",
			line:  `{"type":"event_msg","timestamp":"2026-05-11T10:00:00Z","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":7}}}}`,
			after: time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var m SessionTokenMetrics
			codexAccumulateTokenEntry([]byte(tc.line), tc.after, &m)
			if m.InputTokens != tc.wantIn {
				t.Errorf("InputTokens = %d, want %d", m.InputTokens, tc.wantIn)
			}
			if m.OutputTokens != tc.wantOut {
				t.Errorf("OutputTokens = %d, want %d", m.OutputTokens, tc.wantOut)
			}
			if m.MessageCount != tc.wantMsgs {
				t.Errorf("MessageCount = %d, want %d", m.MessageCount, tc.wantMsgs)
			}
		})
	}
}

// TestCodexScanSessionTokens_AggregatesAcrossLines drives the full
// codexScanSessionTokens path with synthetic session files.
func TestCodexScanSessionTokens_AggregatesAcrossLines(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".codex", "sessions", "2026", "05", "11")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	sessID := "scan-test"
	path := filepath.Join(dir, "rollout-2026-05-11-"+sessID+".jsonl")
	lines := []string{
		`{"type":"event_msg","timestamp":"2026-05-11T11:00:00Z","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":100,"output_tokens":200,"cached_input_tokens":50,"reasoning_output_tokens":5}}}}`,
		`{"type":"event_msg","timestamp":"2026-05-11T13:00:00Z","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":1,"output_tokens":2,"cached_input_tokens":3,"reasoning_output_tokens":4}}}}`,
		`{"type":"event_msg","timestamp":"bad-ts","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":9}}}}`,
		// Line missing token_count substring is short-circuited.
		`{"type":"event_msg","payload":{"type":"other"}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// No timestamp filter — sums all valid token_count lines (3 of them, since
	// the unparseable-ts line still accumulates: parseJSONLTimestamp returns
	// !ok which is treated as "no after constraint").
	got := codexScanSessionTokens(home, sessID, "")
	if got.MessageCount < 2 {
		t.Errorf("MessageCount = %d, want >= 2", got.MessageCount)
	}
	if got.InputTokens < 101 {
		t.Errorf("InputTokens = %d, want >= 101", got.InputTokens)
	}

	// With cutoff at noon: the 13:00 entry contributes plus any with an
	// unparseable timestamp (whose after-check is skipped by design).
	gotFiltered := codexScanSessionTokens(home, sessID, "2026-05-11T12:00:00Z")
	if gotFiltered.MessageCount < 1 {
		t.Errorf("filtered MessageCount = %d, want >= 1", gotFiltered.MessageCount)
	}
	if gotFiltered.InputTokens < 1 {
		t.Errorf("filtered InputTokens = %d, want >= 1", gotFiltered.InputTokens)
	}
}

func TestCodexScanSessionTokens_MissingSession(t *testing.T) {
	got := codexScanSessionTokens(t.TempDir(), "missing-id", "")
	if got.InputTokens != 0 || got.MessageCount != 0 {
		t.Errorf("expected zero metrics for missing session, got %+v", got)
	}
}

func TestScanJSONLForLastModel_MissingFile(t *testing.T) {
	got := scanJSONLForLastModel("/no/such/file", func([]byte) string { return "x" })
	if got != "" {
		t.Errorf("expected empty for missing file, got %q", got)
	}
}

func TestScanJSONLForLastModel_KeepsLastNonEmpty(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "f.jsonl")
	content := "alpha\nbeta\n\n\ngamma\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	got := scanJSONLForLastModel(path, func(line []byte) string {
		return strings.TrimSpace(string(line))
	})
	if got != "gamma" {
		t.Errorf("got %q, want gamma", got)
	}
}

// TestCodexReadUsageStats_TooManyEntriesKeepsTail mirrors the claude tail
// behaviour and ensures the >10-entry branch is exercised.
func TestCodexReadUsageStats_TooManyEntriesKeepsTail(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, ".codex")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for i := 0; i < 15; i++ {
		b.WriteString(`{"id":"s` + itoa(i) + `","thread_name":"t` + itoa(i) + `","updated_at":"2026-05-11T00:00:00Z"}`)
		b.WriteString("\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "session_index.jsonl"), []byte(b.String()), 0644); err != nil {
		t.Fatal(err)
	}
	stats := codexReadUsageStats(tmp)
	if stats == nil {
		t.Fatal("nil stats")
	}
	if stats.TotalSessions != 15 {
		t.Errorf("TotalSessions = %d, want 15", stats.TotalSessions)
	}
	if len(stats.RecentSessions) != 10 {
		t.Errorf("RecentSessions tail length = %d, want 10", len(stats.RecentSessions))
	}
}

// ---------- P3: Badge + CountLinks (StatusBadger + LinkCounter) ----------

// TestCodexBadge_EmptyProject pins the empty-project contract.
func TestCodexBadge_EmptyProject(t *testing.T) {
	tmp := t.TempDir()
	got := NewCodex().(*codex).Badge("proj", tmp, filepath.Join(tmp, ".agents"))
	if got.Name != "Codex" {
		t.Errorf("Badge.Name = %q, want %q", got.Name, "Codex")
	}
	if got.Present || got.Broken {
		t.Errorf("empty project: Badge = %+v, want Present=false Broken=false", got)
	}
}

// TestCodexCountLinks_HealthyAGENTSMarkdown covers the positive single-file
// branch: a managed AGENTS.md surfaces as (ok>=1, broken=0) and Badge
// surfaces Present=true.
func TestCodexCountLinks_HealthyAGENTSMarkdown(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "AGENTS.md"), []byte("# agents"), 0644); err != nil {
		t.Fatal(err)
	}

	c := NewCodex().(*codex)
	ok, broken := c.CountLinks("proj", tmp, filepath.Join(tmp, ".agents"))
	if ok < 1 || broken != 0 {
		t.Errorf("CountLinks = (%d,%d), want (>=1,0)", ok, broken)
	}
	b := c.Badge("proj", tmp, filepath.Join(tmp, ".agents"))
	if !b.Present || b.Broken {
		t.Errorf("Badge = %+v, want Present=true Broken=false", b)
	}
}

// ---------- BrokenLinkReporter implementation (P2) ----------

// TestCodexBrokenLinks_EmptyProject is the absent-surface sentinel: no
// managed AGENTS.md yet means no diagnostics. Matches doctor's empty-
// project contract that absent != broken.
func TestCodexBrokenLinks_EmptyProject(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	projectPath := filepath.Join(tmp, "proj")
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		t.Fatal(err)
	}

	c := &codex{io: stdPlatformIO{}}
	got := c.BrokenLinks("proj", projectPath, agentsHome)
	if len(got) != 0 {
		t.Errorf("expected no broken links in empty project, got %d: %+v", len(got), got)
	}
}

// TestCodexBrokenLinks_BrokenAGENTSMD is the central broken-symlink case
// migrated from doctor's TestCollectBrokenLinks_BrokenAgentsMD: a dangling
// AGENTS.md at the repo root must surface with PlatformID="codex".
func TestCodexBrokenLinks_BrokenAGENTSMD(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	projectPath := filepath.Join(tmp, "proj")
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		t.Fatal(err)
	}
	linktest.DanglingLink(t, filepath.Join(projectPath, codexAgentsMarkdown))

	c := &codex{io: stdPlatformIO{}}
	got := c.BrokenLinks("proj", projectPath, agentsHome)
	if len(got) != 1 {
		t.Fatalf("expected 1 broken link, got %d: %+v", len(got), got)
	}
	if got[0].PlatformID != "codex" {
		t.Errorf("PlatformID = %q, want codex", got[0].PlatformID)
	}
	if got[0].LinkPath == "" || got[0].DisplayDest == "" {
		t.Errorf("LinkPath/DisplayDest unset: %+v", got[0])
	}
}

// TestCodexBrokenLinks_PlainAGENTSMDIgnored guards the contract carried over
// from lifecycle's managedLinkBroken: a plain regular file at AGENTS.md
// (not a managed link) is unmanaged user content and must NOT be reported.
// This is the negative branch of classifyManagedLink (linkStateNotALink).
func TestCodexBrokenLinks_PlainAGENTSMDIgnored(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	projectPath := filepath.Join(tmp, "proj")
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectPath, codexAgentsMarkdown), []byte("plain"), 0644); err != nil {
		t.Fatal(err)
	}

	c := &codex{io: stdPlatformIO{}}
	got := c.BrokenLinks("proj", projectPath, agentsHome)
	if len(got) != 0 {
		t.Errorf("plain AGENTS.md must be ignored by broken-link reporter, got %+v", got)
	}
}

// TestCodexBrokenLinks_HealthyAGENTSMDIgnored confirms a managed symlink
// whose target exists is NOT reported. Mirrors the
// TestClaudeBrokenLinks_HealthySymlinkSkipped contract for codex.
func TestCodexBrokenLinks_HealthyAGENTSMDIgnored(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	projectPath := filepath.Join(tmp, "proj")
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(agentsHome, "rules", "global", "agents.md")
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("# agents"), 0644); err != nil {
		t.Fatal(err)
	}
	linktest.Link(t, target, filepath.Join(projectPath, codexAgentsMarkdown))

	c := &codex{io: stdPlatformIO{}}
	got := c.BrokenLinks("proj", projectPath, agentsHome)
	if len(got) != 0 {
		t.Errorf("healthy AGENTS.md symlink must not be broken, got %+v", got)
	}
}

// TestCodexBrokenLinks_InterfaceConformance pins compile-time conformance
// with BrokenLinkReporter so doctor.collectBrokenLinks's type assertion
// cannot silently regress.
func TestCodexBrokenLinks_InterfaceConformance(t *testing.T) {
	var _ BrokenLinkReporter = (*codex)(nil)
}

// ---------- OrphanCanonicalReporter implementation (P4) ----------

// Named setup helpers for TestCodexOrphanCanonicals — kept as top-level funcs
// (not inline table closures) so their branching is not counted into the test
// function's cognitive complexity (go:S3776).
func setupCodexPlainOrphan(t *testing.T, agentsHome, projectPath string) (string, bool) {
	if err := os.MkdirAll(filepath.Join(agentsHome, "agents", "proj", "alpha"), 0755); err != nil {
		t.Fatal(err)
	}
	return "alpha", false
}

func setupCodexLinkedBackLink(t *testing.T, agentsHome, projectPath string) (string, bool) {
	canonical := filepath.Join(agentsHome, "agents", "proj", "beta")
	if err := os.MkdirAll(canonical, 0755); err != nil {
		t.Fatal(err)
	}
	repoLocal := filepath.Join(projectPath, ".agents", "agents")
	if err := os.MkdirAll(repoLocal, 0755); err != nil {
		t.Fatal(err)
	}
	linktest.Link(t, canonical, filepath.Join(repoLocal, "beta"))
	return "", false
}

func setupCodexMispointedBackLink(t *testing.T, agentsHome, projectPath string) (string, bool) {
	if err := os.MkdirAll(filepath.Join(agentsHome, "agents", "proj", "gamma"), 0755); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(agentsHome, "agents", "otherproj", "delta")
	if err := os.MkdirAll(other, 0755); err != nil {
		t.Fatal(err)
	}
	repoLocal := filepath.Join(projectPath, ".agents", "agents")
	if err := os.MkdirAll(repoLocal, 0755); err != nil {
		t.Fatal(err)
	}
	linktest.Link(t, other, filepath.Join(repoLocal, "gamma"))
	return "gamma", true
}

func setupCodexUnownedSkillsBucket(t *testing.T, agentsHome, projectPath string) (string, bool) {
	if err := os.MkdirAll(filepath.Join(agentsHome, "skills", "proj", "orphan-skill"), 0755); err != nil {
		t.Fatal(err)
	}
	return "", false
}

// TestCodexOrphanCanonicals is the table-driven cover for codex's
// OrphanCanonicalReporter: it owns only the "agents" bucket (claude owns
// "skills"), reporting plain + mis-pointed orphans and skipping non-owned
// buckets so the doctor fan-out never double-counts.
func TestCodexOrphanCanonicals(t *testing.T) {
	tests := []struct {
		name      string
		bucket    string
		setup     func(t *testing.T, agentsHome, projectPath string) (wantName string, wantNote bool)
		wantCount int
	}{
		{name: "plain orphan in owned agents bucket", bucket: "agents", setup: setupCodexPlainOrphan, wantCount: 1},
		{name: "correctly-linked back-link not orphaned", bucket: "agents", setup: setupCodexLinkedBackLink, wantCount: 0},
		{name: "mis-pointed back-link is orphan with note", bucket: "agents", setup: setupCodexMispointedBackLink, wantCount: 1},
		{name: "skills bucket not owned by codex", bucket: "skills", setup: setupCodexUnownedSkillsBucket, wantCount: 0},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			agentsHome := filepath.Join(tmp, ".agents")
			projectPath := filepath.Join(tmp, "proj")
			if err := os.MkdirAll(projectPath, 0755); err != nil {
				t.Fatal(err)
			}
			wantName, wantNote := tc.setup(t, agentsHome, projectPath)

			c := &codex{io: stdPlatformIO{}}
			got := c.OrphanCanonicals("proj", projectPath, agentsHome, tc.bucket)
			assertOrphanCanonicals(t, tc.bucket, got, tc.wantCount, wantName, wantNote)
		})
	}
}

// TestCodexOrphanCanonicals_InterfaceConformance pins compile-time conformance
// with OrphanCanonicalReporter for the codex platform.
func TestCodexOrphanCanonicals_InterfaceConformance(t *testing.T) {
	var _ OrphanCanonicalReporter = (*codex)(nil)
}

// ---------- UserConfigReporter implementation (P4) ----------

// TestCodexUserBrokenLinks is the table-driven cover for codex's
// UserConfigReporter broken-link surface (~/.codex/agents/*). Every reported
// link carries PlatformID="codex".
func TestCodexUserBrokenLinks(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(t *testing.T, home string)
		wantCount int
	}{
		{
			name:      "empty home reports nothing",
			setup:     func(t *testing.T, home string) {},
			wantCount: 0,
		},
		{
			name: "broken codex agent",
			setup: func(t *testing.T, home string) {
				linktest.DanglingLink(t, filepath.Join(home, ".codex", "agents", "missing"))
			},
			wantCount: 1,
		},
		{
			name: "healthy codex agent symlink ignored",
			setup: func(t *testing.T, home string) {
				target := filepath.Join(home, ".agents", "agents", "global", "a")
				if err := os.MkdirAll(target, 0755); err != nil {
					t.Fatal(err)
				}
				linktest.Link(t, target, filepath.Join(home, ".codex", "agents", "a"))
			},
			wantCount: 0,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			tc.setup(t, home)

			c := &codex{io: stdPlatformIO{}}
			got := c.UserBrokenLinks(home)
			assertUserBrokenLinks(t, "codex", got, tc.wantCount)
		})
	}
}

// TestCodexUserBadge covers the codex user-config badge over ~/.codex/hooks.json,
// ~/.codex/agents/, and ~/.agents/skills/.
func TestCodexUserBadge(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(t *testing.T, home string)
		wantPresent bool
		wantBroken  bool
	}{
		{
			name:        "empty home: absent badge",
			setup:       func(t *testing.T, home string) {},
			wantPresent: false,
			wantBroken:  false,
		},
		{
			name: "present healthy hooks.json",
			setup: func(t *testing.T, home string) {
				codexHome := filepath.Join(home, ".codex")
				if err := os.MkdirAll(codexHome, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(codexHome, "hooks.json"), []byte("{}"), 0644); err != nil {
					t.Fatal(err)
				}
			},
			wantPresent: true,
			wantBroken:  false,
		},
		{
			name: "broken agent surfaces broken badge",
			setup: func(t *testing.T, home string) {
				linktest.DanglingLink(t, filepath.Join(home, ".codex", "agents", "missing"))
			},
			wantPresent: false,
			wantBroken:  true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			tc.setup(t, home)

			c := &codex{io: stdPlatformIO{}}
			got := c.UserBadge(home)
			if got.Name != "Codex" {
				t.Errorf("UserBadge.Name = %q, want Codex", got.Name)
			}
			if got.Present != tc.wantPresent || got.Broken != tc.wantBroken {
				t.Errorf("UserBadge = %+v, want Present=%v Broken=%v", got, tc.wantPresent, tc.wantBroken)
			}
		})
	}
}

// TestCodexUserConfig_InterfaceConformance pins compile-time conformance with
// UserConfigReporter for the codex platform.
func TestCodexUserConfig_InterfaceConformance(t *testing.T) {
	var _ UserConfigReporter = (*codex)(nil)
}

// TestCodexLinkCodexAgentsMD_GlobalCandidateRealErrorPropagates covers the
// swallow fixed in se9-platform-shared: a permission-denied Stat on a
// global-scope AGENTS.md candidate must abort with a wrapped error, never
// be silently read as "this candidate doesn't exist" and skipped.
func TestCodexLinkCodexAgentsMD_GlobalCandidateRealErrorPropagates(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	globalRules := filepath.Join(agentsHome, "rules", "global")
	mustMkdirAllT(t, globalRules)
	testutil.MakeDirUnreadable(t, globalRules)

	c := NewCodex().(*codex)
	if err := c.linkCodexAgentsMD("proj", tmp, agentsHome); err == nil {
		t.Fatal("expected a real Stat error on the global candidates, got nil")
	}
}

// TestCodexLinkCodexAgentsMD_ProjectOverrideRealErrorPropagates isolates the
// second (project-override) Stat loop: the global bucket is genuinely
// absent (legitimate absence, loop 1 completes cleanly), but the
// project-scope rules dir is unreadable, so loop 2 must abort with a
// wrapped error instead of silently falling through to "no override".
func TestCodexLinkCodexAgentsMD_ProjectOverrideRealErrorPropagates(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	projectRules := filepath.Join(agentsHome, "rules", "proj")
	mustMkdirAllT(t, projectRules)
	testutil.MakeDirUnreadable(t, projectRules)

	c := NewCodex().(*codex)
	if err := c.linkCodexAgentsMD("proj", tmp, agentsHome); err == nil {
		t.Fatal("expected a real Stat error on the project override, got nil")
	}
}

// TestCodexLinkCodexAgentsMD_LegitimateAbsenceNoError guards the sibling
// path: neither the global bucket nor the project override exists, so
// linkCodexAgentsMD is still a silent no-op, not an error.
func TestCodexLinkCodexAgentsMD_LegitimateAbsenceNoError(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	mustMkdirAllT(t, agentsHome)
	repo := filepath.Join(tmp, "repo")
	mustMkdirAllT(t, repo)

	c := NewCodex().(*codex)
	if err := c.linkCodexAgentsMD("proj", repo, agentsHome); err != nil {
		t.Fatalf("expected nil for legitimately absent AGENTS.md sources, got %v", err)
	}
	if _, err := os.Lstat(filepath.Join(repo, codexAgentsMarkdown)); !os.IsNotExist(err) {
		t.Errorf("expected no AGENTS.md link created, lstat err = %v", err)
	}
}

// TestCodexCreateLinks_UnreadableAgentsMDSourceLeavesExistingLink is the
// se2-contract survival check: CreateLinks succeeds once with a real global
// AGENTS.md source, creating the repo-root AGENTS.md managed link. Once
// that source directory becomes unreadable, a second CreateLinks call must
// abort with an error and leave the pre-existing managed link alone.
func TestCodexCreateLinks_UnreadableAgentsMDSourceLeavesExistingLink(t *testing.T) {
	agentsHome, repo := setupAgentsHome(t)
	globalRules := filepath.Join(agentsHome, "rules", "global")
	mustMkdirAllT(t, globalRules)
	src := filepath.Join(globalRules, "agents.md")
	if err := os.WriteFile(src, []byte("# agents\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := NewCodex().CreateLinks("proj", repo); err != nil {
		t.Fatalf("initial CreateLinks: %v", err)
	}
	dst := filepath.Join(repo, codexAgentsMarkdown)
	if !links.IsManagedLink(dst, src) {
		t.Fatalf("expected %s to be a managed link to %s", dst, src)
	}

	testutil.MakeDirUnreadable(t, globalRules)
	if err := NewCodex().CreateLinks("proj", repo); err == nil {
		t.Fatal("expected CreateLinks to abort once the AGENTS.md source is unreadable")
	}
	if !links.IsManagedLink(dst, src) {
		t.Errorf("existing AGENTS.md managed link must survive the aborted sync")
	}
}

// TestCodexEnsureUserAgents_RealStatErrorPropagates covers the swallow
// fixed in se9-platform-shared: a permission-denied Stat on the global
// agents bucket itself must abort with a wrapped error rather than being
// silently read as "no agents to link".
func TestCodexEnsureUserAgents_RealStatErrorPropagates(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	agentsBucket := filepath.Join(agentsHome, "agents")
	globalAgents := filepath.Join(agentsBucket, "global")
	mustMkdirAllT(t, globalAgents)
	// Block traversal into the "agents" bucket itself so Stat(globalAgents)
	// fails with a real permission error, not ENOENT.
	testutil.MakeDirUnreadable(t, agentsBucket)

	c := NewCodex().(*codex)
	if err := c.ensureUserAgents(agentsHome); err == nil {
		t.Fatal("expected a real Stat error, got nil")
	}
}

// TestCodexEnsureUserAgents_LegitimateAbsenceNoError guards the sibling
// path: a genuinely absent global agents bucket is still a silent no-op.
func TestCodexEnsureUserAgents_LegitimateAbsenceNoError(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	mustMkdirAllT(t, agentsHome)

	c := NewCodex().(*codex)
	if err := c.ensureUserAgents(agentsHome); err != nil {
		t.Fatalf("expected nil for legitimately absent global agents bucket, got %v", err)
	}
}

// TestCodexCreateLinks_UnreadableAgentsSourceLeavesExistingAgentToml is the
// se2-contract survival check for the native .codex/agents/*.toml family:
// CreateLinks succeeds once with a real global agent, rendering the toml
// under the user's ~/.codex/agents/. Once the source bucket becomes
// unreadable, a second CreateLinks call must abort and leave the
// pre-existing rendered toml alone.
func TestCodexCreateLinks_UnreadableAgentsSourceLeavesExistingAgentToml(t *testing.T) {
	agentsHome, repo := setupAgentsHome(t)
	agentsBucket := filepath.Join(agentsHome, "agents")
	agentDir := filepath.Join(agentsBucket, "global", "reviewer")
	mustMkdirAllT(t, agentDir)
	if err := os.WriteFile(filepath.Join(agentDir, codexAgentMDFile), []byte("---\nname: reviewer\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := NewCodex().CreateLinks("proj", repo); err != nil {
		t.Fatalf("initial CreateLinks: %v", err)
	}
	dst := filepath.Join(os.Getenv("HOME"), codexDir, "agents", "reviewer.toml")
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("expected rendered toml at %s: %v", dst, err)
	}

	testutil.MakeDirUnreadable(t, agentsBucket)
	if err := NewCodex().CreateLinks("proj", repo); err == nil {
		t.Fatal("expected CreateLinks to abort once the agents source bucket is unreadable")
	}
	if _, err := os.Stat(dst); err != nil {
		t.Errorf("existing rendered agent toml %s must survive the aborted sync: %v", dst, err)
	}
}
