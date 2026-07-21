package store

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/scoring"
)

// store_bench_test.go — measure-first baselines for the read-through DiskStore
// projection hot paths (Phase 0 of the perf-optimization plan). The seed builds
// a realistic runs×iterations sidecar tree (each iteration: an iter-N.yaml
// record + its iter-N.score.yaml sidecar, plus one session-<id>.score.yaml per
// session), mirroring the fixture shape the *_test.go helpers write, scaled to
// N sessions × M iterations. Each timed call runs against a fresh DiskStore so
// the full readDirState + loadRoot parse + projection cost is measured (the
// cold path an optimization targets), not a warm cache hit.

// benchStore builds a DiskStore whose logger discards below-error output.
func benchStore(root string) *DiskStore {
	lvl := &slog.HandlerOptions{Level: slog.LevelError + 1}
	return New([]string{root}, WithLogger(slog.New(slog.NewTextHandler(os.Stderr, lvl))))
}

// writeBenchIter writes a v2 iteration record with an agent block and token
// telemetry, matching writeV2Iter's shape.
func writeBenchIter(tb testing.TB, dir string, n int, sid, harness string) {
	tb.Helper()
	var b bytes.Buffer
	fmt.Fprintf(&b, "schema_version: 2\niteration: %d\ndate: \"2026-05-01\"\nwave: \"w1\"\ntask_id: \"task-%d\"\ncommit: \"commit%d\"\nfiles_changed: %d\nlines_added: %d\nlines_removed: %d\n", n, n, n, n, n*10, n*2)
	fmt.Fprintf(&b, "agent:\n  session_id: \"%s\"\n  harness: \"%s\"\n  model: \"m-%s\"\n", sid, harness, harness)
	fmt.Fprintf(&b, "impl:\n  summary: \"impl %d\"\n  retries: %d\n", n, n)
	b.WriteString("session_tokens:\n  input_tokens: 100\n  output_tokens: 200\n  cache_read_tokens: 900\n  cache_creation_tokens: 100\n  cache_hit_rate: 0.75\n")
	if err := os.WriteFile(fmt.Sprintf("%s/iter-%d.yaml", dir, n), b.Bytes(), 0o644); err != nil {
		tb.Fatalf("write iter-%d: %v", n, err)
	}
}

// writeBenchIterScore writes an iter-N.score.yaml sidecar via the production
// scoring writer.
func writeBenchIterScore(tb testing.TB, dir string, n int) {
	tb.Helper()
	sc := scoring.Score{
		Iteration:     n,
		RubricVersion: scoring.RubricVersion,
		Value:         0.8,
		Scored:        true,
		Band:          "good",
		Breakdown: []scoring.SignalContribution{{
			Signal:          scoring.SignalLanded,
			Label:           "Landed on master",
			Present:         true,
			SubScore:        1,
			NominalWeight:   0.2,
			EffectiveWeight: 1,
			Contribution:    0.8,
		}},
	}
	if _, err := scoring.WriteIterationScore(dir, sc); err != nil {
		tb.Fatalf("write iter score %d: %v", n, err)
	}
}

// seedRuns builds a temp root with sessions×itersPerSession iterations, each
// fully scored, and one session sidecar per session. Iteration numbers are
// globally unique within the root (the iter-*.yaml namespace is shared).
func seedRuns(tb testing.TB, sessions, itersPerSession int) string {
	tb.Helper()
	dir := tb.TempDir()
	n := 0
	for s := range sessions {
		sid := fmt.Sprintf("sess-%d", s)
		iters := make([]int, 0, itersPerSession)
		refs := make([]scoring.SessionIterRef, 0, itersPerSession)
		for range itersPerSession {
			n++
			writeBenchIter(tb, dir, n, sid, "claude-code")
			writeBenchIterScore(tb, dir, n)
			iters = append(iters, n)
			refs = append(refs, scoring.SessionIterRef{Iteration: n, Scored: true, Value: 0.8, Band: "good"})
		}
		ss := scoring.SessionScore{
			SessionID:     sid,
			RubricVersion: scoring.RubricVersion,
			Iterations:    iters,
			Scored:        true,
			Value:         0.8,
			Band:          "good",
			PerIteration:  refs,
		}
		if _, err := scoring.WriteSessionScore(dir, ss); err != nil {
			tb.Fatalf("write session score %s: %v", sid, err)
		}
	}
	return dir
}

// benchScales is the runs×iterations sweep the store benchmarks parametrize
// over: a small, a mid, and a large sidecar tree.
var benchScales = []struct {
	name     string
	sessions int
	iters    int
}{
	{"10x5", 10, 5},
	{"50x10", 50, 10},
	{"100x20", 100, 20},
}

func BenchmarkDiskStoreListRuns(b *testing.B) {
	ctx := context.Background()
	for _, sc := range benchScales {
		root := seedRuns(b, sc.sessions, sc.iters)
		b.Run(sc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := benchStore(root).ListRuns(ctx, RunFilter{}); err != nil {
					b.Fatalf("ListRuns: %v", err)
				}
			}
		})
	}
}

func BenchmarkDiskStoreGetRun(b *testing.B) {
	ctx := context.Background()
	for _, sc := range benchScales {
		root := seedRuns(b, sc.sessions, sc.iters)
		sid := fmt.Sprintf("sess-%d", sc.sessions/2)
		b.Run(sc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := benchStore(root).GetRun(ctx, sid); err != nil {
					b.Fatalf("GetRun: %v", err)
				}
			}
		})
	}
}

func BenchmarkDiskStoreListIterations(b *testing.B) {
	ctx := context.Background()
	for _, sc := range benchScales {
		root := seedRuns(b, sc.sessions, sc.iters)
		sid := fmt.Sprintf("sess-%d", sc.sessions/2)
		b.Run(sc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := benchStore(root).ListIterations(ctx, sid); err != nil {
					b.Fatalf("ListIterations: %v", err)
				}
			}
		})
	}
}

func BenchmarkDiskStoreHealth(b *testing.B) {
	ctx := context.Background()
	for _, sc := range benchScales {
		root := seedRuns(b, sc.sessions, sc.iters)
		b.Run(sc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := benchStore(root).Health(ctx); err != nil {
					b.Fatalf("Health: %v", err)
				}
			}
		})
	}
}
