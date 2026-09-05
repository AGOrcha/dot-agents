package hooks

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/fsops"
	"github.com/AGOrcha/dot-agents/internal/platform"
	"github.com/AGOrcha/dot-agents/internal/ui"
	"github.com/spf13/cobra"
)

// hooksPruneSkip pairs a detected candidate with the reason it must not be
// deleted even though it was flagged.
type hooksPruneSkip struct {
	candidate platform.ImportArtifactCandidate
	reason    string
}

func newHooksPruneCmd(deps Deps) *cobra.Command {
	var importArtifacts, apply bool
	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Remove hook bundles that are stale captures of another bundle's rendered output",
		Long: `Scans ~/.agents/hooks/ for bundles that exist only because a prior import run
re-captured another bundle's rendered output under a different derived name (the
render/import feedback loop #533 closed going forward — see internal/platform/hooks_provenance.go).

A bundle is a candidate when its own manifest command resolves, per the same provenance
authority import uses, into a DIFFERENT bundle's directory, or when its manifest and any
sidecar script are byte-identical to an older bundle's. Ambiguous candidates, bundles
referenced by name in a project's .agentsrc.json "hooks" list, and bundles that own their
own command are always skipped, never deleted.

Default is dry-run: candidates are listed with their owning bundle and reason, nothing is
removed. Pass --apply to delete the surviving candidates from ~/.agents/hooks/.`,
		Example: exampleBlock(
			"  da hooks prune --import-artifacts",
			"  da hooks prune --import-artifacts --apply",
		),
		Args: deps.MaxArgsWithHints(0, "`da hooks prune` takes no positional arguments."),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !importArtifacts {
				return deps.UsageError(
					"da hooks prune requires a pruning mode",
					"Pass `--import-artifacts` to scan for and remove renderer-owned duplicate bundles.",
				)
			}
			return runHooksPruneImportArtifacts(deps, apply)
		},
	}
	cmd.Flags().BoolVar(&importArtifacts, "import-artifacts", false,
		"Scan for hook bundles that are stale captures of another bundle's rendered output")
	cmd.Flags().BoolVar(&apply, "apply", false,
		"Delete the surviving candidates (default is dry-run: list only, nothing removed)")
	return cmd
}

func runHooksPruneImportArtifacts(deps Deps, apply bool) error {
	agentsHome := config.AgentsHome()
	candidates, err := platform.ImportArtifactCandidates(agentsHome)
	if err != nil {
		return err
	}

	ui.Header("da hooks prune --import-artifacts")

	if len(candidates) == 0 {
		ui.Info("No import-artifact hook bundles found.")
		return nil
	}

	toRemove, skipped := planHooksPrune(candidates)
	printHooksPruneCandidates(toRemove, skipped)

	fmt.Fprintf(os.Stdout, "\n%d candidate(s) found: %d removable, %d skipped (protected).\n",
		len(candidates), len(toRemove), len(skipped))

	if !apply {
		fmt.Fprintln(os.Stdout, "\nDRY RUN - no changes made. Pass --apply to delete the removable candidates.")
		return nil
	}
	if len(toRemove) == 0 {
		ui.Info("Nothing to apply: every candidate was skipped.")
		return nil
	}
	if !deps.Flags.Yes && !deps.Flags.Force {
		if !ui.Confirm(fmt.Sprintf("Delete %d import-artifact hook bundle(s)?", len(toRemove)), false) {
			ui.Info("Cancelled.")
			return nil
		}
	}

	for _, c := range toRemove {
		if err := fsops.RemoveAll(c.BundleDir); err != nil {
			return fmt.Errorf("removing bundle %s/%s: %w", c.Scope, c.Name, err)
		}
	}
	ui.Success(fmt.Sprintf("Removed %d import-artifact hook bundle(s).", len(toRemove)))
	return nil
}

