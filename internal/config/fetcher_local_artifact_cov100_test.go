package config

// Coverage-directed tests driving the last unreached error legs of
// fetcher_local_artifact.go to 100% line coverage under the per-file ratchet.
// They exercise the fstat-on-open-fd failures (statOpenFile seam), the
// ReadDir non-EOF error and the defensive zero-batch termination (readDirBatch
// seam), the capped-read failure and the two post-read size-divergence legs
// (readCappedFile seam), and the single-file TOCTOU where the entry vanishes
// between the pre-open Lstat and the confined read. Helpers use the fetch100
// prefix to avoid colliding with the package's other test helpers.

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// TestFetch100StatOpenFileErrors covers the fh.Stat() error legs shared by
// openConfinedDir and readRootFile via the statOpenFile seam — an fstat that
// fails on a descriptor that opened cleanly.
func TestFetch100StatOpenFileErrors(t *testing.T) {
	orig := statOpenFile
	defer func() { statOpenFile = orig }()
	statOpenFile = func(*os.File) (fs.FileInfo, error) { return nil, errors.New("fstat boom") }

	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		t.Fatalf("OpenRoot: %v", err)
	}
	defer func() { _ = root.Close() }()

	dirInfo, err := os.Lstat(filepath.Join(base, "d"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := openConfinedDir(root, "d", dirInfo); err == nil {
		t.Fatal("expected an fstat error from openConfinedDir")
	}

	fileInfo, err := os.Lstat(filepath.Join(base, "f"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := readRootFile(root, "f", fileInfo, DefaultBundleLimits().orDefault()); err == nil {
		t.Fatal("expected an fstat error from readRootFile")
	}
}

// fetch100OpenDir opens a confined directory root plus the pre-open Lstat that
// streamConfinedDir verifies against, for the readDirBatch-seam tests.
func fetch100OpenDir(t *testing.T) (*os.Root, fs.FileInfo) {
	t.Helper()
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		t.Fatalf("OpenRoot: %v", err)
	}
	t.Cleanup(func() { _ = root.Close() })
	info, err := os.Lstat(filepath.Join(base, "d"))
	if err != nil {
		t.Fatal(err)
	}
	return root, info
}

// TestFetch100ReadDirBatchNonEOFError covers streamConfinedDir's non-EOF
// ReadDir error leg via the readDirBatch seam.
func TestFetch100ReadDirBatchNonEOFError(t *testing.T) {
	orig := readDirBatch
	defer func() { readDirBatch = orig }()
	readDirBatch = func(*os.File, int) ([]os.DirEntry, error) {
		return nil, errors.New("readdir boom")
	}
	root, info := fetch100OpenDir(t)
	err := streamConfinedDir(root, "d", info, func(os.DirEntry) error { return nil })
	if err == nil {
		t.Fatal("expected the non-EOF ReadDir error to surface")
	}
}

// TestFetch100ReadDirBatchZeroNoError covers streamConfinedDir's defensive
// len(batch)==0 termination — a (0, nil) ReadDir return that a real *os.File
// never produces (it signals exhaustion via io.EOF).
func TestFetch100ReadDirBatchZeroNoError(t *testing.T) {
	orig := readDirBatch
	defer func() { readDirBatch = orig }()
	readDirBatch = func(*os.File, int) ([]os.DirEntry, error) {
		return nil, nil
	}
	root, info := fetch100OpenDir(t)
	if err := streamConfinedDir(root, "d", info, func(os.DirEntry) error { return nil }); err != nil {
		t.Fatalf("zero-batch termination should return nil, got %v", err)
	}
}

// fetch100OpenRegular writes a regular file under a fresh root and returns the
// confined root, the file's relative name, and its pre-open Lstat.
func fetch100OpenRegular(t *testing.T, content []byte) (*os.Root, string, fs.FileInfo) {
	t.Helper()
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "f"), content, 0o644); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		t.Fatalf("OpenRoot: %v", err)
	}
	t.Cleanup(func() { _ = root.Close() })
	info, err := os.Lstat(filepath.Join(base, "f"))
	if err != nil {
		t.Fatal(err)
	}
	return root, "f", info
}

// TestFetch100ReadCappedFileReadError covers readRootFile's io.ReadAll error
// leg via the readCappedFile seam.
func TestFetch100ReadCappedFileReadError(t *testing.T) {
	orig := readCappedFile
	defer func() { readCappedFile = orig }()
	readCappedFile = func(*os.File, int64) ([]byte, error) {
		return nil, errors.New("read boom")
	}
	root, rel, info := fetch100OpenRegular(t, []byte("abc"))
	if _, _, _, err := readRootFile(root, rel, info, DefaultBundleLimits().orDefault()); err == nil {
		t.Fatal("expected the capped-read error to surface")
	}
}

// TestFetch100ReadCappedFileExceedsCap covers readRootFile's content-over-cap
// leg: the fstat size passes the gate but the actual read overruns the cap.
func TestFetch100ReadCappedFileExceedsCap(t *testing.T) {
	orig := readCappedFile
	defer func() { readCappedFile = orig }()
	readCappedFile = func(*os.File, int64) ([]byte, error) {
		return make([]byte, 5), nil
	}
	root, rel, info := fetch100OpenRegular(t, []byte("x"))
	limits := DefaultBundleLimits()
	limits.MaxFileBytes = 4
	if _, _, _, err := readRootFile(root, rel, info, limits); err == nil {
		t.Fatal("expected a content-exceeds-cap error")
	}
}

// TestFetch100ReadCappedFileSizeMismatch covers readRootFile's grown/shrunk
// leg: the byte count read no longer matches the fstat size.
func TestFetch100ReadCappedFileSizeMismatch(t *testing.T) {
	orig := readCappedFile
	defer func() { readCappedFile = orig }()
	readCappedFile = func(*os.File, int64) ([]byte, error) {
		return []byte("ab"), nil
	}
	root, rel, info := fetch100OpenRegular(t, []byte("abc"))
	if _, _, _, err := readRootFile(root, rel, info, DefaultBundleLimits().orDefault()); err == nil {
		t.Fatal("expected an fstat-size-mismatch error")
	}
}

// TestFetch100SingleFileVanishesAfterLstat covers readSingleFileArtifact's
// IsNotExist branch: the file classified by the pre-open Lstat is gone by the
// time the confined read opens it (TOCTOU), so readRootFile's Open fails ENOENT.
func TestFetch100SingleFileVanishesAfterLstat(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "gone.txt")
	if err := os.WriteFile(target, []byte("bye"), 0o644); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		t.Fatalf("OpenRoot: %v", err)
	}
	defer func() { _ = root.Close() }()
	fi, err := os.Lstat(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}

	f := &localArtifactFetcher{}
	parts := PackageRefParts{SourceID: "s", ArtifactPath: "gone.txt"}
	_, err = f.readSingleFileArtifact(root, "gone.txt", fi, parts, PostureFromSource(Source{Type: "local"}), false, "")
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonNotFound {
		t.Fatalf("want not_found for a vanished single-file artifact, got %v", err)
	}
}
