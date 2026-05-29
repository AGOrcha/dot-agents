# Codex 019e6245 — Examination and Sequenced Plan

**Status:** project-local examination of PR #120 (and the deferred contract-change half on PR #119)
**Author:** codex-intent-examiner subagent
**Date:** 2026-05-27
**Inputs:** 5 proposals + 2 specs on branch `origin/codex-019e6245-docs-only`; current `master` CLI source.

---

## 1. Executive summary

The codex thread delivered a coherent design pass that (a) restored four PA-cursor
proposals lost in earlier branch churn, (b) authored one new planning-input proposal
(`staged-profile-dispatch-and-return-gate`), and (c) tightened two existing specs.
Read as design, the package is high-quality and internally consistent — including the
one contract change PR #119 originally tried to ship (dropping the duplicate
`workflow advance` after `workflow delegation closeout`), which is in fact **GROUNDED
by current code** (`commands/workflow/delegation.go:1608-1649` already sets the task
to `completed` on accepted closeout).

Where the package is aspirational, it is aspirational by design: it presupposes a
`da config` subtree, per-stage native agents, a two-tier config-distribution surface,
and scope-routed `da review`, none of which exist in the binary on `master` today.
The orchestrator's call to land docs-only on PR #120 and defer starter edits until
the CLI catches up is correct, but with one caveat: **the closeout/advance starter
change is GROUNDED today** and can land independently of the rest.

The recommended sequence is six phases: docs land first; named-reviewer starter
agents are extracted from the loop-worker profile; the closeout/advance contract
edit lands once docs have settled; per-stage native agents and `da config explain`
land in parallel CLI tracks; scope-routed `da review` and the two-tier
config-distribution model land last because they are the largest unconverged
contracts.

---

## 1.5 Original-intent fidelity (transcript ground truth)

The codex session transcript (`/Users/nikashp/.codex/sessions/2026/05/25/rollout-2026-05-25T23-12-44-019e6245-3977-7ff3-94f7-94dc92463bd1.jsonl`, 1,922 lines, 21 user turns, 150 assistant turns) captures the explicit user asks. Mapped against the proposals:

