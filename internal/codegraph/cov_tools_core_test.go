package codegraph

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/crgrelease"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// ── shared helpers ───────────────────────────────────────────────────────────

// covCoreBuiltAt is the naive local build timestamp the release writes into
// `last_updated`, and covCoreNow is a clock pinned 90 seconds after it, so
// every asserted `age_seconds` is exact rather than approximate.
const covCoreBuiltAt = "2024-05-06T07:08:09"

var covCoreNow = time.Date(2024, time.May, 6, 7, 9, 39, 0, time.Local)

// covCoreNoopHandler is a handler RegisterTool must never install. Both
// registration guards reject their name before touching the map, so a test
// that reaches the map at all has found a real regression.
func covCoreNoopHandler(*Engine, crgrelease.Args) (any, error) { return nil, nil }

// covCoreRecoverPanic runs fn and returns the panic value's message, failing
// the test when fn returns normally.
func covCoreRecoverPanic(t *testing.T, fn func()) string {
	t.Helper()
	var message string
	func() {
		defer func() {
			recovered := recover()
			if recovered == nil {
				t.Fatal("want a panic, got a normal return")
			}
			message = fmt.Sprint(recovered)
		}()
		fn()
	}()
	return message
}

// covCoreBindArgs validates an argument map through the pinned release's own
// schema, so a handler is driven with exactly the values a real tools/call
// delivers, including the defaults the schema fills in.
func covCoreBindArgs(t *testing.T, tool string, args map[string]any) crgrelease.Args {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("encode %s arguments: %v", tool, err)
	}
	definition, ok := crgrelease.Lookup(tool)
	if !ok {
		t.Fatalf("%s is not published by code-review-graph %s", tool, crgrelease.Version)
	}
	bound, bindErr := definition.Bind(raw)
	if bindErr != nil {
		t.Fatalf("bind %s arguments %s: %v", tool, raw, bindErr)
	}
	return bound
}

// covCoreBridgedTool is a tool the pinned release publishes that this package
// does not implement natively — exactly the input the bridge-routing arm of
// CallTool exists for.
func covCoreBridgedTool(t *testing.T) string {
	t.Helper()
	native := map[string]bool{}
	for _, name := range NativeToolNames() {
		native[name] = true
	}
	for _, name := range crgrelease.ToolNames() {
		if !native[name] {
			return name
		}
	}
	t.Fatal("every published tool now has a native handler; this test needs a new input")
	return ""
}

// covCoreProvenanceEngine is an engine whose persistence is a fake carrying
// the given build-metadata rows, with the clock pinned to covCoreNow.
func covCoreProvenanceEngine(t *testing.T, meta map[string]string) *Engine {
	t.Helper()
	e := engineWithStore(t, writeFixture(t), &fakeStore{meta: meta})
	e.now = func() time.Time { return covCoreNow }
	return e
}

// covCoreAssertEnvelope compares a `_graph` provenance block field by field and
// reports any key the engine invented or dropped.
func covCoreAssertEnvelope(t *testing.T, got, want map[string]any) {
	t.Helper()
	for key, wantValue := range want {
		gotValue, present := got[key]
		if !present {
			t.Errorf("_graph is missing %q (want %v)", key, wantValue)
			continue
		}
		if gotValue != wantValue {
			t.Errorf("_graph[%q] = %v (%T), want %v", key, gotValue, gotValue, wantValue)
		}
	}
	for key := range got {
		if _, expected := want[key]; !expected {
			t.Errorf("_graph carries unexpected field %q = %v", key, got[key])
		}
	}
}

// ── tools.go: native registration ────────────────────────────────────────────

