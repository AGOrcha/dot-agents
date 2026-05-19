package graphstore

import (
	"context"
	"time"
)

// Path A (spec graphstore-concurrency-contract, decision C-Hybrid) — the
// provider-owned bound + request-timeout enforcement that the published
// Store contract (store.go / CONTRACT.md) promises. This file is the single
// source of truth for the hard caps so the native (SQLite / Postgres BFS)
// and CRG bridge paths enforce *identical* ceilings: the contract's "hard,
// uniform cap across the native and CRG paths" guarantee is one constant
// set, applied through one helper, not re-derived per backend.
//
// Caller semantics (uniform, every provider, every role):
//
//   - A caller-supplied maxNodes/maxDepth/limit is a *requested ceiling*.
//   - A value of 0 (unset) means "use the provider default".
//   - A value above the hard cap is silently clamped down to the hard cap —
//     the provider, not the caller, owns the real limit. A caller can ask
//     for less than the cap but never more.
//   - A negative value is treated as unset (defensive: a negative LIMIT or
//     depth is meaningless and must never widen the query).
//
// These are deliberately generous (real graphs are far smaller) but finite:
// the point is that an adversarial or buggy caller cannot make a single
// invocation fan out unbounded, and that the ceiling is the SAME number
// whether the query went through the Go BFS or the Python CRG subprocess.
const (
	// hardMaxNodes caps the visited frontier of an impact/blast-radius
	// traversal. Beyond this the BFS stops expanding regardless of depth.
	hardMaxNodes = 5000

	// hardMaxDepth caps BFS hop count for impact traversal.
	hardMaxDepth = 12

	// hardSearchLimit caps rows returned by node/note search and direct
	// node/edge reads.
	hardSearchLimit = 2000

	// defaultMaxNodes / defaultMaxDepth / defaultSearchLimit are applied
	// when the caller passes 0 (or a negative) for the respective bound.
	defaultMaxNodes    = 1000
	defaultMaxDepth    = 4
	defaultSearchLimit = 100

	// requestTimeout is the provider-owned deadline for a single graph
	// traversal / query. Callers do NOT wrap Store calls in their own
	// deadline (CONTRACT.md guarantee #2); the provider applies this one.
	// It bounds both the in-process BFS edge load and the CRG Python
	// subprocess (via exec.CommandContext).
	requestTimeout = 30 * time.Second
)

// clampBound normalises a caller-requested ceiling against the provider's
// hard cap. 0 / negative -> def; > hard -> hard; otherwise unchanged. This
// is the single chokepoint every provider routes maxNodes/maxDepth/limit
// through so enforcement is provably uniform.
func clampBound(requested, def, hard int) int {
	if requested <= 0 {
		requested = def
	}
	if requested > hard {
		return hard
	}
	return requested
}

// normalizeTraversalBounds applies clampBound to the impact-radius
// maxNodes/maxDepth pair. Both native backends and the CRG bridge call
// this so a blast-radius query has identical ceilings on every path.
func normalizeTraversalBounds(maxDepth, maxNodes int) (depth, nodes int) {
	depth = clampBound(maxDepth, defaultMaxDepth, hardMaxDepth)
	nodes = clampBound(maxNodes, defaultMaxNodes, hardMaxNodes)
	return depth, nodes
}

// normalizeSearchLimit applies clampBound to a search / direct-read row
// limit. Used by SearchNodes, SearchKGNotes and the CRG ReadNodes/ReadEdges
// direct reads.
func normalizeSearchLimit(limit int) int {
	return clampBound(limit, defaultSearchLimit, hardSearchLimit)
}

// requestContext returns a context carrying the provider-owned request
// timeout, derived from the supplied parent (or context.Background when
// nil). The provider is responsible for calling the returned cancel; the
// timeout, not the caller, bounds the traversal.
func requestContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, requestTimeout)
}
