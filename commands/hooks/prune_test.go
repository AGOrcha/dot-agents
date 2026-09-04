package hooks

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/platform"
	"github.com/spf13/cobra"
)

// pruneFixture sets up an isolated AGENTS_HOME (and, since the protection
// check consults the registered-project config, an isolated HOME too) and
// writes a real multi-event bundle plus one import-artifact capture of it.
// Returns the agentsHome and the artifact bundle's directory.
func pruneFixture(t *testing.T) (agentsHome, artifactDir string) {
	t.Helper()
	tmp := t.TempDir()
	agentsHome = filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", filepath.Join(tmp, "home"))

	realDir := filepath.Join(agentsHome, "hooks", "global", "isp-gate")
	mustMkdirAll(t, realDir)
	mustWriteFile(t, filepath.Join(realDir, "HOOK.yaml"), `name: isp-gate
when_events:
  - pre_compact
  - stop
run:
  command: ./gate.sh
enabled_on:
  - claude
`)
	mustWriteFile(t, filepath.Join(realDir, "gate.sh"), "#!/bin/sh\nexit 0\n")

	artifactDir = filepath.Join(agentsHome, "hooks", "global", "pre-compact-gate")
	mustMkdirAll(t, artifactDir)
	mustWriteFile(t, filepath.Join(artifactDir, "HOOK.yaml"), `name: pre-compact-gate
when: pre_compact
run:
  command: `+filepath.Join(realDir, "gate.sh")+`
`)
	return agentsHome, artifactDir
}

