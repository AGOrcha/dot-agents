package workflow

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

// ── Schema compile/validation tests ──────────────────────────────────────────
// (TestCompiledWorkflowHookOutcomeSchema lives in workflow_hook_outcome_schema_test.go
// — owned by the schema-validator-wiring task, not duplicated here.)

// fakeHookOutcomeDeps is the interface-DI test double for hookOutcomeDeps
// (mirrors commands/review_test.go's fakeReviewDeps). A nil field delegates
// to stdHookOutcomeDeps so a test overrides only the operation it wants to
// fault-inject. Per t.Cleanup contract: rebind fields between tests rather
// than relying on package-level mutation.
type fakeHookOutcomeDeps struct {
	now            func() time.Time
	readFile       func(string) ([]byte, error)
	readDir        func(string) ([]os.DirEntry, error)
	rename         func(string, string) error
	remove         func(string) error
	resolveProject func() (workflowProjectRef, error)
}

func (f fakeHookOutcomeDeps) Now() time.Time {
	if f.now != nil {
		return f.now()
	}
	return stdHookOutcomeDeps{}.Now()
}

func (f fakeHookOutcomeDeps) ReadFile(name string) ([]byte, error) {
	if f.readFile != nil {
		return f.readFile(name)
	}
	return stdHookOutcomeDeps{}.ReadFile(name)
}

func (f fakeHookOutcomeDeps) ReadDir(name string) ([]os.DirEntry, error) {
	if f.readDir != nil {
		return f.readDir(name)
	}
	return stdHookOutcomeDeps{}.ReadDir(name)
}

func (f fakeHookOutcomeDeps) Rename(o, n string) error {
	if f.rename != nil {
		return f.rename(o, n)
	}
	return stdHookOutcomeDeps{}.Rename(o, n)
}

func (f fakeHookOutcomeDeps) Remove(name string) error {
	if f.remove != nil {
		return f.remove(name)
	}
	return stdHookOutcomeDeps{}.Remove(name)
}

func (f fakeHookOutcomeDeps) ResolveProject() (workflowProjectRef, error) {
	if f.resolveProject != nil {
		return f.resolveProject()
	}
	return stdHookOutcomeDeps{}.ResolveProject()
}

// TestFakeHookOutcomeDeps_NilDelegatesToReal pins the nil-delegates-to-real
// contract for every method of the fake. Without this, a future change to a
// default branch could silently regress every happy-path-but-not-overridden
// test without any of them failing.
func TestFakeHookOutcomeDeps_NilDelegatesToReal(t *testing.T) {
	f := fakeHookOutcomeDeps{}
	if got := f.Now(); got.IsZero() {
		t.Error("nil-now should delegate to real time.Now (got zero)")
	}
	tmp := t.TempDir()
	_, err := f.ReadFile(filepath.Join(tmp, "absent"))
	if err == nil || !os.IsNotExist(err) {
		t.Errorf("nil-readFile delegate should yield IsNotExist, got %v", err)
	}
	_, err = f.ReadDir(filepath.Join(tmp, "absent"))
	if err == nil || !os.IsNotExist(err) {
		t.Errorf("nil-readDir delegate should yield IsNotExist, got %v", err)
	}
	// Rename / Remove / ResolveProject default branches are exercised
	// transitively by every test that uses zero-value fakeHookOutcomeDeps;
	// just touch them here so the method is addressable.
	_ = f.Rename
	_ = f.Remove
	_ = f.ResolveProject
}

func newValidHookOutcomeRecord() HookOutcomeRecord {
	return HookOutcomeRecord{
		SchemaVersion:        1,
		SentinelID:           "iteration-close-r1",
		Skill:                "iteration-close",
		LifecyclePoint:       "stop",
		InterventionClass:    "remediate_at_stop",
		Result:               "remediate",
		RuleID:               "iteration-close.R1.1",
		Platform:             "claude",
		TS:                   "2026-05-26T12:00:00Z",
		ArchivedSentinelPath: ".agents/history/p/hook-sentinels/2026-05-26/iteration-close-r1.json",
		CorrelationID:        "iteration-close-r1",
	}
}

func newValidHookOutcomeSidecar() *HookOutcomeSidecar {
	return &HookOutcomeSidecar{
		SchemaVersion: 1,
		Records:       []HookOutcomeRecord{newValidHookOutcomeRecord()},
	}
}

func TestValidateHookOutcomeSidecar_Valid(t *testing.T) {
	if err := validateHookOutcomeSidecar(newValidHookOutcomeSidecar()); err != nil {
		t.Fatalf("valid sidecar rejected: %v", err)
	}
}

func TestValidateHookOutcomeSidecar_Nil(t *testing.T) {
	if err := validateHookOutcomeSidecar(nil); err == nil {
		t.Error("expected error for nil sidecar")
	}
}

func TestValidateHookOutcomeSidecar_BadSkillEnum(t *testing.T) {
	sc := newValidHookOutcomeSidecar()
	sc.Records[0].Skill = "rogue"
	if err := validateHookOutcomeSidecar(sc); err == nil {
		t.Error("expected schema rejection for unknown skill enum value")
	}
}

func TestValidateHookOutcomeSidecar_BadLifecyclePointEnum(t *testing.T) {
	sc := newValidHookOutcomeSidecar()
	sc.Records[0].LifecyclePoint = "made-up"
	if err := validateHookOutcomeSidecar(sc); err == nil {
		t.Error("expected schema rejection for unknown lifecycle_point enum value")
	}
}

