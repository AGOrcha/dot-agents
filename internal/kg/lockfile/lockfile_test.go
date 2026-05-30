package lockfile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

var fixedTime = time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)

func TestViewStatusValid(t *testing.T) {
	valid := []ViewStatus{StatusReady, StatusPendingRecompatCheck, StatusPendingRebuild, StatusDSLUpdateRequired}
	for _, s := range valid {
		if !s.Valid() {
			t.Fatalf("%q.Valid() = false, want true", s)
		}
	}
	if ViewStatus("bogus").Valid() {
		t.Fatal("bogus status reported valid")
	}
}

func TestDigest(t *testing.T) {
	d := Digest([]byte("hello"))
	if !strings.HasPrefix(d, "sha256:") {
		t.Fatalf("Digest = %q, want sha256: prefix", d)
	}
	if d != Digest([]byte("hello")) {
		t.Fatal("Digest not deterministic")
	}
	if d == Digest([]byte("world")) {
		t.Fatal("Digest collision on distinct input")
	}
}

// lockPath returns a .agentsrc.lock path inside a fresh temp dir.
func lockPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), ".agentsrc.lock")
}

func TestLoadMissingFile(t *testing.T) {
	lf, err := Load(lockPath(t))
	if err != nil {
		t.Fatalf("Load missing: %v", err)
	}
	if lf.Adapters == nil || len(lf.Adapters) != 0 {
		t.Fatalf("Load missing adapters = %v, want empty", lf.Adapters)
	}
}

func TestLoadOpenError(t *testing.T) {
	// agentslock.Open fails when the document is not valid JSON.
	path := lockPath(t)
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "open") {
		t.Fatalf("Load malformed = %v, want open error", err)
	}
}

func TestLoadAbsentAdaptersSection(t *testing.T) {
	// A lock document with only a config section and no adapters key must
	// Load to an empty (non-nil) adapter map.
	path := lockPath(t)
	if err := os.WriteFile(path, []byte(`{"lock_version":1,"config":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	lf, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if lf.Adapters == nil {
		t.Fatal("Load left Adapters nil; want empty map")
	}
}

func TestLoadDecodeError(t *testing.T) {
	// adapters present but the wrong JSON shape (array, not object) -> the
	// section decode into map[string]*Adapter fails.
	path := lockPath(t)
	if err := os.WriteFile(path, []byte(`{"lock_version":1,"adapters":[1,2,3]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "decode adapters") {
		t.Fatalf("Load bad adapters shape = %v, want decode error", err)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	path := lockPath(t)
	lf := New()
	lf.Activate("none", "sha256:src", "sha256:schema", fixedTime)
	if err := Save(path, lf); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	ad, ok := got.Adapters["none"]
	if !ok {
		t.Fatal("round-trip lost adapter none")
	}
	if ad.SourceDigest != "sha256:src" || ad.SchemaDigest != "sha256:schema" {
		t.Fatalf("round-trip digests = %+v", ad)
	}
	if ad.ActivatedAt != fixedTime.Format(time.RFC3339) {
		t.Fatalf("ActivatedAt = %q", ad.ActivatedAt)
	}
	// The persisted document is JSON (the shared .agentsrc.lock format), not
	// a standalone YAML file.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(raw) {
		t.Fatalf("lockfile is not JSON:\n%s", raw)
	}
	// No leftover temp files in the document directory.
	entries, _ := os.ReadDir(filepath.Dir(path))
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Fatalf("leftover temp file: %s", e.Name())
		}
	}
}

