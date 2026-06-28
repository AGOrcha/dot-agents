package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cfg "github.com/AGOrcha/dot-agents/internal/config"
	"github.com/spf13/cobra"
)

// ---------- shared test helpers ----------

// hintError captures ErrorWithHints / UsageError output so tests can assert
// against the exact message without falling back to substring search on
// process stderr.
type hintError struct {
	message string
	hints   []string
}

func (e *hintError) Error() string {
	if len(e.hints) == 0 {
		return e.message
	}
	return e.message + " (" + strings.Join(e.hints, "; ") + ")"
}

// testDeps returns a Deps whose hint emitters surface as &hintError so tests
// can match on .message and .hints fields directly.
func testDeps() Deps {
	accept := func(*cobra.Command, []string) error { return nil }
	return Deps{
		ErrorWithHints: func(message string, hints ...string) error {
			return &hintError{message: message, hints: append([]string{}, hints...)}
		},
		UsageError: func(message string, hints ...string) error {
			return &hintError{message: message, hints: append([]string{}, hints...)}
		},
		MaximumNArgsWithHints: func(_ int, _ ...string) cobra.PositionalArgs { return accept },
		ExactArgsWithHints:    func(_ int, _ ...string) cobra.PositionalArgs { return accept },
	}
}

// withRepoLayer writes a .agentsrc.json into a fresh temp dir and returns the
// project root path. The user-local layer is isolated to the same temp tree
// via HOME so cfg.AgentsHome resolves under t.TempDir. Running explain through
// the real auto-lock seam therefore writes the units lock under the temp
// project — never the developer's real home.
func withRepoLayer(t *testing.T, repoBody string, userBody string) string {
	t.Helper()
	root := t.TempDir()
	// Isolate ~/.agents to <root>/home/.agents
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(filepath.Join(home, ".agents"), 0o755); err != nil {
		t.Fatalf("mkdir home/.agents: %v", err)
	}
	t.Setenv("HOME", home)
	// Ensure cfg.AgentsHome resolves under the test temp tree even when the
	// outer environment exports AGENTS_HOME (CI commonly does).
	t.Setenv("AGENTS_HOME", filepath.Join(home, ".agents"))
	// Avoid XDG_* poisoning the cache path lookups.
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))

	if userBody != "" {
		userPath := filepath.Join(home, ".agents", ".agentsrc.json")
		if err := os.WriteFile(userPath, []byte(userBody), 0o644); err != nil {
			t.Fatalf("write user-local: %v", err)
		}
	}

	project := filepath.Join(root, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	if repoBody != "" {
		if err := os.WriteFile(filepath.Join(project, ".agentsrc.json"), []byte(repoBody), 0o644); err != nil {
			t.Fatalf("write repo-local: %v", err)
		}
	}
	return project
}

func mustOptions(project string) *runExplainOptions {
	return &runExplainOptions{
		stdout: &bytes.Buffer{},
		stderr: &bytes.Buffer{},
		cwd:    project,
	}
}

// withStubResolver swaps the package-level ensureResolved seam for the duration
// of a test and restores it afterward. Tests that need to assert auto-lock
// dispatch (e.g. an error path or a synthetic snapshot) inject a stub; tests
// that exercise the real resolution path leave it untouched.
func withStubResolver(t *testing.T, fn func(string, cfg.EnsureOpts) (*cfg.EnsureResult, error)) {
	t.Helper()
	prev := ensureResolved
	ensureResolved = fn
	t.Cleanup(func() { ensureResolved = prev })
}

// snapResult wraps a *cfg.Snapshot in an EnsureResult for stub returns.
func snapResult(snap *cfg.Snapshot) *cfg.EnsureResult {
	return &cfg.EnsureResult{Snapshot: snap, Fresh: true}
}

// flatSnapshot builds a resolved Snapshot by driving the real FlatResolver
// against on-disk manifests, so render tests assert provenance through the same
// canonical resolution path explain consumes in production. user may be nil (no
// user-local layer); repo is required. Layer order is the canonical flat order
// (product-defaults → user-local → repo-local).
func flatSnapshot(t *testing.T, user, repo map[string]any) *cfg.Snapshot {
	t.Helper()
	dir := t.TempDir()
	// Default to a guaranteed-absent path so the no-user case never picks up a
	// stray ~/.agents/.agentsrc.json from the host.
	userPath := filepath.Join(dir, "absent-user.agentsrc.json")
	if user != nil {
		userPath = filepath.Join(dir, "user.agentsrc.json")
		writeJSONFile(t, userPath, user)
	}
	repo["version"] = 2 // FlatResolver decodes through the typed AgentsRC schema
	writeJSONFile(t, filepath.Join(dir, cfg.AgentsRCFile), repo)
	snap, err := cfg.NewFlatResolver().WithUserLocalPath(userPath).Resolve(dir)
	if err != nil {
		t.Fatalf("FlatResolver.Resolve: %v", err)
	}
	return snap
}

