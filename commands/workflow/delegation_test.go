package workflow

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/testutil"
	"github.com/spf13/cobra"
)

func TestRunWorkflowFoldBackList_NoArtifacts(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	out := executeWorkflowCommandOutput(t, repo, "fold-back", "list")
	if !strings.Contains(out, "No fold-back observations") {
		t.Errorf("expected no-artifacts message, got: %s", out)
	}
}

func TestRunWorkflowFoldBackList_NoArtifactsJSON(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = false })
	out := executeWorkflowCommandOutput(t, repo, "fold-back", "list")
	if !strings.Contains(out, "[]") {
		t.Errorf("expected empty JSON array, got: %s", out)
	}
}

func TestWriteAndLoadFoldBackArtifact(t *testing.T) {
	dir := t.TempDir()
	artifact := foldBackArtifact{
		ID:             "fold-test-1",
		PlanID:         "p1",
		TaskID:         "t1",
		Classification: "small",
		Observation:    "tests should cover this",
		RoutedTo:       "task-note",
		CreatedAt:      time.Now().UTC().Format(time.RFC3339),
	}
	if err := writeFoldBackArtifact(dir, artifact); err != nil {
		t.Fatalf("writeFoldBackArtifact: %v", err)
	}
	got, err := loadFoldBackArtifactByID(dir, artifact.ID)
	if err != nil {
		t.Fatalf("loadFoldBackArtifactByID: %v", err)
	}
	if got.ID != artifact.ID || got.PlanID != "p1" {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}

func TestLoadFoldBackArtifactByID_MissingFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := loadFoldBackArtifactByID(dir, "does-not-exist"); err == nil {
		t.Error("expected error for missing artifact")
	}
}

func TestCheckPreVerifierTDDGate_SkipFlag(t *testing.T) {
	if err := checkPreVerifierTDDGate(t.TempDir(), []string{"commands/x.go"}, true, true); err != nil {
		t.Errorf("skip=true should bypass gate, got: %v", err)
	}
}

func TestCheckPreVerifierTDDGate_VerificationNotRequired(t *testing.T) {
	if err := checkPreVerifierTDDGate(t.TempDir(), []string{"commands/x.go"}, false, false); err != nil {
		t.Errorf("verification_required=false should bypass gate, got: %v", err)
	}
}

func TestCheckPreVerifierTDDGate_NonGoScope(t *testing.T) {
	if err := checkPreVerifierTDDGate(t.TempDir(), []string{"docs/x.md"}, true, false); err != nil {
		t.Errorf("non-Go write_scope should bypass gate, got: %v", err)
	}
}

func TestCheckPreVerifierTDDGate_HasAdjacentTests(t *testing.T) {
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "commands")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "x_test.go"), []byte("package commands\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := checkPreVerifierTDDGate(dir, []string{"commands/x.go"}, true, false); err != nil {
		t.Errorf("adjacent test should bypass gate, got: %v", err)
	}
}

func TestCheckPreVerifierTDDGate_NoTestsFails(t *testing.T) {
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "commands")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := checkPreVerifierTDDGate(dir, []string{"commands/x.go"}, true, false); err == nil {
		t.Error("expected TDD gate failure when verification required, Go scope, no tests")
	}
}

func TestWriteScopeHasAdjacentGoTests_DirectoryWithTest(t *testing.T) {
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "internal", "foo")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "foo_test.go"), []byte("package foo\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if !writeScopeHasAdjacentGoTests(dir, []string{"internal/foo"}) {
		t.Error("expected directory-scope to detect adjacent test file")
	}
}

func TestWriteScopeHasAdjacentGoTests_NoTests(t *testing.T) {
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "internal", "foo")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	if writeScopeHasAdjacentGoTests(dir, []string{"internal/foo/main.go"}) {
		t.Error("expected no adjacent tests when none on disk")
	}
}

func TestWriteScopeImpliesNonTestGo(t *testing.T) {
	if !writeScopeImpliesNonTestGo([]string{"x.go"}) {
		t.Error("plain .go file should imply non-test Go scope")
	}
	if writeScopeImpliesNonTestGo([]string{"x_test.go"}) {
		t.Error("_test.go should not imply non-test Go scope")
	}
	if writeScopeImpliesNonTestGo([]string{"docs/x.md"}) {
		t.Error("non-Go file should not imply non-test Go scope")
	}
}

