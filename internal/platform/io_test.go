package platform

// Test-side fake for platformIO. Each func field, when nil, delegates to
// the real os.* impl (the nil-delegates-to-real convention from
// docs/TEST_SEAMS.md). A test that wants to fault-inject one operation
// sets only the matching func field; the others fall through unchanged.
//
// This replaces the legacy `var osMkdirAll = os.MkdirAll` package-level
// func-var seams formerly in seams.go (see seam-interface-di-migration
// plan / pr40-artifacts atomic convergence).

import (
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"
)

var errSeamSynthetic = errors.New("seam synthetic failure")

// fakePlatformIO implements platformIO with per-operation overrides. A nil
// override delegates to the real os impl so a test only has to define the
// failure point it wants to exercise.
type fakePlatformIO struct {
	mkdirAll  func(path string, perm fs.FileMode) error
	remove    func(path string) error
	writeFile func(name string, data []byte, perm fs.FileMode) error
}

func (f *fakePlatformIO) MkdirAll(path string, perm os.FileMode) error {
	if f.mkdirAll != nil {
		return f.mkdirAll(path, perm)
	}
	return os.MkdirAll(path, perm)
}

func (f *fakePlatformIO) Remove(path string) error {
	if f.remove != nil {
		return f.remove(path)
	}
	return os.Remove(path)
}

func (f *fakePlatformIO) WriteFile(name string, data []byte, perm os.FileMode) error {
	if f.writeFile != nil {
		return f.writeFile(name, data, perm)
	}
	return os.WriteFile(name, data, perm)
}

// newFakeIOMkdirAllError returns a fake whose MkdirAll fails for any path
// containing want (or all paths when want == "") and delegates Remove /
// WriteFile to the real os impl.
func newFakeIOMkdirAllError(want string) *fakePlatformIO {
	return &fakePlatformIO{
		mkdirAll: func(path string, _ fs.FileMode) error {
			if want == "" || strings.Contains(path, want) {
				return errSeamSynthetic
			}
			return os.MkdirAll(path, 0755)
		},
	}
}

// newFakeIOMkdirAllErrorAfter returns a fake whose MkdirAll succeeds for
// the first failAt-1 matching calls (want substring) and fails the Nth.
// Non-matching paths always succeed via the real impl. This drives a
// specific call in a chain where earlier MkdirAlls must succeed.
func newFakeIOMkdirAllErrorAfter(want string, failAt int) *fakePlatformIO {
	count := 0
	return &fakePlatformIO{
		mkdirAll: func(path string, perm fs.FileMode) error {
			if want == "" || strings.Contains(path, want) {
				count++
				if count == failAt {
					return errSeamSynthetic
				}
			}
			return os.MkdirAll(path, perm)
		},
	}
}

// newFakeIORemoveError returns a fake whose Remove fails for any path
// containing want.
func newFakeIORemoveError(want string) *fakePlatformIO {
	return &fakePlatformIO{
		remove: func(path string) error {
			if want == "" || strings.Contains(path, want) {
				return errSeamSynthetic
			}
			return os.Remove(path)
		},
	}
}

// newFakeIOWriteFileError returns a fake whose WriteFile fails for any
// path containing want.
func newFakeIOWriteFileError(want string) *fakePlatformIO {
	return &fakePlatformIO{
		writeFile: func(name string, data []byte, perm fs.FileMode) error {
			if want == "" || strings.Contains(name, want) {
				return errSeamSynthetic
			}
			return os.WriteFile(name, data, perm)
		},
	}
}

// stdIO is the production platformIO every test that does not fault-inject
// should use (and every prod constructor passes).
func stdIO() platformIO { return stdPlatformIO{} }

// withMkdirAllError returns a fake configured to fail MkdirAll for paths
// containing want. The retval mirrors the legacy with*Error API surface
// (used by the test bodies that build the receiver via the constructor and
// then assign c.io = fake before calling the unit under test).
func withMkdirAllError(t *testing.T, want string) *fakePlatformIO {
	t.Helper()
	return newFakeIOMkdirAllError(want)
}

func withMkdirAllErrorAfter(t *testing.T, want string, failAt int) *fakePlatformIO {
	t.Helper()
	return newFakeIOMkdirAllErrorAfter(want, failAt)
}

func withRemoveError(t *testing.T, want string) *fakePlatformIO {
	t.Helper()
	return newFakeIORemoveError(want)
}

func withWriteFileError(t *testing.T, want string) *fakePlatformIO {
	t.Helper()
	return newFakeIOWriteFileError(want)
}
