//go:build windows

package linktest

import (
	"fmt"
	"os"

	"github.com/AGOrcha/dot-agents/internal/links"
)

// createManagedLink mirrors the production Windows link model by delegating
// to the single production implementation (junction for directory targets,
// hard link for file targets). Reusing links.CreateManagedLink keeps test
// support from drifting from production and avoids duplicating the Win32
// reparse-point machinery.
func createManagedLink(target, link string) error {
	return links.CreateManagedLink(target, link)
}

// createDanglingLink produces a broken reparse point: create a real
// directory, junction to it (via the production primitive), then remove the
// directory. os.Readlink on the junction still returns missingTarget, but
// os.Stat of it now fails — the same shape the broken-link detectors
// expect. A hard link cannot dangle (its target must exist), so a junction
// is the only viable analogue.
func createDanglingLink(link, missingTarget string) error {
	if err := os.MkdirAll(missingTarget, 0o755); err != nil {
		return fmt.Errorf("seed dangling target %s: %w", missingTarget, err)
	}
	if err := links.CreateJunction(link, missingTarget); err != nil {
		return err
	}
	if err := os.RemoveAll(missingTarget); err != nil {
		return fmt.Errorf("remove dangling target %s: %w", missingTarget, err)
	}
	return nil
}
