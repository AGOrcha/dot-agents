// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import mermaid from 'astro-mermaid';
import { satteri } from '@astrojs/markdown-satteri';
import { defineHastPlugin } from 'satteri';
import { PUBLIC_PAGES, PUBLIC_SLUG_BY_SRC } from './src/public-pages.mjs';

// SITE_BASE / SITE_URL switch between the two supported hosts:
//   * Cloudflare Workers @ agorcha.dev (root)   — set DEPLOY_TARGET=cloudflare
//   * GitHub Pages  @ /dot-agents/   (default)  — any other value
// The Cloudflare CI workflow sets DEPLOY_TARGET=cloudflare so the built
// assets reference root-relative URLs instead of /dot-agents/...
const DEPLOY_TARGET = process.env.DEPLOY_TARGET ?? 'github-pages';
const IS_CLOUDFLARE = DEPLOY_TARGET === 'cloudflare';

// Visibility partition (dm3 / D4). The two-pass `pnpm run build` sets this flag
// only on the second (internal) pass:
//   * off → outDir dist/          : PUBLIC only (internal entries aren't loaded)
//   * on  → outDir dist-internal/ : EVERYTHING (public + internal sections)
// src/content.config.ts reads the same env flag independently to gate the loader.
const INTERNAL_BUILD = process.env.INTERNAL_BUILD === '1';
const SITE_URL = IS_CLOUDFLARE
  ? 'https://agorcha.dev'
  : 'https://nikashprakash.github.io';
const GITHUB_BLOB = 'https://github.com/AGOrcha/dot-agents/blob/master';

