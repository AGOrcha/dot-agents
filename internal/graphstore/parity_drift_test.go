package graphstore

import (
	"testing"

	"github.com/AGOrcha/dot-agents/internal/kg/lockfile"
)

func TestBridgeConsumers_FlagsReadsFromBridge(t *testing.T) {
	lf := lockfile.New()
	lf.Adapters["compliance"] = &lockfile.Adapter{
		MaterializedViews: map[string]*lockfile.View{
			"controls_evidence": {DependsOn: []lockfile.ViewDependency{
				{Adapter: "crg"}, {Adapter: BridgeAdapterName},
			}},
			"pure_crg_view": {DependsOn: []lockfile.ViewDependency{{Adapter: "crg"}}},
		},
	}
	got := BridgeConsumers(lf)
	if len(got) != 1 {
		t.Fatalf("consumers = %v, want exactly the bridge-reading view", got)
	}
	if got[0].Adapter != "compliance" || got[0].View != "controls_evidence" {
		t.Fatalf("unexpected consumer %+v", got[0])
	}
}

func TestBridgeConsumers_ZeroWhenMigrated(t *testing.T) {
	lf := lockfile.New()
	lf.Adapters["research"] = &lockfile.Adapter{
		MaterializedViews: map[string]*lockfile.View{
			"v": {DependsOn: []lockfile.ViewDependency{{Adapter: "crg"}}},
		},
	}
	if got := BridgeConsumers(lf); len(got) != 0 {
		t.Fatalf("fully-migrated lockfile should report zero consumers; got %v", got)
	}
}

func TestBridgeConsumers_SortedDeterministic(t *testing.T) {
	lf := lockfile.New()
	lf.Adapters["zeta"] = &lockfile.Adapter{MaterializedViews: map[string]*lockfile.View{
		"vb": {DependsOn: []lockfile.ViewDependency{{Adapter: BridgeAdapterName}}},
		"va": {DependsOn: []lockfile.ViewDependency{{Adapter: BridgeAdapterName}}},
	}}
	lf.Adapters["alpha"] = &lockfile.Adapter{MaterializedViews: map[string]*lockfile.View{
		"v": {DependsOn: []lockfile.ViewDependency{{Adapter: BridgeAdapterName}}},
	}}
	got := BridgeConsumers(lf)
	if len(got) != 3 {
		t.Fatalf("want 3 consumers, got %d (%v)", len(got), got)
	}
	if got[0].Adapter != "alpha" || got[1].View != "va" || got[2].View != "vb" {
		t.Fatalf("not sorted by (adapter, view): %+v", got)
	}
}

func TestBridgeConsumers_NilSafe(t *testing.T) {
	if got := BridgeConsumers(nil); got != nil {
		t.Fatalf("nil lockfile should yield nil, got %v", got)
	}
}

func TestBridgeConsumers_SkipsNilAdapterAndView(t *testing.T) {
	lf := lockfile.New()
	lf.Adapters["nilad"] = nil
	lf.Adapters["hasnil"] = &lockfile.Adapter{MaterializedViews: map[string]*lockfile.View{
		"nilview":  nil,
		"realview": {DependsOn: []lockfile.ViewDependency{{Adapter: BridgeAdapterName}}},
	}}
	got := BridgeConsumers(lf)
	if len(got) != 1 || got[0].View != "realview" {
		t.Fatalf("nil adapter/view entries must be skipped; got %v", got)
	}
}
