import { test, expect } from '@playwright/test'
import { writeFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * t12 live-iteration smoke — proves a live iteration surfaces in the browser
 * within the 2s propagation budget (API.md §3.7 / OQ3).
 *
 * This is the browser leg of the smoke; its runnable, browser-free companion
 * is tests/e2e/dashboard_live_iteration_test.go, which asserts the same budget
 * at the SSE transport over a loopback socket.
 *
 * It runs in the config's backend-wired mode against a live da-dashboard
 * server (cmd/da-dashboard) that serves both the built SPA and the /api
 * REST+SSE surface. Because it must (a) drive a real browser and (b) write
 * into the exact iter-log directory that server watches, it is GATED behind an
 * explicit opt-in and skips cleanly when the backend/browser stack is not
 * wired (the default in unit-CI). Enable it with:
 *
 *   E2E_LIVE_ITERATION=1
 *   E2E_DASHBOARD_URL=http://127.0.0.1:7300   (the running da-dashboard)
 *   E2E_ITERLOG_DIR=/abs/path                 (a --iter-log-dir the server watches)
 *
 * Budget scope: the dashboard's live-update mechanism is the SSE stream the
 * browser consumes via createEventStream (web/dashboard/src/api/eventStream.ts).
 * The first test asserts the browser's own EventSource receives the iteration
 * within 2s — the propagation budget. A live DOM auto-repaint additionally
 * requires the t11 useLiveUpdates() hook to be mounted at the app root
 * (main.tsx); it is not wired at this integration base, so the second test
 * proves the data path end to end via an explicit refetch instead.
 */

/** The spec's assertion target: live iteration → browser within 2 seconds. */
const PROPAGATION_BUDGET_MS = 2000

/** §3.7 SSE endpoint and §3.6 health probe, relative to the dashboard origin. */
const EVENTS_PATH = '/api/v1/observability/events'
const HEALTH_PATH = '/api/v1/observability/health'

/** The SSE event topic (and frame data type) a scored iteration publishes. */
const SCORED_TOPIC = 'iteration.scored'

const READY =
  process.env.E2E_LIVE_ITERATION === '1' &&
  !!process.env.E2E_DASHBOARD_URL &&
  !!process.env.E2E_ITERLOG_DIR

/** One frame this spec collects from the in-browser EventSource. */
interface LiveEvent {
  at: number
  topic: string
  data: string
}

/** State the spec parks on `window` to bridge browser events back to Node. */
interface LiveState {
  received: LiveEvent[]
  es?: EventSource
}

declare global {
  interface Window {
    __liveSSE?: LiveState
  }
}

/** Read a required env var as a typed string (presence is guaranteed by READY). */
function requireEnv(name: string): string {
  const value = process.env[name]
  if (!value) throw new Error(`${name} is required for the live-iteration smoke`)
  return value
}

/** A minimal but schema-valid iter-N.yaml record the watcher will publish. */
function iterationRecord(iterN: number, sessionId: string): string {
  return [
    'schema_version: 2',
    `iteration: ${iterN}`,
    'date: "2026-07-10"',
    'wave: "w1"',
    'task_id: "t12-e2e"',
    'commit: "deadbeef"',
    'files_changed: 1',
    'lines_added: 3',
    'lines_removed: 0',
    'agent:',
    `  session_id: "${sessionId}"`,
    '  harness: "claude-code"',
    '  model: "m-claude"',
    'impl:',
    '  summary: "live iteration smoke"',
    '  retries: 0',
    '',
  ].join('\n')
}

/** A per-run iter file name/session id pair (unique so reruns never collide). */
function freshIteration(prefix: string): { iterN: number; sessionId: string } {
  const now = Date.now()
  return { iterN: Number(String(now).slice(-6)), sessionId: `${prefix}-${now}` }
}

test.describe('t12 live iteration → dashboard within the 2s budget', () => {
  test.skip(
    !READY,
    'backend-wired live-iteration smoke: set E2E_LIVE_ITERATION=1, E2E_DASHBOARD_URL, and E2E_ITERLOG_DIR ' +
      '(a dir the da-dashboard server watches). See playwright.config.ts. Skipping — no live backend/iter-log ' +
      'wired in this environment.',
  )

  test('a live iteration reaches the browser SSE stream within the budget', async ({ page, request }) => {
    const dashboardUrl = requireEnv('E2E_DASHBOARD_URL')
    const iterlogDir = requireEnv('E2E_ITERLOG_DIR')

    // Runtime reachability guard: skip (do not fail) if the server is down.
    const health = await request.get(`${dashboardUrl}${HEALTH_PATH}`).catch(() => null)
    test.skip(
      !health || !health.ok(),
      `da-dashboard health endpoint unreachable at ${dashboardUrl}${HEALTH_PATH}; skipping backend-wired smoke`,
    )

    await page.goto('/')

    // Subscribe exactly as the SPA's createEventStream does: a browser
    // EventSource against the §3.7 endpoint, stamping each frame's receipt
    // time so the propagation window is measured client-side.
    await page.evaluate(
      ({ eventsPath, topic }) => {
        const state: LiveState = { received: [] }
        window.__liveSSE = state
        const es = new EventSource(eventsPath)
        state.es = es
        es.addEventListener(topic, (event) => {
          // `in`-narrowing keeps event.data checked (MessageEvent-only field).
          const data = 'data' in event ? String(event.data) : ''
          state.received.push({ at: Date.now(), topic, data })
        })
      },
      { eventsPath: EVENTS_PATH, topic: SCORED_TOPIC },
    )

    // Wait for the stream to OPEN before emitting, so the broker has registered
    // this subscriber (an event published before then would fan out to nobody).
    // This handshake is outside the propagation budget by design.
    await page.waitForFunction(() => window.__liveSSE?.es?.readyState === EventSource.OPEN, undefined, {
      timeout: 5000,
    })

    // Emit the live iteration: a fresh iter-<n>.yaml under the watched root.
    const { iterN, sessionId } = freshIteration('sess-live')
    writeFileSync(join(iterlogDir, `iter-${iterN}.yaml`), iterationRecord(iterN, sessionId))

    // The budget gate: the browser must receive the frame within 2s of the write.
    await page.waitForFunction(() => (window.__liveSSE?.received.length ?? 0) > 0, undefined, {
      timeout: PROPAGATION_BUDGET_MS,
    })

    const received = await page.evaluate(() => window.__liveSSE?.received ?? [])
    await page.evaluate(() => window.__liveSSE?.es?.close())

    expect(received.length, 'expected an iteration.scored frame within the budget').toBeGreaterThan(0)
    const frame = received[0]
    expect(frame.topic).toBe(SCORED_TOPIC)

    const payload = JSON.parse(frame.data)
    expect(payload.type).toBe(SCORED_TOPIC)
    expect(payload.payload.iteration).toBe(iterN)
    expect(payload.payload.session_id).toBe(sessionId)
  })

  test('the runs grid reflects a live iteration after a refetch', async ({ page, request }) => {
    const dashboardUrl = requireEnv('E2E_DASHBOARD_URL')
    const iterlogDir = requireEnv('E2E_ITERLOG_DIR')

    const health = await request.get(`${dashboardUrl}${HEALTH_PATH}`).catch(() => null)
    test.skip(
      !health || !health.ok(),
      `da-dashboard health endpoint unreachable at ${dashboardUrl}${HEALTH_PATH}; skipping backend-wired smoke`,
    )

    await page.goto('/')

    // Emit a live iteration for a brand-new session, then refetch. The store
    // keys its read cache on a per-file (name, mtime) fingerprint, so the new
    // file busts the cache on the very next GET /runs — no dependency on the
    // (un-mounted) live hook. The new run row must then render.
    const { iterN, sessionId } = freshIteration('sess-refetch')
    writeFileSync(join(iterlogDir, `iter-${iterN}.yaml`), iterationRecord(iterN, sessionId))

    await page.reload()
    await expect(page.getByTestId(`run-row-${sessionId}`)).toBeVisible({ timeout: 5000 })
  })
})
