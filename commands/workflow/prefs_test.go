package workflow

import (
	"bytes"
	"os"
	"path/filepath"
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

// ── Wave 5: Graph bridge types ────────────────────────────────────────────────
