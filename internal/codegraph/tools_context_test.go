package codegraph

// Context-tool parity against the pinned code-review-graph v2.3.8 release.
//
// Every recorded case for `get_impact_radius_tool` and
// `get_review_context_tool` is replayed against a store seeded DIRECTLY from
// testdata/crg-release/v2.3.8/graph.json, so a failure here is a failure of
// the two handlers in tools_context.go and of nothing else — never of the
// scanner, the derived-view computations, or community detection.
//
// The repository itself is materialized with its real two-commit history,
// because the defaults cases auto-detect their change set from git and the
// confidence marker compares the graph's build commit against live HEAD.
//
// Cases are discovered by globbing, so a fixture added for either tool — a
// behaviour case or one of the validation sweep's bound cases — is covered
// automatically rather than silently skipped.

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/crgrelease"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// ctxTools are the two tools this file owns.
var ctxTools = []string{"get_impact_radius_tool", "get_review_context_tool"}

// ctxAutoDetectedChangedFiles is the change set `base: HEAD~1` yields in the
// fixture repository, whose second commit adds exactly this path.
//
// It is not an assumption: ctxAssertAutoDetection below requires it to equal
// the `changed_files` the release itself recorded for the defaults case, so a
// change to the fixture history fails loudly instead of quietly re-baselining
// the token estimates that depend on it.
var ctxAutoDetectedChangedFiles = []string{"pkg/auth/token.go"}

func TestContextToolsMatchReleaseContract(t *testing.T) {
	root := materializeFixtureRepo(t)
	engine := ctxSeededEngine(t, root)

	paths := ctxFixturePaths(t)
	if len(paths) == 0 {
		t.Fatalf("no recorded cases found for %v under %s/calls", ctxTools, releaseFixtureDir)
	}
	ctxAssertAutoDetection(t, root)

	for _, path := range paths {
		t.Run(strings.TrimSuffix(filepath.Base(path), ".json"), func(t *testing.T) {
			ctxAssertCase(t, engine, root, commLoadFixture(t, path, root))
		})
	}
}

// ctxAssertCase replays one recorded call and compares the WHOLE payload.
func ctxAssertCase(t *testing.T, engine *Engine, root string, f commFixture) {
	t.Helper()

	tool, ok := crgrelease.Lookup(f.Tool)
	if !ok {
		t.Fatalf("%s is not published by code-review-graph %s", f.Tool, crgrelease.Version)
	}
	args, bindErr := tool.Bind(f.Arguments)
	if bindErr != nil {
		// A schema violation is rejected before dispatch, so the handler is
		// never reached and the recorded text is the validator's verbatim.
		if !f.IsError {
			t.Fatalf("bind %s: %v", f.Arguments, bindErr)
		}
		if bindErr.Message != f.ErrorText {
			t.Fatalf("binding error =\n%s\nwant\n%s", bindErr.Message, f.ErrorText)
		}
		return
	}
	handler, ok := toolHandlers[f.Tool]
	if !ok {
		t.Fatalf("%s has no native handler", f.Tool)
	}
	got, err := handler(engine, args)

	if f.IsError {
		// A bound violation is raised before upstream's try block, so it
		// escapes as an MCP transport error with no payload at all. The
		// uniform "Error calling tool '<tool>': " prefix is the server's.
		want := strings.TrimPrefix(f.ErrorText, fmt.Sprintf("Error calling tool '%s': ", f.Tool))
		if err == nil {
			t.Fatalf("want error %q, got payload %v", want, got)
		}
		if err.Error() != want {
			t.Fatalf("error = %q, want %q", err.Error(), want)
		}
		return
	}
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	// `_graph` is Engine.CallTool's provenance envelope, attached around the
	// handler rather than by it.
	want := map[string]any{}
	for key, value := range f.Structured {
		if key != "_graph" {
			want[key] = value
		}
	}
	have := commCanonical(t, got)
	if diff := commDiff("", have, commCanonical(t, want)); diff != "" {
		t.Fatalf("payload mismatch at %s", diff)
	}
	if _, recorded := want["context_savings"]; recorded {
		ctxAssertSavings(t, f, root, have)
	}
}

// ctxAssertSavings checks the context-savings block against a value DERIVED
// for THIS repository root from the release's own recorded payload.
//
// The recorded numbers themselves are not portable: both sides of the ratio
// count tokens over payloads that embed absolute paths, so they scale with the
// length of the generating machine's temp root, and the fixture normalizes
// them to ${SAVED_TOKENS} / ${SAVED_PERCENT}. The RELATION is portable, and it
// is what a handler can get wrong: the baseline is what reading the changed
// files outright would have cost, and the returned side is the payload as it
// stands before the block is attached — which is why the handler must attach
// it last.
//
// The baseline is recomputed from file sizes here rather than through
// EstimateFileTokens, so this is an independent check of that function too.
// The returned side does use EstimateTokens, which
// TestEstimateTokensMatchesPythonJSONLength pins directly against Python.
func ctxAssertSavings(t *testing.T, f commFixture, root string, got any) {
	t.Helper()

	original := ctxFileTokenBaseline(t, root, ctxChangedFiles(t, f))
	if original <= 0 {
		t.Fatalf("baseline estimate %d, want a positive token count", original)
	}
	returned := map[string]any{}
	for key, value := range f.Structured {
		if key != "_graph" && key != "context_savings" {
			returned[key] = value
		}
	}
	saved := max(0, original-EstimateTokens(returned))
	want := map[string]any{
		"estimated":     true,
		"saved_tokens":  float64(saved),
		"saved_percent": math.Round(float64(saved) / float64(original) * 100),
	}
	payload, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("payload is %T, want an object", got)
	}
	if diff := commDiff("context_savings", payload["context_savings"], want); diff != "" {
		t.Fatalf("context_savings mismatch at %s", diff)
	}
}

// ctxFileTokenBaseline sums ceil(size/4) over the files that exist, which is
// upstream's estimate_file_tokens spelled out independently.
func ctxFileTokenBaseline(t *testing.T, root string, files []string) int {
	t.Helper()
	total := 0
	for _, name := range files {
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			continue
		}
		total += max(1, int((info.Size()+3)/4))
	}
	return total
}

