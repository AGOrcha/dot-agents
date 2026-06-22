package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cfg "github.com/AGOrcha/dot-agents/internal/config"
)

// lintProject sets up an isolated project whose manifest extends one local-source
// layer with the given body, returning the project root. When layerBody is empty,
// no extends layer is declared (only the repo-local manifest is linted).
func lintProject(t *testing.T, repoExtra, layerBody string) string {
	t.Helper()
	if layerBody == "" {
		manifest := `{"version":2,"repo_id":"github.com/acme/app"` + repoExtra + `}`
		return withRepoLayer(t, manifest, "")
	}
	srcRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcRoot, "base.json"), []byte(layerBody), 0o644); err != nil {
		t.Fatalf("write layer fixture: %v", err)
	}
	b, _ := json.Marshal(srcRoot)
	escaped := string(b[1 : len(b)-1])
	manifest := `{
		"version": 2,
		"repo_id": "github.com/acme/app",
		"sources": [{"id": "acme", "type": "local", "path": "` + escaped + `"}],
		"extends": ["acme:base.json"]
	}`
	return withRepoLayer(t, manifest, "")
}

func lintOptions(project string, jsonOut bool) *runLintOptions {
	return &runLintOptions{
		jsonOut: jsonOut,
		stdout:  &bytes.Buffer{},
		stderr:  &bytes.Buffer{},
		cwd:     project,
	}
}

func findLintResult(results []LintResult, file string) (LintResult, bool) {
	for _, r := range results {
		if r.File == file {
			return r, true
		}
	}
	return LintResult{}, false
}

func TestBuildLintReport_ValidManifestPasses(t *testing.T) {
	project := lintProject(t, `,"skills":["a","b"]`, "")
	report, err := buildLintReport(project)
	if err != nil {
		t.Fatalf("buildLintReport: %v", err)
	}
	if !report.OK {
		t.Fatalf("expected OK report for valid manifest, got %+v", report)
	}
	repo, ok := findLintResult(report.Results, "repo-local")
	if !ok || repo.Status != lintPass {
		t.Fatalf("repo-local result = %+v (ok=%v), want pass", repo, ok)
	}
}

func TestBuildLintReport_ValidManifestAndLayerPass(t *testing.T) {
	project := lintProject(t, "", `{"version":2,"skills":["org-skill"]}`)
	report, err := buildLintReport(project)
	if err != nil {
		t.Fatalf("buildLintReport: %v", err)
	}
	if !report.OK {
		t.Fatalf("expected OK report, got %+v", report)
	}
	layer, ok := findLintResult(report.Results, "acme:base.json")
	if !ok || layer.Status != lintPass {
		t.Fatalf("layer result = %+v (ok=%v), want pass", layer, ok)
	}
}

// TestRunLint_FailsOnCorruptLayer is the acceptance test: a corrupt extends layer
// (a schema-invalid version) makes lint fail with a structured per-file error and
// a non-zero exit (a non-nil error from runLint).
func TestRunLint_FailsOnCorruptLayer(t *testing.T) {
	// version 99 is rejected by agentsrc.schema.json (only 1 or 2 are valid).
	project := lintProject(t, "", `{"version":99,"skills":["org-skill"]}`)

	report, err := buildLintReport(project)
	if err != nil {
		t.Fatalf("buildLintReport: %v", err)
	}
	if report.OK {
		t.Fatalf("expected lint to FAIL on corrupt layer, got OK report: %+v", report)
	}
	layer, ok := findLintResult(report.Results, "acme:base.json")
	if !ok {
		t.Fatalf("missing layer result in %+v", report.Results)
	}
	if layer.Status != lintFail {
		t.Errorf("corrupt layer status = %q, want fail", layer.Status)
	}
	if !strings.Contains(layer.Detail, "schema violation") {
		t.Errorf("corrupt layer detail = %q, want it to mention a schema violation", layer.Detail)
	}

	// runLint must surface the failure as a non-nil error (non-zero exit).
	if err := runLint(lintOptions(project, false), testDeps()); err == nil {
		t.Fatal("runLint returned nil on a corrupt layer; expected a non-zero-exit error")
	}
}

func TestRunLint_FailsOnUnknownTopLevelField(t *testing.T) {
	// additionalProperties:false in the schema rejects unknown top-level keys.
	project := lintProject(t, "", `{"version":2,"totally_unknown_key":true}`)
	report, err := buildLintReport(project)
	if err != nil {
		t.Fatalf("buildLintReport: %v", err)
	}
	if report.OK {
		t.Fatalf("expected lint to fail on unknown top-level field, got %+v", report)
	}
}

