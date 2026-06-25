package graphstore

import (
	"math"
	"testing"
)

// Shared test literals (hoisted to consts to avoid S1192 duplication).
const (
	kindFn     = "Function"
	kindType   = "Type"
	kindCalls  = "CALLS"
	kindTested = "TESTED_BY"
)

func snap(adapter string, kind, lang, edge map[string]int, files int) ParitySnapshot {
	total := 0
	for _, v := range kind {
		total += v
	}
	return ParitySnapshot{
		Adapter:     adapter,
		NodesTotal:  total,
		NodesByKind: kind, NodesByLanguage: lang, EdgesByKind: edge, Files: files,
	}
}

func TestCompareSnapshots_Equal(t *testing.T) {
	a := snap("crg", map[string]int{kindFn: 80, kindType: 20},
		map[string]int{"go": 34, "ts": 33, "py": 33}, map[string]int{kindCalls: 79}, 40)
	b := snap(BridgeAdapterName, map[string]int{kindFn: 80, kindType: 20},
		map[string]int{"go": 34, "ts": 33, "py": 33}, map[string]int{kindCalls: 79}, 40)
	rep := CompareSnapshots(a, b, DefaultKindTolerance)
	if !rep.Pass {
		t.Fatalf("equal snapshots should pass; detail=%v", rep.Detail)
	}
}

// TestCompareSnapshots_DroppedKindFailsDespiteTotal is the O6-refinement-A
// false-pass guard: a bootstrap that drops every Type row but doubles Function
// rows keeps the grand total identical (100 vs 100) yet must FAIL per-kind.
func TestCompareSnapshots_DroppedKindFailsDespiteTotal(t *testing.T) {
	a := snap("crg", map[string]int{kindFn: 80, kindType: 20},
		map[string]int{"go": 100}, map[string]int{}, 40)
	b := snap(BridgeAdapterName, map[string]int{kindFn: 100, kindType: 0},
		map[string]int{"go": 100}, map[string]int{}, 40)
	if a.NodesTotal != b.NodesTotal {
		t.Fatalf("precondition: totals should match (%d vs %d)", a.NodesTotal, b.NodesTotal)
	}
	rep := CompareSnapshots(a, b, DefaultKindTolerance)
	if rep.Pass {
		t.Fatal("dropped-Type/doubled-Function within total tolerance must fail per-kind")
	}
}

func TestCompareSnapshots_FileCountExact(t *testing.T) {
	a := snap("crg", map[string]int{kindFn: 10}, map[string]int{"go": 10}, map[string]int{}, 5)
	b := snap(BridgeAdapterName, map[string]int{kindFn: 10}, map[string]int{"go": 10}, map[string]int{}, 6)
	rep := CompareSnapshots(a, b, DefaultKindTolerance)
	if rep.Pass {
		t.Fatal("file count must be exact (O6 refinement A)")
	}
}

func TestCompareSnapshots_WithinTolerancePasses(t *testing.T) {
	// 1000 vs 1005 Functions = 0.5% drift < 1% tolerance.
	a := snap("crg", map[string]int{kindFn: 1000}, map[string]int{"go": 1000}, map[string]int{}, 5)
	b := snap(BridgeAdapterName, map[string]int{kindFn: 1005}, map[string]int{"go": 1005}, map[string]int{}, 5)
	rep := CompareSnapshots(a, b, DefaultKindTolerance)
	if !rep.Pass {
		t.Fatalf("0.5%% drift should pass at 1%% tolerance; detail=%v", rep.Detail)
	}
}

func TestCompareSnapshots_EdgeKindDrift(t *testing.T) {
	a := snap("crg", map[string]int{kindFn: 10}, map[string]int{"go": 10},
		map[string]int{kindCalls: 100, kindTested: 20}, 5)
	b := snap(BridgeAdapterName, map[string]int{kindFn: 10}, map[string]int{"go": 10},
		map[string]int{kindCalls: 100, kindTested: 30}, 5)
	rep := CompareSnapshots(a, b, DefaultKindTolerance)
	if rep.Pass {
		t.Fatal("edges.kind drift 20→30 (50%) must fail")
	}
}

