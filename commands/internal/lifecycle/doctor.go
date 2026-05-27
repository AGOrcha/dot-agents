package lifecycle

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/links"
	"github.com/NikashPrakash/dot-agents/internal/platform"
	"github.com/NikashPrakash/dot-agents/internal/ui"
	"github.com/spf13/cobra"
)

const (
	doctorOpenCodeDir = ".opencode"
	doctorClaudeDir   = ".claude"
)

// Owned repo-relative file/name constants shared across doctor's link
// collectors. Centralized so the broken-link and OK-count paths cannot drift.
const (
	doctorAgentsMD     = "AGENTS.md"
	doctorCopilotInstr = "copilot-instructions.md"
	doctorMCPJSON      = "mcp.json"
	doctorOpenCodeJSON = "opencode.json"
	doctorGlobalPrefix = "global--"
)

// DoctorConfigLoader is the narrow collaborator doctor.go's
// fault-injectable LoadConfig operation needs (interface-DI per
// docs/TEST_SEAMS.md). Single-method, file-prefixed -er form; file-scoped
// — do not share with other commands files.
//
// Exported during the t09→t11 window so commands/seams_test.go (still in
// root until t11 splits it per cluster) can construct test doubles via the
// root shim's RunDoctor entry point. After t11 the root shim drops the
// type alias and this can lowercase back to doctorConfigLoader.
type DoctorConfigLoader interface {
	LoadConfig() (*config.Config, error)
}

// StdDoctorConfigLoader is the production DoctorConfigLoader backed by
// internal/config.Load. Exported alongside DoctorConfigLoader for the
// t09→t11 window so the root shim's NewDoctorCmd RunE can construct one
// while keeping lifecycle import-cycle free.
type StdDoctorConfigLoader struct{}

// LoadConfig delegates to internal/config.Load.
func (StdDoctorConfigLoader) LoadConfig() (*config.Config, error) { return config.Load() }

// NewDoctorCmd builds the `da doctor` cobra command. Mirrors the
// NewStatusCmd / NewInstallCmd Deps-injection pattern: lifecycle does not
// import the parent commands/ package (cycle), so Args/Example helpers and
// the UsageError formatter come in via Deps.
//
// The RunE wrapper calls applyDepsToGlobals(deps) before delegating so the
// moved doctor body (which reads lifecycle.Flags / .ErrorWithHintsFn package
// vars directly) sees live state from the caller. After t13a this absorbs
// what the parent commands/doctor.go shim's syncLifecycleGlobals wrap used
// to do — a t13b call site of the form
// `lifecycle.NewDoctorCmd(buildLifecycleDeps())` works end-to-end without a
// separate sync step. The existing parent shim's wrap remains compatible
// because both syncs write the same values (idempotent).
func NewDoctorCmd(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check installations, validate links, detect issues",
		Long: `Audits the local da installation, installed platforms, manifest health,
and managed project links using the same managed paths as da install and
refresh. Doctor is the fastest way to detect drift after manual edits, moved
repositories, or partial setup on a new machine.`,
		Example: deps.ExampleBlock(
			"  da doctor",
			"  da doctor --verbose",
			"  da doctor --dry-run",
		),
		Args: deps.NoArgsWithHints("`da doctor` audits the current installation and does not take a project argument."),
		RunE: func(cmd *cobra.Command, args []string) error {
			applyDepsToGlobals(deps)
			return runDoctor(cmd, args, StdDoctorConfigLoader{})
		},
	}
}

// RunDoctor is the exported entry point used by commands/doctor.go's t09→t11
// root shim so seams_test.go can drive the doctor pipeline with a fault-
// injected DoctorConfigLoader. After t11 the shim is deleted and the
// lowercase runDoctor satisfies the only remaining (intra-package) callers.
func RunDoctor(cmd *cobra.Command, args []string, deps DoctorConfigLoader) error {
	return runDoctor(cmd, args, deps)
}

func runDoctor(cmd *cobra.Command, args []string, deps DoctorConfigLoader) error {
	ui.Header("da doctor")

	agentsHome := config.AgentsHome()

	reportInstallationStatus(agentsHome)
	reportPlatformInventory()
	reportUserConfigHealth(agentsHome)

	cfg, err := deps.LoadConfig()
	if err != nil {
		ui.Bullet("warn", "Could not load config: "+err.Error())
		return nil
	}

	names := cfg.ListProjects()
	if len(names) == 0 {
		ui.Section("Projects")
		ui.Info("No managed projects")
		fmt.Fprintln(os.Stdout)
		return nil
	}

	reportProjectInventory(cfg, names)
	totalFixed, anyBroken := reportLinkHealth(cfg, names, agentsHome)
	reportManifestHealth(cfg, names)
	reportOrphanCanonicals(cfg, names, agentsHome)
	reportPluginHealth(cfg, names, agentsHome)

	finalizeDoctorRun(anyBroken, totalFixed)
	return nil
}

