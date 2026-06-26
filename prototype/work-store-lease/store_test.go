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

// raceScouts runs n scouts concurrently against the same task via dispatch, and
// returns the per-scout results. All goroutines are released from a single
// barrier so the claim window is maximally contended; each also burns a tiny,
// randomized spin before racing so interleavings vary across iterations and a
// non-deterministic double-pick can't hide behind a fixed schedule. The
// dispatch closure is the production path under test (ScoutTTL/ScoutCAS/Naive).
func raceScouts(n int, dispatch func(scout string) DispatchResult) []DispatchResult {
	var start sync.WaitGroup
	start.Add(1)
	var done sync.WaitGroup
	results := make([]DispatchResult, n)
	for i := range n {
		done.Add(1)
		jitter := rand.Intn(7)
		go func(i, jitter int) {
			defer done.Done()
			start.Wait() // all goroutines block here, then race together
			for j := 0; j < jitter; j++ {
				runtime.Gosched()
			}
			results[i] = dispatch(fmt.Sprintf("scout-%d", i))
		}(i, jitter)
	}
	start.Done()
	done.Wait()
	return results
}

// tally counts dispatched vs backed-off, and fails on any hard error.
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

// seedTask seeds one eligible (pending) task with faithful content.
func seedTask(id string) *Store {
	s := New()
	s.Add(id, []string{"config/v2/*.go"}, "wave-engine task", "p1a", "p1b")
	return s
}

// H-claim: N concurrent Claim calls on ONE task -> exactly ONE succeeds; the
// rest observe it claimed and back off. Run many iterations under -race so a
// missing lock or a check-then-act gap shows up as >1 dispatch.
func TestHClaim_ExactlyOneWinnerTTL(t *testing.T) {
	const scouts = 16
	const iterations = 1200
	now := time.Now()
	for iter := range iterations {
		s := seedTask("t1")
		results := raceScouts(scouts, func(scout string) DispatchResult {
			return ScoutTTL(s, scout, "t1", now, defaultTTL)
		})
		dispatched, backedOff := tally(t, results)
		if dispatched != 1 {
			t.Fatalf("iter %d: expected exactly 1 dispatch, got %d (backed off %d)", iter, dispatched, backedOff)
		}
		if backedOff != scouts-1 {
			t.Fatalf("iter %d: expected %d back-offs, got %d", iter, scouts-1, backedOff)
		}
		final, _ := s.Get("t1")
		if final.Status != StatusInProgress {
			t.Fatalf("iter %d: task not in_progress after race: %s", iter, final.Status)
		}
	}
}

func TestHClaim_ExactlyOneWinnerCAS(t *testing.T) {
	const scouts = 16
	const iterations = 1200
	for iter := range iterations {
		s := seedTask("t1")
		results := raceScouts(scouts, func(scout string) DispatchResult {
			return ScoutCAS(s, scout, "t1")
		})
		dispatched, backedOff := tally(t, results)
		if dispatched != 1 {
			t.Fatalf("iter %d: CAS expected exactly 1 dispatch, got %d (backed off %d)", iter, dispatched, backedOff)
		}
		if backedOff != scouts-1 {
			t.Fatalf("iter %d: CAS expected %d back-offs, got %d", iter, scouts-1, backedOff)
		}
	}
}

// H-claim NEGATIVE CONTROL: the naive no-lease path MUST double-dispatch. If
// this ever stops producing >1 dispatch, the negative control is broken and the
// positive tests above prove nothing. Asserts the storm is REPRODUCED, not
// prevented. Counts iterations that double-dispatched and requires the bug to
// surface in a meaningful fraction of races.
func TestHClaim_NaiveProducesDoubleDispatch(t *testing.T) {
	const scouts = 16
	const iterations = 1200
	doubleDispatched := 0
	for range iterations {
		s := seedTask("t1")
		results := raceScouts(scouts, func(scout string) DispatchResult {
			return ScoutNaive(s, scout, "t1")
		})
		dispatched, _ := tally(t, results)
		if dispatched > 1 {
			doubleDispatched++
		}
	}
	if doubleDispatched == 0 {
		t.Fatalf("NEGATIVE CONTROL FAILED: naive path never double-dispatched across %d races — "+
			"the control is broken, so the lease/CAS proofs are not meaningful", iterations)
	}
	t.Logf("negative control OK: naive path double-dispatched in %d/%d races (the storm)", doubleDispatched, iterations)
}

// H-storm: the motivating regression. Replay the 5xp1c storm — 5 scouts (one
// per wave) race to claim the SAME eligible task "p1c". The lease/CAS arms must
// dispatch exactly once every iteration; the naive arm must reproduce the storm
// (>1 dispatch in at least some races). This is done-criterion #2.
func TestHStorm_NoReDispatch_5xp1c(t *testing.T) {
	now := time.Now()
	t.Run("ttl_prevents", func(t *testing.T) {
		stormExactlyOnce(t, func(s *Store, scout string) DispatchResult {
			return ScoutTTL(s, scout, "p1c", now, defaultTTL)
		})
	})
	t.Run("cas_prevents", func(t *testing.T) {
		stormExactlyOnce(t, func(s *Store, scout string) DispatchResult {
			return ScoutCAS(s, scout, "p1c")
		})
	})
	t.Run("naive_reproduces_storm", func(t *testing.T) {
		stormReproducesStorm(t, func(s *Store, scout string) DispatchResult {
			return ScoutNaive(s, scout, "p1c")
		})
	})
}

