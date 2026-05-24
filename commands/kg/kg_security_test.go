package kg

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestIngest_SrcIDPathTraversalSanitized is a regression test for the
// note-id path-traversal finding: an attacker-authored inbox file whose
// YAML frontmatter carries `id: ../../../.../tmp/<marker>` must NOT cause
// ingest to write a note outside KG_HOME. `src.ID` is now slugified (the
// same sanitizer applied to entity IDs and `kg add`), so traversal
// sequences (`/`, `.`, `..`) cannot survive into the filesystem path.
func TestIngest_SrcIDPathTraversalSanitized(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(testIO()); err != nil {
		t.Fatalf("setup: %v", err)
	}

	marker := fmt.Sprintf("kgtrav-%d", time.Now().UnixNano())
	// Deep enough to climb out of any t.TempDir() and land in /tmp.
	evilID := strings.Repeat("../", 14) + "tmp/" + marker
	escapeProbe := filepath.Join("/tmp", marker+".md")
	_ = os.Remove(escapeProbe)
	t.Cleanup(func() { _ = os.Remove(escapeProbe) })

	// Write the raw inbox file directly with malicious frontmatter; the
	// filename itself is benign so resolution is via the frontmatter id.
	inbox := filepath.Join(home, "raw", "inbox")
	if err := os.MkdirAll(inbox, 0755); err != nil {
		t.Fatal(err)
	}
	raw := "---\n" +
		"schema_version: 1\n" +
		"id: \"" + evilID + "\"\n" +
		"title: Evil\n" +
		"source_type: markdown\n" +
		"status: pending\n" +
		"captured_at: 2026-01-01T00:00:00Z\n" +
		"---\n\nWe decided to ship. The Widget class works.\n"
	if err := os.WriteFile(filepath.Join(inbox, "evil.md"), []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}

	captureStdout(t, func() { runSingleIngest(testDeps(), home, "evil") })

	// 1. No file escaped KG_HOME.
	if _, err := os.Stat(escapeProbe); err == nil {
		t.Fatalf("path traversal: ingest wrote outside KG_HOME at %s", escapeProbe)
	}
	// 2. A sanitized source note was created INSIDE KG_HOME (slugify drops
	//    the slashes/dots, leaving the alnum tail).
	wantID := "src-" + slugify(evilID)
	if strings.ContainsAny(wantID, "/.") || strings.Contains(wantID, "..") {
		t.Fatalf("sanitized id still contains traversal chars: %q", wantID)
	}
	if exists, _ := noteExists(home, wantID); !exists {
		t.Errorf("expected sanitized source note %q inside KG_HOME", wantID)
	}
}

// TestIngest_SrcIDAllPunctuationFallsBackToSourceID covers the branch where
// the frontmatter id is entirely traversal/punctuation so slugify() yields
// "" — ingest must fall back to the (sanitized) filename-derived sourceID
// rather than producing a bare "src-" path.
func TestIngest_SrcIDAllPunctuationFallsBackToSourceID(t *testing.T) {
	home := newTempKG(t)
	if err := runKGSetup(testIO()); err != nil {
		t.Fatalf("setup: %v", err)
	}
	inbox := filepath.Join(home, "raw", "inbox")
	if err := os.MkdirAll(inbox, 0755); err != nil {
		t.Fatal(err)
	}
	// id is only slashes/dots → slugify("") path; filename stem "benign"
	// is the safe fallback source id.
	raw := "---\n" +
		"schema_version: 1\n" +
		"id: \"../../../..\"\n" +
		"title: Benign\n" +
		"source_type: markdown\n" +
		"status: pending\n" +
		"captured_at: 2026-01-01T00:00:00Z\n" +
		"---\n\nWe decided to ship. The Widget class works.\n"
	if err := os.WriteFile(filepath.Join(inbox, "benign.md"), []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}

	captureStdout(t, func() { runSingleIngest(testDeps(), home, "benign") })

	if exists, _ := noteExists(home, "src-benign"); !exists {
		t.Errorf("expected fallback source note \"src-benign\" (from sourceID) inside KG_HOME")
	}
}
