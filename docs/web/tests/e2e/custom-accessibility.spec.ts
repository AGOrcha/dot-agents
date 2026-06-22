import { test, expect, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

// Accessibility + AI-parity guard for the custom interactive routes.
//
// The render suite (custom-content.spec.ts) proves the Cytoscape canvas paints.
// This suite proves the two things a <canvas> alone CANNOT give us:
//
//   1. HUMAN accessibility (axe-core): the page meets WCAG landmark / heading /
//      contrast / keyboard rules, and the canvas has a real text-alternative in
//      the accessibility tree (the canvas itself is aria-hidden, so without the
//      alternative a screen-reader user would get nothing).
//
//   2. AI accessibility / parity: the graph is published as a machine-parseable
//      JSON twin (<script type="application/json" data-graph-json>) that an agent
//      can read WITHOUT executing Cytoscape — and that twin's node set EXACTLY
//      matches what Cytoscape actually rendered (window.__cyNodes[graphId]) and
//      what the human text-alternative lists. If the visual and the agent-readable
//      form ever drift apart, the parity assertions fail.
//
// These graph pages are custom .astro routes (NOT in the docs content collection),
// so dm4's per-page .md / llms.txt mirror does not cover them; the equivalent
// forms are emitted by ResourceGraph.astro itself and asserted here.

const GRAPH_ROUTES = [
  '/graphs/da-resources/',
  '/graphs/workflow-resources/',
  '/graphs/workspace-state/',
] as const;

// All custom routes get the human-a11y (axe) pass, including the diagram routes.
const ALL_CUSTOM_ROUTES = [
  ...GRAPH_ROUTES,
  '/diagrams/tier-model/',
  '/diagrams/lens-dispatch/',
  '/diagrams/verifier-registry/',
] as const;

// Rules we explicitly require. axe is scoped to the page main content so unrelated
// Starlight chrome (theme toggle, search) can't mask a real content regression,
// but contrast + region/landmark + heading-order + keyboard rules stay in scope —
// those are exactly the standards the docs-n-design verifier promises.
const REQUIRED_TAGS = ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'] as const;

// Run axe against the page main content. Returns the (filtered) violations.
async function runAxe(page: Page) {
  const builder = new AxeBuilder({ page })
    // Starlight wraps page content in <main>; scope there so the assertion is
    // about OUR content, not the site shell. The graph a11y region + canvas live
    // inside main, so contrast / region / heading checks still cover them.
    .include('main')
    .withTags([...REQUIRED_TAGS]);
  const results = await builder.analyze();
  return results.violations;
}

for (const route of ALL_CUSTOM_ROUTES) {
  test(`a11y: no axe violations on ${route}`, async ({ page }) => {
    const response = await page.goto(route, { waitUntil: 'networkidle' });
    expect(response, `no response for ${route}`).not.toBeNull();
    expect(response!.status(), `expected 200 for ${route}`).toBe(200);

    const violations = await runAxe(page);
    const summary = violations
      .map((v) => `  [${v.impact}] ${v.id}: ${v.help} (${v.nodes.length} node(s))`)
      .join('\n');
    expect(violations, `axe violations on ${route}:\n${summary}`).toEqual([]);
  });
}

for (const route of GRAPH_ROUTES) {
  test(`a11y: canvas text-alternative is in the accessibility tree on ${route}`, async ({ page }) => {
    await page.goto(route, { waitUntil: 'networkidle' });

    // The opaque canvas must be hidden from assistive tech...
    const canvas = page.locator('.graph-canvas');
    await expect(canvas, `.graph-canvas missing on ${route}`).toBeAttached();
    await expect(canvas).toHaveAttribute('aria-hidden', 'true');

    // ...and an ARIA-named text-alternative region must replace it. getByRole
    // resolves through the accessibility tree, so this fails if the region is
    // display:none'd (which would strip it from a11y + rendered-DOM agents).
    const region = page.getByRole('region', { name: /graph data \(text\)/i });
    await expect(region, `named graph-data region missing on ${route}`).toBeAttached();

    // The region must actually enumerate nodes (not be an empty shell).
    const items = region.locator('[data-a11y-node-id]');
    expect(await items.count(), `text-alternative lists no nodes on ${route}`).toBeGreaterThan(0);
  });
}

for (const route of GRAPH_ROUTES) {
  test(`ai-parity: data-graph-json matches the rendered + text-alternative node set on ${route}`, async ({ page }) => {
    await page.goto(route, { waitUntil: 'networkidle' });

    // (a) Agent-parseable form: parse the JSON twin straight from the DOM, no
    //     canvas execution required.
    const jsonText = await page
      .locator('script[type="application/json"][data-graph-json]')
      .first()
      .textContent();
    expect(jsonText, `data-graph-json block missing on ${route}`).toBeTruthy();
    const parsed = JSON.parse(jsonText!) as { nodes: { id: string }[]; edges: unknown[] };
    const jsonIds = new Set(parsed.nodes.map((n) => n.id));
    expect(jsonIds.size, `data-graph-json has no nodes on ${route}`).toBeGreaterThan(0);

    // (b) Rendered form: the node-id set Cytoscape actually mounted, exposed by
    //     the component's client script as window.__cyNodes[graphId].
    const graphId = (await page.locator('[data-graph-id]').first().getAttribute('data-graph-id'))!;
    await expect
      .poll(
        async () =>
          page.evaluate(
            (id) => (window as any).__cyNodes?.[id]?.length ?? -1,
            graphId,
          ),
        { message: `window.__cyNodes[${graphId}] never populated on ${route}` },
      )
      .toBeGreaterThan(0);
    const renderedIds: string[] = await page.evaluate(
      (id) => (window as any).__cyNodes[id],
      graphId,
    );
    const renderedSet = new Set(renderedIds);

    // (c) Text-alternative form: the node ids the human-readable region lists.
    const textIds = await page
      .locator('[data-a11y-node-id]')
      .evaluateAll((els) => els.map((e) => e.getAttribute('data-a11y-node-id')!));
    const textSet = new Set(textIds);

    // Parity: all three forms must describe the SAME node set (count + ids).
    expect(renderedSet.size, `rendered vs JSON node count mismatch on ${route}`).toBe(jsonIds.size);
    expect([...jsonIds].sort(), `JSON vs rendered node ids differ on ${route}`).toEqual(
      [...renderedSet].sort(),
    );
    expect(textSet.size, `text-alternative vs JSON node count mismatch on ${route}`).toBe(
      jsonIds.size,
    );
    expect([...textSet].sort(), `text-alternative vs JSON node ids differ on ${route}`).toEqual(
      [...jsonIds].sort(),
    );
  });
}
