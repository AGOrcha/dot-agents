package lifecycle

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/AGOrcha/dot-agents/internal/agentslock"
	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/platform"
	"github.com/AGOrcha/dot-agents/internal/projectsync"
	"github.com/AGOrcha/dot-agents/internal/ui"
	"github.com/spf13/cobra"
)

// InstallDeps is the multi-method collaborator RunInstall, RunInstallGenerate,
// and their helpers need (interface-DI per docs/TEST_SEAMS.md). File-scoped —
// do not share with other commands files. The four operations are the install
// pipeline's fault-injectable touch points: working-directory resolution
// (Getwd), filesystem materialization of resource link parents and git cache
// roots (MkdirAll), the resource symlink itself (Symlink), and config.json
// load (LoadConfig) for project registration and lookup.
type InstallDeps interface {
	Getwd() (string, error)
	MkdirAll(path string, perm os.FileMode) error
	Symlink(oldname, newname string) error
	LoadConfig() (*config.Config, error)
}

// StdInstallDeps is the production InstallDeps backed by the os package and
// config.Load.
type StdInstallDeps struct{}

func (StdInstallDeps) Getwd() (string, error)                       { return os.Getwd() }
func (StdInstallDeps) MkdirAll(path string, perm os.FileMode) error { return os.MkdirAll(path, perm) }
func (StdInstallDeps) Symlink(oldname, newname string) error        { return os.Symlink(oldname, newname) }
func (StdInstallDeps) LoadConfig() (*config.Config, error)          { return config.Load() }

const installLockSection = "install"

// installOptions carries the install-specific invocation state this file
// threads through the install pipeline: the EXACT/PRUNE opt-out (previously the
// install-local `installInexact` mutable package var) and the build-stamp
// values (Version/Commit/Describe) finalizeInstall records in the lock section.
//
// t17 removes the install-local mutable seam and the direct build-stamp global
// reads inside the pipeline by passing this struct explicitly. The broader
// lifecycle Flags / ErrorWithHintsFn package-var seams are intentionally left
// in place per the t01 SHAPE.md "PRESERVE current package-var seams" decision.
//
// NewInstallCmd's RunE builds one from live state; the exported RunInstall /
// RunInstallGenerate entrypoints fall back to installOptionsFromGlobals() so
// callers outside this file stay source-compatible.
type installOptions struct {
	// inexact opts out of the EXACT/PRUNE shared-target projection
	// (config-v2-coherence §7A.5 / D10), mirroring refreshInexact in
	// commands/refresh.go. Default false ⇒ install projects the resolved set AND
	// prunes managed outputs no longer in it, so the repo tree converges to
	// exactly what the lock declares. True (`--inexact`) keeps the additive
	// behavior: write the wanted set, leave stale managed outputs in place.
	inexact bool
	// version/commit/describe are the build-stamp values finalizeInstall writes
	// into the install lock section, sourced from the lifecycle Version/Commit/
	// Describe package vars (populated by applyDepsToGlobals) at invocation time.
	version  string
	commit   string
	describe string
}

// installOptionsFromGlobals snapshots the current lifecycle build-stamp package
// vars into an installOptions with the default (exact) projection. The exported
// RunInstall / RunInstallGenerate entrypoints use it so callers outside this
// file need not thread opts themselves.
func installOptionsFromGlobals() installOptions {
	return installOptions{
		version:  Version,
		commit:   Commit,
		describe: Describe,
	}
}

type installLockStamp struct {
	Project  string `json:"project"`
	Version  string `json:"version,omitempty"`
	Commit   string `json:"commit,omitempty"`
	Describe string `json:"describe,omitempty"`
	Stamped  string `json:"stamped_at"`
}

