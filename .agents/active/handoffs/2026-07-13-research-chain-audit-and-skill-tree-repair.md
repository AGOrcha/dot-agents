# Research-intake chain + e2e pipeline audit / skill-tree repair (2026-07-13)

**Why this file exists.** A single interactive session (Claude Code, 2026-07-12→13) shipped
the research-intake skill chain, wove it into the kg-ideate pipeline, then ran a 4-agent e2e
audit of skills/profiles/rules/code and repaired what it found — including materializing the
kg-ideate family that was invisible on this machine. This is the pickup map for any other
session (orchestrator loop, wave prep, drain heartbeat) so nothing here has to be
rediscovered from the ~40 uncommitted files. Every ✅ was verified live this session.

## 1. Pipelines are startable again — what changed underneath

- ✅ **kg-ideate is now invocable on this machine.** The completed compound + molecules
  (`spec-scaffold`, `plan-scaffold` as TOP-LEVEL siblings by design; `kg-brief`,
  `staged-execution-handoff` nested) existed only in the git-tracked scaffold
  (`internal/scaffold/home/starter/skills/global/`) and were never synced to
  `~/.agents/skills/global/` — the ideation profile's core skill didn't resolve for ~2 days.
  Materialized + platform-linked; kg-ideate conformance test and `go build ./...` pass.
  Record: `(fb:kg-ideate-scaffold-home-sync)` RESOLVED on `kg-ideate-skill` PLAN.yaml notes.
- ✅ **New skill chain (promoted, registered, linked):** `research-intake` (orchestrator;
  two entrypoints — user link-drop AND ideation-gather dispatched from kg-brief's new corpus
  gap trigger) → `article-extract` (4-tier fetch ladder: WebFetch → Playwright →
  Claude-in-Chrome → codex; save/backfill to `research/articles/` is the terminal step) →
  `research-evaluate` (general systematic-review method in `instructions/method.md`;
  dot-agents formats in `overlays/dot-agents.md`; broad full-pass is an OPTIONAL subtree
  gated by decision class, method §0) → `knowledge-fold-back` (shared routing sink; archive
  convention = `.agents/history/<plan>/fold-backs/[<task>/]<slug>.yaml`).
  Record: `(fb:research-chain-ideation-integration)` RESOLVED on `kg-ideate-skill` notes.
- ✅ **7 stale `~/.agents/skills/dot-agents/` copies synced from `global/`** (isp had lost the
  REQUIRED stop-gate sentinel step); article-extract namespaces converged; ideation-cycle
  `calls:` frontmatter fixed (`converge`/`dogfood`) in scaffold + live.
- ⚠️ **Root cause is still open:** nothing drift-checks scaffold↔home; next scaffold-side
  skill change will fork again until `(fb:scaffold-home-skill-sync-mechanism)` (OPEN,
  `agent-ops-hardening` PLAN.yaml notes) is implemented.

## 2. Wave-prep implications (transcript-analysis-and-pipeline-craft)

- ✅ `(fb:lens-transfer-trap-test)` RESOLVED — Tier-B trap-test transfer check added to
  `evidence/pareto/disposable-tasks.md` (gates instrument trust when a stage prompt ports to
  a SECONDARY-tier arm).
- ⚠️ `(fb:cross-family-lens-autoswap)` OPEN on `pareto-live-waves` task notes — **no code
  auto-selects `cross-harness-adversarial-claude`**; C3/C4/C5-gpt-legs/C6-gpt-gate arm
  configs MUST pin it in lens_set or the family-equality refusal blocks the arm. Operational
  note is in `evidence/pareto/live-contrast-lens-map.md`.
- ⚠️ `(fb:craft-doc-stable-location)` OPEN plan-level — relocate `craft/full-loop-craft.md`
  (or repoint pipeline-architect) AT plan archive time.
- New corpus material: Part K in `research/articles-evaluation-kg-and-adjacent.md` (3
  operating-contract articles; corroboration-first, no wave decision reopened) +
  `research/extracts/operating-contracts-2026-07.md`; chase list lives in the plan digest
  addendum (`evidence/prior/external-references-2026-07-12.md` items 8–10).

## 3. Registry / rules / hygiene fixes (verified)

- ✅ `.agentsrc.json`: +6 skills registered (article-extract, playwright,
  platform-docs-refresh, research-intake, research-evaluate, knowledge-fold-back), +3 agents
  (orchestrator, platform-dirs-change-analyst, promise-gap-analyst), dead `deep-research`
  ref removed. Record: `(fb:agentsrc-registry-completeness)` RESOLVED on
  `agent-ops-hardening` notes.
- ✅ Rules: AGENTS.md "Agent Workflow Notes" rewritten to the canonical `da workflow` flow —
  **edited at the resource source** `~/.agents/resources/dot-agents/AGENTS.md` (direct edits
  to the rendered `~/.agents/rules/dot-agents/agents.md` are silently reverted by
  `da refresh`; see lesson `rendered-rules-edit-the-resource-source`). proposal-routing.md +
  workflow-artifact-model.md now document the resolved-fold-back archive destination.
- ✅ Fold-back queue: 8 artifacts of archived plans swept to
  `.agents/history/<plan>/fold-backs/`; active queue is 29 files, all owned by active plans
  (7 legacy `.md` records without `plan_id` left as-is). `(fb:plan-archive-foldback-sweep)`
  OPEN tracks making `plan archive` do this sweep.

## 4. Fragility warning for other sessions

Everything repo-side is **uncommitted** on `feat/transcript-analysis-pipeline-craft`
(research/, plan artifacts, scaffold skill files, `.agentsrc.json`, AGENTS.md target, this
file). Do NOT `git clean`/`reset --hard` without the classify-first discipline
(lesson `classify-generated-files-before-cleanup`). The `~/.agents` home-tree changes
(skill syncs, new skill dirs) are machine state outside git entirely — they survive repo
operations but will NOT follow a fresh clone; a fresh machine gets them via the (now
registered) `.agentsrc.json` + scaffold once the sync-mechanism fold-back lands.
