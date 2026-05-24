package kg

import (
	"encoding/json"
	"os"
)

// kgIO is the narrow filesystem / serialization collaborator the commands/kg
// package needs. It is the interface-DI replacement for the legacy
// var osMkdirAll = os.MkdirAll-style func-var seams formerly defined in
// seams.go (see docs/TEST_SEAMS.md and the seam-interface-di-migration plan
// — specifically the platform-pkg / commands/add.go reference shapes).
//
// Scope: exactly the seven operations whose err != nil branches cannot be
// triggered from a writable tmp fixture or a stdlib-typed value
// (MkdirAll on a tmp dir, WriteFile to a writable path, ReadFile of a
// present file, OpenFile on a writable path, Rename within a tmp tree,
// ReadDir of a present directory, json.MarshalIndent of a well-typed
// struct). Every production call passes stdKGIO{}; tests pass fakeKGIO
// with the specific operation overridden to return an injected error.
//
// The whole kg package shares one role-named interface rather than per-file
// duplicates because every file's seam need is the same role — "the
// filesystem / serialization collaborator the kg subcommands use for graph
// IO". Splitting it per file would be mechanical duplication, not narrower
// scoping. The kg package IS the unit (matching the convention's "convert
// the whole package as one unit" rule from docs/TEST_SEAMS.md).
type kgIO interface {
	MkdirAll(path string, perm os.FileMode) error
	WriteFile(name string, data []byte, perm os.FileMode) error
	ReadFile(name string) ([]byte, error)
	OpenFile(name string, flag int, perm os.FileMode) (*os.File, error)
	Rename(oldpath, newpath string) error
	ReadDir(name string) ([]os.DirEntry, error)
	MarshalIndent(v any, prefix, indent string) ([]byte, error)
}

// stdKGIO is the production kgIO backed by the os and encoding/json packages.
// It is what every Cobra handler (via Deps.IO) and every direct production
// caller passes when there is no test override.
type stdKGIO struct{}

func (stdKGIO) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (stdKGIO) WriteFile(name string, data []byte, perm os.FileMode) error {
	return os.WriteFile(name, data, perm)
}

func (stdKGIO) ReadFile(name string) ([]byte, error) { return os.ReadFile(name) }

func (stdKGIO) OpenFile(name string, flag int, perm os.FileMode) (*os.File, error) {
	return os.OpenFile(name, flag, perm)
}

func (stdKGIO) Rename(oldpath, newpath string) error { return os.Rename(oldpath, newpath) }

func (stdKGIO) ReadDir(name string) ([]os.DirEntry, error) { return os.ReadDir(name) }

func (stdKGIO) MarshalIndent(v any, prefix, indent string) ([]byte, error) {
	return json.MarshalIndent(v, prefix, indent)
}
