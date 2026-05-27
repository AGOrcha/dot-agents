package commands

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/platform"
	"github.com/NikashPrakash/dot-agents/internal/ui"
	"github.com/spf13/cobra"
)

// removeDeps is the multi-method collaborator runRemove and its
// removeProjectDirs helper need (interface-DI per docs/TEST_SEAMS.md).
// File-scoped — do not share with other commands files. The narrower
// dirCleaner interface below stays scoped to emptyProjectDirs; runRemove
// has its own broader role because it loads config in addition to the
// --clean RemoveAll path.
type removeDeps interface {
	RemoveAll(path string) error
	LoadConfig() (*config.Config, error)
}

// stdRemoveDeps is the production removeDeps backed by os.RemoveAll and
// config.Load.
type stdRemoveDeps struct{}

func (stdRemoveDeps) RemoveAll(path string) error         { return os.RemoveAll(path) }
func (stdRemoveDeps) LoadConfig() (*config.Config, error) { return config.Load() }

func NewRemoveCmd() *cobra.Command {
	var cleanDirs bool

	cmd := &cobra.Command{
		Use:   "remove <project>",
		Short: "Remove a project from da management",
		Long: `Unregisters a project from da and removes platform links (rules, hooks,
MCP, settings, and other managed outputs) the same way install/refresh created them.

With --clean, also removes project directories from ~/.agents/.`,
		Example: ExampleBlock(
			"  da remove billing-api",
			"  da remove billing-api --clean",
			"  da status",
		),
		Args: ExactArgsWithHints(1, "Use the managed project name from `da status`."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRemove(args[0], cleanDirs, stdRemoveDeps{})
		},
	}
	cmd.Flags().BoolVar(&cleanDirs, "clean", false, "Also remove project directories from ~/.agents/")
	return cmd
}

func runRemove(projectName string, cleanDirs bool, deps removeDeps) error {
	cfg, err := deps.LoadConfig()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	projectPath := cfg.GetProjectPath(projectName)
	if projectPath == "" {
		return ErrorWithHints(
			fmt.Sprintf("project not found: %s", projectName),
			"Run `da status` to see registered projects.",
		)
	}

	announceRemoveTarget(projectName, projectPath)
	printRemovePreview(projectName, projectPath, cleanDirs)

	if Flags.DryRun {
		fmt.Fprintln(os.Stdout, "\nDRY RUN - no changes made")
		return nil
	}
	if confirmRemoveProceed() {
		return nil
	}

	if err := removeProjectLinks(projectName, projectPath); err != nil {
		return err
	}

	if err := cleanProjectCanonicalDirs(projectName, cleanDirs, deps); err != nil {
		return err
	}

	if err := unregisterRemovedProject(cfg, projectName); err != nil {
		return err
	}
	emitRemoveSuccessBox(projectName, cleanDirs)
	return nil
}

// announceRemoveTarget prints the header, project/path lines, and the
// "Analyzing project..." bullet.
func announceRemoveTarget(projectName, projectPath string) {
	ui.Header("da remove")
	fmt.Fprintf(os.Stdout, "Removing project: %s\n", ui.BoldText(projectName))
	fmt.Fprintf(os.Stdout, "Path: %s\n", ui.DimText(config.DisplayPath(projectPath)))

	ui.Step("Analyzing project...")
	if _, err := os.Stat(projectPath); err == nil {
		ui.Bullet("ok", "Project directory found")
	} else {
		ui.Bullet("warn", "Project directory not found (links may have been moved)")
	}
}

// printRemovePreview prints the "The following will be removed" preview
// block: per-platform link inventory, registration line, git-source cache
// warning when relevant, and the destructive/non-destructive canonical-dirs
// preview based on cleanDirs.
func printRemovePreview(projectName, projectPath string, cleanDirs bool) {
	displayPath := config.DisplayPath(projectPath)
	ui.Step("The following will be removed:")
	ui.PreviewSection("From "+displayPath+":",
		".cursor/rules/global--*.mdc     (hard links)",
		".cursor/rules/"+projectName+"--*.mdc (hard links)",
		".cursor/hooks.json, .codex/hooks.json (managed links)",
		".claude/rules/"+projectName+"--*.md      (symlinks)",
		"AGENTS.md                       (symlink)",
		"opencode.json and .opencode/agent/* (symlinks)",
		".github/copilot-instructions.md (symlink)",
		".agents/skills/* and .github/agents/*.agent.md (symlinks)",
		".vscode/mcp.json and .claude/settings.local.json (symlinks)",
	)
	ui.PreviewSection("From ~/.agents/config.json:",
		"Project registration for '"+projectName+"'",
	)
	warnRemoveGitSourceCache(projectPath)
	printRemoveCanonicalDirsPreview(projectName, cleanDirs)
}

