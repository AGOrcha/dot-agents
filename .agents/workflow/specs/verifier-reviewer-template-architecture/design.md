# Spec: verifier & reviewer prompt template architecture (base + per-type, scope-resolved)

**Status:** implemented (PRs #30/#31 merged; plan archived)
its own PR stacked on #29.
**Depends on:** the verifier profiles shipped/added in #29 (unit, api, ui-e2e, batch, streaming,
`cli-runner`, and the ideation trio `schema-check`/`citation-check`/`task-schedule`) and the
reviewer lenses (architecture-standards / acceptance-invariants / adversarial).
**Rides on:** the config-v2 scope model (`config-distribution-model` §15) — verifier/reviewer
prompts resolve through the **same scope precedence** as `execution_profile`, not a parallel
mechanism.
**Grounds in:** the `da workflow verify record` surface and the result/decision schemas
(`schemas/verification-result.schema.json`, `schemas/verification-decision.schema.json`) per the
`pr-ci-verifier-integration-audit` census; the three-tier hint in the verifier prompts' "Role
boundary" tables.

---

## 1. Problem & goal

Today every verifier-profile prompt is a **single project-local file**
(`.agents/prompts/verifiers/<type>.project.md`) and each re-states or silently assumes the generic
verifier mechanics: how to drive `da` (`workflow verify record --kind/--verifier-type`), where the
typed result artifact goes, which schema validates it, the no-product-edits role boundary, the
evidence taxonomy. Three problems:

1. **Duplication / drift.** Each new verifier (`cli-runner`, the ideation trio) re-copies the same
   `da`+schema+role boilerplate; they drift.
2. **Nothing ships to other consumers.** The guidance is trapped in this repo's project files. A
   different org/team/project that wants a `unit` or `api` verifier starts from nothing.
3. **Reviewers have no shared base at all** — no shipped "how to produce a review decision, what
   schema, what the role is."

**Goal:** a layered template model with **one shipped base** (verifier + reviewer) that teaches the
`da` surface, the schemas, and the general role/evidence expectations — where both the base and each
per-type template are **scope-mergeable artifacts resolved through the config-v2 scope ladder**
(`config-distribution-model` §15 D1/D9: product → user-local → org → team → repo-imported →
repo-local committed → project-local overlay → runtime), exactly like `execution_profile`. The
starter ships the base at the **product** scope; dot-agents' own paths/matrices/guardrails are simply
the **repo-local committed** layer — one rung of the ladder, not a special "project vs global" tier.

## 2. Two axes: composition × scope

A resolved verifier/reviewer prompt is the merge of **two independent dimensions**.

### Axis A — Composition (what the instruction covers)
- **Base** — generic to *all* verifiers (one) and *all* reviewers (one): the `da workflow verify
  record` surface, the typed result/decision artifacts + their schemas, cold-start from
  `impl-handoff.yaml`, the role boundary (no product edits), the evidence taxonomy.
- **Per-type** — one per verification kind (unit, api, ui-e2e, batch, streaming, cli-runner,
  schema-check, citation-check, task-schedule) and one per reviewer lens (architecture-standards,
  acceptance-invariants, adversarial): e.g. `unit` = scoped `go test` then full suite →
  `unit.result.yaml`; `cli-runner` = build the binary + smoke the CLI.

### Axis B — Scope (whose override applies) — the config-v2 scope ladder
The canonical §15 D1/D9 precedence (low → high; later wins, "who gets the last word"):

`product → user-local → org → team → repo-imported → repo-local committed → project-local overlay
(uncommitted) → runtime`

The starter ships the base + per-type at the **product** scope. Higher rungs refine: an **org** sets
org-wide verifier expectations, a **team** refines, the **repo-local committed** layer (what
dot-agents calls its `.project.md` files today) adds repo paths/test matrices/smoke script, a
**user-local** layer personalizes, a **project-local overlay** (uncommitted, gitignored) holds
machine-local tweaks. Resolved by config-v2's existing scope precedence — no new resolver.

This is the correction the maintainer asked for: not a "global vs project" binary, and not just
"project" — the *proper* ladder, where org/team/user are first-class rungs alongside repo-local.
"Starter baseline" = the product rung; "dot-agents-specific" = the repo-local committed rung.

The two axes are orthogonal: composition says *which* templates compose (base + per-type); scope
says *whose version* of each wins. The effective prompt for a `verifier_profiles.<type>` is the
base + per-type composition, each resolved across the scope ladder.

