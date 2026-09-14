package main

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// parseToPkg builds a synthetic *packages.Package from inline Go source so the
// AST-walking detector can be exercised without invoking the real toolchain.
func parseToPkg(t *testing.T, pkgPath string, files map[string]string) *packages.Package {
	t.Helper()
	fset := token.NewFileSet()
	var syntax []*ast.File
	for _, name := range sortedKeys(files) {
		file, err := parser.ParseFile(fset, name, files[name], parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		syntax = append(syntax, file)
	}
	return &packages.Package{PkgPath: pkgPath, Fset: fset, Syntax: syntax}
}

// sortedKeys keeps synthetic multi-file packages in a deterministic order.
func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// srcInFunc wraps statements in one function that imports exec under the given
// import spec.
func srcInFunc(importSpec, fn string, stmts ...string) string {
	var b strings.Builder
	b.WriteString("package sample\n\nimport " + importSpec + "\n\nfunc " + fn + "() {\n")
	for _, s := range stmts {
		b.WriteString("\t" + s + "\n")
	}
	b.WriteString("}\n")
	return b.String()
}

// scan is the whole detection pipeline for one synthetic package.
func scan(t *testing.T, pkgPath string, files map[string]string) []site {
	t.Helper()
	sites, _ := scanPackage(parseToPkg(t, pkgPath, files))
	return sites
}

// only returns the single site a case is expected to produce.
func only(t *testing.T, sites []site) site {
	t.Helper()
	if len(sites) != 1 {
		t.Fatalf("expected exactly 1 site, got %d (%+v)", len(sites), sites)
	}
	return sites[0]
}

const samplePkg = modulePath + "/internal/sample"

// TestDetectsPolicedEntryPoints is the core table: exactly Command,
// CommandContext, and LookPath on an exec-bound identifier are policed, and
// nothing else in those packages is.
func TestDetectsPolicedEntryPoints(t *testing.T) {
	cases := []struct {
		name  string
		stmt  string
		sites int
		sel   string
	}{
		{"Command", `exec.Command("ls")`, 1, "exec.Command"},
		{"CommandContext", `exec.CommandContext(nil, "ls")`, 1, "exec.CommandContext"},
		{"LookPath", `exec.LookPath("ls")`, 1, "exec.LookPath"},
		{"non-spawning selector is ignored", `_ = exec.ErrNotFound`, 0, ""},
		{"unrelated package is ignored", `other.Command("ls")`, 0, ""},
		{"Cmd construction is not a spawn", `_ = exec.Cmd{}`, 0, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sites := scan(t, samplePkg, map[string]string{
				"a.go": srcInFunc(`"os/exec"`, "f", tc.stmt),
			})
			if len(sites) != tc.sites {
				t.Fatalf("sites = %d, want %d (%+v)", len(sites), tc.sites, sites)
			}
			if tc.sites == 1 && sites[0].sel != tc.sel {
				t.Errorf("sel = %q, want %q", sites[0].sel, tc.sel)
			}
		})
	}
}

// TestPolicesExecabsAndAliases proves the import-awareness: execabs is policed
// like os/exec, and an aliased import cannot hide a call.
func TestPolicesExecabsAndAliases(t *testing.T) {
	cases := []struct {
		name       string
		importSpec string
		stmt       string
		wantSel    string
	}{
		{"execabs", `"golang.org/x/sys/execabs"`, `execabs.Command("git")`, "execabs.Command"},
		{"aliased os/exec", `myexec "os/exec"`, `myexec.Command("git")`, "myexec.Command"},
		{"aliased execabs", `gx "golang.org/x/sys/execabs"`, `gx.Command("git")`, "gx.Command"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := only(t, scan(t, samplePkg, map[string]string{
				"a.go": srcInFunc(tc.importSpec, "f", tc.stmt),
			}))
			if got.sel != tc.wantSel {
				t.Errorf("sel = %q, want %q", got.sel, tc.wantSel)
			}
			if got.exe != gitExe {
				t.Errorf("exe = %q, want %q", got.exe, gitExe)
			}
		})
	}
}

