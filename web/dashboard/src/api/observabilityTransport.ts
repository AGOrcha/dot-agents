import {
  createEventStream,
  EVENTS_URL,
  type EventSourceFactory,
  type EventStreamController,
  type TimerCanceller,
  type TimerScheduler,
} from './eventStream'
import type { R2DashboardSSEEventDTO } from './types.gen'

export type DashboardEventType =
  | 'iteration.scored'
  | 'session.updated'
  | 'score.recomputed'
  | 'rubric.changed'
  | 'heartbeat'

export interface ObservabilityTransportHandlers {
  onMessage(topic: DashboardEventType, event: R2DashboardSSEEventDTO): void
  onOpen?(): void
  onReconnect?(): void
  onError?(delayMs: number): void
}

export interface ObservabilityTransportOptions {
  topics: readonly DashboardEventType[]
  handlers: ObservabilityTransportHandlers
}

export interface ObservabilityTransportConnection {
  close(): void
  attempt(): number
}

export interface ObservabilityTransport {
  connect(options: ObservabilityTransportOptions): ObservabilityTransportConnection
}

const EVENT_TYPES: Record<DashboardEventType, true> = {
  'iteration.scored': true,
  'session.updated': true,
  'score.recomputed': true,
  'rubric.changed': true,
  heartbeat: true,
}
const BANDS: Record<string, true> = {
  excellent: true,
  good: true,
  fair: true,
  poor: true,
  unscored: true,
}
const EVENT_KEYS: Record<string, true> = { type: true, seq: true, ts: true, payload: true }
const ITERATION_PAYLOAD_KEYS: Record<string, true> = {
  session_id: true,
  iteration: true,
  band: true,
}
const SESSION_PAYLOAD_KEYS: Record<string, true> = { session_id: true }
const RUBRIC_PAYLOAD_KEYS: Record<string, true> = { rubric_version: true }
const RFC3339_UTC_SECONDS = /^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$/
const DEFAULT_BASE_DELAY_MS = 1000
const DEFAULT_MAX_DELAY_MS = 30_000

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function hasOnlyKeys(value: Record<string, unknown>, allowed: Record<string, true>): boolean {
  return Object.keys(value).every((key) => Object.hasOwn(allowed, key))
}

function validPayload(value: unknown): boolean {
  if (!isRecord(value)) return false

  let matchingVariants = 0
  if (
    hasOnlyKeys(value, ITERATION_PAYLOAD_KEYS) &&
    typeof value.session_id === 'string' &&
    value.session_id.length > 0 &&
    Number.isInteger(value.iteration) &&
    (value.iteration as number) >= 1 &&
    (!('band' in value) || (typeof value.band === 'string' && Object.hasOwn(BANDS, value.band)))
  ) {
    matchingVariants += 1
  }
  if (
    hasOnlyKeys(value, SESSION_PAYLOAD_KEYS) &&
    typeof value.session_id === 'string' &&
    value.session_id.length > 0
  ) {
    matchingVariants += 1
  }
  if (
    hasOnlyKeys(value, RUBRIC_PAYLOAD_KEYS) &&
    typeof value.rubric_version === 'string' &&
    value.rubric_version.length > 0
  ) {
    matchingVariants += 1
  }
  if (Object.keys(value).length === 0) matchingVariants += 1
  return matchingVariants === 1
}

function isDashboardEvent(value: unknown): value is R2DashboardSSEEventDTO {
  if (!isRecord(value) || !hasOnlyKeys(value, EVENT_KEYS)) return false
  if (typeof value.type !== 'string' || !Object.hasOwn(EVENT_TYPES, value.type)) {
    return false
  }
  if (!Number.isInteger(value.seq) || (value.seq as number) < 0 || !validPayload(value.payload)) {
    return false
  }
  return !('ts' in value) ||
    (typeof value.ts === 'string' && RFC3339_UTC_SECONDS.test(value.ts))
}

function parseDashboardEvent(value: unknown): R2DashboardSSEEventDTO | null {
  let parsed = value
  if (typeof value === 'string') {
    try {
      parsed = JSON.parse(value)
    } catch {
      return null
    }
  }
  return isDashboardEvent(parsed) ? parsed : null
}

export interface SseObservabilityTransportConfig {
  url?: string
  eventSourceFactory?: EventSourceFactory
  baseDelayMs?: number
  maxDelayMs?: number
  setTimer?: TimerScheduler
  clearTimer?: TimerCanceller
}

