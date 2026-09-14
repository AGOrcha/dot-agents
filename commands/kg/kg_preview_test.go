package kg

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestConfigPath verifies the ConfigPath public alias resolves under KG_HOME.
func TestConfigPath_UsesKGHome(t *testing.T) {
	t.Setenv("KG_HOME", "/tmp/cp-test-graph")
	want := filepath.Join("/tmp/cp-test-graph", "self", "config.yaml")
	if got := ConfigPath(); got != want {
		t.Errorf("ConfigPath() = %q, want %q", got, want)
	}
}

// TestPreviewSingleIngest_PrintsHeaderAndCounts captures stdout and asserts
// the dry-run preview line content.
func TestPreviewSingleIngest_PrintsHeaderAndCounts(t *testing.T) {
	content := []byte("# Heading\n\nThis describes Alice and Bob.\n\nDecision: pick Postgres.\n")
	out := string(captureStdout(t, func() {
		previewSingleIngest("src/123", "Sample Title", "url", content)
	}))
	if !strings.Contains(out, "Source ID: src/123") {
		t.Errorf("missing Source ID line: %q", out)
	}
	if !strings.Contains(out, "Title: Sample Title") {
		t.Errorf("missing Title line: %q", out)
	}
	if !strings.Contains(out, "Type: url") {
		t.Errorf("missing Type line: %q", out)
	}
	if !strings.Contains(out, "Entities found:") {
		t.Errorf("missing entities line: %q", out)
	}
	if !strings.Contains(out, "Decisions found:") {
		t.Errorf("missing decisions line: %q", out)
	}
}
