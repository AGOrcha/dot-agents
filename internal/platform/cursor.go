package platform

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/links"
)

type cursor struct{}

const cursorHooksFile = "hooks.json"

func NewCursor() Platform { return &cursor{} }

func (c *cursor) ID() string          { return "cursor" }
func (c *cursor) DisplayName() string { return "Cursor" }

func (c *cursor) IsInstalled() bool {
	if _, err := os.Stat("/Applications/Cursor.app"); err == nil {
		return true
	}
	_, err := exec.LookPath("cursor")
	return err == nil
}

func (c *cursor) Version() string {
	// Try app version on macOS
	if _, err := os.Stat("/Applications/Cursor.app"); err == nil {
		out, err := exec.Command("defaults", "read",
			"/Applications/Cursor.app/Contents/Info.plist",
			"CFBundleShortVersionString").Output()
		if err == nil {
			appVer := strings.TrimSpace(string(out))
			if path, err := exec.LookPath("cursor"); err == nil {
				cliOut, err := exec.Command(path, "--version").Output()
				if err == nil {
					cliVer := strings.TrimSpace(strings.Split(string(cliOut), "\n")[0])
					return appVer + " (CLI: " + cliVer + ")"
				}
			}
			return appVer + " (App)"
		}
	}
	if path, err := exec.LookPath("cursor"); err == nil {
		out, err := exec.Command(path, "--version").Output()
		if err == nil {
			return strings.TrimSpace(strings.Split(string(out), "\n")[0])
		}
	}
	return ""
}

func (c *cursor) HasDeprecatedFormat(repoPath string) bool {
	_, err := os.Stat(filepath.Join(repoPath, ".cursorrules"))
	return err == nil
}

func (c *cursor) DeprecatedDetails(repoPath string) string {
	if c.HasDeprecatedFormat(repoPath) {
		return ".cursorrules → .cursor/rules/*.mdc"
	}
	return ""
}

func (c *cursor) CreateLinks(project, repoPath string) error {
	agentsHome := config.AgentsHome()

	if err := c.createRuleLinks(project, repoPath, agentsHome); err != nil {
		return err
	}
	if err := c.createSettingsLinks(project, repoPath, agentsHome); err != nil {
		return err
	}
	if err := c.createMCPLinks(project, repoPath, agentsHome); err != nil {
		return err
	}
	if err := c.createIgnoreLink(project, repoPath, agentsHome); err != nil {
		return err
	}
	if err := c.createAgentsLinks(project, repoPath, agentsHome); err != nil {
		return err
	}
	if err := c.createHooksLinks(project, repoPath, agentsHome); err != nil {
		return err
	}
	return nil
}

func (c *cursor) createRuleLinks(project, repoPath, agentsHome string) error {
	rulesDir := filepath.Join(repoPath, ".cursor", "rules")
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		return err
	}

	// Global rules → global--{name}.mdc
	globalRulesDir := filepath.Join(agentsHome, "rules", "global")
	if entries, err := os.ReadDir(globalRulesDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if !strings.HasSuffix(name, ".mdc") && !strings.HasSuffix(name, ".md") {
				continue
			}
			targetName := toMDC(name)
			src := filepath.Join(globalRulesDir, name)
			dst := filepath.Join(rulesDir, "global--"+targetName)
			links.Hardlink(src, dst) // best-effort
		}
	}

	// Project rules → {project}--{name}.mdc
	projectRulesDir := filepath.Join(agentsHome, "rules", project)
	if entries, err := os.ReadDir(projectRulesDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if !strings.HasSuffix(name, ".mdc") && !strings.HasSuffix(name, ".md") {
				continue
			}
			targetName := toMDC(name)
			src := filepath.Join(projectRulesDir, name)
			dst := filepath.Join(rulesDir, project+"--"+targetName)
			links.Hardlink(src, dst) // best-effort
		}
	}
	return nil
}

func (c *cursor) createSettingsLinks(project, repoPath, agentsHome string) error {
	if err := os.MkdirAll(filepath.Join(repoPath, ".cursor"), 0755); err != nil {
		return err
	}
	if src := resolveScopedFile(agentsHome, "settings", project, "cursor.json"); src != "" {
		dst := filepath.Join(repoPath, ".cursor", "settings.json")
		links.Hardlink(src, dst) // best-effort
	}
	return nil
}

func (c *cursor) createMCPLinks(project, repoPath, agentsHome string) error {
	if err := os.MkdirAll(filepath.Join(repoPath, ".cursor"), 0755); err != nil {
		return err
	}
	if src := resolveScopedFile(agentsHome, "mcp", project, "cursor.json", "mcp.json"); src != "" {
		dst := filepath.Join(repoPath, ".cursor", "mcp.json")
		links.Hardlink(src, dst) // best-effort
	}
	return nil
}

