import { defineCollection, z } from 'astro:content';
import { glob } from 'astro/loaders';

// Demo markdown collection: source-of-truth lives in ../docs/DEMO_*.md
// (i.e. docs/DEMO_*.md relative to repo root). We ingest by glob loader from
// the parent directory so the canonical files are NOT duplicated under web/.
const demos = defineCollection({
  loader: glob({
    pattern: 'DEMO_*.md',
    base: '../', // resolved relative to this file's directory => docs/
  }),
  schema: z.object({
    title: z.string().optional(),
  }),
});

export const collections = { demos };
