package workflow

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/config"
	"go.yaml.in/yaml/v3"
)

func TestExpandBundleStages_NoVerifiers(t *testing.T) {
	b := &delegationBundleYAML{PlanID: "p1", TaskID: "t1"}
	stages := expandBundleStages(b)
	if len(stages) != 2 {
		t.Fatalf("expected 2 stages (impl+review), got %d: %+v", len(stages), stages)
	}
	if stages[0].Stage != "impl" {
		t.Fatalf("stage[0] = %q, want impl", stages[0].Stage)
	}
	if stages[1].Stage != "review" {
		t.Fatalf("stage[1] = %q, want review", stages[1].Stage)
	}
	if stages[0].VerifierType != "" || stages[1].VerifierType != "" {
		t.Fatalf("unexpected verifier_type on non-verifier stages: %+v", stages)
	}
}

func TestExpandBundleStages_WithVerifiers(t *testing.T) {
	b := &delegationBundleYAML{PlanID: "p1", TaskID: "t1"}
	b.Verification.VerifierSequence = []string{"unit", "api"}
	stages := expandBundleStages(b)
	if len(stages) != 4 {
		t.Fatalf("expected 4 stages, got %d: %+v", len(stages), stages)
	}
	want := []struct{ stage, vt string }{
		{"impl", ""},
		{"verifier", "unit"},
		{"verifier", "api"},
		{"review", ""},
	}
	for i, w := range want {
		if stages[i].Stage != w.stage || stages[i].VerifierType != w.vt {
			t.Fatalf("stage[%d]: got {%q, %q}, want {%q, %q}", i, stages[i].Stage, stages[i].VerifierType, w.stage, w.vt)
		}
	}
}

func TestExpandBundleStages_SkipsBlankVerifiers(t *testing.T) {
	b := &delegationBundleYAML{PlanID: "p1", TaskID: "t1"}
	b.Verification.VerifierSequence = []string{"unit", "", "  ", "api"}
	stages := expandBundleStages(b)
	var verifierCount int
	for _, s := range stages {
		if s.Stage == "verifier" {
			verifierCount++
		}
	}
	if verifierCount != 2 {
		t.Fatalf("expected 2 verifier stages, got %d: %+v", verifierCount, stages)
	}
}

