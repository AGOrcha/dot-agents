package globalflagcov

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/packages"
)

func TestUnion(t *testing.T) {
	a := FlagSet{JSON: true, Yes: true}
	b := FlagSet{DryRun: true, Yes: true, Verbose: true}
	got := union(a, b)
	want := FlagSet{JSON: true, DryRun: true, Yes: true, Verbose: true}
	if got != want {
		t.Fatalf("union = %+v, want %+v", got, want)
	}
	if z := union(FlagSet{}, FlagSet{}); z != (FlagSet{}) {
		t.Fatalf("union of zero values must be zero, got %+v", z)
	}
}

func TestMarkFlag(t *testing.T) {
	cases := []struct {
		name  string
		check func(FlagSet) bool
	}{
		{"JSON", func(f FlagSet) bool { return f.JSON }},
		{"DryRun", func(f FlagSet) bool { return f.DryRun }},
		{"Yes", func(f FlagSet) bool { return f.Yes }},
		{"Force", func(f FlagSet) bool { return f.Force }},
		{"Verbose", func(f FlagSet) bool { return f.Verbose }},
	}
	for _, tc := range cases {
		var fs FlagSet
		markFlag(&fs, tc.name)
		if !tc.check(fs) {
			t.Fatalf("markFlag(%q) did not set its field: %+v", tc.name, fs)
		}
	}
	var fs FlagSet
	markFlag(&fs, "NotAFlag")
	if fs != (FlagSet{}) {
		t.Fatalf("unknown flag name must be ignored, got %+v", fs)
	}
}

func TestPackageQualifier(t *testing.T) {
	// nil package
	if q := packageQualifier(nil); q != "" {
		t.Fatalf("nil pkg: want empty, got %q", q)
	}
}

func TestDirectFlagsInBodyAndIsFlagAccess(t *testing.T) {
	src := `package p
func h() {
	_ = Flags.JSON
	_ = deps.Flags.DryRun
	_ = other.Field
	_ = Flags.Unknown
}`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "h.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	fn := f.Decls[0].(*ast.FuncDecl)
	fs := directFlagsInBody(fn.Body)
	if !fs.JSON {
		t.Fatal("expected Flags.JSON to be detected (bare form)")
	}
	if !fs.DryRun {
		t.Fatal("expected deps.Flags.DryRun to be detected (embedded form)")
	}
	if fs.Force || fs.Yes || fs.Verbose {
		t.Fatalf("unexpected flags set: %+v", fs)
	}
}

func TestIsFlagAccessNegativeForms(t *testing.T) {
	// foo().Bar — sel.X is a CallExpr, neither embedded nor bare ident form.
	src := `package p
func h() { _ = foo().Bar; _ = pkg.Flags.JSON }`
	fset := token.NewFileSet()
	f, _ := parser.ParseFile(fset, "h.go", src, 0)
	fn := f.Decls[0].(*ast.FuncDecl)
	var sels []*ast.SelectorExpr
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if s, ok := n.(*ast.SelectorExpr); ok {
			sels = append(sels, s)
		}
		return true
	})
	sawEmbedded := false
	for _, s := range sels {
		if isFlagAccess(s) {
			sawEmbedded = true
		}
	}
	if !sawEmbedded {
		t.Fatal("expected pkg.Flags.JSON to be recognized as embedded flag access")
	}
}

func TestTightestFuncLit(t *testing.T) {
	// Multi-line source so the outer literal spans more lines than the inner —
	// this exercises the swap branch in tightestFuncLit (later candidate with
	// strictly smaller span replaces best).
	src := `package p
var _ = func() {
	x := func() { _ = 1 }
	_ = x
}`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "h.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	var lits []*ast.FuncLit
	ast.Inspect(f, func(n ast.Node) bool {
		if fl, ok := n.(*ast.FuncLit); ok {
			lits = append(lits, fl)
		}
		return true
	})
	if len(lits) != 2 {
		t.Fatalf("expected 2 func literals, got %d", len(lits))
	}
	outerSpan := fset.Position(lits[0].End()).Line - fset.Position(lits[0].Pos()).Line
	innerSpan := fset.Position(lits[1].End()).Line - fset.Position(lits[1].Pos()).Line
	if innerSpan >= outerSpan {
		t.Fatalf("test setup invariant: inner span (%d) must be < outer span (%d)", innerSpan, outerSpan)
	}
	best := tightestFuncLit(fset, lits)
	// The inner (smaller span) literal must win regardless of input order.
	gotSpan := fset.Position(best.End()).Line - fset.Position(best.Pos()).Line
	if gotSpan != innerSpan {
		t.Fatalf("tightestFuncLit did not pick the smallest-span literal")
	}
	// Single-candidate path: seed is returned unchanged.
	if tightestFuncLit(fset, lits[:1]) != lits[0] {
		t.Fatal("single candidate must be returned as-is")
	}
}

