package globalflagcov

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestTrimDotAgents(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"empty", []string{}, []string{}},
		{"nil", nil, nil},
		{"strips leading da", []string{"da", "workflow", "status"}, []string{"workflow", "status"}},
		{"keeps non-da", []string{"workflow", "status"}, []string{"workflow", "status"}},
		{"single da", []string{"da"}, []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := trimDotAgents(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("len mismatch: got %v want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("element %d: got %q want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestRuntimeFuncName(t *testing.T) {
	named := func(*cobra.Command, []string) error { return nil }
	got := runtimeFuncName(named)
	if got == "" {
		t.Fatal("expected non-empty symbol for a real func")
	}
	// Closures defined in this test belong to package globalflagcov, so the
	// "commands." prefix strip path is not exercised here; assert the
	// path-trim (everything up to and including the last '/') ran.
	if got == "" || containsSlash(got) {
		t.Fatalf("expected slash-trimmed symbol, got %q", got)
	}

	// nil interface -> reflect.Value invalid -> "" (the IsValid guard).
	if n := runtimeFuncName(nil); n != "" {
		t.Fatalf("nil func: want empty, got %q", n)
	}
}

func containsSlash(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			return true
		}
	}
	return false
}

func TestWalkRunHandlersCollectsRunAndRunE(t *testing.T) {
	root := &cobra.Command{Use: "da"}
	withRunE := &cobra.Command{Use: "withrune", RunE: func(*cobra.Command, []string) error { return nil }}
	withRun := &cobra.Command{Use: "withrun", Run: func(*cobra.Command, []string) {}}
	noRun := &cobra.Command{Use: "norun"}
	withRunE.AddCommand(noRun)
	root.AddCommand(withRunE, withRun, noRun)

	var recs []runRecord
	walkRunHandlers(root, &recs)

	if len(recs) != 2 {
		t.Fatalf("expected 2 run records (RunE + Run), got %d", len(recs))
	}
	paths := map[string]bool{}
	for _, r := range recs {
		if len(r.path) == 0 {
			t.Fatal("expected non-empty trimmed path")
		}
		paths[r.path[len(r.path)-1]] = true
		if r.handlerName == "" {
			t.Fatalf("expected handler symbol for %v", r.path)
		}
		if r.pc == 0 {
			t.Fatalf("expected non-zero pc for %v", r.path)
		}
	}
	if !paths["withrune"] || !paths["withrun"] {
		t.Fatalf("missing expected commands: %v", paths)
	}
}

func TestReportRowsSortedByPath(t *testing.T) {
	rows, err := Report("../..")
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(rows); i++ {
		if rows[i-1].Path > rows[i].Path {
			t.Fatalf("rows not sorted: %q before %q", rows[i-1].Path, rows[i].Path)
		}
	}
}

func TestReportBadModuleRoot(t *testing.T) {
	if _, err := Report(string([]byte{0})); err == nil {
		t.Fatal("expected error for invalid module root")
	}
}
