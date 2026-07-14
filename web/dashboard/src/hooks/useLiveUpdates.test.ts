/**
 * Tests for useLiveUpdates — the SSE→TanStack-Query invalidation wiring (t11).
 *
 * Two layers:
 *  1. invalidateForEvent(): pure topic→invalidateQueries mapping, asserted
 *     against a real QueryClient with a spied invalidateQueries.
 *  2. useLiveUpdates(): the hook opens a stream (injected mock EventSource +
 *     timers), routes events through the mapping, invalidates ALL queries on
 *     reconnect, and closes the stream on unmount.
 */
import { createElement, type ReactNode } from 'react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { renderHook } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'

import { queryKeys } from '../api/queryKeys'
import type { EventSourceLike } from '../api/eventStream'
import { invalidateForEvent, useLiveUpdates } from './useLiveUpdates'

const ITER_PREFIX = ['observability', 'iterations'] as const

function newClient(): QueryClient {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } })
}

// ---------------------------------------------------------------------------
// invalidateForEvent — pure mapping
// ---------------------------------------------------------------------------

describe('invalidateForEvent', () => {
  it('iteration.scored → runs, run(sid), iterations/:n prefix', () => {
    const qc = newClient()
    const spy = vi.spyOn(qc, 'invalidateQueries').mockResolvedValue(undefined)

    invalidateForEvent(qc, 'iteration.scored', { payload: { session_id: 's1', iteration: 7 } })

    expect(spy).toHaveBeenCalledWith({ queryKey: queryKeys.runs })
    expect(spy).toHaveBeenCalledWith({ queryKey: queryKeys.run('s1') })
    expect(spy).toHaveBeenCalledWith({ queryKey: [...ITER_PREFIX, 7] })
    expect(spy).toHaveBeenCalledTimes(3)
  })

  it('session.updated → runs + run(sid) only', () => {
    const qc = newClient()
    const spy = vi.spyOn(qc, 'invalidateQueries').mockResolvedValue(undefined)

    invalidateForEvent(qc, 'session.updated', { payload: { session_id: 'sX' } })

    expect(spy).toHaveBeenCalledWith({ queryKey: queryKeys.runs })
    expect(spy).toHaveBeenCalledWith({ queryKey: queryKeys.run('sX') })
    expect(spy).toHaveBeenCalledTimes(2)
  })

  it('score.recomputed → iterations/:n prefix only', () => {
    const qc = newClient()
    const spy = vi.spyOn(qc, 'invalidateQueries').mockResolvedValue(undefined)

    invalidateForEvent(qc, 'score.recomputed', { payload: { session_id: 's1', iteration: 12 } })

    expect(spy).toHaveBeenCalledTimes(1)
    expect(spy).toHaveBeenCalledWith({ queryKey: [...ITER_PREFIX, 12] })
  })

  it('accepts a bare payload (no wrapping event envelope)', () => {
    const qc = newClient()
    const spy = vi.spyOn(qc, 'invalidateQueries').mockResolvedValue(undefined)

    invalidateForEvent(qc, 'session.updated', { session_id: 'bare' })

    expect(spy).toHaveBeenCalledWith({ queryKey: queryKeys.run('bare') })
  })

  it('skips run(sid) when session_id is absent', () => {
    const qc = newClient()
    const spy = vi.spyOn(qc, 'invalidateQueries').mockResolvedValue(undefined)

    invalidateForEvent(qc, 'session.updated', { payload: {} })

    expect(spy).toHaveBeenCalledTimes(1)
    expect(spy).toHaveBeenCalledWith({ queryKey: queryKeys.runs })
  })

  it('ignores unknown topics', () => {
    const qc = newClient()
    const spy = vi.spyOn(qc, 'invalidateQueries').mockResolvedValue(undefined)

    invalidateForEvent(qc, 'rubric.changed', { payload: { rubric_version: '2.0.0' } })

    expect(spy).not.toHaveBeenCalled()
  })
})

