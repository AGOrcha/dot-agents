package main

import (
	"bytes"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// parseToPkg builds a synthetic *packages.Package from inline Go source so the
// AST-walking detector can be exercised without invoking the real toolchain.
// filename is used both for the parse Fset and as the on-disk-style path the
// position resolves to, which lets tests assert the module-relative reporting.
func parseToPkg(t *testing.T, pkgPath, filename, src string) *packages.Package {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v", filename, err)
	}
	return &packages.Package{
		PkgPath: pkgPath,
		Fset:    fset,
		Syntax:  []*ast.File{file},
	}
}

// srcWithCalls is a small Go file template that imports os under the given local
// name and emits each provided call inside a function body, one per line.
func srcWithCalls(importSpec string, calls ...string) string {
	var b strings.Builder
	b.WriteString("package sample\n\nimport " + importSpec + "\n\nfunc f() {\n")
	for _, c := range calls {
		b.WriteString("\t" + c + "\n")
	}
	b.WriteString("}\n")
	return b.String()
}

// TestScanPackageDetectsMutators is the core table: it confirms scanPackage
// flags exactly the raw os.* WRITE primitives and ignores reads, non-os
// selectors, and aliased-but-unrelated identifiers. fsguard's own package is
// allowlisted, so these synthetic packages use a non-fsguard path.
func TestScanPackageDetectsMutators(t *testing.T) {
	const pkg = modulePath + "/internal/sample"
	const file = "/src/dot-agents/internal/sample/sample.go"

	tests := []struct {
		name       string
		importSpec string
		calls      []string
		wantCalls  []string // expected "os.X" findings, in source order
	}{
		{
			name:       "all six mutators flagged",
			importSpec: `"os"`,
			calls: []string{
				`_ = os.Mkdir("d", 0)`,
				`_ = os.MkdirAll("d", 0)`,
				`_ = os.Remove("f")`,
				`_ = os.RemoveAll("d")`,
				`_ = os.Rename("a", "b")`,
				`_ = os.WriteFile("f", nil, 0)`,
			},
			wantCalls: []string{"os.Mkdir", "os.MkdirAll", "os.Remove", "os.RemoveAll", "os.Rename", "os.WriteFile"},
		},
		{
			name:       "reads and non-mutators ignored",
			importSpec: `"os"`,
			calls: []string{
				`_, _ = os.Open("f")`,
				`_, _ = os.ReadFile("f")`,
				`_, _ = os.Stat("f")`,
				`_ = os.Getpid()`,
			},
			wantCalls: nil,
		},
		{
			name:       "aliased os import still caught",
			importSpec: `myos "os"`,
			calls:      []string{`_ = myos.Mkdir("d", 0)`},
			wantCalls:  []string{"os.Mkdir"},
		},
		{
			name:       "unrelated package selector ignored",
			importSpec: `"os"`,
			calls:      []string{`_ = notos.Mkdir("d")`},
			wantCalls:  nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := parseToPkg(t, pkg, file, srcWithCalls(tc.importSpec, tc.calls...))
			got := scanPackage(p)
			if len(got) != len(tc.wantCalls) {
				t.Fatalf("got %d findings, want %d: %+v", len(got), len(tc.wantCalls), got)
			}
			for i, want := range tc.wantCalls {
				if got[i].call != want {
					t.Errorf("finding %d: got %q, want %q", i, got[i].call, want)
				}
				if got[i].relPath != "internal/sample/sample.go" {
					t.Errorf("finding %d: relPath = %q, want module-relative", i, got[i].relPath)
				}
			}
		})
	}
}

// TestCheckPackagesSkipsFsopsAndErrors confirms the package-level filtering:
// internal/fsops is exempt (it IS the abstraction), and error-tagged / nil /
// empty packages are skipped. A non-fsops package's mutator fires.
func TestCheckPackagesSkipsFsopsAndErrors(t *testing.T) {
	good := parseToPkg(t, modulePath+"/internal/other",
		"/src/dot-agents/internal/other/o.go",
		srcWithCalls(`"os"`, `_ = os.Mkdir("d", 0)`))
	fsops := parseToPkg(t, fsopsPkg,
		"/src/dot-agents/internal/fsops/fsops_default.go",
		srcWithCalls(`"os"`, `_ = os.Mkdir("d", 0)`))
	errPkg := parseToPkg(t, modulePath+"/internal/broken",
		"/src/dot-agents/internal/broken/b.go",
		srcWithCalls(`"os"`, `_ = os.Mkdir("d", 0)`))
	errPkg.Errors = []packages.Error{{Msg: "synthetic load failure"}}

	got := checkPackages([]*packages.Package{
		nil,
		{PkgPath: ""},
		fsops,  // exempt
		errPkg, // skipped (errors)
		good,   // fires
	})
	if len(got) != 1 {
		t.Fatalf("got %d findings, want 1: %+v", len(got), got)
	}
	if got[0].pkgPath != modulePath+"/internal/other" || got[0].call != "os.Mkdir" {
		t.Errorf("unexpected finding: %+v", got[0])
	}
}

