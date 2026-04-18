package skills

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/ui"
)

// List prints skills under ~/.agents/skills/<scope>/.
func List(scope string) error {
	agentsHome := config.AgentsHome()
	skillsDir := filepath.Join(agentsHome, "skills", scope)

	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		ui.Info("No skills found in ~/.agents/skills/" + scope + "/")
		return nil
	}

	ui.Header("Skills (" + scope + ")")
	count := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		skillPath := filepath.Join(skillsDir, e.Name())
		skillMD := filepath.Join(skillPath, "SKILL.md")
		if _, err := os.Stat(skillMD); err == nil {
			desc := readDescriptionFromMarkdown(skillMD)
			if desc != "" {
				ui.Bullet("ok", fmt.Sprintf("%s  %s%s%s", e.Name(), ui.Dim, desc, ui.Reset))
			} else {
				ui.Bullet("ok", e.Name())
			}
		} else {
			ui.Bullet("warn", e.Name()+" (no SKILL.md)")
		}
		count++
	}
	fmt.Fprintf(os.Stdout, "\n  %s%d skill(s) in %s scope%s\n\n", ui.Dim, count, scope, ui.Reset)
	return nil
}

// readDescriptionFromMarkdown parses YAML frontmatter for description:.
// Duplicated from package commands (readFrontmatterDescription) so agents list
// stays independent; keep behavior aligned when changing parsing rules.
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
