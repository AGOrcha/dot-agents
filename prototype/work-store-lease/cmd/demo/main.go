// Command demo simulates the wave-engine re-dispatch storm against the
// prototype WorkStore: five scouts race to claim the same eligible task "p1c"
// and we print who won and who backed off. With the lease/claim in place,
// exactly one scout dispatches — the storm is prevented.
package main

import (
	"fmt"
	"sync"
	"time"

	workstore "proto/work-store-lease"
)

func main() {
	fmt.Println("== NAIVE no-lease (the real 5xp1c bug): 5 scouts race, 400 runs ==")
	storms := 0
	for range 400 {
		if dispatchedNaive() > 1 {
			storms++
		}
	}
	fmt.Printf("  => re-dispatch STORM in %d/400 runs (>1 scout dispatched the same task)\n", storms)
	fmt.Println("  (each storm = a duplicate PR; this is the exact wave-engine failure)")

	fmt.Println("\n== TTL-lease: 5 scouts race for p1c ==")
	runStorm(func(s *workstore.Store, scout string, now time.Time) workstore.DispatchResult {
		return workstore.ScoutTTL(s, scout, "p1c", now, 5*time.Minute)
	})

	fmt.Println("\n== compare-and-set: 5 scouts race for p1c ==")
	runStorm(func(s *workstore.Store, scout string, now time.Time) workstore.DispatchResult {
		return workstore.ScoutCAS(s, scout, "p1c")
	})

	fmt.Println("\n== TTL dead-holder reclaim ==")
	demoReclaim()
}

// dispatchedNaive runs one 5-scout naive race and returns the dispatch count.
func dispatchedNaive() int {
	s := workstore.New()
	s.Add("p1c", []string{"config/v2/*.go"}, "wave-engine task", "p1a")
	var wg sync.WaitGroup
	var start sync.WaitGroup
	start.Add(1)
	results := make([]workstore.DispatchResult, 5)
	for i := range 5 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			start.Wait()
			results[i] = workstore.ScoutNaive(s, fmt.Sprintf("wave-%d", i+1), "p1c")
		}(i)
	}
	start.Done()
	wg.Wait()
	n := 0
	for _, r := range results {
		if r.Dispatched {
			n++
		}
	}
	return n
}

func runStorm(dispatch func(s *workstore.Store, scout string, now time.Time) workstore.DispatchResult) {
	s := workstore.New()
	s.Add("p1c", []string{"config/v2/*.go"}, "wave-engine task", "p1a")
	now := time.Now()

	var wg sync.WaitGroup
	var start sync.WaitGroup
	start.Add(1)
	results := make([]workstore.DispatchResult, 5)
	for i := range 5 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			start.Wait()
			results[i] = dispatch(s, fmt.Sprintf("wave-%d", i+1), now)
		}(i)
	}
	start.Done()
	wg.Wait()

	dispatched := 0
	for _, r := range results {
		switch {
		case r.Dispatched:
			fmt.Printf("  %-8s DISPATCHED p1c (won the claim)\n", r.Scout)
			dispatched++
		case r.BackedOff:
			fmt.Printf("  %-8s backed off (already claimed)\n", r.Scout)
		default:
			fmt.Printf("  %-8s ERROR: %v\n", r.Scout, r.Err)
		}
	}
	fmt.Printf("  => total dispatches: %d (storm prevented = %v)\n", dispatched, dispatched == 1)
}

func demoReclaim() {
	s := workstore.New()
	s.Add("p1c", []string{"config/v2/*.go"}, "wave-engine task", "p1a")
	ttl := time.Minute
	t0 := time.Now()

	r := workstore.ScoutTTL(s, "wave-1", "p1c", t0, ttl)
	fmt.Printf("  wave-1 claim: dispatched=%v (then wave-1 dies, never releases)\n", r.Dispatched)

	r = workstore.ScoutTTL(s, "wave-2", "p1c", t0.Add(30*time.Second), ttl)
	fmt.Printf("  wave-2 @ +30s (lease live): backed off=%v\n", r.BackedOff)

	r = workstore.ScoutTTL(s, "wave-2", "p1c", t0.Add(ttl+time.Second), ttl)
	fmt.Printf("  wave-2 @ +TTL  (lease dead): reclaimed=%v -> no deadlock\n", r.Dispatched)
}
