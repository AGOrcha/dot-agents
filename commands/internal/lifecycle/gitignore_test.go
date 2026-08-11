package lifecycle

// Coverage for MaintainManagedGitignore — the knob-aware managed-.gitignore
// step install and refresh share (config-distribution-model §15 D14 / R8).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/platform"
)

const (
	managedBegin = "# >>> dot-agents managed (project outputs) >>>"
	managedEnd   = "# <<< dot-agents managed (project outputs) <<<"
)

// writeManifest writes a .agentsrc.json carrying the given knob state. A nil
// knob writes a manifest with the key absent (the default-on case).
func writeManifest(t *testing.T, dir string, knob *bool) {
	t.Helper()
	rc := config.AgentsRC{Version: 2, Project: "proj", GitignoreProjections: knob}
	data, err := json.Marshal(rc)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, config.AgentsRCFile), data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

// readProjectGitignore returns the project's .gitignore, or "" when absent.
func readProjectGitignore(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("read .gitignore: %v", err)
	}
	return string(data)
}

// claudeAndCodex is a stable two-platform projection set: both are static
// (staticManagedOutputs) so the expected entries do not depend on host state.
func claudeAndCodex(t *testing.T) []platform.Platform {
	t.Helper()
	var out []platform.Platform
	for _, id := range []string{"claude", "codex"} {
		p := platform.ByID(id)
		if p == nil {
			t.Fatalf("platform %q not registered", id)
		}
		out = append(out, p)
	}
	return out
}

func TestMaintainManagedGitignore_KnobStates(t *testing.T) {
	boolPtr := func(b bool) *bool { return &b }

	tests := []struct {
		name string
		// setupManifest writes (or deliberately omits) the manifest.
		setupManifest func(t *testing.T, dir string)
		wantBlock     bool
		wantMsg       string
	}{
		{
			name:          "no manifest defaults to writing the block",
			setupManifest: func(*testing.T, string) {},
			wantBlock:     true,
			wantMsg:       managedGitignoreWroteMsg,
		},
		{
			name:          "manifest without the key defaults to writing the block",
			setupManifest: func(t *testing.T, dir string) { writeManifest(t, dir, nil) },
			wantBlock:     true,
			wantMsg:       managedGitignoreWroteMsg,
		},
		{
			name:          "explicit true writes the block",
			setupManifest: func(t *testing.T, dir string) { writeManifest(t, dir, boolPtr(true)) },
			wantBlock:     true,
			wantMsg:       managedGitignoreWroteMsg,
		},
		{
			name:          "explicit false writes no block",
			setupManifest: func(t *testing.T, dir string) { writeManifest(t, dir, boolPtr(false)) },
			wantBlock:     false,
			wantMsg:       managedGitignoreRemovedMsg,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			tc.setupManifest(t, dir)

			msg, err := MaintainManagedGitignore(dir, claudeAndCodex(t))
			if err != nil {
				t.Fatalf("MaintainManagedGitignore: %v", err)
			}
			if msg != tc.wantMsg {
				t.Errorf("status line = %q, want %q", msg, tc.wantMsg)
			}

			got := readProjectGitignore(t, dir)
			hasBlock := strings.Contains(got, managedBegin)
			if hasBlock != tc.wantBlock {
				t.Fatalf("block present = %v, want %v:\n%s", hasBlock, tc.wantBlock, got)
			}
			if !tc.wantBlock {
				return
			}
			// The block must carry the projected outputs of the platforms it
			// was given, plus the unconditional backup/overlay entries.
			for _, want := range []string{".claude/", ".mcp.json", "AGENTS.md", ".codex/", "*.dot-agents-backup", ".agentsrc.local.json"} {
				if !strings.Contains(got, "\n"+want+"\n") {
					t.Errorf("managed block missing %q:\n%s", want, got)
				}
			}
			// The committed contract must stay tracked.
			for _, forbidden := range []string{".agentsrc.json", ".agentsrc.lock"} {
				if strings.Contains(got, "\n"+forbidden+"\n") {
					t.Errorf("%q must never be ignored:\n%s", forbidden, got)
				}
			}
		})
	}
}

func TestMaintainManagedGitignore_PreservesUserContentAndIsByteStable(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, nil)
	userContent := "# my ignores\nnode_modules/\ndist/\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(userContent), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := MaintainManagedGitignore(dir, claudeAndCodex(t)); err != nil {
		t.Fatalf("first run: %v", err)
	}
	first := readProjectGitignore(t, dir)
	if !strings.HasPrefix(first, userContent) {
		t.Errorf("user content must be preserved byte-for-byte at the head:\n%s", first)
	}

	if _, err := MaintainManagedGitignore(dir, claudeAndCodex(t)); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if second := readProjectGitignore(t, dir); second != first {
		t.Errorf("re-run must be byte-stable:\ngot  %q\nwant %q", second, first)
	}
}

