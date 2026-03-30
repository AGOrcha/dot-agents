package commands

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/links"
	"github.com/NikashPrakash/dot-agents/internal/ui"
	"github.com/spf13/cobra"
)

func NewStatusCmd() *cobra.Command {
	var audit bool
	var agentFilter string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show managed projects and link health",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus(audit, agentFilter)
		},
	}
	cmd.Flags().BoolVar(&audit, "audit", false, "Show detailed link audit for each project")
	cmd.Flags().StringVar(&agentFilter, "agent", "", "Filter to specific agent (cursor, claude, codex, opencode, copilot)")
	return cmd
}

func runStatus(audit bool, agentFilter string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	agentsHome := config.AgentsHome()
	displayHome := config.DisplayPath(agentsHome)

	ui.Header("dot-agents status")
	fmt.Fprintf(os.Stdout, "  %s%s%s\n", ui.Dim, displayHome, ui.Reset)

	// Git repo status for ~/.agents/
	gitDir := filepath.Join(agentsHome, ".git")
	if _, err := os.Stat(gitDir); err == nil {
		branchOut, _ := exec.Command("git", "-C", agentsHome, "rev-parse", "--abbrev-ref", "HEAD").Output()
		branch := strings.TrimSpace(string(branchOut))
		remoteOut, _ := exec.Command("git", "-C", agentsHome, "remote", "get-url", "origin").Output()
		remote := strings.TrimSpace(string(remoteOut))
		if remote != "" {
			fmt.Fprintf(os.Stdout, "  %sgit:%s %s%s%s %s(%s)%s\n", ui.Dim, ui.Reset, ui.Bold, branch, ui.Reset, ui.Dim, remote, ui.Reset)
		} else {
			fmt.Fprintf(os.Stdout, "  %sgit:%s %s%s%s  %s! no remote — run: dot-agents sync init%s\n", ui.Dim, ui.Reset, ui.Bold, branch, ui.Reset, ui.Yellow, ui.Reset)
		}
	} else {
		fmt.Fprintf(os.Stdout, "  %s! not a git repo — run: dot-agents sync init%s\n", ui.Yellow, ui.Reset)
	}

	// User-level config summary (home directory)
	printUserConfigSection(agentsHome, audit, agentFilter)

	names := cfg.ListProjects()
	sort.Strings(names)

	if len(names) == 0 {
		fmt.Fprintln(os.Stdout, "\n  No managed projects.")
		fmt.Fprintln(os.Stdout, "  Add one with: dot-agents add <path>")
		return nil
	}

	for _, name := range names {
		path := cfg.GetProjectPath(name)
		displayPath := config.DisplayPath(path)

		fmt.Fprintf(os.Stdout, "\n  %s%s%s\n", ui.Bold, name, ui.Reset)

		// Suppress path display if it's just ~/name
		homeDir, _ := os.UserHomeDir()
		expectedSimplePath := "~/" + name
		actualDisplayPath := strings.Replace(path, homeDir, "~", 1)
		if actualDisplayPath != expectedSimplePath {
			fmt.Fprintf(os.Stdout, "  %s%s%s\n", ui.Dim, displayPath, ui.Reset)
		}

		if _, err := os.Stat(path); err != nil {
			ui.Bullet("error", "Directory not found")
			continue
		}

		// Quick health check
		healthOK := 0
		healthWarn := 0

		// Per-platform link presence for badge row
		type platformBadge struct {
			name    string
			present bool
			broken  bool
		}
		badges := []platformBadge{}

		// Cursor
		cursorOK, cursorWarn := 0, 0
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
						cursorOK++
					} else {
						srcMD := strings.TrimSuffix(srcName, ".mdc") + ".md"
						src2 := filepath.Join(agentsHome, "rules", "global", srcMD)
						if linked, _ := links.AreHardlinked(f, src2); linked {
							cursorOK++
						} else {
							cursorWarn++
						}
					}
				}
			}
		}
		// Cursor MCP link
		cursorMCP := filepath.Join(path, ".cursor", "mcp.json")
		if _, err := os.Lstat(cursorMCP); err == nil {
			cursorOK++
		}
		healthOK += cursorOK
		healthWarn += cursorWarn
		badges = append(badges, platformBadge{"Cursor", cursorOK > 0, cursorWarn > 0})

		// Claude
		claudeOK, claudeWarn := 0, 0
		claudeRulesDir := filepath.Join(path, ".claude", "rules")
		if entries, err := os.ReadDir(claudeRulesDir); err == nil {
			for _, e := range entries {
				linkPath := filepath.Join(claudeRulesDir, e.Name())
				if dest, err := os.Readlink(linkPath); err == nil {
					if _, err := os.Stat(dest); err == nil {
						claudeOK++
					} else {
						claudeWarn++
					}
				}
			}
		}
		// Claude MCP link (.mcp.json)
		claudeMCP := filepath.Join(path, ".mcp.json")
		if dest, err := os.Readlink(claudeMCP); err == nil {
			if _, err := os.Stat(dest); err == nil {
				claudeOK++
			} else {
				claudeWarn++
			}
		}
		healthOK += claudeOK
		healthWarn += claudeWarn
		badges = append(badges, platformBadge{"Claude", claudeOK > 0, claudeWarn > 0})

		// Codex (AGENTS.md)
		agentsMD := filepath.Join(path, "AGENTS.md")
		codexOK, codexWarn := 0, 0
		if dest, err := os.Readlink(agentsMD); err == nil {
			if _, err := os.Stat(dest); err == nil {
				codexOK++
			} else {
				codexWarn++
			}
		}
		healthOK += codexOK
		healthWarn += codexWarn
		badges = append(badges, platformBadge{"Codex", codexOK > 0, codexWarn > 0})

		// OpenCode
		opencodeOK, opencodeWarn := 0, 0
		opencodeJSON := filepath.Join(path, "opencode.json")
		if dest, err := os.Readlink(opencodeJSON); err == nil {
			if _, err := os.Stat(dest); err == nil {
				opencodeOK++
			} else {
				opencodeWarn++
			}
		}
		healthOK += opencodeOK
		healthWarn += opencodeWarn
		badges = append(badges, platformBadge{"OpenCode", opencodeOK > 0, opencodeWarn > 0})

		// Copilot
		copilotOK, copilotWarn := 0, 0
		copilotPath := filepath.Join(path, ".github", "copilot-instructions.md")
		if dest, err := os.Readlink(copilotPath); err == nil {
			if _, err := os.Stat(dest); err == nil {
				copilotOK++
			} else {
				copilotWarn++
			}
		}
		healthOK += copilotOK
		healthWarn += copilotWarn
		badges = append(badges, platformBadge{"Copilot", copilotOK > 0, copilotWarn > 0})

		// Print badge row
		fmt.Fprintf(os.Stdout, "  ")
		for i, b := range badges {
			if i > 0 {
				fmt.Fprintf(os.Stdout, "  ")
			}
			if b.broken {
				fmt.Fprintf(os.Stdout, "%s!%s %s", ui.Yellow, ui.Reset, b.name)
			} else if b.present {
				fmt.Fprintf(os.Stdout, "%s✓%s %s", ui.Green, ui.Reset, b.name)
			} else {
				fmt.Fprintf(os.Stdout, "%s-%s %s%s%s", ui.Dim, ui.Reset, ui.Dim, b.name, ui.Reset)
			}
		}
		fmt.Fprintln(os.Stdout)

		// Last refreshed
		if ts := readRefreshTimestamp(path); ts != "" {
			fmt.Fprintf(os.Stdout, "  %slast refreshed: %s%s\n", ui.Dim, ts, ui.Reset)
		}

		if audit {
			printAudit(name, path, agentsHome, agentFilter)
		}
	}

	fmt.Fprintln(os.Stdout)
	return nil
}

