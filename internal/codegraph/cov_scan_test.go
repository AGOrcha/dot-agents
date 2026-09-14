package codegraph

// Native-scanner extraction arms: the import forms, call shapes, receiver
// shadowing constructs and position-span edge cases the shared fixture tree
// never reaches. Every case asserts the emitted nodes and edges, because node
// identity, line ranges, parameter text and call-target resolution are the
// ingester's whole contract.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// ── helpers ─────────────────────────────────────────────────────────────────

// covScanUnit extracts body as the named repo-relative Go file of a throwaway
// root and returns the single scanned unit.
func covScanUnit(t *testing.T, rel, body string) SourceFile {
	t.Helper()
	root := t.TempDir()
	writeExtra(t, root, rel, body)
	files, err := ScanPaths(root, []string{rel})
	if err != nil {
		t.Fatalf("ScanPaths: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("ScanPaths extracted %d units, want exactly 1", len(files))
	}
	return files[0]
}

// covScanFileScan parses src in memory and returns the extraction state bound
// to it. It is the way in for the arms that guard against an AST no Go parser
// would ever hand the ingester.
func covScanFileScan(t *testing.T, src string) (*fileScan, *ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "mem.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return &fileScan{
		path:    "mem.go",
		src:     []byte(src),
		fset:    fset,
		defined: definedNames(file),
		dead:    deadGuardSpans(file),
	}, file
}

// covScanCallsFrom lists, in emission order, the CALLS targets attributed to
// one declaration.
func covScanCallsFrom(f SourceFile, source string) []string {
	var out []string
	for _, e := range f.Edges {
		if e.Kind == edgeCalls && e.Source == source {
			out = append(out, e.Target)
		}
	}
	return out
}

// covScanDecl returns the declaration with the given display name.
func covScanDecl(t *testing.T, f SourceFile, name string) Decl {
	t.Helper()
	for _, d := range f.Decls {
		if d.Name == name {
			return d
		}
	}
	t.Fatalf("no declaration named %q in %v", name, f.Decls)
	return Decl{}
}

// covScanDefine builds the `s := v` the shadow index treats as a binding.
func covScanDefine() *ast.AssignStmt {
	return &ast.AssignStmt{
		Lhs: []ast.Expr{ast.NewIdent("s")},
		Tok: token.DEFINE,
		Rhs: []ast.Expr{ast.NewIdent("v")},
	}
}

// ── imports ─────────────────────────────────────────────────────────────────

const covScanImportFixture = `package p

import (
	"fmt"
	alias "strings"
	. "bytes"
	_ "embed"
	` + "`text/template`" + `
)

import "os"

var _ = fmt.Sprint
`

// TestCovScanImportEdgesReportEveryInterpretedPath pins the import contract:
// the target is the raw path and never the alias, every member of a group
// reports the `import (` line rather than its own, and a path written as a raw
// string literal is not an interpreted one, so upstream reads nothing from it.
func TestCovScanImportEdgesReportEveryInterpretedPath(t *testing.T) {
	f := covScanUnit(t, "imports.go", covScanImportFixture)
	var got []string
	for _, e := range f.Edges {
		if e.Kind == edgeImportsFrom {
			if e.Source != f.Path {
				t.Errorf("import edge source = %q, want the file node %q", e.Source, f.Path)
			}
			got = append(got, fmt.Sprintf("%s@%d", e.Target, e.Line))
		}
	}
	want := []string{"fmt@3", "strings@3", "bytes@3", "embed@3", "os@11"}
	if !slices.Equal(got, want) {
		t.Fatalf("IMPORTS_FROM = %v, want %v", got, want)
	}
}

