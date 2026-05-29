package platform

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/links"
)

type codex struct {
	io platformIO
}

const (
	codexAgentsDir      = ".agents"
	codexDir            = ".codex"
	codexConfigTOML     = "config.toml"
	codexHooksJSON      = "hooks.json"
	codexAgentsMarkdown = "AGENTS.md"
	codexAgentMDFile    = "AGENT.md"
)

func NewCodex() Platform { return &codex{io: stdPlatformIO{}} }

func (c *codex) ID() string          { return "codex" }
func (c *codex) DisplayName() string { return "Codex CLI" }

// SessionReader implementation.
// CODEX_SESSION_ID: not yet confirmed from Codex docs; update SessionEnvs when verified.
// ResolveModel: scans ~/.codex/sessions/YYYY/MM/DD/rollout-*-<id>.jsonl for the model field.
func (c *codex) AIAgentPrefix() string    { return "codex" }
func (c *codex) SessionEnvs() []string    { return []string{"CODEX_SESSION_ID"} }
func (c *codex) EntrypointEnvs() []string { return nil }
func (c *codex) ResolveModel(home, _ /* projectPath */, sessionID string) string {
	return resolveCodexModelFromJSONL(home, sessionID)
}

// StatsReader implementation.
func (c *codex) ReadUsageStats(home string) *PlatformUsageStats {
	return codexReadUsageStats(home)
}

func codexReadUsageStats(home string) *PlatformUsageStats {
	indexPath := filepath.Join(home, codexDir, "session_index.jsonl")
	f, err := os.Open(indexPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	type entry struct {
		ID         string `json:"id"`
		ThreadName string `json:"thread_name"`
		UpdatedAt  string `json:"updated_at"`
	}
	var all []entry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e entry
		if err := json.Unmarshal(sc.Bytes(), &e); err == nil {
			all = append(all, e)
		}
	}
	if len(all) == 0 {
		return nil
	}
	stats := &PlatformUsageStats{
		PlatformID:    "codex",
		TotalSessions: len(all),
	}
	start := 0
	if len(all) > 10 {
		start = len(all) - 10
	}
	for _, e := range all[start:] {
		stats.RecentSessions = append(stats.RecentSessions, SessionSummary{
			ID:        e.ID,
			Name:      e.ThreadName,
			UpdatedAt: e.UpdatedAt,
		})
	}
	return stats
}

// SessionTokenScanner implementation.
func (c *codex) ScanSessionTokens(home, _ /* projectPath */, sessionID, afterTimestamp string) SessionTokenMetrics {
	return codexScanSessionTokens(home, sessionID, afterTimestamp)
}

func (c *codex) IsInstalled() bool {
	return probeInstalled("codex")
}

func (c *codex) Version() string {
	return probeVersionLine("codex")
}

func (c *codex) HasDeprecatedFormat(repoPath string) bool { return false }
func (c *codex) DeprecatedDetails(repoPath string) string { return "" }

func (c *codex) CreateLinks(project, repoPath string) error {
	agentsHome := config.AgentsHome()

	if err := c.ensureUserAgents(agentsHome); err != nil {
		return err
	}
	if err := c.ensureUserSkills(agentsHome); err != nil {
		return err
	}

	// AGENTS.md: global then project override
	if err := c.linkCodexAgentsMD(project, repoPath, agentsHome); err != nil {
		return err
	}

	// .codex/config.toml
	if err := c.linkCodexConfigToml(project, repoPath, agentsHome); err != nil {
		return err
	}

	// Project agents → .codex/agents/*.toml (rendered by CollectAndExecuteSharedTargetPlan)
	if err := c.createAgentsLinks(project, repoPath, agentsHome); err != nil {
		return err
	}

	// Project skills → .agents/skills/
	if err := c.createSkillsLinks(project, repoPath, agentsHome); err != nil {
		return err
	}

	// Project hooks → .codex/hooks.json
	if err := c.createHooksLinks(project, repoPath, agentsHome); err != nil {
		return err
	}

	return nil
}

