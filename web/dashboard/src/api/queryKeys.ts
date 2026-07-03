/**
 * TanStack Query key factory for the R2 observability dashboard.
 *
 * Key shape mirrors the API endpoint hierarchy at /api/v1/observability/...
 * (API.md §2). SSE-event invalidation (t04/t11) uses these same keys so
 * invalidateQueries(queryKeys.runs) refetches exactly the right data.
 *
 * Naming convention: endpoint → key
 *   GET /runs                         → queryKeys.runs
 *   GET /runs/:sessionId              → queryKeys.run(sessionId)
 *   GET /runs/:sessionId/iterations   → queryKeys.runIterations(sessionId)
 *   GET /iterations/:n                → queryKeys.iteration(n, iterLogDir?)
 *   GET /rubric                       → queryKeys.rubric
 *   GET /health                       → queryKeys.health
 */

export const queryKeys = {
  /** All runs (list endpoint). Invalidated by iteration.scored / session.updated events. */
  runs: ['observability', 'runs'] as const,

  /** One run's detail (includes per_iteration). Invalidated by session.updated for that sessionId. */
  run: (sessionId: string) => ['observability', 'runs', sessionId] as const,

  /** Iteration list for one run. Invalidated by iteration.scored for that sessionId. */
  runIterations: (sessionId: string) =>
    ['observability', 'runs', sessionId, 'iterations'] as const,

  /**
   * One iteration's full detail.
   * iterLogDir defaults to '' (active root); pass the root path when
   * drilling into a non-active historical root (API.md §1.6).
   * Invalidated by score.recomputed for that iteration number.
   */
  iteration: (n: number, iterLogDir = '') =>
    ['observability', 'iterations', n, iterLogDir] as const,

  /** Active rubric (immutable per process; etag = rubric version). Invalidated by rubric.changed. */
  rubric: ['observability', 'rubric'] as const,

  /** Liveness + counts. Short poll interval recommended (t11). */
  health: ['observability', 'health'] as const,
} as const
