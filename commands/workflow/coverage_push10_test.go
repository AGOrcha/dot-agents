// Package workflow — tiny final push to clear the last few statements
// needed to cross the 95% coverage threshold.
package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ─── state.go: firstMarkdownTitle falls back to filename when no heading ──

func TestFirstMarkdownTitle_NoHeading(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "plain.md")
	if err := os.WriteFile(p, []byte("body only\nno heading at all\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := firstMarkdownTitle(p)
	if err != nil {
		t.Fatal(err)
	}
	if got != "plain.md" {
		t.Fatalf("expected filename fallback, got %q", got)
	}
}

// ─── state.go: readWorkflowPlan uses fallback when no pending items ──────

func TestReadWorkflowPlan_FallbackItems(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "x.plan.md")
	// First line: heading. Body has no checkboxes but has plain bullets.
	content := "# Plan\n- first fallback\n- second\n"
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	summary, err := readWorkflowPlan(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.PendingItems) == 0 {
		t.Fatal("expected fallback items to populate PendingItems")
	}
}

// ─── state.go: collectWorkflowLessons skips blank lines ──────────────────

func TestCollectWorkflowLessons_SkipsBlankLines(t *testing.T) {
	repo := t.TempDir()
	dir := filepath.Join(repo, ".agents", "lessons")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	// File with blank lines mixed in.
	body := "- lesson one\n\n  \n- lesson two\n"
	if err := os.WriteFile(filepath.Join(dir, "index.md"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	out, warns := collectWorkflowLessons(repo)
	if len(warns) != 0 {
		t.Fatalf("expected no warnings, got %v", warns)
	}
	for _, l := range out {
		if l == "" {
			t.Fatal("expected blank lines skipped")
		}
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 lessons, got %d: %v", len(out), out)
	}
}

// ─── state.go: collectWorkflowLessons truncates over 10 ──────────────────

func TestCollectWorkflowLessons_Truncate(t *testing.T) {
	repo := t.TempDir()
	dir := filepath.Join(repo, ".agents", "lessons")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	var lines []string
	for i := 0; i < 15; i++ {
		lines = append(lines, "- lesson")
	}
	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "index.md"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	out, _ := collectWorkflowLessons(repo)
	if len(out) != 10 {
		t.Fatalf("expected 10 (truncated), got %d", len(out))
	}
}

// ─── state.go: runWorkflowOrient with state warnings ────────────────────

func TestRunWorkflowOrient_TextRender(t *testing.T) {
	repo := setupTestProject(t)
	chdirForCov(t, repo)
	out, err := captureCovStdout(t, runWorkflowOrient)
	if err != nil {
		t.Fatalf("orient: %v", err)
	}
	if !strings.Contains(out, "Project") {
		t.Fatalf("expected text render, got: %s", out)
	}
}