// TestCheckPackagesSortsFindings drives the stable-sort comparator: two findings
// in different files (exercising the relPath-differs branch) plus a same-file
// pair from one package with two calls (exercising the line tiebreak). The
// result must come back ordered by relPath then line.
func TestCheckPackagesSortsFindings(t *testing.T) {
	zed := parseToPkg(t, modulePath+"/internal/zed",
		"/src/dot-agents/internal/zed/z.go",
		srcWithCalls(`"os"`, `_ = os.Mkdir("d", 0)`))
	abc := parseToPkg(t, modulePath+"/internal/abc",
		"/src/dot-agents/internal/abc/a.go",
		srcWithCalls(`"os"`, `_ = os.Mkdir("d", 0)`, `_ = os.Remove("e")`))

	got := checkPackages([]*packages.Package{zed, abc})
	if len(got) != 3 {
		t.Fatalf("got %d findings, want 3: %+v", len(got), got)
	}
	wantPaths := []string{
		"internal/abc/a.go",
		"internal/abc/a.go",
		"internal/zed/z.go",
	}
	for i, want := range wantPaths {
		if got[i].relPath != want {
			t.Errorf("finding[%d].relPath = %q, want %q", i, got[i].relPath, want)
		}
	}
	if got[0].line >= got[1].line {
		t.Errorf("same-file findings not line-ordered: %d then %d", got[0].line, got[1].line)
	}
}

// TestAllowed covers the allowlist decision matrix: fsguard's own package is
// always allowed (so its synthetic-plant tests pass), a precise file:line entry
// matches exactly, a grandfathered package matches by path, and an unknown
// package at a non-precise line is rejected.
func TestAllowed(t *testing.T) {
	// agentslock is NOT in grandfatheredPackages — only its single atomic
	// os.Mkdir is precisely allowed — so a DIFFERENT line in agentslock must be
	// rejected. That proves the precise allowlist is line-scoped, not a blanket
	// package pass.
	cases := []struct {
		name             string
		pkgPath, relPath string
		line             int
		want             bool
	}{
		{"fsguard self is exempt", modulePath + "/tools/fsguard", "tools/fsguard/main.go", 999, true},
		{"precise agentslock atomic mkdir", modulePath + "/internal/agentslock", "internal/agentslock/lockfile.go", 258, true},
		{"precise entry wrong line rejected", modulePath + "/internal/agentslock", "internal/agentslock/lockfile.go", 999, false},
		{"grandfathered package", modulePath + "/internal/projectsync", "internal/projectsync/promote.go", 135, true},
		{"unknown package rejected", modulePath + "/internal/brandnew", "internal/brandnew/x.go", 10, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := allowed(c.pkgPath, c.relPath, c.line); got != c.want {
				t.Errorf("allowed(%q,%q,%d) = %v, want %v", c.pkgPath, c.relPath, c.line, got, c.want)
			}
		})
	}
}