// TestCovCoreRegisterToolRejectsUnroutableNames pins the startup guard. A typo
// or a double registration must abort the binary: the alternative is a tool
// that silently stays on the retained bridge (typo) or one whose handler was
// replaced by whichever package initialized last (duplicate).
func TestCovCoreRegisterToolRejectsUnroutableNames(t *testing.T) {
	registered := strings.Join(NativeToolNames(), ",")
	duplicate := NativeToolNames()[0]

	for _, tc := range []struct {
		name string
		tool string
		want []string
	}{
		{
			name: "unpublished_name",
			tool: "covcore_not_a_release_tool",
			want: []string{"covcore_not_a_release_tool", "is not published", crgrelease.Version},
		},
		{
			name: "duplicate_handler",
			tool: duplicate,
			want: []string{"duplicate native handler", duplicate},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			message := covCoreRecoverPanic(t, func() { RegisterTool(tc.tool, covCoreNoopHandler) })
			for _, fragment := range tc.want {
				if !strings.Contains(message, fragment) {
					t.Errorf("panic %q does not mention %q", message, fragment)
				}
			}
		})
	}

	if after := strings.Join(NativeToolNames(), ","); after != registered {
		t.Fatalf("a rejected registration mutated the native surface:\n got %s\nwant %s", after, registered)
	}
}

// TestCovCoreNativeToolNamesIsTheSortedNativeSurface asserts the list the MCP
// server splits its routing on: it must name every natively handled tool,
// nothing else, and in sorted order.
func TestCovCoreNativeToolNamesIsTheSortedNativeSurface(t *testing.T) {
	names := NativeToolNames()
	if len(names) != len(toolHandlers) {
		t.Fatalf("NativeToolNames reported %d tools, %d are registered", len(names), len(toolHandlers))
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("NativeToolNames is not sorted: %v", names)
	}
	for _, name := range names {
		if _, ok := toolHandlers[name]; !ok {
			t.Errorf("%q is reported as native but has no handler", name)
		}
		if _, ok := crgrelease.Lookup(name); !ok {
			t.Errorf("%q is reported as native but is not published by the release", name)
		}
	}
}

// ── tools.go: CallTool ───────────────────────────────────────────────────────

// TestCovCoreCallToolRoutesUnhandledToolsToTheBridge pins the routing
// contract: an unhandled tool is refused with graphstore.ErrToolNotNative so
// the MCP server forwards the call, rather than answered from a graph the
// native engine never computed for it.
func TestCovCoreCallToolRoutesUnhandledToolsToTheBridge(t *testing.T) {
	engine := covCoreProvenanceEngine(t, map[string]string{"last_updated": covCoreBuiltAt})

	for _, tc := range []struct {
		name string
		tool string
	}{
		{name: "published_but_not_native", tool: covCoreBridgedTool(t)},
		{name: "unpublished", tool: "covcore_unknown_tool"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := engine.CallTool(tc.tool, crgrelease.Args{})
			if !errors.Is(err, graphstore.ErrToolNotNative) {
				t.Fatalf("err = %v, want %v", err, graphstore.ErrToolNotNative)
			}
			if !strings.Contains(err.Error(), tc.tool) {
				t.Errorf("error %q does not name the refused tool", err)
			}
			if result != nil {
				t.Errorf("a refused call returned a payload: %#v", result)
			}
		})
	}
}

// TestCovCoreCallToolPropagatesHandlerFailure pins the other half of that
// envelope: a handler's error is a TRANSPORT failure with no payload, and it
// must not be confused with the bridge-routing refusal.
func TestCovCoreCallToolPropagatesHandlerFailure(t *testing.T) {
	engine := covCoreProvenanceEngine(t, map[string]string{"last_updated": covCoreBuiltAt})
	args := covCoreBindArgs(t, "query_graph_tool", map[string]any{
		"pattern":     "callers",
		"target":      "Greet",
		"max_results": 0,
	})

	result, err := engine.CallTool("query_graph_tool", args)
	if err == nil {
		t.Fatalf("a rejected bound returned a payload: %#v", result)
	}
	if errors.Is(err, graphstore.ErrToolNotNative) {
		t.Fatalf("a handler failure was reported as not-native: %v", err)
	}
	if !strings.Contains(err.Error(), "max_results") {
		t.Errorf("error %q does not name the rejected bound", err)
	}
	if result != nil {
		t.Errorf("a failed call returned a payload: %#v", result)
	}
}

