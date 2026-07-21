package workflow

import "sync/atomic"

// gitSpawnCount and resetGitSpawnCount are test/benchmark-only accessors for the
// passive gitSpawnCounter (state.go). They live in a _test.go file so they never
// count as uncovered production code: nothing in production reads the counter,
// only benchmarks snapshot it to prove a hot path's git-spawn cost.

// gitSpawnCount returns the number of git spawns recorded so far.
func gitSpawnCount() int64 { return atomic.LoadInt64(&gitSpawnCounter) }

// resetGitSpawnCount zeroes the git-spawn counter and returns the prior total.
func resetGitSpawnCount() int64 { return atomic.SwapInt64(&gitSpawnCounter, 0) }
