package workflow

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFirstMarkdownTitle_NoHeading(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "plain.md")
	if err := os.WriteFile(p, []byte("body only\nno heading at all\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := firstMarkdownTitle(p)
	if err != nil {
		t.Fatal(err)
	}
	if got != "plain.md" {
		t.Fatalf("expected filename fallback, got %q", got)
	}
}

func TestReadWorkflowPlan_FallbackItems(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "x.plan.md")

	content := "# Plan\n- first fallback\n- second\n"
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	summary, err := readWorkflowPlan(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.PendingItems) == 0 {
		t.Fatal("expected fallback items to populate PendingItems")
	}
}

func TestCollectWorkflowLessons_SkipsBlankLines(t *testing.T) {
	repo := t.TempDir()
	dir := filepath.Join(repo, ".agents", "lessons")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}

	body := "- lesson one\n\n  \n- lesson two\n"
	if err := os.WriteFile(filepath.Join(dir, "index.md"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	out, warns := collectWorkflowLessons(repo)
	if len(warns) != 0 {
		t.Fatalf("expected no warnings, got %v", warns)
	}
	for _, l := range out {
		if l == "" {
			t.Fatal("expected blank lines skipped")
		}
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 lessons, got %d: %v", len(out), out)
	}
}

func TestCollectWorkflowLessons_Truncate(t *testing.T) {
	repo := t.TempDir()
	dir := filepath.Join(repo, ".agents", "lessons")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	var lines []string
	for i := 0; i < 15; i++ {
		lines = append(lines, "- lesson")
	}
	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "index.md"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	out, _ := collectWorkflowLessons(repo)
	if len(out) != 10 {
		t.Fatalf("expected 10 (truncated), got %d", len(out))
	}
}

func TestRunWorkflowOrient_TextRender(t *testing.T) {
	repo := setupTestProject(t)
	chdirForCov(t, repo)
	out, err := captureCovStdout(t, runWorkflowOrient)
	if err != nil {
		t.Fatalf("orient: %v", err)
	}
	if !strings.Contains(out, "Project") {
		t.Fatalf("expected text render, got: %s", out)
	}
}

func TestCanonicalPlansHaveActive(t *testing.T) {
	if canonicalPlansHaveActive(nil) {
		t.Error("nil slice should not have active")
	}
	summaries := []workflowCanonicalPlanSummary{
		{ID: "a", Status: "draft"},
		{ID: "b", Status: "completed"},
	}
	if canonicalPlansHaveActive(summaries) {
		t.Error("no active plan should return false")
	}
	summaries = append(summaries, workflowCanonicalPlanSummary{ID: "c", Status: "active"})
	if !canonicalPlansHaveActive(summaries) {
		t.Error("should return true with an active plan")
	}
}

func TestRenderOrientRecentSessionsSection_RendersSessions(t *testing.T) {
	state := &workflowOrientState{
		RecentSessions: []branchSessionInfo{
			{Platform: "claude", SessionID: "abcdefgh1234567890", Timestamp: "2026-04-30T10:11:12Z", MessageCount: 42},
			{Platform: "codex", SessionID: "short", Timestamp: "2026-04-30T10", MessageCount: 0},
		},
	}
	out, _ := captureCovStdout(t, func() error {
		renderOrientRecentSessionsSection(state, os.Stdout)
		return nil
	})
	for _, want := range []string{"Recent sessions on this branch", "abcdefgh", "claude", "~42 messages", "codex"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output, got: %s", want, out)
		}
	}
}

func TestRenderOrientRecentSessionsSection_EmptySkipped(t *testing.T) {
	state := &workflowOrientState{}
	out, _ := captureCovStdout(t, func() error {
		renderOrientRecentSessionsSection(state, os.Stdout)
		return nil
	})
	if out != "" {
		t.Errorf("expected empty output when no sessions, got: %s", out)
	}
}

func TestRunWorkflowOrient_JSON(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	chdirForCov(t, repo)
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = false })
	out, err := captureCovStdout(t, func() error { return runWorkflowOrient() })
	if err != nil {
		t.Fatalf("runWorkflowOrient: %v", err)
	}
	if !strings.Contains(out, "\"project\":") {
		t.Errorf("expected project field in JSON output, got: %s", out)
	}
}

