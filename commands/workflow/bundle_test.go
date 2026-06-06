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
	stages := expandBundleStages(b, nil)
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
	stages := expandBundleStages(b, nil)
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
	stages := expandBundleStages(b, nil)
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
	stages := expandBundleStages(b, nil)
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

// ── p1c: source-aware verifier_profile.prompt_files ──────────────────────────

// TestExpandBundleStages_SourceAwarePromptFiles asserts that a verifier stage is
// annotated with its profile's typed prompt files (positive path) and that a
// verifier type with no matching profile carries no prompt files (negative path).
func TestExpandBundleStages_SourceAwarePromptFiles(t *testing.T) {
	b := &delegationBundleYAML{PlanID: "p1", TaskID: "t1"}
	b.Verification.VerifierSequence = []string{"unit", "cli-runner", "unmapped"}
	profiles := map[string]config.VerifierProfile{
		"unit": {
			Label: "Unit (Go)",
			PromptFiles: []config.VerifierPromptFile{
				{Path: ".agents/prompts/verifiers/unit.project.md"}, // legacy local
			},
		},
		"cli-runner": {
			Label: "CLI Runner",
			PromptFiles: []config.VerifierPromptFile{
				{Source: "acme", Path: "verifiers/cli-runner.md", Version: "^1.2"}, // typed remote
			},
		},
	}
	stages := expandBundleStages(b, profiles)
	if len(stages) != 5 {
		t.Fatalf("expected 5 stages (impl + 3 verifier + review), got %d: %+v", len(stages), stages)
	}

	unit := stages[1]
	if unit.VerifierType != "unit" || len(unit.PromptFiles) != 1 {
		t.Fatalf("unit stage: got %+v", unit)
	}
	if unit.PromptFiles[0].Source != "" || unit.PromptFiles[0].Path != ".agents/prompts/verifiers/unit.project.md" {
		t.Fatalf("unit prompt file not local/path-only: %+v", unit.PromptFiles[0])
	}

	cli := stages[2]
	if cli.VerifierType != "cli-runner" || len(cli.PromptFiles) != 1 {
		t.Fatalf("cli-runner stage: got %+v", cli)
	}
	if cli.PromptFiles[0].Source != "acme" || cli.PromptFiles[0].Path != "verifiers/cli-runner.md" || cli.PromptFiles[0].Version != "^1.2" {
		t.Fatalf("cli-runner prompt file not source-aware: %+v", cli.PromptFiles[0])
	}

	// Negative: a verifier type with no profile entry has no prompt files.
	unmapped := stages[3]
	if unmapped.VerifierType != "unmapped" || unmapped.PromptFiles != nil {
		t.Fatalf("unmapped verifier should carry no prompt files: %+v", unmapped)
	}
}

// TestWorkflowBundleStages_SourceAwareText runs the full command path against a
// project .agentsrc.json that declares a source-aware verifier profile and a
// legacy string profile, asserting both the typed and legacy forms surface.
func TestWorkflowBundleStages_SourceAwareText(t *testing.T) {
	repo := setupTestProject(t)
	writeAgentsRCWithProfiles(t, repo, `{
  "version": 1,
  "project": "test",
  "sources": [{"type": "local"}],
  "verifier_profiles": {
    "unit": {"label": "Unit", "prompt_files": [".agents/prompts/verifiers/unit.project.md"]},
    "cli-runner": {"label": "CLI", "prompt_files": [{"source": "acme", "path": "verifiers/cli.md", "version": "^1"}]}
  }
}`)
	bundlePath := writeBundleFixture(t, repo, []string{"unit", "cli-runner"})

	got := captureBundleStagesOutput(t, bundlePath)
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 lines, got %d: %v", len(lines), lines)
	}
	if lines[1] != "verifier:unit\t.agents/prompts/verifiers/unit.project.md" {
		t.Fatalf("unit line not source-aware: %q", lines[1])
	}
	if lines[2] != "verifier:cli-runner\tacme/verifiers/cli.md@^1" {
		t.Fatalf("cli-runner line not source-aware: %q", lines[2])
	}
}

// TestWorkflowBundleStages_SourceAwareJSON asserts the JSON output carries the
// typed prompt_files payload for resolved verifier stages.
func TestWorkflowBundleStages_SourceAwareJSON(t *testing.T) {
	repo := setupTestProject(t)
	writeAgentsRCWithProfiles(t, repo, `{
  "version": 1,
  "project": "test",
  "sources": [{"type": "local"}],
  "verifier_profiles": {
    "cli-runner": {"label": "CLI", "prompt_files": [{"source": "acme", "path": "verifiers/cli.md", "version": "^1"}]}
  }
}`)
	bundlePath := writeBundleFixture(t, repo, []string{"cli-runner"})

	workflowTestJSON = true
	defer func() { workflowTestJSON = false }()

	got := captureBundleStagesOutput(t, bundlePath)
	var stages []bundleStageEntry
	if err := json.Unmarshal([]byte(got), &stages); err != nil {
		t.Fatalf("decode json: %v\n%s", err, got)
	}
	var found bool
	for _, s := range stages {
		if s.VerifierType == "cli-runner" {
			found = true
			if len(s.PromptFiles) != 1 || s.PromptFiles[0].Source != "acme" || s.PromptFiles[0].Version != "^1" {
				t.Fatalf("cli-runner JSON prompt files wrong: %+v", s.PromptFiles)
			}
		}
	}
	if !found {
		t.Fatalf("cli-runner stage missing from JSON: %s", got)
	}
}

