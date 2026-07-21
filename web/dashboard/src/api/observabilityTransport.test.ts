import { describe, expect, it, vi } from 'vitest'

import type { EventSourceLike } from './eventStream'
import {
  createSseObservabilityTransport,
  createWebSocketObservabilityTransport,
  type WebSocketLike,
} from './observabilityTransport'

interface FakeTimer {
  fn: () => void
  ms: number
  cancelled: boolean
  done: boolean
}

function timerHarness() {
  const timers: FakeTimer[] = []
  return {
    timers,
    setTimer(fn: () => void, ms: number): FakeTimer {
      const timer = { fn, ms, cancelled: false, done: false }
      timers.push(timer)
      return timer
    },
    clearTimer(token: unknown): void {
      if (typeof token === 'object' && token !== null && 'cancelled' in token) {
        ;(token as FakeTimer).cancelled = true
      }
    },
    runNext(): void {
      const timer = timers.find((candidate) => !candidate.cancelled && !candidate.done)
      if (timer) {
        timer.done = true
        timer.fn()
      }
    },
  }
}

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

  open(): void {
    this.onopen?.(new Event('open'))
  }

  emit(topic: string, value: unknown): void {
    this.listeners.get(topic)?.(new MessageEvent('message', { data: value }))
  }
}

class MockWebSocket implements WebSocketLike {
  closed: { code?: number; reason?: string } | undefined
  readonly listeners = new Map<string, (event: Event) => void>()

  constructor(readonly url: string) {}

  addEventListener(type: 'open' | 'error' | 'close', listener: (event: Event) => void): void
  addEventListener(type: 'message', listener: (event: MessageEvent) => void): void
  addEventListener(
    type: string,
    listener: ((event: Event) => void) | ((event: MessageEvent) => void),
  ): void {
    this.listeners.set(type, listener as (event: Event) => void)
  }

  close(code?: number, reason?: string): void {
    this.closed = { code, reason }
  }

  open(): void {
    this.listeners.get('open')?.(new Event('open'))
  }

  emit(value: unknown): void {
    this.listeners.get('message')?.(new MessageEvent('message', { data: value }))
  }

  error(): void {
    this.listeners.get('error')?.(new Event('error'))
  }
}

const validEvent = JSON.stringify({
  type: 'session.updated',
  seq: 0,
  ts: '2026-07-20T21:00:00Z',
  payload: { session_id: 'session-7' },
})

describe('createSseObservabilityTransport', () => {
  it('delivers one schema-valid dashboard event to onMessage', () => {
    const sources: MockEventSource[] = []
    const timers = timerHarness()
    const onMessage = vi.fn()
    const transport = createSseObservabilityTransport({
      eventSourceFactory: (url) => {
        const source = new MockEventSource(url)
        sources.push(source)
        return source
      },
      setTimer: timers.setTimer,
      clearTimer: timers.clearTimer,
    })

    transport.connect({ topics: ['session.updated'], handlers: { onMessage } })
    sources[0].emit('session.updated', validEvent)

    expect(onMessage).toHaveBeenCalledWith('session.updated', {
      type: 'session.updated',
      seq: 0,
      ts: '2026-07-20T21:00:00Z',
      payload: { session_id: 'session-7' },
    })
  })

  it.each([
    ['invalid JSON', 'not-json'],
    [
      'schema-invalid JSON',
      JSON.stringify({ type: 'session.updated', seq: 'bad', payload: { session_id: 's1' } }),
    ],
  ])('turns %s into an error and post-open reconnect', (_name, frame) => {
    const sources: MockEventSource[] = []
    const timers = timerHarness()
    const onMessage = vi.fn()
    const onError = vi.fn()
    const onReconnect = vi.fn()
    const transport = createSseObservabilityTransport({
      eventSourceFactory: (url) => {
        const source = new MockEventSource(url)
        sources.push(source)
        return source
      },
      setTimer: timers.setTimer,
      clearTimer: timers.clearTimer,
    })

    const connection = transport.connect({
      topics: ['session.updated'],
      handlers: { onMessage, onError, onReconnect },
    })
    sources[0].open()
    sources[0].emit('session.updated', frame)

    expect(onMessage).not.toHaveBeenCalled()
    expect(sources[0].closed).toBe(true)
    expect(onError).toHaveBeenCalledWith(1000)
    expect(connection.attempt()).toBe(1)

    timers.runNext()
    sources[1].open()
    expect(onReconnect).toHaveBeenCalledTimes(1)
    expect(connection.attempt()).toBe(0)
  })
})

