// Command execguard mechanically enforces this module's process-execution
// policy: DEFAULT DENY. Every `os/exec` and `golang.org/x/sys/execabs` spawn or
// lookup site in shipping Go code must be recorded in ledger.go, with the exact
// file, enclosing function, resolved executable, and the reason that process
// boundary exists. A site with no record fails CI.
//
// The rule that motivated the guard: GIT IS CATEGORICALLY UNALLOWABLE. go-git
// is vendored and in production use here, so "we need git" is never a
// native-unavailable boundary — it is at most migration debt, and only with a
// named follow-up record. The guard refuses a git site as a boundary at all
// (ledger validation, not a finding), and refuses ANY git site inside the
// cutover-locked packages listed in lockedPackages. That is what keeps
// internal/graphstore native after the go-git cutover: a re-added
// `git ls-files` or `git diff` shell-out there cannot be allowlisted.
//
// Evasion is closed on three axes:
//
//   - IMPORT-AWARE: an aliased `import myexec "os/exec"` is resolved
//     syntactically, so renaming the import does not hide a call.
//   - INDIRECTION-AWARE: a bare selector REFERENCE (`lookPath: exec.LookPath`,
//     `f := exec.Command`) is policed like a call, because a function value can
//     be invoked anywhere. Wrapper functions are policed at their primitive, so
//     `runGit(...)` and a `gitStateExec` func var both land on the underlying
//     `exec.Command("git", ...)` site, attributed to the wrapper that owns it.
//   - EXECUTABLE-AWARE: the executable argument is resolved through string
//     literals, string constants, `LookPath("git")` results, and — across
//     package-level functions — through PARAMETERS. That is what makes
//     `gitBin, _ := exec.LookPath("git"); CloneGitSource(gitBin, …)` resolve to
//     git inside CloneGitSource, instead of hiding behind an argument.
//
// The one resolution the guard cannot do statically is a binary handed across
// a method receiver, an interface, or a package boundary; such a site is
// reported as <dynamic> and its record's Purpose must justify the boundary. The
// locked-package rule is what makes the migrated packages safe regardless.
//
// Ledger records are keyed by (file, function, executable) and pin the exact
// number of sites, so code motion inside a function is tolerated while a NEW
// site is not. A record matching nothing is reported as stale, which keeps the
// ledger from rotting as boundaries are migrated away.
//
// Scope is shipping Go only (`Tests:false`), matching tools/fsguard, plus the
// named test-scaffolding packages in testScaffoldPackages: building real git
// repositories for fixtures is not the hazard this gate targets.
//
// Usage: execguard [packages...]
// Defaults to "./..." when no package patterns are supplied. Exits 1 on any
// policy violation, 2 on a bad ledger or a load failure.
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

const modulePath = "github.com/AGOrcha/dot-agents"

// execPkgPaths are the two process-spawning packages this guard polices.
// execabs is x/sys's hardened drop-in for os/exec; both reach the same
// syscall, so both are policed identically.
var execPkgPaths = map[string]bool{
	"os/exec":                  true,
	"golang.org/x/sys/execabs": true,
}

// policed are the os/exec + execabs entry points that start or locate a
// process. Everything else in those packages (exec.ExitError, exec.Cmd field
// access) cannot itself spawn and is not policed.
var policed = map[string]bool{
	"Command":        true,
	"CommandContext": true,
	"LookPath":       true,
}

// exeDynamic is the recorded executable for a site whose binary cannot be
// resolved statically (a method receiver's field, an interface value, a
// cross-package hand-off, a config value).
const exeDynamic = "<dynamic>"

// gitExe is the normalized executable name that is categorically unallowable.
const gitExe = "git"

// site is one policed reference found in shipping code.
type site struct {
	pkgPath string
	relPath string
	line    int
	fn      string // enclosing function, method, or func-valued var
	sel     string // "exec.Command" style selector, for the report
	exe     string // resolved executable, or exeDynamic
	isCall  bool   // false for a bare selector reference (a function value)
	// param is the enclosing function's parameter the executable came from,
	// when it came from one. It is what parameter propagation resolves.
	param string
}

// key identifies the ledger record a site belongs to.
func (s site) key() recordKey {
	return recordKey{File: s.relPath, Func: s.fn, Exe: s.exe}
}

// finding is one policy violation for reporting.
type finding struct {
	relPath string
	line    int
	detail  string
}

