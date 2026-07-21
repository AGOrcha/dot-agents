package graphstore

import "testing"

// BenchmarkParseCRGMutationSummary guards the regexp hoist: crgMutationSummaryRE
// is compiled once at package init, so the per-call cost is a match, not a
// recompile. Before the hoist this benchmark paid a regexp.MustCompile on every
// call (hundreds of allocs); after, allocs/op reflect only the scan + Sscanf.
func BenchmarkParseCRGMutationSummary(b *testing.B) {
	out := []byte("INFO: importing graph\nWARNING: noop\n3 files updated 12 nodes 7 edges\n")
	b.ReportAllocs()
	for b.Loop() {
		if _, _, _, ok := parseCRGMutationSummary(out); !ok {
			b.Fatal("expected a summary match")
		}
	}
}
