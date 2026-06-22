# Plan: Docs Starlight migration

Implements `workflow/specs/docs-starlight-migration/design.md` (D1–D7) + the gating contract in
proposal `agorcha-public-vs-internal-and-obs-deploy` §1–§3. Validated spike to crib from:
`/tmp/starlight-spike/docs/web-starlight` (Starlight 0.36 + Astro 5; sync script + 4-section sidebar
already proven). Build order: **dm0 ∥ dm1 → (dm2, dm3, dm4) → dm5**.

## dm1 — Starlight foundation (the big one)

Convert `docs/web/` from the custom Astro site to **Starlight**. One PR; this replaces the site shell.
- `package.json`: add `astro`, `@astrojs/starlight`, `astro-mermaid` (+ `mermaid`). Keep `cytoscape`/`mermaid`
  deps that dm2's ported pages need.
- `astro.config.mjs`: `integrations: [starlight({...}), mermaid()]`. **Preserve the existing base logic**
  (`base: IS_CLOUDFLARE ? '/' : '/dot-agents/'`, `site`). Starlight `sidebar` = the 4 groups (D3) with the
  public docs slotted under them.
- **Content via glob loader (D2):** in `src/content.config.ts`, define the Starlight `docs` collection with
  Astro `glob()` over the repo markdown (`{ base: '../../', pattern: ['README.md','docs/*.md','docs/DEMO_*.md'] }`)
  and a `schema` that **fills `title` from the first H1 when absent** (transform: read body, match `/^#\s+(.+)/m`).
  This avoids editing source docs. (Spike used a copy-prebuild because raw glob fails Starlight's required
  `title`; the H1-deriving schema is the production fix — do NOT ship the copy-prebuild.)
- Remove the old custom content routes (`src/pages/canonical/**`, `src/pages/demos/**`, `Sidebar.astro`,
  `index.astro` pitch) — Starlight owns nav/landing now. Keep `public/`, `wrangler.jsonc`, `src/worker.js`,
  the schema-copy prebuild (dm5 revisits the worker).
- Verify: `npm run build` green; `npm run dev`/preview renders the 4 sections + a mermaid page.

## dm2 — Port interactive pages (hybrid, D1)

From the OLD custom site (now in git history / `/tmp/custom-site/docs/web`), port into the Starlight project:
- `src/pages/graphs/{da-resources,workflow-resources,workspace-state}.astro` + the `ResourceGraph` component +
  bundled cytoscape usage + `src/data/*` they read.
- `src/pages/diagrams/{tier-model,lens-dispatch,verifier-registry}.astro` + their JSON in `src/data/`.
- These are **custom Astro routes** living alongside Starlight (Starlight doesn't own `/graphs`,`/diagrams`).
  Link them from the **Concepts** section (Starlight sidebar can list external/custom links).
- Verify the routes build + render the interactive viz.

## dm3 — visibility gating (D4 / proposal §1.3)

- Add `visibility: z.enum(['public','internal']).default('internal')` to every content collection schema
  (the public `docs` collection + the internal collections — Lessons `.agents/lessons/*/LESSON.md`, Specs
  `.agents/workflow/specs/*/design.md`, Proposals `.agents/proposals/*.md`).
- **Two-output build** (per proposal §1.3): `npm run build` produces `dist/` (public — route generators +
  collections filter to `visibility==='public'`) and a second pass produces `dist-internal/` (no filter,
  everything). Implement via an env flag the config reads (`INTERNAL_BUILD=1` → include internal + output
  `dist-internal`), driven by two `package.json` scripts (`build` runs both). Internal Lessons/Specs/Proposals
  sidebar groups appear ONLY in the internal build.
- Classification: public = the four sections' docs (explicit `visibility: public`, set via the loader for the
  curated set, or per-doc frontmatter); everything under `.agents/**`, ADRs, internal specs ⇒ default internal.