// TestCovCoreCallToolStampsProvenanceOnANativeAnswer is the freshness contract
// a client depends on: a natively served payload carries the `_graph` envelope
// describing WHICH graph answered and how stale it is, attached around the
// handler rather than by it.
func TestCovCoreCallToolStampsProvenanceOnANativeAnswer(t *testing.T) {
	engine := covCoreProvenanceEngine(t, map[string]string{"last_updated": covCoreBuiltAt})

	result, err := engine.CallTool("list_graph_stats_tool", covCoreBindArgs(t, "list_graph_stats_tool", nil))
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	payload, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("want a dict result, got %T", result)
	}
	envelope, ok := payload["_graph"].(map[string]any)
	if !ok {
		t.Fatalf("want a _graph provenance block, got %#v", payload["_graph"])
	}
	covCoreAssertEnvelope(t, envelope, map[string]any{
		"updated_at":  covCoreBuiltAt,
		"age_seconds": 90,
	})
}

// ── tools.go: provenance ─────────────────────────────────────────────────────

// TestCovCoreWithProvenanceLeavesForeignResultsAlone covers the two results
// the envelope must not touch: a non-dict payload (the release attaches
// provenance to dicts only) and a payload that already carries its own block,
// which a second envelope would overwrite.
func TestCovCoreWithProvenanceLeavesForeignResultsAlone(t *testing.T) {
	engine := covCoreProvenanceEngine(t, map[string]string{"last_updated": covCoreBuiltAt})

	t.Run("non_dict_result", func(t *testing.T) {
		got := engine.withProvenance([]string{"lib/lib.go"})
		list, ok := got.([]string)
		if !ok {
			t.Fatalf("a list result came back as %T", got)
		}
		if len(list) != 1 || list[0] != "lib/lib.go" {
			t.Errorf("list result = %v, want it unchanged", list)
		}
	})

	t.Run("existing_envelope", func(t *testing.T) {
		payload := map[string]any{"_graph": "handler-owned", "status": "success"}
		got, ok := engine.withProvenance(payload).(map[string]any)
		if !ok {
			t.Fatalf("a dict result came back as %T", got)
		}
		if got["_graph"] != "handler-owned" {
			t.Errorf("_graph = %v, want the handler's own block preserved", got["_graph"])
		}
	})

	t.Run("unprovable_graph", func(t *testing.T) {
		blank := engineWithStore(t, writeFixture(t), &fakeStore{})
		got, ok := blank.withProvenance(map[string]any{"status": "success"}).(map[string]any)
		if !ok {
			t.Fatalf("a dict result came back as %T", got)
		}
		if _, present := got["_graph"]; present {
			t.Errorf("_graph = %v, want no envelope when nothing about the graph is known", got["_graph"])
		}
	})
}