// Rewrite intra-doc markdown links. Every relative link is first resolved to a
// repo-relative path (public docs live under docs/, so a bare or `./` name is
// under docs/; `../` climbs to the repo root; an already-rooted `docs/X.md`
// stays as-is), then:
//   - a path that matches a PUBLIC page → its in-site Starlight slug, so the
//     reader stays on this site (covers `./X.md`, bare `X.md`, `../README.md`
//     and README's own `docs/X.md` links — including the DEMO_*.md guides)
//   - anything else → a github.com blob URL for the canonical repo doc
//
// PUBLIC_SLUG_BY_SRC is derived from the same PUBLIC_PAGES allowlist the content
// loader uses (src/public-pages.mjs) — one source of truth. Resolving to the
// repo-relative path also fixes the bare-filename 404: a bare `<name>.md` under
// docs/ no longer loses its `docs/` prefix.
//
// Ported to a Sätteri HAST plugin (Astro 6.4+ `markdown.processor` API —
// Sätteri does not run remark/rehype plugins, see astro.build/blog/astro-640).
// `ctx.setProperty` is Sätteri's mutation API: the visited node is a readonly
// snapshot of Rust-arena memory, so direct `node.properties.href = …`
// assignment (the old rehype-plugin style) is silently dropped.
function rewriteRelativeLinks() {
  return defineHastPlugin({
    name: 'rewrite-relative-links',
    element: {
      filter: ['a'],
      visit(node, ctx) {
        const href = node.properties?.href;
        if (typeof href !== 'string' || !href) return;
        // Skip absolute URLs, mailto, anchors, root-relative paths.
        if (/^(https?:|mailto:|#|\/)/i.test(href)) return;

        // Split off any #anchor, then resolve the path to a repo-relative form.
        const hashIdx = href.indexOf('#');
        const anchor = hashIdx >= 0 ? href.slice(hashIdx) : '';
        const pathPart = hashIdx >= 0 ? href.slice(0, hashIdx) : href;

        let repoPath = pathPart;
        if (repoPath.startsWith('./')) {
          repoPath = `docs/${repoPath.slice(2)}`;
        } else if (repoPath.startsWith('../')) {
          repoPath = repoPath.slice(3);
        } else if (!repoPath.includes('/') && repoPath.endsWith('.md')) {
          // Bare sibling .md filename on a docs/ page (the 404 bug class). Scoped
          // to .md so a bare repo-root file (LICENSE, NOTICE) on the README/index
          // page is NOT wrongly pushed under docs/.
          repoPath = `docs/${repoPath}`;
        }
        // else: already repo-relative (e.g. docs/X.md, .agents/foo, LICENSE) — leave as-is.

        // Public target → in-site link. Starlight prepends `base`; use a
        // root-relative path. The `index` page is the site root (`/`).
        const slug = PUBLIC_SLUG_BY_SRC.get(repoPath);
        if (slug) {
          ctx.setProperty(node, 'href', slug === 'index' ? `/${anchor}` : `/${slug}/${anchor}`);
          return;
        }

        // Non-public relative target → canonical GitHub blob URL.
        ctx.setProperty(node, 'href', `${GITHUB_BLOB}/${repoPath}${anchor}`);
        ctx.setProperty(node, 'target', '_blank');
        ctx.setProperty(node, 'rel', 'noopener');
      },
    },
  });
}

// Public information architecture — four curated sections (D3). Entries are
// sourced live from docs/** by src/content.config.ts's custom external-file
// loader, so their entry `filePath` points back into the repo (docs/**), NOT
// under src/content/docs/. Starlight's `autogenerate` only scans entries whose
// filePath is under src/content/docs/, so it finds NONE of these pages and the
// groups render EMPTY. Instead we build the items explicitly from the SAME
// allowlist the loader consumes (src/public-pages.mjs) — grouped by each id's
// section prefix and ordered by the allowlist `order` — so every public page is
// reachable from the nav and the sidebar stays in sync with the allowlist.
// `{ slug }` items carry no label, so Starlight uses each page's own
// loader-derived title (its H1) as the nav label.
function sectionItems(prefix) {
  return PUBLIC_PAGES.filter((p) => p.id.startsWith(`${prefix}/`))
    .sort((a, b) => a.order - b.order)
    .map((p) => ({ slug: p.id }));
}

const sidebar = [
  {
    label: 'Getting Started',
    items: [
      { label: 'Getting Started', slug: '' },
      { label: 'Install & onboard (one paste)', slug: 'guides/install' },
    ],
  },
  { label: 'Guides', items: sectionItems('guides') },
  { label: 'Reference', items: sectionItems('reference') },
  {
    label: 'Concepts',
    items: [
      ...sectionItems('concepts'),
      // Bespoke interactive routes (custom Astro pages, base-aware via link:).
      { label: 'Resource graph: da', link: '/graphs/da-resources' },
      { label: 'Resource graph: workflow', link: '/graphs/workflow-resources' },
      { label: 'Resource graph: workspace state', link: '/graphs/workspace-state' },
      { label: 'Diagram: tier model', link: '/diagrams/tier-model' },
      { label: 'Diagram: lens dispatch', link: '/diagrams/lens-dispatch' },
      { label: 'Diagram: verifier registry', link: '/diagrams/verifier-registry' },
    ],
  },
  // Internal-only sections (dm3 / D4): appended ONLY in the INTERNAL_BUILD pass,
  // so they exist in dist-internal/ but never in the public dist/.
  ...(INTERNAL_BUILD
    ? [
        { label: 'Lessons', items: [{ autogenerate: { directory: 'internal/lessons' } }] },
        { label: 'Specs', items: [{ autogenerate: { directory: 'internal/specs' } }] },
        { label: 'Proposals', items: [{ autogenerate: { directory: 'internal/proposals' } }] },
      ]
    : []),
];

// https://docs.astro.build/en/reference/configuration-reference/
export default defineConfig({
  site: SITE_URL,
  base: IS_CLOUDFLARE ? '/' : '/dot-agents/',
  trailingSlash: 'ignore',
  // Two-output visibility partition (dm3 / D4).
  outDir: INTERNAL_BUILD ? './dist-internal' : './dist',
  // astro-mermaid must run before Starlight so its ```mermaid → <pre class="mermaid">
  // transform happens ahead of Starlight's expressive-code / shiki pass.
  integrations: [
    // useMaxWidth:false makes Mermaid emit native pixel dimensions instead of
    // scaling SVGs down to the ~700px content column (which renders wide
    // flowcharts at ~0.15x — illegible). Paired with the overflow-x rules in
    // src/styles/custom.css so wide diagrams render full-size and scroll.
    mermaid({
      theme: 'default',
      autoTheme: true,
      mermaidConfig: {
        flowchart: { useMaxWidth: false },
        state: { useMaxWidth: false },
      },
    }),
    starlight({
      title: 'dot-agents',
      tagline: 'The operational layer for AI coding agents',
      // Brand favicon (agorcha ouroboros) — overrides Astro's default mark.
      // Derived from .agents/branding/agorcha-ouroboros into public/favicon.png.
      favicon: '/favicon.png',
      // Readability overrides for content wider than the column (wide Mermaid
      // diagrams + tables). See the file header for the rationale.
      customCss: ['./src/styles/custom.css'],
      social: [
        { icon: 'github', label: 'GitHub', href: 'https://github.com/AGOrcha/dot-agents' },
      ],
      sidebar,
    }),
  ],
  markdown: {
    // Rust-based Sätteri processor (astro.build/blog/astro-640) — faster than
    // the unified()-default pipeline. Sätteri doesn't run remark/rehype
    // plugins, so rewriteRelativeLinks is ported above to a Sätteri HAST
    // plugin; astro-mermaid (>=2.1.0) and @astrojs/starlight both detect and
    // configure themselves against `markdown.processor.name === 'satteri'`
    // natively, no porting needed for those two.
    processor: satteri({
      features: { directive: true },
      hastPlugins: [rewriteRelativeLinks],
    }),
  },
});
