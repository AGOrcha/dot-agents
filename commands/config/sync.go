package config

import (
	"fmt"
	"io"
	"sort"

	"github.com/AGOrcha/dot-agents/internal/agentslock"
	cfg "github.com/AGOrcha/dot-agents/internal/config"
	"github.com/spf13/cobra"
)

// SyncedLayer is one declared `extends` layer's outcome in a sync report: the
// ref, its source, the SHA + fetch timestamp recorded in the lockfile after the
// re-resolve, and whether this run actually targeted it. When `--layer` scopes
// the run to a single ref, the other declared layers report Targeted=false with
// a Note explaining they were left to their existing lock state.
type SyncedLayer struct {
	Ref        string `json:"ref"`
	SourceID   string `json:"source_id,omitempty"`
	SourceType string `json:"source_type,omitempty"`
	SHA        string `json:"sha,omitempty"`
	FetchedAt  string `json:"fetched_at,omitempty"`
	Targeted   bool   `json:"targeted"`
	Note       string `json:"note,omitempty"`
}

// SyncReport is the stable JSON shape emitted by `da config sync --json`. OK is
// false only when the re-resolve itself failed (a fetch/transport/schema error
// on a non-optional layer); a project with no declared layers still re-resolves
// the local stack and reports OK with an empty Layers list.
type SyncReport struct {
	OK       bool          `json:"ok"`
	Lockfile string        `json:"lockfile"`
	Layer    string        `json:"layer,omitempty"` // the --layer scope, if any
	Layers   []SyncedLayer `json:"layers"`
}

// forceResolver is the minimal force-re-resolve surface `da config sync` drives.
// *cfg.LayeredResolver (built with WithRefresh(true)) satisfies it; tests inject
// a resolver pointed at a temp user-local manifest with a fixed clock so no run
// touches the network. Resolve rewrites the lock — that is the whole point of
// sync.
type forceResolver interface {
	Resolve(projectPath string) (*cfg.Snapshot, error)
}

// runSyncOptions captures one invocation's state. The shared stdout/stderr/
// cwd/json surface comes from the embedded runContext; the layer scope and the
// resolver factory are injected so the run path is table-drivable without cobra
// or the network.
type runSyncOptions struct {
	runContext
	layer string // optional source-id:path scope; empty syncs all
	// newResolver builds the force-refresh resolver. Nil falls back to a default
	// cfg.NewLayeredResolver().WithRefresh(true); tests inject a resolver wired to
	// a temp user-local path and a fixed clock.
	newResolver func() forceResolver
}

func newSyncCmd(deps Deps) *cobra.Command {
	opts := &runSyncOptions{}
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Re-fetch config layers and rewrite the lock (explicit upstream re-check)",
		Long: `Re-fetch all declared config layers regardless of TTL, re-run pass 1
resolution, and rewrite the config section of .agentsrc.lock (spec
config-distribution-model §13.1, §14).

This is the explicit upstream re-check (the uv ` + "`--upgrade`" + ` analog): it forces
every source's effective cache key to revalidate so a stale cache cannot be
served, then re-resolves the full layer stack and updates each layer's resolved
SHA and fetch timestamp in the lock.

It is intentionally distinct from ` + "`da refresh`" + `, which re-projects local outputs
and only re-resolves local scopes when the lock is already stale. Sync always
re-checks upstream.

  --layer source-id:path  scope the report to a single declared extends layer
                          (the full stack is still re-resolved so the lock stays
                          internally consistent; only the named layer is reported
                          as targeted).`,
		Example: exampleBlock(
			"  da config sync",
			"  da config sync --layer acme:org/base.json",
			"  da config sync --json",
		),
		Args: deps.ExactArgsWithHints(0, "`da config sync` takes no arguments; use --layer to scope and --json for machine-readable output."),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.bind(cmd, deps); err != nil {
				return err
			}
			return runSync(opts, deps)
		},
	}
	cmd.Flags().StringVar(&opts.layer, "layer", "", "filter the sync report to one declared layer (source-id:path); the full stack is still re-resolved")
	return cmd
}

// runSync is the test-friendly entry point. It validates the optional --layer
// scope, force-re-resolves through the resolver seam (which rewrites the lock),
// reads the resulting lock back into a report, renders it (JSON or human), and
// returns a non-nil error iff the re-resolve failed so cobra maps it to a
// non-zero exit.
func runSync(opts *runSyncOptions, deps Deps) error {
	// Validate the --layer scope against the declared extends BEFORE touching the
	// network: a typo'd ref should fail fast, not after a full re-fetch.
	if opts.layer != "" {
		if err := validateLayerScope(opts.cwd, opts.layer); err != nil {
			return deps.ErrorWithHints("config sync --layer "+opts.layer+": "+err.Error(),
				"Run `da config explain` to see the declared `extends` layers.",
			)
		}
	}

	resolver := opts.resolver()
	if _, err := resolver.Resolve(opts.cwd); err != nil {
		return deps.ErrorWithHints("config sync could not re-resolve layers: "+err.Error(),
			"Check the source URLs/paths in .agentsrc.json and network access, then retry.",
		)
	}

	report, err := buildSyncReport(opts.cwd, opts.layer)
	if err != nil {
		return deps.ErrorWithHints("config sync re-resolved but could not read back the lock: " + err.Error())
	}

	if opts.jsonOut {
		return writeJSON(opts.stdout, report)
	}
	printSyncHuman(opts.stdout, report)
	return nil
}

