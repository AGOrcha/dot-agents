package settings

import "testing"

// TestSettingsSeams_NoCollaboratorSeamsYet documents the current state of
// the settings subpackage: no seam interfaces have been introduced because
// the package does not yet talk to a symlink-style collaborator. This
// placeholder will be replaced by real seam tests once the interface-DI
// follow-up (plan root-command-decomposition t15) lands collaborators
// that need injection.
func TestSettingsSeams_NoCollaboratorSeamsYet(t *testing.T) {
	// Compile-time presence check: keeps the file in the build set so the
	// seams_test.go target exists for the per-subpackage convention even
	// while the subpackage has nothing to seam.
}
