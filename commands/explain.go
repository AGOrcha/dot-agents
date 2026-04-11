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
	case "manifest", "agentsrc", "install":
		printManifestExplanation()
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

	ui.Section("Workflow")
	fmt.Fprintf(os.Stdout, "  %sOwner (once):%s\n", ui.Bold, ui.Reset)
	fmt.Fprintf(os.Stdout, "    dot-agents add .               Register project\n")
	fmt.Fprintf(os.Stdout, "    dot-agents install --generate  Create .agentsrc.json\n")
	fmt.Fprintf(os.Stdout, "    git add .agentsrc.json && git commit\n")
	fmt.Fprintln(os.Stdout)
	fmt.Fprintf(os.Stdout, "  %sTeam member (after clone):%s\n", ui.Bold, ui.Reset)
	fmt.Fprintf(os.Stdout, "    dot-agents install             Apply manifest, done\n")
	fmt.Fprintln(os.Stdout)

	ui.Section("Topics")
	fmt.Fprintf(os.Stdout, "  %sdot-agents explain manifest%s    .agentsrc.json schema and workflow\n", ui.Dim, ui.Reset)
	fmt.Fprintf(os.Stdout, "  %sdot-agents explain links%s       Link types (symlinks vs hard links)\n", ui.Dim, ui.Reset)
	fmt.Fprintf(os.Stdout, "  %sdot-agents explain platforms%s   Supported AI platforms\n", ui.Dim, ui.Reset)
	fmt.Fprintf(os.Stdout, "  %sdot-agents explain structure%s   ~/.agents/ directory structure\n", ui.Dim, ui.Reset)
	fmt.Fprintln(os.Stdout)
}

func printManifestExplanation() {
	ui.Header("Manifest (.agentsrc.json)")
	fmt.Fprintf(os.Stdout, "  Commit .agentsrc.json to git so any clone can run\n")
	fmt.Fprintf(os.Stdout, "  %sdot-agents install%s to set up fully — no manual steps.\n\n", ui.Bold, ui.Reset)

	ui.Section("Schema")
	fields := [][2]string{
		{"skills", "Names of skills to link from sources"},
		{"agents", "Names of subagents to link from sources"},
		{"rules", `Scopes: "global", "project"`},
		{"hooks", `true (all), false, or ["PreToolUse", "PostToolUse", ...]`},
		{"mcp", `true (all), false, or ["github", "filesystem", ...]`},
		{"settings", "true/false — link platform settings (Cursor, etc.)"},
		{"sources", `[{"type":"local"} | {"type":"git","url":"...","ref":"..."}]`},
	}
	for _, f := range fields {
		fmt.Fprintf(os.Stdout, "  %s%-10s%s  %s%s%s\n", ui.Cyan, f[0], ui.Reset, ui.Dim, f[1], ui.Reset)
	}
	fmt.Fprintln(os.Stdout)

	ui.Section("Sources")
	fmt.Fprintf(os.Stdout, "  %slocal%s   Search ~/.agents/ (default, no network)\n", ui.Bold, ui.Reset)
	fmt.Fprintf(os.Stdout, `  %s{"type":"local"}%s`+"\n\n", ui.Dim, ui.Reset)
	fmt.Fprintf(os.Stdout, "  %sgit%s     Clone/pull a remote repo into cache\n", ui.Bold, ui.Reset)
	fmt.Fprintf(os.Stdout, `  %s{"type":"git","url":"https://github.com/org/agents.git","ref":"main"}%s`+"\n\n", ui.Dim, ui.Reset)
	fmt.Fprintf(os.Stdout, "  Cache: %s~/.cache/dot-agents/sources/<hash>/%s\n\n", ui.Dim, ui.Reset)

	ui.Section("Workflow")
	fmt.Fprintf(os.Stdout, "  %sOwner (once):%s\n", ui.Bold, ui.Reset)
	fmt.Fprintf(os.Stdout, "    dot-agents add .               Register the project\n")
	fmt.Fprintf(os.Stdout, "    dot-agents install --generate  Create .agentsrc.json from current state\n")
	fmt.Fprintf(os.Stdout, "    git add .agentsrc.json && git commit -m 'Add dot-agents manifest'\n\n")
	fmt.Fprintf(os.Stdout, "  %sTeam member (after clone):%s\n", ui.Bold, ui.Reset)
	fmt.Fprintf(os.Stdout, "    dot-agents init                (one-time per machine)\n")
	fmt.Fprintf(os.Stdout, "    dot-agents install             Apply manifest — all links created\n\n")
	fmt.Fprintf(os.Stdout, "  %sKeeping it up to date:%s\n", ui.Bold, ui.Reset)
	fmt.Fprintf(os.Stdout, "    dot-agents skills new <n> --project <p>  → manifest updated automatically\n")
	fmt.Fprintf(os.Stdout, "    dot-agents agents new <n> --project <p>  → manifest updated automatically\n")
	fmt.Fprintf(os.Stdout, "    dot-agents hooks add <Event> ...         → manifest updated automatically\n")
	fmt.Fprintf(os.Stdout, "    dot-agents install --generate            → regenerate from current state\n\n")

	ui.Section("Flags")
	fmt.Fprintf(os.Stdout, "  %s--generate%s  Create/overwrite .agentsrc.json from current ~/.agents/ state\n", ui.Cyan, ui.Reset)
	fmt.Fprintf(os.Stdout, "  %s--strict%s    Fail if any declared resource is not found\n", ui.Cyan, ui.Reset)
	fmt.Fprintf(os.Stdout, "  %s--dry-run%s   Preview changes without applying\n", ui.Cyan, ui.Reset)
	fmt.Fprintf(os.Stdout, "  %s--force%s     Re-fetch git sources even if recently cached\n\n", ui.Cyan, ui.Reset)
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

	fmt.Fprintf(os.Stdout, "  %sCENTRALIZED SHARED TARGETS%s %s(shared skill mirrors)%s\n", ui.Bold, ui.Reset, ui.Dim, ui.Reset)
	fmt.Fprintf(os.Stdout, "  Shared repo-local skill targets such as .agents/skills/<name> are planned\n")
	fmt.Fprintf(os.Stdout, "  centrally before writes so compatible Claude, Codex, OpenCode, and Copilot\n")
	fmt.Fprintf(os.Stdout, "  projections converge on one managed mirror instead of each platform racing\n")
	fmt.Fprintf(os.Stdout, "  to replace the same directory independently.\n")
	fmt.Fprintln(os.Stdout)
}

func printPlatformsExplanation() {
	ui.Header("Supported Platforms")
	platforms := [][2]string{
		{"Cursor", ".cursor/rules/ (hard links), .cursor/settings.json, .cursor/mcp.json"},
		{"Claude Code", ".claude/rules/ (symlinks), .claude/agents/, .claude/skills/, shared .agents/skills/, .mcp.json"},
		{"Codex CLI", "AGENTS.md (symlink), shared .agents/skills/, .codex/hooks.json"},
		{"OpenCode", "opencode.json (symlink), .opencode/agent/*.md, shared .agents/skills/"},
		{"GitHub Copilot", ".github/copilot-instructions.md (symlink), .vscode/mcp.json, shared .agents/skills/"},
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
