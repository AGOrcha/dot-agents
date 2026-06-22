import { test, expect, type Page, type Response } from '@playwright/test';

// End-to-end guard for the custom interactive routes ported in dm2.
//
// Why this exists: the Cytoscape graphs once rendered blank because the
// .graph-canvas height CSS lived in the host page's scoped <style> while the div
// is emitted by the ResourceGraph component. Astro scopes styles per component,
// so the canvas collapsed to 0 height and Cytoscape painted nothing — a defect
// the build and a plain `curl` both pass. These tests load each route in a real
// (headless) browser and fail if a graph regresses to 0-height / no rendered
// nodes, or if any asset 404s, so that bug class can never pass CI silently.
//
// (A pixel-level visual-snapshot layer is a possible future extension; it is
// intentionally out of scope here.)

const GRAPH_ROUTES = [
  '/graphs/da-resources/',
  '/graphs/workflow-resources/',
  '/graphs/workspace-state/',
] as const;

// Diagram routes that render a JSON-driven tier card grid.
const TIER_DIAGRAM_ROUTE = '/diagrams/tier-model/';

// Stubbed diagram routes: assert the page heading renders, nothing more.
const STUB_DIAGRAM_ROUTES = [
  { path: '/diagrams/lens-dispatch/', heading: 'Lens reviewer dispatch flow' },
  { path: '/diagrams/verifier-registry/', heading: 'Verifier profile registry' },
] as const;

// Attach console-error / pageerror / >=400-response collectors to a page and
// return getters that assert the route loaded cleanly. Astro/Cytoscape emit no
// console.error on a healthy page, and a blank-graph regression typically shows
// up as a 0-height container rather than a thrown error, so we guard on both
// the runtime-error surface AND the rendered geometry below.
function attachErrorCollectors(page: Page) {
  const consoleErrors: string[] = [];
  const pageErrors: string[] = [];
  const badResponses: string[] = [];

  page.on('console', (msg) => {
    if (msg.type() === 'error') consoleErrors.push(msg.text());
  });
  page.on('pageerror', (err) => {
    pageErrors.push(err.message);
  });
  page.on('response', (res: Response) => {
    if (res.status() >= 400) badResponses.push(`${res.status()} ${res.url()}`);
  });

  return {
    assertClean() {
      expect(badResponses, `unexpected >=400 responses:\n${badResponses.join('\n')}`).toEqual([]);
      expect(pageErrors, `unexpected page errors:\n${pageErrors.join('\n')}`).toEqual([]);
      expect(consoleErrors, `unexpected console errors:\n${consoleErrors.join('\n')}`).toEqual([]);
    },
  };
}

for (const route of GRAPH_ROUTES) {
  test(`graph route renders: ${route}`, async ({ page }) => {
    const collectors = attachErrorCollectors(page);

    const response = await page.goto(route, { waitUntil: 'networkidle' });
    expect(response, `no response for ${route}`).not.toBeNull();
    expect(response!.status(), `expected 200 for ${route}`).toBe(200);

    const canvas = page.locator('.graph-canvas');
    await expect(canvas, `.graph-canvas missing on ${route}`).toBeVisible();

    // The container must have real height — this is the exact dimension that
    // collapsed to 0 in the blank-graph regression.
    const box = await canvas.boundingBox();
    expect(box, `.graph-canvas has no bounding box on ${route}`).not.toBeNull();
    expect(box!.height, `.graph-canvas height too small on ${route}`).toBeGreaterThan(100);

    // Cytoscape mounts its render layers as <canvas> elements inside the
    // container; if it rendered nothing, there are none.
    const innerCanvas = page.locator('.graph-canvas canvas');
    await expect
      .poll(() => innerCanvas.count(), { message: `no <canvas> mounted by Cytoscape on ${route}` })
      .toBeGreaterThan(0);

    collectors.assertClean();
  });
}

test(`diagram route renders tier cards: ${TIER_DIAGRAM_ROUTE}`, async ({ page }) => {
  const collectors = attachErrorCollectors(page);

  const response = await page.goto(TIER_DIAGRAM_ROUTE, { waitUntil: 'networkidle' });
  expect(response, `no response for ${TIER_DIAGRAM_ROUTE}`).not.toBeNull();
  expect(response!.status(), `expected 200 for ${TIER_DIAGRAM_ROUTE}`).toBe(200);

  const cards = page.locator('.tier-card');
  await expect(cards.first(), `no visible .tier-card on ${TIER_DIAGRAM_ROUTE}`).toBeVisible();
  expect(await cards.count(), `expected >=1 .tier-card on ${TIER_DIAGRAM_ROUTE}`).toBeGreaterThan(0);

  collectors.assertClean();
});

for (const { path, heading } of STUB_DIAGRAM_ROUTES) {
  test(`stub diagram route renders heading: ${path}`, async ({ page }) => {
    const collectors = attachErrorCollectors(page);

    const response = await page.goto(path, { waitUntil: 'networkidle' });
    expect(response, `no response for ${path}`).not.toBeNull();
    expect(response!.status(), `expected 200 for ${path}`).toBe(200);

    await expect(
      page.getByRole('heading', { name: new RegExp(heading, 'i') }),
      `heading "${heading}" not visible on ${path}`,
    ).toBeVisible();

    collectors.assertClean();
  });
}
