// Single source of truth for the PUBLIC docs allowlist (D2 / dm3).
//
// Both the content loader (src/content.config.ts) and the intra-doc
// link-rewrite rehype plugin (astro.config.mjs) import this list so each
// public page's in-site Starlight slug is declared exactly once. Keeping the
// allowlist here (rather than duplicated in the rewrite map) is what lets every
// public→public cross-link render as an in-site path instead of bouncing the
// reader off to a github.com blob URL.
//
// Each entry: { src (repo-relative path), id (section-prefixed slug == route),
// order (sidebar position) }. The empty/`index` id is the site root + Getting
// Started entry.
export const PUBLIC_PAGES = [
  // Getting Started ← README (install → init → first project).
  { src: 'README.md', id: 'index', order: 1 },

  // Guides
  { src: 'docs/GETTING_STARTED.md', id: 'guides/getting-started', order: 0 },
  { src: 'docs/LAYERED_CONFIG_GUIDE.md', id: 'guides/layered-config', order: 1 },
  { src: 'docs/HOOKS.md', id: 'guides/hooks', order: 2 },
  { src: 'docs/CONFIG_RELEVANCE.md', id: 'guides/config-relevance', order: 3 },
  { src: 'docs/DEMO_README.md', id: 'guides/demo-overview', order: 4 },
  { src: 'docs/DEMO_INDEX.md', id: 'guides/demo-index', order: 5 },
  { src: 'docs/DEMO_WORKFLOW_WALKTHROUGH.md', id: 'guides/demo-workflow-walkthrough', order: 6 },
  { src: 'docs/DEMO_DIAGRAM.md', id: 'guides/demo-diagram', order: 7 },
  { src: 'docs/DEMO_LESSONS_NARRATIVE.md', id: 'guides/demo-lessons', order: 8 },
  { src: 'docs/RESOURCE_MANAGEMENT_GUIDE.md', id: 'guides/resource-management', order: 9 },
  { src: 'docs/SCORE_GUIDE.md', id: 'guides/score', order: 10 },

  // Reference
  { src: 'docs/PLATFORM_DIRS_DOCS.md', id: 'reference/platform-dirs', order: 1 },
  { src: 'docs/GLOBAL_FLAG_CONTRACT.md', id: 'reference/global-flag-contract', order: 2 },
  { src: 'docs/ERROR_MESSAGE_CONTRACT.md', id: 'reference/error-message-contract', order: 3 },
  { src: 'docs/RESOURCE_COMMAND_CONTRACT.md', id: 'reference/resource-command-contract', order: 4 },
  { src: 'docs/PLUGIN_CONTRACT.md', id: 'reference/plugin-contract', order: 5 },
  { src: 'docs/WORKFLOW_CLIENT_COMMANDS.md', id: 'reference/workflow-client-commands', order: 6 },
  { src: 'docs/RELEASE_VERIFICATION.md', id: 'reference/release-verification', order: 7 },

  // Concepts
  { src: 'docs/PROJECT_DIAGRAMS.md', id: 'concepts/project-diagrams', order: 1 },
];

/**
 * Map a public page's repo-relative source path → its in-site Starlight slug.
 * e.g. `docs/HOOKS.md` → `guides/hooks`, `README.md` → `index` (site root),
 * `docs/DEMO_README.md` → `guides/demo-overview`. The link rewriter normalizes
 * every intra-doc link (`./X.md`, bare `X.md`, `../README.md`, `docs/X.md`) to
 * this repo-relative form, then looks it up here to keep public→public
 * cross-links in-site instead of bouncing to a github.com blob URL.
 */
export const PUBLIC_SLUG_BY_SRC = new Map(PUBLIC_PAGES.map((p) => [p.src, p.id]));
