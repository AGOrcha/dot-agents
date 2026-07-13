package lifecycle

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/links"
	"github.com/AGOrcha/dot-agents/internal/platform"
	scaffoldhome "github.com/AGOrcha/dot-agents/internal/scaffold/home"
	scaffoldhooks "github.com/AGOrcha/dot-agents/internal/scaffold/hooks"
	"github.com/AGOrcha/dot-agents/internal/ui"
	"github.com/spf13/cobra"
)

// Package-level seam vars for init (preserved per t01 SHAPE.md "PRESERVE
// current package-var seams during the moves"). Each is a getter so the
// shim in commands/init.go can repoint them at commands.Flags lazily —
// NewInitCmd runs at root-construction time (before flags parse), and
// the getters are evaluated at RunE time. Tests in this package set the
// underlying bool fields directly via the convenience setters below.
// Kept file-scoped (not promoted to deps.go) because t05's bundle
// write_scope intentionally limits this task to the init files only —
// Force/DryRun/Yes are init-specific seams today and consolidate into
// a shared lifecycle.Flags struct alongside the other lifecycle command
// moves (t03/t04/t06+).
var (
	initForce  bool
	initDryRun bool
	initYes    bool

	InitForceFn  = func() bool { return initForce }
	InitDryRunFn = func() bool { return initDryRun }
	InitYesFn    = func() bool { return initYes }
)

// InitDirMakerForTest re-exports the file-private initDirMaker
// interface so the commands package's seams_test.go can pass its own
// fault-injection double to RunInitForTest / ScaffoldWorkflowAssetsForTest
// without lifecycle exposing its full internal type. Folded out in t11
// when seams_test.go itself relocates here.
type InitDirMakerForTest = initDirMaker

// RunInitForTest is the export-for-test bridge for runInit, used by the
// parent package's seams_test.go fault-injection cases until t11
// relocates them. Production callers go through NewInitCmd.
func RunInitForTest(cmd *cobra.Command, args []string, deps InitDirMakerForTest) error {
	return runInit(cmd, args, deps)
}

// ScaffoldWorkflowAssetsForTest mirrors RunInitForTest for the
// scaffoldWorkflowAssets MkdirAll fault-injection test in seams_test.go.
func ScaffoldWorkflowAssetsForTest(agentsHome string, deps InitDirMakerForTest) error {
	return scaffoldWorkflowAssets(agentsHome, deps)
}

// SetInitFlags is the shim hook for repointing the init force/dry-run/
// yes getters at the parent commands.Flags. Callers can pass nil for a
// getter to leave the in-package default in place (used by tests that
// only need to flip a subset of the seams).
func SetInitFlags(force, dryRun, yes func() bool) {
	if force != nil {
		InitForceFn = force
	}
	if dryRun != nil {
		InitDryRunFn = dryRun
	}
	if yes != nil {
		InitYesFn = yes
	}
}

// InitUsageErrorFn is the UsageError seam (defaults to a plain error so
// the lifecycle subpackage does not import commands, avoiding the
// commands→lifecycle→commands cycle). The shim in commands/init.go
// overrides this with commands.UsageError to preserve the formatted-hint
// UX users see today.
var InitUsageErrorFn = func(msg string, hints ...string) error {
	if len(hints) == 0 {
		return fmt.Errorf("%s", msg)
	}
	return fmt.Errorf("%s\n  %s", msg, strings.Join(hints, "\n  "))
}

// InitEnsureGlobalKGMCPConfigsFn is the in-package seam for the KG MCP
// config scaffolder. Defaults to lifecycle.EnsureGlobalKGMCPConfigs
// (lifted into this package by t02b), eliminating the prior cross-package
// indirection through the commands shim. Kept as a var (not a direct
// call) so tests can fault-inject failure scenarios without monkey-patching
// the underlying helper.
var InitEnsureGlobalKGMCPConfigsFn = EnsureGlobalKGMCPConfigs

// initDirMaker is the narrow collaborator init.go's fault-injectable
// operations need (interface-DI per docs/TEST_SEAMS.md). Single-method
// today, named with the -er suffix per Go style; rename to a multi-method
// role name (cf. dirCleaner, schemaCompiler) if init.go grows additional
// seam needs later. File-scoped — do not share with other commands files.
type initDirMaker interface {
	MkdirAll(path string, perm os.FileMode) error
}

// stdInitDirMaker is the production initDirMaker backed by the os package.
type stdInitDirMaker struct{}

func (stdInitDirMaker) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