func TestMaintainManagedGitignore_UpdatesInPlaceWhenProjectionSetChanges(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, nil)

	if _, err := MaintainManagedGitignore(dir, claudeAndCodex(t)); err != nil {
		t.Fatalf("wide run: %v", err)
	}
	wide := readProjectGitignore(t, dir)
	if !strings.Contains(wide, "\n.codex/\n") {
		t.Fatalf("expected codex outputs in the wide block:\n%s", wide)
	}

	// Drop codex from the projection set (a platform disabled between runs).
	if _, err := MaintainManagedGitignore(dir, []platform.Platform{platform.ByID("claude")}); err != nil {
		t.Fatalf("narrow run: %v", err)
	}
	narrow := readProjectGitignore(t, dir)
	if strings.Contains(narrow, "\n.codex/\n") {
		t.Errorf("codex outputs must be regenerated away, not left behind:\n%s", narrow)
	}
	if !strings.Contains(narrow, "\n.claude/\n") {
		t.Errorf("claude outputs must survive:\n%s", narrow)
	}
	if strings.Count(narrow, managedBegin) != 1 || strings.Count(narrow, managedEnd) != 1 {
		t.Errorf("exactly one managed block must remain (regenerated, not appended):\n%s", narrow)
	}
}

func TestMaintainManagedGitignore_OptOutRemovesAnExistingBlock(t *testing.T) {
	dir := t.TempDir()
	userContent := "node_modules/\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(userContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Install once with the knob on, then flip it off — the previously-written
	// block must be retracted, not merely left unrefreshed.
	writeManifest(t, dir, nil)
	if _, err := MaintainManagedGitignore(dir, claudeAndCodex(t)); err != nil {
		t.Fatalf("opt-in run: %v", err)
	}
	if !strings.Contains(readProjectGitignore(t, dir), managedBegin) {
		t.Fatal("precondition: expected a managed block after the opt-in run")
	}

	optOut := false
	writeManifest(t, dir, &optOut)
	msg, err := MaintainManagedGitignore(dir, claudeAndCodex(t))
	if err != nil {
		t.Fatalf("opt-out run: %v", err)
	}
	if msg != managedGitignoreRemovedMsg {
		t.Errorf("status line = %q, want %q", msg, managedGitignoreRemovedMsg)
	}
	got := readProjectGitignore(t, dir)
	if got != userContent {
		t.Errorf("only user content should remain:\ngot  %q\nwant %q", got, userContent)
	}
}

// TestMaintainManagedGitignore_IsPerProject pins that the step is keyed off the
// project path it is handed, not any ambient state — `da refresh` fans out
// across every registered project in one run, so each must get a block derived
// from its OWN manifest, and one project's opt-out must not leak to a sibling.
func TestMaintainManagedGitignore_IsPerProject(t *testing.T) {
	root := t.TempDir()
	optIn := filepath.Join(root, "opted-in")
	optOut := filepath.Join(root, "opted-out")
	noManifest := filepath.Join(root, "no-manifest")
	for _, d := range []string{optIn, optOut, noManifest} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeManifest(t, optIn, nil)
	disabled := false
	writeManifest(t, optOut, &disabled)

	// Seed the opted-out project with a block, as an earlier run would have.
	if _, err := MaintainManagedGitignore(optOut, claudeAndCodex(t)); err != nil {
		t.Fatalf("seed opt-out: %v", err)
	}

	// One fan-out pass over all three, as refreshOneProject does per project.
	for _, d := range []string{optIn, optOut, noManifest} {
		if _, err := MaintainManagedGitignore(d, claudeAndCodex(t)); err != nil {
			t.Fatalf("maintain %s: %v", d, err)
		}
	}

	if got := readProjectGitignore(t, optIn); !strings.Contains(got, managedBegin) {
		t.Errorf("opted-in project must have a block:\n%s", got)
	}
	if got := readProjectGitignore(t, noManifest); !strings.Contains(got, managedBegin) {
		t.Errorf("manifest-less project must default to a block:\n%s", got)
	}
	if got := readProjectGitignore(t, optOut); got != "" {
		t.Errorf("opted-out project must have no block, and its sibling's state must not leak in:\n%s", got)
	}
}

// installFixture stands up an isolated agents home + project dir, chdirs into
// the project, and returns the project path. Mirrors the harness the rest of
// install_test.go uses.
func installFixture(t *testing.T, manifest string) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)
	if err := os.WriteFile(filepath.Join(agentsHome, "config.json"), []byte(`{"version":2}`), 0o644); err != nil {
		t.Fatal(err)
	}

	projDir := filepath.Join(tmp, "proj")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projDir, config.AgentsRCFile), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	prev, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(prev) })
	if err := os.Chdir(projDir); err != nil {
		t.Fatal(err)
	}
	saved := Flags
	Flags = GlobalFlags{Yes: true}
	t.Cleanup(func() { Flags = saved })
	return projDir
}