func TestValidateHookOutcomeSidecar_BadInterventionClassEnum(t *testing.T) {
	sc := newValidHookOutcomeSidecar()
	sc.Records[0].InterventionClass = "wat"
	if err := validateHookOutcomeSidecar(sc); err == nil {
		t.Error("expected schema rejection for unknown intervention_class enum value")
	}
}

func TestValidateHookOutcomeSidecar_BadResultEnum(t *testing.T) {
	sc := newValidHookOutcomeSidecar()
	sc.Records[0].Result = "OK"
	if err := validateHookOutcomeSidecar(sc); err == nil {
		t.Error("expected schema rejection for unknown result enum value")
	}
}

func TestValidateHookOutcomeSidecar_BadPlatformEnum(t *testing.T) {
	sc := newValidHookOutcomeSidecar()
	sc.Records[0].Platform = "gemini"
	if err := validateHookOutcomeSidecar(sc); err == nil {
		t.Error("expected schema rejection for unknown platform enum value")
	}
}

func TestValidateHookOutcomeSidecar_BadRuleIDPattern(t *testing.T) {
	sc := newValidHookOutcomeSidecar()
	sc.Records[0].RuleID = "Has Spaces"
	if err := validateHookOutcomeSidecar(sc); err == nil {
		t.Error("expected schema rejection for rule_id pattern mismatch")
	}
}

func TestValidateHookOutcomeSidecar_BadTSFormat(t *testing.T) {
	sc := newValidHookOutcomeSidecar()
	sc.Records[0].TS = "yesterday"
	if err := validateHookOutcomeSidecar(sc); err == nil {
		t.Error("expected schema rejection for malformed ts")
	}
}

func TestValidateHookOutcomeSidecar_MissingRequiredField(t *testing.T) {
	sc := newValidHookOutcomeSidecar()
	sc.Records[0].SentinelID = ""
	if err := validateHookOutcomeSidecar(sc); err == nil {
		t.Error("expected schema rejection for empty sentinel_id (minLength 1)")
	}
}

// ── Enum / pattern guards ────────────────────────────────────────────────────

func TestValidHookOutcomeSkill(t *testing.T) {
	for _, s := range []string{
		"iteration-close", "isp", "loop-worker",
		"orchestrator-session-start", "delegation-lifecycle",
	} {
		if !validHookOutcomeSkill(s) {
			t.Errorf("expected %q valid", s)
		}
	}
	for _, s := range []string{"", "ISP", "loop_worker", "other"} {
		if validHookOutcomeSkill(s) {
			t.Errorf("expected %q invalid", s)
		}
	}
}

func TestValidHookOutcomeLifecyclePoint(t *testing.T) {
	for _, s := range []string{"pre_tool_use", "stop", "subagent_stop", "subagent_start", "pre_compact", "post_tool_use", "post_tool_use_failure"} {
		if !validHookOutcomeLifecyclePoint(s) {
			t.Errorf("expected %q valid", s)
		}
	}
	if validHookOutcomeLifecyclePoint("PreToolUse") {
		t.Error("expected camelCase variant to be invalid")
	}
}

func TestValidHookOutcomeInterventionClass(t *testing.T) {
	for _, s := range []string{"prevent_before_action", "remediate_at_stop", "continuity_advice", "observe_tool_result"} {
		if !validHookOutcomeInterventionClass(s) {
			t.Errorf("expected %q valid", s)
		}
	}
	if validHookOutcomeInterventionClass("remediate") {
		t.Error("expected bare result enum value to be invalid as intervention class")
	}
}

func TestValidHookOutcomeResult(t *testing.T) {
	for _, s := range []string{"allow", "advise", "remediate"} {
		if !validHookOutcomeResult(s) {
			t.Errorf("expected %q valid", s)
		}
	}
	if validHookOutcomeResult("ok") {
		t.Error("expected ok to be invalid")
	}
}

func TestValidHookOutcomePlatform(t *testing.T) {
	for _, s := range []string{"claude", "codex", "copilot", "cursor"} {
		if !validHookOutcomePlatform(s) {
			t.Errorf("expected %q valid", s)
		}
	}
	if validHookOutcomePlatform("Claude") {
		t.Error("expected CamelCase variant to be invalid")
	}
}

func TestValidHookOutcomeRuleID(t *testing.T) {
	for _, s := range []string{
		"iteration-close.R1.1",
		"loop-worker.R3.1",
		"orchestrator-handoff.R3.3",
		"a.b.c.d",
	} {
		if !validHookOutcomeRuleID(s) {
			t.Errorf("expected %q valid", s)
		}
	}
	for _, s := range []string{
		"",
		"NoDot",
		"Spaces In Name",
		"Uppercase.Prefix.Bad",
		".leading.dot",
		"trailing.dot.",
	} {
		if validHookOutcomeRuleID(s) {
			t.Errorf("expected %q invalid", s)
		}
	}
}

// ── buildHookOutcomeRecord tests ─────────────────────────────────────────────

func validHookOutcomeWriteInputs() hookOutcomeWriteInputs {
	return hookOutcomeWriteInputs{
		SentinelID:           "iteration-close-r1",
		Skill:                "iteration-close",
		LifecyclePoint:       "stop",
		InterventionClass:    "remediate_at_stop",
		Result:               "remediate",
		RuleID:               "iteration-close.R1.1",
		Platform:             "claude",
		CorrelationID:        "",
		ArchivedSentinelPath: "",
		TS:                   "",
	}
}