| User ask (verbatim or near-verbatim) | Proposal that captured it | Fidelity |
|---|---|---|
| "salvage the active loop.md from the proj-mega-branch, Check what else is available" | recovery doc + `staged-profile-dispatch-and-return-gate` §2 ("Verified Current State") | FAITHFUL — recovery doc enumerates what was salvaged |
| "profiles should probably be added to the system prompt upon work of a sub-agent. Should we put the loopworker.md profile into the system prompt and then have the project-specific overlay in the agent MD, or keep a version of the loop worker but stripped down to just other specific details" | `staged-profile-dispatch-and-return-gate` §1 ("Profile and system-instruction concerns were being conflated"); resolves with the four-part composition (stable bounded-stage instructions + named stage/lens instructions + stage-safe overlay + task/bundle context) | FAITHFUL + extends scope (adds the four-part composition the user did not enumerate) |
| "in the stage profile, the only info or verifier type is the specific reviewer. Those should be different agent MDs so it's super easy to spawn them as the worker" | `staged-profile-dispatch-and-return-gate` §7 ("Individual reviewers are the wrong merge-back owner") + Target Ownership Contract table | FAITHFUL on the reviewer split; **UNDER-CAPTURED** on the implementation: the proposal does not actually produce the per-lens AGENT.md files. This is precisely the "contract evaporation" gap §2.4 flagged. |
| "make sure that we inject the system complex into the local complex, loop worker, and the product overlay so that those are natively instilled into the specific stages agents" | `agent-context-resolution-architecture` §2 (dispatch decision) | FAITHFUL conceptually; ASPIRATIONAL because no current CLI does this injection |
| "thorough review of our work so far and just see if there's any duplicate, how else we can clean up our existing agent config set that we're providing as the starter" | starter edits on PR #119 (deferred from #120) + `TestStarterStagedDispatchDoesNotInjectLegacyCloseoutSurface` | FAITHFUL on the audit; the enforcing test is the cleanup contract |
| "for this one i'm leaning to the parent / orchestrator doing it as well, not by child stages" (re: closeout) | `staged-profile-dispatch-and-return-gate` §7 + ownership table (parent owns delegation closeout, direct work owns advance) | FAITHFUL and **GROUNDED in code** — closeout already sets status=completed |
| "config-explain-live-surface and scope-routed-da-review and everything else deemed active should be salvaged" | restored to `.agents/proposals/` | FAITHFUL — direct file restoration |
| "While we have the spec for app-type-profiles and a model defined want us to critique it and spot inefficencies and missed opportunities for config useage" | `staged-profile-dispatch-and-return-gate` §3 ("Current config surfaces cannot explain a dispatched stage") + §4 + §5 + "Missed Config Opportunities" enumeration | FAITHFUL + thorough; 10 missed-opportunity items each map back to an existing config surface |
| "For the bundle worker closeout shape should be aligned with the stages. lets analyze where realistically the merge-back owner should be regardless of curr implementation" | §7 + Target Ownership Contract table (named reviewer ≠ aggregation owner; deterministic parent-invoked return gate owns the consolidated merge-back) | FAITHFUL + **adds scope user did not enumerate** (codex invented the "return-gate" concept rather than reusing an existing entity). Not necessarily wrong, but worth flagging as codex-elaborated, not user-requested. |
| "ensure the proposal + spec have all the details so we can give that to a planning agent to create the canonical workflow/{plans|specs}/<xx>/... with da" | all five proposals + spec updates | FAITHFUL on packaging; **OMITTED** the actual `da workflow plan create` call (deliberately, per the workflow-artifact-model: spec → plan, not proposal → plan) |
| "Did you consider .agents/workflow/specs/external-agent-sources when doing this?" | `staged-profile-dispatch-and-return-gate` §4 ("External source packaging needs an explicit boundary") + Relationship table row 7 + Missed Config Opportunities #8 | FAITHFUL — added in response to the explicit question, correctly bounded |
| ".agents/workflow/specs/org-config-resolution/design.md" | `staged-profile-dispatch-and-return-gate` §5 ("Organization layering requires staged policy locks and override audit") + spec touchup (1-line deletion on PR #120) | FAITHFUL on relationship-mapping; the 1-line deletion is mechanical correction |

**Codex-elaborated additions not in user asks.** These are scope codex added on its own; flag for user review:

1. The "return-gate / aggregation-gate" concept as a distinct resolved entity (vs. just "parent does it") — user asked who owns merge-back, codex answered with a new named gate.
2. The "Tier 1 vs Tier 2 vs runtime-bundle vs registry-bundle" four-way packaging boundary in `staged-profile-dispatch-and-return-gate` §4 + Missed Opportunities #8 — user asked about external-agent-sources consideration, codex enumerated a typology.
3. The non-destructive starter-seed migration boundary in `config-distribution-model` §10 + `staged-profile-dispatch-and-return-gate` §6 + Missed Opportunities #10 — user asked about consistency, codex added an entire migration-discipline section.
4. The execution-telemetry pillar in `agent-context-resolution-architecture` §1.5 + §1.6 — substantial scope addition; not in any user message in this session (likely carried forward from prior sessions / proposals).
5. The §6.5 "Audit-confirmed pipeline state" in `agent-context-resolution-architecture` — derived from a separate audit pass, not from this session's user asks.

**User asks the proposals omit or under-capture.**

1. "keep a version of the loop worker but stripped down to just other specific details that may only be necessary for true dynamic runtime there" — codex chose to make loop-worker "legacy/full-slice compatibility only" rather than stripped-down-runtime. These are different decisions: the user asked *should* it be stripped or relegated; the proposal *picked* relegated. Worth confirming.
2. The starter-AGENT.md files for the three reviewer lenses — flagged as the load-bearing omission in §2.4 above.
3. Implementation-agent (`impl-agent`) and per-verifier-type agent definitions — the user asked broadly for "different agent MDs so it's super easy to spawn them as the worker"; codex restricted scope to reviewers + return-gate. Impl + verifier per-type agents are still aspirational and not even sketched.

**Transcript-derived sequencing hint.** The user's interrupt on the closeout-ownership question ("leaning to the parent / orchestrator doing it as well, not by child stages") + subsequent acceptance of codex's framing is the strongest signal that **Phase 2 (closeout/advance contract fix) is the highest-confidence change in the package** — it has explicit user buy-in AND is GROUNDED by code. Land it first when the lens agents arrive.

---

## 2. Per-artifact assessment

### 2.1 `agent-context-resolution-architecture.md` (proposal)

**Intent.** Frame four parallel specs (`scoped-knowledge-graphs`, `skill-tiering-contract`,
`app-type-profiles`, the unwritten `agent-context-resolution`) against one question:
at task dispatch time, how does the worker get the right knowledge, tools, and bounds?
The doc names the dispatch contract, the resource-graduation matrix, and the
execution-telemetry pillar that the score formula already presupposes.

**Gap classification.**

| Claim | Class | Evidence |
|---|---|---|
| Four anchor specs exist and overlap | GROUNDED | `.agents/workflow/specs/{scoped-knowledge-graphs,skill-tiering-contract,app-type-profiles,org-config-resolution}/` all present |
| `da review` is a usable async peer-review pipeline | GROUNDED | `commands/review.go` implements list/show/approve/reject + apply-then-archive |
| `kg note add/update/view/search`, `kg evidence`, `kg review-packet`, `kg promote`, `kg trace query`, `kg observations sweep` | ASPIRATIONAL | None of these subcommands exist; `commands/kg/` covers code-graph not note CRUD |
| `.agentsrc.json.promotion_gates` and `tier_defaults` are extendable | ASPIRATIONAL | `internal/config/agentsrc.go` has no such fields and `agentsRCKnown` does not list them; per `[[schema-usage]]` adding them is a four-place change + schema |
| `workflow advance` post-closeout is "after reviewing merge-back" | CONTRADICTS the actual CLI behavior — see §2.4 |
| Audit findings (`6.5`) about dead-coded review block, self-review wiring, 8% conversion rate | GROUNDED (per the §6.5 audit; cross-references real iter-log + history paths) |

**Recommended tasks.**
1. Stub `kg note` CRUD + `kg evidence` + `kg review-packet` behind a feature flag — smallest blast radius, unblocks every other claim in this doc.
2. Extend `.agentsrc.json` schema with `promotion_gates` + `tier_defaults` as a four-place atomic change (per `[[schema-usage]]`).
3. Draft canonical spec at `.agents/workflow/specs/agent-context-resolution/design.md` resolving the 10 open questions in §6.

---

### 2.2 `config-explain-live-surface.md` (proposal)

**Intent.** Add a `da config explain [field-path]` introspection primitive that
answers "what is the effective value, and which layer set it?", with `--value-only`,
`--origin-only`, `--json`, `--all`, `--flags` modes. Split from `da explain` (human
docs) and `workflow app-types` (authoring shortcut).

**Gap classification.**

| Claim | Class | Evidence |
|---|---|---|
| No `da config` subtree exists today | GROUNDED | `commands/root.go` has no `NewConfigCmd`; no `commands/config.go` or `commands/config/` directory |
| `da explain` is human docs, not effective-config introspection | GROUNDED | `commands/explain.go` is a static lookup surface |
| `workflow app-types` exists and should consume a shared resolver | GROUNDED | `commands/workflow/app_types.go` exists and currently reads `.agentsrc.json` directly |
| Effective-config snapshot API exists | ASPIRATIONAL | no provenance-tracking resolver in `internal/config/` |
| Resolved-execution-manifest visibility (profile digest, stage agent refs, return-gate ref) | ASPIRATIONAL | depends on the entire staged-dispatch + config-distribution model landing first |

**Recommended tasks.**
1. Land a shared effective-config snapshot API in `internal/config/` returning `{value, layers[], active_layer}` per field — flat-config only first (no extends/packages).
2. Add `da config explain [field-path]` + `--value-only` + `--origin-only` + `--json`.
3. Refactor `workflow app-types` to consume the snapshot API (preserves user-facing contract).

---

### 2.3 `scope-routed-da-review.md` (proposal)

**Intent.** Convert `da review` from user-scope-only into scope-routed (project / user / team / org), where routing follows durable-write scope per `proposal-routing.md`. Defines high-level reviewer-facing requirements only; explicitly defers spec + plan.

**Gap classification.**

| Claim | Class | Evidence |
|---|---|---|
| `proposal-routing.md` rule exists | GROUNDED | `~/.agents/rules/dot-agents/proposal-routing.md` loaded into context |
| Current `da review` is user-scope-only | GROUNDED | `commands/review.go:60` Long says "stored under ~/.agents/proposals"; no project/team/org branch |
| `.agents/proposals/` is project-local | GROUNDED | repo has 12 entries already |
| Team / org backing stores exist | ASPIRATIONAL | no implementation; no scope abstraction |
| Project-local `da review` will need transactional apply + rollback parity with user scope | ASPIRATIONAL | `config.ApplyProposal` is user-scope-coupled; project apply needs its own path |

**Recommended tasks.**
1. Author canonical spec at `.agents/workflow/specs/scope-routed-da-review/design.md` (intentionally separate from this proposal per `[[workflow-artifact-model]]`).
2. Add project-scope `da review` first (smallest delta: read `.agents/proposals/*.md` plus YAML; apply to repo root; archive in repo). Team/org deferred.
3. Add `--scope` flag with deterministic ambiguity error when an ID exists in multiple visible scopes.

---

### 2.4 `staged-profile-dispatch-and-return-gate.md` (proposal)

**Intent.** Decouple pipeline profile (what stages, verifiers, review) from system-instruction injection (bounded-stage instructions + named stage agent + project overlay). Resolve a "return gate" that owns the consolidated merge-back artifact instead of pinning it on the last reviewer. Migrate `loop-worker` to legacy-only; typed ISP stages get native per-stage agents.

**Gap classification.**

| Claim | Class | Evidence |
|---|---|---|
| `app-type-profiles/design.md` defines `profile` as a versioned pipeline bundle | GROUNDED | spec exists; design ref'd from `verified current state` |
| `workflow fanout` persists app-type + verifier sequence only | GROUNDED | `commands/workflow/bundle.go` `b.Closeout.WorkerMust = [...]` (line 1510) hard-codes legacy worker steps |
| `workflow delegation closeout --decision accept` is the parent-owned op that mutates canonical state | GROUNDED | `applyCloseoutDecisionToTasks` in `delegation.go:1608-1649` sets `Status = "completed"` on accept |
| Therefore the starter is wrong when it tells the parent to run `workflow advance` after closeout | GROUNDED | `profiles/loop-worker.md:60`, `skills/global/isp/instructions/staged-runtime.md:41`, `skills/global/iteration-close/instructions/workflow.md:179,200` all carry the redundant advance |
| Per-stage native agents (`impl-agent`, verifier per-type, named reviewer-lens agents) | ASPIRATIONAL | only `loop-worker/AGENT.md` exists in `internal/scaffold/home/starter/agents/global/`; `staged-runtime.md` references `.agents/prompts/{impl,review}-agent.project.md` (which exist locally in this repo only — not in starter, not as proper agent definitions) |
| 3 review lenses (architecture-standards, acceptance-invariants, adversarial) need named agents | ASPIRATIONAL | lenses currently only live as prose in `profiles/loop-worker.md` lines 17-46 |
| Two-lens vs three-lens contract conflict | CONTRADICTS itself today | starter `isp/instructions/staged-runtime.md:31` says "Two-lens contract: phase 1 (product, domain, stability) → phase 2 (tech-lead, architecture, standards)"; starter `profiles/loop-worker.md` says three lenses. This is a pre-existing inconsistency the codex thread surfaces but does not yet resolve |
| `CopyMissingStarterAssets` is non-destructive | GROUNDED | `internal/scaffold/home/copy.go`; `TestCopyMissingStarterAssetsPreservesExistingFiles` exercises this branch on `origin/codex-019e6245-mega-salvage-recovery` |
| The enforcing test on PR #119 (`TestStarterStagedDispatchDoesNotInjectLegacyCloseoutSurface`) asserts removal of "After accepting delegate output: `workflow advance`" and addition of "Accepted delegation closeout completes the delegated task" | GROUNDED + internally consistent with the proposal |

**Skeptical read** (per the mandate): the codex DID propose removal *and* destinations.
The named-reviewer destination ("each reviewer-lens an independently spawnable agent")
is described in §7 of the proposal, but **the proposal does not produce the starter
agent files**. PR #119's diff edits `profiles/loop-worker.md` and `loop-worker/AGENT.md`
but does not add the three reviewer-lens `AGENT.md` files. That is the
"contract evaporation" risk the orchestrator flagged. **The fix is mechanical**:
the lens prose already exists in `profiles/loop-worker.md:17-46` and just needs to
move into three new starter agent definitions.

**Recommended tasks.**
1. **Extract reviewer lenses into starter agents** (the missing half of #119). Add three files (paths in §4).
2. **Drop "after accepted closeout, run `workflow advance`"** from all four starter touchpoints. Add the assertion test as the gate.
3. **Migrate ISP staged-runtime two-lens / three-lens prose to match `profiles/loop-worker.md`** — pick three lenses (consistent with the loop-worker profile) and update `isp/instructions/staged-runtime.md:31`.
4. **Add named stage agents** (`impl-agent`, verifier-per-type) in a follow-up CLI track; keep the bundle's `closeout.worker_must` honest for typed stages.

---

### 2.5 `verify-record-review-direct-iteration.md` (proposal)

**Intent.** Make `workflow verify record --kind review` succeed for direct (non-delegated) iterations by auto-materializing a `direct`-profile delegation contract on first verify or task start. Avoids hand-written `review-decision.yaml` files and lets the iter-log review block stay populated for direct work.

**Gap classification.**

| Claim | Class | Evidence |
|---|---|---|
| `verify record` requires a contract today | GROUNDED | the proposal's failure trace is reproducible; `loadMergeBack` + `reconcileDelegationContractForCloseout` both depend on the contract path |
| `worker.profile: direct` does not exist as a sentinel | GROUNDED — new field |
| The auto-materialize-on-`advance --status in_progress` change is non-breaking | GROUNDED for existing delegated flows: fanout would precede any advance call, so the contract is already on disk |
| `workflow contract create` subcommand | ASPIRATIONAL — new CLI surface |

**Recommended tasks.**
1. Add `da workflow contract create --task <id> [--from-plan] [--force]` that synthesizes a minimal direct-iteration contract.
2. Wire auto-materialize on first `verify record --kind review|test` when no contract is present.
3. Wire auto-materialize on `workflow advance ... --status in_progress`.
4. Teach merge-back / fold-back to no-op gracefully for `worker.profile: direct` (merge-back is not required for direct work).

---

### 2.6 `workflow/specs/config-distribution-model/design.md` (spec)

**Intent.** Define the two-tier model — Tier 1 config layers (policy, fetched as raw files via git/http/local) and Tier 2 executable packages (versioned OCI artifacts) — with `sources` + `extends` + `packages` fields on `.agentsrc.json`, a two-pass resolution engine, unified `.agentsrc.lock` lockfile, per-tier caching semantics, audit taxonomy additions, and the `da config explain` command. The codex addition (§10 "Resolved execution manifest visibility") wires the staged-dispatch + org-policy-lock context into the explain surface.

**Gap classification.**

| Claim | Class | Evidence |
|---|---|---|
| `sources` field exists on `AgentsRC` | GROUNDED for shape, ASPIRATIONAL for content | `internal/config/agentsrc.go:126,249` has `Sources []Source` with `Type ∈ {local, git}` only; no `id`, no `cache_ttl`, no `http`, no `oci` |
| `extends` and `packages` fields | ASPIRATIONAL | not in struct, not in schema, not in `agentsRCKnown` |
| `.agentsrc.lock` file | ASPIRATIONAL | no codepath writes one |
| OCI source type | ASPIRATIONAL | depends on `external-agent-sources` spec, no implementation |
| `da config sync` / `da packages install` / `da config verify` / `da config lint` | ASPIRATIONAL | no `da config` or `da packages` subtree |
| `da install` repurpose, `da refresh` alias to `da config sync` | CONTRADICTS current behavior | `da refresh` is the projection refresh path today; aliasing it changes user contract |
| Feature-flag gating at command entry | ASPIRATIONAL — no feature-flag plumbing in the binary today |

**Recommended tasks.**
1. Schema work first: extend `Source` struct with `id` + `cache_ttl`, add `http` + `oci` types (validation only, no fetch yet).
2. Add `Extends []LayerRef` + `Packages []PackageRef` to `AgentsRC` (four-place atomic change per `[[schema-usage]]`).
3. Land Tier 1 only: fetch layers via git/http, two-pass resolver, lockfile config-section. Defer Tier 2 (OCI) to a later phase.

---

### 2.7 `workflow/specs/org-config-resolution/design.md` (spec)

**Intent.** Define the layered-resolution semantics (product defaults → user-local → org → team → repo layers → repo-local → plan/task/runtime overrides) that the config-distribution model implements. Repo identity is stable cross-checkout; workspaces are optional convenience; feature rollout is per-repo/per-team. The codex diff is a single-line removal — surgical correction.

**Gap classification.**

| Claim | Class | Evidence |
|---|---|---|
| Resolution layer model | ASPIRATIONAL contract; config code today only knows user + repo |
| Repo identity stable cross-checkout | ASPIRATIONAL — `AgentsRC.Project` is a free-form string, not a repo-identity hash |
| Workspace-optional model | GROUNDED design choice; no current workspace concept to invalidate |
| Feature-flag rollout | ASPIRATIONAL |
| §8.6 (inherited staged-policy locks + override audit) | ASPIRATIONAL; depends on `staged-profile-dispatch-and-return-gate` |

**Recommended tasks.**
1. Add `repo_id` to `AgentsRC` as the identity key (separate from `project`); default from `git remote get-url origin` at `da init`.
2. Surface effective layer order in `da config explain --layers` once §2.6 task 1 lands.
3. Defer §8.6 policy-lock work until after `staged-profile-dispatch-and-return-gate` spec is canonical.

---

## 3. Cross-cutting sequenced plan

```
Phase 0 — Land the docs (DONE on PR #120 once merged)
  • All 5 proposals + 2 spec updates land additively. No code changes.

Phase 1 — Reviewer-lens starter agents (the missing half of PR #119)
  • Extract 3 lens prose blocks → 3 starter AGENT.md files
  • Update profiles/loop-worker.md to point at the named agents instead of inlining

Phase 2 — Closeout/advance contract edit (the deferred half of PR #119, GROUNDED today)
  • Drop "After accepted closeout, run workflow advance" from 4 starter touchpoints
  • Add TestStarterStagedDispatchDoesNotInjectLegacyCloseoutSurface
  • Reconcile two-lens vs three-lens prose in isp/instructions/staged-runtime.md

Phase 3 — Direct-iteration contract auto-materialization
  • New `da workflow contract create` subcommand
  • Auto-materialize hooks on verify record + advance --status in_progress

Phase 4 — Effective-config snapshot API + `da config explain` (flat scope only)
  • New `commands/config/` subtree
  • `da config explain [field-path]` + flags
  • Refactor `workflow app-types` to consume snapshot

Phase 5 — Scope-routed `da review` (project scope first)
  • Canonical spec under workflow/specs/scope-routed-da-review/design.md
  • Project-scope review path; team/org deferred

Phase 6 — Tier-1 config-distribution
  • Extend Source struct with id + cache_ttl + http
  • Add Extends to AgentsRC + schema
  • Two-pass resolver + lockfile config-section
  • Defer Tier-2 OCI + packages CLI to a follow-on plan

Phase 7+ — Per-stage native agents, KG note CRUD, telemetry, Tier-2 OCI
  • Each gets its own canonical plan once the substrate (Phase 1–6) settles
```

---

## 4. Tasks ready for `da workflow plan create`

| id (slug) | title | write_scope | depends_on | notes |
|---|---|---|---|---|
| `lens-agents-starter` | Extract 3 reviewer lenses into named starter agents | `internal/scaffold/home/starter/agents/global/architecture-standards-reviewer/AGENT.md`, `internal/scaffold/home/starter/agents/global/acceptance-invariants-reviewer/AGENT.md`, `internal/scaffold/home/starter/agents/global/adversarial-reviewer/AGENT.md`, `internal/scaffold/home/starter/profiles/loop-worker.md`, `internal/scaffold/home/copy_test.go` | — | Each AGENT.md frontmatter: `name: <lens>-reviewer`, `description: Single-lens review worker. Reads delegation bundle, emits findings + verdict for one lens. Never edits production code.`, `tools: Read, Grep, Glob, Bash`. Body: role + lens criteria (copied verbatim from `profiles/loop-worker.md:17-46`) + findings format (BLOCKER/HIGH/MEDIUM/LOW + file:line) + closeout via `/iteration-close`. |
| `closeout-advance-contract-fix` | Drop redundant `workflow advance` after accepted closeout from starter | `internal/scaffold/home/starter/profiles/loop-worker.md`, `internal/scaffold/home/starter/agents/global/loop-worker/AGENT.md`, `internal/scaffold/home/starter/skills/global/isp/instructions/staged-runtime.md`, `internal/scaffold/home/starter/skills/global/iteration-close/instructions/workflow.md`, `internal/scaffold/home/copy_test.go` | `lens-agents-starter` | Add `TestStarterStagedDispatchDoesNotInjectLegacyCloseoutSurface` from PR #119. GROUNDED by `delegation.go:1608-1649`. |
| `isp-lens-count-reconcile` | Reconcile two-lens vs three-lens prose in ISP staged-runtime | `internal/scaffold/home/starter/skills/global/isp/instructions/staged-runtime.md` | `lens-agents-starter` | Adopt the three-lens contract from `profiles/loop-worker.md`; line 31 currently says "two-lens phase 1/phase 2". |
| `direct-contract-create` | `da workflow contract create` subcommand + auto-materialize on verify-record | `commands/workflow/cmd.go`, `commands/workflow/delegation.go`, `commands/workflow/verification.go`, new test file | — | Implements `verify-record-review-direct-iteration.md`. New `worker.profile: direct` sentinel; idempotent + `--force`. |
| `config-snapshot-api` | Effective-config snapshot API + `da config explain` flat | `commands/root.go`, new `commands/config/explain.go`, `internal/config/snapshot.go`, tests | — | Flat-config only (no extends/packages); supports `--value-only`, `--origin-only`, `--json`, `--all`, `--flags`. |
| `app-types-uses-snapshot` | Refactor `workflow app-types` to consume snapshot API | `commands/workflow/app_types.go`, tests | `config-snapshot-api` | No user-facing contract change. |
| `scope-routed-review-spec` | Canonical spec for scope-routed `da review` | `.agents/workflow/specs/scope-routed-da-review/design.md` | — | Spec only; plan + impl follow per `[[workflow-artifact-model]]`. |
| `project-scope-review-mvp` | Add project-scope reading + `--scope` flag to `da review` | `commands/review.go`, `internal/config/proposal.go` (or wherever LoadProposal lives), tests | `scope-routed-review-spec` | Project scope only; team/org deferred. Deterministic ID-ambiguity error. |
| `source-tier1-extension` | Extend `Source` + add `Extends` field for Tier-1 layers | `internal/config/agentsrc.go`, `schemas/agentsrc.schema.json`, tests | `config-snapshot-api` | Four-place atomic change per `[[schema-usage]]`. No fetch implementation yet — validation only. |
| `repo-id-field` | Add `repo_id` field to `AgentsRC` | `internal/config/agentsrc.go`, `schemas/agentsrc.schema.json`, `commands/init.go` (auto-populate from git remote), tests | — | Identity for the `org-config-resolution` layer model. |

---

## 5. TODO follow-ups (too small to canonicalize)

- Update `~/.agents/profiles/loop-worker.md` doc comment to reference named lens agents rather than inlined lens prose (mechanical, part of `lens-agents-starter`).
- Make `iteration-close/instructions/workflow.md:179-200` table consistent with the closeout/advance fix (single-edit follow-up to `closeout-advance-contract-fix`).
- Drop `commands/workflow/bundle.go:1511` `ParentMust = ["workflow_delegation_closeout"]` reminder that no longer needs to mention advance.
- Add an `XXX` comment in `staged-runtime.md` line 12 / 22 / 29 pointing at the missing starter `.agents/prompts/{impl-agent,review-agent,verifiers}.project.md` until `lens-agents-starter` covers reviewers and a follow-on covers impl + verifiers.

---

## 6. Risk register

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| Phase-2 starter edit lands without the Phase-1 lens agents, leaving the loop-worker profile pointing at non-existent agent files | medium | high — "contract evaporation" recurrence | Make `closeout-advance-contract-fix` `depends_on: lens-agents-starter` in TASKS.yaml (already encoded above). |
| `CopyMissingStarterAssets` skips updates for users who have a customized `~/.agents/profiles/loop-worker.md` — proposal-correct starter doesn't reach them | high | medium — split-brain installs | Land §13 of `config-distribution-model` (starter-seed provenance + reviewed upgrade) before recommending users re-run `da init`. Until then, surface drift in `da doctor`. |
| Phase-4 `da config explain` builds a snapshot API that later needs to be rewritten for two-tier resolution | medium | medium | Author `internal/config/snapshot.go` with a `Resolver` interface; flat-config impl is the first; tier-1 impl is a second; explain command depends on the interface. |
| Two-lens vs three-lens reconciliation in ISP staged-runtime triggers an in-flight ISP run inconsistency | low | medium | Land `isp-lens-count-reconcile` in the same commit as `lens-agents-starter`; no in-flight ISP run depends on the count string. |
| `worker.profile: direct` sentinel conflicts with future profile values (e.g. `claude-rescue`) | low | low | Make the sentinel an enum gated by schema; document under `delegation.go` types. |

---

## 7. Open questions for the user

1. **Lens executor neutrality.** `profiles/loop-worker.md:43-46` says any capable bounded worker (loop-worker or codex-rescue) may execute a lens. Should each `*-reviewer/AGENT.md` declare an `executor:` hint, or stay executor-agnostic? Recommend: executor-agnostic; orchestrator picks per bundle.
2. **Two-lens vs three-lens.** Authoritative answer? `profiles/loop-worker.md` says three (architecture-standards, acceptance-invariants, adversarial). `isp/instructions/staged-runtime.md` says two (product/domain → tech-lead/architecture). The codex thread treats three as canonical. Recommend confirming three.
3. **Per-stage agent naming.** Should the impl stage agent be `impl-agent`, `implementer`, or `staged-impl-worker`? The PA-cursor `.agents/prompts/impl-agent.project.md` filename suggests `impl-agent`; the staged-runtime instructions use `impl-agent.project.md`. Recommend `impl-agent`.
4. **`da refresh` aliasing.** Spec §13.3 proposes aliasing `da refresh` to `da config sync`. This changes user contract (today `da refresh` is the projection refresh path). Recommend keeping `da refresh` as-is and giving `da config sync` its own verb.
5. **Direct-contract profile name.** `direct` vs `direct-iteration` vs `none`? Recommend `direct` per the proposal.
6. **Scope routing precedence.** When the same proposal ID exists at project + user scope (e.g. someone copies a global proposal locally to iterate), should the CLI error or pick narrowest scope? Recommend error with `--scope` resolution.
7. **Phase 6 ordering.** Should Tier-1 fetcher land before or after `repo-id` field? Recommend `repo-id` first because the cache path needs an identity component.

---

## 8. Cross-references

- `[[validate-bundle-against-head]]` — applies to Phase-1/Phase-2 starter edits: validate the proposal's quoted starter strings still match starter text on HEAD before authoring the enforcing test.
- `[[schema-usage]]` — applies to Phase-3 (`worker.profile: direct`), Phase-6 (`Extends`/`Packages`/`repo_id` AgentsRC fields). Four-place atomic change required.
- `[[additive-state-fields]]` — applies to the planned snapshot API: add new fields, never repurpose; use `[]T{}` not nil for slice fields.
- `[[worktree-no-cd]]` — applies to any worker spawned to implement these tasks: use `git -C <path>` not `cd`.
- `[[loop-worker-vs-general-purpose]]` (memory) — applies to Phase-1 reviewer agents: declare `tools:` narrowly so they cannot edit production code.
- `[[starter-vs-project-overlay]]` (memory) — applies to Phase-1/Phase-2: never overwrite a customized installed starter without provenance check.

---

## 9. Bottom-line recommendation

Land PR #120 as-is (the docs are good). Open three new PRs immediately after merge:

- **PR-A:** `lens-agents-starter` + `closeout-advance-contract-fix` + `isp-lens-count-reconcile` — the deferred half of PR #119, now safe to land because the starter destination exists.
- **PR-B:** `direct-contract-create` — independent CLI track, unblocks `verify record --kind review` for direct work.
- **PR-C:** `config-snapshot-api` + `app-types-uses-snapshot` — foundation for both `da config explain` and the eventual two-tier resolver.

The remaining proposals (scope-routed review, two-tier config-distribution, KG note CRUD, named impl/verifier per-stage agents) graduate to canonical specs+plans on their own cadence as PR-A through PR-C settle.

---

## 10. Re-sequenced against `config-v2-migration` (added 2026-05-27)

The canonical plan `config-v2-migration` (`.agents/workflow/plans/config-v2-migration/`) now
owns the two-tier `sources`/`extends`/`packages` implementation that this examination's
Phase 6 sketched. See `.agents/proposals/config-v2-dependency-map.md` for the full
dependency table; the integration in one paragraph:

**PR-A and PR-B are unchanged** — both are orthogonal to v2 and ship independently.

**PR-C is RE-SEQUENCED.** Codex's Phase-4 `config-snapshot-api` and `app-types-uses-snapshot`
deliverables map onto `config-v2-migration` tasks `p1-resolver-core-flat`, `p4-config-explain-cli`,
and `p4b-app-types-snapshot-refactor`. Do NOT open a standalone PR-C branch; ship the
snapshot API as the natural milestone of the canonical plan. The user-facing value is
identical (`da config explain` lands at p4) but the implementation is structured for the
v2 substrate from day one rather than retrofitted later.

**Codex Phase 6 (`tier-1 config-distribution`)** is **now fully owned by `config-v2-migration`**:
the task table rows `source-tier1-extension` and `repo-id-field` map to p0 + p0b.
`Extends`/`Packages` schema work lands in p0. Two-pass resolver + lockfile + Tier-2 OCI
roll out across p1b → p5 → p6.

**Codex Phase 5 (`scope-routed-da-review`)** keeps its own cadence: project-scope MVP can
ship against v1 (reads `.agents/proposals/*.md` without resolver changes); team/org scope
waits for `config-v2-migration` p1 + p5.

**Codex Phase 7+ (per-stage native agents, KG note CRUD)** remain their own future plans.
Per-stage agents wait for `config-v2-migration` p6 if their refs need OCI package
resolution; KG note CRUD is fully orthogonal.

No edits to PR-A, PR-B, or the §4 task table are required by this re-sequencing — only
the §9 PR-C recommendation is superseded by the canonical plan's phased structure.