// TestSavePreservesSiblingSections is the forward-compat proof (§7.4): writing
// the "adapters" section through the shared writer must leave a pre-existing
// "config" and "packages" section (owned by the config-v2 resolver) byte-for-
// byte intact. This guards against the lockfile package ever reverting to
// owning the whole file.
func TestSavePreservesSiblingSections(t *testing.T) {
	path := lockPath(t)
	// Seed a lock document the config-v2 resolver would have written.
	seed := `{
  "lock_version": 1,
  "config": {
    "acme:org/base": {"sha": "abc123", "ttl_until": "2026-06-01T00:00:00Z"}
  },
  "packages": {
    "graph/none": {"version": "1.0.0"}
  }
}`
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	// Activate an adapter and Save only the adapters section.
	lf, err := Load(path)
	if err != nil {
		t.Fatalf("Load seed: %v", err)
	}
	lf.Activate("none", "sha256:src", "sha256:schema", fixedTime)
	if err := Save(path, lf); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Re-read the whole document and assert all three sections coexist.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("re-parse document: %v\n%s", err, raw)
	}
	for _, section := range []string{"config", "packages", "adapters", "lock_version"} {
		if _, ok := doc[section]; !ok {
			t.Fatalf("Save dropped %q section; document = %s", section, raw)
		}
	}

	// The sibling sections must be unchanged in content.
	var config map[string]struct {
		SHA      string `json:"sha"`
		TTLUntil string `json:"ttl_until"`
	}
	if err := json.Unmarshal(doc["config"], &config); err != nil {
		t.Fatalf("config section corrupted: %v", err)
	}
	if config["acme:org/base"].SHA != "abc123" {
		t.Fatalf("config sha mutated: %+v", config)
	}
	var packages map[string]struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(doc["packages"], &packages); err != nil {
		t.Fatalf("packages section corrupted: %v", err)
	}
	if packages["graph/none"].Version != "1.0.0" {
		t.Fatalf("packages version mutated: %+v", packages)
	}

	// And the adapters section round-trips back through Load.
	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if _, ok := reloaded.Adapters["none"]; !ok {
		t.Fatal("adapters section lost after preservation save")
	}
}

func TestSaveNil(t *testing.T) {
	if err := Save(lockPath(t), nil); err == nil {
		t.Fatal("Save(nil) want error")
	}
}

func TestSaveOpenError(t *testing.T) {
	// A malformed document makes the open inside Save fail.
	path := lockPath(t)
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, New()); err == nil || !strings.Contains(err.Error(), "open") {
		t.Fatalf("Save open error = %v", err)
	}
}

func TestSaveFlushError(t *testing.T) {
	// agentslock.Flush writes a temp file into the document's directory; a
	// read-only directory makes that fail, exercising the flush-error branch.
	dir := t.TempDir()
	path := filepath.Join(dir, ".agentsrc.lock")
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	err := Save(path, New())
	if err == nil {
		t.Skip("filesystem permitted write to read-only dir (running as root?)")
	}
	if !strings.Contains(err.Error(), "flush") {
		t.Fatalf("Save flush error = %v", err)
	}
}

func TestActivateNewAndReactivate(t *testing.T) {
	lf := New()
	lf.Activate("none", "sha256:a", "sha256:b", fixedTime)
	ad := lf.Adapters["none"]
	// Attach a view, then re-activate and confirm the view is preserved.
	ad.MaterializedViews = map[string]*View{"v": {ViewStatus: StatusReady}}
	later := fixedTime.Add(time.Hour)
	lf.Activate("none", "sha256:c", "sha256:d", later)
	ad = lf.Adapters["none"]
	if ad.SourceDigest != "sha256:c" || ad.SchemaDigest != "sha256:d" {
		t.Fatalf("re-activate digests = %+v", ad)
	}
	if ad.ActivatedAt != later.Format(time.RFC3339) {
		t.Fatalf("re-activate ActivatedAt = %q", ad.ActivatedAt)
	}
	if _, ok := ad.MaterializedViews["v"]; !ok {
		t.Fatal("re-activate dropped materialized views")
	}
}

func TestActivateNilMap(t *testing.T) {
	lf := &Lockfile{} // Adapters nil
	lf.Activate("none", "s", "d", fixedTime)
	if lf.Adapters["none"] == nil {
		t.Fatal("Activate did not initialize Adapters map")
	}
}

func TestAdapterNames(t *testing.T) {
	lf := New()
	lf.Activate("zeta", "s", "d", fixedTime)
	lf.Activate("alpha", "s", "d", fixedTime)
	got := lf.AdapterNames()
	want := []string{"alpha", "zeta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AdapterNames() = %v, want %v", got, want)
	}
}