// TestCovScanImportPathThatCannotUnquoteEmitsNoEdge covers the guard behind
// the parser: go/parser rejects any import literal strconv.Unquote would
// refuse, so only a hand-built AST reaches the arm. A path the ingester cannot
// read must contribute no edge rather than a garbage target.
func TestCovScanImportPathThatCannotUnquoteEmitsNoEdge(t *testing.T) {
	s, file := covScanFileScan(t, "package p\n\nimport \"fmt\"\n")
	decl, ok := file.Decls[0].(*ast.GenDecl)
	if !ok {
		t.Fatalf("first declaration is %T, want an import declaration", file.Decls[0])
	}
	spec, ok := decl.Specs[0].(*ast.ImportSpec)
	if !ok {
		t.Fatalf("first spec is %T, want an import spec", decl.Specs[0])
	}
	spec.Path.Value = `"\q"`

	s.addImports(decl)

	if len(s.edges) != 0 {
		t.Fatalf("edges = %+v, want none for an unreadable import path", s.edges)
	}
}

// ── call targets ────────────────────────────────────────────────────────────

const covScanCallFixture = `package p

import "fmt"

// Store nests itself so a chained selector has an operand to read through.
type Store struct{ dep *Store }

// Name is the only method a receiver call can resolve to.
func (s *Store) Name() string { return "" }

func local() string { return "" }

func (s *Store) Describe(other *Store) string {
	return fmt.Sprint(
		s.Name(),
		(*s).Name(),
		(s).Name(),
		other.Name(),
		s.dep.Name(),
		local(),
		missing(),
		func() string { return "" }(),
	)
}
`

// TestCovScanCallTargetsFollowUpstreamResolution pins the deliberately weak
// resolution: only a call through the enclosing method's own receiver — with
// the parentheses and one pointer dereference unwrapped — names the sibling
// method, a bare name resolves against the file's own declarations, and every
// other selector or non-name callee stays bare or emits nothing at all.
func TestCovScanCallTargetsFollowUpstreamResolution(t *testing.T) {
	f := covScanUnit(t, "calls.go", covScanCallFixture)
	method := f.Path + "::Store.Name"
	want := []string{
		"Sprint", // a package selector keeps the bare member name
		method,   // s.Name()
		method,   // (*s).Name()
		method,   // (s).Name()
		"Name",   // another value of the same type is not the receiver
		"Name",   // s.dep.Name() reads through a field, not the receiver
		f.Path + "::local",
		"missing", // no declaration of that name in this file
		// the immediately-invoked literal is not a named callee at all
	}
	got := covScanCallsFrom(f, f.Path+"::Store.Describe")
	if !slices.Equal(got, want) {
		t.Fatalf("CALLS from Describe = %v, want %v", got, want)
	}
}

const covScanDeadGuardFixture = `package p

func guarded() string {
	if ((false)) {
		return dead()
	}
	return live()
}

func dead() string { return "" }

func live() string { return "" }
`

// TestCovScanParenthesizedFalseGuardDropsItsCalls covers the dead-branch
// index through parentheses: `if ((false))` is still the literal false, so the
// consequence is never evaluated and contributes no call edge.
func TestCovScanParenthesizedFalseGuardDropsItsCalls(t *testing.T) {
	f := covScanUnit(t, "guard.go", covScanDeadGuardFixture)
	got := covScanCallsFrom(f, f.Path+"::guarded")
	want := []string{f.Path + "::live"}
	if !slices.Equal(got, want) {
		t.Fatalf("CALLS from guarded = %v, want %v", got, want)
	}
}

const covScanLocalTypeFixture = `package p

func first(v int) int {
	type inner int
	return int(inner(v))
}

func second(v int) int {
	type inner int
	return int(inner(v))
}
`

