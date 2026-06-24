// Command fsguard mechanically enforces the leverage-cross-platform-fs-helpers
// lesson: filesystem MUTATIONS in this module must route through
// internal/fsops (MkdirAll / Mkdir / WriteFile / Remove / RemoveAll / Rename),
// which carry the Windows PowerShell fallbacks and security hardening that raw
// os.* calls do not. A raw os.* fs-mutator outside internal/fsops is exactly
// the class of bug that produced #148: agentslock acquired its sidecar lock
// with os.Mkdir against a not-yet-created parent, which fails on Windows with
// ERROR_FILE_NOT_FOUND. The compiler cannot catch that; this AST lint does.
//
// Scope: only the six WRITE primitives are policed — os.Mkdir, os.MkdirAll,
// os.Remove, os.RemoveAll, os.Rename, os.WriteFile. Reads (os.Open, os.ReadFile,
// os.Stat) are already portable and are not flagged.
//
// Ratchet model: the module has pre-existing raw-mutator call sites that predate
// this guard. Migrating all of them at once is out of scope, so each is
// grandfathered through the allowlist (allowlist.go) with a documented reason.
// What the guard buys immediately is a one-way ratchet: a NEW raw os.* mutator
// in a package that is not already grandfathered fails CI, forcing the author to
// either route through fsops or add a deliberate, reasoned allowlist entry. As
// packages migrate to fsops, their grandfather entries are deleted, tightening
// the gate.
//
// Usage: fsguard [packages...]
// Defaults to "./..." when no package patterns are supplied. Exits non-zero (and
// prints every offending call site) the moment a non-allowlisted raw mutator is
// found, which is what the CI job in .github/workflows/test.yml keys off.
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

const modulePath = "github.com/AGOrcha/dot-agents"

// fsopsPkg is the one package allowed to call the raw os.* mutators: it IS the
// abstraction the rest of the module routes through.
const fsopsPkg = modulePath + "/internal/fsops"

// mutators is the set of os.* filesystem WRITE primitives the guard forbids
// outside fsops. Reads are portable and intentionally absent.
var mutators = map[string]bool{
	"Mkdir":     true,
	"MkdirAll":  true,
	"Remove":    true,
	"RemoveAll": true,
	"Rename":    true,
	"WriteFile": true,
}

// finding is one offending call site for reporting. relPath is module-relative
// so the CI log points straight at the source regardless of the checkout path.
type finding struct {
	pkgPath string
	relPath string
	line    int
	call    string // e.g. "os.Mkdir"
}

func main() {
	os.Exit(mainRun(os.Args[1:], os.Stderr, run))
}

// runFunc is the package-loading + scanning hook mainRun calls. Threading it as
// a parameter (instead of calling run directly) is the seam tests use to drive
// every exit-code branch without invoking the real Go toolchain.
type runFunc func(patterns []string) ([]finding, error)

// mainRun is main's testable body: parse args, scan, print findings, return the
// process exit code. Pure (no os.Exit, no global mutation beyond its FlagSet) so
// per-branch tests can assert exit codes and stderr directly.
func mainRun(args []string, stderr io.Writer, scan runFunc) int {
	fs := flag.NewFlagSet("fsguard", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintf(stderr,
			"usage: fsguard [packages...]\n"+
				"  default packages: ./...\n"+
				"  exits non-zero on any raw os.{Mkdir,MkdirAll,Remove,RemoveAll,\n"+
				"  Rename,WriteFile} call outside internal/fsops that is not\n"+
				"  grandfathered in tools/fsguard/allowlist.go.\n")
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	patterns := fs.Args()
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}

	findings, err := scan(patterns)
	if err != nil {
		fmt.Fprintf(stderr, "fsguard: %v\n", err)
		return 2
	}
	if len(findings) > 0 {
		reportFindings(stderr, findings)
		return 1
	}
	return 0
}

// run loads the requested packages with syntax + position info and returns every
// non-allowlisted raw mutator call. The load step is a package-level var so tests
// can inject synthetic ASTs without the real toolchain.
func run(patterns []string) ([]finding, error) {
	pkgs, err := loadPackages(patterns)
	if err != nil {
		return nil, err
	}
	if packages.PrintErrors(pkgs) > 0 {
		return nil, fmt.Errorf("package load reported errors (see above)")
	}
	return checkPackages(pkgs), nil
}

// loadPackages is a var so tests can swap in a fake loader. Production callers
// get packages.Load with the syntax + file-position signal the AST walk needs.
var loadPackages = func(patterns []string) ([]*packages.Package, error) {
	cfg := &packages.Config{
		// NeedSyntax is required to walk the AST; NeedName/NeedFiles make package
		// identity and file paths available. NeedTypes is intentionally omitted —
		// the guard's match is purely syntactic (an os-bound ident . mutator
		// call), so it needs no type resolution and stays fast.
		//
		// Tests:false scopes the gate to PRODUCTION code, matching importguard's
		// build-graph-not-test-graph choice. The #148 bug class is a raw fs
		// mutation on a production path (agentslock acquiring its lock); test
		// fixtures legitimately lean on t.TempDir()+os.WriteFile and are not the
		// portability hazard this gate targets. A package's _test.go files share
		// the package's identity, so an out-of-package _test.go is loaded as its
		// own package carrying no production identity — excluding the test graph
		// keeps the surface to real shipping code.
		Mode:  packages.NeedName | packages.NeedFiles | packages.NeedSyntax,
		Tests: false,
	}
	return packages.Load(cfg, patterns...)
}