func TestBuildHookOutcomeRecord_Defaults(t *testing.T) {
	fixed := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	deps := fakeHookOutcomeDeps{now: func() time.Time { return fixed }}
	rec, err := buildHookOutcomeRecord(deps, validHookOutcomeWriteInputs())
	if err != nil {
		t.Fatalf("buildHookOutcomeRecord: %v", err)
	}
	if rec.SchemaVersion != HookOutcomeSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", rec.SchemaVersion, HookOutcomeSchemaVersion)
	}
	if rec.CorrelationID != rec.SentinelID {
		t.Errorf("CorrelationID = %q, want default to sentinel_id %q", rec.CorrelationID, rec.SentinelID)
	}
	if rec.TS != "2026-05-26T12:00:00Z" {
		t.Errorf("TS = %q, want default to now()=2026-05-26T12:00:00Z", rec.TS)
	}
}

func TestBuildHookOutcomeRecord_MissingSentinelID(t *testing.T) {
	in := validHookOutcomeWriteInputs()
	in.SentinelID = "   "
	_, err := buildHookOutcomeRecord(stdHookOutcomeDeps{}, in)
	if err == nil || !strings.Contains(err.Error(), "sentinel-id is required") {
		t.Errorf("expected sentinel-id error, got %v", err)
	}
}

func TestBuildHookOutcomeRecord_BadSkill(t *testing.T) {
	in := validHookOutcomeWriteInputs()
	in.Skill = "made-up"
	_, err := buildHookOutcomeRecord(stdHookOutcomeDeps{}, in)
	if err == nil || !strings.Contains(err.Error(), "--skill must be one of") {
		t.Errorf("expected --skill enum error, got %v", err)
	}
}

func TestBuildHookOutcomeRecord_BadLifecyclePoint(t *testing.T) {
	in := validHookOutcomeWriteInputs()
	in.LifecyclePoint = "PreToolUse" // camelCase variant; schema is snake_case
	_, err := buildHookOutcomeRecord(stdHookOutcomeDeps{}, in)
	if err == nil || !strings.Contains(err.Error(), "--lifecycle-point must be one of") {
		t.Errorf("expected --lifecycle-point enum error, got %v", err)
	}
}

func TestBuildHookOutcomeRecord_BadInterventionClass(t *testing.T) {
	in := validHookOutcomeWriteInputs()
	in.InterventionClass = "block"
	_, err := buildHookOutcomeRecord(stdHookOutcomeDeps{}, in)
	if err == nil || !strings.Contains(err.Error(), "--intervention-class must be one of") {
		t.Errorf("expected --intervention-class enum error, got %v", err)
	}
}

func TestBuildHookOutcomeRecord_BadResult(t *testing.T) {
	in := validHookOutcomeWriteInputs()
	in.Result = "deny"
	_, err := buildHookOutcomeRecord(stdHookOutcomeDeps{}, in)
	if err == nil || !strings.Contains(err.Error(), "--result must be one of") {
		t.Errorf("expected --result enum error, got %v", err)
	}
}

func TestBuildHookOutcomeRecord_BadRuleID(t *testing.T) {
	in := validHookOutcomeWriteInputs()
	in.RuleID = "NoSegment"
	_, err := buildHookOutcomeRecord(stdHookOutcomeDeps{}, in)
	if err == nil || !strings.Contains(err.Error(), "--rule-id") {
		t.Errorf("expected --rule-id pattern error, got %v", err)
	}
}

func TestBuildHookOutcomeRecord_BadPlatform(t *testing.T) {
	in := validHookOutcomeWriteInputs()
	in.Platform = "Gemini"
	_, err := buildHookOutcomeRecord(stdHookOutcomeDeps{}, in)
	if err == nil || !strings.Contains(err.Error(), "--platform must be one of") {
		t.Errorf("expected --platform enum error, got %v", err)
	}
}

func TestBuildHookOutcomeRecord_CustomTSAndCorrelation(t *testing.T) {
	in := validHookOutcomeWriteInputs()
	in.TS = "2026-05-26T13:00:00Z"
	in.CorrelationID = "merged-intent-1"
	rec, err := buildHookOutcomeRecord(stdHookOutcomeDeps{}, in)
	if err != nil {
		t.Fatalf("buildHookOutcomeRecord: %v", err)
	}
	if rec.TS != in.TS {
		t.Errorf("TS = %q, want preserved %q", rec.TS, in.TS)
	}
	if rec.CorrelationID != in.CorrelationID {
		t.Errorf("CorrelationID = %q, want %q (caller-supplied wins)", rec.CorrelationID, in.CorrelationID)
	}
}

// ── Idempotency-key tests ────────────────────────────────────────────────────

func TestHookOutcomeIdempotencyKeyMatches_AllFour(t *testing.T) {
	a := newValidHookOutcomeRecord()
	b := newValidHookOutcomeRecord()
	b.TS = "2099-01-01T00:00:00Z" // outside the key
	b.Result = "allow"            // outside the key
	if !hookOutcomeIdempotencyKeyMatches(a, b) {
		t.Error("expected match on the four-tuple key (sentinel_id, rule_id, lifecycle_point, intervention_class)")
	}
}

func TestHookOutcomeIdempotencyKeyMatches_DiffSentinel(t *testing.T) {
	a := newValidHookOutcomeRecord()
	b := newValidHookOutcomeRecord()
	b.SentinelID = "iteration-close-r2"
	if hookOutcomeIdempotencyKeyMatches(a, b) {
		t.Error("expected no match when sentinel_id differs")
	}
}

func TestHookOutcomeIdempotencyKeyMatches_DiffRule(t *testing.T) {
	a := newValidHookOutcomeRecord()
	b := newValidHookOutcomeRecord()
	b.RuleID = "iteration-close.R1.2"
	if hookOutcomeIdempotencyKeyMatches(a, b) {
		t.Error("expected no match when rule_id differs")
	}
}

