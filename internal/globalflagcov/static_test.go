package globalflagcov

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"
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
	src := `package p
var _ = func() { x := func() { _ = 1 }; _ = x }`
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
	best := tightestFuncLit(fset, lits)
	// The inner (smaller span) literal must win regardless of input order.
	innerSpan := fset.Position(lits[1].End()).Line - fset.Position(lits[1].Pos()).Line
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
