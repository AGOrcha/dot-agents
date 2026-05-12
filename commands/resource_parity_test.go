package commands

import (
	"os"
	"path/filepath"
	"testing"
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
func TestResourceCommandParity(t *testing.T) {
	type resourceCase struct {
		name      string
		dirSeg    string
		sample    string
		body      string
		runList   func(scope string) error
		runShow   func(scope, name string) error
		runRemove func(scope, name string, dryRun, yes, force bool) error
	}

	cases := []resourceCase{
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
					Flags:              rulesGlobalFlags{DryRun: dryRun, Yes: yes, Force: force},
					maxArgsWithHints:   MaximumNArgsWithHints,
					exactArgsWithHints: ExactArgsWithHints,
				}
				return runSettingsRemove(deps, scope, name)
			},
		},
	}

	const scope = "global"
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			agentsHome := filepath.Join(tmp, ".agents")
			fakeHome := filepath.Join(tmp, "home")
			scopeDir := filepath.Join(agentsHome, tc.dirSeg, scope)
			if err := os.MkdirAll(scopeDir, 0o755); err != nil {
				t.Fatalf("setup scope dir: %v", err)
			}
			if err := os.MkdirAll(fakeHome, 0o755); err != nil {
				t.Fatalf("setup fake home: %v", err)
			}
			t.Setenv("HOME", fakeHome)
			t.Setenv("AGENTS_HOME", agentsHome)

			samplePath := filepath.Join(scopeDir, tc.sample)
			if err := os.WriteFile(samplePath, []byte(tc.body), 0o644); err != nil {
				t.Fatalf("write sample: %v", err)
			}

			// list should succeed against a populated scope.
			if err := tc.runList(scope); err != nil {
				t.Fatalf("%s list (populated): %v", tc.name, err)
			}

			// show should read the canonical file without error.
			if err := tc.runShow(scope, tc.sample); err != nil {
				t.Fatalf("%s show: %v", tc.name, err)
			}

			// remove with --force should delete the file. Dry-run is exercised
			// per-subsystem; here we want the destructive path so the next
			// list returns the empty state.
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
		})
	}
}

// TestResourceCommandParity_DryRunPreserves confirms each list/show/remove
// surface honors --dry-run by leaving the underlying file in place.
func TestResourceCommandParity_DryRunPreserves(t *testing.T) {
	type dryCase struct {
		name      string
		dirSeg    string
		sample    string
		body      string
		runRemove func(scope, name string, dryRun, yes, force bool) error
	}
	cases := []dryCase{
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
					Flags:              rulesGlobalFlags{DryRun: dryRun, Yes: yes, Force: force},
					maxArgsWithHints:   MaximumNArgsWithHints,
					exactArgsWithHints: ExactArgsWithHints,
				}
				return runSettingsRemove(deps, scope, name)
			},
		},
	}

	const scope = "global"
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			agentsHome := filepath.Join(tmp, ".agents")
			fakeHome := filepath.Join(tmp, "home")
			scopeDir := filepath.Join(agentsHome, tc.dirSeg, scope)
			if err := os.MkdirAll(scopeDir, 0o755); err != nil {
				t.Fatalf("setup: %v", err)
			}
			if err := os.MkdirAll(fakeHome, 0o755); err != nil {
				t.Fatalf("setup fake home: %v", err)
			}
			t.Setenv("HOME", fakeHome)
			t.Setenv("AGENTS_HOME", agentsHome)

			path := filepath.Join(scopeDir, tc.sample)
			if err := os.WriteFile(path, []byte(tc.body), 0o644); err != nil {
				t.Fatalf("write sample: %v", err)
			}

			if err := tc.runRemove(scope, tc.sample, true, false, false); err != nil {
				t.Fatalf("%s dry-run remove: %v", tc.name, err)
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("%s dry-run should preserve file: %v", tc.name, err)
			}
		})
	}
}
