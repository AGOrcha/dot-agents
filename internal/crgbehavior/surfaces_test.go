package crgbehavior

import (
	"fmt"
	"strings"
	"testing"
)

// grTJoinDetail renders a surface's structural diff for assertion messages and
// substring checks.
func grTJoinDetail(s Surface) string { return strings.Join(s.Detail, "\n") }

// compareRows is the gate's ONE definition of "equal", so its diff has to be a
// faithful set difference: a row the release emitted twice is still one fact
// about the release, and reporting it twice would make a duplicated row look
// like two independent divergences.
func TestGrTCompareRowsReportsEachDifferingRowExactlyOnce(t *testing.T) {
	cases := map[string]struct {
		native, bridge []string
		wantStatus     SurfaceStatus
		wantDetail     []string
	}{
		"identical sets agree": {
			native: []string{"a", "b"}, bridge: []string{"b", "a"},
			wantStatus: StatusAgree,
		},
		"a set equal only after deduping still agrees": {
			native: []string{"a", "a", "b"}, bridge: []string{"b", "a"},
			wantStatus: StatusAgree,
		},
		"a duplicated native-only row is reported once": {
			native: []string{"x", "x", "y"}, bridge: []string{"y"},
			wantStatus: StatusDiverge,
			wantDetail: []string{"only in NATIVE: x"},
		},
		"a duplicated bridge-only row is reported once": {
			native: []string{"y"}, bridge: []string{"z", "z", "y"},
			wantStatus: StatusDiverge,
			wantDetail: []string{"only in BRIDGE: z"},
		},
		"both sides are named, native first": {
			native: []string{"n", "n"}, bridge: []string{"b", "b"},
			wantStatus: StatusDiverge,
			wantDetail: []string{"only in NATIVE: n", "only in BRIDGE: b"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := compareRows(SurfaceChangedNodes, tc.native, tc.bridge)
			if got.Status != tc.wantStatus {
				t.Fatalf("status = %s, want %s (detail %v)", got.Status, tc.wantStatus, got.Detail)
			}
			if !equalStrings(got.Detail, tc.wantDetail) {
				t.Fatalf("detail = %v, want %v", got.Detail, tc.wantDetail)
			}
		})
	}
}

// The metric states both side's row counts BEFORE deduping, so a report reader
// can see that one side emitted a duplicate at all.
func TestGrTCompareRowsMetricCountsRawRowsOnBothSides(t *testing.T) {
	got := compareRows(SurfaceRiskIndex, []string{"a", "a"}, []string{"a"})
	if got.Metric != "native=2 bridge=1 row(s)" {
		t.Fatalf("metric = %q, want the raw per-side row counts", got.Metric)
	}
}

// A systematically divergent surface must not bury the rest of the report, and
// the truncation has to state how much was withheld — a silently clipped diff
// would understate the divergence a decommission decision is made on.
func TestGrTCapDetailBoundsTheDiffAndCountsTheRemainder(t *testing.T) {
	native := make([]string, 0, maxDetailLines+2)
	for i := range maxDetailLines + 2 {
		native = append(native, fmt.Sprintf("row-%02d", i))
	}
	got := compareRows(SurfaceFlows, native, nil)
	if got.Status != StatusDiverge {
		t.Fatalf("status = %s, want a divergence", got.Status)
	}
	if len(got.Detail) != maxDetailLines+1 {
		t.Fatalf("detail has %d line(s), want %d capped rows plus one truncation note:\n%s",
			len(got.Detail), maxDetailLines, grTJoinDetail(got))
	}
	if want := "... and 2 more difference(s)"; got.Detail[maxDetailLines] != want {
		t.Fatalf("last detail line = %q, want %q", got.Detail[maxDetailLines], want)
	}
	if got.Detail[0] != "only in NATIVE: row-00" {
		t.Fatalf("first detail line = %q, want the smallest row kept", got.Detail[0])
	}
}

// A diff exactly at the cap is NOT truncated: an off-by-one here would append a
// "... and 0 more difference(s)" line to a complete diff.
func TestGrTCapDetailKeepsADiffExactlyAtTheCap(t *testing.T) {
	detail := make([]string, maxDetailLines)
	for i := range detail {
		detail[i] = fmt.Sprintf("row-%02d", i)
	}
	got := capDetail(detail)
	if len(got) != maxDetailLines || !equalStrings(got, detail) {
		t.Fatalf("capDetail truncated a diff at the cap: %v", got)
	}
}
