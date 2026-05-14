package workflow

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// withOsExitStub swaps osExit so process-terminating branches can be
// exercised without killing the test binary. The captured codes are
// appended to the returned slice in call order.
func withOsExitStub(t *testing.T) *[]int {
	t.Helper()
	codes := make([]int, 0, 2)
	prev := osExit
	osExit = func(code int) { codes = append(codes, code) }
	t.Cleanup(func() { osExit = prev })
	return &codes
}

// withWorkflowStdin swaps workflowStdin to the provided input for the
// lifetime of the test.
func withWorkflowStdin(t *testing.T, input string) {
	t.Helper()
	prev := workflowStdin
	workflowStdin = strings.NewReader(input)
	t.Cleanup(func() { workflowStdin = prev })
}

// withWorkflowStdinReader swaps workflowStdin to a custom reader.
func withWorkflowStdinReader(t *testing.T, r io.Reader) {
	t.Helper()
	prev := workflowStdin
	workflowStdin = r
	t.Cleanup(func() { workflowStdin = prev })
}

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

// ─── loadCheckScopeSidecar: missing-sidecar branch invokes osExit(2) ────────

func TestLoadCheckScopeSidecar_MissingTriggersExit(t *testing.T) {
	codes := withOsExitStub(t)

	proj := t.TempDir()
	// No sidecar file written: ReadFile must hit os.IsNotExist.
	path, ev, err := loadCheckScopeSidecar(proj, "missing-plan", "missing-task")
	if !errors.Is(err, errCheckScopeSidecarMissing) {
		t.Fatalf("expected errCheckScopeSidecarMissing, got %v", err)
	}
	if path != "" || ev != nil {
		t.Errorf("expected empty/nil returns when sidecar is missing, got path=%q ev=%v", path, ev)
	}
	if len(*codes) != 1 || (*codes)[0] != 2 {
		t.Errorf("expected one osExit(2) call, got %v", *codes)
	}
}

// ─── renderCheckScopeResult: warning branch invokes osExit(1) ───────────────

func TestRenderCheckScopeResult_WarningBranchExits(t *testing.T) {
	codes := withOsExitStub(t)

	renderCheckScopeResult("p1", "t1", "/tmp/sidecar.yaml", checkScopeResult{
		PlanID:       "p1",
		TaskID:       "t1",
		SidecarPath:  "/tmp/sidecar.yaml",
		OutsideScope: []string{"src/forbidden.go"},
		Clean:        false,
	})

	if len(*codes) != 1 || (*codes)[0] != 1 {
		t.Errorf("expected one osExit(1) call, got %v", *codes)
	}
}

func TestRenderCheckScopeResult_CleanBranchNoExit(t *testing.T) {
	codes := withOsExitStub(t)

	renderCheckScopeResult("p1", "t1", "/tmp/sidecar.yaml", checkScopeResult{
		PlanID:      "p1",
		TaskID:      "t1",
		SidecarPath: "/tmp/sidecar.yaml",
		InsideScope: []string{"commands/foo.go"},
		Clean:       true,
	})

	if len(*codes) != 0 {
		t.Errorf("expected no osExit calls on clean result, got %v", *codes)
	}
}

// ─── confirmSweepAction: user-decline branch via workflowStdin ──────────────

func TestConfirmSweepAction_DeclineBranch(t *testing.T) {
	// Ensure Yes flag is off so the prompt branch is taken.
	oldYes := deps.Flags.Yes
	deps.Flags.Yes = func() bool { return false }
	t.Cleanup(func() { deps.Flags.Yes = oldYes })

	// Redirect the sweep log to a temp dir so the test does not pollute
	// the real .agents/ tree.
	t.Setenv("HOME", t.TempDir())

	withWorkflowStdin(t, "n\n")

	action := SweepActionItem{
		Project:              ManagedProject{Name: "p"},
		Action:               SweepActionCreateCheckpointReminder,
		RequiresConfirmation: true,
		Description:          "test reminder",
	}
	if confirmSweepAction(action) {
		t.Error("expected decline (n) to return false")
	}
}