func TestCheckFanoutWriteScopeConflicts_DetectsOverlap(t *testing.T) {
	repo := t.TempDir()

	now := time.Now().UTC().Format(time.RFC3339)
	existing := &DelegationContract{
		SchemaVersion: 1, ID: "del-existing", ParentPlanID: "p1", ParentTaskID: "tx",
		Title: "x", WriteScope: []string{"commands/"}, Status: "active",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := saveDelegationContract(repo, existing); err != nil {
		t.Fatal(err)
	}

	err := checkFanoutWriteScopeConflicts(repo, []string{"commands/foo.go"}, "ty")
	if err == nil || !strings.Contains(err.Error(), "write scope overlaps") {
		t.Fatalf("expected overlap error, got %v", err)
	}
}

func TestCheckFanoutWriteScopeConflicts_SameTaskAllowed(t *testing.T) {
	repo := t.TempDir()
	now := time.Now().UTC().Format(time.RFC3339)
	existing := &DelegationContract{
		SchemaVersion: 1, ID: "del-tx", ParentPlanID: "p1", ParentTaskID: "tx",
		Title: "x", WriteScope: []string{"commands/"}, Status: "active",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := saveDelegationContract(repo, existing); err != nil {
		t.Fatal(err)
	}

	if err := checkFanoutWriteScopeConflicts(repo, []string{"commands/foo.go"}, "tx"); err != nil {
		t.Fatalf("same task should not conflict: %v", err)
	}
}

// TestCheckFanoutWriteScopeConflicts_CorruptContractFailsClosed seeds a
// corrupt delegation contract file alongside a valid one and asserts the
// conflict check fails closed (returns a non-nil error) and warns loudly
// naming the corrupt file, instead of silently excluding its unknowable
// write_scope from overlap detection.
func TestCheckFanoutWriteScopeConflicts_CorruptContractFailsClosed(t *testing.T) {
	repo := t.TempDir()
	now := time.Now().UTC().Format(time.RFC3339)
	valid := &DelegationContract{
		SchemaVersion: 1, ID: "del-valid", ParentPlanID: "p1", ParentTaskID: "tx",
		Title: "x", WriteScope: []string{"internal/"}, Status: "active",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := saveDelegationContract(repo, valid); err != nil {
		t.Fatalf("seed valid contract: %v", err)
	}
	corrupt := filepath.Join(delegationDir(repo), "ty.yaml")
	if err := os.WriteFile(corrupt, []byte("not: [valid: yaml"), 0644); err != nil {
		t.Fatalf("seed corrupt contract: %v", err)
	}

	var err error
	out := captureStdoutToString(t, func() {
		err = checkFanoutWriteScopeConflicts(repo, []string{"commands/foo.go"}, "tz")
	})
	if err == nil {
		t.Fatal("expected checkFanoutWriteScopeConflicts to fail closed on a corrupt contract")
	}
	if !strings.Contains(err.Error(), "unreadable/corrupt") {
		t.Fatalf("expected fail-closed error to name the corrupt state, got: %v", err)
	}
	if !strings.Contains(out, "ty.yaml") {
		t.Fatalf("expected a loud warning naming the corrupt file, got: %q", out)
	}
}

// TestCheckFanoutWriteScopeConflicts_UnreadableContractFailsClosed reuses
// internal/testutil's cross-platform unreadable-file helper (rather than a
// raw chmod) to cover the permission-denied read variant of the same
// fail-closed contract.
func TestCheckFanoutWriteScopeConflicts_UnreadableContractFailsClosed(t *testing.T) {
	repo := t.TempDir()
	now := time.Now().UTC().Format(time.RFC3339)
	valid := &DelegationContract{
		SchemaVersion: 1, ID: "del-valid", ParentPlanID: "p1", ParentTaskID: "tx",
		Title: "x", WriteScope: []string{"internal/"}, Status: "active",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := saveDelegationContract(repo, valid); err != nil {
		t.Fatalf("seed valid contract: %v", err)
	}
	unreadable := &DelegationContract{
		SchemaVersion: 1, ID: "del-unreadable", ParentPlanID: "p1", ParentTaskID: "ty",
		Title: "y", WriteScope: []string{"internal/"}, Status: "active",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := saveDelegationContract(repo, unreadable); err != nil {
		t.Fatalf("seed unreadable contract: %v", err)
	}
	testutil.MakeFileUnreadable(t, filepath.Join(delegationDir(repo), "ty.yaml"))

	err := checkFanoutWriteScopeConflicts(repo, []string{"commands/foo.go"}, "tz")
	if err == nil || !strings.Contains(err.Error(), "unreadable/corrupt") {
		t.Fatalf("expected fail-closed error, got %v", err)
	}
}

// TestListDelegationContractsWithWarnings_CorruptEntryWarnsAndExcludes covers
// the warnings-aware core directly: a corrupt entry is excluded from the
// returned contracts (matching listDelegationContracts's historical
// best-effort behavior for its other callers) but produces exactly one
// warning naming the offending file, rather than vanishing without a trace.
func TestListDelegationContractsWithWarnings_CorruptEntryWarnsAndExcludes(t *testing.T) {
	repo := t.TempDir()
	saveTestDelegationContract(t, repo, "task-good", "plan", "del-good")
	if err := os.WriteFile(filepath.Join(delegationDir(repo), "broken.yaml"), []byte("not: valid: yaml"), 0644); err != nil {
		t.Fatal(err)
	}

	contracts, warnings, err := listDelegationContractsWithWarnings(repo)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(contracts) != 1 {
		t.Fatalf("expected 1 valid contract, got %d", len(contracts))
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "broken.yaml") {
		t.Fatalf("expected one warning naming broken.yaml, got %v", warnings)
	}
}

// TestListDelegationContractsWithWarnings_EmptyDirNoWarnings keeps the
// legitimate-absence case (no delegation dir yet, e.g. a fresh project) free
// of spurious warnings — only actual read/parse failures should surface.
func TestListDelegationContractsWithWarnings_EmptyDirNoWarnings(t *testing.T) {
	repo := t.TempDir()
	contracts, warnings, err := listDelegationContractsWithWarnings(repo)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(contracts) != 0 || len(warnings) != 0 {
		t.Fatalf("expected no contracts/warnings for legitimate-absence dir, got %d/%d", len(contracts), len(warnings))
	}
}

// TestListDelegationContracts_WarnsOnCorruptEntry asserts the
// listDelegationContracts wrapper (used by loadActiveDelegationTaskSet and
// the workflow-state delegation summary) still surfaces a loud warning for a
// corrupt entry even though its own return signature stays best-effort
// (nil error, entry silently excluded from the slice) for backward
// compatibility with those out-of-scope callers.
func TestListDelegationContracts_WarnsOnCorruptEntry(t *testing.T) {
	repo := t.TempDir()
	saveTestDelegationContract(t, repo, "task-good", "plan", "del-good")
	if err := os.WriteFile(filepath.Join(delegationDir(repo), "broken.yaml"), []byte("not: valid: yaml"), 0644); err != nil {
		t.Fatal(err)
	}

	var contracts []DelegationContract
	var err error
	out := captureStdoutToString(t, func() {
		contracts, err = listDelegationContracts(repo)
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(contracts) != 1 {
		t.Fatalf("expected 1 valid contract, got %d", len(contracts))
	}
	if !strings.Contains(out, "broken.yaml") {
		t.Fatalf("expected a loud warning naming broken.yaml, got: %q", out)
	}
}

func TestPersistFanoutBundle_BundleSaveRollback(t *testing.T) {
	repo := t.TempDir()
	now := time.Now().UTC().Format(time.RFC3339)
	contract := &DelegationContract{
		SchemaVersion: 1, ID: "del-1", ParentPlanID: "p1", ParentTaskID: "t1",
		Title: "x", WriteScope: []string{"commands/"}, Status: "active",
		CreatedAt: now, UpdatedAt: now,
	}
	// The fanout flow now materializes the contract (writes it to disk) before
	// the bundle is built. persistFanoutBundle's contract is the prior write
	// to roll back if the bundle save fails.
	if err := saveDelegationContract(repo, contract); err != nil {
		t.Fatalf("seed contract: %v", err)
	}

	bundle := &delegationBundleYAML{}

	err := persistFanoutBundle(repo, contract, bundle)
	if err == nil || !strings.Contains(err.Error(), "save delegation bundle") {
		t.Fatalf("expected bundle save error, got %v", err)
	}

	contractPath := filepath.Join(delegationDir(repo), "t1.yaml")
	if _, err := os.Stat(contractPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected contract file to be removed after bundle save failure, got stat err=%v", err)
	}
}

// TestPersistFanoutBundle_RemoveFailureLogsWarning covers the rollback path
// when saveDelegationBundleWithBase fails AND the subsequent orphan-contract
// os.Remove also fails: the bundle-save error must still surface, plus a
// loud warning naming the orphaned contract path (rather than the historical
// `_ = os.Remove(...)`, which discarded the second failure entirely).
func TestPersistFanoutBundle_RemoveFailureLogsWarning(t *testing.T) {
	repo := t.TempDir()
	now := time.Now().UTC().Format(time.RFC3339)
	contract := &DelegationContract{
		SchemaVersion: 1, ID: "del-1", ParentPlanID: "p1", ParentTaskID: "t1",
		Title: "x", WriteScope: []string{"commands/"}, Status: "active",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := saveDelegationContract(repo, contract); err != nil {
		t.Fatalf("seed contract: %v", err)
	}

	// Deny delete on the delegation dir's children so the rollback
	// os.Remove(contractPath) fails after the (unrelated, validation-only)
	// bundle save failure below — saveDelegationBundleWithBase writes to a
	// different directory (delegationBundlesDir), so this denial only
	// affects the rollback step under test.
	testutil.MakeDirWriteDenied(t, delegationDir(repo))

	bundle := &delegationBundleYAML{} // empty DelegationID -> save fails before touching disk

	var err error
	out := captureStdoutToString(t, func() {
		err = persistFanoutBundle(repo, contract, bundle)
	})
	if err == nil || !strings.Contains(err.Error(), "save delegation bundle") {
		t.Fatalf("expected bundle save error, got %v", err)
	}
	if !strings.Contains(out, "rollback could not remove orphaned delegation contract") {
		t.Fatalf("expected rollback-remove-failure warning, got: %q", out)
	}

	contractPath := filepath.Join(delegationDir(repo), "t1.yaml")
	if _, statErr := os.Stat(contractPath); statErr != nil {
		t.Errorf("expected contract file to survive the denied rollback, got stat err=%v", statErr)
	}
}

func TestValidateFoldBackPriorAgreement_PlanMismatch(t *testing.T) {
	prior := &foldBackArtifact{ID: "fb1", PlanID: "p1", Classification: "small"}
	in := &foldBackUpsertInputs{slug: "fb1", planID: "p2"}
	err := validateFoldBackPriorAgreement(prior, in)
	if err == nil || !strings.Contains(err.Error(), "belongs to plan") {
		t.Fatalf("expected plan mismatch error, got %v", err)
	}
}

func TestValidateFoldBackPriorAgreement_ProposeWithExisting(t *testing.T) {
	prior := &foldBackArtifact{ID: "fb1", PlanID: "p1", Classification: "small"}
	in := &foldBackUpsertInputs{slug: "fb1", planID: "p1", propose: true}
	err := validateFoldBackPriorAgreement(prior, in)
	if err == nil || !strings.Contains(err.Error(), "--propose is not valid") {
		t.Fatalf("expected propose error, got %v", err)
	}
}

func TestValidateFoldBackPriorAgreement_ProposalClassificationOK(t *testing.T) {
	prior := &foldBackArtifact{ID: "fb1", PlanID: "p1", Classification: "proposal"}
	in := &foldBackUpsertInputs{slug: "fb1", planID: "p1"}
	if err := validateFoldBackPriorAgreement(prior, in); err != nil {
		t.Fatalf("proposal classification should pass through: %v", err)
	}
}

func TestValidateFoldBackPriorAgreement_TaskScopedMissing(t *testing.T) {
	prior := &foldBackArtifact{ID: "fb1", PlanID: "p1", Classification: "small", TaskID: "t1"}
	in := &foldBackUpsertInputs{slug: "fb1", planID: "p1"}
	err := validateFoldBackPriorAgreement(prior, in)
	if err == nil || !strings.Contains(err.Error(), "task-scoped") {
		t.Fatalf("expected task-scoped error, got %v", err)
	}
}

func TestValidateFoldBackPriorAgreement_TaskScopedMismatch(t *testing.T) {
	prior := &foldBackArtifact{ID: "fb1", PlanID: "p1", Classification: "small", TaskID: "t1"}
	in := &foldBackUpsertInputs{slug: "fb1", planID: "p1", taskID: "t2"}
	err := validateFoldBackPriorAgreement(prior, in)
	if err == nil || !strings.Contains(err.Error(), "does not match fold-back scope") {
		t.Fatalf("expected task mismatch error, got %v", err)
	}
}

func TestValidateFoldBackPriorAgreement_TaskScopedMatches(t *testing.T) {
	prior := &foldBackArtifact{ID: "fb1", PlanID: "p1", Classification: "small", TaskID: "t1"}
	in := &foldBackUpsertInputs{slug: "fb1", planID: "p1", taskID: "t1"}
	if err := validateFoldBackPriorAgreement(prior, in); err != nil {
		t.Fatalf("matching task should pass: %v", err)
	}
}

func TestValidateFoldBackPriorAgreement_PlanScopedRejectsTask(t *testing.T) {
	prior := &foldBackArtifact{ID: "fb1", PlanID: "p1", Classification: "small"}
	in := &foldBackUpsertInputs{slug: "fb1", planID: "p1", taskID: "t1"}
	err := validateFoldBackPriorAgreement(prior, in)
	if err == nil || !strings.Contains(err.Error(), "plan-scoped") {
		t.Fatalf("expected plan-scoped error, got %v", err)
	}
}

func TestDispatchFoldBackUpsert_RoutingError(t *testing.T) {

	prior := &foldBackArtifact{Classification: "unknown"}
	in := &foldBackUpsertInputs{slug: "fb1"}
	artifact := &foldBackArtifact{}
	err := dispatchFoldBackUpsert(t.TempDir(), in, prior, true, 0, "", artifact)
	if err == nil || !strings.Contains(err.Error(), "internal fold-back routing error") {
		t.Fatalf("expected routing error, got %v", err)
	}
}

func TestUpdateTaskFoldBackNote_TaskNotFound(t *testing.T) {
	repo := t.TempDir()
	if err := saveCanonicalTasks(repo, &CanonicalTaskFile{
		SchemaVersion: 1,
		PlanID:        "p1",
		Tasks:         []CanonicalTask{{ID: "t1", Title: "T", Status: "pending"}},
	}); err != nil {
		t.Fatal(err)
	}
	err := updateTaskFoldBackNote(repo, "p1", "nonexistent", func(n string) string { return n })
	if err == nil {
		t.Fatal("expected error for missing task")
	}
}

func TestUpdateTaskFoldBackNote_LoadError(t *testing.T) {

	err := updateTaskFoldBackNote(t.TempDir(), "missing-plan", "t1", func(n string) string { return n })
	if err == nil {
		t.Fatal("expected load tasks error")
	}
}

func TestWorkflowDelegationGate_TextOutput_MissingDecision(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	saveTestDelegationContract(t, repo, "t1", "p1", "del-t1")
	writeMergeBackFixture(t, repo, "t1", "p1")

	out := executeWorkflowCommandOutput(t, repo, "delegation", "gate", "--plan", "p1", "--task", "t1")
	if !strings.Contains(out, "review_overall_decision: missing") {
		t.Fatalf("expected missing-decision text line, got:\n%s", out)
	}
}

func TestMustGetStringSlice_NoFlagReturnsNil(t *testing.T) {
	c := &cobra.Command{}
	got := mustGetStringSlice(c, "absent-name")
	if got != nil {
		t.Fatalf("expected nil for missing flag, got %v", got)
	}
}

func TestMustGetStringSlice_WrongTypeReturnsNil(t *testing.T) {
	c := &cobra.Command{}

	c.Flags().String("wrong-type", "", "")
	got := mustGetStringSlice(c, "wrong-type")
	if got != nil {
		t.Fatalf("expected nil for wrong-type flag, got %v", got)
	}
}

func TestListDelegationContracts_ReadDirError(t *testing.T) {
	repo := t.TempDir()
	dir := filepath.Join(repo, ".agents", "active", "delegation")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	chmodUnreadableDir(t, dir)
	_, err := listDelegationContracts(repo)
	if err == nil {
		t.Fatal("expected ReadDir error")
	}
}

func TestValidateInsideProjectPath_Traversal(t *testing.T) {
	repo := t.TempDir()
	if _, err := validateInsideProjectPath(repo, ".."); err == nil {
		t.Fatal("expected rejection of traversal")
	}
	if _, err := validateInsideProjectPath(repo, "ok/path"); err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
	if _, err := validateInsideProjectPath(repo, ""); err == nil {
		t.Fatal("expected rejection of empty")
	}
}

func TestValidateProjectFileRef_RejectsBad(t *testing.T) {
	repo := t.TempDir()
	if _, err := validateProjectFileRef(repo, ""); err == nil {
		t.Fatal("empty rejected")
	}
	if _, err := validateProjectFileRef(repo, "../escape"); err == nil {
		t.Fatal("traversal rejected")
	}
	if _, err := validateProjectFileRef(repo, "missing.txt"); err == nil {
		t.Fatal("non-existent rejected")
	}
}

func TestSaveDelegationContract_NoNameDoesNotPanic(t *testing.T) {

	c := &DelegationContract{ParentTaskID: "x"}
	if err := saveDelegationContract(t.TempDir(), c); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestParseFoldBackUpsertInputs_RequiresObservation(t *testing.T) {
	_, err := parseFoldBackUpsertInputs(newFoldBackTestCmd("", "", "p1"), false)
	if err == nil || !strings.Contains(err.Error(), "observation text is required") {
		t.Fatalf("expected obs required error, got %v", err)
	}
}

func TestParseFoldBackUpsertInputs_RequiresSlugForUpdate(t *testing.T) {
	_, err := parseFoldBackUpsertInputs(newFoldBackTestCmd("hi", "", "p1"), true)
	if err == nil || !strings.Contains(err.Error(), "--slug is required") {
		t.Fatalf("expected slug-required error, got %v", err)
	}
}

func TestParseFoldBackUpsertInputs_InvalidSlug(t *testing.T) {
	_, err := parseFoldBackUpsertInputs(newFoldBackTestCmd("hi", "bad slug!", "p1"), false)
	if err == nil {
		t.Fatal("expected invalid-slug error")
	}
}

func TestLoadPriorFoldBackArtifact_EmptySlug(t *testing.T) {
	a, ok, err := loadPriorFoldBackArtifact(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if a != nil || ok {
		t.Fatalf("expected nil,false for empty slug")
	}
}

func TestLoadPriorFoldBackArtifact_NotExist(t *testing.T) {
	a, ok, err := loadPriorFoldBackArtifact(t.TempDir(), "missing")
	if err != nil {
		t.Fatal(err)
	}
	if a != nil || ok {
		t.Fatalf("expected nil,false for missing slug")
	}
}

func TestRunWorkflowFoldBackList_MissingDirText(t *testing.T) {
	repo := setupTestProject(t)
	chdirForCov(t, repo)
	cmd := &cobra.Command{}
	cmd.Flags().String("plan", "", "")
	var buf strings.Builder
	cmd.SetOut(&buf)
	if err := runWorkflowFoldBackList(cmd, nil); err != nil {
		t.Fatalf("foldback list: %v", err)
	}
	if !strings.Contains(buf.String(), "No fold-back observations recorded") {
		t.Fatalf("expected empty message, got %s", buf.String())
	}
}

func TestRunWorkflowFoldBackList_MissingDirJSON(t *testing.T) {
	repo := setupTestProject(t)
	chdirForCov(t, repo)
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = false })
	cmd := &cobra.Command{}
	cmd.Flags().String("plan", "", "")
	var buf strings.Builder
	cmd.SetOut(&buf)
	if err := runWorkflowFoldBackList(cmd, nil); err != nil {
		t.Fatalf("foldback list json: %v", err)
	}
	if !strings.Contains(buf.String(), "[]") {
		t.Fatalf("expected empty-JSON array, got %s", buf.String())
	}
}

func TestEnsureTaskVerificationDir(t *testing.T) {
	repo := t.TempDir()
	if err := ensureTaskVerificationDir(repo, "task-x"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".agents", "active", "verification", "task-x")); err != nil {
		t.Fatalf("expected dir, got %v", err)
	}
}

func TestWriteScopeHasAdjacentGoTests_HasTests(t *testing.T) {
	repo := t.TempDir()
	dir := filepath.Join(repo, "commands", "foo")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "foo.go"), []byte("package foo"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "foo_test.go"), []byte("package foo"), 0644); err != nil {
		t.Fatal(err)
	}
	got := writeScopeHasAdjacentGoTests(repo, []string{"commands/foo/foo.go"})
	if !got {
		t.Fatal("expected adjacent test detected")
	}
}

func TestWriteScopeHasAdjacentGoTests_NoTests_Push7(t *testing.T) {
	repo := t.TempDir()
	dir := filepath.Join(repo, "commands", "bar")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bar.go"), []byte("package bar"), 0644); err != nil {
		t.Fatal(err)
	}
	got := writeScopeHasAdjacentGoTests(repo, []string{"commands/bar/bar.go"})
	if got {
		t.Fatal("expected no adjacent test")
	}
}

func TestWriteScopeHasAdjacentGoTests_DirEntry(t *testing.T) {
	repo := t.TempDir()
	dir := filepath.Join(repo, "internal", "x")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "x_test.go"), []byte("package x"), 0644); err != nil {
		t.Fatal(err)
	}
	if !writeScopeHasAdjacentGoTests(repo, []string{"internal/x"}) {
		t.Fatal("expected test detected via dir entry")
	}
}

func TestWriteScopeImpliesNonTestGo_GoFile(t *testing.T) {
	if !writeScopeImpliesNonTestGo([]string{"commands/x.go"}) {
		t.Fatal("expected true")
	}
}

func TestWriteScopeImpliesNonTestGo_OnlyTests(t *testing.T) {
	if writeScopeImpliesNonTestGo([]string{"commands/x_test.go"}) {
		t.Fatal("expected false for tests-only")
	}
}

func TestWriteScopeImpliesNonTestGo_DocOnly(t *testing.T) {
	if writeScopeImpliesNonTestGo([]string{"README.md", "docs/x.md"}) {
		t.Fatal("expected false for docs-only")
	}
}

func TestCheckPreVerifierTDDGate_Skip(t *testing.T) {
	if err := checkPreVerifierTDDGate(t.TempDir(), []string{"x.go"}, true, true); err != nil {
		t.Fatalf("skip path should pass, got %v", err)
	}
}

func TestCheckPreVerifierTDDGate_NotRequired(t *testing.T) {
	if err := checkPreVerifierTDDGate(t.TempDir(), []string{"x.go"}, false, false); err != nil {
		t.Fatalf("not-required path should pass, got %v", err)
	}
}

func TestCheckPreVerifierTDDGate_DocOnly(t *testing.T) {
	if err := checkPreVerifierTDDGate(t.TempDir(), []string{"README.md"}, true, false); err != nil {
		t.Fatalf("doc-only path should pass, got %v", err)
	}
}

func TestCheckPreVerifierTDDGate_HasAdjacentTest(t *testing.T) {
	repo := t.TempDir()
	dir := filepath.Join(repo, "pkg")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pkg.go"), []byte("package pkg"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pkg_test.go"), []byte("package pkg"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := checkPreVerifierTDDGate(repo, []string{"pkg/pkg.go"}, true, false); err != nil {
		t.Fatalf("adjacent-test path should pass, got %v", err)
	}
}

func TestCheckPreVerifierTDDGate_Fails(t *testing.T) {
	repo := t.TempDir()
	dir := filepath.Join(repo, "pkg")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pkg.go"), []byte("package pkg"), 0644); err != nil {
		t.Fatal(err)
	}
	err := checkPreVerifierTDDGate(repo, []string{"pkg/pkg.go"}, true, false)
	if err == nil || !strings.Contains(err.Error(), "pre-verifier TDD gate") {
		t.Fatalf("expected TDD-gate fail, got %v", err)
	}
}

func TestResolveFanoutSliceTask_EmptyReturnsTask(t *testing.T) {
	tid, ws, err := resolveFanoutSliceTask(t.TempDir(), "p", "", "tx", false)
	if err != nil {
		t.Fatal(err)
	}
	if tid != "tx" || ws != nil {
		t.Fatalf("expected pass-through, got tid=%q ws=%v", tid, ws)
	}
}

func TestResolveFanoutSliceTask_MissingSlice(t *testing.T) {
	repo := setupFanoutSliceProject(t, "pending")
	_, _, err := resolveFanoutSliceTask(repo, "p1", "missing-slice", "", false)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected slice-not-found, got %v", err)
	}
}

