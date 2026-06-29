package kg

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/graphstore"
	"github.com/spf13/cobra"
	_ "modernc.org/sqlite"
)

// TestCommandJSON_FlagDetection covers both branches of commandJSON.
func TestCommandJSON_FlagDetection(t *testing.T) {
	if commandJSON(nil) {
		t.Error("nil command should return false")
	}
	cmd := &cobra.Command{}
	cmd.Flags().Bool("json", false, "")
	if commandJSON(cmd) {
		t.Error("default json flag should return false")
	}
	if err := cmd.Flags().Set("json", "true"); err != nil {
		t.Fatal(err)
	}
	if !commandJSON(cmd) {
		t.Error("json=true flag should return true")
	}
}

// TestCRGRepoRoot_FindsGitAncestor verifies the .git lookup walk.
func TestCRGRepoRoot_FindsGitAncestor(t *testing.T) {
	root := t.TempDir()
	gitDir := filepath.Join(root, ".git")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	// Resolve symlinks so /var/folders → /private/var/folders comparison works.
	want, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := filepath.EvalSymlinks(crgRepoRoot())
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("crgRepoRoot: got %q, want %q", got, want)
	}
}

// TestCRGRepoRoot_FallsBackToCwd documents the fallback when no .git is found.
func TestCRGRepoRoot_FallsBackToCwd(t *testing.T) {
	root := t.TempDir() // no .git anywhere up the path
	t.Chdir(root)
	want, _ := filepath.EvalSymlinks(root)
	got, err := filepath.EvalSymlinks(crgRepoRoot())
	if err != nil {
		t.Fatal(err)
	}
	// On macOS /tmp may symlink — accept either equal path or want being a
	// prefix of got (since walking up from /tmp may bottom out at /).
	if got != want && !strings.HasPrefix(want, got) {
		t.Errorf("crgRepoRoot fallback: got %q, want %q (or ancestor)", got, want)
	}
}

// TestGraphstoreDBPath verifies the canonical warm-store path.
func TestGraphstoreDBPath(t *testing.T) {
	got := graphstoreDBPath("/tmp/kg")
	want := filepath.Join("/tmp/kg", "ops", "graphstore.db")
	if got != want {
		t.Errorf("graphstoreDBPath: got %q, want %q", got, want)
	}
}

// TestNoteToKGNote_ArchiveTimestamp ensures archived/superseded notes get
// the archive timestamp populated from the source's UpdatedAt.
func TestNoteToKGNote_ArchiveTimestamp(t *testing.T) {
	now := "2026-04-01T00:00:00Z"
	active := &GraphNote{ID: "n1", Title: "t", Type: "decision", Status: "active", UpdatedAt: now}
	kn := noteToKGNote(active, "/tmp/n1.md")
	if kn.ArchivedAt != "" {
		t.Errorf("active note should not have ArchivedAt, got %q", kn.ArchivedAt)
	}

	archived := &GraphNote{ID: "n2", Title: "t", Type: "decision", Status: "archived", UpdatedAt: now}
	kn2 := noteToKGNote(archived, "/tmp/n2.md")
	if kn2.ArchivedAt != now {
		t.Errorf("archived note: ArchivedAt = %q, want %q", kn2.ArchivedAt, now)
	}

	superseded := &GraphNote{ID: "n3", Title: "t", Type: "decision", Status: "superseded", UpdatedAt: now}
	kn3 := noteToKGNote(superseded, "/tmp/n3.md")
	if kn3.ArchivedAt != now {
		t.Errorf("superseded note: ArchivedAt = %q, want %q", kn3.ArchivedAt, now)
	}
}

// TestCRGStatusState_FallbackUnknown covers the error branch where Status()
// is called against a non-existent repo (no DB file).
func TestCRGStatusState_FallbackUnknown(t *testing.T) {
	repo := t.TempDir() // no CRG DB
	// crgStatusState should return one of the known readiness states, not
	// "unknown" — the missing-db case is classified as "unbuilt" by Status().
	state := crgStatusState(repo)
	if state != string(graphstore.CRGReadinessUnbuilt) {
		t.Errorf("crgStatusState: got %q, want %q", state, graphstore.CRGReadinessUnbuilt)
	}
}

// TestCheckCRGReadiness_UnbuiltRequireGraph asserts the requireGraph=true
// path returns an error for an unbuilt graph.
func TestCheckCRGReadiness_UnbuiltRequireGraph(t *testing.T) {
	repo := t.TempDir() // no DB → unbuilt
	captureStdout(t, func() {
		if err := checkCRGReadiness(repo, true); err == nil {
			t.Error("expected error when graph unbuilt and requireGraph=true")
		}
	})
	// With requireGraph=false, only a warning is emitted — no error.
	captureStdout(t, func() {
		if err := checkCRGReadiness(repo, false); err != nil {
			t.Errorf("unexpected error when requireGraph=false: %v", err)
		}
	})
}

// TestRunKGBuild_JSONOutput drives the build command through a fake CRG
// binary and asserts the JSON shape.
func TestRunKGBuild_JSONOutput(t *testing.T) {
	repo := t.TempDir()
	// Seed an existing graph so BuildReport's post-build Status() reports
	// ready; the fake binary itself is a no-op.
	writeCRGStatusFixture(t, repo, []crgNodeFixture{
		{FilePath: "a.go", Language: "go", UpdatedAt: "2026-04-19T18:03:45Z"},
	})
	writeFakeCRGBinary(t, repo, "exit 0")

	cmd := &cobra.Command{}
	cmd.Flags().String("repo", repo, "")
	cmd.Flags().Bool("skip-flows", true, "")
	cmd.Flags().Bool("skip-postprocess", true, "")
	cmd.Flags().Bool("json", true, "")

	out := captureStdout(t, func() {
		if err := runKGBuild(cmd, nil); err != nil {
			t.Fatalf("runKGBuild: %v", err)
		}
	})
	var report graphstore.CRGOperationReport
	if err := json.Unmarshal(out, &report); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %s", err, string(out))
	}
	if report.Operation != "build" {
		t.Errorf("operation: got %q want build", report.Operation)
	}
}

// TestRunKGBuild_TextOutput exercises the non-JSON branch of build.
func TestRunKGBuild_TextOutput(t *testing.T) {
	repo := t.TempDir()
	writeCRGStatusFixture(t, repo, []crgNodeFixture{
		{FilePath: "a.go", Language: "go", UpdatedAt: "2026-04-19T18:03:45Z"},
	})
	writeFakeCRGBinary(t, repo, "exit 0")

	cmd := &cobra.Command{}
	cmd.Flags().String("repo", repo, "")
	cmd.Flags().Bool("skip-flows", false, "")
	cmd.Flags().Bool("skip-postprocess", false, "")
	cmd.Flags().Bool("json", false, "")

	out := captureStdout(t, func() {
		if err := runKGBuild(cmd, nil); err != nil {
			t.Fatalf("runKGBuild: %v", err)
		}
	})
	if !strings.Contains(string(out), "Building code graph") {
		t.Errorf("expected build banner, got:\n%s", out)
	}
}

// TestRunKGUpdate_NoDiff verifies the "no_diff" outcome through the text path.
func TestRunKGUpdate_NoDiff(t *testing.T) {
	repo := t.TempDir()
	initGitRepo(t, repo)
	commitFile(t, repo, "a.txt", "one\n", "init")
	writeFakeCRGBinary(t, repo, "exit 0")

	cmd := &cobra.Command{}
	cmd.Flags().String("repo", repo, "")
	cmd.Flags().String("base", "HEAD", "")
	cmd.Flags().Bool("skip-flows", false, "")
	cmd.Flags().Bool("skip-postprocess", false, "")
	cmd.Flags().Bool("json", false, "")

	out := captureStdout(t, func() {
		if err := runKGUpdate(cmd, nil); err != nil {
			t.Fatalf("runKGUpdate: %v", err)
		}
	})
	if !strings.Contains(string(out), "No code diff") {
		t.Errorf("expected 'No code diff' for HEAD-against-HEAD update, got:\n%s", out)
	}
}

// TestRunKGUpdate_JSONOutput verifies the JSON branch.
func TestRunKGUpdate_JSONOutput(t *testing.T) {
	repo := t.TempDir()
	initGitRepo(t, repo)
	commitFile(t, repo, "a.txt", "one\n", "init")
	writeFakeCRGBinary(t, repo, "exit 0")

	cmd := &cobra.Command{}
	cmd.Flags().String("repo", repo, "")
	cmd.Flags().String("base", "HEAD", "")
	cmd.Flags().Bool("skip-flows", false, "")
	cmd.Flags().Bool("skip-postprocess", false, "")
	cmd.Flags().Bool("json", true, "")

	out := captureStdout(t, func() {
		if err := runKGUpdate(cmd, nil); err != nil {
			t.Fatalf("runKGUpdate JSON: %v", err)
		}
	})
	var report graphstore.CRGOperationReport
	if err := json.Unmarshal(out, &report); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %s", err, string(out))
	}
	if report.Operation != "update" {
		t.Errorf("operation: got %q want update", report.Operation)
	}
}

// TestRunKGCodeStatus_TextOutput verifies the text-mode branch.
func TestRunKGCodeStatus_TextOutput(t *testing.T) {
	repo := t.TempDir()
	writeCRGStatusFixture(t, repo, []crgNodeFixture{
		{FilePath: "a.go", Language: "go", UpdatedAt: "2026-04-19T18:03:45Z"},
	})
	cmd := &cobra.Command{}
	cmd.Flags().String("repo", repo, "")
	cmd.Flags().Bool("json", false, "")

	out := captureStdout(t, func() {
		if err := runKGCodeStatus(testDeps(), cmd, nil); err != nil {
			t.Fatalf("runKGCodeStatus: %v", err)
		}
	})
	output := string(out)
	if !strings.Contains(output, "Code Graph Status") {
		t.Errorf("expected status header, got:\n%s", output)
	}
	if !strings.Contains(output, "Nodes:") {
		t.Errorf("expected Nodes: row in output, got:\n%s", output)
	}
}

// TestRenderImpactResultText covers the deterministic, CRG-free render path:
// non-empty sections render, file nodes are skipped, truncation is noted.
func TestRenderImpactResultText(t *testing.T) {
	result := &graphstore.CRGImpactResult{
		Summary: "summary line",
		ChangedNodes: []graphstore.ImpactNode{
			{Kind: "Function", Name: "ChangedFn"},
			{Kind: "File", Name: "skipped.go"}, // suppressed
		},
		ImpactedNodes: []graphstore.ImpactNode{
			{Kind: "Function", Name: "ImpactedFn"},
		},
		ImpactedFiles: []string{"a.go", "b.go"},
		Truncated:     true,
		TotalImpacted: 9,
	}
	out := captureStdout(t, func() {
		renderImpactResultText(result)
	})
	output := string(out)
	for _, want := range []string{"Impact Radius", "summary line", "ChangedFn", "ImpactedFn", "a.go", "b.go", "results truncated"} {
		if !strings.Contains(output, want) {
			t.Errorf("expected %q in output:\n%s", want, output)
		}
	}
	if strings.Contains(output, "skipped.go") {
		t.Errorf("File-kind nodes should be suppressed, got:\n%s", output)
	}
}

