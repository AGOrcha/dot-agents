// Package worktree implements the `da worktree` command tree: first-class
// create / merge-back of managed sub-branch worktrees.
//
// Base resolution reads the recorded base ref (via the gitwt Coordinator) and
// never re-derives a fork point with git merge-base, so a parent branch that
// advanced or was force-pushed is caught loudly (ErrStaleBase) instead of
// silently rebasing the sub-branch onto the wrong commit.
package worktree

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-git/go-git/v6"
	"github.com/spf13/cobra"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/gitwt"
)

// registryIdleTTL is the idle window the Registry uses for its prune-scan
// policy. create / merge-back never consult it (they touch only metadata and
// base refs), so the exact value is immaterial; it is set once here for the
// Registry constructor.
const registryIdleTTL = 24 * time.Hour

// NewCmd builds the `da worktree` command tree.
func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "worktree",
		Short: "Create and merge back managed sub-branch worktrees",
		Long: "Creates and merges back managed sub-branch worktrees stacked on a parent branch.\n\n" +
			"`create` forks a new branch+worktree from a parent branch and records that\n" +
			"parent tip as the immutable base. `merge-back` integrates the sub-branch by\n" +
			"reading that recorded base — never re-deriving it with git merge-base — so a\n" +
			"parent that advanced or was force-pushed is caught loudly instead of silently\n" +
			"rebasing onto the wrong commit.",
		Example: strings.Join([]string{
			"  da worktree create --name wt-feat --path ../wt-feat --base-branch main",
			"  da worktree merge-back --name wt-feat --onto main",
		}, "\n"),
	}
	cmd.AddCommand(newCreateCmd())
	cmd.AddCommand(newMergeBackCmd())
	return cmd
}

