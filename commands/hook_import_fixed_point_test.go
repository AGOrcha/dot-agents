package commands

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/platform"
)

// This file pins the invariant that closes the hook refresh/import feedback
// loop:
//
//	refresh → import → refresh is a FIXED POINT for any bundle name.
//
// `da refresh` RENDERS canonical bundles (~/.agents/hooks/<scope>/<name>/)
// into harness config; the import path CAPTURES hook entries found in
// harness config back into bundles. Before provenance existed, the only
// thing keeping the two from fighting was name coincidence: import names a
// captured entry from its canonical event plus command stem
// ("pre-compact-gate"), which almost never equals the name its author gave
// the bundle it was rendered from ("isp-gate"). Every cycle therefore
// re-captured da's own render output as a NEW bundle, and the
// non-destructive -N alternate naming turned that into unbounded growth.
//
// The tests below drive the real refresh render (platform.CreateLinks) and
// the real import pipeline (runImport) against a sandboxed HOME/AGENTS_HOME.

// hookFixtureHome sets up an isolated HOME + AGENTS_HOME and returns both.
func hookFixtureHome(t *testing.T) (home, agentsHome string) {
	t.Helper()
	home = t.TempDir()
	agentsHome = filepath.Join(home, ".agents")
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("AGENTS_HOME", agentsHome)

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	t.Cleanup(func() { Flags = saved })
	return home, agentsHome
}

