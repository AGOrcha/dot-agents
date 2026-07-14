import { defineConfig, devices } from '@playwright/test'

/**
 * Playwright config for the R2 observability dashboard frontend.
 *
 * Two modes, selected by the E2E_DASHBOARD_URL env var:
 *
 *   - Skeleton mode (default — E2E_DASHBOARD_URL unset): builds dist/ and
 *     serves it via `vite preview` on PORT. The t08 smoke suite
 *     (e2e/smoke.spec.ts) exercises the static SPA shell with no backend —
 *     API calls fail gracefully and the views show their error states.
 *
 *   - Backend-wired mode (E2E_DASHBOARD_URL set): points baseURL at an
 *     already-running standalone da-dashboard server (cmd/da-dashboard) that
 *     serves BOTH the built SPA and the /api REST+SSE surface. The t12
 *     live-iteration smoke (e2e/live-iteration.spec.ts) runs in this mode; it
 *     additionally needs E2E_ITERLOG_DIR (a dir the server watches) so the
 *     test can emit a live iteration and observe it propagate, and
 *     E2E_LIVE_ITERATION=1 to opt in. In this mode Playwright does NOT manage
 *     a webServer — the operator owns the backend lifecycle:
 *
 *       go build -o /tmp/da-dashboard ./cmd/da-dashboard
 *       (cd web/dashboard && node_modules/.bin/vite build)
 *       ITERLOG=$(mktemp -d)
 *       /tmp/da-dashboard --addr 127.0.0.1:7300 \
 *         --iter-log-dir "$ITERLOG" --static-dir web/dashboard/dist &
 *       E2E_LIVE_ITERATION=1 E2E_DASHBOARD_URL=http://127.0.0.1:7300 \
 *         E2E_ITERLOG_DIR="$ITERLOG" node_modules/.bin/playwright test live-iteration
 */

const PORT = 5174
const PREVIEW_URL = `http://localhost:${PORT}`
const BACKEND_URL = process.env.E2E_DASHBOARD_URL
const BASE_URL = BACKEND_URL ?? PREVIEW_URL

export default defineConfig({
  testDir: './e2e',
  outputDir: './e2e/.artifacts',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: 0,
  reporter: 'list',
  use: {
    baseURL: BASE_URL,
    trace: 'on-first-retry',
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
  // Skeleton mode builds + previews the SPA itself. Backend-wired mode expects
  // an externally-managed da-dashboard, so no webServer is spawned there.
  //
  // Uses node_modules/.bin/vite directly so pnpm's dependency-install check
  // does not gate the test run (pnpm 11 build-script policy requires
  // onlyBuiltDependencies approval which is workspace-config-dependent).
  ...(BACKEND_URL
    ? {}
    : {
        webServer: {
          command: `node_modules/.bin/vite build && node_modules/.bin/vite preview --port ${PORT} --strictPort`,
          url: PREVIEW_URL,
          reuseExistingServer: !process.env.CI,
          timeout: 90_000,
          stdout: 'ignore',
          stderr: 'pipe',
        },
      }),
})