// TestCovCoreGraphProvenanceReportsOnlyProvableFields pins the envelope's
// honesty rules. Every field is a claim about the graph a client may act on,
// so a field that cannot be proven is OMITTED rather than defaulted — and a
// build with no recorded commit yields no commit fields at all.
func TestCovCoreGraphProvenanceReportsOnlyProvableFields(t *testing.T) {
	for _, tc := range []struct {
		name   string
		engine func(t *testing.T) *Engine
		want   map[string]any
	}{
		{
			name:   "unbuilt_graph",
			engine: func(t *testing.T) *Engine { return Open(t.TempDir()) },
		},
		{
			name:   "unreadable_graph",
			engine: unreadableEngine,
		},
		{
			name: "metadata_read_fails",
			engine: func(t *testing.T) *Engine {
				return engineWithStore(t, writeFixture(t), &fakeStore{metaErr: errFake})
			},
		},
		{
			name:   "no_metadata_rows",
			engine: func(t *testing.T) *Engine { return covCoreProvenanceEngine(t, nil) },
		},
		{
			name: "branch_without_a_build_commit",
			engine: func(t *testing.T) *Engine {
				return covCoreProvenanceEngine(t, map[string]string{"git_branch": "main"})
			},
		},
		{
			name: "build_time_without_a_build_commit",
			engine: func(t *testing.T) *Engine {
				return covCoreProvenanceEngine(t, map[string]string{"last_updated": covCoreBuiltAt})
			},
			want: map[string]any{"updated_at": covCoreBuiltAt, "age_seconds": 90},
		},
		{
			name: "unparsable_build_time",
			engine: func(t *testing.T) *Engine {
				return covCoreProvenanceEngine(t, map[string]string{"last_updated": "yesterday"})
			},
			want: map[string]any{"updated_at": "yesterday"},
		},
		{
			name: "build_time_in_the_future",
			engine: func(t *testing.T) *Engine {
				ahead := covCoreNow.Add(time.Hour).Format("2006-01-02T15:04:05")
				return covCoreProvenanceEngine(t, map[string]string{"last_updated": ahead})
			},
			want: map[string]any{
				"updated_at":  covCoreNow.Add(time.Hour).Format("2006-01-02T15:04:05"),
				"age_seconds": 0,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.engine(t).graphProvenance()
			if tc.want == nil {
				if got != nil {
					t.Fatalf("provenance = %v, want no envelope at all", got)
				}
				return
			}
			covCoreAssertEnvelope(t, got, tc.want)
		})
	}
}

// TestCovCoreGraphProvenanceNamesTheBuildCommit covers the fields that need a
// real repository: the commit the graph was built from, its branch, the live
// HEAD, and the drift flag between them. `head_matches_build` is a claim about
// COMMITS only, so it must be true for a HEAD equal to the build commit and
// false for any other, which is the signal a client uses to decide whether a
// native answer can be trusted.
func TestCovCoreGraphProvenanceNamesTheBuildCommit(t *testing.T) {
	root := initGitRepo(t)
	live := headCommit(root)
	if live == "" {
		t.Fatal("the fixture repository has no HEAD commit")
	}

	for _, tc := range []struct {
		name      string
		builtFrom string
		match     bool
	}{
		{name: "head_is_the_build_commit", builtFrom: live, match: true},
		{name: "head_moved_past_the_build", builtFrom: strings.Repeat("0", len(live)), match: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeStore{meta: map[string]string{
				"last_updated": covCoreBuiltAt,
				"git_head_sha": tc.builtFrom,
				"git_branch":   "fixture-branch",
			}}
			engine := engineWithStore(t, root, store)
			engine.now = func() time.Time { return covCoreNow }

			covCoreAssertEnvelope(t, engine.graphProvenance(), map[string]any{
				"updated_at":         covCoreBuiltAt,
				"age_seconds":        90,
				"built_at_sha":       tc.builtFrom,
				"built_on_branch":    "fixture-branch",
				"head_sha":           live,
				"head_matches_build": tc.match,
			})
		})
	}
}

// TestCovCoreParseGraphTimestampAcceptsBothRecordedShapes pins the two formats
// the envelope's age is computed from. The release writes a NAIVE local
// timestamp, so reading it as UTC would misreport staleness by the host's
// offset; an RFC3339 value must keep its own offset.
func TestCovCoreParseGraphTimestampAcceptsBothRecordedShapes(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
		want  time.Time
	}{
		{
			name:  "naive_local",
			value: covCoreBuiltAt,
			want:  time.Date(2024, time.May, 6, 7, 8, 9, 0, time.Local),
		},
		{
			name:  "rfc3339_utc",
			value: "2024-05-06T07:08:09Z",
			want:  time.Date(2024, time.May, 6, 7, 8, 9, 0, time.UTC),
		},
		{
			name:  "rfc3339_offset",
			value: "2024-05-06T07:08:09+02:00",
			want:  time.Date(2024, time.May, 6, 5, 8, 9, 0, time.UTC),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseGraphTimestamp(tc.value)
			if err != nil {
				t.Fatalf("parseGraphTimestamp(%q): %v", tc.value, err)
			}
			if !got.Equal(tc.want) {
				t.Errorf("parseGraphTimestamp(%q) = %s, want %s", tc.value, got, tc.want)
			}
		})
	}

	for _, value := range []string{"", "yesterday", "2024-05-06", "2024-05-06 07:08:09"} {
		if got, err := parseGraphTimestamp(value); err == nil {
			t.Errorf("parseGraphTimestamp(%q) = %s, want an error", value, got)
		}
	}
}