// readRefreshTimestamp reads the refreshed_at field from .agents-refresh
func readRefreshTimestamp(projectPath string) string {
	markerPath := filepath.Join(projectPath, ".agents-refresh")
	f, err := os.Open(markerPath)
	if err != nil {
		return ""
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "refreshed_at=") {
			ts := strings.TrimPrefix(line, "refreshed_at=")
			// Simplify ISO timestamp: 2026-03-12T05:18:11Z → 2026-03-12 05:18 UTC
			ts = strings.Replace(ts, "T", " ", 1)
			ts = strings.TrimSuffix(ts, "Z")
			if len(ts) >= 16 {
				ts = ts[:16] + " UTC"
			}
			return ts
		}
	}
	return ""
}

func printAudit(name, path, agentsHome, agentFilter string) {
	fmt.Fprintln(os.Stdout)

	if agentFilter == "" || agentFilter == "cursor" {
		printCursorAudit(name, path, agentsHome)
	}
	if agentFilter == "" || agentFilter == "claude" {
		printClaudeAudit(name, path, agentsHome)
	}
	if agentFilter == "" || agentFilter == "codex" {
		printCodexAudit(name, path, agentsHome)
	}
	if agentFilter == "" || agentFilter == "opencode" {
		printOpenCodeAudit(name, path, agentsHome)
	}
	if agentFilter == "" || agentFilter == "copilot" {
		printCopilotAudit(name, path)
	}
}