describe('createWebSocketObservabilityTransport', () => {
  function socketHarness() {
    const sockets: MockWebSocket[] = []
    const timers = timerHarness()
    const transport = createWebSocketObservabilityTransport({
      url: 'https://obs.agorcha.dev/api/v1/observability/events',
      webSocketFactory: (url) => {
        const socket = new MockWebSocket(url)
        sockets.push(socket)
        return socket
      },
      setTimer: timers.setTimer,
      clearTimer: timers.clearTimer,
    })
    return { sockets, timers, transport }
  }

  it('uses wss and delivers one schema-valid dashboard event in frame order', () => {
    const { sockets, transport } = socketHarness()
    const onMessage = vi.fn()

    transport.connect({ topics: ['session.updated'], handlers: { onMessage } })
    expect(sockets[0].url).toBe('wss://obs.agorcha.dev/api/v1/observability/events')

    sockets[0].emit(validEvent)
    sockets[0].emit(
      JSON.stringify({ type: 'session.updated', seq: 1, payload: { session_id: 'session-8' } }),
    )

    expect(onMessage.mock.calls).toEqual([
      [
        'session.updated',
        {
          type: 'session.updated',
          seq: 0,
          ts: '2026-07-20T21:00:00Z',
          payload: { session_id: 'session-7' },
        },
      ],
      [
        'session.updated',
        { type: 'session.updated', seq: 1, payload: { session_id: 'session-8' } },
      ],
    ])
  })

  it.each([
    ['invalid JSON', 'not-json'],
    [
      'schema-invalid JSON',
      JSON.stringify({ type: 'session.updated', seq: 0, payload: { session_id: '' } }),
    ],
  ])('turns %s into an error and post-open reconnect', (_name, frame) => {
    const { sockets, timers, transport } = socketHarness()
    const onMessage = vi.fn()
    const onError = vi.fn()
    const onReconnect = vi.fn()
    const connection = transport.connect({
      topics: ['session.updated'],
      handlers: { onMessage, onError, onReconnect },
    })

    sockets[0].open()
    sockets[0].emit(frame)

    expect(onMessage).not.toHaveBeenCalled()
    expect(sockets[0].closed).toEqual({
      code: 4007,
      reason: 'invalid observability event',
    })
    expect(onError).toHaveBeenCalledWith(1000)
    expect(connection.attempt()).toBe(1)

    timers.runNext()
    sockets[1].open()
    expect(onReconnect).toHaveBeenCalledTimes(1)
    expect(connection.attempt()).toBe(0)
  })

  it('keeps the reconnect scheduled if the browser rejects close()', () => {
    const { sockets, timers, transport } = socketHarness()
    transport.connect({
      topics: ['session.updated'],
      handlers: { onMessage: vi.fn() },
    })
    sockets[0].close = () => {
      throw new DOMException('invalid close code', 'InvalidAccessError')
    }

    sockets[0].emit('not-json')
    expect(timers.timers).toHaveLength(1)

    timers.runNext()
    expect(sockets).toHaveLength(2)
  })

  it('uses exponential reconnect backoff capped at 30 seconds', () => {
    const { sockets, timers, transport } = socketHarness()
    const delays: number[] = []
    transport.connect({
      topics: ['session.updated'],
      handlers: { onMessage: vi.fn(), onError: (delay) => delays.push(delay) },
    })

    for (let index = 0; index < 7; index += 1) {
      sockets[index].error()
      timers.runNext()
    }

    expect(delays).toEqual([1000, 2000, 4000, 8000, 16000, 30000, 30000])
  })
})
