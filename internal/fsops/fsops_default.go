//go:build !windows

// Package fsops provides filesystem operations with OS-appropriate
// implementations. On POSIX the operations are thin wrappers over the
// standard library; on Windows they add cmd/PowerShell fallbacks for the
// cases where the Win32 layer rejects an operation the Go runtime issued
// (e.g. RemoveAll on a directory containing a junction).
package fsops

import "os"

// MkdirAll creates a directory path and all missing parents.
func MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

// WriteFile writes data to path, creating it if necessary.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	return os.WriteFile(path, data, perm)
}

// Remove removes a single file or empty directory.
func Remove(path string) error {
	return os.Remove(path)
}

// RemoveAll removes path and any children it contains.
func RemoveAll(path string) error {
	return os.RemoveAll(path)
}
