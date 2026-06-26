package graphproj

import (
	"os"
	"path/filepath"

	"github.com/AGOrcha/dot-agents/prototype/work-store-projection/projection"
)

// PlanInputs are the parsed YAML files for one plan dir (whichever exist).
type PlanInputs struct {
	Plan   *projection.Plan
	Tasks  *projection.TaskFile
	Slices *projection.SliceFile
}

// LoadPlanDir parses the PLAN.yaml/TASKS.yaml/SLICES.yaml in dir. Missing files
// yield nil entries (a plan with no SLICES.yaml is normal).
func LoadPlanDir(dir string) (PlanInputs, error) {
	var in PlanInputs
	if err := readIf(filepath.Join(dir, "PLAN.yaml"), func(b []byte) error {
		p, e := projection.ParsePlan(b)
		in.Plan = p
		return e
	}); err != nil {
		return in, err
	}
	if err := readIf(filepath.Join(dir, "TASKS.yaml"), func(b []byte) error {
		t, e := projection.ParseTasks(b)
		in.Tasks = t
		return e
	}); err != nil {
		return in, err
	}
	if err := readIf(filepath.Join(dir, "SLICES.yaml"), func(b []byte) error {
		s, e := projection.ParseSlices(b)
		in.Slices = s
		return e
	}); err != nil {
		return in, err
	}
	return in, nil
}

// readIf reads path and calls parse when it exists; a missing file is a no-op.
func readIf(path string, parse func([]byte) error) error {
	if !exists(path) {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return parse(data)
}

// GraphRoundTrip ingests inputs into a graph under profile p, reconstructs the
// models from readback, and returns them. This is the REAL round-trip: the
// returned models carry only what the graph held.
func GraphRoundTrip(p Profile, in PlanInputs) (plan *projection.Plan, tasks *projection.TaskFile, slices *projection.SliceFile, store interface{ Stats() (int, int) }) {
	s := Ingest(p, in.Plan, in.Tasks, in.Slices)
	plan = ReconstructPlan(s, in.Plan.ID)
	sv := 0
	if in.Tasks != nil {
		sv = in.Tasks.SchemaVersion
		tasks = ReconstructTasks(s, in.Plan.ID, sv)
	}
	if in.Slices != nil {
		slices = ReconstructSlices(s, in.Slices.PlanID, in.Slices.SchemaVersion)
	}
	return plan, tasks, slices, s
}

func exists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}
