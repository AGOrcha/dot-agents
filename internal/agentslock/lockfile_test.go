package agentslock

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

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
	// Hold a FRESH lock so the waiter does not reclaim it as stale; release it
	// from a goroutine partway through the waiter's retry loop. The waiter must
	// then acquire and succeed (not time out).
	seedLockDir(t, path, 0, true)

	lf, _ := Open(path)
	_ = lf.SetSection("config", configSection{Layers: map[string]string{"x": "y"}})

	go func() {
		time.Sleep(200 * time.Millisecond)
		_ = os.RemoveAll(path + ".lock")
	}()

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
	if !lockIsStale(lockDir) {
		t.Fatal("short holder + old dir mtime must be stale")
	}
	// Fresh dir mtime with the same short holder must NOT be stale.
	now := time.Now()
	if err := os.Chtimes(lockDir, now, now); err != nil {
		t.Fatal(err)
	}
	if lockIsStale(lockDir) {
		t.Fatal("short holder + fresh dir mtime must not be stale")
	}
}

func TestDirOlderThanTTLMissingDirIsStale(t *testing.T) {
	// A dir that cannot be stat'd (does not exist) is treated as stale.
	if !dirOlderThanTTL(filepath.Join(t.TempDir(), "no-such-lock")) {
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

func TestUnlockFileLockSurfacesRemoveError(t *testing.T) {
	// Force RemoveAll of the lock dir to fail by stripping write permission from
	// its parent so the child cannot be unlinked. unlockFileLock must take the
	// debugf error branch without panicking. Skipped where the FS does not
	// enforce the perm bit (e.g. running as root).
	parent := filepath.Join(t.TempDir(), "ro-parent")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	lockDir := filepath.Join(parent, "x.lock")
	if err := os.Mkdir(lockDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o700) }) // let TempDir cleanup proceed

	t.Setenv("DA_DEBUG", "1")
	unlockFileLock(lockDir) // must not panic; exercises the RemoveAll-error + debugf branch

	if _, err := os.Stat(lockDir); os.IsNotExist(err) {
		t.Skip("filesystem removed the lock dir despite a read-only parent; cannot force RemoveAll error here")
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
	go func() {
		time.Sleep(200 * time.Millisecond)
		_ = release()
	}()

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
