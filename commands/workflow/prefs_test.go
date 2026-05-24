package workflow

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPreferences_DefaultsAreNonEmpty(t *testing.T) {
	d := defaultWorkflowPreferences()
	if d.Verification.TestCommand == nil || *d.Verification.TestCommand == "" {
		t.Fatal("default test_command must be set")
	}
	if d.Planning.PlanDirectory == nil || *d.Planning.PlanDirectory == "" {
		t.Fatal("default plan_directory must be set")
	}
	if d.Execution.Formatter == nil || *d.Execution.Formatter == "" {
		t.Fatal("default formatter must be set")
	}
}

func TestPreferences_MergeNoOverrides(t *testing.T) {
	d := defaultWorkflowPreferences()
	out := mergePreferences(d, WorkflowPreferences{}, WorkflowPreferences{})
	if strPtrVal(out.Verification.TestCommand) != strPtrVal(d.Verification.TestCommand) {
		t.Fatalf("test_command changed without override")
	}
}

func TestPreferences_RepoOverridesDefault(t *testing.T) {
	d := defaultWorkflowPreferences()
	cmd := "make test"
	repo := WorkflowPreferences{Verification: WorkflowVerificationPrefs{TestCommand: &cmd}}
	out := mergePreferences(d, repo, WorkflowPreferences{})
	if strPtrVal(out.Verification.TestCommand) != "make test" {
		t.Fatalf("repo override not applied: got %q", strPtrVal(out.Verification.TestCommand))
	}
	// Other defaults must be preserved
	if strPtrVal(out.Execution.Formatter) != strPtrVal(d.Execution.Formatter) {
		t.Fatalf("other defaults lost after repo override")
	}
}

func TestPreferences_LocalTrumpsRepo(t *testing.T) {
	d := defaultWorkflowPreferences()
	repo := "make test"
	local := "npm test"
	repoPrefs := WorkflowPreferences{Verification: WorkflowVerificationPrefs{TestCommand: &repo}}
	localPrefs := WorkflowPreferences{Verification: WorkflowVerificationPrefs{TestCommand: &local}}
	out := mergePreferences(d, repoPrefs, localPrefs)
	if strPtrVal(out.Verification.TestCommand) != "npm test" {
		t.Fatalf("local pref did not trump repo: got %q", strPtrVal(out.Verification.TestCommand))
	}
}

func TestPreferences_SetLocalPersists(t *testing.T) {
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	if err := setLocalPreference("my-proj", "verification.test_command", "pytest"); err != nil {
		t.Fatal(err)
	}

	f, err := loadLocalPreferences("my-proj")
	if err != nil {
		t.Fatal(err)
	}
	if f == nil {
		t.Fatal("loadLocalPreferences returned nil after set")
	}
	if strPtrVal(f.Verification.TestCommand) != "pytest" {
		t.Fatalf("persisted test_command = %q, want pytest", strPtrVal(f.Verification.TestCommand))
	}
}

func TestPreferences_SetLocalUpdateExisting(t *testing.T) {
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	_ = setLocalPreference("my-proj", "verification.test_command", "pytest")
	_ = setLocalPreference("my-proj", "execution.formatter", "black")
	// Now update test_command
	_ = setLocalPreference("my-proj", "verification.test_command", "pytest -x")

	f, _ := loadLocalPreferences("my-proj")
	if strPtrVal(f.Verification.TestCommand) != "pytest -x" {
		t.Fatalf("updated test_command = %q, want 'pytest -x'", strPtrVal(f.Verification.TestCommand))
	}
	// Other key must not be clobbered
	if strPtrVal(f.Execution.Formatter) != "black" {
		t.Fatalf("formatter clobbered: got %q", strPtrVal(f.Execution.Formatter))
	}
}

func TestPreferences_InvalidKeyRejected(t *testing.T) {
	if err := applyPreferenceKey(&WorkflowPreferences{}, "nonexistent.key", "val"); err == nil {
		t.Fatal("expected error for unknown key, got nil")
	}
}