func TestResolveFanoutSliceTask_CompletedSliceRejected(t *testing.T) {
	repo := setupFanoutSliceProject(t, "completed")
	_, _, err := resolveFanoutSliceTask(repo, "p1", "s1", "", false)
	if err == nil || !strings.Contains(err.Error(), "already completed") {
		t.Fatalf("expected completed-rejection, got %v", err)
	}
}

func TestResolveFanoutTargetTask_TaskNotFound(t *testing.T) {
	tf := &CanonicalTaskFile{PlanID: "p", Tasks: []CanonicalTask{{ID: "t1", Status: "pending"}}}
	_, err := resolveFanoutTargetTask(tf, "missing", "p")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected task-not-found, got %v", err)
	}
}

func TestResolveFanoutTargetTask_BadStatus(t *testing.T) {
	tf := &CanonicalTaskFile{PlanID: "p", Tasks: []CanonicalTask{{ID: "t1", Status: "completed"}}}
	_, err := resolveFanoutTargetTask(tf, "t1", "p")
	if err == nil || !strings.Contains(err.Error(), "only pending or in_progress") {
		t.Fatalf("expected status-rejection, got %v", err)
	}
}

func TestParseFoldBackUpsertInputs_Happy(t *testing.T) {
	in, err := parseFoldBackUpsertInputs(newFoldBackTestCmd("body", "valid-slug", "p1"), false)
	if err != nil {
		t.Fatal(err)
	}
	if in.slug != "valid-slug" || in.observation != "body" {
		t.Fatalf("unexpected: %+v", in)
	}
}

