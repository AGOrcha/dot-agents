# Docs site usability review — PR #143 (`feature/docs-interactive-html`)

Hands-on usability test of the local Astro dev site at
`http://localhost:4321/dot-agents/`, driven via Playwright. Captured 2026-05-28
against `docs-interactive-html` worktree commit `88aaae14`.

Screenshots referenced below live in `usability-review-shots/`.

## §1 Summary

Overall the site is **demonstrably functional and content-complete**:
all 14 top-level routes return 200, all 103 canonical entries (20 docs + 34
lessons + 30 specs + 19 proposals) are surfaced, all three Cytoscape graphs
mount and render with non-empty canvases, Mermaid SVGs replace raw fenced
blocks, Shiki highlights code blocks, and the 3-way theme toggle persists to
`localStorage` and re-skins the graphs live without a reload.

The **top-3 blocking issues** are:
1. **Mobile layout overflow** — the 280px sticky sidebar does not collapse
   below a breakpoint, so at 375×667 the page has `scrollWidth=626` against a
   `innerWidth=375` viewport (horizontal scroll, content squeezed to 316px).
2. **Demo page titles render as `"DEMO demo readme — dot-agents"`** in the
   browser tab — the `titleFromId` fallback in `src/pages/demos/[...slug].astro`
   only strips the uppercase `DEMO_` prefix, but content collection IDs use
   lowercase `demo_readme.md`, so the prefix is duplicated. Every demo tab
   reads like a placeholder.
3. **Two `<h1>` per page** — the sidebar brand `<h1>dot-agents</h1>` competes
   with the page `<h1>` on every route. Document outline / SEO / screen-reader
   landmark all degrade.

The **top-3 polish issues** are: 5 Cytoscape console warnings per graph page
(wheelSensitivity + deprecated `label` sizing), no in-content cross-links to
the new `/canonical/` surface (the demo pages still link to
`github.com/.../blob/master/docs/*.md` even when the same file is now hosted
locally), and a 300KB unused `agorcha-ouroboros.png` that ships in `public/`
but is never referenced (only the 512 variant is wired up).

## §2 Findings (per scenario)

### A. Landing + navigation

- ✅ Landing `/dot-agents/` renders cleanly in <200ms FCP (148ms measured),
  hero paragraph + "Start here" cards convey what the site is within the
  first viewport. Title `"Home — dot-agents"` reads correctly.
- ✅ All 14 routes I enumerated return 200 (Home, About, canonical index,
  4 demo pages + demo index, 3 graph pages, 3 diagram pages).
- ✅ Brand mark (`branding/agorcha-ouroboros-512.png`) loads (200) and acts
  as a home link via `<a href="/dot-agents/">`. 📸 `A-landing-system.png`,
  `A-landing-dark.png`, `A-landing-light.png`, `F-landing-light.png`.
- ⚠ The brand `<img>` uses `alt=""` (decorative). For an identity mark that
  doubles as the home link, an empty alt is acceptable since the link has its
  own `aria-label`, but a `role="presentation"` would make intent explicit.
- ⚠ Sidebar nav `<h2>` headings ("Overview", "Demo material", "Canonical",
  "Resource graphs", "Diagrams") all render as **all-caps** via CSS
  `text-transform`, but the source is mixed-case. Cosmetic mismatch; mildly
  jarring when scanning the DOM vs the rendered page.
- ❌ **Mobile (375×667): sidebar does not collapse.** Sticky 280px column
  remains visible; main column squeezed to 316px; total scrollWidth = 626.
  📸 `A-landing-mobile.png`.

### B. Demo content (5 pages)

- ✅ All 5 demo pages render: `demo_index`, `demo_readme`, `demo_diagram`,
  `demo_workflow_walkthrough`, `demo_lessons_narrative` — each returns 200,
  rendered length > 1KB, Mermaid blocks become `<svg id="mermaid…">` (2 SVGs
  on `demo_diagram`, 2 on `demo_workflow_walkthrough`), Shiki produces
  `.astro-code` highlighted `<pre>` blocks where source has fences. 📸
  `B-demo-readme.png`, `B-demo-diagram.png`, `B-demo-workflow.png`,
  `B-demo-lessons.png`, `B-demo-index.png`.
- ✅ "On this page" right-rail TOC renders from `headings` and links to the
  `#slug` anchors in the article. Validated on `demo_readme`: 6 entries
  matching the file's H1+H2s.
- ✅ Demo→demo cross-links work (e.g. `demo_readme` links to
  `/dot-agents/demos/demo_diagram/`, all five resolve to 200).
