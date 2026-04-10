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

func NewReviewCmd() *cobra.Command {
	var rejectReason string

	cmd := &cobra.Command{
		Use:   "review",
		Short: "Review pending workflow proposals",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReviewList()
		},
	}

	showCmd := &cobra.Command{
		Use:   "show <id>",
		Short: "Show a pending proposal",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReviewShow(args[0])
		},
	}

	approveCmd := &cobra.Command{
		Use:   "approve <id>",
		Short: "Approve and apply a pending proposal",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReviewApprove(args[0])
		},
	}

	rejectCmd := &cobra.Command{
		Use:   "reject <id>",
		Short: "Reject a pending proposal",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReviewReject(args[0], rejectReason)
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
		fmt.Fprintf(os.Stdout, "  %s\n\n", oneLine(proposal.Rationale))
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

func runReviewApprove(id string) error {
	proposal, err := config.LoadProposal(id)
	if err != nil {
		return err
	}
	if err := config.ValidateProposal(proposal); err != nil {
		return err
	}

	targetPath, err := config.ProposalTargetPath(proposal.Target)
	if err != nil {
		return err
	}
	restore, err := captureProposalRollback(targetPath)
	if err != nil {
		return err
	}

	if err := config.ApplyProposal(proposal); err != nil {
		return err
	}
	if err := runRefresh(""); err != nil {
		_ = restore()
		return fmt.Errorf("refresh after apply: %w", err)
	}

	config.MarkProposalReviewed(proposal, "approved", "")
	if err := config.ArchiveProposal(proposal); err != nil {
		_ = restore()
		return err
	}

	ui.Success("Proposal approved")
	fmt.Fprintf(os.Stdout, "  %s\n\n", proposal.ID)
	return nil
}

func runReviewReject(id, reason string) error {
	proposal, err := config.LoadProposal(id)
	if err != nil {
		return err
	}
	if err := config.ValidateProposal(proposal); err != nil {
		return err
	}
	config.MarkProposalReviewed(proposal, "rejected", reason)
	if err := config.ArchiveProposal(proposal); err != nil {
		return err
	}
	ui.Success("Proposal rejected")
	fmt.Fprintf(os.Stdout, "  %s\n\n", proposal.ID)
	return nil
}

func captureProposalRollback(targetPath string) (func() error, error) {
	content, err := os.ReadFile(targetPath)
	if err == nil {
		original := append([]byte{}, content...)
		return func() error {
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return err
			}
			return os.WriteFile(targetPath, original, 0644)
		}, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	return func() error {
		if err := os.Remove(targetPath); err != nil && !os.IsNotExist(err) {
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