// NewInstallCmd builds the `da install` cobra command. The Deps argument
// carries UX helpers and the global-flags snapshot from the commands package;
// the RunE wrapper calls applyDepsToGlobals(deps) before each invocation so
// the helper functions in this file (which read lifecycle.Flags / .Version /
// .Commit / .Describe / .ErrorWithHintsFn package vars directly per the t01
// SHAPE.md "PRESERVE current package-var seams during the moves" decision)
// stay in sync with the parent process's live state.
//
// After t13a this absorbs what the parent commands/install.go shim's
// syncLifecycleGlobals helper used to do — a t13b call site of the form
// `lifecycle.NewInstallCmd(buildLifecycleDeps())` works end-to-end without a
// separate sync step. The existing parent shim's wrap remains compatible:
// the shim's syncLifecycleGlobals runs before the inner RunE, then this
// wrapper re-applies the same values from deps. Both writes are idempotent.
func NewInstallCmd(deps Deps) *cobra.Command {
	var generate bool
	var strict bool
	var inexact bool

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Set up project from .agentsrc.json manifest",
		Long: `Reads .agentsrc.json in the current directory, materializes declared skills and
agents into ~/.agents/ from configured sources, then applies the manifest to each
installed platform (rules, hooks, MCP configs, settings) with the same link pass
as da refresh.

By default the platform link pass is EXACT: it prunes managed shared-target links
that are no longer in the resolved set, so the tree converges to exactly what the
lock declares. Pass --inexact to keep the additive behavior and leave stale
managed links in place.

Commit .agentsrc.json to git so any contributor can run 'da install'
after cloning — no manual init or sync required.

Use --generate to create or refresh .agentsrc.json from the current ~/.agents/ state.
If a manifest already exists, generated skill and platform lists replace stale values,
but existing source entries (for example git remotes), a non-empty project name, and
unknown JSON keys are preserved.`,
		Example: deps.ExampleBlock(
			"  da install",
			"  da install --strict",
			"  da install --generate",
			"  da install --generate --force",
		),
		Args: deps.NoArgsWithHints("Run install from the target repository directory instead of passing a path."),
		RunE: func(cmd *cobra.Command, args []string) error {
			applyDepsToGlobals(deps)
			opts := installOptionsFromGlobals()
			opts.inexact = inexact
			if generate {
				return runInstallGenerate(StdInstallDeps{}, opts)
			}
			return runInstall(strict, StdInstallDeps{}, opts)
		},
	}
	cmd.Flags().BoolVar(&generate, "generate", false, "Create .agentsrc.json from current ~/.agents/ state")
	cmd.Flags().BoolVar(&strict, "strict", false, "Fail if any declared resource is not found")
	cmd.Flags().BoolVar(&inexact, "inexact", false, "Keep additive behavior: write the resolved set but do NOT prune managed outputs no longer in it (install otherwise converges the tree to exactly what the lock declares)")
	return cmd
}

// ─── RunInstall ──────────────────────────────────────────────────────────────

func RunInstall(strict bool, deps InstallDeps) error {
	return runInstall(strict, deps, installOptionsFromGlobals())
}

func runInstall(strict bool, deps InstallDeps, opts installOptions) error {
	projectPath, err := deps.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	ui.Header("da install")

	rc, err := loadInstallManifest(projectPath)
	if err != nil {
		return err
	}
	if err := ensureAgentsHomeInitialized(); err != nil {
		return err
	}

	projectName := installProjectName(rc.Project, projectPath)
	fmt.Fprintf(os.Stdout, "Project: %s\n", ui.BoldText(projectName))
	fmt.Fprintf(os.Stdout, "Path:    %s\n", ui.DimText(config.DisplayPath(projectPath)))

	ensureRes, err := ensureInstallResolved(projectPath)
	if err != nil {
		return err
	}
	resolvedSources, err := resolveInstallSources(rc.Sources, strict, deps)
	if err != nil {
		return err
	}
	if err := linkInstallResources(projectName, rc, resolvedSources, strict, deps); err != nil {
		return err
	}
	if err := ensureInstallProjectDirs(projectName); err != nil {
		return err
	}
	if err := RegisterInstallProject(projectName, projectPath, deps); err != nil {
		return err
	}

	packagesUnits, packagesParticipated, err := hydrateInstallPackages(projectPath, projectName, ensureRes)
	if err != nil {
		return err
	}

	if err := createInstallPlatformLinks(projectName, projectPath, opts, packagesUnits, packagesParticipated); err != nil {
		return err
	}
	if err := finalizeInstall(projectName, projectPath, opts); err != nil {
		return err
	}

	ui.SuccessBox(
		fmt.Sprintf("Project '%s' installed successfully!", projectName),
		"Check links: da status --audit",
		"Update manifest: da install --generate",
	)
	return nil
}