// TestResolvesExecutable pins executable resolution, which is what decides
// whether a site is git.
func TestResolvesExecutable(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"literal", srcInFunc(`"os/exec"`, "f", `exec.Command("git", "status")`), gitExe},
		{"absolute path basename", srcInFunc(`"os/exec"`, "f", `exec.Command("/usr/bin/git")`), gitExe},
		{"windows exe suffix", srcInFunc(`"os/exec"`, "f", `exec.Command("C:/bin/Git.exe")`), gitExe},
		{"CommandContext skips the context arg",
			srcInFunc(`"os/exec"`, "f", `exec.CommandContext(nil, "git")`), gitExe},
		{"LookPath argument is the executable",
			srcInFunc(`"os/exec"`, "f", `exec.LookPath("git")`), gitExe},
		{"non-git literal", srcInFunc(`"os/exec"`, "f", `exec.Command("gh", "pr", "list")`), "gh"},
		{"parameter is dynamic",
			"package sample\n\nimport \"os/exec\"\n\nfunc f(bin string) {\n\texec.Command(bin)\n}\n",
			exeDynamic},
		{"const resolves",
			"package sample\n\nimport \"os/exec\"\n\nconst gitBin = \"git\"\n\nfunc f() {\n\texec.Command(gitBin)\n}\n",
			gitExe},
		{"LookPath result resolves the later spawn",
			"package sample\n\nimport \"os/exec\"\n\nfunc f() {\n\tbin, _ := exec.LookPath(\"git\")\n\texec.Command(bin, \"status\")\n}\n",
			gitExe},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sites := scan(t, samplePkg, map[string]string{"a.go": tc.src})
			last := sites[len(sites)-1]
			if last.exe != tc.want {
				t.Errorf("exe = %q, want %q (%+v)", last.exe, tc.want, sites)
			}
		})
	}
}

// TestGitCannotHideBehindAParameter is the indirect-wrapper case that the
// install.go clone/pull pair actually exhibits: the executable is resolved in
// one function and handed to another as an argument. Without propagation the
// spawning function reads as <dynamic> and could be recorded as a permanent
// boundary — which is exactly the loophole git must not have.
func TestGitCannotHideBehindAParameter(t *testing.T) {
	src := `package sample

import "os/exec"

func fetch() {
	bin, _ := exec.LookPath("git")
	clone(bin, "url")
}

func clone(gitBin, url string) {
	exec.Command(gitBin, "clone", url)
}
`
	sites := scan(t, samplePkg, map[string]string{"a.go": src})
	byFunc := map[string]site{}
	for _, s := range sites {
		byFunc[s.fn] = s
	}
	if got := byFunc["clone"].exe; got != gitExe {
		t.Errorf("clone's exec.Command exe = %q, want %q — git escaped through a parameter", got, gitExe)
	}
	if got := byFunc["fetch"].exe; got != gitExe {
		t.Errorf("fetch's exec.LookPath exe = %q, want %q", got, gitExe)
	}
}

// TestParameterPropagationResolvesOnlyGit: a helper handed several different
// binaries has no single executable, and inventing one would be a fiction. It
// stays dynamic and therefore still needs a record.
func TestParameterPropagationResolvesOnlyGit(t *testing.T) {
	src := `package sample

import "os/exec"

func probeAll() {
	probe("agent")
	probe("cursor")
}

func probe(bin string) {
	exec.LookPath(bin)
}
`
	sites := scan(t, samplePkg, map[string]string{"a.go": src})
	for _, s := range sites {
		if s.fn == "probe" && s.exe != exeDynamic {
			t.Errorf("probe exe = %q, want %q", s.exe, exeDynamic)
		}
	}
}

// TestPropagationCrossesFilesInAPackage: the caller and the spawning wrapper
// are routinely in different files of the same package.
func TestPropagationCrossesFilesInAPackage(t *testing.T) {
	sites := scan(t, samplePkg, map[string]string{
		"caller.go": "package sample\n\nimport \"os/exec\"\n\nfunc fetch() {\n\tbin, _ := exec.LookPath(\"git\")\n\tclone(bin)\n}\n",
		"spawn.go":  "package sample\n\nimport \"os/exec\"\n\nfunc clone(gitBin string) {\n\texec.Command(gitBin, \"clone\")\n}\n",
	})
	for _, s := range sites {
		if s.fn == "clone" && s.exe != gitExe {
			t.Errorf("clone exe = %q, want %q across files", s.exe, gitExe)
		}
	}
}

// TestPolicesFunctionValueReferences: taking exec.Command as a value is a
// spawn deferred to wherever the value is invoked, so the reference itself is
// policed. Without this, `var f = exec.Command; f("git", …)` would be invisible.
func TestPolicesFunctionValueReferences(t *testing.T) {
	src := `package sample

import "os/exec"

func f() {
	run := exec.Command
	run("git", "status")
}
`
	got := only(t, scan(t, samplePkg, map[string]string{"a.go": src}))
	if got.isCall {
		t.Error("a bare selector must be recorded as a reference, not a call")
	}
	if got.exe != exeDynamic {
		t.Errorf("exe = %q, want %q", got.exe, exeDynamic)
	}
	if !strings.Contains(unrecordedDetail(got), "reference (function value)") {
		t.Errorf("report should name the reference kind, got %q", unrecordedDetail(got))
	}
}

// TestCallSelectorIsNotDoubleCounted: a call's own selector is visited by the
// AST walk twice over; it must yield exactly one site.
func TestCallSelectorIsNotDoubleCounted(t *testing.T) {
	sites := scan(t, samplePkg, map[string]string{
		"a.go": srcInFunc(`"os/exec"`, "f", `exec.Command("git")`),
	})
	if len(sites) != 1 || !sites[0].isCall {
		t.Fatalf("expected one call site, got %+v", sites)
	}
}

