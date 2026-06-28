package lifecycle

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/ui"
	"github.com/spf13/cobra"
	"golang.org/x/sys/execabs"
)

// init_from.go implements `da init --from <home-source>` — the L3 home-config
// PHASE-1 cross-machine bootstrap (home-config-portability D-D). On a fresh
// machine it: (1) reconciles the target ~/.agents fail-safe (FORK-2 — refuse a
// non-empty existing home, allow an empty one); (2) clones the remote home
// source into ~/.agents with ambient-first git auth (NEW-FORK-B); (3) resolves
// the user-scope config — sources, layering policy, and the optional manifest —
// through the SAME §15 + L1 + L2 engines (config.ResolveUserScopeManifests);
// (4) re-establishes the harness-native user surface by RE-RUNNING the existing
// global-link machinery; and (5) joins the synced portable identity registry to
// this machine's binding table, reporting every project known-but-unbound
// (paths are never fabricated, R4/R4a).
//
// It is DISTINCT from `da init --force`: --force clobbers (backup-then-replace)
// an existing home; init --from NEVER clobbers — it refuses a populated home.

// initFromFlag is the `--from` flag name on `da init`.
const initFromFlag = "from"

// cloneHomeSourceFn clones the remote home source into the local ~/.agents. It
// is a package var so tests can inject a fixture-copy without a real network
// clone; production uses gitCloneHomeSource (the git CLI, ambient-first auth).
var cloneHomeSourceFn = gitCloneHomeSource

// initFromValue safely reads the `--from` flag from cmd. A nil command or an
// init invocation that never registered the flag (the lifecycle-only unit-test
// constructors that build a bare cobra command) yields "", routing runInit to
// the ordinary fresh-local scaffold path.
func initFromValue(cmd *cobra.Command) string {
	if cmd == nil {
		return ""
	}
	v, err := cmd.Flags().GetString(initFromFlag)
	if err != nil {
		return ""
	}
	return v
}

// runInitFrom is the `da init --from <ref>` entrypoint, dispatched from runInit
// when --from is set. The steps are kept flat (each a small helper) so the
// control flow reads as the D-D sequence.
func runInitFrom(cmd *cobra.Command, fromRef string, deps initDirMaker) error {
	agentsHome := config.AgentsHome()

	ui.Header("da init --from")
	ui.Bullet("info", "Home source: "+fromRef)

	ui.Step("Checking target ~/.agents...")
	if err := reconcileExistingAgentsHome(agentsHome); err != nil {
		return err
	}

	if InitDryRunFn() {
		reportInitFromDryRun(agentsHome, fromRef)
		return nil
	}

	ui.Step("Cloning home source (ambient git auth)...")
	if err := cloneHomeSourceFn(fromRef, agentsHome); err != nil {
		return fmt.Errorf("cloning home source %q: %w", fromRef, err)
	}
	ui.Bullet("ok", "Cloned home into "+config.DisplayPath(agentsHome))

	if err := reportUserScope(); err != nil {
		return err
	}

	if err := materializeUserSurface(agentsHome, deps); err != nil {
		return err
	}

	known, bound, unbound, err := rebindProjectSet()
	if err != nil {
		return err
	}
	reportRebind(bound, unbound)

	ui.SuccessBox("Home adopted from the remote source!",
		fmt.Sprintf("%d project(s) known, %d bound, %d unbound on this machine", known, len(bound), len(unbound)),
		"Re-detect platforms and re-link projects: da refresh",
		"Bind an unbound project here: da add ~/path/to/project",
		"Resume git sync: da sync push",
	)
	return nil
}

// reconcileExistingAgentsHome implements the FORK-2 reconcile: a missing or
// EMPTY ~/.agents is allowed (treated as fresh — git clone materializes it); a
// NON-EMPTY existing home is refused with a clear message. This is a safe
// reconcile distinct from `da init --force`'s clobber: init --from never
// destroys an existing home. Adopting a remote home INTO a populated one
// (--adopt/--merge) is deferred (FORK-2 near-roadmap).
func reconcileExistingAgentsHome(agentsHome string) error {
	entries, err := os.ReadDir(agentsHome)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // fresh — the common path
		}
		return fmt.Errorf("inspecting %s: %w", config.DisplayPath(agentsHome), err)
	}
	if len(entries) == 0 {
		return nil // an empty placeholder dir is treated as fresh
	}
	return InitUsageErrorFn(
		fmt.Sprintf("%s already exists and is not empty", config.DisplayPath(agentsHome)),
		"init --from refuses to overwrite an existing home (FORK-2: refuse non-empty, allow empty).",
		"Adopting a remote home INTO a populated home (--adopt/--merge) is not yet supported.",
		"Move or remove the existing ~/.agents and re-run, or run `da init` for a fresh local scaffold.",
	)
}