// ensureInstallResolved runs the §7A.5 lock half and returns the EnsureResult
// so the caller (hydrateInstallPackages) can tell whether pass-1 actually
// rewrote the lock this call (H9: pass-2 mirrors that same write/no-write
// decision rather than deciding independently). nil under dry-run — install
// performs no real resolution or packages hydration in that mode.
//
// UnitDigest is the H7 production artifact-store integrity resolver
// (PackagesArtifactDigestResolver): a `kind:artifact` unit whose CAS content
// no longer matches what a locally-cached, digest-pinned re-fetch verifies
// registers as unit-digest-mismatch staleness, so a post-install store tamper
// is caught here — the same seam `da config verify` uses — instead of being
// silently trusted.
func ensureInstallResolved(projectPath string) (*config.EnsureResult, error) {
	ui.Section("Resolving config")
	if Flags.DryRun {
		ui.DryRun("ensure config lock is current")
		return nil, nil
	}
	res, err := config.EnsureResolved(projectPath, config.EnsureOpts{
		UnitDigest: PackagesArtifactDigestResolver(projectPath),
	})
	if err != nil {
		return nil, fmt.Errorf("ensuring resolved config: %w", err)
	}
	ui.Bullet("ok", "Config lock current")
	return res, nil
}

// hydrateInstallPackages runs pass 2 (H9/H13) after pass-1 config resolution
// and before any platform projection reads the store. Skipped under dry-run
// (ensureRes is nil). Returns the resolved unit set plus `participated` — true
// whenever pass-2 ran (packages declared OR artifact units still locked from a
// prior install), which the caller uses to decide whether projection must go
// through the CAS-aware one-to-zero-prune path even for an empty set (review
// #4). Only false when the project has never used packages (D6 no-op).
func hydrateInstallPackages(projectPath, projectName string, ensureRes *config.EnsureResult) ([]platform.ResolvedUnit, bool, error) {
	if ensureRes == nil {
		if Flags.DryRun {
			ui.DryRun("materialize and lock resolved packages[] artifacts")
		}
		return nil, false, nil
	}
	units, participated, err := HydratePackagesUnits(projectPath, projectName, ensureRes)
	if err != nil {
		return nil, false, fmt.Errorf("resolving packages: %w", err)
	}
	if !participated {
		return nil, false, nil
	}
	ui.Section("Resolving packages")
	if len(units) == 0 {
		ui.Bullet("ok", "no packages artifacts declared — pruning any stale projected links")
	} else {
		ui.Bullet("ok", fmt.Sprintf("%d packages artifact(s) materialized", len(units)))
	}
	return units, true, nil
}

func loadInstallManifest(projectPath string) (*config.AgentsRC, error) {
	rc, err := config.LoadAgentsRC(projectPath)
	if err == nil {
		return rc, nil
	}
	if os.IsNotExist(err) {
		return nil, ErrorWithHintsFn(
			config.AgentsRCFile+" not found in current directory",
			"Run `da install --generate` to create one from the current shared state.",
			"If this project is not managed yet, run `da add .` first.",
		)
	}
	return nil, fmt.Errorf("reading %s: %w", config.AgentsRCFile, err)
}

func ensureAgentsHomeInitialized() error {
	if _, err := os.Stat(filepath.Join(config.AgentsHome(), "config.json")); err != nil {
		return ErrorWithHintsFn(
			"~/.agents/ not initialized",
			"Run `da init` once on this machine before using install.",
		)
	}
	return nil
}

func installProjectName(manifestProject, projectPath string) string {
	if manifestProject != "" {
		return manifestProject
	}
	return filepath.Base(projectPath)
}

func resolveInstallSources(sources []config.Source, strict bool, deps InstallDeps) ([]string, error) {
	ui.Section("Resolving sources")
	resolvedSources, err := resolveSources(sources, deps)
	if err != nil && strict {
		return nil, err
	}
	return resolvedSources, nil
}

func linkInstallResources(projectName string, rc *config.AgentsRC, resolvedSources []string, strict bool, deps InstallDeps) error {
	sources := resolvedSources
	if len(sources) == 0 {
		// Manifest may omit explicit sources while listing skills/agents that already exist
		// under ~/.agents/<bucket>/<project>/ (e.g. after promote). Resolve from canonical home.
		sources = []string{config.AgentsHome()}
	}
	if err := linkInstallResourceList("skills", "skill", rc.Skills, projectName, sources, strict, deps); err != nil {
		return err
	}
	return linkInstallResourceList("agents", "agent", rc.Agents, projectName, sources, strict, deps)
}