// TestRunInstall_MaintainsManagedGitignore is the end-to-end pin for the gap
// this change closes: before it, only `da refresh` wrote the managed block, so
// a freshly-installed repo carried its generated outputs as untracked noise
// until someone happened to run refresh.
func TestRunInstall_MaintainsManagedGitignore(t *testing.T) {
	tests := []struct {
		name      string
		manifest  string
		wantBlock bool
	}{
		{
			name:      "knob absent writes the block",
			manifest:  `{"project":"proj","version":2}`,
			wantBlock: true,
		},
		{
			name:      "knob true writes the block",
			manifest:  `{"project":"proj","version":2,"gitignore_projections":true}`,
			wantBlock: true,
		},
		{
			name:      "knob false writes no block",
			manifest:  `{"project":"proj","version":2,"gitignore_projections":false}`,
			wantBlock: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			projDir := installFixture(t, tc.manifest)

			if err := RunInstall(false, StdInstallDeps{}); err != nil {
				t.Fatalf("RunInstall: %v", err)
			}

			got := readProjectGitignore(t, projDir)
			if hasBlock := strings.Contains(got, managedBegin); hasBlock != tc.wantBlock {
				t.Fatalf("block present = %v, want %v:\n%s", hasBlock, tc.wantBlock, got)
			}
			if !tc.wantBlock {
				return
			}
			for _, want := range []string{"*.dot-agents-backup", ".agentsrc.local.json"} {
				if !strings.Contains(got, "\n"+want+"\n") {
					t.Errorf("managed block missing %q:\n%s", want, got)
				}
			}
			if strings.Contains(got, "\n.agentsrc.json\n") || strings.Contains(got, "\n.agentsrc.lock\n") {
				t.Errorf("committed contract must stay tracked:\n%s", got)
			}
		})
	}
}

// TestRunInstall_ManagedGitignoreIsByteStableAcrossRuns pins that a second
// install does not churn the file — the property that makes it safe to commit.
func TestRunInstall_ManagedGitignoreIsByteStableAcrossRuns(t *testing.T) {
	projDir := installFixture(t, `{"project":"proj","version":2}`)
	userContent := "# project ignores\nbuild/\n"
	if err := os.WriteFile(filepath.Join(projDir, ".gitignore"), []byte(userContent), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := RunInstall(false, StdInstallDeps{}); err != nil {
		t.Fatalf("first install: %v", err)
	}
	first := readProjectGitignore(t, projDir)
	if !strings.HasPrefix(first, userContent) {
		t.Errorf("user content must be preserved at the head:\n%s", first)
	}

	if err := RunInstall(false, StdInstallDeps{}); err != nil {
		t.Fatalf("second install: %v", err)
	}
	if second := readProjectGitignore(t, projDir); second != first {
		t.Errorf("re-install must be byte-stable:\ngot  %q\nwant %q", second, first)
	}
}

// TestRunInstall_DryRunWritesNoGitignore pins that a preview stays a preview.
func TestRunInstall_DryRunWritesNoGitignore(t *testing.T) {
	projDir := installFixture(t, `{"project":"proj","version":2}`)
	Flags.DryRun = true

	if err := RunInstall(false, StdInstallDeps{}); err != nil {
		t.Fatalf("dry-run install: %v", err)
	}
	if got := readProjectGitignore(t, projDir); got != "" {
		t.Errorf("dry-run must not write .gitignore:\n%s", got)
	}
}

// TestMaintainManagedGitignore_CorruptManifestSkips pins that an unreadable
// manifest is reported as a skip rather than either guessing the knob's value
// or failing the run — refresh tolerates a corrupt manifest everywhere else,
// and this step must not be the one place that starts failing it.
func TestMaintainManagedGitignore_CorruptManifestSkips(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, config.AgentsRCFile), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	msg, err := MaintainManagedGitignore(dir, claudeAndCodex(t))
	if err != nil {
		t.Fatalf("an unreadable manifest must not fail the run: %v", err)
	}
	if msg != managedGitignoreSkipMsg {
		t.Errorf("status line = %q, want %q", msg, managedGitignoreSkipMsg)
	}
	// A manifest we cannot parse must not be silently treated as opted-in and
	// have a block written against a guessed knob value.
	if got := readProjectGitignore(t, dir); got != "" {
		t.Errorf("no .gitignore should be written for an unparseable manifest:\n%s", got)
	}
}
