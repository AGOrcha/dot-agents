package scoring

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func parseFixture(t *testing.T, name string) (IterationRecord, error) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return ParseIterationRecord(data)
}

func mustParse(t *testing.T, name string) IterationRecord {
	t.Helper()
	rec, err := parseFixture(t, name)
	if err != nil {
		t.Fatalf("ParseIterationRecord(%s) = %v, want nil", name, err)
	}
	return rec
}

func wantOptBool(t *testing.T, label string, got OptionalBool, want bool) {
	t.Helper()
	if !got.Set {
		t.Errorf("%s is unset, want %v", label, want)
		return
	}
	if got.Value != want {
		t.Errorf("%s = %v, want %v", label, got.Value, want)
	}
}

func TestParseV1(t *testing.T) {
	rec := mustParse(t, "v1_iter.yaml")

	if rec.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d, want 1", rec.SchemaVersion)
	}
	if rec.Iteration != 7 {
		t.Errorf("Iteration = %d, want 7", rec.Iteration)
	}
	if rec.Commit != "abc1234" {
		t.Errorf("Commit = %q, want abc1234", rec.Commit)
	}
	if rec.FilesChanged != 8 {
		t.Errorf("FilesChanged = %d, want 8", rec.FilesChanged)
	}
	if rec.Impl.ScopeNote != "on-target" {
		t.Errorf("Impl.ScopeNote = %q, want on-target", rec.Impl.ScopeNote)
	}
	if rec.Impl.Retries != 1 {
		t.Errorf("Impl.Retries = %d, want 1", rec.Impl.Retries)
	}
	if rec.Impl.FocusedTestsAdded != 12 {
		t.Errorf("Impl.FocusedTestsAdded = %d, want 12 (v1 tests_added)", rec.Impl.FocusedTestsAdded)
	}
	// v1 carries an iteration-wide pass flag; it has no focused_tests_pass.
	wantOptBool(t, "Impl.TestsTotalPass", rec.Impl.TestsTotalPass, true)
	if rec.Impl.FocusedTestsPass.Set {
		t.Error("Impl.FocusedTestsPass should be unset for a v1 entry")
	}
	if !rec.Impl.SelfAssessment.LinkedTracesToOutcomes {
		t.Error("Impl.SelfAssessment.LinkedTracesToOutcomes = false, want true")
	}
	if rec.Impl.SelfAssessment.PersistedViaWorkflowCommands != "yes" {
		t.Errorf("SelfAssessment.PersistedViaWorkflowCommands = %q, want yes",
			rec.Impl.SelfAssessment.PersistedViaWorkflowCommands)
	}
	if len(rec.Verifiers) != 0 {
		t.Errorf("v1 entry has %d verifiers, want 0", len(rec.Verifiers))
	}
}