- ❌ **Page titles read `"DEMO demo readme — dot-agents"`** etc. — the
  `titleFromId` strips uppercase `DEMO_` but the collection IDs are
  lowercase, so the literal `"DEMO "` prefix gets pre-pended on top of the
  already-lowercased `"demo readme"`. See `src/pages/demos/[...slug].astro:17-21`.
- ⚠ The `demo_index` page does NOT contain "preview cards" (the scenario
  asked for them) — it renders the source `docs/DEMO_INDEX.md` markdown
  as-is, which is a plain bulleted link list. Acceptable as MVP, but the
  landing page's `Start here` card grid is more usable; the demo index page
  could adopt the same card layout.
- ⚠ Many in-content links point to
  `https://github.com/NikashPrakash/dot-agents/blob/master/docs/*.md` even
  when the same file is now a local `/dot-agents/canonical/docs/…` route.
  External links open out of the site for content that exists locally.

### C. Canonical content (new bucket)

- ✅ `/canonical/` index lists exactly 103 entries (20 docs / 34 lessons /
  30 specs / 19 proposals) — matches `find` counts on the source dirs;
  **zero entries dropped by the tolerant frontmatter loader.** 📸
  `C-canonical-index.png`.
- ✅ Random spot-checks: `canonical/docs/global_flag_contract/`,
  `canonical/docs/active_legacy_plan_migration/`,
  `canonical/lessons/sidecar-manifest-pattern/`,
  `canonical/lessons/rfc-resolves-plan/`,
  `canonical/specs/go-test-fixture-extraction/`,
  `canonical/specs/coverage-gate-per-file/`,
  `canonical/proposals/codex-hooks-agents-linking-gap/`,
  `canonical/proposals/testcontainers-separate-module/` — all 200, content
  bodies render fully (lesson body length matches source minus frontmatter).
  📸 `C-canonical-lesson.png`.
- ⚠ Sidebar links to bucket anchors (`/canonical/#docs`, `…#lessons`, etc.)
  rather than bucket landing routes. `/canonical/docs/` returns 404 (no
  index page exists). Anchors work, but users guessing the URL hit a 404.
- ⚠ Canonical page titles use the raw slug
  (`"lessons: sidecar-manifest-pattern — dot-agents"`) — readable but a
  human-friendly title from frontmatter (e.g. lesson `name:` field) would
  scan better in tabs and bookmarks.

### D. Resource graphs (3 Cytoscape pages)

- ✅ All three graphs mount and produce non-empty canvases:
  - `da-resources` — confirmed `cy.nodes().length > 0`; sample nodes
    `scaffold:home`, `agent:loop-worker`, `agent:verifier` exist.
  - `workflow-resources` — 27 nodes (`plan:pr10`, 25 task nodes,
    `delegation:` nodes).
  - `workspace-state` — 19 nodes (3 plans, worktrees, delegations).
    📸 `D-graph-da-resources.png`, `D-graph-workflow.png`,
    `D-graph-workspace.png`.
- ✅ Node clicks populate the inspector panel: tapping `scaffold:home`,
  `agent:loop-worker`, `agent:verifier` produced inspector HTML lengths
  608 / 349 / 370 with the correct `<h3>` label, kind badge, and payload
  rows. 📸 `D-graph-da-inspector.png`.
- ✅ Theme re-skin works: `--graph-label` CSS var swaps from `#0f172a`
  (light) to `#e5edf7` (dark) on theme change and the `theme-changed`
  event triggers `cy.style(buildStyle(...))`. 📸 `F-graph-da-light.png`.
- ⚠ 5 Cytoscape console warnings per graph page load:
  - `wheelSensitivity` custom value warning (cytoscape recommends 1.0; site
    uses 0.2 for smoother zoom)
  - `label` deprecated for `width` / `height` (`buildStyle` in
    `ResourceGraph.astro:144-146`)
  - Two more echo on each `theme-changed` re-style.
- ⚠ Inspector renders to the **right** of the graph but on a narrow
  viewport (or when the graph canvas is wide) the inspector can be pushed
  off-screen. No mobile breakpoint for it.
- ⚠ No "node not found" state if `cy` fails to mount — the inspector
  silently keeps the placeholder text.

### E. Diagrams (tier model + 2 stubs)

- ✅ `/diagrams/tier-model/` renders 4 cards (Spec / Plan / Tasks /
  History), clicking a card opens its detail section (`tier-tasks`
  validated programmatically — `.tier-detail.open` toggles on click,
  `scrollIntoView({behavior:'smooth'})` runs). 📸 `E-tier-model.png`.
