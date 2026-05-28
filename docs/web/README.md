# docs/web — Interactive dot-agents Docs Site

Static, interactive HTML site that consumes the canonical markdown demo
content from `docs/DEMO_*.md` and surfaces it as:

- **A. Structured markdown pages** — sidebar TOC, anchors, code highlighting,
  Mermaid block rendering.
- **B. Clickable resource graphs** — interactive node-link views of da
  resources, workflow resources, and workspace state.
- **C. Simpler navigable diagrams** — tier model, lens dispatch, verifier
  registry.

## Tech stack — Astro + Cytoscape.js (decided 2026-05-28)

**Framework: [Astro](https://astro.build) 5.x.**

| Candidate | Why considered | Verdict |
|---|---|---|
| **Astro** | Markdown-native (frontmatter, MDX, code highlight, remark plugins), zero-JS by default with selective hydration ("islands"), tiny dep tree, ships static HTML. | **Chosen.** Best fit for "mostly docs, a few interactive widgets." |
| VitePress | Vue-based, great MD, theming opinionated. | Interactive widgets would need Vue components; less freedom for arbitrary canvas-based graphs. |
| Docusaurus | React + MDX, feature-rich. | Heaviest dep tree; overkill for ~5 pages + a few interactive panes. |

**Graph library: [Cytoscape.js](https://js.cytoscape.org/) 3.x.**

| Candidate | Why considered | Verdict |
|---|---|---|
| **Cytoscape.js** | Framework-agnostic (no React/Vue coupling), good built-in layouts (cose, breadthfirst, dagre via extension), click/hover handlers, MIT. | **Chosen.** Pairs cleanly with Astro islands. |
| React Flow | Excellent DX, but forces React. | Would inflate the dep tree for one widget. |
| d3-force | Maximum flexibility. | Too much hand-rolled glue for the time budget. |

## Source-of-truth strategy

The markdown lives in `docs/DEMO_*.md` (canonical). The web site **does not
duplicate** that content. Astro's content collection points at
`../DEMO_*.md` relative to `docs/web/src/content/demos/`. When the canonical
markdown lands on master via [PR #136](https://github.com/NikashPrakash/dot-agents/pull/136),
this site picks it up automatically on next build.

Until PR #136 merges, local builds need the files staged into
`docs/DEMO_*.md` (e.g. via `git show origin/feature/docs-for-demo:docs/<file>.md`).
The build will warn if any demo file is missing rather than failing hard.

## Develop

```bash
cd docs/web
npm install
npm run dev          # local dev server with HMR on http://localhost:4321
```

## Build

```bash
cd docs/web
npm run build        # outputs static site to dist/
npm run preview      # serve dist/ locally to verify
```

The built artifact is plain HTML/CSS/JS and can be served by any static host
(GitHub Pages, S3, Netlify, etc.). A follow-up task will wire `.github/workflows/`
to deploy `dist/` to GitHub Pages on push to master.

## Project layout

```
docs/web/
  package.json
  astro.config.mjs
  tsconfig.json
  README.md                 ← you are here
  public/                   ← static assets served as-is
  src/
    content/
      config.ts             ← Astro content collection schemas
      demos/                ← symlinks/aliases to ../../DEMO_*.md
    data/
      da-resources.json     ← hand-curated graph data: skills, agents, rules, hooks, MCP, plugins
      workflow-resources.json ← graph: plans, tasks, slices, delegations, lessons
      workspace-state.json  ← snapshot of .agents/active/ for the live-ish view
      tier-model.json       ← spec/plan/tasks/history tier model
    layouts/
      BaseLayout.astro      ← shell: sidebar, header, theme
    components/
      Sidebar.astro
      MermaidBlock.astro    ← renders ```mermaid blocks via mermaid 11
      ResourceGraph.astro   ← Cytoscape island for da/workflow/workspace graphs
      TierModel.astro       ← simpler expandable tier diagram
    pages/
      index.astro           ← landing
      demos/[...slug].astro ← dynamic route for demo markdown pages
      graphs/
        da-resources.astro
        workflow-resources.astro
        workspace-state.astro
      diagrams/
        tier-model.astro
        lens-dispatch.astro       ← stub for follow-up
        verifier-registry.astro   ← stub for follow-up
    styles/
      global.css
    lib/
      load-markdown.ts      ← shared helper for ingesting ../DEMO_*.md
```

## What's in this first version vs follow-up

**This PR delivers:**

- All 5 DEMO pages render with sidebar + anchors + Mermaid + code highlight.
- One fully interactive resource graph (**da resources** — skills, agents,
  rules, hooks, MCP, plugins) with click-to-inspect side panel.
- Workflow resources graph populated from the current active plans
  (`.agents/active/`) — clickable, layout via cose.
- Workspace state snapshot view (worktrees + in-flight delegations + eligible queue).
- Tier model diagram (spec → plan → tasks → history) with click-to-expand.

**Follow-up tasks (out of scope for this PR):**

- Lens dispatch diagram (stub page only).
- Verifier registry / app_type_verifier_map diagram (stub page only).
- GitHub Pages publish workflow under `.github/workflows/`.
- Live JSON ingestion for workspace state (currently a snapshot committed
  into `src/data/workspace-state.json`).
