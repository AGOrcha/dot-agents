package graphproj

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AGOrcha/dot-agents/prototype/work-store-projection/projection"
)

func realPlansRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join("..", "..", "..", ".agents", "workflow", "plans")
	if _, err := os.Stat(root); err != nil {
		t.Skipf("real plans tree not found: %v", err)
	}
	return root
}

func planDirs(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read root: %v", err)
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, filepath.Join(root, e.Name()))
		}
	}
	return dirs
}

// TestGraphV4LosesFieldsOnRealTree is the REAL negative control: routing the
// real corpus through the shipped schema-v4 graph and reconstructing from
// readback LOSES fields — quantified. This is NOT the tautological struct
// mirror: the reconstruction uses only node fields + edges the v4 profile
// actually persists.
func TestGraphV4LosesFieldsOnRealTree(t *testing.T) {
	root := realPlansRoot(t)
	total := 0
	byField := map[string]int{}
	plansWithLoss := 0
	for _, dir := range planDirs(t, root) {
		in, err := LoadPlanDir(dir)
		if err != nil || in.Plan == nil || in.Tasks == nil {
			continue
		}
		rep := AnalyzePlan(SchemaV4, in.Plan, in.Tasks, in.Slices)
		if len(rep.Losses) > 0 {
			plansWithLoss++
		}
		for _, l := range rep.Losses {
			total++
			byField[l.Field]++
		}
	}
	if total == 0 {
		t.Fatal("schema-v4 graph round-trip reported ZERO field loss on the real tree — " +
			"either the experiment is blind or v4 is already complete (it is not)")
	}
	// The dropped fields the audit named MUST appear in the loss tally.
	for _, f := range []string{"notes", "write_scope", "owner", "verification_required", "depends_on", "blocks"} {
		if byField[f] == 0 {
			t.Errorf("expected schema-v4 to lose %q on the real tree, but it did not", f)
		}
	}
	t.Logf("schema-v4 REAL-graph round-trip: %d field-losses across %d plans; by field: %v", total, plansWithLoss, byField)
}

// TestGraphCompleteIsLosslessOnRealTree is the positive: a completeness-extended
// graph round-trips EVERY typed field of EVERY plan/task/slice on the real tree
// with zero loss. The delta vs v4 is the schema-completeness gap.
func TestGraphCompleteIsLosslessOnRealTree(t *testing.T) {
	root := realPlansRoot(t)
	checked := 0
	for _, dir := range planDirs(t, root) {
		in, err := LoadPlanDir(dir)
		if err != nil || in.Plan == nil || in.Tasks == nil {
			continue
		}
		rep := AnalyzePlan(Complete, in.Plan, in.Tasks, in.Slices)
		if len(rep.Losses) > 0 {
			t.Errorf("%s: complete graph LOST fields (should be lossless): %+v", filepath.Base(dir), rep.Losses)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no plans checked")
	}
	t.Logf("complete-graph round-trip: %d plans lossless", checked)
}

// TestGraphCompleteReconstructsByteFaithful connects the graph round-trip back
// to git churn: reconstructing from the COMPLETE graph then serializing must
// reproduce the SAME bytes as serializing the directly-parsed model. I.e. the
// graph path introduces no additional churn over the struct path.
func TestGraphCompleteReconstructsByteFaithful(t *testing.T) {
	root := realPlansRoot(t)
	checked := 0
	for _, dir := range planDirs(t, root) {
		in, err := LoadPlanDir(dir)
		if err != nil || in.Plan == nil || in.Tasks == nil {
			continue
		}
		// Direct struct serialization (the baseline).
		wantTasks, _ := projection.SerializeTasks(in.Tasks)
		// Graph-reconstructed serialization.
		gotPlan, gotTasks, _, _ := GraphRoundTrip(Complete, in)
		gotBytes, _ := projection.SerializeTasks(gotTasks)
		if string(wantTasks) != string(gotBytes) {
			t.Errorf("%s: TASKS graph-reconstructed bytes differ from struct-serialized bytes", filepath.Base(dir))
		}
		wantPlan, _ := projection.SerializePlan(in.Plan)
		gotPlanBytes, _ := projection.SerializePlan(gotPlan)
		if string(wantPlan) != string(gotPlanBytes) {
			t.Errorf("%s: PLAN graph-reconstructed bytes differ from struct-serialized bytes", filepath.Base(dir))
		}
		checked++
	}
	t.Logf("graph-reconstructed == struct-serialized for %d plans (complete profile)", checked)
}