export function createSseObservabilityTransport(
  config: SseObservabilityTransportConfig = {},
): ObservabilityTransport {
  return {
    connect({ topics, handlers }) {
      let stream: EventStreamController
      stream = createEventStream({
        ...config,
        topics,
        handlers: {
          onMessage(topic, value) {
            const event = parseDashboardEvent(value)
            if (event === null || event.type !== topic) {
              stream.reconnect()
              return
            }
            handlers.onMessage(event.type, event)
          },
          onOpen: handlers.onOpen,
          onReconnect: handlers.onReconnect,
          onError: handlers.onError,
        },
      })
      return {
        close: () => stream.close(),
        attempt: () => stream.attempt(),
      }
    },
  }
}

export interface WebSocketLike {
  addEventListener(type: 'open' | 'error' | 'close', listener: (event: Event) => void): void
  addEventListener(type: 'message', listener: (event: MessageEvent) => void): void
  close(code?: number, reason?: string): void
}

export type WebSocketFactory = (url: string) => WebSocketLike

export interface WebSocketObservabilityTransportConfig {
  url?: string
  webSocketFactory?: WebSocketFactory
  baseDelayMs?: number
  maxDelayMs?: number
  setTimer?: TimerScheduler
  clearTimer?: TimerCanceller
}

function webSocketUrl(value: string): string {
  const url = new URL(value, globalThis.location?.origin ?? 'http://localhost')
  if (url.protocol === 'https:') {
    url.protocol = 'wss:'
  } else if (url.protocol === 'http:') {
    url.protocol = 'ws:'
  } else if (url.protocol !== 'ws:' && url.protocol !== 'wss:') {
    throw new Error(`unsupported observability API protocol: ${url.protocol}`)
  }
  return url.toString()
}

export function createWebSocketObservabilityTransport(
  config: WebSocketObservabilityTransportConfig = {},
): ObservabilityTransport {
  const url = webSocketUrl(config.url ?? EVENTS_URL)
  const makeSocket: WebSocketFactory =
    config.webSocketFactory ?? ((socketUrl) => new WebSocket(socketUrl))
  const setTimer = config.setTimer ?? ((fn, ms) => setTimeout(fn, ms))
  const clearTimer =
    config.clearTimer ?? ((token) => clearTimeout(token as ReturnType<typeof setTimeout>))
  const baseDelayMs = config.baseDelayMs ?? DEFAULT_BASE_DELAY_MS
  const maxDelayMs = config.maxDelayMs ?? DEFAULT_MAX_DELAY_MS

  return {
    connect({ topics, handlers }) {
      const subscribedTopics = new Set(topics)
      let socket: WebSocketLike | null = null
      let reconnectToken: unknown = null
      let attempt = 0
      let hasConnected = false
      let closed = false

      const scheduleReconnect = (
        failedSocket: WebSocketLike,
        closeWith?: { code: number; reason: string },
      ): void => {
        if (closed || socket !== failedSocket) return
        socket = null
        if (reconnectToken !== null) return

        const delay = Math.min(baseDelayMs * 2 ** attempt, maxDelayMs)
        attempt += 1
        reconnectToken = setTimer(() => {
          reconnectToken = null
          openSocket()
        }, delay)
        handlers.onError?.(delay)
        if (closeWith) {
          try {
            failedSocket.close(closeWith.code, closeWith.reason)
          } catch {
            // The reconnect timer remains authoritative if the browser rejects close().
          }
        }
      }

      function openSocket(): void {
        if (closed) return
        const nextSocket = makeSocket(url)
        socket = nextSocket

        nextSocket.addEventListener('open', () => {
          if (closed || socket !== nextSocket) return
          attempt = 0
          handlers.onOpen?.()
          if (hasConnected) handlers.onReconnect?.()
          hasConnected = true
        })
        nextSocket.addEventListener('message', (message) => {
          if (closed || socket !== nextSocket) return
          const event = parseDashboardEvent(message.data)
          if (event === null) {
            scheduleReconnect(nextSocket, {
              code: 4007,
              reason: 'invalid observability event',
            })
            return
          }
          if (subscribedTopics.has(event.type)) handlers.onMessage(event.type, event)
        })
        nextSocket.addEventListener('error', () =>
          scheduleReconnect(nextSocket, { code: 4011, reason: 'websocket error' }),
        )
        nextSocket.addEventListener('close', () => scheduleReconnect(nextSocket))
      }

      openSocket()

      return {
        close() {
          closed = true
          if (reconnectToken !== null) {
            clearTimer(reconnectToken)
            reconnectToken = null
          }
          if (socket !== null) {
            const activeSocket = socket
            socket = null
            activeSocket.close(1000, 'observability transport closed')
          }
        },
        attempt: () => attempt,
      }
    },
  }
}
