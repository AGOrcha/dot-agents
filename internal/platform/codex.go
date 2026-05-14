package platform

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/links"
)

type codex struct{}

const (
	codexAgentsDir      = ".agents"
	codexDir            = ".codex"
	codexHooksJSON      = "hooks.json"
	codexAgentsMarkdown = "AGENTS.md"
	codexAgentMDFile    = "AGENT.md"
)

func NewCodex() Platform { return &codex{} }

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
	_, err := exec.LookPath("codex")
	return err == nil
}

func (c *codex) Version() string {
	out, err := exec.Command("codex", "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.Split(string(out), "\n")[0])
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
	globalCandidates := []string{
		filepath.Join(agentsHome, "rules", "global", "agents.md"),
		filepath.Join(agentsHome, "rules", "global", "agents.mdc"),
		filepath.Join(agentsHome, "rules", "global", "rules.md"),
		filepath.Join(agentsHome, "rules", "global", "rules.mdc"),
	}
	for _, src := range globalCandidates {
		if _, err := os.Stat(src); err == nil {
			links.Symlink(src, filepath.Join(repoPath, codexAgentsMarkdown))
			break
		}
	}
	// Project override
	for _, name := range []string{"agents.md", "agents.mdc"} {
		src := filepath.Join(agentsHome, "rules", project, name)
		if _, err := os.Stat(src); err == nil {
			links.Symlink(src, filepath.Join(repoPath, codexAgentsMarkdown))
			break
		}
	}

	// .codex/config.toml
	if err := osMkdirAll(filepath.Join(repoPath, codexDir), 0755); err != nil {
		return err
	}
	if src := resolveScopedFile(agentsHome, "settings", project, "codex.toml"); src != "" {
		links.Symlink(src, filepath.Join(repoPath, codexDir, "config.toml"))
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

func (c *codex) ensureUserAgents(agentsHome string) error {
	globalAgents := filepath.Join(agentsHome, "agents", "global")
	if _, err := os.Stat(globalAgents); err != nil {
		return nil
	}
	for _, homeRoot := range config.UserHomeRoots() {
		userAgentsDir := filepath.Join(homeRoot, codexDir, "agents")
		if err := os.MkdirAll(userAgentsDir, 0755); err != nil {
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
		if err := syncScopedDirSymlinks(agentsHome, "skills", "global", "SKILL.md", userSkillsDir); err != nil {
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
	if err := osMkdirAll(filepath.Join(repoPath, codexDir), 0755); err != nil {
		return err
	}
	return emitPreferredHookFile(
		repoTarget,
		renderCodexHookConfig,
		resolveHookSpec(agentsHome, []string{"hooks"}, project, "codex.json", "codex-hooks.json"),
		directSymlinkHookMode,
		removeRenderedCodexHookConfig,
		repoBundles,
	)
}

func (c *codex) writeUserHomeHooks(project, agentsHome string) error {
	globalBundles, err := collectCanonicalHookSpecsForPlatform(agentsHome, project, c.ID(), "global")
	if err != nil {
		return err
	}
	return emitPreferredHookFileToUserHomes(
		filepath.Join(codexDir, codexHooksJSON),
		renderCodexHookConfig,
		resolveHookSpec(agentsHome, []string{"hooks"}, project, "codex.json", "codex-hooks.json"),
		directSymlinkHookMode,
		removeRenderedCodexHookConfig,
		globalBundles,
	)
}

func (c *codex) RemoveLinks(project, repoPath string) error {
	agentsHome := config.AgentsHome()

	links.RemoveIfSymlinkUnder(filepath.Join(repoPath, codexAgentsMarkdown), agentsHome)
	links.RemoveIfSymlinkUnder(filepath.Join(repoPath, codexDir, "config.toml"), agentsHome)
	repoBundles, err := collectCanonicalHookSpecsForPlatform(agentsHome, project, c.ID(), "global", project)
	if err == nil && len(repoBundles) > 0 {
		_ = removeManagedRenderedHookFile(repoBundles, filepath.Join(repoPath, codexDir, codexHooksJSON), renderCodexHookConfig)
	}
	links.RemoveIfSymlinkUnder(filepath.Join(repoPath, codexDir, codexHooksJSON), agentsHome)

	_ = c.pruneManagedCodexAgentTomls(agentsHome, project, filepath.Join(repoPath, codexDir, "agents"))

	skillsDir := filepath.Join(repoPath, codexAgentsDir, "skills")
	if entries, err := os.ReadDir(skillsDir); err == nil {
		for _, e := range entries {
			links.RemoveIfSymlinkUnder(filepath.Join(skillsDir, e.Name()), agentsHome)
		}
	}

	return nil
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
		if err := os.Remove(filepath.Join(dstRoot, entry.Name+".toml")); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func writeCodexAgentTomlFile(dst, agentMD string) error {
	content, err := renderCodexAgentToml(agentMD)
	if err != nil {
		return err
	}
	if err := osMkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	if _, err := os.Lstat(dst); err == nil {
		if err := osRemove(dst); err != nil {
			return err
		}
	}
	return osWriteFile(dst, content, 0644)
}

func (c *codex) writeCodexAgentToml(dst, agentMD string) error {
	return writeCodexAgentTomlFile(dst, agentMD)
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