func TestPreferences_BoolField(t *testing.T) {
	p := WorkflowPreferences{}
	if err := applyPreferenceKey(&p, "verification.require_regression_before_handoff", "false"); err != nil {
		t.Fatal(err)
	}
	if p.Verification.RequireRegressionBeforeHandoff == nil {
		t.Fatal("bool field nil after apply")
	}
	if *p.Verification.RequireRegressionBeforeHandoff != false {
		t.Fatal("expected false")
	}
}

// commit.disable is the new prefs key wc-prefs-optout introduced. Verify it
// parses, applies, and lands in the merged result so `da workflow commit`
// can read it from the resolved preferences.
func TestPreferences_CommitDisableApplyAndMerge(t *testing.T) {
	p := WorkflowPreferences{}
	if err := applyPreferenceKey(&p, "commit.disable", "true"); err != nil {
		t.Fatal(err)
	}
	if p.Commit.Disable == nil || *p.Commit.Disable != true {
		t.Fatalf("Commit.Disable = %v, want *true", p.Commit.Disable)
	}

	// Merge: local should override defaults.
	defaults := defaultWorkflowPreferences()
	if defaults.Commit.Disable == nil || *defaults.Commit.Disable {
		t.Errorf("default Commit.Disable should be false; got %v", defaults.Commit.Disable)
	}
	merged := mergePreferences(defaults, WorkflowPreferences{}, p)
	if merged.Commit.Disable == nil || *merged.Commit.Disable != true {
		t.Fatalf("merged Commit.Disable = %v, want *true (local overrides default)", merged.Commit.Disable)
	}
}

// The key is in the validator registry so `da workflow prefs set
// commit.disable true` accepts it (rather than emitting "unknown key").
func TestPreferences_CommitDisableIsKnownKey(t *testing.T) {
	if !isValidPreferenceKey("commit.disable") {
		t.Fatal("commit.disable not in known-key registry")
	}
}