func TestParseV2(t *testing.T) {
	rec := mustParse(t, "v2_iter.yaml")

	if rec.SchemaVersion != 2 {
		t.Errorf("SchemaVersion = %d, want 2", rec.SchemaVersion)
	}
	if rec.Iteration != 52 {
		t.Errorf("Iteration = %d, want 52", rec.Iteration)
	}
	if rec.CheckpointAt != "2026-04-18T15:26:48Z" {
		t.Errorf("CheckpointAt = %q, want 2026-04-18T15:26:48Z", rec.CheckpointAt)
	}
	if rec.Agent.SessionID != "sess-abc" || rec.Agent.Harness != "claude-code" {
		t.Errorf("Agent = %+v, want session sess-abc / claude-code", rec.Agent)
	}
	if rec.SessionTokens == nil {
		t.Fatal("SessionTokens = nil, want populated")
	}
	if rec.SessionTokens.CacheHitRate != 0.9 {
		t.Errorf("SessionTokens.CacheHitRate = %g, want 0.9", rec.SessionTokens.CacheHitRate)
	}

	if rec.Impl.ScopeNote != "on-target" {
		t.Errorf("Impl.ScopeNote = %q, want on-target", rec.Impl.ScopeNote)
	}
	wantOptBool(t, "Impl.FocusedTestsPass", rec.Impl.FocusedTestsPass, true)
	if rec.Impl.TestsTotalPass.Set {
		t.Error("Impl.TestsTotalPass should be unset for v2 (it lives on verifiers)")
	}

	if len(rec.Verifiers) != 1 {
		t.Fatalf("Verifiers count = %d, want 1", len(rec.Verifiers))
	}
	v := rec.Verifiers[0]
	if v.Type != "test" || v.Status != "pass" || !v.GatePassed {
		t.Errorf("Verifier = %+v, want type test / status pass / gate passed", v)
	}
	wantOptBool(t, "Verifier.TestsTotalPass", v.TestsTotalPass, true)
	if v.ResultArtifact != ".agents/active/verification/c3/test.result.yaml" {
		t.Errorf("Verifier.ResultArtifact = %q", v.ResultArtifact)
	}
	if !v.SelfAssessment.LinkedTracesToOutcomes {
		t.Error("Verifier.SelfAssessment.LinkedTracesToOutcomes = false, want true")
	}

	if rec.Review.OverallDecision != "accept" {
		t.Errorf("Review.OverallDecision = %q, want accept", rec.Review.OverallDecision)
	}
	if rec.Review.DecisionArtifact == "" {
		t.Error("Review.DecisionArtifact is empty, want a path")
	}
}

func TestParseV2Sparse(t *testing.T) {
	rec := mustParse(t, "v2_sparse.yaml")

	if rec.SchemaVersion != 2 || rec.Iteration != 65 {
		t.Fatalf("got schema %d iter %d, want 2 / 65", rec.SchemaVersion, rec.Iteration)
	}
	if rec.SessionTokens != nil {
		t.Error("SessionTokens should be nil when the block is absent")
	}
	if rec.Agent != (AgentInfo{}) {
		t.Errorf("Agent = %+v, want zero when the block is absent", rec.Agent)
	}
	if len(rec.Verifiers) != 0 {
		t.Errorf("Verifiers = %d, want 0", len(rec.Verifiers))
	}
	if rec.Impl.FocusedTestsPass.Set {
		t.Error("Impl.FocusedTestsPass should be unset when unreported")
	}
	if rec.Review.OverallDecision != "" {
		t.Errorf("Review.OverallDecision = %q, want empty", rec.Review.OverallDecision)
	}
}

func TestParseRejectsBadInput(t *testing.T) {
	tests := []struct {
		fixture   string
		errSubstr string
	}{
		{"bad_schema.yaml", "schema_version 99"},
		{"not_yaml.yaml", "probe iteration-log schema"},
		{"bad_v1.yaml", "parse v1 iteration entry"},
		{"bad_v2.yaml", "parse v2 iteration entry"},
	}
	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			_, err := parseFixture(t, tt.fixture)
			if err == nil {
				t.Fatalf("ParseIterationRecord(%s) = nil, want error", tt.fixture)
			}
			if !strings.Contains(err.Error(), tt.errSubstr) {
				t.Errorf("error = %q, want it to contain %q", err, tt.errSubstr)
			}
		})
	}
}

func TestLoadIterationLog(t *testing.T) {
	records, err := LoadIterationLog(filepath.Join("testdata", "iterlog"))
	if err != nil {
		t.Fatalf("LoadIterationLog() = %v, want nil", err)
	}
	if len(records) != 3 {
		t.Fatalf("loaded %d records, want 3 (iter 1, 2, 99)", len(records))
	}

	// Sorted ascending by iteration.
	if records[0].Iteration != 1 || records[1].Iteration != 2 || records[2].Iteration != 99 {
		t.Errorf("iteration order = %d, %d, %d; want 1, 2, 99",
			records[0].Iteration, records[1].Iteration, records[2].Iteration)
	}
	// Dedup: the dedicated iter-1.yaml wins over historical.yaml's iteration 1.
	if records[0].Commit != "dedicated1" {
		t.Errorf("iteration 1 commit = %q, want dedicated1 (dedicated file must win)", records[0].Commit)
	}
	// historical.yaml contributes iteration 99, which has no dedicated file.
	if records[2].Commit != "archived99" {
		t.Errorf("iteration 99 commit = %q, want archived99", records[2].Commit)
	}
}