func writeBundleFixture(t *testing.T, dir string, verifierSeq []string) string {
	t.Helper()
	bundle := delegationBundleYAML{
		SchemaVersion: 1,
		DelegationID:  "del-t1-1",
		PlanID:        "plan-001",
		TaskID:        "task-001",
		Owner:         "test",
	}
	bundle.Worker.Profile = "loop-worker"
	bundle.Verification.VerifierSequence = verifierSeq
	bundle.Verification.FeedbackGoal = "test"
	bundle.Closeout.WorkerMust = []string{"workflow_verify_record"}
	bundle.Closeout.ParentMust = []string{"workflow_delegation_closeout"}
	data, err := yaml.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	bundleDir := filepath.Join(dir, ".agents", "active", "delegation-bundles")
	if err := os.MkdirAll(bundleDir, 0755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(bundleDir, "del-t1-1.yaml")
	if err := os.WriteFile(p, data, 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestWorkflowBundleStages_TextNoVerifiers(t *testing.T) {
	repo := setupTestProject(t)
	bundlePath := writeBundleFixture(t, repo, nil)

	var buf bytes.Buffer
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runWorkflowBundleStages(bundlePath)

	_ = w.Close()
	os.Stdout = oldStdout
	_, _ = buf.ReadFrom(r)

	if err != nil {
		t.Fatalf("runWorkflowBundleStages: %v", err)
	}
	got := strings.TrimSpace(buf.String())
	want := "impl\nreview"
	if got != want {
		t.Fatalf("text output:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestWorkflowBundleStages_TextWithVerifiers(t *testing.T) {
	repo := setupTestProject(t)
	bundlePath := writeBundleFixture(t, repo, []string{"unit", "api"})

	var buf bytes.Buffer
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runWorkflowBundleStages(bundlePath)

	_ = w.Close()
	os.Stdout = oldStdout
	_, _ = buf.ReadFrom(r)

	if err != nil {
		t.Fatalf("runWorkflowBundleStages: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 lines, got %d: %v", len(lines), lines)
	}
	wantLines := []string{"impl", "verifier:unit", "verifier:api", "review"}
	for i, want := range wantLines {
		if lines[i] != want {
			t.Fatalf("line[%d] = %q, want %q", i, lines[i], want)
		}
	}
}

func TestWorkflowBundleStages_ViaCommand(t *testing.T) {
	repo := setupTestProject(t)
	bundlePath := writeBundleFixture(t, repo, []string{"unit"})
	if err := executeWorkflowCommand(t, repo, "bundle", "stages", bundlePath); err != nil {
		t.Fatalf("workflow bundle stages: %v", err)
	}
}

func TestWorkflowBundleStages_MissingTaskID(t *testing.T) {
	dir := t.TempDir()
	bundle := delegationBundleYAML{SchemaVersion: 1, PlanID: "p1"}
	data, _ := yaml.Marshal(bundle)
	p := filepath.Join(dir, "bad.yaml")
	_ = os.WriteFile(p, data, 0644)
	if err := runWorkflowBundleStages(p); err == nil || !strings.Contains(err.Error(), "task_id") {
		t.Fatalf("expected task_id error, got %v", err)
	}
}

// TestWorkflowBundleStages_BundleNotFound covers the read-error branch.
func TestWorkflowBundleStages_BundleNotFound(t *testing.T) {
	err := runWorkflowBundleStages("/nonexistent/path/bundle.yaml")
	if err == nil || !strings.Contains(err.Error(), "read bundle") {
		t.Fatalf("expected read bundle error, got %v", err)
	}
}

// TestWorkflowBundleStages_MalformedYAML covers the yaml.Unmarshal error path.
func TestWorkflowBundleStages_MalformedYAML(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(p, []byte("\t:not yaml:\n"), 0644); err != nil {
		t.Fatal(err)
	}
	err := runWorkflowBundleStages(p)
	if err == nil || !strings.Contains(err.Error(), "parse bundle") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

// TestVerifierProfilePromptRefs_LegacyAndTyped covers the positive path: a
// profile mixing a legacy repo-local string entry, a source entry, and a
// versioned source entry yields the canonical source-aware refs in order.
func TestVerifierProfilePromptRefs_LegacyAndTyped(t *testing.T) {
	p := config.VerifierProfile{
		Label: "Unit",
		PromptFiles: []config.VerifierPromptFile{
			{Path: ".agents/prompts/verifiers/unit.project.md"},
			{Source: "acme", Path: "verifiers/shared.md"},
			{Source: "acme", Path: "verifiers/pinned.md", Version: "^1.2"},
		},
	}
	got := verifierProfilePromptRefs(p)
	want := []string{
		".agents/prompts/verifiers/unit.project.md",
		"acme:verifiers/shared.md",
		"acme:verifiers/pinned.md@^1.2",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d refs, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ref[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestVerifierProfilePromptRefs_SkipsBlankPath covers the negative-input path:
// entries with an empty path are dropped rather than emitting an empty ref.
func TestVerifierProfilePromptRefs_SkipsBlankPath(t *testing.T) {
	p := config.VerifierProfile{
		PromptFiles: []config.VerifierPromptFile{
			{Path: "  "},
			{Path: "verifiers/real.md"},
			{Path: ""},
		},
	}
	got := verifierProfilePromptRefs(p)
	if len(got) != 1 || got[0] != "verifiers/real.md" {
		t.Fatalf("expected only the non-blank ref, got %v", got)
	}
}

// TestVerifierProfilePromptRefs_Empty covers a profile with no prompt files.
func TestVerifierProfilePromptRefs_Empty(t *testing.T) {
	got := verifierProfilePromptRefs(config.VerifierProfile{})
	if len(got) != 0 {
		t.Fatalf("expected no refs, got %v", got)
	}
}

// TestResolveVerifierStagePromptRefs_Found resolves a declared verifier profile
// and returns its source-aware refs.
func TestResolveVerifierStagePromptRefs_Found(t *testing.T) {
	rc := &config.AgentsRC{
		VerifierProfiles: map[string]config.VerifierProfile{
			"unit": {
				Label: "Unit",
				PromptFiles: []config.VerifierPromptFile{
					{Source: "acme", Path: "verifiers/unit.md", Version: "1.0.0"},
				},
			},
		},
	}
	got, err := resolveVerifierStagePromptRefs(rc, " unit ")
	if err != nil {
		t.Fatalf("resolveVerifierStagePromptRefs: %v", err)
	}
	if len(got) != 1 || got[0] != "acme:verifiers/unit.md@1.0.0" {
		t.Fatalf("unexpected refs: %v", got)
	}
}

// TestResolveVerifierStagePromptRefs_Undefined covers the negative path: an
// unknown verifier_type errors, mirroring fanout-time validation.
func TestResolveVerifierStagePromptRefs_Undefined(t *testing.T) {
	rc := &config.AgentsRC{
		VerifierProfiles: map[string]config.VerifierProfile{
			"unit": {Label: "Unit"},
		},
	}
	_, err := resolveVerifierStagePromptRefs(rc, "cli-runner")
	if err == nil || !strings.Contains(err.Error(), "not defined under verifier_profiles") {
		t.Fatalf("expected undefined-profile error, got %v", err)
	}
}

// TestResolveVerifierStagePromptRefs_EmptyType rejects a blank verifier_type.
func TestResolveVerifierStagePromptRefs_EmptyType(t *testing.T) {
	if _, err := resolveVerifierStagePromptRefs(&config.AgentsRC{}, "   "); err == nil ||
		!strings.Contains(err.Error(), "empty verifier_type") {
		t.Fatalf("expected empty verifier_type error, got %v", err)
	}
}

// TestResolveVerifierStagePromptRefs_NilConfig errors when no manifest is loaded.
func TestResolveVerifierStagePromptRefs_NilConfig(t *testing.T) {
	if _, err := resolveVerifierStagePromptRefs(nil, "unit"); err == nil ||
		!strings.Contains(err.Error(), "not defined under verifier_profiles") {
		t.Fatalf("expected not-defined error for nil config, got %v", err)
	}
}

// TestExpandBundleStages_TrimsWhitespace ensures verifier_type values are
// trimmed before stage expansion.
func TestExpandBundleStages_TrimsWhitespace(t *testing.T) {
	b := &delegationBundleYAML{PlanID: "p1", TaskID: "t1"}
	b.Verification.VerifierSequence = []string{"  unit  ", " api "}
	stages := expandBundleStages(b)
	if len(stages) != 4 {
		t.Fatalf("expected 4 stages, got %d", len(stages))
	}
	if stages[1].VerifierType != "unit" || stages[2].VerifierType != "api" {
		t.Fatalf("expected trimmed verifier_type values, got %+v", stages)
	}
}

// TestWorkflowBundleStages_JSONOutput covers the JSON encoding branch.
func TestWorkflowBundleStages_JSONOutput(t *testing.T) {
	repo := setupTestProject(t)
	bundlePath := writeBundleFixture(t, repo, []string{"unit"})

	workflowTestJSON = true
	defer func() { workflowTestJSON = false }()

	var buf bytes.Buffer
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err := runWorkflowBundleStages(bundlePath)
	_ = w.Close()
	os.Stdout = oldStdout
	_, _ = buf.ReadFrom(r)
	if err != nil {
		t.Fatalf("runWorkflowBundleStages: %v", err)
	}
	got := strings.TrimSpace(buf.String())
	if !strings.HasPrefix(got, "[") || !strings.HasSuffix(got, "]") {
		t.Fatalf("expected JSON array output, got: %s", got)
	}
	if !strings.Contains(got, `"stage": "impl"`) || !strings.Contains(got, `"verifier_type": "unit"`) || !strings.Contains(got, `"stage": "review"`) {
		t.Fatalf("missing expected stage entries in JSON: %s", got)
	}
}