// writeHookBundle writes a canonical bundle with a `./gate.sh` command, the
// shape every scaffolded bundle uses.
func writeHookBundle(t *testing.T, agentsHome, scope, name, manifest string) {
	t.Helper()
	dir := filepath.Join(agentsHome, "hooks", scope, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "HOOK.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gate.sh"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// hookBundleNames lists the bundle directories in one scope.
func hookBundleNames(t *testing.T, agentsHome, scope string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(agentsHome, "hooks", scope))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	names := []string{}
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// renderHooks runs the real refresh-time render for one platform.
func renderHooks(t *testing.T, platformID, repoPath string) {
	t.Helper()
	p := platform.ByID(platformID)
	if p == nil {
		t.Fatalf("unknown platform %q", platformID)
	}
	if err := p.CreateLinks("global", repoPath); err != nil {
		t.Fatalf("%s CreateLinks: %v", platformID, err)
	}
}

func importGlobal(t *testing.T) {
	t.Helper()
	if err := runImport("", importScopeGlobal, stdImportDeps{}); err != nil {
		t.Fatalf("runImport: %v", err)
	}
}

func readFileOrEmpty(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

// TestHookRefreshImportIsFixedPoint is the headline regression. A bundle
// whose author-chosen name shares nothing with the name import would derive
// from its rendered entries must survive two full refresh → import → refresh
// cycles with the bundle set unchanged and the rendered config byte-stable.
//
// Before the provenance fix, cycle 1's import minted pre-compact-gate and
// stop-gate from isp-gate's own render output; cycle 2 re-rendered those and
// minted more.
func TestHookRefreshImportIsFixedPoint(t *testing.T) {
	home, agentsHome := hookFixtureHome(t)
	repo := t.TempDir()

	// Multi-event bundle: renders under TWO Claude events, so no single
	// import-derived name could ever match it.
	writeHookBundle(t, agentsHome, "global", "isp-gate", `name: isp-gate
when_events:
  - pre_compact
  - stop
run:
  command: ./gate.sh
enabled_on:
  - claude
`)
	want := []string{"isp-gate"}

	settings := filepath.Join(home, ".claude", "settings.json")

	renderHooks(t, "claude", repo)
	firstRender := readFileOrEmpty(t, settings)
	if firstRender == "" {
		t.Fatal("refresh rendered no ~/.claude/settings.json; the fixture is not exercising the render path")
	}

	for cycle := 1; cycle <= 2; cycle++ {
		importGlobal(t)
		got := hookBundleNames(t, agentsHome, "global")
		if !equalStrings(got, want) {
			t.Fatalf("cycle %d: import created bundles from da's own render output: got %v, want %v", cycle, got, want)
		}

		renderHooks(t, "claude", repo)
		if render := readFileOrEmpty(t, settings); render != firstRender {
			t.Fatalf("cycle %d: render is not byte-stable\nfirst:\n%s\nnow:\n%s", cycle, firstRender, render)
		}
	}
}

// TestHookImportCapturesHandAuthoredEntryExactlyOnce is the other half of the
// contract: provenance must not turn import into a no-op. A genuinely
// hand-authored entry — one no bundle explains — is captured on the first
// import, and every import after that is a no-op because the bundle it
// created now explains it.
func TestHookImportCapturesHandAuthoredEntryExactlyOnce(t *testing.T) {
	home, agentsHome := hookFixtureHome(t)

	claudeDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	handAuthored := `{
  "hooks": {
    "Stop": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "/opt/tools/notify-done.sh"
          }
        ]
      }
    ]
  }
}
`
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(handAuthored), 0o644); err != nil {
		t.Fatal(err)
	}

	importGlobal(t)
	first := hookBundleNames(t, agentsHome, "global")
	if len(first) != 1 {
		t.Fatalf("hand-authored entry must be captured exactly once, got bundles %v", first)
	}

	// The captured bundle now explains the entry, so a second import over the
	// same untouched config adds nothing.
	importGlobal(t)
	second := hookBundleNames(t, agentsHome, "global")
	if !equalStrings(second, first) {
		t.Fatalf("import is not idempotent: %v then %v", first, second)
	}
}

// TestHookImportCapturesOneBundleForACommandInTwoHarnessFiles pins why the
// provenance index is rebuilt per source file: the bundle minted while
// processing the first candidate must be visible to the second. The same
// hand-authored command sitting in two harness config files is one hook, not
// two.
func TestHookImportCapturesOneBundleForACommandInTwoHarnessFiles(t *testing.T) {
	home, agentsHome := hookFixtureHome(t)

	claudeDir := filepath.Join(home, ".claude")
	codexDir := filepath.Join(home, ".codex")
	for _, dir := range []string{claudeDir, codexDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	const command = "/opt/tools/notify-done.sh"
	claudeSettings := `{"hooks":{"Stop":[{"matcher":"*","hooks":[{"type":"command","command":"` + command + `"}]}]}}`
	codexHooks := `{"hooks":{"Stop":[{"matcher":"","hooks":[{"type":"command","command":"` + command + `"}]}]}}`
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(claudeSettings), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "hooks.json"), []byte(codexHooks), 0o644); err != nil {
		t.Fatal(err)
	}

	importGlobal(t)
	got := hookBundleNames(t, agentsHome, "global")
	if len(got) != 1 {
		t.Fatalf("one command in two harness files must yield one bundle, got %v", got)
	}

	importGlobal(t)
	if second := hookBundleNames(t, agentsHome, "global"); !equalStrings(second, got) {
		t.Fatalf("import is not idempotent: %v then %v", got, second)
	}
}

// TestHookImportDoesNotDuplicateAcrossHarnesses covers the surface that
// blocked cross-harness parity: once a bundle is enabled for codex and
// cursor, refresh renders .codex/hooks.json and .cursor/hooks.json too, and
// each of those files is its own import source. Every extra harness used to
// be another mouth feeding the loop.
func TestHookImportDoesNotDuplicateAcrossHarnesses(t *testing.T) {
	home, agentsHome := hookFixtureHome(t)
	repo := t.TempDir()

	writeHookBundle(t, agentsHome, "global", "post-commit-checkpoint", `name: post-commit-checkpoint
when: post_tool_use
match:
  tools:
    - Bash
run:
  command: ./gate.sh
enabled_on:
  - claude
  - codex
  - cursor
`)
	want := []string{"post-commit-checkpoint"}

	for _, id := range []string{"claude", "codex", "cursor"} {
		renderHooks(t, id, repo)
	}
	codexHooks := readFileOrEmpty(t, filepath.Join(home, ".codex", "hooks.json"))
	cursorHooks := readFileOrEmpty(t, filepath.Join(home, ".cursor", "hooks.json"))
	if codexHooks == "" || cursorHooks == "" {
		t.Fatalf("expected codex and cursor renders; codex=%q cursor=%q", codexHooks, cursorHooks)
	}

	for cycle := 1; cycle <= 2; cycle++ {
		importGlobal(t)
		if got := hookBundleNames(t, agentsHome, "global"); !equalStrings(got, want) {
			t.Fatalf("cycle %d: codex/cursor renders were re-imported: got %v, want %v", cycle, got, want)
		}
		for _, id := range []string{"claude", "codex", "cursor"} {
			renderHooks(t, id, repo)
		}
		if got := readFileOrEmpty(t, filepath.Join(home, ".codex", "hooks.json")); got != codexHooks {
			t.Fatalf("cycle %d: codex render not byte-stable:\n%s", cycle, got)
		}
		if got := readFileOrEmpty(t, filepath.Join(home, ".cursor", "hooks.json")); got != cursorHooks {
			t.Fatalf("cycle %d: cursor render not byte-stable:\n%s", cycle, got)
		}
	}
}

// TestHookImportDoesNotGrowAlternateSuffixedBundles reproduces the measured
// production shape: several distinct bundles that all render under the SAME
// canonical event, so import derives the SAME base name for all of them and
// the non-destructive alternate naming appends -2, -3, … Growth here was
// unbounded (a real PreCompact series went 12 → 15 bundles in one cycle).
func TestHookImportDoesNotGrowAlternateSuffixedBundles(t *testing.T) {
	home, agentsHome := hookFixtureHome(t)
	repo := t.TempDir()

	want := []string{}
	for _, name := range []string{"isp-gate", "iteration-close-gate", "loop-worker-gate"} {
		writeHookBundle(t, agentsHome, "global", name, `name: `+name+`
when: pre_compact
run:
  command: ./gate.sh
enabled_on:
  - claude
`)
		want = append(want, name)
	}
	sort.Strings(want)

	settings := filepath.Join(home, ".claude", "settings.json")
	renderHooks(t, "claude", repo)
	firstRender := readFileOrEmpty(t, settings)

	for cycle := 1; cycle <= 2; cycle++ {
		importGlobal(t)
		got := hookBundleNames(t, agentsHome, "global")
		if !equalStrings(got, want) {
			t.Fatalf("cycle %d: alternate-suffixed bundles reappeared: got %v, want %v", cycle, got, want)
		}
		renderHooks(t, "claude", repo)
		if render := readFileOrEmpty(t, settings); render != firstRender {
			t.Fatalf("cycle %d: render not byte-stable:\n%s", cycle, render)
		}
	}
}

// TestDropBundleRenderedHookSpecs pins the filter itself, including the
// ordering guarantee that makes the surviving entries' names stable: a
// bundle-owned spec that sorts first must not push the hand-authored one
// into the -2 slot.
func TestDropBundleRenderedHookSpecs(t *testing.T) {
	agentsHome := t.TempDir()
	dir := filepath.Join(agentsHome, "hooks", "global", "isp-gate")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "HOOK.yaml"), []byte("name: isp-gate\nwhen: stop\nrun:\n  command: ./gate.sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	owned := filepath.Join(dir, "gate.sh")

	specs := []importedHookSpec{
		{when: "stop", command: owned, platform: "claude"},
		{when: "stop", command: "/opt/tools/gate.sh", platform: "claude"},
	}

	kept := dropBundleRenderedHookSpecs(agentsHome, specs)
	if len(kept) != 1 || kept[0].command != "/opt/tools/gate.sh" {
		t.Fatalf("expected only the hand-authored spec to survive, got %+v", kept)
	}

	outputs := buildCanonicalHookOutputs("global", kept)
	if len(outputs) != 1 {
		t.Fatalf("expected one output, got %d", len(outputs))
	}
	// Filtering before naming: the survivor takes the base name, not "-2".
	if got := outputs[0].destRel; got != "hooks/global/stop-gate/HOOK.yaml" {
		t.Fatalf("survivor must take the un-suffixed name, got %q", got)
	}

	// With no agents home nothing can claim ownership.
	if kept := dropBundleRenderedHookSpecs("", specs); len(kept) != 2 {
		t.Fatalf("empty agents home must keep every spec, got %+v", kept)
	}
}

// TestCanonicalHookOutputsForSpecs_AllOwnedStaysHandled pins the reason the
// all-filtered case must report handled=true: reporting "not a hook source"
// would drop the file through to the generic copy import, which for
// .github/hooks/*.json preserves the rendered file as a brand new legacy
// hook — the same duplication by another route.
func TestCanonicalHookOutputsForSpecs_AllOwnedStaysHandled(t *testing.T) {
	agentsHome := t.TempDir()
	dir := filepath.Join(agentsHome, "hooks", "global", "isp-gate")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "HOOK.yaml"), []byte("name: isp-gate\nwhen: stop\nrun:\n  command: ./gate.sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	outputs, handled, err := canonicalHookOutputsForSpecs("global", agentsHome, []importedHookSpec{
		{when: "stop", command: filepath.Join(dir, "gate.sh"), platform: "claude"},
	})
	if err != nil {
		t.Fatalf("canonicalHookOutputsForSpecs: %v", err)
	}
	if !handled {
		t.Fatal("a recognized hook source whose entries are all bundle-owned must stay handled")
	}
	if len(outputs) != 0 {
		t.Fatalf("expected no outputs, got %d", len(outputs))
	}

	if _, handled, _ := canonicalHookOutputsForSpecs("global", agentsHome, nil); handled {
		t.Fatal("a source that parsed to no hook specs at all is not a hook source")
	}
}
