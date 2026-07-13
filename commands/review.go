package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/journal"
	"github.com/AGOrcha/dot-agents/internal/ui"
	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
)

const fmtIndentedLine = "  %s\n\n"

const reviewProposalIDHint = "Pass the proposal ID from `da review`."

// reviewDeps is the narrow collaborator runReviewApprove, runReviewReject, and
// captureProposalRollback need (interface-DI per docs/TEST_SEAMS.md). One
// interface covers both the os-level rollback touch points (MkdirAll,
// WriteFile, Remove) and the higher-order workflow operations
// (ApplyProposal, ArchiveProposal, RunRefresh) so review's approve pipeline
// has a single fault-injection surface. File-scoped — do not share with
// other commands files.
type reviewDeps interface {
	MkdirAll(path string, perm os.FileMode) error
	WriteFile(name string, data []byte, perm os.FileMode) error
	Remove(name string) error
	ApplyProposal(proposal *config.Proposal) error
	ArchiveProposal(proposal *config.Proposal) error
	RunRefresh(projectFilter string) error
}

// stdReviewDeps is the production reviewDeps backed by the os package and the
// real config / runRefresh entry points. RunRefresh mirrors the legacy
// runRefreshFn wrap so the default refresh path still threads
// stdRefreshConfigLoader{} into runRefresh.
type stdReviewDeps struct{}

func (stdReviewDeps) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}
func (stdReviewDeps) WriteFile(name string, data []byte, perm os.FileMode) error {
	return os.WriteFile(name, data, perm)
}
func (stdReviewDeps) Remove(name string) error                 { return os.Remove(name) }
func (stdReviewDeps) ApplyProposal(p *config.Proposal) error   { return config.ApplyProposal(p) }
func (stdReviewDeps) ArchiveProposal(p *config.Proposal) error { return config.ArchiveProposal(p) }
func (stdReviewDeps) RunRefresh(projectFilter string) error {
	return runRefresh(projectFilter, stdRefreshConfigLoader{}, stdImportDeps{}, stdAddDeps{})
}

func NewReviewCmd() *cobra.Command {
	var rejectReason string

	cmd := &cobra.Command{
		Use:   "review",
		Short: "Review pending workflow proposals",
		Long: `Lists and applies queued shared-workflow proposals stored under ~/.agents/proposals.
This is the approval surface for shared preference and rule changes that should
not be applied silently.`,
		Example: ExampleBlock(
			"  da review",
			"  da review show pref-default-model",
			"  da review approve pref-default-model",
		),
		Args: NoArgsWithHints("Use `da review` with no positional args to list pending proposals."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReviewList()
		},
	}

	showCmd := &cobra.Command{
		Use:   "show <id>",
		Short: "Show a pending proposal",
		Example: ExampleBlock(
			"  da review show pref-default-model",
		),
		Args: ExactArgsWithHints(1, reviewProposalIDHint),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReviewShow(args[0])
		},
	}

	approveCmd := &cobra.Command{
		Use:   "approve <id>",
		Short: "Approve and apply a pending proposal",
		Example: ExampleBlock(
			"  da review approve pref-default-model",
		),
		Args: ExactArgsWithHints(1, reviewProposalIDHint),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReviewApprove(args[0], stdReviewDeps{})
		},
	}

	rejectCmd := &cobra.Command{
		Use:   "reject <id>",
		Short: "Reject a pending proposal",
		Example: ExampleBlock(
			"  da review reject pref-default-model --reason \"not ready\"",
		),
		Args: ExactArgsWithHints(1, reviewProposalIDHint),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReviewReject(args[0], rejectReason, stdReviewDeps{})
		},
	}
	rejectCmd.Flags().StringVar(&rejectReason, "reason", "", "Reason for rejection")

	cmd.AddCommand(showCmd, approveCmd, rejectCmd)
	return cmd
}

func runReviewList() error {
	proposals, err := config.ListPendingProposals()
	if err != nil {
		return err
	}
	if len(proposals) == 0 {
		ui.Info("No pending proposals.")
		return nil
	}

	ui.Header("Pending Proposals")
	for _, proposal := range proposals {
		fmt.Fprintf(os.Stdout, "  %s%s%s\n", ui.Bold, proposal.ID, ui.Reset)
		fmt.Fprintf(os.Stdout, "  %s%s%s  %s%s%s  %s\n", ui.Cyan, proposal.Type, ui.Reset, ui.Dim, proposal.Action, ui.Reset, proposal.Target)
		fmt.Fprintf(os.Stdout, fmtIndentedLine, oneLine(proposal.Rationale))
	}
	return nil
}

func runReviewShow(id string) error {
	proposal, err := config.LoadProposal(id)
	if err != nil {
		return err
	}
	if err := config.ValidateProposal(proposal); err != nil {
		return err
	}

	ui.Header("Proposal " + proposal.ID)
	content, err := yaml.Marshal(proposal)
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, string(content))
	return nil
}