// Command-text constants for the init cobra command. The shim in
// commands/init.go constructs the *cobra.Command literal using these so
// the RunE closure (and thus the runtime symbol globalflagcov resolves)
// lives in package commands, not lifecycle. This keeps the t05 move
// transparent to the existing globalflagcov tool — folded into a single
// constructor when t13 deletes the shim and globalflagcov gains
// lifecycle in its package list.
const (
	InitCmdUse   = "init"
	InitCmdShort = "Initialize ~/.agents/ directory structure"
	InitCmdLong  = `Creates the ~/.agents/ directory structure with starter templates.
Safe to run multiple times - existing files are preserved unless --force.

Run this once per machine before using add, install, refresh, or workflow
commands that expect the shared store to exist.`
	InitCmdNoArgsHint = "`da init` bootstraps the shared store and does not take a project path."
)

// InitCmdExample is the rendered Example string for the init command.
var InitCmdExample = strings.Join([]string{
	"  da init",
	"  da init --dry-run",
	"  da init --force",
	"  da init --from git@github.com:you/agents-config.git",
}, "\n")

// RunInit is the exported entry the shim's RunE closure calls. It
// fronts the unexported runInit (which keeps the initDirMaker seam
// file-scoped to lifecycle).
func RunInit(cmd *cobra.Command, args []string) error {
	return runInit(cmd, args, stdInitDirMaker{})
}

// NewInitCmd builds the `da init` cobra command. Mirrors the
// NewInstallCmd / NewDoctorCmd Deps-injection pattern so the t13b worker
// can call `lifecycle.NewInitCmd(buildLifecycleDeps())` without any
// further wiring. Added in t13a — before this, the parent
// commands/init.go shim owned the cobra literal and used SetInitFlags +
// InitUsageErrorFn package-var seam writes to repoint the per-init
// getters at commands.Flags / commands.UsageError.
//
// Construction-time wiring:
//
//  1. SetInitFlags is called with closures over deps so InitForceFn /
//     InitDryRunFn / InitYesFn read live state at RunE time (cobra
//     parses flags AFTER constructor return; a value snapshot taken at
//     construction would be stale). FlagsFn takes precedence over the
//     Deps.Flags value, mirroring applyDepsToGlobals.
//
//  2. InitUsageErrorFn is repointed at deps.UsageError when non-nil so
//     init's positional-arg rejection renders through commands.UsageError's
//     formatted-hint UX. When deps.UsageError is nil (lifecycle-only unit
//     tests that don't construct a hint formatter) the in-package default
//     InitUsageErrorFn is preserved.
//
// The RunE wrapper additionally calls applyDepsToGlobals(deps) so
// downstream helpers that read lifecycle.Flags / .ErrorWithHintsFn /
// .Version / .Commit / .Describe (none for init today, but symmetric with
// NewInstallCmd / NewDoctorCmd) observe the same Deps-derived state.
//
// Compatible with the existing parent commands/init.go shim, which sets
// the same seams via SetInitFlags + direct InitUsageErrorFn assignment
// before constructing its own cobra literal — t13b's shim deletion will
// switch root.go to call NewInitCmd(buildLifecycleDeps()) directly and
// drop the parent shim's manual wiring.
func NewInitCmd(deps Deps) *cobra.Command {
	wireInitSeamsFromDeps(deps)

	cmd := &cobra.Command{
		Use:     InitCmdUse,
		Short:   InitCmdShort,
		Long:    InitCmdLong,
		Example: InitCmdExample,
		Args:    InitNoArgs(InitCmdNoArgsHint),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Re-wire on every invocation in case the Deps closure mutated
			// (defensive — production callers construct Deps once at root
			// composition time, but tests can swap them between calls).
			wireInitSeamsFromDeps(deps)
			applyDepsToGlobals(deps)
			return RunInit(cmd, args)
		},
	}
	// --from <home-source> bootstraps ~/.agents from a remote home (git URL) —
	// the L3 cross-machine adoption path (init_from.go). Read at RunE time via
	// initFromValue so a bare lifecycle-only test command (no --from registered)
	// safely falls back to the fresh-local scaffold.
	cmd.Flags().String(initFromFlag, "", "Bootstrap ~/.agents from a remote home source (git URL) — cross-machine adoption")
	return cmd
}

