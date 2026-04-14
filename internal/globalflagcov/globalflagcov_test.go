package globalflagcov

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestReportNoUnresolvedHandlers(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	rows, err := Report(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) < 10 {
		t.Fatalf("expected many command rows, got %d", len(rows))
	}
	for _, r := range rows {
		if strings.Contains(r.Notes, "unknown") || strings.Contains(r.Notes, "unresolved") {
			t.Errorf("%s: %s", r.Path, r.Notes)
		}
	}
}

func TestSyncPullReadsDryRun(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	rows, err := Report(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Path == "sync pull" {
			if !r.Flags.DryRun {
				t.Fatal("expected sync pull to reference Flags.DryRun (error path)")
			}
			return
		}
	}
	t.Fatal("sync pull row not found")
}