func main() {
	os.Exit(mainRun(os.Args[1:], os.Stderr, run))
}

// runFunc is the package-loading + scanning hook mainRun calls. Threading it as
// a parameter (instead of calling run directly) is the seam tests use to drive
// every exit-code branch without invoking the real Go toolchain.
type runFunc func(patterns []string) ([]finding, error)

// mainRun is main's testable body: validate the ledger, scan, print findings,
// return the process exit code.
func mainRun(args []string, stderr io.Writer, scan runFunc) int {
	fs := flag.NewFlagSet("execguard", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintf(stderr,
			"usage: execguard [packages...]\n"+
				"  default packages: ./...\n"+
				"  exits non-zero on any os/exec or execabs Command/CommandContext/\n"+
				"  LookPath site in shipping code that is not recorded in\n"+
				"  tools/execguard/ledger.go. Git is never approvable.\n")
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	patterns := fs.Args()
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}

	if err := validateLedger(); err != nil {
		fmt.Fprintf(stderr, "execguard: ledger is invalid: %v\n", err)
		return 2
	}
	findings, err := scan(patterns)
	if err != nil {
		fmt.Fprintf(stderr, "execguard: %v\n", err)
		return 2
	}
	if len(findings) > 0 {
		reportFindings(stderr, findings)
		return 1
	}
	return 0
}

// run loads the requested packages with syntax + position info and returns
// every policy violation.
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
		// The match is purely syntactic (an exec-bound ident . policed name),
		// so no type resolution is needed. Tests:false scopes the gate to
		// SHIPPING code, matching tools/fsguard.
		Mode:  packages.NeedName | packages.NeedFiles | packages.NeedSyntax,
		Tests: false,
	}
	return packages.Load(cfg, patterns...)
}

// checkPackages collects every policed site, then reconciles them against the
// ledger. Findings are sorted for stable CI output.
//
// scanned records every file the load actually parsed. Staleness is judged
// only against those files, so a build-tagged boundary
// (internal/fsops/fsops_windows.go, internal/credstore/keyring_darwin.go)
// keeps its record on every OS instead of being reported stale on the ones
// that do not compile it.
func checkPackages(pkgs []*packages.Package) []finding {
	var sites []site
	scanned := map[string]bool{}
	for _, p := range pkgs {
		if p == nil || p.PkgPath == "" || len(p.Errors) > 0 {
			continue
		}
		if skipPackage(canonicalPkgPath(p.PkgPath)) {
			continue
		}
		pkgSites, pkgFiles := scanPackage(p)
		sites = append(sites, pkgSites...)
		for _, f := range pkgFiles {
			scanned[f] = true
		}
	}
	out := reconcile(sites, scanned)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].relPath != out[j].relPath {
			return out[i].relPath < out[j].relPath
		}
		if out[i].line != out[j].line {
			return out[i].line < out[j].line
		}
		return out[i].detail < out[j].detail
	})
	return out
}

// skipPackage reports whether a package is outside the gate's scope: execguard
// itself (its tests plant synthetic sites) and the named test-scaffolding
// packages.
func skipPackage(pkgPath string) bool {
	return pkgPath == modulePath+"/tools/execguard" || testScaffoldPackages[pkgPath]
}

// reconcile turns collected sites into findings: an unrecorded site, a site
// count that no longer matches its record, a git site inside a cutover-locked
// package, and a record that matches nothing are all violations.
func reconcile(sites []site, scanned map[string]bool) []finding {
	grouped := make(map[recordKey][]site)
	for _, s := range sites {
		grouped[s.key()] = append(grouped[s.key()], s)
	}
	index := indexLedger()
	var out []finding
	for key, group := range grouped {
		sort.Slice(group, func(i, j int) bool { return group[i].line < group[j].line })
		first := group[0]
		if locked := lockedPackage(first.pkgPath); locked != "" && first.exe == gitExe {
			out = append(out, finding{first.relPath, first.line, fmt.Sprintf(
				"%s spawns git in cutover-locked package %s (%s); read it in process with go-git",
				first.sel, first.pkgPath, locked)})
			continue
		}
		rec, ok := index[key]
		if !ok {
			out = append(out, finding{first.relPath, first.line, unrecordedDetail(first)})
			continue
		}
		if rec.Sites != len(group) {
			out = append(out, finding{first.relPath, first.line, fmt.Sprintf(
				"%s in %s: ledger records %d site(s) for %s, found %d — re-review and update the count",
				first.sel, first.fn, rec.Sites, describeExe(first.exe), len(group))})
		}
	}
	for _, key := range unmatchedRecords(grouped, scanned) {
		out = append(out, finding{key.File, 0, fmt.Sprintf(
			"stale ledger record: no %s site for %s remains in %s — delete the record",
			describeExe(key.Exe), key.Func, key.File)})
	}
	return out
}

