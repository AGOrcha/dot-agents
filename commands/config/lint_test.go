package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

// TestEmbeddedSchemaMatchesCanonical guards against drift between the schema
// copy embedded into this package (for go:embed) and the canonical
// schemas/agentsrc.schema.json. If the canonical schema changes, this fails
// loudly so the embedded copy is refreshed rather than silently diverging.
func TestEmbeddedSchemaMatchesCanonical(t *testing.T) {
	root := lintRepoRoot(t)
	canonical, err := os.ReadFile(filepath.Join(root, "schemas", "agentsrc.schema.json"))
	if err != nil {
		t.Fatalf("read canonical schema: %v", err)
	}
	if !bytes.Equal(canonical, agentsRCSchemaJSON) {
		t.Fatalf("embedded commands/config/agentsrc.schema.json has drifted from schemas/agentsrc.schema.json; re-copy the canonical file")
	}
}

// lintRepoRoot walks up from the test's working dir to the repo root (the dir
// containing schemas/agentsrc.schema.json).
func lintRepoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := wd
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "schemas", "agentsrc.schema.json")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatalf("could not locate repo root from %s", wd)
	return ""
}
