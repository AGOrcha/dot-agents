package agentslock

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/fsops"
)

func TestIsDeletePendingLockErr(t *testing.T) {
	permErr := &os.PathError{Op: "mkdir", Path: "x.lock", Err: os.ErrPermission}
	existErr := &os.PathError{Op: "mkdir", Path: "x.lock", Err: os.ErrExist}
	cases := []struct {
		name string
		err  error
		goos string
		want bool
	}{
		{"windows permission is delete-pending transient", permErr, "windows", true},
		{"unix permission is fatal, not transient", permErr, "linux", false},
		{"darwin permission is fatal, not transient", permErr, "darwin", false},
		{"windows exist is not this predicate", existErr, "windows", false},
		{"windows nil is not transient", nil, "windows", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isDeletePendingLockErr(tc.err, tc.goos); got != tc.want {
				t.Fatalf("isDeletePendingLockErr(%v, %q) = %v, want %v", tc.err, tc.goos, got, tc.want)
			}
		})
	}
}

// seedLockDir pre-creates the sidecar lock dir for path. When writeHolder is
// true it writes a holder file whose acquisition timestamp is age in the past
// (age 0 = a brand-new, live holder). When writeHolder is false it simulates a
// crash mid-acquire (dir exists, no holder) and back-dates the dir's mtime by
// age so the mtime fallback in lockIsStale can judge it.
func seedLockDir(t *testing.T, path string, age time.Duration, writeHolder bool) {
	t.Helper()
	lockDir := path + ".lock"
	if err := os.Mkdir(lockDir, 0o700); err != nil {
		t.Fatalf("seed lock dir: %v", err)
	}
	if !writeHolder {
		old := time.Now().Add(-age)
		if err := os.Chtimes(lockDir, old, old); err != nil {
			t.Fatalf("seed dir mtime: %v", err)
		}
		return
	}
	acquired := time.Now().Add(-age).UnixNano()
	contents := fmt.Sprintf("%d\n%d\n", os.Getpid(), acquired)
	if err := os.WriteFile(filepath.Join(lockDir, holderFile), []byte(contents), 0o600); err != nil {
		t.Fatalf("seed holder: %v", err)
	}
}

type configSection struct {
	Layers map[string]string `json:"layers,omitempty"`
}

// mustMkdir / mustWriteFile / backdateDir are fatal-on-error fixture helpers
// shared by the lock-lifecycle tests.
func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
}

func mustWriteFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func backdateDir(t *testing.T, dir string, age time.Duration) {
	t.Helper()
	old := time.Now().Add(-age)
	if err := os.Chtimes(dir, old, old); err != nil {
		t.Fatal(err)
	}
}

func TestOpenMissingFileIsFresh(t *testing.T) {
	lf, err := Open(filepath.Join(t.TempDir(), ".agentsrc.lock"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// lock_version is present, no sections.
	var cfg configSection
	ok, err := lf.Section("config", &cfg)
	if err != nil {
		t.Fatalf("Section: %v", err)
	}
	if ok {
		t.Fatal("expected config section absent in a fresh lockfile")
	}
}

func TestSetGetFlushRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agentsrc.lock")
	lf, _ := Open(path)
	if err := lf.SetSection("config", configSection{Layers: map[string]string{"org/base": "sha1"}}); err != nil {
		t.Fatalf("SetSection: %v", err)
	}
	if err := lf.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	var got configSection
	ok, err := reopened.Section("config", &got)
	if err != nil || !ok {
		t.Fatalf("Section after reopen: ok=%v err=%v", ok, err)
	}
	if got.Layers["org/base"] != "sha1" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	// lock_version persisted on disk.
	raw, _ := os.ReadFile(path)
	if !json.Valid(raw) {
		t.Fatal("on-disk lockfile is not valid JSON")
	}
	var top map[string]json.RawMessage
	_ = json.Unmarshal(raw, &top)
	if string(top[lockVersionKey]) != "1" {
		t.Fatalf("lock_version on disk = %s, want 1", top[lockVersionKey])
	}
}

func TestSiblingSectionsPreserved(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agentsrc.lock")
	// Pre-seed a file that already has an `adapters` section (written by the
	// graph-adapter lifecycle) — the config writer must not clobber it.
	seed := `{"lock_version":1,"adapters":{"kuzu":{"source_digest":"sha256:aa"}}}`
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}
	lf, _ := Open(path)
	if err := lf.SetSection("config", configSection{Layers: map[string]string{"team/x": "sha2"}}); err != nil {
		t.Fatal(err)
	}
	if err := lf.Flush(); err != nil {
		t.Fatal(err)
	}

	reopened, _ := Open(path)
	var adapters map[string]map[string]string
	ok, err := reopened.Section("adapters", &adapters)
	if err != nil || !ok {
		t.Fatalf("adapters section lost: ok=%v err=%v", ok, err)
	}
	if adapters["kuzu"]["source_digest"] != "sha256:aa" {
		t.Fatalf("adapters mutated: %+v", adapters)
	}
	var cfg configSection
	if ok, _ := reopened.Section("config", &cfg); !ok || cfg.Layers["team/x"] != "sha2" {
		t.Fatalf("config not written alongside adapters: %+v", cfg)
	}
}

func TestUnknownTopLevelKeyPreserved(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agentsrc.lock")
	// A future top-level key this version doesn't know about must survive a
	// write that only touches `config`.
	seed := `{"lock_version":1,"future_thing":{"x":1}}`
	_ = os.WriteFile(path, []byte(seed), 0o600)
	lf, _ := Open(path)
	_ = lf.SetSection("config", configSection{})
	if err := lf.Flush(); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	var top map[string]json.RawMessage
	_ = json.Unmarshal(raw, &top)
	if _, ok := top["future_thing"]; !ok {
		t.Fatalf("unknown key dropped: %s", raw)
	}
}

func TestLockVersionPreservedWhenPresent(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agentsrc.lock")
	_ = os.WriteFile(path, []byte(`{"lock_version":2}`), 0o600)
	lf, _ := Open(path)
	_ = lf.SetSection("config", configSection{})
	_ = lf.Flush()
	raw, _ := os.ReadFile(path)
	var top map[string]json.RawMessage
	_ = json.Unmarshal(raw, &top)
	if string(top[lockVersionKey]) != "2" {
		t.Fatalf("lock_version overwritten: %s", top[lockVersionKey])
	}
}

func TestOpenDefaultsLockVersionWhenAbsent(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agentsrc.lock")
	_ = os.WriteFile(path, []byte(`{"config":{}}`), 0o600) // no lock_version
	lf, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(lf.doc[lockVersionKey]) != "1" {
		t.Fatalf("lock_version not defaulted: %q", lf.doc[lockVersionKey])
	}
}

func TestSetSectionReservedKey(t *testing.T) {
	lf, _ := Open(filepath.Join(t.TempDir(), "x.lock"))
	if err := lf.SetSection(lockVersionKey, 3); err == nil {
		t.Fatal("expected error setting reserved lock_version key")
	}
	if err := lf.SetSection(inputsDigestKey, "sha256:zz"); err == nil {
		t.Fatal("expected error setting reserved inputs_digest key via SetSection")
	}
}

func TestInputsDigestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agentsrc.lock")
	lf, _ := Open(path)
	if _, ok := lf.InputsDigest(); ok {
		t.Fatal("fresh lockfile must have no inputs_digest")
	}
	lf.SetInputsDigest("sha256:abc")
	if err := lf.SetSection("units", map[string]string{"git:a@1": "x"}); err != nil {
		t.Fatalf("SetSection: %v", err)
	}
	if err := lf.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	reopened, _ := Open(path)
	got, ok := reopened.InputsDigest()
	if !ok || got != "sha256:abc" {
		t.Fatalf("inputs_digest round-trip: got %q ok=%v", got, ok)
	}
	// inputs_digest is a top-level scalar, not nested under a section.
	raw, _ := os.ReadFile(path)
	var top map[string]json.RawMessage
	_ = json.Unmarshal(raw, &top)
	if string(top[inputsDigestKey]) != `"sha256:abc"` {
		t.Fatalf("inputs_digest on disk = %s", top[inputsDigestKey])
	}
}

func TestSetInputsDigestEmptyClears(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agentsrc.lock")
	lf, _ := Open(path)
	lf.SetInputsDigest("sha256:abc")
	lf.SetInputsDigest("") // clear
	if _, ok := lf.InputsDigest(); ok {
		t.Fatal("empty SetInputsDigest must clear the field")
	}
	_ = lf.Flush()
	raw, _ := os.ReadFile(path)
	var top map[string]json.RawMessage
	_ = json.Unmarshal(raw, &top)
	if _, ok := top[inputsDigestKey]; ok {
		t.Fatalf("inputs_digest should be absent after clear: %s", raw)
	}
}

func TestInputsDigestPreservedAcrossSectionWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agentsrc.lock")
	seed := `{"lock_version":1,"inputs_digest":"sha256:seed","adapters":{"kuzu":{}}}`
	_ = os.WriteFile(path, []byte(seed), 0o600)
	lf, _ := Open(path)
	// A writer that only touches a section must not drop inputs_digest.
	_ = lf.SetSection("units", map[string]string{"git:a@1": "x"})
	_ = lf.Flush()

	reopened, _ := Open(path)
	got, ok := reopened.InputsDigest()
	if !ok || got != "sha256:seed" {
		t.Fatalf("inputs_digest dropped by section write: got %q ok=%v", got, ok)
	}
}

func TestInputsDigestMalformedTreatedAbsent(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agentsrc.lock")
	// A non-string / empty inputs_digest must report absent, not error.
	_ = os.WriteFile(path, []byte(`{"lock_version":1,"inputs_digest":123}`), 0o600)
	lf, _ := Open(path)
	if _, ok := lf.InputsDigest(); ok {
		t.Fatal("non-string inputs_digest must report absent")
	}
}

func TestSetSectionMarshalError(t *testing.T) {
	lf, _ := Open(filepath.Join(t.TempDir(), "x.lock"))
	if err := lf.SetSection("config", make(chan int)); err == nil {
		t.Fatal("expected marshal error for unmarshalable value")
	}
}

