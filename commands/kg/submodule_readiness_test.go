package kg

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/graphstore"
	"github.com/spf13/cobra"
	"golang.org/x/sys/execabs"
)

// superprojectWithSubmodule builds a real superproject containing a real
// `git submodule add`ed child and returns its path. The command-level
// behaviour under test is entirely about what git reports for a gitlink, so
// the fixture is a genuine one.
func superprojectWithSubmodule(t *testing.T) string {
	t.Helper()
	if _, err := execabs.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	base := t.TempDir()
	sub := filepath.Join(base, "lib-origin")
	initGitRepo(t, sub)
	commitFile(t, sub, "lib.go", "package lib\n", "lib")
	super := filepath.Join(base, "super")
	initGitRepo(t, super)
	commitFile(t, super, "main.go", "package main\n", "main")
	runGitFixture(t, super, "-c", "protocol.file.allow=always", "submodule", "add", "--quiet",
		filepath.ToSlash(sub), "vendor/lib")
	runGitFixture(t, super, "commit", "--quiet", "-m", "add submodule")
	return super
}

// clonedSuperprojectWithUninitializedSubmodule returns a clone of the
// superproject fixture: the gitlink is present but its working tree was never
// populated, which is what `git clone` (and `git worktree add`) leave behind.
func clonedSuperprojectWithUninitializedSubmodule(t *testing.T) string {
	t.Helper()
	super := superprojectWithSubmodule(t)
	base := t.TempDir()
	clone := filepath.Join(base, "clone")
	runGitFixture(t, base, "clone", "--quiet", filepath.ToSlash(super), clone)
	return clone
}

// runGitFixture runs a git command in dir, failing the test on error.
func runGitFixture(t *testing.T, dir string, args ...string) {
	t.Helper()
	out, err := execabs.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// TestRunKGCodeStatus_NamesUnindexedSubmodule is the operator-facing half of
// the fix: a graph built over the superproject alone used to print READY with
// no hint that a whole repository was missing. It now reports the state as
// incomplete and names the root that is absent.
func TestRunKGCodeStatus_NamesUnindexedSubmodule(t *testing.T) {
	repo := superprojectWithSubmodule(t)
	writeCRGStatusFixture(t, repo, []crgNodeFixture{
		{FilePath: filepath.Join(repo, "main.go"), Language: "go", UpdatedAt: "2026-04-19T18:03:45Z"},
	})

	cmd := &cobra.Command{}
	cmd.Flags().String("repo", repo, "")
	cmd.Flags().Bool("json", false, "")

	out := captureStdout(t, func() {
		if err := runKGCodeStatus(Deps{}, cmd, nil); err != nil {
			t.Fatalf("runKGCodeStatus: %v", err)
		}
	})
	text := string(out)
	for _, want := range []string{"INCOMPLETE", "vendor/lib", "NOT INDEXED"} {
		if !strings.Contains(text, want) {
			t.Errorf("code-status output must contain %q, got:\n%s", want, text)
		}
	}
	if strings.Contains(text, "[READY]") {
		t.Errorf("a graph missing a whole repository must not print READY:\n%s", text)
	}
}

// TestRunKGCodeStatus_JSONCarriesRoots pins the machine-readable half: the
// per-root breakdown is in the JSON payload consumers read.
func TestRunKGCodeStatus_JSONCarriesRoots(t *testing.T) {
	repo := superprojectWithSubmodule(t)
	writeCRGStatusFixture(t, repo, []crgNodeFixture{
		{FilePath: filepath.Join(repo, "main.go"), Language: "go", UpdatedAt: "2026-04-19T18:03:45Z"},
	})

	cmd := &cobra.Command{}
	cmd.Flags().String("repo", repo, "")
	cmd.Flags().Bool("json", true, "")

	out := captureStdout(t, func() {
		if err := runKGCodeStatus(Deps{}, cmd, nil); err != nil {
			t.Fatalf("runKGCodeStatus: %v", err)
		}
	})
	var status graphstore.CRGStatus
	if err := json.Unmarshal(out, &status); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %s", err, out)
	}
	if status.Ready || status.State != graphstore.CRGReadinessIncomplete {
		t.Errorf("status = %+v, want an incomplete, not-ready graph", status)
	}
	if len(status.Roots) != 2 || status.Roots[1].Path != "vendor/lib" || status.Roots[1].Indexed {
		t.Errorf("roots = %+v, want vendor/lib reported as not indexed", status.Roots)
	}
}