// TestLoadVerifierProfiles_NoManifest covers the nil-registry branch when the
// bundle lives outside any project (no .agentsrc.json up the tree).
func TestLoadVerifierProfiles_NoManifest(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "stray.yaml")
	if err := os.WriteFile(p, []byte("task_id: t1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	profiles, err := loadVerifierProfiles(p)
	if err != nil {
		t.Fatalf("loadVerifierProfiles: %v", err)
	}
	if profiles != nil {
		t.Fatalf("expected nil profiles for bundle with no manifest, got %+v", profiles)
	}
}

// TestProjectRootForBundle_FindsManifest asserts the upward walk locates the
// directory holding .agentsrc.json from a nested bundle path.
func TestProjectRootForBundle_FindsManifest(t *testing.T) {
	repo := setupTestProject(t)
	writeAgentsRCWithProfiles(t, repo, `{"version": 1, "project": "test", "sources": [{"type": "local"}]}`)
	bundlePath := writeBundleFixture(t, repo, []string{"unit"})
	got := projectRootForBundle(bundlePath)
	wantAbs, _ := filepath.Abs(repo)
	gotAbs, _ := filepath.Abs(got)
	// macOS /var → /private/var symlink; compare via EvalSymlinks.
	wantReal, _ := filepath.EvalSymlinks(wantAbs)
	gotReal, _ := filepath.EvalSymlinks(gotAbs)
	if gotReal != wantReal {
		t.Fatalf("project root: got %q, want %q", gotReal, wantReal)
	}
}

// TestVerifierProfileTypes_RoundTrip exercises the source-aware
// config.VerifierProfile / VerifierPromptFile marshal+unmarshal contract: legacy
// bare-string prompt files round-trip to strings, typed entries round-trip to
// objects, and forward-compatible profile keys are preserved via Extra.
func TestVerifierProfileTypes_RoundTrip(t *testing.T) {
	in := map[string]config.VerifierProfile{
		"unit": {
			Label:       "Unit",
			PromptFiles: []config.VerifierPromptFile{{Path: ".agents/prompts/unit.md"}},
		},
		"cli-runner": {
			Label: "CLI",
			PromptFiles: []config.VerifierPromptFile{
				{Source: "acme", Path: "verifiers/cli.md", Version: "^1.2"},
				{Path: "local/extra.md"},
			},
			Extra: map[string]json.RawMessage{"kind": json.RawMessage(`"go-cli"`)},
		},
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Legacy bare-string form must serialize as a plain string, not an object.
	if !strings.Contains(string(data), `".agents/prompts/unit.md"`) {
		t.Fatalf("local prompt file did not marshal as bare string: %s", data)
	}
	if !strings.Contains(string(data), `"source":"acme"`) || !strings.Contains(string(data), `"version":"^1.2"`) {
		t.Fatalf("typed prompt file did not marshal as object: %s", data)
	}
	if !strings.Contains(string(data), `"kind":"go-cli"`) {
		t.Fatalf("forward-compat profile key dropped: %s", data)
	}

	var out map[string]config.VerifierProfile
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	unit := out["unit"].PromptFiles[0]
	if !unit.IsLocal() || unit.Path != ".agents/prompts/unit.md" {
		t.Fatalf("unit prompt file not local round-trip: %+v", unit)
	}
	cli := out["cli-runner"]
	if cli.PromptFiles[0].Source != "acme" || cli.PromptFiles[0].Version != "^1.2" {
		t.Fatalf("cli typed prompt file lost source-awareness: %+v", cli.PromptFiles[0])
	}
	if cli.PromptFiles[1].IsLocal() != true {
		t.Fatalf("cli second prompt file should be local: %+v", cli.PromptFiles[1])
	}
	if string(cli.Extra["kind"]) != `"go-cli"` {
		t.Fatalf("forward-compat key not preserved on unmarshal: %v", cli.Extra)
	}
}

// TestVerifierPromptFile_UnmarshalErrors covers the rejection paths: a non
// string/object scalar and an object missing the required path.
func TestVerifierPromptFile_UnmarshalErrors(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"wrong-type", `42`, "must be a string or"},
		{"empty-path", `{"source":"acme"}`, "non-empty path"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var f config.VerifierPromptFile
			err := json.Unmarshal([]byte(tc.in), &f)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got err=%v, want contains %q", err, tc.want)
			}
		})
	}
}