// ── tools.go: response vocabulary ────────────────────────────────────────────

// TestCovCoreErrorResponseIsASuccessfulPayload pins the release's error shape:
// a failure the client can read, not a transport error. `summary` repeats the
// message because the release's clients render that field.
func TestCovCoreErrorResponseIsASuccessfulPayload(t *testing.T) {
	t.Run("bare", func(t *testing.T) {
		got := ErrorResponse("graph not built", nil)
		want := map[string]any{
			"status":  "error",
			"error":   "graph not built",
			"summary": "graph not built",
		}
		covCoreAssertEnvelope(t, got, want)
	})

	t.Run("with_extra_fields", func(t *testing.T) {
		got := ErrorResponse("no match", map[string]any{"candidates": 0, "status": "not_found"})
		want := map[string]any{
			// An extra field is applied AFTER the defaults, so a caller can
			// refine `status` for a failure the release reports differently.
			"status":     "not_found",
			"error":      "no match",
			"summary":    "no match",
			"candidates": 0,
		}
		covCoreAssertEnvelope(t, got, want)
	})
}

// TestCovCoreBoundedAlwaysReportsTheUntruncatedTotal is the bounding contract:
// a caller that sees a short list must still learn how many results existed,
// and a non-positive effective limit shows nothing while still reporting the
// total it withheld.
func TestCovCoreBoundedAlwaysReportsTheUntruncatedTotal(t *testing.T) {
	five := []string{"a", "b", "c", "d", "e"}

	for _, tc := range []struct {
		name          string
		items         []string
		maxResults    int
		hardCap       int
		wantVisible   string
		wantTotal     int
		wantTruncated bool
	}{
		{name: "under_limit", items: five[:3], maxResults: 10, hardCap: 10, wantVisible: "a,b,c", wantTotal: 3},
		{name: "exactly_at_limit", items: five[:3], maxResults: 3, hardCap: 10, wantVisible: "a,b,c", wantTotal: 3},
		{
			name: "cut_by_max_results", items: five, maxResults: 2, hardCap: 10,
			wantVisible: "a,b", wantTotal: 5, wantTruncated: true,
		},
		{
			name: "cut_by_hard_cap", items: five, maxResults: 10, hardCap: 2,
			wantVisible: "a,b", wantTotal: 5, wantTruncated: true,
		},
		{
			name: "negative_max_results", items: five, maxResults: -1, hardCap: 10,
			wantVisible: "", wantTotal: 5, wantTruncated: true,
		},
		{
			name: "negative_hard_cap", items: five, maxResults: 10, hardCap: -3,
			wantVisible: "", wantTotal: 5, wantTruncated: true,
		},
		{name: "nothing_to_bound", items: nil, maxResults: -1, hardCap: -1, wantVisible: "", wantTotal: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			visible, total, truncated := Bounded(tc.items, tc.maxResults, tc.hardCap)
			if got := strings.Join(visible, ","); got != tc.wantVisible {
				t.Errorf("visible = %q, want %q", got, tc.wantVisible)
			}
			if total != tc.wantTotal {
				t.Errorf("total = %d, want %d", total, tc.wantTotal)
			}
			if truncated != tc.wantTruncated {
				t.Errorf("truncated = %v, want %v", truncated, tc.wantTruncated)
			}
		})
	}
}

