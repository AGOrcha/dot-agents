//go:build !windows

package links

import "os"

func createLink(target, linkPath string) error {
	return os.Symlink(target, linkPath)
}