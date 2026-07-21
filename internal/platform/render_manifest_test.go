package platform

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/testutil"
)

func TestLoadRenderManifest_AbsentAndCorruptAreEmpty(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if m := loadRenderManifest(); len(m.Entries) != 0 || m.SchemaVersion != renderManifestSchemaVersion {
		t.Fatalf("absent manifest must be empty/versioned, got %+v", m)
	}
	if err := os.MkdirAll(filepath.Dir(renderManifestPath()), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(renderManifestPath(), []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}
	if m := loadRenderManifest(); len(m.Entries) != 0 {
		t.Errorf("corrupt manifest must degrade to empty, got %+v", m)
	}
	// A render must still work over a corrupt manifest (never blocks).
	dst := filepath.Join(t.TempDir(), "f")
	if err := writeManagedFile(stdPlatformIO{}, dst, []byte("x")); err != nil {
		t.Fatalf("render over corrupt manifest: %v", err)
	}
}

// TestRecordRenderHash_BestEffortOnWriteFailure drives the two best-effort
// branches via a fakePlatformIO whose MkdirAll / WriteFile return synthetic
// errors. recordRenderHash must not panic in either case (the file is
// already correct on disk; the manifest is best-effort persistence).
func TestRecordRenderHash_BestEffortOnWriteFailure(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	mkdirFail := &fakePlatformIO{
		mkdirAll: func(string, fs.FileMode) error { return errors.New("mkdir boom") },
	}
	recordRenderHash(mkdirFail, "/x/y", "deadbeef") // mkdir-fail branch: must not panic

	writeFail := &fakePlatformIO{
		writeFile: func(string, []byte, fs.FileMode) error { return errors.New("write boom") },
	}
	recordRenderHash(writeFail, "/x/y", "deadbeef") // write-fail branch: best-effort, swallowed
}

// TestSidecarBackup_ReadAndWriteErrors verifies the missing-source ReadFile
// error and the injected WriteFile error both propagate from sidecarBackup.
func TestSidecarBackup_ReadAndWriteErrors(t *testing.T) {
	tmp := t.TempDir()
	if err := sidecarBackup(stdPlatformIO{}, filepath.Join(tmp, "missing")); err == nil {
		t.Error("backup of a missing file must error")
	}
	src := filepath.Join(tmp, "f")
	if err := os.WriteFile(src, []byte("d"), 0644); err != nil {
		t.Fatal(err)
	}
	writeFail := &fakePlatformIO{
		writeFile: func(string, []byte, fs.FileMode) error { return errors.New("no space") },
	}
	if err := sidecarBackup(writeFail, src); err == nil {
		t.Error("backup write failure must propagate")
	}
}

func TestWriteManagedFile_ProvenanceGatesOverwrite(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir()) // isolate the render manifest
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "sub", "settings.json")

	// 1. Fresh render: file created, provenance recorded.
	if err := writeManagedFile(stdPlatformIO{}, dst, []byte("v1")); err != nil {
		t.Fatalf("fresh render: %v", err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "v1" {
		t.Fatalf("want v1, got %q", string(b))
	}

	// 2. Identical re-render: no-op, no backup.
	if err := writeManagedFile(stdPlatformIO{}, dst, []byte("v1")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dst + ".dot-agents-backup"); !os.IsNotExist(err) {
		t.Error("identical re-render must not back up")
	}

	// 3. We own it (on-disk hash == recorded) → template change overwrites
	//    freely, NO backup.
	if err := writeManagedFile(stdPlatformIO{}, dst, []byte("v2-our-template-changed")); err != nil {
		t.Fatalf("our-render overwrite: %v", err)
	}
	if _, err := os.Stat(dst + ".dot-agents-backup"); !os.IsNotExist(err) {
		t.Error("overwriting our own prior render must not back up")
	}
	if b, _ := os.ReadFile(dst); string(b) != "v2-our-template-changed" {
		t.Fatalf("want v2, got %q", string(b))
	}

	// 4. User edits the file out of band, then a refresh renders new
	//    content → the user edit is preserved via backup, then replaced.
	if err := os.WriteFile(dst, []byte("USER HAND EDIT"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := writeManagedFile(stdPlatformIO{}, dst, []byte("v3")); err != nil {
		t.Fatalf("user-edited render: %v", err)
	}
	bak, err := os.ReadFile(dst + ".dot-agents-backup")
	if err != nil || string(bak) != "USER HAND EDIT" {
		t.Fatalf("user edit must be backed up verbatim, got %q err=%v", string(bak), err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "v3" {
		t.Fatalf("want v3 after backup+replace, got %q", string(b))
	}
}

// writeManifestFile persists a hand-crafted manifest at the canonical path.
func writeManifestFile(t *testing.T, m renderManifest) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(renderManifestPath()), 0755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(renderManifestPath(), data, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadRenderManifest_SchemaSkewIsUntrusted(t *testing.T) {
	cases := []struct {
		name    string
		version int
	}{
		{"missing/zero version", 0},
		{"future version", renderManifestSchemaVersion + 1},
		{"unknown older version", renderManifestSchemaVersion - 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			writeManifestFile(t, renderManifest{
				SchemaVersion: tc.version,
				Entries: map[string]renderManifestEntry{
					"/some/path": {SHA256: "deadbeef", RenderedAt: "2026-01-01T00:00:00Z"},
				},
			})
			m := loadRenderManifest()
			if len(m.Entries) != 0 {
				t.Fatalf("schema_version %d must be untrusted (empty), got %+v", tc.version, m)
			}
			if m.SchemaVersion != renderManifestSchemaVersion {
				t.Fatalf("untrusted manifest must report current schema, got %d", m.SchemaVersion)
			}
		})
	}
}

// A FUTURE-schema entry whose hash matches the on-disk file must NOT suppress
// BackupBeforeOverwrite. An older binary cannot understand future entry
// semantics, so it must conservatively preserve the divergent file.
func TestWriteManagedFile_FutureSchemaEntryDoesNotSuppressBackup(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "settings.json")

	onDisk := []byte("content from a newer binary")
	if err := os.WriteFile(dst, onDisk, 0644); err != nil {
		t.Fatal(err)
	}
	// A future-schema manifest claiming this exact on-disk content is ours.
	writeManifestFile(t, renderManifest{
		SchemaVersion: renderManifestSchemaVersion + 1,
		Entries: map[string]renderManifestEntry{
			manifestKey(dst): {SHA256: renderContentHash(onDisk), RenderedAt: "2026-01-01T00:00:00Z"},
		},
	})

	if err := writeManagedFile(stdPlatformIO{}, dst, []byte("rerendered")); err != nil {
		t.Fatalf("writeManagedFile: %v", err)
	}
	bak, err := os.ReadFile(dst + ".dot-agents-backup")
	if err != nil {
		t.Fatalf("future-schema entry must NOT suppress backup; no backup found: %v", err)
	}
	if string(bak) != string(onDisk) {
		t.Fatalf("divergent file must be backed up verbatim, got %q", string(bak))
	}
	if b, _ := os.ReadFile(dst); string(b) != "rerendered" {
		t.Fatalf("want rerendered after backup, got %q", string(b))
	}
}

func TestWriteManagedFile_BackupFailurePreservesUserEdit(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "settings.json")
	if err := writeManagedFile(stdPlatformIO{}, dst, []byte("rendered")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("precious user edit"), 0644); err != nil {
		t.Fatal(err)
	}

	// BackupBeforeOverwrite is the deliberate forward-compat extension point
	// (see render_manifest.go docstring) — not a func-var test seam. Tests
	// swap it via its runtime-swap contract; the seam-migration plan does
	// not target it.
	orig := BackupBeforeOverwrite
	BackupBeforeOverwrite = func(string) error { return os.ErrPermission }
	t.Cleanup(func() { BackupBeforeOverwrite = orig })

	if err := writeManagedFile(stdPlatformIO{}, dst, []byte("new")); err == nil {
		t.Fatal("backup failure must abort the overwrite")
	}
	if b, _ := os.ReadFile(dst); string(b) != "precious user edit" {
		t.Errorf("user edit must survive a failed backup, got %q", string(b))
	}
}

// An existing destination that exists but is unreadable (e.g. perms) could
// hold an unsaved user edit we can neither compare nor back up. Overwriting
// it must block, not silently destroy it while reporting success.
func TestWriteManagedFile_UnreadableExistingFileBlocksAndPreserves(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "settings.json")
	const precious = "precious unsaved user edit"
	if err := os.WriteFile(dst, []byte(precious), 0644); err != nil {
		t.Fatal(err)
	}
	// Scope the unreadability to a subtest so the helper's t.Cleanup
	// restores access before we read the file back in the outer test.
	// This matters on Windows where MakeFileUnreadable holds a
	// byte-range lock for the lifetime of its test scope — a same-scope
	// read-after-overwrite-attempt would itself fail with
	// ERROR_LOCK_VIOLATION. POSIX behaves the same way once we route
	// through the cross-platform helper. Using a subtest yields a clean
	// "denial active during writeManagedFile; access restored before
	// readback" envelope on both platforms.
	t.Run("overwrite-blocked-while-unreadable", func(t *testing.T) {
		// testutil.MakeFileUnreadable covers both POSIX (chmod 0) and
		// Windows (byte-range lock) and skips on root-on-POSIX where
		// mode bits do not enforce read denial.
		testutil.MakeFileUnreadable(t, dst)

		if err := writeManagedFile(stdPlatformIO{}, dst, []byte("rerendered")); err == nil {
			t.Fatal("unreadable existing destination must block the overwrite")
		}
	})

	// Subtest finished; helper Cleanup released the denial. Now the
	// outer test can read the bytes and assert preservation.
	if b, _ := os.ReadFile(dst); string(b) != precious {
		t.Errorf("original file must survive an unreadable-destination refresh, got %q", string(b))
	}
}

// resetRenderManifestCache clears the process-global render-manifest cache so a
// test's reload-count assertions are deterministic regardless of test order.
func resetRenderManifestCache(t *testing.T) {
	t.Helper()
	renderManifestMu.Lock()
	defer renderManifestMu.Unlock()
	renderManifestCache = nil
	renderManifestCacheSig = renderManifestSig{}
	renderManifestReloads = 0
}

func renderManifestReloadCount() int {
	renderManifestMu.Lock()
	defer renderManifestMu.Unlock()
	return renderManifestReloads
}

// The H6 cache must serve an unchanged manifest from memory: repeated reads of a
// stable file trigger exactly one disk read+parse, not one per read (the N-1
// redundant-read elimination this optimization exists for).
func TestRenderManifestCache_WarmReuseSkipsReparse(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	resetRenderManifestCache(t)
	dst := "/proj/.claude/settings.json"
	writeManifestFile(t, renderManifest{
		SchemaVersion: renderManifestSchemaVersion,
		Entries:       map[string]renderManifestEntry{manifestKey(dst): {SHA256: "seeded"}},
	})

	for range 5 {
		if got := renderManifestHash(dst); got != "seeded" {
			t.Fatalf("warm read: want seeded, got %q", got)
		}
		if m := loadRenderManifest(); m.Entries[manifestKey(dst)].SHA256 != "seeded" {
			t.Fatalf("warm load: want seeded entry, got %+v", m.Entries)
		}
	}
	if n := renderManifestReloadCount(); n != 1 {
		t.Fatalf("10 reads of an unchanged manifest must reload once, reloaded %d times", n)
	}
}

// The sole in-process writer (recordRenderHash) must keep the cache coherent:
// load → record → load must observe the new hash, never a stale parse, and
// without forcing a disk reparse (the chokepoint updates the cache in place).
func TestRenderManifestCache_RecordIsObservedWithoutReparse(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	resetRenderManifestCache(t)
	dst := "/proj/.claude/settings.json"

	if got := renderManifestHash(dst); got != "" {
		t.Fatalf("absent entry must be empty, got %q", got)
	}
	recordRenderHash(stdPlatformIO{}, dst, "hashA")
	if got := renderManifestHash(dst); got != "hashA" {
		t.Fatalf("record then read: want hashA, got %q (stale serve)", got)
	}
	recordRenderHash(stdPlatformIO{}, dst, "hashB")
	if got := renderManifestHash(dst); got != "hashB" {
		t.Fatalf("second record then read: want hashB, got %q (stale serve)", got)
	}
	// Only the initial empty load hit the disk; every subsequent read/record
	// was served/mutated in memory.
	if n := renderManifestReloadCount(); n != 1 {
		t.Fatalf("chokepoint updates must not reparse: reloaded %d times, want 1", n)
	}
}

// The stat signature must catch a writer that bypasses recordRenderHash (a
// direct file edit / a different process): a mutation between reads is observed.
func TestRenderManifestCache_ExternalWriteIsObserved(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	resetRenderManifestCache(t)
	dst := "/proj/.claude/settings.json"

	recordRenderHash(stdPlatformIO{}, dst, "hashA")
	if got := renderManifestHash(dst); got != "hashA" {
		t.Fatalf("want hashA before external write, got %q", got)
	}

	// External writer replaces the manifest (extra entry ⇒ different size, and
	// bump mtime so the signature shifts on any filesystem clock resolution).
	writeManifestFile(t, renderManifest{
		SchemaVersion: renderManifestSchemaVersion,
		Entries: map[string]renderManifestEntry{
			manifestKey(dst):      {SHA256: "hashB"},
			manifestKey("/other"): {SHA256: "hashC"},
		},
	})
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(renderManifestPath(), future, future); err != nil {
		t.Fatal(err)
	}

	if got := renderManifestHash(dst); got != "hashB" {
		t.Fatalf("external write must invalidate the cache: want hashB, got %q (stale serve)", got)
	}
}

// The cache is keyed by the manifest path, so a change of XDG_STATE_HOME (a
// different manifest) never leaks the previous root's entries.
func TestRenderManifestCache_PathChangeReloads(t *testing.T) {
	resetRenderManifestCache(t)
	dst := "/proj/.claude/settings.json"

	t.Setenv("XDG_STATE_HOME", t.TempDir())
	writeManifestFile(t, renderManifest{
		SchemaVersion: renderManifestSchemaVersion,
		Entries:       map[string]renderManifestEntry{manifestKey(dst): {SHA256: "rootA"}},
	})
	if got := renderManifestHash(dst); got != "rootA" {
		t.Fatalf("root A: want rootA, got %q", got)
	}

	t.Setenv("XDG_STATE_HOME", t.TempDir())
	writeManifestFile(t, renderManifest{
		SchemaVersion: renderManifestSchemaVersion,
		Entries:       map[string]renderManifestEntry{manifestKey(dst): {SHA256: "rootB"}},
	})
	if got := renderManifestHash(dst); got != "rootB" {
		t.Fatalf("root B must not serve root A's cache: want rootB, got %q", got)
	}
}