func TestConfirmSweepAction_AcceptBranch(t *testing.T) {
	oldYes := deps.Flags.Yes
	deps.Flags.Yes = func() bool { return false }
	t.Cleanup(func() { deps.Flags.Yes = oldYes })

	t.Setenv("HOME", t.TempDir())

	withWorkflowStdin(t, "y\n")

	action := SweepActionItem{
		Project:              ManagedProject{Name: "p"},
		Action:               SweepActionCreateCheckpointReminder,
		RequiresConfirmation: true,
		Description:          "test reminder",
	}
	if !confirmSweepAction(action) {
		t.Error("expected accept (y) to return true")
	}
}

func TestConfirmSweepAction_EmptyInputDeclines(t *testing.T) {
	oldYes := deps.Flags.Yes
	deps.Flags.Yes = func() bool { return false }
	t.Cleanup(func() { deps.Flags.Yes = oldYes })

	t.Setenv("HOME", t.TempDir())

	withWorkflowStdin(t, "\n")

	action := SweepActionItem{
		Project:              ManagedProject{Name: "p"},
		Action:               SweepActionCreateCheckpointReminder,
		RequiresConfirmation: true,
		Description:          "test reminder",
	}
	if confirmSweepAction(action) {
		t.Error("expected empty input to decline (default N)")
	}
}

// ─── Marshal seam helpers ────────────────────────────────────────────────────

// withYAMLMarshalStub swaps yamlMarshal for the duration of the test.
func withYAMLMarshalStub(t *testing.T, stub func(any) ([]byte, error)) {
	t.Helper()
	prev := yamlMarshal
	yamlMarshal = stub
	t.Cleanup(func() { yamlMarshal = prev })
}

// withJSONMarshalStub swaps jsonMarshal for the duration of the test.
func withJSONMarshalStub(t *testing.T, stub func(any) ([]byte, error)) {
	t.Helper()
	prev := jsonMarshal
	jsonMarshal = stub
	t.Cleanup(func() { jsonMarshal = prev })
}

// withJSONMarshalIndentStub swaps jsonMarshalIndent for the duration of the test.
func withJSONMarshalIndentStub(t *testing.T, stub func(any, string, string) ([]byte, error)) {
	t.Helper()
	prev := jsonMarshalIndent
	jsonMarshalIndent = stub
	t.Cleanup(func() { jsonMarshalIndent = prev })
}

func yamlMarshalErrStub(sentinel error) func(any) ([]byte, error) {
	return func(any) ([]byte, error) { return nil, sentinel }
}

func jsonMarshalErrStub(sentinel error) func(any) ([]byte, error) {
	return func(any) ([]byte, error) { return nil, sentinel }
}

func jsonMarshalIndentErrStub(sentinel error) func(any, string, string) ([]byte, error) {
	return func(any, string, string) ([]byte, error) { return nil, sentinel }
}

// ─── yamlMarshal error branches ──────────────────────────────────────────────

