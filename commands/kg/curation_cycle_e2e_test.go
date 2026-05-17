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
	// KG_HOME / temp-dir setup must outlive every subtest, so bind it to the
	// parent t (newTempKG calls t.Setenv + t.TempDir, both torn down at the
	// owning test's end).
	home := newTempKG(t)
	// State shared across the ordered pipeline steps. Each step is a t.Run
	// subtest so its local control flow is isolated; runStep replicates the
	// original t.Fatalf "abort the whole pipeline" semantics by returning
	// from the parent as soon as a step subtest fails.
	var (
		deps          Deps
		now           string
		stalePreCount int
	)
	runStep := func(name string, fn func(t *testing.T)) bool {
		return t.Run(name, fn)
	}

	if !runStep("setup", func(t *testing.T) {
		if err := runKGSetup(); err != nil {
			t.Fatalf("runKGSetup: %v", err)
		}
		// Sanity-check baseline directory layout: notes subdirs + raw inbox.
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
	}) {
		return
	}

	if !runStep("ingest_source_file", func(t *testing.T) {
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

		deps = testDeps()
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
	}) {
		return
	}

	if !runStep("warm_sqlite_layer", func(t *testing.T) {
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
	}) {
		return
	}

	if !runStep("query_source_lookup", func(t *testing.T) {
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
	}) {
		return
	}

	if !runStep("seed_broken_link", func(t *testing.T) {
		now = time.Now().UTC().Format(time.RFC3339)
		brokenNote := makeNote(noteSpec{
			id: "dec-broken", typ: "decision", title: "Broken Link Decision",
			summary: "Has a dangling link.", status: "active", ts: now,
			sourceRefs: []string{"src-curation-doc"}, links: []string{"missing-target"},
		})
		if err := createGraphNote(home, brokenNote, "## Body\nload-bearing body content.\n"); err != nil {
			t.Fatalf("createGraphNote dec-broken: %v", err)
		}
	}) {
		return
	}

	if !runStep("lint_surfaces_broken_link", func(t *testing.T) {
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
	}) {
		return
	}

	if !runStep("reweave_preserves_body", func(t *testing.T) {
		if err := runKGReweave(home); err != nil {
			t.Fatalf("runKGReweave: %v", err)
		}
		rewovenData, _ := os.ReadFile(filepath.Join(home, "notes", "decisions", "dec-broken.md"))
		if strings.Contains(string(rewovenData), "missing-target") {
			t.Errorf("expected missing-target link removed after reweave, got:\n%s", rewovenData)
		}
		// Regression for the persistReweavedNote body-loss fix (commit
		// de7ebae): the note body must survive the frontmatter rewrite.
		if !strings.Contains(string(rewovenData), "load-bearing body content") {
			t.Errorf("expected body preserved across reweave, got:\n%s", rewovenData)
		}
	}) {
		return
	}

	if !runStep("lint_after_reweave_zero_broken", func(t *testing.T) {
		postReweaveReport, err := runGraphLint(home)
		if err != nil {
			t.Fatalf("runGraphLint #2: %v", err)
		}
		if countLintCheck(postReweaveReport.Results, "broken_links") != 0 {
			t.Errorf("expected zero broken_links after reweave, got %d (results=%#v)",
				countLintCheck(postReweaveReport.Results, "broken_links"), postReweaveReport.Results)
		}
	}) {
		return
	}

	if !runStep("seed_stale_page", func(t *testing.T) {
		oldTS := time.Now().Add(-200 * 24 * time.Hour).UTC().Format(time.RFC3339)
		staleNote := makeNote(noteSpec{
			id: "ent-stale-cycle", typ: "entity", title: "Stale Cycle Entity",
			summary: "Old entity from a previous era.", status: "active", ts: oldTS,
			sourceRefs: []string{"src-curation-doc"},
		})
		if err := createGraphNote(home, staleNote, "stale body"); err != nil {
			t.Fatalf("createGraphNote ent-stale-cycle: %v", err)
		}
	}) {
		return
	}

	if !runStep("lint_reports_stale", func(t *testing.T) {
		staleReport, err := runGraphLint(home)
		if err != nil {
			t.Fatalf("runGraphLint #3: %v", err)
		}
		if !lintHasFor(staleReport.Results, "stale_pages", "ent-stale-cycle") {
			t.Errorf("expected stale_pages result for ent-stale-cycle, got %#v", staleReport.Results)
		}
		stalePreCount = countLintCheck(staleReport.Results, "stale_pages")
	}) {
		return
	}

	if !runStep("mark_stale_promotes_status", func(t *testing.T) {
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
	}) {
		return
	}

	if !runStep("lint_after_mark_stale_count_drops", func(t *testing.T) {
		// mark-stale rewrites UpdatedAt to time.Now() before saving, so the
		// note is no longer past the stale cutoff and should not be flagged.
		postMarkReport, err := runGraphLint(home)
		if err != nil {
			t.Fatalf("runGraphLint #4: %v", err)
		}
		stalePostCount := countLintCheck(postMarkReport.Results, "stale_pages")
		if stalePostCount >= stalePreCount {
			t.Errorf("expected stale_pages count to drop after mark-stale (pre=%d post=%d)",
				stalePreCount, stalePostCount)
		}
	}) {
		return
	}

	runStep("compact_archives_notes", func(t *testing.T) {
		// Add an archived note so compact has something to move.
		archived := makeNote(noteSpec{
			id: "dec-archived-cycle", typ: "decision", title: "Archived Cycle Decision",
			summary: "Already in archived status.", status: "archived", ts: now,
		})
		if err := createGraphNote(home, archived, ""); err != nil {
			t.Fatalf("createGraphNote dec-archived-cycle: %v", err)
		}
		if err := runKGCompact(home); err != nil {
			t.Fatalf("runKGCompact: %v", err)
		}
		// The archived note must move under notes/_archived/ and be removed
		// from its original subdirectory + index.
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
	})
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