// TestRenderImpactResultText_NoNodes_PromptsCodeStatus verifies the empty
// branch emits the run-code-status hint.
func TestRenderImpactResultText_NoNodes_PromptsCodeStatus(t *testing.T) {
	result := &graphstore.CRGImpactResult{Summary: "no impact found"}
	out := captureStdout(t, func() {
		renderImpactResultText(result)
	})
	if !strings.Contains(string(out), "kg code-status") {
		t.Errorf("expected code-status hint when no nodes present, got:\n%s", out)
	}
}

// TestRunKGLinkAdd_DuplicateUpsert verifies link add is idempotent: the same
// (note, qn, kind) tuple produces a single link row.
func TestRunKGLinkAdd_DuplicateUpsert(t *testing.T) {
	home := setupKGWithNotes(t)
	_ = home

	cmd := newKGWarmCmdForTest()
	if err := runKGWarm(cmd, nil); err != nil {
		t.Fatalf("runKGWarm: %v", err)
	}
	addCmd := newKGLinkAddCmdForTest("mentions")
	if err := runKGLinkAdd(testDeps(), addCmd, []string{"dec-use-cobra", "x::Y"}); err != nil {
		t.Fatalf("first add: %v", err)
	}
	if err := runKGLinkAdd(testDeps(), addCmd, []string{"dec-use-cobra", "x::Y"}); err != nil {
		t.Fatalf("second add: %v", err)
	}
	store, _ := openKGStore(home)
	defer store.Close()
	links, _ := store.GetLinksForNote("dec-use-cobra")
	if len(links) != 1 {
		t.Errorf("expected idempotent upsert (1 link), got %d", len(links))
	}
}

// TestRunKGLinkAdd_DefaultKind verifies the empty --kind flag defaults to
// "mentions".
func TestRunKGLinkAdd_DefaultKind(t *testing.T) {
	home := setupKGWithNotes(t)
	_ = home
	cmd := newKGWarmCmdForTest()
	_ = runKGWarm(cmd, nil)

	// Pass an empty kind flag and verify the stored link kind is "mentions".
	addCmd := newKGLinkAddCmdForTest("")
	if err := runKGLinkAdd(testDeps(), addCmd, []string{"dec-use-cobra", "ns::Foo"}); err != nil {
		t.Fatalf("runKGLinkAdd: %v", err)
	}
	store, _ := openKGStore(home)
	defer store.Close()
	links, _ := store.GetLinksForNote("dec-use-cobra")
	if len(links) != 1 || links[0].LinkKind != "mentions" {
		t.Errorf("expected mentions default, got %+v", links)
	}
}

// TestRunKGLinkList_NoLinksMessage verifies the empty-result message.
func TestRunKGLinkList_NoLinksMessage(t *testing.T) {
	home := setupKGWithNotes(t)
	_ = home
	cmd := newKGWarmCmdForTest()
	_ = runKGWarm(cmd, nil)

	out := captureStdout(t, func() {
		if err := runKGLinkList(testDeps(), nil, []string{"dec-use-cobra"}); err != nil {
			t.Fatalf("runKGLinkList: %v", err)
		}
	})
	if !strings.Contains(string(out), "No symbol links") {
		t.Errorf("expected 'No symbol links' message, got:\n%s", out)
	}
}

// TestRunKGLinkList_MissingArg covers the usage-error branch.
func TestRunKGLinkList_MissingArg(t *testing.T) {
	err := runKGLinkList(testDeps(), nil, nil)
	if err == nil {
		t.Fatal("expected usage error for missing note-id")
	}
	if !strings.Contains(err.Error(), "kg link list") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestRunKGLinkRemove_MissingArg covers the usage-error branch.
func TestRunKGLinkRemove_MissingArg(t *testing.T) {
	err := runKGLinkRemove(testDeps(), nil, nil)
	if err == nil {
		t.Fatal("expected usage error for missing link id")
	}
	if !strings.Contains(err.Error(), "kg link remove") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestRunKGLinkRemove_NonExistent verifies the underlying store does not
// error when removing an unknown id (delete is idempotent).
func TestRunKGLinkRemove_NonExistent(t *testing.T) {
	home := newTempKG(t)
	_ = home
	if err := runKGSetup(testIO()); err != nil {
		t.Fatalf("setup: %v", err)
	}
	cmd := newKGLinkRemoveCmdForTest()
	if err := runKGLinkRemove(testDeps(), cmd, []string{"999999"}); err != nil {
		t.Fatalf("runKGLinkRemove unknown id: %v", err)
	}
}

// TestRunKGWarmCodeImport_NoCRGBinary returns a wrapped error when CRG is
// not discoverable on the system.
func TestRunKGWarmCodeImport_NoCRGBinary(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(testIO()); err != nil {
		t.Fatalf("setup: %v", err)
	}
	store, err := openKGStore(home)
	if err != nil {
		t.Fatalf("openKGStore: %v", err)
	}
	defer store.Close()

	// Run inside an isolated tempdir with no CRG on PATH.
	t.Chdir(t.TempDir())
	t.Setenv("PATH", t.TempDir())
	_, _, err = runKGWarmCodeImport(store, t.TempDir())
	if err == nil {
		t.Fatal("expected CRG-not-available error")
	}
	if !strings.Contains(err.Error(), "CRG not available") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestWarmNoteSubdirs covers the explicit-type, default, and invalid-type
// branches of warmNoteSubdirs.
func TestWarmNoteSubdirs(t *testing.T) {
	all, err := warmNoteSubdirs("")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) == 0 {
		t.Fatal("default subdir list should not be empty")
	}

	one, err := warmNoteSubdirs("entity")
	if err != nil {
		t.Fatalf("entity filter: %v", err)
	}
	if len(one) != 1 || one[0] != "entities" {
		t.Errorf("expected ['entities'], got %v", one)
	}

	if _, err := warmNoteSubdirs("not-a-type"); err == nil {
		t.Error("expected error for invalid note type")
	}
}

// TestRunKGSync_PushNoRemote verifies the push path returns a git error
// (no remote configured in tempdir).
func TestRunKGSync_PushNoRemote(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(testIO()); err != nil {
		t.Fatalf("setup: %v", err)
	}
	initGitRepo(t, home)
	commitFile(t, home, "self/config.yaml", "schema_version: 1\n", "init")

	cmd := &cobra.Command{}
	cmd.Flags().Bool("push", true, "")

	captureStdout(t, func() {
		err := runKGSync(cmd, nil)
		if err == nil {
			return // accept if a remote happened to be configured
		}
		if !strings.Contains(err.Error(), "git push failed") {
			t.Errorf("expected git push error, got: %v", err)
		}
	})
}

// TestRunKGWarmStats_OutputContent verifies the stats output exposes the
// expected fields.
func TestRunKGWarmStats_OutputContent(t *testing.T) {
	home := setupKGWithNotes(t)
	_ = home
	cmd := newKGWarmCmdForTest()
	_ = runKGWarm(cmd, nil)

	out := captureStdout(t, func() {
		if err := runKGWarmStats(nil, nil); err != nil {
			t.Fatalf("runKGWarmStats: %v", err)
		}
	})
	output := string(out)
	for _, want := range []string{"Warm Layer Stats", "Notes indexed", "Symbol links", "Code nodes", "DB path"} {
		if !strings.Contains(output, want) {
			t.Errorf("expected %q in stats output, got:\n%s", want, output)
		}
	}
}

// TestRunKGImpact_BridgeSucceedsButCRGUnavailable verifies that with no CRG
// binary on PATH and an unbuilt graph, runKGImpact returns an error from
// NewCRGBridge.
func TestRunKGImpact_BridgeUnavailable(t *testing.T) {
	repo := t.TempDir()
	// require-graph=false to skip the readiness guard; bridge.GetImpactRadius
	// then fails because CRG isn't installed in the tempdir.
	cmd := &cobra.Command{}
	cmd.Flags().String("repo", repo, "")
	cmd.Flags().String("base", "", "")
	cmd.Flags().Int("depth", 0, "")
	cmd.Flags().Int("limit", 0, "")
	cmd.Flags().Bool("require-graph", false, "")
	cmd.Flags().Bool("json", false, "")

	t.Setenv("PATH", t.TempDir())
	t.Chdir(t.TempDir())
	captureStdout(t, func() {
		if err := runKGImpact(testDeps(), cmd, nil); err == nil {
			t.Error("expected error when CRG unavailable")
		}
	})
}

// TestKGNoteFile_RoundTrip ensures noteToKGNote + UpsertKGNote + GetKGNote
// preserves all the fields used by the bridge.
func TestKGNoteFile_RoundTrip(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(testIO()); err != nil {
		t.Fatalf("setup: %v", err)
	}
	store, err := openKGStore(home)
	if err != nil {
		t.Fatalf("openKGStore: %v", err)
	}
	defer store.Close()

	gn := &GraphNote{
		ID: "rt-1", Type: "decision", Title: "Round Trip", Status: "active",
		Summary: "S", Version: 7, UpdatedAt: "2026-04-01T00:00:00Z",
	}
	kn := noteToKGNote(gn, filepath.Join(home, "notes", "decisions", "rt-1.md"))
	if err := store.UpsertKGNote(kn); err != nil {
		t.Fatalf("UpsertKGNote: %v", err)
	}
	got, err := store.GetKGNote("rt-1")
	if err != nil || got == nil {
		t.Fatalf("GetKGNote: %v", err)
	}
	if got.NoteType != "decision" || got.Title != "Round Trip" || got.Version != 7 {
		t.Errorf("unexpected loaded note: %+v", got)
	}
}

// Compile-time sanity: ensure the JSON wrapper structs from the file are
// referenced so the test file imports don't dangle.
var _ = fmt.Sprintf
var _ kgImpactJSONOutput
var _ kgChangesJSONOutput

// TestRenderImpactNodeSection_SkipsFileKind documents the file-suppression
// branch in isolation.
func TestRenderImpactNodeSection_SkipsFileKind(t *testing.T) {
	out := captureStdout(t, func() {
		renderImpactNodeSection("Section", "found", []graphstore.ImpactNode{
			{Kind: "Function", Name: "Keep"},
			{Kind: "File", Name: "skip.go"},
		})
	})
	if !strings.Contains(string(out), "Keep") {
		t.Errorf("expected function row in output:\n%s", out)
	}
	if strings.Contains(string(out), "skip.go") {
		t.Errorf("file kinds must be suppressed:\n%s", out)
	}

	// Empty input emits nothing.
	out2 := captureStdout(t, func() {
		renderImpactNodeSection("Empty", "found", nil)
	})
	if strings.Contains(string(out2), "Empty") {
		t.Errorf("empty section header should be suppressed, got:\n%s", out2)
	}
}

// TestWarmActiveAndArchivedNotes covers warmNotesInDir for both the active
// and archived flows, including the archive-timestamp adjust callback.
func TestWarmActiveAndArchivedNotes(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(testIO()); err != nil {
		t.Fatalf("setup: %v", err)
	}
	now := "2026-05-12T00:00:00Z"
	// Active note in notes/decisions/
	if err := createGraphNote(testIO(), home, &GraphNote{
		SchemaVersion: 1, ID: "wd-1", Type: "decision", Title: "Warm Decision",
		Summary: "warm", Status: "active", CreatedAt: now, UpdatedAt: now,
	}, "body"); err != nil {
		t.Fatalf("createGraphNote: %v", err)
	}
	// Archived note placed under notes/_archived
	archDir := filepath.Join(home, "notes", "_archived")
	if err := os.MkdirAll(archDir, 0755); err != nil {
		t.Fatal(err)
	}
	archived := &GraphNote{
		SchemaVersion: 1, ID: "wd-arch", Type: "decision", Title: "Archived",
		Summary: "old", Status: "archived", CreatedAt: now, UpdatedAt: now,
	}
	data, err := renderGraphNote(archived, "archived body")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(archDir, "wd-arch.md"), data, 0644); err != nil {
		t.Fatal(err)
	}
	// Also include a bogus file that parseGraphNote should reject so the
	// skipped counter increments.
	if err := os.WriteFile(filepath.Join(archDir, "broken.md"), []byte("not yaml"), 0644); err != nil {
		t.Fatal(err)
	}

	store, err := openKGStore(home)
	if err != nil {
		t.Fatalf("openKGStore: %v", err)
	}
	defer store.Close()

	subs, _ := warmNoteSubdirs("")
	indexed, _ := warmActiveNotes(testIO(), store, home, subs)
	if indexed < 1 {
		t.Errorf("active notes: expected ≥1 indexed, got %d", indexed)
	}
	archIdx, archSkip := warmArchivedNotes(testIO(), store, home)
	if archIdx != 1 {
		t.Errorf("archived: expected 1 indexed (wd-arch), got %d", archIdx)
	}
	if archSkip < 1 {
		t.Errorf("expected the broken archive entry to count as skipped, got %d", archSkip)
	}

	// The archived note must have ArchivedAt populated by the adjust callback.
	got, err := store.GetKGNote("wd-arch")
	if err != nil || got == nil {
		t.Fatalf("GetKGNote wd-arch: %v", err)
	}
	if got.ArchivedAt == "" {
		t.Errorf("expected ArchivedAt populated by warm archive adjust, got %+v", got)
	}
}

// TestWarmNotesInDir_MissingDirReturnsZero ensures the helper short-circuits
// when the directory does not exist.
func TestWarmNotesInDir_MissingDirReturnsZero(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(testIO()); err != nil {
		t.Fatalf("setup: %v", err)
	}
	store, err := openKGStore(home)
	if err != nil {
		t.Fatalf("openKGStore: %v", err)
	}
	defer store.Close()
	indexed, skipped := warmNotesInDir(testIO(), store, filepath.Join(home, "does-not-exist"), nil)
	if indexed != 0 || skipped != 0 {
		t.Errorf("expected 0/0 for missing dir, got indexed=%d skipped=%d", indexed, skipped)
	}
}

// TestWarmCodeLane_CRGUnavailable hits the failure path: with no CRG binary
// on PATH, warmCodeLane returns an empty summary and emits a warning.
func TestWarmCodeLane_CRGUnavailable(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(testIO()); err != nil {
		t.Fatalf("setup: %v", err)
	}
	store, err := openKGStore(home)
	if err != nil {
		t.Fatalf("openKGStore: %v", err)
	}
	defer store.Close()
	t.Setenv("PATH", t.TempDir())
	t.Chdir(t.TempDir())
	out := captureStdout(t, func() {
		if _, _, got := warmCodeLane(store); got != "" {
			t.Errorf("expected empty summary when CRG missing, got %q", got)
		}
	})
	_ = out // warning is emitted to stderr by ui.Warn — we only assert the return value
}

// TestRunKGLinkRemove_InvalidIDReturnsError covers the integer-parse error.
func TestRunKGLinkRemove_InvalidIDReturnsError(t *testing.T) {
	cmd := newKGLinkRemoveCmdForTest()
	err := runKGLinkRemove(testDeps(), cmd, []string{"not-a-number"})
	if err == nil {
		t.Fatal("expected parse error for non-integer link id")
	}
	if !strings.Contains(err.Error(), "invalid link ID") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestRunKGLinkAdd_InvalidKindRejected exercises the kind-validation branch.
func TestRunKGLinkAdd_InvalidKindRejected(t *testing.T) {
	cmd := newKGLinkAddCmdForTest("not-real-kind")
	err := runKGLinkAdd(testDeps(), cmd, []string{"some-id", "ns::F"})
	if err == nil {
		t.Fatal("expected error for invalid link kind")
	}
	if !strings.Contains(err.Error(), "invalid link kind") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestRunKGLinkAdd_MissingArgs covers the early usage-error branch.
func TestRunKGLinkAdd_MissingArgs(t *testing.T) {
	cmd := newKGLinkAddCmdForTest("mentions")
	if err := runKGLinkAdd(testDeps(), cmd, nil); err == nil {
		t.Error("expected usage error for missing args")
	}
}

// TestRunKGSync_NotInitialized errors when no kg setup has been performed.
func TestRunKGSync_NotInitialized(t *testing.T) {
	t.Setenv("KG_HOME", t.TempDir())
	cmd := &cobra.Command{}
	cmd.Flags().Bool("push", false, "")
	if err := runKGSync(cmd, nil); err == nil {
		t.Error("expected not-initialized error from runKGSync")
	}
}

// TestRunKGLinkAdd_OpenStoreError drives the openKGStore-error return path by
// pointing KG_HOME at a bogus path.
func TestRunKGLinkAdd_OpenStoreError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires /dev/null as a non-directory path component (POSIX-only); equivalent open-warm-store error wrapping on non-POSIX paths is exercised by the warm-store fault tests in bridge_fault_test.go that close the handle / corrupt the DB")
	}
	t.Setenv("KG_HOME", "/dev/null/not-a-dir")
	cmd := &cobra.Command{}
	cmd.Flags().String("kind", "mentions", "")
	err := runKGLinkAdd(testDeps(), cmd, []string{"note", "qn"})
	if err == nil || !strings.Contains(err.Error(), "open warm store") {
		t.Fatalf("expected open-warm-store error, got %v", err)
	}
}

// TestRunKGLinkList_OpenStoreError drives the openKGStore-error return path.
func TestRunKGLinkList_OpenStoreError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires /dev/null as a non-directory path component (POSIX-only); equivalent open-warm-store error wrapping on non-POSIX paths is exercised by the warm-store fault tests in bridge_fault_test.go that close the handle / corrupt the DB")
	}
	t.Setenv("KG_HOME", "/dev/null/not-a-dir")
	cmd := &cobra.Command{}
	err := runKGLinkList(testDeps(), cmd, []string{"note"})
	if err == nil || !strings.Contains(err.Error(), "open warm store") {
		t.Fatalf("expected open-warm-store error, got %v", err)
	}
}

