package platform

import "os"

// platformIO is the narrow filesystem collaborator the platform package's
// MkdirAll / Remove / WriteFile call sites need. It is the interface-DI
// replacement for the legacy var osMkdirAll = os.MkdirAll-style func-var
// seams formerly defined in seams.go (see docs/TEST_SEAMS.md and the
// seam-interface-di-migration plan).
//
// Scope: exactly the three filesystem mutators whose err != nil branches
// cannot be triggered from a writable tmp fixture (MkdirAll on a tmp dir,
// Remove of a known-present file, WriteFile to a writable path). Every
// production call passes stdPlatformIO{}; tests pass fakePlatformIO with
// the specific operation overridden to return an injected error.
//
// The whole platform package shares one role-named interface rather than
// per-file duplicates because every file's seam need is the same role —
// "the filesystem mutator the platform package uses for refresh-time IO".
// Splitting it per file would be mechanical duplication, not narrower
// scoping. The platform package IS the unit (matching the convention's
// "convert the whole package as one unit" rule).
type platformIO interface {
	MkdirAll(path string, perm os.FileMode) error
	Remove(path string) error
	WriteFile(name string, data []byte, perm os.FileMode) error
}

// stdPlatformIO is the production platformIO backed by the os package. It
// is what every NewClaude / NewCodex / NewCursor / NewCopilot / NewOpenCode
// constructor and every free-function call site passes when there is no
// test override.
type stdPlatformIO struct{}

func (stdPlatformIO) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (stdPlatformIO) Remove(path string) error { return os.Remove(path) }

func (stdPlatformIO) WriteFile(name string, data []byte, perm os.FileMode) error {
	return os.WriteFile(name, data, perm)
}
