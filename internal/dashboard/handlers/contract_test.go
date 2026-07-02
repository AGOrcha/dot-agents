// contract_test.go is the t03 contract test: it drives every REST endpoint
// through the mount exactly as a browser would and pins (a) a byte-exact
// golden response per endpoint, (b) JSON Schema conformance of every data
// payload against the shipped schemas/dashboard-*.schema.json (the same
// source the t08 frontend type generator consumes — mirroring the store's
// TestDTOsConformToShippedSchemas), (c) the §1.2 envelope invariants, and
// (d) the literal /api/v1/observability path prefix (§1.1).
package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/dashboard/store"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// --- fixture -----------------------------------------------------------------

// testdataPath resolves name under this package's testdata dir regardless of
// the working directory (the fixture setup chdirs the test).
func testdataPath(t *testing.T, name string) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(thisFile), "testdata", name)
}

// fixtureMtimes pins a deterministic mtime per fixture file so the
// mtime-derived response fields (last_update, health's last_iter_log_mtime)
// — and therefore the golden bodies and their etags — are byte-stable. Every
// score sidecar is stamped newer than its iter record so the t06 recompute
// decorator sees fresh sidecars.
var fixtureMtimes = map[string]string{
	"iter-1.yaml":               "2026-05-01T10:01:00Z",
	"iter-1.score.yaml":         "2026-05-01T10:01:30Z",
	"iter-2.yaml":               "2026-05-01T10:02:00Z",
	"iter-2.score.yaml":         "2026-05-01T10:02:30Z",
	"iter-3.yaml":               "2026-05-01T10:03:00Z",
	"iter-3.score.yaml":         "2026-05-01T10:03:30Z",
	"iter-5.yaml":               "2026-05-01T10:05:00Z",
	"session-sess-a.score.yaml": "2026-05-01T10:06:00Z",
}

// fixtureRoot copies testdata/iterlog into a fresh temp dir, applies the
// pinned mtimes, and chdirs the test there so the store can be handed the
// RELATIVE root name "iterlog" — keeping iter_log_dir, health's roots, and
// the etags independent of the temp path, which is what makes byte-exact
// goldens possible. Returns the relative root.
func fixtureRoot(t *testing.T) string {
	t.Helper()
	src := testdataPath(t, "iterlog")
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatalf("read fixture dir: %v", err)
	}
	dir := t.TempDir()
	root := filepath.Join(dir, "iterlog")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir fixture root: %v", err)
	}
	for _, e := range entries {
		stamp, ok := fixtureMtimes[e.Name()]
		if !ok {
			t.Fatalf("fixture file %s has no pinned mtime", e.Name())
		}
		mt, err := time.Parse(time.RFC3339, stamp)
		if err != nil {
			t.Fatalf("parse pinned mtime for %s: %v", e.Name(), err)
		}
		data, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			t.Fatalf("read fixture %s: %v", e.Name(), err)
		}
		dst := filepath.Join(root, e.Name())
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			t.Fatalf("copy fixture %s: %v", e.Name(), err)
		}
		if err := os.Chtimes(dst, mt, mt); err != nil {
			t.Fatalf("chtimes fixture %s: %v", e.Name(), err)
		}
	}
	t.Chdir(dir)
	return "iterlog"
}

// newFixtureMount builds a Mount over the t02 DiskStore on the fixture root.
func newFixtureMount(t *testing.T) *Mount {
	t.Helper()
	m, err := New(Deps{Store: store.New([]string{fixtureRoot(t)})})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return m
}

