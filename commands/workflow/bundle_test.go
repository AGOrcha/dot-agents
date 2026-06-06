package workflow

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// TestResolveVerifierPromptFiles covers the source-aware prompt_files rendering:
// legacy bare paths (migrated to local) render to bare paths, remote entries
// render to "source-id:path@version" refs, and empty-path entries are skipped.
// Both positive (rendered values) and negative (empty inputs yield nil) cases
// are exercised.
func TestResolveVerifierPromptFiles(t *testing.T) {
	cases := []struct {
		name    string
		profile verifierProfile
		want    []string
	}{
		{
			name:    "nil prompt files yields nil",
			profile: verifierProfile{Label: "Unit"},
			want:    nil,
		},
		{
			name: "legacy local path renders bare",
			profile: verifierProfile{PromptFiles: []promptFile{
				{Source: "local", Path: ".agents/prompts/verifiers/unit.project.md"},
			}},
			want: []string{".agents/prompts/verifiers/unit.project.md"},
		},
		{
			name: "empty source treated as local",
			profile: verifierProfile{PromptFiles: []promptFile{
				{Path: "verifiers/api.project.md"},
			}},
			want: []string{"verifiers/api.project.md"},
		},
		{
			name: "remote source renders source:path",
			profile: verifierProfile{PromptFiles: []promptFile{
				{Source: "acme", Path: "verifiers/cli-runner.project.md"},
			}},
			want: []string{"acme:verifiers/cli-runner.project.md"},
		},
		{
			name: "remote source with version renders source:path@version",
			profile: verifierProfile{PromptFiles: []promptFile{
				{Source: "acme", Path: "verifiers/cli-runner.project.md", Version: "2.1.0"},
			}},
			want: []string{"acme:verifiers/cli-runner.project.md@2.1.0"},
		},
		{
			name: "mixed local and remote preserve order",
			profile: verifierProfile{PromptFiles: []promptFile{
				{Path: "verifiers/unit.project.md"},
				{Source: "acme", Path: "verifiers/cli-runner.project.md", Version: "2.1.0"},
			}},
			want: []string{"verifiers/unit.project.md", "acme:verifiers/cli-runner.project.md@2.1.0"},
		},
		{
			name: "blank-path entries are skipped",
			profile: verifierProfile{PromptFiles: []promptFile{
				{Path: "   "},
				{Source: "acme", Path: "verifiers/cli-runner.project.md"},
			}},
			want: []string{"acme:verifiers/cli-runner.project.md"},
		},
		{
			name: "all-blank entries yield nil",
			profile: verifierProfile{PromptFiles: []promptFile{
				{Path: ""},
				{Path: "  "},
			}},
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveVerifierPromptFiles(tc.profile)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("resolveVerifierPromptFiles() = %#v, want %#v", got, tc.want)
			}
		})
	}
}