## 3. Decisions (review these)

- **D1 — Rides config-v2 scope resolution; no parallel override.** Base and per-type templates are
  scope-mergeable artifacts resolved through the §15 D1/D9 ladder (product → user-local → org → team →
  repo-imported → repo-local committed → project-local overlay → runtime), the same machinery
  `execution_profile` uses. "Starter baseline" = the **product** rung; "dot-agents-specific" = the
  **repo-local committed** rung — one layer of several, not a privileged tier.
- **D2 — Reviewers get the same model.** A reviewer base + per-lens templates
  (architecture-standards / acceptance-invariants / adversarial), each scope-resolved, so a reviewer
  knows the `review-decision.yaml` + `verification-decision.schema.json` contract generically and any
  scope refines what (say) "architecture standards" means for that org/team/project.
- **D3 — The base teaches the contract, not the repo.** Base content: `da workflow verify record
  --kind <test|lint|build|format|custom|review> --verifier-type <slug>`; the
  `.agents/active/verification/<task_id>/<slug>.result.yaml` family + `verification-result.schema.json`
  (verifiers) and `review-decision.yaml` + `verification-decision.schema.json` (reviewers); the
  `pass|fail|partial|unknown` status set; the evidence taxonomy
  (`ok|ok-warning|impl-bug|tool-bug|missing-feature|blocked`); cold-start discipline.
- **D4 — Every existing + new type/lens is split.** unit, api, ui-e2e, batch, streaming, cli-runner,
  schema-check, citation-check, task-schedule, and the three lenses each get base + per-type
  content, with generic guidance in the product-scope starter baseline and the repo specifics
  demoted to the **repo-local committed** layer — no net loss of guidance. Note the asymmetry today:
  the **reviewer base already exists but duplicated** across the three starter
  `agents/global/<lens>-reviewer/AGENT.md` files (each restates bundle-read / `da workflow tasks` /
  target-verify / findings-format); the **verifier base is duplicated implicitly** across the nine
  `.agents/prompts/verifiers/*.project.md` files. D4 extracts both into a single product-scope base.
- **D5 — `prompt_files` lists the scope-resolved composition.** A profile's effective `prompt_files`
  is the base then per-type templates, base-first so specifics refine; the *contents* of each are
  the scope-resolved version. The starter ships the baseline; scope layers override through §15
  resolution. Uses the existing array field — no schema change to `verifier_profiles`.
- **D6 — Reviewers get a symmetric `reviewer_profiles` field (review decision 2026-06-06).** Add a
  `reviewer_profiles` map mirroring `verifier_profiles` (`{label, prompt_files []}`), keyed by lens
  (architecture-standards / acceptance-invariants / adversarial), composed **base-first by ordered
  concatenation** — so verifiers and reviewers are one model. *Implementation note (verified
  in-tree):* `verifier_profiles` is **not** a typed `AgentsRC` field — it round-trips via
  `ExtraFields` and scope-merges purely through `resolver.go`'s `fieldCategories`
  (`CategoryMapMerge`). `reviewer_profiles` follows the *same* pattern: add it to `fieldCategories`
  (`CategoryMapMerge`) + `schemas/agentsrc.schema.json`. No struct / `agentsRCKnown` / Marshal change
  (the 6-place lifecycle applies only to typed fields).
- **D7 — Phase-1 wires the missing composition (review decision 2026-06-06).** Finding: today
  `verifier_profiles.<id>.prompt_files` is only *validated* (the id must exist —
  `delegation.go:validateVerifierProfileRefs`); the sole path that puts `prompt_files` into a bundle
  is the manual `--prompt-file` flag (`delegation.go:1552`). So the compose mechanism does **not**
  exist yet. Phase-1 adds it: when a fanned-out verifier/reviewer is dispatched for a profile id,
  read that profile's resolved `prompt_files` and set `bundle.Prompt.PromptFiles` to the base-first
  composition (explicit `--prompt-file` still overrides).

## 4. Done criteria

1. A shipped **verifier base** and **reviewer base** template exist (starter baseline scope) and
   teach the `da` surface + schemas + role/evidence expectations (D3).
2. Each verifier type and each reviewer lens has base + per-type content; generic guidance is in the
   baseline, dot-agents specifics in the **project-scope** layer.
