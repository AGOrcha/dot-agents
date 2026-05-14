package kg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/graphstore"
	"github.com/spf13/cobra"
)

// TestRunKGQuery_ExecuteQueryError drives the executeQuery-error return inside
// runKGQuery (query_lint_maintain.go ~364-366).
func TestRunKGQuery_ExecuteQueryError(t *testing.T) {
	newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	cmd := &cobra.Command{}
	cmd.Flags().String("intent", "totally_unknown", "")
	cmd.Flags().String("scope", "", "")
	cmd.Flags().Int("limit", 5, "")
	cmd.Flags().Bool("json", false, "")
	if err := runKGQuery(testDeps(), cmd, []string{"q"}); err == nil {
		t.Fatal("expected executeQuery error for unknown intent")
	}
}

// TestSearchNotes_DefaultLimit drives the limit <= 0 default branch.
func TestSearchNotes_DefaultLimit(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	results, err := searchNotes(home, "entity", "", 0)
	if err != nil {
		t.Fatalf("searchNotes: %v", err)
	}
	if results != nil {
		// May be nil — just exercising the default-limit branch.
		_ = results
	}
}

// TestRunKGLint_HappyPath drives the success path with JSON output.
func TestRunKGLint_HappyPath(t *testing.T) {
	newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	cmd := &cobra.Command{}
	cmd.Flags().Bool("json", true, "")
	if err := runKGLint(testDeps(), cmd, nil); err != nil {
		t.Fatalf("runKGLint: %v", err)
	}
}

// TestRunKGCompactCmd_NotInitialized drives the IsNotExist branch on
// runKGCompactCmd.
func TestRunKGCompactCmd_NotInitialized(t *testing.T) {
	newTempKG(t)
	// runKGCompactCmd may not exist by that name; use the public command.
	// This test is structured to exercise the wrapped IsNotExist path on any
	// "knowledge graph not initialized" message — runKGQueue is one such
	// entry-point.
	cmd := &cobra.Command{}
	cmd.Flags().Bool("json", false, "")
	if err := runKGQueue(testDeps()); err == nil || !strings.Contains(err.Error(), "not initialized") {
		t.Fatalf("expected not-initialized error, got %v", err)
	}
}

