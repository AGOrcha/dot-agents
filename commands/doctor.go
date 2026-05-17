package commands

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

const doctorOpenCodeDir = ".opencode"

// Owned repo-relative file/name constants shared across doctor's link
// collectors. Centralized so the broken-link and OK-count paths cannot drift.
const (
	doctorAgentsMD     = "AGENTS.md"
	doctorCopilotInstr = "copilot-instructions.md"
	doctorMCPJSON      = "mcp.json"
	doctorOpenCodeJSON = "opencode.json"
	doctorGlobalPrefix = "global--"
)

func NewDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check installations, validate links, detect issues",
		Long: `Audits the local da installation, installed platforms, manifest health,
and managed project links using the same managed paths as da install and
refresh. Doctor is the fastest way to detect drift after manual edits, moved
repositories, or partial setup on a new machine.`,
		Example: ExampleBlock(
			"  da doctor",
			"  da doctor --verbose",
			"  da doctor --dry-run",
		),
		Args: NoArgsWithHints("`da doctor` audits the current installation and does not take a project argument."),
		RunE: runDoctor,
	}
}

func runDoctor(cmd *cobra.Command, args []string) error {
	ui.Header("da doctor")

	agentsHome := config.AgentsHome()

	// Check ~/.agents/
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

	// Check platforms
	ui.Section("Platforms")
	for _, p := range platform.All() {
		if p.IsInstalled() {
			ver := p.Version()
			if ver != "" {
				ui.Bullet("ok", fmt.Sprintf("%s (%s)", p.DisplayName(), ver))
			} else {
				ui.Bullet("ok", p.DisplayName()+" (installed)")
			}
		} else {
			ui.Bullet("none", p.DisplayName()+" (not installed)")
		}
	}

	// Check user-level config in home directory
	ui.Section("User Config")
	userBroken := collectBrokenUserLinks(agentsHome)
	if len(userBroken) == 0 {
		ui.Bullet("ok", "User-level config healthy")
	} else {
		ui.Bullet("warn", fmt.Sprintf("User-level config has %d broken link(s)", len(userBroken)))
	}

	if Flags.Verbose {
		// Show full user-level detail (healthy + broken)
		printUserConfigStatus(agentsHome)
	} else if len(userBroken) > 0 {
		for _, bl := range userBroken {
			fmt.Fprintf(os.Stdout, "      %s✗%s %s %s→ %s%s\n", ui.Red, ui.Reset, bl.linkPath, ui.Dim, bl.dest, ui.Reset)
		}
	}

	// Check projects
	cfg, err := configLoad()
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

	ui.Section(fmt.Sprintf("Projects (%d)", len(names)))
	for _, name := range names {
		path := cfg.GetProjectPath(name)
		if _, err := os.Stat(path); err != nil {
			ui.Bullet("error", fmt.Sprintf("%s — directory missing: %s", name, path))
			continue
		}
		ui.Bullet("ok", fmt.Sprintf("%s (%s)", name, config.DisplayPath(path)))
	}

	// Link health per project
	ui.Section("Link Health")
	totalFixed := 0
	anyBroken := false
	for _, name := range names {
		path := cfg.GetProjectPath(name)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		brokenLinks := collectBrokenLinks(name, path, agentsHome)
		ok, _ := countProjectLinks(name, path, agentsHome)
		total := ok + len(brokenLinks)

		if total == 0 {
			ui.Bullet("none", fmt.Sprintf("%s — no managed links detected", name))
			if Flags.Verbose {
				printAudit(name, path, agentsHome, "", cfg)
			}
			continue
		}
		if len(brokenLinks) == 0 {
			ui.Bullet("ok", fmt.Sprintf("%s — %d links healthy", name, ok))
			if Flags.Verbose {
				printAudit(name, path, agentsHome, "", cfg)
			}
			continue
		}

		anyBroken = true
		ui.Bullet("warn", fmt.Sprintf("%s — %d/%d links OK, %d broken", name, ok, total, len(brokenLinks)))

		if Flags.Verbose {
			// Show full audit detail (healthy + broken) in verbose mode
			printAudit(name, path, agentsHome, "", cfg)
		} else {
			// Default: show only broken links
			for _, bl := range brokenLinks {
				fmt.Fprintf(os.Stdout, "      %s✗%s %s %s→ %s%s\n", ui.Red, ui.Reset, bl.linkPath, ui.Dim, bl.dest, ui.Reset)
			}
		}

		if Flags.DryRun {
			repairedPlatforms := map[string]bool{}
			for _, bl := range brokenLinks {
				if repairedPlatforms[bl.platformID] {
					continue
				}
				p := platform.ByID(bl.platformID)
				if p != nil {
					ui.DryRun(fmt.Sprintf("re-run %s CreateLinks to repair", p.DisplayName()))
				}
				repairedPlatforms[bl.platformID] = true
			}
		} else {
			// Repair: re-run CreateLinks for each affected platform
			repairedPlatforms := map[string]bool{}
			for _, bl := range brokenLinks {
				if repairedPlatforms[bl.platformID] {
					continue
				}
				p := platform.ByID(bl.platformID)
				if p == nil || !p.IsInstalled() {
					continue
				}
				config.SetWindowsMirrorContext(path)
				if err := p.CreateLinks(name, path); err != nil {
					ui.Bullet("warn", fmt.Sprintf("repair %s: %v", p.DisplayName(), err))
				} else {
					ui.Bullet("ok", fmt.Sprintf("repaired %s links", p.DisplayName()))
					totalFixed++
				}
				repairedPlatforms[bl.platformID] = true
			}
		}
	}

	// Manifest checks
	ui.Section("Manifests (.agentsrc.json)")
	anyManifestIssue := false
	for _, name := range names {
		path := cfg.GetProjectPath(name)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		rc, err := config.LoadAgentsRC(path)
		if err != nil {
			if os.IsNotExist(err) {
				ui.Bullet("warn", fmt.Sprintf("%s — no manifest (not git-portable)  hint: da install --generate", name))
			} else {
				ui.Bullet("error", fmt.Sprintf("%s — corrupt manifest: %v", name, err))
			}
			anyManifestIssue = true
			continue
		}
		// Check every declared git source — all must be fetched before reporting healthy.
		var missingGit []string
		var presentGit []string
		for _, src := range rc.Sources {
			if src.Type != "git" || src.URL == "" {
				continue
			}
			cacheDir := config.GitSourceCacheDir(src.URL)
			if _, err := os.Stat(cacheDir); err != nil {
				missingGit = append(missingGit, src.URL)
			} else {
				presentGit = append(presentGit, src.URL)
			}
		}
		if len(missingGit) > 0 {
			for _, url := range missingGit {
				ui.Bullet("warn", fmt.Sprintf("%s — git source not yet fetched: %s  hint: da install", name, url))
			}
			anyManifestIssue = true
		} else if len(presentGit) > 0 {
			ui.Bullet("ok", fmt.Sprintf("%s — manifest ok (%d git source(s))", name, len(presentGit)))
		} else {
			ui.Bullet("ok", fmt.Sprintf("%s — manifest ok (local)", name))
		}
	}
	if !anyManifestIssue {
		fmt.Fprintf(os.Stdout, "  %sTip: run with -v to see per-project manifest details%s\n", ui.Dim, ui.Reset)
	}

	// Orphan canonical resources: ~/.agents/{skills,agents}/<project>/<name>/
	// exists but the project has no .agents/<bucket>/<name> back-link
	// (symlink or real dir).
	ui.Section("Canonical Resources")
	anyOrphan := false
	for _, bucket := range []string{"skills", "agents"} {
		for _, name := range names {
			path := cfg.GetProjectPath(name)
			if _, err := os.Stat(path); err != nil {
				continue
			}
			orphans := collectOrphanCanonicals(name, path, agentsHome, bucket)
			for _, orphan := range orphans {
				anyOrphan = true
				// Orphan entries may carry a "  (mis-pointed: …)" annotation;
				// strip it before composing filesystem paths.
				orphanName := orphan
				if idx := strings.Index(orphan, "  ("); idx >= 0 {
					orphanName = orphan[:idx]
				}
				canonicalPath := filepath.Join(agentsHome, bucket, name, orphanName)
				backLink := filepath.Join(path, ".agents", bucket, orphanName)
				// `promote --force` requires a real <project>/.agents/<bucket>/<name>
				// directory to copy from, which an orphan by definition does not have.
				// Surface the two real recovery options instead.
				ui.Bullet("warn", fmt.Sprintf("%s — orphan canonical %s %q at %s; hint: restore the back-link with `ln -s %s %s` or purge the orphan with `rm -rf %s`.",
					name, bucket, orphan, canonicalPath,
					canonicalPath, backLink,
					canonicalPath))
			}
		}
	}
	if !anyOrphan {
		ui.Bullet("ok", "No orphan canonical resources")
	}

	ui.Section("Plugins")
	pluginSpecs, pluginErr := platform.ListPluginSpecs(agentsHome, "")
	if pluginErr != nil {
		ui.Bullet("error", fmt.Sprintf("plugin bundles unavailable: %v", pluginErr))
	} else if len(pluginSpecs) == 0 {
		ui.Info("No canonical plugin bundles")
	} else {
		for _, spec := range pluginSpecs {
			bundleLabel := filepath.Join(spec.Scope, spec.Name)
			for _, platformID := range spec.Platforms {
				if platformID != "opencode" {
					ui.Bullet("warn", fmt.Sprintf("%s: platforms includes %s but no emitter is implemented yet", bundleLabel, platformID))
				}
			}
			if hasPluginPlatform(spec.Platforms, "opencode") {
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
		}
	}

	fmt.Fprintln(os.Stdout)
	if !anyBroken {
		if !Flags.Verbose {
			// Suggest verbose for full link detail when everything is healthy
			fmt.Fprintf(os.Stdout, "  %sTip: run with -v to see full link details per project%s\n\n", ui.Dim, ui.Reset)
		}
		return nil
	}
	if Flags.DryRun {
		ui.Info("Run without --dry-run to apply repairs.")
	} else if totalFixed > 0 {
		ui.Success(fmt.Sprintf("Repaired links in %d platform(s). Run 'da status --audit' to verify.", totalFixed))
		fmt.Fprintln(os.Stdout)
	}
	return nil
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

// resolveLinkDest normalizes a managed-link target to an absolute path so it
// can be stat'd. Junction targets are already absolute; POSIX symlinks may be
// relative to the link's directory.
func resolveLinkDest(linkPath, dest string) string {
	if dest == "" || filepath.IsAbs(dest) {
		return dest
	}
	return filepath.Clean(filepath.Join(filepath.Dir(linkPath), dest))
}

// managedLinkBroken reports, for a single managed link path, whether it is a
// resolvable managed link (POSIX symlink / Windows junction), its resolved
// target for display, and whether that target is missing (the link is
// broken).
//
// A Windows hard-linked managed *file* has no reparse point and therefore no
// resolvable target — ManagedLinkTarget returns ("", false). Such a file
// cannot dangle (its target inode must exist), so it is reported isLink=false
// and broken=false here; the OK-count path handles healthy hard links via
// links.AreHardlinked instead. This keeps POSIX behavior identical (symlinks
// still resolve via ManagedLinkTarget) while not misreporting Windows hard
// links as broken.
func managedLinkBroken(linkPath string) (dest string, isLink, broken bool) {
	raw, ok := links.ManagedLinkTarget(linkPath)
	if !ok {
		return "", false, false
	}
	resolved := resolveLinkDest(linkPath, raw)
	if _, err := os.Stat(resolved); err != nil {
		return raw, true, true
	}
	return raw, true, false
}

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

// collectBrokenLinks returns all broken managed links for a project.
func collectBrokenLinks(name, path, agentsHome string) []brokenLink {
	var broken []brokenLink
	displayBase := path + "/"

	rel := func(p string) string {
		return strings.TrimPrefix(p, displayBase)
	}

	// Cursor hard links
	cursorRulesDir := filepath.Join(path, ".cursor", "rules")
	if entries, err := os.ReadDir(cursorRulesDir); err == nil {
		for _, e := range entries {
			// Skip backup and non-.mdc files
			if strings.Contains(e.Name(), ".dot-agents-backup") {
				continue
			}
			if !strings.HasSuffix(e.Name(), ".mdc") {
				continue
			}
			f := filepath.Join(cursorRulesDir, e.Name())
			if strings.HasPrefix(e.Name(), doctorGlobalPrefix) {
				srcName := strings.TrimPrefix(e.Name(), doctorGlobalPrefix)
				src := filepath.Join(agentsHome, "rules", "global", srcName)
				if linked, _ := links.AreHardlinked(f, src); linked {
					continue
				}
				srcMD := strings.TrimSuffix(srcName, ".mdc") + ".md"
				src2 := filepath.Join(agentsHome, "rules", "global", srcMD)
				if linked, _ := links.AreHardlinked(f, src2); linked {
					continue
				}
				broken = append(broken, brokenLink{
					platformID: "cursor",
					linkPath:   rel(f),
					dest:       config.DisplayPath(src),
				})
			} else if strings.HasPrefix(e.Name(), name+"--") {
				srcName := strings.TrimPrefix(e.Name(), name+"--")
				src := filepath.Join(agentsHome, "rules", name, srcName)
				if linked, _ := links.AreHardlinked(f, src); linked {
					continue
				}
				srcMD := strings.TrimSuffix(srcName, ".mdc") + ".md"
				src2 := filepath.Join(agentsHome, "rules", name, srcMD)
				if linked, _ := links.AreHardlinked(f, src2); linked {
					continue
				}
				broken = append(broken, brokenLink{
					platformID: "cursor",
					linkPath:   rel(f),
					dest:       config.DisplayPath(src),
				})
			}
		}
	}

	// Claude symlinks
	claudeRulesDir := filepath.Join(path, ".claude", "rules")
	if entries, err := os.ReadDir(claudeRulesDir); err == nil {
		for _, e := range entries {
			linkPath := filepath.Join(claudeRulesDir, e.Name())
			if dest, isLink, isBroken := managedLinkBroken(linkPath); isLink && isBroken {
				broken = append(broken, brokenLink{
					platformID: "claude",
					linkPath:   rel(linkPath),
					dest:       config.DisplayPath(dest),
				})
			}
		}
	}

	singleFiles := []struct {
		platformID string
		path       string
	}{
		{"codex", filepath.Join(path, doctorAgentsMD)},
		{"copilot", filepath.Join(path, ".github", doctorCopilotInstr)},
		{"copilot", filepath.Join(path, ".vscode", doctorMCPJSON)},
		{"claude", filepath.Join(path, ".mcp.json")},
		{"opencode", filepath.Join(path, doctorOpenCodeJSON)},
	}
	for _, sf := range singleFiles {
		if dest, isLink, isBroken := managedLinkBroken(sf.path); isLink && isBroken {
			broken = append(broken, brokenLink{
				platformID: sf.platformID,
				linkPath:   rel(sf.path),
				dest:       config.DisplayPath(dest),
			})
		}
	}

	return broken
}

// collectBrokenUserLinks returns all broken managed user-level links in the home directory.
func collectBrokenUserLinks(agentsHome string) []brokenLink {
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
	claudeHome := filepath.Join(homeDir, ".claude")
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

// countProjectLinks returns (ok, broken) counts for all managed links in a project.
func countProjectLinks(name, path, agentsHome string) (int, int) {
	brokenLinks := collectBrokenLinks(name, path, agentsHome)
	brokenCount := len(brokenLinks)

	ok := 0
	// Cursor hard links
	cursorRulesDir := filepath.Join(path, ".cursor", "rules")
	if entries, err := os.ReadDir(cursorRulesDir); err == nil {
		for _, e := range entries {
			if strings.Contains(e.Name(), ".dot-agents-backup") || !strings.HasSuffix(e.Name(), ".mdc") {
				continue
			}
			f := filepath.Join(cursorRulesDir, e.Name())
			if strings.HasPrefix(e.Name(), doctorGlobalPrefix) {
				srcName := strings.TrimPrefix(e.Name(), doctorGlobalPrefix)
				src := filepath.Join(agentsHome, "rules", "global", srcName)
				if linked, _ := links.AreHardlinked(f, src); linked {
					ok++
					continue
				}
				srcMD := strings.TrimSuffix(srcName, ".mdc") + ".md"
				src2 := filepath.Join(agentsHome, "rules", "global", srcMD)
				if linked, _ := links.AreHardlinked(f, src2); linked {
					ok++
				}
			}
		}
	}
	// Claude rules: a managed reference is a resolvable symlink/junction whose
	// target exists, or (Windows files) a hard link to the canonical rule
	// source reconstructed from the "<scope>--<name>" entry name.
	claudeRulesDir := filepath.Join(path, ".claude", "rules")
	if entries, err := os.ReadDir(claudeRulesDir); err == nil {
		for _, e := range entries {
			linkPath := filepath.Join(claudeRulesDir, e.Name())
			if managedLinkHealthy(linkPath) {
				ok++
				continue
			}
			if claudeRuleHardlinked(linkPath, e.Name(), name, agentsHome) {
				ok++
			}
		}
	}
	// Single-file managed links: a resolvable symlink/junction whose target
	// exists, or (Windows files) a hard link to the canonical source. The
	// canonical source is reconstructed from the project scope, mirroring
	// the cursor/claude paths above and collectBrokenLinks' singleFiles.
	for _, sf := range []struct{ dst, src string }{
		{filepath.Join(path, doctorAgentsMD), filepath.Join(agentsHome, "rules", name, doctorAgentsMD)},
		{filepath.Join(path, ".github", doctorCopilotInstr), filepath.Join(agentsHome, "rules", name, doctorCopilotInstr)},
		{filepath.Join(path, doctorOpenCodeJSON), filepath.Join(agentsHome, "settings", name, doctorOpenCodeJSON)},
		{filepath.Join(path, ".mcp.json"), filepath.Join(agentsHome, "mcp", name, doctorMCPJSON)},
		{filepath.Join(path, ".vscode", doctorMCPJSON), filepath.Join(agentsHome, "mcp", name, "mcp.json.vscode")},
	} {
		if managedLinkHealthy(sf.dst) {
			ok++
			continue
		}
		if linked, _ := links.AreHardlinked(sf.dst, sf.src); linked {
			ok++
		}
	}
	return ok, brokenCount
}

// printUserConfigStatus prints detailed user-level config status (healthy + broken).
func printUserConfigStatus(agentsHome string) {
	homeDir, err := config.UserHomeDir()
	if err != nil {
		return
	}
	displayBase := homeDir + string(os.PathSeparator)
	rel := func(p string) string {
		return strings.TrimPrefix(p, displayBase)
	}

	// printOne renders a single managed reference path. A resolvable managed
	// link (POSIX symlink / Windows junction) prints ✓/✗ by target health; a
	// present non-link path (regular file or Windows hard-linked file, which
	// has no reparse point to resolve) prints "(local file)".
	printOne := func(linkPath string) {
		if dest, isLink, isBroken := managedLinkBroken(linkPath); isLink {
			displayDest := config.DisplayPath(resolveLinkDest(linkPath, dest))
			if isBroken {
				fmt.Fprintf(os.Stdout, "      %s✗%s %s %s→ %s (broken)%s\n", ui.Red, ui.Reset, rel(linkPath), ui.Dim, displayDest, ui.Reset)
			} else {
				fmt.Fprintf(os.Stdout, "      %s✓%s %s %s→ %s%s\n", ui.Green, ui.Reset, rel(linkPath), ui.Dim, displayDest, ui.Reset)
			}
			return
		}
		if _, err := os.Lstat(linkPath); err == nil {
			fmt.Fprintf(os.Stdout, "      %s○%s %s %s(local file)%s\n", ui.Dim, ui.Reset, rel(linkPath), ui.Dim, ui.Reset)
		}
	}
	printDir := func(dir string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			linkPath := filepath.Join(dir, e.Name())
			if _, isLink, _ := managedLinkBroken(linkPath); isLink {
				printOne(linkPath)
			}
		}
	}

	// Claude
	claudeHome := filepath.Join(homeDir, ".claude")
	printOne(filepath.Join(claudeHome, "CLAUDE.md"))
	printOne(filepath.Join(claudeHome, "settings.json"))
	printDir(filepath.Join(claudeHome, "agents"))
	printDir(filepath.Join(claudeHome, "skills"))

	// Codex
	printDir(filepath.Join(homeDir, ".codex", "agents"))

	// OpenCode
	printDir(filepath.Join(homeDir, doctorOpenCodeDir, "agent"))
}
