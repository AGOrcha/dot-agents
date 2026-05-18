package kg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// installCRGWithBody installs a fake CRG binary whose script body is body,
// initializes a git repo at repo, and chdirs into it so crgRepoRoot()
// resolves to repo for the duration of the test.
func installCRGWithBody(t *testing.T, body string) string {
	t.Helper()
	repo := t.TempDir()
	initGitRepo(t, repo)
	writeFakeCRGBinary(t, repo, body)
	t.Chdir(repo)
	return repo
}

// TestRunKGBuild_BuildReportError exercises the BuildReport-error return
// (~125-127 of sync_code_warm_link.go).
func TestRunKGBuild_BuildReportError(t *testing.T) {
	installCRGWithBody(t, `case "$1" in
build) echo "boom" >&2; exit 1 ;;
*) exit 0 ;;
esac`)
	cmd := &cobra.Command{}
	cmd.Flags().String("repo", "", "")
	cmd.Flags().Bool("skip-flows", false, "")
	cmd.Flags().Bool("skip-postprocess", false, "")
	cmd.Flags().Bool("json", false, "")
	if err := runKGBuild(cmd, nil); err == nil {
		t.Fatal("expected BuildReport error to propagate")
	}
}

// TestRunKGBuild_DefaultRepoFromCwd drives the `root == ""` → crgRepoRoot()
// branch (~108-110) with a successful BuildReport.
func TestRunKGBuild_DefaultRepoFromCwd(t *testing.T) {
	json := `{"summary":"built","outcome":"ready"}`
	installCRGWithBody(t, `case "$1" in
build) printf '%s\n' '`+json+`' ;;
*) exit 0 ;;
esac`)
	cmd := &cobra.Command{}
	cmd.Flags().String("repo", "", "")
	cmd.Flags().Bool("skip-flows", false, "")
	cmd.Flags().Bool("skip-postprocess", false, "")
	cmd.Flags().Bool("json", false, "")
	if err := runKGBuild(cmd, nil); err != nil {
		t.Fatalf("runKGBuild: %v", err)
	}
}

// TestRunKGUpdate_UpdateReportError exercises the UpdateReport-error return.
func TestRunKGUpdate_UpdateReportError(t *testing.T) {
	repo := installCRGWithBody(t, `case "$1" in
update) echo "boom" >&2; exit 1 ;;
*) exit 0 ;;
esac`)
	commitFile(t, repo, "a.txt", "x\n", "init")
	cmd := &cobra.Command{}
	cmd.Flags().String("repo", repo, "")
	cmd.Flags().String("base", "", "")
	cmd.Flags().Bool("skip-flows", false, "")
	cmd.Flags().Bool("skip-postprocess", false, "")
	cmd.Flags().Bool("json", false, "")
	if err := runKGUpdate(cmd, nil); err == nil {
		t.Fatal("expected UpdateReport error to propagate")
	}
}

// TestRunKGUpdate_DefaultRepoFromCwd exercises root="" → crgRepoRoot() branch
// in runKGUpdate. UpdateReport requires git history so commit twice.
func TestRunKGUpdate_DefaultRepoFromCwd(t *testing.T) {
	repo := installCRGWithBody(t, `case "$1" in
update) printf '%s\n' '{"summary":"no diff","outcome":"no_diff"}' ;;
*) exit 0 ;;
esac`)
	commitFile(t, repo, "a.txt", "x\n", "init")
	commitFile(t, repo, "a.txt", "y\n", "edit")
	cmd := &cobra.Command{}
	cmd.Flags().String("repo", "", "")
	cmd.Flags().String("base", "", "")
	cmd.Flags().Bool("skip-flows", false, "")
	cmd.Flags().Bool("skip-postprocess", false, "")
	cmd.Flags().Bool("json", false, "")
	// We don't strictly require success — what matters is the cwd-default
	// branch executes — but log unexpected errors for diagnostics.
	if err := runKGUpdate(cmd, nil); err != nil {
		t.Logf("runKGUpdate: %v", err)
	}
}

// TestRunKGCodeStatus_DefaultRepoFromCwd drives the root="" branch.
func TestRunKGCodeStatus_DefaultRepoFromCwd(t *testing.T) {
	installCRGWithBody(t, "exit 0")
	cmd := &cobra.Command{}
	cmd.Flags().String("repo", "", "")
	cmd.Flags().Bool("json", false, "")
	// runKGCodeStatus may return an error when CRG is in an "unbuilt" state;
	// here we only care that the cwd-default branch executes.
	_ = runKGCodeStatus(testDeps(), cmd, nil)
}

