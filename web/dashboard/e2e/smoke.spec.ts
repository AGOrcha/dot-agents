import { test, expect, type Page, type Response } from '@playwright/test'

/**
 * Skeleton smoke tests for the R2 observability dashboard (t08).
 *
 * These tests verify that the static-built SPA loads correctly and that all
 * four routes render their placeholder UI without crashing. API calls will
 * fail gracefully (no backend running) and views show their error/loading
 * states — that is the expected skeleton behaviour.
 *
 * t12 (live-iteration smoke) adds backend-wired assertions once cmd/da-dashboard
 * exists. These tests remain as the baseline "app shell works" gate.
 */

// Collect console errors and non-asset >= 400 responses that indicate
// a real rendering failure vs the expected API-unavailable error states.
function attachErrorCollectors(page: Page) {
  const consoleErrors: string[] = []
  const pageErrors: string[] = []
  // API calls failing with >=400 are expected (no backend); filter them out.
  const unexpectedBadResponses: string[] = []

  page.on('console', (msg) => {
    // TanStack Query logs failed query errors to console.error; suppress:
    //   - "Failed to load resource" — browser's generic message for fetch failures
    //     (API calls always fail with no backend running; this is expected)
    //   - Any message referencing the API path
    // Only collect real JS errors (undefined vars, syntax errors, etc.).
    if (msg.type() === 'error') {
      const text = msg.text()
      if (
        text.includes('Failed to load resource') ||
        text.includes('api/v1/observability') ||
        text.includes('fetch') ||
        text.includes('NetworkError')
      ) {
        return // expected: API unavailable in skeleton e2e
      }
      consoleErrors.push(text)
    }
  })
  page.on('pageerror', (err) => {
    pageErrors.push(err.message)
  })
  page.on('response', (res: Response) => {
    // Skip API calls (expected to fail) and favicon (may 404 in preview).
    const url = res.url()
    if (res.status() >= 400 && !url.includes('/api/') && !url.includes('favicon')) {
      unexpectedBadResponses.push(`${res.status()} ${url}`)
    }
  })

  return {
    assertClean() {
      expect(pageErrors, `unexpected page errors:\n${pageErrors.join('\n')}`).toEqual([])
      expect(
        consoleErrors,
        `unexpected console errors:\n${consoleErrors.join('\n')}`,
      ).toEqual([])
      expect(
        unexpectedBadResponses,
        `unexpected >=400 asset responses:\n${unexpectedBadResponses.join('\n')}`,
      ).toEqual([])
    },
  }
}

test('aggregate view renders at /', async ({ page }) => {
  const collectors = attachErrorCollectors(page)

  const res = await page.goto('/', { waitUntil: 'domcontentloaded' })
  expect(res?.status(), 'expected 200 for /').toBe(200)

  // App shell renders nav
  const nav = page.locator('nav[role="navigation"]')
  await expect(nav, 'nav should be visible').toBeVisible()

  // Heading is present
  await expect(page.getByRole('heading', { name: /runs/i })).toBeVisible()

  // Navigation links
  await expect(page.getByRole('link', { name: /runs/i })).toBeVisible()
  await expect(page.getByRole('link', { name: /rubric/i })).toBeVisible()

  collectors.assertClean()
})

test('run detail view renders at /runs/:sessionId', async ({ page }) => {
  const collectors = attachErrorCollectors(page)

  const res = await page.goto('/runs/test-session-abc', { waitUntil: 'domcontentloaded' })
  expect(res?.status(), 'expected 200 for /runs/:sessionId').toBe(200)

  await expect(page.getByRole('heading', { name: /run detail/i })).toBeVisible()

  collectors.assertClean()
})

test('iteration detail view renders at /iterations/:n', async ({ page }) => {
  const collectors = attachErrorCollectors(page)

  const res = await page.goto('/iterations/3', { waitUntil: 'domcontentloaded' })
  expect(res?.status(), 'expected 200 for /iterations/:n').toBe(200)

  await expect(page.getByRole('heading', { name: /iteration detail/i })).toBeVisible()

  collectors.assertClean()
})

test('rubric view renders at /rubric', async ({ page }) => {
  const collectors = attachErrorCollectors(page)

  const res = await page.goto('/rubric', { waitUntil: 'domcontentloaded' })
  expect(res?.status(), 'expected 200 for /rubric').toBe(200)

  await expect(page.getByRole('heading', { name: /rubric/i })).toBeVisible()

  collectors.assertClean()
})

test('navigation between routes works', async ({ page }) => {
  const collectors = attachErrorCollectors(page)

  await page.goto('/', { waitUntil: 'domcontentloaded' })
  await expect(page.getByRole('heading', { name: /runs/i })).toBeVisible()

  // Navigate to rubric via nav link
  await page.getByRole('link', { name: /rubric/i }).click()
  await expect(page.getByRole('heading', { name: /rubric/i })).toBeVisible()

  // Navigate back to runs
  await page.getByRole('link', { name: /runs/i }).click()
  await expect(page.getByRole('heading', { name: /runs/i })).toBeVisible()

  collectors.assertClean()
})
