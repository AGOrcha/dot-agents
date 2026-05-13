// Package graphstore — coverage for CRGBridge wrappers that shell out to the
// Python code-review-graph CLI. These tests use fake shell scripts to simulate
// CRG subprocess responses; no real Python interpreter or CRG install required.
package graphstore

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// makeFakeCRGEnv writes a fake `code-review-graph` and adjacent `python3` shell
// script under a temp dir and returns (repoRoot, crgBin). The two scripts are
// independently configurable so callers can simulate either subprocess.
func makeFakeCRGEnv(t *testing.T, crgScript, pyScript string) (string, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake shell binaries are POSIX-only")
	}
	repo := t.TempDir()
	binDir := filepath.Join(repo, ".venv", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	crgPath := filepath.Join(binDir, crgBinName)
	if err := os.WriteFile(crgPath, []byte(crgScript), 0o755); err != nil {
		t.Fatal(err)
	}
	pyPath := filepath.Join(binDir, "python3")
	if err := os.WriteFile(pyPath, []byte(pyScript), 0o755); err != nil {
		t.Fatal(err)
	}
	return repo, crgPath
}

// initRepoGit initialises a minimal git repo with one tracked file and commit so
// gitChangedFiles can produce a non-empty diff after a subsequent modification.
func initRepoGit(t *testing.T, repo string) {
	t.Helper()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@x",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@x",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--quiet")
	run("config", "user.email", "t@x")
	run("config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "a.go")
	run("commit", "--quiet", "-m", "c1")
}

// ── runPyQuery ───────────────────────────────────────────────────────────────

func TestCRGBridge_runPyQuery_OK(t *testing.T) {
	// python3 script ignores its args and prints a fixed JSON document.
	pyScript := `#!/bin/sh
echo '{"k":"v"}'
`
	repo, crgBin := makeFakeCRGEnv(t, "#!/bin/sh\nexit 0\n", pyScript)
	b := &CRGBridge{RepoRoot: repo, Bin: crgBin}
	out, err := b.runPyQuery(`print('ignored')`)
	if err != nil {
		t.Fatalf("runPyQuery: %v", err)
	}
	if !strings.Contains(string(out), `"k":"v"`) {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestCRGBridge_runPyQuery_NonZeroExitPropagatesStderr(t *testing.T) {
	pyScript := `#!/bin/sh
echo 'kaboom-py' >&2
exit 9
`
	repo, crgBin := makeFakeCRGEnv(t, "#!/bin/sh\nexit 0\n", pyScript)
	b := &CRGBridge{RepoRoot: repo, Bin: crgBin}
	_, err := b.runPyQuery(`print('x')`)
	if err == nil {
		t.Fatal("expected error from non-zero exit")
	}
	if !strings.Contains(err.Error(), "crg-py") || !strings.Contains(err.Error(), "kaboom-py") {
		t.Errorf("expected stderr propagated; got %v", err)
	}
}

func TestCRGBridge_runPyQuery_SilentExitErrors(t *testing.T) {
	pyScript := `#!/bin/sh
exit 2
`
	repo, crgBin := makeFakeCRGEnv(t, "#!/bin/sh\nexit 0\n", pyScript)
	b := &CRGBridge{RepoRoot: repo, Bin: crgBin}
	_, err := b.runPyQuery(`print('x')`)
	if err == nil {
		t.Fatal("expected error from silent exit 2")
	}
}

// ── GetImpactRadius ──────────────────────────────────────────────────────────

func TestCRGBridge_GetImpactRadius_ParsesJSON(t *testing.T) {
	pyScript := `#!/bin/sh
echo '{"status":"ok","summary":"done","changed_files":["a.go"],"changed_nodes":[],"impacted_nodes":[],"impacted_files":["b.go"],"truncated":false,"total_impacted":1}'
`
	repo, crgBin := makeFakeCRGEnv(t, "#!/bin/sh\nexit 0\n", pyScript)
	b := &CRGBridge{RepoRoot: repo, Bin: crgBin}
	res, err := b.GetImpactRadius(ImpactOptions{ChangedFiles: []string{"a.go"}})
	if err != nil {
		t.Fatalf("GetImpactRadius: %v", err)
	}
	if res.Status != "ok" || res.TotalImpacted != 1 {
		t.Errorf("unexpected result: %+v", res)
	}
}

func TestCRGBridge_GetImpactRadius_DefaultDepthAndResultsAndDiff(t *testing.T) {
	// Exercise the no-ChangedFiles default branch + default depth/results.
	pyScript := `#!/bin/sh
echo '{"status":"ok","summary":"x","changed_files":[],"changed_nodes":[],"impacted_nodes":[],"impacted_files":[],"truncated":false,"total_impacted":0}'
`
	repo, crgBin := makeFakeCRGEnv(t, "#!/bin/sh\nexit 0\n", pyScript)
	b := &CRGBridge{RepoRoot: repo, Bin: crgBin}
	res, err := b.GetImpactRadius(ImpactOptions{}) // no files, zero depth
	if err != nil {
		t.Fatalf("GetImpactRadius: %v", err)
	}
	if res.Status != "ok" {
		t.Errorf("got %q", res.Status)
	}
}

func TestCRGBridge_GetImpactRadius_BadJSONErrors(t *testing.T) {
	pyScript := `#!/bin/sh
echo 'not-json'
`
	repo, crgBin := makeFakeCRGEnv(t, "#!/bin/sh\nexit 0\n", pyScript)
	b := &CRGBridge{RepoRoot: repo, Bin: crgBin}
	_, err := b.GetImpactRadius(ImpactOptions{ChangedFiles: []string{"a.go"}})
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), "parse impact") {
		t.Errorf("expected parse-impact error; got %v", err)
	}
}

func TestCRGBridge_GetImpactRadius_SubprocessError(t *testing.T) {
	pyScript := `#!/bin/sh
exit 1
`
	repo, crgBin := makeFakeCRGEnv(t, "#!/bin/sh\nexit 0\n", pyScript)
	b := &CRGBridge{RepoRoot: repo, Bin: crgBin}
	_, err := b.GetImpactRadius(ImpactOptions{ChangedFiles: []string{"a.go"}})
	if err == nil {
		t.Fatal("expected error")
	}
}

// ── ListFlows ────────────────────────────────────────────────────────────────

func TestCRGBridge_ListFlows_ParsesJSON(t *testing.T) {
	pyScript := `#!/bin/sh
echo '{"status":"ok","summary":"s","flows":[{"id":1,"name":"f","entry_point":"main","step_count":3,"criticality":0.9,"kind":"data"}]}'
`
	repo, crgBin := makeFakeCRGEnv(t, "#!/bin/sh\nexit 0\n", pyScript)
	b := &CRGBridge{RepoRoot: repo, Bin: crgBin}
	res, err := b.ListFlows(0, "")
	if err != nil {
		t.Fatalf("ListFlows: %v", err)
	}
	if len(res.Flows) != 1 || res.Flows[0].Name != "f" {
		t.Errorf("unexpected flows: %+v", res)
	}
}

func TestCRGBridge_ListFlows_BadJSONErrors(t *testing.T) {
	pyScript := `#!/bin/sh
echo 'bad'
`
	repo, crgBin := makeFakeCRGEnv(t, "#!/bin/sh\nexit 0\n", pyScript)
	b := &CRGBridge{RepoRoot: repo, Bin: crgBin}
	_, err := b.ListFlows(5, "criticality")
	if err == nil || !strings.Contains(err.Error(), "parse flows") {
		t.Errorf("expected parse-flows error; got %v", err)
	}
}

func TestCRGBridge_ListFlows_SubprocessError(t *testing.T) {
	pyScript := `#!/bin/sh
exit 1
`
	repo, crgBin := makeFakeCRGEnv(t, "#!/bin/sh\nexit 0\n", pyScript)
	b := &CRGBridge{RepoRoot: repo, Bin: crgBin}
	if _, err := b.ListFlows(10, "name"); err == nil {
		t.Error("expected subprocess error")
	}
}

// ── ListCommunities ──────────────────────────────────────────────────────────

func TestCRGBridge_ListCommunities_ParsesJSON(t *testing.T) {
	pyScript := `#!/bin/sh
echo '{"status":"ok","summary":"s","communities":[{"id":1,"name":"c","size":5,"cohesion":0.5,"dominant_language":"go","description":"d","members":["a","b"]}]}'
`
	repo, crgBin := makeFakeCRGEnv(t, "#!/bin/sh\nexit 0\n", pyScript)
	b := &CRGBridge{RepoRoot: repo, Bin: crgBin}
	res, err := b.ListCommunities(0, "")
	if err != nil {
		t.Fatalf("ListCommunities: %v", err)
	}
	if len(res.Communities) != 1 || res.Communities[0].Size != 5 {
		t.Errorf("unexpected communities: %+v", res)
	}
}

func TestCRGBridge_ListCommunities_BadJSONErrors(t *testing.T) {
	pyScript := `#!/bin/sh
echo 'bad'
`
	repo, crgBin := makeFakeCRGEnv(t, "#!/bin/sh\nexit 0\n", pyScript)
	b := &CRGBridge{RepoRoot: repo, Bin: crgBin}
	_, err := b.ListCommunities(2, "size")
	if err == nil || !strings.Contains(err.Error(), "parse communities") {
		t.Errorf("expected parse-communities error; got %v", err)
	}
}

func TestCRGBridge_ListCommunities_SubprocessError(t *testing.T) {
	pyScript := `#!/bin/sh
exit 1
`
	repo, crgBin := makeFakeCRGEnv(t, "#!/bin/sh\nexit 0\n", pyScript)
	b := &CRGBridge{RepoRoot: repo, Bin: crgBin}
	if _, err := b.ListCommunities(1, "name"); err == nil {
		t.Error("expected subprocess error")
	}
}

// ── Postprocess ──────────────────────────────────────────────────────────────

func TestCRGBridge_Postprocess_OK(t *testing.T) {
	crgScript := `#!/bin/sh
echo "args: $@"
exit 0
`
	repo, crgBin := makeFakeCRGEnv(t, crgScript, "#!/bin/sh\nexit 0\n")
	b := &CRGBridge{RepoRoot: repo, Bin: crgBin}
	if err := b.Postprocess(PostprocessOptions{NoFlows: true, NoCommunities: true, NoFTS: true}); err != nil {
		t.Errorf("Postprocess: %v", err)
	}
}

func TestCRGBridge_Postprocess_PropagatesFailure(t *testing.T) {
	crgScript := `#!/bin/sh
exit 1
`
	repo, crgBin := makeFakeCRGEnv(t, crgScript, "#!/bin/sh\nexit 0\n")
	b := &CRGBridge{RepoRoot: repo, Bin: crgBin}
	if err := b.Postprocess(PostprocessOptions{}); err == nil {
		t.Error("expected postprocess failure")
	}
}

// ── DetectChanges ────────────────────────────────────────────────────────────

func TestCRGBridge_DetectChanges_JSON(t *testing.T) {
	crgScript := `#!/bin/sh
echo '{"summary":"s","risk_score":0.5,"changed_functions":[],"affected_flows":[],"test_gaps":[],"review_priorities":[]}'
`
	repo, crgBin := makeFakeCRGEnv(t, crgScript, "#!/bin/sh\nexit 0\n")
	b := &CRGBridge{RepoRoot: repo, Bin: crgBin}
	rep, err := b.DetectChanges(DetectChangesOptions{Base: "HEAD~2"})
	if err != nil {
		t.Fatalf("DetectChanges: %v", err)
	}
	if rep.Summary != "s" || rep.RiskScore != 0.5 {
		t.Errorf("unexpected report: %+v", rep)
	}
}

func TestCRGBridge_DetectChanges_BriefSkipsParse(t *testing.T) {
	crgScript := `#!/bin/sh
echo 'plain text summary'
`
	repo, crgBin := makeFakeCRGEnv(t, crgScript, "#!/bin/sh\nexit 0\n")
	b := &CRGBridge{RepoRoot: repo, Bin: crgBin}
	rep, err := b.DetectChanges(DetectChangesOptions{Brief: true})
	if err != nil {
		t.Fatalf("DetectChanges brief: %v", err)
	}
	if rep.Summary != "plain text summary" {
		t.Errorf("expected raw text summary; got %q", rep.Summary)
	}
}

func TestCRGBridge_DetectChanges_BadJSONErrors(t *testing.T) {
	crgScript := `#!/bin/sh
echo 'not-json-at-all'
`
	repo, crgBin := makeFakeCRGEnv(t, crgScript, "#!/bin/sh\nexit 0\n")
	b := &CRGBridge{RepoRoot: repo, Bin: crgBin}
	_, err := b.DetectChanges(DetectChangesOptions{})
	if err == nil || !strings.Contains(err.Error(), "parse detect-changes") {
		t.Errorf("expected parse error; got %v", err)
	}
}

func TestCRGBridge_DetectChanges_SubprocessFailure(t *testing.T) {
	crgScript := `#!/bin/sh
echo 'oh no' >&2
exit 1
`
	repo, crgBin := makeFakeCRGEnv(t, crgScript, "#!/bin/sh\nexit 0\n")
	b := &CRGBridge{RepoRoot: repo, Bin: crgBin}
	if _, err := b.DetectChanges(DetectChangesOptions{}); err == nil {
		t.Error("expected subprocess error")
	}
}

// ── gitChangedFiles ──────────────────────────────────────────────────────────

func TestCRGBridge_gitChangedFiles_NoChangesReturnsEmpty(t *testing.T) {
	repo, crgBin := makeFakeCRGEnv(t, "#!/bin/sh\nexit 0\n", "#!/bin/sh\nexit 0\n")
	initRepoGit(t, repo)
	b := &CRGBridge{RepoRoot: repo, Bin: crgBin}
	files, err := b.gitChangedFiles("HEAD")
	if err != nil {
		t.Fatalf("gitChangedFiles: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected no files; got %v", files)
	}
}

func TestCRGBridge_gitChangedFiles_DefaultBase(t *testing.T) {
	// gitChangedFiles defaults base to HEAD~1 when empty. With one commit only,
	// HEAD~1 does not exist → expect error.
	repo, crgBin := makeFakeCRGEnv(t, "#!/bin/sh\nexit 0\n", "#!/bin/sh\nexit 0\n")
	initRepoGit(t, repo)
	b := &CRGBridge{RepoRoot: repo, Bin: crgBin}
	_, err := b.gitChangedFiles("")
	if err == nil {
		t.Error("expected error when HEAD~1 missing on fresh repo")
	}
}

func TestCRGBridge_gitChangedFiles_NotARepoErrors(t *testing.T) {
	repo, crgBin := makeFakeCRGEnv(t, "#!/bin/sh\nexit 0\n", "#!/bin/sh\nexit 0\n")
	b := &CRGBridge{RepoRoot: repo, Bin: crgBin}
	_, err := b.gitChangedFiles("HEAD")
	if err == nil {
		t.Error("expected error in non-git dir")
	}
}

// ── UpdateReport / Update ────────────────────────────────────────────────────

func TestCRGBridge_UpdateReport_NoDiffShortCircuits(t *testing.T) {
	repo, crgBin := makeFakeCRGEnv(t, "#!/bin/sh\nexit 0\n", "#!/bin/sh\nexit 0\n")
	initRepoGit(t, repo)
	b := &CRGBridge{RepoRoot: repo, Bin: crgBin}
	// HEAD base → no diff → no CRG invocation; Status() runs with missing DB.
	rep, err := b.UpdateReport(UpdateOptions{Base: "HEAD"})
	if err != nil {
		t.Fatalf("UpdateReport: %v", err)
	}
	if rep.Outcome != "no_diff" {
		t.Errorf("expected outcome=no_diff; got %q", rep.Outcome)
	}
}

func TestCRGBridge_UpdateReport_PropagatesGitError(t *testing.T) {
	repo, crgBin := makeFakeCRGEnv(t, "#!/bin/sh\nexit 0\n", "#!/bin/sh\nexit 0\n")
	// Not a git repo → gitChangedFiles errors → UpdateReport propagates.
	b := &CRGBridge{RepoRoot: repo, Bin: crgBin}
	if _, err := b.UpdateReport(UpdateOptions{Base: "HEAD~1"}); err == nil {
		t.Error("expected error from missing git repo")
	}
}

func TestCRGBridge_UpdateReport_RunsCRGOnDiff(t *testing.T) {
	// One commit + uncommitted change → diff against HEAD picks up nothing,
	// but diff against HEAD~1 errors. So use two commits: c1, c2 with diff
	// between them.
	repo, crgBin := makeFakeCRGEnv(t,
		"#!/bin/sh\necho 'updated: 1 files, 2 nodes, 3 edges'\nexit 0\n",
		"#!/bin/sh\nexit 0\n")
	initRepoGit(t, repo)
	// Add second commit so HEAD~1 exists.
	if err := os.WriteFile(filepath.Join(repo, "b.go"), []byte("package b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", "b.go")
	cmd.Dir = repo
	_ = cmd.Run()
	cmd = exec.Command("git", "commit", "--quiet", "-m", "c2")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@x",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@x",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}

	b := &CRGBridge{RepoRoot: repo, Bin: crgBin}
	rep, err := b.UpdateReport(UpdateOptions{Base: "HEAD~1", SkipFlows: true, SkipPostprocess: true})
	if err != nil {
		t.Fatalf("UpdateReport: %v", err)
	}
	if rep.Outcome == "no_diff" {
		t.Errorf("expected non-no_diff outcome; got %+v", rep)
	}
	if len(rep.ChangedFiles) == 0 {
		t.Errorf("expected changed files; got %+v", rep)
	}
}

func TestCRGBridge_UpdateReport_CRGRunFailureErrors(t *testing.T) {
	// CRG run fails → classifyCRGRunError wraps with "crg update failed".
	repo, crgBin := makeFakeCRGEnv(t,
		"#!/bin/sh\necho 'boom' >&2\nexit 1\n",
		"#!/bin/sh\nexit 0\n")
	initRepoGit(t, repo)
	if err := os.WriteFile(filepath.Join(repo, "b.go"), []byte("package b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", "b.go")
	cmd.Dir = repo
	_ = cmd.Run()
	cmd = exec.Command("git", "commit", "--quiet", "-m", "c2")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@x",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@x",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}

	b := &CRGBridge{RepoRoot: repo, Bin: crgBin}
	if _, err := b.UpdateReport(UpdateOptions{Base: "HEAD~1"}); err == nil {
		t.Error("expected CRG-run failure to surface as error")
	}
}

func TestCRGBridge_Update_WrapperPropagatesError(t *testing.T) {
	repo, crgBin := makeFakeCRGEnv(t, "#!/bin/sh\nexit 0\n", "#!/bin/sh\nexit 0\n")
	// Not a git repo → UpdateReport errors → Update wraps.
	b := &CRGBridge{RepoRoot: repo, Bin: crgBin}
	if err := b.Update(UpdateOptions{}); err == nil {
		t.Error("expected wrapper to propagate error")
	}
}

func TestCRGBridge_Update_NoDiffOK(t *testing.T) {
	repo, crgBin := makeFakeCRGEnv(t, "#!/bin/sh\nexit 0\n", "#!/bin/sh\nexit 0\n")
	initRepoGit(t, repo)
	b := &CRGBridge{RepoRoot: repo, Bin: crgBin}
	if err := b.Update(UpdateOptions{Base: "HEAD"}); err != nil {
		t.Errorf("Update no-diff: %v", err)
	}
}
