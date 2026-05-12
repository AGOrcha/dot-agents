// Package workflow — second batch of coverage tests targeting schema
// compilation/validation helpers (verification_result_schema.go,
// review_decision_schema.go, iter_log_schema.go), verification helpers, and
// sweep/drift edge cases.
package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// ─────────────────────────────────────────────────────────────────────────────
// verification_result_schema.go
// ─────────────────────────────────────────────────────────────────────────────

func TestCompiledVerificationResultSchema(t *testing.T) {
	sch, err := compiledVerificationResultSchema()
	if err != nil {
		t.Fatalf("compiledVerificationResultSchema: %v", err)
	}
	if sch == nil {
		t.Error("expected non-nil schema")
	}
	// Second call returns cached
	sch2, err := compiledVerificationResultSchema()
	if err != nil {
		t.Fatal(err)
	}
	if sch != sch2 {
		t.Error("expected sync.Once cached schema")
	}
}

func TestValidateVerificationResultDoc_Nil(t *testing.T) {
	if err := validateVerificationResultDoc(nil); err == nil {
		t.Error("expected error for nil doc")
	}
}

func TestValidateVerificationResultDoc_Valid(t *testing.T) {
	doc := &VerificationResultDoc{
		SchemaVersion: 1,
		TaskID:        "task-001",
		ParentPlanID:  "plan-001",
		VerifierType:  "unit",
		Status:        "pass",
		Summary:       "tests pass",
		RecordedAt:    time.Now().UTC().Format(time.RFC3339),
	}
	if err := validateVerificationResultDoc(doc); err != nil {
		t.Errorf("expected valid doc to pass: %v", err)
	}
}

func TestValidateVerificationResultDoc_Invalid(t *testing.T) {
	doc := &VerificationResultDoc{} // missing required fields
	if err := validateVerificationResultDoc(doc); err == nil {
		t.Error("expected validation failure for empty doc")
	}
}

func TestVerificationResultFilePath(t *testing.T) {
	p, err := verificationResultFilePath("/proj", "task-1", "unit")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p, "task-1") || !strings.HasSuffix(p, "unit.result.yaml") {
		t.Errorf("path = %q", p)
	}
	if _, err := verificationResultFilePath("/proj", "", "unit"); err == nil {
		t.Error("expected error for empty task")
	}
	if _, err := verificationResultFilePath("/proj", "t", ""); err == nil {
		t.Error("expected error for empty verifier_type")
	}
	if _, err := verificationResultFilePath("/proj", "t", "BAD"); err == nil {
		t.Error("expected error for invalid verifier_type")
	}
}

func TestValidVerificationVerifierTypeStem(t *testing.T) {
	cases := map[string]bool{
		"unit":     true,
		"api":      true,
		"unit-99":  true,
		"unit_99":  true,
		"":         false,
		"1unit":    false,
		"Unit":     false,
		"unit/bad": false,
		"unit!":    false,
	}
	for in, want := range cases {
		if got := validVerificationVerifierTypeStem(in); got != want {
			t.Errorf("validVerificationVerifierTypeStem(%q)=%v want %v", in, got, want)
		}
	}
}

