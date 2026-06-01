package cmdutil

import (
	"github.com/AGOrcha/dot-agents/internal/platform"
)

// canonicalRemoveArgsHint is shared verbatim across MCPResource,
// SettingsResource, and RulesResource — the remove subcommand's arg-hint
// text is identical for all three canonical-file families.
const canonicalRemoveArgsHint = "`scope` is `global` or a managed project name; `name` matches list/show."

// CanonicalResourceDef carries the STATIC per-resource configuration that
// the mcp/settings/rules subpackages used to inline as 90-line struct
// literals inside their respective canonicalSpec(deps) builders. Pulling
// the strings, dir segment, noun forms, and EnsureScope target into a
// single typed table here is what lets each leaf's canonicalSpec collapse
// into a one-liner forwarding to SpecForResource — eliminating the
// three-way duplication Sonar flagged (settings/list.go 66.2%,
// mcp/seams.go 36.1%, rules/list.go 29.6%) without changing observable
// CLI behavior (help strings, examples, error messages all preserved
// verbatim).
//
// Per-resource RUNNERS (List/Resolve callbacks that need the leaf
// package's platform.* helpers and the leaf's findXxxSpec error wrapping)
// still come from the leaf via ResourceRunners — they cannot live here
// because they close over leaf-specific Deps for hint-aware errors.
type CanonicalResourceDef struct {
	// Identity — used by the data-layer RunCanonical* helpers and the
	// parent commands.* user-facing strings.
	Kind        string // "MCP" | "Settings" | "Rule"
	DirSegment  string // "mcp" | "settings" | "rules"
	SingularRem string // "MCP file" | "settings file" | "rule file"

	// EnsureScope verifies the resolved target path is under
	// <agentsHome>/<DirSegment>/<scope>/. The three platform.EnsureUnder*
	// helpers have the same signature, so we can bind them directly.
	EnsureScope func(agentsHome, scope, target string) error

	// EmptyHint / MissingDirHint produce the informational messages the
	// list path prints when the scope has no files / no directory.
	// MissingDirHint is optional — RunCanonicalList falls back to a
	// generic message when nil (settings uses the fallback).
	EmptyHint      func(scope string) string
	MissingDirHint func(scope string) string

	// CLI surface — parent `da <kind>` cobra.Command.
	Use      string   // "mcp" | "settings" | "rules"
	Short    string   // one-line summary
	Long     string   // multi-line description for `--help`
	Examples []string // top-level Example block lines (joined with "\n")

	// List subcommand strings.
	ListShort    string
	ListExamples []string
	ListArgsHint string // passed to MaxArgsWithHints / MaximumNArgsWithHints

	// Show subcommand strings.
	ShowShort    string
	ShowArgsHint string

	// Remove subcommand strings.
	RemoveShort    string
	RemoveLong     string
	RemoveArgsHint string
}

// MCPResource owns the static `da mcp` resource definition. The matching
// runner closures (List/Resolve) live in commands/internal/mcp/seams.go
// alongside findMCPSpec, which wraps platform.ResolveCanonicalMCPFile
// errors via deps.ErrorWithHints / deps.UsageError.
var MCPResource = CanonicalResourceDef{
	Kind:        "MCP",
	DirSegment:  "mcp",
	SingularRem: "MCP file",
	EnsureScope: platform.EnsureUnderMCPScopeTree,
	EmptyHint: func(scope string) string {
		return "No MCP config files (.json/.yaml/.yml/.toml) under ~/.agents/mcp/" + scope + "/"
	},
	MissingDirHint: func(scope string) string {
		return "No ~/.agents/mcp/" + scope + "/ directory yet (no canonical MCP files for this scope)."
	},
	Use:   "mcp",
	Short: "Inspect and manage canonical ~/.agents/mcp config files",
	Long: `Commands for MCP server configs stored under ~/.agents/mcp/<scope>/.

Scopes are either global (~/.agents/mcp/global/) or a managed project name
(~/.agents/mcp/<project>/), matching da status.

These files are what add, import, refresh, install, and remove wire into
Cursor, Claude Code, Copilot, and related projections. Prefer editing canonical
paths here, then run refresh or install for the project.`,
	Examples: []string{
		"  da mcp list",
		"  da mcp list my-app",
		"  da mcp show global mcp.json",
		"  da mcp remove global stale.json",
	},
	ListShort: "List canonical MCP config files for a scope",
	ListExamples: []string{
		"  da mcp list",
		"  da mcp list billing-api",
	},
	ListArgsHint: "Optionally pass a project scope (or `global`) to inspect that MCP tree.",
	ShowShort:    "Show metadata for one MCP file under ~/.agents/mcp/",
	ShowArgsHint: "`scope` is `global` or a managed project name; `name` is the file (e.g. mcp.json) or stem (mcp).",
	RemoveShort:  "Remove an MCP file from ~/.agents/mcp/ (canonical storage only)",
	RemoveLong: `Deletes the file from managed MCP storage only (not repo links). After removal,
run da refresh or install for the relevant project so platform MCP
links stay consistent.`,
	RemoveArgsHint: canonicalRemoveArgsHint,
}