// ctxChangedFiles is the change set a recorded call's baseline is measured
// over: the caller's explicit list, or the git-detected one when the argument
// was omitted.
func ctxChangedFiles(t *testing.T, f commFixture) []string {
	t.Helper()
	var args struct {
		ChangedFiles []string `json:"changed_files"`
	}
	if err := json.Unmarshal(f.Arguments, &args); err != nil {
		t.Fatalf("decode arguments %s: %v", f.Arguments, err)
	}
	if args.ChangedFiles != nil {
		return args.ChangedFiles
	}
	return ctxAutoDetectedChangedFiles
}

// ctxAssertAutoDetection pins the auto-detected change set against the set the
// release recorded for the same repository, and against what the native
// discovery helper reports for the rebuilt one.
func ctxAssertAutoDetection(t *testing.T, root string) {
	t.Helper()

	recorded := commLoadFixture(t,
		filepath.Join(releaseFixtureDir, "calls", "get_impact_radius_tool__defaults.json"), root)
	want := commCanonical(t, ctxAutoDetectedChangedFiles)
	if diff := commDiff("changed_files", recorded.Structured["changed_files"], want); diff != "" {
		t.Fatalf("recorded auto-detected change set differs at %s", diff)
	}

	detected, err := releaseChangeSet(root, "HEAD~1")
	if err != nil {
		t.Fatalf("release change set: %v", err)
	}
	if diff := commDiff("changed_files", commCanonical(t, detected), want); diff != "" {
		t.Fatalf("native auto-detected change set differs at %s", diff)
	}
}

func ctxFixturePaths(t *testing.T) []string {
	t.Helper()
	var paths []string
	for _, tool := range ctxTools {
		matches, err := filepath.Glob(
			filepath.Join(releaseFixtureDir, "calls", tool+"__*.json"))
		if err != nil {
			t.Fatalf("glob %s: %v", tool, err)
		}
		paths = append(paths, matches...)
	}
	sort.Strings(paths)
	return paths
}

// ─── seeding ─────────────────────────────────────────────────────────────────

// ctxSeededEngine writes the recorded release graph into a real store under
// root, then stamps the build metadata the confidence markers read.
//
// Nodes and edges go through the published writer, so their persisted
// identities are the ones graphstore derives; the recorded id order is
// asserted because the payloads carry node and edge ids.
func ctxSeededEngine(t *testing.T, root string) *Engine {
	t.Helper()

	nodes, edges := loadReleaseGraph(t, root)
	engine := Open(root)
	t.Cleanup(func() { _ = engine.Close() })
	store, err := engine.writeStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	for index, node := range nodes {
		id, err := store.UpsertNode(graphstore.NodeInfo{
			Kind:       node.Kind,
			Name:       node.Name,
			FilePath:   node.FilePath,
			LineStart:  node.LineStart,
			LineEnd:    node.LineEnd,
			Language:   node.Language,
			ParentName: node.ParentName,
			Params:     node.Params,
			ReturnType: node.ReturnType,
			Modifiers:  node.Modifiers,
			IsTest:     node.IsTest == 1,
		}, "")
		if err != nil {
			t.Fatalf("upsert node %s: %v", node.Qualified, err)
		}
		if want := int64(index + 1); id != want {
			t.Fatalf("node %s got id %d, recorded id %d", node.Qualified, id, want)
		}
		stored, err := store.GetNode(node.Qualified)
		if err != nil || stored == nil {
			t.Fatalf("node %s is not retrievable by its recorded qualified name (err %v)",
				node.Qualified, err)
		}
	}
	for index, edge := range edges {
		id, err := store.UpsertEdge(graphstore.EdgeInfo{
			Kind:     edge.Kind,
			Source:   edge.Source,
			Target:   edge.Target,
			FilePath: edge.FilePath,
			Line:     edge.Line,
		})
		if err != nil {
			t.Fatalf("upsert edge %s -> %s: %v", edge.Source, edge.Target, err)
		}
		if want := int64(index + 1); id != want {
			t.Fatalf("edge %s -> %s got id %d, recorded id %d",
				edge.Source, edge.Target, id, want)
		}
	}

	// The graph the release recorded was built from this exact commit, after
	// these exact files were written. Both facts are load-bearing: the
	// confidence marker says "the graph is current" only when the build
	// commit matches live HEAD and no source file is newer than the build.
	head := runFixtureGit(t, root, "rev-parse", "HEAD")
	for key, value := range map[string]string{
		"git_head_sha": head,
		"git_branch":   "main",
		"last_updated": ctxBuildTime.Format(upstreamTimeLayout),
	} {
		if err := store.SetMetadata(key, value); err != nil {
			t.Fatalf("set metadata %s: %v", key, err)
		}
	}
	ctxBackdateSources(t, root)
	if err := store.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if got := headCommit(root); got != head {
		t.Fatalf("live HEAD reads %q, want %q — the currency check cannot be exercised", got, head)
	}
	return engine
}

// ctxBuildTime is the instant the seeded graph claims it was built at.
//
// It is a FIXED point, not `time.Now()`, because the staleness check compares
// a source file's mtime against a stamp that upstream writes with
// whole-second resolution using a strict `>`. Racing the real clock makes the
// confidence marker flip depending on where in the second the test lands.
var ctxBuildTime = time.Date(2030, 1, 1, 12, 0, 0, 0, time.Local)

// ctxBackdateSources moves every source file's mtime an hour behind the build,
// which is the state the release recorded its payloads in: a graph built after
// the files it indexed.
func ctxBackdateSources(t *testing.T, root string) {
	t.Helper()
	edited := ctxBuildTime.Add(-time.Hour)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		return os.Chtimes(path, edited, edited)
	})
	if err != nil {
		t.Fatalf("backdate sources under %s: %v", root, err)
	}
}

// ─── the estimator, pinned against Python ────────────────────────────────────