func (c *cursor) createIgnoreLink(project, repoPath, agentsHome string) error {	
	if src := resolveScopedFile(agentsHome, "settings", project, "cursorignore"); src != "" {
		dst := filepath.Join(repoPath, ".cursorignore")
		links.Hardlink(src, dst) // best-effort
	}
	return nil
}

func (c *cursor) createHooksLinks(project, repoPath, agentsHome string) error {
	if err := os.MkdirAll(filepath.Join(repoPath, ".cursor"), 0755); err != nil {
		return err
	}
	src := resolveScopedFile(agentsHome, "hooks", project, "cursor.json")
	if src != "" {
		links.Hardlink(src, filepath.Join(repoPath, ".cursor", cursorHooksFile))
	}
	// User-level: global scope only
	src = filepath.Join(agentsHome, "hooks", "global", "cursor.json")
	if _, err := os.Stat(src); err == nil {
		for _, homeRoot := range config.UserHomeRoots() {
			cursorDir := filepath.Join(homeRoot, ".cursor")
			if err := os.MkdirAll(cursorDir, 0755); err != nil {
				continue
			}
			dst := filepath.Join(cursorDir, cursorHooksFile)
			if already, _ := links.AreHardlinked(src, dst); already {
				continue
			}
			links.Hardlink(src, dst)
		}
	}
	return nil
}

func (c *cursor) createAgentsLinks(project, repoPath, agentsHome string) error {
	agentsTarget := filepath.Join(repoPath, ".claude", "agents")
	if err := os.MkdirAll(agentsTarget, 0755); err != nil {
		return err
	}

	projectAgents := filepath.Join(agentsHome, "agents", project)
	entries, err := os.ReadDir(projectAgents)
	if err != nil {
		return nil // no agents, fine
	}

	for _, e := range entries {
		agentDir := filepath.Join(projectAgents, e.Name())
		if !links.IsDirEntry(agentDir) {
			continue
		}
		if _, err := os.Stat(filepath.Join(agentDir, "AGENT.md")); err != nil {
			continue
		}
		target := filepath.Join(agentsTarget, e.Name())
		if _, err := os.Lstat(target); err == nil {
			continue // already exists
		}
		links.Symlink(agentDir, target) // best-effort
	}
	return nil
}

func (c *cursor) RemoveLinks(project, repoPath string) error {
	agentsHome := config.AgentsHome()
	rulesDir := filepath.Join(repoPath, ".cursor", "rules")

	if entries, err := os.ReadDir(rulesDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			filePath := filepath.Join(rulesDir, name)

			if strings.HasPrefix(name, "global--") {
				srcName := strings.TrimPrefix(name, "global--")
				// Try both .mdc and .md source
				for _, ext := range []string{srcName, strings.TrimSuffix(srcName, ".mdc") + ".md"} {
					src := filepath.Join(agentsHome, "rules", "global", ext)
					if linked, _ := links.AreHardlinked(filePath, src); linked {
						os.Remove(filePath)
						break
					}
				}
			} else if strings.HasPrefix(name, project+"--") {
				srcName := strings.TrimPrefix(name, project+"--")
				for _, ext := range []string{srcName, strings.TrimSuffix(srcName, ".mdc") + ".md"} {
					src := filepath.Join(agentsHome, "rules", project, ext)
					if linked, _ := links.AreHardlinked(filePath, src); linked {
						os.Remove(filePath)
						break
					}
				}
			}
		}
	}

	// Remove .cursor/hooks.json if hard-linked to our source
	hooksFilePath := filepath.Join(repoPath, ".cursor", cursorHooksFile)
	for _, scope := range []string{project, "global"} {
		src := filepath.Join(agentsHome, "hooks", scope, "cursor.json")
		if linked, _ := links.AreHardlinked(hooksFilePath, src); linked {
			os.Remove(hooksFilePath)
			break
		}
	}

	// Remove agent symlinks
	agentsTarget := filepath.Join(repoPath, ".claude", "agents")
	if entries, err := os.ReadDir(agentsTarget); err == nil {
		for _, e := range entries {
			linkPath := filepath.Join(agentsTarget, e.Name())
			links.RemoveIfSymlinkUnder(linkPath, agentsHome)
		}
	}

	return nil
}

// toMDC converts .md extension to .mdc; leaves .mdc unchanged.
func toMDC(name string) string {
	if strings.HasSuffix(name, ".md") && !strings.HasSuffix(name, ".mdc") {
		return strings.TrimSuffix(name, ".md") + ".mdc"
	}
	return name
}
