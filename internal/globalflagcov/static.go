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

const commandsPkgPath = "github.com/NikashPrakash/dot-agents/commands"

type staticAnalysis struct {
	pkgs []*packages.Package

	// funcKey -> direct flags in that function body
	direct map[string]FlagSet
	// funcKey -> callees (same-command-tree func keys)
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
	// Load explicit command packages only. A glob like ./commands/... would also
	// type-check experimental subpackages (e.g. commands/workflow) that are not
	// yet wired into the root CLI build graph.
	pkgs, err := packages.Load(cfg,
		"./commands",
		"./commands/sync",
		"./commands/hooks",
		"./commands/skills",
	)
	if err != nil {
		return nil, err
	}
	var okPkgs []*packages.Package
	for _, p := range pkgs {
		if len(p.Errors) > 0 {
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

	s := &staticAnalysis{
		pkgs:   okPkgs,
		direct: make(map[string]FlagSet),
		calls:  make(map[string][]string),
	}

	for _, pkg := range okPkgs {
		info := pkg.TypesInfo
		if info == nil {
			continue
		}
		for _, file := range pkg.Syntax {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				obj := info.Defs[fn.Name]
				if obj == nil {
					continue
				}
				f, ok := obj.(*types.Func)
				if !ok {
					continue
				}
				key := symbolKey(f)
				if key == "" {
					continue
				}
				s.direct[key] = directFlagsInBody(fn.Body)
				s.calls[key] = collectCallees(info, fn.Body, f.Pkg())
			}
		}
	}

	return s, nil
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
		// <ident>.Flags.<GlobalFlags field> (e.g. deps.Flags.DryRun in commands/sync)
		if inner, ok := sel.X.(*ast.SelectorExpr); ok && inner.Sel != nil && inner.Sel.Name == "Flags" {
			if _, ok := inner.X.(*ast.Ident); ok {
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
			}
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
		if obj, ok := info.Uses[fun]; ok {
			if fn, ok := obj.(*types.Func); ok {
				if samePkg(fn, callerPkg) {
					return symbolKey(fn)
				}
			}
		}
	case *ast.SelectorExpr:
		if obj, ok := info.Uses[fun.Sel]; ok {
			if fn, ok := obj.(*types.Func); ok {
				if samePkg(fn, callerPkg) {
					return symbolKey(fn)
				}
			}
		}
	}
	return ""
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
		fset := pkg.Fset
		info := pkg.TypesInfo
		if info == nil || pkg.Types == nil {
			continue
		}
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
		if len(candidates) == 0 {
			continue
		}
		best := candidates[0]
		bestSpan := fset.Position(best.End()).Line - fset.Position(best.Pos()).Line
		for _, fl := range candidates[1:] {
			span := fset.Position(fl.End()).Line - fset.Position(fl.Pos()).Line
			if span < bestSpan {
				best = fl
				bestSpan = span
			}
		}
		return best, info, pkg.Types
	}
	return nil, nil, nil
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
