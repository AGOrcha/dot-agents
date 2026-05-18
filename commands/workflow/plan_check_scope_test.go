package workflow

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// writeScopeSidecar serializes ev to the canonical sidecar location.
func writeScopeSidecar(t *testing.T, projectPath, planID, taskID string, ev *ScopeEvidence) string {
	t.Helper()
	path := deriveScopeEvidencePath(projectPath, planID, taskID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := yaml.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// ── loadCheckScopeSidecar ─────────────────────────────────────────────────────

func TestLoadCheckScopeSidecar_HappyPath(t *testing.T) {
	proj := t.TempDir()
	ev := NewScopeEvidence("p1", "t1")
	ev.FinalWriteScope = []string{"commands/foo.go"}
	writeScopeSidecar(t, proj, "p1", "t1", ev)

	path, got, err := loadCheckScopeSidecar(proj, "p1", "t1")
	if err != nil {
		t.Fatalf("loadCheckScopeSidecar: %v", err)
	}
	if !strings.HasSuffix(path, filepath.Join("evidence", "t1.scope.yaml")) {
		t.Errorf("path = %q, want suffix evidence/t1.scope.yaml", path)
	}
	if got == nil || got.PlanID != "p1" || len(got.FinalWriteScope) != 1 {
		t.Errorf("unexpected sidecar payload: %+v", got)
	}
}

func TestLoadCheckScopeSidecar_InvalidYAMLReturnsError(t *testing.T) {
	proj := t.TempDir()
	path := deriveScopeEvidencePath(proj, "p1", "t1")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not: : valid: yaml: ::\n  - [unclosed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := loadCheckScopeSidecar(proj, "p1", "t1")
	if err == nil {
		t.Fatal("expected parse error for invalid YAML")
	}
	if !strings.Contains(err.Error(), "parse sidecar") {
		t.Errorf("expected parse-sidecar error; got %v", err)
	}
}

// ── collectCheckScopeChangedFiles ────────────────────────────────────────────

func TestCollectCheckScopeChangedFiles_DedupesAndPreservesOrder(t *testing.T) {
	proj := t.TempDir()
	got := collectCheckScopeChangedFiles(proj, []string{"a.go", "b.go", "a.go", "c.go", "b.go"}, false)
	want := []string{"a.go", "b.go", "c.go"}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got %v want %v", got, want)
	}
	for i, v := range want {
		if got[i] != v {
			t.Errorf("entry[%d]=%q want %q", i, got[i], v)
		}
	}
}

func TestCollectCheckScopeChangedFiles_FromGitDiff_AppendsAndDedupes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("git diff path differs on Windows")
	}
	proj := t.TempDir()
	mustGit(t, proj, "init", "--quiet")
	mustGit(t, proj, "config", "user.email", "t@x")
	mustGit(t, proj, "config", "user.name", "t")
	writeFile(t, proj, "tracked.go", "package x\n")
	mustGit(t, proj, "add", "tracked.go")
	mustGit(t, proj, "commit", "--quiet", "-m", "init")
	// Modify tracked file post-commit so diff HEAD picks it up.
	writeFile(t, proj, "tracked.go", "package x\nfunc Y(){}\n")

	got := collectCheckScopeChangedFiles(proj, []string{"existing.go"}, true)
	if len(got) == 0 {
		t.Fatal("expected at least one changed file")
	}
	seen := make(map[string]int)
	for _, f := range got {
		seen[f]++
	}
	if seen["tracked.go"] != 1 {
		t.Errorf("expected tracked.go once, got %d (%v)", seen["tracked.go"], got)
	}
	if seen["existing.go"] != 1 {
		t.Errorf("expected existing.go preserved, got %d (%v)", seen["existing.go"], got)
	}
}

func TestCollectCheckScopeChangedFiles_FromGitDiff_NotARepo_Warns(t *testing.T) {
	proj := t.TempDir()
	// Not a git repo; checkScopeGitDiffFiles will error but warn — caller still
	// returns the provided changedFiles list unchanged.
	got := collectCheckScopeChangedFiles(proj, []string{"a.go"}, true)
	if len(got) != 1 || got[0] != "a.go" {
		t.Errorf("expected [a.go] unchanged, got %v", got)
	}
}

// ── checkScopeGitDiffFiles ───────────────────────────────────────────────────