func linkInstallResourceList(resourceType, label string, names []string, projectName string, sources []string, strict bool, deps InstallDeps) error {
	for _, name := range names {
		if err := LinkResourceFromSources(resourceType, name, projectName, sources, deps); err != nil {
			msg := fmt.Sprintf("%s '%s' not found in any source", label, name)
			if strict {
				return fmt.Errorf("%s (--strict mode)", msg)
			}
			ui.Bullet("warn", msg+" — skipping")
		}
	}
	return nil
}

func ensureInstallProjectDirs(projectName string) error {
	if Flags.DryRun {
		ui.DryRun("create ~/.agents/ directories for '" + projectName + "'")
		return nil
	}
	if err := projectsync.CreateProjectDirs(projectName); err != nil {
		return err
	}
	ui.Bullet("ok", "Ensured ~/.agents/ project directories")
	return nil
}

// RegisterInstallProject upserts the project into ~/.agents/config.json,
// honoring --dry-run. Exported because the t03 shim in commands/install.go
// forwards to it and seams_test.go reaches it through the shim.
func RegisterInstallProject(projectName, projectPath string, deps InstallDeps) error {
	cfg, err := deps.LoadConfig()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if cfg.GetProjectPath(projectName) != "" {
		ui.Bullet("skip", "Already registered in config.json")
		return nil
	}
	if Flags.DryRun {
		ui.DryRun("register '" + projectName + "' in config.json")
		return nil
	}
	// A project already in the SYNCED identity registry but unbound on this
	// machine (machine-B rebind) is rebound via BindProject — machine-local path
	// only, preserving the synced repo_id. AddProject (which re-derives repo_id
	// from this machine's git remotes) is reserved for a genuinely new project;
	// using it on a known identity would overwrite/corrupt the synced identity.
	if cfg.IsProjectKnown(projectName) {
		cfg.BindProject(projectName, projectPath)
	} else {
		cfg.AddProject(projectName, projectPath)
	}
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}
	ui.Bullet("ok", "Registered '"+projectName+"' in config.json")
	return nil
}

func createInstallPlatformLinks(projectName, projectPath string, opts installOptions, units []platform.ResolvedUnit, packagesParticipated bool) error {
	return createInstallPlatformLinksFor(projectName, projectPath, platform.All(), opts, units, packagesParticipated)
}

func createInstallPlatformLinksFor(projectName, projectPath string, platforms []platform.Platform, opts installOptions, units []platform.ResolvedUnit, packagesParticipated bool) error {
	ui.Section("Creating platform links")
	config.SetWindowsMirrorContext(projectPath)

	if err := runInstallSharedTargetsFor(projectName, projectPath, platforms, opts, units, packagesParticipated); err != nil {
		return err
	}

	for _, p := range platforms {
		if err := createInstallPlatformLink(p, projectName, projectPath); err != nil {
			return err
		}
	}
	return nil
}

// runInstallSharedTargets runs the shared-target projection across all
// installed platforms and surfaces the resulting plan or warning lines.
func runInstallSharedTargets(projectName, projectPath string, opts installOptions) error {
	return runInstallSharedTargetsFor(projectName, projectPath, platform.All(), opts, nil, false)
}

// runInstallSharedTargetsFor projects the local-authored shared-target set
// plus, when pass-2 participated, the caller-resolved packages units (H13) —
// each linking DIRECTLY to its immutable CAS digest path — through ONE merged
// plan (platform.ProjectResolvedUnits), never a parallel linker (D4).
//
// packagesParticipated (not len(units)) selects the path: it stays true even
// when units is EMPTY (the last package was just removed), so the CAS-aware
// ProjectResolvedUnits still runs and its one-to-zero prune removes the final
// orphaned CAS link (review #4). Only a project that never used packages takes
// the plain RunSharedTargetProjectionExact path (R6 byte-parity).
func runInstallSharedTargetsFor(projectName, projectPath string, platforms []platform.Platform, opts installOptions, units []platform.ResolvedUnit, packagesParticipated bool) error {
	var installed []platform.Platform
	for _, p := range platforms {
		if p.IsInstalled() {
			installed = append(installed, p)
		}
	}
	var lines []string
	var err error
	if packagesParticipated {
		lines, err = platform.ProjectResolvedUnits(projectName, projectPath, units, installed, Flags.DryRun, !opts.inexact, projectName)
	} else {
		lines, err = platform.RunSharedTargetProjectionExact(projectName, projectPath, installed, Flags.DryRun, !opts.inexact)
	}
	if err != nil {
		return fmt.Errorf("shared targets: %w", err)
	}
	for _, line := range lines {
		ui.DryRun(line)
	}
	return nil
}