// ---------------------------------------------------------------------------
// useLiveUpdates — hook integration
// ---------------------------------------------------------------------------

class MockEventSource implements EventSourceLike {
  onopen: ((event: Event) => void) | null = null
  onerror: ((event: Event) => void) | null = null
  closed = false
  readonly listeners = new Map<string, (event: MessageEvent) => void>()

  constructor(readonly url: string) {}

  addEventListener(type: string, listener: (event: MessageEvent) => void): void {
    this.listeners.set(type, listener)
  }
  close(): void {
    this.closed = true
  }
  emit(topic: string, data: unknown): void {
    this.listeners.get(topic)?.(new MessageEvent('message', { data }))
  }
  open(): void {
    this.onopen?.(new Event('open'))
  }
  error(): void {
    this.onerror?.(new Event('error'))
  }
}

interface FakeTimer {
  fn: () => void
  cancelled: boolean
  done: boolean
}

function isFakeTimer(token: unknown): token is FakeTimer {
  return typeof token === 'object' && token !== null && 'cancelled' in token
}

let created: MockEventSource[]
let timers: FakeTimer[]

function streamOptions() {
  return {
    eventSourceFactory: (url: string): EventSourceLike => {
      const es = new MockEventSource(url)
      created.push(es)
      return es
    },
    setTimer: (fn: () => void): FakeTimer => {
      const t: FakeTimer = { fn, cancelled: false, done: false }
      timers.push(t)
      return t
    },
    clearTimer: (token: unknown): void => {
      if (isFakeTimer(token)) token.cancelled = true
    },
  }
}

function runNextTimer(): void {
  const t = timers.find((x) => !x.cancelled && !x.done)
  if (!t) return
  t.done = true
  t.fn()
}

beforeEach(() => {
  created = []
  timers = []
})

function renderLiveUpdates(qc: QueryClient, opts: ReturnType<typeof streamOptions>) {
  const wrapper = ({ children }: { children: ReactNode }) =>
    createElement(QueryClientProvider, { client: qc }, children)
  return renderHook(() => useLiveUpdates({ streamOptions: opts }), { wrapper })
}

describe('useLiveUpdates hook', () => {
  it('opens a stream and routes events into query invalidation', () => {
    const qc = newClient()
    const spy = vi.spyOn(qc, 'invalidateQueries').mockResolvedValue(undefined)

    renderLiveUpdates(qc, streamOptions())
    expect(created).toHaveLength(1)

    created[0].open()
    created[0].emit(
      'iteration.scored',
      JSON.stringify({ type: 'iteration.scored', payload: { session_id: 's9', iteration: 3 } }),
    )

    expect(spy).toHaveBeenCalledWith({ queryKey: queryKeys.runs })
    expect(spy).toHaveBeenCalledWith({ queryKey: queryKeys.run('s9') })
    expect(spy).toHaveBeenCalledWith({ queryKey: ['observability', 'iterations', 3] })
  })

  it('invalidates ALL queries on reconnect', () => {
    const qc = newClient()
    const spy = vi.spyOn(qc, 'invalidateQueries').mockResolvedValue(undefined)

    renderLiveUpdates(qc, streamOptions())
    created[0].open()
    created[0].error() // drop → schedule reconnect
    runNextTimer() // backoff elapses → new socket

    spy.mockClear()
    created[1].open() // reconnect
    expect(spy).toHaveBeenCalledTimes(1)
    expect(spy).toHaveBeenCalledWith() // no args → invalidate everything
  })

  it('closes the stream on unmount', () => {
    const qc = newClient()
    vi.spyOn(qc, 'invalidateQueries').mockResolvedValue(undefined)

    const { unmount } = renderLiveUpdates(qc, streamOptions())
    created[0].open()
    unmount()
    expect(created[0].closed).toBe(true)
  })
})