func TestReconcileNoneAdapterNoViews(t *testing.T) {
	lf := New()
	lf.Activate("none", "s", "d", fixedTime)
	changes := lf.Reconcile(nil, fixedTime)
	if len(changes) != 0 {
		t.Fatalf("Reconcile none-only = %v, want no changes", changes)
	}
}

func TestReconcileReadyMissingTables(t *testing.T) {
	lf := New()
	lf.Activate("comp", "s", "d", fixedTime)
	lf.Adapters["comp"].MaterializedViews = map[string]*View{
		"v": {ViewStatus: StatusReady, ViewDigest: "sha256:v"},
	}
	changes := lf.Reconcile(nil, fixedTime) // nil => treated absent
	if len(changes) != 1 {
		t.Fatalf("Reconcile = %v, want 1 change", changes)
	}
	if changes[0].To != StatusPendingRebuild || !strings.Contains(changes[0].Reason, "absent") {
		t.Fatalf("change = %+v", changes[0])
	}
	if lf.Adapters["comp"].MaterializedViews["v"].ViewStatus != StatusPendingRebuild {
		t.Fatal("view status not flipped to pending-rebuild")
	}
}

func TestReconcileReadyDigestMismatch(t *testing.T) {
	lf := New()
	lf.Activate("comp", "s", "d", fixedTime)
	lf.Adapters["comp"].MaterializedViews = map[string]*View{
		"v": {ViewStatus: StatusReady, ViewDigest: "sha256:expected"},
	}
	present := func(adapter, view string) (bool, string) { return true, "sha256:different" }
	changes := lf.Reconcile(present, fixedTime)
	if len(changes) != 1 || changes[0].Reason != "view digest mismatch" {
		t.Fatalf("Reconcile mismatch = %v", changes)
	}
}

func TestReconcileReadyConsistent(t *testing.T) {
	lf := New()
	lf.Activate("comp", "s", "d", fixedTime)
	lf.Adapters["comp"].MaterializedViews = map[string]*View{
		"v": {ViewStatus: StatusReady, ViewDigest: "sha256:v"},
	}
	present := func(adapter, view string) (bool, string) { return true, "sha256:v" }
	changes := lf.Reconcile(present, fixedTime)
	if len(changes) != 0 {
		t.Fatalf("Reconcile consistent = %v, want none", changes)
	}
}

func TestReconcilePendingStatesNoAction(t *testing.T) {
	lf := New()
	lf.Activate("comp", "s", "d", fixedTime)
	lf.Adapters["comp"].MaterializedViews = map[string]*View{
		"a": {ViewStatus: StatusPendingRecompatCheck},
		"b": {ViewStatus: StatusPendingRebuild},
		"c": {ViewStatus: StatusDSLUpdateRequired},
	}
	present := func(adapter, view string) (bool, string) { return false, "" }
	changes := lf.Reconcile(present, fixedTime)
	if len(changes) != 0 {
		t.Fatalf("Reconcile pending states = %v, want none", changes)
	}
}

func TestReconcileInvalidStatus(t *testing.T) {
	lf := New()
	lf.Activate("comp", "s", "d", fixedTime)
	lf.Adapters["comp"].MaterializedViews = map[string]*View{
		"v": {ViewStatus: ViewStatus("garbage")},
	}
	changes := lf.Reconcile(nil, fixedTime)
	if len(changes) != 1 || changes[0].Reason != "invalid view_status" {
		t.Fatalf("Reconcile invalid = %v", changes)
	}
}

func TestRecordTransitionTruncates(t *testing.T) {
	v := &View{ViewStatus: StatusReady}
	for i := 0; i < maxStateHistory+5; i++ {
		v.recordTransition(StatusPendingRebuild, "t", fixedTime)
		v.recordTransition(StatusReady, "t", fixedTime)
	}
	if len(v.StateHistory) > maxStateHistory {
		t.Fatalf("StateHistory len = %d, want <= %d", len(v.StateHistory), maxStateHistory)
	}
}
