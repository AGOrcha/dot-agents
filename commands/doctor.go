package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dot-agents/dot-agents/internal/config"
	"github.com/dot-agents/dot-agents/internal/links"
	"github.com/dot-agents/dot-agents/internal/platform"
	"github.com/dot-agents/dot-agents/internal/ui"
	"github.com/spf13/cobra"
)

func NewDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check installations, validate links, detect issues",
		RunE:  runDoctor,
	}
}

func runDoctor(cmd *cobra.Command, args []string) error {
	ui.Header("dot-agents doctor")

	agentsHome := config.AgentsHome()

	// Check ~/.agents/
	ui.Section("Installation")
	if _, err := os.Stat(agentsHome); err == nil {
		ui.Bullet("ok", "~/.agents/ exists")
	} else {
		ui.Bullet("error", "~/.agents/ not found — run: dot-agents init")
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
	cfg, err := config.Load()
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
				printAudit(name, path, agentsHome, "")
			}
			continue
		}
		if len(brokenLinks) == 0 {
			ui.Bullet("ok", fmt.Sprintf("%s — %d links healthy", name, ok))
			if Flags.Verbose {
				printAudit(name, path, agentsHome, "")
			}
			continue
		}

		anyBroken = true
		ui.Bullet("warn", fmt.Sprintf("%s — %d/%d links OK, %d broken", name, ok, total, len(brokenLinks)))

		if Flags.Verbose {
			// Show full audit detail (healthy + broken) in verbose mode
			printAudit(name, path, agentsHome, "")
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
		ui.Success(fmt.Sprintf("Repaired links in %d platform(s). Run 'dot-agents status --audit' to verify.", totalFixed))
		fmt.Fprintln(os.Stdout)
	}
	return nil
}

// brokenLink holds info about a single broken managed link.
type brokenLink struct {
	platformID string
	linkPath   string // relative display path
	dest       string // symlink/hardlink target
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
			if strings.HasPrefix(e.Name(), "global--") {
				srcName := strings.TrimPrefix(e.Name(), "global--")
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
			dest, err := os.Readlink(linkPath)
			if err != nil {
				continue
			}
			if _, err := os.Stat(dest); err != nil {
				broken = append(broken, brokenLink{
					platformID: "claude",
					linkPath:   rel(linkPath),
					dest:       config.DisplayPath(dest),
				})
			}
		}
	}

	// Codex AGENTS.md
	agentsMD := filepath.Join(path, "AGENTS.md")
	if dest, err := os.Readlink(agentsMD); err == nil {
		if _, err := os.Stat(dest); err != nil {
			broken = append(broken, brokenLink{
				platformID: "codex",
				linkPath:   rel(agentsMD),
				dest:       config.DisplayPath(dest),
			})
		}
	}

	// Copilot instructions
	copilotPath := filepath.Join(path, ".github", "copilot-instructions.md")
	if dest, err := os.Readlink(copilotPath); err == nil {
		if _, err := os.Stat(dest); err != nil {
			broken = append(broken, brokenLink{
				platformID: "copilot",
				linkPath:   rel(copilotPath),
				dest:       config.DisplayPath(dest),
			})
		}
	}

	// Copilot MCP (.vscode/mcp.json)
	vscodeMCP := filepath.Join(path, ".vscode", "mcp.json")
	if dest, err := os.Readlink(vscodeMCP); err == nil {
		if _, err := os.Stat(dest); err != nil {
			broken = append(broken, brokenLink{
				platformID: "copilot",
				linkPath:   rel(vscodeMCP),
				dest:       config.DisplayPath(dest),
			})
		}
	}

	// Claude MCP (.mcp.json)
	claudeMCP := filepath.Join(path, ".mcp.json")
	if dest, err := os.Readlink(claudeMCP); err == nil {
		if _, err := os.Stat(dest); err != nil {
			broken = append(broken, brokenLink{
				platformID: "claude",
				linkPath:   rel(claudeMCP),
				dest:       config.DisplayPath(dest),
			})
		}
	}

	// OpenCode
	opencodeJSON := filepath.Join(path, "opencode.json")
	if dest, err := os.Readlink(opencodeJSON); err == nil {
		if _, err := os.Stat(dest); err != nil {
			broken = append(broken, brokenLink{
				platformID: "opencode",
				linkPath:   rel(opencodeJSON),
				dest:       config.DisplayPath(dest),
			})
		}
	}

	return broken
}