- Verify: `ls dist/` has NO lessons/specs/proposals; `ls dist-internal/` has them.

## dm4 — agent-readable `.md` + llms.txt (D6)

- `src/pages/[...slug].md.ts` — Astro static endpoint (`getStaticPaths` mirrors the public `docs` collection)
  returning each public entry's **raw markdown body** with `Content-Type: text/markdown`. PUBLIC only (reuse
  the dm3 public filter). 
- `src/pages/llms.txt.ts` — emits an `llms.txt` index: site title + one `- [title](<base><slug>.md)` line per
  public page. Honor `base`.
- Verify: `/llms.txt` lists the public set; `/<some-public-slug>.md` returns raw markdown; no internal slug resolves.

## dm5 — Worker CF Access gate + deploy both (D5 / proposal §1.3, §2.3)

- `src/worker.js`: route `/schemas/*` → ASSETS unauth (carve-out FIRST); `/internal/*` → verify CF Access JWT
  (`aud` == audTag from `docs/cf-access-bootstrap.md`; use `@cloudflare/workers-access`/JWKS) then serve from
  `ASSETS_INTERNAL`; everything else → `ASSETS` (public `dist/`).
- `wrangler.jsonc`: add the `ASSETS_INTERNAL` asset binding (points at `dist-internal/`).
- `.github/workflows/deploy-docs.yml`: build BOTH (`dist` + `dist-internal`), upload both bindings, deploy.
- Verify (manual, maintainer): `agorcha.dev/` + `<page>.md` load unauth; `agorcha.dev/internal/<spec>` challenges
  CF Access; `agorcha.dev/schemas/x.json` loads unauth.

## dm0 — CF Access IaC (Terraform) — parallel (D8)

Terraform under `infra/cloudflare/` (cloudflare provider ~>5, v5 `zero_trust_access_*` resources):
the `agorcha-internal-docs` app (domain agorcha.dev/internal, 24h, GitHub+OTP idps), a **maintainer**
policy (email allow), and a **Service Auth** policy that accepts **any valid service token**.
**Per-user model (decided):** do NOT declare a static service token — tokens are minted per-developer
at runtime by dm6. Outputs: `audTag` (dm5 verifies JWT aud) + `account_id`/`zone_id` for dm6's minting.
Commit the runbook (`infra/cloudflare/README.md`); **maintainer runs `terraform apply`** with a
`CLOUDFLARE_API_TOKEN` (Access apps+policies edit). dm5 consumes audTag; dm6 mints per-user tokens.

## dm6 — Agent internal-docs access, per-user (decided)

- **Direct-attach now:** `da` sets `CF-Access-Client-Id`/`CF-Access-Client-Secret` headers on
  `agorcha.dev/internal/*` (incl. `.md`) fetches, loading the token from `~/.config/da/credentials.json`
  (external-agent-sources credential-ref). The `da service` auth-proxy (proposal §5.5) is the later
  consolidation — direct-attach until it lands.
- **Gated self-provision, per-user:** a CF-Access-gated endpoint (served by the dm5 Worker, or a sibling
  Worker) — after a developer authenticates (GitHub via CF Access) — **mints a per-user CF Access service
  token via the CF API** (named `agorcha-agents-<github-login>`), returns its `client_id`/`client_secret`
  to `da`, which stores + auto-wires it (on `da init`/`refresh`). Revoke a developer = delete their token.
  The minting endpoint needs a **scoped CF API token** (service-token create) as a server secret + the
  `account_id` from dm0.
- The Terraform Service-Auth policy accepts any valid service token, so per-user minted tokens authenticate
  without policy churn; deleting a token revokes that one developer.
- Depends on dm0 (app+policy+account_id) and the dm5 Worker (to host the gated endpoint).

## Notes
- Throwaway spike servers (`:8443` Starlight, `:10000` custom) can be torn down once dm1 lands.
- `app_type: docs` tasks — verification is build + curl, not Go tests.