// TestEstimateTokensMatchesPythonJSONLength pins the token estimate to the
// byte length of Python's canonical JSON rendering.
//
// This is the machine-independent half of the context-savings contract, and
// the reason it is a test of its own: the recorded saved_tokens numbers are
// not portable, so nothing else would catch a refactor that swapped this
// encoder for encoding/json. That swap would change every payload's estimate
// at once, because encoding/json spells an integral float "1" where Python
// spells it "1.0", escapes `<`, `>` and `&`, and emits non-ASCII runes
// literally instead of as \uXXXX.
//
// Expected lengths were produced by, for each case's object:
//
//	json.dumps(obj, default=str, ensure_ascii=True,
//	           separators=(",", ":"), sort_keys=True)
//
// under the pinned release's interpreter.
func TestEstimateTokensMatchesPythonJSONLength(t *testing.T) {
	cases := []struct {
		name  string
		value any
		// json is Python's exact rendering; length is len(json).
		json string
	}{
		{"integral_float", 1.0, "1.0"},
		{"fractional_float", 0.4167, "0.4167"},
		{"empty_string_value", map[string]any{}, "{}"},
		{"int_list", []any{1, 2, 3}, "[1,2,3]"},
		{"nested_empty", map[string]any{"a": map[string]any{}, "b": []any{}}, `{"a":{},"b":[]}`},
		{
			"mixed",
			map[string]any{
				"b": 1.0,
				"a": "x",
				"z": []any{1, true, nil, 2.5},
				"n": map[string]any{"k": "é"},
				"e": "",
			},
			`{"a":"x","b":1.0,"e":"","n":{"k":"\u00e9"},"z":[1,true,null,2.5]}`,
		},
		{
			// The shapes these tools actually emit: an integral float that
			// must not collapse to an int, a node id that must not inflate to
			// a float, a JSON null, an empty array, exponent-form floats and
			// the characters encoding/json would escape differently.
			"payload",
			map[string]any{
				"status": "ok",
				"edges": []map[string]any{{
					"confidence":      1.0,
					"confidence_tier": "EXTRACTED",
					"id":              int64(29),
					"line":            4,
				}},
				"impacted_files": []string{},
				"parent_name":    nil,
				"score":          0.30000000000000004,
				"big":            1e16,
				"small":          1e-05,
				"escapes":        "a/b<c>&d\te\nf\"g\\h",
			},
			`{"big":1e+16,"edges":[{"confidence":1.0,"confidence_tier":"EXTRACTED","id":29,` +
				`"line":4}],"escapes":"a/b<c>&d\te\nf\"g\\h","impacted_files":[],` +
				`"parent_name":null,"score":0.30000000000000004,"small":1e-05,"status":"ok"}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctxAssertPythonJSONLength(t, tc.value, tc.json)
		})
	}
	ctxAssertEstimateTokensStringHandling(t)
}

// ctxAssertPythonJSONLength pins one value's Python rendering and the token
// count derived from that rendering's byte length, which is the only thing
// EstimateTokens is allowed to depend on.
func ctxAssertPythonJSONLength(t *testing.T, value any, wantJSON string) {
	t.Helper()
	if got := pyJSON(value); got != wantJSON {
		t.Fatalf("rendering =\n%s\nwant\n%s", got, wantJSON)
	}
	want := (len(wantJSON) + 3) / 4
	if got := EstimateTokens(value); got != want {
		t.Fatalf("EstimateTokens = %d, want %d (%d bytes)", got, want, len(wantJSON))
	}
}

// ctxAssertEstimateTokensStringHandling covers the inputs that never reach the
// encoder: a bare string is measured directly, so its non-ASCII counts
// unescaped, and an empty or nil value costs nothing at all.
func ctxAssertEstimateTokensStringHandling(t *testing.T) {
	t.Helper()
	// A string is measured directly rather than re-encoded, and non-ASCII is
	// counted in the escaped form only when it goes through the encoder.
	if got, want := EstimateTokens("café 😀"), (len("café 😀")+3)/4; got != want {
		t.Fatalf("EstimateTokens(string) = %d, want %d", got, want)
	}
	if got := pyJSON("café 😀"); got != `"caf\u00e9 \ud83d\ude00"` {
		t.Fatalf("string rendering = %s", got)
	}
	for _, empty := range []any{nil, "", map[string]any(nil)} {
		if empty == nil || empty == "" {
			if got := EstimateTokens(empty); got != 0 {
				t.Fatalf("EstimateTokens(%#v) = %d, want 0", empty, got)
			}
		}
	}
}

// TestPyFloatReprMatchesPythonRepr pins the float spelling across the boundaries
// Python's repr switches on: the mandatory ".0" on an integral value, the
// exponent thresholds at either end, and the digit string at full precision.
func TestPyFloatReprMatchesPythonRepr(t *testing.T) {
	// Each want is `repr(value)` under the pinned release's interpreter.
	cases := map[float64]string{
		0.0:                  "0.0",
		1.0:                  "1.0",
		2.5:                  "2.5",
		0.5:                  "0.5",
		0.4167:               "0.4167",
		0.2857:               "0.2857",
		0.0001:               "0.0001",
		1e-05:                "1e-05",
		9999999999999998.0:   "9999999999999998.0",
		1e16:                 "1e+16",
		1e17:                 "1e+17",
		1e100:                "1e+100",
		0.30000000000000004:  "0.30000000000000004",
		123456789012345.6:    "123456789012345.6",
		math.Copysign(0, -1): "-0.0",
		math.Inf(1):          "Infinity",
		math.Inf(-1):         "-Infinity",
	}
	for value, want := range cases {
		if got := pyFloatRepr(value); got != want {
			t.Fatalf("pyFloatRepr(%v) = %q, want %q", value, got, want)
		}
	}
	if got := pyFloatRepr(math.NaN()); got != "NaN" {
		t.Fatalf("pyFloatRepr(NaN) = %q, want %q", got, "NaN")
	}
}

// TestEstimateTokensRendersTypedStructsAsObjects is the arm that a payload
// built only from map[string]any never reaches.
//
// A handler's payload can carry a typed struct — the `_hints` block's
// next_steps are []hintSuggestion — and the transport renders it through its
// json tags. If the estimator rendered it as a Go string instead, every
// hint-carrying tool's saved_tokens would silently drift, and the drift is
// invisible to a payload comparison because the payload itself is identical.
func TestEstimateTokensRendersTypedStructsAsObjects(t *testing.T) {
	payload := map[string]any{"next_steps": []hintSuggestion{
		{Tool: "detect_changes", Suggestion: "see what changed"},
	}}
	const want = `{"next_steps":[{"suggestion":"see what changed","tool":"detect_changes"}]}`
	if got := pyJSON(payload); got != want {
		t.Fatalf("rendering =\n%s\nwant\n%s", got, want)
	}

	// The rendering must agree with what the transport emits, field for
	// field, or the estimate counts a payload nobody receives.
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var generic any
	if err := json.Unmarshal(encoded, &generic); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := pyJSON(generic); got != want {
		t.Fatalf("transport form renders as\n%s\nwant\n%s", got, want)
	}

	// Field types survive the projection: an int field stays an int rather
	// than being re-spelled as a float, which a round-trip through
	// encoding/json would do.
	type mixed struct {
		Count  int     `json:"count"`
		Score  float64 `json:"score"`
		Hidden string  `json:"-"`
		Absent string  `json:"absent,omitempty"`
	}
	if got, want := pyJSON(mixed{Count: 3, Score: 1.0, Hidden: "x"}),
		`{"count":3,"score":1.0}`; got != want {
		t.Fatalf("rendering = %s, want %s", got, want)
	}
}

// ─── node and edge projections ───────────────────────────────────────────────

// TestNodeToDictRendersUnsetColumnsAsNull is the trap this projection exists
// to avoid.
//
// graphstore.GraphNode flattens every nullable text column to "", while the
// release carries None and serialises it as JSON null. `parent_name` is the
// one the node projection publishes, and the fixtures show it null on every
// top-level symbol — so a handler emitting "" would differ from the release on
// the majority of nodes in every payload it returns, which is exactly the kind
// of difference a spot check misses.
func TestNodeToDictRendersUnsetColumnsAsNull(t *testing.T) {
	unset := NodeToDict(graphstore.GraphNode{
		ID: 5, Kind: "File", Name: "/repo/a.go", QualifiedName: "/repo/a.go",
		FilePath: "/repo/a.go", LineStart: 1, LineEnd: 31, Language: "go",
	})
	if got, ok := unset["parent_name"]; !ok || got != nil {
		t.Fatalf("parent_name = %#v (present %v), want a JSON null", got, ok)
	}
	encoded, err := json.Marshal(unset)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(encoded), `"parent_name":null`) {
		t.Fatalf("encoded node = %s, want parent_name null", encoded)
	}

	// A parent that IS set is published sanitized, not dropped.
	nested := NodeToDict(graphstore.GraphNode{
		ID: 7, Kind: "Function", Name: "Expired",
		QualifiedName: "/repo/a.go::Session.Expired", FilePath: "/repo/a.go",
		ParentName: "Session", Language: "go",
	})
	if nested["parent_name"] != "Session" {
		t.Fatalf("parent_name = %#v, want %q", nested["parent_name"], "Session")
	}

	// The release's node projection publishes exactly these fields. An extra
	// one would be a foreign key in every payload; a missing one would be a
	// hole in all of them.
	wantKeys := []string{
		"file_path", "id", "is_test", "kind", "language",
		"line_end", "line_start", "name", "parent_name", "qualified_name",
	}
	gotKeys := make([]string, 0, len(unset))
	for key := range unset {
		gotKeys = append(gotKeys, key)
	}
	sort.Strings(gotKeys)
	if strings.Join(gotKeys, ",") != strings.Join(wantKeys, ",") {
		t.Fatalf("node fields = %v, want %v", gotKeys, wantKeys)
	}
}

// TestSanitizeNameStripsControlsAndCountsRunes pins the two properties a
// byte-oriented implementation gets wrong: the cap counts CHARACTERS, and the
// control-character filter keeps tab and newline while dropping the rest.
func TestSanitizeNameStripsControlsAndCountsRunes(t *testing.T) {
	if got, want := SanitizeNameLimit("a\x00b\x1fc\td\ne", 32), "abc\td\ne"; got != want {
		t.Fatalf("SanitizeNameLimit = %q, want %q", got, want)
	}
	// Four 2-byte runes under a 3-character cap: a byte-based truncation
	// would keep one and a half runes.
	if got, want := SanitizeNameLimit("ééééé", 3), "ééé"; got != want {
		t.Fatalf("SanitizeNameLimit = %q, want %q", got, want)
	}
	long := strings.Repeat("é", sanitizeNameMaxLen+10)
	if got := SanitizeName(long); len([]rune(got)) != sanitizeNameMaxLen {
		t.Fatalf("SanitizeName kept %d runes, want %d", len([]rune(got)), sanitizeNameMaxLen)
	}
}

// TestEdgeToDictRepublishesAmbiguousTargets pins the bounded republication of
// an unresolved-target list: the cap is applied before the string filter, the
// count reports the UNTRUNCATED length, and the truncation flag is derived
// rather than trusted.
func TestEdgeToDictRepublishesAmbiguousTargets(t *testing.T) {
	targets := make([]any, 0, 25)
	for i := range 25 {
		targets = append(targets, fmt.Sprintf("pkg%d.Target", i))
	}
	dict := EdgeToDict(graphstore.GraphEdge{
		ID: 3, Kind: "CALLS", SourceQualified: "/repo/a.go::f",
		TargetQualified: "g", FilePath: "/repo/a.go", Line: 9,
		Confidence: 0.5, ConfidenceTier: "INFERRED",
		Extra: map[string]any{"ambiguous_targets": targets},
	})
	published, ok := dict["ambiguous_targets"].([]any)
	if !ok || len(published) != edgeTargetListCap {
		t.Fatalf("ambiguous_targets = %#v, want %d entries", dict["ambiguous_targets"], edgeTargetListCap)
	}
	if dict["ambiguous_target_count"] != 25 {
		t.Fatalf("ambiguous_target_count = %#v, want 25", dict["ambiguous_target_count"])
	}
	if dict["ambiguous_targets_truncated"] != true {
		t.Fatalf("ambiguous_targets_truncated = %#v, want true", dict["ambiguous_targets_truncated"])
	}
	// Confidence is passed through, never defaulted: both columns carry the
	// release's dataclass default as a SQL DEFAULT, so a fallback here would
	// mask a genuine low-confidence edge.
	if dict["confidence"] != 0.5 || dict["confidence_tier"] != "INFERRED" {
		t.Fatalf("confidence = %#v / %#v, want 0.5 / INFERRED",
			dict["confidence"], dict["confidence_tier"])
	}

	// A non-string entry is filtered out of the list but still counted, so
	// the count and the visible length disagree and the flag stays set.
	mixed := EdgeToDict(graphstore.GraphEdge{
		Extra: map[string]any{"unresolved_targets": []any{"a", 7, "b"}},
	})
	if got := mixed["unresolved_targets"].([]any); len(got) != 2 {
		t.Fatalf("unresolved_targets = %#v, want the two string entries", got)
	}
	if mixed["unresolved_target_count"] != 3 {
		t.Fatalf("unresolved_target_count = %#v, want 3", mixed["unresolved_target_count"])
	}
	if mixed["unresolved_targets_truncated"] != true {
		t.Fatalf("unresolved_targets_truncated = %#v, want true", mixed["unresolved_targets_truncated"])
	}

	// No list in `extra` publishes no keys at all rather than empty ones.
	plain := EdgeToDict(graphstore.GraphEdge{ID: 1, Kind: "CONTAINS"})
	for _, key := range []string{
		"ambiguous_targets", "ambiguous_target_count", "ambiguous_targets_truncated",
		"unresolved_targets", "unresolved_target_count", "unresolved_targets_truncated",
	} {
		if _, present := plain[key]; present {
			t.Fatalf("%s is present on an edge with no target list", key)
		}
	}
}

// ─── the relaxation's own rules ──────────────────────────────────────────────

// TestImpactRadiusTraversesByEdgeDirection pins the direction policy, which is
// the whole difference between this and an undirected BFS: a dependency edge
// propagates from its TARGET back to its source, TESTED_BY propagates forward,
// and CONTAINS does not propagate at all.
//
// The release fixture cannot reach this: its only cross-file call targets are
// bare names with no node rows, so every recorded blast radius is empty. The
// graph below is the minimum that resolves a call across files.
func TestImpactRadiusTraversesByEdgeDirection(t *testing.T) {
	root := t.TempDir()
	store, _ := openGraphStore(t)
	t.Cleanup(func() { _ = store.Close() })

	caller := filepath.ToSlash(filepath.Join(root, "caller.go"))
	callee := filepath.ToSlash(filepath.Join(root, "callee.go"))
	probe := filepath.ToSlash(filepath.Join(root, "callee_probe.go"))
	ctxSeedDirectionGraph(t, store, caller, callee, probe)
	ctxAssertImpactOneHop(t, store, caller, callee, probe)
	ctxAssertImpactBounds(t, store, callee)
}

// ctxSeedDirectionGraph writes the smallest graph whose cross-file call target
// RESOLVES to a node row: two files with a function each plus a test file, one
// CONTAINS edge per file, one CALLS edge from caller to callee and one
// TESTED_BY edge from callee to its probe.
func ctxSeedDirectionGraph(t *testing.T, store graphstore.Store, caller, callee, probe string) {
	t.Helper()
	for _, node := range []graphstore.NodeInfo{
		{Kind: "File", Name: caller, FilePath: caller, LineStart: 1, LineEnd: 9, Language: "go"},
		{Kind: "Function", Name: "callerFn", FilePath: caller, LineStart: 3, LineEnd: 5, Language: "go"},
		{Kind: "File", Name: callee, FilePath: callee, LineStart: 1, LineEnd: 9, Language: "go"},
		{Kind: "Function", Name: "calleeFn", FilePath: callee, LineStart: 3, LineEnd: 5, Language: "go"},
		{Kind: "File", Name: probe, FilePath: probe, LineStart: 1, LineEnd: 9, Language: "go"},
		{Kind: "Test", Name: "ProbeCallee", FilePath: probe, LineStart: 3, LineEnd: 5, Language: "go", IsTest: true},
	} {
		if _, err := store.UpsertNode(node, ""); err != nil {
			t.Fatalf("upsert node %s: %v", node.Name, err)
		}
	}
	for _, edge := range []graphstore.EdgeInfo{
		{Kind: "CONTAINS", Source: caller, Target: caller + "::callerFn", FilePath: caller, Line: 3},
		{Kind: "CONTAINS", Source: callee, Target: callee + "::calleeFn", FilePath: callee, Line: 3},
		{Kind: "CONTAINS", Source: probe, Target: probe + "::ProbeCallee", FilePath: probe, Line: 3},
		{Kind: "CALLS", Source: caller + "::callerFn", Target: callee + "::calleeFn", FilePath: caller, Line: 4},
		{Kind: "TESTED_BY", Source: callee + "::calleeFn", Target: probe + "::ProbeCallee", FilePath: probe, Line: 4},
	} {
		if _, err := store.UpsertEdge(edge); err != nil {
			t.Fatalf("upsert edge %s: %v", edge.Kind, err)
		}
	}
	if err := store.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

// ctxAssertImpactOneHop pins the membership, scores and ranking one hop out
// from the callee: its caller (CALLS backwards) and its test (TESTED_BY
// forwards). The callee's own File node is a seed, and CONTAINS would
// otherwise drag in the caller's File node too.
func ctxAssertImpactOneHop(t *testing.T, store graphstore.Store, caller, callee, probe string) {
	t.Helper()
	impact, err := ImpactRadius(store, []string{callee}, 1, impactMaxNodes)
	if err != nil {
		t.Fatalf("ImpactRadius: %v", err)
	}
	gotNames := make([]string, 0, len(impact.ImpactedNodes))
	for _, node := range impact.ImpactedNodes {
		gotNames = append(gotNames, node.QualifiedName)
	}
	wantNames := []string{caller + "::callerFn", probe + "::ProbeCallee"}
	sort.Strings(gotNames)
	sort.Strings(wantNames)
	if strings.Join(gotNames, ",") != strings.Join(wantNames, ",") {
		t.Fatalf("impacted = %v, want %v", gotNames, wantNames)
	}
	if impact.TotalImpacted != 2 || impact.Truncated {
		t.Fatalf("total %d truncated %v, want 2 / false", impact.TotalImpacted, impact.Truncated)
	}
	// CALLS weight 1.0 and TESTED_BY weight 0.7, each decayed once by 0.6.
	if got, want := impact.ImpactScores[caller+"::callerFn"], 0.6; got != want {
		t.Fatalf("caller score = %v, want %v", got, want)
	}
	if got, want := impact.ImpactScores[probe+"::ProbeCallee"], 0.42; got != want {
		t.Fatalf("test score = %v, want %v", got, want)
	}
	// Score order first, then qualified name — the caller outranks the test.
	if impact.ImpactedNodes[0].QualifiedName != caller+"::callerFn" {
		t.Fatalf("ranking puts %s first, want the higher-scored caller",
			impact.ImpactedNodes[0].QualifiedName)
	}
}

// ctxAssertImpactBounds pins the three traversals that return less than the
// full radius: the node ceiling still reports the untruncated total, zero
// depth seeds without propagating, and a file with no rows seeds nothing at
// all — the case the confidence marker exists to name.
func ctxAssertImpactBounds(t *testing.T, store graphstore.Store, callee string) {
	t.Helper()
	bounded, err := ImpactRadius(store, []string{callee}, 1, 1)
	if err != nil {
		t.Fatalf("ImpactRadius bounded: %v", err)
	}
	if len(bounded.ImpactedNodes) != 1 || bounded.TotalImpacted != 2 || !bounded.Truncated {
		t.Fatalf("bounded: %d shown, total %d, truncated %v, want 1 / 2 / true",
			len(bounded.ImpactedNodes), bounded.TotalImpacted, bounded.Truncated)
	}
	if zero, err := ImpactRadius(store, []string{callee}, 0, impactMaxNodes); err != nil {
		t.Fatalf("ImpactRadius depth 0: %v", err)
	} else if len(zero.ImpactedNodes) != 0 || len(zero.ChangedNodes) != 2 {
		t.Fatalf("depth 0: %d impacted, %d changed, want 0 / 2",
			len(zero.ImpactedNodes), len(zero.ChangedNodes))
	}
	if unknown, err := ImpactRadius(store, []string{"/nope/x.go"}, 2, impactMaxNodes); err != nil {
		t.Fatalf("ImpactRadius unindexed: %v", err)
	} else if len(unknown.ChangedNodes) != 0 || len(unknown.Edges) != 0 {
		t.Fatalf("unindexed file produced %d changed nodes and %d edges, want none",
			len(unknown.ChangedNodes), len(unknown.Edges))
	}
}

// ─── source snippets ─────────────────────────────────────────────────────────

// TestRelevantSourceLinesMergesWindowsAndTruncates pins the extraction the
// recorded max_lines_per_file case only reaches in its merged-single-range
// form: two separated changed nodes must produce two windows joined by an
// elision marker, and the shared budget must cut the tail with "...
// (truncated)" rather than silently dropping it.
func TestRelevantSourceLinesMergesWindowsAndTruncates(t *testing.T) {
	lines := make([]string, 40)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i+1)
	}
	nodes := []graphstore.GraphNode{
		{FilePath: "/repo/a.go", LineStart: 5, LineEnd: 6},
		{FilePath: "/repo/a.go", LineStart: 30, LineEnd: 31},
		{FilePath: "/repo/other.go", LineStart: 1, LineEnd: 40},
	}

	// The first node's window is indices 2..8 (line_start-3 .. line_end+2),
	// which prints as lines 3..8; the second node's is indices 27..33. With
	// room for six lines the first window fills the budget exactly and the
	// second is cut entirely. Expectations are the v2.3.8
	// _extract_relevant_lines output for this input.
	got := relevantSourceLines(lines, nodes, "/repo/a.go", 6)
	want := strings.Join([]string{
		"3: line 3", "4: line 4", "5: line 5", "6: line 6", "7: line 7", "8: line 8",
		"... (truncated)",
	}, "\n")
	if got != want {
		t.Fatalf("snippet =\n%s\nwant\n%s", got, want)
	}

	// With ample room both windows appear, separated by the elision marker.
	full := relevantSourceLines(lines, nodes, "/repo/a.go", 40)
	if !strings.Contains(full, "\n...\n") {
		t.Fatalf("snippet does not separate its two windows:\n%s", full)
	}
	if strings.Contains(full, "truncated") {
		t.Fatalf("snippet is marked truncated with room to spare:\n%s", full)
	}

	// No changed node in this file falls back to the head of the file, itself
	// bounded, so an unrelated file cannot contribute a whole file.
	fallback := relevantSourceLines(lines, nodes, "/repo/none.go", 4)
	if fallback != "1: line 1\n2: line 2\n3: line 3\n4: line 4" {
		t.Fatalf("fallback =\n%s", fallback)
	}
}

// TestSplitSourceLinesMatchesPythonSplitlines pins the line boundaries source
// numbering depends on. Splitting on "\n" alone would mis-number every line of
// a CRLF file and every line after a bare carriage return.
func TestSplitSourceLinesMatchesPythonSplitlines(t *testing.T) {
	cases := []struct {
		name string
		text string
		want []string
	}{
		{"unix_trailing", "a\nb\n", []string{"a", "b"}},
		{"unix_no_trailing", "a\nb", []string{"a", "b"}},
		{"crlf", "a\r\nb\r\n", []string{"a", "b"}},
		{"bare_cr", "a\rb", []string{"a", "b"}},
		{"blank_line", "a\n\nb", []string{"a", "", "b"}},
		{"empty", "", nil},
		{"form_feed", "a\fb", []string{"a", "b"}},
		{"next_line", "a\u0085b", []string{"a", "b"}},
		{"line_separator", "a\u2028b", []string{"a", "b"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := splitSourceLines(tc.text)
			if len(got) != len(tc.want) {
				t.Fatalf("split %q = %q, want %q", tc.text, got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("split %q = %q, want %q", tc.text, got, tc.want)
				}
			}
		})
	}
}

// TestReplaceInvalidUTF8MatchesPythonReplace pins the undecodable-byte
// rendering, which lands in the payload verbatim as part of a snippet.
//
// Python's errors="replace" applies Unicode's maximal-subpart rule, so the
// replacement COUNT is not one-per-byte and not one-per-invalid-run: a byte
// that cannot start a sequence stands alone, while a truncated-yet-valid
// prefix collapses into a single U+FFFD. strings.ToValidUTF8 gets the second
// case right and the first wrong; a naive per-byte loop does the reverse.
//
// Each want is `bytes.decode("utf-8", "replace")` under the pinned release's
// interpreter.
func TestReplaceInvalidUTF8MatchesPythonReplace(t *testing.T) {
	const bad = "\uFFFD"
	cases := []struct {
		name  string
		bytes string
		want  string
	}{
		{"valid_passes_through", "café 😀", "café 😀"},
		{"literal_replacement_char", bad + "x", bad + "x"},
		{"three_invalid_leads", "\xff\xff\xff", bad + bad + bad},
		{"truncated_prefix_then_stray", "\xe2(\xa1", bad + "(" + bad},
		{"truncated_four_byte", "\xf0\x9f", bad},
		{"truncated_four_byte_three", "\xf0\x9f\x98", bad},
		{"bad_continuation", "a\xc3(b", "a" + bad + "(b"},
		{"truncated_three_byte", "\xe2\x82", bad},
		{"surrogate", "\xed\xa0\x80", bad + bad + bad},
		{"overlong", "\xc0\x80", bad + bad},
		{"stray_continuations", "a\x80\x80b", "a" + bad + bad + "b"},
		{"above_max_rune", "\xf5\x80\x80\x80", bad + bad + bad + bad},
		{"latin1_byte", "caf\xe9 ok", "caf" + bad + " ok"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := replaceInvalidUTF8(tc.bytes); got != tc.want {
				t.Fatalf("replaceInvalidUTF8(%q) = %q (%d replacements), want %q (%d)",
					tc.bytes, got, strings.Count(got, bad),
					tc.want, strings.Count(tc.want, bad))
			}
		})
	}

	// The line numbering a snippet renders must survive a bad byte, which is
	// the reason this runs before splitting at all.
	if got := splitSourceLines("a\xffb\nc\n"); len(got) != 2 ||
		got[0] != "a"+bad+"b" || got[1] != "c" {
		t.Fatalf("splitSourceLines over an undecodable byte = %q", got)
	}
}

// ─── confidence markers ──────────────────────────────────────────────────────

// TestConfidenceNotesStayWithinBudget pins the character budget and the
// target-preserving truncation. A marker exists to save an agent a
// multi-thousand-token fallback search, so one that grew unbounded would
// invert its own justification.
func TestConfidenceNotesStayWithinBudget(t *testing.T) {
	long := strings.Repeat("d/", 200) + "verylongfile.go::TheSymbol"
	note := NotIndexedNote(long)
	if length := len([]rune(note)); length > confidenceMaxChars {
		t.Fatalf("note is %d characters, over the %d budget:\n%s",
			length, confidenceMaxChars, note)
	}
	// Truncation keeps the half that identifies the target: the bare symbol,
	// not a directory prefix.
	if !strings.Contains(note, "TheSymbol") {
		t.Fatalf("note dropped the symbol it is about:\n%s", note)
	}

	// A crafted name cannot forge extra advisory lines.
	forged := ConfidenceNote("first line\nsecond: line\twith\ttabs")
	if strings.ContainsAny(forged, "\n\t") {
		t.Fatalf("note carries raw whitespace control characters: %q", forged)
	}

	// The language-gap table is scoped to the patterns each gap affects.
	if got := LanguageGapNote("go", ImpactGapPattern); got != "" {
		t.Fatalf("go has no impact-radius gap, got %q", got)
	}
	if LanguageGapNote("PHP ", ImpactGapPattern) == "" {
		t.Fatalf("php's import gap applies to the impact radius, got no note")
	}
	if LanguageGapNote("go", "inheritors_of") == "" {
		t.Fatalf("go's structural-interface gap applies to inheritors_of, got no note")
	}
}

// TestGraphStalenessDistinguishesUnverifiedFromCurrent pins the second return,
// which decides between the confident and the hedged marker wording. Claiming
// "the graph is current" without having checked would be exactly the
// misleading freshness signal these markers exist to avoid.
//
// The stamp is written in upstream's own layout — naive LOCAL time at
// whole-second resolution — because that is what the release writes and what
// the comparison has to be valid against. An offset-bearing UTC stamp would
// pass here while being wrong in production by the whole UTC offset.
func TestGraphStalenessDistinguishesUnverifiedFromCurrent(t *testing.T) {
	root := materializeFixtureRepo(t)
	store, _ := openGraphStore(t)
	t.Cleanup(func() { _ = store.Close() })
	source := filepath.Join(root, "pkg", "auth", "auth.go")

	// No build metadata at all: nothing was checked, so nothing is claimed.
	if note, current := GraphStaleness(store, root, source); note != "" || current {
		t.Fatalf("bare store: note %q current %v, want no note and unverified", note, current)
	}

	head := runFixtureGit(t, root, "rev-parse", "HEAD")
	if err := store.SetMetadata("git_head_sha", head); err != nil {
		t.Fatalf("set head sha: %v", err)
	}
	if err := store.SetMetadata("last_updated", ctxBuildTime.Format(upstreamTimeLayout)); err != nil {
		t.Fatalf("set last_updated: %v", err)
	}
	if err := store.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	ctxBackdateSources(t, root)
	if note, current := GraphStaleness(store, root, source); note != "" || !current {
		t.Fatalf("current graph: note %q current %v, want no note and verified", note, current)
	}

	// A source file newer than the build is stale even though the commit
	// matches — which is the case a commit comparison alone cannot see.
	edited := ctxBuildTime.Add(time.Hour)
	if err := os.Chtimes(source, edited, edited); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	note, current := GraphStaleness(store, root, source)
	if note == "" || current {
		t.Fatalf("edited source: note %q current %v, want a staleness note", note, current)
	}
	if !strings.Contains(note, "auth.go") || !strings.Contains(note, updateHint) {
		t.Fatalf("staleness note names neither the file nor the remedy: %q", note)
	}

	// A build commit that is not HEAD is stale regardless of mtimes, and
	// outranks the mtime signal.
	if err := store.SetMetadata("git_head_sha", strings.Repeat("0", 40)); err != nil {
		t.Fatalf("set stale sha: %v", err)
	}
	if err := store.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if note, current := GraphStaleness(store, root, ""); !strings.Contains(note, "older commit") || current {
		t.Fatalf("stale commit: note %q current %v, want the commit staleness note", note, current)
	}
}

// ─── the no-change and budget arms ───────────────────────────────────────────

// TestContextToolsReportNoChangesWithoutAPayload pins the early returns, which
// carry NO context_savings and, for the review context, an empty `context`
// object rather than a populated one.
//
// Both arms are reached two ways that must agree: an explicitly EMPTY
// changed_files list, and an omitted one that auto-detection cannot fill.
// Expected payloads are the v2.3.8 responses for a repository with no diff.
func TestContextToolsReportNoChangesWithoutAPayload(t *testing.T) {
	engine := Open(t.TempDir())
	t.Cleanup(func() { _ = engine.Close() })

	wantImpact := map[string]any{
		"status":         "ok",
		"summary":        "No changed files detected.",
		"changed_nodes":  []any{},
		"impacted_nodes": []any{},
		"impacted_files": []any{},
		"truncated":      false,
		"total_impacted": float64(0),
	}
	wantReview := map[string]any{
		"status":  "ok",
		"summary": "No changes detected. Nothing to review.",
		"context": map[string]any{},
	}

	for _, tc := range []struct {
		name string
		tool string
		args map[string]any
		want map[string]any
	}{
		{"impact_explicit_empty", "get_impact_radius_tool",
			map[string]any{"changed_files": []string{}}, wantImpact},
		{"impact_auto_detect_finds_nothing", "get_impact_radius_tool",
			map[string]any{}, wantImpact},
		{"review_explicit_empty", "get_review_context_tool",
			map[string]any{"changed_files": []string{}}, wantReview},
		{"review_auto_detect_finds_nothing", "get_review_context_tool",
			map[string]any{}, wantReview},
	} {
		t.Run(tc.name, func(t *testing.T) {
			encoded, err := json.Marshal(tc.args)
			if err != nil {
				t.Fatalf("marshal arguments: %v", err)
			}
			tool, _ := crgrelease.Lookup(tc.tool)
			args, bindErr := tool.Bind(encoded)
			if bindErr != nil {
				t.Fatalf("bind: %v", bindErr)
			}
			got, err := toolHandlers[tc.tool](engine, args)
			if err != nil {
				t.Fatalf("handler: %v", err)
			}
			if diff := commDiff("", commCanonical(t, got), commCanonical(t, tc.want)); diff != "" {
				t.Fatalf("payload mismatch at %s", diff)
			}
		})
	}
}

// TestReviewContextSourceSnippetsShareALineBudget pins the guard that keeps a
// whole-repo diff from inlining the repository.
//
// The per-file limit alone does not bound the response: N files each under it
// still contribute N whole files, which is how snippets came to be 109k
// tokens of a 134k-token worst case. Three 400-line files against the shared
// 800-line budget must yield two snippets, not three, and the payload must
// SAY it was cut — the recorded fixtures cannot reach this because every file
// in the fixture repository is under 40 lines.
//
// The expected split (a.go and b.go in full, c.go dropped,
// source_truncated and truncated both set) is the v2.3.8 response for this
// input.
func TestReviewContextSourceSnippetsShareALineBudget(t *testing.T) {
	root := t.TempDir()
	names := []string{"a.go", "b.go", "c.go"}
	for _, name := range names {
		body := []string{"package big", ""}
		for i := range 397 {
			body = append(body, fmt.Sprintf("// filler %d", i))
		}
		body = append(body, "func F"+strings.ToUpper(name[:1])+"() {}")
		if err := os.WriteFile(filepath.Join(root, name),
			[]byte(strings.Join(body, "\n")+"\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	context := map[string]any{}
	attachSourceSnippets(context, root, names, nil, reviewCtxMaxLinesPerFile)

	snippets, ok := context["source_snippets"].(map[string]any)
	if !ok {
		t.Fatalf("source_snippets is %T, want an object", context["source_snippets"])
	}
	if len(snippets) != 2 {
		t.Fatalf("emitted %d snippets, want 2 — the third file must not fit the budget", len(snippets))
	}
	for _, name := range names[:2] {
		text, present := snippets[name].(string)
		if !present {
			t.Fatalf("%s is missing from the snippets", name)
		}
		if lines := strings.Count(text, "\n") + 1; lines != 400 {
			t.Fatalf("%s rendered %d lines, want its whole 400", name, lines)
		}
	}
	if _, present := snippets["c.go"]; present {
		t.Fatalf("c.go was emitted past the exhausted budget")
	}
	if context["source_truncated"] != true || context["truncated"] != true {
		t.Fatalf("source_truncated %v / truncated %v, want both set — an unannounced "+
			"cut is the failure this budget exists to make visible",
			context["source_truncated"], context["truncated"])
	}

	// A file the budget DOES fit is numbered from one, so a reviewer can map
	// the snippet back onto the file.
	first := snippets["a.go"].(string)
	if !strings.HasPrefix(first, "1: package big\n2: \n3: // filler 0\n") {
		t.Fatalf("a.go snippet starts:\n%s", first[:min(80, len(first))])
	}
}
