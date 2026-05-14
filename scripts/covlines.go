//go:build ignore

// covlines parses a Go coverage profile and prints uncovered statement ranges
// for a specific file or function. Usage: covlines <profile> <file-substring>
package main

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

type block struct {
	sLine, sCol, eLine, eCol int
	stmts                    int
	count                    int
}

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: covlines <profile> <file-substring>")
		os.Exit(2)
	}
	prof := os.Args[1]
	want := os.Args[2]
	f, err := os.Open(prof)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<22)
	first := true
	type fileBlock struct {
		file string
		b    block
	}
	var rows []fileBlock
	for sc.Scan() {
		line := sc.Text()
		if first {
			first = false
			continue
		}
		colon := strings.LastIndex(line, ":")
		file := line[:colon]
		rest := line[colon+1:]
		if !strings.Contains(file, want) {
			continue
		}
		parts := strings.Fields(rest)
		if len(parts) != 3 {
			continue
		}
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
		rows = append(rows, fileBlock{file, block{sLine, sCol, eLine, eCol, stmts, count}})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].file != rows[j].file {
			return rows[i].file < rows[j].file
		}
		return rows[i].b.sLine < rows[j].b.sLine
	})
	for _, r := range rows {
		if r.b.count == 0 {
			fmt.Printf("UNCOVERED %s:%d.%d-%d.%d stmts=%d\n", r.file, r.b.sLine, r.b.sCol, r.b.eLine, r.b.eCol, r.b.stmts)
		}
	}
}
