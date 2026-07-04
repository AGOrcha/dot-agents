package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSourceFile writes a Go source file with the given package clause and body.
func writeSourceFile(t *testing.T, path, pkgName, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	content := "package " + pkgName + "\n\n" + body
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// writeGoFile writes a minimal Go source file exporting a single function.
func writeGoFile(t *testing.T, path, pkgName, symbol string) {
	t.Helper()
	writeSourceFile(t, path, pkgName, "func "+symbol+"() {}\n")
}

// writeRefTestFile writes a Go test file whose body references referencedExpr as
// real code (an AST identifier), or references nothing when referencedExpr is "".
func writeRefTestFile(t *testing.T, path, pkgName, referencedExpr string) {
	t.Helper()
	var body string
	if referencedExpr != "" {
		body = "import \"testing\"\n\nvar _ = " + referencedExpr + "\n\nfunc TestRef(t *testing.T) {}\n"
	} else {
		body = "import \"testing\"\n\nfunc TestNothing(t *testing.T) {}\n"
	}
	writeSourceFile(t, path, pkgName, body)
}

// ── Blocker 1: file-scoped write_scope EXPAND must warn ───────────────────────

func TestATG_FileScope_SiblingTestExpands(t *testing.T) {
	dir := t.TempDir()
	// write_scope is a specific FILE. Sibling foo_test.go in the same dir
	// references the symbol but is NOT itself in scope → EXPAND (warn, no error).
	writeGoFile(t, filepath.Join(dir, "pkg", "foo.go"), "pkg", "ExportedFn")
	writeRefTestFile(t, filepath.Join(dir, "pkg", "foo_test.go"), "pkg", "ExportedFn")

	warn := captureStderr(t, func() {
		if err := checkFanoutAssertingTestScope(dir, []string{"pkg/foo.go"}, false); err != nil {
			t.Errorf("same-package sibling should EXPAND (no error), got: %v", err)
		}
	})
	if !strings.Contains(warn, "not listed in write_scope") {
		t.Errorf("expected EXPAND warning content, got: %q", warn)
	}
	if !strings.Contains(warn, "foo_test.go") {
		t.Errorf("EXPAND warning should name the sibling test file, got: %q", warn)
	}
	if !strings.Contains(warn, "ExportedFn") {
		t.Errorf("EXPAND warning should name the symbol, got: %q", warn)
	}
}

func TestATG_FileScope_ExplicitTestInScopeNoWarn(t *testing.T) {
	dir := t.TempDir()
	// Both the source file AND its test are explicitly in write_scope → in scope,
	// no warning, no error.
	writeGoFile(t, filepath.Join(dir, "pkg", "foo.go"), "pkg", "ExportedFn")
	writeRefTestFile(t, filepath.Join(dir, "pkg", "foo_test.go"), "pkg", "ExportedFn")

	warn := captureStderr(t, func() {
		if err := checkFanoutAssertingTestScope(dir, []string{"pkg/foo.go", "pkg/foo_test.go"}, false); err != nil {
			t.Errorf("explicitly-scoped test should be silent, got: %v", err)
		}
	})
	if strings.Contains(warn, "not listed in write_scope") {
		t.Errorf("explicitly-scoped test must NOT warn, got: %q", warn)
	}
}

func TestATG_DirScope_TestInsideNoWarn(t *testing.T) {
	dir := t.TempDir()
	// Directory scope covers the whole package including its tests → no warning.
	writeGoFile(t, filepath.Join(dir, "pkg", "foo.go"), "pkg", "ExportedFn")
	writeRefTestFile(t, filepath.Join(dir, "pkg", "foo_test.go"), "pkg", "ExportedFn")

	warn := captureStderr(t, func() {
		if err := checkFanoutAssertingTestScope(dir, []string{"pkg/"}, false); err != nil {
			t.Errorf("dir-scoped in-package test should pass, got: %v", err)
		}
	})
	if strings.Contains(warn, "not listed in write_scope") {
		t.Errorf("dir-scoped test must NOT warn, got: %q", warn)
	}
}

// ── Blocker 2: directory scope must recurse into subpackages ──────────────────

func TestATG_DirScope_RecursesIntoSubpackage(t *testing.T) {
	dir := t.TempDir()
	// Symbol declared under a SUBDIR of the dir scope. A cross-package test must
	// still see it → REFUSE, proving recursive enumeration.
	writeGoFile(t, filepath.Join(dir, "commands", "workflow", "deep.go"), "workflow", "DeepSymbol")
	writeRefTestFile(t, filepath.Join(dir, "other", "cross_test.go"), "other", "DeepSymbol")

	err := checkFanoutAssertingTestScope(dir, []string{"commands/"}, false)
	if err == nil {
		t.Fatal("recursive dir scope should enumerate subpackage symbol and REFUSE cross-package asserter")
	}
	if !strings.Contains(err.Error(), "DeepSymbol") {
		t.Errorf("error should name the deep symbol, got: %v", err)
	}
}

func TestAtgEnumerateScopeSymbols_Recursive(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, filepath.Join(dir, "top", "a.go"), "top", "TopSym")
	writeGoFile(t, filepath.Join(dir, "top", "sub", "b.go"), "sub", "SubSym")
	names := symbolNameSet(atgEnumerateScopeSymbols(dir, []string{"top/"}))
	if !names["TopSym"] || !names["SubSym"] {
		t.Errorf("recursive enumeration should find TopSym and SubSym, got %+v", names)
	}
}