func runReviewApprove(id string, deps reviewDeps) error {
	// Review decision event (TierReview → durable_delta): record the approve
	// outcome after the proposal is applied + archived. ok flips true once the
	// archive lands; a failure on any earlier step records an input-only failed
	// event (mirrors p3a's deferred Tier-1 tail). Best-effort, never fatal.
	repoPath := reviewJournalRepo()
	input := &journal.ReviewInput{ProposalID: id}
	observed := &journal.ReviewObserved{Decision: "approved"}
	ok := false
	defer func() { journalReview(repoPath, journal.CmdReviewApprove, input, observed, ok) }()

	proposal, err := config.LoadProposal(id)
	if err != nil {
		return err
	}
	if err := config.ValidateProposal(proposal); err != nil {
		return err
	}
	if proposal.Status != "pending" {
		return fmt.Errorf("proposal %q is %s, not pending", proposal.ID, proposal.Status)
	}

	targetPath, err := config.ProposalTargetPath(proposal.Target)
	if err != nil {
		return err
	}
	restore, err := captureProposalRollback(targetPath, deps)
	if err != nil {
		return err
	}

	if err := deps.ApplyProposal(proposal); err != nil {
		return err
	}
	if err := deps.RunRefresh(""); err != nil {
		_ = restore()
		return fmt.Errorf("refresh after apply: %w", err)
	}

	config.MarkProposalReviewed(proposal, "approved", "")
	if err := deps.ArchiveProposal(proposal); err != nil {
		_ = restore()
		return err
	}

	observed.Applied = true
	observed.RefreshTriggered = true
	ok = true
	ui.Success("Proposal approved")
	fmt.Fprintf(os.Stdout, fmtIndentedLine, proposal.ID)
	return nil
}

func runReviewReject(id, reason string, deps reviewDeps) error {
	// Review decision event (TierReview → durable_delta): record the reject
	// outcome after the proposal is archived. A reject neither applies the
	// proposal nor triggers a refresh, so both flags stay false.
	repoPath := reviewJournalRepo()
	input := &journal.ReviewInput{ProposalID: id, Reason: reason}
	observed := &journal.ReviewObserved{Decision: "rejected"}
	ok := false
	defer func() { journalReview(repoPath, journal.CmdReviewReject, input, observed, ok) }()

	proposal, err := config.LoadProposal(id)
	if err != nil {
		return err
	}
	if err := config.ValidateProposal(proposal); err != nil {
		return err
	}
	if proposal.Status != "pending" {
		return fmt.Errorf("proposal %q is %s, not pending", proposal.ID, proposal.Status)
	}
	config.MarkProposalReviewed(proposal, "rejected", reason)
	if err := deps.ArchiveProposal(proposal); err != nil {
		return err
	}
	ok = true
	ui.Success("Proposal rejected")
	fmt.Fprintf(os.Stdout, fmtIndentedLine, proposal.ID)
	return nil
}

// captureProposalRollback snapshots the contents of targetPath (if any) and
// returns a closure that restores them. The closure captures deps so it can
// fault-inject mkdir/write/remove failures during rollback.
func captureProposalRollback(targetPath string, deps reviewDeps) (func() error, error) {
	content, err := os.ReadFile(targetPath)
	if err == nil {
		original := append([]byte{}, content...)
		return func() error {
			if err := deps.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return err
			}
			return deps.WriteFile(targetPath, original, 0644)
		}, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	return func() error {
		if err := deps.Remove(targetPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}, nil
}

// ── session-handoff journal wiring (p3b) ────────────────────────────────────────
//
// The review commands are the only journaled mutators in package commands, so the
// emit seam + helpers live here rather than in a shared file (mirroring p3a's
// commands/workflow/journal_emit.go in spirit, replicated minimally to avoid a
// cross-package import). Emission is BEST-EFFORT and NON-FATAL: a journal error
// is warned and swallowed — it must never turn a successful decision into a failed
// command. A blank repoPath skips emission.

// reviewJournalEmit is the append seam over journal.Emit, overridable in tests so
// they can capture the typed envelope (or inject an append failure) without
// touching the real per-repo state dir.
var reviewJournalEmit = journal.Emit

// reviewJournalRepo resolves the repo the review ran in (the journal key). The
// CLI has no project handle here, so the cwd is the repo identity; a getwd
// failure yields "" which skips emission.
func reviewJournalRepo() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	return dir
}

// emitReviewEvent runs build (NewEvent / NewFailedEvent) and appends the result
// best-effort. A build or append error is warned, never returned; a blank
// repoPath is a no-op.
func emitReviewEvent(repoPath, command string, build func() (journal.Envelope, error)) {
	if repoPath == "" {
		return
	}
	ev, err := build()
	if err == nil {
		err = reviewJournalEmit(repoPath, ev)
	}
	if err != nil {
		ui.Warn(fmt.Sprintf("journal: %s: %v", command, err))
	}
}

// journalReview is the deferred tail a review runner registers: on success it
// records the decision outcome (durable_delta); on failure (the decision did not
// complete) it records an input-only failed event. ok flips true only once the
// proposal archive has landed, so a render error after still records success.
func journalReview(repoPath, command string, input, observed any, ok bool) {
	emitReviewEvent(repoPath, command, func() (journal.Envelope, error) {
		if ok {
			return journal.NewEvent(command, journal.ActorMain, input, observed)
		}
		return journal.NewFailedEvent(command, journal.ActorMain, input)
	})
}

func oneLine(s string) string {
	s = strings.TrimSpace(s)
	lines := strings.Split(s, "\n")
	if len(lines) == 0 {
		return ""
	}
	return strings.TrimSpace(lines[0])
}