// reportInstallationStatus prints the "Installation" section: presence of
// ~/.agents/ and config.json. Pure side-effect on the ui sink — no caller
// state to thread.
func reportInstallationStatus(agentsHome string) {
	ui.Section("Installation")
	if _, err := os.Stat(agentsHome); err == nil {
		ui.Bullet("ok", "~/.agents/ exists")
	} else {
		ui.Bullet("error", "~/.agents/ not found — run: da init")
	}

	cfgPath := filepath.Join(agentsHome, "config.json")
	if _, err := os.Stat(cfgPath); err == nil {
		ui.Bullet("ok", "config.json exists")
	} else {
		ui.Bullet("warn", "config.json not found")
	}
}

// reportPlatformInventory prints the "Platforms" section: one bullet per
// known platform with installed/version status.
func reportPlatformInventory() {
	ui.Section("Platforms")
	for _, p := range platform.All() {
		if !p.IsInstalled() {
			ui.Bullet("none", p.DisplayName()+" (not installed)")
			continue
		}
		ver := p.Version()
		if ver != "" {
			ui.Bullet("ok", fmt.Sprintf("%s (%s)", p.DisplayName(), ver))
		} else {
			ui.Bullet("ok", p.DisplayName()+" (installed)")
		}
	}
}

// reportUserConfigHealth prints the "User Config" section: count of broken
// user-level managed links plus per-link detail (verbose) or just the broken
// ones (non-verbose).
func reportUserConfigHealth(agentsHome string) {
	ui.Section("User Config")
	userBroken := collectBrokenUserLinks(agentsHome)
	if len(userBroken) == 0 {
		ui.Bullet("ok", "User-level config healthy")
	} else {
		ui.Bullet("warn", fmt.Sprintf("User-level config has %d broken link(s)", len(userBroken)))
	}

	if Flags.Verbose {
		printUserConfigStatus(agentsHome)
		return
	}
	for _, bl := range userBroken {
		fmt.Fprintf(os.Stdout, "      %s✗%s %s %s→ %s%s\n", ui.Red, ui.Reset, bl.linkPath, ui.Dim, bl.dest, ui.Reset)
	}
}

// reportProjectInventory prints the "Projects (N)" header + one bullet per
// managed project. Missing project directories are flagged but do not stop
// the run (downstream sections skip them).
func reportProjectInventory(cfg *config.Config, names []string) {
	ui.Section(fmt.Sprintf("Projects (%d)", len(names)))
	for _, name := range names {
		path := cfg.GetProjectPath(name)
		if _, err := os.Stat(path); err != nil {
			ui.Bullet("error", fmt.Sprintf("%s — directory missing: %s", name, path))
			continue
		}
		ui.Bullet("ok", fmt.Sprintf("%s (%s)", name, config.DisplayPath(path)))
	}
}

// reportLinkHealth prints the "Link Health" section: per-project broken/OK
// counts, broken-link detail (or full audit in verbose mode), and triggers
// repair when needed. Returns the cumulative platform-repair count and
// whether any broken links were observed (drives finalizeDoctorRun).
func reportLinkHealth(cfg *config.Config, names []string, agentsHome string) (int, bool) {
	ui.Section("Link Health")
	totalFixed := 0
	anyBroken := false
	for _, name := range names {
		path := cfg.GetProjectPath(name)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		broken := reportOneProjectLinkHealth(name, path, agentsHome, cfg)
		if broken {
			anyBroken = true
			totalFixed += repairManagedProject(name, path)
		}
	}
	return totalFixed, anyBroken
}

// reportOneProjectLinkHealth handles the link-health audit for a single
// project. Returns true when broken links were observed (caller triggers
// repair); false for empty or fully-healthy projects.
func reportOneProjectLinkHealth(name, path, agentsHome string, cfg *config.Config) bool {
	brokenLinks := collectBrokenLinks(name, path, agentsHome)
	ok, _ := countProjectLinks(name, path, agentsHome)
	total := ok + len(brokenLinks)

	if total == 0 {
		ui.Bullet("none", fmt.Sprintf("%s — no managed links detected", name))
		if Flags.Verbose {
			printAudit(name, path, agentsHome, "", cfg)
		}
		return false
	}
	if len(brokenLinks) == 0 {
		ui.Bullet("ok", fmt.Sprintf("%s — %d links healthy", name, ok))
		if Flags.Verbose {
			printAudit(name, path, agentsHome, "", cfg)
		}
		return false
	}

	ui.Bullet("warn", fmt.Sprintf("%s — %d/%d links OK, %d broken", name, ok, total, len(brokenLinks)))
	if Flags.Verbose {
		printAudit(name, path, agentsHome, "", cfg)
	} else {
		for _, bl := range brokenLinks {
			fmt.Fprintf(os.Stdout, "      %s✗%s %s %s→ %s%s\n", ui.Red, ui.Reset, bl.linkPath, ui.Dim, bl.dest, ui.Reset)
		}
	}
	return true
}

