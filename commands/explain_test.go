package commands

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestExplainLinks_MentionsSharedTargetRegistryDiagnostics(t *testing.T) {
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	printLinkTypesExplanation()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(r)
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = old

	s := string(out)
	if !strings.Contains(s, "status --audit") {
		t.Fatalf("explain links output missing status --audit pointer:\n%s", s)
	}
	if !strings.Contains(s, "Shared target registry") {
		t.Fatalf("explain links output missing Shared target registry label:\n%s", s)
	}
	if !strings.Contains(s, "refresh --dry-run") {
		t.Fatalf("explain links output missing refresh --dry-run parity note:\n%s", s)
	}
}
