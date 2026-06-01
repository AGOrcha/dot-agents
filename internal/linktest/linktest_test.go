package linktest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/links"
)

// TestLink_RecognizedByContract asserts that a fixture created via Link is
// seen as a managed link by the production predicate, on whatever OS the
// test runs — the whole point of the helper.
func TestLink_RecognizedByContract(t *testing.T) {
	tmp := t.TempDir()

	fileTarget := filepath.Join(tmp, "canonical.txt")
	if err := os.WriteFile(fileTarget, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	fileLink := filepath.Join(tmp, "sub", "file-link")
	Link(t, fileTarget, fileLink)
	if !links.IsManagedLink(fileLink, fileTarget) {
		t.Errorf("file Link fixture not recognized as managed link to %s", fileTarget)
	}

	dirTarget := filepath.Join(tmp, "canonical-dir")
	if err := os.MkdirAll(dirTarget, 0o755); err != nil {
		t.Fatal(err)
	}
	dirLink := filepath.Join(tmp, "dir-link")
	Link(t, dirTarget, dirLink)
	if !links.IsManagedLink(dirLink, dirTarget) {
		t.Errorf("dir Link fixture not recognized as managed link to %s", dirTarget)
	}
}

// TestDanglingLink_DetectedBroken asserts a DanglingLink fixture exists as
// a link whose target is gone — what broken-link detectors key on.
func TestDanglingLink_DetectedBroken(t *testing.T) {
	tmp := t.TempDir()
	link := filepath.Join(tmp, "broken")
	missing := DanglingLink(t, link)

	if _, err := os.Lstat(link); err != nil {
		t.Fatalf("dangling link should exist as an entry: %v", err)
	}
	if _, err := os.Stat(missing); err == nil {
		t.Errorf("returned target %s should not exist", missing)
	}
	// Resolving the link must not reach a real file.
	if _, err := os.Stat(link); err == nil {
		t.Errorf("dangling link should not resolve to an existing target")
	}
}
