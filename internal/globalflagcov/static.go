package globalflagcov

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"log/slog"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

const commandsPkgPath = "github.com/AGOrcha/dot-agents/commands"

type staticAnalysis struct {
	pkgs []*packages.Package

	// funcKey -> direct flags in that function body
	direct map[string]FlagSet
	// funcKey -> callees (same-command-tree func keys)
	calls map[string][]string
}

func loadStatic(moduleRoot string, runs []runRecord) (*staticAnalysis, error) {
	abs, err := filepath.Abs(moduleRoot)
	if err != nil {
		return nil, err
	}
	okPkgs, err := loadCommandPackages(abs, runs)
	if err != nil {
		return nil, err
	}

	s := &staticAnalysis{
		pkgs:   okPkgs,
		direct: make(map[string]FlagSet),
		calls:  make(map[string][]string),
	}
	for _, pkg := range okPkgs {
		s.indexPackage(pkg)
	}
	return s, nil
}

// loadCommandPackages derives the packages that define handlers in the live
// command tree, then returns only those without errors and with the syntax/types
// data the analyzer needs.
func loadCommandPackages(absDir string, runs []runRecord) ([]*packages.Package, error) {
	patterns, err := commandPackagePatterns(absDir, runs)
	if err != nil {
		return nil, err
	}
	return loadPackagePatterns(absDir, patterns)
}

// commandPackagePatterns maps each reachable handler's runtime source file to
// its package directory. This deliberately avoids ./commands/...: packages with
// no handler wired into the root command tree stay outside the analysis.
func commandPackagePatterns(absDir string, runs []runRecord) ([]string, error) {
	seen := make(map[string]struct{})
	for _, run := range runs {
		fn := runtime.FuncForPC(run.pc)
		if fn == nil {
			return nil, fmt.Errorf("derive command packages: no runtime function for handler %q", run.handlerName)
		}
		file, _ := fn.FileLine(run.pc)
		if file == "" {
			return nil, fmt.Errorf("derive command packages: no source file for handler %q", run.handlerName)
		}
		rel, err := filepath.Rel(absDir, filepath.Dir(file))
		if err != nil {
			return nil, fmt.Errorf("derive command packages for %q: %w", run.handlerName, err)
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("derive command packages: handler %q is outside module root", run.handlerName)
		}
		pattern := "."
		if rel != "." {
			pattern = "./" + filepath.ToSlash(rel)
		}
		seen[pattern] = struct{}{}
	}
	if len(seen) == 0 {
		return nil, fmt.Errorf("derive command packages: no reachable handlers")
	}

	patterns := make([]string, 0, len(seen))
	for pattern := range seen {
		patterns = append(patterns, pattern)
	}
	sort.Strings(patterns)
	return patterns, nil
}

func loadPackagePatterns(absDir string, patterns []string) ([]*packages.Package, error) {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedModule,
		Dir:  absDir,
	}
	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, err
	}
	var okPkgs []*packages.Package
	for _, p := range pkgs {
		if len(p.Errors) > 0 {
			slog.Default().Warn("globalflagcov: package failed to load, excluding from analysis",
				"package", p.PkgPath, "error_count", len(p.Errors), "first_error", p.Errors[0])
			continue
		}
		if p.TypesInfo == nil || p.Types == nil || len(p.Syntax) == 0 {
			continue
		}
		okPkgs = append(okPkgs, p)
	}
	if len(okPkgs) == 0 {
		return nil, fmt.Errorf("packages.Load: no packages without errors")
	}
	return okPkgs, nil
}

// indexPackage walks every top-level FuncDecl in pkg and records its direct
// flag references and same-package callees in the analysis maps.
func (s *staticAnalysis) indexPackage(pkg *packages.Package) {
	info := pkg.TypesInfo
	if info == nil {
		return
	}
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			s.indexFuncDecl(info, decl)
		}
	}
}

