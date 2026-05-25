# Loop-Discipline Stop Hooks Plan

- plan-id: `loop-discipline-stop-hooks`
- spec: [`../../specs/loop-discipline-stop-hooks/design.md`](../../specs/loop-discipline-stop-hooks/design.md)
- predecessor scoping doc: `.agents/proposals/loop-discipline-stop-hooks-scoping.md`
- related: `r1-outcome-scoring` (completed baseline),
  `r1-5-hook-enforcement-telemetry` (gate telemetry follow-up)

## Summary

Stop / SubagentStop hook enforcement for `iteration-close`, `isp`, and
`loop-worker`; promote skills into the starter; close platform
event-coverage gaps. Hooks read sentinel files written by the skills and
validate that the discipline contracts (verify → checkpoint →
merge-back; orchestrator stage gates; subagent write-scope confinement)
were upheld before allowing stop. Hard violations request each platform's
native continuation/remediation behavior; soft violations emit stderr
advisories.

## Reading order

1. The **spec** (`../../specs/loop-discipline-stop-hooks/design.md`) is
   the contract. Decisions, requirements, done criteria, and open
   questions all live there.
2. This plan describes **how** the spec gets implemented and **in what
   order**.
3. `TASKS.yaml` is the live work queue; `PLAN.yaml` is its canonical
   header.
4. Task implementation details live in the linked companion contracts
   below; they pin interfaces, source maps, acceptance checks, and
   explicit exclusions without overloading `TASKS.yaml` notes.

## Companion contracts

| Task | Contract | Pinned decision |
| --- | --- | --- |
| `p0-sentinel-cli` | [`tasks/p0-sentinel-cli.contract.md`](tasks/p0-sentinel-cli.contract.md) | `workflow hook-sentinel`; archive to plan history |
| `p1a-mapper-extensions` | [`tasks/p1a-mapper-extensions.contract.md`](tasks/p1a-mapper-extensions.contract.md) | gate-critical event mappings only |
| `p1b-canonical-when-values` | [`tasks/p1b-canonical-when-values.contract.md`](tasks/p1b-canonical-when-values.contract.md) | non-gate documented parity |
| `p1c-matcher-verification` | [`tasks/p1c-matcher-verification.contract.md`](tasks/p1c-matcher-verification.contract.md) | `when_events`, trace inputs, native outputs |
| `p1d-claude-lifecycle-parity` | [`tasks/p1d-claude-lifecycle-parity.contract.md`](tasks/p1d-claude-lifecycle-parity.contract.md) | verified Claude wider lifecycle parity |
| `p2-hook-scripts` | [`tasks/p2-hook-scripts.contract.md`](tasks/p2-hook-scripts.contract.md) | evidence-backed remediation; missing trace advises |
| `p3-starter-promotion` | [`tasks/p3-starter-promotion.contract.md`](tasks/p3-starter-promotion.contract.md) | recursive starter copy; no loader expansion |
| `p3b-companion-discipline-skills` | [`tasks/p3b-companion-discipline-skills.contract.md`](tasks/p3b-companion-discipline-skills.contract.md) | scaffold companions; hook only with evidence |
| `p4-sentinel-wiring` | [`tasks/p4-sentinel-wiring.contract.md`](tasks/p4-sentinel-wiring.contract.md) | write after invocation context is known |
| `p5-e2e-integration` | [`tasks/p5-e2e-integration.contract.md`](tasks/p5-e2e-integration.contract.md) | repo-conventional shell smoke test |
| `p6-payout-backfill` | [`tasks/p6-payout-backfill.contract.md`](tasks/p6-payout-backfill.contract.md) | explicit downstream readback |
| `p7-legacy-override-migration` | [`tasks/p7-legacy-override-migration.contract.md`](tasks/p7-legacy-override-migration.contract.md) | final legacy override sweep |

## Phase-by-phase narrative

The enforcement core ships through P5. P0 and P1a open parallel work on
P1b, P1c, and P3; P2 follows P1c because its multi-event hook manifest
uses the representation pinned there. P1d expands documented Claude
parity without changing the gate set. P3b assesses and scaffolds adjacent
discipline skills. P6 migrates payout deliberately, and P7 is the final
downstream legacy-override sweep.

