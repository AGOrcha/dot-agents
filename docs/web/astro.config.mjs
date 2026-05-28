// @ts-check
import { defineConfig } from 'astro/config';
import { visit } from 'unist-util-visit';

// SITE_BASE / SITE_URL switch between the two supported hosts:
//   * Cloudflare Workers @ agorcha.dev (root)   — set DEPLOY_TARGET=cloudflare
//   * GitHub Pages  @ /dot-agents/   (default)  — any other value
// The Cloudflare CI workflow sets DEPLOY_TARGET=cloudflare so the built
// assets reference root-relative URLs instead of /dot-agents/...
const DEPLOY_TARGET = process.env.DEPLOY_TARGET ?? 'github-pages';
const IS_CLOUDFLARE = DEPLOY_TARGET === 'cloudflare';
const SITE_BASE = IS_CLOUDFLARE ? '' : '/dot-agents';
const SITE_URL = IS_CLOUDFLARE
  ? 'https://agorcha.dev'
  : 'https://nikashprakash.github.io';
const GITHUB_BLOB = 'https://github.com/NikashPrakash/dot-agents/blob/master';

// Rewrite intra-doc markdown links so that:
//   - ./DEMO_<NAME>.md       → /dot-agents/demos/demo_<name>/    (in-site page)
//   - ./<other>.md           → GH blob URL (other docs not yet in this site)
//   - ../.agents/<path>      → GH blob URL
function rewriteRelativeLinks() {
  return (tree) => {
    visit(tree, 'element', (node) => {
      if (node.tagName !== 'a' || !node.properties?.href) return;
      const href = String(node.properties.href);
      // Skip absolute URLs, anchors, etc.
      if (/^(https?:|mailto:|#|\/)/i.test(href)) return;
      const demoMatch = href.match(/^\.\/DEMO_([A-Z_]+)\.md(#.*)?$/);
      if (demoMatch) {
        const slug = demoMatch[1].toLowerCase();
        const anchor = demoMatch[2] ?? '';
        node.properties.href = `${SITE_BASE || ''}/demos/demo_${slug}/${anchor}`;
        return;
      }
      // Anything else relative: send to GitHub blob URL.
      // ./X.md -> /docs/X.md ; ../.agents/foo -> /.agents/foo
      let path = href;
      if (path.startsWith('./')) {
        path = `docs/${path.slice(2)}`;
      } else if (path.startsWith('../')) {
        path = path.slice(3);
      }
      node.properties.href = `${GITHUB_BLOB}/${path}`;
      node.properties.target = '_blank';
      node.properties.rel = 'noopener';
    });
  };
}

// Static-only build; no SSR adapter.
// Mermaid is rendered client-side inside MermaidRenderer.astro to avoid
// pulling a headless-browser dep into the build pipeline.
export default defineConfig({
  site: SITE_URL,
  base: IS_CLOUDFLARE ? '/' : '/dot-agents/',
  trailingSlash: 'ignore',
  build: {
    format: 'directory',
  },
  markdown: {
    syntaxHighlight: 'shiki',
    shikiConfig: {
      theme: 'github-dark-dimmed',
      wrap: true,
    },
    remarkPlugins: [],
    rehypePlugins: [rewriteRelativeLinks],
  },
  vite: {
    ssr: {
      noExternal: [],
    },
  },
});