// unrecordedDetail renders the message for a site with no ledger record,
// naming git explicitly because git can only ever be recorded as debt.
func unrecordedDetail(s site) string {
	kind := "call"
	if !s.isCall {
		kind = "reference (function value)"
	}
	if s.exe == gitExe {
		return fmt.Sprintf("unrecorded %s %s in %s: git process execution — migrate to go-git, "+
			"or record it as tracked debt with a follow-up", s.sel, kind, s.fn)
	}
	return fmt.Sprintf("unrecorded %s %s in %s (%s): add an exact ledger record "+
		"naming the native-unavailable boundary", s.sel, kind, s.fn, describeExe(s.exe))
}

// describeExe renders an executable for the report.
func describeExe(exe string) string {
	if exe == exeDynamic {
		return "a dynamically resolved executable"
	}
	return exe
}

// scanPackage walks one package and returns every policed site, with
// executables resolved as far as package-local information allows, plus the
// module-relative paths of the files it actually parsed.
func scanPackage(p *packages.Package) ([]site, []string) {
	var sites []site
	params := map[string][]string{}
	var callArgs []resolvedCall
	pkgPath := canonicalPkgPath(p.PkgPath)
	files := make([]string, 0, len(p.Syntax))
	for _, file := range p.Syntax {
		files = append(files, relPath(pkgPath, p.Fset.Position(file.Pos()).Filename))
		names := execImportNames(file)
		collectFuncParams(file, params)
		if len(names) == 0 {
			// A file with no exec import still supplies call arguments that
			// resolve another file's parameters.
			callArgs = append(callArgs, resolveCalls(file, nil)...)
			continue
		}
		sites = append(sites, scanFile(p, file, names)...)
		callArgs = append(callArgs, resolveCalls(file, names)...)
	}
	return propagateParams(sites, params, callArgs), files
}

// scanFile walks one file's declarations, attributing each policed site to the
// function, method, or func-valued var that encloses it.
func scanFile(p *packages.Package, file *ast.File, names map[string]bool) []site {
	consts := literalConsts(file)
	var out []site
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			out = append(out, collectSites(p, d, names, funcName(d),
				consts, lookPathVars(d, names), paramNames(d.Type))...)
		case *ast.GenDecl:
			// `var ghJSON = func(...) {...}` is a function for attribution
			// purposes: it is the seam that owns the spawn.
			for _, spec := range d.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, nameIdent := range vs.Names {
					if i >= len(vs.Values) {
						continue
					}
					out = append(out, collectSites(p, vs.Values[i], names,
						nameIdent.Name, consts, nil, funcLitParams(vs.Values[i]))...)
				}
			}
		}
	}
	return out
}

// collectSites walks one declaration and returns every policed selector in it.
//
// A selector that IS a call's callee becomes a call site with its executable
// resolved from the call's arguments; every other policed selector is a bare
// function-value reference, which is policed too and is always <dynamic>
// because the executable is chosen wherever that value is eventually invoked.
func collectSites(p *packages.Package, node ast.Node, names map[string]bool,
	fn string, consts, gitVars map[string]string, params []string) []site {
	callees := map[*ast.SelectorExpr]*ast.CallExpr{}
	var selectors []*ast.SelectorExpr
	ast.Inspect(node, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if sel, name := policedSelector(call.Fun, names); name != "" {
				callees[sel] = call
			}
			return true
		}
		if sel, name := policedSelector(n, names); name != "" {
			selectors = append(selectors, sel)
		}
		return true
	})
	out := make([]site, 0, len(selectors))
	for _, sel := range selectors {
		name := sel.Sel.Name
		call, isCall := callees[sel]
		if !isCall {
			out = append(out, newSite(p, sel, name, fn, false, exeDynamic, ""))
			continue
		}
		arg := exeArg(name, call.Args)
		exe := normalizeExe(staticString(arg, consts, gitVars))
		out = append(out, newSite(p, sel, name, fn, true, exe, paramSource(arg, exe, params)))
	}
	return out
}