// linkCodexAgentsMD points the owned repo AGENTS.md at the highest-priority
// canonical source: the first existing global candidate, then a project
// override if present (the override symlink-replaces the global). Every
// links.SymlinkReplacing error propagates unchanged — this is the
// link-error-propagation contract added by a prior remediation.
func (c *codex) linkCodexAgentsMD(project, repoPath, agentsHome string) error {
	dst := filepath.Join(repoPath, codexAgentsMarkdown)
	globalCandidates := []string{
		filepath.Join(agentsHome, "rules", "global", "agents.md"),
		filepath.Join(agentsHome, "rules", "global", "agents.mdc"),
		filepath.Join(agentsHome, "rules", "global", "rules.md"),
		filepath.Join(agentsHome, "rules", "global", "rules.mdc"),
	}
	for _, src := range globalCandidates {
		if _, err := os.Stat(src); err == nil {
			// Managed-replace: AGENTS.md is a dot-agents output at a fixed
			// owned repo path that refresh re-points (global → project). A
			// stale managed symlink is idempotently re-pointed; a genuine
			// user-authored AGENTS.md is preserved as
			// AGENTS.md.dot-agents-backup, never silently destroyed.
			if err := links.SymlinkReplacing(src, dst, backupSidecar); err != nil {
				return err
			}
			break
		}
	}
	// Project override
	for _, name := range []string{"agents.md", "agents.mdc"} {
		src := filepath.Join(agentsHome, "rules", project, name)
		if _, err := os.Stat(src); err == nil {
			if err := links.SymlinkReplacing(src, dst, backupSidecar); err != nil {
				return err
			}
			break
		}
	}
	return nil
}

// linkCodexConfigToml ensures .codex/ exists and symlink-replaces
// .codex/config.toml with the scoped codex.toml when one resolves. Link
// errors propagate unchanged.
func (c *codex) linkCodexConfigToml(project, repoPath, agentsHome string) error {
	if err := c.io.MkdirAll(filepath.Join(repoPath, codexDir), 0755); err != nil {
		return err
	}
	if src := resolveScopedFile(agentsHome, "settings", project, "codex.toml"); src != "" {
		// Managed-replace at a fixed owned path (.codex/config.toml).
		if err := links.SymlinkReplacing(src, filepath.Join(repoPath, codexDir, codexConfigTOML), backupSidecar); err != nil {
			return err
		}
	}
	return nil
}

func (c *codex) ensureUserAgents(agentsHome string) error {
	globalAgents := filepath.Join(agentsHome, "agents", "global")
	if _, err := os.Stat(globalAgents); err != nil {
		return nil
	}
	for _, homeRoot := range config.UserHomeRoots() {
		userAgentsDir := filepath.Join(homeRoot, codexDir, "agents")
		if err := c.io.MkdirAll(userAgentsDir, 0755); err != nil {
			continue
		}
		if err := c.writeCodexAgents(agentsHome, "global", userAgentsDir); err != nil {
			return err
		}
	}
	return nil
}

func (c *codex) ensureUserSkills(agentsHome string) error {
	for _, homeRoot := range config.UserHomeRoots() {
		userSkillsDir := filepath.Join(homeRoot, codexAgentsDir, "skills")
		if err := syncScopedDirSymlinks(c.io, agentsHome, "skills", "global", "SKILL.md", userSkillsDir); err != nil {
			return err
		}
	}
	return nil
}

func (c *codex) createAgentsLinks(project, repoPath, agentsHome string) error {
	// TOML files are materialized by CollectAndExecuteSharedTargetPlan; prune stale
	// `.toml` when canonical agents are removed.
	return pruneCodexRepoAgentTomls(project, repoPath, agentsHome)
}

func (c *codex) createSkillsLinks(project, repoPath, _ string) error {
	return nil
}

func (c *codex) createHooksLinks(project, repoPath, agentsHome string) error {
	if err := c.writeRepoHooks(project, repoPath, agentsHome); err != nil {
		return err
	}
	return c.writeUserHomeHooks(project, agentsHome)
}