// reportManifestHealth prints the "Manifests" section: per-project manifest
// presence, corruption status, and per-git-source fetch state.
func reportManifestHealth(cfg *config.Config, names []string) {
	ui.Section("Manifests (.agentsrc.json)")
	anyManifestIssue := false
	for _, name := range names {
		path := cfg.GetProjectPath(name)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		if reportOneProjectManifestHealth(name, path) {
			anyManifestIssue = true
		}
	}
	if !anyManifestIssue {
		fmt.Fprintf(os.Stdout, "  %sTip: run with -v to see per-project manifest details%s\n", ui.Dim, ui.Reset)
	}
}

// reportOneProjectManifestHealth handles the manifest audit for a single
// project. Returns true when an issue was reported (missing manifest, corrupt
// manifest, or any unfetched git source); false for a fully healthy manifest.
func reportOneProjectManifestHealth(name, path string) bool {
	rc, err := config.LoadAgentsRC(path)
	if err != nil {
		if os.IsNotExist(err) {
			ui.Bullet("warn", fmt.Sprintf("%s — no manifest (not git-portable)  hint: da install --generate", name))
		} else {
			ui.Bullet("error", fmt.Sprintf("%s — corrupt manifest: %v", name, err))
		}
		return true
	}
	missingGit, presentGit := partitionManifestGitSources(rc)
	if len(missingGit) > 0 {
		for _, url := range missingGit {
			ui.Bullet("warn", fmt.Sprintf("%s — git source not yet fetched: %s  hint: da install", name, url))
		}
		return true
	}
	if len(presentGit) > 0 {
		ui.Bullet("ok", fmt.Sprintf("%s — manifest ok (%d git source(s))", name, len(presentGit)))
	} else {
		ui.Bullet("ok", fmt.Sprintf("%s — manifest ok (local)", name))
	}
	return false
}

// partitionManifestGitSources splits the manifest's git sources into two
// lists by whether their on-disk cache directory exists. Non-git sources
// and entries with an empty URL are skipped.
func partitionManifestGitSources(rc *config.AgentsRC) (missing, present []string) {
	for _, src := range rc.Sources {
		if src.Type != "git" || src.URL == "" {
			continue
		}
		cacheDir := config.GitSourceCacheDir(src.URL)
		if _, err := os.Stat(cacheDir); err != nil {
			missing = append(missing, src.URL)
		} else {
			present = append(present, src.URL)
		}
	}
	return missing, present
}

// reportOrphanCanonicals prints the "Canonical Resources" section: warns
// about ~/.agents/{skills,agents}/<project>/<name>/ entries with no live
// back-link in the project.
func reportOrphanCanonicals(cfg *config.Config, names []string, agentsHome string) {
	ui.Section("Canonical Resources")
	anyOrphan := false
	for _, bucket := range []string{"skills", "agents"} {
		for _, name := range names {
			path := cfg.GetProjectPath(name)
			if _, err := os.Stat(path); err != nil {
				continue
			}
			for _, orphan := range collectOrphanCanonicals(name, path, agentsHome, bucket) {
				anyOrphan = true
				warnOrphanCanonical(name, path, agentsHome, bucket, orphan)
			}
		}
	}
	if !anyOrphan {
		ui.Bullet("ok", "No orphan canonical resources")
	}
}

// warnOrphanCanonical emits the warn bullet for a single orphan canonical
// entry, recovering the bare entry name from any mis-pointed annotation.
// `promote --force` cannot recover an orphan (no back-link to copy from),
// so the surfaced hint covers the two real recovery options.
func warnOrphanCanonical(name, path, agentsHome, bucket, orphan string) {
	orphanName := orphan
	if idx := strings.Index(orphan, "  ("); idx >= 0 {
		orphanName = orphan[:idx]
	}
	canonicalPath := filepath.Join(agentsHome, bucket, name, orphanName)
	backLink := filepath.Join(path, ".agents", bucket, orphanName)
	ui.Bullet("warn", fmt.Sprintf("%s — orphan canonical %s %q at %s; hint: restore the back-link with `ln -s %s %s` or purge the orphan with `rm -rf %s`.",
		name, bucket, orphan, canonicalPath,
		canonicalPath, backLink,
		canonicalPath))
}