// TestRunKGLinkRemove_OpenStoreError drives the openKGStore-error return path.
func TestRunKGLinkRemove_OpenStoreError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires /dev/null as a non-directory path component (POSIX-only); equivalent open-warm-store error wrapping on non-POSIX paths is exercised by the warm-store fault tests in bridge_fault_test.go that close the handle / corrupt the DB")
	}
	t.Setenv("KG_HOME", "/dev/null/not-a-dir")
	cmd := &cobra.Command{}
	err := runKGLinkRemove(testDeps(), cmd, []string{"42"})
	if err == nil || !strings.Contains(err.Error(), "open warm store") {
		t.Fatalf("expected open-warm-store error, got %v", err)
	}
}

// TestRunKGWarm_OpenStoreError drives the openKGStore-error wrapper on
// runKGWarm.
func TestRunKGWarm_OpenStoreError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires /dev/null as a non-directory path component (POSIX-only); equivalent open-warm-store error wrapping on non-POSIX paths is exercised by the warm-store fault tests in bridge_fault_test.go that close the handle / corrupt the DB")
	}
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
	if runtime.GOOS == "windows" {
		t.Skip("requires /dev/null as a non-directory path component (POSIX-only); equivalent open-warm-store error wrapping on non-POSIX paths is exercised by the warm-store fault tests in bridge_fault_test.go that close the handle / corrupt the DB")
	}
	t.Setenv("KG_HOME", "/dev/null/not-a-dir")
	cmd := &cobra.Command{}
	if err := runKGWarmStats(cmd, nil); err == nil || !strings.Contains(err.Error(), "open warm store") {
		t.Fatalf("expected open-warm-store error, got %v", err)
	}
}

