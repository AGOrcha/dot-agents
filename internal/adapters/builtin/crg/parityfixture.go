package crg

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// LoadCorpus reads a pinned parity corpus from a testdata JSON file
// (testdata/crg-parity/corpus/<commit>.json). The file is the normalized
// Tree-sitter ingestion output for one commit — the same fixture both adapters
// ingest so their parity surfaces are directly comparable.
func LoadCorpus(path string) (Corpus, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Corpus{}, fmt.Errorf("crg: read corpus %s: %w", path, err)
	}
	var c Corpus
	if err := json.Unmarshal(data, &c); err != nil {
		return Corpus{}, fmt.Errorf("crg: parse corpus %s: %w", path, err)
	}
	return c, nil
}

// PinnedCommits reads the ordered list of pinned commit ids from
// testdata/crg-parity/commits.txt (one id per line, blank lines ignored). The
// §11.6 corpus runs both adapters against each of these.
func PinnedCommits(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("crg: read commits %s: %w", path, err)
	}
	var commits []string
	for _, line := range splitLines(string(data)) {
		if line == "" || line[0] == '#' {
			continue // blank lines and # comments are ignored
		}
		commits = append(commits, line)
	}
	return commits, nil
}

// splitLines splits on \n and trims trailing \r and surrounding spaces.
func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == '\n' {
			line := s[start:i]
			line = trimSpace(line)
			out = append(out, line)
			start = i + 1
		}
	}
	return out
}

// trimSpace trims ASCII spaces, tabs, and carriage returns from both ends.
func trimSpace(s string) string {
	isSpace := func(b byte) bool { return b == ' ' || b == '\t' || b == '\r' }
	i, j := 0, len(s)
	for i < j && isSpace(s[i]) {
		i++
	}
	for j > i && isSpace(s[j-1]) {
		j--
	}
	return s[i:j]
}

// CorpusDir resolves the testdata/crg-parity dir relative to a base (the repo
// root); callers pass the path their test computes. SortedCorpusFiles lists the
// per-commit corpus JSON files in commit order.
func SortedCorpusFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("crg: read corpus dir %s: %w", dir, err)
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(files)
	return files, nil
}