// TestCanonicalPkgPath confirms the test-variant suffix stripping so an
// allowlist keyed by the plain import path matches both load variants.
func TestCanonicalPkgPath(t *testing.T) {
	cases := map[string]string{
		"a/b":                 "a/b",
		"a/b [a/b.test]":      "a/b",
		"a/b.test":            "a/b",
		"a/b_test [a/b.test]": "a/b_test",
	}
	for in, want := range cases {
		if got := canonicalPkgPath(in); got != want {
			t.Errorf("canonicalPkgPath(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestRelPath confirms module-relative reporting and the fallbacks.
func TestRelPath(t *testing.T) {
	cases := map[string]string{
		"/home/x/dot-agents/internal/agentslock/lockfile.go": "internal/agentslock/lockfile.go",
		"/tmp/dot-agents-p2/commands/workflow/fs.go":         "commands/workflow/fs.go",
		"/weird/internal/config/config.go":                   "internal/config/config.go",
		// Neither the /dot-agents/ marker nor any repo-dir root matches, so the
		// cleaned absolute path falls through unchanged (the last-resort branch).
		"/opt/elsewhere/pkg/file.go": "/opt/elsewhere/pkg/file.go",
	}
	for in, want := range cases {
		if got := relPath(in); got != want {
			t.Errorf("relPath(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestReportFindings confirms the failure-log shape: header with count, one
// indented site per finding, and guidance naming fsops + the lesson.
func TestReportFindings(t *testing.T) {
	var buf bytes.Buffer
	reportFindings(&buf, []finding{
		{pkgPath: modulePath + "/internal/sample", relPath: "internal/sample/s.go", line: 12, call: "os.Mkdir"},
	})
	out := buf.String()
	for _, want := range []string{
		"fsguard: 1 raw os.* fs-mutator",
		"internal/sample/s.go:12  os.Mkdir",
		"internal/fsops",
		"leverage-cross-platform-fs-helpers",
		"#148",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("reportFindings output missing %q\nfull:\n%s", want, out)
		}
	}
}

// ── mainRun exit-code seams (mirrors importguard's structure) ────────────────

func cleanScan(_ []string) ([]finding, error) { return nil, nil }
func failScan(_ []string) ([]finding, error)  { return nil, errors.New("synthetic load failure") }
func findScan(_ []string) ([]finding, error) {
	return []finding{{relPath: "internal/x/x.go", line: 5, call: "os.Mkdir"}}, nil
}

func runMainCase(args []string, scan runFunc) (int, string) {
	var buf bytes.Buffer
	code := mainRun(args, &buf, scan)
	return code, buf.String()
}

// assertExitCode fails unless mainRun returned the wanted exit code.
func assertExitCode(t *testing.T, got, want int) {
	t.Helper()
	if got != want {
		t.Errorf("exit=%d, want %d", got, want)
	}
}

// assertStderrContains fails unless substr appears in the captured stderr.
func assertStderrContains(t *testing.T, stderr, substr string) {
	t.Helper()
	if !strings.Contains(stderr, substr) {
		t.Errorf("stderr should contain %q, got %q", substr, stderr)
	}
}

// assertPatterns fails unless the scan was invoked with exactly want.
func assertPatterns(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("patterns = %v, want %v", got, want)
	}
}

// capturePatterns runs mainRun, recording the patterns the scan received.
func capturePatterns(t *testing.T, args []string) []string {
	t.Helper()
	var seen []string
	_, _ = runMainCase(args, func(p []string) ([]finding, error) { seen = p; return nil, nil })
	return seen
}

func TestMainRun(t *testing.T) {
	t.Run("clean exits 0", func(t *testing.T) {
		code, _ := runMainCase(nil, cleanScan)
		assertExitCode(t, code, 0)
	})
	t.Run("default pattern is ./...", func(t *testing.T) {
		assertPatterns(t, capturePatterns(t, nil), []string{"./..."})
	})
	t.Run("explicit patterns override", func(t *testing.T) {
		assertPatterns(t, capturePatterns(t, []string{"./internal/..."}), []string{"./internal/..."})
	})
	t.Run("load error exits 2", func(t *testing.T) {
		code, stderr := runMainCase(nil, failScan)
		assertExitCode(t, code, 2)
		assertStderrContains(t, stderr, "synthetic load failure")
	})
	t.Run("findings exit 1 and render", func(t *testing.T) {
		code, stderr := runMainCase(nil, findScan)
		assertExitCode(t, code, 1)
		assertStderrContains(t, stderr, "internal/x/x.go:5  os.Mkdir")
	})
	t.Run("bad flag exits 2", func(t *testing.T) {
		code, _ := runMainCase([]string{"-nope"}, cleanScan)
		assertExitCode(t, code, 2)
	})
}

// TestRunSurfacesPackageErrors / TestRunPropagatesLoaderError exercise run()
// through the loadPackages var, covering the error branches unreachable from
// the clean-repo scan.
func TestRunSurfacesPackageErrors(t *testing.T) {
	original := loadPackages
	t.Cleanup(func() { loadPackages = original })
	loadPackages = func(_ []string) ([]*packages.Package, error) {
		return []*packages.Package{{
			PkgPath: modulePath + "/internal/synthetic",
			Errors:  []packages.Error{{Msg: "fake load error"}},
		}}, nil
	}
	if _, err := run([]string{"./..."}); err == nil || !strings.Contains(err.Error(), "package load reported errors") {
		t.Errorf("run should surface package errors, got %v", err)
	}
}

func TestRunPropagatesLoaderError(t *testing.T) {
	original := loadPackages
	t.Cleanup(func() { loadPackages = original })
	want := errors.New("loader exploded")
	loadPackages = func(_ []string) ([]*packages.Package, error) { return nil, want }
	if _, err := run([]string{"./..."}); !errors.Is(err, want) {
		t.Errorf("run should propagate loader error, got %v", err)
	}
}

// TestRepoIsClean is the live end-to-end assertion: the guard, run against the
// real repository under HEAD, reports zero findings. This is what stops the
// commit that lands fsguard from also tripping it. Skipped in -short and when
// run outside a module checkout.
func TestRepoIsClean(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping repo-level fs scan in -short mode")
	}
	root, ok := repoRoot()
	if !ok {
		t.Skip("repo root not detectable; skipping")
	}
	t.Chdir(root)
	findings, err := run([]string{"./..."})
	if err != nil {
		t.Fatalf("run(./...): %v", err)
	}
	if len(findings) > 0 {
		var b strings.Builder
		for _, f := range findings {
			b.WriteString("  " + f.relPath + ":" + itoa(f.line) + "  " + f.call + "\n")
		}
		t.Fatalf("expected zero raw-mutator findings at HEAD, got %d:\n%s", len(findings), b.String())
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	return string(d)
}

// repoRoot walks up from this test file's location to the module's go.mod.
func repoRoot() (string, bool) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", false
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}
