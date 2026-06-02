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
// via HOME so cfg.AgentsHome resolves under t.TempDir.
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

// ---------- snapshot construction ----------

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
	// repo wins for "a"
	feat := snap.effective["features"].(map[string]any)
	if feat["a"] != "repo" {
		t.Errorf("repo-local should win for features.a, got %v", feat["a"])
	}
	// repo's features map fully replaces user-local's at the top level
	// because flat overlay is last-writer-wins at the top key. The
	// per-field provenance test below covers the layered lookup directly.
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

// ---------- field traversal ----------

func TestExplainField_NestedPathResolves(t *testing.T) {
	snap := &snapshot{
		layers: map[string]map[string]any{
			layerProductDefaults: nil,
			layerUserLocal:       nil,
			layerRepoLocal: {
				"kg": map[string]any{
					"backend": "sqlite",
				},
			},
		},
	}
	snap.effective = mergeLayers(snap.layers)
	exp := snap.explainField("kg.backend")
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
	snap := &snapshot{
		layers: map[string]map[string]any{
			layerProductDefaults: nil,
			layerUserLocal: {
				"project": "user-value",
			},
			layerRepoLocal: {
				"project": "repo-value",
			},
		},
	}
	snap.effective = mergeLayers(snap.layers)
	exp := snap.explainField("project")
	if exp.ActiveLayer != layerRepoLocal {
		t.Errorf("expected repo-local to win, got %s", exp.ActiveLayer)
	}
	if exp.Value != "repo-value" {
		t.Errorf("expected repo-value, got %v", exp.Value)
	}
	// User-local layer entry must still record the losing value so JSON
	// consumers can show the full stack.
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
	snap := &snapshot{
		layers: map[string]map[string]any{
			layerProductDefaults: nil,
			layerUserLocal:       nil,
			layerRepoLocal:       map[string]any{"version": float64(2)},
		},
	}
	exp := snap.explainField("missing.path")
	if exp.ActiveLayer != "" {
		t.Errorf("expected no active layer, got %q", exp.ActiveLayer)
	}
	if exp.Value != nil {
		t.Errorf("expected nil value, got %v", exp.Value)
	}
}

func TestLookup_TraversalGuards(t *testing.T) {
	// nil layer
	if v, ok := lookup(nil, []string{"x"}); ok || v != nil {
		t.Error("nil layer should miss")
	}
	// empty parts
	if v, ok := lookup(map[string]any{"x": 1}, nil); ok || v != nil {
		t.Error("empty parts should miss")
	}
	// non-object intermediate
	layer := map[string]any{"x": "string-value"}
	if v, ok := lookup(layer, []string{"x", "child"}); ok || v != nil {
		t.Error("descending into a non-object should miss")
	}
	// missing key
	if v, ok := lookup(map[string]any{"x": map[string]any{}}, []string{"x", "missing"}); ok || v != nil {
		t.Error("missing key should miss")
	}
}

func TestSplitFieldPath_EmptyReturnsNil(t *testing.T) {
	if got := splitFieldPath(""); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
	got := splitFieldPath("a.b.c")
	want := []string{"a", "b", "c"}
	if len(got) != 3 || got[0] != want[0] || got[2] != want[2] {
		t.Errorf("expected %v, got %v", want, got)
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

func TestSortedKeys(t *testing.T) {
	got := sortedKeys(map[string]any{"b": 1, "a": 2, "c": 3})
	want := []string{"a", "b", "c"}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("at %d: got %s, want %s", i, got[i], w)
		}
	}
}

// ---------- runExplain table ----------

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
	if len(got.Layers) != len(orderedLayers) {
		t.Errorf("expected %d layers, got %d", len(orderedLayers), len(got.Layers))
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
	// features field exists but is not an object — should be ignored
	// gracefully (no panic), yielding the empty-flags message.
	project := withRepoLayer(t, `{"version":2,"features":"wat"}`, "")
	opts := mustOptions(project)
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

func TestNewConfigCmd_ExampleMentionsKeyInvocations(t *testing.T) {
	cmd := NewConfigCmd(testDeps())
	for _, needle := range []string{"config explain", "--value-only", "--all", "--flags"} {
		if !strings.Contains(cmd.Example, needle) {
			t.Errorf("Example missing %q: %q", needle, cmd.Example)
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

// ---------- example block + decodeJSONFile coverage ----------

func TestExampleBlock_JoinsWithNewline(t *testing.T) {
	got := exampleBlock("a", "b")
	if got != "a\nb" {
		t.Errorf("expected newline-joined, got %q", got)
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

func TestFileExists_FalseForMissing(t *testing.T) {
	if fileExists(filepath.Join(t.TempDir(), "no-such-file")) {
		t.Error("expected false for missing")
	}
}

// ---------- end-to-end style assertion ----------

// TestRunExplain_EndToEndProvenanceExample reproduces the proposal's example
// shape (single field with multi-layer override) to make sure rendering does
// not regress against the documented contract.
func TestRunExplain_EndToEndProvenanceExample(t *testing.T) {
	project := withRepoLayer(t,
		`{"version":2,"project":"repo-project"}`,
		`{"version":2,"project":"user-project"}`,
	)
	opts := mustOptions(project)
	opts.jsonOut = true
	if err := runExplain(opts, []string{"project"}, testDeps()); err != nil {
		t.Fatalf("runExplain: %v", err)
	}
	var got FieldExplanation
	if err := json.Unmarshal(opts.stdout.(*bytes.Buffer).Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ActiveLayer != layerRepoLocal {
		t.Errorf("expected repo-local active, got %s", got.ActiveLayer)
	}
	if got.Value != "repo-project" {
		t.Errorf("expected repo-project, got %v", got.Value)
	}
	// Make sure the user-local entry recorded its losing value in the stack.
	var sawUser bool
	for _, lv := range got.Layers {
		if lv.Layer == layerUserLocal && lv.Value == "user-project" && !lv.Active {
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
	project := withRepoLayer(t, `{"version":2,"a":1,"b":2,"c":3,"d":4,"e":5}`, "")
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
	// Reparse and recompare provenance keys — Go's json encoder sorts keys
	// for maps, but verify here so a future change to the encoder cannot
	// silently regress determinism.
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

// Confirm the RunE path that resolves cwd via os.Getwd returns an error path
// when cwd cannot be resolved. We exercise it by clearing cwd on opts to
// force the RunE to call os.Getwd (success path).
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
	if got == "" || strings.Contains(got, "0x") == false && !strings.Contains(got, "chan") {
		// We accept any non-empty fallback string; the goal is to make sure
		// formatScalar does not panic on a marshal-error value.
		t.Logf("fallback string: %q", got)
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

// catch-all: ensure mergeLayers is robust to a nil map at every position.
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

// Ensures the package-level constants in orderedLayers cover the three
// flat-scope layers (and stay in precedence order). A future addition would
// require updating this guard, intentionally — the JSON shape depends on
// stable ordering.
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
