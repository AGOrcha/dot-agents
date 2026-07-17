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
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a sub-branch worktree and record its base",
		Long: "Forks a new branch+worktree named --name from --base-branch, records the\n" +
			"base-branch tip as the worktree's immutable base ref, and registers the\n" +
			"worktree metadata (purpose, parent PR). The recorded base is what\n" +
			"`merge-back` reads later — it is never re-derived.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			coord, err := openCoordinator()
			if err != nil {
				return err
			}
			res, err := coord.CreateSubBranch(gitwt.CreateOptions{
				Name:       name,
				Path:       path,
				BaseBranch: baseBranch,
				Purpose:    purpose,
				ParentPR:   parentPR,
			})
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

// renderCreate writes the create outcome as human text or JSON.
func renderCreate(out io.Writer, asJSON bool, res gitwt.CreateResult) error {
	if asJSON {
		return writeJSON(out, map[string]any{
			"name":      res.Metadata.Name,
			"base":      res.Base.String(),
			"purpose":   res.Metadata.Purpose,
			"parent_pr": res.Metadata.ParentPR,
		})
	}
	fmt.Fprintf(out, "worktree create: %q created on recorded base %s\n", res.Metadata.Name, res.Base)
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