// printUserConfigSection reports on user-level (home directory) config links.
func printUserConfigSection(agentsHome string, audit bool, agentFilter string) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return
	}

	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, "  User Config")

	type platformBadge struct {
		name    string
		present bool
		broken  bool
	}

	var badges []platformBadge

	// Claude user-level config
	if agentFilter == "" || agentFilter == "claude" {
		claudeOK, claudeWarn := 0, 0
		claudeHome := filepath.Join(homeDir, ".claude")

		// CLAUDE.md
		claudeMD := filepath.Join(claudeHome, "CLAUDE.md")
		if info, err := os.Lstat(claudeMD); err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				if dest, err := os.Readlink(claudeMD); err == nil {
					if _, err := os.Stat(dest); err == nil {
						claudeOK++
					} else {
						claudeWarn++
					}
				}
			} else {
				claudeOK++
			}
		}

		// settings.json
		claudeSettings := filepath.Join(claudeHome, "settings.json")
		if info, err := os.Lstat(claudeSettings); err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				if dest, err := os.Readlink(claudeSettings); err == nil {
					if _, err := os.Stat(dest); err == nil {
						claudeOK++
					} else {
						claudeWarn++
					}
				}
			} else {
				claudeOK++
			}
		}

		// agents/
		claudeAgentsDir := filepath.Join(claudeHome, "agents")
		if entries, err := os.ReadDir(claudeAgentsDir); err == nil {
			for _, e := range entries {
				linkPath := filepath.Join(claudeAgentsDir, e.Name())
				if info, err := os.Lstat(linkPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
					if dest, err := os.Readlink(linkPath); err == nil {
						if _, err := os.Stat(dest); err == nil {
							claudeOK++
						} else {
							claudeWarn++
						}
					}
				}
			}
		}

		// skills/
		claudeSkillsDir := filepath.Join(claudeHome, "skills")
		if entries, err := os.ReadDir(claudeSkillsDir); err == nil {
			for _, e := range entries {
				linkPath := filepath.Join(claudeSkillsDir, e.Name())
				if info, err := os.Lstat(linkPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
					if dest, err := os.Readlink(linkPath); err == nil {
						if _, err := os.Stat(dest); err == nil {
							claudeOK++
						} else {
							claudeWarn++
						}
					}
				}
			}
		}

		if claudeOK+claudeWarn > 0 {
			badges = append(badges, platformBadge{"Claude", claudeOK > 0, claudeWarn > 0})
		}

		if audit {
			displayBase := homeDir + string(os.PathSeparator)
			rel := func(p string) string { return strings.TrimPrefix(p, displayBase) }

			// Detailed listing
			if info, err := os.Lstat(claudeMD); err == nil {
				if info.Mode()&os.ModeSymlink != 0 {
					if dest, err := os.Readlink(claudeMD); err == nil {
						displayDest := config.DisplayPath(dest)
						if _, err := os.Stat(dest); err == nil {
							fmt.Fprintf(os.Stdout, "    %s✓%s %s %s→ %s%s\n", ui.Green, ui.Reset, rel(claudeMD), ui.Dim, displayDest, ui.Reset)
						} else {
							fmt.Fprintf(os.Stdout, "    %s✗%s %s %s→ %s (broken)%s\n", ui.Red, ui.Reset, rel(claudeMD), ui.Dim, displayDest, ui.Reset)
						}
					}
				} else {
					fmt.Fprintf(os.Stdout, "    %s○%s %s %s(local file)%s\n", ui.Dim, ui.Reset, rel(claudeMD), ui.Dim, ui.Reset)
				}
			}

			if info, err := os.Lstat(claudeSettings); err == nil {
				if info.Mode()&os.ModeSymlink != 0 {
					if dest, err := os.Readlink(claudeSettings); err == nil {
						displayDest := config.DisplayPath(dest)
						if _, err := os.Stat(dest); err == nil {
							fmt.Fprintf(os.Stdout, "    %s✓%s %s %s→ %s%s\n", ui.Green, ui.Reset, rel(claudeSettings), ui.Dim, displayDest, ui.Reset)
						} else {
							fmt.Fprintf(os.Stdout, "    %s✗%s %s %s→ %s (broken)%s\n", ui.Red, ui.Reset, rel(claudeSettings), ui.Dim, displayDest, ui.Reset)
						}
					}
				} else {
					fmt.Fprintf(os.Stdout, "    %s○%s %s %s(local file)%s\n", ui.Dim, ui.Reset, rel(claudeSettings), ui.Dim, ui.Reset)
				}
			}

			if entries, err := os.ReadDir(claudeAgentsDir); err == nil {
				for _, e := range entries {
					linkPath := filepath.Join(claudeAgentsDir, e.Name())
					if info, err := os.Lstat(linkPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
						if dest, err := os.Readlink(linkPath); err == nil {
							displayDest := config.DisplayPath(dest)
							if _, err := os.Stat(dest); err == nil {
								fmt.Fprintf(os.Stdout, "    %s✓%s %s %s→ %s%s\n", ui.Green, ui.Reset, rel(linkPath), ui.Dim, displayDest, ui.Reset)
							} else {
								fmt.Fprintf(os.Stdout, "    %s✗%s %s %s→ %s (broken)%s\n", ui.Red, ui.Reset, rel(linkPath), ui.Dim, displayDest, ui.Reset)
							}
						}
					}
				}
			}

			if entries, err := os.ReadDir(claudeSkillsDir); err == nil {
				for _, e := range entries {
					linkPath := filepath.Join(claudeSkillsDir, e.Name())
					if info, err := os.Lstat(linkPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
						if dest, err := os.Readlink(linkPath); err == nil {
							displayDest := config.DisplayPath(dest)
							if _, err := os.Stat(dest); err == nil {
								fmt.Fprintf(os.Stdout, "    %s✓%s %s %s→ %s%s\n", ui.Green, ui.Reset, rel(linkPath), ui.Dim, displayDest, ui.Reset)
							} else {
								fmt.Fprintf(os.Stdout, "    %s✗%s %s %s→ %s (broken)%s\n", ui.Red, ui.Reset, rel(linkPath), ui.Dim, displayDest, ui.Reset)
							}
						}
					}
				}
			}
		}
	}

	// Codex user-level config
	if agentFilter == "" || agentFilter == "codex" {
		codexOK, codexWarn := 0, 0
		codexAgentsDir := filepath.Join(homeDir, ".codex", "agents")
		if entries, err := os.ReadDir(codexAgentsDir); err == nil {
			for _, e := range entries {
				linkPath := filepath.Join(codexAgentsDir, e.Name())
				if info, err := os.Lstat(linkPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
					if dest, err := os.Readlink(linkPath); err == nil {
						if _, err := os.Stat(dest); err == nil {
							codexOK++
						} else {
							codexWarn++
						}
					}
				}
			}
		}
		if codexOK+codexWarn > 0 {
			badges = append(badges, platformBadge{"Codex", codexOK > 0, codexWarn > 0})
		}

		if audit {
			displayBase := homeDir + string(os.PathSeparator)
			rel := func(p string) string { return strings.TrimPrefix(p, displayBase) }

			if entries, err := os.ReadDir(codexAgentsDir); err == nil {
				for _, e := range entries {
					linkPath := filepath.Join(codexAgentsDir, e.Name())
					if info, err := os.Lstat(linkPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
						if dest, err := os.Readlink(linkPath); err == nil {
							displayDest := config.DisplayPath(dest)
							if _, err := os.Stat(dest); err == nil {
								fmt.Fprintf(os.Stdout, "    %s✓%s %s %s→ %s%s\n", ui.Green, ui.Reset, rel(linkPath), ui.Dim, displayDest, ui.Reset)
							} else {
								fmt.Fprintf(os.Stdout, "    %s✗%s %s %s→ %s (broken)%s\n", ui.Red, ui.Reset, rel(linkPath), ui.Dim, displayDest, ui.Reset)
							}
						}
					}
				}
			}
		}
	}

	// OpenCode user-level config
	if agentFilter == "" || agentFilter == "opencode" {
		opencodeOK, opencodeWarn := 0, 0
		opencodeAgentDir := filepath.Join(homeDir, ".opencode", "agent")
		if entries, err := os.ReadDir(opencodeAgentDir); err == nil {
			for _, e := range entries {
				linkPath := filepath.Join(opencodeAgentDir, e.Name())
				if info, err := os.Lstat(linkPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
					if dest, err := os.Readlink(linkPath); err == nil {
						if _, err := os.Stat(dest); err == nil {
							opencodeOK++
						} else {
							opencodeWarn++
						}
					}
				}
			}
		}
		if opencodeOK+opencodeWarn > 0 {
			badges = append(badges, platformBadge{"OpenCode", opencodeOK > 0, opencodeWarn > 0})
		}

		if audit {
			displayBase := homeDir + string(os.PathSeparator)
			rel := func(p string) string { return strings.TrimPrefix(p, displayBase) }

			if entries, err := os.ReadDir(opencodeAgentDir); err == nil {
				for _, e := range entries {
					linkPath := filepath.Join(opencodeAgentDir, e.Name())
					if info, err := os.Lstat(linkPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
						if dest, err := os.Readlink(linkPath); err == nil {
							displayDest := config.DisplayPath(dest)
							if _, err := os.Stat(dest); err == nil {
								fmt.Fprintf(os.Stdout, "    %s✓%s %s %s→ %s%s\n", ui.Green, ui.Reset, rel(linkPath), ui.Dim, displayDest, ui.Reset)
							} else {
								fmt.Fprintf(os.Stdout, "    %s✗%s %s %s→ %s (broken)%s\n", ui.Red, ui.Reset, rel(linkPath), ui.Dim, displayDest, ui.Reset)
							}
						}
					}
				}
			}
		}
	}

	// Badge row
	if len(badges) == 0 {
		fmt.Fprintf(os.Stdout, "  %s-%s %s(no managed user-level config detected)%s\n", ui.Dim, ui.Reset, ui.Dim, ui.Reset)
		fmt.Fprintln(os.Stdout)
		return
	}

	fmt.Fprintf(os.Stdout, "  ")
	for i, b := range badges {
		if i > 0 {
			fmt.Fprintf(os.Stdout, "  ")
		}
		if b.broken {
			fmt.Fprintf(os.Stdout, "%s!%s %s", ui.Yellow, ui.Reset, b.name)
		} else if b.present {
			fmt.Fprintf(os.Stdout, "%s✓%s %s", ui.Green, ui.Reset, b.name)
		} else {
			fmt.Fprintf(os.Stdout, "%s-%s %s%s%s", ui.Dim, ui.Reset, ui.Dim, b.name, ui.Reset)
		}
	}
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout)
}