func TestAppendFallbackItem_CapsAt3(t *testing.T) {
	got := []string{}
	got = appendFallbackItem(got, "one")
	got = appendFallbackItem(got, "two")
	got = appendFallbackItem(got, "three")
	got = appendFallbackItem(got, "four")
	if len(got) != 3 {
		t.Errorf("expected 3 fallback items, got %d (%v)", len(got), got)
	}
	if got[2] != "three" {
		t.Errorf("expected third entry to remain 'three', got %q", got[2])
	}
}

func TestAppendWorkflowSessionLog_WritesFullEntry(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "session-log.md")
	cp := workflowCheckpoint{
		Timestamp:  "2026-04-30T10:11:12Z",
		Git:        workflowGitSummary{Branch: "br", SHA: "abc"},
		Message:    "test message",
		NextAction: "do thing",
	}
	cp.Files.Modified = []string{"a.go", "b.go"}
	cp.Verification.Status = "pass"
	cp.Verification.Summary = "all green"
	if err := appendWorkflowSessionLog(logPath, cp); err != nil {
		t.Fatalf("appendWorkflowSessionLog: %v", err)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	for _, want := range []string{"2026-04-30T10:11:12Z", "branch: br", "sha: abc", "files: 2", "test message"} {
		if !strings.Contains(body, want) {
			t.Errorf("expected %q in session log, got:\n%s", want, body)
		}
	}
}

func TestCompletedPlanStatus(t *testing.T) {
	if isC, ok := completedPlanStatus("Status: Completed"); !ok || !isC {
		t.Errorf("Completed should parse to (true,true), got (%v,%v)", isC, ok)
	}
	if isC, ok := completedPlanStatus("Status: in_progress"); !ok || isC {
		t.Errorf("in_progress should parse to (false,true)")
	}
	if _, ok := completedPlanStatus("- bullet"); ok {
		t.Error("non-status line should return ok=false")
	}
}

func TestPendingPlanItem(t *testing.T) {
	if got, ok := pendingPlanItem("- [ ] my task"); !ok || got != "my task" {
		t.Errorf("got (%q,%v), want (my task,true)", got, ok)
	}
	if _, ok := pendingPlanItem("- [x] done"); ok {
		t.Error("checked item should return ok=false")
	}
	if _, ok := pendingPlanItem("not a bullet"); ok {
		t.Error("non-bullet should return ok=false")
	}
}

func TestRenderOrientRecentSessionsSection_TimestampTruncated(t *testing.T) {
	state := &workflowOrientState{
		RecentSessions: []branchSessionInfo{
			{Platform: "claude", SessionID: "x", Timestamp: "2026-04-30T10:11:12Z-extra-stuff", MessageCount: 1},
		},
	}
	out, _ := captureCovStdout(t, func() error {
		renderOrientRecentSessionsSection(state, os.Stdout)
		return nil
	})

	if strings.Contains(out, "extra-stuff") {
		t.Errorf("expected timestamp truncated, got: %s", out)
	}
}

func TestReadWorkflowPlanTitle_NoHeader(t *testing.T) {
	got := readWorkflowPlanTitle([]string{"- not a header"}, "/path/to/myplan.md")
	if got != "myplan.md" {
		t.Errorf("expected fallback to filename, got %q", got)
	}
}

func TestReadWorkflowPlanTitle_EmptyLines(t *testing.T) {
	got := readWorkflowPlanTitle([]string{}, "/path/to/named.md")
	if got != "named.md" {
		t.Errorf("expected fallback to filename for empty lines, got %q", got)
	}
}

