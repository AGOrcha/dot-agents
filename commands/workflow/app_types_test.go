package workflow

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkflowAppTypesJSONSingleRecommended(t *testing.T) {
	repo := setupWorkflowAppTypesProject(t, `{
  "project":"sample-service",
  "version":1,
  "sources":[{"type":"local"}],
  "app_type_verifier_map":{"go-http-service":["unit","api"]}
}`)

	out := captureWorkflowOutput(t, repo, func() error {
		workflowTestJSON = true
		defer func() { workflowTestJSON = false }()
		return executeWorkflowCommand(t, repo, "app-types")
	})

	var parsed workflowAppTypesView
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("parse json output: %v\n%s", err, out)
	}
	if parsed.Project != "sample-service" {
		t.Fatalf("project = %q, want sample-service", parsed.Project)
	}
	if len(parsed.AppTypes) != 1 {
		t.Fatalf("len(app_types) = %d, want 1", len(parsed.AppTypes))
	}
	if parsed.AppTypes[0].Name != "go-http-service" || !parsed.AppTypes[0].Recommended {
		t.Fatalf("unexpected app_types entry: %#v", parsed.AppTypes[0])
	}
}

func TestWorkflowAppTypesTextShowsAliasRecommendation(t *testing.T) {
	repo := setupWorkflowAppTypesProject(t, `{
  "project":"prov-provider-admin-ui",
  "version":1,
  "sources":[{"type":"local"}],
  "app_type_verifier_map":{
    "pa-angular-ui":["pa-ui-unit","pa-ui-lint","pa-ui-e2e"],
    "prov-provider-admin-ui":["pa-ui-unit","pa-ui-lint","pa-ui-e2e"]
  }
}`)

	out := captureWorkflowOutput(t, repo, func() error {
		return executeWorkflowCommand(t, repo, "app-types")
	})

	for _, want := range []string{
		"Workflow App Types",
		"pa-angular-ui",
		"recommended",
		"prov-provider-admin-ui",
		"alias of pa-angular-ui",
		"--app-type pa-angular-ui",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

func TestWorkflowAppTypesFormatFlag(t *testing.T) {
	repo := setupWorkflowAppTypesProject(t, `{
  "project":"sample-service",
  "version":1,
  "sources":[{"type":"local"}],
  "app_type_verifier_map":{"go-http-service":["unit","api"]}
}`)

	out := captureWorkflowOutput(t, repo, func() error {
		return executeWorkflowCommand(t, repo, "app-types", "--format", "flag")
	})
	if got := strings.TrimSpace(out); got != "--app-type go-http-service" {
		t.Fatalf("format output = %q, want %q", got, "--app-type go-http-service")
	}
}

func setupWorkflowAppTypesProject(t *testing.T, agentsrc string) string {
	t.Helper()
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, ".agentsrc.json"), []byte(agentsrc), 0644); err != nil {
		t.Fatal(err)
	}
	return repo
}

