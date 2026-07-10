---
scope: "Performance considerations for changed code"
---

# Performance Considerations

## Data Structures and Algorithms

- Chosen data structures match the access pattern (lookup vs. iteration vs. insertion).
- No accidental O(n^2) or worse from nested loops over growing data.
- Large collections use streaming/pagination instead of loading everything into memory.
- Sorting, searching, and filtering use appropriate algorithms for the data size.

## Database and I/O

- Queries are not executed inside loops (N+1 query problem).
- Indexes exist for columns used in WHERE, JOIN, and ORDER BY clauses.
- Batch operations are used instead of row-by-row inserts/updates.
- File and network I/O is buffered where appropriate.
- Connections are pooled and reused, not opened per request.

## Caching

- Expensive computations or fetches that repeat are cached where safe.
- Cache invalidation strategy is clear and correct.
- Cache keys are deterministic and collision-free.

## Concurrency

- Shared mutable state is protected or avoided.
- Locks are held for the minimum necessary duration.
- Async operations that can run in parallel are not serialized unnecessarily.
- Thread pool or connection pool sizes are appropriate for the workload.

## Resource Usage

- No memory leaks from unclosed resources (streams, connections, event listeners).
- Large objects are released when no longer needed.
- Retry logic has backoff and a maximum attempt count.
- Timeouts are set on all external calls.

## What NOT to Flag

- Micro-optimizations that sacrifice readability for negligible gains.
- Premature optimization in code paths that are not hot paths.
- Theoretical concerns without evidence of actual impact.
- Focus on real, measurable problems within the scope of the change.