func TestLoadWorkflowCheckpoint_ReadFileError(t *testing.T) {
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	contextDir := filepath.Join(agentsHome, "context", "proj-unreadable")
	if err := os.MkdirAll(contextDir, 0755); err != nil {
		t.Fatal(err)
	}
	cpPath := filepath.Join(contextDir, "checkpoint.yaml")
	if err := os.WriteFile(cpPath, []byte("schema_version: 1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	chmodUnreadable(t, cpPath)

	cp, warnings := loadWorkflowCheckpoint("proj-unreadable")
	if cp != nil {
		t.Fatalf("expected nil checkpoint on read error, got %+v", cp)
	}
	if len(warnings) == 0 || !strings.Contains(warnings[0], "checkpoint unreadable") {
		t.Fatalf("expected unreadable warning, got %v", warnings)
	}
}

func TestRunWorkflowLog_ReadError(t *testing.T) {
	repo := t.TempDir()
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	contextDir := filepath.Join(agentsHome, "context", filepath.Base(repo))
	if err := os.MkdirAll(contextDir, 0755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(contextDir, "session-log.md")
	if err := os.WriteFile(logPath, []byte("# log\n"), 0644); err != nil {
		t.Fatal(err)
	}
	chmodUnreadable(t, logPath)

	chdirForCov(t, repo)
	err := runWorkflowLog(false)
	if err == nil {
		t.Fatal("expected ReadFile error to propagate")
	}
}

func TestFirstMarkdownTitle_ReadError(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "note.md")
	if err := os.WriteFile(path, []byte("# Title\n"), 0644); err != nil {
		t.Fatal(err)
	}
	chmodUnreadable(t, path)

	_, err := firstMarkdownTitle(path)
	if err == nil {
		t.Fatal("expected ReadFile error")
	}
}

func TestCollectWorkflowHandoffs_PropagatesReadErr(t *testing.T) {
	repo := t.TempDir()
	handoffDir := filepath.Join(repo, ".agents", "active", "handoffs")
	if err := os.MkdirAll(handoffDir, 0755); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(handoffDir, "bad.md")
	if err := os.WriteFile(bad, []byte("# h\n"), 0644); err != nil {
		t.Fatal(err)
	}
	chmodUnreadable(t, bad)

	_, err := collectWorkflowHandoffs(repo)
	if err == nil {
		t.Fatal("expected handoff read error to propagate")
	}
}

func TestCollectWorkflowPlans_PropagatesReadErr(t *testing.T) {
	repo := t.TempDir()
	planDir := filepath.Join(repo, ".agents", "active")
	if err := os.MkdirAll(planDir, 0755); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(planDir, "x.plan.md")
	if err := os.WriteFile(bad, []byte("# plan\n"), 0644); err != nil {
		t.Fatal(err)
	}
	chmodUnreadable(t, bad)

	_, err := collectWorkflowPlans(repo)
	if err == nil {
		t.Fatal("expected plan read error to propagate")
	}
}

func TestCollectWorkflowPlanItems_PendingCapped(t *testing.T) {
	lines := []string{
		"# Plan",
		"- [ ] a",
		"- [ ] b",
		"- [ ] c",
		"- [ ] should-not-appear",
	}
	pending, _, _ := collectWorkflowPlanItems(lines)
	if len(pending) != 3 {
		t.Fatalf("expected 3 pending (cap), got %d: %v", len(pending), pending)
	}
}

func TestAppendFallbackItem_CappedAtThree(t *testing.T) {
	out := appendFallbackItem([]string{"a", "b", "c"}, "d")
	if len(out) != 3 {
		t.Fatalf("expected 3 (no append), got %d: %v", len(out), out)
	}
}

func TestRunWorkflowStatus_JSON(t *testing.T) {
	repo := setupTestProject(t)
	chdirForCov(t, repo)
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = false })
	out, err := captureCovStdout(t, runWorkflowStatus)
	if err != nil {
		t.Fatalf("status json: %v", err)
	}
	if !strings.Contains(out, `"project"`) {
		t.Fatalf("expected JSON output, got: %s", out)
	}
	if !strings.Contains(out, `"work_tracking"`) || !strings.Contains(out, `"backend": "local"`) {
		t.Fatalf("expected work_tracking backend in JSON status, got: %s", out)
	}
}

func TestListCanonicalPlanIDs_ReadDirError(t *testing.T) {
	repo := t.TempDir()
	base := filepath.Join(repo, ".agents", "workflow", "plans")
	if err := os.MkdirAll(base, 0755); err != nil {
		t.Fatal(err)
	}
	chmodUnreadableDir(t, base)

	_, err := listCanonicalPlanIDs(repo)
	if err == nil {
		t.Fatal("expected ReadDir error")
	}
}

func TestGitOutput_NotARepo(t *testing.T) {
	out := gitOutput(t.TempDir(), "rev-parse", "HEAD")
	if out != "" {
		t.Fatalf("expected empty output from non-repo, got %q", out)
	}
}