func TestHookOutcomeIdempotencyKeyMatches_DiffLifecycle(t *testing.T) {
	a := newValidHookOutcomeRecord()
	b := newValidHookOutcomeRecord()
	b.LifecyclePoint = "subagent_stop"
	if hookOutcomeIdempotencyKeyMatches(a, b) {
		t.Error("expected no match when lifecycle_point differs")
	}
}

func TestHookOutcomeIdempotencyKeyMatches_DiffIntervention(t *testing.T) {
	a := newValidHookOutcomeRecord()
	b := newValidHookOutcomeRecord()
	b.InterventionClass = "prevent_before_action"
	if hookOutcomeIdempotencyKeyMatches(a, b) {
		t.Error("expected no match when intervention_class differs")
	}
}

// ── resolveActiveIterationN tests ────────────────────────────────────────────

func TestResolveActiveIterationN_NoIterDir(t *testing.T) {
	dir := t.TempDir()
	n, active, err := resolveActiveIterationN(stdHookOutcomeDeps{}, dir)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if active {
		t.Error("expected active=false when iter-log dir is missing")
	}
	if n != 0 {
		t.Errorf("expected n=0 when no iteration, got %d", n)
	}
}

func TestResolveActiveIterationN_EmptyIterDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".agents", "active", "iteration-log"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	_, active, err := resolveActiveIterationN(stdHookOutcomeDeps{}, dir)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if active {
		t.Error("expected active=false when iter-log dir exists but is empty")
	}
}

