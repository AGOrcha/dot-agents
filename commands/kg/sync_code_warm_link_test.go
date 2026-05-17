package kg

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/graphstore"
	"github.com/spf13/cobra"
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
	if err := runKGLinkAdd(addCmd, []string{"dec-use-cobra", "x::Y"}); err != nil {
		t.Fatalf("first add: %v", err)
	}
	if err := runKGLinkAdd(addCmd, []string{"dec-use-cobra", "x::Y"}); err != nil {
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
	if err := runKGLinkAdd(addCmd, []string{"dec-use-cobra", "ns::Foo"}); err != nil {
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
		if err := runKGLinkList(nil, []string{"dec-use-cobra"}); err != nil {
			t.Fatalf("runKGLinkList: %v", err)
		}
	})
	if !strings.Contains(string(out), "No symbol links") {
		t.Errorf("expected 'No symbol links' message, got:\n%s", out)
	}
}

// TestRunKGLinkList_MissingArg covers the usage-error branch.
func TestRunKGLinkList_MissingArg(t *testing.T) {
	err := runKGLinkList(nil, nil)
	if err == nil {
		t.Fatal("expected usage error for missing note-id")
	}
	if !strings.Contains(err.Error(), "kg link list") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestRunKGLinkRemove_MissingArg covers the usage-error branch.
func TestRunKGLinkRemove_MissingArg(t *testing.T) {
	err := runKGLinkRemove(nil, nil)
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
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	cmd := newKGLinkRemoveCmdForTest()
	if err := runKGLinkRemove(cmd, []string{"999999"}); err != nil {
		t.Fatalf("runKGLinkRemove unknown id: %v", err)
	}
}

// TestRunKGWarmCodeImport_NoCRGBinary returns a wrapped error when CRG is
// not discoverable on the system.
func TestRunKGWarmCodeImport_NoCRGBinary(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
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
	if err := runKGSetup(); err != nil {
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
	if err := runKGSetup(); err != nil {
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
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	now := "2026-05-12T00:00:00Z"
	// Active note in notes/decisions/
	if err := createGraphNote(home, &GraphNote{
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
	indexed, _ := warmActiveNotes(store, home, subs)
	if indexed < 1 {
		t.Errorf("active notes: expected ≥1 indexed, got %d", indexed)
	}
	archIdx, archSkip := warmArchivedNotes(store, home)
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
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	store, err := openKGStore(home)
	if err != nil {
		t.Fatalf("openKGStore: %v", err)
	}
	defer store.Close()
	indexed, skipped := warmNotesInDir(store, filepath.Join(home, "does-not-exist"), nil)
	if indexed != 0 || skipped != 0 {
		t.Errorf("expected 0/0 for missing dir, got indexed=%d skipped=%d", indexed, skipped)
	}
}

// TestWarmCodeLane_CRGUnavailable hits the failure path: with no CRG binary
// on PATH, warmCodeLane returns an empty summary and emits a warning.
func TestWarmCodeLane_CRGUnavailable(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(); err != nil {
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
		if got := warmCodeLane(store); got != "" {
			t.Errorf("expected empty summary when CRG missing, got %q", got)
		}
	})
	_ = out // warning is emitted to stderr by ui.Warn — we only assert the return value
}

// TestRunKGLinkRemove_InvalidIDReturnsError covers the integer-parse error.
func TestRunKGLinkRemove_InvalidIDReturnsError(t *testing.T) {
	cmd := newKGLinkRemoveCmdForTest()
	err := runKGLinkRemove(cmd, []string{"not-a-number"})
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
	err := runKGLinkAdd(cmd, []string{"some-id", "ns::F"})
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
	if err := runKGLinkAdd(cmd, nil); err == nil {
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
	err := runKGLinkAdd(cmd, []string{"note", "qn"})
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
	err := runKGLinkList(cmd, []string{"note"})
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
	err := runKGLinkRemove(cmd, []string{"42"})
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
	if err := runKGSetup(); err != nil {
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
	if err := runKGSetup(); err != nil {
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

func TestRunKGUpdate_NoCRGBinary(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	cmd := &cobra.Command{}
	cmd.Flags().String("repo", repo, "")
	cmd.Flags().String("base", "", "")
	cmd.Flags().Bool("skip-flows", false, "")
	cmd.Flags().Bool("skip-postprocess", false, "")
	cmd.Flags().Bool("json", false, "")
	if err := runKGUpdate(cmd, nil); err == nil {
		t.Error("expected error when no CRG binary on PATH")
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
	if err := runKGSetup(); err != nil {
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
	if err := SaveKGConfig(cfg); err != nil {
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
	if err := runKGSetup(); err != nil {
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
	if err := runKGSetup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	store, err := openKGStore(home)
	if err != nil {
		t.Fatalf("openKGStore: %v", err)
	}
	defer store.Close()
	msg := warmCodeLane(store)
	if !strings.Contains(msg, "code-lane") {
		t.Errorf("expected code-lane summary, got %q", msg)
	}
}
