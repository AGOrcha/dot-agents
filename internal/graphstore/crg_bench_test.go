package graphstore

import "testing"

// BenchmarkParseCRGBuildOutput guards the regexp hoist: the four CLI-line
// patterns are compiled once at package init, so the per-call cost is a
// match, not a recompile.
func BenchmarkParseCRGBuildOutput(b *testing.B) {
	out := []byte("INFO: importing graph\nWARNING: noop\n" +
		"Incremental: 3 files updated, 12 nodes, 7 edges (postprocess=full)\n")
	b.ReportAllocs()
	for b.Loop() {
		report := parseCRGBuildOutput(out)
		if report.FilesUpdated == nil || *report.FilesUpdated != 3 {
			b.Fatal("expected the incremental line to parse")
		}
	}
}