// paramSource returns the parameter name an unresolved executable came from, so
// propagateParams can finish the resolution. It is "" once the executable is
// already known, or when the argument is not a plain parameter reference.
func paramSource(arg ast.Expr, exe string, params []string) string {
	if exe != exeDynamic || arg == nil {
		return ""
	}
	ident, ok := arg.(*ast.Ident)
	if !ok {
		return ""
	}
	for _, p := range params {
		if p == ident.Name {
			return ident.Name
		}
	}
	return ""
}

// resolvedCall is one intra-package call to a named function, with each
// argument resolved to a compile-time-known executable name where possible.
type resolvedCall struct {
	callee string
	args   []string
}

// resolveCalls returns every call to a plain package-level function in a file,
// with the arguments resolved through literals, consts, and LookPath results.
// A method or interface call is skipped: its callee cannot be named here.
func resolveCalls(file *ast.File, names map[string]bool) []resolvedCall {
	consts := literalConsts(file)
	var out []resolvedCall
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		gitVars := lookPathVars(fn, names)
		ast.Inspect(fn, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			callee, ok := call.Fun.(*ast.Ident)
			if !ok {
				return true
			}
			args := make([]string, len(call.Args))
			for i, arg := range call.Args {
				args[i] = normalizeExe(staticString(arg, consts, gitVars))
			}
			out = append(out, resolvedCall{callee: callee.Name, args: args})
			return true
		})
	}
	return out
}

// propagateParams finishes executable resolution for sites whose binary came
// in as a parameter: when an intra-package caller passes GIT at that position,
// the site is git, not <dynamic>.
//
// This is what stops a git spawn from hiding behind an argument —
// `CloneGitSource(gitBin, …)` fed by `exec.LookPath("git")` resolves the
// `exec.Command(gitBin, …)` inside CloneGitSource to git.
//
// Propagation resolves git and nothing else ON PURPOSE. A helper that is
// handed several different binaries (a CLI version probe, a task runner) has
// no single executable to report, and labelling it with whichever name sorted
// first would be a fiction; those sites stay <dynamic> and still need a record
// whose Purpose explains the boundary.
func propagateParams(sites []site, params map[string][]string, calls []resolvedCall) []site {
	if len(sites) == 0 {
		return sites
	}
	// callee -> argument positions observed carrying git.
	gitAt := map[string]map[int]bool{}
	for _, c := range calls {
		for i, a := range c.args {
			if a != gitExe {
				continue
			}
			if gitAt[c.callee] == nil {
				gitAt[c.callee] = map[int]bool{}
			}
			gitAt[c.callee][i] = true
		}
	}
	for i := range sites {
		s := &sites[i]
		if s.param == "" {
			continue
		}
		idx := indexOf(params[s.fn], s.param)
		if idx >= 0 && gitAt[s.fn][idx] {
			s.exe = gitExe
		}
	}
	return sites
}

// indexOf returns the position of name in names, or -1.
func indexOf(names []string, name string) int {
	for i, n := range names {
		if n == name {
			return i
		}
	}
	return -1
}

// collectFuncParams records each package-level function's flattened parameter
// names, which is the positional vocabulary propagateParams matches against.
// Methods are keyed by their "Receiver.Name" form and are never resolved
// positionally (resolveCalls cannot name them), but recording them keeps the
// lookup total.
func collectFuncParams(file *ast.File, into map[string][]string) {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		into[funcName(fn)] = paramNames(fn.Type)
	}
}

// paramNames flattens a signature's parameter names in declaration order, so
// `func f(a, b string, c int)` yields [a b c].
func paramNames(sig *ast.FuncType) []string {
	if sig == nil || sig.Params == nil {
		return nil
	}
	var out []string
	for _, field := range sig.Params.List {
		if len(field.Names) == 0 {
			out = append(out, "")
			continue
		}
		for _, name := range field.Names {
			out = append(out, name.Name)
		}
	}
	return out
}

// funcLitParams returns the parameter names of a func literal expression.
func funcLitParams(expr ast.Expr) []string {
	lit, ok := expr.(*ast.FuncLit)
	if !ok {
		return nil
	}
	return paramNames(lit.Type)
}

