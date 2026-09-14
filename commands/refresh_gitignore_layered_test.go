package commands

// Layer-awareness coverage for the managed-.gitignore knob on `da refresh`.
//
// Refresh and install share lifecycle.MaintainManagedGitignore, so they must
// also share its INPUT: the layered effective config. Before this, refresh
// reloaded the flat repo-local manifest, so an org/team layer's
// `gitignore_projections: false` was ignored and refresh kept rewriting a
// block the fleet had opted out of — and, worse, an install that honored the
// layer and a refresh that did not would fight over the same file.
//
// The rows below run the REAL runRefresh over a repo that already carries a
// managed block, so a disabled row proves RETRACTION rather than a skipped
// write.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/links"
)

const refreshManagedBegin = "# >>> dot-agents managed (project outputs) >>>"

// gitignoreLayerRepo stands up an isolated home plus a repo that extends a
// layer declaring the knob, seeds a managed .gitignore block, registers the
// project, and returns the repo path. nil knobs mean "key absent".
func gitignoreLayerRepo(t *testing.T, layerKnob, repoKnob *bool) string {
	t.Helper()
	home := seedProjectionFlagsHome(t)

	layerRoot := t.TempDir()
	layerBody := "{}"
	if layerKnob != nil {
		layerBody = `{"gitignore_projections":` + strconv.FormatBool(*layerKnob) + `}`
	}
	writeProjectionFile(t, filepath.Join(layerRoot, "org", "base.json"), layerBody)

	manifest := `{"version":2,"project":"p",` +
		`"sources":[{"id":"org","type":"local","path":` + strconv.Quote(layerRoot) + `}],` +
		`"extends":["org:org/base.json"]`
	if repoKnob != nil {
		manifest += `,"gitignore_projections":` + strconv.FormatBool(*repoKnob)
	}
	manifest += "}"

	repo := filepath.Join(home, "p")
	writeProjectionFile(t, filepath.Join(repo, config.AgentsRCFile), manifest)
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("node_modules/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := links.EnsureManagedGitignore(repo, []string{".claude/"}); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("p", repo)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	return repo
}

func TestRunRefresh_ManagedGitignoreHonorsTheEffectiveKnob(t *testing.T) {
	yes, no := true, false

	tests := []struct {
		name      string
		layerKnob *bool
		repoKnob  *bool
		wantBlock bool
	}{
		{
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
			repo := gitignoreLayerRepo(t, tc.layerKnob, tc.repoKnob)

			saved := Flags
			Flags = GlobalFlags{Yes: true}
			defer func() { Flags = saved }()
			if err := runRefresh(refreshScope{Project: "p"}, stdRefreshConfigLoader{}, stdImportDeps{}, stdAddDeps{}); err != nil {
				t.Fatalf("runRefresh: %v", err)
			}

			data, err := os.ReadFile(filepath.Join(repo, ".gitignore"))
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			got := string(data)
			if hasBlock := strings.Contains(got, refreshManagedBegin); hasBlock != tc.wantBlock {
				t.Fatalf("block present = %v, want %v:\n%s", hasBlock, tc.wantBlock, got)
			}
			if !strings.Contains(got, "node_modules/") {
				t.Errorf("user content must be preserved:\n%s", got)
			}
		})
	}
}
