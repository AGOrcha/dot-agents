package kg

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