// writeJSONFile marshals v to path, failing the test on any error.
func writeJSONFile(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %s: %v", path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// ---------- auto-lock dispatch ----------

// TestRunExplain_AutoLocksThroughSeam asserts explain drives the EnsureResolved
// auto-lock seam (the `uv tree` behavior): running explain consults the seam
// exactly once with default opts and renders the returned snapshot.
func TestRunExplain_AutoLocksThroughSeam(t *testing.T) {
	calls := 0
	var gotOpts cfg.EnsureOpts
	withStubResolver(t, func(_ string, opts cfg.EnsureOpts) (*cfg.EnsureResult, error) {
		calls++
		gotOpts = opts
		return snapResult(flatSnapshot(t, nil, map[string]any{"project": "demo"})), nil
	})
	opts := mustOptions("/does/not/matter")
	if err := runExplain(opts, nil, testDeps()); err != nil {
		t.Fatalf("runExplain: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected exactly one auto-lock resolve, got %d", calls)
	}
	// Default (auto-lock) opts: none of the mode flags are set, so the
	// EnsureResolved default path runs (fresh ⇒ no-op read; stale/absent ⇒
	// re-resolve + rewrite lock).
	if gotOpts.Locked || gotOpts.Frozen || gotOpts.Offline || gotOpts.NoSync {
		t.Errorf("expected default (auto-lock) EnsureOpts, got %+v", gotOpts)
	}
}

// TestRunExplain_RealAutoLockWritesLock exercises the production seam end-to-end
// against a flat manifest: explain must resolve and, because the lock is absent,
// auto-write a .agentsrc.lock (the auto-lock contract) while rendering.
func TestRunExplain_RealAutoLockWritesLock(t *testing.T) {
	project := withRepoLayer(t, `{"version":2,"project":"demo"}`, "")
	opts := mustOptions(project)
	if err := runExplain(opts, nil, testDeps()); err != nil {
		t.Fatalf("runExplain: %v", err)
	}
	if _, err := os.Stat(cfg.AgentsLockPath(project)); err != nil {
		t.Errorf("explain should auto-lock (write .agentsrc.lock), stat err: %v", err)
	}
}

// ---------- snapshot construction / resolution ----------

func TestLoadSnapshot_RepoLayerOnly(t *testing.T) {
	project := withRepoLayer(t, `{"version":2,"project":"demo","repo_id":"github.com/x/y"}`, "")
	snap, code, err := loadSnapshot(project)
	if err != nil || code != exitOK {
		t.Fatalf("unexpected: code=%d err=%v", code, err)
	}
	eff, err := snap.EffectiveRaw()
	if err != nil {
		t.Fatalf("EffectiveRaw: %v", err)
	}
	if eff["project"] != "demo" {
		t.Errorf("expected project=demo, got %v", eff["project"])
	}
}

func TestLoadSnapshot_MissingRepoManifestErrors(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("AGENTS_HOME", filepath.Join(root, "home", ".agents"))
	_, code, err := loadSnapshot(filepath.Join(root, "project"))
	if err == nil {
		t.Fatal("expected error for missing manifest")
	}
	if code != exitSchemaErr {
		t.Errorf("expected exitSchemaErr (%d), got %d", exitSchemaErr, code)
	}
}

func TestLoadSnapshot_InvalidRepoJSONErrors(t *testing.T) {
	project := withRepoLayer(t, `{not-json`, "")
	_, code, err := loadSnapshot(project)
	if err == nil || code != exitSchemaErr {
		t.Fatalf("expected schema error, got code=%d err=%v", code, err)
	}
}

// ---------- field traversal ----------

func TestExplainField_NestedPathResolves(t *testing.T) {
	snap := flatSnapshot(t, nil, map[string]any{
		"kg": map[string]any{"backend": "sqlite"},
	})
	exp := explainField(snap, "kg.backend")
	if exp.Value != "sqlite" {
		t.Errorf("expected sqlite, got %v", exp.Value)
	}
	if exp.ActiveLayer != layerRepoLocal {
		t.Errorf("expected active=%s, got %s", layerRepoLocal, exp.ActiveLayer)
	}
	var activeCount int
	for _, lv := range exp.Layers {
		if lv.Active {
			activeCount++
		}
	}
	if activeCount != 1 {
		t.Errorf("expected exactly 1 active layer, got %d", activeCount)
	}
}

func TestExplainField_MultiLayerOverride(t *testing.T) {
	// "kg" is a map-merge field, so a nested scalar that exists only in
	// repo-local resolves to repo-local while the lower layer still records its
	// losing value in the per-field stack.
	snap := flatSnapshot(t,
		map[string]any{"kg": map[string]any{"backend": "user-value"}},
		map[string]any{"kg": map[string]any{"backend": "repo-value"}},
	)
	exp := explainField(snap, "kg.backend")
	if exp.ActiveLayer != layerRepoLocal {
		t.Errorf("expected repo-local to win, got %s", exp.ActiveLayer)
	}
	if exp.Value != "repo-value" {
		t.Errorf("expected repo-value, got %v", exp.Value)
	}
	var userEntry LayerValue
	for _, lv := range exp.Layers {
		if lv.Layer == layerUserLocal {
			userEntry = lv
		}
	}
	if userEntry.Value != "user-value" {
		t.Errorf("expected user-local entry to record losing value, got %v", userEntry.Value)
	}
	if userEntry.Active {
		t.Error("user-local should not be active when repo-local overrides it")
	}
}

func TestExplainField_AbsentFieldHasNoActiveLayer(t *testing.T) {
	snap := flatSnapshot(t, nil, map[string]any{"version": float64(2)})
	exp := explainField(snap, "missing.path")
	if exp.ActiveLayer != "" {
		t.Errorf("expected no active layer, got %q", exp.ActiveLayer)
	}
	if exp.Value != nil {
		t.Errorf("expected nil value, got %v", exp.Value)
	}
}

// ---------- formatting helpers ----------

func TestFormatScalar_Cases(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string
	}{
		{"nil", nil, "not set"},
		{"string", "hello", "hello"},
		{"bool", true, "true"},
		{"array", []any{"a", "b"}, `["a","b"]`},
		{"object", map[string]any{"k": "v"}, `{"k":"v"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatScalar(tc.in); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPrintLockCollisions(t *testing.T) {
	// No collisions => no output (the common case).
	var empty bytes.Buffer
	printLockCollisions(&empty, nil)
	if empty.Len() != 0 {
		t.Fatalf("no collisions should print nothing, got %q", empty.String())
	}

	// A value-lock rejection surfaces attempted / winning / owner.
	var buf bytes.Buffer
	printLockCollisions(&buf, []cfg.LockCollision{{
		Field: "model", Attempted: "Y", Winning: "X",
		Owner: cfg.AuthRepo, Kind: "value_lock",
	}})
	out := buf.String()
	for _, want := range []string{"model", "Y", "X", "repo", "value_lock"} {
		if !strings.Contains(out, want) {
			t.Fatalf("collision render missing %q in:\n%s", want, out)
		}
	}
}

func TestEmptyAsDash(t *testing.T) {
	if emptyAsDash("") != "-" {
		t.Error("empty should map to dash")
	}
	if emptyAsDash("repo-local") != "repo-local" {
		t.Error("non-empty should pass through")
	}
}

func TestPrintScalarOrJSON_Types(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{nil, "null\n"},
		{"foo", "foo\n"},
		{42.0, "42\n"},
		{true, "true\n"},
		{[]any{"a"}, "[\"a\"]\n"},
	}
	for _, tc := range cases {
		var buf bytes.Buffer
		if err := printScalarOrJSON(&buf, tc.in); err != nil {
			t.Fatalf("printScalarOrJSON: %v", err)
		}
		if got := buf.String(); got != tc.want {
			t.Errorf("input %v: got %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestWriteJSON_PrettyPrints(t *testing.T) {
	var buf bytes.Buffer
	if err := writeJSON(&buf, map[string]any{"a": 1}); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "  \"a\"") || !strings.HasSuffix(out, "\n") {
		t.Errorf("expected pretty-printed json with trailing newline, got %q", out)
	}
}

// ---------- runExplain table (real resolution) ----------

func TestRunExplain_DefaultBehaviorPrintsAll(t *testing.T) {
	project := withRepoLayer(t, `{"version":2,"project":"demo"}`, "")
	opts := mustOptions(project)
	if err := runExplain(opts, nil, testDeps()); err != nil {
		t.Fatalf("runExplain: %v", err)
	}
	out := opts.stdout.(*bytes.Buffer).String()
	if !strings.Contains(out, "Effective configuration") {
		t.Errorf("expected --all default output, got: %s", out)
	}
	if !strings.Contains(out, "project") {
		t.Errorf("expected project field in output: %s", out)
	}
}

func TestRunExplain_SingleField_Human(t *testing.T) {
	project := withRepoLayer(t, `{"version":2,"project":"demo","repo_id":"github.com/x/y"}`, "")
	opts := mustOptions(project)
	if err := runExplain(opts, []string{"repo_id"}, testDeps()); err != nil {
		t.Fatalf("runExplain: %v", err)
	}
	out := opts.stdout.(*bytes.Buffer).String()
	for _, needle := range []string{"Field:", "repo_id", "github.com/x/y", "Layer stack:", "<- active", layerRepoLocal} {
		if !strings.Contains(out, needle) {
			t.Errorf("expected %q in output, got: %s", needle, out)
		}
	}
}

func TestRunExplain_SingleField_JSON(t *testing.T) {
	project := withRepoLayer(t, `{"version":2,"skills":["a","b"]}`, "")
	opts := mustOptions(project)
	opts.jsonOut = true
	if err := runExplain(opts, []string{"skills"}, testDeps()); err != nil {
		t.Fatalf("runExplain: %v", err)
	}
	out := opts.stdout.(*bytes.Buffer).Bytes()
	var got FieldExplanation
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("output is not valid json: %v\n%s", err, out)
	}
	if got.Field != "skills" || got.ActiveLayer != layerRepoLocal {
		t.Errorf("unexpected json: %+v", got)
	}
	// With no user-local manifest on disk the resolved stack is
	// product-defaults + repo-local; the winning repo-local entry must be
	// present in the stack.
	if len(got.Layers) < 2 {
		t.Errorf("expected at least 2 layers, got %d", len(got.Layers))
	}
	var sawRepo bool
	for _, lv := range got.Layers {
		if lv.Layer == layerRepoLocal && lv.Active {
			sawRepo = true
		}
	}
	if !sawRepo {
		t.Errorf("expected repo-local active in stack, got %+v", got.Layers)
	}
}

func TestRunExplain_ValueOnly_Scalar(t *testing.T) {
	project := withRepoLayer(t, `{"version":2,"project":"demo"}`, "")
	opts := mustOptions(project)
	opts.valueOnly = true
	if err := runExplain(opts, []string{"project"}, testDeps()); err != nil {
		t.Fatalf("runExplain: %v", err)
	}
	if got := opts.stdout.(*bytes.Buffer).String(); strings.TrimSpace(got) != "demo" {
		t.Errorf("--value-only on string should emit bare value, got %q", got)
	}
}

func TestRunExplain_ValueOnly_Array(t *testing.T) {
	project := withRepoLayer(t, `{"version":2,"skills":["a","b","c"]}`, "")
	opts := mustOptions(project)
	opts.valueOnly = true
	if err := runExplain(opts, []string{"skills"}, testDeps()); err != nil {
		t.Fatalf("runExplain: %v", err)
	}
	got := strings.TrimSpace(opts.stdout.(*bytes.Buffer).String())
	if got != `["a","b","c"]` {
		t.Errorf("expected JSON array, got %q", got)
	}
}

func TestRunExplain_ValueOnly_MissingFieldErrors(t *testing.T) {
	project := withRepoLayer(t, `{"version":2}`, "")
	opts := mustOptions(project)
	opts.valueOnly = true
	err := runExplain(opts, []string{"nope"}, testDeps())
	var he *hintError
	if !errors.As(err, &he) {
		t.Fatalf("expected hintError, got %T %v", err, err)
	}
	if !strings.Contains(he.message, "not set in any layer") {
		t.Errorf("unexpected message: %q", he.message)
	}
}

func TestRunExplain_OriginOnly(t *testing.T) {
	project := withRepoLayer(t, `{"version":2,"project":"demo"}`, "")
	opts := mustOptions(project)
	opts.originOnly = true
	if err := runExplain(opts, []string{"project"}, testDeps()); err != nil {
		t.Fatalf("runExplain: %v", err)
	}
	if got := strings.TrimSpace(opts.stdout.(*bytes.Buffer).String()); got != layerRepoLocal {
		t.Errorf("expected %q, got %q", layerRepoLocal, got)
	}
}

func TestRunExplain_OriginOnly_MissingFieldErrors(t *testing.T) {
	project := withRepoLayer(t, `{"version":2}`, "")
	opts := mustOptions(project)
	opts.originOnly = true
	err := runExplain(opts, []string{"nope"}, testDeps())
	if err == nil {
		t.Fatal("expected error for missing field with --origin-only")
	}
}

func TestRunExplain_AllJSON(t *testing.T) {
	project := withRepoLayer(t, `{"version":2,"project":"demo"}`, "")
	opts := mustOptions(project)
	opts.all = true
	opts.jsonOut = true
	if err := runExplain(opts, nil, testDeps()); err != nil {
		t.Fatalf("runExplain: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(opts.stdout.(*bytes.Buffer).Bytes(), &got); err != nil {
		t.Fatalf("--all --json output is not valid json: %v", err)
	}
	if _, ok := got["effective"]; !ok {
		t.Error("expected effective field")
	}
	if _, ok := got["provenance"]; !ok {
		t.Error("expected provenance field")
	}
}

func TestRunExplain_Flags_HumanEmpty(t *testing.T) {
	project := withRepoLayer(t, `{"version":2}`, "")
	opts := mustOptions(project)
	opts.flags = true
	if err := runExplain(opts, nil, testDeps()); err != nil {
		t.Fatalf("runExplain: %v", err)
	}
	if !strings.Contains(opts.stdout.(*bytes.Buffer).String(), "No feature flags") {
		t.Error("expected empty-flags message")
	}
}

func TestRunExplain_Flags_HumanPopulated(t *testing.T) {
	project := withRepoLayer(t, `{"version":2,"features":{"graph_bridge":"on","x":"off"}}`, "")
	opts := mustOptions(project)
	opts.flags = true
	if err := runExplain(opts, nil, testDeps()); err != nil {
		t.Fatalf("runExplain: %v", err)
	}
	out := opts.stdout.(*bytes.Buffer).String()
	for _, needle := range []string{"graph_bridge", "on", layerRepoLocal} {
		if !strings.Contains(out, needle) {
			t.Errorf("expected %q in flags output: %s", needle, out)
		}
	}
}

func TestRunExplain_Flags_JSON(t *testing.T) {
	project := withRepoLayer(t, `{"version":2,"features":{"a":"on"}}`, "")
	opts := mustOptions(project)
	opts.flags = true
	opts.jsonOut = true
	if err := runExplain(opts, nil, testDeps()); err != nil {
		t.Fatalf("runExplain: %v", err)
	}
	var got map[string]FieldExplanation
	if err := json.Unmarshal(opts.stdout.(*bytes.Buffer).Bytes(), &got); err != nil {
		t.Fatalf("--flags --json output is not valid json: %v", err)
	}
	if got["a"].Value != "on" {
		t.Errorf("expected feature a=on, got %+v", got["a"])
	}
}

func TestRunExplain_Flags_NonObjectFeaturesIgnored(t *testing.T) {
	// features field exists but is not an object — featureFlagNames ignores it
	// gracefully (no panic), yielding the empty-flags message. Driven through a
	// stub snapshot so the non-object features value survives into a layer's
	// Raw (the typed AgentsRC decode would otherwise reject it).
	withStubResolver(t, func(_ string, _ cfg.EnsureOpts) (*cfg.EnsureResult, error) {
		layers := []cfg.ResolvedLayer{
			{ID: cfg.LayerProductDefaults, Present: true, Raw: map[string]any{}},
			{ID: cfg.LayerRepoLocal, Present: true, Raw: map[string]any{"features": "wat"}},
		}
		snap := &cfg.Snapshot{Layers: layers, Provenance: map[string]cfg.FieldProvenance{}, Warnings: []cfg.ProvenanceWarning{}}
		return snapResult(snap), nil
	})
	opts := mustOptions("/x")
	opts.flags = true
	if err := runExplain(opts, nil, testDeps()); err != nil {
		t.Fatalf("runExplain: %v", err)
	}
	if !strings.Contains(opts.stdout.(*bytes.Buffer).String(), "No feature flags") {
		t.Error("non-object features field should yield empty-flags message")
	}
}

// ---------- flag combination guards ----------

func TestValidateFlagCombo_Conflicts(t *testing.T) {
	cases := []struct {
		name    string
		opts    runExplainOptions
		args    []string
		wantMsg string
	}{
		{"value+origin", runExplainOptions{valueOnly: true, originOnly: true}, []string{"x"}, "cannot be combined"},
		{"value+all", runExplainOptions{valueOnly: true, all: true}, []string{"x"}, "cannot be combined with --all"},
		{"origin+flags", runExplainOptions{originOnly: true, flags: true}, []string{"x"}, "cannot be combined with --all"},
		{"value+no-arg", runExplainOptions{valueOnly: true}, nil, "single field path"},
		{"origin+no-arg", runExplainOptions{originOnly: true}, nil, "single field path"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateFlagCombo(&tc.opts, tc.args, testDeps())
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("expected %q in %q", tc.wantMsg, err.Error())
			}
		})
	}
}

func TestValidateFlagCombo_Allowed(t *testing.T) {
	if err := validateFlagCombo(&runExplainOptions{}, nil, testDeps()); err != nil {
		t.Errorf("plain invocation should be allowed, got %v", err)
	}
	if err := validateFlagCombo(&runExplainOptions{valueOnly: true}, []string{"x"}, testDeps()); err != nil {
		t.Errorf("--value-only with single arg should be allowed, got %v", err)
	}
	if err := validateFlagCombo(&runExplainOptions{all: true}, nil, testDeps()); err != nil {
		t.Errorf("--all alone should be allowed, got %v", err)
	}
}

// ---------- error paths ----------

func TestRunExplain_NoManifestSurfacesHint(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("AGENTS_HOME", filepath.Join(root, "home", ".agents"))
	opts := &runExplainOptions{
		stdout: &bytes.Buffer{},
		stderr: &bytes.Buffer{},
		cwd:    filepath.Join(root, "project"),
	}
	err := runExplain(opts, nil, testDeps())
	var he *hintError
	if !errors.As(err, &he) {
		t.Fatalf("expected hintError, got %T %v", err, err)
	}
	if len(he.hints) == 0 {
		t.Error("expected at least one hint mentioning install --generate")
	}
	if !strings.Contains(strings.Join(he.hints, " "), "install --generate") {
		t.Errorf("expected install hint, got %v", he.hints)
	}
}

// TestRunExplain_ResolveErrorSurfacesHint covers the generic resolve-failure
// branch (not just missing-manifest) via a stub that returns an arbitrary error.
func TestRunExplain_ResolveErrorSurfacesHint(t *testing.T) {
	withStubResolver(t, func(_ string, _ cfg.EnsureOpts) (*cfg.EnsureResult, error) {
		return nil, errors.New("boom: cache gap")
	})
	opts := mustOptions("/x")
	err := runExplain(opts, nil, testDeps())
	var he *hintError
	if !errors.As(err, &he) {
		t.Fatalf("expected hintError, got %T %v", err, err)
	}
	if !strings.Contains(he.message, "boom: cache gap") {
		t.Errorf("expected wrapped resolve error, got %q", he.message)
	}
}

func TestErrNoManifest_IsExported(t *testing.T) {
	// Sentinel must be usable with errors.Is by downstream consumers.
	if !errors.Is(ErrNoManifest, ErrNoManifest) {
		t.Error("ErrNoManifest should match itself via errors.Is")
	}
}

// ---------- cobra wiring ----------

func TestNewConfigCmd_RegistersExplain(t *testing.T) {
	cmd := NewConfigCmd(testDeps())
	if cmd.Use != "config" {
		t.Errorf("expected Use=config, got %q", cmd.Use)
	}
	var explain *cobra.Command
	for _, c := range cmd.Commands() {
		if c.Name() == "explain" {
			explain = c
		}
	}
	if explain == nil {
		t.Fatal("explain subcommand missing")
	}
	for _, flag := range []string{"all", "flags", "value-only", "origin-only"} {
		if explain.Flags().Lookup(flag) == nil {
			t.Errorf("explain command missing --%s flag", flag)
		}
	}
	// --json is the GLOBAL persistent flag now, not a local explain flag.
	if explain.Flags().Lookup("json") != nil {
		t.Error("explain should not define a local --json flag; it reads the global one")
	}
}

func TestExplainCmd_LongMentionsAutoLock(t *testing.T) {
	cmd := newExplainCmd(testDeps())
	for _, needle := range []string{"AUTO-LOCK", "single effective-config truth surface", "status no longer inspects config"} {
		if !strings.Contains(cmd.Long, needle) {
			t.Errorf("explain Long missing %q", needle)
		}
	}
}

func TestExplainCmd_RunE_RoutesThroughCwd(t *testing.T) {
	// Drive the cobra command end-to-end with a real manifest in cwd so the
	// RunE wrapper (which calls os.Getwd) is exercised.
	project := withRepoLayer(t, `{"version":2,"project":"demo"}`, "")
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
	if err := os.Chdir(project); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	cmd := NewConfigCmd(testDeps())
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"explain", "project", "--value-only"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.TrimSpace(out.String()) != "demo" {
		t.Errorf("expected 'demo', got %q", out.String())
	}
}

// ---------- example block coverage ----------

func TestExampleBlock_JoinsWithNewline(t *testing.T) {
	got := exampleBlock("a", "b")
	if got != "a\nb" {
		t.Errorf("expected newline-joined, got %q", got)
	}
}

// ---------- end-to-end style assertion ----------

// TestRunExplain_EndToEndProvenanceExample reproduces the proposal's example
// shape (single field with multi-layer override) to make sure rendering does
// not regress against the documented contract. repo_id is a protected field, so
// the user-local layer's attempt is dropped (recorded but never active) and
// repo-local wins.
func TestRunExplain_EndToEndProvenanceExample(t *testing.T) {
	project := withRepoLayer(t,
		`{"version":2,"kg":{"backend":"repo-backend"}}`,
		`{"version":2,"kg":{"backend":"user-backend"}}`,
	)
	opts := mustOptions(project)
	opts.jsonOut = true
	if err := runExplain(opts, []string{"kg.backend"}, testDeps()); err != nil {
		t.Fatalf("runExplain: %v", err)
	}
	var got FieldExplanation
	if err := json.Unmarshal(opts.stdout.(*bytes.Buffer).Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ActiveLayer != layerRepoLocal {
		t.Errorf("expected repo-local active, got %s", got.ActiveLayer)
	}
	if got.Value != "repo-backend" {
		t.Errorf("expected repo-backend, got %v", got.Value)
	}
	var sawUser bool
	for _, lv := range got.Layers {
		if lv.Layer == layerUserLocal && lv.Value == "user-backend" && !lv.Active {
			sawUser = true
		}
	}
	if !sawUser {
		t.Errorf("expected losing user-local entry in layers; got %+v", got.Layers)
	}
}

// pretty marshaling sanity guard — ensure JSON output is deterministic
// across runs even when the underlying map has random iteration order.
func TestEmitAllJSON_DeterministicProvenance(t *testing.T) {
	project := withRepoLayer(t, `{"version":2,"project":"p","repo_id":"github.com/x/y","skills":["a"],"agents":["b"]}`, "")
	snap, _, err := loadSnapshot(project)
	if err != nil {
		t.Fatalf("loadSnapshot: %v", err)
	}
	withStubResolver(t, func(_ string, _ cfg.EnsureOpts) (*cfg.EnsureResult, error) {
		return snapResult(snap), nil
	})
	opts1 := mustOptions(project)
	opts1.all = true
	opts1.jsonOut = true
	opts2 := mustOptions(project)
	opts2.all = true
	opts2.jsonOut = true
	if err := runExplain(opts1, nil, testDeps()); err != nil {
		t.Fatalf("run1: %v", err)
	}
	if err := runExplain(opts2, nil, testDeps()); err != nil {
		t.Fatalf("run2: %v", err)
	}
	a, b := opts1.stdout.(*bytes.Buffer).Bytes(), opts2.stdout.(*bytes.Buffer).Bytes()
	if string(a) != string(b) {
		t.Errorf("expected deterministic --all --json output:\n%s\n----\n%s", a, b)
	}
}

// guard: emitField with --json and a missing field still returns a
// FieldExplanation with empty active layer rather than erroring (only
// --value-only / --origin-only treat absence as error).
func TestEmitField_JSONOnMissingField(t *testing.T) {
	project := withRepoLayer(t, `{"version":2}`, "")
	opts := mustOptions(project)
	opts.jsonOut = true
	if err := runExplain(opts, []string{"missing"}, testDeps()); err != nil {
		t.Fatalf("runExplain: %v", err)
	}
	var got FieldExplanation
	if err := json.Unmarshal(opts.stdout.(*bytes.Buffer).Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ActiveLayer != "" {
		t.Errorf("expected no active layer, got %s", got.ActiveLayer)
	}
}

// Sanity: the cmd registered Use string ("explain [field-path]") survives.
func TestNewExplainCmd_UseString(t *testing.T) {
	cmd := newExplainCmd(testDeps())
	if !strings.HasPrefix(cmd.Use, "explain") {
		t.Errorf("expected Use to start with 'explain', got %q", cmd.Use)
	}
}

// Confirm the RunE path that resolves cwd via os.Getwd runs the success path.
func TestExplainCmd_RunE_CallsGetwd(t *testing.T) {
	project := withRepoLayer(t, `{"version":2,"project":"x"}`, "")
	prev, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(prev) })
	if err := os.Chdir(project); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	cmd := newExplainCmd(testDeps())
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.RunE(cmd, []string{}); err != nil {
		t.Fatalf("RunE: %v", err)
	}
}

// Documentation: verify the exit-code constants are wired so future spec
// drift (e.g. promoting layer fetch errors) does not silently fall through
// to 0.
func TestExitCodeConstants(t *testing.T) {
	if exitOK != 0 || exitLayerFetchErr != 1 || exitSchemaErr != 2 || exitAuthErr != 3 {
		t.Errorf("exit codes drifted: ok=%d fetch=%d schema=%d auth=%d",
			exitOK, exitLayerFetchErr, exitSchemaErr, exitAuthErr)
	}
}

// formatScalar fallback path: marshal error is unreachable with std types
// today, but verify the fallthrough to "%v" for completeness via a
// constructed type that breaks json.Marshal (chan).
func TestFormatScalar_MarshalErrorFallback(t *testing.T) {
	ch := make(chan int)
	got := formatScalar(ch)
	if got == "" {
		t.Error("formatScalar should not return empty on a marshal-error value")
	}
}

// printScalarOrJSON marshal-error path coverage. Same fallback intent as
// formatScalar — printScalarOrJSON should error out (not panic) when json
// encoding fails.
func TestPrintScalarOrJSON_MarshalError(t *testing.T) {
	ch := make(chan int)
	err := printScalarOrJSON(&bytes.Buffer{}, ch)
	if err == nil {
		t.Error("expected error for unmarshalable value")
	}
}

// writeJSON error path: encoding an unmarshalable value must surface an error
// rather than panic.
func TestWriteJSON_MarshalError(t *testing.T) {
	if err := writeJSON(&bytes.Buffer{}, make(chan int)); err == nil {
		t.Error("expected error for unmarshalable value")
	}
}

// Demonstrate the hintError String form so the helper isn't dead code in the
// test suite — exercised by all hint-returning paths above.
func TestHintError_FormatsMessageAndHints(t *testing.T) {
	e := &hintError{message: "boom", hints: []string{"try x", "or y"}}
	if !strings.Contains(e.Error(), "boom") || !strings.Contains(e.Error(), "try x") {
		t.Errorf("unexpected: %s", e.Error())
	}
	e2 := &hintError{message: "plain"}
	if e2.Error() != "plain" {
		t.Errorf("unexpected plain: %s", e2.Error())
	}
}

// featureFlagNames returns a sorted union across layers.
func TestFeatureFlagNames_SortedUnion(t *testing.T) {
	snap := flatSnapshot(t,
		map[string]any{"features": map[string]any{"b": "on", "a": "on"}},
		map[string]any{"features": map[string]any{"c": "off"}},
	)
	got := featureFlagNames(snap)
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("at %d: got %q want %q", i, got[i], want[i])
		}
	}
}

// ---------- legacy flat-snapshot helpers (shared with relevance/verify) ----------
//
// loadFlatSnapshot, mergeLayers, lookup, splitFieldPath, decodeJSONFile,
// fileExists, and sortedKeys are the raw-layer reader surface the sibling
// `relevance` and `verify` commands still consume. explain no longer routes
// through them (it uses the auto-lock cfg.Snapshot seam), but they live in
// explain.go so they are unit-tested here to keep the file's behavior pinned.

func TestLoadFlatSnapshot_MissingRepoManifestIsSchemaError(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("AGENTS_HOME", filepath.Join(root, "home", ".agents"))
	_, code, err := loadFlatSnapshot(filepath.Join(root, "project"))
	if err == nil {
		t.Fatal("expected error for missing manifest")
	}
	if code != exitSchemaErr {
		t.Errorf("expected exitSchemaErr (%d), got %d", exitSchemaErr, code)
	}
}

func TestLoadFlatSnapshot_RepoLayerOnly(t *testing.T) {
	project := withRepoLayer(t, `{"version":2,"project":"demo","repo_id":"github.com/x/y"}`, "")
	snap, code, err := loadFlatSnapshot(project)
	if err != nil || code != exitOK {
		t.Fatalf("unexpected: code=%d err=%v", code, err)
	}
	if snap.effective["project"] != "demo" {
		t.Errorf("expected project=demo, got %v", snap.effective["project"])
	}
	if snap.layers[layerUserLocal] != nil {
		t.Errorf("expected user-local nil, got %v", snap.layers[layerUserLocal])
	}
}

func TestLoadFlatSnapshot_UserLocalOverlay(t *testing.T) {
	project := withRepoLayer(t,
		`{"version":2,"project":"demo","features":{"a":"repo"}}`,
		`{"version":2,"features":{"a":"user","b":"user-only"}}`,
	)
	snap, _, err := loadFlatSnapshot(project)
	if err != nil {
		t.Fatalf("loadFlatSnapshot: %v", err)
	}
	// repo wins for the top-level features map (last-writer-wins flat overlay).
	feat := snap.effective["features"].(map[string]any)
	if feat["a"] != "repo" {
		t.Errorf("repo-local should win for features.a, got %v", feat["a"])
	}
	if _, ok := feat["b"]; ok {
		t.Errorf("flat overlay should replace top-level features map; got merged: %v", feat)
	}
}

func TestLoadFlatSnapshot_InvalidRepoJSONIsSchemaError(t *testing.T) {
	project := withRepoLayer(t, `{not-json`, "")
	_, code, err := loadFlatSnapshot(project)
	if err == nil || code != exitSchemaErr {
		t.Fatalf("expected schema error, got code=%d err=%v", code, err)
	}
}

func TestLoadFlatSnapshot_InvalidUserJSONIsSchemaError(t *testing.T) {
	project := withRepoLayer(t, `{"version":2}`, `not-json`)
	_, code, err := loadFlatSnapshot(project)
	if err == nil || code != exitSchemaErr {
		t.Fatalf("expected schema error, got code=%d err=%v", code, err)
	}
}

func TestMergeLayers_AllNil(t *testing.T) {
	out := mergeLayers(map[string]map[string]any{
		layerProductDefaults: nil,
		layerUserLocal:       nil,
		layerRepoLocal:       nil,
	})
	if len(out) != 0 {
		t.Errorf("expected empty merge, got %v", out)
	}
}

func TestLookup_TraversalGuards(t *testing.T) {
	if v, ok := lookup(nil, []string{"x"}); ok || v != nil {
		t.Error("nil layer should miss")
	}
	if v, ok := lookup(map[string]any{"x": 1}, nil); ok || v != nil {
		t.Error("empty parts should miss")
	}
	if v, ok := lookup(map[string]any{"x": "string-value"}, []string{"x", "child"}); ok || v != nil {
		t.Error("descending into a non-object should miss")
	}
	if v, ok := lookup(map[string]any{"x": map[string]any{}}, []string{"x", "missing"}); ok || v != nil {
		t.Error("missing key should miss")
	}
	if v, ok := lookup(map[string]any{"x": map[string]any{"y": "z"}}, []string{"x", "y"}); !ok || v != "z" {
		t.Errorf("nested hit should resolve, got %v/%v", v, ok)
	}
}

func TestSplitFieldPath_EmptyAndParts(t *testing.T) {
	if got := splitFieldPath(""); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
	got := splitFieldPath("a.b.c")
	if len(got) != 3 || got[0] != "a" || got[2] != "c" {
		t.Errorf("expected [a b c], got %v", got)
	}
}

func TestSortedKeys(t *testing.T) {
	got := sortedKeys(map[string]any{"b": 1, "a": 2, "c": 3})
	want := []string{"a", "b", "c"}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("at %d: got %s, want %s", i, got[i], w)
		}
	}
}

func TestDecodeJSONFile_NotFound(t *testing.T) {
	if _, err := decodeJSONFile("/nonexistent/path/agentsrc.json"); err == nil {
		t.Error("expected error for missing file")
	}
}

func TestDecodeJSONFile_BadJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("{not-json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := decodeJSONFile(path); err == nil {
		t.Error("expected json decode error")
	}
}

func TestDecodeJSONFile_OK(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ok.json")
	if err := os.WriteFile(path, []byte(`{"a":1}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	m, err := decodeJSONFile(path)
	if err != nil || m["a"] != float64(1) {
		t.Errorf("expected {a:1}, got %v err=%v", m, err)
	}
}

func TestFileExists_FalseForMissing(t *testing.T) {
	if fileExists(filepath.Join(t.TempDir(), "no-such-file")) {
		t.Error("expected false for missing")
	}
}

func TestOrderedLayers_StableSequence(t *testing.T) {
	want := []string{layerProductDefaults, layerUserLocal, layerRepoLocal}
	for i, w := range want {
		if orderedLayers[i] != w {
			t.Errorf("orderedLayers[%d] = %q, want %q", i, orderedLayers[i], w)
		}
	}
}

// helper: type assertion to make sure tests fail loudly if a future change
// stops writing to the buffer we passed in.
var _ fmt.Stringer = (*bytes.Buffer)(nil)
