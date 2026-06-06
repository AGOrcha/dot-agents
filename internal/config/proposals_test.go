package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func TestValidateProposal_HappyPath(t *testing.T) {
	p := &Proposal{
		SchemaVersion: 1,
		ID:            "x",
		Status:        "pending",
		Type:          "rule",
		Action:        "add",
		Target:        "rules/global/x.md",
		Rationale:     "because",
		Content:       "body",
		CreatedAt:     "2026-04-01T00:00:00Z",
		CreatedBy:     "tester",
	}
	if err := ValidateProposal(p); err != nil {
		t.Errorf("expected valid, got %v", err)
	}
}

func TestValidateProposal_Failures(t *testing.T) {
	base := Proposal{
		SchemaVersion: 1, ID: "x", Status: "pending", Type: "rule", Action: "add",
		Target: "rules/global/x.md", Rationale: "r", Content: "c",
		CreatedAt: "2026-04-01T00:00:00Z", CreatedBy: "t",
	}
	cases := []struct {
		name   string
		mutate func(p *Proposal)
		want   string
	}{
		{"bad schema", func(p *Proposal) { p.SchemaVersion = 99 }, "schema_version"},
		{"empty id", func(p *Proposal) { p.ID = "" }, "id is required"},
		{"bad status", func(p *Proposal) { p.Status = "bogus" }, "invalid status"},
		{"bad type", func(p *Proposal) { p.Type = "bogus" }, "invalid type"},
		{"bad action", func(p *Proposal) { p.Action = "bogus" }, "invalid action"},
		{"bad target", func(p *Proposal) { p.Target = "/abs" }, "invalid proposal target"},
		{"empty rationale", func(p *Proposal) { p.Rationale = "" }, "rationale"},
		{"add empty content", func(p *Proposal) { p.Content = "" }, "content is required"},
		{"remove with content", func(p *Proposal) {
			p.Action = "remove"
			p.Content = "nope"
		}, "must be empty"},
		{"empty created_at", func(p *Proposal) { p.CreatedAt = "" }, "created_at"},
		{"empty created_by", func(p *Proposal) { p.CreatedBy = "" }, "created_by"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := base
			tc.mutate(&p)
			err := ValidateProposal(&p)
			if err == nil {
				t.Fatalf("expected error containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}

func TestValidateProposal_RemoveEmptyContentOK(t *testing.T) {
	p := &Proposal{
		SchemaVersion: 1, ID: "x", Status: "pending", Type: "rule", Action: "remove",
		Target: "rules/global/x.md", Rationale: "r", Content: "",
		CreatedAt: "2026-04-01T00:00:00Z", CreatedBy: "t",
	}
	if err := ValidateProposal(p); err != nil {
		t.Errorf("remove with empty content should be valid, got %v", err)
	}
}

func TestValidateProposalTarget_ExtraCases(t *testing.T) {
	if err := ValidateProposalTarget("   "); err == nil {
		t.Error("whitespace-only target should be invalid")
	}
	if err := ValidateProposalTarget("./"); err == nil {
		t.Error("'./' target should be invalid")
	}
	if err := ValidateProposalTarget(".."); err == nil {
		t.Error("'..' target should be invalid")
	}
}

func TestProposalTargetPath(t *testing.T) {
	t.Setenv("AGENTS_HOME", "/tmp/agents")
	got, err := ProposalTargetPath("rules/global/x.md")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/tmp/agents", "rules", "global", "x.md"); got != want {
		t.Errorf("ProposalTargetPath: got %q, want %q", got, want)
	}

	if _, err := ProposalTargetPath("/absolute"); err == nil {
		t.Error("absolute target should error")
	}
}

func TestListPendingProposals_Empty(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	ps, err := ListPendingProposals()
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 0 {
		t.Errorf("expected empty, got %d", len(ps))
	}
}

func TestListPendingProposals_FiltersStatusAndSortsByID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)

	mk := func(id, status string) {
		p := &Proposal{
			SchemaVersion: 1, ID: id, Status: status, Type: "rule", Action: "add",
			Target: "rules/global/" + id + ".md", Rationale: "r", Content: "c",
			CreatedAt: "2026-04-01T00:00:00Z", CreatedBy: "t",
		}
		if err := SaveProposal(p, ProposalPath(id)); err != nil {
			t.Fatal(err)
		}
	}
	mk("z", "pending")
	mk("a", "pending")
	mk("b", "approved")

	if err := os.MkdirAll(filepath.Join(ProposalsDir(), "subdir"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ProposalsDir(), "stray.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	ps, err := ListPendingProposals()
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 2 {
		t.Fatalf("expected 2 pending, got %d", len(ps))
	}
	if ps[0].ID != "a" || ps[1].ID != "z" {
		t.Errorf("expected sorted [a z], got %v", []string{ps[0].ID, ps[1].ID})
	}
}

func TestLoadProposal_BadYAML(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	if err := os.MkdirAll(ProposalsDir(), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ProposalPath("bad"), []byte(":\n  -bad"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadProposal("bad")
	if err == nil || errors.Is(err, ErrProposalNotFound) {
		t.Errorf("expected parse error, got %v", err)
	}
}

func TestApplyProposal_Remove(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)

	target := filepath.Join(home, "rules", "global", "x.md")
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("body"), 0644); err != nil {
		t.Fatal(err)
	}

	p := &Proposal{Action: "remove", Target: "rules/global/x.md"}
	if err := ApplyProposal(p); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("remove should delete target; stat err=%v", err)
	}

	if err := ApplyProposal(p); err != nil {
		t.Errorf("remove of missing path should be no-op, got %v", err)
	}
}

func TestApplyProposal_UnsupportedAction(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	p := &Proposal{Action: "weird", Target: "rules/global/x.md"}
	if err := ApplyProposal(p); err == nil {
		t.Error("unsupported action should error")
	}
}

func TestApplyProposal_BadTarget(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	p := &Proposal{Action: "add", Target: "/absolute", Content: "x"}
	if err := ApplyProposal(p); err == nil {
		t.Error("absolute target should error")
	}
}

func TestArchiveProposal_NoSourceFileTolerated(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	p := &Proposal{
		SchemaVersion: 1, ID: "ghost", Status: "approved", Type: "rule", Action: "add",
		Target: "rules/global/ghost.md", Rationale: "r", Content: "c",
		CreatedAt: "2026-04-01T00:00:00Z", CreatedBy: "t",
	}

	if err := ArchiveProposal(p); err != nil {
		t.Fatalf("archive should tolerate missing source, got %v", err)
	}
	if _, err := os.Stat(ArchivedProposalPath("ghost")); err != nil {
		t.Errorf("archived file should be present: %v", err)
	}
}

func TestMarkProposalReviewed(t *testing.T) {
	p := &Proposal{}
	MarkProposalReviewed(p, "approved", "looks good")
	if p.Status != "approved" {
		t.Errorf("status: got %q", p.Status)
	}
	if p.ReviewReason != "looks good" {
		t.Errorf("reason: got %q", p.ReviewReason)
	}
	if p.ReviewedAt == "" {
		t.Error("ReviewedAt should be set")
	}
}

func TestLoadProposal_ReadFailureNotIsNotExist(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)

	if err := os.MkdirAll(ProposalsDir(), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(ProposalPath("dir-as-file"), 0755); err != nil {
		t.Fatal(err)
	}
	_, err := LoadProposal("dir-as-file")
	if err == nil {
		t.Fatal("expected read error for directory at proposal path")
	}
	if errors.Is(err, ErrProposalNotFound) {
		t.Errorf("should not be ErrProposalNotFound, got %v", err)
	}
}

func TestListPendingProposals_BadYAMLInDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	if err := os.MkdirAll(ProposalsDir(), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ProposalPath("broken"), []byte(":\n  -nope"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := ListPendingProposals()
	if err == nil {
		t.Error("expected error from malformed yaml")
	}
}

func TestApplyProposal_MkdirAllFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)

	blocker := filepath.Join(home, "rules")
	if err := os.WriteFile(blocker, []byte("blocker"), 0644); err != nil {
		t.Fatal(err)
	}
	p := &Proposal{Action: "add", Target: "rules/global/x.md", Content: "x"}
	if err := ApplyProposal(p); err == nil {
		t.Error("expected MkdirAll failure when parent slot is a regular file")
	}
}

func TestArchiveProposal_MkdirAllFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)

	if err := os.MkdirAll(ProposalsDir(), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ArchivedProposalsDir(), []byte("blocker"), 0644); err != nil {
		t.Fatal(err)
	}
	p := &Proposal{ID: "x"}
	if err := ArchiveProposal(p); err == nil {
		t.Error("expected MkdirAll failure when archived dir slot is a file")
	}
}

