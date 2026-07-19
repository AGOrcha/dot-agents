package workflow

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── proposalAbsPathFromRoutedTo ──────────────────────────────────────────────

func TestProposalAbsPathFromRoutedTo_ValidRoute(t *testing.T) {
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	got, err := proposalAbsPathFromRoutedTo("proposal:obs-12345.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(agentsHome, "proposals", "obs-12345.md")
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestProposalAbsPathFromRoutedTo_RejectsNonProposalRoute(t *testing.T) {
	_, err := proposalAbsPathFromRoutedTo("task-note:foo")
	if err == nil {
		t.Fatal("expected error for non-proposal route")
	}
	if !strings.Contains(err.Error(), "not a proposal route") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestProposalAbsPathFromRoutedTo_RejectsEmptyName(t *testing.T) {
	_, err := proposalAbsPathFromRoutedTo("proposal:")
	if err == nil {
		t.Fatal("expected error for empty proposal name")
	}
}

func TestProposalAbsPathFromRoutedTo_RejectsTraversal(t *testing.T) {
	cases := []string{
		"proposal:../etc/passwd",
		"proposal:sub/dir.md",
		"proposal:..",
	}
	for _, in := range cases {
		_, err := proposalAbsPathFromRoutedTo(in)
		if err == nil {
			t.Errorf("expected error for %q", in)
		}
	}
}

// ── readFoldBackProposalFile ─────────────────────────────────────────────────

func TestReadFoldBackProposalFile_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "proposal.md")
	content := `---
title: Test Proposal
observation: original observation
plan_id: p1
task_id: t1
created_at: "2026-04-15T00:00:00Z"
---

Body of the proposal goes here.
Multiple lines allowed.
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	fm, body, err := readFoldBackProposalFile(path)
	if err != nil {
		t.Fatalf("readFoldBackProposalFile: %v", err)
	}
	if fm.Title != "Test Proposal" {
		t.Errorf("Title = %q", fm.Title)
	}
	if fm.Observation != "original observation" {
		t.Errorf("Observation = %q", fm.Observation)
	}
	if fm.PlanID != "p1" || fm.TaskID != "t1" {
		t.Errorf("ids = %q/%q", fm.PlanID, fm.TaskID)
	}
	if !strings.Contains(body, "Body of the proposal") {
		t.Errorf("body missing content: %q", body)
	}
}

func TestReadFoldBackProposalFile_MissingFile(t *testing.T) {
	_, _, err := readFoldBackProposalFile(filepath.Join(t.TempDir(), "no-such-file.md"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestReadFoldBackProposalFile_MissingFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "no-fm.md")
	if err := os.WriteFile(path, []byte("just body, no frontmatter\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := readFoldBackProposalFile(path)
	if err == nil || !strings.Contains(err.Error(), "missing frontmatter") {
		t.Errorf("expected missing-frontmatter error; got %v", err)
	}
}

func TestReadFoldBackProposalFile_UnterminatedFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "open-fm.md")
	if err := os.WriteFile(path, []byte("---\ntitle: x\nplan_id: p1\nbody never closes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := readFoldBackProposalFile(path)
	if err == nil || !strings.Contains(err.Error(), "unterminated frontmatter") {
		t.Errorf("expected unterminated-frontmatter error; got %v", err)
	}
}

func TestReadFoldBackProposalFile_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad-yaml.md")
	if err := os.WriteFile(path, []byte("---\ntitle: : : not valid\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := readFoldBackProposalFile(path)
	if err == nil {
		t.Error("expected yaml parse error")
	}
}

// ── updateExistingProposalFoldBack ────────────────────────────────────────────

func TestUpdateExistingProposalFoldBack_RewritesObservation(t *testing.T) {
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	if err := os.MkdirAll(filepath.Join(agentsHome, "proposals"), 0o755); err != nil {
		t.Fatal(err)
	}
	propPath := filepath.Join(agentsHome, "proposals", "obs-existing.md")
	initial := `---
title: Existing
observation: old text
plan_id: p1
task_id: t1
created_at: "2026-04-10T00:00:00Z"
---

original body
`
	if err := os.WriteFile(propPath, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	prior := &foldBackArtifact{
		ID:             "obs-existing",
		PlanID:         "p1",
		TaskID:         "t1",
		Classification: "proposal",
		RoutedTo:       "proposal:obs-existing.md",
		CreatedAt:      "2026-04-10T00:00:00Z",
	}
	artifact := &foldBackArtifact{
		SchemaVersion: 1,
		PlanID:        "p1",
		Observation:   "new updated text",
	}
	if err := updateExistingProposalFoldBack(prior, "new updated text", artifact); err != nil {
		t.Fatalf("updateExistingProposalFoldBack: %v", err)
	}
	// Verify artifact mutated.
	if artifact.Classification != "proposal" {
		t.Errorf("Classification = %q", artifact.Classification)
	}
	if artifact.TaskID != "t1" {
		t.Errorf("TaskID = %q", artifact.TaskID)
	}
	if artifact.RoutedTo != "proposal:obs-existing.md" {
		t.Errorf("RoutedTo = %q", artifact.RoutedTo)
	}
	// Verify proposal on disk rewritten with new observation.
	updated, err := os.ReadFile(propPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updated), "new updated text") {
		t.Errorf("proposal file did not contain new observation:\n%s", updated)
	}
	if strings.Contains(string(updated), "old text") {
		// 'old text' might still appear inside the YAML key — instead ensure
		// the observation field was rewritten.
		fm, _, err := readFoldBackProposalFile(propPath)
		if err != nil {
			t.Fatal(err)
		}
		if fm.Observation != "new updated text" {
			t.Errorf("observation not updated; got %q", fm.Observation)
		}
	}
}

func TestUpdateExistingProposalFoldBack_InvalidRouteErrors(t *testing.T) {
	prior := &foldBackArtifact{
		RoutedTo: "task-note:p1/t1",
	}
	artifact := &foldBackArtifact{}
	err := updateExistingProposalFoldBack(prior, "obs", artifact)
	if err == nil {
		t.Fatal("expected error for non-proposal route")
	}
}

func TestUpdateExistingProposalFoldBack_MissingProposalFileErrors(t *testing.T) {
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	// proposals dir not created → read will fail.

	prior := &foldBackArtifact{
		RoutedTo: "proposal:ghost.md",
	}
	artifact := &foldBackArtifact{}
	err := updateExistingProposalFoldBack(prior, "obs", artifact)
	if err == nil {
		t.Fatal("expected error when proposal file missing")
	}
	if !strings.Contains(err.Error(), "read proposal") {
		t.Errorf("expected read-proposal error; got %v", err)
	}
}

// ── renderFoldBackList ───────────────────────────────────────────────────────

func TestRenderFoldBackList_EmptyShowsPlaceholder(t *testing.T) {
	var buf bytes.Buffer
	if err := renderFoldBackList(&buf, nil); err != nil {
		t.Fatalf("renderFoldBackList: %v", err)
	}
	if !strings.Contains(buf.String(), "No fold-back observations recorded.") {
		t.Errorf("missing empty placeholder:\n%s", buf.String())
	}
}

func TestRenderFoldBackList_RendersRows(t *testing.T) {
	var buf bytes.Buffer
	artifacts := []foldBackArtifact{
		{
			ID:             "fb-1",
			PlanID:         "p1",
			TaskID:         "t1",
			Classification: "small",
			RoutedTo:       "task-note:p1/t1",
			CreatedAt:      "2026-04-15T00:00:00Z",
		},
		{
			ID:             "fb-2",
			PlanID:         "p1",
			TaskID:         "", // plan-scoped → renders as em-dash
			Classification: "proposal",
			RoutedTo:       "proposal:obs.md",
			CreatedAt:      "2026-04-16T00:00:00Z",
		},
	}
	if err := renderFoldBackList(&buf, artifacts); err != nil {
		t.Fatalf("renderFoldBackList: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"Fold-back observations",
		"fb-1", "fb-2",
		"task-note:p1/t1", "proposal:obs.md",
		"small", "proposal",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "—") {
		t.Errorf("expected em-dash for blank task col:\n%s", out)
	}
}

// ── foldBackProposalFrontmatter write_scope round-trip ───────────────────────

func TestFoldBackProposalFile_WriteScopeRoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "obs.md")
	fm := foldBackProposalFrontmatter{
		Title:       "Fold-back: p1",
		Observation: "obs",
		PlanID:      "p1",
		WriteScope:  []string{"internal/config/", "commands/workflow/types.go"},
		CreatedAt:   "2026-04-15T00:00:00Z",
	}
	if err := writeFoldBackProposalFile(path, fm, "obs"); err != nil {
		t.Fatalf("writeFoldBackProposalFile: %v", err)
	}
	got, _, err := readFoldBackProposalFile(path)
	if err != nil {
		t.Fatalf("readFoldBackProposalFile: %v", err)
	}
	if len(got.WriteScope) != 2 || got.WriteScope[0] != "internal/config/" || got.WriteScope[1] != "commands/workflow/types.go" {
		t.Fatalf("write_scope did not round-trip: %v", got.WriteScope)
	}
}

func TestFoldBackProposalFile_EmptyWriteScopeOmitted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "obs.md")
	fm := foldBackProposalFrontmatter{Title: "t", Observation: "o", PlanID: "p1", CreatedAt: "2026-04-15T00:00:00Z"}
	if err := writeFoldBackProposalFile(path, fm, "o"); err != nil {
		t.Fatalf("writeFoldBackProposalFile: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "write_scope") {
		t.Errorf("empty write_scope should be omitted from frontmatter:\n%s", string(data))
	}
	got, _, err := readFoldBackProposalFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.WriteScope) != 0 {
		t.Errorf("expected empty write_scope, got %v", got.WriteScope)
	}
}
