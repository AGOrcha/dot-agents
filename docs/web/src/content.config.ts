import { defineCollection, z } from 'astro:content';
import { docsSchema } from '@astrojs/starlight/schema';
import { promises as fs } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { PUBLIC_PAGES } from './public-pages.mjs';

// Visibility partition (dm3 / D4): every content entry is `public` or `internal`,
// defaulting to `internal` (fail-safe — anything we forget to classify stays
// hidden from the public build). INTERNAL_BUILD=1 (set by `astro build` in the
// build:internal pass) additionally loads the .agents/** lessons/specs/proposals
// into the same docs collection so they get routes + sidebar groups; with the
// flag off those entries are never loaded, which is what keeps them out of dist/.
const INTERNAL_BUILD = process.env.INTERNAL_BUILD === '1';

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

// PUBLIC allowlist (PUBLIC_PAGES) is imported from ./public-pages.mjs above:
// { src (repo-relative), id (section-prefixed slug), order }. `id` doubles as
// the route slug and the sidebar-group bucket; the empty/`index` id is the site
// root + Getting Started entry. The list is shared with the link-rewrite rehype
// plugin (astro.config.mjs) so in-site slug resolution has one source of truth.

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
  // One-level-deep mapping key currently open (e.g. `sidebar:`), so indented
  // child lines are captured as dotted keys (`sidebar.order`). The repo docs
  // only nest `sidebar`, so a single level of nesting is enough; deeper or
  // sequence nesting is intentionally ignored to keep this scrape malformation-proof.
  let parentKey = '';
  for (const rawLine of block.split('\n')) {
    const line = rawLine.trimEnd();
    if (!line.trim() || line.trimStart().startsWith('#')) continue;
    const indented = /^\s/.test(rawLine);
    const m = /^\s*([A-Za-z_][A-Za-z0-9_-]*)\s*:\s*(.*)$/.exec(line);
    if (!m) continue;
    let val = m[2];
    if ((val.startsWith('"') && val.endsWith('"')) || (val.startsWith("'") && val.endsWith("'"))) {
      val = val.slice(1, -1);
    }
    if (!indented) {
      // Top-level key. A bare `key:` (empty value) opens a nested mapping.
      parentKey = val === '' ? m[1].toLowerCase() : '';
      if (val === '' || val.startsWith('[') || val.startsWith('{') || val === '|' || val === '>') continue;
      fm[m[1].toLowerCase()] = val;
    } else if (parentKey) {
      // Child of the open mapping → store as `parent.child`.
      if (val.startsWith('[') || val.startsWith('{') || val === '|' || val === '>') continue;
      fm[`${parentKey}.${m[1].toLowerCase()}`] = val;
    }
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

        // Sidebar order: prefer the doc's own frontmatter (`sidebar.order`),
        // fall back to the allowlist `order` (README carries no frontmatter).
        const fmOrder = Number(frontmatter['sidebar.order']);
        const order = Number.isFinite(fmOrder) ? fmOrder : page.order;
        const sidebarLabel = frontmatter['sidebar.label'];

        const data = await parseData({
          id: page.id,
          data: {
            title,
            // PUBLIC allowlist entries are the curated 4 sections (dm3 / D4).
            visibility: 'public',
            ...(frontmatter.description ? { description: frontmatter.description } : {}),
            sidebar: { order, ...(sidebarLabel ? { label: sidebarLabel } : {}) },
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

      // ── INTERNAL pass (dm3 / D4) ───────────────────────────────────────────
      // Only when INTERNAL_BUILD: ALSO load the .agents/** artifacts into THIS
      // same docs collection under section-prefixed ids (internal/lessons/*,
      // internal/specs/*, internal/proposals/*) so Starlight's autogenerate
      // sidebar + [...slug] route pick them up. With the flag off none of this
      // runs, so internal pages never reach the public dist/.
      if (!INTERNAL_BUILD) return;

      // (dir, fileGlob, idFor) for each internal section. Each yields a list of
      // { abs, id } the loader renders exactly like a public page.
      type InternalSource = { abs: string; id: string };
      async function collectInternal(): Promise<InternalSource[]> {
        const out: InternalSource[] = [];

        // Lessons: .agents/lessons/<name>/LESSON.md → internal/lessons/<name>
        const lessonsDir = path.join(REPO_ROOT, '.agents/lessons');
        try {
          for (const name of (await fs.readdir(lessonsDir, { withFileTypes: true }))) {
            if (!name.isDirectory()) continue;
            out.push({
              abs: path.join(lessonsDir, name.name, 'LESSON.md'),
              id: `internal/lessons/${name.name}`,
            });
          }
        } catch {
          logger?.warn?.('repo-internal-docs: SKIP missing .agents/lessons');
        }

        // Specs: .agents/workflow/specs/<id>/design.md → internal/specs/<id>
        const specsDir = path.join(REPO_ROOT, '.agents/workflow/specs');
        try {
          for (const name of (await fs.readdir(specsDir, { withFileTypes: true }))) {
            if (!name.isDirectory()) continue;
            out.push({
              abs: path.join(specsDir, name.name, 'design.md'),
              id: `internal/specs/${name.name}`,
            });
          }
        } catch {
          logger?.warn?.('repo-internal-docs: SKIP missing .agents/workflow/specs');
        }

        // Proposals: .agents/proposals/<name>.md → internal/proposals/<name>
        const proposalsDir = path.join(REPO_ROOT, '.agents/proposals');
        try {
          for (const ent of (await fs.readdir(proposalsDir, { withFileTypes: true }))) {
            if (!ent.isFile() || !ent.name.endsWith('.md')) continue;
            out.push({
              abs: path.join(proposalsDir, ent.name),
              id: `internal/proposals/${ent.name.replace(/\.md$/, '')}`,
            });
          }
        } catch {
          logger?.warn?.('repo-internal-docs: SKIP missing .agents/proposals');
        }

        return out;
      }

      let internalCount = 0;
      for (const src of await collectInternal()) {
        let raw: string;
        try {
          raw = await fs.readFile(src.abs, 'utf8');
        } catch {
          logger?.warn?.(`repo-internal-docs: SKIP missing source ${path.relative(REPO_ROOT, src.abs)}`);
          continue;
        }
        const { frontmatter, body } = splitFrontmatter(raw);
        const slugFallback = src.id.split('/').pop()!;
        const title = frontmatter.title || deriveTitle(body, slugFallback);
        const filePath = path.relative(siteRoot, src.abs).split(path.sep).join('/');
        const data = await parseData({
          id: src.id,
          data: {
            title,
            visibility: 'internal',
            ...(frontmatter.description ? { description: frontmatter.description } : {}),
          },
          filePath,
        });
        const rendered = await renderMarkdown(body);
        store.set({
          id: src.id,
          data,
          body,
          filePath,
          digest: generateDigest(raw),
          rendered,
        });
        internalCount++;
      }
      logger?.info?.(`repo-internal-docs: loaded ${internalCount} internal page(s) from ${REPO_ROOT}/.agents`);
    },
  };
}

export const collections = {
  docs: defineCollection({
    loader: repoDocsLoader(),
    // Visibility partition (dm3 / D4): default `internal` is fail-safe — any
    // entry not explicitly marked public stays out of the public dist/.
    schema: docsSchema({
      extend: z.object({
        visibility: z.enum(['public', 'internal']).default('internal'),
      }),
    }),
  }),
};
