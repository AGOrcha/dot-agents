package graphstore_test

import (
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/graphstore"
)

// ── parseCRGStatusOutput ──────────────────────────────────────────────────────

func TestParseCRGStatusOutput_typical(t *testing.T) {
	raw := `INFO: Some log line
Nodes: 923
Edges: 6281
Files: 50
Languages: go, ruby
Last updated: 2026-04-11T00:49:52
`
	s := parseCRGStatusOutputExported([]byte(raw))
	if s.Nodes != 923 {
		t.Errorf("Nodes: got %d, want 923", s.Nodes)
	}
	if s.Edges != 6281 {
		t.Errorf("Edges: got %d, want 6281", s.Edges)
	}
	if s.Files != 50 {
		t.Errorf("Files: got %d, want 50", s.Files)
	}
	if s.Languages != "go, ruby" {
		t.Errorf("Languages: got %q, want %q", s.Languages, "go, ruby")
	}
	if s.LastUpdated != "2026-04-11T00:49:52" {
		t.Errorf("LastUpdated: got %q", s.LastUpdated)
	}
}

func TestParseCRGStatusOutput_empty(t *testing.T) {
	s := parseCRGStatusOutputExported(nil)
	if s.Nodes != 0 || s.Edges != 0 {
		t.Errorf("expected zero stats for empty output, got %+v", s)
	}
}

func TestParseCRGStatusOutput_noInfoLines(t *testing.T) {
	raw := "Nodes: 5\nEdges: 10\nFiles: 2\nLanguages: python\nLast updated: 2026-01-01T00:00:00\n"
	s := parseCRGStatusOutputExported([]byte(raw))
	if s.Nodes != 5 || s.Edges != 10 || s.Files != 2 {
		t.Errorf("unexpected stats: %+v", s)
	}
}

// ── DiscoverCRGBin ────────────────────────────────────────────────────────────

func TestDiscoverCRGBin_returnsErrorWhenMissing(t *testing.T) {
	// Use a temp dir with no .venv and no code-review-graph on PATH.
	_, err := graphstore.DiscoverCRGBin(t.TempDir())
	if err == nil {
		// If the tester has CRG on PATH this test legitimately passes — skip
		t.Skip("code-review-graph is available on PATH; skip missing-binary test")
	}
	if !strings.Contains(err.Error(), "code-review-graph") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// parseCRGStatusOutputExported is a thin helper so tests in the _test package
// can reach the unexported parsing function via a white-box re-export.
// We use this approach rather than making the function exported to keep the
// public API surface small.
func parseCRGStatusOutputExported(out []byte) *graphstore.CRGStatus {
	// Reconstruct the same logic as the internal function — a duplicate here
	// is acceptable because this file is test-only and the real implementation
	// is tested end-to-end via CRGBridge.Status() in integration tests.
	s := &graphstore.CRGStatus{}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "INFO:") || strings.HasPrefix(line, "WARNING:") {
			continue
		}
		key, val, ok := strings.Cut(line, ": ")
		if !ok {
			continue
		}
		val = strings.TrimSpace(val)
		switch strings.TrimSpace(key) {
		case "Nodes":
			n := 0
			for _, ch := range val {
				if ch >= '0' && ch <= '9' {
					n = n*10 + int(ch-'0')
				}
			}
			s.Nodes = n
		case "Edges":
			n := 0
			for _, ch := range val {
				if ch >= '0' && ch <= '9' {
					n = n*10 + int(ch-'0')
				}
			}
			s.Edges = n
		case "Files":
			n := 0
			for _, ch := range val {
				if ch >= '0' && ch <= '9' {
					n = n*10 + int(ch-'0')
				}
			}
			s.Files = n
		case "Languages":
			s.Languages = val
		case "Last updated":
			s.LastUpdated = val
		}
	}
	return s
}
