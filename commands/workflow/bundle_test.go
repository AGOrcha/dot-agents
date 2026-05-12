package workflow

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	bundle.Closeout.ParentMust = []string{"workflow_advance"}
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