// TestCovScanResolvesTypeDeclaredInsideAFunction covers the second resolution
// arm: a type declared in a function body is a node but not a file-scope name,
// so the bare call target only becomes qualified in the post-parse pass.
func TestCovScanResolvesTypeDeclaredInsideAFunction(t *testing.T) {
	f := covScanUnit(t, "localtype.go", covScanLocalTypeFixture)
	inner := f.Path + "::inner"
	for _, fn := range []string{"first", "second"} {
		got := covScanCallsFrom(f, f.Path+"::"+fn)
		want := []string{"int", inner}
		if !slices.Equal(got, want) {
			t.Errorf("CALLS from %s = %v, want %v", fn, got, want)
		}
	}
	if d := covScanDecl(t, f, "inner"); d.Symbol.Kind != kindClass {
		t.Fatalf("inner kind = %q, want %q", d.Symbol.Kind, kindClass)
	}
}

const covScanDuplicateMethodFixture = `package p

type Dup struct{}

func (d *Dup) twice() {}

func (d *Dup) twice() {}

func (d *Dup) call() { d.twice() }
`

// TestCovScanDuplicateDeclarationStillResolves covers the candidate-dedupe: a
// file caught mid-edit can declare one method twice, and the two identical
// candidates must collapse to one, or the receiver call would look ambiguous
// and lose its target.
func TestCovScanDuplicateDeclarationStillResolves(t *testing.T) {
	f := covScanUnit(t, "dup.go", covScanDuplicateMethodFixture)
	got := covScanCallsFrom(f, f.Path+"::Dup.call")
	want := []string{f.Path + "::Dup.twice"}
	if !slices.Equal(got, want) {
		t.Fatalf("CALLS from call = %v, want %v", got, want)
	}
}

// ── receiver shadowing ──────────────────────────────────────────────────────

// covScanShadowFixture writes one method per lexical construct upstream's
// shadow index understands. Each calls Name() through the receiver spelling
// `s`, so the emitted target says whether that spelling still meant the
// receiver at the call site.
const covScanShadowFixture = `package p

type Store struct{}

func (s *Store) Name() string { return "" }

func (s *Store) viaReceiver() string { return s.Name() }

func (s *Store) topLevelRedeclare() string {
	s := 0
	_ = s
	return s.Name()
}

func (s *Store) nestedRedeclare() string {
	{
		s := 0
		_ = s
		return s.Name()
	}
}

func (s *Store) varShadow() string {
	var s int
	_ = s
	return s.Name()
}

func (s *Store) otherVar() string {
	var other int
	_ = other
	return s.Name()
}

func (s *Store) typeShadow() string {
	type s struct{}
	return s.Name()
}

func (s *Store) ifInit() string {
	if s := 0; s == 0 {
		return s.Name()
	}
	return ""
}

func (s *Store) switchInit() string {
	switch s := 0; s {
	case 0:
		return s.Name()
	}
	return ""
}

func (s *Store) forInit() string {
	for s := 0; s < 1; s++ {
		return s.Name()
	}
	return ""
}

func (s *Store) rangeShadow(list []int) string {
	for _, s := range list {
		return s.Name()
	}
	return ""
}

func (s *Store) selectShadow(ch chan int) string {
	select {
	case s := <-ch:
		_ = s
		return s.Name()
	}
}

func (s *Store) typeSwitchShadow(v any) string {
	switch s := v.(type) {
	case int:
		return s.Name()
	case string:
	}
	return ""
}

func (s *Store) plainTypeSwitch(v any) string {
	switch v.(type) {
	case int:
		return s.Name()
	}
	return ""
}

func (s *Store) litParamShadow() string {
	var out string
	func(s int) { out = s.Name() }(0)
	return out
}

func (s *Store) litResultShadow() string {
	var out string
	_ = func() (s int) { out = s.Name(); return 0 }()
	return out
}

func (s *Store) litNoBinding() string {
	var out string
	func(n int) { out = s.Name() }(0)
	return out
}
`

