//go:build windows

package links

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/NikashPrakash/dot-agents/internal/fsops"
)

func createLink(target, linkPath string) error {
	if err := os.Symlink(target, linkPath); err == nil {
		return nil
	} else {
		info, statErr := os.Stat(target)
		if statErr != nil {
			return err
		}
		if info.IsDir() {
			if junctionErr := createJunction(linkPath, target); junctionErr == nil {
				return nil
			} else {
				return fmt.Errorf("create symlink: %v; junction fallback: %w", err, junctionErr)
			}
		}
		hardlinkErr := os.Link(target, linkPath)
		if hardlinkErr == nil {
			return nil
		}
		content, readErr := os.ReadFile(target)
		if readErr != nil {
			return fmt.Errorf("create symlink: %v; hardlink fallback: %v; read fallback source: %w", err, hardlinkErr, readErr)
		}
		if copyErr := fsops.WriteFile(linkPath, content, info.Mode().Perm()); copyErr == nil {
			return nil
		} else {
			return fmt.Errorf("create symlink: %v; hardlink fallback: %v; copy fallback: %w", err, hardlinkErr, copyErr)
		}
	}
}

func createJunction(linkPath, target string) error {
	cmd := exec.Command("cmd", "/c", "mklink", "/J", linkPath, target)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("mklink /J failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}