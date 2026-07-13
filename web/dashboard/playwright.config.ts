import { defineConfig, devices } from '@playwright/test'

/**
 * Playwright config for the R2 observability dashboard frontend (t08 skeleton).
 *
 * Serves the production-built dist/ via `vite preview` so tests exercise the
 * exact artifact the Go server will embed via go:embed (t07).
 *
 * t12 (the full live-iteration smoke) adds backend-wired tests under this
 * same config once the standalone server binary exists. For t08 the suite
 * covers the static-SPA skeleton: all routes render, no console errors, nav
 * links work. API calls fail gracefully (no backend); the views show their
 * error states which is the correct skeleton behaviour.
 */

const PORT = 5174
const BASE_URL = `http://localhost:${PORT}`

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
  webServer: {
    // Build the SPA then serve the dist via vite preview.
    // Uses node_modules/.bin/vite directly so pnpm's dependency-install check
    // does not gate the test run (pnpm 11 build-script policy requires
    // onlyBuiltDependencies approval which is workspace-config-dependent).
    // The CI workflow (t16) will call `pnpm build && pnpm preview` after
    // running `pnpm install` with the workspace config applied properly.
    command: `node_modules/.bin/vite build && node_modules/.bin/vite preview --port ${PORT} --strictPort`,
    url: BASE_URL,
    reuseExistingServer: !process.env.CI,
    timeout: 90_000,
    stdout: 'ignore',
    stderr: 'pipe',
  },
})
