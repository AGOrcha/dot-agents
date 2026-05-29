package fsops

import (
	"fmt"
	"os"
	"path/filepath"
)

// atomicTempFile is the subset of *os.File that WriteFileAtomic uses. It is an
// interface so tests can inject a temp file whose Write or Close fails, covering
// the atomic-write error paths deterministically (filesystem permission tricks
// are not portable across CI hosts).
type atomicTempFile interface {
	Write([]byte) (int, error)
	Close() error
	Name() string
}

// createTemp is the seam for opening the atomic-write temp file. It defaults to
// os.CreateTemp and is overridable in tests.
var createTemp = func(dir, pattern string) (atomicTempFile, error) {
	return os.CreateTemp(dir, pattern)
}

// renameFunc is the seam for the final rename. It defaults to os.Rename and is
// overridable in tests so the rename-failure cleanup path is coverable without
// platform-specific filesystem tricks.
var renameFunc = os.Rename

// WriteFileAtomic writes data to path atomically: a temp file in the same
// directory is written and closed, then renamed into place, so a concurrent
// reader never observes a partial file. The parent directory must already
// exist. The resulting file carries os.CreateTemp's owner-only mode (0600),
// matching the prior sidecar writers; callers needing a different mode chmod
// the final path themselves.
//
// This is the single atomic-write primitive shared by the YAML sidecar writers
// (scoring, review/labels) and the lockfile writer; callers marshal their own
// bytes and hand them here rather than re-implementing temp+rename.
func WriteFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := createTemp(dir, ".fsops-*.tmp")
	if err != nil {
		return fmt.Errorf("fsops: temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("fsops: write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("fsops: close temp: %w", err)
	}
	if err := renameFunc(tmpPath, path); err != nil {
		cleanup()
		return fmt.Errorf("fsops: rename: %w", err)
	}
	return nil
}
