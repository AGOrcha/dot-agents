package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

const (
	resourceBenchBucket  = "skills"
	resourceBenchProject = "bench-project"
	resourceBenchMarker  = "SKILL.md"
)

func buildScopedResourceScanFixture(b testing.TB, n int) (string, []string) {
	b.Helper()
	home := b.TempDir()
	root := filepath.Join(home, resourceBenchBucket, resourceBenchProject)
	if err := os.MkdirAll(root, 0o755); err != nil {
		b.Fatal(err)
	}

	names := make([]string, n+1)
	for i := range n {
		entry := fmt.Sprintf("resource-%04d", i)
		entryDir := filepath.Join(root, entry)
		if err := os.MkdirAll(entryDir, 0o755); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(entryDir, resourceBenchMarker), []byte("bench\n"), 0o644); err != nil {
			b.Fatal(err)
		}
		names[i] = fmt.Sprintf("missing-%04d", i)
	}
	names[n] = fmt.Sprintf("resource-%04d", n-1)
	return home, names
}

func BenchmarkScopedResourceScan(b *testing.B) {
	for _, n := range []int{10, 50, 200} {
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			home, names := buildScopedResourceScanFixture(b, n)
			if got := resolveScopedFile(home, resourceBenchBucket, resourceBenchProject, names...); got == "" {
				b.Fatal("fixture scoped file was not resolved")
			}
			if entries, err := listScopedResourceDirs(home, resourceBenchBucket, resourceBenchProject, resourceBenchMarker); err != nil || len(entries) != n {
				b.Fatalf("fixture resource scan: entries=%d, err=%v", len(entries), err)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				resolveScopedFile(home, resourceBenchBucket, resourceBenchProject, names...)
				if _, err := listScopedResourceDirs(home, resourceBenchBucket, resourceBenchProject, resourceBenchMarker); err != nil {
					b.Fatalf("listScopedResourceDirs: %v", err)
				}
			}
		})
	}
}