// TestAttributesEnclosingFunction covers the three attribution shapes a record
// key can carry: a function, a method, and a func-valued var (the ghJSON seam).
func TestAttributesEnclosingFunction(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"function", srcInFunc(`"os/exec"`, "runIt", `exec.Command("git")`), "runIt"},
		{"method",
			"package sample\n\nimport \"os/exec\"\n\ntype k struct{}\n\nfunc (k) Get() {\n\texec.Command(\"security\")\n}\n",
			"k.Get"},
		{"pointer method",
			"package sample\n\nimport \"os/exec\"\n\ntype k struct{}\n\nfunc (*k) Set() {\n\texec.Command(\"security\")\n}\n",
			"k.Set"},
		{"func-valued var",
			"package sample\n\nimport \"os/exec\"\n\nvar ghJSON = func(a string) {\n\texec.Command(\"gh\", a)\n}\n",
			"ghJSON"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := only(t, scan(t, samplePkg, map[string]string{"a.go": tc.src})).fn; got != tc.want {
				t.Errorf("fn = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestRelPathUsesImportPath: the report path is derived from the package's
// import path, so it is identical in every checkout — including a worktree
// whose directory is not named "dot-agents", which is precisely where a
// filename-substring heuristic reports "internal/lifecycle/install.go" for a
// file that actually lives at "commands/internal/lifecycle/install.go".
func TestRelPathUsesImportPath(t *testing.T) {
	cases := []struct {
		pkgPath, filename, want string
	}{
		{modulePath + "/commands/internal/lifecycle", "/tmp/prfix-557/commands/internal/lifecycle/install.go",
			"commands/internal/lifecycle/install.go"},
		{modulePath, "/anywhere/main.go", "main.go"},
		{"example.com/other/pkg", "/x/y/z.go", "/x/y/z.go"},
	}
	for _, tc := range cases {
		if got := relPath(tc.pkgPath, tc.filename); got != tc.want {
			t.Errorf("relPath(%q, %q) = %q, want %q", tc.pkgPath, tc.filename, got, tc.want)
		}
	}
}

// ── reconciliation ──────────────────────────────────────────────────────────

// gitSite builds a git call site in a package for reconciliation tests.
func gitSite(pkgPath, relPath, fn string, line int) site {
	return site{pkgPath: pkgPath, relPath: relPath, line: line, fn: fn,
		sel: "exec.Command", exe: gitExe, isCall: true}
}

// scannedSet marks the given files as parsed by the current build.
func scannedSet(files ...string) map[string]bool {
	out := map[string]bool{}
	for _, f := range files {
		out[f] = true
	}
	return out
}

// TestReconcileFlagsUnrecordedSite: default deny.
func TestReconcileFlagsUnrecordedSite(t *testing.T) {
	got := reconcile([]site{gitSite(samplePkg, "internal/sample/a.go", "f", 7)},
		scannedSet("internal/sample/a.go"))
	if len(got) != 1 || !strings.Contains(got[0].detail, "unrecorded") {
		t.Fatalf("an unrecorded site must be a finding, got %+v", got)
	}
}

// TestReconcileGitInLockedPackageIsAlwaysAViolation is the cutover lock: even
// WITH a ledger record, a git spawn inside internal/graphstore fails. That is
// what keeps the go-git submodule/diff cutover from silently regressing.
func TestReconcileGitInLockedPackageIsAlwaysAViolation(t *testing.T) {
	locked := modulePath + "/internal/graphstore"
	s := gitSite(locked, "internal/graphstore/submodule.go", "EnumerateTrackedFiles", 12)

	original := ledger
	t.Cleanup(func() { ledger = original })
	ledger = []record{gitDebt(s.relPath, s.fn, 1, "read the index with go-git")}

	got := reconcile([]site{s}, scannedSet(s.relPath))
	if len(got) != 1 || !strings.Contains(got[0].detail, "cutover-locked") {
		t.Fatalf("a recorded git site in a locked package must still fail, got %+v", got)
	}
}

// TestReconcileRecordedSitePasses / count drift.
func TestReconcileSiteCount(t *testing.T) {
	a := gitSite(samplePkg, "internal/sample/a.go", "f", 7)
	b := gitSite(samplePkg, "internal/sample/a.go", "f", 9)
	original := ledger
	t.Cleanup(func() { ledger = original })
	ledger = []record{gitDebt(a.relPath, a.fn, 1, "Worktree.Status")}

	if got := reconcile([]site{a}, scannedSet(a.relPath)); len(got) != 0 {
		t.Fatalf("a recorded site must pass, got %+v", got)
	}
	got := reconcile([]site{a, b}, scannedSet(a.relPath))
	if len(got) != 1 || !strings.Contains(got[0].detail, "found 2") {
		t.Fatalf("a second site under the same record must fail, got %+v", got)
	}
}

// TestReconcileStalenessIsScopedToScannedFiles: a record whose file the
// current build did not compile is NOT stale — otherwise the windows-only
// fsops boundary would fail the darwin and linux runs of the same gate.
func TestReconcileStalenessIsScopedToScannedFiles(t *testing.T) {
	key := recordKey{File: "internal/sample/only_windows.go", Func: "WriteFile", Exe: exeDynamic}
	original := ledger
	t.Cleanup(func() { ledger = original })
	ledger = []record{{recordKey: key, Kind: KindBoundary, Sites: 1, Purpose: "powershell fallback"}}

	if got := reconcile(nil, scannedSet("internal/sample/other.go")); len(got) != 0 {
		t.Fatalf("an uncompiled file's record must not be stale, got %+v", got)
	}
	got := reconcile(nil, scannedSet(key.File))
	if len(got) != 1 || !strings.Contains(got[0].detail, "stale ledger record") {
		t.Fatalf("a scanned file with no site must report a stale record, got %+v", got)
	}
}

// TestSkipPackage: execguard itself and the named test-scaffolding packages
// are out of scope; a lookalike name is not.
func TestSkipPackage(t *testing.T) {
	cases := map[string]bool{
		modulePath + "/tools/execguard":     true,
		modulePath + "/internal/testutil":   true,
		modulePath + "/internal/testutils":  false,
		modulePath + "/internal/graphstore": false,
	}
	for pkgPath, want := range cases {
		if got := skipPackage(pkgPath); got != want {
			t.Errorf("skipPackage(%q) = %v, want %v", pkgPath, got, want)
		}
	}
}

// ── ledger invariants ───────────────────────────────────────────────────────

// TestGitIsNeverABoundary is the assignment's categorical rule, enforced on
// the ledger itself rather than trusted to review.
func TestGitIsNeverABoundary(t *testing.T) {
	err := validateRecord(boundary("internal/sample/a.go", "f", gitExe, 1, "we need git"))
	if err == nil || !strings.Contains(err.Error(), "git is never a native-unavailable boundary") {
		t.Fatalf("a git boundary record must be rejected, got %v", err)
	}
	if err := validateRecord(gitDebt("internal/sample/a.go", "f", 1, "Worktree.Pull")); err != nil {
		t.Errorf("a git debt record with a follow-up must be accepted, got %v", err)
	}
}

// TestValidateRecord covers the remaining ledger invariants.
func TestValidateRecord(t *testing.T) {
	cases := []struct {
		name string
		rec  record
		want string
	}{
		{"missing func", record{recordKey: recordKey{File: "a.go", Exe: "gh"}, Sites: 1, Purpose: "p"},
			"file, func, and exe are all required"},
		{"zero sites", boundary("a.go", "f", "gh", 0, "p"), "sites must be at least 1"},
		{"empty purpose", boundary("a.go", "f", "gh", 1, "   "), "purpose is required"},
		{"debt without follow-up",
			record{recordKey: recordKey{File: "a.go", Func: "f", Exe: "gh"}, Kind: KindDebt,
				Sites: 1, Purpose: "p"},
			"debt requires a follow-up"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateRecord(tc.rec)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

// ── resolution helpers ──────────────────────────────────────────────────────

// TestExeArgSelectsTheExecutablePosition: LookPath's only argument, Command's
// first, CommandContext's second. Getting the position wrong would resolve the
// CONTEXT as the executable and report every git spawn as <dynamic>.
func TestExeArgSelectsTheExecutablePosition(t *testing.T) {
	lit := func(s string) ast.Expr { return &ast.BasicLit{Kind: token.STRING, Value: `"` + s + `"`} }
	cases := []struct {
		name string
		fn   string
		args []ast.Expr
		want string
	}{
		{"Command takes the first arg", "Command", []ast.Expr{lit("git"), lit("status")}, "git"},
		{"CommandContext skips the context", "CommandContext", []ast.Expr{lit("ctx"), lit("git")}, "git"},
		{"LookPath takes its only arg", "LookPath", []ast.Expr{lit("git")}, "git"},
		{"no args at all", "Command", nil, ""},
		{"CommandContext with only a context", "CommandContext", []ast.Expr{lit("ctx")}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := stringLit0(exeArg(tc.fn, tc.args))
			if got != tc.want {
				t.Errorf("exeArg(%s) = %q, want %q", tc.fn, got, tc.want)
			}
		})
	}
}

// stringLit0 renders an exeArg result for assertions; a nil expr is "".
func stringLit0(expr ast.Expr) string { return staticString(expr, nil, nil) }

// TestNormalizeExe pins the executable comparison key: it is what decides
// whether a site is git, so `/usr/bin/git`, `Git.exe`, and a bare `git` must
// all collapse to the same name while an unresolvable value stays dynamic.
func TestNormalizeExe(t *testing.T) {
	cases := map[string]string{
		"git": gitExe, "/usr/bin/git": gitExe, `C:\Program Files\Git\cmd\git.exe`: gitExe,
		"GIT": gitExe, "": exeDynamic, "/": exeDynamic, ".": exeDynamic, "gh": "gh",
	}
	for raw, want := range cases {
		if got := normalizeExe(raw); got != want {
			t.Errorf("normalizeExe(%q) = %q, want %q", raw, got, want)
		}
	}
}

// TestStaticStringAndParamHelpers covers the resolution fall-throughs: a nil
// expression, a non-identifier expression, an unknown identifier, and a
// parameter name that is not in the signature.
func TestStaticStringAndParamHelpers(t *testing.T) {
	if got := staticString(nil, nil, nil); got != "" {
		t.Errorf("staticString(nil) = %q, want empty", got)
	}
	if got := staticString(&ast.CallExpr{}, nil, nil); got != "" {
		t.Errorf("staticString(call) = %q, want empty", got)
	}
	if got := staticString(&ast.Ident{Name: "unknown"}, nil, nil); got != "" {
		t.Errorf("staticString(unknown ident) = %q, want empty", got)
	}
	if got := indexOf([]string{"a", "b"}, "c"); got != -1 {
		t.Errorf("indexOf(missing) = %d, want -1", got)
	}
	if got := paramSource(&ast.CallExpr{}, exeDynamic, []string{"bin"}); got != "" {
		t.Errorf("paramSource(non-ident) = %q, want empty", got)
	}
	if got := paramSource(&ast.Ident{Name: "other"}, exeDynamic, []string{"bin"}); got != "" {
		t.Errorf("paramSource(non-parameter) = %q, want empty", got)
	}
	if got := paramSource(&ast.Ident{Name: "bin"}, gitExe, []string{"bin"}); got != "" {
		t.Errorf("paramSource of an already-resolved exe = %q, want empty", got)
	}
}

// TestParamNamesFlattensSignatures: propagation matches arguments by POSITION,
// so a grouped parameter list has to flatten the same way the call site counts
// — `func f(a, b string, c int)` is three positions, not two — and an UNNAMED
// parameter still occupies its position.
func TestParamNamesFlattensSignatures(t *testing.T) {
	t.Run("grouped names", func(t *testing.T) {
		file := parseToPkg(t, samplePkg, map[string]string{
			"a.go": "package p\nfunc f(a, b string, c int) {}\n",
		}).Syntax[0]
		fn := file.Decls[0].(*ast.FuncDecl)
		if got := strings.Join(paramNames(fn.Type), ","); got != "a,b,c" {
			t.Errorf("paramNames = %q, want a,b,c", got)
		}
	})
	t.Run("unnamed parameters keep their positions", func(t *testing.T) {
		file := parseToPkg(t, samplePkg, map[string]string{
			"a.go": "package p\n\ntype runner func(string, int)\n",
		}).Syntax[0]
		spec := file.Decls[0].(*ast.GenDecl).Specs[0].(*ast.TypeSpec)
		sig := spec.Type.(*ast.FuncType)
		got := paramNames(sig)
		if len(got) != 2 || got[0] != "" || got[1] != "" {
			t.Errorf("paramNames = %q, want two unnamed positions", got)
		}
	})
	t.Run("no parameters", func(t *testing.T) {
		file := parseToPkg(t, samplePkg, map[string]string{"a.go": "package p\nfunc f() {}\n"}).Syntax[0]
		if got := paramNames(file.Decls[0].(*ast.FuncDecl).Type); len(got) != 0 {
			t.Errorf("paramNames = %q, want none", got)
		}
	})
	if paramNames(nil) != nil {
		t.Error("a nil signature has no parameters")
	}
	if funcLitParams(&ast.Ident{}) != nil {
		t.Error("a non-literal expression has no parameters")
	}
}

// TestFuncNameAndReceiver: the ledger key includes the enclosing function, so
// every receiver shape has to render to a stable name — including the generic
// receivers a future refactor could introduce.
func TestFuncNameAndReceiver(t *testing.T) {
	if got := funcName(&ast.FuncDecl{}); got != "" {
		t.Errorf("funcName of a nameless decl = %q, want empty", got)
	}
	cases := map[string]ast.Expr{
		"k": &ast.Ident{Name: "k"},
		"g": &ast.IndexExpr{X: &ast.Ident{Name: "g"}},
		"h": &ast.IndexListExpr{X: &ast.Ident{Name: "h"}},
		"?": &ast.ArrayType{},
	}
	for want, expr := range cases {
		if got := receiverName(expr); got != want {
			t.Errorf("receiverName(%T) = %q, want %q", expr, got, want)
		}
	}
}

// TestLiteralConstsIgnoresNonStringSpecs: only string-literal consts resolve an
// executable; an iota block or a computed const must not be mistaken for one.
func TestLiteralConstsIgnoresNonStringSpecs(t *testing.T) {
	src := "package p\n\nconst (\n\ta = iota\n\tb\n)\n\nconst c = \"git\"\n\nvar d = \"gh\"\n"
	file := parseToPkg(t, samplePkg, map[string]string{"a.go": src}).Syntax[0]
	got := literalConsts(file)
	if len(got) != 1 || got["c"] != gitExe {
		t.Errorf("literalConsts = %v, want only c=git", got)
	}
}

// TestExecImportNamesIgnoresBlankAndUnrelated: a blank import cannot be called
// through, and an unrelated package must not bind the policed names.
func TestExecImportNamesIgnoresBlankAndUnrelated(t *testing.T) {
	src := "package p\n\nimport (\n\t_ \"os/exec\"\n\t\"strings\"\n)\n\nvar _ = strings.TrimSpace\n"
	file := parseToPkg(t, samplePkg, map[string]string{"a.go": src}).Syntax[0]
	got := execImportNames(file)
	if !got["exec"] {
		t.Errorf("a blank os/exec import still binds the package path: %v", got)
	}
	if got["strings"] {
		t.Errorf("an unrelated import must not be policed: %v", got)
	}
	if execImportNames(&ast.File{Imports: []*ast.ImportSpec{{}}}) == nil {
		t.Error("a malformed import spec must be skipped, not panic")
	}
}

// TestCanonicalPkgPathStripsTestVariants: packages.Load can report a package
// as `path [path.test]` or `path.test`; the ledger is keyed by the plain path.
func TestCanonicalPkgPathStripsTestVariants(t *testing.T) {
	cases := map[string]string{
		samplePkg:                               samplePkg,
		samplePkg + " [" + samplePkg + ".test]": samplePkg,
		samplePkg + ".test":                     samplePkg,
	}
	for in, want := range cases {
		if got := canonicalPkgPath(in); got != want {
			t.Errorf("canonicalPkgPath(%q) = %q, want %q", in, got, want)
		}
	}
}

// ── reporting order ─────────────────────────────────────────────────────────

// TestCheckPackagesSkipsUnusablePackagesAndSortsFindings: a nil, unnamed, or
// error-tagged package is skipped rather than half-scanned, and the findings
// are ordered by file, then line, then detail so the CI log is diffable.
func TestCheckPackagesSkipsUnusablePackagesAndSortsFindings(t *testing.T) {
	good := parseToPkg(t, samplePkg, map[string]string{
		"b.go": srcInFunc(`"os/exec"`, "second", `exec.Command("git")`),
		"a.go": srcInFunc(`"os/exec"`, "first", `exec.Command("git")`, `exec.Command("gh")`),
	})
	pkgs := []*packages.Package{
		nil,
		{PkgPath: ""},
		{PkgPath: samplePkg + "/broken", Errors: []packages.Error{{Msg: "boom"}}},
		good,
	}
	got := checkPackages(pkgs)
	var order []string
	for _, f := range got {
		order = append(order, f.relPath+":"+itoa(f.line))
	}
	want := "internal/sample/a.go:6,internal/sample/a.go:7,internal/sample/b.go:6"
	if strings.Join(order, ",") != want {
		t.Errorf("finding order = %v, want %s", order, want)
	}
}

// itoa renders a line number without pulling in strconv for one call.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	return string(d)
}

// TestUnmatchedRecordsIsSorted: stale records are reported in a stable order
// so a CI log diff is readable.
func TestUnmatchedRecordsIsSorted(t *testing.T) {
	original := ledger
	t.Cleanup(func() { ledger = original })
	ledger = []record{
		boundary("b.go", "f", "gh", 1, "p"),
		boundary("a.go", "z", "gh", 1, "p"),
		boundary("a.go", "a", "gh", 1, "p"),
	}
	got := unmatchedRecords(nil, scannedSet("a.go", "b.go"))
	var keys []string
	for _, k := range got {
		keys = append(keys, k.File+"/"+k.Func)
	}
	if strings.Join(keys, ",") != "a.go/a,a.go/z,b.go/f" {
		t.Errorf("stale order = %v", keys)
	}
}

// TestCheckPackagesOrdersTwoFindingsOnOneLine: two different spawns can share
// a file AND a line, so the report falls back to the detail text to stay
// deterministic. Without the final tiebreak the CI log order would depend on
// Go's map iteration.
func TestCheckPackagesOrdersTwoFindingsOnOneLine(t *testing.T) {
	pkg := parseToPkg(t, samplePkg, map[string]string{
		"a.go": "package sample\n\nimport \"os/exec\"\n\nfunc f() {\n" +
			"\texec.Command(\"git\"); exec.LookPath(\"gh\")\n}\n",
	})
	got := checkPackages([]*packages.Package{pkg})
	if len(got) != 2 {
		t.Fatalf("expected two findings on one line, got %+v", got)
	}
	if got[0].line != got[1].line || got[0].relPath != got[1].relPath {
		t.Fatalf("fixture drift: findings are not on the same file:line (%+v)", got)
	}
	if got[0].detail >= got[1].detail {
		t.Errorf("same-line findings must be ordered by detail, got %q then %q",
			got[0].detail, got[1].detail)
	}
}

// TestLiteralConstsSkipsMalformedSpecs: the guard walks ASTs it did not build,
// so a const declaration carrying something that is not a value spec must be
// skipped rather than panic the gate.
func TestLiteralConstsSkipsMalformedSpecs(t *testing.T) {
	file := &ast.File{Decls: []ast.Decl{&ast.GenDecl{
		Tok:   token.CONST,
		Specs: []ast.Spec{&ast.ImportSpec{}},
	}}}
	if got := literalConsts(file); len(got) != 0 {
		t.Errorf("literalConsts = %v, want nothing from a malformed spec", got)
	}
}

// TestMainDelegatesExitCode runs the compiled test binary as a child so main's
// own body executes. main is a single statement — `os.Exit(mainRun(...))` — and
// the CI gate keys off that exit code, so the delegation is worth pinning.
func TestMainDelegatesExitCode(t *testing.T) {
	if os.Getenv("EXECGUARD_MAIN_CHILD") == "1" {
		// os.Args carries the child's -test.run flag, which is not an
		// execguard flag: mainRun must reject it and exit 2.
		main()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestMainDelegatesExitCode$")
	cmd.Env = append(os.Environ(), "EXECGUARD_MAIN_CHILD=1")
	out, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("child should have exited non-zero, got err=%v out=%s", err, out)
	}
	if exitErr.ExitCode() != 2 {
		t.Errorf("child exit = %d, want 2 (mainRun's flag-error code)", exitErr.ExitCode())
	}
	if !strings.Contains(string(out), "usage: execguard") {
		t.Errorf("child should print usage on a bad flag, got %s", out)
	}
}

// TestShippedLedgerIsValid: the ledger this repository ships must satisfy its
// own invariants, including the git prohibition and duplicate-key check.
func TestShippedLedgerIsValid(t *testing.T) {
	if err := validateLedger(); err != nil {
		t.Fatalf("shipped ledger is invalid: %v", err)
	}
}

// TestShippedLedgerHasNoGitInLockedPackages: the cutover lock is also a
// property of the ledger's own contents, checkable without a scan.
func TestShippedLedgerHasNoGitInLockedPackages(t *testing.T) {
	for _, r := range ledger {
		if r.Exe != gitExe {
			continue
		}
		for pkg := range lockedPackages {
			dir := strings.TrimPrefix(pkg, modulePath+"/") + "/"
			if strings.HasPrefix(r.File, dir) {
				t.Errorf("%s records a git site inside cutover-locked %s", r.File, pkg)
			}
		}
	}
}

// TestDuplicateLedgerKeyIsRejected proves the duplicate check fires rather
// than one record silently shadowing another.
func TestDuplicateLedgerKeyIsRejected(t *testing.T) {
	original := ledger
	t.Cleanup(func() { ledger = original })
	dup := boundary("internal/sample/a.go", "f", "gh", 1, "p")
	ledger = []record{dup, dup}
	if err := validateLedger(); err == nil || !strings.Contains(err.Error(), "duplicate record") {
		t.Fatalf("expected a duplicate-record error, got %v", err)
	}
}

func TestRecordKindString(t *testing.T) {
	if KindBoundary.String() != "boundary" || KindDebt.String() != "debt" {
		t.Errorf("kind names = %q / %q", KindBoundary.String(), KindDebt.String())
	}
}

// ── mainRun exit-code seams (mirrors fsguard's structure) ───────────────────

func cleanScan(_ []string) ([]finding, error) { return nil, nil }
func failScan(_ []string) ([]finding, error)  { return nil, errors.New("synthetic load failure") }
func findScan(_ []string) ([]finding, error) {
	return []finding{
		{relPath: "internal/x/x.go", line: 5, detail: "unrecorded exec.Command call in f: git process execution"},
		{relPath: "internal/y/y.go", detail: "stale ledger record"},
	}, nil
}

func runMainCase(args []string, scan runFunc) (int, string) {
	var buf bytes.Buffer
	code := mainRun(args, &buf, scan)
	return code, buf.String()
}

func assertExitCode(t *testing.T, got, want int) {
	t.Helper()
	if got != want {
		t.Errorf("exit=%d, want %d", got, want)
	}
}

func assertStderrContains(t *testing.T, stderr, substr string) {
	t.Helper()
	if !strings.Contains(stderr, substr) {
		t.Errorf("stderr should contain %q, got %q", substr, stderr)
	}
}

func capturePatterns(t *testing.T, args []string) []string {
	t.Helper()
	var seen []string
	_, _ = runMainCase(args, func(p []string) ([]finding, error) { seen = p; return nil, nil })
	return seen
}

func TestMainRun(t *testing.T) {
	t.Run("clean exits 0", func(t *testing.T) {
		code, _ := runMainCase(nil, cleanScan)
		assertExitCode(t, code, 0)
	})
	t.Run("default pattern is ./...", func(t *testing.T) {
		got := capturePatterns(t, nil)
		if len(got) != 1 || got[0] != "./..." {
			t.Errorf("patterns = %v, want [./...]", got)
		}
	})
	t.Run("explicit patterns override", func(t *testing.T) {
		got := capturePatterns(t, []string{"./internal/..."})
		if len(got) != 1 || got[0] != "./internal/..." {
			t.Errorf("patterns = %v, want [./internal/...]", got)
		}
	})
	t.Run("load error exits 2", func(t *testing.T) {
		code, stderr := runMainCase(nil, failScan)
		assertExitCode(t, code, 2)
		assertStderrContains(t, stderr, "synthetic load failure")
	})
	t.Run("findings exit 1 and render both shapes", func(t *testing.T) {
		code, stderr := runMainCase(nil, findScan)
		assertExitCode(t, code, 1)
		assertStderrContains(t, stderr, "internal/x/x.go:5  unrecorded exec.Command call")
		assertStderrContains(t, stderr, "internal/y/y.go  stale ledger record")
		assertStderrContains(t, stderr, "Git is NOT one of those boundaries")
	})
	t.Run("bad flag exits 2", func(t *testing.T) {
		code, _ := runMainCase([]string{"-nope"}, cleanScan)
		assertExitCode(t, code, 2)
	})
	t.Run("invalid ledger exits 2 before scanning", func(t *testing.T) {
		original := ledger
		t.Cleanup(func() { ledger = original })
		ledger = []record{boundary("a.go", "f", gitExe, 1, "we need git")}
		scanned := false
		code, stderr := runMainCase(nil, func([]string) ([]finding, error) {
			scanned = true
			return nil, nil
		})
		assertExitCode(t, code, 2)
		assertStderrContains(t, stderr, "ledger is invalid")
		if scanned {
			t.Error("an invalid ledger must short-circuit before the scan")
		}
	})
}

// TestRunSurfacesPackageErrors / TestRunPropagatesLoaderError exercise run()
// through the loadPackages var, covering the error branches unreachable from
// the clean-repo scan.
func TestRunSurfacesPackageErrors(t *testing.T) {
	original := loadPackages
	t.Cleanup(func() { loadPackages = original })
	loadPackages = func(_ []string) ([]*packages.Package, error) {
		return []*packages.Package{{
			PkgPath: modulePath + "/internal/synthetic",
			Errors:  []packages.Error{{Msg: "fake load error"}},
		}}, nil
	}
	if _, err := run([]string{"./..."}); err == nil ||
		!strings.Contains(err.Error(), "package load reported errors") {
		t.Errorf("run should surface package errors, got %v", err)
	}
}

func TestRunPropagatesLoaderError(t *testing.T) {
	original := loadPackages
	t.Cleanup(func() { loadPackages = original })
	want := errors.New("loader exploded")
	loadPackages = func(_ []string) ([]*packages.Package, error) { return nil, want }
	if _, err := run([]string{"./..."}); !errors.Is(err, want) {
		t.Errorf("run should propagate loader error, got %v", err)
	}
}

// TestRepoIsClean is the live end-to-end assertion: the guard, run against the
// real repository, reports zero violations. This is what stops the commit that
// lands execguard from also tripping it.
func TestRepoIsClean(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping repo-level exec scan in -short mode")
	}
	root, ok := repoRoot()
	if !ok {
		t.Skip("repo root not detectable; skipping")
	}
	t.Chdir(root)
	findings, err := run([]string{"./..."})
	if err != nil {
		t.Fatalf("run(./...): %v", err)
	}
	if len(findings) > 0 {
		var b strings.Builder
		for _, f := range findings {
			fmt.Fprintf(&b, "  %s:%d  %s\n", f.relPath, f.line, f.detail)
		}
		t.Fatalf("expected zero exec-policy violations, got %d:\n%s", len(findings), b.String())
	}
}

// TestGraphstoreHasNoGitProcess is the PR's own cutover assertion, stated as a
// test rather than left to the CI step: after the go-git migration, scanning
// the real internal/graphstore package must find no git site at all.
func TestGraphstoreHasNoGitProcess(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping repo-level exec scan in -short mode")
	}
	root, ok := repoRoot()
	if !ok {
		t.Skip("repo root not detectable; skipping")
	}
	t.Chdir(root)
	pkgs, err := loadPackages([]string{"./internal/graphstore/..."})
	if err != nil {
		t.Fatalf("load internal/graphstore: %v", err)
	}
	for _, p := range pkgs {
		if p == nil || len(p.Errors) > 0 {
			continue
		}
		sites, _ := scanPackage(p)
		for _, s := range sites {
			if s.exe == gitExe {
				t.Errorf("%s:%d %s in %s spawns git; graphstore Git reads must stay in process",
					s.relPath, s.line, s.sel, s.fn)
			}
		}
	}
}

// repoRoot walks up from this test file's location to the module's go.mod.
func repoRoot() (string, bool) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", false
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}
