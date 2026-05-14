package workflow

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// withMkdirAllStub swaps osMkdirAll for the duration of the test.
func withMkdirAllStub(t *testing.T, stub func(string, os.FileMode) error) {
	t.Helper()
	prev := osMkdirAll
	osMkdirAll = stub
	t.Cleanup(func() { osMkdirAll = prev })
}

// withWriteFileStub swaps osWriteFile for the duration of the test.
func withWriteFileStub(t *testing.T, stub func(string, []byte, os.FileMode) error) {
	t.Helper()
	prev := osWriteFile
	osWriteFile = stub
	t.Cleanup(func() { osWriteFile = prev })
}

// withOpenFileStub swaps osOpenFile for the duration of the test.
func withOpenFileStub(t *testing.T, stub func(string, int, os.FileMode) (*os.File, error)) {
	t.Helper()
	prev := osOpenFile
	osOpenFile = stub
	t.Cleanup(func() { osOpenFile = prev })
}

// withRemoveAllStub swaps osRemoveAll for the duration of the test.
func withRemoveAllStub(t *testing.T, stub func(string) error) {
	t.Helper()
	prev := osRemoveAll
	osRemoveAll = stub
	t.Cleanup(func() { osRemoveAll = prev })
}

// ─── scaffoldGraphBridgeConfig seam paths ────────────────────────────────────

func TestScaffoldGraphBridgeConfig_MkdirError(t *testing.T) {
	sentinel := errors.New("mkdir boom")
	withMkdirAllStub(t, func(string, os.FileMode) error { return sentinel })

	_, err := scaffoldGraphBridgeConfig(t.TempDir())
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected mkdir sentinel, got %v", err)
	}
}

func TestScaffoldGraphBridgeConfig_WriteError(t *testing.T) {
	sentinel := errors.New("write boom")
	withWriteFileStub(t, func(string, []byte, os.FileMode) error { return sentinel })

	_, err := scaffoldGraphBridgeConfig(t.TempDir())
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected write sentinel, got %v", err)
	}
}

// ─── writeGraphBridgeHealth seam paths ───────────────────────────────────────

func TestWriteGraphBridgeHealth_MkdirError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	sentinel := errors.New("mkdir boom")
	withMkdirAllStub(t, func(string, os.FileMode) error { return sentinel })

	err := writeGraphBridgeHealth("p", GraphBridgeHealth{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected mkdir sentinel, got %v", err)
	}
}

func TestWriteGraphBridgeHealth_WriteError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	sentinel := errors.New("write boom")
	withWriteFileStub(t, func(string, []byte, os.FileMode) error { return sentinel })

	err := writeGraphBridgeHealth("p", GraphBridgeHealth{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected write sentinel, got %v", err)
	}
}

// ─── saveDriftReport seam paths ──────────────────────────────────────────────

func TestSaveDriftReport_MkdirError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	sentinel := errors.New("mkdir boom")
	withMkdirAllStub(t, func(string, os.FileMode) error { return sentinel })

	err := saveDriftReport(AggregateDriftReport{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected mkdir sentinel, got %v", err)
	}
}

func TestSaveDriftReport_WriteError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	sentinel := errors.New("write boom")
	withWriteFileStub(t, func(string, []byte, os.FileMode) error { return sentinel })

	err := saveDriftReport(AggregateDriftReport{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected write sentinel, got %v", err)
	}
}

// ─── writeHealthSnapshot seam paths ──────────────────────────────────────────

func TestWriteHealthSnapshot_MkdirError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	sentinel := errors.New("mkdir boom")
	withMkdirAllStub(t, func(string, os.FileMode) error { return sentinel })

	err := writeHealthSnapshot("p", WorkflowHealthSnapshot{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected mkdir sentinel, got %v", err)
	}
}

func TestWriteHealthSnapshot_WriteError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	sentinel := errors.New("write boom")
	withWriteFileStub(t, func(string, []byte, os.FileMode) error { return sentinel })

	err := writeHealthSnapshot("p", WorkflowHealthSnapshot{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected write sentinel, got %v", err)
	}
}