// indexFuncDecl records flag-set/callee info for a single ast.FuncDecl when
// the declaration has a body, a resolved type, and a non-empty symbol key.
func (s *staticAnalysis) indexFuncDecl(info *types.Info, decl ast.Decl) {
	fn, ok := decl.(*ast.FuncDecl)
	if !ok || fn.Body == nil {
		return
	}
	obj := info.Defs[fn.Name]
	if obj == nil {
		return
	}
	f, ok := obj.(*types.Func)
	if !ok {
		return
	}
	key := symbolKey(f)
	if key == "" {
		return
	}
	s.direct[key] = directFlagsInBody(fn.Body)
	s.calls[key] = collectCallees(info, fn.Body, f.Pkg())
}

func symbolKey(f *types.Func) string {
	if f == nil || f.Pkg() == nil {
		return ""
	}
	return packageQualifier(f.Pkg()) + funcObjString(f)
}

func packageQualifier(p *types.Package) string {
	if p == nil {
		return ""
	}
	path := p.Path()
	if path == commandsPkgPath {
		return ""
	}
	sub := strings.TrimPrefix(path, commandsPkgPath+"/")
	if sub == "" || strings.Contains(sub, "/") {
		// Nested deeper than one segment under commands/ — not used for CLI today.
		return ""
	}
	return sub + "."
}

func funcObjString(f *types.Func) string {
	sig, ok := f.Type().(*types.Signature)
	if !ok {
		return f.Name()
	}
	if sig.Recv() == nil {
		return f.Name()
	}
	recv := sig.Recv().Type()
	return recvString(recv) + "." + f.Name()
}

func recvString(t types.Type) string {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	if n, ok := t.(*types.Named); ok {
		return n.Obj().Name()
	}
	return t.String()
}

func directFlagsInBody(body ast.Node) FlagSet {
	var fs FlagSet
	ast.Inspect(body, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || sel.Sel == nil {
			return true
		}
		if isFlagAccess(sel) {
			markFlag(&fs, sel.Sel.Name)
		}
		return true
	})
	return fs
}

// isFlagAccess reports whether sel reads a field on a Flags object: either
// the bare `Flags.<Name>` form or the `<ident>.Flags.<Name>` form (used in
// commands/sync style structs that embed a deps.Flags handle).
func isFlagAccess(sel *ast.SelectorExpr) bool {
	if inner, ok := sel.X.(*ast.SelectorExpr); ok && inner.Sel != nil && inner.Sel.Name == "Flags" {
		_, ok := inner.X.(*ast.Ident)
		return ok
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == "Flags"
}

// markFlag sets the matching boolean on fs for a known global flag field
// name (JSON / DryRun / Yes / Force / Verbose). Unknown names are ignored.
func markFlag(fs *FlagSet, name string) {
	switch name {
	case "JSON":
		fs.JSON = true
	case "DryRun":
		fs.DryRun = true
	case "Yes":
		fs.Yes = true
	case "Force":
		fs.Force = true
	case "Verbose":
		fs.Verbose = true
	}
}

func collectCallees(info *types.Info, body ast.Node, callerPkg *types.Package) []string {
	seen := make(map[string]bool)
	var out []string
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		key := calleeKey(info, call, callerPkg)
		if key != "" && !seen[key] {
			seen[key] = true
			out = append(out, key)
		}
		return true
	})
	return out
}

func calleeKey(info *types.Info, call *ast.CallExpr, callerPkg *types.Package) string {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return resolveCalleeIdent(info, fun, callerPkg)
	case *ast.SelectorExpr:
		return resolveCalleeIdent(info, fun.Sel, callerPkg)
	}
	return ""
}

// resolveCalleeIdent returns the symbol key of the function denoted by ident
// when the resolved object is a *types.Func declared in callerPkg. Anything
// else (cross-package call, non-func, unresolved use) returns "".
func resolveCalleeIdent(info *types.Info, ident *ast.Ident, callerPkg *types.Package) string {
	obj, ok := info.Uses[ident]
	if !ok {
		return ""
	}
	fn, ok := obj.(*types.Func)
	if !ok || !samePkg(fn, callerPkg) {
		return ""
	}
	return symbolKey(fn)
}

func samePkg(fn *types.Func, callerPkg *types.Package) bool {
	if fn == nil || fn.Pkg() == nil || callerPkg == nil {
		return false
	}
	return fn.Pkg().Path() == callerPkg.Path()
}