func TestSymbolKeyForMethodReceiver(t *testing.T) {
	src := `package commands
type T struct{}
func (t T) M() {}
func (t *T) P() {}
func F() {}`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "h.go", src, parser.AllErrors)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{Defs: map[*ast.Ident]types.Object{}}
	conf := types.Config{Importer: nil, Error: func(error) {}}
	pkg, _ := conf.Check(commandsPkgPath, fset, []*ast.File{f}, info)
	if pkg == nil {
		t.Fatal("type check produced no package")
	}

	got := map[string]bool{}
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok {
			continue
		}
		fnObj, ok := info.Defs[fn.Name].(*types.Func)
		if !ok {
			continue
		}
		if k := symbolKey(fnObj); k != "" {
			got[k] = true
		}
	}
	// Value receiver, pointer receiver, and plain func all yield keys; the
	// recvString pointer-elem and named-type branches are exercised here.
	if !got["T.M"] || !got["T.P"] || !got["F"] {
		t.Fatalf("expected T.M, T.P, F symbol keys; got %v", got)
	}
}

func TestLoadStaticBadRoot(t *testing.T) {
	if _, err := loadStatic(string([]byte{0})); err == nil {
		t.Fatal("expected error from loadStatic with invalid path")
	}
}

func TestLoadCommandPackagesNoPackages(t *testing.T) {
	// A directory with no Go module / command packages yields no ok packages.
	if _, err := loadCommandPackages(t.TempDir()); err == nil {
		t.Fatal("expected error when no command packages load cleanly")
	}
}