// gitCloneHomeSource clones ref into dest with the system `git` CLI. The git CLI
// is the right seam for AMBIENT-FIRST auth (NEW-FORK-B): ssh-agent /
// credential-helper / on-disk key resolution all happen inside git, so no
// credential is ever threaded through dot-agents — or written into the synced
// tree (R7). `git clone` into an existing EMPTY dir succeeds and creates a
// missing one; the non-empty refusal already ran in reconcileExistingAgentsHome.
func gitCloneHomeSource(ref, dest string) error {
	out, err := execabs.Command("git", "clone", ref, dest).CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// reportUserScope resolves and surfaces the user-scope config the cloned home
// declares — the sources, layering policy, and optional manifest with its
// project-set ref (D-D step 2). A home with no manifest is valid: the user-local
// sources/policy still resolve directly.
func reportUserScope() error {
	manifests, err := config.ResolveUserScopeManifests()
	if err != nil {
		return fmt.Errorf("resolving user-scope config: %w", err)
	}
	if len(manifests) == 0 {
		ui.Bullet("info", "No manifest declared; resolved user-local sources/policy directly")
		return nil
	}
	for _, m := range manifests {
		reportResolvedManifest(m)
	}
	return nil
}

// reportResolvedManifest renders one resolved manifest's referenced source-set
// and optional project-set ref.
func reportResolvedManifest(m config.ResolvedManifest) {
	ui.Bullet("ok", fmt.Sprintf("manifest %s — %d source(s)", m.Ref, len(m.Sources)))
	if m.HasProjectSet {
		ui.Bullet("info", "project-set: "+m.ProjectSet)
	}
}

// materializeUserSurface re-establishes the harness-native user surface on the
// fresh machine by RE-RUNNING the existing global-link machinery — the same
// linkClaudeGlobalSettings / linkCursorGlobalHooks `da init` uses (D-D step 3,
// reuse-not-reimplement). Each is a no-op when its harness is not installed, so
// detection is fresh on this machine (D-E). The machine-local roots (state dir +
// ~/.agents/local/) are (re)created so the binding table has a home outside the
// synced tree.
func materializeUserSurface(agentsHome string, deps initDirMaker) error {
	ui.Step("Materializing harness user surface...")
	if err := linkClaudeGlobalSettings(agentsHome, deps); err != nil {
		return err
	}
	if err := linkCursorGlobalHooks(agentsHome, deps); err != nil {
		return err
	}
	_ = deps.MkdirAll(config.AgentsStateDir(), 0755)
	_ = deps.MkdirAll(filepath.Join(agentsHome, "local"), 0755)
	return nil
}

// rebindProjectSet joins the synced portable identity registry (config.json,
// brought by the clone) to THIS machine's binding table. Every project identity
// travels; the id→path binding is machine-local and unknown on a fresh machine,
// so each project is left UNBOUND and reported known-but-unbound (R4/R4a) — paths
// are NEVER fabricated (D-D step 4). When Load reports a legacy v1 home was
// cloned (UpgradeNeeded), the inline paths it leaked are the SOURCE machine's and
// are discarded before the v2 split is persisted locally (defect 1). Returns
// (knownCount, boundIDs, unboundIDs).
func rebindProjectSet() (int, []string, []string, error) {
	cfg, err := config.Load()
	if err != nil {
		return 0, nil, nil, fmt.Errorf("loading synced identity registry: %w", err)
	}
	migrated := cfg.UpgradeNeeded()
	if migrated {
		cfg.DropLocalBindings() // discard any SOURCE-machine paths a v1 home leaked
	}

	known := cfg.ListProjects()
	sort.Strings(known)
	var bound, unbound []string
	for _, name := range known {
		if cfg.IsProjectBound(name) {
			bound = append(bound, name)
		} else {
			unbound = append(unbound, name)
		}
	}

	if migrated {
		if err := cfg.Save(); err != nil {
			return 0, nil, nil, fmt.Errorf("persisting migrated registry: %w", err)
		}
	}
	return len(known), bound, unbound, nil
}

// reportRebind renders the bound/unbound split. Unbound projects are surfaced as
// known-but-unbound (the R4 guarantee) rather than silently skipped.
func reportRebind(bound, unbound []string) {
	for _, name := range bound {
		ui.Bullet("ok", "bound: "+name)
	}
	for _, name := range unbound {
		ui.Bullet("warn", "known but unbound on this machine: "+name)
	}
}

// reportInitFromDryRun prints the plan without cloning or writing anything.
func reportInitFromDryRun(agentsHome, fromRef string) {
	ui.DryRun("git clone " + fromRef + " " + config.DisplayPath(agentsHome))
	ui.DryRun("Resolve user-scope sources + layering policy + manifest")
	ui.DryRun("Re-establish harness global links (Claude / Cursor)")
	ui.DryRun("Report synced projects as known-but-unbound on this machine")
	fmt.Fprintln(os.Stdout, "\nDRY RUN - no changes made")
}
