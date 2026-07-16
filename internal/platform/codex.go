package platform

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/fsops"
	"github.com/AGOrcha/dot-agents/internal/links"
	"github.com/AGOrcha/dot-agents/internal/ui"
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
	if err := linkFirstResolvedAgentsCandidate(globalCandidates, dst); err != nil {
		return err
	}
	// Project override: symlink-replaces the global link above when present.
	projectCandidates := []string{
		filepath.Join(agentsHome, "rules", project, "agents.md"),
		filepath.Join(agentsHome, "rules", project, "agents.mdc"),
	}
	return linkFirstResolvedAgentsCandidate(projectCandidates, dst)
}

// linkFirstResolvedAgentsCandidate symlink-replaces dst with the first
// candidate that resolves to a real file, skipping a legitimately absent
// candidate (fsops.StatAllowMissing found=false, err=nil) and continuing the
// search. A real Stat error (permission denied, I/O failure, ...) aborts the
// search immediately and propagates — it must never be treated as "this
// candidate doesn't exist" and silently skipped. Managed-replace: a stale
// managed symlink is idempotently re-pointed; a genuine user-authored
// AGENTS.md is preserved as AGENTS.md.dot-agents-backup, never silently
// destroyed.
func linkFirstResolvedAgentsCandidate(candidates []string, dst string) error {
	for _, src := range candidates {
		_, found, err := fsops.StatAllowMissing(src)
		if err != nil {
			return err
		}
		if found {
			return links.SymlinkReplacing(src, dst, backupSidecar)
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
	_, found, err := fsops.StatAllowMissing(globalAgents)
	if err != nil {
		return err
	}
	if !found {
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
	// TOML files (local-authored AND sourced, t2b) are materialized by
	// ProjectResolvedUnits/CollectAndExecuteSharedTargetPlan; prune stale
	// `.toml` when their canonical AGENT.md (local or locked-sourced) is gone.
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
	spec, err := resolveHookSpec(agentsHome, []string{"hooks"}, project, "codex.json", "codex-hooks.json")
	if err != nil {
		return err
	}
	return emitPreferredHookFile(
		c.io,
		repoTarget,
		renderCodexHookConfig,
		spec,
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
	spec, err := resolveHookSpec(agentsHome, []string{"hooks"}, project, "codex.json", "codex-hooks.json")
	if err != nil {
		return err
	}
	return emitPreferredHookFileToUserHomes(
		c.io,
		filepath.Join(codexDir, codexHooksJSON),
		renderCodexHookConfig,
		spec,
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

	errs = append(errs, c.pruneManagedCodexAgentTomls(agentsHome, project, repoPath, filepath.Join(repoPath, codexDir, "agents")))

	skillsDir := filepath.Join(repoPath, codexAgentsDir, "skills")
	if entries, err := os.ReadDir(skillsDir); err == nil {
		for _, e := range entries {
			errs = append(errs, links.RemoveIfSymlinkUnder(filepath.Join(skillsDir, e.Name()), agentsHome))
		}
	}

	return errors.Join(errs...)
}

// sourcedCodexAgentNames returns the names of currently-locked kind:artifact
// "agents"-family packages units for the project at repoPath (t2b): the
// generic exact/prune scan in resource_plan.go only reaps managed SYMLINKS
// (links.IsManagedLinkUnder), so a Codex `.toml` — a rendered regular file,
// never a symlink — is invisible to it. pruneCodexRepoAgentTomls /
// pruneManagedCodexAgentTomls are the codex-specific prune complement for the
// render shape (mirroring writeCodexAgents' local-only wanted-set pattern),
// and need to know which sourced names to treat as "still wanted" without a
// units parameter — codex's Platform interface carries none — so this reads
// the project's own lock directly. A missing/unreadable lock or a ref this
// process cannot parse yields an empty set (never an error): pruning then
// falls back to exactly the pre-t2b local-canonical-only behavior.
func sourcedCodexAgentNames(repoPath string) map[string]bool {
	lock, err := config.ReadUnits(repoPath)
	if err != nil {
		return nil
	}
	names := map[string]bool{}
	for ref, u := range lock.Units {
		if u.Kind != config.UnitKindArtifact {
			continue
		}
		parts, err := config.ParsePackageRef(ref)
		if err != nil {
			continue
		}
		i := strings.IndexByte(parts.ArtifactPath, '/')
		if i <= 0 || parts.ArtifactPath[:i] != "agent" {
			continue
		}
		names[parts.ArtifactPath[i+1:]] = true
	}
	return names
}

// pruneCodexRepoAgentTomls deletes stale `.codex/agents/*.toml` files in the
// repo whose canonical AGENT.md no longer exists — checked against BOTH the
// local canonical agents bucket AND the project's locked sourced agent names
// (t2b, sourcedCodexAgentNames), so a packages-resolved agent's rendered
// toml survives this platform's own CreateLinks-time prune pass instead of
// being deleted the instant after ProjectResolvedUnits renders it. A missing
// local agents bucket is no longer a short-circuit no-op by itself — sourced
// names must still be honored — so only a genuinely unreadable dst dir ends
// the pass early.
func pruneCodexRepoAgentTomls(project, repoPath, agentsHome string) error {
	entries, err := listScopedResourceDirs(agentsHome, "agents", project, codexAgentMDFile)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	wanted := map[string]bool{}
	for _, entry := range entries {
		wanted[entry.Name+".toml"] = true
	}
	for name := range sourcedCodexAgentNames(repoPath) {
		wanted[name+".toml"] = true
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
// canonical AGENT.md entries — local canonical entries under scope AND the
// project's locked sourced agent names (t2b, sourcedCodexAgentNames), so a
// full RemoveLinks teardown also tears down a packages-resolved agent's
// rendered toml, not only local-authored ones. ENOENT on the canonical
// agents bucket is no longer a short-circuit no-op by itself (sourced names
// must still be honored); removal errors are aggregated rather than
// short-circuited so one stuck file cannot hide the rest.
func (c *codex) pruneManagedCodexAgentTomls(agentsHome, scope, repoPath, dstRoot string) error {
	entries, err := listScopedResourceDirs(agentsHome, "agents", scope, codexAgentMDFile)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	names := map[string]bool{}
	for _, entry := range entries {
		names[entry.Name] = true
	}
	for name := range sourcedCodexAgentNames(repoPath) {
		names[name] = true
	}
	var errs []error
	for name := range names {
		if err := c.io.Remove(filepath.Join(dstRoot, name+".toml")); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
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

// PrintAudit implements AuditPrinter for the codex platform: it renders the
// AGENTS.md link, the .codex/config.toml + .codex/hooks.json links, the
// shared .agents/skills/ mirror, and the native .codex/agents/ TOML entries.
// Moved verbatim (output preserved) from the lifecycle-side printCodexAudit
// in Phase 5.
func (c *codex) PrintAudit(w io.Writer, _, repoPath, _ string) {
	fmt.Fprintf(w, "    %sCodex%s\n", ui.Cyan, ui.Reset)
	codexPrintAgentsMD(w, filepath.Join(repoPath, codexAgentsMarkdown))
	codexPrintSymlinkAudit(w, filepath.Join(repoPath, codexDir, codexConfigTOML), ".codex/config.toml")
	codexPrintSymlinkAudit(w, filepath.Join(repoPath, codexDir, codexHooksJSON), ".codex/hooks.json")
	codexPrintSkillsAudit(w, filepath.Join(repoPath, codexAgentsDir, "skills"))
	codexPrintAgentsAudit(w, filepath.Join(repoPath, codexDir, "agents"))
	fmt.Fprintln(w)
}

// codexPrintAgentsMD renders the AGENTS.md link/local-file/absent status to w.
func codexPrintAgentsMD(w io.Writer, path string) {
	if _, err := os.Lstat(path); err == nil {
		if state, _ := classifyManagedLink(path); state != linkStateNotALink {
			printLinkedStatusLine(w, codexAgentsMarkdown, path)
			return
		}
		fmt.Fprintf(w, auditLocalFileIndentedFmt, ui.Dim, ui.Reset, codexAgentsMarkdown, ui.Dim, ui.Reset)
		return
	}
	fmt.Fprintf(w, "      %s(no %s)%s\n", ui.Dim, codexAgentsMarkdown, ui.Reset)
}

// codexPrintSymlinkAudit renders a single codex managed-file link to w. A
// present-but-not-a-link path is a rendered/managed file on disk (e.g.
// .codex/hooks.json, .codex/config.toml), not an absent link; "(not linked)"
// is reserved for a truly absent path.
func codexPrintSymlinkAudit(w io.Writer, path, label string) {
	if state, _ := classifyManagedLink(path); state != linkStateNotALink {
		printLinkedStatusLine(w, label, path)
		return
	}
	if _, err := os.Lstat(path); err == nil {
		fmt.Fprintf(w, auditLocalFileIndentedFmt, ui.Dim, ui.Reset, label, ui.Dim, ui.Reset)
		return
	}
	fmt.Fprintf(w, "      %s-%s %s %s(not linked)%s\n", ui.Dim, ui.Reset, label, ui.Dim, ui.Reset)
}

// codexPrintSkillsAudit renders the shared .agents/skills/ mirror entries to w.
func codexPrintSkillsAudit(w io.Writer, dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	okCount, brokenCount := 0, 0
	for _, entry := range entries {
		linkPath := filepath.Join(dir, entry.Name())
		if state, _ := classifyManagedLink(linkPath); state == linkStateNotALink {
			continue
		}
		if printLinkedStatusLine(w, ".agents/skills/"+entry.Name(), linkPath) {
			okCount++
		} else {
			brokenCount++
		}
	}
	if okCount == 0 && brokenCount == 0 {
		fmt.Fprintf(w, "      %s○%s .agents/skills/ %s(empty)%s\n", ui.Dim, ui.Reset, ui.Dim, ui.Reset)
	}
}

// codexPrintAgentsAudit renders the native .codex/agents/ TOML entries to w.
func codexPrintAgentsAudit(w io.Writer, dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	okCount, brokenCount := 0, 0
	for _, entry := range entries {
		linkPath := filepath.Join(dir, entry.Name())
		if _, err := os.Stat(linkPath); err == nil {
			fmt.Fprintf(w, "      %s✓%s .codex/agents/%s %s(native TOML)%s\n", ui.Green, ui.Reset, entry.Name(), ui.Dim, ui.Reset)
			okCount++
		} else {
			fmt.Fprintf(w, "      %s✗%s .codex/agents/%s %s(unreadable)%s\n", ui.Red, ui.Reset, entry.Name(), ui.Dim, ui.Reset)
			brokenCount++
		}
	}
	if okCount == 0 && brokenCount == 0 {
		fmt.Fprintf(w, "      %s○%s .codex/agents/ %s(empty)%s\n", ui.Dim, ui.Reset, ui.Dim, ui.Reset)
	}
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

// DirMirrorRoots implements DirMirrorRootsProvider (resource_plan.go):
// codex's skills bucket is dir-mirror shaped; its agents bucket is
// FILE-shaped (rendered `.toml`, see SourcedAgentFileIntents) and is
// deliberately absent here.
func (c *codex) DirMirrorRoots() map[string][]string {
	return map[string][]string{"skills": {filepath.Join(codexAgentsDir, "skills")}}
}

// SourcedAgentFileIntents implements SourcedAgentFileProjector
// (resource_plan.go, t2b): codex's agents bucket renders a `.toml` per
// entry rather than symlinking a whole dir, so a sourced "agents"-family
// unit gets a RenderSingle/Write intent — NOT the casDirectOrigin marker
// buildCASAgentFileIntents uses, since that marker routes execution
// straight to the symlink swap in executeResourceIntent, before the
// shape/transport switch that would otherwise dispatch to
// executeRenderSingleWrite. The intent's SourceRef addresses the unit's
// CAS AGENT.md directly (mirroring buildCASIntents' CAS addressing), so
// executeRenderSingleWrite reads and renders it exactly like a local
// canonical AGENT.md. Pruning this shape on removal is NOT handled by the
// generic exact/prune scan (it only reaps managed symlinks) — see
// pruneCodexRepoAgentTomls/sourcedCodexAgentNames.
func (c *codex) SourcedAgentFileIntents(project string, units []ResolvedUnit) []ResourceIntent {
	intents := make([]ResourceIntent, 0, len(units))
	for _, u := range units {
		if u.Family != "agents" {
			continue
		}
		intents = append(intents, ResourceIntent{
			IntentID:    fmt.Sprintf("agents.sourced.codex-toml.%s.%s", sanitizeIntentRoot(u.SourceID), u.Name),
			Project:     project,
			Bucket:      "agents",
			LogicalName: u.Name,
			TargetPath:  filepath.Join(codexDir, "agents", u.Name+".toml"),
			Ownership:   ResourceOwnershipSharedRepo,
			SourceRef: ResourceSourceRef{
				Scope:        "artifacts",
				Bucket:       "cache",
				RelativePath: filepath.Join(u.Family, config.StoreDigestDir(u.Digest), agentManifestName),
				Kind:         ResourceSourceCanonicalFile,
				Origin:       "sourced-codex-agent-toml",
			},
			Shape:         ResourceShapeRenderSingle,
			Transport:     ResourceTransportWrite,
			Materializer:  codexAgentTomlMaterializer,
			ReplacePolicy: ResourceReplaceIfManaged,
			PrunePolicy:   ResourcePruneNone,
		})
	}
	return intents
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

// codexOrphanBucket is the single canonical bucket codex owns for orphan
// reporting. claude owns "skills", codex owns "agents" — disjoint buckets so
// the doctor-side iterator never double-counts a canonical entry.
const codexOrphanBucket = "agents"

// OrphanCanonicals implements OrphanCanonicalReporter for the codex platform.
//
// codex owns the "agents" canonical bucket: entries under
// <agentsHome>/agents/<project>/ with no live back-link at
// <projectPath>/.agents/agents/<name>. A non-matching bucket returns nil so
// the (reporter, bucket) fan-out in doctor stays double-count free (claude
// owns "skills"). Detection is shared with claude via scanOrphanCanonicals.
func (c *codex) OrphanCanonicals(project, projectPath, agentsHome, bucket string) []OrphanCanonical {
	if bucket != codexOrphanBucket {
		return nil
	}
	return scanOrphanCanonicals(project, projectPath, agentsHome, bucket)
}

// codexUserBrokenDirs returns the managed directories codex scans for broken
// user-home links: ~/.codex/agents/. Mirrors the legacy lifecycle
// collectBrokenUserLinks codex block (a single managed agents dir).
func codexUserBrokenDirs(home string) []string {
	return []string{filepath.Join(home, codexDir, "agents")}
}

// codexUserConfigFiles returns the managed single-file user-config references
// codex maintains: ~/.codex/hooks.json. Used by the badge math (the
// broken-link surface is narrower — see codexUserBrokenDirs).
func codexUserConfigFiles(home string) []string {
	return []string{filepath.Join(home, codexDir, codexHooksJSON)}
}

// codexUserConfigDirs returns the managed directories codex counts for its
// user-config badge: ~/.codex/agents/ and ~/.agents/skills/. Mirrors the
// legacy lifecycle collectUserConfigPlatforms codex block.
func codexUserConfigDirs(home string) []string {
	return []string{
		filepath.Join(home, codexDir, "agents"),
		filepath.Join(home, codexAgentsDir, "skills"),
	}
}

// UserBrokenLinks implements UserConfigReporter for the codex platform. The
// broken-link surface is ~/.codex/agents/* (matching the legacy lifecycle
// collectBrokenUserLinks codex block); every entry carries PlatformID="codex".
func (c *codex) UserBrokenLinks(home string) []BrokenLink {
	return scanUserBrokenLinks("codex", nil, codexUserBrokenDirs(home))
}

// UserBadge implements UserConfigReporter for the codex platform: the
// user-config badge over ~/.codex/hooks.json, ~/.codex/agents/, and
// ~/.agents/skills/. Mirrors the legacy lifecycle countPlatformHealth("Codex",
// ...) badge math.
func (c *codex) UserBadge(home string) PlatformBadge {
	ok, broken := scanUserConfigCounts(codexUserConfigFiles(home), codexUserConfigDirs(home))
	return PlatformBadge{Name: "Codex", Present: ok > 0, Broken: broken > 0}
}
