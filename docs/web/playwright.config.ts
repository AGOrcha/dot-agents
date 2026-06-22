import { defineConfig, devices } from '@playwright/test';

// Hermetic e2e config for the custom interactive routes (/graphs/* + /diagrams/*).
//
// webServer builds the production (Cloudflare) bundle and static-serves the
// generated `dist/` so the suite exercises the same artifact CI ships:
//   - DEPLOY_TARGET=cloudflare => astro.config.mjs sets base='/' (assets at root).
//   - `serve dist` runs in DIRECTORY mode (NOT `serve -s`). The SPA flag would
//     rewrite /graphs/da-resources/ to the root index.html, so the test would
//     load the homepage (no .graph-canvas) and time out on a false negative.
//     Directory mode honors each route's own index.html.
const PORT = 4399;
const BASE_URL = `http://localhost:${PORT}`;

export default defineConfig({
  testDir: './tests/e2e',
  // Keep run artifacts (traces, screenshots) inside the in-scope tests/ tree so
  // a single gitignore there covers them; the default top-level test-results/
  // would otherwise need a separate ignore rule.
  outputDir: './tests/.artifacts',
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
    command: `DEPLOY_TARGET=cloudflare npm run build && npx serve dist -l ${PORT} --no-clipboard`,
    url: `${BASE_URL}/`,
    reuseExistingServer: !process.env.CI,
    timeout: 180_000,
    // The build emits useful diagnostics; the static file server's per-request
    // log is noise, so only surface stderr.
    stdout: 'ignore',
    stderr: 'pipe',
  },
});