// TestResolveSequencePromptFiles drives the bundle consumption seam end to end:
// legacy→typed migration, sequence ordering, cross-profile de-duplication,
// unknown/blank ids, the no-input branches, and the malformed-profile error.
func TestResolveSequencePromptFiles(t *testing.T) {
	const raw = `{
	  "unit": {"prompt_files": ["verifiers/unit.project.md"]},
	  "cli-runner": {"label": "CLI Runner", "prompt_files": [
	    "verifiers/unit.project.md",
	    {"source": "acme", "path": "verifiers/cli-runner.project.md", "version": "2.1.0"}
	  ]}
	}`
	t.Run("sequence order with cross-profile de-dup", func(t *testing.T) {
		got, err := resolveSequencePromptFiles(json.RawMessage(raw), []string{"unit", "cli-runner"})
		if err != nil {
			t.Fatalf("resolveSequencePromptFiles: %v", err)
		}
		want := []string{
			"verifiers/unit.project.md",
			"acme:verifiers/cli-runner.project.md@2.1.0",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	})
	t.Run("unknown and blank ids contribute nothing", func(t *testing.T) {
		got, err := resolveSequencePromptFiles(json.RawMessage(raw), []string{"nope", "", "  "})
		if err != nil {
			t.Fatalf("resolveSequencePromptFiles: %v", err)
		}
		if got != nil {
			t.Fatalf("got %#v, want nil", got)
		}
	})
	t.Run("empty profiles or empty sequence yields nil", func(t *testing.T) {
		if got, err := resolveSequencePromptFiles(nil, []string{"unit"}); err != nil || got != nil {
			t.Fatalf("empty profiles: got %#v, err %v", got, err)
		}
		if got, err := resolveSequencePromptFiles(json.RawMessage(raw), nil); err != nil || got != nil {
			t.Fatalf("empty sequence: got %#v, err %v", got, err)
		}
	})
	t.Run("malformed profiles surfaces error", func(t *testing.T) {
		if _, err := resolveSequencePromptFiles(json.RawMessage(`{"unit":{"prompt_files":[42]}}`), []string{"unit"}); err == nil {
			t.Fatal("expected error from malformed profiles, got nil")
		}
	})
}

// TestVerifierProfileMigration_TypedAndLegacy exercises the migration seam
// (promptFile/verifierProfile JSON behaviour, parseVerifierProfiles). This is
// the source-aware prompt_files contract: legacy flat strings migrate to
// repo-local typed objects, typed objects preserve source/version, and the
// rendered ref matches what the delegation bundle consumes.
func TestVerifierProfileMigration_TypedAndLegacy(t *testing.T) {
	const raw = `{
	  "unit": {"label": "Unit (Go)", "prompt_files": [".agents/prompts/verifiers/unit.project.md"]},
	  "cli-runner": {"label": "CLI Runner", "prompt_files": [
	    {"source": "acme", "path": "verifiers/cli-runner.project.md", "version": "2.1.0"},
	    "verifiers/cli-runner.local.md"
	  ]}
	}`
	profiles, err := parseVerifierProfiles(json.RawMessage(raw))
	if err != nil {
		t.Fatalf("parseVerifierProfiles: %v", err)
	}
	if len(profiles) != 2 {
		t.Fatalf("expected 2 profiles, got %d", len(profiles))
	}

	unit := profiles["unit"]
	if unit.Label != "Unit (Go)" {
		t.Fatalf("unit label = %q", unit.Label)
	}
	if len(unit.PromptFiles) != 1 || !unit.PromptFiles[0].isLocal() {
		t.Fatalf("unit: legacy bare string did not migrate to local: %+v", unit.PromptFiles)
	}
	if got := unit.PromptFiles[0].ref(); got != ".agents/prompts/verifiers/unit.project.md" {
		t.Fatalf("unit ref = %q", got)
	}

	cli := profiles["cli-runner"]
	if len(cli.PromptFiles) != 2 {
		t.Fatalf("cli-runner: expected 2 prompt files, got %d", len(cli.PromptFiles))
	}
	if cli.PromptFiles[0].Source != "acme" || cli.PromptFiles[0].Version != "2.1.0" || cli.PromptFiles[0].isLocal() {
		t.Fatalf("cli-runner[0]: typed object not preserved: %+v", cli.PromptFiles[0])
	}
	if got := cli.PromptFiles[0].ref(); got != "acme:verifiers/cli-runner.project.md@2.1.0" {
		t.Fatalf("cli-runner[0] ref = %q", got)
	}
	if !cli.PromptFiles[1].isLocal() {
		t.Fatalf("cli-runner[1]: bare string should be local: %+v", cli.PromptFiles[1])
	}
}

// TestParseVerifierProfiles_EmptyAndNull covers the no-input branches.
func TestParseVerifierProfiles_EmptyAndNull(t *testing.T) {
	for _, raw := range []json.RawMessage{nil, json.RawMessage(""), json.RawMessage("null")} {
		got, err := parseVerifierProfiles(raw)
		if err != nil {
			t.Fatalf("parseVerifierProfiles(%q): %v", raw, err)
		}
		if got != nil {
			t.Fatalf("parseVerifierProfiles(%q) = %#v, want nil", raw, got)
		}
	}
}

// TestParseVerifierProfiles_Malformed exercises the decode-error paths: the
// object-form path-required case, a non-string/non-object entry, and a profile
// that is not an object.
func TestParseVerifierProfiles_Malformed(t *testing.T) {
	cases := []string{
		`{"unit": {"prompt_files": [{"source": "acme"}]}}`, // object form missing path
		`{"unit": {"prompt_files": [42]}}`,                 // entry neither string nor object
		`{"unit": "not-an-object"}`,                        // profile not an object
	}
	for _, raw := range cases {
		if _, err := parseVerifierProfiles(json.RawMessage(raw)); err == nil {
			t.Fatalf("parseVerifierProfiles(%q): expected error, got nil", raw)
		}
	}
}

// TestPromptFileRoundTrip proves a promptFile marshals to the compact legacy
// shape when local+unversioned and to the typed object otherwise, and survives
// a marshal→unmarshal round-trip via the stable rendered ref.
func TestPromptFileRoundTrip(t *testing.T) {
	cases := []struct {
		name     string
		in       promptFile
		wantJSON string
	}{
		{
			name:     "local unversioned marshals to bare string",
			in:       promptFile{Source: "local", Path: "p.md"},
			wantJSON: `"p.md"`,
		},
		{
			name:     "empty source unversioned marshals to bare string",
			in:       promptFile{Path: "p.md"},
			wantJSON: `"p.md"`,
		},
		{
			name:     "remote marshals to object",
			in:       promptFile{Source: "acme", Path: "p.md", Version: "1.0.0"},
			wantJSON: `{"source":"acme","path":"p.md","version":"1.0.0"}`,
		},
		{
			name:     "local versioned marshals to object",
			in:       promptFile{Source: "local", Path: "p.md", Version: "9"},
			wantJSON: `{"source":"local","path":"p.md","version":"9"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.in)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(data) != tc.wantJSON {
				t.Fatalf("marshal = %s, want %s", data, tc.wantJSON)
			}
			var back promptFile
			if err := json.Unmarshal(data, &back); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if back.ref() != tc.in.ref() {
				t.Fatalf("round-trip ref = %q, want %q", back.ref(), tc.in.ref())
			}
		})
	}
}

// TestPromptFileUnmarshal_ObjectMissingPath covers the object-form
// path-required validation error directly on promptFile.UnmarshalJSON, plus the
// non-string/non-object decode-error branch.
func TestPromptFileUnmarshal_ObjectMissingPath(t *testing.T) {
	var p promptFile
	if err := json.Unmarshal([]byte(`{"source":"acme","path":"  "}`), &p); err == nil {
		t.Fatal("expected non-empty path error, got nil")
	}
	if err := json.Unmarshal([]byte(`[1,2,3]`), &p); err == nil {
		t.Fatal("expected decode error for non-string/non-object, got nil")
	}
}

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
