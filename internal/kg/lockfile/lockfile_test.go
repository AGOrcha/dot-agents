package lockfile

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

var fixedTime = time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)

var errFake = errors.New("fake io error")

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

func TestLoadMissingFile(t *testing.T) {
	lf, err := Load(filepath.Join(t.TempDir(), "absent.yaml"))
	if err != nil {
		t.Fatalf("Load missing: %v", err)
	}
	if lf.Adapters == nil || len(lf.Adapters) != 0 {
		t.Fatalf("Load missing adapters = %v, want empty", lf.Adapters)
	}
}

func TestLoadParseError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(path, []byte("adapters: [unterminated"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load malformed want error")
	}
}

func TestLoadEmptyAdaptersKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lock.yaml")
	// Explicit null adapters -> Load must normalize to an empty map.
	if err := os.WriteFile(path, []byte("adapters: null\n"), 0o644); err != nil {
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

// fakeTemp is a fault-injecting tempFile.
type fakeTemp struct {
	writeErr error
	syncErr  error
	closeErr error
	closed   bool
}

func (f *fakeTemp) Name() string { return "/tmp/fake" }
func (f *fakeTemp) Write(p []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return len(p), nil
}
func (f *fakeTemp) Sync() error { return f.syncErr }
func (f *fakeTemp) Close() error {
	f.closed = true
	return f.closeErr
}

// swapSaveSeams overrides the package save collaborators for the duration of
// a test and restores them afterward.
func swapSaveSeams(t *testing.T, mk func(string, os.FileMode) error, ct func(string, string) (tempFile, error), rn func(string, string) error, rm func(string) error) {
	t.Helper()
	om, oc, orn, orm := saveMkdirAll, saveCreateTemp, saveRename, saveRemove
	if mk != nil {
		saveMkdirAll = mk
	}
	if ct != nil {
		saveCreateTemp = ct
	}
	if rn != nil {
		saveRename = rn
	}
	if rm != nil {
		saveRemove = rm
	}
	t.Cleanup(func() {
		saveMkdirAll, saveCreateTemp, saveRename, saveRemove = om, oc, orn, orm
	})
}

func TestSaveCreateTempError(t *testing.T) {
	swapSaveSeams(t, nil, func(string, string) (tempFile, error) {
		return nil, errFake
	}, nil, nil)
	if err := Save("/tmp/whatever/lock.yaml", New()); err == nil || !strings.Contains(err.Error(), "create temp") {
		t.Fatalf("Save create-temp error = %v", err)
	}
}

func TestSaveWriteError(t *testing.T) {
	ft := &fakeTemp{writeErr: errFake}
	swapSaveSeams(t, nil, func(string, string) (tempFile, error) { return ft, nil }, nil, func(string) error { return nil })
	if err := Save("/tmp/lock.yaml", New()); err == nil || !strings.Contains(err.Error(), "write temp") {
		t.Fatalf("Save write error = %v", err)
	}
	if !ft.closed {
		t.Fatal("temp file not closed on write error")
	}
}

func TestSaveSyncError(t *testing.T) {
	swapSaveSeams(t, nil, func(string, string) (tempFile, error) { return &fakeTemp{syncErr: errFake}, nil }, nil, func(string) error { return nil })
	if err := Save("/tmp/lock.yaml", New()); err == nil || !strings.Contains(err.Error(), "fsync temp") {
		t.Fatalf("Save fsync error = %v", err)
	}
}

func TestSaveCloseError(t *testing.T) {
	swapSaveSeams(t, nil, func(string, string) (tempFile, error) { return &fakeTemp{closeErr: errFake}, nil }, nil, func(string) error { return nil })
	if err := Save("/tmp/lock.yaml", New()); err == nil || !strings.Contains(err.Error(), "close temp") {
		t.Fatalf("Save close error = %v", err)
	}
}

func TestSaveRenameError(t *testing.T) {
	removed := false
	swapSaveSeams(t, nil,
		func(string, string) (tempFile, error) { return &fakeTemp{}, nil },
		func(string, string) error { return errFake },
		func(string) error { removed = true; return nil },
	)
	if err := Save("/tmp/lock.yaml", New()); err == nil || !strings.Contains(err.Error(), "rename temp") {
		t.Fatalf("Save rename error = %v", err)
	}
	if !removed {
		t.Fatal("temp file not cleaned up on rename error")
	}
}

func TestLoadReadError(t *testing.T) {
	// A directory cannot be read as a file -> non-IsNotExist error.
	dir := t.TempDir()
	if _, err := Load(dir); err == nil {
		t.Fatal("Load of a directory want error")
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "adapters.lock.yaml")
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
	// No leftover temp files.
	entries, _ := os.ReadDir(filepath.Dir(path))
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Fatalf("leftover temp file: %s", e.Name())
		}
	}
}

func TestSaveNil(t *testing.T) {
	if err := Save(filepath.Join(t.TempDir(), "x.yaml"), nil); err == nil {
		t.Fatal("Save(nil) want error")
	}
}

func TestSaveMkdirError(t *testing.T) {
	dir := t.TempDir()
	// Make a file where a directory is needed.
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(blocker, "sub", "lock.yaml")
	if err := Save(path, New()); err == nil {
		t.Fatal("Save under a file-path want error")
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
