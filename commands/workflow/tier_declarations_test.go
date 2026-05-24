package workflow

import (
	"os"
	"strings"
	"testing"
)

// assertTierMolecule reads the named source file and asserts it carries
// `tier: molecule` + the documented `calls:` list of T0 atoms. Extracted
// from the per-case loop below so each subtest stays linear and the
// overall test's cognitive complexity stays under the gate threshold.
func assertTierMolecule(t *testing.T, path string, callsMust []string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	src := string(data)
	if !strings.Contains(src, "tier: molecule") {
		t.Errorf("%s missing `tier: molecule` marker", path)
	}
	if !strings.Contains(src, "calls:") {
		t.Errorf("%s missing `calls:` marker", path)
	}
	for _, atom := range callsMust {
		if !strings.Contains(src, atom) {
			t.Errorf("%s missing atom %q in calls list", path, atom)
		}
	}
}

// The two T1-molecule client commands (close_task.go, start_task.go) carry
// machine-readable tier metadata in their package doc per the
// skill-tiering-contract: `tier: molecule` + a `calls:` list of the T0
// atoms they invoke. A future lint pass reads these markers; this test
// pins them so an accidental refactor that strips them fails the suite
// immediately rather than silently breaking lint downstream.
func TestTierDeclarationsPresentOnClientCommands(t *testing.T) {
	cases := []struct {
		path      string
		callsMust []string
	}{
		{
			path: "close_task.go",
			callsMust: []string{
				"workflow-checkpoint-log-to-iter",
				"score-iteration",
				"workflow-advance",
				"workflow-plan-update",
				"workflow-commit",
			},
		},
		{
			path: "start_task.go",
			callsMust: []string{
				"workflow-plan-update",
				"workflow-plan-derive-scope",
				"workflow-commit",
			},
		},
	}
	for _, c := range cases {
		t.Run(c.path, func(sub *testing.T) {
			assertTierMolecule(sub, c.path, c.callsMust)
		})
	}
}