// wireInitSeamsFromDeps installs SetInitFlags closures + repoints
// InitUsageErrorFn from the supplied Deps. Used by NewInitCmd both at
// construction time (so a caller that introspects InitForceFn before
// RunE observes the wired value) and on every RunE invocation (defensive
// against test mutation between calls).
//
// FlagsFn takes precedence over deps.Flags on each call (cobra mutates
// the upstream flag state after constructor return; live reads are
// required). When deps.UsageError is nil the in-package default
// InitUsageErrorFn is preserved so lifecycle-only unit tests without a
// hint formatter still produce a readable error.
func wireInitSeamsFromDeps(deps Deps) {
	live := func() GlobalFlags {
		if deps.FlagsFn != nil {
			return deps.FlagsFn()
		}
		return deps.Flags
	}
	SetInitFlags(
		func() bool { return live().Force },
		func() bool { return live().DryRun },
		func() bool { return live().Yes },
	)
	if deps.UsageError != nil {
		InitUsageErrorFn = deps.UsageError
	}
}

// InitNoArgs is the exported Args validator for the init command,
// delegating to the file-local initNoArgs implementation. Exported so
// the shim can wire it into the cobra command literal.
func InitNoArgs(hints ...string) cobra.PositionalArgs {
	return initNoArgs(hints...)
}

// initNoArgs is a file-local mirror of commands.NoArgsWithHints. It
// renders the same "does not accept positional arguments" usage error
// via InitUsageErrorFn, which the shim repoints at commands.UsageError
// so user-visible hint formatting is preserved. Kept file-private (not
// promoted to a lifecycle-wide helper) for the same scope reason
// documented on initForce.
func initNoArgs(hints ...string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return nil
		}
		all := append([]string{
			fmt.Sprintf("Usage: %s", cmd.UseLine()),
			fmt.Sprintf("Run `%s --help` to see examples and supported flags.", cmd.CommandPath()),
		}, hints...)
		return InitUsageErrorFn(
			fmt.Sprintf("%s does not accept positional arguments (got %d)", cmd.CommandPath(), len(args)),
			all...,
		)
	}
}

// reportExistingInstall logs the existing-~/.agents state and returns true if
// init should halt (a home already exists and --force was not given).
func reportExistingInstall(agentsHome string) bool {
	ui.Step("Checking existing installation...")
	if _, err := os.Stat(agentsHome); err != nil {
		ui.Bullet("none", "No existing ~/.agents/ found")
		return false
	}
	if !InitForceFn() {
		ui.Bullet("found", "Existing ~/.agents/ directory found")
		fmt.Fprintln(os.Stdout, "\n  Use --force to reinitialize (creates backup first)")
		return true
	}
	ui.Bullet("warn", "Will reinitialize (--force)")
	return false
}

func runInit(cmd *cobra.Command, args []string, deps initDirMaker) error {
	// `da init --from <home-source>` is the L3 cross-machine bootstrap: it clones
	// a remote home into ~/.agents and re-materializes the user surface, instead
	// of scaffolding a fresh-local home from embedded starters (init_from.go).
	if from := initFromValue(cmd); from != "" {
		return runInitFrom(cmd, from, deps)
	}

	agentsHome := config.AgentsHome()

	ui.Header("da init")

	warnLegacyManifestInCwd()

	if reportExistingInstall(agentsHome) {
		return nil
	}

	if InitDryRunFn() {
		ui.DryRun("Create ~/.agents/ directory structure")
		fmt.Fprintln(os.Stdout, "\nDRY RUN - no changes made")
		return nil
	}

	if !InitYesFn() {
		if !ui.Confirm("Proceed with initialization?", false) {
			ui.Info("Initialization cancelled.")
			return nil
		}
	}

	ui.Step("Creating directories and files...")

	if err := createInitialAgentsDirs(agentsHome, deps); err != nil {
		return err
	}
	ui.Bullet("ok", "Created directory structure")

	if err := seedInitialConfig(agentsHome); err != nil {
		return err
	}

	if err := scaffoldStarterHomeAssets(agentsHome); err != nil {
		return fmt.Errorf("scaffolding starter home assets: %w", err)
	}
	ui.Bullet("ok", "Scaffolded starter home assets")

	if err := scaffoldWorkflowAssets(agentsHome, deps); err != nil {
		return fmt.Errorf("scaffolding starter hook bundles: %w", err)
	}
	ui.Bullet("ok", "Scaffolded starter workflow hook bundles")

	if err := InitEnsureGlobalKGMCPConfigsFn(agentsHome); err != nil {
		return fmt.Errorf("scaffolding starter KG MCP configs: %w", err)
	}

	// Global Claude Code settings symlink — hooks/ takes priority over settings/
	if err := linkClaudeGlobalSettings(agentsHome, deps); err != nil {
		return err
	}
	if err := linkCursorGlobalHooks(agentsHome, deps); err != nil {
		return err
	}

	// State dir — best-effort idempotent create; a real failure here must not
	// be reported as "ok" (MkdirAll already no-ops when the dir exists, so
	// any error is a genuine I/O/permission fault, not legitimate absence).
	if err := deps.MkdirAll(config.AgentsStateDir(), 0755); err != nil {
		ui.Warn(fmt.Sprintf("could not create state directory %s: %v", config.AgentsStateDir(), err))
	} else {
		ui.Bullet("ok", "Created state directory")
	}

	ui.SuccessBox("Initialization complete!",
		"Add your first project: da add ~/path/to/project",
		"See the canonical layout: da explain structure",
		"Team member with manifest: da install  (instead of add)",
		"Set up git sync: da sync init",
		"Check health: da doctor",
	)
	return nil
}