func printCursorAudit(name, path, agentsHome string) {
	fmt.Fprintf(os.Stdout, "    %sCursor%s\n", ui.Cyan, ui.Reset)
	rulesDir := filepath.Join(path, ".cursor", "rules")
	entries, err := os.ReadDir(rulesDir)
	if err != nil {
		fmt.Fprintf(os.Stdout, "      %s(no .cursor/rules/)%s\n", ui.Dim, ui.Reset)
		return
	}
	count := 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".mdc") || strings.Contains(e.Name(), ".dot-agents-backup") {
			continue
		}
		f := filepath.Join(rulesDir, e.Name())
		var srcType, linkedTo string
		if strings.HasPrefix(e.Name(), "global--") {
			srcType = "global"
			srcName := strings.TrimPrefix(e.Name(), "global--")
			linkedTo = "~/.agents/rules/global/" + srcName
		} else if strings.HasPrefix(e.Name(), name+"--") {
			srcType = "project"
			srcName := strings.TrimPrefix(e.Name(), name+"--")
			linkedTo = "~/.agents/rules/" + name + "/" + srcName
		} else {
			srcType = "local"
		}

		if srcType == "local" {
			fmt.Fprintf(os.Stdout, "      %s○%s %s %s(local file)%s\n", ui.Dim, ui.Reset, e.Name(), ui.Dim, ui.Reset)
		} else {
			srcPath := strings.Replace(linkedTo, "~/.agents", agentsHome, 1)
			srcPath = strings.Replace(srcPath, "~", os.Getenv("HOME"), 1)
			if linked, _ := links.AreHardlinked(f, srcPath); linked {
				fmt.Fprintf(os.Stdout, "      %s✓%s %s %s← %s%s\n", ui.Green, ui.Reset, e.Name(), ui.Dim, linkedTo, ui.Reset)
			} else {
				fmt.Fprintf(os.Stdout, "      %s!%s %s %s(not linked to %s)%s\n", ui.Yellow, ui.Reset, e.Name(), ui.Dim, linkedTo, ui.Reset)
			}
		}
		count++
	}
	if count == 0 {
		fmt.Fprintf(os.Stdout, "      %s(no rules)%s\n", ui.Dim, ui.Reset)
	}
	// Cursor MCP link (.cursor/mcp.json)
	cursorMCPPath := filepath.Join(path, ".cursor", "mcp.json")
	if info, err := os.Lstat(cursorMCPPath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			dest, _ := os.Readlink(cursorMCPPath)
			displayDest := config.DisplayPath(dest)
			if _, err := os.Stat(dest); err == nil {
				fmt.Fprintf(os.Stdout, "      %s✓%s .cursor/mcp.json %s→ %s%s\n", ui.Green, ui.Reset, ui.Dim, displayDest, ui.Reset)
			} else {
				fmt.Fprintf(os.Stdout, "      %s✗%s .cursor/mcp.json %s→ %s (broken)%s\n", ui.Red, ui.Reset, ui.Dim, displayDest, ui.Reset)
			}
		} else {
			fmt.Fprintf(os.Stdout, "      %s✓%s .cursor/mcp.json %s(hard link or local file)%s\n", ui.Green, ui.Reset, ui.Dim, ui.Reset)
		}
	} else {
		fmt.Fprintf(os.Stdout, "      %s-%s .cursor/mcp.json %s(not linked)%s\n", ui.Dim, ui.Reset, ui.Dim, ui.Reset)
	}
	fmt.Fprintln(os.Stdout)
}

