package rules

import (
	"fmt"
	"os"
	"strings"

	"github.com/AGOrcha/dot-agents/commands/internal/cmdutil"
	"github.com/AGOrcha/dot-agents/internal/ui"
)

// RunShow renders metadata for a single canonical rule file. Exported so the
// shim in commands/rules.go can delegate to it.
func RunShow(deps Deps, scope, name string) error {
	extra := func(srcPath string) { showFrontmatterExtra(deps, srcPath) }
	return cmdutil.RunCanonicalShow(scope, name, canonicalSpec(deps), extra)
}

// showFrontmatterExtra appends a `description:` line to the show output when
// the rule file has a non-empty frontmatter description. Mirrors the
// rulesShowFrontmatterExtra helper that previously lived in commands/rules.go.
func showFrontmatterExtra(deps Deps, srcPath string) {
	if desc := ExtractRuleFrontmatterDescription(deps, srcPath); desc != "" {
		fmt.Fprintf(os.Stdout, "  %sdescription:%s %s\n", ui.Dim, ui.Reset, desc)
	}
}

// ExtractRuleFrontmatterDescription parses YAML-style frontmatter at the head
// of a rule file and returns the `description:` value (case-insensitive).
// Returns the empty string if no frontmatter, no closing fence, no
// description key, or the file is unreadable. The file read goes through
// deps.io() (the ruleIO seam) so the read-error branch is fault-injectable.
// Exported so the parent-package shim and its tests can reach it.
func ExtractRuleFrontmatterDescription(deps Deps, path string) string {
	data, err := deps.io().ReadFile(path)
	if err != nil || len(data) == 0 {
		return ""
	}
	s := string(data)
	rest := s
	switch {
	case strings.HasPrefix(s, "---\n"):
		rest = strings.TrimPrefix(s, "---\n")
	case strings.HasPrefix(s, "---\r\n"):
		rest = strings.TrimPrefix(s, "---\r\n")
	default:
		return ""
	}
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return ""
	}
	fm := rest[:end]
	for _, line := range strings.Split(fm, "\n") {
		line = strings.TrimSpace(line)
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(key), "description") {
			return strings.TrimSpace(val)
		}
	}
	return ""
}