// collectBrokenUserLinks returns all broken managed user-level links in the home directory.
func collectBrokenUserLinks(agentsHome string) []brokenLink {
	var broken []brokenLink

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return broken
	}
	displayBase := homeDir + string(os.PathSeparator)
	rel := func(p string) string {
		return strings.TrimPrefix(p, displayBase)
	}

	// Claude: ~/.claude/CLAUDE.md
	claudeHome := filepath.Join(homeDir, ".claude")
	claudeMD := filepath.Join(claudeHome, "CLAUDE.md")
	if info, err := os.Lstat(claudeMD); err == nil && info.Mode()&os.ModeSymlink != 0 {
		if dest, err := os.Readlink(claudeMD); err == nil {
			if _, err := os.Stat(dest); err != nil {
				broken = append(broken, brokenLink{
					platformID: "claude",
					linkPath:   rel(claudeMD),
					dest:       config.DisplayPath(dest),
				})
			}
		}
	}

	// Claude: ~/.claude/settings.json
	claudeSettings := filepath.Join(claudeHome, "settings.json")
	if info, err := os.Lstat(claudeSettings); err == nil && info.Mode()&os.ModeSymlink != 0 {
		if dest, err := os.Readlink(claudeSettings); err == nil {
			if _, err := os.Stat(dest); err != nil {
				broken = append(broken, brokenLink{
					platformID: "claude",
					linkPath:   rel(claudeSettings),
					dest:       config.DisplayPath(dest),
				})
			}
		}
	}

	// Claude: ~/.claude/agents/*
	claudeAgentsDir := filepath.Join(claudeHome, "agents")
	if entries, err := os.ReadDir(claudeAgentsDir); err == nil {
		for _, e := range entries {
			linkPath := filepath.Join(claudeAgentsDir, e.Name())
			if info, err := os.Lstat(linkPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
				if dest, err := os.Readlink(linkPath); err == nil {
					if _, err := os.Stat(dest); err != nil {
						broken = append(broken, brokenLink{
							platformID: "claude",
							linkPath:   rel(linkPath),
							dest:       config.DisplayPath(dest),
						})
					}
				}
			}
		}
	}

	// Claude: ~/.claude/skills/*
	claudeSkillsDir := filepath.Join(claudeHome, "skills")
	if entries, err := os.ReadDir(claudeSkillsDir); err == nil {
		for _, e := range entries {
			linkPath := filepath.Join(claudeSkillsDir, e.Name())
			if info, err := os.Lstat(linkPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
				if dest, err := os.Readlink(linkPath); err == nil {
					if _, err := os.Stat(dest); err != nil {
						broken = append(broken, brokenLink{
							platformID: "claude",
							linkPath:   rel(linkPath),
							dest:       config.DisplayPath(dest),
						})
					}
				}
			}
		}
	}

	// Codex: ~/.codex/agents/*
	codexAgentsDir := filepath.Join(homeDir, ".codex", "agents")
	if entries, err := os.ReadDir(codexAgentsDir); err == nil {
		for _, e := range entries {
			linkPath := filepath.Join(codexAgentsDir, e.Name())
			if info, err := os.Lstat(linkPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
				if dest, err := os.Readlink(linkPath); err == nil {
					if _, err := os.Stat(dest); err != nil {
						broken = append(broken, brokenLink{
							platformID: "codex",
							linkPath:   rel(linkPath),
							dest:       config.DisplayPath(dest),
						})
					}
				}
			}
		}
	}

	// OpenCode: ~/.opencode/agent/*
	opencodeAgentDir := filepath.Join(homeDir, ".opencode", "agent")
	if entries, err := os.ReadDir(opencodeAgentDir); err == nil {
		for _, e := range entries {
			linkPath := filepath.Join(opencodeAgentDir, e.Name())
			if info, err := os.Lstat(linkPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
				if dest, err := os.Readlink(linkPath); err == nil {
					if _, err := os.Stat(dest); err != nil {
						broken = append(broken, brokenLink{
							platformID: "opencode",
							linkPath:   rel(linkPath),
							dest:       config.DisplayPath(dest),
						})
					}
				}
			}
		}
	}

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
			if strings.HasPrefix(e.Name(), "global--") {
				srcName := strings.TrimPrefix(e.Name(), "global--")
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
	// Claude symlinks
	claudeRulesDir := filepath.Join(path, ".claude", "rules")
	if entries, err := os.ReadDir(claudeRulesDir); err == nil {
		for _, e := range entries {
			linkPath := filepath.Join(claudeRulesDir, e.Name())
			if dest, err := os.Readlink(linkPath); err == nil {
				if _, err := os.Stat(dest); err == nil {
					ok++
				}
			}
		}
	}
	// Single-file symlinks
	for _, f := range []string{
		filepath.Join(path, "AGENTS.md"),
		filepath.Join(path, ".github", "copilot-instructions.md"),
		filepath.Join(path, "opencode.json"),
		filepath.Join(path, ".mcp.json"),
		filepath.Join(path, ".vscode", "mcp.json"),
	} {
		if dest, err := os.Readlink(f); err == nil {
			if _, err := os.Stat(dest); err == nil {
				ok++
			}
		}
	}
	return ok, brokenCount
}

