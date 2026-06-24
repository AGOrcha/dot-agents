package lifecycle

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/links"
	"github.com/AGOrcha/dot-agents/internal/platform"
	"github.com/AGOrcha/dot-agents/internal/ui"
	"github.com/spf13/cobra"
)

const (
	doctorOpenCodeDir = ".opencode"
	doctorClaudeDir   = ".claude"
)

// doctorGlobalPrefix is the canonical scope prefix for a global-scoped
// managed rule entry (e.g. .claude/rules/global--<name>). Used by the
// Windows hard-link classifier claudeRuleHardlinked.
const doctorGlobalPrefix = "global--"

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
repositories, or partial setup on a new machine.

Doctor is read-only: it reports problems but never repairs them. When it finds
broken links or drift it tells you which command to run (for example da refresh
to relink, or da config sync to reconcile the lockfile).`,
		Example: deps.ExampleBlock(
			"  da doctor",
			"  da doctor --verbose",
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
	anyBroken := reportLinkHealth(cfg, names, agentsHome)
	reportManifestHealth(cfg, names)
	reportLockHealth(cfg, names)
	reportOrphanCanonicals(cfg, names, agentsHome)
	reportPluginHealth(cfg, names, agentsHome)

	finalizeDoctorRun(anyBroken)
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
// counts and broken-link detail (or full audit in verbose mode). It is
// read-only — doctor never repairs (§7A.6); it returns whether any broken
// links were observed so finalizeDoctorRun can point the user at da refresh.
func reportLinkHealth(cfg *config.Config, names []string, agentsHome string) bool {
	ui.Section("Link Health")
	anyBroken := false
	for _, name := range names {
		path := cfg.GetProjectPath(name)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		if reportOneProjectLinkHealth(name, path, agentsHome, cfg) {
			anyBroken = true
		}
	}
	return anyBroken
}

// reportOneProjectLinkHealth handles the link-health audit for a single
// project. Returns true when broken links were observed; false for empty or
// fully-healthy projects.
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
	legacy := reportManifestDeprecation(name, rc)
	missingGit, presentGit := partitionManifestGitSources(rc)
	if len(missingGit) > 0 {
		for _, url := range missingGit {
			ui.Bullet("warn", fmt.Sprintf("%s — git source not yet fetched: %s  hint: da install", name, url))
		}
		return true
	}
	if legacy {
		return true
	}
	if len(presentGit) > 0 {
		ui.Bullet("ok", fmt.Sprintf("%s — manifest ok (%d git source(s))", name, len(presentGit)))
	} else {
		ui.Bullet("ok", fmt.Sprintf("%s — manifest ok (local)", name))
	}
	return false
}

// reportManifestDeprecation emits a warn bullet when the manifest uses a
// legacy v1 shape (old schema version or deprecated v1 keys folded silently on
// load). It is read-only — doctor only surfaces the deprecation (config-v2
// §15.3); the file still loads. Returns true when a warning was emitted so the
// caller can mark the manifest section as having an issue.
func reportManifestDeprecation(name string, rc *config.AgentsRC) bool {
	w := config.DetectV1Deprecation(rc)
	if !w.Detected {
		return false
	}
	ui.Bullet("warn", fmt.Sprintf("%s — %s  hint: da config migrate", name, w.Message()))
	return true
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

// reportLockHealth prints the "Lockfile (.agentsrc.lock)" section: per-project
// drift between the committed lockfile and the declared `extends`/`packages`
// units. It is read-only — doctor never repairs the lock, it only surfaces
// drift (config-v2 p2). Projects that declare no units are silent (lock drift
// is not applicable to a local-only manifest).
func reportLockHealth(cfg *config.Config, names []string) {
	ui.Section("Lockfile (.agentsrc.lock)")
	anyApplicable := false
	anyIssue := false
	for _, name := range names {
		path := cfg.GetProjectPath(name)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		applicable, issue := reportOneProjectLockHealth(name, path)
		anyApplicable = anyApplicable || applicable
		anyIssue = anyIssue || issue
	}
	if !anyApplicable {
		ui.Bullet("ok", "No projects declare config units — lockfile drift not applicable")
		return
	}
	if !anyIssue {
		ui.Bullet("ok", "All declared config units are locked")
	}
}

// reportOneProjectLockHealth reports lock drift for a single project. The first
// return is whether lock drift applies (the manifest declares extends); the
// second is whether any drift was surfaced. A manifest load error is silent
// here — reportManifestHealth already owns missing/corrupt manifest reporting.
func reportOneProjectLockHealth(name, path string) (applicable, issue bool) {
	drift, err := config.LockDrift(path)
	if err != nil {
		// Manifest missing/corrupt is reported by reportManifestHealth; do not
		// double-report here.
		return false, false
	}
	if !drift.HasDeclaredUnits {
		return false, false
	}
	if !drift.LockPresent {
		ui.Bullet("warn", fmt.Sprintf("%s — declares units but has no .agentsrc.lock  hint: da install", name))
		return true, true
	}
	problems := drift.Problems()
	if len(problems) == 0 {
		ui.Bullet("ok", fmt.Sprintf("%s — %d unit(s) locked", name, len(drift.Units)))
		return true, false
	}
	for _, p := range problems {
		ui.Bullet("warn", fmt.Sprintf("%s — %s: %s%s", name, p.Ref, lockDriftMessage(p.Status), lockDriftHint(p.Status)))
	}
	return true, true
}

// lockDriftMessage maps a drift status to a short human-readable phrase.
func lockDriftMessage(s config.LockDriftStatus) string {
	switch s {
	case config.LockStatusMissingFromLock:
		return "declared in manifest but absent from lock"
	case config.LockStatusExtraInLock:
		return "in lock but no longer declared in manifest"
	default:
		return string(s)
	}
}

// lockDriftHint maps a drift status to a remediation hint suffix.
func lockDriftHint(s config.LockDriftStatus) string {
	switch s {
	case config.LockStatusMissingFromLock:
		return "  hint: da config sync"
	case config.LockStatusExtraInLock:
		return "  hint: da config sync to prune"
	default:
		return ""
	}
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
// link-health audit. Doctor is read-only (§7A.6): a healthy run prints an
// optional verbose tip; a broken run points the user at da refresh rather than
// repairing anything itself.
func finalizeDoctorRun(anyBroken bool) {
	fmt.Fprintln(os.Stdout)
	if !anyBroken {
		if !Flags.Verbose {
			fmt.Fprintf(os.Stdout, "  %sTip: run with -v to see full link details per project%s\n\n", ui.Dim, ui.Reset)
		}
		return
	}
	ui.Info("Broken links detected. Run 'da refresh' to relink managed entities, then 'da status --audit' to verify.")
	fmt.Fprintln(os.Stdout)
}

// collectOrphanCanonicals returns the resource names under
// ~/.agents/<bucket>/<projectName>/ that have no back-link
// (symlink or real dir) at <projectPath>/.agents/<bucket>/<name>.
// These are leftovers when a user manually deleted the repo-local source
// after a promote, leaving the canonical copy orphaned.
//
// Per platform-driven-diagnostics P4, the orphan-detection logic now lives in
// internal/platform behind the OrphanCanonicalReporter sister interface
// (claude owns the "skills" bucket, codex owns "agents"). doctor delegates by
// type-assertion and does no orphan classification of its own — each canonical
// bucket is owned by exactly one platform, so iterating every reporter is
// double-count free. The returned []OrphanCanonical is flattened back to the
// annotated-string shape callers (and the orphan-warning printer) already
// expect: "<name>" for a plain orphan, "<name>  (mis-pointed: <target>)" for
// a mis-pointed back-link.
func collectOrphanCanonicals(projectName, projectPath, agentsHome, bucket string) []string {
	var orphans []string
	for _, p := range platform.All() {
		r, ok := p.(platform.OrphanCanonicalReporter)
		if !ok {
			continue
		}
		for _, oc := range r.OrphanCanonicals(projectName, projectPath, agentsHome, bucket) {
			orphans = append(orphans, oc.Name+oc.DisplayNote)
		}
	}
	return orphans
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
// duplicates per SHAPE.md OD-2).

// claudeRuleHardlinked reports whether a .claude/rules entry is a Windows
// managed *file* hard link to its canonical rule source. The entry name is
// "<scope>--<rest>" where scope is "global" or the project name; the source
// lives at <agentsHome>/rules/<scope>/<rest> with a .mdc→.md fallback. On
// POSIX these are symlinks (resolved via managedLinkBroken) so this is a
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

// collectBrokenLinks returns all broken managed links for a project.
//
// Per platform-driven-diagnostics P2, every platform implements
// BrokenLinkReporter and owns its own enumeration. doctor delegates by
// type-assertion (sister-interface pattern from
// internal/platform/diagnostics.go) and does no per-platform classification
// of its own — the legacy projectSingleFiles table + collectSingleFileBrokenLinks
// helper were deleted in P2 because every platform now reports its own
// single-file links (codex AGENTS.md, copilot .github/copilot-instructions.md +
// .vscode/mcp.json, opencode opencode.json). Adding a new platform from this
// point forward is a single internal/platform/<name>.go change.
func collectBrokenLinks(name, path, agentsHome string) []brokenLink {
	displayBase := path + "/"
	rel := func(p string) string {
		return strings.TrimPrefix(p, displayBase)
	}
	var broken []brokenLink
	for _, p := range platform.All() {
		r, ok := p.(platform.BrokenLinkReporter)
		if !ok {
			continue
		}
		for _, bl := range r.BrokenLinks(name, path, agentsHome) {
			broken = append(broken, brokenLink{
				platformID: bl.PlatformID,
				linkPath:   rel(bl.LinkPath),
				dest:       bl.DisplayDest,
			})
		}
	}
	return broken
}

// collectBrokenUserLinks returns all broken managed user-level links in the
// home directory.
//
// Per platform-driven-diagnostics P4, the per-platform user-home enumeration
// now lives in internal/platform behind the UserConfigReporter sister
// interface. doctor resolves the home directory once, delegates by
// type-assertion, and flattens each platform's []platform.BrokenLink into the
// lifecycle brokenLink shape. The link path is rendered home-relative and the
// dest via DisplayPath, identical to the prior inline implementation.
// claude/codex/opencode/cursor report real managed user-home links; copilot
// implements UserConfigReporter but reports nothing because dot-agents does not
// yet wire its (documented) user-config layer (tracked in PLATFORM_DIRS_DOCS).
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

	for _, p := range platform.All() {
		r, ok := p.(platform.UserConfigReporter)
		if !ok {
			continue
		}
		for _, bl := range r.UserBrokenLinks(homeDir) {
			broken = append(broken, brokenLink{
				platformID: bl.PlatformID,
				linkPath:   rel(bl.LinkPath),
				dest:       bl.DisplayDest,
			})
		}
	}

	return broken
}

// countProjectLinks returns (ok, broken) counts for all managed links in a
// project. The healthy tally is delegated to each platform's LinkCounter
// (the same source of truth that drives the badge row and the per-platform
// verbose audit), so the headline "N links healthy" matches the real managed
// link count on disk rather than the partial cursor+claude-rules+single-file
// subset the doctor used to recount on its own. Broken links continue to come
// from the BrokenLinkReporter enumeration (collectBrokenLinks) because that
// path also feeds the broken-link detail rendering and the repair trigger.
func countProjectLinks(name, path, agentsHome string) (int, int) {
	brokenCount := len(collectBrokenLinks(name, path, agentsHome))
	ok := countPlatformLinksOk(name, path, agentsHome)
	return ok, brokenCount
}

// countPlatformLinksOk sums the healthy managed-link tally reported by every
// platform that implements platform.LinkCounter. Only the ok return is used;
// the broken return is intentionally ignored here so it is not double-counted
// against the BrokenLinkReporter enumeration that owns broken-link reporting.
func countPlatformLinksOk(name, path, agentsHome string) int {
	ok := 0
	for _, p := range platform.All() {
		c, isCounter := p.(platform.LinkCounter)
		if !isCounter {
			continue
		}
		platformOK, _ := c.CountLinks(name, path, agentsHome)
		ok += platformOK
	}
	return ok
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