// TestRunKGBuild_NoRecurseSubmodulesReportsExclusion drives the CLI flag end
// to end: the submodule is excluded on request, and the exclusion is stated
// rather than silently applied.
func TestRunKGBuild_NoRecurseSubmodulesReportsExclusion(t *testing.T) {
	repo := superprojectWithSubmodule(t)
	writeCRGStatusFixture(t, repo, []crgNodeFixture{
		{FilePath: filepath.Join(repo, "main.go"), Language: "go", UpdatedAt: "2026-04-19T18:03:45Z"},
	})
	writeFakeCRGBinary(t, repo, "exit 0")

	cmd := &cobra.Command{}
	cmd.Flags().String("repo", repo, "")
	cmd.Flags().Bool("skip-flows", false, "")
	cmd.Flags().Bool("skip-postprocess", false, "")
	cmd.Flags().Bool(noRecurseSubmodulesFlag, true, "")
	cmd.Flags().Bool("json", true, "")

	out := captureStdout(t, func() {
		if err := runKGBuild(cmd, nil); err != nil {
			t.Fatalf("runKGBuild: %v", err)
		}
	})
	var report graphstore.CRGOperationReport
	if err := json.Unmarshal(out, &report); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %s", err, out)
	}
	// The operator declared the scope, so the graph is ready — and the
	// exclusion is still recorded rather than silently applied.
	if report.Outcome != graphstore.CRGReadinessReady {
		t.Errorf("outcome = %q, want ready", report.Outcome)
	}
	if report.Workspace == nil || len(report.Workspace.Skipped) != 1 ||
		report.Workspace.Skipped[0].Reason != graphstore.SkipReasonExcluded {
		t.Errorf("workspace = %+v, want vendor/lib recorded as excluded", report.Workspace)
	}
}

// TestRunKGBuild_IncompleteTextOutput: the human-readable build path warns
// instead of printing a success box when a root is missing from the graph —
// here a submodule that exists in the checkout but was never initialized.
func TestRunKGBuild_IncompleteTextOutput(t *testing.T) {
	repo := clonedSuperprojectWithUninitializedSubmodule(t)
	writeCRGStatusFixture(t, repo, []crgNodeFixture{
		{FilePath: filepath.Join(repo, "main.go"), Language: "go", UpdatedAt: "2026-04-19T18:03:45Z"},
	})
	writeFakeCRGBinary(t, repo, "exit 0")

	cmd := &cobra.Command{}
	cmd.Flags().String("repo", repo, "")
	cmd.Flags().Bool("skip-flows", false, "")
	cmd.Flags().Bool("skip-postprocess", false, "")
	cmd.Flags().Bool(noRecurseSubmodulesFlag, false, "")
	cmd.Flags().Bool("json", false, "")

	out := captureStdout(t, func() {
		if err := runKGBuild(cmd, nil); err != nil {
			t.Fatalf("runKGBuild: %v", err)
		}
	})
	text := string(out)
	for _, want := range []string{"incomplete", "vendor/lib"} {
		if !strings.Contains(text, want) {
			t.Errorf("build output must contain %q, got:\n%s", want, text)
		}
	}
}

