package kg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// curationState is the state shared across the ordered curation pipeline
// steps. Each step is a t.Run subtest that reads/writes this struct so the
// steps stay individually small while still forming one pipeline.
type curationState struct {
	home          string
	deps          Deps
	now           string
	stalePreCount int
}

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
	st := &curationState{home: newTempKG(t)}

	// Ordered pipeline. Each step is a t.Run subtest so its local control
	// flow is isolated; a failed step aborts the whole pipeline (preserving
	// the original t.Fatalf "stop here" semantics) by returning early.
	steps := []struct {
		name string
		fn   func(t *testing.T, st *curationState)
	}{
		{"setup", curationStepSetup},
		{"ingest_source_file", curationStepIngest},
		{"warm_sqlite_layer", curationStepWarm},
		{"query_source_lookup", curationStepQuery},
		{"seed_broken_link", curationStepSeedBroken},
		{"lint_surfaces_broken_link", curationStepLintBroken},
		{"reweave_preserves_body", curationStepReweave},
		{"lint_after_reweave_zero_broken", curationStepLintPostReweave},
		{"seed_stale_page", curationStepSeedStale},
		{"lint_reports_stale", curationStepLintStale},
		{"mark_stale_promotes_status", curationStepMarkStale},
		{"lint_after_mark_stale_count_drops", curationStepLintPostMark},
		{"compact_archives_notes", curationStepCompact},
	}
	for _, s := range steps {
		if !t.Run(s.name, func(t *testing.T) { s.fn(t, st) }) {
			return
		}
	}
}

func curationStepSetup(t *testing.T, st *curationState) {
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
		if _, err := os.Stat(filepath.Join(st.home, sub)); err != nil {
			t.Fatalf("expected %s to exist after setup: %v", sub, err)
		}
	}
}

func curationStepIngest(t *testing.T, st *curationState) {
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

	st.deps = testDeps()
	captureStdout(t, func() {
		if err := runKGIngest(st.deps, newIngestCmd(false, false, "", "markdown"), []string{srcFile}); err != nil {
			t.Fatalf("runKGIngest: %v", err)
		}
	})

	// Source note must exist.
	if exists, _ := noteExists(st.home, "src-curation-doc"); !exists {
		t.Fatal("expected src-curation-doc note after ingest")
	}
	// At least one decision note extracted from the body.
	decEntries, _ := os.ReadDir(filepath.Join(st.home, "notes", "decisions"))
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
	if _, err := os.Stat(filepath.Join(st.home, "raw", "imported", "curation-doc.md")); err != nil {
		t.Errorf("expected raw/imported/curation-doc.md: %v", err)
	}
}

func curationStepWarm(t *testing.T, st *curationState) {
	if err := runKGWarm(newKGWarmCmdForTest(), nil); err != nil {
		t.Fatalf("runKGWarm: %v", err)
	}
	store, err := openKGStore(st.home)
	if err != nil {
		t.Fatalf("openKGStore: %v", err)
	}
	defer store.Close()
	stats, err := store.GetStats()
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.NotesCount == 0 {
		t.Error("expected warm layer to contain at least one note after warm")
	}
}

func curationStepQuery(t *testing.T, _ *curationState) {
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
	for _, r := range lookupResp.Results {
		if r.ID == "src-curation-doc" {
			return
		}
	}
	t.Errorf("expected source_lookup to surface src-curation-doc, got results=%#v", lookupResp.Results)
}

func curationStepSeedBroken(t *testing.T, st *curationState) {
	st.now = time.Now().UTC().Format(time.RFC3339)
	brokenNote := makeNote(noteSpec{
		id: "dec-broken", typ: "decision", title: "Broken Link Decision",
		summary: "Has a dangling link.", status: "active", ts: st.now,
		sourceRefs: []string{"src-curation-doc"}, links: []string{"missing-target"},
	})
	if err := createGraphNote(st.home, brokenNote, "## Body\nload-bearing body content.\n"); err != nil {
		t.Fatalf("createGraphNote dec-broken: %v", err)
	}
}

func curationStepLintBroken(t *testing.T, st *curationState) {
	report, err := runGraphLint(st.home)
	if err != nil {
		t.Fatalf("runGraphLint #1: %v", err)
	}
	if !lintHasFor(report.Results, "broken_links", "dec-broken") {
		t.Errorf("expected broken_links result for dec-broken, got %#v", report.Results)
	}
	if _, err := os.Stat(filepath.Join(st.home, "ops", "lint", "lint-report.json")); err != nil {
		t.Errorf("expected lint-report.json after lint: %v", err)
	}
}