// reportPluginHealth prints the "Plugins" section: lists canonical plugin
// bundles, warns about emitters not yet implemented, and flags broken
// opencode plugin symlinks in each managed project.
func reportPluginHealth(cfg *config.Config, names []string, agentsHome string) {
	ui.Section("Plugins")
	pluginSpecs, pluginErr := platform.ListPluginSpecs(agentsHome, "")
	if pluginErr != nil {
		ui.Bullet("error", fmt.Sprintf("plugin bundles unavailable: %v", pluginErr))
		return
	}
	if len(pluginSpecs) == 0 {
		ui.Info("No canonical plugin bundles")
		return
	}
	for _, spec := range pluginSpecs {
		reportOnePluginSpec(spec, cfg, names)
	}
}

// reportOnePluginSpec prints the per-spec plugin-health detail: emitter-not-
// implemented warnings and, for opencode, broken plugin symlinks per project.
func reportOnePluginSpec(spec platform.PluginSpec, cfg *config.Config, names []string) {
	bundleLabel := filepath.Join(spec.Scope, spec.Name)
	for _, platformID := range spec.Platforms {
		if platformID != "opencode" {
			ui.Bullet("warn", fmt.Sprintf("%s: platforms includes %s but no emitter is implemented yet", bundleLabel, platformID))
		}
	}
	if !hasPluginPlatform(spec.Platforms, "opencode") {
		return
	}
	for _, name := range names {
		projectPath := cfg.GetProjectPath(name)
		if projectPath == "" {
			continue
		}
		linkPath := filepath.Join(projectPath, doctorOpenCodeDir, "plugins", spec.Name)
		raw, ok := links.ManagedLinkTarget(linkPath)
		if !ok {
			continue
		}
		if _, err := os.Stat(resolveLinkDest(linkPath, raw)); err != nil {
			ui.Bullet("error", fmt.Sprintf("%s: broken symlink at %s", bundleLabel, linkPath))
		}
	}
}

// finalizeDoctorRun prints the closing tip/summary line based on the
// link-health audit. Healthy run: optional verbose tip. Broken run:
// dry-run hint or repair summary.
func finalizeDoctorRun(anyBroken bool, totalFixed int) {
	fmt.Fprintln(os.Stdout)
	if !anyBroken {
		if !Flags.Verbose {
			fmt.Fprintf(os.Stdout, "  %sTip: run with -v to see full link details per project%s\n\n", ui.Dim, ui.Reset)
		}
		return
	}
	if Flags.DryRun {
		ui.Info("Run without --dry-run to apply repairs.")
	} else if totalFixed > 0 {
		ui.Success(fmt.Sprintf("Repaired links in %d platform(s). Run 'da status --audit' to verify.", totalFixed))
		fmt.Fprintln(os.Stdout)
	}
}

// doctorInstalledPlatforms returns every installed platform, matching the
// installed-only scoping used by add/install/import when they drive a full
// CreateLinks pass. Exposed as a package var so doctor_test can substitute a
// deterministic platform set for the new repair branch.
var doctorInstalledPlatforms = func() []platform.Platform {
	var installed []platform.Platform
	for _, p := range platform.All() {
		if p.IsInstalled() {
			installed = append(installed, p)
		}
	}
	return installed
}

