//go:build ignore

// t3 migrator (AST-based, TSV-driven). Consumes the reviewed attribution
// (funcname<TAB>dest<TAB>srcgrabbag from map_tests.py --tsv) and relocates
// each Test/Benchmark FuncDecl into its mapped destination file in the
// SAME package. Non-test top-level decls (helpers/types/vars) go to
// testutil_test.go (deduped). Declaration text is moved verbatim via
// go/printer (comments preserved). Imports are re-resolved afterward by
// goimports. Aborts on duplicate symbol whose body differs.
//
// Usage: go run migrate_ast.go <pkgdir> <map.tsv> [--dry-run]
package main

import (
	"bufio"
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

func main() {
	pkg, tsv := os.Args[1], os.Args[2]
	dry := false
	grabPat := `^(coverage_push|integration_harness)[0-9]*_test\.go$`
	pkgName := "workflow"
	helperFile := "testutil_test.go"
	for _, a := range os.Args[3:] {
		switch {
		case a == "--dry-run":
			dry = true
		case strings.HasPrefix(a, "--grab="):
			grabPat = a[len("--grab="):]
		case strings.HasPrefix(a, "--pkg="):
			pkgName = a[len("--pkg="):]
		case strings.HasPrefix(a, "--helpers="):
			helperFile = a[len("--helpers="):]
		}
	}
	grabbag := regexp.MustCompile(grabPat)
	fset := token.NewFileSet()

	dest := map[string]string{} // testfunc -> dest file
	f, err := os.Open(tsv)
	must(err)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		p := strings.Split(sc.Text(), "\t")
		if len(p) >= 2 {
			dest[p[0]] = p[1]
		}
	}
	f.Close()

	ents, _ := os.ReadDir(pkg)
	var grabFiles, otherTest []string
	for _, e := range ents {
		n := e.Name()
		if !strings.HasSuffix(n, ".go") {
			continue
		}
		if grabbag.MatchString(n) {
			grabFiles = append(grabFiles, n)
		} else if strings.HasSuffix(n, "_test.go") {
			otherTest = append(otherTest, n)
		}
	}
	sort.Strings(grabFiles)

	existing := map[string]string{}
	for _, tf := range otherTest {
		af, err := parser.ParseFile(fset, filepath.Join(pkg, tf), nil, 0)
		if err != nil {
			continue
		}
		for _, d := range af.Decls {
			for _, n := range declNames(d) {
				existing[n] = tf
			}
		}
	}

	render := func(d ast.Decl) string {
		var b bytes.Buffer
		cfg := &printer.Config{Mode: printer.UseSpaces | printer.TabIndent, Tabwidth: 8}
		_ = cfg.Fprint(&b, fset, d)
		return b.String()
	}

	moves := map[string][]string{}
	helpers := map[string]string{}
	helperOrigin := map[string]string{}
	testSeen := map[string]string{}
	var errs []string

	for _, g := range grabFiles {
		af, err := parser.ParseFile(fset, filepath.Join(pkg, g), nil, parser.ParseComments)
		if err != nil {
			errs = append(errs, fmt.Sprintf("parse %s: %v", g, err))
			continue
		}
		for _, d := range af.Decls {
			// Drop import decls — imports are re-resolved per-file by
			// goimports after the move; carrying them corrupts targets.
			if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.IMPORT {
				continue
			}
			fn, isFn := d.(*ast.FuncDecl)
			if isFn && fn.Recv == nil && (strings.HasPrefix(fn.Name.Name, "Test") || strings.HasPrefix(fn.Name.Name, "Benchmark")) {
				tn := fn.Name.Name
				if o, dup := testSeen[tn]; dup {
					errs = append(errs, fmt.Sprintf("DUP test %s: %s & %s", tn, o, g))
				}
				testSeen[tn] = g
				if o, dup := existing[tn]; dup {
					errs = append(errs, fmt.Sprintf("DUP test %s vs existing %s", tn, o))
				}
				dst, ok := dest[tn]
				if !ok {
					errs = append(errs, fmt.Sprintf("NO MAPPING for %s (%s)", tn, g))
					continue
				}
				moves[dst] = append(moves[dst], render(d))
			} else {
				names := declNames(d)
				key := strings.Join(names, ",")
				if key == "" {
					key = "anon:" + g
				}
				txt := render(d)
				if prev, ok := helpers[key]; ok {
					if strings.TrimSpace(prev) != strings.TrimSpace(txt) {
						errs = append(errs, fmt.Sprintf("HELPER COLLISION %s: %s vs %s (differs)", key, helperOrigin[key], g))
					}
					continue
				}
				if o, ok := existing[key]; ok {
					errs = append(errs, fmt.Sprintf("HELPER %s already in %s", key, o))
					continue
				}
				helpers[key] = txt
				helperOrigin[key] = g
			}
		}
	}

	if len(errs) > 0 {
		fmt.Println("ABORT — ambiguities:")
		for _, e := range errs {
			fmt.Println("  " + e)
		}
		os.Exit(2)
	}

	if dry {
		fmt.Printf("grab-bag:%d tests:%d helpers:%d\n", len(grabFiles), len(testSeen), len(helpers))
		var ds []string
		for d := range moves {
			ds = append(ds, d)
		}
		sort.Strings(ds)
		for _, d := range ds {
			fmt.Printf("  %-42s += %d\n", d, len(moves[d]))
		}
		fmt.Printf("  %-42s += %d\n", helperFile, len(helpers))
		return
	}

	appendTo := func(dst string, texts []string) {
		p := filepath.Join(pkg, dst)
		if _, err := os.Stat(p); os.IsNotExist(err) {
			must(os.WriteFile(p, []byte("package "+pkgName+"\n"), 0644))
		}
		fh, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0644)
		must(err)
		defer fh.Close()
		for _, t := range texts {
			fh.WriteString("\n" + strings.TrimRight(t, "\n") + "\n")
		}
	}
	for d, cs := range moves {
		appendTo(d, cs)
	}
	var hs []string
	for _, v := range helpers {
		hs = append(hs, v)
	}
	appendTo(helperFile, hs)
	for _, g := range grabFiles {
		must(os.Remove(filepath.Join(pkg, g)))
	}
	fmt.Printf("moved %d tests + %d helpers into %d files; deleted %d grab-bag files\n",
		len(testSeen), len(helpers), len(moves)+1, len(grabFiles))
}

func declNames(d ast.Decl) []string {
	switch t := d.(type) {
	case *ast.FuncDecl:
		if t.Recv != nil && len(t.Recv.List) > 0 {
			return []string{recvTypeName(t.Recv.List[0].Type) + "." + t.Name.Name}
		}
		return []string{t.Name.Name}
	case *ast.GenDecl:
		var out []string
		for _, s := range t.Specs {
			switch sp := s.(type) {
			case *ast.TypeSpec:
				out = append(out, sp.Name.Name)
			case *ast.ValueSpec:
				for _, n := range sp.Names {
					out = append(out, n.Name)
				}
			}
		}
		return out
	}
	return nil
}

func recvTypeName(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.StarExpr:
		return recvTypeName(x.X)
	case *ast.Ident:
		return x.Name
	case *ast.IndexExpr: // generic receiver Foo[T]
		return recvTypeName(x.X)
	}
	return "recv"
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
