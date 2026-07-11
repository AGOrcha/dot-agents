# config-transitive-layering

The dot-agents **code** work that unblocks the cross-repo config-layering rollout.
Extracted from the payout ideation plan `platform-config-layering` (the tasks
there carried `dot-agents-repo:` write scopes); this is now the **canonical
execution home** — the dot-agents `da` loop implements it here.

## Why here (not payout)
The config layers live in separate repos (`roos-agc` org, `provadm-agc`/
`po-agents-config` team), but the resolver/schema/spec **code** lives in
dot-agents. The payout plan originated the design (ideation ✅); execution of the
code belongs in this repo so `da workflow next` in dot-agents drives it.

## Predecessor (shipped)
`config-v2-migration` (this repo, active): `LayeredResolver` with tier-1 extends
resolves **repo-local** `extends` only (`p1b` ✅: product → user → extends →
repo). This plan adds the **transitive** layer on top — a fetched layer's OWN
`sources`/`extends` get expanded recursively (org through team to repo).

## Task spine (strict dependency order)
1. `org-config-spec-transitive-scope` — **focus**. Spec transitive extends
   (post-order: org before team before repo) + `sources[].scope`/`owner` as
   routing metadata (not authority). Specs: `org-config-resolution`,
   `config-distribution-model`, `docs/LAYERED_CONFIG_GUIDE.md`.
2. `config-source-scope-schema` — `Source.Scope` enum + `Owner` in
   `internal/config/agentsrc.go` + `schemas/agentsrc.schema.json`; v1 byte-stable.
3. `config-transitive-extends-resolver` — **THE core gap.** `resolveExtendsGraph`:
   recurse each fetched layer's own sources+extends, children-first, dedupe by
   ref+digest (fail on ref→different-digest), cycle-detect, lock all transitive
   units so offline/locked reproduces the full stack.
4. `layered-consumers-relevance-verify` — migrate `da config relevance`/verify off
   `loadFlatSnapshot` onto the layered/locked resolver (today extends-supplied
   app_types show `matched:false`).

Algorithm/types and the worked PA example are in
`~/proj-docs/payout/.agents/workflow/plans/platform-config-layering/platform-config-layering.plan.md`
(§ Schema Api Shape, § Edge Cases, § Verification).

## Downstream unblocks (the "after deps are met" gate)
On completion of task 3 (and ideally 4), the **content/adoption** plans in the
config repos become runnable — they are currently **paused** pending this plan:
- `agent-conf-ext/roos-agc` → plan `roos-org-source-buildout`
- `agent-conf-ext/provadm-agc` → plan `provadm-config-v2-revamp`
- payout `platform-config-layering` → `po-agents-config-adopt-roos-layer`,
  `repo-rollout-manifest-examples`

**Closeout action:** when this plan completes, unpause those two agent-conf-ext
plans (`da workflow plan update <id> --status active`) so the loop picks up the
content authoring.

Task 4 (`layered-consumers-relevance-verify`) is also a **shared prerequisite**
for `worker-bundle-lessons:load-lessons-2-relevance-lessons-facet`.