// TestCovScanReceiverShadowingDecidesCallTargets pins the lexical-shadow
// index. Inside a range where the receiver's spelling was rebound, the call is
// no longer a receiver call and its target stays bare; outside one it resolves
// to the sibling method. The two quirks upstream's index carries are pinned
// with it: a `:=` in the method body's own scope never shadows, and a func
// literal only shadows when its signature binds the name.
func TestCovScanReceiverShadowingDecidesCallTargets(t *testing.T) {
	f := covScanUnit(t, "shadow.go", covScanShadowFixture)
	resolved := f.Path + "::Store.Name"
	cases := []struct {
		method string
		want   string
	}{
		{"viaReceiver", resolved},
		{"topLevelRedeclare", resolved},
		{"nestedRedeclare", "Name"},
		{"varShadow", "Name"},
		{"otherVar", resolved},
		{"typeShadow", "Name"},
		{"ifInit", "Name"},
		{"switchInit", "Name"},
		{"forInit", "Name"},
		{"rangeShadow", "Name"},
		{"selectShadow", "Name"},
		{"typeSwitchShadow", "Name"},
		{"plainTypeSwitch", resolved},
		{"litParamShadow", "Name"},
		{"litResultShadow", "Name"},
		{"litNoBinding", resolved},
	}
	for _, c := range cases {
		t.Run(c.method, func(t *testing.T) {
			got := covScanCallsFrom(f, f.Path+"::Store."+c.method)
			if !slices.Equal(got, []string{c.want}) {
				t.Fatalf("CALLS = %v, want [%s]", got, c.want)
			}
		})
	}
}

// TestCovScanShadowScanGuardsMalformedInput covers the shadow index's guards.
// The Go parser cannot produce these shapes — a type switch always has a body
// of case clauses, a func literal always has a type — so the guards exist to
// keep a synthesized or partial AST from panicking or inventing a range.
func TestCovScanShadowScanGuardsMalformedInput(t *testing.T) {
	newScan := func() *shadowScan {
		return &shadowScan{name: "s", bound: map[span]bool{}}
	}
	t.Run("a missing func type binds nothing", func(t *testing.T) {
		if newScan().signatureBinds(nil) {
			t.Fatal("a nil signature must not bind the receiver name")
		}
	})
	t.Run("a missing assignment binds nothing", func(t *testing.T) {
		if newScan().lhsBinds(nil) {
			t.Fatal("a nil assignment must not bind the receiver name")
		}
	})
	t.Run("a type switch without a body records no range", func(t *testing.T) {
		sh := newScan()
		sh.typeSwitch(&ast.TypeSwitchStmt{Assign: covScanDefine()})
		if len(sh.spans) != 0 {
			t.Fatalf("spans = %v, want none without case clauses", sh.spans)
		}
	})
	t.Run("a type switch skips a statement that is not a clause", func(t *testing.T) {
		sh := newScan()
		sh.typeSwitch(&ast.TypeSwitchStmt{
			Assign: covScanDefine(),
			Body:   &ast.BlockStmt{List: []ast.Stmt{&ast.EmptyStmt{}}},
		})
		if len(sh.spans) != 0 {
			t.Fatalf("spans = %v, want none for a body with no clause", sh.spans)
		}
	})
	t.Run("a scope that already binds the name records one range", func(t *testing.T) {
		assign := covScanDefine()
		owner := &ast.IfStmt{
			If:   1,
			Init: assign,
			Cond: ast.NewIdent("ok"),
			Body: &ast.BlockStmt{Lbrace: 10, Rbrace: 20},
		}
		first := newScan()
		first.initStmt(assign, owner)
		if len(first.spans) != 1 {
			t.Fatalf("spans = %v, want exactly one binding range", first.spans)
		}
		second := newScan()
		second.bound[key(owner)] = true
		second.initStmt(assign, owner)
		if len(second.spans) != 0 {
			t.Fatalf("spans = %v, want none in an already bound scope", second.spans)
		}
		if !second.isClaimed(assign) {
			t.Fatal("the init binding must be claimed so its own visit adds nothing")
		}
	})
}

// ── position spans ──────────────────────────────────────────────────────────

