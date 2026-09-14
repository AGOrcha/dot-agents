package graphstore

import "testing"

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
