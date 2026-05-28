import { defineCollection, z } from 'astro:content';
import { glob } from 'astro/loaders';
import { promises as fs } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

// Glob `base` is resolved relative to the Astro project root (docs/web/),
// not this file. So `../` reaches `docs/` and `../../` reaches the repo root.

// Demo markdown collection: source-of-truth lives in ../docs/DEMO_*.md
// (i.e. docs/DEMO_*.md relative to repo root). We ingest by glob loader from
// the parent directory so the canonical files are NOT duplicated under web/.
const demos = defineCollection({
  loader: glob({
    pattern: 'DEMO_*.md',
    base: '../',
  }),
  schema: z.object({
    title: z.string().optional(),
  }),
});

// Canonical (non-demo) content surfaces the rest of the repo's authoritative
// markdown in the same way /demos/* surfaces DEMO_*.md. Four source buckets:
//
//   1. docs (`docs/*.md` minus DEMO_*.md)
//   2. lessons (`.agents/lessons/<slug>/LESSON.md`)
//   3. specs (`.agents/workflow/specs/<slug>/design.md`)
//   4. proposals (`.agents/proposals/*.md`)

const docsCanonical = defineCollection({
  loader: glob({
    pattern: ['*.md', '!DEMO_*.md'],
    base: '../',
  }),
  schema: z.object({
    title: z.string().optional(),
  }),
});

// Repo-root resolver — this file lives at docs/web/src/content.config.ts,
// so the repo root is three levels up.
const REPO_ROOT = path.resolve(fileURLToPath(new URL('.', import.meta.url)), '../../..');

/**
 * Tolerant markdown loader used for lesson/spec/proposal content.
 *
 * The built-in `glob` loader parses YAML frontmatter strictly. Several files
 * in `.agents/lessons/` have description strings that contain `: ` outside
 * quotes (a documented anti-pattern but historically present) which causes
 * js-yaml to fail and aborts the whole build. This loader extracts a few
 * known frontmatter keys with a single-line regex, drops the rest, and never
 * throws — bad frontmatter just yields a missing-title entry instead of a
 * dead site.
 *
 * The body (everything after the closing `---`, or the whole file if no
 * frontmatter) is passed through unchanged so MDX/Markdown rendering works
 * the same as the strict loader.
 */
function tolerantMarkdownLoader(opts: {
  rootRelative: string;       // e.g. ".agents/lessons"
  pattern: RegExp;            // matches against paths relative to rootRelative
  idFromPath: (relPath: string) => string;
}) {
  return {
    name: `tolerant-markdown:${opts.rootRelative}`,
    async load({ store, parseData, generateDigest, renderMarkdown, config, logger }: any) {
      const baseDir = path.join(REPO_ROOT, opts.rootRelative);
      // Astro requires entry filePath to be relative to the site (Astro
      // project) root. Resolve once per load.
      const siteRoot = config?.root ? fileURLToPath(config.root) : path.join(REPO_ROOT, 'docs/web');
      logger?.info?.(`tolerant-markdown: scanning ${baseDir}`);
      store.clear();
      let foundAny = false;
      try {
        await walk(baseDir, async (absPath) => {
          const rel = path.relative(baseDir, absPath).split(path.sep).join('/');
          if (!opts.pattern.test(rel)) return;
          const raw = await fs.readFile(absPath, 'utf8');
          const { frontmatter, body } = splitFrontmatter(raw);
          const id = opts.idFromPath(rel);
          const filePath = path.relative(siteRoot, absPath).split(path.sep).join('/');
          const data = await parseData({ id, data: frontmatter, filePath });
          const rendered = await renderMarkdown(body);
          store.set({
            id,
            data,
            body,
            filePath,
            digest: generateDigest(raw),
            rendered,
          });
          foundAny = true;
        });
      } catch (err: any) {
        logger?.warn?.(`tolerant-markdown loader failed for ${opts.rootRelative}: ${err?.message ?? err}`);
      }
      if (!foundAny) {
        logger?.warn?.(`tolerant-markdown loader matched no entries under ${opts.rootRelative} (baseDir=${baseDir})`);
      }
    },
  };
}

async function walk(dir: string, visit: (absPath: string) => Promise<void>) {
  let entries: import('node:fs').Dirent[];
  try {
    entries = await fs.readdir(dir, { withFileTypes: true });
  } catch {
    return;
  }
  for (const ent of entries) {
    const abs = path.join(dir, ent.name);
    if (ent.isDirectory()) {
      await walk(abs, visit);
    } else if (ent.isFile()) {
      await visit(abs);
    }
  }
}

/**
 * Pull a small set of known string keys from YAML-style frontmatter without
 * invoking js-yaml. Only handles plain `key: value` and `key: "value"` lines;
 * anything else is ignored. Designed to be permissive — when the value spans
 * suspicious characters we just drop it rather than fail the build.
 */
function splitFrontmatter(raw: string): { frontmatter: Record<string, unknown>; body: string } {
  if (!raw.startsWith('---\n')) {
    return { frontmatter: {}, body: raw };
  }
  const end = raw.indexOf('\n---', 4);
  if (end < 0) {
    return { frontmatter: {}, body: raw };
  }
  const block = raw.slice(4, end);
  // Body starts after the closing `---` line; skip the newline that follows it.
  let body = raw.slice(end + 4);
  if (body.startsWith('\n')) body = body.slice(1);
  const fm: Record<string, unknown> = {};
  for (const rawLine of block.split('\n')) {
    const line = rawLine.trimEnd();
    if (!line.trim() || line.startsWith('#')) continue;
    const m = /^([A-Za-z_][A-Za-z0-9_-]*)\s*:\s*(.*)$/.exec(line);
    if (!m) continue;
    const key = m[1];
    let val = m[2];
    // Strip surrounding quotes
    if ((val.startsWith('"') && val.endsWith('"')) || (val.startsWith("'") && val.endsWith("'"))) {
      val = val.slice(1, -1);
    }
    // Skip obviously list/object/block-scalar values — we don't need them.
    if (val.startsWith('[') || val.startsWith('{') || val === '|' || val === '>') continue;
    fm[key] = val;
  }
  return { frontmatter: fm, body };
}

const lessons = defineCollection({
  loader: tolerantMarkdownLoader({
    rootRelative: '.agents/lessons',
    pattern: /^[^/]+\/LESSON\.md$/,
    idFromPath: (rel) => rel.replace(/\.md$/, ''),
  }),
  schema: z.object({
    name: z.string().optional(),
    description: z.string().optional(),
    type: z.string().optional(),
    title: z.string().optional(),
  }),
});

const specs = defineCollection({
  loader: tolerantMarkdownLoader({
    rootRelative: '.agents/workflow/specs',
    pattern: /^[^/]+\/design\.md$/,
    idFromPath: (rel) => rel.replace(/\.md$/, ''),
  }),
  schema: z.object({
    title: z.string().optional(),
  }),
});

const proposals = defineCollection({
  loader: tolerantMarkdownLoader({
    rootRelative: '.agents/proposals',
    pattern: /^[^/]+\.md$/,
    idFromPath: (rel) => rel.replace(/\.md$/, ''),
  }),
  schema: z.object({
    title: z.string().optional(),
  }),
});

export const collections = { demos, docsCanonical, lessons, specs, proposals };
