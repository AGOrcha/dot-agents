package crgbehavior

import (
	"errors"
	"testing"
)

// The release stores absolute paths. Normalizing them is the only thing that
// puts both sides of the gate in one id space, so a normalizer that rewrites
// too much (a substring replace) or too little silently manufactures agreement
// or a total divergence. These cases pin the CLEAN PREFIX contract.
func TestNormalizerAppliesCleanPrefixSemantics(t *testing.T) {
	n := NewNormalizer("/abs/repo/")
	cases := []struct{ name, in, want string }{
		{"absolute path under root", "/abs/repo/pkg/a.go", "pkg/a.go"},
		{"redundant separators are cleaned", "/abs/repo//pkg/./a.go", "pkg/a.go"},
		{"already relative is left alone", "pkg/a.go", "pkg/a.go"},
		{"the root itself", "/abs/repo", "."},
		{"windows separators fold to slashes", `/abs/repo\pkg\a.go`, "pkg/a.go"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := n.Path(c.in)
			if err != nil {
				t.Fatalf("Path(%q): %v", c.in, err)
			}
			if got != c.want {
				t.Fatalf("Path(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// A root with a prefix-like sibling is the exact case a strings.ReplaceAll
// normalizer gets wrong: "/abs/repo-vendor/x.go" is NOT under "/abs/repo", and
// rewriting it would invent an id that resolves to nothing.
func TestNormalizerRejectsPathsOutsideTheRoot(t *testing.T) {
	n := NewNormalizer("/abs/repo")
	for _, in := range []string{"/abs/repo-vendor/x.go", "/elsewhere/a.go", "../outside/a.go", `C:\other\a.go`} {
		if got, err := n.Path(in); !errors.Is(err, ErrOutsideRoot) {
			t.Fatalf("Path(%q) = %q, %v; want ErrOutsideRoot", in, got, err)
		}
	}
}

// A qualified name that embeds the root text later in the string must keep it:
// only the leading path component is a path.
func TestNormalizerRewritesOnlyThePathPrefix(t *testing.T) {
	n := NewNormalizer("/abs/repo")
	got, err := n.Qualified("/abs/repo/pkg/a.go::Render/abs/repo/template")
	if err != nil {
		t.Fatalf("Qualified: %v", err)
	}
	if want := "pkg/a.go::Render/abs/repo/template"; got != want {
		t.Fatalf("Qualified = %q, want %q (only the prefix may be trimmed)", got, want)
	}
}

// An unresolved bare call target names no file; running it through path
// normalization would reject or mangle it.
func TestNormalizerPassesBareTargetsThrough(t *testing.T) {
	n := NewNormalizer("/abs/repo")
	got, err := n.Qualified("append")
	if err != nil || got != "append" {
		t.Fatalf("Qualified(\"append\") = %q, %v; want it returned unchanged", got, err)
	}
}

// A FILE node's qualified name is its path, with no "::" separator. It must be
// trimmed like any other path: left absolute it carries the materialization
// root into the compared id space, so the native and bridge sides stop sharing
// one id space and a recorded baseline becomes specific to the machine that
// produced it.
func TestNormalizerTrimsFileNodeQualifiedNames(t *testing.T) {
	cases := map[string]struct {
		root, in, want string
		wantErr        bool
	}{
		"posix file node":     {root: "/abs/repo", in: "/abs/repo/pkg/a.go", want: "pkg/a.go"},
		"the root itself":     {root: "/abs/repo", in: "/abs/repo", want: "."},
		"windows file node":   {root: `c:\repo`, in: `C:\Repo\pkg\a.go`, want: "pkg/a.go"},
		"outside the root":    {root: "/abs/repo", in: "/elsewhere/a.go", wantErr: true},
		"prefix-like sibling": {root: "/abs/repo", in: "/abs/repo-vendor/a.go", wantErr: true},
		// Still a bare target, not a path: no separator AND not absolute.
		"bare target":        {root: "/abs/repo", in: "append", want: "append"},
		"dotted bare target": {root: "/abs/repo", in: "console.log", want: "console.log"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := NewNormalizer(c.root).Qualified(c.in)
			if c.wantErr {
				if !errors.Is(err, ErrOutsideRoot) {
					t.Fatalf("Qualified(%q) = %q, %v; want ErrOutsideRoot", c.in, got, err)
				}
				return
			}
			if err != nil || got != c.want {
				t.Fatalf("Qualified(%q) = %q, %v; want %q", c.in, got, err, c.want)
			}
		})
	}
}

// Under a Windows root the platform itself is case-insensitive, so a graph
// built as C:\Repo must still normalize against a root spelled c:\repo. Under a
// POSIX root the comparison stays case-sensitive.
func TestNormalizerCasePolicyFollowsTheRoot(t *testing.T) {
	win := NewNormalizer(`c:\repo`)
	got, err := win.Path(`C:\Repo\pkg\a.go`)
	if err != nil {
		t.Fatalf("windows Path: %v", err)
	}
	if want := "pkg/a.go"; got != want {
		t.Fatalf("windows Path = %q, want %q", got, want)
	}
	posix := NewNormalizer("/abs/repo")
	if _, err := posix.Path("/ABS/REPO/pkg/a.go"); !errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("posix Path accepted a case-folded root; want ErrOutsideRoot, got %v", err)
	}
}

// An empty root is the pre-normalized-fixture shape: clean, do not trim.
func TestNormalizerWithoutRootOnlyCleans(t *testing.T) {
	n := NewNormalizer("")
	got, err := n.Path("/already/absolute/a.go")
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if want := "/already/absolute/a.go"; got != want {
		t.Fatalf("Path = %q, want %q", got, want)
	}
	if _, err := n.Path(""); err == nil {
		t.Fatal("an empty stored path must be an error")
	}
}

// Root() is what every trim slices against, so it must be the cleaned,
// slash-separated root with NO trailing separator: a root that kept one would
// slice one character too many off every path under it, and a root that
// cleaned to "." would trim a leading directory instead of nothing.
func TestNnTNormalizerRootIsCleanAndSeparatorFree(t *testing.T) {
	cases := []struct{ name, root, want string }{
		{"posix root", "/abs/repo", "/abs/repo"},
		{"trailing separator is dropped", "/abs/repo/", "/abs/repo"},
		{"redundant separators are cleaned", "/abs//repo/./", "/abs/repo"},
		{"windows drive root", `C:\abs\repo`, "C:/abs/repo"},
		{"windows drive root with a trailing separator", `C:\abs\repo\`, "C:/abs/repo"},
		{"unc root keeps its leading pair", `\\server\share\repo`, "//server/share/repo"},
		{"separator-only root collapses to the filesystem root", `\\`, "/"},
		{"dot root is the pre-normalized shape", ".", ""},
		{"dot-slash root is the pre-normalized shape", "./", ""},
		{"empty root is the pre-normalized shape", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := NewNormalizer(c.root).Root(); got != c.want {
				t.Fatalf("NewNormalizer(%q).Root() = %q, want %q", c.root, got, c.want)
			}
		})
	}
}

// A graph built on Windows stores drive-lettered, backslashed paths. The gate
// may run on any host, so the drive root's own case-insensitivity is what
// decides the comparison — and it must still reject a prefix-like sibling.
func TestNnTNormalizerUnderAWindowsDriveRoot(t *testing.T) {
	n := NewNormalizer(`C:\abs\repo`)
	cases := []struct{ name, in, want string }{
		{"mixed separators and folded case", `c:\ABS\Repo/pkg\sub\a.go`, "pkg/sub/a.go"},
		{"the root itself", `C:\abs\repo`, "."},
		{"redundant separators", `C:\abs\repo\\pkg\.\a.go`, "pkg/a.go"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := n.Path(c.in)
			if err != nil {
				t.Fatalf("Path(%q): %v", c.in, err)
			}
			if got != c.want {
				t.Fatalf("Path(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
	if got, err := n.Path(`C:\abs\repo-vendor\x.go`); !errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("Path(sibling) = %q, %v; want ErrOutsideRoot even under a folding root", got, err)
	}
	// The symbol part is preserved verbatim — a language whose symbol names
	// contain backslashes must not have them folded into path separators.
	got, err := n.Qualified(`C:\abs\repo\pkg\a.go::Widget\Render`)
	if err != nil {
		t.Fatalf("Qualified: %v", err)
	}
	if want := `pkg/a.go::Widget\Render`; got != want {
		t.Fatalf("Qualified = %q, want %q", got, want)
	}
}

// A UNC root's leading "//" is significant: path.Clean would collapse it, and a
// collapsed root no longer prefixes the paths stored under it. The share is
// case-insensitive like any Windows volume.
func TestNnTNormalizerUnderAUNCRoot(t *testing.T) {
	n := NewNormalizer(`\\Server\Share\repo`)
	got, err := n.Path(`\\server\share\repo\pkg\a.go`)
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if want := "pkg/a.go"; got != want {
		t.Fatalf("Path = %q, want %q", got, want)
	}
	if got, err := n.Path("//server/share/other/a.go"); !errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("Path(other share) = %q, %v; want ErrOutsideRoot", got, err)
	}
}

// An ANCESTOR of the root is not under the root. It is shorter than the root
// text, so a prefix test that indexed before checking the length would panic
// or — worse — read a match out of the shorter string.
func TestNnTNormalizerRejectsAnAncestorOfTheRoot(t *testing.T) {
	n := NewNormalizer("/abs/repo")
	for _, in := range []string{"/abs", "/", "/a"} {
		if got, err := n.Path(in); !errors.Is(err, ErrOutsideRoot) {
			t.Fatalf("Path(%q) = %q, %v; want ErrOutsideRoot", in, got, err)
		}
	}
}

// A Windows root is recognized by shape, not by the host the gate runs on. The
// shape is letter + ':' + separator: "C:name" is a drive-RELATIVE path, not a
// root, and treating it as absolute would reject it as outside the root
// instead of normalizing it.
func TestNnTHasDriveLetterRequiresALetterColonSeparator(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"backslash drive root", `C:\abs`, true},
		{"slash drive root", "c:/abs", true},
		{"bare drive root", "Z:/", true},
		{"drive-relative path", "C:abs", false},
		{"drive letter alone", "C:", false},
		{"empty", "", false},
		{"posix path", "/abs/repo", false},
		{"digit is not a drive letter", "4:/net", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := hasDriveLetter(c.in); got != c.want {
				t.Fatalf("hasDriveLetter(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}
