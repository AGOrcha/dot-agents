package scoring

import "testing"

func TestPresentSignalClamps(t *testing.T) {
	tests := []struct {
		in   float64
		want float64
	}{
		{0.5, 0.5},
		{0.0, 0.0},
		{1.0, 1.0},
		{-0.3, 0.0}, // clamps below
		{1.7, 1.0},  // clamps above
	}
	for _, tt := range tests {
		got := PresentSignal(tt.in, "detail text")
		if !got.Present {
			t.Errorf("PresentSignal(%g).Present = false, want true", tt.in)
		}
		if got.SubScore != tt.want {
			t.Errorf("PresentSignal(%g).SubScore = %g, want %g", tt.in, got.SubScore, tt.want)
		}
		if got.Detail != "detail text" {
			t.Errorf("PresentSignal(%g).Detail = %q, want %q", tt.in, got.Detail, "detail text")
		}
	}
}

func TestAbsentSignal(t *testing.T) {
	got := AbsentSignal("no data captured")
	if got.Present {
		t.Error("AbsentSignal().Present = true, want false")
	}
	if got.Detail != "no data captured" {
		t.Errorf("AbsentSignal().Detail = %q, want %q", got.Detail, "no data captured")
	}
}
