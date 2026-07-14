/**
 * Reconnecting Server-Sent Events client for the R2 observability dashboard.
 *
 * Wraps the browser `EventSource` over
 *   GET {API_ROOT}/events   (API.md §3.7 — text/event-stream)
 * which emits frames shaped `event: <topic>\nid: <seq>\ndata: <json>` for the
 * topics iteration.scored / session.updated / score.recomputed (+ rubric.changed,
 * heartbeat). Each named topic is delivered via `addEventListener(topic, …)`;
 * the wrapper JSON-parses `event.data` and hands the topic + parsed payload to
 * the caller.
 *
 * The native `EventSource` reconnects on transport errors, but it does so on an
 * opaque schedule and gives no "we came back after being down" signal. This
 * wrapper takes explicit control instead: on error it closes the socket and
 * schedules its own reconnect with exponential backoff (baseDelayMs · 2^attempt,
 * capped at maxDelayMs). A successful (re)open resets the backoff, and every
 * open *after the first* is reported via `onReconnect` so the consumer can
 * recover events missed during the outage (t11: invalidate all queries).
 */

import { API_ROOT } from './client'

/** SSE endpoint path (relative to API_ROOT). */
export const EVENTS_PATH = '/events'

/** Fully-qualified SSE URL: {VITE_API_BASE}/api/v1/observability/events. */
export const EVENTS_URL = `${API_ROOT}${EVENTS_PATH}`

/** Callbacks the stream invokes over its lifecycle. */
export interface EventStreamHandlers {
  /**
   * A named SSE frame arrived. `data` is the JSON-parsed `data:` payload, or
   * the raw string if it was not valid JSON.
   */
  onMessage: (topic: string, data: unknown) => void
  /** The socket (re)opened after having previously dropped. Fired on every open past the first. */
  onReconnect?: () => void
  /** The socket opened (including the very first connect). */
  onOpen?: () => void
  /** The transport errored; a reconnect has been scheduled `delayMs` out. */
  onError?: (delayMs: number) => void
}

/** Minimal `EventSource` surface this wrapper depends on (eases mocking in tests). */
export interface EventSourceLike {
  addEventListener: (type: string, listener: (event: MessageEvent) => void) => void
  close: () => void
  onopen: ((event: Event) => void) | null
  onerror: ((event: Event) => void) | null
}

/** Constructs an `EventSourceLike` for `url` (defaults to the global `EventSource`). */
export type EventSourceFactory = (url: string) => EventSourceLike

/** Schedules `fn` after `ms` and returns a cancellation token. */
export type TimerScheduler = (fn: () => void, ms: number) => unknown
/** Cancels a token previously returned by a `TimerScheduler`. */
export type TimerCanceller = (token: unknown) => void

/** Configuration for {@link createEventStream}. */
export interface EventStreamOptions {
  /** Named topics to subscribe to via `addEventListener`. */
  topics: readonly string[]
  /** Lifecycle callbacks. */
  handlers: EventStreamHandlers
  /** URL to connect to. Defaults to {@link EVENTS_URL}. */
  url?: string
  /** EventSource constructor seam for tests. Defaults to the global `EventSource`. */
  eventSourceFactory?: EventSourceFactory
  /** First-attempt backoff delay in ms. Default 1000. */
  baseDelayMs?: number
  /** Maximum backoff delay in ms. Default 30000. */
  maxDelayMs?: number
  /** Timer seam for tests. Defaults to `setTimeout`. */
  setTimer?: TimerScheduler
  /** Timer-cancel seam for tests. Defaults to `clearTimeout`. */
  clearTimer?: TimerCanceller
}

/** Handle returned by {@link createEventStream}. */
export interface EventStreamController {
  /** Permanently close the stream and cancel any pending reconnect. */
  close: () => void
  /** Number of consecutive failed connects (0 while healthy). Exposed for tests. */
  attempt: () => number
}

const DEFAULT_BASE_DELAY_MS = 1000
const DEFAULT_MAX_DELAY_MS = 30000

/**
 * Open a reconnecting SSE stream. Returns immediately with a controller; the
 * first connection is initiated synchronously.
 */
export function createEventStream(options: EventStreamOptions): EventStreamController {
  const {
    topics,
    handlers,
    url = EVENTS_URL,
    baseDelayMs = DEFAULT_BASE_DELAY_MS,
    maxDelayMs = DEFAULT_MAX_DELAY_MS,
  } = options

  const makeSource: EventSourceFactory =
    options.eventSourceFactory ?? ((u) => new EventSource(u) as unknown as EventSourceLike)
  const setTimer: TimerScheduler =
    options.setTimer ?? ((fn, ms) => setTimeout(fn, ms))
  const clearTimer: TimerCanceller =
    options.clearTimer ?? ((token) => clearTimeout(token as ReturnType<typeof setTimeout>))

  let source: EventSourceLike | null = null
  let reconnectToken: unknown = null
  let attempt = 0
  let hasConnected = false
  let closed = false

  const backoffDelay = (n: number): number => Math.min(baseDelayMs * 2 ** n, maxDelayMs)

  const handleMessage = (topic: string) => (event: MessageEvent) => {
    let data: unknown = event.data
    if (typeof event.data === 'string') {
      try {
        data = JSON.parse(event.data)
      } catch {
        data = event.data
      }
    }
    handlers.onMessage(topic, data)
  }

  const scheduleReconnect = (): void => {
    if (closed) return
    const delay = backoffDelay(attempt)
    attempt += 1
    handlers.onError?.(delay)
    reconnectToken = setTimer(() => {
      reconnectToken = null
      connect()
    }, delay)
  }

  function connect(): void {
    if (closed) return
    const es = makeSource(url)
    source = es

    es.onopen = () => {
      attempt = 0
      handlers.onOpen?.()
      if (hasConnected) {
        handlers.onReconnect?.()
      }
      hasConnected = true
    }

    es.onerror = () => {
      // Tear down the failed socket and back off; the native retry is bypassed
      // so a single, explicit reconnect schedule owns the cadence.
      es.close()
      if (source === es) source = null
      scheduleReconnect()
    }

    for (const topic of topics) {
      es.addEventListener(topic, handleMessage(topic))
    }
  }

  connect()

  return {
    close: () => {
      closed = true
      if (reconnectToken !== null) {
        clearTimer(reconnectToken)
        reconnectToken = null
      }
      if (source) {
        source.close()
        source = null
      }
    },
    attempt: () => attempt,
  }
}