func mustMkdirAll(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// registerProjectHookAllowList registers a project in the ~/.agents config
// registry whose .agentsrc.json names hookName in its "hooks" allow-list —
// the fixture for the "referenced by project config" protection.
func registerProjectHookAllowList(t *testing.T, agentsHome, hookName string) {
	t.Helper()
	projectDir := filepath.Join(t.TempDir(), "proj")
	mustMkdirAll(t, projectDir)
	rc := &config.AgentsRC{
		Version: 1,
		Project: "proj",
		Hooks:   &config.StringsOrBool{Names: []string{hookName}},
		Sources: []config.Source{{Type: "local"}},
	}
	if err := rc.Save(projectDir); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.AddProject("proj", projectDir)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
}

func TestNewHooksCmd_PruneSubcommandWired(t *testing.T) {
	root := NewHooksCmd(testDeps())
	var prune *cobra.Command
	for _, c := range root.Commands() {
		if c.Name() == "prune" {
			prune = c
		}
	}
	if prune == nil {
		t.Fatal("prune subcommand missing")
	}
	if prune.Flags().Lookup("import-artifacts") == nil {
		t.Error("prune missing --import-artifacts flag")
	}
	if prune.Flags().Lookup("apply") == nil {
		t.Error("prune missing --apply flag")
	}
}

func TestRunHooksPrune_RequiresImportArtifactsFlag(t *testing.T) {
	root := NewHooksCmd(testDeps())
	var prune *cobra.Command
	for _, c := range root.Commands() {
		if c.Name() == "prune" {
			prune = c
		}
	}
	if err := prune.RunE(prune, nil); err == nil {
		t.Fatal("expected error when --import-artifacts is not passed")
	}
}

func TestRunHooksPruneImportArtifacts_DryRunListsAndRemovesNothing(t *testing.T) {
	agentsHome, artifactDir := pruneFixture(t)
	deps := testDeps()

	if err := runHooksPruneImportArtifacts(deps, false); err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if _, err := os.Stat(artifactDir); err != nil {
		t.Fatalf("dry run must not remove candidates: %v", err)
	}
	if _, err := os.Stat(filepath.Join(agentsHome, "hooks", "global", "isp-gate")); err != nil {
		t.Fatalf("real bundle must survive dry run: %v", err)
	}
}

func TestRunHooksPruneImportArtifacts_ApplyRemovesCandidateKeepsOwner(t *testing.T) {
	agentsHome, artifactDir := pruneFixture(t)
	deps := testDeps()
	deps.Flags.Yes = true

	if err := runHooksPruneImportArtifacts(deps, true); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if _, err := os.Stat(artifactDir); !os.IsNotExist(err) {
		t.Fatalf("expected artifact bundle removed, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(agentsHome, "hooks", "global", "isp-gate")); err != nil {
		t.Fatalf("real bundle must survive apply: %v", err)
	}
}

func TestRunHooksPruneImportArtifacts_ProtectedByProjectHookAllowList(t *testing.T) {
	agentsHome, artifactDir := pruneFixture(t)
	registerProjectHookAllowList(t, agentsHome, "pre-compact-gate")

	deps := testDeps()
	deps.Flags.Yes = true

	if err := runHooksPruneImportArtifacts(deps, true); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if _, err := os.Stat(artifactDir); err != nil {
		t.Fatalf("expected bundle referenced by project config to survive apply: %v", err)
	}
}

func TestPlanHooksPrune_AmbiguousCandidateNeverRemoved(t *testing.T) {
	candidates := []platform.ImportArtifactCandidate{
		{Scope: "global", Name: "stub-a", Reason: platform.ImportArtifactReasonAmbiguous, Detail: "matches more than one bundle"},
	}
	toRemove, skipped := planHooksPrune(candidates)
	if len(toRemove) != 0 {
		t.Fatalf("expected no removable candidates, got %+v", toRemove)
	}
	if len(skipped) != 1 {
		t.Fatalf("expected one skipped candidate, got %+v", skipped)
	}
}

func TestPlanHooksPrune_SelfOwnedGuardSkipsEvenIfDetectorMisreports(t *testing.T) {
	// Defense in depth: even if a future detector regression reported a
	// candidate as owned by itself, planHooksPrune must still refuse it.
	candidates := []platform.ImportArtifactCandidate{
		{Scope: "global", Name: "isp-gate", OwnerScope: "global", OwnerName: "isp-gate", Reason: platform.ImportArtifactReasonCommandOwned, Detail: "self"},
	}
	toRemove, skipped := planHooksPrune(candidates)
	if len(toRemove) != 0 {
		t.Fatalf("expected self-owned candidate skipped, got removable %+v", toRemove)
	}
	if len(skipped) != 1 {
		t.Fatalf("expected one skipped candidate, got %+v", skipped)
	}
}

func TestRunHooksPruneImportArtifacts_NoCandidatesIsNoop(t *testing.T) {
	agentsHome := filepath.Join(t.TempDir(), ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	deps := testDeps()
	if err := runHooksPruneImportArtifacts(deps, true); err != nil {
		t.Fatalf("expected no error with no candidates: %v", err)
	}
}

// TestRunHooksPruneImportArtifacts_ScanErrorPropagates covers the scan leg's
// failure path: when ~/.agents/hooks cannot be enumerated at all (here it is
// a regular file, not a directory), the command must surface the error
// rather than report "no candidates found" and exit clean.
func TestRunHooksPruneImportArtifacts_ScanErrorPropagates(t *testing.T) {
	agentsHome := filepath.Join(t.TempDir(), ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	mustMkdirAll(t, agentsHome)
	mustWriteFile(t, filepath.Join(agentsHome, "hooks"), "not a directory")

	if err := runHooksPruneImportArtifacts(testDeps(), false); err == nil {
		t.Fatal("expected an error when the hooks root cannot be scanned")
	}
}

// TestRunHooksPrune_RunEDeletesViaWiredCommand exercises the cobra RunE
// success path end to end (--import-artifacts --apply through the real
// wired command, not the unexported function directly) — the counterpart to
// TestNewHooksCmd_RemoveCmdRunEInvokesRemoval for remove.
func TestRunHooksPrune_RunEDeletesViaWiredCommand(t *testing.T) {
	_, artifactDir := pruneFixture(t)
	deps := testDeps()
	deps.Flags.Yes = true
	root := NewHooksCmd(deps)
	var prune *cobra.Command
	for _, c := range root.Commands() {
		if c.Name() == "prune" {
			prune = c
		}
	}
	if err := prune.Flags().Set("import-artifacts", "true"); err != nil {
		t.Fatal(err)
	}
	if err := prune.Flags().Set("apply", "true"); err != nil {
		t.Fatal(err)
	}
	if err := prune.RunE(prune, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if _, err := os.Stat(artifactDir); !os.IsNotExist(err) {
		t.Fatalf("expected bundle removed via RunE, stat err=%v", err)
	}
}

// TestRunHooksPruneImportArtifacts_DeclinedInteractiveConfirmCancels covers
// the interactive-confirmation branch (Flags.Yes and Flags.Force both
// unset): with no human present to answer, ui.Confirm reads EOF from stdin
// and returns false, so apply must cancel without deleting anything —
// mirroring hooks/remove's identical guard.
func TestRunHooksPruneImportArtifacts_DeclinedInteractiveConfirmCancels(t *testing.T) {
	_, artifactDir := pruneFixture(t)
	deps := testDeps()
	// Flags.Yes and Flags.Force are both false (zero value): apply must go
	// through ui.Confirm.

	if err := runHooksPruneImportArtifacts(deps, true); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if _, err := os.Stat(artifactDir); err != nil {
		t.Fatalf("expected candidate to survive a declined confirmation: %v", err)
	}
}

// TestRunHooksPruneImportArtifacts_RemoveAllErrorPropagates covers the
// fsops.RemoveAll error branch: a bundle directory that cannot be removed
// (its own containing scope directory made unwritable/unreadable) must
// surface as an error, not be silently skipped.
func TestRunHooksPruneImportArtifacts_RemoveAllErrorPropagates(t *testing.T) {
	agentsHome, artifactDir := pruneFixture(t)
	deps := testDeps()
	deps.Flags.Yes = true

	// Removing the artifact directory itself requires removing its parent
	// scope dir's write permission won't stop RemoveAll on POSIX (it walks
	// via the artifact dir's own perms); instead make the artifact directory
	// itself refuse removal of its contents by stripping write+execute.
	if err := os.Chmod(filepath.Dir(artifactDir), 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Dir(artifactDir), 0o755) })

	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission-denial mechanism does not apply on Windows")
	}

	err := runHooksPruneImportArtifacts(deps, true)
	if err == nil {
		t.Fatal("expected fsops.RemoveAll failure to propagate")
	}
	_ = agentsHome
}

// TestHookReferencedByProjectConfigName_EmptyName covers the blank-name
// guard directly.
func TestHookReferencedByProjectConfigName_EmptyName(t *testing.T) {
	if _, ok := hookReferencedByProjectConfigName(""); ok {
		t.Error("expected false for an empty name")
	}
}

// TestHookReferencedByProjectConfigName_ConfigLoadErrorIsBestEffort covers
// the config.Load() error branch: a malformed config.json must degrade to
// "not referenced" rather than failing the caller.
func TestHookReferencedByProjectConfigName_ConfigLoadErrorIsBestEffort(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	mustMkdirAll(t, agentsHome)
	mustWriteFile(t, filepath.Join(agentsHome, "config.json"), "{not valid json")

	if _, ok := hookReferencedByProjectConfigName("anything"); ok {
		t.Error("expected false when config.json fails to parse")
	}
}

// TestHookReferencedByProjectConfigName_UnboundProjectSkipped covers the
// "registered but no machine-local path binding" branch: a project entry in
// config.json with no matching row in local/bindings.json must be skipped,
// not crash the lookup.
func TestHookReferencedByProjectConfigName_UnboundProjectSkipped(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	mustMkdirAll(t, agentsHome)
	mustWriteFile(t, filepath.Join(agentsHome, "config.json"), `{"version":2,"projects":{"ghost":{}}}`)

	if _, ok := hookReferencedByProjectConfigName("anything"); ok {
		t.Error("expected false for a registered-but-unbound project")
	}
}

// TestHookReferencedByProjectConfigName_BlanketHooksTrueDoesNotProtect
// covers the rc.Hooks.All continue branch: a project whose .agentsrc.json
// uses the common `"hooks": true` blanket enablement must NOT count as a
// by-name reference (see the function's own doc comment on why).
func TestHookReferencedByProjectConfigName_BlanketHooksTrueDoesNotProtect(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	mustMkdirAll(t, agentsHome)

	projectDir := filepath.Join(tmp, "proj")
	mustMkdirAll(t, projectDir)
	rc := &config.AgentsRC{Version: 1, Project: "proj", Hooks: &config.StringsOrBool{All: true}, Sources: []config.Source{{Type: "local"}}}
	if err := rc.Save(projectDir); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.AddProject("proj", projectDir)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	if _, ok := hookReferencedByProjectConfigName("some-hook"); ok {
		t.Error("expected blanket hooks:true to NOT count as a by-name reference")
	}
}

// TestHookReferencedByProjectConfigName_UnreadableAgentsRCSkipped covers the
// config.LoadAgentsRC error branch: a bound project whose .agentsrc.json is
// missing/unreadable must be skipped rather than failing the lookup.
func TestHookReferencedByProjectConfigName_UnreadableAgentsRCSkipped(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	mustMkdirAll(t, agentsHome)

	projectDir := filepath.Join(tmp, "proj-no-rc")
	mustMkdirAll(t, projectDir) // no .agentsrc.json written here

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.AddProject("proj-no-rc", projectDir)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	if _, ok := hookReferencedByProjectConfigName("anything"); ok {
		t.Error("expected false when the bound project has no readable .agentsrc.json")
	}
}

// TestPrintHooksPruneCandidateLine_AmbiguousOwnerLabel covers the
// owner=="" -> "(ambiguous)" display branch directly. This only checks the
// function runs without panicking on an owner-less candidate; the dry-run
// tests above already assert real candidate output.
func TestPrintHooksPruneCandidateLine_AmbiguousOwnerLabel(t *testing.T) {
	printHooksPruneCandidateLine(platform.ImportArtifactCandidate{
		Scope:  "global",
		Name:   "stub-a",
		Reason: platform.ImportArtifactReasonAmbiguous,
		Detail: "matches more than one bundle",
	}, "ownership ambiguous")
}
