# UI verifier taxonomy + the docs-n-design (ui-e2e) verifier

## Problem
`build + curl` (today's app_type: docs verification) proves the site BUILDS, not that client-rendered content WORKS or meets our standards. The /graphs/da-resources Cytoscape page shipped a green build with a blank graph (asset/hydration failure invisible to build+curl). Interactive + design-bearing content needs a render-and-standards gate.

## Decision — one ui-e2e flavor per UI SURFACE (surfaces have different standards)
- docs-n-design (build now): the docs site's custom interactive content (/graphs/*, /diagrams/*). Standards: it renders, it's accessible to humans, it's accessible to AI. Static Starlight markdown stays on build+curl.
- observability / dashboard ui (DEFERRED): a different surface (obs.agorcha.dev / r2-observability-dashboard), standards skew to UI performance + real-time/dashboard interaction, not content a11y. Its own flavor when that UI lands. Not built now.

## docs-n-design — what it asserts
CI gate; Playwright headless, BUILT with deploy flags (IS_CLOUDFLARE=1) against a locally-served dist (reproduces the Worker base path -> catches asset-404s pre-merge). Per custom route:
1. Renders — 200; no network response >=400 (asset-404 catch); no uncaught console/page errors; graph present (Cytoscape: container <canvas> non-zero or window.__cy.nodes().length>0; diagrams: <svg> with children).
2. Human-accessible — axe-core: landmarks + heading order, contrast, keyboard focus/reachability; plus a canvas text-alternative (ARIA-named region + equivalent textual/structured graph rendering).
3. AI-accessible — same info without rendering the canvas: graph data as JSON and/or clean .md (dm4) in llms.txt; assert the agent-parseable form exists and matches the visual (node set parity).

## Integration
dm7-ui-e2e-custom-content-verifier implements the Playwright + axe + agent-data-parity suite, wired as a CI gate in deploy-docs.yml; dm2 done-criteria includes it passing. The config-v2 stage_profiles work later formalizes this as the ui-e2e verifier_profile's docs-n-design composition (prompt_files/lens); until then the CI suite is the gate.