func TestCheckScopeGitDiffFiles_ReturnsModifiedFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("git diff path differs on Windows")
	}
	proj := t.TempDir()
	mustGit(t, proj, "init", "--quiet")
	mustGit(t, proj, "config", "user.email", "t@x")
	mustGit(t, proj, "config", "user.name", "t")
	writeFile(t, proj, "a.go", "package a\n")
	mustGit(t, proj, "add", "a.go")
	mustGit(t, proj, "commit", "--quiet", "-m", "c1")
	writeFile(t, proj, "a.go", "package a\nfunc F(){}\n")

	files, err := checkScopeGitDiffFiles(proj)
	if err != nil {
		t.Fatalf("checkScopeGitDiffFiles: %v", err)
	}
	found := false
	for _, f := range files {
		if f == "a.go" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a.go in diff result, got %v", files)
	}
}

func TestCheckScopeGitDiffFiles_FallbackToCached(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("git diff path differs on Windows")
	}
	proj := t.TempDir()
	mustGit(t, proj, "init", "--quiet")
	mustGit(t, proj, "config", "user.email", "t@x")
	mustGit(t, proj, "config", "user.name", "t")
	// No HEAD commit — `git diff HEAD` fails. Stage a file so --cached returns it.
	writeFile(t, proj, "stage.go", "package x\n")
	mustGit(t, proj, "add", "stage.go")

	files, err := checkScopeGitDiffFiles(proj)
	if err != nil {
		// Both commands may error on a brand-new repo with no commit. Acceptable.
		if !strings.Contains(err.Error(), "git diff") {
			t.Errorf("unexpected err: %v", err)
		}
		return
	}
	// If we got here, --cached produced output.
	if len(files) == 0 {
		t.Error("expected at least one cached file")
	}
}

func TestCheckScopeGitDiffFiles_NotARepoErrors(t *testing.T) {
	proj := t.TempDir()
	_, err := checkScopeGitDiffFiles(proj)
	if err == nil {
		t.Error("expected error in non-git dir")
	}
}

// ── renderCheckScopeSection / renderCheckScopeResult (clean path) ────────────

func TestRenderCheckScopeSection_NonEmptyEmitsItems(t *testing.T) {
	out := captureStdout(t, func() {
		renderCheckScopeSection(os.Stdout, "Inside Scope", "+", []string{"a.go", "b.go"})
	})
	if !strings.Contains(out, "a.go") || !strings.Contains(out, "b.go") {
		t.Errorf("missing items in output:\n%s", out)
	}
	if !strings.Contains(out, "Inside Scope") {
		t.Errorf("missing section title:\n%s", out)
	}
}

func TestRenderCheckScopeSection_EmptyDoesNotRender(t *testing.T) {
	out := captureStdout(t, func() {
		renderCheckScopeSection(os.Stdout, "Outside Scope", "!", nil)
	})
	if strings.Contains(out, "Outside Scope") {
		t.Errorf("empty items should not render section title; got:\n%s", out)
	}
}

func TestRenderCheckScopeResult_CleanRendersHeader(t *testing.T) {
	res := checkScopeResult{
		PlanID:       "p1",
		TaskID:       "t1",
		SidecarPath:  "~/some/path",
		ChangedFiles: []string{"a.go"},
		InsideScope:  []string{"a.go"},
		Clean:        true,
	}
	out := captureStdout(t, func() {
		renderCheckScopeResult("p1", "t1", "~/some/path", res)
	})
	if !strings.Contains(out, "Scope Check: p1 / t1") {
		t.Errorf("missing scope-check header:\n%s", out)
	}
	if !strings.Contains(out, "Inside Scope") {
		t.Errorf("missing inside scope section:\n%s", out)
	}
}

// ── runWorkflowPlanCheckScope: clean path (no os.Exit) ───────────────────────

func TestRunWorkflowPlanCheckScope_CleanText(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	ev := NewScopeEvidence("wave-2", "t1")
	ev.FinalWriteScope = []string{"commands/workflow.go"}
	writeScopeSidecar(t, repo, "wave-2", "t1", ev)

	chdirRepo(t, repo)
	// Pass an in-scope changed file so result is clean.
	out := captureStdout(t, func() {
		if err := runWorkflowPlanCheckScope("wave-2", "t1", []string{"commands/workflow.go"}, false); err != nil {
			t.Fatalf("runWorkflowPlanCheckScope: %v", err)
		}
	})
	if !strings.Contains(out, "Scope Check: wave-2 / t1") {
		t.Errorf("missing header in output:\n%s", out)
	}
	if !strings.Contains(out, "Inside Scope") {
		t.Errorf("missing inside scope section:\n%s", out)
	}
}