func TestSaveProposal_MkdirAllFails(t *testing.T) {
	tmp := t.TempDir()
	blocker := filepath.Join(tmp, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(blocker, "sub", "x.yaml")
	if err := SaveProposal(&Proposal{ID: "x"}, dst); err == nil {
		t.Error("expected error when parent dir cannot be created")
	}
}

func TestProposalDirHelpers(t *testing.T) {
	t.Setenv("AGENTS_HOME", "/tmp/h")
	if want := filepath.Join("/tmp/h", "proposals"); ProposalsDir() != want {
		t.Errorf("ProposalsDir: %q, want %q", ProposalsDir(), want)
	}
	if want := filepath.Join("/tmp/h", "proposals", "archived"); ArchivedProposalsDir() != want {
		t.Errorf("ArchivedProposalsDir: %q, want %q", ArchivedProposalsDir(), want)
	}
	if want := filepath.Join("/tmp/h", "proposals", "foo.yaml"); ProposalPath("foo") != want {
		t.Errorf("ProposalPath: %q, want %q", ProposalPath("foo"), want)
	}
	if want := filepath.Join("/tmp/h", "proposals", "archived", "foo.yaml"); ArchivedProposalPath("foo") != want {
		t.Errorf("ArchivedProposalPath: %q, want %q", ArchivedProposalPath("foo"), want)
	}
}

// proposalFixture returns a minimal valid pending proposal.
func proposalFixture(id string) *Proposal {
	return &Proposal{
		SchemaVersion: 1, ID: id, Status: "pending", Type: "rule",
		Action: "add", Target: "rules/global/go.mdc", Rationale: "r",
		Content: "x\n", CreatedAt: "2026-04-10T00:00:00Z", CreatedBy: "t",
	}
}

// TestLoadProposal_HappyPath covers the success return that no other test exercised.
func TestLoadProposal_HappyPath(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	if err := SaveProposal(proposalFixture("ok"), ProposalPath("ok")); err != nil {
		t.Fatal(err)
	}
	got, err := LoadProposal("ok")
	if err != nil || got == nil || got.ID != "ok" {
		t.Fatalf("LoadProposal happy path: %+v, %v", got, err)
	}
}

// TestListPendingProposals_DirIsFile covers the non-NotExist ReadDir error branch
// (POSIX ENOTDIR when the proposals path is a regular file).
func TestListPendingProposals_DirIsFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	if err := os.MkdirAll(filepath.Dir(ProposalsDir()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ProposalsDir(), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ListPendingProposals(); err == nil {
		t.Fatal("expected ReadDir error when proposals dir is a file")
	}
}

// TestApplyProposal_RemoveError covers the remove-action non-NotExist error branch
// (os.Remove of a non-empty directory).
func TestApplyProposal_RemoveError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	target := filepath.Join(home, "rules", "global", "go.mdc")
	if err := os.MkdirAll(filepath.Join(target, "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := proposalFixture("rm")
	p.Action = "remove"
	if err := ApplyProposal(p); err == nil {
		t.Fatal("expected remove error on a non-empty directory target")
	}
}

// TestArchiveProposal_RemoveSrcError covers ArchiveProposal's os.Remove(src) error
// branch: dst archives fine, but the source path is a non-empty dir that can't be
// removed.
func TestArchiveProposal_RemoveSrcError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	src := ProposalPath("arc")
	if err := os.MkdirAll(filepath.Join(src, "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ArchiveProposal(proposalFixture("arc")); err == nil {
		t.Fatal("expected os.Remove(src) error when src is a non-empty dir")
	}
}
