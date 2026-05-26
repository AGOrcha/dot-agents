package workflow

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ── Schema compile/validation tests ──────────────────────────────────────────

func TestCompiledWorkflowHookSentinelSchema(t *testing.T) {
	sch, err := compiledWorkflowHookSentinelSchema(stdSchemaCompiler{})
	if err != nil {
		t.Fatalf("compiledWorkflowHookSentinelSchema: %v", err)
	}
	if sch == nil {
		t.Error("expected non-nil schema")
	}
}

func newValidHookSentinelDoc() *HookSentinelDoc {
	return &HookSentinelDoc{
		SchemaVersion:  1,
		Skill:          "loop-worker",
		RunID:          "r1",
		StartedAt:      "2026-05-26T12:00:00Z",
		PlanID:         "plan-1",
		TaskID:         "task-1",
		AgentType:      "loop-worker",
		LifecyclePoint: "skill_entry",
	}
}

func TestValidateHookSentinelDoc_Valid(t *testing.T) {
	if err := validateHookSentinelDoc(newValidHookSentinelDoc()); err != nil {
		t.Fatalf("valid doc rejected: %v", err)
	}
}

func TestValidateHookSentinelDoc_Nil(t *testing.T) {
	if err := validateHookSentinelDoc(nil); err == nil {
		t.Error("expected error for nil doc")
	}
}

func TestValidateHookSentinelDoc_BadSkillEnum(t *testing.T) {
	doc := newValidHookSentinelDoc()
	doc.Skill = "not-a-skill"
	if err := validateHookSentinelDoc(doc); err == nil {
		t.Error("expected schema rejection for unknown skill enum value")
	}
}

func TestValidateHookSentinelDoc_BadAgentTypeEnum(t *testing.T) {
	doc := newValidHookSentinelDoc()
	doc.AgentType = "rogue"
	if err := validateHookSentinelDoc(doc); err == nil {
		t.Error("expected schema rejection for unknown agent_type enum value")
	}
}

func TestValidateHookSentinelDoc_BadStartedAtFormat(t *testing.T) {
	doc := newValidHookSentinelDoc()
	doc.StartedAt = "2026-05-26 12:00:00" // missing 'T' and tz
	if err := validateHookSentinelDoc(doc); err == nil {
		t.Error("expected schema rejection for malformed started_at")
	}
}

func TestValidateHookSentinelDoc_MissingRequired(t *testing.T) {
	doc := newValidHookSentinelDoc()
	doc.PlanID = ""
	if err := validateHookSentinelDoc(doc); err == nil {
		t.Error("expected schema rejection for empty plan_id (minLength: 1)")
	}
}

// ── Filename validation ──────────────────────────────────────────────────────

func TestValidHookSentinelSkill(t *testing.T) {
	for _, s := range []string{"iteration-close", "isp", "loop-worker"} {
		if !validHookSentinelSkill(s) {
			t.Errorf("expected %q to be valid", s)
		}
	}
	for _, s := range []string{"", "ISP", "loop_worker", "other"} {
		if validHookSentinelSkill(s) {
			t.Errorf("expected %q to be invalid", s)
		}
	}
}

func TestValidHookSentinelRunID(t *testing.T) {
	cases := map[string]bool{
		"r1":              true,
		"run-1":           true,
		"run_1":           true,
		"Run.1":           true,
		"abc123":          true,
		"":                false,
		"-leading-dash":   false,
		".leading-dot":    false,
		"_leading-under":  false,
		"with space":      false,
		"with/slash":      false,
		"with\\backslash": false,
	}
	for in, want := range cases {
		if got := validHookSentinelRunID(in); got != want {
			t.Errorf("validHookSentinelRunID(%q)=%v want %v", in, got, want)
		}
	}
}

