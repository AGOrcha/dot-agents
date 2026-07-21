package platform

import (
	"fmt"
	"path/filepath"
	"testing"
)

// BenchmarkManagedHookRefresh models the managed-hook rewrite hot path: a
// single command (refresh/projection) writes N managed files, each of which
// consults the render-manifest for provenance. The steady-state case (files
// already correct on disk) is the common refresh, so the loop re-writes an
// unchanged fixture — exactly the H6 "N-1 redundant reads/parses per command"
// shape for the render-manifest.
func BenchmarkManagedHookRefresh(b *testing.B) {
	for _, n := range []int{10, 50, 200} {
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			stateHome := b.TempDir()
			dstRoot := b.TempDir()
			b.Setenv("XDG_STATE_HOME", stateHome)

			dsts := make([]string, n)
			contents := make([][]byte, n)
			for i := range n {
				dsts[i] = filepath.Join(dstRoot, fmt.Sprintf("managed-%04d.json", i))
				contents[i] = []byte(fmt.Sprintf(`{"managed":%d,"schema":"bench"}`, i))
			}
			// Warm: create every managed file + record provenance once.
			for i := range n {
				if err := writeManagedFile(stdPlatformIO{}, dsts[i], contents[i]); err != nil {
					b.Fatalf("warm writeManagedFile: %v", err)
				}
			}

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				for i := range n {
					if err := writeManagedFile(stdPlatformIO{}, dsts[i], contents[i]); err != nil {
						b.Fatalf("writeManagedFile: %v", err)
					}
				}
			}
		})
	}
}
