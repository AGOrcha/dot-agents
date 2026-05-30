package agentslock

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

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

func TestFlushWriteError(t *testing.T) {
	// Parent dir does not exist → fsops.WriteFileAtomic's temp-create fails.
	lf, _ := Open(filepath.Join(t.TempDir(), "no-such-dir", "x.lock"))
	if err := lf.Flush(); err == nil {
		t.Fatal("expected write error when parent dir is missing")
	}
}

func TestFlushMarshalError(t *testing.T) {
	// White-box: inject an invalid RawMessage so MarshalIndent fails.
	lf, _ := Open(filepath.Join(t.TempDir(), "x.lock"))
	lf.doc["broken"] = json.RawMessage(`{invalid`)
	if err := lf.Flush(); err == nil {
		t.Fatal("expected marshal error with an invalid raw section")
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