// TestBuildCommandRegistersRecurseOptOut pins the flag onto the command so the
// opt-out stays reachable from the CLI.
func TestBuildCommandRegistersRecurseOptOut(t *testing.T) {
	root := NewKGCmd(testDeps())
	build, _, err := root.Find([]string{"build"})
	if err != nil {
		t.Fatalf("find build command: %v", err)
	}
	if build.Flags().Lookup(noRecurseSubmodulesFlag) == nil {
		t.Errorf("kg build must expose --%s", noRecurseSubmodulesFlag)
	}
}

// TestCheckCRGReadiness_IncompleteBlocksRequireGraph: partial results are
// harder to notice than empty ones, so --require-graph refuses an incomplete
// graph instead of warning past it.
func TestCheckCRGReadiness_IncompleteBlocksRequireGraph(t *testing.T) {
	orig := crgBridgeStatus
	t.Cleanup(func() { crgBridgeStatus = orig })
	crgBridgeStatus = func(string) (*graphstore.CRGStatus, error) {
		return &graphstore.CRGStatus{
			State:   graphstore.CRGReadinessIncomplete,
			Message: "submodules detected but not indexed: vendor/lib",
		}, nil
	}

	out := captureStdout(t, func() {
		err := checkCRGReadiness(t.TempDir(), true)
		if err == nil || !strings.Contains(err.Error(), "vendor/lib") {
			t.Errorf("expected an error naming the missing root, got %v", err)
		}
	})
	if !strings.Contains(string(out), "incomplete") {
		t.Errorf("expected an incomplete-graph warning, got: %s", out)
	}
	captureStdout(t, func() {
		if err := checkCRGReadiness(t.TempDir(), false); err != nil {
			t.Errorf("without --require-graph an incomplete graph only warns, got %v", err)
		}
	})
}

// TestWarnIncompleteWorkspace covers the post-update notices.
func TestWarnIncompleteWorkspace(t *testing.T) {
	cases := []struct {
		name   string
		status *graphstore.CRGStatus
		want   string
	}{
		{"nil status", nil, ""},
		{
			"incomplete",
			&graphstore.CRGStatus{State: graphstore.CRGReadinessIncomplete, Message: "missing vendor/lib"},
			"missing vendor/lib",
		},
		{
			"multi-root",
			&graphstore.CRGStatus{
				State: graphstore.CRGReadinessReady,
				Roots: []graphstore.RootStatus{{Path: "."}, {Path: "vendor/lib"}},
			},
			"incremental update covers the superproject only",
		},
		{"single root", &graphstore.CRGStatus{State: graphstore.CRGReadinessReady}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := captureStdout(t, func() { warnIncompleteWorkspace(tc.status) })
			assertOutputContains(t, string(out), tc.want)
		})
	}
}

// TestPrintRootBreakdown covers the per-root rendering, indexed and not.
func TestPrintRootBreakdown(t *testing.T) {
	out := captureStdout(t, func() { printRootBreakdown(nil) })
	if strings.TrimSpace(string(out)) != "" {
		t.Errorf("no roots means no breakdown, got: %s", out)
	}
	out = captureStdout(t, func() {
		printRootBreakdown([]graphstore.RootStatus{
			{Path: ".", Nodes: 5946, Files: 885, Indexed: true},
			{Path: "vendor/lib", Note: graphstore.SkipReasonUninitialized},
		})
	})
	text := string(out)
	for _, want := range []string{"5946 nodes, 885 files", "vendor/lib", "NOT INDEXED", "git submodule update"} {
		if !strings.Contains(text, want) {
			t.Errorf("breakdown must contain %q, got:\n%s", want, text)
		}
	}
}

// assertOutputContains asserts want is present, or — when want is empty —
// that nothing was printed.
func assertOutputContains(t *testing.T, got, want string) {
	t.Helper()
	if want == "" {
		if strings.TrimSpace(got) != "" {
			t.Errorf("expected no output, got: %s", got)
		}
		return
	}
	if !strings.Contains(got, want) {
		t.Errorf("output missing %q, got: %s", want, got)
	}
}