// ── tools.go: source-coverage routing ────────────────────────────────────────

// TestCovCoreSourcesFullyNativeNamesWhatItCannotExtract pins the predicate the
// MCP server routes every call on. The BOOLEAN is the decision — a repository
// with nothing but Go is served natively, and one file upstream indexes and
// the native scanner cannot extract sends the whole repository to the bridge.
// The list is the diagnostic detail behind it.
func TestCovCoreSourcesFullyNativeNamesWhatItCannotExtract(t *testing.T) {
	for _, tc := range []struct {
		name            string
		files           []string
		wantFullyNative bool
		wantNonNative   string
	}{
		{name: "go_only", files: []string{"main.go", "lib/lib.go"}, wantFullyNative: true},
		{
			name: "go_and_python", files: []string{"main.go", "tool.py"},
			wantNonNative: "python",
		},
		{name: "python_only", files: []string{"tool.py"}, wantNonNative: "python"},
		{
			name: "unindexed_only", files: []string{"notes.txt"},
			wantNonNative: LanguageUnindexed,
		},
		{
			// Files upstream does not index cannot force the bridge, so the
			// verdict stays native even though the detail names the bucket.
			name: "go_and_unindexed", files: []string{"main.go", "notes.txt"},
			wantFullyNative: true, wantNonNative: LanguageUnindexed,
		},
		{
			name: "go_python_and_unindexed", files: []string{"main.go", "tool.py", "notes.txt"},
			wantNonNative: "python," + LanguageUnindexed,
		},
		{name: "empty_repository", files: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine := Open(capabilityRepo(t, tc.files...))
			fullyNative, nonNative, err := engine.SourcesFullyNative()
			if err != nil {
				t.Fatalf("SourcesFullyNative: %v", err)
			}
			if fullyNative != tc.wantFullyNative {
				t.Errorf("fullyNative = %v, want %v", fullyNative, tc.wantFullyNative)
			}
			if got := strings.Join(nonNative, ","); got != tc.wantNonNative {
				t.Errorf("nonNative = %q, want %q", got, tc.wantNonNative)
			}
		})
	}
}

// TestCovCoreSourcesFullyNativeSurfacesAScanFailure proves an unscannable root
// is an ERROR rather than a "fully native" verdict: the MCP server treats an
// undetermined repository as bridge work, which it can only do if the failure
// reaches it.
func TestCovCoreSourcesFullyNativeSurfacesAScanFailure(t *testing.T) {
	engine := Open(filepath.Join(t.TempDir(), "absent"))
	fullyNative, nonNative, err := engine.SourcesFullyNative()
	if err == nil {
		t.Fatalf("a missing root reported fullyNative=%v, nonNative=%v", fullyNative, nonNative)
	}
	if fullyNative {
		t.Error("a failed scan claimed the repository is fully native")
	}
	if nonNative != nil {
		t.Errorf("nonNative = %v, want nothing alongside the failure", nonNative)
	}
}

// ── capability.go: the pinned contract loader ────────────────────────────────

// TestCovCoreLanguageContractLoaderRejectsAMissingCopy pins the startup guard
// on the embedded inventory: without it a build that failed to embed the
// contract would answer every routing question from an EMPTY language table,
// declaring every repository fully native and serving graphs that silently
// omit most of the tree.
func TestCovCoreLanguageContractLoaderRejectsAMissingCopy(t *testing.T) {
	original := languageContractFS
	t.Cleanup(func() { languageContractFS = original })
	languageContractFS = embed.FS{}

	message := covCoreRecoverPanic(t, func() { _ = mustLanguageContract() })
	for _, fragment := range []string{embeddedLanguages, "missing"} {
		if !strings.Contains(message, fragment) {
			t.Errorf("panic %q does not mention %q", message, fragment)
		}
	}
}

