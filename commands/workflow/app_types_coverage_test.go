package workflow

import (
	"strings"
	"testing"
)

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

	// No single recommended → error.
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
	// Three entries share a sequence; two are non-project → ambiguous,
	// so none is marked recommended/alias (nonProject == -2 branch).
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
	// No app_type_verifier_map → empty path ("No app_types found").
	empty := setupWorkflowAppTypesProject(t, `{
  "project":"svc","version":1,"sources":[{"type":"local"}]
}`)
	out := captureWorkflowOutput(t, empty, func() error {
		return executeWorkflowCommand(t, empty, "app-types")
	})
	if !strings.Contains(out, "No app_types found") {
		t.Fatalf("expected empty notice, got:\n%s", out)
	}

	// --format doc with a single recommended app_type.
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