func TestLoadPriorFoldBackArtifact_Happy(t *testing.T) {
	repo := t.TempDir()
	dir := foldBackDir(repo)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := writeFoldBackArtifact(repo, foldBackArtifact{
		ID:             "good-slug",
		Classification: "small",
		PlanID:         "p1",
		CreatedAt:      "2026-05-12T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	a, ok, err := loadPriorFoldBackArtifact(repo, "good-slug")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || a == nil || a.PlanID != "p1" {
		t.Fatalf("expected loaded artifact, got ok=%v a=%+v", ok, a)
	}
}

func TestDispatchFoldBackUpsert_DefaultErrors(t *testing.T) {

	prior := &foldBackArtifact{Classification: "bogus"}
	in := &foldBackUpsertInputs{planID: "p1", observation: "x"}
	artifact := &foldBackArtifact{}
	err := dispatchFoldBackUpsert(t.TempDir(), in, prior, true, 1, "2026-05-12T00:00:00Z", artifact)
	if err == nil || !strings.Contains(err.Error(), "internal fold-back routing") {
		t.Fatalf("expected routing error, got %v", err)
	}
}

func TestValidatePriorFoldBack_SmallWithPropose(t *testing.T) {
	prior := &foldBackArtifact{Classification: "small", PlanID: "p1"}
	in := &foldBackUpsertInputs{planID: "p1", slug: "x", propose: true}
	err := validatePriorFoldBack(prior, in)
	if err == nil {
		t.Fatal("expected error for propose-on-existing-small")
	}
}

func TestCheckFanoutScopeEvidenceWarnings_NoSidecarSilent(t *testing.T) {

	checkFanoutScopeEvidenceWarnings(t.TempDir(), "p1", "t1", false)
}

func TestCheckFanoutScopeEvidenceWarnings_LowConfidence(t *testing.T) {
	repo := t.TempDir()
	dir := filepath.Join(repo, ".agents", "workflow", "plans", "p1", "evidence")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "t1.scope.yaml"), []byte("confidence: low\n"), 0644); err != nil {
		t.Fatal(err)
	}
	checkFanoutScopeEvidenceWarnings(repo, "p1", "t1", false)
}

