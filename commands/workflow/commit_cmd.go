// commit_cmd.go wires the `da workflow commit` subcommand. It composes the
// pure path-derivation in commit_pathset.go with a small interface seam over
// git so the orchestration is testable without a real worktree, and stages /
// commits only the derived set — the "never -A" rule the spec mandates.
package workflow

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

// gitOps is the minimal git surface `da workflow commit` needs. Interface-DI
// (not a func-var) per the codebase's prefer-interface-di-over-funcvar-seams
// lesson; tests inject a stub, production wires execGit{}.
type gitOps interface {
	Status() ([]byte, error)
	AddPaths(paths []string) error
	Commit(message string) error
}

// execGit is the production implementation: shells out to git via os/exec.
// `git status --porcelain=v2 -z` is the contract ParseStatus expects.
type execGit struct{}

func (execGit) Status() ([]byte, error) {
	// --untracked-files=all is required: git's default ("normal") collapses
	// an entire untracked directory tree to a single directory entry, so a
	// fresh `.agents/workflow/plans/<id>/PLAN.yaml` would never appear as
	// its own path — only `.agents/` would. The derivation logic operates
	// on file paths, not directory hints.
	cmd := exec.Command("git", "status", "--porcelain=v2", "-z", "--untracked-files=all")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git status: %w: %s", err, stderr.String())
	}
	return stdout.Bytes(), nil
}

func (execGit) AddPaths(paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	args := append([]string{"add", "--"}, paths...)
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git add: %w: %s", err, out)
	}
	return nil
}

func (execGit) Commit(message string) error {
	cmd := exec.Command("git", "commit", "-F", "-")
	cmd.Stdin = strings.NewReader(message)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git commit: %w: %s", err, out)
	}
	return nil
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
			return runWorkflowCommit(cmd.OutOrStdout(), execGit{}, dryRun, includes)
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
	raw, err := git.Status()
	if err != nil {
		return fmt.Errorf("workflow commit: %w", err)
	}
	entries, err := ParseStatus(raw)
	if err != nil {
		return fmt.Errorf("workflow commit: parse status: %w", err)
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
		return fmt.Errorf("workflow commit: %w", err)
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
	return runWorkflowCommit(out, execGit{}, false, nil)
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