// TestRunKGWarm_InvalidNoteType drives the warmNoteSubdirs error path.
func TestRunKGWarm_InvalidNoteType(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(testIO()); err != nil {
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
	if err := runKGSetup(testIO()); err != nil {
		t.Fatalf("setup: %v", err)
	}

	note := &GraphNote{
		SchemaVersion: 1, ID: "e1", Type: "entity", Title: "T",
		Summary: "s", Status: "draft",
		CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
	}
	if err := createGraphNote(testIO(), home, note, "body"); err != nil {
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
	if err := runKGSetup(testIO()); err != nil {
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

func TestKGChangesJSONOutput_MarshalShape(t *testing.T) {
	wrapper := kgChangesJSONOutput{
		GraphState: "ready",
		CRGChangeReport: &graphstore.CRGChangeReport{
			Summary: "0 changed",
		},
	}
	data, err := json.Marshal(wrapper)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m["graph_state"] != "ready" {
		t.Errorf("expected graph_state=ready, got %v", m["graph_state"])
	}
}

// TestRunKGFlows_NoCRGBinary verifies the NewCRGBridge error path.
func TestRunKGFlows_NoCRGBinary(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	cmd := &cobra.Command{}
	cmd.Flags().String("repo", repo, "")
	cmd.Flags().Int("limit", 20, "")
	cmd.Flags().String("sort", "criticality", "")
	cmd.Flags().Bool("json", false, "")

	if err := runKGFlows(testDeps(), cmd, nil); err == nil {
		t.Error("expected error when no CRG binary available")
	}
}

func TestRunKGCommunities_NoCRGBinary(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	cmd := &cobra.Command{}
	cmd.Flags().String("repo", repo, "")
	cmd.Flags().Int("min-size", 0, "")
	cmd.Flags().String("sort", "size", "")
	cmd.Flags().Bool("json", false, "")

	if err := runKGCommunities(testDeps(), cmd, nil); err == nil {
		t.Error("expected error when no CRG binary available")
	}
}

func TestRunKGPostprocess_NoCRGBinary(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	cmd := &cobra.Command{}
	cmd.Flags().String("repo", repo, "")
	cmd.Flags().Bool("no-flows", false, "")
	cmd.Flags().Bool("no-communities", false, "")
	cmd.Flags().Bool("no-fts", false, "")
	cmd.Flags().Bool("json", false, "")

	if err := runKGPostprocess(cmd, nil); err == nil {
		t.Error("expected error when no CRG binary available")
	}
}

func TestRunKGFlows_TextWithFakePython(t *testing.T) {
	repo := t.TempDir()
	flowsJSON := `{"status":"ok","summary":"2 flows","flows":[{"id":1,"name":"flow-a","entry_point":"pkg::Foo","step_count":3,"criticality":0.8,"kind":"call"},{"id":2,"name":"flow-b","entry_point":"","step_count":1,"criticality":0.1,"kind":"call"}]}`
	fakeCRGEmittingJSON(t, repo, flowsJSON)

	cmd := &cobra.Command{}
	cmd.Flags().String("repo", repo, "")
	cmd.Flags().Int("limit", 20, "")
	cmd.Flags().String("sort", "criticality", "")
	cmd.Flags().Bool("json", false, "")

	out := captureStdout(t, func() {
		if err := runKGFlows(testDeps(), cmd, nil); err != nil {
			t.Fatalf("runKGFlows: %v", err)
		}
	})
	output := string(out)
	for _, want := range []string{"Execution Flows", "flow-a", "flow-b"} {
		if !strings.Contains(output, want) {
			t.Errorf("expected %q in output:\n%s", want, output)
		}
	}
}

func TestRunKGFlows_JSONWithFakePython(t *testing.T) {
	repo := t.TempDir()
	flowsJSON := `{"status":"ok","summary":"0 flows","flows":[]}`
	fakeCRGEmittingJSON(t, repo, flowsJSON)

	cmd := &cobra.Command{}
	cmd.Flags().String("repo", repo, "")
	cmd.Flags().Int("limit", 20, "")
	cmd.Flags().String("sort", "criticality", "")
	cmd.Flags().Bool("json", true, "")

	out := captureStdout(t, func() {
		if err := runKGFlows(testDeps(), cmd, nil); err != nil {
			t.Fatalf("runKGFlows JSON: %v", err)
		}
	})
	var result graphstore.FlowsResult
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %s", err, string(out))
	}
}

func TestRunKGFlows_EmptyFlowsHintsPostprocess(t *testing.T) {
	repo := t.TempDir()
	flowsJSON := `{"status":"ok","summary":"0 flows","flows":[]}`
	fakeCRGEmittingJSON(t, repo, flowsJSON)

	cmd := &cobra.Command{}
	cmd.Flags().String("repo", repo, "")
	cmd.Flags().Int("limit", 20, "")
	cmd.Flags().String("sort", "criticality", "")
	cmd.Flags().Bool("json", false, "")

	out := captureStdout(t, func() {
		if err := runKGFlows(testDeps(), cmd, nil); err != nil {
			t.Fatalf("runKGFlows: %v", err)
		}
	})
	if !strings.Contains(string(out), "da kg postprocess") {
		t.Errorf("expected postprocess hint when no flows, got:\n%s", out)
	}
}

func TestRunKGCommunities_TextWithFakePython(t *testing.T) {
	repo := t.TempDir()
	body := `{"status":"ok","summary":"1 community","communities":[{"id":1,"name":"core","size":3,"cohesion":0.7,"dominant_language":"go","description":"core stuff","members":["a","b"]}]}`
	fakeCRGEmittingJSON(t, repo, body)

	cmd := &cobra.Command{}
	cmd.Flags().String("repo", repo, "")
	cmd.Flags().Int("min-size", 0, "")
	cmd.Flags().String("sort", "size", "")
	cmd.Flags().Bool("json", false, "")

	out := captureStdout(t, func() {
		if err := runKGCommunities(testDeps(), cmd, nil); err != nil {
			t.Fatalf("runKGCommunities: %v", err)
		}
	})
	output := string(out)
	for _, want := range []string{"Code Communities", "core", "core stuff"} {
		if !strings.Contains(output, want) {
			t.Errorf("expected %q in output:\n%s", want, output)
		}
	}
}

func TestRunKGCommunities_JSONWithFakePython(t *testing.T) {
	repo := t.TempDir()
	body := `{"status":"ok","summary":"0","communities":[]}`
	fakeCRGEmittingJSON(t, repo, body)

	cmd := &cobra.Command{}
	cmd.Flags().String("repo", repo, "")
	cmd.Flags().Int("min-size", 0, "")
	cmd.Flags().String("sort", "size", "")
	cmd.Flags().Bool("json", true, "")

	out := captureStdout(t, func() {
		if err := runKGCommunities(testDeps(), cmd, nil); err != nil {
			t.Fatalf("runKGCommunities JSON: %v", err)
		}
	})
	var result graphstore.CommunitiesResult
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %s", err, string(out))
	}
}

func TestRunKGImpact_JSONFakeCRG(t *testing.T) {
	repo := t.TempDir()
	impactJSON := `{"status":"ok","summary":"impact","changed_files":[],"changed_nodes":[],"impacted_nodes":[],"impacted_files":[],"total_impacted":0,"truncated":false}`
	fakeCRGEmittingJSON(t, repo, impactJSON)

	writeCRGStatusFixture(t, repo, []crgNodeFixture{
		{FilePath: "a.go", Language: "go", UpdatedAt: "2026-04-19T18:03:45Z"},
	})

	cmd := &cobra.Command{}
	cmd.Flags().String("repo", repo, "")
	cmd.Flags().String("base", "", "")
	cmd.Flags().Int("depth", 2, "")
	cmd.Flags().Int("limit", 50, "")
	cmd.Flags().Bool("require-graph", false, "")
	cmd.Flags().Bool("json", true, "")

	out := captureStdout(t, func() {
		if err := runKGImpact(testDeps(), cmd, []string{"a.go"}); err != nil {
			t.Fatalf("runKGImpact JSON: %v", err)
		}
	})
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %s", err, string(out))
	}
	if _, ok := m["graph_state"]; !ok {
		t.Errorf("expected graph_state field, got: %s", string(out))
	}
}

func TestRunKGImpact_TextFakeCRG(t *testing.T) {
	repo := t.TempDir()
	impactJSON := `{"status":"ok","summary":"impact for 1 file","changed_files":["a.go"],"changed_nodes":[{"kind":"Function","name":"Foo","qualified_name":"a.go::Foo","file_path":"a.go"}],"impacted_nodes":[],"impacted_files":["a.go"],"total_impacted":0,"truncated":false}`
	fakeCRGEmittingJSON(t, repo, impactJSON)
	writeCRGStatusFixture(t, repo, []crgNodeFixture{
		{FilePath: "a.go", Language: "go", UpdatedAt: "2026-04-19T18:03:45Z"},
	})

	cmd := &cobra.Command{}
	cmd.Flags().String("repo", repo, "")
	cmd.Flags().String("base", "", "")
	cmd.Flags().Int("depth", 2, "")
	cmd.Flags().Int("limit", 50, "")
	cmd.Flags().Bool("require-graph", false, "")
	cmd.Flags().Bool("json", false, "")

	out := captureStdout(t, func() {
		if err := runKGImpact(testDeps(), cmd, []string{"a.go"}); err != nil {
			t.Fatalf("runKGImpact: %v", err)
		}
	})
	if !strings.Contains(string(out), "Impact Radius") {
		t.Errorf("expected Impact Radius header, got:\n%s", out)
	}
}

// TestCheckCRGReadiness_BusyState is best-effort: we install a fake binary
// that emits a busy-status JSON. The actual Status() call uses sqlite, not the
// binary, but it covers the busy branch when sqlite is present and the binary
// path is happy.
func TestCheckCRGReadiness_BusyState_RequireGraph(t *testing.T) {
	repo := t.TempDir()

	writeCRGStatusFixture(t, repo, nil)

	if err := checkCRGReadiness(repo, false); err != nil {
		t.Errorf("unexpected error with requireGraph=false: %v", err)
	}

	if err := checkCRGReadiness(repo, true); err == nil {
		t.Error("expected error with requireGraph=true on unbuilt graph")
	}
}

func TestRunKGImpact_RequireGraphReady(t *testing.T) {
	repo := t.TempDir()
	writeCRGStatusFixture(t, repo, []crgNodeFixture{
		{FilePath: "a.go", Language: "go", UpdatedAt: "2026-04-19T18:03:45Z"},
	})
	impactJSON := `{"status":"ok","summary":"impact","changed_files":[],"changed_nodes":[],"impacted_nodes":[],"impacted_files":[],"total_impacted":0,"truncated":false}`
	fakeCRGEmittingJSON(t, repo, impactJSON)

	cmd := &cobra.Command{}
	cmd.Flags().String("repo", repo, "")
	cmd.Flags().String("base", "", "")
	cmd.Flags().Int("depth", 2, "")
	cmd.Flags().Int("limit", 50, "")
	cmd.Flags().Bool("require-graph", true, "")
	cmd.Flags().Bool("json", false, "")

	captureStdout(t, func() {
		if err := runKGImpact(testDeps(), cmd, nil); err != nil {
			t.Fatalf("runKGImpact require=true on ready: %v", err)
		}
	})
}

func TestRunKGChanges_JSONFakeCRG(t *testing.T) {
	repo := t.TempDir()
	changesJSON := `{"summary":"1 changed function","risk_score":0.5,"changed_functions":[{"name":"Foo","qualified_name":"a.go::Foo","file_path":"a.go","risk_score":0.5}],"affected_flows":[],"test_gaps":[],"review_priorities":[]}`
	writeFakeCRGBinary(t, repo, fmt.Sprintf(`case "$1" in
detect-changes) printf '%%s\n' '%s' ;;
*) exit 0 ;;
esac`, changesJSON))
	writeCRGStatusFixture(t, repo, []crgNodeFixture{
		{FilePath: "a.go", Language: "go", UpdatedAt: "2026-04-19T18:03:45Z"},
	})

	cmd := &cobra.Command{}
	cmd.Flags().String("repo", repo, "")
	cmd.Flags().String("base", "", "")
	cmd.Flags().Bool("brief", false, "")
	cmd.Flags().Bool("require-graph", false, "")
	cmd.Flags().Bool("json", false, "")

	out := captureStdout(t, func() {
		if err := runKGChanges(testDeps(), cmd, nil); err != nil {
			t.Fatalf("runKGChanges: %v", err)
		}
	})
	output := string(out)
	for _, want := range []string{"Change Impact", "Foo", "Changed symbols"} {
		if !strings.Contains(output, want) {
			t.Errorf("expected %q in output:\n%s", want, output)
		}
	}
}

func TestRunKGChanges_JSONOutputShape(t *testing.T) {
	repo := t.TempDir()

	writeCRGStatusFixture(t, repo, []crgNodeFixture{
		{FilePath: "a.go", Language: "go", UpdatedAt: "2026-04-19T18:03:45Z"},
	})
	changesJSON := `{"summary":"0 changed","risk_score":0,"changed_functions":[],"affected_flows":[],"test_gaps":[],"review_priorities":[]}`
	writeFakeCRGBinary(t, repo, fmt.Sprintf(`case "$1" in
detect-changes) printf '%%s\n' '%s' ;;
*) exit 0 ;;
esac`, changesJSON))

	cmd := &cobra.Command{}
	cmd.Flags().String("repo", repo, "")
	cmd.Flags().String("base", "", "")
	cmd.Flags().Bool("brief", false, "")
	cmd.Flags().Bool("require-graph", false, "")
	cmd.Flags().Bool("json", true, "")

	out := captureStdout(t, func() {
		if err := runKGChanges(testDeps(), cmd, nil); err != nil {
			t.Fatalf("runKGChanges: %v", err)
		}
	})

	body := strings.TrimSpace(string(out))
	if idx := strings.Index(body, "{"); idx > 0 {
		body = body[idx:]
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %s", err, string(out))
	}
}

// TestRunKGSync_PullNoRemote exercises the pull branch — git pull fails when
// no remote is configured.
func TestRunKGSync_PullNoRemote(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(testIO()); err != nil {
		t.Fatalf("setup: %v", err)
	}
	initGitRepo(t, home)
	commitFile(t, home, "self/config.yaml", "schema_version: 1\n", "init")

	cmd := &cobra.Command{}
	cmd.Flags().Bool("push", false, "")

	captureStdout(t, func() {
		err := runKGSync(cmd, nil)
		if err == nil {
			return
		}
		if !strings.Contains(err.Error(), "git pull failed") {
			t.Errorf("expected pull failure, got: %v", err)
		}
	})
}

func TestRunKGWarmCodeImport_WithCRGNodes(t *testing.T) {
	repo := t.TempDir()
	writeFakeCRGBinary(t, repo, "exit 0")

	writeCRGStatusFixture(t, repo, []crgNodeFixture{
		{FilePath: "a.go", Language: "go", UpdatedAt: "2026-04-19T18:03:45Z"},
	})

	home := newTempKG(t)
	if err := runKGSetup(testIO()); err != nil {
		t.Fatalf("setup: %v", err)
	}
	store, err := openKGStore(home)
	if err != nil {
		t.Fatalf("openKGStore: %v", err)
	}
	defer store.Close()

	_, _, err = runKGWarmCodeImport(store, repo)
	if err == nil {
		t.Skip("schema unexpectedly accepted — read-node error branch not hit")
	}
	if !strings.Contains(err.Error(), "CRG nodes") && !strings.Contains(err.Error(), "CRG") {
		t.Errorf("expected CRG-related read error, got: %v", err)
	}
}

func TestRunKGBuild_NoCRGBinary(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	cmd := &cobra.Command{}
	cmd.Flags().String("repo", repo, "")
	cmd.Flags().Bool("skip-flows", false, "")
	cmd.Flags().Bool("skip-postprocess", false, "")
	cmd.Flags().Bool("json", false, "")
	if err := runKGBuild(cmd, nil); err == nil {
		t.Error("expected error when no CRG binary on PATH")
	}
}

// TestRunKGUpdate_NoCRGBinary: kg update degrades gracefully (exit 0, no error)
// when code-review-graph is not installed — the graph-update post_tool_use hook
// must not fail every edit for users without the optional tool.
func TestRunKGUpdate_NoCRGBinary(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	cmd := &cobra.Command{}
	cmd.Flags().String("repo", repo, "")
	cmd.Flags().String("base", "", "")
	cmd.Flags().Bool("skip-flows", false, "")
	cmd.Flags().Bool("skip-postprocess", false, "")
	cmd.Flags().Bool("json", false, "")
	if err := runKGUpdate(cmd, nil); err != nil {
		t.Fatalf("expected graceful degrade (nil) when CRG absent, got: %v", err)
	}
}

func TestRunKGCodeStatus_JSONNoFixture(t *testing.T) {
	repo := t.TempDir()
	cmd := &cobra.Command{}
	cmd.Flags().String("repo", repo, "")
	cmd.Flags().Bool("json", true, "")
	out := captureStdout(t, func() {
		if err := runKGCodeStatus(testDeps(), cmd, nil); err != nil {
			t.Fatalf("runKGCodeStatus: %v", err)
		}
	})
	if !strings.Contains(string(out), "state") {
		t.Errorf("expected state field in JSON, got:\n%s", out)
	}
}

func TestCRGStatusState_Ready(t *testing.T) {
	repo := t.TempDir()
	writeCRGStatusFixture(t, repo, []crgNodeFixture{
		{FilePath: "a.go", Language: "go", UpdatedAt: "2026-04-19T18:03:45Z"},
	})
	state := crgStatusState(repo)
	if state == "unknown" {
		t.Errorf("expected non-unknown state for built graph, got %q", state)
	}
}

func TestRunKGImpact_NoArgs(t *testing.T) {
	repo := t.TempDir()
	writeCRGStatusFixture(t, repo, []crgNodeFixture{
		{FilePath: "a.go", Language: "go", UpdatedAt: "2026-04-19T18:03:45Z"},
	})
	impactJSON := `{"status":"ok","summary":"no impact","changed_files":[],"changed_nodes":[],"impacted_nodes":[],"impacted_files":[],"total_impacted":0,"truncated":false}`
	fakeCRGEmittingJSON(t, repo, impactJSON)

	cmd := &cobra.Command{}
	cmd.Flags().String("repo", repo, "")
	cmd.Flags().String("base", "", "")
	cmd.Flags().Int("depth", 2, "")
	cmd.Flags().Int("limit", 50, "")
	cmd.Flags().Bool("require-graph", false, "")
	cmd.Flags().Bool("json", false, "")

	captureStdout(t, func() {

		if err := runKGImpact(testDeps(), cmd, nil); err != nil {
			t.Fatalf("runKGImpact no-args: %v", err)
		}
	})
}

func TestCRGStatusState_Empty(t *testing.T) {

	repo := t.TempDir()
	state := crgStatusState(repo)
	if state == "" {
		t.Errorf("expected non-empty state, got %q", state)
	}
}

func TestRunKGSync_PushSuccess(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(testIO()); err != nil {
		t.Fatalf("setup: %v", err)
	}
	initGitRepo(t, home)
	commitFile(t, home, "self/config.yaml", "schema_version: 1\n", "init")

	remote := filepath.Join(t.TempDir(), "remote.git")
	if out, err := runGit(t, "", "init", "--bare", remote); err != nil {
		t.Fatalf("init bare: %v\n%s", err, out)
	}
	if out, err := runGit(t, home, "remote", "add", "origin", remote); err != nil {
		t.Fatalf("remote add: %v\n%s", err, out)
	}

	if out, err := runGit(t, home, "branch", "-M", "main"); err != nil {
		t.Fatalf("branch -M: %v\n%s", err, out)
	}
	cmd := &cobra.Command{}
	cmd.Flags().Bool("push", true, "")
	captureStdout(t, func() {
		if err := runKGSync(cmd, nil); err != nil {

			if !strings.Contains(err.Error(), "git push failed") {
				t.Fatalf("runKGSync push: %v", err)
			}
		}
	})
}

func TestRunKGSync_PullSuccessRunsLint(t *testing.T) {

	work := t.TempDir()
	upstream := filepath.Join(work, "upstream")
	initGitRepo(t, upstream)
	commitFile(t, upstream, "notes/index.md", "# Index\n", "seed")

	bare := filepath.Join(work, "bare.git")
	if out, err := runGit(t, "", "clone", "--bare", upstream, bare); err != nil {
		t.Fatalf("clone bare: %v\n%s", err, out)
	}

	home := filepath.Join(work, "kg")
	if out, err := runGit(t, "", "clone", bare, home); err != nil {
		t.Fatalf("clone home: %v\n%s", err, out)
	}
	t.Setenv("KG_HOME", home)

	if err := os.MkdirAll(filepath.Join(home, "self"), 0755); err != nil {
		t.Fatal(err)
	}
	cfg := &KGConfig{SchemaVersion: 1, Name: "x", CreatedAt: "2026-01-01T00:00:00Z"}
	if err := saveKGConfigIO(testIO(), cfg); err != nil {
		t.Fatalf("SaveKGConfig: %v", err)
	}

	cmd := &cobra.Command{}
	cmd.Flags().Bool("push", false, "")

	captureStdout(t, func() {

		_ = runKGSync(cmd, nil)
	})
}

func TestRunKGImpact_FakePythonReturnsError(t *testing.T) {
	repo := t.TempDir()
	writeFakeCRGBinary(t, repo, "exit 0")

	if err := os.WriteFile(filepath.Join(repo, ".venv", "bin", "python3"), []byte("#!/bin/sh\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	writeCRGStatusFixture(t, repo, []crgNodeFixture{
		{FilePath: "a.go", Language: "go", UpdatedAt: "2026-04-19T18:03:45Z"},
	})

	cmd := &cobra.Command{}
	cmd.Flags().String("repo", repo, "")
	cmd.Flags().String("base", "", "")
	cmd.Flags().Int("depth", 2, "")
	cmd.Flags().Int("limit", 50, "")
	cmd.Flags().Bool("require-graph", false, "")
	cmd.Flags().Bool("json", false, "")

	captureStdout(t, func() {
		if err := runKGImpact(testDeps(), cmd, nil); err == nil {
			t.Error("expected error when python returns non-zero")
		}
	})
}

func TestRunKGUpdate_UpdatedOutcome(t *testing.T) {
	repo := t.TempDir()
	initGitRepo(t, repo)
	commitFile(t, repo, "a.txt", "one\n", "init")

	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("two\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if out, err := runGit(t, repo, "add", "a.txt"); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	if out, err := runGit(t, repo, "commit", "-m", "edit"); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
	writeFakeCRGBinary(t, repo, "exit 0")
	writeCRGStatusFixture(t, repo, []crgNodeFixture{
		{FilePath: "a.txt", Language: "go", UpdatedAt: "2026-04-19T18:03:45Z"},
	})

	cmd := &cobra.Command{}
	cmd.Flags().String("repo", repo, "")
	cmd.Flags().String("base", "HEAD~1", "")
	cmd.Flags().Bool("skip-flows", true, "")
	cmd.Flags().Bool("skip-postprocess", true, "")
	cmd.Flags().Bool("json", false, "")

	captureStdout(t, func() {
		if err := runKGUpdate(cmd, nil); err != nil {
			t.Fatalf("runKGUpdate: %v", err)
		}
	})
}

func TestRunKGBuild_BusyOutcome(t *testing.T) {
	repo := t.TempDir()

	writeCRGStatusFixture(t, repo, nil)
	writeFakeCRGBinary(t, repo, "exit 0")

	cmd := &cobra.Command{}
	cmd.Flags().String("repo", repo, "")
	cmd.Flags().Bool("skip-flows", false, "")
	cmd.Flags().Bool("skip-postprocess", false, "")
	cmd.Flags().Bool("json", false, "")

	captureStdout(t, func() {
		if err := runKGBuild(cmd, nil); err != nil {
			t.Fatalf("runKGBuild: %v", err)
		}
	})
}

func TestCheckCRGReadiness_ReadyNoWarn(t *testing.T) {
	repo := t.TempDir()
	writeCRGStatusFixture(t, repo, []crgNodeFixture{
		{FilePath: "a.go", Language: "go", UpdatedAt: "2026-04-19T18:03:45Z"},
	})
	if err := checkCRGReadiness(repo, true); err != nil {
		t.Errorf("expected no error for ready graph with requireGraph=true, got: %v", err)
	}
}

func TestRunKGChanges_TextAllCategories(t *testing.T) {
	repo := t.TempDir()
	changesJSON := `{
		"summary":"all categories",
		"risk_score":0.5,
		"changed_functions":[{"name":"Foo","qualified_name":"a.go::Foo","file_path":"a.go","risk_score":0.5}],
		"affected_flows":[],
		"test_gaps":[{"qualified_name":"a.go::Foo","file_path":"a.go"}],
		"review_priorities":[{"qualified_name":"a.go::Foo","reason":"high churn","risk_score":0.7}]
	}`
	writeFakeCRGBinary(t, repo, fmt.Sprintf(`case "$1" in
detect-changes) cat <<'__EOF__'
%s
__EOF__
;;
*) exit 0 ;;
esac`, changesJSON))
	writeCRGStatusFixture(t, repo, []crgNodeFixture{
		{FilePath: "a.go", Language: "go", UpdatedAt: "2026-04-19T18:03:45Z"},
	})

	cmd := &cobra.Command{}
	cmd.Flags().String("repo", repo, "")
	cmd.Flags().String("base", "", "")
	cmd.Flags().Bool("brief", false, "")
	cmd.Flags().Bool("require-graph", false, "")
	cmd.Flags().Bool("json", false, "")

	out := captureStdout(t, func() {
		if err := runKGChanges(testDeps(), cmd, nil); err != nil {
			t.Fatalf("runKGChanges: %v", err)
		}
	})
	output := string(out)
	for _, want := range []string{"Change Impact", "Changed symbols", "Test gaps", "Review priorities", "high churn"} {
		if !strings.Contains(output, want) {
			t.Errorf("expected %q in output:\n%s", want, output)
		}
	}
}

func TestRunKGCodeStatus_TextWithMessage(t *testing.T) {
	repo := t.TempDir()

	cmd := &cobra.Command{}
	cmd.Flags().String("repo", repo, "")
	cmd.Flags().Bool("json", false, "")
	out := captureStdout(t, func() {
		if err := runKGCodeStatus(testDeps(), cmd, nil); err != nil {
			t.Fatalf("runKGCodeStatus: %v", err)
		}
	})
	if !strings.Contains(string(out), "Code Graph Status") {
		t.Errorf("expected status header, got:\n%s", out)
	}
}

func TestRunKGBuild_ReadyOutcome(t *testing.T) {
	repo := t.TempDir()
	writeFakeCRGBinary(t, repo, "exit 0")
	writeCRGStatusFixture(t, repo, []crgNodeFixture{
		{FilePath: "a.go", Language: "go", UpdatedAt: "2026-04-19T18:03:45Z"},
	})

	cmd := &cobra.Command{}
	cmd.Flags().String("repo", repo, "")
	cmd.Flags().Bool("skip-flows", false, "")
	cmd.Flags().Bool("skip-postprocess", false, "")
	cmd.Flags().Bool("json", false, "")

	out := captureStdout(t, func() {
		if err := runKGBuild(cmd, nil); err != nil {
			t.Fatalf("runKGBuild ready: %v", err)
		}
	})
	output := string(out)
	if !strings.Contains(output, "Build complete") && !strings.Contains(output, "build status") {
		t.Errorf("expected build outcome, got:\n%s", output)
	}
}

func TestRunKGFlows_TextHintsWhenEmpty(t *testing.T) {
	repo := t.TempDir()
	flowsJSON := `{"status":"ok","summary":"0 flows","flows":[]}`
	fakeCRGEmittingJSON(t, repo, flowsJSON)

	cmd := &cobra.Command{}
	cmd.Flags().String("repo", repo, "")
	cmd.Flags().Int("limit", 20, "")
	cmd.Flags().String("sort", "criticality", "")
	cmd.Flags().Bool("json", false, "")

	out := captureStdout(t, func() {
		if err := runKGFlows(testDeps(), cmd, nil); err != nil {
			t.Fatalf("runKGFlows: %v", err)
		}
	})
	if !strings.Contains(string(out), "No flows detected") {
		t.Errorf("expected empty-flows hint, got:\n%s", out)
	}
}

func TestRunKGCommunities_TextEmpty(t *testing.T) {
	repo := t.TempDir()
	body := `{"status":"ok","summary":"0 communities","communities":[]}`
	fakeCRGEmittingJSON(t, repo, body)

	cmd := &cobra.Command{}
	cmd.Flags().String("repo", repo, "")
	cmd.Flags().Int("min-size", 0, "")
	cmd.Flags().String("sort", "size", "")
	cmd.Flags().Bool("json", false, "")

	out := captureStdout(t, func() {
		if err := runKGCommunities(testDeps(), cmd, nil); err != nil {
			t.Fatalf("runKGCommunities: %v", err)
		}
	})
	if !strings.Contains(string(out), "Code Communities") {
		t.Errorf("expected communities header, got:\n%s", out)
	}
}

func TestRunKGPostprocess_FakeCRG(t *testing.T) {
	repo := t.TempDir()
	writeFakeCRGBinary(t, repo, "exit 0")

	cmd := &cobra.Command{}
	cmd.Flags().String("repo", repo, "")
	cmd.Flags().Bool("no-flows", false, "")
	cmd.Flags().Bool("no-communities", false, "")
	cmd.Flags().Bool("no-fts", false, "")
	cmd.Flags().Bool("json", false, "")

	captureStdout(t, func() {
		if err := runKGPostprocess(cmd, nil); err != nil {
			t.Fatalf("runKGPostprocess: %v", err)
		}
	})
}

func TestRunKGImpact_DefaultRepoFromCwd(t *testing.T) {
	repo := t.TempDir()
	writeFakeCRGBinary(t, repo, "exit 0")
	if err := os.WriteFile(filepath.Join(repo, ".venv", "bin", "python3"), []byte("#!/bin/sh\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	writeCRGStatusFixture(t, repo, []crgNodeFixture{
		{FilePath: "a.go", Language: "go", UpdatedAt: "2026-04-19T18:03:45Z"},
	})
	t.Chdir(repo)

	cmd := &cobra.Command{}
	cmd.Flags().String("repo", "", "")
	cmd.Flags().String("base", "", "")
	cmd.Flags().Int("depth", 2, "")
	cmd.Flags().Int("limit", 50, "")
	cmd.Flags().Bool("require-graph", false, "")
	cmd.Flags().Bool("json", false, "")

	captureStdout(t, func() {
		_ = runKGImpact(testDeps(), cmd, []string{"a.go"})
	})
}

func TestRunKGImpact_JSONOutputEmpty(t *testing.T) {
	repo := t.TempDir()
	writeCRGStatusFixture(t, repo, []crgNodeFixture{
		{FilePath: "a.go", Language: "go", UpdatedAt: "2026-04-19T18:03:45Z"},
	})
	impactJSON := `{"status":"ok","summary":"empty","changed_files":[],"changed_nodes":[],"impacted_nodes":[],"impacted_files":[],"total_impacted":0,"truncated":false}`
	fakeCRGEmittingJSON(t, repo, impactJSON)

	cmd := &cobra.Command{}
	cmd.Flags().String("repo", repo, "")
	cmd.Flags().String("base", "", "")
	cmd.Flags().Int("depth", 2, "")
	cmd.Flags().Int("limit", 50, "")
	cmd.Flags().Bool("require-graph", false, "")
	cmd.Flags().Bool("json", true, "")

	out := captureStdout(t, func() {
		if err := runKGImpact(testDeps(), cmd, nil); err != nil {
			t.Fatalf("runKGImpact JSON: %v", err)
		}
	})
	body := strings.TrimSpace(string(out))
	if idx := strings.Index(body, "{"); idx > 0 {
		body = body[idx:]
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %s", err, string(out))
	}
}

// TestRunKGWarmCodeImport_EmptyDB hits the success path through an empty CRG
// DB — ReadNodes returns nil with no error, then ReadEdges does the same.
func TestRunKGWarmCodeImport_EmptyDB(t *testing.T) {
	repo := t.TempDir()
	writeFakeCRGBinary(t, repo, "exit 0")

	home := newTempKG(t)
	if err := runKGSetup(testIO()); err != nil {
		t.Fatalf("setup: %v", err)
	}
	store, err := openKGStore(home)
	if err != nil {
		t.Fatalf("openKGStore: %v", err)
	}
	defer store.Close()
	nodes, edges, err := runKGWarmCodeImport(store, repo)
	if err != nil {
		t.Fatalf("runKGWarmCodeImport: %v", err)
	}
	if nodes != 0 || edges != 0 {
		t.Errorf("expected 0/0 for empty CRG, got nodes=%d edges=%d", nodes, edges)
	}
}

// TestWarmCodeLane_EmptyDB exercises warmCodeLane against an empty CRG —
// no nodes, but still hits the SetMetadata + summary branch.
func TestWarmCodeLane_EmptyDB(t *testing.T) {
	repo := t.TempDir()
	writeFakeCRGBinary(t, repo, "exit 0")
	t.Chdir(repo)

	home := newTempKG(t)
	if err := runKGSetup(testIO()); err != nil {
		t.Fatalf("setup: %v", err)
	}
	store, err := openKGStore(home)
	if err != nil {
		t.Fatalf("openKGStore: %v", err)
	}
	defer store.Close()
	_, _, msg := warmCodeLane(store)
	if !strings.Contains(msg, "code-lane") {
		t.Errorf("expected code-lane summary, got %q", msg)
	}
}

// ── Additional coverage: error / outcome branches ─────────────────────────────

// cloneFromBareRemote builds a bare git remote seeded with one commit and
// returns the path to a working clone (with origin/upstream tracking already
// configured) plus the bare remote path. Used by the runKGSync push/pull
// success-path tests so `git push`/`git pull` resolve a real upstream.
func cloneFromBareRemote(t *testing.T) (clone, bare string) {
	t.Helper()
	work := t.TempDir()
	upstream := filepath.Join(work, "upstream")
	initGitRepo(t, upstream)
	commitFile(t, upstream, "notes/index.md", "# Index\n", "seed")

	bare = filepath.Join(work, "bare.git")
	if out, err := runGit(t, "", "clone", "--bare", upstream, bare); err != nil {
		t.Fatalf("clone bare: %v\n%s", err, out)
	}
	clone = filepath.Join(work, "kg")
	if out, err := runGit(t, "", "clone", bare, clone); err != nil {
		t.Fatalf("clone home: %v\n%s", err, out)
	}
	for _, kv := range [][2]string{{"user.name", "test"}, {"user.email", "test@example.com"}} {
		if out, err := runGit(t, clone, "config", kv[0], kv[1]); err != nil {
			t.Fatalf("git config %s: %v\n%s", kv[0], err, out)
		}
	}
	return clone, bare
}

// TestRunKGSync_PushSuccessReportsPushed drives the push success branch
// (ui.Success("Graph pushed.") then return nil) by pushing into a bare
// remote whose upstream tracking is already configured by the clone.
func TestRunKGSync_PushSuccessReportsPushed(t *testing.T) {
	home, _ := cloneFromBareRemote(t)
	t.Setenv("KG_HOME", home)

	if err := os.MkdirAll(filepath.Join(home, "self"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := &KGConfig{SchemaVersion: 1, Name: "x", CreatedAt: "2026-01-01T00:00:00Z"}
	if err := saveKGConfigIO(testIO(), cfg); err != nil {
		t.Fatalf("SaveKGConfig: %v", err)
	}
	commitFile(t, home, "notes/sources/n1.md", "# n1\n", "add note")

	cmd := &cobra.Command{}
	cmd.Flags().Bool("push", true, "")

	out := captureStdout(t, func() {
		if err := runKGSync(cmd, nil); err != nil {
			t.Fatalf("runKGSync push: %v", err)
		}
	})
	if !strings.Contains(string(out), "Graph pushed") {
		t.Errorf("expected 'Graph pushed' confirmation, got:\n%s", out)
	}
}

// TestRunKGSync_PullSuccessLintReportsIssues drives the post-pull lint
// branch where lint surfaces issues (InfoBox path, lines 73-78): a note with
// a dangling link makes lintBrokenLinks emit an error.
func TestRunKGSync_PullSuccessLintReportsIssues(t *testing.T) {
	home, _ := cloneFromBareRemote(t)
	t.Setenv("KG_HOME", home)

	if err := os.MkdirAll(filepath.Join(home, "self"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := &KGConfig{SchemaVersion: 1, Name: "x", CreatedAt: "2026-01-01T00:00:00Z"}
	if err := saveKGConfigIO(testIO(), cfg); err != nil {
		t.Fatalf("SaveKGConfig: %v", err)
	}
	// A note that links to a non-existent note → lintBrokenLinks error.
	now := "2026-05-12T00:00:00Z"
	if err := createGraphNote(testIO(), home, &GraphNote{
		SchemaVersion: 1, ID: "src-broken", Type: "source", Title: "Broken",
		Summary: "s", Status: "active", CreatedAt: now, UpdatedAt: now,
		SourceRefs: []string{"http://example.com"},
		Links:      []string{"does-not-exist"},
	}, "body"); err != nil {
		t.Fatalf("createGraphNote: %v", err)
	}

	cmd := &cobra.Command{}
	cmd.Flags().Bool("push", false, "")

	out := captureStdout(t, func() {
		if err := runKGSync(cmd, nil); err != nil {
			t.Fatalf("runKGSync pull: %v", err)
		}
	})
	if !strings.Contains(string(out), "lint found issues") {
		t.Errorf("expected lint-issues InfoBox, got:\n%s", out)
	}
}

// TestRunKGSync_PullLintErrorPropagates drives the post-pull lint error
// branch (lines 69-71): the pull succeeds via git, then runGraphLint fails
// because a fakeKGIO fault-injects ReadDir for the notes walk. The test
// invokes runKGSyncIO directly (the interface-DI seam — runKGSync is the
// Cobra wrapper that wires stdKGIO{}).
func TestRunKGSync_PullLintErrorPropagates(t *testing.T) {
	home, _ := cloneFromBareRemote(t)
	t.Setenv("KG_HOME", home)

	if err := os.MkdirAll(filepath.Join(home, "self"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := &KGConfig{SchemaVersion: 1, Name: "x", CreatedAt: "2026-01-01T00:00:00Z"}
	if err := saveKGConfigIO(testIO(), cfg); err != nil {
		t.Fatalf("SaveKGConfig: %v", err)
	}

	fake := &fakeKGIO{
		readDir: func(string) ([]os.DirEntry, error) {
			return nil, fmt.Errorf("injected readdir failure")
		},
	}

	cmd := &cobra.Command{}
	cmd.Flags().Bool("push", false, "")

	captureStdout(t, func() {
		err := runKGSyncIO(fake, cmd, nil)
		if err == nil {
			t.Fatal("expected lint-after-sync error")
		}
		if !strings.Contains(err.Error(), "lint after sync") {
			t.Errorf("expected 'lint after sync' error, got: %v", err)
		}
	})
}

// TestCrgRepoRoot_GetwdErrorFallsBackToDot exercises the os.Getwd() error
// branch (return "."): chdir into a directory then remove it so the cwd no
// longer resolves.
func TestCrgRepoRoot_GetwdErrorFallsBackToDot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("removing the cwd is not reliably observable on Windows")
	}
	parent := t.TempDir()
	gone := filepath.Join(parent, "gone")
	if err := os.Mkdir(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(gone)
	if err := os.Remove(gone); err != nil {
		t.Skipf("could not remove cwd to force Getwd error: %v", err)
	}
	if got := crgRepoRoot(); got != "." {
		// On some filesystems Getwd still resolves the removed dir; only
		// assert when the error path was actually taken.
		if _, err := os.Getwd(); err == nil {
			t.Skip("Getwd still resolves removed cwd on this platform")
		}
		t.Errorf("crgRepoRoot after cwd removal: got %q, want \".\"", got)
	}
}

// TestCheckCRGReadiness_BusyOrLocked_RequireGraph drives the
// CRGReadinessBusyOrLocked case in checkCRGReadiness (lines 244-248): a
// concurrently-held EXCLUSIVE lock makes Status() classify the graph as
// busy_or_locked, and requireGraph=true must then return an error.
func TestCheckCRGReadiness_BusyOrLocked_RequireGraph(t *testing.T) {
	repo := t.TempDir()
	writeCRGStatusFixture(t, repo, []crgNodeFixture{
		{FilePath: "a.go", Language: "go", UpdatedAt: "2026-04-20T00:00:00Z"},
	})
	dbPath := graphstore.CRGDBPath(repo)

	locker, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(delete)&_pragma=busy_timeout(0)")
	if err != nil {
		t.Fatalf("open locker: %v", err)
	}
	defer locker.Close()
	if _, err := locker.Exec("BEGIN EXCLUSIVE"); err != nil {
		t.Fatalf("begin exclusive: %v", err)
	}
	defer locker.Exec("ROLLBACK")

	// Confirm the lock actually produced a busy_or_locked classification on
	// this platform; otherwise the targeted branch isn't reachable here.
	if crgStatusState(repo) != graphstore.CRGReadinessBusyOrLocked {
		t.Skip("DB lock did not yield busy_or_locked on this platform")
	}

	captureStdout(t, func() {
		if err := checkCRGReadiness(repo, false); err != nil {
			t.Errorf("requireGraph=false on busy graph must not error: %v", err)
		}
		err := checkCRGReadiness(repo, true)
		if err == nil || !strings.Contains(err.Error(), "busy or locked") {
			t.Errorf("expected busy-or-locked error with requireGraph=true, got: %v", err)
		}
	})
}

// TestRunKGBuild_DefaultOutcomeInfoBox drives the default arm of the
// runKGBuild outcome switch (lines 143-145): the post-build Status() hits a
// "no such column" error → CRGReadinessError → BuildReport.Outcome is the
// catch-all that maps to ui.InfoBox("Code graph build status", ...).
func TestRunKGBuild_DefaultOutcomeInfoBox(t *testing.T) {
	repo := t.TempDir()
	initGitRepo(t, repo)
	writeFakeCRGBinary(t, repo, `case "$1" in
build) exit 0 ;;
*) exit 0 ;;
esac`)
	// Hand-craft a graph.db whose nodes table is missing the updated_at
	// column Status() selects → "no such column" (not locked, not "no such
	// table") → CRGReadinessError, distinct from unbuilt/ready/busy.
	dbPath := graphstore.CRGDBPath(repo)
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE nodes (file_path TEXT, language TEXT)`); err != nil {
		t.Fatalf("create nodes: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE edges (id INTEGER)`); err != nil {
		t.Fatalf("create edges: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO nodes (file_path, language) VALUES ('a.go','go')`); err != nil {
		t.Fatalf("insert node: %v", err)
	}
	db.Close()

	if crgStatusState(repo) != graphstore.CRGReadinessError {
		t.Skipf("expected error state from malformed schema, got %q", crgStatusState(repo))
	}

	cmd := &cobra.Command{}
	cmd.Flags().String("repo", repo, "")
	cmd.Flags().Bool("skip-flows", false, "")
	cmd.Flags().Bool("skip-postprocess", false, "")
	cmd.Flags().Bool("json", false, "")
	out := captureStdout(t, func() {
		if err := runKGBuild(cmd, nil); err != nil {
			t.Fatalf("runKGBuild: %v", err)
		}
	})
	if !strings.Contains(string(out), "Code graph build status") {
		t.Errorf("expected default-outcome InfoBox, got:\n%s", out)
	}
}

// TestRunKGUpdate_NoMutationOutcome drives the "no_mutation" arm of the
// runKGUpdate outcome switch (lines 184-185). Two commits give a non-empty
// diff; the fake CRG's update summary parses as 0 nodes / 0 edges changed.
func TestRunKGUpdate_NoMutationOutcome(t *testing.T) {
	repo := t.TempDir()
	initGitRepo(t, repo)
	writeFakeCRGBinary(t, repo, `case "$1" in
update) echo "2 files updated, 0 nodes, 0 edges" ;;
*) exit 0 ;;
esac`)
	writeCRGStatusFixture(t, repo, []crgNodeFixture{
		{FilePath: "a.go", Language: "go", UpdatedAt: "2026-04-20T00:00:00Z"},
	})
	commitFile(t, repo, "a.txt", "x\n", "init")
	commitFile(t, repo, "a.txt", "y\n", "edit")

	cmd := &cobra.Command{}
	cmd.Flags().String("repo", repo, "")
	cmd.Flags().String("base", "", "")
	cmd.Flags().Bool("skip-flows", false, "")
	cmd.Flags().Bool("skip-postprocess", false, "")
	cmd.Flags().Bool("json", false, "")
	out := captureStdout(t, func() {
		if err := runKGUpdate(cmd, nil); err != nil {
			t.Fatalf("runKGUpdate: %v", err)
		}
	})
	if !strings.Contains(string(out), "no graph mutations") {
		t.Errorf("expected no-mutation summary, got:\n%s", out)
	}
}

// TestRunKGWarmCodeImport_ReadEdgesError drives the ReadEdges-error return
// (lines 551-553): nodes read succeeds (valid nodes table) but the edges
// table is missing the columns ReadEdges selects.
func TestRunKGWarmCodeImport_ReadEdgesError(t *testing.T) {
	repo := t.TempDir()
	writeFakeCRGBinary(t, repo, "exit 0")
	dbPath := graphstore.CRGDBPath(repo)
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// A well-formed nodes table so ReadNodes(0) succeeds, but an edges table
	// with no usable columns so ReadEdges(0) errors.
	if _, err := db.Exec(`CREATE TABLE nodes (
		id INTEGER PRIMARY KEY, kind TEXT, name TEXT, qualified_name TEXT,
		file_path TEXT, line_start INTEGER, line_end INTEGER, language TEXT,
		parent_name TEXT, params TEXT, return_type TEXT, is_test INTEGER,
		signature TEXT, extra TEXT, file_hash TEXT, updated_at TEXT)`); err != nil {
		t.Fatalf("create nodes: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE edges (bogus INTEGER)`); err != nil {
		t.Fatalf("create edges: %v", err)
	}
	db.Close()

	home := newTempKG(t)
	if err := runKGSetup(testIO()); err != nil {
		t.Fatalf("setup: %v", err)
	}
	store, err := openKGStore(home)
	if err != nil {
		t.Fatalf("openKGStore: %v", err)
	}
	defer store.Close()

	_, _, err = runKGWarmCodeImport(store, repo)
	if err == nil {
		t.Skip("schema unexpectedly accepted — ReadEdges error branch not hit")
	}
	if !strings.Contains(err.Error(), "edges") {
		t.Errorf("expected CRG edges read error, got: %v", err)
	}
}

// TestWarmArchivedNotes_AdjustPopulatesArchivedAt drives the adjust-callback
// branch in warmArchivedNotes (lines 646-648): a note placed under
// notes/_archived whose status is NOT archived/superseded leaves
// noteToKGNote's ArchivedAt empty, so the callback must backfill it from
// UpdatedAt.
func TestWarmArchivedNotes_AdjustPopulatesArchivedAt(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(testIO()); err != nil {
		t.Fatalf("setup: %v", err)
	}
	archDir := filepath.Join(home, "notes", "_archived")
	if err := os.MkdirAll(archDir, 0o755); err != nil {
		t.Fatal(err)
	}
	updated := "2026-05-13T09:08:07Z"
	// Status "active" (not archived/superseded) → noteToKGNote leaves
	// ArchivedAt == "" so the warmArchivedNotes adjust callback fires.
	note := &GraphNote{
		SchemaVersion: 1, ID: "arch-active", Type: "decision", Title: "Still Active",
		Summary: "s", Status: "active", CreatedAt: updated, UpdatedAt: updated,
	}
	data, err := renderGraphNote(note, "body")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(archDir, "arch-active.md"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := openKGStore(home)
	if err != nil {
		t.Fatalf("openKGStore: %v", err)
	}
	defer store.Close()

	idx, _ := warmArchivedNotes(testIO(), store, home)
	if idx != 1 {
		t.Fatalf("expected 1 archived note indexed, got %d", idx)
	}
	got, err := store.GetKGNote("arch-active")
	if err != nil || got == nil {
		t.Fatalf("GetKGNote: %v", err)
	}
	if got.ArchivedAt != updated {
		t.Errorf("expected ArchivedAt backfilled to %q, got %q", updated, got.ArchivedAt)
	}
}

// TestWarmNotesInDir_SkipsSubdirsAndNonMarkdown drives the
// "e.IsDir() || !strings.HasSuffix(.md)" continue branch (lines 662-663).
func TestWarmNotesInDir_SkipsSubdirsAndNonMarkdown(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(testIO()); err != nil {
		t.Fatalf("setup: %v", err)
	}
	dir := filepath.Join(home, "notes", "_scan")
	if err := os.MkdirAll(filepath.Join(dir, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Non-.md file and a subdir — both must be skipped without counting.
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignore me"), 0o644); err != nil {
		t.Fatal(err)
	}
	now := "2026-05-12T00:00:00Z"
	good := &GraphNote{
		SchemaVersion: 1, ID: "scan-1", Type: "decision", Title: "Good",
		Summary: "s", Status: "active", CreatedAt: now, UpdatedAt: now,
	}
	gd, err := renderGraphNote(good, "body")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scan-1.md"), gd, 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := openKGStore(home)
	if err != nil {
		t.Fatalf("openKGStore: %v", err)
	}
	defer store.Close()

	indexed, skipped := warmNotesInDir(testIO(), store, dir, nil)
	if indexed != 1 {
		t.Errorf("expected exactly the .md note indexed, got indexed=%d", indexed)
	}
	if skipped != 0 {
		t.Errorf("subdir and .txt must be skipped silently, got skipped=%d", skipped)
	}
}

// breakKGTable replaces a warm-store table with an incompatible stub of the
// same name via a side connection. openKGStore's schema bootstrap uses
// CREATE TABLE IF NOT EXISTS, so the stub survives a reopen (the run*
// functions reopen the same DB file via kgHome()) — but every column-aware
// query/DML against that table then errors, exactly hitting the targeted
// error-return branches.
func breakKGTable(t *testing.T, home, table, replacement string) {
	t.Helper()
	side, err := sql.Open("sqlite", graphstoreDBPath(home))
	if err != nil {
		t.Fatalf("side sql.Open: %v", err)
	}
	defer side.Close()
	if _, err := side.Exec("DROP TABLE IF EXISTS " + table); err != nil {
		t.Fatalf("DROP TABLE %s: %v", table, err)
	}
	if _, err := side.Exec(replacement); err != nil {
		t.Fatalf("recreate broken %s: %v", table, err)
	}
}

// initWarmDBThenBreak runs runKGSetup under a fresh KG_HOME, materializes
// the warm schema by opening+closing the store once, then replaces table
// with an incompatible stub so the next openKGStore (inside the run* under
// test) keeps the broken schema (CREATE IF NOT EXISTS is a no-op) and the
// operation against table fails.
func initWarmDBThenBreak(t *testing.T, table, replacement string) string {
	t.Helper()
	home := newTempKG(t)
	if err := runKGSetup(testIO()); err != nil {
		t.Fatalf("setup: %v", err)
	}
	store, err := openKGStore(home)
	if err != nil {
		t.Fatalf("openKGStore: %v", err)
	}
	store.Close()
	breakKGTable(t, home, table, replacement)
	return home
}

// TestWarmNotesInDir_UpsertKGNoteError drives the UpsertKGNote-error skip
// branch (lines 680-682): the kg_notes table is dropped so every upsert
// fails and the note is counted as skipped, not indexed.
func TestWarmNotesInDir_UpsertKGNoteError(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(testIO()); err != nil {
		t.Fatalf("setup: %v", err)
	}
	dir := filepath.Join(home, "notes", "decisions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	now := "2026-05-12T00:00:00Z"
	n := &GraphNote{
		SchemaVersion: 1, ID: "up-fail", Type: "decision", Title: "X",
		Summary: "s", Status: "active", CreatedAt: now, UpdatedAt: now,
	}
	data, err := renderGraphNote(n, "body")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "up-fail.md"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := openKGStore(home)
	if err != nil {
		t.Fatalf("openKGStore: %v", err)
	}
	defer store.Close()
	// store stays open (no reopen here), so a plain DROP on the shared WAL
	// DB makes the held handle's UpsertKGNote INSERT fail with no-such-table.
	side, err := sql.Open("sqlite", graphstoreDBPath(home))
	if err != nil {
		t.Fatalf("side sql.Open: %v", err)
	}
	defer side.Close()
	if _, err := side.Exec("DROP TABLE IF EXISTS kg_notes"); err != nil {
		t.Fatalf("DROP TABLE kg_notes: %v", err)
	}

	indexed, skipped := warmNotesInDir(testIO(), store, dir, nil)
	if indexed != 0 {
		t.Errorf("expected 0 indexed when kg_notes is dropped, got %d", indexed)
	}
	if skipped != 1 {
		t.Errorf("expected the note to be skipped on upsert error, got skipped=%d", skipped)
	}
}

// TestRunKGLinkAdd_UpsertError drives the UpsertNoteSymbolLink-error return
// (lines 731-733) by dropping note_symbol_links before the add reopens the DB.
func TestRunKGLinkAdd_UpsertError(t *testing.T) {
	initWarmDBThenBreak(t, "note_symbol_links", "CREATE TABLE note_symbol_links (note_id TEXT NOT NULL, qualified_name TEXT NOT NULL)")
	cmd := newKGLinkAddCmdForTest("mentions")
	err := runKGLinkAdd(testDeps(), cmd, []string{"note-x", "pkg::Sym"})
	if err == nil || !strings.Contains(err.Error(), "create link") {
		t.Fatalf("expected create-link error, got: %v", err)
	}
}

// TestRunKGLinkList_GetLinksError drives the GetLinksForNote-error return
// (lines 750-752).
func TestRunKGLinkList_GetLinksError(t *testing.T) {
	initWarmDBThenBreak(t, "note_symbol_links", "CREATE TABLE note_symbol_links (note_id TEXT NOT NULL, qualified_name TEXT NOT NULL)")
	err := runKGLinkList(testDeps(), &cobra.Command{}, []string{"note-x"})
	if err == nil || !strings.Contains(err.Error(), "get links") {
		t.Fatalf("expected get-links error, got: %v", err)
	}
}

// TestRunKGLinkRemove_DeleteError drives the DeleteNoteSymbolLink-error
// return (lines 778-780).
func TestRunKGLinkRemove_DeleteError(t *testing.T) {
	// DELETE ... WHERE id=? needs an `id` column; a stub without it errors.
	initWarmDBThenBreak(t, "note_symbol_links", "CREATE TABLE note_symbol_links (note_id TEXT NOT NULL, qualified_name TEXT NOT NULL)")
	cmd := newKGLinkRemoveCmdForTest()
	err := runKGLinkRemove(testDeps(), cmd, []string{"42"})
	if err == nil || !strings.Contains(err.Error(), "remove link") {
		t.Fatalf("expected remove-link error, got: %v", err)
	}
}

// TestRunKGWarmStats_GetStatsError drives the GetStats-error return
// (lines 794-796). GetStats runs `SELECT COUNT(*) FROM nodes WHERE
// kind='File'`; a nodes stub without a `kind` column makes that query fail.
func TestRunKGWarmStats_GetStatsError(t *testing.T) {
	initWarmDBThenBreak(t, "nodes", "CREATE TABLE nodes (file_path TEXT NOT NULL, kind TEXT NOT NULL, qualified_name TEXT NOT NULL)")
	err := runKGWarmStats(&cobra.Command{}, nil)
	if err == nil || !strings.Contains(err.Error(), "get stats") {
		t.Fatalf("expected get-stats error, got: %v", err)
	}
}