func (c *codex) writeRepoHooks(project, repoPath, agentsHome string) error {
	repoTarget := filepath.Join(repoPath, codexDir, codexHooksJSON)
	repoBundles, err := collectCanonicalHookSpecsForPlatform(agentsHome, project, c.ID(), "global", project)
	if err != nil {
		return err
	}
	if err := c.io.MkdirAll(filepath.Join(repoPath, codexDir), 0755); err != nil {
		return err
	}
	return emitPreferredHookFile(
		c.io,
		repoTarget,
		renderCodexHookConfig,
		resolveHookSpec(agentsHome, []string{"hooks"}, project, "codex.json", "codex-hooks.json"),
		directSymlinkHookMode,
		func(p string) error { return removeRenderedCodexHookConfig(c.io, p) },
		repoBundles,
	)
}

func (c *codex) writeUserHomeHooks(project, agentsHome string) error {
	globalBundles, err := collectCanonicalHookSpecsForPlatform(agentsHome, project, c.ID(), "global")
	if err != nil {
		return err
	}
	return emitPreferredHookFileToUserHomes(
		c.io,
		filepath.Join(codexDir, codexHooksJSON),
		renderCodexHookConfig,
		resolveHookSpec(agentsHome, []string{"hooks"}, project, "codex.json", "codex-hooks.json"),
		directSymlinkHookMode,
		func(p string) error { return removeRenderedCodexHookConfig(c.io, p) },
		globalBundles,
	)
}

func (c *codex) RemoveLinks(project, repoPath string) error {
	agentsHome := config.AgentsHome()

	var errs []error
	errs = append(errs,
		links.RemoveIfSymlinkUnder(filepath.Join(repoPath, codexAgentsMarkdown), agentsHome),
		links.RemoveIfSymlinkUnder(filepath.Join(repoPath, codexDir, codexConfigTOML), agentsHome),
	)
	repoBundles, err := collectCanonicalHookSpecsForPlatform(agentsHome, project, c.ID(), "global", project)
	if err == nil && len(repoBundles) > 0 {
		_ = removeManagedRenderedHookFile(c.io, repoBundles, filepath.Join(repoPath, codexDir, codexHooksJSON), renderCodexHookConfig)
	}
	errs = append(errs, links.RemoveIfSymlinkUnder(filepath.Join(repoPath, codexDir, codexHooksJSON), agentsHome))

	errs = append(errs, c.pruneManagedCodexAgentTomls(agentsHome, project, filepath.Join(repoPath, codexDir, "agents")))

	skillsDir := filepath.Join(repoPath, codexAgentsDir, "skills")
	if entries, err := os.ReadDir(skillsDir); err == nil {
		for _, e := range entries {
			errs = append(errs, links.RemoveIfSymlinkUnder(filepath.Join(skillsDir, e.Name()), agentsHome))
		}
	}

	return errors.Join(errs...)
}

