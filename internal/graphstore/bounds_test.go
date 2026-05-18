package graphstore

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestClampBound is the core proof that a caller-requested ceiling is
// treated as a request: 0/negative -> default, over-cap -> hard cap,
// in-range -> unchanged. This single helper is the chokepoint every
// provider routes bounds through, so proving it proves uniformity.
func TestClampBound(t *testing.T) {
	cases := []struct {
		name                 string
		requested, def, hard int
		want                 int
	}{
		{"zero uses default", 0, 100, 5000, 100},
		{"negative uses default", -7, 100, 5000, 100},
		{"in range unchanged", 250, 100, 5000, 250},
		{"equal to hard kept", 5000, 100, 5000, 5000},
		{"over hard clamped", 999999, 100, 5000, 5000},
		{"one over hard clamped", 5001, 100, 5000, 5000},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := clampBound(c.requested, c.def, c.hard); got != c.want {
				t.Fatalf("clampBound(%d,%d,%d)=%d want %d",
					c.requested, c.def, c.hard, got, c.want)
			}
		})
	}
}

// TestNormalizeTraversalBounds asserts the impact-radius pair is clamped
// to the package hard caps — the same numbers the CRG path uses.
func TestNormalizeTraversalBounds(t *testing.T) {
	d, n := normalizeTraversalBounds(0, 0)
	if d != defaultMaxDepth || n != defaultMaxNodes {
		t.Fatalf("unset -> (%d,%d) want (%d,%d)", d, n, defaultMaxDepth, defaultMaxNodes)
	}
	d, n = normalizeTraversalBounds(1<<20, 1<<20)
	if d != hardMaxDepth || n != hardMaxNodes {
		t.Fatalf("huge -> (%d,%d) want hard (%d,%d)", d, n, hardMaxDepth, hardMaxNodes)
	}
}

// TestNormalizeSearchLimit asserts a row limit is a clamped ceiling.
func TestNormalizeSearchLimit(t *testing.T) {
	if got := normalizeSearchLimit(0); got != defaultSearchLimit {
		t.Fatalf("0 -> %d want default %d", got, defaultSearchLimit)
	}
	if got := normalizeSearchLimit(10_000_000); got != hardSearchLimit {
		t.Fatalf("huge -> %d want hard %d", got, hardSearchLimit)
	}
	if got := normalizeSearchLimit(50); got != 50 {
		t.Fatalf("in-range 50 -> %d want 50", got)
	}
}

// TestRequestContextHasProviderDeadline proves guarantee #2: the provider
// attaches its own deadline (callers do not). The returned context must
// carry a deadline in the future bounded by requestTimeout.
func TestRequestContextHasProviderDeadline(t *testing.T) {
	ctx, cancel := requestContext(nil)
	defer cancel()
	dl, ok := ctx.Deadline()
	if !ok {
		t.Fatal("requestContext produced a context with no deadline")
	}
	remaining := time.Until(dl)
	if remaining <= 0 || remaining > requestTimeout+time.Second {
		t.Fatalf("deadline remaining %v not within (0, %v]", remaining, requestTimeout)
	}
}

// TestRequestContextHonorsParentCancel proves the provider deadline does
// not sever caller cancellation: a cancelled parent cancels the request.
func TestRequestContextHonorsParentCancel(t *testing.T) {
	parent, pcancel := context.WithCancel(context.Background())
	ctx, cancel := requestContext(parent)
	defer cancel()
	pcancel()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("request context did not observe parent cancellation")
	}
}

// TestBFSHardCapNeverOvershoots is the regression proof for the spec's
// exact complaint ("bounds advisory, overshoot by a frontier"). With a
// star graph whose center fans out to far more than maxNodes neighbors in
// a single hop, the impacted set must be EXACTLY capped at maxNodes — not
// maxNodes + a leftover frontier.
func TestBFSHardCapNeverOvershoots(t *testing.T) {
	const fanout = 500
	const cap = 10

	seeds := map[string]bool{"center": true}
	fwd := map[string][]string{}
	rev := map[string][]string{}
	for i := 0; i < fanout; i++ {
		leaf := fmt.Sprintf("leaf%d", i)
		fwd["center"] = append(fwd["center"], leaf)
		rev[leaf] = append(rev[leaf], "center")
	}

	impacted := bfsImpacted(seeds, fwd, rev, 4, cap)
	if len(impacted) > cap {
		t.Fatalf("hard cap overshot: got %d impacted, cap was %d", len(impacted), cap)
	}
	if len(impacted) != cap {
		t.Fatalf("expected exactly %d impacted (cap reached), got %d", cap, len(impacted))
	}
}

// TestBFSDepthBound proves maxDepth still bounds hop count after the
// hard-cap rework: a deep chain only expands maxDepth hops from the seed.
func TestBFSDepthBound(t *testing.T) {
	seeds := map[string]bool{"n0": true}
	fwd := map[string][]string{}
	rev := map[string][]string{}
	const chain = 20
	for i := 0; i < chain; i++ {
		from := fmt.Sprintf("n%d", i)
		to := fmt.Sprintf("n%d", i+1)
		fwd[from] = []string{to}
		rev[to] = []string{from}
	}

	impacted := bfsImpacted(seeds, fwd, rev, 3, hardMaxNodes)
	if len(impacted) != 3 {
		t.Fatalf("depth 3 from a chain should reach 3 nodes, got %d", len(impacted))
	}
}