func ut(qn, kind, file string, line int, op UpsertOp) UpsertTuple {
	return UpsertTuple{QualifiedName: qn, Kind: kind, FilePath: file, LineStart: line, Op: op}
}

func TestCompareUpserts_SetEqual(t *testing.T) {
	a := []UpsertTuple{ut("f", kindFn, "a.go", 1, OpInsert), ut("g", kindFn, "b.go", 2, OpUpdate)}
	b := []UpsertTuple{ut("g", kindFn, "b.go", 2, OpUpdate), ut("f", kindFn, "a.go", 1, OpInsert)}
	rep := CompareUpserts(a, b)
	if !rep.Pass {
		t.Fatalf("set-equal upserts (order-independent) should pass; detail=%v", rep.Detail)
	}
}

func TestCompareUpserts_Divergent(t *testing.T) {
	a := []UpsertTuple{ut("f", kindFn, "a.go", 1, OpInsert)}
	b := []UpsertTuple{ut("f", kindFn, "a.go", 1, OpUpdate)} // op differs
	rep := CompareUpserts(a, b)
	if rep.Pass {
		t.Fatal("differing op (insert vs update) must fail the set comparison")
	}
	if len(rep.Detail) != 2 {
		t.Fatalf("expected symmetric-difference detail of 2 (one each side), got %v", rep.Detail)
	}
}

func TestCompareImpactRadius_NodeSetOrderIndependent(t *testing.T) {
	a := []ImpactRow{{NodeID: "x"}, {NodeID: "y"}}
	b := []ImpactRow{{NodeID: "y"}, {NodeID: "x"}}
	rep := CompareImpactRadius(a, b)
	if !rep.Pass {
		t.Fatalf("same node set, different order should pass; detail=%v", rep.Detail)
	}
}

func TestCompareImpactRadius_Divergent(t *testing.T) {
	a := []ImpactRow{{NodeID: "x"}, {NodeID: "y"}}
	b := []ImpactRow{{NodeID: "x"}, {NodeID: "z"}}
	if CompareImpactRadius(a, b).Pass {
		t.Fatal("differing node sets must fail")
	}
}

func TestPartitionAgreement_IdenticalUpToRelabel(t *testing.T) {
	a := map[string]string{"n1": "A", "n2": "A", "n3": "B"}
	b := map[string]string{"n1": "X", "n2": "X", "n3": "Y"} // same partition, relabeled
	got, ok := PartitionAgreement(a, b)
	if !ok || got != 1.0 {
		t.Fatalf("relabeled-identical partition agreement = %v (ok=%v), want 1.0,true", got, ok)
	}
}

func TestPartitionAgreement_Disagreement(t *testing.T) {
	a := map[string]string{"n1": "A", "n2": "A", "n3": "B"}
	b := map[string]string{"n1": "A", "n2": "B", "n3": "B"} // n2 moved
	got, ok := PartitionAgreement(a, b)
	if !ok || got >= 1.0 || got < 0 {
		t.Fatalf("partial agreement should be in [0,1) with ok=true; got %v ok=%v", got, ok)
	}
}

func TestPartitionAgreement_Trivial(t *testing.T) {
	got, ok := PartitionAgreement(map[string]string{"n1": "A"}, map[string]string{"n1": "B"})
	if !ok || got != 1.0 {
		t.Fatalf("single-member partition agreement = %v (ok=%v), want 1.0,true", got, ok)
	}
}

// TestPartitionAgreement_MissingNodeFails is the MEDIUM #5 guard: a node present
// in one partition but absent in the other is a divergence (ok=false), not a
// free 1.0 pass.
func TestPartitionAgreement_MissingNodeFails(t *testing.T) {
	a := map[string]string{"n1": "A", "n2": "A"}
	b := map[string]string{"n1": "A"} // n2 dropped
	if _, ok := PartitionAgreement(a, b); ok {
		t.Fatal("a partition missing a node must report ok=false, not a free pass")
	}
}