// SettingsResource owns the static `da settings` resource definition.
// MissingDirHint is intentionally nil so RunCanonicalList emits the
// generic fallback message ("No ~/.agents/settings/<scope>/ directory yet
// ..."), preserving the pre-refactor settings/list.go behavior verbatim.
var SettingsResource = CanonicalResourceDef{
	Kind:        "Settings",
	DirSegment:  "settings",
	SingularRem: "settings file",
	EnsureScope: platform.EnsureUnderSettingsScopeTree,
	EmptyHint: func(scope string) string {
		return "No settings files under ~/.agents/settings/" + scope + "/"
	},
	// MissingDirHint left nil — fallback message matches pre-refactor.
	Use:   "settings",
	Short: "Inspect and manage canonical ~/.agents/settings files",
	Long: `Commands for platform settings files stored under ~/.agents/settings/<scope>/.

Scopes are either global (~/.agents/settings/global/) or a managed project name
(~/.agents/settings/<project>/), matching da status.

Files include JSON/TOML/YAML configs (e.g. cursor.json, claude-code.json) and
cursorignore. These are wired by add, import, refresh, install, and remove.
Prefer editing canonical paths here, then run refresh or install.`,
	Examples: []string{
		"  da settings list",
		"  da settings list my-app",
		"  da settings show global cursor.json",
		"  da settings remove proj cursorignore",
	},
	ListShort: "List canonical settings files for a scope",
	ListExamples: []string{
		"  da settings list",
		"  da settings list billing-api",
	},
	ListArgsHint: "Optionally pass a project scope (or `global`) to inspect that settings tree.",
	ShowShort:    "Show metadata for one settings file under ~/.agents/settings/",
	ShowArgsHint: "`scope` is `global` or a managed project name; `name` is the file (e.g. cursor.json) or stem.",
	RemoveShort:  "Remove a settings file from ~/.agents/settings/ (canonical storage only)",
	RemoveLong: `Deletes the file from managed settings storage only (not repo links). After removal,
run da refresh or install for the relevant project so platform settings
links stay consistent.`,
	RemoveArgsHint: canonicalRemoveArgsHint,
}

// RulesResource owns the static `da rules` resource definition. Note the
// Kind is "Rule" (singular) because that is what the ui.Header prints —
// matching the pre-refactor rules/list.go literal verbatim.
var RulesResource = CanonicalResourceDef{
	Kind:        "Rule",
	DirSegment:  "rules",
	SingularRem: "rule file",
	EnsureScope: platform.EnsureUnderRulesScopeTree,
	EmptyHint: func(scope string) string {
		return "No rule files (.mdc/.md/.txt) under ~/.agents/rules/" + scope + "/"
	},
	MissingDirHint: func(scope string) string {
		return "No ~/.agents/rules/" + scope + "/ directory yet (no canonical rule files for this scope)."
	},
	Use:   "rules",
	Short: "Inspect and manage canonical ~/.agents/rules files",
	Long: `Commands for rule files stored under ~/.agents/rules/<scope>/.

Scopes are either global (~/.agents/rules/global/) or a managed project name
(~/.agents/rules/<project>/), matching da status.

These files are what add, import, refresh, install, and remove wire into
Cursor, Claude Code, Codex, and Copilot projections. Prefer editing canonical
paths here, then run refresh or install for the project — do not hand-edit
platform copies unless you know they are unmanaged.`,
	Examples: []string{
		"  da rules list",
		"  da rules list my-app",
		"  da rules show global rules.mdc",
		"  da rules remove global old-rule.mdc",
	},
	ListShort: "List canonical rule files for a scope",
	ListExamples: []string{
		"  da rules list",
		"  da rules list billing-api",
	},
	ListArgsHint: "Optionally pass a project scope (or `global`) to inspect that rules tree.",
	ShowShort:    "Show metadata for one rule file under ~/.agents/rules/",
	ShowArgsHint: "`scope` is `global` or a managed project name; `name` is the file (e.g. rules.mdc) or stem (rules).",
	RemoveShort:  "Remove a rule file from ~/.agents/rules/ (canonical storage only)",
	RemoveLong: `Deletes the file from managed rule storage only (not repo links). After removal,
run da refresh or install for the relevant project so platform rule
links stay consistent.`,
	RemoveArgsHint: canonicalRemoveArgsHint,
}
