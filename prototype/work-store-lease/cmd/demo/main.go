// Command demo simulates the wave-engine re-dispatch storm FAITHFULLY: five
// scouts, each in its OWN isolated worktree (private local view of TASKS.yaml),
// every one of them seeing p1c as pending. Without a shared store all five
// dispatch (the real 5xp1c storm); with a shared claim exactly one does.
package main

import (
	"fmt"
	"sync"
	"time"

	workstore "proto/work-store-lease"
)

const waves = 5

func seed() workstore.Task {
	return workstore.Task{
		ID:         "p1c",
		Status:     workstore.StatusPending,
		WriteScope: []string{"config/v2/*.go"},
		Notes:      "wave-engine task",
		DependsOn:  []string{"p1a", "p1b"},
	}
}

// worktrees builds n isolated local views (separate checkouts), each seeing p1c
// pending.
func worktrees(n int) []*workstore.LocalView {
	v := make([]*workstore.LocalView, n)
	for i := range n {
		v[i] = workstore.Worktree(fmt.Sprintf("wave-%d", i+1), seed())
	}
	return v
}

func main() {
	fmt.Println("== NAIVE worktree-isolated (the real 5xp1c bug): no shared store ==")
	runWave(nil, func(_ *workstore.Store, v *workstore.LocalView) workstore.DispatchResult {
		return workstore.ScoutNaive(v, "p1c")
	})

	fmt.Println("\n== TTL-lease: 5 isolated worktrees CLAIM the shared store ==")
	now := time.Now()
	runWave(workstore.New(), func(s *workstore.Store, v *workstore.LocalView) workstore.DispatchResult {
		return workstore.ScoutTTL(s, v, "p1c", now, 5*time.Minute)
	})

	fmt.Println("\n== compare-and-set: 5 isolated worktrees CLAIM the shared store ==")
	runWave(workstore.New(), func(s *workstore.Store, v *workstore.LocalView) workstore.DispatchResult {
		return workstore.ScoutCAS(s, v, "p1c")
	})

	fmt.Println("\n== TTL dead-holder reclaim (isolated views) ==")
	demoReclaim()
}

func runWave(s *workstore.Store, dispatch func(*workstore.Store, *workstore.LocalView) workstore.DispatchResult) {
	if s != nil {
		s.Add("p1c", []string{"config/v2/*.go"}, "wave-engine task", "p1a", "p1b")
	}
	views := worktrees(waves)
	var wg sync.WaitGroup
	var start sync.WaitGroup
	start.Add(1)
	results := make([]workstore.DispatchResult, waves)
	for i := range waves {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			start.Wait()
			results[i] = dispatch(s, views[i])
		}(i)
	}
	start.Done()
	wg.Wait()

	dispatched := 0
	for _, r := range results {
		switch {
		case r.Dispatched:
			fmt.Printf("  %-8s DISPATCHED p1c\n", r.Scout)
			dispatched++
		case r.BackedOff:
			fmt.Printf("  %-8s backed off (store rejected claim)\n", r.Scout)
		default:
			fmt.Printf("  %-8s ERROR: %v\n", r.Scout, r.Err)
		}
	}
	if s != nil {
		attempts, grants := s.ClaimStats()
		fmt.Printf("  => dispatches=%d  shared-store attempts=%d grants=%d  (storm prevented=%v)\n",
			dispatched, attempts, grants, dispatched == 1)
	} else {
		fmt.Printf("  => dispatches=%d  (no shared store; STORM=%v — every worktree dispatched)\n",
			dispatched, dispatched > 1)
	}
}

func demoReclaim() {
	s := workstore.New()
	s.Add("p1c", []string{"config/v2/*.go"}, "wave-engine task", "p1a")
	ttl := time.Minute
	t0 := time.Now()
	va := workstore.Worktree("wave-1", seed())
	vb := workstore.Worktree("wave-2", seed())

	r := workstore.ScoutTTL(s, va, "p1c", t0, ttl)
	fmt.Printf("  wave-1 claim: dispatched=%v (then wave-1 dies, never releases)\n", r.Dispatched)

	r = workstore.ScoutTTL(s, vb, "p1c", t0.Add(30*time.Second), ttl)
	fmt.Printf("  wave-2 @ +30s (lease live): backed off=%v\n", r.BackedOff)

	r = workstore.ScoutTTL(s, vb, "p1c", t0.Add(ttl+time.Second), ttl)
	fmt.Printf("  wave-2 @ +TTL  (lease dead): reclaimed=%v -> no deadlock\n", r.Dispatched)
}