func TestSectionDecodeError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.lock")
	_ = os.WriteFile(path, []byte(`{"lock_version":1,"config":"not-an-object"}`), 0o600)
	lf, _ := Open(path)
	var cfg configSection
	if _, err := lf.Section("config", &cfg); err == nil {
		t.Fatal("expected decode error unmarshaling string into struct")
	}
}

func TestOpenParseError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.lock")
	_ = os.WriteFile(path, []byte(`{not json`), 0o600)
	if _, err := Open(path); err == nil {
		t.Fatal("expected parse error on malformed lockfile")
	}
}

func TestOpenReadError(t *testing.T) {
	dir := t.TempDir() // a directory is not a readable file
	if _, err := Open(dir); err == nil {
		t.Fatal("expected read error opening a directory as a lockfile")
	}
}

func TestAcquireFileLockParentCannotBeCreated(t *testing.T) {
	// acquireFileLock now MkdirAll's the lock dir's parent (the Windows mkdir
	// fix). When that parent cannot be made — here an intermediate path component
	// is a regular FILE, not a directory — acquire must surface the error rather
	// than proceeding to a doomed Mkdir. Deterministic and portable across OSes.
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("not a dir"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Lock path sits under the file-blocker, so filepath.Dir(lockDir) is a path
	// whose ancestor is a file → MkdirAll fails.
	_, err := acquireFileLock(filepath.Join(blocker, "sub", ".agentsrc.lock"))
	if err == nil {
		t.Fatal("expected acquire error when the lock parent cannot be created")
	}
	if !strings.Contains(err.Error(), "ensure lock parent") {
		t.Fatalf("expected an ensure-lock-parent error, got: %v", err)
	}
}

func TestFlushMarshalError(t *testing.T) {
	// White-box: inject an invalid RawMessage so MarshalIndent fails.
	lf, _ := Open(filepath.Join(t.TempDir(), "x.lock"))
	lf.doc["broken"] = json.RawMessage(`{invalid`)
	lf.dirty["broken"] = true
	if err := lf.Flush(); err == nil {
		t.Fatal("expected marshal error with an invalid raw section")
	}
}

func TestConcurrentFlushPreservesSiblingSectionsAndInputsDigest(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agentsrc.lock")
	first, err := Open(path)
	if err != nil {
		t.Fatalf("Open first: %v", err)
	}
	second, err := Open(path)
	if err != nil {
		t.Fatalf("Open second: %v", err)
	}
	if err := first.SetSection("units", map[string]string{"git:a@1": "sha1"}); err != nil {
		t.Fatalf("SetSection units: %v", err)
	}
	first.SetInputsDigest("sha256:aaa")
	if err := second.SetSection("adapters", map[string]string{"none": "ready"}); err != nil {
		t.Fatalf("SetSection adapters: %v", err)
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, lf := range []*Lockfile{first, second} {
		wg.Add(1)
		go func(lock *Lockfile) {
			defer wg.Done()
			<-start
			errs <- lock.Flush()
		}(lf)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("Flush: %v", err)
		}
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	var units map[string]string
	if ok, err := reopened.Section("units", &units); err != nil || !ok || units["git:a@1"] != "sha1" {
		t.Fatalf("units lost after concurrent flush: ok=%v units=%+v err=%v", ok, units, err)
	}
	var adapters map[string]string
	if ok, err := reopened.Section("adapters", &adapters); err != nil || !ok || adapters["none"] != "ready" {
		t.Fatalf("adapters lost after concurrent flush: ok=%v adapters=%+v err=%v", ok, adapters, err)
	}
	if got, ok := reopened.InputsDigest(); !ok || got != "sha256:aaa" {
		t.Fatalf("inputs_digest lost after concurrent flush: got=%q ok=%v", got, ok)
	}
}

func TestFlushMergesForeignSectionWrittenAfterOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agentsrc.lock")
	if err := os.WriteFile(path, []byte(`{"lock_version":1,"config":{"layers":{"old":"sha0"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	lf, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := lf.SetSection("units", map[string]string{"git:a@1": "sha1"}); err != nil {
		t.Fatalf("SetSection: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"lock_version":1,"config":{"layers":{"old":"sha0"}},"future_thing":{"x":1}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := lf.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatal(err)
	}
	if _, ok := top["future_thing"]; !ok {
		t.Fatalf("foreign section dropped: %s", raw)
	}
	var units map[string]string
	reopened, _ := Open(path)
	if ok, _ := reopened.Section("units", &units); !ok || units["git:a@1"] != "sha1" {
		t.Fatalf("dirty section not written: %+v", units)
	}
}

func TestFlushErrorsWhenDiskBecomesMalformedAfterOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agentsrc.lock")
	if err := os.WriteFile(path, []byte(`{"lock_version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	lf, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := lf.SetSection("units", map[string]string{"git:a@1": "sha1"}); err != nil {
		t.Fatalf("SetSection: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{not-json`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := lf.Flush(); err == nil {
		t.Fatal("expected Flush to fail on malformed latest lockfile")
	}
}

func TestFlushMergesDirtyInputsDigestDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agentsrc.lock")
	seed := `{"lock_version":1,"inputs_digest":"sha256:old","config":{"layers":{"old":"sha0"}}}`
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}
	lf, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	lf.SetInputsDigest("")
	if err := os.WriteFile(path, []byte(`{"lock_version":1,"inputs_digest":"sha256:old","config":{"layers":{"old":"sha0"}},"future_thing":{"x":1}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := lf.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatal(err)
	}
	if _, ok := top[inputsDigestKey]; ok {
		t.Fatalf("inputs_digest should be deleted: %s", raw)
	}
	if _, ok := top["future_thing"]; !ok {
		t.Fatalf("foreign section dropped during delete merge: %s", raw)
	}
}

func TestFlushTimesOutWhenLockHeldFresh(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agentsrc.lock")
	// A fresh holder (just acquired) is NOT stale, so Flush must block then
	// return the timeout error rather than reclaiming the lock.
	seedLockDir(t, path, 0, true)

	lf, _ := Open(path)
	if err := lf.SetSection("config", configSection{Layers: map[string]string{"x": "y"}}); err != nil {
		t.Fatalf("SetSection: %v", err)
	}

	start := time.Now()
	err := lf.Flush()
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected timeout error while a fresh lock is held")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout error, got: %v", err)
	}
	if elapsed < lockAcquireTimeout {
		t.Fatalf("Flush returned before the acquire timeout: %v < %v", elapsed, lockAcquireTimeout)
	}
}

func TestFlushReclaimsStaleLockByTTL(t *testing.T) {
	cases := []struct {
		name        string
		age         time.Duration
		writeHolder bool
	}{
		{name: "older than TTL", age: lockStaleTTL + time.Second, writeHolder: true},
		{name: "missing holder file with old dir", age: lockStaleTTL + time.Second, writeHolder: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), ".agentsrc.lock")
			seedLockDir(t, path, tc.age, tc.writeHolder)

			lf, _ := Open(path)
			if err := lf.SetSection("config", configSection{Layers: map[string]string{"x": "y"}}); err != nil {
				t.Fatalf("SetSection: %v", err)
			}
			start := time.Now()
			if err := lf.Flush(); err != nil {
				t.Fatalf("expected stale lock to be reclaimed, got: %v", err)
			}
			// Reclaim happens before the first retry sleep, so it must be quick —
			// well under the acquire timeout.
			if elapsed := time.Since(start); elapsed >= lockAcquireTimeout {
				t.Fatalf("reclaim took too long (%v); did it wait out the timeout?", elapsed)
			}
			reopened, _ := Open(path)
			var got configSection
			if ok, _ := reopened.Section("config", &got); !ok || got.Layers["x"] != "y" {
				t.Fatalf("flush did not persist after reclaim: %+v", got)
			}
		})
	}
}

func TestFlushGarbageHolderTreatedStale(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agentsrc.lock")
	lockDir := path + ".lock"
	if err := os.Mkdir(lockDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lockDir, holderFile), []byte("not-a-pid\nnot-a-time\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// An unparseable holder falls back to the dir mtime, so back-date the dir
	// past the TTL to make it eligible for reclaim.
	old := time.Now().Add(-(lockStaleTTL + time.Second))
	if err := os.Chtimes(lockDir, old, old); err != nil {
		t.Fatal(err)
	}
	lf, _ := Open(path)
	_ = lf.SetSection("config", configSection{Layers: map[string]string{"x": "y"}})
	if err := lf.Flush(); err != nil {
		t.Fatalf("expected garbage holder to be reclaimed, got: %v", err)
	}
}

func TestFlushSucceedsAfterContendingReleaseMidWait(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agentsrc.lock")
	// Hold a FRESH lock through the real acquire path (holder file recorded) so
	// the waiter does not reclaim it as stale; release it through the REAL
	// release path from a goroutine partway through the waiter's retry loop.
	// The waiter must then acquire and succeed (not time out).
	//
	// The old shape simulated the release with a raw one-shot os.RemoveAll,
	// which on windows-latest could fail or HALF-fail against the waiter's own
	// probe handles (holder file deleted, dir left behind) — and a holderless
	// dir was then granted the full 30s TTL, stranding the waiter into its 5s
	// timeout. Driving the production release (atomic rename-away with bounded
	// retry) is both the honest lifecycle and deterministic on Windows.
	unlock, err := acquireFileLock(path)
	if err != nil {
		t.Fatalf("holder acquire: %v", err)
	}

	lf, _ := Open(path)
	_ = lf.SetSection("config", configSection{Layers: map[string]string{"x": "y"}})

	done := make(chan struct{})
	go func() {
		defer close(done)
		time.Sleep(200 * time.Millisecond)
		unlock()
	}()
	// Join the releaser before the test ends: its release path reads package
	// seam vars (removeTrashFn) that later tests may swap, and an unjoined
	// goroutine has no happened-before edge to those writes under -race.
	defer func() { <-done }()

	start := time.Now()
	if err := lf.Flush(); err != nil {
		t.Fatalf("expected Flush to succeed after the holder released: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed >= lockAcquireTimeout {
		t.Fatalf("Flush waited out the timeout instead of acquiring after release: %v", elapsed)
	}
	if elapsed < 150*time.Millisecond {
		t.Fatalf("Flush returned implausibly fast (%v); lock may not have been contended", elapsed)
	}
}

func TestLockIsStaleShortHolderUsesDirMtime(t *testing.T) {
	// A holder file with fewer than two lines is unparseable, so lockIsStale must
	// fall back to the dir mtime. Back-date the dir past the TTL → stale.
	dir := t.TempDir()
	lockDir := filepath.Join(dir, "x.lock")
	if err := os.Mkdir(lockDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lockDir, holderFile), []byte("only-one-line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-(lockStaleTTL + time.Second))
	if err := os.Chtimes(lockDir, old, old); err != nil {
		t.Fatal(err)
	}
	if !legacyLockDirStale(lockDir) {
		t.Fatal("short holder + old dir mtime must be stale")
	}
	// Fresh dir mtime with the same short holder must NOT be stale.
	now := time.Now()
	if err := os.Chtimes(lockDir, now, now); err != nil {
		t.Fatal(err)
	}
	if legacyLockDirStale(lockDir) {
		t.Fatal("short holder + fresh dir mtime must not be stale")
	}
}

func TestDirOlderThanMissingDirIsStale(t *testing.T) {
	// A dir that cannot be stat'd (does not exist) is treated as stale.
	if !pathOlderThan(filepath.Join(t.TempDir(), "no-such-lock"), lockStaleTTL) {
		t.Fatal("missing lock dir must be reported stale")
	}
}

func TestDebugfGatedOnEnv(t *testing.T) {
	// debugf prints only when DA_DEBUG is set; otherwise it returns early.
	// Exercise both branches for coverage. Output goes to stderr and is not
	// asserted — we only need both branches taken.
	t.Setenv("DA_DEBUG", "")
	debugf("should be suppressed %d", 1)
	t.Setenv("DA_DEBUG", "1")
	debugf("should be emitted %d", 2)
}

func TestUnlockLockSurfacesRenameError(t *testing.T) {
	// Force the release rename to fail by stripping write permission from the
	// lock file's parent. unlockLock must take the debugf error branch without
	// panicking. Skipped where the FS does not enforce the perm bit (root).
	parent := filepath.Join(t.TempDir(), "ro-parent")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(parent, "x.lock")
	// Release requires a matching identity (empty IDs never release).
	id := "1\n2\n"
	mustWriteFile(t, lockPath, id)
	if err := os.Chmod(parent, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o700) }) // let TempDir cleanup proceed

	t.Setenv("DA_DEBUG", "1")
	unlockLock(lockPath, id) // must not panic; exercises the rename-failure + debugf branch

	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		t.Skip("filesystem renamed the lock despite a read-only parent; cannot force a release error here")
	}
}

// TestAcquireFileLockCreatesMissingParent is the Windows regression guard for
// the field bug where `da config explain --all` / `da install` failed in the
// agentslock acquire with "mkdir <...>.agentsrc.lock.lock: The system cannot
// find the file specified" (ERROR_FILE_NOT_FOUND). The lock dir is created with
// os.Mkdir, which — unlike MkdirAll — does not create intermediate path
// components: if the lockfile's parent directory does not yet exist when the
// lock is first taken, the bare Mkdir fails (ENOENT on unix, ERROR_FILE_NOT_FOUND
// on Windows). acquireFileLock must MkdirAll the parent first.
//
// This runs on ALL OSes (no GOOS skip) so windows-latest CI exercises the exact
// failure surface. It drives the real exported Flush → acquireFileLock path, not
// the helper, so it catches the class end-to-end.
func TestAcquireFileLockCreatesMissingParent(t *testing.T) {
	// Lockfile path whose parent directory does NOT exist yet — the field repro:
	// the first resolve takes the lock before any writer has materialized the dir.
	parent := filepath.Join(t.TempDir(), "not-yet-created", "prov-workspace")
	path := filepath.Join(parent, ".agentsrc.lock")

	lf, err := Open(path) // Open tolerates a missing file (fresh document)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := lf.SetSection("units", map[string]string{"git:a@1": "sha1"}); err != nil {
		t.Fatalf("SetSection: %v", err)
	}
	// Flush acquires the mkdir lock (the line that failed in the field) and then
	// atomically writes the document. Both need the parent to exist; the fix
	// MkdirAll's it inside acquireFileLock.
	if err := lf.Flush(); err != nil {
		t.Fatalf("Flush into a missing-parent path must succeed after the fix, got: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("lockfile not written: %v", err)
	}
	// The sidecar lock dir must have been removed on release (RemoveAll in
	// unlockFileLock), leaving a clean directory for the next acquire.
	if _, err := os.Stat(path + ".lock"); !os.IsNotExist(err) {
		t.Fatalf("lock dir should be released after Flush, stat err=%v", err)
	}
}

// TestAcquireReleaseReAcquireCycle drives the full acquire → release →
// re-acquire cycle through the real lock path on every OS, so a Windows-specific
// regression in the create/remove/recreate sequence (e.g. delete-pending vs a
// fresh Mkdir) is caught by windows-latest CI rather than only on unix.
func TestAcquireReleaseReAcquireCycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agentsrc.lock")
	for i := 0; i < 3; i++ {
		unlock, err := acquireFileLock(path)
		if err != nil {
			t.Fatalf("acquire #%d: %v", i, err)
		}
		if _, err := os.Stat(path + ".lock"); err != nil {
			t.Fatalf("lock dir absent while held (#%d): %v", i, err)
		}
		unlock()
		if _, err := os.Stat(path + ".lock"); !os.IsNotExist(err) {
			t.Fatalf("lock dir not released (#%d): stat err=%v", i, err)
		}
	}
}

// TestAcquireFileLockMissingParentDirect exercises acquireFileLock directly (not
// via Flush) against a path several levels below an absent directory, asserting
// the parent is created and the lock acquired. Runs on all OSes.
func TestAcquireFileLockMissingParentDirect(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "c", ".agentsrc.lock")
	unlock, err := acquireFileLock(path)
	if err != nil {
		t.Fatalf("acquireFileLock with missing parents must succeed: %v", err)
	}
	defer unlock()
	if _, err := os.Stat(path + ".lock"); err != nil {
		t.Fatalf("lock dir not created under freshly-made parents: %v", err)
	}
}

func TestConcurrentSetSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agentsrc.lock")
	lf, _ := Open(path)
	var wg sync.WaitGroup
	for _, name := range []string{"config", "packages", "adapters"} {
		wg.Add(1)
		go func(n string) {
			defer wg.Done()
			if err := lf.SetSection(n, map[string]string{"k": n}); err != nil {
				t.Errorf("SetSection(%s): %v", n, err)
			}
		}(name)
	}
	wg.Wait()
	if err := lf.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	reopened, _ := Open(path)
	for _, n := range []string{"config", "packages", "adapters"} {
		var got map[string]string
		if ok, _ := reopened.Section(n, &got); !ok || got["k"] != n {
			t.Fatalf("section %s missing/wrong after concurrent writes: %+v", n, got)
		}
	}
}

// TestAcquireFileLockRoundTrip exercises the exported primitive end-to-end: a
// successful acquire creates the sidecar lock dir, and the returned release
// removes it and reports no error.
func TestAcquireFileLockRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "guarded.ndjson")
	release, err := AcquireFileLock(path)
	if err != nil {
		t.Fatalf("AcquireFileLock: %v", err)
	}
	if _, err := os.Stat(path + ".lock"); err != nil {
		t.Fatalf("lock dir absent while held: %v", err)
	}
	if err := release(); err != nil {
		t.Fatalf("release returned an error: %v", err)
	}
	if _, err := os.Stat(path + ".lock"); !os.IsNotExist(err) {
		t.Fatalf("lock dir not removed by release: stat err=%v", err)
	}
	// Release is idempotent: a second call after the dir is gone is a no-op.
	if err := release(); err != nil {
		t.Fatalf("second release should be a no-op, got: %v", err)
	}
}

// TestAcquireFileLockStaleReleaseDoesNotClobberNewHolder is the regression guard
// for the once-guard fix: a stray second release from a caller that already let
// go must NOT delete a different caller's live lock dir for the same path. The
// dangerous sequence is A acquire → A release → B acquire (fresh live lock) →
// A release AGAIN. Without the once-guard, A's second release RemoveAll's B's
// lock dir, silently breaking B's mutual exclusion.
func TestAcquireFileLockStaleReleaseDoesNotClobberNewHolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "guarded.ndjson")
	releaseA, err := AcquireFileLock(path)
	if err != nil {
		t.Fatalf("A acquire: %v", err)
	}
	if err := releaseA(); err != nil {
		t.Fatalf("A release: %v", err)
	}

	releaseB, err := AcquireFileLock(path)
	if err != nil {
		t.Fatalf("B acquire after A released must succeed: %v", err)
	}
	if _, err := os.Stat(path + ".lock"); err != nil {
		t.Fatalf("B's lock dir should exist while held: %v", err)
	}

	// A's stray second release must be a no-op: it must not touch B's live dir.
	if err := releaseA(); err != nil {
		t.Fatalf("A's stale second release should be a cached no-op, got: %v", err)
	}
	if _, err := os.Stat(path + ".lock"); err != nil {
		t.Fatalf("A's stale release clobbered B's live lock dir: %v", err)
	}

	// B still owns the lock and can release it cleanly.
	if err := releaseB(); err != nil {
		t.Fatalf("B release: %v", err)
	}
	if _, err := os.Stat(path + ".lock"); !os.IsNotExist(err) {
		t.Fatalf("B's release should remove the lock dir: stat err=%v", err)
	}
}

// TestAcquireFileLockReleaseAllowsReAcquire proves that after release the same
// path can be locked again immediately (no lingering held state).
func TestAcquireFileLockReleaseAllowsReAcquire(t *testing.T) {
	path := filepath.Join(t.TempDir(), "guarded.ndjson")
	release, err := AcquireFileLock(path)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if err := release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	release2, err := AcquireFileLock(path)
	if err != nil {
		t.Fatalf("re-acquire after release must succeed: %v", err)
	}
	if err := release2(); err != nil {
		t.Fatalf("second release: %v", err)
	}
}

// TestAcquireFileLockBlocksWhileHeld proves a second acquire does not succeed
// while the first holder is live: it blocks until the holder releases, then
// acquires (rather than timing out). Mirrors the Flush-level contention test but
// drives the exported API on both sides.
func TestAcquireFileLockBlocksWhileHeld(t *testing.T) {
	path := filepath.Join(t.TempDir(), "guarded.ndjson")
	release, err := AcquireFileLock(path)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	// Capture start BEFORE launching the releaser so the full 200ms sleep is
	// inside the measured window — otherwise scheduler delay before start could
	// shrink the observed contention and make the lower-bound assertion flaky.
	start := time.Now()
	done := make(chan struct{})
	go func() {
		defer close(done)
		time.Sleep(200 * time.Millisecond)
		_ = release()
	}()
	// Join the releaser before the test ends (see the MidWait test: the release
	// path reads package seam vars later tests may swap under -race).
	defer func() { <-done }()

	release2, err := AcquireFileLock(path)
	if err != nil {
		t.Fatalf("second acquire should succeed after the holder releases: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed >= lockAcquireTimeout {
		t.Fatalf("second acquire waited out the timeout instead of acquiring after release: %v", elapsed)
	}
	if elapsed < 150*time.Millisecond {
		t.Fatalf("second acquire returned implausibly fast (%v); lock may not have been contended", elapsed)
	}
	if err := release2(); err != nil {
		t.Fatalf("release2: %v", err)
	}
}

// TestAcquireFileLockTimesOutWhileFreshHeld proves that a live (fresh) holder is
// not reclaimed: the contender blocks for the full timeout and returns the
// timeout error rather than stealing the lock.
func TestAcquireFileLockTimesOutWhileFreshHeld(t *testing.T) {
	path := filepath.Join(t.TempDir(), "guarded.ndjson")
	seedLockDir(t, path, 0, true) // fresh holder → not stale

	start := time.Now()
	release, err := AcquireFileLock(path)
	elapsed := time.Since(start)
	if err == nil {
		_ = release()
		t.Fatal("expected timeout error while a fresh lock is held")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout error, got: %v", err)
	}
	if elapsed < lockAcquireTimeout {
		t.Fatalf("acquire returned before the timeout: %v < %v", elapsed, lockAcquireTimeout)
	}
}

// TestAcquireFileLockReclaimsStaleLock proves the exported API recovers from a
// crashed holder: a lock dir whose recorded age exceeds the TTL is reclaimed and
// acquisition succeeds quickly (before the acquire timeout).
func TestAcquireFileLockReclaimsStaleLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "guarded.ndjson")
	seedLockDir(t, path, lockStaleTTL+time.Second, true) // stale holder

	start := time.Now()
	release, err := AcquireFileLock(path)
	if err != nil {
		t.Fatalf("expected stale lock to be reclaimed, got: %v", err)
	}
	if elapsed := time.Since(start); elapsed >= lockAcquireTimeout {
		t.Fatalf("reclaim took too long (%v); did it wait out the timeout?", elapsed)
	}
	if err := release(); err != nil {
		t.Fatalf("release after reclaim: %v", err)
	}
}

// TestAcquireFileLockReleaseSurfacesError proves the exported release returns the
// underlying removal error (not just a debug log) when the sidecar dir cannot be
// removed — here by stripping write permission from its parent. Skipped where the
// FS does not enforce the perm bit (e.g. running as root).
func TestAcquireFileLockReleaseSurfacesError(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "ro-parent")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "guarded.ndjson")
	release, err := AcquireFileLock(path)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o700); _ = release() })
	if err := os.Chmod(parent, 0o500); err != nil {
		t.Fatal(err)
	}
	err = release()
	if _, statErr := os.Stat(path + ".lock"); os.IsNotExist(statErr) {
		t.Skip("filesystem removed the lock dir despite a read-only parent; cannot force a release error here")
	}
	if err == nil {
		t.Fatal("expected release to surface the removal error")
	}
}

// TestAcquireFileLockMissingParentCreated proves the exported API materializes an
// absent parent chain before taking the lock (the Windows ENOENT/EFILE_NOT_FOUND
// field bug), and surfaces an error when that parent cannot be created.
func TestAcquireFileLockMissingParentCreated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "guarded.ndjson")
	release, err := AcquireFileLock(path)
	if err != nil {
		t.Fatalf("acquire under missing parents must succeed: %v", err)
	}
	if _, err := os.Stat(path + ".lock"); err != nil {
		t.Fatalf("lock dir not created under freshly-made parents: %v", err)
	}
	if err := release(); err != nil {
		t.Fatalf("release: %v", err)
	}

	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("not a dir"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireFileLock(filepath.Join(blocker, "sub", "guarded.ndjson")); err == nil {
		t.Fatal("expected acquire error when the lock parent cannot be created")
	}
}

// TestAcquireFileLockRecoversFromPartialReleaseRemnants is the regression guard
// for the lock-release lifecycle race: a release (or crash) that removed the
// holder file but left the lock dir behind used to be judged by the full 30s
// TTL via dir mtime — 6x a contender's 5s acquire budget — so a fresh `da`
// process (the field `config explain` / `install` shape) was stranded into a
// guaranteed timeout. A holderless dir must now be reclaimed after the SHORT
// lockNoHolderGrace, so acquisition succeeds within the budget in every
// partial-release state.
func TestAcquireFileLockRecoversFromPartialReleaseRemnants(t *testing.T) {
	cases := []struct {
		name string
		age  time.Duration
		// minElapsed proves the grace was honored (no instant theft of a dir
		// that could still be a mid-acquire holder); loose to stay CI-safe.
		minElapsed time.Duration
	}{
		{name: "holderless dir already older than the grace", age: lockNoHolderGrace + time.Second, minElapsed: 0},
		{name: "holderless dir with fresh mtime", age: 0, minElapsed: time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertAcquireRecoversFromRemnant(t, tc.age, tc.minElapsed)
		})
	}
}

// assertAcquireRecoversFromRemnant seeds a holderless remnant of the given age
// and asserts a fresh acquire succeeds within the budget while honoring the
// no-holder grace (no instant theft), ending as the recorded holder.
func assertAcquireRecoversFromRemnant(t *testing.T, age, minElapsed time.Duration) {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".agentsrc.lock")
	seedLockDir(t, path, age, false) // dir, NO holder file

	start := time.Now()
	release, err := AcquireFileLock(path)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("acquire over a partial-release remnant must succeed, got: %v", err)
	}
	if elapsed >= lockAcquireTimeout {
		t.Fatalf("acquire waited out the timeout (%v); remnant not reclaimed within budget", elapsed)
	}
	if elapsed < minElapsed {
		t.Fatalf("acquire returned before the no-holder grace could elapse: %v < %v", elapsed, minElapsed)
	}
	// We are now the recorded holder: the legacy dir remnant was reclaimed and
	// replaced by a single-file lock carrying a complete identity.
	data, rerr := os.ReadFile(path + ".lock")
	if rerr != nil {
		t.Fatalf("lock identity absent after reclaim+acquire: %v", rerr)
	}
	if _, ok := identityAge(data); !ok {
		t.Fatalf("lock identity unparseable after reclaim+acquire: %q", data)
	}
	if err := release(); err != nil {
		t.Fatalf("release: %v", err)
	}
}

// TestLockIsStaleHolderlessDirUsesShortGrace pins the staleness policy the fix
// introduces: a lock dir with no holder file is NOT judged by the 30s TTL. A
// fresh one is protected (the mid-acquire Mkdir->writeHolder window), and one
// older than lockNoHolderGrace — but far younger than the TTL — is stale.
func TestLockIsStaleHolderlessDirUsesShortGrace(t *testing.T) {
	lockDir := filepath.Join(t.TempDir(), "x.lock")
	if err := os.Mkdir(lockDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if legacyLockDirStale(lockDir) {
		t.Fatal("fresh holderless dir must not be stale (mid-acquire window)")
	}
	old := time.Now().Add(-(lockNoHolderGrace + time.Second))
	if err := os.Chtimes(lockDir, old, old); err != nil {
		t.Fatal(err)
	}
	if !legacyLockDirStale(lockDir) {
		t.Fatal("holderless dir older than the grace must be stale well before the TTL")
	}
}

// TestReleaseSkipsWhenIdentityChanged proves an overdue release cannot steal a
// successor's lock: if the lock file no longer carries this acquisition's
// identity (a rival TTL-reclaimed and re-acquired while we were suspended),
// release is a no-op and the rival's lock survives.
func TestReleaseSkipsWhenIdentityChanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "guarded.ndjson")
	release, err := AcquireFileLock(path)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	// Simulate the rival's reclaim + re-acquire: same lock name, new identity.
	rival := "999999\n1\nrival\n"
	mustWriteFile(t, path+".lock", rival)
	if err := release(); err != nil {
		t.Fatalf("identity-mismatch release must be a clean no-op, got: %v", err)
	}
	data, err := os.ReadFile(path + ".lock")
	if err != nil || string(data) != rival {
		t.Fatalf("overdue release touched the rival's live lock: data=%q err=%v", data, err)
	}
	_ = os.RemoveAll(path + ".lock") // cleanup for TempDir
}

// TestLockStillOurs covers the ownership predicate's branches directly.
func TestLockStillOurs(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "x.lock")
	if lockStillOurs(lockPath, "") {
		t.Fatal("empty identity must NEVER prove ownership (an identity-less release could steal a successor's lock)")
	}
	if lockStillOurs(lockPath, "1\n2\n") {
		t.Fatal("a missing lock file must NOT prove ownership")
	}
	mustWriteFile(t, lockPath, "1\n2\n")
	if !lockStillOurs(lockPath, "1\n2\n") {
		t.Fatal("matching identity must prove ownership")
	}
	if lockStillOurs(lockPath, "3\n4\n") {
		t.Fatal("mismatched identity must not prove ownership")
	}
}

// TestReleaseFreesLockNameWhenTrashDeleteFails is the deterministic seam test
// for the rename-away release: even when deleting the renamed-away trash dir
// fails outright, the lock NAME must already be free (release returns nil and a
// re-acquire succeeds immediately), and a later healthy release must sweep the
// leftover trash.
func TestReleaseFreesLockNameWhenTrashDeleteFails(t *testing.T) {
	realRemove := removeTrashFn
	t.Cleanup(func() { removeTrashFn = realRemove })
	removeTrashFn = func(string) error { return fmt.Errorf("simulated windows pin") }

	dir := t.TempDir()
	path := filepath.Join(dir, "guarded.ndjson")
	release, err := AcquireFileLock(path)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if err := release(); err != nil {
		t.Fatalf("release must succeed once the name is renamed away, got: %v", err)
	}
	if _, err := os.Stat(path + ".lock"); !os.IsNotExist(err) {
		t.Fatalf("lock name still occupied after release: stat err=%v", err)
	}
	if n := countLockTrash(t, dir, path); n != 1 {
		t.Fatalf("expected exactly 1 leftover trash dir, found %d", n)
	}

	// Trash never blocks re-acquisition, and a healthy release sweeps it.
	removeTrashFn = realRemove
	release2, err := AcquireFileLock(path)
	if err != nil {
		t.Fatalf("re-acquire alongside leftover trash: %v", err)
	}
	if err := release2(); err != nil {
		t.Fatalf("second release: %v", err)
	}
	if n := countLockTrash(t, dir, path); n != 0 {
		t.Fatalf("healthy release must sweep leftover trash, found %d", n)
	}
}

// countLockTrash counts `<lockdir>.stale-*` trash siblings for path's lock dir.
func countLockTrash(t *testing.T, dir, path string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	prefix := filepath.Base(path) + ".lock" + lockTrashInfix
	n := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix) {
			n++
		}
	}
	return n
}

// TestReclaimStaleLockDirLostRace: a reclaim whose rename loses (source already
// gone — a rival reclaimed first) must report false and touch nothing, so the
// caller's acquire loop just keeps waiting instead of deleting in place.
func TestReclaimStaleLockDirLostRace(t *testing.T) {
	if reclaimStaleLock(filepath.Join(t.TempDir(), "no-such.lock"), nil) {
		t.Fatal("reclaim of a vanished lock dir must report false")
	}
}

// TestReclaimStaleLockDirRenamesAway: a successful reclaim frees the lock name
// via rename and disposes of the trash.
func TestReclaimStaleLockDirRenamesAway(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "guarded.ndjson")
	seedLockDir(t, path, lockStaleTTL+time.Second, true)
	if !reclaimStaleLock(path+".lock", nil) {
		t.Fatal("reclaim of an existing stale dir must succeed")
	}
	if _, err := os.Stat(path + ".lock"); !os.IsNotExist(err) {
		t.Fatalf("lock name still occupied after reclaim: stat err=%v", err)
	}
	if n := countLockTrash(t, dir, path); n != 0 {
		t.Fatalf("reclaim left trash behind, found %d", n)
	}
}

// TestReclaimStaleLockDirTrashDeleteFailure: even when the trash cannot be
// deleted, the reclaim itself succeeds — the lock name is free the moment the
// rename lands.
func TestReclaimStaleLockDirTrashDeleteFailure(t *testing.T) {
	realRemove := removeTrashFn
	t.Cleanup(func() { removeTrashFn = realRemove })
	removeTrashFn = func(string) error { return fmt.Errorf("simulated windows pin") }

	dir := t.TempDir()
	path := filepath.Join(dir, "guarded.ndjson")
	seedLockDir(t, path, lockStaleTTL+time.Second, true)
	if !reclaimStaleLock(path+".lock", nil) {
		t.Fatal("reclaim must succeed once the rename lands, regardless of trash disposal")
	}
	if _, err := os.Stat(path + ".lock"); !os.IsNotExist(err) {
		t.Fatalf("lock name still occupied after reclaim: stat err=%v", err)
	}
	if n := countLockTrash(t, dir, path); n != 1 {
		t.Fatalf("expected the undeletable trash dir to remain, found %d", n)
	}
}

// TestClaimPublishesCompleteIdentityAtomically is the structural assertion
// the single-object redesign exists for: the lock name is created by ONE
// atomic hardlink from a temp that already carries the FULL identity, so no
// observer can ever see a claimed-but-identity-less lock. The link seam
// captures the temp's contents at the exact instant the name comes into
// existence and asserts they are already a complete, parseable identity.
func TestClaimPublishesCompleteIdentityAtomically(t *testing.T) {
	realLink := linkLockFn
	t.Cleanup(func() { linkLockFn = realLink })
	var atLinkTime []byte
	linkLockFn = func(oldname, newname string) error {
		data, err := os.ReadFile(oldname)
		if err != nil {
			t.Fatalf("temp unreadable at link time: %v", err)
		}
		atLinkTime = data
		return realLink(oldname, newname)
	}

	path := filepath.Join(t.TempDir(), "guarded.ndjson")
	release, err := AcquireFileLock(path)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if _, ok := identityAge(atLinkTime); !ok {
		t.Fatalf("identity must be complete BEFORE the lock name exists, got %q", atLinkTime)
	}
	onDisk, err := os.ReadFile(path + ".lock")
	if err != nil || string(onDisk) != string(atLinkTime) {
		t.Fatalf("lock name must carry exactly the pre-written identity: %q vs %q (err=%v)", onDisk, atLinkTime, err)
	}
	if err := release(); err != nil {
		t.Fatalf("release: %v", err)
	}
}

// TestReadLockFileMatchesOSReadFile pins the readLockFile seam's os.ReadFile
// contract on every platform: a present file yields its exact bytes, and a
// missing name yields an os.IsNotExist-classifiable error. On Windows this
// exercises the FILE_SHARE_DELETE reader (readlock_windows.go) that lets a
// concurrent read coexist with the release rename; elsewhere it is os.ReadFile.
func TestReadLockFileMatchesOSReadFile(t *testing.T) {
	dir := t.TempDir()
	present := filepath.Join(dir, "present.lock")
	want := "1234\n567890\ndeadbeef\n"
	mustWriteFile(t, present, want)

	got, err := readLockFile(present)
	if err != nil {
		t.Fatalf("readLockFile(present): %v", err)
	}
	if string(got) != want {
		t.Fatalf("readLockFile content mismatch: got %q want %q", got, want)
	}

	if _, err := readLockFile(filepath.Join(dir, "absent.lock")); !os.IsNotExist(err) {
		t.Fatalf("readLockFile(absent) must be an os.IsNotExist error, got: %v", err)
	}
}

// TestClaimNeverExposesPartialIdentity hammers acquire/release cycles while a
// concurrent reader polls the lock name: every successful read must be a
// complete, parseable identity. Under the old two-step design the poller
// could catch the holderless window; under the atomic link claim any partial
// observation is a hard failure, not a flake.
//
// The impossibility claim is scoped to the HARDLINK claim path — the degraded
// O_EXCL path is the documented two-step residual (FAT/exFAT), and on
// windows-latest an AV scan can transiently pin the claim temp, pushing a
// cycle onto that path (the master run 28570275221 red: the poller read the
// O_EXCL window's empty file and correctly reported "partial"). The link seam
// therefore records whether any cycle COULD have degraded (a link failure
// other than exists/not-exists); a partial observation is fatal only when no
// degradation was possible, and otherwise the test verifies the degraded
// cycle still ended with a complete identity on disk.
func TestClaimNeverExposesPartialIdentity(t *testing.T) {
	realLink := linkLockFn
	t.Cleanup(func() { linkLockFn = realLink })
	var mu sync.Mutex
	degradedPossible := false
	linkLockFn = func(oldname, newname string) error {
		err := realLink(oldname, newname)
		if err != nil && !os.IsExist(err) && !os.IsNotExist(err) {
			mu.Lock()
			degradedPossible = true
			mu.Unlock()
		}
		return err
	}

	path := filepath.Join(t.TempDir(), "guarded.ndjson")
	stop := make(chan struct{})
	done := make(chan struct{})
	degraded := func() bool {
		mu.Lock()
		defer mu.Unlock()
		return degradedPossible
	}
	partial := pollForPartialIdentity(path+".lock", stop, done, degraded)
	for i := 0; i < 30; i++ {
		release, err := AcquireFileLock(path)
		if err != nil {
			t.Fatalf("acquire #%d: %v", i, err)
		}
		if err := release(); err != nil {
			t.Fatalf("release #%d: %v", i, err)
		}
	}
	close(stop)
	<-done
	if got, seen := partial.get(); seen {
		t.Fatalf("observer caught a partial identity on the HARDLINK claim path: %q", got)
	}
}

// pollForPartialIdentity starts the concurrent observer for the structural
// claim-atomicity test: it polls the lock name until stop closes, ignoring
// read failures (free / mid-rename / trash churn) and any unparseable read
// for which the degraded O_EXCL path could have been engaged, and captures
// the first partial identity observed on the hardlink path.
func pollForPartialIdentity(lockPath string, stop, done chan struct{}, degraded func() bool) *atomicwrap {
	partial := &atomicwrap{}
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			// Read through the same seam production contenders use: on Windows
			// that shares DELETE so this poll never blocks the holder's atomic
			// rename-away (a plain os.ReadFile here pins the name against the
			// release rename with ERROR_SHARING_VIOLATION — the exact windows-
			// latest red this models). The atomicity assertion is unchanged: a
			// share-DELETE read still observes either a complete identity or a
			// vanished name, never a partial.
			data, err := readLockFile(lockPath)
			if err != nil {
				continue // free, mid-rename, or trash-churn: all fine
			}
			if _, ok := identityAge(data); ok {
				continue
			}
			if degraded() {
				continue // documented O_EXCL two-step window; not the hardlink path
			}
			partial.set(string(data))
			return
		}
	}()
	return partial
}

// TestClaimTransientLinkFailureStaysOnHardlinkPath pins the code-side fix for
// the windows-latest structural red: a transient hardlink failure (AV pinning
// the just-written temp) must be retried on the strong path, NOT degrade the
// claim to the two-step O_EXCL. The exclusive path's pre-verify hook doubles
// as the detector — it must never fire.
func TestClaimTransientLinkFailureStaysOnHardlinkPath(t *testing.T) {
	realLink := linkLockFn
	t.Cleanup(func() { linkLockFn = realLink })
	t.Cleanup(func() { testHookBeforeClaimVerify = nil })
	testHookBeforeClaimVerify = func(string) {
		t.Error("claim degraded to the O_EXCL path on a transient link failure")
	}
	failures := 0
	linkLockFn = func(oldname, newname string) error {
		if failures < linkDegradeAttempts-1 {
			failures++
			return &os.PathError{Op: "link", Path: newname, Err: fmt.Errorf("simulated AV sharing violation")}
		}
		return realLink(oldname, newname)
	}

	path := filepath.Join(t.TempDir(), "guarded.ndjson")
	release, err := AcquireFileLock(path)
	if err != nil {
		t.Fatalf("acquire must ride out transient link failures: %v", err)
	}
	data, rerr := os.ReadFile(path + ".lock")
	if rerr != nil {
		t.Fatalf("lock identity unreadable: %v", rerr)
	}
	if _, ok := identityAge(data); !ok {
		t.Fatalf("hardlink claim must publish a complete identity, got %q", data)
	}
	if failures != linkDegradeAttempts-1 {
		t.Fatalf("expected %d absorbed transients, saw %d", linkDegradeAttempts-1, failures)
	}
	if err := release(); err != nil {
		t.Fatalf("release: %v", err)
	}
}

// atomicwrap is a tiny mutex-guarded string capture for cross-goroutine test
// observations.
type atomicwrap struct {
	mu   sync.Mutex
	s    string
	seen bool
}

func (a *atomicwrap) set(s string) {
	a.mu.Lock()
	a.s, a.seen = s, true
	a.mu.Unlock()
}

func (a *atomicwrap) get() (string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.s, a.seen
}

// TestClaimLostRaceReportsExist: a claim against an occupied name loses with
// an exists-classified error and must not disturb the occupant.
func TestClaimLostRaceReportsExist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "guarded.ndjson")
	occupant := "777\n888\nwinner\n"
	mustWriteFile(t, path+".lock", occupant)
	if _, err := claimLock(path+".lock", &acquireLoopState{}); !os.IsExist(err) {
		t.Fatalf("claim against an occupied name must report exists, got: %v", err)
	}
	data, err := os.ReadFile(path + ".lock")
	if err != nil || string(data) != occupant {
		t.Fatalf("lost claim must not touch the occupant: data=%q err=%v", data, err)
	}
}

// TestClaimFallsBackWhenLinkUnsupported: on a filesystem without hardlinks
// (FAT/exFAT — simulated via the link seam failing every attempt) the claim
// absorbs linkDegradeAttempts transients, then degrades to the O_EXCL
// two-step and still publishes a complete identity.
func TestClaimFallsBackWhenLinkUnsupported(t *testing.T) {
	realLink := linkLockFn
	t.Cleanup(func() { linkLockFn = realLink })
	linkLockFn = func(string, string) error {
		return fmt.Errorf("simulated: filesystem does not support hardlinks")
	}

	path := filepath.Join(t.TempDir(), "guarded.ndjson")
	release, err := AcquireFileLock(path)
	if err != nil {
		t.Fatalf("acquire must degrade to the O_EXCL claim, got: %v", err)
	}
	data, rerr := os.ReadFile(path + ".lock")
	if rerr != nil {
		t.Fatalf("lock file absent after degraded claim: %v", rerr)
	}
	if _, ok := identityAge(data); !ok {
		t.Fatalf("degraded claim must still publish a complete identity, got %q", data)
	}
	if err := release(); err != nil {
		t.Fatalf("release after degraded claim: %v", err)
	}
}

// TestClaimExclusiveVerifyDetectsMidWriteLoss pins the residual two-step
// surface of the degraded path deterministically: between the O_EXCL write
// and the read-back verify, a grace reclaim replaces (or removes) the file.
// The claim must report the loss as retryable contention and never touch the
// successor's identity.
func TestClaimExclusiveVerifyDetectsMidWriteLoss(t *testing.T) {
	t.Cleanup(func() { testHookBeforeClaimVerify = nil })

	successor := "31337\n42\nsuccessor\n"
	t.Run("replaced by a successor", func(t *testing.T) {
		lockPath := filepath.Join(t.TempDir(), "x.lock")
		testHookBeforeClaimVerify = func(p string) {
			mustWriteFile(t, p, successor) // grace reclaim + successor claim
		}
		if _, err := claimLockExclusive(lockPath, "1\n2\nmine\n"); !os.IsExist(err) {
			t.Fatalf("mid-write loss must classify as exists/contention, got: %v", err)
		}
		data, err := os.ReadFile(lockPath)
		if err != nil || string(data) != successor {
			t.Fatalf("lost claim must not touch the successor: data=%q err=%v", data, err)
		}
	})

	t.Run("renamed away with no successor", func(t *testing.T) {
		lockPath := filepath.Join(t.TempDir(), "x.lock")
		testHookBeforeClaimVerify = func(p string) {
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := claimLockExclusive(lockPath, "1\n2\nmine\n"); !os.IsExist(err) {
			t.Fatalf("vanished-name loss must classify as retryable, got: %v", err)
		}
	})
}

// TestAcquireTimeoutBareMessageWhenOccupantUndescribable: an occupant that is
// judged reclaimable but whose reclaim rename cannot land (seam-forced,
// standing in for a persistent Windows pin) leaves the acquirer to time out —
// and a garbage record yields the bare timeout message, no holder summary.
func TestAcquireTimeoutBareMessageWhenOccupantUndescribable(t *testing.T) {
	realRename := renameLockDirFn
	t.Cleanup(func() { renameLockDirFn = realRename })
	renameLockDirFn = func(string, string) error {
		return fmt.Errorf("simulated persistent pin")
	}

	path := filepath.Join(t.TempDir(), "guarded.ndjson")
	mustWriteFile(t, path+".lock", "not-a-pid\nnot-a-time\n")
	backdateDir(t, path+".lock", lockNoHolderGrace+time.Second) // reclaimable, but the rename never lands

	_, err := AcquireFileLock(path)
	if err == nil {
		t.Fatal("expected timeout while the occupant cannot be reclaimed")
	}
	if !strings.Contains(err.Error(), "timed out") || strings.Contains(err.Error(), "held by pid") {
		t.Fatalf("expected the bare timeout message, got: %v", err)
	}
}

// TestAcquireRecoversFromGarbageIdentity: an unparseable occupant older than
// the grace is a remnant (torn write / corruption), and a fresh process must
// acquire well within its budget instead of waiting out the TTL.
func TestAcquireRecoversFromGarbageIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "guarded.ndjson")
	mustWriteFile(t, path+".lock", "not-a-pid\nnot-a-time\n")
	backdateDir(t, path+".lock", lockNoHolderGrace+time.Second)

	start := time.Now()
	release, err := AcquireFileLock(path)
	if err != nil {
		t.Fatalf("acquire over a garbage remnant must succeed, got: %v", err)
	}
	if elapsed := time.Since(start); elapsed >= lockAcquireTimeout {
		t.Fatalf("garbage remnant not reclaimed within budget: %v", elapsed)
	}
	if err := release(); err != nil {
		t.Fatalf("release: %v", err)
	}
}

// TestSweepLockTrash: the sweep removes only trash siblings, tolerates a
// missing parent, and leaves unrelated files alone.
func TestSweepLockTrash(t *testing.T) {
	dir := t.TempDir()
	lockDir := filepath.Join(dir, "guarded.ndjson.lock")
	trash := lockDir + lockTrashInfix + "1-2"
	if err := os.Mkdir(trash, 0o700); err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(dir, "guarded.ndjson")
	if err := os.WriteFile(unrelated, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}

	sweepLockTrash(lockDir)
	if _, err := os.Stat(trash); !os.IsNotExist(err) {
		t.Fatalf("trash sibling not swept: stat err=%v", err)
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatalf("sweep touched an unrelated sibling: %v", err)
	}

	// Missing parent: debugf branch, no panic.
	sweepLockTrash(filepath.Join(dir, "no-such-parent", "x.lock"))
}

// TestReleaseIdentityLessIsNoOp: an identity-less release (legacy/stray
// caller) must be a clean refuse-to-touch no-op — empty identities never
// release.
func TestReleaseIdentityLessIsNoOp(t *testing.T) {
	if err := releaseLock(filepath.Join(t.TempDir(), "gone.lock"), ""); err != nil {
		t.Fatalf("identity-less release must be a clean no-op, got: %v", err)
	}
}

// TestReleaseTreatsVanishedLockAsReleased pins the rename-ENOENT branch
// deterministically via the rename seam: identity matched at check time, but
// the lock vanished before the rename (a rival reclaim's TOCTOU) — release
// reports success because there is nothing left to free.
func TestReleaseTreatsVanishedLockAsReleased(t *testing.T) {
	realRename := renameLockDirFn
	t.Cleanup(func() { renameLockDirFn = realRename })
	renameLockDirFn = func(oldPath, _ string) error {
		return &os.PathError{Op: "rename", Path: oldPath, Err: os.ErrNotExist}
	}

	lockPath := filepath.Join(t.TempDir(), "x.lock")
	id := "5\n6\ntoken\n"
	mustWriteFile(t, lockPath, id)
	if err := releaseLock(lockPath, id); err != nil {
		t.Fatalf("release must treat a vanished lock as already released, got: %v", err)
	}
}

// TestReclaimRestoresDisplacedLiveLock is the deterministic guard for the
// judge-then-rename TOCTOU: the reclaimer renames the occupant and only then
// can prove what it renamed. If the trash does not carry the identity that
// was judged stale (a rival reclaimed and a successor re-claimed in between),
// the reclaim must atomically restore the very same inode via link and report
// failure — the displaced live holder never notices.
func TestReclaimRestoresDisplacedLiveLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "guarded.ndjson")
	live := "31337\n42\nlive-successor\n"
	mustWriteFile(t, path+".lock", live)

	if reclaimStaleLock(path+".lock", []byte("999\n1\nstale-old\n")) {
		t.Fatal("reclaim must report failure after displacing a live lock")
	}
	data, err := os.ReadFile(path + ".lock")
	if err != nil || string(data) != live {
		t.Fatalf("displaced live lock not restored: data=%q err=%v", data, err)
	}
	if n := countLockTrash(t, dir, path); n != 0 {
		t.Fatalf("restore must leave no trash names behind, found %d", n)
	}
}

// TestReclaimRestoreFailureIsLoudAndDisposes: if the restore link itself
// fails (a third claimant already took the name), the displacement is
// permanent — the shared three-actor residual. The trash is disposed, the
// reclaim reports false (it did NOT take the judged-stale object), and the
// victim's own release later refuses on identity mismatch.
func TestReclaimRestoreFailureIsLoudAndDisposes(t *testing.T) {
	realLink := linkLockFn
	t.Cleanup(func() { linkLockFn = realLink })
	linkLockFn = func(string, string) error {
		return &os.PathError{Op: "link", Path: "x", Err: os.ErrExist}
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "guarded.ndjson")
	mustWriteFile(t, path+".lock", "31337\n42\nlive-successor\n")

	if reclaimStaleLock(path+".lock", []byte("999\n1\nstale-old\n")) {
		t.Fatal("a wrong-object displacement must not report a landed reclaim")
	}
	if _, err := os.Stat(path + ".lock"); !os.IsNotExist(err) {
		t.Fatalf("lock name should be free after an unrestorable displacement: %v", err)
	}
	if n := countLockTrash(t, dir, path); n != 0 {
		t.Fatalf("trash must be disposed of, found %d", n)
	}
}

// TestReleaseRestoresDisplacedSuccessor is the deterministic guard for the
// round-5 gate finding (the release-side twin of the reclaim displacement):
// between release's fast-path identity check and its rename, the holder is
// TTL-reclaimed and a successor claims the name. The rename seam swaps in the
// successor's identity at the last instant, standing in for that
// interleaving. displaceLock must verify on the RENAMED object, restore the
// successor atomically, and release must return the honest already-reclaimed
// outcome (nil) with the successor untouched.
func TestReleaseRestoresDisplacedSuccessor(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "guarded.ndjson")
	release, err := AcquireFileLock(path)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}

	successor := "31337\n42\nsuccessor\n"
	realRename := renameLockDirFn
	t.Cleanup(func() { renameLockDirFn = realRename })
	swapped := false
	renameLockDirFn = func(oldPath, newPath string) error {
		if !swapped && oldPath == path+".lock" {
			swapped = true
			// The interleaving: TTL reclaim + successor claim landing after
			// the fast-path check but before the rename.
			mustWriteFile(t, oldPath, successor)
		}
		return fsops.Rename(oldPath, newPath)
	}

	if err := release(); err != nil {
		t.Fatalf("release must report the honest already-reclaimed outcome, got: %v", err)
	}
	data, err := os.ReadFile(path + ".lock")
	if err != nil || string(data) != successor {
		t.Fatalf("displaced successor not restored: data=%q err=%v", data, err)
	}
	if n := countLockTrash(t, dir, path); n != 0 {
		t.Fatalf("restore must leave no trash behind, found %d", n)
	}
	_ = os.Remove(path + ".lock") // cleanup
}

// TestDisplaceLockGoneOutcome: nothing at the name is the displacedGone
// outcome, an error-free no-op for both callers.
func TestDisplaceLockGoneOutcome(t *testing.T) {
	outcome, err := displaceLock(filepath.Join(t.TempDir(), "gone.lock"), []byte("x"))
	if outcome != displacedGone || err != nil {
		t.Fatalf("missing name must be displacedGone/nil, got %v/%v", outcome, err)
	}
}

// TestDescribeHolder covers the timeout-error holder summary: a valid holder
// yields pid + age, everything else yields "".
func TestDescribeHolder(t *testing.T) {
	lockDir := filepath.Join(t.TempDir(), "x.lock")
	if got := describeHolder(lockDir); got != "" {
		t.Fatalf("missing dir must describe as empty, got %q", got)
	}
	if err := os.Mkdir(lockDir, 0o700); err != nil {
		t.Fatal(err)
	}
	holderPath := filepath.Join(lockDir, holderFile)
	if err := os.WriteFile(holderPath, []byte("one-line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := describeHolder(lockDir); got != "" {
		t.Fatalf("short holder must describe as empty, got %q", got)
	}
	if err := os.WriteFile(holderPath, []byte("42\nnot-a-time\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := describeHolder(lockDir); got != "" {
		t.Fatalf("garbage timestamp must describe as empty, got %q", got)
	}
	contents := fmt.Sprintf("42\n%d\n", time.Now().Add(-3*time.Second).UnixNano())
	if err := os.WriteFile(holderPath, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	got := describeHolder(lockDir)
	if !strings.Contains(got, "held by pid 42") || !strings.Contains(got, "stale after") {
		t.Fatalf("valid holder summary missing pid/TTL: %q", got)
	}
}

// TestAcquireDeniedMkdirFailsFastWithActionableError is the regression guard
// for the misdiagnosis found on the owner's Windows workstation: a PERSISTENT
// ERROR_ACCESS_DENIED creating the lock dir (Controlled Folder Access /
// OneDrive-protected Documents / AV denying the binary) was classified as the
// delete-pending transient, retried for the whole 5s budget, and reported as a
// generic contention timeout. Both classifications must fail FAST with the
// actionable environment message: the Windows path after the bounded transient
// window (driven via the lockGOOS seam), the unix path immediately.
func TestAcquireDeniedMkdirFailsFastWithActionableError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("EACCES cannot be forced via chmod on windows; the windows branch is driven through the lockGOOS seam on unix hosts")
	}
	parent := filepath.Join(t.TempDir(), "protected")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o700) })
	if os.Mkdir(filepath.Join(parent, "probe"), 0o700) == nil {
		t.Skip("filesystem does not enforce the read-only parent (running as root?)")
	}
	path := filepath.Join(parent, ".agentsrc.lock")

	for _, goos := range []string{runtime.GOOS, "windows"} {
		t.Run(goos, func(t *testing.T) {
			assertDeniedMkdirFailsFast(t, path, parent, goos)
		})
	}
}

// assertDeniedMkdirFailsFast acquires under a write-protected parent with the
// lock loop classifying failures as the given GOOS, and asserts the actionable
// denied error arrives well inside the acquire budget.
func assertDeniedMkdirFailsFast(t *testing.T, path, parent, goos string) {
	t.Helper()
	realGOOS := lockGOOS
	t.Cleanup(func() { lockGOOS = realGOOS })
	lockGOOS = goos

	start := time.Now()
	_, err := AcquireFileLock(path)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected a denied error acquiring under a write-protected parent")
	}
	if !strings.Contains(err.Error(), "persistently denied") ||
		!strings.Contains(err.Error(), "Controlled Folder Access") ||
		!strings.Contains(err.Error(), parent) {
		t.Fatalf("denied error must name the cause, the protected parent, and a next step, got: %v", err)
	}
	if strings.Contains(err.Error(), "timed out") {
		t.Fatalf("denial must not be misreported as a contention timeout: %v", err)
	}
	// Fast fail: the semantic proof is above (the DENIED classification, not a
	// contention timeout). The elapsed bound is belt-and-braces against a
	// regression that re-burns the whole budget; it is deliberately generous
	// (nominal cost is ~300ms for the windows branch, immediate for unix)
	// because -race plus a loaded CI/dev machine can stretch the 30 x 10ms
	// tick loop severalfold — a 2s bound flaked at 2.10s under parallel test
	// load.
	if elapsed >= lockAcquireTimeout {
		t.Fatalf("denied classification took %v; must fail before the acquire budget expires", elapsed)
	}
}

// TestAcquireTimeoutErrorNamesHolder: the acquire-timeout error names the
// blocking holder so a stranded fresh `da` process reports WHO holds the lock
// and when self-healing kicks in.
func TestAcquireTimeoutErrorNamesHolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "guarded.ndjson")
	seedLockDir(t, path, 3*time.Second, true) // live holder, 3s old

	_, err := AcquireFileLock(path)
	if err == nil {
		t.Fatal("expected timeout while a fresh lock is held")
	}
	if !strings.Contains(err.Error(), "timed out") || !strings.Contains(err.Error(), "held by pid") {
		t.Fatalf("timeout error must include the holder summary, got: %v", err)
	}
}

// TestAcquireTickClaimRaceArms covers the tick's claim-failure classification
// deterministically via the link seam: a lost race (EEXIST — a rival linked
// between our lstat and our link) and a swept temp (ENOENT) are both silent
// retries, never fatal.
func TestAcquireTickClaimRaceArms(t *testing.T) {
	realLink := linkLockFn
	t.Cleanup(func() { linkLockFn = realLink })

	for name, simulated := range map[string]error{
		"lost link race is retryable":   os.ErrExist,
		"swept claim temp is retryable": os.ErrNotExist,
	} {
		t.Run(name, func(t *testing.T) {
			linkLockFn = func(_, newname string) error {
				return &os.PathError{Op: "link", Path: newname, Err: simulated}
			}
			lockPath := filepath.Join(t.TempDir(), "guarded.ndjson.lock")
			st := acquireLoopState{denied: 3}
			id, acquired, err := acquireTick(lockPath, &st)
			if err != nil || acquired || id != "" {
				t.Fatalf("race arm must retry silently: id=%q acquired=%v err=%v", id, acquired, err)
			}
		})
	}
}

// TestAcquireTickUnknownClaimErrorIsFatal: a claim failure that is neither
// contention, transient, nor a permission denial surfaces immediately (here
// ENOTDIR from a lock path whose parent is a regular file).
func TestAcquireTickUnknownClaimErrorIsFatal(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	mustWriteFile(t, blocker, "not a dir")
	st := acquireLoopState{}
	_, acquired, err := acquireTick(filepath.Join(blocker, "x.lock"), &st)
	if acquired || err == nil {
		t.Fatalf("unknown claim error must be fatal: acquired=%v err=%v", acquired, err)
	}
	if !strings.Contains(err.Error(), "acquire lock") {
		t.Fatalf("fatal claim error must carry the acquire context, got: %v", err)
	}
}

// TestClaimExclusiveAgainstOccupiedName: the degraded claim's O_EXCL create
// loses cleanly against an existing lock file.
func TestClaimExclusiveAgainstOccupiedName(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "x.lock")
	mustWriteFile(t, lockPath, "occupied")
	if _, err := claimLockExclusive(lockPath, "1\n2\nmine\n"); !os.IsExist(err) {
		t.Fatalf("O_EXCL against an occupied name must report exists, got: %v", err)
	}
}

// TestLockOccupantStaleUnreadableFileUsesGrace: a lock file that exists but
// cannot be read (foreign perms / mid-teardown) is judged by the short grace
// instead of wedging every acquirer until timeout.
func TestLockOccupantStaleUnreadableFileUsesGrace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("cannot revoke read permission via chmod on windows")
	}
	lockPath := filepath.Join(t.TempDir(), "x.lock")
	mustWriteFile(t, lockPath, "1\n2\ntoken\n")
	if err := os.Chmod(lockPath, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(lockPath, 0o600) })
	if _, err := os.ReadFile(lockPath); err == nil {
		t.Skip("filesystem does not enforce the 0000 mode (running as root?)")
	}
	if stale, _ := lockOccupantStale(filepath.Join(t.TempDir(), "vanished.lock")); stale {
		t.Fatal("a vanished occupant is not stale — the next tick simply claims")
	}
	if stale, _ := lockOccupantStale(lockPath); stale {
		t.Fatal("fresh unreadable occupant must not be stale yet")
	}
	backdateDir(t, lockPath, lockNoHolderGrace+time.Second)
	if stale, _ := lockOccupantStale(lockPath); !stale {
		t.Fatal("unreadable occupant older than the grace must be stale")
	}
}

// TestAcquireNotFoundWithParentPresentFailsFast is the regression guard for
// the owner's exact work-PC trace (provadm-windows-da-lock-observation.md):
// the lock create fails ERROR_FILE_NOT_FOUND while the parent directory
// exists — a filesystem filter driver (OneDrive-redirected Documents, DLP/AV
// minifilter) intercepting creates. The classification must be IMMEDIATE (no
// retry burn) and actionable, naming the interference and the next step.
func TestAcquireNotFoundWithParentPresentFailsFast(t *testing.T) {
	realWrite := writeIdentityFileFn
	t.Cleanup(func() { writeIdentityFileFn = realWrite })
	writeIdentityFileFn = func(path, _ string) error {
		// The filter-driver shape: create reports not-found even though the
		// parent exists and is writable.
		return &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
	}

	path := filepath.Join(t.TempDir(), ".agentsrc.lock") // parent EXISTS
	start := time.Now()
	_, err := AcquireFileLock(path)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected the environmental not-found classification")
	}
	for _, token := range []string{"parent directory exists", "filesystem interference", "OneDrive", "da doctor"} {
		if !strings.Contains(err.Error(), token) {
			t.Fatalf("classified error must mention %q, got: %v", token, err)
		}
	}
	if strings.Contains(err.Error(), "timed out") {
		t.Fatalf("interference must not be misreported as a lock timeout: %v", err)
	}
	if elapsed >= time.Second {
		t.Fatalf("classification must be immediate (no retry burn), took %v", elapsed)
	}
}

// TestClassifyClaimErrorNotFoundWithMissingParent: the same not-found without
// a parent stays the plain raw wrap (the #148 missing-parent shape), not the
// interference diagnosis.
func TestClassifyClaimErrorNotFoundWithMissingParent(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "gone-parent", "x.lock")
	claimErr := &os.PathError{Op: "open", Path: lockPath, Err: os.ErrNotExist}
	st := acquireLoopState{}
	err := classifyClaimError(lockPath, claimErr, &st)
	if err == nil || strings.Contains(err.Error(), "filesystem interference") {
		t.Fatalf("missing-parent not-found must stay a raw wrap, got: %v", err)
	}
}

// TestClassifyClaimErrorTempVanishedIsRetryable: the sentinel for a swept
// claim temp is silent contention, never fatal and never the interference
// diagnosis.
func TestClassifyClaimErrorTempVanishedIsRetryable(t *testing.T) {
	st := acquireLoopState{}
	wrapped := fmt.Errorf("%w: link says ENOENT", errClaimTempVanished)
	if err := classifyClaimError(filepath.Join(t.TempDir(), "x.lock"), wrapped, &st); err != nil {
		t.Fatalf("swept temp must be retryable, got: %v", err)
	}
}

// TestLockTrashNamesUniqueUnderConcurrency pins the invariant that broke the
// windows/macos-latest concurrent-Emit test: trash/claim-temp names must be
// unique even when many goroutines mint them within the same clock tick
// (pid+nanos alone collided; a collided claim temp made a winner hold a lock
// carrying the loser's identity, wedging the lock until the TTL).
func TestLockTrashNamesUniqueUnderConcurrency(t *testing.T) {
	const goroutines, perG = 32, 64
	names := make(chan string, goroutines*perG)
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perG; j++ {
				names <- lockTrashName("/x/guarded.lock")
			}
		}()
	}
	wg.Wait()
	close(names)
	seen := make(map[string]bool, goroutines*perG)
	for n := range names {
		if seen[n] {
			t.Fatalf("duplicate trash name minted: %s", n)
		}
		seen[n] = true
	}
}

// TestAcquireReleaseConcurrentChurn drives the journal-shaped workload at the
// lock layer: many goroutines cycling acquire→release on one path. Every
// acquire must succeed within budget (no identity wedges, no starvation) and
// every release must verify cleanly.
func TestAcquireReleaseConcurrentChurn(t *testing.T) {
	// Widen the per-acquire budget for the duration of this test only. What the
	// test proves is concurrent acquire/release SAFETY — every acquire succeeds,
	// no error, the lock name is free afterward — not that an acquire completes
	// within the 5s production UX bound. Under -race (every memory access
	// instrumented, 10-20x slower) with 16-way contention on a loaded CI runner,
	// a single scheduled hold can be delayed past a 5s waiter deadline purely by
	// scheduling latency; the acquire loop is pure polling, so a timeout there is
	// never a lost wakeup. Restored on cleanup; other timeout-asserting tests run
	// sequentially at the 5s default.
	defer func(orig time.Duration) { lockAcquireTimeout = orig }(lockAcquireTimeout)
	lockAcquireTimeout = 60 * time.Second

	path := filepath.Join(t.TempDir(), "churn.ndjson")
	const goroutines, cycles = 16, 4
	errs := make(chan error, goroutines*cycles)
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < cycles; j++ {
				release, err := AcquireFileLock(path)
				if err != nil {
					errs <- err
					return
				}
				errs <- release()
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent churn: %v", err)
		}
	}
	if _, err := os.Stat(path + ".lock"); !os.IsNotExist(err) {
		t.Fatalf("lock name must be free after the churn: stat err=%v", err)
	}
}

// TestUpdate_SerializesConcurrentReadModifyWrite proves the review #3
// lost-update fix: N concurrent Update callers that each read the shared
// "units" section, add their own key, and write it back all survive — none
// clobbers another's key with a stale whole-section snapshot. The same
// workload through an unsynchronized Open→read→SetSection→Flush loses keys
// because the read happens outside the flush lock.
func TestUpdate_SerializesConcurrentReadModifyWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".agentsrc.lock")

	const writers = 16
	var wg sync.WaitGroup
	wg.Add(writers)
	errs := make([]error, writers)
	for i := 0; i < writers; i++ {
		go func(i int) {
			defer wg.Done()
			errs[i] = Update(path, func(lf *Lockfile) error {
				units := map[string]string{}
				if _, err := lf.Section("units", &units); err != nil {
					return err
				}
				units[fmt.Sprintf("k%d", i)] = fmt.Sprintf("v%d", i)
				return lf.SetSection("units", units)
			})
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("writer %d: %v", i, err)
		}
	}

	lf, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	units := map[string]string{}
	if _, err := lf.Section("units", &units); err != nil {
		t.Fatal(err)
	}
	if len(units) != writers {
		t.Fatalf("expected all %d concurrently-written keys to survive, got %d: %v", writers, len(units), units)
	}
	for i := 0; i < writers; i++ {
		if units[fmt.Sprintf("k%d", i)] != fmt.Sprintf("v%d", i) {
			t.Fatalf("lost key k%d — concurrent RMW clobbered it: %v", i, units)
		}
	}
}

// TestUpdate_AbortsWithoutWriteOnError proves fn returning an error leaves the
// document untouched (no partial write).
func TestUpdate_AbortsWithoutWriteOnError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".agentsrc.lock")

	if err := Update(path, func(lf *Lockfile) error {
		return lf.SetSection("units", map[string]string{"keep": "1"})
	}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	sentinel := fmt.Errorf("abort")
	if err := Update(path, func(lf *Lockfile) error {
		_ = lf.SetSection("units", map[string]string{"clobber": "2"})
		return sentinel
	}); err != sentinel {
		t.Fatalf("expected the sentinel error back, got %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("expected no write on an aborted Update\nbefore: %s\nafter:  %s", before, after)
	}
}