func printClaudeAudit(name, path, agentsHome string) {
	fmt.Fprintf(os.Stdout, "    %sClaude Code%s\n", ui.Cyan, ui.Reset)
	rulesDir := filepath.Join(path, ".claude", "rules")
	entries, err := os.ReadDir(rulesDir)
	if err != nil {
		fmt.Fprintf(os.Stdout, "      %s(no .claude/rules/)%s\n", ui.Dim, ui.Reset)
		fmt.Fprintln(os.Stdout)
		return
	}
	okCount, brokenCount := 0, 0
	for _, e := range entries {
		linkPath := filepath.Join(rulesDir, e.Name())
		dest, err := os.Readlink(linkPath)
		if err != nil {
			continue
		}
		displayDest := config.DisplayPath(dest)
		if _, err := os.Stat(dest); err == nil {
			fmt.Fprintf(os.Stdout, "      %s✓%s %s %s→ %s%s\n", ui.Green, ui.Reset, e.Name(), ui.Dim, displayDest, ui.Reset)
			okCount++
		} else {
			fmt.Fprintf(os.Stdout, "      %s✗%s %s %s→ %s (broken)%s\n", ui.Red, ui.Reset, e.Name(), ui.Dim, displayDest, ui.Reset)
			brokenCount++
		}
	}
	if okCount == 0 && brokenCount == 0 {
		fmt.Fprintf(os.Stdout, "      %s○%s .claude/rules/ %s(empty)%s\n", ui.Dim, ui.Reset, ui.Dim, ui.Reset)
	}
	// Claude MCP link (.mcp.json)
	claudeMCPPath := filepath.Join(path, ".mcp.json")
	if dest, err := os.Readlink(claudeMCPPath); err == nil {
		displayDest := config.DisplayPath(dest)
		if _, err := os.Stat(dest); err == nil {
			fmt.Fprintf(os.Stdout, "      %s✓%s .mcp.json %s→ %s%s\n", ui.Green, ui.Reset, ui.Dim, displayDest, ui.Reset)
		} else {
			fmt.Fprintf(os.Stdout, "      %s✗%s .mcp.json %s→ %s (broken)%s\n", ui.Red, ui.Reset, ui.Dim, displayDest, ui.Reset)
		}
	} else {
		fmt.Fprintf(os.Stdout, "      %s-%s .mcp.json %s(not linked)%s\n", ui.Dim, ui.Reset, ui.Dim, ui.Reset)
	}
	fmt.Fprintln(os.Stdout)
}

