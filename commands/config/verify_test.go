package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// okProbe / failProbe are injectable code-review-graph readiness stubs so the
// binary check is deterministic regardless of what is installed on the host.
func okProbe(string) error   { return nil }
func failProbe(string) error { return os.ErrNotExist }

func mustVerifyOptions(project string, json bool, probe func(string) error) *runVerifyOptions {
	return &runVerifyOptions{
		jsonOut:  json,
		stdout:   &bytes.Buffer{},
		stderr:   &bytes.Buffer{},
		cwd:      project,
		crgProbe: probe,
	}
}

func findCheck(checks []VerifyCheck, name string) (VerifyCheck, bool) {
	for _, c := range checks {
		if c.Name == name {
			return c, true
		}
	}
	return VerifyCheck{}, false
}

// ---------- buildVerifyReport ----------

func TestBuildVerifyReport_ManifestMissingShortCircuits(t *testing.T) {
	project := withRepoLayer(t, "", "")
	report := buildVerifyReport(mustVerifyOptions(project, false, okProbe))
	if report.OK {
		t.Fatalf("expected OK=false for missing manifest")
	}
	if len(report.Checks) != 1 || report.Checks[0].Name != "manifest" || report.Checks[0].Status != verifyFail {
		t.Fatalf("expected single failed manifest check, got %+v", report.Checks)
	}
}

func TestBuildVerifyReport_ManifestUnparseable(t *testing.T) {
	project := withRepoLayer(t, "{not-json", "")
	report := buildVerifyReport(mustVerifyOptions(project, false, okProbe))
	if report.OK {
		t.Fatalf("expected OK=false for unparseable manifest")
	}
	if c := report.Checks[0]; c.Status != verifyFail {
		t.Fatalf("manifest check should fail, got %+v", c)
	}
}

func TestBuildVerifyReport_CleanManifest_ProbeBranches(t *testing.T) {
	project := withRepoLayer(t, `{"sources":[{"type":"local"}]}`, "")

	rOK := buildVerifyReport(mustVerifyOptions(project, false, okProbe))
	if !rOK.OK {
		t.Fatalf("expected OK=true, got %+v", rOK)
	}
	if c, _ := findCheck(rOK.Checks, "binary:code-review-graph"); c.Status != verifyPass {
		t.Fatalf("probe ok should pass, got %+v", c)
	}
	if c, _ := findCheck(rOK.Checks, "manifest"); c.Status != verifyPass {
		t.Fatalf("manifest should pass, got %+v", c)
	}

	rWarn := buildVerifyReport(mustVerifyOptions(project, false, failProbe))
	if !rWarn.OK {
		t.Fatalf("a binary warning must not flip OK, got %+v", rWarn)
	}
	if c, _ := findCheck(rWarn.Checks, "binary:code-review-graph"); c.Status != verifyWarn {
		t.Fatalf("probe failure should warn, got %+v", c)
	}
}

func TestBuildVerifyReport_LocalPathMissingFails(t *testing.T) {
	project := withRepoLayer(t, `{"sources":[{"type":"local","path":"no/such/layer"}]}`, "")
	report := buildVerifyReport(mustVerifyOptions(project, false, okProbe))
	if report.OK {
		t.Fatalf("expected OK=false when a declared local layer path is missing")
	}
}

// ---------- verifySources ----------

func TestVerifySources(t *testing.T) {
	cwd := t.TempDir()
	// A present relative path and a present absolute path.
	if err := os.MkdirAll(filepath.Join(cwd, "present"), 0o755); err != nil {
		t.Fatal(err)
	}
	absPresent := filepath.Join(cwd, "abs-present")
	if err := os.WriteFile(absPresent, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name       string
		sources    any
		wantName   string
		wantStatus string
	}{
		{"no sources key", nil, "config-layers", verifyPass},
		{"empty list", []any{}, "config-layers", verifyPass},
		{"local no path", []any{map[string]any{"type": "local"}}, "source[0](local)", verifyPass},
		{"local rel present", []any{map[string]any{"type": "local", "path": "present"}}, "source[0](local)", verifyPass},
		{"local abs present", []any{map[string]any{"type": "local", "path": absPresent}}, "source[0](local)", verifyPass},
		{"local missing", []any{map[string]any{"type": "local", "path": "nope"}}, "source[0](local)", verifyFail},
		{"git remote", []any{map[string]any{"type": "git", "url": "u"}}, "source[0](git)", verifyWarn},
		{"http remote", []any{map[string]any{"type": "http", "url": "u"}}, "source[0](http)", verifyWarn},
		{"oci remote", []any{map[string]any{"type": "oci", "url": "u"}}, "source[0](oci)", verifyWarn},
		{"missing type", []any{map[string]any{"url": "u"}}, "source[0](?)", verifyFail},
		{"unknown type", []any{map[string]any{"type": "ftp"}}, "source[0](ftp)", verifyWarn},
		{"not an object", []any{"oops"}, "source[0]", verifyFail},
		{"id label", []any{map[string]any{"type": "git", "id": "acme", "url": "u"}}, "source:acme", verifyWarn},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := map[string]any{}
			if tc.sources != nil {
				repo["sources"] = tc.sources
			}
			snap := &snapshot{layers: map[string]map[string]any{layerRepoLocal: repo}}
			checks := verifySources(cwd, snap)
			c, ok := findCheck(checks, tc.wantName)
			if !ok {
				t.Fatalf("missing check %q in %+v", tc.wantName, checks)
			}
			if c.Status != tc.wantStatus {
				t.Fatalf("check %q status = %q, want %q (%+v)", tc.wantName, c.Status, tc.wantStatus, c)
			}
		})
	}
}