func TestHookSentinelActivePath_BadInputs(t *testing.T) {
	if _, err := hookSentinelActivePath("/proj", "bogus", "r1"); err == nil {
		t.Error("expected error for invalid skill")
	}
	if _, err := hookSentinelActivePath("/proj", "isp", "bad id"); err == nil {
		t.Error("expected error for invalid run_id")
	}
	p, err := hookSentinelActivePath("/proj", "isp", "r1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(p, filepath.Join(".agents", "active", "hook-sentinels", "isp-r1.json")) {
		t.Errorf("unexpected path: %s", p)
	}
}

// ── Write/read round-trip + collision + atomicity ────────────────────────────

func TestWriteHookSentinelAtomically_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	doc := newValidHookSentinelDoc()
	path, err := writeHookSentinelAtomically(dir, doc)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected sentinel on disk: %v", err)
	}
	// .tmp must NOT remain after a successful publish.
	if _, err := os.Stat(path + ".tmp"); err == nil {
		t.Error("expected .tmp scratch file to be gone after publish")
	}
	got, err := readHookSentinel(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.RunID != doc.RunID || got.Skill != doc.Skill {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}

func TestWriteHookSentinelAtomically_NilDoc(t *testing.T) {
	if _, err := writeHookSentinelAtomically(t.TempDir(), nil); err == nil {
		t.Error("expected error for nil doc")
	}
}

func TestWriteHookSentinelAtomically_InvalidSkillBeforeFS(t *testing.T) {
	dir := t.TempDir()
	doc := newValidHookSentinelDoc()
	doc.Skill = "wrong"
	if _, err := writeHookSentinelAtomically(dir, doc); err == nil {
		t.Error("expected pre-FS rejection for invalid skill")
	}
	// No directory should have been created either.
	if _, err := os.Stat(filepath.Join(dir, ".agents")); err == nil {
		t.Error(".agents dir was created despite invalid skill rejection")
	}
}

func TestWriteHookSentinelAtomically_SchemaFailure(t *testing.T) {
	dir := t.TempDir()
	doc := newValidHookSentinelDoc()
	doc.StartedAt = "not-rfc3339"
	if _, err := writeHookSentinelAtomically(dir, doc); err == nil {
		t.Error("expected schema validation failure to block write")
	}
}

func TestWriteHookSentinelAtomically_CollisionRejected(t *testing.T) {
	dir := t.TempDir()
	doc := newValidHookSentinelDoc()
	if _, err := writeHookSentinelAtomically(dir, doc); err != nil {
		t.Fatalf("first write: %v", err)
	}
	_, err := writeHookSentinelAtomically(dir, doc)
	if err == nil {
		t.Fatal("expected collision rejection on second write")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected `already exists` in error, got %q", err.Error())
	}
}

func TestWriteHookSentinelAtomically_RenameFailureCleansTemp(t *testing.T) {
	dir := t.TempDir()
	doc := newValidHookSentinelDoc()

	sentinel := errors.New("rename boom")
	oldRename := osRename
	osRename = func(src, dst string) error { return sentinel }
	t.Cleanup(func() { osRename = oldRename })

	_, err := writeHookSentinelAtomically(dir, doc)
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected rename sentinel wrapped, got %v", err)
	}
	// .tmp should be cleaned up so a retry can succeed.
	target := filepath.Join(dir, ".agents", "active", "hook-sentinels", "loop-worker-r1.json.tmp")
	if _, statErr := os.Stat(target); statErr == nil {
		t.Errorf("expected .tmp cleanup after rename failure: %s still present", target)
	}
}

func TestWriteHookSentinelAtomically_MkdirFailure(t *testing.T) {
	dir := t.TempDir()
	doc := newValidHookSentinelDoc()

	sentinel := errors.New("mkdir boom")
	oldMkdir := osMkdirAll
	osMkdirAll = func(string, os.FileMode) error { return sentinel }
	t.Cleanup(func() { osMkdirAll = oldMkdir })

	_, err := writeHookSentinelAtomically(dir, doc)
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected mkdir sentinel wrapped, got %v", err)
	}
}

