package workstore

import (
	"sync/atomic"
	"testing"
	"time"
)

// The ANTI-MUTEX PROOF, made executable.
//
// Central claim of this prototype: the storm is prevented by the CLAIM DECISION
// inside the shared store, NOT by the process-local sync.Mutex that serializes
// the goroutines. Describing that in prose is not proof. These tests demonstrate
// it in-run: the SAME mutex is held in every mode (guardFull / guardNone /
// guardCASVersion all take s.mu), so the lock is constant. Only the admission
// decision varies. The contrast — guarded grants exactly 1, the same-mutex
// UNGUARDED store grants all N — is the evidence that the lock is not what
// serializes dispatch.
//
// All run the identical 5-isolated-worktree wave from store_test.go, under
// -race, many iterations.

const antiWaves = 5
const antiIters = 800

// grantsForStore runs the 5-isolated-worktree TTL wave against the given store
// and returns how many claims the store GRANTED (the dispatch count). The store
// is supplied by the caller so the same scenario can run through different guard
// modes that share the identical mutex-based serialization.
func grantsForStoreTTL(s *Store, now time.Time) int32 {
	s.Add("p1c", []string{"config/v2/*.go"}, "wave-engine task", "p1a", "p1b")
	views := worktrees(antiWaves, "p1c")
	var granted int32
	results := runWave(views, func(v *LocalView) DispatchResult {
		r := ScoutTTL(s, v, "p1c", now, defaultTTL)
		if r.Dispatched {
			atomic.AddInt32(&granted, 1)
		}
		return r
	})
	_ = results
	return granted
}

func grantsForStoreCAS(s *Store) int32 {
	s.Add("p1c", []string{"config/v2/*.go"}, "wave-engine task", "p1a", "p1b")
	views := worktrees(antiWaves, "p1c")
	var granted int32
	runWave(views, func(v *LocalView) DispatchResult {
		r := ScoutCAS(s, v, "p1c")
		if r.Dispatched {
			atomic.AddInt32(&granted, 1)
		}
		return r
	})
	return granted
}

// TestAntiMutex_TTL_SameMutex_GuardDecidesNotLock runs guarded vs unguarded TTL
// stores side by side. Both hold the same mutex; only the guard differs.
//   - guarded  (New, guardFull): grants exactly 1   -> storm prevented
//   - unguarded (guardNone):     grants all 5        -> storm LEAKS through the lock
//
// The unguarded leak is the proof: the mutex alone serialized 5 goroutines into
// the store and the store granted every one. So exactly-one comes from the claim
// guard, not the lock.
func TestAntiMutex_TTL_SameMutex_GuardDecidesNotLock(t *testing.T) {
	now := time.Now()
	for iter := range antiIters {
		guardedGrants := grantsForStoreTTL(New(), now)
		if guardedGrants != 1 {
			t.Fatalf("iter %d: GUARDED store must grant exactly 1, granted %d", iter, guardedGrants)
		}
		unguardedGrants := grantsForStoreTTL(newGuarded(guardNone), now)
		if unguardedGrants != antiWaves {
			t.Fatalf("iter %d: UNGUARDED store (same mutex, no claim guard) must leak the storm and grant all %d, granted %d "+
				"(if this is 1, the mutex—not the guard—was serializing, and the central claim is false)",
				iter, antiWaves, unguardedGrants)
		}
		// In-run statement of the contrast: same lock, opposite outcome.
		if guardedGrants == unguardedGrants {
			t.Fatalf("iter %d: guarded and unguarded granted the same (%d) — no contrast, claim not demonstrated", iter, guardedGrants)
		}
		if iter == 0 {
			t.Logf("same sync.Mutex, opposite outcome: guarded grants=%d, unguarded grants=%d (storm leaks through the lock)",
				guardedGrants, unguardedGrants)
		}
	}
}

// TestAntiMutex_CAS_GuardLayers shows the CAS decision, not the lock, serializes:
//   - guardNone        (no checks):      grants all 5  -> storm leaks
//   - guardCASVersion  (version only):   grants 1      -> the version check alone serializes
//   - guardFull        (version+status): grants 1      -> real behaviour
//
// All three hold the same mutex. The version check is load-bearing precisely
// because isolated worktrees carry STALE local versions, so only one scout's
// expected-version matches the store.
func TestAntiMutex_CAS_GuardLayers(t *testing.T) {
	for iter := range antiIters {
		none := grantsForStoreCAS(newGuarded(guardNone))
		if none != antiWaves {
			t.Fatalf("iter %d: CAS guardNone must leak all %d, granted %d", iter, antiWaves, none)
		}
		versionOnly := grantsForStoreCAS(newGuarded(guardCASVersion))
		if versionOnly != 1 {
			t.Fatalf("iter %d: CAS version-guard-only must serialize to 1, granted %d "+
				"(the version check is the serializer, not the mutex)", iter, versionOnly)
		}
		full := grantsForStoreCAS(New())
		if full != 1 {
			t.Fatalf("iter %d: CAS guardFull must grant 1, granted %d", iter, full)
		}
		if iter == 0 {
			t.Logf("CAS same mutex: guardNone grants=%d, versionOnly grants=%d, guardFull grants=%d", none, versionOnly, full)
		}
	}
}