func curationStepReweave(t *testing.T, st *curationState) {
	if err := runKGReweave(st.home); err != nil {
		t.Fatalf("runKGReweave: %v", err)
	}
	rewovenData, _ := os.ReadFile(filepath.Join(st.home, "notes", "decisions", "dec-broken.md"))
	if strings.Contains(string(rewovenData), "missing-target") {
		t.Errorf("expected missing-target link removed after reweave, got:\n%s", rewovenData)
	}
	// Regression for the persistReweavedNote body-loss fix (commit
	// de7ebae): the note body must survive the frontmatter rewrite.
	if !strings.Contains(string(rewovenData), "load-bearing body content") {
		t.Errorf("expected body preserved across reweave, got:\n%s", rewovenData)
	}
}

func curationStepLintPostReweave(t *testing.T, st *curationState) {
	postReweaveReport, err := runGraphLint(st.home)
	if err != nil {
		t.Fatalf("runGraphLint #2: %v", err)
	}
	if countLintCheck(postReweaveReport.Results, "broken_links") != 0 {
		t.Errorf("expected zero broken_links after reweave, got %d (results=%#v)",
			countLintCheck(postReweaveReport.Results, "broken_links"), postReweaveReport.Results)
	}
}

func curationStepSeedStale(t *testing.T, st *curationState) {
	oldTS := time.Now().Add(-200 * 24 * time.Hour).UTC().Format(time.RFC3339)
	staleNote := makeNote(noteSpec{
		id: "ent-stale-cycle", typ: "entity", title: "Stale Cycle Entity",
		summary: "Old entity from a previous era.", status: "active", ts: oldTS,
		sourceRefs: []string{"src-curation-doc"},
	})
	if err := createGraphNote(st.home, staleNote, "stale body"); err != nil {
		t.Fatalf("createGraphNote ent-stale-cycle: %v", err)
	}
}

func curationStepLintStale(t *testing.T, st *curationState) {
	staleReport, err := runGraphLint(st.home)
	if err != nil {
		t.Fatalf("runGraphLint #3: %v", err)
	}
	if !lintHasFor(staleReport.Results, "stale_pages", "ent-stale-cycle") {
		t.Errorf("expected stale_pages result for ent-stale-cycle, got %#v", staleReport.Results)
	}
	st.stalePreCount = countLintCheck(staleReport.Results, "stale_pages")
}

func curationStepMarkStale(t *testing.T, st *curationState) {
	if err := runKGMarkStale(st.home, 90*24*time.Hour); err != nil {
		t.Fatalf("runKGMarkStale: %v", err)
	}
	staleData, _ := os.ReadFile(filepath.Join(st.home, "notes", "entities", "ent-stale-cycle.md"))
	parsedStale, _, err := parseGraphNote(staleData)
	if err != nil {
		t.Fatalf("parse ent-stale-cycle: %v", err)
	}
	if parsedStale.Status != "stale" {
		t.Errorf("expected ent-stale-cycle status=stale after mark-stale, got %s", parsedStale.Status)
	}
}

func curationStepLintPostMark(t *testing.T, st *curationState) {
	// mark-stale rewrites UpdatedAt to time.Now() before saving, so the
	// note is no longer past the stale cutoff and should not be flagged.
	postMarkReport, err := runGraphLint(st.home)
	if err != nil {
		t.Fatalf("runGraphLint #4: %v", err)
	}
	stalePostCount := countLintCheck(postMarkReport.Results, "stale_pages")
	if stalePostCount >= st.stalePreCount {
		t.Errorf("expected stale_pages count to drop after mark-stale (pre=%d post=%d)",
			st.stalePreCount, stalePostCount)
	}
}

func curationStepCompact(t *testing.T, st *curationState) {
	// Add an archived note so compact has something to move.
	archived := makeNote(noteSpec{
		id: "dec-archived-cycle", typ: "decision", title: "Archived Cycle Decision",
		summary: "Already in archived status.", status: "archived", ts: st.now,
	})
	if err := createGraphNote(st.home, archived, ""); err != nil {
		t.Fatalf("createGraphNote dec-archived-cycle: %v", err)
	}
	if err := runKGCompact(st.home); err != nil {
		t.Fatalf("runKGCompact: %v", err)
	}
	// The archived note must move under notes/_archived/ and be removed
	// from its original subdirectory + index.
	if _, err := os.Stat(filepath.Join(st.home, "notes", "_archived", "dec-archived-cycle.md")); err != nil {
		t.Errorf("expected dec-archived-cycle moved to _archived: %v", err)
	}
	if _, err := os.Stat(filepath.Join(st.home, "notes", "decisions", "dec-archived-cycle.md")); !os.IsNotExist(err) {
		t.Errorf("expected dec-archived-cycle removed from decisions/, got: %v", err)
	}
	idxData, _ := os.ReadFile(filepath.Join(st.home, "notes", "index.md"))
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
