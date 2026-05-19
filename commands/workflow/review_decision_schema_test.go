package workflow

import (
	"strings"
	"testing"
	"time"
)

func TestCompiledVerificationDecisionSchema(t *testing.T) {
	sch, err := compiledVerificationDecisionSchema(stdSchemaCompiler{})
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

func TestWorkflowDelegationGate_TextOutput_Reject(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	saveTestDelegationContract(t, repo, "t1", "p1", "del-t1")
	writeMergeBackFixture(t, repo, "t1", "p1")
	writeReviewDecisionFixture(t, repo, "t1", &ReviewDecisionDoc{
		SchemaVersion:   1,
		TaskID:          "t1",
		ParentPlanID:    "p1",
		Phase1Decision:  "reject",
		Phase2Decision:  "accept",
		OverallDecision: "reject",
		FailedGates:     []string{"unit"},
		RecordedAt:      "2026-04-19T12:00:00Z",
	})

	out := executeWorkflowCommandOutput(t, repo, "delegation", "gate", "--plan", "p1", "--task", "t1")
	if !strings.Contains(out, "outcome: reject") {
		t.Fatalf("expected outcome: reject in text output, got:\n%s", out)
	}
	if !strings.Contains(out, "closeout_allowed: false") {
		t.Fatalf("expected closeout_allowed: false, got:\n%s", out)
	}
	if !strings.Contains(out, "reason:") {
		t.Fatalf("expected reason: line, got:\n%s", out)
	}
}

func TestWriteReviewDecisionYAML_SchemaInvalid(t *testing.T) {
	doc := &ReviewDecisionDoc{
		SchemaVersion: 1, TaskID: "t",
	}
	err := writeReviewDecisionYAML(t.TempDir(), doc)
	if err == nil {
		t.Fatal("expected schema error")
	}
}

func TestValidateReviewDecisionDoc_Valid(t *testing.T) {
	if err := validateReviewDecisionDoc(newValidReviewDecisionDoc()); err != nil {
		t.Fatalf("valid doc rejected: %v", err)
	}
}

// TestValidateReviewDecisionDoc_RemapUnmarshalError drives the remap
// json.Unmarshal error branch via a jsonMarshal seam returning invalid JSON.
func TestValidateReviewDecisionDoc_RemapUnmarshalError(t *testing.T) {
	withJSONMarshalStub(t, func(any) ([]byte, error) { return []byte("{not-json"), nil })

	err := validateReviewDecisionDoc(newValidReviewDecisionDoc())
	if err == nil {
		t.Fatal("expected remap unmarshal error, got nil")
	}
	if !strings.Contains(err.Error(), "remap review decision for schema validation") {
		t.Errorf("expected remap error message, got %q", err.Error())
	}
}

func TestValidateReviewDecisionDoc_CompileError(t *testing.T) {
	resetCompiledSchemaOnce(t, &verificationDecisionCompiledOnce,
		&verificationDecisionCompiled, &verificationDecisionCompiledErr)

	// Prime the once-block with an injected failing compiler so the cached
	// CompiledErr is non-nil; validateReviewDecisionDoc then exercises its
	// compiled-schema error-propagation guard on the real (std) call.
	if _, err := compiledVerificationDecisionSchema(addResourceErrCompiler()); err == nil {
		t.Fatal("precondition: primed compile should have failed")
	}

	if err := validateReviewDecisionDoc(newValidReviewDecisionDoc()); err == nil {
		t.Fatal("expected compiled-schema error to propagate, got nil")
	}
}

// TestReviewDecisionYAMLPath_BlankRel drives the rel == "" guard: a task ID
// of only path separators trims non-empty but iterLogReviewDecisionPath
// returns "".
func TestReviewDecisionYAMLPath_BlankRel(t *testing.T) {
	if _, err := reviewDecisionYAMLPath("/proj", "   "); err == nil {
		t.Fatal("expected error for whitespace-only task id")
	}
}
