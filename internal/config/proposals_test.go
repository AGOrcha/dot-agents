package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateProposalTarget(t *testing.T) {
	cases := []struct {
		target string
		valid  bool
	}{
		{"rules/global/rules.mdc", true},
		{"skills/proj/deploy/SKILL.md", true},
		{"/tmp/nope", false},
		{"../escape", false},
		{"", false},
	}
	for _, tc := range cases {
		err := ValidateProposalTarget(tc.target)
		if tc.valid && err != nil {
			t.Fatalf("target %q should be valid: %v", tc.target, err)
		}
		if !tc.valid && err == nil {
			t.Fatalf("target %q should be invalid", tc.target)
		}
	}
}

func TestApplyProposalWritesTargetUnderAgentsHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)

	proposal := &Proposal{
		SchemaVersion: 1,
		ID:            "one",
		Status:        "pending",
		Type:          "rule",
		Action:        "add",
		Target:        "rules/global/go.mdc",
		Rationale:     "test",
		Content:       "hello\n",
		CreatedAt:     "2026-04-10T00:00:00Z",
		CreatedBy:     "test",
	}
	if err := ApplyProposal(proposal); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(home, "rules", "global", "go.mdc"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello\n" {
		t.Fatalf("got %q", string(got))
	}
}

func TestArchiveProposalMovesPendingFileToArchived(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)

	proposal := &Proposal{
		SchemaVersion: 1,
		ID:            "one",
		Status:        "approved",
		Type:          "rule",
		Action:        "add",
		Target:        "rules/global/go.mdc",
		Rationale:     "test",
		Content:       "hello\n",
		CreatedAt:     "2026-04-10T00:00:00Z",
		CreatedBy:     "test",
	}
	if err := SaveProposal(proposal, ProposalPath(proposal.ID)); err != nil {
		t.Fatal(err)
	}
	if err := ArchiveProposal(proposal); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ProposalPath(proposal.ID)); !os.IsNotExist(err) {
		t.Fatalf("proposal should be removed from pending dir")
	}
	if _, err := os.Stat(ArchivedProposalPath(proposal.ID)); err != nil {
		t.Fatalf("archived proposal missing: %v", err)
	}
}

func TestLoadProposalMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)

	_, err := LoadProposal("missing")
	if !errors.Is(err, ErrProposalNotFound) {
		t.Fatalf("expected ErrProposalNotFound, got %v", err)
	}
}
