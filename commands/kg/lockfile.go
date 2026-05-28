package kg

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"time"

	"github.com/NikashPrakash/dot-agents/internal/adapters/builtin/none"
	"github.com/NikashPrakash/dot-agents/internal/kg/lockfile"
	"github.com/NikashPrakash/dot-agents/internal/kg/registry"
	"github.com/spf13/cobra"
)

// lockfilePath returns the adapter lockfile location under KG_HOME.
func lockfilePath() string {
	return filepath.Join(kgHome(), "self", "adapters.lock.yaml")
}

// builtinRegistry returns a registry with every adapter that ships inside
// `da` registered. The `none` adapter is the only built-in for this task;
// later tasks register their adapters here too.
func builtinRegistry() (*registry.Registry, error) {
	reg := registry.New()
	if err := none.Register(reg); err != nil {
		return nil, err
	}
	return reg, nil
}

// resolveBackend resolves a graph_backend ref against the built-in registry.
// It is the command-path entry the profile-selection wiring calls to turn a
// `graph_backend: dotagents-builtin:graph/none@^1.0` ref into an Adapter.
func resolveBackend(ref string) (registry.Adapter, error) {
	reg, err := builtinRegistry()
	if err != nil {
		return nil, err
	}
	return reg.Resolve(ref)
}

// nowFunc is a seam for deterministic timestamps in tests.
var nowFunc = time.Now

// lockfileShowResult is the JSON shape for `da kg lockfile show`.
type lockfileShowResult struct {
	Adapters map[string]*lockfile.Adapter `json:"adapters"`
}

func newLockfileCmd(deps Deps) *cobra.Command {
	lockfileCmd := &cobra.Command{
		Use:   "lockfile",
		Short: "Inspect and reconcile adapter lockfile state",
		Long: `Show per-adapter lockfile state and run fail-closed reconciliation of
materialized-view state against on-disk tables (graph-backend-adapter-contract §10.1).`,
	}

	showCmd := &cobra.Command{
		Use:   "show",
		Short: "Print per-adapter lockfile state including all views",
		RunE: func(cmd *cobra.Command, args []string) error {
			adapter, _ := cmd.Flags().GetString("adapter")
			return runLockfileShow(kgIOFrom(deps), cmd.OutOrStdout(), lockfilePath(), adapter, deps.Flags.JSON)
		},
	}
	showCmd.Flags().String("adapter", "", "Only show this adapter")

	reconcileCmd := &cobra.Command{
		Use:   "reconcile",
		Short: "Force a fail-closed reconciliation pass over view state",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLockfileReconcile(cmd.OutOrStdout(), lockfilePath(), deps.Flags.JSON)
		},
	}

	lockfileCmd.AddCommand(showCmd, reconcileCmd)
	return lockfileCmd
}

// runLockfileShow loads the lockfile and prints adapter state, optionally
// filtered to one adapter, as text or JSON.
func runLockfileShow(_ kgIO, out io.Writer, path, adapter string, asJSON bool) error {
	lf, err := lockfile.Load(path)
	if err != nil {
		return err
	}
	if adapter != "" {
		ad, ok := lf.Adapters[adapter]
		if !ok {
			return fmt.Errorf("lockfile: adapter %q not activated", adapter)
		}
		lf = &lockfile.Lockfile{Adapters: map[string]*lockfile.Adapter{adapter: ad}}
	}
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(lockfileShowResult{Adapters: lf.Adapters})
	}
	names := lf.AdapterNames()
	if len(names) == 0 {
		fmt.Fprintln(out, "No adapters activated.")
		return nil
	}
	for _, name := range names {
		ad := lf.Adapters[name]
		fmt.Fprintf(out, "%s\n", name)
		fmt.Fprintf(out, "  source_digest: %s\n", ad.SourceDigest)
		fmt.Fprintf(out, "  schema_digest: %s\n", ad.SchemaDigest)
		fmt.Fprintf(out, "  activated_at:  %s\n", ad.ActivatedAt)
		viewNames := make([]string, 0, len(ad.MaterializedViews))
		for vn := range ad.MaterializedViews {
			viewNames = append(viewNames, vn)
		}
		sort.Strings(viewNames)
		for _, vn := range viewNames {
			fmt.Fprintf(out, "  view %s: %s\n", vn, ad.MaterializedViews[vn].ViewStatus)
		}
	}
	return nil
}

// reconcileResult is the JSON shape for `da kg lockfile reconcile`.
type reconcileResult struct {
	Changes []lockfile.Inconsistency `json:"changes"`
}

// runLockfileReconcile loads the lockfile, runs the §10.1.3 reconciliation
// pass, persists any state changes atomically, and reports the changes. The
// `none` adapter declares no views, so a none-only lockfile reconciles to a
// no-op — which is exactly the end-to-end proof this task requires.
func runLockfileReconcile(out io.Writer, path string, asJSON bool) error {
	lf, err := lockfile.Load(path)
	if err != nil {
		return err
	}
	// The `none` adapter has no materialized views and no on-disk view
	// tables, so reconciliation uses the conservative nil presence func.
	changes := lf.Reconcile(nil, nowFunc())
	if len(changes) > 0 {
		if err := lockfile.Save(path, lf); err != nil {
			return err
		}
	}
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(reconcileResult{Changes: changes})
	}
	if len(changes) == 0 {
		fmt.Fprintln(out, "Reconcile: no changes; lockfile consistent.")
		return nil
	}
	fmt.Fprintf(out, "Reconcile: %d view(s) updated:\n", len(changes))
	for _, c := range changes {
		fmt.Fprintf(out, "  %s/%s: %s → %s (%s)\n", c.Adapter, c.View, c.From, c.To, c.Reason)
	}
	return nil
}
