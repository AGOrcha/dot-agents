package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func writeScopeFile(t *testing.T, agentsHome, bucket, scope, baseName string, content []byte) {
	t.Helper()
	dir := filepath.Join(agentsHome, bucket, scope)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, baseName), content, 0644); err != nil {
		t.Fatal(err)
	}
}

// mustMkdirAllT calls os.MkdirAll, fatalling via the testing.T helper.
func mustMkdirAllT(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
}

// mustWriteFileT writes a file, fatalling via the testing.T helper.
func mustWriteFileT(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// writeAgentsHomeFile is a convenience helper that ensures the parent dir
// exists then writes a file under agentsHome/<rel>.
func writeAgentsHomeFile(t *testing.T, agentsHome, rel, content string) {
	t.Helper()
	full := filepath.Join(agentsHome, rel)
	mustMkdirAllT(t, filepath.Dir(full))
	mustWriteFileT(t, full, content)
}
