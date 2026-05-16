package platform

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadRenderManifest_AbsentAndCorruptAreEmpty(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if m := loadRenderManifest(); len(m.Entries) != 0 || m.SchemaVersion != renderManifestSchemaVersion {
		t.Fatalf("absent manifest must be empty/versioned, got %+v", m)
	}
	if err := osMkdirAll(filepath.Dir(renderManifestPath()), 0755); err != nil {
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
	if err := writeManagedFile(dst, []byte("x")); err != nil {
		t.Fatalf("render over corrupt manifest: %v", err)
	}
}

func TestRecordRenderHash_BestEffortOnWriteFailure(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	origMk, origWr := osMkdirAll, osWriteFile
	t.Cleanup(func() { osMkdirAll, osWriteFile = origMk, origWr })

	osMkdirAll = func(string, os.FileMode) error { return errors.New("mkdir boom") }
	recordRenderHash("/x/y", "deadbeef") // mkdir-fail branch: must not panic

	osMkdirAll = origMk
	osWriteFile = func(string, []byte, os.FileMode) error { return errors.New("write boom") }
	recordRenderHash("/x/y", "deadbeef") // write-fail branch: best-effort, swallowed
}

func TestSidecarBackup_ReadAndWriteErrors(t *testing.T) {
	tmp := t.TempDir()
	if err := sidecarBackup(filepath.Join(tmp, "missing")); err == nil {
		t.Error("backup of a missing file must error")
	}
	src := filepath.Join(tmp, "f")
	if err := os.WriteFile(src, []byte("d"), 0644); err != nil {
		t.Fatal(err)
	}
	origWr := osWriteFile
	t.Cleanup(func() { osWriteFile = origWr })
	osWriteFile = func(string, []byte, os.FileMode) error { return errors.New("no space") }
	if err := sidecarBackup(src); err == nil {
		t.Error("backup write failure must propagate")
	}
}

func TestWriteManagedFile_ProvenanceGatesOverwrite(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir()) // isolate the render manifest
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "sub", "settings.json")

	// 1. Fresh render: file created, provenance recorded.
	if err := writeManagedFile(dst, []byte("v1")); err != nil {
		t.Fatalf("fresh render: %v", err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "v1" {
		t.Fatalf("want v1, got %q", string(b))
	}

	// 2. Identical re-render: no-op, no backup.
	if err := writeManagedFile(dst, []byte("v1")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dst + ".dot-agents-backup"); !os.IsNotExist(err) {
		t.Error("identical re-render must not back up")
	}

	// 3. We own it (on-disk hash == recorded) → template change overwrites
	//    freely, NO backup.
	if err := writeManagedFile(dst, []byte("v2-our-template-changed")); err != nil {
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
	if err := writeManagedFile(dst, []byte("v3")); err != nil {
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

func TestWriteManagedFile_BackupFailurePreservesUserEdit(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "settings.json")
	if err := writeManagedFile(dst, []byte("rendered")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("precious user edit"), 0644); err != nil {
		t.Fatal(err)
	}

	orig := BackupBeforeOverwrite
	BackupBeforeOverwrite = func(string) error { return os.ErrPermission }
	t.Cleanup(func() { BackupBeforeOverwrite = orig })

	if err := writeManagedFile(dst, []byte("new")); err == nil {
		t.Fatal("backup failure must abort the overwrite")
	}
	if b, _ := os.ReadFile(dst); string(b) != "precious user edit" {
		t.Errorf("user edit must survive a failed backup, got %q", string(b))
	}
}