func TestGitModifiedFiles_NotARepo(t *testing.T) {
	files, err := gitModifiedFiles(t.TempDir())
	if err != nil {
		t.Fatalf("non-repo should not error, got %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("expected empty slice, got %v", files)
	}
}

func TestEnrichWorkflowState_EmptyBranchNoSessions(t *testing.T) {
	state := &workflowOrientState{
		Project: workflowProjectRef{Name: "p", Path: t.TempDir()},
		Git:     workflowGitSummary{Branch: ""},
	}
	enrichWorkflowState(state)
	if len(state.RecentSessions) != 0 {
		t.Fatalf("expected no sessions for empty branch, got %d", len(state.RecentSessions))
	}
}

func TestGatherWorkflowStateInputs_PlansError(t *testing.T) {
	repo := t.TempDir()
	planDir := filepath.Join(repo, ".agents", "active")
	if err := os.MkdirAll(planDir, 0755); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(planDir, "x.plan.md")
	if err := os.WriteFile(bad, []byte("# plan\n"), 0644); err != nil {
		t.Fatal(err)
	}
	chmodUnreadable(t, bad)
	chdirForCov(t, repo)
	_, err := gatherWorkflowStateInputs()
	if err == nil {
		t.Fatal("expected propagated read error")
	}
}

func TestGitOutput_RealRepo(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	out := gitOutput(repo, "rev-parse", "HEAD")
	if out == "" {
		t.Fatal("expected SHA output from real repo")
	}
}

func TestGitModifiedFiles_RealRepo(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	files, err := gitModifiedFiles(repo)
	if err != nil {
		t.Fatal(err)
	}

	if len(files) == 0 {
		t.Fatal("expected modified files")
	}
}

func TestParseGitModifiedFiles(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", []string{}},
		{"whitespace only", "   \n  \n", []string{}},
		{"single untracked", "?? README.md", []string{"README.md"}},
		{"multiple entries", "?? a.go\nA  c.go\n", []string{"a.go", "c.go"}},
		{"blank interior line dropped", "?? a.go\n\n?? b.go", []string{"a.go", "b.go"}},
		{"short line dropped", "?? a.go\nXY\n?? b.go", []string{"a.go", "b.go"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseGitModifiedFiles(tc.in)
			if got == nil {
				t.Fatal("parseGitModifiedFiles must return a non-nil slice")
			}
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Fatalf("parseGitModifiedFiles(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestCollectWorkflowGitSummary_CachesModifiedFiles proves the git summary
// caches the same modified-file list gitModifiedFiles would compute, so the
// checkpoint path can reuse it instead of re-spawning git.
func TestCollectWorkflowGitSummary_CachesModifiedFiles(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	summary, _ := collectWorkflowGitSummary(repo)
	direct, err := gitModifiedFiles(repo)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(summary.modifiedFiles, "|") != strings.Join(direct, "|") {
		t.Fatalf("cached modifiedFiles %v != gitModifiedFiles %v", summary.modifiedFiles, direct)
	}
	if len(direct) == 0 {
		t.Fatal("fixture should have at least one modified file")
	}
}

// TestCollectWorkflowGitSummary_NonRepoModifiedEmpty guards the non-repo early
// return: modifiedFiles must be a non-nil empty slice, matching gitModifiedFiles.
func TestCollectWorkflowGitSummary_NonRepoModifiedEmpty(t *testing.T) {
	summary, warnings := collectWorkflowGitSummary(t.TempDir())
	if summary.modifiedFiles == nil || len(summary.modifiedFiles) != 0 {
		t.Fatalf("expected non-nil empty modifiedFiles, got %#v", summary.modifiedFiles)
	}
	if len(warnings) == 0 {
		t.Fatal("expected a 'git repo not detected' warning")
	}
}

// TestRunWorkflowCheckpoint_ModifiedFilesMatchDirect proves the reuse path
// persists the identical modified-file list the pre-optimization direct
// gitModifiedFiles call produced (behavior preservation).
func TestRunWorkflowCheckpoint_ModifiedFilesMatchDirect(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	t.Setenv("AGENTS_HOME", t.TempDir())
	chdirRepo(t, repo)

	want, err := gitModifiedFiles(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(want) == 0 {
		t.Fatal("fixture should have at least one modified file")
	}
	if err := runWorkflowCheckpoint("cp", "pass", "ok"); err != nil {
		t.Fatal(err)
	}
	project, err := currentWorkflowProject()
	if err != nil {
		t.Fatal(err)
	}
	cp, _ := loadWorkflowCheckpoint(project.Name)
	if cp == nil {
		t.Fatal("expected persisted checkpoint")
	}
	if strings.Join(cp.Files.Modified, "|") != strings.Join(want, "|") {
		t.Fatalf("checkpoint Files.Modified %v != gitModifiedFiles %v", cp.Files.Modified, want)
	}
}

func TestRunWorkflowCheckpoint_HappyPath(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	chdirRepo(t, repo)
	if err := runWorkflowCheckpoint("hello", "pass", "all green"); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestRunWorkflowCheckpoint_PersistsSessionLog(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	chdirRepo(t, repo)
	if err := runWorkflowCheckpoint("first message", "pass", "all good"); err != nil {
		t.Fatal(err)
	}

	matches, _ := filepath.Glob(filepath.Join(agentsHome, "context", "*", "session-log.md"))
	if len(matches) == 0 {
		t.Fatalf("expected session-log.md, found: %v", matches)
	}
}

func TestRunWorkflowCheckpoint_LogToIterFlag(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	addCanonicalPlanFixture(t, repo)
	iterDir := filepath.Join(repo, ".agents", "active", "iteration-log")
	if err := os.MkdirAll(iterDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(iterDir, "iter-1.yaml"),
		[]byte("schema_version: 1\niteration: 1\nstarted_at: 2026-01-01T00:00:00Z\nrole: worker\n"),
		0644); err != nil {
		t.Fatal(err)
	}
	if err := executeWorkflowCommand(t, repo, "checkpoint",
		"--message", "iter checkpoint",
		"--verification-status", "pass",
		"--log-to-iter", "1",
	); err != nil {
		t.Fatalf("checkpoint --log-to-iter: %v", err)
	}
}

func TestRunWorkflowLog_ReadsExistingFile(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	chdirRepo(t, repo)
	if err := runWorkflowCheckpoint("for-log", "pass", "ok"); err != nil {
		t.Fatal(err)
	}
	if err := runWorkflowLog(false); err != nil {
		t.Fatalf("runWorkflowLog: %v", err)
	}
	if err := runWorkflowLog(true); err != nil {
		t.Fatalf("runWorkflowLog all: %v", err)
	}
}

func TestRunWorkflowCheckpoint_NonGitDirectory(t *testing.T) {

	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, ".agentsrc.json"),
		[]byte(`{"project":"no-git","version":1,"sources":[{"type":"local"}]}`),
		0644); err != nil {
		t.Fatal(err)
	}
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	chdirRepo(t, repo)
	if err := runWorkflowCheckpoint("msg", "pass", "sum"); err != nil {
		t.Fatalf("checkpoint should tolerate non-git: %v", err)
	}
}

func TestRunWorkflowCheckpoint_InvalidVerificationStatus(t *testing.T) {
	err := runWorkflowCheckpoint("msg", "garbage", "summary")
	if err == nil || !strings.Contains(err.Error(), "invalid verification status") {
		t.Fatalf("expected invalid verification status, got %v", err)
	}
}

func TestRunWorkflowCheckpoint_AppendLogOpenError(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	chdirRepo(t, repo)

	sentinel := errors.New("open boom")
	prev := osOpenFile
	osOpenFile = func(string, int, os.FileMode) (*os.File, error) { return nil, sentinel }
	t.Cleanup(func() { osOpenFile = prev })

	err := runWorkflowCheckpoint("msg", "pass", "summary")
	if err == nil || !errors.Is(err, sentinel) {
		t.Fatalf("expected open sentinel, got %v", err)
	}
}

func TestRunWorkflowCheckpoint_MarshalError(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	chdirRepo(t, repo)

	sentinel := errors.New("marshal boom")
	prev := yamlMarshal
	yamlMarshal = func(v any) ([]byte, error) { return nil, sentinel }
	t.Cleanup(func() { yamlMarshal = prev })

	err := runWorkflowCheckpoint("msg", "pass", "summary")
	if err == nil || !errors.Is(err, sentinel) {
		t.Fatalf("expected marshal sentinel, got %v", err)
	}
}

// TestRunWorkflowStatus_ShowsLocalBackendByDefault proves `da workflow status`
// surfaces the active coordination-state backend and that an unconfigured repo
// (no work_tracking block) resolves to the default local plane — the working
// copy is the coordination SOT, byte-for-byte today's behaviour.
func TestRunWorkflowStatus_ShowsLocalBackendByDefault(t *testing.T) {
	repo := setupTestProject(t)
	chdirForCov(t, repo)
	prev := workflowTestJSON
	workflowTestJSON = false
	t.Cleanup(func() { workflowTestJSON = prev })

	out, err := captureCovStdout(t, runWorkflowStatus)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out, "Work Tracking") {
		t.Fatalf("expected a Work Tracking section, got:\n%s", out)
	}
	if !strings.Contains(out, "backend: local") {
		t.Fatalf("expected default backend local, got:\n%s", out)
	}
	if !strings.Contains(out, "coordination SOT: working copy (local)") {
		t.Fatalf("expected working-copy SOT for local backend, got:\n%s", out)
	}
	if strings.Contains(out, "refs/agents/state") {
		t.Fatalf("local backend must not claim the state ref is the SOT, got:\n%s", out)
	}
}

// TestRunWorkflowStatus_ShowsGitRefBackend proves that with
// work_tracking.backend=git-ref, status reports the git-ref backend and that
// refs/agents/state is the live coordination source of truth.
func TestRunWorkflowStatus_ShowsGitRefBackend(t *testing.T) {
	repo := setupTestProject(t)
	if err := os.WriteFile(filepath.Join(repo, ".agentsrc.json"),
		[]byte(`{"project":"p","version":2,"sources":[{"type":"local"}],"work_tracking":{"backend":"git-ref"}}`),
		0644); err != nil {
		t.Fatal(err)
	}
	chdirForCov(t, repo)
	prev := workflowTestJSON
	workflowTestJSON = false
	t.Cleanup(func() { workflowTestJSON = prev })

	out, err := captureCovStdout(t, runWorkflowStatus)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out, "backend: git-ref") {
		t.Fatalf("expected git-ref backend, got:\n%s", out)
	}
	if !strings.Contains(out, "coordination SOT: refs/agents/state (live)") {
		t.Fatalf("expected state-ref SOT for git-ref backend, got:\n%s", out)
	}
}

// TestCollectWorkTrackingSummary covers the backend-resolution helper across the
// default (missing manifest), git-ref, and a RESERVED ladder value (kg resolves
// to local until implemented).
func TestCollectWorkTrackingSummary(t *testing.T) {
	t.Run("missing manifest defaults to local", func(t *testing.T) {
		got := collectWorkTrackingSummary(t.TempDir())
		if got.Backend != "local" || got.StateRefSOT || got.StateRef != "" {
			t.Fatalf("missing manifest must resolve to local, got %+v", got)
		}
	})
	t.Run("git-ref backend flags the state ref as SOT", func(t *testing.T) {
		repo := t.TempDir()
		if err := os.WriteFile(filepath.Join(repo, ".agentsrc.json"),
			[]byte(`{"project":"p","version":2,"sources":[{"type":"local"}],"work_tracking":{"backend":"git-ref"}}`),
			0644); err != nil {
			t.Fatal(err)
		}
		got := collectWorkTrackingSummary(repo)
		if got.Backend != "git-ref" || !got.StateRefSOT || got.StateRef != stateRefName {
			t.Fatalf("git-ref must flag state-ref SOT, got %+v", got)
		}
	})
	t.Run("reserved backend resolves to local", func(t *testing.T) {
		repo := t.TempDir()
		if err := os.WriteFile(filepath.Join(repo, ".agentsrc.json"),
			[]byte(`{"project":"p","version":2,"sources":[{"type":"local"}],"work_tracking":{"backend":"kg"}}`),
			0644); err != nil {
			t.Fatal(err)
		}
		got := collectWorkTrackingSummary(repo)
		if got.Backend != "kg" {
			t.Fatalf("reserved backend should be reported verbatim, got %q", got.Backend)
		}
		if got.StateRefSOT || got.StateRef != "" {
			t.Fatalf("reserved backend must not claim state-ref SOT, got %+v", got)
		}
	})
}

// TestRenderOrientWorkTrackingSection proves the orient markdown renderer emits
// the backend + coordination-SOT lines for both the local and git-ref planes.
func TestRenderOrientWorkTrackingSection(t *testing.T) {
	local := &workflowOrientState{WorkTracking: workflowWorkTrackingSummary{Backend: "local"}}
	var lb strings.Builder
	renderOrientWorkTrackingSection(local, &lb)
	if !strings.Contains(lb.String(), "- backend: local") ||
		!strings.Contains(lb.String(), "working copy (local)") {
		t.Fatalf("local orient render missing expected lines:\n%s", lb.String())
	}

	gitRef := &workflowOrientState{WorkTracking: workflowWorkTrackingSummary{
		Backend: "git-ref", StateRefSOT: true, StateRef: stateRefName,
	}}
	var gb strings.Builder
	renderOrientWorkTrackingSection(gitRef, &gb)
	if !strings.Contains(gb.String(), "- backend: git-ref") ||
		!strings.Contains(gb.String(), stateRefName+" (live)") {
		t.Fatalf("git-ref orient render missing expected lines:\n%s", gb.String())
	}
}
