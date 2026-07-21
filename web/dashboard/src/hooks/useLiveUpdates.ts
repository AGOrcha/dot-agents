import { useEffect } from 'react'
import { useQueryClient, type QueryClient } from '@tanstack/react-query'

import {
  type DashboardEventType,
  type ObservabilityTransport,
} from '../api/observabilityTransport'
import { queryKeys } from '../api/queryKeys'
import type { R2DashboardSSEEventDTO } from '../api/types.gen'

export const LIVE_TOPICS = [
  'iteration.scored',
  'session.updated',
  'score.recomputed',
  'rubric.changed',
  'heartbeat',
] as const satisfies readonly DashboardEventType[]

function iterationPrefix(iteration: number): (string | number)[] {
  return queryKeys.iteration(iteration).slice(0, 3) as (string | number)[]
}

export function invalidateForEvent(
  queryClient: QueryClient,
  topic: DashboardEventType,
  event: R2DashboardSSEEventDTO,
): void {
  const payload = event.payload
  const sessionId = 'session_id' in payload ? payload.session_id : undefined
  const iteration = 'iteration' in payload ? payload.iteration : undefined

  switch (topic) {
    case 'iteration.scored':
      queryClient.invalidateQueries({ queryKey: queryKeys.runs })
      if (sessionId) queryClient.invalidateQueries({ queryKey: queryKeys.run(sessionId) })
      if (typeof iteration === 'number') {
        queryClient.invalidateQueries({ queryKey: iterationPrefix(iteration) })
      }
      break

    case 'session.updated':
      queryClient.invalidateQueries({ queryKey: queryKeys.runs })
      if (sessionId) queryClient.invalidateQueries({ queryKey: queryKeys.run(sessionId) })
      break

    case 'score.recomputed':
      if (typeof iteration === 'number') {
        queryClient.invalidateQueries({ queryKey: iterationPrefix(iteration) })
      }
      break

    case 'rubric.changed':
      queryClient.invalidateQueries()
      break

    case 'heartbeat':
      break
  }
}

export interface UseLiveUpdatesOptions {
  enabled?: boolean
}

export function useLiveUpdates(
  transport: ObservabilityTransport,
  options: UseLiveUpdatesOptions = {},
): void {
  const { enabled = true } = options
  const queryClient = useQueryClient()

  useEffect(() => {
    if (!enabled) return

    const connection = transport.connect({
      topics: LIVE_TOPICS,
      handlers: {
        onMessage: (topic, event) => invalidateForEvent(queryClient, topic, event),
        onReconnect: () => {
          queryClient.invalidateQueries()
        },
      },
    })

    return () => connection.close()
  }, [queryClient, enabled, transport])
}