func TestSaveDelegationContract_YAMLMarshalError(t *testing.T) {
	sentinel := errors.New("yaml boom")
	withYAMLMarshalStub(t, yamlMarshalErrStub(sentinel))

	err := saveDelegationContract(t.TempDir(), &DelegationContract{ParentTaskID: "t1"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected yaml sentinel, got %v", err)
	}
}

func TestSaveMergeBack_YAMLMarshalError(t *testing.T) {
	sentinel := errors.New("yaml boom")
	withYAMLMarshalStub(t, yamlMarshalErrStub(sentinel))

	err := saveMergeBack(t.TempDir(), &MergeBackSummary{TaskID: "t1"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected yaml sentinel, got %v", err)
	}
}

func TestWriteFoldBackArtifact_YAMLMarshalError(t *testing.T) {
	sentinel := errors.New("yaml boom")
	withYAMLMarshalStub(t, yamlMarshalErrStub(sentinel))

	err := writeFoldBackArtifact(t.TempDir(), foldBackArtifact{ID: "fb1"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected yaml sentinel, got %v", err)
	}
}

func TestWriteFoldBackProposalFile_YAMLMarshalError(t *testing.T) {
	sentinel := errors.New("yaml boom")
	withYAMLMarshalStub(t, yamlMarshalErrStub(sentinel))

	path := filepath.Join(t.TempDir(), "p.md")
	err := writeFoldBackProposalFile(path, foldBackProposalFrontmatter{}, "body")
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected yaml sentinel, got %v", err)
	}
}

func TestSaveDelegationBundle_YAMLMarshalError(t *testing.T) {
	sentinel := errors.New("yaml boom")
	withYAMLMarshalStub(t, yamlMarshalErrStub(sentinel))

	err := saveDelegationBundle(t.TempDir(), &delegationBundleYAML{DelegationID: "d1"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected yaml sentinel, got %v", err)
	}
}

func TestPersistScopeEvidenceSidecar_YAMLMarshalError(t *testing.T) {
	sentinel := errors.New("yaml boom")
	withYAMLMarshalStub(t, yamlMarshalErrStub(sentinel))

	_, err := persistScopeEvidenceSidecar(t.TempDir(), "plan", "task", &ScopeEvidence{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected yaml sentinel wrapped, got %v", err)
	}
}

func TestSaveCanonicalPlan_YAMLMarshalError(t *testing.T) {
	sentinel := errors.New("yaml boom")
	withYAMLMarshalStub(t, yamlMarshalErrStub(sentinel))

	err := saveCanonicalPlan(t.TempDir(), &CanonicalPlan{ID: "p1"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected yaml sentinel, got %v", err)
	}
}

func TestSaveCanonicalTasks_YAMLMarshalError(t *testing.T) {
	sentinel := errors.New("yaml boom")
	withYAMLMarshalStub(t, yamlMarshalErrStub(sentinel))

	err := saveCanonicalTasks(t.TempDir(), &CanonicalTaskFile{PlanID: "p1"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected yaml sentinel, got %v", err)
	}
}

func TestWriteIterLogEntry_YAMLMarshalError(t *testing.T) {
	sentinel := errors.New("yaml boom")
	withYAMLMarshalStub(t, yamlMarshalErrStub(sentinel))

	iterPath := filepath.Join(t.TempDir(), "iter-1.yaml")
	err := writeIterLogEntry(iterPath, newValidIterLogEntry())
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected yaml sentinel wrapped, got %v", err)
	}
}

func TestWriteVerificationResultYAML_YAMLMarshalError(t *testing.T) {
	sentinel := errors.New("yaml boom")
	withYAMLMarshalStub(t, yamlMarshalErrStub(sentinel))

	err := writeVerificationResultYAML(t.TempDir(), newValidVerificationResultDoc())
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected yaml sentinel wrapped, got %v", err)
	}
}

func TestWriteReviewDecisionYAML_YAMLMarshalError(t *testing.T) {
	sentinel := errors.New("yaml boom")
	withYAMLMarshalStub(t, yamlMarshalErrStub(sentinel))

	err := writeReviewDecisionYAML(t.TempDir(), newValidReviewDecisionDoc())
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected yaml sentinel wrapped, got %v", err)
	}
}

func TestScaffoldGraphBridgeConfig_YAMLMarshalError(t *testing.T) {
	sentinel := errors.New("yaml boom")
	withYAMLMarshalStub(t, yamlMarshalErrStub(sentinel))

	_, err := scaffoldGraphBridgeConfig(t.TempDir())
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected yaml sentinel, got %v", err)
	}
}

// ─── jsonMarshal error branches ──────────────────────────────────────────────

func TestAppendVerificationLog_JSONMarshalError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	sentinel := errors.New("json boom")
	withJSONMarshalStub(t, jsonMarshalErrStub(sentinel))

	err := appendVerificationLog("p", VerificationRecord{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected json sentinel, got %v", err)
	}
}

func TestValidateWorkflowIterLogEntry_JSONMarshalError(t *testing.T) {
	sentinel := errors.New("json boom")
	withJSONMarshalStub(t, jsonMarshalErrStub(sentinel))

	err := validateWorkflowIterLogEntry(newValidIterLogEntry())
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected json sentinel wrapped, got %v", err)
	}
}

func TestValidateReviewDecisionDoc_JSONMarshalError(t *testing.T) {
	sentinel := errors.New("json boom")
	withJSONMarshalStub(t, jsonMarshalErrStub(sentinel))

	err := validateReviewDecisionDoc(newValidReviewDecisionDoc())
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected json sentinel wrapped, got %v", err)
	}
}

