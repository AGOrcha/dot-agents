// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import mermaid from 'astro-mermaid';
import { visit } from 'unist-util-visit';

// SITE_BASE / SITE_URL switch between the two supported hosts:
//   * Cloudflare Workers @ agorcha.dev (root)   — set DEPLOY_TARGET=cloudflare
//   * GitHub Pages  @ /dot-agents/   (default)  — any other value
// The Cloudflare CI workflow sets DEPLOY_TARGET=cloudflare so the built
// assets reference root-relative URLs instead of /dot-agents/...
const DEPLOY_TARGET = process.env.DEPLOY_TARGET ?? 'github-pages';
const IS_CLOUDFLARE = DEPLOY_TARGET === 'cloudflare';

// Visibility partition (dm3 / D4). The two-pass `npm run build` sets this flag
// only on the second (internal) pass:
//   * off → outDir dist/          : PUBLIC only (internal entries aren't loaded)
//   * on  → outDir dist-internal/ : EVERYTHING (public + internal sections)
// src/content.config.ts reads the same env flag independently to gate the loader.
const INTERNAL_BUILD = process.env.INTERNAL_BUILD === '1';
const SITE_URL = IS_CLOUDFLARE
  ? 'https://agorcha.dev'
  : 'https://nikashprakash.github.io';
const GITHUB_BLOB = 'https://github.com/AGOrcha/dot-agents/blob/master';

// DEMO_<NAME>.md source docs are surfaced under Guides as /guides/demo-<name>.
// Maps the repo filename to its Starlight slug so intra-doc links resolve
// in-site instead of going to GitHub.
const DEMO_SLUGS = new Map([
  ['DEMO_README', 'guides/demo-overview'],
  ['DEMO_INDEX', 'guides/demo-index'],
  ['DEMO_WORKFLOW_WALKTHROUGH', 'guides/demo-workflow-walkthrough'],
  ['DEMO_DIAGRAM', 'guides/demo-diagram'],
  ['DEMO_LESSONS_NARRATIVE', 'guides/demo-lessons'],
]);

// Rewrite intra-doc markdown links so that:
//   - ./DEMO_<NAME>.md   → in-site Starlight guide page (when it's a public guide)
//   - ./<other>.md       → GitHub blob URL (docs not surfaced in this site)
//   - ../.agents/<path>  → GitHub blob URL
function rewriteRelativeLinks() {
  return (/** @type {import('hast').Root} */ tree) => {
    visit(tree, 'element', (node) => {
      if (node.tagName !== 'a' || !node.properties?.href) return;
      const href = String(node.properties.href);
      // Skip absolute URLs, anchors, root-relative paths.
      if (/^(https?:|mailto:|#|\/)/i.test(href)) return;
      const demoMatch = href.match(/^\.\/DEMO_([A-Z_]+)\.md(#.*)?$/);
      if (demoMatch) {
        const key = `DEMO_${demoMatch[1]}`;
        const anchor = demoMatch[2] ?? '';
        const slug = DEMO_SLUGS.get(key);
        if (slug) {
          // Starlight prepends `base`; use a root-relative in-site link.
          node.properties.href = `/${slug}/${anchor}`;
          return;
        }
      }
      // Anything else relative: send to GitHub blob URL.
      // ./X.md -> docs/X.md ; ../.agents/foo -> .agents/foo
      let p = href;
      if (p.startsWith('./')) {
        p = `docs/${p.slice(2)}`;
      } else if (p.startsWith('../')) {
        p = p.slice(3);
      }
      node.properties.href = `${GITHUB_BLOB}/${p}`;
      node.properties.target = '_blank';
      node.properties.rel = 'noopener';
    });
  };
}

// Public information architecture — four curated sections (D3). Entries are
// sourced live from docs/** by src/content.config.ts; their section-prefixed
// ids feed these autogenerate groups. Internal artifacts (.agents/**) are
// absent from the public loader pass and so never appear here.
const sidebar = [
  {
    label: 'Getting Started',
    items: [{ label: 'Getting Started', slug: '' }],
  },
  { label: 'Guides', items: [{ autogenerate: { directory: 'guides' } }] },
  { label: 'Reference', items: [{ autogenerate: { directory: 'reference' } }] },
  {
    label: 'Concepts',
    items: [
      { autogenerate: { directory: 'concepts' } },
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
    mermaid({ theme: 'default', autoTheme: true }),
    starlight({
      title: 'dot-agents',
      tagline: 'The operational layer for AI coding agents',
      // Brand favicon (agorcha ouroboros) — overrides Astro's default mark.
      // Derived from .agents/branding/agorcha-ouroboros into public/favicon.png.
      favicon: '/favicon.png',
      social: [
        { icon: 'github', label: 'GitHub', href: 'https://github.com/AGOrcha/dot-agents' },
      ],
      sidebar,
    }),
  ],
  markdown: {
    rehypePlugins: [rewriteRelativeLinks],
  },
});
