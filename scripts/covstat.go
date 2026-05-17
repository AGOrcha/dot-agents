//go:build ignore

// covstat parses a Go coverage profile (mode: set) and a directory of source
// files, and prints per-function coverage similar to `go tool cover -func`.
package main

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type block struct {
	startLine, startCol int
	endLine, endCol     int
	stmts               int
	count               int
}

type fileBlocks map[string][]block

type funcRange struct {
	file      string
	name      string
	startLine int
	endLine   int
}

func parseProfile(path string) (fileBlocks, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := fileBlocks{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<22)
	first := true
	for sc.Scan() {
		line := sc.Text()
		if first {
			first = false
			continue
		}
		// path:sLine.sCol,eLine.eCol nstmts count
		colon := strings.LastIndex(line, ":")
		if colon < 0 {
			continue
		}
		file := line[:colon]
		rest := line[colon+1:]
		parts := strings.Fields(rest)
		if len(parts) != 3 {
			continue
		}
		// parts[0] = sLine.sCol,eLine.eCol
		comma := strings.Index(parts[0], ",")
		left := parts[0][:comma]
		right := parts[0][comma+1:]
		ll := strings.Split(left, ".")
		rr := strings.Split(right, ".")
		sLine, _ := strconv.Atoi(ll[0])
		sCol, _ := strconv.Atoi(ll[1])
		eLine, _ := strconv.Atoi(rr[0])
		eCol, _ := strconv.Atoi(rr[1])
		stmts, _ := strconv.Atoi(parts[1])
		count, _ := strconv.Atoi(parts[2])
		out[file] = append(out[file], block{sLine, sCol, eLine, eCol, stmts, count})
	}
	return out, sc.Err()
}

func funcsInFile(path string) ([]funcRange, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	var out []funcRange
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		start := fset.Position(fd.Pos()).Line
		end := fset.Position(fd.End()).Line
		name := fd.Name.Name
		if recv := receiverName(fd); recv != "" {
			name = "(" + recv + ")." + name
		}
		out = append(out, funcRange{file: path, name: name, startLine: start, endLine: end})
	}
	return out, nil
}

// receiverName renders the receiver type of a method declaration (e.g. "*T"
// or "T"), or "" for free functions.
func receiverName(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return ""
	}
	switch t := fd.Recv.List[0].Type.(type) {
	case *ast.StarExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return "*" + id.Name
		}
	case *ast.Ident:
		return t.Name
	}
	return ""
}

type funcCov struct {
	file, name         string
	totalStmts         int
	coveredStmts       int
	startLine, endLine int
}

func parseModulePath(root string) (string, error) {
	modBytes, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", err
	}
	for _, ln := range strings.Split(string(modBytes), "\n") {
		ln = strings.TrimSpace(ln)
		if strings.HasPrefix(ln, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(ln, "module")), nil
		}
	}
	return "", nil
}

// coverForFunc tallies covered/total statements for a single function range
// against the coverage blocks of its containing file.
func coverForFunc(rel string, fr funcRange, bs []block) funcCov {
	fc := funcCov{file: rel, name: fr.name, startLine: fr.startLine, endLine: fr.endLine}
	for _, b := range bs {
		if b.startLine >= fr.startLine && b.endLine <= fr.endLine {
			fc.totalStmts += b.stmts
			if b.count > 0 {
				fc.coveredStmts += b.stmts
			}
		}
	}
	return fc
}

func collectFuncCov(blocks fileBlocks, root, modulePath string) []funcCov {
	var all []funcCov
	for file, bs := range blocks {
		rel := strings.TrimPrefix(file, modulePath+"/")
		abs := filepath.Join(root, rel)
		funcs, err := funcsInFile(abs)
		if err != nil {
			continue
		}
		for _, fr := range funcs {
			all = append(all, coverForFunc(rel, fr, bs))
		}
	}
	sort.Slice(all, func(i, j int) bool {
		// sort by missed stmts desc
		mi := all[i].totalStmts - all[i].coveredStmts
		mj := all[j].totalStmts - all[j].coveredStmts
		if mi != mj {
			return mi > mj
		}
		return all[i].file < all[j].file
	})
	return all
}

func printFuncCov(all []funcCov) {
	var grandTotal, grandCovered int
	for _, fc := range all {
		grandTotal += fc.totalStmts
		grandCovered += fc.coveredStmts
		if fc.totalStmts == 0 {
			continue
		}
		pct := float64(fc.coveredStmts) / float64(fc.totalStmts) * 100.0
		missed := fc.totalStmts - fc.coveredStmts
		fmt.Printf("%-50s %-60s %5.1f%% missed=%d total=%d\n", fc.file, fc.name, pct, missed, fc.totalStmts)
	}
	if grandTotal > 0 {
		fmt.Printf("\nTOTAL: %d/%d = %.2f%%\n", grandCovered, grandTotal, float64(grandCovered)/float64(grandTotal)*100.0)
	}
}

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: covstat <profile> <module-root>")
		os.Exit(2)
	}
	prof := os.Args[1]
	root := os.Args[2]
	blocks, err := parseProfile(prof)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	modulePath, err := parseModulePath(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	printFuncCov(collectFuncCov(blocks, root, modulePath))
}