func TestLoadIterationLogEmptyDir(t *testing.T) {
	records, err := LoadIterationLog(t.TempDir())
	if err != nil {
		t.Fatalf("LoadIterationLog(empty dir) = %v, want nil", err)
	}
	if len(records) != 0 {
		t.Errorf("loaded %d records from an empty dir, want 0", len(records))
	}
}

func TestLoadIterationLogReportsBadInput(t *testing.T) {
	tests := []struct {
		name string
		dir  string
	}{
		{"malformed iter file", "iterlog_badfile"},
		{"malformed historical.yaml", "iterlog_badhist"},
		{"unreadable iter file", "iterlog_unreadable"},
		{"unreadable historical.yaml", "iterlog_histdir"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadIterationLog(filepath.Join("testdata", tt.dir))
			if err == nil {
				t.Fatalf("LoadIterationLog(%s) = nil, want error", tt.dir)
			}
		})
	}
}

// TestLoadIterationLogRealData is the slice's integration assertion: the whole
// salvaged iteration log — both schemas plus the repaired historical.yaml —
// loads with zero errors.
func TestLoadIterationLogRealData(t *testing.T) {
	dir := filepath.Join("..", "..", ".agents", "active", "iteration-log")
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("iteration-log dir not present: %v", err)
	}
	records, err := LoadIterationLog(dir)
	if err != nil {
		t.Fatalf("LoadIterationLog(real data) = %v, want nil", err)
	}
	if len(records) == 0 {
		t.Fatal("loaded 0 records from the real iteration log")
	}

	var v1, v2 int
	for _, r := range records {
		switch r.SchemaVersion {
		case 1:
			v1++
		case 2:
			v2++
		}
	}
	if v1 == 0 || v2 == 0 {
		t.Errorf("real data: v1=%d v2=%d — expected both schemas present", v1, v2)
	}
	t.Logf("loaded %d iteration records (v1=%d, v2=%d)", len(records), v1, v2)
}

func TestOptionalBoolParsing(t *testing.T) {
	const base = "schema_version: 1\niteration: 1\n"

	ok := []struct {
		name  string
		yaml  string
		set   bool
		value bool
	}{
		{"bool true", "tests_total_pass: true\n", true, true},
		{"bool false", "tests_total_pass: false\n", true, false},
		{"non-zero int", "tests_total_pass: 3\n", true, true},
		{"zero int", "tests_total_pass: 0\n", true, false},
		{"absent", "", false, false},
		{"explicit null", "tests_total_pass: null\n", false, false},
		{"string true", "tests_total_pass: \"true\"\n", true, true},
		{"empty string", "tests_total_pass: \"\"\n", false, false},
	}
	for _, tt := range ok {
		t.Run(tt.name, func(t *testing.T) {
			rec, err := ParseIterationRecord([]byte(base + tt.yaml))
			if err != nil {
				t.Fatalf("ParseIterationRecord = %v, want nil", err)
			}
			got := rec.Impl.TestsTotalPass
			if got.Set != tt.set || (got.Set && got.Value != tt.value) {
				t.Errorf("TestsTotalPass = %+v, want {Set:%v Value:%v}", got, tt.set, tt.value)
			}
		})
	}

	bad := []struct{ name, yaml string }{
		{"unparseable string", "tests_total_pass: \"maybe\"\n"},
		{"wrong YAML type", "tests_total_pass: [a, b]\n"},
	}
	for _, tt := range bad {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParseIterationRecord([]byte(base + tt.yaml)); err == nil {
				t.Errorf("ParseIterationRecord(%s) = nil, want error", tt.name)
			}
		})
	}
}
