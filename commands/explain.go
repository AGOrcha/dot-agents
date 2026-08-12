package commands

import (
	"fmt"
	"os"

	"github.com/AGOrcha/dot-agents/internal/ui"
	"github.com/spf13/cobra"
)

func NewExplainCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "explain [topic]",
		Short: "Explain da concepts",
		Long: `Prints operator-facing documentation for the concepts that matter when
setting up or debugging da. The output is intentionally compact enough
for a human to scan and structured enough for an AI agent to quote or reason over.`,
		Example: ExampleBlock(
			"  da explain",
			"  da explain manifest",
			"  da explain structure",
			"  da explain links",
		),
		Args: MaximumNArgsWithHints(1, "Supported topics include `manifest`, `structure`, `links`, and `platforms`."),
		RunE: runExplain,
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
	ui.Header("da overview")
	fmt.Fprintf(os.Stdout, "  da manages AI agent configurations across your projects.\n")
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
		{"hooks", "List/show/remove hook bundles under ~/.agents/hooks/"},
		{"rules", "List/show/remove rule files under ~/.agents/rules/"},
		{"agents", "Manage agent definitions"},
		{"sync", "Git operations on ~/.agents/"},
	}
	for _, c := range commands {
		fmt.Fprintf(os.Stdout, "  %s%-10s%s  %s%s%s\n", ui.Cyan, c[0], ui.Reset, ui.Dim, c[1], ui.Reset)
	}

	ui.Section("Workflow")
	fmt.Fprintf(os.Stdout, "  %sOwner (once):%s\n", ui.Bold, ui.Reset)
	fmt.Fprintf(os.Stdout, "    da add .               Register project\n")
	fmt.Fprintf(os.Stdout, "    da install --generate  Create .agentsrc.json\n")
	fmt.Fprintf(os.Stdout, "    git add .agentsrc.json && git commit\n")
	fmt.Fprintln(os.Stdout)
	fmt.Fprintf(os.Stdout, "  %sTeam member (after clone):%s\n", ui.Bold, ui.Reset)
	fmt.Fprintf(os.Stdout, "    da install             Apply manifest, done\n")
	fmt.Fprintln(os.Stdout)

	ui.Section("Topics")
	fmt.Fprintf(os.Stdout, "  %sda explain manifest%s    .agentsrc.json schema and workflow\n", ui.Dim, ui.Reset)
	fmt.Fprintf(os.Stdout, "  %sda explain links%s       Link types (symlinks vs hard links)\n", ui.Dim, ui.Reset)
	fmt.Fprintf(os.Stdout, "  %sda explain platforms%s   Supported AI platforms\n", ui.Dim, ui.Reset)
	fmt.Fprintf(os.Stdout, "  %sda explain structure%s   ~/.agents/ directory structure\n", ui.Dim, ui.Reset)
	fmt.Fprintln(os.Stdout)
}

