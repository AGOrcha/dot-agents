// Command work-store-projection drives the KG-as-SOT projection prototype.
//
// The PRIMARY experiment routes through a REAL graph (nodes+edges): it ingests
// the real PLAN/TASKS/SLICES into a graphstore under a schema profile, then
// reconstructs the YAML PURELY from graph readback and reports the field loss.
// This answers the actual D1' question (what must the graph store to be a
// lossless projection), not the tautological struct-mirror round-trip.
//
// Usage:
//
//	go run . --graph-loss <plans-root>   # PRIMARY: schema-v4 vs complete field loss
//	go run . --roundtrip <plan-dir>      # struct-level serializer fidelity (one plan)
//	go run . --sweep <plans-root>        # struct-level serializer fidelity (all)
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/AGOrcha/dot-agents/prototype/work-store-projection/projection"
)

func main() {
	roundtrip := flag.String("roundtrip", "", "path to a single plan dir (struct-level serializer fidelity)")
	sweep := flag.String("sweep", "", "path to a plans root (struct-level serializer fidelity)")
	graphLoss := flag.String("graph-loss", "", "path to a plans root (PRIMARY: real-graph field-loss, v4 vs complete)")
	flag.Parse()

	switch {
	case *graphLoss != "":
		if err := runGraphLoss(*graphLoss); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case *roundtrip != "":
		if err := runOne(*roundtrip); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case *sweep != "":
		if err := runSweep(*sweep); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	default:
		flag.Usage()
		os.Exit(2)
	}
}

// runOne round-trips a single plan dir and prints per-file grades + diffs.
func runOne(dir string) error {
	for _, fr := range projection.RoundTripPlanDir(dir) {
		fmt.Printf("== %s ==\n", fr.Path)
		if fr.Err != nil {
			fmt.Printf("  ERROR: %v\n", fr.Err)
			continue
		}
		fmt.Printf("  grade: %s (orig=%dB regen=%dB)\n", fr.Fidelity.Grade, fr.Fidelity.OrigBytes, fr.Fidelity.RegenBytes)
		for _, r := range fr.Fidelity.Reasons {
			fmt.Printf("    - %s\n", r)
		}
		if d := projection.UnifiedDiff(fr.Orig, fr.Regen); d != "" {
			fmt.Println(projection.Indent(d, "  "))
		}
	}
	return nil
}

// runSweep round-trips every plan dir under root and prints a per-plan table
// plus an aggregate. This is the experiment-fidelity view: per-plan, not just
// a single pass/fail.
func runSweep(root string) error {
	names, err := subdirNames(root)
	if err != nil {
		return err
	}
	var tally sweepTally
	fmt.Printf("%-44s %-13s %-15s %s\n", "PLAN/FILE", "GRADE", "BYTES o/r", "REASON")
	for _, n := range names {
		for _, fr := range projection.RoundTripPlanDir(filepath.Join(root, n)) {
			tally.record(n, fr)
		}
	}
	fmt.Printf("\nAGGREGATE: %d byte-identical, %d semantic-equal, %d normalized, %d lossy, %d errors\n",
		tally.ident, tally.sem, tally.norm, tally.lossy, tally.errs)
	return nil
}

// subdirNames returns the sorted immediate subdirectory names of root.
func subdirNames(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// sweepTally accumulates per-grade counts and prints each file's row.
type sweepTally struct{ ident, sem, norm, lossy, errs int }

func (s *sweepTally) record(plan string, fr projection.FileRoundTrip) {
	label := plan + "/" + filepath.Base(fr.Path)
	if fr.Err != nil {
		s.errs++
		fmt.Printf("%-44s %-13s\n", label, "ERR")
		return
	}
	reason := ""
	if len(fr.Fidelity.Reasons) > 0 {
		reason = fr.Fidelity.Reasons[0]
	}
	fmt.Printf("%-44s %-13s %-15s %s\n", label, fr.Fidelity.Grade,
		fmt.Sprintf("%d/%d", fr.Fidelity.OrigBytes, fr.Fidelity.RegenBytes), reason)
	switch fr.Fidelity.Grade {
	case projection.ByteIdentical:
		s.ident++
	case projection.SemanticEqual:
		s.sem++
	case projection.Normalized:
		s.norm++
	case projection.Lossy:
		s.lossy++
	}
}
