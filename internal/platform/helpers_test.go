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