func TestResolveActiveIterationN_PicksMax(t *testing.T) {
	dir := t.TempDir()
	iterDir := filepath.Join(dir, ".agents", "active", "iteration-log")
	if err := os.MkdirAll(iterDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, name := range []string{"iter-1.yaml", "iter-2.yaml", "iter-3.yaml", "iter-3.score.yaml", "not-an-iter.yaml"} {
		if err := os.WriteFile(filepath.Join(iterDir, name), []byte("schema_version: 2\n"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	n, active, err := resolveActiveIterationN(stdHookOutcomeDeps{}, dir)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !active {
		t.Fatal("expected active=true")
	}
	if n != 3 {
		t.Errorf("expected n=3 (max of iter-1, iter-2, iter-3), got %d (score sidecar must not count)", n)
	}
}

func TestResolveActiveIterationN_SkipsDirsAndNonMatching(t *testing.T) {
	dir := t.TempDir()
	iterDir := filepath.Join(dir, ".agents", "active", "iteration-log")
	if err := os.MkdirAll(iterDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// A subdirectory under iter-log: must be skipped.
	if err := os.Mkdir(filepath.Join(iterDir, "iter-99-subdir"), 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	// A non-numeric iter-* file: regex matches but Sscanf rejects it.
	// (Using a non-iter-* file is also fine, covered by the regex no-match branch.)
	if err := os.WriteFile(filepath.Join(iterDir, "iter-2.yaml"), []byte("schema_version: 2\n"), 0o644); err != nil {
		t.Fatalf("write iter-2: %v", err)
	}
	if err := os.WriteFile(filepath.Join(iterDir, "stray-file.txt"), []byte("noise"), 0o644); err != nil {
		t.Fatalf("write stray: %v", err)
	}
	n, active, err := resolveActiveIterationN(stdHookOutcomeDeps{}, dir)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !active || n != 2 {
		t.Errorf("expected (n=2, active=true), got (n=%d, active=%v)", n, active)
	}
}

func TestLoadHookOutcomeSidecar_NilRecordsNormalizedToEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "iter-1.hook-outcomes.yaml")
	// Sidecar with explicit schema_version but no records key at all.
	if err := os.WriteFile(path, []byte("schema_version: 1\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := loadHookOutcomeSidecar(stdHookOutcomeDeps{}, path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Records == nil {
		t.Error("expected normalized empty slice, got nil")
	}
	if len(got.Records) != 0 {
		t.Errorf("expected 0 records, got %d", len(got.Records))
	}
}

func TestResolveActiveIterationN_ReadDirError(t *testing.T) {
	deps := fakeHookOutcomeDeps{readDir: func(string) ([]os.DirEntry, error) {
		return nil, errors.New("synthetic readdir fault")
	}}
	_, _, err := resolveActiveIterationN(deps, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "read iteration-log dir") {
		t.Errorf("expected wrapped readdir error, got %v", err)
	}
}

// ── loadHookOutcomeSidecar tests ─────────────────────────────────────────────

func TestLoadHookOutcomeSidecar_MissingFileYieldsEmpty(t *testing.T) {
	sc, err := loadHookOutcomeSidecar(stdHookOutcomeDeps{}, filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if sc == nil {
		t.Fatal("expected non-nil empty sidecar")
	}
	if sc.SchemaVersion != HookOutcomeSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", sc.SchemaVersion, HookOutcomeSchemaVersion)
	}
	if len(sc.Records) != 0 {
		t.Errorf("expected empty records slice, got %d", len(sc.Records))
	}
}

func TestLoadHookOutcomeSidecar_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "iter-1.hook-outcomes.yaml")
	orig := newValidHookOutcomeSidecar()
	body, err := yaml.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := loadHookOutcomeSidecar(stdHookOutcomeDeps{}, path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got.Records) != 1 || got.Records[0].SentinelID != orig.Records[0].SentinelID {
		t.Errorf("round-trip records mismatch: got %+v", got.Records)
	}
}

func TestLoadHookOutcomeSidecar_BadYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "iter-1.hook-outcomes.yaml")
	if err := os.WriteFile(path, []byte("not: : valid: : yaml"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := loadHookOutcomeSidecar(stdHookOutcomeDeps{}, path); err == nil {
		t.Error("expected parse error for malformed YAML")
	}
}

func TestLoadHookOutcomeSidecar_MissingSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "iter-1.hook-outcomes.yaml")
	if err := os.WriteFile(path, []byte("records: []\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := loadHookOutcomeSidecar(stdHookOutcomeDeps{}, path)
	if err == nil || !strings.Contains(err.Error(), "missing schema_version") {
		t.Errorf("expected missing schema_version error, got %v", err)
	}
}

func TestLoadHookOutcomeSidecar_UnsupportedSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "iter-1.hook-outcomes.yaml")
	if err := os.WriteFile(path, []byte("schema_version: 999\nrecords: []\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := loadHookOutcomeSidecar(stdHookOutcomeDeps{}, path)
	if err == nil || !strings.Contains(err.Error(), "unsupported schema_version") {
		t.Errorf("expected unsupported schema_version error, got %v", err)
	}
}

// ── appendHookOutcome end-to-end tests ───────────────────────────────────────

func setupProjectWithIter(t *testing.T, n int) string {
	t.Helper()
	dir := t.TempDir()
	iterDir := filepath.Join(dir, ".agents", "active", "iteration-log")
	if err := os.MkdirAll(iterDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(iterDir, fmt.Sprintf("iter-%d.yaml", n)), []byte("schema_version: 2\niteration: 1\n"), 0o644); err != nil {
		t.Fatalf("write iter file: %v", err)
	}
	return dir
}

func TestAppendHookOutcome_WritesNewSidecar(t *testing.T) {
	dir := setupProjectWithIter(t, 1)
	rec := newValidHookOutcomeRecord()
	res, err := appendHookOutcome(stdHookOutcomeDeps{}, dir, rec)
	if err != nil {
		t.Fatalf("appendHookOutcome: %v", err)
	}
	if res.Status != "written" {
		t.Errorf("Status = %q, want written", res.Status)
	}
	if res.Iteration != 1 {
		t.Errorf("Iteration = %d, want 1", res.Iteration)
	}
	wantPath := filepath.Join(dir, ".agents", "active", "iteration-log", "iter-1.hook-outcomes.yaml")
	if res.Path != wantPath {
		t.Errorf("Path = %q, want %q", res.Path, wantPath)
	}
	body, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("read sidecar: %v", err)
	}
	if !strings.Contains(string(body), "iteration-close.R1.1") {
		t.Error("sidecar missing the appended rule_id")
	}
	if !strings.HasPrefix(string(body), "# yaml-language-server:") {
		t.Error("sidecar missing yaml-language-server header")
	}
}

func TestAppendHookOutcome_NoActiveIterationIsSilentExit(t *testing.T) {
	// Empty project: no iter-log dir at all.
	dir := t.TempDir()
	res, err := appendHookOutcome(stdHookOutcomeDeps{}, dir, newValidHookOutcomeRecord())
	if err != nil {
		t.Fatalf("appendHookOutcome: %v", err)
	}
	if res.Status != "no-active-iteration" {
		t.Errorf("Status = %q, want no-active-iteration", res.Status)
	}
	if res.Path != "" {
		t.Errorf("expected empty Path on no-active-iteration, got %q", res.Path)
	}
}

func TestAppendHookOutcome_IdempotentDuplicate(t *testing.T) {
	dir := setupProjectWithIter(t, 1)
	rec := newValidHookOutcomeRecord()
	if _, err := appendHookOutcome(stdHookOutcomeDeps{}, dir, rec); err != nil {
		t.Fatalf("first append: %v", err)
	}
	// Second write with same idempotency-key tuple but a different ts/result:
	// must be a no-op duplicate.
	dup := rec
	dup.TS = "2099-01-01T00:00:00Z"
	dup.Result = "allow"
	res, err := appendHookOutcome(stdHookOutcomeDeps{}, dir, dup)
	if err != nil {
		t.Fatalf("second append: %v", err)
	}
	if res.Status != "duplicate" {
		t.Errorf("Status = %q, want duplicate", res.Status)
	}
	// Sidecar must still hold exactly one record, with the ORIGINAL ts/result.
	sc, err := loadHookOutcomeSidecar(stdHookOutcomeDeps{}, res.Path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(sc.Records) != 1 {
		t.Fatalf("expected 1 record after duplicate, got %d", len(sc.Records))
	}
	if sc.Records[0].TS == "2099-01-01T00:00:00Z" {
		t.Error("expected original ts preserved on duplicate write (append-only invariant)")
	}
	if sc.Records[0].Result != "remediate" {
		t.Errorf("expected original result preserved, got %q", sc.Records[0].Result)
	}
}

func TestAppendHookOutcome_DifferentRuleIDAppends(t *testing.T) {
	dir := setupProjectWithIter(t, 1)
	first := newValidHookOutcomeRecord()
	if _, err := appendHookOutcome(stdHookOutcomeDeps{}, dir, first); err != nil {
		t.Fatalf("first: %v", err)
	}
	second := newValidHookOutcomeRecord()
	second.RuleID = "iteration-close.R1.2"
	res, err := appendHookOutcome(stdHookOutcomeDeps{}, dir, second)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if res.Status != "written" {
		t.Errorf("Status = %q, want written (different rule_id = different key)", res.Status)
	}
	sc, err := loadHookOutcomeSidecar(stdHookOutcomeDeps{}, res.Path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(sc.Records) != 2 {
		t.Errorf("expected 2 records, got %d", len(sc.Records))
	}
}

func TestAppendHookOutcome_TargetsLatestIteration(t *testing.T) {
	dir := setupProjectWithIter(t, 1)
	iterDir := filepath.Join(dir, ".agents", "active", "iteration-log")
	// Add iter-2 and iter-3 so the latest is 3.
	for _, n := range []int{2, 3} {
		if err := os.WriteFile(filepath.Join(iterDir, fmt.Sprintf("iter-%d.yaml", n)), []byte("schema_version: 2\n"), 0o644); err != nil {
			t.Fatalf("write iter-%d: %v", n, err)
		}
	}
	res, err := appendHookOutcome(stdHookOutcomeDeps{}, dir, newValidHookOutcomeRecord())
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if res.Iteration != 3 {
		t.Errorf("Iteration = %d, want 3 (latest)", res.Iteration)
	}
	if !strings.HasSuffix(res.Path, "iter-3.hook-outcomes.yaml") {
		t.Errorf("Path %q does not target latest iter-3 sidecar", res.Path)
	}
}

func TestAppendHookOutcome_RenameError(t *testing.T) {
	dir := setupProjectWithIter(t, 1)
	deps := fakeHookOutcomeDeps{rename: func(string, string) error { return errors.New("synthetic rename fault") }}
	_, err := appendHookOutcome(deps, dir, newValidHookOutcomeRecord())
	if err == nil || !strings.Contains(err.Error(), "publish hook outcome sidecar") {
		t.Errorf("expected wrapped rename error, got %v", err)
	}
}

func TestAppendHookOutcome_ValidationRejectsDisallowedRecordShape(t *testing.T) {
	dir := setupProjectWithIter(t, 1)
	rec := newValidHookOutcomeRecord()
	rec.Skill = "rogue-skill" // schema enum rejection
	_, err := appendHookOutcome(stdHookOutcomeDeps{}, dir, rec)
	if err == nil || !strings.Contains(err.Error(), "workflow-hook-outcome.schema.json") {
		t.Errorf("expected schema rejection wrapped by validator, got %v", err)
	}
}

// ── hookOutcomeRecordKey readback ────────────────────────────────────────────

func TestHookOutcomeRecordKey(t *testing.T) {
	rec := newValidHookOutcomeRecord()
	got := hookOutcomeRecordKey(rec)
	want := "iteration-close-r1|iteration-close.R1.1|stop|remediate_at_stop"
	if got != want {
		t.Errorf("hookOutcomeRecordKey = %q, want %q", got, want)
	}
}

// ── writeHookOutcomeSidecar seam-injection tests ─────────────────────────────

func TestWriteHookOutcomeSidecar_ValidationFailsBeforeDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "iter-1.hook-outcomes.yaml")
	bad := &HookOutcomeSidecar{SchemaVersion: 1, Records: []HookOutcomeRecord{{SentinelID: "x"}}}
	if err := writeHookOutcomeSidecar(stdHookOutcomeDeps{}, path, bad); err == nil {
		t.Fatal("expected validation error for malformed record")
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Errorf("expected no file written on validation failure, stat err=%v", statErr)
	}
}

func TestWriteHookOutcomeSidecar_NilSidecar(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "iter-1.hook-outcomes.yaml")
	if err := writeHookOutcomeSidecar(stdHookOutcomeDeps{}, path, nil); err == nil {
		t.Fatal("expected error for nil sidecar")
	}
}

func TestWriteHookOutcomeSidecar_YamlMarshalError(t *testing.T) {
	prior := yamlMarshal
	yamlMarshal = func(any) ([]byte, error) { return nil, errors.New("synthetic marshal fault") }
	t.Cleanup(func() { yamlMarshal = prior })
	dir := t.TempDir()
	path := filepath.Join(dir, "iter-1.hook-outcomes.yaml")
	err := writeHookOutcomeSidecar(stdHookOutcomeDeps{}, path, newValidHookOutcomeSidecar())
	if err == nil || !strings.Contains(err.Error(), "marshal hook outcome sidecar") {
		t.Errorf("expected wrapped marshal error, got %v", err)
	}
}

func TestWriteHookOutcomeSidecar_MkdirError(t *testing.T) {
	prior := osMkdirAll
	osMkdirAll = func(string, os.FileMode) error { return errors.New("synthetic mkdir fault") }
	t.Cleanup(func() { osMkdirAll = prior })
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "iter-1.hook-outcomes.yaml")
	err := writeHookOutcomeSidecar(stdHookOutcomeDeps{}, path, newValidHookOutcomeSidecar())
	if err == nil || !strings.Contains(err.Error(), "prepare hook outcome dir") {
		t.Errorf("expected wrapped mkdir error, got %v", err)
	}
}

