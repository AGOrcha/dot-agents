package store

import "os"

// Test-only handles onto the filesystem seams, so the external store_test
// package can force the mkdir / atomic-write / stat failure branches
// deterministically. Each returns a restore func the test defers.

// SetMkdirAll overrides the directory-create seam.
func SetMkdirAll(fn func(string, os.FileMode) error) (restore func()) {
	prev := mkdirAll
	mkdirAll = fn
	return func() { mkdirAll = prev }
}

// SetWriteFileAtomic overrides the atomic file-write seam.
func SetWriteFileAtomic(fn func(string, []byte) error) (restore func()) {
	prev := writeFileAtomic
	writeFileAtomic = fn
	return func() { writeFileAtomic = prev }
}

// SetStatPath overrides the adopt-in-place existence-check seam.
func SetStatPath(fn func(string) (os.FileInfo, error)) (restore func()) {
	prev := statPath
	statPath = fn
	return func() { statPath = prev }
}