// warnRemoveGitSourceCache prints the one-shot warn when the project's
// manifest declares any git source, plus the manual rm hint. A missing
// manifest is silently skipped — git-cache cleanup only matters when there
// was a git source to begin with.
func warnRemoveGitSourceCache(projectPath string) {
	rc, err := config.LoadAgentsRC(projectPath)
	if err != nil {
		return
	}
	for _, src := range rc.Sources {
		if src.Type == "git" && src.URL != "" {
			ui.Warn("Git source cache not cleaned automatically")
			fmt.Fprintf(os.Stdout, "  Cache: %s~/.cache/dot-agents/sources/%s\n", ui.Dim, ui.Reset)
			fmt.Fprintf(os.Stdout, "  To clean: %srm -rf ~/.cache/dot-agents/sources/%s\n\n", ui.Dim, ui.Reset)
			return
		}
	}
}

// printRemoveCanonicalDirsPreview prints the destructive --clean WarnBox or
// the non-destructive "contents-cleared" PreviewSection — both list the same
// six canonical dirs owned by the project.
func printRemoveCanonicalDirsPreview(projectName string, cleanDirs bool) {
	if cleanDirs {
		ui.WarnBox("Destructive Action",
			"The --clean flag will permanently delete these directories entirely:",
			"  ~/.agents/rules/"+projectName+"/",
			"  ~/.agents/settings/"+projectName+"/",
			"  ~/.agents/mcp/"+projectName+"/",
			"  ~/.agents/hooks/"+projectName+"/",
			"  ~/.agents/skills/"+projectName+"/",
			"  ~/.agents/agents/"+projectName+"/",
		)
		return
	}
	ui.PreviewSection("From ~/.agents/ (managed canonical dirs):",
		"Contents cleared; the now-empty directories are kept:",
		"  ~/.agents/rules/"+projectName+"/",
		"  ~/.agents/settings/"+projectName+"/",
		"  ~/.agents/mcp/"+projectName+"/",
		"  ~/.agents/hooks/"+projectName+"/",
		"  ~/.agents/skills/"+projectName+"/",
		"  ~/.agents/agents/"+projectName+"/",
		"Pass --clean to remove the directories themselves too.",
	)
}

// confirmRemoveProceed prompts the user when neither --yes nor --force is
// set. Returns true when the user declined (caller returns nil).
func confirmRemoveProceed() bool {
	if Flags.Yes || Flags.Force {
		return false
	}
	if !ui.Confirm("Proceed with removal?", false) {
		ui.Info("Removal cancelled.")
		return true
	}
	return false
}

// removeProjectLinks runs the "Removing project..." link-removal phase:
// shared-target unwind plus per-platform RemoveLinks. A missing project
// directory is treated as "links already removed" (skip). Returns a typed
// error listing every cleanup failure so the registration stays put for a
// retry (no-orphan invariant).
func removeProjectLinks(projectName, projectPath string) error {
	ui.Step("Removing project...")
	if _, err := os.Stat(projectPath); err != nil {
		ui.Bullet("skip", "Skipped link removal (directory not found)")
		return nil
	}
	config.SetWindowsMirrorContext(projectPath)
	var installed []platform.Platform
	for _, p := range platform.All() {
		if p.IsInstalled() {
			installed = append(installed, p)
		}
	}
	var cleanupFailures []string
	if err := platform.RemoveSharedTargetPlan(projectName, projectPath, installed); err != nil {
		ui.Bullet("warn", fmt.Sprintf("shared targets: %v", err))
		cleanupFailures = append(cleanupFailures, fmt.Sprintf("shared targets: %v", err))
	}
	for _, p := range platform.All() {
		if err := p.RemoveLinks(projectName, projectPath); err != nil {
			ui.Bullet("warn", fmt.Sprintf("%s: %v", p.DisplayName(), err))
			cleanupFailures = append(cleanupFailures, fmt.Sprintf("%s: %v", p.DisplayName(), err))
			continue
		}
		ui.Bullet("ok", p.DisplayName()+" links removed")
	}
	if len(cleanupFailures) > 0 {
		return ErrorWithHints(
			fmt.Sprintf("remove incomplete for '%s': %s", projectName, strings.Join(cleanupFailures, "; ")),
			"The project registration was PRESERVED so cleanup can be retried. "+
				"Resolve the warnings above, then re-run `da remove "+projectName+"`.",
		)
	}
	return nil
}

// cleanProjectCanonicalDirs runs the canonical-dir cleanup phase. With
// --clean the dirs themselves go away; otherwise just their contents are
// cleared. A cleanup failure preserves the registration so a re-run still
// has a handle to retry against (no-orphan invariant).
func cleanProjectCanonicalDirs(projectName string, cleanDirs bool, deps removeDeps) error {
	ui.Step("Cleaning project directories...")
	var cleanupErr error
	doneMsg := "Cleared project directory contents (directories kept)"
	if cleanDirs {
		cleanupErr = removeProjectDirs(projectName, deps)
		doneMsg = "Removed project directories"
	} else {
		cleanupErr = emptyProjectDirs(osDirCleaner{}, projectName)
	}
	if cleanupErr != nil {
		retryCmd := "da remove " + projectName
		if cleanDirs {
			retryCmd += " --clean"
		}
		return ErrorWithHints(
			fmt.Sprintf("remove incomplete for '%s': could not clean project directories: %v", projectName, cleanupErr),
			"The project registration was PRESERVED so cleanup can be retried. "+
				"Resolve the errors above (permissions, locked files), then re-run "+
				"`"+retryCmd+"`.",
		)
	}
	ui.Bullet("ok", doneMsg)
	return nil
}