func printCodexAudit(name, path, agentsHome string) {
	fmt.Fprintf(os.Stdout, "    %sCodex%s\n", ui.Cyan, ui.Reset)
	agentsMD := filepath.Join(path, "AGENTS.md")
	if info, err := os.Lstat(agentsMD); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			dest, _ := os.Readlink(agentsMD)
			displayDest := config.DisplayPath(dest)
			if _, err := os.Stat(dest); err == nil {
				fmt.Fprintf(os.Stdout, "      %s✓%s AGENTS.md %s→ %s%s\n", ui.Green, ui.Reset, ui.Dim, displayDest, ui.Reset)
			} else {
				fmt.Fprintf(os.Stdout, "      %s✗%s AGENTS.md %s→ %s (broken)%s\n", ui.Red, ui.Reset, ui.Dim, displayDest, ui.Reset)
			}
		} else {
			fmt.Fprintf(os.Stdout, "      %s○%s AGENTS.md %s(local file)%s\n", ui.Dim, ui.Reset, ui.Dim, ui.Reset)
		}
	} else {
		fmt.Fprintf(os.Stdout, "      %s(no AGENTS.md)%s\n", ui.Dim, ui.Reset)
	}
	fmt.Fprintln(os.Stdout)
}

func printOpenCodeAudit(name, path, agentsHome string) {
	fmt.Fprintf(os.Stdout, "    %sOpenCode%s\n", ui.Cyan, ui.Reset)

	// opencode.json symlink
	opencodeJSON := filepath.Join(path, "opencode.json")
	if info, err := os.Lstat(opencodeJSON); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			dest, _ := os.Readlink(opencodeJSON)
			displayDest := config.DisplayPath(dest)
			if _, err := os.Stat(dest); err == nil {
				fmt.Fprintf(os.Stdout, "      %s✓%s opencode.json %s→ %s%s\n", ui.Green, ui.Reset, ui.Dim, displayDest, ui.Reset)
			} else {
				fmt.Fprintf(os.Stdout, "      %s✗%s opencode.json %s→ %s (broken)%s\n", ui.Red, ui.Reset, ui.Dim, displayDest, ui.Reset)
			}
		} else {
			fmt.Fprintf(os.Stdout, "      %s○%s opencode.json %s(local file)%s\n", ui.Dim, ui.Reset, ui.Dim, ui.Reset)
		}
	}

	// .opencode/agent/ directory
	opencodeAgentDir := filepath.Join(path, ".opencode", "agent")
	if entries, err := os.ReadDir(opencodeAgentDir); err == nil {
		okCount, brokenCount := 0, 0
		for _, e := range entries {
			linkPath := filepath.Join(opencodeAgentDir, e.Name())
			dest, err := os.Readlink(linkPath)
			if err != nil {
				continue
			}
			displayDest := config.DisplayPath(dest)
			if _, err := os.Stat(dest); err == nil {
				fmt.Fprintf(os.Stdout, "      %s✓%s .opencode/agent/%s %s→ %s%s\n", ui.Green, ui.Reset, e.Name(), ui.Dim, displayDest, ui.Reset)
				okCount++
			} else {
				fmt.Fprintf(os.Stdout, "      %s✗%s .opencode/agent/%s %s→ %s (broken)%s\n", ui.Red, ui.Reset, e.Name(), ui.Dim, displayDest, ui.Reset)
				brokenCount++
			}
		}
		if okCount == 0 && brokenCount == 0 {
			fmt.Fprintf(os.Stdout, "      %s○%s .opencode/agent/ %s(empty)%s\n", ui.Dim, ui.Reset, ui.Dim, ui.Reset)
		}
	} else {
		fmt.Fprintf(os.Stdout, "      %s(no .opencode/)%s\n", ui.Dim, ui.Reset)
	}
	fmt.Fprintln(os.Stdout)
}