// pruneCodexRepoAgentTomls deletes stale `.codex/agents/*.toml` files in the
// repo whose canonical AGENT.md no longer exists. ENOENT on the canonical
// agents bucket OR the codex dst dir is a no-op — nothing to prune. Other
// errors propagate.
func pruneCodexRepoAgentTomls(project, repoPath, agentsHome string) error {
	entries, err := listScopedResourceDirs(agentsHome, "agents", project, codexAgentMDFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	wanted := map[string]bool{}
	for _, entry := range entries {
		wanted[entry.Name+".toml"] = true
	}
	dstRoot := filepath.Join(repoPath, codexDir, "agents")
	existing, err := os.ReadDir(dstRoot)
	if err != nil {
		// Genuine absence is a no-op; a path that exists but is not a
		// listable directory is a real fault that must propagate on every
		// OS (Windows os.ReadDir on a regular-file path maps to a
		// NotExist-class error — %v breaks the fs.ErrNotExist chain).
		if _, statErr := os.Lstat(dstRoot); statErr == nil {
			return fmt.Errorf("listing codex agents dir %s: not a listable directory (%v)", dstRoot, err)
		}
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range existing {
		if !strings.HasSuffix(e.Name(), ".toml") || wanted[e.Name()] {
			continue
		}
		_ = os.Remove(filepath.Join(dstRoot, e.Name()))
	}
	return nil
}

// writeCodexAgents renders each canonical AGENT.md as a `.toml` under dstRoot
// and prunes stale tomls. ENOENT on the canonical agents bucket is a no-op;
// other errors propagate.
func (c *codex) writeCodexAgents(agentsHome, scope, dstRoot string) error {
	entries, err := listScopedResourceDirs(agentsHome, "agents", scope, codexAgentMDFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	wanted := map[string]bool{}
	for _, entry := range entries {
		wanted[entry.Name+".toml"] = true
		dst := filepath.Join(dstRoot, entry.Name+".toml")
		if err := c.writeCodexAgentToml(dst, entry.File); err != nil {
			return err
		}
	}
	if existing, err := os.ReadDir(dstRoot); err == nil {
		for _, e := range existing {
			if !strings.HasSuffix(e.Name(), ".toml") || wanted[e.Name()] {
				continue
			}
			_ = os.Remove(filepath.Join(dstRoot, e.Name()))
		}
	}
	return nil
}

// pruneManagedCodexAgentTomls removes the per-entry `.toml` files that map to
// canonical AGENT.md entries. ENOENT on the canonical agents bucket is a
// no-op; other errors propagate.
func (c *codex) pruneManagedCodexAgentTomls(agentsHome, scope, dstRoot string) error {
	entries, err := listScopedResourceDirs(agentsHome, "agents", scope, codexAgentMDFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if err := c.io.Remove(filepath.Join(dstRoot, entry.Name+".toml")); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func writeCodexAgentTomlFile(io platformIO, dst, agentMD string) error {
	content, err := renderCodexAgentToml(agentMD)
	if err != nil {
		return err
	}
	if err := io.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	if _, err := os.Lstat(dst); err == nil {
		if err := io.Remove(dst); err != nil {
			return err
		}
	}
	return io.WriteFile(dst, content, 0644)
}

func (c *codex) writeCodexAgentToml(dst, agentMD string) error {
	return writeCodexAgentTomlFile(c.io, dst, agentMD)
}

func renderCodexAgentToml(agentMD string) ([]byte, error) {
	meta := readFrontmatter(agentMD)
	body, err := readAgentBody(agentMD)
	if err != nil {
		return nil, err
	}

	name := strings.TrimSpace(meta["name"])
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(filepath.Dir(agentMD)), string(filepath.Ext(agentMD)))
	}
	description := strings.TrimSpace(meta["description"])
	model := strings.TrimSpace(meta["model"])
	var b strings.Builder
	fmt.Fprintf(&b, "name = %s\n", strconv.Quote(name))
	fmt.Fprintf(&b, "description = %s\n", strconv.Quote(description))
	if model != "" {
		fmt.Fprintf(&b, "model = %s\n", strconv.Quote(model))
	}
	if strings.TrimSpace(body) != "" {
		b.WriteString("developer_instructions = ")
		b.WriteString(tomlMultilineString(body))
		b.WriteString("\n")
	}
	return []byte(b.String()), nil
}

func readAgentBody(agentMD string) (string, error) {
	data, err := os.ReadFile(agentMD)
	if err != nil {
		return "", err
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return text, nil
	}
	rest := strings.TrimPrefix(text, "---\n")
	end := strings.Index(rest, "\n---\n")
	if end == -1 {
		return text, nil
	}
	body := rest[end+len("\n---\n"):]
	body = strings.TrimLeft(body, "\n")
	return body, nil
}

func tomlMultilineString(value string) string {
	escaped := strings.ReplaceAll(value, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"""`, `\"\"\"`)
	return "\"\"\"\n" + escaped + "\n\"\"\""
}

// BrokenLinks implements BrokenLinkReporter for the codex platform.
//
// Codex's project-scope single-file managed surface is AGENTS.md at the repo
// root — a managed symlink to the highest-priority canonical source under
// <agentsHome>/rules/{global,project}/agents.(md|mdc) (see linkCodexAgentsMD).
// The diagnostic contract carried over from doctor's previous inline
// projectSingleFiles table (collectSingleFileBrokenLinks → managedLinkBroken)
// is "report broken only when the entry is a resolvable managed link AND its
// target is missing" — a hard-linked or absent AGENTS.md is silently passed
// over. classifyManagedLink encodes that semantic exactly, matching claude's
// brokenMCPLink shape.
//
// ScanSingleFileLinks is intentionally NOT used here for the same reason
// documented on claude.brokenMCPLink: its hard-link-only canonical contract
// would flag a symlink-only managed AGENTS.md as broken, which would shift
// the existing doctor_test expectations (TestCollectBrokenLinks_BrokenAgentsMD
// is the central pin).
//
// PlatformID is set on the returned BrokenLink so JSON consumers can
// self-describe per-entry (BrokenLink struct contract).
func (c *codex) BrokenLinks(_, repoPath, _ string) []BrokenLink {
	return classifyCodexSingleFile(filepath.Join(repoPath, codexAgentsMarkdown))
}

// classifyCodexSingleFile returns the broken-link record for a single codex
// managed file path, or nil when the path is not a resolvable managed link
// or its target exists. Extracted from BrokenLinks for symmetry with claude's
// brokenMCPLink and to keep the per-platform helper trivial enough that P3's
// CountLinks/Badge migration can reuse the shape without refactor.
func classifyCodexSingleFile(linkPath string) []BrokenLink {
	state, raw := classifyManagedLink(linkPath)
	if state != linkStateBroken {
		return nil
	}
	return []BrokenLink{{
		PlatformID:  "codex",
		LinkPath:    linkPath,
		Dest:        raw,
		DisplayDest: config.DisplayPath(absolutizeDest(linkPath, raw)),
	}}
}

func (c *codex) SharedTargetIntents(project string) ([]ResourceIntent, error) {
	skills, err := BuildSharedSkillMirrorIntents(project, filepath.Join(codexAgentsDir, "skills"))
	if err != nil {
		return nil, err
	}
	tomls, err := BuildSharedCodexAgentTomlIntents(project)
	if err != nil {
		return nil, err
	}
	return append(skills, tomls...), nil
}

// CountLinks implements LinkCounter for the codex platform: returns the
// (ok, broken) tally of managed links under the project's repo. Mirrors the
// per-platform inline counter previously inlined in status.go's
// codexTextBadge.
//
// Healthy: AGENTS.md, .codex/config.toml, .codex/hooks.json (when each is a
// resolvable managed link with a reachable target, or any present file —
// matching the historical addManagedCounts contract), plus any resolvable
// entry under .codex/agents/ or .agents/skills/. Broken: a resolvable
// managed link whose target is missing.
func (c *codex) CountLinks(_, repoPath, _ string) (ok, broken int) {
	addManagedFileCounts(&ok, &broken, []string{
		filepath.Join(repoPath, codexAgentsMarkdown),
		filepath.Join(repoPath, codexDir, codexConfigTOML),
		filepath.Join(repoPath, codexDir, codexHooksJSON),
	})
	addManagedDirCounts(&ok, &broken, []string{
		filepath.Join(repoPath, codexDir, "agents"),
		filepath.Join(repoPath, codexAgentsDir, "skills"),
	})
	return ok, broken
}

// Badge implements StatusBadger for the codex platform.
func (c *codex) Badge(project, repoPath, agentsHome string) PlatformBadge {
	ok, broken := c.CountLinks(project, repoPath, agentsHome)
	return PlatformBadge{Name: "Codex", Present: ok > 0, Broken: broken > 0}
}