// ─── saveDelegationContract seam paths ───────────────────────────────────────

func TestSaveDelegationContract_MkdirError(t *testing.T) {
	sentinel := errors.New("mkdir boom")
	withMkdirAllStub(t, func(string, os.FileMode) error { return sentinel })

	err := saveDelegationContract(t.TempDir(), &DelegationContract{ParentTaskID: "t1"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected mkdir sentinel, got %v", err)
	}
}

func TestSaveDelegationContract_WriteError(t *testing.T) {
	sentinel := errors.New("write boom")
	withWriteFileStub(t, func(string, []byte, os.FileMode) error { return sentinel })

	err := saveDelegationContract(t.TempDir(), &DelegationContract{ParentTaskID: "t1"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected write sentinel, got %v", err)
	}
}

// ─── saveMergeBack seam paths ────────────────────────────────────────────────

func TestSaveMergeBack_MkdirError(t *testing.T) {
	sentinel := errors.New("mkdir boom")
	withMkdirAllStub(t, func(string, os.FileMode) error { return sentinel })

	err := saveMergeBack(t.TempDir(), &MergeBackSummary{TaskID: "t1"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected mkdir sentinel, got %v", err)
	}
}

func TestSaveMergeBack_WriteError(t *testing.T) {
	sentinel := errors.New("write boom")
	withWriteFileStub(t, func(string, []byte, os.FileMode) error { return sentinel })

	err := saveMergeBack(t.TempDir(), &MergeBackSummary{TaskID: "t1"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected write sentinel, got %v", err)
	}
}

// ─── writeFoldBackArtifact seam paths ────────────────────────────────────────

func TestWriteFoldBackArtifact_MkdirError(t *testing.T) {
	sentinel := errors.New("mkdir boom")
	withMkdirAllStub(t, func(string, os.FileMode) error { return sentinel })

	err := writeFoldBackArtifact(t.TempDir(), foldBackArtifact{ID: "fb1"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected mkdir sentinel, got %v", err)
	}
}

func TestWriteFoldBackArtifact_WriteError(t *testing.T) {
	sentinel := errors.New("write boom")
	withWriteFileStub(t, func(string, []byte, os.FileMode) error { return sentinel })

	err := writeFoldBackArtifact(t.TempDir(), foldBackArtifact{ID: "fb1"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected write sentinel, got %v", err)
	}
}

// ─── saveDelegationBundle seam paths ─────────────────────────────────────────

func TestSaveDelegationBundle_MkdirError(t *testing.T) {
	sentinel := errors.New("mkdir boom")
	withMkdirAllStub(t, func(string, os.FileMode) error { return sentinel })

	err := saveDelegationBundle(t.TempDir(), &delegationBundleYAML{DelegationID: "d1"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected mkdir sentinel, got %v", err)
	}
}

func TestSaveDelegationBundle_WriteError(t *testing.T) {
	sentinel := errors.New("write boom")
	withWriteFileStub(t, func(string, []byte, os.FileMode) error { return sentinel })

	err := saveDelegationBundle(t.TempDir(), &delegationBundleYAML{DelegationID: "d1"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected write sentinel, got %v", err)
	}
}

// ─── persistScopeEvidenceSidecar seam paths ──────────────────────────────────

func TestPersistScopeEvidenceSidecar_MkdirError(t *testing.T) {
	sentinel := errors.New("mkdir boom")
	withMkdirAllStub(t, func(string, os.FileMode) error { return sentinel })

	_, err := persistScopeEvidenceSidecar(t.TempDir(), "plan", "task", &ScopeEvidence{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected mkdir sentinel wrapped, got %v", err)
	}
}

func TestPersistScopeEvidenceSidecar_WriteError(t *testing.T) {
	sentinel := errors.New("write boom")
	withWriteFileStub(t, func(string, []byte, os.FileMode) error { return sentinel })

	_, err := persistScopeEvidenceSidecar(t.TempDir(), "plan", "task", &ScopeEvidence{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected write sentinel wrapped, got %v", err)
	}
}

// ─── saveCanonicalPlan / saveCanonicalTasks seam paths ───────────────────────

func TestSaveCanonicalPlan_MkdirError(t *testing.T) {
	sentinel := errors.New("mkdir boom")
	withMkdirAllStub(t, func(string, os.FileMode) error { return sentinel })

	err := saveCanonicalPlan(t.TempDir(), &CanonicalPlan{ID: "p1"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected mkdir sentinel, got %v", err)
	}
}

func TestSaveCanonicalPlan_WriteError(t *testing.T) {
	sentinel := errors.New("write boom")
	withWriteFileStub(t, func(string, []byte, os.FileMode) error { return sentinel })

	err := saveCanonicalPlan(t.TempDir(), &CanonicalPlan{ID: "p1"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected write sentinel, got %v", err)
	}
}

func TestSaveCanonicalTasks_MkdirError(t *testing.T) {
	sentinel := errors.New("mkdir boom")
	withMkdirAllStub(t, func(string, os.FileMode) error { return sentinel })

	err := saveCanonicalTasks(t.TempDir(), &CanonicalTaskFile{PlanID: "p1"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected mkdir sentinel, got %v", err)
	}
}

func TestSaveCanonicalTasks_WriteError(t *testing.T) {
	sentinel := errors.New("write boom")
	withWriteFileStub(t, func(string, []byte, os.FileMode) error { return sentinel })

	err := saveCanonicalTasks(t.TempDir(), &CanonicalTaskFile{PlanID: "p1"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected write sentinel, got %v", err)
	}
}

// ─── writeVerificationResultYAML seam paths ──────────────────────────────────

func newValidVerificationResultDoc() *VerificationResultDoc {
	return &VerificationResultDoc{
		SchemaVersion: 1,
		TaskID:        "task-1",
		ParentPlanID:  "plan-1",
		VerifierType:  "merge-back",
		Status:        "pass",
		Summary:       "ok",
		RecordedAt:    "2026-05-12T00:00:00Z",
	}
}

func TestWriteVerificationResultYAML_MkdirError(t *testing.T) {
	sentinel := errors.New("mkdir boom")
	withMkdirAllStub(t, func(string, os.FileMode) error { return sentinel })

	err := writeVerificationResultYAML(t.TempDir(), newValidVerificationResultDoc())
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected mkdir sentinel wrapped, got %v", err)
	}
}

func TestWriteVerificationResultYAML_WriteError(t *testing.T) {
	sentinel := errors.New("write boom")
	withWriteFileStub(t, func(string, []byte, os.FileMode) error { return sentinel })

	err := writeVerificationResultYAML(t.TempDir(), newValidVerificationResultDoc())
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected write sentinel wrapped, got %v", err)
	}
}

// ─── writeReviewDecisionYAML seam paths ──────────────────────────────────────

func newValidReviewDecisionDoc() *ReviewDecisionDoc {
	return &ReviewDecisionDoc{
		SchemaVersion:   1,
		TaskID:          "task-1",
		ParentPlanID:    "plan-1",
		Phase1Decision:  "accept",
		Phase2Decision:  "accept",
		OverallDecision: "accept",
		FailedGates:     []string{},
		RecordedAt:      "2026-05-12T00:00:00Z",
	}
}

func TestWriteReviewDecisionYAML_MkdirError(t *testing.T) {
	sentinel := errors.New("mkdir boom")
	withMkdirAllStub(t, func(string, os.FileMode) error { return sentinel })

	err := writeReviewDecisionYAML(t.TempDir(), newValidReviewDecisionDoc())
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected mkdir sentinel wrapped, got %v", err)
	}
}

func TestWriteReviewDecisionYAML_WriteError(t *testing.T) {
	sentinel := errors.New("write boom")
	withWriteFileStub(t, func(string, []byte, os.FileMode) error { return sentinel })

	err := writeReviewDecisionYAML(t.TempDir(), newValidReviewDecisionDoc())
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected write sentinel wrapped, got %v", err)
	}
}

// ─── writeIterLogEntry seam paths ────────────────────────────────────────────

func newValidIterLogEntry() *iterLogEntry {
	return &iterLogEntry{
		SchemaVersion: 2,
		Iteration:     1,
		Date:          "2026-05-12",
		Wave:          "w1",
		TaskID:        "task-1",
		Commit:        "abc",
		FilesChanged:  1,
		LinesAdded:    1,
		LinesRemoved:  0,
		Verifiers:     []iterLogVerifierEntry{},
	}
}

func TestWriteIterLogEntry_WriteError(t *testing.T) {
	tmp := t.TempDir()
	iterPath := filepath.Join(tmp, "iter-1.yaml")

	sentinel := errors.New("write boom")
	withWriteFileStub(t, func(string, []byte, os.FileMode) error { return sentinel })

	err := writeIterLogEntry(iterPath, newValidIterLogEntry())
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected write sentinel wrapped, got %v", err)
	}
}

// ─── appendWorkflowSessionLog seam path ──────────────────────────────────────

func TestAppendWorkflowSessionLog_OpenFileError(t *testing.T) {
	sentinel := errors.New("open boom")
	withOpenFileStub(t, func(string, int, os.FileMode) (*os.File, error) { return nil, sentinel })

	err := appendWorkflowSessionLog(filepath.Join(t.TempDir(), "log.md"), workflowCheckpoint{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected open sentinel, got %v", err)
	}
}

// ─── removeAllWithRetry seam paths ───────────────────────────────────────────

func TestRemoveAllWithRetry_SecondAttemptError(t *testing.T) {
	calls := 0
	sentinel := errors.New("rmall boom")
	withRemoveAllStub(t, func(string) error {
		calls++
		return sentinel
	})

	if err := removeAllWithRetry("/some/path"); !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel, got %v", err)
	}
	if calls != 2 {
		t.Errorf("expected 2 attempts on failure, got %d", calls)
	}
}

func TestRemoveAllWithRetry_FirstAttemptSuccess(t *testing.T) {
	calls := 0
	withRemoveAllStub(t, func(string) error {
		calls++
		return nil
	})

	if err := removeAllWithRetry("/some/path"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 attempt on success, got %d", calls)
	}
}

func TestRemoveAllWithRetry_RetrySucceeds(t *testing.T) {
	calls := 0
	sentinel := errors.New("transient")
	withRemoveAllStub(t, func(string) error {
		calls++
		if calls == 1 {
			return sentinel
		}
		return nil
	})

	if err := removeAllWithRetry("/some/path"); err != nil {
		t.Fatalf("unexpected error after retry: %v", err)
	}
	if calls != 2 {
		t.Errorf("expected 2 attempts, got %d", calls)
	}
}

// ─── appendVerificationLog seam paths ────────────────────────────────────────

func TestAppendVerificationLog_MkdirError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	sentinel := errors.New("mkdir boom")
	withMkdirAllStub(t, func(string, os.FileMode) error { return sentinel })

	err := appendVerificationLog("p", VerificationRecord{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected mkdir sentinel, got %v", err)
	}
}

func TestAppendVerificationLog_OpenFileError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	sentinel := errors.New("open boom")
	withOpenFileStub(t, func(string, int, os.FileMode) (*os.File, error) { return nil, sentinel })

	err := appendVerificationLog("p", VerificationRecord{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected open sentinel, got %v", err)
	}
}

// ─── runWorkflowCheckpoint seam paths (mkdir/write) ──────────────────────────

func TestRunWorkflowCheckpoint_MkdirError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	sentinel := errors.New("mkdir boom")
	withMkdirAllStub(t, func(string, os.FileMode) error { return sentinel })

	err := runWorkflowCheckpoint("msg", "pass", "ok")
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected mkdir sentinel, got %v", err)
	}
}

func TestRunWorkflowCheckpoint_WriteError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	sentinel := errors.New("write boom")
	withWriteFileStub(t, func(string, []byte, os.FileMode) error { return sentinel })

	err := runWorkflowCheckpoint("msg", "pass", "ok")
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected write sentinel, got %v", err)
	}
}
