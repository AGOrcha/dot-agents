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

func TestDecodeAppTypeVerifierMap(t *testing.T) {
	// Nil / wrong-typed / empty inputs all collapse to an empty map, no error.
	for _, v := range []any{nil, "not-an-object", map[string]any{}, []any{"x"}} {
		got, err := decodeAppTypeVerifierMap(v)
		if err != nil {
			t.Fatalf("decode(%#v) err = %v", v, err)
		}
		if len(got) != 0 {
			t.Fatalf("decode(%#v) = %#v, want empty", v, got)
		}
	}

	// Well-formed entries preserve order; non-array and non-string members are
	// skipped without aborting the whole map.
	in := map[string]any{
		"go-cli":  []any{"unit", "lint"},
		"bad-seq": "scalar-not-array",
		"mixed":   []any{"unit", 42, "api"},
	}
	got, err := decodeAppTypeVerifierMap(in)
	if err != nil {
		t.Fatalf("decode err = %v", err)
	}
	if strings.Join(got["go-cli"], ",") != "unit,lint" {
		t.Errorf("go-cli = %v, want [unit lint]", got["go-cli"])
	}
	if _, ok := got["bad-seq"]; ok {
		t.Errorf("non-array entry should be skipped: %v", got["bad-seq"])
	}
	if strings.Join(got["mixed"], ",") != "unit,api" {
		t.Errorf("mixed = %v, want [unit api] (non-string dropped)", got["mixed"])
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