func TestValidateVerificationResultDoc_JSONMarshalError(t *testing.T) {
	sentinel := errors.New("json boom")
	withJSONMarshalStub(t, jsonMarshalErrStub(sentinel))

	err := validateVerificationResultDoc(newValidVerificationResultDoc())
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected json sentinel wrapped, got %v", err)
	}
}

// ─── jsonMarshalIndent error branches ────────────────────────────────────────

func TestSaveDriftReport_JSONMarshalIndentError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	sentinel := errors.New("json boom")
	withJSONMarshalIndentStub(t, jsonMarshalIndentErrStub(sentinel))

	err := saveDriftReport(AggregateDriftReport{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected json sentinel, got %v", err)
	}
}

func TestWriteHealthSnapshot_JSONMarshalIndentError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	sentinel := errors.New("json boom")
	withJSONMarshalIndentStub(t, jsonMarshalIndentErrStub(sentinel))

	err := writeHealthSnapshot("p", WorkflowHealthSnapshot{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected json sentinel, got %v", err)
	}
}

func TestWriteGraphBridgeHealth_JSONMarshalIndentError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	sentinel := errors.New("json boom")
	withJSONMarshalIndentStub(t, jsonMarshalIndentErrStub(sentinel))

	err := writeGraphBridgeHealth("p", GraphBridgeHealth{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected json sentinel, got %v", err)
	}
}

// ─── appendWorkflowSessionLog Fprintf-after-OpenFile error ───────────────────

// closedFileOpenStub returns a *os.File that has already been closed, so any
// Fprintf call against it returns os.ErrClosed. This drives the Fprintf-error
// branches in writers that defer-Close their file handle.
func closedFileOpenStub(t *testing.T) func(string, int, os.FileMode) (*os.File, error) {
	t.Helper()
	return func(string, int, os.FileMode) (*os.File, error) {
		f, err := os.CreateTemp(t.TempDir(), "closed-*.tmp")
		if err != nil {
			return nil, err
		}
		_ = f.Close()
		return f, nil
	}
}

func TestAppendWorkflowSessionLog_FprintfError(t *testing.T) {
	withOpenFileStub(t, closedFileOpenStub(t))

	err := appendWorkflowSessionLog(filepath.Join(t.TempDir(), "log.md"), workflowCheckpoint{})
	if err == nil {
		t.Fatal("expected fprintf-after-close error, got nil")
	}
}

func TestAppendVerificationLog_FprintfError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	withOpenFileStub(t, closedFileOpenStub(t))

	err := appendVerificationLog("p", VerificationRecord{})
	if err == nil {
		t.Fatal("expected fprintf-after-close error, got nil")
	}
}

// withFprintfStub swaps fprintfFunc for the duration of the test.
func withFprintfStub(t *testing.T, stub func(io.Writer, string, ...any) (int, error)) {
	t.Helper()
	prev := fprintfFunc
	fprintfFunc = stub
	t.Cleanup(func() { fprintfFunc = prev })
}

// failOnNthFprintf returns a stub that succeeds for the first n-1 calls
// then returns sentinel on the nth call onwards.
func failOnNthFprintf(n int, sentinel error) func(io.Writer, string, ...any) (int, error) {
	calls := 0
	return func(w io.Writer, format string, args ...any) (int, error) {
		calls++
		if calls >= n {
			return 0, sentinel
		}
		return fmt.Fprintf(w, format, args...)
	}
}

func TestAppendWorkflowSessionLog_FprintfErrorPerCall(t *testing.T) {
	for i := 1; i <= 7; i++ {
		i := i
		t.Run(fmt.Sprintf("call=%d", i), func(t *testing.T) {
			sentinel := errors.New("fprintf boom")
			withFprintfStub(t, failOnNthFprintf(i, sentinel))

			err := appendWorkflowSessionLog(filepath.Join(t.TempDir(), "log.md"), workflowCheckpoint{})
			if !errors.Is(err, sentinel) {
				t.Fatalf("expected fprintf sentinel on call %d, got %v", i, err)
			}
		})
	}
}