func TestWriteVerificationResultYAML(t *testing.T) {
	dir := t.TempDir()
	doc := &VerificationResultDoc{
		SchemaVersion: 1,
		TaskID:        "task-1",
		ParentPlanID:  "plan-1",
		VerifierType:  "unit",
		Status:        "pass",
		Summary:       "ok",
		RecordedAt:    time.Now().UTC().Format(time.RFC3339),
	}
	if err := writeVerificationResultYAML(dir, doc); err != nil {
		t.Fatalf("writeVerificationResultYAML: %v", err)
	}
	want := filepath.Join(dir, ".agents", "active", "verification", "task-1", "unit.result.yaml")
	if _, err := os.Stat(want); err != nil {
		t.Errorf("expected file at %s: %v", want, err)
	}

	if err := writeVerificationResultYAML(dir, nil); err == nil {
		t.Error("nil doc should error")
	}
	bad := &VerificationResultDoc{TaskID: "t", VerifierType: ""}
	if err := writeVerificationResultYAML(dir, bad); err == nil {
		t.Error("missing verifier_type should error")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// review_decision_schema.go
// ─────────────────────────────────────────────────────────────────────────────

func TestCompiledVerificationDecisionSchema(t *testing.T) {
	sch, err := compiledVerificationDecisionSchema()
	if err != nil {
		t.Fatalf("compiledVerificationDecisionSchema: %v", err)
	}
	if sch == nil {
		t.Error("expected non-nil schema")
	}
}

func TestParseReviewPhaseDecision(t *testing.T) {
	for _, s := range []string{"accept", "reject", "escalate", "Accept", "  REJECT  "} {
		got, err := parseReviewPhaseDecision("--phase", s)
		if err != nil {
			t.Errorf("parseReviewPhaseDecision(%q): %v", s, err)
		}
		want := strings.TrimSpace(strings.ToLower(s))
		if got != want {
			t.Errorf("got %q want %q", got, want)
		}
	}
	if _, err := parseReviewPhaseDecision("--phase", "yikes"); err == nil {
		t.Error("expected error for invalid")
	}
}

func TestDeriveOverallReviewDecision(t *testing.T) {
	cases := []struct {
		p1, p2, want string
	}{
		{"accept", "accept", "accept"},
		{"accept", "reject", "reject"},
		{"reject", "accept", "reject"},
		{"accept", "escalate", "escalate"},
		{"escalate", "accept", "escalate"},
		{"escalate", "reject", "reject"},
	}
	for _, c := range cases {
		if got := deriveOverallReviewDecision(c.p1, c.p2); got != c.want {
			t.Errorf("derive(%s,%s)=%s want %s", c.p1, c.p2, got, c.want)
		}
	}
}

func TestOverallDecisionToVerificationStatus(t *testing.T) {
	if got := overallDecisionToVerificationStatus("accept"); got != "pass" {
		t.Errorf("got %s", got)
	}
	if got := overallDecisionToVerificationStatus("reject"); got != "fail" {
		t.Errorf("got %s", got)
	}
	if got := overallDecisionToVerificationStatus("escalate"); got != "partial" {
		t.Errorf("got %s", got)
	}
}

func TestValidateReviewDecisionDoc(t *testing.T) {
	if err := validateReviewDecisionDoc(nil); err == nil {
		t.Error("expected error for nil")
	}
	good := &ReviewDecisionDoc{
		SchemaVersion: 1, TaskID: "t", ParentPlanID: "p",
		Phase1Decision: "accept", Phase2Decision: "accept", OverallDecision: "accept",
		FailedGates: []string{}, RecordedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := validateReviewDecisionDoc(good); err != nil {
		t.Errorf("valid doc rejected: %v", err)
	}
	if err := validateReviewDecisionDoc(&ReviewDecisionDoc{}); err == nil {
		t.Error("expected validation failure")
	}
}

func TestReviewDecisionYAMLPath(t *testing.T) {
	p, err := reviewDecisionYAMLPath("/proj", "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p, "task-1") {
		t.Errorf("path = %s", p)
	}
	if _, err := reviewDecisionYAMLPath("/proj", ""); err == nil {
		t.Error("expected error for empty task")
	}
}

func TestWriteReviewDecisionYAML(t *testing.T) {
	dir := t.TempDir()
	doc := &ReviewDecisionDoc{
		SchemaVersion: 1, TaskID: "t1", ParentPlanID: "p1",
		Phase1Decision: "accept", Phase2Decision: "accept", OverallDecision: "accept",
		FailedGates: []string{}, RecordedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := writeReviewDecisionYAML(dir, doc); err != nil {
		t.Fatalf("writeReviewDecisionYAML: %v", err)
	}
	if err := writeReviewDecisionYAML(dir, nil); err == nil {
		t.Error("nil should error")
	}
	bad := &ReviewDecisionDoc{}
	if err := writeReviewDecisionYAML(dir, bad); err == nil {
		t.Error("empty doc should fail")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// iter_log_schema.go
// ─────────────────────────────────────────────────────────────────────────────

func TestCompiledWorkflowIterLogSchema(t *testing.T) {
	sch, err := compiledWorkflowIterLogSchema()
	if err != nil {
		t.Fatalf("compiledWorkflowIterLogSchema: %v", err)
	}
	if sch == nil {
		t.Error("expected non-nil schema")
	}
}

func TestValidateWorkflowIterLogEntry_Invalid(t *testing.T) {
	if err := validateWorkflowIterLogEntry(&iterLogEntry{}); err == nil {
		t.Error("expected validation failure for empty entry")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// verification.go: validators and log
// ─────────────────────────────────────────────────────────────────────────────

func TestIsValidVerificationKind(t *testing.T) {
	for _, k := range []string{"test", "lint", "build", "format", "custom", "review", "TEST", " custom "} {
		if !isValidVerificationKind(k) {
			t.Errorf("expected %q valid", k)
		}
	}
	if isValidVerificationKind("bogus") {
		t.Error("bogus should not be valid")
	}
}

func TestIsValidVerificationScope(t *testing.T) {
	for _, s := range []string{"file", "package", "repo", "custom"} {
		if !isValidVerificationScope(s) {
			t.Errorf("expected %q valid", s)
		}
	}
	if isValidVerificationScope("global") {
		t.Error("global should not be valid")
	}
}

func TestVerificationLogPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	p := verificationLogPath("p")
	if !strings.HasSuffix(p, "verification-log.jsonl") {
		t.Errorf("path = %s", p)
	}
}

func TestAppendAndReadVerificationLog(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", tmp)
	for i := 0; i < 3; i++ {
		rec := VerificationRecord{
			SchemaVersion: 1, Timestamp: time.Now().UTC().Format(time.RFC3339),
			Kind: "test", Status: "pass", Scope: "repo",
			Summary: "tests pass",
		}
		if err := appendVerificationLog("proj", rec); err != nil {
			t.Fatal(err)
		}
	}
	recs, err := readVerificationLog("proj", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 3 {
		t.Errorf("expected 3 records, got %d", len(recs))
	}
	// Limit
	recs, _ = readVerificationLog("proj", 2)
	if len(recs) != 2 {
		t.Errorf("expected 2 with limit, got %d", len(recs))
	}
	// Missing path returns empty, not error
	recs, err = readVerificationLog("ghost", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 0 {
		t.Errorf("missing log returns 0, got %d", len(recs))
	}
}

func TestReadVerificationLog_SkipsMalformed(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", tmp)
	rec := VerificationRecord{SchemaVersion: 1, Kind: "test", Status: "pass", Summary: "x"}
	if err := appendVerificationLog("p2", rec); err != nil {
		t.Fatal(err)
	}
	// Append a malformed line
	path := verificationLogPath("p2")
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	_, _ = f.WriteString("not json\n")
	_ = f.Close()
	recs, _ := readVerificationLog("p2", 0)
	if len(recs) != 1 {
		t.Errorf("malformed line should be skipped, got %d records", len(recs))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// sweep.go: extra branches
// ─────────────────────────────────────────────────────────────────────────────

func TestPlanSweep_MissingPlanStructureOnly(t *testing.T) {
	reports := []RepoDriftReport{
		{
			Project:              ManagedProject{Name: "needs-plans", Path: "/tmp/x"},
			Reachable:            true,
			MissingPlanStructure: true,
			MissingWorkflowDir:   false, // important: skip workflow scaffold path
			Status:               "warn",
		},
	}
	plan := planSweep(reports)
	found := false
	for _, a := range plan.Actions {
		if a.Action == SweepActionCreatePlanStructure {
			found = true
		}
	}
	if !found {
		t.Error("expected create_plan_structure action")
	}
}

func TestPlanSweep_StaleProposalActionPresent(t *testing.T) {
	reports := []RepoDriftReport{
		{
			Project:            ManagedProject{Name: "proposals", Path: "/tmp/p"},
			Reachable:          true,
			StaleProposalCount: 3,
			Status:             "warn",
		},
	}
	plan := planSweep(reports)
	var found *SweepActionItem
	for i, a := range plan.Actions {
		if a.Action == SweepActionFlagStaleProposals {
			found = &plan.Actions[i]
		}
	}
	if found == nil {
		t.Fatal("expected flag_stale_proposals action")
	}
	if found.RequiresConfirmation {
		t.Error("flag_stale_proposals should NOT require confirmation (read-only)")
	}
}

func TestAppendSweepLog_DirCreation(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", tmp)
	// Remove any pre-existing dir, sweep log should mkdir.
	logPath := sweepLogPath()
	_ = os.RemoveAll(filepath.Dir(logPath))
	appendSweepLog(SweepLogEntry{Timestamp: time.Now().UTC().Format(time.RFC3339), Project: "p"})
	if _, err := os.Stat(logPath); err != nil {
		t.Errorf("sweep log not created: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// drift.go: runWorkflowDrift
// ─────────────────────────────────────────────────────────────────────────────

func TestRunWorkflowDrift_NoProjects(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", tmp)
	cmd := newDriftTestCommand("", 7, 30)
	out, _ := captureCovStdout(t, func() error { return runWorkflowDrift(cmd, nil) })
	if !strings.Contains(out, "No managed projects") {
		t.Errorf("expected 'No managed projects' notice, got %s", out)
	}
}

func TestRunWorkflowDrift_MissingProjectFilter(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", tmp)
	// Register a managed project so we get past the "no managed projects" guard.
	if err := seedManagedProject(tmp, "alpha", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	cmd := newDriftTestCommand("missing", 7, 30)
	err := runWorkflowDrift(cmd, nil)
	if err == nil {
		t.Error("expected error for missing project filter")
	}
}

func TestRunWorkflowDrift_RealProject(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", tmp)
	target := t.TempDir()
	if err := seedManagedProject(tmp, "ok-proj", target); err != nil {
		t.Fatal(err)
	}
	cmd := newDriftTestCommand("", 7, 30)
	out, _ := captureCovStdout(t, func() error { return runWorkflowDrift(cmd, nil) })
	if !strings.Contains(out, "Workflow Drift Report") {
		t.Errorf("expected drift report header, got: %s", out)
	}
}

func TestRunWorkflowDrift_JSON(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", tmp)
	target := t.TempDir()
	if err := seedManagedProject(tmp, "ok-json", target); err != nil {
		t.Fatal(err)
	}
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = false })
	cmd := newDriftTestCommand("", 7, 30)
	out, _ := captureCovStdout(t, func() error { return runWorkflowDrift(cmd, nil) })
	if !strings.Contains(out, "\"timestamp\"") {
		t.Errorf("expected json timestamp field, got %s", out)
	}
}

// newDriftTestCommand builds a cobra.Command with flags runWorkflowDrift reads.
func newDriftTestCommand(project string, staleDays, proposalDays int) *cobra.Command {
	c := &cobra.Command{}
	c.Flags().Int("stale-days", staleDays, "")
	c.Flags().Int("proposal-days", proposalDays, "")
	c.Flags().String("project", project, "")
	return c
}

// seedManagedProject writes AGENTS_HOME/config.json with a single managed project entry.
// When AGENTS_HOME is set, that path is the .agents dir itself; otherwise we
// write to <home>/.agents/config.json.
func seedManagedProject(agentsHome, name, path string) error {
	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		return err
	}
	cfg := map[string]any{
		"version": 1,
		"projects": map[string]any{
			name: map[string]any{
				"path":  path,
				"added": time.Now().UTC().Format(time.RFC3339),
			},
		},
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(agentsHome, "config.json"), data, 0644)
}

// ─────────────────────────────────────────────────────────────────────────────
// runWorkflowSweep with projects registered
// ─────────────────────────────────────────────────────────────────────────────

func TestRunWorkflowSweep_WithHealthyProject(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", tmp)
	target := t.TempDir()
	// Create healthy workflow dir + plans
	if err := os.MkdirAll(filepath.Join(target, ".agents", "workflow", "plans"), 0755); err != nil {
		t.Fatal(err)
	}
	// Recent checkpoint (context dir lives under AGENTS_HOME == tmp).
	chDir := filepath.Join(tmp, "context", "healthy-x")
	if err := os.MkdirAll(chDir, 0755); err != nil {
		t.Fatal(err)
	}
	cp := []byte("schema_version: 1\ntimestamp: " + time.Now().UTC().Format(time.RFC3339) + "\n")
	if err := os.WriteFile(filepath.Join(chDir, "checkpoint.yaml"), cp, 0644); err != nil {
		t.Fatal(err)
	}
	if err := seedManagedProject(tmp, "healthy-x", target); err != nil {
		t.Fatal(err)
	}
	cmd := newSweepTestCommand(false, 7, 30)
	out, _ := captureCovStdout(t, func() error { return runWorkflowSweep(cmd, nil) })
	if !strings.Contains(out, "No sweep actions needed") {
		t.Errorf("expected 'No sweep actions needed', got %s", out)
	}
}

func TestRunWorkflowSweep_DryRunGeneratesPlan(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", tmp)
	target := t.TempDir()
	// No workflow dir → drift will see MissingWorkflowDir, sweep will plan scaffold
	if err := seedManagedProject(tmp, "drift-x", target); err != nil {
		t.Fatal(err)
	}
	cmd := newSweepTestCommand(false, 7, 30)
	out, _ := captureCovStdout(t, func() error { return runWorkflowSweep(cmd, nil) })
	if !strings.Contains(out, "Sweep Plan") {
		t.Errorf("expected 'Sweep Plan' header in output, got: %s", out)
	}
}
