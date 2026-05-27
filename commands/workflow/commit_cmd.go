// commit_cmd.go wires the `da workflow commit` subcommand. It composes the
// pure path-derivation in commit_pathset.go with a small interface seam over
// git so the orchestration is testable without a real worktree, and stages /
// commits only the derived set — the "never -A" rule the spec mandates.
package workflow

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/spf13/cobra"
)

// commitErrFmt is the canonical wrap prefix for any error escaping the
// `da workflow commit` orchestrator (4 sites). Centralized so the user-
// visible "workflow commit:" tag stays consistent across the staging,
// commit, push, and disabled-state branches.
const commitErrFmt = "workflow commit: %w"

// gogitWorktree is the minimal go-git Worktree surface gogitImpl uses,
// extracted into an interface so tests can inject a stub that exercises
// the per-method error branches without standing up a corrupted real
// repo for each one. *git.Worktree satisfies it natively.
type gogitWorktree interface {
	Status() (git.Status, error)
	Add(path string) (plumbing.Hash, error)
	Commit(msg string, opts *git.CommitOptions) (plumbing.Hash, error)
	Submodules() (git.Submodules, error)
}

// gitOps is the minimal git surface `da workflow commit` needs. Interface-DI
// (not a func-var) per the codebase's prefer-interface-di-over-funcvar-seams
// lesson; tests inject a stub, production wires gogitImpl{}.
type gitOps interface {
	// Status returns the parsed status entries for the worktree. Status
	// is the direct programmatic shape (rather than porcelain bytes)
	// because go-git already produces a structured Status map — round-
	// tripping through porcelain text would be a pure waste.
	Status() ([]StatusEntry, error)
	AddPaths(paths []string) error
	Commit(message string) error
}

// gogitImpl is the production implementation: drives git operations via
// go-git/v6 instead of shelling out. No syscall per command, no PATH
// surface (the package-internal go-git code paths replace `git` on PATH),
// no porcelain-text parsing on the hot path. Both the *Repository and
// its *Worktree are resolved once at construction so the per-call
// methods do not re-walk the same error branches each invocation —
// reduces the wrapper surface coverage tests have to cover, too.
type gogitImpl struct {
	repo *git.Repository
	wt   gogitWorktree
}