// newSite builds a site from a resolved selector.
func newSite(p *packages.Package, sel *ast.SelectorExpr, name, fn string,
	isCall bool, exe, param string) site {
	pos := p.Fset.Position(sel.Pos())
	local := "exec"
	if ident, ok := sel.X.(*ast.Ident); ok {
		local = ident.Name
	}
	pkgPath := canonicalPkgPath(p.PkgPath)
	return site{
		pkgPath: pkgPath,
		relPath: relPath(pkgPath, pos.Filename),
		line:    pos.Line,
		fn:      fn,
		sel:     local + "." + name,
		exe:     exe,
		isCall:  isCall,
		param:   param,
	}
}

// policedSelector reports whether n is a selector on an exec-bound identifier
// naming a policed function, returning the selector and the bare name.
func policedSelector(n ast.Node, names map[string]bool) (*ast.SelectorExpr, string) {
	sel, ok := n.(*ast.SelectorExpr)
	if !ok {
		return nil, ""
	}
	ident, ok := sel.X.(*ast.Ident)
	if !ok || !names[ident.Name] {
		return nil, ""
	}
	if !policed[sel.Sel.Name] {
		return nil, ""
	}
	return sel, sel.Sel.Name
}

// exeArg returns the argument that names the executable: LookPath's only
// argument, Command's first, CommandContext's second (the first is the
// context).
func exeArg(name string, args []ast.Expr) ast.Expr {
	idx := 0
	if name == "CommandContext" {
		idx = 1
	}
	if idx >= len(args) {
		return nil
	}
	return args[idx]
}

// staticString resolves an expression to a compile-time-known string: a string
// literal, an identifier bound to a literal const, or a variable assigned from
// a LookPath of a literal.
func staticString(expr ast.Expr, consts, gitVars map[string]string) string {
	if expr == nil {
		return ""
	}
	if lit := stringLit(expr); lit != "" {
		return lit
	}
	ident, ok := expr.(*ast.Ident)
	if !ok {
		return ""
	}
	if v, ok := consts[ident.Name]; ok {
		return v
	}
	return gitVars[ident.Name]
}

// stringLit returns the value of a plain string literal, or "".
func stringLit(expr ast.Expr) string {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind.String() != "STRING" {
		return ""
	}
	return strings.Trim(lit.Value, "`\"")
}