// ── Blocker 3: AST matching must ignore comments and string literals ──────────

func TestATG_SymbolInCommentDoesNotMatch(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, filepath.Join(dir, "pkg", "foo.go"), "pkg", "SecretFn")
	// The symbol name appears ONLY in a comment in a cross-package test file.
	body := "import \"testing\"\n\n// SecretFn is mentioned here but never called\nfunc TestC(t *testing.T) {}\n"
	writeSourceFile(t, filepath.Join(dir, "other", "comment_test.go"), "other", body)

	if err := checkFanoutAssertingTestScope(dir, []string{"pkg/"}, false); err != nil {
		t.Errorf("symbol only in a comment must NOT match, got: %v", err)
	}
}

func TestATG_SymbolInStringLiteralDoesNotMatch(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, filepath.Join(dir, "pkg", "foo.go"), "pkg", "SecretFn")
	// The symbol name appears ONLY inside a string literal.
	body := "import \"testing\"\n\nvar msg = \"call SecretFn now\"\n\nfunc TestS(t *testing.T) { _ = msg }\n"
	writeSourceFile(t, filepath.Join(dir, "other", "string_test.go"), "other", body)

	if err := checkFanoutAssertingTestScope(dir, []string{"pkg/"}, false); err != nil {
		t.Errorf("symbol only in a string literal must NOT match, got: %v", err)
	}
}

func TestATG_MethodReferenceMatches(t *testing.T) {
	dir := t.TempDir()
	// Scope declares an exported symbol; the cross-package test references it via
	// a qualified selector (pkg.CrossHelper) — SelectorExpr.Sel must be seen.
	writeGoFile(t, filepath.Join(dir, "pkg", "foo.go"), "pkg", "CrossHelper")
	body := "import (\n\t\"testing\"\n\n\t\"example.com/pkg\"\n)\n\nvar _ = pkg.CrossHelper\n\nfunc TestM(t *testing.T) {}\n"
	writeSourceFile(t, filepath.Join(dir, "other", "sel_test.go"), "other", body)

	err := checkFanoutAssertingTestScope(dir, []string{"pkg/"}, false)
	if err == nil {
		t.Fatal("qualified selector reference should REFUSE cross-package")
	}
	if !strings.Contains(err.Error(), "CrossHelper") {
		t.Errorf("error should name CrossHelper, got: %v", err)
	}
}

// ── Blocker 4: REFUSE wins over EXPAND within one file ────────────────────────

func TestATG_MixedSameAndCrossInOneFile_Refuses(t *testing.T) {
	dir := t.TempDir()
	// SameSym declared in the same dir as the test; CrossSym declared elsewhere.
	// One test file references BOTH. The same-dir match must NOT mask the
	// cross-package one → REFUSE, naming CrossSym.
	writeGoFile(t, filepath.Join(dir, "pkg", "same.go"), "pkg", "SameSym")
	writeGoFile(t, filepath.Join(dir, "elsewhere", "cross.go"), "elsewhere", "CrossSym")
	body := "import \"testing\"\n\nvar _ = SameSym\nvar _ = CrossSym\n\nfunc TestBoth(t *testing.T) {}\n"
	writeSourceFile(t, filepath.Join(dir, "pkg", "both_test.go"), "pkg", body)

	// Scope lists the two source FILES only (not the pkg dir), so pkg/both_test.go
	// is out of scope and references both SameSym (same dir) and CrossSym (cross).
	err := checkFanoutAssertingTestScope(dir, []string{"pkg/same.go", "elsewhere/cross.go"}, false)
	if err == nil {
		t.Fatal("mixed same+cross in one file must REFUSE")
	}
	if !strings.Contains(err.Error(), "CrossSym") {
		t.Errorf("REFUSE error must name the cross-package symbol CrossSym, got: %v", err)
	}
	if !strings.Contains(err.Error(), "both_test.go") {
		t.Errorf("REFUSE error must name the offending file, got: %v", err)
	}
}

