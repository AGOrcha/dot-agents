package workstore

import (
	"fmt"
	"math/rand"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const defaultTTL = 5 * time.Minute

// eligibleSeed is the shared definition of one pending p1c-style task. Every
// scout's worktree is seeded from an INDEPENDENT clone of it, modeling N
// separate checkouts that all show the task as pending.
func eligibleSeed(id string) Task {
	return Task{
		ID:         id,
		Status:     StatusPending,
		Version:    0,
		WriteScope: []string{"config/v2/*.go"},
		Notes:      "wave-engine task",
		DependsOn:  []string{"p1a", "p1b"},
	}
}

// worktrees builds n isolated local views, each seeded from its own clone of the
// task — i.e. n worktrees that each independently see id as eligible. This is
// the worktree-isolation precondition of the real storm.
func worktrees(n int, id string) []*LocalView {
	views := make([]*LocalView, n)
	seed := eligibleSeed(id)
	for i := range n {
		views[i] = Worktree(fmt.Sprintf("wave-%d", i+1), seed)
	}
	return views
}

// runWave releases n scouts from a single barrier (with randomized Gosched
// jitter so interleavings vary) and collects results. dispatch is the path under
// test; it is handed scout i's OWN isolated view.
func runWave(views []*LocalView, dispatch func(v *LocalView) DispatchResult) []DispatchResult {
	n := len(views)
	var start sync.WaitGroup
	start.Add(1)
	var done sync.WaitGroup
	results := make([]DispatchResult, n)
	for i := range n {
		done.Add(1)
		jitter := rand.Intn(7)
		go func(i, jitter int) {
			defer done.Done()
			start.Wait()
			for j := 0; j < jitter; j++ {
				runtime.Gosched()
			}
			results[i] = dispatch(views[i])
		}(i, jitter)
	}
	start.Done()
	done.Wait()
	return results
}

func tally(t *testing.T, results []DispatchResult) (dispatched, backedOff int) {
	t.Helper()
	for _, r := range results {
		switch {
		case r.Err != nil:
			t.Fatalf("scout %s hit hard error: %v", r.Scout, r.Err)
		case r.Dispatched:
			dispatched++
		case r.BackedOff:
			backedOff++
		default:
			t.Fatalf("scout %s neither dispatched nor backed off: %+v", r.Scout, r)
		}
	}
	return dispatched, backedOff
}

// ---------------------------------------------------------------------------
// H-storm — the motivating regression, modeled FAITHFULLY as worktree isolation
// ---------------------------------------------------------------------------

// TestHStorm_NaiveLocalStaleRead_ReproducesStorm is the NEGATIVE CONTROL and the
// faithful 5xp1c reproduction. Five scouts, each in its OWN worktree, decide
// eligibility and dispatch off their PRIVATE local view with no shared store.
// Every scout sees p1c pending in its own checkout, so ALL FIVE dispatch — a
// 5x re-dispatch storm. This is DETERMINISTIC (5/5 every run): the storm is not
// a lost race, it is the guaranteed consequence of isolated stale reads with no
// shared SOT. That determinism is itself the proof the model is faithful.
func TestHStorm_NaiveLocalStaleRead_ReproducesStorm(t *testing.T) {
	const waves = 5
	const iterations = 500
	for iter := range iterations {
		views := worktrees(waves, "p1c")
		results := runWave(views, func(v *LocalView) DispatchResult {
			return ScoutNaive(v, "p1c") // no shared store at all
		})
		dispatched, backedOff := tally(t, results)
		if dispatched != waves {
			t.Fatalf("iter %d: faithful storm expects ALL %d worktrees to dispatch (no shared SOT), got %d (backed off %d)",
				iter, waves, dispatched, backedOff)
		}
	}
}

// TestHStorm_SharedClaim_PreventsStorm_TTL is the FIX. Same five isolated
// worktrees, same stale local "p1c is pending" view — but now each scout must
// CLAIM the SHARED store before dispatching. Exactly one wins; the other four
// back off. The store's ClaimStats proves the contention was mediated by the
// shared store (5 attempts reached it, 1 granted), not by an external mutex.
func TestHStorm_SharedClaim_PreventsStorm_TTL(t *testing.T) {
	now := time.Now()
	sharedClaimPreventsStorm(t, func(s *Store, v *LocalView) DispatchResult {
		return ScoutTTL(s, v, "p1c", now, defaultTTL)
	})
}

func TestHStorm_SharedClaim_PreventsStorm_CAS(t *testing.T) {
	sharedClaimPreventsStorm(t, func(s *Store, v *LocalView) DispatchResult {
		return ScoutCAS(s, v, "p1c")
	})
}

// sharedClaimPreventsStorm runs the 5-worktree wave through a SHARED store many
// times under -race and asserts: exactly one dispatch, four back-offs, and that
// every scout contended on the store (attempts == waves) while the store granted
// exactly one. The ClaimStats assertion is what proves the fix is the
// shared-store claim, not a trivial in-process lock.
func sharedClaimPreventsStorm(t *testing.T, dispatch func(s *Store, v *LocalView) DispatchResult) {
	t.Helper()
	const waves = 5
	const iterations = 1500
	for iter := range iterations {
		s := New()
		s.Add("p1c", []string{"config/v2/*.go"}, "wave-engine task", "p1a", "p1b")
		views := worktrees(waves, "p1c")
		var dispatchCount int32
		results := runWave(views, func(v *LocalView) DispatchResult {
			r := dispatch(s, v)
			if r.Dispatched {
				atomic.AddInt32(&dispatchCount, 1)
			}
			return r
		})
		dispatched, backedOff := tally(t, results)
		if got := atomic.LoadInt32(&dispatchCount); got != 1 {
			t.Fatalf("iter %d: shared claim must serialize to 1 dispatch, got %d (storm leaked)", iter, got)
		}
		if dispatched != 1 || backedOff != waves-1 {
			t.Fatalf("iter %d: expected 1 dispatch + %d back-offs, got %d + %d", iter, waves-1, dispatched, backedOff)
		}
		attempts, grants := s.ClaimStats()
		if attempts != waves {
			t.Fatalf("iter %d: expected all %d scouts to CONTEND on the shared store, only %d attempted (contention not shared-store-mediated)",
				iter, waves, attempts)
		}
		if grants != 1 {
			t.Fatalf("iter %d: shared store must GRANT exactly 1 claim, granted %d", iter, grants)
		}
	}
}

// ---------------------------------------------------------------------------
// H-claim — N>2 contenders, generalizing the storm
// ---------------------------------------------------------------------------

func TestHClaim_ExactlyOneWinnerTTL(t *testing.T) {
	const scouts = 16
	const iterations = 1200
	now := time.Now()
	for iter := range iterations {
		s := New()
		s.Add("t1", []string{"pkg/a"}, "notes")
		views := worktrees(scouts, "t1")
		results := runWave(views, func(v *LocalView) DispatchResult {
			return ScoutTTL(s, v, "t1", now, defaultTTL)
		})
		dispatched, backedOff := tally(t, results)
		if dispatched != 1 || backedOff != scouts-1 {
			t.Fatalf("iter %d: TTL expected 1 dispatch + %d back-offs, got %d + %d", iter, scouts-1, dispatched, backedOff)
		}
		if _, grants := s.ClaimStats(); grants != 1 {
			t.Fatalf("iter %d: TTL store granted %d (want 1)", iter, grants)
		}
	}
}

func TestHClaim_ExactlyOneWinnerCAS(t *testing.T) {
	const scouts = 16
	const iterations = 1200
	for iter := range iterations {
		s := New()
		s.Add("t1", []string{"pkg/a"}, "notes")
		views := worktrees(scouts, "t1")
		results := runWave(views, func(v *LocalView) DispatchResult {
			return ScoutCAS(s, v, "t1")
		})
		dispatched, backedOff := tally(t, results)
		if dispatched != 1 || backedOff != scouts-1 {
			t.Fatalf("iter %d: CAS expected 1 dispatch + %d back-offs, got %d + %d", iter, scouts-1, dispatched, backedOff)
		}
	}
}

// ---------------------------------------------------------------------------
// H-ttl — dead leaseholder recovery (re-validated against the two-layer model)
// ---------------------------------------------------------------------------

// A leaseholder "dies" (never releases). Under TTL the lease expires and a
// different worktree's scout reclaims it — no permanent deadlock.
func TestHTtl_DeadHolderReclaimableUnderTTL(t *testing.T) {
	const ttl = time.Minute
	t0 := time.Now()
	s := New()
	s.Add("t1", []string{"pkg/a"}, "notes")
	va := Worktree("scout-a", eligibleSeed("t1"))
	vb := Worktree("scout-b", eligibleSeed("t1"))

	if got := ScoutTTL(s, va, "t1", t0, ttl); !got.Dispatched {
		t.Fatalf("scout-a should win initial claim: %+v", got)
	}
	// scout-a dies. scout-b's LOCAL view still says pending (isolation!), so it
	// passes its local pre-filter and reaches the store — the store rejects it
	// while the lease is live.
	if got := ScoutTTL(s, vb, "t1", t0.Add(30*time.Second), ttl); !got.BackedOff {
		t.Fatalf("scout-b should back off at the store while lease is live, got %+v", got)
	}
	// After TTL, the store lets scout-b reclaim. No deadlock.
	if got := ScoutTTL(s, vb, "t1", t0.Add(ttl+time.Second), ttl); !got.Dispatched {
		t.Fatalf("scout-b should reclaim after TTL expiry, got %+v", got)
	}
	final, _ := s.Get("t1")
	if final.Lease.Owner != "scout-b" {
		t.Fatalf("expected scout-b to hold reclaimed lease, got %q", final.Lease.Owner)
	}
}

// CAS has no expiry, so a dead holder wedges the task forever — the failure mode
// that justifies TTL. Re-validated in the two-layer model: scout-b's local view
// stays pending, it reaches the store, but the store never lets it through.
func TestHTtl_CASWedgesOnDeadHolder(t *testing.T) {
	s := New()
	s.Add("t1", []string{"pkg/a"}, "notes")
	va := Worktree("scout-a", eligibleSeed("t1"))
	if got := ScoutCAS(s, va, "t1"); !got.Dispatched {
		t.Fatalf("scout-a should win CAS claim: %+v", got)
	}
	for i := range 3 {
		vb := Worktree(fmt.Sprintf("scout-b%d", i), eligibleSeed("t1"))
		if got := ScoutCAS(s, vb, "t1"); !got.BackedOff {
			t.Fatalf("CAS dead-holder: scout should stay wedged (backed off), got %+v", got)
		}
	}
	final, _ := s.Get("t1")
	if final.Status != StatusInProgress || final.Lease.Owner != "scout-a" {
		t.Fatalf("CAS task should remain wedged on dead holder, got status=%s owner=%q", final.Status, final.Lease.Owner)
	}
}