// stormExactlyOnce asserts exactly one dispatch per iteration (the fix).
func stormExactlyOnce(t *testing.T, dispatch func(s *Store, scout string) DispatchResult) {
	t.Helper()
	const waves = 5
	const iterations = 1500
	for iter := range iterations {
		s := seedTask("p1c")
		var dispatchCount int32
		results := raceScouts(waves, func(scout string) DispatchResult {
			r := dispatch(s, scout)
			if r.Dispatched {
				atomic.AddInt32(&dispatchCount, 1)
			}
			return r
		})
		dispatched, backedOff := tally(t, results)
		if got := atomic.LoadInt32(&dispatchCount); got != 1 {
			t.Fatalf("iter %d: DOUBLE-DISPATCH — %d scouts dispatched p1c (the storm)", iter, got)
		}
		if dispatched != 1 || backedOff != waves-1 {
			t.Fatalf("iter %d: expected 1 dispatch + %d back-offs, got %d + %d", iter, waves-1, dispatched, backedOff)
		}
	}
}

// stormReproducesStorm asserts the naive arm DOES double-dispatch (the control).
func stormReproducesStorm(t *testing.T, dispatch func(s *Store, scout string) DispatchResult) {
	t.Helper()
	const waves = 5
	const iterations = 1500
	storms := 0
	for range iterations {
		s := seedTask("p1c")
		results := raceScouts(waves, func(scout string) DispatchResult {
			return dispatch(s, scout)
		})
		dispatched, _ := tally(t, results)
		if dispatched > 1 {
			storms++
		}
	}
	if storms == 0 {
		t.Fatalf("NEGATIVE CONTROL FAILED: naive 5xp1c never re-dispatched across %d races", iterations)
	}
	t.Logf("negative control OK: naive 5xp1c re-dispatched in %d/%d races", storms, iterations)
}

// H-ttl: a leaseholder "dies" (never releases). Under TTL the lease expires and
// the task is re-claimable by another scout — no permanent deadlock. The CAS
// arm shows the failure mode: without expiry the task is wedged forever.
func TestHTtl_DeadHolderReclaimableUnderTTL(t *testing.T) {
	const ttl = time.Minute
	t0 := time.Now()
	s := seedTask("t1")

	// scout-a wins, then "dies" — never Release/Complete.
	if got := ScoutTTL(s, "scout-a", "t1", t0, ttl); !got.Dispatched {
		t.Fatalf("scout-a should have won the initial claim: %+v", got)
	}

	// Before TTL expiry, scout-b must back off (lease still live).
	if got := ScoutTTL(s, "scout-b", "t1", t0.Add(30*time.Second), ttl); !got.BackedOff {
		t.Fatalf("scout-b should back off while lease is live, got %+v", got)
	}

	// After TTL expiry, scout-b reclaims — no deadlock.
	after := t0.Add(ttl + time.Second)
	if got := ScoutTTL(s, "scout-b", "t1", after, ttl); !got.Dispatched {
		t.Fatalf("scout-b should reclaim after TTL expiry, got %+v", got)
	}
	final, _ := s.Get("t1")
	if final.Lease.Owner != "scout-b" {
		t.Fatalf("expected scout-b to hold the reclaimed lease, got %q", final.Lease.Owner)
	}
}

// H-ttl failure mode (negative control for the TTL benefit): CAS has no expiry,
// so a dead holder wedges the task permanently. This documents WHY TTL is
// needed; if CAS ever grew an expiry this test would break and force a
// re-evaluation.
func TestHTtl_CASWedgesOnDeadHolder(t *testing.T) {
	s := seedTask("t1")
	if got := ScoutCAS(s, "scout-a", "t1"); !got.Dispatched {
		t.Fatalf("scout-a should win CAS claim: %+v", got)
	}
	// scout-a dies. No amount of waiting helps under CAS: every later scout
	// reads the new version and sees status=in_progress -> permanent back-off.
	for i := range 3 {
		if got := ScoutCAS(s, fmt.Sprintf("scout-b%d", i), "t1"); !got.BackedOff {
			t.Fatalf("CAS dead-holder: scout should stay backed off (wedged), got %+v", got)
		}
	}
	final, _ := s.Get("t1")
	if final.Status != StatusInProgress || final.Lease.Owner != "scout-a" {
		t.Fatalf("CAS task should remain wedged on dead holder, got status=%s owner=%q", final.Status, final.Lease.Owner)
	}
}
