package lifecycle

import (
	"fmt"
	"net/url"
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
// machine it: (1) enforces ambient-only git auth (a credential-bearing --from URL
// is refused, never echoed, never persisted); (2) reconciles the target
// ~/.agents fail-safe (FORK-2 — refuse non-empty, allow empty); (3) clones the
// home into a same-filesystem STAGING dir and resolves + rebinds against it,
// renaming it into ~/.agents only after the whole flow succeeds (atomic — a
// clone-then-fail never bricks a retry); (4) re-establishes the harness-native
// user surface by re-running the existing global-link machinery; and (5) joins
// the synced portable identity registry to this machine's binding table, starting
// from ZERO bindings so every project is known-but-unbound (paths never imported).
//
// It is DISTINCT from `da init --force`: --force clobbers an existing home;
// init --from NEVER clobbers — it refuses a populated home.

// initFromFlag is the `--from` flag name on `da init`.
const initFromFlag = "from"

// stagedMachineLocalDirs are the machine-local sync-boundary dirs that must never
// travel inside an adopted home (R7): the binding table (local/) and the caches
// (cache/). Shared by the untrack + gitignore repairs so the path literals have a
// single source.
var stagedMachineLocalDirs = []string{"local/", "cache/"}

// cloneHomeSourceFn clones the remote home source into a local staging dir. It is
// a package var so tests can inject a fixture-copy without a real network clone;
// production uses gitCloneHomeSource (the git CLI, ambient-first auth).
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
// when --from is set. The bootstrap is atomic: nothing lands in ~/.agents until
// the staged clone fully resolves (BUG-1).
func runInitFrom(cmd *cobra.Command, fromRef string, deps initDirMaker) error {
	agentsHome := config.AgentsHome()

	ui.Header("da init --from")
	if err := validateAmbientAuthRef(fromRef); err != nil {
		return err
	}
	ui.Bullet("info", "Home source: "+redactRef(fromRef))

	ui.Step("Checking target ~/.agents...")
	if err := reconcileExistingAgentsHome(agentsHome); err != nil {
		return err
	}

	if InitDryRunFn() {
		reportInitFromDryRun(agentsHome, fromRef)
		return nil
	}

	known, unbound, err := stageAndAdoptHome(fromRef, agentsHome)
	if err != nil {
		return err
	}

	if err := materializeUserSurface(agentsHome, deps); err != nil {
		return err
	}
	reportRebind(unbound)

	ui.SuccessBox("Home adopted from the remote source!",
		fmt.Sprintf("%d project(s) known (all unbound on this machine)", known),
		"Re-detect platforms and re-link projects: da refresh",
		"Bind a project here: da add ~/path/to/project",
		"Resume git sync: da sync push",
	)
	return nil
}

// stageAndAdoptHome clones the home into a same-filesystem staging dir, resolves
// + rebinds against it, and atomically renames it into agentsHome ONLY after the
// whole flow succeeds. ANY failure removes the staging dir, leaving NO partial
// home — so a clone-then-fail does not brick the retry by tripping the non-empty
// refusal (BUG-1). Returns (knownCount, unboundIDs).
func stageAndAdoptHome(fromRef, agentsHome string) (int, []string, error) {
	staging, err := os.MkdirTemp(filepath.Dir(agentsHome), ".agents.tmp-*")
	if err != nil {
		return 0, nil, fmt.Errorf("creating staging dir: %w", err)
	}
	moved := false
	defer func() {
		if !moved {
			_ = os.RemoveAll(staging)
		}
	}()

	ui.Step("Cloning home source (ambient git auth)...")
	if err := cloneHomeSourceFn(fromRef, staging); err != nil {
		return 0, nil, fmt.Errorf("cloning home source: %w", err)
	}

	known, unbound, err := resolveAndRebindStaged(staging)
	if err != nil {
		return 0, nil, err
	}

	if err := moveStagedHome(staging, agentsHome); err != nil {
		return 0, nil, err
	}
	moved = true
	ui.Bullet("ok", "Adopted home into "+config.DisplayPath(agentsHome))
	return known, unbound, nil
}

// resolveAndRebindStaged resolves the user scope and rebinds the project-set
// against the STAGED home by pointing AGENTS_HOME at the staging dir for the
// duration. Both reads (UserScopeSnapshot, config.Load) resolve AGENTS_HOME, so
// the swap-and-restore is what makes the staged flow possible before the rename.
func resolveAndRebindStaged(staging string) (int, []string, error) {
	restore := withAgentsHome(staging)
	defer restore()

	if err := reportUserScope(); err != nil {
		return 0, nil, err
	}
	untrackStagedMachineLocal(staging)
	if err := ensureStagedMachineLocalGitignored(staging); err != nil {
		return 0, nil, fmt.Errorf("writing machine-local .gitignore: %w", err)
	}
	return rebindProjectSet()
}

// withAgentsHome sets AGENTS_HOME to path and returns a restore func that puts the
// prior value back (unsetting it if it was previously unset).
func withAgentsHome(path string) func() {
	old, had := os.LookupEnv("AGENTS_HOME")
	_ = os.Setenv("AGENTS_HOME", path)
	return func() {
		if had {
			_ = os.Setenv("AGENTS_HOME", old)
		} else {
			_ = os.Unsetenv("AGENTS_HOME")
		}
	}
}

// moveStagedHome atomically renames the staged home into agentsHome. reconcile
// already guaranteed agentsHome is missing or an EMPTY placeholder, so the empty
// dir is cleared first to free the rename target (rename onto a non-empty dir
// fails — and a non-empty home was already refused). Same-filesystem rename
// (staging is a sibling of agentsHome) keeps the publish atomic.
func moveStagedHome(staging, agentsHome string) error {
	if err := os.Remove(agentsHome); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clearing empty target %s: %w", config.DisplayPath(agentsHome), err)
	}
	if err := os.Rename(staging, agentsHome); err != nil {
		return fmt.Errorf("moving staged home into place: %w", err)
	}
	return nil
}

