/**
 * useLiveUpdates — wires the reconnecting SSE stream (api/eventStream) into the
 * app-root TanStack QueryClient so live iteration-log / score changes invalidate
 * exactly the affected queries (API.md §3.7, plan task t11).
 *
 * Invalidation map (payload key set per schemas/dashboard-event.schema.json —
 * the SSE frame's `data:` is the full event `{ type, seq, ts, payload }`):
 *   iteration.scored  → runs, runs/:sessionId, iterations/:n
 *   session.updated   → runs, runs/:sessionId
 *   score.recomputed  → iterations/:n
 *
 * Recovery: v1 has NO server-side replay (spec D2.2) — a dropped subscriber is
 * disconnected and must refetch. So on every reconnect (any socket open past
 * the first) we invalidate ALL queries to recover anything missed during the
 * outage. eventStream handles the exponential-backoff reconnect cadence itself.
 *
 * The hook is exported for the App root to call once inside the QueryClientProvider;
 * wiring it into App is out of this task's scope.
 */

import { useEffect } from 'react'
import { useQueryClient, type QueryClient } from '@tanstack/react-query'

import { queryKeys } from '../api/queryKeys'
import {
  createEventStream,
  type EventStreamOptions,
} from '../api/eventStream'

/** SSE topics this hook subscribes to and maps to query invalidations. */
export const LIVE_TOPICS = [
  'iteration.scored',
  'session.updated',
  'score.recomputed',
] as const

/** Thin key set carried in an SSE event payload (schemas/dashboard-event.schema.json). */
interface LivePayload {
  session_id?: string
  iteration?: number
}

/**
 * Iteration invalidation prefix: `['observability', 'iterations', n]`, dropping
 * the trailing iterLogDir segment of queryKeys.iteration(n). The SSE payload
 * does not carry the iter-log root, so a prefix match invalidates the iteration
 * across every root variant (active '' and any historical root).
 */
function iterationPrefix(n: number): (string | number)[] {
  return queryKeys.iteration(n).slice(0, 3) as (string | number)[]
}

/** Extract the thin key-set payload from a parsed SSE event (or a bare payload). */
function readPayload(data: unknown): LivePayload {
  if (data && typeof data === 'object') {
    const obj = data as { payload?: unknown }
    const body = 'payload' in obj ? obj.payload : obj
    if (body && typeof body === 'object') return body as LivePayload
  }
  return {}
}

/**
 * Pure topic→invalidation mapping. Exported so it can be unit-tested directly
 * and reused by the hook's onMessage handler.
 */
export function invalidateForEvent(
  queryClient: QueryClient,
  topic: string,
  data: unknown,
): void {
  const { session_id, iteration } = readPayload(data)

  switch (topic) {
    case 'iteration.scored':
      queryClient.invalidateQueries({ queryKey: queryKeys.runs })
      if (session_id) queryClient.invalidateQueries({ queryKey: queryKeys.run(session_id) })
      if (typeof iteration === 'number')
        queryClient.invalidateQueries({ queryKey: iterationPrefix(iteration) })
      break

    case 'session.updated':
      queryClient.invalidateQueries({ queryKey: queryKeys.runs })
      if (session_id) queryClient.invalidateQueries({ queryKey: queryKeys.run(session_id) })
      break

    case 'score.recomputed':
      if (typeof iteration === 'number')
        queryClient.invalidateQueries({ queryKey: iterationPrefix(iteration) })
      break

    default:
      // Unknown / unsubscribed topic — no-op.
      break
  }
}

/** Options for {@link useLiveUpdates}. */
export interface UseLiveUpdatesOptions {
  /** When false, the hook does not open a stream (default true). */
  enabled?: boolean
  /**
   * eventStream overrides (test seams: eventSourceFactory, timers, delays, url).
   * Pass a STABLE reference — a new object each render re-subscribes.
   */
  streamOptions?: Partial<Omit<EventStreamOptions, 'topics' | 'handlers'>>
}

/**
 * Subscribe to the observability SSE stream for the lifetime of the calling
 * component and invalidate affected TanStack queries as events arrive. Call
 * once at the app root, inside the QueryClientProvider.
 */
export function useLiveUpdates(options: UseLiveUpdatesOptions = {}): void {
  const { enabled = true, streamOptions } = options
  const queryClient = useQueryClient()

  useEffect(() => {
    if (!enabled) return

    const stream = createEventStream({
      topics: LIVE_TOPICS,
      handlers: {
        onMessage: (topic, data) => invalidateForEvent(queryClient, topic, data),
        // No replay in v1: recover missed events by refetching everything.
        onReconnect: () => {
          queryClient.invalidateQueries()
        },
      },
      ...streamOptions,
    })

    return () => stream.close()
    // streamOptions is a test-only seam; production callers omit it. It is
    // intentionally excluded from deps (subscription is created once per mount).
  }, [queryClient, enabled])
}
