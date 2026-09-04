package config

import (
	"fmt"
	"io"

	cfg "github.com/AGOrcha/dot-agents/internal/config"
	"github.com/spf13/cobra"
)

// CachePruneReport is the stable JSON shape emitted by
// `da config cache prune --json`. OK is false only when the prune itself failed;
// a dry run that would remove nothing still reports OK.
type CachePruneReport struct {
	OK bool `json:"ok"`
	// Root is the scanned cache root (~/.agents/cache/config).
	Root string `json:"root"`
	// Applied is true when entries were actually removed; false for the default
	// dry-run listing.
	Applied bool `json:"applied"`
	// Entries are the prunable entries — what WOULD be removed on a dry run, what
	// WAS removed with --apply.
	Entries []cfg.CacheEntry `json:"entries"`
	// Scanned is the total number of entries examined.
	Scanned int `json:"scanned"`
	// Bytes is the reclaimable (dry run) or reclaimed (--apply) size.
	Bytes int64 `json:"bytes"`
	// Projects are the project paths whose lockfiles supplied the live set.
	Projects []string `json:"projects"`
	// Skipped are registered projects carrying no lockfile (they pinned nothing).
	Skipped []string `json:"skipped,omitempty"`
}

// runCachePruneOptions captures one invocation's state.
type runCachePruneOptions struct {
	runContext
	// apply performs the removal; without it the command only lists.
	apply bool
	// projects is the project-path seam (tests inject a sandboxed set instead of
	// reading the real registry). Nil falls back to registeredProjectPaths.
	projects func() ([]string, error)
}

// registeredProjectPaths enumerates the machine-local path of every project in
// the registry (~/.agents/config.json + the machine-local binding table). A known
// but unbound project contributes no path — its checkout does not exist on this
// machine, so it cannot be consuming this cache from here.
var registeredProjectPaths = func() ([]string, error) {
	c, err := cfg.Load()
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, name := range c.ListProjects() {
		if path := c.GetProjectPath(name); path != "" {
			out = append(out, path)
		}
	}
	return out, nil
}

func newCacheCmd(deps Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Inspect and garbage-collect the shared config cache",
		Long: `Manage the shared content-addressed config cache
(~/.agents/cache/config), which holds the fetched bytes of every ` + "`extends`" + `
config layer and every source-qualified prompt file.`,
		Example: exampleBlock(
			"  da config cache prune",
			"  da config cache prune --json",
			"  da config cache prune --apply",
		),
	}
	cmd.AddCommand(newCachePruneCmd(deps))
	return cmd
}

func newCachePruneCmd(deps Deps) *cobra.Command {
	opts := &runCachePruneOptions{}
	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Remove config-cache entries no project lockfile references",
		Long: `Garbage-collect the shared config cache (~/.agents/cache/config).

Every resolve writes a layer's or prompt file's bytes under
<source-id>/<path>/<digest>/, so a source that moves forward leaves its previous
digest behind forever. prune removes what nothing can name: an entry is prunable
when NO registered project's lockfile references its digest.

The live set comes from the lockfiles of this repository plus every project in
the registry that is bound on this machine. A project whose lockfile cannot be
parsed FAILS the prune rather than shrinking the live set — fix or unregister it
first. A registered project with no lockfile at all is reported as skipped.

prune is a DRY RUN by default: it lists what it would remove and how much space
that would reclaim, and touches nothing. Pass --apply to actually delete.`,
		Example: exampleBlock(
			"  da config cache prune",
			"  da config cache prune --json",
			"  da config cache prune --apply",
		),
		Args: deps.ExactArgsWithHints(0, "`da config cache prune` takes no arguments; use --apply to delete and --json for machine-readable output."),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.bind(cmd, deps); err != nil {
				return err
			}
			return runCachePrune(opts, deps)
		},
	}
	cmd.Flags().BoolVar(&opts.apply, "apply", false, "delete the unreferenced entries (default: list only)")
	return cmd
}