// TestVerifierProfile_UnmarshalError covers the malformed-profile path.
func TestVerifierProfile_UnmarshalError(t *testing.T) {
	var p config.VerifierProfile
	if err := json.Unmarshal([]byte(`["not","an","object"]`), &p); err == nil {
		t.Fatalf("expected error unmarshaling array into VerifierProfile")
	}
}

// TestAgentsRC_VerifierProfilesRoundTrip asserts the promoted typed field
// survives a full LoadAgentsRC → Save → LoadAgentsRC cycle on disk, preserving
// both legacy and source-aware prompt files alongside untyped extras.
func TestAgentsRC_VerifierProfilesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	body := `{
  "version": 1,
  "project": "rt",
  "sources": [{"type": "local"}],
  "app_type_verifier_map": {"go-cli": ["unit"]},
  "verifier_profiles": {
    "unit": {"label": "Unit", "prompt_files": [".agents/prompts/unit.md"]},
    "cli-runner": {"label": "CLI", "prompt_files": [{"source": "acme", "path": "v/cli.md", "version": "^1"}]}
  }
}`
	if err := os.WriteFile(filepath.Join(dir, config.AgentsRCFile), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	rc, err := config.LoadAgentsRC(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(rc.VerifierProfiles) != 2 {
		t.Fatalf("expected 2 verifier profiles, got %d", len(rc.VerifierProfiles))
	}
	if err := rc.Save(dir); err != nil {
		t.Fatalf("save: %v", err)
	}
	rc2, err := config.LoadAgentsRC(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !rc2.VerifierProfiles["unit"].PromptFiles[0].IsLocal() {
		t.Fatalf("unit prompt file not local after round-trip: %+v", rc2.VerifierProfiles["unit"])
	}
	cli := rc2.VerifierProfiles["cli-runner"].PromptFiles[0]
	if cli.Source != "acme" || cli.Path != "v/cli.md" || cli.Version != "^1" {
		t.Fatalf("cli prompt file lost source-awareness after round-trip: %+v", cli)
	}
	// app_type_verifier_map (still ExtraFields) must survive the round-trip too.
	if _, ok := rc2.ExtraFields["app_type_verifier_map"]; !ok {
		t.Fatalf("app_type_verifier_map dropped from ExtraFields on round-trip")
	}
}

// TestExpandBundleStages_ProfileWithoutPromptFiles covers the empty-prompt-files
// early return in promptFileRefs: a declared profile carrying no prompt files
// yields a verifier stage with nil PromptFiles.
func TestExpandBundleStages_ProfileWithoutPromptFiles(t *testing.T) {
	b := &delegationBundleYAML{PlanID: "p1", TaskID: "t1"}
	b.Verification.VerifierSequence = []string{"unit"}
	profiles := map[string]config.VerifierProfile{"unit": {Label: "Unit"}}
	stages := expandBundleStages(b, profiles)
	if stages[1].VerifierType != "unit" || stages[1].PromptFiles != nil {
		t.Fatalf("expected verifier stage with no prompt files, got %+v", stages[1])
	}
}

// TestLoadVerifierProfiles_MalformedManifest covers the LoadAgentsRC error branch
// (a non-IsNotExist failure) in loadVerifierProfiles.
func TestLoadVerifierProfiles_MalformedManifest(t *testing.T) {
	repo := setupTestProject(t)
	writeAgentsRCWithProfiles(t, repo, `{ this is : not json`)
	bundlePath := writeBundleFixture(t, repo, []string{"unit"})
	if _, err := loadVerifierProfiles(bundlePath); err == nil || !strings.Contains(err.Error(), "bundle prompt files") {
		t.Fatalf("expected load error, got %v", err)
	}
}

// TestWorkflowBundleStages_MalformedManifestPropagates covers the error return
// from runWorkflowBundleStages when the project manifest fails to parse.
func TestWorkflowBundleStages_MalformedManifestPropagates(t *testing.T) {
	repo := setupTestProject(t)
	writeAgentsRCWithProfiles(t, repo, `{ broken`)
	bundlePath := writeBundleFixture(t, repo, []string{"unit"})
	if err := runWorkflowBundleStages(bundlePath); err == nil || !strings.Contains(err.Error(), "bundle prompt files") {
		t.Fatalf("expected propagated manifest error, got %v", err)
	}
}

func writeAgentsRCWithProfiles(t *testing.T, repo, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, config.AgentsRCFile), []byte(body), 0644); err != nil {
		t.Fatalf("write .agentsrc.json: %v", err)
	}
}

func captureBundleStagesOutput(t *testing.T, bundlePath string) string {
	t.Helper()
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err := runWorkflowBundleStages(bundlePath)
	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	if err != nil {
		t.Fatalf("runWorkflowBundleStages: %v", err)
	}
	return buf.String()
}
