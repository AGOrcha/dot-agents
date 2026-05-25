package kg

// Test-side fake for kgIO. Each func field, when nil, delegates to the real
// os.* / encoding/json impl (the nil-delegates-to-real convention from
// docs/TEST_SEAMS.md). A test that wants to fault-inject one operation sets
// only the matching func field; the others fall through unchanged.
//
// This replaces the legacy `var osMkdirAll = os.MkdirAll` package-level
// func-var seams formerly in seams.go (see seam-interface-di-migration plan
// / pr40-artifacts atomic convergence and PR #59 platform-pkg conversion).

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"
)

// errSeam is the sentinel error returned from injected kgIO stubs. It is the
// drop-in replacement for the package-level errSeam previously declared in
// seams_test.go.
var errSeam = errors.New("seam: injected failure")

// fakeKGIO implements kgIO with per-operation overrides. A nil override
// delegates to the real os / json impl so a test only has to define the
// failure point it wants to exercise.
type fakeKGIO struct {
	mkdirAll      func(path string, perm fs.FileMode) error
	writeFile     func(name string, data []byte, perm fs.FileMode) error
	readFile      func(name string) ([]byte, error)
	openFile      func(name string, flag int, perm fs.FileMode) (*os.File, error)
	rename        func(oldpath, newpath string) error
	readDir       func(name string) ([]os.DirEntry, error)
	marshalIndent func(v any, prefix, indent string) ([]byte, error)
}

func (f *fakeKGIO) MkdirAll(path string, perm os.FileMode) error {
	if f.mkdirAll != nil {
		return f.mkdirAll(path, perm)
	}
	return os.MkdirAll(path, perm)
}

func (f *fakeKGIO) WriteFile(name string, data []byte, perm os.FileMode) error {
	if f.writeFile != nil {
		return f.writeFile(name, data, perm)
	}
	return os.WriteFile(name, data, perm)
}

func (f *fakeKGIO) ReadFile(name string) ([]byte, error) {
	if f.readFile != nil {
		return f.readFile(name)
	}
	return os.ReadFile(name)
}

func (f *fakeKGIO) OpenFile(name string, flag int, perm os.FileMode) (*os.File, error) {
	if f.openFile != nil {
		return f.openFile(name, flag, perm)
	}
	return os.OpenFile(name, flag, perm)
}

func (f *fakeKGIO) Rename(oldpath, newpath string) error {
	if f.rename != nil {
		return f.rename(oldpath, newpath)
	}
	return os.Rename(oldpath, newpath)
}

func (f *fakeKGIO) ReadDir(name string) ([]os.DirEntry, error) {
	if f.readDir != nil {
		return f.readDir(name)
	}
	return os.ReadDir(name)
}

func (f *fakeKGIO) MarshalIndent(v any, prefix, indent string) ([]byte, error) {
	if f.marshalIndent != nil {
		return f.marshalIndent(v, prefix, indent)
	}
	return json.MarshalIndent(v, prefix, indent)
}

// withMkdirAllError returns a fake whose MkdirAll fails for any path
// containing want (or all paths when want == "") and delegates the other
// operations to the real impl. The retval mirrors the legacy swapMkdirAll
// API in semantic intent — a test that drives a specific MkdirAll failure
// builds the fake and passes it to the unit under test.
func withMkdirAllError(t *testing.T, want string) *fakeKGIO {
	t.Helper()
	return &fakeKGIO{
		mkdirAll: func(path string, _ fs.FileMode) error {
			if want == "" || strings.Contains(path, want) {
				return errSeam
			}
			return os.MkdirAll(path, 0755)
		},
	}
}

// withWriteFileError returns a fake whose WriteFile fails for any path
// containing want.
func withWriteFileError(t *testing.T, want string) *fakeKGIO {
	t.Helper()
	return &fakeKGIO{
		writeFile: func(name string, data []byte, perm fs.FileMode) error {
			if want == "" || strings.Contains(name, want) {
				return errSeam
			}
			return os.WriteFile(name, data, perm)
		},
	}
}

// withReadFileError returns a fake whose ReadFile fails for any path
// containing want.
func withReadFileError(t *testing.T, want string) *fakeKGIO {
	t.Helper()
	return &fakeKGIO{
		readFile: func(name string) ([]byte, error) {
			if want == "" || strings.Contains(name, want) {
				return nil, errSeam
			}
			return os.ReadFile(name)
		},
	}
}

// withOpenFileError returns a fake whose OpenFile fails for any path
// containing want.
func withOpenFileError(t *testing.T, want string) *fakeKGIO {
	t.Helper()
	return &fakeKGIO{
		openFile: func(name string, flag int, perm fs.FileMode) (*os.File, error) {
			if want == "" || strings.Contains(name, want) {
				return nil, errSeam
			}
			return os.OpenFile(name, flag, perm)
		},
	}
}

// withRenameError returns a fake whose Rename fails for any path containing
// want.
func withRenameError(t *testing.T, want string) *fakeKGIO {
	t.Helper()
	return &fakeKGIO{
		rename: func(oldpath, newpath string) error {
			if want == "" || strings.Contains(oldpath, want) || strings.Contains(newpath, want) {
				return errSeam
			}
			return os.Rename(oldpath, newpath)
		},
	}
}

// withReadDirError returns a fake whose ReadDir fails for any path containing
// want.
func withReadDirError(t *testing.T, want string) *fakeKGIO {
	t.Helper()
	return &fakeKGIO{
		readDir: func(name string) ([]os.DirEntry, error) {
			if want == "" || strings.Contains(name, want) {
				return nil, errSeam
			}
			return os.ReadDir(name)
		},
	}
}

// withMarshalIndentError returns a fake whose MarshalIndent always returns
// the seam error.
func withMarshalIndentError(t *testing.T) *fakeKGIO {
	t.Helper()
	return &fakeKGIO{
		marshalIndent: func(any, string, string) ([]byte, error) { return nil, errSeam },
	}
}

// testIO is the production kgIO every kg test that does not fault-inject
// should pass to the unit under test. It is the test-side counterpart of
// kgIOFrom(deps) — when a test has no seam to inject it threads testIO()
// through the API surface so the production stdKGIO{} is in play.
func testIO() kgIO { return stdKGIO{} }