// TestCovScanMergeSpansCoalescesRanges pins the merge the shadow and
// dead-branch indexes both depend on: sorted output, touching ranges fused,
// a nested range absorbed, and an empty range dropped. inSpans binary-searches
// the result, so an unsorted or overlapping list would silently miss hits.
func TestCovScanMergeSpansCoalescesRanges(t *testing.T) {
	cases := []struct {
		name string
		in   []span
		want []span
	}{
		{"sorts disjoint ranges", []span{{start: 10, end: 12}, {start: 1, end: 3}},
			[]span{{start: 1, end: 3}, {start: 10, end: 12}}},
		{"fuses overlapping ranges", []span{{start: 1, end: 5}, {start: 3, end: 9}},
			[]span{{start: 1, end: 9}}},
		{"fuses adjacent ranges", []span{{start: 1, end: 5}, {start: 5, end: 9}},
			[]span{{start: 1, end: 9}}},
		{"absorbs a nested range", []span{{start: 1, end: 9}, {start: 3, end: 5}},
			[]span{{start: 1, end: 9}}},
		{"drops an empty range", []span{{start: 4, end: 4}, {start: 1, end: 3}},
			[]span{{start: 1, end: 3}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := mergeSpans(c.in); !slices.Equal(got, c.want) {
				t.Fatalf("mergeSpans = %v, want %v", got, c.want)
			}
		})
	}
}

// ── source text ─────────────────────────────────────────────────────────────

const covScanParamsFixture = `package p

type T struct{}

func Wide(
	a int,
	b string,
) {}

// Extern is implemented elsewhere, so it has no body at all.
func (t *T) Extern() int
`

// TestCovScanParamsTextIsVerbatimSource pins the parameter text a node
// carries: the first parenthesised list exactly as written, newlines and all,
// and for a method the receiver list rather than the call parameters. A
// bodyless method still declares a node, which is also the only way the
// shadow index is asked about a method with no body.
func TestCovScanParamsTextIsVerbatimSource(t *testing.T) {
	f := covScanUnit(t, "params.go", covScanParamsFixture)

	wide := covScanDecl(t, f, "Wide")
	if want := "(\n\ta int,\n\tb string,\n)"; wide.Params != want {
		t.Errorf("Wide params = %q, want %q", wide.Params, want)
	}
	if wide.Symbol.LineStart != 5 || wide.LineEnd != 8 {
		t.Errorf("Wide lines = %d..%d, want 5..8", wide.Symbol.LineStart, wide.LineEnd)
	}

	extern := covScanDecl(t, f, "Extern")
	if want := "(t *T)"; extern.Params != want {
		t.Errorf("Extern params = %q, want %q", extern.Params, want)
	}
	if extern.Symbol.QualifiedName != f.Path+"::T.Extern" {
		t.Errorf("Extern identity = %q, want the receiver-qualified name", extern.Symbol.QualifiedName)
	}
	if extern.Symbol.Kind != kindFunction {
		t.Errorf("Extern kind = %q, want %q", extern.Symbol.Kind, kindFunction)
	}
}

// TestCovScanParamsTextWithoutAParameterListIsEmpty covers the guard for a
// declaration carrying no parenthesised list: the node still exists, it just
// has no parameter text.
func TestCovScanParamsTextWithoutAParameterListIsEmpty(t *testing.T) {
	s := &fileScan{}
	decl := &ast.FuncDecl{Name: ast.NewIdent("F"), Type: &ast.FuncType{}}
	if got := s.paramsText(decl); got != "" {
		t.Fatalf("paramsText = %q, want empty for a declaration with no list", got)
	}
}