// warnLegacyManifestInCwd surfaces a v1 deprecation notice when the current
// working directory holds a legacy (pre-v2) .agentsrc.json. da init bootstraps
// the shared store and writes v2-shaped manifests, so a v1 file in the repo
// being initialized is worth flagging — the file still loads, the warning only
// nudges toward v2 (config-v2 §15.3). Best-effort: a missing/unreadable
// manifest is silent (the common fresh-repo case).
func warnLegacyManifestInCwd() {
	cwd, err := os.Getwd()
	if err != nil {
		return
	}
	rc, err := config.LoadAgentsRC(cwd)
	if err != nil {
		return
	}
	if w := config.DetectV1Deprecation(rc); w.Detected {
		ui.Bullet("warn", w.Message()+"  hint: da config migrate")
	}
}

// sidecarBackupFile preserves an unmanaged occupant before links replaces it
// with a managed link in init's --force path. It mirrors the established
// internal/platform convention: write the existing bytes to a sibling
// <path>.dot-agents-backup. links calls this BEFORE removing the entry and
// only proceeds with replacement if it returns nil, so a backup failure
// aborts the replace and leaves the user's file intact (no data loss).
func sidecarBackupFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s for backup: %w", path, err)
	}
	bak := path + ".dot-agents-backup"
	if err := os.WriteFile(bak, data, 0644); err != nil {
		return fmt.Errorf("write backup %s: %w", bak, err)
	}
	return nil
}

func scaffoldStarterHomeAssets(agentsHome string) error {
	return scaffoldhome.CopyMissingStarterAssets(agentsHome)
}

func scaffoldWorkflowAssets(agentsHome string, deps initDirMaker) error {
	if err := deps.MkdirAll(config.AgentsContextDir(), 0755); err != nil {
		return err
	}
	return scaffoldhooks.CopyMissingGlobalBundles(filepath.Join(agentsHome, "hooks", "global"))
}

// createInitialAgentsDirs creates the canonical ~/.agents/ directory
// shape plus per-platform CanonicalStoreBucket scope roots. Each
// MkdirAll goes through the injected deps so the error branch is
// fault-injectable. Idempotent on re-run.
func createInitialAgentsDirs(agentsHome string, deps initDirMaker) error {
	dirs := []string{
		agentsHome,
		filepath.Join(agentsHome, "resources"),
		filepath.Join(agentsHome, "rules", "global"),
		filepath.Join(agentsHome, "settings", "global"),
		filepath.Join(agentsHome, "mcp", "global"),
		filepath.Join(agentsHome, "skills", "global"),
		filepath.Join(agentsHome, "agents", "global"),
		filepath.Join(agentsHome, "hooks", "global"),
		config.AgentsContextDir(),
		filepath.Join(agentsHome, "scripts"),
		filepath.Join(agentsHome, "local"),
	}
	for _, bucket := range platform.CanonicalStoreBucketSpecs() {
		dirs = append(dirs, platform.CanonicalBucketScopeRoot(agentsHome, bucket.Name, "global"))
	}
	for _, d := range dirs {
		if err := deps.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("creating %s: %w", d, err)
		}
	}
	return nil
}

// seedInitialConfig writes ~/.agents/config.json when it does not
// already exist (or under --force). Detects installed platforms and
// records their state + version so subsequent commands can rely on a
// pre-populated registry.
func seedInitialConfig(agentsHome string) error {
	cfgPath := filepath.Join(agentsHome, "config.json")
	_, statErr := os.Stat(cfgPath)
	switch {
	case statErr == nil && !InitForceFn():
		return nil // config.json already exists; not forcing overwrite.
	case statErr != nil && !os.IsNotExist(statErr) && !InitForceFn():
		// A real Stat error (permission denied, etc.) is not the same as
		// "config.json doesn't exist yet" — surface it instead of silently
		// skipping config creation (da init would otherwise report success
		// without writing the foundational config file).
		return fmt.Errorf("checking for existing config.json: %w", statErr)
	}
	cfg := &config.Config{
		Version:  1,
		Projects: make(map[string]config.Project),
		Agents:   make(map[string]config.Agent),
	}
	ui.Section("Detected Platforms")
	for _, p := range platform.All() {
		recordPlatformState(cfg, p)
	}
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}
	return nil
}

