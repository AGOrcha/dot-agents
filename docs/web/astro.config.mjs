// @ts-check
import { defineConfig } from 'astro/config';
import { visit } from 'unist-util-visit';

const SITE_BASE = '/dot-agents';
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
        node.properties.href = `${SITE_BASE}/demos/demo_${slug}/${anchor}`;
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
  site: 'https://nikashprakash.github.io',
  base: '/dot-agents/',
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