// validateAmbientAuthRef enforces the ambient-only auth contract (BUG-3): a
// --from URL that embeds a credential — a password, or a token in the userinfo of
// an http/https URL — is REFUSED, so no secret is echoed to the console or
// persisted by git into ~/.agents/.git/config. An ssh login user (git@host, no
// password) is NOT a credential and is allowed: ssh-agent supplies the secret
// ambiently. scp-style refs (git@host:path) and unparseable refs carry no URL
// userinfo and pass through.
func validateAmbientAuthRef(ref string) error {
	u, err := url.Parse(ref)
	if err != nil || u.User == nil {
		return nil
	}
	if _, hasPassword := u.User.Password(); hasPassword {
		return ambientAuthError()
	}
	scheme := strings.ToLower(u.Scheme)
	if (scheme == "http" || scheme == "https") && u.User.Username() != "" {
		return ambientAuthError()
	}
	return nil
}

// ambientAuthError is the refusal for a credential-bearing --from URL.
func ambientAuthError() error {
	return InitUsageErrorFn(
		"init --from refuses a --from URL that embeds a credential",
		"Credentials must be ambient: use ssh-agent (ssh:// or git@host refs) or a git credential-helper for https.",
		"Re-run with a credential-free URL — the secret must never enter the synced ~/.agents tree.",
	)
}

// redactRef masks a userinfo password before the source ref is echoed — a
// defensive backstop to validateAmbientAuthRef so no credential can reach the log
// even if a future edit loosens the refusal.
func redactRef(ref string) string {
	u, err := url.Parse(ref)
	if err != nil || u.User == nil {
		return ref
	}
	if _, hasPassword := u.User.Password(); hasPassword {
		u.User = url.User("***")
		return u.String()
	}
	return ref
}

// reconcileExistingAgentsHome implements the FORK-2 reconcile: a missing or
// EMPTY ~/.agents is allowed (treated as fresh); a NON-EMPTY existing home is
// refused with a clear message. This is a safe reconcile distinct from
// `da init --force`'s clobber: init --from never destroys an existing home.
// Adopting a remote home INTO a populated one (--adopt/--merge) is deferred
// (FORK-2 near-roadmap).
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
// credential is threaded through dot-agents. validateAmbientAuthRef already
// refused any credential-bearing ref upstream, so git never persists a secret
// into dest/.git/config. dest is an existing empty staging dir, which `git clone`
// accepts.
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

