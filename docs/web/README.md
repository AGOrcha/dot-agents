# docs/web — dot-agents Docs Site (Astro Starlight)

The **public** dot-agents docs, built on [Astro](https://astro.build) +
[Starlight](https://starlight.astro.build) (nav, search, dark mode, mobile).
Content is sourced **live from the repo's canonical markdown** under `docs/**`
via a custom glob loader — pages are never copied or forked.

The IA is four curated sections (sidebar groups):

| Section | Source docs (repo paths) |
|---|---|
| **Getting Started** | `README.md` (→ site root) |
| **Guides** | `docs/HOOKS.md`, `docs/CONFIG_RELEVANCE.md`, `docs/DEMO_*.md` |
| **Reference** | `docs/PLATFORM_DIRS_DOCS.md`, the `*_CONTRACT.md` docs, `docs/WORKFLOW_CLIENT_COMMANDS.md`, `docs/RELEASE_VERIFICATION.md` |
| **Concepts** | `docs/PROJECT_DIAGRAMS.md` |

## How content is wired

`src/content.config.ts` defines Starlight's `docs` collection with a custom
glob-style loader (`repoDocsLoader`) that reads the PUBLIC allowlist straight
from the repo root, derives each entry's `title` from its **first H1** when the
source has no frontmatter (the raw repo docs carry none), and assigns a
section-prefixed id so the sidebar `autogenerate` groups pick it up. This is the
production replacement for the spike's copy-prebuild — **no copy, no drift, no
mass frontmatter edits**.

` ```mermaid ` fences render via the [`astro-mermaid`](https://www.npmjs.com/package/astro-mermaid)
integration (configured in `astro.config.mjs`).

Internal artifacts (`.agents/**` lessons/specs/proposals, ADRs) are absent from
the loader allowlist; public/internal visibility gating and the ported
interactive graph/diagram pages (parked under `dm2-deferred/`) land in later
slices of the docs-starlight-migration plan.

## Develop

```bash
cd docs/web
npm install
npm run dev          # local dev server with HMR on http://localhost:4321
```

## Build

```bash
cd docs/web
npm run build        # outputs static site to dist/  (GitHub-Pages base path)
npm run preview      # serve dist/ locally to verify
```

By default the build uses base `/dot-agents/` (suitable for
`nikashprakash.github.io/dot-agents/`). For the Cloudflare Workers
deploy at `agorcha.dev`, the CI workflow sets `DEPLOY_TARGET=cloudflare`
so all asset URLs are root-relative:

```bash
DEPLOY_TARGET=cloudflare npm run build
```

See the **Deploy** section below for the full Cloudflare flow.

## Project layout

```
docs/web/
  package.json
  astro.config.mjs          ← Starlight + astro-mermaid; base/site host logic
  tsconfig.json
  README.md                 ← you are here
  public/                   ← static assets served as-is
  scripts/
    copy-schemas.sh         ← prebuild: copies /schemas/*.json → public/schemas/
  src/
    content.config.ts       ← Starlight `docs` collection + repo glob loader (H1 titles)
    worker.js               ← thin static-asset Cloudflare Worker (serves dist/)
  dm2-deferred/             ← interactive graph/diagram pages parked for dm2 (see its README)
```

Starlight owns navigation, the landing page, search, and the markdown render
pipeline — there are no hand-written content routes or sidebar components.

## What's in this slice vs follow-up

**This slice (dm1) delivers:**

- Starlight site shell: the 4-section public IA, sourced live from `docs/**`.
- Glob loader with first-H1 title derivation (no content fork, no frontmatter edits).
- `astro-mermaid` so ` ```mermaid ` fences render.

**Follow-up slices (out of scope here):**

- **dm2** — port the Cytoscape `/graphs/*` + JSON `/diagrams/*` interactive pages
  (parked under `dm2-deferred/`) into Starlight, linked from Concepts.
- **dm3** — `visibility: public|internal` gating + `dist/` vs `dist-internal/`.
- **dm4** — agent-readable `<path>.md` endpoints + `llms.txt`.
- **dm5** — Worker CF Access gate for `/internal/*` + deploy both outputs.

## Deploy — Cloudflare Workers at agorcha.dev

The site ships as a static-asset Cloudflare Worker. A thin pass-through
worker (`src/worker.js`) delegates every request to the
[`assets`](https://developers.cloudflare.com/workers/static-assets/)
binding, which serves files from `dist/` directly with the configured
`not_found_handling`. JSON schemas from `<repo-root>/schemas/` are
copied into `public/schemas/` at build time by `scripts/copy-schemas.sh`
(invoked from the `prebuild` npm script), so they are served at:

```
https://agorcha.dev/schemas/<name>.json
```

### CI workflow

`.github/workflows/deploy-docs.yml` builds and deploys on:

- **push to `master`** that touches `docs/web/**`, `docs/DEMO_*.md`,
  `schemas/**`, or the workflow file itself → **production deploy**
  to `agorcha.dev`.
- **pull requests** touching the same paths → **preview deploy** to a
  separate Worker named `agorcha-dev-docs-preview` (deployed via
  `wrangler deploy --name agorcha-dev-docs-preview`), reachable at
  `agorcha-dev-docs-preview.<account>.workers.dev` (no custom-domain
  route; intended for review).

### Required GitHub secrets (one-time maintainer setup)

The deploy job will fail until both of these are set on the repo:

```bash
# 1. Cloudflare API token with the Workers Scripts:Edit + Workers Routes:Edit
#    permissions (scoped to the account that owns agorcha.dev).
gh secret set CLOUDFLARE_API_TOKEN

# 2. Cloudflare account ID (Dashboard → Workers & Pages → right-hand sidebar).
gh secret set CLOUDFLARE_ACCOUNT_ID
```

Once both secrets exist, the next push to `master` matching the path
filter triggers an automatic production deploy. No further manual action
is needed for routine releases.

### Verifying a deploy

After CI reports success:

1. `curl -sI https://agorcha.dev/ | head -5` — expect `HTTP/2 200` and a
   `cf-ray` response header.
2. `curl -s https://agorcha.dev/schemas/agentsrc.schema.json | jq .title`
   — confirms the schema-copy hook ran.
3. Open `https://agorcha.dev/` in a browser; the landing page and at
   least one demo route should render.

### Local preview without deploying

```bash
cd docs/web
npm install
npm run build                  # runs prebuild (copy-schemas) + astro build
npm run preview                # serves dist/ via Astro's preview server
```

`wrangler dev` is also supported once `wrangler` is installed globally
and `.dev.vars` is populated from `.dev.vars.example` — but the static
output is identical, so `npm run preview` is usually sufficient.
