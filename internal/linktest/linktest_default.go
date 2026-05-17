//go:build !windows

package linktest

import "os"

// createManagedLink creates a POSIX symlink. POSIX symlinks may freely
// point at directories or files and may dangle, so this also backs the
// dangling case.
func createManagedLink(target, link string) error {
	return os.Symlink(target, link)
}

// createDanglingLink creates a symlink to a path that does not exist.
func createDanglingLink(link, missingTarget string) error {
	return os.Symlink(missingTarget, link)
}
