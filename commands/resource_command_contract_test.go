package commands

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found upward from test file")
		}
		dir = parent
	}
}

func TestResourceCommandContractDoc(t *testing.T) {
	path := filepath.Join(repoRoot(t), "docs", "RESOURCE_COMMAND_CONTRACT.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read contract: %v", err)
	}
	body := string(data)

	// Positive: stable anchors for downstream phases and drift callouts.
	mustContain := []string{
		"per-resource Cobra families",
		"hooks list",
		"hooks show",
		"hooks remove",
		"agent-resource-lifecycle",
		"DAG drift",
		"internal/",
	}
	for _, frag := range mustContain {
		if !strings.Contains(body, frag) {
			t.Errorf("contract doc missing expected fragment %q", frag)
		}
	}

	// Negative guard: obsolete audit line claimed hooks were list-only; contract must not regress.
	if strings.Contains(body, "only `hooks list`") {
		t.Error("contract doc still contains obsolete hooks-only-list audit phrasing")
	}
}
