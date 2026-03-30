package commands

import (
	"fmt"
	"os"

	"github.com/NikashPrakash/dot-agents/internal/ui"
	"github.com/spf13/cobra"
)

func NewExplainCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "explain [topic]",
		Short: "Explain dot-agents concepts",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runExplain,
	}
}

func runExplain(cmd *cobra.Command, args []string) error {
	topic := "overview"
	if len(args) > 0 {
		topic = args[0]
	}

	switch topic {
	case "links", "link-types":
		printLinkTypesExplanation()
	case "platforms":
		printPlatformsExplanation()
	case "structure", "layout":
		printStructureExplanation()
	default:
		printOverviewExplanation()
	}
	return nil
}

func printOverviewExplanation() {
	ui.Header("dot-agents overview")
	fmt.Fprintf(os.Stdout, "  dot-agents manages AI agent configurations across your projects.\n")
	fmt.Fprintf(os.Stdout, "  It maintains a single source of truth in %s~/.agents/%s and creates links\n", ui.Bold, ui.Reset)
	fmt.Fprintf(os.Stdout, "  into each project directory for each AI platform you use.\n")

	ui.Section("Commands")
	commands := [][2]string{
		{"init", "Create ~/.agents/ structure"},
		{"add", "Register a project and create links"},
		{"remove", "Unregister a project and remove links"},
		{"refresh", "Re-apply links after updating ~/.agents/"},
		{"status", "Show managed projects and link health"},
		{"doctor", "Diagnose installation issues"},
		{"skills", "Manage skills"},
		{"agents", "Manage agent definitions"},
		{"sync", "Git operations on ~/.agents/"},
	}
	for _, c := range commands {
		fmt.Fprintf(os.Stdout, "  %s%-10s%s  %s%s%s\n", ui.Cyan, c[0], ui.Reset, ui.Dim, c[1], ui.Reset)
	}

	ui.Section("Topics")
	fmt.Fprintf(os.Stdout, "  %sdot-agents explain links%s      Link types (symlinks vs hard links)\n", ui.Dim, ui.Reset)
	fmt.Fprintf(os.Stdout, "  %sdot-agents explain platforms%s   Supported AI platforms\n", ui.Dim, ui.Reset)
	fmt.Fprintf(os.Stdout, "  %sdot-agents explain structure%s   ~/.agents/ directory structure\n", ui.Dim, ui.Reset)
	fmt.Fprintln(os.Stdout)
}

func printLinkTypesExplanation() {
	ui.Header("Link Types")
	fmt.Fprintln(os.Stdout)

	fmt.Fprintf(os.Stdout, "  %sHARD LINKS%s %s(Cursor)%s\n", ui.Bold, ui.Reset, ui.Dim, ui.Reset)
	fmt.Fprintf(os.Stdout, "  Cursor doesn't follow symlinks for rule files, so dot-agents creates\n")
	fmt.Fprintf(os.Stdout, "  hard links instead. Hard links point to the same inode on disk —\n")
	fmt.Fprintf(os.Stdout, "  edits to either file are reflected in both.\n")
	fmt.Fprintln(os.Stdout)
	fmt.Fprintf(os.Stdout, "  %s~/.agents/rules/global/rules.mdc → .cursor/rules/global--rules.mdc%s\n", ui.Dim, ui.Reset)

	fmt.Fprintln(os.Stdout)
	fmt.Fprintf(os.Stdout, "  %sSYMLINKS%s %s(all other platforms)%s\n", ui.Bold, ui.Reset, ui.Dim, ui.Reset)
	fmt.Fprintf(os.Stdout, "  Claude Code, Codex, OpenCode, and GitHub Copilot all follow symlinks\n")
	fmt.Fprintf(os.Stdout, "  correctly, so dot-agents uses standard symbolic links.\n")
	fmt.Fprintln(os.Stdout)
	fmt.Fprintf(os.Stdout, "  %s~/.agents/rules/global/rules.mdc → AGENTS.md%s\n", ui.Dim, ui.Reset)
	fmt.Fprintln(os.Stdout)
}

func printPlatformsExplanation() {
	ui.Header("Supported Platforms")
	platforms := [][2]string{
		{"Cursor", ".cursor/rules/ (hard links), .cursor/settings.json, .cursor/mcp.json"},
		{"Claude Code", ".claude/rules/ (symlinks), .claude/agents/, .mcp.json"},
		{"Codex CLI", "AGENTS.md (symlink), .agents/skills/, .codex/hooks.json"},
		{"OpenCode", "opencode.json (symlink), .opencode/agent/*.md"},
		{"GitHub Copilot", ".github/copilot-instructions.md (symlink), .vscode/mcp.json"},
	}
	fmt.Fprintln(os.Stdout)
	for _, p := range platforms {
		fmt.Fprintf(os.Stdout, "  %s%-16s%s  %s%s%s\n", ui.Cyan, p[0], ui.Reset, ui.Dim, p[1], ui.Reset)
	}
	fmt.Fprintln(os.Stdout)
}

func printStructureExplanation() {
	ui.Header("~/.agents/ Directory Structure")
	fmt.Fprintln(os.Stdout)
	lines := []struct{ indent, name, desc string }{
		{"  ", "~/.agents/", ""},
		{"  ├── ", "config.json", "Project registry"},
		{"  ├── ", "rules/", ""},
		{"  │   ├── ", "global/", "Rules for ALL projects"},
		{"  │   └── ", "{project}/", "Rules for a specific project"},
		{"  ├── ", "settings/", ""},
		{"  │   ├── ", "global/", "Global settings (claude-code.json, cursor.json)"},
		{"  │   └── ", "{project}/", "Project-specific settings"},
		{"  ├── ", "mcp/", ""},
		{"  │   ├── ", "global/", "Global MCP configs"},
		{"  │   └── ", "{project}/", "Project MCP configs"},
		{"  ├── ", "skills/", ""},
		{"  │   ├── ", "global/", "Skills available everywhere"},
		{"  │   └── ", "{project}/", "Project-specific skills"},
		{"  ├── ", "agents/", ""},
		{"  │   ├── ", "global/", "Agents available everywhere"},
		{"  │   └── ", "{project}/", "Project-specific agents"},
		{"  ├── ", "hooks/", ""},
		{"  │   ├── ", "global/", "Global hook configs"},
		{"  │   └── ", "{project}/", "Project-specific hook configs"},
		{"  ├── ", "scripts/", "Helper scripts"},
		{"  ├── ", "local/", "Machine-specific local files"},
		{"  └── ", "resources/", "Backup files (auto-managed)"},
	}
	for _, l := range lines {
		if l.desc != "" {
			fmt.Fprintf(os.Stdout, "%s%s%s%-26s%s%s%s\n", l.indent, ui.Cyan, ui.Bold, l.name, ui.Reset, ui.Dim+l.desc, ui.Reset)
		} else {
			fmt.Fprintf(os.Stdout, "%s%s%s%s%s\n", l.indent, ui.Cyan, ui.Bold, l.name, ui.Reset)
		}
	}
	fmt.Fprintln(os.Stdout)
}