// runCachePrune is the test-friendly entry point: it scans, optionally removes,
// renders, and returns a non-nil error only when the scan or removal failed.
func runCachePrune(opts *runCachePruneOptions, deps Deps) error {
	projects, err := opts.projectPaths()
	if err != nil {
		return deps.ErrorWithHints("config cache prune could not read the project registry: " + err.Error())
	}
	scan, err := cfg.ScanConfigCache(append(projects, opts.cwd))
	if err != nil {
		return deps.ErrorWithHints("config cache prune could not scan the config cache: "+err.Error(),
			"A project lockfile that cannot be parsed blocks the prune; fix it or unregister the project.",
		)
	}

	report := CachePruneReport{
		OK:       true,
		Root:     scan.Root,
		Entries:  scan.Prunable(),
		Scanned:  len(scan.Entries),
		Bytes:    scan.PrunableBytes(),
		Projects: scan.Projects,
		Skipped:  scan.Skipped,
	}
	// The global --dry-run flag also forces the preview, so `--apply --dry-run`
	// previews rather than deleting (the documented "preview mutations" contract
	// wins over the local opt-in).
	if opts.apply && !opts.dryRun {
		removed, bytes, err := cfg.PruneCacheEntries(scan.Root, report.Entries)
		if err != nil {
			return deps.ErrorWithHints(fmt.Sprintf("config cache prune removed %d entr(ies) then failed: %v", removed, err))
		}
		report.Applied = true
		report.Bytes = bytes
	}

	if opts.jsonOut {
		return writeJSON(opts.stdout, report)
	}
	printCachePruneHuman(opts.stdout, report)
	return nil
}

// projectPaths resolves the project set through the injected seam, defaulting to
// the real registry.
func (o *runCachePruneOptions) projectPaths() ([]string, error) {
	if o.projects != nil {
		return o.projects()
	}
	return registeredProjectPaths()
}

// printCachePruneHuman renders the prune outcome as a scannable entry list with a
// one-line summary.
func printCachePruneHuman(w io.Writer, report CachePruneReport) {
	fmt.Fprintln(w, cachePruneHeader(report))
	fmt.Fprintln(w)
	if len(report.Entries) == 0 {
		fmt.Fprintln(w, "  nothing to prune — every cached entry is referenced by a project lockfile")
	}
	for _, e := range report.Entries {
		fmt.Fprintf(w, "  %-10s %s/%s  %s  (%s)\n", cachePruneVerb(report.Applied), e.SourceID, e.UnitPath,
			abbrevSHA(e.Digest), humanBytes(e.Bytes))
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, cachePruneSummary(report))
	if len(report.Skipped) > 0 {
		fmt.Fprintf(w, "Skipped (no lockfile): %d project(s)\n", len(report.Skipped))
	}
}

// cachePruneHeader renders the one-line header, varying by dry-run vs apply.
func cachePruneHeader(report CachePruneReport) string {
	if report.Applied {
		return "Config cache prune (" + report.Root + "):"
	}
	return "Config cache prune --dry-run (" + report.Root + ") — nothing will be removed:"
}

// cachePruneVerb labels each entry row by what happened (or would happen) to it.
func cachePruneVerb(applied bool) string {
	if applied {
		return "removed"
	}
	return "would rm"
}

// cachePruneSummary renders the trailing summary line.
func cachePruneSummary(report CachePruneReport) string {
	if report.Applied {
		return fmt.Sprintf("Summary: removed %d of %d entr(ies), reclaimed %s — live set from %d project lock(s)",
			len(report.Entries), report.Scanned, humanBytes(report.Bytes), len(report.Projects))
	}
	return fmt.Sprintf("Summary: %d of %d entr(ies) prunable, %s reclaimable — re-run with --apply to delete (live set from %d project lock(s))",
		len(report.Entries), report.Scanned, humanBytes(report.Bytes), len(report.Projects))
}

// humanBytes formats a byte count for the human render with one decimal place
// above the KiB threshold.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for n/div >= unit && exp < 3 {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGT"[exp])
}
