package agents

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/ui"
)

func listAgents(scope string) error {
	agentsHome := config.AgentsHome()
	agentsDir := filepath.Join(agentsHome, "agents", scope)

	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		ui.Info("No agents found in ~/.agents/agents/" + scope + "/")
		return nil
	}

	ui.Header("Agents (" + scope + ")")
	count := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		agentPath := filepath.Join(agentsDir, e.Name())
		agentMD := filepath.Join(agentPath, "AGENT.md")
		if _, err := os.Stat(agentMD); err == nil {
			desc := readDescriptionFromMarkdown(agentMD)
			if desc != "" {
				ui.Bullet("ok", fmt.Sprintf("%s  %s%s%s", e.Name(), ui.Dim, desc, ui.Reset))
			} else {
				ui.Bullet("ok", e.Name())
			}
		} else {
			ui.Bullet("warn", e.Name()+" (no AGENT.md)")
		}
		count++
	}
	fmt.Fprintf(os.Stdout, "\n  %s%d agent(s) in %s scope%s\n\n", ui.Dim, count, scope, ui.Reset)
	return nil
}

// readDescriptionFromMarkdown parses YAML frontmatter for description:.
// Duplicated from package commands (readFrontmatterDescription) so this subpackage
// stays independent of commands/skills helpers; keep behavior aligned when changing parsing rules.
func readDescriptionFromMarkdown(mdPath string) string {
	f, err := os.Open(mdPath)
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	inFrontmatter := false
	lineNum := 0
	for scanner.Scan() {
		line := scanner.Text()
		lineNum++
		if lineNum == 1 {
			if strings.TrimSpace(line) == "---" {
				inFrontmatter = true
			} else {
				return ""
			}
			continue
		}
		if inFrontmatter {
			if strings.TrimSpace(line) == "---" {
				break
			}
			if strings.HasPrefix(line, "description:") {
				val := strings.TrimPrefix(line, "description:")
				val = strings.TrimSpace(val)
				val = strings.Trim(val, `"'`)
				return val
			}
		}
	}
	return ""
}
