package kg

import (
	"time"

	"github.com/AGOrcha/dot-agents/commands/internal/cmdutil"
	"github.com/spf13/cobra"
)

const (
	repoRootHelpText = "Repository root (auto-detected from git)"
	gitDiffBaseHelp  = "Git diff base (default: HEAD~1)"
)

func NewKGCmd(deps Deps) *cobra.Command {
	// Wire the production kgIO collaborator into deps so every handler that
	// derives its io from kgIOFrom(deps) sees stdKGIO{} by default. Tests
	// that need to fault-inject a seam construct their own Deps with a
	// fakeKGIO assigned to IO directly before invoking the handler.
	if deps.IO == nil {
		deps.IO = stdKGIO{}
	}
	kgCmd := &cobra.Command{
		Use:   "kg",
		Short: "Manage the local knowledge graph",
		Long: `Creates, queries, and maintains the local knowledge graph used by dot-agents
for structured project memory, bridge queries, and code-to-note context.`,
		Example: deps.ExampleBlock(
			"  da kg setup",
			"  da kg health",
			"  da kg query --intent repo_context \"workflow status\"",
			"  da kg bridge health",
		),
	}

	kgSetupCmd := &cobra.Command{
		Use:   "setup",
		Short: "Initialize the knowledge graph at KG_HOME",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runKGSetup(kgIOFrom(deps))
		},
	}

	kgHealthCmd := &cobra.Command{
		Use:   "health",
		Short: "Show knowledge graph health",
		Example: deps.ExampleBlock(
			"  da kg health",
			"  da --json kg health",
		),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runKGHealth(deps, cmd)
		},
	}

	kgServeCmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the MCP server (stdio transport, JSON-RPC 2.0)",
		RunE:  runKGServe,
	}

	kgIngestCmd := &cobra.Command{
		Use:   "ingest [file]",
		Short: "Ingest a raw source into the knowledge graph",
		Example: deps.ExampleBlock(
			"  da kg ingest notes/design-review.md --type markdown",
			"  da kg ingest --all",
		),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runKGIngest(deps, cmd, args)
		},
	}
	kgIngestCmd.Flags().Bool("all", false, "Process all pending sources in the inbox")
	kgIngestCmd.Flags().String("title", "", "Override source title")
	cmdutil.RegisterEnumFlag(kgIngestCmd, kgIngestTypeEnum)
	kgIngestCmd.Flags().Bool("dry-run", false, "Show what would be created without writing")

	kgQueueCmd := &cobra.Command{
		Use:   "queue",
		Short: "List pending sources in the inbox",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runKGQueue(deps)
		},
	}

	kgQueryCmd := &cobra.Command{
		Use:   "query [query string]",
		Short: "Query the knowledge graph by intent",
		Example: deps.ExampleBlock(
			"  da kg query --intent decision_lookup \"graph backend\"",
			"  da kg query --intent repo_context \"workflow status\" --limit 5",
			"  da --json kg query --intent contradictions \"resource plan\"",
		),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runKGQuery(deps, cmd, args)
		},
	}
	cmdutil.RegisterEnumFlag(kgQueryCmd, kgQueryIntentEnum)
	kgQueryCmd.Flags().Int("limit", 10, "Max results to return")
	kgQueryCmd.Flags().String("scope", "", "Narrow results to notes under this repo path, note id prefix, or source id")

	kgLintCmd := &cobra.Command{
		Use:   "lint",
		Short: "Check graph integrity and knowledge quality",
		Example: deps.ExampleBlock(
			"  da kg lint",
			"  da kg lint --check broken_links",
		),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runKGLint(deps, cmd, args)
		},
	}
	cmdutil.RegisterEnumFlag(kgLintCmd, kgLintCheckEnum)

	kgMaintainCmd := &cobra.Command{
		Use:   "maintain",
		Short: "Graph maintenance operations",
	}

	kgReweaveCmd := &cobra.Command{
		Use:   "reweave",
		Short: "Repair broken links and add missing source_ref links",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runKGReweave(kgIOFrom(deps), kgHome())
		},
	}

	kgMarkStaleCmd := &cobra.Command{
		Use:   "mark-stale",
		Short: "Mark notes not updated beyond threshold as stale",
		RunE: func(cmd *cobra.Command, args []string) error {
			days, _ := cmd.Flags().GetInt("days")
			return runKGMarkStale(kgIOFrom(deps), kgHome(), time.Duration(days)*24*time.Hour)
		},
	}
	kgMarkStaleCmd.Flags().Int("days", 90, "Age threshold in days (default 90)")

	kgCompactCmd := &cobra.Command{
		Use:   "compact",
		Short: "Archive superseded and archived notes",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runKGCompact(kgIOFrom(deps), kgHome())
		},
	}

	kgMaintainCmd.AddCommand(kgReweaveCmd, kgMarkStaleCmd, kgCompactCmd)

	// bridge subcommand tree
	kgBridgeCmd := &cobra.Command{
		Use:   "bridge",
		Short: "Query and inspect the KG bridge surface",
	}
	kgBridgeQueryCmd := &cobra.Command{
		Use:   "query [query string]",
		Short: "Execute a bridge intent query",
		Example: deps.ExampleBlock(
			"  da kg bridge query --intent impact_radius \"internal/platform\"",
			"  da kg bridge query --intent tests_for \"NewRootCommand\"",
		),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runKGBridgeQuery(deps, cmd, args)
		},
	}
	cmdutil.RegisterEnumFlag(kgBridgeQueryCmd, kgBridgeIntentEnum)

	kgBridgeHealthCmd := &cobra.Command{
		Use:   "health",
		Short: "Show adapter availability and health",
		Example: deps.ExampleBlock(
			"  da kg bridge health",
		),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runKGBridgeHealth(deps, cmd, args)
		},
	}
	kgBridgeMappingCmd := &cobra.Command{
		Use:   "mapping",
		Short: "Show bridge intent to KG intent mapping",
		Example: deps.ExampleBlock(
			"  da kg bridge mapping",
		),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runKGBridgeMapping(deps, cmd, args)
		},
	}
	kgBridgeCmd.AddCommand(kgBridgeQueryCmd, kgBridgeHealthCmd, kgBridgeMappingCmd)

	// sync subcommand (Phase 6C)
	kgSyncCmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync graph via git pull + lint (use --push to push)",
		RunE:  runKGSync,
	}
	kgSyncCmd.Flags().Bool("push", false, "Push current state instead of pulling")

	// Phase D: warm layer sync
	kgWarmCmd := &cobra.Command{
		Use:   "warm",
		Short: "Sync hot filesystem notes into the warm SQLite layer",
		Example: deps.ExampleBlock(
			"  da kg warm",
			"  da kg warm --type decision",
			"  da kg warm --include-code",
		),
		RunE: runKGWarm,
	}
	cmdutil.RegisterEnumFlag(kgWarmCmd, kgWarmTypeEnum)
	kgWarmCmd.Flags().Bool("include-code", false, "Also import CRG code nodes and edges into the warm store (requires 'kg build' to have run)")

	kgWarmStatsCmd := &cobra.Command{
		Use:   "stats",
		Short: "Show warm layer statistics",
		RunE:  runKGWarmStats,
	}
	kgWarmCmd.AddCommand(kgWarmStatsCmd)

	// Phase D: note→symbol links
	kgLinkCmd := &cobra.Command{
		Use:   "link",
		Short: "Manage note→code symbol cross-references",
	}
	kgLinkAddCmd := &cobra.Command{
		Use:   "add <note-id> <qualified-name>",
		Short: "Link a knowledge note to a code symbol",
		Example: deps.ExampleBlock(
			"  da kg link add note-042 internal/platform.All --kind documents",
			"  da kg link add note-042 commands.NewRootCommand --kind mentions",
		),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runKGLinkAdd(deps, cmd, args)
		},
	}
	cmdutil.RegisterEnumFlag(kgLinkAddCmd, kgLinkKindEnum)

	kgLinkListCmd := &cobra.Command{
		Use:   "list <note-id>",
		Short: "List all symbol links for a note",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runKGLinkList(deps, cmd, args)
		},
	}
	kgLinkRemoveCmd := &cobra.Command{
		Use:   "remove <link-id>",
		Short: "Remove a note→symbol link by ID",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runKGLinkRemove(deps, cmd, args)
		},
	}
	kgLinkImportCmd := &cobra.Command{
		Use:   "import <manifest-file>",
		Short: "Bulk-apply note→symbol links from a manifest (one `note-id qualified-name [kind]` per line)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runKGLinkImport(deps, cmd, args)
		},
	}
	kgLinkCmd.AddCommand(kgLinkAddCmd, kgLinkListCmd, kgLinkRemoveCmd, kgLinkImportCmd)

	// Phase B: CRG code-graph subcommands
	kgBuildCmd := &cobra.Command{
		Use:   "build",
		Short: "Full code graph build (re-parse all files via code-review-graph)",
		RunE:  runKGBuild,
	}
	kgBuildCmd.Flags().String("repo", "", repoRootHelpText)
	kgBuildCmd.Flags().Bool("skip-flows", false, "Skip flow/community detection (faster)")
	kgBuildCmd.Flags().Bool("skip-postprocess", false, "Skip all post-processing (raw parse only)")

	kgUpdateCmd := &cobra.Command{
		Use:   "update",
		Short: "Incremental code graph update (changed files only)",
		RunE:  runKGUpdate,
	}
	kgUpdateCmd.Flags().String("repo", "", repoRootHelpText)
	kgUpdateCmd.Flags().String("base", "", gitDiffBaseHelp)
	kgUpdateCmd.Flags().Bool("skip-flows", false, "Skip flow/community detection")
	kgUpdateCmd.Flags().Bool("skip-postprocess", false, "Skip all post-processing")

	kgCodeStatusCmd := &cobra.Command{
		Use:   "code-status",
		Short: "Show code graph stats (nodes, edges, languages)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runKGCodeStatus(deps, cmd, args)
		},
	}
	kgCodeStatusCmd.Flags().String("repo", "", repoRootHelpText)

	kgChangesCmd := &cobra.Command{
		Use:   "changes",
		Short: "Detect change impact in the current diff",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runKGChanges(deps, cmd, args)
		},
	}
	kgChangesCmd.Flags().String("repo", "", repoRootHelpText)
	kgChangesCmd.Flags().String("base", "", gitDiffBaseHelp)
	kgChangesCmd.Flags().Bool("brief", false, "Show brief summary only")
	kgChangesCmd.Flags().Bool("require-graph", false, "Return non-zero exit if graph is not ready (unbuilt or locked)")

	// Phase C: impact, flows, communities, postprocess
	kgImpactCmd := &cobra.Command{
		Use:   "impact [file...]",
		Short: "Show blast radius for given files (or current diff)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runKGImpact(deps, cmd, args)
		},
	}
	kgImpactCmd.Flags().String("repo", "", repoRootHelpText)
	kgImpactCmd.Flags().String("base", "", gitDiffBaseHelp)
	kgImpactCmd.Flags().Int("depth", 2, "Max hop depth for impact traversal")
	kgImpactCmd.Flags().Int("limit", 50, "Max impacted nodes to return")
	kgImpactCmd.Flags().Bool("require-graph", false, "Return non-zero exit if graph is not ready (unbuilt or locked)")

	kgFlowsCmd := &cobra.Command{
		Use:   "flows",
		Short: "List detected execution flows",
		Long: `List detected execution flows from the code graph.

Note: flow step chains and entry points are not currently populated by the
underlying graph engine. Results show highly-connected functions sorted by
criticality score, not full execution paths. Use 'kg impact' for blast-radius
analysis instead.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runKGFlows(deps, cmd, args)
		},
	}
	kgFlowsCmd.Flags().String("repo", "", repoRootHelpText)
	kgFlowsCmd.Flags().Int("limit", 20, "Max flows to show")
	cmdutil.RegisterEnumFlag(kgFlowsCmd, kgFlowSortEnum)

	kgCommunitiesCmd := &cobra.Command{
		Use:   "communities",
		Short: "List detected code communities",
		Example: deps.ExampleBlock(
			"  da kg communities --sort cohesion --min-size 5",
		),
		Long: `List detected code communities from the code graph.

Community names, sizes, and dominant language are reliable. Member lists are
not currently populated (members field is always empty) — use 'kg impact' to
analyze specific files within a community. Results include all indexed languages;
third-party dependency directories (e.g. node_modules) may appear prominently
when sorting by size.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runKGCommunities(deps, cmd, args)
		},
	}
	kgCommunitiesCmd.Flags().String("repo", "", repoRootHelpText)
	kgCommunitiesCmd.Flags().Int("min-size", 0, "Only show communities with at least this many members")
	cmdutil.RegisterEnumFlag(kgCommunitiesCmd, kgCommunitySortEnum)

	kgPostprocessCmd := &cobra.Command{
		Use:   "postprocess",
		Short: "Rebuild flows, communities, and FTS index",
		Long: `Rebuild derived graph data: execution flows, code communities, and the
full-text search index.

This command runs automatically as part of 'kg build' and 'kg update'. Run it
manually only to repair stale derived data without rebuilding the full graph.`,
		RunE: runKGPostprocess,
	}
	kgPostprocessCmd.Flags().String("repo", "", repoRootHelpText)
	kgPostprocessCmd.Flags().Bool("no-flows", false, "Skip flow detection")
	kgPostprocessCmd.Flags().Bool("no-communities", false, "Skip community detection")
	kgPostprocessCmd.Flags().Bool("no-fts", false, "Skip FTS rebuild")

	kgCmd.AddCommand(
		kgSetupCmd, kgHealthCmd, kgServeCmd, kgIngestCmd, kgQueueCmd, kgQueryCmd,
		kgLintCmd, kgMaintainCmd, kgBridgeCmd, kgSyncCmd, kgWarmCmd, kgLinkCmd,
		kgBuildCmd, kgUpdateCmd, kgCodeStatusCmd, kgChangesCmd,
		kgImpactCmd, kgFlowsCmd, kgCommunitiesCmd, kgPostprocessCmd,
		newLockfileCmd(deps),
	)
	return kgCmd
}
