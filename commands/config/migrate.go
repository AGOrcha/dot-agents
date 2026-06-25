package config

import (
	"fmt"
	"io"
	"strings"

	cfg "github.com/AGOrcha/dot-agents/internal/config"
	"github.com/spf13/cobra"
)

// MigrateReport is the stable JSON shape emitted by `da config migrate --json`.
// It mirrors cfg.MigrationResult's user-relevant fields so the machine-readable
// output is decoupled from the internal result struct.
type MigrateReport struct {
	OK          bool     `json:"ok"`
	Manifest    string   `json:"manifest"`
	Backup      string   `json:"backup,omitempty"`
	AlreadyV2   bool     `json:"already_v2"`
	FromVersion int      `json:"from_version"`
	ToVersion   int      `json:"to_version"`
	FoldedKeys  []string `json:"folded_keys,omitempty"`
	WroteFile   bool     `json:"wrote_file"`
	WroteBackup bool     `json:"wrote_backup"`
	DryRun      bool     `json:"dry_run,omitempty"`
}

// runMigrateOptions captures one invocation's state. The shared stdout/stderr/
// cwd/json/dry-run surface comes from the embedded runContext.
type runMigrateOptions struct {
	runContext
}

func newMigrateCmd(deps Deps) *cobra.Command {
	opts := &runMigrateOptions{}
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Rewrite a legacy v1 .agentsrc.json into the v2 schema (opt-in, backs up the original)",
		Long: `Rewrite this repository's legacy v1 .agentsrc.json into the equivalent v2
manifest. This is an explicit, opt-in migration: v1 manifests still load and the
existing deprecation warning is unchanged. The migrator simply lets you move a
repo to v2 on your own schedule during the deprecation soak window.

What it does:

  - Detects a legacy v1 shape (an old schema version, or the deprecated keys
    verifier_profiles / reviewer_profiles / app_type_verifier_map that are folded
    silently on load).
  - Backs up the ORIGINAL manifest to .agentsrc.json.v1.bak before writing.
  - Writes the equivalent v2 .agentsrc.json: the version is bumped and the legacy
    keys are folded into the unified stage_profiles / execution_profile model
    (the same fold the loader already performs — nothing is lost).
  - Idempotent: a clean v2 manifest is a no-op with a clear message.

It operates on the .agentsrc.json in the current repository, so it is
repo-agnostic — maintainers run it per-repo during the soak. NO v1 support is
removed: this is a convenience, not a forced cutover.

With the global --dry-run flag it previews the rewrite (and the planned backup)
without writing anything.`,
		Example: exampleBlock(
			"  da config migrate",
			"  da config migrate --dry-run",
			"  da config migrate --json",
		),
		Args: deps.ExactArgsWithHints(0, "`da config migrate` takes no arguments; it migrates the current repo's .agentsrc.json. Use --dry-run to preview and --json for machine-readable output."),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.bind(cmd, deps); err != nil {
				return err
			}
			return runMigrate(opts, deps)
		},
	}
	return cmd
}

// runMigrate is the test-friendly entry point. It drives the core migration in
// internal/config (which loads, folds, backs up, and rewrites), then renders the
// result as JSON or human output. A read/write failure surfaces as a hinted
// error so cobra maps it to a non-zero exit.
func runMigrate(opts *runMigrateOptions, deps Deps) error {
	res, err := cfg.MigrateAgentsRC(opts.cwd, opts.dryRun)
	if err != nil {
		return deps.ErrorWithHints("config migrate could not process "+cfg.AgentsRCFile+": "+err.Error(),
			"Confirm you are in a repository with a .agentsrc.json and that it is valid JSON.",
		)
	}

	report := newMigrateReport(res)
	if opts.jsonOut {
		return writeJSON(opts.stdout, report)
	}
	printMigrateHuman(opts.stdout, report)
	return nil
}

// newMigrateReport projects the internal migration result onto the stable
// JSON/report shape.
func newMigrateReport(res cfg.MigrationResult) MigrateReport {
	return MigrateReport{
		OK:          true,
		Manifest:    res.ManifestPath,
		Backup:      res.BackupPath,
		AlreadyV2:   res.AlreadyV2,
		FromVersion: res.FromVersion,
		ToVersion:   res.ToVersion,
		FoldedKeys:  res.FoldedKeys,
		WroteFile:   res.WroteFile,
		WroteBackup: res.WroteBackup,
		DryRun:      res.DryRun,
	}
}

// printMigrateHuman renders the migration outcome as a short, scannable block.
func printMigrateHuman(w io.Writer, r MigrateReport) {
	if r.AlreadyV2 {
		fmt.Fprintf(w, "%s is already v2 (version %d) — nothing to migrate.\n", r.Manifest, r.FromVersion)
		return
	}
	fmt.Fprintln(w, migrateHeaderLine(r))
	fmt.Fprintf(w, "  schema version %d -> %d\n", r.FromVersion, r.ToVersion)
	if len(r.FoldedKeys) > 0 {
		fmt.Fprintf(w, "  folded legacy keys: %s\n", strings.Join(r.FoldedKeys, ", "))
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, migrateSummaryLine(r))
}

// migrateHeaderLine renders the one-line header, varying by dry-run vs apply.
func migrateHeaderLine(r MigrateReport) string {
	if r.DryRun {
		return "Config migrate --dry-run — would rewrite " + r.Manifest + " as v2:"
	}
	return "Config migrate — rewrote " + r.Manifest + " as v2:"
}

// migrateSummaryLine renders the trailing summary, varying by dry-run.
func migrateSummaryLine(r MigrateReport) string {
	if r.DryRun {
		return "Summary: no changes written (dry run); original would be backed up to " + r.Backup
	}
	return "Summary: original backed up to " + r.Backup
}
