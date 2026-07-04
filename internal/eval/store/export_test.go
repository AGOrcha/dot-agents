package store

import (
	"os"

	"github.com/AGOrcha/dot-agents/internal/scoring"
)

// Test-only handles onto the filesystem seams, so the external store_test
// package can force the atomic-write / rename / mkdir failure branches
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

// SetRenameDir overrides the commit-rename seam.
func SetRenameDir(fn func(string, string) error) (restore func()) {
	prev := renameDir
	renameDir = fn
	return func() { renameDir = prev }
}

// SetStatPath overrides the run-dir existence-check seam.
func SetStatPath(fn func(string) (os.FileInfo, error)) (restore func()) {
	prev := statPath
	statPath = fn
	return func() { statPath = prev }
}

// SetWriteIterationScore overrides the score-persist seam.
func SetWriteIterationScore(fn func(string, scoring.Score) (string, error)) (restore func()) {
	prev := writeIterationScore
	writeIterationScore = fn
	return func() { writeIterationScore = prev }
}
