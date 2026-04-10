package commands

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/config"
)

func writeTestProposal(t *testing.T, proposal config.Proposal) {
	t.Helper()
	if err := config.SaveProposal(&proposal, config.ProposalPath(proposal.ID)); err != nil {
		t.Fatal(err)
	}
}

func TestRunReviewApproveAppliesProposalAndArchivesIt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)

	proposal := config.Proposal{
		SchemaVersion: 1,
		ID:            "add-go-rule",
		Status:        "pending",
		Type:          "rule",
		Action:        "add",
		Target:        "rules/global/go.mdc",
		Rationale:     "Need a shared Go rule",
		Content:       "go rule\n",
		CreatedAt:     "2026-04-10T00:00:00Z",
		CreatedBy:     "test",
	}
	writeTestProposal(t, proposal)

	if err := runReviewApprove(proposal.ID); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(home, "rules", "global", "go.mdc"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "go rule\n" {
		t.Fatalf("applied content = %q", string(got))
	}
	if _, err := os.Stat(config.ProposalPath(proposal.ID)); !os.IsNotExist(err) {
		t.Fatalf("proposal should be removed from pending dir")
	}
	archived, err := os.ReadFile(config.ArchivedProposalPath(proposal.ID))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(archived), "status: approved") {
		t.Fatalf("archived proposal missing approved status:\n%s", string(archived))
	}
}

func TestRunReviewRejectArchivesProposalWithoutApplyingIt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)

	proposal := config.Proposal{
		SchemaVersion: 1,
		ID:            "reject-me",
		Status:        "pending",
		Type:          "rule",
		Action:        "add",
		Target:        "rules/global/go.mdc",
		Rationale:     "Need a shared Go rule",
		Content:       "go rule\n",
		CreatedAt:     "2026-04-10T00:00:00Z",
		CreatedBy:     "test",
	}
	writeTestProposal(t, proposal)

	if err := runReviewReject(proposal.ID, "not now"); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(home, "rules", "global", "go.mdc")); !os.IsNotExist(err) {
		t.Fatalf("target should not be created on reject")
	}
	archived, err := os.ReadFile(config.ArchivedProposalPath(proposal.ID))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(archived), "status: rejected") {
		t.Fatalf("archived proposal missing rejected status:\n%s", string(archived))
	}
	if !strings.Contains(string(archived), "review_reason: not now") {
		t.Fatalf("archived proposal missing review reason:\n%s", string(archived))
	}
}

func TestRunReviewApproveRejectsInvalidTarget(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)

	proposal := config.Proposal{
		SchemaVersion: 1,
		ID:            "bad-target",
		Status:        "pending",
		Type:          "rule",
		Action:        "add",
		Target:        "../escape",
		Rationale:     "bad",
		Content:       "x\n",
		CreatedAt:     "2026-04-10T00:00:00Z",
		CreatedBy:     "test",
	}
	writeTestProposal(t, proposal)

	if err := runReviewApprove(proposal.ID); err == nil {
		t.Fatal("expected invalid target error")
	}
	if _, err := os.Stat(config.ProposalPath(proposal.ID)); err != nil {
		t.Fatalf("proposal should remain pending after failure: %v", err)
	}
}

func TestRunReviewApproveRollsBackTargetWhenRefreshFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)

	targetPath := filepath.Join(home, "rules", "global", "go.mdc")
	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath, []byte("original\n"), 0644); err != nil {
		t.Fatal(err)
	}

	proposal := config.Proposal{
		SchemaVersion: 1,
		ID:            "refresh-fails",
		Status:        "pending",
		Type:          "rule",
		Action:        "modify",
		Target:        "rules/global/go.mdc",
		Rationale:     "Need a new rule",
		Content:       "modified\n",
		CreatedAt:     "2026-04-10T00:00:00Z",
		CreatedBy:     "test",
	}
	writeTestProposal(t, proposal)

	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte("{bad json"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := runReviewApprove(proposal.ID); err == nil {
		t.Fatal("expected refresh failure")
	}
	got, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "original\n" {
		t.Fatalf("target was not rolled back: %q", string(got))
	}
	if _, err := os.Stat(config.ProposalPath(proposal.ID)); err != nil {
		t.Fatalf("proposal should remain pending after refresh failure: %v", err)
	}
}

func TestRunReviewListShowsPendingOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)

	writeTestProposal(t, config.Proposal{
		SchemaVersion: 1,
		ID:            "pending-one",
		Status:        "pending",
		Type:          "rule",
		Action:        "add",
		Target:        "rules/global/a.mdc",
		Rationale:     "one",
		Content:       "a\n",
		CreatedAt:     "2026-04-10T00:00:00Z",
		CreatedBy:     "test",
	})
	if err := os.MkdirAll(config.ArchivedProposalsDir(), 0755); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveProposal(&config.Proposal{
		SchemaVersion: 1,
		ID:            "approved-one",
		Status:        "approved",
		Type:          "rule",
		Action:        "add",
		Target:        "rules/global/b.mdc",
		Rationale:     "two",
		Content:       "b\n",
		CreatedAt:     "2026-04-10T00:00:00Z",
		CreatedBy:     "test",
	}, filepath.Join(config.ArchivedProposalsDir(), "approved-one.yaml")); err != nil {
		t.Fatal(err)
	}

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	if err := runReviewList(); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	os.Stdout = oldStdout

	output, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	rendered := string(output)
	if !strings.Contains(rendered, "pending-one") {
		t.Fatalf("pending proposal missing from output:\n%s", rendered)
	}
	if strings.Contains(rendered, "approved-one") {
		t.Fatalf("archived proposal should not appear in pending list:\n%s", rendered)
	}
}
