package main

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/AGOrcha/dot-agents/prototype/work-store-projection/graphproj"
	"github.com/AGOrcha/dot-agents/prototype/work-store-projection/projection"
)

// runGraphLoss is the PRIMARY experiment: ingest every real plan into a graph
// under the shipped schema-v4 profile AND a completeness-extended profile,
// reconstruct from readback, and report the per-entity field loss. The v4 loss
// IS the schema-completeness gap; the complete profile is the negative control
// that round-trips.
func runGraphLoss(root string) error {
	names, err := subdirNames(root)
	if err != nil {
		return err
	}
	printFieldCatalog()

	var v4 lossAgg
	var comp lossAgg
	for _, n := range names {
		in, err := graphproj.LoadPlanDir(filepath.Join(root, n))
		if err != nil || in.Plan == nil || in.Tasks == nil {
			continue
		}
		v4.add(graphproj.AnalyzePlan(graphproj.SchemaV4, in.Plan, in.Tasks, in.Slices))
		comp.add(graphproj.AnalyzePlan(graphproj.Complete, in.Plan, in.Tasks, in.Slices))
	}

	fmt.Printf("\n== REAL-GRAPH ROUND-TRIP over %d plans ==\n", v4.plans)
	fmt.Printf("schema-v4 (shipped):   %d field-losses across %d tasks (+%d slice entries) — by field: %s\n",
		len(v4.losses), v4.tasks, v4.slices, topFields(v4.losses))
	fmt.Printf("complete (extended):   %d field-losses — %s\n", len(comp.losses), verdict(len(comp.losses)))
	fmt.Printf("\nSCHEMA-COMPLETENESS GAP = %d losses the shipped graph incurs that a complete graph does not.\n",
		len(v4.losses)-len(comp.losses))
	return nil
}

// printFieldCatalog prints the M-field enumeration with v4 coverage — the
// deliverable "N of M fields lost" table.
func printFieldCatalog() {
	report := func(name string, cat []projection.FieldCov) {
		stored, total, lost := projection.CoverageGap(cat)
		fmt.Printf("%-12s graph stores %d of %d typed fields; DROPS %d: %v\n",
			name, stored, total, len(lost), lost)
	}
	fmt.Println("== FIELD CATALOG (shipped schema-v4 coverage) ==")
	report("PLAN.yaml", projection.PlanFields())
	report("TASKS.yaml", projection.TaskFields())
	report("SLICES.yaml", projection.SliceFields())
}

// lossAgg accumulates field losses across plans for one profile.
type lossAgg struct {
	plans, tasks, slices int
	losses               []graphproj.FieldLoss
}

func (a *lossAgg) add(r graphproj.LossReport) {
	a.plans++
	a.tasks += r.TasksChecked
	a.slices += r.SlicesPresent
	a.losses = append(a.losses, r.Losses...)
}

// topFields tallies losses by field name for the summary line.
func topFields(losses []graphproj.FieldLoss) string {
	by := map[string]int{}
	for _, l := range losses {
		by[l.Field]++
	}
	type kv struct {
		k string
		v int
	}
	var pairs []kv
	for k, v := range by {
		pairs = append(pairs, kv{k, v})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].v != pairs[j].v {
			return pairs[i].v > pairs[j].v
		}
		return pairs[i].k < pairs[j].k
	})
	out := ""
	for _, p := range pairs {
		out += fmt.Sprintf("%s=%d ", p.k, p.v)
	}
	return out
}

func verdict(n int) string {
	if n == 0 {
		return "LOSSLESS round-trip"
	}
	return fmt.Sprintf("still %d losses (incomplete)", n)
}