// unregisterRemovedProject removes the project from config.json. Only call
// after every prior cleanup step succeeded — unregistration is the
// success-stamp moment.
func unregisterRemovedProject(cfg *config.Config, projectName string) error {
	cfg.RemoveProject(projectName)
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}
	ui.Bullet("ok", "Unregistered from config.json")
	return nil
}

// emitRemoveSuccessBox prints the final success box. --clean omits the
// follow-up --clean hint (it was already used).
func emitRemoveSuccessBox(projectName string, cleanDirs bool) {
	if cleanDirs {
		ui.SuccessBox(fmt.Sprintf("Project '%s' removed completely!", projectName),
			"Verify removal: da status",
		)
		return
	}
	ui.SuccessBox(fmt.Sprintf("Project '%s' unlinked successfully!", projectName),
		"Verify removal: da status",
		"To also remove project directories: da remove "+projectName+" --clean",
	)
}

// projectCanonicalDirs is the canonical ~/.agents/ directory set owned by a
// single project. removeProjectDirs deletes these entirely; emptyProjectDirs
// clears their contents but keeps the dirs. The hooks scope dir resolves
// through the shared canonical helper so `da remove`/`--clean` and
// `da hooks remove` cannot disagree about where ~/.agents/hooks/<scope> lives.
func projectCanonicalDirs(project string) []string {
	agentsHome := config.AgentsHome()
	return []string{
		agentsHome + "/rules/" + project,
		agentsHome + "/settings/" + project,
		agentsHome + "/mcp/" + project,
		config.HooksScopeDir(project),
		agentsHome + "/skills/" + project,
		agentsHome + "/agents/" + project,
	}
}

// removePathErrFmt wraps a removal failure with the offending path. Shared
// by removeProjectDirs and emptyProjectDirs (3 sites) so the error format
// stays consistent across the two aggregators.
const removePathErrFmt = "%s: %w"

// removeProjectDirs deletes the project's canonical directories under
// ~/.agents/ (the `--clean` behavior: content AND the dirs themselves). It
// aggregates and returns every removal failure (errors.Join) rather than
// discarding them: a swallowed permission/locked-file error left
// `da remove --clean` reporting complete removal while stale canonical data
// remained on disk. A not-exist error is the expected "nothing to clean"
// case and is the only error swallowed.
func removeProjectDirs(project string, deps removeDeps) error {
	var errs []error
	for _, d := range projectCanonicalDirs(project) {
		if err := deps.RemoveAll(d); err != nil && !os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf(removePathErrFmt, d, err))
		}
	}
	return errors.Join(errs...)
}

// dirCleaner is the narrow filesystem collaborator emptyProjectDirs needs.
// It is injected (interface-DI) so the not-exist / aggregated-failure
// branches are provable with a fake rather than a package-level func-var
// seam — the project's preferred test-seam shape.
type dirCleaner interface {
	ReadDir(name string) ([]os.DirEntry, error)
	RemoveAll(path string) error
}

// osDirCleaner is the production dirCleaner backed by the os package.
type osDirCleaner struct{}

func (osDirCleaner) ReadDir(name string) ([]os.DirEntry, error) { return os.ReadDir(name) }
func (osDirCleaner) RemoveAll(path string) error                { return os.RemoveAll(path) }

// emptyProjectDirs deletes the *contents* of each canonical project dir but
// leaves the (now-empty) directory in place. This is the default `da remove`
// behavior: unmanage the project and reclaim its canonical content while
// keeping the dir skeleton so a later re-add lands in the same place;
// `--clean` (removeProjectDirs) additionally removes the directories.
//
// A missing dir is the expected "nothing to clear" case (ReadDir not-exist)
// and is skipped without recreating it. Every child-removal failure is
// aggregated (errors.Join) rather than discarded, mirroring removeProjectDirs:
// a swallowed permission error must not let `da remove` falsely report success
// while stale canonical content remains on disk.
func emptyProjectDirs(dc dirCleaner, project string) error {
	var errs []error
	for _, d := range projectCanonicalDirs(project) {
		entries, err := dc.ReadDir(d)
		if err != nil {
			if !os.IsNotExist(err) {
				errs = append(errs, fmt.Errorf(removePathErrFmt, d, err))
			}
			continue
		}
		for _, e := range entries {
			child := filepath.Join(d, e.Name())
			if err := dc.RemoveAll(child); err != nil && !os.IsNotExist(err) {
				errs = append(errs, fmt.Errorf(removePathErrFmt, child, err))
			}
		}
	}
	return errors.Join(errs...)
}
