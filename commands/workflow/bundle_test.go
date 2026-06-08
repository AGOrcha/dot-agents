package workflow

import (
	"bytes"
	"encoding/json"
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

// ── source-aware prompt_files (config-v2 Q1, Option B) ───────────────────────

// TestBundlePromptFilesFromRefs_TypedAndLegacy proves the bundle preserves the
// source/version provenance of a typed prompt_files entry while flattening a
// legacy bare-string entry to a plain path with empty source/version. This is
// the positive case for the source-aware bundle bridge.
func TestBundlePromptFilesFromRefs_TypedAndLegacy(t *testing.T) {
	refs := []config.PromptFileRef{
		{Path: "verifiers/verifier.base.md"},                             // legacy local
		{Source: "acme", Path: "verifiers/cli-runner.md", Version: "v2"}, // typed, pinned
		{Source: "acme", Path: "verifiers/cli-runner.project.md"},        // typed, no version
	}
	got := bundlePromptFilesFromRefs(refs)
	if len(got) != 3 {
		t.Fatalf("expected 3 prompt files, got %d: %+v", len(got), got)
	}
	if got[0].Source != "" || got[0].Path != "verifiers/verifier.base.md" || got[0].Version != "" {
		t.Errorf("legacy entry not flattened to local path: %+v", got[0])
	}
	if got[1].Source != "acme" || got[1].Path != "verifiers/cli-runner.md" || got[1].Version != "v2" {
		t.Errorf("typed pinned entry lost provenance: %+v", got[1])
	}
	if got[2].Source != "acme" || got[2].Version != "" {
		t.Errorf("typed entry without version mishandled: %+v", got[2])
	}
}

// TestBundlePromptFilesFromRefs_DropsBlankPath is the negative case: an entry
// whose path is empty or whitespace must never enter the bundle.
func TestBundlePromptFilesFromRefs_DropsBlankPath(t *testing.T) {
	refs := []config.PromptFileRef{
		{Path: "  "},                // whitespace-only -> dropped
		{Source: "acme", Path: ""},  // empty path -> dropped
		{Path: "verifiers/unit.md"}, // kept
	}
	got := bundlePromptFilesFromRefs(refs)
	if len(got) != 1 || got[0].Path != "verifiers/unit.md" {
		t.Fatalf("expected only the non-blank entry to survive, got %+v", got)
	}
}

// TestFlattenBundlePromptPaths reduces source-aware files to the flat []string
// surface the existing bundle prompt_files field accepts, skipping blanks.
func TestFlattenBundlePromptPaths(t *testing.T) {
	files := []bundlePromptFile{
		{Path: "a.md"},
		{Source: "acme", Path: "b.md", Version: "v1"},
		{Path: "   "}, // blank -> skipped
	}
	got := flattenBundlePromptPaths(files)
	want := []string{"a.md", "b.md"}
	if len(got) != len(want) {
		t.Fatalf("flatten: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("flatten[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestPromptFileRef_UnmarshalLegacyString covers the bare-string read path: a
// legacy prompt_files entry decodes to {path:<string>} with empty source/version.
func TestPromptFileRef_UnmarshalLegacyString(t *testing.T) {
	var r config.PromptFileRef
	if err := json.Unmarshal([]byte(`"verifiers/unit.md"`), &r); err != nil {
		t.Fatalf("unmarshal legacy string: %v", err)
	}
	if r.Path != "verifiers/unit.md" || r.Source != "" || r.Version != "" {
		t.Fatalf("legacy decode wrong: %+v", r)
	}
}

// TestPromptFileRef_UnmarshalTypedObject covers the typed-object read path.
func TestPromptFileRef_UnmarshalTypedObject(t *testing.T) {
	var r config.PromptFileRef
	in := `{"source":"acme","path":"verifiers/cli-runner.md","version":"v3"}`
	if err := json.Unmarshal([]byte(in), &r); err != nil {
		t.Fatalf("unmarshal typed object: %v", err)
	}
	if r.Source != "acme" || r.Path != "verifiers/cli-runner.md" || r.Version != "v3" {
		t.Fatalf("typed decode wrong: %+v", r)
	}
}

// TestPromptFileRef_UnmarshalRejectsBadShape is the negative case: a non
// string/object entry (and an object missing path) must error.
func TestPromptFileRef_UnmarshalRejectsBadShape(t *testing.T) {
	var r config.PromptFileRef
	if err := json.Unmarshal([]byte(`42`), &r); err == nil {
		t.Error("expected error for numeric prompt_files entry")
	}
	if err := json.Unmarshal([]byte(`{"source":"acme"}`), &r); err == nil {
		t.Error("expected error for object entry missing path")
	}
	if err := json.Unmarshal([]byte(`""`), &r); err == nil {
		t.Error("expected error for empty-string entry")
	}
}

// TestPromptFileRef_MarshalCompactAndObject proves the round-trip shape: a
// path-only ref marshals to the compact string form (legacy byte-for-byte
// stability) while a source/version ref marshals to the typed object.
func TestPromptFileRef_MarshalCompactAndObject(t *testing.T) {
	legacy := config.PromptFileRef{Path: "verifiers/unit.md"}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatalf("marshal legacy: %v", err)
	}
	if string(data) != `"verifiers/unit.md"` {
		t.Fatalf("legacy ref should marshal to a bare string, got %s", data)
	}

	typed := config.PromptFileRef{Source: "acme", Path: "verifiers/cli-runner.md", Version: "v2"}
	data, err = json.Marshal(typed)
	if err != nil {
		t.Fatalf("marshal typed: %v", err)
	}
	var back config.PromptFileRef
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("round-trip unmarshal: %v", err)
	}
	if back != typed {
		t.Fatalf("round-trip mismatch: got %+v want %+v", back, typed)
	}
}

// TestStageProfile_AgentsRCRoundTrip proves a legacy verifier_profiles block
// folds into stage_profiles.verifier on read and survives a full AgentsRC
// marshal/unmarshal cycle (re-emitted under stage_profiles, not the legacy key),
// mixing a legacy string entry and a source-pinned typed entry in one profile.
func TestStageProfile_AgentsRCRoundTrip(t *testing.T) {
	in := []byte(`{
  "version": 1,
  "sources": [{"type": "local"}],
  "verifier_profiles": {
    "cli-runner": {
      "label": "CLI runner",
      "prompt_files": [
        "verifiers/verifier.base.md",
        {"source": "acme", "path": "verifiers/cli-runner.md", "version": "v2"}
      ]
    }
  }
}`)
	var rc config.AgentsRC
	if err := json.Unmarshal(in, &rc); err != nil {
		t.Fatalf("unmarshal agentsrc: %v", err)
	}
	prof, ok := rc.StageProfiles["verifier"]["cli-runner"]
	if !ok {
		t.Fatalf("legacy verifier_profiles did not fold into stage_profiles.verifier: %+v", rc.StageProfiles)
	}
	if rc.ExtraFields["verifier_profiles"] != nil {
		t.Errorf("verifier_profiles leaked into ExtraFields: %s", rc.ExtraFields["verifier_profiles"])
	}
	if len(prof.PromptFiles) != 2 {
		t.Fatalf("expected 2 prompt files, got %d", len(prof.PromptFiles))
	}
	if prof.PromptFiles[0].Path != "verifiers/verifier.base.md" || prof.PromptFiles[0].Source != "" {
		t.Errorf("legacy entry decoded wrong: %+v", prof.PromptFiles[0])
	}
	if prof.PromptFiles[1].Source != "acme" || prof.PromptFiles[1].Version != "v2" {
		t.Errorf("typed entry decoded wrong: %+v", prof.PromptFiles[1])
	}

	out, err := json.Marshal(rc)
	if err != nil {
		t.Fatalf("marshal agentsrc: %v", err)
	}
	var rc2 config.AgentsRC
	if err := json.Unmarshal(out, &rc2); err != nil {
		t.Fatalf("re-unmarshal agentsrc: %v", err)
	}
	prof2 := rc2.StageProfiles["verifier"]["cli-runner"]
	if len(prof2.PromptFiles) != 2 || prof2.PromptFiles[1].Source != "acme" {
		t.Fatalf("round-trip lost typed prompt provenance: %+v", prof2.PromptFiles)
	}
}
