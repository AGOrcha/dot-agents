package workflow

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/config"
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
  "project":"acme-admin-ui",
  "version":1,
  "sources":[{"type":"local"}],
  "app_type_verifier_map":{
    "acme-angular-ui":["acme-ui-unit","acme-ui-lint","acme-ui-e2e"],
    "acme-admin-ui":["acme-ui-unit","acme-ui-lint","acme-ui-e2e"]
  }
}`)

	out := captureWorkflowOutput(t, repo, func() error {
		return executeWorkflowCommand(t, repo, "app-types")
	})

	for _, want := range []string{
		"Workflow App Types",
		"acme-angular-ui",
		"recommended",
		"acme-admin-ui",
		"alias of acme-angular-ui",
		"--app-type acme-angular-ui",
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
	// Isolate the user-local config layer the snapshot resolver merges in, so a
	// stray developer ~/.agents/.agentsrc.json cannot leak app_type_verifier_map
	// entries into these table cases.
	t.Setenv("AGENTS_HOME", t.TempDir())
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

// captureWorkflowStdoutStderr runs fn with both os.Stdout and os.Stderr piped,
// returning (stdout, stderr). Used to assert the incomplete-resolution note lands
// on stderr without polluting the machine-readable stdout stream.
func captureWorkflowStdoutStderr(t *testing.T, repo string, run func() error) (string, string) {
	t.Helper()
	oldwd, _ := os.Getwd()
	defer func() { _ = os.Chdir(oldwd) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	outR, outW := mustPipe(t)
	errR, errW := mustPipe(t)
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW
	defer func() { os.Stdout, os.Stderr = oldOut, oldErr }()
	runErr := run()
	_ = outW.Close()
	_ = errW.Close()
	out := mustReadAll(t, outR)
	errOut := mustReadAll(t, errR)
	if runErr != nil {
		t.Fatalf("run: %v", runErr)
	}
	return out, errOut
}

func mustPipe(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	return r, w
}

func mustReadAll(t *testing.T, r *os.File) string {
	t.Helper()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Close()
	return string(b)
}

func TestWorkflowAppTypesVerboseShowsSourceAndReason(t *testing.T) {
	repo := setupWorkflowAppTypesProject(t, `{
  "project":"acme-admin-ui",
  "version":1,
  "sources":[{"type":"local"}],
  "app_type_verifier_map":{
    "acme-angular-ui":["acme-ui-unit","acme-ui-lint","acme-ui-e2e"],
    "acme-admin-ui":["acme-ui-unit","acme-ui-lint","acme-ui-e2e"]
  }
}`)

	out := captureWorkflowOutput(t, repo, func() error {
		return executeWorkflowCommand(t, repo, "app-types", "--verbose")
	})

	for _, want := range []string{
		"Details",
		"source:",
		"acme-angular-ui: non-repo alias preferred for authoring",
		"acme-admin-ui: alias of acme-angular-ui",
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

// TestWorkflowAppTypesMergesUserLocalLayer proves the snapshot refactor reads
// the *effective* (layered) config: a user-local app_type_verifier_map entry
// must surface alongside the repo-local one, which the pre-refactor raw
// repo-only read could never do.
func TestWorkflowAppTypesMergesUserLocalLayer(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, ".agentsrc.json"), []byte(`{
  "project":"svc","version":1,"sources":[{"type":"local"}],
  "app_type_verifier_map":{"go-cli":["unit"]}
}`), 0644); err != nil {
		t.Fatal(err)
	}
	userHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(userHome, ".agentsrc.json"), []byte(`{
  "app_type_verifier_map":{"my-local-type":["unit","lint"]}
}`), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", userHome)

	out := captureWorkflowOutput(t, repo, func() error {
		workflowTestJSON = true
		defer func() { workflowTestJSON = false }()
		return executeWorkflowCommand(t, repo, "app-types")
	})

	var parsed workflowAppTypesView
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("parse json output: %v\n%s", err, out)
	}
	names := map[string][]string{}
	for _, e := range parsed.AppTypes {
		names[e.Name] = e.VerifierSequence
	}
	if _, ok := names["go-cli"]; !ok {
		t.Fatalf("repo-local app_type missing from effective view: %#v", parsed.AppTypes)
	}
	seq, ok := names["my-local-type"]
	if !ok {
		t.Fatalf("user-local layer app_type not merged into effective view: %#v", parsed.AppTypes)
	}
	if strings.Join(seq, ",") != "unit,lint" {
		t.Fatalf("user-local verifier sequence = %v, want [unit lint]", seq)
	}
}

// TestWorkflowAppTypesNoManifestIsEmpty proves the negative path: a project with
// no repo-local .agentsrc.json resolves to an empty view with no error, matching
// the pre-refactor no-file behavior even though the FLAT resolver treats a
// missing manifest as fatal internally.
func TestWorkflowAppTypesNoManifestIsEmpty(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("AGENTS_HOME", t.TempDir())

	out := captureWorkflowOutput(t, repo, func() error {
		return executeWorkflowCommand(t, repo, "app-types")
	})
	if !strings.Contains(out, "No app_types found") {
		t.Fatalf("missing-manifest project should print empty notice, got:\n%s", out)
	}
}

// TestWorkflowAppTypesRealLockedExtendsLayer is the end-to-end happy path through
// the REAL config.NewLayeredResolver().ResolveLocked (not the stub seam): a project
// that `extends` a layer carrying app_type_verifier_map entries, with a populated
// .agentsrc.lock + on-disk layer cache, must surface that imported layer's app-types
// through `da workflow app-types`. This proves imported-layer ExtraFields flow
// through EffectiveRaw into decodeAppTypeVerifierMap end-to-end — the wiring the
// stub-seam tests cannot exercise.
func TestWorkflowAppTypesRealLockedExtendsLayer(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())

	// A local source dir is a pure-disk extends source (no network): the online
	// Resolve below reads it straight off disk while populating the cache + lock,
	// after which ResolveLocked replays it offline.
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "org"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "org", "base.json"), []byte(`{
  "app_type_verifier_map":{"org-shared-svc":["org-unit","org-api"]}
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, ".agentsrc.json"), []byte(`{
  "project":"svc","version":2,
  "sources":[{"id":"acme","type":"local","path":`+strconv.Quote(src)+`}],
  "extends":["acme:org/base.json"],
  "app_type_verifier_map":{"repo-local-svc":["unit"]}
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Populate .agentsrc.lock + the layer cache via one online (local-disk) resolve.
	if _, err := config.NewLayeredResolver().Resolve(repo); err != nil {
		t.Fatalf("seed online resolve: %v", err)
	}

	// Now drive the command through the REAL default appTypeSnapshot (ResolveLocked).
	out := captureWorkflowOutput(t, repo, func() error {
		workflowTestJSON = true
		defer func() { workflowTestJSON = false }()
		return executeWorkflowCommand(t, repo, "app-types")
	})

	var parsed workflowAppTypesView
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("parse json output: %v\n%s", err, out)
	}
	got := map[string][]string{}
	for _, e := range parsed.AppTypes {
		got[e.Name] = e.VerifierSequence
	}
	if _, ok := got["repo-local-svc"]; !ok {
		t.Fatalf("repo-local app_type missing: %#v", parsed.AppTypes)
	}
	seq, ok := got["org-shared-svc"]
	if !ok {
		t.Fatalf("imported (locked extends) app_type missing from effective view: %#v", parsed.AppTypes)
	}
	if strings.Join(seq, ",") != "org-unit,org-api" {
		t.Fatalf("imported verifier sequence = %v, want [org-unit org-api]", seq)
	}
	if len(parsed.Incomplete) != 0 {
		t.Fatalf("fully-resolved project must report no incomplete notes, got %v", parsed.Incomplete)
	}
}

// TestAppTypeSnapshotConsumesLockedPath proves the production seam consumes the
// read-only, units-lock-backed resolution path (LayeredResolver.ResolveLocked),
// not an online fetch. A project that declares `extends` but has no lockfile must
// surface the offline lock gap as an error — a fetcher would instead try to pull
// the layer. The error is NOT a missing-manifest condition, so it propagates
// rather than collapsing to an empty "No app_types found" view.
func TestAppTypeSnapshotConsumesLockedPath(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, ".agentsrc.json"), []byte(`{
  "project":"svc","version":1,
  "sources":[{"id":"org","type":"local","path":"/nonexistent"}],
  "extends":[{"ref":"org:base.json@v1"}]
}`), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", t.TempDir())

	_, _, err := resolveEffectiveAppTypeMap(repo)
	if err == nil {
		t.Fatal("extends project with no lockfile must surface the offline lock gap, not fetch")
	}
	if isMissingManifestErr(err) {
		t.Fatalf("lock-gap error must propagate, not be swallowed as missing-manifest: %v", err)
	}
}

func TestAppTypeVerifierSequencesFromExecution(t *testing.T) {
	// Nil profile and empty by_app_type both collapse to nil.
	if got := appTypeVerifierSequencesFromExecution(nil); got != nil {
		t.Fatalf("nil profile = %#v, want nil", got)
	}
	if got := appTypeVerifierSequencesFromExecution(&config.ExecutionProfile{}); got != nil {
		t.Fatalf("empty by_app_type = %#v, want nil", got)
	}

	// Each app_type's topology.verifier_sequence is projected in declared order;
	// an app_type with an empty sequence is skipped (not included as an empty key).
	ep := &config.ExecutionProfile{
		ByAppType: map[string]config.AppTypeProfile{
			"go-cli":   {Topology: config.Topology{VerifierSequence: []string{"unit", "cli-runner"}}},
			"ideation": {Topology: config.Topology{VerifierSequence: []string{"schema-check"}}},
			"empty":    {Topology: config.Topology{}},
		},
	}
	got := appTypeVerifierSequencesFromExecution(ep)
	if strings.Join(got["go-cli"], ",") != "unit,cli-runner" {
		t.Errorf("go-cli = %v, want [unit cli-runner]", got["go-cli"])
	}
	if strings.Join(got["ideation"], ",") != "schema-check" {
		t.Errorf("ideation = %v, want [schema-check]", got["ideation"])
	}
	if _, ok := got["empty"]; ok {
		t.Errorf("app_type with empty verifier_sequence should be skipped: %v", got["empty"])
	}
}

// appTypesWithModelManifest is a two-app_type manifest where only one app_type
// pins execution_profile facet 5, so a test can assert both the carried value and
// the absent case from one fixture.
const appTypesWithModelManifest = `{
  "project":"svc","version":1,"sources":[{"type":"local"}],
  "execution_profile":{"by_app_type":{
    "go-cli":{"topology":{"verifier_sequence":["unit"]},"model":"anthropic:claude-opus-4-8"},
    "docs":{"topology":{"verifier_sequence":["schema-check"]}}
  }}
}`

// appTypeEntryByName returns the rendered entry for an app_type, failing when the
// view dropped it.
func appTypeEntryByName(t *testing.T, view workflowAppTypesView, name string) workflowAppTypeEntry {
	t.Helper()
	for _, e := range view.AppTypes {
		if e.Name == name {
			return e
		}
	}
	t.Fatalf("app_type %q missing from view %+v", name, view.AppTypes)
	return workflowAppTypeEntry{}
}

// TestCollectWorkflowAppTypes_CarriesModelFacet proves the resolved JSON surface
// an orchestrator reads carries the app_type's model route, and omits it for an
// app_type that pins none (absence = inherit, never a fabricated model). See
// .agents/proposals/model-facet-apptypeprofile.md §D4.
func TestCollectWorkflowAppTypes_CarriesModelFacet(t *testing.T) {
	repo := setupWorkflowAppTypesProject(t, appTypesWithModelManifest)

	view, err := collectWorkflowAppTypes(workflowProjectRef{Name: "svc", Path: repo})
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if got := appTypeEntryByName(t, view, "go-cli").Model; got != "anthropic:claude-opus-4-8" {
		t.Errorf("go-cli model = %q, want anthropic:claude-opus-4-8", got)
	}
	if got := appTypeEntryByName(t, view, "docs").Model; got != "" {
		t.Errorf("docs pins no model; got %q, want empty (inherit)", got)
	}
}

// TestRunWorkflowAppTypes_RendersModelSuffix proves the human table surfaces the
// model route inline, and that an app_type without one renders unchanged.
func TestRunWorkflowAppTypes_RendersModelSuffix(t *testing.T) {
	repo := setupWorkflowAppTypesProject(t, appTypesWithModelManifest)
	out := captureWorkflowOutput(t, repo, func() error {
		return executeWorkflowCommand(t, repo, "app-types")
	})
	if !strings.Contains(out, "model=anthropic:claude-opus-4-8") {
		t.Fatalf("expected model suffix for go-cli, got:\n%s", out)
	}
	// Exactly one app_type pins a model, so exactly one row may carry the suffix.
	if n := strings.Count(out, "model="); n != 1 {
		t.Fatalf("model suffix rendered %d times, want 1 (docs pins none):\n%s", n, out)
	}
}

// TestAppTypeProfilesFromExecution_SelectionParity proves the whole-profile
// projection applies the SAME selection rule as the verifier-sequence projection
// (non-empty verifier_sequence), so the two never disagree on the app_type set,
// and that it carries the facets the sequence map drops.
func TestAppTypeProfilesFromExecution_SelectionParity(t *testing.T) {
	if got := appTypeProfilesFromExecution(nil); got != nil {
		t.Fatalf("nil profile = %#v, want nil", got)
	}
	if got := appTypeProfilesFromExecution(&config.ExecutionProfile{}); got != nil {
		t.Fatalf("empty by_app_type = %#v, want nil", got)
	}

	ep := &config.ExecutionProfile{
		ByAppType: map[string]config.AppTypeProfile{
			"go-cli": {Topology: config.Topology{VerifierSequence: []string{"unit"}}, Model: "sonnet"},
			// Pins a model but declares no verifier sequence: excluded by the
			// documented selection rule (proposal §D4 known limitation).
			"model-only": {Model: "haiku"},
		},
	}
	profiles := appTypeProfilesFromExecution(ep)
	if profiles["go-cli"].ModelRef() != "sonnet" {
		t.Errorf("go-cli model = %q, want sonnet", profiles["go-cli"].ModelRef())
	}
	if _, ok := profiles["model-only"]; ok {
		t.Error("app_type with empty verifier_sequence must be excluded")
	}
	if len(profiles) != len(appTypeVerifierSequencesFromExecution(ep)) {
		t.Errorf("selection drift: profiles=%d sequences=%d", len(profiles), len(appTypeVerifierSequencesFromExecution(ep)))
	}
}

func TestIsMissingManifestErr(t *testing.T) {
	if !isMissingManifestErr(fmt.Errorf("no %s found at /tmp/x", config.AgentsRCFile)) {
		t.Error("resolver missing-manifest message should be classified as missing")
	}
	if !isMissingManifestErr(os.ErrNotExist) {
		t.Error("fs.ErrNotExist should be classified as missing")
	}
	if isMissingManifestErr(fmt.Errorf("parsing repo-local: unexpected end of JSON input")) {
		t.Error("a parse error must NOT be swallowed as missing-manifest")
	}
}

func TestResolveEffectiveAppTypeMap_ResolverError(t *testing.T) {
	// A non-missing resolver error must propagate, not be swallowed.
	orig := appTypeSnapshot
	t.Cleanup(func() { appTypeSnapshot = orig })
	appTypeSnapshot = func(string) (*config.Snapshot, error) {
		return nil, fmt.Errorf("boom: locked layer missing from cache")
	}
	if _, _, err := resolveEffectiveAppTypeMap(t.TempDir()); err == nil {
		t.Fatal("expected resolver error to propagate")
	}

	// A missing-manifest resolver error is swallowed to an empty map.
	appTypeSnapshot = func(string) (*config.Snapshot, error) {
		return nil, fmt.Errorf("no %s found at /x", config.AgentsRCFile)
	}
	got, _, err := resolveEffectiveAppTypeMap(t.TempDir())
	if err != nil || len(got) != 0 {
		t.Fatalf("missing-manifest err should yield empty map: got %#v, %v", got, err)
	}
}

// TestIncompleteResolutionNotes proves only shrink-causing warnings (optional skip,
// protected-field drop) become user notes; an informational cache_hit_offline (the
// layer WAS resolved) is excluded so it never falsely flags an incomplete map.
func TestIncompleteResolutionNotes(t *testing.T) {
	notes := incompleteResolutionNotes([]config.ProvenanceWarning{
		{FieldPath: "org:base.json@v1", Outcome: "optional_skipped: not in cache"},
		{FieldPath: "org:trust.json@v2", Outcome: "cache_hit_offline"},
		{FieldPath: "protected.field", Outcome: "dropped"},
	})
	if len(notes) != 2 {
		t.Fatalf("want 2 shrink notes (optional_skipped + dropped), got %d: %v", len(notes), notes)
	}
	joined := strings.Join(notes, "|")
	if !strings.Contains(joined, "org:base.json@v1 (optional_skipped") ||
		!strings.Contains(joined, "protected.field (dropped)") {
		t.Fatalf("notes missing expected entries: %v", notes)
	}
	if strings.Contains(joined, "cache_hit_offline") {
		t.Fatalf("cache_hit_offline must not be reported as incomplete: %v", notes)
	}
	if incompleteResolutionNotes(nil) != nil {
		t.Error("no warnings → nil notes")
	}
}

// TestResolveEffectiveAppTypeMap_SurfacesIncompleteNotes proves a skipped optional
// layer flows out as a note alongside the (possibly shrunk) map, so the caller can
// warn instead of silently printing an incomplete app-types list.
func TestResolveEffectiveAppTypeMap_SurfacesIncompleteNotes(t *testing.T) {
	orig := appTypeSnapshot
	t.Cleanup(func() { appTypeSnapshot = orig })
	appTypeSnapshot = func(string) (*config.Snapshot, error) {
		return &config.Snapshot{
			Warnings: []config.ProvenanceWarning{
				{FieldPath: "org:opt.json@v1", Outcome: "optional_skipped: no resolved SHA"},
			},
		}, nil
	}
	_, notes, err := resolveEffectiveAppTypeMap(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "org:opt.json@v1") {
		t.Fatalf("skipped optional layer must surface as a note, got: %v", notes)
	}
}

// TestRunWorkflowAppTypes_IncompleteWarningToStderr proves the silent-wrong-result
// hole is closed end-to-end: a skipped optional layer produces a "may be
// incomplete" note on STDERR (never on stdout, which must stay machine-clean for
// --format/JSON consumers).
func TestRunWorkflowAppTypes_IncompleteWarningToStderr(t *testing.T) {
	orig := appTypeSnapshot
	t.Cleanup(func() { appTypeSnapshot = orig })
	appTypeSnapshot = func(string) (*config.Snapshot, error) {
		return &config.Snapshot{
			Warnings: []config.ProvenanceWarning{
				{FieldPath: "org:opt.json@v1", Outcome: "optional_skipped: not in cache"},
			},
		}, nil
	}
	repo := setupWorkflowAppTypesProject(t, `{
  "project":"svc","version":1,"sources":[{"type":"local"}]
}`)

	stdout, stderr := captureWorkflowStdoutStderr(t, repo, func() error {
		return executeWorkflowCommand(t, repo, "app-types")
	})
	if !strings.Contains(stderr, "may be incomplete") || !strings.Contains(stderr, "org:opt.json@v1") {
		t.Fatalf("incomplete-resolution warning must go to stderr, got stderr:\n%s", stderr)
	}
	if strings.Contains(stdout, "may be incomplete") {
		t.Fatalf("warning must NOT pollute stdout, got stdout:\n%s", stdout)
	}
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