- ✅ Lens-dispatch stub explains what's coming with concrete pointers
  (`pr-a-lens-agents` worktree, `staged-runtime.md` § Review stage,
  per-lens agent paths). Stubbing message is honest and actionable. 📸
  `E-lens-dispatch.png`.
- ✅ Verifier-registry stub similar. 📸 `E-verifier-registry.png`.
- ⚠ Tier cards on first load auto-open `tier-spec` but the visual
  affordance that the OTHER cards are clickable is subtle (just
  `role="button"` + `cursor:pointer`). A clearer hover-state arrow or
  "click to expand" hint would help discoverability.

### F. Theme toggle

- ✅ 3-way toggle (`light` / `dark` / `system`) works on every page;
  `aria-pressed` correctly tracks state.
- ✅ Persistence via `localStorage['theme']` (key is literally `theme`,
  not `da-theme`). Reload preserves choice; clearing storage falls back
  to system.
- ✅ "system" branch correctly removes `data-theme` attribute and
  re-evaluates `prefers-color-scheme` media query.
- ✅ No FOUC observed on reload — the early-paint script in
  `BaseLayout.astro` applies the stored theme synchronously before first
  paint.
- ⚠ Three buttons are styled in lowercase source (`<button>light</button>`)
  but render Title-Cased on tier-model page and lowercased on landing —
  inconsistent CSS `text-transform` between page contexts. Cosmetic.

### G. Performance + accessibility

(Lighthouse not run; dev-mode metrics + DOM/CSS audit instead — dev-mode
sizes are unrepresentative because of HMR shims.)

- ✅ Landing dev FCP 148ms, DCL 19.7ms, transferSize 35KB encoded for the
  initial HTML.
- ⚠ Cytoscape bundle (`chunk-MBSZHDJG.js`) is the largest dependency; in
  prod the graph pages should be measured for bundle size. The Cytoscape
  import is currently per-page (not shared layout chunk).
- ❌ **Two `<h1>` per page** on every route: the sidebar `<h1>dot-agents</h1>`
  + each page's content `<h1>`. Breaks document outline; screen-readers
  announce duplicate top-level landmarks.
- ⚠ `<img alt="">` on the brand mark inside a link with `aria-label` is
  defensible but `role="presentation"` would be more explicit.
- ⚠ No `<main>` landmark with `role="main"` — only the implicit `<main>`
  element. Adequate but no skip-link to jump past the sidebar nav.
- ⚠ 308KB unused `public/branding/agorcha-ouroboros.png` ships to prod
  (only the 67KB 512-variant is referenced).
- ⚠ Default Astro 404 page (purple gradient, no sidebar) takes over on
  bad URLs — jarring brand discontinuity.
- ✅ Light-theme contrast (computed from CSS tokens): text `#0f172a`
  on bg `#f8fafc` is ~16:1 (AAA); text-dim `#5b6878` on bg is ~5.5:1
  (AA pass for normal text). Accent `#0369a1` is high-contrast.

## §3 Improvement task list (prioritized)

### P0 (blocking — must fix before public launch)

1. **Title fallback in demos route duplicates `DEMO ` prefix.**
   File: `src/pages/demos/[...slug].astro:17-21`. Change
   `replace(/^DEMO_/, '')` to `replace(/^demo_/i, '')` and drop the
   uppercase `DEMO ` prefix in the fallback string. ~3 LOC. Affects every
   demo tab title.
2. **Sidebar `<h1>` collides with page `<h1>` (a11y + SEO).**
   File: `src/components/Sidebar.astro:73`. Demote brand to `<span>`
   (with `aria-label` already provided by the wrapping `<a>`) or `<div
   role="heading" aria-level="2">`. ~2 LOC. Affects every page.
3. **Mobile viewport: sidebar does not collapse, causes horizontal
   overflow at ≤640px.** File: `src/styles/global.css` (or wherever
   `.sidebar { width: 280px; position: sticky }` lives). Add a media
   query to switch the layout to single-column with a hamburger
   disclosure below ~720px. ~30 LOC. Affects every page on phones.
   📸 `A-landing-mobile.png`.

### P1 (UX polish — visible degradation, batch into one followup)

Tracked as `docs-ux-polish-batch`:

