# dm2-deferred — interactive pages parked for the dm2 slice

These are the bespoke interactive routes from the pre-Starlight custom site.
They are **not** part of the dm1 Starlight foundation build: they import the old
`BaseLayout.astro`, which imported the removed `Sidebar.astro`, so they cannot
build against the Starlight shell as-is.

Per the docs-starlight-migration plan, **dm2** ports these into the Starlight
project as custom Astro routes living alongside Starlight (Starlight does not own
`/graphs` or `/diagrams`) and links them from the **Concepts** section. To keep
the dm1 build green they were moved out of `src/` (Astro only routes `src/pages/`
and only compiles components reachable from the build) rather than left to break
the build.

## What's here

- `pages/graphs/*.astro` — Cytoscape `ResourceGraph` views (da-resources,
  workflow-resources, workspace-state).
- `pages/diagrams/*.astro` — JSON-driven diagrams (tier-model, lens-dispatch,
  verifier-registry).
- `components/` — `ResourceGraph.astro`, `MermaidRenderer.astro`,
  `ThemeToggle.astro`.
- `layouts/BaseLayout.astro` — old custom shell (dm2 reworks these pages to use
  Starlight's `<StarlightPage>` / custom layout instead).
- `data/*.json` — the JSON the graphs/diagrams read.
- `styles/global.css` — old site stylesheet.

## dm2 work

1. Move `pages/graphs` and `pages/diagrams` back under `src/pages/`.
2. Re-home the data JSON under `src/data/` and the components under
   `src/components/` (the `@data/*`, `@components/*`, `@layouts/*` tsconfig path
   aliases still point at `src/*`).
3. Replace the `BaseLayout` import with a Starlight-compatible layout (e.g.
   `StarlightPage` from `@astrojs/starlight/components`) so the interactive pages
   inherit the Starlight chrome, and add Concepts-section sidebar links to them.
4. `astro-mermaid` (added in dm1) now renders ```mermaid fences globally, so the
   client-side `MermaidRenderer.astro` may be retired during dm2.