// createInstallPlatformLink refreshes (or skips) the link bundle for a
// single platform during install, honoring verbose / dry-run flags.
func createInstallPlatformLink(p platform.Platform, projectName, projectPath string) error {
	if !p.IsInstalled() {
		if Flags.Verbose {
			ui.Skip(p.DisplayName() + " (not installed)")
		}
		return nil
	}
	if Flags.DryRun {
		ui.DryRun("refresh " + p.DisplayName() + " links")
		return nil
	}
	if err := p.CreateLinks(projectName, projectPath); err != nil {
		return fmt.Errorf("%s links: %w", p.DisplayName(), err)
	}
	ui.Bullet("ok", p.DisplayName()+" links created")
	return nil
}

func finalizeInstall(projectName, projectPath string, opts installOptions) error {
	if Flags.DryRun {
		return nil
	}
	lf, err := agentslock.Open(config.AgentsLockPath(projectPath))
	if err != nil {
		return fmt.Errorf("open install lock: %w", err)
	}
	if err := lf.SetSection(installLockSection, installLockStamp{
		Project:  projectName,
		Version:  opts.version,
		Commit:   opts.commit,
		Describe: opts.describe,
		Stamped:  time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		return fmt.Errorf("stage install lock stamp: %w", err)
	}
	if err := lf.Flush(); err != nil {
		return fmt.Errorf("write install lock stamp: %w", err)
	}
	ui.Bullet("ok", "Recorded install stamp in .agentsrc.lock")
	return nil
}

// ─── RunInstallGenerate ──────────────────────────────────────────────────────

func RunInstallGenerate(deps InstallDeps) error {
	return runInstallGenerate(deps, installOptionsFromGlobals())
}

func runInstallGenerate(deps InstallDeps, opts installOptions) error {
	projectPath, err := deps.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	ui.Header("da install --generate")

	// Derive project name from config.json or directory name
	projectName := FindProjectByPath(projectPath, deps)
	if projectName == "" {
		projectName = filepath.Base(projectPath)
		ui.Info("Project not registered — using directory name: " + projectName)
	}

	rc, err := config.GenerateAgentsRC(projectName, projectPath)
	if err != nil {
		return fmt.Errorf("generating manifest: %w", err)
	}

	manifestPath := filepath.Join(projectPath, config.AgentsRCFile)
	if _, statErr := os.Stat(manifestPath); statErr == nil {
		existing, loadErr := config.LoadAgentsRC(projectPath)
		if loadErr != nil {
			return fmt.Errorf("loading existing %s: %w", config.AgentsRCFile, loadErr)
		}
		rc = config.MergeGenerateAgentsRC(existing, rc)
	} else if !os.IsNotExist(statErr) {
		return fmt.Errorf("accessing %s: %w", config.AgentsRCFile, statErr)
	}

	if Flags.DryRun {
		ui.DryRun(fmt.Sprintf("Would write %s with:", config.AgentsRCFile))
		ui.DryRun(fmt.Sprintf("  project:  %s", rc.Project))
		ui.DryRun(fmt.Sprintf("  sources:  %d entries", len(rc.Sources)))
		ui.DryRun(fmt.Sprintf("  skills:   %v", rc.Skills))
		ui.DryRun(fmt.Sprintf("  rules:    %v", rc.Rules))
		ui.DryRun(fmt.Sprintf("  agents:   %v", rc.Agents))
		ui.DryRun(fmt.Sprintf("  hooks:    %v", rc.Hooks))
		ui.DryRun(fmt.Sprintf("  mcp:      %v", rc.MCP))
		ui.DryRun(fmt.Sprintf("  settings: %v", rc.Settings))
		return nil
	}

	if err := rc.Save(projectPath); err != nil {
		return fmt.Errorf("writing %s: %w", config.AgentsRCFile, err)
	}

	ui.Success("Generated " + config.AgentsRCFile)
	fmt.Fprintf(os.Stdout, "  %sSkills: %d, Rules: %d, Agents: %d%s\n",
		ui.Dim, len(rc.Skills), len(rc.Rules), len(rc.Agents), ui.Reset)
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, "Next steps:")
	fmt.Fprintf(os.Stdout, "  1. Review:  cat %s\n", config.AgentsRCFile)
	fmt.Fprintf(os.Stdout, "  2. Commit:  git add %s && git commit -m 'Add da manifest'\n", config.AgentsRCFile)
	fmt.Fprintln(os.Stdout, "  3. Others:  da install   (after cloning)")
	return nil
}