func printCopilotAudit(name, path string) {
	fmt.Fprintf(os.Stdout, "    %sGitHub Copilot%s\n", ui.Cyan, ui.Reset)
	instructionsPath := filepath.Join(path, ".github", "copilot-instructions.md")
	if info, err := os.Lstat(instructionsPath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			dest, _ := os.Readlink(instructionsPath)
			displayDest := config.DisplayPath(dest)
			if _, err := os.Stat(dest); err == nil {
				fmt.Fprintf(os.Stdout, "      %s✓%s .github/copilot-instructions.md %s→ %s%s\n", ui.Green, ui.Reset, ui.Dim, displayDest, ui.Reset)
			} else {
				fmt.Fprintf(os.Stdout, "      %s✗%s .github/copilot-instructions.md %s→ %s (broken)%s\n", ui.Red, ui.Reset, ui.Dim, displayDest, ui.Reset)
			}
		}
	} else {
		fmt.Fprintf(os.Stdout, "      %s-%s .github/copilot-instructions.md %s(not linked)%s\n", ui.Dim, ui.Reset, ui.Dim, ui.Reset)
	}
	// Copilot MCP link (.vscode/mcp.json)
	vscodeMCPPath := filepath.Join(path, ".vscode", "mcp.json")
	if dest, err := os.Readlink(vscodeMCPPath); err == nil {
		displayDest := config.DisplayPath(dest)
		if _, err := os.Stat(dest); err == nil {
			fmt.Fprintf(os.Stdout, "      %s✓%s .vscode/mcp.json %s→ %s%s\n", ui.Green, ui.Reset, ui.Dim, displayDest, ui.Reset)
		} else {
			fmt.Fprintf(os.Stdout, "      %s✗%s .vscode/mcp.json %s→ %s (broken)%s\n", ui.Red, ui.Reset, ui.Dim, displayDest, ui.Reset)
		}
	} else {
		fmt.Fprintf(os.Stdout, "      %s-%s .vscode/mcp.json %s(not linked)%s\n", ui.Dim, ui.Reset, ui.Dim, ui.Reset)
	}
	fmt.Fprintln(os.Stdout)
}
