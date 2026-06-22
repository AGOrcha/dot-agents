# Spec: Docs Starlight migration (hybrid) + public/internal gating + agent-readable `.md`

Status: spec (ratified 2026-06-22 — maintainer chose full Starlight migration after
evaluating the spike vs the current custom site). Builds on
`.agents/proposals/agorcha-public-vs-internal-and-obs-deploy.md` §1–§3 (the public/internal
split + CF Access topology + bootstrap), which this spec treats as the gating contract.

## Problem & why

The custom `docs/web` Astro site (1) dumps ~190 internal artifacts (`.agents` lessons/specs/
proposals, ADRs, internal design specs) onto the public site, (2) has no onboarding path
(opens on a sales pitch; `README` isn't even a page), and (3) is bespoke to maintain. It also
serves HTML only — web-searching AI agents can't fetch clean markdown.

## Decisions (ratified)

- **D1 — Starlight, hybrid.** Migrate `docs/web` to Astro **Starlight** (docs theme: nav,
  search, dark mode, mobile). Keep the bespoke interactive pages — the Cytoscape `/graphs/*`
  and the JSON-driven `/diagrams/*` (`tier-model`/`lens-dispatch`/`verifier-registry`) — as
  **custom Astro routes/components in the same project** (Starlight is an integration, not a
  separate framework). Not either/or.
- **D2 — Content stays canonical in `docs/**`.** Pages are sourced via an Astro **`glob()`
  loader** pointing at the repo markdown, with a schema that **derives `title` from the first
  H1** when frontmatter lacks one — so source docs are not duplicated and need no mass
  frontmatter edits. (The spike proved a copy-prebuild works; production uses the glob loader
  for zero drift.)
- **D3 — Public IA.** Four curated sections: **Getting Started** (README → install/init/first
  project), **Guides** (Hooks, Config Relevance, DEMO walkthroughs), **Reference** (Platform
  dirs, the `*_CONTRACT` docs, workflow commands, release verification), **Concepts**
  (architecture + the ported diagrams/graphs).
- **D4 — Public/internal gating** (per proposal §1.3): `visibility: public | internal`
  (**default `internal`** — fail-safe) on every content collection. Build partitions into
  `dist/` (public only) and `dist-internal/` (everything). Internal sections
  (Lessons/Specs/Proposals from `.agents/**`) appear only in `dist-internal/`.
- **D5 — CF Access topology** (per proposal §2–§3): Worker serves `dist/` at `/*`,
  `dist-internal/` at `/internal/*` **only after CF Access JWT verification** (audTag from the
  IaC-managed `agorcha-internal-docs` app — see D8). `/schemas/*` stays unauth (editor tooling).
- **D6 — Agent-readable.** Every **public** page is also served as raw markdown at `<path>.md`
  (Astro static endpoint reusing the collection's raw body) plus a generated **`llms.txt`**
  index. Internal pages are excluded from both (they live behind CF Access).
- **D7 — Mermaid.** Add the `astro-mermaid` integration so ` ```mermaid ` fences render in
  Starlight (parity with the custom site, which does it client-side today).
- **D8 — CF Access as IaC + a machine account for agents** (supersedes proposal §3.3's
  manual-now). The Access app, its policies, and the agent service token are **declared with
  Terraform** (cloudflare provider) under `infra/cloudflare/` — not click-ops. The
  `agorcha-internal-docs` app carries two policies: a browser **maintainer** allow (email) and a
  **Service Auth** policy accepting service tokens. Tokens are **per-user, minted at runtime** by
  the dm6 provision endpoint — one CF Access service token per developer (e.g.
  `agorcha-agents-<github-login>`), **not** a single static shared token — giving per-user
  attribution and individual revocation (delete that dev's token; the policy accepts any valid
  service token, so a deleted token simply stops authenticating). Agents reading `/internal/*`
  (incl. the D6 `.md` endpoints) authenticate with their token's `CF-Access-Client-Id` /
  `CF-Access-Client-Secret` headers. The `da` CLI loads the token from the
  external-agent-sources credential store (`~/.config/da/credentials.json`, by credential-ref id)
  and attaches it; the `da service` auth-proxy (proposal §5.5) is the eventual injection layer
  (direct-attach until it lands). The credential is **auto-wired into dot-agents developers'
  setup** so their agents get internal-docs access without hand-managing tokens. Forks resolved:
  `da` attaches the token **directly** now (auth-proxy refactor later); **gated self-provision**
  (developer authenticates via CF Access → the endpoint mints *their* token); **per-user** tokens
  (not shared). Tracked on `dm6`.

## Done criteria

See the plan's success_criteria. In short: Starlight site live; public IA from `docs/**` via
glob; interactive pages ported; mermaid renders; `<path>.md` + `llms.txt` for the public set;
`dist/` excludes internal while `dist-internal/` includes it; Worker gates `/internal/*` behind
CF Access; `/schemas/*` unauth; `deploy-docs.yml` builds+deploys both outputs.

## Deferred / out of scope

- `obs.agorcha.dev` observability server (proposal §4–§6) — separate effort.
- A curated "public lessons" subset (proposal §1.4) — lessons stay internal until curated.
- (CF Access IaC is now IN scope per D8 / `dm0` — no longer deferred.)

## Relationship to other artifacts

- Gating contract + CF Access bootstrap steps: proposal `agorcha-public-vs-internal-and-obs-deploy` §1–§3.
- Plan + per-slice code decisions: `workflow/plans/docs-starlight-migration/`.