// checkPackages walks every loaded package's syntax and accumulates each
// non-allowlisted raw mutator call. fsops itself is skipped (it is the
// abstraction). Findings are sorted for stable CI output.
func checkPackages(pkgs []*packages.Package) []finding {
	var out []finding
	for _, p := range pkgs {
		if p == nil || p.PkgPath == "" || len(p.Errors) > 0 {
			continue
		}
		if isFsopsPkg(p.PkgPath) {
			continue // fsops is the sanctioned home of the raw calls
		}
		out = append(out, scanPackage(p)...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].relPath != out[j].relPath {
			return out[i].relPath < out[j].relPath
		}
		return out[i].line < out[j].line
	})
	return out
}

// isFsopsPkg reports whether pkgPath is internal/fsops (the package under test
// load may carry a ".test" / " [pkg.test]" suffix, which we tolerate).
func isFsopsPkg(pkgPath string) bool {
	return pkgPath == fsopsPkg ||
		strings.HasPrefix(pkgPath, fsopsPkg+" ") ||
		strings.HasPrefix(pkgPath, fsopsPkg+".")
}

// scanPackage walks one package's files for os.<mutator> calls and returns each
// that is not allowlisted.
func scanPackage(p *packages.Package) []finding {
	var out []finding
	osNames := osImportNames(p)
	for _, file := range p.Syntax {
		ast.Inspect(file, func(n ast.Node) bool {
			call, mut := mutatorCall(n, osNames)
			if mut == "" {
				return true
			}
			pos := p.Fset.Position(call.Pos())
			rel := relPath(pos.Filename)
			if allowed(canonicalPkgPath(p.PkgPath), rel, pos.Line) {
				return true
			}
			out = append(out, finding{
				pkgPath: canonicalPkgPath(p.PkgPath),
				relPath: rel,
				line:    pos.Line,
				call:    "os." + mut,
			})
			return true
		})
	}
	return out
}

// osImportNames returns the set of local identifiers bound to the standard "os"
// package within a file set. Almost always {"os"}, but an aliased import
// (`import myos "os"`) is honored so the guard cannot be evaded by renaming.
func osImportNames(p *packages.Package) map[string]bool {
	names := map[string]bool{}
	for _, file := range p.Syntax {
		for _, imp := range file.Imports {
			if imp.Path == nil || imp.Path.Value != `"os"` {
				continue
			}
			if imp.Name != nil && imp.Name.Name != "" && imp.Name.Name != "_" {
				names[imp.Name.Name] = true
			} else {
				names["os"] = true
			}
		}
	}
	return names
}

// mutatorCall reports whether n is a call to os.<mutator> (for an os-bound
// identifier in osNames) and, if so, returns the call expr and the bare mutator
// name. A non-match returns ("", "").
func mutatorCall(n ast.Node, osNames map[string]bool) (*ast.CallExpr, string) {
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return nil, ""
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil, ""
	}
	ident, ok := sel.X.(*ast.Ident)
	if !ok || !osNames[ident.Name] {
		return nil, ""
	}
	if !mutators[sel.Sel.Name] {
		return nil, ""
	}
	return call, sel.Sel.Name
}

// canonicalPkgPath strips the "[...]"/".test" suffixes the test variants of a
// package carry under packages.Load(Tests:true) so an allowlist entry keyed by
// the plain import path matches both the production and test load of a package.
func canonicalPkgPath(pkgPath string) string {
	if i := strings.IndexByte(pkgPath, ' '); i >= 0 {
		pkgPath = pkgPath[:i]
	}
	return strings.TrimSuffix(pkgPath, ".test")
}

// relPath converts an absolute source filename to a module-relative slash path
// (e.g. "internal/agentslock/lockfile.go"). When the module root can't be found
// in the path it returns the cleaned absolute path so the log is still usable.
func relPath(filename string) string {
	clean := filepath.ToSlash(filepath.Clean(filename))
	const marker = "/dot-agents/"
	if i := strings.LastIndex(clean, marker); i >= 0 {
		return clean[i+len(marker):]
	}
	// Fallback: trim the longest leading segment up to a recognizable repo dir.
	for _, root := range []string{"/internal/", "/commands/", "/cmd/", "/tools/"} {
		if i := strings.Index(clean, root); i >= 0 {
			return clean[i+1:]
		}
	}
	return clean
}

// reportFindings renders the failure list: header with count, one indented line
// per call site, then guidance pointing at fsops and the lesson.
func reportFindings(w io.Writer, fs []finding) {
	fmt.Fprintf(w, "fsguard: %d raw os.* fs-mutator call(s) outside internal/fsops:\n", len(fs))
	for _, f := range fs {
		fmt.Fprintf(w, "  %s:%d  %s\n", f.relPath, f.line, f.call)
	}
	fmt.Fprintf(w, "\nFilesystem mutations must route through internal/fsops so the\n"+
		"Windows PowerShell fallbacks and security hardening apply uniformly\n"+
		"(this is the leverage-cross-platform-fs-helpers lesson; raw os.Mkdir\n"+
		"against a missing parent is what caused the #148 Windows bug).\n\n"+
		"Fix options:\n"+
		"  1. Replace the call with the fsops equivalent (fsops.MkdirAll,\n"+
		"     fsops.Mkdir, fsops.WriteFile, fsops.Remove, fsops.RemoveAll,\n"+
		"     fsops.Rename) — preferred.\n"+
		"  2. If the call is a deliberate, fsops-less primitive (e.g. an atomic\n"+
		"     mkdir-as-lock), add a reasoned entry to tools/fsguard/allowlist.go.\n")
}
