package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeGoFile writes a minimal Go source file with an exported symbol.
func writeGoFile(t *testing.T, path, pkgName, symbol string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	content := "package " + pkgName + "\n\nfunc " + symbol + "() {}\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// writeTestFile writes a Go test file that optionally references a symbol.
func writeTestFile(t *testing.T, path, pkgName, referencedSymbol string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	var body string
	if referencedSymbol != "" {
		body = "func TestRef(t *testing.T) { _ = " + referencedSymbol + ".(*struct{})(nil) }\n"
	} else {
		body = "func TestNothing(t *testing.T) {}\n"
	}
	content := "package " + pkgName + "\n\nimport \"testing\"\n\n" + body
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// ── checkFanoutAssertingTestScope table-driven tests ─────────────────────────

func TestCheckFanoutAssertingTestScope_SkipFlag(t *testing.T) {
	dir := t.TempDir()
	// Even with a cross-package asserter, skip=true must pass.
	writeGoFile(t, filepath.Join(dir, "pkg", "foo.go"), "pkg", "ExportedFn")
	writeTestFile(t, filepath.Join(dir, "other", "cross_test.go"), "other", "ExportedFn")
	err := checkFanoutAssertingTestScope(dir, []string{"pkg/"}, true)
	if err != nil {
		t.Errorf("skip=true should bypass gate, got: %v", err)
	}
}

func TestCheckFanoutAssertingTestScope_NoGoScope(t *testing.T) {
	dir := t.TempDir()
	// A non-Go write_scope (docs/) must be bypassed silently.
	err := checkFanoutAssertingTestScope(dir, []string{"docs/README.md"}, false)
	if err != nil {
		t.Errorf("non-Go scope should be bypassed, got: %v", err)
	}
}

func TestCheckFanoutAssertingTestScope_NoSymbols(t *testing.T) {
	dir := t.TempDir()
	// write_scope dir exists but is empty — no symbols → gate passes.
	if err := os.MkdirAll(filepath.Join(dir, "pkg"), 0755); err != nil {
		t.Fatal(err)
	}
	err := checkFanoutAssertingTestScope(dir, []string{"pkg/"}, false)
	if err != nil {
		t.Errorf("empty scope should pass gate, got: %v", err)
	}
}

func TestCheckFanoutAssertingTestScope_NoAssertingTests(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, filepath.Join(dir, "pkg", "foo.go"), "pkg", "MyFunc")
	// A test file that references nothing from scope.
	writeTestFile(t, filepath.Join(dir, "other", "other_test.go"), "other", "")
	err := checkFanoutAssertingTestScope(dir, []string{"pkg/"}, false)
	if err != nil {
		t.Errorf("no asserting tests should pass gate, got: %v", err)
	}
}

func TestCheckFanoutAssertingTestScope_AssertingTestInsideScope(t *testing.T) {
	dir := t.TempDir()
	// The test file is in the same directory as the scope file — inside scope dir.
	writeGoFile(t, filepath.Join(dir, "pkg", "foo.go"), "pkg", "MyFunc")
	writeTestFile(t, filepath.Join(dir, "pkg", "foo_test.go"), "pkg", "MyFunc")
	// writeScope covers the pkg directory; test is inside → OK, no error.
	err := checkFanoutAssertingTestScope(dir, []string{"pkg/"}, false)
	if err != nil {
		t.Errorf("in-scope test should pass gate, got: %v", err)
	}
}

func TestCheckFanoutAssertingTestScope_ExpandSamePackage(t *testing.T) {
	dir := t.TempDir()
	// write_scope specifies the .go file explicitly, not the directory.
	// A sibling *_test.go in the same dir references the symbol.
	// It is outside scope (not listed) but same package → EXPAND (warn, no error).
	writeGoFile(t, filepath.Join(dir, "pkg", "foo.go"), "pkg", "ExportedFn")
	writeTestFile(t, filepath.Join(dir, "pkg", "foo_test.go"), "pkg", "ExportedFn")
	// write_scope is the specific file, so pkg/foo_test.go is outside it.
	err := checkFanoutAssertingTestScope(dir, []string{"pkg/foo.go"}, false)
	if err != nil {
		t.Errorf("same-package test should EXPAND (warn only, no error), got: %v", err)
	}
}

func TestCheckFanoutAssertingTestScope_RefuseCrossPackage(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, filepath.Join(dir, "pkg", "foo.go"), "pkg", "CrossFn")
	// A test file in a completely different package references CrossFn.
	writeTestFile(t, filepath.Join(dir, "other", "cross_test.go"), "other", "CrossFn")
	err := checkFanoutAssertingTestScope(dir, []string{"pkg/"}, false)
	if err == nil {
		t.Error("cross-package asserter should REFUSE (return error)")
	}
	if err != nil {
		if !strings.Contains(err.Error(), "cross_test.go") {
			t.Errorf("error should name offending file, got: %v", err)
		}
		if !strings.Contains(err.Error(), "CrossFn") {
			t.Errorf("error should name offending symbol, got: %v", err)
		}
		if !strings.Contains(err.Error(), atgSkipFlag) {
			t.Errorf("error should mention skip flag, got: %v", err)
		}
	}
}

