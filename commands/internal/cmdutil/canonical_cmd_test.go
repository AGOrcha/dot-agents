package cmdutil

import "testing"

func TestCanonicalCmdExampleBlock_JoinsWithNewlines(t *testing.T) {
	got := CanonicalCmdExampleBlock("a", "b", "c")
	want := "a\nb\nc"
	if got != want {
		t.Errorf("CanonicalCmdExampleBlock = %q, want %q", got, want)
	}
}

func TestCanonicalCmdExampleBlock_SingleLine(t *testing.T) {
	got := CanonicalCmdExampleBlock("  da rules list")
	want := "  da rules list"
	if got != want {
		t.Errorf("CanonicalCmdExampleBlock single = %q, want %q", got, want)
	}
}

func TestCanonicalCmdExampleBlock_NoArgsEmpty(t *testing.T) {
	got := CanonicalCmdExampleBlock()
	if got != "" {
		t.Errorf("CanonicalCmdExampleBlock() = %q, want empty string", got)
	}
}

func TestCanonicalCmdFlags_ZeroValue(t *testing.T) {
	var f CanonicalCmdFlags
	if f.DryRun || f.Yes || f.Force {
		t.Errorf("zero-value CanonicalCmdFlags should have all-false fields, got %+v", f)
	}
}

func TestCanonicalCmdFlags_FieldRoundTrip(t *testing.T) {
	f := CanonicalCmdFlags{DryRun: true, Yes: false, Force: true}
	if !f.DryRun || f.Yes || !f.Force {
		t.Errorf("CanonicalCmdFlags fields did not round-trip: %+v", f)
	}
}