### P0 — Sentinel CLI surface (`p0-sentinel-cli`)

**Produces:** `da workflow hook-sentinel {write,read,clear}` with JSON
schema v1 under `.agents/active/hook-sentinels/<skill>-<run-id>.json`
while active and successful archives under
`.agents/history/<plan-id>/hook-sentinels/<YYYY-MM-DD>/`.

**Why first:** Every gate script reads sentinels via this CLI. Without
it, gate scripts cannot be authored or tested. Sentinel shape also
constrains what fields the gates can validate, so freezing the schema
first prevents churn.

**Unblocks:** P1a (matcher logic needs to know what `agent_type` field
the gate self-filter will read), P2 (gate scripts depend on the CLI),
P4 (skill wiring calls the CLI).

**Contract decisions:** Q1 nests the command under `workflow`; Q2
archives cleared successful records in durable plan history, with pruning
deferred.

### P1a — Mapper extensions (`p1a-mapper-extensions`)

**Produces:** Gate-critical extensions to `codexEventName` /
`copilotEventName` / `cursorEventName` in `internal/platform/hooks.go`:
Codex `subagent_stop` to `SubagentStop`, Copilot `stop` to `agentStop`
and `subagent_stop` to `subagentStop`, and Cursor `subagent_stop` to
`subagentStop`. Tests land in `internal/platform/hooks_test.go`.

**Why ordered this way:** Depends on P0 because the matcher tests need
to reflect the gate self-filter pattern (i.e. matchers stay minimal;
selection logic moves into the sentinel-aware gate script). Independent
of P1b/P1c — those refine but do not block this work.

**Unblocks:** P2 (hook scripts target these event names).

### P1b — New canonical `HookSpec.When` values (`p1b-canonical-when-values`)

**Produces:** Canonical When values for Cursor's wider event surface
(D3) and the May 2026 refresh deltas (D5: `post_compact`,
`error_occurred`). Other platforms no-op for the Cursor-only values via
D2 fall-through. A doc note in `docs/HOOKS.md` explaining the
one-to-one principle (D2), non-gate parity, and the operator pattern
for cross-platform coverage.

**Why ordered this way:** Depends on P1a so the mapper test scaffolding
is in place and the doc page exists to extend. Does not block P2/P3/P4
— the three gates themselves use only `stop` / `subagent_stop` /
canonical when_events.

### P1c — Input/matcher verification and `when_events` extension (`p1c-matcher-verification`)

**Produces:** Per-vendor verification table for which events accept a
`matcher` field and which Stop/SubagentStop-style inputs expose a
readable transcript path (resolves spec Q5 / scoping D.6). A
backward-compatible `when_events: []` schema extension lands here so
`iteration-close-gate` ships as one HookSpec instead of two (resolves
spec Q4 / scoping D.8).

**Why ordered this way:** Depends on P1a for the mapper baseline and
unblocks P2, whose iteration-close hook uses `when_events`.

Its vendor table must record that Cursor exposes agent/subagent transcript
paths, but native stop remediation uses `followup_message` rather than
`decision: block`.

### P1d — Claude lifecycle parity (`p1d-claude-lifecycle-parity`)

**Produces:** Canonical and documented Claude mappings for the wider
official lifecycle surface named in R6.6, with table-driven renderer tests.

**Why ordered this way:** It follows the general event-parity work in P1b,
but does not block the three enforcement bundles because they use the
already-critical stop events.

### P2 — Hook scripts (`p2-hook-scripts`)

**Produces:** Three bundles under `internal/scaffold/hooks/global/`:
`iteration-close-gate/`, `isp-gate/`, `loop-worker-gate/`, each with
HOOK.yaml + gate.sh. Gate scripts implement the two-tier violation
matrix from spec R1/R2/R3, reading sentinels via the P0 CLI.