func TestWriteHookSentinelAtomically_WriteFileFailure(t *testing.T) {
	dir := t.TempDir()
	doc := newValidHookSentinelDoc()

	sentinel := errors.New("write boom")
	oldWrite := osWriteFile
	osWriteFile = func(string, []byte, os.FileMode) error { return sentinel }
	t.Cleanup(func() { osWriteFile = oldWrite })

	_, err := writeHookSentinelAtomically(dir, doc)
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected write sentinel wrapped, got %v", err)
	}
}

func TestReadHookSentinel_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "iteration-close-r1.json")
	if err := os.WriteFile(path, []byte("{not-json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readHookSentinel(path); err == nil {
		t.Error("expected parse failure for malformed JSON")
	}
}

func TestReadHookSentinel_Missing(t *testing.T) {
	if _, err := readHookSentinel(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Error("expected read error for missing file")
	}
}

// ── readLatestHookSentinel ───────────────────────────────────────────────────

func TestReadLatestHookSentinel_PicksMostRecentStartedAt(t *testing.T) {
	dir := t.TempDir()
	older := newValidHookSentinelDoc()
	older.RunID = "rA"
	older.StartedAt = "2026-05-26T10:00:00Z"
	if _, err := writeHookSentinelAtomically(dir, older); err != nil {
		t.Fatal(err)
	}
	newer := newValidHookSentinelDoc()
	newer.RunID = "rB"
	newer.StartedAt = "2026-05-26T11:00:00Z"
	if _, err := writeHookSentinelAtomically(dir, newer); err != nil {
		t.Fatal(err)
	}

	got, path, err := readLatestHookSentinel(dir, "loop-worker")
	if err != nil {
		t.Fatalf("read latest: %v", err)
	}
	if got.RunID != "rB" {
		t.Errorf("expected newer rB, got %s (path %s)", got.RunID, path)
	}
}

func TestReadLatestHookSentinel_FilenameTieBreaker(t *testing.T) {
	dir := t.TempDir()
	a := newValidHookSentinelDoc()
	a.RunID = "rA"
	a.StartedAt = "2026-05-26T10:00:00Z"
	if _, err := writeHookSentinelAtomically(dir, a); err != nil {
		t.Fatal(err)
	}
	b := newValidHookSentinelDoc()
	b.RunID = "rB"
	b.StartedAt = "2026-05-26T10:00:00Z" // exact tie
	if _, err := writeHookSentinelAtomically(dir, b); err != nil {
		t.Fatal(err)
	}

	got, _, err := readLatestHookSentinel(dir, "loop-worker")
	if err != nil {
		t.Fatalf("read latest: %v", err)
	}
	// loop-worker-rB.json > loop-worker-rA.json lexicographically
	if got.RunID != "rB" {
		t.Errorf("filename tie-breaker should prefer rB, got %s", got.RunID)
	}
}

func TestReadLatestHookSentinel_NoneForSkill(t *testing.T) {
	dir := t.TempDir()
	// Only an isp sentinel exists; ask for loop-worker.
	other := newValidHookSentinelDoc()
	other.Skill = "isp"
	other.RunID = "r1"
	other.AgentType = "main"
	if _, err := writeHookSentinelAtomically(dir, other); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readLatestHookSentinel(dir, "loop-worker"); err == nil {
		t.Error("expected error when no sentinel exists for the requested skill")
	}
}

func TestReadLatestHookSentinel_DirMissing(t *testing.T) {
	if _, _, err := readLatestHookSentinel(t.TempDir(), "loop-worker"); err == nil {
		t.Error("expected error when hook-sentinels dir is missing")
	}
}

func TestReadLatestHookSentinel_InvalidSkillName(t *testing.T) {
	if _, _, err := readLatestHookSentinel(t.TempDir(), "bogus"); err == nil {
		t.Error("expected error for invalid skill")
	}
}

// ── clearHookSentinel: archive-on-clear ──────────────────────────────────────

