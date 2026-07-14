/**
 * Tests for the reconnecting SSE wrapper (createEventStream).
 *
 * Uses an injected EventSource mock + injected timers so the reconnect cadence
 * is fully deterministic — no real network, no wall-clock waits.
 */
import { describe, it, expect, vi } from 'vitest'
import {
  createEventStream,
  EVENTS_URL,
  type EventSourceLike,
} from './eventStream'

// --- deterministic EventSource mock ----------------------------------------

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

  // test drivers
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
  ms: number
  cancelled: boolean
  done: boolean
}

function isFakeTimer(token: unknown): token is FakeTimer {
  return typeof token === 'object' && token !== null && 'cancelled' in token
}

function makeHarness() {
  const created: MockEventSource[] = []
  const timers: FakeTimer[] = []
  const factory = (url: string): EventSourceLike => {
    const es = new MockEventSource(url)
    created.push(es)
    return es
  }
  const setTimer = (fn: () => void, ms: number): FakeTimer => {
    const t: FakeTimer = { fn, ms, cancelled: false, done: false }
    timers.push(t)
    return t
  }
  const clearTimer = (token: unknown): void => {
    if (isFakeTimer(token)) token.cancelled = true
  }
  // Run the oldest pending, non-cancelled timer.
  const runNextTimer = (): void => {
    const t = timers.find((x) => !x.cancelled && !x.done)
    if (!t) return
    t.done = true
    t.fn()
  }
  return { created, timers, factory, setTimer, clearTimer, runNextTimer }
}

// ---------------------------------------------------------------------------

describe('createEventStream — messages', () => {
  it('subscribes to every topic and parses JSON payloads', () => {
    const h = makeHarness()
    const onMessage = vi.fn()
    createEventStream({
      topics: ['iteration.scored', 'session.updated'],
      handlers: { onMessage },
      eventSourceFactory: h.factory,
      setTimer: h.setTimer,
      clearTimer: h.clearTimer,
    })

    const es = h.created[0]
    expect([...es.listeners.keys()]).toEqual(['iteration.scored', 'session.updated'])

    es.emit(
      'iteration.scored',
      JSON.stringify({ type: 'iteration.scored', payload: { session_id: 's1', iteration: 4 } }),
    )
    expect(onMessage).toHaveBeenCalledWith('iteration.scored', {
      type: 'iteration.scored',
      payload: { session_id: 's1', iteration: 4 },
    })
  })

  it('passes non-JSON data through unparsed', () => {
    const h = makeHarness()
    const onMessage = vi.fn()
    createEventStream({
      topics: ['session.updated'],
      handlers: { onMessage },
      eventSourceFactory: h.factory,
      setTimer: h.setTimer,
      clearTimer: h.clearTimer,
    })
    h.created[0].emit('session.updated', 'not json')
    expect(onMessage).toHaveBeenCalledWith('session.updated', 'not json')
  })

  it('defaults the URL to the observability events endpoint', () => {
    const h = makeHarness()
    createEventStream({
      topics: [],
      handlers: { onMessage: vi.fn() },
      eventSourceFactory: h.factory,
      setTimer: h.setTimer,
      clearTimer: h.clearTimer,
    })
    expect(h.created[0].url).toBe(EVENTS_URL)
    expect(EVENTS_URL.endsWith('/api/v1/observability/events')).toBe(true)
  })
})

describe('createEventStream — open / reconnect', () => {
  it('fires onOpen on first connect but not onReconnect', () => {
    const h = makeHarness()
    const onOpen = vi.fn()
    const onReconnect = vi.fn()
    createEventStream({
      topics: ['session.updated'],
      handlers: { onMessage: vi.fn(), onOpen, onReconnect },
      eventSourceFactory: h.factory,
      setTimer: h.setTimer,
      clearTimer: h.clearTimer,
    })
    h.created[0].open()
    expect(onOpen).toHaveBeenCalledTimes(1)
    expect(onReconnect).not.toHaveBeenCalled()
  })

  it('reconnects after an error and fires onReconnect on the re-open', () => {
    const h = makeHarness()
    const onReconnect = vi.fn()
    createEventStream({
      topics: ['session.updated'],
      handlers: { onMessage: vi.fn(), onReconnect },
      eventSourceFactory: h.factory,
      setTimer: h.setTimer,
      clearTimer: h.clearTimer,
    })
    const first = h.created[0]
    first.open()
    first.error()
    expect(first.closed).toBe(true) // failed socket torn down
    expect(h.created).toHaveLength(1) // reconnect not yet fired

    h.runNextTimer() // backoff elapses → new connection
    expect(h.created).toHaveLength(2)
    h.created[1].open()
    expect(onReconnect).toHaveBeenCalledTimes(1)
  })
})

describe('createEventStream — backoff schedule', () => {
  it('backs off exponentially, capped at maxDelayMs', () => {
    const h = makeHarness()
    const delays: number[] = []
    createEventStream({
      topics: [],
      handlers: { onMessage: vi.fn(), onError: (d) => delays.push(d) },
      eventSourceFactory: h.factory,
      setTimer: h.setTimer,
      clearTimer: h.clearTimer,
      baseDelayMs: 1000,
      maxDelayMs: 30000,
    })

    h.created[0].open() // establish, resets attempt to 0
    let i = 0
    while (delays.length < 7) {
      h.created[i].error()
      h.runNextTimer() // spawn the next connection to error again
      i++
    }
    expect(delays).toEqual([1000, 2000, 4000, 8000, 16000, 30000, 30000])
  })

  it('resets the backoff after a successful re-open', () => {
    const h = makeHarness()
    const delays: number[] = []
    createEventStream({
      topics: [],
      handlers: { onMessage: vi.fn(), onError: (d) => delays.push(d) },
      eventSourceFactory: h.factory,
      setTimer: h.setTimer,
      clearTimer: h.clearTimer,
      baseDelayMs: 1000,
      maxDelayMs: 30000,
    })
    h.created[0].open()
    h.created[0].error() // 1000
    h.runNextTimer()
    h.created[1].error() // 2000 (still backing off)
    h.runNextTimer()
    h.created[2].open() // success → reset
    h.created[2].error() // back to 1000
    expect(delays).toEqual([1000, 2000, 1000])
  })
})

describe('createEventStream — close', () => {
  it('cancels a pending reconnect and closes the active socket', () => {
    const h = makeHarness()
    const controller = createEventStream({
      topics: [],
      handlers: { onMessage: vi.fn() },
      eventSourceFactory: h.factory,
      setTimer: h.setTimer,
      clearTimer: h.clearTimer,
    })
    h.created[0].open()
    h.created[0].error() // schedules a reconnect timer
    controller.close()

    h.runNextTimer() // cancelled → no new connection
    expect(h.created).toHaveLength(1)
    expect(h.created[0].closed).toBe(true)
  })
})
