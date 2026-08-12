package crgbehavior

import (
	"fmt"
	"path/filepath"

	"github.com/AGOrcha/dot-agents/internal/adapters/builtin/crg"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// LiveBridge drives the legacy Python CRG for one repository: it reads the
// bridge's persisted derived views out of its own store, and issues the live
// impact-radius query the review skills issue. It is the only part of the gate
// that needs the Python runtime; everything else is Go over the Store seam.
type LiveBridge struct {
	bridge   *graphstore.CRGBridge
	repoRoot string
}

// NewLiveBridge binds the legacy bridge for graphRepoRoot — the repository
// whose .code-review-graph/graph.db the comparison reads. It returns
// ErrBridgeUnavailable (not a hard error) when the Python CLI is not installed,
// so callers can SKIP rather than fail on a machine without the legacy side.
func NewLiveBridge(graphRepoRoot string) (*LiveBridge, error) {
	// The legacy graph stores ABSOLUTE paths, so the root the gate normalizes
	// them against must be absolute too; a relative "." would leave every id in
	// the bridge's absolute spelling and diverge from the native id space. An
	// unresolvable working directory falls back to the caller's spelling, where
	// Run's normalization guard reports the mismatch explicitly.
	root := graphRepoRoot
	if abs, err := filepath.Abs(graphRepoRoot); err == nil {
		root = abs
	}
	// NewCRGBridge only returns a binary it has already stat'd (workspace
	// .venv, then PATH), so discovery success IS availability.
	b, err := graphstore.NewCRGBridge(root)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBridgeUnavailable, err)
	}
	return &LiveBridge{bridge: b, repoRoot: root}, nil
}

// Views reads the legacy bridge's persisted graph and derived materialized
// views (flows, communities, risk_index, FTS) out of its own store.
func (l *LiveBridge) Views() (BridgeViews, error) {
	return ReadBridgeViews(l.repoRoot, graphstore.CRGDBPath(l.repoRoot))
}

// ImpactRadius issues the legacy bridge's own blast-radius query for a review
// task's changed files and normalizes its answer into the kg-native id space.
func (l *LiveBridge) ImpactRadius(changedFiles []string, maxDepth, maxResults int) (BridgeImpact, error) {
	res, err := l.bridge.GetImpactRadius(graphstore.ImpactOptions{
		ChangedFiles: changedFiles,
		MaxDepth:     maxDepth,
		MaxResults:   maxResults,
	})
	if err != nil {
		return BridgeImpact{}, err
	}
	return BridgeImpact{
		ChangedIDs:  l.nativeIDs(res.ChangedNodes),
		ImpactedIDs: l.nativeIDs(res.ImpactedNodes),
		Truncated:   res.Truncated,
	}, nil
}

// RunLive binds the legacy Python bridge for graphRepoRoot and executes the
// gate against it. It returns ErrBridgeUnavailable when the legacy side cannot
// be driven on this machine, so callers SKIP rather than report a divergence.
func RunLive(cfg Config, graphRepoRoot string) (Report, error) {
	live, err := NewLiveBridge(graphRepoRoot)
	if err != nil {
		return Report{}, err
	}
	views, err := live.Views()
	if err != nil {
		return Report{}, err
	}
	return Run(cfg, views, live)
}

// nativeIDs maps legacy impact nodes onto the kg-native symbol id space so both
// sides of a comparison are keyed identically.
func (l *LiveBridge) nativeIDs(nodes []graphstore.ImpactNode) []string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, crg.SymbolID(crg.Symbol{
			QualifiedName: relativize(n.QualifiedName, l.repoRoot),
			FilePath:      relativize(n.FilePath, l.repoRoot),
		}))
	}
	return out
}