// repairManagedProject runs the full repair pass for one managed project whose
// link health audit found at least one broken link. It is NOT a symlink-only
// repair: for the project it (a) runs the shared-target projection to fix
// broken/missing projected artifacts (repo .codex/agents/*.toml, Claude
// shared-skills projection) and (b) re-runs CreateLinks for every installed
// platform — not merely the platforms whose links were already detected
// broken — so that every managed da entity is (re)linked. This mirrors the
// established call shape on master (refresh.go, add.go, install.go,
// import.go relink) with warn-and-continue error handling.
//
// Idempotence: this only runs for projects the audit already flagged as
// broken, so a healthy managed project produces no spurious changes and no
// noisy output (doctor's diagnostic UX is preserved). Within a repaired
// project, RunSharedTargetProjection.Execute and Platform.CreateLinks are
// themselves idempotent — they re-establish managed state and are no-ops when
// that state is already correct — so re-running doctor on an
// already-repaired project is also a no-op. It returns the number of platforms
// successfully (re)linked, for the run-summary counter.
func repairManagedProject(name, path string) int {
	installed := doctorInstalledPlatforms()

	if Flags.DryRun {
		ui.DryRun("re-run shared-target projection to repair projected artifacts")
		for _, p := range installed {
			ui.DryRun(fmt.Sprintf("re-run %s CreateLinks to repair", p.DisplayName()))
		}
		return 0
	}

	config.SetWindowsMirrorContext(path)

	// (a) Shared-target projection: fixes broken/missing projected
	// shared-target artifacts (repo .codex/agents/*.toml, Claude
	// shared-skills projection). Warn-and-continue so a projection failure
	// does not block the link repair below.
	if _, err := platform.RunSharedTargetProjection(name, path, installed, false); err != nil {
		ui.Bullet("warn", fmt.Sprintf("repair shared targets: %v", err))
	}

	// (b) Full installed-platform CreateLinks pass: relinks ALL managed da
	// entities, not only the links detected as broken.
	fixed := 0
	for _, p := range installed {
		if err := p.CreateLinks(name, path); err != nil {
			ui.Bullet("warn", fmt.Sprintf("repair %s: %v", p.DisplayName(), err))
			continue
		}
		ui.Bullet("ok", fmt.Sprintf("repaired %s links", p.DisplayName()))
		fixed++
	}
	return fixed
}

// collectOrphanCanonicals returns the resource names under
// ~/.agents/<bucket>/<projectName>/ that have no back-link
// (symlink or real dir) at <projectPath>/.agents/<bucket>/<name>.
// These are leftovers when a user manually deleted the repo-local source
// after a promote, leaving the canonical copy orphaned.
func collectOrphanCanonicals(projectName, projectPath, agentsHome, bucket string) []string {
	canonicalDir := filepath.Join(agentsHome, bucket, projectName)
	entries, err := os.ReadDir(canonicalDir)
	if err != nil {
		return nil
	}
	var orphans []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if entry, ok := classifyCanonicalOrphan(projectPath, canonicalDir, bucket, e.Name()); ok {
			orphans = append(orphans, entry)
		}
	}
	return orphans
}

// classifyCanonicalOrphan decides whether a single canonical entry is an
// orphan. It returns the display string to record and true when it is. A
// missing back-link is a plain orphan; a back-link that is a resolvable
// managed link pointing elsewhere is a mis-pointed orphan; any other present
// back-link is a live reference (not an orphan).
func classifyCanonicalOrphan(projectPath, canonicalDir, bucket, name string) (string, bool) {
	backLink := filepath.Join(projectPath, ".agents", bucket, name)
	if _, err := os.Lstat(backLink); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return name, true
		}
		return "", false
	}
	// If the back-link is a resolvable managed link (POSIX symlink /
	// Windows junction), verify it points at THIS canonical. A link that
	// resolves to a different canonical (or anywhere else) is still an
	// orphan — the canonical here has no live reference. A non-resolvable
	// entry (real dir, or a hard-linked file with no reparse point) is a
	// live back-reference and not an orphan.
	raw, ok := links.ManagedLinkTarget(backLink)
	if !ok {
		return "", false
	}
	target := raw
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(backLink), target)
	}
	expected := filepath.Join(canonicalDir, name)
	if filepath.Clean(target) != filepath.Clean(expected) {
		return name + "  (mis-pointed: " + target + ")", true
	}
	return "", false
}

func hasPluginPlatform(platforms []string, want string) bool {
	for _, platformID := range platforms {
		if platformID == want {
			return true
		}
	}
	return false
}

// brokenLink holds info about a single broken managed link.
type brokenLink struct {
	platformID string
	linkPath   string // relative display path
	dest       string // symlink/hardlink target
}

// resolveLinkDest and managedLinkBroken live in status.go (intra-package;
// were duplicated in commands/doctor.go before t09 collapsed the
// duplicates per SHAPE.md OD-2). managedLinkHealthy is doctor-specific
// (status.go has no caller) so it stays here.

// managedLinkHealthy reports whether linkPath is a resolvable managed link
// whose target exists. Used by OK-count paths for symlink/junction links.
func managedLinkHealthy(linkPath string) bool {
	raw, ok := links.ManagedLinkTarget(linkPath)
	if !ok {
		return false
	}
	_, err := os.Stat(resolveLinkDest(linkPath, raw))
	return err == nil
}