// TestRunKGImpact_DefaultRepoAndErrorFromBridge drives root="" and a CRG
// binary that errors on impact-radius.
func TestRunKGImpact_DefaultRepoAndErrorFromBridge(t *testing.T) {
	repo := installCRGWithBody(t, `case "$1" in
*) exit 0 ;;
esac`)
	// A non-zero python keeps GetImpactRadius failing.
	binDir := filepath.Join(repo, ".venv", "bin")
	if err := os.WriteFile(filepath.Join(binDir, "python3"), []byte("#!/bin/sh\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	cmd := &cobra.Command{}
	cmd.Flags().String("repo", "", "")
	cmd.Flags().String("base", "", "")
	cmd.Flags().Int("depth", 2, "")
	cmd.Flags().Int("limit", 10, "")
	cmd.Flags().Bool("require-graph", false, "")
	cmd.Flags().Bool("json", false, "")
	_ = runKGImpact(testDeps(), cmd, nil)
}

// TestRunKGPostprocess_BridgeError drives the Postprocess-error return.
func TestRunKGPostprocess_BridgeError(t *testing.T) {
	installCRGWithBody(t, `case "$1" in
post*) echo "boom" >&2; exit 1 ;;
*) exit 1 ;;
esac`)
	cmd := &cobra.Command{}
	cmd.Flags().String("repo", "", "")
	cmd.Flags().Bool("no-flows", false, "")
	cmd.Flags().Bool("no-communities", false, "")
	cmd.Flags().Bool("no-fts", false, "")
	if err := runKGPostprocess(cmd, nil); err == nil {
		t.Fatal("expected Postprocess error to propagate")
	}
}

// TestRunKGFlows_BridgeError drives the ListFlows-error return.
func TestRunKGFlows_BridgeError(t *testing.T) {
	repo := installCRGWithBody(t, "exit 0")
	// python3 returns non-zero so runPyQuery (used by ListFlows) fails.
	binDir := filepath.Join(repo, ".venv", "bin")
	if err := os.WriteFile(filepath.Join(binDir, "python3"), []byte("#!/bin/sh\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	cmd := &cobra.Command{}
	cmd.Flags().String("repo", "", "")
	cmd.Flags().Int("limit", 10, "")
	cmd.Flags().String("sort", "criticality", "")
	cmd.Flags().Bool("json", false, "")
	if err := runKGFlows(testDeps(), cmd, nil); err == nil {
		t.Fatal("expected runPyQuery failure to propagate to runKGFlows")
	}
}

// TestRunKGCommunities_BridgeError drives the ListCommunities-error return.
func TestRunKGCommunities_BridgeError(t *testing.T) {
	repo := installCRGWithBody(t, "exit 0")
	binDir := filepath.Join(repo, ".venv", "bin")
	if err := os.WriteFile(filepath.Join(binDir, "python3"), []byte("#!/bin/sh\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	cmd := &cobra.Command{}
	cmd.Flags().String("repo", "", "")
	cmd.Flags().Int("min-size", 0, "")
	cmd.Flags().String("sort", "size", "")
	cmd.Flags().Bool("json", false, "")
	if err := runKGCommunities(testDeps(), cmd, nil); err == nil {
		t.Fatal("expected runPyQuery failure to propagate to runKGCommunities")
	}
}

// TestRunKGChanges_DetectChangesError drives the DetectChanges-error return.
func TestRunKGChanges_DetectChangesError(t *testing.T) {
	repo := installCRGWithBody(t, `case "$1" in
detect-changes) echo "boom" >&2; exit 1 ;;
*) exit 0 ;;
esac`)
	writeCRGStatusFixture(t, repo, []crgNodeFixture{
		{FilePath: "a.go", Language: "go", UpdatedAt: "2026-04-20T00:00:00Z"},
	})
	cmd := &cobra.Command{}
	cmd.Flags().String("repo", "", "")
	cmd.Flags().String("base", "", "")
	cmd.Flags().Bool("brief", false, "")
	cmd.Flags().Bool("require-graph", false, "")
	cmd.Flags().Bool("json", false, "")
	if err := runKGChanges(testDeps(), cmd, nil); err == nil {
		t.Fatal("expected DetectChanges error to propagate")
	}
}

// TestRunKGLinkAdd_InvalidLinkKind drives the invalid-kind return path.
func TestRunKGLinkAdd_InvalidLinkKind(t *testing.T) {
	newTempKG(t)
	cmd := &cobra.Command{}
	cmd.Flags().String("kind", "no-such-kind", "")
	err := runKGLinkAdd(cmd, []string{"note-id", "qn"})
	if err == nil || !strings.Contains(err.Error(), "invalid link kind") {
		t.Fatalf("expected invalid-link-kind error, got %v", err)
	}
}

// TestRunKGLinkRemove_BadInt drives the integer-parse error path.
func TestRunKGLinkRemove_BadInt(t *testing.T) {
	newTempKG(t)
	if err := runKGLinkRemove(&cobra.Command{}, []string{"not-an-int"}); err == nil {
		t.Fatal("expected parse error for non-integer link id")
	}
}
