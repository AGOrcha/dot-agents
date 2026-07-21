import { createElement, type ReactNode } from 'react'
import { describe, it, expect, vi } from 'vitest'
import { renderHook } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'

import {
  type DashboardEventType,
  type ObservabilityTransport,
  type ObservabilityTransportOptions,
} from '../api/observabilityTransport'
import { queryKeys } from '../api/queryKeys'
import type { R2DashboardSSEEventDTO } from '../api/types.gen'
import { invalidateForEvent, LIVE_TOPICS, useLiveUpdates } from './useLiveUpdates'

const ITER_PREFIX = ['observability', 'iterations'] as const

function newClient(): QueryClient {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } })
}

function dashboardEvent(
  type: DashboardEventType,
  payload: R2DashboardSSEEventDTO['payload'],
): R2DashboardSSEEventDTO {
  return { type, seq: 0, payload }
}

describe('invalidateForEvent', () => {
  it('iteration.scored invalidates the run list, run, and iteration', () => {
    const queryClient = newClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue(undefined)

    invalidateForEvent(
      queryClient,
      'iteration.scored',
      dashboardEvent('iteration.scored', { session_id: 's1', iteration: 7 }),
    )

    expect(invalidate).toHaveBeenCalledWith({ queryKey: queryKeys.runs })
    expect(invalidate).toHaveBeenCalledWith({ queryKey: queryKeys.run('s1') })
    expect(invalidate).toHaveBeenCalledWith({ queryKey: [...ITER_PREFIX, 7] })
    expect(invalidate).toHaveBeenCalledTimes(3)
  })

  it('session.updated invalidates the run list and run', () => {
    const queryClient = newClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue(undefined)

    invalidateForEvent(
      queryClient,
      'session.updated',
      dashboardEvent('session.updated', { session_id: 'sX' }),
    )

    expect(invalidate).toHaveBeenCalledWith({ queryKey: queryKeys.runs })
    expect(invalidate).toHaveBeenCalledWith({ queryKey: queryKeys.run('sX') })
    expect(invalidate).toHaveBeenCalledTimes(2)
  })

  it('score.recomputed invalidates the iteration', () => {
    const queryClient = newClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue(undefined)

    invalidateForEvent(
      queryClient,
      'score.recomputed',
      dashboardEvent('score.recomputed', { session_id: 's1', iteration: 12 }),
    )

    expect(invalidate).toHaveBeenCalledTimes(1)
    expect(invalidate).toHaveBeenCalledWith({ queryKey: [...ITER_PREFIX, 12] })
  })

  it('rubric.changed invalidates every query and heartbeat is a no-op', () => {
    const queryClient = newClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue(undefined)

    invalidateForEvent(
      queryClient,
      'rubric.changed',
      dashboardEvent('rubric.changed', { rubric_version: '2.0.0' }),
    )
    expect(invalidate).toHaveBeenCalledWith()

    invalidate.mockClear()
    invalidateForEvent(queryClient, 'heartbeat', dashboardEvent('heartbeat', {}))
    expect(invalidate).not.toHaveBeenCalled()
  })
})

class MockTransport implements ObservabilityTransport {
  options: ObservabilityTransportOptions | undefined
  closed = false

  connect(options: ObservabilityTransportOptions) {
    this.options = options
    return {
      close: () => {
        this.closed = true
      },
      attempt: () => 0,
    }
  }
}

function renderLiveUpdates(queryClient: QueryClient, transport: ObservabilityTransport) {
  const wrapper = ({ children }: { children: ReactNode }) =>
    createElement(QueryClientProvider, { client: queryClient }, children)
  return renderHook(() => useLiveUpdates(transport), { wrapper })
}

describe('useLiveUpdates', () => {
  it('depends on the injected transport and subscribes to all dashboard topics', () => {
    const queryClient = newClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue(undefined)
    const transport = new MockTransport()

    renderLiveUpdates(queryClient, transport)
    expect(transport.options?.topics).toEqual(LIVE_TOPICS)

    transport.options?.handlers.onMessage(
      'iteration.scored',
      dashboardEvent('iteration.scored', { session_id: 's9', iteration: 3 }),
    )

    expect(invalidate).toHaveBeenCalledWith({ queryKey: queryKeys.runs })
    expect(invalidate).toHaveBeenCalledWith({ queryKey: queryKeys.run('s9') })
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: ['observability', 'iterations', 3],
    })
  })

  it('invalidates every query on reconnect', () => {
    const queryClient = newClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue(undefined)
    const transport = new MockTransport()

    renderLiveUpdates(queryClient, transport)
    transport.options?.handlers.onReconnect?.()

    expect(invalidate).toHaveBeenCalledTimes(1)
    expect(invalidate).toHaveBeenCalledWith()
  })

  it('closes the injected connection on unmount', () => {
    const queryClient = newClient()
    const transport = new MockTransport()

    const { unmount } = renderLiveUpdates(queryClient, transport)
    unmount()

    expect(transport.closed).toBe(true)
  })
})