// claudeRuleHardlinked reports whether a .claude/rules entry is a Windows
// managed *file* hard link to its canonical rule source. The entry name is
// "<scope>--<rest>" where scope is "global" or the project name; the source
// lives at <agentsHome>/rules/<scope>/<rest> with a .mdc→.md fallback. On
// POSIX these are symlinks (handled by managedLinkHealthy) so this is a
// no-op there.
func claudeRuleHardlinked(linkPath, entryName, projectName, agentsHome string) bool {
	scope, rest := "", ""
	switch {
	case strings.HasPrefix(entryName, doctorGlobalPrefix):
		scope, rest = "global", strings.TrimPrefix(entryName, doctorGlobalPrefix)
	case strings.HasPrefix(entryName, projectName+"--"):
		scope, rest = projectName, strings.TrimPrefix(entryName, projectName+"--")
	default:
		return false
	}
	src := filepath.Join(agentsHome, "rules", scope, rest)
	if linked, _ := links.AreHardlinked(linkPath, src); linked {
		return true
	}
	src2 := filepath.Join(agentsHome, "rules", scope, strings.TrimSuffix(rest, ".mdc")+".md")
	linked, _ := links.AreHardlinked(linkPath, src2)
	return linked
}

// cursorRuleScope returns the (scope, rest) pair for a cursor rule entry, or
// ("", "", false) if the entry is not a managed cursor rule for this project.
func cursorRuleScope(entryName, projectName string) (scope, rest string, ok bool) {
	switch {
	case strings.Contains(entryName, ".dot-agents-backup"):
		return "", "", false
	case !strings.HasSuffix(entryName, ".mdc"):
		return "", "", false
	case strings.HasPrefix(entryName, doctorGlobalPrefix):
		return "global", strings.TrimPrefix(entryName, doctorGlobalPrefix), true
	case strings.HasPrefix(entryName, projectName+"--"):
		return projectName, strings.TrimPrefix(entryName, projectName+"--"), true
	}
	return "", "", false
}

// cursorRuleHardlinkedAny reports whether linkPath is a hardlink to the
// canonical cursor source (with .mdc→.md fallback) and returns the primary
// source path used (regardless of which one matched).
func cursorRuleHardlinkedAny(linkPath, scope, rest, agentsHome string) (src string, linked bool) {
	src = filepath.Join(agentsHome, "rules", scope, rest)
	if ok, _ := links.AreHardlinked(linkPath, src); ok {
		return src, true
	}
	srcMD := strings.TrimSuffix(rest, ".mdc") + ".md"
	src2 := filepath.Join(agentsHome, "rules", scope, srcMD)
	if ok, _ := links.AreHardlinked(linkPath, src2); ok {
		return src, true
	}
	return src, false
}

// collectCursorBrokenLinks walks the cursor rules dir and appends one
// brokenLink per managed entry that fails the hardlink check.
func collectCursorBrokenLinks(name, path, agentsHome string, rel func(string) string) []brokenLink {
	var broken []brokenLink
	cursorRulesDir := filepath.Join(path, ".cursor", "rules")
	entries, err := os.ReadDir(cursorRulesDir)
	if err != nil {
		return broken
	}
	for _, e := range entries {
		scope, rest, ok := cursorRuleScope(e.Name(), name)
		if !ok {
			continue
		}
		f := filepath.Join(cursorRulesDir, e.Name())
		src, linked := cursorRuleHardlinkedAny(f, scope, rest, agentsHome)
		if linked {
			continue
		}
		broken = append(broken, brokenLink{
			platformID: "cursor",
			linkPath:   rel(f),
			dest:       config.DisplayPath(src),
		})
	}
	return broken
}

// collectClaudeBrokenLinks walks the claude rules dir and appends one
// brokenLink per resolvable-but-broken managed link.
func collectClaudeBrokenLinks(path string, rel func(string) string) []brokenLink {
	var broken []brokenLink
	claudeRulesDir := filepath.Join(path, doctorClaudeDir, "rules")
	entries, err := os.ReadDir(claudeRulesDir)
	if err != nil {
		return broken
	}
	for _, e := range entries {
		linkPath := filepath.Join(claudeRulesDir, e.Name())
		dest, isLink, isBroken := managedLinkBroken(linkPath)
		if !isLink || !isBroken {
			continue
		}
		broken = append(broken, brokenLink{
			platformID: "claude",
			linkPath:   rel(linkPath),
			dest:       config.DisplayPath(dest),
		})
	}
	return broken
}

// projectSingleFiles returns the canonical (platform, path) tuples for the
// single-file managed links checked by both collectBrokenLinks and
// countProjectLinks.
func projectSingleFiles(path string) []struct {
	platformID string
	path       string
} {
	return []struct {
		platformID string
		path       string
	}{
		{"codex", filepath.Join(path, doctorAgentsMD)},
		{"copilot", filepath.Join(path, ".github", doctorCopilotInstr)},
		{"copilot", filepath.Join(path, ".vscode", doctorMCPJSON)},
		{"claude", filepath.Join(path, ".mcp.json")},
		{"opencode", filepath.Join(path, doctorOpenCodeJSON)},
	}
}

