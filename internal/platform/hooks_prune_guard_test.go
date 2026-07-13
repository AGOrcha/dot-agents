package platform

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/testutil"
)

// ---------------------------------------------------------------------------
// Slice 2 survival test: source-read-error must ABORT a platform's sync step
// and leave existing destination files untouched — never degrade into an
// empty desired set that then prunes/deletes everything that was already
// there. This is the single highest-value test from the swallowed-errors
// audit: before this fix, an unreadable (not merely absent) source hooks/
// rules/settings directory was misread by hooks.go's resolveHookSpecInScopes
// (or a raw os.ReadDir call) as "nothing to render," and the platform's
// prune/remove step then deleted every pre-existing managed file at the
// destination — active data loss triggered by a transient/permission error,
// not a real absence.
// ---------------------------------------------------------------------------

// hookPruneGuardCase describes one platform's exposure to the
// source-read-error -> destination-prune family: which source directory
// (under agentsHome) must be misread as absent, and which pre-existing
// destination file(s) must survive a CreateLinks call once that directory is
// unreadable.
type hookPruneGuardCase struct {
	name string

	// newPlatform constructs the Platform under test.
	newPlatform func() Platform

	// seedSource creates whatever agentsHome fixtures are needed so
	// CreateLinks reaches the function under test before erroring, and
	// returns the source directory that must be made unreadable.
	seedSource func(t *testing.T, agentsHome, project string) (unreadableDir string)

	// seedDestinations creates the pre-existing managed destination
	// file(s) that must survive, returning their paths.
	seedDestinations func(t *testing.T, home, repo string) []string
}