// TestCovScanSliceRejectsOffsetsOutsideTheSource pins the source-text guard:
// every node's text is read by byte offset, so an offset that cannot name a
// range must yield no text rather than panic on the slice.
func TestCovScanSliceRejectsOffsetsOutsideTheSource(t *testing.T) {
	s := &fileScan{src: []byte("package p")}
	if got := s.slice(0, 7); got != "package" {
		t.Fatalf("slice(0, 7) = %q, want %q", got, "package")
	}
	cases := []struct {
		name       string
		start, end int
	}{
		{"negative start", -1, 3},
		{"end past the source", 0, 99},
		{"empty range", 3, 3},
		{"inverted range", 5, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := s.slice(c.start, c.end); got != "" {
				t.Fatalf("slice(%d, %d) = %q, want empty", c.start, c.end, got)
			}
		})
	}
}

// ── root resolution ─────────────────────────────────────────────────────────

// TestCovScanPathsSkipsNonGoAndRepeatedPaths pins the batch filter: a path
// that is not Go source, and a path named twice, are silently dropped, because
// the incremental caller reads "absent from the scan" as its purge signal.
func TestCovScanPathsSkipsNonGoAndRepeatedPaths(t *testing.T) {
	root := t.TempDir()
	writeExtra(t, root, "lib/lib.go", "package lib\n\nfunc F() {}\n")
	writeExtra(t, root, "README.md", "# not go\n")

	files, err := ScanPaths(root, []string{"lib/lib.go", "README.md", "lib/lib.go"})
	if err != nil {
		t.Fatalf("ScanPaths: %v", err)
	}
	if len(files) != 1 || files[0].RelPath != "lib/lib.go" {
		t.Fatalf("ScanPaths extracted %d units (%+v), want only lib/lib.go", len(files), files)
	}
}

// covScanHideWorkingDir makes the process working directory one whose absolute
// path cannot be resolved: deeper than PATH_MAX, so the getcwd syscall refuses
// it, and under an unreadable parent, so the "walk up ..'" fallback cannot
// rebuild it either. Paths relative to it keep resolving, which is what lets a
// scan get as far as resolving its root.
func covScanHideWorkingDir(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("only a POSIX non-root process can be denied its own working directory")
	}
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}
	base := t.TempDir()
	t.Cleanup(func() {
		_ = os.Chmod("..", 0o755)
		if err := os.Chdir(orig); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})
	if err := os.Chdir(base); err != nil {
		t.Fatalf("chdir %s: %v", base, err)
	}
	name := strings.Repeat("d", 250)
	for i := range 20 {
		if err := os.Mkdir(name, 0o755); err != nil {
			t.Fatalf("mkdir level %d: %v", i, err)
		}
		if err := os.Chdir(name); err != nil {
			t.Fatalf("chdir level %d: %v", i, err)
		}
	}
	if err := os.Chmod("..", 0); err != nil {
		t.Fatalf("hide parent directory: %v", err)
	}
}

// TestCovScanRejectsARootItCannotMakeAbsolute pins the one failure both entry
// points report. Every node identity is the absolute file path, so a root that
// cannot be made absolute would make every identity in the batch wrong: the
// scan must fail loudly instead of producing a graph nobody can join against.
func TestCovScanRejectsARootItCannotMakeAbsolute(t *testing.T) {
	covScanHideWorkingDir(t)
	writeExtra(t, ".", "pkg/x.go", "package pkg\n\nfunc F() {}\n")

	files, _, err := Scan("pkg", "commit")
	if err == nil || !strings.Contains(err.Error(), "resolve root pkg") {
		t.Fatalf("Scan error = %v, want a root-resolution failure", err)
	}
	if files != nil {
		t.Errorf("Scan returned %d units alongside its error, want none", len(files))
	}

	batch, err := ScanPaths("pkg", []string{"x.go"})
	if err == nil || !strings.Contains(err.Error(), "resolve root pkg") {
		t.Fatalf("ScanPaths error = %v, want a root-resolution failure", err)
	}
	if batch != nil {
		t.Errorf("ScanPaths returned %d units alongside its error, want none", len(batch))
	}
}