func TestWriteHookOutcomeSidecar_TempWriteError(t *testing.T) {
	prior := osWriteFile
	osWriteFile = func(string, []byte, os.FileMode) error { return errors.New("synthetic write fault") }
	t.Cleanup(func() { osWriteFile = prior })
	dir := t.TempDir()
	path := filepath.Join(dir, "iter-1.hook-outcomes.yaml")
	err := writeHookOutcomeSidecar(stdHookOutcomeDeps{}, path, newValidHookOutcomeSidecar())
	if err == nil || !strings.Contains(err.Error(), "write hook outcome temp") {
		t.Errorf("expected wrapped temp write error, got %v", err)
	}
}

func TestWriteHookOutcomeSidecar_RenameErrorCleansUpTemp(t *testing.T) {
	removeCalled := 0
	deps := fakeHookOutcomeDeps{
		rename: func(string, string) error { return errors.New("synthetic rename fault") },
		remove: func(name string) error {
			removeCalled++
			return os.Remove(name)
		},
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "iter-1.hook-outcomes.yaml")
	err := writeHookOutcomeSidecar(deps, path, newValidHookOutcomeSidecar())
	if err == nil || !strings.Contains(err.Error(), "publish hook outcome sidecar") {
		t.Errorf("expected wrapped rename error, got %v", err)
	}
	if removeCalled != 1 {
		t.Errorf("expected exactly one cleanup remove call, got %d", removeCalled)
	}
	if _, statErr := os.Stat(path + ".tmp"); !os.IsNotExist(statErr) {
		t.Errorf("expected temp file cleaned up after rename failure, stat err=%v", statErr)
	}
}

