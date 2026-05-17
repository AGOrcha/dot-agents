//go:build !windows

package links

import "os"

// createLink creates a POSIX symlink at linkPath pointing to target.
func createLink(target, linkPath string) error {
	return os.Symlink(target, linkPath)
}
