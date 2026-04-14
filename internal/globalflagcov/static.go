package globalflagcov

import (
	"fmt"
	"go/ast"
	"go/types"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/tools/go/packages"
)

type staticAnalysis struct {
	pkg *packages.Package

	// funcKey -> direct flags in that function body
	direct map[string]FlagSet
	// funcKey -> callees (same-package func keys)
	calls map[string][]string
}

func loadStatic(moduleRoot string) (*staticAnalysis, error) {
	abs, err := filepath.Abs(moduleRoot)
	if err != nil {
		return nil, err
	}
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedModule,
		Dir:  abs,
	}
	pkgs, err := packages.Load(cfg, "./commands")
	if err != nil {
		return nil, err
	}
	if len(pkgs) == 0 || len(pkgs[0].Errors) > 0 {
		var b strings.Builder
		for _, p := range pkgs {
			for _, e := range p.Errors {
				fmt.Fprintf(&b, "%v\n", e)
			}
		}
		return nil, fmt.Errorf("packages.Load: %s", b.String())
	}
	pkg := pkgs[0]
	info := pkg.TypesInfo
	if info == nil {
		return nil, fmt.Errorf("missing TypesInfo")
	}

	s := &staticAnalysis{
		pkg:    pkg,
		direct: make(map[string]FlagSet),
		calls:  make(map[string][]string),
	}

	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			key := funcKey(pkg.Types, info, fn)
			if key == "" {
				continue
			}
			s.direct[key] = directFlagsInBody(fn.Body)
			s.calls[key] = collectCallees(info, fn.Body)
		}
	}

	return s, nil
}

func funcKey(pkg *types.Package, info *types.Info, fn *ast.FuncDecl) string {
	if obj := info.Defs[fn.Name]; obj != nil {
		if f, ok := obj.(*types.Func); ok {
			return funcObjString(f)
		}
	}
	if fn.Recv == nil {
		return fn.Name.Name
	}
	return ""
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
		if !ok {
			return true
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok || id.Name != "Flags" {
			return true
		}
		switch sel.Sel.Name {
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
		return true
	})
	return fs
}

func collectCallees(info *types.Info, body ast.Node) []string {
	seen := make(map[string]bool)
	var out []string
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		key := calleeKey(info, call)
		if key != "" && !seen[key] {
			seen[key] = true
			out = append(out, key)
		}
		return true
	})
	return out
}

func calleeKey(info *types.Info, call *ast.CallExpr) string {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		if obj, ok := info.Uses[fun]; ok {
			if fn, ok := obj.(*types.Func); ok {
				if samePkg(fn) {
					return funcObjString(fn)
				}
			}
		}
	case *ast.SelectorExpr:
		if obj, ok := info.Uses[fun.Sel]; ok {
			if fn, ok := obj.(*types.Func); ok {
				if samePkg(fn) {
					return funcObjString(fn)
				}
			}
		}
	}
	return ""
}

func samePkg(fn *types.Func) bool {
	if fn == nil || fn.Pkg() == nil {
		return false
	}
	return fn.Pkg().Path() == "github.com/NikashPrakash/dot-agents/commands"
}

func (s *staticAnalysis) flagsForRuntimeHandler(runtimeName string, pc uintptr) (FlagSet, string) {
	if runtimeName == "" {
		return FlagSet{}, ""
	}
	if pc != 0 {
		if fn := runtime.FuncForPC(pc); fn != nil {
			file, line := fn.FileLine(pc)
			if file != "" && line > 0 {
				if fl := s.findFuncLitContainingLine(file, line); fl != nil {
					return s.flagsForFuncLit(fl), ""
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

func (s *staticAnalysis) findFuncLitContainingLine(absFile string, line int) *ast.FuncLit {
	want := filepath.Clean(absFile)
	fset := s.pkg.Fset
	var candidates []*ast.FuncLit
	for _, af := range s.pkg.Syntax {
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
	if len(candidates) == 0 {
		return nil
	}
	// Innermost: prefer the smallest source span (nested closures).
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

func (s *staticAnalysis) flagsForFuncLit(fl *ast.FuncLit) FlagSet {
	fs := directFlagsInBody(fl.Body)
	seen := make(map[string]bool)
	for _, c := range collectCallees(s.pkg.TypesInfo, fl.Body) {
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