func TestLoadHookOutcomeSidecar_ReadErrorPropagates(t *testing.T) {
	deps := fakeHookOutcomeDeps{readFile: func(string) ([]byte, error) {
		return nil, errors.New("synthetic read fault")
	}}
	_, err := loadHookOutcomeSidecar(deps, filepath.Join(t.TempDir(), "iter-1.hook-outcomes.yaml"))
	if err == nil || !strings.Contains(err.Error(), "read hook outcome sidecar") {
		t.Errorf("expected wrapped non-IsNotExist read error, got %v", err)
	}
}

// ── appendHookOutcome error-propagation tests ────────────────────────────────

func TestAppendHookOutcome_ResolveIterationError(t *testing.T) {
	deps := fakeHookOutcomeDeps{readDir: func(string) ([]os.DirEntry, error) {
		return nil, errors.New("synthetic readdir fault")
	}}
	_, err := appendHookOutcome(deps, t.TempDir(), newValidHookOutcomeRecord())
	if err == nil || !strings.Contains(err.Error(), "read iteration-log dir") {
		t.Errorf("expected wrapped readdir error, got %v", err)
	}
}

func TestAppendHookOutcome_LoadSidecarError(t *testing.T) {
	dir := setupProjectWithIter(t, 1)
	deps := fakeHookOutcomeDeps{readFile: func(string) ([]byte, error) {
		return nil, errors.New("synthetic read fault")
	}}
	_, err := appendHookOutcome(deps, dir, newValidHookOutcomeRecord())
	if err == nil || !strings.Contains(err.Error(), "read hook outcome sidecar") {
		t.Errorf("expected wrapped sidecar read error, got %v", err)
	}
}

func TestAppendHookOutcome_WriteSidecarError(t *testing.T) {
	dir := setupProjectWithIter(t, 1)
	deps := fakeHookOutcomeDeps{rename: func(string, string) error { return errors.New("synthetic rename fault") }}
	_, err := appendHookOutcome(deps, dir, newValidHookOutcomeRecord())
	if err == nil || !strings.Contains(err.Error(), "publish hook outcome sidecar") {
		t.Errorf("expected wrapped publish error, got %v", err)
	}
}

func TestValidateHookOutcomeSidecar_JSONMarshalError(t *testing.T) {
	prior := jsonMarshal
	jsonMarshal = func(any) ([]byte, error) { return nil, errors.New("synthetic json marshal fault") }
	t.Cleanup(func() { jsonMarshal = prior })
	err := validateHookOutcomeSidecar(newValidHookOutcomeSidecar())
	if err == nil || !strings.Contains(err.Error(), "marshal hook outcome for schema validation") {
		t.Errorf("expected wrapped json-marshal error, got %v", err)
	}
}

// ── runHookOutcomeWrite end-to-end (CLI handler) tests ──────────────────────

func newValidHookOutcomeWriteInputs() hookOutcomeWriteInputs {
	return hookOutcomeWriteInputs{
		SentinelID:        "iteration-close-r1",
		Skill:             "iteration-close",
		LifecyclePoint:    "stop",
		InterventionClass: "remediate_at_stop",
		Result:            "remediate",
		RuleID:            "iteration-close.R1.1",
		Platform:          "claude",
		TS:                "2026-05-26T12:00:00Z",
		CorrelationID:     "iteration-close-r1",
	}
}

// hookOutcomeDepsForProject returns a fakeHookOutcomeDeps that resolves
// the workflow project to (name=p, path=path) without touching cwd.
// Tests pass the result directly to runHookOutcomeWrite.
func hookOutcomeDepsForProject(path string) fakeHookOutcomeDeps {
	return fakeHookOutcomeDeps{resolveProject: func() (workflowProjectRef, error) {
		return workflowProjectRef{Name: "p", Path: path}, nil
	}}
}

func captureStdoutString(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	old := os.Stdout
	os.Stdout = w
	fn()
	_ = w.Close()
	os.Stdout = old
	var buf strings.Builder
	b := make([]byte, 4096)
	for {
		n, err := r.Read(b)
		if n > 0 {
			buf.Write(b[:n])
		}
		if err != nil {
			break
		}
	}
	return buf.String()
}

func TestRunHookOutcomeWrite_WrittenText(t *testing.T) {
	dir := setupProjectWithIter(t, 3)
	d := hookOutcomeDepsForProject(dir)
	out := captureStdoutString(t, func() {
		if err := runHookOutcomeWrite(d, newValidHookOutcomeWriteInputs()); err != nil {
			t.Fatalf("runHookOutcomeWrite: %v", err)
		}
	})
	if !strings.Contains(out, "wrote hook outcome") || !strings.Contains(out, "iter-3") {
		t.Errorf("text output missing written marker / iter-3: %q", out)
	}
}