func TestClearHookSentinel_ArchivesToHistory(t *testing.T) {
	dir := t.TempDir()
	doc := newValidHookSentinelDoc()
	doc.PlanID = "loop-discipline-stop-hooks"
	doc.StartedAt = "2026-05-26T12:00:00Z"
	active, err := writeHookSentinelAtomically(dir, doc)
	if err != nil {
		t.Fatal(err)
	}
	_, archive, err := clearHookSentinel(dir, doc.Skill, doc.RunID)
	if err != nil {
		t.Fatalf("clear: %v", err)
	}
	wantPrefix := filepath.Join(dir, ".agents", "history", "loop-discipline-stop-hooks", "hook-sentinels", "2026-05-26")
	if !strings.HasPrefix(archive, wantPrefix) {
		t.Errorf("archive %s should be under %s", archive, wantPrefix)
	}
	if !strings.HasSuffix(archive, "loop-worker-r1.json") {
		t.Errorf("archive filename should match original: %s", archive)
	}
	// Active record must be gone.
	if _, statErr := os.Stat(active); statErr == nil {
		t.Errorf("expected active record removed after archive: %s", active)
	}
	// And archive must be readable.
	if _, statErr := os.Stat(archive); statErr != nil {
		t.Errorf("expected archive readable: %v", statErr)
	}
}

func TestClearHookSentinel_ArchiveCollisionRejected(t *testing.T) {
	dir := t.TempDir()
	doc := newValidHookSentinelDoc()
	doc.StartedAt = "2026-05-26T12:00:00Z"
	if _, err := writeHookSentinelAtomically(dir, doc); err != nil {
		t.Fatal(err)
	}
	// Pre-populate the archive slot to force collision on clear.
	archiveDir := hookSentinelArchiveDir(dir, doc.PlanID, "2026-05-26")
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		t.Fatal(err)
	}
	colliding := filepath.Join(archiveDir, "loop-worker-r1.json")
	if err := os.WriteFile(colliding, []byte("preexisting"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := clearHookSentinel(dir, doc.Skill, doc.RunID); err == nil {
		t.Error("expected archive-collision rejection (v1 does not overwrite history)")
	}
}

func TestClearHookSentinel_MissingActive(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := clearHookSentinel(dir, "loop-worker", "nope"); err == nil {
		t.Error("expected error when active record missing")
	}
}

func TestClearHookSentinel_BadStartedAt(t *testing.T) {
	dir := t.TempDir()
	// Write a sentinel with valid schema but stash a corrupted started_at on
	// disk by overwriting after the fact (bypassing validation). The clear
	// path re-parses started_at to derive the archive date bucket.
	doc := newValidHookSentinelDoc()
	path, err := writeHookSentinelAtomically(dir, doc)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	mangled := strings.Replace(string(raw), `"started_at": "2026-05-26T12:00:00Z"`, `"started_at": "not-a-time"`, 1)
	if err := os.WriteFile(path, []byte(mangled), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := clearHookSentinel(dir, doc.Skill, doc.RunID); err == nil {
		t.Error("expected error when started_at is not RFC3339 — clear cannot derive archive bucket")
	}
}

// ── buildHookSentinelDoc input validation ────────────────────────────────────

func TestBuildHookSentinelDoc_RequiresAllFields(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name string
		in   hookSentinelWriteInputs
		want string
	}{
		{"bad-skill", hookSentinelWriteInputs{Skill: "bad", RunID: "r1", PlanID: "p", TaskID: "t", AgentType: "main"}, "skill"},
		{"bad-run", hookSentinelWriteInputs{Skill: "isp", RunID: "bad id", PlanID: "p", TaskID: "t", AgentType: "main"}, "run-id"},
		{"missing-plan", hookSentinelWriteInputs{Skill: "isp", RunID: "r1", PlanID: "", TaskID: "t", AgentType: "main"}, "plan"},
		{"missing-task", hookSentinelWriteInputs{Skill: "isp", RunID: "r1", PlanID: "p", TaskID: "", AgentType: "main"}, "task"},
		{"bad-agent", hookSentinelWriteInputs{Skill: "isp", RunID: "r1", PlanID: "p", TaskID: "t", AgentType: "ghost"}, "agent-type"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := buildHookSentinelDoc(dir, c.in)
			if err == nil {
				t.Fatalf("expected rejection for %s", c.name)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %q should mention %q", err.Error(), c.want)
			}
		})
	}
}