func TestPlatformCreateLinks_UnreadableSourceAbortsInsteadOfPruning(t *testing.T) {
	const project = "proj"

	// Content shaped to look like a real dot-agents-rendered hook settings
	// file — i.e. exactly what emitPreferredHookFile's removeRendered
	// branch would have deleted pre-fix, had resolveHookSpecInScopes
	// silently swallowed the permission error and reported "no legacy
	// spec here."
	renderedClaudeHookSettings := `{"hooks":{"pre_tool_use":[{"matcher":"*","hooks":[{"type":"command","command":"/bin/true"}]}]}}`

	cases := []hookPruneGuardCase{
		{
			// claude.go:createRulesLinks — a permission-denied ReadDir on
			// the project rules source must not be read as "zero rules,"
			// which would prune every .claude/rules/proj--*.md link.
			name:        "claude/project-rules-dir",
			newPlatform: NewClaude,
			seedSource: func(t *testing.T, agentsHome, project string) string {
				dir := filepath.Join(agentsHome, "rules", project)
				mustMkdirAllT(t, dir)
				return dir
			},
			seedDestinations: func(t *testing.T, home, repo string) []string {
				dst := filepath.Join(repo, claudeDir, "rules", project+"--stale.md")
				writeTextFile(t, dst, "# pre-existing managed rule\n")
				return []string{dst}
			},
		},
		{
			// claude.go:ensureUserSettings (root-caused by
			// hooks.go:resolveHookSpecInScopes) — a permission-denied Stat
			// under the "settings" bucket fallback must not be read as "no
			// legacy hook spec," which would delete ~/.claude/settings.json
			// across every user home.
			name:        "claude/global-settings-bucket",
			newPlatform: NewClaude,
			seedSource: func(t *testing.T, agentsHome, project string) string {
				dir := filepath.Join(agentsHome, "settings", "global")
				mustMkdirAllT(t, dir)
				return dir
			},
			seedDestinations: func(t *testing.T, home, repo string) []string {
				dst := filepath.Join(home, claudeDir, claudeSettingsJSON)
				writeTextFile(t, dst, renderedClaudeHookSettings)
				return []string{dst}
			},
		},
		{
			// cursor.go:createRuleLinks/collectRuleLinks — a
			// permission-denied ReadDir on the global rules source must not
			// be read as "zero rules," which would prune every
			// .cursor/rules/* managed link.
			name:        "cursor/global-rules-dir",
			newPlatform: NewCursor,
			seedSource: func(t *testing.T, agentsHome, project string) string {
				dir := filepath.Join(agentsHome, "rules", "global")
				mustMkdirAllT(t, dir)
				return dir
			},
			seedDestinations: func(t *testing.T, home, repo string) []string {
				dst := filepath.Join(repo, cursorDir, "rules", "global--old.mdc")
				writeTextFile(t, dst, "# pre-existing managed rule\n")
				return []string{dst}
			},
		},
		{
			// copilot.go:createClaudeCompatLinks (root-caused by
			// hooks.go:resolveHookSpecInScopes) — a permission-denied Stat
			// under the "settings" bucket fallback must not be read as "no
			// legacy hook spec," which would delete
			// .claude/settings.local.json (Copilot's Claude-compat hook
			// file).
			name:        "copilot/project-settings-bucket",
			newPlatform: NewCopilot,
			seedSource: func(t *testing.T, agentsHome, project string) string {
				dir := filepath.Join(agentsHome, "settings", project)
				mustMkdirAllT(t, dir)
				return dir
			},
			seedDestinations: func(t *testing.T, home, repo string) []string {
				dst := filepath.Join(repo, copilotClaudeDir, copilotSettingsLocalJSON)
				writeTextFile(t, dst, renderedClaudeHookSettings)
				return []string{dst}
			},
		},
		{
			// codex.go:writeRepoHooks/writeUserHomeHooks — a
			// permission-denied project-scope hooks bucket must abort the
			// whole hooks sync step, leaving both the repo- and
			// user-home-scope rendered hook files untouched.
			name:        "codex/project-hooks-dir",
			newPlatform: NewCodex,
			seedSource: func(t *testing.T, agentsHome, project string) string {
				dir := filepath.Join(agentsHome, "hooks", project)
				mustMkdirAllT(t, dir)
				return dir
			},
			seedDestinations: func(t *testing.T, home, repo string) []string {
				repoDst := filepath.Join(repo, codexDir, codexHooksJSON)
				homeDst := filepath.Join(home, codexDir, codexHooksJSON)
				writeTextFile(t, repoDst, `{"hooks":{}}`)
				writeTextFile(t, homeDst, `{"hooks":{}}`)
				return []string{repoDst, homeDst}
			},
		},
		{
			// antigravity.go:writeRepoHooks/writeUserHomeHooks — same
			// family as codex, Antigravity's hardlink hook mode.
			name:        "antigravity/project-hooks-dir",
			newPlatform: NewAntigravity,
			seedSource: func(t *testing.T, agentsHome, project string) string {
				dir := filepath.Join(agentsHome, "hooks", project)
				mustMkdirAllT(t, dir)
				return dir
			},
			seedDestinations: func(t *testing.T, home, repo string) []string {
				repoDst := filepath.Join(repo, antigravityDir, antigravityHooksFile)
				homeDst := filepath.Join(home, antigravityDir, antigravityHooksFile)
				writeTextFile(t, repoDst, `{"hooks":{}}`)
				writeTextFile(t, homeDst, `{"hooks":{}}`)
				return []string{repoDst, homeDst}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			agentsHome := filepath.Join(tmp, ".agents")
			home := filepath.Join(tmp, "home")
			repo := filepath.Join(tmp, "repo")
			t.Setenv("AGENTS_HOME", agentsHome)
			t.Setenv("HOME", home)
			mustMkdirAllT(t, agentsHome)
			mustMkdirAllT(t, home)
			mustMkdirAllT(t, repo)

			unreadableDir := tc.seedSource(t, agentsHome, project)
			destinations := tc.seedDestinations(t, home, repo)

			testutil.MakeDirUnreadable(t, unreadableDir)

			err := tc.newPlatform().CreateLinks(project, repo)
			if err == nil {
				t.Fatalf("%s: CreateLinks with unreadable %s: expected error, got nil", tc.name, unreadableDir)
			}

			for _, dst := range destinations {
				if _, statErr := os.Lstat(dst); statErr != nil {
					t.Errorf("%s: destination %s must survive an aborted sync, lstat failed: %v", tc.name, dst, statErr)
				}
			}
		})
	}
}

// TestClaudeCreateRulesLinks_LegitimateAbsenceStillPrunes guards the sibling
// path the fix above must not regress: when the project rules source is
// genuinely absent (never created, not merely unreadable), createRulesLinks
// must still succeed and prune the stale managed rule link — the exact
// behavior the ATOMIC fix must preserve for a real absence, as opposed to an
// unknown/errored read.
func TestClaudeCreateRulesLinks_LegitimateAbsenceStillPrunes(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	home := filepath.Join(tmp, "home")
	repo := filepath.Join(tmp, "repo")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)
	mustMkdirAllT(t, agentsHome)
	mustMkdirAllT(t, home)
	mustMkdirAllT(t, repo)

	// agentsHome/rules/proj is never created — a genuine absence, not an
	// unreadable directory.
	stale := filepath.Join(repo, claudeDir, "rules", "proj--stale.md")
	writeTextFile(t, stale, "# stale managed rule\n")

	if err := NewClaude().CreateLinks("proj", repo); err != nil {
		t.Fatalf("CreateLinks with legitimately absent rules source: unexpected error: %v", err)
	}
	if _, err := os.Lstat(stale); !os.IsNotExist(err) {
		t.Fatalf("expected stale rule link to be pruned on legitimate absence, lstat err = %v", err)
	}
}