**Why ordered this way:** Depends on P0 (CLI) and P1a (event names
need to render on all four platforms), and on P1c for the multi-event
HookSpec/input contract. It does not depend on P3 because hook scripts
ship in the scaffold tree separately from skill bundles. Rules that
depend on command history request hard remediation only when a verified
readable trace is present; lack of trace emits an advisory. Hard outcomes
render the platform-native continuation shape identified by P1c.

**Unblocks:** P5 (integration test exercises these scripts end-to-end).

### P3 — Starter promotion (`p3-starter-promotion`)

**Produces:** Skill bundles under
`internal/scaffold/home/starter/skills/global/{iteration-close,isp,loop-worker}/`,
the `loop-worker` AGENT.md under
`internal/scaffold/home/starter/agents/global/loop-worker/`, the
`loop-worker` profile under `internal/scaffold/home/starter/profiles/`,
and focused copy assertions in `internal/scaffold/home/copy_test.go`.

**Why ordered this way:** Independent of P0/P1 — the skill content
already exists in `~/.agents/skills/dot-agents/`; this task copies it
into the starter. `internal/scaffold/home.CopyMissingStarterAssets`
already walks arbitrary embedded descendants and
`commands/init.go:createInitialAgentsDirs` already creates
`agents/global`, so new loader code is not expected. Schedulable in
parallel with P0 and P1a.

**Unblocks:** P4 (skill wiring edits these starter files).

### P3b — Companion discipline skills (`p3b-companion-discipline-skills`)

**Produces:** Complete starter-scaffold assets for `agent-handoff` and
`delegation-lifecycle`, plus a written assessment of whether a stop-time
hook can enforce a deterministic invariant for either skill.

**Why ordered this way:** It follows P3's starter-copy pattern and P2's
evidence boundary. If no observable hook contract exists, scaffolding and
the assessment are the deliverable; the task must not invent advisory-only
hooks as enforcement.

### P4 — Sentinel wiring in skills (`p4-sentinel-wiring`)

**Produces:** A `da workflow hook-sentinel write` invocation at the
start of governed work in each of the three starter-shipped skill
bundles, after the invocation context required to populate the sentinel
has been resolved.
Plus a gotchas / proposal-criteria note in each SKILL.md so future
skill edits cannot silently break the hook contract.

**Why ordered this way:** Depends on P0 (the CLI it calls) and P3 (the
files it edits live in the starter). Sequenced after both for a
single-shot edit.

**Unblocks:** P5 (integration test runs the wired skills end-to-end).

### P5 — End-to-end integration (`p5-e2e-integration`)

**Produces:** Shell smoke test at `tests/test-loop-discipline-stop-hooks.sh`
following the repository's existing flat test convention. It exercises
an artifact/write-scope hard outcome, a trace-backed forbidden-command
hard outcome where verified input permits it, and advisory behavior for a
soft issue or unavailable trace. It asserts JSON block payloads where
documented and Cursor's native follow-up payload.

**Why ordered this way:** Depends on P2 (scripts under test) and P4
(skills must write sentinels). Final phase; landing this task closes
done-criterion DC7 and demonstrates the end-to-end discipline loop.

### P6 — Payout backfill (`p6-payout-backfill`)

**Produces:** A deliberate payout migration/readback record after applying
the starter-hook change in the payout workspace through an explicitly
scoped downstream operation. The readback verifies active loop resolution,
generated hooks, and preservation of unrelated active artifacts.

**Why ordered this way:** Payout already has active loop state and managed
rules invoking `/iteration-close`, while its `.agentsrc.json` currently
sets `"hooks": false` and omits `isp` / `loop-worker`; it is a real
migration target, not an implicit beneficiary of refresh. It follows
verified hook behavior.

### P7 — Legacy override migration (`p7-legacy-override-migration`)

**Produces:** The final downstream inventory and migration record for
legacy project-specific overrides of these discipline skills, identifying
each override migrated to starter inheritance or retained intentionally.

**Why ordered this way:** Payout provides the first concrete migration
readback. General override migration runs last so the contract is already
proven before replacing downstream variants.

## Dependency graph

