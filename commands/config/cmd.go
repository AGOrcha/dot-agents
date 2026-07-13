// Package config implements the `da config` command subtree.
//
// Per spec config-distribution-model §10 and proposal
// `.agents/proposals/config-explain-live-surface.md`, this subtree is the
// operator-facing surface for inspecting the effective configuration of a
// repository — what value won, which layer set it, and how to extract one
// specific field for scripts.
//
// The first commands in this subtree are `da config explain` and
// `da config verify`. The remaining siblings (`sync`, `lint`) are scoped to
// other tasks (config-v2-migration/p4c).
//
// The subtree intentionally consumes a minimal snapshot view of the manifest.
// Once the layered resolver from config-v2-migration/p1 / p1b lands in
// `internal/config/snapshot.go`, the snapshot helpers in this package are
// expected to delegate to it without changing the command surface.
package config

import (
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// Deps carries UX helpers from the root `commands` package without an import
// cycle (the same shape used by other subtrees like `commands/agents` and
// `commands/hooks`).
type Deps struct {
	ErrorWithHints        func(message string, hints ...string) error
	UsageError            func(message string, hints ...string) error
	MaximumNArgsWithHints func(n int, hints ...string) cobra.PositionalArgs
	ExactArgsWithHints    func(n int, hints ...string) cobra.PositionalArgs
	// JSON reports the resolved global `--json` flag, so `da config` honors the
	// same persistent flag as every other command (status, workflow, kg) rather
	// than defining its own local `--json`. Nil is treated as false.
	JSON func() bool
	// DryRun reports the resolved global `--dry-run` flag. Only the mutating
	// `da config sync` consumes it (explain/verify/lint are read-only); sync
	// short-circuits before the force re-resolve + lock rewrite when it is set,
	// honoring the documented "preview mutations without applying" contract.
	// Nil is treated as false, matching the mcp/settings DryRun wiring.
	DryRun func() bool
}

// jsonFlag returns the resolved global --json value, tolerating a nil getter
// (tests that build Deps directly, or callers that don't wire it).
func (d Deps) jsonFlag() bool { return d.JSON != nil && d.JSON() }

// dryRunFlag returns the resolved global --dry-run value, tolerating a nil
// getter (tests that build Deps directly, or callers that don't wire it).
func (d Deps) dryRunFlag() bool { return d.DryRun != nil && d.DryRun() }

// runContext is the common stdout/stderr/json/cwd surface every `da config`
// subcommand's RunE fills from the cobra command + Deps. Embedding it keeps the
// per-command option structs and their RunE wiring identical-free.
type runContext struct {
	stdout  io.Writer
	stderr  io.Writer
	jsonOut bool
	dryRun  bool
	cwd     string
}

// getwd resolves the working directory; a package var so tests can exercise the
// resolution-failure branch of bind without manipulating the real process cwd.
var getwd = os.Getwd

// bind fills the run context from the live command and Deps: stdout/stderr from
// the command's streams, jsonOut from the global --json, and cwd from os.Getwd
// when not already set (tests inject cwd directly). It returns a hinted error
// only when the working directory cannot be resolved.
func (rc *runContext) bind(cmd *cobra.Command, deps Deps) error {
	rc.stdout = cmd.OutOrStdout()
	rc.stderr = cmd.ErrOrStderr()
	rc.jsonOut = deps.jsonFlag()
	rc.dryRun = deps.dryRunFlag()
	if rc.cwd == "" {
		cwd, err := getwd()
		if err != nil {
			return deps.ErrorWithHints("could not resolve current directory", err.Error())
		}
		rc.cwd = cwd
	}
	return nil
}

// NewConfigCmd builds the `da config` command tree.
func NewConfigCmd(deps Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect effective configuration for this repository",
		Long: `Inspect the effective .agentsrc.json configuration for this repository,
including per-field provenance (which layer set the winning value).

This subtree is the operator-facing introspection surface for the layered
config model documented in docs/CONFIG_DISTRIBUTION_MODEL.md (spec id
config-distribution-model). It is intentionally separate from ` + "`da explain`" + `,
which prints human concept documentation rather than live repo state.`,
		Example: exampleBlock(
			"  da config explain",
			"  da config explain repo_id",
			"  da config explain skills --value-only",
			"  da config explain --all --json",
			"  da config explain --flags",
			"  da config sync",
			"  da config sync --layer acme:org/base.json",
			"  da config lint",
			"  da config lint --json",
			"  da config verify",
			"  da config relevance --filter topology --app-type go-cli",
			"  da config migrate",
			"  da config migrate --dry-run",
		),
	}
	cmd.AddCommand(newExplainCmd(deps))
	cmd.AddCommand(newSyncCmd(deps))
	cmd.AddCommand(newLintCmd(deps))
	cmd.AddCommand(newVerifyCmd(deps))
	cmd.AddCommand(newRelevanceCmd(deps))
	cmd.AddCommand(newMigrateCmd(deps))
	return cmd
}

func exampleBlock(lines ...string) string {
	return strings.Join(lines, "\n")
}
