// Agent-readable raw-markdown endpoints (D6): serve each PUBLIC docs page's raw
// markdown at /<slug>.md so agents (and llms.txt consumers) can fetch the source
// text, not rendered HTML. PUBLIC ONLY — the getCollection filter restricts to
// visibility === 'public', so internal lessons/specs/proposals never resolve to
// a .md endpoint even in the INTERNAL_BUILD pass (where they're loaded into the
// same collection but stay marked internal).
import type { APIRoute, GetStaticPaths } from 'astro';
import { getCollection } from 'astro:content';

export const getStaticPaths: GetStaticPaths = async () => {
  const docs = await getCollection('docs', (e) => e.data.visibility === 'public');
  return docs.map((entry) => ({
    // entry.id is the section-prefixed slug, e.g. 'guides/hooks' or 'index'.
    // 'index' maps params.slug='index' → /index.md.
    params: { slug: entry.id },
    // entry.body holds the RAW markdown the loader stored (not rendered HTML).
    props: { body: entry.body ?? '', title: entry.data.title },
  }));
};

export const GET: APIRoute = ({ props }) =>
  new Response(props.body, {
    headers: { 'Content-Type': 'text/markdown; charset=utf-8' },
  });
