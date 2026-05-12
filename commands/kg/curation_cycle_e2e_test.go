package kg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestKGCurationCycle_IngestThroughCompact exercises the full curation state
// machine — setup → ingest → warm → query → lint → reweave → mark-stale →
// compact — against a synthetic KG_HOME. It is the single end-to-end test that
// catches drift between commands and locks in the cross-cutting invariants
// each step relies on (reweave preserves note bodies, lint reports persist,
// mark-stale promotes status, compact moves notes into _archived).
func TestKGCurationCycle_IngestThroughCompact(t *testing.T) {
	// ── Step 1: setup ──────────────────────────────────────────────────────
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("runKGSetup: %v", err)
	}

	// Sanity-check baseline directory layout: notes subdirs + raw inbox exist.
	for _, sub := range []string{
		filepath.Join("notes", "sources"),
		filepath.Join("notes", "entities"),
		filepath.Join("notes", "decisions"),
		filepath.Join("raw", "inbox"),
	} {
		if _, err := os.Stat(filepath.Join(home, sub)); err != nil {
			t.Fatalf("expected %s to exist after setup: %v", sub, err)
		}
	}

	// ── Step 2: ingest a synthetic source file ─────────────────────────────
	srcFile := filepath.Join(t.TempDir(), "curation-doc.md")
	body := strings.Join([]string{
		"# Curation Doc",
		"",
		"We decided to use Cobra for the CLI surface.",
		"The Knowledge Graph stores notes as Markdown.",
		"",
		"- Should use Markdown for note bodies",
	}, "\n")
	if err := os.WriteFile(srcFile, []byte(body), 0644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	deps := testDeps()
	captureStdout(t, func() {
		if err := runKGIngest(deps, newIngestCmd(false, false, "", "markdown"), []string{srcFile}); err != nil {
			t.Fatalf("runKGIngest: %v", err)
		}
	})

	// Source note must exist.
	if exists, _ := noteExists(home, "src-curation-doc"); !exists {
		t.Fatal("expected src-curation-doc note after ingest")
	}
	// At least one decision note extracted from the body.
	decEntries, _ := os.ReadDir(filepath.Join(home, "notes", "decisions"))
	hasDecision := false
	for _, e := range decEntries {
		if strings.HasPrefix(e.Name(), "dec-curation-doc-") {
			hasDecision = true
			break
		}
	}
	if !hasDecision {
		t.Errorf("expected an extracted decision note, got entries: %v", decEntries)
	}
	// Raw inbox source must have moved to imported.
	if _, err := os.Stat(filepath.Join(home, "raw", "imported", "curation-doc.md")); err != nil {
		t.Errorf("expected raw/imported/curation-doc.md: %v", err)
	}

	// ── Step 3: warm — populate SQLite warm layer ──────────────────────────
	if err := runKGWarm(newKGWarmCmdForTest(), nil); err != nil {
		t.Fatalf("runKGWarm: %v", err)
	}
	store, err := openKGStore(home)
	if err != nil {
		t.Fatalf("openKGStore: %v", err)
	}
	stats, err := store.GetStats()
	if err != nil {
		store.Close()
		t.Fatalf("GetStats: %v", err)
	}
	if stats.NotesCount == 0 {
		t.Error("expected warm layer to contain at least one note after warm")
	}
	store.Close()

	// ── Step 4: query --intent source_lookup must surface ingested source ──
	jsonDeps := Deps{
		Flags:        GlobalFlags{JSON: true},
		ExampleBlock: func(s ...string) string { return strings.Join(s, "\n") },
	}
	out := captureStdout(t, func() {
		if err := runKGQuery(jsonDeps, newQueryCmd("source_lookup", "", 5), []string{"curation-doc.md"}); err != nil {
			t.Fatalf("runKGQuery source_lookup: %v", err)
		}
	})
	var lookupResp GraphQueryResponse
	if err := json.Unmarshal(out, &lookupResp); err != nil {
		t.Fatalf("source_lookup JSON invalid: %v\nraw: %s", err, out)
	}
	foundSource := false
	for _, r := range lookupResp.Results {
		if r.ID == "src-curation-doc" {
			foundSource = true
			break
		}
	}
	if !foundSource {
		t.Errorf("expected source_lookup to surface src-curation-doc, got results=%#v", lookupResp.Results)
	}

	// ── Step 5: seed a broken-link issue ───────────────────────────────────
	now := time.Now().UTC().Format(time.RFC3339)
	brokenNote := makeNote(
		"dec-broken", "decision", "Broken Link Decision",
		"Has a dangling link.", "active", now,
		[]string{"src-curation-doc"}, []string{"missing-target"},
	)
	if err := createGraphNote(home, brokenNote, "## Body\nload-bearing body content.\n"); err != nil {
		t.Fatalf("createGraphNote dec-broken: %v", err)
	}

	// ── Step 6: lint surfaces the broken link, persists report ─────────────
	report, err := runGraphLint(home)
	if err != nil {
		t.Fatalf("runGraphLint #1: %v", err)
	}
	if !lintHasFor(report.Results, "broken_links", "dec-broken") {
		t.Errorf("expected broken_links result for dec-broken, got %#v", report.Results)
	}
	if _, err := os.Stat(filepath.Join(home, "ops", "lint", "lint-report.json")); err != nil {
		t.Errorf("expected lint-report.json after lint: %v", err)
	}

	// ── Step 7: reweave removes the broken link but preserves the body ─────
	if err := runKGReweave(home); err != nil {
		t.Fatalf("runKGReweave: %v", err)
	}
	rewovenData, _ := os.ReadFile(filepath.Join(home, "notes", "decisions", "dec-broken.md"))
	if strings.Contains(string(rewovenData), "missing-target") {
		t.Errorf("expected missing-target link removed after reweave, got:\n%s", rewovenData)
	}
	// Regression for the persistReweavedNote body-loss fix (commit de7ebae):
	// the note body must survive the reweave's frontmatter rewrite.
	if !strings.Contains(string(rewovenData), "load-bearing body content") {
		t.Errorf("expected body preserved across reweave, got:\n%s", rewovenData)
	}

	// ── Step 8: lint after reweave — broken_links count is zero ────────────
	postReweaveReport, err := runGraphLint(home)
	if err != nil {
		t.Fatalf("runGraphLint #2: %v", err)
	}
	if countLintCheck(postReweaveReport.Results, "broken_links") != 0 {
		t.Errorf("expected zero broken_links after reweave, got %d (results=%#v)",
			countLintCheck(postReweaveReport.Results, "broken_links"), postReweaveReport.Results)
	}

	// ── Step 9: seed a stale-pages issue ───────────────────────────────────
	oldTS := time.Now().Add(-200 * 24 * time.Hour).UTC().Format(time.RFC3339)
	staleNote := makeNote(
		"ent-stale-cycle", "entity", "Stale Cycle Entity",
		"Old entity from a previous era.", "active", oldTS,
		[]string{"src-curation-doc"}, nil,
	)
	if err := createGraphNote(home, staleNote, "stale body"); err != nil {
		t.Fatalf("createGraphNote ent-stale-cycle: %v", err)
	}

	// ── Step 10: lint reports the stale entry ──────────────────────────────
	staleReport, err := runGraphLint(home)
	if err != nil {
		t.Fatalf("runGraphLint #3: %v", err)
	}
	if !lintHasFor(staleReport.Results, "stale_pages", "ent-stale-cycle") {
		t.Errorf("expected stale_pages result for ent-stale-cycle, got %#v", staleReport.Results)
	}
	stalePreCount := countLintCheck(staleReport.Results, "stale_pages")

	// ── Step 11: mark-stale promotes the note's status to "stale" ──────────
	if err := runKGMarkStale(home, 90*24*time.Hour); err != nil {
		t.Fatalf("runKGMarkStale: %v", err)
	}
	staleData, _ := os.ReadFile(filepath.Join(home, "notes", "entities", "ent-stale-cycle.md"))
	parsedStale, _, err := parseGraphNote(staleData)
	if err != nil {
		t.Fatalf("parse ent-stale-cycle: %v", err)
	}
	if parsedStale.Status != "stale" {
		t.Errorf("expected ent-stale-cycle status=stale after mark-stale, got %s", parsedStale.Status)
	}

	// ── Step 12: lint after mark-stale — stale_pages count must drop ───────
	// mark-stale rewrites UpdatedAt to time.Now() before saving, so the note
	// is no longer past the stale cutoff and should not be flagged again.
	postMarkReport, err := runGraphLint(home)
	if err != nil {
		t.Fatalf("runGraphLint #4: %v", err)
	}
	stalePostCount := countLintCheck(postMarkReport.Results, "stale_pages")
	if stalePostCount >= stalePreCount {
		t.Errorf("expected stale_pages count to drop after mark-stale (pre=%d post=%d)",
			stalePreCount, stalePostCount)
	}

	// ── Step 13: compact archives superseded/archived notes ────────────────
	// Add an archived note so compact has something to move.
	archived := makeNote(
		"dec-archived-cycle", "decision", "Archived Cycle Decision",
		"Already in archived status.", "archived", now, nil, nil,
	)
	if err := createGraphNote(home, archived, ""); err != nil {
		t.Fatalf("createGraphNote dec-archived-cycle: %v", err)
	}
	if err := runKGCompact(home); err != nil {
		t.Fatalf("runKGCompact: %v", err)
	}
	// The archived note must move under notes/_archived/ and be removed from
	// its original subdirectory + index.
	if _, err := os.Stat(filepath.Join(home, "notes", "_archived", "dec-archived-cycle.md")); err != nil {
		t.Errorf("expected dec-archived-cycle moved to _archived: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "notes", "decisions", "dec-archived-cycle.md")); !os.IsNotExist(err) {
		t.Errorf("expected dec-archived-cycle removed from decisions/, got: %v", err)
	}
	idxData, _ := os.ReadFile(filepath.Join(home, "notes", "index.md"))
	if strings.Contains(string(idxData), "- [dec-archived-cycle]") {
		t.Errorf("expected index entry for dec-archived-cycle removed after compact")
	}
}

// lintHasFor reports whether results contains an entry with the given check
// and note id.
func lintHasFor(results []LintResult, check, noteID string) bool {
	for _, r := range results {
		if r.Check == check && r.NoteID == noteID {
			return true
		}
	}
	return false
}

// countLintCheck returns the number of results whose Check matches.
func countLintCheck(results []LintResult, check string) int {
	n := 0
	for _, r := range results {
		if r.Check == check {
			n++
		}
	}
	return n
}
