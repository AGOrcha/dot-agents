package projection

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// realPlansRoot locates the live .agents/workflow/plans tree relative to this
// prototype package. The proofs run over the REAL corpus (the hard cases ARE
// the experiment), not a hand-picked clean fixture.
func realPlansRoot(t *testing.T) string {
	t.Helper()
	// package dir: <repo>/prototype/work-store-projection/projection
	root := filepath.Join("..", "..", "..", ".agents", "workflow", "plans")
	if _, err := os.Stat(root); err != nil {
		t.Skipf("real plans tree not found at %s: %v", root, err)
	}
	return root
}

func planDirs(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read plans root: %v", err)
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, filepath.Join(root, e.Name()))
		}
	}
	if len(dirs) == 0 {
		t.Fatal("no plan dirs found — corpus empty, proof would be vacuous")
	}
	return dirs
}

// TestHRoundtrip is H-roundtrip: ingest -> regenerate -> grade over the real
// tree. The proof is NOT "everything is byte-identical" (we know some files
// were hand-edited with non-canonical style). The proof is the PRECISE
// classification: every file is byte-identical, semantic-equal, normalized, or
// an explicitly-named lossy case — and there are NO unexplained losses.
func TestHRoundtrip(t *testing.T) {
	root := realPlansRoot(t)
	var c gradeCounts
	sawFile := false
	for _, dir := range planDirs(t, root) {
		for _, fr := range RoundTripPlanDir(dir) {
			sawFile = true
			c.classify(t, fr)
		}
	}
	if !sawFile {
		t.Fatal("no PLAN/TASKS files round-tripped")
	}
	ident, sem, norm, lossy := c.ident, c.sem, c.norm, c.lossy
	t.Logf("H-roundtrip over real tree: %d byte-identical, %d semantic-equal, %d normalized, %d lossy",
		ident, sem, norm, lossy)
	t.Logf("lossy files (explicit, expected): %v", c.lossyFiles)
	// The majority MUST be byte-identical — proves the canonical serializer
	// matches what da itself writes for the un-hand-edited files.
	if ident == 0 {
		t.Fatal("zero byte-identical files: canonical serializer does not match the on-disk format")
	}
	if ident < sem+norm+lossy {
		t.Errorf("byte-identical (%d) should dominate; got non-identical=%d", ident, sem+norm+lossy)
	}
}

// gradeCounts tallies round-trip grades and validates each file's invariants
// (a semantic-equal file must state a reason; a lossy file must name what it
// dropped). Extracted from TestHRoundtrip to keep that test's cognitive
// complexity low while preserving the per-file assertions.
type gradeCounts struct {
	ident, sem, norm, lossy int
	lossyFiles              []string
}

func (c *gradeCounts) classify(t *testing.T, fr FileRoundTrip) {
	t.Helper()
	if fr.Err != nil {
		t.Errorf("%s: round-trip error: %v", fr.Path, fr.Err)
		return
	}
	switch fr.Fidelity.Grade {
	case ByteIdentical:
		c.ident++
	case SemanticEqual:
		c.sem++
		if len(fr.Fidelity.Reasons) == 0 {
			t.Errorf("%s: semantic-equal with no stated reason", fr.Path)
		}
	case Normalized:
		c.norm++
	case Lossy:
		c.lossy++
		c.lossyFiles = append(c.lossyFiles, filepath.Base(filepath.Dir(fr.Path))+"/"+filepath.Base(fr.Path))
		if len(fr.Fidelity.DroppedKeys) == 0 {
			t.Errorf("%s: graded lossy but named no dropped keys — loss must be explicit", fr.Path)
		}
	default:
		t.Errorf("%s: unknown grade %q", fr.Path, fr.Fidelity.Grade)
	}
}

// TestHRoundtripLosslessOrExplained asserts the central D1' claim per file:
// either the round-trip is byte-identical/semantic-equal/normalized (NO
// information lost), or it is lossy with the lost keys NAMED. There is no
// silent-loss path.
func TestHRoundtripLosslessOrExplained(t *testing.T) {
	root := realPlansRoot(t)
	for _, dir := range planDirs(t, root) {
		for _, fr := range RoundTripPlanDir(dir) {
			assertLosslessOrExplained(t, fr)
		}
	}
}

// assertLosslessOrExplained checks one file is either in a lossless tier or a
// lossy file that NAMES every dropped key (no empty names, no silent loss).
func assertLosslessOrExplained(t *testing.T, fr FileRoundTrip) {
	t.Helper()
	if fr.Err != nil {
		t.Errorf("%s: %v", fr.Path, fr.Err)
		return
	}
	if fr.Fidelity.Grade != Lossy {
		return // ByteIdentical / SemanticEqual / Normalized are lossless
	}
	if len(fr.Fidelity.DroppedKeys) == 0 {
		t.Errorf("%s: lossy without naming what was lost", fr.Path)
	}
	for _, k := range fr.Fidelity.DroppedKeys {
		if strings.TrimSpace(k) == "" {
			t.Errorf("%s: empty dropped-key name", fr.Path)
		}
	}
}