// planHooksPrune partitions candidates into what would actually be deleted
// versus what must be skipped, applying the never-delete protections: an
// ambiguous verdict, a bundle that (contrary to the detector's own
// invariant) resolves to itself, or a bundle referenced by name in some
// project's .agentsrc.json "hooks" allow-list.
func planHooksPrune(candidates []platform.ImportArtifactCandidate) (toRemove []platform.ImportArtifactCandidate, skipped []hooksPruneSkip) {
	for _, c := range candidates {
		if reason, protect := hooksPruneProtectionReason(c); protect {
			skipped = append(skipped, hooksPruneSkip{candidate: c, reason: reason})
			continue
		}
		toRemove = append(toRemove, c)
	}
	return toRemove, skipped
}

// hooksPruneProtectionReason reports why a candidate must never be deleted,
// or ("", false) when it is safe to remove.
func hooksPruneProtectionReason(c platform.ImportArtifactCandidate) (string, bool) {
	if c.Reason == platform.ImportArtifactReasonAmbiguous {
		return "ownership ambiguous: " + c.Detail, true
	}
	// Defense in depth: the detector only ever reports a DIFFERENT bundle as
	// owner, so this should be structurally impossible — but a destructive
	// operation must never trust that invariant blindly.
	if c.OwnerScope == c.Scope && c.OwnerName == c.Name {
		return "bundle owns its own command", true
	}
	if project, ok := hookReferencedByProjectConfigName(c.Name); ok {
		return fmt.Sprintf("referenced by name in %s's .agentsrc.json hooks list", project), true
	}
	return "", false
}

// hookReferencedByProjectConfigName reports whether some registered
// project's .agentsrc.json explicitly names this hook bundle in its "hooks"
// allow-list — the operator opted this bundle in by name, so pruning must
// leave it alone even if it also looks like an import artifact. A project's
// blanket `"hooks": true` does not count: that is the common default for
// most projects and would otherwise protect nearly every candidate.
//
// Best-effort: an unreadable config.json/bindings table, an unbound
// project, or an unreadable .agentsrc.json are all treated as "not
// referenced" rather than failing the whole prune run — the registry is a
// convenience safety net on top of the primary provenance-based detection,
// not a hard dependency of it.
func hookReferencedByProjectConfigName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", false
	}
	cfg, err := config.Load()
	if err != nil || cfg == nil {
		return "", false
	}
	projects := cfg.ListProjects()
	sort.Strings(projects)
	for _, project := range projects {
		binding, ok := cfg.ProjectBinding(project)
		if !ok || strings.TrimSpace(binding.Path) == "" {
			continue
		}
		rc, err := config.LoadAgentsRC(binding.Path)
		if err != nil || rc == nil || rc.Hooks == nil || rc.Hooks.All {
			continue
		}
		for _, n := range rc.Hooks.Names {
			if n == name {
				return project, true
			}
		}
	}
	return "", false
}

func printHooksPruneCandidates(toRemove []platform.ImportArtifactCandidate, skipped []hooksPruneSkip) {
	if len(toRemove) > 0 {
		fmt.Fprintf(os.Stdout, "\n%sTo remove:%s\n", ui.Bold, ui.Reset)
		for _, c := range toRemove {
			printHooksPruneCandidateLine(c, "")
		}
	}
	if len(skipped) > 0 {
		fmt.Fprintf(os.Stdout, "\n%sSkipped (protected):%s\n", ui.Bold, ui.Reset)
		for _, s := range skipped {
			printHooksPruneCandidateLine(s.candidate, s.reason)
		}
	}
}

func printHooksPruneCandidateLine(c platform.ImportArtifactCandidate, skipReason string) {
	owner := c.Owner()
	if owner == "" {
		owner = "(ambiguous)"
	}
	fmt.Fprintf(os.Stdout, "  %s%s/%s%s  ->  owner %s%s%s  (%s)\n",
		ui.Cyan, c.Scope, c.Name, ui.Reset, ui.Dim, owner, ui.Reset, c.Reason)
	fmt.Fprintf(os.Stdout, "    %s\n", c.Detail)
	if skipReason != "" {
		fmt.Fprintf(os.Stdout, "    %sSKIP:%s %s\n", ui.Yellow, ui.Reset, skipReason)
	}
}