3. The base and per-type templates resolve through the config-v2 scope ladder (§15 D1/D9) —
   demonstrated by a higher-scope override (e.g. repo-local committed over product) changing the
   effective prompt; `da config verify` stays green.
4. `da install`/`refresh` materializes the product baseline into a consuming project; a fresh project
   with only the product baseline (no user/org/team/repo overrides) resolves usable
   verifier/reviewer prompts.
5. The dot-agents repo-local committed layer is slimmed to repo-specifics with no guidance lost (diff
   shows generic content moved to the product baseline, not deleted).

## 5. Out of scope / deferred

- The first-class **versioned** `verifier_profiles` layer (app-type-profiles §11.4–11.5).
- OCI/package distribution of templates (external-agent-sources §5) — ship via the starter for now.

**Phasing (review decision 2026-06-06): "Phase-1 proof, then migrate."**
- **PR-1 (proof):** both bases (`verifier.base.md`, `reviewer.base.md`) at product scope, the
  `reviewer_profiles` field (D6), the compose wiring (D7), and end-to-end resolution proven on **two
  verifiers (`unit`, `cli-runner`) + one lens (`architecture-standards`)**. Demonstrates a higher-scope
  override changing the effective prompt.
- **PR-2 (migrate):** the remaining seven verifiers (api, ui-e2e, batch, streaming, schema-check,
  citation-check, task-schedule) + two lenses (acceptance-invariants, adversarial), slimming each
  repo-local committed file to repo-specifics with no guidance lost.

## 6. Resolved (Q1) + open questions (Q2–Q4, plan-tier)

- **Q1 — Scope binding & precedence → RESOLVED.** The canonical ladder is `config-distribution-model`
  §15 D1/D9: product → user-local → org → team → repo-imported → repo-local committed → project-local
  overlay → runtime (low → high). The spec's earlier "org → team → project → user" guess was wrong on
  both vocabulary and ordering; bind to this ladder. A template attaches to a scope as a config-v2
  *layer/artifact* at that scope and resolves through the same resolver as `execution_profile` — no
  new code path.
- **Q2 — Scaffold layout & materialization → RESOLVED (new `prompts/{verifiers,reviewers}/`).**
  Product-scope home is new dirs `internal/scaffold/home/starter/prompts/verifiers/`
  (`verifier.base.md` + per-type `<type>.md`) and `internal/scaffold/home/starter/prompts/reviewers/`
  (`reviewer.base.md` + per-lens `<lens>.md`). This mirrors the repo-local `.agents/prompts/` layout
  at product scope. The reviewer `agents/global/<lens>-reviewer/AGENT.md` agents compose from the
  reviewer base + per-lens templates. The repo-local committed rung stays at
  `.agents/prompts/verifiers/*.project.md` (and a reviewer equivalent). `install`/`refresh` projects
  the product baseline into the consuming project; higher scopes override via §15 resolution. (The
  dir segment `global` under `agents/` is *platform-visibility*, not config scope — unchanged.)
- **Q3 — Composition semantics → RESOLVED (symmetric `prompt_files`, base-first concat).** Verifiers
  keep `verifier_profiles.<type>.prompt_files []`; reviewers get the symmetric
  `reviewer_profiles.<lens>.prompt_files []` (D6). Both combine **base-first by ordered
  concatenation** (not first-match). Phase-1 wires the actual expansion into the bundle (D7).
- **Q4 — Naming convention → RESOLVED.** Base `verifier.base.md` / `reviewer.base.md`; per-type
  `<type>.md` / per-lens `<lens>.md`. A higher-scope override is the *same filename* at that scope's
  layer location (resolution is by scope precedence, not a filename suffix); the existing repo-local
  `.project.md` suffix is retained for the repo-local committed rung's files.

## 7. Relationships

- **config-distribution-model §15 D1/D9** — the scope model (product/user/org/team/repo-imported/
  repo-local/overlay/runtime, sources, resolution) this rides on; verifier/reviewer templates are
  scope-mergeable artifacts like `execution_profile`.
- **cli-runner-verifier (t7) / ideation-execution-profile (t8)** — the new verifiers this base
  backs; both consume the layered, scope-resolved `prompt_files` once tiers exist.
- **app-type-profiles §11** — verifier-profile taxonomy + the future versioned layer.
- **pr-ci-verifier-integration-audit** — the `verify record` surface, schemas, and role boundaries
  the base codifies.
- **external-agent-sources §5** — future packaged distribution of these starter templates.