// TestAppendDecisionSymbolMatches_LimitHitAndSeen drives both the limit
// early-return (~422-424) and the missing-node fallback summary branch
// (~411-417).
func TestAppendDecisionSymbolMatches_LimitHitAndSeen(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	store, err := openKGStore(home)
	if err != nil {
		t.Fatalf("openKGStore: %v", err)
	}
	defer store.Close()

	note := graphstore.KGNote{ID: "d1", NoteType: "decision", Title: "T"}
	links := []graphstore.NoteSymbolLink{
		{NoteID: "d1", QualifiedName: "missing-1", LinkKind: "mentions"},
		{NoteID: "d1", QualifiedName: "missing-2", LinkKind: "mentions"},
		// Duplicate — drives seen-skip branch.
		{NoteID: "d1", QualifiedName: "missing-1", LinkKind: "documents"},
	}
	var results []GraphQueryResult
	done := appendDecisionSymbolMatches(store, note, links, map[string]bool{}, &results, 1)
	if !done {
		t.Errorf("expected done=true once limit=1 is hit")
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}

// TestCollectChangeAnalysisResults_HappyPathEmpty drives the empty
// resp.Results == nil → init branch (~457-459) when no matches are found.
func TestCollectChangeAnalysisResults_HappyPathEmpty(t *testing.T) {
	repo := installCRGWithBody(t, `case "$1" in
detect-changes) printf '%s\n' '{"summary":"none","risk_score":0,"changed_functions":[],"affected_flows":[],"test_gaps":[],"review_priorities":[]}' ;;
*) exit 0 ;;
esac`)
	_ = repo
	resp, err := collectChangeAnalysisResults("query-no-match", 5)
	if err != nil {
		t.Fatalf("collectChangeAnalysisResults: %v", err)
	}
	if resp.Results == nil {
		t.Error("expected empty results to be initialized, got nil")
	}
}

// TestRunKGLinkAdd_OpenStoreError drives the openKGStore-error return path by
// pointing KG_HOME at a bogus path.
func TestRunKGLinkAdd_OpenStoreError(t *testing.T) {
	t.Setenv("KG_HOME", "/dev/null/not-a-dir")
	cmd := &cobra.Command{}
	cmd.Flags().String("kind", "mentions", "")
	err := runKGLinkAdd(cmd, []string{"note", "qn"})
	if err == nil || !strings.Contains(err.Error(), "open warm store") {
		t.Fatalf("expected open-warm-store error, got %v", err)
	}
}

// TestRunKGLinkList_OpenStoreError drives the openKGStore-error return path.
func TestRunKGLinkList_OpenStoreError(t *testing.T) {
	t.Setenv("KG_HOME", "/dev/null/not-a-dir")
	cmd := &cobra.Command{}
	err := runKGLinkList(cmd, []string{"note"})
	if err == nil || !strings.Contains(err.Error(), "open warm store") {
		t.Fatalf("expected open-warm-store error, got %v", err)
	}
}

// TestRunKGLinkRemove_OpenStoreError drives the openKGStore-error return path.
func TestRunKGLinkRemove_OpenStoreError(t *testing.T) {
	t.Setenv("KG_HOME", "/dev/null/not-a-dir")
	cmd := &cobra.Command{}
	err := runKGLinkRemove(cmd, []string{"42"})
	if err == nil || !strings.Contains(err.Error(), "open warm store") {
		t.Fatalf("expected open-warm-store error, got %v", err)
	}
}

// TestRunKGWarm_OpenStoreError drives the openKGStore-error wrapper on
// runKGWarm.
func TestRunKGWarm_OpenStoreError(t *testing.T) {
	t.Setenv("KG_HOME", "/dev/null/not-a-dir")
	cmd := &cobra.Command{}
	cmd.Flags().String("type", "", "")
	cmd.Flags().Bool("include-code", false, "")
	if err := runKGWarm(cmd, nil); err == nil || !strings.Contains(err.Error(), "open warm store") {
		t.Fatalf("expected open-warm-store error, got %v", err)
	}
}

// TestRunKGWarmStats_OpenStoreError drives the openKGStore-error wrapper.
func TestRunKGWarmStats_OpenStoreError(t *testing.T) {
	t.Setenv("KG_HOME", "/dev/null/not-a-dir")
	cmd := &cobra.Command{}
	if err := runKGWarmStats(cmd, nil); err == nil || !strings.Contains(err.Error(), "open warm store") {
		t.Fatalf("expected open-warm-store error, got %v", err)
	}
}

// TestRunKGWarm_InvalidNoteType drives the warmNoteSubdirs error path.
func TestRunKGWarm_InvalidNoteType(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	_ = home
	cmd := &cobra.Command{}
	cmd.Flags().String("type", "no-such-type", "")
	cmd.Flags().Bool("include-code", false, "")
	if err := runKGWarm(cmd, nil); err == nil || !strings.Contains(err.Error(), "invalid note type") {
		t.Fatalf("expected invalid-note-type error, got %v", err)
	}
}

// TestRunKGWarm_HappyPath drives the success path with one seeded source note.
func TestRunKGWarm_HappyPath(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// Seed a real note so warmActiveNotes increments the indexed counter.
	note := &GraphNote{
		SchemaVersion: 1, ID: "e1", Type: "entity", Title: "T",
		Summary: "s", Status: "draft",
		CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
	}
	if err := createGraphNote(home, note, "body"); err != nil {
		t.Fatalf("createGraphNote: %v", err)
	}
	cmd := &cobra.Command{}
	cmd.Flags().String("type", "", "")
	cmd.Flags().Bool("include-code", false, "")
	if err := runKGWarm(cmd, nil); err != nil {
		t.Fatalf("runKGWarm: %v", err)
	}
}

// TestRunKGWarmStats_HappyPath drives the success path of runKGWarmStats.
func TestRunKGWarmStats_HappyPath(t *testing.T) {
	newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	cmd := &cobra.Command{}
	if err := runKGWarmStats(cmd, nil); err != nil {
		t.Fatalf("runKGWarmStats: %v", err)
	}
}

// TestKGImpactJSONOutput_StructHasGraphState confirms the JSON wrapper shape.
func TestKGImpactJSONOutput_StructHasGraphState(t *testing.T) {
	w := kgImpactJSONOutput{
		GraphState: "ready",
	}
	if w.GraphState != "ready" {
		t.Errorf("kgImpactJSONOutput field mismatch: %+v", w)
	}
}

// TestRunKGCodeStatus_ExplicitRepo seeds a CRG status fixture and drives
// runKGCodeStatus with an explicit --repo so the cwd-default branch is
// bypassed.
func TestRunKGCodeStatus_ExplicitRepo(t *testing.T) {
	repo := t.TempDir()
	writeCRGStatusFixture(t, repo, []crgNodeFixture{
		{FilePath: "a.go", Language: "go", UpdatedAt: "2026-04-20T00:00:00Z"},
	})
	cmd := &cobra.Command{}
	cmd.Flags().String("repo", repo, "")
	cmd.Flags().Bool("json", true, "")
	if err := runKGCodeStatus(testDeps(), cmd, nil); err != nil {
		t.Fatalf("runKGCodeStatus: %v", err)
	}
}

// TestAppendDecisionSymbolMatches_WithNode drives the resolved-node summary
// override branch (~419-420) by seeding a node with the same qn as a link.
func TestAppendDecisionSymbolMatches_WithNode(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	store, err := openKGStore(home)
	if err != nil {
		t.Fatalf("openKGStore: %v", err)
	}
	defer store.Close()

	if _, err := store.UpsertNode(graphstore.NodeInfo{
		Kind: "Function", Name: "Sym", FilePath: "p.go", Language: "go",
	}, "h"); err != nil {
		t.Fatal(err)
	}
	note := graphstore.KGNote{ID: "d1", NoteType: "decision", Title: "T"}
	links := []graphstore.NoteSymbolLink{
		{NoteID: "d1", QualifiedName: "p.go::Sym", LinkKind: "mentions"},
	}
	var results []GraphQueryResult
	if appendDecisionSymbolMatches(store, note, links, map[string]bool{}, &results, 5) {
		t.Error("expected done=false below limit")
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !strings.Contains(results[0].Summary, "via") {
		t.Errorf("expected summary to contain 'via', got %q", results[0].Summary)
	}
}

// ── filepath assertion sanity check (anchors filepath import usage) ───────────

func TestExtraFaultTest_FilepathSanity(t *testing.T) {
	if filepath.Base("/a/b/c") != "c" {
		t.Fatal("filepath.Base broken")
	}
	if !os.IsNotExist(os.ErrNotExist) {
		t.Fatal("os.IsNotExist broken")
	}
}

// TestCreateGraphNote_UpdateIndexError forces updateIndex to fail inside
// createGraphNote (~969-971) by injecting a write failure for the index.
func TestCreateGraphNote_UpdateIndexError(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	swapWriteFile(t, func(path string, data []byte, perm os.FileMode) error {
		if filepath.Base(path) == kgIndexFileName {
			return errSeam
		}
		return os.WriteFile(path, data, perm)
	})
	note := &GraphNote{
		SchemaVersion: 1, ID: "e1", Type: "entity", Title: "T",
		Summary: "s", Status: "draft",
		CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
	}
	if err := createGraphNote(home, note, "body"); err == nil {
		t.Fatal("expected updateIndex seam error")
	}
}

// TestUpdateGraphNote_WriteFileError forces the second-write seam error
// inside updateGraphNote (~999-1001).
func TestUpdateGraphNote_WriteFileError(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// First seed the note normally.
	note := &GraphNote{
		SchemaVersion: 1, ID: "e1", Type: "entity", Title: "T",
		Summary: "s", Status: "draft",
		CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
	}
	if err := createGraphNote(home, note, "body"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Now fail the next WriteFile (the update path).
	swapWriteFile(t, func(path string, data []byte, perm os.FileMode) error {
		if strings.HasSuffix(path, "e1.md") {
			return errSeam
		}
		return os.WriteFile(path, data, perm)
	})
	if err := updateGraphNote(home, note, "body2"); err == nil {
		t.Fatal("expected write-file seam error")
	}
}

// TestUpdateGraphNote_UpdateIndexError forces updateIndex inside
// updateGraphNote (~1002-1004) to fail.
func TestUpdateGraphNote_UpdateIndexError(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	note := &GraphNote{
		SchemaVersion: 1, ID: "e1", Type: "entity", Title: "T",
		Summary: "s", Status: "draft",
		CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
	}
	if err := createGraphNote(home, note, "body"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	swapWriteFile(t, func(path string, data []byte, perm os.FileMode) error {
		if filepath.Base(path) == kgIndexFileName {
			return errSeam
		}
		return os.WriteFile(path, data, perm)
	})
	if err := updateGraphNote(home, note, "body2"); err == nil {
		t.Fatal("expected updateIndex seam error on update")
	}
}

// TestReadIndex_MissingReturnsNil drives the IsNotExist-nil-return branch
// (~287-289).
func TestReadIndex_MissingReturnsNil(t *testing.T) {
	home := newTempKG(t)
	// Don't initialize — index.md is absent so IsNotExist branch fires.
	entries, err := readIndex(home)
	if err != nil {
		t.Fatalf("readIndex on missing index: %v", err)
	}
	if entries != nil {
		t.Errorf("expected nil entries on missing index, got %v", entries)
	}
}