// ── capability.go: walk classification arms ──────────────────────────────────

// covCoreWalkEntry is the fs.DirEntry of an entry the classifier must not
// count. ScanCapability reads only a name and a type from a walk entry, so
// that is all this carries; Info is never consulted.
type covCoreWalkEntry struct {
	name string
	mode fs.FileMode
}

func (e covCoreWalkEntry) Name() string               { return e.name }
func (e covCoreWalkEntry) IsDir() bool                { return e.mode.IsDir() }
func (e covCoreWalkEntry) Type() fs.FileMode          { return e.mode.Type() }
func (e covCoreWalkEntry) Info() (fs.FileInfo, error) { return nil, fs.ErrInvalid }

// TestCovCoreScanCapabilityCountsOnlyReadableRegularFiles covers the two
// entries the classifier skips, using deliberately bridge-forcing names: an
// entry the walk could not read, and a non-regular entry (a socket or device
// node has no source to parse). Counting either would flip an all-Go
// repository onto the Python bridge for files that contribute no graph rows.
func TestCovCoreScanCapabilityCountsOnlyReadableRegularFiles(t *testing.T) {
	root := t.TempDir()
	original := walkDir
	t.Cleanup(func() { walkDir = original })
	walkDir = func(_ string, fn fs.WalkDirFunc) error {
		entries := []struct {
			entry covCoreWalkEntry
			err   error
		}{
			{entry: covCoreWalkEntry{name: "unreadable.py"}, err: os.ErrPermission},
			{entry: covCoreWalkEntry{name: "socket.py", mode: fs.ModeSocket}},
			{entry: covCoreWalkEntry{name: "main.go"}},
		}
		for _, item := range entries {
			if err := fn(filepath.Join(root, item.entry.name), item.entry, item.err); err != nil {
				return err
			}
		}
		return nil
	}

	report, err := ScanCapability(root)
	if err != nil {
		t.Fatalf("ScanCapability: %v", err)
	}
	if report.NativeFiles != 1 || report.BridgeFiles != 0 || report.UnsupportedFiles != 0 {
		t.Fatalf("counted native=%d bridge=%d unsupported=%d, want only the one readable Go file",
			report.NativeFiles, report.BridgeFiles, report.UnsupportedFiles)
	}
	if got := languageRows(report); len(got) != 1 || got[LanguageNative].Files != 1 {
		t.Errorf("language rows = %v, want only %s", got, LanguageNative)
	}
	if routing := report.Routing(); routing != RoutingNative {
		t.Errorf("routing = %q, want %q (skipped entries must not force the bridge)", routing, RoutingNative)
	}
}

// ── capability.go: the shebang probe ─────────────────────────────────────────

// TestCovCoreHasShebangProbesTheFirstTwoBytes pins the one classification an
// extension cannot make. The probe is the precondition of upstream's
// interpreter table, so it must answer true for exactly the files upstream
// would route — and false, never fatally, for anything it cannot read.
func TestCovCoreHasShebangProbesTheFirstTwoBytes(t *testing.T) {
	const script = "runner"
	const goSource = "main.go"
	const singleByte = "truncated"
	const empty = "blank"
	root := capabilityRepoContent(t, map[string]string{
		script:     "#!/usr/bin/env python3\nprint('hi')\n",
		goSource:   "package main\n",
		singleByte: "#",
		empty:      "",
	}, script, goSource, singleByte, empty)

	for _, tc := range []struct {
		name string
		path string
		want bool
	}{
		{name: "interpreter_line", path: filepath.Join(root, script), want: true},
		{name: "ordinary_source", path: filepath.Join(root, goSource)},
		{name: "one_byte_file", path: filepath.Join(root, singleByte)},
		{name: "empty_file", path: filepath.Join(root, empty)},
		{name: "missing_file", path: filepath.Join(root, "absent")},
		{name: "directory", path: root},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasShebang(tc.path); got != tc.want {
				t.Errorf("hasShebang(%s) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}
