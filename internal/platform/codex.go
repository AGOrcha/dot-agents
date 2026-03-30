package platform

import (
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

const codexHooksJSON = "hooks.json"

func NewCodex() Platform { return &codex{} }

func (c *codex) ID() string          { return "codex" }
func (c *codex) DisplayName() string { return "Codex CLI" }

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
			links.Symlink(src, filepath.Join(repoPath, "AGENTS.md"))
			break
		}
	}
	// Project override
	for _, name := range []string{"agents.md", "agents.mdc"} {
		src := filepath.Join(agentsHome, "rules", project, name)
		if _, err := os.Stat(src); err == nil {
			links.Symlink(src, filepath.Join(repoPath, "AGENTS.md"))
			break
		}
	}

	// .codex/config.toml
	if err := os.MkdirAll(filepath.Join(repoPath, ".codex"), 0755); err != nil {
		return err
	}
	if src := resolveScopedFile(agentsHome, "settings", project, "codex.toml"); src != "" {
		links.Symlink(src, filepath.Join(repoPath, ".codex", "config.toml"))
	}

	// Project agents → .codex/agents/*.toml
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
		userAgentsDir := filepath.Join(homeRoot, ".codex", "agents")
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
		userSkillsDir := filepath.Join(homeRoot, ".agents", "skills")
		if err := syncScopedDirSymlinks(agentsHome, "skills", "global", "SKILL.md", userSkillsDir); err != nil {
			return err
		}
	}
	return nil
}

func (c *codex) createAgentsLinks(project, repoPath, agentsHome string) error {
	agentsTarget := filepath.Join(repoPath, ".codex", "agents")
	if err := os.MkdirAll(agentsTarget, 0755); err != nil {
		return err
	}
	return c.writeCodexAgents(agentsHome, project, agentsTarget)
}

func (c *codex) createSkillsLinks(project, repoPath, agentsHome string) error {
	return syncScopedDirSymlinksTargets(agentsHome, "skills", project, "SKILL.md", filepath.Join(repoPath, ".agents", "skills"))
}

func (c *codex) createHooksLinks(project, repoPath, agentsHome string) error {
	if err := c.writeRepoHooks(project, repoPath, agentsHome); err != nil {
		return err
	}
	return c.writeUserHomeHooks(project, agentsHome)
}

func (c *codex) writeRepoHooks(project, repoPath, agentsHome string) error {
	repoTarget := filepath.Join(repoPath, ".codex", codexHooksJSON)
	repoBundles, err := collectCanonicalHookSpecsForPlatform(agentsHome, project, c.ID(), "global", project)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(repoPath, ".codex"), 0755); err != nil {
		return err
	}
	return emitPreferredHookFile(
		repoTarget,
		renderCodexHookConfig,
		resolveHookSpec(agentsHome, []string{"hooks"}, project, "codex.json", "codex-hooks.json"),
		HookEmissionMode{Shape: HookShapeDirect, Transport: HookTransportSymlink},
		func(path string) error { return removeManagedFileIf(path, isLikelyRenderedCodexHookConfig) },
		repoBundles,
	)
}

func (c *codex) writeUserHomeHooks(project, agentsHome string) error {
	globalBundles, err := collectCanonicalHookSpecsForPlatform(agentsHome, project, c.ID(), "global")
	if err != nil {
		return err
	}
	return emitPreferredHookFileToUserHomes(
		filepath.Join(".codex", codexHooksJSON),
		renderCodexHookConfig,
		resolveHookSpec(agentsHome, []string{"hooks"}, project, "codex.json", "codex-hooks.json"),
		HookEmissionMode{Shape: HookShapeDirect, Transport: HookTransportSymlink},
		func(path string) error { return removeManagedFileIf(path, isLikelyRenderedCodexHookConfig) },
		globalBundles,
	)
}

func (c *codex) RemoveLinks(project, repoPath string) error {
	agentsHome := config.AgentsHome()

	links.RemoveIfSymlinkUnder(filepath.Join(repoPath, "AGENTS.md"), agentsHome)
	links.RemoveIfSymlinkUnder(filepath.Join(repoPath, ".codex", "config.toml"), agentsHome)
	repoBundles, err := collectCanonicalHookSpecsForPlatform(agentsHome, project, c.ID(), "global", project)
	if err == nil && len(repoBundles) > 0 {
		_ = removeManagedRenderedHookFile(repoBundles, filepath.Join(repoPath, ".codex", codexHooksJSON), renderCodexHookConfig)
	}
	links.RemoveIfSymlinkUnder(filepath.Join(repoPath, ".codex", codexHooksJSON), agentsHome)

	_ = c.pruneManagedCodexAgentTomls(agentsHome, project, filepath.Join(repoPath, ".codex", "agents"))

	skillsDir := filepath.Join(repoPath, ".agents", "skills")
	if entries, err := os.ReadDir(skillsDir); err == nil {
		for _, e := range entries {
			links.RemoveIfSymlinkUnder(filepath.Join(skillsDir, e.Name()), agentsHome)
		}
	}

	return nil
}

func (c *codex) writeCodexAgents(agentsHome, scope, dstRoot string) error {
	entries, err := listScopedResourceDirs(agentsHome, "agents", scope, "AGENT.md")
	if err != nil {
		return nil
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

func (c *codex) pruneManagedCodexAgentTomls(agentsHome, scope, dstRoot string) error {
	entries, err := listScopedResourceDirs(agentsHome, "agents", scope, "AGENT.md")
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		if err := os.Remove(filepath.Join(dstRoot, entry.Name+".toml")); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (c *codex) writeCodexAgentToml(dst, agentMD string) error {
	content, err := renderCodexAgentToml(agentMD)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	if _, err := os.Lstat(dst); err == nil {
		if err := os.Remove(dst); err != nil {
			return err
		}
	}
	return os.WriteFile(dst, content, 0644)
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
	background := strings.TrimSpace(meta["is_background"])

	var b strings.Builder
	fmt.Fprintf(&b, "name = %s\n", strconv.Quote(name))
	fmt.Fprintf(&b, "description = %s\n", strconv.Quote(description))
	if model != "" {
		fmt.Fprintf(&b, "model = %s\n", strconv.Quote(model))
	}
	if background != "" {
		fmt.Fprintf(&b, "is_background = %s\n", background)
	}
	if strings.TrimSpace(body) != "" {
		b.WriteString("instructions = ")
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