func TestCheckFanoutAssertingTestScope_RefuseNames_FileAndSymbol(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, filepath.Join(dir, "api", "handler.go"), "api", "HandleRequest")
	writeTestFile(t, filepath.Join(dir, "integration", "handler_test.go"), "integration", "HandleRequest")
	err := checkFanoutAssertingTestScope(dir, []string{"api/"}, false)
	if err == nil {
		t.Fatal("expected REFUSE error for cross-package asserter")
	}
	if !strings.Contains(err.Error(), "handler_test.go") {
		t.Errorf("error must name the test file, got: %v", err)
	}
	if !strings.Contains(err.Error(), "HandleRequest") {
		t.Errorf("error must name the symbol, got: %v", err)
	}
}

func TestCheckFanoutAssertingTestScope_SkippedDirs(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, filepath.Join(dir, "pkg", "foo.go"), "pkg", "VendorFn")
	// A test under vendor/ must be skipped by the walker.
	writeTestFile(t, filepath.Join(dir, "vendor", "ext", "ext_test.go"), "ext", "VendorFn")
	err := checkFanoutAssertingTestScope(dir, []string{"pkg/"}, false)
	if err != nil {
		t.Errorf("test under vendor/ should be skipped, got: %v", err)
	}
}

// ── Unit tests for helpers ────────────────────────────────────────────────────

func TestAtgBuildScopeDirSet_File(t *testing.T) {
	dir := t.TempDir()
	dirs := atgBuildScopeDirSet(dir, []string{"commands/workflow/foo.go"})
	want := filepath.Join(dir, "commands", "workflow")
	if !dirs[want] {
		t.Errorf("expected dir %s in scope set, got %v", want, dirs)
	}
}

func TestAtgBuildScopeDirSet_Directory(t *testing.T) {
	dir := t.TempDir()
	dirs := atgBuildScopeDirSet(dir, []string{"commands/workflow/"})
	want := filepath.Join(dir, "commands", "workflow")
	if !dirs[want] {
		t.Errorf("expected dir %s in scope set, got %v", want, dirs)
	}
}

