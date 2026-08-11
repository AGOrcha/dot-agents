package commands

import (
	"fmt"
	"os"
	"strings"

	"github.com/AGOrcha/dot-agents/commands/internal/lifecycle"
	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/platform"
	"github.com/AGOrcha/dot-agents/internal/projectsync"
	"github.com/AGOrcha/dot-agents/internal/ui"
	"github.com/spf13/cobra"
)

// Version, Commit, and Describe are set at build time via ldflags.
var Version = "dev"
var Commit = ""
var Describe = ""
var refreshImport bool

// refreshInexact opts out of the EXACT/PRUNE outputs projection
// (config-v2-coherence §7A.5 / D10). Default false ⇒ refresh projects the
// resolved asset-store union AND prunes managed outputs no longer in the
// resolved set, so the repo tree converges to exactly what the lock declares.
// True (`--inexact`) keeps the additive behavior: write the wanted set, leave
// stale managed outputs in place. The cobra `--inexact` flag is registered in
// commands/internal/lifecycle/refresh.go and threaded here via the RunRefresh
// closure in NewRefreshCmd.
var refreshInexact bool

// refreshConfigLoader is the narrow collaborator refresh.go's
// fault-injectable LoadConfig operation needs (interface-DI per
// docs/TEST_SEAMS.md). Single-method, file-prefixed -er form; file-scoped
// — do not share with other commands files.
type refreshConfigLoader interface {
	LoadConfig() (*config.Config, error)
}

// stdRefreshConfigLoader is the production refreshConfigLoader backed by
// internal/config.Load.
type stdRefreshConfigLoader struct{}

func (stdRefreshConfigLoader) LoadConfig() (*config.Config, error) { return config.Load() }

// NewRefreshCmd is the root-package shim that wires the lifecycle
// subpackage constructor with closures over the still-in-root runRefresh
// body and package-var seams. Follows SHAPE.md §6 (root shim preserved
// until t13 deletes it and switches root.go to lifecycle.NewRefreshCmd
// directly). See .agents/active/fold-back/t07-refresh-body-deferred.md
// for why the run body cannot move atomically with this PR (t04/t06 still
// own addDeps / importDeps in the root package).
func NewRefreshCmd() *cobra.Command {
	return lifecycle.NewRefreshCmd(lifecycle.Deps{
		ExampleBlock:          ExampleBlock,
		MaximumNArgsWithHints: MaximumNArgsWithHints,
		RunRefresh: func(projectFilter string, importAlso, inexact bool) error {
			refreshImport = importAlso
			refreshInexact = inexact
			return runRefresh(projectFilter, stdRefreshConfigLoader{}, stdImportDeps{}, stdAddDeps{})
		},
	})
}

func runRefresh(projectFilter string, deps refreshConfigLoader, importD importDeps, addD addDeps) error {
	if err := runImportFromRefresh(projectFilter, refreshImportScope(), importD); err != nil {
		return fmt.Errorf("import before refresh: %w", err)
	}

	cfg, err := deps.LoadConfig()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if len(cfg.Projects) == 0 {
		ui.Info("No managed projects. Add one with: da add <path>")
		return nil
	}

	ui.Header("da refresh")

	enabledPlatforms := reportEnabledPlatforms(cfg)
	cfg.Save()
	if len(enabledPlatforms) == 0 {
		ui.Warn("No enabled platforms in config.json. Nothing to refresh.")
		return nil
	}

	installedEnabled := platform.InstalledEnabledPlatforms(cfg)
	refreshCommit, refreshDescribe := resolveRefreshCommit()

	projects, err := resolveRefreshProjects(cfg, projectFilter)
	if err != nil {
		return err
	}

	total := len(projects)
	count := 0
	var failed []string
	for i, name := range projects {
		path := cfg.GetProjectPath(name)
		if !checkRefreshProjectPath(name, path) {
			continue
		}
		announceRefreshProject(name, path, i, total)
		noteManifestGitSources(path)

		projectFailed := refreshOneProject(name, path, enabledPlatforms, installedEnabled, addD)

		stamped := finalizeProjectRefresh(name, path, projectFailed, refreshCommit, refreshDescribe)
		if stamped {
			count++
		} else if !Flags.DryRun {
			failed = append(failed, name)
		}
	}

	fmt.Fprintln(os.Stdout)
	if count == 0 && len(failed) == 0 {
		ui.Info("Nothing to refresh.")
	} else if count > 0 {
		ui.Success(fmt.Sprintf("Refreshed %d project(s).", count))
	}
	if len(failed) > 0 {
		return ErrorWithHints(
			fmt.Sprintf("refresh incomplete for %d project(s): %s", len(failed), strings.Join(failed, ", ")),
			"The listed projects were NOT marked refreshed (partial application). Re-run after resolving the warnings above; unmanaged files in the way must be imported, backed up, or removed.",
		)
	}
	return nil
}