func newCreateCmd() *cobra.Command {
	var (
		name       string
		path       string
		baseBranch string
		purpose    string
		parentPR   int
		appType    string
		profile    string
		plan       string
		task       string
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a sub-branch worktree and record its base",
		Long: "Forks a new branch+worktree named --name from --base-branch, records the\n" +
			"base-branch tip as the worktree's immutable base ref, and registers the\n" +
			"worktree metadata (purpose, parent PR). The recorded base is what\n" +
			"`merge-back` reads later — it is never re-derived.\n\n" +
			"The recorded app_type is resolved task/plan-first: --app-type overrides,\n" +
			"else --task's app_type (from the plan's TASKS.yaml), else --plan's\n" +
			"default_app_type (from PLAN.yaml). The resolved app_type drives the\n" +
			"execution_profile shape loaded from .agentsrc.json and recorded on the\n" +
			"worktree.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			coord, err := openCoordinator()
			if err != nil {
				return err
			}
			root, err := repoRoot()
			if err != nil {
				return err
			}
			resolvedAppType := resolveEffectiveAppType(cmd.ErrOrStderr(), root, appType, plan, task)
			if resolvedAppType == "" {
				fmt.Fprintln(cmd.ErrOrStderr(), "warning: no app_type resolved (pass --app-type, or --plan/--task with an app_type set) — recording worktree with no resolved agent config")
			}
			if profile == "" {
				profile = "loop-worker"
			}
			opts := gitwt.CreateOptions{
				Name:       name,
				Path:       path,
				BaseBranch: baseBranch,
				Purpose:    purpose,
				ParentPR:   parentPR,
				AppType:    resolvedAppType,
				Profile:    profile,
			}
			resolveAgentConfig(cmd.ErrOrStderr(), &opts)
			res, err := coord.CreateSubBranch(opts)
			if err != nil {
				return fmt.Errorf("worktree create: %w", err)
			}
			return renderCreate(cmd.OutOrStdout(), jsonOut(cmd), res)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Worktree and branch name (charset ^[a-zA-Z0-9-]+$)")
	cmd.Flags().StringVar(&path, "path", "", "Directory for the new linked worktree")
	cmd.Flags().StringVar(&baseBranch, "base-branch", "", "Parent branch whose current tip is recorded as the base")
	cmd.Flags().StringVar(&purpose, "purpose", "", "Free-form registry note (task/slice this worktree serves)")
	cmd.Flags().IntVar(&parentPR, "parent-pr", 0, "Pull-request number this work feeds into")
	cmd.Flags().StringVar(&appType, "app-type", "", "app_type whose execution_profile shape is loaded + recorded. If unset, resolved from --task's app_type then --plan's default_app_type. Pass this flag to override or set a specific one.")
	cmd.Flags().StringVar(&plan, "plan", "", "resolve app_type (and record identity) from this canonical plan/task's metadata")
	cmd.Flags().StringVar(&task, "task", "", "resolve app_type (and record identity) from this canonical plan/task's metadata")
	cmd.Flags().StringVar(&profile, "profile", "", "delegate profile to record (default loop-worker); not a task/plan field")
	mustMarkRequired(cmd, "name", "path", "base-branch")
	return cmd
}

func newMergeBackCmd() *cobra.Command {
	var (
		name string
		onto string
	)
	cmd := &cobra.Command{
		Use:   "merge-back",
		Short: "Integrate a sub-branch into its parent using the recorded base",
		Long: "Integrates the sub-branch --name into --onto by reading the recorded base\n" +
			"ref (never git merge-base). If the recorded base no longer matches the\n" +
			"parent's current tip (advanced or force-pushed parent) it fails loudly, and\n" +
			"after integration it verifies the sub-branch HEAD did not drift underneath.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			coord, err := openCoordinator()
			if err != nil {
				return err
			}
			res, err := coord.MergeBack(gitwt.MergeBackOptions{
				Name:         name,
				ParentBranch: onto,
			})
			if err != nil {
				return fmt.Errorf("worktree merge-back: %w", err)
			}
			return renderMergeBack(cmd.OutOrStdout(), jsonOut(cmd), res)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Sub-branch / worktree name to merge back")
	cmd.Flags().StringVar(&onto, "onto", "", "Parent branch to integrate into (its current tip must still equal the recorded base)")
	mustMarkRequired(cmd, "name", "onto")
	return cmd
}

// openCoordinator resolves the git repository from the current directory and
// builds a Coordinator over its Manager + Registry.
func openCoordinator() (*gitwt.Coordinator, error) {
	root, err := repoRoot()
	if err != nil {
		return nil, err
	}
	mgr, err := gitwt.NewManager(root)
	if err != nil {
		return nil, err
	}
	reg, err := gitwt.NewRegistry(mgr, registryIdleTTL)
	if err != nil {
		return nil, err
	}
	return gitwt.NewCoordinator(mgr, reg)
}

// repoRoot resolves the main worktree root at or above the current directory.
// DetectDotGit lets an invocation from a subdirectory still resolve the
// enclosing repo, matching the git CLI's behaviour.
func repoRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("worktree: resolve working directory: %w", err)
	}
	repo, err := git.PlainOpenWithOptions(cwd, &git.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		return "", fmt.Errorf("worktree: open git repository at %s: %w", cwd, err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		return "", fmt.Errorf("worktree: resolve worktree root: %w", err)
	}
	return wt.Filesystem().Root(), nil
}

// resolveAgentConfig loads the project's AgentsRC and records the app_type's
// execution shape onto opts so the worktree self-describes what it runs under.
// It reuses internal/config's resolution wholesale and is deliberately graceful:
//
//   - A truly-empty AppType is a silent no-op (execution_profile is optional).
//   - Any other failure to resolve — rc load error, nil ExecutionProfile, or no
//     by_app_type entry for AppType — leaves the resolved fields empty and emits
//     a warning to warn, so a typo'd app_type invoked directly (not via the
//     validating fanout orchestrator) is visible rather than a silent empty
//     config. AppType/Profile are still recorded either way.
func resolveAgentConfig(warn io.Writer, opts *gitwt.CreateOptions) {
	if opts.AppType == "" {
		return
	}
	prof, ok := loadAppTypeProfile(opts.AppType)
	if !ok {
		fmt.Fprintf(warn, "warning: no execution_profile entry for app_type %q — recording worktree with no resolved agent config\n", opts.AppType)
		return
	}
	opts.VerifierSequence = prof.Topology.VerifierSequence
	opts.LensSet = prof.Lenses.LensSet
	opts.LensConcurrency = prof.Lenses.LensConcurrency
	opts.GraphBackend = prof.GraphBackendRef()
}

// loadAppTypeProfile resolves the project's execution_profile.by_app_type entry
// for appType, reporting ok=false when the repo, AgentsRC, or a by_app_type
// entry cannot be resolved. It never errors: an unconfigured profile is normal.
func loadAppTypeProfile(appType string) (config.AppTypeProfile, bool) {
	root, err := repoRoot()
	if err != nil {
		return config.AppTypeProfile{}, false
	}
	rc, err := config.LoadAgentsRC(root)
	if err != nil || rc.ExecutionProfile == nil || rc.ExecutionProfile.ByAppType == nil {
		return config.AppTypeProfile{}, false
	}
	prof, ok := rc.ExecutionProfile.ByAppType[appType]
	return prof, ok
}

// renderCreate writes the create outcome as human text or JSON.
func renderCreate(out io.Writer, asJSON bool, res gitwt.CreateResult) error {
	if asJSON {
		return writeJSON(out, map[string]any{
			"name":              res.Metadata.Name,
			"base":              res.Base.String(),
			"purpose":           res.Metadata.Purpose,
			"parent_pr":         res.Metadata.ParentPR,
			"app_type":          res.Metadata.AppType,
			"profile":           res.Metadata.Profile,
			"verifier_sequence": res.Metadata.VerifierSequence,
			"lens_set":          res.Metadata.LensSet,
			"lens_concurrency":  res.Metadata.LensConcurrency,
			"graph_backend":     res.Metadata.GraphBackend,
		})
	}
	fmt.Fprintf(out, "worktree create: %q created on recorded base %s\n", res.Metadata.Name, res.Base)
	if res.Metadata.AppType != "" {
		fmt.Fprintf(out, "  agent config: app_type=%s profile=%s verifier_sequence=%v lens_set=%v lens_concurrency=%s graph_backend=%s\n",
			res.Metadata.AppType, res.Metadata.Profile, res.Metadata.VerifierSequence,
			res.Metadata.LensSet, res.Metadata.LensConcurrency, res.Metadata.GraphBackend)
	}
	return nil
}

// renderMergeBack writes the merge-back outcome as human text or JSON.
func renderMergeBack(out io.Writer, asJSON bool, res gitwt.MergeBackResult) error {
	if asJSON {
		return writeJSON(out, map[string]any{
			"base":          res.Base.String(),
			"sub_head":      res.SubHead.String(),
			"parent_branch": res.ParentBranch,
			"parent_tip":    res.ParentTip.String(),
		})
	}
	fmt.Fprintf(out, "worktree merge-back: fast-forwarded %q to %s (recorded base %s)\n",
		res.ParentBranch, res.SubHead, res.Base)
	return nil
}

func writeJSON(out io.Writer, v any) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// jsonOut reports the inherited global --json flag, defaulting to false when the
// flag is absent (e.g. a unit test builds the subcommand outside the root tree).
func jsonOut(cmd *cobra.Command) bool {
	if f := cmd.Flags().Lookup("json"); f != nil {
		if v, err := strconv.ParseBool(f.Value.String()); err == nil {
			return v
		}
	}
	return false
}

// mustMarkRequired marks each flag required, panicking if a name is misspelled
// — a programmer error caught at construction, mirroring cobra's own contract.
func mustMarkRequired(cmd *cobra.Command, names ...string) {
	for _, n := range names {
		if err := cmd.MarkFlagRequired(n); err != nil {
			panic(fmt.Sprintf("worktree: mark %q required: %v", n, err))
		}
	}
}
