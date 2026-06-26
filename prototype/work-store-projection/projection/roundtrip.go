package projection

import (
	"os"
	"path/filepath"
	"strings"
)

// FileRoundTrip is the result of round-tripping one file in a plan dir.
type FileRoundTrip struct {
	Path     string
	Orig     []byte
	Regen    []byte
	Fidelity Fidelity
	Err      error
}

// RoundTripPlanDir ingests + regenerates the PLAN.yaml and TASKS.yaml in dir
// (whichever exist) and returns a graded result per file. This is the single
// production path the demo main and the proof tests both drive — so the tests
// exercise exactly what the tool reports.
func RoundTripPlanDir(dir string) []FileRoundTrip {
	var out []FileRoundTrip
	if p := filepath.Join(dir, "PLAN.yaml"); fileExists(p) {
		out = append(out, roundTripPlanFile(p))
	}
	if p := filepath.Join(dir, "TASKS.yaml"); fileExists(p) {
		out = append(out, roundTripTasksFile(p))
	}
	return out
}

func roundTripPlanFile(path string) FileRoundTrip {
	orig, err := os.ReadFile(path)
	if err != nil {
		return FileRoundTrip{Path: path, Err: err}
	}
	p, err := ParsePlan(orig)
	if err != nil {
		return FileRoundTrip{Path: path, Orig: orig, Err: err}
	}
	regen, err := SerializePlan(p)
	if err != nil {
		return FileRoundTrip{Path: path, Orig: orig, Err: err}
	}
	fid, err := GradePlanRoundTrip(path, orig)
	return FileRoundTrip{Path: path, Orig: orig, Regen: regen, Fidelity: fid, Err: err}
}

func roundTripTasksFile(path string) FileRoundTrip {
	orig, err := os.ReadFile(path)
	if err != nil {
		return FileRoundTrip{Path: path, Err: err}
	}
	tf, err := ParseTasks(orig)
	if err != nil {
		return FileRoundTrip{Path: path, Orig: orig, Err: err}
	}
	regen, err := SerializeTasks(tf)
	if err != nil {
		return FileRoundTrip{Path: path, Orig: orig, Err: err}
	}
	fid, err := GradeTasksRoundTrip(path, orig)
	return FileRoundTrip{Path: path, Orig: orig, Regen: regen, Fidelity: fid, Err: err}
}

// fileExists reports whether path is an existing regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// Indent prefixes every line of s with pad.
func Indent(s, pad string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = pad + l
		}
	}
	return strings.Join(lines, "\n")
}