// reportEnabledPlatforms prints the "Enabled Platforms" section and returns
// the slice of platforms enabled in cfg. Installed platforms have their
// version recorded back into cfg so the caller can persist via cfg.Save().
func reportEnabledPlatforms(cfg *config.Config) []platform.Platform {
	ui.Section("Enabled Platforms")
	// Re-probe for platforms installed AFTER `da init` and enable them so a
	// newly-installed editor becomes managed on the next refresh (rather than
	// staying enabled:false forever). The mutation lands in cfg before the
	// loop below, so each newly-enabled platform is also projected this run.
	for _, name := range lifecycle.DetectAndEnableNewPlatforms(cfg) {
		ui.Bullet("ok", "Detected and enabled: "+name)
	}
	enabled := []platform.Platform{}
	for _, p := range platform.All() {
		if !cfg.IsPlatformEnabled(p.ID()) {
			continue
		}
		enabled = append(enabled, p)
		if !p.IsInstalled() {
			ui.Bullet("none", p.DisplayName()+" (enabled, not detected)")
			continue
		}
		ver := p.Version()
		cfg.SetPlatformState(p.ID(), true, ver)
		if ver != "" {
			ui.Bullet("ok", fmt.Sprintf("%s (%s)", p.DisplayName(), ver))
		} else {
			ui.Bullet("ok", p.DisplayName())
		}
	}
	return enabled
}

// resolveRefreshProjects returns the project list to refresh: every managed
// project, or just the filter target when one was provided. An unknown filter
// produces a typed error with a recovery hint.
func resolveRefreshProjects(cfg *config.Config, projectFilter string) ([]string, error) {
	if projectFilter == "" {
		return cfg.ListProjects(), nil
	}
	if !cfg.IsProjectKnown(projectFilter) {
		return nil, ErrorWithHints(
			fmt.Sprintf("project not found: %s", projectFilter),
			"Run `da status` to see the registered project names.",
		)
	}
	return []string{projectFilter}, nil
}

// checkRefreshProjectPath reports whether the project's recorded path is a
// real, present directory. It emits the user-facing warn on skip so callers
// just consult the bool.
func checkRefreshProjectPath(name, path string) bool {
	const skipPrefix = "Skipping "
	if path == "" {
		// Known in the synced identity registry but with no machine-local
		// binding — report it explicitly rather than silently skip-as-missing
		// (R4). This is the expected machine-B state before `da add` rebinds it.
		ui.Warn(name + ": known but unbound on this machine — run `da add <path>` to bind it")
		return false
	}
	if path == "." {
		ui.Warn(skipPrefix + name + ": path not found")
		return false
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			ui.Warn(skipPrefix + name + ": directory not found at " + path)
		} else {
			// A REAL Stat error (permission denied, TOCTOU) is not the same
			// as "directory not found" — the path may well exist. Say so
			// instead of sending the operator hunting for a directory that's
			// actually just inaccessible.
			ui.Warn(skipPrefix + name + ": could not access " + path + ": " + err.Error())
		}
		return false
	}
	return true
}

// announceRefreshProject prints the per-project banner — StepN heading when
// processing multiple projects, plain bold name for a single-project run —
// followed by the dimmed display path.
func announceRefreshProject(name, path string, i, total int) {
	if total > 1 {
		ui.StepN(i+1, total, name)
	} else {
		fmt.Fprintf(os.Stdout, "\n%s\n", ui.BoldText(name))
	}
	fmt.Fprintf(os.Stdout, "  %s\n", ui.DimText(config.DisplayPath(path)))
}

// noteManifestGitSources prints the one-shot hint that the project's manifest
// has git sources and `install` (not `refresh`) is the way to re-resolve them.
// A missing or unreadable manifest is silently skipped — refresh is
// well-defined for manifest-less projects.
func noteManifestGitSources(path string) {
	rc, err := config.LoadAgentsRC(path)
	if err != nil {
		return
	}
	for _, src := range rc.Sources {
		if src.Type == "git" {
			fmt.Fprintf(os.Stdout, "  %sℹ  .agentsrc.json has git sources — use 'da install' to re-resolve%s\n", ui.Dim, ui.Reset)
			return
		}
	}
}