func TestPreferences_ResolveWithSources(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	// Write a repo preference
	repoPrefsDir := filepath.Join(repo, ".agents", "workflow")
	if err := os.MkdirAll(repoPrefsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoPrefsDir, "preferences.yaml"), []byte("schema_version: 1\nverification:\n  test_command: make test\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Write a local override
	if err := setLocalPreference("workflow-proj", "execution.formatter", "prettier"); err != nil {
		t.Fatal(err)
	}

	sources, err := resolvePreferencesWithSources(repo, "workflow-proj")
	if err != nil {
		t.Fatal(err)
	}

	srcMap := make(map[string]preferenceSource)
	for _, s := range sources {
		srcMap[s.Key] = s
	}

	if srcMap["verification.test_command"].Source != "repo" {
		t.Fatalf("test_command source = %q, want repo", srcMap["verification.test_command"].Source)
	}
	if srcMap["verification.test_command"].Value != "make test" {
		t.Fatalf("test_command value = %q, want 'make test'", srcMap["verification.test_command"].Value)
	}
	if srcMap["execution.formatter"].Source != "local" {
		t.Fatalf("formatter source = %q, want local", srcMap["execution.formatter"].Source)
	}
	if srcMap["verification.lint_command"].Source != "default" {
		t.Fatalf("lint_command source = %q, want default", srcMap["verification.lint_command"].Source)
	}
}

func TestPreferences_OrientIncludesPreferences(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	oldwd, _ := os.Getwd()
	defer os.Chdir(oldwd)
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	state, err := collectWorkflowState()
	if err != nil {
		t.Fatal(err)
	}
	if state.Preferences == nil {
		t.Fatal("workflowOrientState.Preferences must be populated by collectWorkflowState")
	}

	var buf bytes.Buffer
	renderWorkflowOrientMarkdown(state, &buf)
	rendered := buf.String()
	if !strings.Contains(rendered, "# Preferences") {
		t.Fatalf("orient output missing Preferences section:\n%s", rendered)
	}
	if !strings.Contains(rendered, "test_command") {
		t.Fatalf("orient output missing test_command:\n%s", rendered)
	}
}

// TestPreferences_LoadRepoMissingReturnsNil ensures absence of repo prefs is
// not an error.
func TestPreferences_LoadRepoMissingReturnsNil(t *testing.T) {
	tmp := t.TempDir()
	f, err := loadRepoPreferences(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if f != nil {
		t.Fatalf("expected nil for missing repo prefs, got %+v", f)
	}
}

// TestPreferences_LoadRepoMalformedYAML ensures malformed YAML returns an
// error rather than panicking.
func TestPreferences_LoadRepoMalformedYAML(t *testing.T) {
	tmp := t.TempDir()
	prefsDir := filepath.Join(tmp, ".agents", "workflow")
	if err := os.MkdirAll(prefsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prefsDir, "preferences.yaml"), []byte("foo: bar\n  - this is not: valid yaml\n\t\tindent: broken"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := loadRepoPreferences(tmp)
	if err == nil {
		t.Fatal("expected error for malformed YAML")
	}
}

// TestPreferences_LoadLocalMissingReturnsNil mirrors the repo-prefs absence test.
func TestPreferences_LoadLocalMissingReturnsNil(t *testing.T) {
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	f, err := loadLocalPreferences("no-such-project")
	if err != nil {
		t.Fatal(err)
	}
	if f != nil {
		t.Fatalf("expected nil for missing local prefs, got %+v", f)
	}
}

// TestPreferences_LoadLocalMalformedYAML returns an error rather than panic.
func TestPreferences_LoadLocalMalformedYAML(t *testing.T) {
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	dir := filepath.Join(agentsHome, "context", "broken-proj")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "preferences.local.yaml"), []byte("foo: bar\n  - this is not: valid yaml\n\t\tindent: broken"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := loadLocalPreferences("broken-proj")
	if err == nil {
		t.Fatal("expected error for malformed local prefs YAML")
	}
}

// TestPreferences_ApplyBoolPlanningRequirePlanBeforeCode covers a planning bool
// applier so all categories get coverage.
func TestPreferences_ApplyBoolPlanningRequirePlanBeforeCode(t *testing.T) {
	p := WorkflowPreferences{}
	if err := applyPreferenceKey(&p, "planning.require_plan_before_code", "true"); err != nil {
		t.Fatal(err)
	}
	if p.Planning.RequirePlanBeforeCode == nil || *p.Planning.RequirePlanBeforeCode != true {
		t.Fatalf("planning.require_plan_before_code not set to true: %+v", p.Planning.RequirePlanBeforeCode)
	}
}

// TestPreferences_ApplyReviewAndExecutionStringFields covers the remaining
// string appliers in review and execution categories.
func TestPreferences_ApplyReviewAndExecutionStringFields(t *testing.T) {
	p := WorkflowPreferences{}
	for _, kv := range []struct{ key, val string }{
		{"review.review_order", "tests-first"},
		{"review.require_findings_first", "false"},
		{"execution.package_manager", "yarn"},
		{"planning.plan_directory", "docs/plans"},
	} {
		if err := applyPreferenceKey(&p, kv.key, kv.val); err != nil {
			t.Fatalf("apply %s: %v", kv.key, err)
		}
	}
	if strPtrVal(p.Review.ReviewOrder) != "tests-first" {
		t.Fatalf("review.review_order = %q", strPtrVal(p.Review.ReviewOrder))
	}
	if p.Review.RequireFindingsFirst == nil || *p.Review.RequireFindingsFirst != false {
		t.Fatalf("review.require_findings_first not false")
	}
	if strPtrVal(p.Execution.PackageManager) != "yarn" {
		t.Fatalf("execution.package_manager = %q", strPtrVal(p.Execution.PackageManager))
	}
	if strPtrVal(p.Planning.PlanDirectory) != "docs/plans" {
		t.Fatalf("planning.plan_directory = %q", strPtrVal(p.Planning.PlanDirectory))
	}
}

// TestPreferences_MaxParallelWorkers_ValidAndInvalid covers the bespoke int
// applier including range validation.
func TestPreferences_MaxParallelWorkers_ValidAndInvalid(t *testing.T) {
	p := WorkflowPreferences{}
	if err := applyMaxParallelWorkers(&p, "4"); err != nil {
		t.Fatal(err)
	}
	if p.Execution.MaxParallelWorkers == nil || *p.Execution.MaxParallelWorkers != 4 {
		t.Fatalf("max_parallel_workers = %v, want 4", p.Execution.MaxParallelWorkers)
	}
	for _, bad := range []string{"0", "9", "-1", "abc"} {
		if err := applyMaxParallelWorkers(&p, bad); err == nil {
			t.Fatalf("expected error for value %q", bad)
		}
	}
}

// TestPreferences_BoolPtrStrAndIntPtrStr covers the small ptr-to-string helpers.
func TestPreferences_BoolPtrStrAndIntPtrStr(t *testing.T) {
	if boolPtrStr(nil) != "" {
		t.Fatal("boolPtrStr(nil) should be empty")
	}
	tr, fl := true, false
	if boolPtrStr(&tr) != "true" || boolPtrStr(&fl) != "false" {
		t.Fatal("boolPtrStr mismatch")
	}
	if intPtrStr(nil) != "" {
		t.Fatal("intPtrStr(nil) should be empty")
	}
	n := 7
	if intPtrStr(&n) != "7" {
		t.Fatalf("intPtrStr(7) = %q, want 7", intPtrStr(&n))
	}
	if strPtrVal(nil) != "" {
		t.Fatal("strPtrVal(nil) should be empty")
	}
}

// TestPreferences_IsValidPreferenceKey covers the validation map both ways.
func TestPreferences_IsValidPreferenceKey(t *testing.T) {
	if !isValidPreferenceKey("verification.test_command") {
		t.Fatal("verification.test_command should be valid")
	}
	if isValidPreferenceKey("nonsense.field") {
		t.Fatal("nonsense.field should be invalid")
	}
}

// TestPreferences_SetLocalRoundTripReset rebinds a local pref to its default
// (acting as a reset) and verifies the persisted change.
func TestPreferences_SetLocalRoundTripReset(t *testing.T) {
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	if err := setLocalPreference("reset-proj", "execution.formatter", "prettier"); err != nil {
		t.Fatal(err)
	}
	f, err := loadLocalPreferences("reset-proj")
	if err != nil {
		t.Fatal(err)
	}
	if strPtrVal(f.Execution.Formatter) != "prettier" {
		t.Fatalf("after set: formatter = %q, want prettier", strPtrVal(f.Execution.Formatter))
	}

	// Reset back to the default value.
	defaults := defaultWorkflowPreferences()
	if err := setLocalPreference("reset-proj", "execution.formatter", strPtrVal(defaults.Execution.Formatter)); err != nil {
		t.Fatal(err)
	}
	f2, err := loadLocalPreferences("reset-proj")
	if err != nil {
		t.Fatal(err)
	}
	if strPtrVal(f2.Execution.Formatter) != strPtrVal(defaults.Execution.Formatter) {
		t.Fatalf("after reset: formatter = %q, want %q", strPtrVal(f2.Execution.Formatter), strPtrVal(defaults.Execution.Formatter))
	}
}

// TestPreferences_SetLocalRejectsUnknownKey ensures invalid keys surface a hint.
func TestPreferences_SetLocalRejectsUnknownKey(t *testing.T) {
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	err := setLocalPreference("any-proj", "nope.field", "value")
	if err == nil {
		t.Fatal("expected error for unknown preference key in setLocalPreference")
	}
	if !strings.Contains(err.Error(), "unknown preference key") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestRunWorkflowPrefs_RendersHumanReadable covers the listing renderer.
func TestRunWorkflowPrefs_RendersHumanReadable(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	captureStdoutWhileRunning(t, repo, func() error { return runWorkflowPrefs() },
		"Workflow Preferences",
		"[verification]",
		"test_command",
		"(default)",
	)
}

// TestRunWorkflowPrefsSetLocal_PersistsAndRejectsUnknown covers the user-facing
// set-local command including invalid-key rejection.
func TestRunWorkflowPrefsSetLocal_PersistsAndRejectsUnknown(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	oldwd, _ := os.Getwd()
	defer os.Chdir(oldwd)
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	if err := runWorkflowPrefsSetLocal("verification.test_command", "pytest -x"); err != nil {
		t.Fatalf("set-local valid: %v", err)
	}
	f, err := loadLocalPreferences("workflow-proj")
	if err != nil {
		t.Fatal(err)
	}
	if strPtrVal(f.Verification.TestCommand) != "pytest -x" {
		t.Fatalf("persisted value = %q, want 'pytest -x'", strPtrVal(f.Verification.TestCommand))
	}

	if err := runWorkflowPrefsSetLocal("bogus.key", "x"); err == nil {
		t.Fatal("expected error for unknown key, got nil")
	}
}

// TestRunWorkflowPrefsSetShared_CreatesProposal records a pending shared-pref
// change as a config proposal rather than mutating the repo file directly.
func TestRunWorkflowPrefsSetShared_CreatesProposal(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	oldwd, _ := os.Getwd()
	defer os.Chdir(oldwd)
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	if err := runWorkflowPrefsSetShared("verification.test_command", "go test ./pkg/..."); err != nil {
		t.Fatalf("set-shared: %v", err)
	}

	proposalsDir := filepath.Join(agentsHome, "proposals")
	entries, err := os.ReadDir(proposalsDir)
	if err != nil {
		t.Fatalf("expected proposals dir written: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least one proposal file recorded")
	}
	if err := runWorkflowPrefsSetShared("bogus.key", "x"); err == nil {
		t.Fatal("expected error for unknown key in set-shared")
	}
}

// TestPreferences_MergeOverwritesNilDestination verifies merge helpers replace
// nil pointers with src values at every category.
func TestPreferences_MergeOverwritesNilDestination(t *testing.T) {
	d := defaultWorkflowPreferences()
	// Override at the local layer for every category.
	local := WorkflowPreferences{}
	tc := "lint command"
	rg := true
	pd := ".plans"
	rpb := false
	ro := "tests-first"
	rff := false
	pm := "pnpm"
	ft := "biome"
	maxw := 2
	local.Verification.LintCommand = &tc
	local.Verification.RequireRegressionBeforeHandoff = &rg
	local.Planning.PlanDirectory = &pd
	local.Planning.RequirePlanBeforeCode = &rpb
	local.Review.ReviewOrder = &ro
	local.Review.RequireFindingsFirst = &rff
	local.Execution.PackageManager = &pm
	local.Execution.Formatter = &ft
	local.Execution.MaxParallelWorkers = &maxw

	out := mergePreferences(d, WorkflowPreferences{}, local)
	if strPtrVal(out.Verification.LintCommand) != "lint command" {
		t.Fatalf("Verification.LintCommand not overridden")
	}
	if *out.Verification.RequireRegressionBeforeHandoff != true {
		t.Fatalf("Verification.RequireRegressionBeforeHandoff not overridden")
	}
	if strPtrVal(out.Planning.PlanDirectory) != ".plans" {
		t.Fatalf("Planning.PlanDirectory not overridden")
	}
	if *out.Planning.RequirePlanBeforeCode != false {
		t.Fatalf("Planning.RequirePlanBeforeCode not overridden")
	}
	if strPtrVal(out.Review.ReviewOrder) != "tests-first" {
		t.Fatalf("Review.ReviewOrder not overridden")
	}
	if *out.Review.RequireFindingsFirst != false {
		t.Fatalf("Review.RequireFindingsFirst not overridden")
	}
	if strPtrVal(out.Execution.PackageManager) != "pnpm" {
		t.Fatalf("Execution.PackageManager not overridden")
	}
	if strPtrVal(out.Execution.Formatter) != "biome" {
		t.Fatalf("Execution.Formatter not overridden")
	}
	if *out.Execution.MaxParallelWorkers != 2 {
		t.Fatalf("Execution.MaxParallelWorkers not overridden")
	}
}

// ── Wave 5: Graph bridge types ────────────────────────────────────────────────

func TestResolvePreferences_EmptyProject(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", tmp)
	t.Setenv("HOME", tmp)
	prefs, err := resolvePreferences(tmp, "workflow-test-empty-proj")
	if err != nil {
		t.Fatalf("resolvePreferences: %v", err)
	}

	if prefs.Verification.TestCommand == nil {
		t.Error("expected default test_command")
	}
}

func TestLoadRepoPreferences_ReadError(t *testing.T) {
	repo := t.TempDir()
	prefsDir := filepath.Join(repo, ".agents", "workflow")
	if err := os.MkdirAll(prefsDir, 0755); err != nil {
		t.Fatal(err)
	}
	prefsPath := filepath.Join(prefsDir, "preferences.yaml")
	if err := os.WriteFile(prefsPath, []byte("planning:\n"), 0644); err != nil {
		t.Fatal(err)
	}
	chmodUnreadable(t, prefsPath)

	_, err := loadRepoPreferences(repo)
	if err == nil {
		t.Fatal("expected ReadFile error to propagate")
	}
}

func TestResolvePreferences_RepoLoadError(t *testing.T) {
	repo := t.TempDir()
	prefsDir := filepath.Join(repo, ".agents", "workflow")
	if err := os.MkdirAll(prefsDir, 0755); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(prefsDir, "preferences.yaml")
	if err := os.WriteFile(bad, []byte(":\n  - bad: ["), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := resolvePreferences(repo, "p")
	if err == nil {
		t.Fatal("expected malformed YAML to surface")
	}
}

func TestResolvePreferences_LocalLoadError(t *testing.T) {
	repo := t.TempDir()
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	ctx := filepath.Join(agentsHome, "context", "p")
	if err := os.MkdirAll(ctx, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ctx, "preferences.local.yaml"), []byte(":\n  - bad"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := resolvePreferences(repo, "p")
	if err == nil {
		t.Fatal("expected local malformed YAML to surface")
	}
}

func TestSetLocalPreference_UnknownKey(t *testing.T) {
	repo := setupTestProject(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	chdirForCov(t, repo)
	err := runWorkflowPrefsSetLocal("unknown.key", "value")
	if err == nil || !strings.Contains(err.Error(), "unknown preference key") {
		t.Fatalf("expected unknown-key error, got %v", err)
	}
}

func TestSetSharedPreference_UnknownKey(t *testing.T) {
	repo := setupTestProject(t)
	chdirForCov(t, repo)
	err := runWorkflowPrefsSetShared("unknown.key", "value")
	if err == nil || !strings.Contains(err.Error(), "unknown preference key") {
		t.Fatalf("expected unknown-key error, got %v", err)
	}
}

func TestRunWorkflowPrefs_JSON_Push6(t *testing.T) {
	repo := setupTestProject(t)
	chdirForCov(t, repo)
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = false })
	out, err := captureCovStdout(t, runWorkflowPrefs)
	if err != nil {
		t.Fatalf("prefs json: %v", err)
	}
	if !strings.Contains(out, "verification") {
		t.Fatalf("expected verification in JSON: %s", out)
	}
}

func TestApplyMaxParallelWorkers_OutOfRange(t *testing.T) {
	p := &WorkflowPreferences{}
	if err := applyMaxParallelWorkers(p, "100"); err == nil {
		t.Fatal("expected out-of-range error")
	}
	if err := applyMaxParallelWorkers(p, "0"); err == nil {
		t.Fatal("expected lower-bound error")
	}
	if err := applyMaxParallelWorkers(p, "abc"); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestApplyMaxParallelWorkers_Valid(t *testing.T) {
	p := &WorkflowPreferences{}
	if err := applyMaxParallelWorkers(p, "4"); err != nil {
		t.Fatal(err)
	}
	if p.Execution.MaxParallelWorkers == nil || *p.Execution.MaxParallelWorkers != 4 {
		t.Fatalf("expected 4, got %v", p.Execution.MaxParallelWorkers)
	}
}

func TestSetLocalPreference_MalformedExisting(t *testing.T) {
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	ctx := filepath.Join(agentsHome, "context", "p")
	if err := os.MkdirAll(ctx, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ctx, "preferences.local.yaml"), []byte(":\n  - bad: ["), 0644); err != nil {
		t.Fatal(err)
	}
	err := setLocalPreference("p", "execution.max_parallel_workers", "4")
	if err == nil || !strings.Contains(err.Error(), "parse local preferences") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

func TestSetLocalPreference_Writes(t *testing.T) {
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	if err := setLocalPreference("p", "execution.max_parallel_workers", "3"); err != nil {
		t.Fatalf("set: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(agentsHome, "context", "p", "preferences.local.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "max_parallel_workers: 3") {
		t.Fatalf("unexpected file content: %s", string(data))
	}
}

func TestRunWorkflowPrefs_OverridesShown(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	chdirRepo(t, repo)
	if err := runWorkflowPrefsSetLocal("verification.test_command", "make test"); err != nil {
		t.Fatal(err)
	}
	captureStdoutWhileRunning(t, repo, func() error { return runWorkflowPrefs() },
		"verification", "make test")
}

func TestRunWorkflowPrefs_JSON(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	workflowTestJSON = true
	defer func() { workflowTestJSON = false }()

	captureStdoutWhileRunning(t, repo, func() error { return runWorkflowPrefs() },
		`"verification"`)
}

func TestRunWorkflowPrefsSetShared_PropagatesProposalSaveError(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	chdirRepo(t, repo)

	tmp := t.TempDir()
	blockerFile := filepath.Join(tmp, "blocker")
	if err := os.WriteFile(blockerFile, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", blockerFile)

	err := runWorkflowPrefsSetShared("verification.test_command", "go test ./...")
	if err == nil {
		t.Fatal("expected save proposal error")
	}
}

func TestResolvePreferences_RepoParseError(t *testing.T) {
	repo := t.TempDir()
	prefsDir := filepath.Join(repo, ".agents", "workflow")
	if err := os.MkdirAll(prefsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prefsDir, "preferences.yaml"),
		[]byte("foo: bar\n  - this is not: valid yaml\n\t\tindent: broken"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := resolvePreferences(repo, "any-proj"); err == nil {
		t.Fatal("expected repo parse error")
	}
	if _, err := resolvePreferencesWithSources(repo, "any-proj"); err == nil {
		t.Fatal("expected repo parse error from sources")
	}
}

func TestResolvePreferences_LocalParseError(t *testing.T) {
	repo := t.TempDir()
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	dir := filepath.Join(agentsHome, "context", "broken-proj")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "preferences.local.yaml"),
		[]byte("foo: bar\n  - this is not: valid yaml\n\t\tindent: broken"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := resolvePreferences(repo, "broken-proj"); err == nil {
		t.Fatal("expected local parse error")
	}
	if _, err := resolvePreferencesWithSources(repo, "broken-proj"); err == nil {
		t.Fatal("expected local parse error from sources")
	}
}

func TestSetLocalPreference_ParseExistingError(t *testing.T) {
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	dir := filepath.Join(agentsHome, "context", "broken-proj")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "preferences.local.yaml"),
		[]byte("foo: bar\n  - this is not: valid yaml\n\t\tindent: broken"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := setLocalPreference("broken-proj", "verification.test_command", "go test"); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestSetLocalPreference_WriteFails(t *testing.T) {
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	sentinel := errors.New("marshal boom")
	prev := yamlMarshal
	yamlMarshal = func(v any) ([]byte, error) { return nil, sentinel }
	t.Cleanup(func() { yamlMarshal = prev })

	if err := setLocalPreference("p", "verification.test_command", "x"); !errors.Is(err, sentinel) {
		t.Fatalf("expected marshal sentinel, got %v", err)
	}
}

// TestResolvePreferences_RepoFileBranchCovered drives the `if repoFile != nil`
// branch (prefs.go:199-201) in resolvePreferences (not the With-Sources
// variant). Existing tests for resolvePreferences only exercise the
// absent-file path; this one writes a valid preferences.yaml and asserts
// the repo override survives the merge.
func TestResolvePreferences_RepoFileBranchCovered(t *testing.T) {
	repo := t.TempDir()
	prefsDir := filepath.Join(repo, ".agents", "workflow")
	if err := os.MkdirAll(prefsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prefsDir, "preferences.yaml"),
		[]byte("schema_version: 1\nverification:\n  test_command: \"make repo-only-test\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	prefs, err := resolvePreferences(repo, "rfp-proj")
	if err != nil {
		t.Fatal(err)
	}
	if strPtrVal(prefs.Verification.TestCommand) != "make repo-only-test" {
		t.Fatalf("repo preferences.yaml override not applied; got %q", strPtrVal(prefs.Verification.TestCommand))
	}
}

// TestResolvePreferences_LocalFileBranchCovered drives the `if localFile != nil`
// branch (prefs.go:207-209) in resolvePreferences. A local preferences file
// is seeded and the corresponding key must round-trip through the merge.
func TestResolvePreferences_LocalFileBranchCovered(t *testing.T) {
	repo := t.TempDir()
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	if err := setLocalPreference("lfp-proj", "execution.formatter", "prettier-only-local"); err != nil {
		t.Fatal(err)
	}

	prefs, err := resolvePreferences(repo, "lfp-proj")
	if err != nil {
		t.Fatal(err)
	}
	if strPtrVal(prefs.Execution.Formatter) != "prettier-only-local" {
		t.Fatalf("local preferences override not applied; got %q", strPtrVal(prefs.Execution.Formatter))
	}
}

// TestSetLocalPreference_MkdirAllError drives the `os.MkdirAll` failure
// branch (prefs.go:246-248). MkdirAll fails when its parent path is a
// regular file rather than a directory.
func TestSetLocalPreference_MkdirAllError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file-as-dir blocker behaves differently on windows")
	}
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", tmp)
	// Make the would-be parent of the preferences.local.yaml file a regular
	// file so the MkdirAll inside setLocalPreference errors.
	ctxParent := filepath.Join(tmp, "context")
	if err := os.WriteFile(ctxParent, []byte("blocker"), 0644); err != nil {
		t.Fatal(err)
	}
	err := setLocalPreference("any-proj", "verification.test_command", "x")
	if err == nil {
		t.Fatal("expected MkdirAll error when parent path is a file")
	}
}

// TestSetLocalPreference_ReadFileNonNotExistError drives the
// non-IsNotExist read-error branch (prefs.go:239-241). A pre-existing
// preferences.local.yaml is chmod'd unreadable so os.ReadFile returns a
// permission error that is NOT os.IsNotExist.
func TestSetLocalPreference_ReadFileNonNotExistError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod unreadable not portable on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("chmod unreadable unreliable as root")
	}
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", tmp)
	ctx := filepath.Join(tmp, "context", "locked-proj")
	if err := os.MkdirAll(ctx, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(ctx, "preferences.local.yaml")
	if err := os.WriteFile(path, []byte("schema_version: 1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	chmodUnreadable(t, path)

	err := setLocalPreference("locked-proj", "verification.test_command", "x")
	if err == nil {
		t.Fatal("expected ReadFile permission error to propagate")
	}
}