// ─── source resolution ───────────────────────────────────────────────────────

// resolveSources resolves each source to a local root directory.
func resolveSources(sources []config.Source, deps InstallDeps) ([]string, error) {
	var resolved []string
	var firstErr error

	for _, src := range sources {
		root, err := resolveSourceRoot(src, deps)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if root == "" {
			continue
		}
		resolved = append(resolved, root)
	}
	return resolved, firstErr
}

func resolveSourceRoot(src config.Source, deps InstallDeps) (string, error) {
	switch src.Type {
	case "local":
		root := config.AgentsHome()
		if src.Path != "" {
			root = config.ExpandPath(src.Path)
		}
		ui.Bullet("ok", "Local source: "+config.DisplayPath(root))
		return root, nil
	case "git":
		if src.URL == "" {
			ui.Bullet("warn", "Git source missing 'url' — skipping")
			return "", nil
		}
		cacheDir, err := fetchGitSource(src.URL, src.Ref, deps)
		if err != nil {
			ui.Bullet("warn", fmt.Sprintf("Failed to fetch %s — skipping", src.URL))
			return "", err
		}
		ui.Bullet("ok", "Git source: "+src.URL)
		return cacheDir, nil
	default:
		ui.Bullet("warn", fmt.Sprintf("Unknown source type '%s' — skipping", src.Type))
		return "", nil
	}
}

// fetchGitSource clones or updates a git repository to the cache.
func fetchGitSource(url, ref string, deps InstallDeps) (string, error) {
	gitBin, err := exec.LookPath("git")
	if err != nil {
		return "", fmt.Errorf("git not installed")
	}

	cacheDir := config.GitSourceCacheDir(url)
	if hasCachedGitSource(cacheDir) {
		if ShouldUseCachedGitSource(cacheDir, url) {
			return cacheDir, nil
		}
		if Flags.DryRun {
			ui.DryRun("git -C " + cacheDir + " pull")
			return cacheDir, nil
		}
		updateCachedGitSource(gitBin, cacheDir, url)
		return cacheDir, nil
	}

	if Flags.DryRun {
		ui.DryRun(gitCloneDryRunCommand(url, ref, cacheDir))
		return cacheDir, nil
	}
	return CloneGitSource(gitBin, url, ref, cacheDir, deps)
}

func hasCachedGitSource(cacheDir string) bool {
	_, err := os.Stat(filepath.Join(cacheDir, ".git"))
	return err == nil
}

// ShouldUseCachedGitSource is exported so the t03 shim in commands/install.go
// can forward to it. seams_test.go's verbose-info branch test reaches it
// through the lower-case wrapper in the shim.
func ShouldUseCachedGitSource(cacheDir, url string) bool {
	if Flags.Force {
		return false
	}
	lastFetch := filepath.Join(cacheDir, ".last-fetch")
	info, err := os.Stat(lastFetch)
	if err != nil || time.Since(info.ModTime()) >= time.Hour {
		return false
	}
	if Flags.Verbose {
		ui.Info("Using cached source (< 1h old): " + url)
	}
	return true
}

func updateCachedGitSource(gitBin, cacheDir, url string) {
	if Flags.Verbose {
		ui.Info("Updating cached source: " + url)
	}
	// "--" separator prevents an attacker-controlled remote/branch from being
	// parsed as a git flag (CVE-2017-1000117 class). git pull treats subsequent
	// positional args as <repository> <refspec>...
	cmd := exec.Command(gitBin, "-C", cacheDir, "pull", "-q", "--")
	if err := cmd.Run(); err != nil {
		ui.Bullet("warn", "Could not update cached source — using existing copy")
		return
	}
	touchLastFetch(cacheDir)
}

func gitCloneDryRunCommand(url, ref, cacheDir string) string {
	args := "git clone --depth 1"
	if ref != "" {
		args += " --branch " + ref
	}
	// "--" mirrors the real argv built by cloneGitSource so the dry-run
	// preview matches what would actually execute.
	return args + " -- " + url + " " + cacheDir
}

