// Package covprofile parses Go coverage profiles. It is shared by the
// dev-only scripts/cov*.go helpers so the profile-line parser has a single
// implementation instead of being copied per script.
package covprofile

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// Block is one coverage-profile block: a statement range and its hit count.
type Block struct {
	StartLine, StartCol int
	EndLine, EndCol     int
	Stmts               int
	Count               int
}

// Parse reads a Go coverage profile (the leading `mode:` header line is
// skipped) and returns blocks grouped by source file path. Malformed lines
// are skipped, matching `go tool cover` leniency.
func Parse(path string) (map[string][]Block, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	out := map[string][]Block{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<22)
	first := true
	for sc.Scan() {
		line := sc.Text()
		if first {
			first = false
			continue
		}
		if file, b, ok := parseLine(line); ok {
			out[file] = append(out[file], b)
		}
	}
	return out, sc.Err()
}

// parseLine parses one body line "path:sLine.sCol,eLine.eCol nstmts count".
func parseLine(line string) (string, Block, bool) {
	colon := strings.LastIndex(line, ":")
	if colon < 0 {
		return "", Block{}, false
	}
	file := line[:colon]
	parts := strings.Fields(line[colon+1:])
	if len(parts) != 3 {
		return "", Block{}, false
	}
	comma := strings.Index(parts[0], ",")
	if comma < 0 {
		return "", Block{}, false
	}
	ll := strings.Split(parts[0][:comma], ".")
	rr := strings.Split(parts[0][comma+1:], ".")
	if len(ll) < 2 || len(rr) < 2 {
		return "", Block{}, false
	}
	sLine, _ := strconv.Atoi(ll[0])
	sCol, _ := strconv.Atoi(ll[1])
	eLine, _ := strconv.Atoi(rr[0])
	eCol, _ := strconv.Atoi(rr[1])
	stmts, _ := strconv.Atoi(parts[1])
	count, _ := strconv.Atoi(parts[2])
	return file, Block{sLine, sCol, eLine, eCol, stmts, count}, true
}