func TestBuildHookSentinelDoc_PopulatesLifecyclePointAndStartedAt(t *testing.T) {
	dir := t.TempDir()
	fixed := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	oldNow := hookSentinelNow
	hookSentinelNow = func() time.Time { return fixed }
	t.Cleanup(func() { hookSentinelNow = oldNow })

	doc, err := buildHookSentinelDoc(dir, hookSentinelWriteInputs{
		Skill: "isp", RunID: "r1", PlanID: "p", TaskID: "t", AgentType: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	if doc.LifecyclePoint != "skill_entry" {
		t.Errorf("LifecyclePoint = %q, want skill_entry", doc.LifecyclePoint)
	}
	if !strings.HasPrefix(doc.StartedAt, "2026-05-26T12:00:00") {
		t.Errorf("StartedAt = %q, want 2026-05-26T12:00:00*", doc.StartedAt)
	}
}

func TestBuildHookSentinelDoc_OptionalContextOmittedWhenEmpty(t *testing.T) {
	dir := t.TempDir() // not a git repo → rev-parse fails → git_head_at_start stays empty
	doc, err := buildHookSentinelDoc(dir, hookSentinelWriteInputs{
		Skill: "isp", RunID: "r1", PlanID: "p", TaskID: "t", AgentType: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Context != nil {
		t.Errorf("expected nil context when no signals provided and no git head; got %+v", doc.Context)
	}
}

func TestBuildHookSentinelDoc_CarriesAllContextSignals(t *testing.T) {
	dir := t.TempDir()
	yes := true
	batch := 3
	doc, err := buildHookSentinelDoc(dir, hookSentinelWriteInputs{
		Skill: "isp", RunID: "r1", PlanID: "p", TaskID: "t", AgentType: "main",
		WriteScope:             []string{"commands/"},
		EligibleSnapshotLoaded: &yes,
		MaxBatch:               &batch,
		TracePathHint:          "/tmp/trace.jsonl",
	})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Context == nil {
		t.Fatal("expected non-nil context")
	}
	if doc.Context.EligibleSnapshotLoaded == nil || *doc.Context.EligibleSnapshotLoaded != true {
		t.Errorf("EligibleSnapshotLoaded = %v", doc.Context.EligibleSnapshotLoaded)
	}
	if doc.Context.MaxBatch == nil || *doc.Context.MaxBatch != 3 {
		t.Errorf("MaxBatch = %v", doc.Context.MaxBatch)
	}
	if doc.Context.TracePathHint != "/tmp/trace.jsonl" {
		t.Errorf("TracePathHint = %q", doc.Context.TracePathHint)
	}
	if len(doc.Context.WriteScope) != 1 || doc.Context.WriteScope[0] != "commands/" {
		t.Errorf("WriteScope = %v", doc.Context.WriteScope)
	}
}

// ── CLI handler integration ──────────────────────────────────────────────────

func TestRunHookSentinelRead_LatestAndRunIDMutuallyExclusive(t *testing.T) {
	withTempCwd(t, t.TempDir())
	err := runHookSentinelRead("loop-worker", "r1", true, false)
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("expected mutual-exclusion error, got %v", err)
	}
}

func TestRunHookSentinelRead_RequiresOneSelector(t *testing.T) {
	withTempCwd(t, t.TempDir())
	err := runHookSentinelRead("loop-worker", "", false, false)
	if err == nil || !strings.Contains(err.Error(), "--run-id or --latest") {
		t.Errorf("expected selector-required error, got %v", err)
	}
}

func TestRunHookSentinelRead_InvalidSkill(t *testing.T) {
	withTempCwd(t, t.TempDir())
	err := runHookSentinelRead("bogus", "r1", false, false)
	if err == nil || !strings.Contains(err.Error(), "invalid skill") {
		t.Errorf("expected invalid-skill error, got %v", err)
	}
}

// TestWorkflowHookSentinelCLI_EndToEnd exercises write → read --latest → clear
// through the cobra subtree to prove the registration in newWorkflowCmd is
// reachable. Captures stdout for the JSON contract check on `write`.
func TestWorkflowHookSentinelCLI_EndToEnd(t *testing.T) {
	dir := t.TempDir()
	withTempCwd(t, dir)

	// write
	out := captureStdoutToString(t, func() {
		err := executeWorkflowCommand(t, dir,
			"hook-sentinel", "write", "loop-worker",
			"--run-id", "r1",
			"--plan", "loop-discipline-stop-hooks",
			"--task", "p0-sentinel-cli",
			"--agent-type", "loop-worker",
			"--write-scope", "commands/workflow/",
		)
		if err != nil {
			t.Fatalf("write: %v", err)
		}
	})
	if !strings.Contains(out, "wrote hook sentinel") {
		t.Errorf("expected human stdout for write, got %q", out)
	}
	active := filepath.Join(dir, ".agents", "active", "hook-sentinels", "loop-worker-r1.json")
	if _, err := os.Stat(active); err != nil {
		t.Fatalf("expected active record at %s: %v", active, err)
	}

	// read --latest
	if err := executeWorkflowCommand(t, dir,
		"hook-sentinel", "read", "loop-worker", "--latest",
	); err != nil {
		t.Fatalf("read: %v", err)
	}

	// clear archives the record
	if err := executeWorkflowCommand(t, dir,
		"hook-sentinel", "clear", "loop-worker", "--run-id", "r1",
	); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, err := os.Stat(active); err == nil {
		t.Errorf("active record should be archived: %s", active)
	}
}

// TestWorkflowHookSentinelCLI_WriteJSON exercises the --json output path.
func TestWorkflowHookSentinelCLI_WriteJSON(t *testing.T) {
	dir := t.TempDir()
	withTempCwd(t, dir)

	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = false })

	out := captureStdoutToString(t, func() {
		err := executeWorkflowCommand(t, dir,
			"hook-sentinel", "write", "iteration-close",
			"--run-id", "r-json",
			"--plan", "plan-1",
			"--task", "t1",
			"--agent-type", "main",
		)
		if err != nil {
			t.Fatalf("write: %v", err)
		}
	})
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("expected JSON payload on --json write, got %q: %v", out, err)
	}
	if payload["status"] != "written" || payload["skill"] != "iteration-close" {
		t.Errorf("unexpected JSON payload: %+v", payload)
	}
}

// withTempCwd chdirs to dir and restores the original cwd on cleanup.
func withTempCwd(t *testing.T, dir string) {
	t.Helper()
	oldwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(oldwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
}

// TestWorkflowHookSentinelEmbeddedSchemaMatchesCanonical mirrors the iter-log
// dual-write guard: the embedded twin under commands/workflow/static/ must be
// byte-equal to schemas/<name>.schema.json. Drift between them is invisible
// at runtime — the Go binary reads the embedded copy while editors and
// external tooling read the canonical one.
func TestWorkflowHookSentinelEmbeddedSchemaMatchesCanonical(t *testing.T) {
	root := dotAgentsRepoRoot(t)
	want, err := os.ReadFile(filepath.Join(root, "schemas", "workflow-hook-sentinel.schema.json"))
	if err != nil {
		t.Fatalf("read canonical schema: %v", err)
	}
	if string(want) != string(workflowHookSentinelSchemaJSON) {
		t.Fatal("commands/workflow/static/workflow-hook-sentinel.schema.json is out of sync with schemas/workflow-hook-sentinel.schema.json — copy the canonical file after editing")
	}
}