func printManifestExplanation() {
	ui.Header("Manifest (.agentsrc.json)")
	fmt.Fprintf(os.Stdout, "  Commit .agentsrc.json to git so any clone can run\n")
	fmt.Fprintf(os.Stdout, "  %sda install%s to set up fully — no manual steps.\n\n", ui.Bold, ui.Reset)

	ui.Section("Schema")
	fields := [][2]string{
		{"skills", "Names of skills to link from sources"},
		{"agents", "Names of subagents to link from sources"},
		{"rules", `Scopes: "global", "project"`},
		{"hooks", `true (all), false, or ["PreToolUse", ...]; omit to inherit from layers`},
		{"mcp", `true (all), false, or ["github", ...]; omit to inherit from layers`},
		{"settings", "true/false — link platform settings; omit to inherit from layers"},
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
	fmt.Fprintf(os.Stdout, "    da add .               Register the project\n")
	fmt.Fprintf(os.Stdout, "    da install --generate  Create .agentsrc.json from current state\n")
	fmt.Fprintf(os.Stdout, "    git add .agentsrc.json && git commit -m 'Add da manifest'\n\n")
	fmt.Fprintf(os.Stdout, "  %sTeam member (after clone):%s\n", ui.Bold, ui.Reset)
	fmt.Fprintf(os.Stdout, "    da init                (one-time per machine)\n")
	fmt.Fprintf(os.Stdout, "    da install             Apply manifest — all links created\n\n")
	fmt.Fprintf(os.Stdout, "  %sKeeping it up to date:%s\n", ui.Bold, ui.Reset)
	fmt.Fprintf(os.Stdout, "    da skills new <n> --project <p>  → manifest updated automatically\n")
	fmt.Fprintf(os.Stdout, "    da agents new <n> --project <p>  → manifest updated automatically\n")
	fmt.Fprintf(os.Stdout, "    da hooks list|show|remove       → inspect ~/.agents/hooks bundles (author on disk, then refresh/install)\n")
	fmt.Fprintf(os.Stdout, "    da rules list|show|remove       → inspect ~/.agents/rules files (author on disk, then refresh/install)\n")
	fmt.Fprintf(os.Stdout, "    da install --generate            → regenerate from current state\n\n")

	ui.Section("Flags")
	fmt.Fprintf(os.Stdout, "  %s--generate%s  Create/overwrite .agentsrc.json from current ~/.agents/ state\n", ui.Cyan, ui.Reset)
	fmt.Fprintf(os.Stdout, "  %s--strict%s    Fail if any declared resource is not found\n", ui.Cyan, ui.Reset)
	fmt.Fprintf(os.Stdout, "  %s--dry-run%s   Preview changes without applying\n", ui.Cyan, ui.Reset)
	fmt.Fprintf(os.Stdout, "  %s--force%s     Re-fetch git sources even if recently cached\n\n", ui.Cyan, ui.Reset)
}

func printLinkTypeSection(title, scope, body, example string) {
	fmt.Fprintf(os.Stdout, "  %s%s%s %s(%s)%s\n", ui.Bold, title, ui.Reset, ui.Dim, scope, ui.Reset)
	fmt.Fprint(os.Stdout, body)
	fmt.Fprintln(os.Stdout)
	fmt.Fprintf(os.Stdout, "  %s%s%s\n", ui.Dim, example, ui.Reset)
}

func printLinkTypesExplanation() {
	ui.Header("Link Types")
	fmt.Fprintln(os.Stdout)

	printLinkTypeSection(
		"HARD LINKS",
		"Cursor",
		"  Cursor doesn't follow symlinks for rule files, so da creates\n"+
			"  hard links instead. Hard links point to the same inode on disk —\n"+
			"  edits to either file are reflected in both.\n",
		"~/.agents/rules/global/rules.mdc → .cursor/rules/global--rules.mdc",
	)

	fmt.Fprintln(os.Stdout)
	printLinkTypeSection(
		"SYMLINKS",
		"all other platforms",
		"  Claude Code, Codex, OpenCode, and GitHub Copilot all follow symlinks\n"+
			"  correctly, so da uses standard symbolic links.\n",
		"~/.agents/rules/global/rules.mdc → AGENTS.md",
	)
	fmt.Fprintln(os.Stdout)

	fmt.Fprintf(os.Stdout, "  %sCENTRALIZED SHARED TARGETS%s %s(shared skill mirrors)%s\n", ui.Bold, ui.Reset, ui.Dim, ui.Reset)
	fmt.Fprintf(os.Stdout, "  Shared repo-local skill targets such as .agents/skills/<name> are planned\n")
	fmt.Fprintf(os.Stdout, "  centrally before writes so compatible Claude, Codex, OpenCode, and Copilot\n")
	fmt.Fprintf(os.Stdout, "  projections converge on one managed mirror instead of each platform racing\n")
	fmt.Fprintf(os.Stdout, "  to replace the same directory independently.\n")
	fmt.Fprintln(os.Stdout)
	fmt.Fprintf(os.Stdout, "  %sRegistry diagnostics:%s run %sda status --audit%s — the \"Shared target registry\"\n", ui.Dim, ui.Reset, ui.Cyan, ui.Reset)
	fmt.Fprintf(os.Stdout, "  section per project lists the merged plan lines produced by the same builder\n")
	fmt.Fprintf(os.Stdout, "  as %srefresh --dry-run%s (no filesystem writes).\n", ui.Cyan, ui.Reset)
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

// Tree-rendering prefixes + canonical sub-paths shared by the structure
// table. Pulled out so Sonar dup-literal findings on the table do not
// regress and a future tree-glyph change is one edit, not 16+.
const (
	explainTreeBranch    = "  ├── "
	explainTreeChildMid  = "  │   ├── "
	explainTreeChildLast = "  │   └── "
	explainScopeGlobal   = "global/"
	explainScopeProject  = "{project}/"
)

// explainStructureRow describes one rendered line of the structure tree.
type explainStructureRow struct{ indent, name, desc string }

// scopedBucket captures the 13 ~/.agents/<bucket>/ entries that each fan
// out into global/ + {project}/ children. Expanded by appendScopedBucket
// into 3 rows so the explainStructureRow table is not 13× the same
// 3-line literal pattern (which Sonar CPD flags even when the only
// varying token is the description string).
type scopedBucket struct {
	parent      string // e.g. "rules/"
	globalDesc  string
	projectDesc string
}

// scopedBuckets is the source of truth for the parent-with-global+project
// rows in printStructureExplanation. Appending a new bucket here adds three
// rendered tree lines without touching the loop.
var scopedBuckets = []scopedBucket{
	{"rules/", "Rules for ALL projects", "Rules for a specific project"},
	{"settings/", "Global settings (claude-code.json, cursor.json)", "Project-specific settings"},
	{"mcp/", "Global MCP configs", "Project MCP configs"},
	{"skills/", "Skills available everywhere", "Project-specific skills"},
	{"agents/", "Agents available everywhere", "Project-specific agents"},
	{"hooks/", "Global hook configs", "Project-specific hook configs"},
	{"plugins/", "Plugin bundles", "Project-specific plugin bundles"},
	{"commands/", "Command bundles", "Project-specific command bundles"},
	{"output-styles/", "Claude output styles", "Project-specific output styles"},
	{"ignore/", "Ignore files", "Project-specific ignore files"},
	{"modes/", "OpenCode modes", "Project-specific modes"},
	{"themes/", "OpenCode themes", "Project-specific themes"},
	{"prompts/", "Copilot prompts", "Project-specific prompts"},
}

// appendScopedBucket renders one bucket as 3 rows (parent header + global
// child + project child) onto the accumulating row list.
func appendScopedBucket(rows []explainStructureRow, b scopedBucket) []explainStructureRow {
	return append(rows,
		explainStructureRow{explainTreeBranch, b.parent, ""},
		explainStructureRow{explainTreeChildMid, explainScopeGlobal, b.globalDesc},
		explainStructureRow{explainTreeChildLast, explainScopeProject, b.projectDesc},
	)
}

// buildStructureRows returns the full ordered set of rows the structure
// printer renders. Header → config.json → 13 scoped buckets → tail.
func buildStructureRows() []explainStructureRow {
	rows := []explainStructureRow{
		{"  ", "~/.agents/", ""},
		{explainTreeBranch, "config.json", "Project registry"},
	}
	for _, b := range scopedBuckets {
		rows = appendScopedBucket(rows, b)
	}
	rows = append(rows,
		explainStructureRow{explainTreeBranch, "scripts/", "Helper scripts"},
		explainStructureRow{explainTreeBranch, "local/", "Machine-specific local files"},
		explainStructureRow{"  └── ", "resources/", "Backup files (auto-managed)"},
	)
	return rows
}

func printStructureExplanation() {
	ui.Header("~/.agents/ Directory Structure")
	fmt.Fprintln(os.Stdout)
	for _, l := range buildStructureRows() {
		if l.desc != "" {
			fmt.Fprintf(os.Stdout, "%s%s%s%-26s%s%s%s\n", l.indent, ui.Cyan, ui.Bold, l.name, ui.Reset, ui.Dim+l.desc, ui.Reset)
		} else {
			fmt.Fprintf(os.Stdout, "%s%s%s%s%s\n", l.indent, ui.Cyan, ui.Bold, l.name, ui.Reset)
		}
	}
	fmt.Fprintln(os.Stdout)

	ui.Section("Plugins")
	fmt.Fprintf(os.Stdout, "  ~/.agents/plugins/{scope}/{name}/   Plugin bundles\n")
	fmt.Fprintf(os.Stdout, "    PLUGIN.yaml                      Manifest: kind, name, platforms, resources, platform_overrides\n")
	fmt.Fprintf(os.Stdout, "    resources/{agents,skills,...}/   Canonical shared components\n")
	fmt.Fprintf(os.Stdout, "    files/                           Native runtime files (OpenCode JS/TS)\n")
	fmt.Fprintf(os.Stdout, "    platforms/{id}/                  Platform-specific passthrough (e.g. plugin.json)\n")
	fmt.Fprintln(os.Stdout)
}