// refreshOneProject performs the per-project body: optional restore-from-
// resources, shared-target projection, and CreateLinks across every enabled
// platform. Returns true when ANY sub-step failed so the caller can withhold
// the success-stamp from a partial application.
func refreshOneProject(name, path string, enabledPlatforms, installedEnabled []platform.Platform, addD addDeps) bool {
	projectFailed := false
	if !Flags.DryRun {
		projectsync.CreateProjectDirs(name)
		if err := restoreFromResources(name, path, addD); err != nil {
			ui.Bullet("warn", fmt.Sprintf("restore from resources: %v", err))
			projectFailed = true
		}
	}

	ensureRes := ensureLockFreshForRefresh(path)

	config.SetWindowsMirrorContext(path)

	packagesUnits, packagesParticipated, perr := hydrateRefreshPackages(path, name, ensureRes)
	if perr != nil {
		// A packages hydration failure (e.g. a transient fetch error) must NOT
		// fall through to an exact projection with an EMPTY package set: its
		// forced one-to-zero prune would delete the project's already-installed
		// package links, so a momentary fetch failure would destroy installed
		// packages. Skip shared-target projection for this project entirely,
		// leaving every prior link (local-authored and package) intact; the
		// per-platform CreateLinks below is additive (no prune) so the rest of
		// the refresh still runs. (review #2)
		ui.Bullet("warn", fmt.Sprintf("resolving packages: %v — leaving existing links untouched (skipping projection)", perr))
		projectFailed = true
	} else if runSharedTargetsForRefresh(name, path, installedEnabled, packagesUnits, packagesParticipated) {
		projectFailed = true
	}
	if recreatePlatformLinks(name, path, enabledPlatforms) {
		projectFailed = true
	}
	if ensureManagedGitignoreForRefresh(path, enabledPlatforms) {
		projectFailed = true
	}
	return projectFailed
}

// ensureManagedGitignoreForRefresh writes the dot-agents-managed .gitignore
// block (config-distribution-model §15 / D14 / R8) so every enabled platform's
// projected/generated repo-local outputs are ignored while the committed
// .agentsrc.json/.agentsrc.lock contract stays tracked. The output set is
// collected from the platforms themselves (platform.CollectManagedOutputs), not
// hardcoded here, so refresh never has to know each platform's surface. It is
// keyed off the config-enabled set (not install state) so the committed block
// is byte-stable across machines regardless of which platforms are installed.
// links.EnsureManagedGitignore regenerates (not appends) a sorted, de-duplicated
// block and preserves user-authored ignores outside the markers, so re-running
// refresh is idempotent. Dry-run previews the update without touching the file.
// Returns true when a non-dry-run write failed so the caller withholds the
// success stamp.
//
// The knob check and write/remove decision are shared with `da install` via
// lifecycle.MaintainManagedGitignore, so the two commands cannot leave different
// blocks behind on the same repo.
func ensureManagedGitignoreForRefresh(path string, enabledPlatforms []platform.Platform) bool {
	if Flags.DryRun {
		ui.DryRun("Update dot-agents managed .gitignore block")
		return false
	}
	line, err := lifecycle.MaintainManagedGitignore(path, enabledPlatforms)
	if err != nil {
		ui.Bullet("warn", fmt.Sprintf("managed .gitignore: %v", err))
		return true
	}
	ui.Bullet("ok", line)
	return false
}

// ensureLockFreshForRefresh runs the §7A.5 lock half (config.EnsureResolved)
// before refresh projects outputs, so the asset-store union being projected
// reflects a current lock (D12: "refresh — ensures lock fresh first"). It is
// manifest-gated and best-effort: a project with no .agentsrc.json is a
// well-defined manifest-less refresh (skip silently, matching
// noteManifestGitSources); a resolution error is surfaced as a warning but
// does not fail refresh — the projection step still runs against the existing
// lock. Refresh re-resolves LOCAL scopes only (default EnsureResolved); the
// explicit upstream re-check is `da config sync`, never refresh (D10/D12).
// Dry-run skips the (lock-writing) re-resolve entirely.
//
// Returns the EnsureResult (nil under dry-run, manifest-less, or a resolution
// error) so hydrateRefreshPackages can mirror pass-1's write/no-write
// decision (H9) instead of re-deriving it. UnitDigest is the H7 production
// artifact-store integrity resolver — see packagesArtifactDigestResolver.
func ensureLockFreshForRefresh(path string) *config.EnsureResult {
	if Flags.DryRun {
		return nil
	}
	if _, err := config.LoadAgentsRC(path); err != nil {
		return nil
	}
	res, err := config.EnsureResolved(path, config.EnsureOpts{
		UnitDigest: lifecycle.PackagesArtifactDigestResolver(path),
	})
	if err != nil {
		ui.Bullet("warn", fmt.Sprintf("ensure lock fresh: %v", err))
		return nil
	}
	return res
}