func TestAppendVerificationLog_FprintfSeamError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	sentinel := errors.New("fprintf boom")
	withFprintfStub(t, func(io.Writer, string, ...any) (int, error) {
		return 0, sentinel
	})

	err := appendVerificationLog("p", VerificationRecord{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected fprintf sentinel, got %v", err)
	}
}

// withGetwdError swaps osGetwd to a stub that returns the sentinel error,
// driving every handler's `currentWorkflowProject() -> err != nil` branch.
func withGetwdError(t *testing.T, sentinel error) {
	t.Helper()
	saved := osGetwd
	t.Cleanup(func() { osGetwd = saved })
	osGetwd = func() (string, error) { return "", sentinel }
}

// TestHandlers_GetwdErrorPropagates fires the osGetwd seam to error and
// invokes each handler that begins with `project, err := currentWorkflowProject()`.
// Each handler should return a non-nil error wrapping (or containing) the
// sentinel via the os.Getwd-failure path. This covers ~25 single-statement
// error-return branches in one test.
func TestHandlers_GetwdErrorPropagates(t *testing.T) {
	sentinel := errors.New("synthetic getwd error")
	withGetwdError(t, sentinel)

	cmd := &cobra.Command{}

	cases := []struct {
		name string
		fn   func() error
	}{
		{"runWorkflowAdvance", func() error { return runWorkflowAdvance("p", "t", "completed") }},
		{"runWorkflowCheckpoint", func() error { return runWorkflowCheckpoint("m", "pass", "s") }},
		{"runWorkflowCheckpointLogToIter", func() error { return runWorkflowCheckpointLogToIter(1, "impl", "unit") }},
		{"runWorkflowComplete", func() error { return runWorkflowComplete("p") }},
		{"runWorkflowDelegationCloseout", func() error { return runWorkflowDelegationCloseout(cmd, nil) }},
		{"runWorkflowDelegationGate", func() error { return runWorkflowDelegationGate(cmd, nil) }},
		{"runWorkflowEligible", func() error { return runWorkflowEligible("", 0) }},
		{"runWorkflowFanout", func() error { return runWorkflowFanout(cmd, nil) }},
		{"runWorkflowFoldBackList", func() error { return runWorkflowFoldBackList(cmd, nil) }},
		{"runWorkflowFoldBackUpsert", func() error { return runWorkflowFoldBackUpsert(cmd, false) }},
		{"runWorkflowLog", func() error { return runWorkflowLog(false) }},
		{"runWorkflowMergeBack", func() error { return runWorkflowMergeBack(cmd, nil) }},
		{"runWorkflowNext", func() error { return runWorkflowNext("p") }},
		{"runWorkflowPlanCheckScope", func() error { return runWorkflowPlanCheckScope("p", "t", nil, false) }},
		{"runWorkflowPlanCreate", func() error { return runWorkflowPlanCreate("p", "T", "s", "o", "sc", "vs") }},
		{"runWorkflowPlanDeriveScope", func() error { return runWorkflowPlanDeriveScope("p", "t", nil, nil) }},
		{"runWorkflowPlanGraph", func() error { return runWorkflowPlanGraph("p") }},
		{"runWorkflowPlanList", func() error { return runWorkflowPlanList() }},
		{"runWorkflowPlanSchedule", func() error { return runWorkflowPlanSchedule("p") }},
		{"runWorkflowPlanShow", func() error { return runWorkflowPlanShow("p") }},
		{"runWorkflowPlanUpdate", func() error { return runWorkflowPlanUpdate("p", "active", "T", "s", "f", "sc", "vs") }},
		{"runWorkflowPrefs", func() error { return runWorkflowPrefs() }},
		{"runWorkflowPrefsSetLocal", func() error { return runWorkflowPrefsSetLocal("planning.dry_run_first", "true") }},
		{"runWorkflowPrefsSetShared", func() error { return runWorkflowPrefsSetShared("planning.dry_run_first", "true") }},
		{"runWorkflowSlices", func() error { return runWorkflowSlices("p") }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.fn()
			if err == nil {
				t.Fatalf("%s: expected non-nil error from getwd failure", tc.name)
			}
		})
	}
}