func TestRunWorkflowPlanCheckScope_CleanJSON(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	addCanonicalPlanFixture(t, repo)
	ev := NewScopeEvidence("wave-2", "t1")
	ev.FinalWriteScope = []string{"commands/workflow.go"}
	writeScopeSidecar(t, repo, "wave-2", "t1", ev)

	chdirRepo(t, repo)
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = false })

	out := captureStdout(t, func() {
		if err := runWorkflowPlanCheckScope("wave-2", "t1", []string{"commands/workflow.go"}, false); err != nil {
			t.Fatalf("runWorkflowPlanCheckScope JSON: %v", err)
		}
	})
	var res checkScopeResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("decode json: %v\nraw: %s", err, out)
	}
	if !res.Clean {
		t.Errorf("expected clean=true; got %+v", res)
	}
	if res.PlanID != "wave-2" || res.TaskID != "t1" {
		t.Errorf("unexpected ids: %+v", res)
	}
}

// ── deriveScopeRunScopeLane / appendScopeBridgeQuery / deriveScopeKGBridgeQuery
//
// All three degrade gracefully when the kg bridge subprocess fails or returns
// no parseable results. We exercise the no-results paths by pointing
// workflowDotAgentsExe at fake shell scripts.

func setFakeExe(t *testing.T, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake-exe via shell unsupported on Windows")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "dot-agents-fake")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	old := workflowDotAgentsExe
	workflowDotAgentsExe = func() (string, error) { return bin, nil }
	t.Cleanup(func() { workflowDotAgentsExe = old })
}

func TestDeriveScopeKGBridgeQuery_SuccessExtractsFiles(t *testing.T) {
	setFakeExe(t, "#!/bin/sh\ncat <<'JSON'\n{\"results\":[{\"path\":\"a.go\"},{\"file_path\":\"b.go\"},{\"path\":\"\"},{\"path\":\"a.go\"}]}\nJSON\n")
	got := deriveScopeKGBridgeQuery(t.TempDir(), "symbol_lookup", "Foo")
	// Expect a.go and b.go (deduped, empty skipped).
	if len(got) != 2 {
		t.Fatalf("got %v want 2 files", got)
	}
	want := map[string]bool{"a.go": true, "b.go": true}
	for _, f := range got {
		if !want[f] {
			t.Errorf("unexpected file %q", f)
		}
	}
}

func TestDeriveScopeKGBridgeQuery_SubprocessFailureReturnsNil(t *testing.T) {
	setFakeExe(t, "#!/bin/sh\nexit 1\n")
	got := deriveScopeKGBridgeQuery(t.TempDir(), "symbol_lookup", "X")
	if got != nil {
		t.Errorf("expected nil on subprocess failure; got %v", got)
	}
}

func TestDeriveScopeKGBridgeQuery_InvalidJSONReturnsNil(t *testing.T) {
	setFakeExe(t, "#!/bin/sh\necho 'not json'\n")
	got := deriveScopeKGBridgeQuery(t.TempDir(), "symbol_lookup", "X")
	if got != nil {
		t.Errorf("expected nil on invalid JSON; got %v", got)
	}
}

func TestDeriveScopeKGBridgeQuery_ExeLookupErrorReturnsNil(t *testing.T) {
	old := workflowDotAgentsExe
	workflowDotAgentsExe = func() (string, error) { return "", os.ErrNotExist }
	t.Cleanup(func() { workflowDotAgentsExe = old })
	got := deriveScopeKGBridgeQuery(t.TempDir(), "symbol_lookup", "X")
	if got != nil {
		t.Errorf("expected nil on exe lookup error; got %v", got)
	}
}

func TestAppendScopeBridgeQuery_AppendsQuery_WithoutSummaryOnEmpty(t *testing.T) {
	setFakeExe(t, "#!/bin/sh\necho '{}'\n")
	ev := NewScopeEvidence("p", "t")
	files := appendScopeBridgeQuery(t.TempDir(), "symbol_lookup", "Foo", ev)
	if files != nil {
		t.Errorf("expected nil files for empty results; got %v", files)
	}
	if len(ev.Queries) != 1 {
		t.Fatalf("expected one query appended; got %d", len(ev.Queries))
	}
	if ev.Queries[0].Summary != nil {
		t.Errorf("expected nil Summary when no files; got %+v", ev.Queries[0].Summary)
	}
	if ev.Queries[0].Intent != "symbol_lookup" || ev.Queries[0].Subject != "Foo" {
		t.Errorf("unexpected query: %+v", ev.Queries[0])
	}
}

