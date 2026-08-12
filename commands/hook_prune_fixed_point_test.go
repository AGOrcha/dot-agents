package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AGOrcha/dot-agents/commands/hooks"
	"github.com/spf13/cobra"
)

// This file pins the invariant `da hooks prune --import-artifacts` exists to
// restore: pruning the junk bundles the pre-#533 render/import feedback loop
// already left on disk must not reopen the loop. After a prune --apply, a
// refresh -> import -> refresh cycle must be a fixed point (nothing new is
// created, the rendered config is byte-stable) exactly like it already is
// for the real bundle the artifacts were captured from.

// prunePlusImportFixture writes a real multi-event bundle plus two synthetic
// import-artifact bundles captured from ITS render output — the exact shape
// #533's PR description measured in production (pre-compact-gate, stop-gate,
// each a manifest whose run.command is an absolute path into the real
// bundle's directory). Unlike the #533 fixed-point tests, these are written
// directly to disk rather than produced by running import, because import no
// longer creates this shape — prune exists to clean up ones that already
// exist from before that fix landed.
func prunePlusImportFixture(t *testing.T) (home, agentsHome string) {
	t.Helper()
	home, agentsHome = hookFixtureHome(t)

	writeHookBundle(t, agentsHome, "global", "isp-gate", `name: isp-gate
when_events:
  - pre_compact
  - stop
run:
  command: ./gate.sh
enabled_on:
  - claude
`)
	realScript := filepath.Join(agentsHome, "hooks", "global", "isp-gate", "gate.sh")

	// One artifact per event isp-gate renders under — the real production
	// shape: a multi-event bundle's render produces a SEPARATE capture per
	// event, and every capture holds the identical already-resolved
	// absolute command (isp-gate's gate.sh), differing only in `when`.
	for artifact, when := range map[string]string{
		"pre-compact-gate": "pre_compact",
		"stop-gate":        "stop",
	} {
		writeSyntheticImportArtifact(t, agentsHome, artifact, when, realScript)
	}
	return home, agentsHome
}

// writeSyntheticImportArtifact writes a bundle whose manifest command is an
// absolute path into targetScript — the shape a captured render leaves
// behind, without going through the (now provenance-protected) import path.
func writeSyntheticImportArtifact(t *testing.T, agentsHome, name, when, targetScript string) {
	t.Helper()
	dir := filepath.Join(agentsHome, "hooks", "global", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "name: " + name + "\nwhen: " + when + "\nrun:\n  command: " + targetScript + "\n"
	if err := os.WriteFile(filepath.Join(dir, "HOOK.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
}

// pruneCmd resolves the wired `da hooks prune` cobra command from the
// production command tree (NewHooksCmd(), the same entrypoint `da` itself
// runs), so the test exercises real wiring rather than calling the
// unexported implementation function directly.
func pruneCmd(t *testing.T) *cobra.Command {
	t.Helper()
	root := NewHooksCmd()
	for _, c := range root.Commands() {
		if c.Name() == "prune" {
			return c
		}
	}
	t.Fatal("prune subcommand missing from NewHooksCmd()")
	return nil
}

// TestHooksPruneImportArtifacts_PostPruneRefreshImportIsFixedPoint is the
// headline regression for this feature: pruning the artifacts left over from
// before #533 must not create an opening for the loop to restart. A full
// refresh -> import -> refresh cycle after --apply must add no new bundles
// and re-render byte-identical config, exactly like the real bundle already
// does on its own.
func TestHooksPruneImportArtifacts_PostPruneRefreshImportIsFixedPoint(t *testing.T) {
	home, agentsHome := prunePlusImportFixture(t)
	repo := t.TempDir()

	before := hookBundleNames(t, agentsHome, "global")
	wantBefore := []string{"isp-gate", "pre-compact-gate", "stop-gate"}
	if !equalStrings(before, wantBefore) {
		t.Fatalf("fixture setup: got bundles %v, want %v", before, wantBefore)
	}

	cmd := pruneCmd(t)
	if err := cmd.Flags().Set("import-artifacts", "true"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("apply", "true"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("hooks prune --import-artifacts --apply: %v", err)
	}

	afterPrune := hookBundleNames(t, agentsHome, "global")
	want := []string{"isp-gate"}
	if !equalStrings(afterPrune, want) {
		t.Fatalf("prune did not converge to the real bundle: got %v, want %v", afterPrune, want)
	}

	settings := filepath.Join(home, ".claude", "settings.json")
	renderHooks(t, "claude", repo)
	firstRender := readFileOrEmpty(t, settings)
	if firstRender == "" {
		t.Fatal("post-prune refresh rendered no ~/.claude/settings.json")
	}

	for cycle := 1; cycle <= 2; cycle++ {
		importGlobal(t)
		got := hookBundleNames(t, agentsHome, "global")
		if !equalStrings(got, want) {
			t.Fatalf("cycle %d: post-prune import recreated artifacts: got %v, want %v", cycle, got, want)
		}
		renderHooks(t, "claude", repo)
		if render := readFileOrEmpty(t, settings); render != firstRender {
			t.Fatalf("cycle %d: post-prune render not byte-stable:\nfirst:\n%s\nnow:\n%s", cycle, firstRender, render)
		}
	}
}

// TestHooksPruneImportArtifacts_DryRunDefaultLeavesArtifactsForNextImport
// covers the other half: WITHOUT --apply, the artifacts survive (dry-run
// changes nothing), so a subsequent refresh/import cycle behaves exactly as
// it did before prune ever ran — dry-run must be a true no-op.
func TestHooksPruneImportArtifacts_DryRunDefaultLeavesArtifactsForNextImport(t *testing.T) {
	_, agentsHome := prunePlusImportFixture(t)

	cmd := pruneCmd(t)
	if err := cmd.Flags().Set("import-artifacts", "true"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("hooks prune --import-artifacts (dry run): %v", err)
	}

	got := hookBundleNames(t, agentsHome, "global")
	want := []string{"isp-gate", "pre-compact-gate", "stop-gate"}
	if !equalStrings(got, want) {
		t.Fatalf("dry run must not remove anything: got %v, want %v", got, want)
	}
}

// hooksDepsSanityCheck pins that NewHooksCmd() wires the same hooks.Deps
// shape prune expects (Flags.Yes threaded from the package-level Flags),
// which is what lets the fixed-point test above run --apply without an
// interactive confirmation prompt.
func TestHooksDepsThreadsYesFlagForPrune(t *testing.T) {
	saved := Flags
	Flags = GlobalFlags{Yes: true}
	t.Cleanup(func() { Flags = saved })

	deps := hooksDeps()
	if deps.Flags != (hooks.GlobalFlags{Yes: true}) {
		t.Fatalf("hooksDeps() did not thread Flags.Yes: got %+v", deps.Flags)
	}
}