func (s *staticAnalysis) flagsForRuntimeHandler(runtimeName string, pc uintptr) (FlagSet, string) {
	if runtimeName == "" {
		return FlagSet{}, ""
	}
	if pc != 0 {
		if fn := runtime.FuncForPC(pc); fn != nil {
			file, line := fn.FileLine(pc)
			if file != "" && line > 0 {
				if fl, info, litPkg := s.findFuncLitContainingLine(file, line); fl != nil {
					return s.flagsForFuncLit(fl, info, litPkg), ""
				}
			}
		}
	}
	if strings.Contains(runtimeName, ".func") {
		return FlagSet{}, "unresolved closure " + runtimeName
	}
	if _, ok := s.direct[runtimeName]; ok {
		return s.transitiveFlags(runtimeName), ""
	}
	return FlagSet{}, "unknown handler " + runtimeName
}

func (s *staticAnalysis) findFuncLitContainingLine(absFile string, line int) (*ast.FuncLit, *types.Info, *types.Package) {
	want := filepath.Clean(absFile)
	for _, pkg := range s.pkgs {
		if pkg.TypesInfo == nil || pkg.Types == nil {
			continue
		}
		candidates := collectFuncLitCandidates(pkg, want, line)
		if len(candidates) == 0 {
			continue
		}
		return tightestFuncLit(pkg.Fset, candidates), pkg.TypesInfo, pkg.Types
	}
	return nil, nil, nil
}

// collectFuncLitCandidates returns every ast.FuncLit in pkg whose source
// file matches want (by full path or basename) and whose line range covers
// line.
func collectFuncLitCandidates(pkg *packages.Package, want string, line int) []*ast.FuncLit {
	fset := pkg.Fset
	var candidates []*ast.FuncLit
	for _, af := range pkg.Syntax {
		path := filepath.Clean(fset.Position(af.Pos()).Filename)
		if path != want && filepath.Base(path) != filepath.Base(want) {
			continue
		}
		ast.Inspect(af, func(n ast.Node) bool {
			fl, ok := n.(*ast.FuncLit)
			if !ok {
				return true
			}
			start := fset.Position(fl.Pos()).Line
			end := fset.Position(fl.End()).Line
			if line >= start && line <= end {
				candidates = append(candidates, fl)
			}
			return true
		})
	}
	return candidates
}

// tightestFuncLit returns the candidate ast.FuncLit with the smallest line
// span. The first element acts as the seed; ties keep the earlier entry.
func tightestFuncLit(fset *token.FileSet, candidates []*ast.FuncLit) *ast.FuncLit {
	best := candidates[0]
	bestSpan := fset.Position(best.End()).Line - fset.Position(best.Pos()).Line
	for _, fl := range candidates[1:] {
		span := fset.Position(fl.End()).Line - fset.Position(fl.Pos()).Line
		if span < bestSpan {
			best = fl
			bestSpan = span
		}
	}
	return best
}

func (s *staticAnalysis) flagsForFuncLit(fl *ast.FuncLit, info *types.Info, litPkg *types.Package) FlagSet {
	fs := directFlagsInBody(fl.Body)
	seen := make(map[string]bool)
	for _, c := range collectCallees(info, fl.Body, litPkg) {
		if seen[c] {
			continue
		}
		seen[c] = true
		tf := s.transitiveFlags(c)
		fs = union(fs, tf)
	}
	return fs
}

func (s *staticAnalysis) transitiveFlags(root string) FlagSet {
	visited := make(map[string]bool)
	var out FlagSet
	var walk func(string)
	walk = func(name string) {
		if visited[name] {
			return
		}
		visited[name] = true
		out = union(out, s.direct[name])
		for _, c := range s.calls[name] {
			walk(c)
		}
	}
	walk(root)
	return out
}

func union(a, b FlagSet) FlagSet {
	return FlagSet{
		JSON:    a.JSON || b.JSON,
		DryRun:  a.DryRun || b.DryRun,
		Yes:     a.Yes || b.Yes,
		Force:   a.Force || b.Force,
		Verbose: a.Verbose || b.Verbose,
	}
}