func TestCheckFanoutScopeEvidenceWarnings_SkipShortCircuit(t *testing.T) {
	checkFanoutScopeEvidenceWarnings(t.TempDir(), "p1", "t1", true)
}

func TestListDelegationContracts_SkipsNonYaml(t *testing.T) {
	repo := t.TempDir()
	dir := filepath.Join(repo, ".agents", "active", "delegation")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("ignore"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "subdir"), 0755); err != nil {
		t.Fatal(err)
	}
	out, err := listDelegationContracts(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Fatalf("expected zero contracts, got %d", len(out))
	}
}

func TestListDelegationContracts_SkipsUnreadable(t *testing.T) {
	repo := t.TempDir()
	dir := filepath.Join(repo, ".agents", "active", "delegation")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(dir, "task-bad.yaml")
	if err := os.WriteFile(bad, []byte(":\n  - bad: ["), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := listDelegationContracts(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Fatalf("expected unreadable to be skipped, got %d", len(out))
	}
}

func TestWriteFoldBackArtifact_Happy(t *testing.T) {
	repo := t.TempDir()
	if err := writeFoldBackArtifact(repo, foldBackArtifact{
		ID: "good", Classification: "small", PlanID: "p1",
		CreatedAt: "2026-05-12T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestReconcileDelegationContractForCloseout_PlanMismatch(t *testing.T) {
	repo := setupTestProject(t)
	saveTestDelegationContract(t, repo, "task-001", "plan-001", "del-x")
	_, err := reconcileDelegationContractForCloseout(repo, "task-001", "wrong-plan")
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("expected plan-id mismatch, got %v", err)
	}
}

func TestReconcileDelegationContractForCloseout_PromotesActiveToCompleted(t *testing.T) {
	repo := setupTestProject(t)
	saveTestDelegationContract(t, repo, "task-001", "plan-001", "del-x")
	c, err := reconcileDelegationContractForCloseout(repo, "task-001", "plan-001")
	if err != nil {
		t.Fatalf("expected reconcile to succeed, got %v", err)
	}
	if c.Status != "completed" {
		t.Fatalf("expected reconcile to promote status to completed, got %q", c.Status)
	}
}

func TestReconcileDelegationContractForCloseout_MissingContract(t *testing.T) {
	repo := setupTestProject(t)
	_, err := reconcileDelegationContractForCloseout(repo, "missing-task", "plan-001")
	if err == nil || !strings.Contains(err.Error(), "delegation contract for task missing-task") {
		t.Fatalf("expected missing-contract error, got %v", err)
	}
}

func TestReconcileDelegationContractForCloseout_SaveError(t *testing.T) {
	repo := setupTestProject(t)
	saveTestDelegationContract(t, repo, "task-001", "plan-001", "del-x")

	sentinel := errors.New("write boom")
	withWriteFileStub(t, func(string, []byte, os.FileMode) error { return sentinel })

	_, err := reconcileDelegationContractForCloseout(repo, "task-001", "plan-001")
	if err == nil || !strings.Contains(err.Error(), "reconcile delegation status") {
		t.Fatalf("expected save error, got %v", err)
	}
}

func TestApplyCloseoutDecisionToTasks_TaskNotFound(t *testing.T) {
	repo := setupTestProject(t)
	closeout := workflowDelegationCloseoutRecord{
		SchemaVersion: 1, PlanID: "plan-001", TaskID: "ghost", DelegationID: "x",
		Decision: "accept", ClosedAt: time.Now().UTC().Format(time.RFC3339),
	}
	err := applyCloseoutDecisionToTasks(repo, "plan-001", "ghost", closeout)
	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("expected task-not-found, got %v", err)
	}
}

func TestApplyCloseoutDecisionToTasks_PlanLoadFails(t *testing.T) {
	repo := setupTestProject(t)
	closeout := workflowDelegationCloseoutRecord{
		SchemaVersion: 1, PlanID: "plan-001", TaskID: "task-001", DelegationID: "x",
		Decision: "accept", ClosedAt: time.Now().UTC().Format(time.RFC3339),
	}

	if err := os.WriteFile(filepath.Join(repo, ".agents", "workflow", "plans", "plan-001", "PLAN.yaml"),
		[]byte("not: valid: yaml: at: all"), 0644); err != nil {
		t.Fatal(err)
	}
	err := applyCloseoutDecisionToTasks(repo, "plan-001", "task-001", closeout)
	if err == nil {
		t.Fatal("expected plan-load failure to propagate")
	}
}

func TestApplyCloseoutDecisionToTasks_SaveTasksFails(t *testing.T) {
	repo := setupTestProject(t)
	closeout := workflowDelegationCloseoutRecord{
		SchemaVersion: 1, PlanID: "plan-001", TaskID: "task-001", DelegationID: "x",
		Decision: "accept", ClosedAt: time.Now().UTC().Format(time.RFC3339),
	}
	sentinel := errors.New("write boom")
	withWriteFileStub(t, func(string, []byte, os.FileMode) error { return sentinel })

	err := applyCloseoutDecisionToTasks(repo, "plan-001", "task-001", closeout)
	if err == nil || !strings.Contains(err.Error(), "save tasks") {
		t.Fatalf("expected save-tasks error, got %v", err)
	}
}

func TestApplyCloseoutDecisionToTasks_TasksLoadFails(t *testing.T) {
	repo := setupTestProject(t)

	if err := os.Remove(filepath.Join(repo, ".agents", "workflow", "plans", "plan-001", "TASKS.yaml")); err != nil {
		t.Fatal(err)
	}
	closeout := workflowDelegationCloseoutRecord{
		SchemaVersion: 1, PlanID: "plan-001", TaskID: "task-001", Decision: "accept",
	}
	err := applyCloseoutDecisionToTasks(repo, "plan-001", "task-001", closeout)
	if err == nil || !strings.Contains(err.Error(), "load canonical tasks") {
		t.Fatalf("expected tasks-load error, got %v", err)
	}
}

func TestArchiveCloseoutArtifacts_MissingMergeBackFails(t *testing.T) {
	repo := setupTestProject(t)
	saveTestDelegationContract(t, repo, "task-001", "plan-001", "del-x")
	c, _ := loadDelegationContract(repo, "task-001")

	closeout := workflowDelegationCloseoutRecord{
		SchemaVersion: 1, PlanID: "plan-001", TaskID: "task-001", DelegationID: c.ID,
		Decision: "accept", ClosedAt: time.Now().UTC().Format(time.RFC3339),
	}

	_, _, err := archiveCloseoutArtifacts(repo, "task-001", "plan-001", "accept", c, closeout)
	if err == nil || !strings.Contains(err.Error(), "archive merge-back") {
		t.Fatalf("expected archive merge-back error, got %v", err)
	}
}

func TestArchiveCloseoutArtifacts_MarshalCloseoutError(t *testing.T) {
	repo := setupTestProject(t)
	saveTestDelegationContract(t, repo, "task-001", "plan-001", "del-x")
	c, _ := loadDelegationContract(repo, "task-001")

	if err := saveMergeBack(repo, &MergeBackSummary{
		SchemaVersion: 1, TaskID: "task-001", ParentPlanID: "plan-001",
		Title: "x", Summary: "s", VerificationResult: MergeBackVerification{Status: "pass"},
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}

	sentinel := errors.New("marshal boom")
	prev := yamlMarshal
	yamlMarshal = func(v any) ([]byte, error) { return nil, sentinel }
	t.Cleanup(func() { yamlMarshal = prev })

	closeout := workflowDelegationCloseoutRecord{
		SchemaVersion: 1, PlanID: "plan-001", TaskID: "task-001", DelegationID: c.ID,
		Decision: "accept", ClosedAt: time.Now().UTC().Format(time.RFC3339),
	}
	_, _, err := archiveCloseoutArtifacts(repo, "task-001", "plan-001", "accept", c, closeout)
	if err == nil || !errors.Is(err, sentinel) {
		t.Fatalf("expected marshal sentinel, got %v", err)
	}
}

func TestLoadDelegationContract_ParseError(t *testing.T) {
	repo := t.TempDir()
	dir := delegationDir(repo)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "t.yaml"), []byte("not: valid: yaml"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := loadDelegationContract(repo, "t")
	if err == nil || !strings.Contains(err.Error(), "parse") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

func TestProposalAbsPathFromRoutedTo_NotProposal(t *testing.T) {
	_, err := proposalAbsPathFromRoutedTo("not-a-proposal")
	if err == nil || !strings.Contains(err.Error(), "not a proposal route") {
		t.Fatalf("expected not-a-proposal error, got %v", err)
	}
}

func TestProposalAbsPathFromRoutedTo_InvalidName(t *testing.T) {
	cases := []string{
		"proposal:",
		"proposal:../etc/passwd",
		"proposal:foo/bar",
		"proposal:foo\\bar",
	}
	for _, c := range cases {
		c := c
		t.Run(c, func(t *testing.T) {
			_, err := proposalAbsPathFromRoutedTo(c)
			if err == nil || !strings.Contains(err.Error(), "invalid proposal name") {
				t.Fatalf("expected invalid-name error for %q, got %v", c, err)
			}
		})
	}
}

func TestProposalAbsPathFromRoutedTo_HappyPath(t *testing.T) {
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	p, err := proposalAbsPathFromRoutedTo("proposal:obs-123.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(p, filepath.Join("proposals", "obs-123.md")) {
		t.Fatalf("unexpected path %q", p)
	}
}

func TestLoadFoldBackArtifactByID_NotFound(t *testing.T) {
	repo := t.TempDir()
	_, err := loadFoldBackArtifactByID(repo, "no-such")
	if err == nil {
		t.Fatal("expected not-found error")
	}
}

func TestLoadFoldBackArtifactByID_ParseError(t *testing.T) {
	repo := t.TempDir()
	dir := foldBackDir(repo)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(foldBackArtifactFile(repo, "bad"), []byte("not: valid: yaml"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := loadFoldBackArtifactByID(repo, "bad")
	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestWriteFoldBackArtifact_MarshalError(t *testing.T) {
	repo := t.TempDir()
	prev := yamlMarshal
	yamlMarshal = func(v any) ([]byte, error) { return nil, errors.New("marshal boom") }
	t.Cleanup(func() { yamlMarshal = prev })
	err := writeFoldBackArtifact(repo, foldBackArtifact{ID: "x"})
	if err == nil {
		t.Fatal("expected marshal error")
	}
}

func TestListDelegationContracts_SkipsParseErrorFiles(t *testing.T) {
	repo := t.TempDir()
	dir := delegationDir(repo)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}

	saveTestDelegationContract(t, repo, "task-good", "plan", "del-good")
	if err := os.WriteFile(filepath.Join(dir, "broken.yaml"), []byte("not: valid: yaml"), 0644); err != nil {
		t.Fatal(err)
	}
	contracts, err := listDelegationContracts(repo)
	if err == nil {

		_ = contracts
		return
	}
	if !strings.Contains(err.Error(), "parse") && !strings.Contains(err.Error(), "broken") {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestReadFoldBackArtifacts_PlanFilter(t *testing.T) {
	repo := setupFoldBackProject(t)

	if err := executeWorkflowCommand(t, repo, "fold-back", "create",
		"--plan", "p1", "--task", "t1", "--observation", "a"); err != nil {
		t.Fatal(err)
	}

	plansDir := filepath.Join(repo, ".agents", "workflow", "plans", "p2")
	if err := os.MkdirAll(plansDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plansDir, "PLAN.yaml"),
		[]byte("schema_version: 1\nid: p2\ntitle: P2\nstatus: active\ncreated_at: 2026-01-01T00:00:00Z\nupdated_at: 2026-01-01T00:00:00Z\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plansDir, "TASKS.yaml"),
		[]byte("schema_version: 1\nplan_id: p2\ntasks: []\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := executeWorkflowCommand(t, repo, "fold-back", "create",
		"--plan", "p2", "--observation", "b"); err != nil {
		t.Fatal(err)
	}
	dir := foldBackDir(repo)
	all, err := readFoldBackArtifacts(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) < 2 {
		t.Fatalf("expected >=2 artifacts, got %d", len(all))
	}
	filtered, err := readFoldBackArtifacts(dir, "p1")
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range filtered {
		if a.PlanID != "p1" {
			t.Fatalf("expected p1-only, got %+v", a)
		}
	}
}

func TestReadFoldBackArtifacts_SkipsParseErrors(t *testing.T) {
	repo := t.TempDir()
	dir := foldBackDir(repo)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "fold-bogus.yaml"), []byte("not: valid: yaml"), 0644); err != nil {
		t.Fatal(err)
	}

	_, _ = readFoldBackArtifacts(dir, "")
}

func TestLoadPriorFoldBackArtifact_NoSlug(t *testing.T) {
	repo := t.TempDir()
	_, exists, err := loadPriorFoldBackArtifact(repo, "")
	if err != nil {
		t.Fatalf("empty slug should return exists=false err=nil, got err=%v", err)
	}
	if exists {
		t.Fatal("expected exists=false for empty slug")
	}
}

func TestCreateProposalFoldBack_AgentsHomeWriteError(t *testing.T) {
	repo := setupFoldBackProject(t)

	tmp := t.TempDir()
	blockerFile := filepath.Join(tmp, "blocker")
	if err := os.WriteFile(blockerFile, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", blockerFile)

	err := executeWorkflowCommand(t, repo, "fold-back", "create",
		"--plan", "p1", "--task", "t1", "--observation", "x", "--propose")
	if err == nil {
		t.Fatal("expected proposal write error")
	}
}

func TestRunWorkflowFoldBackList_PlanFilterEmpty(t *testing.T) {
	repo := setupFoldBackProject(t)
	out := executeWorkflowCommandOutput(t, repo, "fold-back", "list", "--plan", "no-such")
	if !strings.Contains(out, "No fold-back observations") {
		t.Fatalf("expected empty list for no-match plan, got %s", out)
	}
}

func TestValidateFanoutBundleFlagPaths_AllValid(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)

	if err := os.MkdirAll(filepath.Join(repo, "docs"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "docs", "x.md"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := executeWorkflowCommand(t, repo, "fanout",
		"--plan", "plan-001", "--task", "task-001", "--owner", "w",
		"--prompt-file", "docs/x.md",
		"--context-file", "docs/x.md",
	); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

func TestValidateFanoutBundleFlagPaths_ContextEscape(t *testing.T) {
	repo := setupTestProject(t)
	err := executeWorkflowCommand(t, repo, "fanout",
		"--plan", "plan-001", "--task", "task-001", "--owner", "w",
		"--context-file", "../../etc/passwd",
	)
	if err == nil || !strings.Contains(err.Error(), "context-file") {
		t.Fatalf("expected context-file escape error, got %v", err)
	}
}

func TestRunWorkflowFoldBackList_JSON_WithArtifacts(t *testing.T) {
	repo := setupFoldBackProject(t)
	if err := executeWorkflowCommand(t, repo, "fold-back", "create",
		"--plan", "p1", "--task", "t1", "--observation", "v"); err != nil {
		t.Fatal(err)
	}

	workflowTestJSON = true
	defer func() { workflowTestJSON = false }()

	out := executeWorkflowCommandOutput(t, repo, "fold-back", "list")
	if !strings.Contains(out, `"plan_id"`) {
		t.Fatalf("expected JSON plan_id: %s", out)
	}
}

func TestDelegationCloseoutCobra_JSON_RejectsInvalidDecision(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	err := executeWorkflowCommand(t, repo, "delegation", "closeout",
		"--plan", "p1", "--task", "t1", "--decision", "")
	if err == nil {
		t.Fatal("expected empty-decision error")
	}
}

func TestMergeBack_CobraRoundTrip(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	if err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--slice", "s1", "--owner", "w"); err != nil {
		t.Fatal(err)
	}
	if err := executeWorkflowCommand(t, repo, "merge-back",
		"--task", "t1", "--summary", "implemented",
		"--verification-status", "pass",
		"--integration-notes", "no blockers",
	); err != nil {
		t.Fatal(err)
	}
	mb, err := loadMergeBack(repo, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if mb.VerificationResult.Status != "pass" {
		t.Fatalf("status mismatch: %+v", mb)
	}
}

func TestSaveDelegationContract_UpdatesTimestamp(t *testing.T) {
	repo := t.TempDir()
	saveTestDelegationContract(t, repo, "task-x", "plan-x", "del-x")
	c, err := loadDelegationContract(repo, "task-x")
	if err != nil {
		t.Fatal(err)
	}
	firstUpdate := c.UpdatedAt
	time.Sleep(1100 * time.Millisecond)
	if err := saveDelegationContract(repo, c); err != nil {
		t.Fatal(err)
	}
	c2, err := loadDelegationContract(repo, "task-x")
	if err != nil {
		t.Fatal(err)
	}
	if c2.UpdatedAt == firstUpdate {
		t.Fatalf("expected UpdatedAt to advance: %q == %q", firstUpdate, c2.UpdatedAt)
	}
}

func TestCobra_BundleCmdSurfaceExists(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	if err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--slice", "s1", "--owner", "w"); err != nil {
		t.Fatal(err)
	}
	c, _ := loadDelegationContract(repo, "t1")
	bundlePath := filepath.Join(repo, ".agents", "active", "delegation-bundles", c.ID+".yaml")

	err := executeWorkflowCommand(t, repo, "bundle", "stages", bundlePath+".does-not-exist")
	if err == nil {
		t.Fatal("expected error for missing bundle")
	}
}

func TestMergeBack_NoDelegationContractFails(t *testing.T) {
	repo := setupTestProject(t)
	err := executeWorkflowCommand(t, repo, "merge-back", "--task", "task-001", "--summary", "x", "--verification-status", "pass")
	if err == nil || !strings.Contains(err.Error(), "delegation contract for task task-001") {
		t.Fatalf("expected missing-contract error, got %v", err)
	}
}

func TestMergeBack_RefusesCompletedContract(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	if err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--slice", "s1", "--owner", "w"); err != nil {
		t.Fatal(err)
	}

	c, err := loadDelegationContract(repo, "t1")
	if err != nil {
		t.Fatal(err)
	}
	c.Status = "completed"
	if err := saveDelegationContract(repo, c); err != nil {
		t.Fatal(err)
	}
	err = executeWorkflowCommand(t, repo, "merge-back", "--task", "t1", "--summary", "x", "--verification-status", "pass")
	if err == nil || !strings.Contains(err.Error(), "already") {
		t.Fatalf("expected already-completed error, got %v", err)
	}
}

func TestDelegationCloseout_InvalidDecision(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	err := executeWorkflowCommand(t, repo, "delegation", "closeout",
		"--plan", "p1", "--task", "t1", "--decision", "maybe")
	if err == nil || !strings.Contains(err.Error(), "accept") {
		t.Fatalf("expected accept/reject error, got %v", err)
	}
}

func TestDelegationCloseout_NoMergeBack(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	if err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--slice", "s1", "--owner", "w"); err != nil {
		t.Fatal(err)
	}

	err := executeWorkflowCommand(t, repo, "delegation", "closeout",
		"--plan", "p1", "--task", "t1", "--decision", "accept")
	if err == nil || !strings.Contains(err.Error(), "merge-back") {
		t.Fatalf("expected merge-back-required error, got %v", err)
	}
}

func TestMergeBack_WritesContractCompletion(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	if err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--slice", "s1", "--owner", "w"); err != nil {
		t.Fatal(err)
	}
	if err := executeWorkflowCommand(t, repo, "merge-back", "--task", "t1",
		"--summary", "shipped", "--verification-status", "pass",
		"--integration-notes", "no conflicts"); err != nil {
		t.Fatal(err)
	}
	c, err := loadDelegationContract(repo, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if c.Status != "completed" {
		t.Fatalf("expected delegation completed, got %q", c.Status)
	}
}

func TestFanout_PersistArtifactsWriteError(t *testing.T) {
	repo := setupTestProject(t)
	chdirRepo(t, repo)

	sentinel := errors.New("write boom")
	withWriteFileStub(t, func(string, []byte, os.FileMode) error { return sentinel })

	err := executeWorkflowCommand(t, repo, "fanout",
		"--plan", "plan-001", "--task", "task-001", "--owner", "w")
	if err == nil {
		t.Fatal("expected write-fault error")
	}
	_ = sentinel
}

func TestMergeBack_SaveError(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	if err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--slice", "s1", "--owner", "w"); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("write boom")
	withWriteFileStub(t, func(string, []byte, os.FileMode) error { return sentinel })

	err := executeWorkflowCommand(t, repo, "merge-back",
		"--task", "t1", "--summary", "done", "--verification-status", "pass")
	if err == nil {
		t.Fatal("expected save error")
	}
}

func TestSaveTestDelegationContract_StableID(t *testing.T) {
	repo := t.TempDir()
	saveTestDelegationContract(t, repo, "tx", "px", "del-tx-fixed")
	c, err := loadDelegationContract(repo, "tx")
	if err != nil {
		t.Fatal(err)
	}
	if c.ID != "del-tx-fixed" {
		t.Fatalf("contract id = %q, want del-tx-fixed", c.ID)
	}
	if time.Since(mustParseRFC3339(t, c.UpdatedAt)) > time.Minute {
		t.Fatalf("UpdatedAt should be recent, got %q", c.UpdatedAt)
	}
}

// stubFanoutSnapshot installs an appTypeSnapshot seam that reads the repo-local
// .agentsrc.json and folds the legacy keys via UnmarshalJSON (no user-local /
// scope merge), keeping the fanout-dispatch tests hermetic on a dev machine while
// still exercising the legacy → stage_profiles / execution_profile fold.
func stubFanoutSnapshot(t *testing.T) {
	t.Helper()
	orig := appTypeSnapshot
	t.Cleanup(func() { appTypeSnapshot = orig })
	appTypeSnapshot = func(p string) (*config.Snapshot, error) {
		data, err := os.ReadFile(filepath.Join(p, config.AgentsRCFile))
		if err != nil {
			return nil, err
		}
		var rc config.AgentsRC
		if err := json.Unmarshal(data, &rc); err != nil {
			return nil, err
		}
		return &config.Snapshot{Effective: rc}, nil
	}
}

// writeFanoutTestAgentsrc writes a raw .agentsrc.json body into a fresh temp repo,
// installs the hermetic snapshot seam, and returns the repo path. Raw body (not
// testutil.WriteAgentsRC) because these cases exercise the deprecated
// verifier_profiles / app_type_verifier_map keys and their fold.
func writeFanoutTestAgentsrc(t *testing.T, body string) string {
	t.Helper()
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, ".agentsrc.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	stubFanoutSnapshot(t)
	return repo
}

func TestValidateVerifierProfileRefs(t *testing.T) {
	// empty profiles or sequence is a no-op
	if err := validateVerifierProfileRefs(nil, nil); err != nil {
		t.Fatalf("empty inputs: %v", err)
	}
	if err := validateVerifierProfileRefs([]string{"x"}, nil); err != nil {
		t.Fatalf("empty profiles: %v", err)
	}
	profs := map[string]config.StageProfile{"unit": {}}
	if err := validateVerifierProfileRefs([]string{"ghost"}, profs); err == nil {
		t.Fatal("expected undefined-profile error")
	}
}

func TestExplicitVerifierSequence(t *testing.T) {
	t.Run("empty after split", func(t *testing.T) {
		if _, err := explicitVerifierSequence(t.TempDir(), " , , "); err == nil {
			t.Fatal("expected empty-sequence error")
		}
	})
	t.Run("undefined profile", func(t *testing.T) {
		repo := writeFanoutTestAgentsrc(t, `{"project":"t","version":1,"verifier_profiles":{"unit":{}}}`)
		if _, err := explicitVerifierSequence(repo, "ghost"); err == nil {
			t.Fatal("expected undefined-profile error")
		}
	})
	t.Run("valid", func(t *testing.T) {
		repo := writeFanoutTestAgentsrc(t, `{"project":"t","version":1,"verifier_profiles":{"unit":{}}}`)
		seq, err := explicitVerifierSequence(repo, "unit")
		if err != nil || len(seq) != 1 || seq[0] != "unit" {
			t.Fatalf("valid explicit seq: %v %v", seq, err)
		}
	})
}

func TestMappedVerifierSequence(t *testing.T) {
	t.Run("empty sequence for app type", func(t *testing.T) {
		repo := writeFanoutTestAgentsrc(t, `{"project":"t","version":1,"app_type_verifier_map":{"go-cli":[]},"verifier_profiles":{"unit":{}}}`)
		seq, err := mappedVerifierSequence(repo, "go-cli")
		if err != nil || seq != nil {
			t.Fatalf("empty mapped seq should be nil,nil: %v %v", seq, err)
		}
	})
	t.Run("undefined profile", func(t *testing.T) {
		repo := writeFanoutTestAgentsrc(t, `{"project":"t","version":1,"app_type_verifier_map":{"go-cli":["ghost"]},"verifier_profiles":{"unit":{}}}`)
		if _, err := mappedVerifierSequence(repo, "go-cli"); err == nil {
			t.Fatal("expected undefined-profile error from mapped sequence")
		}
	})
	t.Run("missing manifest is nil", func(t *testing.T) {
		stubFanoutSnapshot(t)
		seq, err := mappedVerifierSequence(t.TempDir(), "go-cli")
		if err != nil || seq != nil {
			t.Fatalf("missing manifest should be nil,nil: %v %v", seq, err)
		}
	})
}

// TestLoadFanoutDispatch_Errors covers the read- and parse-error branches.
func TestLoadFanoutDispatch_Errors(t *testing.T) {
	t.Run("malformed json", func(t *testing.T) {
		stubFanoutSnapshot(t)
		repo := t.TempDir()
		if err := os.WriteFile(filepath.Join(repo, ".agentsrc.json"), []byte(`{bad`), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := loadFanoutDispatch(repo); err == nil {
			t.Fatal("expected parse error")
		}
	})
	t.Run("read error not-not-exist", func(t *testing.T) {
		stubFanoutSnapshot(t)
		repo := t.TempDir()
		if err := os.Mkdir(filepath.Join(repo, ".agentsrc.json"), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := loadFanoutDispatch(repo); err == nil {
			t.Fatal("expected read error when .agentsrc.json is a directory")
		}
	})
}

// TestValidatePathHelpers covers the path-validation error branches:
// parent-escape, stat failure (missing file), and directory-not-regular-file.
func TestValidatePathHelpers(t *testing.T) {
	repo := t.TempDir()
	if _, err := validateInsideProjectPath(repo, "../escape"); err == nil {
		t.Fatal("expected parent-escape error")
	}
	if _, err := validateProjectFileRef(repo, "nope.txt"); err == nil {
		t.Fatal("expected cannot-access error for missing file")
	}
	if err := os.Mkdir(filepath.Join(repo, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := validateProjectFileRef(repo, "d"); err == nil {
		t.Fatal("expected not-a-regular-file error for a directory")
	}
}

// TestValidatePriorFoldBack covers the inline/propose conflict branch and the
// prior-agreement (plan mismatch) passthrough.
func TestValidatePriorFoldBack_ProposeOnSmall(t *testing.T) {
	prior := &foldBackArtifact{ID: "fb1", PlanID: "p1", Classification: "small"}
	if err := validatePriorFoldBack(prior, &foldBackUpsertInputs{slug: "fb1", planID: "p1", propose: true}); err == nil || !strings.Contains(err.Error(), "propose") {
		t.Fatalf("expected propose-on-small error, got %v", err)
	}
	// plan mismatch flows through validateFoldBackPriorAgreement
	if err := validatePriorFoldBack(prior, &foldBackUpsertInputs{slug: "fb1", planID: "p2"}); err == nil || !strings.Contains(err.Error(), "plan") {
		t.Fatalf("expected plan-mismatch passthrough error, got %v", err)
	}
}

// TestReadFoldBackArtifacts_ReadDirError covers the missing-dir ReadDir error branch.
func TestReadFoldBackArtifacts_ReadDirError(t *testing.T) {
	if _, err := readFoldBackArtifacts(filepath.Join(t.TempDir(), "nope"), ""); err == nil {
		t.Fatal("expected ReadDir error for a missing directory")
	}
}

// TestReadFoldBackArtifacts_SkipsNonYAML covers the dir/non-yaml skip branch:
// a directory and a non-.yaml file are both skipped, yielding no artifacts.
func TestReadFoldBackArtifacts_SkipsNonYAML(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := readFoldBackArtifacts(dir, "")
	if err != nil || len(got) != 0 {
		t.Fatalf("expected zero artifacts (non-yaml + dir skipped): %v %v", got, err)
	}
}

// TestReadFoldBackArtifacts_ParseError covers the malformed-yaml parse-error branch.
func TestReadFoldBackArtifacts_ParseError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte(":\n  -x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readFoldBackArtifacts(dir, ""); err == nil {
		t.Fatal("expected parse error from malformed fold-back yaml")
	}
}

// TestLoadPriorFoldBackArtifact_DirAndMalformed covers the is-directory and
// load-error branches of loadPriorFoldBackArtifact.
func TestLoadPriorFoldBackArtifact_DirAndMalformed(t *testing.T) {
	repo := t.TempDir()
	dirPath := foldBackArtifactFile(repo, "isdir")
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if a, ok, err := loadPriorFoldBackArtifact(repo, "isdir"); a != nil || ok || err != nil {
		t.Fatalf("dir path should be (nil,false,nil): %v %v %v", a, ok, err)
	}
	badPath := foldBackArtifactFile(repo, "bad")
	if err := os.MkdirAll(filepath.Dir(badPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(badPath, []byte(":\n  -x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadPriorFoldBackArtifact(repo, "bad"); err == nil {
		t.Fatal("expected load error for malformed fold-back artifact")
	}
	// non-NotExist stat error: an unreadable parent dir makes os.Stat of a child
	// fail with a permission error (not IsNotExist). testutil concentrates the
	// POSIX-chmod / Windows-deny-ACE / root-skip policy, so this stays OS-agnostic
	// instead of branching on runtime.GOOS.
	repo2 := t.TempDir()
	statTarget := foldBackArtifactFile(repo2, "x")
	if err := os.MkdirAll(filepath.Dir(statTarget), 0o755); err != nil {
		t.Fatal(err)
	}
	testutil.MakeDirUnreadable(t, filepath.Dir(statTarget))
	if _, _, err := loadPriorFoldBackArtifact(repo2, "x"); err == nil {
		t.Fatal("expected non-NotExist stat error when the fold-back dir is unreadable")
	}
}

// TestUpdatePlanFoldBackSummary_PlanNotFound covers the loadCanonicalPlan error branch.
func TestUpdatePlanFoldBackSummary_PlanNotFound(t *testing.T) {
	repo := t.TempDir()
	err := updatePlanFoldBackSummary(repo, "ghost-plan", "2026-04-10T00:00:00Z", func(s string) string { return s })
	if err == nil {
		t.Fatal("expected load error for missing plan")
	}
}