func captureWorkflowOutput(t *testing.T, repo string, run func() error) string {
	t.Helper()
	oldwd, _ := os.Getwd()
	defer func() { _ = os.Chdir(oldwd) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()
	if err := run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestWorkflowAppTypesVerboseShowsSourceAndReason(t *testing.T) {
	repo := setupWorkflowAppTypesProject(t, `{
  "project":"prov-provider-admin-ui",
  "version":1,
  "sources":[{"type":"local"}],
  "app_type_verifier_map":{
    "pa-angular-ui":["pa-ui-unit","pa-ui-lint","pa-ui-e2e"],
    "prov-provider-admin-ui":["pa-ui-unit","pa-ui-lint","pa-ui-e2e"]
  }
}`)

	out := captureWorkflowOutput(t, repo, func() error {
		return executeWorkflowCommand(t, repo, "app-types", "--verbose")
	})

	for _, want := range []string{
		"Details",
		"source:",
		"pa-angular-ui: non-repo alias preferred for authoring",
		"prov-provider-admin-ui: alias of pa-angular-ui",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

func TestRenderWorkflowAppTypeFormat_AllForms(t *testing.T) {
	view := workflowAppTypesView{
		AppTypes: []workflowAppTypeEntry{
			{Name: "go-cli", VerifierSequence: []string{"unit"}, Recommended: true},
		},
	}
	cases := map[string]string{
		"flag": "--app-type go-cli",
		"task": "app_type: go-cli",
		"plan": "default_app_type: go-cli",
		"doc":  "Use app_type: go-cli in TASKS.yaml for this repo.",
	}
	for format, want := range cases {
		got, err := renderWorkflowAppTypeFormat(view, format)
		if err != nil {
			t.Fatalf("%s: unexpected err %v", format, err)
		}
		if got != want {
			t.Errorf("%s: got %q want %q", format, got, want)
		}
	}

	if _, err := renderWorkflowAppTypeFormat(view, "bogus"); err == nil {
		t.Error("unknown format should error")
	}

	multi := workflowAppTypesView{AppTypes: []workflowAppTypeEntry{
		{Name: "a", Recommended: true}, {Name: "b", Recommended: true},
	}}
	if _, err := renderWorkflowAppTypeFormat(multi, "flag"); err == nil {
		t.Error("multiple recommended should error for --format")
	}
}

func TestSingleRecommendedAppType_NoneAndMulti(t *testing.T) {
	if _, ok := singleRecommendedAppType(nil); ok {
		t.Error("nil → no recommendation")
	}
	if _, ok := singleRecommendedAppType([]workflowAppTypeEntry{{Name: "x"}}); ok {
		t.Error("none recommended → false")
	}
	if _, ok := singleRecommendedAppType([]workflowAppTypeEntry{
		{Name: "a", Recommended: true}, {Name: "b", Recommended: true},
	}); ok {
		t.Error("two recommended → false")
	}
	got, ok := singleRecommendedAppType([]workflowAppTypeEntry{
		{Name: "only", Recommended: true},
	})
	if !ok || got.Name != "only" {
		t.Errorf("single recommended → %q,%v", got.Name, ok)
	}
}

func TestMarkRecommendedAppTypes_MultipleNonProjectNoAlias(t *testing.T) {

	entries := []workflowAppTypeEntry{
		{Name: "alpha", VerifierSequence: []string{"u", "l"}},
		{Name: "beta", VerifierSequence: []string{"u", "l"}},
		{Name: "proj", VerifierSequence: []string{"u", "l"}},
	}
	markRecommendedAppTypes(entries, "proj")
	for _, e := range entries {
		if e.Recommended || e.AliasOf != "" {
			t.Errorf("ambiguous group must not mark recommended/alias: %#v", e)
		}
	}
}

func TestRunWorkflowAppTypes_FormatJSONConflict(t *testing.T) {
	repo := setupWorkflowAppTypesProject(t, `{
  "project":"svc","version":1,"sources":[{"type":"local"}],
  "app_type_verifier_map":{"go-http-service":["unit"]}
}`)
	out := captureWorkflowOutput(t, repo, func() error {
		workflowTestJSON = true
		defer func() { workflowTestJSON = false }()
		err := executeWorkflowCommand(t, repo, "app-types", "--format", "flag")
		if err == nil {
			t.Error("--format with --json must error")
		}
		return nil
	})
	_ = out
}

func TestRunWorkflowAppTypes_EmptyAndDocFormat(t *testing.T) {

	empty := setupWorkflowAppTypesProject(t, `{
  "project":"svc","version":1,"sources":[{"type":"local"}]
}`)
	out := captureWorkflowOutput(t, empty, func() error {
		return executeWorkflowCommand(t, empty, "app-types")
	})
	if !strings.Contains(out, "No app_types found") {
		t.Fatalf("expected empty notice, got:\n%s", out)
	}

	repo := setupWorkflowAppTypesProject(t, `{
  "project":"svc","version":1,"sources":[{"type":"local"}],
  "app_type_verifier_map":{"go-http-service":["unit","api"]}
}`)
	out = captureWorkflowOutput(t, repo, func() error {
		return executeWorkflowCommand(t, repo, "app-types", "--format", "doc")
	})
	if !strings.Contains(out, "Use app_type: go-http-service in TASKS.yaml") {
		t.Fatalf("doc format output unexpected:\n%s", out)
	}
}