// untrackStagedMachineLocal removes any machine-local paths a source mistakenly
// tracked from the staged clone's git index WITHOUT deleting the working-tree
// files (BUG-2: a source that committed local/bindings.json or cache/ must stop
// re-syncing it). Mirrors commands/sync/init.go's in-place repair;
// --ignore-unmatch makes it a no-op when nothing is tracked.
func untrackStagedMachineLocal(staging string) {
	args := append([]string{"-C", staging, "rm", "--cached", "-r", "--ignore-unmatch", "--quiet"}, stagedMachineLocalDirs...)
	_ = execabs.Command("git", args...).Run()
}

// ensureStagedMachineLocalGitignored guarantees the staged home's .gitignore
// excludes every machine-local dir, so a later re-sync from this machine never
// pushes the binding table or caches back (BUG-2). A missing file is created with
// the full set; an existing file gains only the entries it lacks.
func ensureStagedMachineLocalGitignored(staging string) error {
	path := filepath.Join(staging, ".gitignore")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return os.WriteFile(path, []byte(strings.Join(stagedMachineLocalDirs, "\n")+"\n"), 0644)
		}
		return err
	}
	present := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		present[strings.TrimSpace(line)] = true
	}
	var missing []string
	for _, d := range stagedMachineLocalDirs {
		if !present[d] {
			missing = append(missing, d)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	content := string(data)
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	content += strings.Join(missing, "\n") + "\n"
	return os.WriteFile(path, []byte(content), 0644)
}

// materializeUserSurface re-establishes the harness-native user surface on the
// fresh machine by RE-RUNNING the existing global-link machinery — the same
// linkClaudeGlobalSettings / linkCursorGlobalHooks `da init` uses (D-D step 3,
// reuse-not-reimplement). Each is a no-op when its harness is not installed, so
// detection is fresh on this machine (D-E). It runs AFTER the staged home is
// renamed into place so the links point at the permanent path. The machine-local
// roots (state dir + ~/.agents/local/) are (re)created so the binding table has a
// home outside the synced tree.
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
// brought by the clone) to THIS machine's binding table. It ALWAYS starts from
// ZERO bindings: a fresh machine has no valid path binding, and any binding the
// source tracked (a v2 home that mistakenly synced local/bindings.json, or a
// legacy v1 home with inline paths) holds the SOURCE machine's absolute paths,
// which must NEVER be imported here (BUG-2 / defect 1). Every project is therefore
// known-but-unbound (R4/R4a) — paths are never fabricated. The path-free identity
// registry + the empty machine-local binding table are persisted so the adopted
// home starts from a clean, split-correct slate. Returns (knownCount, unboundIDs).
func rebindProjectSet() (int, []string, error) {
	cfg, err := config.Load()
	if err != nil {
		return 0, nil, fmt.Errorf("loading synced identity registry: %w", err)
	}
	cfg.DropLocalBindings()

	known := cfg.ListProjects()
	sort.Strings(known)

	if err := cfg.Save(); err != nil {
		return 0, nil, fmt.Errorf("persisting adopted registry: %w", err)
	}
	return len(known), known, nil
}

// reportRebind surfaces every project as known-but-unbound on this machine (the
// R4 guarantee) rather than silently skipping it.
func reportRebind(unbound []string) {
	for _, name := range unbound {
		ui.Bullet("warn", "known but unbound on this machine: "+name)
	}
}

// reportInitFromDryRun prints the plan without cloning or writing anything.
func reportInitFromDryRun(agentsHome, fromRef string) {
	ui.DryRun("git clone " + redactRef(fromRef) + " (into a staging dir)")
	ui.DryRun("Resolve user-scope sources + layering policy + manifest")
	ui.DryRun("Drop machine-local bindings; untrack + gitignore local/ + cache/")
	ui.DryRun("Atomically move the staged home into " + config.DisplayPath(agentsHome))
	ui.DryRun("Re-establish harness global links (Claude / Cursor)")
	ui.DryRun("Report synced projects as known-but-unbound on this machine")
	fmt.Fprintln(os.Stdout, "\nDRY RUN - no changes made")
}
