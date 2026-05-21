package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/ui"
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
	return runRefresh(projectFilter, stdRefreshConfigLoader{})
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

	ui.Success("Proposal approved")
	fmt.Fprintf(os.Stdout, fmtIndentedLine, proposal.ID)
	return nil
}

func runReviewReject(id, reason string, deps reviewDeps) error {
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

func oneLine(s string) string {
	s = strings.TrimSpace(s)
	lines := strings.Split(s, "\n")
	if len(lines) == 0 {
		return ""
	}
	return strings.TrimSpace(lines[0])
}