// CloneGitSource is exported so the t03 shim in commands/install.go can
// forward to it. seams_test.go's TestCloneGitSource_MkdirAllError reaches
// it through the lower-case wrapper in the shim.
func CloneGitSource(gitBin, url, ref, cacheDir string, deps InstallDeps) (string, error) {
	if Flags.Verbose {
		ui.Info("Cloning source: " + url)
	}
	if err := deps.MkdirAll(cacheDir, 0755); err != nil {
		return "", err
	}
	args := []string{"clone", "--depth", "1"}
	if ref != "" {
		args = append(args, "--branch", ref)
	}
	// "--" separator forces git to treat url/cacheDir as positionals, even if
	// url starts with "-" or "--upload-pack=…" (CVE-2017-1000117 class).
	args = append(args, "--", url, cacheDir)
	cmd := exec.Command(gitBin, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(cacheDir)
		return "", fmt.Errorf("git clone failed: %s", string(out))
	}
	touchLastFetch(cacheDir)
	return cacheDir, nil
}

func touchLastFetch(cacheDir string) {
	f := filepath.Join(cacheDir, ".last-fetch")
	_ = os.WriteFile(f, []byte(time.Now().Format(time.RFC3339)), 0644)
}

// LinkResourceFromSources symlinks a resource from the first matching source
// into ~/.agents/{resourceType}/{project}/{name}/. Exported because the t03
// shim in commands/install.go forwards to it and seams_test.go reaches it
// through the shim.
func LinkResourceFromSources(resourceType, name, project string, sources []string, deps InstallDeps) error {
	destDir := filepath.Join(config.AgentsHome(), resourceType, project, name)
	markerFile := resourceMarkerFile(resourceType)
	candidate, srcRoot, found := firstResourceCandidate(resourceType, name, markerFile, project, sources)
	if !found {
		return fmt.Errorf("not found in any source")
	}

	if Flags.DryRun {
		ui.DryRun(fmt.Sprintf("link %s/%s → %s", resourceType, name, config.DisplayPath(candidate)))
		return nil
	}
	if shouldSkipLinkDestination(destDir) {
		return nil
	}
	if err := deps.MkdirAll(filepath.Dir(destDir), 0755); err != nil {
		return err
	}
	if err := deps.Symlink(candidate, destDir); err != nil {
		return fmt.Errorf("symlinking %s: %w", name, err)
	}
	if Flags.Verbose {
		ui.Bullet("ok", fmt.Sprintf("Linked %s/%s from %s", resourceType, name, config.DisplayPath(srcRoot)))
	}
	return nil
}

func resourceMarkerFile(resourceType string) string {
	switch resourceType {
	case "skills":
		return "SKILL.md"
	case "agents":
		return "AGENT.md"
	default:
		return ""
	}
}

func firstResourceCandidate(resourceType, name, markerFile, project string, sources []string) (string, string, bool) {
	for _, srcRoot := range sources {
		// Prefer project-scoped canonical dirs (~/.agents/skills/<project>/…), then global/.
		candidates := []string{
			filepath.Join(srcRoot, resourceType, project, name),
			filepath.Join(srcRoot, resourceType, "global", name),
		}
		for _, candidate := range candidates {
			if resourceCandidateIsValid(candidate, markerFile) {
				return candidate, srcRoot, true
			}
		}
	}
	return "", "", false
}

func resourceCandidateIsValid(candidate, markerFile string) bool {
	info, err := os.Stat(candidate)
	if err != nil || !info.IsDir() {
		return false
	}
	if markerFile == "" {
		return true
	}
	_, err = os.Stat(filepath.Join(candidate, markerFile))
	return err == nil
}

func shouldSkipLinkDestination(destDir string) bool {
	if _, err := os.Lstat(destDir); err != nil {
		return false
	}
	if !Flags.Force {
		return true
	}
	_ = os.RemoveAll(destDir)
	return false
}

// FindProjectByPath looks up the registered project name for a given path.
// Exported because the t03 shim in commands/install.go forwards to it and
// seams_test.go reaches it through the shim.
func FindProjectByPath(projectPath string, deps InstallDeps) string {
	cfg, err := deps.LoadConfig()
	if err != nil {
		return ""
	}
	for _, name := range cfg.ListProjects() {
		if cfg.GetProjectPath(name) == projectPath {
			return name
		}
	}
	return ""
}
