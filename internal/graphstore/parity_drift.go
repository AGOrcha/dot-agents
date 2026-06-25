package graphstore

import (
	"sort"

	"github.com/AGOrcha/dot-agents/internal/kg/lockfile"
)

// BridgeAdapterName is the migration-only crg-bridge adapter's name as it
// appears in consumer lockfiles' view dependencies (§11.2).
const BridgeAdapterName = "crg-bridge"

// BridgeConsumer is one materialized view that still reads from the crg-bridge
// mirror — a consumer not yet migrated off the legacy bridge (§11.4 gate
// condition 4, surfaced by `workflow drift`).
type BridgeConsumer struct {
	// Adapter is the adapter that owns the view.
	Adapter string
	// View is the materialized view name declaring reads_from [crg-bridge].
	View string
}

// BridgeConsumers scans a lockfile for materialized views whose dependencies
// include the crg-bridge mirror, returning them sorted by (adapter, view).
// This is the §11.4-gate-condition-4 check the `workflow drift` command
// consumes: while any consumer is returned, the bridge must stay active and
// decommissioning (t6) is blocked. An empty result means zero consumers read
// the bridge — the gate condition is satisfied.
func BridgeConsumers(lf *lockfile.Lockfile) []BridgeConsumer {
	if lf == nil {
		return nil
	}
	var out []BridgeConsumer
	for adapterName, ad := range lf.Adapters {
		if ad == nil {
			continue
		}
		for viewName, view := range ad.MaterializedViews {
			if view != nil && viewDependsOnBridge(view) {
				out = append(out, BridgeConsumer{Adapter: adapterName, View: viewName})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Adapter != out[j].Adapter {
			return out[i].Adapter < out[j].Adapter
		}
		return out[i].View < out[j].View
	})
	return out
}

// viewDependsOnBridge reports whether any of a view's dependencies target the
// crg-bridge mirror.
func viewDependsOnBridge(view *lockfile.View) bool {
	for _, dep := range view.DependsOn {
		if dep.Adapter == BridgeAdapterName {
			return true
		}
	}
	return false
}