// ── Core contract cases ───────────────────────────────────────────────────────

func TestATG_SkipFlagBypasses(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, filepath.Join(dir, "pkg", "foo.go"), "pkg", "ExportedFn")
	writeRefTestFile(t, filepath.Join(dir, "other", "cross_test.go"), "other", "ExportedFn")
	if err := checkFanoutAssertingTestScope(dir, []string{"pkg/"}, true); err != nil {
		t.Errorf("skip=true should bypass gate, got: %v", err)
	}
}

func TestATG_NonGoScopeBypasses(t *testing.T) {
	dir := t.TempDir()
	if err := checkFanoutAssertingTestScope(dir, []string{"docs/README.md"}, false); err != nil {
		t.Errorf("non-Go scope should be bypassed, got: %v", err)
	}
}

func TestATG_NoSymbolsBypasses(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "pkg"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := checkFanoutAssertingTestScope(dir, []string{"pkg/"}, false); err != nil {
		t.Errorf("empty scope should pass gate, got: %v", err)
	}
}

func TestATG_NoAssertingTestsPasses(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, filepath.Join(dir, "pkg", "foo.go"), "pkg", "MyFunc")
	writeRefTestFile(t, filepath.Join(dir, "other", "other_test.go"), "other", "")
	if err := checkFanoutAssertingTestScope(dir, []string{"pkg/"}, false); err != nil {
		t.Errorf("no asserting tests should pass gate, got: %v", err)
	}
}

func TestATG_CrossPackageRefuseNamesFileAndSymbolAndFlag(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, filepath.Join(dir, "api", "handler.go"), "api", "HandleRequest")
	writeRefTestFile(t, filepath.Join(dir, "integration", "handler_test.go"), "integration", "HandleRequest")
	err := checkFanoutAssertingTestScope(dir, []string{"api/"}, false)
	if err == nil {
		t.Fatal("cross-package asserter should REFUSE")
	}
	for _, want := range []string{"handler_test.go", "HandleRequest", atgSkipFlag} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("REFUSE error missing %q, got: %v", want, err)
		}
	}
}

func TestATG_SkippedDirsIgnored(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, filepath.Join(dir, "pkg", "foo.go"), "pkg", "VendorFn")
	// A test under vendor/ must be skipped by the walker (no REFUSE).
	writeRefTestFile(t, filepath.Join(dir, "vendor", "ext", "ext_test.go"), "ext", "VendorFn")
	if err := checkFanoutAssertingTestScope(dir, []string{"pkg/"}, false); err != nil {
		t.Errorf("test under vendor/ should be skipped, got: %v", err)
	}
}

// ── Helper unit tests ─────────────────────────────────────────────────────────

func symbolNameSet(symbols []atgScopeSymbol) map[string]bool {
	names := make(map[string]bool)
	for _, s := range symbols {
		names[s.Name] = true
	}
	return names
}

func TestAtgParseScope_FileVsDir(t *testing.T) {
	dir := t.TempDir()
	s := atgParseScope(dir, []string{"pkg/foo.go", "commands/"})
	if !s.files[filepath.Join(dir, "pkg", "foo.go")] {
		t.Errorf("file entry should be in files set, got %+v", s.files)
	}
	if len(s.dirs) != 1 || s.dirs[0] != filepath.Join(dir, "commands") {
		t.Errorf("dir entry should be in dirs, got %+v", s.dirs)
	}
}

func TestAtgScopeContains_ExplicitFileAndDirPrefix(t *testing.T) {
	dir := t.TempDir()
	s := atgParseScope(dir, []string{"pkg/foo.go", "commands/"})
	if !s.contains(filepath.Join(dir, "pkg", "foo.go")) {
		t.Error("explicit file should be contained")
	}
	if !s.contains(filepath.Join(dir, "commands", "workflow", "x_test.go")) {
		t.Error("file under dir scope should be contained")
	}
	if s.contains(filepath.Join(dir, "pkg", "foo_test.go")) {
		t.Error("sibling test of a file-scope entry must NOT be contained (EXPAND candidate)")
	}
	if s.contains(filepath.Join(dir, "other", "y_test.go")) {
		t.Error("unrelated file must not be contained")
	}
}