func TestAtgEnumerateScopeSymbols_ExportedOnly(t *testing.T) {
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "mypkg")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	content := "package mypkg\n\nfunc Exported() {}\nfunc unexported() {}\n"
	if err := os.WriteFile(filepath.Join(pkgDir, "x.go"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	symbols, err := atgEnumerateScopeSymbols(dir, []string{"mypkg/"})
	if err != nil {
		t.Fatal(err)
	}
	if len(symbols) != 1 || symbols[0].Name != "Exported" {
		t.Errorf("expected exactly [Exported], got %+v", symbols)
	}
}

func TestAtgEnumerateScopeSymbols_TypeAndConst(t *testing.T) {
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "mypkg")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	// GenDecl covers type, var, and const.
	content := "package mypkg\n\ntype MyType struct{}\n\nconst MyConst = 1\n\nvar MyVar = 2\n"
	if err := os.WriteFile(filepath.Join(pkgDir, "types.go"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	symbols, err := atgEnumerateScopeSymbols(dir, []string{"mypkg/"})
	if err != nil {
		t.Fatal(err)
	}
	names := make(map[string]bool)
	for _, s := range symbols {
		names[s.Name] = true
	}
	for _, want := range []string{"MyType", "MyConst", "MyVar"} {
		if !names[want] {
			t.Errorf("expected symbol %q in results, got %+v", want, symbols)
		}
	}
}

func TestAtgEnumerateScopeSymbols_SkipsTestFiles(t *testing.T) {
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "mypkg")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	// _test.go file exports a symbol — must be ignored by symbol enumeration.
	content := "package mypkg\n\nfunc TestOnlyExported() {}\n"
	if err := os.WriteFile(filepath.Join(pkgDir, "x_test.go"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	symbols, err := atgEnumerateScopeSymbols(dir, []string{"mypkg/"})
	if err != nil {
		t.Fatal(err)
	}
	if len(symbols) != 0 {
		t.Errorf("expected no symbols from _test.go, got %+v", symbols)
	}
}

func TestAtgViolationKind_Expand(t *testing.T) {
	symbols := []atgScopeSymbol{{Name: "Foo", DeclDir: "/proj/pkg"}}
	kind := atgViolationKind("/proj/pkg", "Foo", symbols)
	if kind != atgViolationExpand {
		t.Errorf("same dir should be EXPAND, got %q", kind)
	}
}

func TestAtgViolationKind_Refuse(t *testing.T) {
	symbols := []atgScopeSymbol{{Name: "Foo", DeclDir: "/proj/pkg"}}
	kind := atgViolationKind("/proj/other", "Foo", symbols)
	if kind != atgViolationRefuse {
		t.Errorf("different dir should be REFUSE, got %q", kind)
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

func TestAtgCollectFromFile_MalformedGoFile(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.go")
	if err := os.WriteFile(bad, []byte("this is not valid go {{{"), 0644); err != nil {
		t.Fatal(err)
	}
	var out []atgScopeSymbol
	atgCollectFromFile(bad, &out) // must not panic; parse error is silently ignored
	if len(out) != 0 {
		t.Errorf("malformed file should produce no symbols, got %+v", out)
	}
}

func TestAtgMatchedSymbol_UnreadableFile(t *testing.T) {
	re, _ := atgBuildSymbolRegex([]atgScopeSymbol{{Name: "Foo", DeclDir: "/x"}})
	result := atgMatchedSymbol("/nonexistent/path/x_test.go", re)
	if result != "" {
		t.Errorf("unreadable file should return empty string, got %q", result)
	}
}

func TestAtgBuildSymbolRegex_Deduplicates(t *testing.T) {
	symbols := []atgScopeSymbol{
		{Name: "Foo", DeclDir: "/a"},
		{Name: "Foo", DeclDir: "/b"},
		{Name: "Bar", DeclDir: "/a"},
	}
	re, err := atgBuildSymbolRegex(symbols)
	if err != nil {
		t.Fatal(err)
	}
	if !re.MatchString("Foo") {
		t.Error("regex should match Foo")
	}
	if !re.MatchString("Bar") {
		t.Error("regex should match Bar")
	}
	// Must not match partial names (word boundary).
	if re.MatchString("FooBar") {
		t.Error("regex must not match FooBar as Foo due to word boundary")
	}
}

func writeTestFilesInSubDirs(t *testing.T, root string, subdirs []string) {
	t.Helper()
	for _, sub := range subdirs {
		if err := os.MkdirAll(filepath.Join(root, sub), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, sub, "x_test.go"), []byte("package x\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func assertNoPathContains(t *testing.T, paths []string, segment string) {
	t.Helper()
	sep := string(filepath.Separator)
	for _, p := range paths {
		if strings.Contains(p, sep+segment+sep) {
			t.Errorf("walk should skip %s but found %s", segment, p)
		}
	}
}

func assertPathVisited(t *testing.T, paths []string, want string) {
	t.Helper()
	for _, p := range paths {
		if p == want {
			return
		}
	}
	t.Errorf("expected %s to be visited; found: %v", want, paths)
}

func TestAtgWalkTestFiles_SkipsVendorAndGit(t *testing.T) {
	dir := t.TempDir()
	writeTestFilesInSubDirs(t, dir, []string{"vendor/x", ".git/hooks", "testdata/fixtures", ".claude/worktrees", "src"})
	var found []string
	if err := atgWalkTestFiles(dir, func(p string) error {
		found = append(found, p)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, skipped := range []string{"vendor", ".git", "testdata", ".claude"} {
		assertNoPathContains(t, found, skipped)
	}
	assertPathVisited(t, found, filepath.Join(dir, "src", "x_test.go"))
}

func TestAtgClassifyAndReport_AllRefuse(t *testing.T) {
	violations := []atgViolation{
		{TestFile: "/proj/other/x_test.go", Symbol: "Foo", Kind: atgViolationRefuse},
	}
	err := atgClassifyAndReport(violations)
	if err == nil {
		t.Fatal("expected REFUSE error")
	}
	if !strings.Contains(err.Error(), "x_test.go") {
		t.Errorf("error should name file, got: %v", err)
	}
}

func TestAtgClassifyAndReport_AllExpand(t *testing.T) {
	violations := []atgViolation{
		{TestFile: "/proj/pkg/x_test.go", Symbol: "Bar", Kind: atgViolationExpand},
	}
	err := atgClassifyAndReport(violations)
	if err != nil {
		t.Errorf("EXPAND-only should not return error, got: %v", err)
	}
}

func TestAtgClassifyAndReport_MixedExpandAndRefuse(t *testing.T) {
	violations := []atgViolation{
		{TestFile: "/proj/pkg/x_test.go", Symbol: "Bar", Kind: atgViolationExpand},
		{TestFile: "/proj/other/y_test.go", Symbol: "Bar", Kind: atgViolationRefuse},
	}
	err := atgClassifyAndReport(violations)
	if err == nil {
		t.Fatal("expected REFUSE error when any violation is refuse")
	}
}
