// llms.txt index (D6) — https://llmstxt.org/ format, lightweight. Enumerates the
// PUBLIC docs pages as '- [title](<BASE><slug>.md)' lines so an agent can
// discover every agent-readable page in one fetch. PUBLIC ONLY: the getCollection
// filter restricts to visibility === 'public', so internal pages never appear.
// Base-aware via import.meta.env.BASE_URL ('/' on Cloudflare, '/dot-agents/' on
// GitHub Pages) and deterministic (sorted by id).
import type { APIRoute } from 'astro';
import { getCollection } from 'astro:content';

const BASE = import.meta.env.BASE_URL;

export const GET: APIRoute = async () => {
  const docs = await getCollection('docs', (e) => e.data.visibility === 'public');
  // Stable, deterministic order: by section-prefixed id.
  docs.sort((a, b) => a.id.localeCompare(b.id));

  const baseNoSlash = BASE.replace(/\/$/, '');
  const lines = [
    '# dot-agents',
    '',
    '> The operational layer for AI coding agents. Public documentation.',
    '',
    '## Docs',
    '',
    ...docs.map((e) => {
      const slugPath = e.id === 'index' ? 'index' : e.id;
      const url = `${baseNoSlash}/${slugPath}.md`;
      return `- [${e.data.title}](${url})`;
    }),
    '',
  ];

  return new Response(lines.join('\n'), {
    headers: { 'Content-Type': 'text/plain; charset=utf-8' },
  });
};