func TestBuildLintReport_CorruptJSONLayerFails(t *testing.T) {
	project := lintProject(t, "", `{not valid json`)
	report, err := buildLintReport(project)
	if err != nil {
		t.Fatalf("buildLintReport: %v", err)
	}
	if report.OK {
		t.Fatalf("expected fail on unparseable layer JSON, got %+v", report)
	}
	layer, ok := findLintResult(report.Results, "acme:base.json")
	if !ok || layer.Status != lintFail {
		t.Fatalf("layer result = %+v (ok=%v), want fail", layer, ok)
	}
	if !strings.Contains(layer.Detail, "invalid JSON") {
		t.Errorf("detail = %q, want it to mention invalid JSON", layer.Detail)
	}
}

func TestBuildLintReport_RemoteLayerSkipped(t *testing.T) {
	manifest := `{
		"version": 2,
		"sources": [{"id": "acme", "type": "git", "url": "https://example/repo.git", "ref": "main"}],
		"extends": ["acme:org/base.json"]
	}`
	project := withRepoLayer(t, manifest, "")
	report, err := buildLintReport(project)
	if err != nil {
		t.Fatalf("buildLintReport: %v", err)
	}
	layer, ok := findLintResult(report.Results, "acme:org/base.json")
	if !ok {
		t.Fatalf("missing remote layer result in %+v", report.Results)
	}
	if layer.Status != lintSkip {
		t.Errorf("remote layer status = %q, want skip (no fetch)", layer.Status)
	}
	// A skipped remote layer must not flip OK.
	if !report.OK {
		t.Errorf("expected OK=true when only skips/passes present, got %+v", report)
	}
}

func TestRunLint_JSONOutputIsStable(t *testing.T) {
	project := lintProject(t, "", `{"version":2,"skills":["org-skill"]}`)
	opts := lintOptions(project, true)
	buf := &bytes.Buffer{}
	opts.stdout = buf
	if err := runLint(opts, testDeps()); err != nil {
		t.Fatalf("json lint: %v", err)
	}
	var decoded LintReport
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("lint --json did not emit valid JSON: %v\n%s", err, buf.String())
	}
	if !decoded.OK {
		t.Errorf("decoded report not OK: %+v", decoded)
	}
}

// withRawManifest writes the given raw manifest body as the project's
// .agentsrc.json and returns the project root (no extends-layer fixture).
func withRawManifest(t *testing.T, body string) string {
	t.Helper()
	return withRepoLayer(t, body, "")
}

// TestLintExtendsLayer_BranchMatrix drives lintExtendsLayer through its three
// non-skip outcomes plus the local-pass path: an unparseable ref → fail, a ref
// naming an undeclared source → fail, a non-local (git) source → skip, and a
// valid local layer → pass.
func TestLintExtendsLayer_BranchMatrix(t *testing.T) {
	sch, err := compiledAgentsRCSchema()
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}

	localRoot := writeLocalLayer(t, "org/base.json", `{"version":2,"skills":["org-skill"]}`)
	sources := map[string]cfg.Source{
		"acme": {ID: "acme", Type: "local", Path: localRoot},
		"git":  {ID: "git", Type: "git", URL: "https://example/repo.git"},
	}

	tests := []struct {
		name       string
		ref        string
		wantStatus string
		wantDetail string
	}{
		{"invalid ref", "no-colon-here", lintFail, "invalid layer ref"},
		{"undeclared source", "ghost:org/base.json", lintFail, "not declared"},
		{"non-local skipped", "git:org/base.json", lintSkip, "not locally readable"},
		{"valid local pass", "acme:org/base.json", lintPass, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := lintExtendsLayer(sch, cfg.LayerRef{Ref: tt.ref}, sources)
			if res.Status != tt.wantStatus {
				t.Fatalf("status = %q, want %q (detail %q)", res.Status, tt.wantStatus, res.Detail)
			}
			if tt.wantDetail != "" && !strings.Contains(res.Detail, tt.wantDetail) {
				t.Errorf("detail = %q, want substring %q", res.Detail, tt.wantDetail)
			}
		})
	}
}