// normalizeExe reduces a resolved executable to a comparable name: basename,
// no ".exe" suffix, lowercased. An unresolved value becomes <dynamic>.
//
// BOTH separators are normalized, not just the host's. The gate runs on three
// OSes and has to reach the same verdict on each, so a Windows-style literal
// (`C:\Program Files\Git\cmd\git.exe`) must resolve to git when the guard runs
// on Linux too — filepath.ToSlash alone is a no-op there.
func normalizeExe(raw string) string {
	base := path.Base(strings.ReplaceAll(raw, `\`, "/"))
	base = strings.TrimSuffix(strings.ToLower(base), ".exe")
	switch base {
	case "", ".", "/":
		return exeDynamic
	}
	return base
}

// literalConsts returns the file's const identifiers bound to string literals,
// so `exec.Command(gitBinName, ...)` resolves to its executable.
func literalConsts(file *ast.File) map[string]string {
	out := map[string]string{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok.String() != "const" {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, nameIdent := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				if lit := stringLit(vs.Values[i]); lit != "" {
					out[nameIdent.Name] = lit
				}
			}
		}
	}
	return out
}

// lookPathVars returns the function's local variables assigned from a LookPath
// of a string literal. This is what makes the indirect git spawn
// `gitBin, _ := exec.LookPath("git"); exec.Command(gitBin, ...)` resolve to git
// rather than to <dynamic>.
func lookPathVars(fn *ast.FuncDecl, names map[string]bool) map[string]string {
	out := map[string]string{}
	if len(names) == 0 {
		return out
	}
	ast.Inspect(fn, func(n ast.Node) bool {
		lhs, rhs := assignParts(n)
		if len(lhs) == 0 || len(rhs) != 1 {
			return true
		}
		call, ok := rhs[0].(*ast.CallExpr)
		if !ok {
			return true
		}
		_, name := policedSelector(call.Fun, names)
		if name != "LookPath" || len(call.Args) == 0 {
			return true
		}
		lit := stringLit(call.Args[0])
		if lit == "" {
			return true
		}
		if ident, ok := lhs[0].(*ast.Ident); ok {
			out[ident.Name] = lit
		}
		return true
	})
	return out
}

// assignParts returns the left- and right-hand sides of an assignment or a var
// declaration, or nils for anything else.
func assignParts(n ast.Node) ([]ast.Expr, []ast.Expr) {
	switch stmt := n.(type) {
	case *ast.AssignStmt:
		return stmt.Lhs, stmt.Rhs
	case *ast.ValueSpec:
		lhs := make([]ast.Expr, 0, len(stmt.Names))
		for _, name := range stmt.Names {
			lhs = append(lhs, name)
		}
		return lhs, stmt.Values
	}
	return nil, nil
}

// funcName renders a function or method name as it appears in the ledger:
// "Name" for a function, "Receiver.Name" for a method.
func funcName(fn *ast.FuncDecl) string {
	if fn.Name == nil {
		return ""
	}
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	return receiverName(fn.Recv.List[0].Type) + "." + fn.Name.Name
}

// receiverName renders a method receiver's base type name.
func receiverName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return receiverName(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.IndexExpr: // generic receiver
		return receiverName(t.X)
	case *ast.IndexListExpr:
		return receiverName(t.X)
	}
	return "?"
}

// execImportNames returns the local identifiers bound to os/exec or execabs in
// one file. Almost always {"exec", "execabs"}, but an alias is honored so the
// guard cannot be evaded by renaming the import.
func execImportNames(file *ast.File) map[string]bool {
	names := map[string]bool{}
	for _, imp := range file.Imports {
		if imp.Path == nil {
			continue
		}
		importPath := strings.Trim(imp.Path.Value, `"`)
		if !execPkgPaths[importPath] {
			continue
		}
		if imp.Name != nil && imp.Name.Name != "" && imp.Name.Name != "_" {
			names[imp.Name.Name] = true
			continue
		}
		names[path.Base(importPath)] = true
	}
	return names
}

// canonicalPkgPath strips the "[...]"/".test" suffixes the test variants of a
// package carry so a ledger keyed by the plain import path matches both loads.
func canonicalPkgPath(pkgPath string) string {
	if i := strings.IndexByte(pkgPath, ' '); i >= 0 {
		pkgPath = pkgPath[:i]
	}
	return strings.TrimSuffix(pkgPath, ".test")
}

// relPath renders a module-relative slash path (e.g.
// "commands/internal/lifecycle/install.go") from the package's import path and
// the file's base name. Deriving the directory from the IMPORT PATH rather
// than from the absolute filename is what makes the report identical in every
// checkout — including a worktree whose directory is not named "dot-agents",
// and a path that happens to contain "/internal/" more than once.
func relPath(pkgPath, filename string) string {
	base := filepath.Base(filename)
	switch {
	case pkgPath == modulePath:
		return base
	case strings.HasPrefix(pkgPath, modulePath+"/"):
		return strings.TrimPrefix(pkgPath, modulePath+"/") + "/" + base
	}
	return filepath.ToSlash(filepath.Clean(filename))
}

// reportFindings renders the failure list: header with count, one indented
// line per violation, then the policy in the terms an author has to act on.
func reportFindings(w io.Writer, fs []finding) {
	fmt.Fprintf(w, "execguard: %d process-execution policy violation(s):\n", len(fs))
	for _, f := range fs {
		if f.line > 0 {
			fmt.Fprintf(w, "  %s:%d  %s\n", f.relPath, f.line, f.detail)
			continue
		}
		fmt.Fprintf(w, "  %s  %s\n", f.relPath, f.detail)
	}
	fmt.Fprintf(w, "\nProcess execution is DEFAULT DENY in shipping code: every\n"+
		"os/exec + execabs Command/CommandContext/LookPath site needs an exact\n"+
		"record in tools/execguard/ledger.go naming the file, function,\n"+
		"executable, and why no in-process API exists.\n\n"+
		"Git is NOT one of those boundaries. go-git v6 is vendored and in\n"+
		"production use (internal/gitwt, internal/gitremote, internal/graphstore):\n"+
		"read the index, resolve revisions, and diff trees in process. A git site\n"+
		"can only be recorded as KindDebt with a named follow-up, and never at\n"+
		"all inside the cutover-locked packages.\n")
}
