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
	"path/filepath"
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

// ---------------------------------------------------------------------------
// Frontmatter / agent body reader coverage (relocated from coverage_gap_test.go).
// ---------------------------------------------------------------------------

// TestReadFrontmatterEdgeCases exercises the frontmatter parser.
func TestReadFrontmatterEdgeCases(t *testing.T) {
	tmp := t.TempDir()
	cases := []struct {
		name    string
		content string
		want    map[string]string
	}{
		{
			name:    "no-frontmatter",
			content: "# heading\nbody\n",
			want:    map[string]string{},
		},
		{
			name:    "valid-frontmatter",
			content: "---\nname: x\ndesc: y\n---\nbody\n",
			want:    map[string]string{"name": "x", "desc": "y"},
		},
		{
			name:    "unterminated-frontmatter",
			content: "---\nname: x\nno-end\n",
			want:    map[string]string{"name": "x", "no-end": ""},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(tmp, tc.name+".md")
			if err := os.WriteFile(path, []byte(tc.content), 0644); err != nil {
				t.Fatal(err)
			}
			got := readFrontmatter(path)
			for k, v := range tc.want {
				if got[k] != v {
					t.Errorf("[%s] %s = %q, want %q", tc.name, k, got[k], v)
				}
			}
		})
	}
	if got := readFrontmatter(filepath.Join(tmp, "no-such")); got != nil {
		t.Errorf("expected nil for missing file, got %+v", got)
	}
}

// TestReadAgentBody covers the frontmatter-stripping reader.
func TestReadAgentBody(t *testing.T) {
	tmp := t.TempDir()
	cases := []struct {
		content, want string
		hasErr        bool
	}{
		{"---\nname: x\n---\n\nbody\n", "body\n", false},
		{"plain body\n", "plain body\n", false},
		{"---\nno-end", "---\nno-end", false},
	}
	for i, tc := range cases {
		p := filepath.Join(tmp, "x"+itoa(i)+".md")
		if err := os.WriteFile(p, []byte(tc.content), 0644); err != nil {
			t.Fatal(err)
		}
		got, err := readAgentBody(p)
		if (err != nil) != tc.hasErr {
			t.Errorf("[%d] err=%v hasErr=%v", i, err, tc.hasErr)
		}
		if got != tc.want {
			t.Errorf("[%d] got %q, want %q", i, got, tc.want)
		}
	}
	if _, err := readAgentBody("/no/such"); err == nil {
		t.Error("expected error for missing file")
	}
}