// newGogitImpl opens the git repository at (or above) the current
// working directory and resolves its worktree handle. DetectDotGit lets
// an invocation from any subdirectory find the right repo, matching the
// git CLI's behaviour. Surfaces a clear "open git repo" or "worktree"
// error if either step fails so the operator sees the cause without a
// stack trace.
func newGogitImpl() (*gogitImpl, error) {
	// os.Getwd is effectively infallible on a working process; an empty
	// cwd would propagate naturally as a PlainOpenWithOptions error.
	cwd, _ := os.Getwd()
	repo, err := git.PlainOpenWithOptions(cwd, &git.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		return nil, fmt.Errorf("open git repo at %s: %w", cwd, err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("worktree: %w", err)
	}
	return &gogitImpl{repo: repo, wt: wt}, nil
}

func (g *gogitImpl) Status() ([]StatusEntry, error) {
	status, err := g.wt.Status()
	if err != nil {
		return nil, fmt.Errorf("status: %w", err)
	}
	return statusToEntries(g.wt, status)
}

func (g *gogitImpl) AddPaths(paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	for _, p := range paths {
		if _, err := g.wt.Add(p); err != nil {
			return fmt.Errorf("git add %s: %w", p, err)
		}
	}
	return nil
}

func (g *gogitImpl) Commit(message string) error {
	name, email, err := g.userIdentity()
	if err != nil {
		return err
	}
	_, err = g.wt.Commit(message, &git.CommitOptions{
		Author: &object.Signature{Name: name, Email: email, When: time.Now()},
	})
	if err != nil {
		return fmt.Errorf("git commit: %w", err)
	}
	return nil
}

// userIdentity walks the standard config scopes (local → global → system)
// for user.name and user.email, matching what `git commit` reads. go-git
// does not consult config automatically when populating commit signatures
// the way the CLI does; without this lookup, commits would land with an
// empty author and reviewers would lose attribution.
//
// The scope walk always completes (no early-return optimization) so the
// function has a single success path — cheaper to test, no measurable
// runtime cost since ConfigScoped reads are cached and bounded.
func (g *gogitImpl) userIdentity() (string, string, error) {
	var name, email string
	for _, scope := range []config.Scope{config.LocalScope, config.GlobalScope, config.SystemScope} {
		cfg, err := g.repo.ConfigScoped(scope)
		if err != nil {
			continue
		}
		if name == "" {
			name = cfg.User.Name
		}
		if email == "" {
			email = cfg.User.Email
		}
	}
	if name == "" || email == "" {
		return "", "", errors.New("git commit: user.name / user.email not configured (run `git config user.name` and `git config user.email`)")
	}
	return name, email, nil
}

// statusToEntries converts go-git's Worktree.Status() output into the
// []StatusEntry shape DerivePathSet consumes. Submodule paths come from
// repo.Submodules() (go-git's Status does not surface a submodule flag
// per file) so the existing DerivePathSet exclusion rule still fires.
//
// Rename / copy entries have their previous path in FileStatus.Extra;
// the adapter copies it into OrigPath so DerivePathSet can stage both
// sides of the rename together — same contract the porcelain v2 parser
// already established.
func statusToEntries(wt gogitWorktree, status git.Status) ([]StatusEntry, error) {
	subs, err := wt.Submodules()
	if err != nil {
		return nil, fmt.Errorf("submodules: %w", err)
	}
	isSubmodulePath := make(map[string]bool, len(subs))
	for _, s := range subs {
		isSubmodulePath[s.Config().Path] = true
	}
	entries := make([]StatusEntry, 0, len(status))
	for path, fs := range status {
		entries = append(entries, StatusEntry{
			Path:      path,
			OrigPath:  fs.Extra,
			XY:        string([]byte{byte(fs.Staging), byte(fs.Worktree)}),
			Submodule: isSubmodulePath[path],
			Untracked: fs.Staging == git.Untracked || fs.Worktree == git.Untracked,
		})
	}
	return entries, nil
}

// newWorkflowCommitCmd builds the cobra subcommand.
func newWorkflowCommitCmd() *cobra.Command {
	var (
		dryRun   bool
		includes []string
	)
	cmd := &cobra.Command{
		Use:   "commit",
		Short: "Stage and commit workflow-state changes (managed roots + declared session paths)",
		Long: "Stages the deterministic, scoped set of workflow-state paths derived from\n" +
			"`git status --porcelain=v2 -z` and commits them with a generated message that\n" +
			"distinguishes the commit from code commits.\n\n" +
			"Includes paths under `.agents/workflow/` and `.agents/history/` by default; pass\n" +
			"`--include <path>` (repeatable) to declare additional session-touched state files\n" +
			"such as iter-N.yaml under `.agents/active/`. NEVER `-A`; submodule pointers and\n" +
			"pre-existing-untracked entries are excluded by design.\n\n" +
			"Idempotent: a second run with no new mutations is a clean no-op. `--dry-run`\n" +
			"prints the staging set + commit message without touching anything.",
		Example: deps.ExampleBlock(
			"  da workflow commit",
			"  da workflow commit --dry-run",
			"  da workflow commit --include .agents/active/iteration-log/iter-7.yaml",
		),
		Args: deps.NoArgsWithHints("Run workflow commit from inside the project repository."),
		RunE: func(cmd *cobra.Command, _ []string) error {
			gg, err := newGogitImpl()
			if err != nil {
				return fmt.Errorf(commitErrFmt, err)
			}
			return runWorkflowCommit(cmd.OutOrStdout(), gg, dryRun, includes)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print the path set + commit message; make no changes")
	cmd.Flags().StringSliceVar(&includes, "include", nil, "Additional session-touched paths to consider for staging (repeatable)")
	return cmd
}

// runWorkflowCommit is the body — extracted from the cobra closure so tests
// can drive it directly with a stub gitOps and an output buffer.
//
// If the per-project preference `commit.disable=true` is set the command
// short-circuits to a documented no-op (clear status line, exit 0). Same
// short-circuit fires from wc-iteration-close, so the opt-out applies to
// both the standalone command and the iteration-close hook.
func runWorkflowCommit(out io.Writer, git gitOps, dryRun bool, includes []string) error {
	if disabled, reason := commitDisabled(); disabled {
		fmt.Fprintf(out, "workflow commit: opt-out active (%s)\n", reason)
		return nil
	}
	entries, err := git.Status()
	if err != nil {
		return fmt.Errorf(commitErrFmt, err)
	}
	paths := DerivePathSet(entries, includes)
	if len(paths) == 0 {
		fmt.Fprintln(out, "workflow commit: nothing to stage (idempotent no-op)")
		return nil
	}
	message := buildCommitMessage(paths)
	if dryRun {
		fmt.Fprintln(out, "workflow commit (dry-run) — would stage:")
		for _, p := range paths {
			fmt.Fprintf(out, "  %s\n", p)
		}
		fmt.Fprintln(out)
		fmt.Fprintln(out, "with message:")
		fmt.Fprintln(out, indentMessage(message))
		return nil
	}
	if err := git.AddPaths(paths); err != nil {
		return fmt.Errorf("workflow commit: stage: %w", err)
	}
	if err := git.Commit(message); err != nil {
		return fmt.Errorf(commitErrFmt, err)
	}
	fmt.Fprintf(out, "workflow commit: staged %d path(s) and committed\n", len(paths))
	return nil
}

// buildCommitMessage returns the generated commit message. The leading line
// gives the message its identity (workflow-state, not code) so a glance at
// `git log` separates the two flows; the body lists the exact path set so
// reviewers can verify the "never -A" rule held this time.
func buildCommitMessage(paths []string) string {
	var sb strings.Builder
	sb.WriteString("workflow(state) Sync canonical-store changes via `da workflow commit`\n")
	sb.WriteString("\n")
	sb.WriteString("Distinct from code commits; keeps the canonical-store and git history in sync.\n")
	sb.WriteString("\n")
	sb.WriteString("Paths:\n")
	for _, p := range paths {
		sb.WriteString("- ")
		sb.WriteString(p)
		sb.WriteString("\n")
	}
	return sb.String()
}

// iterationCloseCommit is the close-path entry point — called by `advance`
// and `merge-back` when their `--commit-state` flag is set, so the iteration
// log + verification log + plan-state mutation + the workflow-state commit
// land together rather than as two separate operator steps. The function-
// var seam keeps the advance / merge-back tests cheap (no real git, no
// real prefs) — the actual close-flow integration is exercised by
// wc-verify-close.
var iterationCloseCommit = func(out io.Writer) error {
	gg, err := newGogitImpl()
	if err != nil {
		return fmt.Errorf(commitErrFmt, err)
	}
	return runWorkflowCommit(out, gg, false, nil)
}

// commitDisabled resolves whether the workflow-commit auto-flow is opted out
// for the current project. Default points at commitDisabledFromPrefs (the
// real implementation); tests rebind it to a stub so they do not have to
// reach for currentWorkflowProject / preferences-file plumbing.
var commitDisabled = commitDisabledFromPrefs

// commitDisabledFromPrefs is the production implementation: read the
// resolved per-project preferences and return (true, reason) iff
// `commit.disable` is set. If the project cannot be resolved (e.g. running
// outside any managed project, or before `da workflow plan create`), the
// safe default is "not disabled" so the operator still gets the staging
// behaviour rather than a silent skip.
func commitDisabledFromPrefs() (bool, string) {
	project, err := currentWorkflowProject()
	if err != nil {
		return false, ""
	}
	prefs, err := resolvePreferences(project.Path, project.Name)
	if err != nil {
		return false, ""
	}
	if prefs.Commit.Disable != nil && *prefs.Commit.Disable {
		return true, "commit.disable=true in workflow preferences"
	}
	return false, ""
}

func indentMessage(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = "  " + l
		}
	}
	return strings.Join(lines, "\n")
}