func TestSpearmanTau_IdenticalOrder(t *testing.T) {
	a := map[string]float64{"n1": 1, "n2": 2, "n3": 3, "n4": 4}
	b := map[string]float64{"n1": 10, "n2": 20, "n3": 30, "n4": 40} // same order, scaled
	got, ok := SpearmanTau(a, b)
	if !ok || math.Abs(got-1.0) > 1e-9 {
		t.Fatalf("identical rank order tau = %v (ok=%v), want 1.0,true", got, ok)
	}
}

func TestSpearmanTau_ReversedOrder(t *testing.T) {
	a := map[string]float64{"n1": 1, "n2": 2, "n3": 3, "n4": 4}
	b := map[string]float64{"n1": 4, "n2": 3, "n3": 2, "n4": 1}
	got, ok := SpearmanTau(a, b)
	if !ok || math.Abs(got-(-1.0)) > 1e-9 {
		t.Fatalf("reversed rank order tau = %v (ok=%v), want -1.0,true", got, ok)
	}
}

func TestSpearmanTau_AboveThreshold(t *testing.T) {
	// One adjacent swap in a 10-element ranking stays well above 0.85.
	a := map[string]float64{}
	b := map[string]float64{}
	for i := 0; i < 10; i++ {
		a[string(rune('a'+i))] = float64(i)
		b[string(rune('a'+i))] = float64(i)
	}
	b["a"], b["b"] = b["b"], b["a"] // swap top two
	got, ok := SpearmanTau(a, b)
	if !ok || got < DefaultSpearmanTau {
		t.Fatalf("single swap tau = %v (ok=%v), want >= %v", got, ok, DefaultSpearmanTau)
	}
}

func TestSpearmanTau_Ties(t *testing.T) {
	a := map[string]float64{"n1": 1, "n2": 1, "n3": 2}
	b := map[string]float64{"n1": 5, "n2": 5, "n3": 9}
	got, ok := SpearmanTau(a, b)
	if !ok || math.Abs(got-1.0) > 1e-9 {
		t.Fatalf("tie-consistent ranking tau = %v (ok=%v), want 1.0,true", got, ok)
	}
}

func TestSpearmanTau_SingleShared(t *testing.T) {
	got, ok := SpearmanTau(map[string]float64{"n1": 1}, map[string]float64{"n1": 9})
	if !ok || got != 1.0 {
		t.Fatalf("single shared id tau = %v (ok=%v), want 1.0,true", got, ok)
	}
}

func TestSpearmanTau_ZeroVarianceIsDegenerate(t *testing.T) {
	// All-equal scores on one side → zero rank variance → pearson returns 1.0.
	a := map[string]float64{"n1": 5, "n2": 5, "n3": 5}
	b := map[string]float64{"n1": 1, "n2": 2, "n3": 3}
	got, ok := SpearmanTau(a, b)
	if !ok || got != 1.0 {
		t.Fatalf("zero-variance ranking tau = %v (ok=%v), want 1.0,true", got, ok)
	}
}

// TestSpearmanTau_MissingNodeFails is the MEDIUM #5 guard: differing id sets are
// a divergence (ok=false), not a free pass over only the shared ids.
func TestSpearmanTau_MissingNodeFails(t *testing.T) {
	a := map[string]float64{"n1": 1, "n2": 2}
	b := map[string]float64{"n1": 1} // n2 dropped
	if _, ok := SpearmanTau(a, b); ok {
		t.Fatal("differing id sets must report ok=false, not correlate only shared ids")
	}
	if _, ok := SpearmanTau(map[string]float64{"a": 1}, map[string]float64{"b": 2}); ok {
		t.Fatal("disjoint id sets must report ok=false")
	}
}