// resolver returns the configured force-refresh resolver, defaulting to a real
// LayeredResolver with WithRefresh(true) — the seam the spec mandates for the
// explicit upstream re-check — when the test factory is unset.
func (o runSyncOptions) resolver() forceResolver {
	if o.newResolver != nil {
		return o.newResolver()
	}
	return cfg.NewLayeredResolver().WithRefresh(true)
}

// validateLayerScope confirms a --layer ref parses and names a layer actually
// declared in the project's `extends`, so an unknown ref fails before any fetch.
func validateLayerScope(projectPath, layer string) error {
	if _, err := cfg.ParseLayerRef(layer); err != nil {
		return err
	}
	rc, err := cfg.LoadAgentsRC(projectPath)
	if err != nil {
		return fmt.Errorf("reading %s: %w", cfg.AgentsRCFile, err)
	}
	for _, ext := range rc.Extends {
		if ext.Ref == layer {
			return nil
		}
	}
	return fmt.Errorf("ref is not declared in `extends`")
}

// buildSyncReport reads the post-resolve lock and the declared layer set into a
// stable report. The lock's config section carries the freshly-written SHA and
// fetch timestamp per ref; VerifyLayerLocks supplies the per-ref source id/type
// for the human render. When layerScope is set, only that ref is marked Targeted
// and the rest carry an explanatory note.
func buildSyncReport(projectPath, layerScope string) (SyncReport, error) {
	report := SyncReport{
		OK:       true,
		Lockfile: cfg.AgentsLockPath(projectPath),
		Layer:    layerScope,
		Layers:   []SyncedLayer{},
	}

	locked, err := readLockedConfigSection(projectPath)
	if err != nil {
		return SyncReport{}, err
	}

	// VerifyLayerLocks enumerates the declared extends with their source id/type
	// (no fetch). It is the authoritative declared-layer list for the report.
	statuses, err := cfg.VerifyLayerLocks(projectPath)
	if err != nil {
		return SyncReport{}, err
	}

	for _, st := range statuses {
		line := SyncedLayer{
			Ref:        st.Ref,
			SourceID:   st.SourceID,
			SourceType: st.SourceType,
		}
		if lock, ok := locked[st.Ref]; ok {
			line.SHA = lock.ResolvedSHA
			line.FetchedAt = lock.FetchedAt
		}
		if layerScope == "" || layerScope == st.Ref {
			line.Targeted = true
		} else {
			line.Note = "not targeted by --layer " + layerScope
		}
		report.Layers = append(report.Layers, line)
	}

	sort.Slice(report.Layers, func(i, j int) bool {
		return report.Layers[i].Ref < report.Layers[j].Ref
	})
	return report, nil
}

// readLockedConfigSection reads the "config" section of .agentsrc.lock through
// the public agentslock surface, returning an empty map when the file or section
// is absent. This is the report's source of freshly-written SHAs + timestamps.
func readLockedConfigSection(projectPath string) (map[string]cfg.LockedLayer, error) {
	lf, err := agentslock.Open(cfg.AgentsLockPath(projectPath))
	if err != nil {
		return nil, err
	}
	locked := map[string]cfg.LockedLayer{}
	if _, err := lf.Section(cfg.LockSectionConfig, &locked); err != nil {
		return nil, err
	}
	return locked, nil
}

// printSyncHuman renders the sync outcome as a scannable per-layer list with a
// one-line summary.
func printSyncHuman(w io.Writer, report SyncReport) {
	if report.Layer != "" {
		fmt.Fprintf(w, "Config sync (layer %s):\n", report.Layer)
	} else {
		fmt.Fprintln(w, "Config sync (all layers re-fetched):")
	}
	fmt.Fprintln(w)

	if len(report.Layers) == 0 {
		fmt.Fprintln(w, "  no external layers declared; local stack re-resolved")
		fmt.Fprintf(w, "\nLockfile: %s\n", report.Lockfile)
		return
	}

	targeted := 0
	for _, l := range report.Layers {
		mark := "  -"
		if l.Targeted {
			mark = "  *"
			targeted++
		}
		line := fmt.Sprintf("%s %-28s %s", mark, l.Ref, abbrevSHA(l.SHA))
		if l.FetchedAt != "" {
			line += " @ " + l.FetchedAt
		}
		if l.Note != "" {
			line += "  (" + l.Note + ")"
		}
		fmt.Fprintln(w, line)
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Summary: %d of %d layer(s) targeted — lock rewritten at %s\n",
		targeted, len(report.Layers), report.Lockfile)
}