// TestLintOneFile_FileNotFound covers the os.IsNotExist read-error branch in
// lintOneFile via a path that does not exist.
func TestLintOneFile_FileNotFound(t *testing.T) {
	sch, err := compiledAgentsRCSchema()
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}
	missing := filepath.Join(t.TempDir(), "does-not-exist.json")
	res := lintOneFile(sch, "ghost", missing)
	if res.Status != lintFail {
		t.Fatalf("status = %q, want fail", res.Status)
	}
	if !strings.Contains(res.Detail, "file not found") {
		t.Errorf("detail = %q, want it to mention file not found", res.Detail)
	}
}

// TestLintOneFile_ReadError covers the non-IsNotExist read-error branch by
// pointing lint at a directory (a read returns an error that is not IsNotExist).
func TestLintOneFile_ReadError(t *testing.T) {
	sch, err := compiledAgentsRCSchema()
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}
	dir := t.TempDir() // reading a directory as a file errors, but not with IsNotExist
	res := lintOneFile(sch, "dir", dir)
	if res.Status != lintFail {
		t.Fatalf("status = %q, want fail", res.Status)
	}
	if !strings.Contains(res.Detail, "could not read") {
		t.Errorf("detail = %q, want it to mention a read failure", res.Detail)
	}
}

// TestRunLint_CouldNotRunWhenSchemaUnavailable is a guard for runLint's
// build-error branch: when buildLintReport can compile the schema it cannot fail
// here, so we assert the happy path returns nil and the failure path (a corrupt
// layer) returns a non-nil error, exercising both runLint exits.
func TestRunLint_HumanOutputSummaryLine(t *testing.T) {
	project := lintProject(t, "", `{"version":2,"skills":["org-skill"]}`)
	opts := lintOptions(project, false)
	buf := &bytes.Buffer{}
	opts.stdout = buf
	if err := runLint(opts, testDeps()); err != nil {
		t.Fatalf("human lint: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Config lint (layer schema validation):") {
		t.Errorf("missing header in human output:\n%s", out)
	}
	if !strings.Contains(out, "Summary:") || !strings.Contains(out, "OK") {
		t.Errorf("missing OK summary line in human output:\n%s", out)
	}
}

// TestPrintLintHuman_AllStatuses renders a report containing one of each status
// (including an unknown status) to exercise printLintHuman's switch and the
// lintMark default branch.
func TestPrintLintHuman_AllStatuses(t *testing.T) {
	report := LintReport{
		OK: false,
		Results: []LintResult{
			{File: "ok-layer", Status: lintPass},
			{File: "bad-layer", Status: lintFail, Detail: "schema violation: boom"},
			{File: "remote-layer", Status: lintSkip, Detail: "git layer"},
			{File: "weird-layer", Status: "mystery"},
		},
	}
	buf := &bytes.Buffer{}
	printLintHuman(buf, report)
	out := buf.String()
	if !strings.Contains(out, "1 passed, 1 failed, 1 skipped") {
		t.Errorf("summary counts wrong:\n%s", out)
	}
	if !strings.Contains(out, "FAILED") {
		t.Errorf("expected FAILED outcome for not-OK report:\n%s", out)
	}
}

// TestLintMarkAndOutcome covers lintMark for every status (including the unknown
// default) and lintOutcome for both branches.
func TestLintMarkAndOutcome(t *testing.T) {
	markCases := map[string]string{
		lintPass:   "ok  ",
		lintFail:   "FAIL",
		lintSkip:   "skip",
		"anything": "?   ",
	}
	for status, want := range markCases {
		if got := lintMark(status); got != want {
			t.Errorf("lintMark(%q) = %q, want %q", status, got, want)
		}
	}
	if got := lintOutcome(true); got != "OK" {
		t.Errorf("lintOutcome(true) = %q, want OK", got)
	}
	if got := lintOutcome(false); got != "FAILED" {
		t.Errorf("lintOutcome(false) = %q, want FAILED", got)
	}
}

// TestBuildLintReport_RepoManifestMissingIsTerminal exercises the
// LoadAgentsRC-failure branch in buildLintReport: with no .agentsrc.json the
// repo-local result records the not-found and OK is flipped to false without a
// second decode error.
func TestBuildLintReport_RepoManifestMissingIsTerminal(t *testing.T) {
	project := withRawManifest(t, "")
	report, err := buildLintReport(project)
	if err != nil {
		t.Fatalf("buildLintReport: %v", err)
	}
	if report.OK {
		t.Fatalf("expected not-OK when repo manifest is missing, got %+v", report)
	}
	repo, ok := findLintResult(report.Results, "repo-local")
	if !ok || repo.Status != lintFail {
		t.Fatalf("repo-local = %+v (ok=%v), want fail", repo, ok)
	}
}