// recordPlatformState writes one platform's detected presence + version
// into cfg and renders the bullet line. Pulled out so seedInitialConfig
// has a flat control flow (one branch per platform, no nested
// installed/version conditionals).
func recordPlatformState(cfg *config.Config, p platform.Platform) {
	if !p.IsInstalled() {
		cfg.SetPlatformState(p.ID(), false, "")
		ui.Bullet("none", p.DisplayName()+" (not detected)")
		return
	}
	ver := p.Version()
	cfg.SetPlatformState(p.ID(), true, ver)
	if ver != "" {
		ui.Bullet("ok", fmt.Sprintf("%s (%s)", p.DisplayName(), ver))
	} else {
		ui.Bullet("ok", p.DisplayName())
	}
}

// linkClaudeGlobalSettings creates the global ~/.claude/settings.json
// symlink when Claude Code is installed. Hooks/global/claude-code.json
// takes priority over settings/global/claude-code.json. --force routes
// through the backup-preserving link so an unmanaged user file is kept
// as <path>.dot-agents-backup rather than destroyed; link errors are
// propagated (links returns ErrUnmanagedTarget for unmanaged occupants
// and swallowing it would print false success).
func linkClaudeGlobalSettings(agentsHome string, deps initDirMaker) error {
	claudePlatform := platform.ByID("claude")
	if claudePlatform == nil || !claudePlatform.IsInstalled() {
		return nil
	}
	claudeHooksSrc := filepath.Join(agentsHome, "hooks", "global", "claude-code.json")
	claudeSettingsPath := filepath.Join(agentsHome, "settings", "global", "claude-code.json")
	if _, err := os.Stat(claudeHooksSrc); err == nil {
		claudeSettingsPath = claudeHooksSrc
	}
	home := config.UserHome()
	claudeDir := filepath.Join(home, ".claude")
	_ = deps.MkdirAll(claudeDir, 0755) // best-effort; SymlinkReplacing surfaces any real failure
	claudeSettings := filepath.Join(claudeDir, "settings.json")
	if _, err := os.Lstat(claudeSettings); !(os.IsNotExist(err) || InitForceFn()) {
		ui.Bullet("skip", "~/.claude/settings.json exists (use --force to replace)")
		return nil
	}
	if err := links.SymlinkReplacing(claudeSettingsPath, claudeSettings, sidecarBackupFile); err != nil {
		return fmt.Errorf("linking %s: %w", claudeSettings, err)
	}
	ui.Bullet("ok", "Created Claude Code global settings symlink")
	return nil
}

// linkCursorGlobalHooks creates the global ~/.cursor/hooks.json hardlink
// when Cursor is installed and the source hooks file exists. Same
// backup-preserving contract as linkClaudeGlobalSettings.
func linkCursorGlobalHooks(agentsHome string, deps initDirMaker) error {
	cursorPlatform := platform.ByID("cursor")
	if cursorPlatform == nil || !cursorPlatform.IsInstalled() {
		return nil
	}
	cursorHooksSrc := filepath.Join(agentsHome, "hooks", "global", "cursor.json")
	if _, err := os.Stat(cursorHooksSrc); err != nil {
		return nil
	}
	home := config.UserHome()
	cursorDir := filepath.Join(home, ".cursor")
	_ = deps.MkdirAll(cursorDir, 0755) // best-effort; HardlinkReplacing surfaces any real failure
	cursorHooksDst := filepath.Join(cursorDir, "hooks.json")
	if _, err := os.Lstat(cursorHooksDst); !(os.IsNotExist(err) || InitForceFn()) {
		ui.Bullet("skip", "~/.cursor/hooks.json exists (use --force to replace)")
		return nil
	}
	if err := links.HardlinkReplacing(cursorHooksSrc, cursorHooksDst, sidecarBackupFile); err != nil {
		return fmt.Errorf("linking %s: %w", cursorHooksDst, err)
	}
	ui.Bullet("ok", "Created Cursor global hooks hardlink")
	return nil
}