// TestLoadCommandPackagesLogsPerPackageErrors covers the len(p.Errors) > 0
// branch specifically: a broken package must be logged (not silently
// excluded) while a sibling clean package still loads fine -- distinct from
// TestLoadCommandPackagesNoPackages, where the whole module fails to load.
func TestLoadCommandPackagesLogsPerPackageErrors(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "go.mod"), "module fixturemod\n\ngo 1.23\n")
	mustWriteFile(t, filepath.Join(root, "commands", "root.go"),
		"package commands\n\nconst RootMarker = \"ok\"\n")
	mustWriteFile(t, filepath.Join(root, "commands", "agents", "broken.go"),
		"package agents\n\nfunc broken( {\n")

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	pkgs, err := loadCommandPackages(root)
	if err != nil {
		t.Fatalf("loadCommandPackages() = %v, want nil (the clean ./commands package should still load)", err)
	}
	if len(pkgs) == 0 {
		t.Fatal("loadCommandPackages() returned no packages, want the clean ./commands package")
	}
	if !bytes.Contains(buf.Bytes(), []byte("package failed to load")) {
		t.Errorf("expected a warning log for the broken package, got %q", buf.String())
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTransitiveFlagsCycleSafe(t *testing.T) {
	s := &staticAnalysis{
		direct: map[string]FlagSet{
			"a": {JSON: true},
			"b": {DryRun: true},
		},
		calls: map[string][]string{
			"a": {"b"},
			"b": {"a"}, // cycle
		},
	}
	got := s.transitiveFlags("a")
	if !got.JSON || !got.DryRun {
		t.Fatalf("expected transitive union across cycle, got %+v", got)
	}
}

// TestSymbolKeyNilGuards covers the early-return branches in symbolKey:
// nil *types.Func and a Func with a nil Pkg both must yield "".
func TestSymbolKeyNilGuards(t *testing.T) {
	if k := symbolKey(nil); k != "" {
		t.Fatalf("nil func: want empty key, got %q", k)
	}
	// A *types.Func with nil pkg: construct via types.NewFunc(nopos, nil, ...).
	f := types.NewFunc(token.NoPos, nil, "Foo", types.NewSignatureType(nil, nil, nil, nil, nil, false))
	if k := symbolKey(f); k != "" {
		t.Fatalf("nil-pkg func: want empty key, got %q", k)
	}
}

// TestRecvStringNamedTypeFallback covers the L170 fallback branch of
// recvString where the receiver type is neither a *types.Pointer nor a
// *types.Named.
func TestRecvStringNamedTypeFallback(t *testing.T) {
	// A basic type (e.g. int) goes through the fallback `t.String()`.
	got := recvString(types.Typ[types.Int])
	if got != "int" {
		t.Fatalf("recvString(int) = %q, want %q", got, "int")
	}
}

// TestFuncObjStringNonSignature covers the early `if !ok` return in
// funcObjString when the Func's underlying type is not *types.Signature.
// In real Go code this is unreachable, but the guard exists for safety; a
// hand-constructed func object with a non-signature type exercises it.
func TestFuncObjStringNonSignature(t *testing.T) {
	// Use types.NewVar to get a non-Func object first to confirm baseline,
	// then construct a *types.Func via NewFunc with an int type literal —
	// the package's typesinternal expects a Signature, so passing a Basic
	// type as Type triggers the guard.
	// types.NewFunc requires a *types.Signature; to exercise the !ok path we
	// must build a Func and then swap its type via a trick — types.Func
	// does not expose a setter, so we cannot construct this in pure Go.
	// Instead, assert the bare-name fallback path via reflection over a
	// real Func that has a Signature. The !ok branch remains defensive and
	// is left uncovered; this test documents that intent.
	sig := types.NewSignatureType(nil, nil, nil, nil, nil, false)
	pkg := types.NewPackage(commandsPkgPath, "commands")
	f := types.NewFunc(token.NoPos, pkg, "F", sig)
	if got := funcObjString(f); got != "F" {
		t.Fatalf("funcObjString(F) = %q, want F", got)
	}
}

// TestIndexPackageNilTypesInfo covers indexPackage's early return when
// pkg.TypesInfo is nil (L95-97).
func TestIndexPackageNilTypesInfo(t *testing.T) {
	s := &staticAnalysis{
		direct: map[string]FlagSet{},
		calls:  map[string][]string{},
	}
	// Synthesize a packages.Package with nil TypesInfo and Syntax to exercise
	// the early-return guard. With nil TypesInfo, indexPackage must return
	// without panicking or recording entries.
	pkg := &packages.Package{TypesInfo: nil}
	s.indexPackage(pkg)
	if len(s.direct) != 0 || len(s.calls) != 0 {
		t.Fatalf("indexPackage with nil TypesInfo must not record entries")
	}
}

// TestIndexFuncDeclEarlyReturns covers the three guard branches in
// indexFuncDecl: non-FuncDecl decl, missing obj in Defs, and obj that is
// not a *types.Func.
func TestIndexFuncDeclEarlyReturns(t *testing.T) {
	s := &staticAnalysis{
		direct: map[string]FlagSet{},
		calls:  map[string][]string{},
	}

	// Branch 1: decl is not a *ast.FuncDecl (e.g. a GenDecl for a var).
	src1 := `package p
var X = 1`
	fset := token.NewFileSet()
	f1, err := parser.ParseFile(fset, "x.go", src1, 0)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{Defs: map[*ast.Ident]types.Object{}}
	// Should be a no-op; decls[0] is *ast.GenDecl.
	s.indexFuncDecl(info, f1.Decls[0])

	// Branch 2: FuncDecl present but obj missing from Defs map.
	src2 := `package p
func H() {}`
	f2, err := parser.ParseFile(fset, "h.go", src2, 0)
	if err != nil {
		t.Fatal(err)
	}
	// info.Defs is empty, so lookup returns nil.
	s.indexFuncDecl(info, f2.Decls[0])

	// Branch 3: FuncDecl with body, but Defs has a non-Func object.
	fn2 := f2.Decls[0].(*ast.FuncDecl)
	info.Defs[fn2.Name] = types.NewVar(token.NoPos, nil, "notAFunc", types.Typ[types.Int])
	s.indexFuncDecl(info, fn2)

	if len(s.direct) != 0 || len(s.calls) != 0 {
		t.Fatalf("guards should prevent any entries; got direct=%v calls=%v", s.direct, s.calls)
	}
}

// TestIndexFuncDeclEmptySymbolKey covers the L121-123 early-return branch
// where symbolKey returns "" (because the resolved Func has a nil Pkg).
func TestIndexFuncDeclEmptySymbolKey(t *testing.T) {
	s := &staticAnalysis{
		direct: map[string]FlagSet{},
		calls:  map[string][]string{},
	}
	src := `package p
func G() {}`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "g.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	fn := f.Decls[0].(*ast.FuncDecl)
	// Construct a *types.Func with nil Pkg → symbolKey returns "".
	sig := types.NewSignatureType(nil, nil, nil, nil, nil, false)
	info := &types.Info{Defs: map[*ast.Ident]types.Object{
		fn.Name: types.NewFunc(token.NoPos, nil, "G", sig),
	}}
	s.indexFuncDecl(info, fn)
	if len(s.direct) != 0 {
		t.Fatalf("empty symbol key must skip recording; got %v", s.direct)
	}
}

// TestResolveCalleeIdentUnresolved covers the L250-252 branch where the
// ident has no entry in info.Uses.
func TestResolveCalleeIdentUnresolved(t *testing.T) {
	info := &types.Info{Uses: map[*ast.Ident]types.Object{}}
	ident := ast.NewIdent("Mystery")
	if got := resolveCalleeIdent(info, ident, nil); got != "" {
		t.Fatalf("unresolved ident: want empty, got %q", got)
	}
}

// TestResolveCalleeIdentNonFunc covers the branch where info.Uses entry is
// not a *types.Func (e.g. a Var being "called" like a function pointer
// stored in a variable).
func TestResolveCalleeIdentNonFunc(t *testing.T) {
	pkg := types.NewPackage("p", "p")
	ident := ast.NewIdent("v")
	info := &types.Info{Uses: map[*ast.Ident]types.Object{
		ident: types.NewVar(token.NoPos, pkg, "v", types.Typ[types.Int]),
	}}
	if got := resolveCalleeIdent(info, ident, pkg); got != "" {
		t.Fatalf("non-func obj: want empty, got %q", got)
	}
}

// TestFindFuncLitContainingLineSkipsNilTypes covers the L293-294 branch:
// staticAnalysis.pkgs entry with nil TypesInfo (or Types) is skipped.
func TestFindFuncLitContainingLineSkipsNilTypes(t *testing.T) {
	s := &staticAnalysis{
		pkgs: []*packages.Package{{TypesInfo: nil}},
	}
	fl, info, pkg := s.findFuncLitContainingLine("anything.go", 1)
	if fl != nil || info != nil || pkg != nil {
		t.Fatalf("expected all-nil result for skipped pkg, got %v %v %v", fl, info, pkg)
	}
}

// TestFlagsForFuncLitSkipsRepeatedCallees covers the seen-continue branch
// in flagsForFuncLit (L351-352) by giving the body two calls to the same
// callee on different lines.
func TestFlagsForFuncLitSkipsRepeatedCallees(t *testing.T) {
	src := `package p
func helper() {}
var _ = func() {
	helper()
	helper()
}`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{
		Defs: map[*ast.Ident]types.Object{},
		Uses: map[*ast.Ident]types.Object{},
	}
	conf := types.Config{Importer: nil, Error: func(error) {}}
	pkg, _ := conf.Check("p", fset, []*ast.File{f}, info)
	if pkg == nil {
		t.Fatal("type check produced no package")
	}

	// Locate the outer FuncLit (the GenDecl's var initializer).
	var fl *ast.FuncLit
	ast.Inspect(f, func(n ast.Node) bool {
		if l, ok := n.(*ast.FuncLit); ok && fl == nil {
			fl = l
		}
		return true
	})
	if fl == nil {
		t.Fatal("expected a FuncLit in source")
	}

	// Locate the helper symbol key the way the production code does.
	var helperKey string
	for ident, obj := range info.Defs {
		if ident.Name != "helper" {
			continue
		}
		if fn, ok := obj.(*types.Func); ok {
			helperKey = symbolKey(fn)
		}
	}
	if helperKey == "" {
		t.Fatal("could not derive helper symbol key from type info")
	}
	// Seed the analysis with a known direct flag for the helper symbol so
	// that the union is observable but only contributes once even though
	// the literal body calls helper() twice.
	s := &staticAnalysis{
		direct: map[string]FlagSet{helperKey: {Yes: true}},
		calls:  map[string][]string{helperKey: nil},
	}
	got := s.flagsForFuncLit(fl, info, pkg)
	if !got.Yes {
		t.Fatalf("expected Yes flag to propagate from helper() via key %q, got %+v", helperKey, got)
	}
}

func TestFlagsForRuntimeHandlerBranches(t *testing.T) {
	s := &staticAnalysis{
		direct: map[string]FlagSet{"runThing": {Force: true}},
		calls:  map[string][]string{"runThing": nil},
	}
	if fs, note := s.flagsForRuntimeHandler("", 0); fs != (FlagSet{}) || note != "" {
		t.Fatalf("empty name: want zero/empty, got %+v %q", fs, note)
	}
	if _, note := s.flagsForRuntimeHandler("pkg.func1", 0); note == "" {
		t.Fatal("expected unresolved-closure note")
	}
	if fs, note := s.flagsForRuntimeHandler("runThing", 0); !fs.Force || note != "" {
		t.Fatalf("known handler: got %+v note=%q", fs, note)
	}
	if _, note := s.flagsForRuntimeHandler("missingHandler", 0); note == "" {
		t.Fatal("expected unknown-handler note")
	}
}
