package projection

import (
	"os"
	"path/filepath"
	"testing"
)

// TestHChurn is H-churn: regenerating the same model twice yields byte-identical
// output (deterministic ordering) for EVERY real file. This is the no-git-noise
// guarantee — once a file is in canonical form, projecting it again never moves
// a byte. Runs over the real tree including the gnarly block-scalar/notes files.
func TestHChurn(t *testing.T) {
	root := realPlansRoot(t)
	checked := 0
	for _, dir := range planDirs(t, root) {
		if p := filepath.Join(dir, "PLAN.yaml"); fileExists(p) {
			checkPlanChurn(t, p)
			checked++
		}
		if p := filepath.Join(dir, "TASKS.yaml"); fileExists(p) {
			checkTasksChurn(t, p)
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("no files checked for churn")
	}
	t.Logf("H-churn: %d files deterministic + fixed-point", checked)
}

// checkPlanChurn asserts a PLAN.yaml serializes deterministically and is a
// serialize fixed point (regen of regen == regen).
func checkPlanChurn(t *testing.T, p string) {
	t.Helper()
	data, _ := os.ReadFile(p)
	pl, err := ParsePlan(data)
	if err != nil {
		t.Errorf("%s: %v", p, err)
		return
	}
	a, _ := SerializePlan(pl)
	b, _ := SerializePlan(pl)
	if string(a) != string(b) {
		t.Errorf("%s: PLAN serialize is non-deterministic", p)
	}
	pl2, _ := ParsePlan(a)
	c, _ := SerializePlan(pl2)
	if string(a) != string(c) {
		t.Errorf("%s: PLAN not a serialize fixed point", p)
	}
}

// checkTasksChurn is the TASKS.yaml analogue of checkPlanChurn.
func checkTasksChurn(t *testing.T, p string) {
	t.Helper()
	data, _ := os.ReadFile(p)
	tf, err := ParseTasks(data)
	if err != nil {
		t.Errorf("%s: %v", p, err)
		return
	}
	a, _ := SerializeTasks(tf)
	b, _ := SerializeTasks(tf)
	if string(a) != string(b) {
		t.Errorf("%s: TASKS serialize is non-deterministic", p)
	}
	tf2, _ := ParseTasks(a)
	c, _ := SerializeTasks(tf2)
	if string(a) != string(c) {
		t.Errorf("%s: TASKS not a serialize fixed point", p)
	}
}

// TestNegativeControlNaiveChurn is the NEGATIVE CONTROL the directive demands:
// it proves churn-free round-trip is NOT trivial. The naive serializer (generic
// map -> sorted keys) produces real diff churn on the real files, while the
// canonical serializer reduces it to zero on the un-hand-edited ones. The DELTA
// is the finding: struct-order key emission is load-bearing.
func TestNegativeControlNaiveChurn(t *testing.T) {
	root := realPlansRoot(t)
	var naiveTotal, canonTotal, files int
	worstNaive := 0
	for _, dir := range planDirs(t, root) {
		p := filepath.Join(dir, "TASKS.yaml")
		if !fileExists(p) {
			continue
		}
		orig, _ := os.ReadFile(p)
		tf, err := ParseTasks(orig)
		if err != nil {
			continue
		}
		canon, _ := SerializeTasks(tf)
		naive, _ := NaiveSerializeTasks(tf)

		nChurn := DiffLineCount(orig, naive)
		cChurn := DiffLineCount(orig, canon)
		naiveTotal += nChurn
		canonTotal += cChurn
		files++
		if nChurn > worstNaive {
			worstNaive = nChurn
		}
		// On any given file the naive serializer must churn at least as much as
		// canonical (it reorders keys on top of whatever canonical does).
		if nChurn < cChurn {
			t.Errorf("%s: naive churn %d < canonical churn %d (unexpected)", p, nChurn, cChurn)
		}
	}
	if files == 0 {
		t.Fatal("no TASKS files for negative control")
	}
	t.Logf("NEGATIVE CONTROL over %d TASKS files: naive churn=%d lines total (worst single file=%d), canonical churn=%d lines total",
		files, naiveTotal, worstNaive, canonTotal)
	// The whole point: naive churn must DWARF canonical churn. If they were
	// close, the canonical discipline wouldn't be doing anything.
	if naiveTotal <= canonTotal {
		t.Fatalf("negative control failed: naive churn (%d) did not exceed canonical churn (%d) — "+
			"the canonical serializer is not demonstrably better, experiment is uninformative", naiveTotal, canonTotal)
	}
	if naiveTotal < canonTotal*5 {
		t.Logf("WARNING: naive churn only %dx canonical — weaker separation than expected", naiveTotal/max(canonTotal, 1))
	}
}