```
p0-sentinel-cli ──> p1a-mapper-extensions ──> p1b-canonical-when-values
p1b-canonical-when-values ──> p1d-claude-lifecycle-parity ────────────────────────────────┐
p0-sentinel-cli ──> p1a-mapper-extensions ──> p1c-matcher-verification ──> p2-hook-scripts ──> p5-e2e-integration
p0-sentinel-cli ─────────────────────────────────────────────────────────> p2-hook-scripts
p0-sentinel-cli ──> p4-sentinel-wiring ──────────────────────────────────> p5-e2e-integration
p3-starter-promotion ──> p4-sentinel-wiring
p2-hook-scripts ──┐
p3-starter-promotion ──┴──> p3b-companion-discipline-skills ──> p6-payout-backfill ──> p7-legacy-override-migration
p5-e2e-integration ────────────────────────────────────────────> p6-payout-backfill
p1d-claude-lifecycle-parity ───────────────────────────────────────────────────────────────> p7-legacy-override-migration
```

P1a unblocks the maximum downstream parallelism. P3 is the only fully
independent path and should be scheduled alongside P0+P1a.

## Verification checkpoints

Per spec done criteria:

- **After P0:** unit tests for sentinel CLI green (DC1).
- **After P1a:** per-platform golden fixtures for Codex `SubagentStop`,
  Copilot `agentStop`, Cursor `subagentStop` render in
  `internal/platform/hooks_test.go` (DC2).
- **After P1b:** `docs/HOOKS.md` documents D2 mapping principle, D3
  Cursor surface, Copilot `agentStop` footgun (DC3).
- **After P1c:** documentation and tests pin trace fields and
  platform-native hard-remediation output, including Cursor follow-up.
- **After P1d:** verified Claude wider-lifecycle event parity lands
  without broadening the enforcement bundles (DC9).
- **After P2:** three HOOK.yaml + gate.sh exist under
  `internal/scaffold/hooks/global/` (DC4).
- **After P3:** three starter skill bundles + loop-worker AGENT.md
  exist; `CopyMissingStarterAssets` / `da init` against an empty home
  writes them (DC5).
- **After P4:** each starter skill calls `hook-sentinel write` before
  governed actions after required context is resolved (DC6).
- **After P5:** integration test green for artifact hard-remediation,
  trace-backed hard-remediation, and soft-advisory paths using the
  documented platform output contracts (DC7); manual sanity:
  `bin/da refresh` in a
  sandbox project shows no `directory not empty` warnings (DC8).
- **After P3b:** both companion skill trees materialize and the hook
  suitability assessment is recorded (DC10).
- **After P6:** payout migration/readback record is complete (DC11).
- **After P7:** downstream override inventory/migration closes last
  (DC12).

## Open decisions parked from scoping

These are tracked in the spec's "Open questions" section. The plan
resolves them in-task as listed:

- Q1 (CLI scope) → resolved in the P0 contract: nest under `workflow`.
- Q2 (sentinel lifetime) → resolved in the P0 contract: archive on
  successful clear under durable plan history; pruning is deferred.
- Q3 (hard-vs-soft threshold tuning) → constrained by D7: portable
  evidence and verified readable trace may block; unavailable trace
  advises. Calibrate message wording during P2/P5.
- Q4 (`when_events: []` extension) → resolved in the P1c contract:
  add a backward-compatible multi-event representation.
- Q5 (matcher-supported per vendor) → resolved in
  `p1c-matcher-verification`, including Cursor transcript and native
  continuation output verification.
- Q6 (loop-worker matcher portability) → resolved in the spec and P2
  contract: rely on gate-level `agent_type` filtering for v1.
- Q7 (payout backfill) → resolved by `p6-payout-backfill`, before the
  final general override migration in P7.

## Out of scope reminder

See the spec's "Deferred items" section. The plan still does NOT build
an OpenCode hooks-to-plugins bridge or implement hook telemetry itself.
Claude lifecycle parity, companion discipline skill scaffolding, payout
readback, and downstream override migration are now explicitly in scope.
Hook telemetry is tracked in `r1-5-hook-enforcement-telemetry`.