// ---------- sourceLabel ----------

func TestSourceLabel(t *testing.T) {
	cases := []struct {
		name string
		i    int
		src  map[string]any
		want string
	}{
		{"id wins", 2, map[string]any{"id": "acme", "type": "git"}, "source:acme"},
		{"type fallback", 1, map[string]any{"type": "git"}, "source[1](git)"},
		{"empty id falls through", 0, map[string]any{"id": "", "type": "local"}, "source[0](local)"},
		{"no type", 3, map[string]any{}, "source[3](?)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sourceLabel(tc.i, tc.src); got != tc.want {
				t.Fatalf("sourceLabel = %q, want %q", got, tc.want)
			}
		})
	}
}

// ---------- runVerify ----------

func TestRunVerify_HumanOK(t *testing.T) {
	project := withRepoLayer(t, `{"sources":[{"type":"local"}]}`, "")
	opts := mustVerifyOptions(project, false, okProbe)
	if err := runVerify(opts, testDeps()); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	out := opts.stdout.(*bytes.Buffer).String()
	if !strings.Contains(out, "Summary:") || !strings.Contains(out, "OK") {
		t.Fatalf("human output missing summary/OK: %s", out)
	}
}

func TestRunVerify_JSONOK(t *testing.T) {
	project := withRepoLayer(t, `{"sources":[{"type":"local"}]}`, "")
	opts := mustVerifyOptions(project, true, okProbe)
	if err := runVerify(opts, testDeps()); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	var report VerifyReport
	if err := json.Unmarshal(opts.stdout.(*bytes.Buffer).Bytes(), &report); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if !report.OK {
		t.Fatalf("expected OK=true, got %+v", report)
	}
}

func TestRunVerify_FailReturnsError(t *testing.T) {
	project := withRepoLayer(t, "", "") // missing manifest
	opts := mustVerifyOptions(project, false, okProbe)
	err := runVerify(opts, testDeps())
	if err == nil {
		t.Fatalf("expected error for failing verify")
	}
	he, ok := err.(*hintError)
	if !ok || !strings.Contains(he.message, "failed checks") {
		t.Fatalf("expected hintError about failed checks, got %v", err)
	}
}

func TestRunVerify_FailJSON(t *testing.T) {
	project := withRepoLayer(t, "", "")
	opts := mustVerifyOptions(project, true, okProbe)
	if err := runVerify(opts, testDeps()); err == nil {
		t.Fatalf("expected error for failing verify")
	}
	var report VerifyReport
	if err := json.Unmarshal(opts.stdout.(*bytes.Buffer).Bytes(), &report); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if report.OK {
		t.Fatalf("expected OK=false, got %+v", report)
	}
}

// ---------- command wiring + small pure helpers ----------

func TestNewVerifyCmd_Execute(t *testing.T) {
	project := withRepoLayer(t, `{"sources":[{"type":"local"}]}`, "")
	prev, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(prev) })
	if err := os.Chdir(project); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	cmd := newVerifyCmd(testDeps())
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out.String(), "Config verify") {
		t.Fatalf("expected verify output, got %s", out.String())
	}
}

func TestDefaultCRGProbe_DoesNotPanic(t *testing.T) {
	// Result depends on host PATH/.venv; we only exercise the line for coverage
	// and require it to return (nil or error) without panicking.
	_ = defaultCRGProbe(t.TempDir())
}

func TestVerifyMarkAndOutcome(t *testing.T) {
	for status, want := range map[string]string{
		verifyPass: "ok ",
		verifyWarn: "warn",
		verifyFail: "FAIL",
		"bogus":    "?  ",
	} {
		if got := verifyMark(status); got != want {
			t.Fatalf("verifyMark(%q) = %q, want %q", status, got, want)
		}
	}
	if verifyOutcome(true) != "OK" || verifyOutcome(false) != "FAILED" {
		t.Fatalf("verifyOutcome mismatch")
	}
}