func TestRunHookOutcomeWrite_WrittenJSON(t *testing.T) {
	dir := setupProjectWithIter(t, 2)
	d := hookOutcomeDepsForProject(dir)
	prior := deps.Flags.JSON
	deps.Flags.JSON = func() bool { return true }
	t.Cleanup(func() { deps.Flags.JSON = prior })
	out := captureStdoutString(t, func() {
		if err := runHookOutcomeWrite(d, newValidHookOutcomeWriteInputs()); err != nil {
			t.Fatalf("runHookOutcomeWrite: %v", err)
		}
	})
	if !strings.Contains(out, `"status": "written"`) || !strings.Contains(out, `"iteration": 2`) {
		t.Errorf("JSON output missing written/iteration field: %q", out)
	}
}

func TestRunHookOutcomeWrite_DuplicateText(t *testing.T) {
	dir := setupProjectWithIter(t, 1)
	d := hookOutcomeDepsForProject(dir)
	if err := runHookOutcomeWrite(d, newValidHookOutcomeWriteInputs()); err != nil {
		t.Fatalf("first write: %v", err)
	}
	out := captureStdoutString(t, func() {
		if err := runHookOutcomeWrite(d, newValidHookOutcomeWriteInputs()); err != nil {
			t.Fatalf("second write: %v", err)
		}
	})
	if !strings.Contains(out, "duplicate hook outcome") {
		t.Errorf("expected duplicate marker, got %q", out)
	}
}

func TestRunHookOutcomeWrite_DuplicateJSON(t *testing.T) {
	dir := setupProjectWithIter(t, 1)
	d := hookOutcomeDepsForProject(dir)
	if err := runHookOutcomeWrite(d, newValidHookOutcomeWriteInputs()); err != nil {
		t.Fatalf("first: %v", err)
	}
	prior := deps.Flags.JSON
	deps.Flags.JSON = func() bool { return true }
	t.Cleanup(func() { deps.Flags.JSON = prior })
	out := captureStdoutString(t, func() {
		if err := runHookOutcomeWrite(d, newValidHookOutcomeWriteInputs()); err != nil {
			t.Fatalf("second: %v", err)
		}
	})
	if !strings.Contains(out, `"status": "duplicate"`) {
		t.Errorf("expected JSON duplicate status, got %q", out)
	}
}

func TestRunHookOutcomeWrite_NoActiveIterationText(t *testing.T) {
	// Empty project: no iter-log dir → stderr advisory, exit 0, no stdout body.
	dir := t.TempDir()
	d := hookOutcomeDepsForProject(dir)
	stderr := captureStderr(t, func() {
		if err := runHookOutcomeWrite(d, newValidHookOutcomeWriteInputs()); err != nil {
			t.Fatalf("runHookOutcomeWrite: %v", err)
		}
	})
	if !strings.Contains(stderr, "no active iteration") {
		t.Errorf("expected no-active-iteration stderr advisory, got %q", stderr)
	}
}

func TestRunHookOutcomeWrite_NoActiveIterationJSON(t *testing.T) {
	dir := t.TempDir()
	d := hookOutcomeDepsForProject(dir)
	prior := deps.Flags.JSON
	deps.Flags.JSON = func() bool { return true }
	t.Cleanup(func() { deps.Flags.JSON = prior })
	out := captureStdoutString(t, func() {
		if err := runHookOutcomeWrite(d, newValidHookOutcomeWriteInputs()); err != nil {
			t.Fatalf("runHookOutcomeWrite: %v", err)
		}
	})
	if !strings.Contains(out, `"status": "no-active-iteration"`) {
		t.Errorf("expected JSON no-active-iteration status, got %q", out)
	}
}

func TestRunHookOutcomeWrite_AppendErrorPropagates(t *testing.T) {
	dir := setupProjectWithIter(t, 1)
	d := hookOutcomeDepsForProject(dir)
	d.rename = func(string, string) error { return errors.New("synthetic publish fault") }
	err := runHookOutcomeWrite(d, newValidHookOutcomeWriteInputs())
	if err == nil || !strings.Contains(err.Error(), "publish hook outcome sidecar") {
		t.Errorf("expected wrapped publish error, got %v", err)
	}
}

func TestRunHookOutcomeWrite_ResolveProjectError(t *testing.T) {
	d := fakeHookOutcomeDeps{resolveProject: func() (workflowProjectRef, error) {
		return workflowProjectRef{}, errors.New("boom")
	}}
	if err := runHookOutcomeWrite(d, newValidHookOutcomeWriteInputs()); err == nil {
		t.Fatal("expected error from resolve project, got nil")
	}
}

func TestRunHookOutcomeWrite_BadInputBuildError(t *testing.T) {
	dir := setupProjectWithIter(t, 1)
	d := hookOutcomeDepsForProject(dir)
	bad := newValidHookOutcomeWriteInputs()
	bad.Skill = "not-a-real-skill"
	if err := runHookOutcomeWrite(d, bad); err == nil {
		t.Fatal("expected buildHookOutcomeRecord error, got nil")
	}
}

// ── hookOutcomeSidecarPath helper ────────────────────────────────────────────

func TestHookOutcomeSidecarPath(t *testing.T) {
	got := hookOutcomeSidecarPath("/proj", 7)
	want := filepath.Join("/proj", ".agents", "active", "iteration-log", "iter-7.hook-outcomes.yaml")
	if got != want {
		t.Errorf("hookOutcomeSidecarPath = %q, want %q", got, want)
	}
}