// get performs one recorded GET against h with optional request headers.
func get(t *testing.T, h http.Handler, target string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

// --- golden ------------------------------------------------------------------

// assertGolden compares body to the committed golden file byte-for-byte. Run
// the package tests with UPDATE_GOLDEN=1 to regenerate goldens after a
// deliberate contract change.
func assertGolden(t *testing.T, name string, body []byte) {
	t.Helper()
	path := testdataPath(t, filepath.Join("golden", name))
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(path, body, 0o644); err != nil {
			t.Fatalf("update golden %s: %v", name, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s (set UPDATE_GOLDEN=1 to create): %v", name, err)
	}
	if !bytes.Equal(body, want) {
		t.Errorf("response differs from golden %s:\n got: %s\nwant: %s", name, body, want)
	}
}

// --- schema conformance --------------------------------------------------------

// compileSchema loads one shipped schema from the repo's schemas/ dir.
func compileSchema(t *testing.T, name string) *jsonschema.Schema {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
	f, err := os.Open(filepath.Join(repoRoot, "schemas", name))
	if err != nil {
		t.Fatalf("open schema %s: %v", name, err)
	}
	defer f.Close()
	doc, err := jsonschema.UnmarshalJSON(f)
	if err != nil {
		t.Fatalf("parse schema %s: %v", name, err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(name, doc); err != nil {
		t.Fatalf("add schema %s: %v", name, err)
	}
	sch, err := c.Compile(name)
	if err != nil {
		t.Fatalf("compile schema %s: %v", name, err)
	}
	return sch
}

// validateRaw validates one JSON document against sch.
func validateRaw(t *testing.T, sch *jsonschema.Schema, raw []byte) {
	t.Helper()
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("parse payload: %v", err)
	}
	if err := sch.Validate(inst); err != nil {
		t.Fatalf("payload violates schema: %v\njson: %s", err, raw)
	}
}

// validateData unwraps the envelope's data (§1.2: the schemas describe the
// payload, not the envelope) and validates it — each element for list
// payloads — against the shipped schema.
func validateData(t *testing.T, schemaName string, body []byte) {
	t.Helper()
	sch := compileSchema(t, schemaName)
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if bytes.HasPrefix(bytes.TrimSpace(env.Data), []byte("[")) {
		var items []json.RawMessage
		if err := json.Unmarshal(env.Data, &items); err != nil {
			t.Fatalf("unmarshal list data: %v", err)
		}
		for _, item := range items {
			validateRaw(t, sch, item)
		}
		return
	}
	validateRaw(t, sch, env.Data)
}

// --- envelope invariants -------------------------------------------------------

// assertEnvelope checks the §1.2 invariants: the ETag header is the quoted
// meta.etag, and meta.count is present exactly on list endpoints where it
// equals len(data).
func assertEnvelope(t *testing.T, rr *httptest.ResponseRecorder, list bool) {
	t.Helper()
	var env struct {
		Data json.RawMessage `json:"data"`
		Meta struct {
			ETag  string `json:"etag"`
			Count *int   `json:"count"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if env.Meta.ETag == "" {
		t.Error("meta.etag must be non-empty")
	}
	if got := rr.Header().Get("ETag"); got != `"`+env.Meta.ETag+`"` {
		t.Errorf("ETag header %q must be the quoted meta.etag %q", got, env.Meta.ETag)
	}
	if !list {
		if env.Meta.Count != nil {
			t.Errorf("meta.count must be absent on non-list endpoints, got %d", *env.Meta.Count)
		}
		return
	}
	var items []json.RawMessage
	if err := json.Unmarshal(env.Data, &items); err != nil {
		t.Fatalf("list data must be a JSON array: %v", err)
	}
	if env.Meta.Count == nil || *env.Meta.Count != len(items) {
		t.Errorf("meta.count = %v, want %d", env.Meta.Count, len(items))
	}
}

// --- the contract test ---------------------------------------------------------

// contractCases enumerates the REST rows of the API.md §2 endpoint catalogue
// (the SSE row is t05's). An empty schema means the payload shape is inline
// in API.md (§3.6 health) rather than a shipped schema file.
var contractCases = []struct {
	name   string
	target string
	schema string
	golden string
	list   bool
}{
	{"runs_list", basePath + "/runs", "dashboard-run.schema.json", "runs.json", true},
	{"run_detail", basePath + "/runs/sess-a", "dashboard-run.schema.json", "run_sess-a.json", false},
	{"iterations_list", basePath + "/runs/sess-a/iterations", "dashboard-iteration.schema.json", "iterations_sess-a.json", true},
	{"iteration_detail", basePath + "/iterations/2", "dashboard-iteration.schema.json", "iteration_2.json", false},
	{"iteration_unscored", basePath + "/iterations/5", "dashboard-iteration.schema.json", "iteration_5.json", false},
	{"rubric", basePath + "/rubric", "dashboard-rubric.schema.json", "rubric.json", false},
	{"health", basePath + "/health", "", "health.json", false},
}

// TestContractGoldenResponses is the endpoint-by-endpoint contract test.
func TestContractGoldenResponses(t *testing.T) {
	m := newFixtureMount(t)
	for _, tc := range contractCases {
		t.Run(tc.name, func(t *testing.T) {
			rr := get(t, m, tc.target, nil)
			if rr.Code != http.StatusOK {
				t.Fatalf("GET %s = %d, want 200; body: %s", tc.target, rr.Code, rr.Body)
			}
			if ct := rr.Header().Get("Content-Type"); ct != contentTypeJSON {
				t.Errorf("Content-Type = %q, want %q", ct, contentTypeJSON)
			}
			assertEnvelope(t, rr, tc.list)
			if tc.schema != "" {
				validateData(t, tc.schema, rr.Body.Bytes())
			}
			assertGolden(t, tc.golden, rr.Body.Bytes())
		})
	}
}

// TestVersionedPrefixIsLiteral pins API.md §1.1: the version-first
// /api/v1/observability prefix is asserted as a literal so a later /api/v2
// move is a deliberate, testable break.
func TestVersionedPrefixIsLiteral(t *testing.T) {
	const literal = "/api/v1/observability"
	if basePath != literal {
		t.Fatalf("basePath = %q, want the pinned literal %q", basePath, literal)
	}
	m := newFixtureMount(t)
	if rr := get(t, m, literal+"/health", nil); rr.Code != http.StatusOK {
		t.Fatalf("GET %s/health = %d, want 200", literal, rr.Code)
	}
	if rr := get(t, m, "/api/v2/observability/health", nil); rr.Code != http.StatusNotFound {
		t.Fatalf("an unversioned-contract v2 path must not resolve, got %d", rr.Code)
	}
}
