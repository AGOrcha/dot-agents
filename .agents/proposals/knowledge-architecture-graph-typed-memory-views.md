# Knowledge architecture: one graph store, typed memory views

Status: **vision / direction** (maintainer intent, first written down 2026-06-02)
Gated on: graph backend adapter readiness + KG upgrade. Until then, docs are the stopgap.

## Intent

The project's `docs/*.md` (the `*_CONTRACT.md` / `*_SPEC.md`, demos, `PLATFORM_DIRS_DOCS`,
the audit matrices) are a **stopgap** — a flat, human-readable place to keep *all* project
knowledge available and queryable until the real system exists. They are not the
intended end state.

## End state

**One main graph store** (the canonical knowledge), with several **typed views /
queryable services** over it — each surfacing a *kind* of knowledge, on a cognitive
(human-memory) metaphor. Ask for the right type, get it from the one store via the right
lens:

- **working** — active, transient task context (current plan/iteration/loop state).
- **operational / procedural** — *how* we do things: skills, workflows, runbooks, the
  staged executor→verifier→reviewer model.
- **semantic** — facts, concepts, **contracts, invariants, specs** (the declarative
  knowledge currently sprayed across `*_CONTRACT.md` / `*_SPEC.md`).
- **episodic** — events + history: decisions, iteration logs, fold-backs, `history/`,
  git lineage — *what happened and why*.

The typing of views **is** the "splitting" of today's monolithic doc knowledge.

## Why / current state

- The current KG implementation is deliberately **under-invested** — it felt lacking. The
  path is the **graph backend adapter contract** (one store, swappable backends, then
  typed views/query services) → see plan `graph-backend-adapter-contract` (the built-in
  `none` adapter e2e is the seam).
- Until that lands, docs hold all knowledge undifferentiated. That is the stopgap, and it
  is fine *as* a stopgap — the docs-refresh/audit tooling keeps it honest meanwhile.

## Implications for current work

- **docs-refresh / audit-matrix / promise-reconciliation are stopgap-era tooling.** Their
  categories already prefigure the views: contracts/specs → **semantic**; history /
  fold-backs / `history/` → **episodic**; skills / workflows → **operational**; loop/plan
  state → **working**. When the graph is ready, the docs-refresh's job migrates from
  "reconcile docs vs code" to "reconcile the **semantic view** vs code," and the
  knowledge moves into the store (docs become generated/secondary projections).
- The **`release-quality` app-type** (see `[[release-quality-app-type-after-config-v2]]`)
  operates on stopgap docs today; its end state operates on the typed views. Sequence it
  with config-v2 *and* the graph upgrade.
- The **audit matrix** is the seam twice over: today it drives per-category auditors over
  docs; tomorrow its categories map onto the semantic/episodic/operational views.

## Sequencing (rough)

1. graph backend adapter contract — one store + swappable backends (`none` adapter is the e2e seam).
2. KG upgrade — typed views / queryable services (working / operational / semantic / episodic).
3. migrate doc knowledge into the store; docs become generated projections of the views.
4. retarget docs-refresh / release-quality onto the views instead of flat docs.

## Relationship

- `graph-backend-adapter-contract` plan (foundation).
- config-v2 / §7A (`[[section-7a-units-lock-wiring]]`) — the scope/layer model; knowledge
  layering rides on the same coherence work.
- `[[release-quality-app-type-after-config-v2]]` — the docs/quality capability's end state.