func TestAtgEnumerateScopeSymbols_ExportedOnly(t *testing.T) {
	dir := t.TempDir()
	writeSourceFile(t, filepath.Join(dir, "mypkg", "x.go"), "mypkg", "func Exported() {}\nfunc unexported() {}\n")
	symbols := atgEnumerateScopeSymbols(dir, []string{"mypkg/"})
	if len(symbols) != 1 || symbols[0].Name != "Exported" {
		t.Errorf("expected exactly [Exported], got %+v", symbols)
	}
}

func TestAtgEnumerateScopeSymbols_SkipsTestFiles(t *testing.T) {
	dir := t.TempDir()
	writeSourceFile(t, filepath.Join(dir, "mypkg", "x_test.go"), "mypkg", "func TestOnlyExported() {}\n")
	symbols := atgEnumerateScopeSymbols(dir, []string{"mypkg/"})
	if len(symbols) != 0 {
		t.Errorf("expected no symbols from _test.go, got %+v", symbols)
	}
}

func TestAtgEnumerateScopeSymbols_TypeConstVar(t *testing.T) {
	dir := t.TempDir()
	body := "type MyType struct{}\n\nconst MyConst = 1\n\nvar MyVar = 2\n"
	writeSourceFile(t, filepath.Join(dir, "mypkg", "types.go"), "mypkg", body)
	names := symbolNameSet(atgEnumerateScopeSymbols(dir, []string{"mypkg/"}))
	for _, want := range []string{"MyType", "MyConst", "MyVar"} {
		if !names[want] {
			t.Errorf("expected symbol %q, got %+v", want, names)
		}
	}
}

func TestAtgEnumerateScopeSymbols_SingleFileEntry(t *testing.T) {
	dir := t.TempDir()
	// Two files in the same dir; scope lists only one → only its symbol enumerated.
	writeGoFile(t, filepath.Join(dir, "pkg", "a.go"), "pkg", "FromA")
	writeGoFile(t, filepath.Join(dir, "pkg", "b.go"), "pkg", "FromB")
	names := symbolNameSet(atgEnumerateScopeSymbols(dir, []string{"pkg/a.go"}))
	if !names["FromA"] || names["FromB"] {
		t.Errorf("file scope should enumerate only FromA, got %+v", names)
	}
}

func TestAtgEnumerateScopeSymbols_SkipsExcludedSubdirs(t *testing.T) {
	dir := t.TempDir()
	// A vendored package under a dir scope must NOT contribute symbols.
	writeGoFile(t, filepath.Join(dir, "top", "keep.go"), "top", "KeepSym")
	writeGoFile(t, filepath.Join(dir, "top", "vendor", "dep", "skip.go"), "dep", "SkipSym")
	names := symbolNameSet(atgEnumerateScopeSymbols(dir, []string{"top/"}))
	if !names["KeepSym"] {
		t.Error("KeepSym should be enumerated")
	}
	if names["SkipSym"] {
		t.Error("SkipSym under vendor/ must be skipped during enumeration")
	}
}

func TestAtgEnumerateScopeSymbols_MalformedFileSkipped(t *testing.T) {
	dir := t.TempDir()
	// A malformed non-test .go inside a dir scope must be skipped without panic
	// while a valid sibling still contributes its symbol.
	writeGoFile(t, filepath.Join(dir, "pkg", "good.go"), "pkg", "GoodSym")
	if err := os.WriteFile(filepath.Join(dir, "pkg", "bad.go"), []byte("package pkg\nfunc ((("), 0644); err != nil {
		t.Fatal(err)
	}
	names := symbolNameSet(atgEnumerateScopeSymbols(dir, []string{"pkg/"}))
	if !names["GoodSym"] {
		t.Errorf("valid sibling symbol should still be enumerated, got %+v", names)
	}
}