- **Cytoscape deprecation warnings:** swap `label` → numeric `auto-sized`
  pattern in `ResourceGraph.astro:144-146`; either suppress
  `wheelSensitivity` warning by accepting 1.0 default or document the
  trade-off. (3 warnings × 3 graph pages = noisy console.)
- **External-vs-local-link drift:** demo pages link to
  `github.com/.../blob/master/docs/*.md` for files now hosted at
  `/canonical/docs/*`. Add a build-time rewrite (or content-pipeline
  remark plugin) that translates known repo paths to canonical routes.
- **Inspector layout on narrow viewports:** below ~960px, the
  `.graph-page` grid should stack inspector below the canvas instead of
  beside.
- **Canonical bucket landing pages 404:** add `pages/canonical/docs/index.astro`
  (and `lessons/`, `specs/`, `proposals/`) so guessed URLs resolve.
  Currently only the hash anchors `/canonical/#docs` etc. work.
- **Demo index page lacks card layout** — adopt the landing-page
  `Start here` card grid for the 5 demos.
- **Tier-model card affordance:** add a visible "click to expand" cue or
  a chevron icon — current cursor-only affordance is subtle.
- **Theme button capitalization inconsistent** — pick one
  (`text-transform: capitalize` or none) and apply uniformly.
- **Page titles for canonical entries** could pull a human-readable
  title from frontmatter (`name:` for lessons) instead of raw slug.
- **Custom 404 page** matching the site shell (sidebar + theme).
- **Drop unused `public/branding/agorcha-ouroboros.png`** (308KB) — only
  the 512 variant is referenced; full-size png is dead weight in
  `public/`.
- **Skip-link** at the top of `<body>` jumping to `<main>` for keyboard
  users so they don't tab through the 30+ sidebar links every page.
- **Brand `<img alt="">`** → add `role="presentation"` for explicit
  decorative intent.

### P2 (nice-to-have)

- **In-page TOC styling**: the `<aside>` TOC uses inline styles instead
  of a class. Move to a scoped component for easier theming.
- **Sidebar nav uses `<h2>` per section** — visually all-caps suggests
  they're labels, not headings. Consider `aria-label` on the `<nav>`
  with role-less section labels.
- **Cytoscape chunk** could be loaded only when a graph page is on
  screen (dynamic `import('cytoscape')` inside `mountGraph` to keep
  non-graph pages slim).
- **Sidebar collapse-section UX**: 5 sections × multiple items is a long
  scroll. Collapsible sections would compress.
- **Add an "Edit on GitHub" link** per page (footer of `BaseLayout`)
  routing to the source markdown — useful for a docs site that mirrors
  repo content.
- **Pre-render the cytoscape graphs to SVG fallback** so users with JS
  disabled see at least a static diagram.

## §4 Surprises

- **AUDIT.md is shipped in the docs site root** (`docs/web/AUDIT.md`).
  It's a 98-line internal review note that should be deleted before
  merging (it even says so: "Resolve or accept each item, then delete
  this file before merging"). Not user-facing because it's not routed,
  but it lives next to the package.json and ships in any tarball.
- The `_cyreg.cy` instance is reachable from the DOM (Cytoscape stashes
  itself on the container element). Useful for the graph re-skin code,
  but means a third-party script could mutate the graph. Not a security
  concern for a docs site, but worth knowing.
- The "Inspect" / "Audit" / "Settings" / "Menu" buttons that appear in
  the lower-right of every page snapshot are the **Astro Dev Toolbar**
  injected by `astro dev`. They will NOT ship in `astro build` — they're
  visible only in screenshots taken from the dev server. Not a finding,
  but flag for anyone reviewing the screenshots.
- **The tolerant frontmatter loader silently swallows ANY load failure**
  (`logger?.warn?.(\`tolerant-markdown loader failed…\`)`). If
  `.agents/lessons/` were ever empty or unreadable, the site would build
  with zero lessons and no surfaced error. Counts matched exactly today,
  but consider failing the build when expected entry count drops below
  a threshold.
- Console clean except for the 3 Cytoscape warnings on graph pages.
  Demo pages have zero JS noise.
- **Logo + title overlap area is clickable as one link** — including
  the empty padding around them. Good UX.
- The `/canonical/proposals/` bucket includes *this very review file*
  the moment it is committed; the docs site is recursively self-aware.

## Notes on scope

- This review is **read-only**: no `docs/web/` source files were edited.
- Performance was measured against the dev server only (HMR-shimmed
  transfer sizes are unrepresentative of prod). A full Lighthouse pass
  on `astro build && astro preview` is worth running before public launch
  but is out of scope here.