// printUserConfigStatus prints detailed user-level config status (healthy + broken).
func printUserConfigStatus(agentsHome string) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return
	}
	displayBase := homeDir + string(os.PathSeparator)
	rel := func(p string) string {
		return strings.TrimPrefix(p, displayBase)
	}

	// Claude
	claudeHome := filepath.Join(homeDir, ".claude")
	claudeMD := filepath.Join(claudeHome, "CLAUDE.md")
	if info, err := os.Lstat(claudeMD); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			if dest, err := os.Readlink(claudeMD); err == nil {
				displayDest := config.DisplayPath(dest)
				if _, err := os.Stat(dest); err == nil {
					fmt.Fprintf(os.Stdout, "      %s✓%s %s %s→ %s%s\n", ui.Green, ui.Reset, rel(claudeMD), ui.Dim, displayDest, ui.Reset)
				} else {
					fmt.Fprintf(os.Stdout, "      %s✗%s %s %s→ %s (broken)%s\n", ui.Red, ui.Reset, rel(claudeMD), ui.Dim, displayDest, ui.Reset)
				}
			}
		} else {
			fmt.Fprintf(os.Stdout, "      %s○%s %s %s(local file)%s\n", ui.Dim, ui.Reset, rel(claudeMD), ui.Dim, ui.Reset)
		}
	}

	claudeSettings := filepath.Join(claudeHome, "settings.json")
	if info, err := os.Lstat(claudeSettings); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			if dest, err := os.Readlink(claudeSettings); err == nil {
				displayDest := config.DisplayPath(dest)
				if _, err := os.Stat(dest); err == nil {
					fmt.Fprintf(os.Stdout, "      %s✓%s %s %s→ %s%s\n", ui.Green, ui.Reset, rel(claudeSettings), ui.Dim, displayDest, ui.Reset)
				} else {
					fmt.Fprintf(os.Stdout, "      %s✗%s %s %s→ %s (broken)%s\n", ui.Red, ui.Reset, rel(claudeSettings), ui.Dim, displayDest, ui.Reset)
				}
			}
		} else {
			fmt.Fprintf(os.Stdout, "      %s○%s %s %s(local file)%s\n", ui.Dim, ui.Reset, rel(claudeSettings), ui.Dim, ui.Reset)
		}
	}

	claudeAgentsDir := filepath.Join(claudeHome, "agents")
	if entries, err := os.ReadDir(claudeAgentsDir); err == nil {
		for _, e := range entries {
			linkPath := filepath.Join(claudeAgentsDir, e.Name())
			if info, err := os.Lstat(linkPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
				if dest, err := os.Readlink(linkPath); err == nil {
					displayDest := config.DisplayPath(dest)
					if _, err := os.Stat(dest); err == nil {
						fmt.Fprintf(os.Stdout, "      %s✓%s %s %s→ %s%s\n", ui.Green, ui.Reset, rel(linkPath), ui.Dim, displayDest, ui.Reset)
					} else {
						fmt.Fprintf(os.Stdout, "      %s✗%s %s %s→ %s (broken)%s\n", ui.Red, ui.Reset, rel(linkPath), ui.Dim, displayDest, ui.Reset)
					}
				}
			}
		}
	}

	claudeSkillsDir := filepath.Join(claudeHome, "skills")
	if entries, err := os.ReadDir(claudeSkillsDir); err == nil {
		for _, e := range entries {
			linkPath := filepath.Join(claudeSkillsDir, e.Name())
			if info, err := os.Lstat(linkPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
				if dest, err := os.Readlink(linkPath); err == nil {
					displayDest := config.DisplayPath(dest)
					if _, err := os.Stat(dest); err == nil {
						fmt.Fprintf(os.Stdout, "      %s✓%s %s %s→ %s%s\n", ui.Green, ui.Reset, rel(linkPath), ui.Dim, displayDest, ui.Reset)
					} else {
						fmt.Fprintf(os.Stdout, "      %s✗%s %s %s→ %s (broken)%s\n", ui.Red, ui.Reset, rel(linkPath), ui.Dim, displayDest, ui.Reset)
					}
				}
			}
		}
	}

	// Codex
	codexAgentsDir := filepath.Join(homeDir, ".codex", "agents")
	if entries, err := os.ReadDir(codexAgentsDir); err == nil {
		for _, e := range entries {
			linkPath := filepath.Join(codexAgentsDir, e.Name())
			if info, err := os.Lstat(linkPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
				if dest, err := os.Readlink(linkPath); err == nil {
					displayDest := config.DisplayPath(dest)
					if _, err := os.Stat(dest); err == nil {
						fmt.Fprintf(os.Stdout, "      %s✓%s %s %s→ %s%s\n", ui.Green, ui.Reset, rel(linkPath), ui.Dim, displayDest, ui.Reset)
					} else {
						fmt.Fprintf(os.Stdout, "      %s✗%s %s %s→ %s (broken)%s\n", ui.Red, ui.Reset, rel(linkPath), ui.Dim, displayDest, ui.Reset)
					}
				}
			}
		}
	}

	// OpenCode
	opencodeAgentDir := filepath.Join(homeDir, ".opencode", "agent")
	if entries, err := os.ReadDir(opencodeAgentDir); err == nil {
		for _, e := range entries {
			linkPath := filepath.Join(opencodeAgentDir, e.Name())
			if info, err := os.Lstat(linkPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
				if dest, err := os.Readlink(linkPath); err == nil {
					displayDest := config.DisplayPath(dest)
					if _, err := os.Stat(dest); err == nil {
						fmt.Fprintf(os.Stdout, "      %s✓%s %s %s→ %s%s\n", ui.Green, ui.Reset, rel(linkPath), ui.Dim, displayDest, ui.Reset)
					} else {
						fmt.Fprintf(os.Stdout, "      %s✗%s %s %s→ %s (broken)%s\n", ui.Red, ui.Reset, rel(linkPath), ui.Dim, displayDest, ui.Reset)
					}
				}
			}
		}
	}
}
