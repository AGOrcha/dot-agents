//go:build ignore

// covlines parses a Go coverage profile and prints uncovered statement ranges
// for a specific file or function. Usage: covlines <profile> <file-substring>
package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/AGOrcha/dot-agents/scripts/internal/covprofile"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: covlines <profile> <file-substring>")
		os.Exit(2)
	}
	prof := os.Args[1]
	want := os.Args[2]

	byFile, err := covprofile.Parse(prof)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	type fileBlock struct {
		file string
		b    covprofile.Block
	}
	var rows []fileBlock
	for file, blocks := range byFile {
		if !strings.Contains(file, want) {
			continue
		}
		for _, b := range blocks {
			rows = append(rows, fileBlock{file, b})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].file != rows[j].file {
			return rows[i].file < rows[j].file
		}
		if rows[i].b.StartLine != rows[j].b.StartLine {
			return rows[i].b.StartLine < rows[j].b.StartLine
		}
		return rows[i].b.StartCol < rows[j].b.StartCol
	})
	for _, r := range rows {
		if r.b.Count == 0 {
			fmt.Printf("UNCOVERED %s:%d.%d-%d.%d stmts=%d\n",
				r.file, r.b.StartLine, r.b.StartCol, r.b.EndLine, r.b.EndCol, r.b.Stmts)
		}
	}
}