// hydrateRefreshPackages runs pass 2 (H9/H13) after pass-1 config resolution
// and before shared-target projection. Skipped (nil, nil) whenever
// ensureRes is nil (dry-run, manifest-less, or a pass-1 resolution failure —
// refresh already warns on the latter and proceeds against the existing lock,
// so pass-2 is a no-op rather than compounding the warning) or the effective
// config declares no packages[] (HydratePackagesUnits).
func hydrateRefreshPackages(path, name string, ensureRes *config.EnsureResult) ([]platform.ResolvedUnit, bool, error) {
	if ensureRes == nil {
		return nil, false, nil
	}
	return lifecycle.HydratePackagesUnits(path, name, ensureRes)
}

// runSharedTargetsForRefresh runs the shared-target projection and prints any
// dry-run plan lines. Returns true when a non-dry-run projection failed
// (caller withholds the success stamp); dry-run failures are surfaced as
// warnings but do not propagate.
//
// The projection is EXACT/PRUNE by default (config-v2-coherence §7A.5): it
// projects the resolved set AND prunes managed outputs no longer in it, so the
// repo converges to exactly what the lock declares. `--inexact` (refreshInexact)
// opts out, keeping the additive write-only behavior. A prune failure is folded
// into the same warn-and-fail path as a write failure so a partial application
// withholds the success stamp.
func runSharedTargetsForRefresh(name, path string, installedEnabled []platform.Platform, units []platform.ResolvedUnit, packagesParticipated bool) bool {
	var lines []string
	var err error
	if packagesParticipated {
		lines, err = platform.ProjectResolvedUnits(name, path, units, installedEnabled, Flags.DryRun, !refreshInexact, name)
	} else {
		lines, err = platform.RunSharedTargetProjectionExact(name, path, installedEnabled, Flags.DryRun, !refreshInexact)
	}
	if err != nil {
		if Flags.DryRun {
			ui.Bullet("warn", fmt.Sprintf("shared targets plan: %v", err))
			return false
		}
		ui.Bullet("warn", fmt.Sprintf("shared targets: %v", err))
		return true
	}
	for _, line := range lines {
		ui.DryRun(line)
	}
	return false
}

// recreatePlatformLinks re-runs CreateLinks for every enabled+installed
// platform. Returns true when any platform's CreateLinks failed.
func recreatePlatformLinks(name, path string, enabledPlatforms []platform.Platform) bool {
	failed := false
	for _, p := range enabledPlatforms {
		if !p.IsInstalled() {
			ui.Skip(p.DisplayName() + " (not installed)")
			continue
		}
		if Flags.DryRun {
			ui.DryRun("Refresh " + p.DisplayName() + " links")
			continue
		}
		if err := p.CreateLinks(name, path); err != nil {
			ui.Bullet("warn", fmt.Sprintf("%s: %v", p.DisplayName(), err))
			failed = true
			continue
		}
		ui.Bullet("ok", p.DisplayName()+" links refreshed")
	}
	return failed
}

// finalizeProjectRefresh writes the refresh metadata stamp when the project
// finished cleanly. Returns true on a successful stamp (counted toward the
// success total) and false on dry-run, partial application, or stamp failure.
// Dry-run is treated as success for the counter but skips the lock write.
func finalizeProjectRefresh(name, path string, projectFailed bool, refreshCommit, refreshDescribe string) bool {
	if Flags.DryRun {
		msg := "Update .agentsrc.lock refresh details"
		if refreshCommit != "" {
			msg += " (commit=" + refreshCommit[:8] + ")"
		}
		ui.DryRun(msg)
		return true
	}
	if projectFailed {
		ui.Bullet("warn", "skipping refresh metadata for "+name+" — refresh was partial")
		return false
	}
	if err := projectsync.WriteRefreshToLock(name, path, Version, refreshCommit, refreshDescribe); err != nil {
		ui.Bullet("warn", fmt.Sprintf("lock refresh metadata: %v", err))
		return false
	}
	return true
}

func refreshImportScope() string {
	if refreshImport {
		return importScopeAll
	}
	return importScopeProject
}

// resolveRefreshCommit returns the commit hash and describe string embedded at build time.
// Falls back to empty strings for dev builds.
func resolveRefreshCommit() (string, string) {
	return Commit, Describe
}

// restoreFromResources restores files from ~/.agents/resources/<project>/.
// It returns a non-nil error if any walk/mkdir/write/copy failed so callers
// that stamp success metadata can treat a partial restore as a failure
// instead of a silent false-success.
func restoreFromResources(project, projectPath string, deps addDeps) error {
	_, err := restoreFromResourcesCountedWithDeps(project, projectPath, deps)
	return err
}

// mapResourceRelToDest is a thin alias over lifecycle.MapResourceRelToDest
// (lifted in root-command-decomposition t02b). Kept as a function-var
// alias so existing call sites in refresh.go, add.go, and import.go stay
// unchanged until those commands move.
var mapResourceRelToDest = lifecycle.MapResourceRelToDest
