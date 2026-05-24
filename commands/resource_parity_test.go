package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/NikashPrakash/dot-agents/commands/internal/cmdutil"
)

// TestResourceCommandParity exercises the list/show/remove triplet shared by
// the rules, mcp, and settings command surfaces. Each subsystem stores
// canonical files under ~/.agents/<dir>/<scope>/ and routes through
// cmdutil.RunCanonical{List,Show,Remove}; this test validates the surfaces in
// lockstep so behavioral drift between them surfaces immediately.
//
// `hooks` shares the same list/show/remove shape but operates on bundle
// directories (HOOK.yaml) instead of single files and has its own logical-name
// resolution path through commands/hooks; it is exercised by hooks/*_test.go
// and intentionally not included here.
type resourceParityCase struct {
	name      string
	dirSeg    string
	sample    string
	body      string
	runList   func(scope string) error
	runShow   func(scope, name string) error
	runRemove func(scope, name string, dryRun, yes, force bool) error
}

func resourceParityCases() []resourceParityCase {
	return []resourceParityCase{
		{
			name:   "rules",
			dirSeg: "rules",
			sample: "parity-rule.md",
			body:   "---\ndescription: parity rule\n---\n# parity\n",
			runList: func(scope string) error {
				return runRulesList(scope)
			},
			runShow: func(scope, name string) error {
				deps := makeRulesDeps(false, false, false)
				return runRulesShow(deps, scope, name)
			},
			runRemove: func(scope, name string, dryRun, yes, force bool) error {
				deps := makeRulesDeps(dryRun, yes, force)
				return runRulesRemove(deps, scope, name)
			},
		},
		{
			name:   "mcp",
			dirSeg: "mcp",
			sample: "parity-mcp.json",
			body:   `{"mcpServers":{"parity":{"command":"echo"}}}`,
			runList: func(scope string) error {
				return runMCPList(scope)
			},
			runShow: func(scope, name string) error {
				return runMCPShow(scope, name)
			},
			runRemove: func(scope, name string, dryRun, yes, force bool) error {
				deps := makeMCPDeps(dryRun, yes, force)
				return runMCPRemove(deps, scope, name)
			},
		},
		{
			name:   "settings",
			dirSeg: "settings",
			sample: "parity-cursor.json",
			body:   `{"editor.tabSize":2}`,
			runList: func(scope string) error {
				return runSettingsList(scope)
			},
			runShow: func(scope, name string) error {
				return runSettingsShow(scope, name)
			},
			runRemove: func(scope, name string, dryRun, yes, force bool) error {
				deps := settingsDeps{
					Flags:              cmdutil.CanonicalCmdFlags{DryRun: dryRun, Yes: yes, Force: force},
					maxArgsWithHints:   MaximumNArgsWithHints,
					exactArgsWithHints: ExactArgsWithHints,
				}
				return runSettingsRemove(deps, scope, name)
			},
		},
	}
}

// setupResourceParityScope provisions the canonical agents-home/scope layout
// and writes the sample file into it. Returns the absolute sample path.
func setupResourceParityScope(t *testing.T, dirSeg, scope, sample, body string) string {
	t.Helper()
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	fakeHome := filepath.Join(tmp, "home")
	scopeDir := filepath.Join(agentsHome, dirSeg, scope)
	if err := os.MkdirAll(scopeDir, 0o755); err != nil {
		t.Fatalf("setup scope dir: %v", err)
	}
	if err := os.MkdirAll(fakeHome, 0o755); err != nil {
		t.Fatalf("setup fake home: %v", err)
	}
	t.Setenv("HOME", fakeHome)
	t.Setenv("AGENTS_HOME", agentsHome)

	samplePath := filepath.Join(scopeDir, sample)
	if err := os.WriteFile(samplePath, []byte(body), 0o644); err != nil {
		t.Fatalf("write sample: %v", err)
	}
	return samplePath
}

func runResourceParityCase(t *testing.T, tc resourceParityCase) {
	const scope = "global"
	samplePath := setupResourceParityScope(t, tc.dirSeg, scope, tc.sample, tc.body)

	// list should succeed against a populated scope.
	if err := tc.runList(scope); err != nil {
		t.Fatalf("%s list (populated): %v", tc.name, err)
	}

	// show should read the canonical file without error.
	if err := tc.runShow(scope, tc.sample); err != nil {
		t.Fatalf("%s show: %v", tc.name, err)
	}

	// remove with --force should delete the file. Dry-run is exercised
	// per-subsystem; here we want the destructive path so the next list
	// returns the empty state.
	if err := tc.runRemove(scope, tc.sample, false, true, false); err != nil {
		t.Fatalf("%s remove: %v", tc.name, err)
	}
	if _, err := os.Stat(samplePath); !os.IsNotExist(err) {
		t.Fatalf("%s remove: expected file gone, stat err=%v", tc.name, err)
	}

	// list against the now-empty scope should still succeed (info-only).
	if err := tc.runList(scope); err != nil {
		t.Fatalf("%s list (empty): %v", tc.name, err)
	}

	// show of the removed file should error with a not-found hint.
	if err := tc.runShow(scope, tc.sample); err == nil {
		t.Fatalf("%s show after remove: expected error", tc.name)
	}
}

func TestResourceCommandParity(t *testing.T) {
	for _, tc := range resourceParityCases() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			runResourceParityCase(t, tc)
		})
	}
}

// TestResourceCommandParity_DryRunPreserves confirms each list/show/remove
// surface honors --dry-run by leaving the underlying file in place.
type resourceDryRunCase struct {
	name      string
	dirSeg    string
	sample    string
	body      string
	runRemove func(scope, name string, dryRun, yes, force bool) error
}

func resourceDryRunCases() []resourceDryRunCase {
	return []resourceDryRunCase{
		{
			name:   "rules",
			dirSeg: "rules",
			sample: "keep.md",
			body:   "---\ndescription: keep\n---\nbody",
			runRemove: func(scope, name string, dryRun, yes, force bool) error {
				return runRulesRemove(makeRulesDeps(dryRun, yes, force), scope, name)
			},
		},
		{
			name:   "mcp",
			dirSeg: "mcp",
			sample: "keep.json",
			body:   `{}`,
			runRemove: func(scope, name string, dryRun, yes, force bool) error {
				return runMCPRemove(makeMCPDeps(dryRun, yes, force), scope, name)
			},
		},
		{
			name:   "settings",
			dirSeg: "settings",
			sample: "keep.json",
			body:   `{}`,
			runRemove: func(scope, name string, dryRun, yes, force bool) error {
				deps := settingsDeps{
					Flags:              cmdutil.CanonicalCmdFlags{DryRun: dryRun, Yes: yes, Force: force},
					maxArgsWithHints:   MaximumNArgsWithHints,
					exactArgsWithHints: ExactArgsWithHints,
				}
				return runSettingsRemove(deps, scope, name)
			},
		},
	}
}

func runResourceDryRunCase(t *testing.T, tc resourceDryRunCase) {
	const scope = "global"
	path := setupResourceParityScope(t, tc.dirSeg, scope, tc.sample, tc.body)

	if err := tc.runRemove(scope, tc.sample, true, false, false); err != nil {
		t.Fatalf("%s dry-run remove: %v", tc.name, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("%s dry-run should preserve file: %v", tc.name, err)
	}
}

func TestResourceCommandParity_DryRunPreserves(t *testing.T) {
	for _, tc := range resourceDryRunCases() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			runResourceDryRunCase(t, tc)
		})
	}
}