func TestAtgReferencedIdents_ExcludesCommentsAndStrings(t *testing.T) {
	dir := t.TempDir()
	body := "import \"testing\"\n\n// CommentSym here\nvar s = \"StringSym\"\n\nfunc TestX(t *testing.T) { _ = RealSym }\n\nvar RealSym = 1\n"
	path := filepath.Join(dir, "x_test.go")
	writeSourceFile(t, path, "x", body)
	idents := atgReferencedIdents(path)
	if !idents["RealSym"] {
		t.Error("RealSym (real code) should be referenced")
	}
	if idents["CommentSym"] {
		t.Error("CommentSym (comment) must not be referenced")
	}
	if idents["StringSym"] {
		t.Error("StringSym (string literal) must not be referenced")
	}
}

func TestAtgReferencedIdents_MalformedReturnsNil(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad_test.go")
	if err := os.WriteFile(path, []byte("this is not valid go {{{"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := atgReferencedIdents(path); got != nil {
		t.Errorf("malformed file should yield nil idents, got %v", got)
	}
}

func TestAtgClassifyTestFile_StrictestRefuse(t *testing.T) {
	declDirs := map[string]map[string]bool{
		"Same":  {"/proj/pkg": true},
		"Cross": {"/proj/other": true},
	}
	v, ok := atgClassifyTestFile("/proj/pkg/x_test.go", []string{"Cross", "Same"}, declDirs)
	if !ok {
		t.Fatal("expected a violation")
	}
	if v.Kind != atgViolationRefuse {
		t.Errorf("mixed should be REFUSE, got %q", v.Kind)
	}
	if len(v.Symbols) != 1 || v.Symbols[0] != "Cross" {
		t.Errorf("REFUSE should list only cross-package symbol, got %+v", v.Symbols)
	}
}

func TestAtgClassifyTestFile_AllSameExpand(t *testing.T) {
	declDirs := map[string]map[string]bool{"Same": {"/proj/pkg": true}}
	v, ok := atgClassifyTestFile("/proj/pkg/x_test.go", []string{"Same"}, declDirs)
	if !ok || v.Kind != atgViolationExpand {
		t.Errorf("all-same should EXPAND, got ok=%v kind=%q", ok, v.Kind)
	}
}

func TestAtgClassifyTestFile_NoMatchNoViolation(t *testing.T) {
	declDirs := map[string]map[string]bool{"Same": {"/proj/pkg": true}}
	if _, ok := atgClassifyTestFile("/proj/pkg/x_test.go", nil, declDirs); ok {
		t.Error("no matched names should yield no violation")
	}
}

func TestAtgClassifyAndReport_MixedFilesRefuseWins(t *testing.T) {
	warn := captureStderr(t, func() {
		err := atgClassifyAndReport([]atgViolation{
			{TestFile: "/proj/pkg/e_test.go", Symbols: []string{"Bar"}, Kind: atgViolationExpand},
			{TestFile: "/proj/other/r_test.go", Symbols: []string{"Foo"}, Kind: atgViolationRefuse},
		})
		if err == nil {
			t.Fatal("expected REFUSE error when any violation is refuse")
		}
		if !strings.Contains(err.Error(), "r_test.go") {
			t.Errorf("REFUSE error should name the file, got: %v", err)
		}
	})
	if !strings.Contains(warn, "e_test.go") {
		t.Errorf("EXPAND warning should still fire for same-package file, got: %q", warn)
	}
}

func TestIsNonTestGoFile(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"foo.go", true},
		{"foo_test.go", false},
		{"foo.md", false},
		{"foo.go.bak", false},
	}
	for _, tc := range cases {
		if got := isNonTestGoFile(tc.name); got != tc.want {
			t.Errorf("isNonTestGoFile(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestAtgWalkTestFiles_SkipsExcludedDirs(t *testing.T) {
	dir := t.TempDir()
	for _, sub := range []string{"vendor/x", ".git/hooks", "testdata/f", ".claude/w", "worktrees/z", "src"} {
		writeSourceFile(t, filepath.Join(dir, sub, "x_test.go"), "x", "")
	}
	var found []string
	if err := atgWalkTestFiles(dir, func(p string) error {
		found = append(found, p)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sep := string(filepath.Separator)
	for _, p := range found {
		for _, skipped := range []string{"vendor", ".git", "testdata", ".claude", "worktrees"} {
			if strings.Contains(p, sep+skipped+sep) {
				t.Errorf("walk should skip %s but found %s", skipped, p)
			}
		}
	}
	wantSrc := filepath.Join(dir, "src", "x_test.go")
	visited := false
	for _, p := range found {
		if p == wantSrc {
			visited = true
		}
	}
	if !visited {
		t.Errorf("expected src/x_test.go visited; found %v", found)
	}
}
