import { defineCollection } from 'astro:content';
import { docsSchema } from '@astrojs/starlight/schema';
import { promises as fs } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

// ─────────────────────────────────────────────────────────────────────────────
// PUBLIC docs collection — sourced live from the repo's canonical markdown.
//
// Content stays canonical in `docs/**` (D2): we do NOT copy or fork the source
// docs into this site. A custom glob-style loader reads the PUBLIC allowlist
// straight from the repo, derives a Starlight `title` from each file's first
// H1 when frontmatter has none (the raw repo docs carry no frontmatter), and
// assigns each entry a section-prefixed id so Starlight's `autogenerate`
// sidebar groups (guides/ reference/ concepts/) pick them up. The README maps
// to the collection root (`index`) so it is both `/` and Getting Started.
//
// This is the production replacement for the spike's copy-prebuild
// (scripts/sync-content.mjs): no copy, no drift, no mass frontmatter edits.
//
// Internal artifacts (.agents/** lessons/specs/proposals, ADRs, internal design
// specs) are deliberately absent from the allowlist — visibility gating /
// internal sections are handled in a later slice (dm3), not here.
// ─────────────────────────────────────────────────────────────────────────────

// This file lives at docs/web/src/content.config.ts → repo root is three up.
const REPO_ROOT = path.resolve(fileURLToPath(new URL('.', import.meta.url)), '../../..');

// PUBLIC allowlist: { src (repo-relative), id (section-prefixed slug), order }.
// `id` doubles as the route slug and the sidebar-group bucket. The empty/`index`
// id is the site root + Getting Started entry.
const PUBLIC_PAGES: { src: string; id: string; order: number }[] = [
  // Getting Started ← README (install → init → first project).
  { src: 'README.md', id: 'index', order: 1 },

  // Guides
  { src: 'docs/HOOKS.md', id: 'guides/hooks', order: 1 },
  { src: 'docs/CONFIG_RELEVANCE.md', id: 'guides/config-relevance', order: 2 },
  { src: 'docs/DEMO_README.md', id: 'guides/demo-overview', order: 3 },
  { src: 'docs/DEMO_INDEX.md', id: 'guides/demo-index', order: 4 },
  { src: 'docs/DEMO_WORKFLOW_WALKTHROUGH.md', id: 'guides/demo-workflow-walkthrough', order: 5 },
  { src: 'docs/DEMO_DIAGRAM.md', id: 'guides/demo-diagram', order: 6 },
  { src: 'docs/DEMO_LESSONS_NARRATIVE.md', id: 'guides/demo-lessons', order: 7 },

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
 * Split a leading YAML frontmatter block off a markdown string. The raw repo
 * docs have none, but we tolerate one if present and never re-emit it (we only
 * scrape `title`/`description` for precedence). A shallow `key: value` scrape is
 * enough — we deliberately avoid js-yaml so a malformed block can't fail the
 * build (some repo markdown uses `: ` in plain scalars).
 */
function splitFrontmatter(raw: string): {
  frontmatter: Record<string, string>;
  body: string;
} {
  if (!raw.startsWith('---\n')) return { frontmatter: {}, body: raw };
  const end = raw.indexOf('\n---', 4);
  if (end < 0) return { frontmatter: {}, body: raw };
  const block = raw.slice(4, end);
  let body = raw.slice(end + 4);
  if (body.startsWith('\n')) body = body.slice(1);
  const fm: Record<string, string> = {};
  for (const rawLine of block.split('\n')) {
    const line = rawLine.trimEnd();
    if (!line.trim() || line.startsWith('#')) continue;
    const m = /^([A-Za-z_][A-Za-z0-9_-]*)\s*:\s*(.*)$/.exec(line);
    if (!m) continue;
    let val = m[2];
    if ((val.startsWith('"') && val.endsWith('"')) || (val.startsWith("'") && val.endsWith("'"))) {
      val = val.slice(1, -1);
    }
    if (val.startsWith('[') || val.startsWith('{') || val === '|' || val === '>') continue;
    fm[m[1].toLowerCase()] = val;
  }
  return { frontmatter: fm, body };
}

/** Derive a title from the first H1 of the body; fall back to the slug. */
function deriveTitle(body: string, fallback: string): string {
  const m = body.match(/^#\s+(.+)$/m);
  if (!m) return fallback;
  // Strip inline code backticks / emphasis markers for a clean nav title.
  return m[1].replace(/[`*_]/g, '').trim();
}

/**
 * Custom loader: read each PUBLIC allowlist file straight from the repo, derive
 * a Starlight-valid `title` (frontmatter title → first H1 → slug), and store a
 * fully-rendered entry under its section-prefixed id. Modeled on Astro's own
 * glob loader contract (parseData + renderMarkdown + store.set) so entries are
 * indistinguishable from `docsLoader()` ones — Starlight's nav, search and
 * sidebar `autogenerate` all work unchanged.
 */
function repoDocsLoader() {
  return {
    name: 'repo-public-docs-glob',
    async load({ store, parseData, generateDigest, renderMarkdown, config, logger }: any) {
      const siteRoot = config?.root
        ? fileURLToPath(config.root)
        : path.join(REPO_ROOT, 'docs/web');
      store.clear();
      let count = 0;
      for (const page of PUBLIC_PAGES) {
        const absPath = path.join(REPO_ROOT, page.src);
        let raw: string;
        try {
          raw = await fs.readFile(absPath, 'utf8');
        } catch {
          logger?.warn?.(`repo-public-docs: SKIP missing source ${page.src}`);
          continue;
        }
        const { frontmatter, body } = splitFrontmatter(raw);
        const slugFallback = page.id === 'index' ? 'index' : page.id.split('/').pop()!;
        const title = frontmatter.title || deriveTitle(body, slugFallback);
        const filePath = path.relative(siteRoot, absPath).split(path.sep).join('/');

        const data = await parseData({
          id: page.id,
          data: {
            title,
            ...(frontmatter.description ? { description: frontmatter.description } : {}),
            sidebar: { order: page.order },
          },
          filePath,
        });
        const rendered = await renderMarkdown(body);
        store.set({
          id: page.id,
          data,
          body,
          filePath,
          digest: generateDigest(raw),
          rendered,
        });
        count++;
      }
      logger?.info?.(`repo-public-docs: loaded ${count}/${PUBLIC_PAGES.length} public page(s) from ${REPO_ROOT}`);
    },
  };
}

export const collections = {
  docs: defineCollection({ loader: repoDocsLoader(), schema: docsSchema() }),
};