// collectSingleFileBrokenLinks checks each canonical single-file managed link
// for the project and appends one brokenLink per resolvable-but-broken entry.
func collectSingleFileBrokenLinks(path string, rel func(string) string) []brokenLink {
	var broken []brokenLink
	for _, sf := range projectSingleFiles(path) {
		dest, isLink, isBroken := managedLinkBroken(sf.path)
		if !isLink || !isBroken {
			continue
		}
		broken = append(broken, brokenLink{
			platformID: sf.platformID,
			linkPath:   rel(sf.path),
			dest:       config.DisplayPath(dest),
		})
	}
	return broken
}

// collectBrokenLinks returns all broken managed links for a project.
func collectBrokenLinks(name, path, agentsHome string) []brokenLink {
	displayBase := path + "/"
	rel := func(p string) string {
		return strings.TrimPrefix(p, displayBase)
	}
	var broken []brokenLink
	broken = append(broken, collectCursorBrokenLinks(name, path, agentsHome, rel)...)
	broken = append(broken, collectClaudeBrokenLinks(path, rel)...)
	broken = append(broken, collectSingleFileBrokenLinks(path, rel)...)
	return broken
}

// collectBrokenUserLinks returns all broken managed user-level links in the home directory.
func collectBrokenUserLinks(_ string) []brokenLink {
	var broken []brokenLink

	homeDir, err := config.UserHomeDir()
	if err != nil {
		return broken
	}
	displayBase := homeDir + string(os.PathSeparator)
	rel := func(p string) string {
		return strings.TrimPrefix(p, displayBase)
	}

	addBrokenSingle := func(platformID, linkPath string) {
		if dest, isLink, isBroken := managedLinkBroken(linkPath); isLink && isBroken {
			broken = append(broken, brokenLink{
				platformID: platformID,
				linkPath:   rel(linkPath),
				dest:       config.DisplayPath(dest),
			})
		}
	}
	addBrokenDir := func(platformID, dir string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			addBrokenSingle(platformID, filepath.Join(dir, e.Name()))
		}
	}

	// Claude: ~/.claude/CLAUDE.md, settings.json, agents/*, skills/*
	claudeHome := filepath.Join(homeDir, doctorClaudeDir)
	addBrokenSingle("claude", filepath.Join(claudeHome, "CLAUDE.md"))
	addBrokenSingle("claude", filepath.Join(claudeHome, "settings.json"))
	addBrokenDir("claude", filepath.Join(claudeHome, "agents"))
	addBrokenDir("claude", filepath.Join(claudeHome, "skills"))

	// Codex: ~/.codex/agents/*
	addBrokenDir("codex", filepath.Join(homeDir, ".codex", "agents"))

	// OpenCode: ~/.opencode/agent/*
	addBrokenDir("opencode", filepath.Join(homeDir, doctorOpenCodeDir, "agent"))

	return broken
}

// countCursorOk returns the number of healthy cursor rule hardlinks for a
// project. Note: this only counts entries scoped to "global" (matching the
// original implementation, which silently skipped project-scope entries).
func countCursorOk(name, path, agentsHome string) int {
	cursorRulesDir := filepath.Join(path, ".cursor", "rules")
	entries, err := os.ReadDir(cursorRulesDir)
	if err != nil {
		return 0
	}
	ok := 0
	for _, e := range entries {
		if strings.Contains(e.Name(), ".dot-agents-backup") || !strings.HasSuffix(e.Name(), ".mdc") {
			continue
		}
		if !strings.HasPrefix(e.Name(), doctorGlobalPrefix) {
			continue
		}
		_ = name // preserved signature; global-scope entries do not depend on project name
		f := filepath.Join(cursorRulesDir, e.Name())
		rest := strings.TrimPrefix(e.Name(), doctorGlobalPrefix)
		if _, linked := cursorRuleHardlinkedAny(f, "global", rest, agentsHome); linked {
			ok++
		}
	}
	return ok
}

// countClaudeRulesOk returns the number of healthy claude rule references for
// a project (symlink/junction with reachable target, OR Windows hardlink to
// the canonical source).
func countClaudeRulesOk(name, path, agentsHome string) int {
	claudeRulesDir := filepath.Join(path, doctorClaudeDir, "rules")
	entries, err := os.ReadDir(claudeRulesDir)
	if err != nil {
		return 0
	}
	ok := 0
	for _, e := range entries {
		linkPath := filepath.Join(claudeRulesDir, e.Name())
		if managedLinkHealthy(linkPath) || claudeRuleHardlinked(linkPath, e.Name(), name, agentsHome) {
			ok++
		}
	}
	return ok
}

