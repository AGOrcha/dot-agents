package workflow

import (
	"os"
	"strings"
	"testing"
)

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
		t.Run(c.path, func(t *testing.T) {
			data, err := os.ReadFile(c.path)
			if err != nil {
				t.Fatalf("ReadFile: %v", err)
			}
			src := string(data)
			if !strings.Contains(src, "tier: molecule") {
				t.Errorf("%s missing `tier: molecule` marker", c.path)
			}
			if !strings.Contains(src, "calls:") {
				t.Errorf("%s missing `calls:` marker", c.path)
			}
			for _, atom := range c.callsMust {
				if !strings.Contains(src, atom) {
					t.Errorf("%s missing atom %q in calls list", c.path, atom)
				}
			}
		})
	}
}
