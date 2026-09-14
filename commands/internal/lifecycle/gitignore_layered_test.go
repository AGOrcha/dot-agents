package lifecycle

// Layer-awareness coverage for the managed-.gitignore knob on `da install`.
//
// `gitignore_projections` is the ONE field among the audited projection knobs
// that actually gates behavior, and it used to be read by reloading the flat
// repo-local manifest. An org/team layer's value was therefore invisible: a
// fleet that disabled the managed block from its org layer still got one
// written into every repo that did not restate the key.
//
// These rows assert the scalar precedence the layer stack defines (repo-local
// beats imported; absent everywhere means on) through the REAL install path,
// and — for the disabled rows — that a block a previous run left behind is
// RETRACTED rather than merely not refreshed.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/links"
)

// gitignoreLayerManifest builds a repo manifest that extends a layer declaring
// the knob. layerKnob/repoKnob are nil for "key absent".
func gitignoreLayerManifest(t *testing.T, layerKnob, repoKnob *bool) string {
	t.Helper()
	layerRoot := t.TempDir()
	layerBody := "{}"
	if layerKnob != nil {
		layerBody = `{"gitignore_projections":` + boolJSON(*layerKnob) + `}`
	}
	writeJSONFile(t, filepath.Join(layerRoot, "org", "base.json"), layerBody)

	manifest := `{"version":2,"project":"proj",` +
		`"sources":[{"id":"org","type":"local","path":` + jsonQuoted(layerRoot) + `}],` +
		`"extends":["org:org/base.json"]`
	if repoKnob != nil {
		manifest += `,"gitignore_projections":` + boolJSON(*repoKnob)
	}
	return manifest + "}"
}

func boolJSON(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// seedManagedGitignoreBlock leaves the repo in the state a previous opted-in
// run would have: user content plus a managed block.
func seedManagedGitignoreBlock(t *testing.T, projDir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(projDir, ".gitignore"), []byte("node_modules/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := links.EnsureManagedGitignore(projDir, []string{".claude/"}); err != nil {
		t.Fatal(err)
	}
}

func TestRunInstall_ManagedGitignoreHonorsTheEffectiveKnob(t *testing.T) {
	yes, no := true, false

	tests := []struct {
		name      string
		layerKnob *bool
		repoKnob  *bool
		wantBlock bool
	}{
		{
			// The reported failure mode: nobody touched the repo manifest, the
			// org layer opted out, and the block must go away.
			name:      "layer false with no repo-local key retracts the block",
			layerKnob: &no,
			wantBlock: false,
		},
		{
			name:      "layer true with no repo-local key keeps the block",
			layerKnob: &yes,
			wantBlock: true,
		},
		{
			name:      "repo-local true overrides the layer's false",
			layerKnob: &no,
			repoKnob:  &yes,
			wantBlock: true,
		},
		{
			name:      "repo-local false overrides the layer's true",
			layerKnob: &yes,
			repoKnob:  &no,
			wantBlock: false,
		},
		{
			name:      "absent everywhere defaults on",
			wantBlock: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			projDir := installFixture(t, gitignoreLayerManifest(t, tc.layerKnob, tc.repoKnob))
			seedManagedGitignoreBlock(t, projDir)

			if err := RunInstall(false, StdInstallDeps{}); err != nil {
				t.Fatalf("RunInstall: %v", err)
			}

			got := readProjectGitignore(t, projDir)
			if hasBlock := strings.Contains(got, managedBegin); hasBlock != tc.wantBlock {
				t.Fatalf("block present = %v, want %v:\n%s", hasBlock, tc.wantBlock, got)
			}
			// User-authored ignores survive either direction.
			if !strings.Contains(got, "node_modules/") {
				t.Errorf("user content must be preserved:\n%s", got)
			}
		})
	}
}

// TestRunInstall_DryRunNeitherResolvesALockNorTouchesTheBlock: routing the knob
// through the effective snapshot must not turn a preview into a writer. Reading
// the layer stack under dry-run goes through the read-only Frozen path, so no
// .agentsrc.lock appears, and the managed block is left exactly as found —
// including when the effective knob says to remove it.
func TestRunInstall_DryRunNeitherResolvesALockNorTouchesTheBlock(t *testing.T) {
	projDir := installFixture(t, `{"version":2,"project":"proj","gitignore_projections":false}`)
	seedManagedGitignoreBlock(t, projDir)
	before := readProjectGitignore(t, projDir)
	Flags.DryRun = true

	if err := RunInstall(false, StdInstallDeps{}); err != nil {
		t.Fatalf("dry-run RunInstall: %v", err)
	}
	if got := readProjectGitignore(t, projDir); got != before {
		t.Errorf("dry-run must leave .gitignore byte-identical:\ngot  %q\nwant %q", got, before)
	}
	if _, err := os.Stat(filepath.Join(projDir, ".agentsrc.lock")); !os.IsNotExist(err) {
		t.Errorf("dry-run must not write a lock (stat err=%v)", err)
	}
}

// TestEffectiveGitignoreProjections_FallbackTriState pins the no-snapshot
// fallback used when a caller's pass-1 resolve failed: a missing manifest is
// the documented default-on case, an unparseable one is genuinely unknown, and
// a resolvable one answers from the layer stack — never from a flat read.
func TestEffectiveGitignoreProjections_FallbackTriState(t *testing.T) {
	t.Run("missing manifest defaults on", func(t *testing.T) {
		isolateAgentsHome(t)
		enabled, known := EffectiveGitignoreProjections(t.TempDir(), nil)
		if !known || !enabled {
			t.Errorf("missing manifest = (enabled=%v, known=%v), want (true, true)", enabled, known)
		}
	})

	t.Run("unparseable manifest is unknown", func(t *testing.T) {
		isolateAgentsHome(t)
		dir := t.TempDir()
		writeJSONFile(t, filepath.Join(dir, ".agentsrc.json"), "{not json")
		if _, known := EffectiveGitignoreProjections(dir, nil); known {
			t.Error("an unparseable manifest must leave the knob unknown, not guess a default")
		}
	})

	t.Run("user-local layer supplies the value", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("AGENTS_HOME", home)
		writeJSONFile(t, filepath.Join(home, ".agentsrc.json"), `{"version":2,"gitignore_projections":false}`)
		dir := t.TempDir()
		writeJSONFile(t, filepath.Join(dir, ".agentsrc.json"), `{"version":2,"project":"proj"}`)

		enabled, known := EffectiveGitignoreProjections(dir, nil)
		if !known {
			t.Fatal("a resolvable project must yield a known knob value")
		}
		if enabled {
			t.Error("the user-local layer's explicit false must win over the absent repo-local key")
		}
	})
}