// projectSingleFileSources returns the canonical (dst, src) pairs for
// single-file managed links checked by countProjectLinks.
func projectSingleFileSources(name, path, agentsHome string) []struct{ dst, src string } {
	return []struct{ dst, src string }{
		{filepath.Join(path, doctorAgentsMD), filepath.Join(agentsHome, "rules", name, doctorAgentsMD)},
		{filepath.Join(path, ".github", doctorCopilotInstr), filepath.Join(agentsHome, "rules", name, doctorCopilotInstr)},
		{filepath.Join(path, doctorOpenCodeJSON), filepath.Join(agentsHome, "settings", name, doctorOpenCodeJSON)},
		{filepath.Join(path, ".mcp.json"), filepath.Join(agentsHome, "mcp", name, doctorMCPJSON)},
		{filepath.Join(path, ".vscode", doctorMCPJSON), filepath.Join(agentsHome, "mcp", name, "mcp.json.vscode")},
	}
}

// countSingleFilesOk returns the number of healthy single-file managed links
// (symlink/junction OR hardlink to canonical source).
func countSingleFilesOk(name, path, agentsHome string) int {
	ok := 0
	for _, sf := range projectSingleFileSources(name, path, agentsHome) {
		if managedLinkHealthy(sf.dst) {
			ok++
			continue
		}
		if linked, _ := links.AreHardlinked(sf.dst, sf.src); linked {
			ok++
		}
	}
	return ok
}

// countProjectLinks returns (ok, broken) counts for all managed links in a project.
func countProjectLinks(name, path, agentsHome string) (int, int) {
	brokenCount := len(collectBrokenLinks(name, path, agentsHome))
	ok := countCursorOk(name, path, agentsHome) +
		countClaudeRulesOk(name, path, agentsHome) +
		countSingleFilesOk(name, path, agentsHome)
	return ok, brokenCount
}

// printUserConfigStatus prints detailed user-level config status (healthy + broken).
func printUserConfigStatus(_ string) {
	homeDir, err := config.UserHomeDir()
	if err != nil {
		return
	}
	displayBase := homeDir + string(os.PathSeparator)

	printDir := func(dir string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			linkPath := filepath.Join(dir, e.Name())
			if _, isLink, _ := managedLinkBroken(linkPath); isLink {
				printDoctorUserConfigRef(linkPath, displayBase)
			}
		}
	}

	// Claude
	claudeHome := filepath.Join(homeDir, doctorClaudeDir)
	printDoctorUserConfigRef(filepath.Join(claudeHome, "CLAUDE.md"), displayBase)
	printDoctorUserConfigRef(filepath.Join(claudeHome, "settings.json"), displayBase)
	printDir(filepath.Join(claudeHome, "agents"))
	printDir(filepath.Join(claudeHome, "skills"))

	// Codex
	printDir(filepath.Join(homeDir, ".codex", "agents"))

	// OpenCode
	printDir(filepath.Join(homeDir, doctorOpenCodeDir, "agent"))
}

// printDoctorUserConfigRef renders a single managed reference path. A
// resolvable managed link (POSIX symlink / Windows junction) prints OK/X by
// target health; a present non-link path (regular file or Windows hard-linked
// file, which has no reparse point to resolve) prints "(local file)".
// displayBase is the home directory + separator used to render the link as
// relative to home.
func printDoctorUserConfigRef(linkPath, displayBase string) {
	rel := strings.TrimPrefix(linkPath, displayBase)
	if dest, isLink, isBroken := managedLinkBroken(linkPath); isLink {
		displayDest := config.DisplayPath(resolveLinkDest(linkPath, dest))
		if isBroken {
			fmt.Fprintf(os.Stdout, "      %s✗%s %s %s→ %s (broken)%s\n", ui.Red, ui.Reset, rel, ui.Dim, displayDest, ui.Reset)
		} else {
			fmt.Fprintf(os.Stdout, "      %s✓%s %s %s→ %s%s\n", ui.Green, ui.Reset, rel, ui.Dim, displayDest, ui.Reset)
		}
		return
	}
	if _, err := os.Lstat(linkPath); err == nil {
		fmt.Fprintf(os.Stdout, "      %s○%s %s %s(local file)%s\n", ui.Dim, ui.Reset, rel, ui.Dim, ui.Reset)
	}
}