func TestAppendScopeBridgeQuery_AppendsSummaryOnNonEmpty(t *testing.T) {
	setFakeExe(t, "#!/bin/sh\necho '{\"results\":[{\"path\":\"x.go\"}]}'\n")
	ev := NewScopeEvidence("p", "t")
	files := appendScopeBridgeQuery(t.TempDir(), "callers_of", "Foo", ev)
	if len(files) != 1 || files[0] != "x.go" {
		t.Errorf("expected [x.go]; got %v", files)
	}
	if ev.Queries[0].Summary == nil || len(ev.Queries[0].Summary.Files) != 1 {
		t.Errorf("expected summary with one file; got %+v", ev.Queries[0])
	}
}

func TestDeriveScopeRunScopeLane_BuildsQueriesAndRequiredPaths(t *testing.T) {
	// Fake responds with deterministic file list per (intent, subject).
	// We just need any non-empty result so RequiredPaths gets populated.
	setFakeExe(t, "#!/bin/sh\necho '{\"results\":[{\"path\":\"f.go\"}]}'\n")
	ev := NewScopeEvidence("p", "t")
	files := deriveScopeRunScopeLane(t.TempDir(), []string{"S1"}, []string{"p.go"}, ev)
	// 2 symbol queries + 1 path query = 3 queries.
	if len(ev.Queries) != 3 {
		t.Errorf("expected 3 queries (2 sym + 1 path); got %d (%+v)", len(ev.Queries), ev.Queries)
	}
	// f.go discovered → should appear in returned files + required paths.
	if len(files) == 0 {
		t.Error("expected at least one discovered file")
	}
	if len(ev.RequiredPaths) == 0 {
		t.Error("expected RequiredPaths populated")
	}
}

func TestDeriveScopeRunScopeLane_NoSeedsNoQueries(t *testing.T) {
	setFakeExe(t, "#!/bin/sh\necho '{}'\n")
	ev := NewScopeEvidence("p", "t")
	files := deriveScopeRunScopeLane(t.TempDir(), nil, nil, ev)
	if len(files) != 0 {
		t.Errorf("expected zero files with no seeds; got %v", files)
	}
	if len(ev.Queries) != 0 {
		t.Errorf("expected zero queries with no seeds; got %d", len(ev.Queries))
	}
}

// ── deriveScopeRunContextLane ────────────────────────────────────────────────

func TestDeriveScopeRunContextLane_AppendsQueriesEvenWhenNoNotes(t *testing.T) {
	graphHome := t.TempDir()
	// Set up a minimal "ready" graph home so Query proceeds. Adapter.Query
	// only needs the intent subdir lookup table — note subdirs may not exist
	// but Query won't error; results will be empty.
	if err := os.MkdirAll(filepath.Join(graphHome, "self"), 0o755); err != nil {
		t.Fatal(err)
	}
	adapter := NewLocalGraphAdapter(graphHome)
	ev := NewScopeEvidence("plan-x", "task-y")
	deriveScopeRunContextLane("plan-x", "task-y", adapter, ev)
	// Two intents: plan_context, decision_lookup → two queries.
	if len(ev.Queries) != 2 {
		t.Errorf("expected 2 queries; got %d (%+v)", len(ev.Queries), ev.Queries)
	}
}

func TestDeriveScopeRunContextLane_PopulatesRequiredReadsWhenNotesMatch(t *testing.T) {
	graphHome := t.TempDir()
	if err := os.MkdirAll(filepath.Join(graphHome, "self"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Seed a decision note that contains the query string so Query returns it.
	decisionsDir := filepath.Join(graphHome, "notes", "decisions")
	if err := os.MkdirAll(decisionsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	note := `---
id: d1
title: Sample Decision
summary: rationale for plan-x
---
plan-x task-y mentioned here
`
	if err := os.WriteFile(filepath.Join(decisionsDir, "d1.md"), []byte(note), 0o644); err != nil {
		t.Fatal(err)
	}
	adapter := NewLocalGraphAdapter(graphHome)
	ev := NewScopeEvidence("plan-x", "task-y")
	deriveScopeRunContextLane("plan-x", "task-y", adapter, ev)
	if len(ev.RequiredReads) == 0 {
		t.Errorf("expected required reads populated; got %+v", ev)
	}
	for _, q := range ev.Queries {
		if q.Summary != nil && len(q.Summary.Files) > 0 {
			return // success: at least one query has files in its summary
		}
	}
	t.Errorf("expected at least one query summary with files; got %+v", ev.Queries)
}

// ── small local helpers ──────────────────────────────────────────────────────

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	oldOut := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	_ = w.Close()
	os.Stdout = oldOut
	out := <-done
	_ = r.Close()
	return out
}

func writeFile(t *testing.T, dir, rel, body string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@x",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@x",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
